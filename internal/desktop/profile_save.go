package desktop

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"reflect"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/profilepackage"
	"github.com/bharm16/readmit/internal/profileversion"
)

// A local interface profile is saved whole with the seal of its version: the
// readmit-local-profile/v1 document, its readmit-profile-version/v1 seal and,
// when it records one, its origin are published in one revision or none is,
// so no profile is ever current without the seal of exactly its content. A
// version once sealed is never republished with other content.

// ProfileDraft is a local profile as its editor holds it, the metadata pack
// it is authored against, chosen by name, and the origin and attribution it
// records. The pack is not saved beside it: the profile's own pin names it.
//
// PackDocument is the exact readmit-profile-pack document an imported
// package carried, held while the project does not yet hold that pack. Saving
// the profile records it in the same revision, as the profile's own pack
// member, so the profile and its pack are published together or not at all;
// a project already holding exactly that pack reuses it. Once saved, the
// profile object itself is the pack its pin names, and each later revision
// carries the pack forward.
type ProfileDraft struct {
	Profile      localprofile.Profile   `json:"profile"`
	Pack         *ItemRef               `json:"pack,omitzero"`
	Origin       *profilepackage.Origin `json:"origin,omitzero"`
	PackDocument string                 `json:"pack_document,omitzero"`
}

// The roles a profile is saved as.
const (
	profileRole = "profile"
	sealRole    = "seal"
	originRole  = "origin"
	// packRole is the metadata pack a profile carries itself, recorded from
	// an imported package.
	packRole = "pack"
)

// validateProfileDraft validates a whole profile version: the profile held to
// its contract and sealed over its canonical document, its pack pin decided
// from the pack chosen, its version one no earlier revision published and no
// seal in the project records for other content, and its origin read as the
// package contract reads one.
func validateProfileDraft(scope draftScope, draft ItemDraft) ([]catalog.Staged, *ProfileDraft, []FieldProblem) {
	problems := []FieldProblem{}
	held := *draft.Profile
	profile, pack, found := scope.profileWithPack(draft.Name, held)
	problems = append(problems, found...)
	ordered, document, err := localprofile.Canonical(profile)
	if err != nil {
		return nil, nil, append(problems, FieldProblem{Field: "profile.profile", Problem: err.Error()})
	}
	seal, err := profileversion.Seal(profile)
	if err != nil {
		return nil, nil, append(problems, FieldProblem{Field: "profile.profile", Problem: err.Error()})
	}
	if earlier := scope.earlierProfiles(); len(earlier) > 0 {
		if earlier[0].Identity.ID != profile.Identity.ID {
			problems = append(problems, FieldProblem{Field: "profile.profile.id", Problem: "a profile keeps its id; save a copy to give it another"})
		}
		for _, published := range earlier {
			if published.Identity.Version == profile.Identity.Version {
				problems = append(problems, FieldProblem{Field: "profile.profile.version",
					Problem: "version " + profile.Identity.Version + " is already published; a changed profile carries a new version"})
				break
			}
		}
	}
	if err := profileversion.VerifyFolder(scope.root, profile); err != nil {
		problems = append(problems, FieldProblem{Field: "profile.profile.version", Problem: err.Error()})
	}
	sealed, err := seal.Encode()
	if err != nil {
		problems = append(problems, FieldProblem{Field: "profile.profile", Problem: err.Error()})
	}
	staged := []catalog.Staged{{Role: profileRole, File: "profile.json", Data: document}, {Role: sealRole, File: "version.json", Data: sealed}}
	normalized := &ProfileDraft{Profile: ordered, Pack: pack}
	carried := ""
	switch {
	case held.PackDocument != "":
		reused, document, problem := scope.importedPack(profile, held.PackDocument)
		if problem != nil {
			problems = append(problems, *problem)
		}
		if reused != nil {
			normalized.Pack = reused
		}
		carried = document
	case pack != nil && scope.item != "" && pack.ID == scope.item && scope.loaded != nil:
		// A profile that carries its own pack keeps it in every revision.
		data, err := scope.loaded.packBytes(*pack)
		if err != nil {
			problems = append(problems, FieldProblem{Field: "profile.pack", Problem: err.Error()})
		}
		carried = string(data)
	}
	if carried != "" {
		staged = append(staged, catalog.Staged{Role: packRole, File: "pack.json", Data: []byte(carried)})
	}
	if held.Origin != nil {
		origin := *held.Origin
		if origin.Schema == "" {
			origin.Schema = profilepackage.OriginSchema
		}
		encoded, err := json.Marshal(origin, json.Deterministic(true), jsontext.WithIndent("  "))
		if err == nil {
			origin, err = profilepackage.DecodeOrigin(append(encoded, '\n'))
		}
		if err != nil {
			problems = append(problems, FieldProblem{Field: "profile.origin", Problem: err.Error()})
		} else {
			normalized.Origin = &origin
			staged = append(staged, catalog.Staged{Role: originRole, File: "origin.json", Data: append(encoded, '\n')})
		}
	}
	if len(problems) > 0 {
		return nil, nil, problems
	}
	return staged, normalized, nil
}

// importedPack decides the metadata pack an imported package carried: the
// project's own pack when it already holds exactly that pack, or the document
// to record beside the profile when it holds none by that name. A different
// pack under the same identity, or one that is not the pack the profile pins,
// is a problem at the pack.
func (s draftScope) importedPack(profile localprofile.Profile, document string) (*ItemRef, string, *FieldProblem) {
	problem := func(text string) (*ItemRef, string, *FieldProblem) {
		return nil, "", &FieldProblem{Field: "profile.pack_document", Problem: text}
	}
	carried, err := profilepack.Decode([]byte(document))
	if err != nil {
		return problem("the imported package's metadata pack cannot be read: " + err.Error())
	}
	if carried.Satisfies(profile.Base.Pack) != nil {
		return problem("the imported package's metadata pack is not the pack and version this profile pins")
	}
	if s.loaded == nil {
		return nil, document, nil
	}
	for _, item := range s.loaded.document.Items {
		// The object this very submission already published is not another
		// pack, so a repeated click decides the same way again.
		if item.Kind != string(ProfileItem) || s.loaded.removed(item) ||
			s.intent != "" && slices.ContainsFunc(item.Revisions, func(revision catalog.Revision) bool { return revision.Intent == s.intent }) {
			continue
		}
		ref := ItemRef{Kind: ProfileItem, ID: item.ID, Revision: item.RevisionLabel()}
		held, err := s.loaded.packOf(ref)
		if err != nil || held.Identity != carried.Identity {
			continue
		}
		if !reflect.DeepEqual(held, carried) {
			return problem("the project already holds a different metadata pack named " + carried.Identity.ID + " version " + carried.Identity.Version +
				"; the imported package's pack is not saved over it")
		}
		return &ref, "", nil
	}
	return nil, document, nil
}

// profileWithPack is a profile draft as a save decides it: the contract
// declared, an id drawn from the name a new profile is given, and the pin
// taken from the metadata pack chosen when the profile pins none yet. A pack
// chosen that is not the one the profile pins is a problem.
func (s draftScope) profileWithPack(name string, held ProfileDraft) (localprofile.Profile, *ItemRef, []FieldProblem) {
	profile := held.Profile
	if profile.Schema == "" {
		profile.Schema = localprofile.Schema
	}
	if profile.Identity.ID == "" {
		profile.Identity.ID = actionSlug(name)
	}
	if profile.Segments == nil {
		profile.Segments = []localprofile.Segment{}
	}
	if held.Pack == nil || held.Pack.ID == "" {
		return profile, nil, nil
	}
	pack, err := s.pack(*held.Pack)
	switch {
	case err != nil:
		return profile, nil, []FieldProblem{{Field: "profile.pack", Problem: err.Error()}}
	case profile.Base.Pack == profilepack.Identity{}:
		profile.Base.Pack = pack.Identity
	case pack.Satisfies(profile.Base.Pack) != nil:
		return profile, nil, []FieldProblem{{Field: "profile.pack", Problem: "the metadata pack chosen is not the pack and version this profile pins"}}
	}
	ref := *held.Pack
	return profile, &ref, nil
}

// pack reads the metadata pack one profile object of the project is.
func (s draftScope) pack(ref ItemRef) (profilepack.Pack, error) {
	if s.loaded == nil {
		return profilepack.Pack{}, errors.New("the project's metadata packs cannot be read")
	}
	return s.loaded.packOf(ref)
}

// packOf reads the metadata pack one profile object of the project is, at
// any pack version this release reads.
func (c *loadedCatalog) packOf(ref ItemRef) (profilepack.Pack, error) {
	index := c.document.Find(ref.ID)
	if ref.Kind != ProfileItem || index < 0 || c.document.Items[index].Kind != string(ProfileItem) || c.removed(c.document.Items[index]) {
		return profilepack.Pack{}, errors.New("the project holds no such metadata pack")
	}
	data, err := c.packBytes(ref)
	if err != nil {
		return profilepack.Pack{}, err
	}
	pack, err := profileeval.DecodePack(data)
	if err != nil {
		return profilepack.Pack{}, err
	}
	return pack.Metadata, nil
}

// packBytes is the metadata pack document one profile object is or carries:
// a pack the project holds as its own object, or the pack a local profile
// recorded from an imported package.
func (c *loadedCatalog) packBytes(ref ItemRef) ([]byte, error) {
	index := c.document.Find(ref.ID)
	if ref.Kind != ProfileItem || index < 0 || c.document.Items[index].Kind != string(ProfileItem) || c.removed(c.document.Items[index]) {
		return nil, errors.New("the project holds no such metadata pack")
	}
	paths, availability, reason := c.backing(c.document.Items[index])
	if availability != ItemAvailable {
		return nil, errors.New(reason)
	}
	path, held := paths[packRole]
	if !held {
		path = paths[profileRole]
	}
	return boundedFile(path, profilepack.MaxPackBytes)
}

// earlierProfiles are the profiles the object an edit began from published,
// oldest first, except a revision the submission itself published, so a
// repeated click decides the same way again.
func (s draftScope) earlierProfiles() []localprofile.Profile {
	if s.loaded == nil || s.item == "" {
		return nil
	}
	index := s.loaded.document.Find(s.item)
	if index < 0 {
		return nil
	}
	record := s.loaded.document.Items[index]
	profiles := []localprofile.Profile{}
	if len(record.Revisions) == 0 {
		if paths, availability, _ := s.loaded.backing(record); availability == ItemAvailable {
			if profile, _, err := readLocalProfile(paths); err == nil {
				profiles = append(profiles, profile)
			}
		}
		return profiles
	}
	for _, revision := range record.Revisions {
		if s.intent != "" && revision.Intent == s.intent {
			continue
		}
		paths, availability, _ := s.loaded.membersBacking(revision.Members)
		if availability != ItemAvailable {
			continue
		}
		if profile, _, err := readLocalProfile(paths); err == nil {
			profiles = append(profiles, profile)
		}
	}
	return profiles
}

// readLocalProfile reads the local profile and origin one profile object's
// files declare: a local profile, or a package, whose profile and origin are
// read from its verified documents. A metadata pack is not a local profile.
func readLocalProfile(paths map[string]string) (localprofile.Profile, *profilepackage.Origin, error) {
	path := paths[profileRole]
	data, err := boundedFile(path, profilepackage.MaxBytes)
	if err != nil {
		return localprofile.Profile{}, nil, err
	}
	schema, _ := sniffSchema(path)
	switch {
	case strings.HasPrefix(schema, "readmit-profile-pack/"):
		return localprofile.Profile{}, nil, errors.New("a metadata pack is read-only; it is listed under Metadata packs")
	case strings.HasPrefix(schema, "readmit-profile-package/"):
		return profileFromPackage(data)
	}
	profile, err := localprofile.Decode(data)
	if err != nil {
		return localprofile.Profile{}, nil, err
	}
	var origin *profilepackage.Origin
	if path, held := paths[originRole]; held {
		data, err := boundedFile(path, profilepackage.MaxOriginBytes)
		if err != nil {
			return localprofile.Profile{}, nil, err
		}
		decoded, err := profilepackage.DecodeOrigin(data)
		if err != nil {
			return localprofile.Profile{}, nil, err
		}
		origin = &decoded
	}
	return profile, origin, nil
}

// profileFromPackage is the profile and origin a verified package carries.
func profileFromPackage(data []byte) (localprofile.Profile, *profilepackage.Origin, error) {
	verified, err := profilepackage.Decode(data)
	if err != nil {
		return localprofile.Profile{}, nil, err
	}
	documents, err := verified.Documents()
	if err != nil {
		return localprofile.Profile{}, nil, err
	}
	profile, err := localprofile.Decode(documents["profile.json"])
	if err != nil {
		return localprofile.Profile{}, nil, err
	}
	origin, err := profilepackage.DecodeOrigin(documents["origin.json"])
	if err != nil {
		return localprofile.Profile{}, nil, err
	}
	return profile, &origin, nil
}

func verifyProfile(files map[string]string) error {
	profile, _, err := readLocalProfile(files)
	if err != nil {
		return err
	}
	data, err := boundedFile(files[sealRole], profileversion.MaxVersionBytes)
	if err != nil {
		return err
	}
	seal, err := profileversion.DecodeVersion(data)
	if err != nil {
		return err
	}
	if err := seal.Verify(profile); err != nil {
		return err
	}
	if path, held := files[packRole]; held {
		data, err := boundedFile(path, profilepack.MaxPackBytes)
		if err != nil {
			return err
		}
		pack, err := profilepack.Decode(data)
		if err != nil {
			return err
		}
		return pack.Satisfies(profile.Base.Pack)
	}
	return nil
}

// membersBacking resolves the files of one published revision, each still the
// bytes that were published.
func (c *loadedCatalog) membersBacking(members []catalog.Member) (map[string]string, Availability, string) {
	return c.backing(catalog.Item{Revisions: []catalog.Revision{{Members: members}}})
}
