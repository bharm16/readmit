package cli

import (
	"encoding/json/v2"
	"errors"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/testisolation"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/spf13/cobra"
)

func runConnectedTest(cmd *cobra.Command, plan, config, instance, output string, send bool) error {
	raw, readErr := (artifactdir.Document{MaxBytes: 2 << 20}).Read(config)
	var head struct {
		Schema string `json:"schema"`
	}
	if readErr == nil && json.Unmarshal(raw, &head) == nil && (head.Schema == connectedrun.FlowConfigSchema || head.Schema == connectedrun.FHIRFlowConfigSchema || head.Schema == connectedrun.RuntimeFlowConfigSchema) {
		return runConnectedFlow(cmd, plan, config, instance, output, send)
	}
	prepared, err := connectedrun.Prepare(plan, config)
	if err != nil {
		return refusal(err)
	}
	if !send {
		return writeConnected(cmd, struct {
			Prepared bool   `json:"prepared"`
			Verdict  string `json:"verdict"`
			Bindings any    `json:"bindings"`
		}{true, "not-evaluated", prepared.Bindings()})
	}
	if instance == "" {
		return usage("connected test execution requires --instance")
	}
	result, err := connectedrun.Execute(cmd.Context(), prepared, instance, output)
	if err != nil {
		return refusal(err)
	}
	if err = writeConnected(cmd, struct {
		State      string            `json:"state"`
		Verdict    assertion.Verdict `json:"verdict"`
		Phase      string            `json:"phase"`
		Boundaries map[string]string `json:"boundaries,omitzero"`
	}{result.State, result.Verdict, result.Phase, result.Boundaries()}); err != nil {
		return err
	}
	if result.State != "complete" || result.Verdict == assertion.VerdictUndecided {
		return verdict(2, errors.New("connected test execution is unresolved; retained evidence is private"))
	}
	if result.Verdict == assertion.VerdictFail {
		return verdict(1, errors.New("connected test assertions failed"))
	}
	return nil
}

func runConnectedFlow(cmd *cobra.Command, plan, config, instance, output string, send bool) error {
	p, err := connectedrun.PrepareFlow(plan, config, instance)
	if err != nil {
		return refusal(err)
	}
	if !send {
		return writeConnected(cmd, struct {
			Prepared  bool   `json:"prepared"`
			Verdict   string `json:"verdict"`
			Bindings  any    `json:"bindings"`
			Isolation any    `json:"isolation"`
		}{true, "not-evaluated", p.Bindings(), p.IsolationReviews()})
	}
	steps, _ := cmd.Flags().GetStringArray("confirm-setup-step")
	result, err := connectedrun.ExecuteFlow(cmd.Context(), p, output, testisolation.Confirmation{Plan: p.IsolationIdentity(), Instance: instance, Steps: steps})
	if err != nil {
		return refusal(err)
	}
	return printConnectedFlow(cmd, result)
}
func printConnectedFlow(cmd *cobra.Command, r connectedrun.FlowResult) error {
	phases := make([]connectedrun.FlowPhaseResult, len(r.Phases))
	copy(phases, r.Phases)
	for i := range phases {
		phases[i].Wire = nil
	}
	if r.Schema == connectedrun.FlowSchemaV4 {
		// A FHIR lifecycle summary states each declared observation boundary,
		// so its verdict is never read as more than that boundary shows.
		if err := writeConnected(cmd, struct {
			Schema        string                         `json:"schema"`
			State         string                         `json:"state"`
			Verdict       assertion.Verdict              `json:"verdict"`
			Boundary      string                         `json:"boundary"`
			Setup         string                         `json:"setup"`
			Cleanup       string                         `json:"cleanup"`
			Engine        string                         `json:"engine"`
			Qualification []connectedrun.FlowClaim       `json:"qualification"`
			Phases        []connectedrun.FlowPhaseResult `json:"phases"`
		}{"readmit-connected-summary/v2", r.State, r.Verdict, r.Boundary, r.Setup, r.Cleanup, r.Engine, r.Qualification, phases}); err != nil {
			return err
		}
		return connectedFlowExit(r)
	}
	if err := writeConnected(cmd, struct {
		Schema   string                         `json:"schema"`
		State    string                         `json:"state"`
		Verdict  assertion.Verdict              `json:"verdict"`
		Boundary string                         `json:"boundary"`
		Setup    string                         `json:"setup"`
		Cleanup  string                         `json:"cleanup"`
		Engine   string                         `json:"engine"`
		Phases   []connectedrun.FlowPhaseResult `json:"phases"`
	}{"readmit-connected-summary/v1", r.State, r.Verdict, r.Boundary, r.Setup, r.Cleanup, r.Engine, phases}); err != nil {
		return err
	}
	return connectedFlowExit(r)
}
func connectedFlowExit(r connectedrun.FlowResult) error {
	if r.State != "complete" || r.Verdict == assertion.VerdictUndecided {
		return verdict(2, errors.New("connected lifecycle is unresolved; retained evidence is private"))
	}
	if r.Verdict == assertion.VerdictFail {
		return verdict(1, errors.New("connected lifecycle assertions failed"))
	}
	return nil
}
