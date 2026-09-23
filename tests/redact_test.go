package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

func redactFixture(t *testing.T) redact.Request {
	t.Helper()
	dir := t.TempDir()
	request := redact.Request{CasePath: filepath.Join(dir, "original.case"), SpecPath: filepath.Join(dir, "spec.json"), PolicyPath: filepath.Join(dir, "policy.json"), InventoryPath: filepath.Join(dir, "inventory.json"), Output: filepath.Join(dir, "review"), LocalState: filepath.Join(dir, "private")}
	var inputs []bundle.Input
	for _, name := range []string{"booking", "reschedule"} {
		raw, err := os.ReadFile("../testdata/fixtures/redact-" + name + ".mllp")
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, bundle.Input{Path: filepath.Join(dir, "PLANTED-FILENAME-CEDAR-"+name+".mllp"), Data: raw, Options: hl7.Options{Format: hl7.MLLP}})
	}
	imported := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	if _, err := bundle.Write(request.CasePath, inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported}); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"spec": request.SpecPath, "policy": request.PolicyPath, "inventory": request.InventoryPath} {
		raw, err := os.ReadFile("../testdata/fixtures/redact-" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return request
}

func redactJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func redactField(t *testing.T, b *bundle.Bundle, id, path string) string {
	t.Helper()
	raw, err := b.Raw(id)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := hl7.Parse(raw, hl7.Options{})
	if err != nil {
		t.Fatal(err)
	}
	selector, err := hl7.ParseSelector(path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := doc.Select(0, selector)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := hl7.Decode(doc.Bytes(value.Span), doc.Messages[0].Delimiters)
	if err != nil {
		t.Fatal(err)
	}
	return string(decoded)
}

func redactOriginalArtifacts(t *testing.T, request redact.Request) []string {
	t.Helper()
	dir := filepath.Dir(request.CasePath)
	address := freeLoopbackAddress(t, "tcp4")
	plan, err := replay.Prepare(request.CasePath, replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "100ms", MessageTimeout: "100ms", MaxACKBytes: 4096}, replay.Options{Occurrences: []string{"s0001-e000001", "s0002-e000001"}, Transformations: []replay.Transformation{{Name: "rebase-control-ids"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := replay.Execute(context.Background(), plan, filepath.Join(dir, "original-run"))
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Manifest.Changes) != 2 {
		t.Fatal("fixture must plant two original replay-only values")
	}
	for _, change := range run.Manifest.Changes {
		if !strings.HasPrefix(string(change.New), "READMIT00000") {
			t.Fatal("unexpected replay fixture")
		}
	}
	report, err := diagnose.Run(request.CasePath, diagnose.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	report.Findings = append(report.Findings, diagnose.Finding{ID: "f-planted", Summary: "PLANTED-DIAG-MAPLE"})
	redactJSON(t, filepath.Join(dir, "PLANTED-DIAG-FILENAME.json"), report)
	inventory := readStrictDocument[redact.Inventory](t, request.InventoryPath)
	inventory.Artifacts = []redact.OriginalArtifact{{Kind: "run", Path: "original-run"}, {Kind: "diagnosis-json", Path: "PLANTED-DIAG-FILENAME.json"}}
	redactJSON(t, request.InventoryPath, inventory)
	return []string{string(run.Manifest.Changes[0].New), string(run.Manifest.Changes[1].New)}
}

func TestRedactExecutableGeneratesOnlyDerivedProofAndNoPlantedValues(t *testing.T) {
	request := redactFixture(t)
	newOnly := redactOriginalArtifacts(t, request)
	before := treeOf(t, request.CasePath)
	stdout, stderr, err := run(t, "redact", request.CasePath, "--spec", request.SpecPath, "--policy", request.PolicyPath, "--inventory", request.InventoryPath, "--local-state", request.LocalState, "--output", request.Output)
	if err != nil {
		t.Fatalf("redact: %v %s %s", err, stdout, stderr)
	}
	review, err := redact.OpenReview(request.Output)
	if err != nil || review.State != "ready-for-approval" {
		t.Fatalf("review: %+v %v", review, err)
	}
	if !slices.Equal(review.RequiredFailures, []int{1, 2}) || !slices.Equal(review.OriginalFailedAssertions, []int{1, 2}) {
		t.Fatal("original assertion failure was not bound to approval")
	}
	packet := filepath.Join(filepath.Dir(request.Output), "packet")
	out, diagnostic, err := run(t, "redact", "export", request.Output, "--local-state", request.LocalState, "--approve", review.Identity, "--output", packet)
	if err != nil {
		t.Fatalf("export: %v %s %s", err, out, diagnostic)
	}
	manifest := readStrictDocument[redact.ExportManifest](t, filepath.Join(packet, "export-review.json"))
	if manifest.Proof.BaselineStatus != testrunner.AssertionFailure || manifest.Proof.PostfixStatus != testrunner.Pass || !slices.Equal(manifest.Proof.FailedAssertions, []int{1, 2}) {
		t.Fatal("packet did not preserve the agreed defect")
	}
	for _, mode := range []string{"baseline", "postfix"} {
		decisions, err := filepath.Glob(filepath.Join(request.LocalState, "derived-proof-*."+mode+".decision.json"))
		if err != nil || len(decisions) != 1 {
			t.Fatalf("private decision for %s: %v %v", mode, decisions, err)
		}
		if decision := readDecision(t, decisions[0]); !decision.Allowed || !decision.ExplicitSend {
			t.Fatalf("private proof decision: %+v", decision)
		}
		if _, err := os.Stat(filepath.Join(packet, "proof", mode, "result.decision.json")); !os.IsNotExist(err) {
			t.Fatal("local policy decision entered exported evidence")
		}
		artifact, err := testrunner.Open(filepath.Join(packet, "proof", mode, "result"))
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if mode == "baseline" {
			want = 2
		}
		if len(artifact.FinalObservation.Records) != want || len(artifact.Run.Manifest.Changes) != 0 || artifact.Result.InputBundleIdentity != review.DerivedIdentity {
			t.Fatal("new proof has wrong ledger, transformations, or source identity")
		}
		if artifact.Result.Assertions[2].Status != "passed" || artifact.Result.Assertions[3].Status != "passed" {
			t.Fatal("ACK failure masked the ledger defect")
		}
	}
	derived, err := bundle.Open(filepath.Join(packet, "case"))
	if err != nil {
		t.Fatal(err)
	}
	if derived.Manifest.Schema != "readmit-case/v3" || derived.Manifest.Provenance.Mode != bundle.Derived {
		t.Fatal("customer-derived evidence mislabeled as synthetic")
	}
	original, err := bundle.Open(request.CasePath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original.Correlations, derived.Correlations) {
		t.Fatal("rewriting broke captured ACK correlations")
	}
	patient := redactField(t, derived, "s0001-e000001", "PID-3.1")
	if patient == "PLANTED-PATIENT-7391" || patient != redactField(t, derived, "s0002-e000001", "PID-3.1") {
		t.Fatal("patient surrogate is inconsistent")
	}
	for _, message := range []string{"s0001-e000001", "s0002-e000001"} {
		startOld, _ := time.Parse("20060102150405-0700", redactField(t, original, message, "SCH-11.4"))
		startNew, _ := time.Parse("20060102150405-0700", redactField(t, derived, message, "SCH-11.4"))
		dobOld, _ := time.Parse("20060102150405-0700", redactField(t, original, message, "PID-7"))
		dobNew, _ := time.Parse("20060102150405-0700", redactField(t, derived, message, "PID-7"))
		if startNew.Sub(startOld) == 0 || startNew.Sub(startOld) != dobNew.Sub(dobOld) {
			t.Fatal("date transformation did not preserve per-patient intervals")
		}
	}
	forbidden := append([]string{"PLANTED-", "AUTH-ONE", "PRIVATE-APP", "PRIVATE-FACILITY", "patient_key", "\"days\":", "original-proof"}, newOnly...)
	files := treeOf(t, packet)
	for name, raw := range files {
		for _, value := range forbidden {
			if strings.Contains(name, value) || bytes.Contains(raw, []byte(value)) || bytes.Contains(raw, []byte(base64.StdEncoding.EncodeToString([]byte(value)))) {
				t.Fatalf("residual in %s", name)
			}
		}
		if bytes.Contains(raw, []byte(request.LocalState)) || bytes.Contains(raw, []byte(request.CasePath)) {
			t.Fatalf("local path in %s", name)
		}
	}
	if len(manifest.Review.Coverage) != 18 || !slices.Contains(manifest.Review.Uncovered, "face-images") || !slices.Contains(manifest.Review.Uncovered, "dates-and-ages") || manifest.Residual.Limitations == "" {
		t.Fatal("coverage overclaims or misses categories")
	}
	if !reflect.DeepEqual(before, treeOf(t, request.CasePath)) {
		t.Fatal("source bundle changed")
	}
	private, err := os.ReadFile(filepath.Join(request.LocalState, "state.json"))
	if err != nil || !bytes.Contains(private, []byte("PLANTED-PATIENT-7391")) || !bytes.Contains(private, []byte("\"days\":")) {
		t.Fatal("local mapping material was not retained privately")
	}
	if strings.Contains(stdout+stderr+out+diagnostic, "PLANTED-") {
		t.Fatal("CLI disclosed planted source text")
	}
}

// A derived case is readmit-case/v3, which has no collection member. Redaction
// must refuse collected receiver evidence rather than silently drop its record.
func TestRedactRefusesCollectedReceiverEvidence(t *testing.T) {
	request := redactFixture(t)
	if err := os.RemoveAll(request.CasePath); err != nil {
		t.Fatal(err)
	}
	policy, err := collection.DecodePolicy([]byte(collectAnyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	var inputs []bundle.Input
	sessions := []collection.Session{}
	for i, name := range []string{"booking", "reschedule"} {
		raw, err := os.ReadFile("../testdata/fixtures/redact-" + name + ".mllp")
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, bundle.Input{Data: raw, Options: hl7.Options{Format: hl7.MLLP}, Observations: map[int]bundle.Observation{}})
		sessions = append(sessions, collection.Session{SessionID: fmt.Sprintf("c%04d", i+1), SourceID: fmt.Sprintf("s%04d", i+1), Label: policy.SourceLabel})
	}
	record := collection.Record{
		Schema: collection.Schema, SessionID: "0123456789abcdef0123456789abcdef", Policy: policy,
		ApplicationProcessing: collection.NoApplicationProcessing, Sessions: sessions, Received: []collection.Received{},
	}
	if _, err := bundle.WriteCollected(request.CasePath, inputs, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC), record); err != nil {
		t.Fatal(err)
	}
	review, err := redact.Create(context.Background(), request)
	if err == nil || review != nil {
		t.Fatal("redaction derived a case from collected receiver evidence")
	}
	if strings.Contains(err.Error(), request.CasePath) || len(err.Error()) > 300 {
		t.Fatalf("unsafe redaction diagnostic: %v", err)
	}
}

func TestRedactBlocksEachUnreviewedSurface(t *testing.T) {
	for _, surface := range []string{"pid", "nte", "err", "filename", "metadata", "spec", "diagnosis", "run", "unknown", "embedded"} {
		t.Run(surface, func(t *testing.T) {
			request := redactFixture(t)
			redactOriginalArtifacts(t, request)
			policy := readStrictDocument[redact.Policy](t, request.PolicyPath)
			want := ""
			switch surface {
			case "pid":
				policy.Fields = slices.DeleteFunc(policy.Fields, func(r redact.FieldRule) bool { return r.Selector == "PID-3.1" })
				want = "PID[1]-3"
			case "nte":
				policy.Fields = slices.DeleteFunc(policy.Fields, func(r redact.FieldRule) bool { return r.Selector == "NTE-3" })
				want = "NTE[1]-3"
			case "err":
				policy.RemoveSegments = slices.DeleteFunc(policy.RemoveSegments, func(s string) bool { return s == "ERR" })
				want = "ERR[1]-1"
			case "unknown":
				policy.RemoveSegments = slices.DeleteFunc(policy.RemoveSegments, func(s string) bool { return s == "ZXX" })
				want = "ZXX[1]"
			case "embedded":
				policy.RemoveSegments = slices.DeleteFunc(policy.RemoveSegments, func(s string) bool { return s == "OBX" })
				want = "OBX[1]-5"
			case "filename":
				policy.PacketPolicies = slices.DeleteFunc(policy.PacketPolicies, func(s string) bool { return s == redact.Filenames })
				want = "/path"
			case "metadata":
				policy.PacketPolicies = slices.DeleteFunc(policy.PacketPolicies, func(s string) bool { return s == redact.Metadata })
				want = "provenance-and-observed-times"
			case "spec":
				policy.SpecBindings = nil
				want = "spec/assertions/2/expected/records/1/patient_id/value"
			case "diagnosis":
				policy.PacketPolicies = slices.DeleteFunc(policy.PacketPolicies, func(s string) bool { return s == redact.Diagnosis })
				want = "original-artifacts/a0002/findings/"
			case "run":
				policy.PacketPolicies = slices.DeleteFunc(policy.PacketPolicies, func(s string) bool { return s == redact.Rerun })
				want = "original-artifacts/a0001/manifest/changes/1/new"
			}
			redactJSON(t, request.PolicyPath, policy)
			review, err := redact.Create(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if review.State != "blocked" {
				t.Fatal("unhandled surface was approved")
			}
			found := false
			for _, finding := range review.Findings {
				if !finding.Resolved && strings.Contains(finding.Location, want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing located finding for %s", want)
			}
			packet := filepath.Join(filepath.Dir(request.Output), "packet")
			if _, err := redact.Export(context.Background(), redact.ExportRequest{ReviewPath: request.Output, LocalState: request.LocalState, Approval: review.Identity, Output: packet}); err == nil {
				t.Fatal("blocked review exported")
			}
			if _, err := os.Stat(packet); !os.IsNotExist(err) {
				t.Fatal("blocked export created output")
			}
		})
	}
}

func TestRedactRejectsStaleApprovalAndChangedInputs(t *testing.T) {
	for _, change := range []string{"approval", "policy", "inventory", "private", "derived"} {
		t.Run(change, func(t *testing.T) {
			request := redactFixture(t)
			review, err := redact.Create(context.Background(), request)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			if review.State != "ready-for-approval" {
				t.Fatalf("setup: review %s; original proof: %q; %+v", review.State, review.OriginalProofFailure, review)
			}
			approval := review.Identity
			path := ""
			switch change {
			case "approval":
				approval = strings.Repeat("0", 64)
			case "policy":
				path = request.PolicyPath
			case "inventory":
				path = request.InventoryPath
			case "private":
				path = filepath.Join(request.LocalState, "state.json")
			case "derived":
				path = filepath.Join(request.Output, "spec.json")
			}
			if path != "" {
				file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				file.WriteString("\n")
				file.Close()
			}
			packet := filepath.Join(filepath.Dir(request.Output), "packet")
			if _, err := redact.Export(context.Background(), redact.ExportRequest{ReviewPath: request.Output, LocalState: request.LocalState, Approval: approval, Output: packet}); err == nil {
				t.Fatal("stale material exported")
			}
			if _, err := os.Stat(packet); !os.IsNotExist(err) {
				t.Fatal("refused export left packet")
			}
		})
	}
}

func TestRedactOriginalProofRejectsWrongAgreedFailureSet(t *testing.T) {
	request := redactFixture(t)
	policy := readStrictDocument[redact.Policy](t, request.PolicyPath)
	policy.RequiredFailures = []int{1}
	redactJSON(t, request.PolicyPath, policy)
	review, err := redact.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != "blocked" || len(review.OriginalFailedAssertions) != 0 {
		t.Fatal("unrelated or additional original failures were accepted")
	}
	// The located finding stays the fixed one; the cause is said to the caller
	// only, naming the step and the actual verdicts but no path or value.
	unresolved := []exportreview.Finding{}
	for _, finding := range review.Findings {
		if !finding.Resolved {
			unresolved = append(unresolved, finding)
		}
	}
	if !reflect.DeepEqual(unresolved, []exportreview.Finding{{Location: "proof/original-assertions", Class: "other-unique-identifiers", Reason: "original-fixture-proof-failed", Resolved: false}}) {
		t.Fatalf("proof failure changed its finding: %+v", unresolved)
	}
	cause := "fixture proof did not preserve the exact agreed failures [1] and full fixed pass: baseline assertion_failure with failed assertions [1 2]; postfix pass"
	if review.OriginalProofFailure != cause || strings.ContainsAny(review.OriginalProofFailure, `/\`) {
		t.Fatalf("proof failure does not say why: %q", review.OriginalProofFailure)
	}
	for _, name := range []string{filepath.Join(request.Output, "review.json"), filepath.Join(request.LocalState, "state.json")} {
		raw, err := os.ReadFile(name)
		if err != nil || bytes.Contains(raw, []byte("did not preserve")) {
			t.Fatalf("proof failure cause entered %s: %v", filepath.Base(name), err)
		}
	}
	command := redactFixture(t)
	redactJSON(t, command.PolicyPath, policy)
	stdout, stderr, err := run(t, "redact", command.CasePath, "--spec", command.SpecPath, "--policy", command.PolicyPath, "--inventory", command.InventoryPath, "--local-state", command.LocalState, "--output", command.Output)
	if err == nil || !strings.Contains(stderr, "Original fixture proof failed: "+cause+"\n") || strings.Contains(stdout+stderr, filepath.Dir(command.CasePath)) {
		t.Fatalf("command did not report the failed proof step privately: %v %s %s", err, stdout, stderr)
	}
}

func TestRedactRejectsUnknownInventoryAndUnsafeDestination(t *testing.T) {
	for _, kind := range []string{"unknown", "inside-source", "inside-source-symlink"} {
		t.Run(kind, func(t *testing.T) {
			request := redactFixture(t)
			if kind == "unknown" {
				inventory := readStrictDocument[redact.Inventory](t, request.InventoryPath)
				inventory.Artifacts = []redact.OriginalArtifact{{Kind: "arbitrary-archive", Path: "source.zip"}}
				redactJSON(t, request.InventoryPath, inventory)
			} else if kind == "inside-source" {
				request.Output = filepath.Join(request.CasePath, "review")
			} else {
				alias := filepath.Join(filepath.Dir(request.CasePath), "alias")
				if err := os.Symlink(request.CasePath, alias); err != nil {
					t.Skip("symlinks unavailable")
				}
				request.Output = filepath.Join(alias, "review")
			}
			if _, err := redact.Create(context.Background(), request); err == nil {
				t.Fatal("unsupported inventory or nested output accepted")
			}
			if _, err := bundle.Open(request.CasePath); err != nil {
				t.Fatal("source changed on refusal")
			}
		})
	}
}

func TestRedactPreservesScopedIDsAcrossEscapesWithoutMergingAuthorities(t *testing.T) {
	request := redactFixture(t)
	original, err := bundle.Open(request.CasePath)
	if err != nil {
		t.Fatal(err)
	}
	var inputs []bundle.Input
	for _, authority := range []string{"AUTH-ONE", "AUTH-TWO"} {
		for _, source := range original.Manifest.Sources {
			var raw []byte
			for _, event := range original.Events {
				if event.SourceID == source.ID {
					part, err := original.Raw(event.ID)
					if err != nil {
						t.Fatal(err)
					}
					raw = append(raw, part...)
				}
			}
			raw = bytes.ReplaceAll(raw, []byte("AUTH-ONE"), []byte(authority))
			if source.ID == "s0002" {
				raw = bytes.ReplaceAll(raw, []byte("PLANTED-PATIENT-7391"), []byte(`PLANTED-PATIENT-\X37333931\`))
			}
			inputs = append(inputs, bundle.Input{Path: "invented-scoped-source", Data: raw})
		}
	}
	request.CasePath = filepath.Join(filepath.Dir(request.CasePath), "scoped.case")
	if _, err := bundle.Write(request.CasePath, inputs, original.Manifest.Provenance); err != nil {
		t.Fatal(err)
	}
	spec, err := testrunner.ReadSpec(request.SpecPath)
	if err != nil {
		t.Fatal(err)
	}
	spec.Input.Case = "scoped.case"
	redactJSON(t, request.SpecPath, spec)
	review, err := redact.Create(context.Background(), request)
	if err != nil || review.State != "ready-for-approval" {
		t.Fatalf("scoped review: %+v %v", review, err)
	}
	derived, err := bundle.Open(filepath.Join(request.Output, "case"))
	if err != nil {
		t.Fatal(err)
	}
	first := redactField(t, derived, "s0001-e000001", "PID-3.1")
	second := redactField(t, derived, "s0003-e000001", "PID-3.1")
	if first == second || first != redactField(t, derived, "s0002-e000001", "PID-3.1") || second != redactField(t, derived, "s0004-e000001", "PID-3.1") {
		t.Fatal("scope collapsed or equivalent decoded identifiers diverged")
	}
}

func TestRedactLateProofAndResidualFailuresRemainLocatedAndPrivate(t *testing.T) {
	for _, failure := range []string{"proof", "residual", "manifest"} {
		t.Run(failure, func(t *testing.T) {
			request := redactFixture(t)
			if failure == "proof" {
				policy := readStrictDocument[redact.Policy](t, request.PolicyPath)
				policy.Fields = slices.DeleteFunc(policy.Fields, func(rule redact.FieldRule) bool { return rule.Selector == "MSH-9" })
				value := "S12"
				policy.Fields = append(policy.Fields, redact.FieldRule{Selector: "MSH-9.1", Policy: redact.Retain, Class: "structural", Allowed: []string{"SIU", "ACK"}}, redact.FieldRule{Selector: "MSH-9.2", Policy: redact.Replace, Class: "structural", Replacement: &value})
				redactJSON(t, request.PolicyPath, policy)
			} else {
				inventory := readStrictDocument[redact.Inventory](t, request.InventoryPath)
				value := "READMITACK000001"
				if failure == "manifest" {
					value = "readmit-derived-export/v1"
				}
				inventory.ResidualValues = append(inventory.ResidualValues, value)
				redactJSON(t, request.InventoryPath, inventory)
			}
			review, err := redact.Create(context.Background(), request)
			if err != nil || review.State != "ready-for-approval" {
				t.Fatalf("initial review: %+v %v", review, err)
			}
			packet := filepath.Join(filepath.Dir(request.Output), "packet")
			_, exported := redact.Export(context.Background(), redact.ExportRequest{ReviewPath: request.Output, LocalState: request.LocalState, Approval: review.Identity, Output: packet})
			if exported == nil {
				t.Fatal("late failure exported")
			}
			// A failed derived proof says which step failed, naming no path.
			cause := "; derived fixture proof: fixture proof did not preserve the exact agreed failures [1 2] and full fixed pass: baseline "
			_, explanation, explained := strings.Cut(exported.Error(), "; derived fixture proof: ")
			if failure == "proof" && (!strings.Contains(exported.Error(), cause) || !explained || strings.ContainsAny(explanation, `/\`) || strings.Contains(exported.Error(), filepath.Dir(request.CasePath))) {
				t.Fatalf("derived proof failure did not name its step privately: %v", exported)
			}
			if _, err := os.Stat(packet); !os.IsNotExist(err) {
				t.Fatal("failed proof or residual scan produced an export")
			}
			attempts, err := filepath.Glob(filepath.Join(request.LocalState, "derived-proof-*", "attempt-review.json"))
			if err != nil || len(attempts) != 1 {
				t.Fatal("late refusal lost its located review")
			}
			raw, err := os.ReadFile(attempts[0])
			if err != nil || !bytes.Contains(raw, []byte(`"state":"blocked"`)) || !bytes.Contains(raw, []byte(`"resolved":false`)) || bytes.Contains(raw, []byte("did not preserve")) {
				t.Fatal("attempt review concealed unresolved findings or recorded the proof's explanation")
			}
		})
	}
}

func TestRedactPublicReviewResidualBecomesALocatedBlockedReview(t *testing.T) {
	request := redactFixture(t)
	inventory := readStrictDocument[redact.Inventory](t, request.InventoryPath)
	inventory.ResidualValues = append(inventory.ResidualValues, "limited-policy-coverage")
	redactJSON(t, request.InventoryPath, inventory)
	review, err := redact.Create(context.Background(), request)
	if err != nil || review.State != "blocked" {
		t.Fatalf("review-only residual: %+v %v", review, err)
	}
	verified, err := redact.OpenReview(request.Output)
	if err != nil || !slices.Contains(verified.Residual.Locations, "review.json") {
		t.Fatal("public manifest residual lacks a verified located refusal")
	}
}

func TestRedactLiteralMismatchFreeTextRetentionAndUnresolvedScopeCannotBeApproved(t *testing.T) {
	for _, problem := range []string{"literal-mismatch", "free-text", "overlap", "unknown-patient", "control-replacement"} {
		t.Run(problem, func(t *testing.T) {
			request := redactFixture(t)
			policy := readStrictDocument[redact.Policy](t, request.PolicyPath)
			switch problem {
			case "literal-mismatch":
				spec, err := testrunner.ReadSpec(request.SpecPath)
				if err != nil {
					t.Fatal(err)
				}
				(*spec.Assertions[1].Expected.Records)[0].PatientID.Value = "UNRELATED-LITERAL"
				redactJSON(t, request.SpecPath, spec)
			case "free-text":
				for i := range policy.Fields {
					if policy.Fields[i].Selector == "NTE-3" {
						policy.Fields[i] = redact.FieldRule{Selector: "NTE-3", Policy: redact.Retain, Class: "structural", Allowed: []string{"PLANTED-NTE-ALDER"}}
					}
				}
			case "overlap":
				policy.Fields = append(policy.Fields, redact.FieldRule{Selector: "PID-3", Policy: redact.Remove, Class: "medical-record-numbers"})
			case "unknown-patient":
				policy.Patient.Selector = "PID-4"
			case "control-replacement":
				value := "injected\x01control"
				policy.Fields = append(policy.Fields, redact.FieldRule{Selector: "PID-6", Policy: redact.Replace, Class: "names", Replacement: &value})
			}
			redactJSON(t, request.PolicyPath, policy)
			review, err := redact.Create(context.Background(), request)
			if err == nil && review.State != "blocked" {
				t.Fatal("invalid relationship or free text was approved")
			}
		})
	}
}

func TestRedactInventoryRevalidatesTheResolvedAliasParentArtifact(t *testing.T) {
	request := redactFixture(t)
	redactOriginalArtifacts(t, request)
	dir := filepath.Dir(request.CasePath)
	realParent := filepath.Join(dir, "real")
	if err := os.MkdirAll(filepath.Join(realParent, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(realParent, "nested"), filepath.Join(dir, "alias")); err != nil {
		t.Skip("symlinks unavailable")
	}
	realRun := filepath.Join(realParent, "inventoried-run")
	if err := os.Rename(filepath.Join(dir, "original-run"), realRun); err != nil {
		t.Fatal(err)
	}
	cleanedRun := filepath.Join(dir, "inventoried-run")
	if err := os.CopyFS(cleanedRun, os.DirFS(realRun)); err != nil {
		t.Fatal(err)
	}
	inventory := readStrictDocument[redact.Inventory](t, request.InventoryPath)
	// Keep the raw traversal: filepath.Join would lexically erase alias/.. .
	inventory.Artifacts[0].Path = "alias/../inventoried-run"
	redactJSON(t, request.InventoryPath, inventory)
	review, err := redact.Create(context.Background(), request)
	if err != nil || review.State != "ready-for-approval" {
		t.Fatalf("alias inventory review: %+v %v", review, err)
	}
	file, err := os.OpenFile(filepath.Join(realRun, "payloads", "o000001-source.bin"), os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString("PLANTED-POST-REVIEW-MUTATION")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("cannot mutate inventoried fixture")
	}
	if _, err := replay.Open(realRun); err == nil {
		t.Fatal("real inventory artifact was not changed")
	}
	if _, err := replay.Open(cleanedRun); err != nil {
		t.Fatal("lexically cleaned copy should remain independently valid")
	}
	packet := filepath.Join(dir, "packet")
	if _, err := redact.Export(context.Background(), redact.ExportRequest{ReviewPath: request.Output, LocalState: request.LocalState, Approval: review.Identity, Output: packet}); err == nil {
		t.Fatal("export revalidated the lexically cleaned copy instead of the inventoried artifact")
	}
	if _, err := os.Stat(packet); !os.IsNotExist(err) {
		t.Fatal("changed inventoried artifact produced an export")
	}
}

func TestRedactInventoryRequiresExplicitArrayMembers(t *testing.T) {
	for _, member := range []string{"artifacts", "residual_values"} {
		for _, invalid := range []string{"missing", "null", "object", "string", "number", "boolean"} {
			t.Run(member+"/"+invalid, func(t *testing.T) {
				request := redactFixture(t)
				inventory := map[string]any{"schema": redact.InventorySchema, "complete": true, "artifacts": []any{}, "residual_values": []any{}}
				switch invalid {
				case "missing":
					delete(inventory, member)
				case "null":
					inventory[member] = nil
				case "object":
					inventory[member] = map[string]any{}
				case "string":
					inventory[member] = ""
				case "number":
					inventory[member] = 0
				case "boolean":
					inventory[member] = false
				}
				redactJSON(t, request.InventoryPath, inventory)
				if _, err := redact.Create(context.Background(), request); err == nil {
					t.Fatal("inventory accepted an omitted, null, or non-array required member")
				}
				for _, path := range []string{request.Output, request.LocalState} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatal("invalid inventory created review or private output")
					}
				}
			})
		}
	}
	t.Run("explicit-empty-arrays", func(t *testing.T) {
		request := redactFixture(t)
		redactJSON(t, request.InventoryPath, map[string]any{"schema": redact.InventorySchema, "complete": true, "artifacts": []any{}, "residual_values": []any{}})
		review, err := redact.Create(context.Background(), request)
		if err != nil || review.State != "ready-for-approval" {
			t.Fatalf("explicit empty arrays should be supported: %+v %v", review, err)
		}
	})
}
