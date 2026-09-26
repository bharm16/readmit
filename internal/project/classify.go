package project

import (
	"path"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/protect"
	"github.com/bharm16/readmit/internal/secret"
)

// FileClass is the closed set of kinds a project directory's own files are.
// A backup or a maintenance inventory classes each entry with it, so a
// credential reference and a protection control are named for what they are —
// registrations that carry no stored value — and a derived index is named the
// disposable thing it is.
type FileClass string

const (
	// ClassMutableDocument is one of the project's own versioned documents —
	// project.json, revisions.json, quota.json — or a recovery copy of one
	// that the document store retained under its name.
	ClassMutableDocument FileClass = "mutable-project-document"
	// ClassCredentialReference is a readmit-secrets/v1 document: a
	// registration of references into an external store, never a value.
	ClassCredentialReference FileClass = "credential-reference"
	// ClassProtectionKeyReference is a readmit-protection/v1 control: the key
	// material stays behind the external reference it names.
	ClassProtectionKeyReference FileClass = "protection-key-reference"
	// ClassDeclaredExclusion is a derived case index: disposable and rebuilt
	// from canonical evidence, never the only copy of work.
	ClassDeclaredExclusion FileClass = "declared-exclusion"
	// ClassOther is a project file this classification does not name.
	ClassOther FileClass = "other-project-file"
)

// ClassifyFile classes one file of a project directory by its entry name and
// the schema its bytes declare. It reads no values: the caller passes the
// schema its own bounded prefix read decoded, so classifying a file can never
// become a second reader of a credential's or a key's contents. A recovery
// copy counts as the document it was retained for, and only a name the
// document store itself records — a digest suffix, not one that merely
// resembles it — does that.
func ClassifyFile(name, schema string) FileClass {
	base := path.Base(name)
	switch base {
	case DocumentName, RevisionsDocumentName, QuotaDocumentName:
		return ClassMutableDocument
	}
	if document, _, ok := artifactdir.ParsePreviousName(base); ok {
		switch document {
		case DocumentName, RevisionsDocumentName, QuotaDocumentName:
			return ClassMutableDocument
		}
	}
	switch schema {
	case secret.Schema:
		return ClassCredentialReference
	case protect.Schema:
		return ClassProtectionKeyReference
	case index.Schema:
		return ClassDeclaredExclusion
	}
	return ClassOther
}
