package desktop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/reproducer"
)

// One incident: an appointment is booked, accepted, and then rescheduled. The
// reschedule is the occurrence a person selects; the booking it shares a filler
// identifier with is the setup dependency it cannot be reproduced without, and
// the acknowledgement is what the case itself correlated.
const (
	repBooking    = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-1|P|2.5.1\rSCH|PLACER-1^READMIT|FILLER-1^READMIT||||CHECKUP|ROUTINE\rPID|1||MRN-1^^^READMIT^MR||DOE^JANE\r"
	repAccepted   = "MSH|^~\\&|RECV|LAB|READMIT|TEST|20260101120001||ACK^S12|CTL-2|P|2.5.1\rMSA|AA|CTL-1\r"
	repReschedule = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120100||SIU^S13|CTL-3|P|2.5.1\rSCH|PLACER-1^READMIT|FILLER-1^READMIT||||CHECKUP|ROUTINE\rPID|1||MRN-1^^^READMIT^MR||DOE^JANE\r"
	repGarbage    = "NOT-HL7-AT-ALL"

	repBookingID    = "s0001-e000001"
	repAcceptedID   = "s0001-e000002"
	repRescheduleID = "s0001-e000003"
	repGarbageID    = "s0001-e000004"
)

// reproducerWorkspace is that incident in an open workspace, with the app that
// reads it and the identity the grid would have displayed.
func reproducerWorkspace(t *testing.T) (*desktop.App, string, string) {
	t.Helper()
	root := t.TempDir()
	app := workspaceApp(t)
	incident := writeCase(t, root, "incident", framed(repBooking)+framed(repAccepted)+framed(repReschedule)+framed(repGarbage))
	return app, root, incident.Identity
}

func request(root, identity string, plan reproducer.Plan, step reproducer.Step) desktop.ReproducerRequest {
	return desktop.ReproducerRequest{Workspace: root, Case: "incident", Identity: identity, Plan: plan, Step: step}
}

// edit applies one step and fails the test if the facade refused it.
func edit(t *testing.T, app *desktop.App, root, identity string, plan reproducer.Plan, step reproducer.Step) desktop.ReproducerResult {
	t.Helper()
	result := app.EditReproducer(request(root, identity, plan, step))
	if result.Reproducer == nil {
		t.Fatalf("the facade refused a step it supports: %+v", result)
	}
	return result
}

// A person selects the reschedule, asks for its setup dependencies, and the
// window shows the booking and the acknowledgement it retained, each naming
// what required it — before anything is written.
func TestEditingAReproducerRetainsTheDependenciesOfWhatWasSelected(t *testing.T) {
	app, root, identity := reproducerWorkspace(t)
	empty := app.EditReproducer(desktop.ReproducerRequest{Workspace: root, Case: "incident", Identity: identity,
		Step: reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID}})
	if empty.State != desktop.Completed || empty.Reproducer.Plan.Case != identity {
		t.Fatalf("a request carrying no plan did not start one bound to this evidence: %+v", empty)
	}
	withPrior := edit(t, app, root, identity, empty.Reproducer.Plan,
		reproducer.Step{Operator: reproducer.IncludePriorIdentity, Identity: []string{"SCH-2.1", "SCH-2.2"}})
	withACKs := edit(t, app, root, identity, withPrior.Reproducer.Plan,
		reproducer.Step{Operator: reproducer.IncludeAcknowledgements})
	retained := map[string]reproducer.Retained{}
	for _, entry := range withACKs.Reproducer.Resolution.Occurrences {
		retained[entry.Parent] = entry
	}
	if len(retained) != 3 {
		t.Fatalf("retained %d occurrences for one reschedule and its dependencies", len(retained))
	}
	if retained[repRescheduleID].Reason != reproducer.Selected {
		t.Fatal("the selected occurrence lost its reason")
	}
	if retained[repBookingID].Reason != reproducer.PriorIdentity || retained[repBookingID].RequiredBy != repRescheduleID {
		t.Fatalf("the booking was not retained as the reschedule's setup dependency: %+v", retained[repBookingID])
	}
	if retained[repAcceptedID].Reason != reproducer.Acknowledgement || retained[repAcceptedID].RequiredBy != repBookingID {
		t.Fatalf("the acknowledgement of the booking was not retained: %+v", retained[repAcceptedID])
	}
	// The reschedule has no acknowledgement in this evidence, and that is
	// reported rather than left to be read as one being there.
	if len(withACKs.Reproducer.Resolution.Unresolved) == 0 {
		t.Fatal("an unacknowledged retained message was not reported")
	}
	// An empty plan retains nothing, which is a state on the way to a
	// reproducer rather than a failure.
	started := app.EditReproducer(request(root, identity, reproducer.Plan{}, reproducer.Step{Operator: reproducer.IncludeAcknowledgements}))
	if started.State != desktop.Empty || started.Reproducer == nil || len(started.Reproducer.Resolution.Occurrences) != 0 {
		t.Fatalf("a reproducer retaining nothing was not reported as empty: %+v", started)
	}
}

// Undo removes the last step and resolves what remains. A drop takes the edits
// of that occurrence with it, and undoing the drop brings both back, because
// the plan is replayed rather than patched.
func TestUndoingAReproducerStepRestoresWhatCameBefore(t *testing.T) {
	app, root, identity := reproducerWorkspace(t)
	selected := edit(t, app, root, identity, reproducer.Plan{},
		reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID})
	edited := edit(t, app, root, identity, selected.Reproducer.Plan,
		reproducer.Step{Operator: reproducer.SetField, Occurrence: repRescheduleID, Selector: "PID-3.1", Value: "MRN-REPRODUCER"})
	if len(edited.Reproducer.Resolution.Edits) != 1 {
		t.Fatalf("an edit of a retained occurrence was not applied: %+v", edited.Reproducer.Resolution)
	}
	dropped := edit(t, app, root, identity, edited.Reproducer.Plan,
		reproducer.Step{Operator: reproducer.DropOccurrence, Occurrence: repRescheduleID})
	if len(dropped.Reproducer.Resolution.Occurrences) != 0 || len(dropped.Reproducer.Resolution.Edits) != 0 {
		t.Fatal("dropping an occurrence left it or its edits in the reproducer")
	}
	undone := app.UndoReproducer(request(root, identity, dropped.Reproducer.Plan, reproducer.Step{}))
	if undone.State != desktop.Completed || len(undone.Reproducer.Resolution.Edits) != 1 {
		t.Fatalf("undoing a drop did not resolve that occurrence's edits again: %+v", undone)
	}
	for range 2 {
		undone = app.UndoReproducer(request(root, identity, undone.Reproducer.Plan, reproducer.Step{}))
	}
	if undone.State != desktop.Empty || len(undone.Reproducer.Plan.Steps) != 0 {
		t.Fatalf("undoing every step did not empty the plan: %+v", undone)
	}
	if nothing := app.UndoReproducer(request(root, identity, undone.Reproducer.Plan, reproducer.Step{})); nothing.State != desktop.Failed {
		t.Fatalf("undo invented a step to remove: %+v", nothing)
	}
}

// Building writes a new folder of the open workspace holding a derived case and
// the manifest beside it, and reports what was actually written.
func TestBuildingAReproducerWritesNewEvidenceIntoTheWorkspace(t *testing.T) {
	app, root, identity := reproducerWorkspace(t)
	selected := edit(t, app, root, identity, reproducer.Plan{},
		reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID})
	prior := edit(t, app, root, identity, selected.Reproducer.Plan,
		reproducer.Step{Operator: reproducer.IncludePriorIdentity, Identity: []string{"SCH-2.1", "SCH-2.2"}})
	edited := edit(t, app, root, identity, prior.Reproducer.Plan,
		reproducer.Step{Operator: reproducer.SetField, Occurrence: repRescheduleID, Selector: "PID-3.1", Value: "MRN-REPRODUCER"})

	build := request(root, identity, edited.Reproducer.Plan, reproducer.Step{})
	build.Output = "incident-reproducer"
	built := app.BuildReproducer(build)
	if built.State != desktop.Completed || built.Reproducer == nil || built.Reproducer.Output != "incident-reproducer" {
		t.Fatalf("a reproducer was not written: %+v", built)
	}
	derived, err := bundle.Open(filepath.Join(root, "incident-reproducer", reproducer.CaseName))
	if err != nil {
		t.Fatal(err)
	}
	if derived.Identity != built.Reproducer.Identity || derived.Manifest.Provenance.Derivation != reproducer.Derivation {
		t.Fatal("the derived case does not declare the transformation that wrote it")
	}
	// The workspace now lists it, and the case it came from is still exactly
	// what it was.
	opened := app.OpenWorkspace(root)
	if opened.State != desktop.Completed {
		t.Fatalf("the workspace could not be reopened: %+v", opened)
	}
	listed := map[string]desktop.Artifact{}
	for _, artifact := range opened.Workspace.Artifacts {
		listed[artifact.Name] = artifact
	}
	if listed["incident-reproducer"].Kind != desktop.UnsupportedArtifact {
		t.Fatalf("a reproducer folder was listed as something this release opens: %+v", listed["incident-reproducer"])
	}
	if original := app.OpenCase(root, "incident"); original.State != desktop.Completed || original.Case.Identity != identity {
		t.Fatalf("building a reproducer changed the evidence it read: %+v", original)
	}
}

// Every refusal a build promises, asserted where a person would meet it.
func TestBuildingAReproducerRefusesWhatItCannotStandBehind(t *testing.T) {
	app, root, identity := reproducerWorkspace(t)
	selected := edit(t, app, root, identity, reproducer.Plan{},
		reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repBookingID})
	plan := selected.Reproducer.Plan
	if err := os.Mkdir(filepath.Join(root, "taken"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{
		"an entry that already exists": "taken",
		"a path rather than an entry":  "nested/reproducer",
		"a parent reference":           "..",
		"the case it reads":            "incident",
	} {
		build := request(root, identity, plan, reproducer.Step{})
		build.Output = output
		if result := app.BuildReproducer(build); result.State != desktop.Failed {
			t.Errorf("a reproducer was written to %s: %+v", name, result)
		}
	}
	// A plan that retains nothing is not evidence of anything.
	empty := request(root, identity, reproducer.Plan{}, reproducer.Step{})
	empty.Output = "nothing"
	if result := app.BuildReproducer(empty); result.State != desktop.Failed {
		t.Fatalf("an empty reproducer was written: %+v", result)
	}
	// Every operation binds to the evidence the grid displayed.
	stale := request(root, "0000000000000000000000000000000000000000000000000000000000000000", plan, reproducer.Step{})
	stale.Output = "stale"
	if result := app.BuildReproducer(stale); result.State != desktop.Failed {
		t.Fatalf("a build ran against evidence whose identity had changed: %+v", result)
	}
	if result := app.EditReproducer(stale); result.State != desktop.Failed || result.Reproducer != nil {
		t.Fatalf("an edit ran against evidence whose identity had changed: %+v", result)
	}
	// An occurrence nothing decoded is retained exactly as it is and has no
	// field to edit.
	garbage := edit(t, app, root, identity, plan, reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repGarbageID})
	refused := app.EditReproducer(request(root, identity, garbage.Reproducer.Plan,
		reproducer.Step{Operator: reproducer.SetField, Occurrence: repGarbageID, Selector: "PID-3.1", Value: "MRN"}))
	if refused.State != desktop.Failed || refused.Reproducer != nil {
		t.Fatalf("an occurrence nothing decoded was edited: %+v", refused)
	}
}

// Every reproducer operation verifies a case, so each one claims the operation
// slot rather than racing another.
func TestReproducerOperationsRunOneAtATime(t *testing.T) {
	app, root, identity := reproducerWorkspace(t)
	reentrant := &chooser{folder: root}
	second := activatedApp(t, reentrant, t.TempDir())
	var edited, undone, built desktop.ReproducerResult
	reentrant.before = func() {
		start := request(root, identity, reproducer.Plan{}, reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repBookingID})
		start.Output = "reproducer"
		edited = second.EditReproducer(start)
		undone = second.UndoReproducer(start)
		built = second.BuildReproducer(start)
	}
	if opened := second.SelectWorkspace(); opened.State != desktop.Completed {
		t.Fatalf("the first operation did not complete: %+v", opened)
	}
	for name, result := range map[string]desktop.ReproducerResult{"edit": edited, "undo": undone, "build": built} {
		if result.State != desktop.Busy || result.Reproducer != nil {
			t.Errorf("a reproducer %s ran while another operation held the facade: %+v", name, result)
		}
	}
	if recovered := app.EditReproducer(request(root, identity, reproducer.Plan{},
		reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repBookingID})); recovered.State != desktop.Completed {
		t.Fatalf("the facade stayed busy after its operation finished: %+v", recovered)
	}
}

// Every reason this window shows is a fixed sentence. These operations are the
// only ones that forward an engine diagnostic rather than a literal written in
// the facade, so the property `docs/desktop.md` states — a reason never repeats
// a path or a value — is asserted here rather than left to review.
func TestNoReproducerRefusalRepeatsAPathOrAValue(t *testing.T) {
	app, root, identity := reproducerWorkspace(t)
	const secret = "MRN-SHOULD-NEVER-APPEAR"
	const selector = "PID[1]-3[1].1"
	selected := edit(t, app, root, identity, reproducer.Plan{},
		reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repBookingID})
	plan := selected.Reproducer.Plan

	refusals := []desktop.ReproducerResult{}
	for _, step := range []reproducer.Step{
		{Operator: "exec-shell/v1", Occurrence: repBookingID},
		{Operator: reproducer.SelectOccurrence, Occurrence: repBookingID},
		{Operator: reproducer.SelectOccurrence, Occurrence: "s0009-e000001"},
		{Operator: reproducer.SetField, Occurrence: repBookingID, Selector: selector, Value: secret + "|INJECTED"},
		{Operator: reproducer.SetField, Occurrence: repGarbageID, Selector: selector, Value: secret},
		{Operator: reproducer.SetField, Occurrence: repRescheduleID, Selector: selector, Value: secret},
		{Operator: reproducer.SetField, Occurrence: repBookingID, Selector: "PID-99", Value: secret},
		{Operator: reproducer.SetField, Occurrence: repBookingID, Selector: "MSH-1", Value: secret},
		{Operator: reproducer.IncludePriorIdentity, Identity: []string{selector, selector}},
	} {
		refusals = append(refusals, app.EditReproducer(request(root, identity, plan, step)))
	}
	refusals = append(refusals, app.UndoReproducer(request(root, identity, reproducer.Plan{}, reproducer.Step{})))
	for _, output := range []string{"incident", "nested/one", "..", ""} {
		build := request(root, identity, plan, reproducer.Step{})
		build.Output = output
		refusals = append(refusals, app.BuildReproducer(build))
	}
	for _, result := range refusals {
		if result.State != desktop.Failed || result.Reason == "" {
			t.Fatalf("a refusal gave the window nothing to show: %+v", result)
		}
		for _, leaked := range []string{root, secret, selector, "PID-99", "s0009"} {
			if strings.Contains(result.Reason, leaked) {
				t.Errorf("a refusal repeated %q: %s", leaked, result.Reason)
			}
		}
	}
}

// buildRevision writes one reproducer of the open workspace and returns the
// entry it was written to.
func buildRevision(t *testing.T, app *desktop.App, root, identity, output string, steps ...reproducer.Step) string {
	t.Helper()
	plan := reproducer.Plan{}
	for _, step := range steps {
		plan = edit(t, app, root, identity, plan, step).Reproducer.Plan
	}
	request := request(root, identity, plan, reproducer.Step{})
	request.Output = output
	if result := app.BuildReproducer(request); result.State != desktop.Completed {
		t.Fatalf("a reproducer this workspace supports was not written: %+v", result)
	}
	return output
}

// Two revisions of one incident, compared in the window: how they are related,
// what the second plan no longer does, and the setup dependency that went with
// it. Nothing is written, and neither revision is changed.
func TestComparingTwoRevisionsShowsLineageAndTheDependencyThatWasDropped(t *testing.T) {
	app, root, identity := reproducerWorkspace(t)
	withDependencies := buildRevision(t, app, root, identity, "with-dependencies",
		reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID},
		reproducer.Step{Operator: reproducer.IncludePriorIdentity, Identity: []string{"SCH-2.1", "SCH-2.2"}},
	)
	alone := buildRevision(t, app, root, identity, "reschedule-alone",
		reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID},
	)
	result := app.CompareReproducers(desktop.ReproducerComparisonRequest{Workspace: root, Left: withDependencies, Right: alone})
	if result.State != desktop.Completed || result.Comparison == nil {
		t.Fatalf("two revisions of one workspace were not compared: %+v", result)
	}
	comparison := result.Comparison
	if comparison.Lineage != reproducer.SiblingOf {
		t.Fatalf("two revisions of one case were related as %q", comparison.Lineage)
	}
	if comparison.Left.Derivation != reproducer.Derivation || comparison.Left.Provenance != "derived" {
		t.Fatalf("a revision was not reported as the derived evidence it declares: %+v", comparison.Left)
	}
	dropped := map[string]reproducer.RetentionChange{}
	for _, change := range comparison.Retention {
		dropped[change.Parent] = change
	}
	if dropped[repBookingID].Change != reproducer.DroppedPrerequisite || dropped[repBookingID].LeftReason != reproducer.PriorIdentity {
		t.Fatalf("the booking the second revision stopped retaining was not reported as a dropped prerequisite: %+v", comparison.Retention)
	}
	if len(comparison.Steps) != 1 || comparison.Steps[0].Change != reproducer.Removed {
		t.Fatalf("the authored change between the two plans was not reported: %+v", comparison.Steps)
	}
	// Nobody has run either revision, so nothing is claimed about either.
	if comparison.Proof.State != reproducer.ProofNotAttempted || comparison.Proof.Left != nil {
		t.Fatalf("proof was reported for revisions nobody has run: %+v", comparison.Proof)
	}
	// One revision compared with itself has no difference to report and still
	// has an answer: it is the same evidence, and that is what is said.
	same := app.CompareReproducers(desktop.ReproducerComparisonRequest{Workspace: root, Left: alone, Right: alone})
	if same.State != desktop.Completed || same.Comparison == nil || same.Comparison.Lineage != reproducer.SameEvidence ||
		len(same.Comparison.Steps) != 0 || len(same.Comparison.Retention) != 0 {
		t.Fatalf("one revision compared with itself was reported as %+v", same)
	}
	// Neither revision and neither case was changed by comparing them.
	if original := app.OpenCase(root, "incident"); original.State != desktop.Completed || original.Case.Identity != identity {
		t.Fatalf("comparing two revisions changed the evidence they came from: %+v", original)
	}
}

// A comparison reaches nothing outside the open workspace and stands behind
// both revisions or produces nothing.
func TestComparingRevisionsRefusesWhatItCannotStandBehind(t *testing.T) {
	app, root, identity := reproducerWorkspace(t)
	revision := buildRevision(t, app, root, identity, "revision",
		reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID})
	for name, request := range map[string]desktop.ReproducerComparisonRequest{
		"a path rather than an entry":  {Workspace: root, Left: revision, Right: "nested/revision"},
		"a parent reference":           {Workspace: root, Left: revision, Right: ".."},
		"an entry that does not exist": {Workspace: root, Left: revision, Right: "absent"},
		"the case it was derived from": {Workspace: root, Left: revision, Right: "incident"},
		"a retained run outside the workspace": {
			Workspace: root, Left: revision, Right: revision,
			LeftResult: "../run", RightResult: "../run",
		},
		"a retained run that is not one": {
			Workspace: root, Left: revision, Right: revision,
			LeftResult: "incident", RightResult: "incident",
		},
		// A run named beside one revision is read and bound to it even when the
		// other names none and nothing would have been compared anyway.
		"a retained run named on one side only": {
			Workspace: root, Left: revision, Right: revision, LeftResult: "incident",
		},
	} {
		if result := app.CompareReproducers(request); result.State != desktop.Failed || result.Comparison != nil {
			t.Errorf("a comparison accepted %s: %+v", name, result)
		}
	}
	if missing := app.CompareReproducers(desktop.ReproducerComparisonRequest{Workspace: filepath.Join(root, "absent"), Left: revision, Right: revision}); missing.State == desktop.Completed {
		t.Fatalf("a comparison ran against a workspace that is not open: %+v", missing)
	}
}

// Comparing two revisions verifies both and runs to completion, so it claims
// the operation slot rather than racing another operation.
func TestComparingRevisionsRunsOneAtATime(t *testing.T) {
	app, root, identity := reproducerWorkspace(t)
	revision := buildRevision(t, app, root, identity, "revision",
		reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID})
	reentrant := &chooser{folder: root}
	second := activatedApp(t, reentrant, t.TempDir())
	var compared desktop.ReproducerComparisonResult
	reentrant.before = func() {
		compared = second.CompareReproducers(desktop.ReproducerComparisonRequest{Workspace: root, Left: revision, Right: revision})
	}
	if opened := second.SelectWorkspace(); opened.State != desktop.Completed {
		t.Fatalf("the first operation did not complete: %+v", opened)
	}
	if compared.State != desktop.Busy || compared.Comparison != nil {
		t.Fatalf("a comparison ran while another operation held the facade: %+v", compared)
	}
	if recovered := app.CompareReproducers(desktop.ReproducerComparisonRequest{Workspace: root, Left: revision, Right: revision}); recovered.State != desktop.Completed {
		t.Fatalf("the facade did not release its slot: %+v", recovered)
	}
}
