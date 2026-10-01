package desktop

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/assertionauthor"
	"github.com/bharm16/readmit/internal/casegen"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirevidence"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrequest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/profilepackage"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/scenariogen"
	"github.com/bharm16/readmit/internal/strictdoc"
)

const FHIRProfileSchema = "readmit-fhir-profile/v1"
const FHIRProfilePackageSchema = "readmit-fhir-profile-package/v1"
const FHIRProfileVersionSchema = "readmit-fhir-profile-version/v1"
const FHIRScenarioSchema = "readmit-fhir-scenario/v1"
const FHIRCheckGroupSchema = "readmit-fhir-check-group/v1"
const maxProtocolLibraryBytes = 4 << 20

// FHIRProfileDefinition retains exact profile/package requirements. A
// validator names a saved connection revision, and Capability pins its
// immutable local capability. Neither the pin nor a package selection runs
// the worker, resolves a dependency or contacts a canonical URL.
type FHIRProfileDefinition struct {
	Schema       string                     `json:"schema"`
	Identity     localprofile.Identity      `json:"identity"`
	ResourceType string                     `json:"resource_type"`
	Profiles     []fhirvalidator.Canonical  `json:"profiles"`
	Packages     []fhirvalidator.PackageRef `json:"packages"`
	Requirements fhirvalidator.Requirements `json:"requirements"`
	Validator    *ItemRef                   `json:"validator,omitzero"`
	Capability   string                     `json:"capability,omitzero"`
}

type fhirGenerationStep struct {
	ID         string `json:"id"`
	After      string `json:"after"`
	Entry      string `json:"entry"`
	Identity   string `json:"identity"`
	SourceKind string `json:"source_kind"`
}

type fhirGenerationRecord struct {
	Schema          string               `json:"schema"`
	ContentIdentity string               `json:"content_identity"`
	TemplateSHA256  string               `json:"template_sha256"`
	Seed            uint64               `json:"seed"`
	BaseTime        string               `json:"base_time"`
	Steps           []fhirGenerationStep `json:"steps"`
}

func fhirStepDeclaration(step FHIRScenarioStep) fhirevidence.Declaration {
	declaration := fhirevidence.Declaration{SourceKind: step.SourceKind, Context: fhirr4.Context{Version: fhirr4.Version, MediaType: "application/fhir+json"}}
	if step.Request != nil {
		declaration.Context.Base = step.Request.Base
		declaration.Request = &fhirevidence.RequestDeclaration{Method: step.Request.Method, URL: step.Request.URL, Headers: step.Request.Headers}
	}
	return declaration
}

func (a *App) generateFHIRScenarioCases(ctx context.Context, loaded *loadedCatalog, source catalog.Item, name, path string, request ScenarioCasesRequest, result ScenarioCasesResult) ScenarioCasesResult {
	raw, err := boundedFile(path, maxProtocolLibraryBytes)
	if err != nil {
		result.refuse(Failed, "the saved FHIR scenario cannot be read")
		return result
	}
	template, err := decodeFHIRScenario(raw)
	if err != nil {
		result.refuse(Failed, err.Error())
		return result
	}
	if request.Profile.ID != "" || request.Pack != nil || !emptyGenerationSettings(request.Settings) {
		result.refuse(Failed, "FHIR templates use their complete saved resource and request facts; v2 generation parameters cannot be silently discarded")
		return result
	}
	identity := digestOf(raw)
	result.Protocol, result.FHIRVersion, result.Family = "fhir-r4", fhirr4.Version, "FHIR"
	result.Seed, result.BaseTime, result.GeneratorVersion, result.ContentIdentity = template.Seed, template.BaseTime, "readmit-fhir-template-v1", identity
	provenance := fhirevidence.Provenance{Mode: "generated", Derivation: identity}
	steps := []fhirGenerationStep{}
	replayed := true
	a.startCaseProgress()
	defer a.endCaseProgress()
	for i, step := range template.Steps {
		if ctx.Err() != nil {
			result.refuse(Cancelled, "generation stopped; any complete resource/request evidence already written is retained for a retry")
			return result
		}
		declaration := fhirStepDeclaration(step)
		base := "generated-fhir-" + identity[:12] + "-" + strconv.Itoa(i+1) + "-" + actionSlug(step.ID)
		var artifact *fhirevidence.Artifact
		entry := ""
		for attempt := 1; attempt <= maxGenerationAttempts; attempt++ {
			candidate := base
			if attempt > 1 {
				candidate += "-" + strconv.Itoa(attempt)
			}
			destination := filepath.Join(loaded.root, candidate)
			if _, err := os.Lstat(destination); err == nil {
				held, err := fhirevidence.Open(ctx, destination)
				if err == nil && reflect.DeepEqual(held.Manifest.Declaration, declaration) && held.Manifest.Provenance == provenance && bytes.Equal(held.Raw(), []byte(step.Document)) {
					artifact, entry = held, candidate
					break
				}
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				result.refuse(refusalState(err), "the generated case destination cannot be read")
				return result
			}
			artifact, err = fhirevidence.Create(ctx, destination, declaration, []byte(step.Document), provenance)
			if err != nil {
				result.refuse(refusalState(err), "the generated resource/request evidence was not completely written: "+err.Error())
				return result
			}
			entry, replayed = candidate, false
			break
		}
		if artifact == nil {
			result.refuse(Failed, "too many incomplete attempts to retain this generated FHIR step")
			return result
		}
		steps = append(steps, fhirGenerationStep{ID: step.ID, After: step.After, Entry: entry, Identity: artifact.Identity, SourceKind: step.SourceKind})
		a.reportCaseProgress(casegen.Progress{Stage: casegen.Encoding})
	}
	record := fhirGenerationRecord{Schema: "readmit-fhir-template-generation/v1", ContentIdentity: identity, TemplateSHA256: digestOf(raw), Seed: template.Seed, BaseTime: template.BaseTime, Steps: steps}
	recordBytes, err := encodeMember(record)
	if err != nil {
		result.refuse(Failed, "the retained step mapping cannot be encoded")
		return result
	}
	for attempt := 1; attempt <= maxGenerationAttempts; attempt++ {
		entry := "generated-fhir-" + identity[:12]
		if attempt > 1 {
			entry += "-" + strconv.Itoa(attempt)
		}
		entry += "-generation.json"
		path := filepath.Join(loaded.root, entry)
		if held, err := boundedFile(path, maxProtocolLibraryBytes); err == nil {
			if bytes.Equal(held, recordBytes) {
				result.Record = entry
				break
			}
			continue
		}
		if err := newWorkspaceEntry.Create(path, recordBytes); err != nil {
			result.refuse(refusalState(err), "the cases are retained but their step mapping could not be written")
			return result
		}
		result.Record, replayed = entry, false
		break
	}
	if result.Record == "" {
		result.refuse(Failed, "the cases are retained but no complete step mapping could be published")
		return result
	}
	for _, step := range steps {
		if loaded.registered(step.Entry) {
			continue
		}
		if _, err := operation.RegisterCase(loaded.root, step.Entry, operation.CaseRegistration{Title: name + " · " + step.ID, Tags: []string{SyntheticTag}}); err != nil {
			result.refuse(Failed, "the FHIR evidence is retained but one generated step could not be registered: "+err.Error())
			return result
		}
	}
	loaded, declined := a.loadCatalog(ctx, request.Context, true)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	revision, _ := strconv.Atoi(source.RevisionLabel())
	for _, step := range steps {
		index := loaded.document.ByEntry(string(CaseItem), step.Entry)
		if index < 0 {
			result.refuse(Failed, "the retained generated FHIR case is not in the project catalog")
			return result
		}
		item := loaded.document.Items[index]
		if revision > 0 {
			if _, err := loaded.store.RecordOrigin(catalog.Origin{Item: item.ID, Kind: catalog.OriginScenario, Source: source.ID, Revision: revision}, a.now()); err != nil {
				result.refuse(Failed, "the cases are retained but the scenario revision lineage could not be recorded")
				return result
			}
		}
		result.Cases = append(result.Cases, GeneratedCase{Case: loaded.read(item).Ref, Entry: step.Entry, Identity: step.Identity, Row: "template", Variant: "baseline",
			Polarity: "authored", Step: step.ID, After: step.After, SourceKind: step.SourceKind, Messages: 1,
			Phases: []casegen.Phase{{ID: step.ID, Event: step.SourceKind, Expect: "authored-template", Occurrences: []int{1}}}})
	}
	result.State, result.Replayed = Completed, replayed
	return result
}

// Empty typed arrays from a bound window carry no business fact or mutation.
// An authored member, including a wire value or ancestry, remains a clause
// and cannot be discarded when the saved scenario uses another protocol.
func emptyGenerationSettings(settings CaseGenerationSettings) bool {
	b := settings.Bindings
	return settings.Wire == (casegen.Wire{}) && settings.DerivedFrom == "" && len(settings.Variants) == 0 &&
		len(b.Patients)+len(b.Visits)+len(b.Appointments)+len(b.Resources)+len(b.Orders)+len(b.Edits) == 0
}

// A bound zero-value plan represents its raw template as JSON null and can
// carry empty arrays. These are empty carriers, while every authored scalar,
// row, mutation or non-null template remains a retained v2 clause.
func emptyScenarioPlan(plan scenariogen.Plan) bool {
	template := bytes.TrimSpace(plan.Template)
	return plan.Schema == "" && plan.GeneratorVersion == "" && plan.Seed == 0 && len(plan.Rows) == 0 && len(plan.Variants) == 0 &&
		(len(template) == 0 || bytes.Equal(template, []byte("null")))
}

// FHIRProfileAvailability is the installed metadata's scope, never a
// runtime or validation verdict. The local worker has not been launched or
// checked by this read.
type FHIRProfileAvailability struct {
	State      string                    `json:"state"`
	Reason     string                    `json:"reason,omitzero"`
	Capability string                    `json:"capability,omitzero"`
	Packages   []fhirvalidator.Package   `json:"packages"`
	Profiles   []fhirvalidator.Canonical `json:"profiles"`
}

// FHIRScenarioTemplate holds complete resource bytes and HTTP facts for
// every authored step. They are saved whole in the existing scenario flow;
// generation retains the bytes, never substitutes a v2 lifecycle preview.
type FHIRScenarioTemplate struct {
	Schema   string                `json:"schema"`
	Identity localprofile.Identity `json:"identity"`
	Seed     uint64                `json:"seed"`
	BaseTime string                `json:"base_time"`
	Steps    []FHIRScenarioStep    `json:"steps"`
}

type FHIRScenarioStep struct {
	ID         string               `json:"id"`
	After      string               `json:"after"`
	SourceKind string               `json:"source_kind"`
	Document   string               `json:"document"`
	Request    *FHIRScenarioRequest `json:"request,omitzero"`
}

type FHIRScenarioRequest struct {
	Base    string              `json:"base"`
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers fhirrequest.Headers `json:"headers"`
}

// FHIRCheckGroup couples the shared typed dataset assertions to their
// finite Go FHIR projections. Its bindings retain exact source and
// projection identities; no HL7 selector is manufactured for JSON.
type FHIRCheckGroup struct {
	Schema      string                       `json:"schema"`
	Name        string                       `json:"name"`
	Set         assertion.DatasetSetDocument `json:"set"`
	Projections []fhirr4.Projection          `json:"projections"`
}

// readFHIRCheckGroup reads each assertion through the shared dataset
// evaluator's reader. Unsupported assertions retain exact text and position,
// so they remain visible and block the editor's whole Save.
func readFHIRCheckGroup(raw []byte) (*FHIRCheckGroup, []assertionauthor.UnsupportedClause, error) {
	var held struct {
		Schema string `json:"schema"`
		Name   string `json:"name"`
		Set    struct {
			Schema     string                     `json:"schema"`
			Bindings   []assertion.DatasetBinding `json:"bindings"`
			Assertions []jsontext.Value           `json:"assertions"`
		} `json:"set"`
		Projections []fhirr4.Projection `json:"projections"`
	}
	if err := protocolLibraryReader(FHIRCheckGroupSchema, "name", "set", "projections").Decode(raw, &held); err != nil {
		return nil, nil, err
	}
	if !catalog.ValidName(held.Name) || held.Set.Schema != assertion.DatasetSchema || held.Set.Bindings == nil || held.Set.Assertions == nil ||
		len(held.Set.Assertions) < 1 || len(held.Set.Assertions) > assertion.MaxAssertions || len(held.Projections) < 1 || len(held.Projections) > 32 {
		return nil, nil, errors.New("a FHIR check group retains its name, typed bindings, finite projections and complete assertion list")
	}
	projections := map[string]bool{}
	for _, projection := range held.Projections {
		if projection.Validate() != nil {
			return nil, nil, errors.New("a FHIR check uses a supported finite typed projection")
		}
		raw, err := json.Marshal(projection, json.Deterministic(true))
		if err != nil {
			return nil, nil, errors.New("the FHIR projection cannot be encoded")
		}
		projections[dataset.Digest(raw)] = true
	}
	for _, binding := range held.Set.Bindings {
		if !projections[binding.ProjectionIdentity] {
			return nil, nil, errors.New("every FHIR binding pins one of the complete retained projections")
		}
	}
	group := &FHIRCheckGroup{Schema: held.Schema, Name: held.Name, Set: assertion.DatasetSetDocument{Schema: held.Set.Schema, Bindings: held.Set.Bindings, Assertions: []assertion.DatasetAssertion{}}, Projections: held.Projections}
	unsupported := []assertionauthor.UnsupportedClause{}
	seen := map[string]bool{}
	for position, clause := range held.Set.Assertions {
		var named struct {
			ID       string `json:"id"`
			Operator string `json:"operator"`
		}
		if json.Unmarshal(clause, &named) != nil || named.ID == "" || seen[named.ID] {
			return nil, nil, errors.New("each FHIR assertion keeps a distinct identity")
		}
		seen[named.ID] = true
		data, err := json.Marshal(struct {
			Schema     string                     `json:"schema"`
			Bindings   []assertion.DatasetBinding `json:"bindings"`
			Assertions []jsontext.Value           `json:"assertions"`
		}{assertion.DatasetSchema, held.Set.Bindings, []jsontext.Value{clause}}, json.Deterministic(true))
		if err != nil {
			return nil, nil, errors.New("the FHIR assertion cannot be encoded")
		}
		decoded, err := assertion.DecodeDatasets(data)
		if err != nil {
			unsupported = append(unsupported, assertionauthor.UnsupportedClause{ID: named.ID, Operator: named.Operator, Raw: string(clause), Position: position,
				Reason: "this release does not evaluate the complete typed assertion as written"})
			continue
		}
		group.Set.Assertions = append(group.Set.Assertions, decoded.Document().Assertions[0])
	}
	return group, unsupported, nil
}

func fhirCheckGroupDocument(name string, group CheckGroupDraft) ([]byte, error) {
	held := *group.FHIR
	if held.Schema == "" {
		held.Schema = FHIRCheckGroupSchema
	}
	if held.Name == "" {
		held.Name = name
	}
	data, err := encodeMember(held)
	if err != nil {
		return nil, err
	}
	var raw map[string]jsontext.Value
	if json.Unmarshal(data, &raw) != nil {
		return nil, errors.New("the FHIR check group cannot be encoded")
	}
	var set map[string]jsontext.Value
	if json.Unmarshal(raw["set"], &set) != nil {
		return nil, errors.New("the typed check set cannot be encoded")
	}
	var known []jsontext.Value
	_ = json.Unmarshal(set["assertions"], &known)
	clauses := make([]jsontext.Value, len(known)+len(group.Unsupported))
	for _, unsupported := range group.Unsupported {
		if unsupported.Position < 0 || unsupported.Position >= len(clauses) || clauses[unsupported.Position] != nil || !jsontext.Value(unsupported.Raw).IsValid() {
			return nil, errors.New("the unsupported assertion no longer has its complete original text and position")
		}
		clauses[unsupported.Position] = jsontext.Value(unsupported.Raw)
	}
	at := 0
	for i := range clauses {
		if clauses[i] == nil {
			clauses[i], at = known[at], at+1
		}
	}
	set["assertions"], err = json.Marshal(clauses, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	raw["set"], err = json.Marshal(set, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	return encodeMember(raw)
}

func validateFHIRCheckGroupDraft(draft ItemDraft) ([]catalog.Staged, *CheckGroupDraft, []FieldProblem) {
	problem := func(reason string) ([]catalog.Staged, *CheckGroupDraft, []FieldProblem) {
		return nil, nil, []FieldProblem{{Field: "check_group.fhir", Problem: reason}}
	}
	group := *draft.CheckGroup
	if len(group.Unsupported) > 0 {
		return problem("this group retains unsupported typed assertions; no clause is removed and nothing is saved")
	}
	if len(group.Set.Assertions) != 0 {
		return problem("choose one check protocol; retained v2 assertions cannot be discarded implicitly")
	}
	data, err := fhirCheckGroupDocument(draft.Name, group)
	if err != nil {
		return problem(err.Error())
	}
	held, unsupported, err := readFHIRCheckGroup(data)
	if err != nil {
		return problem(err.Error())
	}
	if len(unsupported) > 0 {
		return problem("the group contains assertions this release cannot evaluate as written")
	}
	group.FHIR, group.Unsupported = held, []assertionauthor.UnsupportedClause{}
	ids := []string{}
	for _, clause := range held.Set.Assertions {
		ids = append(ids, clause.ID)
	}
	names, problems := checkNames(group.Names, ids)
	if len(problems) > 0 {
		return nil, nil, problems
	}
	metadata, err := metadataMember(libraryMetadata{Names: names})
	if err != nil {
		return problem(err.Error())
	}
	group.Names = names
	return append([]catalog.Staged{{Role: checkGroupRole, File: "checks.json", Data: data}}, metadata...), &group, nil
}

func (a *App) newProtocolLibraryDraft(kind ItemKind, protocol string) (ItemDraft, error) {
	if protocol == "" {
		return a.newLibraryDraft(kind), nil
	}
	if protocol != "fhir-r4" {
		return ItemDraft{}, errors.New("choose HL7 v2 or FHIR R4 for the new Library object")
	}
	switch kind {
	case ScenarioItem:
		return ItemDraft{Scenario: &ScenarioDraft{FHIR: &FHIRScenarioTemplate{Schema: FHIRScenarioSchema,
			Identity: localprofile.Identity{Version: "1"}, Seed: a.seed(), BaseTime: a.now().UTC().Truncate(time.Second).Format(time.RFC3339), Steps: []FHIRScenarioStep{}}}}, nil
	case ProfileItem:
		return ItemDraft{Profile: &ProfileDraft{FHIR: &FHIRProfileDefinition{Schema: FHIRProfileSchema, Identity: localprofile.Identity{Version: "1"},
			ResourceType: "Patient", Profiles: []fhirvalidator.Canonical{}, Packages: []fhirvalidator.PackageRef{},
			Requirements: fhirvalidator.Requirements{Terminology: "required", Invariants: "required", FailSeverities: []string{"fatal", "error"}}}}}, nil
	default:
		return ItemDraft{}, errors.New("the FHIR protocol starts a profile or scenario inside its existing editor")
	}
}

func decodeFHIRScenario(raw []byte) (*FHIRScenarioTemplate, error) {
	var template FHIRScenarioTemplate
	if protocolLibraryReader(FHIRScenarioSchema, "identity", "seed", "base_time", "steps").Decode(raw, &template) != nil ||
		!catalog.ValidToken(template.Identity.ID) || !catalog.ValidToken(template.Identity.Version) ||
		template.Seed > maxWindowSeed || len(template.Steps) < 1 || len(template.Steps) > 16 {
		return nil, errors.New("the FHIR scenario must retain its supported schema, identity, exact seed and 1 to 16 complete steps")
	}
	base, err := time.Parse(time.RFC3339, template.BaseTime)
	if err != nil || base.Nanosecond() != 0 {
		return nil, errors.New("a FHIR scenario declares its base time to the whole second")
	}
	seen := map[string]bool{}
	for _, step := range template.Steps {
		after, err := time.ParseDuration(step.After)
		if !catalog.ValidToken(step.ID) || seen[step.ID] || err != nil || after < 0 || after > 24*time.Hour ||
			!slices.Contains([]string{"resource", "bundle", "request"}, step.SourceKind) || len(step.Document) > 1<<20 {
			return nil, errors.New("a FHIR scenario step has one identity, an explicit bounded offset and a supported source kind")
		}
		seen[step.ID] = true
		if step.SourceKind == "request" {
			if step.Request == nil {
				return nil, errors.New("a request template retains its HTTP method, base, address and conditional headers")
			}
			r := step.Request
			contentType := ""
			if step.Document != "" {
				contentType = "application/fhir+json"
			}
			if _, err := fhirrequest.Parse(r.Base, r.Method, r.URL, contentType, []byte(step.Document), r.Headers); err != nil {
				return nil, errors.New("the complete FHIR request template is unsupported or invalid; no clause is removed")
			}
			continue
		}
		if step.Request != nil {
			return nil, errors.New("only a request template holds HTTP request facts")
		}
		doc, err := fhirr4.Decode(context.Background(), []byte(step.Document), fhirr4.Context{Version: fhirr4.Version, MediaType: "application/fhir+json"})
		if err != nil {
			return nil, errors.New("the FHIR resource template cannot be interpreted without changing its bytes")
		}
		var declared struct {
			ResourceType string `json:"resourceType"`
		}
		_ = json.Unmarshal(doc.Raw(), &declared)
		if (step.SourceKind == "bundle") != (declared.ResourceType == "Bundle") {
			return nil, errors.New("the source kind must explicitly match resource or Bundle evidence")
		}
	}
	return &template, nil
}

func protocolLibraryReader(schema string, required ...string) strictdoc.Document {
	return strictdoc.Document{MaxBytes: maxProtocolLibraryBytes, Schema: schema, Required: required,
		Invalid: "the complete Library document cannot be interpreted", Unknown: "the document retains unsupported members; no member is removed for you",
		TooLarge: "the Library document exceeds its retained size limit", MustDeclare: "the Library document must declare " + schema,
		Requires: "the Library document must explicitly retain every required member"}
}

var fhirPackageID = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,127}$`)
var fhirPackageVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,3}([+-][A-Za-z0-9.-]+)?$`)

func validFHIRPackage(pin fhirvalidator.PackageRef) bool {
	return fhirPackageID.MatchString(pin.ID) && fhirPackageVersion.MatchString(pin.Version)
}

func decodeFHIRProfile(raw []byte) (*FHIRProfileDefinition, error) {
	var profile FHIRProfileDefinition
	if err := protocolLibraryReader(FHIRProfileSchema, "identity", "resource_type", "profiles", "packages", "requirements").Decode(raw, &profile); err != nil {
		return nil, err
	}
	if !catalog.ValidToken(profile.Identity.ID) || !catalog.ValidToken(profile.Identity.Version) || !fhirr4.KnownResourceType(profile.ResourceType) ||
		profile.Profiles == nil || profile.Packages == nil || len(profile.Profiles) > 32 || len(profile.Packages) > 128 ||
		profile.Capability != "" && !networkaction.ValidDigest(profile.Capability) {
		return nil, errors.New("a FHIR profile retains a named version, a supported resource type and exact profile/package pins")
	}
	if profile.Validator != nil && (profile.Validator.Kind != EnvironmentItem || !catalog.ValidID(profile.Validator.ID) || !validSavedRevision(profile.Validator.Revision)) {
		return nil, errors.New("a validator names one saved connection at its exact revision")
	}
	seenPackages := map[fhirvalidator.PackageRef]bool{}
	for _, pin := range profile.Packages {
		if !validFHIRPackage(pin) || seenPackages[pin] {
			return nil, errors.New("FHIR package requirements retain distinct exact package versions")
		}
		seenPackages[pin] = true
	}
	seenProfiles := map[string]bool{}
	for _, pin := range profile.Profiles {
		address, err := url.Parse(pin.URL)
		key := pin.URL + "|" + pin.Version
		if err != nil || len(pin.URL) > 4096 || address.User != nil || address.Fragment != "" || strings.ContainsAny(pin.URL, "|\\\r\n ") ||
			!((address.Scheme == "http" || address.Scheme == "https") && address.Host != "" || address.Scheme == "urn" && address.Opaque != "") ||
			!networkaction.ValidDigest(pin.SHA256) || !validFHIRPackage(pin.Package) || !seenPackages[pin.Package] || seenProfiles[key] || len(pin.Version) > 128 {
			return nil, errors.New("every FHIR profile requirement pins one canonical revision and its exact selected package")
		}
		seenProfiles[key] = true
	}
	r := profile.Requirements
	if !slices.Contains([]string{"required", "not-requested"}, r.Terminology) || !slices.Contains([]string{"required", "not-requested"}, r.Invariants) ||
		len(r.FailSeverities) < 2 || len(r.FailSeverities) > 4 || !slices.Contains(r.FailSeverities, "fatal") || !slices.Contains(r.FailSeverities, "error") {
		return nil, errors.New("declare terminology, invariants and failure severities including fatal and error")
	}
	seen := map[string]bool{}
	for _, severity := range r.FailSeverities {
		if !slices.Contains([]string{"fatal", "error", "warning", "information"}, severity) || seen[severity] {
			return nil, errors.New("validation failure severities are distinct supported values")
		}
		seen[severity] = true
	}
	return &profile, nil
}

func validSavedRevision(revision string) bool {
	return revision != "" && revision[0] != '0' && len(revision) <= 9 && !strings.ContainsFunc(revision, func(r rune) bool { return r < '0' || r > '9' })
}

type fhirProfileVersion struct {
	Schema  string                 `json:"schema"`
	Profile localprofile.Identity  `json:"profile"`
	Content profileversion.Content `json:"content"`
}

type fhirProfilePackage struct {
	Schema  string                `json:"schema"`
	Profile FHIRProfileDefinition `json:"profile"`
	Seal    fhirProfileVersion    `json:"seal"`
	Origin  profilepackage.Origin `json:"origin"`
}

func fhirProfileSeal(profile FHIRProfileDefinition) (fhirProfileVersion, []byte, error) {
	raw, err := encodeMember(profile)
	if err != nil {
		return fhirProfileVersion{}, nil, err
	}
	return fhirProfileVersion{Schema: FHIRProfileVersionSchema, Profile: profile.Identity,
		Content: profileversion.Content{Bytes: len(raw), SHA256: digestOf(raw)}}, raw, nil
}

func decodeFHIRProfilePackage(raw []byte) (*FHIRProfileDefinition, *profilepackage.Origin, error) {
	var held fhirProfilePackage
	if err := protocolLibraryReader(FHIRProfilePackageSchema, "profile", "seal", "origin").Decode(raw, &held); err != nil {
		return nil, nil, err
	}
	document, err := encodeMember(held.Profile)
	if err != nil {
		return nil, nil, err
	}
	profile, err := decodeFHIRProfile(document)
	if err != nil {
		return nil, nil, err
	}
	seal, _, err := fhirProfileSeal(*profile)
	if err != nil || seal != held.Seal {
		return nil, nil, errors.New("the FHIR package's seal does not identify its exact profile pins")
	}
	originDocument, err := encodeMember(held.Origin)
	if err != nil {
		return nil, nil, err
	}
	origin, err := profilepackage.DecodeOrigin(originDocument)
	return profile, &origin, err
}

func readFHIRProfile(paths map[string]string) (*FHIRProfileDefinition, *profilepackage.Origin, error) {
	raw, err := boundedFile(paths[profileRole], maxProtocolLibraryBytes)
	if err != nil {
		return nil, nil, err
	}
	if schemaOf(raw) == FHIRProfilePackageSchema {
		return decodeFHIRProfilePackage(raw)
	}
	profile, err := decodeFHIRProfile(raw)
	if err != nil {
		return nil, nil, err
	}
	var origin *profilepackage.Origin
	if path, held := paths[originRole]; held {
		raw, err := boundedFile(path, profilepackage.MaxOriginBytes)
		if err != nil {
			return nil, nil, err
		}
		decoded, err := profilepackage.DecodeOrigin(raw)
		if err != nil {
			return nil, nil, err
		}
		origin = &decoded
	}
	return profile, origin, nil
}

func (c *loadedCatalog) fhirProfileAvailability(profile FHIRProfileDefinition) FHIRProfileAvailability {
	state := FHIRProfileAvailability{State: "worker-missing", Packages: []fhirvalidator.Package{}, Profiles: []fhirvalidator.Canonical{}}
	if profile.Validator == nil {
		state.Reason = "choose a saved FHIR connection with an installed offline validation capability"
		return state
	}
	raw, err := c.memberBytes(*profile.Validator, "target", maxProtocolLibraryBytes)
	var connection FHIRConnection
	if err != nil || json.Unmarshal(raw, &connection, json.RejectUnknownMembers(true)) != nil || connection.Schema != FHIRConnectionSchema || connection.Validation == nil ||
		connection.Validation.Engine != "local" || connection.Validation.Capability == "" {
		state.Reason = "the saved connection revision holds no installed offline validation capability"
		return state
	}
	capability, err := fhirvalidator.OpenCapability(connection.Validation.Capability)
	if err != nil {
		state.State, state.Reason = "capability-unavailable", "the staged local validation capability cannot be read"
		return state
	}
	manifest := capability.Manifest()
	state.Capability, state.Packages, state.Profiles = capability.Identity(), manifest.Packages, manifest.Profiles
	if profile.Capability != "" && profile.Capability != state.Capability {
		state.State, state.Reason = "capability-changed", "the installed metadata no longer matches the saved capability pin"
		return state
	}
	if err := capability.Check(); err != nil {
		var unavailable fhirvalidator.Status
		if errors.As(err, &unavailable) {
			state.State, state.Reason = unavailable.State, unavailable.Requirement
		} else {
			state.State, state.Reason = "capability-unavailable", "the local capability metadata cannot satisfy validation requirements"
		}
		return state
	}
	for _, required := range profile.Packages {
		if !slices.ContainsFunc(manifest.Packages, func(p fhirvalidator.Package) bool { return p.ID == required.ID && p.Version == required.Version }) {
			state.State, state.Reason = "package-unavailable", "stage the exact package versions the saved profile requires"
			return state
		}
	}
	for _, required := range profile.Profiles {
		if !slices.Contains(manifest.Profiles, required) {
			state.State, state.Reason = "profile-unavailable", "stage the exact canonical revisions the saved profile pins"
			return state
		}
	}
	state.State = "installed-worker-not-checked"
	return state
}

func validateFHIRProfileDraft(scope draftScope, draft ItemDraft) ([]catalog.Staged, *ProfileDraft, []FieldProblem) {
	problem := func(reason string) ([]catalog.Staged, *ProfileDraft, []FieldProblem) {
		return nil, nil, []FieldProblem{{Field: "profile.fhir", Problem: reason}}
	}
	held := draft.Profile
	if !emptyLocalProfile(held.Profile) || held.Pack != nil || held.PackDocument != "" {
		return problem("choose one profile protocol; no retained v2 rule or package is removed implicitly")
	}
	definition := *held.FHIR
	if definition.Schema == "" {
		definition.Schema = FHIRProfileSchema
	}
	if definition.Identity.ID == "" {
		definition.Identity.ID = actionSlug(draft.Name)
	}
	if scope.loaded != nil {
		availability := scope.loaded.fhirProfileAvailability(definition)
		if availability.State == "capability-changed" {
			return problem(availability.Reason)
		}
		if definition.Capability == "" && availability.Capability != "" {
			definition.Capability = availability.Capability
		}
		if scope.item != "" {
			index := scope.loaded.document.Find(scope.item)
			if index >= 0 {
				item := scope.loaded.document.Items[index]
				for _, revision := range item.Revisions {
					if scope.intent != "" && scope.intent == revision.Intent {
						continue
					}
					paths, availability, _ := scope.loaded.membersBacking(revision.Members)
					if availability != ItemAvailable {
						continue
					}
					earlier, _, err := readFHIRProfile(paths)
					if err != nil {
						continue
					}
					if earlier.Identity.ID != definition.Identity.ID {
						return problem("a profile keeps its identity; save a copy to name another")
					}
					if earlier.Identity.Version == definition.Identity.Version {
						return problem("this profile version is already published; changes require a new version")
					}
				}
			}
		}
	}
	raw, err := encodeMember(definition)
	if err != nil {
		return problem("the complete FHIR profile cannot be encoded")
	}
	profile, err := decodeFHIRProfile(raw)
	if err != nil {
		return problem(err.Error())
	}
	if scope.loaded != nil {
		for _, item := range scope.loaded.document.Items {
			if item.Kind != string(ProfileItem) || scope.loaded.removed(item) {
				continue
			}
			records := []catalog.Item{item}
			if len(item.Revisions) > 0 {
				records = nil
				for i, revision := range item.Revisions {
					if scope.intent != "" && revision.Intent == scope.intent {
						continue
					}
					record := item
					record.Revisions = item.Revisions[:i+1]
					records = append(records, record)
				}
			}
			for _, record := range records {
				paths, availability, _ := scope.loaded.backing(record)
				if availability != ItemAvailable {
					continue
				}
				earlier, _, err := readFHIRProfile(paths)
				if err != nil || earlier.Identity != profile.Identity {
					continue
				}
				oldSeal, _, err := fhirProfileSeal(*earlier)
				newSeal, _, currentErr := fhirProfileSeal(*profile)
				if err != nil || currentErr != nil || oldSeal != newSeal {
					return problem("the project already holds different requirements under this FHIR profile identity and version; publish a new version")
				}
			}
		}
	}
	seal, raw, err := fhirProfileSeal(*profile)
	if err != nil {
		return problem(err.Error())
	}
	sealDocument, err := encodeMember(seal)
	if err != nil {
		return problem(err.Error())
	}
	staged := []catalog.Staged{{Role: profileRole, File: "profile.json", Data: raw}, {Role: sealRole, File: "version.json", Data: sealDocument}}
	normalized := &ProfileDraft{FHIR: profile}
	if held.Origin != nil {
		origin := *held.Origin
		if origin.Schema == "" {
			origin.Schema = profilepackage.OriginSchema
		}
		raw, err := encodeMember(origin)
		if err != nil {
			return problem("the profile origin cannot be encoded")
		}
		origin, err = profilepackage.DecodeOrigin(raw)
		if err != nil {
			return problem(err.Error())
		}
		normalized.Origin = &origin
		staged = append(staged, catalog.Staged{Role: originRole, File: "origin.json", Data: raw})
	}
	return staged, normalized, nil
}

func verifyFHIRProfile(files map[string]string) error {
	profile, _, err := readFHIRProfile(files)
	if err != nil {
		return err
	}
	raw, err := boundedFile(files[sealRole], profileversion.MaxVersionBytes)
	if err != nil {
		return err
	}
	var held fhirProfileVersion
	if err := protocolLibraryReader(FHIRProfileVersionSchema, "profile", "content").Decode(raw, &held); err != nil {
		return err
	}
	seal, _, err := fhirProfileSeal(*profile)
	if err != nil || held != seal {
		return errors.New("the saved version does not seal the exact FHIR profile requirements")
	}
	return nil
}

func validateFHIRScenarioDraft(draft ItemDraft, version string) ([]catalog.Staged, *ScenarioDraft, []FieldProblem) {
	problem := func(reason string) ([]catalog.Staged, *ScenarioDraft, []FieldProblem) {
		return nil, nil, []FieldProblem{{Field: "scenario.fhir", Problem: reason}}
	}
	held := draft.Scenario
	if !emptyScenarioPlan(held.Plan) || held.Template != nil || held.Generation != nil || held.Profile != nil || len(held.Orders) > 0 || len(held.Results) > 0 {
		return problem("choose one scenario protocol; all retained v2 clauses must be reviewed before changing it")
	}
	template := *held.FHIR
	if template.Schema == "" {
		template.Schema = FHIRScenarioSchema
	}
	if template.Identity.ID == "" {
		template.Identity.ID = actionSlug(draft.Name)
	}
	if version != "" {
		template.Identity.Version = version
	}
	raw, err := encodeMember(template)
	if err != nil {
		return problem("the FHIR scenario cannot be encoded")
	}
	decoded, err := decodeFHIRScenario(raw)
	if err != nil {
		return problem(err.Error())
	}
	return []catalog.Staged{{Role: scenarioRole, File: "plan.json", Data: raw}}, &ScenarioDraft{FHIR: decoded}, nil
}
