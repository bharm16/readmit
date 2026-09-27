package desktop

import (
	"encoding/json/v2"
	"errors"
	"io/fs"
	"slices"
)

// Preferences are how this viewer's window looks and the name its local
// approvals carry. They are kept in the shell's own document, never in the
// webview's storage, so the next window opens the way this one was left.

// preferencesSchema is the versioned contract of the shell document that
// keeps the theme, the text size and the local reviewer name. It holds no
// evidence.
const preferencesSchema = "readmit-desktop-preferences/v1"

const maxPreferencesBytes = 4096

// maxReviewerBytes bounds the local reviewer name.
const maxReviewerBytes = 200

// minTextScale and maxTextScale bound a kept text size. Any percent between
// them reads, so a size an earlier release offered stays selectable.
const (
	minTextScale = 100
	maxTextScale = 200
)

// Preferences are the window's appearance and the local reviewer name.
// Reviewer is a label local approvals are attributed to; it is not an
// identity anything authenticates, and a review's binding never reads it.
type Preferences struct {
	Theme     Theme  `json:"theme"`
	TextScale int    `json:"text_scale"`
	Reviewer  string `json:"reviewer,omitzero"`
}

// PreferencesResult carries one state. Preferences is always present: the
// defaults when nothing was kept, or when what was kept cannot be read.
type PreferencesResult struct {
	State       State       `json:"state"`
	Reason      string      `json:"reason,omitzero"`
	Preferences Preferences `json:"preferences"`
}

type preferencesDocument struct {
	Schema    string `json:"schema"`
	Theme     Theme  `json:"theme"`
	TextScale int    `json:"text_scale"`
	Reviewer  string `json:"reviewer,omitzero"`
}

var defaultPreferences = Preferences{Theme: SystemTheme, TextScale: minTextScale}

// unreadablePreferences refuses a kept preferences document this release
// cannot read. It is left exactly as written.
const unreadablePreferences = "the saved preferences cannot be read; they are left exactly as written"

func validPreferences(p Preferences) string {
	switch {
	case !slices.Contains(themes, p.Theme):
		return "choose one of the offered themes"
	case p.TextScale < minTextScale || p.TextScale > maxTextScale:
		return "choose a text size between 100% and 200%"
	case p.Reviewer != "" && !printable(p.Reviewer, maxReviewerBytes):
		return "a reviewer name is at most 200 printable characters"
	}
	return ""
}

func (a *App) readPreferences() (Preferences, error) {
	data, err := a.documents.read(preferencesName, maxPreferencesBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return defaultPreferences, nil
	}
	if err != nil {
		return defaultPreferences, err
	}
	var document preferencesDocument
	if err := json.Unmarshal(data, &document, json.RejectUnknownMembers(true)); err != nil || document.Schema != preferencesSchema {
		return defaultPreferences, errNotADocument
	}
	kept := Preferences{Theme: document.Theme, TextScale: document.TextScale, Reviewer: document.Reviewer}
	if validPreferences(kept) != "" {
		return defaultPreferences, errNotADocument
	}
	return kept, nil
}

// ReadPreferences reads the kept theme, text size and reviewer name. It reads
// one small local document and does not claim the operation slot, so the
// window opens with them while an operation runs.
func (a *App) ReadPreferences() PreferencesResult {
	a.preferencesMu.Lock()
	defer a.preferencesMu.Unlock()
	kept, err := a.readPreferences()
	if err != nil {
		return PreferencesResult{State: Failed, Reason: unreadablePreferences, Preferences: kept}
	}
	return PreferencesResult{State: Completed, Preferences: kept}
}

// SavePreferences keeps exactly the preferences given, replacing the document
// whole. A theme the window does not offer, a text size outside 100–200% or a
// reviewer name that is not printable is refused and nothing is written. Like
// ReadPreferences it does not claim the operation slot.
func (a *App) SavePreferences(p Preferences) PreferencesResult {
	a.preferencesMu.Lock()
	defer a.preferencesMu.Unlock()
	kept, err := a.readPreferences()
	if err != nil {
		return PreferencesResult{State: Failed, Reason: unreadablePreferences, Preferences: kept}
	}
	if reason := validPreferences(p); reason != "" {
		return PreferencesResult{State: Failed, Reason: reason, Preferences: kept}
	}
	data, err := json.Marshal(preferencesDocument{Schema: preferencesSchema, Theme: p.Theme, TextScale: p.TextScale, Reviewer: p.Reviewer}, json.Deterministic(true))
	if err == nil {
		err = a.documents.write(preferencesName, append(data, '\n'))
	}
	if err != nil {
		result := PreferencesResult{State: Failed, Reason: "the preferences could not be saved", Preferences: kept}
		if errors.Is(err, fs.ErrPermission) {
			result.State, result.Reason = PermissionDenied, "this account cannot write the saved preferences"
		}
		return result
	}
	return PreferencesResult{State: Completed, Preferences: p}
}

// savedReviewer is the local reviewer name kept in the preferences, or empty
// when none is kept or the document cannot be read.
func (a *App) savedReviewer() string {
	a.preferencesMu.Lock()
	defer a.preferencesMu.Unlock()
	kept, err := a.readPreferences()
	if err != nil {
		return ""
	}
	return kept.Reviewer
}
