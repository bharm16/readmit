package desktop

import (
	"context"
	"slices"

	"github.com/bharm16/readmit/internal/project"
)

// RecoveryCopyRequest names one recovery copy of a project by the document it
// was kept for and the digest its name records.
type RecoveryCopyRequest struct {
	Context  RequestContext `json:"context"`
	Document string         `json:"document"`
	Digest   string         `json:"digest"`
}

// RecoveryCopyContents is what one readable recovery copy holds, as values:
// for a project document its contract, title, cases and declared interface
// versions; for the editable document its contract, revisions and notes and
// the latest revision's name; for a quota its limits.
type RecoveryCopyContents struct {
	Schema            string   `json:"schema,omitzero"`
	Title             string   `json:"title,omitzero"`
	Cases             int      `json:"cases,omitzero"`
	InterfaceVersions []string `json:"interface_versions,omitzero"`
	Revisions         int      `json:"revisions,omitzero"`
	Notes             int      `json:"notes,omitzero"`
	Latest            string   `json:"latest,omitzero"`
	MaxBytes          int64    `json:"max_bytes,omitzero"`
	MaxFiles          int      `json:"max_files,omitzero"`
}

// RecoveryCopyResult is one recovery copy as listed, and what it holds when
// it is readable.
type RecoveryCopyResult struct {
	State    State                 `json:"state"`
	Reason   string                `json:"reason,omitzero"`
	Copy     *ProjectRecoveryCopy  `json:"copy,omitzero"`
	Contents *RecoveryCopyContents `json:"contents,omitzero"`
}

func (r *RecoveryCopyResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// InspectRecoveryCopy opens one recovery copy read-only: it reads the copy
// through the reader of the document it was kept for, exactly as Restore copy
// would, and answers what it holds. It writes nothing, and never changes the
// project or restores anything.
func (a *App) InspectRecoveryCopy(request RecoveryCopyRequest) RecoveryCopyResult {
	return run(a, false, false, func(context.Context) RecoveryCopyResult {
		source, declined := sourceOf(request.Context)
		if source == nil {
			return RecoveryCopyResult{State: declined.state, Reason: declined.reason}
		}
		copies, err := project.RecoveryCopies(source.root)
		if err != nil {
			return RecoveryCopyResult{State: Failed, Reason: err.Error()}
		}
		at := slices.IndexFunc(copies, func(retained project.RecoveryCopy) bool {
			return retained.Document == request.Document && retained.Digest == request.Digest
		})
		if at < 0 {
			return RecoveryCopyResult{State: Failed, Reason: "the project holds no such recovery copy; list the copies again"}
		}
		view := recoveryCopyView(copies[at])
		result := RecoveryCopyResult{State: Completed, Copy: &view}
		if copies[at].State != project.RecoveryReadable {
			result.refuse(Failed, "this recovery copy is "+view.State+"; it cannot be opened")
			return result
		}
		recovered, err := project.ReadRecoveryCopy(source.root, request.Document, request.Digest)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		contents := RecoveryCopyContents{}
		switch {
		case recovered.Project != nil:
			document := recovered.Project
			contents.Schema, contents.Title, contents.Cases = document.Schema, document.Settings.Title, len(document.Cases)
			contents.InterfaceVersions = document.InterfaceVersions
		case recovered.Revisions != nil:
			revisions := recovered.Revisions
			contents.Schema, contents.Revisions, contents.Notes = revisions.Schema, len(revisions.Revisions), len(revisions.Notes)
			if len(revisions.Revisions) > 0 {
				contents.Latest = revisions.Revisions[len(revisions.Revisions)-1].Name
			}
		case recovered.Quota != nil:
			contents.Schema, contents.MaxBytes, contents.MaxFiles = recovered.Quota.Schema, recovered.Quota.MaxBytes, recovered.Quota.MaxFiles
		}
		result.Contents = &contents
		return result
	})
}
