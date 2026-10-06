package desktop

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/fhirevidence"
	"github.com/bharm16/readmit/internal/operation"
)

// errConnectedTest is why a reader of readmit-test/v1 specs does not read a
// connected test: it has no such spec, and its own readers read it.
var errConnectedTest = errors.New("this is a connected test; its outcome is the records it observes, not an acknowledgement or ledger spec")

// ConnectedTestContext is what the connected editor shows beside its draft:
// the project's connected observations at their current versions, the
// versions the draft pins and the named environments it can run against.
type ConnectedTestContext struct {
	Observations []ConnectedObservationOffer `json:"observations"`
	Pinned       []ConnectedObservationOffer `json:"pinned"`
	Environments []ConnectedEnvironmentOffer `json:"environments"`
}

// ConnectedEnvironmentOffer is one named environment as a connected test
// can use it: its protocol, the name of its typed isolation, whether its FHIR
// capabilities were checked at its current revision, its local validator and
// its authentication. Reason says why a connected test cannot run against it.
type ConnectedEnvironmentOffer struct {
	Ref       ItemRef `json:"ref"`
	Name      string  `json:"name"`
	Protocol  string  `json:"protocol"`
	Isolation string  `json:"isolation,omitzero"`
	// Effects are what its isolation sets up before a run: each resource it
	// creates, claims or selects, by name.
	Effects        []string `json:"effects"`
	Capabilities   bool     `json:"capabilities"`
	Validator      string   `json:"validator,omitzero"`
	Authentication string   `json:"authentication,omitzero"`
	Reason         string   `json:"reason,omitzero"`
}

// savedConnected is one revision of a connected test as saved.
type savedConnected struct {
	data  []byte
	draft ConnectedTestDraft
	links *TestLinks
}

func decodeConnectedTest(data []byte) (ConnectedTestDraft, error) {
	var d ConnectedTestDraft
	if len(data) > catalog.MaxMemberBytes || json.Unmarshal(data, &d, json.RejectUnknownMembers(true)) != nil || d.Schema != ConnectedTestSchema {
		return ConnectedTestDraft{}, errors.New("the connected test cannot be read")
	}
	return d, nil
}

// connectedTestOf reads one revision of a connected test, or errConnectedTest's
// opposite: an error for a test that is not one.
func (c *loadedCatalog) connectedTestOf(item catalog.Item, revision string) (*savedConnected, error) {
	paths, availability, reason := c.revisionBacking(item, revision)
	if availability != ItemAvailable {
		return nil, errors.New(reason)
	}
	path := paths[primaryRole(TestItem)]
	if !declares(path, ConnectedTestSchema) {
		return nil, errors.New("this test is not a connected test")
	}
	data, err := savedFile.Read(path)
	if err != nil {
		return nil, err
	}
	saved := &savedConnected{data: data}
	if saved.draft, err = decodeConnectedTest(data); err != nil {
		return nil, err
	}
	if path, held := paths["links"]; held {
		links, err := readTestLinks(path)
		if err != nil {
			return nil, err
		}
		saved.links = &links
	}
	return saved, nil
}

// isConnectedTest reports whether a test object's current revision is a
// connected test.
func (c *loadedCatalog) isConnectedTest(item catalog.Item) bool {
	paths, availability, _ := c.backing(item)
	return availability == ItemAvailable && declares(paths[primaryRole(TestItem)], ConnectedTestSchema)
}

// verifyConnectedTest reads a staged connected test revision as its readers do.
func verifyConnectedTest(files map[string]string) error {
	data, err := savedFile.Read(files["test"])
	if err != nil {
		return err
	}
	if _, err := decodeConnectedTest(data); err != nil {
		return err
	}
	if path, held := files["links"]; held {
		if _, err := readTestLinks(path); err != nil {
			return err
		}
	}
	return nil
}

func readConnectedTest(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	data, err := savedFile.Read(paths[primaryRole(TestItem)])
	if err != nil {
		return view{}, err
	}
	d, err := decodeConnectedTest(data)
	if err != nil {
		return view{}, err
	}
	summary := &TestSummary{CurrentVersion: item.RevisionLabel(), Boundary: d.Boundary, Entry: c.entryOf(paths[primaryRole(TestItem)])}
	for _, phase := range d.Phases {
		summary.Assertions += len(phase.Checks) + len(phase.Responses) + len(phase.Validations) + len(phase.Acks)
	}
	if len(d.Steps) > 0 {
		if index := c.document.Find(d.Steps[0].Source.Case.ID); index >= 0 {
			summary.SourceCase = &ItemRef{Kind: ItemKind(c.document.Items[index].Kind), ID: d.Steps[0].Source.Case.ID}
		}
	}
	if links, held := paths["links"]; held {
		read, err := readTestLinks(links)
		if err != nil {
			return view{}, err
		}
		summary.Tags = read.Tags
	}
	return view{name: item.Name, summary: ItemSummary{Test: summary}}, nil
}

// connectedContext reads what the connected editor shows beside a draft.
func (c *loadedCatalog) connectedContext(d *ConnectedTestDraft) *ConnectedTestContext {
	context := &ConnectedTestContext{Observations: []ConnectedObservationOffer{}, Pinned: []ConnectedObservationOffer{}, Environments: []ConnectedEnvironmentOffer{}}
	for _, item := range c.document.Items {
		if item.Kind != string(ObservationItem) || c.removed(item) {
			continue
		}
		paths, availability, _ := c.backing(item)
		if availability != ItemAvailable {
			continue
		}
		if _, held := paths["setup"]; !held {
			continue
		}
		read, err := c.connectedObservation(item.ID, "")
		if read != nil {
			context.Observations = append(context.Observations, read.offer(err))
		}
	}
	if d != nil {
		for _, pinned := range c.pinnedOffers(*d) {
			context.Pinned = append(context.Pinned, pinned)
		}
	}
	slices.SortStableFunc(context.Pinned, func(x, y ConnectedObservationOffer) int {
		return cmp.Or(cmp.Compare(x.Ref.ID, y.Ref.ID), cmp.Compare(x.Ref.Revision, y.Ref.Revision))
	})
	for _, item := range c.document.Items {
		if item.Kind != string(EnvironmentItem) || c.removed(item) {
			continue
		}
		offer := ConnectedEnvironmentOffer{Ref: ItemRef{Kind: EnvironmentItem, ID: item.ID, Revision: item.RevisionLabel()}, Name: c.read(item).Name, Protocol: "v2", Effects: []string{}}
		members, err := c.environmentOf(item)
		if err != nil {
			offer.Reason = "this environment cannot be read"
			context.Environments = append(context.Environments, offer)
			continue
		}
		if members.fhir != nil {
			offer.Protocol, offer.Validator, offer.Authentication = "fhir", members.fhir.validatorState(), members.fhir.Authentication
			if record, err := readFHIRCheck(c.root, item.ID, CheckFHIRCapabilitiesAction); err == nil && record.Check.Revision == offer.Ref.Revision && len(record.Capability) > 0 {
				offer.Capabilities = true
			}
		}
		if members.isolation != nil {
			offer.Isolation = members.isolation.Name
			for _, resource := range members.isolation.Resources {
				verb := map[string]string{"create": "Creates", "claim": "Claims", "select": "Selects"}[resource.Ownership]
				offer.Effects = append(offer.Effects, strings.TrimSpace(verb+" "+resource.Name))
			}
		} else {
			offer.Reason = "this environment has no typed isolation"
		}
		context.Environments = append(context.Environments, offer)
	}
	return context
}

// pinnedOffers are the observation versions a draft pins, keyed by id@revision.
func (c *loadedCatalog) pinnedOffers(d ConnectedTestDraft) map[string]ConnectedObservationOffer {
	pinned := map[string]ConnectedObservationOffer{}
	for _, phase := range d.Phases {
		for _, observed := range phase.Observations {
			key := observed.Observation.ID + "@" + observed.Observation.Revision
			if _, held := pinned[key]; held || observed.Observation.ID == "" {
				continue
			}
			read, err := c.connectedObservation(observed.Observation.ID, observed.Observation.Revision)
			if read != nil {
				pinned[key] = read.offer(err)
			}
		}
	}
	return pinned
}

// connectedProblems are every problem a connected draft has against the
// project now: its own shape, its checks' bindings and what changed since it
// was authored, including the environment it links when that is named. It
// compiles nothing.
func (c *loadedCatalog) connectedProblems(d ConnectedTestDraft, environment string) []FieldProblem {
	problems := d.structureProblems()
	pinned := c.pinnedOffers(d)
	problems = append(problems, d.bindingProblems(pinned)...)
	current := map[string]ConnectedObservationOffer{}
	for _, phase := range d.Phases {
		for _, observed := range phase.Observations {
			if read, err := c.connectedObservation(observed.Observation.ID, ""); read != nil {
				current[observed.Observation.ID] = read.offer(err)
			}
		}
	}
	sources := map[string]string{}
	for _, s := range d.Steps {
		index := c.document.Find(s.Source.Case.ID)
		if index < 0 || c.removed(c.document.Items[index]) || c.document.Items[index].Entry == "" {
			continue
		}
		if facts, _, err := operation.VerifiedCase(c.root, c.document.Items[index].Entry); err == nil {
			sources[s.Source.Case.ID] = facts.Identity
		} else {
			sources[s.Source.Case.ID] = ""
		}
	}
	var reach *connectedReach
	if environment != "" {
		reach = &connectedReach{environment: environment, server: d.Server, offers: map[string]ConnectedEnvironmentOffer{}}
		for _, id := range []string{environment, d.Server} {
			if read, err := c.connectedEnvironment(id); err == nil {
				offer := ConnectedEnvironmentOffer{Ref: read.ref, Name: read.name, Protocol: "v2"}
				if read.members.fhir != nil {
					offer.Protocol = "fhir"
				}
				reach.offers[id] = offer
			}
		}
		for _, offer := range pinned {
			if offer.Protocol != "fhir" {
				continue
			}
			if read, err := c.connectedEnvironment(offer.Environment); err == nil {
				reach.offers[offer.Environment] = ConnectedEnvironmentOffer{Ref: read.ref, Name: read.name, Protocol: "fhir"}
			}
		}
	}
	for _, problem := range d.invalidations(current, pinned, sources, reach) {
		if !slices.ContainsFunc(problems, func(p FieldProblem) bool { return p.Field == problem.Field && p.Problem == problem.Problem }) {
			problems = append(problems, problem)
		}
	}
	return problems
}

// connectedLifecycleID is the identity a saved connected test's lifecycle
// carries; a draft not yet saved is compiled as one.
func connectedLifecycleID(item string) string {
	if item == "" {
		return "test-draft"
	}
	return "test-" + item
}

// validateConnectedTestDraft validates a whole connected test draft and
// answers the members its save stages: the authoring document and its links.
func validateConnectedTestDraft(scope draftScope, draft ItemDraft) ([]catalog.Staged, ItemDraft, []FieldProblem) {
	problems := []FieldProblem{}
	var d ConnectedTestDraft
	if draft.ConnectedTest != nil {
		d = *draft.ConnectedTest
	} else {
		decoded, err := decodeConnectedTest([]byte(draft.TestDocument))
		if err != nil {
			return nil, ItemDraft{Name: draft.Name}, []FieldProblem{{Field: "test_document", Problem: err.Error()}}
		}
		d = decoded
	}
	normalized := ItemDraft{Name: draft.Name, ConnectedTest: &d}
	if draft.Test != nil {
		problems = append(problems, FieldProblem{Field: "test", Problem: "a test is a connected test or a spec of one case, not both"})
	}
	if draft.Name == "" {
		problems = append(problems, FieldProblem{Field: "name", Problem: "name this test"})
	} else if named := testNameProblem(draft.Name); named != nil {
		problems = append(problems, FieldProblem{Field: "name", Problem: named.Problem})
	}
	links := TestLinks{}
	if draft.TestLinks != nil {
		links = *draft.TestLinks
		links.Tags = slices.Sorted(slices.Values(links.Tags))
	}
	links.Schema = schemaForTestLinks(links)
	problems = append(problems, checkTestLinks(links)...)
	if links.Environment == "" {
		problems = append(problems, FieldProblem{Field: "test.environment", Problem: "choose the environment this test runs against"})
	}
	if links.Observation != "" {
		problems = append(problems, FieldProblem{Field: "test.observation", Problem: "a connected test reads the observations its phases name"})
	}
	if links.Reset == ResetManual {
		problems = append(problems, FieldProblem{Field: "test.reset", Problem: "a connected test resets through its environment's typed isolation"})
	}
	if len(links.Checks) > 0 {
		problems = append(problems, FieldProblem{Field: "test.checks", Problem: "a connected test's checks are its own; check groups are decided against acknowledgement and ledger runs"})
	}
	if scope.loaded == nil {
		return nil, normalized, append(problems, FieldProblem{Field: "connected", Problem: "the project's catalog cannot be read"})
	}
	problems = append(problems, scope.loaded.connectedProblems(d, links.Environment)...)
	if len(problems) == 0 {
		compiled, found := scope.loaded.compileConnected(d, connectedLifecycleID(scope.item), scope.nextRevision(), connectedEnvironments{environment: links.Environment, server: d.Server})
		problems = append(problems, found...)
		if compiled != nil {
			if err := compiled.prepare(context.Background()); err != nil {
				problems = append(problems, FieldProblem{Field: "connected", Problem: "the connected runner cannot prepare this test as written: " + err.Error()})
			}
		}
	}
	if len(problems) > 0 {
		return nil, normalized, problems
	}
	data, err := encodeMember(d)
	if err != nil {
		return nil, normalized, []FieldProblem{{Field: "connected", Problem: err.Error()}}
	}
	staged := []catalog.Staged{{Role: "test", File: "test.json", Data: data}}
	encoded, err := encodeMember(links)
	if err != nil {
		return nil, normalized, []FieldProblem{{Field: "test_links", Problem: err.Error()}}
	}
	staged = append(staged, catalog.Staged{Role: "links", File: "links.json", Data: encoded})
	normalized.TestLinks = &links
	return staged, normalized, nil
}

// reviewConnected is the expectation review of a connected test compiled as
// one revision: the identity its approval binds, which no environment changes.
func (c *loadedCatalog) reviewConnected(d ConnectedTestDraft, item, revision string, links TestLinks) (expectation.ConnectedReview, error) {
	compiled, problems := c.compileConnected(d, connectedLifecycleID(item), revision, connectedEnvironments{environment: links.Environment, server: d.Server})
	if compiled == nil {
		return expectation.ConnectedReview{}, errors.New(firstProblem(problems))
	}
	return reviewCompiled(compiled)
}

// openConnectedDraft answers a saved connected test revision's whole draft.
func (c *loadedCatalog) openConnectedDraft(result ItemDraftResult, record catalog.Item, revision string) ItemDraftResult {
	saved, err := c.connectedTestOf(record, revision)
	if err != nil {
		result.refuse(Failed, "this test cannot be read: "+err.Error())
		return result
	}
	draft := ItemDraft{Name: record.Name, ConnectedTest: &saved.draft, TestLinks: saved.links}
	shown := &TestContext{Messages: []TestMessage{}, Observations: []TestObservation{}, Unsupported: []TestClause{}, Proposals: []TestProposal{}, Document: string(saved.data),
		ReadOnly: revision != "" && revision != record.RevisionLabel(), Connected: c.connectedContext(&saved.draft)}
	if len(saved.draft.Steps) > 0 {
		index := c.document.Find(saved.draft.Steps[0].Source.Case.ID)
		if index >= 0 {
			shown.Case = &ItemRef{Kind: ItemKind(c.document.Items[index].Kind), ID: c.document.Items[index].ID}
			shown.CaseName = c.read(c.document.Items[index]).Name
		}
	}
	ref := ItemRef{Kind: TestItem, ID: record.ID, Revision: cmp.Or(revision, record.RevisionLabel())}
	result.State, result.Ref, result.Draft, result.Test = Completed, &ref, &draft, shown
	if !shown.ReadOnly {
		environment := ""
		if saved.links != nil {
			environment = saved.links.Environment
		}
		result.Problems = c.connectedProblems(saved.draft, environment)
	}
	return result
}

// connectedChanges names what one version of a connected test changed.
func connectedChanges(before, after *savedConnected) []TestChange {
	links := func(saved *savedConnected) TestLinks {
		if saved.links == nil {
			return TestLinks{}
		}
		return *saved.links
	}
	x, y := before.draft, after.draft
	lx, ly := links(before), links(after)
	sources := func(d ConnectedTestDraft) []string {
		out := []string{}
		for _, s := range d.Steps {
			out = append(out, s.Source.Case.ID)
		}
		return out
	}
	observations := func(d ConnectedTestDraft) []ConnectedPhaseObservation {
		out := []ConnectedPhaseObservation{}
		for _, p := range d.Phases {
			out = append(out, p.Observations...)
		}
		return out
	}
	checks := func(d ConnectedTestDraft) [][]any {
		out := [][]any{}
		for _, p := range d.Phases {
			out = append(out, []any{p.Checks, p.Responses, p.Validations, p.Acks, p.When})
		}
		return out
	}
	steps := func(d ConnectedTestDraft) []any {
		out := []any{d.Variables, d.Generation}
		for _, s := range d.Steps {
			out = append(out, s)
		}
		for _, p := range d.Phases {
			out = append(out, p.ID, p.Name, p.Steps, p.After)
		}
		return out
	}
	changes := []TestChange{}
	for _, change := range []struct {
		name    TestChange
		changed bool
	}{
		{ChangeCase, !slices.Equal(sources(x), sources(y))},
		{ChangeMessages, !reflect.DeepEqual(steps(x), steps(y))},
		{ChangeEnvironment, lx.Environment != ly.Environment || x.Server != y.Server},
		{ChangeBoundary, x.Boundary != y.Boundary},
		{ChangeObservation, !reflect.DeepEqual(observations(x), observations(y))},
		{ChangeReset, lx.Reset != ly.Reset},
		{ChangeChecks, !reflect.DeepEqual(checks(x), checks(y))},
		{ChangeTags, !slices.Equal(lx.Tags, ly.Tags)},
	} {
		if change.changed {
			changes = append(changes, change.name)
		}
	}
	return changes
}

// connectedHistory lists a connected test's versions with what changed in
// each. A connected test's runs are its suites' runs.
func (c *loadedCatalog) connectedHistory(result TestHistoryResult, record catalog.Item) TestHistoryResult {
	var previous *savedConnected
	for i, revision := range record.Revisions {
		label := strconv.Itoa(revision.Number)
		saved, err := c.connectedTestOf(record, label)
		version := TestVersion{Revision: label, PublishedAt: stamped(revision.PublishedAt), Author: revision.Author, Changes: []TestChange{}, Current: i == len(record.Revisions)-1}
		if err == nil && previous != nil {
			version.Changes = connectedChanges(previous, saved)
		}
		if err == nil {
			previous = saved
		}
		result.Versions = append(result.Versions, version)
	}
	slices.Reverse(result.Versions)
	result.State = Completed
	return result
}

// ConnectedSourcesRequest names a case of the project whose evidence a
// connected test's inputs can send.
type ConnectedSourcesRequest struct {
	Context RequestContext `json:"context"`
	Case    ItemRef        `json:"case"`
}

// ConnectedSourceOffer is one occurrence of a case an input can send: its
// occurrence, its type, the resource it holds, whether it can be sent, and,
// for a declared FHIR request, the typed request it declares.
type ConnectedSourceOffer struct {
	Occurrence string         `json:"occurrence"`
	Label      string         `json:"label"`
	Resource   string         `json:"resource,omitzero"`
	Sendable   bool           `json:"sendable"`
	FHIR       *ConnectedFHIR `json:"fhir,omitzero"`
}

// ConnectedSourcesResult is the case's verified evidence identity, its
// protocol and every occurrence of it, in source order.
type ConnectedSourcesResult struct {
	State    State                  `json:"state"`
	Reason   string                 `json:"reason,omitzero"`
	Context  RequestContext         `json:"context"`
	Identity string                 `json:"identity,omitzero"`
	Protocol string                 `json:"protocol,omitzero"`
	Sources  []ConnectedSourceOffer `json:"sources"`
}

func (r *ConnectedSourcesResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ConnectedTestSources reads one case's verified evidence as a connected
// test's inputs: v2 messages by their type, and FHIR resources and declared
// requests with the typed request each prefills. It is a read: nothing is
// sent and no reference in the evidence is followed.
func (a *App) ConnectedTestSources(request ConnectedSourcesRequest) ConnectedSourcesResult {
	return runRead(a, false, func(ctx context.Context) ConnectedSourcesResult {
		result := ConnectedSourcesResult{Context: request.Context, Sources: []ConnectedSourceOffer{}}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Case, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		if request.Case.Kind != CaseItem && request.Case.Kind != VariantItem {
			result.refuse(Failed, "a connected test sends the evidence of a case")
			return result
		}
		entry := loaded.document.Items[loaded.document.Find(item.Ref.ID)].Entry
		path := filepath.Join(loaded.root, entry)
		if entry == "" {
			result.refuse(Failed, "this case has no evidence in the project")
			return result
		}
		if regular(filepath.Join(path, fhirevidence.ManifestName)) {
			opened, err := fhirevidence.Open(ctx, path)
			if err != nil {
				result.refuse(Failed, "the retained R4 evidence cannot be verified as complete and unmodified")
				return result
			}
			result.Identity, result.Protocol = opened.Identity, "fhir"
			declared := connectedRequestOf(opened)
			if opened.Document == nil {
				result.Sources = append(result.Sources, ConnectedSourceOffer{Occurrence: "request", Label: "FHIR request", Sendable: declared != nil && declared.Method == "DELETE", FHIR: declared})
			} else {
				for i, resource := range opened.Document.Resources() {
					offer := ConnectedSourceOffer{Occurrence: resource.Occurrence, Label: fhirResourceCaption(resource.Type), Resource: resource.Type, Sendable: resource.State == "parsed" && resource.Type != "Bundle"}
					if offer.Sendable {
						offer.FHIR = &ConnectedFHIR{Method: "POST", Resource: resource.Type, Target: ConnectedFHIRTarget{Kind: "type"}, Bind: []connectedtest.ResponseBinding{}}
						if i == 0 && declared != nil {
							offer.FHIR = declared
						}
					}
					result.Sources = append(result.Sources, offer)
				}
			}
			result.State = Completed
			return result
		}
		facts, _, err := operation.VerifiedCase(loaded.root, entry)
		if err != nil {
			result.refuse(Failed, "the case's evidence cannot be verified")
			return result
		}
		_, source, declined := openedCase(loaded.root, entry, facts.Identity)
		if source == nil {
			result.refuse(Failed, declined.reason)
			return result
		}
		result.Identity, result.Protocol = source.Identity, "v2"
		for _, message := range caseMessages(source) {
			label := message.MessageCode
			if message.TriggerEvent != "" {
				label += "^" + message.TriggerEvent
			}
			result.Sources = append(result.Sources, ConnectedSourceOffer{Occurrence: message.ID, Label: cmp.Or(label, "Message"), Sendable: message.Sendable})
		}
		result.State = Completed
		return result
	})
}

// connectedRequestOf is the typed request a declared FHIR request evidence
// prefills: its method, the resource it addresses and its identifier
// condition. Anything the typed choices cannot hold is left for the person.
func connectedRequestOf(opened *fhirevidence.Artifact) *ConnectedFHIR {
	declaration := opened.Manifest.Declaration.Request
	if declaration == nil || opened.Request == nil {
		return nil
	}
	r := opened.Request
	f := &ConnectedFHIR{Method: declaration.Method, Resource: r.Resource, Target: ConnectedFHIRTarget{Kind: "type"}, Bind: []connectedtest.ResponseBinding{}}
	if values := r.Parameters["identifier"]; len(values) == 1 && declaration.Method != "POST" {
		if system, value, found := strings.Cut(values[0], "|"); found {
			f.Target = ConnectedFHIRTarget{Kind: "conditional", Identifier: &ConnectedIdentifier{System: system, Value: value}}
		}
	}
	if condition := declaration.Headers.IfNoneExist; condition != "" {
		if system, value, found := strings.Cut(strings.TrimPrefix(condition, "identifier="), "|"); found && strings.HasPrefix(condition, "identifier=") {
			f.IfNone = &ConnectedIdentifier{System: system, Value: value}
		}
	}
	f.Prefer = declaration.Headers.Prefer
	return f
}

// declaresDocument reports whether a document's text declares schema.
func declaresDocument(text, schema string) bool {
	declared, ok := schemaIn([]byte(text[:min(len(text), maxSchemaSniffBytes)]))
	return ok && declared == schema
}

// newConnectedFromEvidence starts a connected test over FHIR evidence: each
// resource it holds that a request can send, in the order it holds them, as
// the typed request its declaration prefills, each after the one before.
// Nothing is saved; the person completes the observations and checks.
func (c *loadedCatalog) newConnectedFromEvidence(result ItemDraftResult, ref ItemRef, name, entry string, origin *TestOrigin) ItemDraftResult {
	opened, err := fhirevidence.Open(context.Background(), filepath.Join(c.root, entry))
	if err != nil {
		result.refuse(Failed, "the retained R4 evidence cannot be verified as complete and unmodified")
		return result
	}
	if origin.Identity != "" && origin.Identity != opened.Identity {
		result.refuse(Failed, "the selected source evidence changed; inspect it again before creating a test")
		return result
	}
	d := ConnectedTestDraft{Schema: ConnectedTestSchema, Boundary: ApplicationBoundary, Generation: connectedtest.Generation{Seed: 1, BaseTime: "2026-01-01T00:00:00Z"}, Variables: []ConnectedVariable{}, Steps: []ConnectedStep{}, Phases: []ConnectedPhase{}}
	declared := connectedRequestOf(opened)
	selected := map[string]bool{}
	for _, id := range origin.Messages {
		selected[id] = true
	}
	if opened.Document != nil {
		resources := opened.Document.Resources()
		indexes := []int{}
		if len(origin.Messages) == 0 {
			for i := range resources {
				indexes = append(indexes, i)
			}
		} else {
			seen := map[string]bool{}
			for _, id := range origin.Messages {
				if seen[id] {
					continue
				}
				seen[id] = true
				for i, resource := range resources {
					if resource.Occurrence == id {
						indexes = append(indexes, i)
						break
					}
				}
			}
		}
		for _, i := range indexes {
			resource := resources[i]

			if resource.State != "parsed" || resource.Type == "Bundle" || len(selected) > 0 && !selected[resource.Occurrence] {
				continue
			}
			request := &ConnectedFHIR{Method: "POST", Resource: resource.Type, Target: ConnectedFHIRTarget{Kind: "type"}, Bind: []connectedtest.ResponseBinding{}}
			if i == 0 && declared != nil {
				request = declared
			}
			step := ConnectedStep{ID: "step-" + strconv.Itoa(len(d.Steps)+1), After: []string{}, Source: ConnectedSource{Case: ref, Identity: opened.Identity, Occurrence: resource.Occurrence}, FHIR: request}
			if len(d.Steps) > 0 {
				step.After = []string{d.Steps[len(d.Steps)-1].ID}
			}
			d.Steps = append(d.Steps, step)
		}
	}
	if len(d.Steps) > 0 {
		phase := ConnectedPhase{ID: "phase-1", Name: "Requests", Steps: []string{}, After: []connectedtest.PhaseDependency{}, Observations: []ConnectedPhaseObservation{}, Checks: []ConnectedCheck{}, Responses: []ConnectedResponseCheck{}, Validations: []ConnectedValidationCheck{}, Acks: []ConnectedAckCheck{}}
		for _, step := range d.Steps {
			phase.Steps = append(phase.Steps, step.ID)
		}
		d.Phases = append(d.Phases, phase)
	}
	draft := ItemDraft{ConnectedTest: &d}
	if title := cmp.Or(origin.Title, name); catalog.ValidName(title) {
		draft.Name = title
	}
	result.State, result.Draft = Completed, &draft
	result.Test = &TestContext{Case: &ref, CaseName: name, Messages: []TestMessage{}, Observations: c.testObservations(), Unsupported: []TestClause{}, Proposals: []TestProposal{}, Connected: c.connectedContext(&d)}
	return result
}
