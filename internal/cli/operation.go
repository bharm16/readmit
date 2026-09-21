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

	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/runqueue"
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
// from each command's Use when the command declares none of its own.
func wireOperations(root *cobra.Command, policy *string, ran *bool) {
	var guard *operationguard.Guard
	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		if run := command.RunE; run != nil {
			if command.Args == nil {
				command.Args = exactArgs(command)
			}
			command.RunE = func(cmd *cobra.Command, args []string) (err error) {
				// The help topic lookup is not a started operation: its refusal
				// is reported the way every pre-execution refusal is reported.
				if cmd.Annotations[helpTopicAnnotation] != "true" {
					*ran = true
				}
				if guard == nil {
					guard = operationguard.New(*policy)
				}
				if cmd.CommandPath() == "readmit runner execute" || cmd.CommandPath() == "readmit runner serve" {
					cmd.SetContext(customerrunner.WithOperationGuard(cmd.Context(), guard))
				}
				if interruptible(cmd) {
					ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
					defer stop()
					cmd.SetContext(ctx)
				}
				capability := operationCapability(cmd)
				if capability == "" {
					return run(cmd, args)
				}
				*ran = true
				release, err := guard.AdmitContext(cmd.Context(), capability)
				if err != nil {
					if cmd.CommandPath() == "readmit suite ci" {
						return refusedCI(cmd)
					}
					return err
				}
				if cmd.CommandPath() == "readmit suite ci" {
					ctx, cancel := context.WithTimeout(cmd.Context(), operationguard.MaxDuration)
					defer cancel()
					cmd.SetContext(runqueue.WithAdmission(ctx, func() error { return guard.CheckExecutionContext(ctx) }))
					destination := cmd.OutOrStdout()
					var buffered bytes.Buffer
					cmd.SetOut(&buffered)
					runErr := run(cmd, args)
					settleErr := release()
					cmd.SetOut(destination)
					if settleErr != nil {
						return refusedCI(cmd)
					}
					if _, copyErr := io.Copy(destination, &buffered); copyErr != nil {
						return errors.New("cannot write CI summary")
					}
					return runErr
				}
				defer func() { err = errors.Join(err, release()) }()
				if capability == "execute" {
					ctx, cancel := context.WithTimeout(cmd.Context(), operationguard.MaxDuration)
					defer cancel()
					cmd.SetContext(runqueue.WithAdmission(ctx, func() error { return guard.CheckExecutionContext(ctx) }))
				}
				return run(cmd, args)
			}
		}
		for _, child := range command.Commands() {
			visit(child)
		}
	}
	visit(root)
}

// Operation capabilities are declared where each command is built, and the
// admission wrapper reads the declaration instead of classifying command
// paths. A runnable command that declares nothing is refused closed: it cannot
// silently acquire the read-only exemption by missing a declaration.
const capabilityAnnotation = "readmit.dev/operation-capability"

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

func refusedCI(cmd *cobra.Command) error {
	if err := writeJSON(cmd, suite.CIError()); err != nil {
		return err
	}
	return statedRefusal(errors.New("suite operation admission unavailable"))
}
