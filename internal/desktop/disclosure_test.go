package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/capability"
	"github.com/bharm16/readmit/internal/desktop"
)

// The window's privacy status is per-operation. Every deliberately
// configurable activity that reaches a destination names that destination,
// what data the activity carries, what must be configured and approved before
// it happens, and — from the facade — whether it is connected or offline
// right now. A blanket claim is exactly what this table replaces, so the
// whole shell description may not carry one again.
func TestPrivacyDisclosureNamesDestinationDataAndAuthorizationPerOperation(t *testing.T) {
	described := shell(t)
	if len(described.Privacy.Operations) == 0 {
		t.Fatal("the privacy status discloses no operations")
	}
	ids := make(map[string]bool, len(described.Privacy.Operations))
	for _, operation := range described.Privacy.Operations {
		ids[operation.ID] = true
		for _, member := range []struct{ name, value string }{
			{"activity", operation.Activity},
			{"destination", operation.Destination},
			{"data", operation.Data},
			{"authorization", operation.Authorization},
		} {
			if strings.TrimSpace(member.value) == "" {
				t.Errorf("operation %q names no %s", operation.ID, member.name)
			}
		}
	}
	for _, id := range []string{"run", "runner", "capture", "environment", "observe", "hub", "portal"} {
		if !ids[id] {
			t.Errorf("the privacy status discloses no %q operation, so its destinations are unstated", id)
		}
	}
	encoded, err := json.Marshal(described)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(encoded)), "no network request of any kind") {
		t.Error("the shell still carries the blanket no-network claim the per-operation table replaces")
	}
}

// The support guidance is derived from the checked capability ledger and the
// verified qualification state, not from what a screen can draw. Every
// cli/desktop row the ledger still has open appears here as still without a
// checked screen, in both directions: a row opened after this list was
// written fails it, and so does a stale entry for a row another delivery
// has since closed. The qualification refusals #35 and #75 own, the
// de-identification and external-equivalence declines, and the development
// preview status are named as not established rather than silently absent.
func TestSupportGuidanceMatchesTheCheckedCapabilityLedger(t *testing.T) {
	data, err := os.ReadFile("../../docs/capability-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := capability.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	var open []string
	for _, row := range ledger.Rows {
		if row.Disposed() || row.Implemented {
			continue
		}
		if row.Kind != capability.KindCLI && row.Kind != capability.KindDesktop {
			continue
		}
		if !slices.Contains(open, row.Action) {
			open = append(open, row.Action)
		}
	}
	slices.Sort(open)

	support := shell(t).Support
	if len(support.Unavailable) == 0 {
		t.Fatal("the shell claims every ledger row is delivered")
	}
	shown := append([]string(nil), support.Unavailable...)
	slices.Sort(shown)
	if !slices.Equal(open, shown) {
		t.Errorf("the support guidance disagrees with the checked ledger.\nledger rows still open: %v\nthe window shows: %v", open, shown)
	}
	notes := strings.ToLower(strings.Join(support.Notes, "\n"))
	for _, refusal := range []string{"#35", "#75", "de-identification", "external_equivalence", "unsigned"} {
		if !strings.Contains(notes, refusal) {
			t.Errorf("the support guidance does not state the %s refusal the qualification state actually holds", refusal)
		}
	}
}

// The disclosure status answers, without contacting anything, whether each
// deliberately configurable activity is connected, offline, configured or
// idle right now. A fresh window has selected nothing and started nothing.
func TestDisclosureStatusReportsConnectionStatesWithoutContactingAnything(t *testing.T) {
	app := newApp(t, &chooser{})
	result := app.DisclosureStatus()
	if result.State != desktop.Completed {
		t.Fatalf("disclosure status: %+v", result)
	}
	states := make(map[string]desktop.DisclosureState, len(result.States))
	for _, state := range result.States {
		states[state.ID] = state
	}
	for _, id := range []string{"run", "runner", "capture", "environment", "observe"} {
		state, ok := states[id]
		if !ok {
			t.Fatalf("disclosure status names no %q state: %+v", id, result.States)
		}
		if state.State != "idle" {
			t.Errorf("%s reports %q on a fresh window, want idle: %+v", id, state.State, state)
		}
	}
	if state := states["hub"]; state.State != "not-configured" {
		t.Errorf("hub reports %q with no configuration selected: %+v", state.State, state)
	}
	if state := states["portal"]; state.State != "not-configured" {
		t.Errorf("portal reports %q with no destinations file selected: %+v", state.State, state)
	}
}

// A selected hub configuration is offline, not unconfigured, and a selected
// commercial destinations file is configured: the states distinguish what a
// person has deliberately set up from what has never been set up at all.
func TestDisclosureStatusDistinguishesConfiguredFromUnconfigured(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "hub-client.json")
	cfg := `{"schema":"readmit-hub-client/v1","hub":"https://hub.example.test","ca":"` +
		filepath.Join(dir, "ca.pem") + `","certificate":"` + filepath.Join(dir, "cert.pem") +
		`","key":{"command":"` + filepath.Join(dir, "read-key") + `","arguments":[]},` +
		`"idp":{"issuer":"https://idp.example.test","client_id":"desktop-app","audience":"hub-aud",` +
		`"authorize_endpoint":"https://idp.example.test/auth","token_endpoint":"https://idp.example.test/token",` +
		`"scopes":["evidence.read"]},"projects":["cardio-study"]}`
	if err := os.WriteFile(config, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	destinations := filepath.Join(dir, "destinations.json")
	if err := os.WriteFile(destinations, []byte(`{"schema":"readmit-commercial-destinations/v1","environment":"sandbox","portal":"https://portal.example.test"}`), 0600); err != nil {
		t.Fatal(err)
	}
	app := desktop.New(&chooser{files: []string{destinations}}, filepath.Join(dir, "recent.json"), filepath.Join(dir, "filters.json"), filepath.Join(dir, "session.json"), filepath.Join(dir, "drafts.json"))
	if result := app.SelectHubConfig(config); result.State != desktop.Completed {
		t.Fatalf("hub configuration: %+v", result)
	}
	if result := app.ChooseCommercialDestinations(); result.State != desktop.Completed {
		t.Fatalf("commercial destinations: %+v", result)
	}
	result := app.DisclosureStatus()
	if result.State != desktop.Completed {
		t.Fatalf("disclosure status: %+v", result)
	}
	states := make(map[string]desktop.DisclosureState, len(result.States))
	for _, state := range result.States {
		states[state.ID] = state
	}
	if state := states["hub"]; state.State != "offline" {
		t.Errorf("hub reports %q with a configuration selected and no connection: %+v", state.State, state)
	}
	if state := states["portal"]; state.State != "configured" {
		t.Errorf("portal reports %q with a destinations file selected: %+v", state.State, state)
	}
}

// The disclosure status is a read of what is running, so it answers while an
// operation holds the slot and reports that operation active: an observation
// window collecting is disclosed as open, and idle again once it has stopped.
// Answering takes nothing from the operation — the slot stays held, and a
// second operation is still refused — and the collection it watched completes.
func TestDisclosureStatusReportsTheOperationHoldingTheSlotWithoutTakingIt(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()
	// A window whose completion rule keeps the collection open for a while.
	writeDocument(t, dir, "window.json", strings.Replace(strings.Replace(facadeWindowDocument,
		`"quiet_period": "10ms"`, `"quiet_period": "1s"`, 1), `"stable_samples": 2`, `"stable_samples": 3`, 1))
	writeDocument(t, dir, "source.json", facadeSourceDocument)
	writeDocument(t, dir, "export.csv", "appointment,status\nA1,booked\n")
	collected := make(chan desktop.ObservationCompletionResult, 1)
	go func() {
		collected <- app.CollectObservation(desktop.ObservationCollectFacadeRequest{
			Workspace: dir, SourceFile: "source.json", WindowFile: "window.json",
			OutputFile: "completion.json", SnapshotDir: "snapshot", Authorize: true,
		})
	}()
	observe := func() desktop.DisclosureState {
		result := app.DisclosureStatus()
		if result.State != desktop.Completed {
			t.Fatalf("disclosure status answered %+v while an operation ran", result)
		}
		for _, state := range result.States {
			if state.ID == "observe" {
				return state
			}
		}
		t.Fatalf("disclosure status names no observation state: %+v", result.States)
		return desktop.DisclosureState{}
	}
	deadline := time.Now().Add(5 * time.Second)
	for observe().State != "active" {
		if time.Now().After(deadline) {
			t.Fatal("the observation window never read as open while it collected")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The slot is still the collection's: another operation is refused.
	if opened := app.OpenWorkspace(dir); opened.State != desktop.Busy {
		t.Errorf("an operation started while the collection held the slot: %+v", opened)
	}
	result := <-collected
	if result.State != desktop.Completed || result.Summary == nil || result.Summary.Status != "complete" {
		t.Fatalf("the collection the disclosure watched: %+v", result)
	}
	if state := observe(); state.State != "idle" {
		t.Errorf("observation reads %q after the collection stopped: %+v", state.State, state)
	}
}

// An operation that holds the slot without a name cannot be attributed to any
// one activity — a connectivity check and a fixture reset are among them — so
// while one does, the answer is busy rather than an idle state the window
// cannot vouch for. Choosing a folder is such an operation.
func TestDisclosureStatusIsBusyWhileAnUnnamedOperationHoldsTheSlot(t *testing.T) {
	dialog := &chooser{folder: t.TempDir()}
	app := newApp(t, dialog)
	var during desktop.DisclosureStatusResult
	dialog.before = func() { during = app.DisclosureStatus() }
	app.SelectWorkspace()
	if during.State != desktop.Busy || len(during.States) != 0 {
		t.Fatalf("disclosure status while an unnamed operation held the slot: %+v", during)
	}
	if after := app.DisclosureStatus(); after.State != desktop.Completed {
		t.Fatalf("disclosure status once the slot was released: %+v", after)
	}
}
