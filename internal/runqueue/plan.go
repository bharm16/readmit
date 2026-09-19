// Package runqueue executes an explicitly selected queue of durable runs with
// bounded parallelism and admits each one against the resources it declares.
//
// The hazard this package exists for is two runs changing one target's fixture
// state at the same time. A queue is data, exactly as a test spec is: it names
// jobs, the order between them and, for each job, whether that job shares the
// mutable state of the environment it sends to. Nothing in the document carries
// a command, a script or an expression, so a queue somebody imported cannot
// make readmit execute anything beyond the specs it names (ADR-0003).
//
// Sharing is the reading a queue gets when it says nothing useful: a job
// declares shared or isolated, and only an explicit isolated declaration lets
// two jobs on one environment run at the same time. readmit cannot verify that
// declaration any more than it can verify that an endpoint labelled
// nonproduction is safe to send to; it is what an operator wrote down, and the
// queue serializes everything else.
package runqueue

import (
	"encoding/json/v2"
	"errors"
	"path/filepath"
)

// PlanSchema is the contract an explicitly selected queue declares.
const PlanSchema = "readmit-run-queue/v1"

const (
	// MaxPlanBytes bounds the document a command reads before decoding it.
	MaxPlanBytes = 64 << 10
	// maxJobs bounds one queue. A queue nobody can read through is not a
	// reviewed one, and an unbounded list is refused rather than truncated.
	maxJobs = 64
	// maxParallelism bounds how many runs one queue may have in flight. The
	// bound is the queue's, not the machine's: a person declares what their
	// environments tolerate, and every isolated job still occupies a slot.
	maxParallelism = 16
)

// Isolation is what a job declares about the target state it changes.
type Isolation string

const (
	// SharedState means the job changes state other jobs on the same
	// environment can see. It holds every resource it declares for the whole
	// run, and no other shared job on those resources starts meanwhile.
	SharedState Isolation = "shared"
	// IsolatedState means the operator declares this job's effects invisible
	// to every other job on the same environment. It holds nothing, so it may
	// run beside any other job within the queue's parallelism.
	IsolatedState Isolation = "isolated"
)

// Plan is the queue an operator selected explicitly. There is no discovered
// queue and no default one.
type Plan struct {
	Schema string `json:"schema"`
	// Parallelism is how many runs this queue may have in flight at once.
	Parallelism int   `json:"parallelism"`
	Jobs        []Job `json:"jobs"`
}

// Job is one durable run the queue starts. Spec is one test spec inside the
// queue document's own directory; After names the jobs that must have passed
// before this one starts, which is how a setup job is made a dependency of the
// tests that need the state it leaves behind.
type Job struct {
	ID        string    `json:"id"`
	Spec      string    `json:"spec"`
	Isolation Isolation `json:"isolation"`
	After     []string  `json:"after,omitzero"`
}

// UnmarshalJSON reads one job exactly as written. Presence is checked first and
// the same bytes are then re-read rejecting unknown members, so an omitted
// isolation declaration is refused as omitted rather than read as a zero value,
// and a member this contract never declared is refused rather than ignored.
func (j *Job) UnmarshalJSON(data []byte) error {
	var required struct {
		ID        *string    `json:"id"`
		Spec      *string    `json:"spec"`
		Isolation *Isolation `json:"isolation"`
		After     *[]string  `json:"after"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.ID == nil || required.Spec == nil || required.Isolation == nil {
		return errors.New("a queued job requires id, spec and isolation")
	}
	type job Job
	var decoded job
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a queued job declares no member beyond id, spec, isolation and after")
	}
	*j = Job(decoded)
	return nil
}

// DecodePlan reads one queue exactly as written. Unknown members are refused,
// so a queue authored against a later contract is never read as though this one
// had always allowed it.
func DecodePlan(data []byte) (Plan, error) {
	if len(data) > MaxPlanBytes {
		return Plan{}, errors.New("a run queue exceeds its size limit")
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan, json.RejectUnknownMembers(true)); err != nil {
		return Plan{}, errors.New("invalid run queue JSON")
	}
	if err := validate(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func validate(plan Plan) error {
	if plan.Schema != PlanSchema {
		return errors.New("a run queue must declare " + PlanSchema)
	}
	if plan.Parallelism < 1 || plan.Parallelism > maxParallelism {
		return errors.New("a run queue declares a parallelism between 1 and 16")
	}
	if len(plan.Jobs) == 0 || len(plan.Jobs) > maxJobs {
		return errors.New("a run queue declares between 1 and 64 jobs")
	}
	declared := make(map[string]bool, len(plan.Jobs))
	for _, job := range plan.Jobs {
		if err := jobID(job.ID); err != nil {
			return err
		}
		if declared[job.ID] {
			return errors.New("a queued job id is declared twice")
		}
		declared[job.ID] = true
		if job.Isolation != SharedState && job.Isolation != IsolatedState {
			return errors.New("a queued job declares isolation as shared or isolated; state isolation is never assumed")
		}
		// A job names its spec inside the queue document's own directory.
		// readmit opens it within that directory rather than by the name
		// alone, so a queue cannot point a run at a file elsewhere.
		if !filepath.IsLocal(job.Spec) {
			return errors.New("a queued job names one test spec inside the queue's own directory")
		}
	}
	// Dependencies are resolved against the declared ids before anything runs.
	// A queue that names a job which does not exist, or that can never reach a
	// job because the order it declares has no beginning, is refused whole.
	if err := order(plan.Jobs, declared); err != nil {
		return err
	}
	return nil
}

// order refuses a dependency on an undeclared job, a job that depends on
// itself, and any cycle. It walks the declared jobs repeatedly, retiring the
// ones whose dependencies are already retired; a pass that retires nothing
// while jobs remain is a cycle.
func order(jobs []Job, declared map[string]bool) error {
	for _, job := range jobs {
		seen := map[string]bool{}
		for _, after := range job.After {
			if !declared[after] {
				return errors.New("a queued job depends on a job this queue does not declare")
			}
			if after == job.ID {
				return errors.New("a queued job depends on itself")
			}
			if seen[after] {
				return errors.New("a queued job declares one dependency twice")
			}
			seen[after] = true
		}
	}
	retired := make(map[string]bool, len(jobs))
	for len(retired) < len(jobs) {
		progressed := false
		for _, job := range jobs {
			if retired[job.ID] {
				continue
			}
			ready := true
			for _, after := range job.After {
				ready = ready && retired[after]
			}
			if ready {
				retired[job.ID] = true
				progressed = true
			}
		}
		if !progressed {
			return errors.New("a run queue declares a dependency cycle; no job in it could ever start")
		}
	}
	return nil
}

// jobID bounds the one name a person reads in the queue's report, and which
// names the run's own output directory beside the other jobs. It is the same
// shape a reset action id and a spec assertion id require, so a person reading
// any of those documents is reading one convention.
func jobID(id string) error {
	if id == "" || len(id) > 64 || id[0] < 'a' || id[0] > 'z' {
		return errors.New("a queued job id begins with a lowercase letter and is at most 64 bytes")
	}
	for _, r := range id {
		if r != '-' && (r < '0' || r > '9') && (r < 'a' || r > 'z') {
			return errors.New("a queued job id holds lowercase letters, digits and '-' only")
		}
	}
	return nil
}
