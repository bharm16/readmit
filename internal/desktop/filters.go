package desktop

import (
	"errors"
	"io/fs"
	"os"

	"github.com/bharm16/readmit/internal/grid"
)

// filtersName is the fixed name of the saved-filter document, beside the recent
// workspace list and the working session in the user configuration directory.
// All three are local shell state: nothing derives from another, and none is
// evidence.
const filtersName = "filters.json"

// maxFiltersBytes bounds the file this release reads. A larger one is refused
// before it is decoded rather than read into memory first.
const maxFiltersBytes = grid.MaxDocumentBytes

// FiltersResult carries one state and the complete saved-filter state of this
// viewer: the filters they saved and the one selected now. Selected is what
// survives navigating from one case to another.
//
// A saved filter holds what a person typed to filter by, which for a field
// value is the same patient data the field holds. That is why it crosses this
// boundary and nothing read out of a case does: it is their own typing, on this
// machine, and a filter whose criteria a person cannot see would hide records
// without saying what it hid them for.
type FiltersResult struct {
	State    State         `json:"state"`
	Reason   string        `json:"reason,omitzero"`
	Filters  []grid.Filter `json:"filters"`
	Selected string        `json:"selected"`
}

func (r refusal) filters() FiltersResult {
	return FiltersResult{State: r.state, Reason: r.reason, Filters: []grid.Filter{}}
}

// DefaultFiltersPath is the owner-only file the shell keeps saved filters in.
func DefaultFiltersPath() (string, error) { return configPath(filtersName) }

// Filters reports the saved filters and the selected one. It reads one small
// local file and deliberately does not claim the operation slot, so the window
// can still show which view is selected while an operation runs. A document
// this release cannot read is reported, never replaced.
func (a *App) Filters() FiltersResult {
	document, declined := a.savedFilters()
	if declined.state != "" {
		return declined.filters()
	}
	if len(document.Filters) == 0 {
		return FiltersResult{State: Empty, Reason: "no filter has been saved yet", Filters: []grid.Filter{}}
	}
	return FiltersResult{State: Completed, Filters: document.Filters, Selected: document.Selected}
}

// SaveFilter stores one named filter and selects it, so the view a person set
// up is the view they get on the next case as well. A filter saved under a name
// that already exists replaces exactly that filter.
//
// It writes one small local file and runs to completion once it starts, so it
// holds the operation slot but is not interruptible.
func (a *App) SaveFilter(filter grid.Filter) FiltersResult {
	release, claimed := a.claim()
	if !claimed {
		return a.filtersFailure(busyRefusal)
	}
	defer release()
	document, declined := a.savedFilters()
	if declined.state != "" {
		return declined.filters()
	}
	if err := grid.ValidateFilter(filter); err != nil {
		return FiltersResult{State: Failed, Filters: document.Filters, Selected: document.Selected,
			Reason: "the filter was not saved: it needs a name, every value it looks for must be bounded printable text, each field predicate must name one canonical selector and ask for a value or a decoded state, and a time filter must begin before it ends"}
	}
	if existing := document.Find(filter.Name); existing != nil {
		*existing = filter
	} else if len(document.Filters) == grid.MaxFilters {
		return FiltersResult{State: Failed, Filters: document.Filters, Selected: document.Selected,
			Reason: "this viewer already holds as many saved filters as this release stores; save over one of them instead"}
	} else {
		document.Filters = append(document.Filters, filter)
	}
	document.Selected = filter.Name
	return a.storeFilters(document)
}

// SelectFilter records which saved filter the grid applies. An empty name
// selects no filter, which shows every occurrence and excludes none.
//
// The selection is stored rather than held in the window, so it survives
// navigating to another case, closing the window, and reopening it. It runs to
// completion once it starts, so it holds the operation slot but is not
// interruptible.
func (a *App) SelectFilter(name string) FiltersResult {
	release, claimed := a.claim()
	if !claimed {
		return a.filtersFailure(busyRefusal)
	}
	defer release()
	document, declined := a.savedFilters()
	if declined.state != "" {
		return declined.filters()
	}
	if name != "" && document.Find(name) == nil {
		return FiltersResult{State: Failed, Reason: "that filter is not one this viewer has saved",
			Filters: document.Filters, Selected: document.Selected}
	}
	document.Selected = name
	return a.storeFilters(document)
}

// savedFilters reads the document this viewer's filters live in. A missing file
// is a viewer who has saved nothing, which is a complete document rather than a
// failure; every other refusal is reported so the caller can separate a folder
// this account cannot read from a document this release cannot read.
func (a *App) savedFilters() (grid.Document, refusal) {
	document, err := readFilters(a.filtersPath)
	switch {
	case errors.Is(err, fs.ErrPermission):
		return grid.Empty(), refusal{PermissionDenied, "this account cannot read the saved filters"}
	case errors.Is(err, grid.ErrUnsupportedVersion):
		return grid.Empty(), refusal{Failed, "the saved filters were written by a version this release cannot read"}
	case err != nil:
		return grid.Empty(), refusal{Failed, "the saved filters cannot be read; they are left exactly as written"}
	}
	return document, refusal{}
}

// storeFilters installs a complete document and reports what it stored.
func (a *App) storeFilters(document grid.Document) FiltersResult {
	data, err := grid.Encode(document)
	if err != nil {
		return a.filtersFailure(refusal{Failed, "these filters no longer fit the bounded document this release writes; save a shorter one, or save over an existing one"})
	}
	if err := writeShellDocument(a.filtersPath, data); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return a.filtersFailure(refusal{PermissionDenied, "this account cannot write the saved filters"})
		}
		return a.filtersFailure(refusal{Failed, "the filter could not be stored; an interrupted write may be retained beside the saved filters"})
	}
	if len(document.Filters) == 0 {
		return FiltersResult{State: Empty, Reason: "no filter has been saved yet", Filters: []grid.Filter{}}
	}
	return FiltersResult{State: Completed, Filters: document.Filters, Selected: document.Selected}
}

// readFilters treats a missing document as a viewer who has saved nothing and
// returns every other failure, so the caller can say which one it was.
func readFilters(path string) (grid.Document, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return grid.Empty(), nil
	}
	if err != nil {
		return grid.Document{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFiltersBytes {
		return grid.Document{}, errors.New("saved filters must be a bounded regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return grid.Document{}, err
	}
	return grid.Decode(data)
}

// filtersFailure reports the retained selection, never an unsaved candidate.
func (a *App) filtersFailure(failure refusal) FiltersResult {
	result := a.Filters()
	result.State, result.Reason = failure.state, failure.reason
	return result
}
