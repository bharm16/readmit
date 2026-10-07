package desktop

import (
	"context"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/hl7reference"
	"github.com/bharm16/readmit/internal/profileeval"
)

// ReadReferenceCatalog verifies an explicit local catalog and returns only its
// bounded identity/coverage. No definition library is shipped to the webview.
func (a *App) ReadReferenceCatalog(path string) ReferenceCatalogResult {
	return runRead(a, false, func(context.Context) ReferenceCatalogResult {
		catalog, err := hl7reference.Read(path)
		if err != nil {
			return ReferenceCatalogResult{State: Failed, Reason: err.Error()}
		}
		answer := catalog.Summary()
		return ReferenceCatalogResult{State: Completed, Reference: &answer}
	})
}

type ReferenceCatalogResult struct {
	State     State                `json:"state"`
	Reason    string               `json:"reason,omitzero"`
	Reference *hl7reference.Answer `json:"reference,omitzero"`
}

func inspectionReference(path string, identity string) (*hl7reference.Catalog, hl7reference.Answer) {
	if path == "" {
		return nil, hl7reference.Answer{Status: "not_selected", Reason: "Choose an HL7 version to view definitions. Original message inspection remains available."}
	}
	c, err := hl7reference.Read(path)
	if err != nil {
		return nil, hl7reference.Answer{Status: "not_available", Reason: err.Error()}
	}
	if identity != "" && c.Summary().Identity != identity {
		return nil, hl7reference.Answer{Status: "not_available", Identity: identity, Reason: "The selected reference catalog changed; select its new identity explicitly. Raw evidence remains inspectable."}
	}
	return c, c.Summary()
}
func referenceFor(c *hl7reference.Catalog, node hl7.Node) hl7reference.Answer {
	// An explicit catalog selects its edition; the actual message declaration is
	// retained independently in FieldMetadata and never rewritten.
	version := c.Edition()
	if node.Kind == "message" || node.Kind == "occurrence" {
		return c.Summary()
	}
	if (node.Kind == "component" || node.Kind == "subcomponent") && c.Schema() == hl7reference.Schema {
		a := c.Summary()
		a.Status = "not_available"
		a.Reason = "This catalog covers segments and fields; component definitions are not available."
		return a
	}
	if node.Kind == "segment" {
		return c.Lookup(version, node.Segment, 0)
	}
	selector, err := hl7.ParseSelector(node.Path)
	if err != nil {
		a := c.Summary()
		a.Status = "not_available"
		a.Reason = "The selected reference path is unavailable."
		return a
	}
	return c.LookupValue(version, selector.Parts())
}

func (r *ReferenceCatalogResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// HL7ReferenceRequest binds a bounded drilldown to the exact catalog bytes the
// inspector showed. The frontend retains the evidence/selection Back context.
type HL7ReferenceRequest struct {
	Catalog   string                        `json:"catalog"`
	Identity  string                        `json:"identity"`
	Edition   string                        `json:"edition"`
	Key       string                        `json:"key"`
	Offset    int                           `json:"offset"`
	Limit     int                           `json:"limit"`
	Query     string                        `json:"query,omitzero"`
	Attribute hl7reference.PreviewAttribute `json:"attribute,omitzero"`
}
type HL7ReferenceResult struct {
	State      State                 `json:"state"`
	Reason     string                `json:"reason,omitzero"`
	Reference  *hl7reference.Answer  `json:"reference,omitzero"`
	Children   []hl7reference.Record `json:"children"`
	Offset     int                   `json:"offset"`
	ChildCount int                   `json:"child_count"`
	TotalCount int                   `json:"total_count"`
	Preview    *hl7reference.Preview `json:"preview,omitzero"`
}

func (r *HL7ReferenceResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// LookupHL7Reference serves local reference content only. A changed catalog is
// refused; it cannot silently mix a new definition with a retained selection.
func (a *App) LookupHL7Reference(request HL7ReferenceRequest) HL7ReferenceResult {
	return runRead(a, false, func(context.Context) HL7ReferenceResult {
		catalog, err := hl7reference.Read(request.Catalog)
		if err != nil {
			return HL7ReferenceResult{State: Failed, Reason: err.Error()}
		}
		if request.Identity == "" || catalog.Summary().Identity != request.Identity {
			return HL7ReferenceResult{State: Failed, Reason: "The selected reference catalog changed; select it again."}
		}
		limit := request.Limit
		if limit == 0 {
			limit = 100
		}
		if request.Attribute != "" {
			preview, answer, children, count, total, err := catalog.AttributePreview(request.Edition, request.Key, request.Attribute, request.Offset, limit, request.Query)
			if err != nil {
				return HL7ReferenceResult{State: Failed, Reason: err.Error()}
			}
			return HL7ReferenceResult{State: Completed, Reference: &answer, Preview: &preview, Children: children, Offset: request.Offset, ChildCount: count, TotalCount: total}
		}
		answer, children, count, total, err := catalog.EntitySearch(request.Edition, request.Key, request.Offset, limit, request.Query)
		if err != nil {
			return HL7ReferenceResult{State: Failed, Reason: err.Error()}
		}
		return HL7ReferenceResult{State: Completed, Reference: &answer, Children: children, Offset: request.Offset, ChildCount: count, TotalCount: total}
	})
}

// HL7ReferenceValue is a bounded projection for a sourced datatype card. Its
// selector and spans come from the parser, and its texts obey current reveal
// consent. It never changes the inspector's original selection.
type HL7ReferenceValue struct {
	Key         string   `json:"key"`
	Node        hl7.Node `json:"node"`
	Encoded     string   `json:"encoded"`
	Decoded     string   `json:"decoded"`
	DecodeState string   `json:"decode_state"`
	Truncated   bool     `json:"truncated"`
}

func attachReferenceValues(view *Inspection, doc *hl7.Document, message int, catalog *hl7reference.Catalog) {
	if view.Reference == nil || view.Reference.Record == nil {
		return
	}
	selector, err := hl7.ParseSelector(view.Selected.Path)
	if err != nil {
		return
	}
	if view.Selected.Kind == "field" && view.ChildCount > 1 {
		view.ReferenceValuesNotice = "Choose a field repetition to show component values."
		return
	}
	parts := selector.Parts()
	seen := map[string]bool{}
	add := func(key string, base hl7.Parts) {
		if key == "" {
			return
		}
		_, records, _, err := catalog.Entity(view.Reference.Edition, key, 0, 100)
		if err != nil {
			return
		}
		for _, record := range records {
			if seen[record.Key] {
				continue
			}
			source := base
			if source.Component == 0 {
				source.Component = record.Position
			} else {
				source.Subcomponent = record.Position
			}
			path, err := hl7.NewSelector(source)
			if err != nil {
				continue
			}
			node, _, err := doc.Navigate(message, path.String())
			if err != nil {
				continue
			}
			shown := HL7ReferenceValue{Key: record.Key, Node: node}
			if view.Revealed {
				value := &Inspection{Selected: node}
				describeValue(value, doc, message)
				shown.DecodeState = value.DecodeState
				shown.Decoded, shown.Truncated = childValue(doc, message, node)
				raw := doc.Bytes(hl7.Span{Start: node.Start, End: node.End})
				shown.Encoded = escapeBytes(raw[:min(len(raw), InspectorChildValueLimit)])
				shown.Truncated = shown.Truncated || len(raw) > InspectorChildValueLimit
			}
			view.ReferenceValues = append(view.ReferenceValues, shown)
			seen[record.Key] = true
		}
	}
	add(view.Reference.DatatypeKey, parts)
	if view.Reference.ParentDatatypeKey != "" {
		if view.Selected.Kind == "subcomponent" {
			parts.Subcomponent = 0
		} else {
			parts.Component, parts.Subcomponent = 0, 0
		}
		add(view.Reference.ParentDatatypeKey, parts)
	}
}

func attachMessageContext(view *Inspection, doc *hl7.Document, message int, catalog *hl7reference.Catalog) {
	selector, _ := hl7.ParseSelector("MSH[1]-9[1].3")
	selected, _, err := doc.Navigate(message, selector.String())
	if err != nil {
		return
	}
	declared := ""
	if selected.State == hl7.Present {
		raw := doc.Bytes(hl7.Span{Start: selected.Start, End: selected.End})
		if len(raw) <= 128 {
			declared = escapeBytes(raw)
		}
	}
	context := catalog.MessageContext(catalog.Edition(), view.MessageCode, view.TriggerEvent, declared, selected.State)
	if context.StructureKey != "" && view.Selected.Segment != "" {
		parts, err := hl7.ParseSelector(view.Selected.Path)
		occurrence := 1
		if err == nil {
			occurrence = parts.Parts().Occurrence
		} else {
			segmentPath, _, parseErr := doc.Navigate(message, view.Selected.Path)
			if parseErr == nil {
				for _, s := range doc.Messages[message].Segments {
					if s.ID == segmentPath.Segment && s.Span.Start < segmentPath.Start {
						occurrence++
					}
				}
			}
		}
		index := -1
		count := 0
		for i, s := range doc.Messages[message].Segments {
			if s.ID == view.Selected.Segment {
				count++
				if count == occurrence {
					index = i
					break
				}
			}
		}
		context.Placement = profileeval.LocateStructure(catalog.Sequence(context.StructureKey), doc.Messages[message].Segments, index)
	}
	view.MessageContext = &context
}
