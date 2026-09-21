package artifactpath

import "strings"

// The retained evidence families, as the prefix of the schema a manifest
// declares. The versions below each prefix belong to the family's own
// contract — bundle owns the case versions, replay the run versions, and the
// result contract its own — so a family is what protection and dispatch agree
// on, never a version.
const (
	FamilyCase   = "readmit-case/"
	FamilyRun    = "readmit-run/"
	FamilyResult = "readmit-result/"
)

// EvidenceFamily reports which retained evidence family a manifest schema
// names, or "" when it names none. This package refuses writes inside any
// family's directories; a reader that dispatches between the families
// compares the returned prefix against the constants above rather than
// re-spelling the list, so adding a family changes protection and dispatch
// together.
func EvidenceFamily(schema string) string {
	for _, prefix := range []string{FamilyCase, FamilyRun, FamilyResult} {
		if strings.HasPrefix(schema, prefix) {
			return prefix
		}
	}
	return ""
}

// IsEvidenceSchema reports whether a manifest schema names one of the
// retained evidence families.
func IsEvidenceSchema(schema string) bool {
	return EvidenceFamily(schema) != ""
}
