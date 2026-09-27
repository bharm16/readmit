package desktop

import (
	"errors"
	"testing"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testlicense"
)

// An environment's target, send policy, reset plan and links are one
// revision. A save interrupted at any member, or after they verified, leaves
// the previous revision current with exactly its own members — seen by a
// new process, as after a crash — or, once every member verified, the whole
// new revision; never a target of one revision beside a policy or plan of
// another.
func TestAnEnvironmentSavePublishesTargetPolicyResetAndLinksAsOneRevision(t *testing.T) {
	policy := testlicense.New(t)
	draft := func(address string, full bool) ItemDraft {
		target := replay.Target{Schema: replay.TargetSchemaV3, TestEndpoint: true, Address: address, Transport: "plain",
			ConnectTimeout: "2s", MessageTimeout: "5s", MaxACKBytes: 65536, Classification: replay.Nonproduction}
		draft := ItemDraft{Name: "Lab", Environment: &target}
		if full {
			draft.SendPolicy = &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}}
			draft.ResetPlan = &fixturereset.Plan{Actions: []fixturereset.Action{{Operator: fixturereset.OperatorConfirms, Instructions: "Clear the store."}}}
			draft.Links = &EnvironmentLinks{ResetName: "Clear store", ActionNames: []string{"Clear"}}
		}
		return draft
	}
	for _, point := range []string{catalog.PointMember + "target", catalog.PointMember + "policy", catalog.PointMember + "reset",
		catalog.PointMember + "links", catalog.PointVerified} {
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
			first := app.SaveItem(SaveItemRequest{Context: context, Kind: EnvironmentItem, Draft: draft("127.0.0.1:2575", false), IntentID: "first"})
			if first.Outcome != SavedOutcome {
				t.Fatalf("%+v", first)
			}
			app.saveFault = func(at string) error {
				if at == point {
					return errors.New("crash")
				}
				return nil
			}
			interrupted := app.SaveItem(SaveItemRequest{Context: context, Kind: EnvironmentItem, Item: first.Saved.ID, BaseRevision: "1",
				Draft: draft("127.0.0.1:2576", true), IntentID: "second"})
			app.saveFault = nil
			if interrupted.Outcome != FailedOutcome {
				t.Fatalf("a save reported success past its crash: %+v", interrupted)
			}

			restarted := window()
			if reopened := restarted.OpenNamedProject(context.Project); reopened.State != Completed {
				t.Fatalf("reopen: %+v", reopened)
			}
			opened := restarted.OpenItemDraft(ItemRequest{Context: context, Ref: ItemRef{Kind: EnvironmentItem, ID: first.Saved.ID}})
			if opened.Draft == nil {
				t.Fatalf("the environment after the crash: %+v", opened)
			}
			current := opened.Draft
			whole := current.SendPolicy != nil && current.ResetPlan != nil && current.Links != nil && current.Environment.Address == "127.0.0.1:2576"
			previous := current.SendPolicy == nil && current.ResetPlan == nil && current.Links == nil && current.Environment.Address == "127.0.0.1:2575"
			switch {
			case point == catalog.PointVerified && (opened.Ref.Revision != "2" || !whole):
				t.Fatalf("a verified save was not completed whole: %+v %+v", opened.Ref, current)
			case point != catalog.PointVerified && (opened.Ref.Revision != "1" || !previous):
				t.Fatalf("a mixed or partial revision is current: %+v %+v", opened.Ref, current)
			}
		})
	}
}
