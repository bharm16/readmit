package cli

import (
	"errors"
	"fmt"

	"github.com/bharm16/readmit/internal/redact"
	"github.com/spf13/cobra"
)

func redactCommand() *cobra.Command {
	var request redact.Request
	cmd := &cobra.Command{Use: "redact CASE", Short: "Derive testing evidence and a fail-closed export review", Annotations: declare(capabilityAuthor), RunE: func(cmd *cobra.Command, args []string) error {
		if request.SpecPath == "" || request.PolicyPath == "" || request.InventoryPath == "" || request.Output == "" || request.LocalState == "" {
			return usage("redact requires spec, policy, inventory, output, and separate local-state paths")
		}
		request.CasePath = args[0]
		review, err := redact.Create(cmd.Context(), request)
		if err != nil {
			return err
		}
		if review.State != "ready-for-approval" {
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Redaction review blocked: %d located findings. Inspect the local review; no share approval exists.\n", len(review.Findings))
			if err == nil && review.OriginalProofFailure != "" {
				_, err = fmt.Fprintf(cmd.ErrOrStderr(), "Original fixture proof failed: %s\n", review.OriginalProofFailure)
			}
			if err != nil {
				return err
			}
			return statedRefusal(errors.New("unresolved redaction review"))
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Review ready for explicit approval: %s\nThe original fixture failure and fixed pass are verified. Export will rerun the derived case and scan the generated packet.\n", review.Identity)
		return err
	}}
	cmd.Flags().StringVar(&request.SpecPath, "spec", "", "Original readmit-test/v1 spec")
	cmd.Flags().StringVar(&request.PolicyPath, "policy", "", "Explicit readmit-redact-policy/v1 named policies")
	cmd.Flags().StringVar(&request.InventoryPath, "inventory", "", "Complete declared inventory of original artifacts and known residual values")
	cmd.Flags().StringVar(&request.LocalState, "local-state", "", "New private directory outside all derived and shared output")
	cmd.Flags().StringVar(&request.Output, "output", "", "New review directory outside immutable sources")
	var export redact.ExportRequest
	child := &cobra.Command{Use: "export REVIEW", Short: "Approve an exact review and generate a proven fixture packet", Annotations: declare(capabilityFree), RunE: func(cmd *cobra.Command, args []string) error {
		if export.LocalState == "" || export.Approval == "" || export.Output == "" {
			return usage("redact export requires local-state, approve, and output")
		}
		export.ReviewPath = args[0]
		manifest, err := redact.Export(cmd.Context(), export)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Derived export generated: %d files; exact defective assertion failures and full fixed pass verified. Limited coverage and residual checks are recorded in export-review.json.\n", len(manifest.Files))
		return err
	}}
	child.Flags().StringVar(&export.LocalState, "local-state", "", "Private mapping and original-proof directory")
	child.Flags().StringVar(&export.Approval, "approve", "", "Exact reviewed identity; approval is not a legal determination")
	child.Flags().StringVar(&export.Output, "output", "", "New packet directory")
	cmd.AddCommand(child, reexecuteCommand())
	return cmd
}
