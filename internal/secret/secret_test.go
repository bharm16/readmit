package secret

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The provider a test points a reference at is this test binary re-executed
// with providerSwitch set. It is a stand-in for an operating system credential
// store, and every value it returns is generated inside the test and marked
// test-only. No credential is committed anywhere in this repository.
const (
	providerSwitch = "READMIT_TEST_SECRET_PROVIDER"
	testOnlyValue  = "test-only-not-a-real-credential-9f2c"
)

func TestMain(m *testing.M) {
	if mode, ok := os.LookupEnv(providerSwitch); ok {
		os.Exit(provider(mode, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// provider emits the bytes of the file its locator argument names. A locator is
// an argument; a value never is, which is the rule this package enforces.
func provider(mode string, args []string) int {
	switch mode {
	case "fail":
		return 3
	case "hang":
		time.Sleep(30 * time.Second)
		return 0
	case "empty":
		return 0
	case "hold":
		// The provider says it has started, then answers only once the test
		// releases it, so the test can look while it runs.
		if len(args) != 1 || os.WriteFile(filepath.Join(args[0], "started"), nil, 0600) != nil {
			return 4
		}
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			if _, err := os.Stat(filepath.Join(args[0], "release")); err == nil {
				os.Stdout.WriteString(testOnlyValue)
				return 0
			}
		}
		return 5
	}
	if len(args) != 1 {
		return 4
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return 4
	}
	os.Stdout.Write(data)
	return 0
}

// providerCommand returns the absolute path of this test binary.
func providerCommand(t *testing.T) string {
	t.Helper()
	// The provider is this race-instrumented binary re-executed. It has no
	// background work to drain; avoid a one-second race-runtime exit sleep
	// on every lookup. The parent race runtime keeps its default settings.
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	path, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// locator writes test-only material to a file and returns its path.
func locator(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test-only-material")
	if err := os.WriteFile(path, []byte(value+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func reference(t *testing.T, name, value string) Reference {
	t.Helper()
	return Reference{
		Name:       name,
		Store:      OSKeychain,
		Purpose:    MLLPEndpoint,
		Address:    "127.0.0.1:2575",
		Command:    providerCommand(t),
		Arguments:  []string{locator(t, value)},
		Generation: 1,
		RotatedAt:  time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC),
		MaxAge:     "720h",
	}
}

func TestStoreRoundTripKeepsReferencesSortedAndDeterministic(t *testing.T) {
	document := Document{Schema: Schema}
	for _, name := range []string{"lab-mllp", "acceptance-mllp"} {
		updated, stored, err := Add(document, reference(t, name, testOnlyValue))
		if err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
		if stored.Name != name || stored.Generation != 1 {
			t.Fatalf("add returned %+v", stored)
		}
		document = updated
	}
	if _, _, err := Add(document, reference(t, "lab-mllp", testOnlyValue)); err == nil {
		t.Fatal("a duplicate reference name was accepted")
	}
	data, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Encode(document)
	if err != nil || string(data) != string(again) {
		t.Fatal("encoding the same document twice produced different bytes")
	}
	decoded, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.References) != 2 || decoded.References[0].Name != "acceptance-mllp" {
		t.Fatalf("references were not stored in one canonical order: %+v", decoded.References)
	}
	if strings.Contains(string(data), testOnlyValue) {
		t.Fatal("the encoded store held a credential value")
	}
}

func TestDecodeRefusesUnknownMembersVersionsAndScopes(t *testing.T) {
	document := Document{Schema: Schema, References: []Reference{reference(t, "lab-mllp", testOnlyValue)}}
	data, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(string) string{
		"unknown member":  func(s string) string { return strings.Replace(s, `"references":`, `"value":"x","references":`, 1) },
		"unknown version": func(s string) string { return strings.Replace(s, Schema, "readmit-secrets/v2", 1) },
		"unknown store":   func(s string) string { return strings.Replace(s, string(OSKeychain), "somewhere", 1) },
		"unknown purpose": func(s string) string { return strings.Replace(s, string(MLLPEndpoint), "anything", 1) },
		"relative command": func(s string) string {
			return strings.Replace(s, `"command":"`+document.References[0].Command+`"`, `"command":"provider"`, 1)
		},
		"zero generation": func(s string) string { return strings.Replace(s, `"generation":1`, `"generation":0`, 1) },
		"negative max age": func(s string) string {
			return strings.Replace(s, `"max_age":"720h"`, `"max_age":"-1h"`, 1)
		},
	} {
		changed := mutate(string(data))
		if changed == string(data) {
			t.Fatalf("%s: the fixture did not change", name)
		}
		if _, err := Decode([]byte(changed)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestValueIsNeverRenderedOrSerialized(t *testing.T) {
	value := Value{raw: []byte(testOnlyValue)}
	for _, rendered := range []string{
		value.String(),
		fmt.Sprintf("%v", value),
		fmt.Sprintf("%s", value),
		fmt.Sprintf("%q", value),
		fmt.Sprintf("%x", value),
		fmt.Sprintf("%#v", value),
		fmt.Sprint(struct{ Credential Value }{value}),
	} {
		if strings.Contains(rendered, testOnlyValue) {
			t.Fatalf("a rendering disclosed the value: %s", rendered)
		}
		if !strings.Contains(rendered, Mask) {
			t.Fatalf("a rendering did not mask the value: %s", rendered)
		}
	}
	if _, err := json.Marshal(struct {
		Credential Value `json:"credential"`
	}{value}); err == nil {
		t.Fatal("a credential value was serialized to JSON")
	}
	if string(value.Expose()) != testOnlyValue {
		t.Fatal("the explicitly exposed value did not match")
	}
}

func TestResolveReadsTheDeclaredStoreThroughItsLocator(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	resolved, err := Resolve(context.Background(), reference(t, "lab-mllp", testOnlyValue))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if string(resolved.Expose()) != testOnlyValue {
		t.Fatalf("resolved %q", resolved.Expose())
	}
}

func TestResolveRefusesCommandsOutsideTheDeclaredContract(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	base := reference(t, "lab-mllp", testOnlyValue)
	relative := base
	relative.Command = "provider"
	missing := base
	missing.Command = filepath.Join(t.TempDir(), "absent")
	if runtime.GOOS == "windows" {
		missing.Command += ".exe"
	}
	directory := base
	directory.Command = t.TempDir()
	for name, candidate := range map[string]Reference{"relative": relative, "missing": missing, "directory": directory} {
		if _, err := Resolve(context.Background(), candidate); err == nil {
			t.Errorf("%s command was accepted", name)
		}
	}
}

func TestResolveBoundsAProviderThatFailsHangsOrReturnsNothing(t *testing.T) {
	for mode, label := range map[string]string{"fail": "a failing provider", "empty": "an empty provider", "hang": "a hanging provider"} {
		t.Setenv(providerSwitch, mode)
		started := time.Now()
		if _, err := Resolve(context.Background(), reference(t, "lab-mllp", testOnlyValue)); err == nil {
			t.Errorf("%s was accepted", label)
		}
		if elapsed := time.Since(started); elapsed > ResolveTimeout+20*time.Second {
			t.Errorf("%s was not bounded: %s", label, elapsed)
		}
	}
}

func TestResolveStopsWhenTheCallerCancels(t *testing.T) {
	t.Setenv(providerSwitch, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Resolve(ctx, reference(t, "lab-mllp", testOnlyValue)); err == nil {
		t.Fatal("a cancelled resolution returned a value")
	}
}

// programCounter counts the declared programs a context's observer is
// told about: how many are running now, and how many started and ended.
type programCounter struct {
	mu                      sync.Mutex
	running, started, ended int
}

func (o *programCounter) observe() func() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.running++
	o.started++
	return func() {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.running--
		o.ended++
	}
}

func (o *programCounter) counts() (running, started, ended int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.running, o.started, o.ended
}

// A caller that has to say while a declared program runs is told by the run
// itself: the program is reported running from just before it starts until it
// has ended, whether it answered or failed, and a locator refused before any
// program starts reports nothing. Without an observer a read is unchanged.
func TestLocatorReportsItsDeclaredProgramWhileItRuns(t *testing.T) {
	t.Setenv(providerSwitch, "hold")
	observer := &programCounter{}
	ctx := ObserveDeclaredPrograms(context.Background(), observer.observe)
	held := t.TempDir()
	answered := make(chan error, 1)
	go func() {
		value, err := Locator{Command: providerCommand(t), Arguments: []string{held}}.Read(ctx)
		if err == nil && string(value.Expose()) != testOnlyValue {
			err = fmt.Errorf("read %q", value.Expose())
		}
		answered <- err
	}()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(held, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the declared program never started")
		}
	}
	if running, started, ended := observer.counts(); running != 1 || started != 1 || ended != 0 {
		t.Fatalf("while the program ran the observer saw %d running, %d started, %d ended", running, started, ended)
	}
	if err := os.WriteFile(filepath.Join(held, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-answered; err != nil {
		t.Fatalf("read: %v", err)
	}
	if running, started, ended := observer.counts(); running != 0 || started != 1 || ended != 1 {
		t.Fatalf("after the program ended the observer saw %d running, %d started, %d ended", running, started, ended)
	}

	t.Setenv(providerSwitch, "fail")
	if _, err := Resolve(ctx, reference(t, "lab-mllp", testOnlyValue)); err == nil {
		t.Fatal("a failing provider was accepted")
	}
	if running, started, ended := observer.counts(); running != 0 || started != 2 || ended != 2 {
		t.Fatalf("a failed program was reported as %d running, %d started, %d ended", running, started, ended)
	}
	refused := reference(t, "lab-mllp", testOnlyValue)
	refused.Command = "provider"
	if _, err := Resolve(ctx, refused); err == nil {
		t.Fatal("a relative command was accepted")
	}
	if _, started, _ := observer.counts(); started != 2 {
		t.Fatal("a locator refused before its program started was reported as a running program")
	}

	t.Setenv(providerSwitch, "emit")
	if _, err := Resolve(context.Background(), reference(t, "lab-mllp", testOnlyValue)); err != nil {
		t.Fatalf("a read without an observer: %v", err)
	}
	DeclaredProgramStarting(context.Background())()
	if _, started, _ := observer.counts(); started != 2 {
		t.Fatal("a read under another context reached this observer")
	}
}

func TestBindRefusesAPurposeOrAddressOutsideTheReferenceScope(t *testing.T) {
	document := Document{Schema: Schema, References: []Reference{reference(t, "lab-mllp", testOnlyValue)}}
	if _, err := Bind(document, "lab-mllp", MLLPEndpoint, "127.0.0.1:2575"); err != nil {
		t.Fatalf("the declared scope did not bind: %v", err)
	}
	if _, err := Bind(document, "lab-mllp", MLLPEndpoint, "127.0.0.1:9999"); err == nil {
		t.Error("a reference bound to an address outside its scope")
	}
	if _, err := Bind(document, "lab-mllp", Purpose("something-else"), "127.0.0.1:2575"); err == nil {
		t.Error("a reference bound to a purpose outside its scope")
	}
	if _, err := Bind(document, "absent", MLLPEndpoint, "127.0.0.1:2575"); err == nil {
		t.Error("an unregistered reference bound")
	}
}

func TestRotateRecordsAGenerationForAKnownReferenceOnly(t *testing.T) {
	document := Document{Schema: Schema, References: []Reference{reference(t, "lab-mllp", testOnlyValue)}}
	at := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	updated, stored, err := Rotate(document, "lab-mllp", at)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if stored.Generation != 2 || !stored.RotatedAt.Equal(at) {
		t.Fatalf("rotation recorded %+v", stored)
	}
	if document.References[0].Generation != 1 {
		t.Fatal("rotate modified the document it was given")
	}
	if _, _, err := Rotate(updated, "absent", at); err == nil {
		t.Fatal("an unregistered reference was rotated")
	}
}

func TestRemoveDeletesKnownReferenceAndLeavesDocumentUnmodified(t *testing.T) {
	ref1 := reference(t, "lab-mllp", testOnlyValue)
	ref2 := reference(t, "source-ehr", testOnlyValue)
	ref2.Purpose = SourceEndpoint
	document := Document{Schema: Schema, References: []Reference{ref1, ref2}}

	updated, err := Remove(document, "lab-mllp")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(updated.References) != 1 || updated.References[0].Name != "source-ehr" {
		t.Fatalf("expected 1 reference named source-ehr, got: %+v", updated.References)
	}
	if len(document.References) != 2 {
		t.Fatal("remove modified the document it was given")
	}
	if _, err := Remove(updated, "absent"); err == nil {
		t.Fatal("an unregistered reference was removed without error")
	}
	// Removing the last reference yields a valid empty document
	empty, err := Remove(updated, "source-ehr")
	if err != nil {
		t.Fatalf("remove last reference: %v", err)
	}
	if len(empty.References) != 0 {
		t.Fatalf("expected 0 references, got %d", len(empty.References))
	}
}

func TestRotationStateReportsAnOverdueCredentialExplicitly(t *testing.T) {
	entry := reference(t, "lab-mllp", testOnlyValue)
	if state := entry.Rotation(entry.RotatedAt.Add(time.Hour)); state != RotationCurrent {
		t.Errorf("recent rotation reported %s", state)
	}
	if state := entry.Rotation(entry.RotatedAt.Add(800 * time.Hour)); state != RotationOverdue {
		t.Errorf("stale rotation reported %s", state)
	}
	entry.MaxAge = ""
	if state := entry.Rotation(entry.RotatedAt.Add(800 * time.Hour)); state != RotationNotDeclared {
		t.Errorf("undeclared rotation interval reported %s", state)
	}
}

// A declared interval is read as a Go duration, compound ones included, and a
// zero interval declares no rotation at all rather than one that is always
// overdue. The window shows this state; it never reads the interval itself.
func TestRotationReadsACompoundIntervalAndAZeroIntervalDeclaresNone(t *testing.T) {
	entry := reference(t, "lab-mllp", testOnlyValue)
	entry.MaxAge = "1h30m"
	if state := entry.Rotation(entry.RotatedAt.Add(80 * time.Minute)); state != RotationCurrent {
		t.Errorf("a rotation within 1h30m reported %s", state)
	}
	if state := entry.Rotation(entry.RotatedAt.Add(100 * time.Minute)); state != RotationOverdue {
		t.Errorf("a rotation older than 1h30m reported %s", state)
	}
	entry.MaxAge = "0s"
	if state := entry.Rotation(entry.RotatedAt.Add(time.Hour)); state != RotationNotDeclared {
		t.Errorf("a zero interval reported %s", state)
	}
}

func TestWriteStoreRetainsAnInterruptedWriteAndKeepsItOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	document := Document{Schema: Schema, References: []Reference{reference(t, "lab-mllp", testOnlyValue)}}
	if err := WriteStore(path, document); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("store mode %v", info.Mode().Perm())
	}
	reopened, err := ReadStore(path)
	if err != nil || len(reopened.References) != 1 {
		t.Fatalf("read: %v", err)
	}
	if err := os.WriteFile(path+".incomplete", []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteStore(path, document); err == nil {
		t.Fatal("an interrupted write was overwritten")
	}
	if reopened, err = ReadStore(path); err != nil || len(reopened.References) != 1 {
		t.Fatalf("the previous store did not survive the refused write: %v", err)
	}
}

// Collect reads the regular files of the trees it is pointed at, names each one
// by the path the caller used, and reports what it did not read.
func TestCollectReadsRegularFilesAndNamesWhatItDidNotRead(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		filepath.Join("run", "manifest.json"): "{}",
		filepath.Join("logs", "readmit.log"):  "operation=replay",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	single := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(single, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	files, skipped, err := Collect([]string{root, single})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("collect skipped %d entries of regular files", skipped)
	}
	for _, want := range []string{root + "/run/manifest.json", root + "/logs/readmit.log", single} {
		if _, ok := files[want]; !ok {
			t.Fatalf("collect did not read %q: %v", want, files)
		}
	}
	if len(files) != 3 {
		t.Fatalf("collect read %d files", len(files))
	}
	if _, _, err := Collect([]string{filepath.Join(root, "absent")}); err == nil {
		t.Fatal("a path that is not there was collected")
	}
}

// Every bound is applied to what the tree declares, before anything is read, so
// a tree past a limit is refused rather than reported as fully checked.
func TestCollectRefusesTreesPastItsBounds(t *testing.T) {
	sparse := func(t *testing.T, directory string, count int, size int64) string {
		t.Helper()
		for i := range count {
			file, err := os.Create(filepath.Join(directory, "large-"+strconv.Itoa(i)))
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Truncate(size); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}
		return directory
	}
	t.Run("more files than the scan reads", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "manifest.json")
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		roots := slices.Repeat([]string{path}, MaxScanFiles)
		if _, _, err := Collect(roots); err != nil {
			t.Fatalf("a scan at the file count limit was refused: %v", err)
		}
		if _, _, err := Collect(append(roots, path)); err == nil {
			t.Fatal("a scan past the file count limit was collected")
		}
	})
	t.Run("one file larger than the scan reads", func(t *testing.T) {
		directory := sparse(t, t.TempDir(), 1, MaxScanFileBytes+1)
		if _, _, err := Collect([]string{directory}); err == nil {
			t.Fatal("a file past the per-file limit was collected")
		}
	})
	t.Run("more bytes than the scan holds", func(t *testing.T) {
		directory := sparse(t, t.TempDir(), MaxScanBytes/MaxScanFileBytes+1, MaxScanFileBytes)
		if _, _, err := Collect([]string{directory}); err == nil {
			t.Fatal("a tree past the total byte limit was collected")
		}
	})
}

// FuzzSecretStore exercises the reader of the secret reference document. An
// accepted document must encode, decode again and encode to the same bytes, so
// no input can reach a state the store cannot faithfully rewrite.
func FuzzSecretStore(f *testing.F) {
	f.Add([]byte(`{"schema":"readmit-secrets/v1","references":[]}`))
	f.Add([]byte(`{"schema":"readmit-secrets/v1","references":[{"name":"lab","store":"os-keychain",` +
		`"purpose":"mllp-endpoint","address":"127.0.0.1:2575","command":"/usr/bin/true","arguments":["-s","lab"],` +
		`"generation":1,"rotated_at":"2026-09-18T09:00:00Z","max_age":"720h"}]}`))
	f.Add([]byte(`{"schema":"readmit-secrets/v1"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := Decode(data)
		if err != nil {
			return
		}
		encoded, err := Encode(decoded)
		if err != nil {
			t.Fatalf("an accepted document could not be encoded: %v", err)
		}
		again, err := Decode(encoded)
		if err != nil {
			t.Fatalf("an encoded document was not accepted: %v", err)
		}
		second, err := Encode(again)
		if err != nil || string(second) != string(encoded) {
			t.Fatalf("encoding is not stable: %v", err)
		}
	})
}
