package desktop

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
)

// maxSchemaSniffBytes bounds how far into a regular file the listing looks for
// the contract the file declares. Listing never verifies evidence and never
// reads a whole file: the fixed-name documents readmit writes declare their
// contract within this window, and anything that does not is listed as
// unsupported with the reason, exactly as before.
const maxSchemaSniffBytes = 4096

// schemaMarkerFiles are the fixed-name records a retained artifact directory
// holds beside its evidence. They locate what a directory claims to be the
// same way a project document locates a project: one canonical name, decoded
// only when the entry is opened, never concluded from an arbitrary file name.
var schemaMarkerFiles = []struct {
	name string
	kind Kind
}{
	// A durable run carries its job record beside the journal and the result
	// directory it retains; a plain result holds only its result record.
	{"engine.json", JobArtifact},
	{"result.json", ResultArtifact},
	{"review.json", ReviewArtifact},
}

// declaredSchemas are the contracts a regular file can declare that this
// release offers an applicable picker or a coherent context for. An entry
// whose contract is on this list is offered to the panels that apply; an
// entry whose contract is not is unsupported here, with the reason, exactly as
// every other entry this window does not open.
var declaredSchemas = map[string]Kind{
	"readmit-index/v1":             IndexArtifact,
	"readmit-target/v1":            TargetArtifact,
	"readmit-target/v2":            TargetArtifact,
	"readmit-target/v3":            TargetArtifact,
	"readmit-correlation-rules/v1": RulesArtifact,
	"readmit-transform-plan/v1":    PlanArtifact,
	"readmit-test/v1":              SpecArtifact,
	"readmit-profile-pack/v1":      PackArtifact,
	"readmit-sequence-analysis/v1": AnalysisArtifact,
}

// classify reports what one workspace entry declares, beyond what the case
// reader and the two project documents already answer. A directory holding a
// fixed-name record of a retained artifact is named as that artifact; a
// regular file declaring a contract within the sniff bound is named as that
// contract's kind. Both are claims the listing makes and never verifications:
// opening the entry remains the verification step, and an entry classified
// here is not admitted anywhere by this alone.
func classify(root, name string, isDir bool) (Kind, bool) {
	path := filepath.Join(root, name)
	if isDir {
		for _, marker := range schemaMarkerFiles {
			if info, err := os.Lstat(filepath.Join(path, marker.name)); err == nil && info.Mode().IsRegular() {
				return marker.kind, true
			}
		}
		return UnsupportedArtifact, false
	}
	schema, ok := sniffSchema(path)
	if !ok {
		return UnsupportedArtifact, false
	}
	kind, known := declaredSchemas[schema]
	return kind, known
}

// sniffSchema reads a bounded window of a regular file and reports the
// contract a `schema` member declares within it. It is a claim about the
// bytes, not an acceptance of them: the readers that open each contract
// decide what a file really is, and this only decides what the listing calls
// it.
func sniffSchema(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	window := make([]byte, maxSchemaSniffBytes)
	read, err := io.ReadFull(file, window)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", false
	}
	head := window[:read]
	const member = `"schema"`
	at := bytes.Index(head, []byte(member))
	if at < 0 {
		return "", false
	}
	rest := head[at+len(member):]
	rest = bytes.TrimLeft(rest, " \t\r\n:")
	if len(rest) < 2 || rest[0] != '"' {
		return "", false
	}
	end := bytes.IndexByte(rest[1:], '"')
	if end < 0 {
		return "", false
	}
	return string(rest[1 : 1+end]), true
}
