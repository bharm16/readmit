package operationguard_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/operationguard"
)

// The profiles every entry point declares: nothing, the configured author,
// one execution held for the whole operation, and execution admitted by each
// job the operation starts.
var (
	free    = operationguard.Profile{Name: "free"}
	author  = operationguard.Profile{Name: "author", Author: true}
	execute = operationguard.Profile{Name: "execute", Interruptible: true, Execution: operationguard.Execute}
	eachJob = operationguard.Profile{Name: "each-job", Interruptible: true, Execution: operationguard.ExecuteEachJob}
)

// declined is the admission refusal an operation answered with, or nil.
func declined(err error) *operationguard.Declined {
	var refused *operationguard.Declined
	if errors.As(err, &refused) {
		return refused
	}
	return nil
}

func TestAProfileTakesExactlyTheAdmissionItDeclares(t *testing.T) {
	unconfigured := operationguard.New("")
	ran := false
	if err := unconfigured.Run(t.Context(), free, func(context.Context) error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("free work without an activation: ran %v, %v", ran, err)
	}
	for _, profile := range []operationguard.Profile{author, execute} {
		ran = false
		err := unconfigured.Run(t.Context(), profile, func(context.Context) error { ran = true; return nil })
		if refused := declined(err); refused == nil || refused.Cancelled || !errors.Is(err, operationguard.ErrUnavailable) || ran {
			t.Errorf("%s without an activation: ran %v, %v", profile.Name, ran, err)
		} else if refused.Execution != (profile.Execution == operationguard.Execute) {
			t.Errorf("%s without an activation was declined with execution %v", profile.Name, refused.Execution)
		}
	}
	// A profile that admits the author and then executes is declined by the
	// author admission first.
	both := operationguard.Profile{Name: "both", Author: true, Execution: operationguard.Execute}
	if refused := declined(unconfigured.Run(t.Context(), both, func(context.Context) error { return nil })); refused == nil || refused.Execution {
		t.Errorf("an author and execution profile was not declined by its author admission: %+v", refused)
	}
	// Taking each job's admission is the jobs' own: the operation itself
	// starts, and its first job is refused.
	ran = false
	err := unconfigured.Run(t.Context(), eachJob, func(ctx context.Context) error {
		ran = true
		return operationguard.RunJob(ctx, func(context.Context) error { t.Fatal("an unadmitted job ran"); return nil })
	})
	if !ran || declined(err) == nil || !errors.Is(err, operationguard.ErrUnavailable) {
		t.Fatalf("each job without an activation: ran %v, %v", ran, err)
	}

	f := activated(t)
	g := operationguard.NewWithClock(f.policy, f.clock)
	for _, profile := range []operationguard.Profile{free, author, execute} {
		ran = false
		if err := g.Run(t.Context(), profile, func(context.Context) error { ran = true; return nil }); err != nil || !ran {
			t.Errorf("%s under an activation: ran %v, %v", profile.Name, ran, err)
		}
	}
	// An author's admission holds no runner instance, so only an execution
	// can exhaust the authority's one instance.
	err = g.Run(t.Context(), author, func(context.Context) error {
		return operationguard.NewWithClock(f.policy, f.clock).Run(t.Context(), execute, func(context.Context) error { return nil })
	})
	if err != nil {
		t.Fatalf("an author's admission held a runner instance: %v", err)
	}
}

func TestAnExecutionIsBoundedAndSettledAndReturnsItsWorksResult(t *testing.T) {
	f := activated(t)
	g := operationguard.NewWithClock(f.policy, f.clock)
	failed := errors.New("the work failed")
	started := time.Now()
	err := g.Run(t.Context(), execute, func(ctx context.Context) error {
		deadline, bounded := ctx.Deadline()
		if !bounded || deadline.After(started.Add(operationguard.MaxDuration).Add(time.Minute)) || deadline.Before(started.Add(operationguard.MaxDuration).Add(-time.Minute)) {
			t.Errorf("the execution was handed deadline %v (bounded %v), want MaxDuration from its start", deadline, bounded)
		}
		// The one instance is held for the whole operation.
		if err := operationguard.NewWithClock(f.policy, f.clock).Run(ctx, execute, func(context.Context) error { return nil }); !errors.Is(err, entitlement.ErrCapacityExhausted) {
			t.Errorf("a second execution was admitted beside a held one: %v", err)
		}
		return failed
	})
	if err != failed {
		t.Fatalf("the execution answered %v, want its work's own result", err)
	}
	// Settled: the instance is free for the next execution.
	if err := operationguard.NewWithClock(f.policy, f.clock).Run(t.Context(), execute, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("a finished execution was not settled: %v", err)
	}
}

// A queue holds one instance for its whole life and rechecks the term before
// each new job, as TestEightJobsShareOneInstanceButEveryJobRechecksTerm does
// at the guard: a term that ends part way stops the jobs not yet started.
func TestEveryJobAnExecutionQueuesIsRecheckedUnderItsInstance(t *testing.T) {
	if err := operationguard.Recheck(t.Context()); err != nil {
		t.Fatalf("a queue with no admission in its context was refused: %v", err)
	}
	f := activated(t)
	g := operationguard.NewWithClock(f.policy, f.clock)
	err := g.Run(t.Context(), execute, func(ctx context.Context) error {
		for range 8 {
			if err := operationguard.Recheck(ctx); err != nil {
				t.Fatalf("a job under a current term was refused: %v", err)
			}
		}
		f.wall = f.claims.Expires
		if err := operationguard.Recheck(ctx); !errors.Is(err, entitlement.ErrExpired) {
			t.Errorf("a job after the term ended was admitted: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the queue's instance was not settled after its term ended: %v", err)
	}
	// A context whose jobs are admitted one by one holds no instance for a
	// queue to recheck under.
	err = g.Run(t.Context(), eachJob, func(ctx context.Context) error { return operationguard.Recheck(ctx) })
	if !errors.Is(err, operationguard.ErrUnavailable) {
		t.Fatalf("a queue job was admitted under no held instance: %v", err)
	}
}

// A runner's job is its own execution when nothing holds one, and is rechecked
// under the operation's instance when something does, so admitting the
// operation first never fails the job as a second instance.
func TestAJobIsItsOwnExecutionUnlessItsOperationHoldsOne(t *testing.T) {
	if err := operationguard.RunJob(t.Context(), func(context.Context) error { t.Fatal("a job ran with no admission"); return nil }); !errors.Is(err, operationguard.ErrUnavailable) {
		t.Fatalf("a job with no admission in its context: %v", err)
	}
	f := activated(t)
	g := operationguard.NewWithClock(f.policy, f.clock)
	jobs := 0
	err := g.Run(t.Context(), eachJob, func(ctx context.Context) error {
		if _, bounded := ctx.Deadline(); bounded {
			t.Error("an operation whose jobs are admitted one by one was itself bounded")
		}
		// Two jobs in turn under an authority of one instance: each is
		// admitted, bounded and settled before the next.
		for range 2 {
			if err := operationguard.RunJob(ctx, func(ctx context.Context) error {
				if _, bounded := ctx.Deadline(); !bounded {
					t.Error("a job's execution was not bounded")
				}
				jobs++
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil || jobs != 2 {
		t.Fatalf("jobs admitted one by one: %d ran, %v", jobs, err)
	}
	jobs = 0
	err = g.Run(t.Context(), execute, func(ctx context.Context) error {
		if err := operationguard.RunJob(ctx, func(context.Context) error { jobs++; return nil }); err != nil {
			t.Errorf("a job under its operation's held instance: %v", err)
		}
		f.wall = f.claims.Expires
		return operationguard.RunJob(ctx, func(context.Context) error { jobs++; return nil })
	})
	if declined(err) == nil || !errors.Is(err, entitlement.ErrExpired) || jobs != 1 {
		t.Fatalf("a job after its operation's term ended: %d ran, %v", jobs, err)
	}
}

// A settlement failure overrides whatever the work answered: the retained
// instance must be reconciled before new work, and the work's own result is
// kept beside the failure rather than dropped.
func TestASettlementFailureOverridesTheResult(t *testing.T) {
	for _, result := range []error{nil, errors.New("the work failed")} {
		f := activated(t)
		g := operationguard.NewWithClock(f.policy, f.clock)
		admissions := filepath.Join(filepath.Dir(f.policy), "admissions.json")
		err := g.Run(t.Context(), execute, func(context.Context) error {
			// The record the instance is released into is no longer readable.
			if err := os.WriteFile(admissions, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			return result
		})
		var unsettled *operationguard.Unsettled
		if !errors.As(err, &unsettled) || unsettled.Err == nil || declined(err) != nil {
			t.Fatalf("an execution whose settlement failed answered %v", err)
		}
		if result != nil && (!errors.Is(err, result) || unsettled.Result != result) {
			t.Errorf("the settlement failure dropped the work's own result: %v", err)
		}
		if want := errors.Join(result, unsettled.Err).Error(); err.Error() != want {
			t.Errorf("the settlement failure reads %q, want %q", err, want)
		}
	}
}

// Admission can wait while another update of the operation clock is retained.
// A cancellation that arrives meanwhile is the person's, not a refusal: the
// operation answers cancelled, promptly, and nothing was admitted to settle.
func TestACancellationDuringAdmissionIsReportedAsCancelled(t *testing.T) {
	for _, profile := range []operationguard.Profile{author, execute} {
		f := activated(t)
		lock := filepath.Join(filepath.Dir(f.policy), "clock.json.incomplete")
		if err := os.WriteFile(lock, []byte("retained update"), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(20*time.Millisecond, cancel)
		requested := time.Now()
		err := operationguard.NewWithClock(f.policy, f.clock).Run(ctx, profile, func(context.Context) error {
			t.Fatal("work ran while its admission waited")
			return nil
		})
		if refused := declined(err); refused == nil || !refused.Cancelled {
			t.Errorf("%s cancelled during admission answered %v, want cancelled", profile.Name, err)
		}
		if waited := time.Since(requested); waited > 500*time.Millisecond {
			t.Errorf("%s waited %s for admission to give up", profile.Name, waited)
		}
		admissions, err := entitlement.OpenAdmissions(filepath.Join(filepath.Dir(f.policy), "admissions.json"))
		if err != nil || len(admissions.Record.Admissions) != 0 {
			t.Errorf("%s cancelled during admission admitted work: %v", profile.Name, err)
		}
		// Refused without a cancellation, the same wait is a refusal.
		os.Remove(lock)
		if err := operationguard.Release(f.policy); err != nil {
			t.Fatal(err)
		}
		if refused := declined(operationguard.NewWithClock(f.policy, f.clock).Run(t.Context(), profile, func(context.Context) error { return nil })); refused == nil || refused.Cancelled || !errors.Is(refused, entitlement.ErrReleased) {
			t.Errorf("%s refused by a released activation answered %v", profile.Name, refused)
		}
	}
}

func TestAProfileStatesItsAdmissionInTheLedgersWords(t *testing.T) {
	for _, declared := range []struct {
		profile operationguard.Profile
		words   []string
	}{
		{free, nil}, {author, []string{"author"}}, {execute, []string{"execute"}}, {eachJob, []string{"execute"}},
		{operationguard.Profile{Author: true, Execution: operationguard.Execute}, []string{"author", "execute"}},
	} {
		if got := declared.profile.Prerequisites(); !slices.Equal(got, declared.words) {
			t.Errorf("%+v states %v, want %v", declared.profile, got, declared.words)
		}
	}
}
