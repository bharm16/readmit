package desktop

import (
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/project"
)

// Declared source names (#549): a source reads by the name declared for it,
// else by its exact source ID. A file name is never a source's name.

// SourceFacet is one source of a case as the filter offers it: the exact ID a
// query names it by, and the name it reads by, empty when nothing names it.
type SourceFacet struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// sourceNames is the name each source of a verified case reads by, keyed by
// source ID. The first of these that names a source is its name:
//
//  1. the name a person gave it on the project's entry for this exact case,
//     in Edit details or at import;
//  2. the label the collection session that carried it declared, for a
//     collected case;
//  3. the source label a mapping recipe declared for it, in the case's receipt
//     beside it (`<case>-receipt.json`), used only when the receipt reads
//     strictly, names this exact case and maps only sources the case declares.
//
// Each is checked against the verified case, and one that does not verify is
// passed over without a word. A source nothing names is absent.
func sourceNames(root, caseName string, opened *bundle.Bundle) map[string]string {
	declared := make(map[string]bool, len(opened.Manifest.Sources))
	for _, source := range opened.Manifest.Sources {
		declared[source.ID] = true
	}
	names := map[string]string{}
	name := func(id, value string) {
		if declared[id] && value != "" && names[id] == "" {
			names[id] = value
		}
	}
	if registered, entry := registeredEntry(root, caseName, opened.Identity); registered {
		for _, source := range entry.Sources {
			name(source.ID, source.Name)
		}
	}
	if opened.Collection != nil {
		for _, session := range opened.Collection.Sessions {
			name(session.SourceID, session.Label)
		}
	}
	for id, label := range receiptLabels(root, caseName, opened, declared) {
		name(id, label)
	}
	return names
}

// registeredEntry is the project's registration of the case under caseName
// with this identity, when root is a project that registers it.
func registeredEntry(root, caseName, identity string) (bool, project.Case) {
	opened, err := project.Open(root)
	if err != nil {
		return false, project.Case{}
	}
	for _, entry := range opened.Document.Cases {
		if entry.Name == caseName && entry.Identity == identity {
			return true, entry
		}
	}
	return false, project.Case{}
}

// receiptLabels are the source labels the case's mapping receipt declares,
// or none when the receipt is absent or does not describe this case.
func receiptLabels(root, caseName string, opened *bundle.Bundle, declared map[string]bool) map[string]string {
	name := caseName + "-receipt.json"
	if _, err := artifactpath.File(root, name); err != nil {
		return nil
	}
	data, err := readBoundedEntry(artifactpath.JoinReference(root, name), importer.MaxDocumentBytes)
	if err != nil {
		return nil
	}
	receipt, err := importer.DecodeMappingReceipt(data)
	if err != nil || receipt.Case.Identity != opened.Identity {
		return nil
	}
	labels := map[string]string{}
	for _, mapping := range receipt.Mappings {
		if !declared[mapping.SourceID] {
			return nil
		}
		if mapping.State == importer.Mapped && importer.IsLabel(mapping.Source) {
			labels[mapping.SourceID] = mapping.Source
		}
	}
	return labels
}

// sourceFacets are a case's sources in the order it declares them, each with
// the name it reads by.
func sourceFacets(opened *bundle.Bundle, names map[string]string) []SourceFacet {
	facets := make([]SourceFacet, 0, len(opened.Manifest.Sources))
	for _, source := range opened.Manifest.Sources {
		facets = append(facets, SourceFacet{ID: source.ID, Name: names[source.ID]})
	}
	return facets
}
