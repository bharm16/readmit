package desktop

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/findingreview"
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
	{"report.json", DiagnosisArtifact},
	{"machine.json", CorrelationReviewArtifact},
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
	"readmit-local-profile/v1":     ProfileArtifact,
	"readmit-profile-package/v1":   PackageArtifact,
	"readmit-sequence-analysis/v1": AnalysisArtifact,
	"readmit-secrets/v1":           SecretArtifact,
	"readmit-send-policy/v1":       PolicyArtifact,
	"readmit-reset-plan/v1":        ResetArtifact,
	"readmit-reset-outcome/v1":     ResetArtifact,

	"readmit-normalization-policy/v1": NormalizationArtifact,
	"readmit-diagnose-config/v1":      DiagnoseConfigArtifact,
	"readmit-finding-decisions/v1":    DecisionsArtifact,
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
				return refinedMarker(filepath.Join(path, marker.name), marker.kind)
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

// refinedMarker lets a marker file's own declared contract refine what the
// directory is called, exactly as a flat file's declared contract already
// does. Two names carry more than one contract: review.json is an export
// review unless it declares the finding-review contract, so a finding review
// never lists as the export review it is not; and report.json names a
// diagnosis only when it declares a diagnosis contract, so a directory
// holding some other report.json stays unsupported here, exactly as before
// this release read any report.json at all. Every other marker keeps its one
// kind, and nothing here verifies the directory — opening it still does.
func refinedMarker(path string, kind Kind) (Kind, bool) {
	switch kind {
	case ReviewArtifact:
		if schema, ok := sniffSchema(path); ok && schema == findingreview.Schema {
			return FindingReviewArtifact, true
		}
		return ReviewArtifact, true
	case DiagnosisArtifact:
		schema, ok := sniffSchema(path)
		if !ok || (schema != diagnose.Schema && schema != diagnose.GroupsSchema) {
			return UnsupportedArtifact, false
		}
		return DiagnosisArtifact, true
	}
	return kind, true
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
