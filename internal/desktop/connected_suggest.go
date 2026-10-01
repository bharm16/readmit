package desktop

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/suite"
)

// Suggest checks for a connected test proposes typed checks from what one
// retained run of exactly this test's definition observed: a record count
// for each business key its observations read after a phase, and the value
// of each field of that record. Every proposal reaches the editor undecided
// and none is in the draft until a person accepts it; a value the run read
// from a server-assigned identity — a resource identity, a reference, or a
// value a response bound — is never proposed as an expected value.

// ConnectedProposal is one check proposed for one phase from a retained run,
// or, with Reason, a field the run read that cannot be an expected value.
type ConnectedProposal struct {
	ID     string         `json:"id"`
	Phase  string         `json:"phase"`
	Check  ConnectedCheck `json:"check"`
	Reason string         `json:"reason,omitzero"`
}

// ConnectedSuggestRequest names a saved connected test version and, once
// chosen, the retained run its proposals are read from.
type ConnectedSuggestRequest struct {
	Context RequestContext `json:"context"`
	Test    ItemRef        `json:"test"`
	Run     *ItemRef       `json:"run,omitzero"`
}

// ConnectedSuggestRun is one retained run that executed exactly this test's
// definition and completed: the run, its name and when it started.
type ConnectedSuggestRun struct {
	Run       ItemRef `json:"run"`
	Name      string  `json:"name"`
	StartedAt *string `json:"started_at"`
}

// ConnectedSuggestResult is the eligible runs, and the chosen run's proposals.
type ConnectedSuggestResult struct {
	State     State                 `json:"state"`
	Reason    string                `json:"reason,omitzero"`
	Context   RequestContext        `json:"context"`
	Runs      []ConnectedSuggestRun `json:"runs"`
	Proposals []ConnectedProposal   `json:"proposals"`
}

func (r *ConnectedSuggestResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// eligibleFlow is one completed job of a retained suite run whose lifecycle
// is this test's definition.
type eligibleFlow struct {
	run  ConnectedSuggestRun
	path string
}

// SuggestConnectedChecks lists the retained runs of a saved connected test's
// definition and proposes checks from the chosen one. It is a read: nothing
// runs, nothing is sent and nothing is recorded.
func (a *App) SuggestConnectedChecks(request ConnectedSuggestRequest) ConnectedSuggestResult {
	return runRead(a, false, func(ctx context.Context) ConnectedSuggestResult {
		result := ConnectedSuggestResult{Context: request.Context, Runs: []ConnectedSuggestRun{}, Proposals: []ConnectedProposal{}}
		if request.Test.Kind != TestItem {
			result.refuse(Failed, "checks are suggested for a saved connected test")
			return result
		}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Test, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		record := loaded.document.Items[loaded.document.Find(item.Ref.ID)]
		saved, err := loaded.connectedTestOf(record, request.Test.Revision)
		if err != nil {
			result.refuse(Failed, "checks are suggested from runs of a saved connected test")
			return result
		}
		links := TestLinks{}
		if saved.links != nil {
			links = *saved.links
		}
		revision := cmp.Or(request.Test.Revision, record.RevisionLabel())
		definition, err := loaded.reviewConnected(saved.draft, record.ID, revision, links)
		if err != nil {
			result.refuse(Failed, "this test cannot be compiled: "+err.Error())
			return result
		}
		flows := loaded.eligibleFlows(ctx, definition)
		for _, flow := range flows {
			result.Runs = append(result.Runs, flow.run)
		}
		if request.Run == nil {
			result.State = Completed
			return result
		}
		at := slices.IndexFunc(flows, func(f eligibleFlow) bool { return f.run.Run.ID == request.Run.ID })
		if at < 0 {
			result.refuse(Failed, "that run did not complete this test's definition")
			return result
		}
		evidence, err := connectedrun.OpenFlowEvidence(ctx, flows[at].path)
		if err != nil {
			result.refuse(Failed, "the run's retained evidence does not verify")
			return result
		}
		result.Proposals = loaded.connectedProposals(saved.draft, evidence)
		result.State = Completed
		return result
	})
}

// eligibleFlows are the completed jobs of the project's retained suite runs
// whose lifecycle is exactly this definition, the most recently started first.
func (c *loadedCatalog) eligibleFlows(ctx context.Context, definition expectation.ConnectedReview) []eligibleFlow {
	flows := []eligibleFlow{}
	for _, item := range c.document.Items {
		if item.Kind != string(RunItem) || item.Entry == "" || c.removed(item) {
			continue
		}
		path := filepath.Join(c.root, item.Entry)
		execution, err := suite.OpenConnectedExecution(ctx, path)
		if err != nil {
			continue
		}
		for _, job := range execution.Report.Jobs {
			if job.Flow == nil || job.Flow.State != "complete" || job.Flow.Setup != "ready" {
				continue
			}
			flow := filepath.Join(path, "runs", job.ID)
			review, err := expectation.ReviewConnected(filepath.Join(flow, "plan"))
			if err != nil || review != definition {
				continue
			}
			run := ConnectedSuggestRun{Run: ItemRef{Kind: RunItem, ID: item.ID}, Name: cmp.Or(c.read(item).Name, item.Entry)}
			if !job.Flow.StartedAt.IsZero() {
				run.StartedAt = stampedTime(job.Flow.StartedAt)
			}
			flows = append(flows, eligibleFlow{run: run, path: flow})
		}
	}
	slices.SortStableFunc(flows, func(x, y eligibleFlow) int {
		return cmp.Compare(stampOf(y.run.StartedAt), stampOf(x.run.StartedAt))
	})
	return flows
}

// connectedProposals reads every phase's after observations and proposes, for
// each business key, the record count and each field's value. Identities a
// server assigned are offered only as reasons, never as values.
func (c *loadedCatalog) connectedProposals(d ConnectedTestDraft, evidence connectedrun.FlowEvidence) []ConnectedProposal {
	proposals := []ConnectedProposal{}
	next := func() string { return "suggested-" + strconv.Itoa(len(proposals)+1) }
	for _, phase := range d.Phases {
		pe, held := evidence.Phases[phase.ID]
		if !held {
			continue
		}
		runtime := []string{}
		for _, v := range pe.Bound {
			runtime = append(runtime, v)
		}
		for _, v := range pe.Inputs {
			runtime = append(runtime, v)
		}
		for _, observed := range phase.Observations {
			if observed.When != "after" {
				continue
			}
			table, read := pe.Tables[observed.Dataset]
			if !read || !table.Usable {
				continue
			}
			identities := map[string]bool{}
			if pinned, err := c.connectedObservation(observed.Observation.ID, observed.Observation.Revision); err == nil && pinned.fhir != nil {
				for _, column := range pinned.fhir.Columns {
					identities[column.Name] = column.Value.Kind != "field"
				}
			}
			keys := []int{}
			for i, column := range table.Columns {
				if column.Key {
					keys = append(keys, i)
				}
			}
			groups := map[string][]dataset.Row{}
			order := []string{}
			filters := map[string][]assertion.RowFilter{}
			for _, row := range table.Rows {
				where := []assertion.RowFilter{}
				usable := len(keys) > 0
				for _, k := range keys {
					value := row.Values[k]
					if value.State != "present" || !dataset.ValidExpected(value) {
						usable = false
					}
					where = append(where, assertion.RowFilter{Column: table.Columns[k].Name, Equals: value})
				}
				if !usable {
					continue
				}
				raw, _ := json.Marshal(where, json.Deterministic(true))
				if _, seen := groups[string(raw)]; !seen {
					order = append(order, string(raw))
					filters[string(raw)] = where
				}
				groups[string(raw)] = append(groups[string(raw)], row)
			}
			for _, group := range order {
				rows, where := groups[group], filters[group]
				label := []string{}
				for _, filter := range where {
					label = append(label, filter.Equals.Text)
				}
				name := strings.Join(label, " · ")
				count := len(rows)
				proposals = append(proposals, ConnectedProposal{ID: next(), Phase: phase.ID, Check: ConnectedCheck{Name: "Records of " + name, Check: assertion.DatasetAssertion{ID: "", Operator: "row-count", Subject: assertion.RowSelection{Dataset: observed.Dataset, Where: where}, Count: &count}}})
				if len(rows) != 1 {
					continue
				}
				for i, column := range table.Columns {
					if column.Key {
						continue
					}
					value := rows[0].Values[i]
					proposal := ConnectedProposal{ID: next(), Phase: phase.ID, Check: ConnectedCheck{Name: column.Name + " of " + name, Check: assertion.DatasetAssertion{Operator: "value-equals", Subject: assertion.RowSelection{Dataset: observed.Dataset, Where: where}, Column: column.Name}}}
					switch {
					case identities[column.Name] || value.State == "present" && slices.ContainsFunc(runtime, func(id string) bool { return id != "" && strings.Contains(value.Text, id) }):
						proposal.Reason = "a server-assigned identity is never an expected value"
					case !expectable(&value):
						proposal.Reason = "the run read no value this field can be expected to hold"
					default:
						expected := value
						proposal.Check.Check.Expected = &expected
						if column.Type == "decimal" {
							proposal.Check.Check.Operator = "decimal-equals"
						} else if column.Type == "date" || column.Type == "datetime" {
							proposal.Check.Check.Operator = "instant-equals"
						}
					}
					proposals = append(proposals, proposal)
				}
			}
		}
	}
	return proposals
}

// expectable is whether a value read can be written as an expected value of
// the same type, in the canonical form the typed reader projects; it sets
// that form. Only its representation changes, never what it states.
func expectable(v *dataset.Value) bool {
	if dataset.ValidExpected(*v) {
		return true
	}
	candidate := *v
	if candidate.Timezone == "Z" {
		candidate.Timezone = "+00:00"
	}
	if dataset.ValidExpected(candidate) {
		*v = candidate
		return true
	}
	candidate.Precision, candidate.Timezone = "", ""
	if dataset.ValidExpected(candidate) {
		*v = candidate
		return true
	}
	return false
}
