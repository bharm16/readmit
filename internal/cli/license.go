package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/spf13/cobra"
)

func licenseCommand() *cobra.Command {
	command := &cobra.Command{
		Use:         "license",
		Annotations: declare(capabilityFree),
		Short:       "Verify and manage an offline organization entitlement",
		Args:        cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return usage("license requires a subcommand: verify, import, show, renew, export, release, or runner")
		},
	}
	command.AddCommand(licenseOperation(), licenseVerify(), licenseImport(), licenseShow(), licenseRenew(), licenseExport(), licenseRelease(), licenseRunner())
	return command
}

func licenseVerify() *cobra.Command {
	var trustPath, author, device, require string
	command := &cobra.Command{
		Use:         "verify ENTITLEMENT --trust TRUST_STORE",
		Annotations: declare(capabilityFree),
		Short:       "Verify a received entitlement locally against trusted signing keys",
		Args:        licenseOneArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			trust, err := readTrust(trustPath)
			if err != nil {
				return err
			}
			data, err := readInputFile(args[0], entitlement.MaxDocumentBytes)
			if err != nil {
				return err
			}
			version, err := entitlement.DeclaredVersion(data)
			if err != nil {
				return err
			}
			if version == entitlement.SchemaV2 {
				grant, err := entitlement.VerifyV2(data, trust)
				if err != nil {
					return err
				}
				return writeGrantV2(cmd.OutOrStdout(), "Entitlement verified: "+grant.Claims.ID, grant, author, device, require, "")
			}
			if author != "" {
				return errors.New("a v1 entitlement binds devices, not named authors; --author applies to a v2 entitlement")
			}
			grant, err := entitlement.Verify(data, trust)
			if err != nil {
				return err
			}
			return writeGrant(cmd.OutOrStdout(), "Entitlement verified: "+grant.Claims.ID, grant, device, require, "")
		},
	}
	command.Flags().StringVar(&trustPath, "trust", "", "Trust store of vendor signing keys to verify against")
	command.Flags().StringVar(&author, "author", "", "Report the assignment a v2 entitlement makes to a named author")
	command.Flags().StringVar(&device, "device", "", "Report the activation this entitlement binds to a device identifier (with --author under v2)")
	command.Flags().StringVar(&require, "require", "", "Refuse unless the entitlement grants this capability now")
	return command
}

func licenseImport() *cobra.Command {
	var trustPath, author, device, output string
	command := &cobra.Command{
		Use:         "import ENTITLEMENT --trust TRUST_STORE --device ID [--output NEW_DIRECTORY]",
		Annotations: declare(capabilityFree),
		Short:       "Verify a received entitlement and install it for one device",
		Args:        licenseOneArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			if device == "" {
				return usage("license import requires --device with the identifier this entitlement names")
			}
			trust, err := readTrust(trustPath)
			if err != nil {
				return err
			}
			data, err := readInputFile(args[0], entitlement.MaxDocumentBytes)
			if err != nil {
				return err
			}
			version, err := entitlement.DeclaredVersion(data)
			if err != nil {
				return err
			}
			if version == entitlement.SchemaV2 && author == "" {
				return usage("license import of a v2 entitlement requires --author with the named author this device is assigned to")
			}
			if version != entitlement.SchemaV2 && author != "" {
				return errors.New("a v1 entitlement binds devices, not named authors; --author applies to a v2 entitlement")
			}
			if output == "" {
				return installLicense(cmd.OutOrStdout(), data, trustPath, trust, author, device)
			}
			if version == entitlement.SchemaV2 {
				store, err := entitlement.ImportV2(output, data, trust, author, device, licenseNow())
				if err != nil {
					return err
				}
				return storeV2{store}.report(cmd.OutOrStdout(), "Entitlement installed: "+store.Claims.ID, trust, "")
			}
			store, err := entitlement.Import(output, data, trust, device, licenseNow())
			if err != nil {
				return err
			}
			return storeV1{store}.report(cmd.OutOrStdout(), "Entitlement installed: "+store.Claims.ID, trust, "")
		},
	}
	command.Flags().StringVar(&trustPath, "trust", "", "Trust store of vendor signing keys to verify against")
	command.Flags().StringVar(&author, "author", "", "Named author to activate for, which a v2 entitlement must assign this device to")
	command.Flags().StringVar(&device, "device", "", "Device identifier to activate, which the entitlement must name")
	command.Flags().StringVar(&output, "output", "", "New entitlement store directory (never overwrite); without it, install as this computer's license")
	return command
}

func licenseShow() *cobra.Command {
	var trustPath, require string
	command := &cobra.Command{
		Use:         "show [STORE] --trust TRUST_STORE",
		Annotations: declare(capabilityFree),
		Short:       "Report the installed entitlement, its term state and its scope",
		Args:        licenseOptionalStore,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, installed, err := licenseStore(args)
			if err != nil {
				return err
			}
			if installed && trustPath == "" {
				trustPath = operationguard.InstalledTrustIn(path)
			}
			trust, err := readTrust(trustPath)
			if err != nil {
				return err
			}
			store, err := openStore(path)
			if err != nil {
				return err
			}
			return store.report(cmd.OutOrStdout(), "Entitlement installed: "+store.id(), trust, require)
		},
	}
	command.Flags().StringVar(&trustPath, "trust", "", "Trust store of vendor signing keys to verify against")
	command.Flags().StringVar(&require, "require", "", "Refuse unless the entitlement grants this capability now")
	return command
}

func licenseRenew() *cobra.Command {
	var trustPath string
	command := &cobra.Command{
		Use:         "renew [STORE] ENTITLEMENT --trust TRUST_STORE",
		Annotations: declare(capabilityFree),
		Short:       "Install a later issue of the same entitlement for this device",
		Args:        licenseTwoArguments,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, installed, err := licenseStore(args[:len(args)-1])
			if err != nil {
				return err
			}
			if installed {
				return renewInstalledLicense(cmd.OutOrStdout(), path, args[len(args)-1], trustPath)
			}
			trust, err := readTrust(trustPath)
			if err != nil {
				return err
			}
			data, err := readInputFile(args[1], entitlement.MaxDocumentBytes)
			if err != nil {
				return err
			}
			store, err := openStore(path)
			if err != nil {
				return err
			}
			if err := store.renew(data, trust); err != nil {
				return err
			}
			return store.report(cmd.OutOrStdout(), "Entitlement renewed: "+store.id(), trust, "")
		},
	}
	command.Flags().StringVar(&trustPath, "trust", "", "Trust store of vendor signing keys to verify against")
	return command
}

func licenseExport() *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:         "export [STORE] --output NEW_FILE",
		Annotations: declare(capabilityFree),
		Short:       "Write the installed entitlement back out, byte for byte",
		Args:        licenseOptionalStore,
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				return usage("license export requires --output with a new file")
			}
			path, _, err := licenseStore(args)
			if err != nil {
				return err
			}
			store, err := openStore(path)
			if err != nil {
				return err
			}
			if err := store.export(output); err != nil {
				return err
			}
			return writeLicense(cmd.OutOrStdout(), func(w io.Writer) {
				fmt.Fprintf(w, "Entitlement exported: %s\nDocument: %s\nBytes: exactly as received; the exported file verifies because it is the same file\n",
					store.id(), store.schema())
			})
		},
	}
	command.Flags().StringVar(&output, "output", "", "New entitlement file (never overwrite)")
	return command
}

func licenseRelease() *cobra.Command {
	command := &cobra.Command{
		Use:         "release [STORE]",
		Annotations: declare(capabilityFree),
		Short:       "Release this device's activation so the seat can be reissued",
		Args:        licenseOptionalStore,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, installed, err := licenseStore(args)
			if err != nil {
				return err
			}
			if installed {
				// This computer's license stops admitting new work before its
				// store records the release, in the window and here alike.
				if err := operationguard.ReleaseInstalledLicense(path, licenseNow()); err != nil {
					return err
				}
			}
			store, err := openStore(path)
			if err != nil {
				return err
			}
			if !installed {
				if err := store.release(licenseNow()); err != nil {
					return err
				}
			}
			return writeLicense(cmd.OutOrStdout(), func(w io.Writer) {
				fmt.Fprintf(w, "Activation released: %s\nEntitlement: %s\nReleased: %s\nTransfer: ask the vendor to reissue this entitlement for the new device, then import it there\nLocal record: releasing is recorded here, not proven to the vendor; seat accounting is settled when the vendor reissues\n",
					store.activated(), store.id(), licenseTimestamp(store.released()))
			})
		},
	}
	return command
}

// licenseNow is the instant every license command records and decides against:
// UTC, whole seconds, read once per report so one invocation cannot straddle a
// second boundary and describe two different states. A term state is only ever
// as trustworthy as this clock; readmit keeps no hidden monotonic record.
func licenseNow() time.Time { return time.Now().UTC().Truncate(time.Second) }

func licenseTimestamp(value time.Time) string { return value.Format(time.RFC3339) }

// writeLicense flushes one command's report. It is separate from the project
// writer so a failure here names the command that failed.
func writeLicense(out io.Writer, render func(io.Writer)) error {
	w := bufio.NewWriter(out)
	render(w)
	if err := w.Flush(); err != nil {
		return errors.New("cannot write license output")
	}
	return nil
}

func readTrust(path string) (entitlement.Trust, error) {
	if path == "" {
		return entitlement.Trust{}, usage("license requires --trust with the vendor signing keys to verify against")
	}
	data, err := readInputFile(path, entitlement.MaxDocumentBytes)
	if err != nil {
		return entitlement.Trust{}, err
	}
	return entitlement.DecodeTrust(data)
}

func activation(store *entitlement.Store) string {
	if !store.Activation.Released.IsZero() {
		return "released " + licenseTimestamp(store.Activation.Released)
	}
	return "imported " + licenseTimestamp(store.Activation.Imported)
}

// writeGrant reports one verified entitlement. Every line is data the document
// declares or a state this release decided from it; nothing is inferred, and
// the offline limit of local verification is stated rather than implied.
func writeGrant(out io.Writer, headline string, grant entitlement.Grant, device, require, installed string) error {
	claims := grant.Claims
	at := licenseNow()
	binding := "not selected"
	if device != "" {
		bound, err := grant.Device(device)
		if err != nil {
			return err
		}
		binding = bound.ID + " (" + string(bound.Kind) + ")"
	}
	if require != "" {
		if err := grant.Allows(require, at); err != nil {
			return err
		}
	}
	return writeLicense(out, func(w io.Writer) {
		fmt.Fprintf(w, "%s\nDocument: %s\nOrganization: %s\nPlan: %s\nIssue sequence: %d\nSigned by: %s (%s, %s)\n",
			headline, entitlement.Schema, claims.Organization, claims.Plan, claims.Sequence, grant.KeyID, entitlement.Algorithm, grant.KeyStatus)
		fmt.Fprintf(w, "Term: %s to %s\nGrace: %s\nState: %s\n",
			licenseTimestamp(claims.NotBefore), licenseTimestamp(claims.Expires), grace(claims.GraceDays, claims.GraceEnds()), grant.StateAt(at))
		fmt.Fprintf(w, "Scope: %d seats, %d runners\nBound devices: %s\nCapabilities: %s\nDevice: %s\n",
			claims.Scope.Seats, claims.Scope.Runners, devices(claims.Scope.Devices), list(claims.Capabilities), binding)
		writeGrantTrailer(w, installed, require)
	})
}

// grace renders the issuer's configured window, which both contract versions
// carry the same way.
func grace(days int, ends time.Time) string {
	if days > 0 {
		return strconv.Itoa(days) + " days, through " + licenseTimestamp(ends)
	}
	return "none; the term ends at expiry"
}

// writeGrantTrailer closes every grant report the same way, whichever version
// it reports: the local activation, the required capability, and the two
// limits every entitlement shares.
func writeGrantTrailer(w io.Writer, installed, require string) {
	if installed != "" {
		fmt.Fprintf(w, "Activation: %s\n", installed)
	}
	if require != "" {
		fmt.Fprintf(w, "Required capability: %s (granted)\n", require)
	}
	fmt.Fprint(w, "Evidence: read, verification and export never consult an entitlement; expiry withdraws capabilities only\n")
	fmt.Fprint(w, "Offline limit: a revocation issued after this document was signed cannot be observed locally\n")
}

func devices(bound []entitlement.Device) string {
	if len(bound) == 0 {
		return "none"
	}
	named := make([]string, len(bound))
	for i, device := range bound {
		named[i] = device.ID + " (" + string(device.Kind) + ")"
	}
	return strings.Join(named, ", ")
}

func licenseOneArgument(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("license subcommand requires exactly one file or directory")
	}
	return nil
}

func licenseTwoArguments(_ *cobra.Command, args []string) error {
	if len(args) != 1 && len(args) != 2 {
		return errors.New("license renew requires one received entitlement, after an entitlement store unless it renews this computer's license")
	}
	return nil
}

// licenseOptionalStore accepts one named store, or none for this computer's
// license.
func licenseOptionalStore(_ *cobra.Command, args []string) error {
	if len(args) > 1 {
		return errors.New("license subcommand takes at most one entitlement store; without one it acts on this computer's license")
	}
	return nil
}

// licenseStore names the store a command acts on: the one named, or this
// computer's license when none is. installed reports that the store is this
// computer's license, named or not, so its release and renewal keep the
// operation state and trust beside it in step with the store.
func licenseStore(args []string) (path string, installed bool, err error) {
	root, rootErr := operationguard.InstalledLicensePath()
	if len(args) == 1 {
		return args[0], rootErr == nil && samePath(args[0], root), nil
	}
	if rootErr != nil {
		return "", false, rootErr
	}
	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
		return "", false, operationguard.ErrNoLicense
	}
	return root, true, nil
}

func samePath(a, b string) bool {
	resolved := func(path string) string {
		if real, err := filepath.EvalSymlinks(path); err == nil {
			path = real
		}
		if absolute, err := filepath.Abs(path); err == nil {
			path = absolute
		}
		return filepath.Clean(path)
	}
	return resolved(a) == resolved(b)
}

// installLicense installs a received entitlement as this computer's license:
// the store `license import --output` writes, in this account's configuration
// directory, with the trust store beside it and, under v2, activated for new
// work through the shared operation guard. The report is import's own.
func installLicense(out io.Writer, data []byte, trustPath string, trust entitlement.Trust, author, device string) error {
	root, err := operationguard.InstalledLicensePath()
	if err != nil {
		return err
	}
	trustData, err := readInputFile(trustPath, entitlement.MaxDocumentBytes)
	if err != nil {
		return err
	}
	if err := operationguard.InstallLicense(root, data, trustData, author, device, "", licenseNow()); err != nil {
		return err
	}
	store, err := openStore(root)
	if err != nil {
		return err
	}
	return store.report(out, "Entitlement installed: "+store.id(), trust, "")
}

// renewInstalledLicense renews this computer's license in place. A named
// trust store verifies the later issue and replaces the one installed beside
// it; without one the installed trust store verifies it. The report is
// renew's own.
func renewInstalledLicense(out io.Writer, root, received, trustPath string) error {
	data, err := readInputFile(received, entitlement.MaxDocumentBytes)
	if err != nil {
		return err
	}
	var trustData []byte
	if trustPath != "" {
		if trustData, err = readInputFile(trustPath, entitlement.MaxDocumentBytes); err != nil {
			return err
		}
	}
	if err := operationguard.RenewInstalledLicense(root, data, trustData); err != nil {
		return err
	}
	trust, err := readTrust(operationguard.InstalledTrustIn(root))
	if err != nil {
		return err
	}
	store, err := openStore(root)
	if err != nil {
		return err
	}
	return store.report(out, "Entitlement renewed: "+store.id(), trust, "")
}
