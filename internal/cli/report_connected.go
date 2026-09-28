package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/runcompare"
	"github.com/spf13/cobra"
)

// reportConnectedCommand is retained proof of actual connected lifecycle runs:
// a versioned packet, its offline verification and reanalysis, comparison,
// the portable review and the disclosure-reviewed extract. None of them sends,
// reruns or contacts a source; console output names identities, states and
// surfaces, never observed values, paths outside the packet or endpoints.
func reportConnectedCommand() *cobra.Command {
	root := &cobra.Command{Use: "connected", Short: "Retain, verify, compare, review and extract actual connected lifecycle proof offline"}
	var input report.ConnectedInput
	var output string
	assemble := &cobra.Command{Use: "assemble --current RESULT --output NEW_PACKET", Short: "Copy retained connected lifecycle results into a sealed customer-local packet without sending", Annotations: declareInterruptible(capabilityFree), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		packet, err := report.AssembleConnected(cmd.Context(), input, output)
		if err != nil {
			return err
		}
		m := packet.Manifest
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Connected packet complete: %s\nCustomer-local original evidence; source values retained; no disclosure approval\nCurrent: %s (%s)\nBaseline present: %t\nRegression equivalence: %s\n", packet.Identity, m.Current.Verdict, m.Current.State, m.Baseline != nil, m.Equivalence.State)
		if err != nil {
			return errors.New("cannot write connected packet summary")
		}
		return nil
	}}
	assemble.Flags().StringVar(&input.Current, "current", "", "Retained current connected lifecycle result")
	assemble.Flags().StringVar(&input.Baseline, "baseline", "", "Optional distinct retained baseline lifecycle result")
	assemble.Flags().StringVar(&input.Replay, "replay", "", "Optional distinct retained replay of the same plan and target, for a reproduction claim")
	assemble.Flags().StringVar(&output, "output", "", "New private packet directory; never overwrite")

	var asJSON bool
	verify := &cobra.Command{Use: "verify PACKET", Short: "Verify a connected packet offline and re-analyze it without contacting any source", Annotations: declare(capabilityFree), Args: reportOneArgument, RunE: func(cmd *cobra.Command, args []string) error {
		packet, err := report.OpenConnected(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if asJSON {
			// The console names identities and states, never an endpoint:
			// each pinned server keeps its ID and capability digest only.
			endpointFree := func(run *report.ConnectedRun) *report.ConnectedRun {
				if run == nil {
					return nil
				}
				copied := *run
				copied.Environment.Servers = []report.ConnectedServer{}
				for _, server := range run.Environment.Servers {
					copied.Environment.Servers = append(copied.Environment.Servers, report.ConnectedServer{ID: server.ID, Capability: server.Capability})
				}
				return &copied
			}
			return writeJSON(cmd, struct {
				Identity    string                       `json:"identity"`
				Current     report.ConnectedRun          `json:"current"`
				Baseline    *report.ConnectedRun         `json:"baseline"`
				Replay      *report.ConnectedRun         `json:"replay"`
				Equivalence report.ConnectedEquivalence  `json:"equivalence"`
				Reanalysis  []report.ConnectedReanalysis `json:"reanalysis"`
			}{packet.Identity, *endpointFree(&packet.Manifest.Current), endpointFree(packet.Manifest.Baseline), endpointFree(packet.Manifest.Replay), packet.Manifest.Equivalence, packet.Reanalysis})
		}
		var out strings.Builder
		fmt.Fprintf(&out, "Connected packet verified: %s\nCustomer-local only; integrity is not disclosure approval, source authentication or target software identity\nRegression equivalence: %s\n", packet.Identity, packet.Manifest.Equivalence.State)
		for _, r := range packet.Reanalysis {
			fmt.Fprintf(&out, "%s: original %s by engine %s; reanalysis %s by engine %s\n", r.Section, r.OriginalVerdict, r.OriginalEngine, r.ReanalyzedVerdict, r.CurrentEngine)
			for _, limitation := range r.Limitations {
				fmt.Fprintf(&out, "  %s\n", limitation)
			}
		}
		if _, err := fmt.Fprint(cmd.OutOrStdout(), out.String()); err != nil {
			return errors.New("cannot write connected verification")
		}
		return nil
	}}
	verify.Flags().BoolVar(&asJSON, "json", false, "Write the verified claims and reanalysis as JSON")

	var compareJSON bool
	compare := &cobra.Command{Use: "compare BASELINE CURRENT", Short: "Compare two retained connected lifecycle results offline by declared dimension, check and keyed record", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := runcompare.CompareFlows(cmd.Context(), args[0], args[1])
		if err != nil {
			return err
		}
		if compareJSON {
			return writeJSON(cmd, c)
		}
		var out strings.Builder
		fmt.Fprintf(&out, "Baseline: %s %s (%s)\nCurrent: %s %s (%s)\n", c.Baseline.Identity, c.Baseline.Verdict, c.Baseline.State, c.Current.Identity, c.Current.Verdict, c.Current.State)
		for _, d := range c.Dimensions {
			fmt.Fprintf(&out, "%s: %s\n", d.Dimension, d.State)
		}
		fmt.Fprintf(&out, "Attribution: %s\n", c.Attribution.Outcome)
		for _, check := range c.Checks {
			fmt.Fprintf(&out, "%s %s: %s -> %s; definition %s; behavior %s\n", check.Phase, check.Check, check.Baseline, check.Current, check.Definition, check.Behavior)
		}
		for _, r := range c.Records {
			fmt.Fprintf(&out, "records %s %s: %s", r.Phase, r.Dataset, r.State)
			for i, k := range r.Keys {
				fmt.Fprintf(&out, "; key %d %d -> %d %s", i+1, k.Baseline, k.Current, k.State)
			}
			out.WriteString("\n")
		}
		if _, err := fmt.Fprint(cmd.OutOrStdout(), out.String()); err != nil {
			return errors.New("cannot write connected comparison")
		}
		return nil
	}}
	compare.Flags().BoolVar(&compareJSON, "json", false, "Write the comparison as JSON")

	var reviewOutput string
	export := &cobra.Command{Use: "export PACKET --output NEW_REVIEW", Short: "Export a connected packet and its typed report in five inert formats into a sealed private review", Annotations: declareInterruptible(capabilityFree), Args: reportOneArgument, RunE: func(cmd *cobra.Command, args []string) error {
		review, err := report.ExportConnectedReview(cmd.Context(), args[0], reviewOutput)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Connected portable review sealed: %s\nCustomer-local sensitive evidence; no disclosure approval\n", review.Identity); err != nil {
			return errors.New("cannot write connected review summary")
		}
		return nil
	}}
	export.Flags().StringVar(&reviewOutput, "output", "", "New private review directory; never overwrite")

	var format string
	review := &cobra.Command{Use: "review REVIEW", Short: "Verify a connected portable review offline in read-only mode", Annotations: declare(capabilityFree), Args: reportOneArgument, RunE: func(cmd *cobra.Command, args []string) error {
		r, err := report.OpenConnectedReview(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if format == "" {
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Read-only connected review verified: %s\nCustomer-local sensitive evidence; no execution or disclosure approval\n", r.Identity)
		} else {
			var data []byte
			if data, err = r.Render(format); err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
		}
		if err != nil {
			return errors.New("cannot write connected review")
		}
		return nil
	}}
	review.Flags().StringVar(&format, "format", "", "Explicit sensitive content to stdout: html, pdf, markdown, json or junit")

	var policy, approve, extractOutput, keyPath, previewFormat string
	extract := &cobra.Command{Use: "extract PACKET --policy POLICY", Short: "Preview or publish a disclosure-reviewed extract of a connected packet: value-free (policy v1) or reviewed transformed (policy v2)", Annotations: declareInterruptible(capabilityFree), Args: reportOneArgument, RunE: func(cmd *cobra.Command, args []string) error {
		version, err := report.DisclosurePolicyVersion(policy)
		if err != nil {
			return err
		}
		if version == report.TransformPolicySchema {
			return transformedExtract(cmd, args[0], policy, keyPath, previewFormat, approve, extractOutput)
		}
		if keyPath != "" || previewFormat != "" {
			return errors.New("--key and --format apply to a reviewed transformed extract (policy v2) only")
		}
		candidate, err := report.PrepareExtract(cmd.Context(), args[0], policy)
		if err != nil {
			return err
		}
		if approve == "" && extractOutput == "" {
			var out strings.Builder
			fmt.Fprintf(&out, "Extract preview: %s\nValue-free; nothing written\n", candidate.Identity())
			for _, item := range candidate.Extract.Inventory {
				fmt.Fprintf(&out, "%s: %d files, %s\n", item.Surface, item.Files, item.Disposition)
			}
			for _, reason := range candidate.Blocked {
				fmt.Fprintf(&out, "Blocked: %s\n", reason)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), out.String()); err != nil {
				return errors.New("cannot write extract preview")
			}
			if len(candidate.Blocked) > 0 {
				return errors.New("the extract is blocked; map every surface the packet holds or keep the evidence customer-local")
			}
			return nil
		}
		if approve == "" || extractOutput == "" {
			return errors.New("publishing an extract requires both --approve EXACT_PREVIEW_ID and --output NEW_EXTRACT")
		}
		if err := candidate.Publish(cmd.Context(), approve, extractOutput); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Extract published: %s\nValue-free; not an equivalent reproducer; not a de-identification determination\n", candidate.Identity()); err != nil {
			return errors.New("cannot write extract summary")
		}
		return nil
	}}
	extract.Flags().StringVar(&policy, "policy", "", "Reviewed readmit-connected-disclosure-policy/v1 (value-free) or /v2 (transformed) mapping every surface")
	extract.Flags().StringVar(&approve, "approve", "", "Exact preview identity to publish")
	extract.Flags().StringVar(&extractOutput, "output", "", "New extract directory; never overwrite")
	extract.Flags().StringVar(&keyPath, "key", "", "Customer-local pseudonym key for a transformed extract; never exported")
	extract.Flags().StringVar(&previewFormat, "format", "", "Preview a transformed extract's derived content to stdout: json, markdown or html")

	var keyOutput string
	pseudonymKey := &cobra.Command{Use: "pseudonym-key --output NEW_KEY_FILE", Short: "Create a private customer-local key for the pseudonyms of transformed extracts", Annotations: declare(capabilityFree), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if keyOutput == "" {
			return errors.New("select a new key file with --output")
		}
		if err := report.WritePseudonymKey(keyOutput); err != nil {
			return err
		}
		if _, err := fmt.Fprint(cmd.OutOrStdout(), "Pseudonym key created; keep it customer-local and private. It is never part of an extract.\n"); err != nil {
			return errors.New("cannot write pseudonym key summary")
		}
		return nil
	}}
	pseudonymKey.Flags().StringVar(&keyOutput, "output", "", "New private key file; never overwrite")

	var revalidation report.RevalidationOptions
	var revalidationOutput string
	revalidate := &cobra.Command{Use: "revalidate PACKET --capability INSTALLED_CAPABILITY --output NEW_ANALYSIS", Short: "Run the installed, identically pinned FHIR validator again on a packet's retained resources as a separate analysis", Annotations: declareInterruptible(capabilityFree), Args: reportOneArgument, RunE: func(cmd *cobra.Command, args []string) error {
		analysis, err := report.RevalidateConnected(cmd.Context(), args[0], revalidation, revalidationOutput)
		if err != nil {
			return err
		}
		return writeRevalidation(cmd, analysis)
	}}
	revalidate.Flags().StringVar(&revalidation.Capability, "capability", "", "The administrator's staged validator capability; it must be the historical pin")
	revalidate.Flags().StringVar(&revalidation.Engine, "engine", "local", "local runs the worker in the local container engine; none records what is not run")
	revalidate.Flags().StringVar(&revalidation.Socket, "socket", "", "Local container engine socket (default: the engine's own)")
	revalidate.Flags().StringVar(&revalidationOutput, "output", "", "New analysis directory beside the packet; never overwrite")

	revalidationView := &cobra.Command{Use: "revalidation ANALYSIS PACKET", Short: "Verify a connected revalidation offline against its packet without starting anything", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		analysis, err := report.OpenConnectedRevalidation(cmd.Context(), args[0], args[1])
		if err != nil {
			return err
		}
		return writeRevalidation(cmd, analysis)
	}}

	root.AddCommand(assemble, verify, compare, export, review, extract, pseudonymKey, revalidate, revalidationView)
	return root
}

// writeRevalidation names identities, states and reasons, never a finding's
// text, a path outside the analysis or an endpoint.
func writeRevalidation(cmd *cobra.Command, analysis *report.ConnectedRevalidation) error {
	m := analysis.Manifest
	var out strings.Builder
	fmt.Fprintf(&out, "Connected revalidation: %s\nPacket: %s\nInstalled capability: %s\nEngine: %s (%s)\nHistorical verdicts unchanged; this is a separate analysis\n", analysis.Identity, m.PacketIdentity, orNone(m.Capability), m.Engine.Selection, m.Engine.State)
	for _, v := range m.Validations {
		fmt.Fprintf(&out, "%s %s validation:%s: %s", v.Section, v.Phase, v.Check, v.Status)
		if v.Reason != "" {
			fmt.Fprintf(&out, " (%s)", v.Reason)
		}
		if v.Historical != nil {
			fmt.Fprintf(&out, "; historical %s %s", v.Historical.State, v.Historical.Verdict)
		}
		if v.Revalidated != nil {
			fmt.Fprintf(&out, "; now %s %s", v.Revalidated.State, v.Revalidated.Verdict)
		}
		fmt.Fprintf(&out, "; %s\n", v.Agreement)
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), out.String()); err != nil {
		return errors.New("cannot write connected revalidation")
	}
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "none selected"
	}
	return s
}

// transformedExtract previews or publishes a reviewed transformed extract.
// The preview names identities, surfaces and reasons; derived content reaches
// stdout only when --format asks for it.
func transformedExtract(cmd *cobra.Command, packet, policy, keyPath, format, approve, output string) error {
	if keyPath == "" {
		return errors.New("a transformed extract requires the customer-local pseudonym key (--key)")
	}
	key, err := report.ReadPseudonymKey(keyPath)
	if err != nil {
		return err
	}
	candidate, err := report.PrepareTransformedExtract(cmd.Context(), packet, policy, key)
	if err != nil {
		return err
	}
	if approve == "" && output == "" {
		if format != "" {
			data, err := candidate.Render(format)
			if err != nil {
				return err
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return errors.New("cannot write extract preview")
			}
		} else {
			var out strings.Builder
			fmt.Fprintf(&out, "Transformed extract preview: %s\nDerived by the reviewed policy; nothing written\n", candidate.Identity())
			for _, item := range candidate.Extract.Inventory {
				fmt.Fprintf(&out, "%s: %d files, %s\n", item.Surface, item.Files, item.Disposition)
			}
			for _, reason := range candidate.Blocked {
				fmt.Fprintf(&out, "Blocked: %s\n", reason)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), out.String()); err != nil {
				return errors.New("cannot write extract preview")
			}
		}
		if len(candidate.Blocked) > 0 {
			return errors.New("the extract is blocked; map every surface the packet holds, keep only what the policy may disclose, or keep the evidence customer-local")
		}
		return nil
	}
	if format != "" {
		return errors.New("--format previews only; publish without it")
	}
	if approve == "" || output == "" {
		return errors.New("publishing an extract requires both --approve EXACT_PREVIEW_ID and --output NEW_EXTRACT")
	}
	if err := candidate.Publish(cmd.Context(), approve, output); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Transformed extract published: %s\nDerived, not original evidence; not an equivalent reproducer; not a de-identification determination\n", candidate.Identity()); err != nil {
		return errors.New("cannot write extract summary")
	}
	return nil
}
