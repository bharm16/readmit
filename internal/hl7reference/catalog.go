// Package hl7reference reads explicitly selected offline, edition-specific
// reference material. It does not parse evidence or perform validation.
package hl7reference

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/profileeval"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/strictdoc"
)

const Schema = "readmit-hl7-reference/v1"
const SchemaV2 = "readmit-hl7-reference/v2"
const SchemaV3 = "readmit-hl7-reference/v3"
const SchemaV4 = "readmit-hl7-reference/v4"
const SchemaV5 = "readmit-hl7-reference/v5"
const SchemaV6 = "readmit-hl7-reference/v6"
const LegacyMaxBytes = 16 << 20
const MaxBytes = 32 << 20
const MaxRecords = 20000
const MaxDefinitionBytes = 64 << 10

// Attribute preserves source notation and the reason no value is available.
type Attribute struct {
	Origin Origin `json:"origin,omitzero"`
	State  string `json:"state"`
	Value  string `json:"value"`
}
type Source struct {
	Role      string `json:"role"`
	File      string `json:"file"`
	SHA256    string `json:"sha256"`
	Publisher string `json:"publisher"`
}
type Coverage struct {
	Messages    int      `json:"messages,omitzero"`
	Structures  int      `json:"structures,omitzero"`
	Tables      int      `json:"tables,omitzero"`
	Elements    int      `json:"elements,omitzero"`
	Codes       int      `json:"codes,omitzero"`
	Datatypes   int      `json:"datatypes,omitzero"`
	Components  int      `json:"components,omitzero"`
	Segments    int      `json:"segments"`
	Fields      int      `json:"fields"`
	Definitions int      `json:"definitions"`
	Missing     []string `json:"missing"`
}

// Record is an entity-specific answer, not a label borrowed from its parent.
type TableMetadata struct {
	TableOID          string `json:"table_oid,omitzero"`
	CodeSystemOID     string `json:"code_system_oid,omitzero"`
	ValueSetOID       string `json:"value_set_oid,omitzero"`
	CodeSystemURL     string `json:"code_system_url,omitzero"`
	CodeSystemVersion string `json:"code_system_version,omitzero"`
	Origin            Origin `json:"origin"`
}

type Record struct {
	TableMetadata     TableMetadata      `json:"table_metadata,omitzero"`
	MessageCode       string             `json:"message_code,omitzero"`
	Event             string             `json:"event,omitzero"`
	Structures        []string           `json:"structures,omitzero"`
	StructureID       string             `json:"structure_id,omitzero"`
	Sequence          []profileeval.Node `json:"sequence,omitzero"`
	UnparsedRows      int                `json:"unparsed_rows,omitzero"`
	TableID           string             `json:"table_id,omitzero"`
	TableKind         string             `json:"table_kind,omitzero"`
	ItemID            string             `json:"item_id,omitzero"`
	Code              string             `json:"code,omitzero"`
	ContentState      string             `json:"content_state,omitzero"`
	Uses              []string           `json:"uses,omitzero"`
	Container         string             `json:"container,omitzero"`
	Position          int                `json:"position,omitzero"`
	Key               string             `json:"key"`
	Kind              string             `json:"kind"`
	Segment           string             `json:"segment"`
	Field             int                `json:"field"`
	Name              string             `json:"name"`
	NameOrigin        Origin             `json:"name_origin,omitzero"`
	DefinitionOrigin  Origin             `json:"definition_origin,omitzero"`
	Datatype          Attribute          `json:"datatype"`
	Optionality       Attribute          `json:"optionality"`
	Length            Attribute          `json:"length"`
	ConformanceLength Attribute          `json:"conformance_length"`
	Repetition        Attribute          `json:"repetition"`
	Item              Attribute          `json:"item"`
	Table             Attribute          `json:"table"`
	Section           Attribute          `json:"section"`
	Definition        string             `json:"definition"`
	Source            string             `json:"source"`
}
type document struct {
	Schema   string   `json:"schema"`
	Edition  string   `json:"edition"`
	Sources  []Source `json:"sources"`
	Coverage Coverage `json:"coverage"`
	Records  []Record `json:"records"`
}

// Catalog holds validated records. Identity hashes the exact selected bytes.
type Catalog struct {
	identity string
	document document
	records  map[string]Record
}

// Answer remains small: one record plus catalog identity and coverage, never
// the complete catalog. Missing reference material does not refuse inspection.
type Answer struct {
	TableKeys         []string `json:"table_keys,omitzero"`
	ElementKey        string   `json:"element_key,omitzero"`
	SectionKey        string   `json:"section_key,omitzero"`
	DatatypeKey       string   `json:"datatype_key,omitzero"`
	ParentDatatypeKey string   `json:"parent_datatype_key,omitzero"`
	ParentField       *Record  `json:"parent_field,omitzero"`
	DeclaredDatatype  string   `json:"declared_datatype,omitzero"`
	ResolvedDatatype  string   `json:"resolved_datatype,omitzero"`
	TypeResolution    string   `json:"type_resolution,omitzero"`
	Sources           []Source `json:"sources,omitzero"`
	Status            string   `json:"status"`
	Reason            string   `json:"reason"`
	Identity          string   `json:"identity"`
	Edition           string   `json:"edition"`
	Coverage          Coverage `json:"coverage"`
	MissingCount      int      `json:"missing_count"`
	Record            *Record  `json:"record,omitzero"`
}

var contract = strictdoc.Document{MaxBytes: LegacyMaxBytes, Schema: Schema, Required: []string{"edition", "sources", "coverage", "records"}, Invalid: "invalid reference catalog JSON", TooLarge: "reference catalog exceeds 16 MiB", MustDeclare: "unsupported reference catalog contract", Requires: "reference catalog requires edition, sources, coverage and records"}
var oidNotation = regexp.MustCompile(`^[0-2](?:\.[0-9]+)+$`)
var code = regexp.MustCompile(`^[A-Z][A-Z0-9]{2}$`)
var structureCode = regexp.MustCompile(`^[A-Z][A-Z0-9]{2}(?:_[A-Z0-9]{3})?$`)
var datatypeCode = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,7}(?:_[A-Z0-9]{1,16}){0,4}$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var edition = regexp.MustCompile(`^2\.[0-9]+(?:\.[0-9]+)?$`)

func Decode(raw []byte) (*Catalog, error) {
	var d document
	reader := contract
	var header struct {
		Schema string `json:"schema"`
	}
	if len(raw) <= MaxBytes && json.Unmarshal(raw, &header) == nil && (header.Schema == SchemaV2 || header.Schema == SchemaV3 || header.Schema == SchemaV4 || header.Schema == SchemaV5 || header.Schema == SchemaV6) {
		reader.Schema = header.Schema
		if schemaRank(header.Schema) >= 5 {
			reader.MaxBytes = MaxBytes
			reader.TooLarge = "reference catalog v5 exceeds 32 MiB"
		}
	}
	if err := reader.Decode(raw, &d); err != nil {
		return nil, err
	}
	if err := checkVersionMembers(raw, d.Schema); err != nil {
		return nil, err
	}
	if !edition.MatchString(d.Edition) || len(d.Sources) < 1 || len(d.Sources) > 32 || len(d.Records) < 1 || len(d.Records) > MaxRecords || d.Coverage.Missing == nil || len(d.Coverage.Missing) > MaxRecords {
		return nil, errors.New("invalid reference identity or coverage")
	}
	for _, notice := range d.Coverage.Missing {
		if notice == "" || len(notice) > 256 {
			return nil, errors.New("invalid or unbounded reference coverage notice")
		}
	}
	sourceRoles := map[string]bool{}
	for _, s := range d.Sources {
		if s.Role == "" || s.File == "" || len(s.File) > 256 || !digest.MatchString(s.SHA256) || s.Publisher == "" || len(s.Publisher) > 256 {
			return nil, errors.New("invalid reference source provenance")
		}
		if schemaRank(d.Schema) >= 5 && (len(s.Role) > 64 || sourceRoles[s.Role]) {
			return nil, errors.New("v5 source roles must be bounded and distinct")
		}
		sourceRoles[s.Role] = true
	}
	records := make(map[string]Record, len(d.Records))
	segments, fields, definitions, datatypes, components, tables, elements, codes, messages, structures := 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
	for _, r := range d.Records {
		if r.UnparsedRows < 0 || r.UnparsedRows > MaxRecords || r.Field < 0 || r.Field > 999 || r.Name == "" || len(r.Name) > 256 || len(r.Definition) > MaxDefinitionBytes || r.Source == "" || len(r.Source) > 256 {
			return nil, errors.New("invalid reference record")
		}
		key := recordKey(r.Segment, r.Field)
		switch r.Kind {
		case "segment", "field":
			if !code.MatchString(r.Segment) || r.Container != "" || r.Position != 0 || (r.Kind == "segment") != (r.Field == 0) {
				return nil, errors.New("invalid segment or field reference identity")
			}
		case "datatype", "component":
			if d.Schema == Schema || r.Segment != "" || r.Field != 0 || !datatypeCode.MatchString(r.Container) || r.Position < 0 || r.Position > 999 || (r.Kind == "datatype") != (r.Position == 0) {
				return nil, errors.New("invalid datatype or component reference identity")
			}
			key = "datatype/" + r.Container
			if r.Kind == "component" {
				key = fmt.Sprintf("component/%s/%d", r.Container, r.Position)
			}
			if r.Item.State != "not_applicable" || r.Repetition.State != "not_applicable" {
				return nil, errors.New("datatype components have no independent field item or repetition")
			}
		case "table", "element", "code":
			if schemaRank(d.Schema) < 3 || r.Segment != "" || r.Field != 0 || r.Container != "" || r.Position != 0 {
				return nil, errors.New("invalid vocabulary or element identity")
			}
			if r.ContentState != "available" && r.ContentState != "not_available" && r.ContentState != "not_specified" && r.ContentState != "ambiguous" {
				return nil, errors.New("invalid reference content availability")
			}
			if r.Kind == "element" {
				if len(r.ItemID) != 5 || strings.Trim(r.ItemID, "0123456789") != "" || r.TableID != "" || r.Code != "" {
					return nil, errors.New("data element identifiers have five digits")
				}
				key = "element/" + r.ItemID
			}
			if r.Kind == "table" || r.Kind == "code" {
				if len(r.TableID) != 4 || strings.Trim(r.TableID, "0123456789") != "" || r.ItemID != "" {
					return nil, errors.New("table identifiers have four digits")
				}
				key = "table/" + r.TableID
			}
			if r.Kind == "code" {
				if r.Code == "" || len(r.Code) > 64 {
					return nil, errors.New("invalid reference code")
				}
				key = CodeKey(r.TableID, r.Code)
			}
			if r.Repetition.State != "not_applicable" {
				return nil, errors.New("reference entities carry no field repetition")
			}
		case "message", "structure":
			if schemaRank(d.Schema) < 4 || r.Segment != "" || r.Field != 0 || r.Container != "" || r.Position != 0 {
				return nil, errors.New("invalid message or structure identity")
			}
			if r.Kind == "message" {
				if !code.MatchString(r.MessageCode) || !code.MatchString(r.Event) || len(r.Structures) > 32 || len(r.Structures) == 0 {
					return nil, errors.New("invalid sourced message event")
				}
				key = "message/" + r.MessageCode + "/" + r.Event
			}
			if r.Kind == "structure" {
				if !structureCode.MatchString(r.StructureID) || len(r.Sequence) == 0 && r.ContentState != "not_available" {
					return nil, errors.New("invalid sourced structure")
				}
				key = "structure/" + r.StructureID
				count := 0
				if err := validateSequence(r.Sequence, 0, &count); err != nil {
					return nil, err
				}
			}
		default:
			return nil, errors.New("unsupported reference entity kind")
		}
		if schemaRank(d.Schema) < 3 && (r.TableID != "" || r.TableKind != "" || r.ItemID != "" || r.Code != "" || r.ContentState != "" || r.UnparsedRows != 0 || len(r.Uses) != 0) {
			return nil, errors.New("vocabulary entities require catalog v3")
		}
		if schemaRank(d.Schema) < 4 && (r.MessageCode != "" || r.Event != "" || r.StructureID != "" || len(r.Structures) != 0 || len(r.Sequence) != 0) {
			return nil, errors.New("message context requires catalogv4")
		}
		if len(r.Uses) > 1024 {
			return nil, errors.New("data element uses exceed their bound")
		}
		for _, use := range r.Uses {
			if len(use) > 128 {
				return nil, errors.New("unbounded data element use")
			}
		}
		if r.Key != key {
			return nil, errors.New("reference key does not identify its entity")
		}
		if _, exists := records[key]; exists {
			return nil, errors.New("duplicate reference entity")
		}
		if r.TableMetadata != (TableMetadata{}) {
			if schemaRank(d.Schema) < 6 || r.Kind != "table" {
				return nil, errors.New("table metadata requires a v6 table entity")
			}
			for _, oid := range []string{r.TableMetadata.TableOID, r.TableMetadata.CodeSystemOID, r.TableMetadata.ValueSetOID} {
				if oid != "" && (len(oid) > 128 || !oidNotation.MatchString(oid)) {
					return nil, errors.New("invalid table OID")
				}
			}
			if len(r.TableMetadata.CodeSystemURL) > 512 || len(r.TableMetadata.CodeSystemVersion) > 128 {
				return nil, errors.New("unbounded terminology metadata")
			}
			if !validOrigin(r.TableMetadata.Origin, sourceRoles) || r.TableMetadata.Origin.Source == "" {
				return nil, errors.New("invalid terminology metadata source")
			}
		}
		if schemaRank(d.Schema) >= 5 {
			if err := validateRecordOrigins(r, sourceRoles); err != nil {
				return nil, err
			}
		}
		for _, a := range []Attribute{r.Datatype, r.Optionality, r.Length, r.ConformanceLength, r.Repetition, r.Item, r.Table, r.Section} {
			if len(a.Value) > 128 || (a.State != "specified" && a.State != "not_specified" && a.State != "not_applicable" && a.State != "not_available") || (a.State == "specified") != (a.Value != "") {
				return nil, errors.New("invalid reference attribute availability")
			}
		}
		if r.Item.State == "specified" && (len(r.Item.Value) != 5 || strings.Trim(r.Item.Value, "0123456789") != "") {
			return nil, errors.New("a data element identifier has five digits")
		}
		records[key] = r
		switch r.Kind {
		case "segment":
			segments++
		case "field":
			fields++
		case "datatype":
			datatypes++
		case "component":
			components++
		case "table":
			tables++
		case "element":
			elements++
		case "code":
			codes++
		case "message":
			messages++
		case "structure":
			structures++
		}
		if r.Definition != "" {
			definitions++
		}
	}
	if d.Coverage.Segments != segments || d.Coverage.Fields != fields || d.Coverage.Definitions != definitions || d.Coverage.Datatypes != datatypes || d.Coverage.Components != components || d.Coverage.Tables != tables || d.Coverage.Elements != elements || d.Coverage.Codes != codes || d.Coverage.Messages != messages || d.Coverage.Structures != structures {
		return nil, errors.New("reference coverage does not match its records")
	}
	for _, r := range records {
		if r.Kind == "code" {
			if _, ok := records["table/"+r.TableID]; !ok {
				return nil, errors.New("code reference has no owning table")
			}
		}
		if r.Kind == "component" {
			if _, ok := records["datatype/"+r.Container]; !ok {
				return nil, errors.New("component reference has no owning datatype")
			}
		}
	}
	return &Catalog{identity: fmt.Sprintf("sha256:%x", sha256.Sum256(raw)), document: d, records: records}, nil
}

// Read uses the same bounded regular-file reader as other artifact contracts.
// The path must be explicit and absolute; links and special files are refused.
func Read(path string) (*Catalog, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("select an absolute reference catalog path")
	}
	raw, err := (artifactdir.Document{MaxBytes: MaxBytes}).Read(path)
	if err != nil {
		return nil, errors.New("reference catalog is missing, unreadable, linked or exceeds its size bound")
	}
	return Decode(raw)
}
func recordKey(segment string, field int) string {
	if field == 0 {
		return "segment/" + segment
	}
	return fmt.Sprintf("field/%s/%d", segment, field)
}
func (c *Catalog) Summary() Answer {
	coverage := c.document.Coverage
	coverage.Missing = append([]string{}, coverage.Missing[:min(len(coverage.Missing), 100)]...)
	return Answer{Status: "available", Identity: c.identity, Edition: c.document.Edition, Coverage: coverage, MissingCount: len(c.document.Coverage.Missing), Sources: append([]Source{}, c.document.Sources...)}
}
func (c *Catalog) Lookup(version, segment string, field int) Answer {
	answer := c.Summary()
	if version != c.document.Edition {
		answer.Status = "unsupported_edition"
		answer.Reason = "The selected catalog does not describe the message's declared edition."
		return answer
	}
	record, ok := c.records[recordKey(segment, field)]
	if !ok {
		answer.Status = "not_available"
		answer.Reason = "The selected catalog has no definition for this entity."
		return answer
	}
	answer.Record = &record
	return c.withLinks(answer)
}

func (c *Catalog) Edition() string { return c.document.Edition }
func (c *Catalog) Schema() string  { return c.document.Schema }

// LookupValue follows declared composition only; repetition/segment occurrence
// address evidence, while component records belong to the declared datatype.
// Variable or missing declarations never borrow a conveniently named type.
func (c *Catalog) LookupValue(version string, parts hl7.Parts) Answer {
	answer := c.Lookup(version, parts.Segment, parts.Field)
	answer.DatatypeKey, answer.ParentDatatypeKey = "", ""
	answer.TableKeys = nil
	answer.ElementKey = ""
	answer.SectionKey = ""
	if answer.Record == nil {
		return answer
	}
	parent := *answer.Record
	answer.DeclaredDatatype = parent.Datatype.Value
	answer.TypeResolution = "declared"
	if !datatypeCode.MatchString(parent.Datatype.Value) {
		answer.TypeResolution = "unresolved"
	}
	if parts.Component == 0 {
		return c.withLinks(answer)
	}
	answer.ParentField = &parent
	answer.Record = nil
	container := parent.Datatype.Value
	for _, position := range []int{parts.Component, parts.Subcomponent} {
		if position == 0 {
			break
		}
		answer.Record = nil
		if !datatypeCode.MatchString(container) {
			answer.Status = "unresolved_datatype"
			answer.TypeResolution = "unresolved"
			answer.Reason = "The declared variable or unavailable datatype has no supported contextual resolution."
			return answer
		}
		record, ok := c.records[fmt.Sprintf("component/%s/%d", container, position)]
		if !ok {
			answer.Status = "not_available"
			answer.Reason = "This catalog has no definition for the selected datatype component."
			return answer
		}
		answer.Record = &record
		answer.DeclaredDatatype = record.Datatype.Value
		container = record.Datatype.Value
	}
	return c.withLinks(answer)
}

// Entity returns one reference and a bounded, stable window of its children.
// It never changes an evidence selection; callers retain their own Back context.
func (c *Catalog) Entity(version, key string, offset, limit int) (Answer, []Record, int, error) {
	answer, children, matched, _, err := c.EntitySearch(version, key, offset, limit, "")
	return answer, children, matched, err
}

// EntitySearch filters lexical source codes and meanings, never interprets a
// regular expression or evaluates evidence. Both matched and source totals are
// explicit; changing search resets the client's bounded page.
func (c *Catalog) EntitySearch(version, key string, offset, limit int, query string) (Answer, []Record, int, int, error) {
	if len(key) > 128 || offset < 0 || limit < 1 || limit > 100 || len(query) > 128 {
		return Answer{}, nil, 0, 0, errors.New("reference lookup must name a bounded entity window and search")
	}
	answer := c.Summary()
	if version != c.document.Edition {
		answer.Status = "unsupported_edition"
		answer.Reason = "The catalog does not describe the requested edition."
		return answer, []Record{}, 0, 0, nil
	}
	record, ok := c.records[key]
	if !ok {
		answer.Status = "not_available"
		answer.Reason = "The selected reference entity is not available in this catalog."
		return answer, []Record{}, 0, 0, nil
	}
	record.Uses = append([]string{}, record.Uses...)
	record.Structures = append([]string{}, record.Structures...)
	record.Sequence = cloneSequence(record.Sequence)
	answer.Record = &record
	if record.ContentState != "" && record.ContentState != "available" {
		answer.Status = record.ContentState
		answer.Reason = "Reference content is " + strings.ReplaceAll(record.ContentState, "_", " ") + " in this edition; no content is borrowed."
		return c.withLinks(answer), []Record{}, 0, 0, nil
	}
	children := []Record{}
	total := 0
	term := strings.ToLower(query)
	for _, r := range c.records {
		child := record.Kind == "datatype" && r.Kind == "component" && r.Container == record.Container || record.Kind == "table" && r.Kind == "code" && r.TableID == record.TableID
		if !child {
			continue
		}
		total++
		if term == "" || strings.Contains(strings.ToLower(r.Code+" "+r.Name+" "+r.Definition), term) {
			children = append(children, r)
		}
	}
	slices.SortFunc(children, func(a, b Record) int {
		if record.Kind == "table" {
			return strings.Compare(a.Code, b.Code)
		}
		return a.Position - b.Position
	})
	if offset > len(children) {
		return Answer{}, nil, 0, total, errors.New("the reference window is outside its children")
	}
	return c.withLinks(answer), children[offset:min(len(children), offset+limit)], len(children), total, nil
}

func CodeKey(table, code string) string {
	return "code/" + table + "/" + base64.RawURLEncoding.EncodeToString([]byte(code))
}

func (c *Catalog) withLinks(answer Answer) Answer {
	answer.DatatypeKey, answer.ParentDatatypeKey = "", ""
	if answer.Record == nil {
		return answer
	}
	if answer.Record.Section.State == "specified" {
		answer.SectionKey = answer.Record.Key
	}
	if answer.Record.Item.State == "specified" && len(answer.Record.Item.Value) == 5 && strings.Trim(answer.Record.Item.Value, "0123456789") == "" {
		answer.ElementKey = "element/" + answer.Record.Item.Value
	}
	if answer.Record.Table.State == "specified" {
		for _, id := range strings.Split(answer.Record.Table.Value, "/") {
			if len(id) == 4 && strings.Trim(id, "0123456789") == "" {
				answer.TableKeys = append(answer.TableKeys, "table/"+id)
			}
		}
	}
	if _, ok := c.records["datatype/"+answer.Record.Datatype.Value]; ok {
		answer.DatatypeKey = "datatype/" + answer.Record.Datatype.Value
	}
	if answer.Record.Kind == "component" {
		if _, ok := c.records["datatype/"+answer.Record.Container]; ok {
			answer.ParentDatatypeKey = "datatype/" + answer.Record.Container
		}
	}
	return answer
}
