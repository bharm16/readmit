package bundle_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/engineexport"
)

func TestEngineExportReopensExactContainer(t *testing.T) {
	plan := engineexport.Plan{Schema: engineexport.Schema, Engine: "mirth", Version: "4.5.2", Format: "raw", Terminator: "cr"}
	original := []byte("MSH|^~\\&|S|F|R|F|20260101||ADT^A01|one|P|2.5.1\rPID|1\r\n\r\n\r\n")
	path := filepath.Join(t.TempDir(), "case")
	written, err := bundle.WriteEngineExport(context.Background(), path, "export.hl7", original, plan, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	container, err := opened.EngineContainer()
	if err != nil || !bytes.Equal(container, original) || opened.Manifest.Schema != "readmit-case/v5" || opened.Identity != written.Identity {
		t.Fatal("container or identity lost")
	}
	if opened.Events[0].Direction != bundle.Unknown || opened.Events[0].ObservedAt != nil {
		t.Fatal("invented observation")
	}
	container[0] = 'X'
	again, _ := opened.EngineContainer()
	if !bytes.Equal(again, original) {
		t.Fatal("mutable evidence escaped")
	}
	if err := os.WriteFile(filepath.Join(path, "engine-container.bin"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Open(path); err == nil {
		t.Fatal("accepted changed container")
	}
}
func TestEngineExportCancellationCreatesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "case")
	_, err := bundle.WriteEngineExport(ctx, path, "export", []byte("bad"), engineexport.Plan{}, time.Now())
	if err == nil {
		t.Fatal("accepted cancellation")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("wrote cancelled output")
	}
}

func TestEngineExportRejectsRehashedExtractionAndLegacyInjection(t *testing.T) {
	plan := engineexport.Plan{Schema: engineexport.Schema, Engine: "mirth", Version: "4.5.2", Format: "raw", Terminator: "cr"}
	for _, tc := range []struct{ name, from, to string }{
		{"container", "MSH", "NSH"},
		{"direction", `"direction":"unknown"`, `"direction":"inbound"`},
		{"schema", `"readmit-case/v5"`, `"readmit-case/v1"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "case")
			_, err := bundle.WriteEngineExport(context.Background(), path, "export", []byte("MSH|^~\\&|S|F|R|F|20260101||ADT^A01|one|P|2.5.1\r"), plan, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			file := "manifest.json"
			if tc.name == "container" {
				file = "engine-container.bin"
			}
			if tc.name == "direction" {
				file = "events.jsonl"
			}
			p := filepath.Join(path, file)
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			raw = bytes.Replace(raw, []byte(tc.from), []byte(tc.to), 1)
			if err := os.WriteFile(p, raw, 0600); err != nil {
				t.Fatal(err)
			}
			version := "readmit-case/v5"
			if tc.name == "schema" {
				version = "readmit-case/v1"
			}
			rehashCaseVersion(t, path, version)
			if _, err := bundle.Open(path); err == nil {
				t.Fatal("accepted mismatched extraction after resealing")
			}
		})
	}
}

func TestDescribeDoesNotExtendLegacyEngineExportMembers(t *testing.T) {
	for _, version := range []string{"readmit-case/v1", "readmit-case/v2", "readmit-case/v3", "readmit-case/v4"} {
		for _, value := range []string{"null", `{"path":"engine-export.json","size":0,"sha256":""}`} {
			dir := t.TempDir()
			document := `{"schema":"` + version + `","engine_export":` + value + `}`
			if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(document), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := bundle.Describe(dir); err == nil {
				t.Fatalf("Describe widened %s", version)
			}
		}
	}
}
