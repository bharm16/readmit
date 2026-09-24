package cli

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/index"
	"github.com/spf13/cobra"
)

// incompleteBackup and incompleteRestore are what a command reports when the
// artifact it wrote is honest but not whole. They never name the evidence that
// was missing: the report on stdout does that, and a diagnostic repeats no path.
var (
	incompleteBackup  = errors.New("this project holds evidence the backup could not verify; the backup records what it found and puts nothing in its place")
	incompleteRestore = errors.New("this backup holds evidence the restore could not account for; it is reported exactly as it was found and nothing was put in its place")
)

func backupCommand() *cobra.Command {
	command := &cobra.Command{
		Use:         "backup",
		Annotations: declare(capabilityFree),
		Short:       "Back up a project, verify a backup, and restore one with its indexes rebuilt",
		Args:        cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return usage("backup requires a subcommand: create, verify, or restore")
		},
	}
	command.AddCommand(backupCreate(), backupVerify(), backupRestore())
	return command
}

func backupCreate() *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:         "create PROJECT --output NEW_DIRECTORY",
		Annotations: declareInterruptible(capabilityFree),
		Short:       "Copy a project into a new verified backup directory",
		Args:        backupOneArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				return usage("backup create requires --output with a new directory")
			}
			ctx := cmd.Context()
			report, err := backup.Create(ctx, args[0], output)
			if err != nil {
				return err
			}
			if err := writeBackupReport(cmd.OutOrStdout(), report); err != nil {
				return err
			}
			if !report.Complete() {
				return incompleteBackup
			}
			return nil
		},
	}
	command.Flags().StringVar(&output, "output", "", "New backup directory (never overwrite)")
	return command
}

func backupVerify() *cobra.Command {
	return &cobra.Command{
		Use:         "verify BACKUP",
		Annotations: declare(capabilityFree),
		Short:       "Read a backup whole and report what it holds and what it could not verify",
		Args:        backupOneArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			document, err := backup.Verify(args[0])
			if err != nil {
				return err
			}
			if err := writeBackupDocument(cmd.OutOrStdout(), document); err != nil {
				return err
			}
			if !document.Complete() {
				return incompleteBackup
			}
			return nil
		},
	}
}

func backupRestore() *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:         "restore BACKUP --output NEW_DIRECTORY",
		Annotations: declareInterruptible(capabilityFree),
		Short:       "Write a backup into a new project directory and rebuild its indexes",
		Args:        backupOneArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				return usage("backup restore requires --output with a new directory")
			}
			ctx := cmd.Context()
			report, err := backup.Restore(ctx, args[0], output, time.Now().UTC())
			if err != nil {
				return err
			}
			if err := writeRestoreReport(cmd.OutOrStdout(), report); err != nil {
				return err
			}
			if !report.Complete() {
				return incompleteRestore
			}
			return nil
		},
	}
	command.Flags().StringVar(&output, "output", "", "New project directory (never overwrite)")
	return command
}

// writeBackupReport prints what a backup found. A backup reports the state of
// every registered case and revision as it found it, because that state is what
// it is recording.
func writeBackupReport(out io.Writer, report backup.Report) error {
	return writeLines(out, func(w io.Writer) {
		backupSummary(w, "Backup written", report)
		for _, entry := range report.Evidence {
			fmt.Fprintf(w, "  %s kind=%s evidence=%s identity=%s\n", entry.Name, entry.Kind, entry.State, entry.Identity)
		}
		backupIndexes(w, report.Indexes)
	})
}

// writeRestoreReport prints what a restore found beside what the backup had
// recorded, so evidence that was already missing when the backup ran reads as
// that rather than as something the restore lost.
func writeRestoreReport(out io.Writer, report backup.Report) error {
	return writeLines(out, func(w io.Writer) {
		backupSummary(w, "Project restored", report)
		for _, entry := range report.Evidence {
			fmt.Fprintf(w, "  %s kind=%s recorded=%s restored=%s identity=%s\n",
				entry.Name, entry.Kind, entry.Recorded, entry.State, entry.Identity)
		}
		backupIndexes(w, report.Indexes)
	})
}

// backupSummary and backupIndexes are the parts of a report that read the same
// whichever operation produced them. Neither prints the directory that was read
// or written: the operator named those, and a report repeats no path of its own.
func backupSummary(w io.Writer, headline string, report backup.Report) {
	fmt.Fprintf(w, "%s: %s\nComplete: %s\nFiles: %d\nBytes: %d\nRegistered artifacts: %d\n",
		headline, backup.Schema, yesNo(report.Complete()), report.Files, report.Bytes, len(report.Evidence))
}

func backupIndexes(w io.Writer, entries []backup.IndexOutcome) {
	fmt.Fprintf(w, "Indexes: %d\n", len(entries))
	for _, entry := range entries {
		fmt.Fprintf(w, "  %s case=%s index=%s\n", entry.Name, absent(entry.Case), entry.State)
	}
}

// writeBackupDocument prints what a verified backup records, including the
// retention declarations each index would be built again under. A field
// selector is not a value, and no retained value is held in a backup at all.
func writeBackupDocument(out io.Writer, document backup.Document) error {
	return writeLines(out, func(w io.Writer) {
		fmt.Fprintf(w, "Backup: %s\nComplete: %s\nFiles: %d\nBytes: %d\nRegistered artifacts: %d\n",
			document.Schema, yesNo(document.Complete()), len(document.Files), backupBytes(document), len(document.Evidence))
		for _, entry := range document.Evidence {
			fmt.Fprintf(w, "  %s kind=%s evidence=%s identity=%s\n", entry.Name, entry.Kind, entry.State, entry.Identity)
		}
		fmt.Fprintf(w, "Indexes: %d\n", len(document.Indexes))
		for _, entry := range document.Indexes {
			fmt.Fprintf(w, "  %s case=%s recipe=%s fields=%s retention=%s retain-until=%s\n",
				entry.Name, absent(entry.Case), entry.Recipe, list(entry.Fields), absent(entry.Retention), retainUntil(entry))
		}
	})
}

func backupBytes(document backup.Document) int64 {
	total := int64(0)
	for _, file := range document.Files {
		total += file.Size
	}
	return total
}

// retainUntil reports a recorded retention end the way `index show` does, so
// the same declaration reads the same in both commands. An index that declared
// no end is reported with the word an operator had to type for it.
func retainUntil(entry backup.Index) string {
	if entry.Recipe != backup.Declared {
		return "none"
	}
	if entry.RetainUntil == nil {
		return index.Indefinite
	}
	return entry.RetainUntil.Format(time.RFC3339)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func backupOneArgument(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("backup subcommand requires exactly one directory")
	}
	return nil
}
