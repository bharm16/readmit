package desktop_test

// The catalog is the named objects of a project as their own readers read
// them now. These tests populate a project from the shipped synthetic
// fixtures and the facade's own writers — never a hardcoded row — and require
// every kind to be listed through its reader, with a missing, unreadable or
// unsupported object kept as a named row with its reason.

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/desktop"
)

// catalogProject is the sample workspace registered as a project, holding one
// object of every kind this release catalogs, written by the facade's own
// writers or copied from the shipped fixtures. It returns the app and the
// project folder.
func catalogProject(t *testing.T) (*desktop.App, string) {
	t.Helper()
	app := newApp(t, &chooser{folder: t.TempDir()})
	root := sample(t, app).Workspace.Root
	writeProject(t, root, registeredRegression)

	// A run: the registered case, sent once to a loopback laboratory.
	receiver := newReplayReceiver(t)
	writeDocument(t, root, "lab.json", replayTarget(receiver.address, "nonproduction"))
	writeDocument(t, root, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`)
	opened := app.OpenCase(root, "regression")
	if opened.Case == nil {
		t.Fatalf("the sample case: %+v", opened)
	}
	send := desktop.ReplayRequest{Workspace: root, Case: "regression", Identity: opened.Case.Identity, Target: "lab.json", Policy: "policy.json",
		Messages: []string{"s0001-e000001"}, Output: "first-run"}
	preview := replayPreviewed(t, app, send)
	if sent := app.SendReplay(desktop.ReplaySendRequest{Replay: send, Expected: preview.Identity, Approved: true}); sent.Run == nil {
		t.Fatalf("send: %+v", sent)
	}

	writeDocument(t, root, "reschedule-test.json", strings.Replace(fixture(t, "test-reschedule.json"), `"case": "test-case"`, `"case": "regression"`, 1))
	writeDocument(t, root, "test-target.json", fixture(t, "test-target.json"))
	writeDocument(t, root, "suite.json", suiteFixture)
	writeDocument(t, root, "checks.json", fixture(t, "assertion-set.json"))
	writeDocument(t, root, "local-profile.json", fixture(t, "local-profile.json"))
	writeDocument(t, root, "pack.json", fixture(t, "profile-pack.json"))
	writeDocument(t, root, "scenario.json", fixture(t, "scenario-siu.json"))
	writeDocument(t, root, "window.json", facadeWindowDocument)
	writeDocument(t, root, "source.json", facadeSourceDocument)
	writeDocument(t, root, "export.csv", "appointment,status\nA1,booked\n")
	if diagnosed := app.RunDiagnosis(desktop.DiagnosisRequest{Workspace: root, Case: "regression", Identity: opened.Case.Identity, Builtin: "siu", Output: "diagnosis"}); diagnosed.Diagnosis == nil {
		t.Fatalf("diagnosis: %+v", diagnosed)
	}
	generated(t, app, root, "packet")
	writeDocument(t, root, "runner.json", `{"schema":"readmit-runner/v1","hub":"https://hub.example:8443","project":"alpha",`+
		`"environment":"lab","root":"/var/lib/readmit-runner/runs","ca":"/etc/readmit-runner/ca.pem",`+
		`"certificate":"/etc/readmit-runner/client.pem",`+
		`"key":{"command":"/usr/local/bin/customer-secret-reader","arguments":["runner-key"]},`+
		`"token":{"command":"/usr/local/bin/customer-secret-reader","arguments":["runner-token"]},`+
		`"update_key":"`+base64.StdEncoding.EncodeToString(make([]byte, 32))+`","update_engine":"NEXT_APPROVED_BUILD"}`)
	if saved := app.SaveSchedulePolicy(desktop.SchedulePolicyRequest{Output: filepath.Join(root, "schedules.json"),
		Entries: []desktop.ScheduleEntryInput{scheduleEntry(strings.Repeat("b", 64), false)}}); saved.State != desktop.Completed {
		t.Fatalf("schedule: %+v", saved)
	}
	// A variant: a transformation plan of the registered case. A backup: a
	// verified backup of another project, placed here.
	writeDocument(t, root, "variant-plan.json", `{"schema":"readmit-transform-plan/v1","case":"`+opened.Case.Identity+
		`","rules":"`+strings.Repeat("a", 64)+`","steps":[]}`)
	other := writeProject(t, t.TempDir(), "")
	backupFolder := filepath.Join(t.TempDir(), "backup")
	if created := app.CreateProjectBackup(desktop.BackupCreateRequest{Project: other, Destination: backupFolder}); created.State != desktop.Completed {
		t.Fatalf("backup: %+v", created)
	}
	copyEntry(t, backupFolder, filepath.Join(root, "project-backup"))
	// Two objects no reader accepts: one declares a version this release
	// does not read, one is damaged.
	writeDocument(t, root, "future-target.json", `{"schema":"readmit-target/v9","address":"127.0.0.1:1"}`)
	writeDocument(t, root, "damaged-target.json", `{"schema":"readmit-target/v3","address":7}`)

	// Listing is a read: a project whose catalog was never recorded is
	// listed without a byte written into it. Opening it by name records it.
	if unrecorded := listed(t, app, root, desktop.CaseItem); len(unrecorded) == 0 {
		t.Fatal("an unrecorded project listed nothing")
	}
	if _, err := os.Lstat(filepath.Join(root, catalog.Folder)); !os.IsNotExist(err) {
		t.Fatal("listing a project wrote its catalog")
	}
	if opened := app.OpenNamedProject(root); opened.State != desktop.Completed || !opened.Recorded {
		t.Fatalf("open: %+v", opened)
	}
	return app, root
}

// listed lists one kind and fails unless the listing completed.
func listed(t *testing.T, app *desktop.App, root string, kind desktop.ItemKind) map[string]desktop.CatalogItem {
	t.Helper()
	result := app.ListCatalog(desktop.CatalogQuery{Context: desktop.RequestContext{Project: root, Generation: 7}, Kind: kind})
	if result.Page == nil || result.Context.Generation != 7 || result.Context.Project != root {
		t.Fatalf("listing %s: %+v", kind, result)
	}
	// An object that neither a person nor the object itself named has no
	// name; the test finds it by the project entry it was discovered at, as
	// "@entry".
	entries := map[string]string{}
	for _, entry := range entriesOf(t, root) {
		entries[catalog.DiscoveredID(string(kind), entry)] = entry
	}
	items := map[string]desktop.CatalogItem{}
	for _, item := range result.Page.Items {
		if item.Ref.Kind != kind {
			t.Fatalf("listing %s answered a %s", kind, item.Ref.Kind)
		}
		key := item.Name
		if key == "" {
			key = "@" + entries[item.Ref.ID]
		}
		items[key] = item
	}
	return items
}

func TestTheCatalogListsEveryKindThroughItsOwnReader(t *testing.T) {
	app, root := catalogProject(t)
	before := bytesUnder(t, filepath.Join(root, "regression"))

	cases := listed(t, app, root, desktop.CaseItem)
	registered, held := cases["Duplicate appointment after reschedule"]
	if !held || registered.Availability != desktop.ItemAvailable || registered.Summary.Case == nil ||
		!registered.Summary.Case.Registered || registered.Summary.Case.Status != "investigating" || registered.Summary.Case.Owner != "scheduling-team" ||
		registered.Summary.Case.Evidence != "verified" {
		t.Fatalf("the registered case: %+v", cases)
	}
	if !slices.Contains(registered.Capabilities, desktop.ReplaySendAction) || registered.CreatedAt != nil {
		t.Fatalf("a generated case has no import time, and an activated window may send it: %+v", registered)
	}
	if other, held := cases["@cancellation"]; !held || other.Availability != desktop.ItemAvailable || other.Summary.Case.Registered || other.Summary.Case.Status != "" {
		t.Fatalf("an unregistered case: %+v", cases)
	}

	tests := listed(t, app, root, desktop.TestItem)
	test := tests["Rescheduling updates the original appointment"]
	if test.Availability != desktop.ItemAvailable || test.Summary.Test == nil || test.Summary.Test.SourceCase == nil || test.Summary.Test.SourceCase.ID != registered.Ref.ID {
		t.Fatalf("the test and its source case: %+v", tests)
	}

	runs := listed(t, app, root, desktop.RunItem)
	run := runs["@first-run"]
	if run.Availability != desktop.ItemAvailable || run.Summary.Run == nil || run.Summary.Run.Outcome != "accepted" ||
		run.Summary.Run.StartedAt == nil || run.Summary.Run.CompletedAt == nil || run.CreatedAt == nil || !strings.HasPrefix(run.Summary.Run.Target, "127.0.0.1:") {
		t.Fatalf("the run: %+v", runs)
	}

	environments := listed(t, app, root, desktop.EnvironmentItem)
	if lab := environments["lab-replay"]; lab.Availability != desktop.ItemAvailable || lab.Summary.Environment.Classification != "nonproduction" || lab.Summary.Environment.LastCheckedAt != nil {
		t.Fatalf("the laboratory: %+v", environments)
	}
	// An explicit check is retained as the environment's latest one and read
	// back with its date, its outcome and the revision it checked.
	lab := environments["lab-replay"]
	checked := app.CheckEnvironment(desktop.ItemRequest{Context: desktop.RequestContext{Project: root}, Ref: lab.Ref})
	if checked.CheckedAt == "" || checked.Report == nil {
		t.Fatalf("a check of the laboratory: %+v", checked)
	}
	if again := listed(t, app, root, desktop.EnvironmentItem)["lab-replay"].Summary.Environment; again.LastCheckedAt == nil || *again.LastCheckedAt != checked.CheckedAt ||
		again.LastCheckOutcome != checked.Report.Outcome || again.LastCheckRevision != lab.Ref.Revision {
		t.Fatalf("the laboratory's latest check: %+v", again)
	}
	if future := environments["@future-target.json"]; future.Availability != desktop.ItemUnsupported || future.Reason == "" {
		t.Fatalf("a target of a later version: %+v", future)
	}
	if damaged := environments["@damaged-target.json"]; damaged.Availability != desktop.ItemUnreadable || damaged.Reason == "" ||
		slices.Contains(damaged.Capabilities, desktop.RenameAction) {
		t.Fatalf("a damaged target: %+v", damaged)
	}

	if suites := listed(t, app, root, desktop.SuiteItem); suites["nightly"].Summary.Suite == nil || suites["nightly"].Summary.Suite.Tests != 1 ||
		!slices.Equal(suites["nightly"].Summary.Suite.Environments, []string{"east"}) {
		t.Fatalf("the suite: %+v", suites)
	}
	if observations := listed(t, app, root, desktop.ObservationItem); observations["scheduling-archive"].Summary.Observation == nil ||
		observations["scheduling-archive"].Summary.Observation.SourceType != "file-export" || observations["scheduling-archive"].Summary.Observation.LatestCollection != nil {
		t.Fatalf("the observation source: %+v", observations)
	}
	reports := listed(t, app, root, desktop.ReportItem)
	if len(reports) != 1 {
		t.Fatalf("reports: %+v", reports)
	}
	for _, packet := range reports {
		if packet.Availability != desktop.ItemAvailable || packet.Summary.Report.Form != "synthetic-packet" {
			t.Fatalf("the packet: %+v", packet)
		}
	}
	if checks := listed(t, app, root, desktop.CheckGroupItem); checks["Synthetic reschedule expectations"].Summary.CheckGroup == nil {
		t.Fatalf("the check group: %+v", checks)
	}
	profiles := listed(t, app, root, desktop.ProfileItem)
	if local := profiles["fixture-local-siu"]; local.Summary.Profile == nil || local.Summary.Profile.Family != "SIU" ||
		local.Summary.Profile.ProtocolVersion != "2.5.1" || local.Summary.Profile.PublishedVersion != "1" {
		t.Fatalf("the local profile: %+v", profiles)
	}
	if len(profiles) != 2 {
		t.Fatalf("the profile pack: %+v", profiles)
	}
	if scenarios := listed(t, app, root, desktop.ScenarioItem); scenarios["siu-appointment-lifecycle"].Summary.Scenario == nil {
		t.Fatalf("the scenario: %+v", scenarios)
	}
	analyses := listed(t, app, root, desktop.AnalysisItem)
	if diagnosis := analyses["@diagnosis"]; diagnosis.Summary.Analysis == nil || diagnosis.Summary.Analysis.RelatedCase == nil ||
		diagnosis.Summary.Analysis.RelatedCase.ID != registered.Ref.ID {
		t.Fatalf("the diagnosis: %+v", analyses)
	}
	variants := listed(t, app, root, desktop.VariantItem)
	if plan := variants["@variant-plan.json"]; plan.Availability != desktop.ItemAvailable || plan.Summary.Variant == nil ||
		plan.Summary.Variant.Form != "transform-plan" || plan.Summary.Variant.Parent == nil || plan.Summary.Variant.Parent.ID != registered.Ref.ID {
		t.Fatalf("the variant: %+v", variants)
	}
	if backups := listed(t, app, root, desktop.BackupItem); backups["@project-backup"].Summary.Backup == nil || !backups["@project-backup"].Summary.Backup.Complete {
		t.Fatalf("the backup: %+v", backups)
	}
	if runners := listed(t, app, root, desktop.RunnerItem); runners["@runner.json"].Summary.Runner == nil || runners["@runner.json"].Summary.Runner.Environment != "lab" {
		t.Fatalf("the runner: %+v", runners)
	}
	if schedules := listed(t, app, root, desktop.ScheduleItem); schedules["@schedules.json"].Summary.Schedule == nil || schedules["@schedules.json"].Summary.Schedule.Schedules != 1 {
		t.Fatalf("the schedule: %+v", schedules)
	}

	// Listing is a read of evidence: the case's bytes are what they were, and
	// listing again answers the same identities.
	if after := bytesUnder(t, filepath.Join(root, "regression")); !maps.EqualFunc(after, before, bytes.Equal) {
		t.Fatal("listing the catalog changed evidence")
	}
	if again := listed(t, app, root, desktop.CaseItem); again["@cancellation"].Ref != cases["@cancellation"].Ref {
		t.Fatalf("an object's identity changed between reads: %+v %+v", again["@cancellation"].Ref, cases["@cancellation"].Ref)
	}
}

// A missing object stays a named row with a way to find it; locating it
// keeps its identity, and a name is catalog metadata alone.
func TestAMovedObjectStaysVisibleAndIsLocatedUnderItsIdentity(t *testing.T) {
	app, root := catalogProject(t)
	tests := listed(t, app, root, desktop.TestItem)
	original := tests["Rescheduling updates the original appointment"]
	if err := os.Rename(filepath.Join(root, "reschedule-test.json"), filepath.Join(root, "moved-test.json")); err != nil {
		t.Fatal(err)
	}
	var missing desktop.CatalogItem
	for _, item := range listed(t, app, root, desktop.TestItem) {
		if item.Ref.ID == original.Ref.ID {
			missing = item
		}
	}
	if missing.Availability != desktop.ItemMissing || missing.Reason == "" || !slices.Contains(missing.Capabilities, desktop.LocateAction) {
		t.Fatalf("a moved test is not a visible missing row: %+v", missing)
	}
	context := desktop.RequestContext{Project: root, Generation: 1}
	located := app.LocateItem(desktop.LocateRequest{Context: context, Ref: original.Ref, Entry: "moved-test.json"})
	if located.State != desktop.Completed || located.Item == nil || located.Item.Ref.ID != original.Ref.ID || located.Item.Availability != desktop.ItemAvailable {
		t.Fatalf("locate: %+v", located)
	}
	if after := listed(t, app, root, desktop.TestItem); len(after) != 1 {
		t.Fatalf("the located test is listed twice: %+v", after)
	}
	renamed := app.RenameItem(desktop.RenameRequest{Context: context, Ref: original.Ref, Name: "Reschedule keeps one appointment"})
	if renamed.State != desktop.Completed || renamed.Item.Name != "Reschedule keeps one appointment" {
		t.Fatalf("rename: %+v", renamed)
	}
	if moved := read(t, filepath.Join(root, "moved-test.json")); !strings.Contains(moved, "Rescheduling updates the original appointment") {
		t.Fatal("renaming rewrote the test")
	}
}

// A list longer than a page is continued from the snapshot its first page
// was cut from, whatever changed on disk meanwhile; a cursor whose snapshot
// is not held is refused rather than continued against another list.
func TestAPageContinuesTheSnapshotItWasCutFrom(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	writeProject(t, root, "")
	for _, name := range []string{"a", "b", "c"} {
		writeDocument(t, root, name+".json", strings.Replace(replayTarget("127.0.0.1:2575", "nonproduction"), "lab-replay", "lab-"+name, 1))
	}
	context := desktop.RequestContext{Project: root}
	first := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.EnvironmentItem, Limit: 2})
	if first.Page == nil || len(first.Page.Items) != 2 || first.Page.Total == nil || *first.Page.Total != 3 || first.Page.NextCursor == "" ||
		first.Page.Items[0].Name != "lab-a" || first.Page.Items[1].Name != "lab-b" {
		t.Fatalf("first page: %+v", first.Page)
	}
	writeDocument(t, root, "0.json", strings.Replace(replayTarget("127.0.0.1:2575", "nonproduction"), "lab-replay", "lab-0", 1))
	second := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.EnvironmentItem, Limit: 2, Cursor: first.Page.NextCursor})
	if second.Page == nil || len(second.Page.Items) != 1 || second.Page.Items[0].Name != "lab-c" || second.Page.Snapshot != first.Page.Snapshot || second.Page.NextCursor != "" {
		t.Fatalf("the second page did not continue the snapshot: %+v", second.Page)
	}
	if other := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem, Cursor: first.Page.NextCursor}); other.State != desktop.Failed {
		t.Fatalf("a cursor continued another list: %+v", other)
	}
	if fresh := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.EnvironmentItem}); fresh.Page.Total == nil || *fresh.Page.Total != 4 {
		t.Fatalf("a fresh read: %+v", fresh.Page)
	}

	// A later page carries what the guard admits now, not what it admitted
	// when the snapshot was cut.
	writeCase(t, root, "one", framed(fixture(t, "listen-s12.hl7")))
	writeCase(t, root, "two", framed(fixture(t, "listen-s13.hl7")))
	cases := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem, Limit: 1})
	if cases.Page == nil || !slices.Contains(cases.Page.Items[0].Capabilities, desktop.ReplaySendAction) {
		t.Fatalf("an activated window may send: %+v", cases.Page)
	}
	if selected := app.SelectOperationPolicy(authorOnlyPolicy(t)); selected.State != desktop.Completed {
		t.Fatal(selected)
	}
	later := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem, Limit: 1, Cursor: cases.Page.NextCursor})
	if later.Page == nil || len(later.Page.Items) != 1 || slices.Contains(later.Page.Items[0].Capabilities, desktop.ReplaySendAction) {
		t.Fatalf("a later page offered a send the guard no longer admits: %+v", later.Page)
	}
}

// A suite's latest run is the latest retained execution of its current
// version, read through the suite's own reader: an interrupted execution is
// never a completed one, and an execution of an earlier version is not one
// of the version the suite holds now.
func TestASuiteNamesItsLatestExecutionOfItsCurrentVersion(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	root := ackWorkspace(t, peer.address)
	writeAckSpec(t, root, "booking.json", "AA")
	writeSuite(t, root, "nightly.json")
	writeProject(t, root, "")
	app := workspaceApp(t)
	if nightly := listed(t, app, root, desktop.SuiteItem)["nightly"]; nightly.Summary.Suite == nil || nightly.Summary.Suite.LatestRun != nil {
		t.Fatalf("a suite never run: %+v", nightly)
	}
	identity := preflighted(t, app, desktop.RunPreflightRequest{Workspace: root, Spec: "nightly.json", Environment: "east"})
	if executed := app.StartSuiteRun(desktop.SuiteRunRequest{Workspace: root, Suite: "nightly.json", Environment: "east", Output: "suite-run", Expected: identity}); executed.State != desktop.Completed {
		t.Fatalf("the suite run: %+v", executed)
	}
	nightly := listed(t, app, root, desktop.SuiteItem)["nightly"]
	run := listed(t, app, root, desktop.RunItem)["@suite-run"]
	if run.Availability != desktop.ItemAvailable || run.Summary.Run == nil || run.Summary.Run.Outcome != "executed" || run.Summary.Run.Jobs != 1 ||
		run.Summary.Run.Suite == nil || run.Summary.Run.Suite.ID != nightly.Ref.ID || run.Summary.Run.Target != "" ||
		run.Summary.Run.StartedAt == nil || run.Summary.Run.CompletedAt == nil || run.Summary.Run.Uncertain != 0 {
		t.Fatalf("the suite execution: %+v", run)
	}
	if latest := nightly.Summary.Suite; latest.LatestRun == nil || latest.LatestRun.ID != run.Ref.ID || latest.LatestRunAt == nil ||
		*latest.LatestRunAt != *run.Summary.Run.StartedAt || latest.LatestOutcome != "executed" {
		t.Fatalf("the suite's latest run: %+v", latest)
	}
	// Without its queue report the execution was interrupted.
	if err := os.Remove(filepath.Join(root, "suite-run", "report.json")); err != nil {
		t.Fatal(err)
	}
	if interrupted := listed(t, app, root, desktop.RunItem)["@suite-run"]; interrupted.Summary.Run == nil || interrupted.Summary.Run.Outcome != "incomplete" {
		t.Fatalf("an execution without its report: %+v", interrupted)
	}
	if latest := listed(t, app, root, desktop.SuiteItem)["nightly"].Summary.Suite; latest.LatestOutcome != "incomplete" {
		t.Fatalf("the suite's interrupted latest run: %+v", latest)
	}
	// The suite edited: no execution ran the version it holds now, and the
	// earlier execution still names the suite it ran.
	writeSuiteRows(t, root, "nightly.json", "one", "two")
	if edited := listed(t, app, root, desktop.SuiteItem)["nightly"].Summary.Suite; edited.LatestRun != nil || edited.LatestRunAt != nil || edited.LatestOutcome != "" {
		t.Fatalf("an edited suite kept an earlier version's run: %+v", edited)
	}
	if earlier := listed(t, app, root, desktop.RunItem)["@suite-run"]; earlier.Summary.Run.Suite == nil || earlier.Summary.Run.Suite.ID != nightly.Ref.ID {
		t.Fatalf("an earlier version's execution: %+v", earlier)
	}
}

// walked lists every page of one kind, following each cursor, and returns
// every page.
func walked(t *testing.T, app *desktop.App, root string, kind desktop.ItemKind) []desktop.CatalogPage {
	t.Helper()
	var pages []desktop.CatalogPage
	cursor := ""
	for {
		result := app.ListCatalog(desktop.CatalogQuery{Context: desktop.RequestContext{Project: root}, Kind: kind, Cursor: cursor})
		if result.Page == nil {
			t.Fatalf("listing %s after %d pages: %+v", kind, len(pages), result)
		}
		pages = append(pages, *result.Page)
		if cursor = result.Page.NextCursor; cursor == "" {
			return pages
		}
		if len(pages) > 100 {
			t.Fatal("the cursors never ended")
		}
	}
}

// identities are the distinct objects every page listed, failing on one
// listed twice.
func identities(t *testing.T, pages []desktop.CatalogPage) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	for _, page := range pages {
		for _, item := range page.Items {
			if seen[item.Ref.ID] {
				t.Fatalf("%s is listed twice", item.Ref.ID)
			}
			seen[item.Ref.ID] = true
		}
	}
	return seen
}

// A folder with more entries than one listing reads is listed in full, one
// page after another, and never refused for its size.
func TestAFolderPastTheListingBoundIsContinuedByCursor(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	writeProject(t, root, "")
	for i := range 1100 {
		writeDocument(t, root, fmt.Sprintf("lab-%04d.json", i), strings.Replace(replayTarget("127.0.0.1:2575", "nonproduction"), "lab-replay", fmt.Sprintf("lab-%04d", i), 1))
	}
	for i := range 5 {
		writeCase(t, root, fmt.Sprintf("case-%d", i), framed(fixture(t, "listen-s12.hl7")))
	}
	if opened := app.OpenNamedProject(root); opened.State != desktop.Completed || !opened.Recorded {
		t.Fatalf("open: %+v", opened)
	}
	pages := walked(t, app, root, desktop.EnvironmentItem)
	if len(pages) != 6 || len(pages[0].Items) != desktop.CatalogPageSize || pages[0].NextCursor == "" {
		t.Fatalf("%d pages, the first of %d", len(pages), len(pages[0].Items))
	}
	for _, page := range pages {
		if page.Partial || page.Total == nil || *page.Total != 1100 || page.Snapshot != pages[0].Snapshot {
			t.Fatalf("a page of one window: %+v", page)
		}
	}
	if listed := identities(t, pages); len(listed) != 1100 {
		t.Fatalf("%d environments listed", len(listed))
	}
	if cases := walked(t, app, root, desktop.CaseItem); len(cases) != 1 || len(cases[0].Items) != 5 || *cases[0].Total != 5 {
		t.Fatalf("the cases: %+v", cases)
	}
}

// A project with more entries than this release lists at once is listed
// window by window: until the last window is read a page says it is partial
// and why, and claims no total; the cursor continues into the next window,
// and every object is listed once.
func TestTheDiscoveryWindowNamesWhatItCut(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	writeProject(t, root, "")
	const count = catalog.MaxItems + 4
	for i := range count {
		writeDocument(t, root, fmt.Sprintf("lab-%04d.json", i), strings.Replace(replayTarget("127.0.0.1:2575", "nonproduction"), "lab-replay", fmt.Sprintf("lab-%04d", i), 1))
	}
	// Recording the project records what the catalog holds, and is not
	// refused for the rest.
	if opened := app.OpenNamedProject(root); opened.State != desktop.Completed || !opened.Recorded {
		t.Fatalf("open: %+v", opened)
	}
	pages := walked(t, app, root, desktop.EnvironmentItem)
	last := pages[len(pages)-1]
	for _, page := range pages[:len(pages)-1] {
		if !page.Partial || page.Reason == "" || page.Total != nil {
			t.Fatalf("a page before the last window: %+v", page.Total)
		}
	}
	if last.Partial || last.Reason != "" || last.Total == nil || *last.Total != count {
		t.Fatalf("the last page: partial %v, total %v", last.Partial, last.Total)
	}
	listed := identities(t, pages)
	if len(listed) != count {
		t.Fatalf("%d of %d environments listed", len(listed), count)
	}
	// An object of a later window opens by its identity like any other. The
	// catalog is full, so it offers nothing more and says why; an object it
	// records is changed as ever.
	item := last.Items[len(last.Items)-1]
	if opened := app.OpenItem(desktop.ItemRequest{Context: desktop.RequestContext{Project: root}, Ref: item.Ref}); opened.Item == nil || opened.Item.Ref.ID != item.Ref.ID {
		t.Fatalf("open an object of the last window: %+v", opened)
	}
	if !slices.Equal(item.Capabilities, []desktop.ActionID{desktop.OpenAction}) || item.Reason == "" || item.Availability != desktop.ItemAvailable {
		t.Fatalf("an object the catalog cannot record: %+v", item)
	}
	recorded := pages[0].Items[0]
	if renamed := app.RenameItem(desktop.RenameRequest{Context: desktop.RequestContext{Project: root}, Ref: recorded.Ref, Name: "Renamed"}); renamed.State != desktop.Completed {
		t.Fatalf("rename a recorded object: %+v", renamed)
	}
}

// An object of a later discovery window that the catalog has room for is
// changed and reviewed where it is: its actions are the ones it is offered.
func TestAnObjectOfALaterWindowIsChangedAndReviewedWhereItIs(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	writeProject(t, root, "")
	writeCase(t, root, "a-incident", framed(fixture(t, "listen-s12.hl7")))
	for i := range 9 {
		writeDocument(t, root, fmt.Sprintf("a-note-%d.txt", i), "not an object")
	}
	for i := range catalog.MaxItems - 6 {
		writeDocument(t, root, fmt.Sprintf("lab-%04d.json", i), strings.Replace(replayTarget("127.0.0.1:2575", "nonproduction"), "lab-replay", fmt.Sprintf("lab-%04d", i), 1))
	}
	if opened := app.OpenNamedProject(root); opened.State != desktop.Completed {
		t.Fatalf("open: %+v", opened)
	}
	pages := walked(t, app, root, desktop.EnvironmentItem)
	last := pages[len(pages)-1]
	if !pages[0].Partial || len(last.Items) == 0 {
		t.Fatalf("the project is not read in windows: %d pages", len(pages))
	}
	later := last.Items[len(last.Items)-1]
	if !slices.Contains(later.Capabilities, desktop.RenameAction) || later.Reason != "" {
		t.Fatalf("an object the catalog has room for: %+v", later)
	}
	context := desktop.RequestContext{Project: root}
	if renamed := app.RenameItem(desktop.RenameRequest{Context: context, Ref: later.Ref, Name: "Far laboratory"}); renamed.State != desktop.Completed || renamed.Item.Name != "Far laboratory" {
		t.Fatalf("rename an object of a later window: %+v", renamed)
	}
	incident := listed(t, app, root, desktop.CaseItem)["@a-incident"]
	review := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ReplaySendAction, Items: []desktop.ItemRef{incident.Ref},
		Destination: &desktop.ItemRef{Kind: desktop.EnvironmentItem, ID: later.Ref.ID}})
	if review.State != desktop.Completed || review.Review == nil || len(review.Review.Items) != 2 || review.Review.Items[1].Ref.ID != later.Ref.ID {
		t.Fatalf("a review of an object of a later window: %+v", review)
	}
}

// When a person last opened an object is this viewer's own record: the
// explicit open writes it beside the viewer's other documents and never into
// the project, listing reads it back, and another viewer has none.
func TestOpeningAnObjectRecordsWhenThisViewerOpenedIt(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	writeDocument(t, root, "lab.json", replayTarget("127.0.0.1:2575", "nonproduction"))
	lab := listed(t, app, root, desktop.EnvironmentItem)["lab-replay"]
	if lab.LastOpenedAt != nil {
		t.Fatalf("an object never opened: %+v", lab)
	}
	if again := listed(t, app, root, desktop.EnvironmentItem)["lab-replay"]; again.LastOpenedAt != nil {
		t.Fatalf("listing recorded an opening: %+v", again)
	}
	before := bytesUnder(t, root)
	opened := app.OpenItem(desktop.ItemRequest{Context: context, Ref: lab.Ref})
	if opened.Item == nil || opened.Item.LastOpenedAt == nil {
		t.Fatalf("open: %+v", opened)
	}
	if after := bytesUnder(t, root); !maps.EqualFunc(after, before, bytes.Equal) {
		t.Fatal("opening an object wrote into the project")
	}
	if listedAgain := listed(t, app, root, desktop.EnvironmentItem)["lab-replay"]; listedAgain.LastOpenedAt == nil || *listedAgain.LastOpenedAt != *opened.Item.LastOpenedAt {
		t.Fatalf("the listing after an open: %+v", listedAgain)
	}
	other := workspaceApp(t)
	if reopened := other.OpenNamedProject(root); reopened.State != desktop.Completed {
		t.Fatalf("another viewer: %+v", reopened)
	}
	if theirs := listed(t, other, root, desktop.EnvironmentItem)["lab-replay"]; theirs.LastOpenedAt != nil {
		t.Fatalf("another viewer sees this viewer's opening: %+v", theirs)
	}
}

// A report names the case it is about, as its own reader relates them, and
// whether it was reviewed: a sealed packet is not reviewed until a portable
// review is exported from it, which is itself a sealed report of that case.
func TestAReportNamesItsCaseAndWhetherItWasReviewed(t *testing.T) {
	app := newApp(t, &chooser{})
	root, spec, baseline, current := packetWorkspace(t, app)
	writeProject(t, root, "")
	assembled := app.AssemblePacket(packetRequest(root, spec, baseline, current))
	if assembled.Packet == nil {
		t.Fatalf("a packet: %+v", assembled)
	}
	incident := listed(t, app, root, desktop.CaseItem)["@case"]
	packet := listed(t, app, root, desktop.ReportItem)["@"+assembled.Packet.Entry]
	if packet.Summary.Report == nil || packet.Summary.Report.Form != "packet" || packet.Summary.Report.Status != "not-reviewed" ||
		packet.Summary.Report.RelatedCase == nil || packet.Summary.Report.RelatedCase.ID != incident.Ref.ID {
		t.Fatalf("the sealed packet: %+v", packet)
	}
	if exported := app.ExportPacketReview(desktop.PacketExportRequest{Workspace: root, Packet: assembled.Packet.Entry, Destination: filepath.Join(root, "review")}); exported.State != desktop.Completed {
		t.Fatalf("a portable review: %+v", exported)
	}
	reports := listed(t, app, root, desktop.ReportItem)
	if reviewed := reports["@"+assembled.Packet.Entry]; reviewed.Summary.Report.Status != "reviewed" {
		t.Fatalf("the reviewed packet: %+v", reviewed)
	}
	if review := reports["@review"]; review.Summary.Report == nil || review.Summary.Report.Form != "portable-review" || review.Summary.Report.Status != "sealed" ||
		review.Summary.Report.RelatedCase == nil || review.Summary.Report.RelatedCase.ID != incident.Ref.ID {
		t.Fatalf("the portable review: %+v", review)
	}
}

// An observation's latest collection is its latest trustworthy completed one:
// a collection that observed nothing is never chosen.
func TestAnObservationNamesItsLatestCompletedCollection(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	writeDocument(t, root, "export.csv", "appointment,status\nA1,booked\n")
	collect := func(name string, draft *desktop.ObservationDraft) desktop.ReviewedActionResult {
		saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Draft: desktop.ItemDraft{Name: name, Observation: draft}, IntentID: "save-" + name})
		if saved.Outcome != desktop.SavedOutcome {
			t.Fatalf("%+v", saved)
		}
		review := prepared(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.CollectObservationAction, Items: []desktop.ItemRef{*saved.Saved}})
		return app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "collect-" + name})
	}
	if collected := collect("Appointments", observationDraft(t, "appointments")); collected.Collected == nil || !collected.Collected.Trustworthy {
		t.Fatalf("a completed collection: %+v", collected)
	}
	missing := observationDraft(t, "cancellations")
	missing.Source.File.Path = "not-exported.csv"
	missing.Window.Completion.Deadline = "300ms"
	if incomplete := collect("Cancellations", missing); incomplete.Collected == nil || incomplete.Collected.Trustworthy {
		t.Fatalf("an incomplete collection: %+v", incomplete)
	}
	observations := listed(t, app, root, desktop.ObservationItem)
	if appointments := observations["Appointments"].Summary.Observation; appointments == nil || appointments.LatestCollection == nil || appointments.LatestStatus != "complete" {
		t.Fatalf("the completed observation: %+v", appointments)
	}
	if cancellations := observations["Cancellations"].Summary.Observation; cancellations == nil || cancellations.LatestCollection != nil || cancellations.LatestStatus != "" {
		t.Fatalf("an observation that observed nothing: %+v", cancellations)
	}
}
