package customerrunner

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testisolation"
)

type connectedSuiteClaim struct {
	Schema    string `json:"schema"`
	Instance  string `json:"instance"`
	Input     string `json:"input"`
	Authority string `json:"authority"`
	Promotion string `json:"promotion"`
	Output    string `json:"output"`
}

// RunConnectedSuite is the actual customer execution path: one passive
// expansion, current installed finite authority, real mTLS hub admission,
// fresh effect bindings and the same suite/lifecycle service. A lost dispatch
// cannot be claimed again or used to manufacture a new successful result.
func RunConnectedSuite(ctx context.Context, c Config, request suite.ConnectedRequest, authorityPath string) (report runqueue.ConnectedReport, err error) {
	if c.validate() != nil || request.Execute != nil || !runnerprotocol.ID(request.Instance) || request.Promotion == "" || request.PromotionIdentity == "" {
		return report, refuseConnected("runner-not-configured", ErrRefused, "private-runner-config", "exact-promotion", "finite-authority", "dispatch-identity")
	}
	parent, err := os.MkdirTemp("", "readmit-runner-suite-")
	if err != nil {
		return report, err
	}
	defer os.RemoveAll(parent)
	preview := request
	preview.Output = filepath.Join(parent, "prepared")
	prepared, err := suite.PrepareConnected(preview)
	if err != nil {
		return report, refuseConnected("prepared-capability-or-input-unavailable", err, "exact-test-revision", "profile-and-terminology-pins", "collector-driver", "selected-validator-worker")
	}
	promotion, err := prepared.VerifyPromotion(request.Promotion, request.PromotionIdentity, request.Environment, request.Revision)
	if err != nil {
		return report, refuseConnected("promotion-not-approved", err, "exact-environment-promotion")
	}
	installed, err := readConnectedAuthority(authorityPath)
	if err != nil {
		return report, refuseConnected("installed-authority-unavailable", err, "finite-connected-authority")
	}
	capability, err := prepared.Capabilities.Identity()
	if err != nil {
		return report, err
	}
	project, environment, err := prepared.RuntimeScope()
	if err != nil || c.Project != project || c.Environment != environment || installed.declaration.Input != prepared.Identity ||
		installed.declaration.Promotion != promotion.Identity() || installed.declaration.Capabilities != capability {
		return report, refuseConnected("authority-scope-mismatch", ErrRefused, "exact-promoted-input", "capability-agreement", "registered-environment")
	}
	operations := slices.Clone(installed.declaration.Operations)
	slices.Sort(operations)
	if !slices.Equal(operations, prepared.Operations()) {
		return report, refuseConnected("authority-operation-mismatch", ErrRefused, "exact-permitted-operations")
	}
	err = operationguard.RunJob(ctx, func(ctx context.Context) error {
		var runErr error
		report, runErr = runConnectedSuiteAdmitted(ctx, c, request, prepared, installed)
		return runErr
	})
	return report, err
}

func runConnectedSuiteAdmitted(ctx context.Context, c Config, request suite.ConnectedRequest, prepared *suite.ConnectedPrepared, installed installedConnectedAuthority) (report runqueue.ConnectedReport, err error) {
	var jobs []string
	id := "c-" + networkaction.Digest([]byte(request.Instance))[:40]
	if retained, e := Retained(c.Root, id); e != nil || retained {
		return report, errors.New("this connected occurrence is already retained and will never run again")
	}
	if _, e := os.Lstat(request.Output); !os.IsNotExist(e) {
		return report, errors.New("connected suite output must be a new private directory")
	}
	// The root's atomic claim also excludes a legacy process using this root.
	// A process kill retains this claim; expiry never removes it or retries it.
	active := filepath.Join(c.Root, ".active")
	if err = os.Mkdir(active, 0700); err != nil {
		return report, errors.New("the customer runner requires recovery before another occurrence")
	}
	jobs, err = Jobs(c.Root)
	if err != nil || len(jobs) >= installed.declaration.MaxOccurrences {
		_ = os.Remove(active)
		return report, errors.New("the finite connected authority's retained occurrence limit is exhausted")
	}
	jobDirectory := filepath.Join(c.Root, id)
	if err = os.Mkdir(jobDirectory, 0700); err != nil {
		return report, errors.New("this connected occurrence is already retained and will never run again")
	}
	claim := connectedSuiteClaim{Schema: "readmit-connected-suite-claim/v1", Instance: request.Instance, Input: prepared.Identity, Authority: installed.declaration.Identity(), Promotion: request.PromotionIdentity, Output: request.Output}
	raw, err := json.Marshal(claim, json.Deterministic(true))
	if err != nil || persist(filepath.Join(jobDirectory, "connected-claim.json"), raw) != nil || syncDirectory(c.Root) != nil {
		return report, errors.New("connected dispatch claim could not be persisted; inspect retained uncertainty")
	}
	remoteRequest := runnerprotocol.ConnectedRequest{Schema: runnerprotocol.ConnectedRequestSchema, Environment: c.Environment, Instance: request.Instance, Job: id,
		Engine: prepared.Capabilities.Engine, Capabilities: prepared.Capabilities, Resources: prepared.Resources(), Input: prepared.Identity}
	client, err := EnrollConnected(ctx, c, remoteRequest)
	if err != nil {
		// No effect was admitted. The occurrence ID stays consumed locally.
		_ = os.Remove(active)
		if errors.Is(err, ErrHubRefused) {
			return report, refuseConnected("capability-or-admission-denied", err, "current-enrollment", "exact-engine-contract-pack-collector-smart-validator-agreement")
		}
		return report, refuseConnected("hub-or-provider-unavailable", err, "private-credential-provider", "verified-hub-tls", "current-enrollment")
	}
	lease := client.Lease()
	if len(jobs)+1 > lease.MaxJobs {
		_ = client.Settle(context.WithoutCancel(ctx), false)
		_ = os.Remove(active)
		return report, errors.New("the current hub retained job limit is exhausted")
	}
	maxSeconds := min(lease.MaxSeconds, installed.declaration.MaxSeconds)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(maxSeconds)*time.Second)
	defer cancel()
	var effects atomic.Uint64
	var control sync.Mutex
	expiry := time.AfterFunc(time.Until(lease.Expires), cancel)
	defer expiry.Stop()
	store := func() error {
		current := client.Lease()
		if current.MaxJobs < lease.MaxJobs || current.MaxSeconds < lease.MaxSeconds {
			return errors.New("the current connected runner grant was narrowed")
		}
		raw, e := json.Marshal(current, json.Deterministic(true))
		if e != nil {
			return e
		}
		root, e := os.OpenRoot(active)
		if e != nil {
			return e
		}
		defer root.Close()
		if e = leaseFile.ReplaceIn(root, "lease.json", raw); e != nil {
			return e
		}
		if runCtx.Err() != nil || !current.Expires.After(time.Now()) {
			return errors.New("the current connected lease expired")
		}
		expiry.Reset(time.Until(current.Expires))
		return nil
	}
	check := func(callCtx context.Context) error {
		control.Lock()
		defer control.Unlock()
		if callCtx.Err() != nil || installed.current() != nil || operationguard.Recheck(callCtx) != nil {
			cancel()
			return errors.New("connected authority is no longer current")
		}
		if e := client.Check(callCtx); e != nil {
			cancel()
			return e
		}
		if e := store(); e != nil {
			cancel()
			return e
		}
		return nil
	}
	if err = store(); err != nil {
		_ = client.Settle(context.WithoutCancel(ctx), true)
		return report, err
	}
	// Renewals are independent of target calls or worker execution. Once the
	// lease timer cancels, a later renewal can never restore authority.
	done := make(chan struct{})
	stopHeartbeat := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if check(runCtx) != nil {
					return
				}
			}
		}
	}()
	inputPins := map[string]bool{}
	for _, job := range prepared.Queue.Jobs {
		inputPins[job.Input] = true
	}
	occurrenceExpiry, _ := runCtx.Deadline()
	occurrenceExpiry = minTime(occurrenceExpiry, installed.declaration.Expires)
	execute := func(jobCtx context.Context, flow *connectedrun.PreparedFlow, output string) (connectedrun.FlowResult, error) {
		input, e := flow.InputIdentity()
		if e != nil || !inputPins[input] {
			return connectedrun.FlowResult{}, errors.New("actual lifecycle inputs are outside the installed promoted authority")
		}
		allowed := map[networkaction.Binding]bool{}
		for _, binding := range flow.Bindings() {
			allowed[binding] = true
		}
		authority := connectedEffectAuthority{check: check, allowed: allowed, installed: installed, client: client, effects: &effects, expires: occurrenceExpiry}
		jobCtx = networkaction.WithAuthority(jobCtx, authority)
		return connectedrun.ExecuteFlow(jobCtx, flow, output, testisolation.Confirmation{})
	}
	report, err = suite.ExecuteConnected(runCtx, prepared, request.Instance, request.Output, execute)
	close(stopHeartbeat)
	<-done
	uncertain := err != nil && effects.Load() > 0
	for _, job := range report.Jobs {
		if job.ExecutionError && effects.Load() > 0 {
			uncertain = true
		}
		if job.Flow != nil && job.Flow.State != "complete" {
			uncertain = true
		}
	}
	if settleErr := client.Settle(context.WithoutCancel(ctx), uncertain); settleErr != nil {
		return report, errors.New("connected settlement was not acknowledged; retained dispatch remains uncertain")
	}
	if !uncertain {
		_ = os.Remove(filepath.Join(active, "lease.json"))
		_ = os.Remove(filepath.Join(active, "lease.next"))
		_ = os.Remove(active)
	}
	return report, err
}

type connectedEffectAuthority struct {
	check     func(context.Context) error
	allowed   map[networkaction.Binding]bool
	installed installedConnectedAuthority
	client    *ConnectedClient
	effects   *atomic.Uint64
	expires   time.Time
}

func (a connectedEffectAuthority) Check(ctx context.Context, binding networkaction.Binding) (networkaction.Actor, error) {
	if !a.allowed[binding] || !slices.Contains(a.installed.declaration.Operations, binding.Operation) {
		return networkaction.Actor{}, errors.New("the effect is outside the exact installed connected scope")
	}
	if err := a.check(ctx); err != nil {
		return networkaction.Actor{}, err
	}
	step := fmt.Sprintf("effect-%d", a.effects.Add(1))
	if err := a.client.Attempt(ctx, step); err != nil {
		return networkaction.Actor{}, err
	}
	lease := a.client.Lease()
	actor := a.installed.declaration
	// Actor identity is fixed for this finite occurrence. The short renewable
	// lease is rechecked independently before every use; changing Actor fields
	// during renewal would violate the lifecycle's exact pinned-actor rule.
	return networkaction.Actor{Kind: "runner", ID: actor.Actor, Generation: actor.Generation, Expires: a.expires,
		EvidenceIdentity: networkaction.Digest([]byte(actor.Identity() + "/" + fmt.Sprint(lease.Generation)))}, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
