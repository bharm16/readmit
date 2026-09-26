package desktop

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/reproducer"
)

// RegisteredCase is one registered case of the project overview: the metadata
// the project records, beside what re-verifying its evidence just found. The
// evidence facts are reported exactly as recorded, whatever the verification
// found; the state says which of them still hold.
type RegisteredCase struct {
	Name             string         `json:"name"`
	Identity         string         `json:"identity"`
	Schema           string         `json:"schema"`
	Provenance       string         `json:"provenance"`
	InterfaceVersion string         `json:"interface_version"`
	Title            string         `json:"title"`
	Status           project.Status `json:"status"`
	Owner            string         `json:"owner,omitzero"`
	Tags             []string       `json:"tags"`
	Incidents        []string       `json:"incidents"`
	Evidence         string         `json:"evidence"`
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
			Status:           entry.Status,
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

// RevisionRegistration registers one derived case as a revision of a registered
// case or revision. Name is the project entry that holds the derived case.
// Source, when set, is a built reproducer folder of the same project: its
// derived case is copied into Name first, exactly as the documented
// `cp -R …/case` step does on the command line, and the build folder is left
// intact so comparison can still open it. Nothing here rewrites the parent.
type RevisionRegistration struct {
	Workspace string `json:"workspace"`
	Name      string `json:"name"`
	Parent    string `json:"parent"`
	Source    string `json:"source,omitzero"`
}

// RegisterRevision registers derived evidence as a revision through the shared
// operation `readmit project revise` runs, and returns the project re-read from
// disk. When Source names a built reproducer folder, the derived case inside it
// is copied into Name first; an entry that already exists is refused rather
// than replaced. Original evidence is never touched.
//
// A copy this call placed is removed again when the project refuses to record
// it, so a refused registration leaves the workspace exactly as it was and the
// same name can be used again once the refusal is dealt with. Nothing else is
// ever removed: the copy was created by this call and recorded nowhere.
func (a *App) RegisterRevision(request RevisionRegistration) ProjectOverviewResult {
	return run(a, false, true, func(context.Context) ProjectOverviewResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return declined.projectOverview()
		}
		if request.Parent == "" {
			return ProjectOverviewResult{State: Failed, Reason: "a revision names the registered case or revision it was derived from"}
		}
		placed := ""
		if request.Source != "" {
			copied, err := placeDerivedCase(root, request.Source, request.Name)
			if err != nil {
				if errors.Is(err, fs.ErrPermission) {
					return ProjectOverviewResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
				}
				return ProjectOverviewResult{State: Failed, Reason: err.Error()}
			}
			placed = copied
		}
		if _, err := operation.RegisterRevision(root, request.Name, request.Parent); err != nil {
			refused := refusedOverview(root, err)
			if placed != "" {
				refused.Reason = withdrawn(placed, refused.Reason)
			}
			return refused
		}
		return a.refreshOverview(root)
	})
}

// placeDerivedCase copies the derived case out of a built reproducer folder
// into one new entry of the project and returns where it placed it. The build
// folder itself stays where it is, and an entry it created is removed again
// when the copy does not verify.
func placeDerivedCase(root, source, destination string) (string, error) {
	built, err := artifactpath.Child(root, source)
	if err != nil {
		return "", errors.New("a built reproducer must be named by one directory entry of the open workspace")
	}
	if err := artifactpath.EntryName(destination); err != nil {
		return "", errors.New("a revision must be named by one directory entry of the project")
	}
	from := filepath.Join(built, reproducer.CaseName)
	if _, err := bundle.Open(from); err != nil {
		return "", errors.New("the built reproducer's derived case could not be verified as complete, unmodified evidence")
	}
	to := artifactpath.JoinReference(root, destination)
	if _, err := os.Lstat(to); err == nil {
		return "", errors.New("that name is already an entry of this workspace")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("the revision folder could not be created in the open workspace")
	}
	if err := os.Mkdir(to, 0700); err != nil {
		return "", err
	}
	if err := os.CopyFS(to, os.DirFS(from)); err != nil {
		return "", errors.New(withdrawn(to, "the derived case could not be placed beside the project evidence"))
	}
	if _, err := bundle.Open(to); err != nil {
		return "", errors.New(withdrawn(to, "the placed derived case could not be verified as complete, unmodified evidence"))
	}
	return to, nil
}

// withdrawn removes an entry this call placed and the project does not hold,
// and returns the refusal it was placed for. A copy that could not be removed
// is named in the refusal rather than left behind unmentioned.
func withdrawn(placed, reason string) string {
	if err := os.RemoveAll(placed); err != nil {
		return reason + "; the derived case placed for it could not be removed and is still an entry of the workspace"
	}
	return reason
}
