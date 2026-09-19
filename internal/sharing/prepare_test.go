package sharing_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/sharing"
	"github.com/bharm16/readmit/internal/testrunner"
)

func write(t *testing.T, p string, b []byte) {
	t.Helper()
	if e := os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func fixture(t *testing.T) (sharing.Request, string) {
	t.Helper()
	root := t.TempDir()
	now := time.Now().UTC()
	inputs := []bundle.Input{}
	for _, name := range []string{"booking", "reschedule"} {
		raw, e := os.ReadFile("../../testdata/fixtures/redact-" + name + ".mllp")
		if e != nil {
			t.Fatal(e)
		}
		inputs = append(inputs, bundle.Input{Path: "PLANTED-NAME-" + name, Data: raw, Options: hl7.Options{Format: hl7.MLLP}})
	}
	source := filepath.Join(root, "original.case")
	if _, e := bundle.Write(source, inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now}); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"spec", "policy", "inventory"} {
		raw, e := os.ReadFile("../../testdata/fixtures/redact-" + name + ".json")
		if e != nil {
			t.Fatal(e)
		}
		if name == "spec" {
			var spec map[string]any
			if e := json.Unmarshal(raw, &spec); e != nil {
				t.Fatal(e)
			}
			spec["name"] = "<script>PLANTED-SCRIPT</script>"
			raw, e = json.Marshal(spec)
			if e != nil {
				t.Fatal(e)
			}
		}
		write(t, filepath.Join(root, name+".json"), raw)
	}
	review, private := filepath.Join(root, "review"), filepath.Join(root, "private")
	if _, e := redact.Create(context.Background(), redact.Request{CasePath: source, SpecPath: filepath.Join(root, "spec.json"), PolicyPath: filepath.Join(root, "policy.json"), InventoryPath: filepath.Join(root, "inventory.json"), Output: review, LocalState: private}); e != nil {
		t.Fatal(e)
	}
	policy := filepath.Join(root, "sharing.json")
	write(t, policy, []byte(`{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["local-file","customer-hub-download"],"max_bytes":4096}`))
	return sharing.Request{Source: review, Kind: "derived-review", Private: private, Policy: policy}, root
}
func TestReviewedSupportExcludesEvidenceAndInvalidatesChangedInputs(t *testing.T) {
	r, root := fixture(t)
	ctx := context.Background()
	candidate, e := sharing.Prepare(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	mutated := candidate.Bytes()
	mutated[0] = 0
	decoded, e := sharing.Decode(candidate.Bytes())
	if e != nil || decoded.Outcome != "reviewed-extract-only" || decoded.ExternalEquivalence != "declined" {
		t.Fatalf("summary: %+v %v", decoded, e)
	}
	output := filepath.Join(root, "support")
	if e = candidate.Publish(ctx, candidate.Identity(), output); e != nil {
		t.Fatal(e)
	}
	if _, e = sharing.Open(output); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"support.json", "event.json", "identity.sha256"} {
		raw, e := os.ReadFile(filepath.Join(output, name))
		if e != nil || bytes.Contains(raw, []byte("PLANTED")) || bytes.Contains(raw, []byte(root)) {
			t.Fatal("support leaked source content")
		}
	}
	if e = candidate.Publish(ctx, candidate.Identity(), output); e == nil {
		t.Fatal("overwrote existing output")
	}
	for _, surface := range []string{"messages", "names", "notes", "specs", "run-values", "reports", "caches", "logs"} {
		t.Run(surface, func(t *testing.T) {
			// Every unlisted source surface blocks the public boundary, even if named by
			// an operator. No general recursive support collector exists.
			bad := r
			bad.Kind = surface
			bad.Source = filepath.Join(root, surface)
			write(t, bad.Source, []byte("PLANTED-"+surface))
			if _, e := sharing.Prepare(ctx, bad); e == nil {
				t.Fatal("arbitrary content admitted")
			}
		})
	}
	for _, name := range []string{"spec.json", "policy.json", "inventory.json"} {
		t.Run("changed-"+name, func(t *testing.T) {
			p := filepath.Join(root, name)
			raw, _ := os.ReadFile(p)
			write(t, p, append(raw, ' '))
			defer write(t, p, raw)
			if e := candidate.Publish(ctx, candidate.Identity(), filepath.Join(root, "changed")); e == nil {
				t.Fatal("old approval survived source change")
			}
		})
	}
	raw, _ := os.ReadFile(r.Policy)
	write(t, r.Policy, append(raw, ' '))
	if e = candidate.Publish(ctx, candidate.Identity(), filepath.Join(root, "changed-policy")); e == nil {
		t.Fatal("old approval survived sharing policy change")
	}
}
func TestReportSupportAndPublicCLIRefuseUnsafePathsAndEgress(t *testing.T) {
	r, root := fixture(t)
	ctx := context.Background()
	packet := filepath.Join(root, "retained")
	proof := filepath.Join(r.Private, "original-proof", "baseline", "result")
	if _, e := report.Assemble(ctx, report.RetainedInput{Case: filepath.Join(root, "original.case"), Spec: filepath.Join(proof, "spec.json"), Current: proof}, packet); e != nil {
		t.Fatal(e)
	}
	// Prove the corpus is present in legitimate verified evidence, not merely
	// in unsupported kind labels. Those values must survive source retention
	// while never reaching any reviewed support output or security event.
	original, e := bundle.Open(filepath.Join(root, "original.case"))
	if e != nil {
		t.Fatal(e)
	}
	message, e := original.Raw("s0001-e000001")
	if e != nil {
		t.Fatal(e)
	}
	for _, marker := range []string{"PLANTED-PATIENT-7391", "PLANTED-NAME-ORCHID", "PLANTED-NTE-ALDER"} {
		if !bytes.Contains(message, []byte(marker)) {
			t.Fatal("ineffective message/name/note corpus", marker)
		}
	}
	run, e := testrunner.Open(proof)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(run.Spec.Name, "PLANTED-SCRIPT") || !strings.Contains(run.Spec.Setup.ResetInstructions, "PLANTED-RESET-SPRUCE") {
		t.Fatal("ineffective spec/note corpus")
	}
	sent, e := run.Run.Raw(run.Run.Events[0].Sent)
	if e != nil || !bytes.Contains(sent, []byte("PLANTED-PATIENT-7391")) {
		t.Fatal("ineffective run-value corpus")
	}
	resultJSON, e := json.Marshal(run.Result)
	if e != nil || !bytes.Contains(resultJSON, []byte("PLANTED-PATIENT-7391")) {
		t.Fatal("ineffective result-value corpus")
	}
	portable := filepath.Join(root, "portable")
	if _, e := report.ExportReview(ctx, packet, portable); e != nil {
		t.Fatal(e)
	}
	reportReview, e := report.OpenReview(ctx, portable)
	if e != nil {
		t.Fatal(e)
	}
	reportJSON, e := reportReview.Render("json")
	if e != nil || !bytes.Contains(reportJSON, []byte("PLANTED-PATIENT-7391")) || !bytes.Contains(reportJSON, []byte("PLANTED-SCRIPT")) {
		t.Fatal("ineffective report corpus")
	}
	derivedRequest := r
	for kind, path := range map[string]string{"retained-packet": packet, "portable-review": portable, "derived-review": r.Source} {
		r.Kind = kind
		r.Source = path
		c, e := sharing.Prepare(ctx, r)
		if e != nil {
			t.Fatal(e)
		}
		var published, security bytes.Buffer
		output := filepath.Join(root, "published-"+kind)
		args := []string{"share", path, "--kind", kind, "--policy", r.Policy, "--approve", c.Identity(), "--output", output}
		if kind == "derived-review" {
			args = append(args, "--local-state", derivedRequest.Private)
		}
		if e := cli.Execute("test", args, &published, &security); e != nil {
			t.Fatal("public publication", e)
		}
		if _, e := sharing.Open(output); e != nil {
			t.Fatal("published bundle", e)
		}
		for _, name := range []string{"support.json", "event.json", "identity.sha256"} {
			data, e := os.ReadFile(filepath.Join(output, name))
			if e != nil {
				t.Fatal(e)
			}
			published.Write(data)
		}
		published.Write(security.Bytes())
		for _, marker := range []string{"PLANTED-PATIENT-7391", "PLANTED-NAME-ORCHID", "PLANTED-NTE-ALDER", "PLANTED-RESET-SPRUCE", "PLANTED-SCRIPT", root} {
			if bytes.Contains(published.Bytes(), []byte(marker)) {
				t.Fatal("published source surface", kind)
			}
		}
		if !bytes.Contains(security.Bytes(), []byte(`"action":"published"`)) {
			t.Fatal("missing publication event")
		}
		s, e := sharing.Decode(c.Bytes())
		if e != nil || (s.Outcome != "assertion_failure" && s.Outcome != "reviewed-extract-only") || s.ExternalEquivalence != "declined" {
			t.Fatal("changed evidence claim")
		}
		for _, out := range []string{filepath.Join(path, "nested"), "https://PLANTED.invalid/upload"} {
			if e = c.Publish(ctx, c.Identity(), out); e == nil {
				t.Fatal("unsafe output accepted")
			}
		}
		if e = c.Publish(ctx, "wrong", filepath.Join(root, "wrong")); e == nil {
			t.Fatal("wrong approval accepted")
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if e = c.Publish(cancelled, c.Identity(), filepath.Join(root, "cancelled")); e == nil {
			t.Fatal("cancellation ignored")
		}
		// Canonical closed fields prevent active renderer strings and unproven proof.
		var doc map[string]any
		if e = json.Unmarshal(c.Bytes(), &doc); e != nil {
			t.Fatal(e)
		}
		doc["outcome"] = "<script>PLANTED</script>"
		raw, _ := json.Marshal(doc, json.Deterministic(true))
		if _, e = sharing.Decode(raw); e == nil {
			t.Fatal("active text admitted")
		}
	}
	rendered, e := report.OpenReview(ctx, portable)
	if e != nil {
		t.Fatal(e)
	}
	html, e := rendered.Render("html")
	if e != nil || bytes.Contains(html, []byte("<script>PLANTED-SCRIPT</script>")) {
		t.Fatal("active report content")
	}
	r.Kind = "portable-review"
	r.Source = portable
	var stdout, stderr bytes.Buffer
	if e := cli.Execute("test", []string{"share", portable, "--kind", r.Kind, "--policy", r.Policy}, &stdout, &stderr); e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(stderr.Bytes(), []byte(`"action":"prepared"`)) || bytes.Contains(stdout.Bytes(), []byte(root)) {
		t.Fatal("unsafe preview/event")
	}
	stdout.Reset()
	stderr.Reset()
	if e := cli.Execute("test", []string{"share", "PLANTED-PRIVATE-PATH", "--kind", "logs", "--policy", r.Policy}, &stdout, &stderr); e == nil {
		t.Fatal("unsafe CLI accepted")
	}
	if bytes.Contains(stderr.Bytes(), []byte("PLANTED")) || !bytes.Contains(stderr.Bytes(), []byte(`"action":"refused"`)) {
		t.Fatal("unsafe refusal event")
	}
	// A symbolic-link root and an archive are not supported evidence sources.
	alias := filepath.Join(root, "alias")
	if e := os.Symlink(portable, alias); e == nil {
		r.Source = alias
		if _, e = sharing.Prepare(ctx, r); e == nil {
			t.Fatal("symlink accepted")
		}
	}
	archive := filepath.Join(root, "archive.zip")
	write(t, archive, []byte("PK PLANTED ../escape"))
	r.Source = archive
	if _, e := sharing.Prepare(ctx, r); e == nil {
		t.Fatal("archive accepted")
	}
}

func TestSupportPartialWriteAndAlteredOutputStayUnapproved(t *testing.T) {
	r, root := fixture(t)
	ctx := context.Background()
	c, e := sharing.Prepare(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(root, "support")
	if e = c.Publish(ctx, c.Identity(), out); e != nil {
		t.Fatal(e)
	}
	marker, _ := os.ReadFile(filepath.Join(out, "identity.sha256"))
	if e = os.Remove(filepath.Join(out, "identity.sha256")); e != nil {
		t.Fatal(e)
	}
	if _, e = sharing.Open(out); e == nil {
		t.Fatal("incomplete output accepted")
	}
	if e = c.Publish(ctx, c.Identity(), out); e == nil {
		t.Fatal("incomplete output overwritten")
	}
	write(t, filepath.Join(out, "identity.sha256"), marker)
	write(t, filepath.Join(out, "support.json"), append(c.Bytes(), ' '))
	if _, e = sharing.Open(out); e == nil {
		t.Fatal("changed output accepted")
	}
	write(t, filepath.Join(out, "support.json"), c.Bytes())
	write(t, filepath.Join(out, "event.json"), []byte(`{"schema":"readmit-sharing-event/v1","action":"PLANTED"}`))
	if _, e = sharing.Open(out); e == nil {
		t.Fatal("changed security event accepted")
	}
	// Recovery is a new independently approved directory, not a blind resume.
	if e = c.Publish(ctx, c.Identity(), filepath.Join(root, "retry")); e != nil {
		t.Fatal(e)
	}
	// A value planted beside a legitimate report/review is refused, not skipped.
	for _, surface := range []string{"notes", "caches", "logs"} {
		p := filepath.Join(r.Source, surface)
		write(t, p, []byte("PLANTED"))
		if _, e = sharing.Prepare(ctx, r); e == nil {
			t.Fatal("unreviewed extra file ignored")
		}
		if e = os.Remove(p); e != nil {
			t.Fatal(e)
		}
	}
}
