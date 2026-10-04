package hl7reference_test

import (
	"github.com/bharm16/readmit/internal/hl7reference"
	"os"
	"path/filepath"
	"testing"
)

func TestOfficialEarlierReferenceEditionsRetainTheirOwnAttributesAndInventories(t *testing.T) {
	dir := os.Getenv("READMIT_HL7_REFERENCE_DIRECTORY")
	if dir == "" {
		t.Skip("requires explicitly selected private official-source catalogs")
	}
	// Independent expected values checked in the pinned edition chapter tables:
	// v231 control 2.24.1.9/PV1 3.3.3.7; v24 control 2.16.9.9/PV1 3.4.3.7;
	// v25 control 2.15.9.9/PV1 3.4.3.7. They are not computed by extraction.
	for _, want := range []struct{ edition, file, datatype, length, section, doctorLength, componentType, componentSection string }{
		{"2.3.1", "hl7-v231-qualification.json", "CM", "7", "2.24.1.9", "60", "", "2.8.49.2"},
		{"2.4", "hl7-v24-qualification.json", "CM", "13", "2.16.9.9", "250", "FN", "2.9.52.2"},
		{"2.5", "hl7-v25-qualification.json", "MSG", "15", "2.15.9.9", "250", "FN", "2.A.86.2"},
	} {
		t.Run(want.edition, func(t *testing.T) {
			c, err := hl7reference.Read(filepath.Join(dir, want.file))
			if err != nil {
				t.Fatal(err)
			}
			field := c.Lookup(want.edition, "MSH", 9)
			if field.Record == nil {
				t.Fatal("missing message type")
			}
			r := field.Record
			if r.Name != "Message Type" || r.Datatype.Value != want.datatype || r.Length.Value != want.length || r.Section.Value != want.section || r.Item.Value != "00009" || r.Optionality.Value != "R" || r.Definition == "" {
				t.Fatalf("wrong edition attributes: %+v", r)
			}
			doctor := c.Lookup(want.edition, "PV1", 7)
			if doctor.Record == nil || doctor.Record.Name != "Attending Doctor" || doctor.Record.Length.Value != want.doctorLength || doctor.Record.Repetition.Value != "Y" {
				t.Fatalf("doctor edition mismatch: %+v", doctor)
			}
			component, _, _, err := c.Entity(want.edition, "component/XCN/2", 0, 100)
			if err != nil || component.Record == nil || component.Record.Datatype.Value != want.componentType || component.Record.Section.Value != want.componentSection || component.Record.Item.State != "not_applicable" {
				t.Fatalf("component inherited another edition: %+v %v", component, err)
			}
			coverage := c.Summary().Coverage
			if coverage.Segments < 100 || coverage.Fields < 1400 || coverage.Datatypes < 50 || coverage.Components < 190 || coverage.Tables < 300 || coverage.Codes < 600 || coverage.Elements < 1200 || coverage.Messages < 190 || coverage.Structures < 110 || coverage.Definitions < 3500 || len(coverage.Missing) == 0 {
				t.Fatalf("missing generic inventory or explicit gaps: %+v", coverage)
			}
			if c.Lookup("9.9", "MSH", 9).Record != nil {
				t.Fatal("borrowed a different edition")
			}
		})
	}
}

func TestOfficialLaterReferenceEditionsPreserveLengthsAndConformanceNotation(t *testing.T) {
	dir := os.Getenv("READMIT_HL7_LATER_REFERENCE_DIRECTORY")
	if dir == "" {
		t.Skip("requires explicit private later-edition catalogs")
	}
	for _, ed := range []struct{ edition, file string }{{"2.6", "v26"}, {"2.7.1", "v271"}, {"2.8.2", "v282"}} {
		t.Run(ed.edition, func(t *testing.T) {
			c, err := hl7reference.Read(filepath.Join(dir, "hl7-"+ed.file+"-qualification.json"))
			if err != nil {
				t.Fatal(err)
			}
			// Chapter 2 attribute tables independently checked from supplied PDFs, not
			// generated expected output; all editions retain their own section numbering.
			for _, want := range []struct {
				field                     int
				dt, length, clen, section string
			}{{2, "ST", "4..5", "", "2.14.9.2"}, {7, "DTM", "", "", "2.14.9.7"}, {8, "ST", "", "40=", "2.14.9.8"}, {10, "ST", "1..199", "=", "2.14.9.10"}} {
				if ed.edition == "2.6" {
					want.length = map[int]string{2: "4", 7: "24", 8: "40", 10: "199"}[want.field]
					want.clen = ""
				}
				got := c.Lookup(ed.edition, "MSH", want.field)
				if got.Record == nil {
					t.Fatal("missing reference field")
				}
				r := got.Record
				if r.Datatype.Value != want.dt || r.Length.Value != want.length || r.ConformanceLength.Value != want.clen || r.Section.Value != want.section || r.Definition == "" {
					t.Fatalf("source notation reduced: %+v", r)
				}
				if want.length == "" && r.Length.State != "not_specified" {
					t.Fatal("blank became inferred length")
				}
				if want.clen == "" && r.ConformanceLength.State != "not_specified" {
					t.Fatal("blank became conformance length")
				}
			}
			coverage := c.Summary().Coverage
			if coverage.Fields < 2400 || coverage.Segments < 170 || coverage.Components < 440 || coverage.Tables < 530 || coverage.Codes < 3300 || coverage.Elements < 2100 || coverage.Messages < 310 || coverage.Structures < 185 || coverage.Definitions < 9300 || len(coverage.Missing) == 0 {
				t.Fatalf("partial source inventory: %+v", coverage)
			}
		})
	}
}
