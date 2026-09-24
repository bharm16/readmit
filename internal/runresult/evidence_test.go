package runresult_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testrunner"
)

var native = filepath.Join("..", "..", "testdata", "acceptance", "native-109")

// Every family is named and opened by one call, and states the same members
// whichever family it is: its own identity, the input it names, and the target
// it retained with the identity every reader of that target agrees on.
func TestOpenEvidenceNamesAndOpensEveryFamily(t *testing.T) {
	result, err := runresult.OpenEvidence(filepath.Join(native, "baseline"), "test")
	if err != nil {
		t.Fatal(err)
	}
	a := result.Artifact
	if result.Family != runresult.ResultFamily || a == nil || result.Identity != a.Identity || result.Run != a.Run || result.Pin != nil || result.Lifecycle != nil {
		t.Fatalf("a result is opened as a result: %+v", result)
	}
	if result.Input == nil || result.Input.Identity != a.Result.InputBundleIdentity || result.Input.Changes != len(a.Run.Manifest.Changes) {
		t.Fatalf("a result names the input its run replayed: %+v", result.Input)
	}
	// The one target identity is the one the result recorded when it ran.
	if result.Target == nil || result.Target.Identity == "" || result.Target.Identity != a.Result.TargetIdentity || result.Target.Record != *a.Result.Target {
		t.Fatalf("a result's target identity disagrees with the one it recorded: %+v", result.Target)
	}

	run, err := runresult.OpenEvidence(filepath.Join(native, "baseline", "run"), "test")
	if err != nil {
		t.Fatal(err)
	}
	if run.Family != runresult.RunFamily || run.Run == nil || run.Identity != a.Run.Identity || run.Artifact != nil {
		t.Fatalf("a run is opened as a run: %+v", run)
	}
	if run.Target == nil || run.Target.Identity != result.Target.Identity || run.Input == nil || run.Input.Identity != result.Input.Identity {
		t.Fatalf("a run and the result that retained it state the same input and target: %+v %+v", run.Input, run.Target)
	}

	c, err := runresult.OpenEvidence(filepath.Join(native, "regression"), "test")
	if err != nil {
		t.Fatal(err)
	}
	if c.Family != runresult.CaseFamily || c.Case == nil || c.Identity != c.Case.Identity || c.Input == nil || c.Input.Identity != c.Identity || len(c.Input.Transformations) != 0 || c.Target != nil || c.Pin != nil {
		t.Fatalf("a case declares the input it is and nothing else: %+v", c)
	}
}

// A durable run and a result are named by the record they hold, the pin before
// the result, in a directory on disk and in a tree already read alike. The
// opener names a family by any entry and leaves refusing it to that family's
// reader; a caller that reads nothing names one only by a regular file.
func TestExecutionFamilyNamesADurableRunBeforeAResult(t *testing.T) {
	dir := t.TempDir()
	for _, marker := range []runresult.Marker{runresult.AnyEntry, runresult.RegularFile} {
		if family := runresult.ExecutionFamily(dir, marker); family != "" {
			t.Fatalf("an empty directory is named %q", family)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "engine.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "result.json"), nil)
	if family := runresult.ExecutionFamily(dir, runresult.RegularFile); family != runresult.ResultFamily {
		t.Fatalf("a folder named like a pin names no durable run to a listing: %q", family)
	}
	if family := runresult.ExecutionFamily(dir, runresult.AnyEntry); family != runresult.JobFamily {
		t.Fatalf("the opener hands a folder named like a pin to the durable run reader: %q", family)
	}
	if _, err := runresult.OpenEvidence(dir, "test"); err == nil || err.Error() != "cannot read the engine pin this durable run retained" {
		t.Fatalf("a folder named like a pin was read as one: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "engine.json")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "engine.json"), nil)
	if family := runresult.ExecutionFamily(dir, runresult.RegularFile); family != runresult.JobFamily {
		t.Fatalf("a durable run also holds a result and is named by its pin: %q", family)
	}
	files := map[string][]byte{"current/engine.json": {}, "current/result/result.json": {}, "baseline/result.json": {}, "engine.json": {}}
	for directory, want := range map[string]runresult.Family{"current": runresult.JobFamily, "current/result": runresult.ResultFamily, "baseline": runresult.ResultFamily, "": runresult.JobFamily, "case": ""} {
		if family := runresult.ExecutionFamilyIn(files, directory); family != want {
			t.Errorf("ExecutionFamilyIn(%q) = %q, want %q", directory, family, want)
		}
	}
}

// A directory no family reader would accept is refused before any reader is
// handed it, in the consumer's own words, and no refusal echoes the path.
func TestNameRefusesWhatNamesNoEvidenceFamily(t *testing.T) {
	root := t.TempDir()
	for name, manifest := range map[string]string{
		"no-manifest": "",
		"invalid":     "{not json",
		"unrelated":   `{"schema":"readmit-backup/v1"}`,
		"result":      `{"schema":"readmit-result/v1"}`,
	} {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if manifest != "" {
			write(t, filepath.Join(dir, "manifest.json"), []byte(manifest))
		}
	}
	for name, refusal := range map[string]string{
		"no-manifest": "cannot read consumer artifact manifest",
		"invalid":     "invalid consumer artifact manifest",
		"unrelated":   "unsupported consumer artifact contract",
		// A result is named by its record, never by a manifest claiming it.
		"result": "unsupported consumer artifact contract",
		"absent": "cannot resolve artifact input",
	} {
		dir := filepath.Join(root, name)
		family, err := runresult.Name(dir, "consumer")
		if err == nil || err.Error() != refusal || family != "" {
			t.Errorf("%s: family %q, error %v, want %q", name, family, err, refusal)
		}
		if _, err := runresult.OpenEvidence(dir, "consumer"); err == nil || strings.Contains(err.Error(), root) {
			t.Errorf("%s was opened or its refusal echoed the path: %v", name, err)
		}
	}
}

// A durable run keeps its pin's digest whether or not this build reads the
// pin, and one that stopped before it retained a result states the pin alone:
// no identity, input or target it did not retain.
func TestOpenEvidenceKeepsTheDigestOfAPinItCannotRead(t *testing.T) {
	valid, err := engine.Encode(engine.Pin{Schema: engine.Schema, Engine: engine.Version(), Spec: testrunner.SpecSchema, Profile: observation.Profile})
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		pin      []byte
		readable bool
	}{
		"readable":   {valid, true},
		"unreadable": {[]byte("{not a pin"), false},
		"later":      {[]byte(`{"schema":"readmit-engine/v2","engine":"9.9.9","spec":"readmit-test/v1","profile":"readmit-siu-v1","ledger":true}` + "\n"), false},
	} {
		job := filepath.Join(t.TempDir(), "job")
		if err := os.Mkdir(job, 0o700); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(job, "engine.json"), test.pin)
		opened, err := runresult.OpenEvidence(job, "test")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if opened.Family != runresult.JobFamily || opened.Identity != "" || opened.Input != nil || opened.Target != nil || opened.Artifact != nil || opened.Pin == nil || len(opened.Pin.Digest) != 64 {
			t.Fatalf("%s: a job without a result states its pin alone: %+v", name, opened)
		}
		if (opened.Pin.Document != nil) != test.readable || test.readable && opened.Pin.Document.Profile != observation.Profile {
			t.Fatalf("%s: pin document %+v", name, opened.Pin.Document)
		}
	}
	oversized := filepath.Join(t.TempDir(), "job")
	if err := os.Mkdir(oversized, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(oversized, "engine.json"), make([]byte, engine.MaxPinBytes+1))
	if _, err := runresult.OpenEvidence(oversized, "test"); err == nil || err.Error() != "cannot read the engine pin this durable run retained" {
		t.Fatalf("a pin past its bound was read: %v", err)
	}
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
