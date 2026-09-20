package cli

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/suite"
	"io"

	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/spf13/cobra"
)

// wireOperations wraps the actual operation, ensuring admission is released on
// every return, including failures. Help, parsing, and retained reads stay free.
func wireOperations(root *cobra.Command, policy *string, ran *bool) {
	var guard *operationguard.Guard
	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		if run := command.RunE; run != nil {
			command.RunE = func(cmd *cobra.Command, args []string) (err error) {
				if guard == nil {
					guard = operationguard.New(*policy)
				}
				if cmd.CommandPath() == "readmit runner execute" || cmd.CommandPath() == "readmit runner serve" {
					cmd.SetContext(customerrunner.WithOperationGuard(cmd.Context(), guard))
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

func licenseOperation(ran *bool) *cobra.Command {
	root := &cobra.Command{Use: "operation", Short: "Activate and inspect local operation admission"}
	for _, action := range []string{"activate", "status", "resolve", "release"} {
		command := &cobra.Command{Use: action, Args: cobra.NoArgs, Annotations: declare(capabilityFree), RunE: func(cmd *cobra.Command, _ []string) error {
			*ran = true
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
			data, err := json.Marshal(state)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return err
		}}
		root.AddCommand(command)
	}
	return root
}

func refusedCI(cmd *cobra.Command) error {
	if err := writeJSON(cmd, suite.CIError()); err != nil {
		return err
	}
	return &ExitError{Code: 2, Err: errors.New("suite operation admission unavailable"), Reported: true}
}
