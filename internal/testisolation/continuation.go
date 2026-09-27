package testisolation

// ContinuationReview reads an exact, independently verified ready checkpoint.
// It grants no authority, contacts no target, and never revives setup consent.
// A caller must obtain a new read or cleanup grant for this returned binding.
func ContinuationReview(p *Prepared, previous, phase string) (Review, error) {
	if phase != "read" && phase != "cleanup" {
		return Review{}, refused
	}
	result, identity, _, err := readyContinuationSnapshot(p, previous)
	if err != nil {
		return Review{}, err
	}
	next := *p
	next.recovery = identity
	review := next.Review(phase)
	review.Identity = review.Binding.Configuration
	review.Effects = []Effect{}
	for i := range result.Resources {
		at := i
		if phase == "cleanup" {
			at = len(result.Resources) - 1 - i
		}
		resource := result.Resources[at]
		operation := "verify-unchanged-version-" + resource.Version
		if phase == "cleanup" {
			if resource.Owner != p.document.Scope.Owner {
				continue
			}
			operation = "delete-owned-version-" + resource.Version
		}
		review.Effects = append(review.Effects, Effect{Alias: resource.Alias, Kind: resource.Kind, ID: resource.ID, Operation: operation, Expected: clone(resource.Attributes)})
	}
	return review, nil
}
