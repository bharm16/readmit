package artifactdir_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/report"
)

// A report runs its trials in an execution workspace it removes before it
// answers, and keeps what they wrote only as copies inside its packet: the
// ledger its fixtures install without flushing (#350) among them. So nothing
// in that workspace is flushed (#346), while every file the packet holds is
// synced holding its final bytes, and every directory naming one, the packet
// itself and the folder holding it are synced after its completion record,
// before the report is created.
func TestAReportSyncsEveryEntryOfItsPacketAndNothingInItsWorkspace(t *testing.T) {
	workspace := isolateWorkspace(t)
	// The fixture and the sender write at once, so their syncs are recorded
	// under one lock, each with its place in the order they were made.
	var mu sync.Mutex
	made := 0
	syncedFiles, fileSynced, directorySynced := map[string][]byte{}, map[string]int{}, map[string]int{}
	var scratch []string
	t.Cleanup(artifactdir.ObserveFileSyncsForTest(func(path string) error {
		data, err := os.ReadFile(path)
		mu.Lock()
		defer mu.Unlock()
		made++
		path = filepath.Clean(path)
		if inside(workspace, path) {
			scratch = append(scratch, path)
		}
		syncedFiles[path], fileSynced[path] = data, made
		return err
	}))
	t.Cleanup(artifactdir.ObserveDirectorySyncsForTest(func(directory string) error {
		mu.Lock()
		defer mu.Unlock()
		made++
		directory = filepath.Clean(directory)
		if inside(workspace, directory) {
			scratch = append(scratch, directory)
		}
		directorySynced[directory] = made
		return nil
	}))
	folder := caseFolder(t)
	packet := filepath.Join(folder, "packet")
	created, err := report.Create(t.Context(), report.Scenario, packet)
	if err != nil {
		t.Fatal(err)
	}
	if len(scratch) != 0 {
		t.Errorf("the report flushed %d entries of the workspace it removes, the first %s", len(scratch), scratch[0])
	}
	if left, err := os.ReadDir(workspace); err != nil || len(left) != 0 {
		t.Fatalf("the report did not remove its workspace: %d entries left, %v", len(left), err)
	}
	for _, trial := range []string{"baseline", "post-fix"} {
		if _, err := os.Stat(filepath.Join(packet, trial, "observation.json")); err != nil {
			t.Fatalf("the packet holds no %s ledger copy: %v", trial, err)
		}
	}

	completed, ok := fileSynced[filepath.Join(packet, "identity.sha256")]
	if !ok {
		t.Fatal("the report was created before its completion record was synced")
	}
	directories := []string{folder}
	err = filepath.WalkDir(packet, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			directories = append(directories, filepath.Clean(path))
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if written, ok := syncedFiles[filepath.Clean(path)]; !ok || !bytes.Equal(written, data) {
			relative, _ := filepath.Rel(packet, path)
			t.Errorf("the report was created before %s was synced holding its bytes", filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(directories) != 11 {
		t.Fatalf("the packet and its folder are %d directories, want the folder, the packet and its 9 member directories", len(directories))
	}
	for _, directory := range directories {
		if synced, ok := directorySynced[directory]; !ok || synced < completed {
			relative, _ := filepath.Rel(folder, directory)
			t.Errorf("the report was created before the directory %s was synced after its completion record", filepath.ToSlash(relative))
		}
	}
	if opened, err := report.Open(packet); err != nil || opened.Identity != created.Identity {
		t.Fatalf("the synced packet did not reopen as created: %v", err)
	}
}

// A packet whose ledger copy, or any directory entry it is found through,
// cannot be synced is never reported created; one missing its ledger copy is
// not a packet at all.
func TestAReportWhosePacketCannotBeSyncedIsNeverCreated(t *testing.T) {
	isolateWorkspace(t)
	unsynced := filepath.Join(caseFolder(t), "packet")
	ledgerCopy := filepath.Join(unsynced, "baseline", "observation.json")
	t.Cleanup(artifactdir.ObserveFileSyncsForTest(func(path string) error {
		if filepath.Clean(path) == ledgerCopy {
			return errors.New("injected file sync failure")
		}
		return nil
	}))
	if _, err := report.Create(t.Context(), report.Scenario, unsynced); err == nil || !strings.Contains(err.Error(), "incomplete output retained") {
		t.Fatalf("a report whose ledger copy could not be synced was not refused as incomplete: %v", err)
	}
	if _, err := report.Open(unsynced); err == nil {
		t.Fatal("a packet whose ledger copy could not be synced opened as complete")
	}

	for _, failing := range []struct {
		name      string
		directory func(folder string) string
	}{
		{"payloads/", func(folder string) string { return filepath.Join(folder, "packet", "post-fix", "run", "payloads") }},
		{"packet", func(folder string) string { return filepath.Join(folder, "packet") }},
		{"folder", func(folder string) string { return folder }},
	} {
		t.Run(failing.name, func(t *testing.T) {
			folder := caseFolder(t)
			unsyncable := failing.directory(folder)
			t.Cleanup(artifactdir.ObserveDirectorySyncsForTest(func(directory string) error {
				if filepath.Clean(directory) == unsyncable {
					return errors.New("injected directory sync failure")
				}
				return nil
			}))
			created, err := report.Create(t.Context(), report.Scenario, filepath.Join(folder, "packet"))
			if err == nil || !strings.Contains(err.Error(), "incomplete output retained") {
				t.Fatalf("a report whose %s entries were not synced was not refused as incomplete: %v", failing.name, err)
			}
			if created != nil {
				t.Fatalf("a report whose %s entries were not synced was reported created as %s", failing.name, created.Identity)
			}
		})
	}
}

// isolateWorkspace points the folder this process makes its temporary
// workspaces in at a new one of the test's own, resolved as the writers
// resolve it, so every path below it is scratch.
func isolateWorkspace(t *testing.T) string {
	t.Helper()
	workspace := caseFolder(t)
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, workspace)
	}
	return workspace
}

func inside(folder, path string) bool {
	return strings.HasPrefix(path, folder+string(filepath.Separator))
}
