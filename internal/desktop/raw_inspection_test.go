package desktop_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
)

// rawInspectionFiles are the inputs the window and the command are compared
// over: labelled and positional messages, MLLP framing, CR, LF and CRLF
// terminators, repeated fields, bytes that are not UTF-8 beside bytes that
// are, and a message whose declared version the dictionary does not label.
func rawInspectionFiles(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{}
	for _, fixture := range []string{"adt-cr.hl7", "two-messages.mllp", "siu-lf.hl7", "ack-crlf.hl7", "non-utf8.hl7", "custom-delimiters.hl7"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", fixture))
		if err != nil {
			t.Fatal(err)
		}
		files[fixture] = writeRaw(t, dir, fixture, data)
	}
	// Latin-1 and UTF-8 in one message, a repeated field, an empty and a null
	// field, and a version the bundled labels do not describe.
	files["mixed-encoding.hl7"] = writeRaw(t, dir, "mixed-encoding.hl7",
		[]byte("MSH|^~\\&|SYNTH|LAB|RECV|LAB|20260101120000||ADT^A08|MIX-1|P|2.5.1\rPID|1||R-1~R-2~R-3^^^SYNTH||N\xe9\xff^Z\xc3\xa9||\"\"\rZPD|\xe2\x82\xac|\r"))
	files["positional.hl7"] = writeRaw(t, dir, "positional.hl7",
		[]byte("MSH|^~\\&|SYNTH|LAB|RECV|LAB|20260101120000||ADT^A08|POS-1|P|2.3\rPID|1||R-1\r"))
	return files
}

func writeRaw(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// inspectCommand runs `readmit inspect` in process and returns what it printed.
func inspectCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := cli.Execute("dev", append([]string{"inspect"}, args...), &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

// sourceState is what an inspection must leave exactly as it found it.
type sourceState struct {
	digest string
	info   os.FileInfo
}

func sourceStateOf(t *testing.T, path string) sourceState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return sourceState{hex.EncodeToString(sum[:]), info}
}

func (s sourceState) unchanged(t *testing.T, path string) {
	t.Helper()
	now := sourceStateOf(t, path)
	if now.digest != s.digest || now.info.Size() != s.info.Size() || now.info.Mode() != s.info.Mode() || !now.info.ModTime().Equal(s.info.ModTime()) {
		t.Fatalf("inspecting %s changed it", filepath.Base(path))
	}
}

// listFile lists every message of one file and fails the test unless it read.
func listFile(t *testing.T, app *desktop.App, path, format, terminator string) desktop.FileMessagesResult {
	t.Helper()
	listed := app.ListFileMessages(desktop.FileMessagesRequest{File: path, Format: format, Terminator: terminator})
	if listed.State != desktop.Completed {
		t.Fatalf("listing %s: %+v", filepath.Base(path), listed)
	}
	return listed
}

// The standalone reader parses by the same parser and declarations `readmit
// inspect` uses, so every message the command reports is one the list shows,
// at the same bytes, under the same framing, and the file is left exactly as
// it was with nothing written beside it.
func TestFileMessagesMatchTheInspectCommand(t *testing.T) {
	app := workspaceApp(t)
	files := rawInspectionFiles(t)
	messageLine := regexp.MustCompile(`(?m)^Message (\d+): terminator=(\w+) \((\w+)\), bytes \[(\d+),(\d+)\)`)
	for name, path := range files {
		before := sourceStateOf(t, path)
		for _, options := range [][2]string{{"auto", "auto"}, {"raw", "auto"}, {"mllp", "auto"}} {
			stdout, _, err := inspectCommand(t, path, "--format", options[0], "--terminator", options[1])
			listed := app.ListFileMessages(desktop.FileMessagesRequest{File: path, Format: options[0], Terminator: options[1]})
			if err != nil {
				if listed.State != desktop.Failed || len(listed.Rows) != 0 {
					t.Fatalf("%s %v: the command refused and the window answered %+v", name, options, listed)
				}
				continue
			}
			var lines []string
			for _, match := range messageLine.FindAllStringSubmatch(stdout, -1) {
				lines = append(lines, fmt.Sprintf("%s %s %s %s %s", match[1], match[2], match[3], match[4], match[5]))
			}
			var window []string
			for _, row := range listed.Rows {
				window = append(window, fmt.Sprintf("%d %s %s %d %d", row.Index+1, listed.Terminator, listed.TerminatorSelection, row.Start, row.End))
			}
			header := fmt.Sprintf("Format: %s (%s)\nMessages: %d\n", listed.Format, listed.FormatSelection, listed.Total)
			if !strings.HasPrefix(stdout, header) || strings.Join(lines, "|") != strings.Join(window, "|") ||
				listed.Name != name || listed.SHA256 != before.digest || int64(listed.Bytes) != before.info.Size() {
				t.Errorf("%s %v: the window listed %v under %q, the command printed %v under\n%s", name, options, window, header, lines, stdout)
			}
		}
		before.unchanged(t, path)
	}
	adt := listFile(t, app, files["adt-cr.hl7"], "auto", "auto")
	if adt.Rows[0].MessageCode != "ADT" || adt.Rows[0].TriggerEvent == "" {
		t.Fatalf("the message type was not listed: %+v", adt.Rows[0])
	}
	if listed, err := os.ReadDir(filepath.Dir(files["adt-cr.hl7"])); err != nil || len(listed) != len(files) {
		t.Fatalf("listing wrote beside the files it read: %v %v", listed, err)
	}
}

// One message of a multi-message file opens in the same inspector a case
// occurrence opens in, at the file's own offsets, with values withheld until
// revealed.
func TestInspectFileMessageIsTheCaseInspectorOverOneMessageOfAFile(t *testing.T) {
	app, root, _, opened := messagesWorkspace(t)
	path := writeRaw(t, t.TempDir(), "two.hl7", []byte(framed(gridAccepted)+framed(gridRebooked)))
	listed := listFile(t, app, path, "mllp", "cr")
	if listed.Total != 2 || listed.Rows[1].MessageCode != "SIU" || listed.Rows[1].TriggerEvent != "S13" || listed.Rows[0].MessageCode != "ACK" {
		t.Fatalf("listing: %+v", listed)
	}
	request := desktop.FileInspectRequest{File: path, Format: "mllp", Terminator: "cr", Expect: listed.SHA256, Message: 1, Path: "PID[1]-5[1].1", ByteOffset: -1}
	hidden := app.InspectFileMessage(request)
	if hidden.State != desktop.Completed || hidden.Inspection.Raw != "" || hidden.Inspection.Bytes[0].Text != "" || hidden.Inspection.Message != 1 {
		t.Fatalf("an unrevealed file inspection: %+v", hidden)
	}
	request.Reveal = true
	shown := app.InspectFileMessage(request).Inspection
	caseView := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "incident", Identity: opened.Identity,
		Occurrence: "s0001-e000003", Path: "PID[1]-5[1].1", ByteOffset: -1, Reveal: true}).Inspection
	if shown == nil || caseView == nil || shown.Raw != "ROE" || shown.Raw != caseView.Raw || shown.Selector != caseView.Selector ||
		shown.Metadata != caseView.Metadata || shown.MessageCode != "SIU" || shown.TriggerEvent != "S13" ||
		shown.Selected.Start != len(framed(gridAccepted))+caseView.Selected.Start || shown.Identity != listed.SHA256 {
		t.Fatalf("the file and the case inspector disagree:\nfile %+v\ncase %+v", shown, caseView)
	}
	// Raw is that one message's text at the file's offsets, the selection
	// marked as it is in the case.
	raw := shown.RawWindow
	if raw == nil || caseView.RawWindow == nil || raw.MessageStart != len(framed(gridAccepted))+1 || raw.MessageEnd != raw.MessageStart+len(gridRebooked) ||
		raw.Before+raw.Selected+raw.After != escapedText(gridRebooked) || raw.Selected != "ROE" || *raw != shiftedWindow(*caseView.RawWindow, len(framed(gridAccepted))) {
		t.Fatalf("the file's Raw window %+v, the case's %+v", raw, caseView.RawWindow)
	}
	request.Message = 2
	if outside := app.InspectFileMessage(request); outside.State != desktop.Failed {
		t.Fatalf("a message past the file: %+v", outside)
	}
	bytesView := app.ReadFileBytes(desktop.FileBytesRequest{File: path, Expect: listed.SHA256, Offset: 17})
	if bytesView.State != desktop.Completed || bytesView.Offset != 16 || bytesView.Bytes != len(framed(gridAccepted)+framed(gridRebooked)) ||
		len(bytesView.Rows) != (bytesView.Bytes-16+desktop.HexRowBytes-1)/desktop.HexRowBytes || bytesView.Rows[0].Text != "" {
		t.Fatalf("a byte window: %+v", bytesView)
	}
}

// shiftedWindow is a Raw window as it reads further into a file.
func shiftedWindow(window desktop.RawWindow, by int) desktop.RawWindow {
	window.Offset, window.End, window.MessageStart, window.MessageEnd = window.Offset+by, window.End+by, window.MessageStart+by, window.MessageEnd+by
	return window
}

// A file that changed since it was listed is refused by every later read and
// by Save copy, rather than joined to a list of a different file.
func TestTheFileReaderRefusesAFileThatChangedSinceItWasListed(t *testing.T) {
	app := workspaceApp(t)
	path := rawInspectionFiles(t)["two-messages.mllp"]
	listed := listFile(t, app, path, "mllp", "auto")
	changed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Replace(changed, []byte("FRAME-001"), []byte("FRAME-009"), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	const reason = "the file changed since it was opened; open it again"
	if got := app.InspectFileMessage(desktop.FileInspectRequest{File: path, Format: "mllp", Terminator: "auto", Expect: listed.SHA256, ByteOffset: -1}); got.State != desktop.Failed || got.Reason != reason {
		t.Fatalf("inspecting a changed file: %+v", got)
	}
	if got := app.ReadFileBytes(desktop.FileBytesRequest{File: path, Expect: listed.SHA256}); got.State != desktop.Failed || got.Reason != reason {
		t.Fatalf("reading a changed file: %+v", got)
	}
	destination := filepath.Join(t.TempDir(), "copy.mllp")
	if got := app.SaveFileCopy(desktop.SaveCopyRequest{File: path, Format: "mllp", Terminator: "auto", Expect: listed.SHA256, Destination: destination}); got.State != desktop.Failed || got.Reason != reason {
		t.Fatalf("copying a changed file: %+v", got)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("a refused copy left a file")
	}
	if again := listFile(t, app, path, "mllp", "auto"); again.SHA256 == listed.SHA256 {
		t.Fatal("listing the changed file again read the old bytes")
	}
}

// Save copy is the command's --roundtrip: the copy is the source's bytes
// exactly, written once the file parsed, as one new file. The source by any
// name — itself, a link to it, a hard link — and an existing file are
// refused and left unchanged, a malformed file writes nothing, and the source
// is never changed.
func TestSaveFileCopyWritesTheCommandsByteIdenticalCopy(t *testing.T) {
	app := workspaceApp(t)
	files := rawInspectionFiles(t)
	source := files["two-messages.mllp"]
	before := sourceStateOf(t, source)
	folder := t.TempDir()
	listed := listFile(t, app, source, "mllp", "auto")
	save := func(destination string) desktop.RoundTripResult {
		return app.SaveFileCopy(desktop.SaveCopyRequest{File: source, Format: "mllp", Terminator: "auto", Expect: listed.SHA256, Destination: destination})
	}

	commandCopy := filepath.Join(folder, "command.mllp")
	if _, stderr, err := inspectCommand(t, source, "--format", "mllp", "--roundtrip", commandCopy); err != nil || stderr != "" {
		t.Fatalf("inspect --roundtrip: %v %s", err, stderr)
	}
	result := save(filepath.Join(folder, "window.mllp"))
	if result.State != desktop.Completed || result.Path != filepath.Join(resolved(t, folder), "window.mllp") || result.SHA256 != before.digest || int64(result.Bytes) != before.info.Size() {
		t.Fatalf("save copy: %+v", result)
	}
	original, _ := os.ReadFile(source)
	for _, path := range []string{commandCopy, result.Path} {
		if copied, err := os.ReadFile(path); err != nil || !bytes.Equal(copied, original) {
			t.Fatalf("%s is not the source byte for byte", filepath.Base(path))
		}
	}
	before.unchanged(t, source)

	linkedFolder := t.TempDir()
	if err := os.Symlink(source, filepath.Join(linkedFolder, "link.mllp")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, filepath.Join(linkedFolder, "hard.mllp")); err != nil {
		t.Fatal(err)
	}
	for name, destination := range map[string]string{
		"the command's copy":      commandCopy,
		"the source itself":       source,
		"the source, uncleaned":   filepath.Join(filepath.Dir(source), ".", filepath.Base(source)),
		"a link to the source":    filepath.Join(linkedFolder, "link.mllp"),
		"a hard link to it":       filepath.Join(linkedFolder, "hard.mllp"),
		"a nested name":           filepath.Join(folder, "nested", "copy.mllp"),
		"an escape":               filepath.Join(folder, "..", "..", "copy.mllp") + string(filepath.Separator) + "..",
		"a relative destination":  "copy.mllp",
		"a folder that is absent": filepath.Join(folder, "absent", "copy.mllp"),
	} {
		if got := save(destination); got.State != desktop.Failed || got.Reason == "" {
			t.Errorf("a copy over %s answered %+v", name, got)
		}
	}
	before.unchanged(t, source)
	if copied, _ := os.ReadFile(commandCopy); !bytes.Equal(copied, original) {
		t.Fatal("a refused copy changed an existing file")
	}

	malformed := writeRaw(t, t.TempDir(), "malformed.mllp", []byte("\x0bMSH|^~\\&|A|B\r"))
	wrong := app.ListFileMessages(desktop.FileMessagesRequest{File: malformed, Format: "auto", Terminator: "auto"})
	got := app.SaveFileCopy(desktop.SaveCopyRequest{File: malformed, Format: "auto", Terminator: "auto", Expect: wrong.SHA256, Destination: filepath.Join(folder, "malformed.mllp")})
	if got.State != desktop.Failed {
		t.Fatalf("a malformed file was copied: %+v", got)
	}
	if listed := entriesOf(t, folder); len(listed) != 2 {
		t.Fatalf("refused copies wrote into the folder: %v", listed)
	}
}

// The reader's dialogs choose exactly one file to open, or one new file to
// save a copy as, offering the source's own name; a person who dismisses one
// is answered cancelled with nothing chosen.
func TestChooseInspectionPathKinds(t *testing.T) {
	c := &chooser{files: []string{"/chosen/file.hl7"}, destination: "/chosen/copy.hl7"}
	app := newApp(t, c)
	if got := app.ChooseInspectionPath("file", ""); got.State != desktop.Completed || got.Path != "/chosen/file.hl7" || got.Kind != "file" {
		t.Fatalf("file: %+v", got)
	}
	if got := app.ChooseInspectionPath("copy-destination", "/chosen/file.hl7"); got.State != desktop.Completed || got.Path != "/chosen/copy.hl7" {
		t.Fatalf("copy-destination: %+v", got)
	}
	if got := strings.Join(c.titles, "|"); got != "Open HL7 file|Save copy" {
		t.Fatalf("dialog titles: %s", got)
	}
	if got := strings.Join(c.named, "|"); got != "file.hl7" {
		t.Fatalf("the save dialog offered %q", got)
	}
	c.files = []string{"/chosen/a.hl7", "/chosen/b.hl7"}
	if got := app.ChooseInspectionPath("file", ""); got.State != desktop.Failed || got.Path != "" || got.Reason != "choose exactly one file" {
		t.Fatalf("two files: %+v", got)
	}
	c.files, c.destination = nil, ""
	if got := app.ChooseInspectionPath("file", ""); got.State != desktop.Cancelled || got.Path != "" {
		t.Fatalf("dismissed: %+v", got)
	}
	if got := app.ChooseInspectionPath("copy-destination", "/chosen/file.hl7"); got.State != desktop.Cancelled || got.Path != "" {
		t.Fatalf("dismissed save: %+v", got)
	}
	if got := app.ChooseInspectionPath("round-trip-folder", ""); got.State != desktop.Failed {
		t.Fatalf("a retired kind: %+v", got)
	}
}

// Malformed input, a declaration the bytes contradict, a file past the
// parser's bound, a folder and a missing file are refused by the window with
// the command's own sentence, and the refused file is never changed.
func TestFileMessagesRefuseWhatTheCommandRefuses(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()
	adt, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "adt-cr.hl7"))
	if err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]struct {
		path       string
		format     string
		terminator string
	}{
		"a control byte in a field":  {writeRaw(t, dir, "control.hl7", []byte("MSH|^~\\&|A|B\rPID|1\x00\r")), "auto", "auto"},
		"a truncated MLLP frame":     {writeRaw(t, dir, "truncated.mllp", []byte("\x0bMSH|^~\\&|A|B\r")), "auto", "auto"},
		"mllp declared over raw":     {writeRaw(t, dir, "raw.hl7", adt), "mllp", "auto"},
		"lf declared over cr":        {filepath.Join(dir, "raw.hl7"), "raw", "lf"},
		"no bytes":                   {writeRaw(t, dir, "empty.hl7", nil), "auto", "auto"},
		"past the parser's bound":    {writeRaw(t, dir, "large.hl7", bytes.Repeat([]byte("A"), hl7.MaxInputBytes+1)), "auto", "auto"},
		"a folder":                   {dir, "auto", "auto"},
		"a file that does not exist": {filepath.Join(dir, "absent.hl7"), "auto", "auto"},
	} {
		var before sourceState
		info, statErr := os.Stat(input.path)
		if statErr == nil && info.Mode().IsRegular() {
			before = sourceStateOf(t, input.path)
		}
		_, stderr, cliErr := inspectCommand(t, input.path, "--format", input.format, "--terminator", input.terminator)
		result := app.ListFileMessages(desktop.FileMessagesRequest{File: input.path, Format: input.format, Terminator: input.terminator})
		if cliErr == nil || result.State != desktop.Failed || len(result.Rows) != 0 {
			t.Fatalf("%s: the command answered %v and the window %+v", name, cliErr, result)
		}
		if want := "readmit: " + result.Reason + "\n"; stderr != want {
			t.Errorf("%s: the window refused with %q where the command printed %q", name, result.Reason, stderr)
		}
		if strings.Contains(result.Reason, dir) {
			t.Errorf("%s: the refusal named the path: %q", name, result.Reason)
		}
		if statErr == nil && info.Mode().IsRegular() {
			before.unchanged(t, input.path)
			// A file that was read but did not parse keeps its original bytes
			// readable, named by the digest it was read with.
			if input.path != filepath.Join(dir, "large.hl7") {
				if result.SHA256 != before.digest || result.Name != filepath.Base(input.path) {
					t.Errorf("%s: a refused parse lost what was read: %+v", name, result)
				}
				view := app.ReadFileBytes(desktop.FileBytesRequest{File: input.path, Expect: result.SHA256})
				if view.State != desktop.Completed || view.Bytes != int(info.Size()) {
					t.Errorf("%s: the original bytes of a refused file: %+v", name, view)
				}
			}
		}
	}
	// The declarations and the window are checked before anything is read.
	path := filepath.Join(dir, "raw.hl7")
	for request, want := range map[desktop.FileMessagesRequest]string{
		{File: path, Format: "hl7", Terminator: "auto"}:                  "format must be auto, raw, or mllp",
		{File: path, Format: "auto", Terminator: "nul"}:                  "terminator must be auto, cr, lf, or crlf",
		{File: "raw.hl7", Format: "auto", Terminator: "auto"}:            "choose the file with the file dialog; an inspection reads one file named by its full path",
		{File: path, Format: "auto", Terminator: "auto", Limit: 201}:     "a message window begins at or after the first message and lists at most 200 of them",
		{File: path, Format: "auto", Terminator: "auto", Offset: -1}:     "a message window begins at or after the first message and lists at most 200 of them",
		{File: path, Format: "auto", Terminator: "auto", Offset: 100000}: "the message window begins after the last message",
	} {
		if got := app.ListFileMessages(request); got.State != desktop.Failed || got.Reason != want {
			t.Errorf("%+v answered %+v, want %q", request, got, want)
		}
	}
}
