package desktop_test

// A suite is one named object of a project: each Save publishes one version,
// whose definition is the draft its editor held and whose suite document is
// the readmit-suite/v1 document `readmit suite` reads, compiled against the
// project's saved tests, cases and environments. These drive the facade the
// Suites screens call — saving, reopening, validating, versions, coverage,
// approvals, import, export, run configuration and runs — against the same
// suite readers, preparation, coverage assessment and release operations the
// command line uses.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/hubprotocol"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testlicense"
	"github.com/bharm16/readmit/internal/testrunner"
)

// suiteProject is a project holding one case, two saved environments and
// three saved tests of it: two decided at the acknowledgement and one that
// reads appointment records.
type suiteProject struct {
	app                         *desktop.App
	dialog                      *chooser
	context                     desktop.RequestContext
	identity                    string
	incident                    desktop.ItemRef
	lab, backup                 desktop.ItemRef
	booking, reschedule, ledger desktop.ItemRef
}

// newSuiteProject creates the project on a fresh app whose dialogs the test
// answers; every environment's target is the laboratory at address.
func newSuiteProject(t *testing.T, address string) *suiteProject {
	t.Helper()
	dialog := &chooser{folder: t.TempDir()}
	return suiteProjectOn(t, newApp(t, dialog), dialog, address)
}

// suiteProjectOn creates the project on app, in the folder its dialog
// answers.
func suiteProjectOn(t *testing.T, app *desktop.App, dialog *chooser, address string) *suiteProject {
	t.Helper()
	chosen := app.ChooseProjectLocation()
	created := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Scheduling QA", Location: chosen.Location})
	if created.State != desktop.Completed {
		t.Fatalf("create: %+v", created)
	}
	p := &suiteProject{app: app, dialog: dialog, context: created.Context}
	root := p.context.Project
	p.identity = writeCase(t, root, "incident", framed(fixture(t, "listen-s12.hl7"))+framed(fixture(t, "listen-s13.hl7"))).Identity
	p.incident = desktop.ItemRef{Kind: desktop.CaseItem, ID: listed(t, app, root, desktop.CaseItem)["@incident"].Ref.ID}
	p.lab = saveEnvironment(t, app, p.context, desktop.SaveItemRequest{IntentID: "lab", Draft: desktop.ItemDraft{Name: "Scheduling lab", Environment: targetDraft(address)}})
	p.backup = saveEnvironment(t, app, p.context, desktop.SaveItemRequest{IntentID: "backup", Draft: desktop.ItemDraft{Name: "Backup lab", Environment: targetDraft(address)}})
	p.booking = p.saveTest(t, "booking", "", "Booking receives ACK", suiteCheck("accepted", "MSA-1", "AA"))
	p.reschedule = p.saveTest(t, "reschedule", "", "Reschedule keeps one appointment", suiteCheck("accepted", "MSA-1", "AA"), suiteCheck("control", "MSA-2", "LISTEN-BOOK"))
	one := 1
	records := p.draft(t, "Booking writes one appointment")
	records.Boundary, records.Observation = testrunner.LedgerBoundary, "appointments.json"
	records.Expectations = []testauthor.Expectation{{ID: "one-appointment", Operator: testauthor.LedgerCount, Count: &one}}
	p.ledger = saveTest(t, app, desktop.SaveItemRequest{Context: p.context, IntentID: "ledger", Draft: desktop.ItemDraft{Test: records, TestLinks: &desktop.TestLinks{Environment: p.lab.ID}}})
	return p
}

// draft is an acknowledgement test of the incident's first message.
func (p *suiteProject) draft(t *testing.T, name string) *testauthor.Draft {
	t.Helper()
	draft, err := testauthor.NewDraft("incident", p.identity)
	if err != nil {
		t.Fatal(err)
	}
	draft.Name, draft.Messages, draft.Boundary = name, []string{"s0001-e000001"}, testrunner.ACKBoundary
	draft.Reset = "Restart the listener before running this."
	return &draft
}

// saveTest saves a version of an acknowledgement test with checks: a new
// test when item is empty.
func (p *suiteProject) saveTest(t *testing.T, intent, item, name string, checks ...testauthor.Expectation) desktop.ItemRef {
	t.Helper()
	draft := p.draft(t, name)
	draft.Expectations = checks
	request := desktop.SaveItemRequest{Context: p.context, IntentID: intent, Item: item, Draft: desktop.ItemDraft{Test: draft, TestLinks: &desktop.TestLinks{Environment: p.lab.ID}}}
	if item != "" {
		request.BaseRevision = listed(t, p.app, p.context.Project, desktop.TestItem)[name].Ref.Revision
	}
	return saveTest(t, p.app, request)
}

// named is an environment as a suite binds it: by identity, following it to
// its current version.
func named(ref desktop.ItemRef) desktop.ItemRef { return desktop.ItemRef{Kind: ref.Kind, ID: ref.ID} }

func suiteCheck(id, selector, text string) testauthor.Expectation {
	return testauthor.Expectation{ID: id, Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: selector, Field: ackText(text)}
}

// smokeSuite is a whole suite over the project: two tests, each pinned to
// its first version, over two datasets, one row overriding a check and one
// not, bound in two environments with two bindings each, with requirements
// — one explicitly uncovered — and an exclusion.
func (p *suiteProject) smokeSuite() desktop.SuiteDraft {
	sequence := []string{"s0001-e000001"}
	return desktop.SuiteDraft{Tags: []string{"smoke"}, Concurrency: 2,
		Tests: []desktop.SuiteTestDraft{
			{ID: "booking", Test: p.booking, Dataset: "scheduling", Parameter: "scheduling", After: []string{}, Isolation: runqueue.SharedState, Sequence: sequence, Tags: []string{}},
			{ID: "reschedule", Test: p.reschedule, Dataset: "registration", Parameter: "registration", After: []string{"booking"}, Isolation: runqueue.IsolatedState,
				Sequence: sequence, Owner: "registration-team", Tags: []string{"reschedule"}},
		},
		Datasets: []desktop.SuiteDataset{
			{ID: "scheduling", Name: "Scheduling cases", Rows: []desktop.SuiteDataRow{
				{ID: "first", Case: p.incident, Expected: map[string]testrunner.Value{"accepted": {Field: ackText("AE")}}},
				{ID: "second", Case: p.incident},
			}},
			{ID: "registration", Name: "Registration cases", Rows: []desktop.SuiteDataRow{{ID: "only", Case: p.incident}}},
		},
		Environments: []desktop.SuiteEnvironment{
			{ID: "lab", Name: "Scheduling lab", Site: "hospital-a", Bindings: []desktop.SuiteBinding{
				{Parameter: "scheduling", Target: named(p.lab)}, {Parameter: "registration", Target: named(p.backup)}}},
			{ID: "backup", Name: "Backup lab", Site: "hospital-b", Bindings: []desktop.SuiteBinding{
				{Parameter: "scheduling", Target: named(p.backup)}, {Parameter: "registration", Target: named(p.lab)}}},
		},
		Requirements: []desktop.SuiteRequirement{
			{ID: "books", Name: "Booking is accepted", Tests: []string{"booking"}},
			{ID: "downstream", Name: "Downstream records", Tests: []string{}},
		},
		Exclusions: []desktop.SuiteExclusion{{Test: "reschedule", State: "quarantined", Reason: "fixture under investigation", Until: "2026-12-01T00:00:00Z"}},
	}
}

// saveSuite saves one version of a suite and answers its reference.
func (p *suiteProject) saveSuite(t *testing.T, intent, item, base, name string, draft desktop.SuiteDraft) desktop.ItemRef {
	t.Helper()
	saved := p.app.SaveItem(desktop.SaveItemRequest{Context: p.context, Kind: desktop.SuiteItem, Item: item, BaseRevision: base, IntentID: intent,
		Draft: desktop.ItemDraft{Name: name, Suite: &draft}})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("save %s: %+v", intent, saved)
	}
	return *saved.Saved
}

// A whole suite — two environments of two bindings each, two datasets, a row
// overriding a check and a row not — saves as one version and reopens with
// nothing dropped; editing one override and one binding saves the next
// version beside it, and the earlier one reopens read-only exactly as it was.
// The compiled suite is the document `readmit suite prepare` prepares.
func TestASuiteSavesAndReopensWholeAcrossVersions(t *testing.T) {
	p := newSuiteProject(t, "127.0.0.1:2575")
	root := p.context.Project
	draft := p.smokeSuite()
	ref := p.saveSuite(t, "create", "", "", "Scheduling smoke", draft)
	if ref.Revision != "1" {
		t.Fatalf("the first version: %+v", ref)
	}
	want := draft
	want.ID = "scheduling-smoke"
	opened := p.app.OpenItemDraft(desktop.ItemRequest{Context: p.context, Ref: ref})
	if opened.State != desktop.Completed || opened.Draft == nil || opened.Draft.Suite == nil || opened.Suite == nil || opened.Draft.Name != "Scheduling smoke" {
		t.Fatalf("reopen: %+v", opened)
	}
	if !reflect.DeepEqual(*opened.Draft.Suite, want) {
		t.Fatalf("the suite reopened as\n%+v\nnot\n%+v", *opened.Draft.Suite, want)
	}
	if shown := opened.Suite; shown.ReadOnly || shown.Original || !shown.Runnable || len(shown.Tests) != 2 || shown.Tests[0].Name != "Booking receives ACK" ||
		!slices.Equal(shown.Tests[1].Sequence, []string{"s0001-e000001"}) || len(shown.Tests[1].Checks) != 2 || len(shown.Tests[0].Messages) != 1 ||
		!strings.Contains(shown.Document, suite.Schema) {
		t.Fatalf("the suite's context: %+v", shown)
	}
	// The tests the editor reads beside the draft are the versions it pins.
	if pinned := p.app.SuiteTests(desktop.SuiteTestsRequest{Context: p.context, Tests: []desktop.ItemRef{p.reschedule, p.ledger}}); pinned.State != desktop.Completed ||
		!reflect.DeepEqual(pinned.Tests[0], opened.Suite.Tests[1]) || !pinned.Tests[1].Ledger || pinned.Tests[1].Checks[0].Operator != testauthor.LedgerCount {
		t.Fatalf("the pinned tests: %+v", pinned)
	}
	summary := listed(t, p.app, root, desktop.SuiteItem)["Scheduling smoke"].Summary.Suite
	if summary == nil || summary.Tests != 2 || !slices.Equal(summary.Environments, []string{"Scheduling lab", "Backup lab"}) || !summary.Runnable || summary.Entry == "" {
		t.Fatalf("the listed suite: %+v", summary)
	}
	// The compiled member is the suite document the command line reads and
	// prepares, every reference an entry of the project.
	compiled, err := suite.Decode([]byte(read(t, filepath.Join(root, summary.Entry))))
	if err != nil || compiled.ID != "scheduling-smoke" || compiled.Owner != "unassigned" || compiled.Tests[1].Owner != "registration-team" ||
		compiled.Tests[0].Spec != memberEntry(t, root, p.booking, "test") || compiled.Environments[0].Bindings[1].Target != memberEntry(t, root, p.backup, "target") {
		t.Fatalf("the compiled suite: %+v %v", compiled, err)
	}
	if _, stderr, err := commandLine(t, "--operation-policy", testlicense.New(t), "suite", "prepare", filepath.Join(root, summary.Entry),
		"--environment", "backup", "--output", filepath.Join(t.TempDir(), "prepared")); err != nil {
		t.Fatalf("the command line refused the compiled suite: %v %s", err, stderr)
	}

	edited := *opened.Draft.Suite
	edited.Datasets[0].Rows[1].Expected = map[string]testrunner.Value{"accepted": {Field: &testrunner.FieldValue{State: hl7.Empty}}}
	edited.Environments[1].Bindings[1].Target = named(p.backup)
	second := p.saveSuite(t, "edit", ref.ID, "1", "Scheduling smoke", edited)
	reopened := p.app.OpenItemDraft(desktop.ItemRequest{Context: p.context, Ref: desktop.ItemRef{Kind: desktop.SuiteItem, ID: ref.ID}})
	if second.Revision != "2" || reopened.Draft == nil || !reflect.DeepEqual(*reopened.Draft.Suite, edited) || reopened.Suite.ReadOnly {
		t.Fatalf("the edited version: %+v", reopened)
	}
	if rows := reopened.Draft.Suite.Datasets[0].Rows; rows[0].Expected["accepted"].Field.State != hl7.Present || rows[1].Expected["accepted"].Field.State != hl7.Empty {
		t.Fatalf("an override was dropped: %+v", rows)
	}
	first := p.app.OpenItemDraft(desktop.ItemRequest{Context: p.context, Ref: ref})
	if first.Draft == nil || !reflect.DeepEqual(*first.Draft.Suite, want) || !first.Suite.ReadOnly || first.Ref.Revision != "1" {
		t.Fatalf("the first version: %+v", first)
	}
	// A save from the first version is a conflict, never a silent overwrite.
	if stale := p.app.SaveItem(desktop.SaveItemRequest{Context: p.context, Kind: desktop.SuiteItem, Item: ref.ID, BaseRevision: "1", IntentID: "stale",
		Draft: desktop.ItemDraft{Name: "Scheduling smoke", Suite: &want}}); stale.Outcome != desktop.ConflictOutcome {
		t.Fatalf("a stale save: %+v", stale)
	}

	// A suite with no test yet saves, and cannot run.
	empty := p.saveSuite(t, "empty", "", "", "Registration smoke", desktop.SuiteDraft{Concurrency: 1})
	listing := listed(t, p.app, root, desktop.SuiteItem)["Registration smoke"].Summary.Suite
	if listing.Runnable || listing.Entry != "" || listing.Tests != 0 {
		t.Fatalf("a suite with no test: %+v", listing)
	}
	emptyOpened := p.app.OpenItemDraft(desktop.ItemRequest{Context: p.context, Ref: empty})
	if emptyOpened.Suite == nil || emptyOpened.Suite.Runnable || emptyOpened.Suite.Document != "" {
		t.Fatalf("a suite with no test reopened as %+v", emptyOpened)
	}
	export := p.app.ExportSuiteItem(desktop.SuiteExportRequest{Context: p.context, Suite: empty})
	if export.State != desktop.Failed || export.Reason != "Add tests and an environment to run this suite" {
		t.Fatalf("a suite with no test exported: %+v", export)
	}
}

// Every problem a suite draft has is answered at the member it is about, and
// nothing is saved: a dependency cycle at each test in it, an unsupported
// isolation, a send order of its own, a partial row, a check no test over
// the dataset declares, an override of the wrong type, an environment that
// leaves a parameter unbound, a binding with no observation for a test of
// appointment records, and an exclusion until a time that is not UTC.
func TestEveryProblemOfASuiteIsTargetedAtItsMember(t *testing.T) {
	p := newSuiteProject(t, "127.0.0.1:2575")
	for _, lane := range []struct {
		name   string
		change func(*desktop.SuiteDraft)
		fields []string
	}{
		{"a cycle", func(d *desktop.SuiteDraft) { d.Tests[0].After = []string{"reschedule"} }, []string{"tests.0.after", "tests.1.after"}},
		{"an isolation", func(d *desktop.SuiteDraft) { d.Tests[1].Isolation = "parallel" }, []string{"tests.1.isolation"}},
		{"a send order", func(d *desktop.SuiteDraft) { d.Tests[0].Sequence = []string{"s0001-e000002"} }, []string{"tests.0.sequence"}},
		{"a partial row", func(d *desktop.SuiteDraft) {
			d.Datasets[0].Rows = append(d.Datasets[0].Rows, desktop.SuiteDataRow{ID: "third", Case: desktop.ItemRef{Kind: desktop.CaseItem}})
		}, []string{"datasets.0.rows.2.case"}},
		{"an unknown check", func(d *desktop.SuiteDraft) {
			d.Datasets[0].Rows[1].Expected = map[string]testrunner.Value{"control": {Field: ackText("LISTEN-BOOK")}}
		}, []string{"datasets.0.rows.1.expected.control"}},
		{"a mismatched value", func(d *desktop.SuiteDraft) {
			count := 1
			d.Datasets[0].Rows[0].Expected = map[string]testrunner.Value{"accepted": {Count: &count}}
		}, []string{"datasets.0.rows.0.expected.accepted"}},
		{"a missing binding", func(d *desktop.SuiteDraft) { d.Environments[0].Bindings = d.Environments[0].Bindings[:1] }, []string{"environments.0.bindings"}},
		{"a missing observation", func(d *desktop.SuiteDraft) {
			d.Tests = append(d.Tests, desktop.SuiteTestDraft{ID: "records", Test: p.ledger, Dataset: "registration", Parameter: "records",
				Isolation: runqueue.SharedState, Sequence: []string{"s0001-e000001"}})
			for n := range d.Environments {
				d.Environments[n].Bindings = append(d.Environments[n].Bindings, desktop.SuiteBinding{Parameter: "records", Target: named(p.lab)})
			}
		}, []string{"environments.0.bindings.2.observation", "environments.1.bindings.2.observation"}},
		{"a local time", func(d *desktop.SuiteDraft) { d.Exclusions[0].Until = "2026-12-01T00:00:00+02:00" }, []string{"exclusions.0.until"}},
	} {
		t.Run(lane.name, func(t *testing.T) {
			draft := p.smokeSuite()
			lane.change(&draft)
			validated := p.app.ValidateDraft(desktop.DraftRequest{Context: p.context, Kind: desktop.SuiteItem, Draft: desktop.ItemDraft{Name: "Scheduling smoke", Suite: &draft}})
			if validated.State != desktop.Completed || !slices.Equal(fields(validated.Problems), lane.fields) {
				t.Fatalf("problems at %v, want %v: %+v", fields(validated.Problems), lane.fields, validated.Problems)
			}
			saved := p.app.SaveItem(desktop.SaveItemRequest{Context: p.context, Kind: desktop.SuiteItem, IntentID: "save-" + strings.ReplaceAll(lane.name, " ", "-"),
				Draft: desktop.ItemDraft{Name: "Scheduling smoke", Suite: &draft}})
			if saved.Outcome != desktop.InvalidOutcome || saved.Saved != nil {
				t.Fatalf("an invalid suite saved: %+v", saved)
			}
		})
	}
	// The send order problem says where the order comes from.
	draft := p.smokeSuite()
	draft.Tests[0].Sequence = []string{"s0001-e000002"}
	validated := p.app.ValidateDraft(desktop.DraftRequest{Context: p.context, Kind: desktop.SuiteItem, Draft: desktop.ItemDraft{Name: "Scheduling smoke", Suite: &draft}})
	if len(validated.Problems) != 1 || !strings.Contains(validated.Problems[0].Problem, "comes from the test") {
		t.Fatalf("the send order problem: %+v", validated.Problems)
	}
	if listed(t, p.app, p.context.Project, desktop.SuiteItem)["Scheduling smoke"].Ref.ID != "" {
		t.Fatal("an invalid suite was saved")
	}
}

// originalSuite is a readmit-suite/v1 file as the command line wrote it into
// the project: its references are the entries of saved test and environment
// versions, and its formatting is not canonical.
func (p *suiteProject) originalSuite(t *testing.T) string {
	t.Helper()
	root := p.context.Project
	return `{"schema":"readmit-suite/v1", "id":"nightly","owner":"interop","tags":["siu"],"parallelism":2,` +
		`"environments":[{"id":"east","site":"hospital-a","bindings":[{"parameter":"interface","target":"` + memberEntry(t, root, p.lab, "target") + `"}]}],` +
		`"tables":[{"id":"patients","rows":[{"id":"one","case":"incident","expected":{"accepted":{"field":{"state":"present","text":"AE"}}}}]}],` +
		`"tests":[{"id":"booking","spec":"` + memberEntry(t, root, p.booking, "test") + `","owner":"scheduling","tags":["smoke"],"parameter":"interface",` +
		`"table":"patients","isolation":"shared","sequence":["s0001-e000001"]}]}`
}

// A suite file the project already held opens as it is, read-only and with
// no clause dropped, as its original version, which has no number. Its first
// Save publishes the first managed version of the same suite, and the file
// is never rewritten.
func TestAnOriginalSuiteOpensAsItIsAndItsFirstSavePublishesVersionOne(t *testing.T) {
	p := newSuiteProject(t, "127.0.0.1:2575")
	root := p.context.Project
	original := p.originalSuite(t)
	writeDocument(t, root, "nightly.json", original)
	held := listed(t, p.app, root, desktop.SuiteItem)["nightly"]
	if held.Summary.Suite == nil || !held.Summary.Suite.Runnable || held.Summary.Suite.Entry != "nightly.json" || !slices.Equal(held.Summary.Suite.Environments, []string{"east"}) {
		t.Fatalf("the original suite: %+v", held)
	}
	opened := p.app.OpenItemDraft(desktop.ItemRequest{Context: p.context, Ref: held.Ref})
	if opened.State != desktop.Completed || opened.Suite == nil || !opened.Suite.Original || !opened.Suite.ReadOnly || opened.Suite.Document != original || len(opened.Problems) != 0 {
		t.Fatalf("open the original: %+v", opened)
	}
	draft := *opened.Draft.Suite
	if draft.ID != "nightly" || draft.Owner != "interop" || draft.Concurrency != 2 || len(draft.Tests) != 1 || draft.Tests[0].Test != p.booking ||
		draft.Tests[0].Owner != "scheduling" || draft.Datasets[0].Name != "patients" || draft.Datasets[0].Rows[0].Case != p.incident ||
		draft.Datasets[0].Rows[0].Expected["accepted"].Field == nil || draft.Environments[0].Name != "east" ||
		draft.Environments[0].Bindings[0].Target != (desktop.ItemRef{Kind: desktop.EnvironmentItem, ID: p.lab.ID}) {
		t.Fatalf("the original's draft dropped or invented a clause: %+v", draft)
	}
	if refused := p.app.PrepareAction(desktop.PrepareActionRequest{Context: p.context, Action: desktop.ApproveSuiteBaselineAction, Items: []desktop.ItemRef{held.Ref}}); refused.State != desktop.Failed || refused.Reason != "Save this suite to approve it." {
		t.Fatalf("an original was reviewed for approval: %+v", refused)
	}
	first := p.saveSuite(t, "first-save", held.Ref.ID, "", "Nightly", draft)
	if first.ID != held.Ref.ID || first.Revision != "1" || read(t, filepath.Join(root, "nightly.json")) != original {
		t.Fatalf("the first save: %+v", first)
	}
	history := p.app.SuiteHistory(desktop.ItemRequest{Context: p.context, Ref: first})
	if history.State != desktop.Completed || len(history.Versions) != 2 || history.Versions[0].Revision != "1" || !history.Versions[0].Current ||
		!history.Versions[1].Original || history.Versions[1].Revision != "" || history.Versions[1].Current {
		t.Fatalf("the versions: %+v", history.Versions)
	}
	again := p.app.OpenItemDraft(desktop.ItemRequest{Context: p.context, Ref: desktop.ItemRef{Kind: desktop.SuiteItem, ID: first.ID, Revision: "original"}})
	if again.Suite == nil || !again.Suite.Original || again.Suite.Document != original {
		t.Fatalf("the original after the first save: %+v", again)
	}
	listing := listed(t, p.app, root, desktop.SuiteItem)["Nightly"]
	if listing.Ref.ID != first.ID || listing.Summary.Suite.Entry == "nightly.json" || !listing.Summary.Suite.Runnable {
		t.Fatalf("the saved suite: %+v", listing)
	}
}

// A suite imported from a file outside the project fills one draft: each
// reference that holds exactly what a test, environment or case of the
// project holds is that object, and one the project does not hold stays as
// the file declares it, a problem at its member. Nothing is saved.
func TestAnImportedSuiteFillsOneDraftAndKeepsWhatItCannotResolve(t *testing.T) {
	p := newSuiteProject(t, "127.0.0.1:2575")
	root := p.context.Project
	outside := t.TempDir()
	copyEntry(t, filepath.Join(root, "incident"), filepath.Join(outside, "case"))
	writeDocument(t, outside, "booking.json", read(t, filepath.Join(root, memberEntry(t, root, p.booking, "test"))))
	writeDocument(t, outside, "lab.json", read(t, filepath.Join(root, memberEntry(t, root, p.lab, "target"))))
	unknown := strings.Replace(read(t, filepath.Join(root, memberEntry(t, root, p.booking, "test"))), "Booking receives ACK", "Unknown test", 1)
	writeDocument(t, outside, "unknown.json", unknown)
	writeDocument(t, outside, "imported.json", `{"schema":"readmit-suite/v1","id":"imported-smoke","owner":"interop","tags":[],"parallelism":1,`+
		`"environments":[{"id":"east","site":"hospital-a","bindings":[{"parameter":"interface","target":"lab.json"}]}],`+
		`"tables":[{"id":"patients","rows":[{"id":"one","case":"case"}]}],`+
		`"tests":[{"id":"booking","spec":"booking.json","owner":"interop","tags":[],"parameter":"interface","table":"patients","isolation":"shared","sequence":["s0001-e000001"]},`+
		`{"id":"unknown","spec":"unknown.json","owner":"interop","tags":[],"parameter":"interface","table":"patients","isolation":"shared","sequence":["s0001-e000001"]}]}`)
	before := entries(t, root)
	p.dialog.files = []string{filepath.Join(outside, "imported.json")}
	imported := p.app.ImportSuiteItem(p.context)
	if imported.State != desktop.Completed || !imported.New || imported.Draft == nil || imported.Draft.Name != "Imported smoke" || imported.Suite == nil {
		t.Fatalf("import: %+v", imported)
	}
	draft := imported.Draft.Suite
	if draft.Tests[0].Test != p.booking || draft.Tests[1].Test.ID != "" || draft.Tests[1].Source != "unknown.json" || draft.Datasets[0].Rows[0].Case != p.incident ||
		draft.Environments[0].Bindings[0].Target != (desktop.ItemRef{Kind: desktop.EnvironmentItem, ID: p.lab.ID}) {
		t.Fatalf("the imported draft: %+v", draft)
	}
	if !slices.Equal(fields(imported.Problems), []string{"tests.1.test"}) {
		t.Fatalf("the import's problems: %+v", imported.Problems)
	}
	if p.dialog.titles[len(p.dialog.titles)-1] != "Import suite" || !slices.Equal(entries(t, root), before) {
		t.Fatalf("the import wrote something or asked another dialog: %v", p.dialog.titles)
	}
}

// Export writes one version's exact suite document to a new file the person
// names, and the run configuration is the configuration `readmit suite
// prepare` writes from that document into a new folder the person names.
// Neither sends, and neither writes anywhere else.
func TestExportAndRunConfigurationWriteOnlyWhereThePersonChooses(t *testing.T) {
	p := newSuiteProject(t, "127.0.0.1:2575")
	root := p.context.Project
	ref := p.saveSuite(t, "create", "", "", "Scheduling smoke", p.smokeSuite())
	entry := listed(t, p.app, root, desktop.SuiteItem)["Scheduling smoke"].Summary.Suite.Entry
	before := entries(t, root)
	out := t.TempDir()
	p.dialog.destination = filepath.Join(out, "exported.json")
	exported := p.app.ExportSuiteItem(desktop.SuiteExportRequest{Context: p.context, Suite: ref})
	if exported.State != desktop.Completed || exported.Output != p.dialog.destination || read(t, exported.Output) != read(t, filepath.Join(root, entry)) {
		t.Fatalf("export: %+v", exported)
	}
	if again := p.app.ExportSuiteItem(desktop.SuiteExportRequest{Context: p.context, Suite: ref}); again.State != desktop.Failed {
		t.Fatalf("an export replaced a file: %+v", again)
	}
	p.dialog.destination = filepath.Join(out, "configuration")
	configured := p.app.ExportSuiteRunConfiguration(desktop.SuiteExportRequest{Context: p.context, Suite: ref, Environment: "lab"})
	if configured.State != desktop.Completed || configured.Output != p.dialog.destination {
		t.Fatalf("run configuration: %+v", configured)
	}
	if _, err := suite.Prepare(suite.Request{Path: filepath.Join(root, entry), Environment: "lab", Output: filepath.Join(out, "engine")}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"suite.json", "selection.json", "queue.json"} {
		if read(t, filepath.Join(out, "configuration", name)) != read(t, filepath.Join(out, "engine", name)) {
			t.Fatalf("%s differs from what the engine prepares", name)
		}
	}
	if !slices.Equal(p.dialog.titles[len(p.dialog.titles)-3:], []string{"Export suite", "Export suite", "Export run configuration"}) ||
		!slices.Equal(p.dialog.named, []string{"Scheduling smoke.json", "Scheduling smoke.json"}) {
		t.Fatalf("the dialogs: %v %v", p.dialog.titles, p.dialog.named)
	}
	if after := entries(t, root); !slices.Equal(after, before) {
		t.Fatalf("an export wrote into the project: %v", after)
	}
	if refused := p.app.ExportSuiteRunConfiguration(desktop.SuiteExportRequest{Context: p.context, Suite: ref}); refused.State != desktop.Failed {
		t.Fatalf("a run configuration for no environment: %+v", refused)
	}
}

// A saved suite version runs through the queue exactly as its entry would:
// preflighted and executed compiled against the current version of each
// environment it binds, pinned to the identity the preflight showed. Its
// history then lists the run of that version with each test's result, its
// coverage is the engine's assessment of that run, and a later version
// compares with it by its exact check changes.
func TestASuiteVersionRunsAndItsHistoryCoverageAndComparisonReadTheRun(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	p := newSuiteProject(t, peer.address)
	root := p.context.Project
	draft := p.smokeSuite()
	draft.Tests = draft.Tests[:1]
	draft.Datasets[0].Rows = draft.Datasets[0].Rows[1:]
	draft.Datasets = draft.Datasets[:1]
	draft.Environments = draft.Environments[:1]
	draft.Environments[0].Bindings = draft.Environments[0].Bindings[:1]
	draft.Exclusions = []desktop.SuiteExclusion{}
	ref := p.saveSuite(t, "create", "", "", "Scheduling smoke", draft)
	target := desktop.SuiteRunTarget{Context: p.context, Suite: ref}

	preflight := p.app.PreflightRun(desktop.RunPreflightRequest{Suite: &target, Environment: "lab"})
	if preflight.State != desktop.Completed || preflight.Preflight == nil || preflight.Preflight.Kind != "suite" || preflight.Preflight.Name != "Scheduling smoke" ||
		preflight.Preflight.Suite == nil || len(preflight.Preflight.Suite.Targets) != 1 || preflight.Preflight.Suite.Targets[0].Address != peer.address {
		t.Fatalf("preflight: %+v", preflight)
	}
	if peer.deliveries() != 0 {
		t.Fatal("the preflight sent something")
	}
	if stale := p.app.StartSuiteRun(desktop.SuiteRunRequest{Item: &target, Environment: "lab", Output: "suite-run-stale", Expected: strings.Repeat("0", 64)}); stale.State != desktop.Failed ||
		!strings.Contains(stale.Reason, "preflight it again") || peer.deliveries() != 0 {
		t.Fatalf("a run of another identity: %+v", stale)
	}
	executed := p.app.StartSuiteRun(desktop.SuiteRunRequest{Item: &target, Environment: "lab", Output: "suite-run", Expected: preflight.Preflight.Identity})
	if executed.State != desktop.Completed || executed.Report == nil || executed.Report.Executed != 1 || executed.Report.Jobs[0].Run.State != durablerun.Passed {
		t.Fatalf("run: %+v", executed)
	}
	if slices.ContainsFunc(entries(t, root), func(entry string) bool { return strings.HasPrefix(entry, ".readmit-compiled-suite-") }) {
		t.Fatal("the compiled suite was left in the project")
	}

	history := p.app.SuiteHistory(desktop.ItemRequest{Context: p.context, Ref: ref})
	if history.State != desktop.Completed || len(history.Runs) != 1 || history.Runs[0].Revision != "1" || history.Runs[0].Environment != "Scheduling lab" ||
		history.Runs[0].Outcome != "executed" || len(history.Results) != 1 || history.Results[0] != (desktop.SuiteTestResult{Test: "booking", Result: desktop.SuitePassed, Run: history.Runs[0].Run}) {
		t.Fatalf("history: %+v", history)
	}
	if summary := listed(t, p.app, root, desktop.SuiteItem)["Scheduling smoke"].Summary.Suite; summary.LatestRun == nil || *summary.LatestRun != history.Runs[0].Run || summary.LatestOutcome != "executed" {
		t.Fatalf("the listed latest run: %+v", summary)
	}

	// Coverage is the engine's assessment of the run, over the version's
	// requirements; nothing runs.
	coverage := p.app.SuiteCoverage(desktop.SuiteCoverageRequest{Context: p.context, Suite: ref, Previous: []desktop.ItemRef{}})
	if coverage.State != desktop.Completed || coverage.Run == nil || *coverage.Run != history.Runs[0].Run || coverage.Denominator != 2 || coverage.Passed != 1 ||
		!reflect.DeepEqual(coverage.Requirements, []desktop.SuiteRequirementResult{{ID: "books", State: "passed"}, {ID: "downstream", State: "uncovered"}}) ||
		len(coverage.Jobs) != 1 || coverage.Jobs[0].Test != "booking" || coverage.Jobs[0].Row != "second" || !coverage.Jobs[0].Eligible {
		t.Fatalf("coverage: %+v", coverage)
	}
	policy, err := suite.BuildCoverage(filepath.Join(root, "suite-run"), []suite.Requirement{{ID: "books", Jobs: []string{"booking-second"}}, {ID: "downstream", Jobs: []string{}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	declared := filepath.Join(t.TempDir(), "coverage.json")
	if err := os.WriteFile(declared, policy, 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := suite.AssessCoverage(t.Context(), filepath.Join(root, "suite-run"), declared, nil, time.Now())
	if err != nil || engine.Denominator != coverage.Denominator || engine.Passed != coverage.Passed || engine.Jobs[0].Execution != coverage.Jobs[0].Execution {
		t.Fatalf("the window's coverage %+v differs from the engine's %+v: %v", coverage, engine, err)
	}
	if peer.deliveries() != 1 {
		t.Fatalf("coverage sent something: %d deliveries", peer.deliveries())
	}

	// A later version pins a changed test version: it compares by its exact
	// check change, and has no run of its own.
	changed := p.saveTest(t, "booking-2", p.booking.ID, "Booking receives ACK", suiteCheck("accepted", "MSA-1", "AE"))
	later := draft
	later.Tests = slices.Clone(draft.Tests)
	later.Tests[0].Test = changed
	opened := p.app.OpenItemDraft(desktop.ItemRequest{Context: p.context, Ref: ref})
	later.ID = opened.Draft.Suite.ID
	second := p.saveSuite(t, "later", ref.ID, "1", "Scheduling smoke", later)
	compared := p.app.CompareSuiteVersions(desktop.SuiteCompareRequest{Context: p.context, Suite: ref, From: "1", To: second.Revision})
	if compared.State != desktop.Completed || compared.First || len(compared.Tests) != 1 || compared.Tests[0].From != "1" || compared.Tests[0].To != "2" ||
		len(compared.Tests[0].Checks) != 1 || *compared.Tests[0].Checks[0].Earlier.Field.Text != "AA" || *compared.Tests[0].Checks[0].Later.Field.Text != "AE" ||
		!slices.Contains(compared.Tests[0].Changes, desktop.ChangeChecks) ||
		!slices.Contains(compared.Changes, desktop.SuiteChange{Area: "test", Subject: "Booking receives ACK", Earlier: "version 1", Later: "version 2"}) {
		t.Fatalf("the comparison: %+v", compared)
	}
	alone := p.app.CompareSuiteVersions(desktop.SuiteCompareRequest{Context: p.context, Suite: ref, To: "1"})
	if !alone.First || len(alone.Changes) != 0 || len(alone.Tests) != 1 || alone.Tests[0].Checks[0].Earlier != nil || alone.Tests[0].Checks[0].Later == nil {
		t.Fatalf("the first version alone: %+v", alone)
	}
	if none := p.app.SuiteCoverage(desktop.SuiteCoverageRequest{Context: p.context, Suite: second}); none.State != desktop.Empty || none.Run != nil {
		t.Fatalf("coverage of a version no run executed: %+v", none)
	}
}

// approvalItem is the one suite approval history of the project and the
// approval its current revision holds.
func approvalOf(t *testing.T, p *suiteProject) map[string]any {
	t.Helper()
	histories := listed(t, p.app, p.context.Project, desktop.SuiteApprovalItem)
	if len(histories) != 1 {
		t.Fatalf("the approval histories: %+v", histories)
	}
	for _, history := range histories {
		var record map[string]any
		if err := json.Unmarshal([]byte(read(t, filepath.Join(p.context.Project, memberEntry(t, p.context.Project, history.Ref, "suite-approval")))), &record); err != nil {
			t.Fatal(err)
		}
		return record
	}
	return nil
}

// approve prepares and makes one suite approval with its rationale.
func approve(t *testing.T, p *suiteProject, request desktop.PrepareActionRequest, click string) desktop.ReviewedActionResult {
	t.Helper()
	review := prepared(t, p.app, request)
	approved := p.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: p.context, Token: review.Token, IntentID: click,
		Decisions: desktop.ReviewDecisions{Rationale: "Reviewed " + click}})
	if approved.Outcome != desktop.ActionCompleted || approved.SuiteApproval == nil {
		t.Fatalf("%s: %+v", click, approved)
	}
	return approved
}

// A baseline and an environment approval keep their own scopes and actors.
// A baseline releases every test version the version pins, as the release
// `readmit expectation show` reads and whose baseline `readmit baseline show`
// reads; an environment approval binds those releases for one environment
// and a target revision through the promotion `readmit suite
// approve-promotion` records, is refused as stale when an environment
// changes under its review, and reads stale once an environment it bound
// moves on. A team request is refused without a hub session.
func TestBaselineAndEnvironmentApprovalsKeepTheirScopesAndActors(t *testing.T) {
	p := newSuiteProject(t, "127.0.0.1:2575")
	root := p.context.Project
	draft := p.smokeSuite()
	draft.Datasets[0].Rows[0].Expected = nil
	ref := p.saveSuite(t, "create", "", "", "Scheduling smoke", draft)
	suiteRow := listed(t, p.app, root, desktop.SuiteItem)["Scheduling smoke"]
	for _, action := range []desktop.ActionID{desktop.ApproveSuiteBaselineAction, desktop.RequestSuiteReviewAction, desktop.ApproveSuiteReleaseAction, desktop.ApprovePromotionAction} {
		if !slices.Contains(suiteRow.Capabilities, action) {
			t.Fatalf("the suite does not offer %s: %+v", action, suiteRow.Capabilities)
		}
	}
	environment := desktop.PrepareActionRequest{Context: p.context, Action: desktop.ApprovePromotionAction, Items: []desktop.ItemRef{ref},
		SuiteApproval: &desktop.SuiteApprovalOptions{Environment: "lab", Revision: "build-7"}}
	if early := p.app.PrepareAction(environment); early.Review == nil || early.Review.Ready || !strings.Contains(early.Review.Refusal, "baseline") {
		t.Fatalf("an environment approval before a baseline: %+v", early)
	}

	baselineRequest := desktop.PrepareActionRequest{Context: p.context, Action: desktop.ApproveSuiteBaselineAction, Items: []desktop.ItemRef{ref}}
	review := prepared(t, p.app, baselineRequest)
	shown := review.SuiteApproval
	if shown == nil || shown.Scope != desktop.BaselineApproval || shown.Actor != reviewer(t) || shown.Suite != "Scheduling smoke" || shown.Version != "1" ||
		len(shown.Tests) != 2 || shown.Tests[0] != (desktop.SuiteApprovalTest{Name: "Booking receives ACK", Version: "1", Release: "1"}) ||
		shown.Comparison == nil || !shown.Comparison.First || !slices.Equal(review.Requirements, []desktop.ReviewRequirement{desktop.RationaleRequirement}) {
		t.Fatalf("the baseline review: %+v", review)
	}
	if missing := p.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: p.context, Token: review.Token, IntentID: "no-reason"}); missing.Outcome != desktop.ActionRefused {
		t.Fatalf("a baseline without a reason: %+v", missing)
	}
	baseline := approve(t, p, baselineRequest, "baseline")
	if recorded := baseline.SuiteApproval; recorded.Scope != desktop.BaselineApproval || recorded.Actor != reviewer(t) || recorded.Revision != "1" || !recorded.Current {
		t.Fatalf("the baseline: %+v", recorded)
	}
	// The release the baseline recorded is the one the command line shows.
	record := approvalOf(t, p)
	tests := record["tests"].([]any)
	var bytes []byte
	if err := json.Unmarshal([]byte(`"`+tests[0].(map[string]any)["bytes"].(string)+`"`), &bytes); err != nil {
		t.Fatal(err)
	}
	released := filepath.Join(t.TempDir(), "release.json")
	if err := os.WriteFile(released, bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := commandLine(t, "expectation", "show", released)
	var printed printedRelease
	if err != nil || json.Unmarshal([]byte(stdout), &printed, json.RejectUnknownMembers(true)) != nil || printed.ID != "test-"+p.booking.ID ||
		printed.Approver != reviewer(t) || printed.Rationale != "Reviewed baseline" || printed.Identity != tests[0].(map[string]any)["release"] {
		t.Fatalf("the command line shows %+v: %v %s", printed, err, stderr)
	}
	release, err := expectation.Read(released)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := release.Baseline.Encode()
	if err != nil {
		t.Fatal(err)
	}
	revision := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(revision, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = commandLine(t, "baseline", "show", revision)
	var inspected printedBaseline
	if err != nil || json.Unmarshal([]byte(stdout), &inspected, json.RejectUnknownMembers(true)) != nil || inspected.Approver != reviewer(t) || inspected.Baseline.Revision != 1 {
		t.Fatalf("the command line shows the baseline %+v: %v %s", inspected, err, stderr)
	}

	// The environment approval binds those releases for one environment.
	review = prepared(t, p.app, environment)
	if shown := review.SuiteApproval; shown.Scope != desktop.EnvironmentApproval || shown.Environment != "Scheduling lab" || shown.Site != "hospital-a" ||
		shown.TargetRevision != "build-7" || shown.Actor != reviewer(t) || len(shown.Targets) != 2 ||
		shown.Targets[0] != (desktop.SuiteApprovalTarget{Parameter: "scheduling", Target: "Scheduling lab", Version: "1"}) || shown.Tests[1].Release != "1" {
		t.Fatalf("the environment review: %+v", shown)
	}
	// An environment changed under the review refuses the click, with a
	// refreshed review to look at.
	moved := saveEnvironment(t, p.app, p.context, desktop.SaveItemRequest{Item: p.lab.ID, BaseRevision: "1", IntentID: "lab-2",
		Draft: desktop.ItemDraft{Name: "Scheduling lab", Environment: targetDraft("127.0.0.1:2576")}})
	stale := p.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: p.context, Token: review.Token, IntentID: "stale-environment",
		Decisions: desktop.ReviewDecisions{Rationale: "Reviewed"}})
	if stale.Outcome != desktop.ActionStale || stale.Refreshed == nil || stale.SuiteApproval != nil || stale.Refreshed.SuiteApproval.Targets[0].Version != moved.Revision {
		t.Fatalf("a changed environment approved: %+v", stale)
	}
	approved := approve(t, p, environment, "environment")
	if recorded := approved.SuiteApproval; recorded.Scope != desktop.EnvironmentApproval || recorded.Environment != "Scheduling lab" || recorded.TargetRevision != "build-7" ||
		recorded.Actor != reviewer(t) || !recorded.Current {
		t.Fatalf("the environment approval: %+v", recorded)
	}
	if promotion, err := suite.DecodePromotion(approvalBytes(t, approvalOf(t, p)["environment"].(map[string]any)["promotion"].(string))); err != nil ||
		promotion.Review.Environment != "lab" || promotion.Review.Revision != "build-7" || promotion.Approver != reviewer(t) {
		t.Fatalf("the promotion the approval records: %+v %v", promotion, err)
	}
	// Once an environment it bound moves on, the approval reads stale and
	// stays in history; nothing renews it.
	saveEnvironment(t, p.app, p.context, desktop.SaveItemRequest{Item: p.backup.ID, BaseRevision: "1", IntentID: "backup-2",
		Draft: desktop.ItemDraft{Name: "Backup lab", Environment: targetDraft("127.0.0.1:2577")}})
	history := p.app.SuiteHistory(desktop.ItemRequest{Context: p.context, Ref: ref})
	approvals := history.Versions[0].Approvals
	if len(approvals) != 2 || approvals[0].Scope != desktop.BaselineApproval || !approvals[0].Current || approvals[1].Scope != desktop.EnvironmentApproval ||
		approvals[1].Current || !strings.Contains(approvals[1].Stale, "Backup lab") {
		t.Fatalf("the approval history: %+v", approvals)
	}

	// A team review is the signed-in hub subject's: none is made without one.
	request := desktop.PrepareActionRequest{Context: p.context, Action: desktop.RequestSuiteReviewAction, Items: []desktop.ItemRef{ref},
		SuiteApproval: &desktop.SuiteApprovalOptions{Reviewer: "reviewer@hospital.org"}}
	if refused := p.app.PrepareAction(request); refused.State != desktop.Failed || refused.Review != nil {
		t.Fatalf("a team review without a hub: %+v", refused)
	}
	if refused := p.app.SuiteReviewers(p.context); refused.State != desktop.Failed || len(refused.Reviewers) != 0 {
		t.Fatalf("reviewers without a hub: %+v", refused)
	}
}

// baselineChange is one change a release comparison reports.
type baselineChange = baseline.Change

// approvalBytes decodes the exact bytes an approval record carries.
func approvalBytes(t *testing.T, encoded string) []byte {
	t.Helper()
	var decoded []byte
	if err := json.Unmarshal([]byte(`"`+encoded+`"`), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

// A team review of a suite version is made through the signed-in customer
// hub, by the person signed in: the author requests it of one reviewer for
// the exact bytes of each release the version's baseline recorded, and only
// that reviewer approves it, answering the request. Each is recorded in the
// suite's history under its own scope and actor, beside the local baseline.
func TestATeamReviewOfASuiteVersionIsTheSignedInReviewers(t *testing.T) {
	var mu sync.Mutex
	events := []hubprotocol.ReviewEvent{}
	uploaded := map[string][]byte{}
	hubMux := http.NewServeMux()
	hubMux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	hubMux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	hubMux.HandleFunc("/v1/projects/cardio-study/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		mu.Lock()
		uploaded[filepath.Base(r.URL.Path)] = body
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	})
	hubMux.HandleFunc("/v2/projects/cardio-study/history", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, hubprotocol.ReviewHistory{Schema: hubprotocol.ReviewHistoryV2, Head: len(events), Events: slices.Clone(events)})
	})
	hubMux.HandleFunc("/v2/projects/cardio-study/reviews", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8192))
		command, err := hubprotocol.DecodeReviewCommand(body)
		mu.Lock()
		defer mu.Unlock()
		switch {
		case err != nil:
			w.WriteHeader(http.StatusBadRequest)
			return
		case command.Expected != len(events):
			w.WriteHeader(http.StatusConflict)
			return
		}
		// The stub hub knows each subject by what it may do here: the author
		// requests, and the reviewer answers the request addressed to them.
		actor := "author@hospital.org"
		switch command.Kind {
		case "review-request":
			if uploaded[command.Release] == nil || command.Evidence != command.Release {
				w.WriteHeader(http.StatusNotFound)
				return
			}
		case "approval":
			actor = "reviewer@hospital.org"
			answered := slices.ContainsFunc(events, func(event hubprotocol.ReviewEvent) bool {
				return event.Command.ID == command.Parent && event.Command.Kind == "review-request" && event.Command.Release == command.Release &&
					event.Command.Recipient == actor
			})
			if !answered {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
		event := hubprotocol.ReviewEvent{Schema: hubprotocol.ReviewEventV1, Project: "cardio-study", Sequence: len(events) + 1,
			Issuer: "https://idp.hospital.org", Actor: actor, At: time.Now().UTC().Format(time.RFC3339Nano), Command: command}
		events = append(events, event)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.MarshalWrite(w, event)
	})
	hubMux.HandleFunc("/v2/projects/cardio-study/reviewers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, hubprotocol.ProjectMembers{Schema: hubprotocol.ProjectMembersSchema, Project: "cardio-study", Issuer: "https://idp.hospital.org",
			Members: []hubprotocol.ProjectMember{{Subject: "author@hospital.org", Role: "owner", Status: "active"}, {Subject: "reviewer@hospital.org", Role: "reviewer", Status: "active"}}})
	})
	scopes := []string{"evidence.read", "evidence.write", "approval"}
	author := newAuthenticatedHubApp(t, hubMux, "author@hospital.org", scopes)
	p := suiteProjectOn(t, author.app, author.dialog, "127.0.0.1:2575")
	draft := p.smokeSuite()
	draft.Datasets[0].Rows[0].Expected = nil
	ref := p.saveSuite(t, "create", "", "", "Scheduling smoke", draft)
	approve(t, p, desktop.PrepareActionRequest{Context: p.context, Action: desktop.ApproveSuiteBaselineAction, Items: []desktop.ItemRef{ref}}, "baseline")

	if reviewers := p.app.SuiteReviewers(p.context); reviewers.State != desktop.Completed || reviewers.SignedIn != "author@hospital.org" || !slices.Equal(reviewers.Reviewers, []string{"reviewer@hospital.org"}) {
		t.Fatalf("the reviewers the project's members name: %+v", reviewers)
	}
	request := desktop.PrepareActionRequest{Context: p.context, Action: desktop.RequestSuiteReviewAction, Items: []desktop.ItemRef{ref},
		SuiteApproval: &desktop.SuiteApprovalOptions{Reviewer: "reviewer@hospital.org"}}
	if shown := prepared(t, p.app, request).SuiteApproval; shown.Scope != desktop.ReviewRequested || shown.Actor != "author@hospital.org" ||
		shown.Reviewer != "reviewer@hospital.org" || len(shown.Tests) != 2 {
		t.Fatalf("the request review: %+v", shown)
	}
	requested := approve(t, p, request, "request").SuiteApproval
	if requested.Scope != desktop.ReviewRequested || requested.Actor != "author@hospital.org" || requested.Reviewer != "reviewer@hospital.org" || !requested.Current {
		t.Fatalf("the request: %+v", requested)
	}
	record := approvalOf(t, p)
	for _, test := range record["tests"].([]any) {
		released := approvalBytes(t, test.(map[string]any)["bytes"].(string))
		if string(uploaded[digestOfBytes(released)]) != string(released) {
			t.Fatal("the hub does not hold the exact bytes of a release the request names")
		}
	}
	if len(events) != 2 || len(record["hub"].([]any)) != 2 {
		t.Fatalf("one request per release: %+v", events)
	}
	// The author cannot answer their own request.
	release := desktop.PrepareActionRequest{Context: p.context, Action: desktop.ApproveSuiteReleaseAction, Items: []desktop.ItemRef{ref}}
	if own := p.app.PrepareAction(release); own.Review == nil || own.Review.Ready || !strings.Contains(own.Review.Refusal, "addressed to you") {
		t.Fatalf("the author's own release: %+v", own)
	}

	signedIn := newAuthenticatedHubApp(t, hubMux, "reviewer@hospital.org", scopes)
	opened := signedIn.app.OpenNamedProject(p.context.Project)
	if opened.State != desktop.Completed {
		t.Fatalf("open: %+v", opened)
	}
	q := *p
	q.app, q.context = signedIn.app, opened.Context
	release.Context = q.context
	if shown := prepared(t, q.app, release).SuiteApproval; shown.Scope != desktop.ReleaseApproval || shown.Actor != "reviewer@hospital.org" || shown.Request != "author@hospital.org" {
		t.Fatalf("the release review: %+v", shown)
	}
	released := approve(t, &q, release, "release").SuiteApproval
	if released.Scope != desktop.ReleaseApproval || released.Actor != "reviewer@hospital.org" || !released.Current || len(events) != 4 {
		t.Fatalf("the release: %+v %+v", released, events)
	}
	if reviewers := q.app.SuiteReviewers(q.context); !slices.Equal(reviewers.Reviewers, []string{"author@hospital.org"}) {
		t.Fatalf("the reviewers the project's members name: %+v", reviewers)
	}
	history := p.app.SuiteHistory(desktop.ItemRequest{Context: p.context, Ref: ref})
	scopes = []string{}
	for _, approval := range history.Versions[0].Approvals {
		scopes = append(scopes, string(approval.Scope)+":"+approval.Actor)
	}
	if !slices.Equal(scopes, []string{"baseline:" + reviewer(t), "review-request:author@hospital.org", "release:reviewer@hospital.org"}) {
		t.Fatalf("the approvals: %v", scopes)
	}
}

// digestOfBytes is the SHA-256 a hub names bytes by.
func digestOfBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// approvalAt is the approval one revision of the project's one suite
// approval history holds.
func approvalAt(t *testing.T, p *suiteProject, revision string) map[string]any {
	t.Helper()
	for _, history := range listed(t, p.app, p.context.Project, desktop.SuiteApprovalItem) {
		var record map[string]any
		entry := memberEntry(t, p.context.Project, desktop.ItemRef{Kind: desktop.SuiteApprovalItem, ID: history.Ref.ID, Revision: revision}, "suite-approval")
		if err := json.Unmarshal([]byte(read(t, filepath.Join(p.context.Project, entry))), &record); err != nil {
			t.Fatal(err)
		}
		return record
	}
	t.Fatal("no suite approval history")
	return nil
}

// A comparison of two suite versions names exactly the tests `readmit
// expectation impact` finds affected between the releases the two versions'
// baselines recorded, each with the checks whose expectation changed.
func TestAVersionComparisonNamesTheTestsReleaseImpactFinds(t *testing.T) {
	p := newSuiteProject(t, "127.0.0.1:2575")
	root := p.context.Project
	draft := p.smokeSuite()
	draft.Datasets[0].Rows[0].Expected = nil
	ref := p.saveSuite(t, "create", "", "", "Scheduling smoke", draft)
	first := listed(t, p.app, root, desktop.SuiteItem)["Scheduling smoke"].Summary.Suite.Entry
	approve(t, p, desktop.PrepareActionRequest{Context: p.context, Action: desktop.ApproveSuiteBaselineAction, Items: []desktop.ItemRef{ref}}, "baseline-1")
	changed := p.saveTest(t, "booking-2", p.booking.ID, "Booking receives ACK", suiteCheck("accepted", "MSA-1", "AE"))
	later := draft
	later.Tests = slices.Clone(draft.Tests)
	later.Tests[0].Test = changed
	second := p.saveSuite(t, "later", ref.ID, "1", "Scheduling smoke", later)
	baseline := prepared(t, p.app, desktop.PrepareActionRequest{Context: p.context, Action: desktop.ApproveSuiteBaselineAction, Items: []desktop.ItemRef{second}})
	if shown := baseline.SuiteApproval; shown.Comparison == nil || shown.Comparison.First || shown.Comparison.From != "1" ||
		shown.Tests[0].Release != "2" || shown.Tests[1].Release != "1" {
		t.Fatalf("the second baseline's review: %+v", shown)
	}
	approve(t, p, desktop.PrepareActionRequest{Context: p.context, Action: desktop.ApproveSuiteBaselineAction, Items: []desktop.ItemRef{second}}, "baseline-2")

	folder := t.TempDir()
	released := map[string]string{}
	for revision, record := range map[string]map[string]any{"1": approvalAt(t, p, "1"), "2": approvalAt(t, p, "2")} {
		for _, test := range record["tests"].([]any) {
			held := test.(map[string]any)
			name := held["test"].(string) + "-" + revision + ".json"
			writeDocument(t, folder, name, string(approvalBytes(t, held["bytes"].(string))))
			released[name] = held["release"].(string)
		}
	}
	references := suite.ReleaseReferences{Schema: suite.ReleasesSchema, Tests: []suite.ReleaseReference{
		{Test: "booking", Release: "booking-1.json", Identity: released["booking-1.json"]},
		{Test: "reschedule", Release: "reschedule-1.json", Identity: released["reschedule-1.json"]},
	}}
	writeDocument(t, folder, "releases.json", string(marshal(t, references)))
	stdout, stderr, err := commandLine(t, "expectation", "impact", filepath.Join(folder, "booking-1.json"), filepath.Join(folder, "booking-2.json"),
		filepath.Join(root, first), "--releases", filepath.Join(folder, "releases.json"))
	var impact suite.ImpactReport
	if err != nil || json.Unmarshal([]byte(stdout), &impact) != nil {
		t.Fatalf("the command line's impact: %v %s", err, stderr)
	}
	affected := []string{}
	for _, test := range impact.Tests {
		if test.State == "affected" {
			affected = append(affected, test.Test)
		}
	}
	compared := p.app.CompareSuiteVersions(desktop.SuiteCompareRequest{Context: p.context, Suite: ref, From: "1", To: "2"})
	moved := []string{}
	for _, test := range compared.Tests {
		at := slices.IndexFunc(later.Tests, func(held desktop.SuiteTestDraft) bool { return held.Test == test.Test })
		moved = append(moved, later.Tests[at].ID)
		for _, check := range test.Checks {
			if !slices.ContainsFunc(impact.Comparison.Baseline.Changes, func(change baselineChange) bool { return change.Part == "assertion:"+check.Later.ID }) {
				t.Fatalf("the window names a check change the command line does not: %+v", check)
			}
		}
	}
	if !slices.Equal(moved, affected) || !slices.Equal(affected, []string{"booking"}) || len(compared.Tests[0].Checks) != 1 {
		t.Fatalf("the window compares %v, the command line finds %v affected: %+v", moved, affected, compared)
	}
}
