package sendpolicy

import (
	"errors"
	"os"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
)

// WriteDecision retains a decision in a new, owner-only file outside evidence.
// Callers must stop before sending if recording fails.
func WriteDecision(path string, decision Decision) error {
	return WriteDecisionWithDurability(path, decision, artifactdir.Durable)
}

// WriteDecisionWithDurability is WriteDecision with the durability its caller
// chose: Scratch only for a decision beside evidence in a throwaway workspace
// its owner removes before it answers. It is recorded before any send either
// way.
func WriteDecisionWithDurability(path string, decision Decision, durability artifactdir.Durability) error {
	data, err := EncodeDecision(decision)
	if err != nil {
		return err
	}
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create the policy decision file")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = durability.Sync(file)
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("cannot write the policy decision")
	}
	return nil
}
