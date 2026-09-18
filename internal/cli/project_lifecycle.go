package cli

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bharm16/readmit/internal/lifecycle"
	"github.com/bharm16/readmit/internal/project"
	"github.com/spf13/cobra"
)

func projectMigrationPreview(ran *bool) *cobra.Command {
	return &cobra.Command{Use: "migration-preview PROJECT", Short: "Preview supported project and index schemas without changing files", Args: projectOneArgument, RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		plan, err := lifecycle.Preview(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		data, err := json.Marshal(plan, json.Deterministic(true))
		if err != nil {
			return errors.New("cannot encode migration preview")
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(data)); err != nil {
			return err
		}
		if !plan.Compatible {
			return errors.New("unsupported or damaged documents; no migration available")
		}
		return nil
	}}
}
func projectQuota(ran *bool) *cobra.Command {
	var maxBytes int64
	var maxFiles int
	command := &cobra.Command{Use: "quota PROJECT [--max-bytes N --max-files N]", Short: "Check retained-file quota, or set explicit positive limits", Args: projectOneArgument, RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		if cmd.Flags().Changed("max-bytes") || cmd.Flags().Changed("max-files") {
			if !cmd.Flags().Changed("max-bytes") || !cmd.Flags().Changed("max-files") {
				return errors.New("setting quota requires both --max-bytes and --max-files")
			}
			if err := project.SetQuota(args[0], project.Quota{Schema: project.QuotaSchema, MaxBytes: maxBytes, MaxFiles: maxFiles}); err != nil {
				return err
			}
		}
		opened, err := project.Open(args[0])
		if err != nil {
			return err
		}
		q, present, err := project.ReadQuota(opened.Root)
		if err != nil {
			return err
		}
		usage, checkErr := project.CheckQuota(opened.Root)
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Retained files: %d\nRetained bytes: %d\nQuota declared: %t\n", usage.Files, usage.Bytes, present); err != nil {
			return err
		}
		if present {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Maximum files: %d\nMaximum bytes: %d\n", q.MaxFiles, q.MaxBytes); err != nil {
				return err
			}
		}
		return checkErr
	}}
	command.Flags().Int64Var(&maxBytes, "max-bytes", 0, "Maximum retained regular-file bytes, including recovery copies")
	command.Flags().IntVar(&maxFiles, "max-files", 0, "Maximum retained regular files (1 to 65536)")
	return command
}
func projectRecover(ran *bool) *cobra.Command {
	var name, digest string
	command := &cobra.Command{Use: "recover PROJECT --document NAME --digest SHA256", Short: "Restore a selected recovery copy and retain the current document", Args: projectOneArgument, RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		if err := project.Recover(args[0], name, digest); err != nil {
			return err
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "Document recovered; previous bytes retained.")
		return err
	}}
	command.Flags().StringVar(&name, "document", "", "project.json, revisions.json, or quota.json")
	command.Flags().StringVar(&digest, "digest", "", "SHA-256 suffix of the selected recovery copy")
	return command
}
func projectArchive(ran *bool, deleteSource bool) *cobra.Command {
	var output string
	var confirm bool
	name := "archive"
	description := "Create a verified recovery archive; keep the source project"
	if deleteSource {
		name = "delete"
		description = "Create a verified recovery archive, then unlink the whole source project"
	}
	command := &cobra.Command{Use: name + " PROJECT --output NEW_ARCHIVE", Short: description, Args: projectOneArgument, RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		if output == "" {
			return errors.New("archive and delete require --output with a new recovery directory")
		}
		if deleteSource && !confirm {
			return errors.New("project delete requires --confirm-delete; recovery archive will be retained")
		}
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		report, operationErr := lifecycle.Archive(ctx, args[0], output, deleteSource)
		if report.Root != "" {
			if err := writeBackupReport(cmd.OutOrStdout(), report); err != nil {
				return err
			}
		}
		if operationErr != nil {
			return operationErr
		}
		if deleteSource {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Project unlinked; recovery archive retained. This is not secure erasure.")
			return err
		}
		return nil
	}}
	command.Flags().StringVar(&output, "output", "", "New recovery archive outside the source tree")
	if deleteSource {
		command.Flags().BoolVar(&confirm, "confirm-delete", false, "Explicitly unlink the source after complete recovery verification")
	}
	return command
}
