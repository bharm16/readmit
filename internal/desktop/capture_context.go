package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/importer"
	"os"
	"path/filepath"
	"slices"
)

// CaptureContextSchema describes source-associated context kept beside original
// evidence. It conveys provenance, never connectivity or send authority.
const CaptureContextSchema = "readmit-capture-context/v1"
const CaptureContextItem ItemKind = "capture-context"

type CaptureAssociation struct {
	ReceivedAtName  string   `json:"received_at_name,omitzero"`
	SourceID        string   `json:"source_id"`
	Source          string   `json:"source"`
	Channel         string   `json:"channel"`
	ReceivedAt      *ItemRef `json:"received_at,omitzero"`
	Basis           string   `json:"basis"`
	ReceiptIdentity string   `json:"receipt_identity,omitzero"`
	Remember        bool     `json:"remember,omitzero"`
}
type CaptureContext struct {
	Schema   string               `json:"schema"`
	Case     ItemRef              `json:"case"`
	Identity string               `json:"identity"`
	Sources  []CaptureAssociation `json:"sources"`
}
type CaptureContextResult struct {
	State   State           `json:"state"`
	Reason  string          `json:"reason,omitzero"`
	Context RequestContext  `json:"context"`
	Ref     *ItemRef        `json:"ref,omitzero"`
	Capture *CaptureContext `json:"capture,omitzero"`
}

func (r *CaptureContextResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

type CaptureContextSaveRequest struct {
	Context  RequestContext       `json:"context"`
	Source   ItemRef              `json:"source"`
	Identity string               `json:"identity"`
	Base     *ItemRef             `json:"base,omitzero"`
	IntentID string               `json:"intent_id"`
	Sources  []CaptureAssociation `json:"sources"`
}

func init() {
	readers[CaptureContextItem] = func(_ *loadedCatalog, _ catalog.Item, paths map[string]string) (view, error) {
		_, err := readCaptureContextFile(paths[string(CaptureContextItem)])
		return view{name: "Capture source context"}, err
	}
}

func validateCaptureContext(value CaptureContext) error {
	if value.Schema != CaptureContextSchema || (value.Case.Kind != CaseItem && value.Case.Kind != VariantItem) || !catalog.ValidID(value.Case.ID) || !validExportDigest(value.Identity) || len(value.Sources) > 128 {
		return errors.New("invalid capture source context")
	}
	seen := map[string]bool{}
	for _, source := range value.Sources {
		if !catalog.ValidToken(source.SourceID) || seen[source.SourceID] || (source.Source != "" && !printable(source.Source, 800)) || (source.Channel != "" && !printable(source.Channel, 800)) || (source.ReceivedAtName != "" && !printable(source.ReceivedAtName, 800)) || !slices.Contains([]string{"unknown", "manual", "mapped", "observed"}, source.Basis) {
			return errors.New("invalid source association")
		}
		seen[source.SourceID] = true
		if source.ReceivedAt != nil && ((source.ReceivedAt.Kind != EnvironmentItem && source.ReceivedAt.Kind != SourceItem) || !catalog.ValidID(source.ReceivedAt.ID) || source.ReceivedAt.Revision == "") {
			return errors.New("a received-at association names an exact saved target revision")
		}
		if source.Basis == "unknown" && (source.ReceivedAt != nil || source.Remember) {
			return errors.New("unknown provenance supplies no target association")
		}
		if source.ReceiptIdentity != "" && !validExportDigest(source.ReceiptIdentity) {
			return errors.New("invalid provenance receipt identity")
		}
	}
	return nil
}
func readCaptureContextFile(path string) (CaptureContext, error) {
	var value CaptureContext
	raw, err := boundedFile(path, 256<<10)
	if err != nil {
		return value, err
	}
	if err = json.Unmarshal(raw, &value, json.RejectUnknownMembers(true)); err != nil {
		return value, err
	}
	return value, validateCaptureContext(value)
}
func (c *loadedCatalog) contexts() ([]CaptureContext, []ItemRef, error) {
	values := []CaptureContext{}
	refs := []ItemRef{}
	for _, item := range c.document.Items {
		if item.Kind != string(CaptureContextItem) || c.removed(item) {
			continue
		}
		current := item.Current()
		if current == nil {
			continue
		}
		for _, member := range current.Members {
			if member.Role != string(CaptureContextItem) {
				continue
			}
			raw, err := boundedFile(c.store.Path(member), 256<<10)
			if err == nil && digestOf(raw) != member.SHA256 {
				err = errors.New("capture context member identity changed")
			}
			var value CaptureContext
			if err == nil {
				value, err = readCaptureContextFile(c.store.Path(member))
			}
			if err != nil {
				return nil, nil, errors.New("retained capture context is unreadable; it is left exactly as written")
			}
			values = append(values, value)
			refs = append(refs, ItemRef{Kind: CaptureContextItem, ID: item.ID, Revision: item.RevisionLabel()})
		}
	}
	return values, refs, nil
}
func (c *loadedCatalog) currentCaptureContext(ref ItemRef, entry, identity string) (*CaptureContext, *ItemRef, error) {
	values, refs, err := c.contexts()
	if err != nil {
		return nil, nil, err
	}
	for i, value := range values {
		if value.Case.ID == ref.ID && value.Case.Kind == ref.Kind {
			if value.Identity != identity {
				return nil, nil, errors.New("the source evidence changed since its context was recorded")
			}
			copy := value
			copy.Sources = slices.Clone(value.Sources)
			return &copy, &refs[i], nil
		}
	}
	return nil, nil, nil
}
func (c *loadedCatalog) captureSourceContext(ref ItemRef, entry, identity string) (*CaptureContext, *ItemRef, error) {
	if value, held, err := c.currentCaptureContext(ref, entry, identity); value != nil || err != nil {
		return value, held, err
	}
	_, evidence, declined := openedCase(c.root, entry, identity)
	if evidence == nil {
		return nil, nil, errors.New(declined.reason)
	}
	value := &CaptureContext{Schema: CaptureContextSchema, Case: ref, Identity: evidence.Identity, Sources: []CaptureAssociation{}}
	for _, input := range evidence.Manifest.Sources {
		value.Sources = append(value.Sources, CaptureAssociation{SourceID: input.ID, Source: filepath.Base(input.Path), Basis: "unknown"})
	}
	if evidence.Collection != nil && evidence.Manifest.Provenance.Mode == bundle.Collected {
		if observed := c.observedReceiver(entry); observed != nil {
			name, valid := c.receiveSourceRevision(observed.record.Source)
			if valid {
				for i := range value.Sources {
					source := observed.record.Source
					value.Sources[i].ReceivedAt = &source
					value.Sources[i].ReceivedAtName = name
					value.Sources[i].Source = observed.record.SourceName
					value.Sources[i].Basis = "observed"
					value.Sources[i].ReceiptIdentity = observed.digest
				}
				return value, nil, nil
			}
		}
	}
	// Only an exact retained receipt supplies channel/source mapping metadata;
	// file names and MSH fields never invent it.
	raw, err := boundedFile(filepath.Join(c.root, entry+"-receipt.json"), importer.MaxDocumentBytes)
	if err == nil {
		receipt, err := importer.DecodeMappingReceipt(raw)
		if err == nil && receipt.Case.Identity == evidence.Identity && receipt.Recipe.Validate() == nil {
			for i := range value.Sources {
				for _, mapping := range receipt.Mappings {
					if mapping.SourceID == value.Sources[i].SourceID && mapping.State == importer.Mapped {
						value.Sources[i].Source = mapping.Source
						value.Sources[i].Channel = mapping.Channel
						value.Sources[i].ReceiptIdentity = digestOf(raw)
					}
				}
			}
		}
	}
	held, _, err := c.contexts()
	if err != nil {
		return nil, nil, err
	}
	for i := range value.Sources {
		source := &value.Sources[i]
		if source.Source == "" || source.Channel == "" || source.ReceiptIdentity == "" {
			continue
		}
		matches := []ItemRef{}
		for _, prior := range held {
			for _, known := range prior.Sources {
				if known.Remember && known.Source == source.Source && known.Channel == source.Channel && known.ReceivedAt != nil {
					if !slices.Contains(matches, *known.ReceivedAt) {
						matches = append(matches, *known.ReceivedAt)
					}
				}
			}
		}
		if len(matches) == 1 {
			target, declined := c.namedEnvironment(matches[0].ID)
			if target != nil && declined.state == "" && target.ref.Revision == matches[0].Revision {
				copy := matches[0]
				source.ReceivedAt = &copy
				source.Basis = "mapped"
				source.ReceivedAtName = target.name
			}
		}
	}
	return value, nil, nil
}

// resolveCaptureAssociations reads exact target revisions; saved endpoint
// metadata never proves successful exchange or grants authority.
func (c *loadedCatalog) resolveCaptureAssociations(value *CaptureContext) string {
	reason := ""
	for i := range value.Sources {
		association := &value.Sources[i]
		if association.ReceivedAt == nil {
			continue
		}
		if association.ReceivedAt.Kind == SourceItem {
			name, valid := c.receiveSourceRevision(*association.ReceivedAt)
			if valid {
				association.ReceivedAtName = name
			} else {
				reason = "The recorded receiving configuration is unavailable; original evidence remains intact."
				association.ReceivedAtName = "Recorded receiving configuration unavailable"
			}
			continue
		}
		target, declined := c.namedEnvironment(association.ReceivedAt.ID)
		if target == nil || declined.state != "" || target.ref.Revision != association.ReceivedAt.Revision {
			reason = "A recorded target association changed or is unavailable; correct its context explicitly."
			association.ReceivedAt = nil
			association.ReceivedAtName = ""
			association.Basis = "unknown"
			association.Remember = false
		} else {
			association.ReceivedAtName = target.name
		}
	}
	return reason
}
func (a *App) ReadCaptureContext(request ItemRequest) CaptureContextResult {
	return runRead(a, false, func(ctx context.Context) CaptureContextResult {
		result := CaptureContextResult{Context: request.Context}
		loaded, entry, declined := a.caseEntry(ctx, request.Context, request.Ref, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		_, source, declined := openedCase(loaded.root, entry, loaded.identities[entry])
		if source == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		value, ref, err := loaded.captureSourceContext(request.Ref, entry, source.Identity)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		result.Reason = loaded.resolveCaptureAssociations(value)
		result.State, result.Capture, result.Ref = Completed, value, ref
		return result
	})
}
func (a *App) SaveCaptureContext(request CaptureContextSaveRequest) CaptureContextResult {
	return run(a, false, true, func(ctx context.Context) CaptureContextResult {
		result := CaptureContextResult{Context: request.Context}
		if !catalog.ValidToken(request.IntentID) {
			result.refuse(Failed, "a context save names its deliberate click")
			return result
		}
		loaded, entry, declined := a.caseEntry(ctx, request.Context, request.Source, true)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		_, source, declined := openedCase(loaded.root, entry, request.Identity)
		if source == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		_, base, err := loaded.currentCaptureContext(request.Source, entry, source.Identity)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		if base != nil && (request.Base == nil || *request.Base != *base) || base == nil && request.Base != nil {
			result.refuse(Failed, "the capture context changed; read it again before saving")
			return result
		}
		value := CaptureContext{Schema: CaptureContextSchema, Case: request.Source, Identity: source.Identity, Sources: slices.Clone(request.Sources)}
		if err := validateCaptureContext(value); err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		if len(value.Sources) != len(source.Manifest.Sources) {
			result.refuse(Failed, "context names every retained source exactly once")
			return result
		}
		for i := range value.Sources {
			association := value.Sources[i]
			if association.ReceiptIdentity != "" && association.ReceivedAt != nil && association.ReceivedAt.Kind != SourceItem {
				raw, err := boundedFile(filepath.Join(loaded.root, entry+"-receipt.json"), importer.MaxDocumentBytes)
				if err != nil || digestOf(raw) != association.ReceiptIdentity {
					result.refuse(Failed, "the source mapping changed; read its context again before correcting it")
					return result
				}
			}
			value.Sources[i].ReceiptIdentity = ""
			value.Sources[i].ReceivedAtName = ""
			if !slices.ContainsFunc(source.Manifest.Sources, func(input bundle.Source) bool { return input.ID == association.SourceID }) {
				result.refuse(Failed, "context names only retained sources")
				return result
			}
			if association.Basis != "manual" && association.Basis != "unknown" {
				result.refuse(Failed, "a correction is manual; it cannot claim observed or mapped provenance")
				return result
			}
			if association.ReceivedAt != nil {
				if association.ReceivedAt.Kind == SourceItem {
					name, valid := loaded.receiveSourceRevision(*association.ReceivedAt)
					if !valid {
						result.refuse(Failed, "the receiving configuration revision is unavailable")
						return result
					}
					value.Sources[i].ReceivedAtName = name
					continue
				}
				target, declined := loaded.namedEnvironment(association.ReceivedAt.ID)
				if target == nil || declined.state != "" || target.ref.Revision != association.ReceivedAt.Revision {
					result.refuse(Failed, "the chosen target revision changed or is unavailable")
					return result
				}
				value.Sources[i].ReceivedAtName = target.name
			}
		}
		raw, err := encodeMember(value)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		draft := catalog.Draft{Kind: string(CaptureContextItem), Intent: request.IntentID, Digest: digestOf(raw), Author: a.reviewerName(), Members: []catalog.Staged{{Role: string(CaptureContextItem), File: "context.json", Data: raw}}}
		if base != nil {
			draft.ItemID = base.ID
			draft.Base = base.Revision
		}
		saved, err := loaded.store.Save(draft, verifierFor(CaptureContextItem), catalog.Options{Now: a.now, Fault: a.saveFault})
		if err != nil {
			result.refuse(Failed, fmt.Sprintf("the source context was not saved: %v", err))
			return result
		}
		result.State, result.Capture = Completed, &value
		result.Ref = &ItemRef{Kind: CaptureContextItem, ID: saved.Item.ID, Revision: saved.Item.RevisionLabel()}
		return result
	})
}

// observedCapture is the owned intake receipt for an actually finalized case.
type observedCapture struct {
	record captureRecord
	digest string
}

func (c *loadedCatalog) observedReceiver(entry string) *observedCapture {
	if c.observedReceivers == nil {
		c.observedReceivers = map[string]*observedCapture{}
		folders, _ := os.ReadDir(filepath.Join(c.root, catalog.Folder, capturesFolder))
		for _, folder := range folders {
			if !folder.IsDir() || !stagedID(folder.Name()) {
				continue
			}
			path := filepath.Join(c.root, catalog.Folder, capturesFolder, folder.Name())
			record, err := readCaptureSession(path)
			if err != nil || record.State != CaptureFinished || record.SourceType != MLLPListenerSource || record.Entry == "" || record.Source.Kind != SourceItem || !catalog.ValidID(record.Source.ID) || record.Source.Revision == "" {
				continue
			}
			raw, err := sessionFile.Read(filepath.Join(path, sessionDocument))
			if err != nil {
				continue
			}
			if _, exists := c.observedReceivers[record.Entry]; exists {
				c.observedReceivers[record.Entry] = nil
			} else {
				c.observedReceivers[record.Entry] = &observedCapture{record: record, digest: digestOf(raw)}
			}
		}
	}
	return c.observedReceivers[entry]
}
func (c *loadedCatalog) receiveSourceRevision(ref ItemRef) (string, bool) {
	index := c.document.Find(ref.ID)
	if index < 0 || ref.Kind != SourceItem || c.document.Items[index].Kind != string(SourceItem) || c.removed(c.document.Items[index]) {
		return "", false
	}
	record, ok := itemAt(c.document.Items[index], ref.Revision)
	if !ok {
		return "", false
	}
	paths, availability, _ := c.backing(record)
	if availability != ItemAvailable {
		return "", false
	}
	source, err := readCaptureSource(paths)
	if err != nil || source.Listener == nil {
		return "", false
	}
	return record.Name, true
}
