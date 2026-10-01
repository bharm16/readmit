package fhirr4

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"strings"

	"github.com/bharm16/readmit/internal/dataset"
)

// EditPrimitive makes one explicitly selected typed edit. It preserves every
// byte outside that primitive (or its property when removed), including
// unknown clauses and primitive companions. It never reserializes a resource.
func (d *Document) EditPrimitive(ctx context.Context, occurrence string, selector Selector, operator string, value *dataset.Value) (*Document, error) {
	bad := errors.New("this R4 edit requires one supported primitive position and its matching typed value")
	selection := d.Select(ctx, occurrence, selector)
	if selection.State == "invalid" || selection.State == "unsupported" || selection.State == "ambiguous" || len(selection.Readings) != 1 {
		return nil, bad
	}
	reading := selection.Readings[0]
	if !primitive(reading.Datatype) || strings.HasPrefix(selector.Steps[len(selector.Steps)-1].Field, "_") {
		return nil, bad
	}
	start, end := 0, 0
	replacement := []byte{}
	if operator == "set" {
		if value == nil || value.State != "present" || value.Items != nil || value.Type != scalar(reading.Datatype, nil).Type {
			return nil, bad
		}
		if value.Type == "boolean" || value.Type == "decimal" {
			replacement = []byte(value.Text)
		} else {
			var err error
			replacement, err = json.Marshal(value.Text)
			if err != nil {
				return nil, bad
			}
		}
		n, err := parse(ctx, replacement)
		interpreted := scalar(reading.Datatype, n)
		if err != nil || interpreted.State != "present" || value.Precision != "" && value.Precision != interpreted.Precision || value.Timezone != "" && value.Timezone != interpreted.Timezone {
			return nil, bad
		}
		if value.CodeSystem != "" && value.CodeSystem != reading.Value.CodeSystem {
			return nil, bad
		}
		if reading.source != nil {
			start, end = reading.source.start, reading.source.end
		} else {
			parentPointer, key := splitPointer(reading.Pointer)
			parent := d.nodeAt(parentPointer)
			if parent == nil || parent.kind != '{' {
				return nil, bad
			}
			start, end = parent.end-1, parent.end-1
			encodedKey, _ := json.Marshal(key)
			replacement = append(append(encodedKey, ':'), replacement...)
			if len(parent.members) > 0 {
				replacement = append([]byte{','}, replacement...)
			}
		}
	} else if operator == "remove" {
		if value != nil || reading.source == nil || len(reading.Companion) > 0 || selector.Steps[len(selector.Steps)-1].Each || selector.Steps[len(selector.Steps)-1].Index != nil {
			return nil, bad
		}
		parentPointer, key := splitPointer(reading.Pointer)
		parent := d.nodeAt(parentPointer)
		if parent == nil || parent.kind != '{' {
			return nil, bad
		}
		at := -1
		for i, member := range parent.members {
			if member.key == key {
				at = i
				break
			}
		}
		if at < 0 {
			return nil, bad
		}
		// Key bytes start after the previous property's value (or opening
		// brace). Scan only this gap, never inside a string value.
		from := parent.start + 1
		if at > 0 {
			from = parent.members[at-1].value.end
		}
		quote := bytes.IndexByte(d.raw[from:reading.source.start], '"')
		if quote < 0 {
			return nil, bad
		}
		start, end = from+quote, reading.source.end
		if at+1 < len(parent.members) {
			comma := bytes.IndexByte(d.raw[end:parent.members[at+1].value.start], ',')
			if comma < 0 {
				return nil, bad
			}
			end += comma + 1
		} else if at > 0 {
			comma := bytes.IndexByte(d.raw[from:start], ',')
			if comma < 0 {
				return nil, bad
			}
			start = from + comma
		}
	} else {
		return nil, bad
	}
	raw := make([]byte, 0, len(d.raw)-(end-start)+len(replacement))
	raw = append(raw, d.raw[:start]...)
	raw = append(raw, replacement...)
	raw = append(raw, d.raw[end:]...)
	changed, err := Decode(ctx, raw, d.context)
	if err != nil {
		return nil, err
	}
	readback := changed.Select(ctx, occurrence, selector)
	if operator == "set" && (len(readback.Readings) != 1 || readback.Readings[0].Value.State != "present" || readback.Readings[0].Value.Text != value.Text) {
		return nil, bad
	}
	if operator == "remove" && readback.State != "absent" {
		return nil, bad
	}
	return changed, nil
}

func splitPointer(p string) (string, string) {
	at := strings.LastIndex(p, "/")
	if at < 0 {
		return "", ""
	}
	key := strings.ReplaceAll(strings.ReplaceAll(p[at+1:], "~1", "/"), "~0", "~")
	return p[:at], key
}
func (d *Document) nodeAt(pointer string) *node {
	var find func(*node) *node
	find = func(n *node) *node {
		if n == nil {
			return nil
		}
		if n.pointer == pointer {
			return n
		}
		for _, m := range n.members {
			if result := find(m.value); result != nil {
				return result
			}
		}
		for _, item := range n.items {
			if result := find(item); result != nil {
				return result
			}
		}
		return nil
	}
	return find(d.root)
}
