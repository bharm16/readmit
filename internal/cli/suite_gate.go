package cli

import (
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/spf13/cobra"
	"time"
)

func suiteGateCommand(ran *bool, verify bool) *cobra.Command {
	var baseline, policy, pin, output string
	name := "gate"
	if verify {
		name = "verify-gate"
	}
	command := &cobra.Command{Use: name + " DIRECTORY", Short: "Assess retained suite evidence against an explicitly pinned CI gate policy without sending", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		var r suite.GateReport
		if verify {
			r = suite.VerifyGate(cmd.Context(), args[0], pin, time.Now().UTC())
		} else {
			r = suite.RetainGate(cmd.Context(), args[0], baseline, policy, pin, output, time.Now().UTC())
		}
		if e := writeJSON(cmd, r); e != nil {
			return &ExitError{Code: 2, Err: errors.New("cannot write CI gate summary")}
		}
		if r.ExitCode != 0 {
			return &ExitError{Code: r.ExitCode, Err: errors.New("CI gate did not pass; inspect retained evidence privately"), Reported: true}
		}
		return nil
	}}
	command.Flags().StringVar(&pin, "policy-identity", "", "Externally approved full canonical gate policy identity")
	if !verify {
		command.Flags().StringVar(&baseline, "baseline", "", "Explicit reviewed baseline suite directory")
		command.Flags().StringVar(&policy, "policy", "", "Reviewed strict CI gate policy")
		command.Flags().StringVar(&output, "output", "", "New customer-private retained snapshot directory")
	}
	return command
}

func suiteGatePolicyCommand(ran *bool) *cobra.Command {
	return &cobra.Command{Use: "gate-policy FILE", Short: "Print the canonical identity of a privately reviewed CI gate policy", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		raw, e := baseline.ReadBytes(args[0], suite.MaxBytes)
		if e != nil {
			return &ExitError{Code: 2, Err: errors.New("cannot read CI gate policy")}
		}
		p, e := suite.DecodeGatePolicy(raw)
		if e != nil {
			return &ExitError{Code: 2, Err: e}
		}
		_, e = fmt.Fprintln(cmd.OutOrStdout(), p.Identity())
		return e
	}}
}
