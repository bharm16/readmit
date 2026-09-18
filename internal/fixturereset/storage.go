package fixturereset

import (
	"errors"
	"os"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// ReserveOutcome checks that a reset outcome may be written where the operator
// asked for it, before any action runs. A reset opens a connection, and a
// destination readmit was never going to be able to use should cost nobody one.
// Reserving a name here never authorizes overwriting it later: WriteOutcome
// still creates it exclusively.
func ReserveOutcome(path string) error {
	_, err := artifactpath.Destination(path)
	return err
}

// WriteOutcome retains one reset outcome in a new, owner-only file outside
// evidence. A reset is either recorded as what it established or it is not
// recorded at all: this never replaces an existing outcome, so rerunning a
// reset after performing the manual step needs a new destination.
func WriteOutcome(path string, result Result) error {
	data, err := EncodeOutcome(result)
	if err != nil {
		return err
	}
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create the reset outcome file; the destination must be new")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(destination)
		return errors.New("cannot write the reset outcome")
	}
	return nil
}
