package reduce

import (
	"encoding/json/v2"
	"errors"
	"regexp"
	"slices"

	"github.com/bharm16/readmit/internal/durablerun"
)

var (
	identityPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	occurrencePattern  = regexp.MustCompile(`^s[0-9]{4}-e[0-9]{6}$`)
	assertionIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
)

// DecodePlan reads a reduction plan. Unknown members and unknown versions are
// errors; there is no migration and no repair.
func DecodePlan(data []byte) (Plan, error) {
	if len(data) > MaxPlanBytes {
		return Plan{}, errors.New("reduction plan exceeds its size limit")
	}
	// The declared contract version is read before the strict decode, so a plan
	// written under a later version is reported as one this release does not
	// read rather than as an invalid document.
	var declared struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &declared) != nil {
		return Plan{}, errors.New("invalid reduction plan")
	}
	if declared.Schema != PlanSchema {
		return Plan{}, errors.New("reduction plan declares a contract version this release does not read")
	}
	var present struct {
		Case          *string    `json:"case"`
		Grouping      *string    `json:"grouping"`
		Signature     *Signature `json:"signature"`
		Trials        *int       `json:"trials"`
		Confirmations *int       `json:"confirmations"`
	}
	if json.Unmarshal(data, &present) != nil || present.Case == nil || present.Grouping == nil ||
		present.Signature == nil || present.Trials == nil || present.Confirmations == nil {
		return Plan{}, errors.New("a reduction plan declares the case it was authored against, its grouping, the failure signature it holds, its trial budget and how many times it confirms")
	}
	var plan Plan
	if json.Unmarshal(data, &plan, json.RejectUnknownMembers(true)) != nil {
		return Plan{}, errors.New("invalid reduction plan")
	}
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// UnmarshalJSON reads one signature exactly as written. Presence is checked
// first and the same bytes are then re-read rejecting unknown members, so an
// omitted member is refused as omitted rather than read as a zero value.
func (s *Signature) UnmarshalJSON(data []byte) error {
	var required struct {
		State      *durablerun.State `json:"state"`
		Assertions *[]string         `json:"assertions"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.State == nil || required.Assertions == nil {
		return errors.New("a failure signature declares the state a run stops in and the assertions that failed")
	}
	type signature Signature
	var decoded signature
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a failure signature declares no member beyond state and assertions")
	}
	*s = Signature(decoded)
	return nil
}

func validatePlan(plan Plan) error {
	if plan.Schema != PlanSchema {
		return errors.New("reduction plan declares a contract version this release does not read")
	}
	if !identityPattern.MatchString(plan.Case) {
		return errors.New("a plan names the verified identity of the case it was authored against")
	}
	switch plan.Grouping {
	case GroupPerOccurrence:
		if plan.Rules != "" {
			return errors.New("a plan that groups per occurrence relates nothing; naming correlation rules there would declare a grouping it does not perform")
		}
	case GroupByCorrelation:
		if !identityPattern.MatchString(plan.Rules) {
			return errors.New("grouping by correlation names the SHA-256 of the exact rules whose relations form the groups")
		}
	default:
		return errors.New("this release does not perform that reduction grouping")
	}
	if plan.Trials < 1 || plan.Trials > MaxTrials {
		return errors.New("a reduction spends between 1 and 512 trials")
	}
	if plan.Confirmations < 1 || plan.Confirmations > MaxConfirmations {
		return errors.New("a reduction confirms its oracle between 1 and 8 times")
	}
	return validateSignature(plan.Signature)
}

// validateSignature holds a signature to the one class of failure a reduction
// can be about. A run that timed out, errored, was cancelled or left a delivery
// uncertain establishes nothing about an expectation, so it cannot be declared
// as the failure to preserve; reducing towards one would shrink the sequence
// towards an unstable environment rather than towards a defect.
func validateSignature(signature Signature) error {
	if signature.State != durablerun.AssertionFailed {
		return errors.New("a reduction holds a chosen assertion failure; a pass, an execution error, a cancellation and a timeout are not equivalent reduced failures")
	}
	if len(signature.Assertions) == 0 || len(signature.Assertions) > maxSignatureAssertions {
		return errors.New("a failure signature names between 1 and 256 failed assertions")
	}
	seen := make(map[string]bool, len(signature.Assertions))
	for _, id := range signature.Assertions {
		if !assertionIDPattern.MatchString(id) || seen[id] {
			return errors.New("a failure signature names each assertion of the spec once, by the identifier the spec gave it")
		}
		seen[id] = true
	}
	return nil
}

// maxSignatureAssertions is the assertion bound one spec is already held to, so
// a signature cannot name more assertions than a spec can declare.
const maxSignatureAssertions = 256

// matches reports whether the assertions one run failed are the chosen failure.
// It is exact in both directions: a run that failed fewer than the signature
// names failed something else, and one that failed those and something else as
// well failed differently. Whether the run stopped as an assertion failure at
// all is settled by the caller, before there is an assertion set to compare.
func (s Signature) matches(observed []string) bool {
	if len(observed) != len(s.Assertions) {
		return false
	}
	declared := slices.Clone(s.Assertions)
	found := slices.Clone(observed)
	slices.Sort(declared)
	slices.Sort(found)
	return slices.Equal(declared, found)
}
