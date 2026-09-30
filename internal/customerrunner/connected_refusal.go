package customerrunner

import "errors"

// ConnectedRefusalMetadata contains only fixed diagnostic vocabulary. Detailed
// provider/server text never becomes an imported result or CI log field.
type ConnectedRefusalMetadata struct {
	Category string   `json:"category"`
	Required []string `json:"required"`
}

func (m ConnectedRefusalMetadata) valid() bool {
	categories := map[string]bool{"runner-not-configured": true, "prepared-capability-or-input-unavailable": true, "promotion-not-approved": true, "installed-authority-unavailable": true, "authority-scope-mismatch": true, "authority-operation-mismatch": true, "capability-or-admission-denied": true, "hub-or-provider-unavailable": true, "execution-refused": true}
	allowed := map[string]bool{"private-runner-config": true, "exact-promotion": true, "finite-authority": true, "dispatch-identity": true, "exact-test-revision": true, "profile-and-terminology-pins": true, "collector-driver": true, "selected-validator-worker": true, "exact-environment-promotion": true, "finite-connected-authority": true, "exact-promoted-input": true, "capability-agreement": true, "registered-environment": true, "exact-permitted-operations": true, "current-enrollment": true, "exact-engine-contract-pack-collector-smart-validator-agreement": true, "private-credential-provider": true, "verified-hub-tls": true, "inspect-private-execution-proof": true}
	if !categories[m.Category] || len(m.Required) < 1 || len(m.Required) > 8 {
		return false
	}
	seen := map[string]bool{}
	for _, requirement := range m.Required {
		if !allowed[requirement] || seen[requirement] {
			return false
		}
		seen[requirement] = true
	}
	return true
}

type connectedSetupRefusal struct {
	metadata ConnectedRefusalMetadata
	cause    error
}

func (e *connectedSetupRefusal) Error() string {
	return "connected runner refused: " + e.metadata.Category
}
func (e *connectedSetupRefusal) Unwrap() error { return e.cause }
func refuseConnected(category string, cause error, required ...string) error {
	return &connectedSetupRefusal{ConnectedRefusalMetadata{Category: category, Required: required}, cause}
}
func connectedRefusalMetadata(err error) ConnectedRefusalMetadata {
	var refusal *connectedSetupRefusal
	if errors.As(err, &refusal) {
		return refusal.metadata
	}
	return ConnectedRefusalMetadata{Category: "execution-refused", Required: []string{"inspect-private-execution-proof"}}
}
