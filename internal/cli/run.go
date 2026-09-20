package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/spf13/cobra"
)

func runCommand() *cobra.Command {
	command := &cobra.Command{Use: "run", Short: "Execute or recover a durable local test run"}
	var output, deadline string
	var send, asJSON bool
	start := &cobra.Command{Use: "start SPEC", Short: "Execute once into a new durable run directory", Annotations: declareInterruptible(capabilityExecute), RunE: func(cmd *cobra.Command, args []string) error {
		if !send || output == "" {
			return usage("run start requires --send and --output; existing jobs are never resumed")
		}
		ctx, cancel, err := deadlineContext(cmd.Context(), deadline)
		if err != nil {
			return err
		}
		defer cancel()
		result, err := durablerun.Start(ctx, args[0], output)
		if err != nil && result.Schema == "" {
			return &ExitError{Code: 2, Err: errors.New("durable run could not finish; inspect retained output with run status")}
		}
		return printRun(cmd, result, asJSON, err)
	}}
	start.Flags().StringVar(&output, "output", "", "New customer-local durable run directory")
	start.Flags().BoolVar(&send, "send", false, "Explicitly authorize one execution against the test target")
	start.Flags().StringVar(&deadline, "deadline", "", "Stop new sends after this duration; an in-flight delivery is reported uncertain")
	start.Flags().BoolVar(&asJSON, "json", false, "Write one versioned machine-readable summary")
	var statusJSON, recovery, pinned bool
	status := &cobra.Command{Use: "status JOB", Short: "Recover retained evidence read-only; never resend", Annotations: declare(capabilityFree), RunE: func(cmd *cobra.Command, args []string) error {
		if pinned {
			return printEngine(cmd, args[0], statusJSON)
		}
		if recovery {
			result, err := durablerun.Recover(args[0])
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			return printRecovery(cmd, result, statusJSON)
		}
		result, err := durablerun.Open(args[0])
		if err != nil {
			return &ExitError{Code: 2, Err: err}
		}
		return printRun(cmd, result, statusJSON, nil)
	}}
	status.Flags().BoolVar(&statusJSON, "json", false, "Write one versioned machine-readable summary")
	status.Flags().BoolVar(&recovery, "recovery", false, "Report what is known about every occurrence, the lease, and whether a resume would repeat only never-attempted work")
	status.Flags().BoolVar(&pinned, "engine", false, "Report the engine build, spec contract and profile the run retained, and whether this build reads them")
	status.MarkFlagsMutuallyExclusive("engine", "recovery")
	var resumeOutput, resumeDeadline string
	var resumeSend, resumeJSON bool
	resume := &cobra.Command{Use: "resume JOB SPEC", Short: "Repeat never-attempted work into a new run directory; refuses after any send", Annotations: declareInterruptible(capabilityExecute), RunE: func(cmd *cobra.Command, args []string) error {
		if !resumeSend || resumeOutput == "" {
			return usage("run resume requires --send and a new --output; the existing job is never written")
		}
		ctx, cancel, err := deadlineContext(cmd.Context(), resumeDeadline)
		if err != nil {
			return err
		}
		defer cancel()
		result, err := durablerun.Resume(ctx, args[0], args[1], resumeOutput)
		if err != nil && result.Run.Schema == "" {
			return &ExitError{Code: 2, Err: err}
		}
		return printResume(cmd, result, resumeJSON, err)
	}}
	resume.Flags().StringVar(&resumeOutput, "output", "", "New customer-local durable run directory")
	resume.Flags().BoolVar(&resumeSend, "send", false, "Explicitly authorize one execution against the test target")
	resume.Flags().StringVar(&resumeDeadline, "deadline", "", "Stop new sends after this duration; an in-flight delivery is reported uncertain")
	resume.Flags().BoolVar(&resumeJSON, "json", false, "Write one versioned machine-readable summary")
	var cleanJSON bool
	clean := &cobra.Command{Use: "clean JOB", Short: "Remove a stale lease after a recorded completion; evidence is never removed", Annotations: declare(capabilityFree), RunE: func(cmd *cobra.Command, args []string) error {
		result, err := durablerun.Clean(args[0])
		if err != nil {
			return &ExitError{Code: 2, Err: err}
		}
		return printCleanup(cmd, result, cleanJSON)
	}}
	clean.Flags().BoolVar(&cleanJSON, "json", false, "Write one versioned machine-readable summary")
	var queueRuns, queueDeadline string
	var queueSend, queueJSON bool
	queue := &cobra.Command{Use: "queue PLAN", Short: "Execute a queue of durable runs with bounded parallelism, serializing every job that shares target state", Annotations: declareInterruptible(capabilityExecute), RunE: func(cmd *cobra.Command, args []string) error {
		if !queueSend || queueRuns == "" {
			return usage("run queue requires --send and --runs naming the durable runs directory; existing jobs are never resumed")
		}
		document, err := readInputFile(args[0], runqueue.MaxPlanBytes)
		if err != nil {
			return &ExitError{Code: 2, Err: err}
		}
		// A queued job names its spec inside the queue document's own
		// directory, resolved after the document's own symlink exactly as a
		// reset plan's declared file is, so what a queue may execute is fixed
		// by where the operator put it rather than by a working directory.
		resolved, err := artifactpath.Resolve(args[0])
		if err != nil {
			return &ExitError{Code: 2, Err: errors.New("cannot resolve the run queue")}
		}
		runs, err := artifactpath.Directory(queueRuns)
		if err != nil {
			return usage("--runs must name the existing directory the durable runs are written beside")
		}
		ctx, cancel, err := deadlineContext(cmd.Context(), queueDeadline)
		if err != nil {
			return err
		}
		defer cancel()
		report, err := runqueue.Run(ctx, runqueue.Request{PlanBytes: document, PlanDirectory: filepath.Dir(resolved), Runs: runs})
		if err != nil {
			return &ExitError{Code: 2, Err: err}
		}
		return printQueue(cmd, report, queueJSON)
	}}
	queue.Flags().StringVar(&queueRuns, "runs", "", "Existing directory each queued job writes its new durable run into")
	queue.Flags().BoolVar(&queueSend, "send", false, "Explicitly authorize the queued executions against their test targets")
	queue.Flags().StringVar(&queueDeadline, "deadline", "", "Stop the whole queue after this duration; no further job starts and an in-flight delivery is reported uncertain")
	queue.Flags().BoolVar(&queueJSON, "json", false, "Write one versioned machine-readable report")
	command.AddCommand(start, status, resume, clean, queue)
	return command
}

// deadlineContext bounds a run by the explicit deadline. A deadline is a
// positive duration from now. The interrupt itself reaches the run through the
// construction pass, which gives interruptible commands a context cancelled by
// an interrupt or a termination signal.
func deadlineContext(parent context.Context, deadline string) (context.Context, context.CancelFunc, error) {
	if deadline == "" {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel, nil
	}
	duration, err := time.ParseDuration(deadline)
	if err != nil || duration <= 0 {
		return nil, nil, usage("--deadline requires a positive duration such as 30s or 5m")
	}
	ctx, cancel := context.WithTimeout(parent, duration)
	return ctx, cancel, nil
}

func writeSummary(cmd *cobra.Command, result durablerun.Summary) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Run state: %s\nStop reason: %s\nDelivery uncertain: %t\nRecorded messages: %d/%d\nContains source values: true (customer-local-only)\n", result.State, result.StopReason, result.DeliveryUncertain, result.Recorded, result.Planned)
	if err == nil && result.Recovered {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "Completion was not recorded; the writer may still be active. Recovery never sends or resumes.")
	}
	return err
}

// printRun writes the summary, then reports a storage failure the run stopped
// on, so a full disk is named rather than folded into the run's state.
func printRun(cmd *cobra.Command, result durablerun.Summary, asJSON bool, problem error) error {
	err := writeReport(cmd, asJSON, result, func(c *cobra.Command) error { return writeSummary(c, result) }, "cannot write durable run summary")
	return runStatus(result, err, problem)
}

func runStatus(result durablerun.Summary, err, problem error) error {
	if err != nil {
		return &ExitError{Code: 2, Err: err}
	}
	if problem != nil {
		return &ExitError{Code: 2, Err: problem}
	}
	if result.ExitCode() != 0 {
		return &ExitError{Code: result.ExitCode(), Err: errors.New("run did not pass; inspect retained evidence"), Reported: true}
	}
	return nil
}

// printEngine reports the pin a job retained whether or not this build reads
// it, then exits 2 when it does not, so an operator sees the versions before
// the refusal rather than only the refusal. The machine form is the retained
// document's own encoding, not a second one that happens to agree with it.
func printEngine(cmd *cobra.Command, job string, asJSON bool) error {
	pin, err := durablerun.Engine(job)
	if err != nil {
		return &ExitError{Code: 2, Err: err}
	}
	supported := pin.Supported()
	if asJSON {
		var document []byte
		if document, err = engine.Encode(pin); err == nil {
			_, err = cmd.OutOrStdout().Write(document)
		}
	} else {
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Engine build: %s\nSpec contract: %s\nProfile: %s\nRead by this build: %t\n", pin.Engine, pin.Spec, pin.Profile, supported == nil)
	}
	if err != nil {
		return &ExitError{Code: 2, Err: errors.New("cannot write durable run engine pin")}
	}
	if supported != nil {
		return &ExitError{Code: 2, Err: errors.New("the durable run was evaluated under a version this release does not read")}
	}
	return nil
}

func printRecovery(cmd *cobra.Command, result durablerun.Recovery, asJSON bool) error {
	err := writeReport(cmd, asJSON, result, func(c *cobra.Command) error { return writeRecoverySummary(c, result) }, "cannot write durable run recovery")
	return runStatus(result.Run, err, nil)
}

func writeRecoverySummary(cmd *cobra.Command, result durablerun.Recovery) error {
	if err := writeSummary(cmd, result.Run); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Completion recorded: %t\nOccurrences: %d acknowledged, %d uncertain, %d not attempted\nLease: %s\nSafe to repeat: %t\n", result.Terminal, result.Acknowledged, result.Uncertain, result.NotAttempted, result.Lease, result.SafeToRepeat); err != nil {
		return err
	}
	if result.ResumeRefusal != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Resume refused: %s\n", result.ResumeRefusal); err != nil {
			return err
		}
	}
	return nil
}

func printResume(cmd *cobra.Command, result durablerun.Resumption, asJSON bool, problem error) error {
	err := writeReport(cmd, asJSON, result, func(c *cobra.Command) error { return writeResumeSummary(c, result) }, "cannot write durable run resume report")
	return runStatus(result.Run, err, problem)
}

func writeResumeSummary(cmd *cobra.Command, result durablerun.Resumption) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Resumed from: %s\nRepeated never-attempted occurrences: %d\n", result.ResumedFrom, result.Repeated); err != nil {
		return err
	}
	return writeSummary(cmd, result.Run)
}

func printCleanup(cmd *cobra.Command, result durablerun.Cleanup, asJSON bool) error {
	return writeReport(cmd, asJSON, result, func(c *cobra.Command) error { return writeCleanupSummary(c, result) }, "cannot write durable run cleanup")
}

func writeCleanupSummary(cmd *cobra.Command, result durablerun.Cleanup) error {
	removed := "nothing"
	if len(result.Removed) > 0 {
		removed = strings.Join(result.Removed, ", ")
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Run state: %s\nRemoved: %s\nRetained: %s\n", result.Run.State, removed, strings.Join(result.Retained, ", "))
	return err
}

// printQueue writes the queue's own report. A job's line names the queue's
// decision, the state of the run when one executed, and the resource the job
// waited for, so serialization is visible rather than assumed.
func printQueue(cmd *cobra.Command, result runqueue.Report, asJSON bool) error {
	if err := writeReport(cmd, asJSON, result, func(c *cobra.Command) error { return writeQueueSummary(c, result) }, "cannot write the durable run queue report"); err != nil {
		return err
	}
	if result.ExitCode() != 0 {
		return &ExitError{Code: result.ExitCode(), Err: errors.New("the run queue did not pass; inspect each retained run"), Reported: true}
	}
	return nil
}

func writeQueueSummary(cmd *cobra.Command, result runqueue.Report) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Queue parallelism: %d\nJobs: %d executed, %d not started, %d refused, %d skipped\n", result.Parallelism, result.Executed, result.StartFailed, result.Refused, result.Skipped)
	for _, job := range result.Jobs {
		if err != nil {
			break
		}
		line := job.ID + ": " + string(job.Admission)
		if job.Run != nil {
			line += " (" + string(job.Run.State) + ")"
		}
		if job.WaitedFor != "" {
			line += ", waited for " + job.WaitedFor
		}
		if job.Reason != "" {
			line += ": " + job.Reason
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), line)
	}
	return err
}
