package desktop_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/operation"
)

const facadeWindowDocument = `{
  "schema": "readmit-observation-window/v1",
  "source": {"kind": "file-export", "identity": "scheduling-archive", "scope": "appointments"},
  "watermark": {"kind": "none", "position": ""},
  "pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""},
  "completion": {"deadline": "3s", "quiet_period": "10ms", "stable_samples": 2, "max_records": 100, "max_samples": 32}
}`

const facadeSourceDocument = `{
  "schema": "readmit-observation-source/v1",
  "source": {"kind": "file-export", "identity": "scheduling-archive", "scope": "appointments"},
  "enabled": true,
  "freshness": {"max_age": "1h"},
  "extraction": {
    "envelope": "csv",
    "encoding": "utf-8",
    "csv": {"delimiter": ",", "record_separator": "lf", "header": "present", "fields": 2},
    "record_key": ["appointment"]
  },
  "file": {"path": "export.csv", "max_bytes": 65536},
  "http": null
}`

func TestObservationFacadeOpensWithoutQuerying(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()
	opened := app.OpenObservationSource(dir, "missing-source.json")
	if opened.State != desktop.Completed || opened.Source == nil {
		t.Fatalf("open defaults: %+v", opened)
	}
	if opened.Source.Observes.Kind != observesource.FileExport {
		t.Fatalf("default kind = %s", opened.Source.Observes.Kind)
	}
	if opened.Identity != "" {
		t.Fatalf("missing source has a pinned identity: %q", opened.Identity)
	}
	window := app.OpenObservationWindow(dir, "missing-window.json")
	if window.State != desktop.Completed || window.Window == nil || window.Identity != "" {
		t.Fatalf("missing window has a pinned identity: %+v", window)
	}
	support := app.ObservationSupport()
	if support.State != desktop.Completed || len(support.Support) < 4 {
		t.Fatalf("support: %+v", support)
	}
}

func TestObservationFacadeSavesAndValidatesLocally(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()
	writeDocument(t, dir, "window.json", facadeWindowDocument)
	writeDocument(t, dir, "source.json", facadeSourceDocument)
	writeDocument(t, dir, "export.csv", "appointment,status\nA1,booked\n")
	source := app.ValidateObservationSource(dir, "source.json")
	if source.State != desktop.Completed || source.Identity == "" {
		t.Fatalf("validate source: %+v", source)
	}
	window := app.ValidateObservationWindow(dir, "window.json")
	if window.State != desktop.Completed || window.Identity == "" {
		t.Fatalf("validate window: %+v", window)
	}
	if opened := app.OpenObservationSource(dir, "source.json"); opened.State != desktop.Completed || opened.Identity != source.Identity {
		t.Fatalf("opened source identity differs from strict read: %+v", opened)
	}
	if opened := app.OpenObservationWindow(dir, "window.json"); opened.State != desktop.Completed || opened.Identity != window.Identity {
		t.Fatalf("opened window identity differs from strict read: %+v", opened)
	}
	pair := app.ValidateObservationPair(desktop.ObservationValidateRequest{
		Workspace: dir, SourceFile: "source.json", WindowFile: "window.json",
	})
	if pair.State != desktop.Completed {
		t.Fatalf("pair: %+v", pair)
	}
}

func TestObservationFacadeRefusesUnauthorizedCollect(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()
	writeDocument(t, dir, "window.json", facadeWindowDocument)
	writeDocument(t, dir, "source.json", facadeSourceDocument)
	writeDocument(t, dir, "export.csv", "appointment,status\nA1,booked\n")

	result := app.CollectObservation(desktop.ObservationCollectFacadeRequest{
		Workspace: dir, SourceFile: "source.json", WindowFile: "window.json",
		OutputFile: "completion.json", SnapshotDir: "snapshot", Authorize: false,
	})
	if result.State != desktop.Failed || !strings.Contains(result.Reason, "explicit authorization") {
		t.Fatalf("unauthorized collect: %+v", result)
	}
}

func TestObservationFacadeCollectsAndExplains(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()
	writeDocument(t, dir, "window.json", facadeWindowDocument)
	writeDocument(t, dir, "source.json", facadeSourceDocument)
	writeDocument(t, dir, "export.csv", "appointment,status\nA1,booked\n")

	result := app.CollectObservation(desktop.ObservationCollectFacadeRequest{
		Workspace: dir, SourceFile: "source.json", WindowFile: "window.json",
		OutputFile: "completion.json", SnapshotDir: "snapshot", Authorize: true, Produced: []string{"A1"},
	})
	if result.State != desktop.Completed || result.Completion == nil || result.Summary == nil {
		t.Fatalf("collect: %+v", result)
	}
	if !result.Summary.Supported || result.Summary.Mapped != 1 {
		t.Fatalf("summary: %+v", result.Summary)
	}
	explained := app.ExplainObservation(desktop.ObservationExplainRequest{
		Workspace: dir, CompletionFile: "completion.json", WindowFile: "window.json",
	})
	if explained.State != desktop.Completed || explained.Completion == nil {
		t.Fatalf("explain: %+v", explained)
	}
}

func TestObservationFacadeBindsCaptureWithoutCollecting(t *testing.T) {
	app := workspaceApp(t)
	result := app.BindCaptureObservation(desktop.ObservationCaptureBindRequest{
		Workspace: t.TempDir(),
		Binding: operation.CaptureObservationBinding{
			CasePath: "/unused/absolute", Identity: "scheduling-downstream", Scope: "appointments",
			Kinds: []string{"message", "ack"}, RecordKey: "SCH-1.1", MaxOccurrences: 25,
		},
		RelativeCase: "downstream.case",
	})
	if result.State != desktop.Completed || result.Source == nil || result.Source.Capture == nil {
		t.Fatalf("bind: %+v", result)
	}
	if result.Source.Capture.Path != "downstream.case" || result.Source.Extraction != nil {
		t.Fatalf("capture source: %+v", result.Source)
	}
	if _, err := os.Stat(filepath.Join(t.TempDir(), "should-not-exist")); err == nil {
		t.Fatal("unexpected")
	}
}

func TestObservationFacadeSavePinsIdentity(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()
	opened := app.OpenObservationWindow(dir, "new-window.json")
	if opened.State != desktop.Completed || opened.Window == nil {
		t.Fatalf("open: %+v", opened)
	}
	saved := app.SaveObservationWindow(desktop.ObservationWindowRequest{
		Workspace: dir, WindowFile: "new-window.json", Window: opened.Window,
	})
	if saved.State != desktop.Completed || saved.Identity == "" {
		t.Fatalf("save: %+v", saved)
	}
	again := app.ValidateObservationWindow(dir, "new-window.json")
	if again.Identity != saved.Identity {
		t.Fatalf("identity drifted: %q vs %q", again.Identity, saved.Identity)
	}
}

// Saving a source through the facade writes, through the shared writer, the
// document the shared reader reads back, pins the identity of exactly the
// bytes written, and answers with the document as it declares itself. A
// source whose export is relative to its own folder keeps that one identity
// when it is validated and reopened, and saving the answer again writes the
// same bytes: the declared path never becomes this machine's absolute one.
func TestObservationFacadeSavesASourceTheSharedReaderReadsBack(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()
	// The source arrives the way the window sends it: a v1 document, decoded.
	var edited observesource.Source
	if err := json.Unmarshal([]byte(strings.Replace(facadeSourceDocument, "export.csv", "downstream/appointments.csv", 1)), &edited); err != nil {
		t.Fatal(err)
	}
	saved := app.SaveObservationSource(desktop.ObservationSourceRequest{
		Workspace: dir, SourceFile: "new-source.json", Source: &edited,
	})
	if saved.State != desktop.Completed || saved.Identity == "" || saved.Source == nil {
		t.Fatalf("save: %+v", saved)
	}
	written, err := os.ReadFile(filepath.Join(dir, "new-source.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bytes.TrimSuffix(written, []byte("\n")))
	if hex.EncodeToString(sum[:]) != saved.Identity {
		t.Fatalf("the save pinned %s, not the identity of the bytes it wrote", saved.Identity)
	}
	read, err := observesource.ReadSource(filepath.Join(dir, "new-source.json"))
	if err != nil {
		t.Fatal(err)
	}
	// The reader anchors the declared export path to the document's folder.
	if read.Schema != observesource.SchemaV1 || read.File == nil || !filepath.IsAbs(read.File.Path) ||
		!strings.HasSuffix(read.File.Path, filepath.Join("downstream", "appointments.csv")) {
		t.Fatalf("the shared reader read %+v", read)
	}
	if saved.Source.File == nil || saved.Source.File.Path != "downstream/appointments.csv" {
		t.Fatalf("the facade answered %+v, not the document it saved", saved.Source)
	}
	validated := app.ValidateObservationSource(dir, "new-source.json")
	if validated.State != desktop.Completed || validated.Identity != saved.Identity {
		t.Fatalf("validating the saved source reported %+v, the save pinned %s", validated, saved.Identity)
	}
	opened := app.OpenObservationSource(dir, "new-source.json")
	if opened.State != desktop.Completed || opened.Identity != saved.Identity || opened.Source.File.Path != "downstream/appointments.csv" {
		t.Fatalf("reopening the saved source answered %+v, the save pinned %s", opened, saved.Identity)
	}
	again := app.SaveObservationSource(desktop.ObservationSourceRequest{Workspace: dir, SourceFile: "new-source.json", Source: opened.Source})
	if again.State != desktop.Completed || again.Identity != saved.Identity {
		t.Fatalf("saving the reopened source again answered %+v", again)
	}
	if rewritten, err := os.ReadFile(filepath.Join(dir, "new-source.json")); err != nil || !bytes.Equal(rewritten, written) {
		t.Fatalf("saving the reopened source again changed the document:\n%s\n%s", written, rewritten)
	}
}

// A source or window saved into a retained case is refused by the shared
// output reservation, and leaves that case exactly as it was: no folder the
// save would have created is left behind, so the case still verifies as
// complete, unmodified evidence.
func TestObservationFacadeRefusesASaveInsideACaseAndLeavesItVerifiable(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	root := sample(t, app).Workspace.Root
	if opened := app.OpenCase(root, "regression"); opened.State != desktop.Completed {
		t.Fatalf("the sample case did not open: %+v", opened)
	}
	window := app.OpenObservationWindow(root, "observation-window.json")
	source := app.OpenObservationSource(root, "observation-source.json")
	if window.State != desktop.Completed || source.State != desktop.Completed {
		t.Fatalf("new documents did not open: %+v %+v", window, source)
	}
	savedSource := app.SaveObservationSource(desktop.ObservationSourceRequest{Workspace: root, SourceFile: "regression/observations/source.json", Source: source.Source})
	if savedSource.State != desktop.Failed || savedSource.Reason != "cannot write an observation source here" {
		t.Fatalf("a source inside the case: %+v", savedSource)
	}
	savedWindow := app.SaveObservationWindow(desktop.ObservationWindowRequest{Workspace: root, WindowFile: "regression/windows/window.json", Window: window.Window})
	if savedWindow.State != desktop.Failed || savedWindow.Reason != "cannot write an observation window here" {
		t.Fatalf("a window inside the case: %+v", savedWindow)
	}
	for _, folder := range []string{"observations", "windows"} {
		if _, err := os.Lstat(filepath.Join(root, "regression", folder)); !os.IsNotExist(err) {
			t.Fatalf("a refused save left %s inside the case", folder)
		}
	}
	if opened := app.OpenCase(root, "regression"); opened.State != desktop.Completed {
		t.Fatalf("the case no longer verifies after a refused save: %+v", opened)
	}
}
