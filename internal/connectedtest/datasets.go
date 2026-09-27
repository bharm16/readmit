package connectedtest

import (
	"context"
	"slices"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const DatasetResultSchema = "readmit-dataset-execution-result/v1"

func validateTypedBindings(checks assertion.DatasetSetDocument, test Test, files map[string][]byte) error {
	if len(checks.Bindings) != len(test.Datasets) {
		return invalid
	}
	projections := map[string]dataset.Projection{}
	for _, b := range checks.Bindings {
		i := slices.IndexFunc(test.Datasets, func(d Dataset) bool { return d.ID == b.Name })
		if i < 0 {
			return invalid
		}
		d := test.Datasets[i]
		if d.Kind != "typed-rows" || d.Projection == nil || d.Namespace != b.Namespace || d.Phase != b.Phase || d.Source != b.Source {
			return invalid
		}
		p, err := dataset.DecodeProjection(files["dependencies/"+d.Projection.SHA256])
		if err != nil {
			return err
		}
		if p.Identity() != b.ProjectionIdentity {
			return invalid
		}
		projections[b.Name] = p
	}
	column := func(name, c string) bool {
		p, ok := projections[name]
		return ok && slices.ContainsFunc(p.Columns, func(v dataset.Column) bool { return v.Name == c })
	}
	selection := func(s assertion.RowSelection) bool {
		if _, ok := projections[s.Dataset]; !ok {
			return false
		}
		for _, w := range s.Where {
			if !column(s.Dataset, w.Column) {
				return false
			}
		}
		return true
	}
	compatible := func(name, c string, v dataset.Value) bool {
		p, ok := projections[name]
		if !ok {
			return false
		}
		i := slices.IndexFunc(p.Columns, func(column dataset.Column) bool { return column.Name == c })
		return i >= 0 && dataset.Compatible(p.Columns[i], v)
	}
	for _, a := range checks.Assertions {
		if !selection(a.Subject) || a.Column != "" && !column(a.Subject.Dataset, a.Column) || a.Other != nil && (!selection(*a.Other) || !column(a.Other.Dataset, a.OtherColumn)) || a.When != nil && (!selection(a.When.Subject) || !column(a.When.Subject.Dataset, a.When.Column)) {
			return invalid
		}
		subjects := []assertion.RowSelection{a.Subject}
		if a.Other != nil {
			subjects = append(subjects, *a.Other)
		}
		if a.When != nil {
			subjects = append(subjects, a.When.Subject)
			if !compatible(a.When.Subject.Dataset, a.When.Column, a.When.Equals) {
				return invalid
			}
		}
		for _, subject := range subjects {
			for _, w := range subject.Where {
				if !compatible(subject.Dataset, w.Column, w.Equals) {
					return invalid
				}
			}
		}
		if a.Expected != nil && !compatible(a.Subject.Dataset, a.Column, *a.Expected) {
			return invalid
		}
		for _, v := range a.Sequence {
			if !compatible(a.Subject.Dataset, a.Column, v) {
				return invalid
			}
		}

	}
	return nil
}

// CollectDataset is the IG06 engine handoff: choose a declared dataset by ID,
// then acquire using its compiled projection and exact selected source identity.
// No manual intermediate dataset-file step or live-read evaluator is involved.
func CollectDataset(ctx context.Context, p *Plan, id, instance string, request observesource.DatasetRequest) (*dataset.Snapshot, error) {
	if p == nil || p.document.Test.Schema != TestSchemaV2 && p.document.Test.Schema != TestSchemaV3 || !identifier.MatchString(instance) {
		return nil, invalid
	}
	i := slices.IndexFunc(p.document.Test.Datasets, func(d Dataset) bool { return d.ID == id })
	if i < 0 {
		return nil, invalid
	}
	d := p.document.Test.Datasets[i]
	if d.Kind != "typed-rows" || d.Projection == nil || request.Source.Identity() != d.Source {
		return nil, invalid
	}
	projection, err := dataset.DecodeProjection(p.files["dependencies/"+d.Projection.SHA256])
	if err != nil {
		return nil, err
	}
	if request.Network != nil && !networkScopeMatches(p, d.ID, request.Network.Binding()) {
		return nil, invalid
	}
	if request.DatabaseNetwork != nil && !networkScopeMatches(p, d.ID, request.DatabaseNetwork.Binding()) {
		return nil, invalid
	}
	request.Projection = projection
	request.Binding = dataset.Binding{Run: instance, Phase: d.Phase, Source: d.Source, Namespace: d.Namespace}
	return observesource.CollectDataset(ctx, request)
}

type DatasetResult struct {
	Schema        string                  `json:"schema"`
	PlanIdentity  string                  `json:"plan_identity"`
	CheckIdentity string                  `json:"check_identity"`
	Execution     Execution               `json:"execution"`
	Verdict       assertion.Verdict       `json:"verdict"`
	Report        assertion.DatasetReport `json:"report"`
}

// EvaluateDatasets consumes retained typed acquisitions through the same
// assertion package. The owning orchestrator still gates horizon completion;
// a single snapshot cannot settle a connected execution on its own.
func EvaluateDatasets(ctx context.Context, p *Plan, execution Execution, evidence map[string]*dataset.Snapshot) (DatasetResult, error) {
	if p == nil || p.document.Test.Schema != TestSchemaV2 && p.document.Test.Schema != TestSchemaV3 || p.document.Test.Checks.Schema != assertion.DatasetSchema {
		return DatasetResult{}, invalid
	}
	if err := validateExecution(p, execution); err != nil {
		return DatasetResult{}, err
	}
	if len(evidence) != len(p.document.Test.Datasets) {
		return DatasetResult{}, invalid
	}
	set, err := assertion.DecodeDatasets(p.files["dependencies/"+p.document.Test.Checks.SHA256])
	if err != nil {
		return DatasetResult{}, err
	}
	report, err := set.Evaluate(ctx, execution.Instance, evidence)
	if err != nil {
		return DatasetResult{}, err
	}
	verdict := report.Verdict
	if execution.State != "complete" && verdict == assertion.VerdictPass {
		verdict = assertion.VerdictUndecided
	}
	return DatasetResult{Schema: DatasetResultSchema, PlanIdentity: p.Identity(), CheckIdentity: p.document.Test.Checks.SHA256, Execution: execution, Verdict: verdict, Report: report}, nil
}

func networkScopeMatches(p *Plan, endpoint string, b networkaction.Binding) bool {
	env := p.document.Environment
	return b.Plan == p.Identity() && b.Project == env.Project && b.Environment == env.ID && b.Revision == env.Revision && b.Endpoint == endpoint && b.Policy == env.AddressPolicyIdentity && b.Operation == sendpolicy.ObservationRead
}
