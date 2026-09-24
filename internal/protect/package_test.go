package protect

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func packed(t *testing.T) (string, []Source) {
	t.Helper()
	root := t.TempDir()
	evidence := filepath.Join(root, "run-2026-09-18")
	if err := os.MkdirAll(filepath.Join(evidence, "observations"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"manifest.json":             `{"schema":"readmit-run/v1"}`,
		"observations/ledger.json":  `{"schema":"readmit-observation/v1","subject":"SYNTH-001"}`,
		"observations/request.mllp": "MSH|^~\\&|READMIT|TEST",
	} {
		if err := os.WriteFile(filepath.Join(evidence, filepath.FromSlash(name)), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sources, notRead, err := Collect([]string{evidence})
	if err != nil || notRead != 0 {
		t.Fatalf("collect: %v, %d not read", err, notRead)
	}
	if len(sources) != 3 || sources[0].Name != "run-2026-09-18/manifest.json" {
		t.Fatalf("collect recorded %v", names(sources))
	}
	return root, sources
}

func names(sources []Source) []string {
	recorded := make([]string, 0, len(sources))
	for _, source := range sources {
		recorded = append(recorded, source.Name)
	}
	return recorded
}

func packInto(t *testing.T, entry Control, sources []Source, output string) Package {
	t.Helper()
	t.Setenv(storeSwitch, "emit")
	descriptor, err := Pack(context.Background(), entry, sources, 0, output, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	return descriptor
}

func TestPackAndOpenPreserveEveryByteAndLeaveTheSourcesUntouched(t *testing.T) {
	root, sources := packed(t)
	entry := control(t, "lab-evidence", testOnlyKey)
	before, err := os.ReadFile(filepath.Join(root, "run-2026-09-18", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	descriptor := packInto(t, entry, sources, filepath.Join(root, "packet"))
	if descriptor.Cipher != Cipher || descriptor.Derivation != Derivation || descriptor.Generation != 1 {
		t.Fatalf("descriptor declares %+v", descriptor)
	}
	if !descriptor.RetainUntil.Equal(time.Date(2026, 12, 17, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("declared retention resolved to %s", descriptor.RetainUntil)
	}
	after, err := os.ReadFile(filepath.Join(root, "run-2026-09-18", "manifest.json"))
	if err != nil || string(after) != string(before) {
		t.Fatal("packing altered the evidence it read")
	}
	opened := filepath.Join(root, "opened")
	reopened, index, err := Open(context.Background(), entry, filepath.Join(root, "packet"), opened)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if reopened.Package != descriptor.Package || len(index.Entries) != 3 {
		t.Fatalf("reopened %s with %d entries", reopened.Package, len(index.Entries))
	}
	for _, source := range sources {
		got, err := os.ReadFile(filepath.Join(opened, filepath.FromSlash(source.Name)))
		if err != nil || string(got) != string(source.Data) {
			t.Fatalf("%s reopened as %q (%v)", source.Name, got, err)
		}
	}
}

// A package's own index is sensitive data, not harmless metadata: the one
// plaintext file of a package must name no packed file and hold no content.
func TestThePlaintextDescriptorNamesNothingItProtects(t *testing.T) {
	root, sources := packed(t)
	packInto(t, control(t, "lab-evidence", testOnlyKey), sources, filepath.Join(root, "packet"))
	manifest, err := os.ReadFile(filepath.Join(root, "packet", DescriptorName))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"run-2026-09-18", "manifest.json", "ledger.json", "SYNTH-001", "readmit-run", "MSH", testOnlyKey} {
		if strings.Contains(string(manifest), leak) {
			t.Errorf("the plaintext descriptor holds %q", leak)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "packet"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode&0077 != 0 {
			t.Errorf("%s is mode %04o, not owner-only", entry.Name(), mode)
		}
		body, err := os.ReadFile(filepath.Join(root, "packet", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name() != DescriptorName && strings.Contains(string(body), "SYNTH-001") {
			t.Errorf("%s holds plaintext evidence", entry.Name())
		}
	}
}

func TestOpenRefusesAWrongKeyARotatedAwayKeyAndAnotherControl(t *testing.T) {
	root, sources := packed(t)
	entry := control(t, "lab-evidence", testOnlyKey)
	packInto(t, entry, sources, filepath.Join(root, "packet"))

	other := entry
	other.Arguments = []string{locator(t, testOnlyOtherKey)}
	assertOpenFails(t, other, root, "was not written with the key", "a wrong key")

	rotated := other
	rotated.Generation = 2
	assertOpenFails(t, rotated, root, "earlier key generation", "a rotated-away key")

	renamed := entry
	renamed.Name = "other-control"
	assertOpenFails(t, renamed, root, "different protection control", "another control")

	t.Setenv(storeSwitch, "fail")
	if _, _, err := Open(context.Background(), entry, filepath.Join(root, "packet"), filepath.Join(root, "out-store")); err == nil {
		t.Error("a store that did not answer was treated as an open")
	}
	t.Setenv(storeSwitch, "short")
	if _, _, err := Open(context.Background(), entry, filepath.Join(root, "packet"), filepath.Join(root, "out-short")); err == nil {
		t.Error("key material below the declared floor was accepted")
	}
}

func assertOpenFails(t *testing.T, entry Control, root, want, label string) {
	t.Helper()
	t.Setenv(storeSwitch, "emit")
	output := filepath.Join(root, "out-"+strings.ReplaceAll(label, " ", "-"))
	_, _, err := Open(context.Background(), entry, filepath.Join(root, "packet"), output)
	if err == nil {
		t.Fatalf("%s opened the package", label)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("%s reported %q, want a message naming %q", label, err, want)
	}
	if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("%s left an output directory behind", label)
	}
}

func TestOpenRefusesATruncatedEntryAndACoherentlyResealedOne(t *testing.T) {
	root, sources := packed(t)
	entry := control(t, "lab-evidence", testOnlyKey)
	descriptor := packInto(t, entry, sources, filepath.Join(root, "packet"))
	target := filepath.Join(root, "packet", descriptor.Entries[0].ID+entrySuffix)

	sealed, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	rewrite(t, target, sealed[:len(sealed)-1])
	assertOpenFails(t, entry, root, "size and digest the descriptor records", "a truncated entry")

	// Altering an entry and resealing the descriptor over it defeats the digest
	// check. The authenticated-encryption tag, and only it, ties an entry to
	// the key, so the alteration is still named.
	altered := append([]byte(nil), sealed...)
	altered[0] ^= 0xff
	rewrite(t, target, altered)
	reseal(t, root, descriptor, 0, altered)
	assertOpenFails(t, entry, root, "failed authentication", "a resealed entry")
}

// An entry is bound to its package and its identifier, so an entry moved from
// another package written with the same key is refused rather than decrypted.
func TestOpenRefusesAnEntryMovedFromAnotherPackage(t *testing.T) {
	root, sources := packed(t)
	entry := control(t, "lab-evidence", testOnlyKey)
	first := packInto(t, entry, sources, filepath.Join(root, "packet"))
	packInto(t, entry, sources, filepath.Join(root, "second"))
	foreign, err := os.ReadFile(filepath.Join(root, "second", first.Entries[0].ID+entrySuffix))
	if err != nil {
		t.Fatal(err)
	}
	rewrite(t, filepath.Join(root, "packet", first.Entries[0].ID+entrySuffix), foreign)
	reseal(t, root, first, 0, foreign)
	assertOpenFails(t, entry, root, "failed authentication", "a foreign entry")
}

func rewrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

// reseal rewrites the descriptor so it agrees with an altered entry, the way an
// audit-hardening test forges coherent evidence rather than obvious damage.
func reseal(t *testing.T, root string, descriptor Package, position int, sealed []byte) {
	t.Helper()
	digest := sha256.Sum256(sealed)
	descriptor.Entries[position].Bytes = int64(len(sealed))
	descriptor.Entries[position].SHA256 = digest[:]
	encoded, err := json.Marshal(descriptor, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	rewrite(t, filepath.Join(root, "packet", DescriptorName), append(encoded, '\n'))
}

func TestPackRefusesARetiredControlAnEmptySetAndAnOutputInsideEvidence(t *testing.T) {
	root, sources := packed(t)
	entry := control(t, "lab-evidence", testOnlyKey)
	t.Setenv(storeSwitch, "emit")

	retired := entry
	retired.State = Retired
	if _, err := Pack(context.Background(), retired, sources, 0, filepath.Join(root, "retired"), time.Now()); err == nil {
		t.Error("a retired control wrote a new package")
	}
	if _, err := Pack(context.Background(), entry, nil, 0, filepath.Join(root, "empty"), time.Now()); err == nil {
		t.Error("an empty package was written")
	}

	// Retained evidence is immutable: a package is a new artifact beside it,
	// never a write inside it.
	evidence := filepath.Join(root, "sealed-case")
	if err := os.Mkdir(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidence, "identity.sha256"), []byte("0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Pack(context.Background(), entry, sources, 0, filepath.Join(evidence, "packet"), time.Now()); err == nil {
		t.Error("a package was written inside retained evidence")
	}

	packInto(t, entry, sources, filepath.Join(root, "packet"))
	if _, err := Pack(context.Background(), entry, sources, 0, filepath.Join(root, "packet"), time.Now()); err == nil {
		t.Error("an existing destination was overwritten")
	}
	if _, _, err := Open(context.Background(), entry, filepath.Join(root, "packet"), filepath.Join(evidence, "opened")); err == nil {
		t.Error("a package was opened inside retained evidence")
	}
}

// Retirement is not revocation: a retired control still opens what it wrote.
func TestARetiredControlStillOpensThePackagesItWrote(t *testing.T) {
	root, sources := packed(t)
	entry := control(t, "lab-evidence", testOnlyKey)
	packInto(t, entry, sources, filepath.Join(root, "packet"))
	retired := entry
	retired.State = Retired
	t.Setenv(storeSwitch, "emit")
	if _, _, err := Open(context.Background(), retired, filepath.Join(root, "packet"), filepath.Join(root, "opened")); err != nil {
		t.Fatalf("a retired control did not open its own package: %v", err)
	}
}

func TestReadPackageRefusesWhatItDoesNotDefine(t *testing.T) {
	root, sources := packed(t)
	packInto(t, control(t, "lab-evidence", testOnlyKey), sources, filepath.Join(root, "packet"))
	manifest := filepath.Join(root, "packet", DescriptorName)
	original, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, refused := range []struct{ name, replacement string }{
		{"an unknown version", `{"schema":"readmit-transfer/v2"}`},
		{"an unknown member", strings.Replace(string(original), `"cipher"`, `"compression":"gzip","cipher"`, 1)},
		{"an unread cipher", strings.Replace(string(original), Cipher, "aes-128-cbc", 1)},
		{"an unread kdf", strings.Replace(string(original), Derivation, "pbkdf2-sha1", 1)},
		{"a rewritten identity", strings.Replace(string(original), `"package":"`, `"package":"zz`, 1)},
		{"a truncated file", string(original[:len(original)/2])},
	} {
		rewrite(t, manifest, []byte(refused.replacement))
		if _, _, err := ReadPackage(filepath.Join(root, "packet")); err == nil {
			t.Errorf("%s was accepted", refused.name)
		}
		if _, err := DecodePackage([]byte(refused.replacement)); err == nil {
			t.Errorf("%s decoded", refused.name)
		}
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadPackage(filepath.Join(root, "packet")); err == nil {
		t.Error("a directory holding no descriptor was read as a package")
	}
	if _, err := DecodePackage([]byte(`{"schema":"readmit-transfer/v2"}`)); !errors.Is(err, ErrUnsupportedPackage) {
		t.Error("a later package version is not reported as the version it declares")
	}
	if _, err := DecodeIndex([]byte(`{"schema":"readmit-transfer-index/v2"}`)); !errors.Is(err, ErrUnsupportedPackage) {
		t.Error("a later index version is not reported as the version it declares")
	}
	if _, _, err := ReadPackage(filepath.Join(root, "missing")); err == nil {
		t.Error("a directory that is not a package was read as one")
	}
}

// discardAt is well past the retention every packed fixture declares, so the
// retention gate is exercised on its own rather than incidentally here.
var discardAt = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

func TestDiscardRemovesOnlyWhatThePackageDeclares(t *testing.T) {
	root, sources := packed(t)
	entry := control(t, "lab-evidence", testOnlyKey)
	descriptor := packInto(t, entry, sources, filepath.Join(root, "packet"))

	stray := filepath.Join(root, "packet", "notes.txt")
	if err := os.WriteFile(stray, []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Discard(filepath.Join(root, "packet"), discardAt, false); err == nil {
		t.Error("a package directory holding an undeclared file was removed")
	}
	if _, err := os.Stat(filepath.Join(root, "packet", DescriptorName)); err != nil {
		t.Fatal("a refused discard removed something anyway")
	}
	if err := os.Remove(stray); err != nil {
		t.Fatal(err)
	}
	discarded, removed, err := Discard(filepath.Join(root, "packet"), discardAt, false)
	if err != nil {
		t.Fatalf("discard: %v", err)
	}
	if discarded.Package != descriptor.Package || removed != len(descriptor.Entries)+2 {
		t.Fatalf("discard removed %d files of %s", removed, discarded.Package)
	}
	if _, err := os.Stat(filepath.Join(root, "packet")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the package directory was left behind")
	}
	if _, _, err := Discard(filepath.Join(root, "packet"), discardAt, false); err == nil {
		t.Error("a removed package was discarded twice")
	}
}

func TestCollectRefusesNamesAPackageCouldNotWriteBackOut(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	for _, directory := range []string{first, second} {
		if err := os.Mkdir(filepath.Join(directory, "run"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "run", "manifest.json"), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := Collect([]string{filepath.Join(first, "run"), filepath.Join(second, "run")}); err == nil {
		t.Error("two paths were recorded under the same name")
	}

	// A name that is a path prefix of another is worse than a duplicate: the
	// package would be written, and an open could never create a directory
	// where a file of that name already exists.
	prefixed := filepath.Join(t.TempDir(), "run")
	if err := os.WriteFile(prefixed, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Collect([]string{prefixed, filepath.Join(first, "run")}); err == nil {
		t.Error("a file and a directory sharing a final name were both packed")
	}
	if _, _, err := Collect([]string{filepath.Join(first, "absent")}); err == nil {
		t.Error("a missing path was collected")
	}
	for name, recorded := range map[string]string{
		"a parent traversal": "../escape.json",
		"an absolute path":   "/etc/hosts",
		"a bare parent":      "..",
		"an empty name":      "",
	} {
		if err := EntryName(recorded); err == nil {
			t.Errorf("%s was accepted as a packed name", name)
		}
	}
}

func FuzzDecodePackage(f *testing.F) {
	f.Add(`{"schema":"readmit-transfer/v1"}`)
	f.Add(`{"schema":"readmit-transfer/v2"}`)
	f.Add(`{"schema":"readmit-transfer/v1","package":"00000000000000000000000000000000","created_at":"2026-09-18T12:00:00Z","control":"lab","generation":1,"cipher":"aes-256-gcm","derivation":"hkdf-sha256","salt":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=","key_check":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=","index":{"id":"index","nonce":"AAAAAAAAAAAAAAAA","bytes":32,"sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},"entries":[]}`)
	f.Fuzz(func(t *testing.T, raw string) {
		descriptor, err := DecodePackage([]byte(raw))
		if err != nil {
			return
		}
		encoded, err := json.Marshal(descriptor, json.Deterministic(true))
		if err != nil {
			t.Fatalf("an accepted descriptor did not re-encode: %v", err)
		}
		again, err := DecodePackage(encoded)
		if err != nil {
			t.Fatalf("a re-encoded descriptor did not decode: %v", err)
		}
		if again.Package != descriptor.Package || len(again.Entries) != len(descriptor.Entries) {
			t.Fatal("re-encoding changed what a descriptor declares")
		}
	})
}

func FuzzDecodeIndex(f *testing.F) {
	f.Add(`{"schema":"readmit-transfer-index/v1","package":"00000000000000000000000000000000","entries":[],"entries_not_read":0}`)
	f.Add(`{"schema":"readmit-transfer-index/v1","package":"00000000000000000000000000000000","entries":[{"id":"e0001","name":"run/manifest.json","bytes":2}],"entries_not_read":1}`)
	f.Add(`{"schema":"readmit-transfer-index/v2"}`)
	f.Fuzz(func(t *testing.T, raw string) {
		index, err := DecodeIndex([]byte(raw))
		if err != nil {
			return
		}
		for _, entry := range index.Entries {
			if err := EntryName(entry.Name); err != nil {
				t.Fatalf("an accepted index holds a name an open cannot write: %v", err)
			}
		}
		encoded, err := json.Marshal(index, json.Deterministic(true))
		if err != nil {
			t.Fatalf("an accepted index did not re-encode: %v", err)
		}
		if _, err := DecodeIndex(encoded); err != nil {
			t.Fatalf("a re-encoded index did not decode: %v", err)
		}
	})
}

// A declared retention period gates the destructive action rather than being
// reported after it. An undeclared period gates nothing, and is reported as
// never declared rather than as satisfied.
func TestDiscardRefusesAPackageInsideItsDeclaredRetention(t *testing.T) {
	root, sources := packed(t)
	entry := control(t, "lab-evidence", testOnlyKey)
	descriptor := packInto(t, entry, sources, filepath.Join(root, "packet"))
	inside := descriptor.CreatedAt.Add(time.Hour)

	_, removed, err := Discard(filepath.Join(root, "packet"), inside, false)
	if err == nil {
		t.Fatal("a package inside its declared retention was removed")
	}
	if removed != 0 || !strings.Contains(err.Error(), "retained until") {
		t.Fatalf("discard removed %d files and reported %v", removed, err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "packet", DescriptorName)); statErr != nil {
		t.Fatal("a refused discard removed something anyway")
	}
	if _, _, err := Discard(filepath.Join(root, "packet"), inside, true); err != nil {
		t.Fatalf("an overridden retention did not discard: %v", err)
	}

	// A control declaring no retention writes a package that is not gated, and
	// reports not-declared rather than within-retention.
	open := entry
	open.Retain = ""
	ungated := packInto(t, open, sources, filepath.Join(root, "ungated"))
	if got := ungated.Retention(inside); got != RetentionNotDeclared {
		t.Fatalf("an undeclared retention reported %s", got)
	}
	if _, _, err := Discard(filepath.Join(root, "ungated"), inside, false); err != nil {
		t.Fatalf("an undeclared retention gated a discard: %v", err)
	}
}

// A caller that cancels is refused before anything is created. Cancellation is
// noticed while the key is being read, which is before either command reserves
// a destination, so what this asserts is that neither leaves an artifact — not
// that the mid-write cleanup ran, which no reachable input triggers.
func TestPackAndOpenStopWhenTheCallerCancels(t *testing.T) {
	root, sources := packed(t)
	entry := control(t, "lab-evidence", testOnlyKey)
	packInto(t, entry, sources, filepath.Join(root, "packet"))
	t.Setenv(storeSwitch, "emit")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cancelled := filepath.Join(root, "cancelled-pack")
	if _, err := Pack(ctx, entry, sources, 0, cancelled, time.Now()); err == nil {
		t.Fatal("a cancelled pack wrote a package")
	}
	if _, err := os.Stat(cancelled); !errors.Is(err, os.ErrNotExist) {
		t.Error("a cancelled pack left a directory behind")
	}
	opened := filepath.Join(root, "cancelled-open")
	if _, _, err := Open(ctx, entry, filepath.Join(root, "packet"), opened); err == nil {
		t.Fatal("a cancelled open wrote an output directory")
	}
	if _, err := os.Stat(opened); !errors.Is(err, os.ErrNotExist) {
		t.Error("a cancelled open left a directory behind")
	}
}

// A package this release did not write can name one packed file inside another.
// The index reader refuses it, so an open creates nothing rather than failing
// part way through writing a tree it cannot finish. The documents below are
// authored here from the contract, never produced by this package.
func TestAnIndexNamingOnePackedFileInsideAnotherIsRefused(t *testing.T) {
	const identity = `"package":"00000000000000000000000000000000"`
	for name, raw := range map[string]string{
		"a file inside a file": `{"schema":"readmit-transfer-index/v1",` + identity + `,"entries":[{"id":"e0001","name":"run","bytes":2},{"id":"e0002","name":"run/manifest.json","bytes":2}],"entries_not_read":0}`,
		"the same name twice":  `{"schema":"readmit-transfer-index/v1",` + identity + `,"entries":[{"id":"e0001","name":"run","bytes":2},{"id":"e0002","name":"run","bytes":2}],"entries_not_read":0}`,
		"a deeper collision":   `{"schema":"readmit-transfer-index/v1",` + identity + `,"entries":[{"id":"e0001","name":"run/a/b","bytes":2},{"id":"e0002","name":"run/a","bytes":2}],"entries_not_read":0}`,
		"a parent traversal":   `{"schema":"readmit-transfer-index/v1",` + identity + `,"entries":[{"id":"e0001","name":"../escape","bytes":2}],"entries_not_read":0}`,
	} {
		if _, err := DecodeIndex([]byte(raw)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// Sibling paths that merely share a prefix string are not a collision.
	accepted := `{"schema":"readmit-transfer-index/v1",` + identity + `,"entries":[{"id":"e0001","name":"run.json","bytes":2},{"id":"e0002","name":"run/a","bytes":2}],"entries_not_read":0}`
	if _, err := DecodeIndex([]byte(accepted)); err != nil {
		t.Errorf("sibling names were refused: %v", err)
	}
}
