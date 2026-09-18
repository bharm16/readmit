package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/environment"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/spf13/cobra"
)

func targetCommand(ran *bool) *cobra.Command {
	command := &cobra.Command{
		Use:   "target",
		Short: "Configure, validate and diagnose one named test environment",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("target requires a subcommand: set, show, check, or reset")
		},
	}
	command.AddCommand(targetSet(ran), targetShow(ran), targetCheck(ran), targetReset(ran))
	return command
}

func targetSet(ran *bool) *cobra.Command {
	var file, name, classification, address, transport string
	var caFile, serverName, clientCertificate, secretsFile, reference string
	var connectTimeout, messageTimeout string
	var maxACKBytes int
	var approved bool
	command := &cobra.Command{
		Use:   "set --target FILE --name NAME --classification CLASS --address HOST:PORT",
		Short: "Record or edit the endpoint, timeouts, TLS material and classification of one environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			*ran = true
			config, err := openOrNewTarget(file)
			if err != nil {
				return err
			}
			changed := cmd.Flags().Changed
			apply := func(name string, member *string, value string) {
				if changed(name) {
					*member = value
				}
			}
			apply("name", &config.Name, name)
			apply("classification", (*string)(&config.Classification), classification)
			apply("address", &config.Address, address)
			apply("transport", &config.Transport, transport)
			apply("ca", &config.CAFile, caFile)
			apply("server-name", &config.ServerName, serverName)
			apply("client-certificate", &config.ClientCertificate, clientCertificate)
			apply("secrets", &config.Credential.SecretsFile, secretsFile)
			apply("credential", &config.Credential.Reference, reference)
			apply("connect-timeout", &config.ConnectTimeout, connectTimeout)
			apply("message-timeout", &config.MessageTimeout, messageTimeout)
			if changed("max-ack-bytes") {
				config.MaxACKBytes = maxACKBytes
			}
			if changed("approved-transport") {
				config.ApprovedTransport = approved
			}
			if err := replay.WriteTarget(file, config); err != nil {
				return err
			}
			// The configuration is read back through the same reader every
			// other command uses, so what is reported is what readmit reads
			// rather than what this command intended to write.
			stored, err := replay.ReadTarget(file)
			if err != nil {
				return err
			}
			return writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				fmt.Fprintf(w, "Environment recorded: %s\n", stored.Name)
				writeEnvironmentBanner(w, stored.Environment())
				writeConfigurationLines(w, stored)
			})
		},
	}
	defaults := newEnvironment()
	command.Flags().StringVar(&file, "target", "", "Target configuration file to record this environment in")
	command.Flags().StringVar(&name, "name", "", "Name for this environment")
	command.Flags().StringVar(&classification, "classification", "", "Recorded class of this environment: nonproduction, production or unclassified")
	command.Flags().StringVar(&address, "address", "", "Explicit host and numeric port of the endpoint")
	command.Flags().StringVar(&transport, "transport", defaults.Transport, "Transport: plain or verified tls")
	command.Flags().BoolVar(&approved, "approved-transport", false, "Explicitly approve this configured transport; required for hostnames and nonloopback addresses")
	command.Flags().StringVar(&caFile, "ca", "", `Explicit PEM CA file replacing the system roots; pass "" to use the system roots`)
	command.Flags().StringVar(&serverName, "server-name", "", `TLS server name to verify the certificate against; pass "" to use the address host`)
	command.Flags().StringVar(&clientCertificate, "client-certificate", "", `PEM certificate chain this environment presents; pass "" to present none`)
	secretsFlag(command, &secretsFile)
	command.Flags().StringVar(&reference, "credential", "", "Registered reference naming the private key for the client certificate")
	command.Flags().StringVar(&connectTimeout, "connect-timeout", defaults.ConnectTimeout, "Positive Go duration bounding dialling and TLS setup together")
	command.Flags().StringVar(&messageTimeout, "message-timeout", defaults.MessageTimeout, "Positive Go duration bounding one message and its acknowledgement")
	command.Flags().IntVar(&maxACKBytes, "max-ack-bytes", defaults.MaxACKBytes, "Largest acknowledgement this environment may return")
	return command
}

func targetShow(ran *bool) *cobra.Command {
	var file string
	command := &cobra.Command{
		Use:   "show --target FILE",
		Short: "Validate one environment configuration and show its endpoint, timeouts, TLS material and classification",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			*ran = true
			config, err := readTargetFile(file)
			if err != nil {
				return err
			}
			return writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				fmt.Fprintln(w, "Configuration: valid; no connection was opened")
				writeEnvironmentBanner(w, config.Environment())
				writeConfigurationLines(w, config)
			})
		},
	}
	command.Flags().StringVar(&file, "target", "", "Target configuration file describing the environment")
	return command
}

func targetCheck(ran *bool) *cobra.Command {
	var file, policyPath, decisionPath string
	command := &cobra.Command{
		Use:   "check --target FILE [--policy FILE]",
		Short: "Reach one configured environment and report the transport, TLS status and send decision, sending no HL7 payload",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			*ran = true
			config, err := readTargetFile(file)
			if err != nil {
				return err
			}
			policy, err := readSendPolicy(policyPath)
			if err != nil {
				return err
			}
			// Interrupt and termination reach the diagnosis, so a cancelled
			// check reports cancellation rather than claiming nothing.
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			// The same rule the send path enforces, asked here with the send
			// not requested, because a check sends nothing. A destination this
			// reports as refused is one a replay refuses; there is one
			// implementation of the rule and no second copy to drift from it.
			duration, _ := time.ParseDuration(config.ConnectTimeout)
			decisionCtx, stop := context.WithTimeout(ctx, duration)
			decision := sendpolicy.Decide(decisionCtx, policy, sendpolicy.Request{
				Address: config.Address, Classification: string(config.Environment().Classification),
			}, sendpolicy.SystemResolver)
			stop()
			if decisionPath != "" {
				if err := sendpolicy.WriteDecision(decisionPath, decision); err != nil {
					return err
				}
			}
			report, err := environment.Diagnose(ctx, config)
			if err != nil {
				return err
			}
			if err := writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				// The banner comes from the report, so the environment a
				// verdict names is the one the diagnosis was produced against.
				writeEnvironmentBanner(w, report.Environment)
				writeConfigurationLines(w, config)
				writeDecisionLines(w, decision)
				writeDiagnosisLines(w, report)
			}); err != nil {
				return err
			}
			if report.Outcome != environment.Reachable {
				return errors.New("the configured environment was not reached; the reported outcome names why")
			}
			return nil
		},
	}
	command.Flags().StringVar(&file, "target", "", "Target configuration file describing the environment")
	command.Flags().StringVar(&decisionPath, "decision", "", "New file retaining the send policy decision")
	command.Flags().StringVar(&policyPath, "policy", "", "Existing "+sendpolicy.PolicySchema+" document to report this environment's send decision against")
	return command
}

func targetReset(ran *bool) *cobra.Command {
	var file, planPath, outcomePath, policyPath string
	var confirmed []string
	command := &cobra.Command{
		Use:   "reset --target FILE --plan FILE --outcome NEW_FILE [--policy FILE] [--confirm ID]",
		Short: "Return one named nonproduction environment to its declared starting state through reviewed reset actions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			*ran = true
			config, err := readTargetFile(file)
			if err != nil {
				return resetError(err)
			}
			if planPath == "" {
				return resetError(errors.New("target reset requires --plan naming a " + fixturereset.PlanSchema + " document"))
			}
			if outcomePath == "" {
				return resetError(errors.New("target reset requires --outcome naming a new file to retain the reset outcome in"))
			}
			planBytes, err := readInputFile(planPath, fixturereset.MaxPlanBytes)
			if err != nil {
				return resetError(err)
			}
			// A read-authority action names its file inside the plan's own
			// directory, resolved after the plan's own symlink exactly as a
			// spec's relative paths are, so what a plan may read is fixed by
			// where the operator put the plan rather than by a working
			// directory readmit happened to be started in.
			resolved, err := artifactpath.Resolve(planPath)
			if err != nil {
				return resetError(errors.New("cannot resolve the reset plan"))
			}
			policy, err := readSendPolicy(policyPath)
			if err != nil {
				return resetError(err)
			}
			// The outcome destination is checked before anything runs. A reset
			// can open a connection, and a destination readmit was never going
			// to be able to write should not cost the environment one. Checking
			// the name here never authorizes overwriting it: the write below is
			// still exclusive, at the exact path the path owner returned.
			if _, err := artifactpath.Destination(outcomePath); err != nil {
				return resetError(err)
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			result, plan := fixturereset.Run(ctx, fixturereset.Request{
				Target: config, PlanBytes: planBytes, PlanDirectory: filepath.Dir(resolved),
				Policy: policy, Confirmed: confirmed,
			}, sendpolicy.SystemResolver)
			outcome, err := fixturereset.EncodeOutcome(result)
			if err != nil {
				return resetError(err)
			}
			// One reset attempt retains one document. The destination must be
			// new, so rerunning after performing a manual step needs a new file
			// and the attempt that stopped is still there to read.
			if err := writeNewFile(outcomePath, outcome,
				"cannot create the reset outcome file; the destination must be new and writable",
				"cannot write the reset outcome"); err != nil {
				return resetError(err)
			}
			if err := writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				writeEnvironmentBanner(w, config.Environment())
				writeConfigurationLines(w, config)
				writeResetLines(w, result)
				writeResetInstructionLines(w, plan, result)
			}); err != nil {
				return resetError(err)
			}
			if result.ExitCode() != 0 {
				return &ExitError{Code: result.ExitCode(), Err: errors.New("the fixture reset was not confirmed; the retained outcome names why")}
			}
			return nil
		},
	}
	command.Flags().StringVar(&file, "target", "", "Target configuration file describing the environment to reset")
	command.Flags().StringVar(&planPath, "plan", "", "Existing "+fixturereset.PlanSchema+" document declaring the reviewed reset actions")
	command.Flags().StringVar(&outcomePath, "outcome", "", "New file retaining the "+fixturereset.OutcomeSchema+" outcome")
	command.Flags().StringVar(&policyPath, "policy", "", "Existing "+sendpolicy.PolicySchema+" document the reset's one connection is held to")
	command.Flags().StringArrayVar(&confirmed, "confirm", nil, "Action id the operator performed and explicitly confirms; repeatable")
	return command
}

// resetError gives every refusal reached inside this command the same
// execution-error status a reset outcome carries, so nothing a reset does exits
// 1 and no caller has to read 1 as an assertion failure a reset cannot produce.
func resetError(err error) error { return &ExitError{Code: 2, Err: err} }

// writeResetLines renders one reset outcome and states its boundary. Every
// action is named with the operator it ran and the authority it ran under, so
// what the reset was permitted to do is read from the same place as what it
// established.
func writeResetLines(w io.Writer, result fixturereset.Result) {
	fmt.Fprintf(w, "Reset plan: %s (%s)\n", result.PlanSHA256, fixturereset.PlanSchema)
	if result.Decision != "" {
		fmt.Fprintf(w, "Reset connection: held to the send decision for this environment (%s); no HL7 payload is sent\n", result.Decision)
	}
	for _, action := range result.Actions {
		fmt.Fprintf(w, "  %s: %s authority=%s %s (%s)", action.ID, action.Operator, action.Authority, action.Outcome, action.Reason)
		if action.Diagnosis != "" {
			fmt.Fprintf(w, " diagnosis=%s", action.Diagnosis)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "Reset: %s (%s), execution state %s\n", result.Outcome, result.Reason, result.State)
	fmt.Fprintln(w, "Every action is a typed operator this release reviewed: a plan is data naming one and carries no command, script, interpreter or argument, and its instructions are executed by nothing.")
	if result.Outcome == fixturereset.Confirmed {
		fmt.Fprintln(w, "A confirmed reset establishes the declared starting state and nothing else: it is not evidence that an application processed, stored or forgot anything.")
		return
	}
	fmt.Fprintln(w, "A reset that failed, or that readmit could not confirm, is an execution error. It is never an assertion failure and never a pass.")
}

// writeResetInstructionLines prints what the person still has to do. Only an
// action awaiting their confirmation is printed, and only its own prose: this
// is the operator-assisted half of a reset, and it is the one place the
// instructions somebody wrote for themselves are of any use.
func writeResetInstructionLines(w io.Writer, plan fixturereset.Plan, result fixturereset.Result) {
	for _, action := range result.Actions {
		if action.Reason != fixturereset.AwaitingOperator {
			continue
		}
		prose := plan.Instructions(action.ID)
		if prose == "" {
			continue
		}
		fmt.Fprintf(w, "Awaiting %s. Perform it, then run target reset again with --confirm %s and a new --outcome file:\n", action.ID, action.ID)
		fmt.Fprintln(w, prose)
	}
}

// newEnvironment is what target set starts from when the file does not exist
// yet. The same values are this command's flag defaults, so what --help states
// and what a first configuration records cannot drift apart. test_endpoint is
// recorded true and reported back: it is the separate acknowledgement replay
// requires before it opens a connection, and it is not the classification.
func newEnvironment() replay.Target {
	return replay.Target{
		Schema: replay.TargetSchemaV3, TestEndpoint: true, Transport: "plain",
		ConnectTimeout: "2s", MessageTimeout: "5s", MaxACKBytes: 65536,
	}
}

func readTargetFile(path string) (replay.Target, error) {
	if path == "" {
		return replay.Target{}, errors.New("target requires --target naming a target configuration file")
	}
	return replay.ReadTarget(path)
}

// openOrNewTarget reads a configuration to edit, or starts the first one. A
// file readmit cannot read as this contract is reported, never replaced, and a
// configuration written under an earlier version is left exactly as it is:
// readmit-target/v1 and v2 are still read everywhere, and nothing rewrites one.
func openOrNewTarget(path string) (replay.Target, error) {
	if path == "" {
		return replay.Target{}, errors.New("target set requires --target naming the file to record this environment in")
	}
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return newEnvironment(), nil
	}
	config, err := replay.ReadDeclaredTarget(path)
	if err != nil {
		return replay.Target{}, err
	}
	if config.Schema != replay.TargetSchemaV3 {
		return replay.Target{}, errors.New("that file declares " + config.Schema + " and target set records " + replay.TargetSchemaV3 + "; record the named environment in a new file and leave this one as it is")
	}
	return config, nil
}

// writeConfigurationLines renders one configuration below its banner. Declared
// file paths are not echoed; the transport material is reported as configured
// or absent, exactly as run evidence reports a CA without naming its path.
func writeConfigurationLines(w io.Writer, config replay.Target) {
	fmt.Fprintf(w, "Contract: %s\n", config.Schema)
	fmt.Fprintf(w, "Endpoint: %s (%s)\n", config.Address, config.Transport)
	fmt.Fprintf(w, "Acknowledged as a test endpoint: %t  Transport explicitly approved: %t\n", config.TestEndpoint, config.ApprovedTransport)
	fmt.Fprintf(w, "Timeouts: connect=%s message=%s  Largest acknowledgement: %d bytes\n", config.ConnectTimeout, config.MessageTimeout, config.MaxACKBytes)
	if config.Transport != "tls" {
		return
	}
	roots := "explicitly configured, replacing the system roots"
	if config.CAFile == "" {
		roots = "system roots"
	}
	fmt.Fprintf(w, "Certificate authority: %s\n", roots)
	fmt.Fprintf(w, "Verified server name: %s\n", absent(config.ServerName))
	if config.ClientCertificate == "" {
		fmt.Fprintln(w, "Client certificate: none configured")
		return
	}
	fmt.Fprintf(w, "Client certificate: configured; its private key is %s, read through the reference %q\n", secret.Mask, config.Credential.Reference)
}

// writeEnvironmentBanner states what a command is pointed at before it reports
// anything else. The classification is stated with what it is worth beside it:
// it is what somebody recorded, and readmit neither established it nor treats
// it as permission to send anywhere. It can only refuse, and what a send may
// reach is decided separately against resolved addresses. See docs/target.md.
func writeEnvironmentBanner(w io.Writer, named replay.Environment) {
	fmt.Fprintf(w, "Environment: %s\n", absent(named.Name))
	fmt.Fprintf(w, "Classification: %s (recorded by a person; readmit did not establish it and never reads it as permission)\n", named.Classification)
}

// writeDiagnosisLines renders one diagnosis and states its boundary. An expiry
// is reported as the certificate states it and nothing acts on it.
func writeDiagnosisLines(w io.Writer, report environment.Report) {
	fmt.Fprintf(w, "Diagnosis: %s (phase=%s)\n", report.Outcome, report.Phase)
	if report.Peer != "" {
		fmt.Fprintf(w, "Reached: %s\n", report.Peer)
	}
	if report.Unsolicited > 0 {
		fmt.Fprintf(w, "Received without being asked: %d bytes, retained nowhere and interpreted as nothing\n", report.Unsolicited)
	}
	if report.TLS != nil {
		fmt.Fprintf(w, "TLS: %s cipher=%s server_name=%s client_certificate=requested:%t presented:%t\n",
			report.TLS.Version, report.TLS.CipherSuite, report.TLS.ServerName,
			report.TLS.ClientCertificateRequested, report.TLS.ClientCertificatePresented)
		now := time.Now()
		for i, certificate := range report.TLS.Chain {
			fmt.Fprintf(w, "  certificate %d: subject=%s issuer=%s not_before=%s not_after=%s expires_in=%s\n",
				i+1, quoted(certificate.Subject), quoted(certificate.Issuer),
				certificate.NotBefore.Format(time.RFC3339), certificate.NotAfter.Format(time.RFC3339),
				certificate.NotAfter.Sub(now).Truncate(time.Second))
		}
	}
	for i, certificate := range report.Unverified {
		fmt.Fprintf(w, "  unverified certificate %d: subject=%s issuer=%s not_before=%s not_after=%s\n",
			i+1, quoted(certificate.Subject), quoted(certificate.Issuer),
			certificate.NotBefore.Format(time.RFC3339), certificate.NotAfter.Format(time.RFC3339))
	}
	if len(report.Unverified) > 0 {
		fmt.Fprintln(w, "  the endpoint presented these and verification refused them, so they identify nothing here.")
	}
	fmt.Fprintln(w, "No HL7 payload was sent. Reaching an endpoint and verifying its certificate are evidence about the transport only:")
	fmt.Fprintln(w, "they are not evidence that an application accepted, processed or stored anything, and an expiry above is reported, not acted on.")
}

// quoted renders a name an endpoint chose. A certificate readmit is describing
// may be one verification refused, so its subject is bytes from the network
// like any other: it is escaped to printable ASCII rather than written through.
func quoted(name string) string { return strconv.QuoteToASCII(name) }
