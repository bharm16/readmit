package receiver

import (
	"errors"
	"os"
	"time"

	"github.com/bharm16/readmit/internal/observation"
)

// NewWithObservationLimitForTest changes only the resource budget, before Serve
// starts. The external tests still exercise the real receiver and evidence readers.
// This constructor is absent from application builds.
func NewWithObservationLimitForTest(config Config, limit int) (*Receiver, error) {
	if limit <= 0 || limit > observation.MaxBytes {
		return nil, errors.New("test observation limit must fit the production bound")
	}
	r, err := New(config)
	if err != nil {
		return nil, err
	}
	r.observationLimit = limit
	return r, nil
}

// DelayLedgerSyncForTest makes each ledger flush of receivers created
// afterwards take delay longer, standing in for a device whose flushes stall.
// The returned function restores the real flush.
func DelayLedgerSyncForTest(delay time.Duration) (restore func()) {
	original := syncLedger
	syncLedger = func(file *os.File) error {
		time.Sleep(delay)
		return original(file)
	}
	return func() { syncLedger = original }
}
