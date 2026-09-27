package importer

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"encoding/xml"
	"errors"
	"io"
	"slices"
	"unicode/utf8"
)

// ProjectedValue preserves envelope states before any domain type is applied.
// Text is a decoded scalar; JSON number spelling is retained exactly. Items
// preserve repeated values in source order, including nulls and duplicates.
type ProjectedValue struct {
	State, Kind, Text string
	Items             []ProjectedValue
}
type ProjectedRecord struct {
	Offset, Size int
	Values       []ProjectedValue
	Reason       string
}

// Project is the typed companion to the frozen Divide contract. It uses the
// same bounded record splitters, with strict complete-document validation and
// source-specific value states. Missing JSON members do not become empty text.
func (s EnvelopeShape) Project(locators []Locator, data []byte) ([]ProjectedRecord, error) {
	if len(data) > 16<<20 || s.Validate(locators) != nil || declaredEncoding(s.Encoding, data) != nil {
		return nil, ErrDeclaredEnvelope
	}
	if s.Envelope == XMLEnvelope {
		return s.projectXML(locators, data)
	}
	selected := locators
	if s.Envelope == JSONEnvelope {
		decoder := jsontext.NewDecoder(bytes.NewReader(data))
		if _, err := decoder.ReadValue(); err != nil {
			return nil, ErrDeclaredEnvelope
		}
		if _, err := decoder.ReadValue(); err != io.EOF {
			return nil, ErrDeclaredEnvelope
		}
		selected = nil
	}
	records, err := envelopeRecords(s.recipe(), selected, data)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectedRecord, 0, len(records))
	budget := projectionBudget{}
	for _, r := range records {
		row := ProjectedRecord{Offset: r.offset, Size: r.size, Reason: r.reason, Values: make([]ProjectedValue, len(locators))}
		for i, l := range locators {
			value := ProjectedValue{State: "invalid"}
			if r.reason == "" {
				if s.Envelope == JSONEnvelope {
					value = jsonProjection(jsontext.Value(data[r.offset:r.offset+r.size]), l)
				} else if raw, ok := r.value(l); ok {
					value = textProjection(raw)
				} else {
					value.State = "absent"
				}
			}
			if !budget.take(value) {
				return nil, ErrProjectionLimit
			}
			row.Values[i] = value
		}
		out = append(out, row)
	}
	return out, nil
}
func textProjection(raw []byte) ProjectedValue {
	if !utf8.Valid(raw) {
		return ProjectedValue{State: "unreadable"}
	}
	state := "present"
	if len(raw) == 0 {
		state = "empty"
	}
	return ProjectedValue{State: state, Kind: "string", Text: string(raw)}
}
func jsonProjection(raw jsontext.Value, path Locator) ProjectedValue {
	for _, name := range path {
		var object map[string]jsontext.Value
		if raw.Kind() != '{' || json.Unmarshal(raw, &object) != nil {
			return ProjectedValue{State: "invalid"}
		}
		next, ok := object[name]
		if !ok {
			return ProjectedValue{State: "absent"}
		}
		raw = next
	}
	return jsonScalar(raw, false)
}
func jsonScalar(raw jsontext.Value, item bool) ProjectedValue {
	switch raw.Kind() {
	case 'n':
		return ProjectedValue{State: "null"}
	case '"':
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return ProjectedValue{State: "unreadable"}
		}
		return textProjection([]byte(text))
	case '0':
		return ProjectedValue{State: "present", Kind: "number", Text: string(raw)}
	case 't', 'f':
		return ProjectedValue{State: "present", Kind: "boolean", Text: string(raw)}
	case '[':
		if item {
			return ProjectedValue{State: "invalid"}
		}
		var array []jsontext.Value
		if json.Unmarshal(raw, &array) != nil || len(array) > 1024 {
			return ProjectedValue{State: "invalid"}
		}
		v := ProjectedValue{State: "present", Kind: "array", Items: []ProjectedValue{}}
		for _, a := range array {
			v.Items = append(v.Items, jsonScalar(a, true))
		}
		return v
	}
	return ProjectedValue{State: "invalid"}
}

// XML uses the same raw-character-data reader as import mapping. Namespaces
// are refused until a locator explicitly names them; a local-name collision
// must never select the first element silently. Repeated elements remain ordered.
func (s EnvelopeShape) projectXML(locators []Locator, data []byte) ([]ProjectedRecord, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	type selected struct {
		index, start, depth int
		null                bool
	}
	var path []string
	rows := []ProjectedRecord{}
	var row *ProjectedRecord
	active := []selected{}
	parentSeen := false
	roots := 0
	budget := projectionBudget{}
	completedBudget := projectionBudget{}
	for {
		before := int(decoder.InputOffset())
		token, err := decoder.Token()
		after := int(decoder.InputOffset())
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrDeclaredEnvelope
		}
		switch e := token.(type) {
		case xml.StartElement:
			if len(path) >= maxDocumentDepth || e.Name.Space != "" {
				return nil, ErrDeclaredEnvelope
			}
			if len(path) == 0 {
				roots++
				if roots > 1 {
					return nil, ErrDeclaredEnvelope
				}
			}
			path = append(path, e.Name.Local)
			if slices.Equal(path, s.XML.RecordPath[:len(s.XML.RecordPath)-1]) {
				parentSeen = true
			}
			if slices.Equal(path, s.XML.RecordPath) {
				if row != nil || len(rows) >= MaxEnvelopeRecords {
					return nil, ErrDeclaredEnvelope
				}
				parentSeen = true
				row = &ProjectedRecord{Offset: before, Values: make([]ProjectedValue, len(locators))}
				for i := range row.Values {
					row.Values[i].State = "absent"
				}
			}
			if row != nil && len(path) > len(s.XML.RecordPath) {
				relative := path[len(s.XML.RecordPath):]
				for i, l := range locators {
					if slices.Equal(relative, l) {
						isNull := false
						for _, a := range e.Attr {
							if a.Name.Space == "http://www.w3.org/2001/XMLSchema-instance" && a.Name.Local == "nil" {
								if a.Value != "true" && a.Value != "1" && a.Value != "false" && a.Value != "0" {
									return nil, ErrDeclaredEnvelope
								}
								isNull = a.Value == "true" || a.Value == "1"
							}
						}
						active = append(active, selected{i, after, len(path), isNull})
					}
				}
			}
		case xml.EndElement:
			for i := len(active) - 1; i >= 0; i-- {
				a := active[i]
				if a.depth != len(path) {
					continue
				}
				raw, err := xmlContent(data[a.start:before])
				v := textProjection(raw)
				if err != nil {
					v = ProjectedValue{State: "invalid"}
				}
				if a.null && err == nil {
					if len(raw) != 0 {
						v = ProjectedValue{State: "invalid"}
					} else {
						v = ProjectedValue{State: "null"}
					}
				}
				if !budget.take(v) {
					return nil, ErrProjectionLimit
				}
				previous := row.Values[a.index]
				if previous.State == "absent" {
					row.Values[a.index] = v
				} else if previous.Kind == "array" {
					previous.Items = append(previous.Items, v)
					if len(previous.Items) > 1024 {
						return nil, ErrDeclaredEnvelope
					}
					row.Values[a.index] = previous
				} else {
					row.Values[a.index] = ProjectedValue{State: "present", Kind: "array", Items: []ProjectedValue{previous, v}}
				}
				active = append(active[:i], active[i+1:]...)
			}
			if row != nil && slices.Equal(path, s.XML.RecordPath) {
				row.Size = after - row.Offset
				for _, v := range row.Values {
					if !completedBudget.take(v) {
						return nil, ErrProjectionLimit
					}
				}
				rows = append(rows, *row)
				row = nil
			}
			if len(path) == 0 {
				return nil, ErrDeclaredEnvelope
			}
			path = path[:len(path)-1]
		case xml.CharData:
			if len(path) == 0 && len(bytes.TrimSpace(e)) != 0 {
				return nil, ErrDeclaredEnvelope
			}
		case xml.Directive:
			return nil, ErrDeclaredEnvelope
		}
	}
	if !parentSeen || roots != 1 || row != nil || len(path) != 0 {
		return nil, ErrDeclaredEnvelope
	}
	return rows, nil
}

// DocumentValue selects one strict JSON value without interpreting a protocol.
func DocumentValue(data []byte, path Locator) (ProjectedValue, error) {
	if len(data) > 16<<20 || len(path) < 1 || len(path) > 16 {
		return ProjectedValue{}, ErrDeclaredEnvelope
	}
	d := jsontext.NewDecoder(bytes.NewReader(data))
	raw, err := d.ReadValue()
	if err != nil {
		return ProjectedValue{}, ErrDeclaredEnvelope
	}
	raw = bytes.Clone(raw)
	if _, err = d.ReadValue(); err != io.EOF {
		return ProjectedValue{}, ErrDeclaredEnvelope
	}
	return jsonProjection(raw, path), nil
}

// Projection expansion is bounded independently from source bytes and rows.
const MaxProjectedValues = 100000

var ErrProjectionLimit = errors.New("typed envelope projection exceeds its value budget")

type projectionBudget struct{ values, bytes int }

func (b *projectionBudget) take(v ProjectedValue) bool {
	b.values++
	b.bytes += len(v.Text)
	if b.values > MaxProjectedValues || b.bytes > 16<<20 {
		return false
	}
	for _, item := range v.Items {
		if !b.take(item) {
			return false
		}
	}
	return true
}
