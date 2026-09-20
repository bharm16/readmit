package cli

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/suite"
	"io"

	"strings"

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

func operationCapability(cmd *cobra.Command) string {
	path := strings.TrimPrefix(cmd.CommandPath(), "readmit ")
	if path == "license" || strings.HasPrefix(path, "license ") {
		return ""
	}
	switch path {
	case "runner execute", "runner serve":
		// The long-lived runner checks each job through the installed guard.
		return ""
	case "test", "replay":
		send, _ := cmd.Flags().GetBool("send")
		if send {
			return "execute"
		}
		return ""
	case "run start", "run resume", "run queue", "suite run", "suite ci", "target reset", "redact reexecute", "collect", "listen", "observe collect", "source collect", "source diagnose", "target check":
		return "execute"
	case "project quota":
		if cmd.Flags().Changed("max-bytes") || cmd.Flags().Changed("max-files") {
			return "author"
		}
		return ""
	case "capture", "import", "index build", "corpus generate", "project init", "project add", "project update", "project revise", "project note", "project settings", "scenario generate", "synth", "redact", "diagnose review", "baseline approve", "expectation release", "suite prepare", "suite approve-promotion", "target set", "secret add", "secret update", "secret rotate", "protect register", "protect rotate", "protect retire":
		return "author"
	}
	switch path {
	case "help", "inspect", "timeline", "index", "index show", "index search", "corpus", "corpus scan",
		"project", "project show", "project migration-preview", "project recover", "project archive", "project retire", "project delete",
		"backup", "backup create", "backup verify", "backup restore", "upgrade", "upgrade check", "upgrade prepare",
		"secret", "secret show", "secret scan", "protect", "protect show", "protect pack", "protect open", "protect inspect", "protect discard",
		"target", "target show", "source", "collect status", "observe", "observe validate", "observe explain",
		"diagnose", "correlate", "transform", "explain", "diff", "drift", "normalize", "redact export",
		"scenario preview", "scenario check-library", "baseline review", "baseline show", "expectation review", "expectation show", "expectation impact",
		"suite coverage", "suite review-promotion", "suite gate", "suite verify-gate", "suite gate-policy", "run status", "run clean",
		"runner enroll", "runner status", "runner verify-update", "report", "report verify", "report prepare", "report assemble", "report verify-retained", "report export", "report review",
		"share", "share verify", "sample synth", "sample capture", "sample index", "profile import", "profile export":
		return ""
	}
	// A newly registered operation must be classified explicitly; it cannot
	// silently acquire the read-only exemption by missing this inventory.
	return "unsupported-operation"
}

func licenseOperation(ran *bool) *cobra.Command {
	root := &cobra.Command{Use: "operation", Short: "Activate and inspect local operation admission"}
	for _, action := range []string{"activate", "status", "resolve", "release"} {
		command := &cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
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
