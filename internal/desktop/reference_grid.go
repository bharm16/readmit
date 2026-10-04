package desktop

import (
	"strings"

	"github.com/bharm16/readmit/internal/dictionary"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/hl7reference"
)

// HL7InspectorGrid is independent of detail-tree navigation. It retains a
// bounded segment's sibling fields while the selected branch is expanded.
// All paths, states, spans, decoded views and labels originate in Go owners.
type HL7InspectorGrid struct {
	Segment    string          `json:"segment"`
	Rows       []InspectorNode `json:"rows"`
	Offset     int             `json:"offset"`
	FieldCount int             `json:"field_count"`
}

func describeInspectorNode(doc *hl7.Document, message int, node hl7.Node, labels *dictionary.Dictionary, catalog *hl7reference.Catalog, reveal bool) InspectorNode {
	described := InspectorNode{Node: node, Selector: nodeSelector(node), SegmentName: dictionary.SegmentName(node.Segment)}
	if labels != nil && (node.Kind == "field" || node.Kind == "repetition") {
		described.Label = labels.At(dictionary.Position{Kind: node.Kind, Segment: node.Segment, Field: node.Field}).Label
	}
	if catalog != nil {
		answer := referenceFor(catalog, node)
		described.Reference, described.ReferenceStatus = answer.Record, answer.Status
		if answer.Record != nil {
			described.Label = answer.Record.Name
			if node.Kind == "segment" {
				described.SegmentName = answer.Record.Name
			}
		}
	}
	if reveal {
		described.Value, described.Truncated = childValue(doc, message, node)
		value := &Inspection{Selected: node}
		describeValue(value, doc, message)
		described.DecodeState = value.DecodeState
		raw := doc.Bytes(hl7.Span{Start: node.Start, End: node.End})
		described.Raw = escapeBytes(raw[:min(len(raw), InspectorChildValueLimit)])
		described.Truncated = described.Truncated || len(raw) > InspectorChildValueLimit
	}
	return described
}
func attachInspectorGrid(view *Inspection, doc *hl7.Document, message int, labels *dictionary.Dictionary, catalog *hl7reference.Catalog) {
	if view.Selected.Path == "" {
		view.Grid = &HL7InspectorGrid{Rows: append([]InspectorNode{}, view.Children...), Offset: view.NodeOffset, FieldCount: view.ChildCount}
		return
	}
	segmentPath, _, _ := strings.Cut(view.Selected.Path, "-")
	segment, fields, err := doc.Navigate(message, segmentPath)
	if err != nil {
		return
	}
	grid := &HL7InspectorGrid{Segment: segmentPath, Rows: []InspectorNode{describeInspectorNode(doc, message, segment, labels, catalog, view.Revealed)}, FieldCount: len(fields)}
	if view.Selected.Field > 0 {
		grid.Offset = (view.Selected.Field - 1) / InspectorNodeWindow * InspectorNodeWindow
	} else {
		grid.Offset = view.NodeOffset
	}
	grid.Offset = min(grid.Offset, len(fields))
	add := func(node hl7.Node, depth int) {
		row := describeInspectorNode(doc, message, node, labels, catalog, view.Revealed)
		row.Depth = depth
		grid.Rows = append(grid.Rows, row)
	}
	for _, field := range fields[grid.Offset:min(len(fields), grid.Offset+InspectorNodeWindow)] {
		add(field, 1)
		if field.Field != view.Selected.Field {
			continue
		}
		_, repetitions, err := doc.Navigate(message, field.Path)
		if err != nil {
			continue
		}
		chosen := hl7.Node{}
		if len(repetitions) == 1 {
			chosen = repetitions[0]
			if view.Selected.Kind == "repetition" {
				add(chosen, 2)
			}
		} else {
			for _, repetition := range inspectorBranchWindow(repetitions, field.Path, view) {
				add(repetition, 2)
				if view.Selected.Path == repetition.Path || strings.HasPrefix(view.Selected.Path, repetition.Path+".") {
					chosen = repetition
				}
			}
		}
		if chosen.Path == "" {
			continue
		}
		_, components, err := doc.Navigate(message, chosen.Path)
		if err != nil {
			continue
		}
		if len(repetitions) == 1 && view.Selected.Kind == "field" && len(components) > InspectorNodeWindow {
			add(chosen, 2)
		}
		// Catalogue-known omitted components remain explicit, never fabricated present.
		observed := map[string]hl7.Node{}
		for _, node := range inspectorBranchWindow(components, chosen.Path, view) {
			observed[node.Path] = node
		}
		for _, value := range view.ReferenceValues {
			if value.Node.Parent == chosen.Path && value.Node.State == hl7.Omitted && view.NodeOffset == 0 {
				observed[value.Node.Path] = value.Node
			}
		}
		ordered := []hl7.Node{}
		for _, node := range observed {
			ordered = append(ordered, node)
		}
		sortNodes(ordered)
		for _, component := range ordered {
			add(component, 2)
			if view.Selected.Path != component.Path && !strings.HasPrefix(view.Selected.Path, component.Path+".") {
				continue
			}
			_, subcomponents, err := doc.Navigate(message, component.Path)
			if err != nil {
				continue
			}
			for _, sub := range inspectorBranchWindow(subcomponents, component.Path, view) {
				add(sub, 3)
			}
		}
	}
	// An explicitly selected omitted descendant may have no observed sibling
	// window. Keep the exact Go-owned node instead of losing its selection.
	selectedVisible := false
	for _, row := range grid.Rows {
		if row.Node.Path == view.Selected.Path {
			selectedVisible = true
			break
		}
	}
	if !selectedVisible && view.Selected.Kind != "segment" {
		add(view.Selected, 2)
	}
	view.Grid = grid
}
func sortNodes(nodes []hl7.Node) {
	// Parser positions give stable numeric order even when omitted nodes have no span.
	for i := 1; i < len(nodes); i++ {
		for j := i; j > 0; j-- {
			left, _ := hl7.ParseSelector(nodes[j-1].Path)
			right, _ := hl7.ParseSelector(nodes[j].Path)
			if left.Parts().Component <= right.Parts().Component {
				break
			}
			nodes[j-1], nodes[j] = nodes[j], nodes[j-1]
		}
	}
}

// Only the selected branch uses NodeOffset. Ancestors page to the actual
// selected descendant so later occurrences remain visible and selectable.
func inspectorBranchWindow(nodes []hl7.Node, parent string, view *Inspection) []hl7.Node {
	offset := 0
	if parent == view.Selected.Path {
		offset = view.NodeOffset
	} else {
		for i, node := range nodes {
			if node.Path == view.Selected.Path || strings.HasPrefix(view.Selected.Path, node.Path+".") {
				offset = i / InspectorNodeWindow * InspectorNodeWindow
				break
			}
		}
	}
	offset = min(offset, len(nodes))
	return nodes[offset:min(len(nodes), offset+InspectorNodeWindow)]
}
