// Package runresult opens and classifies retained test results and durable run
// directories. Consumers no longer probe engine.json, join result paths, or
// restate lifecycle usability and identity checks.
package runresult

import (
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

type Result struct {
	Path       string
	Durable    bool
	Lifecycle  durablerun.Summary
	Artifact   *testrunner.Artifact
	Spec       *testrunner.Spec
	Run        *replay.Run
	Assertions []testrunner.AssertionResult
}

// Open verifies one direct result or durable job. A durable job that has no
// finalized result is still a valid lifecycle record and returns no Artifact.
func Open(path string) (*Result, error) {
	directory, err := artifactpath.Directory(path)
	if err != nil {
		return nil, errors.New("execution must be a readable result or durable run directory")
	}
	opened := &Result{Path: directory}
	resultPath := directory
	if _, err := os.Lstat(filepath.Join(directory, "engine.json")); err == nil {
		opened.Durable = true
		opened.Lifecycle, err = durablerun.Open(directory)
		if err != nil {
			return nil, err
		}
		if opened.Lifecycle.ResultIdentity == "" {
			return opened, nil
		}
		resultPath, err = artifactpath.Child(directory, "result")
		if err != nil {
			return nil, err
		}
	}
	opened.Artifact, err = testrunner.Open(resultPath)
	if err != nil {
		return nil, err
	}
	if opened.Durable && opened.Artifact.Identity != opened.Lifecycle.ResultIdentity {
		return nil, errors.New("durable run result identity does not match its lifecycle")
	}
	opened.Spec = opened.Artifact.Spec
	opened.Run = opened.Artifact.Run
	opened.Assertions = slices.Clone(opened.Artifact.Result.Assertions)
	return opened, nil
}

// Usable is the one lifecycle policy for consumers that require a certain,
// finalized result. Open remains useful for explaining incomplete jobs.
func (r Result) Usable() (bool, string) {
	if !r.Durable {
		if r.Artifact == nil {
			return false, "no finalized result"
		}
		return true, ""
	}
	if r.Artifact == nil || r.Lifecycle.ResultIdentity == "" {
		return false, "no finalized result"
	}
	return UsableLifecycle(string(r.Lifecycle.State), r.Lifecycle.JournalIncomplete, r.Lifecycle.DeliveryUncertain)
}

// UsableLifecycle is the common policy for retained summaries that no longer
// sit beside their original directory, such as portable reports.
func UsableLifecycle(state string, journalIncomplete, deliveryUncertain bool) (bool, string) {
	if journalIncomplete {
		return false, "journal incomplete"
	}
	if deliveryUncertain {
		return false, "delivery uncertain"
	}
	if state != "not_recorded" && state != string(durablerun.Passed) && state != string(durablerun.AssertionFailed) && state != string(durablerun.ExecutionError) {
		return false, "run did not reach a usable terminal state"
	}
	return true, ""
}
