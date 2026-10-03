package desktop

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/interfacespec"
	"github.com/bharm16/readmit/internal/strictdoc"
)

const InterfaceAssociationEditorSchema = "readmit-interface-association-editor/v1"
const CaptureSourceEditorSchema = "readmit-capture-source-context-editor/v1"

type InterfaceAssociationEditorDraft struct {
	Schema     string                        `json:"schema"`
	ProjectID  string                        `json:"project_id"`
	Source     ItemRef                       `json:"source"`
	Identity   string                        `json:"identity"`
	Occurrence string                        `json:"occurrence"`
	Selector   string                        `json:"selector"`
	Base       *ItemRef                      `json:"base,omitzero"`
	Name       string                        `json:"name"`
	Profile    *ItemRef                      `json:"profile,omitzero"`
	Documents  []interfacespec.Documentation `json:"documents"`
}
type CaptureSourceEditorDraft struct {
	Schema    string               `json:"schema"`
	ProjectID string               `json:"project_id"`
	Source    ItemRef              `json:"source"`
	Identity  string               `json:"identity"`
	Base      *ItemRef             `json:"base,omitzero"`
	Sources   []CaptureAssociation `json:"sources"`
}

func editorSource(projectID string, source ItemRef, identity string) bool {
	return catalog.ValidID(projectID) && (source.Kind == CaseItem || source.Kind == VariantItem) && catalog.ValidID(source.ID) && (source.Revision == "" || catalog.ValidToken(source.Revision)) && validExportDigest(identity)
}
func editorPin(ref *ItemRef, kind ItemKind) bool {
	return ref == nil || ref.Kind == kind && catalog.ValidID(ref.ID) && catalog.ValidToken(ref.Revision)
}
func decodeInterfaceAssociationEditor(raw []byte) (InterfaceAssociationEditorDraft, error) {
	var d InterfaceAssociationEditorDraft
	err := (strictdoc.Document{Schema: InterfaceAssociationEditorSchema, MaxBytes: maxDraftContentBytes, Required: []string{"project_id", "source", "identity", "occurrence", "selector", "name", "documents"}, Invalid: "invalid authored specification draft", MustDeclare: "unsupported specification editor draft", TooLarge: "specification editor draft exceeds private bound", Requires: "specification draft needs its original source and authored fields"}).Decode(raw, &d)
	if err != nil {
		return d, err
	}
	if !editorSource(d.ProjectID, d.Source, d.Identity) || !editorPin(d.Base, InterfaceSpecItem) || !editorPin(d.Profile, ProfileItem) || !utf8.ValidString(d.Name) || len(d.Name) > 800 || d.Documents == nil || len(d.Documents) > interfacespec.MaxDocuments || d.Occurrence != "" && !catalog.ValidToken(d.Occurrence) {
		return d, errors.New("invalid specification draft owner or authored fields")
	}
	if d.Selector != "" {
		if _, err := hl7.ParseSelector(d.Selector); err != nil {
			return d, err
		}
	}
	seen := map[string]bool{}
	for _, doc := range d.Documents {
		if !catalog.ValidName(doc.Name) || !utf8.ValidString(doc.Text) || len(doc.Text) > interfacespec.MaxDocumentBytes || digestOf([]byte(doc.Text)) != doc.SHA256 || seen[doc.SHA256] {
			return d, errors.New("authored documentation byte identity does not verify")
		}
		seen[doc.SHA256] = true
	}
	return d, nil
}
func decodeCaptureSourceEditor(raw []byte) (CaptureSourceEditorDraft, error) {
	var d CaptureSourceEditorDraft
	err := (strictdoc.Document{Schema: CaptureSourceEditorSchema, MaxBytes: maxDraftContentBytes, Required: []string{"project_id", "source", "identity", "sources"}, Invalid: "invalid authored source correction draft", MustDeclare: "unsupported source correction editor draft", TooLarge: "source correction exceeds private draft bound", Requires: "source correction draft needs its exact original source"}).Decode(raw, &d)
	if err != nil {
		return d, err
	}
	if !editorSource(d.ProjectID, d.Source, d.Identity) || !editorPin(d.Base, CaptureContextItem) || d.Sources == nil {
		return d, errors.New("invalid source correction draft owner")
	}
	for _, s := range d.Sources {
		if s.ReceivedAtName != "" || s.ReceiptIdentity != "" || s.Basis != "manual" && s.Basis != "unknown" {
			return d, errors.New("source correction retains authored fields only")
		}
	}
	if err := validateCaptureContext(CaptureContext{Schema: CaptureContextSchema, Case: d.Source, Identity: d.Identity, Sources: d.Sources}); err != nil {
		return d, err
	}
	return d, nil
}
func validateContextEditorDraft(d EditorDraft) error {
	switch d.ContentSchema {
	case InterfaceAssociationEditorSchema:
		value, err := decodeInterfaceAssociationEditor(d.Content)
		if err != nil {
			return err
		}
		if d.Kind != "interface-association" || d.Identity != value.Identity || d.Item != nil {
			return errors.New("specification draft envelope does not match its source")
		}
	case CaptureSourceEditorSchema:
		value, err := decodeCaptureSourceEditor(d.Content)
		if err != nil {
			return err
		}
		if d.Kind != "capture-source-context" || d.Identity != value.Identity || d.Item != nil {
			return errors.New("source correction envelope does not match its source")
		}
	}
	return nil
}

type ContextEditorDraftRequest struct {
	Context    RequestContext `json:"context"`
	Schema     string         `json:"schema"`
	Source     ItemRef        `json:"source"`
	Identity   string         `json:"identity"`
	Occurrence string         `json:"occurrence"`
	Selector   string         `json:"selector"`
}
type ContextEditorDraftResult struct {
	State       State                            `json:"state"`
	Reason      string                           `json:"reason,omitzero"`
	Context     RequestContext                   `json:"context"`
	Retained    *EditorDraft                     `json:"retained,omitzero"`
	Association *InterfaceAssociationEditorDraft `json:"association,omitzero"`
	Correction  *CaptureSourceEditorDraft        `json:"correction,omitzero"`
}

func (r *ContextEditorDraftResult) refuse(s State, reason string) { r.State, r.Reason = s, reason }

// ReadContextEditorDraft restores only this project's exact source owner. It
// reads authored text and references, never inspected values or permission.
func (a *App) ReadContextEditorDraft(request ContextEditorDraftRequest) ContextEditorDraftResult {
	return runRead(a, false, func(ctx context.Context) ContextEditorDraftResult {
		r := ContextEditorDraftResult{Context: request.Context}
		if request.Schema != InterfaceAssociationEditorSchema && request.Schema != CaptureSourceEditorSchema {
			r.refuse(Failed, "unsupported context editor owner")
			return r
		}
		loaded, no := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			r.refuse(no.state, no.reason)
			return r
		}
		r.Context.ProjectID = loaded.document.Project.ID
		drafts, no := a.retainedDrafts()
		if no.state != "" {
			r.refuse(no.state, no.reason)
			return r
		}
		for i := len(drafts) - 1; i >= 0; i-- {
			held := drafts[i]
			if held.Workspace != loaded.root || held.ContentSchema != request.Schema {
				continue
			}
			switch request.Schema {
			case InterfaceAssociationEditorSchema:
				d, err := decodeInterfaceAssociationEditor(held.Content)
				if err != nil {
					r.refuse(Failed, err.Error())
					return r
				}
				if d.ProjectID == loaded.document.Project.ID && d.Source == request.Source && d.Identity == request.Identity && d.Occurrence == request.Occurrence && d.Selector == request.Selector {
					r.State, r.Retained, r.Association = Completed, &held, &d
					return r
				}
			case CaptureSourceEditorSchema:
				d, err := decodeCaptureSourceEditor(held.Content)
				if err != nil {
					r.refuse(Failed, err.Error())
					return r
				}
				if d.ProjectID == loaded.document.Project.ID && d.Source == request.Source && d.Identity == request.Identity {
					r.State, r.Retained, r.Correction = Completed, &held, &d
					return r
				}
			default:
				r.refuse(Failed, "unsupported context editor owner")
				return r
			}
		}
		r.State = Empty
		return r
	})
}
