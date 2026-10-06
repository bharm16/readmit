package desktop

import (
	"sort"
	"strconv"
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
	if view.Selected.Field > len(fields) {
		missing, _, err := doc.Navigate(message, segmentPath+"-"+strconv.Itoa(view.Selected.Field))
		if err == nil {
			fields = append(fields, missing)
		}
	}
	grid := &HL7InspectorGrid{Segment: segmentPath, Rows: []InspectorNode{describeInspectorNode(doc, message, segment, labels, catalog, view.Revealed)}, FieldCount: len(fields)}
	if view.Selected.Field > 0 {
		for index, field := range fields {
			if field.Field == view.Selected.Field {
				grid.Offset = index / InspectorNodeWindow * InspectorNodeWindow
				break
			}
		}
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
		selectedParts, selectedErr := hl7.ParseSelector(view.Selected.Path)
		missingRepetition := selectedErr == nil && selectedParts.Parts().Repetition > len(repetitions)
		if missingRepetition {
			for _, repetition := range inspectorBranchWindow(repetitions, field.Path, view) {
				add(repetition, 2)
			}
			parts := selectedParts.Parts()
			parts.Component, parts.Subcomponent = 0, 0
			if path, err := hl7.NewSelector(parts); err == nil {
				chosen, _, _ = doc.Navigate(message, path.String())
			}
			if chosen.Path != "" {
				add(chosen, 2)
			}
		} else if len(repetitions) == 1 {
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
		if !inspectorHasCompositeChildren(components, chosen, view.Selected, catalog) {
			continue
		}
		if len(repetitions) == 1 && view.Selected.Kind == "field" && len(components) > InspectorNodeWindow {
			add(chosen, 2)
		}
		for _, component := range inspectorGridBranch(doc, message, chosen, components, view, catalog) {
			add(component, 2)
			if view.Selected.Path != component.Path && !strings.HasPrefix(view.Selected.Path, component.Path+".") {
				continue
			}
			_, subcomponents, err := doc.Navigate(message, component.Path)
			if err != nil {
				continue
			}
			if !inspectorHasCompositeChildren(subcomponents, component, view.Selected, catalog) {
				continue
			}
			for _, sub := range inspectorGridBranch(doc, message, component, subcomponents, view, catalog) {
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
	// Parse each Go-owned position once. Large valid composites must not pay
	// quadratic regex/allocation work before their bounded page is selected.
	type position struct {
		node  hl7.Node
		parts hl7.Parts
	}
	ordered := make([]position, len(nodes))
	for i, node := range nodes {
		selector, _ := hl7.ParseSelector(node.Path)
		ordered[i] = position{node: node, parts: selector.Parts()}
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i].parts, ordered[j].parts
		if a.Component != b.Component {
			return a.Component < b.Component
		}
		return a.Subcomponent < b.Subcomponent
	})
	for i, row := range ordered {
		nodes[i] = row.node
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

// A scalar's first syntactic component is the same value, not another branch.
// Composite metadata or actual separators supply meaningful child positions.
func inspectorHasCompositeChildren(children []hl7.Node, parent, selected hl7.Node, catalog *hl7reference.Catalog) bool {
	if strings.HasPrefix(selected.Path, parent.Path+".") {
		return true
	}
	if len(children) > 1 {
		return true
	}
	if len(children) == 1 && (children[0].Start != parent.Start || children[0].End != parent.End || selected.Path == children[0].Path || strings.HasPrefix(selected.Path, children[0].Path+".")) {
		return true
	}
	if catalog != nil {
		answer := referenceFor(catalog, parent)
		if answer.DatatypeKey != "" {
			_, _, count, err := catalog.Entity(catalog.Edition(), answer.DatatypeKey, 0, InspectorNodeWindow)
			return err == nil && count > 0
		}
	}
	return false
}

// Every expanded ancestor owns its reference children, independently of the
// selected descendant's datatype. The parser supplies omitted states and spans.
func inspectorGridBranch(doc *hl7.Document, message int, parent hl7.Node, children []hl7.Node, view *Inspection, catalog *hl7reference.Catalog) []hl7.Node {
	observed := map[string]hl7.Node{}
	for _, node := range children {
		observed[node.Path] = node
	}
	selector, err := hl7.ParseSelector(parent.Path)
	if err != nil {
		return inspectorBranchWindow(children, parent.Path, view)
	}
	base := selector.Parts()
	addPosition := func(position int) {
		parts := base
		if parts.Component == 0 {
			parts.Component = position
		} else {
			parts.Subcomponent = position
		}
		path, err := hl7.NewSelector(parts)
		if err != nil {
			return
		}
		node, _, err := doc.Navigate(message, path.String())
		if err == nil {
			observed[node.Path] = node
		}
	}
	if catalog != nil {
		answer := referenceFor(catalog, parent)
		if answer.DatatypeKey != "" {
			_, records, _, err := catalog.Entity(catalog.Edition(), answer.DatatypeKey, 0, InspectorNodeWindow)
			if err == nil {
				for _, record := range records {
					addPosition(record.Position)
				}
			}
		}
	}
	// An explicitly addressed missing descendant stays at its parent and depth,
	// even when no reference catalog defines it.
	if strings.HasPrefix(view.Selected.Path, parent.Path+".") {
		if selected, err := hl7.ParseSelector(view.Selected.Path); err == nil {
			if parent.Kind == "repetition" {
				addPosition(selected.Parts().Component)
			} else if parent.Kind == "component" {
				addPosition(selected.Parts().Subcomponent)
			}
		}
	}
	ordered := make([]hl7.Node, 0, len(observed))
	for _, node := range observed {
		ordered = append(ordered, node)
	}
	sortNodes(ordered)
	return inspectorBranchWindow(ordered, parent.Path, view)
}

// Message identity and bounded segment navigation belong to every inspection,
// including a directly restored descendant, independently of reference visits.
func attachInspectorMessage(view *Inspection, doc *hl7.Document, message int, labels *dictionary.Dictionary, catalog *hl7reference.Catalog) {
	_, segments, err := doc.Navigate(message, "")
	if err != nil {
		return
	}
	view.SegmentCount = len(segments)
	for _, segment := range segments[:min(len(segments), InspectorNodeWindow)] {
		view.Segments = append(view.Segments, describeInspectorNode(doc, message, segment, labels, catalog, false))
	}
	if !view.Revealed {
		return
	}
	control, _, err := doc.Navigate(message, "MSH[1]-10")
	if err != nil {
		return
	}
	row := describeInspectorNode(doc, message, control, labels, nil, true)
	if control.State == hl7.Present && !row.Truncated && row.DecodeState == "decoded" {
		view.ControlID = row.Value
	}
}
