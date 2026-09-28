package report_test

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/runcompare"
)

// The oracle for the lab appointment integration, authored here apart from
// the lab and the engine: one Appointment per business ID at the reviewed time.
const (
	labBooked      = "2026-03-01T09:00:00Z"
	labRescheduled = "2026-03-02T10:30:00Z"
	labAltered     = "2026-03-02T11:45:00Z"
	labStatus      = "http://hl7.org/fhir/appointmentstatus"
)

func labMessages() []connectedlab.Message {
	return []connectedlab.Message{
		{Step: "book", Raw: "MSH|^~\\&|SENDER|LAB|ENGINE|LAB|20260101000000||SIU^S12|BOOK|P|2.5.1\rSCH|APPT-1||||||||||" + labBooked + "\r"},
		{Step: "move", Raw: "MSH|^~\\&|SENDER|LAB|ENGINE|LAB|20260101000001||SIU^S13|MOVE|P|2.5.1\rSCH|APPT-1||||||||||" + labRescheduled + "\r"},
	}
}

// labFlow sends v2 through the lab's independent engine and observes the
// receiving FHIR store. expected is the reschedule check's authored time, so
// a changed assertion definition is one argument away.
func labFlow(h *connectedlab.Harness, expected string) connectedtest.FlowTest {
	query := "identifier=urn%3Areadmit-lab%3Aappointment%7CAPPT-1"
	appointments := func(phase, id, when string) connectedtest.Dataset {
		return h.Observe(phase, id, when, "Appointment", query, "reference-fhir-store",
			connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"),
			connectedlab.FieldColumn("status", "code", labStatus, false, true, "status"),
			connectedlab.FieldColumn("start", "datetime", "", false, true, "start"),
			connectedlab.IdentityColumn())
	}
	ackSet := func(name, step string) connectedtest.Reference {
		raw := `{"schema":"readmit-assertion-set/v1","name":"Positive ACK","assertions":[{"id":"accepted","operator":"field_equals","subject":{"field":{"scope":"observed","message":"` + h.Occurrences[step] + `","selector":"MSA-1"}},"when":null,"expected":{"field":{"state":"present","text":"AA"}}}]}`
		return h.Ref(name, assertion.Schema, name+".json", []byte(raw))
	}
	before, booked := appointments("booking", "prior", "before"), appointments("booking", "appointments", "after")
	moved := appointments("reschedule", "appointments", "after")
	return connectedtest.FlowTest{ID: "appointment-integration", Steps: []connectedtest.Step{h.V2Step("book"), h.V2Step("move", "book")}, Phases: []connectedtest.FlowPhase{
		{ID: "booking", Steps: []string{"book"}, After: []connectedtest.PhaseDependency{}, Datasets: []connectedtest.Dataset{before, booked}, Wire: &connectedtest.WireChecks{Set: ackSet("ack-checks", "book"), Observed: "transport-acks"}, Checks: h.Checks("booking", []connectedtest.Dataset{before, booked},
			connectedlab.RowCount("none-before", "prior", 0), connectedlab.RowCount("one", "appointments", 1), connectedlab.Unique("unique", "appointments"),
			connectedlab.Equals("booked", "appointments", "status", connectedlab.Code("booked", labStatus)), connectedlab.Instant("start", "appointments", "start", labBooked))},
		{ID: "reschedule", Steps: []string{"move"}, After: []connectedtest.PhaseDependency{{Phase: "booking", Requires: "pass"}}, Datasets: []connectedtest.Dataset{moved}, Wire: &connectedtest.WireChecks{Set: ackSet("move-ack-checks", "move"), Observed: "transport-acks"}, Checks: h.Checks("reschedule", []connectedtest.Dataset{moved},
			connectedlab.RowCount("one", "appointments", 1), connectedlab.Unique("unique", "appointments"),
			connectedlab.Equals("booked", "appointments", "status", connectedlab.Code("booked", labStatus)), connectedlab.Instant("start", "appointments", "start", expected))},
	}}
}

// alterRescheduledStart makes the application store a different start for the
// one rescheduled appointment, under its unchanged business and logical IDs,
// after the engine processed the reschedule and before it acknowledges it.
func alterRescheduledStart(t *testing.T, h *connectedlab.Harness) {
	var handled atomic.Int32
	h.Engine.SetAfter(func() {
		if handled.Add(1) != 2 {
			return
		}
		request, err := http.NewRequest("GET", h.Lab.Base()+"/Appointment?identifier="+url.QueryEscape(connectedlab.AppointmentSystem+"|APPT-1"), nil)
		if err != nil {
			t.Error(err)
			return
		}
		request.Header.Set("Accept", "application/fhir+json")
		response, err := h.Lab.Server().Client().Do(request)
		if err != nil {
			t.Error(err)
			return
		}
		defer response.Body.Close()
		var bundle struct {
			Entry []struct {
				Resource struct {
					ID string `json:"id"`
				} `json:"resource"`
			} `json:"entry"`
		}
		raw, _ := io.ReadAll(response.Body)
		if err := json.Unmarshal(raw, &bundle); err != nil || len(bundle.Entry) != 1 {
			t.Error("the rescheduled appointment is not one resource", err, response.StatusCode, string(raw))
			return
		}
		id := bundle.Entry[0].Resource.ID
		h.Lab.With(func() {
			h.Lab.Put("Appointment", id, map[string]any{"identifier": []any{map[string]any{"system": connectedlab.AppointmentSystem, "value": "APPT-1"}}, "status": "booked", "start": labAltered, "participant": []any{map[string]any{"status": "accepted", "actor": map[string]any{"display": "Synthetic patient"}}}}, 0)
		})
	})
}

// labRuns are actual retained lifecycles of the lab integration. The lab and
// its engine are closed before any packet is read, so every read below is
// offline by construction.
type labRuns struct {
	defect, fixed, again, altered, redefined string
}

func runLab(t *testing.T) labRuns {
	t.Helper()
	h := connectedlab.New(t, "", labMessages()...)
	h.Compile(labFlow(h, labRescheduled))
	run := func(mode, instance string, want assertion.Verdict) string {
		h.Engine.SetMode(mode)
		r, path := h.Run(instance)
		if r.Verdict != want || r.State != "complete" {
			t.Fatalf("%s: verdict=%s state=%s, want %s", instance, r.Verdict, r.State, want)
		}
		return path
	}
	runs := labRuns{defect: run("defective", "defect", assertion.VerdictFail), fixed: run("fixed", "fixed", assertion.VerdictPass), again: run("defective", "again", assertion.VerdictFail)}
	alterRescheduledStart(t, h)
	runs.altered = run("fixed", "altered", assertion.VerdictFail)
	h.Engine.SetAfter(nil)
	// The same retained test with one assertion definition changed.
	h.Compile(labFlow(h, labAltered))
	runs.redefined = run("fixed", "redefined", assertion.VerdictFail)
	h.Lab.Server().Close()
	h.Engine.Listener.Close()
	return runs
}

// labLifecycles runs lab lifecycles once without race instrumentation, through
// make test-fhir-lab, as the connectedrun lab tests do.
func labLifecycles(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("the connected lab lifecycles run uninstrumented through make test-fhir-lab")
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o700)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), raw, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assemble(t *testing.T, in report.ConnectedInput) (string, *report.ConnectedPacket) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "packet")
	p, err := report.AssembleConnected(t.Context(), in, out)
	if err != nil {
		t.Fatal(err)
	}
	return out, p
}

func findFile(t *testing.T, root string, match func(rel string) bool) string {
	t.Helper()
	found := ""
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(root, path)
		if err == nil && !d.IsDir() && found == "" && match(filepath.ToSlash(rel)) {
			found = filepath.ToSlash(rel)
		}
		return nil
	})
	if found == "" {
		t.Fatal("no retained file matches")
	}
	return found
}

func changesOf(t *testing.T, err error) []report.EvidenceChange {
	t.Helper()
	var changed *report.ChangedEvidenceError
	if !errors.As(err, &changed) {
		t.Fatalf("verification did not name what changed: %v", err)
	}
	return changed.Changes
}

// reseal rewrites a packet's index and seal for its current bytes, as someone
// hiding an edit would; the nested lifecycle seals still bind the evidence.
func reseal(t *testing.T, dir string, edit func(*report.ConnectedManifest)) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m report.ConnectedManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for i, entry := range m.Files {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(entry.Path)))
		if err != nil {
			t.Fatal(err)
		}
		m.Files[i].Size, m.Files[i].SHA256 = len(data), sha(data)
	}
	if edit != nil {
		edit(&m)
	}
	raw, err = json.Marshal(m, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "identity.sha256"), []byte(sha(raw)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConnectedRetainedProofOfTheLabIntegration(t *testing.T) {
	labLifecycles(t)
	runs := runLab(t)

	t.Run("a packet moved to a clean folder verifies and re-analyzes with every source gone", func(t *testing.T) {
		packet, _ := assemble(t, report.ConnectedInput{Current: runs.fixed, Baseline: runs.defect})
		moved := filepath.Join(t.TempDir(), "clean machine", "moved packet")
		copyTree(t, packet, moved)
		if err := os.RemoveAll(packet); err != nil {
			t.Fatal(err)
		}
		p, err := report.OpenConnected(t.Context(), moved)
		if err != nil {
			t.Fatal("a relocated packet does not verify offline:", err)
		}
		m := p.Manifest
		if m.Schema != report.ConnectedSchema || m.EvidenceClass != report.EvidenceOriginal || !m.ContainsSourceValues || m.Current.Verdict != "pass" || m.Baseline == nil || m.Baseline.Verdict != "fail" || m.Equivalence.State != report.EquivalenceNotClaimed {
			t.Fatalf("the verified claims are not the retained runs': %+v", m)
		}
		if !slices.Equal(m.Baseline.Failures, []string{"reschedule/typed:one", "reschedule/typed:unique"}) || len(m.Current.Failures) != 0 {
			t.Fatal("failure signatures", m.Baseline.Failures, m.Current.Failures)
		}
		if m.Current.Environment.TargetRevision.Value != "independent-lab" || len(m.Current.Qualification) != 3 || m.Current.Setup != "ready" || m.Current.Cleanup != "complete" {
			t.Fatal("environment, target revision, qualification or setup/cleanup were not carried", m.Current)
		}
		if len(p.Reanalysis) != 2 {
			t.Fatal("each lifecycle is re-analyzed apart from its original verdict")
		}
		for _, r := range p.Reanalysis {
			if r.OriginalVerdict == "" || r.ReanalyzedVerdict != r.OriginalVerdict || len(r.Limitations) == 0 {
				t.Fatalf("reanalysis did not keep the original verdict apart: %+v", r)
			}
		}
		review := filepath.Join(t.TempDir(), "review")
		exported, err := report.ExportConnectedReview(t.Context(), moved, review)
		if err != nil {
			t.Fatal(err)
		}
		reopened, err := report.OpenConnectedReview(t.Context(), review)
		if err != nil || reopened.Identity != exported.Identity {
			t.Fatal("the review does not reopen", err)
		}
		doc, _ := reopened.Render("json")
		var typed report.ConnectedReport
		if err := json.Unmarshal(doc, &typed, json.RejectUnknownMembers(true)); err != nil || typed.Schema != report.ConnectedReportSchema || len(typed.Runs) != 2 || typed.Comparison == nil {
			t.Fatal("the typed report", err)
		}
		claims := map[string]string{}
		for _, phase := range typed.Runs[1].Phases {
			for _, c := range phase.Checks {
				claims[c.ID] = c.Claim
			}
		}
		if claims["wire:accepted"] != "transport acceptance" || claims["typed:one"] != "observed workflow assertion" {
			t.Fatal("transport acceptance and observed workflow assertions are not marked apart", claims)
		}
		markdown, _ := reopened.Render("markdown")
		junit, _ := reopened.Render("junit")
		html, _ := reopened.Render("html")
		if bytes.Contains(markdown, []byte(`"schema"`)) || !bytes.Contains(markdown, []byte("| typed:one | observed workflow assertion | failed |")) || !bytes.Contains(junit, []byte("<failure")) || bytes.Contains(html, []byte("<script")) || !bytes.Contains(html, []byte("<table>")) {
			t.Fatal("renderings are not typed summaries of the same verdicts")
		}
		if _, err := report.OpenRetained(t.Context(), moved); err == nil {
			t.Fatal("the frozen v1 retained reader accepted a v2 packet")
		}
	})

	t.Run("an altered snapshot, check set, response, pin, environment or completion record is refused by surface", func(t *testing.T) {
		packet, _ := assemble(t, report.ConnectedInput{Current: runs.defect})
		for _, tc := range []struct {
			name    string
			match   func(string) bool
			surface string
		}{
			{"snapshot", func(rel string) bool {
				return strings.HasPrefix(rel, "current/phases/reschedule/intervals/appointments/samples/") && strings.HasSuffix(rel, "/sample.json")
			}, "phase reschedule: observation sample"},
			{"check set", func(rel string) bool {
				return strings.HasPrefix(rel, "current/plan/dependencies/")
			}, "pinned dependency"},
			{"response", func(rel string) bool {
				return strings.HasSuffix(rel, "reschedule/transport/run/payloads/o000001-received.bin")
			}, "phase reschedule: v2 transport evidence"},
			{"profile or capability pin", func(rel string) bool {
				return strings.HasPrefix(rel, "current/phases/booking/plan/dependencies/")
			}, "phase booking: phase plan"},
			{"environment", func(rel string) bool { return rel == "current/plan/test.json" }, "compiled plan"},
			{"completion record", func(rel string) bool { return rel == "current/phases/reschedule/intervals/appointments/manifest.json" }, "phase reschedule: observation completion record"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "packet")
				copyTree(t, packet, dir)
				rel := findFile(t, dir, tc.match)
				path := filepath.Join(dir, filepath.FromSlash(rel))
				raw, _ := os.ReadFile(path)
				if err := os.WriteFile(path, append(raw, ' '), 0o600); err != nil {
					t.Fatal(err)
				}
				_, err := report.OpenConnected(t.Context(), dir)
				changes := changesOf(t, err)
				if len(changes) != 1 || changes[0].Path != rel || changes[0].Kind != "changed" || changes[0].Section != "current" || !strings.Contains(changes[0].Surface, tc.surface) {
					t.Fatalf("the change was not named precisely: %+v", changes)
				}
				// Recomputing the packet's own index and seal hides nothing:
				// the lifecycle's nested seals and re-derivation still refuse.
				reseal(t, dir, nil)
				_, err = report.OpenConnected(t.Context(), dir)
				changes = changesOf(t, err)
				if len(changes) != 1 || changes[0].Section != "current" || !strings.Contains(changes[0].Surface, "does not re-derive") {
					t.Fatalf("a resealed edit was not refused: %+v", changes)
				}
			})
		}
		// An invented verdict in a resealed manifest is named as the claim it
		// contradicts; an added file is named as added.
		dir := filepath.Join(t.TempDir(), "packet")
		copyTree(t, packet, dir)
		reseal(t, dir, func(m *report.ConnectedManifest) { m.Current.Verdict, m.Current.Failures = "pass", []string{} })
		_, err := report.OpenConnected(t.Context(), dir)
		if changes := changesOf(t, err); len(changes) != 1 || changes[0].Surface != "run claims" || changes[0].Section != "current" {
			t.Fatalf("an invented pass was not refused as a contradicted claim: %+v", changes)
		}
		dir = filepath.Join(t.TempDir(), "packet")
		copyTree(t, packet, dir)
		if err := os.WriteFile(filepath.Join(dir, "current", "phases", "reschedule", "extra.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err = report.OpenConnected(t.Context(), dir)
		if changes := changesOf(t, err); len(changes) != 1 || changes[0].Kind != "added" {
			t.Fatalf("an added file was not named: %+v", changes)
		}
	})

	t.Run("comparison keeps identity, value, multiplicity and definition changes apart", func(t *testing.T) {
		keys := func(c runcompare.FlowComparison, phase string) []runcompare.FlowKeyComparison {
			for _, r := range c.Records {
				if r.Phase == phase && r.Dataset == "appointments" {
					if r.State != "compared" {
						t.Fatalf("%s records were not compared: %s", phase, r.Reason)
					}
					return r.Keys
				}
			}
			t.Fatal("no record comparison for", phase)
			return nil
		}
		check := func(c runcompare.FlowComparison, phase, id string) runcompare.FlowCheckComparison {
			for _, x := range c.Checks {
				if x.Phase == phase && x.Check == id {
					return x
				}
			}
			t.Fatal("no check comparison for", phase, id)
			return runcompare.FlowCheckComparison{}
		}
		changed := func(c runcompare.FlowComparison) []string { return c.Attribution.Changed }

		// A duplicate is two records under one key, never one.
		fix, err := runcompare.CompareFlows(t.Context(), runs.defect, runs.fixed)
		if err != nil {
			t.Fatal(err)
		}
		if k := keys(fix, "reschedule"); len(k) != 1 || k[0].Baseline != 2 || k[0].Current != 1 || k[0].State != "multiplicity-changed" {
			t.Fatalf("duplicate resources were not compared by multiplicity: %+v", k)
		}
		if k := keys(fix, "booking"); len(k) != 1 || k[0].State != "identities-changed" || len(k[0].Values) != 0 || !slices.Equal(k[0].Identities, []string{"identity"}) {
			t.Fatalf("a new server-assigned ID under unchanged fields was reported as a value change: %+v", k)
		}
		if x := check(fix, "reschedule", "typed:one"); x.Definition != "unchanged" || x.Behavior != "changed" || fix.Attribution.Outcome != "no-declared-change" || len(changed(fix)) != 0 {
			t.Fatalf("the fix: %+v %+v", x, fix.Attribution)
		}

		// Unchanged business ID, altered value.
		altered, err := runcompare.CompareFlows(t.Context(), runs.fixed, runs.altered)
		if err != nil {
			t.Fatal(err)
		}
		if k := keys(altered, "reschedule"); len(k) != 1 || k[0].Baseline != 1 || k[0].Current != 1 || k[0].State != "values-changed" || !slices.Equal(k[0].Values, []string{"start"}) {
			t.Fatalf("an altered value under an unchanged ID was not found: %+v", k)
		}
		if x := check(altered, "reschedule", "typed:start"); x.Definition != "unchanged" || x.Behavior != "changed" || x.Current != "failed" {
			t.Fatalf("altered behavior: %+v", x)
		}

		// A changed assertion definition is never a behavior change.
		redefined, err := runcompare.CompareFlows(t.Context(), runs.fixed, runs.redefined)
		if err != nil {
			t.Fatal(err)
		}
		if x := check(redefined, "reschedule", "typed:start"); x.Definition != "changed" || x.Behavior != "not_compared" {
			t.Fatalf("a redefined check was compared as behavior: %+v", x)
		}
		if x := check(redefined, "reschedule", "typed:one"); x.Definition != "unchanged" || x.Behavior != "unchanged" {
			t.Fatalf("an untouched check in a changed set: %+v", x)
		}
		if redefined.Attribution.Outcome != "single-declared-change" || !slices.Equal(changed(redefined), []string{runcompare.DimensionDefinition}) {
			t.Fatalf("attribution: %+v", redefined.Attribution)
		}
		for _, d := range redefined.Dimensions {
			if d.Dimension == runcompare.DimensionDefinition && !slices.Equal(d.Changed, []string{"checks:reschedule"}) {
				t.Fatal("the changed declaration was not named", d.Changed)
			}
		}
	})

	t.Run("reproduction needs two retained real executions with one failure signature", func(t *testing.T) {
		_, reproduced := assemble(t, report.ConnectedInput{Current: runs.defect, Replay: runs.again})
		if e := reproduced.Manifest.Equivalence; e.State != report.EquivalenceReproduced || reproduced.Manifest.Replay == nil {
			t.Fatalf("two failing executions of one plan did not reproduce: %+v", e)
		}
		_, fixed := assemble(t, report.ConnectedInput{Current: runs.defect, Replay: runs.fixed})
		if e := fixed.Manifest.Equivalence; e.State != report.EquivalenceNotReproduced {
			t.Fatalf("a passing replay was called a reproduction: %+v", e)
		}
		_, other := assemble(t, report.ConnectedInput{Current: runs.altered, Replay: runs.redefined})
		if e := other.Manifest.Equivalence; e.State != report.EquivalenceNotReproduced || !strings.Contains(e.Reason, "different plan") {
			t.Fatalf("a replay of a different plan was called a reproduction: %+v", e)
		}
		if _, err := report.AssembleConnected(t.Context(), report.ConnectedInput{Current: runs.defect, Replay: runs.defect}, filepath.Join(t.TempDir(), "same")); err == nil {
			t.Fatal("the current run relabelled as its own replay was accepted")
		}
	})

	t.Run("the connected commands verify, compare, export, review and refuse offline", func(t *testing.T) {
		root := t.TempDir()
		var stdout, stderr bytes.Buffer
		packet := filepath.Join(root, "packet")
		if err := cli.Execute("report", []string{"report", "connected", "assemble", "--current", runs.fixed, "--baseline", runs.defect, "--output", packet}, &stdout, &stderr); err != nil {
			t.Fatal(err, stderr.String())
		}
		for _, args := range [][]string{
			{"report", "connected", "verify", packet},
			{"report", "connected", "verify", packet, "--json"},
			{"report", "connected", "compare", runs.defect, runs.fixed},
			{"report", "connected", "export", packet, "--output", filepath.Join(root, "review")},
			{"report", "connected", "review", filepath.Join(root, "review")},
		} {
			stdout.Reset()
			if err := cli.Execute("report", args, &stdout, &stderr); err != nil {
				t.Fatal(args, err, stderr.String())
			}
			if out := stdout.String(); strings.Contains(out, "APPT-1") || strings.Contains(out, runs.fixed) || strings.Contains(out, "127.0.0.1") {
				t.Fatalf("%v wrote a value, path or endpoint: %s", args, out)
			}
		}
		file := filepath.Join(packet, filepath.FromSlash(findFile(t, packet, func(rel string) bool { return strings.HasSuffix(rel, "o000001-received.bin") })))
		raw, _ := os.ReadFile(file)
		_ = os.WriteFile(file, append(raw, ' '), 0o600)
		err := cli.Execute("report", []string{"report", "connected", "verify", packet}, &stdout, &stderr)
		if cli.ExitCode(err) != 1 || !strings.Contains(err.Error(), "v2 transport evidence") {
			t.Fatal("a changed packet was not refused by surface", err)
		}
	})
}

// A FHIR-native lifecycle with synthetic planted values in every new evidence
// surface: narrative, extension and attachment in the stored resource, the
// business identifier in the search query, typed rows, the bound server ID
// and the fixture's setup resources.
const (
	plantedIdentifier = "PLANTED-IDENT-4411"
	plantedNarrative  = "PLANTED-NARRATIVE-4411"
	plantedExtension  = "PLANTED-EXTENSION-4411"
	plantedFamily     = "PLANTEDFAMILY"
)

func plantedFlow(h *connectedlab.Harness, extension string) connectedtest.FlowTest {
	body := `{"resourceType":"Patient","identifier":[{"system":"urn:readmit-lab:patient","value":"` + plantedIdentifier + `"}],"name":[{"family":"` + plantedFamily + `"}],` +
		`"text":{"status":"generated","div":"<div xmlns=\"http://www.w3.org/1999/xhtml\">` + plantedNarrative + `</div>"},` +
		`"extension":[{"url":"urn:readmit-lab:note","valueString":"` + extension + `"}],` +
		`"photo":[{"contentType":"text/plain","data":"UExBTlRFRC1BVFRBQ0gtNDQxMQ=="}]}`
	patients := h.Observe("register", "patients", "after", "Patient", "identifier="+url.QueryEscape("urn:readmit-lab:patient|"+plantedIdentifier), "authoritative-application-api",
		connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"),
		connectedlab.FieldColumn("family", "text", "", false, true, "name#0", "family"),
		connectedlab.IdentityColumn())
	return connectedtest.FlowTest{ID: "planted-registration", Variables: []connectedtest.Variable{{ID: "patient-id", Kind: "response"}},
		Steps: []connectedtest.Step{h.FHIRStep("register", "POST", "Patient", body, connectedtest.FHIRHeaders{}, []connectedtest.ResponseBinding{connectedlab.BindID("patient-id", "logical-id", "phase")})},
		Phases: []connectedtest.FlowPhase{{ID: "register", Steps: []string{"register"}, After: []connectedtest.PhaseDependency{}, Datasets: []connectedtest.Dataset{patients},
			Responses: []connectedtest.ResponseCheck{{ID: "created", Step: "register", Outcome: "succeeded"}},
			Checks:    h.Checks("register", []connectedtest.Dataset{patients}, connectedlab.RowCount("one", "patients", 1))}}}
}

func writePolicy(t *testing.T, surfaces ...string) string {
	t.Helper()
	mapped := map[string]string{}
	for _, s := range surfaces {
		mapped[s] = "exclude"
	}
	raw, err := json.Marshal(report.DisclosurePolicy{Schema: report.DisclosurePolicySchema, Surfaces: mapped})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConnectedExtractMapsEveryPlantedSurfaceOrBlocks(t *testing.T) {
	labLifecycles(t)
	h := connectedlab.New(t, "")
	h.Compile(plantedFlow(h, plantedExtension))
	r, planted := h.Run("planted")
	if r.Verdict != assertion.VerdictPass {
		t.Fatalf("the planted registration did not pass: %s %s", r.Verdict, r.State)
	}
	// A token that should never be retained, planted where a careless
	// integration would echo it.
	h.Compile(plantedFlow(h, "Bearer PLANTEDTOKEN4411abcdefgh"))
	_, leaked := h.Run("leaked")
	base := h.Lab.Base()
	h.Lab.Server().Close()
	h.Engine.Listener.Close()

	packet, p := assemble(t, report.ConnectedInput{Current: planted})
	inventory, err := p.Inventory(t.Context(), packet)
	if err != nil {
		t.Fatal(err)
	}
	surfaces := []string{}
	for _, item := range inventory {
		surfaces = append(surfaces, item.Surface)
	}
	for _, want := range []string{report.SurfaceFHIRResource, report.SurfaceFHIRNarrative, report.SurfaceFHIRExtension, report.SurfaceAttachment, report.SurfaceHTTPExchange, report.SurfaceTypedDataset, report.SurfaceMapping, report.SurfaceSetup, report.SurfacePlan} {
		if !slices.Contains(surfaces, want) {
			t.Errorf("the inventory missed the %s surface: %v", want, surfaces)
		}
	}
	if slices.Contains(surfaces, report.SurfaceCredential) {
		t.Fatal("credential material was reported where none was retained")
	}

	// Every surface but the narrative is mapped: the narrative blocks.
	partial := slices.DeleteFunc(slices.Clone(report.DisclosureSurfaces), func(s string) bool { return s == report.SurfaceFHIRNarrative })
	blocked, err := report.PrepareExtract(t.Context(), packet, writePolicy(t, partial...))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked.Blocked) != 1 || !strings.Contains(blocked.Blocked[0], report.SurfaceFHIRNarrative) {
		t.Fatal("an unmapped surface did not block the extract", blocked.Blocked)
	}
	if err := blocked.Publish(t.Context(), blocked.Identity(), filepath.Join(t.TempDir(), "extract")); err == nil {
		t.Fatal("a blocked extract was published")
	}

	candidate, err := report.PrepareExtract(t.Context(), packet, writePolicy(t, report.DisclosureSurfaces...))
	if err != nil || len(candidate.Blocked) != 0 {
		t.Fatal("a fully mapped packet was blocked", err, candidate.Blocked)
	}
	output := filepath.Join(t.TempDir(), "extract")
	if err := candidate.Publish(t.Context(), "0000000000000000000000000000000000000000000000000000000000000000", output); err == nil {
		t.Fatal("an extract was published without its exact approval")
	}
	if err := candidate.Publish(t.Context(), candidate.Identity(), output); err != nil {
		t.Fatal(err)
	}
	x, err := report.OpenExtract(output)
	if err != nil || x.EvidenceClass != report.EvidenceExtract || x.Equivalence.State != report.EquivalenceUnverified || x.Residual.Status != "passed" || x.Residual.KnownValues == 0 {
		t.Fatal("the published extract", err, x.Equivalence, x.Residual)
	}
	raw, _ := os.ReadFile(filepath.Join(output, "extract.json"))
	phaseName := p.Manifest.Current.Phases[0].ID
	for _, secret := range []string{plantedIdentifier, plantedNarrative, plantedExtension, plantedFamily, "PLANTED", "UExBTlRFRC1BVFRBQ0gtNDQxMQ", base, h.Root, "pat-", "Synthetic lab patient", phaseName} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("the extract carries a retained value: %q", secret)
		}
	}
	// The customer-local review keeps the values, inertly, and says so.
	review, err := report.ExportConnectedReview(t.Context(), packet, filepath.Join(t.TempDir(), "review"))
	if err != nil {
		t.Fatal(err)
	}
	markdown, _ := review.Render("markdown")
	if !bytes.Contains(markdown, []byte(plantedIdentifier)) || !review.Report.ContainsSourceValues || review.Report.EvidenceClass != report.EvidenceOriginal {
		t.Fatal("the customer-local review does not keep and label its source values")
	}

	// Credential material blocks every extract, whatever the policy maps.
	leakedPacket, _ := assemble(t, report.ConnectedInput{Current: leaked})
	refused, err := report.PrepareExtract(t.Context(), leakedPacket, writePolicy(t, report.DisclosureSurfaces...))
	if err != nil {
		t.Fatal(err)
	}
	if len(refused.Blocked) == 0 || !strings.Contains(strings.Join(refused.Blocked, " "), "credential material") {
		t.Fatal("a retained token did not block the extract", refused.Blocked)
	}
	if err := refused.Publish(t.Context(), refused.Identity(), filepath.Join(t.TempDir(), "leak")); err == nil {
		t.Fatal("an extract of a packet retaining a token was published")
	}
	if _, err := report.DecodeDisclosurePolicy([]byte(`{"schema":"readmit-connected-disclosure-policy/v1","surfaces":{"credential-material":"exclude"}}`)); err == nil {
		t.Fatal("a policy admitted credential material")
	}
}
