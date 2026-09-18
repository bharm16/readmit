package importer

import (
	"encoding/json/v2"
	"errors"
	"strconv"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

// The three contract versions a mapping recipe owns. A new member of any of
// them is a new version string with a reader for every older one, never an
// added member and never an in-place migration.
const (
	RecipeSchema         = "readmit-mapping-recipe/v1"
	MappingPreviewSchema = "readmit-mapping-preview/v1"
	MappingReceiptSchema = "readmit-mapping-receipt/v1"
)

// Envelope is the container format one recipe reads. It is declared, never
// detected: which of these a file is cannot be read from its name.
type Envelope string

const (
	CSVEnvelope  Envelope = "csv"
	JSONEnvelope Envelope = "json"
	XMLEnvelope  Envelope = "xml"
	TextEnvelope Envelope = "text"
)

// Separator ends one envelope record. A lone carriage return is deliberately
// absent: an HL7 payload carried inside a record uses that byte as its own
// segment terminator, so a record separator that collides with it could not
// divide the envelope without cutting the evidence.
type Separator string

const (
	LFSeparator   Separator = "lf"
	CRLFSeparator Separator = "crlf"
)

// Header states whether a CSV member's first record names its columns. There
// is no automatic value: a first row that happens to look like names is not a
// header until the recipe says it is.
type Header string

const (
	HeaderPresent Header = "present"
	HeaderAbsent  Header = "absent"
)

// PayloadOperator turns one located envelope value into payload bytes.
// verbatim keeps the value's bytes after the envelope's own escaping is undone;
// base64 decodes strict padded standard base64. Neither repairs anything.
type PayloadOperator string

const (
	VerbatimPayload PayloadOperator = "verbatim"
	Base64Payload   PayloadOperator = "base64"
)

// TimeOperator reads one located envelope value as an observed time. Every
// operator but UnknownTime requires the value to carry its own UTC offset:
// supplying one for a corpus would infer provenance rather than read it.
type TimeOperator string

const (
	UnknownTime      TimeOperator = "unknown"
	RFC3339Time      TimeOperator = "rfc3339"
	UnixSecondsTime  TimeOperator = "unix-seconds"
	UnixMilliseconds TimeOperator = "unix-milliseconds"
	HL7DTMTime       TimeOperator = "hl7-dtm"
)

// LabelOperator supplies the declared source or channel of a record: an
// explicit statement that the recipe does not declare one, one constant the
// operator declared for every record, or the value at a declared location.
type LabelOperator string

const (
	UnknownLabel  LabelOperator = "unknown"
	DeclaredLabel LabelOperator = "declared"
	FieldLabel    LabelOperator = "field"
)

// DirectionOperator supplies the declared direction of a record. There is no
// unknown operator because "unknown" is itself a direction the operator may
// declare, and no automatic operator: direction is never read from a file name,
// a column that merely looks like one, or the shape of a message.
type DirectionOperator string

const (
	DeclaredDirection DirectionOperator = "declared"
	FieldDirection    DirectionOperator = "field"
)

// The bounds one recipe is held to. They sit inside the import bounds rather
// than beside them: a member still divides into at most the sources one case
// bundle holds, and a recipe document is bounded exactly like a plan.
const (
	MaxRecipeBytes     = MaxPlanBytes
	MaxEnvelopeFields  = 64
	MaxEnvelopeRecords = bundle.MaxSources
	MaxLocatorElements = 8
	MaxDirectionValues = 16
	maxLabelBytes      = 128
	maxRecipeNameBytes = 64
)

// ErrUnsupportedRecipe reports a recipe written under a contract version this
// release does not read. It is distinct from a recipe this release reads and
// rejects, so a caller can say which one it was handed.
var ErrUnsupportedRecipe = errors.New("unsupported mapping recipe version")

// Locator names one value inside one envelope record. Its elements are member
// names for a JSON envelope and element names for an XML envelope. A CSV or
// text envelope holds one flat record, so its locator is exactly one element:
// the column name when a header is declared, and the one-based decimal column
// index when it is not. A locator is data naming a position; it is never an
// expression, a query, or a pattern.
type Locator []string

// PayloadMapping names where a record's message bytes are and how they are
// framed once they have been read out of the envelope.
type PayloadMapping struct {
	Operator   PayloadOperator `json:"operator"`
	Locator    Locator         `json:"locator"`
	Framing    Framing         `json:"framing"`
	Terminator hl7.Terminator  `json:"terminator"`
}

// TimeMapping names the observed time of a record. Locator is declared exactly
// when Operator is not UnknownTime.
type TimeMapping struct {
	Operator TimeOperator `json:"operator"`
	Locator  Locator      `json:"locator,omitzero"`
}

// LabelMapping names the source or channel of a record. Declared is stated
// exactly with DeclaredLabel, and Locator exactly with FieldLabel.
type LabelMapping struct {
	Operator LabelOperator `json:"operator"`
	Declared string        `json:"declared,omitzero"`
	Locator  Locator       `json:"locator,omitzero"`
}

// DirectionValue is one entry of the declared direction table: the exact value
// the envelope holds, and the direction the operator declared it means.
type DirectionValue struct {
	Envelope string           `json:"envelope"`
	Mapped   bundle.Direction `json:"mapped"`
}

// DirectionMapping names the direction of a record. Declared is stated exactly
// with DeclaredDirection; Locator and an exhaustive Values table exactly with
// FieldDirection. A record whose value is outside the table is not guessed at:
// it is retained as quarantined evidence with its direction explicitly unknown,
// so the operator can add the entry and re-run.
type DirectionMapping struct {
	Operator DirectionOperator `json:"operator"`
	Declared bundle.Direction  `json:"declared,omitzero"`
	Locator  Locator           `json:"locator,omitzero"`
	Values   []DirectionValue  `json:"values,omitzero"`
}

// CSVDialect is the declared shape of a CSV member. Every part of it is stated,
// including the field count, so a header row that disagrees with the
// declaration is a refusal rather than a quietly different reading.
type CSVDialect struct {
	Delimiter       string    `json:"delimiter"`
	RecordSeparator Separator `json:"record_separator"`
	Header          Header    `json:"header"`
	Fields          int       `json:"fields"`
}

// TextDialect is the declared shape of a timestamped text log. The last
// declared field holds the remainder of the record, so a payload carrying the
// field separator is read whole rather than cut at its first occurrence.
type TextDialect struct {
	FieldSeparator  string    `json:"field_separator"`
	RecordSeparator Separator `json:"record_separator"`
	Fields          int       `json:"fields"`
}

// DocumentDialect is the declared shape of a JSON or XML member: the path to
// the records. A JSON path names object members from the document's own root,
// and an empty one says the root is itself the array of records. An XML path
// names elements starting with the document element, so it always has at least
// that one name. Neither is assumed: a document holding an array somewhere is
// not a corpus until the recipe says which array it is.
type DocumentDialect struct {
	RecordPath []string `json:"record_path"`
}

// Recipe is the complete, reusable mapping configuration: which envelope a
// member is, which entries are members, and the typed operator that supplies
// each of the five declarations an import needs. It is recorded verbatim in
// the preview and the receipt, together with the identity of its own bytes, so
// the mapping an import ran under stays with the evidence and is comparable.
//
// A recipe is data that names operators. It holds no expression, no pattern
// and no hook, and an envelope this release cannot map is met by adding a
// typed operator in Go with tests, never by making a recipe executable.
type Recipe struct {
	Schema   string   `json:"schema"`
	Name     string   `json:"name"`
	Revision int      `json:"revision"`
	Envelope Envelope `json:"envelope"`
	// Encoding is the declared character encoding of the envelope member. It
	// is recorded, and checked only where bytes can contradict it, exactly as
	// an import plan's is; it never converts evidence.
	Encoding Encoding `json:"encoding"`
	// Members are the lowercase file-name suffixes a folder or archive entry
	// must end with to be mapped, read exactly as an import plan reads them.
	Members []string `json:"members"`
	// Exactly one dialect is declared, and it is the one Envelope names.
	CSV  *CSVDialect      `json:"csv,omitzero"`
	Text *TextDialect     `json:"text,omitzero"`
	JSON *DocumentDialect `json:"json,omitzero"`
	XML  *DocumentDialect `json:"xml,omitzero"`

	Payload    PayloadMapping   `json:"payload"`
	ObservedAt TimeMapping      `json:"observed_at"`
	Source     LabelMapping     `json:"source"`
	Direction  DirectionMapping `json:"direction"`
	Channel    LabelMapping     `json:"channel"`
}

// UnmarshalJSON requires the declarations whose absence a zero value would
// hide, then re-decodes with unknown members refused so a misspelled
// declaration is an error rather than a declaration that quietly did nothing.
func (r *Recipe) UnmarshalJSON(data []byte) error {
	var required struct {
		Schema   *string   `json:"schema"`
		Name     *string   `json:"name"`
		Revision *int      `json:"revision"`
		Envelope *string   `json:"envelope"`
		Encoding *string   `json:"encoding"`
		Members  *[]string `json:"members"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Schema == nil {
		return errors.New("a mapping recipe declares its contract version")
	}
	if *required.Schema != RecipeSchema {
		return ErrUnsupportedRecipe
	}
	if required.Name == nil || required.Revision == nil || required.Envelope == nil || required.Encoding == nil || required.Members == nil {
		return errors.New("a mapping recipe declares a name, a revision, an envelope, an encoding, and an explicit member list")
	}
	type plainRecipe Recipe
	var value plainRecipe
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid mapping recipe")
	}
	*r = Recipe(value)
	return nil
}

// DecodeRecipe reads a recipe document. Unknown members and unknown versions
// are errors; there is no migration and no repair. Diagnostics name the
// declaration at fault and never repeat the value that failed.
func DecodeRecipe(data []byte) (Recipe, error) {
	if len(data) > MaxRecipeBytes {
		return Recipe{}, errors.New("mapping recipe exceeds its size limit")
	}
	var recipe Recipe
	if err := json.Unmarshal(data, &recipe); err != nil {
		if errors.Is(err, ErrUnsupportedRecipe) {
			return Recipe{}, ErrUnsupportedRecipe
		}
		return Recipe{}, errors.New("invalid mapping recipe")
	}
	if err := recipe.Validate(); err != nil {
		return Recipe{}, err
	}
	return recipe, nil
}

// Identity is the SHA-256 of the recipe's deterministic encoding. Two recipes
// that declare the same mapping have the same identity however their documents
// were spaced or ordered, so a receipt names one comparable recipe version.
//
// It identifies the declarations it is given and does not re-check them, so a
// caller passes a recipe DecodeRecipe returned or one it validated itself. The
// two document constructors that publish an identity validate before asking for
// one, so nothing this package writes can name a recipe that could not run.
func (r Recipe) Identity() (string, error) {
	data, err := json.Marshal(r, json.Deterministic(true))
	if err != nil {
		return "", errors.New("cannot encode the mapping recipe")
	}
	return digest(data), nil
}

// Validate reports the first reason a recipe cannot be used.
func (r Recipe) Validate() error {
	if r.Schema != RecipeSchema {
		return ErrUnsupportedRecipe
	}
	if !label(r.Name) || len(r.Name) > maxRecipeNameBytes {
		return errors.New("a mapping recipe declares a name of at most " + strconv.Itoa(maxRecipeNameBytes) + " printable characters")
	}
	if r.Revision < 1 {
		return errors.New("a mapping recipe declares a revision of at least 1")
	}
	if err := r.validateDialect(); err != nil {
		return err
	}
	if err := r.validateEncoding(); err != nil {
		return err
	}
	if err := validateMembers(r.Members, "a mapping recipe"); err != nil {
		return err
	}
	if err := r.validatePayload(); err != nil {
		return err
	}
	if err := r.validateObservedAt(); err != nil {
		return err
	}
	if err := r.validateLabel(r.Source, "source"); err != nil {
		return err
	}
	if err := r.validateLabel(r.Channel, "channel"); err != nil {
		return err
	}
	return r.validateDirection()
}

// validateDialect requires exactly the dialect the envelope names, so a recipe
// cannot carry a second reading of the same member alongside the one it uses.
func (r Recipe) validateDialect() error {
	declared := 0
	for _, present := range []bool{r.CSV != nil, r.Text != nil, r.JSON != nil, r.XML != nil} {
		if present {
			declared++
		}
	}
	if declared != 1 {
		return errors.New("a mapping recipe declares exactly one of csv, text, json, and xml")
	}
	switch r.Envelope {
	case CSVEnvelope:
		if r.CSV == nil {
			return errors.New("a csv envelope declares a csv dialect")
		}
		if err := separatorByte(r.CSV.Delimiter, r.CSV.RecordSeparator); err != nil {
			return errors.New("csv delimiter: " + err.Error())
		}
		if r.CSV.Delimiter == `"` {
			return errors.New("csv delimiter must not be the quote character")
		}
		if r.CSV.Header != HeaderPresent && r.CSV.Header != HeaderAbsent {
			return errors.New("a csv dialect declares a header of present or absent")
		}
		return fieldCount(r.CSV.Fields)
	case TextEnvelope:
		if r.Text == nil {
			return errors.New("a text envelope declares a text dialect")
		}
		if err := separatorByte(r.Text.FieldSeparator, r.Text.RecordSeparator); err != nil {
			return errors.New("text field separator: " + err.Error())
		}
		return fieldCount(r.Text.Fields)
	case JSONEnvelope, XMLEnvelope:
		path := r.JSON
		if r.Envelope == XMLEnvelope {
			path = r.XML
		}
		if path == nil {
			return errors.New("a json or xml envelope declares the dialect of its own name")
		}
		if path.RecordPath == nil {
			return errors.New("a json or xml dialect declares an explicit record path")
		}
		if r.Envelope == XMLEnvelope && len(path.RecordPath) == 0 {
			return errors.New("an xml record path names the document element and the elements below it")
		}
		if len(path.RecordPath) > MaxLocatorElements {
			return errors.New("a record path names at most " + strconv.Itoa(MaxLocatorElements) + " elements")
		}
		for _, element := range path.RecordPath {
			if !label(element) {
				return errors.New("a record path element is one bounded printable name")
			}
		}
		return nil
	default:
		return errors.New("envelope must be declared as csv, json, xml, or text")
	}
}

// validateEncoding holds a JSON or XML envelope to an encoding those formats
// can actually carry here. Both readers refuse bytes that are not valid UTF-8,
// so declaring an encoding they would reject would be a declaration that could
// never hold.
func (r Recipe) validateEncoding() error {
	switch r.Encoding {
	case UTF8, USASCII:
		return nil
	case Latin1, UnknownEncoding:
		if r.Envelope == JSONEnvelope || r.Envelope == XMLEnvelope {
			return errors.New("a json or xml envelope declares an encoding of utf-8 or us-ascii")
		}
		return nil
	default:
		return errors.New("encoding must be declared as utf-8, us-ascii, iso-8859-1, or unknown")
	}
}

func (r Recipe) validatePayload() error {
	if r.Payload.Operator != VerbatimPayload && r.Payload.Operator != Base64Payload {
		return errors.New("a payload mapping declares an operator of verbatim or base64")
	}
	// A batch boundary divides one member into many messages, which is what an
	// import plan is for. One envelope record holds one message, so a payload
	// that turns out to hold more than one is retained as quarantined evidence
	// rather than split at a boundary no recipe declared.
	if r.Payload.Framing != RawFraming && r.Payload.Framing != MLLPFraming {
		return errors.New("a payload mapping declares a framing of raw or mllp")
	}
	if r.Payload.Terminator != hl7.CR && r.Payload.Terminator != hl7.LF && r.Payload.Terminator != hl7.CRLF {
		return errors.New("a payload mapping declares a terminator of cr, lf, or crlf")
	}
	return r.validateLocator(r.Payload.Locator, "payload")
}

func (r Recipe) validateObservedAt() error {
	switch r.ObservedAt.Operator {
	case UnknownTime:
		if r.ObservedAt.Locator != nil {
			return errors.New("an unknown observed time declares no locator")
		}
		return nil
	case RFC3339Time, UnixSecondsTime, UnixMilliseconds, HL7DTMTime:
		return r.validateLocator(r.ObservedAt.Locator, "observed time")
	default:
		return errors.New("an observed time mapping declares an operator of unknown, rfc3339, unix-seconds, unix-milliseconds, or hl7-dtm")
	}
}

func (r Recipe) validateLabel(mapping LabelMapping, what string) error {
	switch mapping.Operator {
	case UnknownLabel:
		if mapping.Declared != "" || mapping.Locator != nil {
			return errors.New("an unknown " + what + " declares neither a value nor a locator")
		}
		return nil
	case DeclaredLabel:
		if mapping.Locator != nil {
			return errors.New("a declared " + what + " declares no locator")
		}
		if !label(mapping.Declared) {
			return errors.New("a declared " + what + " is one bounded printable label")
		}
		return nil
	case FieldLabel:
		if mapping.Declared != "" {
			return errors.New("a " + what + " read from a field declares no constant value")
		}
		return r.validateLocator(mapping.Locator, what)
	default:
		return errors.New("a " + what + " mapping declares an operator of unknown, declared, or field")
	}
}

func (r Recipe) validateDirection() error {
	switch r.Direction.Operator {
	case DeclaredDirection:
		if r.Direction.Locator != nil || r.Direction.Values != nil {
			return errors.New("a declared direction declares neither a locator nor a value table")
		}
		return validDirection(r.Direction.Declared)
	case FieldDirection:
		if r.Direction.Declared != "" {
			return errors.New("a direction read from a field declares no constant value")
		}
		if len(r.Direction.Values) == 0 {
			return errors.New("a direction read from a field declares the value table it is read through")
		}
		if len(r.Direction.Values) > MaxDirectionValues {
			return errors.New("a direction value table holds at most " + strconv.Itoa(MaxDirectionValues) + " entries")
		}
		for i, entry := range r.Direction.Values {
			if !label(entry.Envelope) {
				return errors.New("a direction value table entry names one bounded printable envelope value")
			}
			if err := validDirection(entry.Mapped); err != nil {
				return err
			}
			for _, earlier := range r.Direction.Values[:i] {
				if earlier.Envelope == entry.Envelope {
					return errors.New("a direction value table names one envelope value twice")
				}
			}
		}
		return r.validateLocator(r.Direction.Locator, "direction")
	default:
		return errors.New("a direction mapping declares an operator of declared or field")
	}
}

// validateLocator holds a locator to the shape its envelope can address. A
// flat envelope takes exactly one element, and without a header that element
// is the one-based column index, checked against the declared field count so a
// locator can never reach past the record it names.
func (r Recipe) validateLocator(locator Locator, what string) error {
	if len(locator) == 0 {
		return errors.New("a " + what + " mapping declares where its value is")
	}
	if len(locator) > MaxLocatorElements {
		return errors.New("a " + what + " locator names at most " + strconv.Itoa(MaxLocatorElements) + " elements")
	}
	for _, element := range locator {
		if !label(element) {
			return errors.New("a " + what + " locator element is one bounded printable name")
		}
	}
	named, fields := false, 0
	switch r.Envelope {
	case CSVEnvelope:
		named, fields = r.CSV.Header == HeaderPresent, r.CSV.Fields
	case TextEnvelope:
		fields = r.Text.Fields
	default:
		return nil
	}
	if len(locator) != 1 {
		return errors.New("a " + what + " locator in a flat envelope names exactly one column")
	}
	if named {
		return nil
	}
	if _, ok := flatColumn(locator, fields); !ok {
		return errors.New("a " + what + " locator without a declared header names a column between 1 and the declared field count")
	}
	return nil
}

func validDirection(declared bundle.Direction) error {
	if declared != bundle.Unknown && declared != bundle.Inbound && declared != bundle.Outbound {
		return errors.New("direction must be declared as unknown, inbound, or outbound")
	}
	return nil
}

func fieldCount(fields int) error {
	if fields < 1 || fields > MaxEnvelopeFields {
		return errors.New("a flat envelope declares between 1 and " + strconv.Itoa(MaxEnvelopeFields) + " fields")
	}
	return nil
}

// separatorByte holds a declared delimiter to one byte that cannot collide with
// a record separator or with the bytes an HL7 payload uses as its own
// terminators, so dividing a record can never cut the evidence inside it.
func separatorByte(value string, record Separator) error {
	if len(value) != 1 {
		return errors.New("must be exactly one byte")
	}
	b := value[0]
	if b != 0x09 && (b < 0x20 || b > 0x7e) {
		return errors.New("must be a tab or a printable ASCII character")
	}
	if record != LFSeparator && record != CRLFSeparator {
		return errors.New("record separator must be declared as lf or crlf")
	}
	return nil
}

// label reports whether a value is one bounded, single-line printable label.
// Mapped source and channel values, declared names and locator elements are all
// held to it, so nothing that reaches a receipt can carry a control byte, an
// unbounded string, or a byte sequence that is not valid UTF-8.
func label(value string) bool {
	if value == "" || len(value) > maxLabelBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
