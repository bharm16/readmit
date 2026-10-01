package desktop

import (
	"context"
	"encoding/json/v2"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/fhirevidence"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/transform"
)

type FHIRVariantView struct {
	Parent    string                `json:"parent"`
	Resources []fhirr4.Resource     `json:"resources"`
	Changes   []fhirevidence.Change `json:"changes"`
}
type fhirVariantResolved struct {
	plan    FHIRVariantPlan
	changes []fhirevidence.Change
	changed *fhirr4.Document
}

func resolveFHIRVariant(source variantSource, draft VariantDraft) (*variantResolved, []FieldProblem) {
	if source.fhir == nil || draft.FHIR == nil {
		return nil, variantProblem("variant.fhir", "R4 evidence requires a typed FHIR plan")
	}
	if draft.Plan.Schema != "" || draft.Plan.Case != "" || len(draft.Plan.Steps) > 0 || draft.Transform != nil {
		return nil, variantProblem("variant.fhir", "v2 selectors and sequence transforms cannot be applied to R4 evidence; no retained clause is removed")
	}
	plan := *draft.FHIR
	if plan.Schema == "" {
		plan.Schema = fhirevidence.VariantSchema
	}
	changed, changes, err := fhirevidence.ResolveVariant(context.Background(), source.fhir, plan)
	if err != nil {
		return nil, variantProblem("variant.fhir", err.Error())
	}
	return &variantResolved{source: source, fhir: &fhirVariantResolved{plan: plan, changes: changes, changed: changed}}, nil
}

func validateFHIRVariantDraft(scope draftScope, draft ItemDraft) ([]catalog.Staged, ItemDraft, []FieldProblem) {
	resolved, problems := resolveVariant(scope, *draft.Variant, false)
	if problems != nil {
		return nil, ItemDraft{Name: draft.Name}, problems
	}
	if len(resolved.fhir.plan.Steps) == 0 {
		return nil, ItemDraft{Name: draft.Name}, variantProblem("variant.fhir.steps", "a reviewed R4 variant retains at least one explicit edit")
	}
	plan := resolved.fhir.plan
	data, err := json.Marshal(plan, json.Deterministic(true))
	if err != nil {
		return nil, ItemDraft{}, variantProblem("variant.fhir", "the typed plan cannot be retained")
	}
	normalized := ItemDraft{Name: draft.Name, Variant: &VariantDraft{Source: ItemRef{Kind: draft.Variant.Source.Kind, ID: draft.Variant.Source.ID}, FHIR: &plan}}
	return []catalog.Staged{{Role: "plan", File: "plan.json", Data: append(data, '\n')}}, normalized, nil
}
func fhirVariantEntry(resolved *variantResolved) *catalog.Entry {
	return &catalog.Entry{Prefix: variantPrefix, Owes: resolved.source.entry, Build: func(path string) error {
		_, err := fhirevidence.CreateVariant(context.Background(), path, resolved.source.fhir, resolved.fhir.plan)
		return err
	}}
}
func (r *variantResolved) fhirView(reveal bool) *VariantView {
	view := &VariantView{FHIR: &FHIRVariantView{Parent: r.fhir.plan.Parent, Resources: r.source.fhir.Document.Resources(), Changes: []fhirevidence.Change{}}, Messages: []VariantMessage{}, Sequence: []VariantEntry{}, Changes: []VariantChange{}, Relations: []VariantRelation{}, Profile: []transform.Combination{}, Notes: []VariantNote{}, Blocking: []VariantNote{}, Revealed: reveal}
	for _, change := range r.fhir.changes {
		if !reveal {
			hideFHIRSelection(&change.Before)
			hideFHIRSelection(&change.After)
			change.Edit.Value = nil
		}
		view.FHIR.Changes = append(view.FHIR.Changes, change)
	}
	if !reveal {
		for i := range view.FHIR.Resources {
			resource := &view.FHIR.Resources[i]
			resource.Type = fhirResourceCaption(resource.Type)
			resource.Base, resource.FullURL, resource.LogicalID, resource.VersionID, resource.CanonicalURL, resource.CanonicalVersion = "", "", "", "", "", ""
			for j := range resource.Identifiers {
				resource.Identifiers[j] = fhirr4.BusinessID{}
			}
		}
	}
	return view
}
