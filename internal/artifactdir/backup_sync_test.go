package artifactdir_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/lifecycle"
	"github.com/bharm16/readmit/internal/project"
)

// A retirement deletes its source once the backup replacing it verifies, so by
// then the backup must survive a power loss: its manifest and completion
// marker synced holding their bytes, and every directory entry naming them or
// a stored file — each directory below files/, files/, the backup and the
// folder holding it — synced after the marker, all while the source still
// exists. Before #472 the manifest and marker were written without a sync and
// no directory was synced, so a power loss after the deletion could leave
// neither copy.
func TestAnArchiveDeletesItsSourceOnlyOnceItsBackupIsSynced(t *testing.T) {
	source := newProject(t)
	folder := caseFolder(t)
	archive := filepath.Join(folder, "archive")
	var mu sync.Mutex
	made := 0
	files, directories := map[string]fileSync{}, map[string]directorySync{}
	var sourceGone []string
	present := func(synced string) {
		if _, err := os.Stat(source); err != nil {
			sourceGone = append(sourceGone, synced)
		}
	}
	t.Cleanup(artifactdir.ObserveFileSyncsForTest(func(path string) error {
		data, err := os.ReadFile(path)
		mu.Lock()
		defer mu.Unlock()
		made++
		files[filepath.Clean(path)] = fileSync{made, data}
		present(path)
		return err
	}))
	t.Cleanup(artifactdir.ObserveDirectorySyncsForTest(func(directory string) error {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		made++
		synced := directorySync{at: made}
		for _, entry := range entries {
			synced.names = append(synced.names, entry.Name())
		}
		directories[filepath.Clean(directory)] = synced
		present(directory)
		return nil
	}))

	retirement, err := lifecycle.PreviewRetirement(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	report, err := lifecycle.Archive(context.Background(), source, archive, retirement.Selection, true)
	if err != nil || !report.Complete() {
		t.Fatalf("archive: %+v %v", report, err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("the archive did not retire its source: %v", err)
	}
	if len(sourceGone) != 0 {
		t.Fatalf("the source was deleted before %s was synced", sourceGone[0])
	}
	marker, ok := files[filepath.Join(archive, backup.MarkerName)]
	for _, name := range []string{backup.DocumentName, backup.MarkerName} {
		written, err := os.ReadFile(filepath.Join(archive, name))
		if err != nil {
			t.Fatal(err)
		}
		if synced, ok := files[filepath.Join(archive, name)]; !ok || !bytes.Equal(synced.data, written) {
			t.Errorf("the backup's %s was not synced holding its bytes before the archive answered", name)
		}
	}
	if !ok {
		t.Fatal("the backup's completion marker was never synced")
	}
	err = filepath.WalkDir(folder, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return err
		}
		relative, _ := filepath.Rel(folder, name)
		synced, ok := directories[name]
		if !ok || synced.at < marker.at {
			t.Errorf("the directory %s was not synced after the backup's completion marker", filepath.ToSlash(relative))
			return nil
		}
		entries, err := os.ReadDir(name)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !slices.Contains(synced.names, entry.Name()) {
				t.Errorf("the entry naming %s in %s was not synced", entry.Name(), filepath.ToSlash(relative))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := directories[filepath.Join(archive, backup.FilesDirectory, "case", "payloads")]; !ok {
		t.Fatal("a directory of the stored project was never synced")
	}
}

// A backup whose directory entries cannot be synced is not one a retirement
// can rely on: the archive fails, says the backup was written in full but
// could still be lost, and retains its source.
func TestAnArchiveWhoseBackupCannotBeSyncedRetainsItsSource(t *testing.T) {
	source := newProject(t)
	archive := filepath.Join(caseFolder(t), "archive")
	t.Cleanup(artifactdir.ObserveDirectorySyncsForTest(func(directory string) error {
		if filepath.Clean(directory) == archive {
			return errors.New("injected directory sync failure")
		}
		return nil
	}))
	retirement, err := lifecycle.PreviewRetirement(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Archive(context.Background(), source, archive, retirement.Selection, true); err == nil || !strings.Contains(err.Error(), "the backup was written in full but a power loss could still lose it") {
		t.Fatalf("an archive whose backup could not be synced answered %v", err)
	}
	if _, err := project.Open(source); err != nil {
		t.Fatalf("an archive whose backup could not be synced did not retain its source: %v", err)
	}
}
