package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/spf13/cobra"
)

// The scan reads local evidence and configuration in bounded amounts. A tree
// larger than this is scanned in parts rather than reported as clean.
const (
	maxScanFiles      = 8192
	maxScanFileBytes  = 16 << 20
	maxScanTotalBytes = 256 << 20
)

func secretCommand(ran *bool) *cobra.Command {
	command := &cobra.Command{
		Use:   "secret",
		Short: "Reference credentials that stay in an OS or customer-managed secret store",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("secret requires a subcommand: add, update, rotate, show, or scan")
		},
	}
	command.AddCommand(secretAdd(ran), secretUpdate(ran), secretRotate(ran), secretShow(ran), secretScan(ran))
	return command
}

func secretAdd(ran *bool) *cobra.Command {
	var file, name, store, purpose, address, program, maxAge string
	var arguments []string
	command := &cobra.Command{
		Use:   "add --secrets FILE --name NAME --store KIND --address HOST:PORT --command PROGRAM",
		Short: "Register a reference to a credential held in a secret store",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			*ran = true
			document, err := openOrEmptyStore(file)
			if err != nil {
				return err
			}
			updated, stored, err := secret.Add(document, secret.Reference{
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
			if err := secret.WriteStore(file, updated); err != nil {
				return err
			}
			return writeReference(cmd.OutOrStdout(), "Credential reference registered: "+stored.Name, stored)
		},
	}
	secretsFlag(command, &file)
	command.Flags().StringVar(&name, "name", "", "Name this reference is used under")
	command.Flags().StringVar(&store, "store", "", "Declared storage: os-keychain or customer-managed")
	command.Flags().StringVar(&purpose, "purpose", string(secret.MLLPEndpoint), "The single use this credential may be bound to")
	command.Flags().StringVar(&address, "address", "", "The one endpoint address this credential may be presented to")
	command.Flags().StringVar(&program, "command", "", "Absolute path of the program that reads the credential from its store")
	command.Flags().StringArrayVar(&arguments, "argument", nil, "Locator argument for that program; repeat for more. Never a credential")
	command.Flags().StringVar(&maxAge, "max-age", "", "How long a recorded rotation stays current, as a Go duration")
	return command
}

func secretUpdate(ran *bool) *cobra.Command {
	var file, name, store, address, program, maxAge string
	var arguments []string
	command := &cobra.Command{
		Use:   "update --secrets FILE --name NAME",
		Short: "Change where a registered reference reads its credential from, or what it may be presented to",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			*ran = true
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
				return errors.New("secret update requires at least one change")
			}
			document, err := readStore(file)
			if err != nil {
				return err
			}
			updated, stored, err := secret.Update(document, name, change)
			if err != nil {
				return err
			}
			if err := secret.WriteStore(file, updated); err != nil {
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

func secretRotate(ran *bool) *cobra.Command {
	var file, name string
	command := &cobra.Command{
		Use:   "rotate --secrets FILE --name NAME",
		Short: "Record that the credential behind a reference was replaced in its store",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			*ran = true
			document, err := readStore(file)
			if err != nil {
				return err
			}
			entry, err := secret.Find(document, name)
			if err != nil {
				return err
			}
			// The store must answer for this reference before a rotation is
			// recorded, so a generation is never recorded against a credential
			// readmit cannot read. The value itself is discarded here.
			if _, err := secret.Resolve(cmd.Context(), entry); err != nil {
				return errors.New("the credential did not resolve from its declared store; the recorded rotation is unchanged")
			}
			updated, stored, err := secret.Rotate(document, name, time.Now())
			if err != nil {
				return err
			}
			if err := secret.WriteStore(file, updated); err != nil {
				return err
			}
			return writeReference(cmd.OutOrStdout(), "Rotation recorded: "+stored.Name, stored)
		},
	}
	secretsFlag(command, &file)
	command.Flags().StringVar(&name, "name", "", "Registered reference whose credential was replaced")
	return command
}

func secretShow(ran *bool) *cobra.Command {
	var file string
	command := &cobra.Command{
		Use:   "show --secrets FILE",
		Short: "Show every registered reference, its scope and its rotation state, with the credential masked",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			*ran = true
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

func secretScan(ran *bool) *cobra.Command {
	var file, name string
	command := &cobra.Command{
		Use:   "scan --secrets FILE PATH...",
		Short: "Check configuration, manifests, reports, logs and local browser state for a known credential value",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("secret scan requires at least one file or directory to check")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			document, err := readStore(file)
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			references := document.References
			if name != "" {
				entry, err := secret.Find(document, name)
				if err != nil {
					return &ExitError{Code: 2, Err: err}
				}
				references = []secret.Reference{entry}
			}
			if len(references) == 0 {
				return &ExitError{Code: 2, Err: errors.New("the secret reference document registers nothing to check for")}
			}
			terms := make([][]byte, 0, len(references))
			for _, entry := range references {
				resolved, err := secret.Resolve(cmd.Context(), entry)
				if err != nil {
					return &ExitError{Code: 2, Err: errors.New("a registered credential did not resolve, so this scan checked nothing")}
				}
				terms = append(terms, resolved.Expose())
			}
			// The secret reference document is always checked: a store that
			// carries the value it references is the leak this command exists
			// to find, and a shared configuration is the first place to look.
			files, err := collectScanFiles(append([]string{file}, args...))
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			scan := exportreview.Residual(files, terms)
			if err := writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				fmt.Fprintf(w, "Credential leakage scan\nReferences checked: %d\nFiles checked: %d\nStatus: %s\nLocations holding a known credential value: %d\n",
					scan.KnownValues, scan.Files, scan.Status, len(scan.Locations))
				for _, location := range scan.Locations {
					fmt.Fprintf(w, "  %s\n", location)
				}
				fmt.Fprintf(w, "Limitations: %s\n", scan.Limitations)
			}); err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			if len(scan.Locations) > 0 {
				return &ExitError{Code: 1, Err: errors.New("a checked location holds a known credential value")}
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
		return secret.Document{}, errors.New("secret requires --secrets naming a secret reference document")
	}
	return secret.ReadStore(path)
}

// openOrEmptyStore reads an existing document, or starts the first one. A path
// that exists but cannot be read as this contract is reported, never replaced.
func openOrEmptyStore(path string) (secret.Document, error) {
	if path == "" {
		return secret.Document{}, errors.New("secret requires --secrets naming a secret reference document")
	}
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return secret.Document{Schema: secret.Schema}, nil
	}
	return secret.ReadStore(path)
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

// collectScanFiles reads the bounded contents of every regular file under the
// named paths. Symbolic links are skipped rather than followed, so a scan
// reports on the tree it was pointed at and cannot be redirected out of it.
func collectScanFiles(roots []string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	total := 0
	for _, root := range roots {
		resolved, err := artifactpath.Resolve(root)
		if err != nil {
			return nil, errors.New("a path to check could not be resolved")
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, errors.New("a path to check could not be read")
		}
		if !info.IsDir() {
			if err := addScanFile(files, root, resolved, info, &total); err != nil {
				return nil, err
			}
			continue
		}
		walkErr := filepath.WalkDir(resolved, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return errors.New("a path to check could not be read")
			}
			if entry.IsDir() || !entry.Type().IsRegular() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return errors.New("a path to check could not be read")
			}
			relative, err := filepath.Rel(resolved, path)
			if err != nil {
				return errors.New("a path to check could not be read")
			}
			return addScanFile(files, root+"/"+filepath.ToSlash(relative), path, info, &total)
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	return files, nil
}

func addScanFile(files map[string][]byte, name, path string, info os.FileInfo, total *int) error {
	if !info.Mode().IsRegular() {
		return nil
	}
	if info.Size() > maxScanFileBytes {
		return errors.New("a file to check exceeds the scan's size limit")
	}
	if len(files) >= maxScanFiles || *total+int(info.Size()) > maxScanTotalBytes {
		return errors.New("the paths to check exceed the scan's limits; check them in parts")
	}
	data, err := readInputFile(path, maxScanFileBytes)
	if err != nil {
		return errors.New("a file to check could not be read")
	}
	*total += len(data)
	files[strings.TrimPrefix(name, "./")] = data
	return nil
}
