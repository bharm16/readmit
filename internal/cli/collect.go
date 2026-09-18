package cli

import (
	"context"
	"crypto/tls"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/transportsecurity"
	"github.com/spf13/cobra"
)

// maxCertificateBytes bounds each PEM file the listener is configured with,
// matching the bound a target configuration applies to its own CA file.
const maxCertificateBytes = 1 << 20

// listenerTLS is the declared transport of one capture listener. Every member
// is a path or a name the operator supplied; no key ever appears here, because
// the private key is read from the store its credential reference names and is
// held only for as long as the configuration is built.
type listenerTLS struct {
	certificate  string
	keyReference string
	secretsFile  string
	clientCA     string
}

func (t listenerTLS) declared() bool {
	return t.certificate != "" || t.keyReference != "" || t.secretsFile != "" || t.clientCA != ""
}

func collectCommand(ran *bool) *cobra.Command {
	var address, policyPath string
	var approvedBind bool
	var transport listenerTLS
	var config receiver.CollectorConfig
	command := &cobra.Command{
		Use:   "collect --policy FILE --output NEW_DIRECTORY",
		Short: "Collect downstream HL7 over MLLP under a declared acknowledgement policy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if err := sendpolicy.BindAddress(address, approvedBind); err != nil {
				return err
			}
			if policyPath == "" {
				return errors.New("collect requires --policy with a receiver policy file")
			}
			data, err := readInputFile(policyPath, collection.MaxPolicyBytes)
			if err != nil {
				return err
			}
			config.Policy, err = collection.DecodePolicy(data)
			if err != nil {
				return err
			}
			if config.Policy.Faults != nil {
				if err := config.Policy.Faults.ApproveEndpoint(address); err != nil {
					return err
				}
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			secured, err := listenerConfig(ctx, transport, address)
			if err != nil {
				return err
			}
			config.TLS = secured != nil
			config.ClientCertificate = transport.clientCA != ""
			listener, err := net.Listen("tcp", address)
			if err != nil {
				return errors.New("cannot bind collector address")
			}
			defer listener.Close()
			if secured != nil {
				listener = tls.NewListener(listener, secured)
			}
			c, err := receiver.NewCollector(config)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Listening: %s\nPolicy: %s\nSource label: %s\nAcknowledgement: %s %s\nEnhanced acknowledgement: %s\nApplication processing: %s\nTransport: %s\nClient certificate: %s\nConcurrent connections: %d\nCapture journal: %s\n",
				listener.Addr(), config.Policy.Name, config.Policy.SourceLabel, config.Policy.Acknowledgement.Operator, config.Policy.Acknowledgement.Code,
				enhancedSummary(config.Policy), collection.NoApplicationProcessing, transportName(config.TLS), clientCertificateName(config.ClientCertificate),
				max(1, config.MaxConnections), journalName(config.JournalPath)); err != nil {
				return errors.New("cannot write collector startup output")
			}
			b, err := c.Serve(ctx, listener)
			if b != nil {
				if outErr := renderBundle(cmd.OutOrStdout(), b, false, false); outErr != nil {
					return errors.Join(err, outErr)
				}
			}
			// The capture states how it stopped; it does not take its exit
			// status from the journal. A capture that was cancelled or that
			// reached what the operator declared has finished doing what it was
			// asked to do, and whether the journal finalized is the question
			// `collect status` answers.
			if summary := c.Journal(); summary != nil {
				if outErr := writeCapture(cmd, *summary, false); outErr != nil {
					return errors.Join(err, outErr)
				}
			}
			return err
		},
	}
	command.Flags().StringVar(&address, "address", "127.0.0.1:2575", "TCP listen address; port 0 chooses an available port")
	command.Flags().BoolVar(&approvedBind, "approved-bind", false, "Explicitly approve binding a nonloopback address, accepting connections from beyond this machine")
	command.Flags().StringVar(&policyPath, "policy", "", "Existing readmit-receiver-policy/v1, /v2, or /v3 JSON file")
	command.Flags().StringVar(&config.OutputPath, "output", "", "New final case bundle directory")
	command.Flags().StringVar(&config.JournalPath, "journal", "", "New capture journal directory; without one an interrupted capture cannot be recovered")
	command.Flags().IntVar(&config.MaxFrameBytes, "max-frame-bytes", 1<<20, "Maximum MLLP payload bytes, excluding the three framing bytes")
	command.Flags().DurationVar(&config.IdleTimeout, "idle-timeout", 30*time.Second, "Maximum interval without receiving bytes, and acknowledgement write timeout")
	command.Flags().DurationVar(&config.ApplicationTimeout, "application-ack-timeout", 10*time.Second, "Connect and write timeout for one acknowledgement sent to a separate application endpoint")
	command.Flags().IntVar(&config.MaxMessages, "max-messages", 0, "Finalize after this many complete inbound frames; 0 waits for cancellation")
	command.Flags().IntVar(&config.MaxConnections, "max-connections", 1, "Peers served at once; an additional peer waits for a free slot")
	command.Flags().IntVar(&config.MaxSessions, "max-sessions", 0, "Stop after serving this many connections in total; 0 uses the case source limit")
	command.Flags().IntVar(&config.MaxCaptureBytes, "max-capture-bytes", 0, "Stop before retaining more than this many bytes; 0 uses the case evidence limit")
	command.Flags().StringVar(&transport.certificate, "tls-certificate", "", "Existing PEM certificate chain this listener presents")
	command.Flags().StringVar(&transport.keyReference, "tls-key-reference", "", "Registered credential reference naming the certificate's private key")
	command.Flags().StringVar(&transport.secretsFile, "secrets", "", "Existing readmit-secrets/v1 store holding that credential reference")
	command.Flags().StringVar(&transport.clientCA, "client-ca", "", "Existing PEM authority whose client certificates this listener requires and verifies")
	command.AddCommand(collectStatusCommand(ran))
	return command
}

func collectStatusCommand(ran *bool) *cobra.Command {
	var asJSON bool
	status := &cobra.Command{
		Use:   "status JOURNAL",
		Short: "Recover an interrupted capture read-only; never resend or resume",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("collect status requires one capture journal directory")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			summary, err := capturejournal.Open(args[0])
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			if err := writeCapture(cmd, summary, asJSON); err != nil {
				return err
			}
			if summary.ExitCode() != 0 {
				return &ExitError{Code: summary.ExitCode(), Err: errors.New("capture did not finalize; inspect the retained journal"), Reported: true}
			}
			return nil
		},
	}
	status.Flags().BoolVar(&asJSON, "json", false, "Write one versioned machine-readable capture summary")
	return status
}

func writeCapture(cmd *cobra.Command, summary capturejournal.Summary, asJSON bool) error {
	var err error
	if asJSON {
		var data []byte
		data, err = json.Marshal(summary, json.Deterministic(true))
		if err == nil {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		}
	} else {
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Capture state: %s\nStop reason: %s\nDelivery uncertain: %t\nReceived frames: %d\nAcknowledgements sent: %d\nAcknowledgements unsent: %d\nAcknowledgements uncertain: %d\n",
			summary.State, summary.StopReason, summary.DeliveryUncertain, summary.Received, summary.Acknowledged, summary.Unsent, summary.Uncertain)
		if err == nil && summary.Recovered {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Finalization was not recorded; the writer may still be active. Recovery never sends, resends or resumes.")
		}
	}
	if err != nil {
		return &ExitError{Code: 2, Err: errors.New("cannot write capture summary")}
	}
	return nil
}

// listenerConfig builds the capture listener's TLS configuration, or nil when
// the operator declared none. The certificate chain and the client authority
// are configuration files; the private key is a credential, read from the store
// its reference names for this one process and never written anywhere.
func listenerConfig(ctx context.Context, declared listenerTLS, address string) (*tls.Config, error) {
	if !declared.declared() {
		return nil, nil
	}
	if declared.certificate == "" || declared.keyReference == "" || declared.secretsFile == "" {
		return nil, errors.New("a TLS listener requires --tls-certificate, --tls-key-reference and --secrets together")
	}
	chain, err := readInputFile(declared.certificate, maxCertificateBytes)
	if err != nil {
		return nil, errors.New("cannot read the configured listener certificate")
	}
	var authorities []byte
	if declared.clientCA != "" {
		if authorities, err = readInputFile(declared.clientCA, maxCertificateBytes); err != nil {
			return nil, errors.New("cannot read the configured client certificate authority")
		}
	}
	store, err := secret.ReadStore(declared.secretsFile)
	if err != nil {
		return nil, err
	}
	// The reference is bound to this listener's own endpoint before it is read,
	// so a credential registered for another address is refused rather than
	// presented here.
	reference, err := secret.Bind(store, declared.keyReference, secret.MLLPEndpoint, address)
	if err != nil {
		return nil, err
	}
	value, err := secret.Resolve(ctx, reference)
	if err != nil {
		return nil, errors.New("the private key the listener certificate's reference names did not resolve from its declared store")
	}
	return transportsecurity.ServerConfig(chain, value.Expose(), authorities)
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
