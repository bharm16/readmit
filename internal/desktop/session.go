package desktop

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// SessionSchema is the versioned contract of one viewer's working session:
// what they had open. It is the third local shell document, beside the recent
// workspace list and the saved filters, and like both of them it is per-viewer
// state rather than evidence: no case, run, result, review or report ever holds
// it, and nothing in it is read out of a case bundle.
//
// A document written by an earlier release may also carry drafts, the note
// edits that release retained here. They are read past and never written
// again: unstored work now lives in the editor draft store
// (readmit-desktop-drafts), and notes are stored in the project.
//
// A new member means a new version string and a reader for both. Nothing is
// added to readmit-filters/v1, readmit-revisions/v1
// or readmit-job/v1, none of which this file touches. See
// [ADR-0003](../../docs/adr/0003-specs-are-strict-json-with-typed-operators.md).
const SessionSchema = "readmit-desktop-session/v1"

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
	Workspace string `json:"workspace"`
	Region    string `json:"region"`
	Case      string `json:"case"`
	Run       string `json:"run"`
}

// Session is the whole retained working state of one viewer.
type Session struct {
	Schema string `json:"schema"`
	View   View   `json:"view"`
}

// storedSession is a session document as it is read: an earlier release's
// drafts member is accepted and ignored.
type storedSession struct {
	Schema string         `json:"schema"`
	View   View           `json:"view"`
	Drafts jsontext.Value `json:"drafts,omitzero"`
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
		return a.sessionFailure(refusal{Failed, "the view was not recorded: a workspace and a run are absolute folder paths, a case is one entry of the open workspace, and a region is one the window declares"})
	}
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
	if declared.Schema != SessionSchema {
		return Session{}, errUnsupportedSession
	}
	var stored storedSession
	if err := json.Unmarshal(data, &stored, json.RejectUnknownMembers(true)); err != nil {
		return Session{}, errors.New("invalid retained working session")
	}
	session := Session{Schema: stored.Schema, View: stored.View}
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
	return nil
}
