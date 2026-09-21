package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/spf13/cobra"
)

func replayCommand() *cobra.Command {
	var targetPath, output, shift, policyPath, decisionPath string
	var messages, transforms []string
	var send bool
	command := &cobra.Command{
		Use:         "replay CASE --target config [--policy file --decision new_file] [--send --output new_run]",
		Annotations: declareInterruptible(capabilityExecuteIfSend),
		Short:       "Preview or explicitly send selected case messages and retain local run evidence",
		RunE: func(cmd *cobra.Command, args []string) error {
			if targetPath == "" {
				return usage("replay requires --target with a test endpoint configuration")
			}
			if send && output == "" {
				return usage("replay --send requires --output with a new run directory")
			}
			// Actual sends always retain their decision, including denials when
			// no policy was selected. Previews need an explicit destination.
			if send && decisionPath == "" {
				decisionPath = output + ".decision.json"
			}
			if policyPath != "" && decisionPath == "" {
				return usage("a policy preview requires --decision with a new file")
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
				return usage("--shift requires --transform shift-timestamps")
			}
			target, err := replay.ReadTarget(targetPath)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			var decision sendpolicy.Decision
			record := func(value sendpolicy.Decision) error {
				decision = value
				if decisionPath != "" {
					return sendpolicy.WriteDecision(decisionPath, value)
				}
				return nil
			}
			// Prepare also rejects production for every caller. Record that
			// refusal before preparation so it survives as inspectable evidence.
			if !send || sendpolicy.RefusesEverySend(string(target.Environment().Classification)) {
				duration, _ := time.ParseDuration(target.ConnectTimeout)
				decisionCtx, stop := context.WithTimeout(ctx, duration)
				value := sendpolicy.Decide(decisionCtx, policy, sendpolicy.Request{
					Address: target.Address, Classification: string(target.Environment().Classification), Explicit: send,
				}, sendpolicy.SystemResolver)
				stop()
				if err := record(value); err != nil {
					return err
				}
			}
			plan, err := replay.Prepare(args[0], target, options)
			if err != nil {
				return err
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
			result, err := replay.ExecuteWithPolicy(ctx, plan, output, policy, record)
			if err != nil {
				if decision.Schema != "" && !decision.Allowed {
					writeDecisionLines(writer, decision)
					_ = writer.Flush()
				}
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
	command.Flags().StringVar(&decisionPath, "decision", "", "New file retaining the "+sendpolicy.DecisionSchema+" decision (send default: OUTPUT.decision.json)")
	return command
}
