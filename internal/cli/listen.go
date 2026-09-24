package cli

import (
	"errors"
	"fmt"

	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/spf13/cobra"
)

func listenCommand() *cobra.Command {
	var mode string
	var config operation.ListenConfig
	command := &cobra.Command{
		Use:         "listen --output NEW_DIRECTORY --observation NEW_FILE",
		Annotations: declareInterruptible(capabilityExecute),
		Short:       "Run a controlled SIU test fixture and export its appointment ledger",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			config.Mode = observation.Mode(mode)
			config.Listening = func(bound string) error {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Listening: %s\nProfile: %s\nMode: %s\n", bound, observation.Profile, mode); err != nil {
					return errors.New("cannot write receiver startup output")
				}
				return nil
			}
			result, err := operation.StartListen(cmd.Context(), config)
			if result.Bundle != nil {
				if outErr := renderBundle(cmd.OutOrStdout(), result.Bundle, false, false); outErr != nil {
					return errors.Join(err, outErr)
				}
			}
			return err
		},
	}
	command.Flags().StringVar(&config.Address, "address", "127.0.0.1:2575", "TCP listen address; port 0 chooses an available port")
	command.Flags().BoolVar(&config.ApprovedBind, "approved-bind", false, "Explicitly approve binding a nonloopback address, accepting connections from beyond this machine")
	command.Flags().StringVar(&mode, "mode", "fixed", "Fixture behavior: fixed or defective (both return AA)")
	command.Flags().StringVar(&config.OutputPath, "output", "", "New final case bundle directory")
	command.Flags().StringVar(&config.ObservationPath, "observation", "", "New live observation JSON file, atomically replaced during this session")
	command.Flags().IntVar(&config.MaxFrameBytes, "max-frame-bytes", operation.DefaultMaxFrameBytes, "Maximum MLLP payload bytes, excluding the three framing bytes")
	command.Flags().DurationVar(&config.IdleTimeout, "idle-timeout", operation.DefaultIdleTimeout, "Maximum interval without receiving bytes, and ACK write timeout")
	command.Flags().IntVar(&config.MaxMessages, "max-messages", 0, "Finalize after this many complete inbound frames; 0 waits for cancellation")
	return command
}
