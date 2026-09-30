package desktop

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirobserve"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/operation"
)

const ConnectedObservationSchema = "readmit-connected-observation-setup/v1"

// ConnectedObservation holds typed projection and completion choices. The
// Save adapter generates source, projection and interval members together.
// Source identities are computed here, never entered in the window. FHIR
// searches use selected finite parameters and positions, not query text.
type ConnectedObservation struct {
	Schema             string                     `json:"schema"`
	Environment        string                     `json:"environment,omitzero"`
	Namespace          string                     `json:"namespace"`
	Phase              string                     `json:"phase"`
	BusinessKeys       []BusinessKeyMapping       `json:"business_keys"`
	Baseline           string                     `json:"baseline"`
	BarrierObservation string                     `json:"barrier_observation,omitzero"`
	BarrierDestination string                     `json:"barrier_destination,omitzero"`
	BarrierWork        string                     `json:"barrier_work,omitzero"`
	Completion         observeinterval.Definition `json:"completion"`
	Projection         *dataset.Projection        `json:"projection,omitzero"`
	FHIR               *FHIRSearchDraft           `json:"fhir,omitzero"`
}
type BusinessKeyMapping struct {
	Field    string `json:"field"`
	Variable string `json:"variable"`
}
type FHIRCriterion struct {
	Parameter string `json:"parameter"`
	Type      string `json:"type"`
	System    string `json:"system,omitzero"`
	Value     string `json:"value"`
}
type FHIRFieldProjection struct {
	Name     string `json:"name"`
	Field    string `json:"field"`
	Key      bool   `json:"key"`
	Required bool   `json:"required"`
}
type FHIRSearchDraft struct {
	Resource string                `json:"resource"`
	Boundary string                `json:"boundary"`
	Criteria []FHIRCriterion       `json:"criteria"`
	Fields   []FHIRFieldProjection `json:"fields"`
	Budget   fhirrest.Budget       `json:"budget"`
}

type FHIRProjectionChoice struct {
	Resource string                   `json:"resource"`
	Fields   []fhirr4.ProjectionField `json:"fields"`
}
type ConnectedVocabulary struct {
	Connection    FHIRConnection         `json:"connection"`
	Observation   ConnectedObservation   `json:"observation"`
	Search        FHIRSearchDraft        `json:"search"`
	Resources     []FHIRProjectionChoice `json:"resources"`
	ValueTypes    []string               `json:"value_types"`
	Phases        []string               `json:"phases"`
	Boundaries    []string               `json:"boundaries"`
	BaselineModes []string               `json:"baseline_modes"`
}

func connectedVocabulary() ConnectedVocabulary {
	v := ConnectedVocabulary{Connection: FHIRConnection{Schema: FHIRConnectionSchema, Version: fhirr4.Version, Classification: "unclassified"}, Observation: ConnectedObservation{Schema: ConnectedObservationSchema, Namespace: "observed", Phase: "both", Baseline: "before-run", BusinessKeys: []BusinessKeyMapping{}, Completion: observeinterval.Definition{Schema: observeinterval.Schema, Enabled: true, Mode: "snapshots", Freshness: "snapshot-only", HorizonMS: 30000, SampleMS: 1000, MaxGapMS: 3000, MaxSamples: 64, MaxRecords: 1000, MaxBytes: 16 << 20}}, Search: FHIRSearchDraft{Resource: "Appointment", Boundary: "", Criteria: []FHIRCriterion{}, Fields: []FHIRFieldProjection{}, Budget: fhirrest.Budget{Pages: 16, Rows: 1000, Bytes: 16 << 20, TimeoutMS: 30000}}, Resources: []FHIRProjectionChoice{}, ValueTypes: []string{"text", "decimal", "boolean", "date", "datetime", "code"}, Phases: []string{"before", "after", "both"}, Boundaries: []string{"authoritative-application-api", "delayed-replica", "reference-fhir-store"}, BaselineModes: []string{"before-run"}}
	for _, resource := range fhirr4.ProjectionResources() {
		v.Resources = append(v.Resources, FHIRProjectionChoice{Resource: resource, Fields: fhirr4.ProjectionFields(resource)})
	}
	return v
}

func (s FHIRSearchDraft) observation(id, server string) (fhirobserve.Observation, error) {
	o := fhirobserve.Observation{Schema: fhirobserve.Schema, ID: id, Server: server, Resource: s.Resource, Boundary: s.Boundary, Columns: []fhirobserve.Column{}, MaxRows: s.Budget.Rows, MaxValues: fhirr4.MaxNodes, Budget: s.Budget, Retry: fhirrest.Retry{MaxAttempts: 1}}
	query := url.Values{}
	if len(s.Criteria) == 0 || len(s.Criteria) > 16 {
		return o, errors.New("Choose an explicit identifier or supported search criterion")
	}
	for _, criterion := range s.Criteria {
		if !slices.Contains([]string{"token", "reference", "date", "string", "number", "uri"}, criterion.Type) || criterion.Parameter == "" || strings.HasPrefix(criterion.Parameter, "_") && criterion.Parameter != "_id" || strings.ContainsAny(criterion.Parameter, " :&=?\r\n") || criterion.Value == "" || len(criterion.Value) > 1024 || strings.ContainsAny(criterion.Value, "\r\n") {
			return o, errors.New("Choose a supported search parameter and its typed value")
		}
		value := criterion.Value
		if criterion.Parameter == "identifier" {
			if criterion.Type != "token" || criterion.System == "" || strings.ContainsAny(criterion.System, "|\r\n") || strings.Contains(criterion.Value, "|") {
				return o, errors.New("An identifier has an explicit system and value")
			}
			value = criterion.System + "|" + value
		} else if criterion.System != "" {
			return o, errors.New("Only a qualified identifier declares a system")
		}
		query.Add(criterion.Parameter, value)
	}
	o.Query = query.Encode()
	choices := fhirr4.ProjectionFields(s.Resource)
	for _, field := range s.Fields {
		if field.Field == "resource-identity" {
			o.Columns = append(o.Columns, fhirobserve.Column{Name: field.Name, Type: "text", Key: field.Key, Required: field.Required, Value: fhirobserve.Value{Kind: "identity"}})
			continue
		}
		index := slices.IndexFunc(choices, func(choice fhirr4.ProjectionField) bool { return choice.ID == field.Field })
		if index < 0 {
			return o, errors.New("A projected position is unsupported; retain the draft and choose a supported field")
		}
		choice := choices[index]
		selector := choice.Selector
		o.Columns = append(o.Columns, fhirobserve.Column{Name: field.Name, Type: choice.Type, CodeSystem: choice.CodeSystem, Key: field.Key, Required: field.Required, Repeated: choice.Repeated, Value: fhirobserve.Value{Kind: "field", Selector: &selector}})
	}
	return o, o.Validate()
}

func validateConnectedObservationDraft(scope draftScope, draft ItemDraft) ([]catalog.Staged, *ObservationDraft, []FieldProblem) {
	held := *draft.Observation
	setup := *held.Connected
	problems := []FieldProblem{}
	add := func(field, reason string) {
		problems = append(problems, FieldProblem{Field: "observation.connected." + field, Problem: reason})
	}
	if setup.Schema == "" {
		setup.Schema = ConnectedObservationSchema
	}
	if setup.Schema != ConnectedObservationSchema || !slices.Contains([]string{"before", "after", "both"}, setup.Phase) {
		add("phase", "Choose a supported before/after phase mapping")
	}
	if setup.Baseline != "before-run" {
		add("baseline", "The connected collector records a fresh baseline before this run")
	}
	staged := []catalog.Staged{}
	var columns []dataset.Column
	sourceIdentity := ""
	if setup.FHIR != nil {
		if setup.Projection != nil {
			add("projection", "Choose one source projection")
		}
		if scope.loaded == nil || setup.Environment == "" {
			add("environment", "Choose the saved FHIR environment")
		} else {
			index := scope.loaded.document.Find(setup.Environment)
			if index < 0 || scope.loaded.removed(scope.loaded.document.Items[index]) {
				add("environment", "The selected environment is no longer available")
			} else if members, err := scope.loaded.environmentOf(scope.loaded.document.Items[index]); err != nil || members.fhir == nil {
				add("environment", "Choose a readable FHIR environment")
			}
		}
		if scope.loaded != nil {
			parameters, _ := fhirSearchParameters(scope.loaded, setup.Environment, setup.FHIR.Resource)
			for _, criterion := range setup.FHIR.Criteria {
				if !slices.ContainsFunc(parameters, func(parameter fhirr4.SearchParameter) bool {
					return parameter.Name == criterion.Parameter && parameter.Type == criterion.Type
				}) {
					add("fhir.criteria", "Choose a supported parameter and its declared type; check capabilities for this environment before using other criteria")
				}
			}
		}
		id := "observation-" + binding(scope.intent, scope.item, draft.Name)[:12]
		// Keep a projection's stable ID through an edit; its identity still
		// changes with every selected field, scope and acquisition budget.
		if scope.item != "" && scope.loaded != nil {
			if index := scope.loaded.document.Find(scope.item); index >= 0 {
				paths, _, _ := scope.loaded.backing(scope.loaded.document.Items[index])
				if raw, err := savedFile.Read(paths["source"]); err == nil {
					if old, err := fhirobserve.Decode(raw); err == nil {
						id = old.ID
					}
				}
			}
		}
		o, err := setup.FHIR.observation(id, "fhir")
		if err != nil {
			add("fhir", err.Error())
		} else {
			raw, _ := encodeMember(o)
			staged = append(staged, catalog.Staged{Role: "source", File: "source.json", Data: raw})
			sourceIdentity = o.Identity()
			columns = o.TypedColumns()
		}
	} else {
		if setup.Projection == nil {
			add("projection", "Choose the typed fields this observation reads")
		} else if err := setup.Projection.Validate(); err != nil {
			add("projection", err.Error())
		} else {
			raw, _ := encodeMember(*setup.Projection)
			staged = append(staged, catalog.Staged{Role: "projection", File: "projection.json", Data: raw})
			columns = setup.Projection.Columns
		}
		legacy := held
		legacy.Connected = nil
		members, normalized, found := validateObservationDraft(scope, ItemDraft{Name: draft.Name, Observation: &legacy})
		problems = append(problems, found...)
		if len(found) == 0 && setup.Projection != nil {
			if err := observesource.ValidateDatasetProjection(normalized.Source, *setup.Projection); err != nil {
				add("projection", err.Error())
			}
		}
		if len(found) == 0 {
			held = *normalized
			sourceIdentity = held.Source.Identity()
			staged = append(staged, members...)
		}
	}
	if len(setup.BusinessKeys) == 0 || len(setup.BusinessKeys) > 16 {
		add("business_keys", "Map at least one projected business key to a run variable")
	}
	seen := map[string]bool{}
	for _, key := range setup.BusinessKeys {
		if key.Variable == "" || seen[key.Field] || !slices.ContainsFunc(columns, func(column dataset.Column) bool { return column.Name == key.Field && column.Key }) {
			add("business_keys", "A business key selects one projected key field and a run variable")
		}
		seen[key.Field] = true
	}
	definition := setup.Completion
	definition.Schema = observeinterval.Schema
	definition.Source = sourceIdentity
	definition.Namespace = setup.Namespace
	if setup.FHIR != nil {
		definition.Mode = "snapshots"
		definition.Freshness = "snapshot-only"
	}
	if setup.BarrierObservation != "" {
		barrier, err := scope.barrierObservation(setup.BarrierObservation, setup.BarrierDestination, setup.BarrierWork)
		if err != nil {
			add("barrier_observation", err.Error())
		} else {
			definition.Barrier = barrier
		}
	} else if definition.Barrier != nil {
		add("barrier_observation", "Choose the named processing barrier")
	}
	if err := definition.Validate(); err != nil {
		add("completion", "Choose complete, bounded interval limits and a supported completion policy")
	}
	setup.Completion = definition
	held.Connected = &setup
	if len(problems) > 0 {
		return nil, nil, problems
	}
	raw, _ := encodeMember(setup)
	staged = append(staged, catalog.Staged{Role: "setup", File: "setup.json", Data: raw})
	raw, _ = encodeMember(definition)
	staged = append(staged, catalog.Staged{Role: "interval", File: "interval.json", Data: raw})
	return staged, &held, nil
}

func (s draftScope) barrierObservation(id, environment, work string) (*observeinterval.Barrier, error) {
	if s.loaded == nil {
		return nil, errors.New("Choose a saved typed processing barrier")
	}
	i := s.loaded.document.Find(id)
	if i < 0 || s.loaded.removed(s.loaded.document.Items[i]) {
		return nil, errors.New("The processing barrier is unavailable")
	}
	draft, err := s.loaded.observationOf(s.loaded.document.Items[i])
	if err != nil || draft.Connected == nil || draft.Connected.Projection == nil {
		return nil, errors.New("The processing barrier requires a saved typed projection")
	}
	p := *draft.Connected.Projection
	index := s.loaded.document.Find(environment)
	if index < 0 || s.loaded.removed(s.loaded.document.Items[index]) || s.loaded.document.Items[index].Kind != string(EnvironmentItem) {
		return nil, errors.New("Choose the named processing destination")
	}
	members, err := s.loaded.environmentOf(s.loaded.document.Items[index])
	if err != nil {
		return nil, errors.New("The processing destination is unavailable")
	}
	destination := "receiver"
	if members.fhir != nil {
		destination = "fhir"
	}
	return &observeinterval.Barrier{Source: draft.Source.Identity(), Projection: p, Destination: destination, Work: work}, nil
}

func openConnectedObservation(paths map[string]string) (*ObservationDraft, error) {
	if err := verifyConnectedObservation(paths); err != nil {
		return nil, err
	}
	raw, err := savedFile.Read(paths["setup"])
	if err != nil {
		return nil, err
	}
	var setup ConnectedObservation
	if json.Unmarshal(raw, &setup, json.RejectUnknownMembers(true)) != nil {
		return nil, errors.New("The observation setup cannot be read")
	}
	held := &ObservationDraft{Connected: &setup, Source: operation.DefaultObservationSource(), Window: operation.DefaultObservationWindow()}
	if setup.FHIR == nil {
		raw, err := savedFile.Read(paths["source"])
		if err != nil {
			return nil, err
		}
		held.Source, err = observesource.DecodeSource(raw)
		if err != nil {
			return nil, err
		}
		raw, err = savedFile.Read(paths["window"])
		if err != nil {
			return nil, err
		}
		if json.Unmarshal(raw, &held.Window, json.RejectUnknownMembers(true)) != nil {
			return nil, errors.New("The legacy window cannot be read")
		}
		if path, ok := paths["links"]; ok {
			links, err := readObservationLinks(path)
			if err != nil {
				return nil, err
			}
			held.Credential = links.Credential
			withheldArguments(held)
		}
	}
	return held, nil
}

func verifyConnectedObservation(paths map[string]string) error {
	invalid := errors.New("The observation source, projection or completion is incompatible")
	raw, err := savedFile.Read(paths["setup"])
	if err != nil {
		return err
	}
	var setup ConnectedObservation
	if json.Unmarshal(raw, &setup, json.RejectUnknownMembers(true)) != nil || setup.Schema != ConnectedObservationSchema {
		return invalid
	}
	intervalRaw, err := savedFile.Read(paths["interval"])
	if err != nil {
		return err
	}
	interval, err := observeinterval.Decode(intervalRaw)
	if err != nil {
		return err
	}
	wanted, _ := encodeMember(setup.Completion)
	if !bytes.Equal(wanted, intervalRaw) {
		return invalid
	}
	sourceRaw, err := savedFile.Read(paths["source"])
	if err != nil {
		return err
	}
	if setup.FHIR != nil {
		o, err := fhirobserve.Decode(sourceRaw)
		if err != nil || o.Identity() != interval.Source {
			return invalid
		}
		compiled, err := setup.FHIR.observation(o.ID, o.Server)
		if err != nil || compiled.Identity() != o.Identity() {
			return invalid
		}
	} else {
		source, err := observesource.DecodeSource(sourceRaw)
		if err != nil || source.Identity() != interval.Source {
			return invalid
		}
		if _, _, err := operation.ValidateObservationPair(paths["source"], paths["window"]); err != nil {
			return err
		}
		projectionRaw, err := savedFile.Read(paths["projection"])
		if err != nil {
			return err
		}
		projection, err := dataset.DecodeProjection(projectionRaw)
		if err != nil {
			return err
		}
		if setup.Projection == nil || setup.Projection.Identity() != projection.Identity() {
			return invalid
		}
	}
	return nil
}

func readConnectedObservation(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	held, err := openConnectedObservation(paths)
	if err != nil {
		return view{}, err
	}
	setup := held.Connected
	kind := held.Source.Observes.Kind
	if setup.FHIR != nil {
		kind = "fhir-r4"
	}
	summary := &ObservationSummary{SourceType: kind, Enabled: setup.Completion.Enabled, Typed: true, Fields: len(setup.BusinessKeys), Completion: "full-horizon"}
	if setup.FHIR != nil {
		summary.Fields = len(setup.FHIR.Fields)
	} else if setup.Projection != nil {
		summary.Fields = len(setup.Projection.Columns)
	}
	if setup.BarrierObservation != "" {
		summary.Completion = "processing-barrier"
	}
	history := typedObservationHistory(c, ItemRequest{Ref: ItemRef{Kind: ObservationItem, ID: item.ID}}, held)
	if len(history.Collections) > 0 {
		latest := history.Collections[0]
		summary.LatestCollection = latest.ClosedAt
		summary.LatestStatus = latest.Status
	}
	return view{name: item.Name, summary: ItemSummary{Observation: summary}}, nil
}
