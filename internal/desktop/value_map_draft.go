package desktop

import (
	"context"
	"errors"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/strictdoc"
	"github.com/bharm16/readmit/internal/valuemap"
)

const ValueMapEditorDraftSchema = "readmit-field-value-map-editor/v1"

type ValueMapEditorDraft struct {
	Schema       string            `json:"schema"`
	Source       ItemRef           `json:"source"`
	Identity     string            `json:"identity"`
	Occurrence   string            `json:"occurrence"`
	Selector     string            `json:"selector"`
	Edition      string            `json:"edition"`
	Base         *ItemRef          `json:"base,omitzero"`
	Draft        valuemap.Document `json:"draft"`
	WorkingEntry *valuemap.Entry   `json:"working_entry,omitzero"`
	WorkingIndex *int              `json:"working_index,omitzero"`
}

func decodeValueMapEditorDraft(raw []byte) (ValueMapEditorDraft, error) {
	var d ValueMapEditorDraft
	if err := (strictdoc.Document{Schema: ValueMapEditorDraftSchema, MaxBytes: maxDraftContentBytes, Required: []string{"source", "identity", "occurrence", "selector", "edition", "draft"}, Invalid: "invalid value map editor draft", MustDeclare: "unsupported value map editor draft", TooLarge: "map draft exceeds private retention bound", Requires: "map editor draft needs its exact source IDs and authored values"}).Decode(raw, &d); err != nil {
		return d, err
	}
	if d.Source.Kind != CaseItem && d.Source.Kind != VariantItem || !catalog.ValidID(d.Source.ID) || !validExportDigest(d.Identity) || !catalog.ValidToken(d.Occurrence) || d.Edition != "" && !printable(d.Edition, maxDraftIdentityBytes) || d.Draft.Schema != valuemap.Schema || len(d.Draft.Entries) > valuemap.MaxEntries || d.Draft.Entries == nil || d.Draft.Associations == nil {
		return d, errors.New("map draft needs its original source and bounded authored declaration")
	}
	if _, err := hl7.ParseSelector(d.Selector); err != nil {
		return d, err
	}
	if d.WorkingIndex != nil && (d.WorkingEntry == nil || *d.WorkingIndex < 0 || *d.WorkingIndex >= len(d.Draft.Entries)) {
		return d, errors.New("working entry index is outside the authored draft")
	}
	if d.Base != nil && (d.Base.Kind != ValueMapItem || !catalog.ValidID(d.Base.ID) || !catalog.ValidToken(d.Base.Revision)) {
		return d, errors.New("existing map draft requires exact base revision")
	}
	return d, nil
}
func validValueMapEditorDraft(d EditorDraft) bool {
	if d.Kind != "field-value-map" {
		return false
	}
	content, err := decodeValueMapEditorDraft(d.Content)
	return err == nil && (d.Item == nil && content.Base == nil || d.Item != nil && content.Base != nil && d.Item.Ref == *content.Base)
}

type ValueMapDraftRequest struct {
	Context    RequestContext `json:"context"`
	Source     ItemRef        `json:"source"`
	Identity   string         `json:"identity"`
	Occurrence string         `json:"occurrence"`
	Selector   string         `json:"selector"`
	Edition    string         `json:"edition"`
}
type ValueMapDraftResult struct {
	State    State                `json:"state"`
	Reason   string               `json:"reason,omitzero"`
	Context  RequestContext       `json:"context"`
	Retained *EditorDraft         `json:"retained,omitzero"`
	Draft    *ValueMapEditorDraft `json:"draft,omitzero"`
}

func (r *ValueMapDraftResult) refuse(s State, reason string) { r.State, r.Reason = s, reason }

// ReadValueMapDraft exposes only the owning editor's typed authored draft for
// these exact source IDs. It carries no inspected value or reveal consent.
func (a *App) ReadValueMapDraft(request ValueMapDraftRequest) ValueMapDraftResult {
	return runRead(a, false, func(ctx context.Context) ValueMapDraftResult {
		r := ValueMapDraftResult{Context: request.Context}
		if c, no := a.loadCatalog(ctx, request.Context, false); c == nil {
			r.refuse(no.state, no.reason)
			return r
		}
		drafts, no := a.retainedDrafts()
		if no.state != "" {
			r.refuse(no.state, no.reason)
			return r
		}
		selector, err := hl7.ParseSelector(request.Selector)
		if err != nil {
			r.refuse(Failed, err.Error())
			return r
		}
		for i := len(drafts) - 1; i >= 0; i-- {
			held := drafts[i]
			if held.ContentSchema != ValueMapEditorDraftSchema || held.Workspace != request.Context.Project {
				continue
			}
			d, err := decodeValueMapEditorDraft(held.Content)
			if err != nil {
				r.refuse(Failed, err.Error())
				return r
			}
			selected, err := hl7.ParseSelector(d.Selector)
			if err != nil {
				continue
			}
			if d.Source == request.Source && d.Identity == request.Identity && d.Occurrence == request.Occurrence && selected.String() == selector.String() && d.Edition == request.Edition {
				r.State, r.Retained, r.Draft = Completed, &held, &d
				return r
			}
		}
		r.State = Empty
		return r
	})
}
