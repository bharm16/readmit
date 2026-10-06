package desktop

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// SessionSchema is the versioned contract of one viewer's working session:
// what they had open. It is the third local shell document, beside the recent
// workspace list and the saved filters, and like both of them it is per-viewer
// state rather than evidence: no case, run, result, review or report ever holds
// it. Only navigation identities are retained; source values are never copied
// into it.
//
// A document written by an earlier release may also carry drafts, the note
// edits that release retained here. They are read past and never written
// again: unstored work now lives in the editor draft store
// (readmit-desktop-drafts), and notes are stored in the project.
//
// A new member means a new version string and a reader for both. Nothing is
// added to readmit-filters/v2, readmit-revisions/v1
// or readmit-job/v1, none of which this file touches. See
// [ADR-0003](../../docs/adr/0003-specs-are-strict-json-with-typed-operators.md).
const SessionSchema = "readmit-desktop-session/v3"
const navigationSessionSchema = "readmit-desktop-session/v2"
const legacySessionSchema = "readmit-desktop-session/v1"

const (
	maxSessionBytes = 1 << 18
	maxEntryBytes   = 255
)

// errUnsupportedSession reports a working session written under a contract
// version this release does not read. It is distinct from a document this
// release reads and rejects, so the facade can say which one it was handed.
var errUnsupportedSession = errors.New("unsupported working session document version")

// View is where one viewer was when the window last recorded it: the workspace
// folder they had open, the region that held focus, the case they had selected,
// and the durable run they were watching.
//
// It is restored so a person comes back to what they were doing. Restoring it
// opens folders and reads retained evidence; it never resumes or resends
// anything. The run named here is reopened read-only, exactly as
// OpenDurableRun reopens one.
type View struct {
	Workspace  string          `json:"workspace"`
	Region     string          `json:"region"`
	Case       string          `json:"case"`
	Run        string          `json:"run"`
	Navigation *ViewNavigation `json:"navigation,omitzero"`
}

// ViewNavigation stores bounded identities and layout choices. Saved filters
// are referenced by name; source values, consent and reveal authorization have
// no fields. PHIMasked retains only the protective display preference.
type ViewNavigation struct {
	PHIMasked          bool                  `json:"phi_masked,omitzero"`
	CheckedOccurrences []string              `json:"checked_occurrences,omitzero"`
	FileMessages       []int                 `json:"file_messages,omitzero"`
	ReferenceSelection HL7ReferenceSelection `json:"reference_selection,omitzero"`
	ProjectIdentity    string                `json:"project_identity,omitzero"`
	SourceKind         string                `json:"source_kind,omitzero"`
	FileFormat         string                `json:"file_format,omitzero"`
	FileTerminator     string                `json:"file_terminator,omitzero"`
	Destination        string                `json:"destination"`
	Object             string                `json:"object,omitzero"`
	LocalView          string                `json:"local_view,omitzero"`
	SourceIdentity     string                `json:"source_identity,omitzero"`
	Occurrence         string                `json:"occurrence,omitzero"`
	FieldPath          string                `json:"field_path,omitzero"`
	NodeOffset         int                   `json:"node_offset,omitzero"`
	Filter             string                `json:"filter,omitzero"`
	Sort               string                `json:"sort,omitzero"`
	ScrollTop          int                   `json:"scroll_top,omitzero"`
	ReferencePath      string                `json:"reference_path,omitzero"`
	ReferenceIdentity  string                `json:"reference_identity,omitzero"`
	ReferenceEdition   string                `json:"reference_edition,omitzero"`
	File               string                `json:"file,omitzero"`
	FileIdentity       string                `json:"file_identity,omitzero"`
	FileMessage        int                   `json:"file_message,omitzero"`
}

// WorkingSession reads private navigation without opening a source or resuming
// an effect. A v1 session is adapted in memory without rewriting its bytes.
func (a *App) WorkingSession() SessionResult {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	session, declined := a.retainedSession()
	if declined.state != "" {
		return SessionResult{State: declined.state, Reason: declined.reason}
	}
	return SessionResult{State: Completed, Session: &session}
}

// Session is the whole retained working state of one viewer.
type Session struct {
	Schema string `json:"schema"`
	View   View   `json:"view"`
}

// SessionResult carries one state and the working session as it now stands.
// Session is present whenever the document could be read, so a refused edit
// still reports what stays retained rather than an unstored candidate.
type SessionResult struct {
	State   State    `json:"state"`
	Reason  string   `json:"reason,omitzero"`
	Session *Session `json:"session,omitzero"`
}

// emptySession is a viewer who has had nothing open: a complete document
// rather than a missing one, so reading never writes.
func emptySession() Session { return Session{Schema: SessionSchema} }

// RecordView retains where this viewer is, so an interruption does not also
// lose which workspace, case and run they were looking at. It replaces the
// recorded view.
//
// It writes one small local file and does not claim the operation slot: the
// view worth restoring is the view at the moment of the interruption, and
// refusing to record it because a case is being verified would lose exactly the
// state a crash during that verification needs. Recording is serialized against
// itself, so a reader never observes a partial document.
func (a *App) RecordView(view View) SessionResult {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	session, declined := a.retainedSession()
	if declined.state != "" {
		return SessionResult{State: declined.state, Reason: declined.reason}
	}
	if err := validateView(view); err != nil {
		return a.sessionFailure(refusal{Failed, "the view was not recorded: " + err.Error()})
	}
	session.Schema = SessionSchema
	session.View = view
	return a.storeSession(session)
}

// retainedSession reads the document this viewer's session lives in, through
// the shell document store. A missing file is a viewer who has retained
// nothing, which is a complete document rather than a failure; every other
// refusal is reported so the caller can separate a file this account cannot
// read from one this release cannot read.
func (a *App) retainedSession() (Session, refusal) {
	session, err := readSession(a.documents)
	switch {
	case errors.Is(err, fs.ErrPermission):
		return Session{}, refusal{PermissionDenied, "this account cannot read the retained working session"}
	case errors.Is(err, errUnsupportedSession):
		return Session{}, refusal{Failed, "the retained working session was written by a version this release cannot read"}
	case err != nil:
		return Session{}, refusal{Failed, "the retained working session cannot be read; it is left exactly as written"}
	}
	return session, refusal{}
}

// storeSession installs a complete document and reports what it retained.
func (a *App) storeSession(session Session) SessionResult {
	data, err := encodeSession(session)
	if err != nil {
		return a.sessionFailure(refusal{Failed, "this working session no longer fits the bounded document this release retains; store or discard some of it"})
	}
	if err := a.documents.write(sessionName, data); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return a.sessionFailure(refusal{PermissionDenied, "this account cannot write the retained working session"})
		}
		return a.sessionFailure(refusal{Failed, "the working session could not be retained; an interrupted write may be kept beside it"})
	}
	return SessionResult{State: Completed, Session: &session}
}

// sessionFailure reports the session that stays retained, never the candidate
// that was refused, so the window never shows work as kept that is not.
func (a *App) sessionFailure(failure refusal) SessionResult {
	retained, declined := a.retainedSession()
	if declined.state != "" {
		return SessionResult{State: declined.state, Reason: declined.reason}
	}
	return SessionResult{State: failure.state, Reason: failure.reason, Session: &retained}
}

// readSession treats a missing document as a viewer who has retained nothing
// and returns every other failure, so the caller can say which one it was.
// The reading is the store's one rule: a bounded regular file, never a link.
func readSession(documents ShellDocuments) (Session, error) {
	data, err := documents.read(sessionName, maxSessionBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return emptySession(), nil
	}
	if err != nil {
		return Session{}, err
	}
	return decodeSession(data)
}

// decodeSession reads a retained working session. Unknown members and unknown
// versions are errors; there is no migration and no repair, so a document this
// release cannot read stays exactly as it was written.
//
// The declared contract version is read before the strict decode, for the same
// reason every other document of this product reads it first: a later release
// bumps the version precisely because it adds members. Nothing nested here
// declares an unmarshaler of its own, so the strict pass reaches every member
// of the view. An earlier release's drafts are read past, whatever they hold.
func decodeSession(data []byte) (Session, error) {
	if len(data) > maxSessionBytes {
		return Session{}, errors.New("retained working session exceeds its size limit")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return Session{}, errors.New("invalid retained working session")
	}
	if declared.Schema != SessionSchema && declared.Schema != navigationSessionSchema && declared.Schema != legacySessionSchema {
		return Session{}, errUnsupportedSession
	}
	var session Session
	if declared.Schema == legacySessionSchema {
		var legacy struct {
			Schema string `json:"schema"`
			View   struct {
				Workspace string `json:"workspace"`
				Region    string `json:"region"`
				Case      string `json:"case"`
				Run       string `json:"run"`
			} `json:"view"`
			Drafts jsontext.Value `json:"drafts,omitzero"`
		}
		if err := json.Unmarshal(data, &legacy, json.RejectUnknownMembers(true)); err != nil {
			return Session{}, errors.New("invalid legacy working session")
		}
		session = Session{Schema: SessionSchema, View: View{Workspace: legacy.View.Workspace, Region: legacy.View.Region, Case: legacy.View.Case, Run: legacy.View.Run}}
	} else if err := json.Unmarshal(data, &session, json.RejectUnknownMembers(true)); err != nil {
		return Session{}, errors.New("invalid retained working session")
	}

	if declared.Schema == navigationSessionSchema {
		var wire struct {
			View struct {
				Navigation map[string]jsontext.Value `json:"navigation"`
			} `json:"view"`
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			return Session{}, err
		}
		for _, member := range []string{"reference_selection", "file_messages", "checked_occurrences", "phi_masked"} {
			if _, added := wire.View.Navigation[member]; added {
				return Session{}, errors.New("retained reader selections require working session v3")
			}
		}
		session.Schema = SessionSchema
	}
	if err := validateSession(session); err != nil {
		return Session{}, err
	}
	return session, nil
}

// encodeSession writes a validated document deterministically, so the same
// retained work produces the same bytes.
func encodeSession(session Session) ([]byte, error) {
	if err := validateSession(session); err != nil {
		return nil, err
	}
	data, err := json.Marshal(session, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode the retained working session")
	}
	data = append(data, '\n')
	if len(data) > maxSessionBytes {
		return nil, errors.New("retained working session exceeds its size limit")
	}
	return data, nil
}

// validateSession reports the first reason a working session cannot be
// retained. Past a bound the document is refused, never truncated.
func validateSession(session Session) error {
	if session.Schema != SessionSchema {
		return errUnsupportedSession
	}
	return validateView(session.View)
}

// validateView holds every recorded location to the rule that location is read
// under. A workspace and a run are absolute folder paths the shell resolves
// again when it opens them; a case is one entry of the open workspace, checked
// by the one path policy that checks it at OpenCase; a region is one the window
// declares, so a focus target the facade does not have cannot be restored.
func validateView(view View) error {
	for _, folder := range []string{view.Workspace, view.Run} {
		if folder == "" {
			continue
		}
		if !filepath.IsAbs(folder) || !printable(folder, maxRootBytes) {
			return errors.New("a recorded workspace and run must be bounded absolute folder paths")
		}
	}
	if view.Case != "" {
		if !printable(view.Case, maxEntryBytes) {
			return errors.New("a recorded case must be a bounded printable entry name")
		}
		if err := artifactpath.EntryName(view.Case); err != nil {
			return errors.New("a recorded case must be one entry of the open workspace")
		}
	}
	if view.Case != "" && view.Workspace == "" {
		return errors.New("a recorded case names the workspace it is an entry of")
	}
	if view.Region != "" && !slices.ContainsFunc(regions, func(region Region) bool { return string(region.ID) == view.Region }) {
		return errors.New("a recorded region must be one the window declares")
	}
	if nav := view.Navigation; nav != nil {
		if nav.ProjectIdentity != "" && view.Workspace == "" {
			return errors.New("a recorded project identity names its workspace")
		}
		if nav.SourceKind != "" && nav.SourceKind != "case" && nav.SourceKind != "file" {
			return errors.New("a recorded source must be case evidence or a loose file")
		}
		if nav.FileFormat != "" && !slices.Contains([]string{"auto", "raw", "mllp"}, nav.FileFormat) || nav.FileTerminator != "" && !slices.Contains([]string{"auto", "cr", "lf", "crlf"}, nav.FileTerminator) {
			return errors.New("recorded file framing must be a declared choice")
		}

		places := []string{"home", "messages", "cases", "tests", "runs", "environments", "reports", "tools", "settings", "help", "library", "suite", "edit-suite", "suite-review", "compare-runs", "minimize-failure", "schedules", "share-report", "share-templates", "encrypted-packages", "notes", "case-notes", "case-attachments", "project-files", "inspect-file", "sample-data", "benchmarks", "encryption", "license-setup", "new-test", "edit-test", "similar-findings", "help-article"}
		if !slices.Contains(places, nav.Destination) {
			return errors.New("a recorded destination must have a route owner")
		}
		for _, id := range []string{nav.ProjectIdentity, nav.Object, nav.LocalView, nav.SourceIdentity, nav.Occurrence, nav.Filter, nav.Sort, nav.ReferenceIdentity, nav.ReferenceEdition, nav.FileIdentity} {
			if id != "" && !printable(id, 255) {
				return errors.New("recorded navigation identities must be bounded printable tokens")
			}
		}
		if nav.FieldPath != "" && !printable(nav.FieldPath, 4096) {
			return errors.New("a recorded field path must be bounded and printable")
		}
		if nav.ScrollTop < 0 || nav.ScrollTop > 1<<26 || nav.NodeOffset < 0 || nav.NodeOffset > 1<<20 || nav.FileMessage < 0 || nav.FileMessage > 1<<20 {
			return errors.New("recorded layout and occurrence offsets must be bounded")
		}
		for _, path := range []string{nav.ReferencePath, nav.File} {
			if path != "" && (!filepath.IsAbs(path) || !printable(path, maxRootBytes)) {
				return errors.New("a recorded source and reference must have an absolute bounded path")
			}
		}
		if nav.ReferencePath != "" && !strings.HasPrefix(nav.ReferenceIdentity, "sha256:") {
			return errors.New("a recorded reference must retain its verified identity")
		}
		if len(nav.CheckedOccurrences) > 1024 || len(nav.CheckedOccurrences) > 0 && (nav.SourceIdentity == "" || view.Case == "") {
			return errors.New("recorded checked occurrences require a verified case and at most 1024 entries")
		}
		seenOccurrences := make(map[string]bool, len(nav.CheckedOccurrences))
		for _, id := range nav.CheckedOccurrences {
			if strings.TrimSpace(id) == "" || !printable(id, 255) || seenOccurrences[id] {
				return errors.New("recorded checked occurrences require distinct bounded printable identities")
			}
			seenOccurrences[id] = true
		}
		if len(nav.FileMessages) > 1024 || len(nav.FileMessages) > 0 && (nav.File == "" || nav.FileIdentity == "") {
			return errors.New("recorded file selections require a verified source and at most 1024 occurrences")
		}
		seenMessages := make(map[int]bool, len(nav.FileMessages))
		for _, index := range nav.FileMessages {
			if index < 0 || index > 1<<20 || seenMessages[index] {
				return errors.New("recorded file selections require distinct bounded occurrence indexes")
			}
			seenMessages[index] = true
		}
		for _, selected := range []struct{ path, identity string }{{nav.ReferenceSelection.Profile, nav.ReferenceSelection.ProfileIdentity}, {nav.ReferenceSelection.Pack, nav.ReferenceSelection.PackIdentity}, {nav.ReferenceSelection.Documentation, nav.ReferenceSelection.DocumentationIdentity}} {
			if selected.path == "" && selected.identity == "" {
				continue
			}
			if !filepath.IsAbs(selected.path) || !printable(selected.path, maxRootBytes) || len(selected.identity) != 71 || !strings.HasPrefix(selected.identity, "sha256:") {
				return errors.New("recorded reference selections require absolute paths and exact verified SHA-256 identities")
			}
			for _, digit := range selected.identity[7:] {
				if digit < '0' || digit > '9' && digit < 'a' || digit > 'f' {
					return errors.New("recorded reference identities must be lowercase SHA-256")
				}
			}
		}
		if nav.SourceIdentity != "" && view.Case == "" || nav.Occurrence != "" && nav.SourceIdentity == "" || nav.File != "" && nav.FileIdentity == "" {
			return errors.New("recorded selections must name their verified source identity")
		}
	}
	return nil
}
