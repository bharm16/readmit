package testisolation

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/networkaction"
)

const ContinuationSchema = "readmit-isolation-continuation/v1"

var continuationFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "isolation continuation", Nested: []string{"previous", "checks", "cleanup"}, RequiredFiles: []string{"result.json", "identity.sha256"}, AllowFile: func(name string) bool { return name == "result.json" || name == "identity.sha256" }, MaxFiles: 12000, MaxFileBytes: 16 << 20, MaxBytes: 512 << 20}, Seal: artifactdir.DirectoryHash(ContinuationSchema)}

type continuationProof struct {
	Schema   string   `json:"schema"`
	Previous string   `json:"previous"`
	Checks   []string `json:"checks"`
	Result   Result   `json:"result"`
}

// Continuation is process-local authority over the unchanged prerequisites of
// one interrupted execution. It cannot acquire a lease or repeat setup effects.
type Continuation struct {
	p                *Prepared
	authorities      Authorities
	actors           map[string]networkaction.Actor
	writer           *artifactdir.Writer
	previous         Result
	proof            continuationProof
	ready, closed    bool
	cleanupAttempted bool
}

func currentPrerequisites(p *Prepared, want Result, actual Snapshot) error {
	if p.validateSnapshot(actual) != nil || actual.Lease != want.Lease {
		return refused
	}
	for _, resource := range want.Resources {
		found := find(actual.Resources, resource.Kind, resource.ID)
		if found == nil || !reflect.DeepEqual(*found, resource) {
			return refused
		}
	}
	for _, resource := range actual.Resources {
		if resource.Owner == p.document.Scope.Owner && find(want.Resources, resource.Kind, resource.ID) == nil {
			return refused
		}
	}
	return nil
}

// CheckReady repeats a real scoped read immediately before another live phase.
// Fresh observations establish current prerequisites; the old setup receipt
// alone never implies that the target remained unchanged.
func (s *Session) CheckReady(ctx context.Context) error {
	if !s.Ready() {
		return refused
	}
	actual, err := s.snapshot(ctx)
	if err != nil || currentPrerequisites(s.p, s.result, actual) != nil {
		s.ready = false
		return refused
	}
	return nil
}

func Continue(ctx context.Context, p *Prepared, a Authorities, previous, output string, confirmation Confirmation) (*Continuation, error) {
	prior, identity, files, err := readyContinuationSnapshot(p, previous)
	if err != nil {
		return nil, err
	}
	bound := *p
	bound.recovery = identity
	review := bound.Review("read")
	if len(p.document.Contract.Manual) > 0 {
		if confirmation.Plan != review.Binding.Configuration || confirmation.Instance != p.document.Options.Instance || len(confirmation.Steps) != len(p.document.Contract.Manual) {
			return nil, refused
		}
		seen := map[string]bool{}
		for _, id := range confirmation.Steps {
			if seen[id] {
				return nil, refused
			}
			seen[id] = true
		}
		for _, step := range p.document.Contract.Manual {
			if !seen[step.ID] {
				return nil, refused
			}
		}
	}
	actors := map[string]networkaction.Actor{}
	for _, phase := range []string{"read", "cleanup"} {
		actor, e := bound.Check(ctx, a, phase)
		if e != nil {
			return nil, e
		}
		actors[phase] = actor
	}
	w, err := artifactdir.Create(output, continuationFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	c := &Continuation{p: &bound, authorities: a, actors: actors, writer: w, previous: clone(prior), proof: continuationProof{Schema: ContinuationSchema, Previous: identity, Checks: []string{}, Result: clone(prior)}, ready: true}
	c.proof.Result.Schema = ContinuationSchema
	c.proof.Result.Setup = "continued-ready"
	c.proof.Result.Entries = []Entry{}
	c.proof.Result.Manual = []ManualClaim{}
	for _, step := range p.document.Contract.Manual {
		c.proof.Result.Manual = append(c.proof.Result.Manual, ManualClaim{ID: step.ID, Provenance: "operator-declared-current-continuation"})
	}
	for name, raw := range files {
		if err = w.WriteFile("previous/"+name, raw); err != nil {
			w.Close()
			return nil, err
		}
	}
	if err = w.Mkdir("checks"); err != nil {
		w.Close()
		return nil, err
	}
	if err = w.Sync(); err != nil {
		w.Close()
		return nil, err
	}
	if err = c.Check(ctx); err != nil {
		c.Close()
		return c, err
	}
	return c, nil
}
func (c *Continuation) Ready() bool    { return c != nil && c.ready && !c.closed }
func (c *Continuation) Result() Result { return clone(c.proof.Result) }
func (c *Continuation) actor(ctx context.Context, phase string) (networkaction.Actor, error) {
	actor, err := c.p.Check(ctx, c.authorities, phase)
	if err != nil || actor != c.actors[phase] {
		return networkaction.Actor{}, refused
	}
	return actor, nil
}

type continuationAuthority struct {
	continuation *Continuation
	phase        string
	exact        networkaction.Binding
}

func (a continuationAuthority) Check(ctx context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	if b != a.exact {
		return networkaction.Actor{}, refused
	}
	return a.continuation.actor(ctx, a.phase)
}
func (c *Continuation) scoped(p *Prepared) Authorities {
	return Authorities{Read: continuationAuthority{c, "read", p.Review("read").Binding}, Cleanup: continuationAuthority{c, "cleanup", p.Review("cleanup").Binding}}
}

// Check retains fresh read-only evidence and refuses any lease or resource
// version change, including selected unowned prerequisites.
func (c *Continuation) Check(ctx context.Context) (err error) {
	defer func() {
		if err != nil && c != nil && !c.closed {
			c.ready = false
			c.proof.Result.Setup = "continuation-invalidated"
		}
	}()
	if !c.Ready() || len(c.proof.Checks) >= 128 {
		return refused
	}
	if _, err := c.actor(ctx, "read"); err != nil {
		c.ready = false
		c.proof.Result.Setup = "continuation-invalidated"
		return err
	}
	name := fmt.Sprintf("checks/c%04d", len(c.proof.Checks)+1)
	s, err := newSession(c.p, c.scoped(c.p), filepath.Join(c.writer.Path(), name))
	if err != nil {
		return err
	}
	defer s.Close()
	s.result.Lease = c.previous.Lease
	if err = s.pinActor(ctx, "read"); err != nil {
		c.ready = false
		c.proof.Result.Setup = "continuation-invalidated"
		return err
	}
	actual, err := s.snapshot(ctx)
	if err != nil || currentPrerequisites(c.p, c.previous, actual) != nil {
		c.ready = false
		c.proof.Result.Setup = "continuation-invalidated"
		return refused
	}
	s.result.Setup = "reconciled-not-ready"
	s.result.Cleanup = "not-applicable"
	s.result.Resources = ownedResources(c.p, actual)
	if err = s.seal(); err != nil {
		c.ready = false
		c.proof.Result.Setup = "continuation-invalidated"
		return err
	}
	c.proof.Checks = append(c.proof.Checks, name)
	return nil
}

// Cleanup is one newly authorized guarded cleanup. A failed or uncertain write
// is never replayed by calling this object again.
func (c *Continuation) Cleanup(ctx context.Context) error {
	if c == nil || c.closed || c.cleanupAttempted {
		return refused
	}
	if err := c.Check(ctx); err != nil {
		return err
	}
	c.cleanupAttempted = true
	c.ready = false
	reconciled := filepath.Join(c.writer.Path(), c.proof.Checks[len(c.proof.Checks)-1])
	next, _, _, err := recoveryPreparation(c.p, reconciled)
	if err != nil {
		return err
	}
	result, err := CleanupReconciled(ctx, c.p, c.scoped(next), reconciled, filepath.Join(c.writer.Path(), "cleanup"))
	c.proof.Result.Cleanup = result.Cleanup
	c.proof.Result.Lease = result.Lease
	c.proof.Result.Entries = clone(result.Entries)
	if err != nil {
		if c.proof.Result.Cleanup == "" {
			c.proof.Result.Cleanup = "failed"
		}
		c.proof.Result.Lease = c.previous.Lease
	}
	if err == nil {
		c.Close()
	}
	return err
}
func (c *Continuation) Close() {
	if c == nil || c.closed {
		return
	}
	c.closed = true
	c.ready = false
	c.proof.Result.Complete = true
	if c.writer.WriteFile("result.json", canonical(c.proof)) == nil {
		_, _ = c.writer.Seal(nil)
	}
	c.writer.Close()
}

// OpenContinuation restores only verified facts, never a live continuation or
// authority. Nested old setup and new reads stay independent retained evidence.
func OpenContinuation(path string) (Result, error) {
	files, err := artifactdir.Read(path, continuationFamily.Layout)
	if err != nil {
		return Result{}, err
	}
	return VerifyContinuation(files)
}

// VerifyContinuation verifies an already captured bounded artifact without reopening paths.
func VerifyContinuation(files map[string][]byte) (Result, error) {
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(ContinuationSchema, files) {
		return Result{}, refused
	}
	var proof continuationProof
	if decodeResult(files["result.json"], &proof) != nil || proof.Schema != ContinuationSchema || proof.Result.Schema != ContinuationSchema || !proof.Result.Complete || len(proof.Checks) > 128 {
		return Result{}, refused
	}
	p, err := readPlan(nestedFiles(files, "previous"))
	if err != nil {
		return Result{}, err
	}
	prior, identity, _, err := verifyReadyContinuationSnapshot(p, nestedFiles(files, "previous"))
	if err != nil || identity != proof.Previous {
		return Result{}, refused
	}
	result := proof.Result
	if result.Plan != prior.Plan || result.Scope != prior.Scope || !reflect.DeepEqual(result.Resources, prior.Resources) || len(result.Manual) != len(p.document.Contract.Manual) || result.Reconciliation != "" || !slices.Contains([]string{"continued-ready", "continuation-invalidated"}, result.Setup) {
		return Result{}, refused
	}
	for i, step := range p.document.Contract.Manual {
		if result.Manual[i] != (ManualClaim{ID: step.ID, Provenance: "operator-declared-current-continuation"}) {
			return Result{}, refused
		}
	}
	for i, name := range proof.Checks {
		if name != fmt.Sprintf("checks/c%04d", i+1) {
			return Result{}, refused
		}
		checkFiles := nestedFiles(files, name)
		check, err := openSnapshot(checkFiles)
		if err != nil || check.Plan != p.identity || check.Setup != "reconciled-not-ready" || check.Cleanup != "not-applicable" {
			return Result{}, refused
		}
		var actual Snapshot
		if decode(checkFiles["actions/n0001/response.bin"], &actual) != nil || currentPrerequisites(p, prior, actual) != nil {
			return Result{}, refused
		}
	}
	if result.Setup == "continued-ready" && len(proof.Checks) == 0 {
		return Result{}, refused
	}
	if result.Cleanup == "complete" {
		cleanup, err := openSnapshot(nestedFiles(files, "cleanup"))
		if err != nil || cleanup.Plan != p.identity || cleanup.Setup != "recovered-no-setup" || cleanup.Cleanup != "complete" || !reflect.DeepEqual(cleanup.Entries, result.Entries) || result.Lease != (Lease{}) || len(proof.Checks) == 0 {
			return Result{}, refused
		}
		last := nestedFiles(files, proof.Checks[len(proof.Checks)-1])
		if cleanup.Reconciliation != strings.TrimSpace(string(last["identity.sha256"])) {
			return Result{}, refused
		}
	} else {
		if !slices.Contains([]string{"not-started", "failed", "uncertain"}, result.Cleanup) || result.Lease != prior.Lease {
			return Result{}, refused
		}
		cleanupFiles := nestedFiles(files, "cleanup")
		if result.Cleanup == "not-started" {
			if len(result.Entries) != 0 || len(cleanupFiles) != 0 {
				return Result{}, refused
			}
		} else {
			owned := []Resource{}
			for _, r := range prior.Resources {
				if r.Owner == p.document.Scope.Owner {
					owned = append(owned, r)
				}
			}
			if !(result.Cleanup == "failed" && len(cleanupFiles) == 0 && len(result.Entries) == 0) && verifyIncompleteCleanup(p, cleanupFiles, result, ResultSchema, "recovered-no-setup", owned) != nil {
				return Result{}, refused
			}
		}
	}
	return result, nil
}
