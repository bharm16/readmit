package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
)

// One incident as two systems saw it. The sender's capture and the receiver's
// capture are separate sources, and the receiver recorded the booking a second
// *before* the sender did: the two clocks do not agree, which is exactly why a
// sequence has to be built from the recorded times rather than from the order
// the sources were captured in.
const (
	// The sender's capture: a booking, its acknowledgement, a second booking
	// whose declared time is not a time at all and which nothing acknowledges,
	// and an acknowledgement naming a control ID this source does not carry.
	seqBooking      = "MSH|^~\\&|SEND|A|RECV|B|20260101120000||SIU^S12|CTL-1|P|2.5.1\rSCH|PLACER-1^READMIT|FILLER-1^READMIT||||CHECKUP|ROUTINE\rPID|1||MRN-1^^^READMIT^MR||DOE^JANE\r"
	seqBookingACK   = "MSH|^~\\&|RECV|B|SEND|A|20260101120001||ACK^S12|ACK-1|P|2.5.1\rMSA|AA|CTL-1\r"
	seqUntimed      = "MSH|^~\\&|SEND|A|RECV|B|NOT-A-TIME-AT-ALL||SIU^S12|CTL-2|P|2.5.1\rPID|1||MRN-1^^^READMIT^MR||DOE^JANE\r"
	seqStrayACK     = "MSH|^~\\&|RECV|B|SEND|A|20260101120004||ACK^S12|ACK-2|P|2.5.1\rMSA|AA|CTL-404\r"
	seqUndecodable  = "NOT-HL7-AT-ALL"
	seqRulesEntry   = "interface.rules.json"
	seqPatientRule  = "patient"
	seqControlRule  = "same-message"
	seqAcknowledges = "acknowledgements"
)

// seqRules declares one rule of each operator over both captures. Nothing here
// is a default: without this document the sequence reports only what the
// evidence itself recorded.
const seqRules = `{
  "schema": "readmit-correlation-rules/v1",
  "authorities": [{"key": "READMIT-MR", "namespace": "READMIT", "universal_id": "", "universal_id_type": ""}],
  "rules": [
    {"id": "acknowledgements", "operator": "acknowledges", "scope": "source"},
    {"id": "same-message", "operator": "control-id", "scope": "declared", "sources": ["s0001", "s0002"]},
    {"id": "patient", "operator": "identifier", "scope": "declared", "sources": ["s0001", "s0002"],
     "value": "PID-3.1", "authority": ["PID-3.4.1", "PID-3.4.2", "PID-3.4.3"]}
  ]
}`

func seqTime(second int) *time.Time {
	at := time.Date(2026, 1, 1, 12, 0, second, 0, time.UTC)
	return &at
}

// sequenceWorkspace writes that incident as one two-source case of an open
// workspace, with the declared rules beside it.
func sequenceWorkspace(t *testing.T) (*desktop.App, string, string) {
	t.Helper()
	root := t.TempDir()
	app := workspaceApp(t)
	written := writeInputs(t, root, "incident", []bundle.Input{
		{
			Path:    "sender-capture-path",
			Data:    []byte(framed(seqBooking) + framed(seqBookingACK) + framed(seqUntimed) + framed(seqStrayACK)),
			Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
			Observations: map[int]bundle.Observation{
				1: {Direction: bundle.Outbound, ObservedAt: seqTime(2)},
				2: {Direction: bundle.Inbound, ObservedAt: seqTime(3)},
				3: {Direction: bundle.Outbound},
				4: {Direction: bundle.Inbound, ObservedAt: seqTime(5)},
			},
		},
		{
			Path:    "receiver-capture-path",
			Data:    []byte(framed(seqBooking) + framed(seqUndecodable)),
			Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
			Observations: map[int]bundle.Observation{
				1: {Direction: bundle.Inbound, ObservedAt: seqTime(1)},
			},
		},
	})
	if err := os.WriteFile(filepath.Join(root, seqRulesEntry), []byte(seqRules), 0o600); err != nil {
		t.Fatal(err)
	}
	return app, root, written.Identity
}

func sequenceRequest(root, identity, rules string) desktop.SequenceRequest {
	return desktop.SequenceRequest{
		Workspace: root, Case: "incident", Identity: identity, Rules: rules,
		Offset: 0, Limit: desktop.MaxSequenceEvents,
	}
}

// laidOut runs one sequence and fails the test if the facade refused it.
func laidOut(t *testing.T, app *desktop.App, request desktop.SequenceRequest) *desktop.Sequence {
	t.Helper()
	result := app.OpenSequence(request)
	if result.State != desktop.Completed || result.Sequence == nil {
		t.Fatalf("the facade refused a sequence it supports: %+v", result)
	}
	return result.Sequence
}

func eventAt(t *testing.T, sequence *desktop.Sequence, occurrence string) desktop.SequenceEvent {
	t.Helper()
	for _, event := range sequence.Events {
		if event.Occurrence == occurrence {
			return event
		}
	}
	t.Fatalf("the sequence holds no event for %q", occurrence)
	return desktop.SequenceEvent{}
}

// The whole delivery in one sequence: every sent, received and acknowledgement
// event of both captures, in the order the recorded times put them rather than
// the order the sources were captured in, with the declared time beside the
// observed one, and with the occurrence nothing timed listed after all of them
// instead of being sorted into a position nothing establishes.
func TestASequenceOrdersByRecordedTimeAndLeavesUnknownTimesUnplaced(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	sequence := laidOut(t, app, sequenceRequest(root, identity, seqRulesEntry))

	if sequence.Total != 6 || len(sequence.Events) != 6 {
		t.Fatalf("the two captures hold six occurrences between them: %+v", sequence.Events)
	}
	// The receiver recorded the booking a second before the sender did, so the
	// receiver's copy is first. Source order would have put it fifth.
	order := []string{"s0002-e000001", "s0001-e000001", "s0001-e000002", "s0001-e000004", "s0001-e000003", "s0002-e000002"}
	for i, want := range order {
		event := sequence.Events[i]
		if event.Occurrence != want || event.Position != i+1 {
			t.Fatalf("position %d is %q at %d, not %q: %+v", i+1, event.Occurrence, event.Position, want, sequence.Events)
		}
	}
	// The first four were placed by a recorded time; the last two were not
	// placed at all, and say so rather than claiming the position they occupy.
	for i, event := range sequence.Events {
		want := desktop.ObservedOrder
		if i >= 4 {
			want = desktop.UnknownOrder
		}
		if event.Ordering != want {
			t.Fatalf("%s is ordered %q, not %q: %+v", event.Occurrence, event.Ordering, want, event)
		}
		if (event.ObservedAt == nil) != (want == desktop.UnknownOrder) {
			t.Fatalf("%s carries an observed time that disagrees with its ordering: %+v", event.Occurrence, event)
		}
	}
	if sequence.Summary.Ordered != 4 || sequence.Summary.Unordered != 2 {
		t.Fatalf("the counts do not split placed events from unplaced ones: %+v", sequence.Summary)
	}

	// Sent, received and acknowledgement events are each what the evidence
	// recorded them as; nothing here is inferred from a message type.
	if sequence.Summary.Messages != 3 || sequence.Summary.Acknowledgements != 2 || sequence.Summary.Unparsed != 1 {
		t.Fatalf("the kinds do not describe three messages, two acknowledgements and one unparsed occurrence: %+v", sequence.Summary)
	}
	if sequence.Summary.Sent != 2 || sequence.Summary.Received != 3 || sequence.Summary.UnknownDirection != 1 {
		t.Fatalf("the directions do not describe what the capture declared: %+v", sequence.Summary)
	}

	// The declared time is the sender's own claim and the observed time is the
	// capture's; both are beside each other, and neither is derived from the
	// other. The occurrence whose declared time is not a time keeps its state
	// and is not displayed as one.
	booking := eventAt(t, sequence, "s0001-e000001")
	if booking.DeclaredTime != "20260101120000" || booking.DeclaredState != hl7.Present {
		t.Fatalf("the booking does not carry the time the message itself declares: %+v", booking)
	}
	if booking.ObservedAt == nil || !booking.ObservedAt.Equal(*seqTime(2)) {
		t.Fatalf("the booking does not carry the time the capture recorded: %+v", booking)
	}
	untimed := eventAt(t, sequence, "s0001-e000003")
	if untimed.DeclaredTime != "" || untimed.DeclaredState != hl7.Present {
		t.Fatalf("a declared time that is not a timestamp was displayed anyway: %+v", untimed)
	}

	// The clock and ordering statements are carried, because reading two
	// captures beside one another without them invites exactly the conclusion
	// this view refuses to support.
	if !strings.Contains(sequence.Clock, "No clock is assumed to agree with another") {
		t.Fatalf("the sequence does not state its clock assumptions: %q", sequence.Clock)
	}
	if !strings.Contains(sequence.Scope, "Order is not causality") {
		t.Fatalf("the sequence does not state what its order is not: %q", sequence.Scope)
	}
}

// Every declared source is a lane, and a lane states its own recorded span
// rather than a span shared with another source's clock.
func TestEveryDeclaredSourceIsALaneIncludingOneThatObservedNothing(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	sequence := laidOut(t, app, sequenceRequest(root, identity, seqRulesEntry))

	if len(sequence.Lanes) != 2 || sequence.Lanes[0].SourceID != "s0001" || sequence.Lanes[1].SourceID != "s0002" {
		t.Fatalf("the lanes are not the declared sources in the order the case declares them: %+v", sequence.Lanes)
	}
	sender := sequence.Lanes[0]
	if sender.Occurrences != 4 || sender.Messages != 2 || sender.Acknowledgements != 2 || sender.Unparsed != 0 {
		t.Fatalf("the sender lane does not hold what that capture holds: %+v", sender)
	}
	if sender.Ordered != 3 || sender.Unordered != 1 {
		t.Fatalf("the sender lane does not separate placed occurrences from unplaced ones: %+v", sender)
	}
	if sender.Earliest == nil || !sender.Earliest.Equal(*seqTime(2)) || sender.Latest == nil || !sender.Latest.Equal(*seqTime(5)) {
		t.Fatalf("the sender lane does not span its own recorded times: %+v", sender)
	}
	receiver := sequence.Lanes[1]
	if receiver.Occurrences != 2 || receiver.Unparsed != 1 || receiver.Unordered != 1 {
		t.Fatalf("the receiver lane does not hold what that capture holds: %+v", receiver)
	}

	// A case with a source that recorded no time at all still has that lane,
	// with no span rather than a fabricated one.
	written := writeInputs(t, root, "silent", []bundle.Input{
		{Path: "quiet", Data: []byte(framed(seqBooking)), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}},
	})
	silent := laidOut(t, app, desktop.SequenceRequest{
		Workspace: root, Case: "silent", Identity: written.Identity, Limit: desktop.MaxSequenceEvents,
	})
	if len(silent.Lanes) != 1 || silent.Lanes[0].Earliest != nil || silent.Lanes[0].Latest != nil {
		t.Fatalf("a lane that observed no time was given a span: %+v", silent.Lanes)
	}
}

// Gaps are where this case stops saying what happened, in the evidence's own
// words. None of them is an explanation: an unacknowledged message is missing
// evidence and never proof that no acknowledgement was sent.
func TestASequenceReportsEveryGapTheEvidenceAlreadyRecorded(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	sequence := laidOut(t, app, sequenceRequest(root, identity, seqRulesEntry))

	counted := make(map[desktop.Gap]int, len(sequence.Gaps))
	for _, gap := range sequence.Gaps {
		counted[gap.Gap] = gap.Count
	}
	if len(sequence.Gaps) != 6 {
		t.Fatalf("a sequence does not report every gap it knows: %+v", sequence.Gaps)
	}
	want := map[desktop.Gap]int{
		desktop.UnknownObservedTime:       2,
		desktop.UnknownDeclaredTime:       1,
		desktop.UninterpretedDeclaredTime: 1,
		desktop.UnacknowledgedMessage:     2,
		desktop.UnmatchedAcknowledgement:  1,
		desktop.AmbiguousAcknowledgement:  0,
	}
	for gap, count := range want {
		if counted[gap] != count {
			t.Fatalf("gap %q was counted %d times, not %d: %+v", gap, counted[gap], count, sequence.Gaps)
		}
	}

	// A gap belongs to the occurrence whose evidence is incomplete.
	stray := eventAt(t, sequence, "s0001-e000004")
	if !containsGap(stray.Gaps, desktop.UnmatchedAcknowledgement) {
		t.Fatalf("the acknowledgement that resolved to nothing carries no gap: %+v", stray)
	}
	untimed := eventAt(t, sequence, "s0001-e000003")
	for _, gap := range []desktop.Gap{desktop.UnknownObservedTime, desktop.UninterpretedDeclaredTime, desktop.UnacknowledgedMessage} {
		if !containsGap(untimed.Gaps, gap) {
			t.Fatalf("the unacknowledged, untimed booking does not carry %q: %+v", gap, untimed)
		}
	}
	undecodable := eventAt(t, sequence, "s0002-e000002")
	if undecodable.Decoded || !containsGap(undecodable.Gaps, desktop.UnknownDeclaredTime) {
		t.Fatalf("an occurrence nothing decoded does not say so: %+v", undecodable)
	}
	// The acknowledged booking is complete evidence, so it carries no gap.
	if acknowledged := eventAt(t, sequence, "s0001-e000001"); len(acknowledged.Gaps) != 0 {
		t.Fatalf("complete evidence was reported as a gap: %+v", acknowledged)
	}
}

func containsGap(gaps []desktop.Gap, want desktop.Gap) bool {
	for _, gap := range gaps {
		if gap == want {
			return true
		}
	}
	return false
}

// Clicking an event reaches the original occurrence and everything recorded
// about it. The references are the two engines' own readings, distinguished:
// what the evidence itself declares is observed linkage, and what a declared
// rule inferred from equal keys says so and names the rule that produced it.
func TestAnEventCarriesTheReferencesTheEnginesRecordedAboutIt(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	sequence := laidOut(t, app, sequenceRequest(root, identity, seqRulesEntry))

	if sequence.Report != correlate.ReportSchema || sequence.RulesSHA256 == "" {
		t.Fatalf("the sequence does not name the contract and the exact rules it read: %+v", sequence)
	}
	if len(sequence.Declared) != 3 || sequence.Declared[0].ID != seqAcknowledges {
		t.Fatalf("the declared rules are not restated in document order: %+v", sequence.Declared)
	}
	if !strings.Contains(sequence.Boundary, "A link records that a rule's declared key was equal") {
		t.Fatalf("the correlation report's own boundary statement was not carried: %q", sequence.Boundary)
	}

	// An acknowledgement the case itself matched: the evidence declares it, so
	// it is observed linkage with no rule behind it at all.
	booking := eventAt(t, sequence, "s0001-e000001")
	matched := referenceOf(t, booking, desktop.AcknowledgementReference, "")
	if matched.Linkage != correlate.Observed || len(matched.Related) != 1 || matched.Related[0] != "s0001-e000002" {
		t.Fatalf("the case's own acknowledgement match is not an observed reference to the acknowledgement: %+v", matched)
	}

	// The same control ID in two captures is a declared rule's inference over
	// equal keys, never the evidence referring to itself.
	crossSource := referenceOf(t, booking, desktop.LinkReference, seqControlRule)
	if crossSource.Linkage != correlate.Inferred || crossSource.Operator != correlate.ControlID {
		t.Fatalf("a cross-source control-ID link was not reported as an inference: %+v", crossSource)
	}
	if len(crossSource.Related) != 1 || crossSource.Related[0] != "s0002-e000001" {
		t.Fatalf("the cross-source link does not name the other capture's occurrence: %+v", crossSource)
	}

	// The identifier link names the configured authority it was qualified by,
	// so a person can see which mapping they wrote produced it.
	patient := referenceOf(t, booking, desktop.LinkReference, seqPatientRule)
	if patient.Authority != "READMIT-MR" || patient.Occurrences != 3 {
		t.Fatalf("the identifier link does not name its configured authority and its whole membership: %+v", patient)
	}

	// An occurrence no rule could read is referenced as unsupported, which is
	// not a pass: it is in no link of the rule that reported it.
	undecodable := eventAt(t, sequence, "s0002-e000002")
	unsupported := referenceOf(t, undecodable, desktop.UnsupportedReference, "")
	if unsupported.Reason != "unparsed_occurrence" || unsupported.Linkage != "" {
		t.Fatalf("an unparsed occurrence was not reported as unevaluated: %+v", unsupported)
	}
	if undecodable.Referenced != len(undecodable.References) {
		t.Fatalf("the reference count disagrees with the references beside it: %+v", undecodable)
	}

	// An occurrence nothing refers to still carries a list the window can draw,
	// never an absent one, so the two sides of this boundary agree about shape
	// as well as about content.
	unreferenced := laidOut(t, app, sequenceRequest(root, identity, ""))
	stray := eventAt(t, unreferenced, "s0002-e000002")
	if stray.References == nil || len(stray.References) != 0 || stray.Referenced != 0 {
		t.Fatalf("an occurrence nothing refers to did not carry an empty list: %+v", stray)
	}
}

func referenceOf(t *testing.T, event desktop.SequenceEvent, kind desktop.ReferenceKind, rule string) desktop.EvidenceReference {
	t.Helper()
	for _, reference := range event.References {
		if reference.Kind == kind && reference.Rule == rule {
			return reference
		}
	}
	t.Fatalf("%s carries no %q reference for rule %q: %+v", event.Occurrence, kind, rule, event.References)
	return desktop.EvidenceReference{}
}

// There is no default rule set here either. Asked without one, the sequence
// reports what the evidence itself recorded and nothing more: no link, no
// collision, and no claim that any rule was applied.
func TestASequenceWithoutDeclaredRulesCorrelatesNothing(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	sequence := laidOut(t, app, sequenceRequest(root, identity, ""))

	if sequence.Rules != "" || sequence.Report != "" || sequence.RulesSHA256 != "" || sequence.Boundary != "" {
		t.Fatalf("a sequence with no declared rules claimed a correlation report: %+v", sequence)
	}
	if len(sequence.Declared) != 0 || len(sequence.Unsupported) != 0 {
		t.Fatalf("a sequence with no declared rules restated rules: %+v", sequence)
	}
	if sequence.Summary.Links != 0 || sequence.Summary.Collisions != 0 || sequence.Summary.Unsupported != 0 {
		t.Fatalf("a sequence with no declared rules counted correlations: %+v", sequence.Summary)
	}
	// What the evidence carries with no configuration at all is still there.
	if sequence.Total != 6 {
		t.Fatalf("the events themselves depend on a rules document: %+v", sequence.Events)
	}
	booking := eventAt(t, sequence, "s0001-e000001")
	for _, reference := range booking.References {
		if reference.Kind != desktop.AcknowledgementReference {
			t.Fatalf("a reference appeared with no rule behind it: %+v", reference)
		}
	}
}

// A window is a window. Every one of them states where it begins and how many
// events the sequence holds, and the lanes, gaps and counts beside it cover the
// whole case, so a window can never read as all of it.
func TestASequenceWindowNeverReadsAsTheWholeSequence(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	request := sequenceRequest(root, identity, seqRulesEntry)
	request.Offset, request.Limit = 2, 2

	window := laidOut(t, app, request)
	if window.Total != 6 || len(window.Events) != 2 || window.Offset != 2 || window.Limit != 2 {
		t.Fatalf("a window of two events does not say where it begins in six: %+v", window)
	}
	if window.Events[0].Position != 3 || window.Events[1].Position != 4 {
		t.Fatalf("a window's events do not keep their positions in the whole sequence: %+v", window.Events)
	}
	if window.Summary.Occurrences != 6 || len(window.Lanes) != 2 {
		t.Fatalf("the counts beside a window describe the window: %+v", window.Summary)
	}

	request.Offset = 6
	past := app.OpenSequence(request)
	if past.State != desktop.Empty || past.Sequence == nil || past.Sequence.Total != 6 {
		t.Fatalf("a window past the last event was not empty with the counts beside it: %+v", past)
	}
	if !strings.Contains(past.Reason, "past the last event") {
		t.Fatalf("the reason does not say why the window is empty: %q", past.Reason)
	}
}

// A sequence carries no message content. The one exception is a declared time
// whose bytes are shaped like a timestamp and can be nothing else, which is the
// rule `readmit timeline` already applies to the same field.
func TestASequenceCarriesNoMessageContentBeyondADeclaredTimestamp(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	encoded, err := json.Marshal(laidOut(t, app, sequenceRequest(root, identity, seqRulesEntry)))
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"MRN-", "DOE", "JANE", "CTL-", "ACK-1", "ACK-2", "FILLER-", "PLACER-", "CHECKUP", "NOT-A-TIME", "NOT-HL7", "capture-path", root} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("the sequence exposed %q: %s", leaked, encoded)
		}
	}
	if !strings.Contains(string(encoded), "20260101120000") {
		t.Fatal("the declared timestamps this view does display were not displayed")
	}
}

// Everything a sequence refuses, and the recovery after it: the slot is
// released, the evidence is untouched, and no refusal repeats a path or a value.
func TestASequenceRefusesWhatItCannotRead(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("not evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken.rules.json"), []byte(`{"schema":"readmit-correlation-rules/v2","rules":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "elsewhere.json"), []byte(seqRules), 0o600); err != nil {
		t.Fatal(err)
	}

	refusals := map[string]desktop.SequenceRequest{
		"a workspace that is not a folder":          {Workspace: filepath.Join(root, "notes.txt"), Case: "incident", Identity: identity, Limit: 10},
		"a case outside the workspace":              {Workspace: root, Case: filepath.Join(outside, "elsewhere"), Identity: identity, Limit: 10},
		"a case naming a parent":                    {Workspace: root, Case: "../incident", Identity: identity, Limit: 10},
		"an entry that is not a case bundle":        {Workspace: root, Case: "notes.txt", Identity: identity, Limit: 10},
		"evidence that changed since it was shown":  {Workspace: root, Case: "incident", Identity: "0000", Limit: 10},
		"no identity to bind the sequence to":       {Workspace: root, Case: "incident", Limit: 10},
		"rules outside the workspace":               {Workspace: root, Case: "incident", Identity: identity, Rules: filepath.Join(outside, "elsewhere.json"), Limit: 10},
		"rules naming a parent":                     {Workspace: root, Case: "incident", Identity: identity, Rules: "../" + seqRulesEntry, Limit: 10},
		"rules that are not a file of the folder":   {Workspace: root, Case: "incident", Identity: identity, Rules: "incident", Limit: 10},
		"rules this release does not read":          {Workspace: root, Case: "incident", Identity: identity, Rules: "broken.rules.json", Limit: 10},
		"rules that are not a rules document":       {Workspace: root, Case: "incident", Identity: identity, Rules: "notes.txt", Limit: 10},
		"a window beginning before the first event": {Workspace: root, Case: "incident", Identity: identity, Offset: -1, Limit: 10},
		"an unbounded window":                       {Workspace: root, Case: "incident", Identity: identity, Limit: desktop.MaxSequenceEvents + 1},
		"a window of no events at all":              {Workspace: root, Case: "incident", Identity: identity, Limit: 0},
	}
	for name, request := range refusals {
		t.Run(name, func(t *testing.T) {
			refused := app.OpenSequence(request)
			if refused.State != desktop.Failed || refused.Sequence != nil {
				t.Fatalf("the facade laid out %s: %+v", name, refused)
			}
			if refused.Reason == "" {
				t.Fatal("a refusal says nothing about itself")
			}
			for _, disclosed := range []string{root, outside, "MRN-", "CTL-", "not evidence"} {
				if strings.Contains(refused.Reason, disclosed) {
					t.Fatalf("the refusal repeated %q: %q", disclosed, refused.Reason)
				}
			}
		})
	}
	// Recovery: every refusal above released the slot and changed nothing.
	if recovered := app.OpenSequence(sequenceRequest(root, identity, seqRulesEntry)); recovered.State != desktop.Completed {
		t.Fatalf("a refused sequence left the facade unusable: %+v", recovered)
	}
}

// Laying out a sequence verifies a case and reads the rules, so it holds the
// one operation slot while it runs and releases it whatever the outcome.
// Cancelling does not interrupt it: it runs to completion under the readers'
// own limits once it starts.
func TestLayingOutASequenceHoldsTheSameOperationSlot(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)

	reentrant := &chooser{folder: root}
	second := desktop.New(reentrant, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"), filepath.Join(filepath.Dir(filepath.Join(t.TempDir(), "session.json")), "drafts.json"))
	var concurrent desktop.SequenceResult
	reentrant.before = func() { concurrent = second.OpenSequence(sequenceRequest(root, identity, seqRulesEntry)) }
	if opened := second.SelectWorkspace(); opened.State != desktop.Completed {
		t.Fatalf("the first operation did not complete: %+v", opened)
	}
	if concurrent.State != desktop.Busy || concurrent.Sequence != nil {
		t.Fatalf("a sequence ran while another operation held the facade: %+v", concurrent)
	}

	app.Cancel("")
	if uninterrupted := laidOut(t, app, sequenceRequest(root, identity, seqRulesEntry)); uninterrupted.Total != 6 {
		t.Fatalf("cancelling changed what a sequence reports: %+v", uninterrupted)
	}
}

// A sequence reads. The case is byte-identical afterwards, whether the sequence
// completed or was refused, and nothing is written beside it.
func TestLayingOutASequenceChangesNoEvidence(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	before := evidenceDigest(t, filepath.Join(root, "incident"))
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}

	if laidOut(t, app, sequenceRequest(root, identity, seqRulesEntry)).Total != 6 {
		t.Fatal("the sequence this test reads did not run")
	}
	if refused := app.OpenSequence(sequenceRequest(root, "0000", seqRulesEntry)); refused.State != desktop.Failed {
		t.Fatalf("the refusal this test reads did not happen: %+v", refused)
	}

	if after := evidenceDigest(t, filepath.Join(root, "incident")); after != before {
		t.Fatal("reading a sequence changed the case it read")
	}
	written, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != len(entries) {
		t.Fatalf("a sequence wrote something into the workspace: %d entries, not %d", len(written), len(entries))
	}
}

// A capture that recorded nothing is still a lane, and what it preserved is
// still an occurrence nothing decoded rather than an absence quietly dropped
// out of the picture. A sequence over it says so instead of looking complete.
func TestACaptureThatRecordedNothingStaysVisibleAsMissingEvidence(t *testing.T) {
	app, root, _ := sequenceWorkspace(t)
	written := writeInputs(t, root, "nothing", []bundle.Input{
		{Path: "silent-capture-path", Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}},
	})

	sequence := laidOut(t, app, desktop.SequenceRequest{
		Workspace: root, Case: "nothing", Identity: written.Identity, Limit: desktop.MaxSequenceEvents,
	})
	if len(sequence.Lanes) != 1 || sequence.Lanes[0].Occurrences != 1 || sequence.Lanes[0].Unparsed != 1 {
		t.Fatalf("a capture that recorded nothing lost its lane: %+v", sequence.Lanes)
	}
	if sequence.Lanes[0].Earliest != nil || sequence.Lanes[0].Latest != nil {
		t.Fatalf("a lane with no recorded time was given a span: %+v", sequence.Lanes[0])
	}
	only := sequence.Events[0]
	if only.Decoded || only.Ordering != desktop.UnknownOrder {
		t.Fatalf("what the capture preserved was reported as readable evidence: %+v", only)
	}
	for _, gap := range []desktop.Gap{desktop.UnknownObservedTime, desktop.UnknownDeclaredTime} {
		if !containsGap(only.Gaps, gap) {
			t.Fatalf("the occurrence nothing decoded does not carry %q: %+v", gap, only)
		}
	}
	// Every gap this view knows is still reported, including the ones nothing
	// here carries, so a case with nothing missing says so rather than showing
	// an empty list.
	if len(sequence.Gaps) != 6 {
		t.Fatalf("a sequence does not report every gap it knows: %+v", sequence.Gaps)
	}
}
