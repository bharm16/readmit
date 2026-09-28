package report_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/report"
)

// transformPolicy maps every surface: the four transformable ones as given,
// every other one excluded.
func transformPolicy(t *testing.T, transform []string, names string, elements []report.ElementRule, columns []report.ColumnRule, parameters []report.ParameterRule) string {
	t.Helper()
	surfaces := map[string]string{}
	for _, s := range report.DisclosureSurfaces {
		surfaces[s] = "exclude"
	}
	for _, s := range transform {
		surfaces[s] = "transform"
	}
	raw, err := json.Marshal(report.TransformPolicy{Schema: report.TransformPolicySchema, Surfaces: surfaces, Names: names, Elements: elements, Columns: columns, Parameters: parameters})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

var transformAll = []string{report.SurfaceFHIRResource, report.SurfaceTypedDataset, report.SurfaceMapping, report.SurfaceHTTPExchange}

func pseudonymKey(t *testing.T) (string, report.PseudonymKey) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key")
	if err := report.WritePseudonymKey(path); err != nil {
		t.Fatal(err)
	}
	key, err := report.ReadPseudonymKey(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, key
}

// plantedPolicy discloses what a reviewer needs to follow the registration:
// the identifier system kept, the identifier value, key column, identity
// column and the search token pseudonymized, the family name redacted.
func plantedPolicy(t *testing.T, names string) string {
	return transformPolicy(t, transformAll, names,
		[]report.ElementRule{{Resource: "Patient", Path: "identifier.system", Action: "keep"}, {Resource: "Patient", Path: "identifier.value", Action: "pseudonymize"}},
		[]report.ColumnRule{{Dataset: "patients", Column: "key", Action: "pseudonymize"}, {Dataset: "patients", Column: "identity", Action: "pseudonymize"}, {Dataset: "patients", Column: "family", Action: "redact"}},
		[]report.ParameterRule{{Name: "identifier", Action: "pseudonymize"}})
}

func TestConnectedLabTransformedExtractKeepsRelationshipsAndNoOriginalValue(t *testing.T) {
	labLifecycles(t)
	h := connectedlab.New(t, "")
	h.Compile(plantedFlow(h, plantedExtension))
	_, planted := h.Run("planted")
	h.Compile(plantedFlow(h, "Bearer PLANTEDTOKEN4411abcdefgh"))
	_, leaked := h.Run("leaked")
	base := h.Lab.Base()
	h.Lab.Server().Close()
	h.Engine.Listener.Close()

	packet, _ := assemble(t, report.ConnectedInput{Current: planted})
	keyPath, key := pseudonymKey(t)
	policy := plantedPolicy(t, "authored")
	candidate, err := report.PrepareTransformedExtract(t.Context(), packet, policy, key)
	if err != nil || len(candidate.Blocked) != 0 {
		t.Fatal("a fully reviewed policy was blocked", err, candidate.Blocked)
	}
	output := filepath.Join(t.TempDir(), "extract")
	if err := candidate.Publish(t.Context(), candidate.Identity(), output); err != nil {
		t.Fatal(err)
	}
	x, err := report.OpenTransformedExtract(output)
	if err != nil {
		t.Fatal(err)
	}
	if x.EvidenceClass != report.EvidenceTransformed || x.Equivalence.State != report.EquivalenceUnverified || x.Residual.Status != "passed" || x.Residual.KnownValues == 0 || !strings.Contains(x.Derivation, "not an original observation") {
		t.Fatalf("the transformed extract does not state what it is: %+v", x)
	}

	t.Run("no original value leaves through any rendering", func(t *testing.T) {
		for _, name := range []string{"extract.json", "report.md", "report.html"} {
			raw, err := os.ReadFile(filepath.Join(output, name))
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{plantedIdentifier, plantedNarrative, plantedExtension, plantedFamily, "PLANTED", "UExBTlRFRC1BVFRBQ0gtNDQxMQ", base, "127.0.0.1", h.Root, "pat-1", "Synthetic lab patient"} {
				if bytes.Contains(raw, []byte(secret)) {
					t.Errorf("%s carries an original value: %q", name, secret)
				}
			}
		}
	})

	t.Run("pseudonyms keep references and identifiers consistent", func(t *testing.T) {
		phase := x.Evidence[0].Phases[0]
		if len(phase.Exchanges) != 1 || len(phase.Observations) != 1 || len(phase.Bindings) != 1 {
			t.Fatalf("transformed evidence is missing: %+v", phase)
		}
		exchange, observation := phase.Exchanges[0], phase.Observations[0]
		response, _ := exchange.Response.(map[string]any)
		request, _ := exchange.Request.(map[string]any)
		id, _ := response["id"].(string)
		identifier := func(res map[string]any) string {
			list, _ := res["identifier"].([]any)
			if len(list) != 1 {
				t.Fatalf("no identifier: %v", res)
			}
			entry := list[0].(map[string]any)
			if entry["system"] != "urn:readmit-lab:patient" {
				t.Fatalf("the kept system was not kept: %v", entry)
			}
			return entry["value"].(string)
		}
		business := identifier(response)
		if !strings.HasPrefix(id, "p-") || !strings.HasPrefix(business, "p-") || identifier(request) != business || exchange.Method != "POST" || exchange.Path != "Patient" || exchange.Status != 201 || exchange.Derivation != "transformed" {
			t.Fatalf("the exchange: %+v", exchange)
		}
		for _, excluded := range []string{"text", "extension", "photo", "name", "meta"} {
			if _, ok := response[excluded]; ok {
				t.Errorf("the response kept %s", excluded)
			}
		}
		if len(observation.Resources) != 1 || observation.Resources[0].(map[string]any)["id"] != id || identifier(observation.Resources[0].(map[string]any)) != business {
			t.Fatalf("the observed resource is not the same pseudonymized resource: %+v", observation.Resources)
		}
		fields := map[string]report.ReportField{}
		for _, f := range observation.Records[0] {
			fields[f.Column] = f
		}
		if fields["key"].Text != business || fields["identity"].Text != "Patient/"+id || fields["family"].State != "present" || fields["family"].Text != "" {
			t.Fatalf("typed records are not consistent with the resources: %+v", fields)
		}
		if phase.Bindings[0].Value != id || phase.Bindings[0].Variable != "patient-id" {
			t.Fatalf("the identity mapping is not consistent: %+v", phase.Bindings[0])
		}
	})

	t.Run("positions hide authored names", func(t *testing.T) {
		c, err := report.PrepareTransformedExtract(t.Context(), packet, plantedPolicy(t, "positions"), key)
		if err != nil || len(c.Blocked) != 0 {
			t.Fatal(err, c.Blocked)
		}
		raw, _ := c.Render("json")
		if bytes.Contains(raw, []byte(`"register"`)) || bytes.Contains(raw, []byte(`"patient-id"`)) {
			t.Fatal("authored names were shown in a positions-only extract")
		}
	})

	t.Run("unmapped, opaque, credential, changed or unapproved content blocks publication", func(t *testing.T) {
		partial := transformPolicy(t, []string{report.SurfaceFHIRResource}, "authored", nil, nil, nil)
		raw, _ := os.ReadFile(partial)
		var p report.TransformPolicy
		_ = json.Unmarshal(raw, &p)
		delete(p.Surfaces, report.SurfaceFHIRNarrative)
		raw, _ = json.Marshal(p)
		_ = os.WriteFile(partial, raw, 0o600)
		if c, err := report.PrepareTransformedExtract(t.Context(), packet, partial, key); err != nil || len(c.Blocked) != 1 || !strings.Contains(c.Blocked[0], report.SurfaceFHIRNarrative) {
			t.Fatal("an unmapped surface did not block", err)
		}
		for _, opaque := range []string{report.SurfaceFHIRNarrative, report.SurfaceFHIRExtension, report.SurfaceAttachment, report.SurfaceHTTPResponse, report.SurfaceHL7Transport, report.SurfaceValidator, report.SurfacePlan} {
			if _, err := report.DecodeTransformPolicy([]byte(`{"schema":"readmit-connected-disclosure-policy/v2","names":"positions","surfaces":{"` + opaque + `":"transform"},"elements":[],"columns":[],"parameters":[]}`)); err == nil {
				t.Errorf("a policy transformed the opaque %s surface", opaque)
			}
		}
		if _, err := report.DecodeTransformPolicy([]byte(`{"schema":"readmit-connected-disclosure-policy/v2","names":"positions","surfaces":{"credential-material":"exclude"},"elements":[],"columns":[],"parameters":[]}`)); err == nil {
			t.Fatal("a policy admitted credential material")
		}
		leakedPacket, _ := assemble(t, report.ConnectedInput{Current: leaked})
		if c, err := report.PrepareTransformedExtract(t.Context(), leakedPacket, policy, key); err != nil || !strings.Contains(strings.Join(c.Blocked, " "), "credential material") {
			t.Fatal("a retained token did not block the transformed extract", err)
		}
		// Approval binds the exact bytes: another key, another policy or a
		// guessed identity is refused.
		_, other := pseudonymKey(t)
		changed, _ := report.PrepareTransformedExtract(t.Context(), packet, policy, other)
		if changed.Identity() == candidate.Identity() {
			t.Fatal("pseudonyms do not depend on the customer-local key")
		}
		if err := changed.Publish(t.Context(), candidate.Identity(), filepath.Join(t.TempDir(), "x")); err == nil {
			t.Fatal("an extract under another key was published with an old approval")
		}
		if err := candidate.Publish(t.Context(), "0000000000000000000000000000000000000000000000000000000000000000", filepath.Join(t.TempDir(), "y")); err == nil {
			t.Fatal("an unapproved identity was published")
		}
		keep := transformPolicy(t, transformAll, "authored", []report.ElementRule{{Resource: "Patient", Path: "identifier.value", Action: "keep"}}, nil, nil)
		if c, _ := report.PrepareTransformedExtract(t.Context(), packet, keep, key); c.Identity() == candidate.Identity() {
			t.Fatal("a changed policy produced the approved bytes")
		}
	})

	t.Run("each version's reader refuses the other and a changed extract", func(t *testing.T) {
		if _, err := report.OpenExtract(output); err == nil {
			t.Fatal("the value-free reader accepted a transformed extract")
		}
		v1, err := report.PrepareExtract(t.Context(), packet, writePolicy(t, report.DisclosureSurfaces...))
		if err != nil {
			t.Fatal(err)
		}
		v1Output := filepath.Join(t.TempDir(), "v1")
		if err := v1.Publish(t.Context(), v1.Identity(), v1Output); err != nil {
			t.Fatal(err)
		}
		if _, err := report.OpenTransformedExtract(v1Output); err == nil {
			t.Fatal("the transformed reader accepted a value-free extract")
		}
		if _, err := report.OpenExtract(v1Output); err != nil {
			t.Fatal("the value-free extract no longer reads", err)
		}
		tampered := filepath.Join(t.TempDir(), "tampered")
		copyTree(t, output, tampered)
		raw, _ := os.ReadFile(filepath.Join(tampered, "extract.json"))
		raw = bytes.Replace(raw, []byte(`"urn:readmit-lab:patient"`), []byte(`"`+plantedIdentifier+`"`), 1)
		_ = os.WriteFile(filepath.Join(tampered, "extract.json"), raw, 0o600)
		_ = os.WriteFile(filepath.Join(tampered, "identity.sha256"), []byte(sha(raw)+"\n"), 0o600)
		if _, err := report.OpenTransformedExtract(tampered); err == nil {
			t.Fatal("a changed transformed extract verified")
		}
	})

	t.Run("the commands create a key, preview and publish without original values", func(t *testing.T) {
		root := t.TempDir()
		var stdout, stderr bytes.Buffer
		key := filepath.Join(root, "key")
		if err := cli.Execute("report", []string{"report", "connected", "pseudonym-key", "--output", key}, &stdout, &stderr); err != nil {
			t.Fatal(err, stderr.String())
		}
		if err := cli.Execute("report", []string{"report", "connected", "pseudonym-key", "--output", key}, &stdout, &stderr); err == nil {
			t.Fatal("an existing key was overwritten")
		}
		stdout.Reset()
		if err := cli.Execute("report", []string{"report", "connected", "extract", packet, "--policy", policy}, &stdout, &stderr); err == nil {
			t.Fatal("a transformed extract was prepared without a key")
		}
		for _, format := range []string{"", "markdown", "json", "html"} {
			stdout.Reset()
			args := []string{"report", "connected", "extract", packet, "--policy", policy, "--key", key}
			if format != "" {
				args = append(args, "--format", format)
			}
			if err := cli.Execute("report", args, &stdout, &stderr); err != nil {
				t.Fatal(err, stderr.String())
			}
			if out := stdout.String(); strings.Contains(out, "PLANTED") || strings.Contains(out, "pat-1") || strings.Contains(out, base) {
				t.Fatalf("the %q preview carries an original value", format)
			}
		}
		stdout.Reset()
		_ = cli.Execute("report", []string{"report", "connected", "extract", packet, "--policy", policy, "--key", key}, &stdout, &stderr)
		identity := strings.TrimPrefix(strings.SplitN(stdout.String(), "\n", 2)[0], "Transformed extract preview: ")
		stdout.Reset()
		if err := cli.Execute("report", []string{"report", "connected", "extract", packet, "--policy", policy, "--key", key, "--approve", identity, "--output", filepath.Join(root, "extract")}, &stdout, &stderr); err != nil || !strings.Contains(stdout.String(), "Derived, not original evidence") {
			t.Fatal(err, stdout.String(), stderr.String())
		}
		if err := cli.Execute("report", []string{"report", "connected", "extract", packet, "--policy", policy, "--key", keyPath, "--approve", identity, "--output", filepath.Join(root, "other")}, &stdout, &stderr); err == nil {
			t.Fatal("another key published the approved identity")
		}
	})
}
