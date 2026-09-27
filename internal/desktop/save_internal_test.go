package desktop

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/testlicense"
)

type folderAnswer string

func (f folderAnswer) ChooseFolder(string) (string, error) { return string(f), nil }

const faultWindow = `{"schema":"readmit-observation-window/v1","source":{"kind":"file-export","identity":"scheduling-archive","scope":"appointments"},` +
	`"watermark":{"kind":"none","position":""},"pre_existing_state":{"declaration":"declared-empty","baseline_identity":""},` +
	`"completion":{"deadline":"3s","quiet_period":"10ms","stable_samples":2,"max_records":100,"max_samples":32}}`

const faultSource = `{"schema":"readmit-observation-source/v1","source":{"kind":"file-export","identity":"scheduling-archive","scope":"appointments"},` +
	`"enabled":true,"freshness":{"max_age":"1h"},"extraction":{"envelope":"csv","encoding":"utf-8",` +
	`"csv":{"delimiter":",","record_separator":"lf","header":"present","fields":2},"record_key":["appointment"]},` +
	`"file":{"path":"export.csv","max_bytes":65536},"http":null}`

func faultDraft(t *testing.T, scope string) ItemDraft {
	t.Helper()
	var source observesource.Source
	var window observewindow.Window
	if err := json.Unmarshal([]byte(faultSource), &source); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(faultWindow), &window); err != nil {
		t.Fatal(err)
	}
	source.Observes.Scope, window.Source.Scope = scope, scope
	return ItemDraft{Observation: &ObservationDraft{Source: source, Window: window}}
}

// A save interrupted before, between or after its member writes, or after
// they verified but before the catalog named them, leaves the previous
// revision current with both of its members — seen by a new process, as
// after a crash. Recovery completes a save whose every member verified and
// reports any other as incomplete work, which the same click then finishes.
func TestAnInterruptedSaveNeverExposesAMixedObservation(t *testing.T) {
	policy := testlicense.New(t)
	for _, point := range []string{catalog.PointPending, catalog.PointMember + "source", catalog.PointMember + "window", catalog.PointVerified, catalog.PointPublished} {
		t.Run(point, func(t *testing.T) {
			state := t.TempDir()
			window := func() *App {
				app := New(folderAnswer(t.TempDir()), ShellDocuments{Folder: state})
				if selected := app.SelectOperationPolicy(policy); selected.State != Completed {
					t.Fatal(selected)
				}
				return app
			}
			app := window()
			chosen := app.ChooseProjectLocation()
			created := app.CreateNamedProject(NewProjectRequest{Name: "Faults", Location: chosen.Location})
			if created.State != Completed {
				t.Fatalf("%+v", created)
			}
			context := created.Context
			first := app.SaveItem(SaveItemRequest{Context: context, Kind: ObservationItem, Draft: faultDraft(t, "appointments"), IntentID: "first"})
			if first.Outcome != SavedOutcome {
				t.Fatalf("%+v", first)
			}
			crash := errors.New("crash")
			app.saveFault = func(at string) error {
				if at == point {
					return crash
				}
				return nil
			}
			interrupted := app.SaveItem(SaveItemRequest{Context: context, Kind: ObservationItem, Item: first.Saved.ID, BaseRevision: "1",
				Draft: faultDraft(t, "cancellations"), IntentID: "second"})
			app.saveFault = nil
			if point != catalog.PointPublished && interrupted.Outcome != FailedOutcome {
				t.Fatalf("a save reported success past its crash: %+v", interrupted)
			}

			// A new process reads the project.
			restarted := window()
			// Reading reports interrupted work and settles nothing; opening
			// the project for writing settles it.
			before := restarted.ListCatalog(CatalogQuery{Context: context, Kind: ObservationItem})
			if before.Page == nil || before.Page.Items[0].Ref.Revision != "1" && point != catalog.PointPublished {
				t.Fatalf("a read settled an interrupted save: %+v", before)
			}
			// Every save that left a pending record behind unpublished is
			// reported by the read, under its operation.
			reported := len(before.Page.Incomplete) == 1 && before.Page.Incomplete[0].Operation == "second"
			if point != catalog.PointPending && point != catalog.PointPublished && !reported {
				t.Fatalf("a read did not report the interrupted save: %+v", before.Page.Incomplete)
			}
			if reopened := restarted.OpenNamedProject(context.Project); reopened.State != Completed {
				t.Fatalf("reopen: %+v", reopened)
			}
			listing := restarted.ListCatalog(CatalogQuery{Context: context, Kind: ObservationItem})
			if listing.Page == nil || len(listing.Page.Items) != 1 {
				t.Fatalf("listing: %+v", listing)
			}
			current := listing.Page.Items[0]
			scope := scopeOf(t, context.Project, current)
			completed := point == catalog.PointVerified || point == catalog.PointPublished
			switch {
			case completed && (current.Ref.Revision != "2" || scope["source"] != "cancellations" || scope["window"] != "cancellations" || len(listing.Page.Incomplete) != 0):
				t.Fatalf("a verified save was not completed whole: %+v %v", listing.Page, scope)
			case !completed && (current.Ref.Revision != "1" || scope["source"] != "appointments" || scope["window"] != "appointments"):
				t.Fatalf("a mixed or partial revision is current: %+v %v", current, scope)
			}
			if point == catalog.PointMember+"source" || point == catalog.PointMember+"window" {
				if len(listing.Page.Incomplete) != 1 || listing.Page.Incomplete[0].Operation != "second" || listing.Page.Incomplete[0].Item == nil {
					t.Fatalf("interrupted work is not reported: %+v", listing.Page.Incomplete)
				}
				retried := restarted.SaveItem(SaveItemRequest{Context: context, Kind: ObservationItem, Item: first.Saved.ID, BaseRevision: "1",
					Draft: faultDraft(t, "cancellations"), IntentID: "second"})
				if retried.Outcome != SavedOutcome || retried.Saved.Revision != "2" {
					t.Fatalf("the same click did not finish the save: %+v", retried)
				}
			}
		})
	}
}

// scopeOf reads the scope each member of an observation's current revision
// declares, from the files on disk.
func scopeOf(t *testing.T, root string, item CatalogItem) map[string]string {
	t.Helper()
	store, err := catalog.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	scopes := map[string]string{}
	for _, member := range document.Items[document.Find(item.Ref.ID)].Current().Members {
		data, err := os.ReadFile(filepath.Join(root, member.Path))
		if err != nil {
			t.Fatal(err)
		}
		var declared struct {
			Source struct {
				Scope string `json:"scope"`
			} `json:"source"`
		}
		if err := json.Unmarshal(data, &declared); err != nil {
			t.Fatal(err)
		}
		scopes[member.Role] = declared.Source.Scope
	}
	return scopes
}

// faultIncident is a registered case of a booking, its acknowledgement and
// its reschedule in the project at root, and a plan keeping the reschedule.
func faultIncident(t *testing.T, root string) reproducer.Plan {
	t.Helper()
	frame := func(message string) string { return "\x0b" + message + "\x1c\r" }
	wire := frame("MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-1|P|2.5.1\rSCH|PLACER-1^READMIT|FILLER-1^READMIT||||CHECKUP|ROUTINE\rPID|1||MRN-1^^^READMIT^MR||DOE^JANE\r") +
		frame("MSH|^~\\&|RECV|LAB|READMIT|TEST|20260101120001||ACK^S12|CTL-2|P|2.5.1\rMSA|AA|CTL-1\r") +
		frame("MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120100||SIU^S13|CTL-3|P|2.5.1\rSCH|PLACER-1^READMIT|FILLER-1^READMIT||||CHECKUP|ROUTINE\rPID|1||MRN-1^^^READMIT^MR||DOE^JANE\r")
	imported := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	written, err := bundle.Write(filepath.Join(root, "incident"), []bundle.Input{{Path: "fixture", Data: []byte(wire),
		Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operation.RegisterCase(root, "incident", operation.CaseRegistration{Title: "Original incident"}); err != nil {
		t.Fatal(err)
	}
	plan, err := reproducer.NewPlan(written.Identity)
	if err != nil {
		t.Fatal(err)
	}
	plan.Steps = append(plan.Steps, reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: "s0001-e000003"})
	return plan
}

// A variant save interrupted anywhere — before its plan is staged, before its
// derived case is built, once it verified, once it is published as an entry,
// once it is registered, or once the catalog names it — never leaves a derived
// case the project lists without its association, seen by a new process as
// after a crash. Recovery completes a save whose case verified, registering it
// once as a revision of its source, or reports it incomplete with its staging
// removed; the same click then saves it, once. The source is never touched.
func TestAnInterruptedVariantSaveNeverPublishesAnUnassociatedCase(t *testing.T) {
	policy := testlicense.New(t)
	points := []string{catalog.PointPending, catalog.PointMember + "plan", catalog.PointMember + catalog.EntryRole, catalog.PointVerified,
		catalog.PointRenamed, catalog.PointAssociated, catalog.PointPublished, "damaged staging"}
	for _, point := range points {
		t.Run(point, func(t *testing.T) {
			state := t.TempDir()
			window := func() *App {
				app := New(folderAnswer(t.TempDir()), ShellDocuments{Folder: state})
				if selected := app.SelectOperationPolicy(policy); selected.State != Completed {
					t.Fatal(selected)
				}
				return app
			}
			app := window()
			chosen := app.ChooseProjectLocation()
			created := app.CreateNamedProject(NewProjectRequest{Name: "Variant faults", Location: chosen.Location})
			if created.State != Completed {
				t.Fatalf("%+v", created)
			}
			context := created.Context
			root := context.Project
			plan := faultIncident(t, root)
			source := app.ListCatalog(CatalogQuery{Context: context, Kind: CaseItem})
			if source.Page == nil || len(source.Page.Items) != 1 {
				t.Fatalf("the source: %+v", source)
			}
			incident := source.Page.Items[0].Ref
			before := evidenceUnder(t, filepath.Join(root, "incident"))
			request := SaveItemRequest{Context: context, Kind: VariantItem, IntentID: "variant-click",
				Draft: ItemDraft{Name: "Reschedule only", Variant: &VariantDraft{Source: incident, Plan: plan}}}
			crashAt := point
			if point == "damaged staging" {
				crashAt = catalog.PointVerified
			}
			app.saveFault = func(at string) error {
				if at == crashAt {
					return errors.New("crash")
				}
				return nil
			}
			interrupted := app.SaveItem(request)
			app.saveFault = nil
			if point != catalog.PointPublished && interrupted.Outcome != FailedOutcome {
				t.Fatalf("a save reported success past its crash: %+v", interrupted)
			}
			staging := filepath.Join(root, catalog.Folder, "staging")
			if point == "damaged staging" {
				// The verified case is damaged before anything publishes it.
				held, _ := os.ReadDir(staging)
				if len(held) != 1 {
					t.Fatalf("staging holds %d folders", len(held))
				}
				damaged := filepath.Join(staging, held[0].Name())
				if err := os.WriteFile(filepath.Join(damaged, "manifest.json"), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// A new process reads the project: no variant is listed until its
			// association is recorded and the catalog names it.
			restarted := window()
			variants := func() []CatalogItem {
				listing := restarted.ListCatalog(CatalogQuery{Context: context, Kind: VariantItem})
				if listing.Page == nil {
					t.Fatalf("listing: %+v", listing)
				}
				for _, item := range listing.Page.Items {
					if item.Summary.Variant == nil || item.Summary.Variant.Form != "revision" || item.Summary.Variant.Parent == nil || item.Summary.Variant.Parent.ID != incident.ID {
						t.Fatalf("a variant listed without its association: %+v", item)
					}
				}
				return listing.Page.Items
			}
			if listed := variants(); len(listed) != 0 && point != catalog.PointPublished {
				t.Fatalf("a read listed an unpublished variant: %+v", listed)
			}
			if reopened := restarted.OpenNamedProject(root); reopened.State != Completed {
				t.Fatalf("reopen: %+v", reopened)
			}
			completed := point == catalog.PointVerified || point == catalog.PointRenamed || point == catalog.PointAssociated || point == catalog.PointPublished
			listed := variants()
			revisions, err := project.ReadRevisions(root)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case completed && (len(listed) != 1 || listed[0].Name != "Reschedule only" || len(revisions.Revisions) != 1 ||
				revisions.Revisions[0].Operation.ParentIdentity != plan.Case || revisions.Revisions[0].Name != "variant-001"):
				t.Fatalf("a verified save was not completed whole: %+v %+v", listed, revisions)
			case !completed && (len(listed) != 0 || len(revisions.Revisions) != 0):
				t.Fatalf("an unverified save published: %+v %+v", listed, revisions)
			}
			if held, _ := os.ReadDir(staging); len(held) != 0 {
				t.Fatalf("recovery left staging behind: %v", held)
			}
			if after := evidenceUnder(t, filepath.Join(root, "incident")); !maps.EqualFunc(after, before, bytes.Equal) {
				t.Fatal("the source case changed")
			}
			// The same click finishes the save, once.
			retried := restarted.SaveItem(request)
			if retried.Outcome != SavedOutcome || retried.Saved == nil {
				t.Fatalf("the same click did not finish the save: %+v", retried)
			}
			if again := variants(); len(again) != 1 || again[0].Ref.ID != retried.Saved.ID {
				t.Fatalf("the retried save: %+v", again)
			}
			if revisions, _ := project.ReadRevisions(root); len(revisions.Revisions) != 1 {
				t.Fatalf("the variant was registered %d times", len(revisions.Revisions))
			}
		})
	}
}

// evidenceUnder is every file under dir, by relative path.
func evidenceUnder(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		files[path] = data
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// A profile save interrupted anywhere leaves the previous version current
// with its own seal, seen by a new process as after a crash: a profile is
// never current without the seal of exactly its content. Recovery completes
// a save whose profile and seal both verified, and the same click finishes
// any other.
func TestAnInterruptedSaveNeverPublishesAProfileWithoutItsSeal(t *testing.T) {
	policy := testlicense.New(t)
	data, err := os.ReadFile("../../testdata/fixtures/local-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	first, err := localprofile.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.Identity.Version = "2"
	second.Segments = slices.Clone(first.Segments)
	second.Segments[0].Description = "Edited in version 2"
	for _, point := range []string{catalog.PointPending, catalog.PointMember + profileRole, catalog.PointMember + sealRole, catalog.PointVerified, catalog.PointPublished} {
		t.Run(point, func(t *testing.T) {
			state := t.TempDir()
			window := func() *App {
				app := New(folderAnswer(t.TempDir()), ShellDocuments{Folder: state})
				if selected := app.SelectOperationPolicy(policy); selected.State != Completed {
					t.Fatal(selected)
				}
				return app
			}
			app := window()
			chosen := app.ChooseProjectLocation()
			created := app.CreateNamedProject(NewProjectRequest{Name: "Profile faults", Location: chosen.Location})
			if created.State != Completed {
				t.Fatalf("%+v", created)
			}
			context := created.Context
			saved := app.SaveItem(SaveItemRequest{Context: context, Kind: ProfileItem, IntentID: "first",
				Draft: ItemDraft{Name: "Scheduling profile", Profile: &ProfileDraft{Profile: first}}})
			if saved.Outcome != SavedOutcome {
				t.Fatalf("%+v", saved)
			}
			app.saveFault = func(at string) error {
				if at == point {
					return errors.New("crash")
				}
				return nil
			}
			request := SaveItemRequest{Context: context, Kind: ProfileItem, Item: saved.Saved.ID, BaseRevision: "1", IntentID: "second",
				Draft: ItemDraft{Name: "Scheduling profile", Profile: &ProfileDraft{Profile: second}}}
			interrupted := app.SaveItem(request)
			app.saveFault = nil
			if point != catalog.PointPublished && interrupted.Outcome != FailedOutcome {
				t.Fatalf("a save reported success past its crash: %+v", interrupted)
			}

			restarted := window()
			current := func() (string, string) {
				t.Helper()
				store, err := catalog.Open(context.Project)
				if err != nil {
					t.Fatal(err)
				}
				document, _, err := store.Read()
				if err != nil {
					t.Fatal(err)
				}
				item := document.Items[document.Find(saved.Saved.ID)]
				files := map[string]string{}
				for _, member := range item.Current().Members {
					files[member.Role] = store.Path(member)
				}
				// The current revision is always a profile and the seal of it.
				if err := verifyProfile(files); err != nil {
					t.Fatalf("the current revision is not a sealed profile: %v", err)
				}
				profile, _, err := readLocalProfile(files)
				if err != nil {
					t.Fatal(err)
				}
				return item.RevisionLabel(), profile.Identity.Version
			}
			if revision, version := current(); point != catalog.PointPublished && (revision != "1" || version != "1") {
				t.Fatalf("a read shows revision %s at version %s", revision, version)
			}
			if reopened := restarted.OpenNamedProject(context.Project); reopened.State != Completed {
				t.Fatalf("reopen: %+v", reopened)
			}
			completed := point == catalog.PointVerified || point == catalog.PointPublished
			revision, version := current()
			switch {
			case completed && (revision != "2" || version != "2"):
				t.Fatalf("a verified save was not completed whole: revision %s at version %s", revision, version)
			case !completed && (revision != "1" || version != "1"):
				t.Fatalf("a partial profile is current: revision %s at version %s", revision, version)
			}
			if retried := restarted.SaveItem(request); retried.Outcome != SavedOutcome || retried.Saved.Revision != "2" {
				t.Fatalf("the same click did not finish the save: %+v", retried)
			}
		})
	}
}

// A save refused before it read its draft, busy included, still answers an
// empty list of problems rather than none: the window reads the list.
func TestARefusedSaveAnswersAnEmptyListOfProblems(t *testing.T) {
	var answer SaveItemResult
	answer.refuse(Busy, "another operation is already running")
	encoded, err := json.Marshal(answer)
	if err != nil || !bytes.Contains(encoded, []byte(`"problems":[]`)) {
		t.Fatalf("a busy save answered %s %v", encoded, err)
	}
}
