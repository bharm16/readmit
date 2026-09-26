package desktop

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
)

// A note is named working text a person keeps about a case or about the
// project itself, stored where the project has always kept notes: the
// project's editable document, readmit-revisions/v1. The note editor sees a
// name, its content and the case it is about; the note's identity is the
// application's.

// NotesRequest names the notes of one case, or, with no case, the project's
// own notes that are about no case.
type NotesRequest struct {
	Context RequestContext `json:"context"`
	Case    *ItemRef       `json:"case,omitzero"`
}

// NoteItem is one note. UpdatedAt is null: the project document records no
// time for a note, and a file's time never stands in for one.
type NoteItem struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Content   string   `json:"content"`
	Case      *ItemRef `json:"case,omitzero"`
	UpdatedAt *string  `json:"updated_at"`
}

// NotesResult carries the notes of the scope asked for, and, after a save,
// the note saved.
type NotesResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Notes   []NoteItem     `json:"notes"`
	Saved   *NoteItem      `json:"saved,omitzero"`
}

func (r *NotesResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// NoteInput is the whole of one note as the editor holds it. ID is empty for
// a new note; Case is the case it is about, or empty for a project note.
type NoteInput struct {
	ID      string   `json:"id,omitzero"`
	Name    string   `json:"name"`
	Content string   `json:"content"`
	Case    *ItemRef `json:"case,omitzero"`
}

// NoteSaveRequest is one deliberate Save of a note, under the identity its
// click allocated.
type NoteSaveRequest struct {
	Context  RequestContext `json:"context"`
	IntentID string         `json:"intent_id"`
	Note     NoteInput      `json:"note"`
}

// ListNotes lists the notes of one case, or the project's notes that are
// about no case. It is a read.
func (a *App) ListNotes(request NotesRequest) NotesResult {
	return run(a, false, false, func(ctx context.Context) NotesResult {
		result := NotesResult{Context: request.Context, Notes: []NoteItem{}}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		subject, declined := loaded.noteSubject(request.Case)
		if declined.state != "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		result.Notes = loaded.notesAbout(subject)
		result.State = Completed
		if len(result.Notes) == 0 {
			result.State = Empty
		}
		return result
	})
}

// SaveNoteItem saves one whole note. A new note is given an identity drawn
// from its click, so the same click again replaces the same note rather than
// adding a second; a different note under that click is refused. A note is
// about a case the project registers, or about no case.
func (a *App) SaveNoteItem(request NoteSaveRequest) NotesResult {
	return run(a, false, true, func(ctx context.Context) NotesResult {
		result := NotesResult{Context: request.Context, Notes: []NoteItem{}}
		if !catalog.ValidToken(request.IntentID) {
			result.refuse(Failed, "a save names the click it was submitted by")
			return result
		}
		loaded, declined := a.loadCatalog(ctx, request.Context, true)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		subject, declined := loaded.noteSubject(request.Note.Case)
		if declined.state != "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if subject != "" && !loaded.registered(subject) {
			result.refuse(Failed, "a note is kept about a case the project registers; save the case's details first")
			return result
		}
		name := strings.TrimSpace(request.Note.Name)
		// A note is kept in readmit-revisions/v1, whose titles are bounded in
		// bytes.
		if !validName(name, project.CheckNoteTitle) {
			result.refuse(Failed, "a note's name is 1 to 200 bytes of printable text")
			return result
		}
		if project.CheckNoteBody(request.Note.Content) != nil {
			result.refuse(Failed, "a note's content is at most 4096 bytes of text, whose only control character is a line break")
			return result
		}
		id := request.Note.ID
		switch {
		case id == "":
			id = "note-" + submissionOf("readmit-note", loaded.document.Project.ID, request.IntentID)[:16]
		case !slices.ContainsFunc(loaded.revisions.Notes, func(note project.Note) bool { return note.Name == id }):
			result.refuse(Failed, "that note is no longer in the project")
			return result
		}
		digest := submissionOf("note", id, subject, name, request.Note.Content)
		if _, declined := a.intents.claim(request.IntentID, digest); declined.state != "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		stored, err := operation.SetProjectNote(loaded.root, project.Note{Name: id, Subject: subject, Title: name, Body: request.Note.Content})
		if errors.Is(err, operation.ErrProjectNoteInvalid) {
			result.refuse(Failed, "the note was not stored: a project holds a bounded number of notes, and a note is about a case it registers")
			return result
		} else if err != nil {
			declined := writeRefusal(loaded.root, err)
			result.refuse(declined.state, declined.reason+"; the note was not stored")
			return result
		}
		a.intents.record(request.IntentID, digest)
		loaded.revisions = stored.Revisions
		result.Notes = loaded.notesAbout(subject)
		for i := range result.Notes {
			if result.Notes[i].ID == id {
				result.Saved = &result.Notes[i]
			}
		}
		result.State = Completed
		return result
	})
}

// noteSubject is the project entry a case reference names, for its notes,
// or empty for the project's own notes.
func (c *loadedCatalog) noteSubject(ref *ItemRef) (string, refusal) {
	if ref == nil {
		return "", refusal{}
	}
	index := c.document.Find(ref.ID)
	if (ref.Kind != CaseItem && ref.Kind != VariantItem) || index < 0 || c.document.Items[index].Kind != string(ref.Kind) ||
		c.removed(c.document.Items[index]) || c.document.Items[index].Entry == "" {
		return "", refusal{Failed, "the project holds no such case"}
	}
	return c.document.Items[index].Entry, refusal{}
}

// notesAbout are the notes whose subject is the entry given, in the order
// the project keeps them.
func (c *loadedCatalog) notesAbout(subject string) []NoteItem {
	notes := []NoteItem{}
	for _, note := range c.revisions.Notes {
		if note.Subject != subject {
			continue
		}
		item := NoteItem{ID: note.Name, Name: note.Title, Content: note.Body}
		if subject != "" {
			item.Case = c.entryRef(CaseItem, subject)
			if item.Case == nil {
				item.Case = c.entryRef(VariantItem, subject)
			}
		}
		notes = append(notes, item)
	}
	return notes
}
