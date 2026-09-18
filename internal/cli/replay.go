package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/spf13/cobra"
)

func replayCommand(ran *bool) *cobra.Command {
	var targetPath, output, shift, policyPath, decisionPath string
	var messages, transforms []string
	var send bool
	command := &cobra.Command{
		Use:   "replay CASE --target CONFIG [--policy FILE --decision NEW_FILE] [--send --output NEW_RUN]",
		Short: "Preview or explicitly send selected case messages and retain local run evidence",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("replay requires exactly one case bundle and an explicitly configured target")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if targetPath == "" {
				return errors.New("replay requires --target with a test endpoint configuration")
			}
			if send && output == "" {
				return errors.New("replay --send requires --output with a new run directory")
			}
			// A decision a policy reached is retained evidence of what was
			// allowed or denied and why, so selecting a policy and retaining
			// its decision are one act rather than two.
			if (policyPath == "") != (decisionPath == "") {
				return errors.New("replay --policy and --decision are used together: a selected policy records the decision it reached in a new file")
			}
			policy, err := readSendPolicy(policyPath)
			if err != nil {
				return err
			}
			options := replay.Options{Occurrences: messages}
			hasShift := false
			for _, name := range transforms {
				transformation := replay.Transformation{Name: name}
				if name == "shift-timestamps" {
					hasShift = true
					transformation.Shift = shift
				}
				options.Transformations = append(options.Transformations, transformation)
			}
			if cmd.Flags().Changed("shift") && !hasShift {
				return errors.New("--shift requires --transform shift-timestamps")
			}
			target, err := replay.ReadTarget(targetPath)
			if err != nil {
				return err
			}
			plan, err := replay.Prepare(args[0], target, options)
			if err != nil {
				return err
			}
			// Interrupt and termination reach the decision as well as the send,
			// so a name lookup is cancellable rather than a wait nothing stops.
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			// One rule, asked here exactly as the send below asks it. A preview
			// does not request a send, so the reason it reports is the first
			// destination rule that refuses, or send_not_explicit when nothing
			// about the destination does.
			decision := sendpolicy.Decide(ctx, policy, sendpolicy.Request{
				Address: target.Address, Classification: string(target.Environment().Classification), Explicit: send,
			}, sendpolicy.SystemResolver)
			if decisionPath != "" {
				if err := writeSendDecision(decisionPath, decision); err != nil {
					return err
				}
			}
			writer := bufio.NewWriter(cmd.OutOrStdout())
			if !send {
				fmt.Fprintf(writer, "Dry run: no connection opened\nTarget: %q (%s)\n", target.Address, target.Transport)
				writeEnvironmentBanner(writer, target.Environment())
				writeDecisionLines(writer, decision)
				fmt.Fprintf(writer, "Messages: %d\n", plan.Count())
				if len(transforms) == 0 {
					fmt.Fprintln(writer, "Transformations: none; message payload bytes unchanged")
				}
				for _, transformation := range options.Transformations {
					fmt.Fprintf(writer, "Transformation: %s", transformation.Name)
					if transformation.Shift != "" {
						fmt.Fprintf(writer, " shift=%s", transformation.Shift)
					}
					fmt.Fprintln(writer)
				}
				for _, mapping := range plan.Mappings() {
					raw, _ := plan.Outbound(mapping.OutboundOccurrence)
					fmt.Fprintf(writer, "  %s source=%s wire_bytes=%d\n", mapping.OutboundOccurrence, mapping.SourceOccurrence, len(raw))
				}
				if err := writer.Flush(); err != nil {
					return errors.New("cannot write replay preview")
				}
				return nil
			}
			// The decision is reached and retained before anything is opened.
			// It can stop a send; it cannot retract bytes already sent, and the
			// refusal below is stated before the first of them leaves.
			if !decision.Allowed {
				writeDecisionLines(writer, decision)
				if err := writer.Flush(); err != nil {
					return errors.New("cannot write the policy decision")
				}
				return errors.New("the send was refused by policy before anything was sent; the reported decision names why")
			}
			result, err := replay.Execute(ctx, plan, output)
			if err != nil {
				return err
			}
			fmt.Fprintf(writer, "Run: %s\nSchema: %s\n", result.Identity, result.Manifest.Schema)
			writeEnvironmentBanner(writer, target.Environment())
			writeDecisionLines(writer, decision)
			fmt.Fprintf(writer, "Messages: %d\nContains source values: true (customer-local-only)\n", len(result.Events))
			for _, event := range result.Events {
				fmt.Fprintf(writer, "  %s outcome=%s delivery=%s sent_bytes=%d received_bytes=%d ack=%s correlation=%s elapsed=%s", event.OutboundOccurrence, event.Outcome, event.Delivery, event.Sent.Size, event.Received.Size, event.ACK.Code, event.ACK.Correlation, time.Duration(event.ElapsedNS))
				if event.TransportError != nil {
					fmt.Fprintf(writer, " error_class=%s phase=%s", event.TransportError.Class, event.TransportError.Phase)
				}
				fmt.Fprintln(writer)
			}
			if err := writer.Flush(); err != nil {
				return errors.New("cannot write replay summary")
			}
			if !result.Successful() {
				return errors.New("replay completed with nonaccepted or uncertain outcomes; inspect the customer-local run bundle")
			}
			return nil
		},
	}
	command.Flags().StringVar(&targetPath, "target", "", "Explicit readmit-target/v1, /v2 or /v3 test endpoint configuration")
	command.Flags().StringVar(&output, "output", "", "New customer-local run directory; only created with --send")
	command.Flags().BoolVar(&send, "send", false, "Explicitly connect and send; default is a local-only dry run")
	command.Flags().StringArrayVar(&messages, "message", nil, "Source message occurrence to include (repeatable; source order is preserved)")
	command.Flags().StringArrayVar(&transforms, "transform", nil, "Named transformation: rebase-control-ids or shift-timestamps (repeatable)")
	command.Flags().StringVar(&shift, "shift", "", "Explicit whole-second duration for shift-timestamps, e.g. 24h or -2h")
	command.Flags().StringVar(&policyPath, "policy", "", "Existing "+sendpolicy.PolicySchema+" document naming the destinations approved for sending")
	command.Flags().StringVar(&decisionPath, "decision", "", "New file retaining the "+sendpolicy.DecisionSchema+" decision the selected policy reached")
	return command
}
