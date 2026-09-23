package report

import (
	"context"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// ObserveSenderForTest makes each trial started afterwards send through the
// ordinary test runner, writing its result as scratch as a trial does,
// reporting to observer, which stands in for the sender's own evidence
// storage. The returned function restores the runner.
func ObserveSenderForTest(observer replay.Observer) (restore func()) {
	original := sendTrial
	sendTrial = func(ctx context.Context, specPath, output string) (*testrunner.Artifact, error) {
		plan, err := testrunner.PrepareWithDurability(specPath, artifactdir.Scratch)
		if err != nil {
			return nil, err
		}
		return testrunner.ExecuteObserved(ctx, plan, output, observer)
	}
	return func() { sendTrial = original }
}
