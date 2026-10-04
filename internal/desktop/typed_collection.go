package desktop

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirobserve"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/smartbackend"
)

const typedCollectionSchema = "readmit-desktop-typed-collection/v1"

type TypedCollectReview struct {
	Columns   []dataset.Column `json:"columns"`
	MaxPages  int              `json:"max_pages"`
	MaxRows   int              `json:"max_rows"`
	MaxBytes  int              `json:"max_bytes"`
	TimeoutMS int64            `json:"timeout_ms"`
	Meaning   string           `json:"meaning"`
}

// TypedCollectionView carries actual typed readings. A snapshot has a complete
// paging/acquisition boundary only; no standalone Collect manufactures a
// stimulus marker or asserts that the saved connected-run horizon completed.
type TypedCollectionView struct {
	Columns  []dataset.Column `json:"columns"`
	Rows     []dataset.Row    `json:"rows"`
	Meaning  string           `json:"meaning"`
	Coverage string           `json:"coverage"`
}
type typedCollectionRecord struct {
	Schema      string                   `json:"schema"`
	FHIR        *fhirobserve.Observation `json:"fhir,omitzero"`
	Projection  string                   `json:"projection,omitzero"`
	Observation string                   `json:"observation"`
	Revision    string                   `json:"revision"`
	Setup       string                   `json:"setup"`
	Source      string                   `json:"source"`
	Protocol    string                   `json:"protocol"`
	Binding     dataset.Binding          `json:"binding"`
	Base        string                   `json:"base,omitzero"`
	URL         string                   `json:"url,omitzero"`
	OpenedAt    time.Time                `json:"opened_at"`
	ClosedAt    time.Time                `json:"closed_at"`
	Status      string                   `json:"status"`
}

var typedCollectionFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "typed observation collection", RequiredFiles: []string{"manifest.json", "identity.sha256"}, Nested: []string{"acquisition"}, AllowFile: func(n string) bool { return n == "manifest.json" || n == "identity.sha256" }, MaxFiles: 1100, MaxFileBytes: 32 << 20, MaxBytes: 280 << 20}, Seal: artifactdir.DirectoryHash(typedCollectionSchema)}

type typedCollectBinding struct {
	root, output string
	ref          ItemRef
	setup        ConnectedObservation
	source       observesource.Source
	fhir         *fhirobserve.Observation
	expect       fhirobserve.Expect
	policy       string
}

func bindTypedCollect(a *App, ctx context.Context, request PrepareActionRequest, held bool, loaded *loadedCatalog, items []CatalogItem, record catalog.Item, paths map[string]string) (*boundAction, refusal) {
	draft, err := openConnectedObservation(paths)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	setup := *draft.Connected
	if setup.Capture != nil {
		return nil, refusal{Failed, "live HL7 collection executes only inside the reviewed connected Run lifecycle"}
	}
	destination, declined := destinationFor(loaded.root, "", "typed-observation")
	if destination.Name == "" {
		return nil, declined
	}
	typed := &typedCollectBinding{root: loaded.root, output: destination.Name, ref: items[0].Ref, setup: setup, source: draft.Source}
	if setup.FHIR == nil {
		raw, err := savedFile.Read(paths["source"])
		if err != nil {
			return nil, refusal{Failed, "The saved source is unavailable"}
		}
		source, err := observesource.DecodeSource(raw)
		if err != nil {
			return nil, refusal{Failed, "The saved source is unsupported"}
		}
		typed.source = source
	}
	review := &CollectReview{Revision: items[0].Ref.Revision, Source: items[0].Name, SourceType: items[0].Summary.Observation.SourceType, Scope: setup.Namespace, Bounds: observewindow.Rule{}, Destination: sourceDestination(draft.Source)}
	review.Typed = &TypedCollectReview{Columns: []dataset.Column{}, Meaning: "Read one source snapshot; the saved run horizon and barrier are used only by an actual test run."}
	bound := &boundAction{action: CollectObservationAction, origin: request, typedCollect: typed, review: ActionReview{Items: items[:1], Ready: true, Collect: review, Destination: ReviewDestination{Name: items[0].Name, Address: review.Destination, Output: destination.Name}}}
	deny := func(reason string) { bound.review.Ready = false; bound.review.Refusal = reason }
	if !setup.Completion.Enabled {
		deny("Enable this source and save before collecting")
	}
	if setup.FHIR != nil {
		// The legacy source is unused by a FHIR observation. A refused
		// review still describes the selected FHIR destination.
		review.Destination = ""
		bound.review.Destination.Address = ""
		raw, err := savedFile.Read(paths["source"])
		if err != nil {
			return nil, refusal{Failed, "The saved search is unavailable"}
		}
		savedSearch, err := fhirobserve.Decode(raw)
		if err != nil {
			return nil, refusal{Failed, "The saved search is unsupported"}
		}
		review.Typed.Columns = savedSearch.TypedColumns()
		review.Typed.MaxPages = savedSearch.Budget.Pages
		review.Typed.MaxRows = savedSearch.Budget.Rows
		review.Typed.MaxBytes = savedSearch.Budget.Bytes
		review.Typed.TimeoutMS = savedSearch.Budget.TimeoutMS
		i := loaded.document.Find(setup.Environment)
		if i < 0 || loaded.removed(loaded.document.Items[i]) {
			deny("Choose an available FHIR environment")
			return bound, refusal{}
		}
		environment := loaded.read(loaded.document.Items[i])
		ref := environment.Ref
		connection, declined := bindFHIRCheck(a, ctx, PrepareActionRequest{Context: request.Context, Action: CheckFHIRCapabilitiesAction, Items: []ItemRef{ref}}, held)
		if connection == nil {
			return nil, declined
		}
		if connection.fhirCheck != nil {
			address := connection.fhirCheck.connection.Base
			if search, err := savedSearch.URL(address, nil); err == nil {
				address = search
			}
			review.Destination = address
			bound.review.Destination.Address = address
		}
		if !connection.review.Ready {
			deny(connection.review.Refusal)
			return bound, refusal{}
		}
		check, err := readFHIRCheck(loaded.root, ref.ID, CheckFHIRCapabilitiesAction)
		if err != nil || check.Check.Revision != ref.Revision || len(check.Capability) == 0 {
			deny("Check capabilities for the current environment revision before collecting")
			return bound, refusal{}
		}
		raw, err = savedFile.Read(paths["source"])
		if err != nil {
			return nil, refusal{Failed, "The saved search is unavailable"}
		}
		o, err := fhirobserve.Decode(raw)
		if err != nil {
			return nil, refusal{Failed, "The saved search is unsupported"}
		}
		if len(o.Variables()) > 0 {
			deny("This search uses run variables. Collect it through the test run that binds them.")
			return bound, refusal{}
		}
		fixed := connection.fhirCheck
		address, err := o.URL(fixed.connection.Base, nil)
		if err != nil {
			return nil, refusal{Failed, "The saved search cannot be resolved"}
		}
		http, ok := fixed.http.DeclarationV2()
		if !ok {
			return nil, refusal{Failed, "The saved transport is unavailable"}
		}
		http.HTTP.URL = address
		http.HTTP.Operation = "fhir-search"
		http.HTTP.Source = o.Identity()
		http.HTTP.TimeoutMS = o.Budget.TimeoutMS
		http.HTTP.MaxBytes = min(o.Budget.Bytes, 16<<20)
		spec := fhirrest.Spec{Schema: fhirrest.PlanSchema, Base: fixed.connection.Base, HTTP: http, Capability: check.Capability, Budget: o.Budget, Retry: o.Retry}
		specRaw, _ := encodeMember(spec)
		fixed.observation, err = fhirrest.Prepare(specRaw, fixed.policyRaw)
		if err != nil {
			deny("The selected resource or search criterion is unsupported by the recorded capabilities")
			return bound, refusal{}
		}
		typed.fhir = &o
		review.Typed.Columns = o.TypedColumns()
		review.Typed.MaxPages = o.Budget.Pages
		review.Typed.MaxRows = o.Budget.Rows
		review.Typed.MaxBytes = o.Budget.Bytes
		review.Typed.TimeoutMS = o.Budget.TimeoutMS
		typed.expect = fhirobserve.Expect{Base: fixed.connection.Base, URL: address, Binding: dataset.Binding{Run: "observation-" + binding(items[0].Ref.ID)[:12], Phase: "after", Source: o.Identity(), Namespace: setup.Namespace}}
		bound.fhirCheck = fixed
		review.Destination = address
		bound.review.Destination.Address = address
		bound.binding = binding(string(CollectObservationAction), connection.binding, items[0].Ref.ID, items[0].Ref.Revision, memberDigest(record, "source"), memberDigest(record, "setup"), memberDigest(record, "interval"), string(specRaw))
	} else {
		limits := setup.Projection.Limits
		review.Typed.Columns = setup.Projection.Columns
		review.Typed.MaxRows = limits.MaxRows
		review.Typed.MaxBytes = limits.MaxBytes
		review.Typed.TimeoutMS = limits.TimeoutMS
		if request.Destination != nil {
			if i := loaded.document.Find(request.Destination.ID); i >= 0 {
				environment, _, _ := loaded.backing(loaded.document.Items[i])
				typed.policy = environment["policy"]
			}
		}
		bound.binding = binding(string(CollectObservationAction), loaded.root, items[0].Ref.ID, items[0].Ref.Revision, memberDigest(record, "source"), memberDigest(record, "projection"), memberDigest(record, "setup"), memberDigest(record, "interval"), fileDigest(typed.policy), a.reviewer(), a.policyBinding(ctx, true, held))
	}
	return bound, refusal{}
}

// typedCollectAuthorized rebinds the saved whole object before every source
// effect. It checks the existing backend-held review, never a frontend grant.
func typedCollectAuthorized(a *App, ctx context.Context, bound *boundAction) error {
	deny := errors.New("the observation review is no longer authorized")
	if bound.executionReview == nil || ctx.Err() != nil {
		return deny
	}
	lease := bound.executionReview
	store := &a.reviews
	store.mu.Lock()
	review := store.reviews[lease.token]
	valid := review != nil && !review.withdrawn && review.consumed == lease.intent && store.running == lease.intent && a.now().Before(review.expires)
	store.mu.Unlock()
	if !valid {
		return deny
	}
	guard, _ := a.selectedOperation()
	if guard.CheckExecutionContext(ctx) != nil {
		return deny
	}
	fresh, declined := bindCollect(a, ctx, bound.origin, true)
	if fresh == nil || declined.reason != "" || !fresh.review.Ready || fresh.binding != bound.binding {
		return deny
	}
	return nil
}

func executeTypedCollect(a *App, ctx context.Context, bound *boundAction) ReviewedActionResult {
	result := ReviewedActionResult{State: Failed, Outcome: ActionRefused}
	if typedCollectAuthorized(a, ctx, bound) != nil {
		result.Reason = "Review this observation again"
		return result
	}
	typed := bound.typedCollect
	a.reach(reachingTarget{ref: "observation:" + typed.ref.ID, name: bound.review.Items[0].Name, kind: ConnectionSource, destination: bound.review.Destination.Address})
	w, err := artifactdir.Create(filepath.Join(typed.root, typed.output), typedCollectionFamily, artifactdir.Durable)
	if err != nil {
		result.Reason = "The observation output cannot be created"
		return result
	}
	defer w.Close()
	record := typedCollectionRecord{Schema: typedCollectionSchema, Observation: typed.ref.ID, Revision: typed.ref.Revision, Setup: binding(string(mustMember(typed.setup))), Source: typed.setup.Completion.Source, Protocol: "typed", OpenedAt: time.Now().UTC(), Status: "unavailable"}
	view := &TypedCollectionView{Rows: []dataset.Row{}, Columns: []dataset.Column{}, Coverage: "unavailable", Meaning: "One source snapshot; it does not establish the connected run's full observation horizon."}
	if typed.fhir != nil {
		record.Protocol = "fhir-r4"
		record.FHIR = typed.fhir
		record.Projection = typed.fhir.ProjectionIdentity()
		record.Binding = typed.expect.Binding
		record.Base = typed.expect.Base
		record.URL = typed.expect.URL
		fixed := bound.fhirCheck
		lease := bound.executionReview
		actor := networkaction.Actor{Kind: "action-review", ID: "reviewer-" + binding(a.reviewer())[:16], Generation: "review-" + binding(lease.token)[:16], EvidenceIdentity: bound.binding, Expires: lease.expires}
		authority := fhirReviewAuthority{app: a, bound: bound, actor: actor}
		var provider networkaction.RuntimeProvider
		var session *smartbackend.Session
		if fixed.client != nil {
			session = fixed.client.Session(authority, nil)
			defer session.Disconnect()
			provider = session
		}
		sample, e := fhirobserve.Acquire(ctx, *typed.fhir, typed.expect, fixed.observation, authority, provider, nil, filepath.Join(w.Path(), "acquisition"))
		err = e
		if sample != nil {
			view.Rows = sample.Record().Table.Rows
			view.Columns = typed.fhir.TypedColumns()
			view.Coverage = sample.Status()
			view.Meaning = fhirobserve.Meaning(typed.fhir.Boundary) + " One source snapshot; no connected-run horizon was completed."
			record.Status = sample.Status()
		}
	} else {
		record.Projection = typed.setup.Projection.Identity()
		record.Binding = dataset.Binding{Run: "observation-" + binding(typed.ref.ID)[:12], Phase: "after", Source: typed.source.Identity(), Namespace: typed.setup.Namespace}
		var policy *sendpolicy.Policy
		if typed.policy != "" {
			policyValue, e := operation.ReadSendPolicy(typed.policy)
			if e != nil {
				err = e
			} else {
				policy = &policyValue
			}
		}
		if err == nil {
			snapshot, e := observesource.CollectDataset(ctx, observesource.DatasetRequest{SourceRoot: typed.root, Source: typed.source, Projection: *typed.setup.Projection, Binding: record.Binding, Output: filepath.Join(w.Path(), "acquisition"), Policy: policy, Authorize: func(ctx context.Context) error { return typedCollectAuthorized(a, ctx, bound) }})
			err = e
			if snapshot != nil {
				document := snapshot.Document()
				view.Columns = document.Projection.Columns
				view.Rows = document.Rows
				view.Coverage = document.Status
				record.Status = document.Status
			}
		}
	}
	record.ClosedAt = time.Now().UTC()
	raw, _ := encodeMember(record)
	if w.WriteFile("manifest.json", raw) != nil {
		result.Reason = "The collection could not be retained"
		return result
	}
	if _, e := w.Seal(nil); e != nil {
		result.Reason = "The collection could not be sealed"
		return result
	}
	row := typedCollectionRow(typed.output, record, view, false)
	result.Collected = &row
	result.TypedCollection = view
	if err == nil && record.Status == "complete" {
		result.State = Completed
		result.Outcome = ActionCompleted
	} else {
		result.Reason = "The source could not be completely observed; inspect the retained collection"
	}
	if ctx.Err() != nil {
		result.State = Cancelled
		result.Outcome = ActionCancelled
	}
	return result
}

func mustMember(value any) []byte { raw, _ := encodeMember(value); return raw }

func typedCollectionRow(entry string, record typedCollectionRecord, view *TypedCollectionView, incompatible bool) CollectionRow {
	row := CollectionRow{Entry: entry, ClosedAt: stampedTime(record.ClosedAt), Status: record.Status, Trustworthy: record.Status == "complete" && !incompatible, Reason: "One snapshot; the connected-run observation horizon is separate"}
	if incompatible {
		row.Status = "incompatible"
		row.Reason = "The source, projection or completion was edited; this collection remains evidence of its earlier scope"
	} else if record.Status == "complete" && view != nil {
		count := len(view.Rows)
		row.Records = &count
	}
	return row
}

func readTypedCollection(root, entry string, draft *ObservationDraft) (typedCollectionRecord, *TypedCollectionView, error) {
	path, err := artifactpath.Child(root, entry)
	if err != nil {
		return typedCollectionRecord{}, nil, err
	}
	files, err := artifactdir.Read(path, typedCollectionFamily.Layout)
	if err != nil {
		return typedCollectionRecord{}, nil, err
	}
	if string(bytes.TrimSpace(files["identity.sha256"])) != artifactdir.Identity(typedCollectionSchema, files) {
		return typedCollectionRecord{}, nil, errors.New("The typed collection cannot be verified")
	}
	var record typedCollectionRecord
	if json.Unmarshal(files["manifest.json"], &record, json.RejectUnknownMembers(true)) != nil || record.Schema != typedCollectionSchema {
		return record, nil, errors.New("The collection cannot be read")
	}
	view := &TypedCollectionView{Rows: []dataset.Row{}, Columns: []dataset.Column{}, Coverage: record.Status, Meaning: "One source snapshot; no connected-run horizon was completed"}
	if record.Status != "complete" {
		return record, view, nil
	}
	if record.Protocol == "fhir-r4" {
		if record.FHIR == nil || record.FHIR.Identity() != record.Source || record.FHIR.ProjectionIdentity() != record.Projection {
			return record, nil, errors.New("The retained projection is incompatible")
		}
		sample, err := fhirobserve.Open(context.Background(), filepath.Join(path, "acquisition"), *record.FHIR, fhirobserve.Expect{Base: record.Base, URL: record.URL, Binding: record.Binding})
		if err != nil || !artifactdir.MatchesSubtree(files, "acquisition", fhirobserve.SampleSchema, sample.Identity()) {
			return record, nil, errors.New("The retained acquisition cannot be verified")
		}
		view.Rows = sample.Record().Table.Rows
		view.Columns = record.FHIR.TypedColumns()
		view.Coverage = sample.Status()
		view.Meaning = fhirobserve.Meaning(record.FHIR.Boundary) + " One snapshot; no connected-run horizon was completed"
	} else {
		snapshot, err := dataset.Open(context.Background(), filepath.Join(path, "acquisition", "dataset"))
		if err != nil {
			return record, nil, err
		}
		if !artifactdir.MatchesSubtree(files, "acquisition/dataset", dataset.Schema, snapshot.Identity()) {
			return record, nil, errors.New("The retained acquisition cannot be verified")
		}
		document := snapshot.Document()
		if document.Binding != record.Binding || document.Projection.Identity() != record.Projection || document.Binding.Source != record.Source {
			return record, nil, errors.New("The retained collection scope is incompatible")
		}
		view.Rows = document.Rows
		view.Columns = document.Projection.Columns
		view.Coverage = document.Status
	}
	if view.Coverage != record.Status {
		return record, nil, errors.New("The retained collection outcome is incompatible")
	}
	return record, view, nil
}

func typedObservationHistory(c *loadedCatalog, request ItemRequest, draft *ObservationDraft) ObservationHistoryResult {
	result := ObservationHistoryResult{State: Empty, Context: request.Context, Collections: []CollectionRow{}}
	if draft.Connected != nil && draft.Connected.Capture != nil {
		result.Reason = "live capture evidence is retained in its connected run"
		return result
	}
	entries, err := os.ReadDir(c.root)
	if err != nil {
		result.refuse(Failed, "The project cannot be read")
		return result
	}
	for _, entry := range entries {
		if !entry.IsDir() || !declares(filepath.Join(c.root, entry.Name(), "manifest.json"), typedCollectionSchema) {
			continue
		}
		record, view, err := readTypedCollection(c.root, entry.Name(), draft)
		if err != nil || record.Observation != request.Ref.ID {
			continue
		}
		incompatible := record.Setup != binding(string(mustMember(*draft.Connected)))
		result.Collections = append(result.Collections, typedCollectionRow(entry.Name(), record, view, incompatible))
	}
	slices.SortFunc(result.Collections, func(a, b CollectionRow) int { return strings.Compare(stampOf(b.ClosedAt), stampOf(a.ClosedAt)) })
	if len(result.Collections) > 0 {
		result.State = Completed
	}
	return result
}

func typedObservationInspection(c *loadedCatalog, request CompletionRequest, draft *ObservationDraft) CompletionInspectionResult {
	result := CompletionInspectionResult{Context: request.Context}
	record, view, err := readTypedCollection(c.root, request.Entry, draft)
	if err != nil || record.Observation != request.Ref.ID {
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		result.refuse(Failed, "This collection cannot be read for the observation")
		return result
	}
	incompatible := record.Setup != binding(string(mustMember(*draft.Connected)))
	result.State = Completed
	result.Completion = &CompletionInspection{CollectionRow: typedCollectionRow(request.Entry, record, view, incompatible), OpenedAt: stampedTime(record.OpenedAt), Samples: 1, Supported: !incompatible, Typed: view}
	return result
}
