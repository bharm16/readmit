package desktop_test

import (
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/transform"
)

func TestSaveTransformPlanAuthorsAndPreviews(t *testing.T) {
	app, root, identity := transformWorkspace(t, "")
	saved := app.SaveTransformPlan(desktop.TransformPlanRequest{
		Workspace: root, Case: "incident", Identity: identity,
		Rules: "rules.json",
		Steps: []transform.Step{
			{Operator: transform.RebaseIdentifiers, Rule: "patient"},
			{Operator: transform.ShiftDates, Shift: "24h"},
		},
		Output: "authored-plan.json",
	})
	if saved.State != desktop.Completed || saved.Plan == nil || saved.Plan.Output != "authored-plan.json" {
		t.Fatalf("save: %+v", saved)
	}
	previewed := app.PreviewTransformation(desktop.TransformRequest{
		Workspace: root, Case: "incident", Identity: identity,
		Rules: "rules.json", Plan: "authored-plan.json",
	})
	if previewed.State != desktop.Completed || previewed.Transformation == nil {
		t.Fatalf("preview authored plan: %+v", previewed)
	}
	if len(previewed.Transformation.Preview.Plan.Steps) != 2 {
		t.Fatalf("preview lost authored steps: %+v", previewed.Transformation.Preview.Plan)
	}
	opened := app.OpenTransformPlan(root, "authored-plan.json")
	if opened.State != desktop.Completed || opened.Plan == nil || opened.Plan.Digest != saved.Plan.Digest {
		t.Fatalf("open: %+v", opened)
	}
}
