package desktop_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/transform"
)

// One booking and its reschedule, declaring one patient under one configured
// assigning authority, plus the acknowledgement of the booking. It is the
// smallest evidence a relationship-preserving rename can be previewed on: the
// patient identifier relates the two messages, and the acknowledgement refers
// to the control ID of the first one.
const (
	tfBooking     = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|TF-CTL-1|P|2.5.1\rSCH|PLACER-1^READMIT|FILLER-1^READMIT|||||||^^^20260102100000^20260102103000\rPID|1||TF-MRN-1^^^READMIT^MR||DOE^JANE\r"
	tfRescheduled = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120100||SIU^S13|TF-CTL-2|P|2.5.1\rSCH|PLACER-1^READMIT|FILLER-1^READMIT|||||||^^^20260103100000^20260103103000\rPID|1||TF-MRN-1^^^READMIT^MR||DOE^JANE\r"
	tfAccepted    = "MSH|^~\\&|RECV|LAB|READMIT|TEST|20260101120001||ACK^S12|TF-CTL-9|P|2.5.1\rMSA|AA|TF-CTL-1\r"

	// The declared rules the preview preserves: one patient relation under one
	// configured assigning authority, the control IDs of the source, and the
	// acknowledgement relation the case already carries.
	tfRules = `{"schema":"readmit-correlation-rules/v1",` +
		`"authorities":[{"key":"READMIT-MR","namespace":"READMIT","universal_id":"","universal_id_type":""}],` +
		`"rules":[{"id":"acknowledgements","operator":"acknowledges","scope":"source"},` +
		`{"id":"message","operator":"control-id","scope":"source"},` +
		`{"id":"patient","operator":"identifier","scope":"source","value":"PID-3.1",` +
		`"authority":["PID-3.4.1","PID-3.4.2","PID-3.4.3"]}]}`
)

// transformWorkspace writes the two scheduling messages, the declared rules and
// a plan bound to both into one open workspace, and returns the app that reads
// it with the identity the window verified.
func transformWorkspace(t *testing.T, steps string) (*desktop.App, string, string) {
	t.Helper()
	root := t.TempDir()
	state := t.TempDir()
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"))
	opened := writeCase(t, root, "incident", framed(tfBooking)+framed(tfRescheduled)+framed(tfAccepted))
	writeDocument(t, root, "rules.json", tfRules)
	rules, err := correlate.ParseRules([]byte(tfRules))
	if err != nil {
		t.Fatal(err)
	}
	report, err := correlate.Run(filepath.Join(root, "incident"), rules)
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, "plan.json", `{"schema":"`+transform.PlanSchema+`","case":"`+opened.Identity+
		`","rules":"`+report.RulesSHA256+`","steps":[`+steps+`]}`)
	return app, root, opened.Identity
}

// writeDocument writes one declared document into an open workspace, the way an
// operator authors a rules or a plan document beside the evidence it is about.
func writeDocument(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func transformRequest(root, identity string) desktop.TransformRequest {
	return desktop.TransformRequest{
		Workspace: root, Case: "incident", Identity: identity,
		Rules: "rules.json", Plan: "plan.json",
	}
}

// previewed runs one preview and fails the test if the facade refused it.
func previewed(t *testing.T, app *desktop.App, request desktop.TransformRequest) *desktop.Transformation {
	t.Helper()
	result := app.PreviewTransformation(request)
	if result.State != desktop.Completed || result.Transformation == nil {
		t.Fatalf("the facade refused a preview it supports: %+v", result)
	}
	return result.Transformation
}

// The whole preview delivery in one plan: the identifier one declared rule
// relates is renamed once across the relation, every supported timestamp moves
// by the same duration, a repeated occurrence is still a copy of the one it
// repeats, and the acknowledgement of a renamed message is repaired rather than
// stranded.
func TestPreviewingATransformationShowsEveryPositionItWouldRewrite(t *testing.T) {
	app, root, identity := transformWorkspace(t,
		`{"operator":"rebase-identifiers/v1","rule":"patient"},`+
			`{"operator":"shift-dates/v1","shift":"24h"},`+
			`{"operator":"duplicate-occurrence/v1","entry":"t000001"}`)
	previewing := previewed(t, app, transformRequest(root, identity))

	preview := previewing.Preview
	if preview.Schema != transform.PreviewSchema || preview.Case.Identity != identity {
		t.Fatalf("the preview did not declare its contract over the verified case: %+v", preview)
	}
	if previewing.Boundary == "" || preview.Scope == "" {
		t.Fatal("the preview reached the window without the statements a person needs before acting on it")
	}
	// A duplication adds one entry naming the same parent occurrence, and the
	// copy receives its original's renamed value: repeating a message does not
	// make it a different message.
	if preview.Summary.Entries != 4 || preview.Summary.Copies != 1 {
		t.Fatalf("the duplicated occurrence did not reach the window as a copy: %+v", preview.Summary)
	}
	groups := map[string]int{}
	for _, change := range preview.Changes {
		if change.Selector == "PID[1]-3[1].1" {
			groups[change.Entry] = change.Group
		}
	}
	if len(groups) != 3 {
		t.Fatalf("the rename did not reach every occurrence declaring the related identifier: %+v", groups)
	}
	for entry, group := range groups {
		if group != groups["t000001"] {
			t.Fatalf("entry %s received its own surrogate, so the relation was not preserved: %+v", entry, groups)
		}
	}
	// A date shift states, once, that every other date and timestamp field was
	// left exactly as it is. Nothing here can establish which other fields carry
	// a date, so the preview says so rather than leaving it to a page.
	if !reported(preview, transform.UnshiftedPositions) {
		t.Fatalf("the preview shifted dates without stating what it left alone: %+v", preview.Unsupported)
	}
	for _, relation := range preview.Relations {
		if !relation.Preserved {
			t.Fatalf("a relation was severed by a plan that drops nothing: %+v", relation)
		}
	}
}

// The one thing this panel must never do. A transformation preview is a record
// of positions, not a second copy of the evidence it transformed: no byte of any
// value crosses the facade boundary, before or after, and the surrogate a rename
// would write does not either.
func TestATransformationPreviewNeverReportsAValueItWouldRewrite(t *testing.T) {
	app, root, identity := transformWorkspace(t,
		`{"operator":"rebase-identifiers/v1","rule":"patient"},`+
			`{"operator":"shift-dates/v1","shift":"24h"}`)
	result := app.PreviewTransformation(transformRequest(root, identity))
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		"TF-MRN-1", "DOE^JANE", "TF-CTL-1", "TF-CTL-9",
		"20260102100000", "20260103100000", "READMIT000001",
	} {
		if strings.Contains(string(encoded), value) {
			t.Fatalf("the window disclosed a transformed or original value: %s", encoded)
		}
	}
}

// A plan that says nothing is not a failure and is not a transformation: it is
// the state a person is in on the way to one, so it is its own outcome.
func TestAPlanDeclaringNoStepIsEmptyRatherThanCompleted(t *testing.T) {
	app, root, identity := transformWorkspace(t, "")
	result := app.PreviewTransformation(transformRequest(root, identity))
	if result.State != desktop.Empty || result.Transformation == nil {
		t.Fatalf("a plan with no step was not reported as empty: %+v", result)
	}
	if len(result.Transformation.Preview.Sequence) != 3 {
		t.Fatalf("an empty plan did not leave the sequence the case's own: %+v", result.Transformation.Preview.Summary)
	}
}

// Every refusal the panel promises. A preview is bound to the evidence the
// window verified and to documents of the folder the person opened; a plan
// authored elsewhere is refused rather than applied to whatever is there.
func TestAPreviewIsRefusedOutsideTheEvidenceAndDocumentsItWasBoundTo(t *testing.T) {
	app, root, identity := transformWorkspace(t, `{"operator":"shift-dates/v1","shift":"24h"}`)
	outside := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(outside, []byte(tfRules), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string]desktop.TransformRequest{
		"a case the window did not verify": {
			Workspace: root, Case: "incident", Identity: "", Rules: "rules.json", Plan: "plan.json",
		},
		"evidence that changed since": {
			Workspace: root, Case: "incident", Identity: strings.Repeat("0", 64), Rules: "rules.json", Plan: "plan.json",
		},
		"rules outside the open workspace": {
			Workspace: root, Case: "incident", Identity: identity, Rules: outside, Plan: "plan.json",
		},
		"a plan that is not an entry of the workspace": {
			Workspace: root, Case: "incident", Identity: identity, Rules: "rules.json", Plan: "absent.json",
		},
		"a case that is not an entry of the workspace": {
			Workspace: root, Case: "../incident", Identity: identity, Rules: "rules.json", Plan: "plan.json",
		},
	} {
		result := app.PreviewTransformation(request)
		if result.State != desktop.Failed || result.Transformation != nil {
			t.Fatalf("%s was previewed: %+v", name, result)
		}
	}
	// A pack the plan did not pin is refused by the engine rather than used to
	// validate the transformed sequence against whatever was named here.
	declared, err := os.ReadFile("../../testdata/fixtures/profile-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, "pack.json", string(declared))
	pinned := transformRequest(root, identity)
	pinned.Profile = "pack.json"
	if result := app.PreviewTransformation(pinned); result.State != desktop.Failed {
		t.Fatalf("a pack the plan did not pin was accepted: %+v", result)
	}
}

// Renaming a control ID repairs what refers to it. The acknowledgement of a
// renamed message has its own reference rewritten, because a rename that
// stranded it would break the one relation the case carries with no
// configuration at all.
func TestRenamingAControlIDRepairsTheAcknowledgementThatNamesIt(t *testing.T) {
	app, root, identity := transformWorkspace(t, `{"operator":"rebase-identifiers/v1","rule":"message"}`)
	preview := previewed(t, app, transformRequest(root, identity)).Preview

	// The renamed message and the reference to it carry one relation number, so
	// the repair is visible as the relation it preserved rather than as a second
	// edit that happens to be beside it.
	renamed, repaired := 0, 0
	for _, change := range preview.Changes {
		switch {
		case change.Entry == "t000001" && change.Selector == "MSH[1]-10[1]":
			renamed = change.Group
		case change.Entry == "t000003" && change.Selector == "MSA[1]-2[1]":
			repaired = change.Group
		}
	}
	if renamed == 0 || renamed != repaired {
		t.Fatalf("the acknowledgement of a renamed message was left pointing at a control ID that is nowhere: %+v", preview.Changes)
	}
}

func reported(preview transform.Preview, code string) bool {
	for _, item := range preview.Unsupported {
		if item.Code == code {
			return true
		}
	}
	return false
}

// blockedReview derives one export review from the planted-identifier corpus
// under a policy that handles nothing, and returns the workspace holding it.
// Every finding stays unresolved, so no derived case is written and no fixture
// proof runs: this is exactly the incomplete review a reviewer must not be able
// to turn into an approval.
func blockedReview(t *testing.T, root string) *redact.Review {
	t.Helper()
	inputs := t.TempDir()
	request := redact.Request{
		CasePath:      filepath.Join(inputs, "original.case"),
		SpecPath:      filepath.Join(inputs, "spec.json"),
		PolicyPath:    filepath.Join(inputs, "policy.json"),
		InventoryPath: filepath.Join(inputs, "inventory.json"),
		Output:        filepath.Join(root, "review"),
		LocalState:    filepath.Join(inputs, "private"),
	}
	var sources []bundle.Input
	for _, name := range []string{"booking", "reschedule"} {
		raw, err := os.ReadFile("../../testdata/fixtures/redact-" + name + ".mllp")
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, bundle.Input{
			Path:    filepath.Join(inputs, "PLANTED-FILENAME-CEDAR-"+name+".mllp"),
			Data:    raw,
			Options: hl7.Options{Format: hl7.MLLP},
		})
	}
	imported := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	if _, err := bundle.Write(request.CasePath, sources, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported}); err != nil {
		t.Fatal(err)
	}
	for fixture, path := range map[string]string{
		"spec": request.SpecPath, "policy-blocked": request.PolicyPath, "inventory": request.InventoryPath,
	} {
		raw, err := os.ReadFile("../../testdata/fixtures/redact-" + fixture + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	created, err := redact.Create(context.Background(), request)
	if err != nil {
		t.Fatalf("the planted corpus no longer produces a review: %v", err)
	}
	if created.State != "blocked" {
		t.Fatalf("a policy that handles nothing produced a review that is not blocked: %s", created.State)
	}
	return created
}

func reviewRequest(root, approve string) desktop.ReviewRequest {
	return desktop.ReviewRequest{
		Workspace: root, Review: "review", Approve: approve,
		Offset: 0, Limit: desktop.MaxReviewFindings,
	}
}

// The inventory is the whole of what a review found, laid out by the export
// surface each item is on. Every surface the engine located is here and every
// finding belongs to exactly one of them, because a surface left out of a
// disclosure review is the one omission that matters.
func TestAReviewInventoriesEveryDeclaredExportSurfaceWithoutOmittingOne(t *testing.T) {
	root := t.TempDir()
	created := blockedReview(t, root)
	state := t.TempDir()
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"))

	result := app.OpenReview(reviewRequest(root, ""))
	if result.State != desktop.Completed || result.Review == nil {
		t.Fatalf("the facade refused a review it supports: %+v", result)
	}
	review := result.Review
	if review.Report != redact.ReviewSchema || review.Identity != created.Identity {
		t.Fatalf("the window did not report the review the reader verified: %+v", review)
	}
	counted := 0
	named := map[string]bool{}
	for _, surface := range review.Surfaces {
		counted += surface.Findings
		named[surface.Name+" "+surface.Content] = true
	}
	if counted != review.Total || review.Total != len(created.Findings) {
		t.Fatalf("the surfaces do not sum to the inventory: %d of %d", counted, review.Total)
	}
	// The messages, the source filenames and paths, the source metadata, the
	// specification and the regenerated report are each their own surface, so a
	// reviewer sees every one of them rather than one total per location.
	for _, surface := range []string{
		"case unmapped-field", "case free-text-or-embedded-payload",
		"case unknown-segment", "case source-filename", "case source-metadata",
		"spec spec-literal", "spec source-filename",
		"spec spec-name-reset-paths-and-assertion-labels",
		"packet diagnosis-regeneration-required",
	} {
		if !named[surface] {
			t.Fatalf("the %q surface was not inventoried: %+v", surface, review.Surfaces)
		}
	}
	if review.Unresolved == 0 || review.Unresolved != review.Total {
		t.Fatalf("a policy that handles nothing left resolved findings: %d of %d", review.Unresolved, review.Total)
	}
	// The eighteen checklist categories and the scan's own limitations reach the
	// window as the engine wrote them: a clean scan is one check on known values
	// and never an identification assessment.
	if len(review.Coverage) != len(exportreview.Categories) || review.Scope == "" || review.Residual.Limitations == "" {
		t.Fatalf("the coverage checklist or its boundary did not reach the window: %+v", review)
	}
	// What a review is not is stated rather than left to be assumed: it is no
	// equivalence claim, no certification and no Safe Harbor determination.
	for _, statement := range []string{"regression-equivalent", "Safe Harbor"} {
		if !strings.Contains(review.Boundary, statement) {
			t.Fatalf("the window did not state that a review is no %s claim: %q", statement, review.Boundary)
		}
	}
}

// An incomplete review cannot authorize disclosure, and naming its exact bytes
// does not change that. The identity is the reviewer's own decision, not a way
// around the gate.
func TestAnIncompleteReviewCannotBeApprovedEvenWithItsExactIdentity(t *testing.T) {
	root := t.TempDir()
	created := blockedReview(t, root)
	state := t.TempDir()
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"))

	undecided := app.OpenReview(reviewRequest(root, ""))
	if undecided.Review == nil || undecided.Review.Decision != desktop.IncompleteReview {
		t.Fatalf("a blocked review was not reported as incomplete: %+v", undecided)
	}
	if undecided.Review.Establishes != "" {
		t.Fatalf("an incomplete review established something: %q", undecided.Review.Establishes)
	}
	approved := app.OpenReview(reviewRequest(root, created.Identity))
	if approved.Review == nil || approved.Review.Decision != desktop.IncompleteReview {
		t.Fatalf("the exact identity approved an incomplete review: %+v", approved)
	}
	if !strings.Contains(approved.Review.DecisionReason, "cannot authorize disclosure") {
		t.Fatalf("the refusal did not say why: %q", approved.Review.DecisionReason)
	}
}

// The other thing this panel must never do. A review names locations, classes
// and policies; no planted value of the corpus it reviewed reaches the window,
// resolved or not, and no private mapping is read at all.
func TestAReviewNeverReportsAValueTheCorpusPlanted(t *testing.T) {
	root := t.TempDir()
	blockedReview(t, root)
	state := t.TempDir()
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"))

	encoded, err := json.Marshal(app.OpenReview(reviewRequest(root, "")))
	if err != nil {
		t.Fatal(err)
	}
	for _, planted := range []string{
		"PLANTED-PATIENT-7391", "PLANTED-NAME-ORCHID", "PLANTED-NTE-ALDER",
		"PLANTED-ERR-BIRCH", "PLANTED-ACK-WILLOW", "PLANTED-EMBEDDED-ELM",
		"PLANTED-UNKNOWN-ASH", "PLANTED-SPEC-JUNIPER", "PLANTED-RESET-SPRUCE",
	} {
		if strings.Contains(string(encoded), planted) {
			t.Fatalf("the window disclosed the planted value %s", planted)
		}
	}
}

// Changing any byte of a review changes the identity an approval must name, so
// a review that was edited after it was approved is either refused by the
// reader or is a different review than the one anybody approved.
func TestChangingAReviewInvalidatesWhatWasReadFromIt(t *testing.T) {
	root := t.TempDir()
	created := blockedReview(t, root)
	state := t.TempDir()
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"))

	document := filepath.Join(root, "review", "review.json")
	raw, err := os.ReadFile(document)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), `"state":"blocked"`, `"state":"ready-for-approval"`, 1)
	if edited == string(raw) {
		t.Fatal("the review document no longer declares the state an approval is gated on")
	}
	if err := os.WriteFile(document, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	result := app.OpenReview(reviewRequest(root, created.Identity))
	if result.State != desktop.Failed || result.Review != nil {
		t.Fatalf("an edited review was read as the review that was approved: %+v", result)
	}
	sum := sha256.Sum256([]byte(edited))
	if strings.Contains(result.Reason, hex.EncodeToString(sum[:])) {
		t.Fatalf("the refusal repeated the edited review's own bytes: %q", result.Reason)
	}
}

// Every refusal the review panel promises around the window and the entry it
// names. A window past the last finding is empty with the counts beside it,
// because how much the review found is the answer in that case.
func TestAReviewWindowIsBoundedAndNamedByOneEntryOfTheWorkspace(t *testing.T) {
	root := t.TempDir()
	blockedReview(t, root)
	state := t.TempDir()
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"))

	for name, request := range map[string]desktop.ReviewRequest{
		"a window before the first finding": {Workspace: root, Review: "review", Offset: -1, Limit: 10},
		"a window of no findings":           {Workspace: root, Review: "review", Offset: 0, Limit: 0},
		"a window past the bound":           {Workspace: root, Review: "review", Offset: 0, Limit: desktop.MaxReviewFindings + 1},
		"an entry outside the workspace":    {Workspace: root, Review: "../review", Offset: 0, Limit: 10},
		"an entry that is not a review":     {Workspace: root, Review: "absent", Offset: 0, Limit: 10},
	} {
		result := app.OpenReview(request)
		if result.State != desktop.Failed || result.Review != nil {
			t.Fatalf("%s was read: %+v", name, result)
		}
	}
	past := app.OpenReview(desktop.ReviewRequest{Workspace: root, Review: "review", Offset: 10_000, Limit: 10})
	if past.State != desktop.Empty || past.Review == nil || past.Review.Total == 0 {
		t.Fatalf("a window past the last finding lost the counts: %+v", past)
	}
}

// An interrupted review is refused rather than read as a complete one. The
// completion marker is written last, so a review missing it is output an
// interrupted attempt retained; nothing here repairs it or reads around it.
func TestAnInterruptedReviewIsRefusedRatherThanReadAsComplete(t *testing.T) {
	root := t.TempDir()
	created := blockedReview(t, root)
	state := t.TempDir()
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"))

	if err := os.Remove(filepath.Join(root, "review", "identity.sha256")); err != nil {
		t.Fatal(err)
	}
	result := app.OpenReview(reviewRequest(root, created.Identity))
	if result.State != desktop.Failed || result.Review != nil {
		t.Fatalf("an interrupted review was read as complete: %+v", result)
	}
}

// Both operations run to completion under their readers' own limits once they
// start, so each holds the one operation slot and a second request reports busy
// rather than racing the first. Neither can be cancelled part-way, because
// neither writes anything there would be to cancel.
func TestReviewingAndPreviewingHoldTheOneOperationSlot(t *testing.T) {
	root := t.TempDir()
	blockedReview(t, root)
	state := t.TempDir()
	reentrant := &chooser{folder: root}
	app := desktop.New(reentrant, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"))

	var concurrentReview desktop.ReviewResult
	var concurrentPreview desktop.TransformResult
	reentrant.before = func() {
		concurrentReview = app.OpenReview(reviewRequest(root, ""))
		concurrentPreview = app.PreviewTransformation(desktop.TransformRequest{
			Workspace: root, Case: "incident", Identity: "", Rules: "rules.json", Plan: "plan.json",
		})
	}
	if result := app.SelectWorkspace(); result.State != desktop.Completed {
		t.Fatalf("the first operation did not complete: %+v", result)
	}
	if concurrentReview.State != desktop.Busy || concurrentReview.Review != nil {
		t.Fatalf("a review ran while another operation held the facade: %+v", concurrentReview)
	}
	if concurrentPreview.State != desktop.Busy || concurrentPreview.Transformation != nil {
		t.Fatalf("a preview ran while another operation held the facade: %+v", concurrentPreview)
	}
	// Recovery: the slot is released once the first operation finishes.
	if result := app.OpenReview(reviewRequest(root, "")); result.State != desktop.Completed {
		t.Fatalf("the facade stayed busy after its operation finished: %+v", result)
	}
}
