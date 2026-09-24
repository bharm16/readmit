package artifactdir_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
)

// The sentences an example family reports its failures in. The writer answers
// them exactly, so each is compared by identity.
var (
	errReserve   = errors.New("cannot create example; destination must be new and parent readable and writable")
	errDirectory = errors.New("cannot create example directory")
	errCreate    = errors.New("cannot create example member")
	errWrite     = errors.New("cannot write example member; incomplete example retained")
	errSync      = errors.New("cannot sync example; the example was written in full but a power loss could still lose it")
	errCancelled = errors.New("example write cancelled; incomplete example retained")
)

// example is a family sealed by seal: a manifest, payloads one and two
// directories deep, a nested packet another writer makes under nested/, and
// whatever completion record seal makes.
func example(seal artifactdir.Seal) artifactdir.Family {
	return artifactdir.Family{
		Layout: artifactdir.Layout{
			Noun:               "example",
			AllowedDirectories: []string{"payloads", "payloads/deep"},
			Nested:             []string{"nested"},
			RequiredFiles:      []string{"manifest.json"},
			AllowFile: func(name string) bool {
				return name == "manifest.json" || name == "record.json" || name == "identity.sha256" || name == "manifest.sha256" || strings.HasPrefix(name, "payloads/")
			},
			MaxFiles:     64,
			MaxFileBytes: 1 << 16,
			MaxBytes:     1 << 20,
		},
		Seal:   seal,
		Errors: artifactdir.Errors{Reserve: errReserve, Directory: errDirectory, Create: errCreate, Write: errWrite, Sync: errSync, Cancelled: errCancelled},
	}
}

// rule is one member of the closed set of completion rules: the seal, the
// file its record is written to, the file that is synced holding it (a
// staged record is synced before it is renamed), and the identity it states
// for a manifest.
type rule struct {
	name     string
	seal     artifactdir.Seal
	record   string
	synced   string
	identity func(files map[string][]byte) string
}

var rules = []rule{
	{"directory hash", artifactdir.DirectoryHash("readmit-example/v1"), "identity.sha256", "identity.sha256", func(files map[string][]byte) string {
		return artifactdir.Identity("readmit-example/v1", files)
	}},
	{"manifest hash", artifactdir.ManifestHash("", "manifest.json", "manifest.sha256"), "manifest.sha256", "manifest.sha256", func(files map[string][]byte) string {
		sum := sha256.Sum256(files["manifest.json"])
		return hex.EncodeToString(sum[:])
	}},
	{"manifest hash in a domain", artifactdir.ManifestHash("readmit-example/v1", "manifest.json", "identity.sha256"), "identity.sha256", "identity.sha256", func(files map[string][]byte) string {
		sum := sha256.Sum256(append([]byte("readmit-example/v1\n"), files["manifest.json"]...))
		return hex.EncodeToString(sum[:])
	}},
	{"completion record", artifactdir.CompletionRecord("record.json", ""), "record.json", "record.json", nil},
	{"staged completion record", artifactdir.CompletionRecord("record.json", ".record.incomplete"), "record.json", ".record.incomplete", nil},
}

// recordFor is the completion record a rule's family makes itself, and nil
// for a rule whose record the writer makes.
func (r rule) recordFor() []byte {
	if r.identity == nil {
		return []byte("{\"complete\":true}\n")
	}
	return nil
}

// syncLog records every sync a writer makes, in order: each file with the
// bytes it held and each directory with the names it held.
type syncLog struct {
	mu          sync.Mutex
	made        int
	files       map[string]fileSync
	directories map[string]directorySync
}

type fileSync struct {
	at   int
	data []byte
}

type directorySync struct {
	at    int
	names []string
}

// observeSyncs records every sync started afterwards. A file or directory
// failing answers true for is refused instead, as a failed sync.
func observeSyncs(t *testing.T, failFile, failDirectory func(string) bool) *syncLog {
	t.Helper()
	log := &syncLog{files: map[string]fileSync{}, directories: map[string]directorySync{}}
	t.Cleanup(artifactdir.ObserveFileSyncsForTest(func(path string) error {
		path = filepath.Clean(path)
		if failFile != nil && failFile(path) {
			return errors.New("injected file sync failure")
		}
		data, err := os.ReadFile(path)
		log.mu.Lock()
		defer log.mu.Unlock()
		log.made++
		log.files[path] = fileSync{log.made, data}
		return err
	}))
	t.Cleanup(artifactdir.ObserveDirectorySyncsForTest(func(directory string) error {
		directory = filepath.Clean(directory)
		if failDirectory != nil && failDirectory(directory) {
			return errors.New("injected directory sync failure")
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		log.mu.Lock()
		defer log.mu.Unlock()
		log.made++
		log.directories[directory] = directorySync{log.made, names}
		return nil
	}))
	return log
}

// wholeMap writes the example's files in one call.
func wholeMap(t *testing.T, path string, r rule) (string, error) {
	files := map[string][]byte{"manifest.json": []byte("{\"state\":\"complete\"}\n"), "payloads/a.bin": []byte("a"), "payloads/deep/b.bin": []byte("b")}
	if record := r.recordFor(); record != nil {
		files[r.record] = record
	}
	return artifactdir.Write(context.Background(), path, example(r.seal), artifactdir.Durable, files)
}

// stream writes the example member by member, as a run or a job does: a
// payload, a nested packet another writer makes inside it, a log appended to
// twice, and a manifest written in progress and replaced once complete.
func stream(t *testing.T, path string, r rule) (string, error) {
	w, err := artifactdir.Create(path, example(r.seal), artifactdir.Durable)
	if err != nil {
		return "", err
	}
	defer w.Close()
	if err := w.Mkdir("payloads"); err != nil {
		return "", err
	}
	if err := w.WriteFile("manifest.json", []byte("{\"state\":\"in_progress\"}\n")); err != nil {
		return "", err
	}
	if err := w.WriteFile("payloads/a.bin", []byte("a")); err != nil {
		return "", err
	}
	if _, err := artifactdir.Write(context.Background(), filepath.Join(w.Path(), "nested"), example(artifactdir.DirectoryHash("readmit-nested/v1")), artifactdir.Durable, map[string][]byte{"manifest.json": []byte("{}\n")}); err != nil {
		t.Fatal(err)
	}
	log, err := w.Open("payloads/deep/log.jsonl")
	if err != nil {
		return "", err
	}
	for _, line := range []string{"{\"n\":1}\n", "{\"n\":2}\n"} {
		if err := artifactdir.WriteFileSync(log, []byte(line)); err != nil {
			log.Close()
			return "", err
		}
	}
	if err := log.Close(); err != nil {
		return "", err
	}
	if err := w.Replace("manifest.json", ".manifest.pending", []byte("{\"state\":\"complete\"}\n")); err != nil {
		return "", err
	}
	return w.Seal(r.recordFor())
}

var modes = []struct {
	name  string
	write func(*testing.T, string, rule) (string, error)
}{{"whole map", wholeMap}, {"stream", stream}}

// A sealed directory is found through names: its own in the folder holding
// it, each member's in the directory holding it. So before a write reports a
// directory written, every file is synced holding the bytes it keeps, the
// completion record after every other file, and every directory is synced
// holding every name it keeps: each the writer made, the directory itself and
// the folder holding it after the completion record. Each rule states the
// identity its family's reader verifies.
func TestASealedDirectoryIsReportedWrittenOnlyOnceEveryEntryIsSyncedAfterItsCompletionRecord(t *testing.T) {
	for _, mode := range modes {
		for _, r := range rules {
			t.Run(mode.name+"/"+r.name, func(t *testing.T) {
				log := observeSyncs(t, nil, nil)
				folder := caseFolder(t)
				path := filepath.Join(folder, "example")
				identity, err := mode.write(t, path, r)
				if err != nil {
					t.Fatal(err)
				}
				files, err := artifactdir.Read(path, example(r.seal).Layout)
				if err != nil {
					t.Fatalf("the written directory does not read back through its layout: %v", err)
				}
				if _, err := os.Lstat(filepath.Join(path, ".record.incomplete")); !os.IsNotExist(err) {
					t.Fatalf("a staged completion record was left under its staging name: %v", err)
				}
				if _, err := os.Lstat(filepath.Join(path, ".manifest.pending")); !os.IsNotExist(err) {
					t.Fatalf("a replaced member was left under its staging name: %v", err)
				}
				if r.identity != nil {
					if want := r.identity(files); identity != want || string(files[r.record]) != want+"\n" {
						t.Fatalf("identity %s, record %q, want %s", identity, files[r.record], want)
					}
				} else if identity != "" || string(files[r.record]) != string(r.recordFor()) {
					t.Fatalf("a completion record family answered %q and recorded %q", identity, files[r.record])
				}
				completed, ok := log.files[filepath.Join(path, r.synced)]
				if !ok || string(completed.data) != string(files[r.record]) {
					t.Fatal("the completion record was not synced holding its bytes")
				}
				err = filepath.WalkDir(folder, func(name string, entry fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					relative, _ := filepath.Rel(folder, name)
					relative = filepath.ToSlash(relative)
					if entry.IsDir() {
						synced, ok := log.directories[name]
						entries, readErr := os.ReadDir(name)
						if readErr != nil {
							return readErr
						}
						for _, entry := range entries {
							if !ok || !slices.Contains(synced.names, entry.Name()) {
								t.Errorf("reported written before the entry naming %s in %s was synced", entry.Name(), relative)
							}
						}
						if !strings.HasPrefix(relative, "example/nested") && (!ok || synced.at < completed.at) {
							t.Errorf("the directory %s was not synced after the completion record", relative)
						}
						return nil
					}
					data, err := os.ReadFile(name)
					if err != nil {
						return err
					}
					// A member renamed into place was synced under its staging
					// name, holding the bytes it keeps.
					staged := map[string]string{"manifest.json": ".manifest.pending", r.record: r.synced}
					synced, ok := log.files[name]
					if base, err := filepath.Rel(path, name); err == nil && staged[filepath.ToSlash(base)] != "" && (!ok || string(synced.data) != string(data)) {
						synced, ok = log.files[filepath.Join(path, staged[filepath.ToSlash(base)])]
					}
					if !ok || string(synced.data) != string(data) {
						t.Errorf("reported written before %s was synced holding its bytes", relative)
					}
					if name != filepath.Join(path, r.record) && ok && synced.at > completed.at {
						t.Errorf("%s was synced after the completion record", relative)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// The last syncs a write makes follow its completion record, when every file
// is written and synced. A write one of them fails for does not report
// success, and does not say it left an incomplete directory either: it says,
// in its family's sentence, that the directory was written in full but may not
// survive a power loss, and the directory does read back.
func TestALateDirectorySyncFailureSaysTheDirectoryWasWrittenInFull(t *testing.T) {
	for _, mode := range modes {
		for _, failing := range []string{"payloads/deep", "example", "folder"} {
			t.Run(mode.name+"/"+failing, func(t *testing.T) {
				folder := caseFolder(t)
				path := filepath.Join(folder, "example")
				unsyncable := map[string]string{"payloads/deep": filepath.Join(path, "payloads", "deep"), "example": path, "folder": folder}[failing]
				var completed bool
				observeSyncs(t, nil, func(directory string) bool {
					if _, err := os.Stat(filepath.Join(path, "identity.sha256")); err == nil {
						completed = true
					}
					return directory == unsyncable && completed
				})
				_, err := mode.write(t, path, rules[0])
				if !errors.Is(err, errSync) {
					t.Fatalf("a write whose last syncs failed answered %v, want its family's written-in-full sentence", err)
				}
				if _, err := artifactdir.Read(path, example(rules[0].seal).Layout); err != nil {
					t.Fatalf("the directory said to be written in full does not read back: %v", err)
				}
			})
		}
	}
}

// A family that removes what it did not complete still keeps a directory
// whose last syncs failed: its completion record is written, so what it keeps
// is written in full, and the write says so.
func TestALateDirectorySyncFailureKeepsWhatAFamilyThatRemovesIncompleteOutputWrote(t *testing.T) {
	folder := caseFolder(t)
	path := filepath.Join(folder, "example")
	observeSyncs(t, nil, func(directory string) bool { return directory == folder })
	family := example(rules[0].seal)
	family.Incomplete = artifactdir.RemoveIncomplete
	if _, err := artifactdir.Write(context.Background(), path, family, artifactdir.Durable, map[string][]byte{"manifest.json": []byte("{}\n")}); !errors.Is(err, errSync) {
		t.Fatalf("a write whose last sync failed answered %v", err)
	}
	if _, err := artifactdir.Read(path, family.Layout); err != nil {
		t.Fatalf("the directory written in full was not kept: %v", err)
	}
}

// Replace renames a member written in full under its staging name onto the
// member, so the member never holds a partial write. A staging name another
// write left behind is refused rather than reused, and a rename that fails
// keeps what was staged and says, in the family's sentence, that it could not
// be put in place.
func TestReplaceRefusesAStaleStagingNameAndKeepsWhatItStagedWhenTheRenameFails(t *testing.T) {
	errReplace := errors.New("cannot put example member in place")
	family := example(rules[0].seal)
	family.Errors.Replace = errReplace
	family.Layout.AllowedDirectories = append(family.Layout.AllowedDirectories, "payloads/slot")
	w, err := artifactdir.Create(filepath.Join(caseFolder(t), "example"), family, artifactdir.Durable)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.WriteFile("manifest.json", []byte("{\"state\":\"in_progress\"}\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Replace("manifest.json", ".manifest.pending", []byte("{\"state\":\"complete\"}\n")); err != nil {
		t.Fatal(err)
	}
	if manifest, err := os.ReadFile(filepath.Join(w.Path(), "manifest.json")); err != nil || string(manifest) != "{\"state\":\"complete\"}\n" {
		t.Fatalf("the replaced member holds %q, %v", manifest, err)
	}
	if err := os.WriteFile(filepath.Join(w.Path(), ".stale.pending"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := w.Replace("manifest.json", ".stale.pending", []byte("{}\n")); !errors.Is(err, errCreate) {
		t.Fatalf("a replacement reused a staging name another write left: %v", err)
	}
	if err := w.WriteFile("payloads/slot/member.bin", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := w.Replace("payloads/slot", "payloads/.slot.pending", []byte("x")); !errors.Is(err, errReplace) {
		t.Fatalf("a rename onto a directory answered %v", err)
	}
	if staged, err := os.ReadFile(filepath.Join(w.Path(), "payloads", ".slot.pending")); err != nil || string(staged) != "x" {
		t.Fatalf("a failed rename did not keep what it staged: %q %v", staged, err)
	}
}

// Read admits a nested packet whole by its prefix, an evidence tree through
// its directory and file rules, and refuses an empty directory its layout
// does not accept. A reader whose refusals predate Read reports them in its
// own sentences.
func TestReadAdmitsNestedPacketsAndRefusesInTheReadersOwnSentences(t *testing.T) {
	dir := caseFolder(t)
	for name, data := range map[string]string{"manifest.json": "{}\n", "nested/deep/inner.bin": "inner", "tree/a/b.bin": "b"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(filepath.FromSlash(name))), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	errEmpty, errLink, errFiles, errSize := errors.New("empty"), errors.New("link"), errors.New("files"), errors.New("size")
	layout := artifactdir.Layout{
		Nested:         []string{"nested"},
		AllowDirectory: func(name string) bool { return strings.HasPrefix(name+"/", "tree/") },
		AllowFile:      func(name string) bool { return name == "manifest.json" || strings.HasPrefix(name, "tree/") },
		AllowEmpty:     func(directory string, _ map[string][]byte) bool { return directory == "tree/allowed" },
		MaxFiles:       3,
		MaxFileBytes:   8,
		MaxBytes:       64,
		Refusals:       artifactdir.Refusals{Empty: errEmpty, Link: errLink, Files: errFiles, Size: errSize},
	}
	files, err := artifactdir.Read(dir, layout)
	if err != nil || string(files["nested/deep/inner.bin"]) != "inner" || string(files["tree/a/b.bin"]) != "b" {
		t.Fatalf("read %v: %v", files, err)
	}
	for _, step := range []struct {
		name   string
		change func() error
		want   error
	}{
		{"an accepted empty directory", func() error { return os.Mkdir(filepath.Join(dir, "tree", "allowed"), 0700) }, nil},
		{"an empty directory", func() error { return os.Mkdir(filepath.Join(dir, "tree", "empty"), 0700) }, errEmpty},
		{"a link", func() error {
			if err := os.Remove(filepath.Join(dir, "tree", "empty")); err != nil {
				return err
			}
			return os.Symlink("b.bin", filepath.Join(dir, "tree", "a", "link.bin"))
		}, errLink},
		{"a file past the limit", func() error {
			if err := os.Remove(filepath.Join(dir, "tree", "a", "link.bin")); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "tree", "a", "c.bin"), []byte("c"), 0600)
		}, errFiles},
		{"a file past its size", func() error {
			if err := os.Remove(filepath.Join(dir, "tree", "a", "c.bin")); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "tree", "a", "b.bin"), []byte("too large"), 0600)
		}, errSize},
	} {
		if err := step.change(); err != nil {
			t.Fatal(err)
		}
		if _, err := artifactdir.Read(dir, layout); err != step.want {
			t.Fatalf("%s: read answered %v, want %v", step.name, err, step.want)
		}
	}
}

// A writer syncs the folder holding its directory last, so a folder it can
// create in but cannot open is refused before it creates anything.
func TestAWriterRefusesAFolderItCannotSyncBeforeCreatingAnything(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a folder its owner can create in but not open")
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			folder := caseFolder(t)
			if err := os.Chmod(folder, 0300); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(folder, 0700) })
			if _, err := mode.write(t, filepath.Join(folder, "example"), rules[0]); !errors.Is(err, errReserve) {
				t.Fatalf("a write into a folder that cannot be synced answered %v", err)
			}
			if err := os.Chmod(folder, 0700); err != nil {
				t.Fatal(err)
			}
			if left, err := os.ReadDir(folder); err != nil || len(left) != 0 {
				t.Fatalf("a refused write left %d entries behind: %v", len(left), err)
			}
		})
	}
}

// A member that cannot be synced stops the write before its completion
// record, so what was written is never read as complete. A family that
// retains incomplete output leaves it; one that removes it leaves nothing.
func TestAMemberThatCannotBeSyncedStopsTheWriteBeforeItsCompletionRecord(t *testing.T) {
	for _, mode := range modes {
		for incomplete, name := range map[artifactdir.Incomplete]string{artifactdir.RetainIncomplete: "retained", artifactdir.RemoveIncomplete: "removed"} {
			t.Run(mode.name+"/"+name, func(t *testing.T) {
				path := filepath.Join(caseFolder(t), "example")
				observeSyncs(t, func(file string) bool { return file == filepath.Join(path, "payloads", "a.bin") }, nil)
				family := example(rules[0].seal)
				family.Incomplete = incomplete
				var err error
				if mode.name == "whole map" {
					_, err = artifactdir.Write(context.Background(), path, family, artifactdir.Durable, map[string][]byte{"manifest.json": []byte("{}\n"), "payloads/a.bin": []byte("a")})
				} else {
					var w *artifactdir.Writer
					if w, err = artifactdir.Create(path, family, artifactdir.Durable); err != nil {
						t.Fatal(err)
					}
					if err = w.WriteFile("manifest.json", []byte("{}\n")); err == nil {
						err = w.WriteFile("payloads/a.bin", []byte("a"))
					}
					w.Close()
				}
				if !errors.Is(err, errWrite) {
					t.Fatalf("a write whose member could not be synced answered %v", err)
				}
				_, err = os.Lstat(path)
				switch incomplete {
				case artifactdir.RetainIncomplete:
					if err != nil {
						t.Fatalf("the incomplete directory was not retained: %v", err)
					}
					if _, err := os.Lstat(filepath.Join(path, "identity.sha256")); !os.IsNotExist(err) {
						t.Fatalf("an incomplete directory carries a completion record: %v", err)
					}
				case artifactdir.RemoveIncomplete:
					if !os.IsNotExist(err) {
						t.Fatalf("the incomplete directory was not removed: %v", err)
					}
				}
			})
		}
	}
}

// A completion record that cannot be written is reported in the family's own
// sentence for it when it declares one, and as any member otherwise.
func TestACompletionRecordThatCannotBeSyncedIsReportedInItsFamilysSentence(t *testing.T) {
	errComplete := errors.New("cannot complete example; incomplete example retained")
	for _, declared := range []error{nil, errComplete} {
		path := filepath.Join(caseFolder(t), "example")
		observeSyncs(t, func(file string) bool { return filepath.Base(file) == "identity.sha256" }, nil)
		family := example(rules[0].seal)
		family.Errors.Complete = declared
		_, err := artifactdir.Write(context.Background(), path, family, artifactdir.Durable, map[string][]byte{"manifest.json": []byte("{}\n")})
		want := errWrite
		if declared != nil {
			want = errComplete
		}
		if !errors.Is(err, want) {
			t.Fatalf("a completion record that could not be synced answered %v, want %v", err, want)
		}
	}
}

// A writer makes only what its family's layout admits, never writes the
// completion record as a member, never writes into a directory another writer
// made, and writes nothing once it is complete.
func TestAWriterWritesOnlyTheLayoutItsFamilyDeclares(t *testing.T) {
	w, err := artifactdir.Create(filepath.Join(caseFolder(t), "example"), example(rules[0].seal), artifactdir.Durable)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for name, err := range map[string]error{
		"unexpected.txt":  w.WriteFile("unexpected.txt", []byte("x")),
		"identity.sha256": w.WriteFile("identity.sha256", []byte("x\n")),
		"../escape.bin":   w.WriteFile("../escape.bin", []byte("x")),
		"other/":          w.Mkdir("other"),
	} {
		if err == nil {
			t.Errorf("%s was written although the family does not admit it", name)
		}
	}
	if _, err := artifactdir.Write(context.Background(), filepath.Join(w.Path(), "nested"), example(artifactdir.DirectoryHash("readmit-nested/v1")), artifactdir.Durable, map[string][]byte{"manifest.json": []byte("{}\n")}); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteFile("nested/extra.bin", []byte("x")); !errors.Is(err, errDirectory) {
		t.Fatalf("a writer wrote into a directory another writer made: %v", err)
	}
	if err := w.WriteFile("manifest.json", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Seal(nil); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteFile("payloads/late.bin", []byte("x")); err == nil {
		t.Fatal("a member was written after the completion record")
	}
	if _, err := w.Complete(nil); err == nil {
		t.Fatal("a directory was completed twice")
	}
	entries, err := os.ReadDir(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !slices.Equal(names, []string{"identity.sha256", "manifest.json", "nested"}) {
		t.Fatalf("the writer left %q", names)
	}

	// A family that declares only Write reports every member failure in it.
	bare := artifactdir.Family{Layout: example(rules[0].seal).Layout, Seal: rules[0].seal, Errors: artifactdir.Errors{Write: errWrite}}
	fallback, err := artifactdir.Create(filepath.Join(caseFolder(t), "example"), bare, artifactdir.Durable)
	if err != nil {
		t.Fatal(err)
	}
	defer fallback.Close()
	if err := fallback.Mkdir("other"); !errors.Is(err, errWrite) {
		t.Fatalf("a directory the family does not admit answered %v", err)
	}
	if err := fallback.WriteFile("unexpected.txt", nil); !errors.Is(err, errWrite) {
		t.Fatalf("a member the family does not admit answered %v", err)
	}
	if _, err := artifactdir.Write(context.Background(), filepath.Join(caseFolder(t), "unsealed"), artifactdir.Family{Layout: bare.Layout}, artifactdir.Durable, map[string][]byte{"manifest.json": nil}); err == nil {
		t.Fatal("a family that writes no completion record was written as a sealed directory")
	}
}

// A writer that has not completed can checkpoint: every directory it made,
// the directory itself and the folder holding it are synced holding the
// names it made so far, and a directory another writer made inside it, such
// as a nested packet's, is synced when asked. A failed checkpoint is the
// family's sync sentence.
func TestACheckpointSyncsWhatTheWriterMadeBeforeItCompletes(t *testing.T) {
	log := observeSyncs(t, nil, nil)
	folder := caseFolder(t)
	w, err := artifactdir.Create(filepath.Join(folder, "example"), example(artifactdir.ManifestHash("", "manifest.json", "manifest.sha256")), artifactdir.Durable)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.WriteFile("payloads/deep/a.bin", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := w.Sync(); err != nil {
		t.Fatal(err)
	}
	for directory, name := range map[string]string{folder: "example", w.Path(): "payloads", filepath.Join(w.Path(), "payloads"): "deep", filepath.Join(w.Path(), "payloads", "deep"): "a.bin"} {
		if !slices.Contains(log.directories[directory].names, name) {
			t.Errorf("a checkpoint did not sync the entry naming %s", name)
		}
	}
	if _, err := artifactdir.Write(context.Background(), filepath.Join(w.Path(), "nested"), example(artifactdir.DirectoryHash("readmit-nested/v1")), artifactdir.Durable, map[string][]byte{"manifest.json": []byte("{}\n")}); err != nil {
		t.Fatal(err)
	}
	before := log.directories[filepath.Join(w.Path(), "nested")].at
	if err := w.SyncDirectories("nested"); err != nil {
		t.Fatal(err)
	}
	if log.directories[filepath.Join(w.Path(), "nested")].at <= before {
		t.Fatal("a directory another writer made was not synced when asked")
	}
	observeSyncs(t, nil, func(string) bool { return true })
	if err := w.Sync(); !errors.Is(err, errSync) {
		t.Fatalf("a failed checkpoint answered %v", err)
	}
	if err := w.SyncDirectories("nested"); !errors.Is(err, errSync) {
		t.Fatalf("a failed directory sync answered %v", err)
	}
}

// CreateTemp makes a new directory named by its pattern and a random number
// each time, for a workspace whose name only has to be new, and refuses a
// folder that does not exist rather than making it.
func TestCreateTempMakesANewWorkspaceEachTime(t *testing.T) {
	folder := caseFolder(t)
	workspace := artifactdir.Family{Layout: artifactdir.Layout{AllowFile: func(name string) bool { return name == "note.txt" }}, Errors: artifactdir.Errors{Reserve: errReserve, Write: errWrite}}
	seen := map[string]bool{}
	for range 2 {
		w, err := artifactdir.CreateTemp(folder, "attempt-", workspace, artifactdir.Durable)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.WriteFile("note.txt", []byte("x")); err != nil {
			t.Fatal(err)
		}
		w.Close()
		name := filepath.Base(w.Path())
		if !strings.HasPrefix(name, "attempt-") || filepath.Dir(w.Path()) != folder || seen[name] {
			t.Fatalf("a workspace was made as %s", w.Path())
		}
		seen[name] = true
	}
	if _, err := artifactdir.CreateTemp(filepath.Join(folder, "missing"), "attempt-", workspace, artifactdir.Durable); err == nil {
		t.Fatal("a workspace was made in a folder that does not exist")
	}
}
