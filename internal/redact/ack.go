package redact

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/testrunner"
)

// Successful fixture ACKs have one closed shape. Their request control/trigger
// come from approved evidence; only the valid UTC timestamp and already bound
// receipt session vary. Matching two retained copies cannot approve extra text.
func verifyFixtureACKs(source *bundle.Bundle, artifact *testrunner.Artifact) error {
	invalid := errors.New("proof ACK is not the canonical built-in fixture acknowledgement")
	if artifact.Run == nil || artifact.FinalObservation == nil || len(artifact.Run.Events) == 0 || len(artifact.Run.Events) != len(artifact.FinalObservation.Processed) {
		return invalid
	}
	for i, event := range artifact.Run.Events {
		approved, err := source.Raw(event.SourceOccurrence)
		if err != nil {
			return invalid
		}
		request, err := hl7.Parse(approved, hl7.Options{})
		if err != nil || len(request.Messages) != 1 {
			return invalid
		}
		family, familyOK := selectedText(request, "MSH-9.1")
		version, versionOK := selectedText(request, "MSH-12.1")
		if !familyOK || family != "SIU" || !versionOK || version != "2.5.1" {
			return invalid
		}
		trigger, ok := selectedText(request, "MSH-9.2")
		if !ok || !slices.Contains([]string{"S12", "S13"}, trigger) {
			return invalid
		}
		control := request.Messages[0].Segments[0].Field(10)
		controlID := string(request.Bytes(control.Span))
		receivedID := fmt.Sprintf("s0001-e%06d", 2*i+1)
		processed := artifact.FinalObservation.Processed[i]
		if control.State != hl7.Present || processed.ControlID != controlID || processed.OccurrenceID != receivedID {
			return invalid
		}
		raw, err := artifact.Run.Raw(event.Received)
		if err != nil {
			return invalid
		}
		ack, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
		if err != nil || len(ack.Messages) != 1 {
			return invalid
		}
		stamp := string(ack.Bytes(ack.Messages[0].Segments[0].Field(7).Span))
		parsed, err := time.Parse("20060102150405-0700", stamp)
		if err != nil || parsed.UTC().Format("20060102150405-0700") != stamp {
			return invalid
		}
		want := mllp.Frame(hl7.Encode([][]string{
			{"MSH", "^~\\&", "READMIT", "FIXTURE", "", "", stamp, "", "ACK^" + trigger, fmt.Sprintf("READMITACK%06d", i+1), "P", "2.5.1"},
			{"MSA", "AA", controlID},
			{"ZRT", testrunner.ReceiptSchema, artifact.Result.ReceiverSessionID, receivedID},
		}))
		if !bytes.Equal(raw, want) {
			return invalid
		}
	}
	return nil
}
