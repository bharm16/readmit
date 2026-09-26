package backup_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/project"
)

// booking and acknowledgement are one framed SIU occurrence and the ACK that
// answers it. PID-3 is present, so an index built over this case has something
// to retain and a restore has something to find again.
const booking = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-%d|P|2.5.1\rPID|1||MRN-%d^^^READMIT^MR||DOE^JANE||\"\"|\rSCH|1||||||||||20260101130000\r"

const acknowledgement = "MSH|^~\\&|RECV|LAB|READMIT|TEST|20260101120001||ACK^S12|CTL-%d-A|P|2.5.1\rMSA|AA|CTL-%d\r"

func source(n int) string {
	return "\x0b" + fmt.Sprintf(booking, n, n) + "\x1c\r" + "\x0b" + fmt.Sprintf(acknowledgement, n, n) + "\x1c\r"
}

func builtAt() time.Time { return time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC) }

func importedAt() *time.Time {
	instant := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return &instant
}

// newProject creates an empty project directory in its own temporary folder, so
// a backup destination beside it is never inside it.
func newProject(t *testing.T) *project.Project {
	t.Helper()
	created, err := project.Create(filepath.Join(t.TempDir(), "investigation"), project.Document{
		Schema:            project.Schema,
		Settings:          project.Settings{Title: "Epic scheduling interface", DefaultInterfaceVersion: "siu-2.5.1-v1"},
		InterfaceVersions: []string{"siu-2.5.1-v1"},
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return created
}

// registerCase writes one imported case bundle inside the project and registers
// it, returning the identity the shared reader gave it.
func registerCase(t *testing.T, opened *project.Project, name string, occurrence int) string {
	t.Helper()
	written, err := bundle.Write(filepath.Join(opened.Root, name), []bundle.Input{{
		Path:    "fixture-" + name,
		Data:    []byte(source(occurrence)),
		Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
	}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: importedAt()})
	if err != nil {
		t.Fatalf("write case bundle: %v", err)
	}
	revisions, err := project.ReadRevisions(opened.Root)
	if err != nil {
		t.Fatalf("read revisions: %v", err)
	}
	document, _, err := project.AddCase(opened.Document, revisions, project.Case{
		Name: name, Identity: written.Identity, Schema: written.Manifest.Schema,
		Provenance: string(written.Manifest.Provenance.Mode), Title: "Duplicate appointment after reschedule",
	})
	if err != nil {
		t.Fatalf("register case: %v", err)
	}
	if err := opened.Save(document); err != nil {
		t.Fatalf("save project: %v", err)
	}
	return written.Identity
}

// buildIndex writes a derived index of one registered case beside the project,
// the way `readmit index build` does, and returns the file name.
func buildIndex(t *testing.T, opened *project.Project, caseName, name string) string {
	t.Helper()
	evidence, err := bundle.Open(filepath.Join(opened.Root, caseName))
	if err != nil {
		t.Fatalf("open case: %v", err)
	}
	until := time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC)
	document, err := index.Build(context.Background(), evidence, index.Policy{
		Fields: []string{"PID[1]-3[1]"}, Retention: index.RetainValues, RetainUntil: &until,
	}, builtAt())
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	if _, err := index.Write(filepath.Join(opened.Root, name), document); err != nil {
		t.Fatalf("write index: %v", err)
	}
	return name
}

func create(t *testing.T, root string) (backup.Report, string) {
	t.Helper()
	destination := filepath.Join(t.TempDir(), "backup")
	report, err := backup.Create(context.Background(), root, destination)
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	return report, report.Root
}

func restore(t *testing.T, stored string) (backup.Report, string) {
	t.Helper()
	destination := filepath.Join(t.TempDir(), "restored")
	report, err := backup.Restore(context.Background(), stored, destination, builtAt())
	if err != nil {
		t.Fatalf("restore backup: %v", err)
	}
	return report, report.Root
}

// tree is every file of a directory and its exact bytes, keyed by the path
// relative to that directory. Comparing two trees is how "the same project came
// back" is checked against bytes rather than asserted.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		sum := sha256.Sum256(data)
		files[filepath.ToSlash(relative)] = hex.EncodeToString(sum[:])
		return nil
	}); err != nil {
		t.Fatalf("read directory: %v", err)
	}
	return files
}

func stateOf(t *testing.T, entries []backup.Verification, name string) backup.Verification {
	t.Helper()
	i := slices.IndexFunc(entries, func(entry backup.Verification) bool { return entry.Name == name })
	if i < 0 {
		t.Fatalf("no registered artifact named %q in %+v", name, entries)
	}
	return entries[i]
}

func indexOutcome(t *testing.T, entries []backup.IndexOutcome, name string) backup.IndexOutcome {
	t.Helper()
	i := slices.IndexFunc(entries, func(entry backup.IndexOutcome) bool { return entry.Name == name })
	if i < 0 {
		t.Fatalf("no index named %q in %+v", name, entries)
	}
	return entries[i]
}

// A project backed up in one place and restored in another comes back as the
// same bytes, and every case it registers still opens under the identity the
// project recorded. ADR-0002 makes a bundle identity a hash over relative paths
// and contents only, never absolute paths, which is exactly what this checks.
func TestRestoredProjectKeepsEveryIdentityAtADifferentPath(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "regression", 1)
	registerCase(t, opened, "cancellation", 2)
	revisions, err := project.ReadRevisions(opened.Root)
	if err != nil {
		t.Fatalf("read revisions: %v", err)
	}
	notes, _, err := project.SetNote(opened.Document, revisions, project.Note{Name: "draft", Subject: "regression", Title: "First look", Body: "two\nlines"})
	if err != nil {
		t.Fatalf("set note: %v", err)
	}
	if err := project.WriteRevisions(opened.Root, notes); err != nil {
		t.Fatalf("write revisions: %v", err)
	}
	before := tree(t, opened.Root)

	report, stored := create(t, opened.Root)
	if !report.Complete() || report.Files != len(before) {
		t.Fatalf("backup of an intact project: complete=%t files=%d of %d", report.Complete(), report.Files, len(before))
	}
	for _, name := range []string{"regression", "cancellation"} {
		if entry := stateOf(t, report.Evidence, name); entry.State != backup.Verified || entry.Kind != backup.CaseKind {
			t.Errorf("registered case %q: %+v", name, entry)
		}
	}
	if changed := tree(t, opened.Root); !sameTree(changed, before) {
		t.Errorf("taking a backup changed the project it copied")
	}

	restored, target := restore(t, stored)
	if !restored.Complete() {
		t.Fatalf("restore of an intact backup: %+v", restored)
	}
	if after := tree(t, target); !sameTree(after, before) {
		t.Fatalf("restored project is not the project that was backed up:\n%v\n%v", after, before)
	}
	for _, name := range []string{"regression", "cancellation"} {
		entry := stateOf(t, restored.Evidence, name)
		if entry.Recorded != backup.Verified || entry.State != backup.Verified {
			t.Errorf("restored case %q: %+v", name, entry)
		}
		evidence, err := bundle.Open(filepath.Join(target, name))
		if err != nil {
			t.Fatalf("open restored case %q: %v", name, err)
		}
		if evidence.Identity != entry.Identity {
			t.Errorf("restored case %q opened under a different identity: %s", name, evidence.Identity)
		}
	}
}

func sameTree(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for name, digest := range a {
		if b[name] != digest {
			return false
		}
	}
	return true
}

// Evidence a project registers but no longer holds is recorded as missing,
// restored as missing, and never replaced by anything. The same is true of a
// case the reader refuses and of one whose bytes are no longer the bytes the
// project registered.
func TestIncompleteEvidenceIsRecordedAndRestoredRatherThanReplaced(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "intact", 1)
	registerCase(t, opened, "gone", 2)
	registerCase(t, opened, "damaged", 3)
	registerCase(t, opened, "rewritten", 4)

	// A registered case whose directory is no longer there.
	if err := os.RemoveAll(filepath.Join(opened.Root, "gone")); err != nil {
		t.Fatal(err)
	}
	// A registered case the shared reader refuses: one payload file removed.
	if err := os.Remove(filepath.Join(opened.Root, "damaged", "payloads", "s0001-e000002.bin")); err != nil {
		t.Fatal(err)
	}
	// A registered case that still opens, under evidence the project never
	// recorded. It is replaced wholesale in a staging directory, because a
	// production writer never gains an exception for editing sealed evidence.
	staged := filepath.Join(t.TempDir(), "staged")
	replacement, err := bundle.Write(staged, []bundle.Input{{
		Path: "fixture-rewritten", Data: []byte(source(99)), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
	}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: importedAt()})
	if err != nil {
		t.Fatalf("stage replacement evidence: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(opened.Root, "rewritten")); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(filepath.Join(opened.Root, "rewritten"), os.DirFS(staged)); err != nil {
		t.Fatal(err)
	}

	report, stored := create(t, opened.Root)
	if report.Complete() {
		t.Fatal("a backup of a project missing evidence reported itself complete")
	}
	for name, want := range map[string]backup.State{
		"intact": backup.Verified, "gone": backup.Missing, "damaged": backup.Unreadable, "rewritten": backup.Changed,
	} {
		if entry := stateOf(t, report.Evidence, name); entry.State != want {
			t.Errorf("case %q recorded as %q, want %q", name, entry.State, want)
		}
	}

	restored, target := restore(t, stored)
	if restored.Complete() {
		t.Fatal("a restore of a backup missing evidence reported itself complete")
	}
	for name, want := range map[string]backup.State{
		"intact": backup.Verified, "gone": backup.Missing, "damaged": backup.Unreadable, "rewritten": backup.Changed,
	} {
		entry := stateOf(t, restored.Evidence, name)
		if entry.Recorded != want || entry.State != want {
			t.Errorf("case %q restored as recorded=%q found=%q, want both %q", name, entry.Recorded, entry.State, want)
		}
	}
	// Nothing was manufactured in place of the case that was not there.
	if _, err := os.Lstat(filepath.Join(target, "gone")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a restore created something where the missing case had been")
	}
	// The case the reader refused came back exactly as damaged as it was: the
	// occurrence that is gone is still gone, and the rest is byte-identical.
	if before, after := tree(t, filepath.Join(opened.Root, "damaged")), tree(t, filepath.Join(target, "damaged")); !sameTree(after, before) {
		t.Error("a restore repaired evidence the reader refused")
	}
	// The identity of changed evidence is reported as recorded, never as found.
	entry := stateOf(t, restored.Evidence, "rewritten")
	if entry.Identity == replacement.Identity {
		t.Error("a restore re-identified changed evidence under the bytes it found")
	}
}

// A backup records what it holds, not what it saw: every state in its manifest
// is read back from the files the backup stored. Checking each one against those
// same files is what makes a verification stand on the backup alone, without
// reaching for the project it was taken from.
func TestBackupRecordsWhatItHoldsRatherThanWhatItSaw(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "regression", 1)
	registerCase(t, opened, "gone", 2)
	if err := os.RemoveAll(filepath.Join(opened.Root, "gone")); err != nil {
		t.Fatal(err)
	}
	_, stored := create(t, opened.Root)
	document, err := backup.Verify(stored)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	held := filepath.Join(stored, backup.FilesDirectory)
	// The project document the states were read from is the stored one.
	copied, err := project.Open(held)
	if err != nil {
		t.Fatalf("open the project the backup stored: %v", err)
	}
	if len(copied.Document.Cases) != len(document.Evidence) {
		t.Fatalf("the manifest records %d artifacts, the stored project registers %d", len(document.Evidence), len(copied.Document.Cases))
	}
	for _, entry := range document.Evidence {
		switch entry.State {
		case backup.Verified:
			evidence, err := bundle.Open(filepath.Join(held, entry.Name))
			if err != nil || evidence.Identity != entry.Identity {
				t.Errorf("%q is recorded as verified but the stored files do not open under that identity: %v", entry.Name, err)
			}
		case backup.Missing:
			if _, err := os.Lstat(filepath.Join(held, entry.Name)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%q is recorded as missing but the backup holds something under that name", entry.Name)
			}
		default:
			t.Errorf("%q: unexpected recorded state %q", entry.Name, entry.State)
		}
	}
}

// An interrupted backup carries no completion marker, so it is refused by every
// reader rather than presented as a usable one.
func TestInterruptedBackupIsRefusedRatherThanRestored(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "regression", 1)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	partial := filepath.Join(t.TempDir(), "interrupted")
	if _, err := backup.Create(cancelled, opened.Root, partial); err == nil {
		t.Fatal("a cancelled backup reported success")
	}
	if _, err := os.Stat(filepath.Join(partial, backup.MarkerName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a cancelled backup left a completion marker")
	}
	if _, err := backup.Verify(partial); !errors.Is(err, backup.ErrIncomplete) {
		t.Errorf("verify an interrupted backup: %v", err)
	}
	target := filepath.Join(t.TempDir(), "restored")
	if _, err := backup.Restore(context.Background(), partial, target, builtAt()); !errors.Is(err, backup.ErrIncomplete) {
		t.Errorf("restore an interrupted backup: %v", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Error("restoring an interrupted backup created a destination")
	}

	// The same is true of a completed backup whose marker is removed, which is
	// what a write interrupted after its last file looks like.
	_, stored := create(t, opened.Root)
	if err := os.Remove(filepath.Join(stored, backup.MarkerName)); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Verify(stored); !errors.Is(err, backup.ErrIncomplete) {
		t.Errorf("verify a backup with no marker: %v", err)
	}
}

// A restore is cancellable, and a cancelled one is refused rather than reported
// as a project. What it wrote is retained; what it had not reached is simply not
// there, and nothing stands in its place.
func TestCancelledRestoreIsRefusedAndPutsNothingInPlaceOfWhatItDidNotWrite(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "regression", 1)
	_, stored := create(t, opened.Root)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	target := filepath.Join(t.TempDir(), "restored")
	report, err := backup.Restore(cancelled, stored, target, builtAt())
	if err == nil {
		t.Fatalf("a cancelled restore reported success: %+v", report)
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("a cancelled restore did not report cancellation: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(target, "regression")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a cancelled restore left evidence it had not reached")
	}
	// Part of a project is still part of a project: whatever a cancelled
	// restore did write is read by the same reader, and evidence that never
	// arrived reads as missing rather than as anything else.
	if opened, err := project.Open(target); err == nil {
		for _, entry := range opened.Document.Cases {
			if _, err := bundle.Open(filepath.Join(target, entry.Name)); err == nil {
				t.Errorf("a cancelled restore reported failure but left %q complete", entry.Name)
			}
		}
	}
}

// A backup whose bytes no longer match the backup that was written is refused
// whole. Nothing partial is restored from it, because a restore that wrote the
// files it could still read would be filling the rest from nowhere.
func TestDamagedBackupIsRefusedBeforeAnythingIsRestored(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "regression", 1)

	for _, damage := range []struct {
		name  string
		apply func(t *testing.T, stored string)
		want  error
	}{
		{"altered manifest", func(t *testing.T, stored string) {
			manifest := filepath.Join(stored, backup.DocumentName)
			data, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifest, append(data, ' '), 0600); err != nil {
				t.Fatal(err)
			}
		}, backup.ErrDamaged},
		{"altered stored file", func(t *testing.T, stored string) {
			name := filepath.Join(stored, backup.FilesDirectory, project.DocumentName)
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(name, append(data, ' '), 0600); err != nil {
				t.Fatal(err)
			}
		}, nil},
		{"removed stored file", func(t *testing.T, stored string) {
			if err := os.Remove(filepath.Join(stored, backup.FilesDirectory, project.DocumentName)); err != nil {
				t.Fatal(err)
			}
		}, nil},
		{"unrecorded stored file", func(t *testing.T, stored string) {
			if err := os.WriteFile(filepath.Join(stored, backup.FilesDirectory, "extra.json"), []byte("{}\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}, nil},
		{"removed file directory", func(t *testing.T, stored string) {
			if err := os.RemoveAll(filepath.Join(stored, backup.FilesDirectory)); err != nil {
				t.Fatal(err)
			}
		}, nil},
		// A marker with anything appended to it must not truncate into a match.
		{"extended marker", func(t *testing.T, stored string) {
			marker := filepath.Join(stored, backup.MarkerName)
			data, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(marker, append(data, 'a'), 0600); err != nil {
				t.Fatal(err)
			}
		}, backup.ErrDamaged},
		// Every file copied and the manifest never written is what a backup
		// interrupted after its last file looks like on disk.
		{"no document and no marker", func(t *testing.T, stored string) {
			for _, name := range []string{backup.DocumentName, backup.MarkerName} {
				if err := os.Remove(filepath.Join(stored, name)); err != nil {
					t.Fatal(err)
				}
			}
		}, backup.ErrIncomplete},
	} {
		t.Run(damage.name, func(t *testing.T) {
			_, stored := create(t, opened.Root)
			damage.apply(t, stored)
			err := func() error { _, err := backup.Verify(stored); return err }()
			if err == nil {
				t.Fatal("a damaged backup verified")
			}
			if damage.want != nil && !errors.Is(err, damage.want) {
				t.Fatalf("verify: %v, want %v", err, damage.want)
			}
			target := filepath.Join(t.TempDir(), "restored")
			if _, err := backup.Restore(context.Background(), stored, target, builtAt()); err == nil {
				t.Fatal("a damaged backup restored")
			}
			if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
				t.Error("restoring a damaged backup created a destination")
			}
		})
	}
}

// An index is derived and disposable, so a backup records what it was built
// under and never carries a second copy of the values it retained. A restore
// builds it again from the restored canonical case.
func TestIndexIsRebuiltFromCanonicalEvidenceAndNeverCarried(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "regression", 1)
	name := buildIndex(t, opened, "regression", "regression.index.json")
	retained, err := os.ReadFile(filepath.Join(opened.Root, name))
	if err != nil {
		t.Fatal(err)
	}

	report, stored := create(t, opened.Root)
	if !report.Complete() {
		t.Fatalf("backup of a project with one index: %+v", report)
	}
	if outcome := indexOutcome(t, report.Indexes, name); outcome.Case != "regression" || outcome.State != backup.IndexRecorded || outcome.Path != "" {
		t.Fatalf("recorded index: %+v", outcome)
	}
	// The backup holds the project's files and not the index; the declarations
	// it records are what a restore builds the index again from.
	for path := range tree(t, stored) {
		if strings.Contains(path, name) {
			t.Errorf("the backup carried a copy of a derived index at %q", path)
		}
	}
	document, err := backup.Verify(stored)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(document.Indexes) != 1 {
		t.Fatalf("recorded indexes: %+v", document.Indexes)
	}
	recorded := document.Indexes[0]
	if recorded.Recipe != backup.Declared || recorded.Case != "regression" ||
		!slices.Equal(recorded.Fields, []string{"PID[1]-3[1]"}) || recorded.Retention != string(index.RetainValues) ||
		recorded.RetainUntil == nil || recorded.RetainUntil.Year() != 2099 {
		t.Fatalf("recorded recipe: %+v", recorded)
	}

	restored, target := restore(t, stored)
	if !restored.Complete() {
		t.Fatalf("restore: %+v", restored)
	}
	outcome := indexOutcome(t, restored.Indexes, name)
	if outcome.State != backup.IndexRebuilt || outcome.Path != filepath.Join(target, name) {
		t.Fatalf("rebuilt index: %+v", outcome)
	}
	rebuilt, err := index.Open(outcome.Path)
	if err != nil {
		t.Fatalf("open the rebuilt index: %v", err)
	}
	evidence, err := bundle.Open(filepath.Join(target, "regression"))
	if err != nil {
		t.Fatalf("open restored case: %v", err)
	}
	if err := rebuilt.Describes(evidence); err != nil {
		t.Fatalf("the rebuilt index does not describe the restored case: %v", err)
	}
	result, err := rebuilt.Search(builtAt(), index.Query{Field: "PID[1]-3[1]", Match: index.Equals, Term: []byte("MRN-1^^^READMIT^MR")})
	if err != nil || len(result.Hits) != 1 {
		t.Fatalf("search the rebuilt index: %v %+v", err, result)
	}
	// Built again from the same evidence under the same declarations at the
	// same instant, the index is the same document it was.
	if again, err := os.ReadFile(outcome.Path); err != nil || string(again) != string(retained) {
		t.Errorf("rebuilding did not reproduce the index the project held: %v", err)
	}
}

// An index whose case did not come back is reported, and nothing is written in
// its place. An empty index would claim the case holds none of the values it
// was built to find.
func TestIndexOfEvidenceThatDidNotComeBackIsReportedAndNotWritten(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "regression", 1)
	name := buildIndex(t, opened, "regression", "regression.index.json")
	if err := os.RemoveAll(filepath.Join(opened.Root, "regression")); err != nil {
		t.Fatal(err)
	}

	report, stored := create(t, opened.Root)
	if report.Complete() {
		t.Fatal("a backup of a project whose evidence is gone reported itself complete")
	}
	restored, target := restore(t, stored)
	if outcome := indexOutcome(t, restored.Indexes, name); outcome.State != backup.IndexCaseUnavailable || outcome.Path != "" {
		t.Fatalf("index of missing evidence: %+v", outcome)
	}
	if _, err := os.Lstat(filepath.Join(target, name)); !errors.Is(err, os.ErrNotExist) {
		t.Error("a restore wrote an index for evidence that did not come back")
	}
}

// A damaged index is a rebuild, never a loss: the case stays readable, the
// backup records that it could not read the declarations, and the restore says
// so rather than carrying a derived document nothing stands behind.
func TestDamagedIndexIsReportedAndTheCaseIsUnaffected(t *testing.T) {
	opened := newProject(t)
	identity := registerCase(t, opened, "regression", 1)
	name := buildIndex(t, opened, "regression", "regression.index.json")
	damaged := filepath.Join(opened.Root, name)
	data, err := os.ReadFile(damaged)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["digest"] = strings.Repeat("0", 64)
	rewritten, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(damaged, rewritten, 0600); err != nil {
		t.Fatal(err)
	}

	report, stored := create(t, opened.Root)
	if report.Complete() {
		t.Fatal("a backup whose index could not be read reported itself complete")
	}
	if outcome := indexOutcome(t, report.Indexes, name); outcome.State != backup.IndexUndeclared || outcome.Case != "" {
		t.Fatalf("damaged index: %+v", outcome)
	}
	restored, target := restore(t, stored)
	if outcome := indexOutcome(t, restored.Indexes, name); outcome.State != backup.IndexUndeclared {
		t.Fatalf("restored damaged index: %+v", outcome)
	}
	if _, err := os.Lstat(filepath.Join(target, name)); !errors.Is(err, os.ErrNotExist) {
		t.Error("a restore carried an index whose declarations it could not read")
	}
	// The case is evidence; a damaged index beside it never touches it.
	if entry := stateOf(t, restored.Evidence, "regression"); entry.State != backup.Verified || entry.Identity != identity {
		t.Errorf("a damaged index changed what the case verified as: %+v", entry)
	}
}

// An index describing evidence the project does not register has no canonical
// case to be built from, so it is recorded as such rather than guessed at.
func TestIndexOfUnregisteredEvidenceIsRecordedAsSuch(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "regression", 1)
	name := buildIndex(t, opened, "regression", "regression.index.json")
	// The evidence stays where it is and the project stops registering it.
	document := opened.Document
	document.Cases = nil
	if err := opened.Save(document); err != nil {
		t.Fatalf("save project: %v", err)
	}

	report, stored := create(t, opened.Root)
	if report.Complete() {
		t.Fatal("a backup of an index nothing registers reported itself complete")
	}
	if outcome := indexOutcome(t, report.Indexes, name); outcome.State != backup.IndexUnregistered {
		t.Fatalf("unregistered index: %+v", outcome)
	}
	restored, target := restore(t, stored)
	if outcome := indexOutcome(t, restored.Indexes, name); outcome.State != backup.IndexUnregistered {
		t.Fatalf("restored unregistered index: %+v", outcome)
	}
	if _, err := os.Lstat(filepath.Join(target, name)); !errors.Is(err, os.ErrNotExist) {
		t.Error("a restore wrote an index for evidence nothing registers")
	}
}

// A backup is never written inside the project it copies, and a project is
// never restored inside the backup it comes from.
func TestArtifactPathsAreRefusedInsideTheirOwnSource(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "regression", 1)
	if _, err := backup.Create(context.Background(), opened.Root, filepath.Join(opened.Root, "inside")); err == nil {
		t.Error("a backup was written inside the project it copies")
	}
	if _, err := backup.Create(context.Background(), opened.Root, filepath.Join(opened.Root, "regression", "inside")); err == nil {
		t.Error("a backup was written inside retained case evidence")
	}
	_, stored := create(t, opened.Root)
	if _, err := backup.Restore(context.Background(), stored, filepath.Join(stored, "inside"), builtAt()); err == nil {
		t.Error("a project was restored inside the backup it came from")
	}
	if _, err := backup.Restore(context.Background(), stored, filepath.Join(stored, backup.FilesDirectory, "inside"), builtAt()); err == nil {
		t.Error("a project was restored inside the stored files of its own backup")
	}
	// An existing destination is never overwritten, either way round.
	if _, err := backup.Create(context.Background(), opened.Root, stored); err == nil {
		t.Error("a backup overwrote an existing directory")
	}
	if _, err := backup.Restore(context.Background(), stored, opened.Root, builtAt()); err == nil {
		t.Error("a restore overwrote an existing project")
	}
}

// A project whose own document cannot be read is refused: a backup records what
// a project registers, and there is no honest way to record that from a
// document nothing can read.
func TestProjectWithNoReadableDocumentIsRefused(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "regression", 1)
	if err := os.WriteFile(filepath.Join(opened.Root, project.DocumentName), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "backup")
	if _, err := backup.Create(context.Background(), opened.Root, destination); err == nil {
		t.Fatal("a project with an unreadable document was backed up")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Error("a refused backup created a destination")
	}
}

// Backing up the same project twice produces the same manifest: nothing in a
// backup is read from the clock or from an absolute path.
func TestBackupOfTheSameProjectIsTheSameDocument(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "regression", 1)
	_, first := create(t, opened.Root)
	_, second := create(t, opened.Root)
	left, err := os.ReadFile(filepath.Join(first, backup.DocumentName))
	if err != nil {
		t.Fatal(err)
	}
	right, err := os.ReadFile(filepath.Join(second, backup.DocumentName))
	if err != nil {
		t.Fatal(err)
	}
	if string(left) != string(right) {
		t.Error("two backups of one project recorded different documents")
	}
	marker, err := os.ReadFile(filepath.Join(first, backup.MarkerName))
	if err != nil {
		t.Fatal(err)
	}
	// The marker is independently derived here from the rule the package
	// documents: a digest over the contract name and the manifest bytes.
	sum := sha256.Sum256(append([]byte(backup.Schema+"\n"), left...))
	if string(marker) != hex.EncodeToString(sum[:])+"\n" {
		t.Errorf("completion marker is not the digest of the document it seals")
	}
}

// Every reason a manifest cannot be read, including a forged path in a document
// whose seal was recomputed: the seal detects damage, not forgery, so the path
// rules are what refuse a backup that names somewhere it must not write.
func TestBackupDocumentRefusesEveryMalformedManifest(t *testing.T) {
	valid := backup.Document{
		Schema: backup.Schema,
		Files:  []backup.File{{Path: "project.json", Size: 2, SHA256: strings.Repeat("a", 64)}},
		Evidence: []backup.Artifact{{
			Name: "regression", Kind: backup.CaseKind, Identity: strings.Repeat("b", 64), State: backup.Verified,
		}},
		Indexes: []backup.Index{{Name: "search.json", Recipe: backup.Undeclared, Fields: []string{}}},
	}
	encoded, err := backup.Encode(valid)
	if err != nil {
		t.Fatalf("encode a valid document: %v", err)
	}
	if _, err := backup.Decode(encoded); err != nil {
		t.Fatalf("decode a valid document: %v", err)
	}

	for name, mutate := range map[string]func(document map[string]any){
		"unknown member":    func(d map[string]any) { d["unexpected"] = true },
		"unknown version":   func(d map[string]any) { d["schema"] = "readmit-backup/v2" },
		"traversing path":   func(d map[string]any) { file(d)["path"] = "../escape.json" },
		"absolute path":     func(d map[string]any) { file(d)["path"] = "/etc/passwd" },
		"current directory": func(d map[string]any) { file(d)["path"] = "." },
		"parent element":    func(d map[string]any) { file(d)["path"] = "cases/../../escape" },
		"empty path":        func(d map[string]any) { file(d)["path"] = "" },
		"negative size":     func(d map[string]any) { file(d)["size"] = -1 },
		"short digest":      func(d map[string]any) { file(d)["sha256"] = "abcd" },
		"uppercase digest":  func(d map[string]any) { file(d)["sha256"] = strings.Repeat("A", 64) },
		"unknown state":     func(d map[string]any) { artifact(d)["state"] = "probably-fine" },
		"unknown kind":      func(d map[string]any) { artifact(d)["kind"] = "packet" },
		"artifact path":     func(d map[string]any) { artifact(d)["name"] = "cases/regression" },
		"short identity":    func(d map[string]any) { artifact(d)["identity"] = "abcd" },
		"unknown recipe":    func(d map[string]any) { recipe(d)["recipe"] = "maybe" },
		"index with no name": func(d map[string]any) {
			delete(recipe(d), "name")
		},
		"index with no recipe": func(d map[string]any) {
			delete(recipe(d), "recipe")
		},
		"index with no field list": func(d map[string]any) {
			delete(recipe(d), "fields")
		},
		"index with no retention form": func(d map[string]any) {
			delete(recipe(d), "retention")
		},
		"index with no retention end": func(d map[string]any) {
			delete(recipe(d), "retain_until")
		},
		"index path": func(d map[string]any) { recipe(d)["name"] = "../search.json" },
		"undeclared with case": func(d map[string]any) {
			recipe(d)["case"] = "regression"
		},
		"undeclared with fields": func(d map[string]any) {
			recipe(d)["fields"] = []any{"PID[1]-3[1]"}
		},
		"declared with no policy": func(d map[string]any) {
			recipe(d)["recipe"] = "declared"
			recipe(d)["case"] = "regression"
		},
		"declared with unknown retention": func(d map[string]any) {
			recipe(d)["recipe"] = "declared"
			recipe(d)["case"] = "regression"
			recipe(d)["fields"] = []any{"PID[1]-3[1]"}
			recipe(d)["retention"] = "everything"
		},
		"declared with an uncanonical field": func(d map[string]any) {
			recipe(d)["recipe"] = "declared"
			recipe(d)["case"] = "regression"
			recipe(d)["fields"] = []any{"PID-3"}
			recipe(d)["retention"] = "values"
		},
	} {
		t.Run(name, func(t *testing.T) {
			var document map[string]any
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			mutate(document)
			data, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backup.Decode(data); err == nil {
				t.Fatalf("decoded a manifest with %s", name)
			}
		})
	}

	// Two files recorded out of order, and the same file recorded twice.
	for name, files := range map[string][]backup.File{
		"unsorted": {
			{Path: "revisions.json", Size: 2, SHA256: strings.Repeat("a", 64)},
			{Path: "project.json", Size: 2, SHA256: strings.Repeat("a", 64)},
		},
		"repeated": {
			{Path: "project.json", Size: 2, SHA256: strings.Repeat("a", 64)},
			{Path: "project.json", Size: 2, SHA256: strings.Repeat("a", 64)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			document := valid
			document.Files = files
			if _, err := backup.Encode(document); err == nil {
				t.Fatalf("encoded a manifest recording %s files", name)
			}
		})
	}
}

// A retention end is a declaration with no default. An index that declared one
// and whose manifest no longer states it must be refused, not read as an index
// retained until somebody deletes it: a typed pointer cannot tell an absent
// member from an explicit null, so presence is read from the raw member.
func TestARecordedIndexMustStateItsRetentionEndExplicitly(t *testing.T) {
	until := time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC)
	encoded, err := backup.Encode(backup.Document{
		Schema: backup.Schema,
		Files:  []backup.File{},
		Evidence: []backup.Artifact{{
			Name: "regression", Kind: backup.CaseKind, Identity: strings.Repeat("b", 64), State: backup.Verified,
		}},
		Indexes: []backup.Index{{
			Name: "search.json", Case: "regression", Recipe: backup.Declared,
			Fields: []string{"PID[1]-3[1]"}, Retention: string(index.RetainValues), RetainUntil: &until,
		}},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	read, err := backup.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if read.Indexes[0].RetainUntil == nil || !read.Indexes[0].RetainUntil.Equal(until) {
		t.Fatalf("a declared retention end did not survive a round trip: %+v", read.Indexes[0])
	}

	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	delete(recipe(document), "retain_until")
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Decode(data); err == nil {
		t.Fatal("a recorded index with no retention end was read as one retained indefinitely")
	}
	// An explicit null is how indefinite retention is stated, and it reads back
	// as exactly that.
	recipe(document)["retain_until"] = nil
	if data, err = json.Marshal(document); err != nil {
		t.Fatal(err)
	}
	indefinite, err := backup.Decode(data)
	if err != nil || indefinite.Indexes[0].RetainUntil != nil {
		t.Fatalf("an explicit null retention end: %v %+v", err, indefinite.Indexes)
	}
}

func file(document map[string]any) map[string]any {
	return document["files"].([]any)[0].(map[string]any)
}

func artifact(document map[string]any) map[string]any {
	return document["evidence"].([]any)[0].(map[string]any)
}

func recipe(document map[string]any) map[string]any {
	return document["indexes"].([]any)[0].(map[string]any)
}

// FuzzBackupDocument drives the manifest reader with arbitrary bytes. A
// document it accepts must encode again to exactly the bytes it was decoded
// from, so no input can round-trip into a different backup than it named.
func FuzzBackupDocument(f *testing.F) {
	document := backup.Document{
		Schema: backup.Schema,
		Files:  []backup.File{{Path: "cases/regression/manifest.json", Size: 7, SHA256: strings.Repeat("c", 64)}},
		Evidence: []backup.Artifact{{
			Name: "regression", Kind: backup.CaseKind, Identity: strings.Repeat("d", 64), State: backup.Missing,
		}},
		Indexes: []backup.Index{{
			Name: "search.json", Case: "regression", Recipe: backup.Declared,
			Fields: []string{"PID[1]-3[1]"}, Retention: string(index.RetainStates),
		}},
	}
	seed, err := backup.Encode(document)
	if err != nil {
		f.Fatalf("encode seed: %v", err)
	}
	f.Add(seed)
	f.Add([]byte(`{"schema":"readmit-backup/v1","files":[],"evidence":[],"indexes":[]}`))
	f.Add([]byte(`{"schema":"readmit-backup/v1","files":[{"path":"../x","size":0,"sha256":""}],"evidence":[],"indexes":[]}`))
	f.Add([]byte("{"))
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := backup.Decode(data)
		if err != nil {
			return
		}
		encoded, err := backup.Encode(decoded)
		if err != nil {
			t.Fatalf("a decoded document did not encode again: %v", err)
		}
		again, err := backup.Decode(encoded)
		if err != nil {
			t.Fatalf("an encoded document did not decode again: %v", err)
		}
		second, err := backup.Encode(again)
		if err != nil || string(second) != string(encoded) {
			t.Fatalf("encoding is not stable across a round trip: %v", err)
		}
	})
}

// Cancellation is triggered by observing bytes in the final destination file,
// so this exercises cancellation after copying starts without timing a goroutine.
type cancelAfterWrite struct {
	context.Context
	path string
}

func (c cancelAfterWrite) Err() error {
	if info, err := os.Stat(c.path); err == nil && info.Size() > 0 {
		return context.Canceled
	}
	return nil
}

func TestCancellationDuringFinalCopyDoesNotComplete(t *testing.T) {
	opened := newProject(t)
	if err := os.WriteFile(filepath.Join(opened.Root, "z-last"), make([]byte, 1<<20), 0600); err != nil {
		t.Fatal(err)
	}
	_, stored := create(t, opened.Root)
	t.Run("backup", func(t *testing.T) {
		destination := filepath.Join(t.TempDir(), "backup")
		ctx := cancelAfterWrite{context.Background(), filepath.Join(destination, backup.FilesDirectory, "z-last")}
		if _, err := backup.Create(ctx, opened.Root, destination); err == nil {
			t.Fatal("cancelled final copy completed")
		}
		if _, err := os.Stat(filepath.Join(destination, backup.MarkerName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cancelled backup left a marker: %v", err)
		}
	})
	t.Run("restore", func(t *testing.T) {
		destination := filepath.Join(t.TempDir(), "restored")
		ctx := cancelAfterWrite{context.Background(), filepath.Join(destination, "z-last")}
		if _, err := backup.Restore(ctx, stored, destination, builtAt()); err == nil {
			t.Fatal("cancelled final restore copy completed")
		}
	})
}

func TestExcludedIndexDoesNotConsumeStoredByteLimit(t *testing.T) {
	opened := newProject(t)
	// Sparse files put the scan near its real bound without allocating or
	// copying a GiB. Keep them below the project root: only top-level files
	// can be indexes, and probing these zero-filled files as index documents
	// needlessly reads a GiB. They still count toward the same backup bound.
	// Cancellation stops the first copy after a successful scan.
	dataDir := filepath.Join(opened.Root, "data")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for n := range 16 {
		f, err := os.Create(filepath.Join(dataDir, fmt.Sprintf("data-%02d", n)))
		if err != nil {
			t.Fatal(err)
		}
		err = f.Truncate(backup.MaxFileBytes - 1024)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	data := []byte(`{"schema":"readmit-index/v1"}` + strings.Repeat(" ", 1<<20))
	if err := os.WriteFile(filepath.Join(opened.Root, "derived.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := backup.Create(ctx, opened.Root, filepath.Join(t.TempDir(), "backup"))
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("excluded index prevented the scan reaching copying: %v", err)
	}
}
