package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/project"
)

// SessionSchema is the versioned contract of one viewer's working session: what
// they had open, and the note edits they had typed and not stored yet. It is
// the third local shell document, beside the recent workspace list and the
// saved filters, and like both of them it is per-viewer state rather than
// evidence: no case, run, result, review or report ever holds it, and nothing
// in it is read out of a case bundle.
//
// A new member means a new version string and a reader for both. Nothing is
// added to readmit-desktop-recent/v1, readmit-filters/v1, readmit-revisions/v1
// or readmit-job/v1, none of which this file touches. See
// [ADR-0003](../../docs/adr/0003-specs-are-strict-json-with-typed-operators.md).
const SessionSchema = "readmit-desktop-session/v1"

// sessionName is the fixed name of the working-session document, beside the
// recent workspace list and the saved filters in the user configuration
// directory. Nothing derives from the others: each is named explicitly where
// the application is wired up.
const sessionName = "session.json"

// MaxDrafts bounds the unstored note edits one viewer retains. Past the bound
// the edit is refused rather than another one being dropped, because silently
// dropping working text is the loss this document exists to prevent.
const MaxDrafts = 16

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

// Draft is one note a person is still writing, retained before it is stored.
//
// Note is the canonical editable type the project document holds, so a draft is
// the same working text a stored note is and is held to the same rule; Project
// is the folder it is a draft of. A draft is not in that project's document and
// is not evidence: storing it is SaveNote, which is the step that checks the
// subject against the cases and revisions the project registers.
type Draft struct {
	Project string       `json:"project"`
	Note    project.Note `json:"note"`
}

// Session is the whole retained working state of one viewer.
//
// Drafts are held sorted by the project they belong to and then by name, so the
// same retained work produces the same bytes, and one project cannot hold two
// drafts of the same note.
type Session struct {
	Schema string  `json:"schema"`
	View   View    `json:"view"`
	Drafts []Draft `json:"drafts"`
}

// SessionResult carries one state and the working session as it now stands.
// Session is present whenever the document could be read, so a refused edit
// still reports what stays retained rather than an unstored candidate.
type SessionResult struct {
	State   State    `json:"state"`
	Reason  string   `json:"reason,omitzero"`
	Session *Session `json:"session,omitzero"`
}

// RecoveryResult is what the window restores after an interruption: the
// retained session, and the state of the durable run that session was watching.
//
// Run is that run reopened read-only. RunReason is the fixed sentence for a
// remembered run that could not be verified, so a run the session names is
// never silently absent. Neither member is ever produced by starting, resuming
// or resending anything: an interrupted send stays interrupted, and it is
// reported here as the uncertain result it is.
type RecoveryResult struct {
	State     State               `json:"state"`
	Reason    string              `json:"reason,omitzero"`
	Session   *Session            `json:"session,omitzero"`
	Run       *durablerun.Summary `json:"run,omitzero"`
	RunReason string              `json:"run_reason,omitzero"`
}

func (r *RecoveryResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// DefaultSessionPath is the owner-only file the shell retains the session in.
func DefaultSessionPath() (string, error) { return configPath(sessionName) }

// emptySession is a viewer who has had nothing open and typed nothing: a
// complete document rather than a missing one, so reading never writes.
func emptySession() Session { return Session{Schema: SessionSchema, Drafts: []Draft{}} }

// RecoverSession restores the working session after an interruption.
//
// It reports what the viewer had open and every note edit they had not stored,
// so unsaved work comes back instead of being lost with the process that held
// it. When the session names a durable run it reopens that run through the same
// read-only recovery OpenDurableRun uses, which verifies retained evidence,
// changes no byte of it, and opens no network connection.
//
// It never resumes or resends. A run whose completion was not recorded stays
// interrupted, a delivery whose effect is unknown stays uncertain, and neither
// becomes a pass because the window was opened again. Restarting a run is
// StartDurableRun, which requires a new output and an explicit operator action.
// It runs to completion once it starts, so it holds the operation slot but is
// not interruptible.
func (a *App) RecoverSession() RecoveryResult {
	return run(a, false, false, func(context.Context) RecoveryResult {
		return a.recoverSession()
	})
}

func (a *App) recoverSession() RecoveryResult {
	session, declined := a.retainedSession()
	if declined.state != "" {
		return RecoveryResult{State: declined.state, Reason: declined.reason}
	}
	if session.View == (View{}) && len(session.Drafts) == 0 {
		return RecoveryResult{State: Empty, Reason: "no working session was retained"}
	}
	restored := RecoveryResult{State: Completed, Session: &session}
	if session.View.Run == "" {
		return restored
	}
	summary, err := durablerun.Open(session.View.Run)
	if err != nil {
		restored.RunReason = "the run this session was watching could not be verified; its retained evidence is unchanged, and nothing was resumed or resent"
		return restored
	}
	restored.Run = &summary
	return restored
}

// RecordView retains where this viewer is, so an interruption does not also
// lose which workspace, case and run they were looking at. It replaces the
// recorded view and leaves every retained draft exactly as it is.
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

// SaveDraft retains one note a person has typed and not stored yet, so a
// controlled crash, a lost window or a restart returns the text instead of
// discarding it. A draft saved for a note that already has one replaces exactly
// that draft.
//
// The draft is held to the same rule a stored note is, so working text this
// keeps is working text the project can accept. It is retained outside the
// project, so nothing here reaches evidence, a project document or any other
// retained artifact: storing the note is SaveNote. It writes one small local
// file and does not claim the operation slot.
func (a *App) SaveDraft(draft Draft) SessionResult {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	if err := a.admitAuthor(); err != nil {
		return SessionResult{State: PermissionDenied, Reason: err.Error()}
	}
	session, declined := a.retainedSession()
	if declined.state != "" {
		return SessionResult{State: declined.state, Reason: declined.reason}
	}
	if err := validateDraft(draft); err != nil {
		return a.sessionFailure(refusal{Failed, "the draft was not retained: it names the project folder it belongs to by absolute path, and its note needs a name and a title, with bounded printable text"})
	}
	index, held := slices.BinarySearchFunc(session.Drafts, draft, compareDrafts)
	switch {
	case held:
		session.Drafts[index] = draft
	case len(session.Drafts) == MaxDrafts:
		return a.sessionFailure(refusal{Failed, "this viewer already holds as many unstored note edits as this release retains; store one of them before starting another"})
	default:
		session.Drafts = slices.Insert(session.Drafts, index, draft)
	}
	return a.storeSession(session)
}

// DiscardDraft drops one retained draft. The window discards a draft once the
// note it was an edit of has been stored, so recovery offers back only work
// that is still unstored. A draft this viewer does not hold is refused rather
// than reported as discarded. It writes one small local file and does not claim
// the operation slot.
func (a *App) DiscardDraft(projectRoot, name string) SessionResult {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	session, declined := a.retainedSession()
	if declined.state != "" {
		return SessionResult{State: declined.state, Reason: declined.reason}
	}
	wanted := Draft{Project: projectRoot, Note: project.Note{Name: name}}
	index, held := slices.BinarySearchFunc(session.Drafts, wanted, compareDrafts)
	if !held {
		return a.sessionFailure(refusal{Failed, "that draft is not one this viewer has retained"})
	}
	session.Drafts = slices.Delete(session.Drafts, index, index+1)
	return a.storeSession(session)
}

// compareDrafts orders drafts by the project they belong to and then by note
// name, which is also what makes one note of one project hold one draft.
func compareDrafts(a, b Draft) int {
	if held := strings.Compare(a.Project, b.Project); held != 0 {
		return held
	}
	return strings.Compare(a.Note.Name, b.Note.Name)
}

// retainedSession reads the document this viewer's session lives in. A missing
// file is a viewer who has retained nothing, which is a complete document
// rather than a failure; every other refusal is reported so the caller can
// separate a file this account cannot read from one this release cannot read.
func (a *App) retainedSession() (Session, refusal) {
	session, err := readSession(a.sessionPath)
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
	if err := writeShellDocument(a.sessionPath, data); err != nil {
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
func readSession(path string) (Session, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return emptySession(), nil
	}
	if err != nil {
		return Session{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxSessionBytes {
		return Session{}, errors.New("a retained working session must be a bounded regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Session{}, err
	}
	session, err := decodeSession(data)
	if err != nil {
		return Session{}, err
	}
	// A document that declared no draft holds no draft. The empty list rather
	// than a nil one is what the facade hands out, so the interface always
	// reads a list it can count rather than an absent member.
	if session.Drafts == nil {
		session.Drafts = []Draft{}
	}
	return session, nil
}

// decodeSession reads a retained working session. Unknown members and unknown
// versions are errors; there is no migration and no repair, so a document this
// release cannot read stays exactly as it was written.
//
// The declared contract version is read before the strict decode, for the same
// reason every other document of this product reads it first: a later release
// bumps the version precisely because it adds members. Nothing nested here
// declares an unmarshaler of its own, so the strict pass reaches every member
// of the view and of each draft.
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
	var session Session
	if err := json.Unmarshal(data, &session, json.RejectUnknownMembers(true)); err != nil {
		return Session{}, errors.New("invalid retained working session")
	}
	if err := validateSession(session); err != nil {
		return Session{}, err
	}
	return session, nil
}

// encodeSession writes a validated document deterministically, so the same
// retained work produces the same bytes.
func encodeSession(session Session) ([]byte, error) {
	if session.Drafts == nil {
		session.Drafts = []Draft{}
	}
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
	if err := validateView(session.View); err != nil {
		return err
	}
	if len(session.Drafts) > MaxDrafts {
		return errors.New("a viewer retains at most " + strconv.Itoa(MaxDrafts) + " unstored note edits")
	}
	for i, draft := range session.Drafts {
		if err := validateDraft(draft); err != nil {
			return err
		}
		if i > 0 && compareDrafts(session.Drafts[i-1], draft) >= 0 {
			return errors.New("drafts must be uniquely named within their project and sorted")
		}
	}
	return nil
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
	if view.Region != "" && !slices.ContainsFunc(regions, func(region Region) bool { return region.ID == view.Region }) {
		return errors.New("a recorded region must be one the window declares")
	}
	return nil
}

// validateDraft holds an unstored edit to exactly the rule a stored note is
// held to, so retained working text is working text the project can accept.
// Whether its subject names evidence the project registers is settled when the
// note is stored, because a draft is written before that project is opened.
func validateDraft(draft Draft) error {
	if !filepath.IsAbs(draft.Project) || !printable(draft.Project, maxRootBytes) {
		return errors.New("a draft names the project folder it belongs to by absolute path")
	}
	return project.ValidateNote(draft.Note)
}
