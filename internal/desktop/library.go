package desktop

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/assertionauthor"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/profilelibrary"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/profilepackage"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/scenario"
	"github.com/bharm16/readmit/internal/scenariogen"
)

// The library is a project's reusable check groups, local interface profiles
// and synthetic scenarios. Each is one saved object published whole through
// the catalog, as the documents the command line reads: a check group as a
// readmit-assertion-set/v1, a profile as a readmit-local-profile/v1 with its
// version seal and, when it has one, its origin, and a scenario as a
// readmit-scenario-generator/v1 plan. Nothing here evaluates a message.

// CheckGroupDraft is a check group as its editor holds it: the checks this
// release evaluates, and every check it holds that it does not, kept beside
// them so none is dropped. A group holding an unsupported check is not saved.
// Names are the display names a person gave its checks, by check identity,
// kept beside the set as library metadata.
type CheckGroupDraft struct {
	Set         assertionauthor.Draft               `json:"set"`
	Unsupported []assertionauthor.UnsupportedClause `json:"unsupported"`
	Names       map[string]string                   `json:"names,omitzero"`
}

// ScenarioDraft is a generator plan as its editor holds it. Template, when
// present, is the plan's lifecycle workflow as typed members: a save writes it
// into the plan's template, so the plan saved is exactly the one previewed.
// An order workflow is carried in the plan's template alone. Profile is the
// saved local profile, at its exact revision, the scenario is authored for,
// kept beside the plan as library metadata; it must cover the family of the
// workflow.
type ScenarioDraft struct {
	Plan     scenariogen.Plan   `json:"plan"`
	Template *scenario.Scenario `json:"template,omitzero"`
	Profile  *ItemRef           `json:"profile,omitzero"`
}

// maxWindowSeed is the largest seed the window carries exactly: a JSON number
// the window reads is a double, so a seed above 2^53-1 would change on its
// way back.
const maxWindowSeed = 1<<53 - 1

// The files and roles a library object is saved as.
const (
	checkGroupRole = "check-group"
	scenarioRole   = "scenario"
)

// readCheckGroup reads a check group clause by clause, so a group holding a
// check this release cannot evaluate is still listed, with every check it
// holds counted and the unsupported ones named as its reason.
func readCheckGroup(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	data, err := boundedFile(paths[primaryRole(CheckGroupItem)], 1<<20)
	if err != nil {
		return view{}, err
	}
	set, unsupported, err := assertionauthor.ReadLenient(data)
	if err != nil {
		return view{}, err
	}
	summary := &CheckGroupSummary{Assertions: len(set.Assertions) + len(unsupported), Revision: item.RevisionLabel(), Unsupported: len(unsupported)}
	read := view{name: set.Name, summary: ItemSummary{CheckGroup: summary}}
	if len(unsupported) > 0 {
		return read, &unsupportedClauses{count: len(unsupported)}
	}
	return read, nil
}

// unsupportedClauses is a check group read whole whose count checks use an
// operator, or members, this release does not evaluate.
type unsupportedClauses struct{ count int }

func (u *unsupportedClauses) Error() string {
	if u.count == 1 {
		return "one check uses an operator this release does not evaluate"
	}
	return strconv.Itoa(u.count) + " checks use an operator this release does not evaluate"
}

func readProfile(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	path := paths[primaryRole(ProfileItem)]
	data, err := boundedFile(path, 4<<20)
	if err != nil {
		return view{}, err
	}
	schema, _ := sniffSchema(path)
	switch {
	case strings.HasPrefix(schema, "readmit-local-profile/"):
		profile, err := profileeval.DecodeProfile(data)
		if err != nil {
			return view{}, err
		}
		definition := profile.Definition
		return view{name: definition.Identity.ID, summary: ItemSummary{Profile: &ProfileSummary{Form: "local-profile", Family: definition.Base.Family,
			ProtocolVersion: definition.Base.HL7Version, PublishedVersion: definition.Identity.Version}}}, nil
	case strings.HasPrefix(schema, "readmit-profile-pack/"):
		pack, err := profileeval.DecodePack(data)
		if err != nil {
			return view{}, err
		}
		metadata := pack.Metadata
		summary := &ProfileSummary{Form: "profile-pack", PublishedVersion: metadata.Identity.Version}
		if len(metadata.Coverage) == 1 {
			summary.Family, summary.ProtocolVersion = metadata.Coverage[0].Family, metadata.Coverage[0].HL7Version
		}
		return view{name: metadata.Identity.ID, summary: ItemSummary{Profile: summary}}, nil
	}
	if _, err := profilepackage.Decode(data); err != nil {
		return view{}, err
	}
	return view{summary: ItemSummary{Profile: &ProfileSummary{Form: "profile-package"}}}, nil
}

func readScenario(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	path := paths[primaryRole(ScenarioItem)]
	data, err := boundedFile(path, 4<<20)
	if err != nil {
		return view{}, err
	}
	if declares(path, scenariogen.Schema) {
		plan, err := scenariogen.Decode(data)
		if err != nil {
			return view{}, err
		}
		workflow, err := scenario.DecodeDocument(plan.Template)
		if err != nil {
			return view{}, err
		}
		identity := workflow.Identity()
		seed, base := plan.Seed, catalog.Stamp(workflow.BaseTime)
		metadata, err := readMetadata(paths)
		if err != nil {
			return view{}, err
		}
		return view{name: identity.ID, summary: ItemSummary{Scenario: &ScenarioSummary{Version: identity.Version, Profile: string(workflow.Profile),
			Family: lifecycleFamily(workflow.Profile), Plan: true, Seed: &seed, BaseTime: &base, GeneratorVersion: plan.GeneratorVersion,
			LocalProfile: metadata.Profile}}}, nil
	}
	if declares(path, scenario.OrderSchema) {
		orders, err := scenario.DecodeOrders(data)
		if err != nil {
			return view{}, err
		}
		return view{name: orders.Scenario.ID, summary: ItemSummary{Scenario: &ScenarioSummary{Version: orders.Scenario.Version, Profile: string(orders.Profile),
			Family: lifecycleFamily(orders.Profile)}}}, nil
	}
	declared, err := scenario.Decode(data)
	if err != nil {
		return view{}, err
	}
	return view{name: declared.Scenario.ID, summary: ItemSummary{Scenario: &ScenarioSummary{Version: declared.Scenario.Version, Profile: string(declared.Profile),
		Family: lifecycleFamily(declared.Profile)}}}, nil
}

// lifecycleFamily is the message family a lifecycle profile generates, the
// inverse of lifecycleForFamily; empty for a profile this release does not
// implement.
func lifecycleFamily(profile scenario.ProfileName) string {
	for _, family := range []string{"ADT", "SIU", "ORM", "ORU"} {
		if named, _ := lifecycleForFamily(family); named == profile {
			return family
		}
	}
	return ""
}

// validateCheckGroupDraft validates a whole check group: no unsupported check
// is held, every check reads on its own and the group reads as one set.
// A check with no identity is given a stable one; an existing one keeps its.
func validateCheckGroupDraft(draft ItemDraft) ([]catalog.Staged, *CheckGroupDraft, []FieldProblem) {
	problems := []FieldProblem{}
	group := *draft.CheckGroup
	if len(group.Unsupported) > 0 {
		problems = append(problems, FieldProblem{Field: "check_group.unsupported",
			Problem: "this group holds checks this release does not evaluate; nothing is saved while one is held, and none is removed for you"})
	}
	set := group.Set
	if set.Schema == "" {
		set.Schema = assertionauthor.Schema
	}
	if set.Name == "" {
		set.Name = draft.Name
	}
	set.Assertions = slices.Clone(set.Assertions)
	taken := map[string]bool{}
	for _, clause := range set.Assertions {
		taken[clause.ID] = true
	}
	for i := range set.Assertions {
		if set.Assertions[i].ID != "" {
			continue
		}
		id := "check-" + strconv.Itoa(i+1)
		for n := 2; taken[id]; n++ {
			id = "check-" + strconv.Itoa(i+1) + "-" + strconv.Itoa(n)
		}
		taken[id], set.Assertions[i].ID = true, id
	}
	for i, clause := range set.Assertions {
		if _, err := assertionauthor.Generate(assertionauthor.Draft{Schema: set.Schema, Name: "check", Assertions: []assertionauthor.Clause{clause}}); err != nil {
			problems = append(problems, FieldProblem{Field: "check_group.set.assertions." + strconv.Itoa(i), Problem: err.Error()})
		}
	}
	if len(problems) > 0 {
		return nil, nil, problems
	}
	names, found := checkNames(group.Names, checkIDs(CheckGroupDraft{Set: set}))
	if len(found) > 0 {
		return nil, nil, found
	}
	data, err := assertionauthor.Generate(set)
	if err != nil {
		return nil, nil, []FieldProblem{{Field: "check_group.set", Problem: err.Error()}}
	}
	metadata, err := metadataMember(libraryMetadata{Names: names})
	if err != nil {
		return nil, nil, []FieldProblem{{Field: "check_group.names", Problem: err.Error()}}
	}
	return append([]catalog.Staged{{Role: checkGroupRole, File: "checks.json", Data: data}}, metadata...),
		&CheckGroupDraft{Set: set, Unsupported: []assertionauthor.UnsupportedClause{}, Names: names}, nil
}

// pinnedPack is the project's metadata pack a profile pins, by its reference,
// or nil when the project holds none or holds two different documents that
// both claim it.
func (c *loadedCatalog) pinnedPack(pin profilepack.Identity) (*ItemRef, profilepack.Pack) {
	var found *ItemRef
	var chosen profilepack.Pack
	for _, item := range c.document.Items {
		if item.Kind != string(ProfileItem) || c.removed(item) {
			continue
		}
		ref := ItemRef{Kind: ProfileItem, ID: item.ID, Revision: item.RevisionLabel()}
		pack, err := c.packOf(ref)
		if err != nil || pack.Satisfies(pin) != nil {
			continue
		}
		if found != nil {
			return nil, profilepack.Pack{}
		}
		found, chosen = &ref, pack
	}
	return found, chosen
}

// validateScenarioDraft validates a whole generator plan: its typed template
// written into the plan, the plan read by the generator's own reader, and a
// seed the window carries exactly.
func validateScenarioDraft(draft ItemDraft, version string) ([]catalog.Staged, *ScenarioDraft, []FieldProblem) {
	plan := draft.Scenario.Plan
	if plan.Schema == "" {
		plan.Schema = scenariogen.Schema
	}
	if plan.GeneratorVersion == "" {
		plan.GeneratorVersion = scenariogen.Version
	}
	if template := draft.Scenario.Template; template != nil {
		workflow := *template
		if workflow.Schema == "" {
			workflow.Schema = scenario.Schema
		}
		if workflow.Scenario.ID == "" {
			// A scenario is named by the name it is saved under; one not
			// named yet, as a preview of a fresh draft is, reads as a scenario.
			workflow.Scenario.ID = cmp.Or(actionSlug(draft.Name), "scenario")
		}
		if version != "" {
			workflow.Scenario.Version = version
		} else if workflow.Scenario.Version == "" {
			workflow.Scenario.Version = "1"
		}
		encoded, err := json.Marshal(workflow, json.Deterministic(true))
		if err != nil {
			return nil, nil, []FieldProblem{{Field: "scenario.template", Problem: "the workflow cannot be encoded"}}
		}
		plan.Template = encoded
	}
	if plan.Seed > maxWindowSeed {
		return nil, nil, []FieldProblem{{Field: "scenario.plan.seed", Problem: "a seed is a whole number from 0 to " + strconv.FormatUint(maxWindowSeed, 10)}}
	}
	data, err := json.Marshal(plan, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, nil, []FieldProblem{{Field: "scenario.plan", Problem: "the plan cannot be encoded"}}
	}
	data = append(data, '\n')
	decoded, err := scenariogen.Decode(data)
	if err != nil {
		return nil, nil, []FieldProblem{{Field: "scenario.plan", Problem: err.Error()}}
	}
	return []catalog.Staged{{Role: scenarioRole, File: "plan.json", Data: data}}, scenarioDraftOf(decoded), nil
}

// scenarioDraftOf is a decoded plan as an editor holds it, with its lifecycle
// workflow typed when the template is one.
func scenarioDraftOf(plan scenariogen.Plan) *ScenarioDraft {
	held := &ScenarioDraft{Plan: plan}
	if declared, err := scenario.Decode(plan.Template); err == nil {
		held.Template = &declared
	}
	return held
}

func verifyCheckGroup(files map[string]string) error {
	data, err := boundedFile(files[checkGroupRole], assertion.MaxSetBytes)
	if err != nil {
		return err
	}
	if _, err = assertion.Decode(data); err != nil {
		return err
	}
	_, err = readMetadata(files)
	return err
}

func verifyScenario(files map[string]string) error {
	data, err := boundedFile(files[scenarioRole], scenariogen.MaxBytes)
	if err != nil {
		return err
	}
	if _, err = scenariogen.Decode(data); err != nil {
		return err
	}
	_, err = readMetadata(files)
	return err
}

// itemAt is an object as it was at one of its revisions: the object itself
// when the revision is its current one or none is named.
func itemAt(item catalog.Item, revision string) (catalog.Item, bool) {
	if revision == "" || revision == item.RevisionLabel() {
		return item, true
	}
	for i, published := range item.Revisions {
		if strconv.Itoa(published.Number) == revision {
			item.Revisions = item.Revisions[:i+1]
			return item, true
		}
	}
	return catalog.Item{}, false
}

// libraryDraftOf reads the saved members of one library object into the draft
// its editor starts from.
func (a *App) libraryDraftOf(c *loadedCatalog, record catalog.Item, name string) (ItemDraft, error) {
	paths, availability, reason := c.backing(record)
	if availability != ItemAvailable {
		return ItemDraft{}, errors.New(reason)
	}
	draft := ItemDraft{Name: name}
	switch ItemKind(record.Kind) {
	case CheckGroupItem:
		data, err := boundedFile(paths[checkGroupRole], assertion.MaxSetBytes)
		if err != nil {
			return ItemDraft{}, err
		}
		set, unsupported, err := assertionauthor.ReadLenient(data)
		if err != nil {
			return ItemDraft{}, err
		}
		metadata, err := readMetadata(paths)
		if err != nil {
			return ItemDraft{}, err
		}
		draft.CheckGroup = &CheckGroupDraft{Set: set, Unsupported: unsupported, Names: metadata.Names}
	case ProfileItem:
		profile, origin, err := readLocalProfile(paths)
		if err != nil {
			return ItemDraft{}, err
		}
		pack, _ := c.pinnedPack(profile.Base.Pack)
		draft.Profile = &ProfileDraft{Profile: profile, Pack: pack, Origin: origin}
	case ScenarioItem:
		data, err := boundedFile(paths[scenarioRole], scenariogen.MaxBytes)
		if err != nil {
			return ItemDraft{}, err
		}
		held, err := a.scenarioFromDocument(data)
		if err != nil {
			return ItemDraft{}, err
		}
		metadata, err := readMetadata(paths)
		if err != nil {
			return ItemDraft{}, err
		}
		held.Profile, draft.Scenario = metadata.Profile, held
	}
	return draft, nil
}

// scenarioFromDocument is the plan a scenario document is edited as: a
// generator plan exactly as saved, or a workflow document made the template
// of a new plan, which allocates its seed once and keeps the workflow's own
// base time.
func (a *App) scenarioFromDocument(data []byte) (*ScenarioDraft, error) {
	var declared struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &declared) != nil {
		return nil, errors.New("the scenario cannot be read")
	}
	if declared.Schema == scenariogen.Schema {
		plan, err := scenariogen.Decode(data)
		if err != nil {
			return nil, err
		}
		if plan.Seed > maxWindowSeed {
			return nil, errors.New("the plan's seed is larger than this window carries exactly; it is left as written")
		}
		return scenarioDraftOf(plan), nil
	}
	workflow, err := scenario.DecodeDocument(data)
	if err != nil {
		return nil, err
	}
	// The plan generator writes no resource workflow, so a draft of one could
	// never be saved; its cases come from a case generation request.
	if workflow.Schema == scenario.ResourceSchema {
		return nil, errors.New("a resource workflow is not edited here; generate its cases from a case generation request")
	}
	template, err := json.Marshal(workflow, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("the scenario cannot be read")
	}
	return scenarioDraftOf(basicPlan(a.seed(), template)), nil
}

// seed is a new plan's seed, allocated once when its draft is created and a
// number the window carries exactly.
func (a *App) seed() uint64 {
	if a.seeds != nil {
		return a.seeds() & maxWindowSeed
	}
	return rand.Uint64N(maxWindowSeed + 1)
}

// basicPlan is a plan over one workflow: one synthetic row and the baseline
// variant, which changes nothing.
func basicPlan(seed uint64, template []byte) scenariogen.Plan {
	return scenariogen.Plan{
		Schema: scenariogen.Schema, GeneratorVersion: scenariogen.Version, Seed: seed, Template: template,
		Rows:     []scenariogen.Row{{ID: "basic", PatientName: "SYNTHETIC PATIENT", Notes: []string{}, Encoding: "utf-8"}},
		Variants: []scenariogen.Variant{{ID: "baseline", Mutations: []scenariogen.Mutation{}}},
	}
}

// ScenarioTemplate is one named workflow a new scenario starts from: its
// lifecycle profile, the family it generates, and the subjects and ordered
// steps it declares. Every identifier in it is synthetic.
type ScenarioTemplate struct {
	ID       string               `json:"id"`
	Name     string               `json:"name"`
	Profile  scenario.ProfileName `json:"profile"`
	Family   string               `json:"family"`
	Subjects []scenario.Subject   `json:"subjects"`
	Steps    []scenario.Step      `json:"steps"`
}

// scenarioTemplates are the named workflows this release starts a scenario
// from, one per lifecycle family its generator implements as a workflow.
func scenarioTemplates() []ScenarioTemplate {
	patient := scenario.Subject{ID: "patient-a", Kind: scenario.PatientSubject, Namespace: "READMIT", Identifier: "SYNTH-PATIENT-A", InitialState: scenario.PatientActive}
	return []ScenarioTemplate{
		{ID: "siu-basic", Name: "Booking and reschedule", Profile: scenario.SIULifecycle, Family: "SIU",
			Subjects: []scenario.Subject{patient, {ID: "appointment-a", Kind: scenario.AppointmentSubject, Namespace: "READMIT", Identifier: "SYNTH-APPOINTMENT-A",
				Patient: "patient-a", InitialState: scenario.AppointmentNone}},
			Steps: []scenario.Step{
				{ID: "booking", Event: "S12", Subject: "appointment-a", After: "0s", Expect: scenario.Accepted},
				{ID: "reschedule", Event: "S13", Subject: "appointment-a", After: "10m", Expect: scenario.Accepted},
			}},
		{ID: "adt-basic", Name: "Admission and discharge", Profile: scenario.ADTLifecycle, Family: "ADT",
			Subjects: []scenario.Subject{patient, {ID: "visit-a", Kind: scenario.VisitSubject, Namespace: "READMIT", Identifier: "SYNTH-VISIT-A",
				Patient: "patient-a", InitialState: scenario.VisitNone}},
			Steps: []scenario.Step{
				{ID: "admit", Event: "A01", Subject: "visit-a", After: "0s", Expect: scenario.Accepted},
				{ID: "discharge", Event: "A03", Subject: "visit-a", After: "4h", Expect: scenario.Accepted},
			}},
	}
}

// newScenarioDraft is the draft a new scenario starts from: the basic SIU
// workflow at the current time truncated to the whole second the generator
// declares, under a seed allocated once, now.
func (a *App) newScenarioDraft() *ScenarioDraft {
	basic := scenarioTemplates()[0]
	workflow := scenario.Scenario{Schema: scenario.Schema, Scenario: scenario.Identity{Version: "1"}, Profile: basic.Profile,
		BaseTime: a.now().UTC().Truncate(time.Second), Subjects: basic.Subjects, Steps: basic.Steps}
	template, _ := json.Marshal(workflow, json.Deterministic(true))
	return &ScenarioDraft{Plan: basicPlan(a.seed(), template), Template: &workflow}
}

// newLibraryDraft is the draft a new library object starts from.
func (a *App) newLibraryDraft(kind ItemKind) ItemDraft {
	switch kind {
	case CheckGroupItem:
		return ItemDraft{CheckGroup: &CheckGroupDraft{Set: assertionauthor.NewDraft(), Unsupported: []assertionauthor.UnsupportedClause{}}}
	case ProfileItem:
		return ItemDraft{Profile: &ProfileDraft{Profile: localprofile.Profile{Schema: localprofile.Schema,
			Identity: localprofile.Identity{Version: "1"}, Segments: []localprofile.Segment{}}}}
	}
	return ItemDraft{Scenario: a.newScenarioDraft()}
}

// ProfileResolutionResult is an unsaved profile draft resolved against the
// metadata pack it pins: every rule with its origin (base, overridden or
// local), the pack's support for its combination, and the seal a save would
// publish. Problems are the reasons it cannot be resolved yet.
type ProfileResolutionResult struct {
	State      State                    `json:"state"`
	Reason     string                   `json:"reason,omitzero"`
	Context    RequestContext           `json:"context"`
	Problems   []FieldProblem           `json:"problems"`
	Resolution *localprofile.Resolution `json:"resolution,omitzero"`
	Support    *profilepack.Outcomes    `json:"support,omitzero"`
	Seal       *profileversion.Version  `json:"seal,omitzero"`
}

func (r *ProfileResolutionResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ResolveProfileDraft resolves an unsaved profile draft against the pack
// chosen or, with none chosen, the project's pack its pin names. It is a read:
// nothing is saved, and a field no pack supports is reported as it is.
func (a *App) ResolveProfileDraft(request DraftRequest) ProfileResolutionResult {
	return run(a, false, false, func(ctx context.Context) ProfileResolutionResult {
		result := ProfileResolutionResult{Context: request.Context, Problems: []FieldProblem{}}
		if request.Kind != ProfileItem || request.Draft.Profile == nil {
			result.refuse(Failed, "a profile draft is resolved")
			return result
		}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		scope := draftScope{root: loaded.root, loaded: loaded, item: request.Item}
		profile, ref, problems := scope.profileWithPack(request.Draft.Name, *request.Draft.Profile)
		result.State, result.Problems = Completed, problems
		if err := profile.Validate(); err != nil {
			result.Problems = append(result.Problems, FieldProblem{Field: "profile.profile", Problem: err.Error()})
		}
		if len(result.Problems) > 0 {
			return result
		}
		var pack profilepack.Pack
		if ref != nil {
			pack, _ = loaded.packOf(*ref)
		} else if _, found := loaded.pinnedPack(profile.Base.Pack); found.Satisfies(profile.Base.Pack) == nil {
			pack = found
		} else {
			pack = profilelibrary.FindPinned(loaded.root, profile.Base.Pack)
		}
		resolution := localprofile.Resolve(profile, pack)
		support := resolution.Support
		result.Resolution, result.Support = &resolution, &support
		if seal, err := profileversion.Seal(profile); err == nil {
			result.Seal = &seal
		}
		return result
	})
}

// AffectedTest is one saved test that pins a version of a profile: the test,
// the exact pin it records, and what the change from that version to the one
// asked about says about its contract. Impact is one of profileversion's
// closed set, or unknown when the pinned version's document is not in the
// project, so nothing can say whether it changed.
type AffectedTest struct {
	Ref    ItemRef            `json:"ref"`
	Name   string             `json:"name"`
	Pinned profileversion.Pin `json:"pinned"`
	Impact string             `json:"impact"`
}

// ImpactUnknown is a pinned version whose document the project does not hold.
const ImpactUnknown = "unknown"

// AffectedTestsResult lists the saved tests pinning a profile.
type AffectedTestsResult struct {
	State   State                  `json:"state"`
	Reason  string                 `json:"reason,omitzero"`
	Context RequestContext         `json:"context"`
	Profile *localprofile.Identity `json:"profile,omitzero"`
	Tests   []AffectedTest         `json:"tests"`
}

func (r *AffectedTestsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ProfileAffectedTests lists the project's released tests that pin a version
// of this profile, each with the impact profileversion.Assess states for the
// change from the version it pins to the version asked about. It reads; no pin
// moves.
func (a *App) ProfileAffectedTests(request ItemRequest) AffectedTestsResult {
	return run(a, false, false, func(ctx context.Context) AffectedTestsResult {
		result := AffectedTestsResult{Context: request.Context, Tests: []AffectedTest{}}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Ref, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		record, ok := itemAt(loaded.document.Items[loaded.document.Find(item.Ref.ID)], request.Ref.Revision)
		if !ok {
			result.refuse(Failed, "the profile has no such revision")
			return result
		}
		paths, availability, reason := loaded.backing(record)
		if availability != ItemAvailable {
			result.refuse(Failed, reason)
			return result
		}
		current, _, err := readLocalProfile(paths)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		versions := map[string]localprofile.Profile{}
		for _, earlier := range (draftScope{root: loaded.root, loaded: loaded, item: record.ID}).earlierProfiles() {
			versions[earlier.Identity.Version] = earlier
		}
		for _, test := range loaded.document.Items {
			if test.Kind != string(TestItem) || loaded.removed(test) {
				continue
			}
			paths, availability, _ := loaded.backing(test)
			path := paths[primaryRole(TestItem)]
			if availability != ItemAvailable || !declares(path, expectation.Schema) {
				continue
			}
			release, err := expectation.Read(path)
			if err != nil {
				continue
			}
			for _, pinned := range release.Profiles {
				if pinned.Profile.ID != current.Identity.ID {
					continue
				}
				affected := AffectedTest{Ref: ItemRef{Kind: TestItem, ID: test.ID, Revision: test.RevisionLabel()},
					Name: cmp.Or(test.Name, release.Baseline.Spec.Name), Pinned: pinned.Pin(), Impact: pinImpact(pinned, current, versions, path, release)}
				result.Tests = append(result.Tests, affected)
			}
		}
		slices.SortFunc(result.Tests, func(x, y AffectedTest) int { return strings.Compare(x.Name+x.Ref.ID, y.Name+y.Ref.ID) })
		identity := current.Identity
		result.State, result.Profile = Completed, &identity
		if len(result.Tests) == 0 {
			result.State = Empty
		}
		return result
	})
}

// pinImpact is what the change from a test's pinned version to the current
// profile says about the test, as profileversion.Assess decides it.
func pinImpact(pinned profileversion.Version, current localprofile.Profile, versions map[string]localprofile.Profile, path string, release expectation.Release) string {
	from := current
	if pinned.Profile.Version != current.Identity.Version {
		earlier, held := versions[pinned.Profile.Version]
		if !held {
			return ImpactUnknown
		}
		from = earlier
	}
	if pinned.Verify(from) != nil {
		return ImpactUnknown
	}
	if pinned.Profile.Version == current.Identity.Version {
		return string(profileversion.ImpactCurrent)
	}
	comparison, err := profileversion.Compare(from, current)
	if err != nil {
		return ImpactUnknown
	}
	data, err := boundedFile(path, expectation.MaxBytes)
	if err != nil {
		return ImpactUnknown
	}
	sum := sha256.Sum256(data)
	assessment, err := profileversion.Assess(comparison, profileversion.References{Schema: profileversion.ReferencesSchema,
		Tests: []profileversion.Reference{{Test: filepath.Base(path), Case: cmp.Or(release.Baseline.Spec.Input.Case, filepath.Base(path)),
			SHA256: hex.EncodeToString(sum[:]), Pinned: pinned.Pin()}}})
	if err != nil || len(assessment.Tests) != 1 {
		return ImpactUnknown
	}
	return string(assessment.Tests[0].Impact)
}

// MetadataPack is one metadata pack of the project as the catalog lists it —
// kept when it is missing, unreadable or unsupported, with its reason — and,
// when it reads, its identity, provenance and support matrix: every one of the
// HL7 versions and message families this release names, answered at each of
// the four levels exactly as the pack declares, unknown where it declares
// nothing.
type MetadataPack struct {
	Item       CatalogItem             `json:"item"`
	Pack       *profilepack.Identity   `json:"pack,omitzero"`
	Provenance *profilepack.Provenance `json:"provenance,omitzero"`
	Bundleable bool                    `json:"bundleable"`
	Matrix     []ProfileLibraryRow     `json:"matrix"`
}

// MetadataPacksResult lists the project's metadata packs.
type MetadataPacksResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Packs   []MetadataPack `json:"packs"`
}

func (r *MetadataPacksResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// MetadataPacks lists the metadata packs the project holds, read-only. A pack
// that does not read stays listed with its reason; support nothing declares
// is answered unknown, never inferred.
func (a *App) MetadataPacks(request RequestContext) MetadataPacksResult {
	return run(a, false, false, func(ctx context.Context) MetadataPacksResult {
		result := MetadataPacksResult{Context: request, Packs: []MetadataPack{}}
		loaded, declined := a.loadCatalog(ctx, request, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		for _, item := range loaded.document.Items {
			if item.Kind != string(ProfileItem) || loaded.removed(item) {
				continue
			}
			// A pack is one the project holds as a file, or one a profile
			// recorded from an imported package, listed under the pack's name.
			path, carried := filepath.Join(loaded.root, item.Entry), false
			if len(item.Revisions) > 0 {
				paths, availability, _ := loaded.backing(item)
				if availability != ItemAvailable {
					continue
				}
				path, carried = paths[packRole]
				if !carried {
					path = paths[profileRole]
				}
			}
			if schema, ok := sniffSchema(path); !ok || !strings.HasPrefix(schema, "readmit-profile-pack/") {
				continue
			}
			listed := MetadataPack{Item: loaded.read(item), Matrix: []ProfileLibraryRow{}}
			if carried {
				if pack, err := loaded.packOf(listed.Item.Ref); err == nil {
					listed.Item.Name = pack.Identity.ID
				}
			}
			if pack, err := loaded.packOf(listed.Item.Ref); err == nil {
				identity, provenance := pack.Identity, pack.Provenance
				listed.Pack, listed.Provenance, listed.Bundleable = &identity, &provenance, pack.Bundleable() == nil
				for _, version := range profilepack.HL7Versions() {
					for _, family := range profilepack.Families() {
						row := ProfileLibraryRow{HL7Version: version, Family: family, Outcomes: pack.Outcomes(version, family)}
						if row.Outcomes.Covered() {
							row.Pack = identity
						}
						listed.Matrix = append(listed.Matrix, row)
					}
				}
			}
			result.Packs = append(result.Packs, listed)
		}
		slices.SortFunc(result.Packs, func(x, y MetadataPack) int {
			return strings.Compare(strings.ToLower(x.Item.Name)+x.Item.Ref.ID, strings.ToLower(y.Item.Name)+y.Item.Ref.ID)
		})
		result.State = Completed
		if len(result.Packs) == 0 {
			result.State = Empty
		}
		return result
	})
}

// ChooseLibraryFile presents the host's file dialog for one library document
// to import: a check group ("check-group"), a profile or profile package
// ("profile") or a scenario plan ("scenario"). Choosing reads and writes
// nothing.
func (a *App) ChooseLibraryFile(kind string) PathChoiceResult {
	return run(a, true, false, func(ctx context.Context) PathChoiceResult {
		title := ""
		switch ItemKind(kind) {
		case CheckGroupItem:
			title = "Import check group"
		case ProfileItem:
			title = "Import profile"
		case ScenarioItem:
			title = "Import scenario"
		default:
			return PathChoiceResult{State: Failed, Reason: "unknown library file kind"}
		}
		files, declined := a.chooseFiles(ctx, title, "readmit documents (*.json)", "*.json")
		if len(files) == 0 {
			return PathChoiceResult{State: declined.state, Reason: declined.reason}
		}
		return PathChoiceResult{State: Completed, Kind: kind, Paths: files[:1]}
	})
}

// LibraryImportRequest names one library document chosen elsewhere on this
// computer, by its full path, and the kind it is imported as.
type LibraryImportRequest struct {
	Context RequestContext `json:"context"`
	Kind    ItemKind       `json:"kind"`
	Path    string         `json:"path"`
}

// ImportLibraryItem reads one chosen library document into a new draft, which
// the editor's one Save publishes. It writes nothing. A check group's
// unsupported checks are kept as such; a profile package is verified and its
// origin kept; a workflow document becomes the template of a new plan.
func (a *App) ImportLibraryItem(request LibraryImportRequest) ItemDraftResult {
	return run(a, false, false, func(ctx context.Context) ItemDraftResult {
		result := ItemDraftResult{Context: request.Context}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if !filepath.IsAbs(request.Path) {
			result.refuse(Failed, "choose the document to import with the file dialog")
			return result
		}
		data, err := readChosenFile(request.Path, profilepackage.MaxBytes)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				result.refuse(PermissionDenied, "this account cannot read the chosen document")
				return result
			}
			result.refuse(Failed, "the chosen document cannot be read")
			return result
		}
		draft := ItemDraft{}
		switch request.Kind {
		case CheckGroupItem:
			set, unsupported, err := assertionauthor.ReadLenient(data)
			if err != nil {
				result.refuse(Failed, err.Error())
				return result
			}
			draft.Name, draft.CheckGroup = set.Name, &CheckGroupDraft{Set: set, Unsupported: unsupported}
		case ProfileItem:
			profile, origin, carried, err := importedProfile(data)
			if err != nil {
				result.refuse(Failed, err.Error())
				return result
			}
			pack, _ := loaded.pinnedPack(profile.Base.Pack)
			draft.Name, draft.Profile = profile.Identity.ID, &ProfileDraft{Profile: profile, Pack: pack, Origin: origin}
			// A package whose pack the project does not hold yet carries it
			// into the draft, and the profile's Save records it.
			if pack == nil && carried != nil {
				draft.Profile.PackDocument = string(carried)
			}
		case ScenarioItem:
			held, err := a.scenarioFromDocument(data)
			if err != nil {
				result.refuse(Failed, err.Error())
				return result
			}
			var identity struct {
				Scenario scenario.Identity `json:"scenario"`
			}
			_ = json.Unmarshal(held.Plan.Template, &identity)
			draft.Name, draft.Scenario = identity.Scenario.ID, held
		default:
			result.refuse(Failed, "a check group, a profile or a scenario is imported")
			return result
		}
		result.State, result.Draft, result.New = Completed, &draft, true
		result.Ref = &ItemRef{Kind: request.Kind}
		return result
	})
}

// importedProfile is the profile and origin one chosen document carries: a
// local profile, or a verified package with its origin and the exact metadata
// pack document it carries.
func importedProfile(data []byte) (localprofile.Profile, *profilepackage.Origin, []byte, error) {
	var declared struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &declared) != nil {
		return localprofile.Profile{}, nil, nil, errors.New("the chosen document is not JSON")
	}
	switch {
	case declared.Schema == profilepackage.Schema:
		profile, origin, err := profileFromPackage(data)
		if err != nil {
			return localprofile.Profile{}, nil, nil, err
		}
		verified, err := profilepackage.Decode(data)
		if err != nil {
			return localprofile.Profile{}, nil, nil, err
		}
		documents, err := verified.Documents()
		if err != nil {
			return localprofile.Profile{}, nil, nil, err
		}
		return profile, origin, documents["pack.json"], nil
	case strings.HasPrefix(declared.Schema, "readmit-profile-pack/"):
		return localprofile.Profile{}, nil, nil, errors.New("a metadata pack is not a profile; it is read under Metadata packs")
	}
	profile, err := localprofile.Decode(data)
	return profile, nil, nil, err
}

// LibraryExportRequest exports one library object at one revision to a new
// file. Destination is the full path the host's save dialog named; left
// empty, the dialog asks for it.
type LibraryExportRequest struct {
	Context     RequestContext `json:"context"`
	Ref         ItemRef        `json:"ref"`
	Destination string         `json:"destination,omitzero"`
}

// LibraryExportResult is the file an export wrote.
type LibraryExportResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Path    string         `json:"path,omitzero"`
	Schema  string         `json:"schema,omitzero"`
	Bytes   int            `json:"bytes"`
	SHA256  string         `json:"sha256,omitzero"`
}

func (r *LibraryExportResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ExportLibraryItem writes one library object's current contract bytes, at
// the revision named, to one new file: a check group's assertion set and a
// scenario's plan exactly as saved, and a profile as a package carrying its
// seal, the metadata pack it pins, and its origin and attribution. A profile
// that records no origin is not packaged, and the refusal says so.
func (a *App) ExportLibraryItem(request LibraryExportRequest) LibraryExportResult {
	return run(a, true, false, func(ctx context.Context) LibraryExportResult {
		result := LibraryExportResult{Context: request.Context}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Ref, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		kind := request.Ref.Kind
		if kind != CheckGroupItem && kind != ProfileItem && kind != ScenarioItem {
			result.refuse(Failed, "a check group, a profile or a scenario is exported")
			return result
		}
		record, ok := itemAt(loaded.document.Items[loaded.document.Find(item.Ref.ID)], request.Ref.Revision)
		if !ok {
			result.refuse(Failed, "the object has no such revision")
			return result
		}
		paths, availability, reason := loaded.backing(record)
		if availability != ItemAvailable {
			result.refuse(Failed, reason)
			return result
		}
		data, suggested, err := loaded.exported(kind, paths)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		destination := request.Destination
		if destination == "" {
			chosen, declined := a.chooseNamedDestination(ctx, "Export "+strings.ReplaceAll(string(kind), "-", " "), cmp.Or(actionSlug(item.Name), suggested)+".json")
			if chosen == "" {
				result.refuse(declined.state, declined.reason)
				return result
			}
			destination = chosen
		}
		if !filepath.IsAbs(destination) {
			result.refuse(Failed, "choose the export's destination with the save dialog")
			return result
		}
		folder, declined := chosenFolder(filepath.Dir(destination))
		if folder == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		path := filepath.Join(folder, filepath.Base(destination))
		if err := operation.WriteNewFile(path, data, "cannot create the exported file; the destination must be new and writable", "cannot write the exported file"); err != nil {
			result.refuse(refusalState(err), err.Error())
			return result
		}
		var declared struct {
			Schema string `json:"schema"`
		}
		_ = json.Unmarshal(data, &declared)
		result.State, result.Path, result.Schema, result.Bytes, result.SHA256 = Completed, path, declared.Schema, len(data), digestOf(data)
		return result
	})
}

// exported is the contract bytes one library object is exported as, and the
// name a save dialog offers for them.
func (c *loadedCatalog) exported(kind ItemKind, paths map[string]string) ([]byte, string, error) {
	switch kind {
	case CheckGroupItem:
		data, err := boundedFile(paths[checkGroupRole], assertion.MaxSetBytes)
		return data, "checks", err
	case ScenarioItem:
		data, err := boundedFile(paths[scenarioRole], scenariogen.MaxBytes)
		return data, "scenario", err
	}
	data, err := boundedFile(paths[profileRole], profilepackage.MaxBytes)
	if err != nil {
		return nil, "", err
	}
	switch schema, _ := sniffSchema(paths[profileRole]); {
	case schema == profilepackage.Schema:
		return data, "profile", nil
	case strings.HasPrefix(schema, "readmit-profile-pack/"):
		return data, "metadata-pack", nil
	}
	originPath, held := paths[originRole]
	if !held {
		return nil, "", errors.New("this profile records no origin and attribution, so it is not exported as a package; record its origin and save it first")
	}
	origin, err := boundedFile(originPath, profilepackage.MaxOriginBytes)
	if err != nil {
		return nil, "", err
	}
	profile, err := localprofile.Decode(data)
	if err != nil {
		return nil, "", err
	}
	var seal []byte
	if sealPath, held := paths[sealRole]; held {
		if seal, err = boundedFile(sealPath, profileversion.MaxVersionBytes); err != nil {
			return nil, "", err
		}
	} else {
		sealed, err := profileversion.Seal(profile)
		if err != nil {
			return nil, "", err
		}
		if seal, err = sealed.Encode(); err != nil {
			return nil, "", err
		}
	}
	ref, _ := c.pinnedPack(profile.Base.Pack)
	if ref == nil {
		return nil, "", errors.New("the metadata pack this profile pins is not in the project, so the package cannot carry it")
	}
	pack, err := c.packBytes(*ref)
	if err != nil {
		return nil, "", err
	}
	packaged, err := profilepackage.Export(data, pack, seal, origin)
	return packaged, "profile", err
}

// nextRevision is the revision number a save of the object an edit began
// from publishes, as text: a scenario's workflow carries it as its version,
// so the version the Library lists is the saved revision it reads. The
// revision this very submission already published is that number again, so a
// repeated click decides the same way.
func (s draftScope) nextRevision() string {
	if s.loaded == nil || s.item == "" {
		return "1"
	}
	index := s.loaded.document.Find(s.item)
	if index < 0 {
		return "1"
	}
	revisions := s.loaded.document.Items[index].Revisions
	for _, revision := range revisions {
		if s.intent != "" && revision.Intent == s.intent {
			return strconv.Itoa(revision.Number)
		}
	}
	if len(revisions) == 0 {
		return "1"
	}
	return strconv.Itoa(revisions[len(revisions)-1].Number + 1)
}
