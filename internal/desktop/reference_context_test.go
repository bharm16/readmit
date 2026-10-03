package desktop_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

func TestInspectionProfileSelectionStaysSeparateAndPinsEveryLocalRead(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	profile := filepath.Join(root, "owned-profile.json")
	raw := `{"schema":"readmit-local-profile/v1","profile":{"id":"owned-adt","version":"1"},"base":{"pack":{"id":"owned-pack","version":"1"},"hl7_version":"2.5.1","family":"ADT"},"segments":[{"id":"PV1","description":"Owned local requirement","fields":[{"position":7,"usage":"C","condition":{"segment":"PV1","position":2,"operator":"present"},"type":"XCN","cardinality":{"min":0,"max":"2"}}]}]}`
	if err := os.WriteFile(profile, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	selected := app.ReadHL7ReferenceSelection(desktop.HL7ReferenceSelection{Profile: profile})
	if selected.State != desktop.Completed || selected.Overlay.Profile == nil || selected.Overlay.Profile.ID != "owned-adt" || selected.Overlay.Field != nil {
		t.Fatalf("profile summary: %+v", selected)
	}
	b := writeCase(t, root, "profile-inspection", framed("MSH|^~\\&|A|B|C|D|20260101||ADT^A08|id|P|2.5.1\rPV1|||||||123^EXAMPLE\r"))
	selection := selected.Overlay.Selection
	got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "profile-inspection", Identity: b.Identity, Occurrence: "s0001-e000001", Path: "PV1[1]-7", ByteOffset: -1, RawOffset: -1, ReferenceSelection: &selection})
	if got.State != desktop.Completed || got.Inspection.ReferenceOverlay.Field == nil || got.Inspection.ReferenceOverlay.Field.Usage != "C" || got.Inspection.ReferenceOverlay.Field.Condition == nil || got.Inspection.ReferenceOverlay.Pack != nil || got.Inspection.Raw != "" {
		t.Fatalf("profile/base/hidden values merged: %+v", got)
	}
	if err := os.WriteFile(profile, append([]byte(raw), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	changed := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "profile-inspection", Identity: b.Identity, Occurrence: "s0001-e000001", Path: "PV1[1]-7", ByteOffset: -1, RawOffset: -1, ReferenceSelection: &selection})
	if changed.State != desktop.Completed || changed.Inspection.ReferenceOverlay.Status != "not_available" || changed.Inspection.ReferenceOverlay.Field != nil {
		t.Fatalf("stale profile repopulated constraint: %+v", changed)
	}
}

func TestInspectorSegmentGridRetainsSiblingFieldsWhenAComponentIsSelected(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	b := writeCase(t, root, "grid-context", framed("MSH|^~\\&|SENDER|OWNED|RECV|LAB|20260101||SIU^S13|id|P|2.5.1\rSCH|OWNED\r"))
	got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "grid-context", Identity: b.Identity, Occurrence: "s0001-e000001", Path: "MSH[1]-9[1].2", ByteOffset: -1, RawOffset: -1, Reveal: true})
	if got.State != desktop.Completed || got.Inspection.Grid == nil {
		t.Fatalf("no bounded segment grid: %+v", got)
	}
	seen := map[string]bool{}
	for _, row := range got.Inspection.Grid.Rows {
		seen[row.Node.Path] = true
	}
	if !seen["MSH[1]-3"] || !seen["MSH[1]-12"] || !seen["MSH[1]-9[1].1"] || !seen["MSH[1]-9[1].2"] || got.Inspection.Selected.Path != "MSH[1]-9[1].2" || got.Inspection.RawWindow.Selected != "S13" {
		t.Fatalf("detail selection replaced sibling grid: %+v", got.Inspection)
	}
}

func TestInspectionProfilePackVerificationRequiresExactAuthoredIdentity(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	profile := filepath.Join(root, "profile.json")
	pack := filepath.Join(root, "pack.json")
	raw := `{"schema":"readmit-local-profile/v1","profile":{"id":"owned-adt","version":"1"},"base":{"pack":{"id":"fixture-siu","version":"1"},"hl7_version":"2.5.1","family":"ADT"},"segments":[{"id":"PV1","fields":[{"position":7,"usage":"R"}]}]}`
	if err := os.WriteFile(profile, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	packBytes, err := os.ReadFile("../../testdata/fixtures/profile-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(pack, packBytes, 0600); err != nil {
		t.Fatal(err)
	}
	verified := app.ReadHL7ReferenceSelection(desktop.HL7ReferenceSelection{Profile: profile, Pack: pack})
	if verified.State != desktop.Completed || verified.Overlay.Status != "profile_and_pack_verified" || verified.Overlay.Pack.ID != "fixture-siu" || verified.Overlay.Selection.PackIdentity == "" {
		t.Fatalf("unverified pack: %+v", verified)
	}
	mismatched := strings.Replace(string(packBytes), `"fixture-siu"`, `"different-owned-pack"`, 1)
	if err = os.WriteFile(pack, []byte(mismatched), 0600); err != nil {
		t.Fatal(err)
	}
	refused := app.ReadHL7ReferenceSelection(desktop.HL7ReferenceSelection{Profile: profile, Pack: pack})
	if refused.State != desktop.Failed || refused.Overlay.Status != "not_available" || refused.Overlay.Field != nil {
		t.Fatalf("mismatch claimed verified: %+v", refused)
	}
	changed := app.ReadHL7ReferenceSelection(verified.Overlay.Selection)
	if changed.State != desktop.Failed || !strings.Contains(changed.Reason, "changed") {
		t.Fatalf("saved selection silently repinned: %+v", changed)
	}
}

func TestInspectorGridPagesRepetitionsComponentsAndSubcomponentsWithExactLaterSpans(t *testing.T) {
	for _, test := range []struct{ name, delimiter, parent, later string }{{"repetitions", "~", "ZAA[1]-1", "ZAA[1]-1[101]"}, {"components", "^", "ZAA[1]-1[1]", "ZAA[1]-1[1].101"}, {"subcomponents", "&", "ZAA[1]-1[1].1", "ZAA[1]-1[1].1.101"}} {
		t.Run(test.name, func(t *testing.T) {
			app, root, _ := gridWorkspace(t)
			parts := []string{}
			for i := 1; i <= 101; i++ {
				parts = append(parts, fmt.Sprintf("OWNED_%03d", i))
			}
			wire := framed("MSH|^~\\&|A|B|C|D|20260101||SIU^S12|id|P|2.5.1\rZAA|" + strings.Join(parts, test.delimiter) + "|SIBLING\r")
			b := writeCase(t, root, "paged-branch", wire)
			request := desktop.InspectRequest{Workspace: root, Case: "paged-branch", Identity: b.Identity, Occurrence: "s0001-e000001", Path: test.parent, ByteOffset: -1, RawOffset: -1, Reveal: true}
			first := app.InspectOccurrence(request)
			request.NodeOffset = 100
			next := app.InspectOccurrence(request)
			if first.State != desktop.Completed || next.State != desktop.Completed || next.Inspection.ChildCount != 101 {
				t.Fatal(first, next)
			}
			seen := map[string]desktop.InspectorNode{}
			for _, row := range next.Inspection.Grid.Rows {
				seen[row.Node.Path] = row
			}
			row, ok := seen[test.later]
			if !ok || row.Raw != "OWNED_101" || row.Node.Start != strings.Index(wire, "OWNED_101") || row.Node.End-row.Node.Start != len("OWNED_101") {
				t.Fatalf("grid pager lost later exact span: %+v", next.Inspection.Grid)
			}
			if _, ok := seen["ZAA[1]-2"]; !ok {
				t.Fatal("paging lost sibling field context")
			}
			request.Path = test.later
			request.NodeOffset = 0
			selected := app.InspectOccurrence(request)
			if selected.State != desktop.Completed || selected.Inspection.RawWindow.Selected != "OWNED_101" {
				t.Fatalf("later selection lost original value: %+v", selected)
			}
			held := false
			for _, row := range selected.Inspection.Grid.Rows {
				if row.Node.Path == test.later {
					held = true
				}
			}
			if !held {
				t.Fatal("selected later occurrence disappeared from its grid branch")
			}
		})
	}
}

func TestInspectionProfileUnknownEditionNeverEnablesKnownFamilyMismatch(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	profile := filepath.Join(root, "owned-adt-no-edition.json")
	raw := `{"schema":"readmit-local-profile/v1","profile":{"id":"owned-adt","version":"1"},"base":{"pack":{"id":"owned-pack","version":"1"},"hl7_version":"2.5.1","family":"ADT"},"segments":[{"id":"PID","fields":[{"position":3,"usage":"R"}]}]}`
	if err := os.WriteFile(profile, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	selected := app.ReadHL7ReferenceSelection(desktop.HL7ReferenceSelection{Profile: profile})
	if selected.State != desktop.Completed {
		t.Fatal(selected)
	}
	for _, family := range []string{"SIU", "ADT"} {
		b := writeCase(t, root, "missing-edition-"+family, framed("MSH|^~\\&|A|B|C|D|20260101||"+family+"^A08|id|P|\rPID|1||OWNED\r"))
		selection := selected.Overlay.Selection
		got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "missing-edition-" + family, Identity: b.Identity, Occurrence: "s0001-e000001", Path: "PID[1]-3", ByteOffset: -1, RawOffset: -1, Reveal: true, ReferenceSelection: &selection})
		if got.State != desktop.Completed || got.Inspection.ReferenceOverlay.Field != nil {
			t.Fatalf("unknown source edition exposed applicable field constraints: %+v", got)
		}
		if family == "ADT" && got.Inspection.ReferenceOverlay.Applicability != "unknown_edition" {
			t.Fatal("unknown edition falsely established applicability")
		}
		if family == "SIU" && got.Inspection.ReferenceOverlay.Applicability != "incompatible" {
			t.Fatal("known family mismatch falsely established applicability")
		}
		if family == "SIU" && got.Inspection.ReferenceOverlay.Status != "not_available" {
			t.Fatal("known family mismatch was ignored")
		}
		if got.Inspection.Metadata.HL7Version != "" || got.Inspection.RawWindow.Selected != "OWNED" {
			t.Fatal("source bytes or actual missing edition changed")
		}
	}
}
