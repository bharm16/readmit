package project_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/project"
)

func TestSaveRetainsPreviousDocumentAndRefusesDamagedRecovery(t *testing.T) {
	p, err := project.Create(filepath.Join(t.TempDir(), "workspace"), document())
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(p.Root, project.DocumentName))
	next := p.Document
	next.Settings.Title = "Updated"
	if err := p.Save(next); err != nil {
		t.Fatal(err)
	}
	copies, _ := filepath.Glob(filepath.Join(p.Root, "project.json.recovery-*"))
	if len(copies) != 1 {
		t.Fatalf("wanted one recovery copy, got %d", len(copies))
	}
	retained, _ := os.ReadFile(copies[0])
	if !bytes.Equal(before, retained) {
		t.Fatal("prior document changed")
	}
	if err := p.Save(document()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copies[0], []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(next); err == nil {
		t.Fatal("damaged recovery copy accepted")
	}
	current, _ := os.ReadFile(filepath.Join(p.Root, project.DocumentName))
	if !bytes.Equal(before, current) {
		t.Fatal("failed save changed current document")
	}
}

func TestRecoverSelectedRevisionAndPreserveDamagedCurrent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	p, err := project.Create(root, document())
	if err != nil {
		t.Fatal(err)
	}
	if err := project.WriteRevisions(root, revisions()); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteRevisions(root, project.Revisions{Schema: project.RevisionsSchema}); err != nil {
		t.Fatal(err)
	}
	copies, _ := filepath.Glob(filepath.Join(root, "revisions.json.recovery-*"))
	if len(copies) != 1 {
		t.Fatal("prior revision not retained")
	}
	digest := strings.TrimPrefix(filepath.Base(copies[0]), "revisions.json.recovery-")
	if err := os.WriteFile(filepath.Join(root, project.RevisionsDocumentName), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := project.Recover(p.Root, project.RevisionsDocumentName, digest); err != nil {
		t.Fatal(err)
	}
	got, err := project.ReadRevisions(root)
	if err != nil || len(got.Notes) != 1 || len(got.Revisions) != 1 {
		t.Fatalf("recovery: %+v %v", got, err)
	}
	copies, _ = filepath.Glob(filepath.Join(root, "revisions.json.recovery-*"))
	if len(copies) != 2 {
		t.Fatal("damaged bytes were not retained alongside earlier versions")
	}
}

// digestOf is the name a recovery copy of these bytes is retained under.
func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Every copy Recover can restore is listed with the document it was retained
// for, the digest its name records and its length, and the copy holding the
// document as it stands says so. Recovering one listed copy restores exactly
// its bytes and lists the document it replaced as another copy.
func TestRecoveryCopiesListsWhatRecoverRestores(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	p, err := project.Create(root, document())
	if err != nil {
		t.Fatal(err)
	}
	if copies, err := project.RecoveryCopies(root); err != nil || len(copies) != 0 {
		t.Fatalf("a project nothing replaced listed %+v %v", copies, err)
	}
	original, err := os.ReadFile(filepath.Join(root, project.DocumentName))
	if err != nil {
		t.Fatal(err)
	}
	renamed := p.Document
	renamed.Settings.Title = "Renamed by mistake"
	if err := p.Save(renamed); err != nil {
		t.Fatal(err)
	}
	for _, limits := range []project.Quota{{MaxBytes: 1 << 20, MaxFiles: 100}, {MaxBytes: 2 << 20, MaxFiles: 200}} {
		limits.Schema = project.QuotaSchema
		if err := project.SetQuota(root, limits); err != nil {
			t.Fatal(err)
		}
	}
	quota, err := os.ReadFile(filepath.Join(root, project.QuotaDocumentName+".recovery-"+firstCopy(t, root, project.QuotaDocumentName)))
	if err != nil {
		t.Fatal(err)
	}
	before := treeOf(t, root)
	copies, err := project.RecoveryCopies(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []project.RecoveryCopy{
		{Document: project.DocumentName, Digest: digestOf(original), Size: int64(len(original)), State: project.RecoveryReadable},
		{Document: project.QuotaDocumentName, Digest: digestOf(quota), Size: int64(len(quota)), State: project.RecoveryReadable},
	}
	if !reflect.DeepEqual(copies, want) {
		t.Fatalf("listed %+v, want %+v", copies, want)
	}
	if after := treeOf(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("listing the recovery copies wrote into the project")
	}

	current, err := os.ReadFile(filepath.Join(root, project.DocumentName))
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Recover(root, project.DocumentName, copies[0].Digest); err != nil {
		t.Fatal(err)
	}
	if restored, err := os.ReadFile(filepath.Join(root, project.DocumentName)); err != nil || !bytes.Equal(restored, original) {
		t.Fatalf("recovery restored %q, want the listed copy's bytes: %v", restored, err)
	}
	copies, err = project.RecoveryCopies(root)
	if err != nil {
		t.Fatal(err)
	}
	projectCopies := map[string]project.RecoveryCopy{}
	for _, retained := range copies {
		if retained.Document == project.DocumentName {
			projectCopies[retained.Digest] = retained
		}
	}
	if len(projectCopies) != 2 || !projectCopies[digestOf(original)].Current || projectCopies[digestOf(current)].Current ||
		projectCopies[digestOf(current)].State != project.RecoveryReadable {
		t.Fatalf("after recovery the copies listed %+v", copies)
	}
}

// A copy whose bytes changed is damaged, one the document's reader refuses or
// that is not a regular file is unreadable, and Recover refuses each of them
// as the listing reports it; a file that is not named as a copy of a
// document is not listed at all.
func TestRecoveryCopiesReportsCopiesRecoverRefuses(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	p, err := project.Create(root, document())
	if err != nil {
		t.Fatal(err)
	}
	renamed := p.Document
	renamed.Settings.Title = "Renamed"
	if err := p.Save(renamed); err != nil {
		t.Fatal(err)
	}
	damaged := firstCopy(t, root, project.DocumentName)
	if err := os.WriteFile(filepath.Join(root, project.DocumentName+".recovery-"+damaged), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	later := []byte(`{"schema":"readmit-project/v2"}` + "\n")
	if err := os.WriteFile(filepath.Join(root, project.DocumentName+".recovery-"+digestOf(later)), later, 0600); err != nil {
		t.Fatal(err)
	}
	folder := digestOf([]byte("a folder"))
	if err := os.Mkdir(filepath.Join(root, project.RevisionsDocumentName+".recovery-"+folder), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		project.DocumentName + ".recovery-" + strings.ToUpper(digestOf(later)),
		project.DocumentName + ".recovery-not-a-digest",
		project.DocumentName + ".incomplete",
		"notes.json.recovery-" + digestOf(later),
	} {
		if err := os.WriteFile(filepath.Join(root, name), later, 0600); err != nil {
			t.Fatal(err)
		}
	}
	copies, err := project.RecoveryCopies(root)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]project.RecoveryState{}
	for _, retained := range copies {
		states[retained.Document+" "+retained.Digest] = retained.State
	}
	want := map[string]project.RecoveryState{
		project.DocumentName + " " + damaged:         project.RecoveryDamaged,
		project.DocumentName + " " + digestOf(later): project.RecoveryUnreadable,
		project.RevisionsDocumentName + " " + folder: project.RecoveryUnreadable,
	}
	if !reflect.DeepEqual(states, want) {
		t.Fatalf("listed %v, want %v", states, want)
	}
	current, err := os.ReadFile(filepath.Join(root, project.DocumentName))
	if err != nil {
		t.Fatal(err)
	}
	for _, refused := range []struct{ document, digest, reason string }{
		{project.DocumentName, damaged, "recovery copy is damaged"},
		{project.DocumentName, digestOf(later), project.ErrUnsupportedVersion.Error()},
		{project.RevisionsDocumentName, folder, "recovery copy must be a regular file"},
	} {
		if err := project.Recover(root, refused.document, refused.digest); err == nil || err.Error() != refused.reason {
			t.Fatalf("recovering %s %s: %v, want %q", refused.document, refused.digest, err, refused.reason)
		}
	}
	if after, err := os.ReadFile(filepath.Join(root, project.DocumentName)); err != nil || !bytes.Equal(after, current) {
		t.Fatal("a refused recovery changed the current document")
	}
	if _, err := project.RecoveryCopies(filepath.Join(root, "absent")); err == nil {
		t.Fatal("a folder that is not there listed recovery copies")
	}
}

// firstCopy is the digest of the one recovery copy retained for a document.
func firstCopy(t *testing.T, root, document string) string {
	t.Helper()
	copies, _ := filepath.Glob(filepath.Join(root, document+".recovery-*"))
	if len(copies) != 1 {
		t.Fatalf("wanted one recovery copy of %s, found %d", document, len(copies))
	}
	return strings.TrimPrefix(filepath.Base(copies[0]), document+".recovery-")
}

// treeOf is every file under root with its bytes.
func treeOf(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		files[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
