package desktop_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/index"
)

// The field picker lists every field position the case's parsed messages
// hold, each once in the order it first occurs, with its bundled label and the
// selector a field rule names it by, and carries no value. A listed selector
// is one a query accepts.
func TestMessageFieldsListsPresentFieldsWithLabelsAndNoValues(t *testing.T) {
	app, root, _, opened := messagesWorkspace(t)
	result := app.MessageFields(desktop.MessageFieldsRequest{Workspace: root, Case: "incident", Identity: opened.Identity})
	if result.State != desktop.Completed || !result.Complete || len(result.Fields) == 0 {
		t.Fatalf("fields: %+v", result)
	}
	seen := map[string]bool{}
	byKey := map[string]desktop.MessageField{}
	for _, field := range result.Fields {
		if seen[field.Selector] {
			t.Fatalf("%s is listed twice", field.Selector)
		}
		seen[field.Selector] = true
		byKey[field.Selector] = field
		query := grid.Query{Fields: []grid.FieldPredicate{{Selector: field.Selector, Match: index.State, State: "present"}}}
		if err := grid.ValidateQuery(query); err != nil {
			t.Fatalf("%s is not a selector a rule can name: %v", field.Selector, err)
		}
	}
	if patient := byKey[patientField]; patient.Segment != "PID" || patient.Field != 3 || patient.Label != "Patient Identifier List" || patient.SegmentName == "" {
		t.Fatalf("the patient identifier: %+v", patient)
	}
	if outcome := byKey[grid.AckCodeSelector]; outcome.Segment != "MSA" || outcome.Field != 1 {
		t.Fatalf("the acknowledgement code: %+v", outcome)
	}
	if result.Fields[0].Segment != "MSH" {
		t.Fatalf("fields are not in the order they first occur: %+v", result.Fields[0])
	}
	data, _ := json.Marshal(result)
	for _, value := range []string{"MRN-1", "DOE", "CTL-1", "NOT-HL7"} {
		if strings.Contains(string(data), value) {
			t.Fatalf("the field list carries the value %q", value)
		}
	}
	if refused := app.MessageFields(desktop.MessageFieldsRequest{Workspace: root, Case: "incident", Identity: strings.Repeat("0", 64)}); refused.State != desktop.Failed {
		t.Fatalf("a case of another identity: %+v", refused)
	}
}
