package desktop

import (
	"errors"
	"testing"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/testlicense"
)

func TestConnectedObservationInterruptedSaveNeverPublishesMixedSourceAndHorizon(t *testing.T) {
	for _, point := range []string{catalog.PointMember + "source", catalog.PointMember + "setup", catalog.PointMember + "interval", catalog.PointVerified} {
		t.Run(point, func(t *testing.T) {
			state := t.TempDir()
			policy := testlicense.New(t)
			window := func() *App {
				a := New(folderAnswer(t.TempDir()), ShellDocuments{Folder: state})
				if answer := a.SelectOperationPolicy(policy); answer.State != Completed {
					t.Fatal(answer)
				}
				return a
			}
			app := window()
			location := app.ChooseProjectLocation()
			created := app.CreateNamedProject(NewProjectRequest{Name: "Atomic FHIR", Location: location.Location})
			ctx := created.Context
			environment := app.SaveItem(SaveItemRequest{Context: ctx, Kind: EnvironmentItem, Draft: ItemDraft{Name: "FHIR QA", FHIR: &FHIRConnection{Schema: FHIRConnectionSchema, Version: "4.0.1", Base: "https://qa.invalid/fhir", ServerName: "qa.invalid", Classification: "unclassified", Authentication: "none"}}, IntentID: "environment"})
			if environment.Outcome != SavedOutcome {
				t.Fatal(environment)
			}
			draft := func(value string, horizon int64) ItemDraft {
				v := connectedVocabulary()
				setup := v.Observation
				search := v.Search
				search.Boundary = "reference-fhir-store"
				search.Criteria = []FHIRCriterion{{Parameter: "identifier", Type: "token", System: "urn:qa", Value: value}}
				search.Fields = []FHIRFieldProjection{{Name: "identity", Field: "resource-identity", Key: true}, {Name: "status", Field: "status"}}
				setup.FHIR = &search
				setup.Environment = environment.Saved.ID
				setup.BusinessKeys = []BusinessKeyMapping{{Field: "identity", Variable: "business-id"}}
				setup.Completion.HorizonMS = horizon
				return ItemDraft{Name: "Appointment state", Observation: &ObservationDraft{Connected: &setup}}
			}
			first := app.SaveItem(SaveItemRequest{Context: ctx, Kind: ObservationItem, Draft: draft("before", 30000), IntentID: "first"})
			if first.Outcome != SavedOutcome {
				t.Fatal(first)
			}
			app.saveFault = func(at string) error {
				if at == point {
					return errors.New("interrupted publication")
				}
				return nil
			}
			interrupted := app.SaveItem(SaveItemRequest{Context: ctx, Kind: ObservationItem, Item: first.Saved.ID, BaseRevision: first.Saved.Revision, Draft: draft("after", 40000), IntentID: "second"})
			app.saveFault = nil
			if interrupted.Outcome != FailedOutcome {
				t.Fatalf("interrupted save: %+v", interrupted)
			}
			restarted := window()
			if answer := restarted.OpenNamedProject(ctx.Project); answer.State != Completed {
				t.Fatal(answer)
			}
			opened := restarted.OpenItemDraft(ItemRequest{Context: ctx, Ref: ItemRef{Kind: ObservationItem, ID: first.Saved.ID}})
			if opened.State != Completed || opened.Draft == nil {
				t.Fatal(opened)
			}
			setup := opened.Draft.Observation.Connected
			if point == catalog.PointVerified {
				if opened.Ref.Revision != "2" || setup.FHIR.Criteria[0].Value != "after" || setup.Completion.HorizonMS != 40000 {
					t.Fatalf("verified revision not whole: %+v", setup)
				}
			} else if opened.Ref.Revision != "1" || setup.FHIR.Criteria[0].Value != "before" || setup.Completion.HorizonMS != 30000 {
				t.Fatalf("mixed source/horizon after crash: %+v", setup)
			}
		})
	}
}
