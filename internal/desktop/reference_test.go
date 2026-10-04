package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7reference"
)

func ownedReferenceCatalog(t *testing.T, root string) string {
	t.Helper()
	specified := func(v string) hl7reference.Attribute { return hl7reference.Attribute{State: "specified", Value: v} }
	absent := hl7reference.Attribute{State: "not_specified"}
	record := hl7reference.Record{Key: "field/ZAA/1", Kind: "field", Segment: "ZAA", Field: 1, Name: "Owned value", Datatype: specified("ST"), Optionality: specified("C"), Length: specified("1..16"), ConformanceLength: absent, Repetition: specified("Y/5"), Item: specified("90001"), Table: absent, Section: specified("3.9.1.8"), Definition: "Owned definition with its explicit condition.", Source: "owned-chapter.pdf"}
	body, err := json.Marshal(struct {
		Schema   string                `json:"schema"`
		Edition  string                `json:"edition"`
		Sources  []hl7reference.Source `json:"sources"`
		Coverage hl7reference.Coverage `json:"coverage"`
		Records  []hl7reference.Record `json:"records"`
	}{hl7reference.Schema, "2.5.1", []hl7reference.Source{{Role: "standard", File: "owned-chapter.pdf", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Publisher: "Owned fixture"}}, hl7reference.Coverage{Fields: 1, Definitions: 1, Missing: []string{}}, []hl7reference.Record{record}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "owned-reference.json")
	if err = os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInspectorReferenceCatalogKeepsEvidenceAndTruthfulAvailability(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	path := ownedReferenceCatalog(t, root)
	b := writeCase(t, root, "local-fields", framed("MSH|^~\\&|A|B|C|D|20260101||ADT^A01|id|P|2.5.1\rZAA|A\\F\\B\r"))
	request := desktop.InspectRequest{Workspace: root, Case: "local-fields", Identity: b.Identity, Occurrence: "s0001-e000001", Path: "ZAA[1]-1", ByteOffset: -1, RawOffset: -1, Reveal: true, ReferenceCatalog: path}
	got := app.InspectOccurrence(request)
	if got.State != desktop.Completed || got.Inspection == nil {
		t.Fatalf("read: %+v", got)
	}
	v := got.Inspection
	if v.Raw != "A\\x5cF\\x5cB" || v.Decoded != "A|B" || v.Reference == nil || v.Reference.Record == nil || v.Reference.Record.Item.Value != "90001" || v.Reference.Record.Section.Value != "3.9.1.8" {
		t.Fatalf("reference/span/decoding: %+v", v)
	}
	if v.ReadableWindow == nil || v.ReadableWindow.Selected != "A\\F\\B" || !strings.Contains(v.ReadableWindow.Before, "\nZAA|") || v.ReadableWindow.Offset != v.RawWindow.Offset || v.ReadableWindow.End != v.RawWindow.End {
		t.Fatalf("readable original changed spans or encoded values: %+v", v.ReadableWindow)
	}
	if v.RawWindow.Selected != v.Raw {
		t.Fatal("grid and original-byte selection differ")
	}
	if summary := app.ReadReferenceCatalog(path); summary.State != desktop.Completed || summary.Reference.Record != nil || summary.Reference.Coverage.Fields != 1 {
		t.Fatalf("bounded summary: %+v", summary)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	corrupt := app.InspectOccurrence(request)
	if corrupt.State != desktop.Completed || corrupt.Inspection.Raw != v.Raw || corrupt.Inspection.Reference.Status != "not_available" {
		t.Fatalf("catalog failure prevented inspection: %+v", corrupt)
	}
	request.ReferenceCatalog = ""
	request.Path = "ZAA[1]-1[1].1"
	child := app.InspectOccurrence(request)
	if child.State != desktop.Completed || child.Inspection.Metadata.Label != "" {
		t.Fatalf("component inherited field label: %+v", child)
	}
}

func TestInspectorExplicitReferenceEditionDoesNotRewriteDeclaredMessage(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	path := ownedReferenceCatalog(t, root)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `"edition":"2.5.1"`, `"edition":"2.4"`, 1))
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	b := writeCase(t, root, "alternate-reference", framed("MSH|^~\\&|A|B|C|D|20260101||ADT^A01|id|P|2.5.1\rZAA|OWNED\r"))
	got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "alternate-reference", Identity: b.Identity, Occurrence: "s0001-e000001", Path: "ZAA[1]-1", ByteOffset: -1, RawOffset: -1, ReferenceCatalog: path, Reveal: true})
	if got.State != desktop.Completed || got.Inspection.Reference.Record == nil || got.Inspection.Reference.Edition != "2.4" || got.Inspection.Metadata.HL7Version != "2.5.1" || got.Inspection.Raw != "OWNED" {
		t.Fatalf("explicit reference changed declaration or failed to browse: %+v", got)
	}
}

func TestInspectorCatalogPinRefusesChangedBytesBetweenPublicReadsWithoutLosingRaw(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	path := ownedReferenceCatalog(t, root)
	selected := app.ReadReferenceCatalog(path)
	if selected.State != desktop.Completed {
		t.Fatal(selected)
	}
	b := writeCase(t, root, "pinned-reference", framed("MSH|^~\\&|A|B|C|D|20260101||ADT^A01|id|P|2.5.1\rZAA|OWNED\r"))
	request := desktop.InspectRequest{Workspace: root, Case: "pinned-reference", Identity: b.Identity, Occurrence: "s0001-e000001", Path: "ZAA[1]-1", ByteOffset: -1, RawOffset: -1, ReferenceCatalog: path, ReferenceIdentity: selected.Reference.Identity, Reveal: true}
	before := app.InspectOccurrence(request)
	if before.State != desktop.Completed || before.Inspection.Reference.Record == nil {
		t.Fatalf("first pinned read: %+v", before)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	changed := app.InspectOccurrence(request)
	if changed.State != desktop.Completed || changed.Inspection.Reference.Status != "not_available" || changed.Inspection.Reference.Record != nil || changed.Inspection.Raw != before.Inspection.Raw || changed.Inspection.ReadableWindow.Selected != "OWNED" || len(changed.Inspection.ReferenceValues) != 0 || changed.Inspection.MessageContext != nil {
		t.Fatalf("catalog silently repinned or raw erased: %+v", changed)
	}
	request.ReferenceIdentity = ""
	compatible := app.InspectOccurrence(request)
	if compatible.State != desktop.Completed || compatible.Inspection.Reference.Record == nil {
		t.Fatalf("old single-read caller broke: %+v", compatible)
	}
	file := filepath.Join(root, "pin.mllp")
	if err = os.WriteFile(file, []byte(framed("MSH|^~\\&|A|B|C|D|20260101||ADT^A01|id|P|2.5.1\rZAA|OWNED\r")), 0600); err != nil {
		t.Fatal(err)
	}
	listing := app.ListFileMessages(desktop.FileMessagesRequest{File: file, Format: "auto", Terminator: "auto", Limit: 100})
	loose := app.InspectFileMessage(desktop.FileInspectRequest{File: file, Format: "auto", Terminator: "auto", Expect: listing.SHA256, Message: 0, Path: "ZAA[1]-1", ByteOffset: -1, RawOffset: -1, ReferenceCatalog: path, ReferenceIdentity: selected.Reference.Identity, Reveal: true})
	if loose.State != desktop.Completed || loose.Inspection.Reference.Record != nil || loose.Inspection.Raw != "OWNED" || loose.Inspection.Reference.Status != "not_available" {
		t.Fatalf("loose-file pin TOCTOU: %+v", loose)
	}
}
