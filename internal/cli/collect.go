package cli

import (
	"errors"
	"fmt"

	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/spf13/cobra"
)

func collectCommand() *cobra.Command {
	var config operation.CollectConfig
	command := &cobra.Command{
		Use:         "collect --policy FILE --output NEW_DIRECTORY",
		Annotations: declareInterruptible(capabilityExecute),
		Short:       "Collect downstream HL7 over MLLP under a declared acknowledgement policy",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			config.Listening = func(bound string, policy collection.Policy) error {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Listening: %s\nPolicy: %s\nSource label: %s\nAcknowledgement: %s %s\nEnhanced acknowledgement: %s\nApplication processing: %s\nTransport: %s\nClient certificate: %s\nConcurrent connections: %d\nCapture journal: %s\n",
					bound, policy.Name, policy.SourceLabel, policy.Acknowledgement.Operator, policy.Acknowledgement.Code,
					enhancedSummary(policy), collection.NoApplicationProcessing, transportName(config.TLSCertificatePath != ""), clientCertificateName(config.ClientCAPath != ""),
					max(1, config.MaxConnections), journalName(config.JournalPath)); err != nil {
					return errors.New("cannot write collector startup output")
				}
				return nil
			}
			result, err := operation.StartCollect(cmd.Context(), config)
			switch {
			case errors.Is(err, operation.ErrReceiverPolicyRequired):
				return usage("collect requires --policy with a receiver policy file")
			case errors.Is(err, operation.ErrListenerTLSIncomplete):
				return usage("a TLS listener requires --tls-certificate, --tls-key-reference and --secrets together")
			}
			if result.Bundle != nil {
				if outErr := renderBundle(cmd.OutOrStdout(), result.Bundle, false, false); outErr != nil {
					return errors.Join(err, outErr)
				}
			}
			// The capture states how it stopped; it does not take its exit
			// status from the journal. A capture that was cancelled or that
			// reached what the operator declared has finished doing what it was
			// asked to do, and whether the journal finalized is the question
			// `collect status` answers.
			if result.Journal != nil {
				if outErr := writeCapture(cmd, *result.Journal, false); outErr != nil {
					return errors.Join(err, outErr)
				}
			}
			return err
		},
	}
	command.Flags().StringVar(&config.Address, "address", "127.0.0.1:2575", "TCP listen address; port 0 chooses an available port")
	command.Flags().BoolVar(&config.ApprovedBind, "approved-bind", false, "Explicitly approve binding a nonloopback address, accepting connections from beyond this machine")
	command.Flags().StringVar(&config.PolicyPath, "policy", "", "Existing readmit-receiver-policy/v1, /v2, or /v3 JSON file")
	command.Flags().StringVar(&config.OutputPath, "output", "", "New final case bundle directory")
	command.Flags().StringVar(&config.JournalPath, "journal", "", "New capture journal directory; without one an interrupted capture cannot be recovered")
	command.Flags().IntVar(&config.MaxFrameBytes, "max-frame-bytes", operation.DefaultMaxFrameBytes, "Maximum MLLP payload bytes, excluding the three framing bytes")
	command.Flags().DurationVar(&config.IdleTimeout, "idle-timeout", operation.DefaultIdleTimeout, "Maximum interval without receiving bytes, and acknowledgement write timeout")
	command.Flags().DurationVar(&config.ApplicationTimeout, "application-ack-timeout", operation.DefaultApplicationTimeout, "Connect and write timeout for one acknowledgement sent to a separate application endpoint")
	command.Flags().IntVar(&config.MaxMessages, "max-messages", 0, "Finalize after this many complete inbound frames; 0 waits for cancellation")
	command.Flags().IntVar(&config.MaxConnections, "max-connections", 1, "Peers served at once; an additional peer waits for a free slot")
	command.Flags().IntVar(&config.MaxSessions, "max-sessions", 0, "Stop after serving this many connections in total; 0 uses the case source limit")
	command.Flags().IntVar(&config.MaxCaptureBytes, "max-capture-bytes", 0, "Stop before retaining more than this many bytes; 0 uses the case evidence limit")
	command.Flags().StringVar(&config.TLSCertificatePath, "tls-certificate", "", "Existing PEM certificate chain this listener presents")
	command.Flags().StringVar(&config.TLSKeyReference, "tls-key-reference", "", "Registered credential reference naming the certificate's private key")
	command.Flags().StringVar(&config.SecretsFile, "secrets", "", "Existing readmit-secrets/v1 store holding that credential reference")
	command.Flags().StringVar(&config.ClientCAPath, "client-ca", "", "Existing PEM authority whose client certificates this listener requires and verifies")
	command.AddCommand(collectStatusCommand())
	return command
}

func collectStatusCommand() *cobra.Command {
	var asJSON bool
	status := &cobra.Command{
		Use:         "status JOURNAL",
		Annotations: declare(capabilityFree),
		Short:       "Recover an interrupted capture read-only; never resend or resume",
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := capturejournal.Open(args[0])
			if err != nil {
				return refusal(err)
			}
			if err := writeCapture(cmd, summary, asJSON); err != nil {
				return err
			}
			if summary.ExitCode() != 0 {
				return verdict(summary.ExitCode(), errors.New("capture did not finalize; inspect the retained journal"))
			}
			return nil
		},
	}
	status.Flags().BoolVar(&asJSON, "json", false, "Write one versioned machine-readable capture summary")
	return status
}

func writeCapture(cmd *cobra.Command, summary capturejournal.Summary, asJSON bool) error {
	return writeReport(cmd, asJSON, summary, func(c *cobra.Command) error { return writeCaptureSummary(c, summary) }, "cannot write capture summary")
}

func writeCaptureSummary(cmd *cobra.Command, summary capturejournal.Summary) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Capture state: %s\nStop reason: %s\nDelivery uncertain: %t\nReceived frames: %d\nAcknowledgements sent: %d\nAcknowledgements unsent: %d\nAcknowledgements uncertain: %d\n",
		summary.State, summary.StopReason, summary.DeliveryUncertain, summary.Received, summary.Acknowledged, summary.Unsent, summary.Uncertain)
	if err == nil && summary.Recovered {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "Finalization was not recorded; the writer may still be active. Recovery never sends, resends or resumes.")
	}
	return err
}

func transportName(secured bool) string {
	if secured {
		return "tls"
	}
	return "plain"
}

func clientCertificateName(required bool) string {
	if required {
		return "required"
	}
	return "none"
}

// journalName states whether this capture can be recovered after a kill. It
// never prints the journal's path: a diagnostic or a startup line that echoed
// a filename would disclose where evidence is kept.
func journalName(path string) string {
	if path == "" {
		return "unavailable"
	}
	return "enabled"
}
