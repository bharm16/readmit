package desktop

import (
	"context"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/dataset"
)

// ConnectedIndividualEvidence adapts verified retained lifecycle evidence for
// the existing run page. Original values remain withheld until Reveal.
type ConnectedIndividualEvidence struct {
	actual       connectedrun.FlowResult
	Lifecycle    ConnectedLifecycleView         `json:"lifecycle"`
	Identity     string                         `json:"identity"`
	Checks       []ConnectedIndividualCheck     `json:"checks"`
	Steps        []ConnectedIndividualStep      `json:"steps"`
	Observations []ConnectedObservationEvidence `json:"observations"`
}
type ConnectedIndividualCheck struct {
	Phase         string            `json:"phase"`
	ID            string            `json:"id"`
	Kind          string            `json:"kind"`
	Operator      string            `json:"operator"`
	Outcome       assertion.Outcome `json:"outcome"`
	ExpectedCount *int              `json:"expected_count,omitzero"`
	ObservedCount *int              `json:"observed_count,omitzero"`
	Expected      *dataset.Value    `json:"expected,omitzero"`
	Observed      *dataset.Value    `json:"observed,omitzero"`
	Dataset       string            `json:"dataset,omitzero"`
	Hidden        bool              `json:"hidden"`
	Unavailable   string            `json:"unavailable,omitzero"`
}
type ConnectedIndividualStep struct {
	Phase      string `json:"phase"`
	Step       string `json:"step"`
	Protocol   string `json:"protocol"`
	Outcome    string `json:"outcome"`
	Uncertain  bool   `json:"uncertain"`
	ACK        string `json:"ack,omitzero"`
	HTTPStatus int    `json:"http_status,omitzero"`
}
type ConnectedObservationEvidence struct {
	Reason          string `json:"reason,omitzero"`
	CaptureIdentity string `json:"capture_identity,omitzero"`
	Phase           string `json:"phase"`
	Dataset         string `json:"dataset"`
	Identity        string `json:"identity"`
	Records         int    `json:"records"`
	Boundary        string `json:"boundary"`
	Available       bool   `json:"available"`
}

func displayDatasetValue(value dataset.Value, reveal bool) *dataset.Value {
	out := value
	out.Items = nil
	if !reveal {
		out.Text = ""
	}
	for _, child := range value.Items {
		out.Items = append(out.Items, *displayDatasetValue(child, reveal))
	}
	return &out
}
func readConnectedIndividualDetail(ctx context.Context, path string, reveal bool) (*ConnectedIndividualEvidence, error) {
	proof, err := connectedrun.OpenFlowEvidence(ctx, path)
	if err != nil {
		recovered, err := connectedrun.InspectFlow(ctx, path)
		if err != nil {
			return nil, err
		}
		return &ConnectedIndividualEvidence{actual: recovered, Lifecycle: connectedLifecycleView("", recovered), Checks: []ConnectedIndividualCheck{}, Steps: []ConnectedIndividualStep{}, Observations: []ConnectedObservationEvidence{}}, nil
	}
	view := &ConnectedIndividualEvidence{actual: proof.Result, Identity: proof.Identity, Lifecycle: connectedLifecycleView("", proof.Result), Checks: []ConnectedIndividualCheck{}, Steps: []ConnectedIndividualStep{}, Observations: []ConnectedObservationEvidence{}}
	for i, phase := range proof.Plan.Document().Test.Phases {
		result := proof.Result.Phases[i]
		observed := proof.Phases[phase.ID]
		outcomes := map[string]assertion.Outcome{}
		for _, check := range result.Checks {
			outcomes[check.ID] = check.Outcome
		}
		for _, attempt := range result.Steps {
			step := ConnectedIndividualStep{Phase: phase.ID, Step: attempt.Step, Protocol: attempt.Kind, Outcome: attempt.Outcome, Uncertain: attempt.Uncertain}
			index := 0
			for j, id := range phase.Steps {
				if id == attempt.Step {
					index = j
					break
				}
			}
			if observed.Transport != nil && index < len(observed.Transport.Events) {
				step.ACK = string(observed.Transport.Events[index].ACK.Code)
			}
			step.HTTPStatus = observed.HTTPStatus[attempt.Step]
			view.Steps = append(view.Steps, step)
		}
		for _, declaration := range phase.Datasets {
			table, ok := observed.Tables[declaration.ID]
			view.Observations = append(view.Observations, ConnectedObservationEvidence{Phase: phase.ID, Dataset: declaration.ID, Reason: observed.Intervals[declaration.ID].Reason, CaptureIdentity: func() string {
				if capture := observed.Captures[declaration.ID]; capture != nil {
					return capture.Identity
				}
				return ""
			}(), Identity: table.Identity, Records: len(table.Rows), Boundary: observed.Boundaries[declaration.ID], Available: ok && table.Usable && (observed.Intervals[declaration.ID].Schema == "" || observed.Intervals[declaration.ID].Sufficient())})
		}
		typedIDs := map[string]bool{}
		if phase.Checks.SHA256 != "" {
			set, err := assertion.DecodeDatasets(proof.Plan.Phase(phase.ID).Files()["dependencies/"+phase.Checks.SHA256])
			if err != nil {
				return nil, err
			}
			evaluated, err := set.EvaluateTables(ctx, proof.Result.Instance, observed.Tables)
			actual := map[string]assertion.DatasetResult{}
			if err == nil {
				for _, check := range evaluated.Results {
					actual[check.ID] = check
				}
			}
			for _, check := range set.Document().Assertions {
				id := "typed:" + check.ID
				typedIDs[id] = true
				kind := "application"
				if proof.Plan.Document().Test.Boundary == EngineOutputBoundary {
					kind = "received-hl7"
				}
				row := ConnectedIndividualCheck{Phase: phase.ID, ID: id, Kind: kind, Operator: check.Operator, Outcome: outcomes[id], ExpectedCount: check.Count, Dataset: check.Subject.Dataset, Hidden: !reveal}
				if check.Expected != nil {
					row.Expected = displayDatasetValue(*check.Expected, reveal)
				}
				if measured, ok := actual[check.ID]; ok && (measured.Outcome == assertion.OutcomePassed || measured.Outcome == assertion.OutcomeFailed) {
					count := measured.Count
					row.ObservedCount = &count
					if len(measured.Rows) == 1 {
						table := observed.Tables[check.Subject.Dataset]
						column := -1
						for n, c := range table.Columns {
							if c.Name == check.Column {
								column = n
								break
							}
						}
						for _, record := range table.Rows {
							if column >= 0 && column < len(record.Values) && strings.HasSuffix(measured.Rows[0], "/"+record.ID) {
								row.Observed = displayDatasetValue(record.Values[column], reveal)
								break
							}
						}
					}
				} else {
					row.Unavailable = "the declared observation or check did not complete"
				}
				view.Checks = append(view.Checks, row)
			}
		}
		for _, check := range result.Checks {
			if typedIDs[check.ID] {
				continue
			}
			row := ConnectedIndividualCheck{Phase: phase.ID, ID: check.ID, Kind: "transport", Operator: "transport", Outcome: check.Outcome}
			text := func(v string) *dataset.Value { return &dataset.Value{State: "present", Type: "text", Text: v} }
			for _, response := range phase.Responses {
				if check.ID == "response:"+response.ID {
					row.Expected = text(response.Outcome)
					for _, step := range observed.Steps {
						if step.Step == response.Step && step.Result != "" {
							row.Observed = text(step.Outcome + " · HTTP " + strconv.Itoa(observed.HTTPStatus[step.Step]))
						}
					}
				}
			}
			if phase.Wire != nil {
				set, err := assertion.Decode(proof.Plan.Dependency(phase.Wire.Set))
				if err != nil {
					return nil, err
				}
				for _, wire := range set.Assertions {
					if check.ID == "wire:"+wire.ID && wire.Subject.Field != nil && wire.Subject.Field.Selector == "MSA-1" && wire.Expected.Field != nil && wire.Expected.Field.Text != nil {
						row.Expected = text(*wire.Expected.Field.Text)
						if observed.Transport != nil {
							for _, event := range observed.Transport.Events {
								if event.SourceOccurrence == wire.Subject.Field.Message && event.ACK.Code != "" {
									row.Observed = text(string(event.ACK.Code))
									break
								}
							}
						}
					}
				}
			}
			if strings.HasPrefix(check.ID, "validation:") {
				row.Kind, row.Operator = "profile", "profile"
				for _, validation := range phase.Validations {
					if check.ID == "validation:"+validation.ID {
						row.Expected = text("conforms to the pinned profile requirements")
						if actual, ok := observed.Validators[validation.ID]; ok {
							value := actual.RuntimeState
							if value == "" {
								value = string(check.Outcome)
							}
							row.Observed = text(value + " · " + actual.Capability)
						}
					}
				}
			}
			if row.Observed == nil {
				row.Unavailable = "the requested evidence value was not recorded; inspect its retained outcome"
			}
			view.Checks = append(view.Checks, row)
		}
	}
	return view, nil
}

// ConnectedObservationRequest reads one retained phase table through its run
// identity. It cannot choose a live source or initiate another acquisition.
type ConnectedObservationRequest struct {
	Family         RetainedObservationFamily `json:"family,omitzero"`
	Identity       string                    `json:"identity,omitzero"`
	SourceIdentity string                    `json:"source_identity,omitzero"`
	Job            string                    `json:"job,omitzero"`
	Context        RequestContext            `json:"context"`
	Run            ItemRef                   `json:"run"`
	Phase          string                    `json:"phase"`
	Dataset        string                    `json:"dataset"`
	Offset         int                       `json:"offset"`
	Limit          int                       `json:"limit"`
	Reveal         bool                      `json:"reveal"`
}
type ConnectedObservationColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type ConnectedObservationRow struct {
	Occurrence string          `json:"occurrence,omitzero"`
	ID         string          `json:"id"`
	Values     []dataset.Value `json:"values"`
}
type ConnectedObservationResult struct {
	Context   RequestContext               `json:"context"`
	State     State                        `json:"state"`
	Reason    string                       `json:"reason,omitzero"`
	Available bool                         `json:"available"`
	Identity  string                       `json:"identity,omitzero"`
	Phase     string                       `json:"phase"`
	Dataset   string                       `json:"dataset"`
	Columns   []ConnectedObservationColumn `json:"columns"`
	Rows      []ConnectedObservationRow    `json:"rows"`
	Total     int                          `json:"total"`
	Offset    int                          `json:"offset"`
	Hidden    bool                         `json:"hidden"`
}

func (r *ConnectedObservationResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}
func (a *App) ReadConnectedObservation(request ConnectedObservationRequest) ConnectedObservationResult {
	answer := runRead(a, false, func(ctx context.Context) ConnectedObservationResult {
		result := ConnectedObservationResult{Context: request.Context, Phase: request.Phase, Dataset: request.Dataset, Columns: []ConnectedObservationColumn{}, Rows: []ConnectedObservationRow{}, Hidden: !request.Reveal, Offset: request.Offset}
		if request.Offset < 0 || request.Limit < 0 || request.Limit > 200 {
			result.refuse(Failed, "select a bounded retained observation page")
			return result
		}
		loaded, _, path, declined := a.runOf(ctx, RunRequest{Context: request.Context, Run: request.Run})
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if request.Family == LegacyLedgerObservation {
			return readLegacyRetainedObservation(ctx, path, request, result)
		}
		if request.Family != "" && request.Family != ConnectedLifecycleObservation || request.Job != "" || request.SourceIdentity != "" {
			result.refuse(Failed, "choose a supported retained observation family and scope")
			return result
		}
		proof, err := connectedrun.OpenFlowEvidence(ctx, path)
		if err != nil {
			result.refuse(Failed, "the retained connected lifecycle cannot be verified")
			return result
		}
		phase, exists := proof.Phases[request.Phase]
		table, available := phase.Tables[request.Dataset]
		if !exists || !available || !table.Usable || (phase.Intervals[request.Dataset].Schema != "" && !phase.Intervals[request.Dataset].Sufficient()) {
			result.State = Completed
			result.Reason = "the declared observation did not complete; no absence is established"
			if reason := phase.Intervals[request.Dataset].Reason; reason != "" {
				result.Reason = reason + "; no absence is established"
			}
			return result
		}
		if request.Identity != "" && request.Identity != table.Identity {
			result.refuse(Failed, "the retained dataset identity changed; reopen this run")
			return result
		}
		result.State, result.Available, result.Identity, result.Total = Completed, true, table.Identity, len(table.Rows)
		for _, column := range table.Columns {
			result.Columns = append(result.Columns, ConnectedObservationColumn{Name: column.Name, Type: column.Type})
		}
		limit := request.Limit
		if limit == 0 {
			limit = 50
		}
		start := min(request.Offset, len(table.Rows))
		end := start + min(limit, len(table.Rows)-start)
		for _, row := range table.Rows[start:end] {
			shown := ConnectedObservationRow{Occurrence: row.Provenance.SourceRecord, ID: row.ID, Values: []dataset.Value{}}
			for _, value := range row.Values {
				shown.Values = append(shown.Values, *displayDatasetValue(value, request.Reveal))
			}
			result.Rows = append(result.Rows, shown)
		}
		return result
	})
	answer.Context = request.Context
	return answer
}

// ConnectedCaptureRequest selects only the original captured bytes supporting
// one verified phase dataset. It cannot select a receiver or initiate capture.
type ConnectedCaptureRequest struct {
	Context  RequestContext `json:"context"`
	Run      ItemRef        `json:"run"`
	Phase    string         `json:"phase"`
	Dataset  string         `json:"dataset"`
	Identity string         `json:"identity"`
}

func (a *App) OpenConnectedCapture(request ConnectedCaptureRequest) RetainedCaptureResult {
	answer := runRead(a, false, func(ctx context.Context) RetainedCaptureResult {
		result := RetainedCaptureResult{Context: request.Context, Session: request.Run.ID}
		loaded, _, path, declined := a.runOf(ctx, RunRequest{Context: request.Context, Run: request.Run})
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		proof, err := connectedrun.OpenFlowEvidence(ctx, path)
		if err != nil {
			result.refuse(Failed, "the retained lifecycle cannot be verified")
			return result
		}
		capture := proof.Phases[request.Phase].Captures[request.Dataset]
		if capture == nil || request.Identity == "" || capture.Identity != request.Identity {
			result.refuse(Failed, "choose the exact supporting capture of this verified dataset")
			return result
		}
		for _, part := range []string{"phases", request.Phase, "intervals", request.Dataset, "capture"} {
			next, err := artifactpath.Child(path, part)
			if err != nil {
				result.refuse(Failed, "the supporting capture is unavailable")
				return result
			}
			path = next
		}
		opened := a.openCase(path, "case")
		if opened.State != Completed || opened.Case == nil || opened.Case.Identity != request.Identity {
			result.refuse(Failed, "the exact retained capture changed or is unavailable")
			return result
		}
		result.State, result.Workspace, result.Case = Completed, path, opened.Case
		return result
	})
	answer.Context = request.Context
	return answer
}
