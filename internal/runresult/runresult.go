// Package runresult names and opens retained evidence directories: cases,
// runs, test results and durable run directories. Consumers no longer probe
// engine.json or result.json, join result paths, compute target identities,
// or restate lifecycle usability and identity checks.
package runresult

import (
	"errors"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

type Result struct {
	Path       string
	ResultPath string
	Durable    bool
	Lifecycle  durablerun.Summary
	// Pin is the engine pin a durable run retained.
	Pin        *Pin
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
	if ExecutionFamily(directory, AnyEntry) == JobFamily {
		opened.Durable = true
		opened.Lifecycle, err = durablerun.Open(directory)
		if err != nil {
			return nil, err
		}
		if opened.Pin, err = readPin(directory); err != nil {
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
	opened.ResultPath = resultPath
	opened.Run = opened.Artifact.Run
	opened.Assertions = slices.Clone(opened.Artifact.Result.Assertions)
	return opened, nil
}

// OpenWorkspace verifies one retained execution named by a single workspace
// entry. It accepts a direct result or a durable job through the same readers,
// while refusing path traversal and symbolic links before either reader runs.
func OpenWorkspace(root, entry string) (*Result, error) {
	path, err := artifactpath.Child(root, entry)
	if err != nil {
		return nil, err
	}
	return Open(path)
}

// NoRunState is what a report or comparison records when the execution it
// describes was not a durable run and so carries no durable state. It is a
// view convention, not a run state: nothing executed or retained ever has it.
const NoRunState = "not_recorded"

// Usable is the one lifecycle policy for consumers that require a certain,
// finalized result. Open remains useful for explaining incomplete jobs.
func (r Result) Usable() (bool, durablerun.Usability) {
	if !r.Durable {
		if r.Artifact == nil {
			return false, durablerun.UsabilityNoResult
		}
		return true, durablerun.UsableResult
	}
	if r.Artifact == nil {
		return false, durablerun.UsabilityNoResult
	}
	usability := r.Lifecycle.Usability()
	return usability == durablerun.UsableResult, usability
}

// UsableLifecycle is the common policy for retained summaries that no longer
// sit beside their original directory, such as portable reports. RunState is
// the recorded view convention: NoRunState for an execution that was not a
// durable run, or the summary state it recorded.
func UsableLifecycle(state string, journalIncomplete, deliveryUncertain bool) (bool, durablerun.Usability) {
	if journalIncomplete {
		return false, durablerun.UsabilityJournalIncomplete
	}
	if deliveryUncertain {
		return false, durablerun.UsabilityDeliveryUncertain
	}
	if state != NoRunState && !durablerun.State(state).Decided() {
		return false, durablerun.UsabilityUndecided
	}
	return true, durablerun.UsableResult
}
