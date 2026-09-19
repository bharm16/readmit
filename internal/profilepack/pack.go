// Package profilepack reads the shared profile-pack contract of ADR-0009: one
// versioned strict-JSON document that carries normalized HL7 metadata for the
// byte-preserving parser to answer questions with, and says explicitly, per
// HL7 version and message family, which questions it can answer.
//
// A pack is data interpreted by typed Go operators, exactly as a test spec is.
// It carries no command, script, interpreter, expression or program path, and
// nothing in it changes how a message is parsed: the parser keeps decoding
// evidence as views over the original bytes, and a pack is consulted after the
// fact for what a position means.
//
// Four support levels are four separate answers. Parsing, field labels,
// structural validation and workflow semantics are declared distinctly for
// every combination a pack covers, and an answer is never borrowed: a
// combination the pack does not declare is unknown, a level it declares
// unsupported or untested does not pass, and a name is returned for a field
// only under a combination whose labels are declared supported. This contract
// version carries labels content and nothing else, so a v1 pack cannot claim
// structural or workflow support; a later contract name adds that content
// with a reader that supports both, as ADR-0003 requires.
//
// Metadata is not clinical certification. A pack records where its content
// came from, how it was extracted, under which license, and whether
// redistribution of that exact content was reviewed; the reader checks that
// those declarations are present and well formed, and cannot establish that
// they are true. The bundled readmit-field-labels/v1 dictionary is unchanged
// by this package and is not read through it.
package profilepack

import (
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// Schema is the contract a profile pack declares.
const Schema = "readmit-profile-pack/v1"

const (
	// MaxPackBytes bounds the document a reader decodes. Seven versions of
	// field labels for four families fit comfortably; an unbounded document
	// is refused rather than read.
	MaxPackBytes = 4 << 20
	// maxSegments bounds the labelled segments one version carries.
	maxSegments = 512
	// maxPosition bounds a labelled field position. The parser numbers
	// positions from one; a pack labels nothing beyond this.
	maxPosition = 999
	// maxLabelBytes bounds one field name.
	maxLabelBytes = 128
	// maxProseBytes bounds the extraction description a reviewer reads.
	maxProseBytes = 1024
	// maxShortBytes bounds every other free-text provenance member.
	maxShortBytes = 256
	// maxSPDXBytes bounds a license identifier.
	maxSPDXBytes = 64
)

// hl7Versions and families are the closed sets D1 decided. A version or
// family outside them is refused in a declaration and unknown in a query.
var (
	hl7Versions = []string{"2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2"}
	families    = []string{"ADT", "SIU", "ORM", "ORU"}
)

// HL7Versions returns the HL7 versions a pack may declare coverage for.
func HL7Versions() []string { return slices.Clone(hl7Versions) }

// Families returns the message families a pack may declare coverage for.
func Families() []string { return slices.Clone(families) }

// Level names one of the four separately declared support levels.
type Level string

const (
	// LevelParse: messages of this combination were parsed by readmit's own
	// byte-preserving parser against independently authored fixtures.
	LevelParse Level = "parse"
	// LevelLabels: the pack carries field names for this combination's
	// version, and they were checked against independently authored fixtures.
	LevelLabels Level = "labels"
	// LevelStructural: segment order, cardinality and required fields. No v1
	// pack carries this content, so no v1 pack may declare it supported.
	LevelStructural Level = "structural"
	// LevelWorkflow: what a message means for an appointment, an order or a
	// result. No v1 pack carries this content, so no v1 pack may declare it
	// supported; a dictionary does not certify clinical behavior.
	LevelWorkflow Level = "workflow"
)

// levels is the closed set of support levels, in the order a coverage entry
// declares them. A level outside it is unknown in a query.
var levels = []Level{LevelParse, LevelLabels, LevelStructural, LevelWorkflow}

// Levels returns the support levels a pack declares separately for every
// combination it covers, so a consumer that walks all four does not keep its
// own copy of the set.
func Levels() []Level { return slices.Clone(levels) }

// Support is what a pack declares for one level of one combination.
type Support string

const (
	// Supported was verified against independently authored fixtures.
	Supported Support = "supported"
	// Untested carries no verification either way. It does not pass.
	Untested Support = "untested"
	// Unsupported was decided against, or found not to work. It does not pass.
	Unsupported Support = "unsupported"
)

// ReviewStatus is where the rights review of a pack's exact content stands.
type ReviewStatus string

const (
	// ReviewPending: the content may be read and inspected, and may not be
	// bundled with a release until the review is recorded.
	ReviewPending ReviewStatus = "pending"
	// ReviewApproved: redistribution of the exact extracted content under the
	// declared license was reviewed, and Reference names the record.
	ReviewApproved ReviewStatus = "approved"
)

// Pack is one profile pack exactly as written. Only Decode produces one that
// answers: a Pack assembled in Go without going through the reader answers
// unknown at every level, so the reader's refusals cannot be bypassed by
// construction.
type Pack struct {
	Schema string `json:"schema"`
	// Identity is what a consumer pins.
	Identity   Identity   `json:"pack"`
	Provenance Provenance `json:"provenance"`
	// Coverage declares, per HL7 version and family, the four support levels.
	// A combination absent from it is unknown, never inferred.
	Coverage []Coverage `json:"coverage"`
	// Labels carries field names per HL7 version. It is optional: a pack that
	// declares parse support only carries none.
	Labels []Labels `json:"labels,omitzero"`

	decoded bool
}

// Identity is what a consumer pins: the pack's id and version, compared byte
// for byte. There are no ranges and no "latest".
type Identity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// Provenance records where a pack's content came from. Every member is
// required, because a pack whose origin nobody wrote down cannot be reviewed.
type Provenance struct {
	Source       Source       `json:"source"`
	Extraction   Extraction   `json:"extraction"`
	License      License      `json:"license"`
	RightsReview RightsReview `json:"rights_review"`
}

// Source names the upstream the content was taken from, pinned to a revision.
type Source struct {
	Name     string `json:"name"`
	Location string `json:"location"`
	Revision string `json:"revision"`
}

// Extraction records how the content was produced from the source and a
// SHA-256 over the exact extracted content, in the form the rights review
// saw it (for the bundled labels, dictionary/fields-v251.json). The digest
// is a recorded claim; the reader checks its shape and cannot verify it.
type Extraction struct {
	Method        string `json:"method"`
	ContentDigest string `json:"content_digest"`
}

// License is the SPDX identifier the content is redistributed under and the
// notice file, as one path inside the distribution, that carries its text.
type License struct {
	SPDX   string `json:"spdx"`
	Notice string `json:"notice"`
}

// RightsReview is the gate before an extraction is bundled. An approved review
// names its record; a pending one is readable and not bundleable.
type RightsReview struct {
	Status    ReviewStatus `json:"status"`
	Reference string       `json:"reference"`
}

// Coverage declares the four levels for one version and family.
type Coverage struct {
	HL7Version string  `json:"hl7_version"`
	Family     string  `json:"family"`
	Parse      Support `json:"parse"`
	Labels     Support `json:"labels"`
	Structural Support `json:"structural"`
	Workflow   Support `json:"workflow"`
}

// Labels carries the field names for one HL7 version, in the same shape the
// bundled dictionary uses: segment id to one-based position to name.
type Labels struct {
	HL7Version string                    `json:"hl7_version"`
	Segments   map[string]map[int]string `json:"segments"`
}

// UnmarshalJSON reads one identity exactly as written: presence first, then
// the same bytes again rejecting unknown members.
func (i *Identity) UnmarshalJSON(data []byte) error {
	var required struct {
		ID      *string `json:"id"`
		Version *string `json:"version"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.ID == nil || required.Version == nil {
		return errors.New("a pack identity requires id and version")
	}
	type identity Identity
	var decoded identity
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a pack identity declares no member beyond id and version")
	}
	*i = Identity(decoded)
	return nil
}

// UnmarshalJSON reads provenance exactly as written, requiring every member.
func (p *Provenance) UnmarshalJSON(data []byte) error {
	var required struct {
		Source       *Source       `json:"source"`
		Extraction   *Extraction   `json:"extraction"`
		License      *License      `json:"license"`
		RightsReview *RightsReview `json:"rights_review"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Source == nil || required.Extraction == nil || required.License == nil || required.RightsReview == nil {
		return errors.New("pack provenance requires source, extraction, license and rights_review")
	}
	type provenance Provenance
	var decoded provenance
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("pack provenance declares no member beyond source, extraction, license and rights_review")
	}
	*p = Provenance(decoded)
	return nil
}

// UnmarshalJSON reads one source exactly as written.
func (s *Source) UnmarshalJSON(data []byte) error {
	var required struct {
		Name     *string `json:"name"`
		Location *string `json:"location"`
		Revision *string `json:"revision"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Name == nil || required.Location == nil || required.Revision == nil {
		return errors.New("a pack source requires name, location and revision")
	}
	type source Source
	var decoded source
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a pack source declares no member beyond name, location and revision")
	}
	*s = Source(decoded)
	return nil
}

// UnmarshalJSON reads one extraction record exactly as written.
func (e *Extraction) UnmarshalJSON(data []byte) error {
	var required struct {
		Method        *string `json:"method"`
		ContentDigest *string `json:"content_digest"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Method == nil || required.ContentDigest == nil {
		return errors.New("a pack extraction requires method and content_digest")
	}
	type extraction Extraction
	var decoded extraction
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a pack extraction declares no member beyond method and content_digest")
	}
	*e = Extraction(decoded)
	return nil
}

// UnmarshalJSON reads one license record exactly as written.
func (l *License) UnmarshalJSON(data []byte) error {
	var required struct {
		SPDX   *string `json:"spdx"`
		Notice *string `json:"notice"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.SPDX == nil || required.Notice == nil {
		return errors.New("a pack license requires spdx and notice")
	}
	type license License
	var decoded license
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a pack license declares no member beyond spdx and notice")
	}
	*l = License(decoded)
	return nil
}

// UnmarshalJSON reads one rights review exactly as written.
func (r *RightsReview) UnmarshalJSON(data []byte) error {
	var required struct {
		Status    *ReviewStatus `json:"status"`
		Reference *string       `json:"reference"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Status == nil || required.Reference == nil {
		return errors.New("a pack rights review requires status and reference")
	}
	type review RightsReview
	var decoded review
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a pack rights review declares no member beyond status and reference")
	}
	*r = RightsReview(decoded)
	return nil
}

// UnmarshalJSON reads one coverage entry exactly as written. All four levels
// are required: an omitted level would otherwise read as the empty string,
// and the empty string is not a declaration.
func (c *Coverage) UnmarshalJSON(data []byte) error {
	var required struct {
		HL7Version *string  `json:"hl7_version"`
		Family     *string  `json:"family"`
		Parse      *Support `json:"parse"`
		Labels     *Support `json:"labels"`
		Structural *Support `json:"structural"`
		Workflow   *Support `json:"workflow"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.HL7Version == nil || required.Family == nil ||
		required.Parse == nil || required.Labels == nil || required.Structural == nil || required.Workflow == nil {
		return errors.New("a coverage entry requires hl7_version, family, parse, labels, structural and workflow")
	}
	type coverage Coverage
	var decoded coverage
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a coverage entry declares no member beyond hl7_version, family, parse, labels, structural and workflow")
	}
	*c = Coverage(decoded)
	return nil
}

// UnmarshalJSON reads one labels entry exactly as written.
func (l *Labels) UnmarshalJSON(data []byte) error {
	var required struct {
		HL7Version *string                    `json:"hl7_version"`
		Segments   *map[string]map[int]string `json:"segments"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.HL7Version == nil || required.Segments == nil {
		return errors.New("a labels entry requires hl7_version and segments")
	}
	type labels Labels
	var decoded labels
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a labels entry declares no member beyond hl7_version and segments, and every position is a positive integer")
	}
	*l = Labels(decoded)
	return nil
}

// Decode reads one profile pack exactly as written and refuses every document
// it could not stand behind: unknown members, a contract this release does not
// read, a version or family outside the decided sets, a combination declared
// twice, a level that claims what the pack carries no content for, and
// content no coverage entry accounts for.
func Decode(data []byte) (Pack, error) {
	if len(data) > MaxPackBytes {
		return Pack{}, errors.New("a profile pack exceeds its 4 MiB size limit")
	}
	var required struct {
		Schema     *string     `json:"schema"`
		Pack       *Identity   `json:"pack"`
		Provenance *Provenance `json:"provenance"`
		Coverage   *[]Coverage `json:"coverage"`
	}
	if err := json.Unmarshal(data, &required); err != nil {
		return Pack{}, errors.New("invalid profile pack JSON")
	}
	if required.Schema == nil || *required.Schema != Schema {
		return Pack{}, errors.New("a profile pack must declare " + Schema)
	}
	if required.Pack == nil || required.Provenance == nil || required.Coverage == nil {
		return Pack{}, errors.New("a profile pack requires pack, provenance and coverage")
	}
	var pack Pack
	if err := json.Unmarshal(data, &pack, json.RejectUnknownMembers(true)); err != nil {
		return Pack{}, errors.New("invalid profile pack JSON")
	}
	if err := validate(pack); err != nil {
		return Pack{}, err
	}
	pack.decoded = true
	return pack, nil
}

func validate(pack Pack) error {
	if err := validateIdentity(pack.Identity); err != nil {
		return err
	}
	if err := validateProvenance(pack.Provenance); err != nil {
		return err
	}
	if len(pack.Coverage) == 0 || len(pack.Coverage) > len(hl7Versions)*len(families) {
		return errors.New("a profile pack declares between 1 and 28 coverage entries")
	}
	labelled := make(map[string]bool, len(pack.Labels))
	for _, labels := range pack.Labels {
		if labelled[labels.HL7Version] {
			return errors.New("a profile pack carries labels for one HL7 version twice")
		}
		labelled[labels.HL7Version] = true
	}
	declared := make(map[Combination]bool, len(pack.Coverage))
	claimsLabels := make(map[string]bool, len(pack.Labels))
	for _, coverage := range pack.Coverage {
		if err := validateCoverage(coverage, labelled); err != nil {
			return err
		}
		combination := Combination{Version: coverage.HL7Version, Family: coverage.Family}
		if declared[combination] {
			return errors.New("a profile pack declares one HL7 version and family combination twice")
		}
		declared[combination] = true
		if coverage.Labels == Supported {
			claimsLabels[coverage.HL7Version] = true
		}
	}
	for _, labels := range pack.Labels {
		if !claimsLabels[labels.HL7Version] {
			return errors.New("a profile pack carries labels for an HL7 version no coverage entry declares labels supported for")
		}
		if err := validateLabels(labels); err != nil {
			return err
		}
	}
	return nil
}

func validateIdentity(identity Identity) error {
	if identity.ID == "" || len(identity.ID) > 64 || identity.ID[0] < 'a' || identity.ID[0] > 'z' {
		return errors.New("a pack id begins with a lowercase letter and is at most 64 bytes")
	}
	for _, r := range identity.ID {
		if r != '-' && (r < '0' || r > '9') && (r < 'a' || r > 'z') {
			return errors.New("a pack id holds lowercase letters, digits and '-' only")
		}
	}
	if identity.Version == "" || len(identity.Version) > 32 || identity.Version[0] < '0' || identity.Version[0] > '9' {
		return errors.New("a pack version begins with a digit and is at most 32 bytes")
	}
	for _, r := range identity.Version {
		if r != '-' && r != '.' && (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return errors.New("a pack version holds letters, digits, '.' and '-' only")
		}
	}
	return nil
}

func validateProvenance(provenance Provenance) error {
	for _, member := range []struct {
		name, value string
		limit       int
	}{
		{"source name", provenance.Source.Name, maxShortBytes},
		{"source location", provenance.Source.Location, maxShortBytes},
		{"source revision", provenance.Source.Revision, maxShortBytes},
		{"extraction method", provenance.Extraction.Method, maxProseBytes},
		{"license spdx", provenance.License.SPDX, maxSPDXBytes},
	} {
		if member.value == "" || len(member.value) > member.limit || !readableLine(member.value) {
			return errors.New("a pack " + member.name + " is one nonempty line of readable text")
		}
	}
	if !contentDigest(provenance.Extraction.ContentDigest) {
		return errors.New("a pack content_digest is sha256: followed by 64 lowercase hexadecimal digits")
	}
	if !distributionPath(provenance.License.Notice) {
		return errors.New("a pack license notice is one relative path inside the distribution")
	}
	switch provenance.RightsReview.Status {
	case ReviewPending:
		if len(provenance.RightsReview.Reference) > maxShortBytes || !readableLine(provenance.RightsReview.Reference) {
			return errors.New("a pack rights review reference is one line of readable text")
		}
	case ReviewApproved:
		if provenance.RightsReview.Reference == "" || len(provenance.RightsReview.Reference) > maxShortBytes || !readableLine(provenance.RightsReview.Reference) {
			return errors.New("an approved rights review names its record in reference")
		}
	default:
		return errors.New("a pack rights review status is pending or approved")
	}
	return nil
}

func validateCoverage(coverage Coverage, labelled map[string]bool) error {
	if !slices.Contains(hl7Versions, coverage.HL7Version) {
		return errors.New("a coverage entry names one of the HL7 versions " + strings.Join(hl7Versions, ", "))
	}
	if !slices.Contains(families, coverage.Family) {
		return errors.New("a coverage entry names one of the message families " + strings.Join(families, ", "))
	}
	for _, level := range []Support{coverage.Parse, coverage.Labels, coverage.Structural, coverage.Workflow} {
		if level != Supported && level != Untested && level != Unsupported {
			return errors.New("a support level is supported, untested or unsupported")
		}
	}
	if coverage.Parse != Supported && (coverage.Labels == Supported || coverage.Structural == Supported || coverage.Workflow == Supported) {
		return errors.New("a coverage entry cannot declare labels, structural or workflow supported without parse supported")
	}
	if coverage.Structural == Supported || coverage.Workflow == Supported {
		return errors.New(Schema + " carries no structural or workflow content, so a coverage entry cannot declare either supported")
	}
	if coverage.Labels == Supported && !labelled[coverage.HL7Version] {
		return errors.New("a coverage entry declares labels supported for an HL7 version the pack carries no labels for")
	}
	return nil
}

func validateLabels(labels Labels) error {
	if !slices.Contains(hl7Versions, labels.HL7Version) {
		return errors.New("a labels entry names one of the HL7 versions " + strings.Join(hl7Versions, ", "))
	}
	if len(labels.Segments) == 0 || len(labels.Segments) > maxSegments {
		return errors.New("a labels entry carries between 1 and 512 segments")
	}
	for segment, fields := range labels.Segments {
		if !segmentID(segment) {
			return errors.New("a labelled segment id is three uppercase letters or digits, beginning with a letter")
		}
		if len(fields) == 0 {
			return errors.New("a labelled segment carries at least one position")
		}
		for position, name := range fields {
			if position < 1 || position > maxPosition {
				return errors.New("a labelled position is between 1 and 999")
			}
			if name == "" || len(name) > maxLabelBytes || !readableLine(name) {
				return errors.New("a field label is one nonempty line of at most 128 UTF-8 bytes")
			}
		}
	}
	return nil
}

// segmentID is the parser's own rule for a segment identifier, so a pack
// cannot label a segment the parser would never produce.
func segmentID(id string) bool {
	if len(id) != 3 {
		return false
	}
	for i, c := range []byte(id) {
		if !(c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func contentDigest(digest string) bool {
	rest, ok := strings.CutPrefix(digest, "sha256:")
	if !ok || len(rest) != 64 {
		return false
	}
	for _, r := range rest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func distributionPath(path string) bool {
	return path != "" && len(path) <= maxShortBytes && readableLine(path) && filepath.IsLocal(path) && !strings.Contains(path, "\\")
}

// readableLine bounds what a member of a pack may say to the person reading
// it: valid UTF-8 with no control character at all, because a document
// somebody imported must not be able to drive the terminal it is displayed on.
func readableLine(text string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
