package redact

import (
	"context"

	"github.com/bharm16/readmit/internal/testrunner"
)

// Prover is the prove step's seam, the way reduction's Oracle is its own: the
// one thing Create trusts a fixture trial to do, named so the wiring is
// explicit and tests stand in a fake rather than swap a package variable.
// The production adapter is [FixtureProver], which runs genuinely fresh
// built-in fixture sessions through the fixture-trial module.
type Prover interface {
	// Prove runs spec against the case at casePath — the original evidence
	// Create reviews — and writes the two fresh fixture sessions it needs
	// under dir. required names the assertion positions that must fail on the
	// defective fixture and pass on the fixed one. An error reports which
	// step failed, naming no path and no evidence value.
	Prove(ctx context.Context, spec testrunner.Spec, casePath, dir string, required []int) (Proof, error)
}

// FixtureProver proves a case against fresh built-in fixture sessions. It is
// Create's default prover, and the adapter Export re-proves the derived case
// through.
type FixtureProver struct{}

// Prove implements Prover with the fixture-trial lifecycle.
func (FixtureProver) Prove(ctx context.Context, spec testrunner.Spec, casePath, dir string, required []int) (Proof, error) {
	return runProof(ctx, spec, casePath, dir, required)
}
