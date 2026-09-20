package cli

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/synth"
	"github.com/spf13/cobra"
)

var synthBaseTimePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func synthCommand() *cobra.Command {
	var seed uint64
	var baseTime, generatorVersion, profileVersion, output string
	cmd := &cobra.Command{
		Use: "synth", Short: "Generate reproducible regression, cancellation, and known-invalid SIU case bundles", Annotations: declare(capabilityAuthor),
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, flag := range []string{"seed", "base-time", "generator-version", "profile-version", "output"} {
				if !cmd.Flags().Changed(flag) {
					return usage("synth requires --seed, --base-time, --generator-version, --profile-version, and --output")
				}
			}
			if output == "" {
				return usage("synthetic family destination cannot be empty")
			}
			base, err := time.Parse(time.RFC3339, baseTime)
			if err != nil || !synthBaseTimePattern.MatchString(baseTime) {
				return usage("base time must be a whole-second RFC3339 timestamp with an explicit timezone")
			}
			manifest, err := synth.Write(output, bundle.GeneratorInputs{
				Seed: seed, BaseTime: base, GeneratorVersion: generatorVersion, ProfileVersion: profileVersion,
			})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Synthetic SIU family: complete\nProvenance: generated\nCases: regression (S12, S13); cancellation (S12, S13, S15); invalid (S12, S13 with an unbooked filler identifier)"); err != nil {
				return errors.New("cannot write synthetic family output")
			}
			for _, c := range manifest.Cases {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", c.Variant, c.Identity); err != nil {
					return errors.New("cannot write synthetic family output")
				}
			}
			return nil
		},
	}
	cmd.Flags().Uint64Var(&seed, "seed", 0, "Explicit deterministic seed, including zero")
	cmd.Flags().StringVar(&baseTime, "base-time", "", "Declared whole-second scenario time in RFC3339 (never inferred from the clock)")
	cmd.Flags().StringVar(&generatorVersion, "generator-version", "", "Implemented generator version: readmit-synth-v1")
	cmd.Flags().StringVar(&profileVersion, "profile-version", "", "Implemented fixture profile: readmit-siu-v1")
	cmd.Flags().StringVar(&output, "output", "", "New family directory containing regression, cancellation, and invalid case bundles")
	return cmd
}
