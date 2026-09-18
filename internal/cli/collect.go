package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/spf13/cobra"
)

func collectCommand(ran *bool) *cobra.Command {
	var address, policyPath string
	var config receiver.CollectorConfig
	command := &cobra.Command{
		Use:   "collect --policy FILE --output NEW_DIRECTORY",
		Short: "Collect downstream HL7 over MLLP under a declared acknowledgement policy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if address == "" {
				return errors.New("collect address cannot be empty")
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
			listener, err := net.Listen("tcp", address)
			if err != nil {
				return errors.New("cannot bind collector address")
			}
			defer listener.Close()
			c, err := receiver.NewCollector(config)
			if err != nil {
				return err
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Listening: %s\nPolicy: %s\nSource label: %s\nAcknowledgement: %s %s\nApplication processing: %s\n",
				listener.Addr(), config.Policy.Name, config.Policy.SourceLabel, config.Policy.Acknowledgement.Operator, config.Policy.Acknowledgement.Code, collection.NoApplicationProcessing); err != nil {
				return errors.New("cannot write collector startup output")
			}
			b, err := c.Serve(ctx, listener)
			if b != nil {
				if outErr := renderBundle(cmd.OutOrStdout(), b, false, false); outErr != nil {
					return errors.Join(err, outErr)
				}
			}
			return err
		},
	}
	command.Flags().StringVar(&address, "address", "127.0.0.1:2575", "TCP listen address; port 0 chooses an available port")
	command.Flags().StringVar(&policyPath, "policy", "", "Existing readmit-receiver-policy/v1 JSON file")
	command.Flags().StringVar(&config.OutputPath, "output", "", "New final case bundle directory")
	command.Flags().IntVar(&config.MaxFrameBytes, "max-frame-bytes", 1<<20, "Maximum MLLP payload bytes, excluding the three framing bytes")
	command.Flags().DurationVar(&config.IdleTimeout, "idle-timeout", 30*time.Second, "Maximum interval without receiving bytes, and acknowledgement write timeout")
	command.Flags().IntVar(&config.MaxMessages, "max-messages", 0, "Finalize after this many complete inbound frames; 0 waits for cancellation")
	return command
}
