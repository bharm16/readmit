package desktop_test

// Every operation that opens the host's native folder, file or save dialog is
// held to the same three outcomes through the facade seam the dialog answers
// at: a dismissed dialog is a cancellation, an unavailable dialog is a
// recoverable failure with a reason, and a chosen entry is answered — never
// refused as busy by the operation that opened the dialog, and never taken as
// a choice it was not. A folder that exists is picked in the folder dialog; a
// new folder a writer creates is named in the save dialog, because the folder
// dialog returns only a folder that already exists. The native dialog itself
// is the host's; what is exercised here is what the window does with each of
// its answers.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/report"
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
		// opens is the host dialog the operation presents first.
		opens string
		// folder and files are what a person chooses, and destination is the
		// new folder they name; none is set for an operation whose choice
		// needs more than one dialog to complete.
		folder      string
		files       []string
		destination string
	}
	fresh := func() string { return t.TempDir() }
	// A new folder is named in a folder that exists, and is not there yet.
	unnamed := func() string { return filepath.Join(t.TempDir(), "named-here") }
	maintenance := func(kind string) func(*desktop.App) any {
		return func(a *desktop.App) any { return a.ChooseMaintenancePath(kind) }
	}
	synthetic := func(kind string) func(*desktop.App) any {
		return func(a *desktop.App) any { return a.ChooseSyntheticPacketPath(kind) }
	}
	dialogs := []dialog{
		{name: "SelectWorkspace", call: func(a *desktop.App) any { return a.SelectWorkspace() }, opens: "folder", folder: workspace},
		{name: "CreateSampleWorkspace", call: func(a *desktop.App) any { return a.CreateSampleWorkspace() }, opens: "folder", folder: fresh()},
		{name: "CreateProject", call: func(a *desktop.App) any {
			return a.CreateProject("investigation", "Scheduling interface", "integration-team", []string{"siu-2.5.1-v1"})
		}, opens: "folder", folder: fresh()},
		{name: "ChooseCapturePath", call: func(a *desktop.App) any { return a.ChooseCapturePath("source-root") }, opens: "folder", folder: fresh()},
		{name: "ChooseImportSources", call: func(a *desktop.App) any { return a.ChooseImportSources("folder") }, opens: "folder", folder: fresh()},
		{name: "ChooseHubConfig", call: func(a *desktop.App) any { return a.ChooseHubConfig() }, opens: "folder", folder: hubFolder},
		{name: "ChooseCommercialDestinations", call: func(a *desktop.App) any { return a.ChooseCommercialDestinations() }, opens: "files", files: []string{destinations}},
		{name: "ChooseLicenseFolder", call: func(a *desktop.App) any { return a.ChooseLicenseFolder() }, opens: "folder", folder: fresh()},
		{name: "VerifyLicenseDocument", call: func(a *desktop.App) any { return a.VerifyLicenseDocument() }, opens: "files"},
		{name: "RenewLicenseDocument", call: func(a *desktop.App) any { return a.RenewLicenseDocument() }, opens: "files"},
		{name: "ExportLicenseDocument", call: func(a *desktop.App) any { return a.ExportLicenseDocument() }, opens: "folder", folder: fresh()},
		{name: "ChooseMaintenancePath(backup-destination)", call: maintenance("backup-destination"), opens: "save", destination: unnamed()},
		{name: "ChooseMaintenancePath(restore-destination)", call: maintenance("restore-destination"), opens: "save", destination: unnamed()},
		{name: "ChooseMaintenancePath(archive-destination)", call: maintenance("archive-destination"), opens: "save", destination: unnamed()},
		{name: "ChooseMaintenancePath(backup-source)", call: maintenance("backup-source"), opens: "folder", folder: fresh()},
		{name: "ChooseMaintenancePath(upgrade-candidate)", call: maintenance("upgrade-candidate"), opens: "folder", folder: fresh()},
		{name: "ChooseOperationPolicy", call: func(a *desktop.App) any { return a.ChooseOperationPolicy() }, opens: "folder", folder: filepath.Dir(policy)},
		{name: "ChooseSupportExportPath", call: func(a *desktop.App) any { return a.ChooseSupportExportPath() }, opens: "save", destination: unnamed()},
		{name: "ChoosePacketExportPath", call: func(a *desktop.App) any { return a.ChoosePacketExportPath() }, opens: "save", destination: unnamed()},
		{name: "ChooseSyntheticPacketPath(packet-destination)", call: synthetic("packet-destination"), opens: "save", destination: unnamed()},
		{name: "ChooseSyntheticPacketPath(rerun-destination)", call: synthetic("rerun-destination"), opens: "save", destination: unnamed()},
		{name: "ChooseSyntheticPacketPath(packet)", call: synthetic("packet"), opens: "folder", folder: fresh()},
		{name: "ChooseRunSpec", call: func(a *desktop.App) any { return a.ChooseRunSpec(workspace) }, opens: "files", files: []string{filepath.Join(resolved(t, workspace), "spec.json")}},
		{name: "ChooseExplanationInput(run)", call: func(a *desktop.App) any { return a.ChooseExplanationInput(workspace, "run") }, opens: "folder", folder: filepath.Join(resolved(t, workspace), "case")},
		{name: "ChooseExplanationInput(assertions)", call: func(a *desktop.App) any { return a.ChooseExplanationInput(workspace, "assertions") }, opens: "files", files: []string{filepath.Join(resolved(t, workspace), "spec.json")}},
		{name: "ChooseInspectionPath", call: func(a *desktop.App) any { return a.ChooseInspectionPath("file") }, opens: "files", files: []string{filepath.Join(resolved(t, workspace), "spec.json")}},
		{name: "ChooseCorpusPath", call: func(a *desktop.App) any { return a.ChooseCorpusPath("corpus-folder") }, opens: "folder", folder: fresh()},
	}
	window := func(c *chooser) *desktop.App {
		state := t.TempDir()
		app := desktop.NewWithOperationSelection(c, desktop.ShellDocuments{Folder: state})
		if result := app.SelectOperationPolicy(policy); result.State != desktop.Completed {
			t.Fatal(result)
		}
		return app
	}
	for _, d := range dialogs {
		if _, ok := reflect.TypeOf(&desktop.App{}).MethodByName(strings.Split(d.name, "(")[0]); !ok {
			t.Fatalf("%s is not a bound operation", d.name)
		}
		dismissed := &chooser{}
		if state, reason := stateOf(d.call(window(dismissed))); state != desktop.Cancelled || reason == "" {
			t.Errorf("%s: a dismissed dialog is %s %q, want a cancellation with its reason", d.name, state, reason)
		}
		if len(dismissed.opened) == 0 || dismissed.opened[0] != d.opens {
			t.Errorf("%s presented the %v dialogs, want the %s dialog first", d.name, dismissed.opened, d.opens)
		}
		if state, reason := stateOf(d.call(window(&chooser{err: errors.New("the dialog host went away")}))); state != desktop.Failed || reason == "" {
			t.Errorf("%s: an unavailable dialog is %s %q, want a recoverable failure with its reason", d.name, state, reason)
		}
		if d.folder == "" && d.files == nil && d.destination == "" {
			continue
		}
		answered := &chooser{folder: d.folder, files: d.files, destination: d.destination}
		app := window(answered)
		result := d.call(app)
		if state, reason := stateOf(result); state != desktop.Completed {
			t.Errorf("%s: a chosen entry is %s %q, want it answered", d.name, state, reason)
		}
		if d.destination != "" {
			// The named folder is handed on exactly as the dialog returned it,
			// and naming it created nothing: its writer creates it.
			if path := reflect.ValueOf(result).FieldByName("Path").String(); path != d.destination {
				t.Errorf("%s answered %q, want the named %q", d.name, path, d.destination)
			}
			if _, err := os.Lstat(d.destination); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s: naming a new folder created something: %v", d.name, err)
			}
			if !slices.Equal(answered.opened, []string{"save"}) {
				t.Errorf("%s presented the %v dialogs to name a new folder, want the save dialog alone", d.name, answered.opened)
			}
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

// Every new folder the window asks for is named in the host's save dialog and
// created only by the writer it is handed to — a backup, a restored project, a
// recovery archive (a rollback archive is the same kind and writer), a portable
// review, a support bundle, a synthetic demonstration packet and its runnable
// copies. A name that already exists, which a save dialog
// returns once the person confirms replacing it, is handed on unchanged and
// refused by the writer with its reason, and nothing is written into it.
func TestEveryNewFolderIsNamedInTheSaveDialogAndCreatedOnlyByItsWriter(t *testing.T) {
	dialogs := &chooser{folder: t.TempDir()}
	app := newApp(t, dialogs)
	project := sampleProject(t, app)
	backup := filepath.Join(t.TempDir(), "backup")
	if created := app.CreateProjectBackup(desktop.BackupCreateRequest{Project: project, Destination: backup}); created.State != desktop.Completed {
		t.Fatalf("the backup a restore reads was not created: %+v", created)
	}
	retirement := app.PreviewProjectRetirement(project)
	if retirement.State != desktop.Completed || retirement.Preview == nil {
		t.Fatalf("retirement preview: %+v", retirement)
	}
	packets, spec, baseline, current := packetWorkspace(t, app)
	packet := app.AssemblePacket(packetRequest(packets, spec, baseline, current))
	if packet.State != desktop.Completed || packet.Packet == nil {
		t.Fatalf("assemble: %+v", packet)
	}
	shared, review, _ := readyReview(t, app)
	if saved := app.SaveSharingPolicy(desktop.SupportPolicyRequest{
		Workspace: shared, Output: "sharing.json", Support: true, Destinations: []string{"local-file"}, MaxBytes: 4096,
	}); saved.State != desktop.Completed {
		t.Fatalf("sharing policy: %+v", saved)
	}
	summary := app.PreviewSupportSummary(desktop.SupportRequest{
		Workspace: shared, Source: review.Review, Kind: "derived-review", Private: review.Private, Policy: "sharing.json",
	})
	if summary.State != desktop.Completed || summary.Summary == nil {
		t.Fatalf("support preview: %+v", summary)
	}
	synthetic := filepath.Join(t.TempDir(), "synthetic")
	if generated := app.GenerateSyntheticPacket(desktop.SyntheticPacketRequest{Scenario: report.Scenario, Destination: synthetic}); generated.State != desktop.Completed {
		t.Fatalf("the synthetic packet runnable copies are prepared from was not generated: %+v", generated)
	}

	type destination struct {
		name   string
		choose func() any
		write  func(path string) any
		// exists is what the writer says of a destination that already exists.
		exists string
	}
	for _, d := range []destination{
		{"backup", func() any { return app.ChooseMaintenancePath("backup-destination") }, func(path string) any {
			return app.CreateProjectBackup(desktop.BackupCreateRequest{Project: project, Destination: path})
		}, "cannot create backup; destination must be new and parent writable"},
		{"restored project", func() any { return app.ChooseMaintenancePath("restore-destination") }, func(path string) any {
			return app.RestoreProjectBackup(desktop.BackupRestoreRequest{Backup: backup, Destination: path})
		}, "cannot create the restored project; destination must be new and parent writable"},
		{"recovery archive", func() any { return app.ChooseMaintenancePath("archive-destination") }, func(path string) any {
			return app.ArchiveOrDeleteProject(desktop.ProjectArchiveRequest{Project: project, Destination: path, Selection: retirement.Preview.Selection})
		}, "destination must be new"},
		{"portable review", func() any { return app.ChoosePacketExportPath() }, func(path string) any {
			return app.ExportPacketReview(desktop.PacketExportRequest{Workspace: packets, Packet: packet.Packet.Entry, Destination: path})
		}, "destination must be new"},
		{"support bundle", func() any { return app.ChooseSupportExportPath() }, func(path string) any {
			return app.PublishSupportSummary(desktop.SupportPublishRequest{
				Workspace: shared, Source: review.Review, Kind: "derived-review", Private: review.Private,
				Policy: "sharing.json", Approval: summary.Summary.Identity, Output: path,
			})
		}, "the publication was refused"},
		{"synthetic packet", func() any { return app.ChooseSyntheticPacketPath("packet-destination") }, func(path string) any {
			return app.GenerateSyntheticPacket(desktop.SyntheticPacketRequest{Scenario: report.Scenario, Destination: path})
		}, "destination must be new"},
		{"runnable copies", func() any { return app.ChooseSyntheticPacketPath("rerun-destination") }, func(path string) any {
			return app.PrepareSyntheticRerun(desktop.SyntheticRerunRequest{Packet: synthetic, Destination: path, Address: "127.0.0.1:2575"})
		}, "destination must be new"},
	} {
		named := filepath.Join(t.TempDir(), "new-"+strings.ReplaceAll(d.name, " ", "-"))
		occupied := t.TempDir()
		for _, path := range []string{named, occupied} {
			dialogs.destination, dialogs.opened = path, nil
			chosen := d.choose()
			if state, reason := stateOf(chosen); state != desktop.Completed || reflect.ValueOf(chosen).FieldByName("Path").String() != path {
				t.Fatalf("%s: the save dialog's answer was not handed on: %s %q %+v", d.name, state, reason, chosen)
			}
			if !slices.Equal(dialogs.opened, []string{"save"}) {
				t.Fatalf("%s: presented the %v dialogs, want the save dialog alone", d.name, dialogs.opened)
			}
		}
		if _, err := os.Lstat(named); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s: naming the new folder created something: %v", d.name, err)
		}
		if state, reason := stateOf(d.write(named)); state != desktop.Completed {
			t.Fatalf("%s: the writer refused the new folder it was handed: %s %q", d.name, state, reason)
		}
		if info, err := os.Lstat(named); err != nil || !info.IsDir() {
			t.Fatalf("%s: the writer did not create the named folder: %v", d.name, err)
		}
		if state, reason := stateOf(d.write(occupied)); state != desktop.Failed || !strings.Contains(reason, d.exists) {
			t.Errorf("%s: an existing destination is %s %q, want it refused with %q", d.name, state, reason, d.exists)
		}
		if entries, err := os.ReadDir(occupied); err != nil || len(entries) != 0 {
			t.Errorf("%s: a refused destination was written into: %v %v", d.name, entries, err)
		}
	}
}

// A dismissed save dialog names nothing, so nothing is handed to a writer and
// the window is free again; a window whose host offers no save dialog says so
// rather than picking a folder.
func TestADismissedSaveDialogDoesNothing(t *testing.T) {
	app := newApp(t, &chooser{})
	for _, call := range []func() any{
		func() any { return app.ChooseMaintenancePath("backup-destination") },
		func() any { return app.ChooseMaintenancePath("restore-destination") },
		func() any { return app.ChooseMaintenancePath("archive-destination") },
		func() any { return app.ChoosePacketExportPath() },
		func() any { return app.ChooseSupportExportPath() },
		func() any { return app.ChooseSyntheticPacketPath("packet-destination") },
		func() any { return app.ChooseSyntheticPacketPath("rerun-destination") },
	} {
		result := call()
		if state, reason := stateOf(result); state != desktop.Cancelled || reason != "no new folder was named" {
			t.Fatalf("a dismissed save dialog is %s %q", state, reason)
		}
		if path := reflect.ValueOf(result).FieldByName("Path").String(); path != "" {
			t.Fatalf("a dismissed save dialog answered a path: %q", path)
		}
	}
	if state, _ := stateOf(app.DisclosureStatus()); state == desktop.Busy {
		t.Fatal("a dismissed save dialog left the operation slot held")
	}
	state := t.TempDir()
	folderOnly := activatedApp(t, &folderDialogOnly{folder: t.TempDir()}, state)
	if result := folderOnly.ChoosePacketExportPath(); result.State != desktop.Failed || result.Reason != "the save dialog is unavailable" || result.Path != "" {
		t.Fatalf("a host without a save dialog answered %+v", result)
	}
}

// folderDialogOnly is a host that offers the folder dialog and nothing else.
type folderDialogOnly struct{ folder string }

func (f *folderDialogOnly) ChooseFolder(string) (string, error) { return f.folder, nil }
