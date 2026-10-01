package desktop_test

// A named environment and its observation are edited, checked, collected,
// reset and removed as the saved objects they are. These tests drive the
// facade over real project folders: a new environment starts neutral with
// its transport unchosen, a save publishes every member in one revision, a
// check and a collection act only on the revision shown, a reset runs only
// what its review shows with each manual step confirmed, a credential's
// arguments are never answered, and a removal names what still uses the
// object.

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const emptyReceiverSnapshot = `{"schema":"readmit-observation/v1","profile":"readmit-siu-v1","session_id":"0123456789abcdef0123456789abcdef","mode":"fixed","processed":[],"consistent":true,"records":[]}`

// environmentDraft is a new environment as its editor fills it in.
func environmentDraft(address, classification, transport string) *replay.Target {
	target := replay.Target{Schema: replay.TargetSchemaV3, TestEndpoint: true, Address: address, Transport: transport,
		ConnectTimeout: "2s", MessageTimeout: "5s", MaxACKBytes: 65536, Classification: replay.Classification(classification)}
	return &target
}

func saveEnvironment(t *testing.T, app *desktop.App, context desktop.RequestContext, request desktop.SaveItemRequest) desktop.ItemRef {
	t.Helper()
	request.Context, request.Kind = context, desktop.EnvironmentItem
	saved := app.SaveItem(request)
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("save environment: %+v", saved)
	}
	return *saved.Saved
}

// approveTransport records the reviewed approval of a saved environment's
// transport and answers the revision that holds it.
func approveTransport(t *testing.T, app *desktop.App, context desktop.RequestContext, ref desktop.ItemRef) desktop.ItemRef {
	t.Helper()
	prepared := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ApproveTransportAction, Items: []desktop.ItemRef{ref}})
	if prepared.Review == nil || !prepared.Review.Ready {
		t.Fatalf("a transport approval review: %+v", prepared)
	}
	approved := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "approve-" + ref.ID})
	if approved.State != desktop.Completed || approved.Approved == nil {
		t.Fatalf("approving the transport: %+v", approved)
	}
	return *approved.Approved
}

func catalogRow(t *testing.T, app *desktop.App, context desktop.RequestContext, ref desktop.ItemRef) desktop.CatalogItem {
	t.Helper()
	opened := app.OpenItem(desktop.ItemRequest{Context: context, Ref: ref})
	if opened.Item == nil {
		t.Fatalf("open %s: %+v", ref.ID, opened)
	}
	return *opened.Item
}

// memberEntry is the project entry one member of an object's revision was
// saved as.
func memberEntry(t *testing.T, root string, ref desktop.ItemRef, role string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".readmit", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Items []struct {
			ID        string `json:"id"`
			Revisions []struct {
				Number  int `json:"number"`
				Members []struct {
					Role string `json:"role"`
					Path string `json:"path"`
				} `json:"members"`
			} `json:"revisions"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, item := range document.Items {
		if item.ID != ref.ID {
			continue
		}
		for _, revision := range item.Revisions {
			if ref.Revision != "" && ref.Revision != strconv.Itoa(revision.Number) {
				continue
			}
			for _, member := range revision.Members {
				if member.Role == role {
					return member.Path
				}
			}
		}
	}
	t.Fatalf("no %s member of %+v", role, ref)
	return ""
}

func TestANewEnvironmentDraftProjectsValidatedDefaultsAndLeavesTransportUnchosen(t *testing.T) {
	app, context := namedProject(t)
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.EnvironmentItem}})
	if opened.State != desktop.Completed || !opened.New || opened.Draft == nil || opened.Draft.Environment == nil {
		t.Fatalf("a new environment draft: %+v", opened)
	}
	target := opened.Draft.Environment
	if target.Transport != "" || target.Classification != replay.Unclassified || target.ConnectTimeout == "" || target.MessageTimeout == "" || target.MaxACKBytes == 0 {
		t.Fatalf("the new environment's defaults: %+v", target)
	}
	written := entries(t, context.Project)
	target.Address = "127.0.0.1:2575"
	invalid := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Draft: desktop.ItemDraft{Name: "Scheduling QA", Environment: target}, IntentID: "unchosen"})
	if invalid.Outcome != desktop.InvalidOutcome || len(invalid.Problems) != 1 || invalid.Problems[0].Field != "transport" || invalid.Problems[0].Problem != desktop.TransportProblem {
		t.Fatalf("a save with no transport chosen: %+v", invalid)
	}
	if now := entries(t, context.Project); !slices.Equal(now, written) {
		t.Fatalf("an invalid environment wrote: %v", now)
	}
	// Choosing a transport is all that was missing, and the classification
	// stays the neutral one it started with.
	target.Transport = "tls"
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Draft: desktop.ItemDraft{Name: "Scheduling QA", Environment: target}, IntentID: "chosen"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("a save with its transport chosen: %+v", saved)
	}
	if row := catalogRow(t, app, context, *saved.Saved); row.Summary.Environment == nil || row.Summary.Environment.Classification != "unclassified" ||
		row.Summary.Environment.LastCheckedAt != nil {
		t.Fatalf("a new environment reads as: %+v", row)
	}

	observation := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ObservationItem}})
	if !observation.New || observation.Draft == nil || observation.Draft.Observation == nil ||
		observation.Draft.Observation.Source.Observes != observation.Draft.Observation.Window.Source {
		t.Fatalf("a new observation draft: %+v", observation)
	}
}

// The name an environment's target records is the environment a run's
// prepared inputs bind, which a runner is configured for by its hub's
// environment ID: whatever the display name, it is one the runner accepts.
func TestAnEnvironmentsTargetNameIsAnEnvironmentARunnerAccepts(t *testing.T) {
	app, context := namedProject(t)
	for index, display := range []string{"Scheduling QA", "Lab_2.East", "  QA  (night) "} {
		saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, IntentID: "env-" + strconv.Itoa(index),
			Draft: desktop.ItemDraft{Name: display, Environment: environmentDraft("qa.example:2575", "nonproduction", "plain")}})
		if saved.Outcome != desktop.SavedOutcome || saved.Projection == nil || !runnerprotocol.ID(saved.Projection.Environment.Name) {
			t.Fatalf("%q was saved as a target no runner can be configured for: %+v", display, saved)
		}
	}
}

func TestSaveNeverApprovesATransportAndApprovalIsItsOwnReviewedAction(t *testing.T) {
	app, context := namedProject(t)
	request := desktop.SaveItemRequest{Draft: desktop.ItemDraft{Name: "Scheduling QA", Environment: environmentDraft("qa.example:2575", "nonproduction", "plain")}, IntentID: "first"}
	request.Draft.Environment.ApprovedTransport = true
	first := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Draft: request.Draft, IntentID: "first"})
	if first.Outcome != desktop.SavedOutcome || first.Projection == nil || first.Projection.Environment.ApprovedTransport || first.Projection.Environment.Name != "scheduling-qa" {
		t.Fatalf("a save of a plain transport to a host name approved it: %+v", first)
	}
	if summary := catalogRow(t, app, context, *first.Saved).Summary.Environment; summary.TransportApproved || !summary.ApprovalRequired {
		t.Fatalf("an unapproved environment reads as: %+v", summary)
	}
	// The same click again is the same environment; the name it was given is
	// not taken from itself.
	if again := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Draft: request.Draft, IntentID: "first"}); !again.Replayed || again.Saved.ID != first.Saved.ID {
		t.Fatalf("a repeated click: %+v", again)
	}
	second := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Draft: request.Draft, IntentID: "second"})
	if second.Outcome != desktop.SavedOutcome || second.Projection.Environment.Name != "scheduling-qa-2" {
		t.Fatalf("a second environment of the same name: %+v", second)
	}
	// Nothing reaches an address whose transport nobody approved.
	if checked := app.CheckEnvironment(desktop.ItemRequest{Context: context, Ref: *first.Saved}); checked.State != desktop.Failed || checked.Report != nil ||
		!strings.Contains(checked.Reason, "approve the transport") {
		t.Fatalf("a check of an unapproved transport: %+v", checked)
	}

	approve := desktop.PrepareActionRequest{Context: context, Action: desktop.ApproveTransportAction, Items: []desktop.ItemRef{*first.Saved}}
	prepared := app.PrepareAction(approve)
	review := prepared.Review
	if review == nil || !review.Ready || review.Consent != desktop.ApproveConsent || review.Transport == nil || review.Transport.Address != "qa.example:2575" ||
		review.Transport.Transport != "plain" || review.Transport.Classification != "nonproduction" {
		t.Fatalf("a transport approval review: %+v", prepared)
	}
	approved := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "approve-1"})
	if approved.State != desktop.Completed || approved.Approved == nil || approved.Approved.ID != first.Saved.ID || approved.Approved.Revision != "2" {
		t.Fatalf("approving the transport: %+v", approved)
	}
	if summary := catalogRow(t, app, context, *approved.Approved).Summary.Environment; !summary.TransportApproved {
		t.Fatalf("an approved environment reads as: %+v", summary)
	}
	if again := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ApproveTransportAction, Items: []desktop.ItemRef{*approved.Approved}}); again.Review == nil || again.Review.Ready {
		t.Fatalf("an approval of an approved transport: %+v", again)
	}

	// An edit keeps the name the environment was created with, and the
	// approval while the address, transport and TLS settings stay as they
	// were.
	edit := desktop.ItemDraft{Name: "Scheduling QA renamed", Environment: environmentDraft("qa.example:2575", "nonproduction", "plain")}
	edit.Environment.Name, edit.Environment.MessageTimeout = "something-else", "6s"
	edited := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Item: first.Saved.ID, BaseRevision: "2", Draft: edit, IntentID: "edit"})
	if edited.Outcome != desktop.SavedOutcome || edited.Projection.Environment.Name != "scheduling-qa" || !edited.Projection.Environment.ApprovedTransport {
		t.Fatalf("an edit that keeps the transport: %+v", edited)
	}
	// A review prepared before a change is stale, and a changed address
	// clears the approval.
	stale := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ApproveTransportAction, Items: []desktop.ItemRef{*second.Saved}})
	moved := desktop.ItemDraft{Name: "Scheduling QA renamed", Environment: environmentDraft("qa2.example:2575", "nonproduction", "plain")}
	cleared := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Item: first.Saved.ID, BaseRevision: "3", Draft: moved, IntentID: "move"})
	if cleared.Outcome != desktop.SavedOutcome || cleared.Projection.Environment.ApprovedTransport {
		t.Fatalf("a changed address kept its approval: %+v", cleared)
	}
	edit.Environment.Address = "qa3.example:2575"
	saveEnvironment(t, app, context, desktop.SaveItemRequest{Item: second.Saved.ID, BaseRevision: "1", Draft: edit, IntentID: "move-second"})
	if refused := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: stale.Review.Token, IntentID: "approve-2"}); refused.Outcome != desktop.ActionStale || refused.Approved != nil {
		t.Fatalf("approving a transport changed since its review: %+v", refused)
	}
}

// resetDraft is a reset of two actions: a manual step and a check that the
// receiver's ledger is empty.
func resetDraft() (*fixturereset.Plan, *desktop.EnvironmentLinks) {
	plan := &fixturereset.Plan{Actions: []fixturereset.Action{
		{Operator: fixturereset.OperatorConfirms, Instructions: "Stop the scheduling listener and clear its store."},
		{Operator: fixturereset.ObservationEmpty, Instructions: "The receiver's ledger is empty.", Observation: "ledger.json"},
	}}
	return plan, &desktop.EnvironmentLinks{ResetName: "Empty appointment store", ActionNames: []string{"Stop listener", "Ledger is empty"}}
}

func TestOpenItemDraftReturnsSavedValuesAndDuplicateKeepsTheOriginal(t *testing.T) {
	app, context := namedProject(t)
	observation := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem,
		Draft: desktop.ItemDraft{Name: "Appointments", Observation: observationDraft(t, "appointments")}, IntentID: "observation"})
	if observation.Outcome != desktop.SavedOutcome {
		t.Fatalf("%+v", observation)
	}
	plan, links := resetDraft()
	links.Observation = observation.Saved.ID
	policy := &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}}
	original := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "environment", Draft: desktop.ItemDraft{Name: "Scheduling QA",
		Environment: environmentDraft("127.0.0.1:2575", "nonproduction", "plain"), SendPolicy: policy, ResetPlan: plan, Links: links}})

	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: original})
	draft := opened.Draft
	if opened.State != desktop.Completed || opened.New || *opened.Ref != original || draft == nil || draft.Name != "Scheduling QA" ||
		draft.Environment.Address != "127.0.0.1:2575" || draft.SendPolicy == nil || !slices.Equal(draft.SendPolicy.ApprovedDestinations, []string{"127.0.0.1/32"}) ||
		draft.ResetPlan == nil || draft.ResetPlan.Environment != "scheduling-qa" || draft.ResetPlan.Actions[0].ID != "stop-listener" ||
		draft.ResetPlan.Actions[1].ID != "ledger-is-empty" || draft.ResetPlan.Actions[1].Authority != fixturereset.ReadDeclaredFile ||
		draft.Links == nil || draft.Links.Observation != observation.Saved.ID || draft.Links.ResetName != "Empty appointment store" {
		t.Fatalf("the saved environment's draft: %+v", opened)
	}
	summary := catalogRow(t, app, context, original).Summary.Environment
	if summary.Observation == nil || summary.Observation.ID != observation.Saved.ID || summary.ObservationName != "Appointments" || !summary.HasPolicy ||
		summary.ResetName != "Empty appointment store" || summary.ResetActions != 2 {
		t.Fatalf("the environment's summary: %+v", summary)
	}

	// A duplicate is the draft saved under no identity: a new environment
	// with a name of its own, and the original exactly as it was.
	draft.Name = "Scheduling QA copy"
	copied := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Draft: *draft, IntentID: "duplicate"})
	if copied.Outcome != desktop.SavedOutcome || copied.Saved.ID == original.ID || copied.Projection.Environment.Name != "scheduling-qa-copy" ||
		copied.Projection.ResetPlan.Environment != "scheduling-qa-copy" {
		t.Fatalf("a duplicate: %+v", copied)
	}
	if again := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: original}); *again.Ref != original || again.Draft.Environment.Name != "scheduling-qa" {
		t.Fatalf("duplicating changed the original: %+v", again)
	}
}

func TestCheckEnvironmentUsesTheSavedRevisionAndRetainsLastChecked(t *testing.T) {
	app, context := namedProject(t)
	endpoint := newCountingEndpoint(t, false)
	draft := desktop.ItemDraft{Name: "Local fixture", Environment: environmentDraft(endpoint.address, "nonproduction", "plain"),
		SendPolicy: &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}}}
	saved := saveEnvironment(t, app, context, desktop.SaveItemRequest{Draft: draft, IntentID: "first"})

	checked := app.CheckEnvironment(desktop.ItemRequest{Context: context, Ref: saved})
	if checked.Report == nil || checked.Decision == nil || !checked.Decision.PolicySelected || checked.CheckedAt == "" || *checked.Ref != saved || endpoint.accepted.Load() != 1 {
		t.Fatalf("a check of the saved environment: %+v %+v %+v %+v, %d connections", checked, checked.Ref, checked.Report, checked.Decision, endpoint.accepted.Load())
	}
	summary := catalogRow(t, app, context, saved).Summary.Environment
	if summary.LastCheckedAt == nil || *summary.LastCheckedAt != checked.CheckedAt || summary.LastCheckOutcome != checked.Report.Outcome || summary.LastCheckRevision != "1" {
		t.Fatalf("the retained check: %+v", summary)
	}

	// A later save makes the check one of an older revision, and a check of a
	// revision that is no longer current connects to nothing.
	draft.Environment.MessageTimeout = "6s"
	later := saveEnvironment(t, app, context, desktop.SaveItemRequest{Item: saved.ID, BaseRevision: "1", Draft: draft, IntentID: "second"})
	if stale := app.CheckEnvironment(desktop.ItemRequest{Context: context, Ref: saved}); stale.State != desktop.Failed || stale.Report != nil || endpoint.accepted.Load() != 1 {
		t.Fatalf("a check of a revision no longer current: %+v", stale)
	}
	row := catalogRow(t, app, context, later)
	if row.Ref.Revision != "2" || row.Summary.Environment.LastCheckRevision != "1" {
		t.Fatalf("the check is not shown as one of the earlier revision: %+v", row)
	}

	// A destination is decided under the environment's saved policy, without
	// connecting.
	allowed := app.CheckEnvironmentDestination(desktop.DestinationCheckRequest{Context: context, Ref: later, Address: "127.0.0.1:2575", Classification: "nonproduction"})
	refused := app.CheckEnvironmentDestination(desktop.DestinationCheckRequest{Context: context, Ref: later, Address: "10.1.2.3:2575", Classification: "nonproduction"})
	if allowed.Decision == nil || !allowed.Decision.Allowed || refused.Decision == nil || refused.Decision.Allowed || endpoint.accepted.Load() != 1 {
		t.Fatalf("destination checks: %+v %+v", allowed, refused)
	}
	// With no classification named, the check is decided under the
	// environment's own, never an assumed nonproduction one.
	unclassified := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "unclassified", Draft: desktop.ItemDraft{Name: "Unclassified",
		Environment: environmentDraft(endpoint.address, "unclassified", "plain"), SendPolicy: draft.SendPolicy}})
	if assumed := app.CheckEnvironmentDestination(desktop.DestinationCheckRequest{Context: context, Ref: unclassified, Address: "127.0.0.1:2575"}); assumed.Decision == nil ||
		assumed.Decision.Classification != "unclassified" {
		t.Fatalf("a destination check with no classification named: %+v %+v", assumed, assumed.Decision)
	}
}

func TestCredentialRowsNeverCarryArgumentsAndRemovalNamesDependents(t *testing.T) {
	app, context := namedProject(t)
	created := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: "lab-mllp", Purpose: secret.MLLPEndpoint, Store: secret.OSKeychain,
		Address: "127.0.0.1:2575", Command: "/usr/bin/security", Arguments: []string{"find-generic-password", "-s", "lab-mllp-arg"}})
	if created.State != desktop.Completed || len(created.Credentials) != 1 || created.Credentials[0].ArgumentCount != 3 || !created.Credentials[0].Bindable {
		t.Fatalf("a new credential: %+v", created)
	}
	answered, _ := json.Marshal(created)
	if strings.Contains(string(answered), "lab-mllp-arg") {
		t.Fatalf("a credential's arguments were answered: %s", answered)
	}
	// An edit that leaves the arguments alone keeps them; replacing them
	// with none clears them.
	kept := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: "lab-mllp", Update: true, Store: secret.CustomerManaged,
		Address: "127.0.0.1:2575", Command: "/usr/bin/security"})
	if kept.State != desktop.Completed || kept.Credentials[0].ArgumentCount != 3 || kept.Credentials[0].Store != secret.CustomerManaged {
		t.Fatalf("an edit that keeps the arguments: %+v", kept)
	}
	if renamed := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: "lab-mllp", Update: true, Purpose: secret.SourceEndpoint,
		Store: secret.CustomerManaged, Address: "127.0.0.1:2575", Command: "/usr/bin/security"}); renamed.State != desktop.Failed {
		t.Fatalf("an edit changed a reference's purpose: %+v", renamed)
	}
	cleared := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: "lab-mllp", Update: true, Store: secret.CustomerManaged,
		Address: "127.0.0.1:2575", Command: "/usr/bin/security", ReplaceArguments: true})
	if cleared.State != desktop.Completed || cleared.Credentials[0].ArgumentCount != 0 {
		t.Fatalf("replacing the arguments with none: %+v", cleared)
	}

	// A refused save names the member its refusal is about.
	for field, request := range map[string]desktop.CredentialSaveRequest{
		"name":    {Name: "lab mllp", Purpose: secret.MLLPEndpoint, Store: secret.OSKeychain, Address: "127.0.0.1:2575", Command: "/usr/bin/security"},
		"address": {Name: "lab-2", Purpose: secret.MLLPEndpoint, Store: secret.OSKeychain, Address: "127.0.0.1", Command: "/usr/bin/security"},
		"max_age": {Name: "lab-3", Purpose: secret.MLLPEndpoint, Store: secret.OSKeychain, Address: "127.0.0.1:2575", Command: "/usr/bin/security", MaxAge: "forever"},
		"command": {Name: "lab-4", Purpose: secret.MLLPEndpoint, Store: secret.OSKeychain, Address: "127.0.0.1:2575", Command: "security"},
	} {
		request.Context = context
		if refused := app.SaveCredential(request); refused.State != desktop.Failed || len(refused.Problems) != 1 || refused.Problems[0].Field != field {
			t.Errorf("a refusal at %s: %+v", field, refused)
		}
	}

	target := environmentDraft("127.0.0.1:2575", "nonproduction", "tls")
	target.Credential = replay.Credential{Reference: "lab-mllp"}
	environment := saveEnvironment(t, app, context, desktop.SaveItemRequest{Draft: desktop.ItemDraft{Name: "Secured lab", Environment: target}, IntentID: "secured"})
	refused := app.RemoveCredential(desktop.CredentialRequest{Context: context, Name: "lab-mllp"})
	if refused.State != desktop.Failed || len(refused.Referring) != 1 || refused.Referring[0].Ref.ID != environment.ID || refused.Referring[0].Name != "Secured lab" {
		t.Fatalf("removing a credential an environment presents: %+v", refused)
	}
	if listed := app.ListCredentials(desktop.ItemRequest{Context: context}); len(listed.Credentials) != 1 {
		t.Fatalf("a refused removal removed the credential: %+v", listed)
	}
	if removed := app.RemoveItem(desktop.ItemRequest{Context: context, Ref: environment}); removed.State != desktop.Completed {
		t.Fatalf("remove the environment: %+v", removed)
	}
	if removed := app.RemoveCredential(desktop.CredentialRequest{Context: context, Name: "lab-mllp"}); removed.State != desktop.Completed || len(removed.Credentials) != 0 {
		t.Fatalf("remove the credential: %+v", removed)
	}
}

func TestRemoveItemRefusesAReferencedEnvironment(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	environment := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "east",
		Draft: desktop.ItemDraft{Name: "East", Environment: environmentDraft("127.0.0.1:2575", "nonproduction", "plain")}})
	// A later revision is saved; the suite binds the first one's file.
	first := memberEntry(t, root, environment, "target")
	environment = saveEnvironment(t, app, context, desktop.SaveItemRequest{Item: environment.ID, BaseRevision: "1", IntentID: "east-2",
		Draft: desktop.ItemDraft{Name: "East", Environment: environmentDraft("127.0.0.1:2576", "nonproduction", "plain")}})
	writeDocument(t, root, "nightly.json", `{"schema":"readmit-suite/v1","id":"nightly","owner":"interop","tags":["siu"],"parallelism":1,"environments":[{"id":"east","site":"hospital-a","bindings":[{"parameter":"interface","target":"`+first+`"}]}],"tables":[{"id":"patients","rows":[{"id":"one","case":"case-one"}]}],"tests":[{"id":"booking","spec":"booking.json","owner":"scheduling","tags":["smoke"],"parameter":"interface","table":"patients","isolation":"shared","sequence":["s0001-e000001"]}]}`)
	refused := app.RemoveItem(desktop.ItemRequest{Context: context, Ref: environment})
	if refused.State != desktop.Failed || len(refused.Referring) != 1 || refused.Referring[0].Ref.Kind != desktop.SuiteItem {
		t.Fatalf("removing an environment a suite binds: %+v", refused)
	}
	if listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.EnvironmentItem}); listed.Page == nil || len(listed.Page.Items) != 1 {
		t.Fatalf("a refused removal removed the environment: %+v", listed)
	}
	if err := os.Remove(filepath.Join(root, "nightly.json")); err != nil {
		t.Fatal(err)
	}
	if removed := app.RemoveItem(desktop.ItemRequest{Context: context, Ref: environment}); removed.State != desktop.Completed {
		t.Fatalf("removing an environment nothing uses: %+v", removed)
	}
	if listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.EnvironmentItem}); listed.State != desktop.Empty {
		t.Fatalf("a removed environment is still listed: %+v", listed)
	}
	// Its files stay where they are.
	if _, err := os.Stat(filepath.Join(root, first)); err != nil {
		t.Fatalf("removing the environment removed its file: %v", err)
	}
}

func TestAReviewedCollectRefusesAChangedSourceAndNeverReportsZeroForIncomplete(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	writeDocument(t, root, "export.csv", "appointment,status\nA1,booked\n")
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem,
		Draft: desktop.ItemDraft{Name: "Appointments", Observation: observationDraft(t, "appointments")}, IntentID: "first"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("%+v", saved)
	}
	prepare := desktop.PrepareActionRequest{Context: context, Action: desktop.CollectObservationAction, Items: []desktop.ItemRef{*saved.Saved}}
	prepared := app.PrepareAction(prepare)
	review := prepared.Review
	if review == nil || !review.Ready || review.Consent != desktop.CollectConsent || review.Collect == nil || review.Collect.Destination != "export.csv" ||
		review.Collect.Source != "scheduling-archive" || review.Collect.Bounds.StableSamples != 2 || review.Destination.Output == "" {
		t.Fatalf("a collection review: %+v %+v", prepared, prepared.Review)
	}
	// The observation is saved again: the review is withdrawn and a fresh one
	// needs its own click.
	changed := observationDraft(t, "appointments")
	changed.Source.Freshness.MaxAge = "2h"
	edit := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Item: saved.Saved.ID, BaseRevision: "1",
		Draft: desktop.ItemDraft{Observation: changed}, IntentID: "second"})
	if edit.Outcome != desktop.SavedOutcome {
		t.Fatalf("%+v", edit)
	}
	stale := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "collect-1"})
	if stale.Outcome != desktop.ActionStale || stale.Collected != nil || slices.Contains(entries(t, root), review.Destination.Output) {
		t.Fatalf("a collection of a changed source: %+v", stale)
	}
	prepare.Items = []desktop.ItemRef{*edit.Saved}
	current := app.PrepareAction(prepare)
	collected := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: current.Review.Token, IntentID: "collect-2"})
	if collected.State != desktop.Completed || collected.Collected == nil || !collected.Collected.Trustworthy || collected.Collected.Records == nil || *collected.Collected.Records != 1 {
		t.Fatalf("a completed collection: %+v", collected)
	}

	// A source whose export is not there observed nothing: incomplete, with
	// its reason, and no count.
	missing := observationDraft(t, "cancellations")
	missing.Source.File.Path = "not-exported.csv"
	missing.Window.Completion.Deadline = "300ms"
	absent := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem,
		Draft: desktop.ItemDraft{Name: "Cancellations", Observation: missing}, IntentID: "missing"})
	reviewed := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.CollectObservationAction, Items: []desktop.ItemRef{*absent.Saved}})
	if reviewed.Review == nil || !reviewed.Review.Ready {
		t.Fatalf("%+v", reviewed)
	}
	incomplete := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: reviewed.Review.Token, IntentID: "collect-3"})
	if incomplete.State != desktop.Failed || incomplete.Collected == nil || incomplete.Collected.Trustworthy || incomplete.Collected.Records != nil ||
		incomplete.Collected.Status == "complete" || incomplete.Collected.Reason == "" {
		t.Fatalf("an incomplete collection: %+v", incomplete)
	}
	history := app.ObservationHistory(desktop.ItemRequest{Context: context, Ref: *absent.Saved})
	if history.State != desktop.Completed || len(history.Collections) != 1 || history.Collections[0].Records != nil || history.Collections[0].Entry != incomplete.Collected.Entry {
		t.Fatalf("the incomplete observation's history: %+v", history)
	}
	before := entries(t, root)
	inspected := app.InspectCompletion(desktop.CompletionRequest{Context: context, Ref: *absent.Saved, Entry: incomplete.Collected.Entry})
	if inspected.Completion == nil || inspected.Completion.Records != nil || inspected.Completion.Trustworthy || !inspected.Completion.Supported {
		t.Fatalf("inspecting an incomplete collection: %+v", inspected)
	}
	if after := entries(t, root); !slices.Equal(after, before) {
		t.Fatalf("inspecting a collection collected again: %v", after)
	}
	if other := app.InspectCompletion(desktop.CompletionRequest{Context: context, Ref: *absent.Saved, Entry: collected.Collected.Entry}); other.State != desktop.Failed {
		t.Fatalf("another observation's collection was inspected as this one's: %+v", other)
	}
}

func TestReceiverSnapshotsListNewestFirstWithWhenEachWasWritten(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	older, newer := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC), time.Date(2026, 9, 25, 17, 30, 0, 0, time.UTC)
	for name, at := range map[string]time.Time{"a-ledger.json": older, "b-ledger.json": newer} {
		writeDocument(t, root, name, emptyReceiverSnapshot)
		if err := os.Chtimes(filepath.Join(root, name), at, at); err != nil {
			t.Fatal(err)
		}
	}
	writeDocument(t, root, "notes.json", `{"schema":"readmit-observation/v1"}`)
	listed := app.ListReceiverSnapshots(desktop.ItemRequest{Context: context})
	if listed.State != desktop.Completed || len(listed.Snapshots) != 2 {
		t.Fatalf("the project's receiver snapshots: %+v", listed)
	}
	for i, want := range []struct{ entry, at string }{{"b-ledger.json", "2026-09-25T17:30:00Z"}, {"a-ledger.json", "2026-09-20T08:00:00Z"}} {
		if got := listed.Snapshots[i]; got.Entry != want.entry || got.CollectedAt == nil || *got.CollectedAt != want.at {
			t.Fatalf("snapshot %d: %+v, want %s at %s", i, got, want.entry, want.at)
		}
	}
}

func TestAReviewedResetRequiresEachManualConfirmationAndReportsPerActionOutcomes(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	writeDocument(t, root, "ledger.json", emptyReceiverSnapshot)
	if snapshots := app.ListReceiverSnapshots(desktop.ItemRequest{Context: context}); len(snapshots.Snapshots) != 1 || snapshots.Snapshots[0].Entry != "ledger.json" {
		t.Fatalf("the project's receiver snapshots: %+v", snapshots)
	}
	plan, links := resetDraft()
	environment := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "lab", Draft: desktop.ItemDraft{Name: "Lab",
		Environment: environmentDraft("127.0.0.1:2575", "nonproduction", "plain"), ResetPlan: plan, Links: links}})

	prepared := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ResetEnvironmentAction, Items: []desktop.ItemRef{environment}})
	review := prepared.Review
	if review == nil || !review.Ready || review.Reset == nil || len(review.Reset.Actions) != 2 || review.Reset.Name != "Empty appointment store" ||
		review.Reset.Actions[0].Name != "Stop listener" || review.Reset.Actions[0].Type != fixturereset.OperatorConfirms ||
		!strings.Contains(review.Reset.Actions[1].Effect, "ledger.json") || !slices.Contains(review.Requirements, desktop.ConfirmationsRequirement) {
		t.Fatalf("a reset review: %+v", prepared)
	}
	// Without its manual step confirmed nothing runs, and the review is kept.
	unconfirmed := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "reset-1"})
	if unconfirmed.State != desktop.Failed || unconfirmed.Reset != nil {
		t.Fatalf("a reset without its confirmation: %+v", unconfirmed)
	}
	manual := review.Reset.Actions[0].ID
	if extra := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "reset-2",
		Decisions: desktop.ReviewDecisions{Confirmed: []string{manual, review.Reset.Actions[1].ID}}}); extra.State != desktop.Failed || extra.Reset != nil {
		t.Fatalf("a reset confirming a machine action: %+v", extra)
	}
	reset := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "reset-3",
		Decisions: desktop.ReviewDecisions{Confirmed: []string{manual}}})
	if reset.State != desktop.Completed || reset.Outcome != desktop.ActionCompleted || reset.Reset == nil || len(reset.Reset.Result.Actions) != 2 {
		t.Fatalf("a confirmed reset: %+v", reset)
	}
	for _, action := range reset.Reset.Result.Actions {
		if action.Outcome != fixturereset.Confirmed {
			t.Fatalf("an action's own outcome: %+v", reset.Reset.Result.Actions)
		}
	}
	if _, err := os.Stat(filepath.Join(root, reset.Reset.Output)); err != nil {
		t.Fatalf("the reset's outcome was not retained: %v", err)
	}

	// A changed plan withdraws the review.
	again := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ResetEnvironmentAction, Items: []desktop.ItemRef{environment}})
	plan.Actions[0].Instructions = "Stop the listener."
	current := saveEnvironment(t, app, context, desktop.SaveItemRequest{Item: environment.ID, BaseRevision: "1", IntentID: "lab-2", Draft: desktop.ItemDraft{Name: "Lab",
		Environment: environmentDraft("127.0.0.1:2575", "nonproduction", "plain"), ResetPlan: plan, Links: links}})
	if stale := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: again.Review.Token, IntentID: "reset-4",
		Decisions: desktop.ReviewDecisions{Confirmed: []string{manual}}}); stale.Outcome != desktop.ActionStale || stale.Reset != nil {
		t.Fatalf("a reset of a changed plan: %+v", stale)
	}
	// So does a changed target.
	moved := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ResetEnvironmentAction, Items: []desktop.ItemRef{current}})
	saveEnvironment(t, app, context, desktop.SaveItemRequest{Item: environment.ID, BaseRevision: "2", IntentID: "lab-3", Draft: desktop.ItemDraft{Name: "Lab",
		Environment: environmentDraft("127.0.0.1:2576", "nonproduction", "plain"), ResetPlan: plan, Links: links}})
	if stale := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: moved.Review.Token, IntentID: "reset-5",
		Decisions: desktop.ReviewDecisions{Confirmed: []string{manual}}}); stale.Outcome != desktop.ActionStale || stale.Reset != nil {
		t.Fatalf("a reset of a changed target: %+v", stale)
	}

	// A production environment is never reset.
	production := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "production", Draft: desktop.ItemDraft{Name: "Production",
		Environment: environmentDraft("127.0.0.1:2575", "production", "plain"), ResetPlan: &fixturereset.Plan{Actions: plan.Actions[:1]}}})
	if refused := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ResetEnvironmentAction, Items: []desktop.ItemRef{production}}); refused.Review == nil || refused.Review.Ready || refused.Review.Token != "" {
		t.Fatalf("a production reset review: %+v", refused)
	}
}

func TestChooseEnvironmentFileKinds(t *testing.T) {
	c := &chooser{files: []string{"/etc/ssl/ca.pem"}}
	app := newApp(t, c)
	for _, kind := range []string{"ca-certificate", "client-certificate", "locator-program", "observation-input", "connection-example", "example-file"} {
		chosen := app.ChooseEnvironmentFile(kind)
		if chosen.State != desktop.Completed || chosen.Kind != kind || len(chosen.Paths) != 1 || chosen.Paths[0] != "/etc/ssl/ca.pem" {
			t.Fatalf("%s: %+v", kind, chosen)
		}
	}
	if unknown := app.ChooseEnvironmentFile("secrets"); unknown.State != desktop.Failed {
		t.Fatalf("an unknown kind: %+v", unknown)
	}
	c.files = nil
	if dismissed := app.ChooseEnvironmentFile("ca-certificate"); dismissed.State != desktop.Cancelled {
		t.Fatalf("dismissed: %+v", dismissed)
	}
}

func TestAnEnvironmentRefusalIsAnsweredAtTheFieldThatHoldsIt(t *testing.T) {
	app, context := namedProject(t)
	for field, change := range map[string]func(*replay.Target){
		"environment.max_ack_bytes":   func(target *replay.Target) { target.MaxACKBytes = 0 },
		"environment.connect_timeout": func(target *replay.Target) { target.ConnectTimeout = "" },
		"environment.message_timeout": func(target *replay.Target) { target.MessageTimeout = "10m" },
		"environment.address":         func(target *replay.Target) { target.Address = "qa.example" },
	} {
		target := environmentDraft("127.0.0.1:2575", "nonproduction", "plain")
		change(target)
		refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Draft: desktop.ItemDraft{Name: "Lab", Environment: target},
			IntentID: "refused-" + strings.ReplaceAll(field, ".", "-")})
		if refused.Outcome != desktop.InvalidOutcome || len(refused.Problems) != 1 || refused.Problems[0].Field != field {
			t.Errorf("a refusal at %s: %+v", field, refused.Problems)
		}
	}
}

func TestACredentialEditKeepsAChangeMadeElsewhereWhileItWasOpen(t *testing.T) {
	app, context := namedProject(t)
	if created := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: "lab-cli", Purpose: secret.MLLPEndpoint, Store: secret.OSKeychain,
		Address: "127.0.0.1:2577", Command: "/usr/bin/security"}); created.State != desktop.Completed {
		t.Fatalf("a new credential: %+v", created)
	}
	shown := &desktop.CredentialShown{Store: secret.OSKeychain, Address: "127.0.0.1:2577", Command: "/usr/bin/security"}
	// While the edit is open, the reference is re-pointed elsewhere.
	if moved := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: "lab-cli", Update: true, Store: secret.OSKeychain,
		Address: "127.0.0.1:2578", Command: "/usr/bin/security"}); moved.State != desktop.Completed {
		t.Fatalf("the change made elsewhere: %+v", moved)
	}
	// The edit changed only the maximum age; the address it still shows is
	// the one it opened with, not a change.
	saved := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: "lab-cli", Update: true, Store: secret.OSKeychain,
		Address: "127.0.0.1:2577", Command: "/usr/bin/security", MaxAge: "720h", Shown: shown})
	if saved.State != desktop.Completed || saved.Credentials[0].Address != "127.0.0.1:2578" || saved.Credentials[0].MaxAge != "720h" {
		t.Fatalf("the edit overwrote the change made elsewhere: %+v", saved)
	}
	// A member the person did change is written, whatever it was before.
	changed := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: "lab-cli", Update: true, Store: secret.OSKeychain,
		Address: "127.0.0.1:2579", Command: "/usr/bin/security", MaxAge: "720h", Shown: shown})
	if changed.State != desktop.Completed || changed.Credentials[0].Address != "127.0.0.1:2579" {
		t.Fatalf("an address the person changed: %+v", changed)
	}
}
