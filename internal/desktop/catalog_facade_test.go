package desktop_test

// The catalog is the named objects of a project as their own readers read
// them now. These tests populate a project from the shipped synthetic
// fixtures and the facade's own writers — never a hardcoded row — and require
// every kind to be listed through its reader, with a missing, unreadable or
// unsupported object kept as a named row with its reason.

import (
	"bytes"
	"encoding/base64"
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
	if first.Page == nil || len(first.Page.Items) != 2 || first.Page.Total != 3 || first.Page.NextCursor == "" ||
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
	if fresh := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.EnvironmentItem}); fresh.Page.Total != 4 {
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
