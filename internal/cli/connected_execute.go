package cli

import (
	"errors"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/spf13/cobra"
)

func runConnectedTest(cmd *cobra.Command, plan, config, instance, output string, send bool) error {
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
		State   string            `json:"state"`
		Verdict assertion.Verdict `json:"verdict"`
		Phase   string            `json:"phase"`
	}{result.State, result.Verdict, result.Phase}); err != nil {
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
