package durablerun

import (
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/replay"
)

// acknowledgedEvent is the recorded outcome of o000001: the intended bytes the
// plan pinned, evidence named the way the protocol names it, delivered and
// acknowledged.
func acknowledgedEvent() replay.Event {
	at := eventPayloads("o000001")
	return replay.Event{
		OutboundOccurrence: "o000001",
		Source:             at.source,
		Intended:           at.intended,
		Sent:               at.sent,
		Received:           at.received,
		Delivery:           "acknowledged",
	}
}

func recordedEntry(event replay.Event) *entry {
	return &entry{Kind: recordRecorded, Occurrence: event.OutboundOccurrence, Event: &event}
}

func eventPayloads(id string) struct {
	source   bundle.Payload
	intended bundle.Payload
	sent     bundle.Payload
	received bundle.Payload
} {
	return struct {
		source   bundle.Payload
		intended bundle.Payload
		sent     bundle.Payload
		received bundle.Payload
	}{
		source:   bundle.Payload{Path: evidencePath(id, "source"), Size: 3, SHA256: "source-digest"},
		intended: bundle.Payload{Path: evidencePath(id, "intended"), Size: 3, SHA256: "intended-digest"},
		sent:     bundle.Payload{Path: evidencePath(id, "sent"), Size: 5, SHA256: "sent-digest"},
		received: bundle.Payload{Path: evidencePath(id, "received"), Size: 7, SHA256: "received-digest"},
	}
}

// One planned occurrence, and the plan identities a recorded event is checked
// against.
func testProtocol() *protocol {
	return newProtocol(1, []payload{{"intended/o000001.bin", 3, "intended-digest"}}, []string{"source-digest"})
}

func readyRunning(p *protocol) {
	if err := p.apply(&entry{Kind: recordReady}); err != nil {
		panic(err)
	}
	if err := p.apply(&entry{Kind: recordRunning}); err != nil {
		panic(err)
	}
}

func intentEntry() *entry { return &entry{Kind: recordIntent, Occurrence: "o000001"} }
func sentEntry() *entry {
	return &entry{Kind: recordSent, Occurrence: "o000001", Sent: &payload{sentPath("o000001"), 5, "sent-digest"}}
}
func finishedEntry() *entry {
	return &entry{Kind: recordFinished, Final: &Summary{Schema: Schema, State: Passed, StopReason: Passed, Planned: 1, Recorded: 1}}
}

// The protocol is the one statement of what a journal means. A change to any
// rule below is a change to what recovery reports, and must be made here.
func TestJournalProtocolAcceptsOneCompleteRun(t *testing.T) {
	p := testProtocol()
	readyRunning(p)
	for _, e := range []*entry{intentEntry(), sentEntry(), recordedEntry(acknowledgedEvent())} {
		if err := p.apply(e); err != nil {
			t.Fatalf("%s record refused: %v", e.Kind, err)
		}
	}
	final, err := p.finish(finishedEntry())
	if err != nil {
		t.Fatalf("finished record refused: %v", err)
	}
	p.commit(final)
	if !p.finished || p.summary.State != Passed || p.summary.Recorded != 1 || p.summary.DeliveryUncertain {
		t.Fatalf("re-derivation disagree: %+v", p.summary)
	}
	if p.occurrences[0].Delivery != Acknowledged {
		t.Fatalf("acknowledged occurrence misread: %+v", p.occurrences[0])
	}
}

// An intent whose journal ends without a resolved outcome stays uncertain —
// including when a finished record is absent and when the journal stops there.
func TestJournalProtocolLeavesAnUnresolvedIntentUncertain(t *testing.T) {
	p := testProtocol()
	readyRunning(p)
	if err := p.apply(intentEntry()); err != nil {
		t.Fatal(err)
	}
	if !p.summary.DeliveryUncertain || p.occurrences[0].Delivery != Uncertain {
		t.Fatalf("an intent did not make the delivery uncertain: %+v", p.summary)
	}
	// A finished record that claims certainty is refused: the intent above it
	// was never resolved.
	states := finishedEntry()
	states.Final.DeliveryUncertain = false
	states.Final.Recorded = 0
	states.Final.State = Passed
	states.Final.StopReason = Passed
	if _, err := p.finish(states); err == nil {
		t.Fatal("a certain finish was accepted over an unresolved intent")
	}
	// The agreeing form names the uncertainty and the uncertain state.
	states.Final.DeliveryUncertain = true
	states.Final.State = DeliveryUncertain
	states.Final.StopReason = Passed
	if _, err := p.finish(states); err != nil {
		t.Fatalf("the agreeing finish was refused: %v", err)
	}
}

// The sequence rules: nothing before ready, nothing twice, nothing after a
// committed finish, and a sent record never without its intent.
func TestJournalProtocolRefusesRecordsOutOfOrder(t *testing.T) {
	refused := func(name string, steps func(p *protocol) error) {
		t.Helper()
		p := testProtocol()
		if err := steps(p); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	refused("intent before ready", func(p *protocol) error { return p.apply(intentEntry()) })
	refused("running before ready", func(p *protocol) error { return p.apply(&entry{Kind: recordRunning}) })
	refused("a second ready", func(p *protocol) error {
		readyRunning(p)
		return p.apply(&entry{Kind: recordReady})
	})
	refused("a second intent for one occurrence", func(p *protocol) error {
		readyRunning(p)
		if err := p.apply(intentEntry()); err != nil {
			return err
		}
		return p.apply(intentEntry())
	})
	refused("sent without intent", func(p *protocol) error {
		readyRunning(p)
		return p.apply(sentEntry())
	})
	refused("a second sent for one intent", func(p *protocol) error {
		readyRunning(p)
		if err := p.apply(intentEntry()); err != nil {
			return err
		}
		if err := p.apply(sentEntry()); err != nil {
			return err
		}
		return p.apply(sentEntry())
	})
	refused("a record after the finish", func(p *protocol) error {
		readyRunning(p)
		for _, e := range []*entry{intentEntry(), sentEntry(), recordedEntry(acknowledgedEvent())} {
			if err := p.apply(e); err != nil {
				return err
			}
		}
		final, err := p.finish(finishedEntry())
		if err != nil {
			return err
		}
		p.commit(final)
		return p.apply(&entry{Kind: recordIntent, Occurrence: "o000001"})
	})
	refused("an unknown record kind", func(p *protocol) error {
		readyRunning(p)
		return p.apply(&entry{Kind: "replayed"})
	})
}

// A recorded event is checked against the plan: same occurrence, same intended
// bytes, and the evidence payload names the protocol uses.
func TestJournalProtocolChecksARecordedEventAgainstThePlan(t *testing.T) {
	p := testProtocol()
	readyRunning(p)
	if err := p.apply(intentEntry()); err != nil {
		t.Fatal(err)
	}
	event := acknowledgedEvent()
	event.OutboundOccurrence = "o000002"
	if _, err := p.recorded(&entry{Kind: recordRecorded, Occurrence: "o000002", Event: &event}); err == nil {
		t.Fatal("an event for another occurrence was accepted")
	}
	event = acknowledgedEvent()
	event.Intended.SHA256 = "different"
	if _, err := p.recorded(&entry{Kind: recordRecorded, Occurrence: "o000001", Event: &event}); err == nil {
		t.Fatal("an event with other intended bytes was accepted")
	}
	event = acknowledgedEvent()
	event.Sent.Path = "payloads/o000001-elsewhere.bin"
	if _, err := p.recorded(&entry{Kind: recordRecorded, Occurrence: "o000001", Event: &event}); err == nil {
		t.Fatal("an event with unexpected evidence names was accepted")
	}
	// A delivery acknowledged with no matching intent is refused: the intent
	// named o000001, and nothing else may resolve it.
	p2 := testProtocol()
	readyRunning(p2)
	if err := p2.apply(&entry{Kind: recordIntent, Occurrence: "o000001"}); err != nil {
		t.Fatal(err)
	}
	missing := acknowledgedEvent()
	missing.OutboundOccurrence = "o000001"
	stranger := &entry{Kind: recordRecorded, Occurrence: "o000001", Event: &missing}
	stranger.Event.Delivery = "acknowledged"
	p2.pending = ""
	if _, err := p2.recorded(stranger); err == nil {
		t.Fatal("an acknowledgement resolved an intent it did not name")
	}
}
