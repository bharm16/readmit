package cli

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/spf13/cobra"
	"path/filepath"
	"time"
)

func suiteGateCommand(verify bool) *cobra.Command {
	var baseline, policy, pin, output string
	name := "gate"
	if verify {
		name = "verify-gate"
	}
	command := &cobra.Command{Use: name + " DIRECTORY", Short: "Assess retained suite evidence against an explicitly pinned CI gate policy without sending", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var r suite.GateReport
		selectedPolicy := policy
		if verify {
			selectedPolicy = filepath.Join(args[0], "policy.json")
		}
		raw, readErr := (artifactdir.Document{MaxBytes: suite.MaxBytes}).Read(selectedPolicy)
		var header struct {
			Schema string `json:"schema"`
		}
		connected := readErr == nil && json.Unmarshal(raw, &header) == nil && header.Schema == suite.ConnectedGatePolicySchema
		if verify {
			if connected {
				r = suite.VerifyConnectedGate(cmd.Context(), args[0], pin, time.Now().UTC())
			} else {
				r = suite.VerifyGate(cmd.Context(), args[0], pin, time.Now().UTC())
			}
		} else {
			if connected {
				r = suite.RetainConnectedGate(cmd.Context(), args[0], baseline, policy, pin, output, time.Now().UTC())
			} else {
				r = suite.RetainGate(cmd.Context(), args[0], baseline, policy, pin, output, time.Now().UTC())
			}
		}
		if e := writeJSON(cmd, r); e != nil {
			return refusal(errors.New("cannot write CI gate summary"))
		}
		if r.ExitCode != 0 {
			return verdict(r.ExitCode, errors.New("CI gate did not pass; inspect retained evidence privately"))
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

func suiteGatePolicyCommand() *cobra.Command {
	return &cobra.Command{Use: "gate-policy FILE", Short: "Print the canonical identity of a privately reviewed CI gate policy", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		raw, e := baseline.ReadBytes(args[0], suite.MaxBytes)
		if e != nil {
			return refusal(errors.New("cannot read CI gate policy"))
		}
		p, e := suite.DecodeGatePolicy(raw)
		if e != nil {
			if modern, err := suite.DecodeConnectedGatePolicy(raw); err == nil {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), modern.Identity())
				return err
			}
			return refusal(e)
		}
		_, e = fmt.Fprintln(cmd.OutOrStdout(), p.Identity())
		return e
	}}
}
