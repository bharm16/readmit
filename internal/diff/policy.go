package diff

import (
	"encoding/json/v2"
	"errors"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/hl7"
)

// PolicySchema is the contract an authored normalization policy declares. It is
// a new document beside the existing ones: readmit-diff/v1 gains no member and
// changes no byte, exactly as ADR-0003 requires of a contract that shipped.
const PolicySchema = "readmit-normalization-policy/v1"

// A policy is bounded like every other typed configuration this build reads,
// and a rule count no larger than the exact-ignore limit the raw comparison
// already accepts.
const (
	MaxPolicyBytes = 256 << 10
	MaxRules       = 256
)

// The three typed operators. There is no fourth, no wildcard and no expression:
// a rule names an operator and its parameters, and Go decides what it means.
const (
	IgnoreOperator    = "ignore"
	TimestampOperator = "timestamp"
	NumericOperator   = "numeric"
)

// What one rule established about one compared selection.
//
// Retained and Undecided are told apart deliberately. Retained means the rule
// read both values and they are not equivalent under it. Undecided means the
// rule could not read them as the kind of value it compares, so it settled
// nothing — which is not a reason to hide the difference, and not agreement.
const (
	Suppressed  = "suppressed"
	Retained    = "retained"
	Undecided   = "undecided"
	Unaddressed = "unaddressed"
)

// Why a rule settled nothing. Each is a bounded word, never a value.
const (
	NotPresent          = "value_not_present"
	NotNumeric          = "value_not_numeric"
	NotTimestamp        = "value_not_timestamp"
	CoarserThanRule     = "value_precision_coarser_than_rule"
	OffsetNotIdentical  = "offset_not_identical"
	UnsupportedEvidence = "unsupported_evidence"
)

// The declared precisions, coarsest first. A rule compares exactly the leading
// components this names; it never invents the ones below it.
var precisions = []string{"year", "month", "day", "hour", "minute", "second"}

var ruleID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var decimal = regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]+)?|\.[0-9]+)$`)
var unsigned = regexp.MustCompile(`^([0-9]+(\.[0-9]+)?|\.[0-9]+)$`)

// Policy is a document a person authors and this build only ever reads. The
// selectors are stored exactly as they were written and matched on their
// canonical form; nothing here rewrites the document it was given.
type Policy struct {
	Schema string `json:"schema"`
	Rules  []Rule `json:"rules"`
}

// Rule scopes one typed operator to exactly one canonical selector, the same
// scope an exact ignore already has. Precision belongs only to a timestamp rule
// and Tolerance only to a numeric one; either member on the wrong operator is a
// refusal rather than a member that is quietly unused.
type Rule struct {
	ID        string `json:"id"`
	Selector  string `json:"selector"`
	Operator  string `json:"operator"`
	Precision string `json:"precision,omitzero"`
	Tolerance string `json:"tolerance,omitzero"`
}

// DecodePolicy rejects unknown members rather than warning about them, so a
// misspelled parameter cannot silently disable a rule a reader believes is
// scoping their comparison.
func DecodePolicy(data []byte) (Policy, error) {
	var policy Policy
	if len(data) > MaxPolicyBytes || json.Unmarshal(data, &policy, json.RejectUnknownMembers(true)) != nil {
		return Policy{}, errors.New("invalid normalization policy JSON")
	}
	return policy, policy.Validate()
}

// ReadPolicy opens one bounded local file. It follows no path recorded inside
// an artifact and opens no network connection.
func ReadPolicy(path string) (Policy, error) {
	data, err := readFile(path, MaxPolicyBytes)
	if err != nil {
		return Policy{}, errors.New("normalization policy must be a bounded regular file")
	}
	return DecodePolicy(data)
}

// Validate holds an authored document to the same rules the report describes,
// so a policy is refused where it is read rather than half-applied later.
func (p Policy) Validate() error {
	invalid := errors.New("invalid normalization policy contract")
	if p.Schema != PolicySchema || len(p.Rules) == 0 || len(p.Rules) > MaxRules {
		return invalid
	}
	ids, selectors := map[string]bool{}, map[string]bool{}
	for _, rule := range p.Rules {
		if !ruleID.MatchString(rule.ID) || ids[rule.ID] {
			return invalid
		}
		ids[rule.ID] = true
		selector, err := hl7.ParseSelector(rule.Selector)
		if err != nil {
			return err
		}
		// One rule per selection keeps the report answerable: every suppressed
		// difference names the single rule that suppressed it.
		if selectors[selector.String()] {
			return errors.New("normalization policy addresses one selector with more than one rule")
		}
		selectors[selector.String()] = true
		switch rule.Operator {
		case IgnoreOperator:
			if rule.Precision != "" || rule.Tolerance != "" {
				return invalid
			}
		case TimestampOperator:
			if rule.Tolerance != "" || precisionOf(rule.Precision) == 0 {
				return invalid
			}
		case NumericOperator:
			if rule.Precision != "" || len(rule.Tolerance) == 0 || len(rule.Tolerance) > 32 || !unsigned.MatchString(rule.Tolerance) {
				return errors.New("a numeric tolerance is an unsigned decimal distance")
			}
		default:
			return invalid
		}
	}
	return nil
}

func precisionOf(name string) int {
	for i, precision := range precisions {
		if precision == name {
			return i + 1
		}
	}
	return 0
}

// decide answers what one rule established about one difference. It receives
// the decoded bytes because deciding equivalence is the only thing anyone does
// with them here: the outcome and a bounded reason are all that leaves.
func (r Rule) decide(left, right Value, leftBytes, rightBytes []byte) (string, string) {
	// An undecodable field is not made comparable by naming it in a policy.
	for _, value := range []Value{left, right} {
		if value.Encoding == "unsupported_escape" || value.Encoding == "non_utf8" {
			return Undecided, UnsupportedEvidence
		}
	}
	if r.Operator == IgnoreOperator {
		return Suppressed, ""
	}
	// A rule that compares values has none to compare when a field is absent,
	// empty or an explicit null. Those four states stay four states.
	if left.State != hl7.Present || right.State != hl7.Present {
		return Undecided, NotPresent
	}
	if r.Operator == NumericOperator {
		return r.decideNumeric(string(leftBytes), string(rightBytes))
	}
	return r.decideTimestamp(string(leftBytes), string(rightBytes))
}

func (r Rule) decideNumeric(left, right string) (string, string) {
	l, lok := number(left)
	s, sok := number(right)
	if !lok || !sok {
		return Undecided, NotNumeric
	}
	tolerance, _ := number(r.Tolerance) // validated when the document was read
	difference := new(big.Rat).Sub(l, s)
	if difference.Abs(difference).Cmp(tolerance) <= 0 {
		return Suppressed, ""
	}
	return Retained, ""
}

func number(text string) (*big.Rat, bool) {
	if len(text) == 0 || len(text) > 64 || !decimal.MatchString(text) {
		return nil, false
	}
	value, ok := new(big.Rat).SetString(text)
	return value, ok
}

// decideTimestamp compares exactly the leading components the rule names.
//
// Two refusals keep it honest. A value that declares less precision than the
// rule compares is undecided rather than padded with zeros, because the
// components it omitted are unknown and not midnight. And two values whose
// declared UTC offsets are not identical are undecided rather than converted,
// because this operator does no zone arithmetic and will not present a
// converted instant as the value the message carried.
func (r Rule) decideTimestamp(left, right string) (string, string) {
	l, lok := instantOf(left)
	s, sok := instantOf(right)
	if !lok || !sok {
		return Undecided, NotTimestamp
	}
	if l.offset != s.offset {
		return Undecided, OffsetNotIdentical
	}
	width := precisionOf(r.Precision)
	if l.declared < width || s.declared < width {
		return Undecided, CoarserThanRule
	}
	if slices.Equal(l.parts[:width], s.parts[:width]) {
		return Suppressed, ""
	}
	return Retained, ""
}

// instant is one HL7 date/time as the digits declared it: the components it
// actually carried, how many of them there were, and the UTC offset it named.
type instant struct {
	parts    [6]int
	declared int
	offset   string
}

var timestampPattern = regexp.MustCompile(`^([0-9]{4}(?:[0-9]{2}){0,5})(\.[0-9]{1,4})?([+-][0-9]{4})?$`)

func instantOf(text string) (instant, bool) {
	if len(text) > 32 {
		return instant{}, false
	}
	parts := timestampPattern.FindStringSubmatch(text)
	if parts == nil {
		return instant{}, false
	}
	digits := parts[1]
	// A fraction is declared only where the seconds it refines are.
	if parts[2] != "" && len(digits) != 14 {
		return instant{}, false
	}
	value := instant{declared: len(digits)/2 - 1, offset: parts[3]}
	value.parts[1], value.parts[2] = 1, 1
	if !value.read(digits) {
		return instant{}, false
	}
	if parts[3] != "" {
		hours, _ := strconv.Atoi(parts[3][1:3])
		minutes, _ := strconv.Atoi(parts[3][3:5])
		if hours > 23 || minutes > 59 {
			return instant{}, false
		}
	}
	return value, true
}

// read fills the declared components and refuses a date the calendar does not
// have, so 20260231 is not a timestamp two rules could agree about.
func (i *instant) read(digits string) bool {
	year, err := strconv.Atoi(digits[:4])
	if err != nil || year < 1 {
		return false
	}
	i.parts[0] = year
	for component := 1; component < i.declared; component++ {
		n, err := strconv.Atoi(digits[2+component*2 : 4+component*2])
		if err != nil {
			return false
		}
		i.parts[component] = n
	}
	at := time.Date(i.parts[0], time.Month(i.parts[1]), i.parts[2], i.parts[3], i.parts[4], i.parts[5], 0, time.UTC)
	return at.Year() == i.parts[0] && int(at.Month()) == i.parts[1] && at.Day() == i.parts[2] &&
		at.Hour() == i.parts[3] && at.Minute() == i.parts[4] && at.Second() == i.parts[5]
}
