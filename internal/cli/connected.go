package cli

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/spf13/cobra"
)

func connectedCommand() *cobra.Command {
	root := &cobra.Command{Use: "connected", Short: "Prepare and inspect connected execution contracts"}
	var seed uint64
	var base string
	prepare := &cobra.Command{Use: "prepare INPUT_DIRECTORY OUTPUT", Short: "Compile local inputs into an immutable plan without connecting", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(2), RunE: func(c *cobra.Command, a []string) error {
		if !c.Flags().Changed("seed") || !c.Flags().Changed("base-time") {
			return usage("connected prepare requires explicit --seed and --base-time")
		}
		raw, readErr := (artifactdir.Document{MaxBytes: connectedtest.MaxBytes}).Read(filepath.Join(a[0], "test.json"))
		var head struct {
			Schema string `json:"schema"`
		}
		if readErr == nil && json.Unmarshal(raw, &head) == nil && head.Schema == connectedtest.FlowTestSchema {
			p, err := connectedtest.PrepareFlowDirectory(a[0], connectedtest.Generation{Seed: seed, BaseTime: base})
			if err != nil {
				return refusal(err)
			}
			if err = p.Write(c.Context(), a[1]); err != nil {
				return refusal(err)
			}
			_, err = fmt.Fprintln(c.OutOrStdout(), p.Identity())
			return err
		}
		p, err := connectedtest.PrepareDirectory(a[0], connectedtest.Generation{Seed: seed, BaseTime: base})
		if err != nil {
			return refusal(err)
		}
		if err := p.Write(c.Context(), a[1]); err != nil {
			return refusal(err)
		}
		_, err = fmt.Fprintln(c.OutOrStdout(), p.Identity())
		return err
	}}
	prepare.Flags().Uint64Var(&seed, "seed", 0, "Explicit synthetic generation seed")
	prepare.Flags().StringVar(&base, "base-time", "", "Explicit generation base instant in RFC3339")
	var convertSeed uint64
	var convertBase, project, id, revision string
	convert := &cobra.Command{Use: "convert LEGACY_SPEC CHECKS OUTPUT", Short: "Create an explicit connected revision with legacy ancestry and independent checks", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(3), RunE: func(c *cobra.Command, a []string) error {
		if !c.Flags().Changed("seed") || !c.Flags().Changed("base-time") {
			return usage("connected convert requires explicit --seed and --base-time")
		}
		b, err := (artifactdir.Document{MaxBytes: assertion.MaxSetBytes}).Read(a[1])
		if err != nil {
			return refusal(err)
		}
		p, err := connectedtest.ConvertLegacy(a[0], project, id, revision, b, connectedtest.Generation{Seed: convertSeed, BaseTime: convertBase})
		if err != nil {
			return refusal(err)
		}
		if err := p.Write(c.Context(), a[2]); err != nil {
			return refusal(err)
		}
		_, err = fmt.Fprintln(c.OutOrStdout(), p.Identity())
		return err
	}}
	convert.Flags().Uint64Var(&convertSeed, "seed", 0, "Explicit synthetic generation seed")
	convert.Flags().StringVar(&convertBase, "base-time", "", "Explicit generation base instant")
	convert.Flags().StringVar(&project, "project", "", "Project identifier")
	convert.Flags().StringVar(&id, "id", "", "New logical test identifier")
	convert.Flags().StringVar(&revision, "revision", "", "New logical revision")
	var send bool
	var instance, output string
	run := &cobra.Command{Use: "run PLAN LEGACY_SPEC", Short: "Execute the exact legacy ACK fixture adapter", Annotations: declareInterruptible(capabilityExecuteIfSend), Args: cobra.ExactArgs(2), RunE: func(c *cobra.Command, a []string) error {
		if !send || instance == "" || output == "" {
			return usage("connected run requires --send, --instance and --output")
		}
		p, err := connectedtest.OpenPlan(a[0])
		if err != nil {
			return refusal(err)
		}
		r, err := connectedtest.ExecuteLegacy(c.Context(), p, a[1], instance, output)
		if err != nil {
			return refusal(err)
		}
		if err := writeConnected(c, r.Document()); err != nil {
			return err
		}
		if r.Document().Verdict != "pass" {
			return verdict(1, errors.New("connected fixture did not pass"))
		}
		return nil
	}}
	run.Flags().BoolVar(&send, "send", false, "Explicitly send to the exact prepared legacy target")
	run.Flags().StringVar(&instance, "instance", "", "Distinct execution instance identifier")
	run.Flags().StringVar(&output, "output", "", "New customer-local result directory")
	show := &cobra.Command{Use: "show RESULT", Short: "Verify and re-evaluate a retained result offline", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
		r, err := connectedtest.OpenResult(c.Context(), a[0])
		if err != nil {
			return refusal(err)
		}
		return writeConnected(c, r.Document())
	}}
	root.AddCommand(prepare, convert, run, show, connectedProfileCommand(), connectedTransportCommand(), connectedIsolationCommand())
	return root
}
func writeConnected(c *cobra.Command, v any) error {
	b, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return refusal(err)
	}
	_, err = fmt.Fprintf(c.OutOrStdout(), "%s\n", b)
	return err
}
