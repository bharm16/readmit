package desktop

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/scenario"
	"github.com/bharm16/readmit/internal/scenariogen"
)

// A library object may carry what the Library shows beside its document and
// no reader of that document takes: the names a person gave a check group's
// checks, and the local profile a scenario is authored for. It is saved as
// its own readmit-library-metadata/v1 member of the same revision, so the
// assertion set and the generator plan stay exactly the documents the
// command line reads, and an export writes those documents alone.

// LibraryMetadataSchema is the contract of a library object's metadata
// member.
const LibraryMetadataSchema = "readmit-library-metadata/v1"

// metadataRole is the role a library object's metadata is saved as.
const metadataRole = "metadata"

// maxMetadataBytes bounds one metadata member.
const maxMetadataBytes = 256 << 10

// maxCheckNameRunes bounds one check's display name in characters.
const maxCheckNameRunes = 200

// libraryMetadata is a library object's metadata member: check names by
// check identity, and the scenario's local profile at its exact revision.
type libraryMetadata struct {
	Schema  string            `json:"schema"`
	Names   map[string]string `json:"names,omitzero"`
	Profile *ItemRef          `json:"profile,omitzero"`
}

// metadataMember is the staged metadata member, or nothing when there is
// nothing to keep.
func metadataMember(metadata libraryMetadata) ([]catalog.Staged, error) {
	if len(metadata.Names) == 0 && metadata.Profile == nil {
		return nil, nil
	}
	metadata.Schema = LibraryMetadataSchema
	data, err := encodeMember(metadata)
	if err != nil {
		return nil, err
	}
	return []catalog.Staged{{Role: metadataRole, File: "metadata.json", Data: data}}, nil
}

// readMetadata reads a saved revision's metadata member exactly as written,
// or none when the revision holds none.
func readMetadata(paths map[string]string) (libraryMetadata, error) {
	path, held := paths[metadataRole]
	if !held {
		return libraryMetadata{}, nil
	}
	data, err := boundedFile(path, maxMetadataBytes)
	if err != nil {
		return libraryMetadata{}, err
	}
	var metadata libraryMetadata
	if json.Unmarshal(data, &metadata, json.RejectUnknownMembers(true)) != nil || metadata.Schema != LibraryMetadataSchema {
		return libraryMetadata{}, errors.New("the library object's metadata cannot be read")
	}
	return metadata, nil
}

// checkNames validates the names a person gave checks: each names a check the
// group holds, by its identity, and is 1 to 200 printable characters once
// trimmed.
func checkNames(names map[string]string, ids []string) (map[string]string, []FieldProblem) {
	if len(names) == 0 {
		return nil, nil
	}
	problems := []FieldProblem{}
	kept := map[string]string{}
	for _, id := range slices.Sorted(maps.Keys(names)) {
		name := strings.TrimSpace(names[id])
		switch {
		case !slices.Contains(ids, id):
			problems = append(problems, FieldProblem{Field: "check_group.names." + id, Problem: "a name is given to a check this group holds"})
		case name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxCheckNameRunes || strings.ContainsFunc(name, unicode.IsControl):
			problems = append(problems, FieldProblem{Field: "check_group.names." + id, Problem: "a check's name is 1 to 200 printable characters"})
		default:
			kept[id] = name
		}
	}
	return kept, problems
}

// namesFor keeps the names of the checks a group still holds.
func namesFor(names map[string]string, group CheckGroupDraft) map[string]string {
	ids := checkIDs(group)
	kept := map[string]string{}
	for id, name := range names {
		if slices.Contains(ids, id) {
			kept[id] = name
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// checkIDs are the identities of every check a group holds, supported or not.
func checkIDs(group CheckGroupDraft) []string {
	ids := []string{}
	for _, clause := range group.Set.Assertions {
		ids = append(ids, clause.ID)
	}
	for _, clause := range group.Unsupported {
		ids = append(ids, clause.ID)
	}
	return ids
}

// scenarioProfile decides the local profile a scenario is authored for: a
// saved local profile of the project at an exact revision, covering the
// family of the scenario's workflow.
func (s draftScope) scenarioProfile(ref ItemRef, plan scenariogen.Plan) *FieldProblem {
	problem := func(text string) *FieldProblem { return &FieldProblem{Field: "scenario.profile", Problem: text} }
	number, err := strconv.Atoi(ref.Revision)
	if ref.Kind != ProfileItem || !catalog.ValidID(ref.ID) || err != nil || number < 1 || strconv.Itoa(number) != ref.Revision {
		return problem("a scenario names a saved local profile by its identity and exact version")
	}
	if s.loaded == nil {
		return problem("the project holds no such profile")
	}
	index := s.loaded.document.Find(ref.ID)
	if index < 0 || s.loaded.document.Items[index].Kind != string(ProfileItem) || s.loaded.removed(s.loaded.document.Items[index]) {
		return problem("the project holds no such profile")
	}
	record, held := itemAt(s.loaded.document.Items[index], ref.Revision)
	if !held || len(record.Revisions) == 0 {
		return problem("the profile has no saved version " + ref.Revision)
	}
	paths, availability, reason := s.loaded.backing(record)
	if availability != ItemAvailable {
		return problem(reason)
	}
	profile, _, err := readLocalProfile(paths)
	if err != nil {
		return problem("that version is not a local profile: " + err.Error())
	}
	workflow, err := scenario.DecodeDocument(plan.Template)
	if err != nil {
		return problem("the scenario's workflow cannot be read, so no profile can be matched to it")
	}
	family := lifecycleFamily(workflow.Profile)
	if !strings.EqualFold(profile.Base.Family, family) {
		return problem("the profile covers " + cmp.Or(profile.Base.Family, "no family") + "; this scenario's workflow is " + cmp.Or(family, "another family"))
	}
	return nil
}
