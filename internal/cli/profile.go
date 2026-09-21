package cli

import (
	"fmt"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/profilepackage"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/spf13/cobra"
)

func profileCommand() *cobra.Command {
	command := &cobra.Command{Use: "profile", Short: "Import and export reviewed reusable interface metadata"}
	var pack, version, origin, output string
	var reviewed bool
	export := &cobra.Command{Use: "export PROFILE --pack PACK --version SEAL --origin ORIGIN --output PACKAGE --reviewed", Short: "Copy an existing sealed local profile and its pinned metadata into a package", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if pack == "" || version == "" || origin == "" || output == "" || !reviewed {
			return usage("profile export requires --pack, --version, --origin, --output and --reviewed")
		}
		p, err := readInputFile(args[0], localprofile.MaxProfileBytes)
		if err != nil {
			return err
		}
		b, err := readInputFile(pack, profilepack.MaxPackBytes)
		if err != nil {
			return err
		}
		v, err := readInputFile(version, profileversion.MaxVersionBytes)
		if err != nil {
			return err
		}
		o, err := readInputFile(origin, profilepackage.MaxOriginBytes)
		if err != nil {
			return err
		}
		data, err := profilepackage.Export(p, b, v, o)
		if err != nil {
			return err
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		if err = writeNewFile(output, data, "cannot create profile package; destination must be new", "cannot write profile package"); err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "Exported reviewed profile metadata. Integrity is not source authentication or a conformance verdict.")
		return err
	}}
	export.Flags().StringVar(&pack, "pack", "", "Exactly pinned metadata pack")
	export.Flags().StringVar(&version, "version", "", "Existing profile version seal")
	export.Flags().StringVar(&origin, "origin", "", "Local profile source, license notice, mapping limits and review record")
	export.Flags().StringVar(&output, "output", "", "New package file")
	export.Flags().BoolVar(&reviewed, "reviewed", false, "Confirm metadata and notices were reviewed for disclosure; no patient data or secrets")
	var destination string
	importCommand := &cobra.Command{Use: "import PACKAGE --output NEW_DIRECTORY", Short: "Verify and copy package documents without activating or upgrading a profile", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if destination == "" {
			return usage("profile import requires --output")
		}
		data, err := readInputFile(args[0], profilepackage.MaxBytes)
		if err != nil {
			return err
		}
		if err = profilepackage.Import(cmd.Context(), destination, data); err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "Imported verified profile metadata. No project changed, no tests repinned, no message conformance evaluated.")
		return err
	}}
	importCommand.Flags().StringVar(&destination, "output", "", "New private directory for package and independently readable documents")
	command.AddCommand(export, importCommand)
	return command
}
