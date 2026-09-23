//go:build !windows

package desktop_test

// Most entries that name a case, a retained run or a retained review are held
// to the listing's rule by the facade itself, through artifactpath, before
// any reader is handed them: one real folder of the open workspace, never a
// symbolic link wherever it points. Each member below is handed a symbolic
// link out of the workspace and a link to one of its own entries, each
// leading to a real entry of its kind that the member accepts when named
// directly, and must refuse both with the facade's own sentence, creating
// nothing in the workspace and changing nothing outside it. A state alone
// would not show which check refused the link, or that a reader had not
// followed it first.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
	"github.com/bharm16/readmit/internal/transform"
)

// caseEntry is the sentence the facade refuses a case with when it is not one
// folder of the open workspace.
var caseEntry = []string{"a case must be named by one directory entry of the open workspace"}

// Opening a case, its grid, its index and one occurrence, building and
// describing its index, and comparing two collections.
func TestCaseReadersRefuseALinkToACaseTheyAccept(t *testing.T) {
	root := t.TempDir()
	app := workspaceApp(t)
	incident := writeCase(t, root, "incident", framed(gridBooking)+framed(gridAccepted))
	writeIndex(t, root, "incident.index.json", incident, nil)
	before := writeCase(t, root, "before", framed(cmpBookedBefore)+framed(cmpMovedBefore))
	writeCase(t, root, "after", framed(cmpBookedAfter)+framed(cmpInsertedAfter)+framed(cmpMovedAfter))
	grid := app.OpenGrid(root, "incident", "incident.index.json", 0, 10)
	if grid.State != desktop.Completed || grid.Grid == nil || len(grid.Grid.Rows) == 0 {
		t.Fatalf("the grid an occurrence is inspected from: %+v", grid)
	}
	compare := func(left, right string) refused {
		result := app.Compare(desktop.CompareRequest{Workspace: root, Left: left, Identity: before.Identity, Right: right,
			Keys: []string{cmpKey}, Limit: desktop.MaxComparisonRows})
		return refused{result.State, result.Reason}
	}
	compared := []string{"a compared collection must be named by one directory entry of the open workspace"}
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"OpenCase", "incident", caseEntry, func(entry string) refused {
			result := app.OpenCase(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"OpenGrid(name)", "incident", caseEntry, func(entry string) refused {
			result := app.OpenGrid(root, entry, "incident.index.json", 0, 10)
			return refused{result.State, result.Reason}
		}},
		{"DescribeIndex(caseName)", "incident", caseEntry, func(entry string) refused {
			result := app.DescribeIndex(root, entry, "")
			return refused{result.State, result.Reason}
		}},
		{"BuildIndex(Case)", "incident", caseEntry, func(entry string) refused {
			result := app.BuildIndex(desktop.BuildIndexRequest{Workspace: root, Case: entry, Output: "built-" + entry + ".index.json",
				Fields: []string{"PID-5"}, Retention: "values", RetainUntil: "indefinite"})
			return refused{result.State, result.Reason}
		}},
		{"InspectOccurrence(Case)", "incident", caseEntry, func(entry string) refused {
			result := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: entry, Identity: incident.Identity,
				Occurrence: grid.Grid.Rows[0].ID, Path: "PID[1]-5", ByteOffset: -1})
			return refused{result.State, result.Reason}
		}},
		{"Compare(Left)", "before", compared, func(entry string) refused { return compare(entry, "after") }},
		{"Compare(Right)", "after", compared, func(entry string) refused { return compare("before", entry) }},
	})
}

// The case a sequence, a correlation review and a correlation decision are
// read from, and the retained review a decision is reopened from.
func TestSequenceAndCorrelationRefuseALinkToACaseOrReviewTheyAccept(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	review := func(caseName, previous string) desktop.CorrelationReviewRequest {
		return desktop.CorrelationReviewRequest{Workspace: root, Case: caseName, Identity: identity, Rules: seqRulesEntry, Previous: previous}
	}
	opened := app.OpenCorrelationReview(review("incident", ""))
	if opened.State != desktop.Completed || opened.View == nil || len(opened.View.Links) < 2 {
		t.Fatalf("correlation review: %+v", opened)
	}
	// decided decides one link of the review, over the retained review
	// previous when one is named, as the view it was shown maps it.
	decided := func(caseName, previous, output string, view *correlate.ReviewedView, link int) refused {
		request := review(caseName, previous)
		request.Mapping = view.Mapping
		request.Decision = correlate.Decision{Action: "reject", Link: view.Links[link].ID, Actor: "local analyst", Reason: "separate observation"}
		request.Output = output
		result := app.DecideCorrelation(request)
		return refused{result.State, result.Reason}
	}
	if first := decided("incident", "", "decided", opened.View, 0); first.state != desktop.Completed {
		t.Fatalf("a retained review to reopen: %+v", first)
	}
	reopened := app.OpenCorrelationReview(review("incident", "decided"))
	if reopened.State != desktop.Completed || reopened.View == nil {
		t.Fatalf("the retained review reopened: %+v", reopened)
	}
	retained := []string{"a review must be one directory entry of the open workspace"}
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"OpenSequence(Case)", "incident", caseEntry, func(entry string) refused {
			request := sequenceRequest(root, identity, seqRulesEntry)
			request.Case = entry
			result := app.OpenSequence(request)
			return refused{result.State, result.Reason}
		}},
		{"OpenCorrelationReview(Case)", "incident", caseEntry, func(entry string) refused {
			result := app.OpenCorrelationReview(review(entry, ""))
			return refused{result.State, result.Reason}
		}},
		{"DecideCorrelation(Case)", "incident", caseEntry, func(entry string) refused {
			return decided(entry, "", "decided-"+entry, opened.View, 0)
		}},
		{"OpenCorrelationReview(Previous)", "decided", retained, func(entry string) refused {
			result := app.OpenCorrelationReview(review("incident", entry))
			return refused{result.State, result.Reason}
		}},
		{"DecideCorrelation(Previous)", "decided", retained, func(entry string) refused {
			return decided("incident", entry, "redecided-"+entry, reopened.View, 1)
		}},
	})
}

// The case a diagnosis runs over and a finding review joins, the retained
// report a person reopens and reviews, and every case a grouping diagnoses.
func TestDiagnosisAndFindingReviewRefuseALinkToACaseOrReportTheyAccept(t *testing.T) {
	app, root, identity, reportSHA256 := reviewableWorkspace(t)
	findings := func(set func(*desktop.FindingReviewRequest)) desktop.FindingReviewRequest {
		request := findingReviewRequest(root, identity, reportSHA256)
		set(&request)
		return request
	}
	report := []string{"a diagnosis report must be one directory entry of the open workspace"}
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"RunDiagnosis(Case)", "acked", caseEntry, func(entry string) refused {
			result := app.RunDiagnosis(desktop.DiagnosisRequest{Workspace: root, Case: entry, Identity: identity, Builtin: "siu", Output: "diagnosed-" + entry})
			return refused{result.State, result.Reason}
		}},
		{"OpenDiagnosisReport", "diagnosis-out", report, func(entry string) refused {
			result := app.OpenDiagnosisReport(root, entry, 0)
			return refused{result.State, result.Reason}
		}},
		{"GroupDiagnoses(Cases)", "acked", []string{"every grouped case must be named by one directory entry of the open workspace"}, func(entry string) refused {
			result := app.GroupDiagnoses(desktop.GroupDiagnosesRequest{Workspace: root, Cases: []string{entry}, Builtin: "siu"})
			return refused{result.State, result.Reason}
		}},
		{"ReviewFindings(Case)", "acked", caseEntry, func(entry string) refused {
			result := app.ReviewFindings(findings(func(r *desktop.FindingReviewRequest) { r.Case = entry }))
			return refused{result.State, result.Reason}
		}},
		{"ReviewFindings(Report)", "diagnosis-out", report, func(entry string) refused {
			result := app.ReviewFindings(findings(func(r *desktop.FindingReviewRequest) { r.Report = entry }))
			return refused{result.State, result.Reason}
		}},
		{"DecideFindings(Case)", "acked", caseEntry, func(entry string) refused {
			result := app.DecideFindings(findings(func(r *desktop.FindingReviewRequest) {
				r.Case, r.Output, r.DecisionsOutput = entry, "reviewed-case-"+entry, "decisions-case-"+entry+".json"
			}))
			return refused{result.State, result.Reason}
		}},
		{"DecideFindings(Report)", "diagnosis-out", report, func(entry string) refused {
			result := app.DecideFindings(findings(func(r *desktop.FindingReviewRequest) {
				r.Report, r.Output, r.DecisionsOutput = entry, "reviewed-report-"+entry, "decisions-report-"+entry+".json"
			}))
			return refused{result.State, result.Reason}
		}},
	})
}

// The case a transformation is previewed over and a plan is authored against.
func TestTransformationRefusesALinkToACaseItAccepts(t *testing.T) {
	app, root, identity := transformWorkspace(t, `{"operator":"shift-dates/v1","shift":"24h"}`)
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"PreviewTransformation(Case)", "incident", caseEntry, func(entry string) refused {
			request := transformRequest(root, identity)
			request.Case = entry
			result := app.PreviewTransformation(request)
			return refused{result.State, result.Reason}
		}},
		{"SaveTransformPlan(Case)", "incident", caseEntry, func(entry string) refused {
			result := app.SaveTransformPlan(desktop.TransformPlanRequest{Workspace: root, Case: entry, Identity: identity, Rules: "rules.json",
				Steps: []transform.Step{{Operator: transform.ShiftDates, Shift: "24h"}}, Output: "plan-" + entry + ".json"})
			return refused{result.State, result.Reason}
		}},
	})
}

// The case a reproducer is edited, undone and built from, and the two
// revisions a comparison of revisions reads.
func TestReproducerRefusesALinkToACaseOrRevisionItAccepts(t *testing.T) {
	app, root, identity := reproducerWorkspace(t)
	selected := edit(t, app, root, identity, reproducer.Plan{}, reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID})
	// Undoing a second step leaves the selection, so the undo named directly
	// has a reproducer to report.
	widened := edit(t, app, root, identity, selected.Reproducer.Plan, reproducer.Step{Operator: reproducer.IncludePriorIdentity, Identity: []string{"SCH-2.1", "SCH-2.2"}})
	buildRevision(t, app, root, identity, "revision", reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID})
	// on names entry as the case of a request over plan.
	on := func(entry string, plan reproducer.Plan, step reproducer.Step) desktop.ReproducerRequest {
		named := request(root, identity, plan, step)
		named.Case = entry
		return named
	}
	compare := func(left, right string) refused {
		result := app.CompareReproducers(desktop.ReproducerComparisonRequest{Workspace: root, Left: left, Right: right})
		return refused{result.State, result.Reason}
	}
	compared := []string{"a compared revision must be named by one directory entry of the open workspace"}
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"EditReproducer(Case)", "incident", caseEntry, func(entry string) refused {
			result := app.EditReproducer(on(entry, reproducer.Plan{}, reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID}))
			return refused{result.State, result.Reason}
		}},
		{"UndoReproducer(Case)", "incident", caseEntry, func(entry string) refused {
			result := app.UndoReproducer(on(entry, widened.Reproducer.Plan, reproducer.Step{}))
			return refused{result.State, result.Reason}
		}},
		{"BuildReproducer(Case)", "incident", caseEntry, func(entry string) refused {
			built := on(entry, selected.Reproducer.Plan, reproducer.Step{})
			built.Output = "built-" + entry
			result := app.BuildReproducer(built)
			return refused{result.State, result.Reason}
		}},
		{"CompareReproducers(Left)", "revision", compared, func(entry string) refused { return compare(entry, "revision") }},
		{"CompareReproducers(Right)", "revision", compared, func(entry string) refused { return compare("revision", entry) }},
	})
	// The run offered as a revision's proof is refused by the same rule before
	// it is read. No run of this revision is retained here, so the links lead to
	// the revision's own folder and its copy outside, and the refusal must be
	// the facade's sentence rather than the proof reader's.
	proof := func(left, right string) refused {
		result := app.CompareReproducers(desktop.ReproducerComparisonRequest{Workspace: root, Left: "revision", Right: "revision", LeftResult: left, RightResult: right})
		return refused{result.State, result.Reason}
	}
	links := map[string]string{
		"a symbolic link out of the workspace":         "link-revision",
		"a symbolic link to an entry of the workspace": "alias-revision",
	}
	run := []string{"a retained run must be named by one directory entry of the open workspace"}
	refusesEveryEntry(t, []confinedMember{
		{"CompareReproducers(LeftResult)", run, links, func(entry string) refused { return proof(entry, "") }},
		{"CompareReproducers(RightResult)", run, links, func(entry string) refused { return proof("", entry) }},
	})
}

// The retained export review a person reads and decides on.
func TestAnExportReviewRefusesALinkToAReviewItAccepts(t *testing.T) {
	app := workspaceApp(t)
	root, review, _ := readyReview(t, app)
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"OpenReview(Review)", review.Review, []string{"an export review must be named by one directory entry of the open workspace"}, func(entry string) refused {
			result := app.OpenReview(desktop.ReviewRequest{Workspace: root, Review: entry, Limit: desktop.MaxReviewFindings})
			return refused{result.State, result.Reason}
		}},
	})
}

// The case a regression test is authored, saved, suggested and approved
// against, and the retained results a run comparison reads.
func TestAuthoringAndRunComparisonRefuseALinkToACaseOrResultTheyAccept(t *testing.T) {
	app, root := guided(t)
	root = resolved(t, root)
	spec := authorGuidedTest(t, app, root, "guided-test.json", 1)
	for output, trial := range map[string]string{"broken": guide.StepBaseline, "fixed": guide.StepPostFix, "repeat": guide.StepPostFix} {
		if practiced := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: trial, Output: output}); practiced.State != desktop.Completed {
			t.Fatalf("a practice run: %+v", practiced)
		}
		if err := os.Rename(filepath.Join(root, output, "result"), filepath.Join(root, output+"-result")); err != nil {
			t.Fatal(err)
		}
	}
	opened := app.OpenCase(root, guide.CaseName)
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("open the sample case: %+v", opened)
	}
	authoring := desktop.TestRequest{Workspace: root, Case: guide.CaseName, Identity: opened.Case.Identity}
	for _, answer := range []testauthor.Answer{
		{Stage: testauthor.StageName, Name: "Rescheduling updates the original appointment"},
		{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001", "s0001-e000002"}},
		{Stage: testauthor.StageTarget, Target: guide.TargetName},
		{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		{Stage: testauthor.StageObservation, Observation: "practice-observation.json"},
		{Stage: testauthor.StageReset, Reset: "Start a fresh practice receiver with an empty ledger before each run."},
	} {
		authoring.Answer = answer
		authored := app.AuthorTest(authoring)
		if authored.Test == nil {
			t.Fatalf("%s: %+v", answer.Stage, authored)
		}
		authoring.Draft, authoring.Answer = authored.Test.Draft, testauthor.Answer{}
	}
	authoring.Suggest = &testauthor.SuggestionRequest{Result: "fixed-result", Ledger: true}
	proposed := app.SuggestExpectations(authoring)
	if proposed.State != desktop.Completed || proposed.Test == nil || proposed.Test.Suggestions == nil || len(proposed.Test.Suggestions.Suggestions) == 0 {
		t.Fatalf("proposed expectations: %+v", proposed)
	}
	suggestions := proposed.Test.Suggestions
	// on names entry as the case a copy of the authoring request is made
	// against.
	on := func(entry string) desktop.TestRequest {
		named := authoring
		named.Case = entry
		return named
	}
	compare := func(set func(*desktop.RunComparisonRequest)) refused {
		named := desktop.RunComparisonRequest{Workspace: root, Baseline: "broken-result", Current: "fixed-result"}
		set(&named)
		result := app.CompareRuns(named)
		return refused{result.State, result.Reason}
	}
	comparison := []string{"comparison requires verified workspace results or durable runs, an optional valid approval, and distinct complete repeat samples"}
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"AuthorTest(Case)", guide.CaseName, caseEntry, func(entry string) refused {
			named := on(entry)
			named.Suggest, named.Answer = nil, testauthor.Answer{Stage: testauthor.StageName, Name: "Rescheduling updates the original appointment"}
			result := app.AuthorTest(named)
			return refused{result.State, result.Reason}
		}},
		{"SuggestExpectations(Case)", guide.CaseName, caseEntry, func(entry string) refused {
			result := app.SuggestExpectations(on(entry))
			return refused{result.State, result.Reason}
		}},
		{"ApproveExpectations(Case)", guide.CaseName, caseEntry, func(entry string) refused {
			named := on(entry)
			named.Review = &testauthor.Review{Result: "fixed-result", Identity: suggestions.Origin.Identity,
				Decisions: []testauthor.Decision{{Suggestion: suggestions.Suggestions[0].ID, Approved: true}}}
			result := app.ApproveExpectations(named)
			return refused{result.State, result.Reason}
		}},
		{"SaveTest(Case)", guide.CaseName, caseEntry, func(entry string) refused {
			named := on(entry)
			named.Suggest = nil
			named.Draft.Expectations = []testauthor.Expectation{{ID: "one-appointment", Operator: testauthor.LedgerCount, Count: suggestions.Suggestions[0].Count}}
			named.Output = "saved-" + entry + ".json"
			result := app.SaveTest(named)
			return refused{result.State, result.Reason}
		}},
		{"CompareRuns(Baseline)", "broken-result", comparison, func(entry string) refused {
			return compare(func(r *desktop.RunComparisonRequest) { r.Baseline = entry })
		}},
		{"CompareRuns(Current)", "fixed-result", comparison, func(entry string) refused {
			return compare(func(r *desktop.RunComparisonRequest) { r.Current = entry })
		}},
		{"CompareRuns(Repeats)", "repeat-result", comparison, func(entry string) refused {
			return compare(func(r *desktop.RunComparisonRequest) { r.Repeats = []string{entry} })
		}},
	})
}

// The case a controlled reduction previews and runs its trials from, against
// the acking peer.
func TestAReductionRefusesALinkToACaseItAccepts(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	root, _ := confinementWorkspaces(t, peer.address)
	app := workspaceApp(t)
	opened := app.OpenCase(root, "case")
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("open the case a reduction reduces: %+v", opened)
	}
	reduction := func(entry string) desktop.ReductionRequest {
		return desktop.ReductionRequest{Workspace: root, Case: entry, Identity: opened.Case.Identity,
			Spec: "booking.json", Assertions: []string{"ack"}, Trials: 4, Confirmations: 1,
			ResetPlan: "plan.json", Target: "target.json", Work: "reduction-" + entry}
	}
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"PreviewReduction(Case)", "case", caseEntry, func(entry string) refused {
			result := app.PreviewReduction(reduction(entry))
			return refused{result.State, result.Reason}
		}},
		{"StartReduction(Case)", "case", caseEntry, func(entry string) refused {
			result := app.StartReduction(reduction(entry))
			return refused{result.State, result.Reason}
		}},
	})
}
