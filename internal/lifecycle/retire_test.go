package lifecycle_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/lifecycle"
	"github.com/bharm16/readmit/internal/project"
)

// Retire deletes a source only against a verified archive that accounts for
// it as it stands and the selection the archive was taken under; anything
// else retains the source, and a removal that stops part way names the
// remainder and refuses to be retried over it.
func TestRetireDeletesOnlyAgainstTheVerifiedArchiveOfTheSource(t *testing.T) {
	ctx := context.Background()
	root := workspace(t)
	preview, err := lifecycle.PreviewRetirement(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "archive")
	if report, err := lifecycle.Archive(ctx, root, archive, preview.Selection, false); err != nil || !report.Complete() {
		t.Fatalf("archive: %+v %v", report, err)
	}

	// A different archive, of another project, never authorizes the delete.
	other := workspace(t)
	opened, err := project.Open(other)
	if err != nil {
		t.Fatal(err)
	}
	opened.Document.Settings.Title = "Another investigation"
	if err := opened.Save(opened.Document); err != nil {
		t.Fatal(err)
	}
	otherPreview, err := lifecycle.PreviewRetirement(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "foreign")
	if _, err := lifecycle.Archive(ctx, other, foreign, otherPreview.Selection, false); err != nil {
		t.Fatal(err)
	}
	if outcome, err := lifecycle.Retire(ctx, root, foreign, preview.Selection); err == nil || outcome.State != lifecycle.Retained {
		t.Fatalf("a foreign archive authorized a delete: %+v %v", outcome, err)
	}
	// A source changed after its archive is retained.
	changed, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	changed.Document.Settings.Title = "Edited"
	if err := changed.Save(changed.Document); err != nil {
		t.Fatal(err)
	}
	if outcome, err := lifecycle.Retire(ctx, root, archive, preview.Selection); err == nil || outcome.State != lifecycle.Retained {
		t.Fatalf("a changed source was deleted: %+v %v", outcome, err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("a refused retirement removed the source")
	}

	// Archived again as it stands, the source is deleted and nothing else is.
	fresh, err := lifecycle.PreviewRetirement(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(t.TempDir(), "second")
	if _, err := lifecycle.Archive(ctx, root, second, fresh.Selection, false); err != nil {
		t.Fatal(err)
	}
	neighbour := filepath.Join(filepath.Dir(root), "unrelated.txt")
	if err := os.WriteFile(neighbour, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome, err := lifecycle.Retire(ctx, root, second, fresh.Selection)
	if err != nil || outcome.State != lifecycle.Deleted || outcome.Remainder != "" {
		t.Fatalf("retire: %+v %v", outcome, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("the source is still there")
	}
	if _, err := os.Stat(neighbour); err != nil {
		t.Fatal("retiring a project removed a file beside it")
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatal("retiring a project removed its archive")
	}
}

func TestRetireReportsARemainderAndRefusesToRetryOverIt(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a directory's write permission does not hold here")
	}
	ctx := context.Background()
	root := workspace(t)
	if err := os.Mkdir(filepath.Join(root, "notes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	preview, err := lifecycle.PreviewRetirement(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "archive")
	if _, err := lifecycle.Archive(ctx, root, archive, preview.Selection, false); err != nil {
		t.Fatal(err)
	}
	remainder := root + ".retiring"
	// The renamed source's inner folder refuses the unlink of its contents.
	if err := os.Chmod(filepath.Join(root, "notes"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(remainder, "notes"), 0o700) })
	outcome, err := lifecycle.Retire(ctx, root, archive, preview.Selection)
	if err == nil || outcome.State != lifecycle.RemovalIncomplete || outcome.Remainder != remainder {
		t.Fatalf("an incomplete removal: %+v %v", outcome, err)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatal("the archive was not retained")
	}
	again, err := lifecycle.Retire(ctx, root, archive, preview.Selection)
	if err == nil || again.State != lifecycle.RemovalIncomplete || again.Remainder != remainder || !strings.Contains(err.Error(), "remainder") {
		t.Fatalf("a retry over a remainder: %+v %v", again, err)
	}
}

// A copy is verified against its source file by file, digest by digest,
// indexes included, and differs in nothing but where it is.
func TestVerifyCopyComparesEveryFileOfTheCopy(t *testing.T) {
	ctx := context.Background()
	root := workspace(t)
	copied := filepath.Join(t.TempDir(), "moved")
	if err := lifecycle.Copy(ctx, root, copied); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.VerifyCopy(ctx, root, copied); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := lifecycle.Copy(ctx, root, copied); err == nil {
		t.Fatal("a copy was written over an existing folder")
	}
	if err := os.WriteFile(filepath.Join(copied, "extra.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.VerifyCopy(ctx, root, copied); err == nil {
		t.Fatal("a copy holding an extra file was verified")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := lifecycle.Copy(cancelled, root, filepath.Join(t.TempDir(), "stopped")); err == nil {
		t.Fatal("a cancelled copy reported success")
	}
}
