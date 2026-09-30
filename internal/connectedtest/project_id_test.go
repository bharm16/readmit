package connectedtest_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/bharm16/readmit/internal/connectedtest"
)

func TestCompilationRetainsNumericCatalogProjectIDsWithoutChangingAuthoredNameRules(t *testing.T) {
	raw, files := example(t)
	var test connectedtest.Test
	if err := json.Unmarshal(raw, &test); err != nil {
		t.Fatal(err)
	}
	project := "0123456789abcdef01234567"
	test.Project, test.Environment.Project = project, project
	test.Checks.Project = project
	for i := range test.Steps {
		test.Steps[i].V2.Input.Project = project
	}
	raw, _ = json.Marshal(test)
	plan, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil || plan.Document().Test.Project != project || plan.Document().Environment.Project != project {
		t.Fatalf("canonical opaque project changed or refused: %v", err)
	}
	for _, mutation := range []struct {
		name   string
		change func(*connectedtest.Test)
	}{
		{"noncanonical numeric project", func(d *connectedtest.Test) {
			d.Project, d.Environment.Project = "1-not-a-catalog-id", "1-not-a-catalog-id"
		}},
		{"numeric step", func(d *connectedtest.Test) { d.Steps[0].ID = "1-book" }},
		{"numeric variable", func(d *connectedtest.Test) {
			d.Variables = []connectedtest.Variable{{ID: "1-key", Kind: "literal", Value: "value"}}
		}},
		{"foreign dependency", func(d *connectedtest.Test) { d.Checks.Project = "other" }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			d := test
			d.Steps = append([]connectedtest.Step(nil), test.Steps...)
			mutation.change(&d)
			raw, _ := json.Marshal(d)
			if _, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
				t.Fatal("project support relaxed another authored contract")
			}
		})
	}
}
