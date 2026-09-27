// Package dataset retains typed, ordered observations and their original
// material. Values and keys are customer-local sensitive evidence. An identity
// checks integrity; it does not authenticate a source or establish a horizon.
package dataset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
)

const Schema = "readmit-dataset/v1"
const ProjectionSchema = "readmit-dataset-projection/v1"
const DatabaseSchema = "readmit-dataset-database-read/v1"
const CaptureSchema = "readmit-dataset-capture-read/v1"
const MaxBytes = 16 << 20
const MaxProjectionBytes = 32 << 20

var invalid = errors.New("invalid typed dataset contract")
var id = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var hash = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Binding struct {
	Run       string `json:"run"`
	Phase     string `json:"phase"`
	Source    string `json:"source_identity"`
	Namespace string `json:"namespace"`
}
type Limits struct {
	MaxRows   int   `json:"max_rows"`
	MaxBytes  int   `json:"max_bytes"`
	TimeoutMS int64 `json:"timeout_ms"`
}
type Envelope struct {
	Encoding importer.Encoding         `json:"encoding"`
	CSV      *importer.CSVDialect      `json:"csv,omitzero"`
	Text     *importer.TextDialect     `json:"text,omitzero"`
	JSON     *importer.DocumentDialect `json:"json,omitzero"`
	XML      *importer.DocumentDialect `json:"xml,omitzero"`
}
type Column struct {
	Name       string           `json:"name"`
	Type       string           `json:"type"`
	Locator    importer.Locator `json:"locator,omitzero"`
	Selector   string           `json:"selector,omitzero"`
	Key        bool             `json:"key"`
	Required   bool             `json:"required"`
	Repeated   bool             `json:"repeated"`
	CodeSystem string           `json:"code_system,omitzero"`
}
type Projection struct {
	Continuation importer.Locator `json:"continuation,omitzero"`
	Order        string           `json:"order"`
	Schema       string           `json:"schema"`
	ID           string           `json:"id"`
	Format       string           `json:"format"`
	Envelope     *Envelope        `json:"envelope,omitzero"`
	Columns      []Column         `json:"columns"`
	Limits       Limits           `json:"limits"`
}

func (p Projection) Shape() importer.EnvelopeShape {
	if p.Envelope == nil {
		return importer.EnvelopeShape{}
	}
	e := p.Envelope
	return importer.EnvelopeShape{Envelope: importer.Envelope(p.Format), Encoding: e.Encoding, CSV: e.CSV, Text: e.Text, JSON: e.JSON, XML: e.XML}
}
func (p Projection) Validate() error {
	if p.Schema != ProjectionSchema || !id.MatchString(p.ID) || len(p.Columns) < 1 || len(p.Columns) > 32 || p.Limits.MaxRows < 1 || p.Limits.MaxRows > 10000 || p.Limits.MaxBytes < 1 || p.Limits.MaxBytes > MaxBytes || p.Limits.TimeoutMS < 1 || p.Limits.TimeoutMS > 300000 {
		return invalid
	}
	if (p.Order != "source" && p.Order != "unordered") || p.Format == "database" && p.Order != "unordered" || !slices.Contains([]string{"json", "csv", "xml", "text", "hl7", "database"}, p.Format) {
		return invalid
	}
	if p.Format != "database" && p.Format != "hl7" && p.Limits.MaxRows > importer.MaxEnvelopeRecords {
		return invalid
	}
	if len(p.Continuation) > 16 || len(p.Continuation) > 0 && p.Format != "json" {
		return invalid
	}
	locators := []importer.Locator{}
	seen := map[string]bool{}
	positions := map[string]bool{}
	keys := 0
	for _, c := range p.Columns {
		if !id.MatchString(c.Name) || seen[c.Name] || !slices.Contains([]string{"text", "decimal", "boolean", "date", "datetime", "code"}, c.Type) || c.Type == "code" && (c.CodeSystem == "" || len(c.CodeSystem) > 256) || c.Type != "code" && c.CodeSystem != "" || c.Key && c.Repeated || c.Repeated && (p.Format == "csv" || p.Format == "text" || p.Format == "database") {
			return invalid
		}
		seen[c.Name] = true
		position := strings.Join(c.Locator, "\x00")
		if p.Format == "hl7" {
			sel, err := hl7.ParseSelector(c.Selector)
			if err != nil {
				return invalid
			}
			position = sel.String()
		}
		if positions[position] {
			return invalid
		}
		positions[position] = true
		if c.Key {
			keys++
		}
		if p.Format == "hl7" {
			selector, err := hl7.ParseSelector(c.Selector)
			if err != nil || len(c.Locator) != 0 || c.Repeated && selector.Parts().Repetition != 1 {
				return invalid
			}
		} else {
			if c.Selector != "" || len(c.Locator) == 0 {
				return invalid
			}
			locators = append(locators, c.Locator)
		}
		if p.Format == "database" && (len(c.Locator) != 1 || !databaseColumn.MatchString(c.Locator[0]) || c.Repeated) {
			return invalid
		}
	}
	if len(p.Continuation) > 0 {
		locators = append(locators, p.Continuation)
	}
	if keys < 1 {
		return invalid
	}
	if p.Format == "hl7" || p.Format == "database" {
		if p.Envelope != nil {
			return invalid
		}
	} else if p.Envelope == nil || p.Shape().Validate(locators) != nil {
		return invalid
	}
	return nil
}

var databaseColumn = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

func DecodeProjection(raw []byte) (Projection, error) {
	var p Projection
	if len(raw) > 256<<10 || json.Unmarshal(raw, &p, json.RejectUnknownMembers(true)) != nil {
		return p, invalid
	}
	return p, p.Validate()
}

type Value struct {
	State      string  `json:"state"`
	Type       string  `json:"type"`
	Text       string  `json:"text,omitzero"`
	Precision  string  `json:"precision,omitzero"`
	Timezone   string  `json:"timezone,omitzero"`
	CodeSystem string  `json:"code_system,omitzero"`
	Items      []Value `json:"items,omitzero"`
}
type Provenance struct {
	Record       int    `json:"record"`
	Offset       int    `json:"offset"`
	Size         int    `json:"size"`
	SourceRecord string `json:"source_record,omitzero"`
}
type Row struct {
	ID         string     `json:"id"`
	Values     []Value    `json:"values"`
	Provenance Provenance `json:"provenance"`
}
type AcquisitionFacts struct {
	Status       string    `json:"status"`
	Attempts     int       `json:"attempts"`
	Retries      int       `json:"retries"`
	HTTPStatus   int       `json:"http_status"`
	StatedAge    string    `json:"stated_age"`
	Bytes        int       `json:"bytes"`
	Records      int       `json:"records"`
	AsOf         time.Time `json:"as_of"`
	ObservedFrom time.Time `json:"observed_from"`
	Note         string    `json:"note"`
}
type Acquisition struct {
	Facts               *AcquisitionFacts `json:"facts,omitzero"`
	Kind                string            `json:"kind"`
	Status              string            `json:"status"`
	StartedAt           time.Time         `json:"started_at"`
	CompletedAt         time.Time         `json:"completed_at"`
	SourceConfiguration []byte            `json:"source_configuration"`
	// Completeness covers this snapshot only, never downstream quiescence.
	Completion string `json:"completion"`
}
type Material struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Size    int    `json:"size"`
	Meaning string `json:"meaning"`
}
type ProjectionBudget struct {
	MaxValues int `json:"max_values"`
	MaxBytes  int `json:"max_bytes"`
}
type Document struct {
	Budget      ProjectionBudget `json:"projection_budget"`
	Schema      string           `json:"schema"`
	Binding     Binding          `json:"binding"`
	Projection  Projection       `json:"projection"`
	Acquisition Acquisition      `json:"acquisition"`
	Material    Material         `json:"material"`
	Status      string           `json:"status"`
	Rows        []Row            `json:"rows"`
}
type Snapshot struct {
	document Document
	raw      []byte
	identity string
}

func (s *Snapshot) Document() Document {
	var d Document
	b, _ := json.Marshal(s.document)
	_ = json.Unmarshal(b, &d)
	return d
}
func (s *Snapshot) Identity() string { return s.identity }
func (s *Snapshot) Usable() bool     { return s != nil && s.document.Status == "complete" }
func Digest(raw []byte) string       { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func Equal(a, b Value) bool {
	return a.State == b.State && a.Type == b.Type && a.Text == b.Text && a.Precision == b.Precision && a.Timezone == b.Timezone && a.CodeSystem == b.CodeSystem && (a.Items == nil) == (b.Items == nil) && slices.EqualFunc(a.Items, b.Items, Equal)
}

func ValidateBinding(b Binding) error {
	if !id.MatchString(b.Run) || !id.MatchString(b.Namespace) || !hash.MatchString(b.Source) || b.Phase != "before" && b.Phase != "after" {
		return invalid
	}
	return nil
}

func (p Projection) Identity() string {
	if p.Validate() != nil {
		return ""
	}
	raw, err := json.Marshal(p, json.Deterministic(true))
	if err != nil {
		return ""
	}
	return Digest(raw)
}
