package desktop_test

// The suite-management surface: authoring suites through the same strict
// reader the command line applies, previewing the exact expansion, preparing
// private configuration, authoring coverage over retained bytes, and
// reviewing and approving environment promotion. Every operation here shares
// the engine with `readmit suite`; the tests hold the window to that equality
// and to the refusals the contracts promise.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
)

const suiteFixture = `{"schema":"readmit-suite/v1","id":"nightly","owner":"interop","tags":["siu"],"parallelism":2,` +
	`"environments":[{"id":"east","site":"hospital-a","bindings":[{"parameter":"interface","target":"east.json"}]}],` +
	`"tables":[{"id":"patients","rows":[{"id":"one","case":"case-one"}]}],` +
	`"tests":[{"id":"booking","spec":"booking.json","owner":"scheduling","tags":["smoke"],"parameter":"interface",` +
	`"table":"patients","isolation":"shared","sequence":["s0001-e000001"]}]}`

// writeSuiteFixture builds the suite fixture inside a folder an app will open.
func writeSuiteFixture(t *testing.T, root string) {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	_, err = bundle.Write(filepath.Join(root, "case-one"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	target := replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: "127.0.0.1:1", Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "2s", MaxACKBytes: 4096}
	targetRaw, _ := json.Marshal(target)
	writeDocument(t, root, "east.json", string(targetRaw))
	value := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "booking", Input: testrunner.Input{Case: "unbound", Messages: []string{"s0001-e000001"}}, Target: "unbound", Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset fixture deliberately"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "ack", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &value}}}}}
	specRaw, _ := json.Marshal(spec)
	writeDocument(t, root, "booking.json", string(specRaw))
	// The suite file keeps non-canonical formatting, the way the command line
	// or an operator wrote it: opening must not require canonical bytes.
	writeDocument(t, root, "suite.json", suiteFixture)
}

// suiteApp opens a fresh app over a folder holding the suite fixture.
func suiteApp(t *testing.T) (*desktop.App, string) {
	t.Helper()
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	writeSuiteFixture(t, root)
	app := workspaceApp(t)
	if result := app.OpenWorkspace(root); result.State != desktop.Completed {
		t.Fatalf("open workspace: %+v", result)
	}
	return app, root
}

func TestOpenSuiteReadsCLIAuthoredDocumentWithoutDroppingClauses(t *testing.T) {
	app, root := suiteApp(t)
	result := app.OpenSuite(root, "suite.json")
	if result.State != desktop.Completed || result.Suite == nil {
		t.Fatalf("%+v", result)
	}
	document := result.Suite
	if document.ID != "nightly" || document.Owner != "interop" || len(document.Tags) != 1 || document.Parallelism != 2 ||
		len(document.Environments) != 1 || document.Environments[0].Site != "hospital-a" || len(document.Environments[0].Bindings) != 1 ||
		len(document.Tables) != 1 || len(document.Tables[0].Rows) != 1 || document.Tables[0].Rows[0].Case != "case-one" ||
		len(document.Tests) != 1 || document.Tests[0].Spec != "booking.json" || document.Tests[0].Parameter != "interface" ||
		document.Tests[0].Isolation != "shared" || len(document.Tests[0].Sequence) != 1 || document.Tests[0].Sequence[0] != "s0001-e000001" {
		t.Fatalf("clause dropped: %+v", document)
	}
	sum := sha256.Sum256([]byte(suiteFixture))
	if result.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("identity is of the bytes as they sit: %+v", result)
	}
	// An unsupported version is refused, never migrated.
	writeDocument(t, root, "next.json", `{"schema":"readmit-suite/v2","id":"n","owner":"o","tags":["t"],"parallelism":1,"environments":[],"tables":[],"tests":[]}`)
	if refused := app.OpenSuite(root, "next.json"); refused.State != desktop.Failed || refused.Suite != nil {
		t.Fatalf("%+v", refused)
	}
}

func TestSaveSuiteWritesCanonicalNewRevisionsAndRefusesInvalidDocuments(t *testing.T) {
	app, root := suiteApp(t)
	opened := app.OpenSuite(root, "suite.json")
	if opened.State != desktop.Completed {
		t.Fatalf("%+v", opened)
	}
	document := *opened.Suite
	document.Owner = "release-team"
	saved := app.SaveSuite(desktop.RuleDocumentSaveRequest{Workspace: root, Document: string(marshal(t, document)), Output: "suite-v2.json"})
	if saved.State != desktop.Completed || saved.Output != "suite-v2.json" || saved.Suite == nil || saved.Suite.Owner != "release-team" {
		t.Fatalf("%+v", saved)
	}
	if _, err := os.Stat(filepath.Join(root, "suite-v2.json")); err != nil {
		t.Fatal("revision not written")
	}
	// The original entry is untouched; a save never rewrites another revision.
	if app.OpenSuite(root, "suite.json").Suite.Owner != "interop" {
		t.Fatal("original revision was rewritten")
	}
	if again := app.SaveSuite(desktop.RuleDocumentSaveRequest{Workspace: root, Document: saved.Document, Output: "suite-v2.json"}); again.State != desktop.Failed {
		t.Fatalf("existing entry replaced: %+v", again)
	}
	// A document the suite reader refuses is not written anywhere.
	broken := document
	broken.Schema = "readmit-suite/v2"
	if refused := app.SaveSuite(desktop.RuleDocumentSaveRequest{Workspace: root, Document: string(marshal(t, broken)), Output: "broken.json"}); refused.State != desktop.Failed {
		t.Fatalf("%+v", refused)
	}
	if _, err := os.Stat(filepath.Join(root, "broken.json")); !os.IsNotExist(err) {
		t.Fatal("refused save left a file")
	}
	// The expert path: pasted canonical text decodes to the same typed document.
	validated := app.ValidateSuite(saved.Document)
	if validated.State != desktop.Completed || validated.Suite == nil || validated.Suite.Owner != "release-team" {
		t.Fatalf("%+v", validated)
	}
	if refused := app.ValidateSuite(`{"schema":"readmit-suite/v1"}`); refused.State != desktop.Failed {
		t.Fatalf("%+v", refused)
	}
}

func TestPreviewSuiteShowsTheExactExpansionAndRefusesWhatPreparationRefuses(t *testing.T) {
	app, root := suiteApp(t)
	result := app.PreviewSuite(desktop.SuitePreviewRequest{Workspace: root, Entry: "suite.json", Environment: "east"})
	if result.State != desktop.Completed || result.Expansion == nil {
		t.Fatalf("%+v", result)
	}
	expansion := result.Expansion
	if len(expansion.Jobs) != 1 || expansion.Jobs[0].ID != "booking-one" || expansion.Jobs[0].Target != filepath.Join(root, "east.json") ||
		expansion.Jobs[0].Case != filepath.Join(root, "case-one") || expansion.Jobs[0].Boundary != testrunner.ACKBoundary || expansion.Engine == "" {
		t.Fatalf("%+v", expansion)
	}
	if _, err := os.Stat(filepath.Join(root, "prepared")); !os.IsNotExist(err) {
		t.Fatal("preview wrote a directory")
	}
	// An editor-held document previews identically.
	fromEditor := app.PreviewSuite(desktop.SuitePreviewRequest{Workspace: root, Document: expansionText(t, root), Environment: "east"})
	if fromEditor.State != desktop.Completed || len(fromEditor.Expansion.Jobs) != 1 {
		t.Fatalf("%+v", fromEditor)
	}
	// Exactly one of document and entry.
	if both := app.PreviewSuite(desktop.SuitePreviewRequest{Workspace: root, Document: "{}", Entry: "suite.json", Environment: "east"}); both.State != desktop.Failed {
		t.Fatalf("%+v", both)
	}
	if neither := app.PreviewSuite(desktop.SuitePreviewRequest{Workspace: root, Environment: "east"}); neither.State != desktop.Failed {
		t.Fatalf("%+v", neither)
	}
	if absent := app.PreviewSuite(desktop.SuitePreviewRequest{Workspace: root, Entry: "suite.json", Environment: "west"}); absent.State != desktop.Failed {
		t.Fatalf("%+v", absent)
	}
}

func TestPrepareSuiteRetainsWhatTheCommandLineWrites(t *testing.T) {
	app, root := suiteApp(t)
	result := app.PrepareSuite(desktop.SuitePrepareRequest{Workspace: root, Entry: "suite.json", Environment: "east", Output: "prepared"})
	if result.State != desktop.Completed || result.Queue == nil || result.Directory != "prepared" {
		t.Fatalf("%+v", result)
	}
	if len(result.Queue.Jobs) != 1 || result.Queue.Jobs[0].ID != "booking-one" {
		t.Fatalf("%+v", result.Queue)
	}
	// The same files the command line writes, readable by its own readers.
	if _, err := suite.Decode([]byte(read(t, filepath.Join(root, "prepared", "suite.json")))); err != nil {
		t.Fatal(err)
	}
	if _, err := runqueue.DecodePlan([]byte(read(t, filepath.Join(root, "prepared", "queue.json")))); err != nil {
		t.Fatal(err)
	}
	if _, err := testrunner.ReadSpec(filepath.Join(root, "prepared", "booking-one.json")); err != nil {
		t.Fatal(err)
	}
	// Existing output is refused, never resumed.
	if again := app.PrepareSuite(desktop.SuitePrepareRequest{Workspace: root, Entry: "suite.json", Environment: "east", Output: "prepared"}); again.State != desktop.Failed {
		t.Fatalf("%+v", again)
	}
	// A refused preparation leaves no directory.
	if refused := app.PrepareSuite(desktop.SuitePrepareRequest{Workspace: root, Entry: "suite.json", Environment: "absent", Output: "refused"}); refused.State != desktop.Failed {
		t.Fatalf("%+v", refused)
	}
	if _, err := os.Stat(filepath.Join(root, "refused")); !os.IsNotExist(err) {
		t.Fatal("refused preparation left a directory")
	}
}

func TestCoverageAuthoringPinsRetainedBytesAndAssessmentMatchesTheEngine(t *testing.T) {
	app, root := suiteApp(t)
	prepared := app.PrepareSuite(desktop.SuitePrepareRequest{Workspace: root, Entry: "suite.json", Environment: "east", Output: "prepared"})
	if prepared.State != desktop.Completed {
		t.Fatalf("%+v", prepared)
	}
	authored := app.SaveSuiteCoverage(desktop.SuiteCoverageSaveRequest{
		Workspace: root, Prepared: "prepared",
		Requirements: []suite.Requirement{{ID: "accept-booking", Jobs: []string{"booking-one"}}, {ID: "downstream", Jobs: []string{}}},
		Exclusions:   []suite.Exclusion{{Job: "booking-one", State: "quarantined", Reason: "fixture under investigation", Expires: "2026-10-01T00:00:00Z"}},
		Output:       "coverage.json",
	})
	if authored.State != desktop.Completed || authored.Document == "" {
		t.Fatalf("%+v", authored)
	}
	// The authored document is the same one the engine assesses with.
	path := filepath.Join(root, "coverage.json")
	report := app.AssessSuiteCoverage(desktop.SuiteCoverageAssessRequest{Workspace: root, Prepared: "prepared", Requirements: "coverage.json", At: "2026-09-19T00:00:00Z"})
	if report.State != desktop.Completed || report.Report == nil {
		t.Fatalf("%+v", report)
	}
	engine, err := suite.AssessCoverage(t.Context(), filepath.Join(root, "prepared"), path, nil, time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if report.Report.Denominator != engine.Denominator || report.Report.Passed != engine.Passed || len(report.Report.Jobs) != len(engine.Jobs) {
		t.Fatalf("window and engine disagree: %+v %+v", report.Report, engine)
	}
	// Before expiry the exclusion is visible and still prevents a pass; at or
	// after expiry it is marked expired, stays visible and still prevents one.
	if report.Report.Jobs[0].Execution != "unknown" || report.Report.Jobs[0].Expired || report.Report.Jobs[0].Exclusion != "quarantined" || report.Report.Passed != 0 {
		t.Fatalf("unexecuted job must stay unknown behind its quarantine: %+v", report.Report.Jobs[0])
	}
	expired := app.AssessSuiteCoverage(desktop.SuiteCoverageAssessRequest{Workspace: root, Prepared: "prepared", Requirements: "coverage.json", At: "2026-10-02T00:00:00Z"})
	if expired.State != desktop.Completed || !expired.Report.Jobs[0].Expired || expired.Report.Passed != 0 {
		t.Fatalf("expiry must stay visible without enabling a pass: %+v", expired.Report.Jobs[0])
	}
	// A declaration naming a job the suite did not expand is refused and written nowhere.
	if refused := app.SaveSuiteCoverage(desktop.SuiteCoverageSaveRequest{Workspace: root, Prepared: "prepared", Requirements: []suite.Requirement{{ID: "absent", Jobs: []string{"nope-one"}}}, Output: "refused.json"}); refused.State != desktop.Failed {
		t.Fatalf("%+v", refused)
	}
	if _, err := os.Stat(filepath.Join(root, "refused.json")); !os.IsNotExist(err) {
		t.Fatal("refused coverage authoring left a file")
	}
}

func TestPromotionReviewAndApprovalInvalidateOnChangedConfiguration(t *testing.T) {
	app, root := releaseWorkspace(t)
	review := app.ReviewSuitePromotion(desktop.SuitePromotionRequest{Workspace: root, Entry: "suite.json", Environment: "east", Releases: "releases.json", Revision: "fixture-build-7"})
	if review.State != desktop.Completed || review.Review == nil {
		t.Fatalf("%+v", review)
	}
	if review.Review.Environment != "east" || review.Review.Revision != "fixture-build-7" || len(review.Review.Jobs) != 1 || review.Review.Jobs[0].Job != "booking-one" {
		t.Fatalf("%+v", review.Review)
	}
	approved := app.ApproveSuitePromotion(desktop.SuitePromotionApproveRequest{
		Workspace: root, Entry: "suite.json", Environment: "east", Releases: "releases.json", Revision: "fixture-build-7",
		Reviewed: review.Review.Identity(), Approver: "Local reviewer", Rationale: "Reviewed dev mapping and isolation", Output: "dev-promotion.json",
	})
	if approved.State != desktop.Completed || approved.Identity == "" || approved.Output != "dev-promotion.json" {
		t.Fatalf("%+v", approved)
	}
	retained, err := suite.DecodePromotion([]byte(read(t, filepath.Join(root, "dev-promotion.json"))))
	if err != nil || retained.Identity() != approved.Identity {
		t.Fatalf("%+v %v", retained, err)
	}
	// A changed revision assumption invalidates the review: approval re-reads
	// every input and refuses stale commitments.
	stale := app.ApproveSuitePromotion(desktop.SuitePromotionApproveRequest{
		Workspace: root, Entry: "suite.json", Environment: "east", Releases: "releases.json", Revision: "fixture-build-8",
		Reviewed: review.Review.Identity(), Approver: "Local reviewer", Rationale: "changed revision", Output: "stale.json",
	})
	if stale.State != desktop.Failed {
		t.Fatalf("stale review accepted: %+v", stale)
	}
	if _, err := os.Stat(filepath.Join(root, "stale.json")); !os.IsNotExist(err) {
		t.Fatal("stale approval wrote a file")
	}
}

// releaseWorkspace extends the suite fixture with a released template and its
// sidecar, exactly as the expectation workflow writes them.
func releaseWorkspace(t *testing.T) (*desktop.App, string) {
	t.Helper()
	app, root := suiteApp(t)
	raw := []byte(read(t, filepath.Join(root, "booking.json")))
	commitment, err := expectation.Review("booking", raw, []profileversion.Version{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	released, err := expectation.Approve("booking", raw, []profileversion.Version{}, nil, commitment.Identity, "reviewer", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err = expectation.Save(filepath.Join(root, "release-1.json"), released); err != nil {
		t.Fatal(err)
	}
	refs := suite.ReleaseReferences{Schema: suite.ReleasesSchema, Tests: []suite.ReleaseReference{{Test: "booking", Release: "release-1.json", Identity: released.Identity()}}}
	writeDocument(t, root, "releases.json", string(marshal(t, refs)))
	return app, root
}

func TestSaveSuiteReleasesAndExpectationImpactMatchTheEngine(t *testing.T) {
	app, root := releaseWorkspace(t)
	// The sidecar editor writes the same document the engine reads.
	saved := app.SaveSuiteReleases(desktop.RuleDocumentSaveRequest{Workspace: root, Document: `{"schema":"readmit-suite-releases/v1","tests":[{"test":"booking","release":"release-1.json","identity":"` + identityOf(t, root, "release-1.json") + `"}]}`, Output: "sidecar.json"})
	if saved.State != desktop.Completed || saved.References == nil || len(saved.References.Tests) != 1 {
		t.Fatalf("%+v", saved)
	}
	if _, err := suite.DecodeReleases([]byte(read(t, filepath.Join(root, "sidecar.json")))); err != nil {
		t.Fatal(err)
	}
	// A successor release drives the impact report.
	raw := []byte(read(t, filepath.Join(root, "booking.json")))
	first, err := expectation.Read(filepath.Join(root, "release-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte(replaceOnce(t, string(raw), `"AA"`, `"AE"`))
	commitment, err := expectation.Review("booking", changed, []profileversion.Version{}, &first, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := expectation.Approve("booking", changed, []profileversion.Version{}, &first, commitment.Identity, "reviewer", "changed")
	if err != nil {
		t.Fatal(err)
	}
	if err = expectation.Save(filepath.Join(root, "release-2.json"), second); err != nil {
		t.Fatal(err)
	}
	impact := app.ExpectationImpact(desktop.SuiteImpactRequest{Workspace: root, Suite: "suite.json", Releases: "releases.json", From: "release-1.json", To: "release-2.json"})
	if impact.State != desktop.Completed || impact.Impact == nil {
		t.Fatalf("%+v", impact)
	}
	if len(impact.Impact.Tests) != 1 || impact.Impact.Tests[0].State != "affected" || impact.Impact.Tests[0].Rows != 1 {
		t.Fatalf("%+v", impact.Impact)
	}
	engine, err := suite.AssessReleases(filepath.Join(root, "suite.json"), filepath.Join(root, "releases.json"), first, second, false)
	if err != nil || engine.From != impact.Impact.From || engine.To != impact.Impact.To || len(engine.Tests) != len(impact.Impact.Tests) {
		t.Fatalf("window and engine disagree: %+v %+v", impact.Impact, engine)
	}
	// The same successor read out of order is refused.
	if refused := app.ExpectationImpact(desktop.SuiteImpactRequest{Workspace: root, Suite: "suite.json", Releases: "releases.json", From: "release-2.json", To: "release-1.json"}); refused.State != desktop.Failed {
		t.Fatalf("%+v", refused)
	}
}

func marshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func identityOf(t *testing.T, root, entry string) string {
	t.Helper()
	release, err := expectation.Read(filepath.Join(root, entry))
	if err != nil {
		t.Fatal(err)
	}
	return release.Identity()
}

func replaceOnce(t *testing.T, text, from, to string) string {
	t.Helper()
	index := indexOf(text, from)
	if index < 0 {
		t.Fatal("fixture string not found")
	}
	return text[:index] + to + text[index+len(from):]
}

func indexOf(text, part string) int {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}

// expansionText re-reads the suite entry as the editor would hold it.
func expansionText(t *testing.T, root string) string {
	t.Helper()
	return string(read(t, filepath.Join(root, "suite.json")))
}
