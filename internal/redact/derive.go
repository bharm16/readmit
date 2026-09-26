package redact

import (
	"slices"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// deriveInputs carries the verified inputs one derive step turns into derived
// evidence. Every member is already read and verified by the prepare step;
// derive itself opens no file and no socket, so it is callable — and
// fuzzable — without either.
type deriveInputs struct {
	// Case is the verified case bundle derive transforms.
	Case *bundle.Bundle
	// CasePath is where the verified case lives. Derive never reads it; the
	// seal step hands it to the original proof.
	CasePath string
	// Spec is the verified regression spec the derivation must preserve.
	Spec testrunner.Spec
	// Policy is the decoded disclosure policy.
	Policy Policy
	// Inventory is the decoded original-artifact inventory.
	Inventory Inventory
	// Artifacts holds each inventoried artifact's verified facts, in
	// inventory order: the in-memory runs, results and reports the findings
	// locate, with the private linkage pinning where their verified bytes
	// came from.
	Artifacts []artifactFact
	// Sources is the private source linkage of the case, spec, policy and
	// inventory documents themselves.
	Sources []sourceReference
}

// artifactFact is one inventoried original artifact after verification: the
// private linkage that pins its verified bytes, and the in-memory facts
// derive locates findings about. Exactly the members a kind needs are set.
type artifactFact struct {
	reference sourceReference
	run       *replay.Run
	result    *testrunner.Artifact
	report    *diagnose.Report
}

// derivation is what one derive step established.
type derivation struct {
	findingLog
	// Occurrences are the derived source bytes of the derived case, one per
	// source of the verified case, each carrying its occurrences' directions.
	Occurrences []bundle.Input
	// Spec is the derived spec whose expected literals bind to the derived
	// occurrences.
	Spec testrunner.Spec
	// Local is the private mapping state: the source linkage, the surrogate
	// mappings, the per-patient date shifts and the known residual values.
	Local localState
}

// derive is the pure step of Create: verified case, spec, policy and
// inventory facts in; derived occurrences, derived spec, located findings and
// the private mapping state out. It reads no file and opens no socket.
// Reviewing the inventory facts first keeps the findings in the order they
// are located: the original artifacts, then each occurrence, then the spec.
func derive(in deriveInputs) (derivation, error) {
	t := &transformer{
		policy: in.Policy,
		findingLog: findingLog{
			Findings: []exportreview.Finding{},
			Policies: map[string]bool{},
		},
		local: localState{
			Schema:         PrivateSchema,
			Sources:        slices.Clone(in.Sources),
			Mappings:       []mapping{},
			Shifts:         []shift{},
			ResidualValues: [][]byte{},
		},
		original: map[string]*hl7.Document{},
		derived:  map[string]*hl7.Document{},
	}
	if err := t.reviewFacts(in.Inventory, in.Artifacts); err != nil {
		return derivation{}, err
	}
	occurrences, err := t.transformCase(in.Case)
	if err != nil {
		return derivation{}, err
	}
	spec, err := t.transformSpec(in.Spec)
	if err != nil {
		return derivation{}, err
	}
	return derivation{findingLog: t.findingLog, Occurrences: occurrences, Spec: spec, Local: t.local}, nil
}
