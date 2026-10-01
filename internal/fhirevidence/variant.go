package fhirevidence

import (
	"context"
	"encoding/json/v2"
	"errors"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/strictdoc"
)

const VariantSchema = "readmit-fhir-variant/v1"

type VariantEdit struct {
	Occurrence string          `json:"occurrence"`
	Selector   fhirr4.Selector `json:"selector"`
	Operator   string          `json:"operator"`
	Value      *dataset.Value  `json:"value,omitzero"`
}
type VariantPlan struct {
	Schema string        `json:"schema"`
	Parent string        `json:"parent"`
	Steps  []VariantEdit `json:"steps"`
}
type Change struct {
	Field  string           `json:"field"`
	Edit   VariantEdit      `json:"edit"`
	Before fhirr4.Selection `json:"before"`
	After  fhirr4.Selection `json:"after"`
}

func DecodeVariant(raw []byte) (VariantPlan, error) {
	var plan VariantPlan
	reader := strictdoc.Document{MaxBytes: 4 << 20, Schema: VariantSchema, Required: []string{"parent", "steps"}, Invalid: invalid.Error(), TooLarge: invalid.Error(), MustDeclare: invalid.Error(), Requires: invalid.Error(), Unsupported: invalid}
	if reader.Decode(raw, &plan) != nil || !digest.MatchString(plan.Parent) || len(plan.Steps) > 64 {
		return plan, invalid
	}
	for _, step := range plan.Steps {
		if step.Occurrence == "" || len(step.Occurrence) > 64 || step.Selector.Validate() != nil || step.Operator != "set" && step.Operator != "remove" || step.Operator == "set" && step.Value == nil || step.Operator == "remove" && step.Value != nil {
			return plan, invalid
		}
	}
	return plan, nil
}

// ResolveVariant replays every retained edit through the R4 primitive reader.
// It changes neither the source artifact nor any expected result.
func ResolveVariant(ctx context.Context, source *Artifact, plan VariantPlan) (*fhirr4.Document, []Change, error) {
	bad := errors.New("a typed R4 variant is bound to the exact retained resource evidence it was reviewed from")
	raw, err := json.Marshal(plan, json.Deterministic(true))
	if err != nil {
		return nil, nil, bad
	}
	if _, err = DecodeVariant(raw); err != nil || source == nil || source.Identity != plan.Parent || source.Document == nil {
		return nil, nil, bad
	}
	changed := source.Document
	changes := []Change{}
	for _, step := range plan.Steps {
		before := changed.Select(ctx, step.Occurrence, step.Selector)
		next, err := changed.EditPrimitive(ctx, step.Occurrence, step.Selector, step.Operator, step.Value)
		if err != nil {
			return nil, nil, err
		}
		changes = append(changes, Change{Field: step.Selector.String(), Edit: step, Before: before, After: next.Select(ctx, step.Occurrence, step.Selector)})
		changed = next
	}
	if _, _, err := Interpret(ctx, source.Manifest.Declaration, changed.Raw()); err != nil {
		return nil, nil, err
	}
	return changed, changes, nil
}

func CreateVariant(ctx context.Context, destination string, source *Artifact, plan VariantPlan) (*Artifact, error) {
	if len(plan.Steps) == 0 {
		return nil, errors.New("a reviewed R4 variant retains at least one explicit edit")
	}
	changed, _, err := ResolveVariant(ctx, source, plan)
	if err != nil {
		return nil, err
	}
	return Create(ctx, destination, source.Manifest.Declaration, changed.Raw(), Provenance{Mode: "derived", Parent: source.Identity, Derivation: VariantSchema})
}
