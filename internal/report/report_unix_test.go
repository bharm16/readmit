//go:build !windows

package report_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/bharm16/readmit/internal/report"
)

func TestPacketRejectsLinksDevicesAndSymlinkOutputTraversal(t *testing.T) {
	parent := t.TempDir()
	original := filepath.Join(parent, "packet")
	if _, err := report.Create(context.Background(), report.Scenario, original); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"symlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			dir := clone(t, original)
			name := filepath.Join(dir, "unexpected")
			var err error
			if kind == "symlink" {
				err = os.Symlink(filepath.Join(original, "spec.json"), name)
			} else {
				err = syscall.Mkfifo(name, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := report.Open(dir); err == nil {
				t.Fatal("nonregular evidence accepted")
			}
		})
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(filepath.Join(original, "baseline", "run"), alias); err != nil {
		t.Fatal(err)
	}
	resolved, err := report.Open(alias + "/../..")
	if err != nil || resolved.Manifest.Scenario != report.Scenario {
		t.Fatalf("raw input traversal did not resolve before joins: %v", err)
	}
	before := snapshot(t, original)
	for _, output := range []string{alias + "/new", alias + "/../new"} {
		if _, err := report.Create(context.Background(), report.Scenario, output); err == nil {
			t.Fatal("symlink alias created output inside packet")
		}
		if _, err := report.Prepare(original, output, "127.0.0.1:2575"); err == nil {
			t.Fatal("symlink alias prepared output inside packet")
		}
	}
	if !reflect.DeepEqual(before, snapshot(t, original)) {
		t.Fatal("symlink traversal mutated packet")
	}
}
