package desktop_test

// The customer hub configuration a person selects is remembered as
// readmit-desktop-hub-selection/v1 beside the operation selection, and the
// next window restores it by reading two local files: the selection and the
// configuration it names. Restoring reaches nothing, so a restored
// configuration is selected and offline and connecting stays the person's own
// act. A remembered selection that can no longer be restored is shown with
// why, never dropped, until a configuration is selected again (#335).

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

// jsonString is value as the JSON string a selection document holds.
func jsonString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func hubDisclosure(t *testing.T, app *desktop.App) string {
	t.Helper()
	for _, state := range app.DisclosureStatus().States {
		if state.ID == "hub" {
			return state.State
		}
	}
	t.Fatal("the privacy status discloses no hub")
	return ""
}

func TestTheHubSelectionIsRememberedAndRestoredWithoutReachingTheHub(t *testing.T) {
	hub := newCountingEndpoint(t, false)
	selection := filepath.Join(t.TempDir(), "operations.json")
	config := writeHubClientConfig(t, t.TempDir(), "https://"+hub.address)

	// Nothing is remembered before a configuration is selected.
	first := freshApp(t, &queueChooser{}, selection)
	if status := first.HubStatus(); status.State != desktop.Empty || status.ConfigPath != "" {
		t.Fatalf("a first window has a hub configuration: %+v", status)
	}
	if result := first.SelectHubConfig(config); result.State != desktop.Completed || result.Connected {
		t.Fatalf("select: %+v", result)
	}
	// The remembered selection is the v1 document, byte for byte: its two
	// members and nothing else.
	want := `{"schema":"readmit-desktop-hub-selection/v1","config":` + jsonString(t, config) + "}\n"
	remembered := filepath.Join(filepath.Dir(selection), "hub.json")
	if got := string(mustRead(t, remembered)); got != want {
		t.Fatalf("the remembered selection is %q, want %q", got, want)
	}

	// The next window is configured and offline: not connected, not signed
	// in, and the hub has not been reached.
	restored := freshApp(t, &queueChooser{}, selection)
	status := restored.HubStatus()
	if status.State != desktop.Completed || status.ConfigPath != config || status.HubURL != "https://"+hub.address || status.Reason != "" {
		t.Fatalf("the remembered hub configuration was not restored: %+v", status)
	}
	if status.Connected || status.Authenticated || len(status.Projects) != 0 {
		t.Fatalf("restoring the hub selection connected or signed in: %+v", status)
	}
	if state := hubDisclosure(t, restored); state != "offline" {
		t.Fatalf("the privacy status reads the restored hub as %q, want offline", state)
	}
	if got := string(mustRead(t, remembered)); got != want {
		t.Fatalf("restoring rewrote the remembered selection: %q", got)
	}
	if n := hub.accepted.Load(); n != 0 {
		t.Fatalf("the hub received %d connections; selecting and restoring reach nothing", n)
	}
}

func TestARememberedHubSelectionThatNoLongerValidatesIsShownUntilChosenAgain(t *testing.T) {
	hub := newCountingEndpoint(t, false)
	const (
		vanished   = "the remembered hub configuration is no longer there; choose a hub configuration again"
		invalid    = "the remembered hub configuration no longer validates ("
		unreadable = "the remembered hub selection cannot be read; choose a hub configuration again"
		reselect   = "; choose a hub configuration again"
	)
	selectionDocument := func(member string) string {
		return `{"schema":"readmit-desktop-hub-selection/v1",` + member + `}` + "\n"
	}
	for _, c := range []struct {
		name string
		// spoil changes what the first window remembered or what it names.
		spoil func(t *testing.T, remembered, config string)
		// named reports whether the configuration the selection names is
		// still shown beside the reason.
		named  bool
		reason string
	}{
		{"the configuration was removed", func(t *testing.T, _, config string) {
			if err := os.Remove(config); err != nil {
				t.Fatal(err)
			}
		}, true, vanished},
		{"the configuration names an address that is not https", func(t *testing.T, _, config string) {
			data := strings.Replace(string(mustRead(t, config)), `"hub":"https://`, `"hub":"http://`, 1)
			writeDocument(t, filepath.Dir(config), filepath.Base(config), data)
		}, true, invalid},
		{"the configuration has an unknown member", func(t *testing.T, _, config string) {
			data := strings.Replace(string(mustRead(t, config)), `{`, `{"retry":true,`, 1)
			writeDocument(t, filepath.Dir(config), filepath.Base(config), data)
		}, true, invalid},
		{"the selection has an unknown member", func(t *testing.T, remembered, config string) {
			writeDocument(t, filepath.Dir(remembered), "hub.json", selectionDocument(`"config":`+jsonString(t, config)+`,"connect":true`))
		}, false, unreadable},
		{"the selection declares another version", func(t *testing.T, remembered, config string) {
			writeDocument(t, filepath.Dir(remembered), "hub.json", `{"schema":"readmit-desktop-hub-selection/v2","config":`+jsonString(t, config)+"}\n")
		}, false, unreadable},
		{"the selection names a relative configuration", func(t *testing.T, remembered, _ string) {
			writeDocument(t, filepath.Dir(remembered), "hub.json", selectionDocument(`"config":"hub-client.json"`))
		}, false, unreadable},
		{"the selection names no configuration", func(t *testing.T, remembered, _ string) {
			writeDocument(t, filepath.Dir(remembered), "hub.json", `{"schema":"readmit-desktop-hub-selection/v1"}`+"\n")
		}, false, unreadable},
		{"the selection is not JSON", func(t *testing.T, remembered, _ string) {
			writeDocument(t, filepath.Dir(remembered), "hub.json", "hub-client.json\n")
		}, false, unreadable},
	} {
		t.Run(c.name, func(t *testing.T) {
			selection := filepath.Join(t.TempDir(), "operations.json")
			remembered := filepath.Join(filepath.Dir(selection), "hub.json")
			config := writeHubClientConfig(t, t.TempDir(), "https://"+hub.address)
			if result := freshApp(t, &queueChooser{}, selection).SelectHubConfig(config); result.State != desktop.Completed {
				t.Fatalf("select: %+v", result)
			}
			c.spoil(t, remembered, config)
			before := mustRead(t, remembered)

			restored := freshApp(t, &queueChooser{}, selection)
			status := restored.HubStatus()
			if status.State != desktop.Failed || !strings.HasPrefix(status.Reason, c.reason) || !strings.HasSuffix(status.Reason, reselect) {
				t.Fatalf("the remembered selection was not shown as recoverable: %+v", status)
			}
			if c.reason == invalid && strings.HasPrefix(status.Reason, invalid+")") {
				t.Fatalf("the reason does not say why the configuration no longer validates: %q", status.Reason)
			}
			shown := ""
			if c.named {
				shown = config
			}
			if status.ConfigPath != shown {
				t.Fatalf("the configuration shown beside the reason is %q, want %q", status.ConfigPath, shown)
			}
			if status.Connected || status.Authenticated || status.HubURL != "" {
				t.Fatalf("a selection that was not restored is reported usable: %+v", status)
			}
			// It is not dropped: the remembered document is left as it was, and
			// the reason stays until a configuration is chosen.
			if after := mustRead(t, remembered); string(after) != string(before) {
				t.Fatalf("restoring changed the remembered selection from %q to %q", before, after)
			}
			if again := restored.HubStatus(); again.State != desktop.Failed || again.Reason != status.Reason {
				t.Fatalf("the reason did not last: %+v", again)
			}
			// Every hub operation stays unavailable, and none reaches anything.
			if state := hubDisclosure(t, restored); state != "not-configured" {
				t.Fatalf("the privacy status reads the unrestored hub as %q", state)
			}
			if result := restored.DiagnoseHub(); result.State != desktop.Failed || len(result.Checks) != 0 {
				t.Fatalf("diagnose: %+v", result)
			}
			if result := restored.ConnectHub(); result.State != desktop.Failed || result.Connected {
				t.Fatalf("connect: %+v", result)
			}

			// Choosing a configuration again recovers, and is what the next
			// window restores.
			chosen := writeHubClientConfig(t, t.TempDir(), "https://"+hub.address)
			if result := restored.SelectHubConfig(chosen); result.State != desktop.Completed || result.ConfigPath != chosen {
				t.Fatalf("choosing again: %+v", result)
			}
			if status := restored.HubStatus(); status.State != desktop.Completed || status.Reason != "" || status.ConfigPath != chosen {
				t.Fatalf("choosing again did not recover: %+v", status)
			}
			if status := freshApp(t, &queueChooser{}, selection).HubStatus(); status.State != desktop.Completed || status.ConfigPath != chosen {
				t.Fatalf("the next window did not restore the configuration chosen again: %+v", status)
			}
		})
	}
	if n := hub.accepted.Load(); n != 0 {
		t.Fatalf("the hub received %d connections; nothing here reaches it", n)
	}
}

// A choice this window cannot remember is refused and changes nothing, as the
// operation and commercial selections refuse theirs (#364): the window keeps
// the configuration it had, or the reason a remembered one could not be
// restored, and the next window restores what it did before. The refusal says
// how to recover, and choosing again once the selection can be written does.
// The failure is injected as an interrupted write retained beside the
// remembered selection, which a replacement never reuses, and it is kept.
func TestAHubChoiceThatCannotBeRememberedIsRefusedAndChangesNothing(t *testing.T) {
	hub := newCountingEndpoint(t, false)
	const refused = "cannot retain the hub configuration selection, so the selection is unchanged; choose a hub configuration again"
	for _, c := range []struct {
		name string
		// selected reports whether an earlier window selected a
		// configuration, and removed whether it was then removed, so this
		// window shows why it could not be restored.
		selected, removed bool
		// shown is what this window's hub status says before the choice.
		shown desktop.State
	}{
		{"nothing was selected", false, false, desktop.Empty},
		{"a configuration was selected", true, false, desktop.Completed},
		{"a remembered configuration was removed", true, true, desktop.Failed},
	} {
		t.Run(c.name, func(t *testing.T) {
			selection := filepath.Join(t.TempDir(), "operations.json")
			remembered := filepath.Join(filepath.Dir(selection), "hub.json")
			if c.selected {
				earlierConfig := writeHubClientConfig(t, t.TempDir(), "https://"+hub.address)
				if result := freshApp(t, &queueChooser{}, selection).SelectHubConfig(earlierConfig); result.State != desktop.Completed {
					t.Fatalf("select: %+v", result)
				}
				if c.removed {
					if err := os.Remove(earlierConfig); err != nil {
						t.Fatal(err)
					}
				}
			}
			rememberedBefore, rememberedErr := os.ReadFile(remembered)

			chosen := writeHubClientConfig(t, t.TempDir(), "https://"+hub.address)
			app := freshApp(t, &queueChooser{folders: []string{filepath.Dir(chosen), filepath.Dir(chosen)}}, selection)
			before := app.HubStatus()
			if before.State != c.shown {
				t.Fatalf("before the choice the hub status is %+v, want %s", before, c.shown)
			}
			interrupted := remembered + ".incomplete"
			if err := os.WriteFile(interrupted, []byte("interrupted"), 0o600); err != nil {
				t.Fatal(err)
			}

			result := app.ChooseHubConfig()
			if result.State != desktop.Failed || result.Reason != refused {
				t.Fatalf("a choice that cannot be remembered was not refused: %+v", result)
			}
			if result.ConfigPath != "" || result.HubURL != "" || result.Connected {
				t.Fatalf("the refusal reports the refused configuration as selected: %+v", result)
			}
			// The window says what it said before: the same selection, or the
			// same reason, and nothing about the refused choice.
			if after := app.HubStatus(); !reflect.DeepEqual(after, before) {
				t.Fatalf("a refused choice changed the hub status from %+v to %+v", before, after)
			}
			// What was remembered is left as it was, and so is the interrupted
			// write that stood in the way.
			rememberedAfter, err := os.ReadFile(remembered)
			if (err == nil) != (rememberedErr == nil) || string(rememberedAfter) != string(rememberedBefore) {
				t.Fatalf("a refused choice changed the remembered selection from %q (%v) to %q (%v)", rememberedBefore, rememberedErr, rememberedAfter, err)
			}
			if kept := mustRead(t, interrupted); string(kept) != "interrupted" {
				t.Fatalf("the interrupted write was reused: %q", kept)
			}
			// The next window restores what this one had, not the refused choice.
			if next := freshApp(t, &queueChooser{}, selection).HubStatus(); !reflect.DeepEqual(next, before) {
				t.Fatalf("the next window restored %+v, want %+v", next, before)
			}

			// Once the selection can be written, choosing again recovers, and it
			// is what the next window restores.
			if err := os.Remove(interrupted); err != nil {
				t.Fatal(err)
			}
			if result := app.ChooseHubConfig(); result.State != desktop.Completed || result.ConfigPath != chosen {
				t.Fatalf("choosing again: %+v", result)
			}
			if status := freshApp(t, &queueChooser{}, selection).HubStatus(); status.State != desktop.Completed || status.ConfigPath != chosen || status.Reason != "" {
				t.Fatalf("the next window did not restore the configuration chosen again: %+v", status)
			}
		})
	}
	if n := hub.accepted.Load(); n != 0 {
		t.Fatalf("the hub received %d connections; nothing here reaches it", n)
	}
}
