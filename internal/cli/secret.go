package cli

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/spf13/cobra"
)

func secretCommand() *cobra.Command {
	command := &cobra.Command{
		Use:         "secret",
		Annotations: declare(capabilityFree),
		Short:       "Reference credentials that stay in an OS or customer-managed secret store",
		Args:        cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return usage("secret requires a subcommand: add, update, rotate, show, or scan")
		},
	}
	command.AddCommand(secretAdd(), secretUpdate(), secretRotate(), secretShow(), secretScan())
	return command
}

func secretAdd() *cobra.Command {
	var file, name, store, purpose, address, program, maxAge string
	var arguments []string
	command := &cobra.Command{
		Use:         "add --secrets FILE --name NAME --store KIND --address HOST:PORT --command PROGRAM",
		Annotations: declare(capabilityAuthor),
		Short:       "Register a reference to a credential held in a secret store",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, stored, err := operation.AddSecretReference(file, secret.Reference{
				Name:       name,
				Store:      secret.Store(store),
				Purpose:    secret.Purpose(purpose),
				Address:    address,
				Command:    program,
				Arguments:  arguments,
				Generation: 1,
				RotatedAt:  time.Now().UTC().Truncate(time.Second),
				MaxAge:     maxAge,
			})
			if err != nil {
				return err
			}
			return writeReference(cmd.OutOrStdout(), "Credential reference registered: "+stored.Name, stored)
		},
	}
	secretsFlag(command, &file)
	command.Flags().StringVar(&name, "name", "", "Name this reference is used under")
	command.Flags().StringVar(&store, "store", "", "Declared storage: os-keychain or customer-managed")
	command.Flags().StringVar(&purpose, "purpose", string(secret.MLLPEndpoint), "The one use this credential may be bound to: mllp-endpoint or source-endpoint")
	command.Flags().StringVar(&address, "address", "", "The one endpoint address this credential may be presented to")
	command.Flags().StringVar(&program, "command", "", "Absolute path of the program that reads the credential from its store")
	command.Flags().StringArrayVar(&arguments, "argument", nil, "Locator argument for that program; repeat for more. Never a credential")
	command.Flags().StringVar(&maxAge, "max-age", "", "How long a recorded rotation stays current, as a Go duration")
	return command
}

func secretUpdate() *cobra.Command {
	var file, name, store, address, program, maxAge string
	var arguments []string
	command := &cobra.Command{
		Use:         "update --secrets FILE --name NAME",
		Annotations: declare(capabilityAuthor),
		Short:       "Change where a registered reference reads its credential from, or what it may be presented to",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var change secret.Change
			if cmd.Flags().Changed("store") {
				value := secret.Store(store)
				change.Store = &value
			}
			if cmd.Flags().Changed("address") {
				change.Address = &address
			}
			if cmd.Flags().Changed("command") {
				change.Command = &program
			}
			if cmd.Flags().Changed("argument") {
				declared, err := declaredValues(arguments)
				if err != nil {
					return err
				}
				change.Arguments = &declared
			}
			if cmd.Flags().Changed("max-age") {
				change.MaxAge = &maxAge
			}
			if change.Empty() {
				return usage("secret update requires at least one change")
			}
			_, stored, err := operation.UpdateSecretReference(file, name, change)
			if err != nil {
				return err
			}
			return writeReference(cmd.OutOrStdout(), "Credential reference updated: "+stored.Name, stored)
		},
	}
	secretsFlag(command, &file)
	command.Flags().StringVar(&name, "name", "", "Registered reference to change")
	command.Flags().StringVar(&store, "store", "", "Declared storage: os-keychain or customer-managed")
	command.Flags().StringVar(&address, "address", "", "The one endpoint address this credential may be presented to")
	command.Flags().StringVar(&program, "command", "", "Absolute path of the program that reads the credential from its store")
	command.Flags().StringArrayVar(&arguments, "argument", nil, `Replace the locator arguments with these, or pass "" alone to clear them`)
	command.Flags().StringVar(&maxAge, "max-age", "", "How long a recorded rotation stays current, as a Go duration")
	return command
}

func secretRotate() *cobra.Command {
	var file, name string
	command := &cobra.Command{
		Use:         "rotate --secrets FILE --name NAME",
		Annotations: declare(capabilityAuthor),
		Short:       "Record that the credential behind a reference was replaced in its store",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, stored, err := operation.RotateSecretReference(cmd.Context(), file, name)
			if err != nil {
				return err
			}
			return writeReference(cmd.OutOrStdout(), "Rotation recorded: "+stored.Name, stored)
		},
	}
	secretsFlag(command, &file)
	command.Flags().StringVar(&name, "name", "", "Registered reference whose credential was replaced")
	return command
}

func secretShow() *cobra.Command {
	var file string
	command := &cobra.Command{
		Use:         "show --secrets FILE",
		Annotations: declare(capabilityFree),
		Short:       "Show every registered reference, its scope and its rotation state, with the credential masked",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			document, err := readStore(file)
			if err != nil {
				return err
			}
			now := time.Now()
			return writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				fmt.Fprintf(w, "Document: %s\nReferences: %d\n", document.Schema, len(document.References))
				for _, entry := range document.References {
					writeReferenceLines(w, entry, now)
				}
			})
		},
	}
	secretsFlag(command, &file)
	return command
}

func secretScan() *cobra.Command {
	var file, name string
	command := &cobra.Command{
		Use:         "scan --secrets FILE PATH...",
		Annotations: declare(capabilityFree),
		Short:       "Check configuration, manifests, reports, logs and local browser state for a known credential value",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("secret scan requires at least one file or directory to check")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			scan, skipped, err := operation.ScanSecrets(cmd.Context(), file, args, name)
			if err != nil {
				return refusal(err)
			}
			if err := writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				fmt.Fprintf(w, "Credential leakage scan\nReferences checked: %d\nFiles checked: %d\nEntries not read: %d\nStatus: %s\nLocations holding a known credential value: %d\n",
					scan.KnownValues, scan.Files, skipped, scan.Status, len(scan.Locations))
				for _, location := range scan.Locations {
					fmt.Fprintf(w, "  %s\n", location)
				}
				fmt.Fprintf(w, "Limitations: %s\n", scan.Limitations)
			}); err != nil {
				return refusal(err)
			}
			if len(scan.Locations) > 0 {
				return unstatedFailure(errors.New("a checked location holds a known credential value"))
			}
			return nil
		},
	}
	secretsFlag(command, &file)
	command.Flags().StringVar(&name, "name", "", "Check for one registered reference instead of every one")
	return command
}

func secretsFlag(command *cobra.Command, file *string) {
	command.Flags().StringVar(file, "secrets", "", "Secret reference document holding the references")
}

func readStore(path string) (secret.Document, error) {
	if path == "" {
		return secret.Document{}, usage("secret requires --secrets naming a secret reference document")
	}
	return operation.ReadSecrets(path)
}

// openOrEmptyStore reads an existing document, or starts the first one. A path
// that exists but cannot be read as this contract is reported, never replaced.
func openOrEmptyStore(path string) (secret.Document, error) {
	if path == "" {
		return secret.Document{}, usage("secret requires --secrets naming a secret reference document")
	}
	return operation.OpenOrEmptySecrets(path)
}

func writeReference(out io.Writer, headline string, entry secret.Reference) error {
	return writeLines(out, func(w io.Writer) {
		fmt.Fprintf(w, "%s\n", headline)
		writeReferenceLines(w, entry, time.Now())
	})
}

// writeReferenceLines renders one reference. The credential is masked because
// this command never read one, and the locator arguments are counted rather
// than echoed: an argument is the one place an operator could have put a value,
// and readmit does not repeat it back. `secret scan` finds it if they did.
func writeReferenceLines(w io.Writer, entry secret.Reference, now time.Time) {
	fmt.Fprintf(w, "  %s store=%s purpose=%s address=%s generation=%d\n", entry.Name, entry.Store, entry.Purpose, entry.Address, entry.Generation)
	fmt.Fprintf(w, "    value: %s (never read, never stored, never exported)\n", secret.Mask)
	fmt.Fprintf(w, "    rotated: %s rotation: %s max-age: %s\n", entry.RotatedAt.UTC().Format(time.RFC3339), entry.Rotation(now), absent(entry.MaxAge))
	fmt.Fprintf(w, "    command: %s (%d locator arguments)\n", entry.Command, len(entry.Arguments))
}
