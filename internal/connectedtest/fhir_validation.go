package connectedtest

import (
	"context"

	"github.com/bharm16/readmit/internal/fhirvalidator"
)

// ReadFHIRValidationCheck maps exact retained validation evidence into the same
// Go check carrier used by connected execution. It is an independent versioned
// check and does not widen any historical assertion-set or execution-result.
func ReadFHIRValidationCheck(ctx context.Context, directory, expectedIdentity, expectedInput, checkID string) (fhirvalidator.Check, error) {
	evidence, err := fhirvalidator.Open(ctx, directory)
	if err != nil {
		return fhirvalidator.Check{}, err
	}
	if evidence.Identity() != expectedIdentity || evidence.Result().InputSHA256 != expectedInput {
		return fhirvalidator.Check{}, invalid
	}
	return evidence.Check(checkID)
}
