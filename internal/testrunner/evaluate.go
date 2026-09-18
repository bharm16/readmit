package testrunner

import (
	"bytes"
	"errors"
	"reflect"
	"regexp"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
)

var sessionID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func pending(spec Spec) []AssertionResult {
	assertions := make([]AssertionResult, len(spec.Assertions))
	for i, assertion := range spec.Assertions {
		assertions[i] = AssertionResult{Assertion: assertion, Status: "not_evaluated"}
	}
	return assertions
}

func initialState(snapshot *observation.Snapshot) bool {
	return snapshot != nil && snapshot.Consistent && len(snapshot.Processed) == 0 && len(snapshot.Records) == 0
}

// evaluate operates only on retained evidence. Every required observation is
// validated before any assertion can pass; transport uncertainty is never data.
func evaluate(spec Spec, run *replay.Run, initial, final *observation.Snapshot) (Status, string, []AssertionResult) {
	assertions := pending(spec)
	fail := func(class string) (Status, string, []AssertionResult) { return ExecutionError, class, pending(spec) }
	if spec.Observation.Boundary == LedgerBoundary && !initialState(initial) {
		return fail("initial_observation")
	}
	if run == nil {
		return fail("replay_execution")
	}
	if len(run.Events) != len(spec.Input.Messages) {
		return fail("run_selection")
	}
	selected := make(map[string]bool)
	for _, id := range spec.Input.Messages {
		selected[id] = true
	}
	acks := make(map[string]*hl7.Document)
	var processed []observation.Occurrence
	seen := make(map[string]bool)
	for _, event := range run.Events {
		if !selected[event.SourceOccurrence] || acks[event.SourceOccurrence] != nil {
			return fail("run_selection")
		}
		if event.Outcome != replay.Accepted && event.Outcome != replay.ApplicationError && event.Outcome != replay.Rejected {
			return fail("transport")
		}
		if event.ACK.Correlation != "matched" || event.Delivery != "acknowledged" {
			return fail("transport")
		}
		raw, err := run.Raw(event.Received)
		if err != nil {
			return fail("ack_observation")
		}
		doc, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
		if err != nil || len(doc.Messages) != 1 {
			return fail("ack_observation")
		}
		acks[event.SourceOccurrence] = doc
		if spec.Observation.Boundary == LedgerBoundary {
			receiptSession, receivedID, err := receipt(doc)
			if err != nil || receiptSession != initial.SessionID || seen[receivedID] {
				return fail("receipt")
			}
			seen[receivedID] = true
			sent, err := run.Raw(event.Sent)
			if err != nil {
				return fail("receipt")
			}
			request, err := hl7.Parse(sent, hl7.Options{Format: hl7.MLLP})
			if err != nil || len(request.Messages) != 1 {
				return fail("receipt")
			}
			// The ledger stores the literal MSH-10; replay separately verifies
			// decoded MSA-2 correlation. These representations are not conflated.
			control := request.Messages[0].Segments[0].Field(10)
			value := request.Bytes(control.Span)
			if control.State != hl7.Present || !utf8.Valid(value) {
				return fail("receipt")
			}
			processed = append(processed, observation.Occurrence{OccurrenceID: receivedID, ControlID: string(value)})
		}
	}
	if spec.Observation.Boundary == LedgerBoundary {
		if final == nil || !final.Consistent || final.SessionID != initial.SessionID || final.Mode != initial.Mode || !reflect.DeepEqual(final.Processed, processed) {
			return fail("final_observation")
		}
	}
	status := Pass
	encodedBytes := 256 << 10 // reserve bounded result metadata and JSON overhead
	for i, assertion := range spec.Assertions {
		var observed Value
		switch assertion.Operator {
		case "ledger_count":
			count := len(final.Records)
			observed.Count = &count
		case "ledger_equals":
			records := final.Records
			observed.Records = &records
		case "ack_field_equals":
			doc := acks[assertion.Message]
			selector, _ := hl7.ParseSelector(assertion.Selector)
			value, err := doc.Select(0, selector)
			if err != nil {
				return fail("ack_observation")
			}
			field := &FieldValue{State: value.State}
			if value.State == hl7.Present {
				decoded, err := hl7.Decode(doc.Bytes(value.Span), doc.Messages[0].Delimiters)
				if err != nil || !utf8.Valid(decoded) {
					return fail("ack_observation")
				}
				text := string(decoded)
				field.Text = &text
			}
			observed.Field = field
		}
		assertionJSON, err := encode(assertion)
		if err != nil {
			return fail("assertion_evidence_limit")
		}
		observedJSON, err := encode(observed)
		if err != nil {
			return fail("assertion_evidence_limit")
		}
		encodedBytes += len(assertionJSON) + len(observedJSON) + 256
		if encodedBytes > maxResultBytes {
			return fail("assertion_evidence_limit")
		}
		assertions[i].Observed = &observed
		assertions[i].Status = "passed"
		if !reflect.DeepEqual(assertion.Expected, observed) {
			assertions[i].Status = "failed"
			status = AssertionFailure
		}
	}
	return status, "", assertions
}

func receipt(doc *hl7.Document) (string, string, error) {
	invalid := errors.New("invalid fixture ACK receipt")
	count := 0
	var receipt *hl7.Segment
	for i := range doc.Messages[0].Segments {
		segment := &doc.Messages[0].Segments[i]
		if segment.ID == "ZRT" {
			count++
			receipt = segment
		}
	}
	if count != 1 || len(receipt.Fields) != 3 {
		return "", "", invalid
	}
	values := make([]string, 3)
	for i := range values {
		field := receipt.Field(i + 1)
		if field.State != hl7.Present {
			return "", "", invalid
		}
		values[i] = string(doc.Bytes(field.Span))
	}
	if values[0] != ReceiptSchema || !sessionID.MatchString(values[1]) || !occurrenceID.MatchString(values[2]) {
		return "", "", invalid
	}
	return values[1], values[2], nil
}

func sameJSON(a, b any) bool {
	left, err := encode(a)
	if err != nil {
		return false
	}
	right, err := encode(b)
	return err == nil && bytes.Equal(left, right)
}
