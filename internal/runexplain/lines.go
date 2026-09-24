package runexplain

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
)

// The plain-language reading of one explanation. `readmit explain` writes it
// to the console and the application's run-explanation panel draws it, so
// both entry points say what an assertion read, required and found in the
// same words, and neither hides or shows a value the other would not.

// hidden is what stands in for one customer-local value nobody asked to see.
// Expected and observed values are both message content — an expectation is
// the value an interface should have produced — so both are hidden unless
// values were asked for.
const hidden = "hidden"

// Instant renders one recorded time the way every other retained record
// spells one.
func Instant(at time.Time) string { return at.UTC().Format(time.RFC3339) }

// ReadsLine states what one assertion addresses.
func (d Detail) ReadsLine() string {
	subject := d.Subject
	switch {
	case subject.Field != nil:
		return fieldRefLine(*subject.Field)
	case subject.Pair != nil:
		return fieldRefLine(subject.Pair.Left) + " and " + fieldRefLine(subject.Pair.Right)
	case subject.Collection != nil:
		return "the records of the " + string(subject.Collection.Scope) + " observation"
	case subject.Each != nil:
		return string(subject.Each.Quantifier) + " record of the " + string(subject.Each.Scope) + " observation"
	case subject.Transition != nil:
		return "the transition from the " + string(subject.Transition.From) + " observation to the " + string(subject.Transition.To) + " observation"
	}
	return "nothing this release can address"
}

// ConditionLine states the field one assertion's condition reads and the
// value it must equal, and is empty for an assertion that has no condition.
func (d Detail) ConditionLine(showValues bool) string {
	if d.When == nil {
		return ""
	}
	return fieldRefLine(d.When.Field) + " equals " + valueLine(d.When.Equals, showValues)
}

func fieldRefLine(ref assertion.FieldRef) string {
	return ref.Selector + " of " + string(ref.Scope) + " message " + ref.Message
}

// valueLine renders one four-state value. Only a present value carries text,
// so only a present value can hide any.
func valueLine(value assertion.FieldValue, showValues bool) string {
	if value.Text == nil {
		return string(value.State)
	}
	if !showValues {
		return string(value.State) + ", text " + hidden
	}
	return string(value.State) + ", text " + strconv.QuoteToASCII(*value.Text)
}

// ExpectedLine states what one assertion required, in the operator's own
// terms. Counts are aggregates and are always shown; every value drawn from
// message content is hidden by default, including one an author wrote down.
func (d Detail) ExpectedLine(showValues bool) string {
	expected := d.Expected
	switch {
	case expected.Field != nil && d.Operator == assertion.FieldNotEquals:
		return "a value differing from " + valueLine(*expected.Field, showValues)
	case expected.Field != nil:
		return valueLine(*expected.Field, showValues)
	case expected.State != nil:
		return "state " + string(*expected.State)
	case expected.Pattern != nil:
		if !showValues {
			return "present text matching one bounded declared pattern (" + hidden + ")"
		}
		return "present text matching " + strconv.QuoteToASCII(*expected.Pattern)
	case expected.Range != nil:
		if !showValues {
			return "a present decimal inside a declared range (" + hidden + ")"
		}
		return "a present decimal inside " + strconv.QuoteToASCII(expected.Range.Min) + " to " + strconv.QuoteToASCII(expected.Range.Max) + ", inclusively"
	case expected.Tolerance != nil:
		if !showValues {
			return "a present decimal within a declared tolerance of a declared value (" + hidden + ")"
		}
		return "a present decimal within " + strconv.QuoteToASCII(expected.Tolerance.Tolerance) + " of " + strconv.QuoteToASCII(expected.Tolerance.Value)
	case expected.Window != nil:
		if !showValues {
			return "a present HL7 timestamp inside a declared window (" + hidden + ")"
		}
		return "a present HL7 timestamp inside " + strconv.QuoteToASCII(expected.Window.From) + " to " + strconv.QuoteToASCII(expected.Window.To) + ", inclusively"
	case expected.Count != nil:
		return fmt.Sprintf("exactly %d records", *expected.Count)
	case expected.Keys != nil:
		return declaredKeys(*expected.Keys, showValues)
	case expected.Multiplicity != nil:
		if !showValues {
			return fmt.Sprintf("one declared key (%s) producing exactly %d records", hidden, expected.Multiplicity.Count)
		}
		return fmt.Sprintf("the key %s producing exactly %d records", strconv.QuoteToASCII(expected.Multiplicity.Key), expected.Multiplicity.Count)
	case expected.Change != nil:
		return fmt.Sprintf("exactly %d distinct keys added and %d removed", expected.Change.Added, expected.Change.Removed)
	case expected.Holds != nil:
		return holdsLine(d.Operator, *expected.Holds)
	}
	return "nothing this release can express"
}

// holdsLine spells out what a boolean expectation means for the one operator
// that carries it, so a reader never has to remember which way true points.
func holdsLine(operator assertion.Operator, holds bool) string {
	positive, negative := "the condition holds", "the condition does not hold"
	switch operator {
	case assertion.ValuesEqual:
		positive, negative = "the two values are equal", "the two values differ"
	case assertion.RecordsUnique:
		positive, negative = "every observed key is distinct", "some observed key repeats"
	case assertion.RecordsAbsent:
		positive, negative = "the observation held no record at all", "the observation held at least one record"
	}
	if holds {
		return positive
	}
	return negative
}

// ObservedLine states what deciding one assertion actually read. A skipped
// assertion read nothing beyond its own condition, and saying so is the point:
// a skipped assertion is not a pass.
func (d Detail) ObservedLine(showValues bool) string {
	reading := d.Observed
	switch {
	case d.Outcome == assertion.OutcomeSkipped:
		return "not read; the condition did not hold, so this assertion asserted nothing"
	case d.Outcome == "":
		return "not evaluated"
	case reading.Field != nil && reading.Compared != nil:
		return valueLine(*reading.Field, showValues) + " and " + valueLine(*reading.Compared, showValues)
	case reading.Field != nil:
		return valueLine(*reading.Field, showValues)
	case reading.Added != nil && reading.Removed != nil:
		return fmt.Sprintf("%d distinct keys added and %d removed", *reading.Added, *reading.Removed)
	case reading.Records != nil && reading.Matched != nil:
		return fmt.Sprintf("%d records held, %d matched", *reading.Records, *reading.Matched)
	case reading.Records != nil:
		return fmt.Sprintf("%d records held", *reading.Records)
	}
	return "nothing the evidence could decide"
}

// Line names where one value can be read again. A field link names the exact
// payload file where the run retained one; a link the run holds no occurrence
// for says so, which is what makes an unknown_message actionable.
func (l Link) Line() string {
	if l.Selector == "" {
		artifact := l.Artifact
		if artifact == "" {
			artifact = "none"
		}
		return "the " + l.Scope + " observation's records, in " + artifact
	}
	if l.Payload == "" {
		return l.Scope + " " + l.Selector + " of " + l.Message + ": this run retained no readable payload for that occurrence"
	}
	return l.Scope + " " + l.Selector + " of " + l.Message + ": " + l.Artifact + "/" + l.Payload
}

// InputPayload describes the payload an input assertion would read. A send
// fewer bytes of which reached the peer than the run intended is named as the
// truncation it was: a field those bytes never reached is not a field the
// message omitted, so a partial send is never evidence.
func (m Message) InputPayload() string {
	if m.SentPartial && m.Sent != "" {
		return m.Sent + " (retained; a partial send, so it is not evidence of any value)"
	}
	return payloadState(m.Sent, m.SentReadable)
}

// ObservedPayload describes the payload an observed assertion would read.
func (m Message) ObservedPayload() string { return payloadState(m.Received, m.ReceivedReadable) }

// payloadState names one retained payload and whether it could be read as a
// message. A payload that was retained and did not parse is reported as that
// rather than left out, because an assertion naming it is an execution error
// and this is the line that explains why.
func payloadState(path string, readable bool) string {
	if path == "" {
		return "no payload retained"
	}
	if !readable {
		return path + " (retained; not readable as one message)"
	}
	return path
}

// CorrelationLine states what binds this observation to this run. The correlations a
// collection recorded are the only thing in a completion that names a run at
// all, and every one of them has been checked against what this run actually
// produced. A record carrying none says so rather than implying a link nobody
// recorded.
func (o ObservationContext) CorrelationLine() string {
	if len(o.Correlations) == 0 {
		return "none recorded, so nothing in this record binds the observation to this run"
	}
	return fmt.Sprintf("%d recorded, %d matched, %d unmatched, %d ambiguous; every one was produced by this run",
		len(o.Correlations), o.Matched, o.Unmatched, o.Ambiguous)
}

// KeysLine states the records this observation settled on: how many, and
// which only when values were asked for, because a record key is patient
// data in the same way an indexed field value is.
func (o ObservationContext) KeysLine(showValues bool) string { return keyList(o.Keys, showValues) }

func keyList(keys []string, showValues bool) string {
	if len(keys) == 0 {
		return "none"
	}
	if !showValues {
		return fmt.Sprintf("%d, %s", len(keys), hidden)
	}
	quoted := make([]string, 0, len(keys))
	for _, key := range keys {
		quoted = append(quoted, strconv.QuoteToASCII(key))
	}
	return strings.Join(quoted, ", ")
}

// declaredKeys states how many record keys an expectation named, and which
// ones only when values were asked for. An expectation is the value an
// interface should have produced, so it is the same customer-local evidence
// the reading is.
func declaredKeys(keys []string, showValues bool) string {
	label := fmt.Sprintf("%d declared keys", len(keys))
	if len(keys) == 1 {
		label = "1 declared key"
	}
	if !showValues {
		return label + ", " + hidden
	}
	return label + ": " + keyList(keys, showValues)
}
