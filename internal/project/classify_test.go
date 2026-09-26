package project_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/project"
)

// Every file of a project directory falls into one named class: the project's
// own documents and the recovery copies the document store retained under
// their names are the mutable documents; a readmit-secrets/v1 registration is
// a credential reference and a readmit-protection/v1 control a protection key
// reference, whichever name it sits under; a declared index is the disposable
// exclusion it is; and everything else is other. Classification reads only the
// name and the schema its bytes declare, never a value.
func TestClassifyFile(t *testing.T) {
	digest := sha256.Sum256([]byte("retained bytes"))
	recovery := project.DocumentName + ".recovery-" + hex.EncodeToString(digest[:])
	for _, test := range []struct {
		what, name, schema string
		want               project.FileClass
	}{
		{"the project document", project.DocumentName, "", project.ClassMutableDocument},
		{"the revisions document", project.RevisionsDocumentName, "", project.ClassMutableDocument},
		{"the quota document", project.QuotaDocumentName, "", project.ClassMutableDocument},
		{"a recovery copy the store named", recovery, "whatever a damaged copy declares", project.ClassMutableDocument},
		{"a credential reference", "endpoints.json", "readmit-secrets/v1", project.ClassCredentialReference},
		{"a credential reference under any name", "mllp-target.secrets", "readmit-secrets/v1", project.ClassCredentialReference},
		{"a protection key reference", "vault.json", "readmit-protection/v1", project.ClassProtectionKeyReference},
		{"a protection key reference under any name", "at-rest.control", "readmit-protection/v1", project.ClassProtectionKeyReference},
		{"a declared index", "regression.index.json", "readmit-index/v1", project.ClassDeclaredExclusion},
		{"a name that merely resembles a recovery copy", project.DocumentName + ".recovery-not-a-digest", "", project.ClassOther},
		{"a name whose suffix is not the store's digest", project.DocumentName + ".recovery-" + strings.Repeat("g", 64), "", project.ClassOther},
		{"an index contract this release does not read is other", "future.json", "readmit-index/v99", project.ClassOther},
		{"an unclassified document", "notes.txt", "", project.ClassOther},
		{"a contract this classification does not name", "mystery.json", "readmit-case/v5", project.ClassOther},
	} {
		t.Run(test.what, func(t *testing.T) {
			if got := project.ClassifyFile(test.name, test.schema); got != test.want {
				t.Fatalf("ClassifyFile(%q, %q) = %q, want %q", test.name, test.schema, got, test.want)
			}
		})
	}
}
