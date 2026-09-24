package desktop_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/operation"
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

// windowLines pages through every window of one inspection and writes each
// row the way a terminal reads it. It is the test's own reading of what the
// window shows, independent of the command's renderer, so equal text means
// the window was given every line the command prints, in the same order.
func windowLines(t *testing.T, app *desktop.App, request desktop.RawInspectionRequest) string {
	t.Helper()
	var out strings.Builder
	for offset := 0; ; {
		request.Offset, request.Limit = offset, desktop.MaxInspectionRows
		result := app.InspectRawFile(request)
		if result.State != desktop.Completed || result.Inspection == nil {
			t.Fatalf("inspecting %s: %+v", filepath.Base(request.File), result)
		}
		view := result.Inspection
		if offset == 0 {
			fmt.Fprintf(&out, "Format: %s (%s)\nMessages: %d\n", view.Format, view.FormatSelection, view.Messages)
		}
		if len(view.Rows) > desktop.MaxInspectionRows || view.Offset != offset {
			t.Fatalf("a window exceeded its bound or began elsewhere: %d rows from %d", len(view.Rows), view.Offset)
		}
		for _, row := range view.Rows {
			switch row.Kind {
			case operation.InspectMessageRow:
				fmt.Fprintf(&out, "Message %d: terminator=%s (%s), bytes [%d,%d), %s\n", row.Message, row.Terminator, view.TerminatorSelection, row.Start, row.End, row.Profile)
			case operation.InspectSegmentRow:
				fmt.Fprintf(&out, "  %s bytes [%d,%d)\n", row.Segment, row.Start, row.End)
			case operation.InspectFieldRow:
				name := fmt.Sprintf("%s-%d", row.Segment, row.Field)
				if row.Label != "" {
					name += " " + row.Label
				}
				fmt.Fprintf(&out, "    %s: %s", name, row.State)
				if row.State != hl7.Omitted {
					fmt.Fprintf(&out, " (%d bytes)", row.End-row.Start)
					if request.ShowValues {
						fmt.Fprintf(&out, " %s", row.Value)
					}
				} else if row.Value != "" || row.End != 0 {
					t.Fatalf("an omitted field carried bytes: %+v", row)
				}
				out.WriteString("\n")
			case operation.InspectRepetitionRow:
				fmt.Fprintf(&out, "      repetition %d: %s (%d bytes)\n", row.Repetition, row.State, row.End-row.Start)
			default:
				t.Fatalf("unknown row kind %q", row.Kind)
			}
			if !request.ShowValues && row.Value != "" {
				t.Fatalf("a row carried a value nobody asked for: %+v", row)
			}
		}
		offset += len(view.Rows)
		if offset >= view.Total {
			return out.String()
		}
	}
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

// The window's raw inspection reaches the same shared operation `readmit
// inspect` renders, so every line the command prints for a file — under
// detected and declared framing and terminators, with and without values —
// is a row the window shows, in the same order, and the file is left exactly
// as it was. The command and the window refuse a file for the same reason in
// the same words, and neither imports or writes anything.
func TestRawInspectionMatchesTheInspectCommand(t *testing.T) {
	app := workspaceApp(t)
	files := rawInspectionFiles(t)
	declarations := map[string][2]string{
		"adt-cr.hl7":            {"raw", "cr"},
		"two-messages.mllp":     {"mllp", "cr"},
		"siu-lf.hl7":            {"raw", "lf"},
		"ack-crlf.hl7":          {"raw", "crlf"},
		"non-utf8.hl7":          {"raw", "cr"},
		"custom-delimiters.hl7": {"raw", "cr"},
		"mixed-encoding.hl7":    {"raw", "cr"},
		"positional.hl7":        {"raw", "cr"},
	}
	for name, path := range files {
		before := sourceStateOf(t, path)
		declared := declarations[name]
		for _, options := range [][2]string{{"auto", "auto"}, declared} {
			for _, values := range []bool{false, true} {
				args := []string{path, "--format", options[0], "--terminator", options[1]}
				if values {
					args = append(args, "--show-values")
				}
				stdout, stderr, err := inspectCommand(t, args...)
				if err != nil || stderr != "" {
					t.Fatalf("inspect %s %v: %v %s", name, options, err, stderr)
				}
				window := windowLines(t, app, desktop.RawInspectionRequest{File: path, Format: options[0], Terminator: options[1], ShowValues: values})
				if window != stdout {
					t.Errorf("the window and the command differ on %s %v values=%v:\nwindow:\n%s\ncommand:\n%s", name, options, values, window, stdout)
				}
			}
		}
		before.unchanged(t, path)
	}
	// Explicit values are escaped byte strings on both sides: a byte that is
	// not UTF-8 reaches neither the terminal nor the window unescaped.
	mixed := app.InspectRawFile(desktop.RawInspectionRequest{File: files["mixed-encoding.hl7"], Format: "auto", Terminator: "auto", ShowValues: true})
	found := false
	for _, row := range mixed.Inspection.Rows {
		if strings.Contains(row.Value, `\xe9\xff`) && strings.Contains(row.Value, `\u00e9`) {
			found = true
		}
		for _, r := range row.Value {
			if r < 0x20 || r > 0x7e {
				t.Fatalf("a value reached the window unescaped: %q", row.Value)
			}
		}
	}
	if !found {
		t.Fatal("the mixed-encoding field was not shown as the command shows it")
	}
	if listed, err := os.ReadDir(filepath.Dir(files["adt-cr.hl7"])); err != nil || len(listed) != len(files) {
		t.Fatalf("inspection wrote beside the files it read: %v %v", listed, err)
	}
}

// Malformed input, a declaration the bytes contradict, a file past the
// parser's bound, a folder and a missing file are refused by the window with
// the command's own sentence, and the refused file is never changed.
func TestRawInspectionRefusesWhatTheCommandRefuses(t *testing.T) {
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
		result := app.InspectRawFile(desktop.RawInspectionRequest{File: input.path, Format: input.format, Terminator: input.terminator})
		if cliErr == nil || result.State != desktop.Failed || result.Inspection != nil {
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
		}
	}
	// The declarations and the window are checked before anything is read.
	path := filepath.Join(dir, "raw.hl7")
	for request, want := range map[desktop.RawInspectionRequest]string{
		{File: path, Format: "hl7", Terminator: "auto"}:                  "format must be auto, raw, or mllp",
		{File: path, Format: "auto", Terminator: "nul"}:                  "terminator must be auto, cr, lf, or crlf",
		{File: "raw.hl7", Format: "auto", Terminator: "auto"}:            "choose the file with the file dialog; an inspection reads one file named by its full path",
		{File: path, Format: "auto", Terminator: "auto", Limit: 201}:     "an inspection window begins at or after the first row and shows at most 200 rows",
		{File: path, Format: "auto", Terminator: "auto", Offset: -1}:     "an inspection window begins at or after the first row and shows at most 200 rows",
		{File: path, Format: "auto", Terminator: "auto", Offset: 100000}: "the inspection window begins after the last row",
	} {
		if got := app.InspectRawFile(request); got.State != desktop.Failed || got.Reason != want {
			t.Errorf("%+v answered %+v, want %q", request, got, want)
		}
	}
}

// A round trip is the command's --roundtrip through the same operation: the
// copy is the source's bytes exactly, written once the file parsed, into one
// new entry of the chosen folder. An existing entry — the command's own copy
// or the source itself — is refused and left unchanged, a malformed file
// writes nothing, and the source is never changed.
func TestRoundTripWritesTheCommandsByteIdenticalCopy(t *testing.T) {
	app := workspaceApp(t)
	files := rawInspectionFiles(t)
	source := files["two-messages.mllp"]
	before := sourceStateOf(t, source)
	folder := t.TempDir()

	commandCopy := filepath.Join(folder, "command.mllp")
	if _, stderr, err := inspectCommand(t, source, "--format", "mllp", "--roundtrip", commandCopy); err != nil || stderr != "" {
		t.Fatalf("inspect --roundtrip: %v %s", err, stderr)
	}
	result := app.WriteRoundTrip(desktop.RoundTripRequest{File: source, Format: "mllp", Terminator: "auto", Folder: folder, Name: "window.mllp"})
	if result.State != desktop.Completed || result.Path != filepath.Join(resolved(t, folder), "window.mllp") || result.SHA256 != before.digest || int64(result.Bytes) != before.info.Size() {
		t.Fatalf("round trip: %+v", result)
	}
	original, _ := os.ReadFile(source)
	for _, path := range []string{commandCopy, result.Path} {
		if copied, err := os.ReadFile(path); err != nil || !bytes.Equal(copied, original) {
			t.Fatalf("%s is not the source byte for byte", filepath.Base(path))
		}
	}
	before.unchanged(t, source)

	for name, request := range map[string]desktop.RoundTripRequest{
		"the command's copy": {File: source, Format: "mllp", Terminator: "auto", Folder: folder, Name: "command.mllp"},
		"the source itself":  {File: source, Format: "mllp", Terminator: "auto", Folder: filepath.Dir(source), Name: filepath.Base(source)},
	} {
		got := app.WriteRoundTrip(request)
		if got.State != desktop.Failed || got.Reason != "cannot create round-trip file; destination must be new and writable" {
			t.Errorf("a round trip over %s answered %+v", name, got)
		}
	}
	before.unchanged(t, source)
	if copied, _ := os.ReadFile(commandCopy); !bytes.Equal(copied, original) {
		t.Fatal("a refused round trip changed an existing copy")
	}

	malformed := writeRaw(t, t.TempDir(), "malformed.mllp", []byte("\x0bMSH|^~\\&|A|B\r"))
	got := app.WriteRoundTrip(desktop.RoundTripRequest{File: malformed, Format: "auto", Terminator: "auto", Folder: folder, Name: "malformed.mllp"})
	if got.State != desktop.Failed {
		t.Fatalf("a malformed file was copied: %+v", got)
	}
	if _, err := os.Lstat(filepath.Join(folder, "malformed.mllp")); !os.IsNotExist(err) {
		t.Fatal("a malformed file's round trip left a file behind")
	}
	for name, request := range map[string]desktop.RoundTripRequest{
		"a nested name":           {File: source, Format: "auto", Terminator: "auto", Folder: folder, Name: "nested/copy.mllp"},
		"an escape":               {File: source, Format: "auto", Terminator: "auto", Folder: folder, Name: "../copy.mllp"},
		"no name":                 {File: source, Format: "auto", Terminator: "auto", Folder: folder},
		"a folder that is absent": {File: source, Format: "auto", Terminator: "auto", Folder: filepath.Join(folder, "absent"), Name: "copy.mllp"},
	} {
		if got := app.WriteRoundTrip(request); got.State != desktop.Failed {
			t.Errorf("a round trip to %s answered %+v", name, got)
		}
	}
	if listed := entriesOf(t, folder); len(listed) != 2 {
		t.Fatalf("refused round trips wrote into the folder: %v", listed)
	}
}

// The inspection dialogs choose exactly one file, or one folder, and a person
// who dismisses one is answered cancelled with nothing chosen.
func TestChooseInspectionPathKinds(t *testing.T) {
	c := &chooser{files: []string{"/chosen/file.hl7"}, folder: "/chosen/folder"}
	app := newApp(t, c)
	if got := app.ChooseInspectionPath("file"); got.State != desktop.Completed || got.Path != "/chosen/file.hl7" || got.Kind != "file" {
		t.Fatalf("file: %+v", got)
	}
	if got := app.ChooseInspectionPath("round-trip-folder"); got.State != desktop.Completed || got.Path != "/chosen/folder" {
		t.Fatalf("round-trip-folder: %+v", got)
	}
	if got := strings.Join(c.titles, "|"); got != "Choose the HL7 file to inspect|Choose the folder for the byte-identical copy" {
		t.Fatalf("dialog titles: %s", got)
	}
	c.files = []string{"/chosen/a.hl7", "/chosen/b.hl7"}
	if got := app.ChooseInspectionPath("file"); got.State != desktop.Failed || got.Path != "" || got.Reason != "choose exactly one file" {
		t.Fatalf("two files: %+v", got)
	}
	c.files, c.folder = nil, ""
	if got := app.ChooseInspectionPath("file"); got.State != desktop.Cancelled || got.Path != "" {
		t.Fatalf("dismissed: %+v", got)
	}
	if got := app.ChooseInspectionPath("round-trip-folder"); got.State != desktop.Cancelled || got.Path != "" {
		t.Fatalf("dismissed folder: %+v", got)
	}
	if got := app.ChooseInspectionPath("elsewhere"); got.State != desktop.Failed {
		t.Fatalf("unknown kind: %+v", got)
	}
}

// A field longer than the window's value bound is shown escaped in part, and
// the row says so; the rest of the row, and every other row, is what the
// command prints, which shows the whole value.
func TestRawInspectionShowsALongValueInPartAndSaysSo(t *testing.T) {
	app := workspaceApp(t)
	long := bytes.Repeat([]byte("\xe9A"), desktop.MaxInspectionValueBytes)
	path := writeRaw(t, t.TempDir(), "long.hl7",
		append(append([]byte("MSH|^~\\&|SYNTH|LAB|RECV|LAB|20260101120000||ORU^R01|LONG-1|P|2.5.1\rOBX|1|TX|NOTE||"), long...), '\r'))
	stdout, _, err := inspectCommand(t, path, "--show-values")
	if err != nil {
		t.Fatal(err)
	}
	result := app.InspectRawFile(desktop.RawInspectionRequest{File: path, Format: "auto", Terminator: "auto", ShowValues: true})
	if result.State != desktop.Completed {
		t.Fatal(result)
	}
	var cut []operation.InspectionRow
	for _, row := range result.Inspection.Rows {
		if row.ValueTruncated {
			cut = append(cut, row)
		} else if row.Value != "" && !strings.Contains(stdout, row.Value) {
			t.Errorf("a whole value the window showed is not the command's: %q", row.Value)
		}
	}
	if len(cut) != 1 || cut[0].Segment != "OBX" || cut[0].Field != 5 || cut[0].End-cut[0].Start != len(long) {
		t.Fatalf("the long field was not the one row shown in part: %+v", cut)
	}
	want := strconv.QuoteToASCII(string(long[:desktop.MaxInspectionValueBytes]))
	if cut[0].Value != want || !strings.Contains(stdout, strings.TrimSuffix(want, `"`)) {
		t.Fatalf("the value was not the escaped first %d bytes of the field", desktop.MaxInspectionValueBytes)
	}

	// A character the bound would split is left out whole, so what is shown
	// is the start of what the command prints, never a broken byte of it.
	text := append(bytes.Repeat([]byte("A"), desktop.MaxInspectionValueBytes-1), []byte("é and the rest")...)
	path = writeRaw(t, t.TempDir(), "utf8.hl7",
		append(append([]byte("MSH|^~\\&|SYNTH|LAB|RECV|LAB|20260101120000||ORU^R01|UTF8-1|P|2.5.1\rOBX|1|TX|NOTE||"), text...), '\r'))
	stdout, _, err = inspectCommand(t, path, "--show-values")
	if err != nil {
		t.Fatal(err)
	}
	result = app.InspectRawFile(desktop.RawInspectionRequest{File: path, Format: "auto", Terminator: "auto", ShowValues: true})
	var shown string
	for _, row := range result.Inspection.Rows {
		if row.ValueTruncated {
			shown = row.Value
		}
	}
	if shown == "" || strings.Contains(shown, `\x`) || !strings.Contains(stdout, strings.TrimSuffix(shown, `"`)) || strings.Contains(shown, `\u00e9`) {
		t.Fatalf("a value cut inside a character: %q", shown)
	}
}

// Every page after the first names the digest the earlier pages were read
// from. A file that changed between two pages is refused rather than shown as
// rows of two different files, and inspecting it again reads it whole.
func TestRawInspectionRefusesAPageOfAFileThatChanged(t *testing.T) {
	app := workspaceApp(t)
	path := rawInspectionFiles(t)["two-messages.mllp"]
	first := app.InspectRawFile(desktop.RawInspectionRequest{File: path, Format: "mllp", Terminator: "auto", Limit: 10})
	if first.State != desktop.Completed || first.Inspection.Total <= 10 {
		t.Fatalf("first page: %+v", first)
	}
	next := desktop.RawInspectionRequest{File: path, Format: "mllp", Terminator: "auto", Offset: 10, Limit: 10, Expect: first.Inspection.SHA256}
	if same := app.InspectRawFile(next); same.State != desktop.Completed || same.Inspection.SHA256 != first.Inspection.SHA256 {
		t.Fatalf("a page of the unchanged file: %+v", same)
	}
	changed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Replace(changed, []byte("FRAME-001"), []byte("FRAME-009"), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if refused := app.InspectRawFile(next); refused.State != desktop.Failed || refused.Inspection != nil ||
		refused.Reason != "the file changed since its earlier rows were read; inspect it again" {
		t.Fatalf("a page of a changed file: %+v", refused)
	}
	if again := app.InspectRawFile(desktop.RawInspectionRequest{File: path, Format: "mllp", Terminator: "auto"}); again.State != desktop.Completed || again.Inspection.SHA256 == first.Inspection.SHA256 {
		t.Fatalf("inspecting the changed file again: %+v", again)
	}
}
