package suite

import (
	"time"

	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/runresult"
)

// Execution is one retained suite execution as its folder holds it: the
// identity of the suite document it executed, that document, the environment
// it was expanded for, its queue and, once the queue stopped, the queue's
// report. Report is nil for an execution that retained none, which was
// interrupted: it is never read as one that completed.
type Execution struct {
	Identity    string
	Suite       Document
	Environment string
	Queue       runqueue.Plan
	Report      *runqueue.Report
	Jobs        []ExecutedJob
}

// ExecutedJob is one job's retained run: when it started and completed, as
// its own run records it (zero when it records neither), and whether a
// delivery no acknowledgement settled.
type ExecutedJob struct {
	ID                string
	StartedAt         time.Time
	CompletedAt       time.Time
	DeliveryUncertain bool
}

// OpenExecution reads one retained suite folder, checking its suite, selection,
// queue and report against each other as coverage does, and each job's
// retained run through its own reader. It writes nothing.
func OpenExecution(dir string) (Execution, error) {
	retained, err := loadRetainedSuite(dir, func(string) bool { return true })
	if err != nil {
		return Execution{}, err
	}
	execution := Execution{Identity: retained.identity, Suite: retained.document, Environment: retained.selection.Environment,
		Queue: retained.queue, Report: retained.report, Jobs: []ExecutedJob{}}
	for _, job := range retained.queue.Jobs {
		path, present, err := retained.retainedJob(job.ID)
		if err != nil {
			return Execution{}, err
		}
		if !present {
			continue
		}
		opened, err := runresult.Open(path)
		if err != nil {
			return Execution{}, err
		}
		executed := ExecutedJob{ID: job.ID, DeliveryUncertain: opened.Lifecycle.DeliveryUncertain}
		if opened.Run != nil {
			executed.StartedAt, executed.CompletedAt = opened.Run.Manifest.StartedAt, opened.Run.Manifest.CompletedAt
		}
		execution.Jobs = append(execution.Jobs, executed)
	}
	return execution, nil
}
