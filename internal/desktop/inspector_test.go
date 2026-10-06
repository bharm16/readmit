package desktop_test

import (
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/dictionary"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/operation"
)

func TestInspectorSelectsExactOriginalRepeatedComponentBytes(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	grid := openGrid(t, app, root, "incident", 0, 50).Grid
	got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "incident", Identity: grid.Identity, Occurrence: grid.Rows[0].ID, Path: "PID[1]-3[1].4", ByteOffset: -1, Reveal: true})
	if got.State != desktop.Completed || got.Inspection == nil {
		t.Fatalf("inspect: %+v", got)
	}
	view := got.Inspection
	if view.Selected.Path != "PID[1]-3[1].4" || view.Raw != "READMIT" || view.Decoded != "READMIT" || view.Selected.State != "present" {
		t.Fatalf("selected wrong bytes: %+v", view)
	}
	row := view.Bytes[0]
	within := view.Selected.Start - row.Offset
	if row.Offset%desktop.HexRowBytes != 0 || within < 0 || within >= desktop.HexRowBytes ||
		hexCells(row)[within] != "52" || row.Text[within] != 'R' || view.ByteOffset != row.Offset {
		t.Fatalf("byte window lost selection: %+v", view.Bytes)
	}
	if view.SourceOffset != grid.Rows[0].Offset {
		t.Fatal("lost source position")
	}
}

func TestInspectorTreeStatesEncodingAndRefusals(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	// A second frame has a nonzero source offset. Raw controls decoded from X
	// escapes and HTML metacharacters must never enter presentation literally.
	message := "MSH|^~\\&|A|B|C|D|20260101||ADT^A01|id|P|2.5.1\rPID||\"\"|first^one&two~second^<script>\\X1B00\\\r"
	b := writeCase(t, root, "values", framed(gridBooking)+framed(message))
	before := fingerprint(t, root)
	request := desktop.InspectRequest{Workspace: root, Case: "values", Identity: b.Identity, Occurrence: "s0001-e000002", ByteOffset: -1, Reveal: true}
	read := func(path string) *desktop.Inspection {
		t.Helper()
		r := request
		r.Path = path
		got := app.InspectOccurrence(r)
		if got.State != desktop.Completed || got.Inspection == nil {
			t.Fatalf("%s: %+v", path, got)
		}
		return got.Inspection
	}
	for _, tc := range []struct{ path, state string }{{"PID[1]-1", "empty"}, {"PID[1]-2", "null"}, {"PID[1]-9", "omitted"}, {"PID[1]-3[9]", "omitted"}, {"ZZZ[1]-1", "omitted"}} {
		v := read(tc.path)
		if string(v.Selected.State) != tc.state {
			t.Fatalf("%s: %+v", tc.path, v)
		}
	}
	for _, tc := range []struct{ path, child string }{{"", "MSH[1]"}, {"PID[1]", "PID[1]-1"}, {"PID[1]-3", "PID[1]-3[1]"}, {"PID[1]-3[1]", "PID[1]-3[1].1"}, {"PID[1]-3[1].2", "PID[1]-3[1].2.1"}} {
		v := read(tc.path)
		if len(v.Children) == 0 || v.Children[0].Node.Path != tc.child {
			t.Fatalf("children %s: %+v", tc.path, v.Children)
		}
	}
	v := read("PID[1]-3[2].2")
	if v.Decoded != `\x3cscript\x3e\x1b\x00` || v.DecodeState != "decoded" || v.SourceOffset != len(framed(gridBooking)) {
		t.Fatalf("unsafe or wrong decoded bytes: %+v", v)
	}
	if read("MSH[1]-2").DecodeState != "decoded" {
		t.Fatal("delimiter declaration interpreted as escapes")
	}
	for _, edit := range []func(*desktop.InspectRequest){func(r *desktop.InspectRequest) { r.Identity = "stale" }, func(r *desktop.InspectRequest) { r.Occurrence = "absent" }, func(r *desktop.InspectRequest) { r.Path = "<script>" }, func(r *desktop.InspectRequest) { r.Case = "../values" }, func(r *desktop.InspectRequest) { r.ByteOffset = 99999 }, func(r *desktop.InspectRequest) { r.NodeOffset = -1 }} {
		r := request
		edit(&r)
		got := app.InspectOccurrence(r)
		if got.State != desktop.Failed || got.Inspection != nil {
			t.Fatalf("accepted bad selection %+v", got)
		}
	}
	if read("PID[1]-3[1].2.2").Raw != "two" {
		t.Fatal("did not recover after refused request")
	}
	if !maps.Equal(before, fingerprint(t, root)) {
		t.Fatal("inspection changed evidence or persisted values")
	}
}

func TestInspectorReportsUnsupportedEncodingAndEscapesWithoutReplacingBytes(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	for i, tc := range []struct{ charset, value, state string }{
		{"8859/1", "secret", "unsupported_encoding"},
		{"", "\\Zsecret\\", "unsupported_escape"},
		{"", "\\Xff\\", "invalid_encoding"},
		{"UNICODE UTF-8", "caf\xc3\xa9", "decoded"},
		{"UNICODE UTF-8", "\xff", "invalid_encoding"},
	} {
		name := fmt.Sprintf("encoding%d", i)
		header := []string{"MSH", "^~\\&", "A", "B", "C", "D", "20260101", "", "ADT^A01", "id", "P", "2.5.1", "", "", "", "", "", tc.charset}
		b := writeCase(t, root, name, framed(strings.Join(header, "|")+"\rPID|"+tc.value+"\r"))
		got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: name, Identity: b.Identity, Occurrence: b.Events[0].ID, Path: "PID[1]-1", ByteOffset: -1, Reveal: true})
		if got.Inspection == nil || got.Inspection.DecodeState != tc.state {
			t.Fatalf("%+v: %+v", tc, got)
		}
		if tc.state != "decoded" && got.Inspection.Decoded != "" {
			t.Fatal("unsupported decoding fabricated a value")
		}
		if tc.state == "decoded" && got.Inspection.Decoded != `caf\u00e9` {
			t.Fatalf("UTF-8: %q", got.Inspection.Decoded)
		}
	}
}

func TestInspectorWindowsEveryByteAndTreeChildWithoutTruncatingEvidence(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	message := "MSH|^~\\&|A|B|C|D|20260101||ADT^A01|id|P|2.5.1\rPID|" + strings.Repeat("x", 5000) + "\r" + strings.Repeat("NTE|one\r", 125)
	b := writeCase(t, root, "large", framed(message))
	request := desktop.InspectRequest{Workspace: root, Case: "large", Identity: b.Identity, Occurrence: b.Events[0].ID, ByteOffset: -1, Reveal: true}
	first := app.InspectOccurrence(request).Inspection
	if first == nil || first.ChildCount != 127 || len(first.Children) != desktop.InspectorNodeWindow {
		t.Fatalf("tree not bounded: %+v", first)
	}
	request.NodeOffset = 100
	last := app.InspectOccurrence(request).Inspection
	if len(last.Children) != 27 || last.Children[26].Node.Path != "NTE[125]" {
		t.Fatal("tree silently lost late children")
	}
	request.NodeOffset = 0
	request.Path = "PID[1]-1"
	large := app.InspectOccurrence(request).Inspection
	if large.DecodeState != "too_large" || large.Raw != "" {
		t.Fatal("large selected value not explicitly bounded")
	}
	request.Path = ""
	var wire []byte
	for offset := 0; offset < len(framed(message)); offset += desktop.InspectorByteWindow {
		request.ByteOffset = offset
		window := app.InspectOccurrence(request).Inspection
		if window == nil || len(window.Bytes) > desktop.InspectorByteWindow/desktop.HexRowBytes {
			t.Fatal("byte window not bounded")
		}
		for _, row := range window.Bytes {
			raw, err := hex.DecodeString(strings.Join(hexCells(row), ""))
			if err != nil || len(raw) > desktop.HexRowBytes || len(row.Text) != len(raw) {
				t.Fatal(err, row)
			}
			wire = append(wire, raw...)
		}
	}
	if string(wire) != framed(message) {
		t.Fatal("byte pages did not recover exact original framing and payload")
	}
}

func TestInspectorUnparsedAndChangedEvidenceStayExplicit(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	grid := openGrid(t, app, root, "incident", 0, 50).Grid
	request := desktop.InspectRequest{Workspace: root, Case: "incident", Identity: grid.Identity, Occurrence: grid.Rows[4].ID, ByteOffset: -1}
	got := app.InspectOccurrence(request)
	if got.State != desktop.Completed || got.Inspection.DecodeState != "unparsed" || len(got.Inspection.Children) != 0 || len(got.Inspection.Bytes) == 0 {
		t.Fatalf("unparsed evidence hidden: %+v", got)
	}
	b, err := bundle.Open(filepath.Join(root, "incident"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "incident", b.Events[0].Payload.Path), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	got = app.InspectOccurrence(request)
	if got.State != desktop.Failed || got.Inspection != nil {
		t.Fatalf("served changed evidence: %+v", got)
	}
}

func TestInspectorSharesOperationSlotAndRecoversAfterCancellation(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	grid := openGrid(t, app, root, "incident", 0, 50).Grid
	request := desktop.InspectRequest{Workspace: root, Case: "incident", Identity: grid.Identity, Occurrence: grid.Rows[0].ID, ByteOffset: -1}
	chooser := &chooser{folder: root}
	second := desktop.New(chooser, desktop.ShellDocuments{Folder: t.TempDir()})
	var concurrent desktop.InspectionResult
	chooser.before = func() { concurrent = second.InspectOccurrence(request); second.Cancel("") }
	second.SelectWorkspace()
	if concurrent.State != desktop.Busy || concurrent.Inspection != nil {
		t.Fatalf("inspector escaped operation slot: %+v", concurrent)
	}
	if result := second.InspectOccurrence(request); result.State != desktop.Completed {
		t.Fatalf("could not recover: %+v", result)
	}
}

func TestInspectorUsesOnlyVersionMatchedBundledFieldLabels(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	for _, tc := range []struct{ name, version, path, status, label string }{
		{"known", "2.5.1", "PID[1]-3[1].4", "labeled_field", "Patient Identifier List"},
		{"unknown", "2.5.1", "PID[1]-999", "unlabeled_position", ""},
		{"segment", "2.5.1", "PID[1]", "field_labels_only", ""},
		{"uncovered", "2.5.1", "ZZZ[1]-1", "unsupported_segment", ""},
		{"different", "2.6", "PID[1]-3", "unsupported_version", ""},
	} {
		b := writeCase(t, root, tc.name, framed(strings.Replace(gridBooking, "2.5.1", tc.version, 1)))
		result := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: tc.name, Identity: b.Identity, Occurrence: b.Events[0].ID, Path: tc.path, ByteOffset: -1})
		if result.Inspection == nil {
			t.Fatal(result)
		}
		metadata := result.Inspection.Metadata
		if metadata.Status != tc.status || metadata.Label != tc.label || metadata.HL7Version != tc.version {
			t.Fatalf("%s: %+v", tc.name, metadata)
		}
		if tc.status == "labeled_field" && (metadata.Contract != "readmit-field-labels/v1" || !strings.Contains(metadata.Provenance, "MPL-2.0")) {
			t.Fatal("label lost its provenance")
		}
	}
}

func TestInspectorBoundsUnsupportedEncodingMetadata(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	header := []string{"MSH", "^~\\&", "A", "B", "C", "D", "20260101", "", "ADT^A01", "id", "P", strings.Repeat("2", 5000), "", "", "", "", "", strings.Repeat("X", 5000)}
	b := writeCase(t, root, "long-metadata", framed(strings.Join(header, "|")+"\rPID|value\r"))
	got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "long-metadata", Identity: b.Identity, Occurrence: b.Events[0].ID, Path: "PID[1]-1", ByteOffset: -1, Reveal: true})
	if got.Inspection == nil {
		t.Fatal(got)
	}
	view := got.Inspection
	if len(view.Encoding) > 128 || len(view.Metadata.HL7Version) > 128 || !strings.Contains(view.Encoding, "exceeds") || !strings.Contains(view.Metadata.HL7Version, "exceeds") {
		t.Fatalf("unbounded or silently shortened metadata: encoding=%d version=%q", len(view.Encoding), view.Metadata.HL7Version)
	}
	if view.Raw != "value" || view.DecodeState != "unsupported_encoding" {
		t.Fatal("lost original selected value")
	}
}

func TestInspectorParentNavigationPreservesOmittedSegments(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	grid := openGrid(t, app, root, "incident", 0, 50).Grid
	for _, path := range []string{"ZZZ[1]-1", "PID[2]-1"} {
		t.Run(path, func(t *testing.T) {
			request := desktop.InspectRequest{Workspace: root, Case: "incident", Identity: grid.Identity, Occurrence: grid.Rows[0].ID, Path: path, ByteOffset: -1}
			field := app.InspectOccurrence(request)
			if field.State != desktop.Completed || field.Inspection.Selected.State != "omitted" {
				t.Fatalf("missing field: %+v", field)
			}
			request.Path = field.Inspection.Selected.Parent
			parent := app.InspectOccurrence(request)
			if parent.State != desktop.Completed || parent.Inspection == nil {
				t.Fatalf("parent navigation failed: %+v", parent)
			}
			segment := parent.Inspection.Selected
			if segment.Kind != "segment" || segment.State != "omitted" || segment.Start != 0 || segment.End != 0 || len(parent.Inspection.Children) != 0 {
				t.Fatalf("missing segment fabricated bytes or children: %+v", parent.Inspection)
			}
			request.Path = segment.Parent
			message := app.InspectOccurrence(request)
			if message.State != desktop.Completed || message.Inspection.Selected.Kind != "message" || message.Inspection.ChildCount != 2 {
				t.Fatalf("could not navigate back to message: %+v", message)
			}
		})
	}
}

// TestInspectorAndInspectCommandAgreeOnDeclaredLabels is the regression the
// one declared-version reader owes: a componentised MSH-12 such as 2.5.1^USA
// and a repeated MSH-12 are decided at the first component of the first
// repetition, so the window's occurrence inspector and `readmit inspect` name
// the same field with the same label — including where the command used to
// read the whole field and the window read the selector, and they disagreed.
func TestInspectorAndInspectCommandAgreeOnDeclaredLabels(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	for _, tc := range []struct{ name, version string }{
		{"componentised", "2.5.1^USA"},
		{"repeated-labels-first", "2.5.1~2.9"},
		{"repeated-other-first", "2.9^ZZZ~2.5.1"},
		{"unsupported", "2.6"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := strings.Replace(gridBooking, "2.5.1", tc.version, 1)
			b := writeCase(t, root, tc.name, framed(message))
			window := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: tc.name, Identity: b.Identity, Occurrence: b.Events[0].ID, Path: "PID[1]-3", ByteOffset: -1})
			if window.State != desktop.Completed || window.Inspection == nil {
				t.Fatalf("inspector: %+v", window)
			}
			metadata := window.Inspection.Metadata

			path := filepath.Join(t.TempDir(), tc.name+".hl7")
			if err := os.WriteFile(path, []byte(message), 0600); err != nil {
				t.Fatal(err)
			}
			command, err := operation.InspectFile(path, hl7.Options{Terminator: hl7.CR}, "")
			if err != nil {
				t.Fatalf("inspect: %v", err)
			}
			var profile, label string
			command.Rows(func(row operation.InspectionRow) bool {
				switch {
				case row.Kind == operation.InspectMessageRow && row.Message == 1:
					profile = row.Profile
				case row.Kind == operation.InspectFieldRow && row.Segment == "PID" && row.Field == 3:
					label = row.Label
					return false
				}
				return true
			})

			if label != metadata.Label {
				t.Fatalf("the command named PID-3 %q but the window named it %q", label, metadata.Label)
			}
			switch {
			case metadata.Status == dictionary.StatusLabeledField:
				if label == "" || profile != operation.InspectLabelled {
					t.Fatalf("the window labelled PID-3 but the command did not: %q %q", label, profile)
				}
			case metadata.Status == dictionary.StatusUnsupportedVersion:
				if label != "" || profile != operation.InspectPositional {
					t.Fatalf("the window refused the version but the command did not: %q %q", label, profile)
				}
			default:
				t.Fatalf("unexpected metadata status %q", metadata.Status)
			}
		})
	}
}

// hexCells splits one hex row into its byte cells.
func hexCells(row desktop.HexRow) []string { return strings.Fields(row.Hex) }

// Without an explicit reveal the inspector says where and what every part is
// and shows its hex, but no value text: not the selected value, not its decoded
// form and not the printable column of a hex row.
func TestInspectorWithholdsValueTextUntilRevealed(t *testing.T) {
	app, root, _, opened := messagesWorkspace(t)
	request := desktop.InspectRequest{Workspace: root, Case: "incident", Identity: opened.Identity, Occurrence: "s0001-e000001", Path: "PID[1]-5[1].1", ByteOffset: -1}
	hidden := app.InspectOccurrence(request)
	if hidden.State != desktop.Completed || hidden.Inspection == nil {
		t.Fatalf("inspect: %+v", hidden)
	}
	view := hidden.Inspection
	if view.Revealed || view.Raw != "" || view.Decoded != "" || view.DecodeState != "decoded" || view.Selected.State != hl7.Present || len(view.Bytes) == 0 {
		t.Fatalf("an unrevealed inspection: %+v", view)
	}
	for _, row := range view.Bytes {
		if row.Text != "" || row.Hex != "" {
			t.Fatalf("an unrevealed hex row carried text: %+v", row)
		}
	}
	if encoded, _ := json.Marshal(hidden); strings.Contains(string(encoded), "DOE") {
		t.Fatalf("an unrevealed inspection carried a value: %s", encoded)
	}
	request.Reveal = true
	shown := app.InspectOccurrence(request).Inspection
	if shown == nil || !shown.Revealed || shown.Raw != "DOE" || shown.Decoded != "DOE" || !strings.HasPrefix(shown.Bytes[0].Text[shown.Selected.Start-shown.ByteOffset:], "DOE") {
		t.Fatalf("a revealed inspection: %+v", shown)
	}
}

// The inspector names what it shows: the message type, the observed time, the
// readable segment names beside their codes, the dictionary label of each
// field, and the canonical selector a field filter names each one by.
func TestInspectorNamesSegmentsFieldsAndSelectorsAFilterCanUse(t *testing.T) {
	app, root, _, opened := messagesWorkspace(t)
	request := desktop.InspectRequest{Workspace: root, Case: "incident", Identity: opened.Identity, Occurrence: "s0001-e000001", ByteOffset: -1}
	message := app.InspectOccurrence(request).Inspection
	if message == nil || message.MessageCode != "SIU" || message.TriggerEvent != "S12" || message.ObservedAt == nil || !message.ObservedAt.Equal(*minute(10)) {
		t.Fatalf("message header: %+v", message)
	}
	segments := map[string]string{}
	for _, child := range message.Children {
		segments[child.Node.Path] = child.SegmentName
		if child.Selector != "" || child.Label != "" {
			t.Fatalf("a segment carried a field selector or label: %+v", child)
		}
	}
	if segments["MSH[1]"] != "Message header" || segments["PID[1]"] != "Patient identification" {
		t.Fatalf("segment names: %v", segments)
	}
	request.Path = "PID[1]"
	pid := app.InspectOccurrence(request).Inspection
	if pid.SegmentName != "Patient identification" || pid.Selector != "" {
		t.Fatalf("selected segment: %+v", pid)
	}
	third := pid.Children[2]
	if third.Node.Path != "PID[1]-3" || third.Selector != "PID[1]-3[1]" || third.Label != "Patient Identifier List" {
		t.Fatalf("PID-3: %+v", third)
	}
	request.Path = third.Node.Path
	field := app.InspectOccurrence(request).Inspection
	if field.Selector != "PID[1]-3[1]" || field.Children[0].Selector != "PID[1]-3[1]" {
		t.Fatalf("the field's selector does not name what a filter asks: %+v", field)
	}
	// The selector the inspector reports is one a filter accepts, and asks
	// exactly the field that was selected.
	filtered := readMessages(t, app, root, opened, grid.Query{Fields: []grid.FieldPredicate{{Selector: field.Selector, Match: index.Contains, Term: "MRN-1"}}})
	if filtered.State != desktop.Completed || !reflect.DeepEqual(rowIDs(filtered.Rows), []string{"s0001-e000001"}) {
		t.Fatalf("filter by the inspected field: %+v", filtered)
	}
	request.Path = "ZZZ[1]-1"
	unknown := app.InspectOccurrence(request).Inspection
	if unknown.SegmentName != "" || unknown.Metadata.Label != "" || unknown.Selector != "ZZZ[1]-1[1]" {
		t.Fatalf("an unknown segment was named: %+v", unknown)
	}
}

// A field row carries its value only once values are revealed, so a segment's
// fields can be read in place without selecting each one.
func TestInspectorChildRowsCarryValuesOnlyWhenRevealed(t *testing.T) {
	app, root, _, opened := messagesWorkspace(t)
	request := desktop.InspectRequest{Workspace: root, Case: "incident", Identity: opened.Identity, Occurrence: "s0001-e000001", Path: "PID[1]-5[1]", ByteOffset: -1}
	hidden := app.InspectOccurrence(request).Inspection
	if hidden == nil || len(hidden.Children) == 0 {
		t.Fatalf("inspect: %+v", hidden)
	}
	for _, child := range hidden.Children {
		if child.Value != "" {
			t.Fatalf("an unrevealed child carried a value: %+v", child)
		}
	}
	request.Reveal = true
	shown := app.InspectOccurrence(request).Inspection
	values := []string{}
	for _, child := range shown.Children {
		values = append(values, child.Value)
	}
	if len(values) == 0 || values[0] != "DOE" {
		t.Fatalf("revealed child values: %q", values)
	}
}

// escapedText is the escaped form the reader shows original bytes in: printable
// ASCII other than a backslash or an HTML metacharacter stands for itself, and
// every other byte is \xNN.
func escapedText(raw string) string {
	var out strings.Builder
	for _, b := range []byte(raw) {
		if b >= 32 && b < 127 && b != '\\' && b != '<' && b != '>' && b != '&' {
			out.WriteByte(b)
		} else {
			fmt.Fprintf(&out, `\x%02x`, b)
		}
	}
	return out.String()
}

// Raw is the whole message's escaped original text, in windows of whole bytes
// that together are exactly the message, with the selected field marked where
// it falls; an occurrence nothing could parse has its whole text too, and
// nothing is shown until values are revealed.
func TestInspectorRawWindowShowsTheWholeOccurrenceIncludingUnparsedAndLarge(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	message := "MSH|^~\\&|A|B|C|D|20260101||ADT^A01|id|P|2.5.1\rPID|1||" + strings.Repeat("x", 5000) + "||DOE^JANE\r" + strings.Repeat("NTE|one\\T\\two\r", 300)
	b := writeCase(t, root, "large", framed(message))
	request := desktop.InspectRequest{Workspace: root, Case: "large", Identity: b.Identity, Occurrence: b.Events[0].ID, ByteOffset: -1, RawOffset: 0, Reveal: true}
	var text strings.Builder
	offset := 0
	for pages := 0; ; pages++ {
		request.RawOffset = offset
		window := app.InspectOccurrence(request).Inspection.RawWindow
		if window == nil || window.MessageStart != 1 || window.MessageEnd != 1+len(message) || window.End-window.Offset > desktop.InspectorRawWindow ||
			window.Offset != 1+pages*desktop.InspectorRawWindow || window.Selected != "" {
			t.Fatalf("window %d: %+v", pages, window)
		}
		text.WriteString(window.Before + window.Selected + window.After)
		if window.End == window.MessageEnd {
			break
		}
		offset = window.End
	}
	if text.String() != escapedText(message) {
		t.Fatal("the Raw windows are not exactly the whole message's escaped text")
	}

	// A field in a later window: -1 opens the window holding it, marked.
	request.Path, request.RawOffset = "PID[1]-5", -1
	field := app.InspectOccurrence(request).Inspection
	if field == nil || field.RawWindow == nil || field.RawWindow.Selected != "DOE^JANE" || field.Raw != "DOE^JANE" ||
		field.RawWindow.Offset != 1+desktop.InspectorRawWindow || !strings.HasSuffix(field.RawWindow.Before, "x||") ||
		!strings.HasPrefix(field.RawWindow.After, `\x0dNTE|one\x5cT\x5ctwo`) {
		t.Fatalf("the selected field's window: %+v", field)
	}
	if past := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "large", Identity: b.Identity, Occurrence: b.Events[0].ID,
		ByteOffset: -1, RawOffset: len(framed(message)) + 1, Reveal: true}); past.State != desktop.Failed {
		t.Fatalf("a Raw window past the message: %+v", past)
	}
	request.Reveal = false
	if hidden := app.InspectOccurrence(request).Inspection; hidden.RawWindow != nil || hidden.Raw != "" {
		t.Fatalf("Raw text without a reveal: %+v", hidden.RawWindow)
	}

	grid := openGrid(t, app, root, "incident", 0, 50).Grid
	unparsed := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "incident", Identity: grid.Identity, Occurrence: grid.Rows[4].ID,
		ByteOffset: -1, RawOffset: -1, Reveal: true}).Inspection
	if unparsed == nil || unparsed.RawWindow == nil || unparsed.RawWindow.Before != escapedText(framed(gridGarbage)) || unparsed.RawWindow.Selected != "" {
		t.Fatalf("an unparsed occurrence's Raw text: %+v", unparsed.RawWindow)
	}
}

func TestInspectorPHIMasksAllValueSurfacesWithoutChangingSource(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	original := "MSH|^~\\&|SENDER|FACILITY|RECEIVER|FACILITY|20260101||ADT^A01|control|P|2.5.1\rPID|1||OWNED-ID||OWNED-NAME^GIVEN||19800101|F|||OWNED-STREET^CITY\rOBX|1|TX|NOTE||OWNED-NARRATIVE\rZAA|OWNED-EXTENSION\r"
	b := writeCase(t, root, "phi", framed(original))
	before := fingerprint(t, root)
	for _, path := range []string{"", "PID[1]", "PID[1]-3", "PID[1]-5[1].1", "OBX[1]-5", "ZAA[1]-1"} {
		request := desktop.InspectRequest{Workspace: root, Case: "phi", Identity: b.Identity, Occurrence: "s0001-e000001", Path: path, ByteOffset: -1, RawOffset: -1, Reveal: true, MaskPHI: true}
		got := app.InspectOccurrence(request)
		if got.State != desktop.Completed || got.Inspection == nil {
			t.Fatalf("%s: %+v", path, got)
		}
		body, _ := json.Marshal(got.Inspection)
		for _, secret := range []string{"OWNED-ID", "OWNED-NAME", "GIVEN", "19800101", "OWNED-STREET", "OWNED-NARRATIVE", "OWNED-EXTENSION"} {
			if strings.Contains(string(body), secret) {
				t.Fatalf("%s leaked %s", path, secret)
			}
		}
		if !got.Inspection.PHIMasked || !got.Inspection.Revealed || got.Inspection.ReadableWindow == nil {
			t.Fatal("masking hid the entire message")
		}
		text := got.Inspection.ReadableWindow.Before + got.Inspection.ReadableWindow.Selected + got.Inspection.ReadableWindow.After
		if !strings.Contains(text, "SENDER") || !strings.Contains(text, "ADT^A01") || !strings.Contains(text, "PID|1||********||**********^*****") {
			t.Fatalf("lost structure: %s", text)
		}
		request.MaskPHI = false
		plain := app.InspectOccurrence(request)
		if plain.Inspection.PHIMasked || plain.Inspection.Selected.Start != got.Inspection.Selected.Start || !strings.Contains(plain.Inspection.ReadableWindow.Before+plain.Inspection.ReadableWindow.Selected+plain.Inspection.ReadableWindow.After, "OWNED-NAME") {
			t.Fatal("toggle failed to restore original values and spans")
		}
	}
	if !reflect.DeepEqual(before, fingerprint(t, root)) {
		t.Fatal("display masking wrote evidence")
	}
}

func TestPHIMaskedFileHexProtectsAdjacentMessagesAndOriginalSyntax(t *testing.T) {
	app := desktop.New(nil, desktop.ShellDocuments{})
	file := filepath.Join(t.TempDir(), "messages.hl7")
	original := "MSH*^~\\&*A*B*C*D*20260101**ADT^A01*one*P*2.5.1\rPID*1**FIRST_ID**\"\"^GIVEN~SECOND^\"\"\rMSH|^~\\&|A|B|C|D|20260101||ADT^A01|two|P|2.5.1\rPID|1||NEIGHBOR_ID||NEIGHBOR_NAME\r"
	first, second, _ := strings.Cut(original, "MSH|^~")
	original = framed(first) + framed("MSH|^~"+second)
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	listed := app.ListFileMessages(desktop.FileMessagesRequest{File: file, Format: "auto", Terminator: "auto"})
	for _, path := range []string{"PID[1]-5[1].1", "PID[1]-5[1].2", "PID[1]-5[2].2"} {
		req := desktop.FileInspectRequest{File: file, Format: "auto", Terminator: "auto", Expect: listed.SHA256, Path: path, Reveal: true, MaskPHI: true, ByteOffset: 0, RawOffset: -1}
		masked := app.InspectFileMessage(req)
		req.MaskPHI = false
		plain := app.InspectFileMessage(req)
		if masked.Inspection == nil || plain.Inspection == nil {
			t.Fatalf("inspect: %+v", masked)
		}
		if masked.Inspection.Selected != plain.Inspection.Selected {
			t.Fatal("display masking changed a node, null state or original span")
		}
		var data []byte
		for _, row := range masked.Inspection.Bytes {
			part, err := hex.DecodeString(strings.Join(strings.Fields(row.Hex), ""))
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, part...)
		}
		for _, value := range []string{"FIRST_ID", "GIVEN", "SECOND", "NEIGHBOR_ID", "NEIGHBOR_NAME"} {
			if strings.Contains(string(data), value) {
				t.Fatalf("masked hex leaked %s", value)
			}
		}
		if !strings.Contains(string(data), "\"\"^#####~######^\"\"") {
			t.Fatalf("custom delimiter or null token changed: %s", data)
		}
	}
	after, _ := os.ReadFile(file)
	if string(after) != original {
		t.Fatal("masking changed the file")
	}
}
