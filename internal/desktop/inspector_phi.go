package desktop

import (
	"bytes"
	"strings"

	"github.com/bharm16/readmit/internal/hl7"
)

// PHI masking is a presentation aid, not de-identification. These positions
// hold patient/contact identifiers, providers, accounts, or free text. Unknown
// custom segments are masked conservatively. Structural separators and byte
// offsets are retained; the original source document is never modified.
var inspectorPHIFields = map[string][]int{
	"PID": {2, 3, 4, 5, 6, 7, 9, 10, 11, 12, 13, 14, 18, 19, 20, 21, 23, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40},
	"PD1": {3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 14, 15, 16, 17, 18, 19, 20, 21},
	"PV1": {3, 5, 6, 7, 8, 9, 11, 17, 19, 37, 39, 42, 43, 44, 45, 50, 52},
	"PV2": {3, 4, 5, 8, 9, 12, 13, 16, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50},
	"ORC": {2, 3, 4, 9, 10, 11, 12, 13, 14, 15, 19, 21, 22, 23, 24},
	"OBR": {2, 3, 7, 8, 9, 10, 13, 14, 16, 17, 18, 19, 20, 21, 22, 28, 31, 32, 33, 34, 35},
	"OBX": {5, 11, 12, 14, 15, 16, 18, 19, 23, 24, 25},
	"NTE": {3}, "SCH": {1, 2, 6, 7, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22},
	"EVN": {2, 3, 5, 6}, "MSH": {8},
}

func inspectorPHIField(segment string, field int) bool {
	switch segment {
	case "NK1", "GT1", "IN1", "IN2", "IN3", "ROL", "PRT", "CON", "IAM", "AL1", "DG1", "TXA", "ACC":
		return field > 1
	}
	if strings.HasPrefix(segment, "Z") {
		return true
	}
	for _, position := range inspectorPHIFields[segment] {
		if field == position {
			return true
		}
	}
	return false
}

func maskInspectorPHI(raw []byte, doc *hl7.Document) []byte {
	masked := bytes.Clone(raw)
	if doc == nil {
		for i, b := range masked {
			if b != '\r' && b != '\n' && b != 0x0b && b != 0x1c {
				masked[i] = '*'
			}
		}
		return masked
	}
	// Hex pages may cross message boundaries: protect every message in this file.
	for _, current := range doc.Messages {
		marker := byte('*')
		for _, candidate := range []byte("*#x?") {
			d := current.Delimiters
			if candidate != d.Field && candidate != d.Component && candidate != d.Repetition && candidate != d.Subcomponent && candidate != d.Escape {
				marker = candidate
				break
			}
		}
		delimiter := func(b byte) bool {
			return b == current.Delimiters.Component || b == current.Delimiters.Repetition || b == current.Delimiters.Subcomponent
		}
		for _, segment := range current.Segments {
			for _, field := range segment.Fields {
				if field.State != hl7.Present || !inspectorPHIField(segment.ID, field.Number) {
					continue
				}
				for i := field.Span.Start; i < field.Span.End; i++ {
					b := masked[i]
					if delimiter(b) {
						continue
					}
					if b == '"' && i+1 < field.Span.End && masked[i+1] == '"' && (i == field.Span.Start || delimiter(masked[i-1])) && (i+2 == field.Span.End || delimiter(masked[i+2])) {
						i++
						continue
					}
					masked[i] = marker
				}
			}
		}
	}
	return masked
}

// Values are projected through the original parser's spans. Never reparse the
// masked display: custom delimiters, escaped separators and null components
// cannot change the evidence's tree, states, decode diagnostics, or selection.
func maskInspectorValues(view *Inspection, raw []byte) {
	text := func(node hl7.Node, limit int) string {
		if node.Start < 0 || node.End > len(raw) || node.End < node.Start {
			return ""
		}
		return escapeBytes(raw[node.Start:min(node.End, node.Start+limit)])
	}
	protect := func(node hl7.Node) bool {
		return node.State == hl7.Present && (node.Kind == "segment" || node.Kind == "message" || inspectorPHIField(node.Segment, node.Field))
	}
	row := func(node *InspectorNode) {
		if !protect(node.Node) {
			return
		}
		if node.Raw != "" {
			node.Raw = text(node.Node, InspectorChildValueLimit)
		}
		if node.Value != "" {
			node.Value = text(node.Node, InspectorChildValueLimit)
		}
	}
	for i := range view.Children {
		row(&view.Children[i])
	}
	for i := range view.Parents {
		row(&view.Parents[i])
	}
	if view.Grid != nil {
		for i := range view.Grid.Rows {
			row(&view.Grid.Rows[i])
		}
	}
	for i := range view.ReferenceValues {
		value := &view.ReferenceValues[i]
		if protect(value.Node) {
			if value.Encoded != "" {
				value.Encoded = text(value.Node, InspectorChildValueLimit)
			}
			if value.Decoded != "" {
				value.Decoded = text(value.Node, InspectorChildValueLimit)
			}
		}
	}
	if protect(view.Selected) {
		if view.Raw != "" {
			view.Raw = text(view.Selected, inspectorValueLimit)
		}
		if view.Decoded != "" {
			view.Decoded = text(view.Selected, inspectorValueLimit)
		}
	}
}
