package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bharm16/readmit/internal/report"
	"github.com/spf13/cobra"
)

func reportCommand(ran *bool) *cobra.Command {
	var scenario, output string
	command := &cobra.Command{
		Use: "report --scenario siu-reschedule-v1 --output NEW_PACKET", Short: "Generate a sealed synthetic packet using fresh built-in loopback fixtures",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			*ran = true
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			packet, err := report.Create(ctx, scenario, output)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Synthetic-only packet complete\nExecution: fresh built-in defective and fixed fixtures on loopback only\nPacket: %s\nObservation boundary: appointment-ledger\nInput unchanged; receiver behavior changed\nBaseline: assertion_failure; post-fix: pass\nRerun: follow the packet's RERUN.md; prepare a new workspace outside the sealed packet\n", packet.Identity); err != nil {
				return errors.New("cannot write report summary")
			}
			return nil
		},
	}
	command.Flags().StringVar(&scenario, "scenario", "", "Committed synthetic scenario; only siu-reschedule-v1 is supported")
	command.Flags().StringVar(&output, "output", "", "New sealed packet directory; never overwrite")
	verify := &cobra.Command{Use: "verify PACKET", Short: "Verify a complete synthetic packet offline", Args: reportOneArgument, RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		packet, err := report.Open(args[0])
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Packet verified: %s\nSynthetic-only; input unchanged; appointment-ledger boundary\n", packet.Identity); err != nil {
			return errors.New("cannot write packet verification")
		}
		return nil
	}}
	var workspace, address string
	prepare := &cobra.Command{Use: "prepare PACKET --output NEW_WORKSPACE", Short: "Prepare runnable copies outside a verified sealed packet", Args: reportOneArgument, RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		prepared, err := report.Prepare(args[0], workspace, address)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Rerun workspace prepared; no connection opened\nPacket: %s\nInput identity and assertion semantics preserved\nTrials: baseline (defective), post-fix (fixed), reintroduced (defective)\nFollow the workspace's RERUN.md; wait for Listening before each test\n", prepared.PacketIdentity); err != nil {
			return errors.New("cannot write report preparation")
		}
		return nil
	}}
	prepare.Flags().StringVar(&workspace, "output", "", "New mutable workspace outside the sealed packet")
	prepare.Flags().StringVar(&address, "address", "127.0.0.1:2575", "Explicit numeric loopback endpoint for manual fixture reruns")
	command.AddCommand(verify, prepare)
	return command
}

func reportOneArgument(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("report subcommand requires exactly one packet")
	}
	return nil
}
