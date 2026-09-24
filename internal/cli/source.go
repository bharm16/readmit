package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/spf13/cobra"
)

func sourceCommand() *cobra.Command {
	command := &cobra.Command{
		Use:         "source",
		Annotations: declare(capabilityFree),
		Short:       "Diagnose access to, and collect evidence from, one approved customer-controlled source",
		Args:        cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return usage("source requires a subcommand: diagnose or collect")
		},
	}
	command.AddCommand(sourceDiagnose(), sourceCollect())
	return command
}

func sourceDiagnose() *cobra.Command {
	var policy, report string
	var machine bool
	var declared declaration
	command := &cobra.Command{
		Use:         "diagnose SOURCE --framing raw --terminator cr --encoding utf-8 --direction inbound",
		Annotations: declareInterruptible(capabilityExecute),
		Short:       "Report what access to the declared source was actually available, collecting nothing",
		RunE: func(cmd *cobra.Command, args []string) error {
			source, options, err := sourceRun(cmd, args[0], policy, declared, "")
			if err != nil {
				return err
			}
			access, err := operation.SourceDiagnose(cmd.Context(), source, options, report)
			if err != nil {
				return err
			}
			if machine {
				encoded, err := evidencesource.EncodeAccess(access)
				if err != nil {
					return err
				}
				if _, err := cmd.OutOrStdout().Write(encoded); err != nil {
					return errors.New("cannot write the access diagnosis")
				}
			} else if err := renderAccess(cmd.OutOrStdout(), access); err != nil {
				return err
			}
			if access.Status != observewindow.Complete {
				return statedRefusalWhen(machine, errors.New("source access is not available: "+access.Reason))
			}
			return nil
		},
	}
	command.Flags().StringVar(&policy, "policy", "", "Existing readmit-send-policy/v1 JSON file approving the destinations a source may be reached at")
	command.Flags().StringVar(&report, "report", "", "New readmit-source-access/v1 file for this diagnosis (never overwrite)")
	command.Flags().BoolVar(&machine, "json", false, "Write the readmit-source-access/v1 document to stdout instead of the summary")
	addDeclarationFlags(command, &declared, true)
	return command
}

func sourceCollect() *cobra.Command {
	var policy, saved, output, receipt string
	var declared declaration
	command := &cobra.Command{
		Use:         "collect SOURCE --output new_directory --receipt new_file --framing raw --terminator cr --encoding utf-8 --direction inbound",
		Annotations: declareInterruptible(capabilityExecute),
		Short:       "Stage the declared source's evidence in a new directory with the receipt of what was collected",
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" || receipt == "" {
				return usage("source collect requires --output with a new directory and --receipt with a new file")
			}
			source, options, err := sourceRun(cmd, args[0], policy, declared, saved)
			if err != nil {
				return err
			}
			collection, collectErr := operation.SourceCollect(cmd.Context(), source, output, receipt, options)
			if collectErr != nil && !errors.Is(collectErr, evidencesource.ErrIncomplete) {
				return collectErr
			}
			if err := renderCollection(cmd.OutOrStdout(), collection); err != nil {
				return err
			}
			if collectErr != nil {
				return refusal(errors.New("the collection did not complete: " + collection.Reason))
			}
			return nil
		},
	}
	command.Flags().StringVar(&policy, "policy", "", "Existing readmit-send-policy/v1 JSON file approving the destinations a source may be reached at")
	command.Flags().StringVar(&saved, "plan", "", "Existing readmit-import-plan/v1 JSON file holding the declarations below")
	command.Flags().StringVar(&output, "output", "", "New directory the collected evidence is staged in (never overwrite)")
	command.Flags().StringVar(&receipt, "receipt", "", "New readmit-source-collection/v1 receipt file (never overwrite)")
	addDeclarationFlags(command, &declared, true)
	return command
}

// sourceRun reads the declaration, the plan and the approved-destination policy
// one subcommand runs under. Both subcommands read the same three, so a
// diagnosis cannot answer under declarations a collection would not run under.
func sourceRun(cmd *cobra.Command, path, policy string, declared declaration, saved string) (evidencesource.Source, evidencesource.Options, error) {
	source, err := operation.SourceRead(path)
	if err != nil {
		return evidencesource.Source{}, evidencesource.Options{}, err
	}
	plan, err := declared.plan(cmd, saved)
	if err != nil {
		return evidencesource.Source{}, evidencesource.Options{}, err
	}
	options, err := operation.SourceOptions(plan, policy)
	if err != nil {
		return evidencesource.Source{}, evidencesource.Options{}, err
	}
	return source, options, nil
}

// renderAccess reports what access was available. Every line is a count, a
// declaration the person made or a status this release names: no entry name, no
// credential, no address and no byte of the source can reach it.
func renderAccess(out io.Writer, access evidencesource.Access) error {
	w := bufio.NewWriter(out)
	fmt.Fprintf(w, "Access diagnosis: %s\nSource: %s\nKind: %s\nScope: %s\n",
		access.Schema, access.Source.Name, access.Source.Kind, access.Source.Scope)
	fmt.Fprintf(w, "Status: %s\nRun state: %s\n", access.Status, runStateOrNone(access.RunState))
	if access.Reason != "" {
		fmt.Fprintf(w, "Reason: %s\n", access.Reason)
	}
	fmt.Fprintf(w, "Credential: %s\nRotation: %s\n", access.Credential.State, access.Credential.Rotation)
	if access.Destination == nil {
		fmt.Fprintln(w, "Destination: none; this source reaches no address")
	} else {
		fmt.Fprintf(w, "Destination: %s (%s)\n", allowedOrDenied(access.Destination.Allowed), access.Destination.Reason)
	}
	fmt.Fprintf(w, "Listed: %t\nDeclared entries: %d\nSelected: %d\nReadable: %d\nUnreadable: %d\nEntries not read: %d\n",
		access.Listed, access.Declared, access.Selected, access.Readable, access.Unreadable, access.NotRead)
	fmt.Fprintf(w, "Declared bytes: %d\nQuota: %d entries, %d bytes per entry, %d bytes in total\n",
		access.DeclaredBytes, access.Quota.MaxEntries, access.Quota.MaxEntryBytes, access.Quota.MaxTotalBytes)
	if len(access.QuotaExceeded) == 0 {
		fmt.Fprintln(w, "Quota: within")
	} else {
		fmt.Fprintf(w, "Quota: exceeded %s\n", strings.Join(access.QuotaExceeded, ", "))
	}
	if err := w.Flush(); err != nil {
		return errors.New("cannot write the access diagnosis summary")
	}
	return nil
}

// renderCollection reports what one collection staged. It names each entry,
// because an entry name is what a person acts on and is already in the receipt
// they chose to keep; it displays no byte of the evidence at all, and there is
// no --show-values here, because `timeline --show-values` is where evidence is
// read.
func renderCollection(out io.Writer, collection evidencesource.Collection) error {
	w := bufio.NewWriter(out)
	fmt.Fprintf(w, "Collection receipt: %s\nSource: %s\nKind: %s\nScope: %s\n",
		collection.Schema, collection.Source.Name, collection.Source.Kind, collection.Source.Scope)
	fmt.Fprintf(w, "Status: %s\nRun state: %s\n", collection.Status, runStateOrNone(collection.RunState))
	if collection.Reason != "" {
		fmt.Fprintf(w, "Reason: %s\n", collection.Reason)
	}
	fmt.Fprintf(w, "Collection identity: %s\nPlan: %s\nFraming: %s\nTerminator: %s\nEncoding: %s\nDirection: %s\n",
		collection.Identity, collection.Plan.Schema, collection.Plan.Framing,
		collection.Plan.Terminator, collection.Plan.Encoding, collection.Plan.Direction)
	totals := collection.Totals
	fmt.Fprintf(w, "Declared entries: %d\nCollected: %d\nDuplicates: %d\nExcluded: %d\nUnreadable: %d\nEntries not read: %d\n",
		totals.Declared, totals.Collected, totals.Duplicates, totals.Excluded, totals.Unreadable, totals.NotRead)
	fmt.Fprintf(w, "Bytes: %d\nRecords: %d\nOccurrences: %d\n", totals.Bytes, totals.Records, totals.Occurrences)
	for _, entry := range collection.Entries {
		fmt.Fprintf(w, "  %s %s size %d attempts %d", entry.Name, entry.State, entry.Size, entry.Attempts)
		if entry.DuplicateOf != "" {
			fmt.Fprintf(w, " duplicate of %s", entry.DuplicateOf)
		}
		if entry.Reason != "" {
			fmt.Fprintf(w, ": %s", entry.Reason)
		}
		fmt.Fprintln(w)
	}
	if err := w.Flush(); err != nil {
		return errors.New("cannot write the collection summary")
	}
	return nil
}

// runStateOrNone renders the durable-run state a status produces. A completed
// observation produces none, and saying so is clearer than an empty field.
func runStateOrNone(state string) string {
	if state == "" {
		return "none; the observation completed"
	}
	return state
}

func allowedOrDenied(allowed bool) string {
	if allowed {
		return "allowed"
	}
	return "denied"
}
