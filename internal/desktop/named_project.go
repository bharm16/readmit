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

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/project"
)

// A project is created by naming it. Its folder is the application's to
// choose, inside the one folder this viewer keeps projects in; its display
// name is the project document's title, and its identity the catalog's. The
// three are separate: renaming changes the title only, and moving the folder
// changes neither the identity nor any byte inside it.

// projectsSchema is the versioned contract of the shell document that
// remembers the folder new projects are created in and every project this
// viewer has opened: its identity, the folder it was last opened from, its
// name then, and when. It holds no evidence.
const projectsSchema = "readmit-desktop-projects/v1"

// MaxKnownProjects bounds the projects one viewer remembers; the least
// recently opened is forgotten first.
const MaxKnownProjects = 64

const maxProjectsBytes = 256 << 10

type projectsDocument struct {
	Schema   string         `json:"schema"`
	Location string         `json:"location,omitzero"`
	Projects []knownProject `json:"projects"`
}

type knownProject struct {
	ID       string `json:"id"`
	Folder   string `json:"folder"`
	Name     string `json:"name"`
	OpenedAt string `json:"opened_at,omitzero"`
}

func (a *App) readProjects() (projectsDocument, error) {
	data, err := a.documents.read(projectsName, maxProjectsBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return projectsDocument{Schema: projectsSchema, Projects: []knownProject{}}, nil
	}
	if err != nil {
		return projectsDocument{}, err
	}
	var document projectsDocument
	if err := json.Unmarshal(data, &document, json.RejectUnknownMembers(true)); err != nil || document.Schema != projectsSchema {
		return projectsDocument{}, errNotADocument
	}
	if document.Location != "" && !filepath.IsAbs(document.Location) || len(document.Projects) > MaxKnownProjects {
		return projectsDocument{}, errNotADocument
	}
	for _, known := range document.Projects {
		if !catalog.ValidID(known.ID) || !filepath.IsAbs(known.Folder) || !printable(known.Folder, maxRootBytes) || !catalog.ValidName(known.Name) {
			return projectsDocument{}, errNotADocument
		}
	}
	return document, nil
}

func (a *App) writeProjects(document projectsDocument) error {
	if document.Projects == nil {
		document.Projects = []knownProject{}
	}
	data, err := json.Marshal(document, json.Deterministic(true))
	if err != nil {
		return err
	}
	return a.documents.write(projectsName, append(data, '\n'))
}

// rememberProject records where the project with id is, under the name it
// has now. A folder holds one project, so another identity remembered at the
// same folder is forgotten. A document this release cannot read is left as
// it is: remembering is the viewer's convenience, and a failure to remember
// never unsays what was done to the project itself.
func (a *App) rememberProject(id, folder, name string) {
	a.remember(knownProject{ID: id, Folder: folder, Name: name}, false)
}

// rememberOpened is rememberProject that also records when the project was
// opened.
func (a *App) rememberOpened(id, folder, name string) {
	a.remember(knownProject{ID: id, Folder: folder, Name: name}, true)
}

func (a *App) remember(entry knownProject, opened bool) {
	if !catalog.ValidID(entry.ID) || !catalog.ValidName(entry.Name) {
		return
	}
	a.projectsMu.Lock()
	defer a.projectsMu.Unlock()
	document, err := a.readProjects()
	if err != nil {
		return
	}
	if at := slices.IndexFunc(document.Projects, func(known knownProject) bool { return known.ID == entry.ID }); at >= 0 {
		entry.OpenedAt = document.Projects[at].OpenedAt
		if document.Projects[at] == entry && !opened {
			return
		}
	} else if !opened {
		// Only opening a project adds it: reading one this viewer forgot
		// does not bring it back.
		return
	}
	if opened {
		entry.OpenedAt = catalog.Stamp(a.now())
	}
	document.Projects = slices.DeleteFunc(document.Projects, func(known knownProject) bool { return known.ID == entry.ID || known.Folder == entry.Folder })
	document.Projects = append([]knownProject{entry}, document.Projects...)
	if len(document.Projects) > MaxKnownProjects {
		document.Projects = document.Projects[:MaxKnownProjects]
	}
	a.writeProjects(document)
}

// rememberProjectAt records, as opened now, the project a folder holds when
// its catalog has recorded the project's identity. A folder that holds no
// project, or one whose identity is not recorded yet, is remembered nowhere:
// opening it writes nothing into it.
func (a *App) rememberProjectAt(folder string) {
	opened, err := project.Open(folder)
	if err != nil {
		return
	}
	store, err := catalog.Open(opened.Root)
	if err != nil {
		return
	}
	if document, present, err := store.Read(); err == nil && present {
		a.rememberOpened(document.Project.ID, opened.Root, opened.Document.Settings.Title)
	}
}

// knownProjects reads every remembered project where it was last opened:
// its project document and the identity its catalog records, and nothing
// else, so the list of projects stays cheap however large each one is.
func (a *App) knownProjects(ctx context.Context) []CatalogItem {
	document, err := a.readProjects()
	if err != nil {
		return []CatalogItem{}
	}
	admitted := a.admitted(ctx)
	items := []CatalogItem{}
	for _, known := range document.Projects {
		item := CatalogItem{Ref: ItemRef{Kind: ProjectItem, ID: known.ID}, Name: known.Name, ProjectID: known.ID,
			Summary: ItemSummary{Project: &ProjectSummary{Folder: known.Folder, InterfaceVersions: []string{}, Tags: []string{}, Revisions: []InterfaceRevision{}}}}
		opened, catalogDocument, reason, err := projectAt(known.Folder, known.ID)
		switch {
		case opened != nil:
			item = summarized(opened, catalogDocument, true)
		case isMissing(known.Folder):
			item.Availability, item.Reason = ItemMissing, "the project is no longer in the folder it was last opened from"
		case errors.Is(err, project.ErrUnsupportedVersion):
			item.Availability, item.Reason = ItemUnsupported, reason
		default:
			item.Availability, item.Reason = ItemUnreadable, reason
		}
		item.Capabilities = capabilitiesFor(ProjectItem, item.Availability, admitted)
		item.LastOpenedAt = stamped(known.OpenedAt)
		items = append(items, item)
	}
	return items
}

// rememberedFolder is the folder the project with id was last opened from,
// or empty when this viewer has not opened it.
func (a *App) rememberedFolder(id string) string {
	document, err := a.readProjects()
	if err != nil {
		return ""
	}
	for _, known := range document.Projects {
		if known.ID == id {
			return known.Folder
		}
	}
	return ""
}

// projectAt reads the project document in folder and the catalog that must
// record the identity id, or says why it could not.
func projectAt(folder, id string) (*project.Project, catalog.Document, string, error) {
	opened, err := project.Open(folder)
	if errors.Is(err, project.ErrUnsupportedVersion) {
		return nil, catalog.Document{}, "the project document was written by a version this release cannot read", err
	}
	if err != nil {
		return nil, catalog.Document{}, "the folder holds no project document this release reads", err
	}
	store, err := catalog.Open(opened.Root)
	if err != nil {
		return nil, catalog.Document{}, "the folder holds no project document this release reads", err
	}
	document, present, err := store.Read()
	switch {
	case errors.Is(err, catalog.ErrUnsupportedVersion):
		return nil, catalog.Document{}, "the project's catalog was written by a version this release cannot read", err
	case err != nil:
		return nil, catalog.Document{}, "the project's catalog cannot be read; it is left exactly as written", err
	case !present || document.Project.ID != id:
		return nil, catalog.Document{}, "the folder it was last opened from now holds a different project", nil
	}
	return opened, document, "", nil
}

// projectItem is one loaded project as the catalog lists it.
func projectItem(loaded *loadedCatalog) CatalogItem {
	item := summarized(loaded.project, loaded.document, loaded.recorded)
	item.Capabilities = capabilitiesFor(ProjectItem, ItemAvailable, admissions{author: loaded.author, execute: loaded.execute})
	return item
}

// summarized is a project as the catalog lists it: its title, its identity,
// where it is and what its document declares.
func summarized(opened *project.Project, document catalog.Document, recorded bool) CatalogItem {
	item := CatalogItem{Ref: ItemRef{Kind: ProjectItem, ID: document.Project.ID, Revision: projectRevision(opened.Document)}, Name: opened.Document.Settings.Title,
		ProjectID: document.Project.ID, Availability: ItemAvailable, Capabilities: []ActionID{},
		Summary: ItemSummary{Project: &ProjectSummary{Folder: opened.Root, Schema: opened.Document.Schema, Cases: len(opened.Document.Cases),
			InterfaceVersions: orEmpty(opened.Document.InterfaceVersions), Owner: opened.Document.Settings.DefaultOwner,
			Tags: orEmpty(opened.Document.Settings.Tags), Revisions: interfaceRevisions(opened.Document)}}}
	if recorded {
		item.CreatedAt = stamped(document.Project.RecordedAt)
	}
	return item
}

func isMissing(path string) bool {
	_, err := os.Lstat(path)
	return errors.Is(err, fs.ErrNotExist)
}

// NewProjectRequest names a new project. The name is all a person gives:
// its folder is generated in the remembered projects folder, which
// ChooseProjectLocation chose beforehand.
type NewProjectRequest struct {
	Name string `json:"name"`
}

// ProjectOpenResult carries one project as the catalog lists it, and the
// context later requests about it carry.
type ProjectOpenResult struct {
	State    State          `json:"state"`
	Reason   string         `json:"reason,omitzero"`
	Context  RequestContext `json:"context"`
	Recorded bool           `json:"recorded"`
	Project  *CatalogItem   `json:"project,omitzero"`
}

func (r *ProjectOpenResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// CreateNamedProject creates a project from a name alone: a new
// readmit-project/v2 document with no interface revision declared and none
// invented, in a new folder the application names in the remembered projects
// folder, and the project's catalog with its new identity. Two projects may
// share a name; they never share a folder or an identity. No dialog opens:
// a projects folder that is not remembered, no longer there, or not
// writable is refused with that reason, and it is never created.
func (a *App) CreateNamedProject(request NewProjectRequest) ProjectOpenResult {
	return run(a, true, true, func(ctx context.Context) ProjectOpenResult {
		name := strings.TrimSpace(request.Name)
		if !validName(name, func(name string) error { return project.CheckTitle(project.SchemaV2, name) }) {
			return ProjectOpenResult{State: Failed, Reason: nameRule}
		}
		parent, declined := a.usableLocation()
		if parent == "" {
			return ProjectOpenResult{State: declined.state, Reason: declined.reason}
		}
		folder, err := freeFolder(parent, name)
		if err != nil {
			return ProjectOpenResult{State: Failed, Reason: err.Error()}
		}
		created, err := project.Create(folder, project.Document{Schema: project.SchemaV2, Settings: project.Settings{Title: name}})
		if err != nil {
			return probeWriteFailure(parent,
				"this account cannot create a folder in the projects folder",
				"the project could not be created in the projects folder").namedProject()
		}
		return a.openNamed(ctx, created.Root, true)
	})
}

// OpenNamedProject opens the project in a folder. A project the catalog
// has not recorded yet, or one an interruption left a save in, is recorded
// and settled first, which needs the author admission; without it the
// project still opens, read as it is, and Recorded says its identity is not
// recorded yet. A project that was moved is the same project, under the
// same identity, in its new folder.
func (a *App) OpenNamedProject(path string) ProjectOpenResult {
	admitted := a.needsRecording(path)
	opened := run(a, false, admitted, func(ctx context.Context) ProjectOpenResult {
		// Asked again holding the slot: a project that came to need
		// recording since is opened as it is, never written without the
		// admission taken for it.
		return a.openNamed(ctx, path, admitted && a.needsRecording(path))
	})
	if admitted && opened.State == PermissionDenied {
		return run(a, false, false, func(ctx context.Context) ProjectOpenResult {
			return a.openNamed(ctx, path, false)
		})
	}
	return opened
}

// needsRecording reports whether opening the project in path writes: it has
// no catalog yet, or holds an interrupted save.
func (a *App) needsRecording(path string) bool {
	store, err := catalog.Open(path)
	if err != nil {
		return false
	}
	_, present, err := store.Read()
	return err == nil && (!present || len(store.PendingFiles()) > 0)
}

func (a *App) openNamed(ctx context.Context, path string, record bool) ProjectOpenResult {
	loaded, declined := a.loadCatalog(ctx, RequestContext{Project: path}, record)
	if loaded == nil {
		return declined.namedProject()
	}
	item := projectItem(loaded)
	if !loaded.recorded {
		item.Ref.ID, item.ProjectID = "", ""
		return ProjectOpenResult{State: Completed, Context: RequestContext{Project: loaded.root}, Project: &item}
	}
	a.rememberOpened(item.Ref.ID, loaded.root, item.Name)
	item.LastOpenedAt = stamped(catalog.Stamp(a.now()))
	return ProjectOpenResult{State: Completed, Recorded: true, Context: RequestContext{Project: loaded.root, ProjectID: item.Ref.ID}, Project: &item}
}

func (r refusal) namedProject() ProjectOpenResult {
	return ProjectOpenResult{State: r.state, Reason: r.reason}
}

// unreadableProjects refuses a remembered projects document this release
// cannot read.
var unreadableProjects = refusal{Failed, "the remembered projects folder cannot be read; it is left exactly as written"}

// usableLocation is the remembered projects folder when it is still an
// existing folder, not a link, that this account can write in. Anything else
// is refused with its reason, and the remembered value is left as it is.
func (a *App) usableLocation() (string, refusal) {
	document, err := a.readProjects()
	if err != nil {
		return "", unreadableProjects
	}
	if document.Location == "" {
		return "", refusal{Failed, "no folder is chosen to keep projects in; choose one first"}
	}
	if info, err := os.Lstat(document.Location); err != nil || !info.IsDir() {
		return "", refusal{Failed, "the folder projects are kept in is no longer there; choose one again"}
	}
	probe, err := os.MkdirTemp(document.Location, ".readmit-access-")
	if err != nil {
		return "", refusal{PermissionDenied, "this account cannot create a folder in the folder projects are kept in; choose another"}
	}
	os.Remove(probe)
	return document.Location, refusal{}
}

func (a *App) rememberLocation(folder string) error {
	a.projectsMu.Lock()
	defer a.projectsMu.Unlock()
	document, err := a.readProjects()
	if err != nil {
		return err
	}
	document.Location = folder
	return a.writeProjects(document)
}

// ProjectLocationResult carries the remembered projects folder.
type ProjectLocationResult struct {
	State    State  `json:"state"`
	Reason   string `json:"reason,omitzero"`
	Location string `json:"location,omitzero"`
}

func (r *ProjectLocationResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ChooseProjectLocation asks the host for the folder new projects are
// created in and remembers it. Nothing is created or moved.
func (a *App) ChooseProjectLocation() ProjectLocationResult {
	return run(a, true, false, func(ctx context.Context) ProjectLocationResult {
		folder, declined := a.chooseFolder(ctx, "Choose where projects are kept")
		if folder == "" {
			return ProjectLocationResult{State: declined.state, Reason: declined.reason}
		}
		if err := a.rememberLocation(folder); err != nil {
			return ProjectLocationResult{State: Failed, Reason: "the projects folder could not be remembered"}
		}
		return ProjectLocationResult{State: Completed, Location: folder}
	})
}

// ProjectLocation reports the remembered projects folder while a new project
// can be created in it: it is still there, a folder, and writable. Otherwise
// it is Empty with the reason, and the remembered value is left as it is.
func (a *App) ProjectLocation() ProjectLocationResult {
	folder, declined := a.usableLocation()
	switch {
	case folder != "":
		return ProjectLocationResult{State: Completed, Location: folder}
	case declined == unreadableProjects:
		return ProjectLocationResult{State: Failed, Reason: declined.reason}
	}
	return ProjectLocationResult{State: Empty, Reason: declined.reason}
}

// freeFolder names a new folder for a project called name inside parent: the
// name's letters and digits, then -2, -3 and so on until no entry holds it.
// The folder is the application's; nothing is concluded from it later.
func freeFolder(parent, name string) (string, error) {
	var base strings.Builder
	dash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			base.WriteRune(r)
			dash = false
		case !dash && base.Len() > 0:
			base.WriteByte('-')
			dash = true
		}
		if base.Len() >= 48 {
			break
		}
	}
	stem := strings.Trim(base.String(), "-")
	if stem == "" {
		stem = "project"
	}
	for i := 1; i <= 999; i++ {
		candidate := stem
		if i > 1 {
			candidate += "-" + strconv.Itoa(i)
		}
		path := filepath.Join(parent, candidate)
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", errors.New("the projects folder cannot be read")
		}
	}
	return "", errors.New("the projects folder holds more projects of this name than this release numbers")
}

// MigrateProjectDocument converts a readmit-project/v1 project to
// readmit-project/v2, which is what lets it hold no interface revision or a
// case with none assigned. It is an explicit act, never a side effect of
// reading: every member is kept, only the declared contract changes, and the
// v1 bytes are retained as the document's recovery copy.
func (a *App) MigrateProjectDocument(path string) ProjectOverviewResult {
	return run(a, false, true, func(context.Context) ProjectOverviewResult {
		opened, declined := openProjectFolder(path)
		if opened == nil {
			return declined.projectOverview()
		}
		migrated, err := project.Migrate(opened.Document)
		if errors.Is(err, project.ErrAlreadyCurrent) {
			return ProjectOverviewResult{State: Failed, Reason: "the project already declares the current project contract"}
		}
		if err != nil {
			return ProjectOverviewResult{State: Failed, Reason: "the project document cannot be migrated"}
		}
		if err := opened.Save(migrated); err != nil {
			return probeWriteFailure(opened.Root,
				"this account cannot write to the project folder",
				"the project could not be migrated; its v1 document is unchanged").projectOverview()
		}
		return a.refreshOverview(opened.Root)
	})
}
