package desktop

import (
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
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
			created := app.CreateNamedProject(NewProjectRequest{Name: "Faults"})
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
