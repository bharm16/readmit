package cli

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/bharm16/readmit/internal/protect"
	"github.com/spf13/cobra"
)

func protectCommand() *cobra.Command {
	command := &cobra.Command{
		Use:         "protect",
		Annotations: declare(capabilityFree),
		Short:       "Encrypt evidence and configuration into transfer packages with a key readmit never holds",
		Args:        cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return usage("protect requires a subcommand: register, rotate, retire, show, pack, open, inspect, or discard")
		},
	}
	command.AddCommand(
		protectRegister(), protectRotate(), protectRetire(), protectShow(),
		protectPack(), protectOpen(), protectInspect(), protectDiscard(),
	)
	return command
}

func protectRegister() *cobra.Command {
	var file, name, storage, program, maxAge, retain string
	var arguments []string
	command := &cobra.Command{
		Use:         "register --protection FILE --name NAME --storage KIND --command PROGRAM",
		Annotations: declare(capabilityAuthor),
		Short:       "Register a protection control whose key stays in an OS or customer-managed store",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			protection, err := protectionFile(file)
			if err != nil {
				return err
			}
			_, stored, err := protection.Register(protect.Control{
				Name:      name,
				Storage:   protect.Storage(storage),
				Command:   program,
				Arguments: arguments,
				MaxAge:    maxAge,
				Retain:    retain,
			}, time.Now())
			if err != nil {
				return err
			}
			return writeControl(cmd.OutOrStdout(), "Protection control registered: "+stored.Name, stored)
		},
	}
	protectionFlag(command, &file)
	command.Flags().StringVar(&name, "name", "", "Name this control is used under")
	command.Flags().StringVar(&storage, "storage", "", "Declared at-rest control: os-volume-encryption, customer-key, or none-declared")
	command.Flags().StringVar(&program, "command", "", "Absolute path of the program that reads the key from its store")
	command.Flags().StringArrayVar(&arguments, "argument", nil, "Locator argument for that program; repeat for more. Never key material")
	command.Flags().StringVar(&maxAge, "max-age", "", "How long a recorded key rotation stays current, as a Go duration")
	command.Flags().StringVar(&retain, "retain", "", "Retention period packages written under this control declare, as a Go duration")
	return command
}

func protectRotate() *cobra.Command {
	var file, name string
	command := &cobra.Command{
		Use:         "rotate --protection FILE --name NAME",
		Annotations: declare(capabilityAuthor),
		Short:       "Record that the key behind a control was replaced in its own store",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			protection, err := protectionFile(file)
			if err != nil {
				return err
			}
			// The declared store answers for the control before protect records
			// anything, so a rotation is never recorded against a key readmit
			// cannot read.
			_, stored, err := protection.Rotate(cmd.Context(), name, time.Now())
			if err != nil {
				return err
			}
			return writeControl(cmd.OutOrStdout(), "Key rotation recorded: "+stored.Name, stored)
		},
	}
	protectionFlag(command, &file)
	command.Flags().StringVar(&name, "name", "", "Registered control whose key was replaced")
	return command
}

func protectRetire() *cobra.Command {
	var file, name string
	command := &cobra.Command{
		Use:         "retire --protection FILE --name NAME",
		Annotations: declare(capabilityAuthor),
		Short:       "Stop a control writing new packages; it still opens the packages it wrote",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			protection, err := protectionFile(file)
			if err != nil {
				return err
			}
			_, stored, err := protection.Retire(name)
			if err != nil {
				return err
			}
			return writeControl(cmd.OutOrStdout(), "Protection control retired: "+stored.Name, stored)
		},
	}
	protectionFlag(command, &file)
	command.Flags().StringVar(&name, "name", "", "Registered control to retire")
	return command
}

func protectShow() *cobra.Command {
	var file string
	command := &cobra.Command{
		Use:         "show --protection FILE",
		Annotations: declare(capabilityFree),
		Short:       "Show every registered control, its declared storage, rotation and retention, with the key masked",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			protection, err := protectionFile(file)
			if err != nil {
				return err
			}
			document, err := protection.Read()
			if err != nil {
				return err
			}
			now := time.Now()
			return writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				fmt.Fprintf(w, "Document: %s\nControls: %d\n", document.Schema, len(document.Controls))
				for _, entry := range document.Controls {
					writeControlLines(w, entry, now)
				}
				fmt.Fprintf(w, "Limitations: %s\n", protect.Limitations)
			})
		},
	}
	protectionFlag(command, &file)
	return command
}

func protectPack() *cobra.Command {
	var file, name, output string
	command := &cobra.Command{
		Use:         "pack --protection FILE --name NAME --output NEW_DIRECTORY PATH...",
		Annotations: declare(capabilityFree),
		Short:       "Write an encrypted transfer package from the named files and directories, leaving them unchanged",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("protect pack requires at least one file or directory to pack")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				return usage("protect pack requires --output naming a new directory")
			}
			protection, err := protectionFile(file)
			if err != nil {
				return err
			}
			descriptor, notRead, err := protection.Pack(cmd.Context(), name, args, output, time.Now())
			if err != nil {
				return err
			}
			return writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				fmt.Fprintf(w, "Encrypted transfer package written\n")
				writePackageLines(w, descriptor, time.Now())
				fmt.Fprintf(w, "Files packed: %d\nEntries not read: %d\n", len(descriptor.Entries), notRead)
				fmt.Fprintf(w, "Limitations: %s\n", protect.Limitations)
			})
		},
	}
	protectionFlag(command, &file)
	command.Flags().StringVar(&name, "name", "", "Registered control whose key encrypts this package")
	command.Flags().StringVar(&output, "output", "", "New directory to write the encrypted package into")
	return command
}

func protectOpen() *cobra.Command {
	var file, name, source, output string
	command := &cobra.Command{
		Use:         "open --protection FILE --package DIRECTORY --output NEW_DIRECTORY",
		Annotations: declare(capabilityFree),
		Short:       "Decrypt a transfer package into a new directory",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if source == "" || output == "" {
				return usage("protect open requires --package and --output")
			}
			protection, err := protectionFile(file)
			if err != nil {
				return err
			}
			// Without --name the package's own control opens it; naming a
			// different one is refused.
			descriptor, index, err := protection.Open(cmd.Context(), name, source, output)
			if err != nil {
				return err
			}
			return writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				fmt.Fprintf(w, "Transfer package opened\n")
				writePackageLines(w, descriptor, time.Now())
				fmt.Fprintf(w, "Files written: %d\nEntries the pack did not read: %d\n", len(index.Entries), index.NotRead)
				fmt.Fprintf(w, "Limitations: %s\n", protect.Limitations)
			})
		},
	}
	protectionFlag(command, &file)
	command.Flags().StringVar(&name, "name", "", "Control to open with; the package's own control by default")
	command.Flags().StringVar(&source, "package", "", "Encrypted transfer package to open")
	command.Flags().StringVar(&output, "output", "", "New directory to write the decrypted files into")
	return command
}

func protectInspect() *cobra.Command {
	command := &cobra.Command{
		Use:         "inspect PACKAGE",
		Annotations: declare(capabilityFree),
		Short:       "Report what a transfer package declares about itself, without a key",
		Args:        protectOneArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			descriptor, _, err := protect.ReadPackage(args[0])
			if err != nil {
				return err
			}
			return writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				fmt.Fprintf(w, "Encrypted transfer package\n")
				writePackageLines(w, descriptor, time.Now())
				fmt.Fprintf(w, "Packed names: encrypted in the package index, never in this descriptor\n")
				fmt.Fprintf(w, "Limitations: %s\n", protect.Limitations)
			})
		},
	}
	return command
}

func protectDiscard() *cobra.Command {
	var override bool
	command := &cobra.Command{
		Use:         "discard PACKAGE",
		Annotations: declare(capabilityFree),
		Short:       "Unlink the files a transfer package declares and state what that does not establish",
		Args:        protectOneArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			now := time.Now()
			descriptor, removed, err := protect.Discard(args[0], now, override)
			if err != nil {
				return err
			}
			return writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				fmt.Fprintf(w, "Transfer package discarded\nPackage: %s\nControl: %s\nFiles unlinked: %d\nDeclared retention: %s\n",
					descriptor.Package, descriptor.Control, removed, descriptor.Retention(now))
				if override && !descriptor.RetainUntil.IsZero() {
					fmt.Fprintf(w, "Declared retention overridden: retained until %s\n", descriptor.RetainUntil.UTC().Format(time.RFC3339))
				}
				fmt.Fprintf(w, "Limitations: %s\n", protect.DeletionLimitations)
			})
		},
	}
	command.Flags().BoolVar(&override, "override-retention", false, "Discard a package that is still inside its declared retention period")
	return command
}

func protectOneArgument(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("protect subcommand requires exactly one transfer package directory")
	}
	return nil
}

func protectionFlag(command *cobra.Command, file *string) {
	command.Flags().StringVar(file, "protection", "", "Protection document holding the registered controls")
}

// protectionFile names the protection document a subcommand reads and writes.
// Every subcommand but register refuses a file that does not exist.
func protectionFile(path string) (protect.File, error) {
	if path == "" {
		return protect.File{}, usage("protect requires --protection naming a protection document")
	}
	return protect.File{Path: path}, nil
}

func writeControl(out io.Writer, headline string, entry protect.Control) error {
	return writeLines(out, func(w io.Writer) {
		fmt.Fprintf(w, "%s\n", headline)
		writeControlLines(w, entry, time.Now())
		fmt.Fprintf(w, "Limitations: %s\n", protect.Limitations)
	})
}

// writeControlLines renders one control. The key is masked because this command
// never read one, and the locator arguments are counted rather than echoed: an
// argument is the one place an operator could have put key material, and
// readmit does not repeat it back.
func writeControlLines(w io.Writer, entry protect.Control, now time.Time) {
	fmt.Fprintf(w, "  %s storage=%s (declared, never verified) state=%s generation=%d\n", entry.Name, entry.Storage, entry.State, entry.Generation)
	fmt.Fprintf(w, "    key: %s (never stored, never written, never exported)\n", protect.Mask)
	fmt.Fprintf(w, "    rotated: %s rotation: %s max-age: %s retain: %s\n",
		entry.RotatedAt.UTC().Format(time.RFC3339), entry.Rotation(now), absent(entry.MaxAge), absent(entry.Retain))
	fmt.Fprintf(w, "    command: %s (%d locator arguments)\n", entry.Command, len(entry.Arguments))
}

func writePackageLines(w io.Writer, descriptor protect.Package, now time.Time) {
	fmt.Fprintf(w, "Document: %s\nPackage: %s\nControl: %s\nKey generation: %d\nCreated: %s\nEncryption: %s with %s\nEntries: %d\nRetention: %s\n",
		descriptor.Schema, descriptor.Package, descriptor.Control, descriptor.Generation,
		descriptor.CreatedAt.UTC().Format(time.RFC3339), descriptor.Cipher, descriptor.Derivation,
		len(descriptor.Entries), descriptor.Retention(now))
	if !descriptor.RetainUntil.IsZero() {
		fmt.Fprintf(w, "Retain until: %s\n", descriptor.RetainUntil.UTC().Format(time.RFC3339))
	}
}
