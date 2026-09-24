package hub

import (
	"context"
	"net/http"
	"time"
)

// RunnerHandlerWithClockForTest is RunnerHandler reading clock instead of the
// host's clock, so a test ends the ten-second hold by advancing clock rather
// than by waiting. The hold's length is unchanged. This constructor is absent
// from application builds.
func (s *Store) RunnerHandlerWithClockForTest(access *Access, policyPath string, clock func() time.Time) http.Handler {
	return s.runnerHandler(access, policyPath, clock, clock().Add(runnerHold))
}

// ServeSchedulesWithClockForTest is ServeSchedules with its runner handler and
// scheduler waiting out the hold on clock. It is absent from application builds.
func (s *Store) ServeSchedulesWithClockForTest(ctx context.Context, access *Access, runnerPath, policyPath string, clock func() time.Time) error {
	return s.serveSchedules(ctx, access, runnerPath, policyPath, clock)
}

// ScheduledRunForTest is the profile the scheduler runs each scheduled job
// under, so a test ticks a scheduler exactly as ServeSchedules does.
var ScheduledRunForTest = scheduledRun
