package desktop_test

import (
	"encoding/hex"
	"fmt"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectorSelectsExactOriginalRepeatedComponentBytes(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	grid := openGrid(t, app, root, "incident", 0, 50).Grid
	got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "incident", Identity: grid.Identity, Occurrence: grid.Rows[0].ID, Path: "PID[1]-3[1].4", ByteOffset: -1})
	if got.State != desktop.Completed || got.Inspection == nil {
		t.Fatalf("inspect: %+v", got)
	}
	view := got.Inspection
	if view.Selected.Path != "PID[1]-3[1].4" || view.Raw != "READMIT" || view.Decoded != "READMIT" || view.Selected.State != "present" {
		t.Fatalf("selected wrong bytes: %+v", view)
	}
	if view.Bytes[0].Offset != view.Selected.Start || view.Bytes[0].Hex != "52" || !view.Bytes[0].Selected {
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
	request := desktop.InspectRequest{Workspace: root, Case: "values", Identity: b.Identity, Occurrence: "s0001-e000002", ByteOffset: -1}
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
		if len(v.Children) == 0 || v.Children[0].Path != tc.child {
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
		got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: name, Identity: b.Identity, Occurrence: b.Events[0].ID, Path: "PID[1]-1", ByteOffset: -1})
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
	request := desktop.InspectRequest{Workspace: root, Case: "large", Identity: b.Identity, Occurrence: b.Events[0].ID, ByteOffset: -1}
	first := app.InspectOccurrence(request).Inspection
	if first == nil || first.ChildCount != 127 || len(first.Children) != desktop.InspectorNodeWindow {
		t.Fatalf("tree not bounded: %+v", first)
	}
	request.NodeOffset = 100
	last := app.InspectOccurrence(request).Inspection
	if len(last.Children) != 27 || last.Children[26].Path != "NTE[125]" {
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
		if window == nil || len(window.Bytes) > desktop.InspectorByteWindow {
			t.Fatal("byte window not bounded")
		}
		for _, cell := range window.Bytes {
			raw, err := hex.DecodeString(cell.Hex)
			if err != nil {
				t.Fatal(err)
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
	app, root, filters := gridWorkspace(t)
	grid := openGrid(t, app, root, "incident", 0, 50).Grid
	request := desktop.InspectRequest{Workspace: root, Case: "incident", Identity: grid.Identity, Occurrence: grid.Rows[0].ID, ByteOffset: -1}
	chooser := &chooser{folder: root}
	second := desktop.New(chooser, filepath.Join(t.TempDir(), "recent.json"), filters)
	var concurrent desktop.InspectionResult
	chooser.before = func() { concurrent = second.InspectOccurrence(request); second.Cancel() }
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
	got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "long-metadata", Identity: b.Identity, Occurrence: b.Events[0].ID, Path: "PID[1]-1", ByteOffset: -1})
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
