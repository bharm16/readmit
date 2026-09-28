package runcompare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"maps"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
)

// The dimensions a connected comparison keeps apart. A behavior change is
// attributed to one of them only when it is the single one that changed, and
// even then the attribution is a statement about declarations, not causality.
const (
	DimensionDefinition  = "check-definition"
	DimensionInput       = "input"
	DimensionEnvironment = "environment"
	DimensionBoundary    = "protocol-boundary"
	DimensionTarget      = "target"
	DimensionProfile     = "profile-validator"
	DimensionCompletion  = "completion-policy"
	DimensionCollector   = "collector"
	DimensionEngine      = "engine"
)

var flowDimensions = []string{DimensionDefinition, DimensionInput, DimensionEnvironment, DimensionBoundary, DimensionTarget, DimensionProfile, DimensionCompletion, DimensionCollector, DimensionEngine}

// FlowExecution is one compared lifecycle as its verified result states it.
type FlowExecution struct {
	Identity string `json:"identity"`
	Schema   string `json:"schema"`
	Plan     string `json:"plan"`
	Instance string `json:"instance"`
	Engine   string `json:"engine"`
	State    string `json:"state"`
	Verdict  string `json:"verdict"`
}

// FlowDimension says whether one declared dimension changed and, value-free,
// which declarations within it did: a phase, step, dataset or server ID.
type FlowDimension struct {
	Dimension string   `json:"dimension"`
	State     string   `json:"state"`
	Changed   []string `json:"changed"`
}

// FlowAttribution never names a cause when several dimensions changed.
type FlowAttribution struct {
	Outcome string   `json:"outcome"`
	Changed []string `json:"changed"`
	Reason  string   `json:"reason"`
}

// FlowCheckComparison compares one check by phase and ID. Behavior is compared
// only when its definition is unchanged and both outcomes were decided.
type FlowCheckComparison struct {
	Phase      string `json:"phase"`
	Check      string `json:"check"`
	Baseline   string `json:"baseline"`
	Current    string `json:"current"`
	Definition string `json:"definition"`
	Behavior   string `json:"behavior"`
}

// FlowKeyComparison compares the observed records sharing one declared key.
// Key is a digest of the key values, never the values themselves. Counts keep
// multiplicity: a duplicate is two records under one key, never one. Values
// names field columns whose observed values differ; Identities names identity
// and reference columns, whose server-assigned values can differ between two
// runs without any field changing.
type FlowKeyComparison struct {
	Key        string   `json:"key"`
	Baseline   int      `json:"baseline"`
	Current    int      `json:"current"`
	State      string   `json:"state"`
	Values     []string `json:"values"`
	Identities []string `json:"identities"`
}

// FlowRecordComparison matches one dataset's final records through its
// declared key columns. Unusable, missing or redefined observations are not
// compared: a missing observation is never an empty one.
type FlowRecordComparison struct {
	Phase   string              `json:"phase"`
	Dataset string              `json:"dataset"`
	State   string              `json:"state"`
	Reason  string              `json:"reason,omitzero"`
	Keys    []FlowKeyComparison `json:"keys"`
}

// FlowComparison is an in-memory view of two verified retained lifecycles; it
// is not a stored evidence contract and it never contacts a source.
type FlowComparison struct {
	Baseline    FlowExecution          `json:"baseline"`
	Current     FlowExecution          `json:"current"`
	Dimensions  []FlowDimension        `json:"dimensions"`
	Attribution FlowAttribution        `json:"attribution"`
	Checks      []FlowCheckComparison  `json:"checks"`
	Records     []FlowRecordComparison `json:"records"`
	Scope       string                 `json:"scope"`
}

const FlowScope = "Compared offline from two verified retained lifecycles. Declared definitions, inputs, environment, protocol boundary, target, profile and validator, completion policy, collector and engine are compared separately; a behavior change is never attributed to one of them when several changed, and a single declared change is still not proof of causality. Records are matched only through declared key columns, keeping multiplicity. Target software beyond its declared revision and unobserved state remain unknown."

// CompareFlows opens both lifecycles through the verified reader and compares
// them. Comparing a lifecycle with itself is allowed and shows no change.
func CompareFlows(ctx context.Context, baseline, current string) (FlowComparison, error) {
	left, err := connectedrun.OpenFlowEvidence(ctx, baseline)
	if err != nil {
		return FlowComparison{}, err
	}
	if err := ctx.Err(); err != nil {
		return FlowComparison{}, err
	}
	right, err := connectedrun.OpenFlowEvidence(ctx, current)
	if err != nil {
		return FlowComparison{}, err
	}
	return CompareFlowEvidence(ctx, left, right)
}

// CompareFlowEvidence compares two already verified lifecycles.
func CompareFlowEvidence(ctx context.Context, left, right connectedrun.FlowEvidence) (FlowComparison, error) {
	c := FlowComparison{Baseline: flowExecution(left), Current: flowExecution(right), Dimensions: []FlowDimension{}, Checks: []FlowCheckComparison{}, Records: []FlowRecordComparison{}, Scope: FlowScope}
	a, b := fingerprints(left), fingerprints(right)
	changedDimensions := []string{}
	for _, dimension := range flowDimensions {
		d := FlowDimension{Dimension: dimension, State: "unchanged", Changed: differing(a[dimension], b[dimension])}
		if len(d.Changed) > 0 {
			d.State = "changed"
			changedDimensions = append(changedDimensions, dimension)
		}
		c.Dimensions = append(c.Dimensions, d)
	}
	c.Attribution = FlowAttribution{Outcome: "no-declared-change", Changed: changedDimensions, Reason: "No declared dimension changed; an outcome difference comes from the observed target or its state, which remain otherwise unknown."}
	switch {
	case len(changedDimensions) == 1:
		c.Attribution.Outcome = "single-declared-change"
		c.Attribution.Reason = "Only one declared dimension changed. That is the only declared difference, not proof that it caused an outcome difference."
	case len(changedDimensions) > 1:
		c.Attribution.Outcome = "multiple-declared-changes"
		c.Attribution.Reason = "Several declared dimensions changed; no outcome difference is attributed to any one of them."
	}
	c.Checks = compareFlowChecks(left, right)
	for _, phase := range phaseOrder(left, right) {
		if err := ctx.Err(); err != nil {
			return FlowComparison{}, err
		}
		c.Records = append(c.Records, compareFlowRecords(left, right, phase)...)
	}
	return c, nil
}

func flowExecution(e connectedrun.FlowEvidence) FlowExecution {
	r := e.Result
	return FlowExecution{Identity: e.Identity, Schema: r.Schema, Plan: r.Plan, Instance: r.Instance, Engine: r.Engine, State: r.State, Verdict: string(r.Verdict)}
}

func canonical(v any) string {
	raw, _ := json.Marshal(v, json.Deterministic(true))
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// fingerprints names, for every dimension, each declaration it holds and the
// digest of that declaration, read from the lifecycle's own pinned plan and
// retained validator runs.
func fingerprints(e connectedrun.FlowEvidence) map[string]map[string]string {
	f := map[string]map[string]string{}
	for _, d := range flowDimensions {
		f[d] = map[string]string{}
	}
	doc := e.Plan.Document()
	t := doc.Test
	env := t.Environment
	f[DimensionEnvironment]["environment"] = canonical([]any{env.Project, env.ID, env.Revision, env.Name, env.Classification, env.Endpoint, env.Grants, env.TLS})
	f[DimensionEnvironment]["address-policy"] = canonical(env.AddressPolicyIdentity)
	f[DimensionEnvironment]["isolation"] = canonical(t.Isolation)
	f[DimensionTarget]["target-configuration"] = canonical(env.TargetIdentity)
	f[DimensionTarget]["target-revision"] = canonical(env.TargetRevision)
	for _, s := range t.Servers {
		f[DimensionTarget]["server:"+s.ID] = canonical([]any{s.Base, s.Capability.SHA256})
	}
	f[DimensionBoundary]["lifecycle"] = canonical(t.Boundary)
	for _, q := range e.Result.Qualification {
		f[DimensionBoundary]["dataset:"+q.Phase+"/"+q.Dataset] = canonical(q.Boundary)
	}
	f[DimensionInput]["generation"] = canonical(doc.Generation)
	for _, v := range t.Variables {
		f[DimensionInput]["variable:"+v.ID] = canonical(v)
	}
	for _, s := range t.Steps {
		declaration := []any{s.Endpoint, s.After, s.BusinessKeys}
		if s.V2 != nil {
			declaration = append(declaration, s.V2)
		}
		if s.Interaction != nil {
			declaration = append(declaration, s.Interaction)
		}
		f[DimensionInput]["step:"+s.ID] = canonical(declaration)
	}
	for _, p := range t.Profiles {
		f[DimensionProfile]["profile:"+p.ID] = canonical(p.SHA256)
	}
	f[DimensionEngine]["engine"] = canonical(e.Result.Engine)
	for _, phase := range t.Phases {
		plan := e.Plan.Phase(phase.ID)
		f[DimensionDefinition]["phase:"+phase.ID] = canonical([]any{phase.Steps, phase.After, phase.When, phase.IsolationChanges})
		if plan != nil {
			f[DimensionDefinition]["checks:"+phase.ID] = canonical(plan.Files()["dependencies/"+phase.Checks.SHA256])
		}
		if phase.Wire != nil {
			f[DimensionDefinition]["wire:"+phase.ID] = canonical([]any{e.Plan.Dependency(phase.Wire.Set), phase.Wire.Acknowledgements, phase.Wire.Observed, phase.Wire.Before, phase.Wire.After})
		}
		for _, r := range phase.Responses {
			f[DimensionDefinition]["response:"+phase.ID+"/"+r.ID] = canonical(r)
		}
		for _, v := range phase.Validations {
			f[DimensionDefinition]["validation:"+phase.ID+"/"+v.ID] = canonical([]any{v.Step, v.TimeoutMS, v.MaxOutputBytes})
			f[DimensionProfile]["validation:"+phase.ID+"/"+v.ID] = canonical([]any{v.Profiles, v.Requirements})
		}
		for _, ds := range phase.Datasets {
			key := phase.ID + "/" + ds.ID
			f[DimensionCompletion]["dataset:"+key] = canonical(completion(plan, ds.Completion))
			f[DimensionCollector]["dataset:"+key] = canonical(collector(plan, ds))
		}
		for id, run := range e.Phases[phase.ID].Validators {
			f[DimensionProfile]["validator:"+phase.ID+"/"+id] = canonical(run)
		}
	}
	return f
}

// completion is a dataset's completion declaration with its pinned policy
// and barrier bytes, so a changed policy document is a changed declaration.
func completion(plan *connectedtest.Plan, c connectedtest.Completion) []any {
	out := []any{c.Kind, c.HorizonMS, c.MaxRecords, c.MaxBytes}
	for _, ref := range []*connectedtest.Reference{c.Policy, c.Barrier} {
		if ref != nil && plan != nil {
			out = append(out, plan.Files()["dependencies/"+ref.SHA256])
		}
	}
	return out
}

// collector is what a dataset reads and how: its kind, source, namespace,
// phase and the pinned projection or FHIR observation bytes.
func collector(plan *connectedtest.Plan, ds connectedtest.Dataset) []any {
	out := []any{ds.Kind, ds.Source, ds.Namespace, ds.Phase}
	if ds.Projection != nil && plan != nil {
		out = append(out, plan.Files()["dependencies/"+ds.Projection.SHA256])
	}
	return out
}

func differing(a, b map[string]string) []string {
	out := []string{}
	for _, name := range slices.Sorted(maps.Keys(a)) {
		if b[name] != a[name] {
			out = append(out, name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(b)) {
		if _, ok := a[name]; !ok {
			out = append(out, name)
		}
	}
	return out
}

func phaseOrder(left, right connectedrun.FlowEvidence) []string {
	order := []string{}
	for _, e := range []connectedrun.FlowEvidence{left, right} {
		for _, p := range e.Result.Phases {
			if !slices.Contains(order, p.ID) {
				order = append(order, p.ID)
			}
		}
	}
	return order
}

// checkDefinition is the digest of one check's own declaration, or "" when
// the lifecycle does not declare it.
func checkDefinition(e connectedrun.FlowEvidence, phaseID, check string) string {
	var phase *connectedtest.FlowPhase
	for _, p := range e.Plan.Document().Test.Phases {
		if p.ID == phaseID {
			phase = &p
		}
	}
	if phase == nil {
		return ""
	}
	kind, id, _ := strings.Cut(check, ":")
	switch kind {
	case "typed":
		if plan := e.Plan.Phase(phaseID); plan != nil {
			if set, err := assertion.DecodeDatasets(plan.Files()["dependencies/"+phase.Checks.SHA256]); err == nil {
				for _, a := range set.Document().Assertions {
					if a.ID == id {
						return canonical(a)
					}
				}
			}
		}
	case "wire":
		if phase.Wire != nil {
			if set, err := assertion.Decode(e.Plan.Dependency(phase.Wire.Set)); err == nil {
				for _, a := range set.Assertions {
					if a.ID == id {
						return canonical([]any{a, phase.Wire.Acknowledgements, phase.Wire.Observed})
					}
				}
			}
		}
	case "response":
		for _, r := range phase.Responses {
			if r.ID == id {
				return canonical(r)
			}
		}
	case "validation":
		for _, v := range phase.Validations {
			if v.ID == id {
				return canonical(v)
			}
		}
	}
	return ""
}

func compareFlowChecks(left, right connectedrun.FlowEvidence) []FlowCheckComparison {
	rows := []FlowCheckComparison{}
	outcomes := func(e connectedrun.FlowEvidence, phase string) ([]string, map[string]string) {
		order, by := []string{}, map[string]string{}
		for _, p := range e.Result.Phases {
			if p.ID == phase {
				for _, check := range p.Checks {
					order = append(order, check.ID)
					by[check.ID] = string(check.Outcome)
				}
			}
		}
		return order, by
	}
	for _, phase := range phaseOrder(left, right) {
		leftOrder, leftOutcomes := outcomes(left, phase)
		rightOrder, rightOutcomes := outcomes(right, phase)
		ids := slices.Clone(leftOrder)
		for _, id := range rightOrder {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		for _, id := range ids {
			row := FlowCheckComparison{Phase: phase, Check: id, Baseline: "excluded", Current: "excluded", Definition: "unchanged", Behavior: "not_compared"}
			l, inLeft := leftOutcomes[id]
			r, inRight := rightOutcomes[id]
			if inLeft {
				row.Baseline = l
			}
			if inRight {
				row.Current = r
			}
			switch {
			case !inLeft:
				row.Definition = "added"
			case !inRight:
				row.Definition = "removed"
			case checkDefinition(left, phase, id) != checkDefinition(right, phase, id):
				row.Definition = "changed"
			}
			decided := func(o string) bool {
				return o == string(assertion.OutcomePassed) || o == string(assertion.OutcomeFailed)
			}
			if row.Definition == "unchanged" && decided(l) && decided(r) {
				row.Behavior = "unchanged"
				if l != r {
					row.Behavior = "changed"
				}
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// identityColumns names a FHIR observation's identity and reference columns,
// whose values are server-assigned logical IDs.
func identityColumns(e connectedrun.FlowEvidence, phase, datasetID string) map[string]bool {
	out := map[string]bool{}
	plan := e.Plan.Phase(phase)
	if plan == nil {
		return out
	}
	o, _, _, err := plan.ObservationURL(datasetID)
	if err != nil {
		return out
	}
	for _, c := range o.Columns {
		if c.Value.Kind == "identity" || c.Value.Kind == "reference" {
			out[c.Name] = true
		}
	}
	return out
}

func compareFlowRecords(left, right connectedrun.FlowEvidence, phase string) []FlowRecordComparison {
	datasets := []string{}
	for _, e := range []connectedrun.FlowEvidence{left, right} {
		for _, p := range e.Plan.Document().Test.Phases {
			if p.ID != phase {
				continue
			}
			for _, ds := range p.Datasets {
				if !slices.Contains(datasets, ds.ID) {
					datasets = append(datasets, ds.ID)
				}
			}
		}
	}
	out := []FlowRecordComparison{}
	for _, id := range datasets {
		row := FlowRecordComparison{Phase: phase, Dataset: id, State: "not_compared", Keys: []FlowKeyComparison{}}
		l, lok := left.Phases[phase].Tables[id]
		r, rok := right.Phases[phase].Tables[id]
		switch {
		case !lok || !rok:
			row.Reason = "the observation was not retained complete in both lifecycles; a missing observation is not an empty one"
		case !l.Usable || !r.Usable:
			row.Reason = "an observation is unusable; it is not compared as an empty or partial set"
		case canonical(l.Columns) != canonical(r.Columns):
			row.Reason = "the declared columns differ, so records cannot be matched through one key contract"
		default:
			row.State = "compared"
			row.Keys = compareKeyed(l, r, identityColumns(right, phase, id))
		}
		out = append(out, row)
	}
	return out
}

// compareKeyed groups both tables' rows by their declared key columns and
// compares each key's records as multisets, column by column.
func compareKeyed(l, r assertion.Table, identities map[string]bool) []FlowKeyComparison {
	keyed := func(t assertion.Table) (map[string][]dataset.Row, []string) {
		groups, order := map[string][]dataset.Row{}, []string{}
		for _, row := range t.Rows {
			key := []dataset.Value{}
			for i, c := range t.Columns {
				if c.Key && i < len(row.Values) {
					key = append(key, row.Values[i])
				}
			}
			k := canonical(key)
			if _, seen := groups[k]; !seen {
				order = append(order, k)
			}
			groups[k] = append(groups[k], row)
		}
		return groups, order
	}
	lg, lo := keyed(l)
	rg, ro := keyed(r)
	keys := slices.Clone(lo)
	for _, k := range ro {
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	out := []FlowKeyComparison{}
	for _, k := range keys {
		b, c := lg[k], rg[k]
		row := FlowKeyComparison{Key: k, Baseline: len(b), Current: len(c), State: "unchanged", Values: []string{}, Identities: []string{}}
		switch {
		case len(b) == 0:
			row.State = "added"
		case len(c) == 0:
			row.State = "removed"
		case len(b) != len(c):
			row.State = "multiplicity-changed"
		default:
			for i, column := range l.Columns {
				if column.Key || multiset(b, i) == multiset(c, i) {
					continue
				}
				if identities[column.Name] {
					row.Identities = append(row.Identities, column.Name)
				} else {
					row.Values = append(row.Values, column.Name)
				}
			}
			if len(row.Values) > 0 {
				row.State = "values-changed"
			} else if len(row.Identities) > 0 {
				row.State = "identities-changed"
			}
		}
		out = append(out, row)
	}
	return out
}

// multiset is the digest of one column's values across rows, order-free and
// counting repeats, so equal multiplicities of equal values compare equal.
func multiset(rows []dataset.Row, column int) string {
	values := []string{}
	for _, row := range rows {
		if column < len(row.Values) {
			values = append(values, canonical(row.Values[column]))
		} else {
			values = append(values, "absent")
		}
	}
	slices.Sort(values)
	return canonical(values)
}
