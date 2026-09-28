package report_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/report"
)

// surfaceProof carries one lifecycle whose planted values actually travelled
// through one evidence path, and proves them handled from retention to every
// share-oriented export: the unmapped surface blocks both extract versions,
// the approved disposition applies, and no planted value reappears in the
// packet's claims and summary, the command output, the value-free extract or
// the transformed extract and its renderings.
type surfaceProof struct {
	packet   string
	surface  string
	planted  []string
	elements []report.ElementRule
	columns  []report.ColumnRule
}

func (s surfaceProof) prove(t *testing.T) *report.TransformedCandidate {
	t.Helper()
	absent := func(where string, data []byte) {
		t.Helper()
		for _, v := range s.planted {
			if bytes.Contains(data, []byte(v)) {
				t.Errorf("%s carries the planted value %q", where, v)
			}
		}
	}
	p, err := report.OpenConnected(t.Context(), s.packet)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := p.Inventory(t.Context(), s.packet)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(inventory, func(i report.InventoryItem) bool { return i.Surface == s.surface && i.Files > 0 }) {
		t.Fatalf("the inventory missed the %s surface the lifecycle retained: %+v", s.surface, inventory)
	}
	summary, _ := os.ReadFile(filepath.Join(s.packet, "SUMMARY.md"))
	manifest, _ := os.ReadFile(filepath.Join(s.packet, "manifest.json"))
	absent("the packet summary", summary)
	absent("the packet claims", manifest)

	// The value-free extract: the unmapped surface blocks; mapped, nothing
	// planted is published.
	without := slices.DeleteFunc(slices.Clone(report.DisclosureSurfaces), func(x string) bool { return x == s.surface })
	blocked, err := report.PrepareExtract(t.Context(), s.packet, writePolicy(t, without...))
	if err != nil || len(blocked.Blocked) != 1 || !strings.Contains(blocked.Blocked[0], s.surface) {
		t.Fatalf("the unmapped %s surface did not block the value-free extract: %v %v", s.surface, err, blocked.Blocked)
	}
	valueFree, err := report.PrepareExtract(t.Context(), s.packet, writePolicy(t, report.DisclosureSurfaces...))
	if err != nil || len(valueFree.Blocked) != 0 {
		t.Fatal("the fully mapped value-free extract was blocked", err, valueFree.Blocked)
	}
	output := filepath.Join(t.TempDir(), "value-free")
	if err := valueFree.Publish(t.Context(), valueFree.Identity(), output); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(output, "extract.json"))
	absent("the value-free extract", raw)

	// The transformed extract: the same surface unmapped blocks; mapped by
	// the reviewed rules, the derived material and every rendering hold no
	// planted value.
	_, key := pseudonymKey(t)
	partial := transformPolicy(t, transformAll, "authored", s.elements, s.columns, []report.ParameterRule{{Name: "identifier", Action: "pseudonymize"}})
	var policy report.TransformPolicy
	rawPolicy, _ := os.ReadFile(partial)
	_ = json.Unmarshal(rawPolicy, &policy)
	delete(policy.Surfaces, s.surface)
	rawPolicy, _ = json.Marshal(policy)
	_ = os.WriteFile(partial, rawPolicy, 0o600)
	if c, err := report.PrepareTransformedExtract(t.Context(), s.packet, partial, key); err != nil || len(c.Blocked) != 1 || !strings.Contains(c.Blocked[0], s.surface) {
		t.Fatalf("the unmapped %s surface did not block the transformed extract: %v", s.surface, err)
	}
	full := transformPolicy(t, transformAll, "authored", s.elements, s.columns, []report.ParameterRule{{Name: "identifier", Action: "pseudonymize"}})
	transformed, err := report.PrepareTransformedExtract(t.Context(), s.packet, full, key)
	if err != nil || len(transformed.Blocked) != 0 {
		t.Fatal("the reviewed transformed extract was blocked", err, transformed.Blocked)
	}
	published := filepath.Join(t.TempDir(), "transformed")
	if err := transformed.Publish(t.Context(), transformed.Identity(), published); err != nil {
		t.Fatal(err)
	}
	if _, err := report.OpenTransformedExtract(published); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"extract.json", "report.md", "report.html"} {
		raw, _ := os.ReadFile(filepath.Join(published, name))
		absent("the transformed "+name, raw)
	}

	// The customer-local review is original evidence and says so.
	review, err := report.ExportConnectedReview(t.Context(), s.packet, filepath.Join(t.TempDir(), "review"))
	if err != nil || review.Report.EvidenceClass != report.EvidenceOriginal || !review.Report.ContainsSourceValues {
		t.Fatal("the customer-local review does not label its source values", err)
	}

	// Command output names identities, states and surfaces only.
	var stdout, stderr bytes.Buffer
	for _, args := range [][]string{
		{"report", "connected", "verify", s.packet},
		{"report", "connected", "verify", s.packet, "--json"},
		{"report", "connected", "extract", s.packet, "--policy", writePolicy(t, report.DisclosureSurfaces...)},
	} {
		stdout.Reset()
		if err := cli.Execute("report", args, &stdout, &stderr); err != nil {
			t.Fatal(args, err, stderr.String())
		}
		absent(strings.Join(args[:3], " ")+" output", stdout.Bytes())
	}
	return transformed
}

// retained reads one retained file of the packet's current lifecycle.
func retained(t *testing.T, packet, rel string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(packet, "current", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestConnectedLabSurfacesTravelFromCollectionToExport(t *testing.T) {
	labLifecycles(t)

	t.Run("validator diagnostics from the actual worker", func(t *testing.T) {
		const family = "DIAGFAMILY-PLANTED-7733"
		engine := labValidationEngine(t)
		h := connectedlab.New(t, "")
		h.EnableValidation(engine.Socket)
		h.Compile(validatedFlow(h, validatedPatient(family), 5000))
		r, run := h.Run("diagnosed")
		if connectedlab.CheckOutcome(connectedlab.PhaseResult(r, "register"), "validation:patient") != assertion.OutcomePassed || len(engine.Runs()) != 1 {
			t.Fatalf("the validation did not run through the worker: %+v", r)
		}
		h.Lab.Server().Close()
		h.Engine.Listener.Close()
		packet, _ := assemble(t, report.ConnectedInput{Current: run})
		// The planted values reached the retained diagnostics through the
		// worker's own outcome.
		if result := retained(t, packet, "phases/register/validations/patient/result.json"); !bytes.Contains(result, []byte(family)) || !bytes.Contains(result, []byte(validatedIdentifier)) {
			t.Fatal("the worker's diagnostics did not carry the planted values")
		}
		surfaceProof{packet: packet, surface: report.SurfaceValidator, planted: []string{family, validatedIdentifier},
			elements: []report.ElementRule{{Resource: "Patient", Path: "identifier.value", Action: "pseudonymize"}, {Resource: "Patient", Path: "name.family", Action: "pseudonymize"}},
			columns:  []report.ColumnRule{{Dataset: "patients", Column: "key", Action: "pseudonymize"}}}.prove(t)
		// An explicit revalidation's own diagnostics stay in its evidence.
		var stdout, stderr bytes.Buffer
		installed := connectedlab.StageValidatorCapability(t, t.TempDir(), "installed", "")
		if err := cli.Execute("report", []string{"report", "connected", "revalidate", packet, "--capability", installed, "--socket", engine.Socket, "--output", filepath.Join(t.TempDir(), "analysis")}, &stdout, &stderr); err != nil || !strings.Contains(stdout.String(), "revalidated") {
			t.Fatal(err, stdout.String(), stderr.String())
		}
		if bytes.Contains(stdout.Bytes(), []byte(family)) || bytes.Contains(stdout.Bytes(), []byte(validatedIdentifier)) {
			t.Fatal("the revalidation output carries a diagnostic value")
		}
	})

	t.Run("an HTTP error body from the application", func(t *testing.T) {
		const (
			submitted   = "ERR-PLANTED-6621"
			serverOnly  = "ERR-SERVER-SECRET-9043"
			serverName  = "ERRFAMILYSERVER"
			textRefused = "ERR-TEXT-PLANTED-1188"
			textServer  = "ERR-TEXT-SERVER-5521"
		)
		h := connectedlab.New(t, "")
		reject := func(typ string, body []byte) (int, string, []byte) {
			switch {
			case bytes.Contains(body, []byte(submitted)):
				return 422, "application/fhir+json", []byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"duplicate","diagnostics":"Patient ` + submitted + ` conflicts with existing record ` + serverOnly + ` (` + serverName + `)"}]}`)
			case bytes.Contains(body, []byte(textRefused)):
				return 400, "text/plain", []byte("invalid record " + textRefused + "; already held by " + textServer)
			}
			return 0, "", nil
		}
		h.Lab.Reject.Store(&reject)
		patient := func(id string) string {
			return `{"resourceType":"Patient","identifier":[{"system":"urn:readmit-lab:patient","value":"` + id + `"}]}`
		}
		patients := h.Observe("register", "patients", "after", "Patient", "identifier=urn%3Areadmit-lab%3Apatient%7C"+submitted, "authoritative-application-api",
			connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"), connectedlab.IdentityColumn())
		h.Compile(connectedtest.FlowTest{ID: "refused-registration",
			Steps: []connectedtest.Step{h.FHIRStep("register", "POST", "Patient", patient(submitted), connectedtest.FHIRHeaders{}, nil), h.FHIRStep("amend", "POST", "Patient", patient(textRefused), connectedtest.FHIRHeaders{}, nil)},
			Phases: []connectedtest.FlowPhase{{ID: "register", Steps: []string{"register", "amend"}, After: []connectedtest.PhaseDependency{}, Datasets: []connectedtest.Dataset{patients},
				Responses: []connectedtest.ResponseCheck{{ID: "refused", Step: "register", Outcome: "rejected"}, {ID: "invalid", Step: "amend", Outcome: "rejected"}},
				Checks:    h.Checks("register", []connectedtest.Dataset{patients}, connectedlab.RowCount("none", "patients", 0))}}})
		r, run := h.Run("refused")
		if r.Verdict != assertion.VerdictPass || r.State != "complete" {
			t.Fatalf("the refused registrations were not retained as observed: %s %s", r.Verdict, r.State)
		}
		h.Lab.Server().Close()
		h.Engine.Listener.Close()
		packet, _ := assemble(t, report.ConnectedInput{Current: run})
		if !bytes.Contains(retained(t, packet, "phases/register/steps/register/response-0001.bin"), []byte(serverOnly)) || !bytes.Contains(retained(t, packet, "phases/register/steps/amend/response-0001.bin"), []byte(textServer)) {
			t.Fatal("the error bodies were not retained as the application sent them")
		}
		planted := []string{submitted, serverOnly, serverName, textRefused, textServer}
		keepStructure := []report.ElementRule{{Resource: "OperationOutcome", Path: "issue.severity", Action: "keep"}, {Resource: "OperationOutcome", Path: "issue.code", Action: "keep"}, {Resource: "Patient", Path: "identifier.value", Action: "pseudonymize"}}
		proof := surfaceProof{packet: packet, surface: report.SurfaceHTTPResponse, planted: planted, elements: keepStructure, columns: []report.ColumnRule{{Dataset: "patients", Column: "key", Action: "pseudonymize"}}}
		transformed := proof.prove(t)
		proof.surface = report.SurfaceFHIRResource
		proof.prove(t)
		// The reviewed transformation shows what was refused and why in
		// structure, never the application's words; the plain-text body is
		// excluded as opaque and says so.
		exchanges := transformed.Extract.Evidence[0].Phases[0].Exchanges
		if len(exchanges) != 2 || exchanges[0].Status != 422 || exchanges[0].Outcome != "rejected" || exchanges[1].Status != 400 || exchanges[1].Response != nil || !slices.Contains(exchanges[1].Excluded, "a non-FHIR body (opaque)") {
			t.Fatalf("the transformed exchanges: %+v", exchanges)
		}
		issue := exchanges[0].Response.(map[string]any)["issue"].([]any)[0].(map[string]any)
		if issue["severity"] != "error" || issue["code"] != "duplicate" || issue["diagnostics"] != nil {
			t.Fatalf("the transformed OperationOutcome: %v", issue)
		}
	})

	t.Run("typed fields from a real PostgreSQL database", func(t *testing.T) {
		const (
			recordKey = "DB-PLANTED-3310"
			family    = "DBFAMILY-PLANTED"
		)
		db := startPostgres(t)
		db.Exec(t, `CREATE TABLE registrations (patient text NOT NULL, family text NOT NULL, status text NOT NULL); INSERT INTO registrations VALUES ('`+recordKey+`', '`+family+`', 'registered'), ('DB-OTHER-0001', 'Otherfamily', 'withdrawn'); CREATE VIEW observed AS SELECT patient, family, status FROM registrations; GRANT SELECT ON observed TO observer`)
		h := connectedlab.New(t, "")
		records := h.DatabaseDataset("register", "registrations", "after", db.Address, db.CA, "registered", "patient", "family")
		h.Compile(connectedtest.FlowTest{ID: "database-registration",
			Steps: []connectedtest.Step{h.FHIRStep("register", "POST", "Patient", validatedPatient("Plainfamily"), connectedtest.FHIRHeaders{}, nil)},
			Phases: []connectedtest.FlowPhase{{ID: "register", Steps: []string{"register"}, After: []connectedtest.PhaseDependency{}, Datasets: []connectedtest.Dataset{records},
				Responses: []connectedtest.ResponseCheck{{ID: "created", Step: "register", Outcome: "succeeded"}},
				Checks:    h.Checks("register", []connectedtest.Dataset{records}, connectedlab.RowCount("one", "registrations", 1))}}})
		r, run := h.Run("database")
		if r.Verdict != assertion.VerdictPass || r.State != "complete" {
			t.Fatalf("the database observation did not decide: %s %s", r.Verdict, r.State)
		}
		h.Lab.Server().Close()
		h.Engine.Listener.Close()
		packet, p := assemble(t, report.ConnectedInput{Current: run})
		evidence := p.Manifest.Current
		if len(evidence.Phases) != 1 {
			t.Fatal("phases", evidence.Phases)
		}
		// The planted fields were read by the real driver and retained.
		review, err := report.ExportConnectedReview(t.Context(), packet, filepath.Join(t.TempDir(), "review"))
		if err != nil {
			t.Fatal(err)
		}
		markdown, _ := review.Render("markdown")
		if !bytes.Contains(markdown, []byte(recordKey)) || !bytes.Contains(markdown, []byte(family)) || bytes.Contains(markdown, []byte("DB-OTHER-0001")) {
			t.Fatal("the database's selected fields were not retained and reported customer-locally")
		}
		transformed := surfaceProof{packet: packet, surface: report.SurfaceTypedDataset, planted: []string{recordKey, family, db.Address},
			elements: []report.ElementRule{{Resource: "Patient", Path: "identifier.value", Action: "pseudonymize"}},
			columns:  []report.ColumnRule{{Dataset: "registrations", Column: "patient", Action: "pseudonymize"}, {Dataset: "registrations", Column: "family", Action: "redact"}}}.prove(t)
		var observation *report.TransformedObservation
		for _, phase := range transformed.Extract.Evidence[0].Phases {
			for i := range phase.Observations {
				if phase.Observations[i].Dataset == "registrations" {
					observation = &phase.Observations[i]
				}
			}
		}
		if observation == nil || len(observation.Records) != 1 || !strings.HasPrefix(observation.Records[0][0].Text, "p-") || observation.Records[0][1].State != "present" || observation.Records[0][1].Text != "" {
			t.Fatalf("the database record was not transformed by its reviewed rules: %+v", observation)
		}
	})
}
