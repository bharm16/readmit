package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/spf13/cobra"
)

func listenCommand(ran *bool) *cobra.Command {
	var address, mode string
	var config receiver.Config
	command := &cobra.Command{
		Use:   "listen --output NEW_DIRECTORY --observation NEW_FILE",
		Short: "Run a controlled SIU test fixture and export its appointment ledger",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			config.Mode = observation.Mode(mode)
			if address == "" {
				return errors.New("listen address cannot be empty")
			}
			listener, err := net.Listen("tcp", address)
			if err != nil {
				return errors.New("cannot bind receiver address")
			}
			defer listener.Close()
			r, err := receiver.New(config)
			if err != nil {
				return err
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Listening: %s\nProfile: %s\nMode: %s\n", listener.Addr(), observation.Profile, mode); err != nil {
				return errors.New("cannot write receiver startup output")
			}
			b, err := r.Serve(ctx, listener)
			if b != nil {
				if outErr := renderBundle(cmd.OutOrStdout(), b, false, false); outErr != nil {
					return errors.Join(err, outErr)
				}
			}
			return err
		},
	}
	command.Flags().StringVar(&address, "address", "127.0.0.1:2575", "TCP listen address; port 0 chooses an available port")
	command.Flags().StringVar(&mode, "mode", "fixed", "Fixture behavior: fixed or defective (both return AA)")
	command.Flags().StringVar(&config.OutputPath, "output", "", "New final case bundle directory")
	command.Flags().StringVar(&config.ObservationPath, "observation", "", "New live observation JSON file, atomically replaced during this session")
	command.Flags().IntVar(&config.MaxFrameBytes, "max-frame-bytes", 1<<20, "Maximum MLLP payload bytes, excluding the three framing bytes")
	command.Flags().DurationVar(&config.IdleTimeout, "idle-timeout", 30*time.Second, "Maximum interval without receiving bytes, and ACK write timeout")
	command.Flags().IntVar(&config.MaxMessages, "max-messages", 0, "Finalize after this many complete inbound frames; 0 waits for cancellation")
	return command
}
