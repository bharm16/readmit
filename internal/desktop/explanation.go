package desktop

import (
	"context"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/runexplain"
)

// The run-explanation panel. `readmit explain` re-decides an assertion set
// against the evidence one run retained; this is that explanation for a
// retained run of the open workspace. It is assembled by
// runexplain.Explain, the operation the command renders: the run bundle is
// opened through the verifying replay reader, the set through the assertion
// reader, an observation through its own two readers, and the set is
// evaluated again. It is a reading, not an operation on evidence: it needs no
// admission, sends nothing, writes nothing and keeps nothing.

// explanationOperation is the name an explanation holds the slot under, which
// the panel's cancel names.
const explanationOperation = "run-explanation"

// RunExplanationRequest names what one explanation is assembled from, each an
// entry of the open workspace. Run is a retained execution the run history
// lists, one job inside a suite execution's runs, or a run bundle itself.
// Assertions is the readmit-assertion-set/v1 document to re-decide. A
// completion record and its observation source are supplied together, and
// only for a scope the set asks about. Reveal is the deliberate local reveal
// of expected and observed values and of observed record keys.
type RunExplanationRequest struct {
	Workspace    string `json:"workspace"`
	Run          string `json:"run"`
	Assertions   string `json:"assertions"`
	Before       string `json:"before,omitzero"`
	BeforeSource string `json:"before_source,omitzero"`
	After        string `json:"after,omitzero"`
	AfterSource  string `json:"after_source,omitzero"`
	Reveal       bool   `json:"reveal"`
}

// RunExplanationResult carries one state. A refusal is Failed with the
// sentence `readmit explain` prints for the same evidence; a completed
// explanation still reports an execution error, because what the evaluator
// could not read is itself something to explain.
type RunExplanationResult struct {
	State       State           `json:"state"`
	Reason      string          `json:"reason,omitzero"`
	Explanation *RunExplanation `json:"explanation,omitzero"`
}

func (r *RunExplanationResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// RunExplanation is what one run's evidence decided, and what deciding it
// read. Verdict and ErrorClass are exclusive: an execution error produces no
// verdict, and every assertion is then left unevaluated. Bundle is the run
// bundle the named run retained, relative to the workspace, which is the path
// `readmit explain` is given for the same explanation; every payload and
// capture is named relative to the workspace too.
type RunExplanation struct {
	Run                  string                 `json:"run"`
	Bundle               string                 `json:"bundle"`
	Verdict              string                 `json:"verdict,omitzero"`
	ErrorClass           string                 `json:"error_class,omitzero"`
	ErrorAssertion       string                 `json:"error_assertion,omitzero"`
	Declared             int                    `json:"declared"`
	Passed               int                    `json:"passed"`
	Failed               int                    `json:"failed"`
	Undecided            int                    `json:"undecided"`
	Skipped              int                    `json:"skipped"`
	SetName              string                 `json:"set_name"`
	SetSchema            string                 `json:"set_schema"`
	SetIdentity          string                 `json:"set_identity"`
	RunSchema            string                 `json:"run_schema"`
	RunState             string                 `json:"run_state"`
	RunIdentity          string                 `json:"run_identity"`
	SourceIdentity       string                 `json:"source_identity"`
	ContainsSourceValues bool                   `json:"contains_source_values"`
	ExportPolicy         string                 `json:"export_policy"`
	Target               string                 `json:"target"`
	Transport            string                 `json:"transport"`
	TargetIdentity       string                 `json:"target_identity"`
	StartedAt            string                 `json:"started_at"`
	CompletedAt          string                 `json:"completed_at"`
	Elapsed              string                 `json:"elapsed"`
	Messages             []ExplainedMessage     `json:"messages"`
	Observations         []ExplainedObservation `json:"observations"`
	Assertions           []ExplainedAssertion   `json:"assertions"`
	Revealed             bool                   `json:"revealed"`
}

// ExplainedMessage is one message of the run as the run recorded it, and
// whether each of its payloads is evidence an assertion may read.
type ExplainedMessage struct {
	Source         string `json:"source"`
	Outbound       string `json:"outbound"`
	Outcome        string `json:"outcome"`
	Delivery       string `json:"delivery"`
	ACKCode        string `json:"ack_code,omitzero"`
	ACKCorrelation string `json:"ack_correlation,omitzero"`
	Elapsed        string `json:"elapsed"`
	Input          string `json:"input"`
	Observed       string `json:"observed"`
}

// ExplainedObservation is one observed scope the records were derived again
// for: what its window settled on, what binds it to this run, and the capture
// the records were read again from. Keys stay hidden until revealed.
type ExplainedObservation struct {
	Scope          string `json:"scope"`
	Status         string `json:"status"`
	Schema         string `json:"schema"`
	SourceSchema   string `json:"source_schema"`
	Window         string `json:"window"`
	SourceKind     string `json:"source_kind"`
	SourceIdentity string `json:"source_identity"`
	SourceScope    string `json:"source_scope"`
	Records        int    `json:"records"`
	Correlations   string `json:"correlations"`
	Capture        string `json:"capture"`
	Keys           string `json:"keys"`
}

// ExplainedAssertion is one assertion and what deciding it read, in the words
// the command prints. Outcome is empty for an assertion no evaluation reached.
type ExplainedAssertion struct {
	ID        string   `json:"id"`
	Operator  string   `json:"operator"`
	Outcome   string   `json:"outcome,omitzero"`
	Reads     string   `json:"reads"`
	Condition string   `json:"condition,omitzero"`
	Expected  string   `json:"expected"`
	Observed  string   `json:"observed"`
	Evidence  []string `json:"evidence"`
}

// ExplainRun re-decides one assertion set against the evidence one retained
// run of the open workspace kept, exactly as `readmit explain` does given the
// run bundle that run retained. It can be cancelled; evaluation retains
// nothing, so explaining again decides exactly what the cancelled one would
// have.
func (a *App) ExplainRun(request RunExplanationRequest) RunExplanationResult {
	return runNamed[RunExplanationResult, *RunExplanationResult](a, explanationOperation, true, false, func(ctx context.Context) RunExplanationResult {
		return explainRun(ctx, request)
	})
}

func explainRun(ctx context.Context, request RunExplanationRequest) RunExplanationResult {
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return RunExplanationResult{State: declined.state, Reason: declined.reason}
	}
	if request.Run == "" || request.Assertions == "" {
		return RunExplanationResult{State: Empty, Reason: "an explanation needs one retained run and one assertion set"}
	}
	bundle, bundleEntry, declined := explainedBundle(root, request.Run)
	if bundle == "" {
		return RunExplanationResult{State: declined.state, Reason: declined.reason}
	}
	input := runexplain.Input{Run: bundle}
	documents := []struct {
		entry  string
		into   *string
		reason string
	}{
		{request.Assertions, &input.Assertions, "an assertion set is one regular file of the open workspace, never a symbolic link"},
		{request.Before, &input.Before.Completion, "a completion record is one regular file of the open workspace, never a symbolic link"},
		{request.BeforeSource, &input.Before.Source, "an observation source is one regular file of the open workspace, never a symbolic link"},
		{request.After, &input.After.Completion, "a completion record is one regular file of the open workspace, never a symbolic link"},
		{request.AfterSource, &input.After.Source, "an observation source is one regular file of the open workspace, never a symbolic link"},
	}
	for _, document := range documents {
		if document.entry == "" {
			continue
		}
		path, err := artifactpath.File(root, document.entry)
		if err != nil {
			return RunExplanationResult{State: Failed, Reason: document.reason}
		}
		*document.into = path
	}
	if ctx.Err() != nil {
		return RunExplanationResult{State: Cancelled, Reason: explanationCancelled}
	}
	explanation, err := runexplain.Explain(ctx, input)
	if ctx.Err() != nil {
		return RunExplanationResult{State: Cancelled, Reason: explanationCancelled}
	}
	if err != nil {
		// The sentence the command prints after "readmit: ". runexplain and
		// the readers it opens name no path and no value in a refusal.
		return RunExplanationResult{State: Failed, Reason: err.Error()}
	}
	return RunExplanationResult{State: Completed, Explanation: explanationView(root, request, bundleEntry, explanation)}
}

// explanationCancelled is what a cancelled explanation says. Nothing was
// retained, so there is nothing to recover but the reading itself.
const explanationCancelled = "the explanation was cancelled; it retained nothing, so explaining again decides exactly what it would have"

// retainedRunFolder resolves one entry naming a retained run to its folder:
// one real folder of the workspace, or one job reached through a suite
// execution's real `runs` folder, never through a symbolic link at any step.
func retainedRunFolder(root, entry string) (string, error) {
	path, err := runEvidencePath(root, entry)
	if err != nil {
		return "", err
	}
	return artifactpath.Child(filepath.Dir(path), filepath.Base(path))
}

// notARetainedRun is how a run entry that is not a folder of the workspace is
// refused, wherever it was named.
const notARetainedRun = "a retained run is one folder of the open workspace, or one job inside a suite execution's runs, and never a symbolic link"

// explainedBundle resolves one retained run entry to the run bundle it
// retained, by what the entry declares as the listing classifies it: a
// durable run keeps its result in result/, a result keeps its run in run/,
// and anything else is named as the run bundle itself. Each step is a real
// folder, never a symbolic link, so the bundle the reader is handed is inside
// the workspace. Whether the folder reached is a run bundle is the verifying
// reader's question, which it answers in its own words. It returns the
// bundle's path and its workspace-relative name.
func explainedBundle(root, entry string) (string, string, refusal) {
	path, err := retainedRunFolder(root, entry)
	if err != nil {
		return "", "", refusal{Failed, notARetainedRun}
	}
	named := entry
	kind, _ := classify(filepath.Dir(path), filepath.Base(path), true)
	if kind == JobArtifact {
		if path, err = artifactpath.Child(path, "result"); err != nil {
			return "", "", refusal{Failed, "this durable run holds no finalized result folder, so it retained no run bundle to explain; the run history reads its journal"}
		}
		named += "/result"
	}
	if kind == JobArtifact || kind == ResultArtifact {
		if path, err = artifactpath.Child(path, "run"); err != nil {
			return "", "", refusal{Failed, "this result holds no run folder, so it retained no run bundle to explain"}
		}
		named += "/run"
	}
	return path, named, refusal{}
}

// explanationView is the view of one explanation: the command's own wording for
// every assertion, message and observation, with every retained path named
// relative to the workspace, as the command names them when it is given the
// same relative bundle.
func explanationView(root string, request RunExplanationRequest, bundleEntry string, explanation runexplain.Explanation) *RunExplanation {
	run := explanation.Run
	view := &RunExplanation{
		Run: request.Run, Bundle: bundleEntry,
		Verdict:  string(explanation.Verdict),
		Declared: explanation.Set.Count, Passed: explanation.Passed, Failed: explanation.Failed,
		Undecided: explanation.Undecided, Skipped: explanation.Skipped,
		SetName: explanation.Set.Name, SetSchema: explanation.Set.Schema, SetIdentity: explanation.Set.Identity,
		RunSchema: run.Schema, RunState: run.State, RunIdentity: run.Identity, SourceIdentity: run.SourceBundleIdentity,
		ContainsSourceValues: run.ContainsSourceValues, ExportPolicy: run.ExportPolicy,
		Target: run.Target.Address, Transport: run.Target.Transport, TargetIdentity: run.TargetIdentity,
		StartedAt: runexplain.Instant(run.StartedAt), CompletedAt: runexplain.Instant(run.CompletedAt), Elapsed: run.Elapsed().String(),
		Messages:     make([]ExplainedMessage, 0, len(run.Messages)),
		Observations: make([]ExplainedObservation, 0, len(explanation.Observations)),
		Assertions:   make([]ExplainedAssertion, 0, len(explanation.Details)),
		Revealed:     request.Reveal,
	}
	if explanation.Failure != nil {
		view.ErrorClass, view.ErrorAssertion = explanation.Failure.Class, explanation.Failure.Assertion
	}
	for _, message := range run.Messages {
		view.Messages = append(view.Messages, ExplainedMessage{
			Source: message.Source, Outbound: message.Outbound, Outcome: string(message.Outcome), Delivery: message.Delivery,
			ACKCode: message.ACKCode, ACKCorrelation: message.ACKCorrelation, Elapsed: message.Elapsed.String(),
			Input: message.InputPayload(), Observed: message.ObservedPayload(),
		})
	}
	for _, observed := range explanation.Observations {
		view.Observations = append(view.Observations, ExplainedObservation{
			Scope: string(observed.Scope), Status: string(observed.Status), Schema: observed.Schema, SourceSchema: observed.SourceSchema,
			Window: observed.WindowIdentity, SourceKind: string(observed.Source.Kind), SourceIdentity: observed.Source.Identity,
			SourceScope: observed.Source.Scope, Records: observed.Records, Correlations: observed.CorrelationLine(),
			Capture: workspaceRelative(root, observed.CapturePath), Keys: observed.KeysLine(request.Reveal),
		})
	}
	for _, detail := range explanation.Details {
		evidence := make([]string, 0, len(detail.Evidence))
		for _, link := range detail.Evidence {
			if link.Artifact == run.Path {
				link.Artifact = bundleEntry
			} else if link.Artifact != "" {
				link.Artifact = workspaceRelative(root, link.Artifact)
			}
			evidence = append(evidence, link.Line())
		}
		view.Assertions = append(view.Assertions, ExplainedAssertion{
			ID: detail.ID, Operator: string(detail.Operator), Outcome: string(detail.Outcome),
			Reads: detail.ReadsLine(), Condition: detail.ConditionLine(request.Reveal),
			Expected: detail.ExpectedLine(request.Reveal), Observed: detail.ObservedLine(request.Reveal),
			Evidence: evidence,
		})
	}
	return view
}

// workspaceRelative names a retained path by its place in the workspace, and
// leaves a path outside it, such as a capture an observation source declares
// elsewhere, as the source declared it.
func workspaceRelative(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(relative) {
		return path
	}
	return filepath.ToSlash(relative)
}

// ExplanationChoiceResult carries one state. Entry names the chosen workspace
// entry for the kind of input that was asked for.
type ExplanationChoiceResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Kind   string `json:"kind,omitzero"`
	Entry  string `json:"entry,omitzero"`
}

func (r *ExplanationChoiceResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// outsideTheWorkspace is how a choice outside the open workspace is refused.
const outsideTheWorkspace = "an explanation reads entries of the open workspace; advanced selection cannot reach outside it"

// The inputs an explanation is chosen natively for.
const (
	runInput          = "run"
	assertionsInput   = "assertions"
	beforeInput       = "before"
	beforeSourceInput = "before-source"
	afterInput        = "after"
	afterSourceInput  = "after-source"
)

// explanationDialogs are the inputs an explanation is chosen natively for,
// each with the title its dialog shows. A run is a folder; everything else is
// one document.
var explanationDialogs = map[string]string{
	runInput:          "Choose a retained run to explain",
	assertionsInput:   "Choose the assertion set to re-decide",
	beforeInput:       "Choose the completion record of the observation before the run",
	beforeSourceInput: "Choose the observation source the observation before the run read",
	afterInput:        "Choose the completion record of the observation after the run",
	afterSourceInput:  "Choose the observation source the observation after the run read",
}

// ChooseExplanationInput is the native selection of one input of an
// explanation: the host's folder dialog for the retained run, and its file
// dialog over JSON documents for the assertion set and an observation's two
// documents. The choice must be one entry of the open workspace, the rule
// ChooseRunSpec keeps: the folder the dialog's answer names is resolved as the
// filesystem traverses it and must be the workspace itself, and the entry is
// never a symbolic link. A run may also be one job inside a suite execution's
// runs, whose `runs` folder must then be the workspace's own. A dismissed
// dialog is a cancellation.
func (a *App) ChooseExplanationInput(workspace, kind string) ExplanationChoiceResult {
	return run(a, true, false, func(ctx context.Context) ExplanationChoiceResult {
		title, known := explanationDialogs[kind]
		if !known {
			return ExplanationChoiceResult{State: Failed, Reason: "that is not an input an explanation is chosen for"}
		}
		root, declined := resolveFolder(workspace)
		if root == "" {
			return ExplanationChoiceResult{State: declined.state, Reason: declined.reason}
		}
		var chosen string
		if kind == runInput {
			folder, declined := a.chooseFolder(ctx, title)
			if folder == "" {
				return ExplanationChoiceResult{State: declined.state, Reason: declined.reason}
			}
			chosen = folder
		} else {
			files, declined := a.chooseFiles(ctx, title, "readmit documents", "*.json")
			if len(files) == 0 {
				return ExplanationChoiceResult{State: declined.state, Reason: declined.reason}
			}
			if len(files) != 1 {
				return ExplanationChoiceResult{State: Failed, Reason: "choose exactly one file"}
			}
			chosen = files[0]
		}
		folder, name := filepath.Split(filepath.Clean(chosen))
		folder, err := artifactpath.Resolve(folder)
		if err != nil || artifactpath.EntryName(name) != nil {
			return ExplanationChoiceResult{State: Failed, Reason: outsideTheWorkspace}
		}
		entry := name
		if folder != root {
			// A run may be one job of a suite execution, chosen inside the
			// `runs` folder that retains it.
			relative, err := filepath.Rel(root, folder)
			if kind != runInput || err != nil || !filepath.IsLocal(relative) {
				return ExplanationChoiceResult{State: Failed, Reason: outsideTheWorkspace}
			}
			entry = filepath.ToSlash(relative) + "/" + name
		}
		if kind == runInput {
			if _, err := retainedRunFolder(root, entry); err != nil {
				return ExplanationChoiceResult{State: Failed, Reason: notARetainedRun}
			}
		} else if _, err := artifactpath.File(root, entry); err != nil {
			return ExplanationChoiceResult{State: Failed, Reason: "the document is a regular file of the open workspace, never a symbolic link"}
		}
		return ExplanationChoiceResult{State: Completed, Kind: kind, Entry: entry}
	})
}
