package desktop_test

import (
	"encoding/json/v2"
	"os"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7reference"
)

func TestOfflineReferenceSearchAndElementsUseBoundedPinnedCatalog(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	path := ownedCompositionReference(t, root)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Schema   string                `json:"schema"`
		Edition  string                `json:"edition"`
		Sources  []hl7reference.Source `json:"sources"`
		Coverage hl7reference.Coverage `json:"coverage"`
		Records  []hl7reference.Record `json:"records"`
	}
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	na := hl7reference.Attribute{State: "not_applicable"}
	section := hl7reference.Attribute{State: "specified", Value: "2.17.2"}
	entity := func(key, kind, name, meaning string) hl7reference.Record {
		return hl7reference.Record{Key: key, Kind: kind, Name: name, Datatype: na, Optionality: na, Length: na, ConformanceLength: na, Repetition: na, Item: na, Table: na, Section: section, Definition: meaning, Source: "owned-table.pdf", ContentState: "available"}
	}
	table := entity("table/0003", "table", "Event type", "Owned table definition.")
	table.TableID = "0003"
	table.TableKind = "hl7"
	code := entity(hl7reference.CodeKey("0003", "S13"), "code", "S13", "Owned rescheduling notice.")
	code.Code = "S13"
	code.TableID = "0003"
	element := entity("element/00009", "element", "Message Type", "Owned full element definition.")
	element.ItemID = "00009"
	element.Section = hl7reference.Attribute{State: "specified", Value: "2.15.9.9"}
	document.Schema = hl7reference.SchemaV3
	document.Records = append(document.Records, table, code, element)
	document.Coverage.Tables = 1
	document.Coverage.Codes = 1
	document.Coverage.Elements = 1
	document.Coverage.Definitions += 3
	raw, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	summary := app.ReadReferenceCatalog(path)
	if summary.State != desktop.Completed {
		t.Fatalf("summary: %+v", summary)
	}
	request := desktop.HL7ReferenceRequest{Catalog: path, Identity: summary.Reference.Identity, Edition: "2.5.1", Key: "table/0003", Query: "resched", Limit: 1}
	got := app.LookupHL7Reference(request)
	if got.State != desktop.Completed || got.Reference.Record.TableID != "0003" || got.TotalCount != 1 || got.ChildCount != 1 || len(got.Children) != 1 || got.Children[0].Code != "S13" {
		t.Fatalf("bounded source search: %+v", got)
	}
	request.Key = "element/00009"
	request.Query = ""
	elementRead := app.LookupHL7Reference(request)
	if elementRead.State != desktop.Completed || elementRead.Reference.Record.ItemID != "00009" || elementRead.Reference.Record.Section.Value != "2.15.9.9" {
		t.Fatalf("exact element: %+v", elementRead)
	}
	request.Key = "table/0003"
	request.Edition = "2.4"
	wrong := app.LookupHL7Reference(request)
	if wrong.State != desktop.Completed || wrong.Reference.Status != "unsupported_edition" || wrong.Reference.Record != nil || len(wrong.Children) != 0 {
		t.Fatalf("borrowed other edition: %+v", wrong)
	}
}
