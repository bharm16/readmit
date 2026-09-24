package sendpolicy

import (
	"errors"

	"github.com/bharm16/readmit/internal/artifactdir"
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
	document := decisionFile
	document.Durability = durability
	return document.Create(path, data)
}

// decisionFile is how a policy decision is created, through the shared
// document store. A decision whose write failed is retained incomplete.
var decisionFile = artifactdir.Document{
	RetainFailed: true,
	Errors: artifactdir.DocumentErrors{
		Create: errors.New("cannot create the policy decision file"),
		Write:  errors.New("cannot write the policy decision"),
	},
}
