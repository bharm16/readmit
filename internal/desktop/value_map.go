package desktop

import (
	"context"
	"errors"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/valuemap"
	"path/filepath"
)

const ValueMapItem ItemKind = "field-value-map"

type ValueMapsResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Items   []CatalogItem  `json:"items"`
}

func (r *ValueMapsResult) refuse(s State, reason string) { r.State, r.Reason = s, reason }

type ValueMapResult struct {
	State   State              `json:"state"`
	Reason  string             `json:"reason,omitzero"`
	Context RequestContext     `json:"context"`
	Ref     *ItemRef           `json:"ref,omitzero"`
	Map     *valuemap.Document `json:"map,omitzero"`
}

func (r *ValueMapResult) refuse(s State, reason string) { r.State, r.Reason = s, reason; r.Map = nil }

type ValueMapSaveRequest struct {
	Context  RequestContext    `json:"context"`
	Base     *ItemRef          `json:"base,omitzero"`
	IntentID string            `json:"intent_id"`
	Draft    valuemap.Document `json:"draft"`
}
type ValueMapImportRequest struct {
	Context RequestContext `json:"context"`
	Path    string         `json:"path,omitzero"`
}
type ValueMapExportRequest struct {
	Context     RequestContext `json:"context"`
	Ref         ItemRef        `json:"ref"`
	Destination string         `json:"destination,omitzero"`
}
type ValueMapExportResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Path    string         `json:"path,omitzero"`
	SHA256  string         `json:"sha256,omitzero"`
	Bytes   int            `json:"bytes"`
}

func (r *ValueMapExportResult) refuse(s State, reason string) { r.State, r.Reason = s, reason }
func decodeValueMapFile(path string) (valuemap.Document, error) {
	raw, err := boundedFile(path, valuemap.MaxBytes)
	if err != nil {
		return valuemap.Document{}, err
	}
	return valuemap.Decode(raw)
}
func init() {
	extraSchemas = append(extraSchemas, valuemap.Schema)
	itemKinds = append(itemKinds, ValueMapItem)
	familyKinds["readmit-field-value-map/"] = ValueMapItem
	readers[ValueMapItem] = func(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
		d, err := decodeValueMapFile(paths[string(ValueMapItem)])
		result := view{name: d.Name}
		if err != nil {
			return result, err
		}
		if d.Project != c.document.Project.ID {
			return result, errors.New("the map belongs to another interface project")
		}
		if item.RevisionLabel() == "" {
			return result, errors.New("this map file has no retained catalog revision; import CSV and explicitly save a map revision; the original file is kept")
		}
		return result, nil
	}
}
func (c *loadedCatalog) valueMap(ref ItemRef) (valuemap.Document, error) {
	if ref.Kind != ValueMapItem || ref.Revision == "" {
		return valuemap.Document{}, errors.New("choose an exact field value map revision")
	}
	raw, err := c.memberBytes(ref, string(ValueMapItem), valuemap.MaxBytes)
	if err != nil {
		return valuemap.Document{}, err
	}
	d, err := valuemap.Decode(raw)
	if err != nil {
		return d, err
	}
	if d.Project != c.document.Project.ID {
		return valuemap.Document{}, errors.New("the map belongs to another interface project")
	}
	return d, nil
}
func (a *App) ListValueMaps(request RequestContext) ValueMapsResult {
	return runRead(a, false, func(ctx context.Context) ValueMapsResult {
		r := ValueMapsResult{Context: request, Items: []CatalogItem{}}
		c, no := a.loadCatalog(ctx, request, false)
		if c == nil {
			r.refuse(no.state, no.reason)
			return r
		}
		r.State, r.Items = Completed, c.list(ValueMapItem)
		return r
	})
}
func (a *App) ReadValueMap(request ItemRequest) ValueMapResult {
	return runRead(a, false, func(ctx context.Context) ValueMapResult {
		r := ValueMapResult{Context: request.Context}
		c, no := a.loadCatalog(ctx, request.Context, false)
		if c == nil {
			r.refuse(no.state, no.reason)
			return r
		}
		d, err := c.valueMap(request.Ref)
		if err != nil {
			r.refuse(Failed, err.Error())
			return r
		}
		r.State, r.Map, r.Ref = Completed, &d, &request.Ref
		return r
	})
}
func (a *App) SaveValueMap(request ValueMapSaveRequest) ValueMapResult {
	return run(a, false, true, func(ctx context.Context) ValueMapResult {
		r := ValueMapResult{Context: request.Context}
		c, no := a.loadCatalog(ctx, request.Context, true)
		if c == nil {
			r.refuse(no.state, no.reason)
			return r
		}
		fail := func(err error) ValueMapResult { r.refuse(Failed, err.Error()); return r }
		d := request.Draft
		for _, path := range []*string{&d.SourceSelector, &d.DestinationSelector} {
			selected, err := hl7.ParseSelector(*path)
			if err != nil {
				return fail(err)
			}
			*path = selected.String()
		}
		if d.Project != "" && d.Project != c.document.Project.ID {
			return fail(errors.New("a value map cannot inherit another interface project's scope"))
		}
		d.Project = c.document.Project.ID
		raw, err := valuemap.Encode(d)
		if err != nil {
			return fail(err)
		}
		if !catalog.ValidToken(request.IntentID) {
			return fail(errors.New("a map save requires its explicit intent"))
		}
		for _, association := range d.Associations {
			at := c.document.Find(association.Item)
			if at < 0 {
				return fail(errors.New("the declared associated object is not retained in this interface"))
			}
			item := c.document.Items[at]
			if item.Kind != association.Kind {
				return fail(errors.New("association kind disagrees with the retained object"))
			}
			record, ok := itemAt(item, association.Revision)
			if !ok {
				return fail(errors.New("association revision is unavailable"))
			}
			_, availability, reason := c.backing(record)
			if availability != ItemAvailable {
				return fail(errors.New(reason))
			}
		}
		draft := catalog.Draft{Kind: string(ValueMapItem), Intent: request.IntentID, Digest: digestOf(raw), Author: a.reviewerName(), Members: []catalog.Staged{{Role: string(ValueMapItem), File: "map.json", Data: raw}}}
		if request.Base != nil {
			if _, err := c.valueMap(*request.Base); err != nil {
				return fail(err)
			}
			draft.ItemID, draft.Base = request.Base.ID, request.Base.Revision
		}
		saved, err := c.store.Save(draft, func(paths map[string]string) error {
			_, err := decodeValueMapFile(paths[string(ValueMapItem)])
			return err
		}, catalog.Options{Now: a.now, Fault: a.saveFault})
		if err != nil {
			return fail(err)
		}
		ref := ItemRef{Kind: ValueMapItem, ID: saved.Item.ID, Revision: saved.Item.RevisionLabel()}
		r.State, r.Map, r.Ref = Completed, &d, &ref
		return r
	})
}

// ImportValueMapCSV validates one complete draft. It never publishes it.
func (a *App) ImportValueMapCSV(request ValueMapImportRequest) ValueMapResult {
	return run(a, false, false, func(ctx context.Context) ValueMapResult {
		r := ValueMapResult{Context: request.Context}
		c, no := a.loadCatalog(ctx, request.Context, false)
		if c == nil {
			r.refuse(no.state, no.reason)
			return r
		}
		path := request.Path
		if path == "" {
			chosen, no := a.chooseFiles(ctx, "Import value map CSV", "CSV", "*.csv")
			if len(chosen) != 1 {
				r.refuse(no.state, no.reason)
				if len(chosen) > 1 {
					r.refuse(Failed, "choose one complete CSV")
				}
				return r
			}
			path = chosen[0]
		}
		raw, _, reason := selectionBytes(path, "", valuemap.MaxBytes)
		if reason != "" {
			r.refuse(Failed, reason)
			return r
		}
		d, err := valuemap.ImportCSV(raw, c.document.Project.ID)
		if err != nil {
			r.refuse(Failed, err.Error())
			return r
		}
		r.State, r.Map = Completed, &d
		return r
	})
}

// ExportValueMapCSV exports authored declarations, never clinical source values.
func (a *App) ExportValueMapCSV(request ValueMapExportRequest) ValueMapExportResult {
	return run(a, true, false, func(ctx context.Context) ValueMapExportResult {
		r := ValueMapExportResult{Context: request.Context}
		c, no := a.loadCatalog(ctx, request.Context, false)
		if c == nil {
			r.refuse(no.state, no.reason)
			return r
		}
		d, err := c.valueMap(request.Ref)
		if err != nil {
			r.refuse(Failed, err.Error())
			return r
		}
		raw, err := valuemap.CSV(d)
		if err != nil {
			r.refuse(Failed, err.Error())
			return r
		}
		path := request.Destination
		if path == "" {
			chosen, no := a.chooseNamedDestination(ctx, "Export value map CSV", actionSlug(d.Name)+".csv")
			if chosen == "" {
				r.refuse(no.state, no.reason)
				return r
			}
			path = chosen
		}
		if !filepath.IsAbs(path) {
			r.refuse(Failed, "choose an absolute new CSV destination")
			return r
		}
		folder, no := chosenFolder(filepath.Dir(path))
		if folder == "" {
			r.refuse(no.state, no.reason)
			return r
		}
		path = filepath.Join(folder, filepath.Base(path))
		if err := operation.WriteNewFile(path, raw, "CSV destination must be new and writable", "cannot write the map CSV"); err != nil {
			r.refuse(refusalState(err), err.Error())
			return r
		}
		r.State, r.Path, r.SHA256, r.Bytes = Completed, path, digestOf(raw), len(raw)
		return r
	})
}

type ValueMapInspectionRequest struct {
	Context    RequestContext `json:"context"`
	Ref        ItemRef        `json:"ref"`
	Source     ItemRef        `json:"source"`
	Identity   string         `json:"identity"`
	Occurrence string         `json:"occurrence"`
	Selector   string         `json:"selector"`
	Reveal     bool           `json:"reveal"`
}
type ValueMapInspectionResult struct {
	State      State           `json:"state"`
	Reason     string          `json:"reason,omitzero"`
	Context    RequestContext  `json:"context"`
	Ref        ItemRef         `json:"ref"`
	Identity   string          `json:"identity"`
	Occurrence string          `json:"occurrence"`
	Selector   string          `json:"selector"`
	Revealed   bool            `json:"revealed"`
	Mapping    string          `json:"mapping"`
	FieldState string          `json:"field_state"`
	Entry      *valuemap.Entry `json:"entry,omitzero"`
}

func (r *ValueMapInspectionResult) refuse(s State, reason string) {
	r.State, r.Reason = s, reason
	r.Entry = nil
}
func (a *App) InspectValueMap(request ValueMapInspectionRequest) ValueMapInspectionResult {
	return runRead(a, false, func(ctx context.Context) ValueMapInspectionResult {
		r := ValueMapInspectionResult{Context: request.Context, Ref: request.Ref, Identity: request.Identity, Occurrence: request.Occurrence, Selector: request.Selector, Revealed: request.Reveal, Mapping: "not_available"}
		c, entry, no := a.caseEntry(ctx, request.Context, request.Source, false)
		if c == nil {
			r.refuse(no.state, no.reason)
			return r
		}
		mapping, err := c.valueMap(request.Ref)
		if err != nil {
			r.refuse(Failed, err.Error())
			return r
		}
		if request.Identity == "" {
			r.refuse(Failed, "contextual map inspection requires the exact source identity")
			return r
		}
		_, source, declined := openedCase(c.root, entry, request.Identity)
		if source == nil {
			r.refuse(declined.state, declined.reason)
			return r
		}
		selector, err := hl7.ParseSelector(request.Selector)
		if err != nil {
			r.refuse(Failed, err.Error())
			return r
		}
		r.Selector = selector.String()
		if r.Selector != mapping.SourceSelector {
			r.State, r.Mapping = Completed, "not_applicable"
			r.Reason = "This exact field selector is outside the recorded map scope; a shared table does not broaden it."
			return r
		}
		var event *bundle.Event
		for i := range source.Events {
			if source.Events[i].ID == request.Occurrence {
				event = &source.Events[i]
				break
			}
		}
		if event == nil {
			r.refuse(Failed, "the selected source occurrence is unavailable")
			return r
		}
		doc, err := source.Document(*event)
		if err != nil {
			r.State, r.Mapping = Completed, "undecodable"
			return r
		}
		node, _, err := doc.Navigate(0, selector.String())
		if err != nil {
			r.refuse(Failed, err.Error())
			return r
		}
		if fieldMetadata(doc, 0, node).HL7Version != mapping.Edition {
			r.State, r.Mapping = Completed, "incompatible"
			r.Reason = "Actual message edition differs from the map; no destination is inferred."
			return r
		}
		reading, err := doc.Read(0, selector, hl7.EnforceMSH18)
		if err != nil {
			r.refuse(Failed, err.Error())
			return r
		}
		r.State = Completed
		r.FieldState = string(reading.State)
		if reading.State != hl7.Present {
			r.Mapping = "absent"
			return r
		}
		value, ok := reading.Text()
		if !ok {
			r.Mapping = "undecodable"
			return r
		}
		if !request.Reveal {
			r.Mapping = "hidden"
			return r
		}
		r.Mapping = "unmapped"
		for _, e := range mapping.Entries {
			if e.Source == value {
				copy := e
				r.Mapping, r.Entry = "mapped", &copy
				break
			}
		}
		return r
	})
}
