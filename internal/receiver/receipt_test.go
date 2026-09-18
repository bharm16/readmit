package receiver_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
)

func TestACKReceiptBindsActualSessionAndReceivedOccurrence(t *testing.T) {
	for _, code := range []string{"AA", "AR"} {
		t.Run(code, func(t *testing.T) {
			h := start(t, observation.Fixed, 1, 1<<20, time.Second)
			initial, err := observation.Read(h.config.ObservationPath)
			if err != nil {
				t.Fatal(err)
			}
			conn := h.connect(t)
			reader, _ := mllp.NewReader(conn, 1<<20)
			name, control := "listen-s12.hl7", "LISTEN-BOOK"
			if code == "AR" {
				name, control = "listen-s13.hl7", "LISTEN-MOVE"
			}
			send(t, conn, mllp.Frame(fixture(t, name)))
			raw := ack(t, reader, code, control)
			doc, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range []string{"readmit-receipt/v1", initial.SessionID, "s0001-e000001"} {
				selector, _ := hl7.ParseSelector(fmt.Sprintf("ZRT-%d", i+1))
				value, err := doc.Select(0, selector)
				if err != nil || value.State != hl7.Present || string(doc.Bytes(value.Span)) != want {
					t.Fatal("ACK receipt did not identify committed receiver evidence")
				}
			}
			h.await(t)
			if h.err != nil || h.result.Observation.Processed[0].OccurrenceID != "s0001-e000001" {
				t.Fatal("receipt did not match immutable recorded case")
			}
		})
	}
}
