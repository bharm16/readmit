package cli

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bharm16/readmit/internal/sharing"
	"github.com/spf13/cobra"
)

func shareCommand(ran *bool) *cobra.Command {
	var r sharing.Request
	var approve, output string
	cmd := &cobra.Command{Use: "share SOURCE --kind KIND --policy POLICY", Short: "Review value-free support diagnostics; never upload evidence", Annotations: declare(capabilityFree), Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return errors.New("share requires one source")
		}
		return nil
	}, RunE: func(cmd *cobra.Command, args []string) (err error) {
		*ran = true
		action := "refused"
		defer func() {
			if _, e := fmt.Fprintf(cmd.ErrOrStderr(), "{\"schema\":\"readmit-sharing-security-event/v1\",\"action\":%q}\n", action); err == nil && e != nil {
				err = errors.New("cannot record sharing security event")
			}
		}()
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		r.Source = args[0]
		candidate, e := sharing.Prepare(ctx, r)
		if e != nil {
			return sharing.ErrRefused
		}
		if output == "" {
			if approve != "" {
				return sharing.ErrRefused
			}
			if _, e = fmt.Fprintf(cmd.OutOrStdout(), "%s\nApproval identity: %s\nLocal byte review only; use the hub for authenticated team approval.\n", candidate.Bytes(), candidate.Identity()); e != nil {
				return errors.New("cannot write support preview")
			}
			action = "prepared"
			return nil
		}
		if e = candidate.Publish(ctx, approve, output); e != nil {
			return sharing.ErrRefused
		}
		action = "published"
		_, e = fmt.Fprintf(cmd.OutOrStdout(), "Reviewed support summary: %s\nNo evidence payloads; external equivalence declined.\n", candidate.Identity())
		if e != nil {
			return errors.New("cannot write support summary")
		}
		return nil
	}}
	cmd.Flags().StringVar(&r.Kind, "kind", "", "retained-packet, portable-review or derived-review")
	cmd.Flags().StringVar(&r.Private, "local-state", "", "Private source linkage, required for a derived review; never copied")
	cmd.Flags().StringVar(&r.Policy, "policy", "", "Explicit readmit-sharing-policy/v1")
	cmd.Flags().StringVar(&approve, "approve", "", "Exact preview identity; local approval is not authenticated team approval")
	cmd.Flags().StringVar(&output, "output", "", "New private support directory; omit to preview")
	cmd.AddCommand(&cobra.Command{
		Use: "verify SUPPORT", Short: "Verify a local reviewed support bundle without opening its source", Annotations: declare(capabilityFree),
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("share verify requires one bundle")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if cmd.Context().Err() != nil {
				return sharing.ErrRefused
			}
			summary, err := sharing.Open(args[0])
			if err != nil {
				return sharing.ErrRefused
			}
			raw, err := json.Marshal(summary, json.Deterministic(true))
			if err != nil {
				return sharing.ErrRefused
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\nVerified support identity: %s\nIntegrity only; confirm customer authorization and approved recipient separately.\n", raw, sharing.Digest(raw))
			if err != nil {
				return errors.New("cannot write support verification")
			}
			return nil
		},
	})
	return cmd
}
