package suite

import (
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const ConnectedPreparedSchema = "readmit-suite-prepared/v2"

type ConnectedRequest struct {
	Path, Environment, Output, Identity    string
	Promotion, PromotionIdentity, Revision string
	Instance                               string
	Execute                                runqueue.ConnectedExecution
}

// ConnectedPrepared is an immutable expansion. Readiness is checked only by
// an explicitly executing caller; preparing never starts a worker or provider.
type ConnectedPrepared struct {
	Directory    string
	Document     ConnectedDocument
	Queue        runqueue.ConnectedPlan
	Capabilities runnerprotocol.Capabilities
	Identity     string
	flows        map[string]*connectedrun.PreparedFlow
}

type connectedPreparation struct {
	Schema       string                      `json:"schema"`
	Suite        string                      `json:"suite"`
	Environment  string                      `json:"environment"`
	Input        string                      `json:"input"`
	Promotion    string                      `json:"promotion"`
	Revision     string                      `json:"revision_assumption"`
	Capabilities runnerprotocol.Capabilities `json:"capabilities"`
}

var connectedPreparedFamily = artifactdir.Family{
	Layout: artifactdir.Layout{Noun: "connected suite preparation", RequiredFiles: []string{"suite.json", "queue.json", "manifest.json", "identity.sha256"},
		AllowedDirectories: []string{"configurations", "releases", "inputs", "registries"}, Nested: []string{"plans"}, MaxFiles: 200000, MaxFileBytes: 64 << 20, MaxBytes: 1 << 30,
		AllowFile: func(n string) bool {
			return n == "suite.json" || n == "queue.json" || n == "manifest.json" || n == "identity.sha256" ||
				n == "promotion.json" ||
				strings.HasPrefix(n, "configurations/") && strings.HasSuffix(n, ".json") || strings.HasPrefix(n, "releases/") && strings.HasSuffix(n, ".json") || strings.HasPrefix(n, "inputs/") && strings.HasSuffix(n, ".json") || strings.HasPrefix(n, "registries/") && strings.HasSuffix(n, ".json")
		}}, Seal: artifactdir.DirectoryHash(ConnectedPreparedSchema),
}

func validateConnectedRequest(r ConnectedRequest) error {
	if r.Path == "" || r.Environment == "" || r.Output == "" || r.Identity != "" && !validDigest(r.Identity) ||
		r.Promotion == "" && (r.PromotionIdentity != "" || r.Revision != "") ||
		r.Promotion != "" && (!validDigest(r.PromotionIdentity) || !text(r.Revision, 256)) || r.Promotion != "" && r.Identity != "" {
		return errors.New("invalid connected suite operation and pins")
	}
	return nil
}

// PrepareConnected validates all declarations, approvals and local runtime
// inputs before it reserves output, then retains the exact selected plans.
func PrepareConnected(request ConnectedRequest) (*ConnectedPrepared, error) {
	if err := validateConnectedRequest(request); err != nil {
		return nil, err
	}
	raw, err := read(request.Path, MaxBytes)
	if err != nil {
		return nil, err
	}
	if request.Identity != "" && Identity(raw) != request.Identity {
		return nil, ErrChanged
	}
	doc, err := DecodeConnected(raw)
	if err != nil {
		return nil, err
	}
	resolved, err := artifactpath.Resolve(request.Path)
	if err != nil {
		return nil, err
	}
	root := filepath.Dir(resolved)
	selected := slices.IndexFunc(doc.Environments, func(e ConnectedEnvironment) bool { return e.ID == request.Environment })
	if selected < 0 {
		return nil, errors.New("connected suite does not declare the selected environment")
	}
	p := &ConnectedPrepared{Document: doc, Queue: runqueue.ConnectedPlan{Schema: runqueue.ConnectedPlanSchema, Parallelism: doc.Parallelism, Jobs: []runqueue.ConnectedJob{}},
		flows: map[string]*connectedrun.PreparedFlow{}, Capabilities: runnerprotocol.Capabilities{Schema: runnerprotocol.CapabilitiesSchema, Pins: []runnerprotocol.CapabilityPin{}}}
	plans, configurations, releases := map[string]*connectedtest.FlowPlan{}, map[string][]byte{}, map[string][]byte{}
	pins := map[string]runnerprotocol.CapabilityPin{}
	for _, test := range doc.Tests {
		at := slices.IndexFunc(doc.Environments[selected].Bindings, func(b ConnectedBinding) bool { return b.Test == test.ID })
		binding := doc.Environments[selected].Bindings[at]
		planPath := artifactpath.JoinReference(root, binding.Plan)
		plan, err := connectedtest.OpenFlowPlan(planPath)
		if err != nil || plan.Identity() != binding.PlanIdentity {
			return nil, errors.New("connected suite plan differs from its selected revision")
		}
		review, err := expectation.ReviewConnected(planPath)
		if err != nil || review.ID != test.ID || review.Revision != test.Revision || review.Definition != test.Definition {
			return nil, errors.New("connected suite authored expectations differ from their revision pin")
		}
		releaseRaw, err := read(artifactpath.JoinReference(root, test.Release), expectation.MaxBytes)
		if err != nil {
			return nil, err
		}
		release, err := expectation.DecodeConnected(releaseRaw)
		if err != nil || release.Identity() != test.ReleaseIdentity || release.Review != review {
			return nil, errors.New("connected suite expectations lack the exact approved release")
		}
		config := artifactpath.JoinReference(root, binding.Config)
		prepared, err := connectedrun.PrepareFlow(planPath, config, "suite-preview")
		if err != nil {
			return nil, err
		}
		input, err := prepared.InputIdentity()
		if err != nil {
			return nil, err
		}
		capabilities, err := prepared.Capabilities()
		if err != nil {
			return nil, err
		}
		if p.Capabilities.Engine != "" && p.Capabilities.Engine != capabilities.Engine {
			return nil, errors.New("connected suite requires incompatible engines")
		}
		p.Capabilities.Engine = capabilities.Engine
		for _, pin := range capabilities.Pins {
			key := pin.Kind + "/" + pin.ID + "/" + pin.Version
			if old, found := pins[key]; found && old != pin {
				return nil, errors.New("connected suite requires incompatible capability pins")
			}
			pins[key] = pin
		}
		configurations[test.ID], err = read(config, 2<<20)
		if err != nil {
			return nil, err
		}
		plans[test.ID], releases[test.ID], p.flows[test.ID] = plan, releaseRaw, prepared
		p.Queue.Jobs = append(p.Queue.Jobs, runqueue.ConnectedJob{ID: test.ID, Plan: "plans/" + test.ID, PlanIdentity: plan.Identity(), Config: config, Input: input, After: test.After, State: test.State})
	}
	keys := []string{}
	for key := range pins {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		p.Capabilities.Pins = append(p.Capabilities.Pins, pins[key])
	}
	if err := p.Capabilities.Validate(); err != nil {
		return nil, err
	}
	queueRaw, err := json.Marshal(p.Queue, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	if _, err = runqueue.DecodeConnectedPlan(queueRaw); err != nil {
		return nil, err
	}
	// This identity does not depend on the fresh output directory or runtime
	// instance. It commits every job, capability and environment choice.
	p.Identity = identity(struct {
		Suite        string
		Environment  string
		Queue        runqueue.ConnectedPlan
		Capabilities runnerprotocol.Capabilities
	}{Identity(raw), request.Environment, p.Queue, p.Capabilities})
	// Approved promotion is checked against this exact expansion before its
	// output is reserved. Use a private review record without publishing it.
	var promotionRaw []byte
	if request.Promotion != "" {
		var e error
		promotionRaw, e = read(request.Promotion, MaxBytes)
		if e != nil {
			return nil, e
		}
		promotion, e := DecodeConnectedPromotion(promotionRaw)
		capability, capErr := p.Capabilities.Identity()
		review := ConnectedPromotionReview{Schema: ConnectedPromotionReviewSchema, Suite: Identity(raw), Environment: request.Environment, Revision: request.Revision, Input: p.Identity, Capabilities: capability, Jobs: []CoverageSpecification{}}
		for _, job := range p.Queue.Jobs {
			review.Jobs = append(review.Jobs, CoverageSpecification{Job: job.ID, SHA256: job.Input})
		}
		if e != nil || capErr != nil || promotion.Identity() != request.PromotionIdentity || review.Identity() != promotion.Reviewed {
			return nil, errors.New("connected suite inputs or environment differ from promotion approval")
		}
	}
	w, err := artifactdir.Create(request.Output, connectedPreparedFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer w.Close()
	p.Directory = w.Path()
	if err = w.WriteFile("suite.json", raw); err != nil {
		return nil, err
	}
	if err = w.WriteFile("queue.json", queueRaw); err != nil {
		return nil, err
	}
	if request.Promotion != "" {
		if e := w.WriteFile("promotion.json", promotionRaw); e != nil {
			return nil, e
		}
	}
	if err = w.Mkdir("plans"); err != nil {
		return nil, err
	}
	for _, test := range doc.Tests {
		if err = plans[test.ID].Write(context.Background(), filepath.Join(w.Path(), "plans", test.ID)); err != nil {
			return nil, err
		}
		if err = w.WriteFile("configurations/"+test.ID+".json", configurations[test.ID]); err != nil {
			return nil, err
		}
		if err = w.WriteFile("releases/"+test.ID+".json", releases[test.ID]); err != nil {
			return nil, err
		}
		input, err := p.flows[test.ID].InputSnapshot()
		if err != nil {
			return nil, err
		}
		if err = w.WriteFile("inputs/"+test.ID+".json", input); err != nil {
			return nil, err
		}
		registry, err := p.flows[test.ID].RegistrySnapshot()
		if err != nil {
			return nil, err
		}
		if err = w.WriteFile("registries/"+test.ID+".json", registry); err != nil {
			return nil, err
		}
	}
	manifest, err := json.Marshal(connectedPreparation{Schema: ConnectedPreparedSchema, Suite: Identity(raw), Environment: request.Environment, Input: p.Identity, Promotion: request.PromotionIdentity, Revision: request.Revision, Capabilities: p.Capabilities}, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	if err = w.WriteFile("manifest.json", manifest); err != nil {
		return nil, err
	}
	if _, err = w.Seal(nil); err != nil {
		return nil, err
	}
	return p, nil
}

// CheckCapabilities is explicit execution preflight, separate from all passive
// readers and preparation. It checks every requested worker before any job.
func (p *ConnectedPrepared) CheckCapabilities(ctx context.Context) error {
	for _, job := range p.Queue.Jobs {
		if job.State == "enabled" {
			if err := p.flows[job.ID].CheckCapabilities(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *ConnectedPrepared) Resources() []string {
	keys := map[string]bool{}
	for _, flow := range p.flows {
		for _, resource := range flow.Resources() {
			keys[resource] = true
		}
	}
	resources := []string{}
	for resource := range keys {
		resources = append(resources, resource)
	}
	slices.Sort(resources)
	return resources
}

// Operations is the finite set the compiler-derived bindings require. It is
// read-only metadata; the installed runner authority separately permits it.
func (p *ConnectedPrepared) Operations() []sendpolicy.Operation {
	seen := map[sendpolicy.Operation]bool{}
	for _, flow := range p.flows {
		for _, binding := range flow.Bindings() {
			seen[binding.Operation] = true
		}
	}
	operations := []sendpolicy.Operation{}
	for operation := range seen {
		operations = append(operations, operation)
	}
	slices.Sort(operations)
	return operations
}
