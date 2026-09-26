package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/project"
)

// DraftsSchema is the versioned contract of the editor draft store: the
// unstored work every editor of this window retains while it is being edited.
// It is the fourth local shell document, beside the recent workspace list, the
// saved filters and the working session, and like all of them it is per-viewer
// state rather than evidence: no case, run, result, review or report ever holds
// it, and nothing in it is read out of a case bundle.
//
// One entry is one editor's draft, named by an internal identity the store
// mints. A draft never requires a valid final artifact: an unfinished note with
// no name and no title yet, a half-answered test draft, a canonical document
// that would not pass the reader today, and a reproducer plan still being
// edited are all retained as they are. The envelope carries the draft's kind
// and the contract its content declares; the content itself is interpreted only
// by the editor that owns it, through that contract's own strict reader. An
// editor this release does not carry can therefore adopt the store without
// changing it, and a new meaning for the envelope itself is a new version read
// beside this one. The store never holds a credential value or an approval: no
// facade method can express either, because credentials are references under
// ADR-0006 and an approval is a typed decision about bytes just read, never a
// document. See
// [ADR-0003](../../docs/adr/0003-specs-are-strict-json-with-typed-operators.md)
// and [ADR-0005](../../docs/adr/0005-desktop-shell-is-a-separate-module-over-a-typed-go-facade.md).
const DraftsSchema = "readmit-desktop-drafts/v1"

// DraftsSchemaV2 is the draft store once a draft names the object it is an
// edit of: the same envelope with one more member, item, which names the
// project, the object and the revision the edit began from, so a recovered
// draft returns to that object and a save of it is checked against that
// revision. The store is written as v2 only while it holds such a draft; a
// store of v1 drafts is written as v1, exactly as before, and both are read.
const DraftsSchemaV2 = "readmit-desktop-drafts/v2"

// MaxEditorDrafts bounds the unstored editor drafts one viewer retains. Past
// the bound the new draft is refused rather than an existing one being dropped,
// because silently dropping working text is the loss this store exists to
// prevent.
const MaxEditorDrafts = 16

// NoteDraftSchema is the contract of the note editor's own draft content: the
// working text of one note, every member of which may still be empty. It is
// deliberately looser than the note a project stores, which needs a name and a
// title, because a draft is written before either exists.
const NoteDraftSchema = "readmit-note-draft/v1"

const (
	maxDraftsBytes        = 1 << 20
	maxDraftContentBytes  = 1 << 18
	maxDraftTokenBytes    = 64
	maxDraftIdentityBytes = 128
)

// errUnsupportedDrafts reports a draft store written under a contract version
// this release does not read. It is distinct from a document this release reads
// and rejects, so the facade can say which one it was handed.
var errUnsupportedDrafts = errors.New("unsupported editor draft store version")

// EditorDraft is one editor's unstored work, retained under an internal
// identity so it can be replaced and dropped without ever requiring a valid
// final artifact.
//
// Workspace is the folder the editor is working in, and Case and Identity name
// the entry of that folder the draft was authored against together with the
// identity the window verified for it, so a restored draft is never silently
// rebound to different evidence: the editor that adopts it compares the
// identity it verified now against the one retained here. Content is the
// owning editor's draft document, carried as JSON text of the contract
// ContentSchema names.
type EditorDraft struct {
	ID            string         `json:"id"`
	Kind          string         `json:"kind"`
	Workspace     string         `json:"workspace"`
	Case          string         `json:"case"`
	Identity      string         `json:"identity"`
	ContentSchema string         `json:"content_schema"`
	Content       jsontext.Value `json:"content"`
	// Item names the object this draft edits and the revision the edit
	// began from, when it edits a catalog object.
	Item *DraftItem `json:"item,omitzero"`
}

// DraftItem is the object one draft edits: the project's identity, and the
// object's reference with the revision the edit began from.
type DraftItem struct {
	ProjectID string  `json:"project_id"`
	Ref       ItemRef `json:"ref"`
}

// EditorDraftsResult carries one state and the drafts the store retains as it
// now stands. Drafts is present whenever the store could be read, so a refused
// edit still reports what stays retained rather than an unstored candidate.
type EditorDraftsResult struct {
	State  State         `json:"state"`
	Reason string        `json:"reason,omitzero"`
	Drafts []EditorDraft `json:"drafts,omitzero"`
}

// NoteDraft is the note editor's own draft content: the working text of one
// note, every member of which may still be empty. Name and Title may be empty
// while the note is being started, which is exactly the working state the
// working session cannot hold and this store can.
type NoteDraft struct {
	Schema  string `json:"schema"`
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Title   string `json:"title"`
	Body    string `json:"body"`
}

// emptyDrafts is a viewer who has retained no editor draft: a complete document
// rather than a missing one, so reading never writes.
func emptyDrafts() []EditorDraft { return []EditorDraft{} }

// SaveEditorDraft retains one editor's unstored work, replacing the draft it is
// an edit of. An empty identity mints a new draft and the result names the
// identity it was retained under; an identity that is no longer held is refused
// rather than silently creating a second draft, so a window that raced a
// discard is told so instead of resurrecting work a person dropped.
//
// The draft is retained outside the project and outside evidence: storing the
// note, exporting the test or building the reproducer are the separate
// deliberate steps that write final artifacts, and each one discards its draft.
// It writes one small local file and does not claim the operation slot, because
// a crash while an operation runs is exactly when unstored work has to survive.
// It is serialized against the other draft writes, so a reader never observes a
// partial document.
func (a *App) SaveEditorDraft(draft EditorDraft) EditorDraftsResult {
	a.draftsMu.Lock()
	defer a.draftsMu.Unlock()
	if err := a.admitAuthor(); err != nil {
		return EditorDraftsResult{State: PermissionDenied, Reason: err.Error()}
	}
	drafts, declined := a.retainedDrafts()
	if declined.state != "" {
		return EditorDraftsResult{State: declined.state, Reason: declined.reason}
	}
	if err := validateEditorDraft(draft); err != nil {
		return a.draftsFailure(refusal{Failed, "the draft was not retained: it needs a kind, the workspace folder it belongs to by absolute path, the contract its content declares, and bounded content"})
	}
	if a.reviews.mentions(draft.Content) {
		return a.draftsFailure(refusal{Failed, "the draft was not retained: a draft never holds an action review"})
	}
	if draft.ID == "" {
		if len(drafts) == MaxEditorDrafts {
			return a.draftsFailure(refusal{Failed, "this viewer already holds as many unstored editor drafts as this release retains; store or discard one of them before starting another"})
		}
		id, err := mintDraftID(drafts)
		if err != nil {
			return a.draftsFailure(refusal{Failed, "the draft was not retained: another identity could not be minted for it"})
		}
		draft.ID = id
		drafts = append(drafts, draft)
	} else {
		index := slices.IndexFunc(drafts, func(held EditorDraft) bool { return held.ID == draft.ID })
		if index < 0 {
			return a.draftsFailure(refusal{Failed, "the draft was not retained: the draft it edits is no longer one this viewer has retained"})
		}
		drafts[index] = draft
	}
	return a.storeDrafts(drafts)
}

// DiscardEditorDraft drops one retained draft. An editor discards its draft
// once the work it retains has actually been stored or the person explicitly
// asked to drop it, so recovery offers back only work that is still unstored.
// A draft this viewer does not hold is refused rather than reported as
// discarded. It writes one small local file and does not claim the operation
// slot.
func (a *App) DiscardEditorDraft(id string) EditorDraftsResult {
	a.draftsMu.Lock()
	defer a.draftsMu.Unlock()
	drafts, declined := a.retainedDrafts()
	if declined.state != "" {
		return EditorDraftsResult{State: declined.state, Reason: declined.reason}
	}
	index := slices.IndexFunc(drafts, func(held EditorDraft) bool { return held.ID == id })
	if index < 0 {
		return a.draftsFailure(refusal{Failed, "that draft is not one this viewer has retained"})
	}
	drafts = slices.Delete(drafts, index, index+1)
	return a.storeDrafts(drafts)
}

// EditorDrafts reports every editor draft this viewer retains. It reads one
// small local file and deliberately does not claim the operation slot, so the
// window can restore unstored work while an operation runs.
func (a *App) EditorDrafts() EditorDraftsResult {
	drafts, declined := a.retainedDrafts()
	if declined.state != "" {
		return EditorDraftsResult{State: declined.state, Reason: declined.reason}
	}
	if len(drafts) == 0 {
		return EditorDraftsResult{State: Empty, Drafts: drafts}
	}
	return EditorDraftsResult{State: Completed, Drafts: drafts}
}

// mintDraftID mints an internal draft identity no held draft already carries.
func mintDraftID(held []EditorDraft) (string, error) {
	for range 64 {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		id := hex.EncodeToString(raw)
		if !slices.ContainsFunc(held, func(draft EditorDraft) bool { return draft.ID == id }) {
			return id, nil
		}
	}
	return "", errors.New("no unused draft identity")
}

// retainedDrafts reads the document this viewer's editor drafts live in,
// through the shell document store. A missing file is a viewer who has
// retained nothing, which is a complete document rather than a failure; every
// other refusal is reported so the caller can separate a file this account
// cannot read from one this release cannot read.
func (a *App) retainedDrafts() ([]EditorDraft, refusal) {
	drafts, err := readEditorDrafts(a.documents)
	switch {
	case errors.Is(err, fs.ErrPermission):
		return nil, refusal{PermissionDenied, "this account cannot read the retained editor drafts"}
	case errors.Is(err, errUnsupportedDrafts):
		return nil, refusal{Failed, "the retained editor drafts were written by a version this release cannot read"}
	case err != nil:
		return nil, refusal{Failed, "the retained editor drafts cannot be read; they are left exactly as written"}
	}
	return drafts, refusal{}
}

// storeDrafts installs a complete document and reports what it retained.
func (a *App) storeDrafts(drafts []EditorDraft) EditorDraftsResult {
	data, err := encodeEditorDrafts(drafts)
	if err != nil {
		return a.draftsFailure(refusal{Failed, "these editor drafts no longer fit the bounded document this release retains; store or discard some of them"})
	}
	if err := a.documents.write(draftsName, data); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return a.draftsFailure(refusal{PermissionDenied, "this account cannot write the retained editor drafts"})
		}
		return a.draftsFailure(refusal{Failed, "the editor drafts could not be retained; an interrupted write may be kept beside them"})
	}
	return EditorDraftsResult{State: Completed, Drafts: drafts}
}

// draftsFailure reports the drafts that stay retained, never the candidate that
// was refused, so the window never shows work as kept that is not.
func (a *App) draftsFailure(failure refusal) EditorDraftsResult {
	retained, declined := a.retainedDrafts()
	if declined.state != "" {
		return EditorDraftsResult{State: declined.state, Reason: declined.reason}
	}
	return EditorDraftsResult{State: failure.state, Reason: failure.reason, Drafts: retained}
}

// readEditorDrafts treats a missing document as a viewer who has retained
// nothing and returns every other failure, so the caller can say which one it
// was. The reading is the store's one rule: a bounded regular file, never a
// link.
func readEditorDrafts(documents ShellDocuments) ([]EditorDraft, error) {
	data, err := documents.read(draftsName, maxDraftsBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return emptyDrafts(), nil
	}
	if err != nil {
		return nil, err
	}
	return decodeEditorDrafts(data)
}

// draftsDocument is the whole store: the declared contract and the drafts it
// holds, sorted by identity.
type draftsDocument struct {
	Schema string        `json:"schema"`
	Drafts []EditorDraft `json:"drafts"`
}

// decodeEditorDrafts reads a retained editor draft store. Unknown members and
// unknown versions are errors; there is no migration and no repair, so a
// document this release cannot read stays exactly as it was written.
func decodeEditorDrafts(data []byte) ([]EditorDraft, error) {
	if len(data) > maxDraftsBytes {
		return nil, errors.New("retained editor drafts exceed their size limit")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return nil, errors.New("invalid retained editor drafts")
	}
	if declared.Schema != DraftsSchema && declared.Schema != DraftsSchemaV2 {
		return nil, errUnsupportedDrafts
	}
	var store draftsDocument
	if err := json.Unmarshal(data, &store, json.RejectUnknownMembers(true)); err != nil {
		return nil, errors.New("invalid retained editor drafts")
	}
	for i, draft := range store.Drafts {
		if i > 0 && strings.Compare(store.Drafts[i-1].ID, draft.ID) >= 0 {
			return nil, errors.New("drafts must carry distinct identities and be sorted by them")
		}
		if draft.Item != nil && declared.Schema == DraftsSchema {
			return nil, errors.New("invalid retained editor drafts")
		}
	}
	if err := validateEditorDrafts(store.Drafts); err != nil {
		return nil, err
	}
	return store.Drafts, nil
}

// encodeEditorDrafts writes a validated document deterministically, so the same
// retained work produces the same bytes. The document is held sorted by
// identity; the order the window's editors saved in is not part of what it
// means.
func encodeEditorDrafts(drafts []EditorDraft) ([]byte, error) {
	if drafts == nil {
		drafts = emptyDrafts()
	}
	if err := validateEditorDrafts(drafts); err != nil {
		return nil, err
	}
	sorted := slices.Clone(drafts)
	slices.SortFunc(sorted, func(a, b EditorDraft) int { return strings.Compare(a.ID, b.ID) })
	schema := DraftsSchema
	if slices.ContainsFunc(sorted, func(draft EditorDraft) bool { return draft.Item != nil }) {
		schema = DraftsSchemaV2
	}
	data, err := json.Marshal(draftsDocument{Schema: schema, Drafts: sorted}, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode the retained editor drafts")
	}
	data = append(data, '\n')
	if len(data) > maxDraftsBytes {
		return nil, errors.New("retained editor drafts exceed their size limit")
	}
	return data, nil
}

// validateEditorDrafts reports the first reason a set of drafts cannot be
// retained. Past a bound the document is refused, never truncated.
func validateEditorDrafts(drafts []EditorDraft) error {
	if len(drafts) > MaxEditorDrafts {
		return errors.New("a viewer retains at most " + strconv.Itoa(MaxEditorDrafts) + " unstored editor drafts")
	}
	for i, draft := range drafts {
		if !printable(draft.ID, maxDraftTokenBytes) {
			return errors.New("a retained draft carries an internal identity")
		}
		if err := validateEditorDraft(draft); err != nil {
			return err
		}
		for _, held := range drafts[:i] {
			if held.ID == draft.ID {
				return errors.New("drafts must carry distinct identities")
			}
		}
	}
	return nil
}

// validateEditorDraft holds one draft to the envelope rule. The kind and the
// content contract are open vocabularies on purpose: the store interprets
// neither, so an editor this release does not carry can adopt the store without
// changing it, and what the content means is settled by the contract its own
// reader enforces when the draft is used. The note content is the one this
// package owns, and it is validated here.
func validateEditorDraft(draft EditorDraft) error {
	if draft.ID != "" && !printable(draft.ID, maxDraftTokenBytes) {
		return errors.New("a draft identity must be bounded printable text")
	}
	if !draftToken(draft.Kind) {
		return errors.New("a draft kind must be a bounded lowercase word")
	}
	if !filepath.IsAbs(draft.Workspace) || !printable(draft.Workspace, maxRootBytes) {
		return errors.New("a draft names the workspace folder it belongs to by absolute path")
	}
	if draft.Case != "" {
		if !printable(draft.Case, maxEntryBytes) || artifactpath.EntryName(draft.Case) != nil {
			return errors.New("a draft names its case by one entry of the workspace")
		}
	}
	if draft.Identity != "" && !printable(draft.Identity, maxDraftIdentityBytes) {
		return errors.New("a draft records the identity its case was verified under as bounded printable text")
	}
	if !draftToken(draft.ContentSchema) || !strings.Contains(draft.ContentSchema, "/") {
		return errors.New("a draft declares the contract its content is written in")
	}
	if len(draft.Content) == 0 || len(draft.Content) > maxDraftContentBytes {
		return errors.New("a draft carries bounded content")
	}
	if !draft.Content.IsValid() {
		return errors.New("a draft carries its content as a JSON value")
	}
	if item := draft.Item; item != nil && (!catalog.ValidID(item.ProjectID) || !slices.Contains(itemKinds, item.Ref.Kind) ||
		item.Ref.Kind == ProjectItem || !catalog.ValidID(item.Ref.ID) || len(item.Ref.Revision) > 16) {
		return errors.New("a draft names the object it edits by its project, kind, identity and revision")
	}
	if draft.ContentSchema == NoteDraftSchema {
		return validateNoteDraft(draft.Content)
	}
	if draft.ContentSchema == SuiteDraftSchema {
		return validateSuiteDraft([]byte(draft.Content))
	}
	return nil
}

// validateNoteDraft holds a retained note to the rule a stored note is held to,
// relaxed in exactly one direction: a draft may still have no name and no
// title, because a note is written before either exists. Whatever it does
// declare must be text the project could accept.
func validateNoteDraft(content jsontext.Value) error {
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(content, &declared); err != nil || declared.Schema != NoteDraftSchema {
		return errors.New("note draft content declares " + NoteDraftSchema)
	}
	var note NoteDraft
	if err := json.Unmarshal(content, &note, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid note draft content")
	}
	if note.Name != "" {
		if err := project.CheckNoteName(note.Name); err != nil {
			return err
		}
	}
	if note.Subject != "" {
		if err := artifactpath.EntryName(note.Subject); err != nil {
			return errors.New("note subject: must be one entry of the workspace")
		}
		if len(note.Subject) > maxEntryBytes {
			return errors.New("note subject: must be at most " + strconv.Itoa(maxEntryBytes) + " bytes")
		}
	}
	if note.Title != "" {
		if err := project.CheckNoteTitle(note.Title); err != nil {
			return err
		}
	}
	return project.CheckNoteBody(note.Body)
}

// draftToken accepts the bounded lowercase words a kind and a content contract
// are written as. Control characters, whitespace and shell-significant letters
// are refused rather than escaped, so nothing stored here can rewrite a
// rendered line when it is displayed back.
func draftToken(value string) bool {
	if value == "" || len(value) > maxDraftTokenBytes {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-' || r == '/':
		default:
			return false
		}
	}
	return true
}
