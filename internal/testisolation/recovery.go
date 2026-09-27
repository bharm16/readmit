package testisolation

import (
	"context"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
)

func recoveryPreparation(p *Prepared, reconciled string) (*Prepared, Result, map[string][]byte, error) {
	result, identity, files, err := readSnapshot(reconciled)
	if err != nil || result.Plan != p.identity || result.Setup != "reconciled-not-ready" || result.Cleanup != "not-applicable" || result.Reconciliation != "" {
		return nil, Result{}, nil, refused
	}
	next := *p
	next.recovery = identity
	return &next, result, files, nil
}

// RecoveryReview binds cleanup authority to a new read-only reconciliation,
// including its actual resource versions. It cannot revive a setup review.
func RecoveryReview(p *Prepared, reconciled, phase string) (Review, error) {
	if phase != "read" && phase != "cleanup" {
		return Review{}, refused
	}
	next, result, _, err := recoveryPreparation(p, reconciled)
	if err != nil {
		return Review{}, err
	}
	review := next.Review(phase)
	review.Identity = review.Binding.Configuration
	review.Manual = nil
	if phase == "cleanup" {
		review.Effects = []Effect{}
		for i := len(result.Resources) - 1; i >= 0; i-- {
			r := result.Resources[i]
			review.Effects = append(review.Effects, Effect{Alias: r.Alias, Kind: r.Kind, ID: r.ID, Operation: "delete-owned-version-" + r.Version, Expected: clone(r.Attributes)})
		}
	}
	return review, nil
}

// CleanupReconciled is a new explicitly authorized operation, never a retry of
// an old write. It deletes only the exact owned versions independently observed
// by Reconcile, and never provisions or restores manual setup confirmation.
func CleanupReconciled(ctx context.Context, p *Prepared, a Authorities, reconciled, output string) (Result, error) {
	next, result, files, err := recoveryPreparation(p, reconciled)
	if err != nil {
		return Result{}, err
	}
	for _, phase := range []string{"read", "cleanup"} {
		if _, err := next.Check(ctx, a, phase); err != nil {
			return Result{}, err
		}
	}
	s, err := newSession(next, a, output)
	if err != nil {
		return Result{}, err
	}
	defer s.Close()
	// Nested proof preserves the reconciliation without retaining a mutable path.
	delete(files, "identity.sha256")
	if identity, err := artifactdir.Write(ctx, filepath.Join(output, "recovery"), family, artifactdir.Durable, files); err != nil || identity != next.recovery {
		return Result{}, refused
	}
	s.result.Setup = "recovered-no-setup"
	s.result.Reconciliation = next.recovery
	s.result.Resources = clone(result.Resources)
	s.result.Lease = result.Lease
	for _, phase := range []string{"read", "cleanup"} {
		if err := s.pinActor(ctx, phase); err != nil {
			return Result{}, err
		}
	}
	err = s.Cleanup(ctx)
	return s.Result(), err
}
