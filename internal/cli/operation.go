package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bharm16/readmit/internal/suite"
	"io"

	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/spf13/cobra"
)

// The interruptible annotation declares that a command executes work a person
// can stop: the construction pass gives its RunE a context cancelled by an
// interrupt or a termination signal. A command without it runs to completion,
// and its context answers nothing.
const interruptibleAnnotation = "readmit.dev/interruptible"

// helpTopicAnnotation marks the shell's own help-lookup command, whose refusal
// is reported before any operation starts.
const helpTopicAnnotation = "readmit.dev/help-topic"

func interruptible(cmd *cobra.Command) bool {
	return cmd.Annotations[interruptibleAnnotation] == "true"
}

// exactArgs derives arg-count validation from the command's Use line: one
// uppercase placeholder per required argument. A command whose Use carries no
// placeholders takes none. A command with a shape this cannot express declares
// its own Args, and the construction pass leaves it alone.
func exactArgs(command *cobra.Command) cobra.PositionalArgs {
	return cobra.ExactArgs(placeholderCount(command.Use))
}

// placeholderCount counts the argument placeholders a Use line declares: the
// fields after the command name that are entirely uppercase.
func placeholderCount(use string) int {
	count := 0
	for _, field := range strings.Fields(use)[1:] {
		if field == strings.ToUpper(field) && strings.ContainsFunc(field, func(r rune) bool { return r >= 'A' && r <= 'Z' }) {
			count++
		}
	}
	return count
}

// wireOperations wraps the actual operation, ensuring admission is released on
// every return, including failures. Help, parsing, and retained reads stay free.
// It is also the one construction pass over the finished command tree: the
// started mark is set here, once, the moment a command's own RunE begins —
// never inside the ~110 command bodies — and arg-count validation is derived
// from each command's Use when the command declares none of its own. Each
// command's admission is the profile its annotations declare, taken through
// the one admitted execution the operation guard owns; no command is told
// apart by its path.
func wireOperations(root *cobra.Command, policy *string, ran *bool) {
	var guard *operationguard.Guard
	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		if run := command.RunE; run != nil {
			if command.Args == nil {
				command.Args = exactArgs(command)
			}
			command.RunE = func(cmd *cobra.Command, args []string) error {
				// The help topic lookup is not a started operation: its refusal
				// is reported the way every pre-execution refusal is reported.
				if cmd.Annotations[helpTopicAnnotation] != "true" {
					*ran = true
				}
				if guard == nil {
					guard = operationguard.New(operationPolicyPath(*policy))
				}
				profile, declared := commandProfile(cmd)
				if profile.Interruptible {
					ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
					defer stop()
					cmd.SetContext(ctx)
				}
				if !declared {
					// A newly registered operation that declares nothing is
					// refused closed rather than run free.
					_, err := guard.AdmitContext(cmd.Context(), operationCapability(cmd))
					return err
				}
				work := func(ctx context.Context) error {
					cmd.SetContext(ctx)
					return run(cmd, args)
				}
				if cmd.Annotations[ciSummaryAnnotation] == "true" {
					return runCISummary(cmd, guard, profile, work)
				}
				return guard.Run(cmd.Context(), profile, work)
			}
		}
		for _, child := range command.Commands() {
			visit(child)
		}
	}
	visit(root)
}

// runCISummary runs a command whose output is a CI summary. The summary is
// withheld until the execution has settled, and any admission or settlement
// refusal is answered with the fixed CI error summary instead, so a pipeline
// never reads a verdict whose instance was not released.
func runCISummary(cmd *cobra.Command, guard *operationguard.Guard, profile operationguard.Profile, work func(context.Context) error) error {
	destination := cmd.OutOrStdout()
	var buffered bytes.Buffer
	cmd.SetOut(&buffered)
	started := false
	err := guard.Run(cmd.Context(), profile, func(ctx context.Context) error {
		started = true
		return work(ctx)
	})
	cmd.SetOut(destination)
	var unsettled *operationguard.Unsettled
	if !started || errors.As(err, &unsettled) {
		return refusedCI(cmd)
	}
	if _, copyErr := io.Copy(destination, &buffered); copyErr != nil {
		return errors.New("cannot write CI summary")
	}
	return err
}

// Operation capabilities are declared where each command is built, and the
// admission wrapper reads the declaration instead of classifying command
// paths. A runnable command that declares nothing is refused closed: it cannot
// silently acquire the read-only exemption by missing a declaration.
const capabilityAnnotation = "readmit.dev/operation-capability"

// executionAnnotation declares how a command that executes is admitted when
// it is not one execution held for the whole invocation: each-job commands
// run job after job, each admitted, bounded and settled as its own.
const executionAnnotation = "readmit.dev/execution"

// executeEachJob is the executionAnnotation value of a runner that serves
// jobs.
const executeEachJob = "each-job"

// ciSummaryAnnotation declares that a command's output is a CI summary, which
// is withheld until its execution settles and replaced by the fixed CI error
// summary on any admission or settlement refusal.
const ciSummaryAnnotation = "readmit.dev/ci-summary"

const (
	capabilityFree                = "free"
	capabilityAuthor              = "author"
	capabilityExecute             = "execute"
	capabilityExecuteIfSend       = "execute-if-send"
	capabilityAuthorIfQuotaChange = "author-if-quota-change"
)

func declare(capability string) map[string]string {
	return map[string]string{capabilityAnnotation: capability}
}

// commandProfile is the operation profile a command's annotations declare:
// its path, whether an interrupt stops it, and its admission. A command that
// declares no admission the vocabulary knows is not declared.
func commandProfile(cmd *cobra.Command) (operationguard.Profile, bool) {
	profile := operationguard.Profile{Name: cmd.CommandPath(), Interruptible: interruptible(cmd)}
	switch operationCapability(cmd) {
	case "":
	case capabilityAuthor:
		profile.Author = true
	case capabilityExecute:
		profile.Execution = operationguard.Execute
		if cmd.Annotations[executionAnnotation] == executeEachJob {
			profile.Execution = operationguard.ExecuteEachJob
		}
	default:
		return profile, false
	}
	return profile, true
}

func operationCapability(cmd *cobra.Command) string {
	switch declared := cmd.Annotations[capabilityAnnotation]; declared {
	case capabilityFree:
		return ""
	case capabilityAuthor, capabilityExecute:
		return declared
	case capabilityExecuteIfSend:
		if send, _ := cmd.Flags().GetBool("send"); send {
			return capabilityExecute
		}
		return ""
	case capabilityAuthorIfQuotaChange:
		if cmd.Flags().Changed("max-bytes") || cmd.Flags().Changed("max-files") {
			return capabilityAuthor
		}
		return ""
	}
	// A newly registered operation must be classified explicitly; it cannot
	// silently acquire the read-only exemption by missing a declaration.
	return "unsupported-operation"
}

func licenseOperation() *cobra.Command {
	root := &cobra.Command{Use: "operation", Short: "Activate and inspect local operation admission"}
	for _, action := range []string{"activate", "status", "resolve", "release"} {
		command := &cobra.Command{Use: action, Args: cobra.NoArgs, Annotations: declare(capabilityFree), RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("operation-policy")
			path = operationPolicyPath(path)
			var err error
			switch action {
			case "activate":
				err = operationguard.Activate(path)
			case "resolve":
				err = operationguard.Resolve(path)
			case "release":
				err = operationguard.Release(path)
			}
			if err != nil {
				return err
			}
			state, err := operationguard.Read(path)
			if err != nil {
				return err
			}
			return writeJSON(cmd, state)
		}}
		root.AddCommand(command)
	}
	return root
}

// operationPolicyPath is the operation policy new work is admitted through:
// the one --operation-policy names, or this computer's license when none is
// named. An account with no installed license refuses new work exactly as an
// absent policy always has.
func operationPolicyPath(named string) string {
	if named != "" {
		return named
	}
	return operationguard.InstalledPolicy()
}

func refusedCI(cmd *cobra.Command) error {
	if err := writeJSON(cmd, suite.CIError()); err != nil {
		return err
	}
	return statedRefusal(errors.New("suite operation admission unavailable"))
}
