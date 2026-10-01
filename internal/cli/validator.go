package cli

import (
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/spf13/cobra"
)

// validatorCommand installs, checks and removes the optional local FHIR
// validator on the machine that runs validations, from a package exported on
// the build machine (docs/fhir-validation.md). Nothing here downloads; an
// installation loads only a package whose published identity matches.
func validatorCommand() *cobra.Command {
	root := &cobra.Command{Use: "validator", Short: "Install, check and remove the optional local FHIR validator offline"}
	var socket string
	root.PersistentFlags().StringVar(&socket, "socket", "", "The local container engine's Unix socket; the engine's default when empty")
	engine := func() (*fhirvalidator.Engine, error) { return fhirvalidator.LocalEngine(socket) }

	var identity string
	verify := &cobra.Command{Use: "verify PACKAGE", Short: "Verify a validator package offline against its published identity", Args: cobra.ExactArgs(1), Annotations: declare(capabilityFree),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := fhirvalidator.VerifyPackage(args[0], identity)
			if err != nil {
				return err
			}
			return writeJSON(cmd, validatorPackageView(p))
		}}
	verify.Flags().StringVar(&identity, "identity", "", "The package identity its administrator published")

	var installIdentity, output string
	install := &cobra.Command{Use: "install PACKAGE", Short: "Verify a validator package and install it into the local container engine", Args: cobra.ExactArgs(1), Annotations: declareInterruptible(capabilityFree),
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				return usage("install requires --output, a new folder for the installed capability")
			}
			e, err := engine()
			if err != nil {
				return err
			}
			defer e.Close()
			c, err := e.InstallPackage(cmd.Context(), args[0], installIdentity, output)
			if err != nil {
				return err
			}
			return writeJSON(cmd, validatorStateView{Schema: validatorStateSchema, Capability: c.Identity(), State: fhirvalidator.StateReady})
		}}
	install.Flags().StringVar(&installIdentity, "identity", "", "The package identity its administrator published")
	install.Flags().StringVar(&output, "output", "", "A new folder for the installed capability")

	check := &cobra.Command{Use: "check CAPABILITY", Short: "Check an installed validator against its pins and the local container engine", Args: cobra.ExactArgs(1), Annotations: declare(capabilityFree),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, status := fhirvalidator.CheckInstalled(cmd.Context(), args[0], socket)
			if status.State != fhirvalidator.StateReady {
				return status
			}
			return writeJSON(cmd, validatorStateView{Schema: validatorStateSchema, Capability: c.Identity(), State: fhirvalidator.StateReady})
		}}

	var keep []string
	remove := &cobra.Command{Use: "remove CAPABILITY", Short: "Remove an installed validator's worker image and folder", Args: cobra.ExactArgs(1), Annotations: declareInterruptible(capabilityFree),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := fhirvalidator.OpenCapability(args[0])
			if err != nil {
				return fhirvalidator.Status{State: fhirvalidator.StateCapabilityUnavailable, Requirement: "name an installed validator folder"}
			}
			e, err := engine()
			if err != nil {
				return err
			}
			defer e.Close()
			if err := e.RemoveInstalled(cmd.Context(), args[0], keep...); err != nil {
				return err
			}
			return writeJSON(cmd, validatorStateView{Schema: validatorStateSchema, Capability: c.Identity(), State: validatorRemoved})
		}}
	remove.Flags().StringArrayVar(&keep, "keep", nil, "An installed validator that stays; its image is kept when it is the same")

	root.AddCommand(verify, install, check, remove)
	return root
}

// validatorStateSchema is the answer of install, check and remove: the
// capability identity and its state, ready or removed.
const validatorStateSchema = "readmit-fhir-validator-state/v1"

// validatorRemoved is the state remove answers.
const validatorRemoved = "removed"

type validatorStateView struct {
	Schema     string `json:"schema"`
	Capability string `json:"capability"`
	State      string `json:"state"`
}

// validatorPackageSchema is what verify answers about a package.
const validatorPackageSchema = "readmit-fhir-validator-package-check/v1"

type validatorPackageVersions struct {
	Schema     string                     `json:"schema"`
	Package    string                     `json:"package"`
	Capability string                     `json:"capability"`
	Platform   string                     `json:"platform"`
	Validator  string                     `json:"validator"`
	Runtime    string                     `json:"runtime"`
	Packages   []fhirvalidator.PackageRef `json:"packages"`
}

func validatorPackageView(p *fhirvalidator.VerifiedPackage) validatorPackageVersions {
	m := p.Capability.Manifest()
	view := validatorPackageVersions{Schema: validatorPackageSchema, Package: p.Identity, Capability: p.Capability.Identity(), Platform: m.Platform, Validator: m.Validator.Version, Runtime: m.Runtime.Version, Packages: []fhirvalidator.PackageRef{}}
	for _, pkg := range m.Packages {
		view.Packages = append(view.Packages, fhirvalidator.PackageRef{ID: pkg.ID, Version: pkg.Version})
	}
	return view
}
