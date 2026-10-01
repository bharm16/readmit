package desktop_test

import (
	"context"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testisolation"
)

func expectationReview(plan string) (expectation.ConnectedReview, error) {
	return expectation.ReviewConnected(plan)
}

type runqueueReport struct {
	code     int
	verdicts map[string]assertion.Verdict
}

// suiteRunConnected runs a saved connected suite as the customer runner
// does, provisioning each binding's grant as its operator would.
func suiteRunConnected(t *testing.T, path, environment, output, instance string, provision func(string, networkaction.Binding)) (runqueueReport, error) {
	t.Helper()
	report, err := suite.RunConnected(t.Context(), suite.ConnectedRequest{Path: path, Environment: environment, Output: output, Instance: instance, Execute: func(ctx context.Context, p *connectedrun.PreparedFlow, destination string) (connectedrun.FlowResult, error) {
		for name, binding := range p.Bindings() {
			provision(name, binding)
		}
		return connectedrun.ExecuteFlow(ctx, p, destination, testisolation.Confirmation{})
	}})
	out := runqueueReport{code: report.ExitCode(), verdicts: map[string]assertion.Verdict{}}
	for _, job := range report.Jobs {
		if job.Flow != nil {
			out.verdicts[job.ID] = job.Flow.Verdict
		}
	}
	return out, err
}
