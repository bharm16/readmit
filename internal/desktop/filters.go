package desktop

import (
	"errors"
	"io/fs"
	"slices"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/grid"
)

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
	release, claimed := a.claim("")
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
	release, claimed := a.claim("")
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

// savedFilters reads the document this viewer's filters live in, through the
// shell document store. A missing file is a viewer who has saved nothing,
// which is a complete document rather than a failure; every other refusal is
// reported so the caller can separate a folder this account cannot read from
// a document this release cannot read.
func (a *App) savedFilters() (grid.Document, refusal) {
	document, err := readFilters(a.documents)
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
	if err := a.documents.write(filtersName, data); err != nil {
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
// returns every other failure, so the caller can say which one it was. The
// reading is the store's one rule: a bounded regular file, never a link.
func readFilters(documents ShellDocuments) (grid.Document, error) {
	data, err := documents.read(filtersName, maxFiltersBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return grid.Empty(), nil
	}
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

// ViewsResult carries one state and the views saved for one project, in the
// order they were saved. A view holds what a person chose to filter by,
// values included, and crosses this boundary for the reason a saved filter
// does: it is their own choice, stored on this machine only because they
// saved it.
type ViewsResult struct {
	State  State       `json:"state"`
	Reason string      `json:"reason,omitzero"`
	Views  []grid.View `json:"views"`
}

func (r refusal) views() ViewsResult {
	return ViewsResult{State: r.state, Reason: r.reason, Views: []grid.View{}}
}

// ListViews reports the views saved for the project open at workspace. Like
// Filters it reads one small local file, and the project's catalog for the
// identity its views are saved under, and does not claim the operation slot.
func (a *App) ListViews(workspace string) ViewsResult {
	root, declined := resolveFolder(workspace)
	if root == "" {
		return declined.views()
	}
	document, declined := a.savedFilters()
	if declined.state != "" {
		return declined.views()
	}
	return listedViews(projectViews(document, root))
}

// viewsKey is what a project's views are saved under: the stable identity the
// application gave the project (#547) once its catalog records one, so a
// project that is renamed or moved keeps its views, and otherwise its resolved
// folder. Views saved under the folder before the project had an identity are
// read under it until a change saves them under the identity.
func viewsKey(root string) (key, folder string) {
	if store, err := catalog.Open(root); err == nil {
		if document, present, err := store.Read(); err == nil && present && document.Project.ID != "" {
			return document.Project.ID, root
		}
	}
	return root, root
}

// projectViews are the views saved for the project at root.
func projectViews(document grid.Document, root string) []grid.View {
	key, folder := viewsKey(root)
	if views := document.ProjectViews(key); len(views) > 0 || key == folder {
		return views
	}
	return document.ProjectViews(folder)
}

// SaveView stores one named query as a view of the project open at
// workspace. A view saved under a name that project already uses replaces
// exactly that view. Saving is the only way a query's values are stored:
// applying one writes nothing. Nothing is written beside the evidence.
func (a *App) SaveView(workspace, name string, query grid.Query) ViewsResult {
	return a.changeViews(workspace, func(views []grid.View) ([]grid.View, refusal) {
		if !grid.ValidViewName(name) {
			return nil, refusal{Failed, "a view is named with 1 to 200 printable characters"}
		}
		if err := grid.ValidateQuery(query); err != nil {
			return nil, refusal{Failed, "the view was not saved: " + err.Error()}
		}
		for i := range views {
			if views[i].Name == name {
				views[i].Query = query
				return views, refusal{}
			}
		}
		if len(views) == grid.MaxViews {
			return nil, refusal{Failed, "this project already holds as many views as this release stores; save over one of them instead"}
		}
		return append(views, grid.View{Name: name, Query: query}), refusal{}
	})
}

// RenameView renames one view of the project open at workspace. A name
// another view of that project already uses is refused rather than merged.
func (a *App) RenameView(workspace, from, to string) ViewsResult {
	return a.changeViews(workspace, func(views []grid.View) ([]grid.View, refusal) {
		if !grid.ValidViewName(to) {
			return nil, refusal{Failed, "a view is named with 1 to 200 printable characters"}
		}
		found := -1
		for i, view := range views {
			switch view.Name {
			case from:
				found = i
			case to:
				return nil, refusal{Failed, "another view of this project already has that name"}
			}
		}
		if found < 0 {
			return nil, refusal{Failed, "that view is not one this project has saved"}
		}
		views[found].Name = to
		return views, refusal{}
	})
}

// RemoveView removes one view of the project open at workspace. It removes the
// saved query only; no evidence is read, changed or removed.
func (a *App) RemoveView(workspace, name string) ViewsResult {
	return a.changeViews(workspace, func(views []grid.View) ([]grid.View, refusal) {
		for i, view := range views {
			if view.Name == name {
				return append(views[:i:i], views[i+1:]...), refusal{}
			}
		}
		return nil, refusal{Failed, "that view is not one this project has saved"}
	})
}

// ClearViews removes every view saved for the project open at workspace. It
// removes the saved queries only: no evidence, index or export is read,
// changed or removed, and the views of every other project are kept.
func (a *App) ClearViews(workspace string) ViewsResult {
	return a.changeViews(workspace, func([]grid.View) ([]grid.View, refusal) {
		return nil, refusal{}
	})
}

// changeViews applies one change to one project's views under the operation
// slot and stores the whole document. A refused change stores nothing and
// reports the views as they were.
func (a *App) changeViews(workspace string, change func([]grid.View) ([]grid.View, refusal)) ViewsResult {
	release, claimed := a.claim("")
	if !claimed {
		return busyRefusal.views()
	}
	defer release()
	root, declined := resolveFolder(workspace)
	if root == "" {
		return declined.views()
	}
	document, declined := a.savedFilters()
	if declined.state != "" {
		return declined.views()
	}
	current := projectViews(document, root)
	changed, declined := change(slices.Clone(current))
	if declined.state != "" {
		result := listedViews(current)
		result.State, result.Reason = declined.state, declined.reason
		return result
	}
	if key, folder := viewsKey(root); key != folder {
		document = document.WithProjectViews(folder, nil).WithProjectViews(key, changed)
	} else {
		document = document.WithProjectViews(root, changed)
	}
	if len(document.Views) > grid.MaxViewProjects {
		result := listedViews(current)
		result.State, result.Reason = Failed, "views are already saved for as many projects as this release stores"
		return result
	}
	data, err := grid.Encode(document)
	if err != nil {
		result := listedViews(current)
		result.State, result.Reason = Failed, "these views no longer fit the bounded document this release writes; save a shorter one, or remove one"
		return result
	}
	if err := a.documents.write(filtersName, data); err != nil {
		result := listedViews(current)
		result.State, result.Reason = Failed, "the view could not be stored; an interrupted write may be retained beside the saved filters"
		if errors.Is(err, fs.ErrPermission) {
			result.State, result.Reason = PermissionDenied, "this account cannot write the saved views"
		}
		return result
	}
	return listedViews(changed)
}

func listedViews(views []grid.View) ViewsResult {
	if len(views) == 0 {
		return ViewsResult{State: Empty, Reason: "no view has been saved for this project", Views: []grid.View{}}
	}
	return ViewsResult{State: Completed, Views: views}
}
