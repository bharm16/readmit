package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/project"
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
	return run(a, true, false, func(ctx context.Context) (out SourceAccessResult) {
		guard, _ := a.selectedOperation()
		settle, admissionErr := guard.AdmitContext(ctx, "execute")
		if admissionErr != nil {
			return SourceAccessResult{State: PermissionDenied, Reason: admissionErr.Error()}
		}
		defer func() {
			if err := settle(); err != nil {
				out.State = Failed
				out.Reason = "runner settlement failed; reconcile the retained admission before new work"
			}
		}()
		source, options, err := a.sourceWork(ctx, request)
		if err != nil {
			return SourceAccessResult{State: Failed, Reason: err.Error()}
		}
		access, err := operation.SourceDiagnose(ctx, source, options)
		if err != nil {
			if ctx.Err() != nil {
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
	return runNamed[SourceCollectionResult, *SourceCollectionResult](a, "collect", true, true, func(ctx context.Context) (out SourceCollectionResult) {
		guard, _ := a.selectedOperation()
		settle, admissionErr := guard.AdmitContext(ctx, "execute")
		if admissionErr != nil {
			return SourceCollectionResult{State: PermissionDenied, Reason: admissionErr.Error()}
		}
		defer func() {
			if err := settle(); err != nil {
				out.State = Failed
				out.Reason = "runner settlement failed; reconcile the retained admission before new work"
			}
		}()
		source, options, err := a.sourceWork(ctx, request)
		if err != nil {
			return SourceCollectionResult{State: Failed, Reason: err.Error()}
		}
		root := request.Workspace
		if root == "" {
			return SourceCollectionResult{State: Failed, Reason: "a workspace is required to stage collected evidence"}
		}
		outputName := request.OutputName
		if outputName == "" {
			outputName = "collected"
		}
		receiptName := request.ReceiptName
		if receiptName == "" {
			receiptName = "collection.json"
		}
		output := filepath.Join(root, outputName)
		receipt := filepath.Join(root, receiptName)
		collection, err := operation.SourceCollect(ctx, source, output, receipt, options)
		if err != nil && !errors.Is(err, evidencesource.ErrIncomplete) {
			if ctx.Err() != nil {
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
			state = Failed
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
	options := evidencesource.Options{Plan: plan}
	if request.PolicyFile != "" {
		path, ref := resolveWorkspacePath(request.Workspace, request.PolicyFile)
		if path == "" {
			return evidencesource.Source{}, evidencesource.Options{}, errors.New(ref.reason)
		}
		policy, err := operation.ReadSendPolicy(path)
		if err != nil {
			return evidencesource.Source{}, evidencesource.Options{}, err
		}
		options.Policy = &policy
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
			return ReceiverPolicyResult{State: Failed, Reason: err.Error()}
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
}

func (r *CaptureSessionResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

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
	return runNamed[CaptureSessionResult, *CaptureSessionResult](a, "capture", true, true, func(ctx context.Context) (out CaptureSessionResult) {
		guard, _ := a.selectedOperation()
		settle, admissionErr := guard.AdmitContext(ctx, "execute")
		if admissionErr != nil {
			return CaptureSessionResult{State: PermissionDenied, Reason: admissionErr.Error(), Phase: CaptureFailed}
		}
		defer func() {
			if err := settle(); err != nil {
				out.State = Failed
				out.Reason = "runner settlement failed; reconcile the retained admission before new work"
			}
		}()
		bounded, cancel := context.WithTimeout(ctx, operationguard.MaxDuration)
		defer cancel()

		preview, err := a.capturePreview(request)
		if err != nil {
			return CaptureSessionResult{State: Failed, Reason: err.Error(), Phase: CaptureFailed}
		}
		root := request.Workspace
		if root == "" {
			return CaptureSessionResult{State: Failed, Reason: "a workspace is required", Phase: CaptureFailed}
		}
		output := filepath.Join(root, request.OutputName)

		switch request.Kind {
		case "listen":
			obsName := request.ObservationName
			if obsName == "" {
				obsName = request.OutputName + "-observation.json"
			}
			cfg := operation.ListenConfig{
				Address:         request.Address,
				ApprovedBind:    request.ApprovedBind,
				Mode:            observation.Mode(request.FixtureMode),
				OutputPath:      output,
				ObservationPath: filepath.Join(root, obsName),
				MaxFrameBytes:   request.MaxFrameBytes,
				MaxMessages:     request.MaxMessages,
			}
			if request.IdleTimeout != "" {
				if d, err := time.ParseDuration(request.IdleTimeout); err == nil {
					cfg.IdleTimeout = d
				}
			}
			result, serveErr := operation.StartListen(bounded, cfg)
			out = CaptureSessionResult{
				BoundAddress:    result.BoundAddress,
				CasePath:        request.OutputName,
				ObservationPath: obsName,
				Preview:         &preview,
			}
			if result.Observation != nil {
				out.Received = len(result.Observation.Processed)
			}
			return a.finishCapture(bounded, out, result.Bundle, serveErr, CaptureListening)
		case "collect":
			cfg, err := a.collectConfig(request, output)
			if err != nil {
				return CaptureSessionResult{State: Failed, Reason: err.Error(), Phase: CaptureFailed, Preview: &preview}
			}
			result, serveErr := operation.StartCollect(bounded, cfg)
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
			return a.finishCapture(bounded, out, result.Bundle, serveErr, CaptureCollecting)
		default:
			return CaptureSessionResult{State: Failed, Reason: "capture kind must be collect or listen", Phase: CaptureFailed}
		}
	})
}

func (a *App) finishCapture(ctx context.Context, out CaptureSessionResult, b *bundle.Bundle, serveErr error, active CapturePhase) CaptureSessionResult {
	if b != nil {
		counts := b.Counts()
		out.Case = &Case{
			Name:             out.CasePath,
			Identity:         b.Identity,
			Schema:           b.Manifest.Schema,
			Provenance:       string(b.Manifest.Provenance.Mode),
			Sources:          len(b.Manifest.Sources),
			Occurrences:      len(b.Events),
			Messages:         counts[bundle.Message],
			Acknowledgements: counts[bundle.Acknowledgement],
			Unparsed:         counts[bundle.Unparsed],
		}
		out.Connections = len(b.Manifest.Sources)
	}
	switch {
	case serveErr == nil:
		out.State = Completed
		out.Phase = CaptureStopped
	case ctx.Err() != nil:
		out.State = Cancelled
		out.Reason = cancelledRefusal.reason
		if b != nil {
			out.Phase = CaptureStopped
		} else {
			out.Phase = CaptureStopping
		}
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

// FinalizeCaptureImport imports a collected folder into a new verified case and
// optionally registers it on the project, then offers exploration binding.
type FinalizeCaptureRequest struct {
	Workspace         string         `json:"workspace"`
	Project           string         `json:"project,omitzero"`
	Folder            string         `json:"folder"`
	OutputName        string         `json:"output_name"`
	ReceiptName       string         `json:"receipt_name,omitzero"`
	Plan              *importer.Plan `json:"plan,omitzero"`
	RegisterInProject bool           `json:"register_in_project,omitzero"`
	CaseTitle         string         `json:"case_title,omitzero"`
	CaseOwner         string         `json:"case_owner,omitzero"`
	CaseVersion       string         `json:"case_version,omitzero"`
}

// FinalizeCaptureImport commits staged collected material through the shared
// import operation and links project registration when asked.
func (a *App) FinalizeCaptureImport(request FinalizeCaptureRequest) ImportCommitResult {
	return runNamed[ImportCommitResult, *ImportCommitResult](a, "import", true, true, func(ctx context.Context) ImportCommitResult {
		folder, ref := resolveWorkspacePath(request.Workspace, request.Folder)
		if folder == "" {
			return ImportCommitResult{State: ref.state, Reason: ref.reason}
		}
		plan := operation.DefaultImportPlan()
		if request.Plan != nil {
			plan = *request.Plan
		}
		targetDir := request.Workspace
		if request.Project != "" {
			targetDir = request.Project
		}
		if targetDir == "" {
			return ImportCommitResult{State: Failed, Reason: "a workspace or project is required"}
		}
		if request.OutputName == "" {
			return ImportCommitResult{State: Failed, Reason: "an output case name is required"}
		}
		casePath := filepath.Join(targetDir, request.OutputName)
		receiptName := request.ReceiptName
		if receiptName == "" {
			receiptName = request.OutputName + "-import.json"
		}
		receiptPath := filepath.Join(targetDir, receiptName)
		b, _, err := operation.ImportPlanCommit(ctx, plan, nil, []string{folder}, nil, casePath, receiptPath)
		if err != nil {
			if ctx.Err() != nil {
				return ImportCommitResult{State: Cancelled, Reason: cancelledRefusal.reason}
			}
			if errors.Is(err, os.ErrPermission) {
				return ImportCommitResult{State: PermissionDenied, Reason: "this account cannot write case evidence into the selected directory"}
			}
			return ImportCommitResult{State: Failed, Reason: err.Error()}
		}
		counts := b.Counts()
		c := &Case{
			Name:             request.OutputName,
			Identity:         b.Identity,
			Schema:           b.Manifest.Schema,
			Provenance:       string(b.Manifest.Provenance.Mode),
			Sources:          len(b.Manifest.Sources),
			Occurrences:      len(b.Events),
			Messages:         counts[bundle.Message],
			Acknowledgements: counts[bundle.Acknowledgement],
			Unparsed:         counts[bundle.Unparsed],
		}
		registered := false
		var projDoc *project.Document
		if request.RegisterInProject && request.Project != "" {
			title := request.CaseTitle
			if title == "" {
				title = request.OutputName
			}
			_, regErr := operation.RegisterCase(request.Project, request.OutputName, operation.CaseRegistration{
				Title:            title,
				Owner:            request.CaseOwner,
				InterfaceVersion: request.CaseVersion,
			})
			if regErr != nil {
				return ImportCommitResult{
					State:       Completed,
					Reason:      "the case and receipt were written, but the case was not registered: " + regErr.Error(),
					Case:        c,
					CasePath:    casePath,
					ReceiptPath: receiptPath,
				}
			}
			registered = true
			if openedProj, openErr := project.Open(request.Project); openErr == nil {
				projDoc = &openedProj.Document
			}
		}
		return ImportCommitResult{
			State:       Completed,
			Case:        c,
			CasePath:    casePath,
			ReceiptPath: receiptPath,
			Registered:  registered,
			Project:     projDoc,
		}
	})
}

func (a *App) capturePreview(request CaptureRequest) (operation.CapturePreview, error) {
	switch request.Kind {
	case "listen":
		cfg := operation.ListenConfig{
			Address:         request.Address,
			ApprovedBind:    request.ApprovedBind,
			Mode:            observation.Mode(request.FixtureMode),
			OutputPath:      request.OutputName,
			ObservationPath: request.ObservationName,
			MaxFrameBytes:   request.MaxFrameBytes,
			MaxMessages:     request.MaxMessages,
		}
		if cfg.ObservationPath == "" {
			cfg.ObservationPath = request.OutputName + "-observation.json"
		}
		if request.IdleTimeout != "" {
			if d, err := time.ParseDuration(request.IdleTimeout); err == nil {
				cfg.IdleTimeout = d
			}
		}
		return operation.PreviewListen(cfg)
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

func (a *App) collectConfig(request CaptureRequest, output string) (operation.CollectConfig, error) {
	policy, err := a.resolvePolicy(request)
	if err != nil {
		return operation.CollectConfig{}, err
	}
	cfg := operation.CollectConfig{
		Address:         request.Address,
		ApprovedBind:    request.ApprovedBind,
		Policy:          policy,
		OutputPath:      output,
		MaxFrameBytes:   request.MaxFrameBytes,
		MaxMessages:     request.MaxMessages,
		MaxConnections:  request.MaxConnections,
		MaxSessions:     request.MaxSessions,
		MaxCaptureBytes: request.MaxCaptureBytes,
		TLSKeyReference: request.TLSKeyReference,
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
	if request.IdleTimeout != "" {
		if d, err := time.ParseDuration(request.IdleTimeout); err == nil {
			cfg.IdleTimeout = d
		}
	}
	if request.ApplicationTimeout != "" {
		if d, err := time.ParseDuration(request.ApplicationTimeout); err == nil {
			cfg.ApplicationTimeout = d
		}
	}
	return cfg, nil
}

func (a *App) resolvePolicy(request CaptureRequest) (collection.Policy, error) {
	if request.Policy != nil {
		policy := *request.Policy
		if err := policy.Validate(); err != nil {
			return collection.Policy{}, err
		}
		return policy, nil
	}
	if request.PolicyFile == "" {
		return collection.Policy{}, errors.New("a receiver policy is required")
	}
	path, ref := resolveWorkspacePath(request.Workspace, request.PolicyFile)
	if path == "" {
		return collection.Policy{}, errors.New(ref.reason)
	}
	return operation.ReceiverPolicyRead(path)
}
