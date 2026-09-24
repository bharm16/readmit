package desktop

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/dictionary"
	"github.com/bharm16/readmit/internal/hl7"
)

const InspectorByteWindow = 256
const InspectorNodeWindow = 100
const inspectorValueLimit = 4096

// InspectRequest explicitly reveals one occurrence in a verified case. Identity
// binds the grid selection to the evidence it displayed. Negative ByteOffset
// means jump to the selected part; nonnegative offsets page original bytes.
type InspectRequest struct {
	Workspace  string `json:"workspace"`
	Case       string `json:"case"`
	Identity   string `json:"identity"`
	Occurrence string `json:"occurrence"`
	Path       string `json:"path"`
	NodeOffset int    `json:"node_offset"`
	ByteOffset int    `json:"byte_offset"`
}

type InspectorByte struct {
	Offset   int    `json:"offset"`
	Hex      string `json:"hex"`
	Text     string `json:"text"`
	Selected bool   `json:"selected"`
}

// Inspection is an ephemeral read view, never an evidence artifact. Offsets are
// zero-based half-open ranges within the original occurrence, including MLLP.
// Add SourceOffset to locate the same byte in the captured source. Values are
// escaped in Go before entering the webview; original bytes remain untouched.
type FieldMetadata struct {
	Label      string `json:"label"`
	Status     string `json:"status"`
	HL7Version string `json:"hl7_version"`
	Contract   string `json:"contract"`
	Provenance string `json:"provenance"`
}

type Inspection struct {
	Metadata     FieldMetadata   `json:"metadata"`
	Identity     string          `json:"identity"`
	Occurrence   string          `json:"occurrence"`
	SourceID     string          `json:"source_id"`
	SourceOffset int             `json:"source_offset"`
	Size         int             `json:"size"`
	Selected     hl7.Node        `json:"selected"`
	Children     []hl7.Node      `json:"children"`
	NodeOffset   int             `json:"node_offset"`
	ChildCount   int             `json:"child_count"`
	Bytes        []InspectorByte `json:"bytes"`
	ByteOffset   int             `json:"byte_offset"`
	Raw          string          `json:"raw"`
	Decoded      string          `json:"decoded"`
	Encoding     string          `json:"encoding"`
	DecodeState  string          `json:"decode_state"`
	Notice       string          `json:"notice"`
}

type InspectionResult struct {
	State      State       `json:"state"`
	Reason     string      `json:"reason,omitzero"`
	Inspection *Inspection `json:"inspection,omitzero"`
}

func (r *InspectionResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// InspectOccurrence verifies the case on every read and serves bounded tree
// and byte windows. Like OpenGrid it runs under the operation slot to completion
// and is not interruptible; the shell must not offer cancellation for this read.
func (a *App) InspectOccurrence(request InspectRequest) InspectionResult {
	return run(a, false, false, func(context.Context) InspectionResult {
		return a.inspectOccurrence(request)
	})
}

func (a *App) inspectOccurrence(request InspectRequest) InspectionResult {
	fail := func(reason string) InspectionResult { return InspectionResult{State: Failed, Reason: reason} }
	if request.NodeOffset < 0 || request.ByteOffset < -1 {
		return fail("inspector offsets must be in range")
	}
	root, opened, declined := openedCase(request.Workspace, request.Case, request.Identity)
	if root == "" {
		return InspectionResult{State: declined.state, Reason: declined.reason}
	}
	var event *bundle.Event
	for i := range opened.Events {
		if opened.Events[i].ID == request.Occurrence {
			event = &opened.Events[i]
			break
		}
	}
	if event == nil {
		return fail("the selected occurrence does not belong to this case")
	}
	raw, err := opened.Raw(event.ID)
	if err != nil {
		return fail("the occurrence bytes are unavailable")
	}
	view := &Inspection{Identity: opened.Identity, Occurrence: event.ID, SourceID: event.SourceID, SourceOffset: event.Offset, Size: len(raw), Children: []hl7.Node{}, Bytes: []InspectorByte{}, Selected: hl7.Node{Kind: "occurrence", State: hl7.Present, End: len(raw)}, DecodeState: "unparsed", Notice: "The occurrence could not be parsed; original bytes remain available."}
	if event.Kind != bundle.Unparsed {
		var options hl7.Options
		for _, source := range opened.Manifest.Sources {
			if source.ID == event.SourceID {
				options = hl7.Options{Format: source.Format, Terminator: event.Terminator}
				break
			}
		}
		doc, err := hl7.Parse(raw, options)
		if err != nil {
			return fail("the occurrence could not be parsed consistently with its case")
		}
		selected, children, err := doc.Navigate(0, request.Path)
		if err != nil {
			return fail("the inspector path is unsupported or exceeds the syntax limit")
		}
		view.Selected = selected
		view.Metadata = fieldMetadata(doc, selected)
		view.ChildCount = len(children)
		if request.NodeOffset > len(children) {
			return fail("the tree window is outside the selected branch")
		}
		view.NodeOffset = request.NodeOffset
		view.Children = children[request.NodeOffset:min(len(children), request.NodeOffset+InspectorNodeWindow)]
		describeValue(view, doc)
	} else if request.Path != "" || request.NodeOffset != 0 {
		return fail("an unparsed occurrence has no selectable field tree")
	}
	offset := request.ByteOffset
	if offset == -1 {
		offset = view.Selected.Start
	}
	if offset > len(raw) {
		return fail("the byte window is outside the occurrence")
	}
	view.ByteOffset = offset
	for i := offset; i < min(len(raw), offset+InspectorByteWindow); i++ {
		view.Bytes = append(view.Bytes, InspectorByte{Offset: i, Hex: fmt.Sprintf("%02x", raw[i]), Text: escapeBytes(raw[i : i+1]), Selected: view.Selected.State != hl7.Omitted && i >= view.Selected.Start && i < view.Selected.End})
	}
	return InspectionResult{State: Completed, Inspection: view}
}

func describeValue(view *Inspection, doc *hl7.Document) {
	selected := view.Selected
	m := doc.Messages[0]
	charset := m.Segments[0].Field(18)
	declared := doc.Bytes(charset.Span)
	view.Encoding = "declaration exceeds the 128-byte metadata display limit"
	if len(declared) <= 128 {
		view.Encoding = escapeBytes(declared)
	}
	if charset.State == hl7.Omitted || charset.State == hl7.Empty {
		view.Encoding = "ASCII (default)"
	}
	view.Notice = ""
	if selected.State == hl7.Omitted {
		view.DecodeState = "omitted"
		return
	}
	raw := doc.Bytes(hl7.Span{Start: selected.Start, End: selected.End})
	if len(raw) > inspectorValueLimit {
		view.DecodeState = "too_large"
		view.Notice = "The selected value exceeds the 4096-byte display limit; navigate every original byte in the byte window."
		return
	}
	view.Raw = escapeBytes(raw)
	if _, readable := doc.CharacterSet(0); !readable {
		view.DecodeState = "unsupported_encoding"
		view.Notice = "Character-set transcoding is unsupported; original bytes remain available."
		return
	}
	if selected.Kind == "message" || selected.Kind == "segment" {
		view.DecodeState = "structural"
		view.Notice = "Choose a field or value to decode escapes."
		return
	}
	if selected.State == hl7.Null || selected.State == hl7.Empty {
		view.DecodeState = string(selected.State)
		return
	}
	// The inspector holds a value to the character set MSH-18 declares. The
	// node is a field or a part of one that Navigate returned for this message.
	reading, _ := doc.ReadNode(0, selected, hl7.EnforceMSH18)
	decoded, ok := reading.Text()
	switch {
	case reading.Reason == hl7.UnsupportedEscape:
		view.DecodeState = "unsupported_escape"
		view.Notice = "The selected value contains an unsupported or invalid HL7 escape."
		return
	case !ok:
		view.DecodeState = "invalid_encoding"
		view.Notice = "The selected bytes do not match the declared character set."
		return
	}
	view.DecodeState = "decoded"
	quoted := strconv.QuoteToASCII(decoded)
	view.Decoded = strings.NewReplacer("<", `\x3c`, ">", `\x3e`, "&", `\x26`).Replace(quoted[1 : len(quoted)-1])
}

func escapeBytes(raw []byte) string {
	var out strings.Builder
	for _, b := range raw {
		if b >= 32 && b < 127 && b != '\\' && b != '<' && b != '>' && b != '&' {
			out.WriteByte(b)
		} else {
			fmt.Fprintf(&out, `\x%02x`, b)
		}
	}
	return out.String()
}

// The bundled dictionary supplies field labels only. It does not supply
// segment names, datatypes, cardinality, clinical meaning or conformance.
func fieldMetadata(doc *hl7.Document, selected hl7.Node) FieldMetadata {
	metadata := FieldMetadata{Status: "unsupported_version"}
	versionSelector, _ := hl7.ParseSelector("MSH-12.1")
	version, _ := doc.Select(0, versionSelector)
	versionBytes := doc.Bytes(version.Span)
	// A declaration is shown escaped and bounded even when labels are unsupported.
	metadata.HL7Version = "declaration exceeds the 128-byte metadata display limit"
	if len(versionBytes) <= 128 {
		metadata.HL7Version = escapeBytes(versionBytes)
	}
	labels, err := dictionary.Load()
	if err != nil {
		metadata.Status = "unavailable"
		return metadata
	}
	if string(versionBytes) != labels.HL7Version {
		return metadata
	}
	metadata.Contract = labels.Contract
	metadata.Provenance = "nHapi 2495edd1e23a85ab9146cb03947c17d45120cf1f; MPL-2.0; docs/dictionary-provenance.md"
	metadata.Status = "unlabeled_position"
	if selected.Kind == "message" {
		metadata.Status = "field_labels_only"
		return metadata
	}
	fields, ok := labels.Segments[selected.Segment]
	if !ok {
		metadata.Status = "unsupported_segment"
		return metadata
	}
	if selected.Kind == "segment" {
		metadata.Status = "field_labels_only"
		return metadata
	}
	if label, ok := fields[selected.Field]; ok {
		metadata.Label = label
		metadata.Status = "labeled_field"
	}
	return metadata
}
