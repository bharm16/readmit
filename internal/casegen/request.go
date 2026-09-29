// Package casegen generates executable v2 cases from a saved scenario: every
// designed step becomes a message whose structure, fields and data types come
// from the exact local profile and profile pack the request pins, encoded with
// the delimiters, character set and timestamp form the request declares.
//
// A positive case is read back through the lossless reader and evaluated by
// the profile evaluator before anything is written; a message that evaluator
// fails is never emitted as positive, and the event it was made for is
// reported unsupported under that profile rather than repaired. A negative
// variant is declared as one and built exactly as declared, never repaired and
// never gated on passing. Generation is a pure function of the request and the
// two pinned documents: no clock, random source, host or path reaches it, and
// it never sends, provisions a target or approves an expectation.
package casegen

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/scenario"
	"github.com/bharm16/readmit/internal/strictdoc"
)

const (
	// Schema is the generation request contract.
	Schema = "readmit-case-generation/v1"
	// Version is the one generator version this release implements.
	Version = "readmit-case-generator-v1"
	// MaxBytes bounds a request document.
	MaxBytes = 256 << 10

	maxRows        = 8
	maxVariants    = 16
	maxMutations   = 32
	maxNotes       = 8
	maxEdits       = 64
	maxResources   = 16
	maxIdentifiers = 4
	maxTextBytes   = 256
	maxValueBytes  = 64
	// maxOccurrences bounds the messages of one case: a case bundle holds at
	// most this many sources, one per message.
	maxOccurrences = 128
	// maxOutputBytes bounds every message of one generation together.
	maxOutputBytes = 16 << 20
)

// Request is everything one generation reads besides the local profile and the
// profile pack it pins by digest.
type Request struct {
	Schema           string         `json:"schema"`
	GeneratorVersion string         `json:"generator_version"`
	Scenario         jsontext.Value `json:"scenario"`
	Profile          Pin            `json:"profile"`
	Pack             Pin            `json:"pack"`
	Seed             uint64         `json:"seed"`
	Wire             Wire           `json:"wire"`
	Bindings         Bindings       `json:"bindings"`
	Rows             []Row          `json:"rows"`
	Variants         []Variant      `json:"variants"`
	// DerivedFrom is the content identity of the generation this one derives
	// from. It is recorded as ancestry; the ancestor's cases are never read,
	// changed or replaced.
	DerivedFrom string `json:"derived_from,omitzero"`
}

// Pin names one document by its declared contract, identity and the SHA-256 of
// its exact bytes.
type Pin struct {
	Schema  string `json:"schema"`
	ID      string `json:"id"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// Wire is how every message is written: the five delimiters in MSH-1 and MSH-2
// order, the timestamp precision and zone offset every declared instant is
// written with, the processing mode, the sending and receiving applications,
// and how a modifying scheduling event updates an appointment's resources.
// There is no default: each is declared.
type Wire struct {
	Delimiters      string      `json:"delimiters"`
	Precision       string      `json:"precision"`
	Offset          string      `json:"offset"`
	ProcessingID    string      `json:"processing_id"`
	Sending         Application `json:"sending"`
	Receiving       Application `json:"receiving"`
	ResourceUpdates string      `json:"resource_updates"`
}

// Resource update modes (HL7 v2.5.1 section 2.10.4). In action-code mode, the
// mode chapter 10's segment action code field requires of every updating or
// modifying trigger event, a modifying event sends only the resources it
// changes, each with its action code. Snapshot mode, which a site must agree
// on, sends every resource the appointment holds and a removed one is absent.
const (
	ActionCodeUpdates = "action-code"
	SnapshotUpdates   = "snapshot"
)

// Application is one HD namespace pair of MSH-3/4 or MSH-5/6.
type Application struct {
	Application string `json:"application"`
	Facility    string `json:"facility"`
}

// Code is one coded value: code, text and coding system.
type Code struct {
	Code   string `json:"code"`
	Text   string `json:"text"`
	System string `json:"system"`
}

// Person is one XCN person: identifier, family and given name, the assigning
// authority of the identifier, the identifier's type code and the name's type
// code. An empty member is not written; an edition that requires it refuses
// the message by name.
type Person struct {
	ID        string `json:"id"`
	Family    string `json:"family"`
	Given     string `json:"given"`
	Authority string `json:"authority"`
	IDType    string `json:"id_type"`
	NameType  string `json:"name_type"`
}

// Location is one PL location.
type Location struct {
	PointOfCare string `json:"point_of_care"`
	Room        string `json:"room"`
	Bed         string `json:"bed"`
}

// Identifier is an identifier and the assigning authority that issued it.
type Identifier struct {
	Namespace  string `json:"namespace"`
	Identifier string `json:"identifier"`
}

// TypedIdentifier is an identifier, its assigning authority and its
// identifier type code.
type TypedIdentifier struct {
	Namespace  string `json:"namespace"`
	Identifier string `json:"identifier"`
	Type       string `json:"type"`
}

// Resource is one scheduled service or resource an appointment holds.
type Resource struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Identifier Code   `json:"identifier"`
	Role       Code   `json:"role"`
	// A personnel resource's identifier type and name type codes (XCN-13,
	// XCN-10), which some editions require beside its identifier; no other
	// kind declares them.
	IdentifierType string `json:"identifier_type,omitzero"`
	NameType       string `json:"name_type,omitzero"`
}

// Resource kinds and the segment each is written in.
const (
	ServiceResource   = "service"
	GeneralResource   = "general"
	LocationResource  = "location"
	PersonnelResource = "personnel"
)

// Bindings are the business facts the scenario model does not carry and a
// message needs: the facts each subject keeps across steps, and the edits a
// step makes to them, which every later step carries.
type Bindings struct {
	Patients     []PatientBinding     `json:"patients"`
	Visits       []VisitBinding       `json:"visits"`
	Appointments []AppointmentBinding `json:"appointments"`
	Resources    []ResourceBinding    `json:"resources"`
	Orders       []OrderBinding       `json:"orders"`
	Edits        []Edit               `json:"edits"`
}

// ResourceBinding is what a message says of one resource subject of a
// readmit-scenario/v2 workflow besides the identifier and coding system the
// subject declares: the segment it is written in, its text and its role.
type ResourceBinding struct {
	Subject string `json:"subject"`
	Kind    string `json:"kind"`
	Text    string `json:"text"`
	Role    Code   `json:"role"`
	// As for Resource, a personnel resource's type codes only.
	IdentifierType string `json:"identifier_type,omitzero"`
	NameType       string `json:"name_type,omitzero"`
}

// PatientBinding is a patient's identifier type code and the identifiers it
// carries after its own, as further repetitions of PID-3.
type PatientBinding struct {
	Subject        string            `json:"subject"`
	IdentifierType string            `json:"identifier_type"`
	Additional     []TypedIdentifier `json:"additional_identifiers"`
}

// VisitBinding is a visit's patient class, the type code of its visit number
// and its location when the first step sends it.
type VisitBinding struct {
	Subject        string   `json:"subject"`
	Class          string   `json:"class"`
	IdentifierType string   `json:"identifier_type"`
	Location       Location `json:"location"`
}

// AppointmentBinding is an appointment's slot, the people and reason a
// schedule records, an optional placer identity, and the resources it holds.
type AppointmentBinding struct {
	Subject   string      `json:"subject"`
	Placer    *Identifier `json:"placer"`
	Start     string      `json:"start_after"`
	Duration  int         `json:"duration_minutes"`
	Reason    Code        `json:"reason"`
	Contact   Person      `json:"contact"`
	EnteredBy Person      `json:"entered_by"`
	Resources []Resource  `json:"resources"`
}

// OrderBinding is the service an order requests and the coding system of its
// results' observation codes.
type OrderBinding struct {
	Subject           string `json:"subject"`
	Service           Code   `json:"service"`
	ObservationSystem string `json:"observation_system"`
}

// Edit is one change a step makes to a subject's bound facts: renaming the
// patient (an update), relocating a visit (a transfer), moving an
// appointment's slot (a reschedule), or changing the resources it holds (a
// modification). The step's message and every later message carry it.
type Edit struct {
	Step   string `json:"step"`
	Op     string `json:"op"`
	Family string `json:"family,omitzero"`
	// Given is a pointer so a rename to an empty given name is written as
	// one, never omitted.
	Given    *string   `json:"given,omitzero"`
	Location *Location `json:"location,omitzero"`
	Start    string    `json:"start_after,omitzero"`
	Duration int       `json:"duration_minutes,omitzero"`
}

// Edit operators and the lifecycle event each belongs to.
const (
	RenameEdit     = "rename"
	RelocateEdit   = "relocate"
	RescheduleEdit = "reschedule"
)

// Row is one parameter row: the patient's name, the notes every message
// carries and the character set the row's messages are written in. Rows are
// separate workflows over the same designed identities.
type Row struct {
	ID      string   `json:"id"`
	Family  string   `json:"family"`
	Given   string   `json:"given"`
	Notes   []string `json:"notes"`
	Charset string   `json:"charset"`
}

// Character sets MSH-18 declares.
const (
	ASCII  = "ASCII"
	UTF8   = "UNICODE UTF-8"
	Latin1 = "8859/1"
)

// Variant is one declared case over every row. Its polarity is the author's
// declaration: a positive variant must evaluate without failure, a negative
// one is built exactly as declared and never gated on passing.
type Variant struct {
	ID        string     `json:"id"`
	Polarity  string     `json:"polarity"`
	Mutations []Mutation `json:"mutations"`
}

// Polarities.
const (
	Positive = "positive"
	Negative = "negative"
)

// Mutation is a closed union; each operator carries exactly its own members.
type Mutation struct {
	Op       string `json:"op"`
	Step     string `json:"step"`
	Charset  string `json:"charset,omitzero"`
	Offset   string `json:"offset,omitzero"`
	After    string `json:"after,omitzero"`
	Before   string `json:"before,omitzero"`
	Subject  string `json:"subject,omitzero"`
	Value    string `json:"value,omitzero"`
	Selector string `json:"selector,omitzero"`
	State    string `json:"state,omitzero"`
}

// Field states a field mutation writes.
const (
	AbsentState  = "absent"
	EmptyState   = "empty"
	NullState    = "null"
	InvalidState = "invalid"
)

// Mutation operators. Charset, offset and delay keep a message valid and may
// appear in a positive variant; the rest exist to make a negative case.
const (
	CharsetOp    = "charset"
	OffsetOp     = "offset"
	DelayOp      = "delay"
	DuplicateOp  = "duplicate"
	OmitOp       = "omit"
	MoveOp       = "move"
	IdentifierOp = "identifier"
	NamespaceOp  = "namespace"
	FieldOp      = "field"
	UnlinkOp     = "unlink-correction"
)

var operatorMembers = map[string][]string{
	CharsetOp:    {"charset"},
	OffsetOp:     {"offset"},
	DelayOp:      {"after"},
	DuplicateOp:  {},
	OmitOp:       {},
	MoveOp:       {"before"},
	IdentifierOp: {"subject", "value"},
	NamespaceOp:  {"subject", "value"},
	FieldOp:      {"selector", "state"},
	UnlinkOp:     {},
}

var editMembers = map[string][]string{
	RenameEdit:     {"family", "given"},
	RelocateEdit:   {"location"},
	RescheduleEdit: {"start_after", "duration_minutes"},
}

// strictly reads one nested object: every named member present and not null,
// and no member the type does not declare. It never names a value.
func strictly(data []byte, target any, what string, required ...string) error {
	var members map[string]jsontext.Value
	if err := json.Unmarshal(data, &members); err != nil || members == nil {
		return errors.New(what + " is a JSON object")
	}
	for _, name := range required {
		v, ok := members[name]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return errors.New(what + " requires explicit members: " + strings.Join(required, ", "))
		}
	}
	if err := json.Unmarshal(data, target, json.RejectUnknownMembers(true)); err != nil {
		return errors.New(what + " declares no other member than: " + strings.Join(required, ", "))
	}
	return nil
}

// exactly is strictly for a union: the members present are exactly the ones
// named, so an operator never carries another operator's member.
func exactly(data []byte, target any, what string, members ...string) error {
	if err := strictly(data, target, what, members...); err != nil {
		return err
	}
	var present map[string]jsontext.Value
	_ = json.Unmarshal(data, &present)
	if len(present) != len(members) {
		return errors.New(what + " carries members of another operator")
	}
	return nil
}

func (p *Pin) UnmarshalJSON(data []byte) error {
	type plain Pin
	var v plain
	if err := strictly(data, &v, "a pin", "schema", "id", "version", "sha256"); err != nil {
		return err
	}
	*p = Pin(v)
	return nil
}

func (w *Wire) UnmarshalJSON(data []byte) error {
	type plain Wire
	var v plain
	if err := strictly(data, &v, "wire", "delimiters", "precision", "offset", "processing_id", "sending", "receiving", "resource_updates"); err != nil {
		return err
	}
	*w = Wire(v)
	return nil
}

func (a *Application) UnmarshalJSON(data []byte) error {
	type plain Application
	var v plain
	if err := strictly(data, &v, "an application", "application", "facility"); err != nil {
		return err
	}
	*a = Application(v)
	return nil
}

func (c *Code) UnmarshalJSON(data []byte) error {
	type plain Code
	var v plain
	if err := strictly(data, &v, "a code", "code", "text", "system"); err != nil {
		return err
	}
	*c = Code(v)
	return nil
}

func (p *Person) UnmarshalJSON(data []byte) error {
	type plain Person
	var v plain
	if err := strictly(data, &v, "a person", "id", "family", "given", "authority", "id_type", "name_type"); err != nil {
		return err
	}
	*p = Person(v)
	return nil
}

func (l *Location) UnmarshalJSON(data []byte) error {
	type plain Location
	var v plain
	if err := strictly(data, &v, "a location", "point_of_care", "room", "bed"); err != nil {
		return err
	}
	*l = Location(v)
	return nil
}

func (i *Identifier) UnmarshalJSON(data []byte) error {
	type plain Identifier
	var v plain
	if err := strictly(data, &v, "an identifier", "namespace", "identifier"); err != nil {
		return err
	}
	*i = Identifier(v)
	return nil
}

func (i *TypedIdentifier) UnmarshalJSON(data []byte) error {
	type plain TypedIdentifier
	var v plain
	if err := strictly(data, &v, "a typed identifier", "namespace", "identifier", "type"); err != nil {
		return err
	}
	*i = TypedIdentifier(v)
	return nil
}

func (r *Resource) UnmarshalJSON(data []byte) error {
	type plain Resource
	var v plain
	if err := strictly(data, &v, "a resource", "id", "kind", "identifier", "role"); err != nil {
		return err
	}
	*r = Resource(v)
	return nil
}

func (b *Bindings) UnmarshalJSON(data []byte) error {
	type plain Bindings
	var v plain
	if err := strictly(data, &v, "bindings", "patients", "visits", "appointments", "resources", "orders", "edits"); err != nil {
		return err
	}
	*b = Bindings(v)
	return nil
}

func (p *PatientBinding) UnmarshalJSON(data []byte) error {
	type plain PatientBinding
	var v plain
	if err := strictly(data, &v, "a patient binding", "subject", "identifier_type", "additional_identifiers"); err != nil {
		return err
	}
	*p = PatientBinding(v)
	return nil
}

func (b *VisitBinding) UnmarshalJSON(data []byte) error {
	type plain VisitBinding
	var v plain
	if err := strictly(data, &v, "a visit binding", "subject", "class", "identifier_type", "location"); err != nil {
		return err
	}
	*b = VisitBinding(v)
	return nil
}

func (b *AppointmentBinding) UnmarshalJSON(data []byte) error {
	type plain AppointmentBinding
	var v plain
	// placer is declared explicitly, as null when the appointment has none.
	var members map[string]jsontext.Value
	if err := json.Unmarshal(data, &members); err != nil {
		return errors.New("an appointment binding is a JSON object")
	}
	if _, ok := members["placer"]; !ok {
		return errors.New("an appointment binding declares placer, as null when there is none")
	}
	if err := strictly(data, &v, "an appointment binding", "subject", "start_after", "duration_minutes", "reason", "contact", "entered_by", "resources"); err != nil {
		return err
	}
	*b = AppointmentBinding(v)
	return nil
}

func (b *ResourceBinding) UnmarshalJSON(data []byte) error {
	type plain ResourceBinding
	var v plain
	if err := strictly(data, &v, "a resource binding", "subject", "kind", "text", "role"); err != nil {
		return err
	}
	*b = ResourceBinding(v)
	return nil
}

func (b *OrderBinding) UnmarshalJSON(data []byte) error {
	type plain OrderBinding
	var v plain
	if err := strictly(data, &v, "an order binding", "subject", "service", "observation_system"); err != nil {
		return err
	}
	*b = OrderBinding(v)
	return nil
}

func (e *Edit) UnmarshalJSON(data []byte) error {
	var head struct {
		Op string `json:"op"`
	}
	_ = json.Unmarshal(data, &head)
	members, ok := editMembers[head.Op]
	if !ok {
		return errors.New("an edit names one of rename, relocate, reschedule or resources")
	}
	type plain Edit
	var v plain
	if err := exactly(data, &v, "a "+head.Op+" edit", append([]string{"step", "op"}, members...)...); err != nil {
		return err
	}
	*e = Edit(v)
	return nil
}

func (r *Row) UnmarshalJSON(data []byte) error {
	type plain Row
	var v plain
	if err := strictly(data, &v, "a row", "id", "family", "given", "notes", "charset"); err != nil {
		return err
	}
	*r = Row(v)
	return nil
}

func (v *Variant) UnmarshalJSON(data []byte) error {
	type plain Variant
	var p plain
	if err := strictly(data, &p, "a variant", "id", "polarity", "mutations"); err != nil {
		return err
	}
	*v = Variant(p)
	return nil
}

func (m *Mutation) UnmarshalJSON(data []byte) error {
	var head struct {
		Op string `json:"op"`
	}
	_ = json.Unmarshal(data, &head)
	members, ok := operatorMembers[head.Op]
	if !ok {
		return errors.New("a mutation names one declared operator")
	}
	type plain Mutation
	var v plain
	if err := exactly(data, &v, "a "+head.Op+" mutation", append([]string{"op", "step"}, members...)...); err != nil {
		return err
	}
	*m = Mutation(v)
	return nil
}

var requestDocument = strictdoc.Document{
	MaxBytes:    MaxBytes,
	Schema:      Schema,
	Required:    []string{"generator_version", "scenario", "profile", "pack", "seed", "wire", "bindings", "rows", "variants"},
	Invalid:     "invalid case generation request",
	TooLarge:    "a case generation request exceeds 256 KiB",
	MustDeclare: "a case generation request must declare " + Schema,
	Requires:    "a case generation request requires generator_version, scenario, profile, pack, seed, wire, bindings, rows and variants",
}

// Decode reads and validates one request, including the scenario it embeds and
// every reference from bindings and variants into it. It reads neither the
// profile nor the pack: those are checked against their pins when generating.
// No value ever appears in an error.
func Decode(data []byte) (Request, scenario.Workflow, error) {
	var r Request
	if err := requestDocument.Decode(data, &r); err != nil {
		return Request{}, scenario.Workflow{}, err
	}
	if r.GeneratorVersion != Version {
		return Request{}, scenario.Workflow{}, errors.New("unsupported case generator version")
	}
	// The embedded scenario is identified by its canonical form, so how its
	// JSON is spaced or ordered never changes a digest.
	if err := r.Scenario.Canonicalize(); err != nil {
		return Request{}, scenario.Workflow{}, errors.New("the embedded scenario is not canonical JSON")
	}
	workflow, err := scenario.DecodeDocument(r.Scenario)
	if err != nil {
		return Request{}, scenario.Workflow{}, errors.New("the request embeds no readable scenario: " + err.Error())
	}
	if _, err := workflow.Preview(); err != nil {
		return Request{}, scenario.Workflow{}, errors.New("the embedded scenario's lifecycle outcomes are not confirmed: " + err.Error())
	}
	if err := r.validate(workflow); err != nil {
		return Request{}, scenario.Workflow{}, err
	}
	return r, workflow, nil
}

var (
	portable = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	hexPin   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	token    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
)

func (r Request) validate(w scenario.Workflow) error {
	for _, pin := range []Pin{r.Profile, r.Pack} {
		if !token.MatchString(pin.ID) || !token.MatchString(pin.Version) || !hexPin.MatchString(pin.SHA256) || pin.Schema == "" {
			return errors.New("a pin names a contract, an id, a version and a lowercase SHA-256")
		}
	}
	if r.DerivedFrom != "" && !hexPin.MatchString(r.DerivedFrom) {
		return errors.New("derived_from is the lowercase SHA-256 content identity of a generation")
	}
	if err := r.Wire.validate(); err != nil {
		return err
	}
	subjects := map[string]scenario.Subject{}
	for _, s := range w.Subjects {
		subjects[s.ID] = s
	}
	steps := map[string]scenario.Step{}
	for _, s := range w.Steps {
		steps[s.ID] = s
	}
	if err := r.Bindings.validate(w, subjects, steps); err != nil {
		return err
	}
	if len(r.Rows) < 1 || len(r.Rows) > maxRows {
		return errors.New("a request declares 1 to 8 rows")
	}
	seen := map[string]bool{}
	for _, row := range r.Rows {
		if !portable.MatchString(row.ID) || seen[row.ID] {
			return errors.New("row ids are distinct lowercase names of at most 32 bytes")
		}
		seen[row.ID] = true
		if !charset(row.Charset) {
			return errors.New("a row's charset is ASCII, UNICODE UTF-8 or 8859/1")
		}
		if err := text(row.Family, "a family name", false); err != nil {
			return err
		}
		if err := text(row.Given, "a given name", true); err != nil {
			return err
		}
		if len(row.Notes) > maxNotes {
			return errors.New("a row carries at most 8 notes")
		}
		for _, note := range row.Notes {
			if err := text(note, "a note", false); err != nil {
				return err
			}
		}
	}
	if len(r.Variants) < 1 || len(r.Variants) > maxVariants {
		return errors.New("a request declares 1 to 16 variants")
	}
	seen = map[string]bool{}
	for _, v := range r.Variants {
		if !portable.MatchString(v.ID) || seen[v.ID] {
			return errors.New("variant ids are distinct lowercase names of at most 32 bytes")
		}
		seen[v.ID] = true
		if err := v.validate(w, subjects, steps); err != nil {
			return err
		}
	}
	return nil
}

func (w Wire) validate() error {
	d := w.Delimiters
	if len(d) != 5 {
		return errors.New("wire delimiters are five characters: field, component, repetition, escape and subcomponent")
	}
	for i := 0; i < 5; i++ {
		c := d[i]
		if c < 0x21 || c > 0x7e || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || bytes.IndexByte([]byte(d[:i]), c) >= 0 {
			return errors.New("wire delimiters are five distinct printable punctuation characters")
		}
	}
	if w.Precision != "minute" && w.Precision != "second" {
		return errors.New("wire precision is minute or second")
	}
	if w.Offset != "none" {
		if _, err := zone(w.Offset); err != nil {
			return err
		}
	}
	if w.ProcessingID != "P" && w.ProcessingID != "D" && w.ProcessingID != "T" {
		return errors.New("wire processing_id is P, D or T")
	}
	if w.ResourceUpdates != ActionCodeUpdates && w.ResourceUpdates != SnapshotUpdates {
		return errors.New("wire resource_updates is action-code or snapshot")
	}
	for _, a := range []Application{w.Sending, w.Receiving} {
		if err := value(a.Application, "an application", true); err != nil {
			return err
		}
		if err := value(a.Facility, "a facility", true); err != nil {
			return err
		}
	}
	return nil
}

func (b Bindings) validate(w scenario.Workflow, subjects map[string]scenario.Subject, steps map[string]scenario.Step) error {
	bound := map[string]bool{}
	claim := func(subject string, kind scenario.Kind) error {
		s, ok := subjects[subject]
		if !ok || s.Kind != kind || bound[subject] {
			return errors.New("a binding names a " + string(kind) + " subject the scenario declares, once")
		}
		bound[subject] = true
		return nil
	}
	for _, p := range b.Patients {
		if err := claim(p.Subject, scenario.PatientSubject); err != nil {
			return err
		}
		if err := value(p.IdentifierType, "an identifier type code", true); err != nil {
			return err
		}
		if len(p.Additional) > maxIdentifiers {
			return errors.New("a patient carries at most 4 additional identifiers")
		}
		for _, id := range p.Additional {
			if err := (Identifier{Namespace: id.Namespace, Identifier: id.Identifier}).validate(); err != nil {
				return err
			}
			if err := value(id.Type, "an identifier type code", true); err != nil {
				return err
			}
		}
	}
	for _, v := range b.Visits {
		if err := claim(v.Subject, scenario.VisitSubject); err != nil {
			return err
		}
		if err := value(v.Class, "a patient class", false); err != nil {
			return err
		}
		if err := value(v.IdentifierType, "an identifier type code", true); err != nil {
			return err
		}
		if err := v.Location.validate(); err != nil {
			return err
		}
	}
	for _, a := range b.Appointments {
		if err := claim(a.Subject, scenario.AppointmentSubject); err != nil {
			return err
		}
		if a.Placer != nil {
			if err := a.Placer.validate(); err != nil {
				return err
			}
		}
		if err := slot(a.Start, a.Duration); err != nil {
			return err
		}
		if err := a.Reason.validate(); err != nil {
			return err
		}
		for _, p := range []Person{a.Contact, a.EnteredBy} {
			if err := p.validate(); err != nil {
				return err
			}
		}
		if err := resources(a.Resources); err != nil {
			return err
		}
	}
	for _, r := range b.Resources {
		if err := claim(r.Subject, scenario.ResourceSubject); err != nil {
			return err
		}
		if !slices.Contains([]string{ServiceResource, GeneralResource, LocationResource, PersonnelResource}, r.Kind) {
			return errors.New("a resource is a service, general, location or personnel resource")
		}
		if err := text(r.Text, "a resource's text", true); err != nil {
			return err
		}
		if err := r.Role.validate(); err != nil {
			return err
		}
		if err := personnelTypes(r.Kind, r.IdentifierType, r.NameType); err != nil {
			return err
		}
	}
	for _, o := range b.Orders {
		if err := claim(o.Subject, scenario.OrderSubject); err != nil {
			return err
		}
		if err := o.Service.validate(); err != nil {
			return err
		}
		if err := value(o.ObservationSystem, "an observation coding system", true); err != nil {
			return err
		}
	}
	// Every subject a message is written for is bound, so no business fact is
	// ever invented.
	for _, s := range w.Subjects {
		if !bound[s.ID] {
			return errors.New("every subject of the scenario has a binding")
		}
	}
	if len(b.Edits) > maxEdits {
		return errors.New("a request declares at most 64 edits")
	}
	edited := map[string]bool{}
	for _, e := range b.Edits {
		step, ok := steps[e.Step]
		if !ok || edited[e.Step] {
			return errors.New("an edit names a step the scenario declares, at most once")
		}
		edited[e.Step] = true
		want := map[string]scenario.Event{RenameEdit: "A08", RelocateEdit: "A02", RescheduleEdit: "S13"}[e.Op]
		if step.Event != want {
			return errors.New("a " + e.Op + " edit belongs to a " + string(want) + " step")
		}
		switch e.Op {
		case RenameEdit:
			if err := text(e.Family, "a family name", false); err != nil {
				return err
			}
			if err := text(*e.Given, "a given name", true); err != nil {
				return err
			}
		case RelocateEdit:
			if err := e.Location.validate(); err != nil {
				return err
			}
		case RescheduleEdit:
			if err := slot(e.Start, e.Duration); err != nil {
				return err
			}
		}
	}
	return nil
}

func resources(list []Resource) error {
	if len(list) > maxResources {
		return errors.New("an appointment holds at most 16 resources")
	}
	seen := map[string]bool{}
	for _, r := range list {
		if !portable.MatchString(r.ID) || seen[r.ID] {
			return errors.New("resource ids are distinct lowercase names of at most 32 bytes")
		}
		seen[r.ID] = true
		if !slices.Contains([]string{ServiceResource, GeneralResource, LocationResource, PersonnelResource}, r.Kind) {
			return errors.New("a resource is a service, general, location or personnel resource")
		}
		if err := r.Identifier.validate(); err != nil {
			return err
		}
		if err := r.Role.validate(); err != nil {
			return err
		}
		if err := personnelTypes(r.Kind, r.IdentifierType, r.NameType); err != nil {
			return err
		}
	}
	return nil
}

// personnelTypes checks a resource's identifier type and name type codes:
// only a personnel resource declares them.
func personnelTypes(kind, identifierType, nameType string) error {
	if kind != PersonnelResource && (identifierType != "" || nameType != "") {
		return errors.New("only a personnel resource declares an identifier type or name type")
	}
	for _, v := range []string{identifierType, nameType} {
		if err := value(v, "a personnel resource's type code", true); err != nil {
			return err
		}
	}
	return nil
}

func slot(start string, minutes int) error {
	d, err := time.ParseDuration(start)
	if err != nil || d < 0 || d > 8760*time.Hour || d%time.Minute != 0 {
		return errors.New("a slot starts a whole number of minutes, at most 8760h, after the base time")
	}
	if minutes < 1 || minutes > 1440 {
		return errors.New("a slot lasts 1 to 1440 minutes")
	}
	return nil
}

func (v Variant) validate(w scenario.Workflow, subjects map[string]scenario.Subject, steps map[string]scenario.Step) error {
	if v.Polarity != Positive && v.Polarity != Negative {
		return errors.New("a variant declares its polarity as positive or negative")
	}
	if len(v.Mutations) > maxMutations {
		return errors.New("a variant carries at most 32 mutations")
	}
	targets := map[string]bool{}
	for _, m := range v.Mutations {
		step, ok := steps[m.Step]
		if !ok {
			return errors.New("a mutation names a step the scenario declares")
		}
		key := m.Op + "/" + m.Step + "/" + m.Subject + "/" + m.Selector
		if targets[key] {
			return errors.New("a mutation target is repeated")
		}
		targets[key] = true
		if v.Polarity == Positive && m.Op != CharsetOp && m.Op != OffsetOp && m.Op != DelayOp {
			return errors.New("a positive variant changes only charset, offset or delay; declare " + m.Op + " in a negative variant")
		}
		switch m.Op {
		case CharsetOp:
			if !charset(m.Charset) {
				return errors.New("a charset mutation names ASCII, UNICODE UTF-8 or 8859/1")
			}
		case OffsetOp:
			if _, err := zone(m.Offset); err != nil {
				return err
			}
		case DelayOp:
			if _, err := delay(m.After); err != nil {
				return err
			}
		case MoveOp:
			if _, ok := steps[m.Before]; !ok || m.Before == m.Step {
				return errors.New("a move names another declared step to arrive before")
			}
		case IdentifierOp, NamespaceOp:
			s, ok := subjects[m.Subject]
			if !ok {
				return errors.New("an identity mutation names a declared subject")
			}
			// A scheduling step's message carries its appointment and that
			// appointment's resources, whichever of them the step acts on.
			appointment := step.Subject
			if a := subjects[step.Subject].Appointment; a != "" {
				appointment = a
			}
			if s.Kind != scenario.PatientSubject && s.ID != step.Subject && s.ID != appointment && s.Appointment != appointment {
				return errors.New("an identity mutation names a subject the step's message carries")
			}
			if err := value(m.Value, "a replacement "+m.Op, false); err != nil {
				return err
			}
		case FieldOp:
			if _, err := fieldTarget(m.Selector); err != nil {
				return err
			}
			if !slices.Contains([]string{AbsentState, EmptyState, NullState, InvalidState}, m.State) {
				return errors.New("a field mutation's state is absent, empty, null or invalid")
			}
		case UnlinkOp:
			if step.Event != "ORU-C" {
				return errors.New("unlink-correction applies to a corrected result report")
			}
		}
	}
	return nil
}

func (i Identifier) validate() error {
	if err := value(i.Namespace, "an assigning authority", false); err != nil {
		return err
	}
	return value(i.Identifier, "an identifier", false)
}

func (c Code) validate() error {
	if err := value(c.Code, "a code", false); err != nil {
		return err
	}
	if err := text(c.Text, "a code's text", true); err != nil {
		return err
	}
	return value(c.System, "a coding system", true)
}

func (p Person) validate() error {
	if err := value(p.ID, "a person identifier", false); err != nil {
		return err
	}
	if err := text(p.Family, "a family name", true); err != nil {
		return err
	}
	if err := text(p.Given, "a given name", true); err != nil {
		return err
	}
	for _, v := range []string{p.Authority, p.IDType, p.NameType} {
		if err := value(v, "a person's authority or type code", true); err != nil {
			return err
		}
	}
	return nil
}

func (l Location) validate() error {
	if err := value(l.PointOfCare, "a point of care", false); err != nil {
		return err
	}
	if err := value(l.Room, "a room", true); err != nil {
		return err
	}
	return value(l.Bed, "a bed", true)
}

// value bounds a declared code or identifier: printable, at most 64 bytes.
// Delimiters are escaped when written, so they are allowed.
func value(s, what string, empty bool) error {
	if s == "" && !empty || len(s) > maxValueBytes || !utf8.ValidString(s) {
		return errors.New(what + " is 1 to 64 bytes of text")
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return errors.New(what + " holds no control characters")
		}
	}
	return nil
}

// text bounds declared free text: at most 256 bytes without control
// characters. Delimiters are escaped when written.
func text(s, what string, empty bool) error {
	if s == "" && !empty || len(s) > maxTextBytes || !utf8.ValidString(s) {
		return errors.New(what + " is 1 to 256 bytes of text")
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return errors.New(what + " holds no control characters")
		}
	}
	return nil
}

func charset(s string) bool { return s == ASCII || s == UTF8 || s == Latin1 }

func delay(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 || d > 8760*time.Hour || d%time.Second != 0 {
		return 0, errors.New("a delay is a positive whole number of seconds, at most 8760h")
	}
	return d, nil
}

// zone reads a signed HH:MM offset of at most fourteen hours.
func zone(s string) (*time.Location, error) {
	if len(s) != 6 || (s[0] != '+' && s[0] != '-') || s[3] != ':' || strings.Trim(s[1:3]+s[4:6], "0123456789") != "" {
		return nil, errors.New("an offset is a signed HH:MM of at most 14 hours")
	}
	hours, err1 := strconv.Atoi(s[1:3])
	minutes, err2 := strconv.Atoi(s[4:6])
	if err1 != nil || err2 != nil || minutes > 59 || hours*60+minutes > 14*60 {
		return nil, errors.New("an offset is a signed HH:MM of at most 14 hours")
	}
	seconds := (hours*60 + minutes) * 60
	if s[0] == '-' {
		seconds = -seconds
	}
	return time.FixedZone("", seconds), nil
}
