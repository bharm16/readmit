package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/bharm16/readmit/internal/lifecycle"
	"github.com/bharm16/readmit/internal/upgrade"
	"github.com/spf13/cobra"
)

func upgradeCommand() *cobra.Command {
	command := &cobra.Command{
		Use:         "upgrade",
		Annotations: declare(capabilityFree),
		Short:       "Check a staged upgrade against this machine and take the archive it rolls back to",
		Args:        cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return usage("upgrade requires a subcommand: check or prepare")
		},
	}
	command.AddCommand(upgradeCheck(), upgradePrepare())
	return command
}

// upgradeCheck is the explicit update check. Nothing about it is automatic:
// the candidate is a directory an administrator staged, no network connection
// is opened, and a refusal is reported after the plan rather than instead of
// it, so an operator reads why before they read that.
func upgradeCheck() *cobra.Command {
	var candidate string
	var projects, runs []string
	command := &cobra.Command{
		Use:         "check --candidate STAGED_DIRECTORY [--project PROJECT] [--run RUN]",
		Annotations: declareInterruptible(capabilityFree),
		Short:       "Read a staged candidate and report what this build makes of the evidence here",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if candidate == "" {
				return usage("upgrade check requires --candidate with the directory an administrator staged")
			}
			// A review of nothing is not a compatibility review, and reporting
			// one as ready would be the auto-pass this command exists to
			// prevent. What this machine holds is named, never discovered.
			if len(projects)+len(runs) == 0 {
				return usage("upgrade check requires at least one --project or --run: a check that reviewed nothing is not a compatibility review")
			}
			ctx := cmd.Context()
			plan, err := upgrade.Check(ctx, candidate, projects, runs)
			if err != nil {
				return err
			}
			if err := writeUpgradePlan(cmd.OutOrStdout(), plan); err != nil {
				return err
			}
			if refusal := plan.Refusal(); refusal != nil {
				return &ExitError{Code: 2, Err: refusal}
			}
			return nil
		},
	}
	upgradeCandidateFlag(command, &candidate)
	command.Flags().StringArrayVar(&projects, "project", nil, "A project directory this machine already holds (repeatable)")
	command.Flags().StringArrayVar(&runs, "run", nil, "A durable run directory this machine already holds (repeatable)")
	return command
}

// upgradePrepare takes the recovery archive an upgrade is rolled back to. It
// requires --approve because it is the point at which a machine is committed
// to an upgrade, and it deliberately does not require the candidate to be
// signed for distribution: a rollback point must never wait on a signing
// decision. Its status reports the archive it wrote, never permission to
// install anything, and it restates the check's refusal on its last line.
func upgradePrepare() *cobra.Command {
	var candidate, output string
	var approve bool
	command := &cobra.Command{
		Use:         "prepare PROJECT --candidate STAGED_DIRECTORY --output NEW_ARCHIVE --approve",
		Annotations: declareInterruptible(capabilityFree),
		Short:       "Take the verified recovery archive this upgrade would be rolled back to",
		Args:        upgradeOneArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			if candidate == "" || output == "" {
				return usage("upgrade prepare requires --candidate with the staged directory and --output with a new recovery archive")
			}
			if !approve {
				return usage("upgrade prepare requires --approve; an administrator approves an upgrade before anything is written")
			}
			ctx := cmd.Context()
			plan, err := upgrade.Check(ctx, candidate, []string{args[0]}, nil)
			if err != nil {
				return err
			}
			if err := writeUpgradePlan(cmd.OutOrStdout(), plan); err != nil {
				return err
			}
			// A rollback point stands for an upgrade that was actually staged
			// and for evidence the build taking it can verify. The refusals
			// are the check's own, so the two commands state one rule; the
			// signing refusal deliberately is not among them.
			if !plan.StagedIntact() {
				return upgrade.ErrNotStaged
			}
			if !plan.RetainedReadable() {
				return upgrade.ErrNotReadable
			}
			report, operationErr := lifecycle.Archive(ctx, args[0], output, false)
			if report.Root != "" {
				if err := writeBackupReport(cmd.OutOrStdout(), report); err != nil {
					return err
				}
			}
			if operationErr != nil {
				return operationErr
			}
			if !report.Complete() {
				return incompleteBackup
			}
			return writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				fmt.Fprintf(w, "Rollback point taken under engine build: %s\n", plan.Installed)
				if refusal := plan.Refusal(); refusal != nil {
					fmt.Fprintf(w, "Installing this candidate is still refused: %s\n", refusal)
				}
			})
		},
	}
	upgradeCandidateFlag(command, &candidate)
	command.Flags().StringVar(&output, "output", "", "New recovery archive outside the project (never overwrite)")
	command.Flags().BoolVar(&approve, "approve", false, "Record that an administrator approved this upgrade before anything is written")
	return command
}

func upgradeCandidateFlag(command *cobra.Command, candidate *string) {
	command.Flags().StringVar(candidate, "candidate", "", "Directory holding the staged packages and their "+upgrade.CandidateDocumentName)
}

func upgradeOneArgument(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("upgrade prepare requires exactly one project directory")
	}
	return nil
}

// writeUpgradePlan prints the plan document itself rather than a second
// rendering that happens to agree with it, so what an operator reads and what
// a script parses are one artifact.
func writeUpgradePlan(out io.Writer, plan upgrade.Plan) error {
	document, err := upgrade.Encode(plan)
	if err != nil {
		return err
	}
	if _, err := out.Write(document); err != nil {
		return errors.New("cannot write command output")
	}
	return nil
}
