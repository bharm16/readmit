package customerrunner

import (
	"context"

	"github.com/bharm16/readmit/internal/durablerun"
)

// RunWithForTest is Run with its lease claimed, renewed and released through
// hub and timed by clock, instead of the configured hub and the host's
// clock. It is absent from application builds.
func RunWithForTest(ctx context.Context, c Config, job Job, hub Hub, clock Clock) (durablerun.Summary, error) {
	return run(ctx, c, job, nil, hub, clock)
}

// RunPinnedWithForTest is RunPinned through hub and clock, as RunWithForTest
// is Run. It is absent from application builds.
func RunPinnedWithForTest(ctx context.Context, c Config, job Job, inputIdentity string, hub Hub, clock Clock) (durablerun.Summary, error) {
	return run(ctx, c, job, &inputIdentity, hub, clock)
}
