package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/synth"
	"github.com/spf13/cobra"
)

// sampleCommand is the ungated frozen walkthrough, not an admission override.
// Every byte/seed/profile/selector is pinned here or, for the capture, in the
// shared operation the window's guided sample runs too; arbitrary authoring
// continues through the ordinary gated commands.
func sampleCommand() *cobra.Command {
	root := &cobra.Command{Use: "sample", Short: "Prepare only the frozen synthetic walkthrough without activation"}
	var output, fixtures string
	generate := &cobra.Command{Use: "synth", Args: cobra.NoArgs, Annotations: declare(capabilityFree), RunE: func(cmd *cobra.Command, _ []string) error {
		manifest, err := synth.Write(output, bundle.GeneratorInputs{Seed: 0, BaseTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), GeneratorVersion: "readmit-synth-v1", ProfileVersion: "readmit-siu-v1"})
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintln(cmd.OutOrStdout(), "Synthetic SIU family: complete\nProvenance: generated\nCases: regression (S12, S13); cancellation (S12, S13, S15); invalid (S12, S13 with an unbooked filler identifier)"); err != nil {
			return errors.New("cannot write sample output")
		}
		for _, c := range manifest.Cases {
			if _, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", c.Variant, c.Identity); err != nil {
				return errors.New("cannot write sample output")
			}
		}
		return nil
	}}
	generate.Flags().StringVar(&output, "output", "", "New frozen family directory")
	capture := &cobra.Command{Use: "capture", Args: cobra.NoArgs, Annotations: declare(capabilityFree), RunE: func(cmd *cobra.Command, _ []string) error {
		b, err := operation.CaptureSample(fixtures, output, time.Now().UTC())
		if err != nil {
			return err
		}
		return renderBundle(cmd.OutOrStdout(), b, false, false)
	}}
	capture.Flags().StringVar(&fixtures, "fixtures", "", "Directory containing the two frozen synthetic receiver fixtures")
	capture.Flags().StringVar(&output, "output", "", "New sample capture directory")
	build := &cobra.Command{Use: "index CASE", Args: cobra.ExactArgs(1), Annotations: declare(capabilityFree), RunE: func(cmd *cobra.Command, args []string) error {
		b, err := bundle.Open(args[0])
		if err != nil {
			return err
		}
		if b.Identity != "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df" {
			return errors.New("sample index requires the frozen seed-zero regression case")
		}
		policy, err := declaredPolicy([]string{"SCH-2", "MSH-10"}, "states", index.Indefinite)
		if err != nil {
			return err
		}
		at := time.Now().UTC()
		doc, err := index.Build(cmd.Context(), b, policy, at)
		if err != nil {
			return err
		}
		if _, err = index.Write(output, doc); err != nil {
			return err
		}
		return writeIndex(cmd.OutOrStdout(), "Index written", doc, at)
	}}
	build.Flags().StringVar(&output, "output", "", "New states-only frozen sample index")
	root.AddCommand(generate, capture, build)
	return root
}
