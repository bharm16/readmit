package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/project"
)

// A case's attachments: files a person adds to a case, copied into the
// project's own storage (readmit-attachments/v1, internal/catalog) under
// names the application generates. The window sees each one's name, type and
// when it was added; nothing here opens, interprets or runs an attachment,
// and removing one removes its association with the case.

// MaxAttachmentsAdded bounds the files one Add attachment copies.
const MaxAttachmentsAdded = 32

// Attachment is one file attached to a case.
type Attachment struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	AddedAt *string `json:"added_at"`
}

// AttachmentsResult carries the attachments of one case as they stand.
type AttachmentsResult struct {
	State       State          `json:"state"`
	Reason      string         `json:"reason,omitzero"`
	Context     RequestContext `json:"context"`
	Attachments []Attachment   `json:"attachments"`
}

func (r *AttachmentsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// AttachmentRemoveRequest names one attachment of one case.
type AttachmentRemoveRequest struct {
	Context RequestContext `json:"context"`
	Case    ItemRef        `json:"case"`
	ID      string         `json:"id"`
}

// ListAttachments lists the attachments of one case, oldest first. It is a
// read.
func (a *App) ListAttachments(request ItemRequest) AttachmentsResult {
	return run(a, false, false, func(ctx context.Context) AttachmentsResult {
		result := AttachmentsResult{Context: request.Context, Attachments: []Attachment{}}
		loaded, refused := a.attachedCase(ctx, request.Context, request.Ref, false)
		if loaded == nil {
			result.refuse(refused.state, refused.reason)
			return result
		}
		return loaded.attachments(result, request.Ref.ID)
	})
}

// AddAttachments asks the host's file dialog for one or more files and
// copies each into the project's storage as an attachment of the case. A
// file is read bounded, as a regular file and never through a symbolic link;
// the files are attached together or not at all, and the originals are left
// exactly where they are. A dismissed dialog attaches nothing.
func (a *App) AddAttachments(request ItemRequest) AttachmentsResult {
	return run(a, true, true, func(ctx context.Context) AttachmentsResult {
		result := AttachmentsResult{Context: request.Context, Attachments: []Attachment{}}
		loaded, refused := a.attachedCase(ctx, request.Context, request.Ref, true)
		if loaded == nil {
			result.refuse(refused.state, refused.reason)
			return result
		}
		chosen, declined := a.chooseFiles(ctx, "Add attachment", "All files", "*")
		if chosen == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if len(chosen) > MaxAttachmentsAdded {
			result.refuse(Failed, "at most "+strconv.Itoa(MaxAttachmentsAdded)+" files are attached at once")
			return result
		}
		files := make([]catalog.NewAttachment, 0, len(chosen))
		var total int64
		for _, path := range chosen {
			if ctx.Err() != nil {
				result.refuse(cancelledRefusal.state, cancelledRefusal.reason)
				return result
			}
			data, err := attachmentSource.Read(path)
			if err != nil {
				result.refuse(Failed, "a chosen file is not a regular file of at most "+strconv.Itoa(catalog.MaxAttachmentBytes>>20)+
					" MiB that this account can read; nothing was attached")
				return result
			}
			name := filepath.Base(path)
			if !catalog.ValidName(name) {
				name = "attachment"
			}
			files = append(files, catalog.NewAttachment{Name: name, Type: catalog.AttachmentType(filepath.Ext(path)), Data: data})
			total += int64(len(data))
		}
		// The quota is decided under the lock the copies are stored under, so
		// nothing another writer adds meanwhile is left out of the decision.
		admit := func(detached catalog.Detached) error { return loaded.quotaRoom(len(files), total, detached) }
		if _, err := loaded.store.Attach(request.Ref.ID, files, a.now(), admit); err != nil {
			switch {
			case errors.Is(err, errQuotaUnreadable):
				result.refuse(Failed, "the project's quota cannot be read; nothing was attached")
				return result
			case errors.Is(err, project.ErrQuota):
				result.refuse(Failed, "the project's quota does not leave room for these files; nothing was attached")
				return result
			}
			if errors.Is(err, catalog.ErrTooManyAttachments) {
				result.refuse(Failed, "the project holds as many attachments as this release keeps; nothing was attached")
				return result
			}
			declined := probeWriteFailure(loaded.root, "this account cannot write to the project folder",
				"the files could not be stored in the project; nothing was attached")
			result.refuse(declined.state, declined.reason)
			return result
		}
		return loaded.attachments(result, request.Ref.ID)
	})
}

// RemoveAttachment removes one attachment from its case. Only the
// association goes: nothing is deleted.
func (a *App) RemoveAttachment(request AttachmentRemoveRequest) AttachmentsResult {
	return run(a, false, true, func(ctx context.Context) AttachmentsResult {
		result := AttachmentsResult{Context: request.Context, Attachments: []Attachment{}}
		loaded, refused := a.attachedCase(ctx, request.Context, request.Case, true)
		if loaded == nil {
			result.refuse(refused.state, refused.reason)
			return result
		}
		if err := loaded.store.Detach(request.Case.ID, request.ID); errors.Is(err, catalog.ErrNoAttachment) {
			result.refuse(Failed, "the case holds no such attachment")
			return result
		} else if err != nil {
			result.refuse(Failed, "the project's attachments could not be replaced; they are left as they were")
			return result
		}
		return loaded.attachments(result, request.Case.ID)
	})
}

// attachmentSource reads a file a person chose to attach: bounded, regular,
// never a symbolic link.
var attachmentSource = artifactdir.Document{MaxBytes: catalog.MaxAttachmentBytes}

// attachedCase loads the project for a case whose attachments are asked
// for.
func (a *App) attachedCase(ctx context.Context, context RequestContext, ref ItemRef, record bool) (*loadedCatalog, refusal) {
	if ref.Kind != CaseItem {
		return nil, refusal{Failed, "attachments belong to a case"}
	}
	loaded, _, refused := a.catalogItem(ctx, context, ref, record)
	if loaded == nil {
		return nil, refusal{refused.State, refused.Reason}
	}
	return loaded, refusal{}
}

// attachments answers the attachments of the object with id.
func (c *loadedCatalog) attachments(result AttachmentsResult, id string) AttachmentsResult {
	held, err := c.store.Attachments()
	if err != nil {
		result.refuse(Failed, "the project's attachments cannot be read; they are left exactly as written")
		return result
	}
	for _, attachment := range held {
		if attachment.Item == id {
			result.Attachments = append(result.Attachments, Attachment{ID: attachment.ID, Name: attachment.Name, Type: attachment.Type,
				AddedAt: stamped(attachment.AddedAt)})
		}
	}
	result.State = Completed
	if len(result.Attachments) == 0 {
		result.State = Empty
	}
	return result
}

// errQuotaUnreadable reports a quota or project usage that cannot be read.
var errQuotaUnreadable = errors.New("the project's quota cannot be read")

// quotaRoom decides whether the project's quota, when it declares one,
// leaves room for files more files of total bytes. The copies removed
// attachments left behind are kept, never deleted, and are left out of the
// decision: removing an attachment is what frees its room.
func (c *loadedCatalog) quotaRoom(files int, total int64, detached catalog.Detached) error {
	quota, present, err := project.ReadQuota(c.root)
	if err != nil {
		return errQuotaUnreadable
	}
	if !present {
		return nil
	}
	usage, err := project.CheckQuota(c.root)
	if err != nil && !errors.Is(err, project.ErrQuota) {
		return errQuotaUnreadable
	}
	if usage.Files-detached.Files+files > quota.MaxFiles || usage.Bytes-detached.Bytes+total > quota.MaxBytes {
		return project.ErrQuota
	}
	return nil
}
