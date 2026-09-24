package artifactdir_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
)

// The sentences an example document reports its failures in. The store
// answers them as declared, so each is compared with errors.Is.
var (
	errDocumentDestination = errors.New("cannot write an example document here")
	errDocumentCreate      = errors.New("cannot create the new example document; an interrupted write is retained")
	errDocumentWrite       = errors.New("cannot write the new example document")
	errDocumentInstall     = errors.New("cannot replace the example document")
	errDocumentSync        = errors.New("cannot confirm the example document was retained durably")
	errDocumentInspect     = errors.New("an example document must be a readable regular file")
	errDocumentIrregular   = errors.New("an example document must be a regular file")
	errDocumentOpen        = errors.New("cannot open the example document")
	errDocumentChanged     = errors.New("the example document changed while it was read")
	errDocumentRead        = errors.New("cannot read the example document")
	errDocumentSize        = errors.New("example document exceeds size limit")
	errPreviousIrregular   = errors.New("recovery copy must be a regular file")
	errPreviousDamaged     = errors.New("recovery copy is damaged; current document was not changed")
	errPreviousCreate      = errors.New("cannot retain recovery copy")
	errPreviousWrite       = errors.New("recovery copy incomplete; current document was not changed")
)

// exampleDocument is a document of at most eight bytes, reporting every
// failure in a sentence of its own.
func exampleDocument() artifactdir.Document {
	return artifactdir.Document{
		MaxBytes: 8,
		Errors: artifactdir.DocumentErrors{
			Destination: errDocumentDestination, Create: errDocumentCreate, Write: errDocumentWrite,
			Install: errDocumentInstall, Sync: errDocumentSync,
		},
		Refusals: artifactdir.DocumentRefusals{
			Inspect: errDocumentInspect, Irregular: errDocumentIrregular, Open: errDocumentOpen, Changed: errDocumentChanged,
			Read: errDocumentRead, Size: errDocumentSize,
		},
	}
}

// documentFolder is a new folder holding the document name with data, or
// nothing at the name when data is nil.
func documentFolder(t *testing.T, name string, data []byte) (string, string) {
	t.Helper()
	folder, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, name)
	if data != nil {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return folder, path
}

func readBack(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func names(t *testing.T, folder string) []string {
	t.Helper()
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	listed := []string{}
	for _, entry := range entries {
		listed = append(listed, entry.Name())
	}
	return listed
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// A replacement is written in full and synced under its staged name, renamed
// onto the document, and only then is the folder naming it synced, so a
// replacement the store reports survives a power loss.
func TestReplaceSyncsTheStagedFileAndThenTheFolderNamingIt(t *testing.T) {
	folder, path := documentFolder(t, "doc.json", []byte("old\n"))
	log := observeSyncs(t, nil, nil)
	if err := exampleDocument().Replace(path, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if got := readBack(t, path); string(got) != "new\n" {
		t.Fatalf("the document holds %q", got)
	}
	staged, ok := log.files[filepath.Join(folder, "doc.json.incomplete")]
	if !ok || string(staged.data) != "new\n" {
		t.Fatalf("the staged replacement was not synced holding the new bytes: %+v", log.files)
	}
	synced, ok := log.directories[folder]
	if !ok || synced.at < staged.at || !slices.Equal(synced.names, []string{"doc.json"}) {
		t.Fatalf("the folder was not synced naming only the replaced document after it was written: %+v", log.directories)
	}
}

// The folder is synced after the rename, so a folder that cannot be synced
// is reported: the document was written in full but is not confirmed.
func TestReplaceThatCannotSyncItsFolderSaysSo(t *testing.T) {
	folder, path := documentFolder(t, "doc.json", []byte("old\n"))
	observeSyncs(t, nil, func(directory string) bool { return directory == folder })
	if err := exampleDocument().Replace(path, []byte("new\n")); !errors.Is(err, errDocumentSync) || err.Error() != errDocumentSync.Error() {
		t.Fatalf("an unsynced folder was reported as %v", err)
	}
	if got := readBack(t, path); string(got) != "new\n" || !slices.Equal(names(t, folder), []string{"doc.json"}) {
		t.Fatalf("the replaced document was not left in place alone: %q %v", got, names(t, folder))
	}
}

// A replacement that cannot be written in full leaves the previous document
// exactly as it was and removes its staged file.
func TestReplaceThatCannotWriteLeavesThePreviousDocument(t *testing.T) {
	folder, path := documentFolder(t, "doc.json", []byte("old\n"))
	observeSyncs(t, func(string) bool { return true }, nil)
	if err := exampleDocument().Replace(path, []byte("new\n")); !errors.Is(err, errDocumentWrite) {
		t.Fatalf("a failed write was reported as %v", err)
	}
	if got := readBack(t, path); string(got) != "old\n" || !slices.Equal(names(t, folder), []string{"doc.json"}) {
		t.Fatalf("a failed replacement changed the folder: %q %v", got, names(t, folder))
	}
}

// An interrupted replacement is retained at the staged name and reported,
// never overwritten, and the document is left as it was.
func TestReplaceRefusesARetainedInterruptedReplacement(t *testing.T) {
	folder, path := documentFolder(t, "doc.json", []byte("old\n"))
	if err := os.WriteFile(path+".incomplete", []byte("torn"), 0600); err != nil {
		t.Fatal(err)
	}
	err := exampleDocument().Replace(path, []byte("new\n"))
	if !errors.Is(err, errDocumentCreate) || !errors.Is(err, fs.ErrExist) || err.Error() != errDocumentCreate.Error() {
		t.Fatalf("a retained replacement was reported as %v", err)
	}
	if string(readBack(t, path)) != "old\n" || string(readBack(t, path+".incomplete")) != "torn" {
		t.Fatalf("a refused replacement changed the folder: %v", names(t, folder))
	}
}

// A folder at the document's name is never replaced.
func TestReplaceRefusesAFolderAtTheName(t *testing.T) {
	folder, path := documentFolder(t, "doc.json", nil)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := exampleDocument().Replace(path, []byte("new\n")); !errors.Is(err, errDocumentInstall) {
		t.Fatalf("a folder at the name was reported as %v", err)
	}
	if !slices.Equal(names(t, folder), []string{"doc.json"}) {
		t.Fatalf("a refused replacement left %v", names(t, folder))
	}
}

// Retained evidence is never written into.
func TestReplaceAndCreateRefuseRetainedEvidence(t *testing.T) {
	folder, path := documentFolder(t, "doc.json", nil)
	if err := os.WriteFile(filepath.Join(folder, "identity.sha256"), []byte("x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := exampleDocument().Replace(path, []byte("new\n")); err != errDocumentDestination {
		t.Fatalf("a replacement inside evidence was reported as %v", err)
	}
	if err := exampleDocument().Create(path, []byte("new\n")); err != errDocumentDestination {
		t.Fatalf("a creation inside evidence was reported as %v", err)
	}
	undeclared := exampleDocument()
	undeclared.Errors.Destination = nil
	if err := undeclared.Create(path, []byte("new\n")); err == nil || err.Error() != "output must be outside the immutable input case or enclosing evidence" {
		t.Fatalf("an undeclared refusal was not reported as artifactpath words it: %v", err)
	}
	if !slices.Equal(names(t, folder), []string{"identity.sha256"}) {
		t.Fatalf("a refused write left %v", names(t, folder))
	}
}

// A Scratch document is written through the same staged rename and syncs
// nothing.
func TestScratchDocumentSyncsNothing(t *testing.T) {
	_, path := documentFolder(t, "doc.json", []byte("old\n"))
	log := observeSyncs(t, nil, nil)
	scratch := exampleDocument()
	scratch.Durability = artifactdir.Scratch
	if err := scratch.Replace(path, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if err := scratch.Create(path+".other", []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if len(log.files) != 0 || len(log.directories) != 0 || string(readBack(t, path)) != "new\n" {
		t.Fatalf("a scratch document synced %v and %v", log.files, log.directories)
	}
}

// A document with a flush of its own is flushed by it, in place of the shared
// sync, and its folder is still synced.
func TestADocumentsOwnFlushReplacesTheSharedSync(t *testing.T) {
	folder, path := documentFolder(t, "doc.json", []byte("old\n"))
	log := observeSyncs(t, nil, nil)
	flushed := []string{}
	flushing := exampleDocument()
	flushing.Flush = func(file *os.File) error {
		flushed = append(flushed, filepath.Base(file.Name()))
		return file.Sync()
	}
	if err := flushing.Replace(path, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(flushed, []string{"doc.json.incomplete"}) || len(log.files) != 0 {
		t.Fatalf("flushed %v, shared syncs %v", flushed, log.files)
	}
	if _, ok := log.directories[folder]; !ok {
		t.Fatal("the folder of a document flushed its own way was not synced")
	}
}

// A fixed staging name is exclusive as a suffix is; a temporary one is new
// each time, so one a crash left behind refuses nothing and stays as it was.
func TestStagingNamesAFixedOrATemporaryFile(t *testing.T) {
	folder, path := documentFolder(t, "lease.json", []byte("old\n"))
	named := exampleDocument()
	named.Staging = artifactdir.StagingName("lease.next")
	if err := named.Replace(path, []byte("new\n")); err != nil || !slices.Equal(names(t, folder), []string{"lease.json"}) {
		t.Fatalf("a named staging left %v: %v", names(t, folder), err)
	}
	if err := os.WriteFile(filepath.Join(folder, "lease.next"), []byte("torn"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := named.Replace(path, []byte("newer\n")); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("a retained named staging was reported as %v", err)
	}
	if err := os.WriteFile(filepath.Join(folder, ".lease-1.tmp"), []byte("torn"), 0600); err != nil {
		t.Fatal(err)
	}
	temporary := exampleDocument()
	temporary.Staging = artifactdir.StagingTemp(".lease-*.tmp")
	if err := temporary.Replace(path, []byte("newer\n")); err != nil {
		t.Fatal(err)
	}
	if string(readBack(t, path)) != "newer\n" || !slices.Equal(names(t, folder), []string{".lease-1.tmp", "lease.json", "lease.next"}) {
		t.Fatalf("a temporary staging left %v", names(t, folder))
	}
}

// A document that keeps previous copies keeps the exact bytes each
// replacement replaces under a name recording their digest, shares one copy
// between identical versions and never overwrites one.
func TestReplaceKeepsThePreviousBytesUnderTheirDigest(t *testing.T) {
	folder, path := documentFolder(t, "doc.json", []byte("one\n"))
	keeping := exampleDocument()
	keeping.Previous = &artifactdir.Previous{Irregular: errPreviousIrregular, Damaged: errPreviousDamaged, Create: errPreviousCreate, Write: errPreviousWrite}
	for _, next := range []string{"two\n", "two\n", "one\n", "two\n"} {
		if err := keeping.Replace(path, []byte(next)); err != nil {
			t.Fatal(err)
		}
	}
	first, second := artifactdir.PreviousName("doc.json", digestOf([]byte("one\n"))), artifactdir.PreviousName("doc.json", digestOf([]byte("two\n")))
	if want := slices.Sorted(slices.Values([]string{"doc.json", first, second})); !slices.Equal(names(t, folder), want) {
		t.Fatalf("kept %v, want %v", names(t, folder), want)
	}
	if string(readBack(t, filepath.Join(folder, first))) != "one\n" || string(readBack(t, filepath.Join(folder, second))) != "two\n" {
		t.Fatal("a copy does not hold the bytes its name records")
	}
	want := []artifactdir.PreviousCopy{{"doc.json", digestOf([]byte("one\n"))}, {"doc.json", digestOf([]byte("two\n"))}}
	slices.SortFunc(want, func(a, b artifactdir.PreviousCopy) int { return strings.Compare(a.Digest, b.Digest) })
	listed, err := artifactdir.ListPrevious(folder)
	if err != nil || !slices.Equal(listed, want) {
		t.Fatalf("listed %+v: %v", listed, err)
	}
}

// A copy the store cannot keep refuses the replacement, which leaves the
// document as it was: a damaged copy is never overwritten, and a copy's name
// holding a folder is refused.
func TestReplaceRefusesACopyItCannotKeep(t *testing.T) {
	keeping := exampleDocument()
	keeping.Previous = &artifactdir.Previous{Irregular: errPreviousIrregular, Damaged: errPreviousDamaged, Create: errPreviousCreate, Write: errPreviousWrite}
	copyOfOld := artifactdir.PreviousName("doc.json", digestOf([]byte("old\n")))
	for _, c := range []struct {
		name    string
		prepare func(string) error
		want    error
	}{
		{"damaged", func(copy string) error { return os.WriteFile(copy, []byte("damaged"), 0600) }, errPreviousDamaged},
		{"folder", func(copy string) error { return os.Mkdir(copy, 0700) }, errPreviousIrregular},
	} {
		folder, path := documentFolder(t, "doc.json", []byte("old\n"))
		if err := c.prepare(filepath.Join(folder, copyOfOld)); err != nil {
			t.Fatal(err)
		}
		if err := keeping.Replace(path, []byte("new\n")); err != c.want {
			t.Errorf("%s copy: %v, want %v", c.name, err, c.want)
		}
		if string(readBack(t, path)) != "old\n" || !slices.Equal(names(t, folder), []string{"doc.json", copyOfOld}) {
			t.Errorf("%s copy: the refused replacement changed the folder: %v", c.name, names(t, folder))
		}
	}
	folder, path := documentFolder(t, "doc.json", []byte("old\n"))
	observeSyncs(t, func(file string) bool { return filepath.Base(file) == copyOfOld }, nil)
	if err := keeping.Replace(path, []byte("new\n")); !errors.Is(err, errPreviousWrite) {
		t.Fatalf("an unwritten copy was reported as %v", err)
	}
	if string(readBack(t, path)) != "old\n" || !slices.Equal(names(t, folder), []string{"doc.json", copyOfOld}) {
		t.Fatalf("an unwritten copy was not left for the next replacement to report: %v", names(t, folder))
	}
}

// Only a lowercase SHA-256 after the document's name is a copy's name.
func TestParsePreviousNameAcceptsOnlyTheNameTheStoreGives(t *testing.T) {
	digest := digestOf([]byte("x"))
	if document, parsed, ok := artifactdir.ParsePreviousName(artifactdir.PreviousName("project.json", digest)); !ok || document != "project.json" || parsed != digest {
		t.Fatalf("parsed %q %q %v", document, parsed, ok)
	}
	for _, name := range []string{
		"project.json.recovery-" + digest[:63],
		"project.json.recovery-" + digest + "0",
		"project.json.recovery-not-a-digest",
		"project.json.recovery-" + string(bytes.ToUpper([]byte(digest))),
		".recovery-" + digest,
		"project.json",
	} {
		if _, _, ok := artifactdir.ParsePreviousName(name); ok {
			t.Errorf("%s was taken for a previous copy", name)
		}
	}
}

// A new document is created exclusively, synced, and its folder synced; a
// name already taken is refused and left as it was, and a failed write
// leaves nothing at the name.
func TestCreateIsExclusiveAndDurable(t *testing.T) {
	folder, path := documentFolder(t, "doc.json", nil)
	log := observeSyncs(t, nil, nil)
	if err := exampleDocument().Create(path, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	written, ok := log.files[path]
	synced, synchronized := log.directories[folder]
	if !ok || string(written.data) != "new\n" || !synchronized || synced.at < written.at {
		t.Fatalf("a created document was not synced before its folder: %+v %+v", log.files, log.directories)
	}
	if err := exampleDocument().Create(path, []byte("other\n")); !errors.Is(err, errDocumentCreate) || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("a taken name was reported as %v", err)
	}
	if string(readBack(t, path)) != "new\n" {
		t.Fatal("a refused creation changed the document")
	}
	observeSyncs(t, func(string) bool { return true }, nil)
	if err := exampleDocument().Create(path+".second", []byte("new\n")); !errors.Is(err, errDocumentWrite) {
		t.Fatalf("a failed write was reported as %v", err)
	}
	if !slices.Equal(names(t, folder), []string{"doc.json"}) {
		t.Fatalf("a failed creation left %v", names(t, folder))
	}
}

// A document created by link is never seen partial at its name, and a name
// already taken is refused without being touched.
func TestCreateByLinkPublishesOnlyAWholeDocument(t *testing.T) {
	folder, path := documentFolder(t, "observation.json", nil)
	linked := exampleDocument()
	linked.CreateByLink = true
	linked.Staging = artifactdir.StagingTemp(".readmit-observation-*")
	log := observeSyncs(t, nil, nil)
	if err := linked.Create(path, []byte("whole\n")); err != nil {
		t.Fatal(err)
	}
	if string(readBack(t, path)) != "whole\n" || !slices.Equal(names(t, folder), []string{"observation.json"}) {
		t.Fatalf("created %v", names(t, folder))
	}
	if synced, ok := log.directories[folder]; !ok || !slices.Equal(synced.names, []string{"observation.json"}) {
		t.Fatalf("the folder was not synced naming the created document: %+v", log.directories)
	}
	if err := linked.Create(path, []byte("other\n")); !errors.Is(err, errDocumentInstall) || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("a taken name was reported as %v", err)
	}
	if string(readBack(t, path)) != "whole\n" || !slices.Equal(names(t, folder), []string{"observation.json"}) {
		t.Fatalf("a refused creation changed the folder: %v", names(t, folder))
	}
}

// A begun replacement holds its staged name until it is committed or
// abandoned; one closed after a failed step leaves what was staged.
func TestBeginHoldsTheStagedNameUntilCommitOrAbandon(t *testing.T) {
	folder, path := documentFolder(t, "state.json", []byte("old\n"))
	held, err := exampleDocument().Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exampleDocument().Begin(path); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("a second replacement began while the first was held: %v", err)
	}
	if err := held.Write([]byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if err := held.Commit(); err != nil {
		t.Fatal(err)
	}
	held.Abandon()
	if string(readBack(t, path)) != "new\n" || !slices.Equal(names(t, folder), []string{"state.json"}) {
		t.Fatalf("a committed replacement left %v", names(t, folder))
	}
	abandoned, err := exampleDocument().Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	abandoned.Abandon()
	retained, err := exampleDocument().Begin(path)
	if err != nil {
		t.Fatalf("an abandoned replacement still held its name: %v", err)
	}
	if err := retained.Write([]byte("staged\n")); err != nil {
		t.Fatal(err)
	}
	retained.Close()
	if string(readBack(t, path)) != "new\n" || string(readBack(t, path+".incomplete")) != "staged\n" {
		t.Fatal("a closed replacement did not leave what it staged beside the document")
	}
}

// A read accepts a regular file within the bound and refuses everything else
// in the document's own sentences.
func TestReadIsBoundedAndRefusesAnythingButARegularFile(t *testing.T) {
	folder, path := documentFolder(t, "doc.json", []byte("12345678"))
	document := exampleDocument()
	if data, err := document.Read(path); err != nil || string(data) != "12345678" {
		t.Fatalf("a document at the bound was read as %q, %v", data, err)
	}
	if err := os.WriteFile(path, []byte("123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := document.Read(path); err != errDocumentSize {
		t.Fatalf("a document past the bound was read: %v", err)
	}
	if _, err := document.Read(filepath.Join(folder, "missing.json")); !errors.Is(err, errDocumentInspect) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing document was reported as %v", err)
	}
	if _, err := document.Read(folder); err != errDocumentIrregular {
		t.Fatalf("a folder was reported as %v", err)
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := document.ReadIn(root, "doc.json"); err != errDocumentSize {
		t.Fatalf("a document past the bound was read below its folder: %v", err)
	}
	if _, err := document.ReadIn(root, "missing.json"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing document was reported below its folder as %v", err)
	}
}

// A link at the document's name is refused unless the document follows
// links, and one that does reads what the link leads to.
func TestReadFollowsALinkOnlyWhenTheDocumentSaysSo(t *testing.T) {
	folder, _ := documentFolder(t, "doc.json", []byte("target\n"))
	link := filepath.Join(folder, "link.json")
	if err := os.Symlink("doc.json", link); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}
	document := exampleDocument()
	if _, err := document.Read(link); err != errDocumentIrregular {
		t.Fatalf("a link was reported as %v", err)
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := document.ReadIn(root, "link.json"); err != errDocumentIrregular {
		t.Fatalf("a link below the folder was reported as %v", err)
	}
	document.Links = artifactdir.FollowLinks
	if data, err := document.Read(link); err != nil || string(data) != "target\n" {
		t.Fatalf("a followed link was read as %q, %v", data, err)
	}
	if data, err := document.ReadIn(root, "link.json"); err != nil || string(data) != "target\n" {
		t.Fatalf("a followed link below the folder was read as %q, %v", data, err)
	}
}

// A replacement over a link replaces the entry at the name and leaves what
// the link led to exactly as it was.
func TestReplaceOverALinkLeavesWhatItLedTo(t *testing.T) {
	outside, target := documentFolder(t, "doc.json", []byte("outside\n"))
	folder, path := documentFolder(t, "doc.json", nil)
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}
	if err := exampleDocument().Replace(path, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || string(readBack(t, path)) != "new\n" {
		t.Fatalf("the link was not replaced by a regular file: %v", err)
	}
	if string(readBack(t, target)) != "outside\n" || !slices.Equal(names(t, outside), []string{"doc.json"}) || !slices.Equal(names(t, folder), []string{"doc.json"}) {
		t.Fatal("a replacement wrote through a link")
	}
}

// A document that retains failed writes leaves what a failed write wrote, so
// the next write is refused until someone looks at it.
func TestRetainFailedLeavesWhatAFailedWriteWrote(t *testing.T) {
	folder, path := documentFolder(t, "state.json", []byte("old\n"))
	retaining := exampleDocument()
	retaining.RetainFailed = true
	retaining.Staging = artifactdir.StagingName("state.next")
	observeSyncs(t, func(string) bool { return true }, nil)
	if err := retaining.Replace(path, []byte("new\n")); !errors.Is(err, errDocumentWrite) {
		t.Fatalf("a failed replacement was reported as %v", err)
	}
	if err := retaining.Create(filepath.Join(folder, "clock.json"), []byte("new\n")); !errors.Is(err, errDocumentWrite) {
		t.Fatalf("a failed creation was reported as %v", err)
	}
	if string(readBack(t, path)) != "old\n" || !slices.Equal(names(t, folder), []string{"clock.json", "state.json", "state.next"}) {
		t.Fatalf("failed writes were not retained: %v", names(t, folder))
	}
	if err := retaining.Replace(path, []byte("new\n")); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("a retained staged file did not refuse the next replacement: %v", err)
	}
}

// A document replaced while it is read is read whole, as it was or as it
// became, never refused because the name changed underneath the read.
func TestReadOfADocumentReplacedUnderneathIsWhole(t *testing.T) {
	_, path := documentFolder(t, "live.json", []byte("even\n"))
	document := exampleDocument()
	document.Staging = artifactdir.StagingTemp(".live-*")
	document.Durability = artifactdir.Scratch
	for _, links := range []artifactdir.Links{artifactdir.RefuseLinks, artifactdir.FollowLinks} {
		document.Links = links
		done := make(chan struct{})
		failed := make(chan error, 1)
		go func() {
			defer close(done)
			for i := range 400 {
				if err := document.Replace(path, []byte([]string{"even\n", "odd\n"}[i%2])); err != nil {
					failed <- err
					return
				}
				time.Sleep(100 * time.Microsecond)
			}
		}()
	reading:
		for {
			select {
			case <-done:
				break reading
			default:
			}
			data, err := document.Read(path)
			if err != nil || (string(data) != "even\n" && string(data) != "odd\n") {
				t.Fatalf("a document replaced underneath was read as %q, %v", data, err)
			}
		}
		select {
		case err := <-failed:
			t.Fatal(err)
		default:
		}
	}
}

// A document that keeps previous copies is replaced whole, never begun.
func TestBeginRefusesADocumentThatKeepsPreviousCopies(t *testing.T) {
	folder, path := documentFolder(t, "doc.json", []byte("old\n"))
	keeping := exampleDocument()
	keeping.Previous = &artifactdir.Previous{}
	if _, err := keeping.Begin(path); err == nil {
		t.Fatal("a document keeping previous copies began a replacement")
	}
	if !slices.Equal(names(t, folder), []string{"doc.json"}) {
		t.Fatalf("a refused replacement left %v", names(t, folder))
	}
}

// A step declared as FilesystemReport reports the filesystem's own error, for
// a caller that has always shown it; any other step keeps its sentence.
func TestFilesystemReportIsTheFilesystemsOwnError(t *testing.T) {
	_, path := documentFolder(t, "doc.json", []byte("old\n"))
	if err := os.WriteFile(path+".incomplete", []byte("torn"), 0600); err != nil {
		t.Fatal(err)
	}
	reporting := exampleDocument()
	reporting.Errors.Create = artifactdir.FilesystemReport
	err := reporting.Replace(path, []byte("new\n"))
	var filesystem *fs.PathError
	if !errors.As(err, &filesystem) || !errors.Is(err, fs.ErrExist) || errors.Is(err, artifactdir.FilesystemReport) {
		t.Fatalf("a create failure declared as the filesystem's own was reported as %v", err)
	}
}
