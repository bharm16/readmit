package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/receiver"
)

// A capture session (#552) collects from one saved capture source into the
// project's own area, .readmit/captures/<session>, and only a capture that
// finished is published: its case is moved into a generated entry of the
// project and registered with its name, as an import is. Finishing is a
// controlled stop, so the journal reads finalized; cancelling stops the work
// and keeps what arrived under the session, unregistered. A finalization that
// fails keeps the session for a retry, which moves and registers, or imports
// the staged collection, and never collects again.

// CaptureSessionSchema is the contract of a session's own record.
const CaptureSessionSchema = "readmit-capture-session/v1"

// capturesFolder holds every session of a project.
const capturesFolder = "captures"

// MaxCaptureMessages bounds the recent messages a capture's progress keeps.
const MaxCaptureMessages = 200

// CaptureOutcome is how a capture from a saved source ended.
type CaptureOutcome string

const (
	// CaptureFinished: the capture stopped in a controlled way and its case
	// is registered.
	CaptureFinished CaptureOutcome = "finished"
	// CaptureCancelled: a person cancelled it; what arrived is kept under the
	// session and nothing is registered.
	CaptureCancelled CaptureOutcome = "cancelled"
	// CaptureInterrupted: it stopped at a bound or on an error, or its
	// process ended while it ran; what arrived is kept, unregistered.
	CaptureInterrupted CaptureOutcome = "interrupted"
	// CaptureFinalizeFailed: it finished, and its case could not be
	// published; RetryCaptureFinalization publishes it.
	CaptureFinalizeFailed CaptureOutcome = "finalize-failed"
	// CaptureRunning: a session this process is serving now.
	CaptureRunning CaptureOutcome = "running"
)

// CaptureLimits bound one capture beyond its source's saved settings. Zero
// keeps the source's own.
type CaptureLimits struct {
	MaxMessages     int `json:"max_messages,omitzero"`
	MaxCaptureBytes int `json:"max_capture_bytes,omitzero"`
}

// captureRecord is a session's own record, beside what it collected.
type captureRecord struct {
	Schema      string            `json:"schema"`
	ID          string            `json:"id"`
	Intent      string            `json:"intent"`
	Name        string            `json:"name"`
	Source      ItemRef           `json:"source"`
	SourceName  string            `json:"source_name"`
	SourceType  CaptureSourceType `json:"source_type"`
	Environment *ItemRef          `json:"environment,omitzero"`
	StartedAt   string            `json:"started_at"`
	EndedAt     string            `json:"ended_at,omitzero"`
	State       CaptureOutcome    `json:"state"`
	Reason      string            `json:"reason,omitzero"`
	Entry       string            `json:"entry,omitzero"`
	Received    int               `json:"received"`
}

const sessionDocument = "session.json"

var sessionFile = artifactdir.Document{MaxBytes: 64 << 10}

func readCaptureSession(folder string) (captureRecord, error) {
	data, err := sessionFile.Read(filepath.Join(folder, sessionDocument))
	if err != nil {
		return captureRecord{}, err
	}
	var record captureRecord
	if err := json.Unmarshal(data, &record, json.RejectUnknownMembers(true)); err != nil || record.Schema != CaptureSessionSchema {
		return captureRecord{}, errors.New("the capture session's record cannot be read")
	}
	return record, nil
}

func writeCaptureSession(folder string, record captureRecord) error {
	record.Schema = CaptureSessionSchema
	data, err := json.Marshal(record, json.Deterministic(true))
	if err != nil {
		return err
	}
	return sessionFile.Replace(filepath.Join(folder, sessionDocument), append(data, '\n'))
}

// capturedSource is a saved capture source as a capture reads it.
type capturedSource struct {
	ref   ItemRef
	name  string
	draft *CaptureSourceDraft
}

// savedCaptureSource reads the exact saved revision of a capture source.
func (a *App) savedCaptureSource(ctx context.Context, request RequestContext, ref ItemRef) (*capturedSource, refusal) {
	loaded, item, refused := a.catalogItem(ctx, request, ref, false)
	if loaded == nil {
		return nil, refusal{refused.State, refused.Reason}
	}
	if ref.Kind != SourceItem {
		return nil, refusal{Failed, "a capture starts from a saved capture source"}
	}
	if item.Availability != ItemAvailable {
		return nil, refusal{Failed, "the capture source is " + string(item.Availability) + ": " + item.Reason}
	}
	if ref.Revision != "" && ref.Revision != item.Ref.Revision {
		return nil, refusal{Failed, "the capture source was saved again since this capture was set up; review it again"}
	}
	paths, availability, reason := loaded.backing(loaded.document.Items[loaded.document.Find(item.Ref.ID)])
	if availability != ItemAvailable {
		return nil, refusal{Failed, reason}
	}
	draft, err := readCaptureSource(paths)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	return &capturedSource{ref: item.Ref, name: item.Name, draft: draft}, refusal{}
}

// startSourceCapture is StartCapture from a saved capture source.
func (a *App) startSourceCapture(ctx context.Context, request CaptureRequest) CaptureSessionResult {
	fail := func(state State, reason string) CaptureSessionResult {
		return CaptureSessionResult{State: state, Reason: reason, Phase: CaptureFailed}
	}
	root, declined := a.projectRoot(ctx, request.Context)
	if root == "" {
		return fail(declined.state, declined.reason)
	}
	name := strings.TrimSpace(request.Name)
	if !catalog.ValidName(name) {
		return fail(Failed, nameRule)
	}
	if !catalog.ValidToken(request.IntentID) {
		return fail(Failed, "a capture names the click it was started by")
	}
	session := intentFolder("capture\x00" + request.IntentID)
	folder, err := managedFolder(root, catalog.Folder, capturesFolder, session)
	if err != nil {
		return fail(Failed, "the project's capture area cannot be written")
	}
	if held, err := readCaptureSession(folder); err == nil {
		// This click already started a capture: it is answered, never
		// served again.
		out := a.sessionResult(ctx, request.Context, held)
		out.Replayed = true
		return out
	}
	if err := clearFolder(folder); err != nil {
		return fail(Failed, "the project's capture area cannot be written")
	}
	source, declined := a.savedCaptureSource(ctx, request.Context, *request.Source)
	if source == nil {
		return fail(declined.state, declined.reason)
	}
	if source.draft.Type == APISource {
		return fail(Failed, apiUnavailable)
	}
	var environment *ItemRef
	// A transfer source reaches off this machine, and the environment the
	// capture names decides where it may: its approved-destination policy
	// governs the source's address exactly as it governs a send. Without
	// one, only a loopback source is reached.
	policyPath := ""
	if request.Environment != nil {
		loaded, item, refused := a.catalogItem(ctx, request.Context, *request.Environment, false)
		if item == nil || request.Environment.Kind != EnvironmentItem {
			return fail(Failed, cmpOr(refused.Reason, "the environment is not one of this project's"))
		}
		environment = &item.Ref
		if paths, availability, _ := loaded.backing(loaded.document.Items[loaded.document.Find(item.Ref.ID)]); availability == ItemAvailable {
			policyPath = paths["policy"]
		}
	}
	started := a.now().UTC()
	record := captureRecord{ID: session, Intent: request.IntentID, Name: name, Source: source.ref, SourceName: source.name,
		SourceType: source.draft.Type, Environment: environment, StartedAt: catalog.Stamp(started), State: CaptureRunning}
	if err := writeCaptureSession(folder, record); err != nil {
		return fail(Failed, "the capture session could not be recorded")
	}
	a.captureMu.Lock()
	a.captureProgress = &CaptureProgress{Kind: string(source.draft.Type), Session: session, Name: name, Source: &source.ref, SourceName: source.name,
		SourceType: source.draft.Type, StartedAt: stampedTime(started), Messages: []CaptureMessage{}}
	a.captureStop, a.captureFinishing, a.captureConnections = nil, false, nil
	a.captureMu.Unlock()
	defer a.endCaptureProgress()

	out := CaptureSessionResult{Session: session}
	var b *bundleResult
	switch source.draft.Type {
	case MLLPListenerSource:
		b, err = a.serveListener(ctx, root, folder, name, source.draft, request.Limits)
	default:
		// A capture reaches what its source declares, named by the case it
		// writes, as soon as it starts reading it.
		where := source.draft.Evidence.Address
		if where == "" {
			where = source.draft.Evidence.Root
		}
		a.reach(reachingTarget{name: name, kind: ConnectionSource, destination: where})
		b, err = a.collectFolderSource(ctx, folder, source.draft, policyPath)
	}
	out.BoundAddress, out.Received = b.bound, b.received
	out.Journal = b.journal
	out = a.finishCapture(ctx, out, b.bundle, err)
	record.Received, record.EndedAt = b.received, catalog.Stamp(a.now())
	switch {
	case out.State == Cancelled:
		record.State, record.Reason = CaptureCancelled, out.Reason
	case b.collected && ctx.Err() == nil && out.State != Completed:
		// The source was read whole and its import failed: what it staged
		// is kept, and a retry imports it again without reading the source.
		_ = os.RemoveAll(filepath.Join(folder, "case"))
		_ = os.Remove(filepath.Join(folder, "receipt.json"))
		record.State, record.Reason = CaptureFinalizeFailed, "the capture read its source and its case could not be written: "+out.Reason
		out.State, out.Reason = Failed, record.Reason
	case out.State != Completed:
		record.State, record.Reason = CaptureInterrupted, out.Reason
		if b.bundle == nil && !b.started {
			// Nothing was served or collected: the capture never began.
			record.State = CaptureInterrupted
		}
	default:
		record.State = CaptureFinalizeFailed
		destination, declined := caseEntryRule.destination(root, "")
		if declined.state == "" {
			record.Entry = destination.Name
			if err = writeCaptureSession(folder, record); err == nil {
				_, err = publishCase(root, folder, record.Entry, name)
			}
		} else {
			err = errors.New(declined.reason)
		}
		if err != nil {
			record.Reason = "the capture finished and its case could not be published: " + err.Error()
		} else {
			record.State, record.Reason = CaptureFinished, ""
		}
	}
	_ = writeCaptureSession(folder, record)
	result := a.sessionResult(ctx, request.Context, record)
	result.BoundAddress, result.Journal, result.Phase = out.BoundAddress, out.Journal, out.Phase
	if record.State == CaptureCancelled || record.State == CaptureInterrupted {
		result.State, result.Reason = out.State, out.Reason
	}
	return result
}

func cmpOr(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// bundleResult is what one capture's collection produced.
type bundleResult struct {
	bound    string
	received int
	started  bool
	// collected says a folder or transfer source was read whole and staged:
	// anything that failed after it is its import.
	collected bool
	bundle    *bundle.Bundle
	journal   *capturejournal.Summary
}

// serveListener serves a saved MLLP listener into the session folder until it
// is finished, cancelled or bounded.
func (a *App) serveListener(ctx context.Context, root, folder, name string, source *CaptureSourceDraft, limits *CaptureLimits) (*bundleResult, error) {
	listener := source.Listener
	idle, err := time.ParseDuration(listener.IdleTimeout)
	if err != nil || idle <= 0 {
		idle = operation.DefaultIdleTimeout
	}
	cfg := operation.CollectConfig{
		Address: listener.Address(), ApprovedBind: listener.AllowRemote, Policy: source.Responder,
		OutputPath: filepath.Join(folder, "case"), JournalPath: filepath.Join(folder, "journal"),
		MaxFrameBytes: operation.DefaultMaxFrameBytes, IdleTimeout: idle, ApplicationTimeout: operation.DefaultApplicationTimeout,
		MaxMessages: listener.MessageLimit, MaxConnections: listener.ConnectionLimit,
		TLSKeyReference: listener.TLSKeyReference,
	}
	if limits != nil {
		cfg.MaxMessages = cmpPositive(limits.MaxMessages, cfg.MaxMessages)
		cfg.MaxCaptureBytes = limits.MaxCaptureBytes
	}
	for _, file := range []struct {
		name string
		into *string
	}{{listener.TLSCertificate, &cfg.TLSCertificatePath}, {listener.SecretsFile, &cfg.SecretsFile}, {listener.ClientCA, &cfg.ClientCAPath}} {
		if file.name == "" {
			continue
		}
		path, err := artifactpath.File(root, file.name)
		if err != nil {
			return &bundleResult{}, errors.New("a TLS file the listener names is not a file of this project")
		}
		*file.into = path
	}
	report := a.reportCaptureProgress("collect")
	// A capture reaches whatever connects to the port it listens on: it is
	// named by the case it writes, at the address it bound.
	cfg.Listening = func(bound string, _ collection.Policy) error {
		a.reach(reachingTarget{name: name, kind: ConnectionSource, destination: bound})
		return report(bound)
	}
	cfg.Started = func(stop func()) {
		a.captureMu.Lock()
		a.captureStop = stop
		finishing := a.captureFinishing
		a.captureMu.Unlock()
		if finishing {
			stop()
		}
	}
	cfg.Observe = func(seen receiver.FrameSeen) {
		a.captureMu.Lock()
		defer a.captureMu.Unlock()
		if a.captureProgress == nil {
			return
		}
		a.captureProgress.Received++
		if a.captureConnections == nil {
			a.captureConnections = map[string]int{}
		}
		connection, known := a.captureConnections[seen.Session]
		if !known {
			connection = len(a.captureConnections) + 1
			a.captureConnections[seen.Session] = connection
		}
		a.captureProgress.Messages = append(a.captureProgress.Messages, CaptureMessage{At: catalog.Stamp(seen.At), Type: seen.MessageType, Connection: connection})
		if extra := len(a.captureProgress.Messages) - MaxCaptureMessages; extra > 0 {
			a.captureProgress.Messages = slices.Delete(a.captureProgress.Messages, 0, extra)
		}
	}
	result, serveErr := operation.StartCollect(ctx, cfg)
	out := &bundleResult{bound: result.BoundAddress, started: result.BoundAddress != "", journal: result.Journal}
	if result.Journal != nil {
		out.received = result.Journal.Received
	}
	if result.Bundle != nil {
		out.bundle = result.Bundle
	}
	return out, serveErr
}

func cmpPositive(stated, fallback int) int {
	if stated > 0 {
		return stated
	}
	return fallback
}

// collectFolderSource collects a local folder or transfer source into the
// session folder and imports what it staged, under the plan its receipt
// records, into the session's case.
func (a *App) collectFolderSource(ctx context.Context, folder string, source *CaptureSourceDraft, policyPath string) (*bundleResult, error) {
	out := &bundleResult{started: true}
	staged, receipt := filepath.Join(folder, "collected"), filepath.Join(folder, "collection.json")
	options, err := operation.SourceOptions(*source.Plan, policyPath)
	if err != nil {
		return out, err
	}
	collected, err := operation.SourceCollect(ctx, *source.Evidence, staged, receipt, options)
	out.received = collected.Totals.Collected
	if err != nil {
		return out, err
	}
	out.collected = true
	if a.saveFault != nil {
		if err := a.saveFault("capture-import"); err != nil {
			return out, err
		}
	}
	b, _, err := operation.ImportCollectionCommit(ctx, receipt, staged, filepath.Join(folder, "case"), filepath.Join(folder, "receipt.json"))
	out.bundle = b
	return out, err
}

// FinishCapture asks the running capture to finish: it stops accepting,
// finishes the frames it is answering and seals its case, which StartCapture
// then publishes and registers. It does not take the operation slot. A
// capture that collects a folder or transfer source finishes when its source
// is read, so it is not asked.
func (a *App) FinishCapture() CaptureProgressResult {
	a.captureMu.Lock()
	if a.captureProgress == nil || a.captureProgress.Session == "" {
		a.captureMu.Unlock()
		return CaptureProgressResult{State: Empty}
	}
	if a.captureProgress.SourceType != MLLPListenerSource {
		a.captureMu.Unlock()
		return CaptureProgressResult{State: Failed, Reason: "a folder or transfer capture finishes when it has read its source"}
	}
	a.captureFinishing, a.captureProgress.Finishing = true, true
	stop := a.captureStop
	progress := *a.captureProgress
	progress.Messages = slices.Clone(progress.Messages)
	a.captureMu.Unlock()
	if stop != nil {
		stop()
	}
	return CaptureProgressResult{State: Completed, Progress: &progress}
}

// CaptureSessionRequest names one capture session of the project.
type CaptureSessionRequest struct {
	Context RequestContext `json:"context"`
	Session string         `json:"session"`
}

// RetryCaptureFinalization publishes a capture whose finalization failed:
// it imports the staged collection if its case was not written, then moves
// the case into the project and registers it. It never listens, collects or
// sends again, and refuses any session that did not fail finalization.
func (a *App) RetryCaptureFinalization(request CaptureSessionRequest) ImportCaseResult {
	return runNamed[ImportCaseResult, *ImportCaseResult](a, profiles["RetryCaptureFinalization"], func(ctx context.Context) ImportCaseResult {
		result := ImportCaseResult{Context: request.Context}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if !stagedID(request.Session) {
			result.refuse(Failed, "the project holds no such capture session")
			return result
		}
		folder := filepath.Join(root, catalog.Folder, capturesFolder, request.Session)
		record, err := readCaptureSession(folder)
		if err != nil {
			result.refuse(Failed, "the project holds no such capture session")
			return result
		}
		if record.State != CaptureFinalizeFailed {
			result.refuse(Failed, "only a capture whose finalization failed is finalized again")
			return result
		}
		casePath := filepath.Join(folder, "case")
		if _, err := os.Lstat(casePath); errors.Is(err, fs.ErrNotExist) && (record.Entry == "" || !regular(filepath.Join(root, record.Entry, "manifest.json"))) {
			collection := filepath.Join(folder, "collection.json")
			if _, _, err := operation.ImportCollectionCommit(ctx, collection, filepath.Join(folder, "collected"), casePath, filepath.Join(folder, "receipt.json")); err != nil {
				if ctx.Err() != nil {
					result.refuse(Cancelled, cancelledRefusal.reason)
					return result
				}
				result.refuse(Failed, "the staged collection could not be imported: "+err.Error())
				result.Operation = request.Session
				return result
			}
		}
		if record.Entry == "" {
			destination, declined := caseEntryRule.destination(root, "")
			if declined.state != "" {
				result.refuse(declined.state, declined.reason)
				return result
			}
			record.Entry = destination.Name
			if err := writeCaptureSession(folder, record); err != nil {
				result.refuse(Failed, "the capture session could not be recorded")
				return result
			}
		}
		if _, err := publishCase(root, folder, record.Entry, record.Name); err != nil {
			result.refuse(Failed, "the case could not be published: "+err.Error())
			result.Operation = request.Session
			return result
		}
		record.State, record.Reason = CaptureFinished, ""
		_ = writeCaptureSession(folder, record)
		ref, declined := a.caseRef(ctx, request.Context, record.Entry)
		if ref == nil {
			result.refuse(declined.state, "the case was published and registered, and "+declined.reason)
			return result
		}
		result.State, result.Case = Completed, ref
		return result
	})
}

// CaptureSessionRow is one capture session as its record and journal read.
type CaptureSessionRow struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Source      ItemRef           `json:"source"`
	SourceName  string            `json:"source_name"`
	SourceType  CaptureSourceType `json:"source_type"`
	Environment *ItemRef          `json:"environment"`
	StartedAt   *string           `json:"started_at"`
	EndedAt     *string           `json:"ended_at"`
	State       CaptureOutcome    `json:"state"`
	Reason      string            `json:"reason,omitzero"`
	Received    int               `json:"received"`
	Recovered   bool              `json:"recovered"`
	Case        *ItemRef          `json:"case"`
	// Retained says a session that did not publish kept a case bundle
	// OpenRetainedCapture opens read-only: a cancelled, interrupted or
	// unfinalized capture whose case folder is there.
	Retained bool `json:"retained"`
}

// CaptureSessionsResult lists a project's capture sessions, newest first.
type CaptureSessionsResult struct {
	State    State               `json:"state"`
	Reason   string              `json:"reason,omitzero"`
	Context  RequestContext      `json:"context"`
	Sessions []CaptureSessionRow `json:"sessions"`
}

func (r *CaptureSessionsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ListCaptureSessions reads every capture session of the project from its
// record and its journal, read-only: nothing is recovered, resumed or sent.
// A session its record says is running, and this process is not serving,
// ended with its process, and reads as interrupted.
func (a *App) ListCaptureSessions(request RequestContext) CaptureSessionsResult {
	return runRead(a, false, func(ctx context.Context) CaptureSessionsResult {
		result := CaptureSessionsResult{Context: request, Sessions: []CaptureSessionRow{}}
		root, declined := a.projectRoot(ctx, request)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		entries, err := os.ReadDir(filepath.Join(root, catalog.Folder, capturesFolder))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			result.refuse(Failed, "the project's capture sessions cannot be read")
			return result
		}
		for _, entry := range entries {
			if !entry.IsDir() || !stagedID(entry.Name()) {
				continue
			}
			record, err := readCaptureSession(filepath.Join(root, catalog.Folder, capturesFolder, entry.Name()))
			if err != nil {
				continue
			}
			result.Sessions = append(result.Sessions, a.sessionRow(ctx, request, root, record))
		}
		slices.SortStableFunc(result.Sessions, func(x, y CaptureSessionRow) int {
			return strings.Compare(cmpOrPtr(y.StartedAt), cmpOrPtr(x.StartedAt))
		})
		result.State = Completed
		if len(result.Sessions) == 0 {
			result.State = Empty
		}
		return result
	})
}

func cmpOrPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// sessionRow reads one session: its record, its journal when it has one,
// and the registered case of a finished one.
func (a *App) sessionRow(ctx context.Context, request RequestContext, root string, record captureRecord) CaptureSessionRow {
	row := CaptureSessionRow{ID: record.ID, Name: record.Name, Source: record.Source, SourceName: record.SourceName, SourceType: record.SourceType,
		Environment: record.Environment, StartedAt: stamped(record.StartedAt), EndedAt: stamped(record.EndedAt), State: record.State,
		Reason: record.Reason, Received: record.Received}
	if row.State == CaptureRunning && !a.serving(record.ID) {
		row.State, row.Reason = CaptureInterrupted, "the application ended while this capture ran"
	}
	if summary, err := capturejournal.Open(filepath.Join(root, catalog.Folder, capturesFolder, record.ID, "journal")); err == nil {
		row.Received, row.Recovered = summary.Received, summary.Recovered
	}
	if record.State == CaptureFinished && record.Entry != "" {
		if ref, _ := a.caseRefRead(ctx, request, record.Entry); ref != nil {
			row.Case = ref
		}
	}
	if retainable(row.State) {
		info, err := os.Lstat(filepath.Join(root, catalog.Folder, capturesFolder, record.ID, "case"))
		row.Retained = err == nil && info.IsDir()
	}
	return row
}

// retainable reports an outcome whose session keeps what it collected,
// unpublished: the case of a finished capture is the registered one.
func retainable(state CaptureOutcome) bool {
	return state == CaptureCancelled || state == CaptureInterrupted || state == CaptureFinalizeFailed
}

// RetainedCaptureResult is the case a capture that did not publish kept, as
// the shared reader verified it. Workspace is the session's own folder and
// Case.Name the case's entry in it, so the window reads its messages, grid
// and occurrences as it reads any case's, by that folder, that name and the
// identity shown; nothing about the project changes.
type RetainedCaptureResult struct {
	State     State          `json:"state"`
	Reason    string         `json:"reason,omitzero"`
	Context   RequestContext `json:"context"`
	Session   string         `json:"session"`
	Workspace string         `json:"workspace,omitzero"`
	Case      *Case          `json:"case,omitzero"`
}

func (r *RetainedCaptureResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// OpenRetainedCapture opens the case a cancelled, interrupted or unfinalized
// capture kept under its session, read-only, through the reader OpenCase
// verifies a case with. It never registers, publishes, resumes or sends: a
// capture whose finalization failed is published by
// RetryCaptureFinalization, and a finished one is opened as the case it
// registered. A session whose case was never sealed is refused with the
// reader's reason.
func (a *App) OpenRetainedCapture(request CaptureSessionRequest) RetainedCaptureResult {
	return runRead(a, false, func(ctx context.Context) RetainedCaptureResult {
		result := RetainedCaptureResult{Context: request.Context, Session: request.Session}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if !stagedID(request.Session) {
			result.refuse(Failed, "the project holds no such capture session")
			return result
		}
		folder := filepath.Join(root, catalog.Folder, capturesFolder, request.Session)
		record, err := readCaptureSession(folder)
		if err != nil {
			result.refuse(Failed, "the project holds no such capture session")
			return result
		}
		state := record.State
		if state == CaptureRunning && !a.serving(record.ID) {
			state = CaptureInterrupted
		}
		switch {
		case state == CaptureFinished:
			result.refuse(Failed, "this capture finished and its case is registered; open that case")
			return result
		case !retainable(state):
			result.refuse(Failed, "this capture is still recording")
			return result
		}
		opened := a.openCase(folder, "case")
		if opened.State != Completed {
			result.refuse(opened.State, "the capture kept no case that opens: "+opened.Reason)
			return result
		}
		result.State, result.Workspace, result.Case = Completed, folder, opened.Case
		return result
	})
}

// serving reports whether this process is serving the session now.
func (a *App) serving(session string) bool {
	a.captureMu.Lock()
	defer a.captureMu.Unlock()
	return a.captureProgress != nil && a.captureProgress.Session == session
}

// caseRefRead is caseRef as a read, recording nothing.
func (a *App) caseRefRead(ctx context.Context, request RequestContext, entry string) (*ItemRef, refusal) {
	loaded, declined := a.loadCatalog(ctx, request, false)
	if loaded == nil {
		return nil, declined
	}
	index := loaded.document.ByEntry(string(CaseItem), entry)
	if index < 0 {
		return nil, refusal{Failed, "the project's catalog does not list the case"}
	}
	item := loaded.read(loaded.document.Items[index])
	return &item.Ref, refusal{}
}

// sessionResult answers a capture session as its record reads.
func (a *App) sessionResult(ctx context.Context, request RequestContext, record captureRecord) CaptureSessionResult {
	out := CaptureSessionResult{State: Completed, Phase: CaptureStopped, Outcome: record.State, Session: record.ID, Received: record.Received, Reason: record.Reason}
	switch record.State {
	case CaptureFinished:
		ref, declined := a.caseRef(ctx, request, record.Entry)
		if ref == nil {
			out.State, out.Reason = declined.state, declined.reason
			return out
		}
		out.CaseRef = ref
	case CaptureCancelled:
		out.State = Cancelled
	case CaptureRunning:
		out.State, out.Phase = Busy, CaptureCollecting
	default:
		out.State = Failed
	}
	return out
}
