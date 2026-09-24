package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/sequenceanalysis"
)

// correlationRulesEntry is the shipped acceptance rules document, copied into
// the workspace byte for byte as a person copies a documented example.
const correlationRulesEntry = "correlate-rules.json"

// correlationParityWorkspace captures the shipped case evidence twice, which
// is two independent sources, copies in the case the native window retained in
// September (testdata/acceptance/native-109), and copies the shipped rules.
func correlationParityWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	evidence := "../testdata/fixtures/case-evidence.mllp"
	if _, stderr, err := run(t, "capture", evidence, evidence, "--output", filepath.Join(workspace, "two-sources")); err != nil || stderr != "" {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	copyTree(t, "../testdata/acceptance/native-109/regression", filepath.Join(workspace, "native"))
	rules, err := os.ReadFile(correlateRules)
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, workspace, correlationRulesEntry, string(rules))
	return workspace
}

// correlated is `readmit correlate --format json` over one entry of the
// workspace under one rules entry of it, read strictly, with the exact bytes
// it printed.
func correlated(t *testing.T, workspace, entry, rules string) (correlate.Report, string) {
	t.Helper()
	return runJSON[correlate.Report](t, "correlate", filepath.Join(workspace, entry), "--rules", filepath.Join(workspace, rules), "--format", "json")
}

// verifiedIdentity opens one case entry in the window, as a person does before
// the sequence panel is offered, and returns the identity it verified.
func verifiedIdentity(t *testing.T, app *desktop.App, workspace, entry string) string {
	t.Helper()
	opened := app.OpenCase(workspace, entry)
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("%s was not verified: %+v", entry, opened)
	}
	return opened.Case.Identity
}

// relationsDrawn is every relation a declared rule put beside the events of a
// sequence, one line per occurrence and relation, in no particular order. The
// acknowledgements the case bundle itself records are not the rules' and are
// left out.
func relationsDrawn(events []desktop.SequenceEvent) []string {
	var lines []string
	for _, event := range events {
		for _, reference := range event.References {
			if reference.Kind == desktop.AcknowledgementReference {
				continue
			}
			lines = append(lines, relationLine(event.Occurrence, string(reference.Kind), reference.Rule, string(reference.Linkage), reference.Authority, reference.Reason, reference.Field, reference.Related))
		}
	}
	slices.Sort(lines)
	return lines
}

// relationsReported is the same lines taken from the command line's report:
// every occurrence of a link or a collision beside the rest of it, and every
// unsupported item that names an occurrence.
func relationsReported(report correlate.Report) []string {
	var lines []string
	named := func(references []correlate.Reference) []string {
		ids := make([]string, 0, len(references))
		for _, reference := range references {
			ids = append(ids, reference.Occurrence)
		}
		return ids
	}
	besides := func(ids []string, occurrence string) []string {
		return slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return id == occurrence })
	}
	for _, link := range report.Links {
		ids := named(link.Occurrences)
		for _, id := range ids {
			lines = append(lines, relationLine(id, "link", link.Rule, string(link.Linkage), link.Authority, "", "", besides(ids, id)))
		}
	}
	for _, collision := range report.Collisions {
		ids := named(collision.Occurrences)
		if collision.Declaring != nil && !slices.Contains(ids, collision.Declaring.Occurrence) {
			ids = append(ids, collision.Declaring.Occurrence)
		}
		for _, id := range ids {
			lines = append(lines, relationLine(id, "collision", collision.Rule, "", "", collision.Reason, "", besides(ids, id)))
		}
	}
	for _, item := range report.Unsupported {
		if item.Occurrence != "" {
			lines = append(lines, relationLine(item.Occurrence, "unsupported", item.Rule, "", "", item.Code, item.Field, nil))
		}
	}
	slices.Sort(lines)
	return lines
}

func relationLine(occurrence, kind, rule, linkage, authority, reason, field string, related []string) string {
	return fmt.Sprintf("%s %s rule=%s linkage=%s authority=%s reason=%s field=%s with=%s", occurrence, kind, rule, linkage, authority, reason, field, strings.Join(related, ","))
}

// A case laid out in the window under a chosen rules document carries exactly
// the correlation `readmit correlate` reports over the same case and the same
// document: every declared rule with its counts, the summary, the unsupported
// items, the boundary, the canonical rules digest, and every link, collision
// and unsupported item beside the occurrences it names. The case the native
// window retained in September is read the same way by both, including the
// source the rules declare and that case does not have, and neither entry
// point changes a byte of either case.
func TestTheWindowLinksTheCaseExactlyAsReadmitCorrelateDoes(t *testing.T) {
	workspace := correlationParityWorkspace(t)
	app := desktopApp(t, workspace)
	for _, entry := range []string{"two-sources", "native"} {
		before := treeOf(t, filepath.Join(workspace, entry))
		report, _ := correlated(t, workspace, entry, correlationRulesEntry)
		identity := verifiedIdentity(t, app, workspace, entry)
		laid := app.OpenSequence(desktop.SequenceRequest{
			Workspace: workspace, Case: entry, Identity: identity, Rules: correlationRulesEntry, Limit: desktop.MaxSequenceEvents,
		})
		if laid.State != desktop.Completed || laid.Sequence == nil {
			t.Fatalf("%s was not laid out under the rules: %+v", entry, laid)
		}
		sequence := laid.Sequence
		if sequence.Identity != report.CaseIdentity || sequence.Report != report.Schema || sequence.RulesSHA256 != report.RulesSHA256 || sequence.Boundary != report.Scope {
			t.Fatalf("%s: the window laid the case out as %s under %s, the command line reported %s under %s", entry, sequence.Identity, sequence.RulesSHA256, report.CaseIdentity, report.RulesSHA256)
		}
		if !reflect.DeepEqual(sequence.Declared, report.Rules) || !reflect.DeepEqual(sequence.Unsupported, report.Unsupported) {
			t.Fatalf("%s: the window applied %+v with %+v, the command line %+v with %+v", entry, sequence.Declared, sequence.Unsupported, report.Rules, report.Unsupported)
		}
		if sequence.Summary.Links != report.Summary.Links || sequence.Summary.Collisions != report.Summary.Collisions ||
			sequence.Summary.Unsupported != report.Summary.Unsupported || sequence.Total != report.Summary.Occurrences || len(sequence.Events) != sequence.Total {
			t.Fatalf("%s: the window counted %+v, the command line %+v", entry, sequence.Summary, report.Summary)
		}
		if drawn, reported := relationsDrawn(sequence.Events), relationsReported(report); !reflect.DeepEqual(drawn, reported) {
			t.Fatalf("%s: the window drew\n%s\nthe command line reported\n%s", entry, strings.Join(drawn, "\n"), strings.Join(reported, "\n"))
		}
		if !reflect.DeepEqual(treeOf(t, filepath.Join(workspace, entry)), before) {
			t.Fatalf("laying out or correlating %s changed it", entry)
		}
	}
	// The shipped acceptance fixture's documented reading, so a change in
	// either entry point cannot pass by changing both.
	report, _ := correlated(t, workspace, "two-sources", correlationRulesEntry)
	if report.Summary.Links != 7 || report.Summary.Collisions != 4 || report.Summary.Unsupported != 4 {
		t.Fatalf("the acceptance fixture no longer reads as the correlation guide documents: %+v", report.Summary)
	}
}

// Every rules document `readmit correlate` refuses is refused by the window
// in the same sentence wherever the window reads one: laying a case out under
// it, opening it in the editor, reviewing links under it, and saving it as a
// new entry, which writes nothing. A document changed on disk after a
// sequence was laid out under it is refused for review, because the canonical
// rules the command line now reports are not the ones on screen; a change
// that keeps the declarations the same keeps the digest, and the review.
func TestTheWindowRefusesTheRulesReadmitCorrelateRefuses(t *testing.T) {
	workspace := correlationParityWorkspace(t)
	app := desktopApp(t, workspace)
	identity := verifiedIdentity(t, app, workspace, "two-sources")
	refused := map[string]struct{ document, sentence string }{
		"misspelled.json": {`{"schema":"readmit-correlation-rules/v1","rules":[{"id":"a","operator":"control-id","scope":"declared","sorces":["s0001"]}]}`, "invalid correlation rules JSON"},
		"two-parts.json": {`{"schema":"readmit-correlation-rules/v1","rules":[{"id":"patient","operator":"identifier","scope":"source","value":"PID-3.1","authority":["PID-3.4.1","PID-3.4.2"]}]}`,
			"an identifier rule declares exactly three authority selectors: namespace, universal ID and universal ID type"},
		"truncated.json":  {`{"schema":"readmit-correlation-rules/v1","rules":[{"id":"a","operator":"ackno`, "invalid correlation rules JSON"},
		"newer.json":      {`{"schema":"readmit-correlation-rules/v2","rules":[{"id":"a","operator":"acknowledges","scope":"source"}]}`, "correlation rules must declare readmit-correlation-rules/v1"},
		"unknown-op.json": {`{"schema":"readmit-correlation-rules/v1","rules":[{"id":"a","operator":"fuzzy","scope":"source"}]}`, "a correlation operator is acknowledges, control-id or identifier"},
		"empty.json":      {`{"schema":"readmit-correlation-rules/v1","rules":[]}`, "a correlation rules document declares between 1 and 64 rules"},
	}
	for entry, declared := range refused {
		writeDocument(t, workspace, entry, declared.document)
	}
	before := treeOf(t, workspace)
	for entry, declared := range refused {
		stdout, stderr, err := run(t, "correlate", filepath.Join(workspace, "two-sources"), "--rules", filepath.Join(workspace, entry))
		if exitCode(t, err) != 1 || stdout != "" || stderr != "readmit: "+declared.sentence+"\n" {
			t.Fatalf("%s: the command line answered %v %q %q", entry, err, stdout, stderr)
		}
		laid := app.OpenSequence(desktop.SequenceRequest{Workspace: workspace, Case: "two-sources", Identity: identity, Rules: entry, Limit: desktop.MaxSequenceEvents})
		if laid.State != desktop.Failed || laid.Reason != declared.sentence || laid.Sequence != nil {
			t.Fatalf("%s: the sequence answered %+v", entry, laid)
		}
		opened := app.OpenCorrelationRules(workspace, entry)
		if opened.State != desktop.Failed || opened.Reason != declared.sentence || opened.Rules != nil {
			t.Fatalf("%s: the editor opened %+v", entry, opened)
		}
		review := app.OpenCorrelationReview(desktop.CorrelationReviewRequest{Workspace: workspace, Case: "two-sources", Identity: identity, Rules: entry})
		if review.State != desktop.Failed || review.Reason != declared.sentence || review.View != nil {
			t.Fatalf("%s: the review answered %+v", entry, review)
		}
		saved := app.SaveCorrelationRules(desktop.RuleDocumentSaveRequest{Workspace: workspace, Document: declared.document, Output: "saved-" + entry})
		if saved.State != desktop.Failed || saved.Reason != declared.sentence || saved.Output != "" {
			t.Fatalf("%s: the editor saved %+v", entry, saved)
		}
	}
	if !reflect.DeepEqual(treeOf(t, workspace), before) {
		t.Fatal("refusing rules documents changed the workspace")
	}

	// The shipped rules, laid out, then changed on disk.
	laid := app.OpenSequence(desktop.SequenceRequest{Workspace: workspace, Case: "two-sources", Identity: identity, Rules: correlationRulesEntry, Limit: desktop.MaxSequenceEvents})
	if laid.State != desktop.Completed || laid.Sequence == nil {
		t.Fatalf("the shipped rules were not applied: %+v", laid)
	}
	shown := laid.Sequence.RulesSHA256
	review := desktop.CorrelationReviewRequest{Workspace: workspace, Case: "two-sources", Identity: identity, Rules: correlationRulesEntry, RulesSHA256: shown}
	original, err := os.ReadFile(filepath.Join(workspace, correlationRulesEntry))
	if err != nil {
		t.Fatal(err)
	}
	// Reindented, the declarations are the same, and so are their digest and
	// the review under them.
	writeDocument(t, workspace, correlationRulesEntry, strings.ReplaceAll(string(original), "\n", "\n  "))
	if report, _ := correlated(t, workspace, "two-sources", correlationRulesEntry); report.RulesSHA256 != shown {
		t.Fatalf("reindenting the rules changed their canonical digest: %s", report.RulesSHA256)
	}
	if got := app.OpenCorrelationReview(review); got.State != desktop.Completed || got.View == nil {
		t.Fatalf("the same declarations, reindented, were refused for review: %+v", got)
	}
	// Without the patient rule they are other declarations.
	narrowed := `{"schema":"readmit-correlation-rules/v1","rules":[{"id":"acknowledgements","operator":"acknowledges","scope":"source"}]}`
	writeDocument(t, workspace, correlationRulesEntry, narrowed)
	if report, _ := correlated(t, workspace, "two-sources", correlationRulesEntry); report.RulesSHA256 == shown {
		t.Fatal("other declarations kept the canonical digest on screen")
	}
	if got := app.OpenCorrelationReview(review); got.State != desktop.Failed || got.Reason != "the displayed rules changed; reopen the sequence before reviewing" || got.View != nil {
		t.Fatalf("a review under rules changed on disk was answered: %+v", got)
	}
}

// machineBytes is the machine finding exactly as `readmit correlate --format
// json` printed it, without the final newline a terminal needs.
func machineBytes(printed string) string {
	return strings.TrimSuffix(printed, "\n")
}

// A review opened in the window with no retained history starts from the
// machine finding `readmit correlate` reports over the same case and rules:
// the same links in the same order, each with its own linkage, rule and
// occurrences, unreviewed, and the same collisions, never resolved. The
// machine finding the review binds to is the digest of what the command line
// printed. The case the native window retained is reviewed the same way.
func TestTheWindowReviewsTheLinksReadmitCorrelateReports(t *testing.T) {
	workspace := correlationParityWorkspace(t)
	app := desktopApp(t, workspace)
	for _, entry := range []string{"two-sources", "native"} {
		report, printed := correlated(t, workspace, entry, correlationRulesEntry)
		identity := verifiedIdentity(t, app, workspace, entry)
		opened := app.OpenCorrelationReview(desktop.CorrelationReviewRequest{
			Workspace: workspace, Case: entry, Identity: identity, Rules: correlationRulesEntry, RulesSHA256: report.RulesSHA256,
		})
		if opened.State != desktop.Completed || opened.View == nil {
			t.Fatalf("%s: the review did not open: %+v", entry, opened)
		}
		view := opened.View
		if view.Machine != sha256Hex([]byte(machineBytes(printed))) {
			t.Fatalf("%s: the review binds to machine finding %s, not the one the command line printed", entry, view.Machine)
		}
		if view.TotalLinks != len(report.Links) || view.TotalCollisions != len(report.Collisions) || view.TotalDecisions != 0 || len(view.History) != 0 {
			t.Fatalf("%s: the review counted %d links and %d collisions, the command line %d and %d", entry, view.TotalLinks, view.TotalCollisions, len(report.Links), len(report.Collisions))
		}
		for index, link := range report.Links {
			reviewed := view.Links[index]
			if reviewed.ID != link.ID || reviewed.Linkage != string(link.Linkage) || reviewed.Rule != link.Rule ||
				!reflect.DeepEqual(reviewed.Occurrences, link.Occurrences) || reviewed.Status != "unreviewed" || reviewed.TotalOccurrences != len(link.Occurrences) {
				t.Fatalf("%s: link %s reviewed as %+v", entry, link.ID, reviewed)
			}
		}
		for index, collision := range report.Collisions {
			if !reflect.DeepEqual(view.Collisions[index].Finding, collision) {
				t.Fatalf("%s: collision %d reviewed as %+v, reported as %+v", entry, index, view.Collisions[index].Finding, collision)
			}
		}
	}
}

// Every decision the window records is kept beside the machine finding the
// command line reports, never in it. Accepting, rejecting and adding a pair
// each write one new review directory whose machine finding is byte for byte
// what `readmit correlate --format json` printed, whose history the shared
// reader reads back against that report, and whose earlier revisions stay as
// they were. A write over an existing directory is refused and changes
// nothing; a retained history is refused under other rules; and the command
// line's own report, which never consumes a human review, is unchanged.
func TestTheWindowRecordsCorrelationDecisionsBesideTheReportReadmitCorrelateProduces(t *testing.T) {
	workspace := correlationParityWorkspace(t)
	app := desktopApp(t, workspace)
	casePath := filepath.Join(workspace, "two-sources")
	report, printed := correlated(t, workspace, "two-sources", correlationRulesEntry)
	evidence := treeOf(t, casePath)
	identity := verifiedIdentity(t, app, workspace, "two-sources")
	request := desktop.CorrelationReviewRequest{Workspace: workspace, Case: "two-sources", Identity: identity, Rules: correlationRulesEntry, RulesSHA256: report.RulesSHA256}
	opened := app.OpenCorrelationReview(request)
	if opened.State != desktop.Completed || opened.View == nil {
		t.Fatalf("the review did not open: %+v", opened)
	}
	pair := [2]string{report.Collisions[0].Occurrences[0].Occurrence, report.Collisions[0].Occurrences[1].Occurrence}
	decisions := []correlate.Decision{
		{Action: "accept", Link: report.Links[0].ID, Actor: "analyst one", Reason: "the acknowledgement names this booking"},
		{Action: "reject", Link: report.Links[1].ID, Actor: "analyst two", Reason: "a separate observation of that message"},
		{Action: "add", From: pair[0], To: pair[1], Actor: "analyst one", Reason: "the capture context places these together"},
	}
	var previous, mapping string
	for index, decision := range decisions {
		request.Previous, request.Mapping, request.Decision = previous, mapping, decision
		if index == 0 {
			request.Mapping = opened.View.Mapping
		}
		request.Output = fmt.Sprintf("review-%d", index+1)
		saved := app.DecideCorrelation(request)
		if saved.State != desktop.Completed || saved.View == nil || saved.Output != request.Output {
			t.Fatalf("decision %d was not recorded: %+v", index+1, saved)
		}
		directory := filepath.Join(workspace, request.Output)
		machine, err := os.ReadFile(filepath.Join(directory, "machine.json"))
		if err != nil || string(machine) != machineBytes(printed) {
			t.Fatalf("review %d retains a machine finding other than the one the command line printed: %v", index+1, err)
		}
		retained, err := correlate.ReadReview(directory, openedBundle(t, casePath), report)
		if err != nil || len(retained.Decisions) != index+1 || retained.Decisions[index] != decision || retained.Identity() != saved.View.Mapping {
			t.Fatalf("review %d is not read back against the command line's report: %v %+v", index+1, err, retained)
		}
		previous, mapping = request.Output, saved.View.Mapping
	}
	// Each revision is its own history: the first still holds one decision.
	first := treeOf(t, filepath.Join(workspace, "review-1"))
	reopened := app.OpenCorrelationReview(desktop.CorrelationReviewRequest{
		Workspace: workspace, Case: "two-sources", Identity: identity, Rules: correlationRulesEntry, RulesSHA256: report.RulesSHA256, Previous: "review-1", ShowValues: true,
	})
	if reopened.State != desktop.Completed || len(reopened.View.History) != 1 || reopened.View.History[0] != decisions[0] || reopened.View.Links[0].Status != "accepted" {
		t.Fatalf("the first revision did not reopen as itself: %+v", reopened)
	}
	// A decision written over an existing revision is refused and changes it not.
	request.Previous, request.Mapping, request.Output = "review-1", reopened.View.Mapping, "review-1"
	request.Decision = correlate.Decision{Action: "reject", Link: report.Links[2].ID, Actor: "analyst two", Reason: "written over the first revision"}
	if got := app.DecideCorrelation(request); got.State != desktop.Failed || got.Reason != "correlation review destination must be new and its parent readable and writable" || got.Output != "" {
		t.Fatalf("a decision over an existing revision was answered: %+v", got)
	}
	if !reflect.DeepEqual(treeOf(t, filepath.Join(workspace, "review-1")), first) {
		t.Fatal("a refused decision changed the revision it named")
	}
	// Under other rules the machine finding is another one, and the retained
	// history is refused rather than applied to it.
	writeDocument(t, workspace, "narrow-rules.json", `{"schema":"readmit-correlation-rules/v1","rules":[{"id":"acknowledgements","operator":"acknowledges","scope":"source"}]}`)
	stale := app.OpenCorrelationReview(desktop.CorrelationReviewRequest{Workspace: workspace, Case: "two-sources", Identity: identity, Rules: "narrow-rules.json", Previous: "review-3"})
	if stale.State != desktop.Failed || stale.Reason != "correlation review is incomplete, changed or incompatible; reopen the original evidence and rules" || stale.View != nil {
		t.Fatalf("a history retained under other rules was answered: %+v", stale)
	}
	if _, again := correlated(t, workspace, "two-sources", correlationRulesEntry); again != printed {
		t.Fatal("recording human decisions changed the command line's report")
	}
	if !reflect.DeepEqual(treeOf(t, casePath), evidence) {
		t.Fatal("reviewing correlation links changed the case")
	}
}

func openedBundle(t *testing.T, path string) *bundle.Bundle {
	t.Helper()
	opened, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return opened
}

// A retained sequence-analysis declaration opens in the window as the shared
// reader reads it, named by the digest of its exact bytes, and the sequence
// laid out under it explains the case exactly as the shared evaluator does
// over the report `readmit correlate` produces. Saved again from what was
// opened, it reopens as itself. Every declaration the reader refuses is
// refused by opening, saving and laying out in one sentence, writing nothing;
// a declaration of another case opens, and laying this case out under it is
// refused.
func TestTheWindowOpensTheSequenceAnalysisTheSequenceApplies(t *testing.T) {
	workspace := correlationParityWorkspace(t)
	app := desktopApp(t, workspace)
	report, _ := correlated(t, workspace, "two-sources", correlationRulesEntry)
	identity := verifiedIdentity(t, app, workspace, "two-sources")
	declared := `{
  "schema": "readmit-sequence-analysis/v1",
  "case_identity": "` + identity + `",
  "rules_sha256": "` + report.RulesSHA256 + `",
  "clock_tolerance_seconds": 5,
  "windows": [
    {"source": "s0001", "start": "2026-01-02T12:00:00+01:00", "end": "2026-01-02T13:00:00+01:00", "coverage": "partial"},
    {"source": "s0002", "start": "2026-01-02T11:00:00Z", "end": "2026-01-02T12:00:00Z", "coverage": "complete"}
  ],
  "retries": [{"first": "s0001-e000001", "retry": "s0001-e000002", "basis": "operator_reported_retry"}],
  "downstream": [{"occurrence": "s0001-e000004", "source": "s0002", "rule": "same-message"}]
}
`
	path := writeDocument(t, workspace, "analysis.json", declared)
	parsed, err := sequenceanalysis.Parse([]byte(declared))
	if err != nil {
		t.Fatal(err)
	}
	opened := app.OpenSequenceAnalysis(workspace, "analysis.json")
	if opened.State != desktop.Completed || opened.Declaration == nil || !reflect.DeepEqual(*opened.Declaration, parsed) || opened.SHA256 != sha256Hex([]byte(declared)) {
		t.Fatalf("the declaration opened as %+v, the shared reader reads %+v", opened, parsed)
	}
	if canonical, err := sequenceanalysis.Parse([]byte(opened.Document)); err != nil || !reflect.DeepEqual(canonical, parsed) {
		t.Fatalf("the document the editor holds is not the declaration it opened: %v", err)
	}

	laid := app.OpenSequence(desktop.SequenceRequest{
		Workspace: workspace, Case: "two-sources", Identity: identity, Rules: correlationRulesEntry, Analysis: "analysis.json", Limit: desktop.MaxSequenceEvents,
	})
	if laid.State != desktop.Completed || laid.Sequence == nil || laid.Sequence.Analysis == nil || laid.Sequence.AnalysisEntry != "analysis.json" {
		t.Fatalf("the sequence was not laid out under the declaration: %+v", laid)
	}
	evaluated, err := sequenceanalysis.Evaluate(openedBundle(t, filepath.Join(workspace, "two-sources")), parsed, &report)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*laid.Sequence.Analysis, *evaluated) {
		t.Fatalf("the window explained the case as %+v, the shared evaluator over the command line's report as %+v", laid.Sequence.Analysis, evaluated)
	}

	saved := app.SaveSequenceAnalysis(desktop.RuleDocumentSaveRequest{Workspace: workspace, Document: opened.Document, Output: "analysis-2.json"})
	written, err := os.ReadFile(filepath.Join(workspace, "analysis-2.json"))
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != desktop.Completed || string(written) != saved.Document || saved.SHA256 != sha256Hex(written) || !reflect.DeepEqual(*saved.Declaration, parsed) {
		t.Fatalf("the declaration was saved as %+v", saved)
	}
	if again := app.OpenSequenceAnalysis(workspace, "analysis-2.json"); again.SHA256 != saved.SHA256 || again.Document != saved.Document {
		t.Fatalf("a saved declaration does not reopen as itself: %+v", again)
	}
	if current, _ := os.ReadFile(path); string(current) != declared {
		t.Fatal("opening and saving a declaration changed the one it was opened from")
	}

	other := strings.Replace(declared, identity, strings.Repeat("a", 64), 1)
	writeDocument(t, workspace, "other-case.json", other)
	if got := app.OpenSequenceAnalysis(workspace, "other-case.json"); got.State != desktop.Completed || got.Declaration.CaseIdentity != strings.Repeat("a", 64) {
		t.Fatalf("a declaration of another case did not open as that case's: %+v", got)
	}
	if got := app.OpenSequence(desktop.SequenceRequest{Workspace: workspace, Case: "two-sources", Identity: identity, Rules: correlationRulesEntry, Analysis: "other-case.json", Limit: desktop.MaxSequenceEvents}); got.State != desktop.Failed || got.Reason != "sequence analysis names a different case identity" || got.Sequence != nil {
		t.Fatalf("this case was laid out under another case's declaration: %+v", got)
	}

	refused := map[string]struct{ document, sentence string }{
		"misspelled.json":     {strings.Replace(declared, `"retries"`, `"retrys"`, 1), "sequence analysis requires every declared member"},
		"unknown-offset.json": {strings.Replace(declared, "2026-01-02T11:00:00Z", "2026-01-02T11:00:00-00:00", 1), "invalid sequence analysis document"},
		"newer.json":          {strings.Replace(declared, "readmit-sequence-analysis/v1", "readmit-sequence-analysis/v2", 1), "unsupported sequence analysis declaration or limits"},
		"truncated.json":      {declared[:120], "invalid sequence analysis JSON"},
	}
	for entry, declaration := range refused {
		writeDocument(t, workspace, entry, declaration.document)
	}
	before := treeOf(t, workspace)
	for entry, declaration := range refused {
		if got := app.OpenSequenceAnalysis(workspace, entry); got.State != desktop.Failed || got.Reason != declaration.sentence || got.Declaration != nil {
			t.Fatalf("%s opened as %+v", entry, got)
		}
		if got := app.SaveSequenceAnalysis(desktop.RuleDocumentSaveRequest{Workspace: workspace, Document: declaration.document, Output: "saved-" + entry}); got.State != desktop.Failed || got.Reason != declaration.sentence {
			t.Fatalf("%s saved as %+v", entry, got)
		}
		if got := app.OpenSequence(desktop.SequenceRequest{Workspace: workspace, Case: "two-sources", Identity: identity, Rules: correlationRulesEntry, Analysis: entry, Limit: desktop.MaxSequenceEvents}); got.State != desktop.Failed || got.Reason != declaration.sentence {
			t.Fatalf("the case was laid out under %s: %+v", entry, got)
		}
	}
	if !reflect.DeepEqual(treeOf(t, workspace), before) {
		t.Fatal("refusing declarations changed the workspace")
	}
}
