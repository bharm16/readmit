package runqueue

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/durablerun"
)

// ReportSchema is what one queue established: for every job it was given, the
// queue's own decision, what the job declared, what it waited for, and the
// run's own summary when one executed. It is a separate document beside the
// readmit-job/v1 summaries it carries, which gain no member.
const ReportSchema = "readmit-run-queue-report/v1"

// Admission is the queue's own decision about one job. It is deliberately not
// a run state: a run says what happened once it existed, and these four say
// what the queue did about the job before that.
type Admission string

const (
	// Executed means the run was created and its own summary says what
	// happened, whatever that was.
	Executed Admission = "executed"
	// StartFailed means the queue admitted the job and the run could not be
	// created at all, so nothing was established about a target. The reason
	// says why and there is no summary.
	StartFailed Admission = "start_failed"
	// Refused means admission was refused because something outside this queue
	// may still be using a resource the job declares. Nothing executed.
	Refused Admission = "refused"
	// Skipped means the queue decided not to start the job: the queue stopped,
	// or a job it comes after did not pass.
	Skipped Admission = "skipped"
)

// JobReport is what the queue decided about one job and what came back.
type JobReport struct {
	ID        string                `json:"id"`
	Admission Admission             `json:"admission"`
	Isolation Isolation             `json:"isolation"`
	Resources []durablerun.Resource `json:"resources"`
	// WaitedFor names a resource this job was held back by at least once. It
	// is how a queue shows that it serialized rather than that it happened to.
	WaitedFor string              `json:"waited_for,omitzero"`
	Reason    string              `json:"reason,omitzero"`
	Run       *durablerun.Summary `json:"run,omitzero"`
}

// Report is one queue's visible state after it stopped.
type Report struct {
	Schema      string      `json:"schema"`
	Parallelism int         `json:"parallelism"`
	Jobs        []JobReport `json:"jobs"`
	Executed    int         `json:"executed"`
	StartFailed int         `json:"start_failed"`
	Refused     int         `json:"refused"`
	Skipped     int         `json:"skipped"`
}

// ExitCode is 0 only when the queue ran every job it was given and each one
// passed. A job that was refused, skipped or never created is never a passing
// queue, however the runs that did execute ended.
func (r Report) ExitCode() int {
	code := 0
	for _, job := range r.Jobs {
		if job.Admission != Executed || job.Run == nil {
			return 2
		}
		if job.Run.ExitCode() > code {
			code = job.Run.ExitCode()
		}
	}
	return code
}

// Request is one queue execution: the selected document, the directory it was
// selected from, and the durable runs directory every job writes a new run into.
type Request struct {
	PlanBytes     []byte
	PlanDirectory string
	Runs          string
	// ApprovedInputs, when present, must pin every job before any is started.
	ApprovedInputs map[string]string
}

// runner is what the queue needs from one durable run: what it will claim, and
// executing it. It is the one seam the scheduler's own tests take.
type runner interface {
	Resources() []durablerun.Resource
	Start(ctx context.Context, output string) (durablerun.Summary, error)
}

var prepare = func(specPath string) (runner, error) {
	prepared, err := durablerun.Prepare(specPath)
	if err != nil {
		return nil, err
	}
	return prepared, nil
}

type state struct {
	job    Job
	runner runner
	output string
	report JobReport
	// done is set when nothing more will happen to this job, whether it ran,
	// was refused or was skipped. passed is set only by a run that passed.
	done    bool
	passed  bool
	started bool
	after   []*state
}

func (s *state) pending() bool { return !s.done && !s.started }

func (s *state) stop(admission Admission, reason string) {
	s.report.Admission = admission
	s.report.Reason = reason
	s.done = true
}

// ready reports that every job this one comes after has finished, whatever it
// finished as. A dependency that finished without passing has already stopped
// this job before ready is consulted.
func (s *state) ready() bool {
	for _, after := range s.after {
		if !after.done {
			return false
		}
	}
	return true
}

// keys are the resources this job holds while it runs. An isolated job holds
// nothing: the operator declared its effects invisible to the other jobs on
// the same environment, and the queue cannot verify that declaration any more
// than it can verify a recorded nonproduction classification.
func (s *state) keys() []string {
	if s.job.Isolation != SharedState {
		return nil
	}
	held := make([]string, 0, len(s.report.Resources))
	for _, resource := range s.report.Resources {
		held = append(held, key(resource))
	}
	return held
}

func key(r durablerun.Resource) string   { return r.Kind + "\x00" + r.Name }
func label(r durablerun.Resource) string { return r.Kind + " " + r.Name }

// Run executes one queue. Every spec it names is read before anything executes,
// so a queue holding one unreadable spec sends nothing at all. A run starts
// only when every job it declares it comes after has passed and no other job in
// the queue holds a resource it declares; a cancelled queue starts nothing
// further and the runs already in flight record their own cancellation.
func Run(ctx context.Context, request Request) (Report, error) {
	plan, err := DecodePlan(request.PlanBytes)
	if err != nil {
		return Report{}, err
	}
	// A durable runs directory that cannot be read is refused before a single
	// job is prepared, rather than as each admission reaches it.
	if _, err := durablerun.Claims(request.Runs, nil); err != nil {
		return Report{}, err
	}
	queue, err := newSchedule(plan, request)
	if err != nil {
		return Report{}, err
	}
	queue.run(ctx, plan.Parallelism)
	report := Report{Schema: ReportSchema, Parallelism: plan.Parallelism, Jobs: make([]JobReport, len(queue.states))}
	for index, current := range queue.states {
		// Every job reaches one of the four decisions; saying so here rather
		// than assuming it keeps a job that somehow reached none from being
		// tallied as a skip nobody can explain.
		if current.report.Admission == "" {
			current.stop(Skipped, "the queue stopped before this job started")
		}
		report.Jobs[index] = current.report
		switch current.report.Admission {
		case Executed:
			report.Executed++
		case StartFailed:
			report.StartFailed++
		case Refused:
			report.Refused++
		default:
			report.Skipped++
		}
	}
	return report, nil
}

// schedule is one queue in flight: the jobs, the resources they hold while
// they run, and the runs directory admission is read against. Every scheduling
// decision is made on one goroutine, so all of it is read and written in
// exactly one place.
type schedule struct {
	states []*state
	held   map[string]string
	runs   string
	// owned is the ids of the jobs this queue is running. Their leases are
	// never read for admission: their resources are already decided here, and
	// a lease a job of ours is writing at this moment would be refused as
	// changed evidence.
	owned map[string]bool
}

// newSchedule reads every spec before anything executes, so a queue holding
// one unreadable spec or one run directory that already exists sends nothing
// at all rather than part of itself.
func newSchedule(plan Plan, request Request) (*schedule, error) {
	if request.ApprovedInputs != nil && len(request.ApprovedInputs) != len(plan.Jobs) {
		return nil, errors.New("approved queue must pin every job")
	}
	queue := &schedule{states: make([]*state, len(plan.Jobs)), held: map[string]string{},
		runs: request.Runs, owned: make(map[string]bool, len(plan.Jobs))}
	byID := make(map[string]*state, len(plan.Jobs))
	for index, job := range plan.Jobs {
		output := filepath.Join(request.Runs, job.ID)
		if _, err := os.Lstat(output); err == nil {
			return nil, errors.New("a queued job's run directory already exists; every job writes a new one")
		}
		prepared, err := prepare(filepath.Join(request.PlanDirectory, job.Spec))
		if err != nil {
			return nil, err
		}
		if request.ApprovedInputs != nil {
			sealed, ok := prepared.(interface{ InputIdentity() (string, error) })
			if !ok {
				return nil, errors.New("queue cannot verify prepared inputs")
			}
			identity, err := sealed.InputIdentity()
			if err != nil || identity != request.ApprovedInputs[job.ID] {
				return nil, errors.New("queued inputs differ from approved promotion")
			}
		}
		current := &state{job: job, runner: prepared, output: output,
			report: JobReport{ID: job.ID, Isolation: job.Isolation, Resources: prepared.Resources()}}
		queue.states[index] = current
		byID[job.ID] = current
		queue.owned[job.ID] = true
	}
	// The declared order is resolved once, into the jobs themselves. Every id
	// is known to exist and the order to have a beginning: DecodePlan refused
	// the queue otherwise.
	for _, current := range queue.states {
		for _, after := range current.job.After {
			current.after = append(current.after, byID[after])
		}
	}
	return queue, nil
}

// run owns every scheduling decision on one goroutine. A run is started on its
// own goroutine and reports back over one channel, so a run can never be
// started against a decision already stale.
func (q *schedule) run(ctx context.Context, parallelism int) {
	finished := make(chan int)
	running := 0
	for {
		for running < parallelism {
			index := q.admit(ctx)
			if index < 0 {
				break
			}
			current := q.states[index]
			if holder, err := q.conflict(current); err != nil {
				current.stop(Refused, err.Error())
				continue
			} else if holder != "" {
				current.stop(Refused, "another durable run in this runs directory still holds "+holder+"; readmit does not join a holder, and run clean removes a lease a stopped writer could not release")
				continue
			}
			for _, resource := range current.keys() {
				q.held[resource] = current.job.ID
			}
			current.started = true
			running++
			go func() {
				summary, err := current.runner.Start(ctx, current.output)
				current.report.Admission = Executed
				if err != nil {
					current.report.Reason = err.Error()
				}
				if summary.Schema == "" {
					current.report.Admission = StartFailed
				} else {
					current.report.Run = &summary
					current.passed = summary.State == durablerun.Passed
				}
				finished <- index
			}()
		}
		if running == 0 {
			return
		}
		current := q.states[<-finished]
		running--
		for _, resource := range current.keys() {
			delete(q.held, resource)
		}
		current.done = true
	}
}

// admit reports the next job that may start now, or -1 when none may. It first
// resolves, to a fixed point, every pending job whose dependencies can no
// longer be met, so a setup job that did not pass takes the tests that needed
// its state with it rather than letting them run against state nobody
// established.
func (q *schedule) admit(ctx context.Context) int {
	if ctx.Err() != nil {
		for _, current := range q.states {
			if current.pending() {
				current.stop(Skipped, "the queue stopped before this job started")
			}
		}
		return -1
	}
	for changed := true; changed; {
		changed = false
		for _, current := range q.states {
			if !current.pending() {
				continue
			}
			for _, after := range current.after {
				if after.done && !after.passed {
					current.stop(Skipped, "a job this one depends on did not pass; the state it was to leave behind was never established")
					changed = true
					break
				}
			}
		}
	}
	for index, current := range q.states {
		if !current.pending() || !current.ready() {
			continue
		}
		waiting := ""
		for _, resource := range current.keys() {
			if _, taken := q.held[resource]; taken {
				waiting = resource
				break
			}
		}
		if waiting == "" {
			return index
		}
		for _, resource := range current.report.Resources {
			if key(resource) == waiting {
				current.report.WaitedFor = label(resource)
			}
		}
	}
	return -1
}

// conflict reports a resource this job declares that a durable run outside this
// queue still holds a lease on. A run somebody started by hand into the same
// runs directory is exactly what this read is for.
//
// It is a read, not a filesystem lock. Two schedulers started against one runs
// directory at the same instant can each admit the same environment, because
// each reads the leases before either has written one. One runs directory is
// one queue's, and a lease outside it stays what durable runs have always
// documented it as: a statement, not a lock.
func (q *schedule) conflict(current *state) (string, error) {
	if current.job.Isolation != SharedState {
		return "", nil
	}
	claims, err := durablerun.Claims(q.runs, q.owned)
	if err != nil {
		return "", err
	}
	declared := map[string]durablerun.Resource{}
	for _, resource := range current.report.Resources {
		declared[key(resource)] = resource
	}
	for _, claim := range claims {
		for _, resource := range claim.Resources {
			if found, taken := declared[key(resource)]; taken {
				return label(found), nil
			}
		}
	}
	return "", nil
}
