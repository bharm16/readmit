package cli

import (
	"errors"

	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/spf13/cobra"
)

func connectedTransportCommand() *cobra.Command {
	root := &cobra.Command{Use: "transport", Short: "Review and execute explicitly authorized connected v2 transports"}
	var credential string
	prepare := func(args []string) (*connectedtransport.Prepared, error) {
		plan, err := connectedtest.OpenPlan(args[0])
		if err != nil {
			return nil, err
		}
		return connectedtransport.Prepare(plan, connectedtransport.Selection{Case: args[1], Target: args[2], Policy: args[3], Credential: credential})
	}
	review := &cobra.Command{Use: "review PLAN CASE TARGET POLICY", Short: "Bind exact local inputs without DNS, secrets or sockets", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(4), RunE: func(c *cobra.Command, a []string) error {
		p, err := prepare(a)
		if err != nil {
			return refusal(err)
		}
		return writeConnected(c, p.Binding())
	}}
	var send bool
	var instance, output, grant, actor, generation string
	run := &cobra.Command{Use: "run PLAN CASE TARGET POLICY", Short: "Send the bound v2 steps once using a current configured runner grant", Annotations: declareInterruptible(capabilityExecuteIfSend), Args: cobra.ExactArgs(4), RunE: func(c *cobra.Command, a []string) error {
		if !send || instance == "" || output == "" || grant == "" || actor == "" || generation == "" {
			return usage("transport run requires --send, --instance, --output, --grant, --actor and --generation")
		}
		p, err := prepare(a)
		if err != nil {
			return refusal(err)
		}
		r, err := connectedtransport.Execute(c.Context(), p, connectedtransport.FileAuthority{Path: grant, Actor: actor, Generation: generation}, instance, output, nil)
		if err != nil {
			return refusal(err)
		}
		// Operational output contains no endpoint address, policy ranges, certificate
		// paths, provider arguments or payloads. Full evidence stays customer-local.
		if err := writeConnected(c, struct {
			State              string `json:"state"`
			ApplicationVerdict string `json:"application_verdict"`
		}{r.State, r.ApplicationVerdict}); err != nil {
			return err
		}
		if r.State != "settled" {
			return verdict(1, errors.New("connected delivery did not settle; no automatic resend"))
		}
		return nil
	}}
	root.PersistentFlags().StringVar(&credential, "credential", "", "Purpose-bound private-key reference document for mTLS")
	run.Flags().BoolVar(&send, "send", false, "Explicitly execute the prepared transport")
	run.Flags().StringVar(&instance, "instance", "", "New execution instance identifier")
	run.Flags().StringVar(&output, "output", "", "New private transport evidence directory")
	run.Flags().StringVar(&grant, "grant", "", "Owner-only configured runner grant")
	run.Flags().StringVar(&actor, "actor", "", "Explicit runner actor")
	run.Flags().StringVar(&generation, "generation", "", "Current runner grant generation")
	show := &cobra.Command{Use: "show RESULT", Short: "Verify retained transport evidence offline", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
		r, err := connectedtransport.Open(a[0])
		if err != nil {
			return refusal(err)
		}
		return writeConnected(c, r)
	}}
	root.AddCommand(review, run, show)
	return root
}
