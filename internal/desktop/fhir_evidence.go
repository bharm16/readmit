package desktop

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirevidence"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
)

// FHIRInspection is the existing reader's protocol-specific view. Every
// selector, state and local reference target comes from the Go R4 interpreter.
// Narrative and attachments are visible only in escaped original JSON.
type FHIRInspection struct {
	FieldsLimited bool                     `json:"fields_limited,omitzero"`
	Declaration   fhirevidence.Declaration `json:"declaration"`
	Resources     []fhirr4.Resource        `json:"resources"`
	Fields        []FHIRFieldView          `json:"fields"`
	Selected      *FHIRFieldView           `json:"selected,omitzero"`
	References    []fhirr4.Relationship    `json:"references"`
	Findings      []fhirr4.Finding         `json:"findings"`
}
type FHIRFieldView struct {
	Field     fhirr4.ProjectionField `json:"field"`
	Selection fhirr4.Selection       `json:"selection"`
	Preset    *FHIRCheckPreset       `json:"preset,omitzero"`
}

// FHIRCheckPreset is a complete finite projection and typed check binding.
// Expected is absent until reveal or explicit author entry supplies a value.
// The source pins the document's context and original bytes, not a URL lookup.
type FHIRCheckPreset struct {
	ValueType  string                     `json:"value_type"`
	CodeSystem string                     `json:"code_system,omitzero"`
	Repeated   bool                       `json:"repeated"`
	Projection fhirr4.Projection          `json:"projection"`
	Binding    assertion.DatasetBinding   `json:"binding"`
	Assertion  assertion.DatasetAssertion `json:"assertion"`
}

const unsupportedFHIRResourceCaption = "Unsupported R4 resource"

// An unrecognized resourceType spelling is source content, which may contain
// private data. Passive captions use only the fixed R4 type vocabulary.
func fhirResourceCaption(resourceType string) string {
	if !fhirr4.KnownResourceType(resourceType) {
		return unsupportedFHIRResourceCaption
	}
	return resourceType
}

func openedFHIRCase(ctx context.Context, workspace, entry, identity string) (string, *fhirevidence.Artifact, bool, refusal) {
	root, declined := resolveFolder(workspace)
	if root == "" {
		return "", nil, false, declined
	}
	path, err := artifactpath.Child(root, entry)
	if err != nil {
		return "", nil, false, refusal{Failed, "a case must be one directory entry of the open workspace"}
	}
	if !regular(filepath.Join(path, fhirevidence.ManifestName)) {
		return "", nil, false, refusal{}
	}
	opened, err := fhirevidence.Open(ctx, path)
	if err != nil {
		return "", nil, true, refusal{Failed, "the retained R4 evidence cannot be verified as complete and unmodified"}
	}
	if identity == "" || opened.Identity != identity {
		return "", nil, true, refusal{Failed, "the case identity changed; reopen the case"}
	}
	return root, opened, true, refusal{}
}

type fhirMessageFacts struct {
	resources map[string]fhirr4.Resource
	source    *fhirevidence.Artifact
	name      string
}

func (f fhirMessageFacts) Type(r index.Record) grid.MessageType {
	return grid.MessageType{Kind: bundle.Message, Code: f.resources[r.ID].Type}
}
func (f fhirMessageFacts) ControlID(index.Record) []byte  { return nil }
func (f fhirMessageFacts) SourceName(index.Record) string { return f.name }
func (f fhirMessageFacts) Content(r index.Record) []byte {
	if f.source.Document == nil || f.resources[r.ID].Pointer == "" {
		return f.source.Raw()
	}
	raw, _ := f.source.Document.ResourceBytes(r.ID)
	return raw
}

func readFHIRMessages(ctx context.Context, request MessagesRequest) (MessagesResult, bool) {
	root, source, handled, declined := openedFHIRCase(ctx, request.Workspace, request.Case, request.Identity)
	if !handled {
		return MessagesResult{}, false
	}
	if root == "" {
		return refusedMessages(declined.state, declined.reason), true
	}
	limit := request.Limit
	if limit == 0 {
		limit = grid.MaxRows
	}
	if len(request.Query.Fields)+len(request.Query.AckCodes) != 0 {
		return refusedMessages(Failed, "R4 fields use typed FHIR selectors; v2 field and acknowledgement filters cannot read this case"), true
	}
	if err := grid.ValidateQuery(request.Query); err != nil {
		return refusedMessages(Failed, err.Error()), true
	}
	facts := fhirMessageFacts{resources: map[string]fhirr4.Resource{}, source: source, name: recordedFHIRSourceName(root, request.Case)}
	resources := []fhirr4.Resource{}
	if source.Document != nil {
		resources = source.Document.Resources()
	}
	if source.Document == nil {
		resources = append(resources, fhirr4.Resource{Occurrence: "request", Type: "FHIR request", State: "parsed"})
	}
	document := index.Document{Records: []index.Record{}}
	facets := MessageFacets{Types: []grid.MessageType{}, Sources: []SourceFacet{{ID: fhirevidence.SourceID, Name: facts.name}}, AckCodes: []string{}}
	wanted := map[string]bool{}
	for _, id := range request.Occurrences {
		if wanted[id] {
			return refusedMessages(Failed, "each referenced resource is named once"), true
		}
		wanted[id] = true
	}
	if len(wanted) > grid.MaxRows {
		return refusedMessages(Failed, "too many referenced resources are requested"), true
	}
	for i, resource := range resources {
		if source.Document != nil {
			resource.Type = fhirResourceCaption(resource.Type)
		}
		facts.resources[resource.Occurrence] = resource
		start, end := 0, source.Manifest.Size
		if source.Document != nil {
			start, end, _ = source.Document.ResourceSpan(resource.Occurrence)
			if resource.Pointer == "" {
				start, end = 0, source.Manifest.Size
			}
		}
		record := index.Record{ID: resource.Occurrence, SourceID: fhirevidence.SourceID, Sequence: i + 1, Offset: start, Size: end - start, Kind: bundle.Message, Direction: bundle.Unknown, Values: []index.Value{}}
		if resource.State != "parsed" {
			record.ParseError = resource.State
		}
		document.Records = append(document.Records, record)
		typ := facts.Type(record)
		if !slices.Contains(facets.Types, typ) {
			facets.Types = append(facets.Types, typ)
		}
	}
	query, offset, sort := request.Query, request.Offset, request.Sort
	total := len(document.Records)
	if len(wanted) > 0 {
		query, offset, sort, limit = grid.Query{}, 0, grid.EvidenceOrder, grid.MaxRows
		document.Records = slices.DeleteFunc(document.Records, func(r index.Record) bool { return !wanted[r.ID] })
	}
	page, err := grid.SelectQuery(document, time.Now().UTC(), query, facts, sort, grid.Window{Offset: offset, Limit: limit})
	if err != nil {
		return refusedMessages(Failed, err.Error()), true
	}
	result := MessagesResult{State: Completed, Protocol: "fhir-r4", Rows: []MessageRow{}, Total: total, Matched: page.Matched, Undecodable: page.Undecodable, Complete: true, Scanned: total, Facets: facets}
	for _, record := range page.Rows {
		if len(wanted) > 0 && !wanted[record.ID] {
			continue
		}
		resource := facts.resources[record.ID]
		result.Rows = append(result.Rows, MessageRow{ID: record.ID, SourceID: record.SourceID, SourceName: facts.name, Sequence: record.Sequence, Offset: record.Offset, Size: record.Size, Kind: record.Kind, Direction: record.Direction, Decoded: resource.State == "parsed", MessageCode: resource.Type, Protocol: "fhir-r4", ResourceType: resource.Type, ResourceState: resource.State})
	}
	if len(wanted) > 0 {
		if len(result.Rows) != len(wanted) {
			return refusedMessages(Failed, "a referenced resource is not in this case window"), true
		}
		result.Matched = len(result.Rows)
	}
	return result, true
}

func recordedFHIRSourceName(root, entry string) string {
	if p, err := openProjectFolder(root); p != nil && err.state == "" {
		for _, c := range p.Document.Cases {
			if c.Name == entry {
				return c.SourceNamed(fhirevidence.SourceID)
			}
		}
	}
	return ""
}

func inspectFHIR(ctx context.Context, identity, occurrence string, declaration fhirevidence.Declaration, raw []byte, doc *fhirr4.Document, window inspectorWindow) InspectionResult {
	fail := func(reason string) InspectionResult { return InspectionResult{State: Failed, Reason: reason} }
	if window.NodeOffset < 0 || window.ByteOffset < -1 || window.RawOffset < -1 {
		return fail("inspector offsets must be in range")
	}
	view := &Inspection{Identity: identity, Occurrence: occurrence, SourceID: fhirevidence.SourceID, Revealed: window.Reveal, Children: []InspectorNode{}, Bytes: []HexRow{}, Encoding: "UTF-8", DecodeState: "decoded", Metadata: FieldMetadata{Contract: fhirevidence.Schema, HL7Version: fhirr4.Version}}
	fhir := &FHIRInspection{Declaration: declaration, Resources: []fhirr4.Resource{}, Fields: []FHIRFieldView{}, References: []fhirr4.Relationship{}, Findings: []fhirr4.Finding{}}
	view.FHIR = fhir
	selectedRaw := raw
	resource := fhirr4.Resource{Occurrence: "request", Type: "FHIR request"}
	if doc != nil {
		fhir.Resources, fhir.Findings = doc.Resources(), doc.Findings()
		found := false
		for _, r := range fhir.Resources {
			if r.Occurrence == occurrence {
				resource, found = r, true
				break
			}
		}
		if !found {
			return fail("the selected resource does not belong to this evidence")
		}
		view.SourceOffset, _, _ = doc.ResourceSpan(occurrence)
		if resource.Pointer == "" {
			selectedRaw = raw
			view.SourceOffset = 0
		} else {
			selectedRaw, _ = doc.ResourceBytes(occurrence)
		}
		fields, complete := doc.InspectionFields(ctx, occurrence)
		fhir.FieldsLimited = !complete
		view.ChildCount = len(fields)
		sourceIdentity := doc.Identity()
		row, position := "", 0
		for _, r := range fhir.Resources {
			if r.Type != resource.Type {
				continue
			}
			position++
			if r.Occurrence == occurrence {
				row = fmt.Sprintf("row-%06d", position)
				break
			}
		}
		for i, field := range fields {
			if (i < window.NodeOffset || i >= window.NodeOffset+InspectorNodeWindow) && field.ID != window.Path {
				continue
			}
			shown := FHIRFieldView{Field: field, Selection: doc.Select(ctx, occurrence, field.Selector)}
			if shown.Selection.State != "invalid" && shown.Selection.State != "unsupported" && shown.Selection.State != "ambiguous" {
				shown.Preset = fhirCheckPreset(sourceIdentity, row, resource, field, shown.Selection, window.Reveal)
			}
			if !window.Reveal {
				hideFHIRSelection(&shown.Selection)
			}
			if field.ID == window.Path {
				copied := shown
				fhir.Selected = &copied
			}
			if i >= window.NodeOffset && i < window.NodeOffset+InspectorNodeWindow {
				fhir.Fields = append(fhir.Fields, shown)
			}
		}
		if window.Path != "" && fhir.Selected == nil {
			return fail("the selected R4 field is not a supported typed selector")
		}
		fhir.References = doc.Relationships(ctx, occurrence)
	} else if occurrence != "request" || window.Path != "" {
		return fail("an empty-body R4 request has no resource field tree")
	}
	view.Size, view.MessageCode = len(selectedRaw), resource.Type
	if doc != nil {
		view.MessageCode = fhirResourceCaption(resource.Type)
	}
	view.NodeOffset = window.NodeOffset
	view.Notice = "Finite local R4 interpretation; profile validation and workflow correctness are separate. External references are not fetched."
	if fhir.FieldsLimited {
		view.Notice += " The concrete field listing reached its limit; all original bytes remain retained."
	}
	offset := max(0, window.ByteOffset)
	if offset > len(selectedRaw) {
		return fail("the byte window is outside the resource")
	}
	view.ByteOffset = offset - offset%HexRowBytes
	view.Bytes = hexRows(selectedRaw, view.ByteOffset, InspectorByteWindow, window.Reveal)
	if window.Reveal {
		view.RawWindow, _ = rawWindow(selectedRaw, hl7.Span{End: len(selectedRaw)}, hl7.Node{End: len(selectedRaw)}, window.RawOffset)
		if view.RawWindow == nil {
			return fail("the Raw window is outside the resource")
		}
		view.Raw = escapeBytes(selectedRaw[:min(len(selectedRaw), InspectorRawWindow)])
	} else {
		fhir.Declaration.Context.Base = ""
		if declaration.Request != nil {
			request := *declaration.Request
			request.URL = ""
			request.Headers = fhirevidence.RequestDeclaration{}.Headers
			fhir.Declaration.Request = &request
		}
		for i := range fhir.Resources {
			r := &fhir.Resources[i]
			r.Type = fhirResourceCaption(r.Type)
			r.Base, r.LogicalID, r.VersionID, r.FullURL, r.CanonicalURL, r.CanonicalVersion = "", "", "", "", "", ""
			for j := range r.Identifiers {
				r.Identifiers[j] = fhirr4.BusinessID{}
			}
		}
		for i := range fhir.References {
			fhir.References[i].Reference = ""
		}
		for i := range fhir.Findings {
			fhir.Findings[i].Pointer = ""
		}
	}
	return InspectionResult{State: Completed, Inspection: view}
}

func hideFHIRSelection(selection *fhirr4.Selection) {
	for i := range selection.Readings {
		r := &selection.Readings[i]
		r.Raw, r.Companion, r.Canonical = nil, nil, nil
		r.Value.Text, r.Value.Precision, r.Value.Timezone = "", "", ""
		r.Value.Items = nil
	}
}

func fhirCheckPreset(sourceIdentity, row string, resource fhirr4.Resource, field fhirr4.ProjectionField, selection fhirr4.Selection, reveal bool) *FHIRCheckPreset {
	projection := fhirr4.Projection{Schema: fhirr4.ProjectionSchema, ResourceType: resource.Type, Columns: []fhirr4.Column{{Name: "value", Selector: field.Selector, Repeated: field.Repeated}}, MaxRows: fhirr4.MaxResources, MaxValues: fhirr4.MaxNodes}
	if row == "" {
		return nil
	}
	data, _ := json.Marshal(projection, json.Deterministic(true))
	check := assertion.DatasetAssertion{ID: "field-check", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "evidence", Row: row, Where: []assertion.RowFilter{}}, Column: "value"}
	if reveal && len(selection.Readings) == 1 && dataset.ValidExpected(selection.Readings[0].Value) {
		expected := selection.Readings[0].Value
		check.Expected = &expected
	}
	if field.Repeated {
		check.Operator, check.Expected = "sequence-equals", nil
		if reveal {
			check.Sequence = []dataset.Value{}
			for _, reading := range selection.Readings {
				if !dataset.ValidExpected(reading.Value) {
					return nil
				}
				check.Sequence = append(check.Sequence, reading.Value)
			}
		}
	}
	return &FHIRCheckPreset{ValueType: field.Type, CodeSystem: field.CodeSystem, Repeated: field.Repeated, Projection: projection, Binding: assertion.DatasetBinding{Name: "evidence", Namespace: "fhir", Phase: "after", Source: sourceIdentity, ProjectionIdentity: dataset.Digest(data)}, Assertion: check}
}
