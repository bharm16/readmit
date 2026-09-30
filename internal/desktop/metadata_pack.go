package desktop

import (
	"bytes"
	"errors"
	"strings"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/profilepack"
)

// metadataPackDraftOf interprets the original supported pack document once.
// The evaluator's metadata projection does not replace its v2-v5 clauses.
func metadataPackDraftOf(raw []byte) (*MetadataPackDraft, error) {
	if len(raw) > profilepack.MaxPackBytes {
		return nil, errors.New("the metadata pack exceeds the Library's retained size limit")
	}
	pack, err := profileeval.DecodePack(raw)
	if err != nil {
		return nil, err
	}
	return &MetadataPackDraft{Document: string(raw), Metadata: pack.Metadata, Schema: pack.Schema}, nil
}

// validateMetadataPackDraft stages the exact external document as one named
// Library object. Identity/version, rights provenance and support remain the
// source's declarations. Replacing a published version with different bytes
// is refused, including changes outside the v1 metadata projection.
func validateMetadataPackDraft(scope draftScope, draft ItemDraft) ([]catalog.Staged, *ProfileDraft, []FieldProblem) {
	problem := func(reason string) ([]catalog.Staged, *ProfileDraft, []FieldProblem) {
		return nil, nil, []FieldProblem{{Field: "profile.metadata_pack", Problem: reason}}
	}
	held := draft.Profile
	if !emptyLocalProfile(held.Profile) || held.Pack != nil || held.Origin != nil || held.PackDocument != "" || held.FHIR != nil {
		return problem("a metadata pack is retained whole; no local or FHIR profile clauses are removed to publish it")
	}
	raw := []byte(held.MetadataPack.Document)
	pack, err := metadataPackDraftOf(raw)
	if err != nil {
		return problem("the complete metadata pack cannot be read: " + err.Error())
	}
	if scope.loaded != nil {
		for _, item := range scope.loaded.document.Items {
			if item.Kind != string(ProfileItem) || scope.loaded.removed(item) {
				continue
			}
			for _, record := range metadataPackRecords(item, scope.intent) {
				paths, availability, _ := scope.loaded.backing(record)
				if availability != ItemAvailable {
					continue
				}
				path := paths[profileRole]
				if item.ID == scope.item {
					if schema, _ := sniffSchema(path); !strings.HasPrefix(schema, "readmit-profile-pack/") {
						return problem("a profile is not replaced by a metadata pack; save the imported pack as its own Library object")
					}
				}
				if carried, held := paths[packRole]; held {
					path = carried
				}
				earlierRaw, err := boundedFile(path, profilepack.MaxPackBytes)
				earlier, readErr := metadataPackDraftOf(earlierRaw)
				if item.ID == scope.item && (!strings.HasPrefix(schemaOf(earlierRaw), "readmit-profile-pack/") || readErr != nil) {
					return problem("a profile is not replaced by a metadata pack; save the imported pack as its own Library object")
				}
				if err != nil || readErr != nil {
					continue
				}
				if item.ID == scope.item && earlier.Metadata.Identity.ID != pack.Metadata.Identity.ID {
					return problem("a named metadata pack keeps its identity; import another pack as a new object")
				}
				if earlier.Metadata.Identity == pack.Metadata.Identity && !bytes.Equal(earlierRaw, raw) {
					return problem("the project already holds different clauses under this metadata pack identity and version; retain a new authored version")
				}
				if scope.item == "" && earlier.Metadata.Identity == pack.Metadata.Identity {
					return problem("the project already holds this exact metadata pack; select the saved pack instead of publishing another object for the same pin")
				}
			}
		}
	}
	return []catalog.Staged{{Role: profileRole, File: "pack.json", Data: raw}}, &ProfileDraft{MetadataPack: pack}, nil
}

func metadataPackRecords(item catalog.Item, intent string) []catalog.Item {
	if len(item.Revisions) == 0 {
		return []catalog.Item{item}
	}
	records := []catalog.Item{}
	for i, revision := range item.Revisions {
		if intent != "" && revision.Intent == intent {
			continue
		}
		record := item
		record.Revisions = item.Revisions[:i+1]
		records = append(records, record)
	}
	return records
}
