package desktop

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/dictionary"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/hl7reference"
)

// InspectionGridRequest selects a bounded window of the message's visible
// tree. Expansion is presentation state; every row still comes from Navigate.
// A nil request retains the earlier segment projection for existing callers.
type InspectionGridRequest struct {
	Expanded        []string `json:"expanded"`
	ShowOmitted     bool     `json:"show_omitted"`
	Offset          int      `json:"offset"`
	FollowSelection bool     `json:"follow_selection"`
}

const maxGridExpansions = 128

type messageGridPosition struct {
	node   hl7.Node
	parent string
	depth  int
}

type inspectorPathLabels struct {
	doc         *hl7.Document
	message     int
	segments    map[string]int
	repetitions map[string]int
}

func newInspectorPathLabels(doc *hl7.Document, message int) *inspectorPathLabels {
	labels := &inspectorPathLabels{doc: doc, message: message, segments: map[string]int{}, repetitions: map[string]int{}}
	for _, segment := range doc.Messages[message].Segments {
		labels.segments[segment.ID]++
	}
	return labels
}

func (labels *inspectorPathLabels) display(node hl7.Node) string {
	path := node.Path
	if labels.segments[node.Segment] <= 1 {
		path = strings.Replace(path, node.Segment+"[1]", node.Segment, 1)
	}
	if node.Kind != "component" && node.Kind != "subcomponent" {
		return path
	}
	selector, err := hl7.ParseSelector(node.Path)
	if err != nil || selector.Parts().Repetition != 1 {
		return path
	}
	parts := selector.Parts()
	field := parts.Segment + "[" + strconv.Itoa(parts.Occurrence) + "]-" + strconv.Itoa(parts.Field)
	count, ok := labels.repetitions[field]
	if !ok {
		_, repetitions, _ := labels.doc.Navigate(labels.message, field)
		count = len(repetitions)
		labels.repetitions[field] = count
	}
	if count <= 1 {
		path = strings.Replace(path, "-"+strconv.Itoa(parts.Field)+"[1]", "-"+strconv.Itoa(parts.Field), 1)
	}
	return path
}

// messageGridTree folds only single repetitions, never original positions.
// Its cache contains nodes and spans, not copied values or authority.
type messageGridTree struct {
	doc         *hl7.Document
	message     int
	catalog     *hl7reference.Catalog
	selected    hl7.Node
	showOmitted bool
	forced      map[string][]hl7.Node
	branches    map[string][]hl7.Node
	failed      bool
}

func (tree *messageGridTree) children(parent hl7.Node) []hl7.Node {
	if children, ok := tree.branches[parent.Path]; ok {
		return children
	}
	_, children, err := tree.doc.Navigate(tree.message, parent.Path)
	if err != nil {
		tree.failed = true
		return nil
	}
	children = mergeGridNodes(children, tree.forced[parent.Path])
	if tree.showOmitted && tree.catalog != nil {
		children = mergeGridNodes(children, tree.referenceChildren(parent))
	}
	if parent.Kind == "field" {
		if len(children) == 0 {
			selector, err := hl7.ParseSelector(parent.Path)
			if err == nil {
				parts := selector.Parts()
				parts.Repetition = 1
				if path, err := hl7.NewSelector(parts); err == nil {
					child, _, err := tree.doc.Navigate(tree.message, path.String())
					if err == nil {
						children = []hl7.Node{child}
					}
				}
			}
		}
		if len(children) == 1 && !(tree.selected.Kind == "repetition" && tree.selected.Parent == parent.Path) {
			children = tree.children(children[0])
		}
	} else if parent.Kind == "repetition" || parent.Kind == "component" {
		if !inspectorHasCompositeChildren(children, parent, tree.selected, tree.catalog) {
			children = nil
		}
	}
	tree.branches[parent.Path] = children
	return children
}

func (tree *messageGridTree) referenceChildren(parent hl7.Node) []hl7.Node {
	positions := []int{}
	if parent.Kind == "segment" {
		positions = tree.catalog.FieldPositions(tree.catalog.Edition(), parent.Segment)
	} else if parent.Kind == "repetition" || parent.Kind == "component" {
		answer := referenceFor(tree.catalog, parent)
		if answer.DatatypeKey == "" {
			return nil
		}
		for offset := 0; ; offset += InspectorNodeWindow {
			_, records, total, err := tree.catalog.Entity(tree.catalog.Edition(), answer.DatatypeKey, offset, InspectorNodeWindow)
			if err != nil {
				return nil
			}
			for _, record := range records {
				positions = append(positions, record.Position)
			}
			if offset+InspectorNodeWindow >= total {
				break
			}
		}
	} else {
		return nil
	}
	children := make([]hl7.Node, 0, len(positions))
	for _, position := range positions {
		path := ""
		if parent.Kind == "segment" {
			path = parent.Path + "-" + strconv.Itoa(position)
		} else if selector, err := hl7.ParseSelector(parent.Path); err == nil {
			parts := selector.Parts()
			if parent.Kind == "repetition" {
				parts.Component = position
			} else {
				parts.Subcomponent = position
			}
			if child, err := hl7.NewSelector(parts); err == nil {
				path = child.String()
			}
		}
		if path != "" {
			if child, _, err := tree.doc.Navigate(tree.message, path); err == nil {
				children = append(children, child)
			}
		}
	}
	return children
}

func mergeGridNodes(observed, forced []hl7.Node) []hl7.Node {
	if len(forced) == 0 {
		return observed
	}
	all := append([]hl7.Node{}, observed...)
	for _, node := range forced {
		if !slices.ContainsFunc(all, func(n hl7.Node) bool { return n.Path == node.Path }) {
			all = append(all, node)
		}
	}
	// Root segments retain their actual source order. An explicitly addressed
	// absent occurrence is appended as Omitted, without inventing an offset.
	if len(all) == 0 || all[0].Kind == "segment" {
		return all
	}
	type position struct {
		node  hl7.Node
		parts hl7.Parts
	}
	positions := make([]position, len(all))
	for i, node := range all {
		selector, _ := hl7.ParseSelector(node.Path)
		positions[i] = position{node, selector.Parts()}
	}
	sort.SliceStable(positions, func(i, j int) bool {
		a, b := positions[i].parts, positions[j].parts
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Repetition != b.Repetition {
			return a.Repetition < b.Repetition
		}
		if a.Component != b.Component {
			return a.Component < b.Component
		}
		return a.Subcomponent < b.Subcomponent
	})
	for i, position := range positions {
		all[i] = position.node
	}
	return all
}

func attachMessageGrid(view *Inspection, doc *hl7.Document, message int, labels *dictionary.Dictionary, catalog *hl7reference.Catalog, request InspectionGridRequest) string {
	if request.Offset < 0 || len(request.Expanded) > maxGridExpansions {
		return "the message grid request exceeds its position or expansion bound"
	}
	expanded := map[string]bool{}
	pathLabels := newInspectorPathLabels(doc, message)
	view.DisplayPath = pathLabels.display(view.Selected)
	for _, path := range request.Expanded {
		if path == "" || len(path) > 128 {
			return "the message grid contains an unsupported expansion"
		}
		node, _, err := doc.Navigate(message, path)
		if err != nil {
			return "the message grid contains an unsupported expansion"
		}
		expanded[node.Path] = true
	}
	tree := messageGridTree{doc: doc, message: message, catalog: catalog, selected: view.Selected, showOmitted: request.ShowOmitted, forced: map[string][]hl7.Node{}, branches: map[string][]hl7.Node{}}
	ancestors := []string{}
	for node := view.Selected; node.Path != ""; {
		tree.forced[node.Parent] = append(tree.forced[node.Parent], node)
		if node.Parent == "" {
			break
		}
		parent, _, err := doc.Navigate(message, node.Parent)
		if err != nil {
			return "the selected grid position is unavailable"
		}
		ancestors = append(ancestors, parent.Path)
		if request.FollowSelection {
			expanded[parent.Path] = true
		}
		node = parent
	}
	if request.FollowSelection && view.Selected.Kind == "segment" {
		expanded[view.Selected.Path] = true
	}
	if len(expanded) > maxGridExpansions {
		return "collapse a branch before expanding more of the message"
	}
	slices.Reverse(ancestors)
	for _, path := range ancestors {
		node, _, _ := doc.Navigate(message, path)
		parent := describeInspectorNode(doc, message, node, labels, catalog, view.Revealed)
		parent.DisplayPath = pathLabels.display(node)
		view.Parents = append(view.Parents, parent)
	}
	_, segments, err := doc.Navigate(message, "")
	if err != nil {
		return "the message grid is unavailable"
	}
	positions := []messageGridPosition{}
	var walk func(hl7.Node, string, int)
	walk = func(node hl7.Node, parent string, depth int) {
		positions = append(positions, messageGridPosition{node: node, parent: parent, depth: depth})
		if expanded[node.Path] {
			for _, child := range tree.children(node) {
				walk(child, node.Path, depth+1)
			}
		}
	}
	for _, segment := range mergeGridNodes(segments, tree.forced[""]) {
		walk(segment, "", 0)
	}
	selectedOffset := -1
	for i, position := range positions {
		if position.node.Path == view.Selected.Path {
			selectedOffset = i
			break
		}
	}
	offset := min(request.Offset, max(0, len(positions)-1))
	if request.FollowSelection && selectedOffset >= 0 {
		offset = selectedOffset / InspectorNodeWindow * InspectorNodeWindow
	}
	grid := &HL7InspectorGrid{Mode: "message", FollowSelection: request.FollowSelection, Rows: []InspectorNode{}, RowCount: len(positions), Offset: offset, SelectedOffset: selectedOffset, Ancestors: ancestors, ShowOmitted: request.ShowOmitted}
	if selectedOffset >= 0 {
		grid.SelectedParent = positions[selectedOffset].parent
	}
	for path := range expanded {
		grid.Expanded = append(grid.Expanded, path)
	}
	slices.Sort(grid.Expanded)
	for _, position := range positions[offset:min(len(positions), offset+InspectorNodeWindow)] {
		row := describeInspectorNode(doc, message, position.node, labels, catalog, view.Revealed)
		row.DisplayPath = pathLabels.display(position.node)
		row.Depth, row.GridParent, row.HasChildren = position.depth, position.parent, len(tree.children(position.node)) > 0
		row.Expanded = row.HasChildren && expanded[row.Node.Path]
		grid.Rows = append(grid.Rows, row)
	}
	if tree.failed {
		return "the grid branch exceeds the supported syntax limit"
	}
	view.Grid = grid
	return ""
}
