package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
	for _, id := range []string{"run", "runner", "capture", "observe", "hub", "portal"} {
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
	for _, id := range []string{"run", "runner", "capture", "observe"} {
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

// The disclosure status answers through the operation slot like every other
// read, so a window in the middle of an operation is never told a state from
// halfway through it: it refuses busy instead of guessing.
func TestDisclosureStatusRefusesWhileAnOperationHoldsTheSlot(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	held := make(chan struct{})
	release := make(chan struct{})
	c := &chooser{
		folder: workspace,
		before: func() {
			close(held)
			<-release
		},
	}
	app := desktop.New(c, filepath.Join(dir, "recent.json"), filepath.Join(dir, "filters.json"), filepath.Join(dir, "session.json"), filepath.Join(dir, "drafts.json"))
	done := make(chan desktop.WorkspaceResult, 1)
	go func() { done <- app.SelectWorkspace() }()
	<-held
	if result := app.DisclosureStatus(); result.State != desktop.Busy {
		t.Errorf("disclosure status answered %v while an operation held the slot", result.State)
	}
	close(release)
	opened := <-done
	if opened.Workspace == nil || (opened.State != desktop.Completed && opened.State != desktop.Empty) {
		t.Fatalf("workspace open after the disclosure read: %+v", opened)
	}
}
