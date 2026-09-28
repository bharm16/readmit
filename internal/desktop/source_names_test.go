package desktop_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
)

// sourceNamesOf is the name each row of a case reads by, in evidence order.
func sourceNamesOf(t *testing.T, app *desktop.App, root, name string, opened *bundle.Bundle) ([]string, []desktop.SourceFacet) {
	t.Helper()
	result := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: name, Identity: opened.Identity})
	if result.State != desktop.Completed {
		t.Fatalf("%s: %+v", name, result)
	}
	names := []string{}
	for _, row := range result.Rows {
		names = append(names, row.SourceID+"="+row.SourceName)
	}
	return names, result.Facets.Sources
}

// A source reads by the name declared for it — a collection session's label,
// a recipe's declared source in the case's own receipt, or the name a person
// gave it on the project — and otherwise by its exact source ID. A receipt of
// another case, an unmapped record and a plain import name nothing, and a
// file name is never a source's name.
func TestReadMessagesNamesSourcesByTheirDeclaredLabelsAndFallsBackToTheID(t *testing.T) {
	app, root, _, incident := messagesWorkspace(t)

	policy, err := collection.DecodePolicy([]byte(`{"schema":"readmit-receiver-policy/v1","name":"sink","source_label":"front-desk-feed","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]}}`))
	if err != nil {
		t.Fatal(err)
	}
	record := collection.Record{Schema: collection.Schema, SessionID: "0123456789abcdef0123456789abcdef", Policy: policy,
		ApplicationProcessing: collection.NoApplicationProcessing,
		Sessions:              []collection.Session{{SessionID: "c0001", SourceID: "s0001", Label: policy.SourceLabel}},
		Received: []collection.Received{{SessionID: "c0001", OccurrenceID: "s0001-e000001", ControlID: "CTL-1", Mode: collection.OriginalMode,
			Accept:      collection.Stage{Code: collection.NotAcknowledged, Destination: collection.NoDestination},
			Application: collection.Stage{Code: "AA", ControlID: "READMITCOLLECT000001", Destination: collection.SameConnection}}}}
	collected, err := bundle.WriteCollected(filepath.Join(root, "collected"), []bundle.Input{{Data: []byte(framed(gridBooking)),
		Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}, Observations: map[int]bundle.Observation{1: {Direction: bundle.Inbound, ObservedAt: minute(1)}}}},
		*minute(0), record)
	if err != nil {
		t.Fatal(err)
	}

	recipe := validDesktopRecipe()
	recipe.Source = importer.LabelMapping{Operator: importer.FieldLabel, Locator: importer.Locator{"source"}}
	export := filepath.Join(t.TempDir(), "export.csv")
	csv := "time,payload,source,channel\n2026-01-01T12:00:00Z,\"" + sampleImportHL7 + "\",lab-feed,ch\n2026-01-01T12:01:00Z,\"" + sampleImportHL7 + "\",,ch\n"
	if err := os.WriteFile(export, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	mapped, receipt, err := operation.ImportRecipeCommit(context.Background(), recipe, []string{export}, nil, nil,
		filepath.Join(root, "recipe"), filepath.Join(root, "recipe-receipt.json"))
	if err != nil || receipt.UnmappedRecords != 1 {
		t.Fatalf("recipe import: %+v %v", receipt, err)
	}
	// The recipe's receipt beside another case describes that other case.
	data, err := os.ReadFile(filepath.Join(root, "recipe-receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, "incident-receipt.json", string(data))

	for _, expected := range []struct {
		name    string
		opened  *bundle.Bundle
		rows    []string
		sources []desktop.SourceFacet
	}{
		{"incident", incident, []string{"s0001=", "s0001=", "s0001=", "s0001=", "s0001="}, []desktop.SourceFacet{{ID: "s0001"}}},
		{"collected", collected, []string{"s0001=front-desk-feed"}, []desktop.SourceFacet{{ID: "s0001", Name: "front-desk-feed"}}},
		{"recipe", mapped, []string{"s0001=lab-feed", "s0002="}, []desktop.SourceFacet{{ID: "s0001", Name: "lab-feed"}, {ID: "s0002"}}},
	} {
		rows, sources := sourceNamesOf(t, app, root, expected.name, expected.opened)
		if !reflect.DeepEqual(rows, expected.rows) || !reflect.DeepEqual(sources, expected.sources) {
			t.Fatalf("%s: rows %v, sources %+v", expected.name, rows, sources)
		}
	}
	// The query still names a source by its ID.
	filtered := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: "recipe", Identity: mapped.Identity, Query: grid.Query{Sources: []string{"s0001"}}})
	if filtered.Matched != 1 || filtered.Rows[0].SourceName != "lab-feed" {
		t.Fatalf("filtering by the named source's ID: %+v", filtered)
	}
	// A metadata search finds a source by its declared name and by its ID.
	for term, want := range map[string][]string{"lab-feed": {"s0001-e000001"}, "s0002": {"s0002-e000001"}} {
		found := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: "recipe", Identity: mapped.Identity,
			Query: grid.Query{Search: &grid.TextSearch{Scope: grid.SearchMetadata, Text: term}}})
		if !reflect.DeepEqual(rowIDs(found.Rows), want) {
			t.Fatalf("a metadata search for %q found %v", term, rowIDs(found.Rows))
		}
	}
	inspected := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "recipe", Identity: mapped.Identity, Occurrence: "s0001-e000001", ByteOffset: -1, RawOffset: -1})
	if inspected.Inspection == nil || inspected.Inspection.SourceName != "lab-feed" || inspected.Inspection.Direction != bundle.Inbound {
		t.Fatalf("the reader's source and direction: %+v", inspected)
	}

	// A name a person gave a source on the project entry of this exact case
	// is the one it reads by; an entry of another identity names nothing.
	document := project.Document{Schema: project.SchemaV2, Settings: project.Settings{Title: "Scheduling QA"}, Cases: []project.Case{
		{Name: "collected", Identity: collected.Identity, Schema: collected.Manifest.Schema, Provenance: string(collected.Manifest.Provenance.Mode),
			Title: "Collected", Status: project.StatusOpen, Sources: []project.Source{{ID: "s0001", Name: "Front desk"}}},
		{Name: "recipe", Identity: incident.Identity, Schema: mapped.Manifest.Schema, Provenance: string(mapped.Manifest.Provenance.Mode),
			Title: "Recipe", Status: project.StatusOpen, Sources: []project.Source{{ID: "s0002", Name: "Not this case"}}},
	}}
	if err := project.WriteDocument(root, document); err != nil {
		t.Fatal(err)
	}
	if rows, _ := sourceNamesOf(t, app, root, "collected", collected); !reflect.DeepEqual(rows, []string{"s0001=Front desk"}) {
		t.Fatalf("a person's name for the source: %v", rows)
	}
	if rows, _ := sourceNamesOf(t, app, root, "recipe", mapped); !reflect.DeepEqual(rows, []string{"s0001=lab-feed", "s0002="}) {
		t.Fatalf("a project entry of another identity named a source: %v", rows)
	}
}

// Edit details names a case's sources in a readmit-project/v2 project: the
// names are saved whole, a blank name leaves a source unnamed, a source the
// case does not declare is refused at its member, and leaving the names out
// keeps them.
func TestEditDetailsNamesTheSourcesTheCaseDeclares(t *testing.T) {
	app, _, context := casesProject(t)
	item := caseAt(t, app, context, "regression")
	if sources := item.Summary.Case.Sources; !reflect.DeepEqual(sources, []project.Source{{ID: "s0001"}}) {
		t.Fatalf("the case's sources before: %+v", sources)
	}
	draft := desktop.CaseDraft{Name: item.Name, Status: item.Summary.Case.Status, Tags: item.Summary.Case.Tags,
		InterfaceRevision: item.Summary.Case.InterfaceRevision, Incidents: item.Summary.Case.Incidents,
		Sources: []project.Source{{ID: "s0001", Name: "  Front desk "}}}
	saved := saveCase(app, context, item, "click-1", draft)
	if saved.State != desktop.Completed || !reflect.DeepEqual(saved.Projection.Case.Sources, []project.Source{{ID: "s0001", Name: "Front desk"}}) {
		t.Fatalf("save: %+v", saved)
	}
	item = caseAt(t, app, context, "regression")
	if !reflect.DeepEqual(item.Summary.Case.Sources, []project.Source{{ID: "s0001", Name: "Front desk"}}) {
		t.Fatalf("the case's sources after: %+v", item.Summary.Case.Sources)
	}
	opened, err := bundle.Open(filepath.Join(context.Project, "regression"))
	if err != nil {
		t.Fatal(err)
	}
	listed := app.ReadMessages(desktop.MessagesRequest{Workspace: context.Project, Case: "regression", Identity: opened.Identity, Limit: 1})
	if listed.State != desktop.Completed || listed.Rows[0].SourceName != "Front desk" {
		t.Fatalf("the list after naming: %+v", listed)
	}

	undeclared := draft
	undeclared.Sources = []project.Source{{ID: "s0009", Name: "Elsewhere"}}
	if refused := saveCase(app, context, item, "click-2", undeclared); refused.Outcome != desktop.InvalidOutcome ||
		len(refused.Problems) != 1 || refused.Problems[0].Field != "case.sources" {
		t.Fatalf("an undeclared source: %+v", refused)
	}
	kept := draft
	kept.Name, kept.Sources = "Renamed", nil
	if saved := saveCase(app, context, item, "click-3", kept); saved.State != desktop.Completed ||
		!reflect.DeepEqual(saved.Projection.Case.Sources, []project.Source{{ID: "s0001", Name: "Front desk"}}) {
		t.Fatalf("leaving the names out: %+v", saved)
	}
	item = caseAt(t, app, context, "regression")
	cleared := kept
	cleared.Sources = []project.Source{{ID: "s0001", Name: " "}}
	if saved := saveCase(app, context, item, "click-4", cleared); saved.State != desktop.Completed || len(saved.Projection.Case.Sources) != 0 {
		t.Fatalf("a blank name: %+v", saved)
	}
	if reread, err := project.Open(context.Project); err != nil || reread.Document.Cases[0].Sources != nil {
		t.Fatalf("an unnamed source was stored: %+v %v", reread, err)
	}
}
