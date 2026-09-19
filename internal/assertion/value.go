package assertion

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"math/big"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/hl7"
)

// written reads an object's members by name, before nullable pointers can
// erase an explicitly supplied null or an irrelevant member. A member named
// here but written as JSON null is returned as that null rather than as an
// omission, so a null can never read as a member that was left out, and a
// member this object does not name is refused.
//
// Only the three readers that must tell a null apart from an omission use it:
// the two closed unions, whose members must be neither, and the assertion,
// whose condition is the one member a null is a declaration for. Every other
// reader below takes the ordinary presence-then-strict shape, a struct of
// required pointers followed by the same bytes decoded strictly.
func written(data []byte, names ...string) (map[string]jsontext.Value, error) {
	var members map[string]jsontext.Value
	if err := json.Unmarshal(data, &members); err != nil {
		return nil, errors.New("invalid object")
	}
	for name := range members {
		if !slices.Contains(names, name) {
			return nil, errors.New("unknown member")
		}
	}
	return members, nil
}

func isNull(raw jsontext.Value) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

// onlyMember reports whether exactly one of the named members was written and
// it was not null, which is what a closed typed union requires.
func onlyMember(data []byte, names ...string) bool {
	members, err := written(data, names...)
	if err != nil || len(members) != 1 {
		return false
	}
	for _, raw := range members {
		if isNull(raw) {
			return false
		}
	}
	return true
}

// UnmarshalJSON reads the subject union: exactly one member, never null.
func (s *Subject) UnmarshalJSON(data []byte) error {
	invalid := errors.New("a subject names exactly one of field, pair, collection, each or transition")
	if !onlyMember(data, "field", "pair", "collection", "each", "transition") {
		return invalid
	}
	type subject Subject
	var decoded subject
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return invalid
	}
	*s = Subject(decoded)
	return nil
}

// kind names the one member a decoded subject carries.
func (s Subject) kind() (subjectKind, error) {
	switch {
	case s.Field != nil:
		return subjectField, nil
	case s.Pair != nil:
		return subjectPair, nil
	case s.Collection != nil:
		return subjectCollection, nil
	case s.Each != nil:
		return subjectEach, nil
	case s.Transition != nil:
		return subjectTransition, nil
	}
	return "", errors.New("a subject names exactly one of field, pair, collection, each or transition")
}

// UnmarshalJSON reads the expectation union: exactly one member, never null.
func (e *Expected) UnmarshalJSON(data []byte) error {
	invalid := errors.New("an expectation carries exactly one typed member")
	if !onlyMember(data, "field", "state", "pattern", "range", "tolerance", "window", "holds", "count", "keys", "multiplicity", "change") {
		return invalid
	}
	type expected Expected
	var decoded expected
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return invalid
	}
	*e = Expected(decoded)
	return nil
}

// member names the one member a decoded expectation carries.
func (e Expected) member() (expectedMember, error) {
	switch {
	case e.Field != nil:
		return expectField, nil
	case e.State != nil:
		return expectState, nil
	case e.Pattern != nil:
		return expectPattern, nil
	case e.Range != nil:
		return expectRange, nil
	case e.Tolerance != nil:
		return expectTolerance, nil
	case e.Window != nil:
		return expectWindow, nil
	case e.Holds != nil:
		return expectHolds, nil
	case e.Count != nil:
		return expectCount, nil
	case e.Keys != nil:
		return expectKeys, nil
	case e.Multiplicity != nil:
		return expectMultiplicity, nil
	case e.Change != nil:
		return expectChange, nil
	}
	return "", errors.New("an expectation carries exactly one typed member")
}

// UnmarshalJSON reads one assertion exactly as written. Every member is
// required, and when is written as an explicit null when an assertion is
// unconditional, so an omitted condition is never read as one that was
// declared and left empty.
func (a *Assertion) UnmarshalJSON(data []byte) error {
	invalid := errors.New("an assertion requires id, operator, subject, when and expected")
	members, err := written(data, "id", "operator", "subject", "when", "expected")
	if err != nil || len(members) != 5 {
		return invalid
	}
	for name, raw := range members {
		if isNull(raw) && name != "when" {
			return errors.New("only an assertion's when may be written as null")
		}
	}
	type assertion Assertion
	var decoded assertion
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return invalid
	}
	*a = Assertion(decoded)
	return nil
}

// UnmarshalJSON reads one field reference exactly as written and parses its
// selector through the shared grammar, so a path outside that grammar is
// refused when the set is read rather than when it is evaluated.
func (f *FieldRef) UnmarshalJSON(data []byte) error {
	invalid := errors.New("a field reference requires scope, message and selector")
	var required struct {
		Scope    *MessageScope `json:"scope"`
		Message  *string       `json:"message"`
		Selector *string       `json:"selector"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Scope == nil || required.Message == nil || required.Selector == nil {
		return invalid
	}
	type reference FieldRef
	var decoded reference
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a field reference declares no member beyond scope, message and selector")
	}
	selector, err := hl7.ParseSelector(decoded.Selector)
	if err != nil {
		return errors.New("a field reference carries one selector of the shared grammar")
	}
	decoded.selector = selector
	*f = FieldRef(decoded)
	return nil
}

// UnmarshalJSON reads one pair exactly as written.
func (p *PairRef) UnmarshalJSON(data []byte) error {
	invalid := errors.New("a pair requires left and right field references")
	var required struct {
		Left  *FieldRef `json:"left"`
		Right *FieldRef `json:"right"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Left == nil || required.Right == nil {
		return invalid
	}
	type pair PairRef
	var decoded pair
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a pair declares no member beyond left and right")
	}
	*p = PairRef(decoded)
	return nil
}

// UnmarshalJSON reads one collection reference exactly as written.
func (r *RecordRef) UnmarshalJSON(data []byte) error {
	var required struct {
		Scope *RecordScope `json:"scope"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Scope == nil {
		return errors.New("a collection reference requires scope")
	}
	type reference RecordRef
	var decoded reference
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a collection reference declares no member beyond scope")
	}
	*r = RecordRef(decoded)
	return nil
}

// UnmarshalJSON reads one quantified reference exactly as written.
func (e *EachRef) UnmarshalJSON(data []byte) error {
	var required struct {
		Scope      *RecordScope `json:"scope"`
		Quantifier *Quantifier  `json:"quantifier"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Scope == nil || required.Quantifier == nil {
		return errors.New("a quantified reference requires scope and quantifier")
	}
	type reference EachRef
	var decoded reference
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a quantified reference declares no member beyond scope and quantifier")
	}
	*e = EachRef(decoded)
	return nil
}

// UnmarshalJSON reads one transition exactly as written.
func (t *TransitionRef) UnmarshalJSON(data []byte) error {
	var required struct {
		From *RecordScope `json:"from"`
		To   *RecordScope `json:"to"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.From == nil || required.To == nil {
		return errors.New("a transition requires from and to")
	}
	type transition TransitionRef
	var decoded transition
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a transition declares no member beyond from and to")
	}
	*t = TransitionRef(decoded)
	return nil
}

// UnmarshalJSON reads one condition exactly as written.
func (c *Condition) UnmarshalJSON(data []byte) error {
	var required struct {
		Field  *FieldRef   `json:"field"`
		Equals *FieldValue `json:"equals"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Field == nil || required.Equals == nil {
		return errors.New("a condition requires field and equals")
	}
	type condition Condition
	var decoded condition
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a condition declares no member beyond field and equals")
	}
	*c = Condition(decoded)
	return nil
}

// UnmarshalJSON keeps JSON null separate from the supported HL7 null state.
// Only a present value carries text; every other state omits that member.
func (v *FieldValue) UnmarshalJSON(data []byte) error {
	invalid := errors.New("a value requires a supported state and text only when present")
	members, err := written(data, "state", "text")
	if err != nil {
		return invalid
	}
	var state hl7.State
	if raw, ok := members["state"]; !ok || isNull(raw) || json.Unmarshal(raw, &state) != nil {
		return invalid
	}
	decoded := FieldValue{State: state}
	text, carried := members["text"]
	switch state {
	case hl7.Present:
		if !carried || isNull(text) {
			return invalid
		}
		var value string
		if err := json.Unmarshal(text, &value); err != nil {
			return invalid
		}
		decoded.Text = &value
	case hl7.Empty, hl7.Null, hl7.Omitted:
		if carried {
			return invalid
		}
	default:
		return invalid
	}
	*v = decoded
	return nil
}

// UnmarshalJSON reads one range exactly as written.
func (r *Range) UnmarshalJSON(data []byte) error {
	var required struct {
		Min *string `json:"min"`
		Max *string `json:"max"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Min == nil || required.Max == nil {
		return errors.New("a range requires min and max")
	}
	type bounds Range
	var decoded bounds
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a range declares no member beyond min and max")
	}
	*r = Range(decoded)
	return nil
}

// UnmarshalJSON reads one tolerance exactly as written.
func (t *Tolerance) UnmarshalJSON(data []byte) error {
	var required struct {
		Value     *string `json:"value"`
		Tolerance *string `json:"tolerance"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Value == nil || required.Tolerance == nil {
		return errors.New("a tolerance requires value and tolerance")
	}
	type tolerance Tolerance
	var decoded tolerance
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a tolerance declares no member beyond value and tolerance")
	}
	*t = Tolerance(decoded)
	return nil
}

// UnmarshalJSON reads one window exactly as written.
func (w *Window) UnmarshalJSON(data []byte) error {
	var required struct {
		From *string `json:"from"`
		To   *string `json:"to"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.From == nil || required.To == nil {
		return errors.New("a window requires from and to")
	}
	type window Window
	var decoded window
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a window declares no member beyond from and to")
	}
	*w = Window(decoded)
	return nil
}

// UnmarshalJSON reads one multiplicity exactly as written.
func (m *Multiplicity) UnmarshalJSON(data []byte) error {
	var required struct {
		Key   *string `json:"key"`
		Count *int    `json:"count"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Key == nil || required.Count == nil {
		return errors.New("a multiplicity requires key and count")
	}
	type multiplicity Multiplicity
	var decoded multiplicity
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a multiplicity declares no member beyond key and count")
	}
	*m = Multiplicity(decoded)
	return nil
}

// UnmarshalJSON reads one change exactly as written.
func (c *Change) UnmarshalJSON(data []byte) error {
	var required struct {
		Added   *int `json:"added"`
		Removed *int `json:"removed"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Added == nil || required.Removed == nil {
		return errors.New("a change requires added and removed")
	}
	type change Change
	var decoded change
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a change declares no member beyond added and removed")
	}
	*c = Change(decoded)
	return nil
}

// number reads one bounded decimal literal exactly. Values are compared as
// exact rationals rather than as floating-point approximations, because a
// regression expectation that drifts by a rounding step is not a regression
// expectation. Exponent notation is refused: it is a second spelling of the
// same value and this contract has one.
func number(literal string) (*big.Rat, bool) {
	if len(literal) > maxNumberBytes || !decimal.MatchString(literal) {
		return nil, false
	}
	value, ok := new(big.Rat).SetString(literal)
	return value, ok
}

// instant reads one HL7 timestamp of whole seconds with an explicit numeric
// offset. A value carrying no offset names no instant: no zone is assumed for
// it, here or anywhere else in readmit. What is compared is the instant rather
// than its spelling, so two offsets naming the same moment are the same
// moment; an impossible date or time is refused by the parser itself.
func instant(value string) (time.Time, bool) {
	if len(value) != len(timestampLayout) {
		return time.Time{}, false
	}
	parsed, err := time.Parse(timestampLayout, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
