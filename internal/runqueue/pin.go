package runqueue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/durablerun"
)

// PinnedJob is one test of a prepared suite as a schedule runs it: its queue
// id, the spec it executes, the jobs that must pass first and the input
// identity its run is pinned to.
type PinnedJob struct {
	ID    string
	Spec  string
	After []string
	Input string
}

// PreparedQueue owns the approved execution configuration and all prepared
// inputs. Relocation changes no pin; changes to scheduling require review.
type PreparedQueue struct {
	identity string
	plan     Plan
	jobs     []PinnedJob
	runners  []runner
}

func (p *PreparedQueue) Identity() string { return p.identity }
func (p *PreparedQueue) Jobs() []PinnedJob {
	jobs := slices.Clone(p.jobs)
	for i := range jobs {
		jobs[i].After = slices.Clone(jobs[i].After)
	}
	return jobs
}

// ErrUnpinned reports a prepared suite whose queue or any test in it
// does not prepare on this host.
var ErrUnpinned = errors.New("the prepared suite cannot be read on this host")

// PinnedJobs reads a prepared suite's queue.json and prepares every test in
// it, answering one identity over the ordered jobs and their input
// identities, and the jobs. The same bytes answer the same identity on the
// machine that prepared the suite and on the runner host that runs it.
func PinnedJobs(ctx context.Context, queuePath string) (string, []PinnedJob, error) {
	prepared, err := PrepareQueue(ctx, queuePath)
	if err != nil {
		return "", nil, err
	}
	return prepared.Identity(), prepared.Jobs(), nil
}

// PrepareQueue uses a versioned identity over queue configuration and prepared
// test identities. Legacy partial pins remain readable history but cannot match
// this identity: an existing schedule pauses for a new explicit review.
func PrepareQueue(ctx context.Context, queuePath string) (*PreparedQueue, error) {
	raw, err := artifactdir.Document{MaxBytes: 1 << 20}.Read(queuePath)
	if err != nil {
		return nil, ErrUnpinned
	}
	plan, err := DecodePlan(raw)
	if err != nil {
		return nil, ErrUnpinned
	}
	prepared := &PreparedQueue{plan: plan}
	jobs := make([]PinnedJob, 0, len(plan.Jobs))
	for _, job := range plan.Jobs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		spec := filepath.Join(filepath.Dir(queuePath), job.Spec)
		input, err := durablerun.Prepare(spec)
		if err != nil {
			return nil, ErrUnpinned
		}
		identity, err := input.InputIdentity()
		if err != nil {
			return nil, ErrUnpinned
		}
		jobs = append(jobs, PinnedJob{ID: job.ID, Spec: spec, After: job.After, Input: identity})
		prepared.runners = append(prepared.runners, input)
	}
	inputs := make([]string, len(jobs))
	for i := range jobs {
		inputs[i] = jobs[i].Input
	}
	pinBytes, err := json.Marshal(struct {
		Schema string
		Plan   Plan
		Inputs []string
	}{"readmit-prepared-queue/v2", plan, inputs}, json.Deterministic(true))
	if err != nil {
		return nil, ErrUnpinned
	}
	hash := sha256.Sum256(pinBytes)
	prepared.identity, prepared.jobs = hex.EncodeToString(hash[:]), jobs
	return prepared, nil
}

// PinnedExecution is the existing local/remote runner adapter, not a scheduler.
// It executes one exact job and returns its real durable lifecycle summary.
type PinnedExecution func(context.Context, PinnedJob, string) (durablerun.Summary, error)

type dispatchedRunner struct {
	runner
	job     PinnedJob
	id      string
	execute PinnedExecution
}

func (r dispatchedRunner) Start(ctx context.Context, _ string) (durablerun.Summary, error) {
	return r.execute(ctx, r.job, r.id)
}

// Dispatch runs the existing queue scheduler at a legacy customer runner's
// capacity of one environment lease. It retains skipped/refused jobs and the
// whole denominator; no hub caller interprets dependency order itself.
func (p *PreparedQueue) Dispatch(ctx context.Context, runs, instance string, execute PinnedExecution) (Report, error) {
	if p == nil || execute == nil || jobID(instance) != nil {
		return Report{}, ErrUnpinned
	}
	if _, err := durablerun.Claims(runs, nil); err != nil {
		return Report{}, err
	}
	q := &schedule{runs: runs, held: map[string]string{}, owned: map[string]bool{}}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	guarded := func(ctx context.Context, job PinnedJob, id string) (durablerun.Summary, error) {
		summary, err := execute(ctx, job, id)
		if summary.DeliveryUncertain || summary.JournalIncomplete || summary.State == durablerun.Cancelled {
			stop()
		}
		return summary, err
	}
	byID := map[string]*state{}
	for i, job := range p.plan.Jobs {
		id := fmt.Sprintf("%s-%d", instance, i)
		output := filepath.Join(runs, id)
		if _, err := os.Lstat(output); !os.IsNotExist(err) {
			return Report{}, errors.New("a dispatched job's output is already held or unreadable; nothing started")
		}
		input := p.jobs[i]
		input.After = slices.Clone(input.After)
		current := &state{job: job, output: output, runner: dispatchedRunner{runner: p.runners[i], job: input, id: id, execute: guarded}, report: JobReport{ID: job.ID, Isolation: job.Isolation, Resources: p.runners[i].Resources()}}
		q.states = append(q.states, current)
		q.owned[id] = true
		byID[job.ID] = current
	}
	for _, current := range q.states {
		for _, after := range current.job.After {
			current.after = append(current.after, byID[after])
		}
	}
	q.run(ctx, 1)
	return q.report(1), nil
}
