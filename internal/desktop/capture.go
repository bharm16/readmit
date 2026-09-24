package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
)

// CapturePhase names what the capture screen is doing without disclosing values.
type CapturePhase string

const (
	CaptureIdle       CapturePhase = "idle"
	CapturePreviewing CapturePhase = "previewing"
	CaptureListening  CapturePhase = "listening"
	CaptureCollecting CapturePhase = "collecting"
	CaptureStopping   CapturePhase = "stopping"
	CaptureStopped    CapturePhase = "stopped"
	CaptureFailed     CapturePhase = "failed"
)

// PathChoiceResult is the outcome of a native file or folder dialog used to
// fill a source or listener field.
type PathChoiceResult struct {
	State  State    `json:"state"`
	Reason string   `json:"reason,omitzero"`
	Kind   string   `json:"kind,omitzero"`
	Paths  []string `json:"paths,omitzero"`
}

func (r *PathChoiceResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ChooseCapturePath presents a native dialog for a source root, transfer
// program, certificate, policy file, or journal directory.
func (a *App) ChooseCapturePath(kind string) PathChoiceResult {
	return run(a, true, false, func(ctx context.Context) PathChoiceResult {
		switch kind {
		case "source-root", "output-folder", "journal-folder":
			title := "Choose a folder"
			switch kind {
			case "source-root":
				title = "Choose the local export folder"
			case "output-folder":
				title = "Choose where to stage collected evidence"
			case "journal-folder":
				title = "Choose a parent folder for the capture journal"
			}
			folder, declined := a.chooseFolder(ctx, title)
			if folder == "" {
				return PathChoiceResult{State: declined.state, Reason: declined.reason}
			}
			return PathChoiceResult{State: Completed, Kind: kind, Paths: []string{folder}}
		case "transfer-program":
			files, declined := a.chooseFiles(ctx, "Choose the customer transfer program", "Programs (*.*)", "*.*")
			if len(files) == 0 {
				return PathChoiceResult{State: declined.state, Reason: declined.reason}
			}
			return PathChoiceResult{State: Completed, Kind: kind, Paths: files[:1]}
		case "certificate", "client-ca", "secrets", "policy", "source", "send-policy":
			title := "Choose a file"
			filter := "All files (*.*)"
			pattern := "*.*"
			switch kind {
			case "certificate", "client-ca":
				title = "Choose a PEM certificate"
				filter = "PEM certificates (*.pem *.crt)"
				pattern = "*.pem;*.crt"
			case "secrets":
				title = "Choose the secrets store"
			case "policy":
				title = "Choose a receiver policy"
			case "source":
				title = "Choose a source registration"
			case "send-policy":
				title = "Choose an approved-destination policy"
			}
			files, declined := a.chooseFiles(ctx, title, filter, pattern)
			if len(files) == 0 {
				return PathChoiceResult{State: declined.state, Reason: declined.reason}
			}
			return PathChoiceResult{State: Completed, Kind: kind, Paths: files[:1]}
		default:
			return PathChoiceResult{State: Failed, Reason: "unsupported capture path kind"}
		}
	})
}

// SourceRegistrationRequest saves or validates a source registration document.
type SourceRegistrationRequest struct {
	Workspace  string                `json:"workspace"`
	SourceFile string                `json:"source_file"`
	Source     evidencesource.Source `json:"source"`
}

// SourceRegistrationResult carries a validated source registration.
type SourceRegistrationResult struct {
	State      State                  `json:"state"`
	Reason     string                 `json:"reason,omitzero"`
	Source     *evidencesource.Source `json:"source,omitzero"`
	SourceFile string                 `json:"source_file,omitzero"`
}

func (r *SourceRegistrationResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// SaveSourceRegistration writes a validated source registration through the
// shared operation.
func (a *App) SaveSourceRegistration(request SourceRegistrationRequest) SourceRegistrationResult {
	return run(a, false, true, func(context.Context) SourceRegistrationResult {
		path, ref := resolveWorkspacePath(request.Workspace, request.SourceFile)
		if path == "" {
			return SourceRegistrationResult{State: ref.state, Reason: ref.reason}
		}
		saved, err := operation.SourceSave(path, request.Source)
		if err != nil {
			return SourceRegistrationResult{State: Failed, Reason: err.Error()}
		}
		return SourceRegistrationResult{State: Completed, Source: &saved, SourceFile: request.SourceFile}
	})
}

// ReadSourceRegistration opens one declared source registration.
func (a *App) ReadSourceRegistration(workspace, sourceFile string) SourceRegistrationResult {
	return run(a, false, false, func(context.Context) SourceRegistrationResult {
		path, ref := resolveWorkspacePath(workspace, sourceFile)
		if path == "" {
			return SourceRegistrationResult{State: ref.state, Reason: ref.reason}
		}
		source, err := operation.SourceRead(path)
		if err != nil {
			return SourceRegistrationResult{State: Failed, Reason: err.Error()}
		}
		return SourceRegistrationResult{State: Completed, Source: &source, SourceFile: sourceFile}
	})
}

// SourceWorkRequest diagnoses or collects from an approved source under a plan.
type SourceWorkRequest struct {
	Workspace   string                 `json:"workspace"`
	SourceFile  string                 `json:"source_file,omitzero"`
	Source      *evidencesource.Source `json:"source,omitzero"`
	PolicyFile  string                 `json:"policy_file,omitzero"`
	Plan        *importer.Plan         `json:"plan,omitzero"`
	OutputName  string                 `json:"output_name,omitzero"`
	ReceiptName string                 `json:"receipt_name,omitzero"`
}

// SourceAccessSummary is the value-free diagnosis the window shows.
type SourceAccessSummary struct {
	Schema     string `json:"schema"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitzero"`
	Listed     bool   `json:"listed"`
	Declared   int    `json:"declared"`
	Selected   int    `json:"selected"`
	Readable   int    `json:"readable"`
	Unreadable int    `json:"unreadable"`
	NotRead    int    `json:"not_read"`
	SourceName string `json:"source_name,omitzero"`
	SourceKind string `json:"source_kind,omitzero"`
	Scope      string `json:"scope,omitzero"`
}

// SourceAccessResult carries a value-free access diagnosis.
type SourceAccessResult struct {
	State  State                `json:"state"`
	Reason string               `json:"reason,omitzero"`
	Access *SourceAccessSummary `json:"access,omitzero"`
}

func (r *SourceAccessResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SourceCollectionSummary is the value-free receipt summary the window shows.
type SourceCollectionSummary struct {
	Schema      string `json:"schema"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitzero"`
	Declared    int    `json:"declared"`
	Collected   int    `json:"collected"`
	Duplicates  int    `json:"duplicates"`
	Excluded    int    `json:"excluded"`
	Unreadable  int    `json:"unreadable"`
	NotRead     int    `json:"not_read"`
	Bytes       int64  `json:"bytes"`
	Records     int64  `json:"records"`
	Occurrences int64  `json:"occurrences"`
}

// SourceCollectionResult carries a staged collection receipt and destinations.
type SourceCollectionResult struct {
	State       State                    `json:"state"`
	Reason      string                   `json:"reason,omitzero"`
	Collection  *SourceCollectionSummary `json:"collection,omitzero"`
	OutputPath  string                   `json:"output_path,omitzero"`
	ReceiptPath string                   `json:"receipt_path,omitzero"`
}

func (r *SourceCollectionResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// DiagnoseSource reports what access was available without collecting.
func (a *App) DiagnoseSource(request SourceWorkRequest) SourceAccessResult {
	return runNamed[SourceAccessResult, *SourceAccessResult](a, profiles["DiagnoseSource"], func(ctx context.Context) SourceAccessResult {
		source, options, err := a.sourceWork(ctx, request)
		if err != nil {
			return SourceAccessResult{State: Failed, Reason: err.Error()}
		}
		access, err := operation.SourceDiagnose(ctx, source, options, "")
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return SourceAccessResult{State: Cancelled, Reason: cancelledRefusal.reason}
			}
			return SourceAccessResult{State: Failed, Reason: err.Error()}
		}
		return SourceAccessResult{State: Completed, Access: &SourceAccessSummary{
			Schema: access.Schema, Status: string(access.Status), Reason: access.Reason,
			Listed: access.Listed, Declared: access.Declared, Selected: access.Selected,
			Readable: access.Readable, Unreadable: access.Unreadable, NotRead: access.NotRead,
			SourceName: access.Source.Name, SourceKind: string(access.Source.Kind), Scope: access.Source.Scope,
		}}
	})
}

// CollectSource stages evidence from an approved source with a receipt.
func (a *App) CollectSource(request SourceWorkRequest) SourceCollectionResult {
	return runNamed[SourceCollectionResult, *SourceCollectionResult](a, profiles["CollectSource"], func(ctx context.Context) SourceCollectionResult {
		outputName := request.OutputName
		if outputName == "" {
			outputName = "collected"
		}
		receiptName := request.ReceiptName
		if receiptName == "" {
			receiptName = "collection.json"
		}
		if artifactpath.EntryName(outputName) != nil {
			return SourceCollectionResult{State: Failed, Reason: "the staging folder must be one new entry of the open workspace"}
		}
		if artifactpath.EntryName(receiptName) != nil {
			return SourceCollectionResult{State: Failed, Reason: "the collection receipt must be one new entry of the open workspace"}
		}
		if request.Workspace == "" {
			return SourceCollectionResult{State: Failed, Reason: "a workspace is required to stage collected evidence"}
		}
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return SourceCollectionResult{State: declined.state, Reason: declined.reason}
		}
		source, options, err := a.sourceWork(ctx, request)
		if err != nil {
			return SourceCollectionResult{State: Failed, Reason: err.Error()}
		}
		output := filepath.Join(root, outputName)
		receipt := filepath.Join(root, receiptName)
		collection, err := operation.SourceCollect(ctx, source, output, receipt, options)
		if err != nil && !errors.Is(err, evidencesource.ErrIncomplete) {
			if errors.Is(ctx.Err(), context.Canceled) {
				return SourceCollectionResult{State: Cancelled, Reason: cancelledRefusal.reason, OutputPath: outputName, ReceiptPath: receiptName}
			}
			if errors.Is(err, os.ErrPermission) {
				return SourceCollectionResult{State: PermissionDenied, Reason: "this account cannot stage collected evidence here"}
			}
			return SourceCollectionResult{State: Failed, Reason: err.Error()}
		}
		state := Completed
		reason := ""
		if err != nil {
			// The receipt already says why the collection stopped; a person's
			// cancellation is answered as one, never as a failed source.
			state = Failed
			if collection.Status == observewindow.Cancelled {
				state = Cancelled
			}
			reason = collection.Reason
			if reason == "" {
				reason = "the collection did not complete"
			}
		}
		summary := &SourceCollectionSummary{
			Schema: collection.Schema, Status: string(collection.Status), Reason: collection.Reason,
			Declared: collection.Totals.Declared, Collected: collection.Totals.Collected,
			Duplicates: collection.Totals.Duplicates, Excluded: collection.Totals.Excluded,
			Unreadable: collection.Totals.Unreadable, NotRead: collection.Totals.NotRead,
			Bytes: collection.Totals.Bytes, Records: collection.Totals.Records, Occurrences: collection.Totals.Occurrences,
		}
		return SourceCollectionResult{State: state, Reason: reason, Collection: summary, OutputPath: outputName, ReceiptPath: receiptName}
	})
}

func (a *App) sourceWork(_ context.Context, request SourceWorkRequest) (evidencesource.Source, evidencesource.Options, error) {
	var source evidencesource.Source
	switch {
	case request.Source != nil:
		source = *request.Source
		if source.Schema == "" {
			source.Schema = evidencesource.Schema
		}
		if err := source.Validate(); err != nil {
			return evidencesource.Source{}, evidencesource.Options{}, err
		}
	case request.SourceFile != "":
		path, ref := resolveWorkspacePath(request.Workspace, request.SourceFile)
		if path == "" {
			return evidencesource.Source{}, evidencesource.Options{}, errors.New(ref.reason)
		}
		var err error
		source, err = operation.SourceRead(path)
		if err != nil {
			return evidencesource.Source{}, evidencesource.Options{}, err
		}
	default:
		return evidencesource.Source{}, evidencesource.Options{}, errors.New("a source registration is required")
	}
	plan := operation.DefaultImportPlan()
	if request.Plan != nil {
		plan = *request.Plan
	}
	policyPath := ""
	if request.PolicyFile != "" {
		var ref refusal
		if policyPath, ref = resolveWorkspacePath(request.Workspace, request.PolicyFile); policyPath == "" {
			return evidencesource.Source{}, evidencesource.Options{}, errors.New(ref.reason)
		}
	}
	options, err := operation.SourceOptions(plan, policyPath)
	if err != nil {
		return evidencesource.Source{}, evidencesource.Options{}, err
	}
	return source, options, nil
}

// ReceiverPolicyRequest saves a declarative responder policy.
type ReceiverPolicyRequest struct {
	Workspace  string            `json:"workspace"`
	PolicyFile string            `json:"policy_file"`
	Policy     collection.Policy `json:"policy"`
}

// ReceiverPolicyResult carries a validated receiver policy.
type ReceiverPolicyResult struct {
	State      State              `json:"state"`
	Reason     string             `json:"reason,omitzero"`
	Policy     *collection.Policy `json:"policy,omitzero"`
	PolicyFile string             `json:"policy_file,omitzero"`
}

func (r *ReceiverPolicyResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SaveReceiverPolicy writes a declarative responder policy.
func (a *App) SaveReceiverPolicy(request ReceiverPolicyRequest) ReceiverPolicyResult {
	return run(a, false, true, func(context.Context) ReceiverPolicyResult {
		path, ref := resolveWorkspacePath(request.Workspace, request.PolicyFile)
		if path == "" {
			return ReceiverPolicyResult{State: ref.state, Reason: ref.reason}
		}
		saved, err := operation.ReceiverPolicySave(path, request.Policy)
		if err != nil {
			return ReceiverPolicyResult{State: Failed, Reason: err.Error()}
		}
		return ReceiverPolicyResult{State: Completed, Policy: &saved, PolicyFile: request.PolicyFile}
	})
}

// ReadReceiverPolicy opens one declared responder policy.
func (a *App) ReadReceiverPolicy(workspace, policyFile string) ReceiverPolicyResult {
	return run(a, false, false, func(context.Context) ReceiverPolicyResult {
		path, ref := resolveWorkspacePath(workspace, policyFile)
		if path == "" {
			return ReceiverPolicyResult{State: ref.state, Reason: ref.reason}
		}
		policy, err := operation.ReceiverPolicyRead(path)
		if err != nil {
			return ReceiverPolicyResult{State: refusalState(err), Reason: err.Error()}
		}
		return ReceiverPolicyResult{State: Completed, Policy: &policy, PolicyFile: policyFile}
	})
}

// CaptureRequest configures a collector or SIU fixture before preview or start.
type CaptureRequest struct {
	Workspace          string             `json:"workspace"`
	Kind               string             `json:"kind"` // "collect" or "listen"
	Address            string             `json:"address"`
	ApprovedBind       bool               `json:"approved_bind,omitzero"`
	PolicyFile         string             `json:"policy_file,omitzero"`
	Policy             *collection.Policy `json:"policy,omitzero"`
	FixtureMode        string             `json:"fixture_mode,omitzero"`
	OutputName         string             `json:"output_name"`
	JournalName        string             `json:"journal_name,omitzero"`
	ObservationName    string             `json:"observation_name,omitzero"`
	MaxFrameBytes      int                `json:"max_frame_bytes,omitzero"`
	IdleTimeout        string             `json:"idle_timeout,omitzero"`
	ApplicationTimeout string             `json:"application_ack_timeout,omitzero"`
	MaxMessages        int                `json:"max_messages,omitzero"`
	MaxConnections     int                `json:"max_connections,omitzero"`
	MaxSessions        int                `json:"max_sessions,omitzero"`
	MaxCaptureBytes    int                `json:"max_capture_bytes,omitzero"`
	TLSCertificateFile string             `json:"tls_certificate_file,omitzero"`
	TLSKeyReference    string             `json:"tls_key_reference,omitzero"`
	SecretsFile        string             `json:"secrets_file,omitzero"`
	ClientCAFile       string             `json:"client_ca_file,omitzero"`
}

// CapturePreviewResult carries the value-free preview before an authorized start.
type CapturePreviewResult struct {
	State   State                     `json:"state"`
	Reason  string                    `json:"reason,omitzero"`
	Phase   CapturePhase              `json:"phase,omitzero"`
	Preview *operation.CapturePreview `json:"preview,omitzero"`
}

func (r *CapturePreviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// CaptureSessionResult is the outcome of one authorized listen/collect serve.
type CaptureSessionResult struct {
	State           State                     `json:"state"`
	Reason          string                    `json:"reason,omitzero"`
	Phase           CapturePhase              `json:"phase,omitzero"`
	BoundAddress    string                    `json:"bound_address,omitzero"`
	Case            *Case                     `json:"case,omitzero"`
	CasePath        string                    `json:"case_path,omitzero"`
	Journal         *capturejournal.Summary   `json:"journal,omitzero"`
	JournalPath     string                    `json:"journal_path,omitzero"`
	ObservationPath string                    `json:"observation_path,omitzero"`
	Received        int                       `json:"received,omitzero"`
	Connections     int                       `json:"connections,omitzero"`
	Dropped         int                       `json:"dropped,omitzero"`
	Preview         *operation.CapturePreview `json:"preview,omitzero"`
	Ledger          *FixtureLedger            `json:"ledger,omitzero"`
}

func (r *CaptureSessionResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// refuseExecution answers a capture its execution admission declined: it
// never started, so it failed, or stopped when the person cancelled while
// admission waited.
func (r *CaptureSessionResult) refuseExecution(state State, reason string) {
	r.State, r.Reason, r.Phase = state, reason, CaptureFailed
	if state == Cancelled {
		r.Phase = CaptureStopped
	}
}

// FixtureLedger is the appointment ledger a fixture listen sealed into its
// case, counted as `readmit listen` and `readmit timeline` print it: the
// receiver's testimony, never a verdict, and never one of its values.
type FixtureLedger struct {
	Schema     string `json:"schema"`
	Profile    string `json:"profile"`
	Mode       string `json:"mode"`
	Processed  int    `json:"processed"`
	Records    int    `json:"records"`
	Consistent bool   `json:"consistent"`
}

// CaptureProgress is where a running collector or fixture accepts
// connections: the address it bound, which is the only place a port of 0
// becomes a port a sender can be pointed at.
type CaptureProgress struct {
	Kind         string `json:"kind"`
	BoundAddress string `json:"bound_address"`
}

// CaptureProgressResult is Empty while nothing listens, and Completed with the
// bound address while a collector or fixture is ready to accept.
type CaptureProgressResult struct {
	State    State            `json:"state"`
	Reason   string           `json:"reason,omitzero"`
	Progress *CaptureProgress `json:"progress,omitzero"`
}

func (r *CaptureProgressResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// CaptureProgress reads where the running capture listens. It does not claim
// the operation slot, so the screen can read it while StartCapture holds the
// slot, and it starts, binds and changes nothing.
func (a *App) CaptureProgress() CaptureProgressResult {
	a.captureMu.Lock()
	defer a.captureMu.Unlock()
	if a.captureProgress == nil {
		return CaptureProgressResult{State: Empty}
	}
	listening := *a.captureProgress
	return CaptureProgressResult{State: Completed, Progress: &listening}
}

func (a *App) reportCaptureProgress(kind string) func(bound string) error {
	return func(bound string) error {
		a.captureMu.Lock()
		defer a.captureMu.Unlock()
		a.captureProgress = &CaptureProgress{Kind: kind, BoundAddress: bound}
		return nil
	}
}

func (a *App) endCaptureProgress() {
	a.captureMu.Lock()
	defer a.captureMu.Unlock()
	a.captureProgress = nil
}

// CaptureJournalResult recovers a retained capture journal read-only.
type CaptureJournalResult struct {
	State   State                   `json:"state"`
	Reason  string                  `json:"reason,omitzero"`
	Phase   CapturePhase            `json:"phase,omitzero"`
	Journal *capturejournal.Summary `json:"journal,omitzero"`
	Path    string                  `json:"path,omitzero"`
}

func (r *CaptureJournalResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// PreviewCapture validates listener/collector configuration without binding.
func (a *App) PreviewCapture(request CaptureRequest) CapturePreviewResult {
	return run(a, false, false, func(ctx context.Context) CapturePreviewResult {
		preview, err := a.capturePreview(request)
		if err != nil {
			return CapturePreviewResult{State: Failed, Reason: err.Error(), Phase: CaptureFailed}
		}
		return CapturePreviewResult{State: Completed, Phase: CapturePreviewing, Preview: &preview}
	})
}

// StartCapture starts a collector or SIU fixture only after explicit authorized
// action. Stop/cancel uses Cancel through the shared engine.
func (a *App) StartCapture(request CaptureRequest) CaptureSessionResult {
	return runNamed[CaptureSessionResult, *CaptureSessionResult](a, profiles["StartCapture"], func(ctx context.Context) (out CaptureSessionResult) {
		if artifactpath.EntryName(request.OutputName) != nil {
			return CaptureSessionResult{State: Failed, Reason: "the captured case must be one new entry of the open workspace", Phase: CaptureFailed}
		}
		if request.Kind == "listen" && request.ObservationName != "" && artifactpath.EntryName(request.ObservationName) != nil {
			return CaptureSessionResult{State: Failed, Reason: "the observation record must be one new entry of the open workspace", Phase: CaptureFailed}
		}
		if request.Workspace == "" {
			return CaptureSessionResult{State: Failed, Reason: "a workspace is required", Phase: CaptureFailed}
		}
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return CaptureSessionResult{State: declined.state, Reason: declined.reason, Phase: CaptureFailed}
		}
		defer a.endCaptureProgress()

		preview, err := a.capturePreview(request)
		if err != nil {
			return CaptureSessionResult{State: Failed, Reason: err.Error(), Phase: CaptureFailed}
		}
		output := filepath.Join(root, request.OutputName)

		switch request.Kind {
		case "listen":
			obsName := observationName(request)
			cfg := listenConfig(request, output, filepath.Join(root, obsName))
			cfg.Listening = a.reportCaptureProgress("listen")
			result, serveErr := operation.StartListen(ctx, cfg)
			out = CaptureSessionResult{
				BoundAddress:    result.BoundAddress,
				CasePath:        request.OutputName,
				ObservationPath: obsName,
				Preview:         &preview,
			}
			if result.Observation != nil {
				out.Received = len(result.Observation.Processed)
			}
			return a.finishCapture(ctx, out, result.Bundle, serveErr, CaptureListening)
		case "collect":
			cfg, err := a.collectConfig(request, output)
			if err != nil {
				return CaptureSessionResult{State: Failed, Reason: err.Error(), Phase: CaptureFailed, Preview: &preview}
			}
			report := a.reportCaptureProgress("collect")
			cfg.Listening = func(bound string, _ collection.Policy) error { return report(bound) }
			result, serveErr := operation.StartCollect(ctx, cfg)
			out = CaptureSessionResult{
				BoundAddress: result.BoundAddress,
				CasePath:     request.OutputName,
				Journal:      result.Journal,
				JournalPath:  request.JournalName,
				Preview:      &preview,
			}
			if result.Journal != nil {
				out.Received = result.Journal.Received
			}
			if result.Bundle != nil {
				out.Connections = len(result.Bundle.Manifest.Sources)
			}
			return a.finishCapture(ctx, out, result.Bundle, serveErr, CaptureCollecting)
		default:
			return CaptureSessionResult{State: Failed, Reason: "capture kind must be collect or listen", Phase: CaptureFailed}
		}
	})
}

func (a *App) finishCapture(ctx context.Context, out CaptureSessionResult, b *bundle.Bundle, serveErr error, active CapturePhase) CaptureSessionResult {
	if b != nil {
		out.Case = caseView(out.CasePath, b)
		out.Connections = len(b.Manifest.Sources)
		if snapshot := b.Observation; snapshot != nil {
			out.Ledger = &FixtureLedger{
				Schema: snapshot.Schema, Profile: snapshot.Profile, Mode: string(snapshot.Mode),
				Processed: len(snapshot.Processed), Records: len(snapshot.Records), Consistent: snapshot.Consistent,
			}
		}
	}
	// A person's cancellation is answered as one even when the serve finalized
	// in an orderly way, as a fixture's and a collector's controlled stops do:
	// the case sealed what arrived before it, not what was declared. Reaching
	// the bound on an admitted execution is not a cancellation.
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		out.State = Cancelled
		out.Reason = cancelledRefusal.reason
		if b != nil {
			out.Phase = CaptureStopped
		} else {
			out.Phase = CaptureStopping
		}
	case ctx.Err() != nil:
		out.State = Failed
		out.Reason = "the capture stopped at the longest an admitted execution may run"
		out.Phase = CaptureFailed
		if b != nil {
			out.Phase = CaptureStopped
		}
	case serveErr == nil:
		out.State = Completed
		out.Phase = CaptureStopped
	default:
		out.State = Failed
		out.Reason = serveErr.Error()
		if b != nil {
			out.Phase = CaptureStopped
		} else {
			out.Phase = CaptureFailed
		}
	}
	_ = active
	return out
}

// OpenCaptureJournal recovers an interrupted capture read-only.
func (a *App) OpenCaptureJournal(workspace, journalPath string) CaptureJournalResult {
	return run(a, false, false, func(context.Context) CaptureJournalResult {
		path, ref := resolveWorkspacePath(workspace, journalPath)
		if path == "" {
			return CaptureJournalResult{State: ref.state, Reason: ref.reason}
		}
		summary, err := operation.CaptureJournalStatus(path)
		if err != nil {
			return CaptureJournalResult{State: Failed, Reason: err.Error(), Phase: CaptureFailed}
		}
		phase := CaptureStopped
		if summary.Recovered || summary.State != capturejournal.Finalized {
			phase = CaptureStopped
		}
		return CaptureJournalResult{State: Completed, Phase: phase, Journal: &summary, Path: journalPath}
	})
}

// FinalizeCaptureRequest names a staged source collection — the folder it
// staged and the receipt it wrote beside it — and the case it becomes. It
// carries no plan: the collection is imported under the plan its receipt
// records.
type FinalizeCaptureRequest struct {
	Workspace         string `json:"workspace"`
	Project           string `json:"project,omitzero"`
	Folder            string `json:"folder"`
	CollectionReceipt string `json:"collection_receipt"`
	OutputName        string `json:"output_name"`
	ReceiptName       string `json:"receipt_name,omitzero"`
	RegisterInProject bool   `json:"register_in_project,omitzero"`
	CaseTitle         string `json:"case_title,omitzero"`
	CaseOwner         string `json:"case_owner,omitzero"`
	CaseVersion       string `json:"case_version,omitzero"`
}

// FinalizeCaptureImport imports a staged collection into a new verified case
// through the operation `readmit import --collection` uses: under the plan its
// receipt records, and only when the receipt says the collection completed
// and the folder holds exactly what it staged. It registers the case on the
// project when asked, by the same flow CommitImport uses, and then offers
// exploration binding.
func (a *App) FinalizeCaptureImport(request FinalizeCaptureRequest) ImportCommitResult {
	return runNamed[ImportCommitResult, *ImportCommitResult](a, profiles["FinalizeCaptureImport"], func(ctx context.Context) ImportCommitResult {
		folder, ref := resolveWorkspacePath(request.Workspace, request.Folder)
		if folder == "" {
			return ImportCommitResult{State: ref.state, Reason: ref.reason}
		}
		collection, ref := resolveWorkspacePath(request.Workspace, request.CollectionReceipt)
		if collection == "" {
			return ImportCommitResult{State: ref.state, Reason: ref.reason}
		}
		into := importCommit{
			workspace: request.Workspace, project: request.Project,
			outputName: request.OutputName, receiptName: request.ReceiptName,
			register: request.RegisterInProject, title: request.CaseTitle, owner: request.CaseOwner, version: request.CaseVersion,
		}
		return a.commitImport(ctx, into, func(casePath, receiptPath string) (*bundle.Bundle, error) {
			b, _, err := operation.ImportCollectionCommit(ctx, collection, folder, casePath, receiptPath)
			return b, err
		})
	})
}

func (a *App) capturePreview(request CaptureRequest) (operation.CapturePreview, error) {
	switch request.Kind {
	case "listen":
		return operation.PreviewListen(listenConfig(request, request.OutputName, observationName(request)))
	case "collect":
		cfg, err := a.collectConfig(request, request.OutputName)
		if err != nil {
			return operation.CapturePreview{}, err
		}
		return operation.PreviewCollect(cfg)
	default:
		return operation.CapturePreview{}, errors.New("capture kind must be collect or listen")
	}
}

// observationName is the ledger a fixture request names, or the one beside
// its case when it names none.
func observationName(request CaptureRequest) string {
	if request.ObservationName != "" {
		return request.ObservationName
	}
	return request.OutputName + "-observation.json"
}

// listenConfig is the fixture configuration a request states. A bound the
// request left out, or stated as nothing usable, is the operation's default,
// as the command line's flags default it.
func listenConfig(request CaptureRequest, output, observationPath string) operation.ListenConfig {
	return operation.ListenConfig{
		Address:         request.Address,
		ApprovedBind:    request.ApprovedBind,
		Mode:            observation.Mode(request.FixtureMode),
		OutputPath:      output,
		ObservationPath: observationPath,
		MaxFrameBytes:   positiveOr(request.MaxFrameBytes, operation.DefaultMaxFrameBytes),
		IdleTimeout:     durationOr(request.IdleTimeout, operation.DefaultIdleTimeout),
		MaxMessages:     request.MaxMessages,
	}
}

// positiveOr is a count a request states, or the fallback when it states
// none or one that is not positive.
func positiveOr(stated, fallback int) int {
	if stated > 0 {
		return stated
	}
	return fallback
}

// durationOr reads a duration a request states, or the fallback when it
// states none, one that does not parse, or one that is not positive.
func durationOr(stated string, fallback time.Duration) time.Duration {
	if d, err := time.ParseDuration(stated); err == nil && d > 0 {
		return d
	}
	return fallback
}

// collectConfig is the collector configuration a request states. The policy
// is the one the request carries, or the document it names, which the
// operation reads once the address is approved.
func (a *App) collectConfig(request CaptureRequest, output string) (operation.CollectConfig, error) {
	cfg := operation.CollectConfig{
		Address:            request.Address,
		ApprovedBind:       request.ApprovedBind,
		OutputPath:         output,
		MaxFrameBytes:      positiveOr(request.MaxFrameBytes, operation.DefaultMaxFrameBytes),
		IdleTimeout:        durationOr(request.IdleTimeout, operation.DefaultIdleTimeout),
		ApplicationTimeout: durationOr(request.ApplicationTimeout, operation.DefaultApplicationTimeout),
		MaxMessages:        request.MaxMessages,
		MaxConnections:     request.MaxConnections,
		MaxSessions:        request.MaxSessions,
		MaxCaptureBytes:    request.MaxCaptureBytes,
		TLSKeyReference:    request.TLSKeyReference,
	}
	switch {
	case request.Policy != nil:
		cfg.Policy = request.Policy
	case request.PolicyFile != "":
		path, ref := resolveWorkspacePath(request.Workspace, request.PolicyFile)
		if path == "" {
			return operation.CollectConfig{}, errors.New(ref.reason)
		}
		cfg.PolicyPath = path
	}
	if request.JournalName != "" {
		path, ref := resolveWorkspacePath(request.Workspace, request.JournalName)
		if path == "" {
			return operation.CollectConfig{}, errors.New(ref.reason)
		}
		cfg.JournalPath = path
	}
	if request.TLSCertificateFile != "" {
		path, ref := resolveWorkspacePath(request.Workspace, request.TLSCertificateFile)
		if path == "" {
			return operation.CollectConfig{}, errors.New(ref.reason)
		}
		cfg.TLSCertificatePath = path
	}
	if request.SecretsFile != "" {
		path, ref := resolveWorkspacePath(request.Workspace, request.SecretsFile)
		if path == "" {
			return operation.CollectConfig{}, errors.New(ref.reason)
		}
		cfg.SecretsFile = path
	}
	if request.ClientCAFile != "" {
		path, ref := resolveWorkspacePath(request.Workspace, request.ClientCAFile)
		if path == "" {
			return operation.CollectConfig{}, errors.New(ref.reason)
		}
		cfg.ClientCAPath = path
	}
	return cfg, nil
}
