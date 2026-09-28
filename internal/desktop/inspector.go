package desktop

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/dictionary"
	"github.com/bharm16/readmit/internal/hl7"
)

// InspectorByteWindow is how many original bytes one inspector window shows,
// as InspectorByteWindow/HexRowBytes rows. InspectorNodeWindow bounds the
// children one window lists.
const InspectorByteWindow = 256
const InspectorNodeWindow = 100
const inspectorValueLimit = 4096

// InspectorRawWindow is how many original bytes of the whole message one Raw
// window shows as escaped text.
const InspectorRawWindow = 4096

// HexRowBytes is the width of one hex row everywhere the window shows original
// bytes.
const HexRowBytes = 16

// InspectRequest inspects one occurrence in a verified case. Identity binds the
// list selection to the evidence it displayed. Negative ByteOffset means jump
// to the selected part; a nonnegative one pages original bytes and is read
// from the start of the hex row that holds it. RawOffset pages the Raw text
// the same way: -1 is the window holding the start of the selected part, and
// a nonnegative one is read from the start of the Raw window that holds it.
// Reveal asks for the selected value, the Raw text and the printable column of
// the hex rows; without it, the inspection carries positions, states, labels
// and hex but no value text.
type InspectRequest struct {
	Workspace  string `json:"workspace"`
	Case       string `json:"case"`
	Identity   string `json:"identity"`
	Occurrence string `json:"occurrence"`
	Path       string `json:"path"`
	NodeOffset int    `json:"node_offset"`
	ByteOffset int    `json:"byte_offset"`
	RawOffset  int    `json:"raw_offset"`
	Reveal     bool   `json:"reveal"`
}

// RawWindow is one window of the whole message's original bytes as escaped
// text: bytes [Offset, End) of the occurrence (or file), within the message's
// own bytes [MessageStart, MessageEnd), which for an occurrence nothing could
// parse are all of its bytes. Windows begin every InspectorRawWindow bytes
// from MessageStart, and a byte is escaped whole, so a window never splits an
// escape. The text is divided where the selected part begins and ends within
// the window, so Selected is exactly the part to mark; it is empty, and the
// window is all Before, when the selection is the whole message, has no bytes
// or lies outside the window.
type RawWindow struct {
	Offset       int    `json:"offset"`
	End          int    `json:"end"`
	MessageStart int    `json:"message_start"`
	MessageEnd   int    `json:"message_end"`
	Before       string `json:"before"`
	Selected     string `json:"selected"`
	After        string `json:"after"`
}

// HexRow is one row of original bytes: its offset, up to HexRowBytes bytes as
// two-digit lowercase hex in two groups of eight, and one character per byte
// in which printable ASCII other than an HTML metacharacter stands for itself
// and every other byte is a dot. Text is empty unless values were revealed.
type HexRow struct {
	Offset int    `json:"offset"`
	Hex    string `json:"hex"`
	Text   string `json:"text"`
}

// FieldMetadata is what the bundled labels say about the selected position.
type FieldMetadata struct {
	Label      string `json:"label"`
	Status     string `json:"status"`
	HL7Version string `json:"hl7_version"`
	Contract   string `json:"contract"`
	Provenance string `json:"provenance"`
}

// InspectorNode is one child of the selection: the tree node, its dictionary
// field label where the bundled labels name it, the canonical selector a field
// filter names it by (empty for a segment), and a segment's readable name.
type InspectorNode struct {
	Node        hl7.Node `json:"node"`
	Label       string   `json:"label"`
	Selector    string   `json:"selector"`
	SegmentName string   `json:"segment_name"`
	// Value is the child's decoded text, escaped, only when values were
	// revealed and the child is a present field or part of one that decodes.
	// It is bounded to InspectorChildValueLimit bytes; Truncated says so.
	Value     string `json:"value"`
	Truncated bool   `json:"truncated"`
}

// InspectorChildValueLimit bounds the value shown beside each child row. The
// whole value is the selection's own Decoded once the child is selected.
const InspectorChildValueLimit = 128

// Inspection is an ephemeral read view, never an evidence artifact. Offsets are
// zero-based half-open ranges within the original occurrence (or file),
// including MLLP. Add SourceOffset to locate the same byte in the captured
// source. Values are escaped in Go before entering the webview; original bytes
// remain untouched. Raw, Decoded and RawWindow are present only when values
// were revealed: Raw is the selected part's own escaped bytes, which Copy
// value copies, and RawWindow the whole message's. SourceName is the name
// declared for the occurrence's source, empty when nothing names it, and
// Direction the direction the case recorded for it; a standalone file has
// neither.
type Inspection struct {
	Metadata     FieldMetadata    `json:"metadata"`
	Identity     string           `json:"identity"`
	Occurrence   string           `json:"occurrence"`
	Message      int              `json:"message"`
	SourceID     string           `json:"source_id"`
	SourceName   string           `json:"source_name"`
	Direction    bundle.Direction `json:"direction,omitzero"`
	SourceOffset int              `json:"source_offset"`
	Size         int              `json:"size"`
	MessageCode  string           `json:"message_code"`
	TriggerEvent string           `json:"trigger_event"`
	ObservedAt   *time.Time       `json:"observed_at"`
	Selected     hl7.Node         `json:"selected"`
	Selector     string           `json:"selector"`
	SegmentName  string           `json:"segment_name"`
	Children     []InspectorNode  `json:"children"`
	NodeOffset   int              `json:"node_offset"`
	ChildCount   int              `json:"child_count"`
	Bytes        []HexRow         `json:"bytes"`
	ByteOffset   int              `json:"byte_offset"`
	Revealed     bool             `json:"revealed"`
	Raw          string           `json:"raw"`
	RawWindow    *RawWindow       `json:"raw_window,omitzero"`
	Decoded      string           `json:"decoded"`
	Encoding     string           `json:"encoding"`
	DecodeState  string           `json:"decode_state"`
	Notice       string           `json:"notice"`
}

type InspectionResult struct {
	State      State       `json:"state"`
	Reason     string      `json:"reason,omitzero"`
	Inspection *Inspection `json:"inspection,omitzero"`
}

func (r *InspectionResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// InspectOccurrence verifies the case on every read and serves bounded tree
// and byte windows. Like ReadMessages it runs under the operation slot to
// completion and is not interruptible; the shell must not offer cancellation
// for this read.
func (a *App) InspectOccurrence(request InspectRequest) InspectionResult {
	return runRead(a, false, func(context.Context) InspectionResult {
		return a.inspectOccurrence(request)
	})
}

func (a *App) inspectOccurrence(request InspectRequest) InspectionResult {
	fail := func(reason string) InspectionResult { return InspectionResult{State: Failed, Reason: reason} }
	if request.NodeOffset < 0 || request.ByteOffset < -1 || request.RawOffset < -1 {
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
	var doc *hl7.Document
	if event.Kind != bundle.Unparsed {
		var options hl7.Options
		for _, source := range opened.Manifest.Sources {
			if source.ID == event.SourceID {
				options = hl7.Options{Format: source.Format, Terminator: event.Terminator}
				break
			}
		}
		if doc, err = hl7.Parse(raw, options); err != nil || len(doc.Messages) != 1 {
			return fail("the occurrence could not be parsed consistently with its case")
		}
	}
	view, reason := inspectDocument(raw, doc, 0, inspectorWindow{Path: request.Path, NodeOffset: request.NodeOffset, ByteOffset: request.ByteOffset,
		RawOffset: request.RawOffset, Reveal: request.Reveal})
	if view == nil {
		return fail(reason)
	}
	view.Identity, view.Occurrence, view.SourceID, view.SourceOffset = opened.Identity, event.ID, event.SourceID, event.Offset
	view.SourceName, view.Direction = sourceNames(root, request.Case, opened)[event.SourceID], event.Direction
	view.ObservedAt = event.ObservedAt
	return InspectionResult{State: Completed, Inspection: view}
}

// inspectorWindow is the part of an inspection request that selects within
// one parsed message: the tree path, the child, byte and Raw windows, and
// whether values are revealed.
type inspectorWindow struct {
	Path       string
	NodeOffset int
	ByteOffset int
	RawOffset  int
	Reveal     bool
}

// inspectDocument is the one inspector over one message of parsed original
// bytes, for a case occurrence and a standalone file alike. A nil document is
// an occurrence nothing could parse: it has original bytes and no tree. The
// reason is returned with a nil inspection when the window is refused.
func inspectDocument(raw []byte, doc *hl7.Document, message int, window inspectorWindow) (*Inspection, string) {
	view := &Inspection{Message: message, Size: len(raw), Children: []InspectorNode{}, Bytes: []HexRow{}, Revealed: window.Reveal,
		Selected: hl7.Node{Kind: "occurrence", State: hl7.Present, End: len(raw)}, DecodeState: "unparsed",
		Notice: "The occurrence could not be parsed; original bytes remain available."}
	if doc != nil {
		if message < 0 || message >= len(doc.Messages) {
			return nil, "the selected message is not one this file holds"
		}
		selected, children, err := doc.Navigate(message, window.Path)
		if err != nil {
			return nil, "the inspector path is unsupported or exceeds the syntax limit"
		}
		if window.NodeOffset > len(children) {
			return nil, "the tree window is outside the selected branch"
		}
		view.Selected, view.Selector, view.SegmentName = selected, nodeSelector(selected), dictionary.SegmentName(selected.Segment)
		view.MessageCode, view.TriggerEvent = messageType(doc, message)
		labels := labelsFor(doc, message)
		view.Metadata = fieldMetadata(doc, message, selected)
		view.ChildCount = len(children)
		view.NodeOffset = window.NodeOffset
		for _, child := range children[window.NodeOffset:min(len(children), window.NodeOffset+InspectorNodeWindow)] {
			described := InspectorNode{Node: child, Selector: nodeSelector(child), SegmentName: dictionary.SegmentName(child.Segment)}
			if labels != nil && child.Kind != "segment" {
				described.Label = labels.At(dictionary.Position{Kind: child.Kind, Segment: child.Segment, Field: child.Field}).Label
			}
			if window.Reveal {
				described.Value, described.Truncated = childValue(doc, message, child)
			}
			view.Children = append(view.Children, described)
		}
		describeValue(view, doc, message)
		if !window.Reveal {
			view.Raw, view.Decoded = "", ""
		}
	} else if window.Path != "" || window.NodeOffset != 0 {
		return nil, "an unparsed occurrence has no selectable field tree"
	}
	offset := window.ByteOffset
	if offset == -1 {
		offset = view.Selected.Start
	}
	if offset > len(raw) {
		return nil, "the byte window is outside the occurrence"
	}
	view.ByteOffset = offset - offset%HexRowBytes
	view.Bytes = hexRows(raw, view.ByteOffset, InspectorByteWindow, window.Reveal)
	bounds := hl7.Span{End: len(raw)}
	if doc != nil {
		bounds = doc.Messages[message].Span
	}
	if window.Reveal {
		shown, ok := rawWindow(raw, bounds, view.Selected, window.RawOffset)
		if !ok {
			return nil, "the Raw window is outside the message"
		}
		view.RawWindow = shown
	}
	return view, ""
}

// rawWindow is the Raw window of the message's bytes that offset asks for:
// the one holding the selection's start for -1, else the one holding offset,
// where an offset before the message, such as its MLLP start block, is its
// first window.
func rawWindow(raw []byte, message hl7.Span, selected hl7.Node, offset int) (*RawWindow, bool) {
	if offset == -1 {
		offset = min(max(selected.Start, message.Start), message.End)
	}
	if offset > message.End {
		return nil, false
	}
	offset = max(offset, message.Start)
	start := message.Start + (offset-message.Start)/InspectorRawWindow*InspectorRawWindow
	end := min(message.End, start+InspectorRawWindow)
	markFrom, markTo := end, end
	if selected.Kind != "message" && selected.Kind != "occurrence" && selected.End > selected.Start {
		markFrom, markTo = min(max(selected.Start, start), end), min(max(selected.End, start), end)
	}
	if markFrom == markTo {
		markFrom, markTo = end, end
	}
	return &RawWindow{Offset: start, End: end, MessageStart: message.Start, MessageEnd: message.End,
		Before: escapeBytes(raw[start:markFrom]), Selected: escapeBytes(raw[markFrom:markTo]), After: escapeBytes(raw[markTo:end])}, true
}

// hexRows renders at most limit original bytes from offset, which is the
// start of a row, as rows of HexRowBytes. The printable column is left empty
// unless reveal is set, and so is the hex of each row: the digits are the values.
func hexRows(raw []byte, offset, limit int, reveal bool) []HexRow {
	rows := []HexRow{}
	end := min(len(raw), offset+limit)
	for start := offset; start < end; start += HexRowBytes {
		chunk := raw[start:min(end, start+HexRowBytes)]
		var hexed, text strings.Builder
		for i, b := range chunk {
			switch {
			case i == 8:
				hexed.WriteString("  ")
			case i > 0:
				hexed.WriteByte(' ')
			}
			fmt.Fprintf(&hexed, "%02x", b)
			if b >= 32 && b < 127 && b != '<' && b != '>' && b != '&' {
				text.WriteByte(b)
			} else {
				text.WriteByte('.')
			}
		}
		// The hex digits are the values themselves, so they are withheld
		// with the printable text until values are revealed.
		row := HexRow{Offset: start}
		if reveal {
			row.Hex, row.Text = hexed.String(), text.String()
		}
		rows = append(rows, row)
	}
	return rows
}

// nodeSelector is the canonical selector a field filter names a node by: a
// field is its first repetition, and a message or segment has none.
func nodeSelector(node hl7.Node) string {
	path := node.Path
	switch node.Kind {
	case "field":
		path += "[1]"
	case "repetition", "component", "subcomponent":
	default:
		return ""
	}
	selector, err := hl7.ParseSelector(path)
	if err != nil || selector.String() != path {
		return ""
	}
	return path
}

// messageType is the escaped, bounded MSH-9 code and trigger of one message.
func messageType(doc *hl7.Document, message int) (string, string) {
	code, _ := hl7.ParseSelector("MSH-9.1")
	trigger, _ := hl7.ParseSelector("MSH-9.2")
	return typePart(doc, message, code), typePart(doc, message, trigger)
}

// labelsFor is the bundled labels when they apply to this message, else nil.
func labelsFor(doc *hl7.Document, message int) *dictionary.Dictionary {
	labels, err := dictionary.Load()
	if err != nil || !labels.Applies(dictionary.Declared(doc, message)) {
		return nil
	}
	return labels
}

func describeValue(view *Inspection, doc *hl7.Document, message int) {
	selected := view.Selected
	m := doc.Messages[message]
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
	if _, readable := doc.CharacterSet(message); !readable {
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
	reading, _ := doc.ReadNode(message, selected, hl7.EnforceMSH18)
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

// childValue decodes one present child the way describeValue decodes the
// selection, and answers nothing for anything that is not a readable value.
func childValue(doc *hl7.Document, message int, node hl7.Node) (string, bool) {
	if node.Kind == "segment" || node.Kind == "message" || node.State != hl7.Present {
		return "", false
	}
	if _, readable := doc.CharacterSet(message); !readable {
		return "", false
	}
	reading, _ := doc.ReadNode(message, node, hl7.EnforceMSH18)
	decoded, ok := reading.Text()
	if !ok || reading.Reason == hl7.UnsupportedEscape {
		return "", false
	}
	truncated := false
	if len(decoded) > InspectorChildValueLimit {
		decoded, truncated = strings.ToValidUTF8(decoded[:InspectorChildValueLimit], ""), true
	}
	quoted := strconv.QuoteToASCII(decoded)
	return strings.NewReplacer("<", `\x3c`, ">", `\x3e`, "&", `\x26`).Replace(quoted[1 : len(quoted)-1]), truncated
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

// fieldMetadata asks the one label module what applies here: the declared
// version is read once through the shared selector, and the label, status and
// provenance of the selected position are the module's answer, so the window
// reports a position exactly as the command line does.
func fieldMetadata(doc *hl7.Document, message int, selected hl7.Node) FieldMetadata {
	declared := dictionary.Declared(doc, message)
	metadata := FieldMetadata{Status: dictionary.StatusUnsupportedVersion}
	// A declaration is shown escaped and bounded even when labels are unsupported.
	metadata.HL7Version = "declaration exceeds the 128-byte metadata display limit"
	if len(declared.Version) <= 128 {
		metadata.HL7Version = escapeBytes([]byte(declared.Version))
	}
	labels, err := dictionary.Load()
	if err != nil {
		metadata.Status = dictionary.StatusUnavailable
		return metadata
	}
	if !labels.Applies(declared) {
		return metadata
	}
	metadata.Contract = labels.Contract
	metadata.Provenance = dictionary.Provenance
	answer := labels.At(dictionary.Position{Kind: selected.Kind, Segment: selected.Segment, Field: selected.Field})
	metadata.Status = answer.Status
	metadata.Label = answer.Label
	return metadata
}
