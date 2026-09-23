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
// bytes written, and answers with what the reader read.
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
	if saved.Source.File == nil || saved.Source.File.Path != read.File.Path {
		t.Fatalf("the facade answered %+v, not what the reader read", saved.Source)
	}
}
