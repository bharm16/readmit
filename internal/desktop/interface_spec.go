package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/interfacespec"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileeval"
)

const InterfaceSpecItem ItemKind = "interface-spec"

type InterfaceSpecsResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Items   []CatalogItem  `json:"items"`
}

func (r *InterfaceSpecsResult) refuse(s State, reason string) { r.State, r.Reason = s, reason }

type InterfaceSpecResult struct {
	State   State                   `json:"state"`
	Reason  string                  `json:"reason,omitzero"`
	Context RequestContext          `json:"context"`
	Ref     *ItemRef                `json:"ref,omitzero"`
	Spec    *interfacespec.Document `json:"spec,omitzero"`
}

func (r *InterfaceSpecResult) refuse(s State, reason string) { r.State, r.Reason = s, reason }

type InterfaceSpecSaveRequest struct {
	Context   RequestContext                `json:"context"`
	Base      *ItemRef                      `json:"base,omitzero"`
	IntentID  string                        `json:"intent_id"`
	Name      string                        `json:"name"`
	Profile   ItemRef                       `json:"profile"`
	Documents []interfacespec.Documentation `json:"documents"`
}
type InterfaceSpecDocumentResult struct {
	State    State                        `json:"state"`
	Reason   string                       `json:"reason,omitzero"`
	Context  RequestContext               `json:"context"`
	Document *interfacespec.Documentation `json:"document,omitzero"`
}

func (r *InterfaceSpecDocumentResult) refuse(s State, reason string) { r.State, r.Reason = s, reason }

func decodeInterfaceSpecFile(path string) (interfacespec.Document, error) {
	raw, err := boundedFile(path, interfacespec.MaxBytes)
	if err != nil {
		return interfacespec.Document{}, err
	}
	return interfacespec.Decode(raw)
}
func init() {
	extraSchemas = append(extraSchemas, interfacespec.Schema)
	itemKinds = append(itemKinds, InterfaceSpecItem)
	familyKinds["readmit-interface-spec/"] = InterfaceSpecItem
	readers[InterfaceSpecItem] = func(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
		d, err := decodeInterfaceSpecFile(paths[string(InterfaceSpecItem)])
		result := view{name: d.Name}
		if err != nil {
			return result, err
		}
		if d.Project != c.document.Project.ID {
			return result, errors.New("the specification belongs to another interface project")
		}
		if item.RevisionLabel() == "" {
			return result, errors.New("this specification file has no retained catalog revision; create a specification with an exact saved profile revision; the original file is kept")
		}
		return result, nil
	}
}

func (a *App) ListInterfaceSpecs(contextRequest RequestContext) InterfaceSpecsResult {
	return runRead(a, false, func(ctx context.Context) InterfaceSpecsResult {
		result := InterfaceSpecsResult{Context: contextRequest, Items: []CatalogItem{}}
		loaded, declined := a.loadCatalog(ctx, contextRequest, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		result.Items = loaded.list(InterfaceSpecItem)
		result.State = Completed
		return result
	})
}
func (a *App) ReadInterfaceSpec(request ItemRequest) InterfaceSpecResult {
	return runRead(a, false, func(ctx context.Context) InterfaceSpecResult {
		result := InterfaceSpecResult{Context: request.Context}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if request.Ref.Kind != InterfaceSpecItem {
			result.refuse(Failed, "choose an interface specification revision")
			return result
		}
		raw, err := loaded.memberBytes(request.Ref, string(InterfaceSpecItem), interfacespec.MaxBytes)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		value, err := interfacespec.Decode(raw)
		if err != nil || value.Project != loaded.document.Project.ID {
			result.refuse(Failed, "the specification does not belong to this interface project")
			return result
		}
		result.State, result.Spec, result.Ref = Completed, &value, &request.Ref
		return result
	})
}

// SaveInterfaceSpec publishes one whole-object catalog revision. Its selected
// local profile is read at an exact revision; documents stay descriptive bytes.
func (a *App) SaveInterfaceSpec(request InterfaceSpecSaveRequest) InterfaceSpecResult {
	return run(a, false, true, func(ctx context.Context) InterfaceSpecResult {
		result := InterfaceSpecResult{Context: request.Context}
		loaded, declined := a.loadCatalog(ctx, request.Context, true)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		fail := func(reason string) InterfaceSpecResult { result.refuse(Failed, reason); return result }
		if !catalog.ValidToken(request.IntentID) || request.Profile.Kind != ProfileItem || request.Profile.Revision == "" {
			return fail("a specification save requires its intent and exact local profile revision")
		}
		raw, err := loaded.memberBytes(request.Profile, profileRole, profileeval.MaxBytes)
		if err != nil {
			return fail("the selected profile revision cannot be read")
		}
		profile, err := profileeval.DecodeProfile(raw)
		if err != nil {
			return fail("choose a supported local profile; documents and metadata packs are not requirements")
		}
		value := interfacespec.Document{Schema: interfacespec.Schema, Project: loaded.document.Project.ID, Name: request.Name, Profile: interfacespec.ProfilePin{Item: request.Profile.ID, Revision: request.Profile.Revision, Schema: profile.Schema, ID: profile.Definition.Identity.ID, Version: profile.Definition.Identity.Version, SHA256: digestOf(raw)}, Documents: request.Documents}
		encoded, err := interfacespec.Encode(value)
		if err != nil {
			return fail(err.Error())
		}
		draft := catalog.Draft{Kind: string(InterfaceSpecItem), Intent: request.IntentID, Digest: digestOf(encoded), Author: a.reviewerName(), Members: []catalog.Staged{{Role: string(InterfaceSpecItem), File: "spec.json", Data: encoded}}}
		if request.Base != nil {
			if request.Base.Kind != InterfaceSpecItem || request.Base.Revision == "" {
				return fail("an existing specification needs its exact base revision")
			}
			prior, err := loaded.memberBytes(*request.Base, string(InterfaceSpecItem), interfacespec.MaxBytes)
			if err != nil {
				return fail("the specification base cannot be read")
			}
			old, err := interfacespec.Decode(prior)
			if err != nil || old.Project != value.Project {
				return fail("the specification base belongs to another interface")
			}
			draft.ItemID, draft.Base = request.Base.ID, request.Base.Revision
		}
		saved, err := loaded.store.Save(draft, func(files map[string]string) error {
			_, err := decodeInterfaceSpecFile(files[string(InterfaceSpecItem)])
			return err
		}, catalog.Options{Now: a.now, Fault: a.saveFault})
		if err != nil {
			return fail(err.Error())
		}
		ref := ItemRef{Kind: InterfaceSpecItem, ID: saved.Item.ID, Revision: saved.Item.RevisionLabel()}
		result.State, result.Ref, result.Spec = Completed, &ref, &value
		return result
	})
}

// ChooseInterfaceSpecDocument returns a bounded local text copy for an authored
// save. Reading a document never interprets it as a profile or a commitment.
func (a *App) ChooseInterfaceSpecDocument(contextRequest RequestContext) InterfaceSpecDocumentResult {
	return run(a, false, false, func(ctx context.Context) InterfaceSpecDocumentResult {
		result := InterfaceSpecDocumentResult{Context: contextRequest}
		if loaded, declined := a.loadCatalog(ctx, contextRequest, false); loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		chosen, declined := a.chooseFiles(ctx, "Add interface specification document", "UTF-8 text", "*.txt;*.md")
		if chosen == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if len(chosen) != 1 {
			result.refuse(Failed, "choose one UTF-8 text document")
			return result
		}
		raw, _, reason := selectionBytes(chosen[0], "", interfacespec.MaxDocumentBytes)
		if reason != "" || !utf8.Valid(raw) {
			result.refuse(Failed, "the document is not bounded UTF-8 text")
			return result
		}
		doc := interfacespec.Documentation{Name: filepath.Base(chosen[0]), Text: string(raw), SHA256: digestOf(raw)}
		result.State, result.Document = Completed, &doc
		return result
	})
}

func (c *loadedCatalog) pinnedSpecProfile(value interfacespec.Document) (ItemRef, error) {
	ref := ItemRef{Kind: ProfileItem, ID: value.Profile.Item, Revision: value.Profile.Revision}
	raw, err := c.memberBytes(ref, profileRole, profileeval.MaxBytes)
	if err != nil {
		return ref, err
	}
	if digestOf(raw) != value.Profile.SHA256 {
		return ref, errors.New("the specification's pinned profile bytes changed")
	}
	if !strings.HasPrefix(schemaOf(raw), "readmit-local-profile/") {
		return ref, errors.New("the pinned profile is unavailable")
	}
	return ref, nil
}

type InterfaceRequirementsRequest struct {
	Context    RequestContext `json:"context"`
	Source     ItemRef        `json:"source"`
	Identity   string         `json:"identity"`
	Occurrence string         `json:"occurrence"`
	Selector   string         `json:"selector"`
	Spec       ItemRef        `json:"spec"`
}
type InterfaceRequirementsResult struct {
	State         State                    `json:"state"`
	Reason        string                   `json:"reason,omitzero"`
	Context       RequestContext           `json:"context"`
	SpecRef       *ItemRef                 `json:"spec_ref,omitzero"`
	Spec          *interfacespec.Document  `json:"spec,omitzero"`
	Profile       *ItemRef                 `json:"profile,omitzero"`
	Selector      string                   `json:"selector"`
	Segment       string                   `json:"segment"`
	Position      int                      `json:"position"`
	Applicability string                   `json:"applicability"`
	Resolution    *localprofile.Resolution `json:"resolution,omitzero"`
}

func (r *InterfaceRequirementsResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
	r.Resolution = nil
	r.Profile = nil
}

// ReadInterfaceRequirements binds a chosen interface specification to one
// verified field/finding occurrence. It resolves authored provenance only;
// evaluation remains explicit and historical evidence pins remain untouched.
func (a *App) ReadInterfaceRequirements(request InterfaceRequirementsRequest) InterfaceRequirementsResult {
	return runRead(a, false, func(ctx context.Context) InterfaceRequirementsResult {
		result := InterfaceRequirementsResult{Context: request.Context, Selector: request.Selector, Applicability: "not_available"}
		loaded, entry, declined := a.caseEntry(ctx, request.Context, request.Source, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		_, source, declined := openedCase(loaded.root, entry, request.Identity)
		if source == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if request.Identity == "" || request.Spec.Kind != InterfaceSpecItem || request.Spec.Revision == "" {
			result.refuse(Failed, "choose one exact interface specification revision for the displayed source")
			return result
		}
		raw, err := loaded.memberBytes(request.Spec, string(InterfaceSpecItem), interfacespec.MaxBytes)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		spec, err := interfacespec.Decode(raw)
		if err != nil || spec.Project != loaded.document.Project.ID {
			result.refuse(Failed, "the specification belongs to a different interface or is invalid")
			return result
		}
		result.Spec, result.SpecRef = &spec, &request.Spec
		ref, err := loaded.pinnedSpecProfile(spec)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		profileRaw, err := loaded.memberBytes(ref, profileRole, profileeval.MaxBytes)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		profile, err := profileeval.DecodeProfile(profileRaw)
		if err != nil {
			result.refuse(Failed, "the selected local profile is unavailable")
			return result
		}
		if request.Occurrence == "" && request.Selector == "" {
			result.State, result.Profile, result.Applicability = Completed, &ref, "unspecified"
			result.Reason = "Case-level specification context; no field applicability is inferred. Evaluate the case explicitly."
			return result
		}
		var event *bundle.Event
		for i := range source.Events {
			if source.Events[i].ID == request.Occurrence {
				event = &source.Events[i]
				break
			}
		}
		if event == nil {
			result.refuse(Failed, "the requirement source occurrence is unavailable")
			return result
		}
		doc, err := source.Document(*event)
		if err != nil {
			result.State, result.Reason = Completed, "The source occurrence cannot establish profile applicability."
			return result
		}
		selected, _, err := doc.Navigate(0, request.Selector)
		if err != nil {
			result.refuse(Failed, "the requirement field path is invalid")
			return result
		}
		result.Selector = selected.Path
		result.Segment, result.Position = selected.Segment, selected.Field
		metadata := fieldMetadata(doc, 0, selected)
		family, _ := messageType(doc, 0)
		result.State = Completed
		if profile.Definition.Base.HL7Version != metadata.HL7Version || profile.Definition.Base.Family != family {
			result.Applicability, result.Reason = "incompatible", "The selected profile's declared edition/family differs from the original message; no constraints are applied."
			return result
		}
		_, pack := loaded.pinnedPack(profile.Definition.Base.Pack)
		resolution := localprofile.Resolve(profile.Definition, pack)
		result.Profile, result.Resolution, result.Applicability = &ref, &resolution, "declared_match"
		result.Reason = "Declared profile context matches; this read is not an evaluation or receiver commitment."
		return result
	})
}
