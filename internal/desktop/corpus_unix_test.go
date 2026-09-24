//go:build !windows

package desktop_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/importer"
)

// The raw-inspection and performance-corpus screens write into a folder the
// host's dialog chose, and each writes one new entry of it. A chosen folder
// reached through a symbolic link is refused, and so is an entry name that is
// already a link, before anything is written: nothing reaches the folder the
// link leads to.
func TestRawAndCorpusWritesStayInsideTheChosenFolder(t *testing.T) {
	app := workspaceApp(t)
	root, outside := besideWorkspace(t)
	framed := corpusPlan(importer.MLLPFraming, "", importer.USASCII)
	if generated := app.GenerateCorpus(generationRequest(root, "stream.mllp", "7", 3, framed)); generated.State != desktop.Completed {
		t.Fatal(generated)
	}
	stream := filepath.Join(root, "stream.mllp")
	linked := filepath.Join(filepath.Dir(root), "linked")
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"copy.mllp", "corpus", "benchmark.json"} {
		if err := os.Symlink(filepath.Join(outside, name), filepath.Join(root, "planted-"+name)); err != nil {
			t.Fatal(err)
		}
	}
	listed, before := entriesOf(t, root), bytesUnder(t, outside)
	roundTrip := func(folder, name string) refused {
		result := app.WriteRoundTrip(desktop.RoundTripRequest{File: stream, Format: "mllp", Terminator: "cr", Folder: folder, Name: name})
		return refused{result.State, result.Reason}
	}
	generate := func(folder, name string) refused {
		result := app.GenerateCorpus(generationRequest(folder, name, "7", 3, framed))
		return refused{result.State, result.Reason}
	}
	scan := func(folder, name string) refused {
		result := app.ScanCorpus(desktop.CorpusScanRequest{File: stream, Plan: framed, ReportFolder: folder, ReportName: name})
		return refused{result.State, result.Reason}
	}
	for label, got := range map[string]refused{
		"a round trip into a linked folder": roundTrip(linked, "copy.mllp"),
		"a round trip onto a planted link":  roundTrip(root, "planted-copy.mllp"),
		"a corpus into a linked folder":     generate(linked, "corpus"),
		"a corpus onto a planted link":      generate(root, "planted-corpus"),
		"a benchmark into a linked folder":  scan(linked, "benchmark.json"),
		"a benchmark onto a planted link":   scan(root, "planted-benchmark.json"),
	} {
		if got.state != desktop.Failed || got.reason == "" {
			t.Errorf("%s: %+v", label, got)
		}
	}
	if after := entriesOf(t, root); !reflect.DeepEqual(listed, after) {
		t.Fatalf("a refused write changed the chosen folder: %v, was %v", after, listed)
	}
	if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
		t.Fatal("a refused write reached the folder a link leads to")
	}
}

// A file this account cannot read is denied, not failed, and a folder it
// cannot write is denied for each writer; nothing is left behind, and a scan
// whose benchmark could not be written still reports what it counted.
func TestRawAndCorpusSeparatePermissionFromFailure(t *testing.T) {
	app := workspaceApp(t)
	source := t.TempDir()
	framed := corpusPlan(importer.MLLPFraming, "", importer.USASCII)
	if generated := app.GenerateCorpus(generationRequest(source, "stream.mllp", "7", 3, framed)); generated.State != desktop.Completed {
		t.Fatal(generated)
	}
	stream := filepath.Join(source, "stream.mllp")
	locked := filepath.Join(source, "locked.mllp")
	if err := os.WriteFile(locked, mustReadFile(t, stream), 0o600); err != nil {
		t.Fatal(err)
	}
	unreadable(t, locked)
	if got := app.InspectRawFile(desktop.RawInspectionRequest{File: locked, Format: "auto", Terminator: "auto"}); got.State != desktop.PermissionDenied || got.Inspection != nil {
		t.Errorf("inspecting an unreadable file: %+v", got)
	}
	if got := app.ScanCorpus(desktop.CorpusScanRequest{File: locked, Plan: framed}); got.State != desktop.PermissionDenied || got.Scan != nil {
		t.Errorf("scanning an unreadable file: %+v", got)
	}

	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(readOnly, 0o700) })
	if got := app.WriteRoundTrip(desktop.RoundTripRequest{File: stream, Format: "auto", Terminator: "auto", Folder: readOnly, Name: "copy.mllp"}); got.State != desktop.PermissionDenied {
		t.Errorf("a round trip into a read-only folder: %+v", got)
	}
	if got := app.GenerateCorpus(generationRequest(readOnly, "corpus", "7", 3, framed)); got.State != desktop.PermissionDenied || got.Manifest != nil {
		t.Errorf("a corpus into a read-only folder: %+v", got)
	}
	got := app.ScanCorpus(desktop.CorpusScanRequest{File: stream, Plan: framed, ReportFolder: readOnly, ReportName: "benchmark.json"})
	if got.State != desktop.PermissionDenied || got.Scan == nil || got.Scan.Records != 3 || got.Benchmark != "" {
		t.Errorf("a benchmark into a read-only folder: %+v", got)
	}
	if listed := entriesOf(t, readOnly); len(listed) != 0 {
		t.Fatalf("a refused write left %v", listed)
	}

	// A folder this account can read but not search: nothing inside it can be
	// looked at, so a stream there is denied by both screens alike. A
	// destination there is refused by the shared reservation, which cannot
	// show such a folder is not retained evidence and refuses it in the
	// command's words, before anything is written.
	unsearchable := t.TempDir()
	if err := os.WriteFile(filepath.Join(unsearchable, "stream.mllp"), mustReadFile(t, stream), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsearchable, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unsearchable, 0o700) })
	hidden := filepath.Join(unsearchable, "stream.mllp")
	if got := app.InspectRawFile(desktop.RawInspectionRequest{File: hidden, Format: "auto", Terminator: "auto"}); got.State != desktop.PermissionDenied {
		t.Errorf("inspecting a file in an unsearchable folder: %+v", got)
	}
	if got := app.ScanCorpus(desktop.CorpusScanRequest{File: hidden, Plan: framed}); got.State != desktop.PermissionDenied {
		t.Errorf("scanning a file in an unsearchable folder: %+v", got)
	}
	const unprovable = "output must be outside the immutable input case or enclosing evidence"
	if got := app.ScanCorpus(desktop.CorpusScanRequest{File: stream, Plan: framed, ReportFolder: unsearchable, ReportName: "benchmark.json"}); got.State != desktop.Failed || got.Reason != unprovable || got.Scan != nil {
		t.Errorf("a benchmark into an unsearchable folder: %+v", got)
	}
	if got := app.GenerateCorpus(generationRequest(unsearchable, "corpus", "7", 3, framed)); got.State != desktop.Failed || got.Reason != unprovable {
		t.Errorf("a corpus into an unsearchable folder: %+v", got)
	}
	os.Chmod(unsearchable, 0o700)
	if listed := entriesOf(t, unsearchable); !reflect.DeepEqual(listed, []string{"stream.mllp"}) {
		t.Fatalf("a refused write left %v", listed)
	}
}

// A named pipe is refused as not a regular file, promptly, by every screen
// that reads a chosen file: nothing opens it, so nothing waits for a writer
// while it holds the operation slot, and the next operation runs.
func TestRawAndCorpusRefuseAPipeWithoutWaitingOnIt(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()
	pipe := filepath.Join(dir, "pipe.hl7")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Fatal(err)
	}
	framed := corpusPlan(importer.MLLPFraming, "", importer.USASCII)
	for label, read := range map[string]func() refused{
		"InspectRawFile": func() refused {
			r := app.InspectRawFile(desktop.RawInspectionRequest{File: pipe, Format: "auto", Terminator: "auto"})
			return refused{r.State, r.Reason}
		},
		"WriteRoundTrip": func() refused {
			r := app.WriteRoundTrip(desktop.RoundTripRequest{File: pipe, Format: "auto", Terminator: "auto", Folder: dir, Name: "copy.hl7"})
			return refused{r.State, r.Reason}
		},
		"ScanCorpus": func() refused {
			r := app.ScanCorpus(desktop.CorpusScanRequest{File: pipe, Plan: framed})
			return refused{r.State, r.Reason}
		},
	} {
		got := answeredWithin(t, label+" of a pipe", read)
		if got.state != desktop.Failed || !strings.Contains(got.reason, "regular file") {
			t.Errorf("%s of a pipe: %+v", label, got)
		}
	}
	if listed := entriesOf(t, dir); !reflect.DeepEqual(listed, []string{"pipe.hl7"}) {
		t.Fatalf("a refused read wrote %v", listed)
	}
}

// Retained evidence is never a destination: a corpus, its manifest or a
// benchmark aimed into a case bundle is refused by the shared writer's own
// reservation, and nothing — not even a probe of whether the folder is
// writable — is created inside the case, which still verifies.
func TestCorpusDestinationsInsideACaseAreRefusedWithoutTouchingIt(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	writeCase(t, root, "sealed", framed("MSH|^~\\&|SYNTH|LAB|RECV|LAB|20260101120000||SIU^S12|SEALED-1|T|2.5.1\rPID|1||R-1\r"))
	sealed := filepath.Join(root, "sealed")
	before := bytesUnder(t, sealed)
	plan := corpusPlan(importer.MLLPFraming, "", importer.USASCII)
	generated := app.GenerateCorpus(generationRequest(sealed, "corpus", "7", 3, plan))
	if generated.State != desktop.Failed || generated.Manifest != nil {
		t.Fatalf("a corpus into a case: %+v", generated)
	}
	stream := filepath.Join(root, "stream.mllp")
	writeDocument(t, root, "stream.mllp", framed("MSH|^~\\&|SYNTH|LAB|RECV|LAB|20260101120000||SIU^S12|SCAN-1|T|2.5.1\rPID|1||R-1\r"))
	scanned := app.ScanCorpus(desktop.CorpusScanRequest{File: stream, Plan: plan, ReportFolder: sealed, ReportName: "benchmark.json"})
	if scanned.State != desktop.Failed || scanned.Benchmark != "" {
		t.Fatalf("a benchmark into a case: %+v", scanned)
	}
	copied := app.WriteRoundTrip(desktop.RoundTripRequest{File: stream, Format: "auto", Terminator: "auto", Folder: sealed, Name: "copy.mllp"})
	if copied.State != desktop.Failed {
		t.Fatalf("a round trip into a case: %+v", copied)
	}
	if after := bytesUnder(t, sealed); !reflect.DeepEqual(before, after) {
		t.Fatal("a refused write changed the case")
	}
	if opened := app.OpenCase(root, "sealed"); opened.State != desktop.Completed {
		t.Fatalf("the case no longer verifies: %+v", opened)
	}
}
