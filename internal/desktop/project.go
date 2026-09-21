package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
)

// RegisteredCase is one registered case of the project overview: the metadata
// the project records, beside what re-verifying its evidence just found. The
// evidence facts are reported exactly as recorded, whatever the verification
// found; the state says which of them still hold.
type RegisteredCase struct {
	Name             string   `json:"name"`
	Identity         string   `json:"identity"`
	Schema           string   `json:"schema"`
	Provenance       string   `json:"provenance"`
	InterfaceVersion string   `json:"interface_version"`
	Title            string   `json:"title"`
	Status           string   `json:"status"`
	Owner            string   `json:"owner,omitzero"`
	Tags             []string `json:"tags"`
	Incidents        []string `json:"incidents"`
	Evidence         string   `json:"evidence"`
}

// RegisteredRevision is one registered revision of the project overview, with
// the lineage the editable document records and the same evidence state a
// registered case carries.
type RegisteredRevision struct {
	Name       string `json:"name"`
	Identity   string `json:"identity"`
	Schema     string `json:"schema"`
	Provenance string `json:"provenance"`
	Operation  string `json:"operation"`
	Parent     string `json:"parent"`
	Evidence   string `json:"evidence"`
}

// ProjectOverview is what the project holds, re-read from disk: the settings,
// every registered case and revision with its evidence state, and every note.
// It is the `readmit project show` of this window, written as one typed value.
type ProjectOverview struct {
	Root              string               `json:"root"`
	Title             string               `json:"title"`
	DefaultOwner      string               `json:"default_owner,omitzero"`
	DefaultVersion    string               `json:"default_version,omitzero"`
	InterfaceVersions []string             `json:"interface_versions"`
	Cases             []RegisteredCase     `json:"cases"`
	Revisions         []RegisteredRevision `json:"revisions"`
	Notes             []project.Note       `json:"notes"`
}

// ProjectOverviewResult carries one state. Overview is present whenever both
// project documents were read, even when the state is Empty because the
// project registers nothing yet: the overview a person acts from is the answer
// even when the action is to register the first case.
type ProjectOverviewResult struct {
	State    State            `json:"state"`
	Reason   string           `json:"reason,omitzero"`
	Overview *ProjectOverview `json:"overview,omitzero"`
}

func (r *ProjectOverviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

func (r refusal) projectOverview() ProjectOverviewResult {
	return ProjectOverviewResult{State: r.state, Reason: r.reason}
}

// readOverview re-reads both documents of a project and re-verifies every
// registered entry through the same reader the command line's project show
// uses. It is the one place the window asks what the project now holds, so a
// panel is never left holding state from documents that have since changed.
func (a *App) readOverview(path string) ProjectOverviewResult {
	opened, declined := openProjectFolder(path)
	if opened == nil {
		return declined.projectOverview()
	}
	revisions, declined := readRevisions(opened.Root)
	if revisions == nil {
		return declined.projectOverview()
	}
	overview := &ProjectOverview{
		Root:              opened.Root,
		Title:             opened.Document.Settings.Title,
		DefaultOwner:      opened.Document.Settings.DefaultOwner,
		DefaultVersion:    opened.Document.Settings.DefaultInterfaceVersion,
		InterfaceVersions: opened.Document.InterfaceVersions,
		Cases:             []RegisteredCase{},
		Revisions:         []RegisteredRevision{},
		Notes:             orNotes(revisions.Notes),
	}
	for _, entry := range opened.Document.Cases {
		overview.Cases = append(overview.Cases, RegisteredCase{
			Name:             entry.Name,
			Identity:         entry.Identity,
			Schema:           entry.Schema,
			Provenance:       entry.Provenance,
			InterfaceVersion: entry.InterfaceVersion,
			Title:            entry.Title,
			Status:           string(entry.Status),
			Owner:            entry.Owner,
			Tags:             orEmpty(entry.Tags),
			Incidents:        orEmpty(entry.Incidents),
			Evidence: operation.EvidenceState(opened.Root, operation.Facts{
				Name: entry.Name, Identity: entry.Identity, Schema: entry.Schema, Provenance: entry.Provenance,
			}),
		})
	}
	for _, entry := range revisions.Revisions {
		overview.Revisions = append(overview.Revisions, RegisteredRevision{
			Name:       entry.Name,
			Identity:   entry.Identity,
			Schema:     entry.Schema,
			Provenance: entry.Provenance,
			Operation:  entry.Operation.Name,
			Parent:     entry.Operation.Parent,
			Evidence: operation.EvidenceState(opened.Root, operation.Facts{
				Name: entry.Name, Identity: entry.Identity, Schema: entry.Schema, Provenance: entry.Provenance,
			}),
		})
	}
	return ProjectOverviewResult{State: overviewState(overview), Overview: overview}
}

// overviewState reports Empty for a project that registers nothing yet, the
// same distinction OpenProject draws: the operation succeeded and there is
// nothing to show, which is a state a person is on the way out of.
func overviewState(overview *ProjectOverview) State {
	if len(overview.Cases) == 0 && len(overview.Revisions) == 0 && len(overview.Notes) == 0 {
		return Empty
	}
	return Completed
}

func orNotes(notes []project.Note) []project.Note {
	if notes == nil {
		return []project.Note{}
	}
	return notes
}

func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// refreshOverview re-reads the project after a successful write and reports
// the result as Completed: the overview is present, so there is something to
// show even when the project registers nothing yet. Empty is the state of a
// read that found nothing, and a person who just wrote something is looking
// at what they wrote.
func (a *App) refreshOverview(path string) ProjectOverviewResult {
	result := a.readOverview(path)
	if result.State == Empty && result.Overview != nil {
		result.State = Completed
	}
	return result
}

// OpenProjectOverview reads a project and re-verifies every registered case
// and revision, exactly as the project show command does over the same
// documents. It writes nothing. It runs to completion once it starts, so it
// holds the operation slot but is not interruptible.
func (a *App) OpenProjectOverview(path string) ProjectOverviewResult {
	return run(a, false, false, func(context.Context) ProjectOverviewResult {
		return a.readOverview(path)
	})
}

// CreateProject asks the host for the folder to create the project in, then
// writes a new project directory named by `name` holding its first document,
// through the same shared operation `readmit project init` runs. The first
// declared interface version becomes the project default. The destination must
// be new: an entry already there is refused and left exactly as it was. The
// dialog can be cancelled, so the operation is interruptible, and what it
// returns is the new project re-read from disk.
func (a *App) CreateProject(name, title, owner string, versions []string) ProjectOverviewResult {
	return run(a, true, true, func(ctx context.Context) ProjectOverviewResult {
		if strings.TrimSpace(name) == "" {
			return ProjectOverviewResult{State: Failed, Reason: "the new project needs a folder name"}
		}
		if len(versions) == 0 {
			return ProjectOverviewResult{State: Failed, Reason: "a new project declares at least one interface version"}
		}
		for _, version := range versions {
			if strings.TrimSpace(version) == "" {
				return ProjectOverviewResult{State: Failed, Reason: "an interface version must not be empty"}
			}
		}
		parent, declined := a.chooseFolder(ctx, "Choose a folder for the new project")
		if parent == "" {
			return ProjectOverviewResult{State: declined.state, Reason: declined.reason}
		}
		// The same shared writer `readmit project init` runs. The first
		// declared interface version becomes the project default.
		created, err := project.Create(filepath.Join(parent, name), project.Document{
			Schema:            project.Schema,
			Settings:          project.Settings{Title: title, DefaultOwner: owner, DefaultInterfaceVersion: versions[0]},
			InterfaceVersions: versions,
		})
		if err != nil {
			return probeWriteFailure(parent,
				"this account cannot create a folder in the chosen folder",
				"the project could not be created there; the destination must be a new folder in a folder this account can write").projectOverview()
		}
		return a.refreshOverview(created.Root)
	})
}

// SettingsChange is one project-settings edit through the window. A member
// left null is left exactly as it was; declare_versions names further
// interface versions to declare, and one already declared is left as it was.
// A declared version is never removed, because registered cases still name it.
type SettingsChange struct {
	Title                   *string   `json:"title,omitzero"`
	DefaultOwner            *string   `json:"default_owner,omitzero"`
	DefaultInterfaceVersion *string   `json:"default_interface_version,omitzero"`
	DeclareVersions         *[]string `json:"declare_versions,omitzero"`
}

// UpdateProjectSettings changes the settings of the project at `path` through
// the shared operation `readmit project settings` runs, and returns the
// project re-read from disk, so the window renders what is stored rather than
// what the edit hoped for.
func (a *App) UpdateProjectSettings(path string, change SettingsChange) ProjectOverviewResult {
	return run(a, false, true, func(context.Context) ProjectOverviewResult {
		if _, err := operation.UpdateProjectSettings(path, operation.SettingsChange{
			Title:                   change.Title,
			DefaultOwner:            change.DefaultOwner,
			DefaultInterfaceVersion: change.DefaultInterfaceVersion,
			DeclareVersions:         deref(change.DeclareVersions),
		}); err != nil {
			return refusedOverview(path, err)
		}
		return a.refreshOverview(path)
	})
}

// CaseRegistration is the metadata a person supplies when registering a case.
// A member left empty inherits the project default, exactly as an absent flag
// does on the command line. The evidence facts are never taken from here.
type CaseRegistration struct {
	Title            string   `json:"title,omitzero"`
	Owner            string   `json:"owner,omitzero"`
	Status           string   `json:"status,omitzero"`
	InterfaceVersion string   `json:"interface_version,omitzero"`
	Tags             []string `json:"tags,omitzero"`
	Incidents        []string `json:"incidents,omitzero"`
}

// RegisterCase registers one case of the project through the shared operation
// `readmit project add` runs: the bundle named by `name`, one directory entry
// of the project, is verified through the shared reader and the facts it
// declared are what get recorded. Derived evidence is refused with the
// project's own reason, because registering it as a case would lose its
// lineage; it is registered as a revision from the command line instead.
func (a *App) RegisterCase(path, name string, registration CaseRegistration) ProjectOverviewResult {
	return run(a, false, true, func(context.Context) ProjectOverviewResult {
		if _, err := operation.RegisterCase(path, name, operation.CaseRegistration{
			Title:            registration.Title,
			Owner:            registration.Owner,
			Status:           project.Status(registration.Status),
			InterfaceVersion: registration.InterfaceVersion,
			Tags:             registration.Tags,
			Incidents:        registration.Incidents,
		}); err != nil {
			return refusedOverview(path, err)
		}
		return a.refreshOverview(path)
	})
}

// CaseChange is the mutable metadata of one registered case. A member left
// null is left exactly as it was, so changing a status does not restate the
// tags. No member can reach the recorded evidence facts.
type CaseChange struct {
	Title            *string   `json:"title,omitzero"`
	Owner            *string   `json:"owner,omitzero"`
	Status           *string   `json:"status,omitzero"`
	InterfaceVersion *string   `json:"interface_version,omitzero"`
	Tags             *[]string `json:"tags,omitzero"`
	Incidents        *[]string `json:"incidents,omitzero"`
}

// UpdateRegisteredCase changes the title, owner, status, interface version,
// tags or linked incidents of one registered case through the shared operation
// `readmit project update` runs, and returns the project re-read from disk.
func (a *App) UpdateRegisteredCase(path, name string, change CaseChange) ProjectOverviewResult {
	return run(a, false, true, func(context.Context) ProjectOverviewResult {
		var status *project.Status
		if change.Status != nil {
			value := project.Status(*change.Status)
			status = &value
		}
		if _, err := operation.UpdateRegisteredCase(path, name, operation.CaseChange{
			Title:            change.Title,
			Owner:            change.Owner,
			Status:           status,
			InterfaceVersion: change.InterfaceVersion,
			Tags:             change.Tags,
			Incidents:        change.Incidents,
		}); err != nil {
			return refusedOverview(path, err)
		}
		return a.refreshOverview(path)
	})
}

func deref(values *[]string) []string {
	if values == nil {
		return nil
	}
	return *values
}

// refusedOverview maps a shared operation's refusal to the overview result the
// window reports. A project or editable document written by a version this
// release cannot read is named as such; a folder this account cannot write is
// separated from the rest by probing, after the write has already failed; and
// a change the project refused is reported in the project's own sentence,
// which names the member at fault and never repeats a value or a path.
func refusedOverview(path string, err error) ProjectOverviewResult {
	switch {
	case errors.Is(err, operation.ErrProjectOpen):
		if errors.Is(err, project.ErrUnsupportedVersion) {
			return ProjectOverviewResult{State: Failed, Reason: "the project document was written by a version this release cannot read"}
		}
		return probeReadFailure(path).projectOverview()
	case errors.Is(err, operation.ErrProjectRevisions):
		if errors.Is(err, project.ErrUnsupportedVersion) {
			return ProjectOverviewResult{State: Failed, Reason: "the editable project document was written by a version this release cannot read"}
		}
		return ProjectOverviewResult{State: Failed, Reason: "the editable project document cannot be read"}
	case errors.Is(err, operation.ErrProjectWrite):
		return probeWriteFailure(path,
			"this account cannot write to the project folder",
			"the project could not be updated; an interrupted write may be retained beside the project document").projectOverview()
	case errors.Is(err, operation.ErrProjectInvalid):
		detail, _ := operation.InvalidDetail(err)
		return ProjectOverviewResult{State: Failed, Reason: detail}
	case errors.Is(err, operation.ErrCaseUnverified):
		return ProjectOverviewResult{State: Failed, Reason: err.Error()}
	}
	return ProjectOverviewResult{State: Failed, Reason: err.Error()}
}
