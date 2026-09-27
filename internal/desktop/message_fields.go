package desktop

import (
	"context"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/dictionary"
)

// MaxMessageFields bounds the fields one MessageFields answer lists.
const MaxMessageFields = 2000

// MessageFieldsRequest names one verified case, bound to the identity the
// window displayed.
type MessageFieldsRequest struct {
	Workspace string `json:"workspace"`
	Case      string `json:"case"`
	Identity  string `json:"identity"`
}

// MessageField is one field position some message of the case holds: the
// segment and its readable name, the field number, the bundled label where
// the labels apply to a message that holds it (else empty, and the position
// is its name), and the canonical selector a field rule names it by. It
// carries no value and no state.
type MessageField struct {
	Segment     string `json:"segment"`
	SegmentName string `json:"segment_name"`
	Field       int    `json:"field"`
	Label       string `json:"label"`
	Selector    string `json:"selector"`
}

// MessageFieldsResult lists the fields the case's parsed messages hold, each
// once, in the order they first occur. Complete is false when the case holds
// more than MaxMessageFields and the list stops there; a field not listed can
// still be named by its selector.
type MessageFieldsResult struct {
	State    State          `json:"state"`
	Reason   string         `json:"reason,omitzero"`
	Fields   []MessageField `json:"fields"`
	Complete bool           `json:"complete"`
}

func (r *MessageFieldsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// MessageFields lists the field positions the verified case's messages
// actually hold, for the filter's field picker. No value is read into the
// answer and nothing is written. It holds the operation slot but is not
// interruptible.
func (a *App) MessageFields(request MessageFieldsRequest) MessageFieldsResult {
	return run(a, false, false, func(context.Context) MessageFieldsResult {
		result := MessageFieldsResult{Fields: []MessageField{}}
		root, opened, declined := openedCase(request.Workspace, request.Case, request.Identity)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		result.State, result.Complete = Completed, true
		listed := map[string]int{}
		parsed := false
		for _, event := range opened.Events {
			if event.ParseError != "" || event.Kind == bundle.Unparsed {
				continue
			}
			document, err := opened.Document(event)
			if err != nil {
				continue
			}
			parsed = true
			labels := labelsFor(document, 0)
			_, segments, err := document.Navigate(0, "")
			if err != nil {
				continue
			}
			for _, segment := range segments {
				_, fields, err := document.Navigate(0, segment.Path)
				if err != nil {
					continue
				}
				for _, field := range fields {
					selector := nodeSelector(field)
					if selector == "" {
						continue
					}
					label := ""
					if labels != nil {
						label = labels.At(dictionary.Position{Kind: field.Kind, Segment: field.Segment, Field: field.Field}).Label
					}
					if at, ok := listed[selector]; ok {
						// A later message the labels apply to names a field
						// an earlier one left positional.
						if result.Fields[at].Label == "" {
							result.Fields[at].Label = label
						}
						continue
					}
					if len(result.Fields) == MaxMessageFields {
						result.Complete = false
						return result
					}
					listed[selector] = len(result.Fields)
					result.Fields = append(result.Fields, MessageField{Segment: field.Segment, SegmentName: dictionary.SegmentName(field.Segment),
						Field: field.Field, Label: label, Selector: selector})
				}
			}
		}
		if !parsed {
			result.State, result.Reason = Empty, "this case holds no parsed message"
		}
		return result
	})
}
