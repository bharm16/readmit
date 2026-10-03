// Package valuemap retains authored, field-scoped interface declarations.
// Reading a mapping never applies it to source evidence or a receiver.
package valuemap

import (
	"encoding/csv"
	"encoding/json/v2"
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/strictdoc"
	"io"
	"slices"
	"strings"
	"unicode/utf8"
)

const Schema = "readmit-field-value-map/v1"
const MaxBytes = 2 << 20
const MaxEntries = 2000
const MaxText = 4096

type Entry struct {
	Source             string `json:"source"`
	Destination        string `json:"destination"`
	SourceMeaning      string `json:"source_meaning"`
	DestinationMeaning string `json:"destination_meaning"`
	Provenance         string `json:"provenance"`
}
type Association struct {
	Kind     string `json:"kind"`
	Item     string `json:"item"`
	Revision string `json:"revision"`
}
type Table struct {
	ID         string `json:"id"`
	Edition    string `json:"edition"`
	Provenance string `json:"provenance"`
}
type Document struct {
	Schema              string        `json:"schema"`
	Project             string        `json:"project"`
	Name                string        `json:"name"`
	Edition             string        `json:"edition"`
	SourceSelector      string        `json:"source_selector"`
	DestinationSelector string        `json:"destination_selector"`
	SourceMeaning       string        `json:"source_meaning"`
	DestinationMeaning  string        `json:"destination_meaning"`
	Provenance          string        `json:"provenance"`
	Table               *Table        `json:"table,omitzero"`
	Entries             []Entry       `json:"entries"`
	Associations        []Association `json:"associations"`
}

func text(s string) bool {
	return utf8.ValidString(s) && len(s) <= MaxText && !strings.ContainsRune(s, 0)
}
func edition(s string) bool {
	return slices.Contains([]string{"2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2"}, s)
}
func Validate(d Document) error {
	if d.Schema != Schema || !catalog.ValidID(d.Project) || !catalog.ValidName(d.Name) || !edition(d.Edition) || d.Entries == nil || len(d.Entries) > MaxEntries || d.Associations == nil || len(d.Associations) > 32 {
		return errors.New("value map requires its project, name, supported edition, bounded entries and explicit association list")
	}
	for _, selector := range []string{d.SourceSelector, d.DestinationSelector} {
		s, err := hl7.ParseSelector(selector)
		if err != nil || s.String() != selector {
			return errors.New("source and destination need exact canonical field selectors; use SEG[1]-field[1].component")
		}
	}
	for _, s := range []string{d.SourceMeaning, d.DestinationMeaning, d.Provenance} {
		if !text(s) || strings.TrimSpace(s) == "" {
			return errors.New("record both field meanings and provenance")
		}
	}
	if t := d.Table; t != nil {
		if len(t.ID) != 4 || !edition(t.Edition) || t.Edition != d.Edition || !text(t.Provenance) || strings.TrimSpace(t.Provenance) == "" {
			return errors.New("bound table needs four-digit identity, matching edition and provenance")
		}
		for _, c := range t.ID {
			if c < '0' || c > '9' {
				return errors.New("table identity must retain four digits")
			}
		}
	}
	seen := map[string]bool{}
	for i, e := range d.Entries {
		if seen[e.Source] {
			return fmt.Errorf("entry %d duplicates a source value; no entries were saved", i+1)
		}
		seen[e.Source] = true
		for _, s := range []string{e.Source, e.Destination, e.SourceMeaning, e.DestinationMeaning, e.Provenance} {
			if !text(s) || strings.TrimSpace(s) == "" {
				return fmt.Errorf("entry %d requires bounded source/destination values, meanings and provenance", i+1)
			}
		}
	}
	declared := map[Association]bool{}
	for _, a := range d.Associations {
		if !slices.Contains([]string{"run", "environment", "test"}, a.Kind) || !catalog.ValidID(a.Item) || !catalog.ValidToken(a.Revision) || declared[a] {
			return errors.New("associations require distinct exact run, target configuration or test revisions")
		}
		declared[a] = true
	}
	return nil
}
func Decode(raw []byte) (Document, error) {
	var d Document
	err := (strictdoc.Document{Schema: Schema, MaxBytes: MaxBytes, Required: []string{"project", "name", "edition", "source_selector", "destination_selector", "source_meaning", "destination_meaning", "provenance", "entries", "associations"}, Invalid: "invalid value map JSON", MustDeclare: "unsupported value map schema", TooLarge: "value map exceeds byte bound", Requires: "value map requires all declaration members"}).Decode(raw, &d)
	if err != nil {
		return d, err
	}
	return d, Validate(d)
}
func Encode(d Document) ([]byte, error) {
	if err := Validate(d); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(d, json.Deterministic(true))
	if len(raw) > MaxBytes {
		return nil, errors.New("value map exceeds encoded byte bound")
	}
	return raw, err
}

var Header = []string{"name", "edition", "source_selector", "destination_selector", "source_field_meaning", "destination_field_meaning", "map_provenance", "table_id", "table_edition", "table_provenance", "source_value", "destination_value", "source_meaning", "destination_meaning", "entry_provenance", "associations_json"}

func CSV(d Document) ([]byte, error) {
	if err := Validate(d); err != nil {
		return nil, err
	}
	if len(d.Entries) == 0 {
		return nil, errors.New("CSV export requires at least one entry")
	}
	var out strings.Builder
	w := csv.NewWriter(&out)
	_ = w.Write(Header)
	a, err := json.Marshal(d.Associations, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	for _, e := range d.Entries {
		table := []string{"", "", ""}
		if d.Table != nil {
			table = []string{d.Table.ID, d.Table.Edition, d.Table.Provenance}
		}
		row := []string{d.Name, d.Edition, d.SourceSelector, d.DestinationSelector, d.SourceMeaning, d.DestinationMeaning, d.Provenance}
		row = append(row, table...)
		row = append(row, e.Source, e.Destination, e.SourceMeaning, e.DestinationMeaning, e.Provenance, string(a))
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	if out.Len() > MaxBytes {
		return nil, errors.New("CSV exceeds byte bound")
	}
	return []byte(out.String()), nil
}
func ImportCSV(raw []byte, project string) (Document, error) {
	var d Document
	if len(raw) > MaxBytes || !utf8.Valid(raw) {
		return d, errors.New("CSV must be bounded UTF-8")
	}
	r := csv.NewReader(strings.NewReader(string(raw)))
	r.FieldsPerRecord = len(Header)
	header, err := r.Read()
	if err != nil || !slices.Equal(header, Header) {
		return d, errors.New("CSV columns must match the exported value-map header exactly")
	}
	var metadata []string
	for number := 2; ; number++ {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Document{}, fmt.Errorf("CSV row %d: %w; no entries imported", number, err)
		}
		if metadata == nil {
			metadata = append([]string(nil), row[:10]...)
			metadata = append(metadata, row[15])
			d = Document{Schema: Schema, Project: project, Name: row[0], Edition: row[1], SourceSelector: row[2], DestinationSelector: row[3], SourceMeaning: row[4], DestinationMeaning: row[5], Provenance: row[6], Entries: []Entry{}}
			if row[7] != "" || row[8] != "" || row[9] != "" {
				d.Table = &Table{row[7], row[8], row[9]}
			}
			if err := json.Unmarshal([]byte(row[15]), &d.Associations, json.RejectUnknownMembers(true)); err != nil {
				return Document{}, errors.New("CSV associations_json must contain exact revision declarations")
			}
		} else if !slices.Equal(metadata, append(append([]string(nil), row[:10]...), row[15])) {
			return Document{}, fmt.Errorf("CSV row %d changes map scope or metadata; no entries imported", number)
		}
		d.Entries = append(d.Entries, Entry{row[10], row[11], row[12], row[13], row[14]})
		if len(d.Entries) > MaxEntries {
			return Document{}, errors.New("CSV exceeds entry bound")
		}
	}
	if metadata == nil {
		return d, errors.New("CSV has no entries")
	}
	if err := Validate(d); err != nil {
		return Document{}, err
	}
	return d, nil
}
