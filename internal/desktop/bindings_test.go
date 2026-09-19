package desktop_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
	"github.com/bharm16/readmit/internal/transform"
)

// bindingsFile is the frontend's only view of the Go facade. Wails publishes
// each bound method at window.go.desktop.App.<Method>, so a method added to the
// facade without a declaration here is a binding the frontend cannot call.
const bindingsFile = "../../desktop/frontend/src/bindings.ts"

func TestFrontendBindingsCoverTheFacade(t *testing.T) {
	declarations, err := os.ReadFile(bindingsFile)
	if err != nil {
		t.Fatal(err)
	}
	bindings := string(declarations)
	if !strings.Contains(bindings, "window.go?.desktop?.App") {
		t.Fatalf("%s does not read the namespace Wails publishes for this facade", bindingsFile)
	}
	facade := reflect.TypeOf(&desktop.App{})
	if facade.NumMethod() == 0 {
		t.Fatal("the facade exposes no bound methods")
	}
	for i := range facade.NumMethod() {
		method := facade.Method(i).Name
		if !strings.Contains(bindings, method+"(") {
			t.Errorf("facade method %s has no typed declaration in %s", method, bindingsFile)
		}
	}
}

// The frontend receives typed objects, never rendered prose, so the six states
// the facade can report must all be expressible on that side of the boundary.
func TestFrontendBindingsDeclareEveryOperationState(t *testing.T) {
	declarations, err := os.ReadFile(bindingsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []desktop.State{desktop.Empty, desktop.Busy, desktop.Cancelled, desktop.Failed, desktop.PermissionDenied, desktop.Completed} {
		if !strings.Contains(string(declarations), `"`+string(state)+`"`) {
			t.Errorf("state %q has no declaration in %s", state, bindingsFile)
		}
	}
}

// The frontend reads these members by name. A Go member the declarations do not
// carry is a silently broken interface that still type-checks on both sides, so
// every member of every bound result type must be declared.
func TestFrontendBindingsDeclareEveryResultMember(t *testing.T) {
	declarations, err := os.ReadFile(bindingsFile)
	if err != nil {
		t.Fatal(err)
	}
	bindings := string(declarations)
	for _, bound := range []reflect.Type{
		reflect.TypeOf(desktop.WorkspaceResult{}),
		reflect.TypeOf(desktop.Workspace{}),
		reflect.TypeOf(desktop.Artifact{}),
		reflect.TypeOf(desktop.CaseResult{}),
		reflect.TypeOf(desktop.Case{}),
		reflect.TypeOf(desktop.RecentResult{}),
		reflect.TypeOf(desktop.ProjectResult{}),
		reflect.TypeOf(desktop.RevisionsResult{}),
		reflect.TypeOf(desktop.ShellResult{}),
		reflect.TypeOf(desktop.Shell{}),
		reflect.TypeOf(desktop.Region{}),
		reflect.TypeOf(desktop.Indicator{}),
		reflect.TypeOf(desktop.Command{}),
		reflect.TypeOf(desktop.Privacy{}),
		reflect.TypeOf(desktop.SearchResult{}),
		reflect.TypeOf(desktop.Match{}),
		reflect.TypeOf(desktop.FiltersResult{}),
		reflect.TypeOf(desktop.GridResult{}),
		reflect.TypeOf(desktop.Grid{}),
		reflect.TypeOf(desktop.Row{}),
		reflect.TypeOf(desktop.SessionResult{}),
		reflect.TypeOf(desktop.RecoveryResult{}),
		reflect.TypeOf(desktop.Session{}),
		reflect.TypeOf(desktop.View{}),
		reflect.TypeOf(desktop.Draft{}),
		reflect.TypeOf(desktop.ReproducerRequest{}),
		reflect.TypeOf(desktop.ReproducerResult{}),
		reflect.TypeOf(desktop.Reproducer{}),
		reflect.TypeOf(reproducer.Plan{}),
		reflect.TypeOf(reproducer.Step{}),
		reflect.TypeOf(reproducer.Resolution{}),
		reflect.TypeOf(reproducer.Retained{}),
		reflect.TypeOf(reproducer.Edit{}),
		reflect.TypeOf(reproducer.Unresolved{}),
		reflect.TypeOf(desktop.ReproducerComparisonRequest{}),
		reflect.TypeOf(desktop.ReproducerComparisonResult{}),
		reflect.TypeOf(reproducer.Comparison{}),
		reflect.TypeOf(reproducer.RevisionSummary{}),
		reflect.TypeOf(reproducer.Artifact{}),
		reflect.TypeOf(reproducer.StepChange{}),
		reflect.TypeOf(reproducer.RetentionChange{}),
		reflect.TypeOf(reproducer.EditChange{}),
		reflect.TypeOf(reproducer.UnresolvedChange{}),
		reflect.TypeOf(reproducer.Proof{}),
		reflect.TypeOf(reproducer.ProofSide{}),
		reflect.TypeOf(reproducer.AssertionProof{}),
		reflect.TypeOf(desktop.TestRequest{}),
		reflect.TypeOf(desktop.TestResult{}),
		reflect.TypeOf(desktop.TestDraft{}),
		reflect.TypeOf(testauthor.Draft{}),
		reflect.TypeOf(testauthor.Evidence{}),
		reflect.TypeOf(testauthor.Answer{}),
		reflect.TypeOf(testauthor.Expectation{}),
		reflect.TypeOf(testauthor.Resolution{}),
		reflect.TypeOf(testauthor.Target{}),
		reflect.TypeOf(testrunner.FieldValue{}),
		reflect.TypeOf(desktop.CompareRequest{}),
		reflect.TypeOf(desktop.CompareResult{}),
		reflect.TypeOf(desktop.Comparison{}),
		reflect.TypeOf(desktop.ComparisonRow{}),
		reflect.TypeOf(desktop.FieldDifference{}),
		reflect.TypeOf(diff.InputSummary{}),
		reflect.TypeOf(diff.Reference{}),
		reflect.TypeOf(diff.Summary{}),
		reflect.TypeOf(diff.SegmentChange{}),
		reflect.TypeOf(diff.Unsupported{}),
		reflect.TypeOf(desktop.ReviewRequest{}),
		reflect.TypeOf(desktop.ReviewResult{}),
		reflect.TypeOf(desktop.Review{}),
		reflect.TypeOf(desktop.ReviewSurface{}),
		reflect.TypeOf(desktop.ReviewFinding{}),
		reflect.TypeOf(exportreview.Coverage{}),
		reflect.TypeOf(exportreview.Scan{}),
		reflect.TypeOf(desktop.TransformRequest{}),
		reflect.TypeOf(desktop.TransformResult{}),
		reflect.TypeOf(desktop.Transformation{}),
		reflect.TypeOf(transform.Preview{}),
		reflect.TypeOf(transform.Artifact{}),
		reflect.TypeOf(transform.Plan{}),
		reflect.TypeOf(transform.Step{}),
		reflect.TypeOf(transform.Summary{}),
		reflect.TypeOf(transform.Entry{}),
		reflect.TypeOf(transform.Change{}),
		reflect.TypeOf(transform.Relation{}),
		reflect.TypeOf(transform.Combination{}),
		reflect.TypeOf(transform.Unsupported{}),
		reflect.TypeOf(profilepack.Identity{}),
		reflect.TypeOf(desktop.GuideResult{}),
		reflect.TypeOf(desktop.PracticeRequest{}),
		reflect.TypeOf(desktop.PracticeResult{}),
		reflect.TypeOf(desktop.Practice{}),
		reflect.TypeOf(desktop.PracticeAssertion{}),
		reflect.TypeOf(guide.Progress{}),
		reflect.TypeOf(guide.Step{}),
		reflect.TypeOf(desktop.SequenceRequest{}),
		reflect.TypeOf(desktop.SequenceResult{}),
		reflect.TypeOf(desktop.Sequence{}),
		reflect.TypeOf(desktop.SequenceEvent{}),
		reflect.TypeOf(desktop.SequenceSummary{}),
		reflect.TypeOf(desktop.Lane{}),
		reflect.TypeOf(desktop.GapCount{}),
		reflect.TypeOf(desktop.EvidenceReference{}),
		reflect.TypeOf(correlate.RuleReport{}),
		reflect.TypeOf(correlate.Unsupported{}),
		reflect.TypeOf(desktop.InspectRequest{}),
		reflect.TypeOf(desktop.InspectionResult{}),
		reflect.TypeOf(desktop.Inspection{}),
		reflect.TypeOf(desktop.InspectorByte{}),
		reflect.TypeOf(desktop.FieldMetadata{}),
		reflect.TypeOf(hl7.Node{}),
		reflect.TypeOf(grid.Filter{}),
		reflect.TypeOf(grid.FieldPredicate{}),
		reflect.TypeOf(project.Document{}),
		reflect.TypeOf(project.Settings{}),
		reflect.TypeOf(project.Case{}),
		reflect.TypeOf(project.Revisions{}),
		reflect.TypeOf(project.Revision{}),
		reflect.TypeOf(project.Operation{}),
		reflect.TypeOf(project.Note{}),
	} {
		for i := range bound.NumField() {
			member, _, _ := strings.Cut(bound.Field(i).Tag.Get("json"), ",")
			if member == "" {
				t.Fatalf("%s.%s carries no JSON member name", bound.Name(), bound.Field(i).Name)
			}
			if !strings.Contains(bindings, member+":") && !strings.Contains(bindings, member+"?:") {
				t.Errorf("%s member %q has no typed declaration in %s", bound.Name(), member, bindingsFile)
			}
		}
	}
}
