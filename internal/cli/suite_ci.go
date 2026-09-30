package cli

import (
	"encoding/json/v2"
	"errors"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/spf13/cobra"
)

func suiteCICommand() *cobra.Command {
	var request suite.CIRequest
	var send bool
	var deadline string
	var runnerConfig, authority, instance string
	command := &cobra.Command{Use: "ci FILE", Short: "Execute a saved suite once with private evidence and fixed-label CI summaries", Annotations: mergeAnnotations(declareInterruptible(capabilityExecute), map[string]string{ciSummaryAnnotation: "true"}), RunE: func(cmd *cobra.Command, args []string) error {
		result := suite.CIError()
		if len(args) == 1 && send {
			request.Path = args[0]
			ctx, cancel, err := deadlineContext(cmd.Context(), deadline)
			if err == nil {
				defer cancel()
				var header struct {
					Schema string `json:"schema"`
				}
				raw, readErr := (artifactdir.Document{MaxBytes: suite.MaxBytes}).Read(request.Path)
				if readErr == nil && json.Unmarshal(raw, &header) == nil && header.Schema == suite.ConnectedSchema {
					if len(request.Previous) == 0 && request.Releases == "" && runnerConfig != "" && authority != "" && instance != "" {
						config, e := customerrunner.ReadConfig(runnerConfig)
						if e == nil {
							result = customerrunner.RunConnectedCI(ctx, config, suite.ConnectedRequest{Path: request.Path, Environment: request.Environment, Output: request.Output, Promotion: request.Promotion, PromotionIdentity: request.PromotionIdentity, Revision: request.Revision, Instance: instance}, authority, request.Requirements)
						}
					}
				} else if runnerConfig == "" && authority == "" && instance == "" {
					result = suite.RunCI(ctx, request)
				}
			}
		}
		if err := writeJSON(cmd, result); err != nil {
			return refusal(errors.New("cannot write CI summary"))
		}
		if result.ExitCode != 0 {
			return verdict(result.ExitCode, errors.New("suite CI gate did not pass; inspect retained evidence privately"))
		}
		return nil
	}}
	command.Flags().StringVar(&request.Environment, "environment", "", "Explicit suite environment")
	command.Flags().StringVar(&request.Output, "output", "", "New customer-private evidence directory")
	command.Flags().BoolVar(&send, "send", false, "Authorize one execution; never resume or retry")
	command.Flags().StringVar(&deadline, "deadline", "", "Positive execution deadline")
	command.Flags().StringVar(&request.Requirements, "requirements", "", "Explicit coverage declarations; all requirements and jobs must qualify")
	command.Flags().StringArrayVar(&request.Previous, "previous", nil, "Prior suite for coverage stability assessment; requires --requirements")
	command.Flags().StringVar(&request.Releases, "releases", "", "Exact released expectation references")
	command.Flags().StringVar(&request.Promotion, "promotion", "", "Explicit approved environment promotion")
	command.Flags().StringVar(&request.PromotionIdentity, "promotion-identity", "", "Exact full promotion identity")
	command.Flags().StringVar(&request.Revision, "revision", "", "Current operator-asserted target revision")
	command.Flags().StringVar(&runnerConfig, "runner-config", "", "Installed customer-controlled runner configuration for a connected suite")
	command.Flags().StringVar(&authority, "authority", "", "Installed finite connected runner authority, separate from GUI consent")
	command.Flags().StringVar(&instance, "instance", "", "Customer-selected dispatch identity, stable across retries and unique for a new authorized occurrence")
	return command
}
