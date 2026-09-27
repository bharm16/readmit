package dataset

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
)

// Build projects already acquired bounded material. Acquisition owns effects;
// Build and Open have no live source, resolver, credential or network inputs.
func Build(ctx context.Context, binding Binding, p Projection, a Acquisition, raw []byte) (*Snapshot, error) {
	if p.Validate() != nil || !id.MatchString(binding.Run) || !id.MatchString(binding.Namespace) || !hash.MatchString(binding.Source) || binding.Phase != "before" && binding.Phase != "after" || len(a.SourceConfiguration) > 64<<10 || Digest(a.SourceConfiguration) != binding.Source || a.StartedAt.IsZero() || a.CompletedAt.Before(a.StartedAt) || a.Completion != "snapshot" || !contains([]string{"file", "http", "database", "capture"}, a.Kind) || !contains([]string{"complete", "missing", "failed", "truncated", "cancelled"}, a.Status) || len(raw) > MaxBytes {
		return nil, invalid
	}
	if a.Kind == "http" && p.Format == "json" && len(p.Continuation) == 0 {
		return nil, invalid
	}
	if f := a.Facts; f != nil {
		if len(f.Status) > 64 || len(f.StatedAge) > 64 || len(f.Note) > 200 || f.Attempts < 0 || f.Attempts > 5 || f.Retries < 0 || f.Retries > 4 || f.HTTPStatus < 0 || f.HTTPStatus > 599 || f.Bytes < 0 || f.Bytes > MaxBytes || f.Records < 0 || f.Records > 1000000 {
			return nil, invalid
		}
	}
	meaning := "original-response-bytes"
	if p.Format == "database" {
		meaning = "typed-driver-result"
	}
	if p.Format == "hl7" {
		meaning = "verified-capture-occurrence-bytes"
	}
	if (a.Kind == "database") != (p.Format == "database") || (a.Kind == "capture") != (p.Format == "hl7") {
		return nil, invalid
	}
	s := &Snapshot{document: Document{Schema: Schema, Binding: binding, Projection: p, Acquisition: a, Material: Material{Path: "material.bin", SHA256: Digest(raw), Size: len(raw), Meaning: meaning}, Budget: ProjectionBudget{MaxValues: importer.MaxProjectedValues, MaxBytes: MaxProjectionBytes}, Status: a.Status, Rows: []Row{}}, raw: bytes.Clone(raw)}
	// Own all caller-provided configuration and slices before returning a snapshot.
	owned, err := json.Marshal(s.document, json.Deterministic(true))
	if err != nil {
		return nil, invalid
	}
	if json.Unmarshal(owned, &s.document) != nil {
		return nil, invalid
	}
	p = s.document.Projection
	a = s.document.Acquisition
	if a.Status == "complete" {
		switch {
		case len(raw) > p.Limits.MaxBytes:
			s.document.Status = "byte-limit"
		case a.CompletedAt.Sub(a.StartedAt).Milliseconds() > p.Limits.TimeoutMS:
			s.document.Status = "time-limit"
		default:
			if len(p.Continuation) > 0 {
				v, err := importer.DocumentValue(raw, p.Continuation)
				if err != nil || v.State != "absent" && v.State != "empty" && v.State != "null" {
					s.document.Status = "truncated"
					break
				}
			}
			var rows []Row
			var err error
			switch p.Format {
			case "database":
				rows, err = projectDatabase(p, raw)
			case "hl7":
				rows, err = projectCapture(p, raw)
			default:
				rows, err = projectEnvelope(p, raw)
			}
			if err != nil {
				s.document.Status = "projection-failed"
				if errors.Is(err, importer.ErrProjectionLimit) {
					s.document.Status = "projection-limit"
				}
			} else {
				s.document.Rows = rows
				if len(rows) > p.Limits.MaxRows {
					s.document.Status = "row-limit"
				}
				for _, row := range rows {
					for i, v := range row.Values {
						if invalidValue(v) || v.State == "absent" && p.Columns[i].Required {
							s.document.Status = "projection-failed"
						}
					}
				}
			}
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	files := s.files()
	if len(files["manifest.json"]) == 0 || len(files["manifest.json"]) > family.Layout.MaxFileBytes || len(files["manifest.json"])+len(raw) > family.Layout.MaxBytes {
		return nil, importer.ErrProjectionLimit
	}
	s.identity = artifactdir.Identity(Schema, files)
	return s, nil
}
func contains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}
func projectEnvelope(p Projection, raw []byte) ([]Row, error) {
	locators := []importer.Locator{}
	for _, c := range p.Columns {
		locators = append(locators, c.Locator)
	}
	records, err := p.Shape().Project(locators, raw)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, min(len(records), p.Limits.MaxRows+1))
	budget := rowBudget{}
	for i, r := range records {
		if i > p.Limits.MaxRows {
			break
		}
		if r.Reason != "" {
			return nil, invalid
		}
		row := Row{ID: fmt.Sprintf("row-%06d", i+1), Provenance: Provenance{Record: i + 1, Offset: r.Offset, Size: r.Size}, Values: []Value{}}
		for j, c := range p.Columns {
			value := r.Values[j]
			if p.Format == "xml" && c.Repeated && value.Kind != "array" && value.State != "absent" {
				value = importer.ProjectedValue{State: "present", Kind: "array", Items: []importer.ProjectedValue{value}}
			}
			row.Values = append(row.Values, projectValue(c, value))
		}
		if !budget.take(row) {
			return nil, importer.ErrProjectionLimit
		}
		rows = append(rows, row)
	}
	return rows, nil
}

type CaptureRead struct {
	Schema   string       `json:"schema"`
	Identity string       `json:"capture_identity"`
	Rows     []CaptureRow `json:"rows"`
}
type CaptureRow struct {
	Occurrence string `json:"occurrence"`
	Raw        []byte `json:"raw"`
}

func projectCapture(p Projection, raw []byte) ([]Row, error) {
	var capture CaptureRead
	if json.Unmarshal(raw, &capture, json.RejectUnknownMembers(true)) != nil || capture.Schema != CaptureSchema || !hash.MatchString(capture.Identity) || len(capture.Rows) > 10000 {
		return nil, invalid
	}
	rows := []Row{}
	budget := rowBudget{}
	seen := map[string]bool{}
	for i, r := range capture.Rows {
		if len(r.Occurrence) > 64 || r.Occurrence == "" || seen[r.Occurrence] {
			return nil, invalid
		}
		seen[r.Occurrence] = true
		doc, err := hl7.Parse(r.Raw, hl7.Options{})
		if err != nil || len(doc.Messages) != 1 {
			return nil, invalid
		}
		row := Row{ID: fmt.Sprintf("row-%06d", i+1), Provenance: Provenance{Record: i + 1, Size: len(r.Raw), SourceRecord: r.Occurrence}, Values: []Value{}}
		for _, c := range p.Columns {
			selector, _ := hl7.ParseSelector(c.Selector)
			parts := selector.Parts()
			values := []importer.ProjectedValue{}
			repeats := 1
			segments := 0
			fieldRepeats := 0
			fieldState := hl7.Omitted
			for _, seg := range doc.Messages[0].Segments {
				if seg.ID == parts.Segment {
					segments++
					if segments == parts.Occurrence {
						field := seg.Field(parts.Field)
						fieldRepeats = len(field.Repetitions)
						fieldState = field.State
					}
				}
			}
			if segments > 1 && !strings.HasPrefix(c.Selector, parts.Segment+"[") {
				return nil, invalid
			}
			if !c.Repeated && fieldRepeats > 1 && !explicitRepetition.MatchString(c.Selector) {
				return nil, invalid
			}
			if c.Repeated {
				repeats = max(1, fieldRepeats)
				if repeats > 1024 {
					return nil, invalid
				}
			}

			for n := 1; n <= repeats; n++ {
				if c.Repeated {
					parts.Repetition = n
					selector, _ = hl7.NewSelector(parts)
				}
				reading, err := doc.Read(0, selector, hl7.EnforceMSH18)
				v := importer.ProjectedValue{State: string(reading.State), Kind: "string"}
				if v.State == "omitted" {
					v.State = "absent"
				}
				if err != nil || reading.Reason != "" {
					v.State = "unreadable"
				} else if text, ok := reading.Text(); ok {
					v.Text = text
				}
				values = append(values, v)
			}
			v := values[0]
			if c.Repeated && fieldState == hl7.Present {
				v = importer.ProjectedValue{State: "present", Kind: "array", Items: values}
			}
			row.Values = append(row.Values, projectValue(c, v))
		}
		if !budget.take(row) {
			return nil, importer.ErrProjectionLimit
		}
		rows = append(rows, row)
		if i >= p.Limits.MaxRows {
			break
		}
	}
	return rows, nil
}
func (s *Snapshot) files() map[string][]byte {
	raw, _ := json.Marshal(s.document, json.Deterministic(true))
	return map[string][]byte{"manifest.json": raw, "material.bin": bytes.Clone(s.raw)}
}

var family = artifactdir.Family{Layout: artifactdir.Layout{Noun: "typed dataset", RequiredFiles: []string{"manifest.json", "material.bin", "identity.sha256"}, AllowFile: func(n string) bool { return n == "manifest.json" || n == "material.bin" || n == "identity.sha256" }, MaxFiles: 3, MaxFileBytes: 64 << 20, MaxBytes: 80 << 20}, Seal: artifactdir.DirectoryHash(Schema)}

func (s *Snapshot) Write(ctx context.Context, output string) error {
	if s == nil {
		return invalid
	}
	_, err := artifactdir.Write(ctx, output, family, artifactdir.Durable, s.files())
	return err
}
func Open(ctx context.Context, directory string) (*Snapshot, error) {
	files, err := artifactdir.Read(directory, family.Layout)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(Schema, files) {
		return nil, invalid
	}
	var d Document
	if json.Unmarshal(files["manifest.json"], &d, json.RejectUnknownMembers(true)) != nil || d.Schema != Schema {
		return nil, invalid
	}
	snapshot, err := Build(ctx, d.Binding, d.Projection, d.Acquisition, files["material.bin"])
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(snapshot.files()["manifest.json"], files["manifest.json"]) {
		return nil, invalid
	}
	return snapshot, nil
}

var explicitRepetition = regexp.MustCompile(`-[1-9][0-9]*\[[1-9][0-9]*\]`)

type rowBudget struct{ values, bytes int }

func (b *rowBudget) take(row Row) bool {
	var take func(Value) bool
	take = func(v Value) bool {
		b.values++
		if b.values > importer.MaxProjectedValues {
			return false
		}
		for _, item := range v.Items {
			if !take(item) {
				return false
			}
		}
		return true
	}
	for _, v := range row.Values {
		if !take(v) {
			return false
		}
	}
	raw, err := json.Marshal(row, json.Deterministic(true))
	if err != nil {
		return false
	}
	b.bytes += len(raw)
	return b.bytes <= MaxProjectionBytes
}
