package runqueue

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/testlicense"
)

const staging = "staging"
const endpoint = "127.0.0.1:2575"

// tracker records what was actually concurrent. The queue's own report says
// what it decided; this says what happened, so a serialization claim is not
// proved by the code that makes it.
type tracker struct {
	mu      sync.Mutex
	inside  map[string]int
	peak    map[string]int
	running int
	widest  int
	order   []string
	arrived int
	both    chan struct{}
}

func newTracker() *tracker {
	return &tracker{inside: map[string]int{}, peak: map[string]int{}, both: make(chan struct{})}
}

// rendezvous returns only once two jobs are inside it at the same time, so a
// test that claims declared isolation permits concurrency fails on a queue
// that serialized rather than on a timing coincidence.
func (t *tracker) rendezvous() error {
	t.mu.Lock()
	t.arrived++
	if t.arrived == 2 {
		close(t.both)
	}
	t.mu.Unlock()
	select {
	case <-t.both:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("no other job was running at the same time")
	}
}

func (t *tracker) enter(id string, resources []durablerun.Resource) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.running++
	if t.running > t.widest {
		t.widest = t.running
	}
	t.order = append(t.order, "enter "+id)
	for _, resource := range resources {
		t.inside[key(resource)]++
		if t.inside[key(resource)] > t.peak[key(resource)] {
			t.peak[key(resource)] = t.inside[key(resource)]
		}
	}
}

func (t *tracker) leave(id string, resources []durablerun.Resource) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.running--
	t.order = append(t.order, "leave "+id)
	for _, resource := range resources {
		t.inside[key(resource)]--
	}
}

// fake is one queued run that never opens a connection. It reports the
// resources a real run would declare and occupies them for long enough that an
// overlap the scheduler allowed would be observed rather than missed.
type fake struct {
	id        string
	resources []durablerun.Resource
	state     durablerun.State
	tracker   *tracker
	failure   error
	// meet makes a job wait for another job to be inside at the same time, so
	// declared isolation is proved to permit concurrency rather than assumed.
	meet bool
	// identity and identityErr drive the approved-inputs pin through the same
	// seam a real prepared run answers, so a pin refusal is asserted against
	// the interface rather than against a live spec.
	identity    string
	identityErr error
	// during runs while the job is inside, as something else happening to
	// the machine while the queue executes it.
	during func()
}

func (f *fake) Resources() []durablerun.Resource { return f.resources }

func (f *fake) InputIdentity() (string, error) { return f.identity, f.identityErr }

func (f *fake) Start(ctx context.Context, output string) (durablerun.Summary, error) {
	f.tracker.enter(f.id, f.resources)
	defer f.tracker.leave(f.id, f.resources)
	if f.during != nil {
		f.during()
	}
	if f.meet {
		if err := f.tracker.rendezvous(); err != nil {
			return durablerun.Summary{}, err
		}
	}
	// Holding the resources briefly widens the window in which a scheduler
	// that failed to serialize would be observed doing so. It cannot make a
	// correct scheduler fail: the assertions are on what was concurrent, so a
	// short hold can only under-detect a defect, never invent one.
	time.Sleep(2 * time.Millisecond)
	if f.failure != nil {
		return durablerun.Summary{}, f.failure
	}
	if err := ctx.Err(); err != nil {
		return durablerun.Summary{Schema: durablerun.Schema, State: durablerun.Cancelled, StopReason: durablerun.Cancelled, Planned: 1}, nil
	}
	return durablerun.Summary{Schema: durablerun.Schema, State: f.state, StopReason: f.state, Planned: 1, Recorded: 1}, nil
}

func environmentResources() []durablerun.Resource {
	return []durablerun.Resource{{Kind: durablerun.EnvironmentResource, Name: staging}, {Kind: durablerun.EndpointResource, Name: endpoint}}
}

// queued installs the one seam the scheduler's tests take: the runs are stood
// in for, and every scheduling decision under test is the real one.
func queued(t *testing.T, runs map[string]*fake) {
	t.Helper()
	original := prepare
	t.Cleanup(func() { prepare = original })
	prepare = func(specPath string) (runner, error) {
		run, found := runs[filepath.Base(specPath)]
		if !found {
			return nil, errors.New("no spec at " + filepath.Base(specPath))
		}
		return run, nil
	}
}

func queue(t *testing.T, document string, runsDirectory string) (Report, error) {
	t.Helper()
	return Run(t.Context(), Request{PlanBytes: []byte(document), PlanDirectory: t.TempDir(), Runs: runsDirectory})
}

func jobs(report Report) map[string]JobReport {
	found := map[string]JobReport{}
	for _, job := range report.Jobs {
		found[job.ID] = job
	}
	return found
}

// TestSharedJobsNeverOccupyOneEnvironmentAtTheSameTime is the ticket's rule:
// tests sharing mutable target state cannot race unless explicitly isolated.
// The queue is given room to run both at once and must still not.
func TestSharedJobsNeverOccupyOneEnvironmentAtTheSameTime(t *testing.T) {
	watch := newTracker()
	queued(t, map[string]*fake{
		"a.json": {id: "a", resources: environmentResources(), state: durablerun.Passed, tracker: watch},
		"b.json": {id: "b", resources: environmentResources(), state: durablerun.Passed, tracker: watch},
	})
	document := `{"schema":"readmit-run-queue/v1","parallelism":4,"jobs":[` +
		`{"id":"a","spec":"a.json","isolation":"shared"},` +
		`{"id":"b","spec":"b.json","isolation":"shared"}]}`
	report, err := queue(t, document, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if report.Schema != ReportSchema || report.Executed != 2 || report.Refused != 0 || report.Skipped != 0 || report.ExitCode() != 0 {
		t.Fatalf("%+v", report)
	}
	for resource, peak := range watch.peak {
		if peak > 1 {
			t.Fatalf("%d shared jobs held %q at once", peak, resource)
		}
	}
	if watch.widest != 1 {
		t.Fatalf("shared jobs ran %d at a time", watch.widest)
	}
	if jobs(report)["b"].WaitedFor != durablerun.EnvironmentResource+" "+staging {
		t.Fatalf("the queue did not report what the second job waited for: %+v", report.Jobs)
	}
}

// Declared isolation is what permits concurrency, and nothing else does. Both
// jobs must be inside at the same time, or the meeting they wait for never
// happens and the run reports it.
func TestIsolatedJobsRunTogetherWithinTheDeclaredParallelism(t *testing.T) {
	watch := newTracker()
	queued(t, map[string]*fake{
		"a.json": {id: "a", resources: environmentResources(), state: durablerun.Passed, tracker: watch, meet: true},
		"b.json": {id: "b", resources: environmentResources(), state: durablerun.Passed, tracker: watch, meet: true},
	})
	document := `{"schema":"readmit-run-queue/v1","parallelism":2,"jobs":[` +
		`{"id":"a","spec":"a.json","isolation":"isolated"},` +
		`{"id":"b","spec":"b.json","isolation":"isolated"}]}`
	report, err := queue(t, document, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if report.Executed != 2 || report.ExitCode() != 0 {
		t.Fatalf("%+v", report)
	}
	if watch.widest != 2 {
		t.Fatalf("isolated jobs ran %d at a time", watch.widest)
	}
}

// Parallelism is the queue's bound, and every job occupies a slot whatever it
// declared about state.
func TestParallelismBoundsHowManyIsolatedJobsRunAtOnce(t *testing.T) {
	watch := newTracker()
	runs := map[string]*fake{}
	document := `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[`
	for _, id := range []string{"a", "b", "c"} {
		runs[id+".json"] = &fake{id: id, resources: environmentResources(), state: durablerun.Passed, tracker: watch}
		if id != "a" {
			document += ","
		}
		document += `{"id":"` + id + `","spec":"` + id + `.json","isolation":"isolated"}`
	}
	queued(t, runs)
	report, err := queue(t, document+`]}`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if report.Executed != 3 || watch.widest != 1 {
		t.Fatalf("%d at a time: %+v", watch.widest, report)
	}
}

// A setup job is a dependency of the tests that need the state it leaves
// behind: they start after it and only if it passed.
func TestADependentJobStartsOnlyAfterTheJobItComesAfterPassed(t *testing.T) {
	watch := newTracker()
	queued(t, map[string]*fake{
		"setup.json":   {id: "setup", resources: environmentResources(), state: durablerun.Passed, tracker: watch},
		"booking.json": {id: "booking", resources: environmentResources(), state: durablerun.Passed, tracker: watch},
	})
	document := `{"schema":"readmit-run-queue/v1","parallelism":4,"jobs":[` +
		`{"id":"booking","spec":"booking.json","isolation":"isolated","after":["setup"]},` +
		`{"id":"setup","spec":"setup.json","isolation":"isolated"}]}`
	report, err := queue(t, document, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if report.Executed != 2 || report.ExitCode() != 0 {
		t.Fatalf("%+v", report)
	}
	if len(watch.order) != 4 || watch.order[0] != "enter setup" || watch.order[1] != "leave setup" || watch.order[2] != "enter booking" {
		t.Fatalf("%v", watch.order)
	}
}

// A setup that did not pass takes the work that depended on it with it, down
// the whole chain, and nothing it would have run is started.
func TestAFailedSetupSkipsEveryJobThatDependedOnIt(t *testing.T) {
	watch := newTracker()
	queued(t, map[string]*fake{
		"setup.json":   {id: "setup", resources: environmentResources(), state: durablerun.ExecutionError, tracker: watch},
		"booking.json": {id: "booking", resources: environmentResources(), state: durablerun.Passed, tracker: watch},
		"review.json":  {id: "review", resources: environmentResources(), state: durablerun.Passed, tracker: watch},
	})
	document := `{"schema":"readmit-run-queue/v1","parallelism":4,"jobs":[` +
		`{"id":"setup","spec":"setup.json","isolation":"shared"},` +
		`{"id":"booking","spec":"booking.json","isolation":"shared","after":["setup"]},` +
		`{"id":"review","spec":"review.json","isolation":"shared","after":["booking"]}]}`
	report, err := queue(t, document, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if report.Executed != 1 || report.Skipped != 2 || report.ExitCode() != 2 {
		t.Fatalf("%+v", report)
	}
	for _, id := range []string{"booking", "review"} {
		job := jobs(report)[id]
		if job.Admission != Skipped || job.Run != nil || job.Reason != "a job this one depends on did not pass; the state it was to leave behind was never established" {
			t.Fatalf("%+v", job)
		}
	}
	if len(watch.order) != 2 {
		t.Fatalf("a skipped job executed: %v", watch.order)
	}
}

// Cancelling the queue starts nothing further. The runs already in flight
// record their own stop; the queue records that it never started the rest.
func TestCancellingTheQueueStartsNothingFurther(t *testing.T) {
	watch := newTracker()
	queued(t, map[string]*fake{
		"a.json": {id: "a", resources: environmentResources(), state: durablerun.Passed, tracker: watch},
		"b.json": {id: "b", resources: environmentResources(), state: durablerun.Passed, tracker: watch},
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	document := `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[` +
		`{"id":"a","spec":"a.json","isolation":"shared"},` +
		`{"id":"b","spec":"b.json","isolation":"shared"}]}`
	report, err := Run(ctx, Request{PlanBytes: []byte(document), PlanDirectory: t.TempDir(), Runs: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if report.Skipped != 2 || report.Executed != 0 || report.ExitCode() != 2 || len(watch.order) != 0 {
		t.Fatalf("%+v %v", report, watch.order)
	}
	if jobs(report)["a"].Reason != "the queue stopped before this job started" {
		t.Fatalf("%+v", report.Jobs)
	}
}

// A durable run outside this queue still holding a lease on a resource a
// shared job declares refuses that job's admission. An isolated job declares
// its effects invisible and is admitted beside it.
func TestALeaseHeldOutsideTheQueueRefusesAdmissionToASharedJob(t *testing.T) {
	runs := t.TempDir()
	foreign := filepath.Join(runs, "manual")
	if err := os.Mkdir(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	lease := durablerun.Lease{Schema: durablerun.LeaseSchema, Holder: durablerun.Holder{PID: 1, StartedAt: time.Now().UTC()}, Resources: environmentResources()}
	raw, err := json.Marshal(lease, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "lease.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	watch := newTracker()
	queued(t, map[string]*fake{
		"a.json": {id: "a", resources: environmentResources(), state: durablerun.Passed, tracker: watch},
		"b.json": {id: "b", resources: environmentResources(), state: durablerun.Passed, tracker: watch},
	})
	document := `{"schema":"readmit-run-queue/v1","parallelism":2,"jobs":[` +
		`{"id":"a","spec":"a.json","isolation":"shared"},` +
		`{"id":"b","spec":"b.json","isolation":"isolated"}]}`
	report, err := queue(t, document, runs)
	if err != nil {
		t.Fatal(err)
	}
	if report.Refused != 1 || report.Executed != 1 || report.ExitCode() != 2 {
		t.Fatalf("%+v", report)
	}
	refused := jobs(report)["a"]
	if refused.Admission != Refused || refused.Run != nil || refused.Reason != "another durable run in this runs directory still holds "+durablerun.EnvironmentResource+" "+staging+"; readmit does not join a holder, and run clean removes a lease a stopped writer could not release" {
		t.Fatalf("%+v", refused)
	}
	if jobs(report)["b"].Admission != Executed {
		t.Fatalf("%+v", report.Jobs)
	}
	// Releasing the lease admits the same job; nothing else changed.
	if err := os.Remove(filepath.Join(foreign, "lease.json")); err != nil {
		t.Fatal(err)
	}
	report, err = queue(t, document, runs)
	if err != nil || report.Executed != 2 || report.ExitCode() != 0 {
		t.Fatalf("%v %+v", err, report)
	}
}

// A queue holding one unreadable spec sends nothing at all: every spec is read
// before the first run starts.
func TestAQueueWithAnUnreadableSpecExecutesNothing(t *testing.T) {
	watch := newTracker()
	queued(t, map[string]*fake{"a.json": {id: "a", resources: environmentResources(), state: durablerun.Passed, tracker: watch}})
	document := `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[` +
		`{"id":"a","spec":"a.json","isolation":"shared"},` +
		`{"id":"b","spec":"missing.json","isolation":"shared"}]}`
	if _, err := queue(t, document, t.TempDir()); err == nil {
		t.Fatal("a queue with an unreadable spec was scheduled")
	}
	if len(watch.order) != 0 {
		t.Fatalf("a refused queue executed: %v", watch.order)
	}
}

// Every job writes a new run directory. An existing one refuses the queue
// before anything executes rather than after part of it has.
func TestAnExistingRunDirectoryRefusesTheQueueBeforeAnythingExecutes(t *testing.T) {
	runs := t.TempDir()
	if err := os.Mkdir(filepath.Join(runs, "b"), 0700); err != nil {
		t.Fatal(err)
	}
	watch := newTracker()
	queued(t, map[string]*fake{
		"a.json": {id: "a", resources: environmentResources(), state: durablerun.Passed, tracker: watch},
		"b.json": {id: "b", resources: environmentResources(), state: durablerun.Passed, tracker: watch},
	})
	document := `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[` +
		`{"id":"a","spec":"a.json","isolation":"shared"},` +
		`{"id":"b","spec":"b.json","isolation":"shared"}]}`
	if _, err := queue(t, document, runs); err == nil {
		t.Fatal("a queue reusing a run directory was scheduled")
	}
	if len(watch.order) != 0 {
		t.Fatalf("a refused queue executed: %v", watch.order)
	}
}

// A run that could not be created at all is not reported as one that executed:
// nothing was established about a target, the reason says why, and the queue is
// never a passing one.
func TestARunThatCouldNotBeCreatedIsNotReportedAsExecuted(t *testing.T) {
	watch := newTracker()
	queued(t, map[string]*fake{"a.json": {id: "a", resources: environmentResources(), state: durablerun.Passed, tracker: watch, failure: errors.New("cannot create durable run; destination must be new")}})
	document := `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"a","spec":"a.json","isolation":"shared"}]}`
	report, err := queue(t, document, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	job := jobs(report)["a"]
	if job.Admission != StartFailed || job.Run != nil || job.Reason == "" || report.Executed != 0 || report.StartFailed != 1 || report.ExitCode() != 2 {
		t.Fatalf("%+v", report)
	}
}

// A runs directory that cannot be read is refused before any spec is read.
func TestAnUnreadableRunsDirectoryRefusesTheQueue(t *testing.T) {
	watch := newTracker()
	queued(t, map[string]*fake{"a.json": {id: "a", resources: environmentResources(), state: durablerun.Passed, tracker: watch}})
	document := `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"a","spec":"a.json","isolation":"shared"}]}`
	if _, err := queue(t, document, filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a queue against an unreadable runs directory was scheduled")
	}
	if len(watch.order) != 0 {
		t.Fatalf("a refused queue executed: %v", watch.order)
	}
}

// The invocation holds one instance for the whole queue, and the execution
// that holds it rechecks it before each new job: a term that ends while one
// job runs lets that job finish and refuses the next.
func TestAdmissionIsRecheckedForEachNewJobWithoutStoppingAnAdmittedJob(t *testing.T) {
	policy := testlicense.New(t)
	watch := newTracker()
	queued(t, map[string]*fake{
		"a.json": {id: "a", state: durablerun.Passed, tracker: watch, during: func() {
			if err := operationguard.Release(policy); err != nil {
				t.Error(err)
			}
		}},
		"b.json": {id: "b", state: durablerun.Passed, tracker: watch},
	})
	document := `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"a","spec":"a.json","isolation":"isolated"},{"id":"b","spec":"b.json","isolation":"isolated"}]}`
	var report Report
	execute := operationguard.Profile{Name: "queue", Execution: operationguard.Execute}
	err := operationguard.New(policy).Run(t.Context(), execute, func(ctx context.Context) (err error) {
		report, err = Run(ctx, Request{PlanBytes: []byte(document), PlanDirectory: t.TempDir(), Runs: t.TempDir()})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Executed != 1 || report.Refused != 1 || jobs(report)["a"].Run.State != durablerun.Passed || jobs(report)["b"].Run != nil ||
		jobs(report)["b"].Reason != entitlement.ErrReleased.Error() {
		t.Fatalf("admission did not isolate new work: %+v", report)
	}
}

// An approved queue pins every job through the declared seam before anything
// starts, and a queue whose inputs match the promotion runs.
func TestAnApprovedQueueRunsWhenEveryJobMatchesItsPin(t *testing.T) {
	watch := newTracker()
	queued(t, map[string]*fake{
		"a.json": {id: "a", resources: environmentResources(), state: durablerun.Passed, tracker: watch, identity: "pin-a"},
		"b.json": {id: "b", resources: environmentResources(), state: durablerun.Passed, tracker: watch, identity: "pin-b"},
	})
	document := `{"schema":"readmit-run-queue/v1","parallelism":2,"jobs":[` +
		`{"id":"a","spec":"a.json","isolation":"isolated"},` +
		`{"id":"b","spec":"b.json","isolation":"isolated"}]}`
	report, err := Run(t.Context(), Request{PlanBytes: []byte(document), PlanDirectory: t.TempDir(),
		Runs: t.TempDir(), ApprovedInputs: map[string]string{"a": "pin-a", "b": "pin-b"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Executed != 2 || report.ExitCode() != 0 {
		t.Fatalf("%+v", report)
	}
}

// Inputs that differ from the approved promotion are refused while every job
// is still being read, so nothing starts and nothing executes.
func TestAnApprovedQueueWhoseInputsDifferFromThePromotionRunsNothing(t *testing.T) {
	watch := newTracker()
	queued(t, map[string]*fake{
		"a.json": {id: "a", resources: environmentResources(), state: durablerun.Passed, tracker: watch, identity: "pin-a"},
		"b.json": {id: "b", resources: environmentResources(), state: durablerun.Passed, tracker: watch, identity: "pin-b"},
	})
	document := `{"schema":"readmit-run-queue/v1","parallelism":2,"jobs":[` +
		`{"id":"a","spec":"a.json","isolation":"isolated"},` +
		`{"id":"b","spec":"b.json","isolation":"isolated"}]}`
	if _, err := Run(t.Context(), Request{PlanBytes: []byte(document), PlanDirectory: t.TempDir(),
		Runs: t.TempDir(), ApprovedInputs: map[string]string{"a": "pin-a", "b": "changed"}}); err == nil {
		t.Fatal("an approved queue ran with inputs the promotion did not approve")
	}
	if len(watch.order) != 0 {
		t.Fatalf("a refused queue executed: %v", watch.order)
	}
}

// A pin that cannot be computed at all refuses the queue the same way: the
// approval cannot be checked, so nothing may run.
func TestAnApprovedQueueRefusesWhenTheIdentityCannotBeComputed(t *testing.T) {
	watch := newTracker()
	queued(t, map[string]*fake{
		"a.json": {id: "a", resources: environmentResources(), state: durablerun.Passed, tracker: watch,
			identityErr: errors.New("no credential store")},
	})
	document := `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"a","spec":"a.json","isolation":"isolated"}]}`
	if _, err := Run(t.Context(), Request{PlanBytes: []byte(document), PlanDirectory: t.TempDir(),
		Runs: t.TempDir(), ApprovedInputs: map[string]string{"a": "pin-a"}}); err == nil {
		t.Fatal("an approved queue ran with an unpinnable job")
	}
	if len(watch.order) != 0 {
		t.Fatalf("a refused queue executed: %v", watch.order)
	}
}
