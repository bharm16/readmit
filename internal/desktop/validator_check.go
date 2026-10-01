package desktop

import (
	"context"
	"errors"

	"github.com/bharm16/readmit/internal/fhirvalidator"
)

// validatorOperation is the name a validator check, installation or removal
// runs under: the privacy status says the local container engine's command
// line is running while it asks, loads or removes the worker image.
const validatorOperation = "validator"

// ValidatorState is what a validator check, installation or removal found.
type ValidatorState string

// The validator states, the same the command line and a validation report.
const (
	ValidatorReady                 ValidatorState = fhirvalidator.StateReady
	ValidatorNotConfigured         ValidatorState = fhirvalidator.StateNotConfigured
	ValidatorCapabilityUnavailable ValidatorState = fhirvalidator.StateCapabilityUnavailable
	ValidatorCapabilityExists      ValidatorState = fhirvalidator.StateCapabilityExists
	ValidatorUntrustedPackage      ValidatorState = fhirvalidator.StateUntrustedPackage
	ValidatorPackageInvalid        ValidatorState = fhirvalidator.StatePackageInvalid
	ValidatorPackageUnavailable    ValidatorState = fhirvalidator.StatePackageUnavailable
	ValidatorUnsupportedRuntime    ValidatorState = fhirvalidator.StateUnsupportedRuntime
	ValidatorWorkerMissing         ValidatorState = fhirvalidator.StateWorkerMissing
	ValidatorWorkerUnavailable     ValidatorState = fhirvalidator.StateWorkerUnavailable
)

// ValidatorCheck is what the local validator check found for one saved FHIR
// connection: the state the validation itself would meet, what to do about
// it, and the pinned versions the selected capability names.
type ValidatorCheck struct {
	State       ValidatorState `json:"state"`
	Requirement string         `json:"requirement,omitzero"`
	Validator   string         `json:"validator,omitzero"`
	Runtime     string         `json:"runtime,omitzero"`
	Packages    []string       `json:"packages"`
}

// ValidatorCheckResult answers one validator check.
type ValidatorCheckResult struct {
	State   State           `json:"state"`
	Reason  string          `json:"reason,omitzero"`
	Context RequestContext  `json:"context"`
	Check   *ValidatorCheck `json:"check,omitzero"`
	// Saved is the environment revision an installation or removal saved.
	Saved *ItemRef `json:"saved,omitzero"`
}

func (r *ValidatorCheckResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// CheckValidator checks the optional local validator a saved FHIR connection
// selects, on the person's explicit action: the capability's pins offline,
// then whether the selected local container engine is the qualified platform
// and holds the exact worker image — the check the deployment command and a
// validation make. It downloads nothing, resolves no credential, contacts no
// FHIR server and saves nothing; opening Help, Settings or the environment
// never runs it.
func (a *App) CheckValidator(request ItemRequest) ValidatorCheckResult {
	return runNamed[ValidatorCheckResult, *ValidatorCheckResult](a, profiles["CheckValidator"], func(ctx context.Context) ValidatorCheckResult {
		result := ValidatorCheckResult{Context: request.Context}
		loaded, _, members, declined := a.savedEnvironment(ctx, request)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if members.fhir == nil {
			result.refuse(Failed, "only a FHIR connection selects a local validator")
			return result
		}
		result.State, result.Check = Completed, checkValidator(ctx, members.fhir.Validation)
		return result
	})
}

func checkValidator(ctx context.Context, validation *ConnectionValidation) *ValidatorCheck {
	if validation == nil || validation.Capability == "" || validation.Engine == "none" {
		return checkFailure(fhirvalidator.Status{State: fhirvalidator.StateNotConfigured, Requirement: "choose the installed validator folder in Edit connection"})
	}
	capability, status := fhirvalidator.CheckInstalled(ctx, validation.Capability, validation.Socket)
	check := checkFailure(status)
	if capability != nil {
		m := capability.Manifest()
		check.Validator, check.Runtime = m.Validator.Version, m.Runtime.Version
		for _, p := range m.Packages {
			check.Packages = append(check.Packages, p.ID+"#"+p.Version)
		}
	}
	return check
}

// checkFailure is the check answering a state: a refusal's, or the state an
// installed validator was found in.
func checkFailure(err error) *ValidatorCheck {
	var status fhirvalidator.Status
	if !errors.As(err, &status) {
		status = fhirvalidator.Status{State: fhirvalidator.StateCapabilityUnavailable, Requirement: "install the validator package, then choose its folder in Edit connection"}
	}
	return &ValidatorCheck{State: ValidatorState(status.State), Requirement: status.Requirement, Packages: []string{}}
}
