package transform

import (
	"encoding/json/v2"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/profilepack"
)

var (
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	entryPattern  = regexp.MustCompile(`^t[0-9]{6}$`) // entryNameFormat, as a plan writes it
	// ruleToken is the rule identifier the correlation rules reader already
	// accepted. A plan names a rule of that document; it declares none of its
	// own.
	ruleToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+/-]{0,127}$`)
)

// DecodePlan reads a plan. Unknown members and unknown versions are errors;
// there is no migration and no repair.
func DecodePlan(data []byte) (Plan, error) {
	if len(data) > MaxPlanBytes {
		return Plan{}, errors.New("transformation plan exceeds its size limit")
	}
	// The declared contract version is read before the strict decode, so a plan
	// written under a later version is reported as one this release does not
	// read rather than as an invalid document.
	var declared struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &declared) != nil {
		return Plan{}, errors.New("invalid transformation plan")
	}
	if declared.Schema != PlanSchema {
		return Plan{}, errors.New("transformation plan declares a contract version this release does not read")
	}
	var present struct {
		Case  *string `json:"case"`
		Rules *string `json:"rules"`
		Steps *[]Step `json:"steps"`
	}
	if json.Unmarshal(data, &present) != nil || present.Case == nil || present.Rules == nil || present.Steps == nil {
		return Plan{}, errors.New("a transformation plan declares the case and the correlation rules it was authored against, and its steps")
	}
	var plan Plan
	if json.Unmarshal(data, &plan, json.RejectUnknownMembers(true)) != nil {
		return Plan{}, errors.New("invalid transformation plan")
	}
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	if len(plan.Steps) > MaxSteps {
		return Plan{}, errors.New("a transformation plan holds at most 256 steps")
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
		return errors.New("transformation plan declares a contract version this release does not read")
	}
	if !digestPattern.MatchString(plan.Case) {
		return errors.New("a plan names the verified identity of the case it was authored against")
	}
	if !digestPattern.MatchString(plan.Rules) {
		return errors.New("a plan names the SHA-256 of the correlation rules whose relations it preserves")
	}
	return validateProfile(plan.Profile)
}

// validateProfile accepts the zero identity, which pins no pack, or one whose
// id and version are both bounded printable text.
func validateProfile(pin profilepack.Identity) error {
	if pin == (profilepack.Identity{}) {
		return nil
	}
	if !packToken(pin.ID) || !packToken(pin.Version) {
		return errors.New("a pinned profile pack declares a printable id and version of 1 to 128 bytes")
	}
	return nil
}

func packToken(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

// validateStep accepts one well-formed operator. Every member another operator
// uses must be absent: a step that carries one means something the plan cannot
// state, so it is refused rather than applied with the extra member ignored.
func validateStep(step Step) (Step, error) {
	invalid := errors.New("a transformation step carries only the members its operator declares")
	switch step.Operator {
	case DropOccurrence, DuplicateOccurrence:
		if !entryPattern.MatchString(step.Entry) || step.Position != 0 || step.Rule != "" || step.Shift != "" {
			return Step{}, invalid
		}
	case ReorderOccurrence:
		if !entryPattern.MatchString(step.Entry) || step.Rule != "" || step.Shift != "" {
			return Step{}, invalid
		}
		if step.Position < 1 || step.Position > MaxEntries {
			return Step{}, errors.New("a reorder names a one-based position of the sequence")
		}
	case RebaseIdentifiers:
		if step.Entry != "" || step.Position != 0 || step.Shift != "" {
			return Step{}, invalid
		}
		if !ruleToken.MatchString(step.Rule) {
			return Step{}, errors.New("a rename names one rule of the declared correlation rules")
		}
	case ShiftDates:
		if step.Entry != "" || step.Position != 0 || step.Rule != "" {
			return Step{}, invalid
		}
		if _, err := ParseShift(step.Shift); err != nil {
			return Step{}, err
		}
	default:
		return Step{}, errors.New("this release does not perform that transformation operator")
	}
	return step, nil
}

// ParseShift reads the one duration a date shift is held to, and it is the one
// `readmit replay --shift` accepts, because both ask [hl7.ParseShift]: nonzero
// whole seconds within ten 365-day years, so every occurrence moves by the
// same explicit amount and the intervals between them are unchanged.
func ParseShift(shift string) (time.Duration, error) {
	parsed, err := hl7.ParseShift(shift)
	if err != nil {
		return 0, errors.New("a date shift is a nonzero whole-second duration within ten years, for example 24h or -2h")
	}
	return parsed, nil
}
