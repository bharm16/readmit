package reproducer

import (
	"encoding/json/v2"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/hl7"
)

var (
	occurrencePattern = regexp.MustCompile(`^s[0-9]{4}-e[0-9]{6}$`)
	identityPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// NewPlan starts an empty plan bound to one verified case identity. A plan
// names the evidence it was authored against, so replaying it somewhere else is
// refused rather than applied to whatever happens to be there.
func NewPlan(identity string) (Plan, error) {
	if !identityPattern.MatchString(identity) {
		return Plan{}, errors.New("a plan names the verified identity of the case it was authored against")
	}
	return Plan{Schema: PlanSchema, Case: identity, Steps: []Step{}}, nil
}

// Append adds one step to a plan. It checks only what a step means on its own:
// whether the evidence supports it is decided by Resolve, against the case.
//
// The selector a step addresses is recorded in its canonical form, with the
// segment occurrence and the field repetition written out, so the position a
// step edits is explicit rather than left to a default.
func Append(plan Plan, step Step) (Plan, error) {
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	if len(plan.Steps) >= MaxSteps {
		return Plan{}, errors.New("a plan holds at most 256 steps")
	}
	step, err := validateStep(step)
	if err != nil {
		return Plan{}, err
	}
	return Plan{Schema: plan.Schema, Case: plan.Case, Steps: append(slices.Clone(plan.Steps), step)}, nil
}

// Undo removes the last step of a plan. A plan is the whole of what an edit
// session is, so undoing one is dropping the step that was added last and
// resolving what remains; nothing about the removed step is retained.
func Undo(plan Plan) (Plan, error) {
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	if len(plan.Steps) == 0 {
		return Plan{}, errors.New("this plan has no step to undo")
	}
	return Plan{Schema: plan.Schema, Case: plan.Case, Steps: slices.Clone(plan.Steps[:len(plan.Steps)-1])}, nil
}

// DecodePlan reads a plan. Unknown members and unknown versions are errors;
// there is no migration and no repair.
func DecodePlan(data []byte) (Plan, error) {
	if len(data) > maxPlanBytes {
		return Plan{}, errors.New("reproducer plan exceeds its size limit")
	}
	// The declared contract version is read before the strict decode, so a plan
	// written under a later version is reported as one this release does not
	// read rather than as an invalid document.
	var declared struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &declared) != nil {
		return Plan{}, errors.New("invalid reproducer plan")
	}
	if declared.Schema != PlanSchema {
		return Plan{}, errors.New("reproducer plan declares a contract version this release does not read")
	}
	var present struct {
		Case  *string `json:"case"`
		Steps *[]Step `json:"steps"`
	}
	if json.Unmarshal(data, &present) != nil || present.Case == nil || present.Steps == nil {
		return Plan{}, errors.New("a reproducer plan declares the case it was authored against and its steps")
	}
	var plan Plan
	if json.Unmarshal(data, &plan, json.RejectUnknownMembers(true)) != nil {
		return Plan{}, errors.New("invalid reproducer plan")
	}
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	if len(plan.Steps) > MaxSteps {
		return Plan{}, errors.New("a plan holds at most 256 steps")
	}
	for i, step := range plan.Steps {
		validated, err := validateStep(step)
		if err != nil {
			return Plan{}, err
		}
		plan.Steps[i] = validated
	}
	return plan, nil
}

func validatePlan(plan Plan) error {
	if plan.Schema != PlanSchema {
		return errors.New("reproducer plan declares a contract version this release does not read")
	}
	if !identityPattern.MatchString(plan.Case) {
		return errors.New("a plan names the verified identity of the case it was authored against")
	}
	return nil
}

// validateStep accepts one well-formed operator and returns it with its
// selectors written out canonically. Every member another operator uses must be
// absent: a step that carries one means something the plan cannot state, so it
// is refused rather than applied with the extra member ignored.
func validateStep(step Step) (Step, error) {
	invalid := errors.New("a reproducer step carries only the members its operator declares")
	switch step.Operator {
	case SelectOccurrence, DropOccurrence:
		if !occurrencePattern.MatchString(step.Occurrence) || step.Selector != "" || len(step.Identity) != 0 || step.Value != "" {
			return Step{}, invalid
		}
	case IncludeAcknowledgements:
		if step.Occurrence != "" || step.Selector != "" || len(step.Identity) != 0 || step.Value != "" {
			return Step{}, invalid
		}
	case IncludePriorIdentity:
		if step.Occurrence != "" || step.Selector != "" || step.Value != "" {
			return Step{}, invalid
		}
		if len(step.Identity) == 0 || len(step.Identity) > MaxIdentityKeys {
			return Step{}, errors.New("a declared identity is between 1 and 8 field selectors")
		}
		identity := make([]string, 0, len(step.Identity))
		for _, declared := range step.Identity {
			selector, err := hl7.ParseSelector(declared)
			if err != nil {
				return Step{}, errors.New("a declared identity selector is not one this release addresses")
			}
			if slices.Contains(identity, selector.String()) {
				return Step{}, errors.New("a declared identity names each field once")
			}
			identity = append(identity, selector.String())
		}
		step.Identity = identity
	case SetField, ClearField:
		if !occurrencePattern.MatchString(step.Occurrence) || len(step.Identity) != 0 {
			return Step{}, invalid
		}
		selector, err := hl7.ParseSelector(step.Selector)
		if err != nil {
			return Step{}, errors.New("an edited field selector is not one this release addresses")
		}
		if step.Operator == ClearField && step.Value != "" {
			return Step{}, invalid
		}
		if step.Operator == SetField && !safeValue(step.Value) {
			return Step{}, errors.New("an edited value is 1 to 1024 bytes of printable text and cannot declare a delimiter or an explicit null")
		}
		step.Selector = selector.String()
	default:
		return Step{}, errors.New("this release does not perform that reproducer operator")
	}
	return step, nil
}

// safeValue is the rule a replacement is held to, and it is the one `redact`
// already holds its own replacements to: a scalar cannot introduce a field,
// component, repetition, escape or subcomponent delimiter, cannot introduce the
// two bytes that declare an explicit HL7 null, and cannot introduce a control
// byte. An edit changes one value; it can never restructure the message around
// it, so the syntax a reproducer produces is the syntax a person selected.
func safeValue(value string) bool {
	return value != "" && len(value) <= MaxValueBytes && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0 && !strings.ContainsAny(value, `|^~\&"`)
}
