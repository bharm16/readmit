package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// A named environment is one saved object: its target configuration and,
// when it has them, the send policy its destinations are decided under, the
// reset plan that returns it to its starting state and the links that name
// its observation and label its reset. Every member is published in one
// revision through the catalog, or none is.

// EnvironmentLinksSchema is the contract of an environment's links member.
const EnvironmentLinksSchema = "readmit-environment-links/v1"

// ProjectSecrets is the project entry that holds the project's credential
// references. An environment's credential names a reference in it.
const ProjectSecrets = "secrets.json"

// maxLinksBytes bounds one links member.
const maxLinksBytes = 64 << 10

// EnvironmentLinks is what an environment names beside its target: the
// catalog identity of the observation its results are collected through,
// empty for none, and the names a person gave its reset and each of the
// reset's actions, in the plan's order. The plan itself holds no names.
type EnvironmentLinks struct {
	Schema      string   `json:"schema,omitzero"`
	Observation string   `json:"observation,omitzero"`
	ResetName   string   `json:"reset_name,omitzero"`
	ActionNames []string `json:"action_names,omitzero"`
}

// TransportProblem is the problem an environment draft reports while its
// transport is not chosen.
const TransportProblem = "Choose a transport"

// validateEnvironmentDraft validates a whole environment draft: its target
// under the name the environment keeps, its approval decided from the
// transport the person chose, and each optional member against the target.
func validateEnvironmentDraft(scope draftScope, draft ItemDraft) ([]catalog.Staged, ItemDraft, []FieldProblem) {
	problems := []FieldProblem{}
	normalized := ItemDraft{Name: draft.Name}
	target := *draft.Environment
	chosen := target.Transport != ""
	if !chosen {
		problems = append(problems, FieldProblem{Field: "transport", Problem: TransportProblem})
		// The rest of the target is still validated, under the verified
		// transport, so every other problem is reported at once.
		target.Transport = "tls"
	}
	// Choosing a transport in the deliberate editor is the approval the
	// configuration records; nothing else sets it.
	target.ApprovedTransport = true
	name, problem := scope.environmentName(draft.Name, target.Name)
	if problem != nil {
		problems = append(problems, *problem)
	}
	target.Name = name
	if target.Credential.Reference != "" && target.Credential.SecretsFile == "" {
		target.Credential.SecretsFile = ProjectSecrets
	}
	declared, data, err := declaredTarget(target)
	if err != nil {
		problems = append(problems, FieldProblem{Field: "environment", Problem: err.Error()})
	}
	if !chosen {
		declared.Transport = ""
	}
	staged := []catalog.Staged{{Role: "target", File: "target.json", Data: data}}
	normalized.Environment = &declared

	if draft.SendPolicy != nil {
		policy := *draft.SendPolicy
		if policy.Schema == "" {
			policy.Schema = sendpolicy.PolicySchema
		}
		if policy.ApprovedDestinations == nil {
			policy.ApprovedDestinations = []string{}
		}
		encoded, err := encodeMember(policy)
		if err == nil {
			policy, err = sendpolicy.DecodePolicy(encoded)
		}
		if err != nil {
			problems = append(problems, FieldProblem{Field: "policy", Problem: err.Error()})
		} else {
			normalized.SendPolicy = &policy
			staged = append(staged, catalog.Staged{Role: "policy", File: "policy.json", Data: encoded})
		}
	}

	links := EnvironmentLinks{}
	if draft.Links != nil {
		links = *draft.Links
	}
	links.Schema = EnvironmentLinksSchema
	if draft.ResetPlan != nil {
		plan, found := normalizedPlan(*draft.ResetPlan, name, draft.Environment.Name, links.ActionNames)
		problems = append(problems, found...)
		encoded, err := encodeMember(plan)
		if err == nil {
			plan, err = fixturereset.DecodePlan(encoded)
		}
		switch {
		case len(found) > 0:
		case err != nil:
			problems = append(problems, FieldProblem{Field: "reset", Problem: err.Error()})
		default:
			normalized.ResetPlan = &plan
			staged = append(staged, catalog.Staged{Role: "reset", File: "reset.json", Data: encoded})
		}
	} else if links.ResetName != "" || len(links.ActionNames) > 0 {
		problems = append(problems, FieldProblem{Field: "links.reset_name", Problem: "only an environment with a reset plan names its reset"})
	}
	if links.ResetName != "" && !catalog.ValidName(links.ResetName) {
		problems = append(problems, FieldProblem{Field: "links.reset_name", Problem: nameRule})
	}
	for i, action := range links.ActionNames {
		if !catalog.ValidName(action) {
			problems = append(problems, FieldProblem{Field: "links.action_names." + strconv.Itoa(i), Problem: nameRule})
		}
	}
	if links.Observation != "" && !scope.holdsObservation(links.Observation) {
		problems = append(problems, FieldProblem{Field: "links.observation", Problem: "the project holds no such observation"})
	}
	if draft.Links != nil && (links.Observation != "" || links.ResetName != "" || len(links.ActionNames) > 0) {
		encoded, err := encodeMember(links)
		if err != nil {
			problems = append(problems, FieldProblem{Field: "links", Problem: err.Error()})
		} else {
			normalized.Links = &links
			staged = append(staged, catalog.Staged{Role: "links", File: "links.json", Data: encoded})
		}
	}
	return staged, normalized, problems
}

// normalizedPlan is a reset plan as a save records it: under this
// environment's name, each action with the authority its operator requires
// and an identity generated from the name a person gave it when it has none.
// A plan that names the environment the draft was opened from, as a
// duplicate's does, resets the environment it is now saved with.
func normalizedPlan(plan fixturereset.Plan, environment, drafted string, names []string) (fixturereset.Plan, []FieldProblem) {
	problems := []FieldProblem{}
	if plan.Schema == "" {
		plan.Schema = fixturereset.PlanSchema
	}
	switch plan.Environment {
	case "", environment, drafted:
		plan.Environment = environment
	default:
		problems = append(problems, FieldProblem{Field: "reset.environment", Problem: "a reset plan resets the environment it is saved with"})
	}
	if len(names) > 0 && len(names) != len(plan.Actions) {
		problems = append(problems, FieldProblem{Field: "links.action_names", Problem: "every reset action is named, in the plan's order"})
	}
	plan.Actions = slices.Clone(plan.Actions)
	taken := map[string]bool{}
	for _, action := range plan.Actions {
		taken[action.ID] = true
	}
	for i := range plan.Actions {
		action := &plan.Actions[i]
		if action.Authority == "" {
			if required, ok := fixturereset.RequiredAuthority(action.Operator); ok {
				action.Authority = required
			}
		}
		if action.ID != "" {
			continue
		}
		base := ""
		if i < len(names) {
			base = actionSlug(names[i])
		}
		if base == "" {
			base = "step"
		}
		id := base
		for n := 2; taken[id]; n++ {
			id = base + "-" + strconv.Itoa(n)
		}
		taken[id], action.ID = true, id
	}
	return plan, problems
}

// actionSlug is a reset action identity drawn from a name: lowercase letters,
// digits and '-', beginning with a letter, at most 56 bytes so a suffix fits.
func actionSlug(name string) string {
	var out strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9' && out.Len() > 0:
			if dash && out.Len() > 0 {
				out.WriteByte('-')
			}
			out.WriteRune(r)
			dash = false
		default:
			dash = true
		}
		if out.Len() >= 56 {
			break
		}
	}
	return strings.TrimSuffix(out.String(), "-")
}

// environmentSlug is a target name drawn from a display name: letters,
// digits, '-', '_' and '.', at most 60 bytes so a suffix fits.
func environmentSlug(name string) string {
	var out strings.Builder
	dash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.':
			if dash && out.Len() > 0 {
				out.WriteByte('-')
			}
			out.WriteRune(r)
			dash = false
		default:
			dash = true
		}
		if out.Len() >= 60 {
			break
		}
	}
	return out.String()
}

// environmentName is the name an environment's target records. It is drawn
// from the display name when the environment is created, made distinct from
// every other environment of the project, and never changes afterwards.
func (s draftScope) environmentName(display, declared string) (string, *FieldProblem) {
	if s.item != "" && s.loaded != nil {
		if index := s.loaded.document.Find(s.item); index >= 0 {
			if target, err := s.loaded.targetOf(s.loaded.document.Items[index]); err == nil && target.Name != "" {
				return target.Name, nil
			}
		}
	}
	base := environmentSlug(display)
	if base == "" {
		base = declared
	}
	if base == "" {
		return "", &FieldProblem{Field: "name", Problem: "an environment has a name"}
	}
	if s.item != "" || s.loaded == nil {
		return base, nil
	}
	taken := s.loaded.environmentNames(s.intent)
	name := base
	for n := 2; taken[name]; n++ {
		name = base + "-" + strconv.Itoa(n)
	}
	return name, nil
}

// holdsObservation reports an observation of the project that is not removed.
func (s draftScope) holdsObservation(id string) bool {
	if s.loaded == nil {
		return false
	}
	index := s.loaded.document.Find(id)
	return index >= 0 && s.loaded.document.Items[index].Kind == string(ObservationItem) && !s.loaded.removed(s.loaded.document.Items[index])
}

// environmentNames are the target names the project's environments record,
// except an environment the submission intent itself published, so a
// repeated click decides the same name again.
func (c *loadedCatalog) environmentNames(intent string) map[string]bool {
	names := map[string]bool{}
	for _, item := range c.document.Items {
		if item.Kind != string(EnvironmentItem) || c.removed(item) {
			continue
		}
		if intent != "" && slices.ContainsFunc(item.Revisions, func(revision catalog.Revision) bool { return revision.Intent == intent }) {
			continue
		}
		if target, err := c.targetOf(item); err == nil {
			names[target.Name] = true
		}
	}
	return names
}

// targetOf reads the target an environment's current backing declares.
func (c *loadedCatalog) targetOf(item catalog.Item) (replay.Target, error) {
	paths, availability, reason := c.backing(item)
	if availability != ItemAvailable {
		return replay.Target{}, errors.New(reason)
	}
	return replay.ReadDeclaredTarget(paths["target"])
}

// encodeMember is a member's deterministic encoding.
func encodeMember(value any) ([]byte, error) {
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("the member cannot be encoded")
	}
	return append(data, '\n'), nil
}

// decodeLinks reads one links member exactly as written.
func decodeLinks(data []byte) (EnvironmentLinks, error) {
	var links EnvironmentLinks
	if len(data) > maxLinksBytes || json.Unmarshal(data, &links, json.RejectUnknownMembers(true)) != nil || links.Schema != EnvironmentLinksSchema {
		return EnvironmentLinks{}, errors.New("the environment's links cannot be read")
	}
	for _, name := range append([]string{links.ResetName}, links.ActionNames...) {
		if name != "" && !catalog.ValidName(name) {
			return EnvironmentLinks{}, errors.New("the environment's links name something with a name that is not printable text")
		}
	}
	if links.Observation != "" && !catalog.ValidID(links.Observation) {
		return EnvironmentLinks{}, errors.New("the environment's links name an observation by an identity that is not one")
	}
	return links, nil
}

func readLinks(path string) (EnvironmentLinks, error) {
	data, err := boundedFile(path, maxLinksBytes)
	if err != nil {
		return EnvironmentLinks{}, err
	}
	return decodeLinks(data)
}

// verifyEnvironment reads a staged environment revision through the readers
// the command line reads each of its documents with, together.
func verifyEnvironment(files map[string]string) error {
	target, err := operation.ReadTarget(files["target"])
	if err != nil {
		return err
	}
	if path, held := files["policy"]; held {
		if _, err := operation.ReadSendPolicy(path); err != nil {
			return err
		}
	}
	if path, held := files["reset"]; held {
		plan, err := operation.ReadResetPlan(path)
		if err != nil {
			return err
		}
		if plan.Environment != target.Name {
			return errors.New("a reset plan resets the environment it is saved with")
		}
	}
	if path, held := files["links"]; held {
		if _, err := readLinks(path); err != nil {
			return err
		}
	}
	return nil
}

// environmentMembers is what an environment's current backing declares.
type environmentMembers struct {
	target replay.Target
	policy *sendpolicy.Policy
	reset  *fixturereset.Plan
	links  EnvironmentLinks
	paths  map[string]string
}

// environmentOf reads every member an available environment declares.
func (c *loadedCatalog) environmentOf(item catalog.Item) (*environmentMembers, error) {
	paths, availability, reason := c.backing(item)
	if availability != ItemAvailable {
		return nil, errors.New(reason)
	}
	target, err := replay.ReadDeclaredTarget(paths["target"])
	if err != nil {
		return nil, err
	}
	members := &environmentMembers{target: target, paths: paths}
	if path, held := paths["policy"]; held {
		policy, err := operation.ReadSendPolicy(path)
		if err != nil {
			return nil, err
		}
		members.policy = &policy
	}
	if path, held := paths["reset"]; held {
		plan, err := operation.ReadResetPlan(path)
		if err != nil {
			return nil, err
		}
		members.reset = &plan
	}
	if path, held := paths["links"]; held {
		if members.links, err = readLinks(path); err != nil {
			return nil, err
		}
	}
	return members, nil
}

// ItemDraftResult is one editor's starting draft: the saved members of an
// object at its current revision, or, for a new object, the validated
// defaults a new one starts from. New says which.
type ItemDraftResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Ref     *ItemRef       `json:"ref,omitzero"`
	Draft   *ItemDraft     `json:"draft,omitzero"`
	New     bool           `json:"new"`
}

func (r *ItemDraftResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// OpenItemDraft answers the draft an editor starts from. A reference with no
// identity is a new environment or observation: a new environment starts
// unclassified with its transport unchosen, so the person chooses its
// security mode, and a new observation from the default source and window.
// An existing object answers the members its current revision declares,
// exactly as saved, and a copy is this draft saved under no identity. It is
// a read: nothing is connected, collected or written.
func (a *App) OpenItemDraft(request ItemRequest) ItemDraftResult {
	return run(a, false, false, func(ctx context.Context) ItemDraftResult {
		result := ItemDraftResult{Context: request.Context}
		if request.Ref.Kind != EnvironmentItem && request.Ref.Kind != ObservationItem {
			result.refuse(Failed, "this release opens environment and observation drafts")
			return result
		}
		if request.Ref.ID == "" {
			if root, declined := a.projectRoot(ctx, request.Context); root == "" {
				result.refuse(declined.state, declined.reason)
				return result
			}
			draft := ItemDraft{}
			if request.Ref.Kind == EnvironmentItem {
				target := operation.DefaultTarget()
				target.Classification, target.Transport = replay.Unclassified, ""
				draft.Environment = &target
			} else {
				draft.Observation = &ObservationDraft{Source: operation.DefaultObservationSource(), Window: operation.DefaultObservationWindow()}
			}
			result.State, result.Draft, result.New = Completed, &draft, true
			result.Ref = &ItemRef{Kind: request.Ref.Kind}
			return result
		}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Ref, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		if item.Availability != ItemAvailable {
			result.refuse(Failed, "this object is "+string(item.Availability)+": "+item.Reason)
			return result
		}
		record := loaded.document.Items[loaded.document.Find(item.Ref.ID)]
		draft := ItemDraft{Name: item.Name}
		if request.Ref.Kind == EnvironmentItem {
			members, err := loaded.environmentOf(record)
			if err != nil {
				result.refuse(Failed, err.Error())
				return result
			}
			draft.Environment, draft.SendPolicy, draft.ResetPlan = &members.target, members.policy, members.reset
			if _, held := members.paths["links"]; held {
				links := members.links
				draft.Links = &links
			}
		} else {
			observation, err := loaded.observationOf(record)
			if err != nil {
				result.refuse(Failed, err.Error())
				return result
			}
			draft.Observation = observation
		}
		ref := item.Ref
		result.State, result.Ref, result.Draft = Completed, &ref, &draft
		return result
	})
}

// observationOf reads the source and window an available observation
// declares. A source discovered on its own has no window yet; its draft
// starts from the default window over the source it declares.
func (c *loadedCatalog) observationOf(item catalog.Item) (*ObservationDraft, error) {
	paths, availability, reason := c.backing(item)
	if availability != ItemAvailable {
		return nil, errors.New(reason)
	}
	data, err := boundedFile(paths["source"], catalog.MaxMemberBytes)
	if err != nil {
		return nil, err
	}
	source, err := observesource.DecodeSource(data)
	if err != nil {
		return nil, err
	}
	window := operation.DefaultObservationWindow()
	window.Source = source.Observes
	if path, held := paths["window"]; held {
		data, err := boundedFile(path, catalog.MaxMemberBytes)
		if err != nil {
			return nil, err
		}
		if window, err = observewindow.DecodeWindow(data); err != nil {
			return nil, err
		}
	}
	return &ObservationDraft{Source: source, Window: window}, nil
}
