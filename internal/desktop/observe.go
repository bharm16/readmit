package desktop

import (
	"context"
	"path/filepath"

	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
)

// ObservationSupportResult lists adapter/version support and qualification.
type ObservationSupportResult struct {
	State   State                                 `json:"state"`
	Reason  string                                `json:"reason,omitzero"`
	Support []operation.ObservationAdapterSupport `json:"support,omitzero"`
}

func (r *ObservationSupportResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ObservationWindowRequest names a workspace-relative or absolute window file.
type ObservationWindowRequest struct {
	Workspace  string                `json:"workspace"`
	WindowFile string                `json:"window_file"`
	Window     *observewindow.Window `json:"window,omitzero"`
}

// ObservationWindowResult carries one declared window and its identity.
type ObservationWindowResult struct {
	State      State                 `json:"state"`
	Reason     string                `json:"reason,omitzero"`
	Window     *observewindow.Window `json:"window,omitzero"`
	WindowFile string                `json:"window_file,omitzero"`
	Identity   string                `json:"identity,omitzero"`
}

func (r *ObservationWindowResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ObservationSourceRequest names a workspace-relative or absolute source file.
type ObservationSourceRequest struct {
	Workspace  string                `json:"workspace"`
	SourceFile string                `json:"source_file"`
	Source     *observesource.Source `json:"source,omitzero"`
}

// ObservationSourceResult carries one declared source and its identity.
type ObservationSourceResult struct {
	State      State                                `json:"state"`
	Reason     string                               `json:"reason,omitzero"`
	Source     *observesource.Source                `json:"source,omitzero"`
	SourceFile string                               `json:"source_file,omitzero"`
	Identity   string                               `json:"identity,omitzero"`
	Support    *operation.ObservationAdapterSupport `json:"support,omitzero"`
}

func (r *ObservationSourceResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ObservationValidateRequest checks source and window locally without collecting.
type ObservationValidateRequest struct {
	Workspace  string `json:"workspace"`
	SourceFile string `json:"source_file"`
	WindowFile string `json:"window_file"`
}

// ObservationValidateResult reports local validation only.
type ObservationValidateResult struct {
	State   State                                `json:"state"`
	Reason  string                               `json:"reason,omitzero"`
	Source  *observesource.Source                `json:"source,omitzero"`
	Window  *observewindow.Window                `json:"window,omitzero"`
	Support *operation.ObservationAdapterSupport `json:"support,omitzero"`
}

func (r *ObservationValidateResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ObservationCollectFacadeRequest authorizes one collection attempt.
type ObservationCollectFacadeRequest struct {
	Workspace   string   `json:"workspace"`
	SourceFile  string   `json:"source_file"`
	WindowFile  string   `json:"window_file"`
	OutputFile  string   `json:"output_file"`
	SnapshotDir string   `json:"snapshot_dir"`
	PolicyFile  string   `json:"policy_file,omitzero"`
	Produced    []string `json:"produced,omitzero"`
	Authorize   bool     `json:"authorize"`
}

// ObservationCompletionResult carries a retained completion and summary counts.
type ObservationCompletionResult struct {
	State      State                                `json:"state"`
	Reason     string                               `json:"reason,omitzero"`
	Completion *observewindow.Completion            `json:"completion,omitzero"`
	Summary    *operation.ObservationAbsenceSummary `json:"summary,omitzero"`
	OutputFile string                               `json:"output_file,omitzero"`
}

func (r *ObservationCompletionResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ObservationExplainRequest re-reads a retained completion.
type ObservationExplainRequest struct {
	Workspace      string `json:"workspace"`
	CompletionFile string `json:"completion_file"`
	WindowFile     string `json:"window_file,omitzero"`
}

// ObservationCaptureBindRequest is the #250 handoff into observation authoring.
type ObservationCaptureBindRequest struct {
	Workspace    string                              `json:"workspace"`
	Binding      operation.CaptureObservationBinding `json:"binding"`
	RelativeCase string                              `json:"relative_case,omitzero"`
}

// ObservationSupport reports adapter/version support without observing anything.
func (a *App) ObservationSupport() ObservationSupportResult {
	return ObservationSupportResult{State: Completed, Support: operation.ObservationSupport()}
}

// OpenObservationWindow opens an existing window or returns defaults for a new file.
func (a *App) OpenObservationWindow(workspace, windowFile string) ObservationWindowResult {
	return run(a, true, false, func(ctx context.Context) ObservationWindowResult {
		path, ref := resolveWorkspacePath(workspace, windowFile)
		if path == "" {
			return ObservationWindowResult{State: ref.state, Reason: ref.reason}
		}
		window, err := operation.OpenOrNewObservationWindow(path)
		if err != nil {
			return ObservationWindowResult{State: Failed, Reason: err.Error()}
		}
		return ObservationWindowResult{State: Completed, Window: &window, WindowFile: windowFile, Identity: window.Identity()}
	})
}

// SaveObservationWindow writes a window through the shared writer and re-reads it.
func (a *App) SaveObservationWindow(request ObservationWindowRequest) ObservationWindowResult {
	return run(a, true, true, func(ctx context.Context) ObservationWindowResult {
		if request.Window == nil {
			return ObservationWindowResult{State: Failed, Reason: "an observation window is required"}
		}
		path, ref := resolveWorkspacePath(request.Workspace, request.WindowFile)
		if path == "" {
			return ObservationWindowResult{State: ref.state, Reason: ref.reason}
		}
		window, identity, err := operation.SaveObservationWindow(path, *request.Window)
		if err != nil {
			return ObservationWindowResult{State: Failed, Reason: err.Error()}
		}
		return ObservationWindowResult{State: Completed, Window: &window, WindowFile: request.WindowFile, Identity: identity}
	})
}

// ValidateObservationWindow validates without observing.
func (a *App) ValidateObservationWindow(workspace, windowFile string) ObservationWindowResult {
	return run(a, true, false, func(ctx context.Context) ObservationWindowResult {
		path, ref := resolveWorkspacePath(workspace, windowFile)
		if path == "" {
			return ObservationWindowResult{State: ref.state, Reason: ref.reason}
		}
		window, identity, err := operation.ValidateObservationWindow(path)
		if err != nil {
			return ObservationWindowResult{State: Failed, Reason: err.Error()}
		}
		return ObservationWindowResult{State: Completed, Window: &window, WindowFile: windowFile, Identity: identity}
	})
}

// OpenObservationSource opens an existing source or returns defaults for a new file.
func (a *App) OpenObservationSource(workspace, sourceFile string) ObservationSourceResult {
	return run(a, true, false, func(ctx context.Context) ObservationSourceResult {
		path, ref := resolveWorkspacePath(workspace, sourceFile)
		if path == "" {
			return ObservationSourceResult{State: ref.state, Reason: ref.reason}
		}
		source, err := operation.OpenOrNewObservationSource(path)
		if err != nil {
			return ObservationSourceResult{State: Failed, Reason: err.Error()}
		}
		support := supportFor(source)
		return ObservationSourceResult{State: Completed, Source: &source, SourceFile: sourceFile, Identity: source.Identity(), Support: &support}
	})
}

// SaveObservationSource writes a source through the shared writer and re-reads it.
func (a *App) SaveObservationSource(request ObservationSourceRequest) ObservationSourceResult {
	return run(a, true, true, func(ctx context.Context) ObservationSourceResult {
		if request.Source == nil {
			return ObservationSourceResult{State: Failed, Reason: "an observation source is required"}
		}
		path, ref := resolveWorkspacePath(request.Workspace, request.SourceFile)
		if path == "" {
			return ObservationSourceResult{State: ref.state, Reason: ref.reason}
		}
		source, identity, err := operation.SaveObservationSource(path, *request.Source)
		if err != nil {
			return ObservationSourceResult{State: Failed, Reason: err.Error()}
		}
		support := supportFor(source)
		return ObservationSourceResult{State: Completed, Source: &source, SourceFile: request.SourceFile, Identity: identity, Support: &support}
	})
}

// ValidateObservationSource validates without collecting.
func (a *App) ValidateObservationSource(workspace, sourceFile string) ObservationSourceResult {
	return run(a, true, false, func(ctx context.Context) ObservationSourceResult {
		path, ref := resolveWorkspacePath(workspace, sourceFile)
		if path == "" {
			return ObservationSourceResult{State: ref.state, Reason: ref.reason}
		}
		source, identity, err := operation.ValidateObservationSource(path)
		if err != nil {
			return ObservationSourceResult{State: Failed, Reason: err.Error()}
		}
		support := supportFor(source)
		return ObservationSourceResult{State: Completed, Source: &source, SourceFile: sourceFile, Identity: identity, Support: &support}
	})
}

// ValidateObservationPair checks source and window locally without network I/O.
func (a *App) ValidateObservationPair(request ObservationValidateRequest) ObservationValidateResult {
	return run(a, true, false, func(ctx context.Context) ObservationValidateResult {
		sourcePath, ref := resolveWorkspacePath(request.Workspace, request.SourceFile)
		if sourcePath == "" {
			return ObservationValidateResult{State: ref.state, Reason: ref.reason}
		}
		windowPath, wRef := resolveWorkspacePath(request.Workspace, request.WindowFile)
		if windowPath == "" {
			return ObservationValidateResult{State: wRef.state, Reason: wRef.reason}
		}
		source, window, err := operation.ValidateObservationPair(sourcePath, windowPath)
		if err != nil {
			return ObservationValidateResult{State: Failed, Reason: err.Error()}
		}
		support := supportFor(source)
		return ObservationValidateResult{State: Completed, Source: &source, Window: &window, Support: &support}
	})
}

// CollectObservation runs one explicitly authorized collection. It names its
// operation, so the privacy status can tell a window in the middle of a
// collection from an idle one, and the window's own cancel command still stops
// it exactly as before. Like `readmit observe collect`, it is admitted as
// execution as well as authoring.
func (a *App) CollectObservation(request ObservationCollectFacadeRequest) ObservationCompletionResult {
	return runNamed[ObservationCompletionResult, *ObservationCompletionResult](a, "observation", true, true, func(ctx context.Context) (out ObservationCompletionResult) {
		settle, admitted := a.admitExecution(ctx)
		if admitted != nil {
			return ObservationCompletionResult{State: PermissionDenied, Reason: admitted.Error()}
		}
		defer func() {
			if err := settle(); err != nil {
				out.State, out.Reason = Failed, settlementFailed
			}
		}()
		sourcePath, ref := resolveWorkspacePath(request.Workspace, request.SourceFile)
		if sourcePath == "" {
			return ObservationCompletionResult{State: ref.state, Reason: ref.reason}
		}
		windowPath, wRef := resolveWorkspacePath(request.Workspace, request.WindowFile)
		if windowPath == "" {
			return ObservationCompletionResult{State: wRef.state, Reason: wRef.reason}
		}
		outputPath, oRef := resolveWorkspacePath(request.Workspace, request.OutputFile)
		if outputPath == "" {
			return ObservationCompletionResult{State: oRef.state, Reason: oRef.reason}
		}
		snapshotPath, sRef := resolveWorkspacePath(request.Workspace, request.SnapshotDir)
		if snapshotPath == "" {
			return ObservationCompletionResult{State: sRef.state, Reason: sRef.reason}
		}
		policyPath := ""
		if request.PolicyFile != "" {
			var pRef refusal
			policyPath, pRef = resolveWorkspacePath(request.Workspace, request.PolicyFile)
			if policyPath == "" {
				return ObservationCompletionResult{State: pRef.state, Reason: pRef.reason}
			}
		}
		completion, err := operation.CollectObservation(ctx, operation.ObservationCollectRequest{
			SourcePath: sourcePath, WindowPath: windowPath, OutputPath: outputPath, SnapshotPath: snapshotPath,
			PolicyPath: policyPath, Produced: request.Produced, Authorize: request.Authorize,
		})
		if err != nil {
			state := Failed
			if ctx.Err() != nil {
				state = Cancelled
			}
			return ObservationCompletionResult{State: state, Reason: err.Error()}
		}
		summary := operation.SummarizeObservationCompletion(completion)
		return ObservationCompletionResult{State: Completed, Completion: &completion, Summary: &summary, OutputFile: request.OutputFile}
	})
}

// ExplainObservation re-reads a retained completion.
func (a *App) ExplainObservation(request ObservationExplainRequest) ObservationCompletionResult {
	return run(a, true, false, func(ctx context.Context) ObservationCompletionResult {
		completionPath, ref := resolveWorkspacePath(request.Workspace, request.CompletionFile)
		if completionPath == "" {
			return ObservationCompletionResult{State: ref.state, Reason: ref.reason}
		}
		windowPath := ""
		if request.WindowFile != "" {
			var wRef refusal
			windowPath, wRef = resolveWorkspacePath(request.Workspace, request.WindowFile)
			if windowPath == "" {
				return ObservationCompletionResult{State: wRef.state, Reason: wRef.reason}
			}
		}
		completion, err := operation.ExplainObservation(completionPath, windowPath)
		if err != nil {
			return ObservationCompletionResult{State: Failed, Reason: err.Error()}
		}
		summary := operation.SummarizeObservationCompletion(completion)
		return ObservationCompletionResult{State: Completed, Completion: &completion, Summary: &summary, OutputFile: request.CompletionFile}
	})
}

// BindCaptureObservation prepares a downstream-capture source from a retained
// case without collecting. This is the navigation seam #250 completes into.
func (a *App) BindCaptureObservation(request ObservationCaptureBindRequest) ObservationSourceResult {
	return run(a, true, false, func(ctx context.Context) ObservationSourceResult {
		relative := request.RelativeCase
		if relative == "" {
			relative = filepath.Base(request.Binding.CasePath)
		}
		source, err := operation.SourceFromCaptureBinding(request.Binding, relative)
		if err != nil {
			return ObservationSourceResult{State: Failed, Reason: err.Error()}
		}
		support := supportFor(source)
		return ObservationSourceResult{State: Completed, Source: &source, Identity: source.Identity(), Support: &support}
	})
}

func supportFor(source observesource.Source) operation.ObservationAdapterSupport {
	adapter := source.Observes.Kind
	if source.Database != nil {
		adapter = source.Database.Driver
	}
	for _, row := range operation.ObservationSupport() {
		if row.Kind == source.Observes.Kind && (row.Adapter == adapter || row.Adapter == source.Observes.Kind) {
			return row
		}
	}
	return operation.ObservationAdapterSupport{
		Kind: source.Observes.Kind, Adapter: adapter, Qualification: "unknown", ProductionClaim: false,
	}
}
