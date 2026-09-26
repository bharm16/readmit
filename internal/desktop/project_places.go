package desktop

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/project"
)

// Where a project and its objects are, as the Projects list and the project
// menu reach them: forgetting a remembered project, showing an object in the
// host's file manager, and listing a project's loose files by name.

// ProjectForgetResult answers Remove from recents.
type ProjectForgetResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
}

func (r *ProjectForgetResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ForgetProject removes one project from the projects this viewer remembers.
// Only the entry is forgotten: the project's folder, its documents and its
// evidence stay exactly as they are, and opening it again remembers it
// again. A project the list no longer holds is refused rather than forgotten
// quietly. It writes one small local file and holds the operation slot.
func (a *App) ForgetProject(id string) ProjectForgetResult {
	release, claimed := a.claim("")
	if !claimed {
		return ProjectForgetResult{State: busyRefusal.state, Reason: busyRefusal.reason}
	}
	defer release()
	a.projectsMu.Lock()
	defer a.projectsMu.Unlock()
	document, err := a.readProjects()
	if err != nil {
		return ProjectForgetResult{State: Failed, Reason: "the remembered projects cannot be read; they are left exactly as written"}
	}
	at := slices.IndexFunc(document.Projects, func(known knownProject) bool { return known.ID == id })
	if at < 0 {
		return ProjectForgetResult{State: Failed, Reason: "that project is not in the projects list any more"}
	}
	document.Projects = slices.Delete(document.Projects, at, at+1)
	if err := a.writeProjects(document); errors.Is(err, fs.ErrPermission) {
		return ProjectForgetResult{State: PermissionDenied, Reason: "this account cannot write the remembered projects"}
	} else if err != nil {
		return ProjectForgetResult{State: Failed, Reason: "the remembered projects could not be replaced; they are left as they were"}
	}
	return ProjectForgetResult{State: Completed}
}

// RevealResult answers Show in Finder (Show in folder elsewhere). The place
// shown is never answered: the window names objects, and the host's file
// manager is what shows where one is.
type RevealResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
}

func (r *RevealResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// RevealItem shows where one object is in the host's file manager: a
// project's folder, a case's evidence, or an attachment's stored copy,
// selected in its folder. An object that is not where it was recorded is
// refused with that reason rather than showing some other place. Nothing is
// opened, run or changed.
func (a *App) RevealItem(request ItemRequest) RevealResult {
	return run(a, false, false, func(ctx context.Context) RevealResult {
		result := RevealResult{Context: request.Context}
		place, declined := a.placeOf(ctx, request)
		if place == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if err := a.revealer()(place); err != nil {
			result.refuse(Failed, "the file manager could not be opened")
			return result
		}
		result.State = Completed
		return result
	})
}

// placeOf is where one object is now, or why it cannot be shown.
func (a *App) placeOf(ctx context.Context, request ItemRequest) (string, refusal) {
	switch request.Ref.Kind {
	case ProjectItem:
		folder := a.rememberedFolder(request.Ref.ID)
		if folder == "" && request.Ref.ID == request.Context.ProjectID {
			folder = request.Context.Project
		}
		if folder == "" {
			return "", refusal{Failed, "this viewer has not opened that project"}
		}
		opened, _, reason, _ := projectAt(folder, request.Ref.ID)
		if opened == nil {
			return "", refusal{Failed, reason}
		}
		return opened.Root, refusal{}
	case AttachmentItem:
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			return "", declined
		}
		store, err := catalog.Open(root)
		if err != nil {
			return "", refusal{Failed, "the project folder cannot be opened"}
		}
		attachments, err := store.Attachments()
		if err != nil {
			return "", refusal{Failed, "the project's attachments cannot be read; they are left exactly as written"}
		}
		at := slices.IndexFunc(attachments, func(attachment catalog.Attachment) bool { return attachment.ID == request.Ref.ID })
		if at < 0 {
			return "", refusal{Failed, "the project holds no such attachment"}
		}
		path := store.AttachmentPath(attachments[at])
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			return "", refusal{Failed, "the attachment's stored copy is no longer in the project"}
		}
		return path, refusal{}
	}
	loaded, item, refused := a.catalogItem(ctx, request.Context, request.Ref, false)
	if loaded == nil {
		return "", refusal{refused.State, refused.Reason}
	}
	if item.Availability == ItemMissing {
		return "", refusal{Failed, cmp.Or(item.Reason, "the object is no longer where the project recorded it")}
	}
	recorded := loaded.document.Items[loaded.document.Find(request.Ref.ID)]
	if current := recorded.Current(); current != nil {
		return loaded.store.Path(current.Members[0]), refusal{}
	}
	return filepath.Join(loaded.root, recorded.Entry), refusal{}
}

// Revealer selects a path in the host's file manager. The desktop shell
// provides it beside its dialogs; the facade itself starts no program.
type Revealer interface {
	Reveal(path string) error
}

// revealer is how an object's place is shown: the one a test set, or the
// host's, when the host has one.
func (a *App) revealer() func(string) error {
	if a.reveal != nil {
		return a.reveal
	}
	if host, ok := a.chooser.(Revealer); ok {
		return host.Reveal
	}
	return func(string) error { return errNoRevealer }
}

var errNoRevealer = errors.New("this host cannot show a file's place")

// ProjectFile is one entry of a project folder that is no object of the
// project: a loose file an older release or a person left there. Kind is
// what the entry declares, as the workspace listing reads it.
type ProjectFile struct {
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
}

// ProjectFilesResult carries the loose entries of a project, by name.
type ProjectFilesResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Files   []ProjectFile  `json:"files"`
}

func (r *ProjectFilesResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ProjectFiles lists, by name, the entries of the open project that are none
// of its objects — not a case, a test, a run or any other kind the catalog
// lists — and none of the project's own documents or the application's
// storage. It is a read: nothing is opened, verified or written.
func (a *App) ProjectFiles(request ItemRequest) ProjectFilesResult {
	return run(a, false, false, func(ctx context.Context) ProjectFilesResult {
		result := ProjectFilesResult{Context: request.Context, Files: []ProjectFile{}}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		entries, err := os.ReadDir(root)
		switch {
		case errors.Is(err, fs.ErrPermission):
			result.refuse(PermissionDenied, "this account cannot read the project folder")
			return result
		case err != nil:
			result.refuse(Failed, "the project folder cannot be read")
			return result
		case len(entries) > MaxWorkspaceEntries:
			result.refuse(Failed, "the folder holds more entries than this release lists")
			return result
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				result.refuse(cancelledRefusal.state, cancelledRefusal.reason)
				return result
			}
			name := entry.Name()
			if _, _, previous := artifactdir.ParsePreviousName(name); previous || name == catalog.Folder || name == project.DocumentName ||
				name == project.RevisionsDocumentName || name == project.QuotaDocumentName || strings.HasSuffix(name, ".incomplete") {
				continue
			}
			if _, object := entryKind(root, entry); object {
				continue
			}
			result.Files = append(result.Files, ProjectFile{Name: name, Kind: describe(root, entry).Kind})
		}
		result.State = Completed
		if len(result.Files) == 0 {
			result.State = Empty
		}
		return result
	})
}
