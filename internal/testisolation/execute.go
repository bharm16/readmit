package testisolation

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/networkaction"
)

var family = artifactdir.Family{Layout: artifactdir.Layout{Noun: "test isolation", Nested: []string{"actions", "recovery"}, RequiredFiles: []string{"plan.json"}, AllowFile: func(name string) bool {
	return name == "plan.json" || name == "interrupted.json" || name == "manual.json" || name == "result.json" || name == "preflight.json" || name == "setup.json" || name == "identity.sha256" || strings.HasPrefix(name, "entry-") && strings.HasSuffix(name, ".json")
}, MaxFiles: 4096, MaxFileBytes: 16 << 20, MaxBytes: 256 << 20}, Seal: artifactdir.DirectoryHash(ResultSchema)}

type Session struct {
	p           *Prepared
	authorities Authorities
	writer      *artifactdir.Writer
	result      Result
	sequence    int
	closed      bool
	uncertain   bool
	ready       bool
	actors      map[string]networkaction.Actor
}
type scopedAuthority struct {
	session *Session
	phase   string
	exact   networkaction.Binding
}

func (a scopedAuthority) Check(ctx context.Context, binding networkaction.Binding) (networkaction.Actor, error) {
	if binding != a.exact {
		return networkaction.Actor{}, refused
	}
	actor, err := a.session.p.Check(ctx, a.session.authorities, a.phase)
	if err != nil {
		return networkaction.Actor{}, err
	}
	if previous, ok := a.session.actors[a.phase]; ok && previous != actor {
		return networkaction.Actor{}, refused
	}
	return actor, nil
}
func (p *Prepared) httpPlan(phase, action string, request *Request) (*networkaction.HTTPPlan, error) {
	role := p.role(phase)
	spec := networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: p.document.Options.ParentPlan, Source: p.identity, Project: p.document.Contract.Project, Environment: p.document.Contract.Environment, Revision: p.document.Contract.Revision, Endpoint: role.Endpoint, Classification: p.registration.Classification, Operation: role.Reference.Purpose, URL: p.registration.URL + "/fixture/v1/" + action, ServerName: p.registration.ServerName, Authorities: p.registration.Authorities, Credential: &role.Reference, TimeoutMS: p.registration.TimeoutMS, MaxBytes: 128 << 10}
	if phase == "read" {
		spec.Method = "GET"
		spec.URL += "?scope=" + url.QueryEscape(string(canonical(p.document.Scope)))
	} else {
		spec.Method = "POST"
		spec.ContentType = "application/json"
		if request != nil {
			spec.Body = canonical(request)
		}
	}
	return networkaction.PrepareHTTP(canonical(spec), p.document.Policy)
}
func newSession(p *Prepared, a Authorities, output string) (*Session, error) {
	w, err := artifactdir.Create(output, family, artifactdir.Durable)
	if err != nil {
		return nil, refused
	}
	s := &Session{p: p, authorities: a, writer: w, actors: map[string]networkaction.Actor{}, result: Result{Schema: ResultSchema, Plan: p.identity, Scope: p.document.Scope, Setup: "not-started", Cleanup: "not-started", Entries: []Entry{}, Resources: []Resource{}, Manual: []ManualClaim{}}}
	if w.WriteFile("plan.json", canonical(p.document)) != nil || w.Mkdir("actions") != nil || w.Sync() != nil {
		w.Close()
		return nil, refused
	}
	return s, nil
}
func (s *Session) pinActor(ctx context.Context, phase string) error {
	a, err := s.p.Check(ctx, s.authorities, phase)
	if err != nil {
		return err
	}
	s.actors[phase] = a
	return nil
}
func (s *Session) call(ctx context.Context, phase, action string, request *Request, destination any) (string, error) {
	if s.closed {
		return "", refused
	}
	p, err := s.p.httpPlan(phase, action, request)
	if err != nil {
		return "", err
	}
	s.sequence++
	name := fmt.Sprintf("actions/n%04d", s.sequence)
	response, receipt, err := p.Execute(ctx, scopedAuthority{session: s, phase: phase, exact: p.Binding()}, filepath.Join(s.writer.Path(), name), nil)
	if err != nil || receipt.State != "responded" || !receipt.ResponseRetained || response.Status != 200 {
		return name, refused
	}
	if decode(response.Body.Expose(), destination) != nil {
		return name, refused
	}
	return name, nil
}
func (p *Prepared) Preflight(ctx context.Context, a Authorities, output string) (Capabilities, error) {
	if _, err := p.Check(ctx, a, "read"); err != nil {
		return Capabilities{}, err
	}
	s, err := newSession(p, a, output)
	if err != nil {
		return Capabilities{}, err
	}
	defer s.Close()
	if s.pinActor(ctx, "read") != nil {
		return Capabilities{}, refused
	}
	var capabilities Capabilities
	_, err = s.call(ctx, "read", "capabilities", nil, &capabilities)
	if err != nil || p.validateCapabilities(capabilities) != nil {
		return Capabilities{}, refused
	}
	if s.writer.WriteFile("preflight.json", canonical(capabilities)) != nil {
		return Capabilities{}, refused
	}
	s.result.Setup = "preflight-verified"
	s.result.Cleanup = "not-applicable"
	if s.seal() != nil {
		return Capabilities{}, refused
	}
	return capabilities, nil
}
func (p *Prepared) validateCapabilities(c Capabilities) error {
	if c.Schema != ProtocolSchema || c.Protocol != "typed-fixture-v1" || c.Scope != p.document.Scope || c.LeaseMode != "exclusive-no-expiry" || !c.VersionGuards || len(c.Templates) > 32 {
		return refused
	}
	for _, resource := range p.document.Resources {
		found := false
		for _, t := range c.Templates {
			if t.ID == resource.Template && t.Kind == resource.Kind {
				found = true
				for attribute := range resource.Attributes {
					if !slices.Contains(t.Attributes, attribute) {
						return refused
					}
				}
			}
		}
		if !found {
			return refused
		}
	}
	return nil
}

// Start requires a completed explicit preflight and fresh process-local manual
// confirmation. A returned session holds the target lease through test work.
// Cleanup is a separate explicit call; cancellation never deletes resources.
func Start(ctx context.Context, p *Prepared, a Authorities, preflight, output string, confirmation Confirmation) (*Session, error) {
	if p == nil {
		return nil, refused
	}
	receipt, err := Open(preflight)
	if err != nil || receipt.Plan != p.identity || receipt.Setup != "preflight-verified" || !receipt.Complete {
		return nil, refused
	}
	if len(p.document.Contract.Manual) > 0 {
		if confirmation.Plan != p.identity || confirmation.Instance != p.document.Options.Instance || len(confirmation.Steps) != len(p.document.Contract.Manual) {
			return nil, refused
		}
		seen := map[string]bool{}
		for _, id := range confirmation.Steps {
			if seen[id] {
				return nil, refused
			}
			seen[id] = true
		}
		for _, m := range p.document.Contract.Manual {
			if !seen[m.ID] {
				return nil, refused
			}
		}
	}
	for _, phase := range []string{"read", "setup", "cleanup"} {
		if _, err := p.Check(ctx, a, phase); err != nil {
			return nil, err
		}
	}
	s, err := newSession(p, a, output)
	if err != nil {
		return nil, err
	}
	for _, phase := range []string{"read", "setup", "cleanup"} {
		if s.pinActor(ctx, phase) != nil {
			s.Close()
			return s, refused
		}
	}
	for _, m := range p.document.Contract.Manual {
		s.result.Manual = append(s.result.Manual, ManualClaim{ID: m.ID, Provenance: "operator-declared-current-execution"})
	}
	if s.writer.WriteFile("manual.json", canonical(s.result.Manual)) != nil || s.writer.Sync() != nil {
		s.Close()
		return s, refused
	}
	s.result.Setup = "failed"
	if err := s.mutate(ctx, "setup", "lease-acquire", nil, nil); err != nil {
		return s, err
	}
	before, err := s.snapshot(ctx)
	if err != nil {
		return s, err
	}
	if err = p.verifyInitial(before); err != nil {
		return s, err
	}
	for i, resource := range p.document.Resources {
		requirement := p.document.Contract.Resources[i]
		if requirement.Ownership == "select" {
			s.result.Resources = append(s.result.Resources, *find(before.Resources, resource.Kind, resource.ID))
			continue
		}
		if err := s.mutate(ctx, "setup", requirement.Ownership, &resource, &before); err != nil {
			return s, err
		}
		after, err := s.snapshot(ctx)
		if err != nil {
			s.uncertain = true
			s.result.Setup = "uncertain"
			return s, err
		}
		actual := find(after.Resources, resource.Kind, resource.ID)
		if actual == nil || !matches(*actual, resource) || actual.Owner != p.document.Scope.Owner || actual.Version == "" || !unchangedOthers(before, after, resource.Kind, resource.ID) {
			s.uncertain = true
			s.result.Setup = "uncertain"
			return s, refused
		}
		s.result.Resources = append(s.result.Resources, *actual)
		if s.recordObserved(after) != nil {
			return s, refused
		}
		before = after
	}
	s.result.Setup = "ready"
	s.ready = true
	if s.writer.WriteFile("setup.json", canonical(s.result)) != nil || s.writer.Sync() != nil {
		s.ready = false
		s.uncertain = true
		return s, refused
	}
	return s, nil
}
func (s *Session) Ready() bool    { return s != nil && s.ready && !s.closed && !s.uncertain }
func (s *Session) Result() Result { return clone(s.result) }
func (s *Session) Close() {
	if s != nil && !s.closed {
		if !s.result.Complete {
			_ = s.writer.WriteFile("interrupted.json", canonical(s.result))
			_ = s.writer.Sync()
		}
		s.writer.Close()
		s.closed = true
		s.ready = false
	}
}
func (s *Session) snapshot(ctx context.Context) (Snapshot, error) {
	var snapshot Snapshot
	_, err := s.call(ctx, "read", "state", nil, &snapshot)
	if err != nil || s.p.validateSnapshot(snapshot) != nil || snapshot.Lease != s.result.Lease {
		return Snapshot{}, refused
	}
	return snapshot, nil
}
func (p *Prepared) validateSnapshot(s Snapshot) error {
	if s.Schema != ProtocolSchema || s.Scope != p.document.Scope || len(s.Resources) > 64 {
		return refused
	}
	seen := map[string]bool{}
	for _, r := range s.Resources {
		key := r.Kind + "/" + r.ID
		if !kind(r.Kind) || !token.MatchString(r.ID) || !token.MatchString(r.Version) || r.Owner != "" && !networkaction.ValidDigest(r.Owner) || seen[key] || len(r.Attributes) > 32 || len(r.Identifiers) > 16 || len(r.References) > 32 {
			return refused
		}
		seen[key] = true
		for k, v := range r.Attributes {
			if !token.MatchString(k) || len(v) > 1024 {
				return refused
			}
		}
	}
	return nil
}

// SnapshotIdentity is used only for an explicitly recorded baseline. It binds
// every resource version and property; a name or count cannot stand in for it.
func SnapshotIdentity(s Snapshot) string {
	resources := clone(s.Resources)
	slices.SortFunc(resources, func(a, b Resource) int { return strings.Compare(a.Kind+"/"+a.ID, b.Kind+"/"+b.ID) })
	scope := s.Scope
	scope.Owner = ""
	scope.LeaseKey = ""
	return networkaction.Digest(canonical(struct {
		Scope     Scope      `json:"scope"`
		Resources []Resource `json:"resources"`
	}{scope, resources}))
}
func (p *Prepared) verifyInitial(s Snapshot) error {
	if p.document.Contract.Mode == "recorded-baseline" && SnapshotIdentity(s) != p.document.Contract.Baseline {
		return refused
	}
	expected := 0
	for i, r := range p.document.Resources {
		actual := find(s.Resources, r.Kind, r.ID)
		requirement := p.document.Contract.Resources[i]
		if requirement.Ownership == "create" {
			if actual != nil {
				return refused
			}
			continue
		}
		expected++
		if actual == nil || !matches(*actual, r) || actual.Version != r.Version || actual.Owner != "" {
			return refused
		}
	}
	if p.document.Contract.Mode != "recorded-baseline" && len(s.Resources) != expected {
		return refused
	}
	return nil
}
func find(resources []Resource, kind, id string) *Resource {
	for i := range resources {
		if resources[i].Kind == kind && resources[i].ID == id {
			return &resources[i]
		}
	}
	return nil
}
func matches(actual, want Resource) bool {
	return actual.Kind == want.Kind && actual.ID == want.ID && actual.Template == want.Template && reflect.DeepEqual(actual.Attributes, want.Attributes) && reflect.DeepEqual(actual.Identifiers, want.Identifiers) && reflect.DeepEqual(actual.References, want.References)
}
func unchangedOthers(before, after Snapshot, kind, id string) bool {
	filter := func(s Snapshot) []Resource {
		resources := []Resource{}
		for _, r := range s.Resources {
			if r.Kind != kind || r.ID != id {
				resources = append(resources, r)
			}
		}
		slices.SortFunc(resources, func(a, b Resource) int { return strings.Compare(a.Kind+"/"+a.ID, b.Kind+"/"+b.ID) })
		return resources
	}
	return bytes.Equal(canonical(filter(before)), canonical(filter(after)))
}
func (s *Session) mutate(ctx context.Context, phase, action string, resource *Resource, before *Snapshot) error {
	if s.uncertain || s.closed {
		return refused
	}
	if resource != nil {
		copied := clone(*resource)
		resource = &copied
	}
	if before != nil {
		copied := clone(*before)
		before = &copied
	}
	req := Request{Schema: ProtocolSchema, Scope: s.p.document.Scope, Lease: s.result.Lease, Action: action, Resource: resource}
	n := len(s.result.Entries) + 1
	entry := Entry{Sequence: n, Action: action, State: "uncertain", Request: req, Before: before}
	if resource != nil {
		entry.Alias = resource.Alias
	}
	if s.writer.WriteFile(fmt.Sprintf("entry-%04d-intent.json", n), canonical(entry)) != nil || s.writer.Sync() != nil {
		return refused
	}
	s.result.Entries = append(s.result.Entries, entry)
	var reply Reply
	path, err := s.call(ctx, phase, action, &req, &reply)
	entry.Network = path
	if err != nil || reply.Schema != ProtocolSchema || reply.Scope != s.p.document.Scope || reply.Outcome != "applied" {
		s.uncertain = true
		if phase == "cleanup" {
			s.result.Cleanup = "uncertain"
		} else {
			s.result.Setup = "uncertain"
		}
		s.result.Entries[n-1] = entry
		return refused
	}
	if action == "lease-acquire" {
		if reply.Lease.Key != s.p.document.Scope.LeaseKey || reply.Lease.Owner != s.p.document.Scope.Owner || !token.MatchString(reply.Lease.Version) {
			s.uncertain = true
			return refused
		}
		s.result.Lease = reply.Lease
	} else if action != "lease-release" && reply.Lease != s.result.Lease {
		s.uncertain = true
		return refused
	}
	if action == "lease-release" {
		if reply.Lease != (Lease{}) {
			s.uncertain = true
			return refused
		}
		s.result.Lease = Lease{}
	}
	entry.State = "acknowledged"
	s.result.Entries[n-1] = entry
	if s.writer.WriteFile(fmt.Sprintf("entry-%04d-reply.json", n), canonical(entry)) != nil || s.writer.Sync() != nil {
		s.uncertain = true
		return refused
	}
	return nil
}
func (s *Session) recordObserved(after Snapshot) error {
	i := len(s.result.Entries) - 1
	s.result.Entries[i].After = &after
	s.result.Entries[i].State = "verified"
	if s.writer.WriteFile(fmt.Sprintf("entry-%04d-observed.json", i+1), canonical(s.result.Entries[i])) != nil || s.writer.Sync() != nil {
		s.uncertain = true
		return refused
	}
	return nil
}

// Cleanup only deletes exact owned resources, in reverse prerequisite order,
// using the version observed after setup. Changed resources are never deleted.
func (s *Session) Cleanup(ctx context.Context) error {
	if s == nil || s.closed || s.uncertain {
		return refused
	}
	s.ready = false
	s.result.Cleanup = "failed"
	before, err := s.snapshot(ctx)
	if err != nil {
		return err
	}
	for i := len(s.result.Resources) - 1; i >= 0; i-- {
		resource := s.result.Resources[i]
		if resource.Owner != s.p.document.Scope.Owner {
			continue
		}
		actual := find(before.Resources, resource.Kind, resource.ID)
		if actual == nil || !reflect.DeepEqual(*actual, resource) {
			return refused
		}
		if err := s.mutate(ctx, "cleanup", "delete", &resource, &before); err != nil {
			return err
		}
		after, err := s.snapshot(ctx)
		if err != nil {
			s.uncertain = true
			s.result.Cleanup = "uncertain"
			return err
		}
		if find(after.Resources, resource.Kind, resource.ID) != nil || !unchangedOthers(before, after, resource.Kind, resource.ID) {
			s.uncertain = true
			s.result.Cleanup = "uncertain"
			return refused
		}
		if s.recordObserved(after) != nil {
			return refused
		}
		before = after
	}
	if !s.p.noOwnedResources(before) {
		return refused
	}
	if err := s.mutate(ctx, "cleanup", "lease-release", nil, &before); err != nil {
		return err
	}
	after, err := s.snapshot(ctx)
	if err != nil || !unchangedOthers(before, after, "", "") || !s.p.noOwnedResources(after) {
		s.uncertain = true
		s.result.Cleanup = "uncertain"
		return refused
	}
	if s.recordObserved(after) != nil {
		s.result.Cleanup = "uncertain"
		return refused
	}
	s.result.Cleanup = "complete"
	return s.seal()
}
func (s *Session) seal() error {
	s.result.Complete = true
	if s.writer.WriteFile("result.json", canonical(s.result)) != nil {
		s.result.Complete = false
		return refused
	}
	if _, err := s.writer.Seal(nil); err != nil {
		s.result.Complete = false
		return refused
	}
	s.Close()
	return nil
}
