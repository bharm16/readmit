package hl7reference_test

import (
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/hl7reference"
	"github.com/bharm16/readmit/internal/profileeval"
	"os"
	"testing"
)

func TestMessageStructureInferenceNeverFillsTransmittedStructure(t *testing.T) {
	var d map[string]any
	_ = json.Unmarshal([]byte(catalog), &d)
	na := hl7reference.Attribute{State: "not_applicable"}
	section := hl7reference.Attribute{State: "specified", Value: "10.4.2"}
	row := func(key, kind, name string) hl7reference.Record {
		return hl7reference.Record{Key: key, Kind: kind, Name: name, Datatype: na, Optionality: na, Length: na, ConformanceLength: na, Repetition: na, Item: na, Table: na, Section: section, Definition: "Owned overview.", Source: "owned-chapter.pdf", ContentState: "available"}
	}
	message := row("message/SIU/S13", "message", "Appointment rescheduling")
	message.MessageCode = "SIU"
	message.Event = "S13"
	message.Structures = []string{"SIU_S12"}
	structure := row("structure/SIU_S12", "structure", "SIU_S12")
	structure.StructureID = "SIU_S12"
	structure.Sequence = []profileeval.Node{{Name: "header", Segment: "MSH", Min: 1, Max: "1"}, {Name: "appointment", Segment: "SCH", Min: 1, Max: "1"}}
	var old []hl7reference.Record
	b, _ := json.Marshal(d["records"])
	_ = json.Unmarshal(b, &old)
	d["records"] = append(old, message, structure)
	d["schema"] = hl7reference.SchemaV4
	d["coverage"] = hl7reference.Coverage{Segments: 1, Fields: 1, Messages: 1, Structures: 1, Definitions: 4, Missing: []string{}}
	raw, _ := json.Marshal(d)
	c, err := hl7reference.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := c.MessageContext("2.5.1", "SIU", "S13", "", hl7.Omitted)
	if got.Status != "inferred" || got.ResolvedStructure != "SIU_S12" || got.DeclaredStructure != "" || got.DeclaredStructureState != hl7.Omitted {
		t.Fatalf("manufactured transmitted structure: %+v", got)
	}
	wrong := c.MessageContext("2.5.1", "SIU", "S13", "ADT_A01", hl7.Present)
	if wrong.Status != "contradictory" || wrong.ResolvedStructure != "" || wrong.DeclaredStructure != "ADT_A01" {
		t.Fatalf("concealed contradiction: %+v", wrong)
	}
}

func TestOfficialV251MessageEventsRetainSourceStructureOmission(t *testing.T) {
	path := os.Getenv("READMIT_HL7_CONTEXT_CATALOG")
	if path == "" {
		t.Skip("requires explicitly selected controlled-source message catalog")
	}
	c, err := hl7reference.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ code, event, structure, section string }{{"SIU", "S13", "SIU_S12", "10.4.2"}, {"ADT", "A08", "ADT_A01", "3.3.8"}} {
		context := c.MessageContext("2.5.1", want.code, want.event, "", hl7.Omitted)
		if context.Status != "inferred" || context.ResolvedStructure != want.structure || context.DeclaredStructureState != hl7.Omitted || context.DeclaredStructure != "" {
			t.Fatalf("wrong source context: %+v", context)
		}
		event, _, _, err := c.Entity("2.5.1", context.MessageKey, 0, 100)
		if err != nil || event.Record == nil || event.Record.Section.Value != want.section || event.Record.Definition == "" || len(c.Sequence(context.StructureKey)) == 0 {
			t.Fatalf("unsourced event/structure: %+v %v", event, err)
		}
	}
}
