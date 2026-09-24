// Package localprofile reads and writes readmit-local-profile/v1: the document
// a team writes down its own interface contract in, beside the shared
// readmit-profile-pack/v1 metadata rather than inside it.
//
// A local profile constrains one HL7 version and message family of one pinned
// pack. It carries site-defined Z-segments and the standard segments a site
// constrains, and per field a usage code, a conditional requirement, a
// cardinality, a data type, a local terminology set, an assigning authority
// and a date-handling rule. Every one of those is data interpreted by typed Go
// operators, exactly as ADR-0003 requires: the document carries no command,
// script, interpreter, expression or program path, a conditional requirement
// is a typed predicate over one named position rather than a formula, and
// nothing in the document changes how a message is parsed.
//
// Where a rule came from is never guessed. Resolve answers, per rule, whether
// the pinned pack declares it, the local profile replaced what the pack
// declares, the local profile declared it where the pack declares nothing, or
// nobody declared it at all. Under readmit-profile-pack/v1 a pack carries
// field labels and nothing else, so a field name is the only rule a pack can
// ever originate; every cardinality, usage, type, terminology, authority and
// date rule is local and is reported as carrying no profile backing. That is
// ADR-0009's rule kept by construction rather than by convention.
//
// This release models and validates the profile document. It evaluates no
// message against one: a local profile is the contract a team wrote down, not
// a verdict about evidence, and no evidence, case bundle or existing contract
// is read, written or changed by this package.
package localprofile

import (
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/profilepack"

	"github.com/bharm16/readmit/internal/strictdoc"
)

// Schema is the contract a local profile declares.
const Schema = "readmit-local-profile/v1"

const (
	// MaxProfileBytes bounds the document a reader decodes. A hand-authored
	// interface contract with local code tables fits comfortably; an
	// unbounded document is refused rather than read.
	MaxProfileBytes = 4 << 20
	// maxSegments bounds the segments one profile constrains.
	maxSegments = 256
	// maxFields bounds the constrained positions of one segment.
	maxFields = 512
	// maxPosition bounds a constrained field position. The parser numbers
	// positions from one; a profile constrains nothing beyond this.
	maxPosition = 999
	// maxSets bounds the terminology sets, authorities and date rules a
	// profile declares, each separately.
	maxSets = 128
	// maxCodes bounds one local code table.
	maxCodes = 1024
	// maxValues bounds the values one conditional requirement tests against.
	maxValues = 256
	// maxRepetitions bounds a declared cardinality. A profile that needs more
	// repetitions than this states unbounded instead.
	maxRepetitions = 9999
	// maxCodeBytes bounds one code, and maxNameBytes one field or code name.
	maxCodeBytes = 64
	maxNameBytes = 128
	// maxProseBytes bounds a description a person reads.
	maxProseBytes = 256
	// maxIdentifierBytes bounds a declared identity and every reference to one.
	maxIdentifierBytes = 64
)

// Usage is what a field's presence is required to be, in the conformance
// vocabulary HL7 profiles already use.
type Usage string

const (
	// UsageRequired: the field is present in every message.
	UsageRequired Usage = "R"
	// UsageRequiredOrEmpty: the sender supports the field and may send it
	// empty; the receiver accepts it either way.
	UsageRequiredOrEmpty Usage = "RE"
	// UsageOptional: nothing is declared about the field's presence.
	UsageOptional Usage = "O"
	// UsageConditional: presence is required exactly when the field's
	// condition holds, and a conditional field declares one.
	UsageConditional Usage = "C"
	// UsageNotSupported: the field is not part of this interface. It carries
	// no other constraint, because a field that must not appear cannot also
	// have a type, a code table, an authority or a date rule.
	UsageNotSupported Usage = "X"
)

// ConditionOperator is the closed set of typed predicates a conditional
// requirement may test. Each reads one named position of one segment; there
// is no expression, no arithmetic and no combination of predicates, because a
// profile a customer imported must not acquire general evaluation.
type ConditionOperator string

const (
	// ConditionPresent: the named position carries a value.
	ConditionPresent ConditionOperator = "present"
	// ConditionAbsent: the named position is omitted, empty or explicitly null.
	ConditionAbsent ConditionOperator = "absent"
	// ConditionValueIn: the named position carries one of the declared values.
	ConditionValueIn ConditionOperator = "value_in"
)

// Binding is how strictly a field is bound to its local terminology set.
type Binding string

const (
	// BindingRequired: a value outside the set does not conform.
	BindingRequired Binding = "required"
	// BindingSuggested: the set records what a site normally sends, and a
	// value outside it is not a conformance statement.
	BindingSuggested Binding = "suggested"
)

// Precision is the finest component a date-carrying field is declared to.
type Precision string

const (
	PrecisionYear     Precision = "year"
	PrecisionMonth    Precision = "month"
	PrecisionDay      Precision = "day"
	PrecisionHour     Precision = "hour"
	PrecisionMinute   Precision = "minute"
	PrecisionSecond   Precision = "second"
	PrecisionFraction Precision = "fraction"
)

// TimeZoneRule is what a date-carrying field declares about its offset.
type TimeZoneRule string

const (
	// TimeZoneRequired: every value carries an offset. A date with no time of
	// day cannot declare this, because an offset states nothing about it.
	TimeZoneRequired TimeZoneRule = "required"
	// TimeZoneOptional: an offset may be present and nothing follows from
	// its absence.
	TimeZoneOptional TimeZoneRule = "optional"
	// TimeZoneForbidden: a value carries no offset, and the local time zone
	// of the sending system is what it means.
	TimeZoneForbidden TimeZoneRule = "forbidden"
)

// DataType names one HL7 v2 data type from the closed set below. A type
// outside it is refused rather than carried, so an editor offering types and
// a reader accepting them cannot disagree.
type DataType string

// dataTypes is the closed set of data types a field may declare. It is the
// finite vocabulary of HL7 v2 field types across the versions a pack covers;
// a composite's own components are not modelled by this contract version.
var dataTypes = []string{
	"AD", "CE", "CF", "CNE", "CP", "CQ", "CWE", "CX", "DLN", "DR", "DT", "DTM",
	"ED", "EI", "EIP", "FN", "FT", "HD", "ID", "IS", "MO", "MSG", "NM", "PL",
	"PT", "RP", "SAD", "SI", "SN", "ST", "TM", "TS", "TX", "VID", "XAD", "XCN",
	"XON", "XPN", "XTN",
}

// codedTypes carry a code drawn from a table, so they are the only types a
// terminology set may be bound to. identifierTypes carry an assigning
// authority, and dateTypes carry a date or a time.
var (
	codedTypes      = []string{"CE", "CF", "CNE", "CWE", "ID", "IS"}
	identifierTypes = []string{"CX", "EI", "HD", "PL", "XCN", "XON"}
	dateTypes       = []string{"DR", "DT", "DTM", "TM", "TS"}
)

// universalIDTypes is HL7 table 0301, the closed set an assigning authority's
// universal identifier is declared in.
var universalIDTypes = []string{
	"DNS", "GUID", "HCD", "HL7", "ISO", "L", "M", "N", "Random", "URI", "UUID",
	"x400", "x500",
}

// DataTypes, UniversalIDTypes, Usages, ConditionOperators, Precisions,
// TimeZoneRules and Bindings return copies of the closed sets, for an editor
// that offers them rather than keeping a second list that can drift.
func DataTypes() []string        { return slices.Clone(dataTypes) }
func UniversalIDTypes() []string { return slices.Clone(universalIDTypes) }

// Usages returns the usage codes a field may declare, in decreasing strength.
func Usages() []Usage {
	return []Usage{UsageRequired, UsageRequiredOrEmpty, UsageConditional, UsageOptional, UsageNotSupported}
}

// ConditionOperators returns the typed predicates a conditional requirement
// may test.
func ConditionOperators() []ConditionOperator {
	return []ConditionOperator{ConditionPresent, ConditionAbsent, ConditionValueIn}
}

// Precisions returns the date precisions a date rule may declare, coarsest
// first.
func Precisions() []Precision {
	return []Precision{PrecisionYear, PrecisionMonth, PrecisionDay, PrecisionHour, PrecisionMinute, PrecisionSecond, PrecisionFraction}
}

// TimeZoneRules returns the offset rules a date rule may declare.
func TimeZoneRules() []TimeZoneRule {
	return []TimeZoneRule{TimeZoneRequired, TimeZoneOptional, TimeZoneForbidden}
}

// Bindings returns the terminology bindings a field may declare.
func Bindings() []Binding { return []Binding{BindingRequired, BindingSuggested} }

// Identity is the local profile's own id and version. It is what #47 versions
// and what a saved test pins, compared byte for byte like every other identity
// in the product: there are no ranges and no "latest".
type Identity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// Base is the one combination this profile constrains and the pack it was
// authored against. The pin is exact: a pack whose content changed is a pack
// whose answers changed, so a different version is a different pack.
type Base struct {
	Pack       profilepack.Identity `json:"pack"`
	HL7Version string               `json:"hl7_version"`
	Family     string               `json:"family"`
}

// Cardinality is how many times a segment or a field may repeat. Max is a
// decimal count or "*" for unbounded, the form HL7 conformance profiles
// already write, so an unbounded maximum is stated rather than encoded as a
// sentinel number.
type Cardinality struct {
	Min int    `json:"min"`
	Max string `json:"max"`
}

// Unbounded is the maximum a cardinality states when a field may repeat
// without a declared limit.
const Unbounded = "*"

// Bounded reports the declared maximum and whether there is one. An unbounded
// maximum returns false, and the count is meaningless then.
func (c Cardinality) Bounded() (int, bool) {
	if c.Max == Unbounded {
		return 0, false
	}
	count, err := strconv.Atoi(c.Max)
	if err != nil {
		return 0, false
	}
	return count, true
}

// Condition is one typed predicate over one named position. It is read by the
// operators this package declares and is never parsed as an expression.
type Condition struct {
	Segment  string            `json:"segment"`
	Position int               `json:"position"`
	Operator ConditionOperator `json:"operator"`
	// Values are the values ConditionValueIn tests against. Every other
	// operator carries none.
	Values []string `json:"values,omitzero"`
}

// Field is one constrained position of one segment. Only the position and the
// usage are required: a profile that has decided nothing else about a field
// says nothing else about it, rather than carrying a default somebody could
// mistake for a decision.
type Field struct {
	Position int    `json:"position"`
	Name     string `json:"name,omitzero"`
	Usage    Usage  `json:"usage"`
	// Condition is required when Usage is UsageConditional and refused
	// otherwise, because a condition on an unconditional field is a rule
	// nothing would ever read.
	Condition   *Condition   `json:"condition,omitzero"`
	Cardinality *Cardinality `json:"cardinality,omitzero"`
	Type        DataType     `json:"type,omitzero"`
	// Terminology, Authority and Date name a set this profile declares. A
	// name no declaration matches is refused, so a rule can never dangle.
	Terminology string `json:"terminology,omitzero"`
	Authority   string `json:"authority,omitzero"`
	Date        string `json:"date,omitzero"`
}

// Segment is one segment this profile constrains, site-defined or standard.
type Segment struct {
	ID          string       `json:"id"`
	Description string       `json:"description,omitzero"`
	Cardinality *Cardinality `json:"cardinality,omitzero"`
	Fields      []Field      `json:"fields"`
}

// SiteDefined reports whether this is a Z-segment: one whose identifier begins
// with Z, which HL7 reserves for local use and which therefore no published
// metadata describes.
func (s Segment) SiteDefined() bool { return strings.HasPrefix(s.ID, "Z") }

// Code is one entry of a local code table.
type Code struct {
	Code    string `json:"code"`
	Display string `json:"display,omitzero"`
}

// TerminologySet is one local code table a coded field is bound to.
type TerminologySet struct {
	ID          string  `json:"id"`
	Description string  `json:"description,omitzero"`
	Binding     Binding `json:"binding"`
	Codes       []Code  `json:"codes"`
}

// Authority is one assigning authority: the namespace rule an identifier
// field is issued under. At least one of Namespace and UniversalID is
// declared, and a universal identifier is declared with the type it is in.
type Authority struct {
	ID              string `json:"id"`
	Description     string `json:"description,omitzero"`
	Namespace       string `json:"namespace,omitzero"`
	UniversalID     string `json:"universal_id,omitzero"`
	UniversalIDType string `json:"universal_id_type,omitzero"`
}

// DateHandling is one date rule: how precise a date-carrying field is and
// what it declares about its offset.
type DateHandling struct {
	ID          string       `json:"id"`
	Description string       `json:"description,omitzero"`
	Precision   Precision    `json:"precision"`
	TimeZone    TimeZoneRule `json:"timezone"`
}

// Profile is one local interface contract exactly as written.
type Profile struct {
	Schema   string   `json:"schema"`
	Identity Identity `json:"profile"`
	Base     Base     `json:"base"`
	// Terminology, Authorities and Dates are the sets fields name. They are
	// optional: a profile that binds nothing declares none.
	Terminology []TerminologySet `json:"terminology,omitzero"`
	Authorities []Authority      `json:"authorities,omitzero"`
	Dates       []DateHandling   `json:"dates,omitzero"`
	Segments    []Segment        `json:"segments"`
}

// UnmarshalJSON reads one identity exactly as written: presence first, then
// the same bytes again rejecting unknown members.
func (i *Identity) UnmarshalJSON(data []byte) error {
	var required struct {
		ID      *string `json:"id"`
		Version *string `json:"version"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.ID == nil || required.Version == nil {
		return errors.New("a local profile identity requires id and version")
	}
	type identity Identity
	var decoded identity
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a local profile identity declares no member beyond id and version")
	}
	*i = Identity(decoded)
	return nil
}

// UnmarshalJSON reads the base exactly as written.
func (b *Base) UnmarshalJSON(data []byte) error {
	var required struct {
		Pack       *profilepack.Identity `json:"pack"`
		HL7Version *string               `json:"hl7_version"`
		Family     *string               `json:"family"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Pack == nil || required.HL7Version == nil || required.Family == nil {
		return errors.New("a local profile base requires pack, hl7_version and family")
	}
	type base Base
	var decoded base
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a local profile base declares no member beyond pack, hl7_version and family")
	}
	*b = Base(decoded)
	return nil
}

// UnmarshalJSON reads one cardinality exactly as written. Both bounds are
// required: an omitted minimum would read as zero, and zero is a decision.
func (c *Cardinality) UnmarshalJSON(data []byte) error {
	var required struct {
		Min *int    `json:"min"`
		Max *string `json:"max"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Min == nil || required.Max == nil {
		return errors.New("a cardinality requires min and max")
	}
	type cardinality Cardinality
	var decoded cardinality
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a cardinality declares no member beyond min and max")
	}
	*c = Cardinality(decoded)
	return nil
}

// UnmarshalJSON reads one conditional requirement exactly as written.
func (c *Condition) UnmarshalJSON(data []byte) error {
	var required struct {
		Segment  *string            `json:"segment"`
		Position *int               `json:"position"`
		Operator *ConditionOperator `json:"operator"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Segment == nil || required.Position == nil || required.Operator == nil {
		return errors.New("a conditional requirement requires segment, position and operator")
	}
	type condition Condition
	var decoded condition
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a conditional requirement declares no member beyond segment, position, operator and values")
	}
	*c = Condition(decoded)
	return nil
}

// UnmarshalJSON reads one field rule exactly as written.
func (f *Field) UnmarshalJSON(data []byte) error {
	var required struct {
		Position *int   `json:"position"`
		Usage    *Usage `json:"usage"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Position == nil || required.Usage == nil {
		return errors.New("a field rule requires position and usage")
	}
	type field Field
	var decoded field
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a field rule declares no member beyond position, name, usage, condition, cardinality, type, terminology, authority and date")
	}
	*f = Field(decoded)
	return nil
}

// UnmarshalJSON reads one segment rule exactly as written.
func (s *Segment) UnmarshalJSON(data []byte) error {
	var required struct {
		ID     *string  `json:"id"`
		Fields *[]Field `json:"fields"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.ID == nil || required.Fields == nil {
		return errors.New("a segment rule requires id and fields")
	}
	type segment Segment
	var decoded segment
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a segment rule declares no member beyond id, description, cardinality and fields")
	}
	*s = Segment(decoded)
	return nil
}

// UnmarshalJSON reads one code exactly as written.
func (c *Code) UnmarshalJSON(data []byte) error {
	var required struct {
		Code *string `json:"code"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Code == nil {
		return errors.New("a code requires code")
	}
	type code Code
	var decoded code
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a code declares no member beyond code and display")
	}
	*c = Code(decoded)
	return nil
}

// UnmarshalJSON reads one terminology set exactly as written.
func (s *TerminologySet) UnmarshalJSON(data []byte) error {
	var required struct {
		ID      *string  `json:"id"`
		Binding *Binding `json:"binding"`
		Codes   *[]Code  `json:"codes"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.ID == nil || required.Binding == nil || required.Codes == nil {
		return errors.New("a terminology set requires id, binding and codes")
	}
	type set TerminologySet
	var decoded set
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a terminology set declares no member beyond id, description, binding and codes")
	}
	*s = TerminologySet(decoded)
	return nil
}

// UnmarshalJSON reads one assigning authority exactly as written.
func (a *Authority) UnmarshalJSON(data []byte) error {
	var required struct {
		ID *string `json:"id"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.ID == nil {
		return errors.New("an assigning authority requires id")
	}
	type authority Authority
	var decoded authority
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("an assigning authority declares no member beyond id, description, namespace, universal_id and universal_id_type")
	}
	*a = Authority(decoded)
	return nil
}

// UnmarshalJSON reads one date rule exactly as written.
func (d *DateHandling) UnmarshalJSON(data []byte) error {
	var required struct {
		ID        *string       `json:"id"`
		Precision *Precision    `json:"precision"`
		TimeZone  *TimeZoneRule `json:"timezone"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.ID == nil || required.Precision == nil || required.TimeZone == nil {
		return errors.New("a date rule requires id, precision and timezone")
	}
	type handling DateHandling
	var decoded handling
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a date rule declares no member beyond id, description, precision and timezone")
	}
	*d = DateHandling(decoded)
	return nil
}

// Decode reads one local profile exactly as written and refuses every document
// it could not stand behind: unknown members anywhere, a contract this release
// does not read, a combination outside the sets a pack may cover, a rule that
// contradicts the usage it sits under, a reference to a set the profile does
// not declare, and anything declared twice.
func Decode(data []byte) (Profile, error) {
	var profile Profile
	if err := profileDocument.Decode(data, &profile); err != nil {
		return Profile{}, err
	}
	if err := profile.Validate(); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// profileDocument states how this contract is read; strictdoc owns the reading.
var profileDocument = strictdoc.Document{
	MaxBytes:    MaxProfileBytes,
	Schema:      Schema,
	Required:    []string{"profile", "base", "segments"},
	Invalid:     "invalid local profile JSON",
	TooLarge:    "a local profile exceeds its 4 MiB size limit",
	MustDeclare: "a local profile must declare " + Schema,
	Requires:    "a local profile requires profile, base and segments",
}

// ValidateIdentity holds an id and a version to the rules every identity in
// this product follows. The what argument names whose identity it is, so the
// refusal says which of the two a document carries wrongly. A document that pins a local
// profile by identity rather than carrying one — a sealed version, or the
// references a saved test makes — reads the rule here instead of restating it,
// so a pin and the profile it names cannot differ in what they allow.
func ValidateIdentity(what string, identity Identity) error {
	return validateIdentity(what, identity)
}

// Validate is the profile checked against itself: every rule the contract
// requires, with no pack involved. Decode runs it, and Canonical runs it, so a
// profile that was assembled in Go is held to exactly what a profile that was
// read from a file is held to.
func (p Profile) Validate() error {
	if p.Schema != Schema {
		return errors.New("a local profile must declare " + Schema)
	}
	if err := validateIdentity("local profile", p.Identity); err != nil {
		return err
	}
	if err := validateBase(p.Base); err != nil {
		return err
	}
	sets, err := validateDeclarations(p)
	if err != nil {
		return err
	}
	if len(p.Segments) == 0 {
		return errors.New("a local profile constrains at least one segment")
	}
	if len(p.Segments) > maxSegments {
		return errors.New("a local profile constrains at most 256 segments")
	}
	if id, twice := duplicate(p.Segments, func(s Segment) string { return s.ID }); twice {
		return errors.New("a local profile constrains the segment " + id + " twice")
	}
	for _, segment := range p.Segments {
		if !segmentID(segment.ID) {
			return errors.New("a constrained segment id is three uppercase letters or digits, beginning with a letter")
		}
		if err := validateSegment(segment, sets); err != nil {
			return err
		}
	}
	return nil
}

// declarations are the sets a field may name, by kind.
type declarations struct {
	terminology map[string]TerminologySet
	authorities map[string]Authority
	dates       map[string]DateHandling
}

// index is the one place a profile's declared sets become maps, so validating
// a reference and resolving one cannot disagree about what was declared.
func index(p Profile) declarations {
	declared := declarations{
		terminology: make(map[string]TerminologySet, len(p.Terminology)),
		authorities: make(map[string]Authority, len(p.Authorities)),
		dates:       make(map[string]DateHandling, len(p.Dates)),
	}
	for _, set := range p.Terminology {
		declared.terminology[set.ID] = set
	}
	for _, authority := range p.Authorities {
		declared.authorities[authority.ID] = authority
	}
	for _, date := range p.Dates {
		declared.dates[date.ID] = date
	}
	return declared
}

func validateDeclarations(p Profile) (declarations, error) {
	if len(p.Terminology) > maxSets || len(p.Authorities) > maxSets || len(p.Dates) > maxSets {
		return declarations{}, errors.New("a local profile declares at most 128 terminology sets, 128 authorities and 128 date rules")
	}
	for _, set := range p.Terminology {
		if err := validateTerminology(set); err != nil {
			return declarations{}, err
		}
	}
	for _, authority := range p.Authorities {
		if err := validateAuthority(authority); err != nil {
			return declarations{}, err
		}
	}
	for _, date := range p.Dates {
		if err := validateDate(date); err != nil {
			return declarations{}, err
		}
	}
	if id, twice := duplicate(p.Terminology, func(s TerminologySet) string { return s.ID }); twice {
		return declarations{}, errors.New("a local profile declares the terminology set " + id + " twice")
	}
	if id, twice := duplicate(p.Authorities, func(a Authority) string { return a.ID }); twice {
		return declarations{}, errors.New("a local profile declares the authority " + id + " twice")
	}
	if id, twice := duplicate(p.Dates, func(d DateHandling) string { return d.ID }); twice {
		return declarations{}, errors.New("a local profile declares the date rule " + id + " twice")
	}
	return index(p), nil
}

// duplicate reports the first key a list declares twice, so a refusal names
// what was declared twice rather than only that something was.
func duplicate[T any, K comparable](list []T, key func(T) K) (K, bool) {
	seen := make(map[K]bool, len(list))
	for _, entry := range list {
		if seen[key(entry)] {
			return key(entry), true
		}
		seen[key(entry)] = true
	}
	var none K
	return none, false
}

// validateIdentity holds an id and a version to the rules every identity in
// this product follows. what names whose identity it is, so the refusal says
// which of the two a document carries wrongly.
func validateIdentity(what string, identity Identity) error {
	if identity.ID == "" || len(identity.ID) > maxIdentifierBytes || identity.ID[0] < 'a' || identity.ID[0] > 'z' {
		return errors.New("a " + what + " id begins with a lowercase letter and is at most 64 bytes")
	}
	for _, r := range identity.ID {
		if r != '-' && (r < '0' || r > '9') && (r < 'a' || r > 'z') {
			return errors.New("a " + what + " id holds lowercase letters, digits and '-' only")
		}
	}
	if identity.Version == "" || len(identity.Version) > 32 || identity.Version[0] < '0' || identity.Version[0] > '9' {
		return errors.New("a " + what + " version begins with a digit and is at most 32 bytes")
	}
	for _, r := range identity.Version {
		if r != '-' && r != '.' && (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return errors.New("a " + what + " version holds letters, digits, '.' and '-' only")
		}
	}
	return nil
}

func validateBase(base Base) error {
	if !slices.Contains(profilepack.HL7Versions(), base.HL7Version) {
		return errors.New("a local profile names one of the HL7 versions " + strings.Join(profilepack.HL7Versions(), ", "))
	}
	if !slices.Contains(profilepack.Families(), base.Family) {
		return errors.New("a local profile names one of the message families " + strings.Join(profilepack.Families(), ", "))
	}
	// The pin follows the pack contract's own identity rules, so a profile
	// cannot pin an id or version no pack could ever carry.
	return validateIdentity("pinned pack", Identity(base.Pack))
}

func validateSegment(segment Segment, sets declarations) error {
	if !prose(segment.Description) {
		return errors.New("a segment description is one line of at most 256 readable bytes")
	}
	if segment.Cardinality != nil {
		if err := validateCardinality(*segment.Cardinality); err != nil {
			return err
		}
	}
	if len(segment.Fields) == 0 || len(segment.Fields) > maxFields {
		return errors.New("a constrained segment carries between 1 and 512 field rules")
	}
	if position, twice := duplicate(segment.Fields, func(f Field) int { return f.Position }); twice {
		return errors.New("a local profile constrains " + segment.ID + "-" + strconv.Itoa(position) + " twice")
	}
	for _, field := range segment.Fields {
		if field.Position < 1 || field.Position > maxPosition {
			return errors.New("a constrained field position is between 1 and 999")
		}
		if err := validateField(segment, field, sets); err != nil {
			return err
		}
	}
	return nil
}

func validateField(segment Segment, field Field, sets declarations) error {
	if field.Name != "" && (len(field.Name) > maxNameBytes || !readableLine(field.Name)) {
		return errors.New("a local field name is one line of at most 128 readable bytes")
	}
	if !slices.Contains(Usages(), field.Usage) {
		return errors.New("a field usage is one of R, RE, C, O or X")
	}
	where := segment.ID + "-" + strconv.Itoa(field.Position)
	if field.Usage == UsageNotSupported {
		if field.Condition != nil || field.Cardinality != nil || field.Type != "" ||
			field.Terminology != "" || field.Authority != "" || field.Date != "" {
			return errors.New(where + " is not supported by this interface, so it carries no other constraint")
		}
		return nil
	}
	if err := validateCondition(segment.ID, where, field); err != nil {
		return err
	}
	if field.Cardinality != nil {
		if err := validateCardinality(*field.Cardinality); err != nil {
			return err
		}
		minimum := field.Cardinality.Min
		if field.Usage == UsageRequired && minimum < 1 {
			return errors.New(where + " is required, so its cardinality begins at one or more")
		}
		if field.Usage != UsageRequired && minimum > 0 {
			return errors.New(where + " declares a cardinality of one or more without being required")
		}
	}
	return validateBindings(where, field, sets)
}

func validateCondition(segment, where string, field Field) error {
	if field.Usage == UsageConditional && field.Condition == nil {
		return errors.New(where + " is conditional, so it declares the condition its presence depends on")
	}
	if field.Usage != UsageConditional && field.Condition != nil {
		return errors.New(where + " declares a condition without being conditional")
	}
	if field.Condition == nil {
		return nil
	}
	condition := *field.Condition
	if !segmentID(condition.Segment) {
		return errors.New("a condition names a segment of three uppercase letters or digits, beginning with a letter")
	}
	if condition.Position < 1 || condition.Position > maxPosition {
		return errors.New("a condition names a position between 1 and 999")
	}
	if condition.Segment == segment && condition.Position == field.Position {
		return errors.New(where + " declares a condition on itself")
	}
	if !slices.Contains(ConditionOperators(), condition.Operator) {
		return errors.New("a condition operator is one of present, absent or value_in")
	}
	if condition.Operator == ConditionValueIn {
		if len(condition.Values) == 0 || len(condition.Values) > maxValues {
			return errors.New("a value_in condition tests between 1 and 256 values")
		}
		seen := make(map[string]bool, len(condition.Values))
		for _, value := range condition.Values {
			if value == "" || len(value) > maxCodeBytes || !readableLine(value) {
				return errors.New("a condition value is one nonempty line of at most 64 readable bytes")
			}
			if seen[value] {
				return errors.New("a condition tests the same value twice")
			}
			seen[value] = true
		}
		return nil
	}
	if len(condition.Values) != 0 {
		return errors.New("a present or absent condition tests no values")
	}
	return nil
}

// validateBindings holds the three references a field may make to the sets the
// profile declares. Each one needs a data type that can carry it, so a code
// table bound to a number, an assigning authority on a free-text field and a
// date rule on something that is not a date are each refused rather than kept
// as a rule nothing could ever apply.
func validateBindings(where string, field Field, sets declarations) error {
	if field.Type != "" && !slices.Contains(dataTypes, string(field.Type)) {
		return errors.New("a field type is one of the HL7 data types " + strings.Join(dataTypes, ", "))
	}
	for _, binding := range []struct {
		reference, kind string
		types           []string
		declared        bool
	}{
		{field.Terminology, "terminology set", codedTypes, has(sets.terminology, field.Terminology)},
		{field.Authority, "authority", identifierTypes, has(sets.authorities, field.Authority)},
		{field.Date, "date rule", dateTypes, has(sets.dates, field.Date)},
	} {
		if binding.reference == "" {
			continue
		}
		if !binding.declared {
			return errors.New(where + " names the " + binding.kind + " " + binding.reference + ", which this profile does not declare")
		}
		if field.Type == "" {
			return errors.New(where + " binds the " + binding.kind + " without declaring the data type that would carry it")
		}
		if !slices.Contains(binding.types, string(field.Type)) {
			return errors.New(where + " binds the " + binding.kind + " to a field whose type is not one of " + strings.Join(binding.types, ", "))
		}
	}
	return nil
}

func has[T any](declared map[string]T, id string) bool {
	_, ok := declared[id]
	return ok
}

func validateCardinality(cardinality Cardinality) error {
	if cardinality.Min < 0 || cardinality.Min > maxRepetitions {
		return errors.New("a cardinality minimum is between 0 and 9999")
	}
	if cardinality.Max == Unbounded {
		return nil
	}
	maximum, bounded := cardinality.Bounded()
	if !bounded || cardinality.Max != strconv.Itoa(maximum) {
		return errors.New(`a cardinality maximum is a decimal count or "*"`)
	}
	if maximum < 1 || maximum > maxRepetitions {
		return errors.New("a bounded cardinality maximum is between 1 and 9999")
	}
	if maximum < cardinality.Min {
		return errors.New("a cardinality maximum is not below its minimum")
	}
	return nil
}

func validateTerminology(set TerminologySet) error {
	if !identifier(set.ID) {
		return errors.New("a terminology set id begins with a lowercase letter, holds lowercase letters, digits and '-', and is at most 64 bytes")
	}
	if !prose(set.Description) {
		return errors.New("a terminology set description is one line of at most 256 readable bytes")
	}
	if !slices.Contains(Bindings(), set.Binding) {
		return errors.New("a terminology binding is required or suggested")
	}
	if len(set.Codes) == 0 || len(set.Codes) > maxCodes {
		return errors.New("a terminology set carries between 1 and 1024 codes")
	}
	seen := make(map[string]bool, len(set.Codes))
	for _, code := range set.Codes {
		if code.Code == "" || len(code.Code) > maxCodeBytes || !readableLine(code.Code) {
			return errors.New("a code is one nonempty line of at most 64 readable bytes")
		}
		if code.Display != "" && (len(code.Display) > maxNameBytes || !readableLine(code.Display)) {
			return errors.New("a code display name is one line of at most 128 readable bytes")
		}
		if seen[code.Code] {
			return errors.New("a terminology set carries the code " + code.Code + " twice")
		}
		seen[code.Code] = true
	}
	return nil
}

func validateAuthority(authority Authority) error {
	if !identifier(authority.ID) {
		return errors.New("an authority id begins with a lowercase letter, holds lowercase letters, digits and '-', and is at most 64 bytes")
	}
	if !prose(authority.Description) {
		return errors.New("an authority description is one line of at most 256 readable bytes")
	}
	for _, component := range []string{authority.Namespace, authority.UniversalID} {
		if component != "" && (len(component) > maxNameBytes || !readableLine(component)) {
			return errors.New("an authority namespace and universal id are each one line of at most 128 readable bytes")
		}
	}
	if authority.Namespace == "" && authority.UniversalID == "" {
		return errors.New("an authority declares a namespace, a universal id, or both")
	}
	if (authority.UniversalID == "") != (authority.UniversalIDType == "") {
		return errors.New("an authority declares a universal id together with the type it is in")
	}
	if authority.UniversalIDType != "" && !slices.Contains(universalIDTypes, authority.UniversalIDType) {
		return errors.New("a universal id type is one of " + strings.Join(universalIDTypes, ", "))
	}
	return nil
}

func validateDate(date DateHandling) error {
	if !identifier(date.ID) {
		return errors.New("a date rule id begins with a lowercase letter, holds lowercase letters, digits and '-', and is at most 64 bytes")
	}
	if !prose(date.Description) {
		return errors.New("a date rule description is one line of at most 256 readable bytes")
	}
	if !slices.Contains(Precisions(), date.Precision) {
		return errors.New("a date precision is one of " + precisionNames())
	}
	if !slices.Contains(TimeZoneRules(), date.TimeZone) {
		return errors.New("a date time zone rule is required, optional or forbidden")
	}
	// An offset states nothing about a value that carries no time of day, so
	// a date that coarse cannot require one.
	if date.TimeZone == TimeZoneRequired && slices.Index(Precisions(), date.Precision) < slices.Index(Precisions(), PrecisionHour) {
		return errors.New("a date rule with no time of day cannot require a time zone offset")
	}
	return nil
}

func precisionNames() string {
	names := make([]string, 0, len(Precisions()))
	for _, precision := range Precisions() {
		names = append(names, string(precision))
	}
	return strings.Join(names, ", ")
}

// segmentID is the parser's own rule for a segment identifier, so a profile
// cannot constrain a segment the parser would never produce.
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

// identifier is the rule every declared set id and every reference to one
// follows, so a reference and a declaration cannot differ in what they allow.
func identifier(id string) bool {
	if id == "" || len(id) > maxIdentifierBytes || id[0] < 'a' || id[0] > 'z' {
		return false
	}
	for _, r := range id {
		if r != '-' && (r < '0' || r > '9') && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func prose(text string) bool {
	return len(text) <= maxProseBytes && readableLine(text)
}

// readableLine bounds what a member of a profile may say to the person reading
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
