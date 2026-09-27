package cli

import (
	"encoding/json/v2"
	"errors"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/testisolation"
	"github.com/spf13/cobra"
)

func connectedIsolationCommand() *cobra.Command {
	root := &cobra.Command{Use: "isolation", Short: "Review and execute scoped synthetic fixture setup and cleanup"}
	var registry, policy, parent, instance, authorityPath, output, prior, phase string
	var seed uint64
	var execute bool
	var confirmed []string
	prepare := func(cmd *cobra.Command, args []string) (*testisolation.Prepared, error) {
		if registry == "" || policy == "" || parent == "" || instance == "" || !cmd.Flags().Changed("seed") {
			return nil, usage("isolation requires --registry, --policy, --plan, --instance and explicit --seed")
		}
		plan, err := connectedtest.OpenPlan(parent)
		if err != nil {
			return nil, refusal(err)
		}
		raw, err := readInputFile(args[0], testisolation.MaxBytes)
		if err != nil {
			return nil, err
		}
		p, err := testisolation.Prepare(raw, registry, policy, testisolation.Options{ParentPlan: plan.Identity(), Instance: instance, Seed: seed})
		if err != nil {
			return nil, refusal(err)
		}
		scope := p.Scope()
		document := plan.Document()
		if scope.Project != document.Test.Project || scope.Environment != document.Environment.ID || scope.Revision != document.Environment.Revision {
			return nil, refusal(errors.New("isolation environment differs from parent plan"))
		}
		return p, nil
	}
	authority := func() (testisolation.Authorities, error) {
		type grant struct {
			Path       string `json:"path"`
			Actor      string `json:"actor"`
			Generation string `json:"generation"`
		}
		var config struct {
			Schema  string `json:"schema"`
			Read    grant  `json:"read"`
			Setup   grant  `json:"setup"`
			Cleanup grant  `json:"cleanup"`
		}
		raw, err := (artifactdir.Document{MaxBytes: 64 << 10}).Read(authorityPath)
		if err != nil || json.Unmarshal(raw, &config, json.RejectUnknownMembers(true)) != nil || config.Schema != "readmit-isolation-authorities/v1" {
			return testisolation.Authorities{}, errors.New("isolation requires separately selected scoped authority configuration")
		}
		convert := func(g grant) networkaction.Authority {
			return networkaction.FileAuthority{Path: g.Path, Actor: g.Actor, Generation: g.Generation}
		}
		return testisolation.Authorities{Read: convert(config.Read), Setup: convert(config.Setup), Cleanup: convert(config.Cleanup)}, nil
	}
	review := &cobra.Command{Use: "review CONTRACT", Short: "Describe exact fixture effects without discovery or connections", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		p, err := prepare(cmd, args)
		if err != nil {
			return err
		}
		if phase != "read" && phase != "setup" && phase != "cleanup" {
			return usage("isolation phase must be read, setup or cleanup")
		}
		r := p.Review(phase)
		if prior != "" {
			r, err = testisolation.RecoveryReview(p, prior, phase)
			if err != nil {
				return refusal(err)
			}
		}
		return writeConnected(cmd, r)
	}}
	for _, name := range []string{"preflight", "setup", "reconcile", "cleanup"} {
		action := name
		command := &cobra.Command{Use: action + " CONTRACT", Short: map[string]string{"preflight": "Discover registered fixture capabilities under read authority", "setup": "Provision exact prerequisites and retain the target lease", "reconcile": "Observe interrupted effects without retrying them", "cleanup": "Clean exact versions after a separately reviewed reconciliation"}[action], Annotations: declareInterruptible(capabilityExecute), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if !execute || output == "" || authorityPath == "" {
				return usage("isolation action requires --execute, --output and --authorities")
			}
			if action != "preflight" && prior == "" {
				return usage("isolation action requires --prior naming its preflight or reconciliation evidence")
			}
			p, err := prepare(cmd, args)
			if err != nil {
				return err
			}
			a, err := authority()
			if err != nil {
				return refusal(err)
			}
			switch action {
			case "preflight":
				_, err = p.Preflight(cmd.Context(), a, output)
			case "setup":
				var s *testisolation.Session
				s, err = testisolation.Start(cmd.Context(), p, a, prior, output, testisolation.Confirmation{Plan: p.Identity(), Instance: instance, Steps: confirmed})
				if s != nil {
					defer s.Close()
				}
			case "reconcile":
				_, err = testisolation.Reconcile(cmd.Context(), p, a, prior, output)
			case "cleanup":
				_, err = testisolation.CleanupReconciled(cmd.Context(), p, a, prior, output)
			}
			if err != nil {
				return refusal(err)
			}
			return writeConnected(cmd, struct {
				Operation          string `json:"operation"`
				State              string `json:"state"`
				ApplicationVerdict string `json:"application_verdict"`
			}{action, "completed", "not-evaluated"})
		}}
		root.AddCommand(command)
	}
	show := &cobra.Command{Use: "show RESULT", Short: "Inspect retained fixture effects without restoring execution consent", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		result, err := testisolation.Inspect(args[0])
		if err != nil {
			return refusal(err)
		}
		return writeConnected(cmd, result)
	}}
	root.PersistentFlags().StringVar(&registry, "registry", "", "Operator-selected fixture adapter registry")
	root.PersistentFlags().StringVar(&policy, "policy", "", "Current scoped network policy")
	root.PersistentFlags().StringVar(&parent, "plan", "", "Verified parent connected plan")
	root.PersistentFlags().StringVar(&instance, "instance", "", "Explicit runtime identity")
	root.PersistentFlags().Uint64Var(&seed, "seed", 0, "Explicit deterministic namespace and identifier seed")
	root.PersistentFlags().StringVar(&authorityPath, "authorities", "", "Separately selected read, setup and cleanup grants")
	root.PersistentFlags().StringVar(&output, "output", "", "New private effect evidence directory")
	root.PersistentFlags().StringVar(&prior, "prior", "", "Required retained preflight, interrupted effects or reconciliation")
	root.PersistentFlags().StringVar(&phase, "phase", "read", "Review phase: read, setup or cleanup")
	root.PersistentFlags().BoolVar(&execute, "execute", false, "Explicitly perform the selected scoped operation")
	root.PersistentFlags().StringSliceVar(&confirmed, "confirm", nil, "Manual step IDs confirmed for this execution only")
	root.AddCommand(review, show)
	return root
}
