package guide

import (
	"context"
	"os"

	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// ObserveSenderForTest makes each practice run started afterwards send through
// the ordinary test runner reporting to observer, which stands in for the
// sender's own evidence storage. The returned function restores the runner.
func ObserveSenderForTest(observer replay.Observer) (restore func()) {
	original := sendPractice
	sendPractice = func(ctx context.Context, specPath, output string) (*testrunner.Artifact, error) {
		plan, err := testrunner.Prepare(specPath)
		if err != nil {
			return nil, err
		}
		return testrunner.ExecuteObserved(ctx, plan, output, observer)
	}
	return func() { sendPractice = original }
}

// ObserveLedgerSyncForTest calls observe with the path of the ledger each
// practice run started afterwards keeps, just before it flushes it. An error
// from observe stands in for a failed flush. The returned function restores the
// real flush.
func ObserveLedgerSyncForTest(observe func(path string) error) (restore func()) {
	original := syncLedger
	syncLedger = func(file *os.File) error {
		if err := observe(file.Name()); err != nil {
			return err
		}
		return original(file)
	}
	return func() { syncLedger = original }
}
