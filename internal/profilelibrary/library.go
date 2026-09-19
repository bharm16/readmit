// Package profilelibrary holds the multi-version HL7 profile library: the set
// of readmit-profile-pack/v1 packs one release carries, and the single place
// their answers are combined and published.
//
// A library is not a new document. Every pack in it is already the versioned
// strict-JSON contract internal/profilepack reads, and a library is the
// directory those documents sit in: no pack gains a member, no byte of one
// changes, and nothing here interprets pack content the pack reader did not
// already interpret, as ADR-0003 requires. There is no second reader, no
// second copy of the segment rule and no second definition of a support level.
//
// Combining answers is the only decision this package makes, and it makes it
// by refusing to decide. Two packs that declare the same HL7 version and
// message family are refused when the library is opened, because a library
// that chose between them would be guessing at which upstream is right. Every
// question is therefore answered by exactly one pack or by nobody. There is no
// precedence, no nearest version, no fallback from one family or level to
// another, and no merge of two packs' content.
//
// The matrix a library publishes is complete: all 28 combinations of the seven
// HL7 versions and four message families D1 decided, covered or not, so the
// combinations nothing covers are published rather than inferred from silence.
// Every one of them answers unknown at all four levels, and unknown does not
// pass.
//
// Rights review is a person's work. Bundleable reports whether every pack in
// the library records an approved review; it cannot establish that the review
// happened, that it covered the exact extracted content, or that the content
// may be redistributed. No library is bundled with this release.
package profilelibrary

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/profilepack"
)

// MaxPacks bounds the pack documents one library holds. Seven HL7 versions
// from two upstream sources need far fewer; an unbounded directory is refused
// rather than read.
const MaxPacks = 32

// Library is the set of packs one release carries. The zero Library holds no
// pack: it answers unknown for every combination, publishes the complete
// matrix as unknown, and is not bundleable, which is exactly what this release
// carries.
type Library struct {
	packs   []profilepack.Pack
	answers map[profilepack.Combination]int
}

// Entry is one pack the library holds, with the provenance its publisher
// recorded. Pack content is not exposed here: a field name is read through
// Label, which withholds it unless the combination's labels are supported.
type Entry struct {
	Pack       profilepack.Identity
	Provenance profilepack.Provenance
}

// Answer is the library's answer about one level of one combination, together
// with the pack that gave it. Pack is the zero identity when the outcome is
// unknown, because nothing answered.
type Answer struct {
	Outcome profilepack.Outcome
	Pack    profilepack.Identity
}

// Label is the library's answer about one field position. Name is nonempty
// only when Outcome is supported and the answering pack names the position.
type Label struct {
	Outcome profilepack.Outcome
	Name    string
	Pack    profilepack.Identity
}

// Row is one published combination of the support matrix: the four levels
// answered separately, and the pack that declared them. Pack is the zero
// identity when no pack declares the combination.
type Row struct {
	HL7Version string
	Family     string
	Parse      profilepack.Outcome
	Labels     profilepack.Outcome
	Structural profilepack.Outcome
	Workflow   profilepack.Outcome
	Pack       profilepack.Identity
}

// Open reads one directory of profile packs as a library. The directory holds
// regular pack documents named *.json and nothing else: a subdirectory, a
// symbolic link, a device, a socket and any other file are each refused rather
// than skipped, so a library is exactly what somebody put in it and no member
// of one is read through a link out of it. Every member is bounded before it
// is read, not after. Documents are read in name order, each through the pack
// reader, and the library is refused whole the moment one of them is.
func Open(directory string) (Library, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return Library{}, errors.New("cannot read a profile library directory")
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type() != 0 || !strings.HasSuffix(entry.Name(), ".json") {
			return Library{}, errors.New("a profile library directory holds regular profile pack documents named *.json and nothing else")
		}
		names = append(names, entry.Name())
	}
	if len(names) > MaxPacks {
		return Library{}, errors.New("a profile library holds at most 32 packs")
	}
	slices.Sort(names)
	library := Library{answers: make(map[profilepack.Combination]int)}
	for _, name := range names {
		data, err := read(filepath.Join(directory, name))
		if err != nil {
			return Library{}, err
		}
		pack, err := profilepack.Decode(data)
		if err != nil {
			return Library{}, err
		}
		if err := library.add(pack); err != nil {
			return Library{}, err
		}
	}
	return library, nil
}

// read opens one library member and reads it under the pack contract's own
// size limit. The file is opened first and measured through that open file, so
// a member that is not a regular file, or is longer than a pack may be, is
// refused before its bytes are held in memory rather than after.
func read(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read a profile library member")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("a profile library member is one regular file")
	}
	if info.Size() > profilepack.MaxPackBytes {
		return nil, errors.New("a profile library member exceeds the 4 MiB a profile pack may be")
	}
	data, err := io.ReadAll(io.LimitReader(file, profilepack.MaxPackBytes+1))
	if err != nil {
		return nil, errors.New("cannot read a profile library member")
	}
	if len(data) > profilepack.MaxPackBytes {
		return nil, errors.New("a profile library member exceeds the 4 MiB a profile pack may be")
	}
	return data, nil
}

// add holds one more pack, refusing every library that could not answer
// without choosing. Nothing is written until every refusal has been checked,
// so a refused library is never half assembled.
func (l *Library) add(pack profilepack.Pack) error {
	for _, held := range l.packs {
		if held.Identity.ID == pack.Identity.ID {
			return errors.New("a profile library holds one version of each pack id, and holds " + pack.Identity.ID + " more than once")
		}
	}
	combinations := make([]profilepack.Combination, 0, len(pack.Coverage))
	for _, coverage := range pack.Coverage {
		combination := profilepack.Combination{Version: coverage.HL7Version, Family: coverage.Family}
		if held, ok := l.answers[combination]; ok {
			return errors.New("the packs " + l.packs[held].Identity.ID + " and " + pack.Identity.ID +
				" both declare HL7 " + combination.Version + " " + combination.Family + ", and a library chooses between neither")
		}
		combinations = append(combinations, combination)
	}
	index := len(l.packs)
	for _, combination := range combinations {
		l.answers[combination] = index
	}
	l.packs = append(l.packs, pack)
	return nil
}

// Entries lists what the library holds, in the order it was read, each with
// the provenance and rights review its publisher recorded.
func (l Library) Entries() []Entry {
	entries := make([]Entry, 0, len(l.packs))
	for _, pack := range l.packs {
		entries = append(entries, Entry{Pack: pack.Identity, Provenance: pack.Provenance})
	}
	return entries
}

// Support answers for one level of one combination. The pack that declares the
// combination answers it alone; a combination no pack declares, and a version,
// family or level outside the closed sets, are unknown. Nothing is borrowed
// from a neighbouring pack, version, family or level.
func (l Library) Support(version, family string, level profilepack.Level) Answer {
	pack, ok := l.declaring(version, family)
	if !ok {
		return Answer{Outcome: profilepack.OutcomeUnknown}
	}
	outcome := pack.Support(version, family, level)
	if outcome == profilepack.OutcomeUnknown {
		return Answer{Outcome: profilepack.OutcomeUnknown}
	}
	return Answer{Outcome: outcome, Pack: pack.Identity}
}

// Label answers for one field position under one combination, from the pack
// that declares it and no other. The name is withheld unless that pack's
// labels level is supported for this combination, so content a pack carries
// for a version cannot reach a family it was not verified for, and a second
// pack's labels never fill a first pack's gap.
func (l Library) Label(version, family, segment string, position int) Label {
	pack, ok := l.declaring(version, family)
	if !ok {
		return Label{Outcome: profilepack.OutcomeUnknown}
	}
	found := pack.Label(version, family, segment, position)
	if found.Outcome == profilepack.OutcomeUnknown {
		return Label{Outcome: profilepack.OutcomeUnknown}
	}
	return Label{Outcome: found.Outcome, Name: found.Name, Pack: pack.Identity}
}

// Matrix is the published version-by-message-family support matrix: every one
// of the 28 combinations D1 decided, in version then family order, whether a
// pack covers it or not. A combination nothing covers is published as unknown
// at all four levels rather than left out.
func (l Library) Matrix() []Row {
	versions := profilepack.HL7Versions()
	families := profilepack.Families()
	rows := make([]Row, 0, len(versions)*len(families))
	for _, version := range versions {
		for _, family := range families {
			row := Row{
				HL7Version: version,
				Family:     family,
				Parse:      l.Support(version, family, profilepack.LevelParse).Outcome,
				Labels:     l.Support(version, family, profilepack.LevelLabels).Outcome,
				Structural: l.Support(version, family, profilepack.LevelStructural).Outcome,
				Workflow:   l.Support(version, family, profilepack.LevelWorkflow).Outcome,
			}
			// The row names the pack that declares the combination, not the
			// pack that happened to answer one level of it.
			if pack, ok := l.declaring(version, family); ok {
				row.Pack = pack.Identity
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// Bundleable reports whether this library may be bundled with a release. It
// requires at least one pack, because an empty library is not a library to
// ship, and an approved recorded rights review on every pack it holds. It
// checks what was written down; it cannot establish that a review happened,
// that it covered the exact extracted content, or that the content may be
// redistributed. A library with a pending review stays readable, which is how
// a reviewer inspects exactly what would ship.
func (l Library) Bundleable() error {
	if len(l.packs) == 0 {
		return errors.New("a profile library holding no pack is not a library to bundle")
	}
	for _, pack := range l.packs {
		if pack.Provenance.RightsReview.Status != profilepack.ReviewApproved {
			return errors.New("the pack " + pack.Identity.ID + " records no approved rights review, so it is readable and not bundleable")
		}
	}
	return nil
}

// declaring finds the one pack that declares a combination. A zero Library has
// no map and therefore declares nothing.
func (l Library) declaring(version, family string) (profilepack.Pack, bool) {
	index, ok := l.answers[profilepack.Combination{Version: version, Family: family}]
	if !ok {
		return profilepack.Pack{}, false
	}
	return l.packs[index], true
}
