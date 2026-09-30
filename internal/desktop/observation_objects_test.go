package desktop_test

// A named observation is edited, collected and read back as the saved object
// it is. These tests drive the facade over real project folders: a completed
// collection offers the state it settled on as a baseline, and a collection
// review names the revision it reads.

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/secret"
)

// collect reviews and runs one collection of an observation.
func collect(t *testing.T, app *desktop.App, context desktop.RequestContext, ref desktop.ItemRef, intent string) desktop.ReviewedActionResult {
	t.Helper()
	prepared := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.CollectObservationAction, Items: []desktop.ItemRef{ref}})
	if prepared.Review == nil || !prepared.Review.Ready {
		t.Fatalf("a collection review: %+v", prepared)
	}
	if prepared.Review.Collect.Revision != ref.Revision {
		t.Fatalf("the review names revision %q of %+v", prepared.Review.Collect.Revision, ref)
	}
	return app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: intent})
}

func TestObservationHistoryOffersEachCompletedCollectionAsABaseline(t *testing.T) {
	app, context := namedProject(t)
	writeDocument(t, context.Project, "export.csv", "appointment,status\nA1,booked\nA2,booked\n")
	draft := observationDraft(t, "appointments")
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem,
		Draft: desktop.ItemDraft{Name: "Appointments", Observation: draft}, IntentID: "first"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("%+v", saved)
	}
	collected := collect(t, app, context, *saved.Saved, "collect-1")
	if collected.Collected == nil || !collected.Collected.Trustworthy || collected.Collected.Baseline == "" {
		t.Fatalf("a completed collection offers no baseline: %+v", collected)
	}
	history := app.ObservationHistory(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if len(history.Collections) != 1 || history.Collections[0].Baseline != collected.Collected.Baseline {
		t.Fatalf("the history's baseline: %+v", history)
	}

	// The offered baseline is the state a recorded-baseline window names: a
	// window opened on it completes against the unchanged export.
	draft.Window.PreExisting = observewindow.PreExisting{Declaration: observewindow.RecordedBaseline, BaselineIdentity: history.Collections[0].Baseline}
	edited := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Item: saved.Saved.ID, BaseRevision: "1",
		Draft: desktop.ItemDraft{Name: "Appointments", Observation: draft}, IntentID: "second"})
	if edited.Outcome != desktop.SavedOutcome {
		t.Fatalf("%+v", edited)
	}
	again := collect(t, app, context, *edited.Saved, "collect-2")
	if again.Collected == nil || !again.Collected.Trustworthy {
		t.Fatalf("a collection against the offered baseline: %+v", again)
	}
}

func TestCollectionProgressReportsEachReadAndClearsWhenDone(t *testing.T) {
	app, context := namedProject(t)
	writeDocument(t, context.Project, "export.csv", "appointment,status\nA1,booked\n")
	draft := observationDraft(t, "appointments")
	draft.Window.Completion.QuietPeriod, draft.Window.Completion.StableSamples = "600ms", 3
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem,
		Draft: desktop.ItemDraft{Name: "Appointments", Observation: draft}, IntentID: "first"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("%+v", saved)
	}
	if idle := app.CollectionProgress(); idle.State != desktop.Empty || idle.Progress != nil {
		t.Fatalf("progress with nothing collecting: %+v", idle)
	}
	prepared := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.CollectObservationAction, Items: []desktop.ItemRef{*saved.Saved}})
	if prepared.Review == nil || !prepared.Review.Ready {
		t.Fatalf("a collection review: %+v", prepared)
	}
	finished := make(chan desktop.ReviewedActionResult, 1)
	go func() {
		finished <- app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "collect-1"})
	}()
	var measured *desktop.CollectionProgress
	for deadline := time.Now().Add(5 * time.Second); measured == nil && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if reached := app.CollectionProgress(); reached.State == desktop.Completed && reached.Progress.Samples > 0 {
			measured = reached.Progress
		}
	}
	if measured == nil || measured.Observation.ID != saved.Saved.ID || measured.Records == nil || *measured.Records != 1 || measured.Bytes == 0 ||
		measured.RequiredStable != 3 || measured.OpenedAt == nil || measured.Deadline == nil || measured.Elapsed == "" {
		t.Fatalf("the running collection's progress: %+v", measured)
	}
	if done := <-finished; done.Collected == nil || !done.Collected.Trustworthy {
		t.Fatalf("the collection: %+v", done)
	}
	if after := app.CollectionProgress(); after.State != desktop.Empty {
		t.Fatalf("progress after the collection ended: %+v", after)
	}
}

const databaseObservationSource = `{
 "schema":"readmit-observation-source/v3",
 "source":{"kind":"database-query","identity":"synthetic-lab","scope":"appointments"},
 "enabled":true,"freshness":{"max_age":"1h"},"extraction":null,"file":null,"http":null,"capture":null,
 "database":{"driver":"postgresql","address":"db.example:5432","classification":"nonproduction",
 "name":"synthetic","username":"observer","ca_file":"","server_name":"db.example",
 "credential":{"store":"os-keychain","address":"","purpose":"","command":"","arguments":[]},
 "view":["scheduling","appointments"],"record_key":"appointment","key_type":"text","filters":[{"column":"status","value":"ready"}],"limits":null}
}`

const httpObservationSource = `{
 "schema":"readmit-observation-source/v1",
 "source":{"kind":"http-api","identity":"scheduling-api","scope":"appointments"},
 "enabled":true,"freshness":{"max_age":"1h"},
 "extraction":{"envelope":"json","encoding":"utf-8","json":{"record_path":["appointments"]},"record_key":["id"]},
 "file":null,
 "http":{"url":"https://api.example/appointments","classification":"nonproduction","ca_file":"","server_name":"","timeout":"5s","max_bytes":65536,
  "retry":{"attempts":0,"delay":"0s"},"credential":{"store":"os-keychain","address":"","header":"Authorization","command":"","arguments":[]}}
}`

// observationOf is a draft of source over a window declaring the same
// source.
func observationOf(t *testing.T, document string) *desktop.ObservationDraft {
	t.Helper()
	var source observesource.Source
	if err := json.Unmarshal([]byte(document), &source); err != nil {
		t.Fatal(err)
	}
	window := observationDraft(t, "appointments").Window
	window.Source = source.Observes
	return &desktop.ObservationDraft{Source: source, Window: window}
}

func registerSourceCredential(t *testing.T, app *desktop.App, context desktop.RequestContext, name, address string, purpose secret.Purpose) {
	t.Helper()
	registered := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: name, Purpose: purpose, Store: secret.CustomerManaged,
		Address: address, Command: "/usr/bin/true", Arguments: []string{"--vault-path", name + "-locator"}})
	if registered.State != desktop.Completed {
		t.Fatalf("register %s: %+v", name, registered)
	}
}

func TestAnObservationNamesItsCredentialAndSavesItResolved(t *testing.T) {
	app, context := namedProject(t)
	registerSourceCredential(t, app, context, "scheduling-api", "api.example:443", secret.SourceEndpoint)
	registerSourceCredential(t, app, context, "scheduling-db", "db.example:5432", secret.SourceEndpoint)
	registerSourceCredential(t, app, context, "lab-mllp", "api.example:443", secret.MLLPEndpoint)

	for _, tc := range []struct{ document, credential string }{{httpObservationSource, "scheduling-api"}, {databaseObservationSource, "scheduling-db"}} {
		draft := observationOf(t, tc.document)
		draft.Credential = tc.credential
		saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Draft: desktop.ItemDraft{Name: tc.credential, Observation: draft}, IntentID: tc.credential})
		if saved.Outcome != desktop.SavedOutcome {
			t.Fatalf("an observation naming %s: %+v", tc.credential, saved)
		}
		written, err := os.ReadFile(filepath.Join(context.Project, memberEntry(t, context.Project, *saved.Saved, "source")))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(written), tc.credential+"-locator") || !strings.Contains(string(written), `"address":"`+map[string]string{"scheduling-api": "api.example:443", "scheduling-db": "db.example:5432"}[tc.credential]) {
			t.Fatalf("the saved source does not present the resolved reference: %s", written)
		}
		opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
		answered, _ := json.Marshal(opened)
		if opened.Draft == nil || opened.Draft.Observation.Credential != tc.credential || strings.Contains(string(answered), tc.credential+"-locator") {
			t.Fatalf("the opened draft of %s: %s", tc.credential, answered)
		}
		// Saving the opened draft as it is resolves the reference again.
		again := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Item: saved.Saved.ID, BaseRevision: "1",
			Draft: *opened.Draft, IntentID: tc.credential + "-again"})
		if again.Outcome != desktop.SavedOutcome {
			t.Fatalf("resaving %s: %+v", tc.credential, again)
		}
		if rewritten, _ := os.ReadFile(filepath.Join(context.Project, memberEntry(t, context.Project, *again.Saved, "source"))); !strings.Contains(string(rewritten), tc.credential+"-locator") {
			t.Fatalf("a resave lost the reference's arguments: %s", rewritten)
		}
	}

	for credential, want := range map[string]string{"lab-mllp": "another purpose", "scheduling-db": "scoped to db.example:5432", "absent": "no credential reference"} {
		draft := observationOf(t, httpObservationSource)
		draft.Credential = credential
		refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Draft: desktop.ItemDraft{Observation: draft}, IntentID: "refused-" + credential})
		if refused.Outcome != desktop.InvalidOutcome || len(refused.Problems) != 1 || refused.Problems[0].Field != "observation.credential" || !strings.Contains(refused.Problems[0].Problem, want) {
			t.Fatalf("an observation naming %s: %+v", credential, refused)
		}
	}
}

func TestRemoveCredentialNamesObservationsThatUseIt(t *testing.T) {
	app, context := namedProject(t)
	registerSourceCredential(t, app, context, "scheduling-api", "api.example:443", secret.SourceEndpoint)
	draft := observationOf(t, httpObservationSource)
	draft.Credential = "scheduling-api"
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Draft: desktop.ItemDraft{Name: "Scheduling API", Observation: draft}, IntentID: "api"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("%+v", saved)
	}
	refused := app.RemoveCredential(desktop.CredentialRequest{Context: context, Name: "scheduling-api"})
	if refused.State != desktop.Failed || len(refused.Referring) != 1 || refused.Referring[0].Ref.ID != saved.Saved.ID || refused.Referring[0].Name != "Scheduling API" {
		t.Fatalf("removing a credential an observation presents: %+v", refused)
	}
	if removed := app.RemoveItem(desktop.ItemRequest{Context: context, Ref: *saved.Saved}); removed.State != desktop.Completed {
		t.Fatalf("%+v", removed)
	}
	if removed := app.RemoveCredential(desktop.CredentialRequest{Context: context, Name: "scheduling-api"}); removed.State != desktop.Completed {
		t.Fatalf("removing a credential nothing presents: %+v", removed)
	}
}

func TestAnObservationRefusalIsAnsweredAtTheFieldThatHoldsIt(t *testing.T) {
	app, context := namedProject(t)
	registerSourceCredential(t, app, context, "scheduling-db", "db.example:5432", secret.SourceEndpoint)
	database := func(change func(*observesource.Database)) *desktop.ObservationDraft {
		draft := observationOf(t, databaseObservationSource)
		draft.Credential = "scheduling-db"
		change(draft.Source.Database)
		return draft
	}
	file := func(change func(*desktop.ObservationDraft)) *desktop.ObservationDraft {
		draft := observationDraft(t, "appointments")
		change(draft)
		return draft
	}
	// A schema-qualified view is read as the two identifiers it names.
	if qualified := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, IntentID: "qualified",
		Draft: desktop.ItemDraft{Observation: database(func(*observesource.Database) {})}}); qualified.Outcome != desktop.SavedOutcome ||
		!slices.Equal(qualified.Projection.Observation.Source.Database.View, []string{"scheduling", "appointments"}) {
		t.Fatalf("a schema-qualified view: %+v", qualified)
	}
	for field, draft := range map[string]*desktop.ObservationDraft{
		"observation.source.file.max_bytes":            file(func(d *desktop.ObservationDraft) { d.Source.File.MaxBytes = 0 }),
		"observation.window.completion.stable_samples": file(func(d *desktop.ObservationDraft) { d.Window.Completion.StableSamples = 0 }),
		"observation.window.completion.quiet_period":   file(func(d *desktop.ObservationDraft) { d.Window.Completion.QuietPeriod = "" }),
		"observation.source.database.driver":           database(func(d *observesource.Database) { d.Driver = "postgres" }),
		"observation.source.database.name":             database(func(d *observesource.Database) { d.Name = "" }),
		"observation.source.database.username":         database(func(d *observesource.Database) { d.Username = "" }),
		"observation.source.database.server_name":      database(func(d *observesource.Database) { d.ServerName = "" }),
		"observation.source.database.classification":   database(func(d *observesource.Database) { d.Classification = "" }),
		"observation.source.database.key_type":         database(func(d *observesource.Database) { d.KeyType = "" }),
		"observation.source.database.view":             database(func(d *observesource.Database) { d.View = []string{"a", "b", "c"} }),
		"observation.source.database.record_key":       database(func(d *observesource.Database) { d.RecordKey = "" }),
		"observation.source.database.filters.1": database(func(d *observesource.Database) {
			d.Filters = append(d.Filters, observesource.DatabaseFilter{Value: "x"})
		}),
	} {
		refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Draft: desktop.ItemDraft{Observation: draft}, IntentID: "refused-" + strings.ReplaceAll(field, ".", "-")})
		if refused.Outcome != desktop.InvalidOutcome || !slices.ContainsFunc(refused.Problems, func(p desktop.FieldProblem) bool { return p.Field == field }) {
			t.Errorf("a refusal at %s: %+v", field, refused.Problems)
		}
	}
}

func TestObservationFieldsReadsOnlyTheChosenExportHeader(t *testing.T) {
	app, context := namedProject(t)
	writeDocument(t, context.Project, "export.csv", "appointment,status\nA1,booked\n")
	before := entries(t, context.Project)
	draft := observationDraft(t, "appointments")
	listed := app.ObservationFields(desktop.ObservationFieldsRequest{Context: context, Source: &draft.Source})
	if listed.State != desktop.Completed || !slices.Equal(listed.Fields, []string{"appointment", "status"}) {
		t.Fatalf("the export's fields: %+v", listed)
	}
	if after := entries(t, context.Project); !slices.Equal(after, before) {
		t.Fatalf("listing fields wrote: %v", after)
	}
	draft.Source.File.Path = "not-exported.csv"
	if missing := app.ObservationFields(desktop.ObservationFieldsRequest{Context: context, Source: &draft.Source}); missing.State != desktop.Failed || len(missing.Fields) != 0 {
		t.Fatalf("fields of an absent export: %+v", missing)
	}
	if http := app.ObservationFields(desktop.ObservationFieldsRequest{Context: context, Source: &observationOf(t, httpObservationSource).Source}); http.State != desktop.Failed {
		t.Fatalf("fields of an HTTPS source: %+v", http)
	}
}

// A receiver ledger handoff, the file a test run reads as appointment
// records, names its record fields even before any record is written.
func TestObservationFieldsOfAnEmptyReceiverLedgerNameItsRecordFields(t *testing.T) {
	app, context := namedProject(t)
	writeDocument(t, context.Project, "appointments.json", emptyLedger(t))
	draft := observationDraft(t, "appointments")
	draft.Source.File.Path = "appointments.json"
	listed := app.ObservationFields(desktop.ObservationFieldsRequest{Context: context, Source: &draft.Source})
	if listed.State != desktop.Completed || !slices.Equal(listed.Fields, []string{"record_id", "patient_id", "placer_id", "filler_id", "appointment_start"}) {
		t.Fatalf("the ledger's fields: %+v", listed)
	}
}

func TestAResetChecksTheNamedObservationsLatestCompletedCollection(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	writeDocument(t, root, "export.csv", "appointment,status\n")
	observation := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem,
		Draft: desktop.ItemDraft{Name: "Appointments", Observation: observationDraft(t, "appointments")}, IntentID: "observation"})
	if observation.Outcome != desktop.SavedOutcome {
		t.Fatalf("%+v", observation)
	}
	if collected := collect(t, app, context, *observation.Saved, "collect-1"); collected.Collected == nil || *collected.Collected.Records != 0 {
		t.Fatalf("an empty collection: %+v", collected)
	}
	plan := func(named string) *fixturereset.Plan {
		return &fixturereset.Plan{Actions: []fixturereset.Action{{Operator: fixturereset.CollectionEmpty, Instructions: "The store is empty.", Observation: named}}}
	}
	links := &desktop.EnvironmentLinks{ResetName: "Empty store", ActionNames: []string{"Store is empty"}}
	if refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, IntentID: "unnamed", Draft: desktop.ItemDraft{Name: "Lab",
		Environment: environmentDraft("127.0.0.1:2575", "nonproduction", "plain"), ResetPlan: plan("export.csv"), Links: links}}); refused.Outcome != desktop.InvalidOutcome ||
		len(refused.Problems) != 1 || refused.Problems[0].Field != "reset.actions.0.observation" {
		t.Fatalf("a check naming no observation: %+v", refused)
	}
	environment := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "lab", Draft: desktop.ItemDraft{Name: "Lab",
		Environment: environmentDraft("127.0.0.1:2575", "nonproduction", "plain"), ResetPlan: plan(observation.Saved.ID), Links: links}})
	saved, err := os.ReadFile(filepath.Join(root, memberEntry(t, root, environment, "reset")))
	if err != nil || !strings.Contains(string(saved), memberEntry(t, root, *observation.Saved, "source")) {
		t.Fatalf("the saved plan does not name the observation's source: %s %v", saved, err)
	}
	if opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: environment}); opened.Draft == nil || opened.Draft.ResetPlan.Actions[0].Observation != observation.Saved.ID {
		t.Fatalf("the reset's draft names: %+v", opened.Draft)
	}

	reset := func(intent string) desktop.ReviewedActionResult {
		t.Helper()
		prepared := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ResetEnvironmentAction, Items: []desktop.ItemRef{environment}})
		review := prepared.Review
		if review == nil || !review.Ready || review.Reset.Actions[0].Observation == nil || review.Reset.Actions[0].Observation.ID != observation.Saved.ID ||
			review.Reset.Actions[0].ObservationName != "Appointments" || !strings.Contains(review.Reset.Actions[0].Effect, "Appointments") {
			t.Fatalf("a reset review: %+v", prepared)
		}
		return app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: intent})
	}
	if confirmed := reset("reset-1"); confirmed.State != desktop.Completed || confirmed.Reset.Result.Actions[0].Reason != fixturereset.CollectionEmptied {
		t.Fatalf("a reset of an empty store: %+v", confirmed)
	}
	// Saving the observation again: the reset reads its current revision.
	edited := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Item: observation.Saved.ID, BaseRevision: "1",
		Draft: desktop.ItemDraft{Name: "Appointments", Observation: observationDraft(t, "appointments")}, IntentID: "observation-2"})
	if edited.Outcome != desktop.SavedOutcome {
		t.Fatalf("%+v", edited)
	}
	writeDocument(t, root, "export.csv", "appointment,status\nA1,booked\n")
	if collected := collect(t, app, context, *edited.Saved, "collect-2"); collected.Collected == nil || *collected.Collected.Records != 1 {
		t.Fatalf("a collection that found a record: %+v", collected)
	}
	if failed := reset("reset-2"); failed.State != desktop.Failed || failed.Reset.Result.Actions[0].Reason != fixturereset.CollectionNotEmpty {
		t.Fatalf("a reset of a store that holds a record: %+v", failed)
	}
	if removed := app.RemoveItem(desktop.ItemRequest{Context: context, Ref: *edited.Saved}); removed.State != desktop.Failed || len(removed.Referring) != 1 ||
		removed.Referring[0].Ref.ID != environment.ID {
		t.Fatalf("removing an observation a reset checks: %+v", removed)
	}
}

func TestANewObservationStartsFromARetainedCapture(t *testing.T) {
	app, context := namedProject(t)
	binding := &operation.CaptureObservationBinding{CasePath: filepath.Join(context.Project, "captured-case"), Identity: "scheduling-sink", Scope: "appointments", RecordKey: "SCH-1.1"}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ObservationItem}, Capture: binding})
	if opened.State != desktop.Completed || !opened.New || opened.Draft == nil {
		t.Fatalf("an observation from a capture: %+v", opened)
	}
	draft := opened.Draft.Observation
	if draft.Source.Observes.Kind != observesource.DownstreamCapture || draft.Source.Capture == nil || draft.Source.Capture.Path != "captured-case" ||
		draft.Source.Capture.RecordKey != "SCH-1.1" || draft.Window.Source != draft.Source.Observes {
		t.Fatalf("the draft from a capture: %+v", draft)
	}
	if saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Draft: desktop.ItemDraft{Name: "Sink", Observation: draft}, IntentID: "sink"}); saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("saving it: %+v", saved)
	}
	binding.CasePath = filepath.Join(t.TempDir(), "elsewhere")
	if outside := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ObservationItem}, Capture: binding}); outside.State != desktop.Failed {
		t.Fatalf("a capture outside the project: %+v", outside)
	}
}

// Collections and a reset's empty check find an observation by the source it
// reads, so a second new observation over the same source and scope reads as
// its own, the same way on a retry, and a saved one moved onto another's
// source and scope is refused there.
func TestTwoObservationsNeverReadTheSameSource(t *testing.T) {
	app, context := namedProject(t)
	save := func(name, intent, item, base string, draft *desktop.ObservationDraft) desktop.SaveItemResult {
		return app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Item: item, BaseRevision: base,
			Draft: desktop.ItemDraft{Name: name, Observation: draft}, IntentID: intent})
	}
	// A new observation starts from the facade's defaults, one source for all.
	fresh := func() *desktop.ObservationDraft {
		started := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ObservationItem}})
		if started.Draft == nil || started.Draft.Observation == nil {
			t.Fatalf("a new observation: %+v", started)
		}
		started.Draft.Observation.Source.File.Path, started.Draft.Observation.Source.Extraction.RecordKey = "export.csv", importer.Locator{"appointment"}
		return started.Draft.Observation
	}
	first := save("Appointments", "first", "", "", fresh())
	second := save("Cancellations", "second", "", "", fresh())
	if first.Outcome != desktop.SavedOutcome || second.Outcome != desktop.SavedOutcome {
		t.Fatalf("two new observations over one source: %+v %+v", first, second)
	}
	source := func(ref desktop.ItemRef) observewindow.Source {
		opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: ref})
		if opened.Draft == nil || opened.Draft.Observation.Source.Observes != opened.Draft.Observation.Window.Source {
			t.Fatalf("open %s: %+v", ref.ID, opened)
		}
		return opened.Draft.Observation.Source.Observes
	}
	one, two := source(*first.Saved), source(*second.Saved)
	if one == two || one.Kind != two.Kind || one.Scope != two.Scope {
		t.Fatalf("the second observation reads the first's source: %+v %+v", one, two)
	}
	if retried := save("Cancellations", "second", "", "", fresh()); retried.Outcome != desktop.SavedOutcome || retried.Saved.ID != second.Saved.ID || source(*retried.Saved) != two {
		t.Fatalf("a retried save read another source: %+v", retried)
	}

	moved := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *second.Saved}).Draft.Observation
	moved.Source.Observes, moved.Window.Source = one, one
	refused := save("Cancellations", "move", second.Saved.ID, second.Saved.Revision, moved)
	if refused.Outcome != desktop.InvalidOutcome || len(refused.Problems) != 1 || refused.Problems[0].Field != "observation.source.source.scope" ||
		!strings.Contains(refused.Problems[0].Problem, "Appointments") {
		t.Fatalf("a saved observation moved onto another's source: %+v", refused)
	}
}
