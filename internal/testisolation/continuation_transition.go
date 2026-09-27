package testisolation

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/networkaction"
)

const TransitionPolicySchema = "readmit-isolation-transition-policy/v1"
const TransitionSchema = "readmit-isolation-transitions/v1"
const TransitionEffectsSchema = "readmit-isolation-transition-effects/v1"

type Transition struct {
	Phase      string            `json:"phase"`
	Alias      string            `json:"alias"`
	Attributes map[string]string `json:"attributes"`
}
type TransitionPolicy struct {
	Schema     string       `json:"schema"`
	ParentPlan string       `json:"parent_plan"`
	Phases     []Transition `json:"phases"`
}
type TransitionPrepared struct {
	p        *Prepared
	policy   TransitionPolicy
	identity string
}
type transitionRead struct {
	Path  string `json:"path"`
	Phase string `json:"phase,omitzero"`
	Proof string `json:"proof,omitzero"`
}
type transitionDocument struct {
	Schema   string           `json:"schema"`
	Previous string           `json:"previous"`
	Policy   TransitionPolicy `json:"policy"`
	Reads    []transitionRead `json:"reads"`
	Result   Result           `json:"result"`
}

var transitionFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "isolation transitions", Nested: []string{"previous", "reads", "effects"}, RequiredFiles: []string{"result.json", "identity.sha256"}, AllowFile: func(n string) bool { return n == "result.json" || n == "identity.sha256" }, MaxFiles: 16000, MaxFileBytes: 16 << 20, MaxBytes: 512 << 20}, Seal: artifactdir.DirectoryHash(TransitionSchema)}

func PrepareTransitions(p *Prepared, policy TransitionPolicy) (*TransitionPrepared, error) {
	if p == nil || policy.Schema != TransitionPolicySchema || policy.ParentPlan != p.document.Options.ParentPlan || len(policy.Phases) < 1 || len(policy.Phases) > 128 {
		return nil, refused
	}
	seen := map[string]bool{}
	last := ""
	closed := map[string]bool{}
	for _, change := range policy.Phases {
		if !token.MatchString(change.Phase) || !token.MatchString(change.Alias) || len(change.Attributes) > 32 {
			return nil, refused
		}
		if last != "" && last != change.Phase {
			closed[last] = true
		}
		if closed[change.Phase] || seen[change.Phase+"/"+change.Alias] {
			return nil, refused
		}
		seen[change.Phase+"/"+change.Alias] = true
		last = change.Phase
		index := slices.IndexFunc(p.document.Resources, func(r Resource) bool { return r.Alias == change.Alias })
		if index < 0 || p.document.Contract.Resources[index].Ownership == "select" {
			return nil, refused
		}
		template := p.document.Resources[index].Template
		allowed := []string{}
		for _, v := range p.registration.Templates {
			if v.ID == template {
				allowed = v.Attributes
			}
		}
		for key, value := range change.Attributes {
			if !slices.Contains(allowed, key) || len(value) > 1024 {
				return nil, refused
			}
		}
	}
	next := *p
	identity := networkaction.Digest(canonical(policy))
	next.recovery = identity
	return &TransitionPrepared{p: &next, policy: clone(policy), identity: identity}, nil
}
func (p *TransitionPrepared) Review(phase string) (Review, error) {
	if p == nil || phase != "read" && phase != "cleanup" {
		return Review{}, refused
	}
	review := p.p.Review(phase)
	review.Identity = review.Binding.Configuration
	for i := range review.Effects {
		if phase == "cleanup" {
			review.Effects[i].Operation = "delete-owned-version-before-transitions"
		}
	}
	for _, change := range p.policy.Phases {
		resource := p.p.document.Resources[slices.IndexFunc(p.p.document.Resources, func(r Resource) bool { return r.Alias == change.Alias })]
		operation := "verify-successor-after-" + change.Phase
		if phase == "cleanup" {
			operation = "delete-owned-version-after-" + change.Phase
		}
		review.Effects = append(review.Effects, Effect{Alias: resource.Alias, Kind: resource.Kind, ID: resource.ID, Operation: operation, Expected: clone(change.Attributes)})
	}
	return review, nil
}

type TransitionSession struct {
	prepared                        *TransitionPrepared
	authorities                     Authorities
	actors                          map[string]networkaction.Actor
	writer                          *artifactdir.Writer
	doc                             transitionDocument
	current                         Result
	next                            int
	ready, closed, cleanupAttempted bool
}
type transitionAuthority struct {
	s       *TransitionSession
	phase   string
	binding networkaction.Binding
}

func (a transitionAuthority) Check(ctx context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	if b != a.binding {
		return networkaction.Actor{}, refused
	}
	return a.s.actor(ctx, a.phase)
}
func (s *TransitionSession) actor(ctx context.Context, phase string) (networkaction.Actor, error) {
	a, err := s.prepared.p.Check(ctx, s.authorities, phase)
	if err != nil || a != s.actors[phase] {
		return networkaction.Actor{}, refused
	}
	return a, nil
}
func (s *TransitionSession) authoritiesForEffects() Authorities {
	p := s.prepared.p
	return Authorities{Read: transitionAuthority{s, "read", p.Review("read").Binding}, Cleanup: transitionAuthority{s, "cleanup", p.Review("cleanup").Binding}}
}

// StartTransitions transfers an already verified live setup into an explicitly
// approved successor policy. It never repeats provisioning or acquires a lease.
func StartTransitions(ctx context.Context, live *Session, p *TransitionPrepared, a Authorities, output string) (*TransitionSession, error) {
	if live == nil || !live.Ready() || p == nil || live.p.identity != p.p.identity {
		return nil, refused
	}
	prior, identity, files, err := readyContinuationSnapshot(live.p, live.writer.Path())
	if err != nil {
		return nil, err
	}
	actors := map[string]networkaction.Actor{}
	for _, phase := range []string{"read", "cleanup"} {
		actor, e := p.p.Check(ctx, a, phase)
		if e != nil {
			return nil, e
		}
		actors[phase] = actor
	}
	w, err := artifactdir.Create(output, transitionFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	s := &TransitionSession{prepared: p, authorities: a, actors: actors, writer: w, current: clone(prior), ready: true, doc: transitionDocument{Schema: TransitionSchema, Previous: identity, Policy: clone(p.policy), Reads: []transitionRead{}, Result: clone(prior)}}
	s.doc.Result.Schema = TransitionSchema
	s.doc.Result.Setup = "transition-ready"
	s.doc.Result.Entries = []Entry{}
	for name, raw := range files {
		if err = w.WriteFile("previous/"+name, raw); err != nil {
			w.Close()
			return nil, err
		}
	}
	if err = w.Mkdir("reads"); err != nil {
		w.Close()
		return nil, err
	}
	if err = s.Check(ctx); err != nil {
		s.Close()
		return s, err
	}
	live.Close()
	return s, nil
}
func (s *TransitionSession) Ready() bool    { return s != nil && s.ready && !s.closed }
func (s *TransitionSession) Result() Result { return clone(s.doc.Result) }
func (s *TransitionSession) read(ctx context.Context) (Snapshot, string, error) {
	if !s.Ready() || len(s.doc.Reads) >= 256 {
		return Snapshot{}, "", refused
	}
	p, err := s.prepared.p.httpPlan("read", "state", nil)
	if err != nil {
		return Snapshot{}, "", err
	}
	name := fmt.Sprintf("reads/r%04d", len(s.doc.Reads)+1)
	response, receipt, err := p.Execute(ctx, transitionAuthority{s, "read", p.Binding()}, filepath.Join(s.writer.Path(), name), nil)
	var snapshot Snapshot
	if err != nil || receipt.State != "responded" || receipt.HTTPStatus != 200 || !receipt.ResponseRetained || decode(response.Body.Expose(), &snapshot) != nil || s.prepared.p.validateSnapshot(snapshot) != nil {
		return Snapshot{}, name, refused
	}
	return snapshot, name, nil
}
func (s *TransitionSession) Check(ctx context.Context) error {
	if s == nil {
		return refused
	}
	current, name, err := s.read(ctx)
	if err != nil || currentPrerequisites(s.prepared.p, s.current, current) != nil {
		s.ready = false
		s.doc.Result.Setup = "transition-invalidated"
		return refused
	}
	s.doc.Reads = append(s.doc.Reads, transitionRead{Path: name})
	return nil
}

// Accept observes the next authored phase successor. The proof names the exact
// passed phase result; the owning flow verifier also binds that result to this
// phase. A digest alone never grants authority to change undeclared attributes.
func (s *TransitionSession) Accept(ctx context.Context, phase, phaseProofIdentity string) error {
	if !s.Ready() || !networkaction.ValidDigest(phaseProofIdentity) || s.next >= len(s.prepared.policy.Phases) || s.prepared.policy.Phases[s.next].Phase != phase {
		return refused
	}
	actual, name, err := s.read(ctx)
	if err != nil {
		s.ready = false
		s.doc.Result.Setup = "transition-invalidated"
		return err
	}
	next, position, err := successor(s.prepared, s.current, actual, s.next, phase)
	if err != nil {
		s.ready = false
		s.doc.Result.Setup = "transition-invalidated"
		return err
	}
	s.current = next
	s.next = position
	s.doc.Result.Resources = clone(next.Resources)
	s.doc.Reads = append(s.doc.Reads, transitionRead{Path: name, Phase: phase, Proof: phaseProofIdentity})
	return nil
}
func successor(p *TransitionPrepared, before Result, actual Snapshot, position int, phase string) (Result, int, error) {
	if p.p.validateSnapshot(actual) != nil || actual.Lease != before.Lease {
		return Result{}, position, refused
	}
	expected := clone(before)
	changed := map[string]bool{}
	for position < len(p.policy.Phases) && p.policy.Phases[position].Phase == phase {
		change := p.policy.Phases[position]
		index := slices.IndexFunc(expected.Resources, func(r Resource) bool { return r.Alias == change.Alias })
		if index < 0 {
			return Result{}, position, refused
		}
		want := &expected.Resources[index]
		old := before.Resources[index]
		found := find(actual.Resources, want.Kind, want.ID)
		if found == nil || found.Owner != p.p.document.Scope.Owner || found.Alias != want.Alias {
			return Result{}, position, refused
		}
		want.Attributes = clone(change.Attributes)
		want.Version = found.Version
		if !reflect.DeepEqual(*want, *found) || !token.MatchString(found.Version) || !reflect.DeepEqual(old.Attributes, found.Attributes) && old.Version == found.Version {
			return Result{}, position, refused
		}
		changed[want.Alias] = true
		position++
	}
	if len(changed) == 0 || currentPrerequisites(p.p, expected, actual) != nil {
		return Result{}, position, refused
	}
	return expected, position, nil
}
func (s *TransitionSession) Cleanup(ctx context.Context) error {
	if s == nil || s.closed || s.cleanupAttempted {
		return refused
	}
	if err := s.Check(ctx); err != nil {
		return err
	}
	s.cleanupAttempted = true
	s.ready = false
	effectFamily := family
	effectFamily.Seal = artifactdir.DirectoryHash(TransitionEffectsSchema)
	w, err := artifactdir.Create(filepath.Join(s.writer.Path(), "effects"), effectFamily, artifactdir.Durable)
	if err != nil {
		return err
	}
	effect := &Session{p: s.prepared.p, authorities: s.authoritiesForEffects(), writer: w, actors: map[string]networkaction.Actor{}, result: clone(s.current)}
	effect.result.Schema = TransitionEffectsSchema
	effect.result.Setup = "transition-ready"
	effect.result.Cleanup = "failed"
	effect.result.Entries = []Entry{}
	effect.result.Complete = false
	if w.WriteFile("plan.json", canonical(s.prepared.p.document)) != nil || w.Mkdir("actions") != nil || w.Sync() != nil {
		w.Close()
		return refused
	}
	defer func() {
		effect.Close()
		s.doc.Result.Cleanup = effect.result.Cleanup
		s.doc.Result.Entries = clone(effect.result.Entries)
	}()
	for _, phase := range []string{"read", "cleanup"} {
		if err = effect.pinActor(ctx, phase); err != nil {
			return err
		}
	}
	err = effect.Cleanup(ctx)
	s.doc.Result.Cleanup = effect.result.Cleanup
	s.doc.Result.Entries = clone(effect.result.Entries)
	if err == nil {
		s.doc.Result.Lease = Lease{}
		s.Close()
	}
	return err
}
func (s *TransitionSession) Close() {
	if s == nil || s.closed {
		return
	}
	s.ready = false
	s.closed = true
	s.doc.Result.Complete = true
	if s.writer.WriteFile("result.json", canonical(s.doc)) == nil {
		_, _ = s.writer.Seal(nil)
	}
	s.writer.Close()
}
