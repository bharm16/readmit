package desktop_test

// Every operation that opens the host's native folder or file dialog is held
// to the same three outcomes through the facade seam the dialog answers at: a
// dismissed dialog is a cancellation, an unavailable dialog is a recoverable
// failure with a reason, and a chosen entry is answered — never refused as
// busy by the operation that opened the dialog, and never taken as a choice it
// was not. The native dialog itself is the host's; what is exercised here is
// what the window does with each of its answers.

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/testlicense"
)

// stateOf reads the one state and reason every facade result carries.
func stateOf(result any) (desktop.State, string) {
	value := reflect.ValueOf(result)
	return desktop.State(value.FieldByName("State").String()), value.FieldByName("Reason").String()
}

func TestEveryNativeDialogCancelsFailsRecoverablyAndAnswersItsChoice(t *testing.T) {
	workspace := t.TempDir()
	writeCase(t, workspace, "case", framed(string(listenFrame(t))))
	writeAckSpec(t, workspace, "spec.json", "AA")
	hubFolder := t.TempDir()
	writeHubClientConfig(t, hubFolder, "https://127.0.0.1:1")
	destinations := filepath.Join(t.TempDir(), "destinations.json")
	writeDocument(t, filepath.Dir(destinations), filepath.Base(destinations),
		`{"schema":"readmit-commercial-destinations/v1","environment":"sandbox","portal":"https://portal.example.test/checkouts"}`)
	policy := testlicense.New(t)

	type dialog struct {
		name string
		call func(*desktop.App) any
		// folder and files are what a person chooses; neither is set for an
		// operation whose choice needs more than one dialog to complete.
		folder string
		files  []string
	}
	fresh := func() string { return t.TempDir() }
	dialogs := []dialog{
		{name: "SelectWorkspace", call: func(a *desktop.App) any { return a.SelectWorkspace() }, folder: workspace},
		{name: "CreateSampleWorkspace", call: func(a *desktop.App) any { return a.CreateSampleWorkspace() }, folder: fresh()},
		{name: "CreateProject", call: func(a *desktop.App) any {
			return a.CreateProject("investigation", "Scheduling interface", "integration-team", []string{"siu-2.5.1-v1"})
		}, folder: fresh()},
		{name: "ChooseCapturePath", call: func(a *desktop.App) any { return a.ChooseCapturePath("source-root") }, folder: fresh()},
		{name: "ChooseImportSources", call: func(a *desktop.App) any { return a.ChooseImportSources("folder") }, folder: fresh()},
		{name: "ChooseHubConfig", call: func(a *desktop.App) any { return a.ChooseHubConfig() }, folder: hubFolder},
		{name: "ChooseCommercialDestinations", call: func(a *desktop.App) any { return a.ChooseCommercialDestinations() }, files: []string{destinations}},
		{name: "ChooseLicenseFolder", call: func(a *desktop.App) any { return a.ChooseLicenseFolder() }, folder: fresh()},
		{name: "VerifyLicenseDocument", call: func(a *desktop.App) any { return a.VerifyLicenseDocument() }},
		{name: "RenewLicenseDocument", call: func(a *desktop.App) any { return a.RenewLicenseDocument() }},
		{name: "ExportLicenseDocument", call: func(a *desktop.App) any { return a.ExportLicenseDocument() }, folder: fresh()},
		{name: "ChooseMaintenancePath", call: func(a *desktop.App) any { return a.ChooseMaintenancePath("backup-destination") }, folder: fresh()},
		{name: "ChooseOperationPolicy", call: func(a *desktop.App) any { return a.ChooseOperationPolicy() }, folder: filepath.Dir(policy)},
		{name: "ChooseSupportExportPath", call: func(a *desktop.App) any { return a.ChooseSupportExportPath() }, folder: fresh()},
		{name: "ChoosePacketExportPath", call: func(a *desktop.App) any { return a.ChoosePacketExportPath() }, folder: fresh()},
		{name: "ChooseRunSpec", call: func(a *desktop.App) any { return a.ChooseRunSpec(workspace) }, files: []string{filepath.Join(resolved(t, workspace), "spec.json")}},
	}
	window := func(c *chooser) *desktop.App {
		state := t.TempDir()
		app := desktop.NewWithOperationSelection(c, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"),
			filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"), filepath.Join(state, "operations.json"))
		if result := app.SelectOperationPolicy(policy); result.State != desktop.Completed {
			t.Fatal(result)
		}
		return app
	}
	for _, d := range dialogs {
		if _, ok := reflect.TypeOf(&desktop.App{}).MethodByName(d.name); !ok {
			t.Fatalf("%s is not a bound operation", d.name)
		}
		if state, reason := stateOf(d.call(window(&chooser{}))); state != desktop.Cancelled || reason == "" {
			t.Errorf("%s: a dismissed dialog is %s %q, want a cancellation with its reason", d.name, state, reason)
		}
		if state, reason := stateOf(d.call(window(&chooser{err: errors.New("the dialog host went away")}))); state != desktop.Failed || reason == "" {
			t.Errorf("%s: an unavailable dialog is %s %q, want a recoverable failure with its reason", d.name, state, reason)
		}
		if d.folder == "" && d.files == nil {
			continue
		}
		app := window(&chooser{folder: d.folder, files: d.files})
		if state, reason := stateOf(d.call(app)); state != desktop.Completed {
			t.Errorf("%s: a chosen entry is %s %q, want it answered", d.name, state, reason)
		}
		// The window is usable after every answer: the dialog released the
		// operation slot it held.
		if state, _ := stateOf(app.DisclosureStatus()); state == desktop.Busy {
			t.Errorf("%s left the operation slot held", d.name)
		}
	}
	// The hub configuration a person chose through the dialog is the one the
	// hub screen reports, and choosing it connected to nothing.
	chosen := window(&chooser{folder: hubFolder})
	chosen.ChooseHubConfig()
	if status := chosen.HubStatus(); status.ConfigPath != filepath.Join(hubFolder, "hub-client.json") || status.Connected {
		t.Fatalf("the chosen hub configuration is not the one selected: %+v", status)
	}
}
