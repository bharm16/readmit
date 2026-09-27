package desktop

import (
	"encoding/json/v2"
	"errors"
	"io/fs"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/catalog"
)

// When a person last opened an object is this viewer's own record, like the
// projects this viewer opened: reading a project never writes into it, so the
// time is kept beside the shell's other documents, by project identity and
// object identity, and nothing else.

// openedSchema is the versioned contract of the shell document that records
// when this viewer last opened each object of each project.
const openedSchema = "readmit-desktop-opened/v1"

// MaxOpenedItems bounds the objects of one project whose last opening is
// remembered; the least recently opened is forgotten first. The projects are
// bounded as the remembered projects are, by MaxKnownProjects.
const MaxOpenedItems = 512

const maxOpenedBytes = 4 << 20

type openedDocument struct {
	Schema   string          `json:"schema"`
	Projects []openedProject `json:"projects"`
}

// openedProject is one project's opened objects, the most recently opened
// first.
type openedProject struct {
	ID    string       `json:"id"`
	Items []openedItem `json:"items"`
}

type openedItem struct {
	ID       string `json:"id"`
	OpenedAt string `json:"opened_at"`
}

func (a *App) readOpened() (openedDocument, error) {
	data, err := a.documents.read(openedName, maxOpenedBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return openedDocument{Schema: openedSchema, Projects: []openedProject{}}, nil
	}
	if err != nil {
		return openedDocument{}, err
	}
	var document openedDocument
	if err := json.Unmarshal(data, &document, json.RejectUnknownMembers(true)); err != nil || document.Schema != openedSchema ||
		len(document.Projects) > MaxKnownProjects {
		return openedDocument{}, errNotADocument
	}
	for _, project := range document.Projects {
		if !catalog.ValidID(project.ID) || len(project.Items) > MaxOpenedItems {
			return openedDocument{}, errNotADocument
		}
		for _, item := range project.Items {
			if _, err := time.Parse(time.RFC3339, item.OpenedAt); !catalog.ValidID(item.ID) || err != nil {
				return openedDocument{}, errNotADocument
			}
		}
	}
	return document, nil
}

// recordOpened records that this viewer opened the object id of the project
// projectID now. A document this release cannot read is left as it is:
// remembering is the viewer's convenience, and never unsays the open. It
// answers the time recorded, or empty when nothing was.
func (a *App) recordOpened(projectID, id string) string {
	if !catalog.ValidID(projectID) || !catalog.ValidID(id) {
		return ""
	}
	a.openedMu.Lock()
	defer a.openedMu.Unlock()
	document, err := a.readOpened()
	if err != nil {
		return ""
	}
	stamp := catalog.Stamp(a.now())
	project := openedProject{ID: projectID}
	if at := slices.IndexFunc(document.Projects, func(held openedProject) bool { return held.ID == projectID }); at >= 0 {
		project = document.Projects[at]
		document.Projects = slices.Delete(document.Projects, at, at+1)
	}
	project.Items = slices.DeleteFunc(slices.Clone(project.Items), func(held openedItem) bool { return held.ID == id })
	project.Items = append([]openedItem{{ID: id, OpenedAt: stamp}}, project.Items...)
	if len(project.Items) > MaxOpenedItems {
		project.Items = project.Items[:MaxOpenedItems]
	}
	document.Projects = append([]openedProject{project}, document.Projects...)
	if len(document.Projects) > MaxKnownProjects {
		document.Projects = document.Projects[:MaxKnownProjects]
	}
	data, err := json.Marshal(document, json.Deterministic(true))
	if err != nil || a.documents.write(openedName, append(data, '\n')) != nil {
		return ""
	}
	return stamp
}

// openedIn is when this viewer last opened each object of the project
// projectID, by object identity; empty when it opened none or the document
// cannot be read.
func (a *App) openedIn(projectID string) map[string]string {
	opened := map[string]string{}
	if projectID == "" {
		return opened
	}
	document, err := a.readOpened()
	if err != nil {
		return opened
	}
	for _, project := range document.Projects {
		if project.ID == projectID {
			for _, item := range project.Items {
				opened[item.ID] = item.OpenedAt
			}
		}
	}
	return opened
}
