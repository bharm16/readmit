package desktop

import (
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/suite"
)

// This file reads the retained executions of the project's suites. A suite
// itself is a named object of the project (suite_items.go), its versions are
// read and compiled in suite_versions.go, and every execution, preview,
// preparation, coverage assessment and promotion goes through the same
// internal/suite readers and writers `readmit suite` uses: the window adds no
// second suite language.

// suiteRunView is one retained suite execution: the identity and fingerprint
// of the suite document it executed, that suite's own id and document, the
// suite environment it ran against, where it is and what it did.
type suiteRunView struct {
	id, identity, suite, outcome string
	fingerprint, environment     string
	dir                          string
	document                     suite.Document
	started, completed           time.Time
	jobs, uncertain              int
}

// suiteExecutions reads every retained suite execution of the project once
// per load.
func (c *loadedCatalog) suiteExecutions() []suiteRunView {
	if c.suitesRead {
		return c.suiteRuns
	}
	c.suitesRead = true
	for _, item := range c.document.Items {
		if item.Kind != string(RunItem) || item.Entry == "" || c.removed(item) {
			continue
		}
		path := filepath.Join(c.root, item.Entry)
		if !regular(filepath.Join(path, "suite.json")) {
			continue
		}
		if run, err := suiteExecution(item.ID, path); err == nil {
			c.suiteRuns = append(c.suiteRuns, run)
		}
	}
	return c.suiteRuns
}

// suiteExecution reads one retained suite execution through the suite's own
// reader. An execution without its queue report was interrupted; one whose
// queue did not execute every job was stopped.
func suiteExecution(id, path string) (suiteRunView, error) {
	execution, err := suite.OpenExecution(path)
	if err != nil {
		return suiteRunView{}, err
	}
	run := suiteRunView{id: id, identity: execution.Identity, suite: execution.Suite.ID, jobs: len(execution.Queue.Jobs), outcome: "incomplete",
		fingerprint: suiteFingerprint(execution.Suite), environment: execution.Environment, dir: path, document: execution.Suite}
	if execution.Report != nil {
		run.outcome = "stopped"
		if execution.Report.Executed == len(execution.Report.Jobs) {
			run.outcome = "executed"
		}
	}
	for _, job := range execution.Jobs {
		if !job.StartedAt.IsZero() && (run.started.IsZero() || job.StartedAt.Before(run.started)) {
			run.started = job.StartedAt
		}
		if job.CompletedAt.After(run.completed) {
			run.completed = job.CompletedAt
		}
		if job.DeliveryUncertain {
			run.uncertain++
		}
	}
	return run, nil
}
