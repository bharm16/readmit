package cli

import (
	"encoding/json/v2"
	"fmt"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/spf13/cobra"
)

func runnerCommand(ran *bool) *cobra.Command {
	root := &cobra.Command{Use: "runner", Short: "Operate a customer-controlled, hub-enrolled local runner"}
	var config string
	root.PersistentFlags().StringVar(&config, "config", "", "Private runner configuration path")
	for _, operation := range []string{"enroll", "status", "execute", "serve", "verify-update"} {
		use := operation
		count := 0
		switch operation {
		case "execute":
			use += " JOB"
			count = 1
		case "serve":
			use += " INBOX"
			count = 1
		case "verify-update":
			use += " MANIFEST BINARY"
			count = 2
		}
		var send bool
		// The long-lived runner checks each job through the installed guard, so
		// execute and serve declare free like the read-only runner operations.
		cmd := &cobra.Command{Use: use, Args: cobra.ExactArgs(count), Annotations: declare(capabilityFree), RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			ctx, cancel, err := runContext(cmd.Context(), "")
			if err != nil {
				return err
			}
			defer cancel()
			c, err := customerrunner.ReadConfig(config)
			if err != nil {
				return err
			}
			var result any
			switch operation {
			case "enroll":
				result, err = customerrunner.Enroll(ctx, c)
			case "status":
				result, err = customerrunner.Health(c.Root)
			case "execute":
				if !send {
					return fmt.Errorf("runner execution requires --send")
				}
				job, e := customerrunner.ReadJob(args[0])
				if e != nil {
					return e
				}
				summary, e := customerrunner.Run(ctx, c, job)
				if e != nil {
					return e
				}
				return printRun(cmd, summary, true, nil)
			case "serve":
				if !send {
					return fmt.Errorf("runner service requires --send")
				}
				return customerrunner.Serve(ctx, c, args[0])
			case "verify-update":
				if e := customerrunner.VerifyUpdate(c, args[0], args[1]); e != nil {
					return e
				}
				result = struct {
					Schema   string `json:"schema"`
					Verified bool   `json:"verified"`
				}{"readmit-runner-update-check/v1", true}
			}
			if err != nil {
				return err
			}
			data, err := json.Marshal(result)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return err
		}}
		if operation == "execute" || operation == "serve" {
			cmd.Flags().BoolVar(&send, "send", false, "Explicitly authorize approved nonproduction execution")
		}
		root.AddCommand(cmd)
	}
	return root
}
