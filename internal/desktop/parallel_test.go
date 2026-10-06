package desktop_test

import "testing"

// These lifecycle tests own their projects, listeners, providers and app state.
// Bound the CPU-heavy verification work even when Go's default parallelism is
// high, so other tests' real acquisition deadlines retain headroom. Tests that
// change process globals or exercise tight timing boundaries stay serial.
var lifecycleTests = make(chan struct{}, 4)

func parallelLifecycleTest(t *testing.T) {
	t.Helper()
	t.Parallel()
	lifecycleTests <- struct{}{}
	// Registered before the fixture cleanups: release only after they finish.
	t.Cleanup(func() { <-lifecycleTests })
}
