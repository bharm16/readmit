package sequenceanalysis

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/hl7"
)

func framed(message string) string { return "\x0b" + message + "\x1c\r" }

func options() hl7.Options { return hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR} }

func seqAt(second int) *time.Time {
	at := time.Date(2026, 1, 1, 12, 0, second, 0, time.UTC)
	return &at
}

// One booking and its acknowledgement as two captures saw them. The booking's
// declared instant carries an explicit offset; the acknowledgement answers
// CTL-1 one second later.
const (
	evalBooking    = "MSH|^~\\&|SEND|A|RECV|B|20260101120100+0000||SIU^S12|CTL-1|P|2.5.1\rPID|1||MRN-1^^^READMIT^MR||DOE^JANE\r"
	evalBookingACK = "MSH|^~\\&|RECV|B|SEND|A|20260101120101+0000||ACK^S12|ACK-1|P|2.5.1\rMSA|AA|CTL-1\r"
)

func evalCase(t *testing.T, inputs []bundle.Input) *bundle.Bundle {
	t.Helper()
	written, err := bundle.Write(filepath.Join(t.TempDir(), "case"), inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: seqAt(0)})
	if err != nil {
		t.Fatalf("case bundle: %v", err)
	}
	return written
}

func declaration(identity string, tolerance int, windows []Window) Declaration {
	return Declaration{Schema: Schema, CaseIdentity: identity, ClockToleranceSeconds: tolerance,
		Windows: windows, Retries: []Retry{}, Downstream: []Downstream{}}
}

func day(source string) Window {
	return Window{Source: source, Start: *seqAt(0), End: *seqAt(3600), Coverage: "complete"}
}

func kinds(r *Report) map[string]int {
	found := map[string]int{}
	for _, f := range r.Findings {
		found[f.Kind]++
	}
	return found
}

func ackLink(message, acknowledgement string) *correlate.Report {
	return &correlate.Report{RulesSHA256: "rules-digest", Links: []correlate.Link{{
		Rule: "acks", Operator: correlate.Acknowledges, Occurrences: []correlate.Reference{
			{Occurrence: message, SourceID: "s0001"},
			{Occurrence: acknowledgement, SourceID: "s0002"},
		},
	}}}
}

// Declared clocks: a second-precision instant with a known offset is compared
// to the observed time; anything else stays unknown rather than inferred.
func TestEvaluateExplainsDeclaredClocks(t *testing.T) {
	b := evalCase(t, []bundle.Input{{
		Path: "sender", Data: []byte(
			framed(evalBooking) + framed(evalBooking) +
				framed(strings.Replace(evalBooking, "20260101120100+0000", "20260101120000", 1)) +
				framed(strings.Replace(evalBooking, "CTL-1", "CTL-2", 1)) +
				framed(strings.Replace(evalBooking, "20260101120100+0000", "20260101250100+0000", 1))),
		Options: options(),
		Observations: map[int]bundle.Observation{
			1: {Direction: bundle.Outbound, ObservedAt: seqAt(1)},
			2: {Direction: bundle.Outbound, ObservedAt: seqAt(2)},
			3: {Direction: bundle.Outbound, ObservedAt: seqAt(3)},
			4: {Direction: bundle.Outbound},
			5: {Direction: bundle.Outbound, ObservedAt: seqAt(5)},
		},
	}})
	report, err := Evaluate(b, declaration(b.Identity, 5, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := kinds(report)
	// Identical bytes observed twice apart, both beyond the tolerance.
	if found["duplicate_occurrence"] != 1 || found["clock_mismatch"] != 2 {
		t.Fatalf("byte and clock evidence lost: %+v", found)
	}
	// A timezone-free DTM, a missing observation and an impossible hour each
	// stay unknown; none is corrected and none becomes a mismatch.
	if found["clock_unknown"] != 3 {
		t.Fatalf("unsupported declarations must stay unknown: %+v", found)
	}
	// Without a declared window no acknowledgement absence is assessed.
	if found["ack_coverage_unknown"] != 5 {
		t.Fatalf("absence assessed outside a window: %+v", found)
	}
}

// Windows decide whether an unacknowledged message is missing evidence,
// unresolved, or answered — and the acknowledgement's own window counts.
func TestEvaluateWindowsDecideAckFindings(t *testing.T) {
	// Observed one second after each declared instant, so clock findings stay
	// out of the way of the window findings under test.
	message := bundle.Input{Path: "sender", Data: []byte(framed(evalBooking)), Options: options(),
		Observations: map[int]bundle.Observation{1: {Direction: bundle.Outbound, ObservedAt: seqAt(60)}}}
	acknowledged := func(observed *time.Time) bundle.Input {
		return bundle.Input{Path: "receiver", Data: []byte(framed(evalBookingACK)), Options: options(),
			Observations: map[int]bundle.Observation{1: {Direction: bundle.Inbound, ObservedAt: observed}}}
	}

	t.Run("no window for the message", func(t *testing.T) {
		b := evalCase(t, []bundle.Input{message, acknowledged(seqAt(61))})
		report, err := Evaluate(b, declaration(b.Identity, 5, []Window{day("s0002")}), nil)
		if err != nil {
			t.Fatal(err)
		}
		if kinds(report)["ack_coverage_unknown"] != 1 {
			t.Fatalf("absence assessed without a containing window: %+v", report.Findings)
		}
	})
	t.Run("window with no linked acknowledgement", func(t *testing.T) {
		b := evalCase(t, []bundle.Input{message, acknowledged(seqAt(61))})
		report, err := Evaluate(b, declaration(b.Identity, 5, []Window{day("s0001"), day("s0002")}), nil)
		if err != nil {
			t.Fatal(err)
		}
		if kinds(report)["missing_ack"] != 1 {
			t.Fatalf("an unlinked acknowledgement must be missing evidence: %+v", report.Findings)
		}
	})
	t.Run("linked acknowledgement inside its own window", func(t *testing.T) {
		b := evalCase(t, []bundle.Input{message, acknowledged(seqAt(61))})
		report, err := Evaluate(b, declaration(b.Identity, 5, []Window{day("s0001"), day("s0002")}), ackLink("s0001-e000001", "s0002-e000001"))
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Findings) != 0 {
			t.Fatalf("an answered message was treated as unanswered: %+v", report.Findings)
		}
	})
	t.Run("acknowledgement observed outside its window", func(t *testing.T) {
		b := evalCase(t, []bundle.Input{message, acknowledged(seqAt(61))})
		answered := day("s0002")
		answered.End = *seqAt(30)
		report, err := Evaluate(b, declaration(b.Identity, 5, []Window{day("s0001"), answered}), ackLink("s0001-e000001", "s0002-e000001"))
		if err != nil {
			t.Fatal(err)
		}
		if kinds(report)["missing_ack"] != 1 {
			t.Fatalf("an out-of-window acknowledgement must not answer: %+v", report.Findings)
		}
	})
	t.Run("acknowledgement with no observed time", func(t *testing.T) {
		b := evalCase(t, []bundle.Input{message, acknowledged(nil)})
		report, err := Evaluate(b, declaration(b.Identity, 5, []Window{day("s0001"), day("s0002")}), ackLink("s0001-e000001", "s0002-e000001"))
		if err != nil {
			t.Fatal(err)
		}
		if kinds(report)["ack_coverage_unknown"] != 1 {
			t.Fatalf("an untimed acknowledgement must stay unresolved: %+v", report.Findings)
		}
	})
}

// An operator-reported retry is explained as a likely retransmission only with
// same-source, ordered, byte-equal evidence inside the window.
func TestEvaluateRetriesAreDeclarationsNotProofs(t *testing.T) {
	// One source holds both occurrences: a reported retry is same-source
	// evidence, and the observed times follow the declared instant closely
	// enough that clock findings stay out of the way.
	retry := func(second string, at *time.Time) *bundle.Bundle {
		return evalCase(t, []bundle.Input{bundle.Input{
			Path:    "sender",
			Data:    []byte(framed(evalBooking) + framed(second)),
			Options: options(),
			Observations: map[int]bundle.Observation{
				1: {Direction: bundle.Outbound, ObservedAt: seqAt(61)},
				2: {Direction: bundle.Outbound, ObservedAt: at},
			},
		}})
	}
	window := []Window{day("s0001")}

	t.Run("equal bytes in order", func(t *testing.T) {
		b := retry(evalBooking, seqAt(62))
		d := declaration(b.Identity, 5, window)
		d.Retries = []Retry{{First: "s0001-e000001", Retry: "s0001-e000002", Basis: "operator_reported_retry"}}
		report, err := Evaluate(b, d, nil)
		if err != nil {
			t.Fatal(err)
		}
		if kinds(report)["likely_retransmission"] != 1 || kinds(report)["retry_unresolved"] != 0 {
			t.Fatalf("an evidenced retry was not explained: %+v", report.Findings)
		}
	})
	t.Run("transformed bytes", func(t *testing.T) {
		b := retry(strings.Replace(evalBooking, "DOE", "DOEY", 1), seqAt(62))
		d := declaration(b.Identity, 5, window)
		d.Retries = []Retry{{First: "s0001-e000001", Retry: "s0001-e000002", Basis: "operator_reported_retry"}}
		report, err := Evaluate(b, d, nil)
		if err != nil {
			t.Fatal(err)
		}
		if kinds(report)["retry_unresolved"] != 1 {
			t.Fatalf("a transformed retry was explained: %+v", report.Findings)
		}
	})
	t.Run("observed times out of order", func(t *testing.T) {
		b := retry(evalBooking, seqAt(60))
		d := declaration(b.Identity, 5, window)
		d.Retries = []Retry{{First: "s0001-e000001", Retry: "s0001-e000002", Basis: "operator_reported_retry"}}
		report, err := Evaluate(b, d, nil)
		if err != nil {
			t.Fatal(err)
		}
		if kinds(report)["likely_retransmission"] != 0 || kinds(report)["retry_unresolved"] != 1 {
			t.Fatalf("an unordered retry was explained: %+v", report.Findings)
		}
	})
	t.Run("wrong basis, unknown or repeated occurrences refuse", func(t *testing.T) {
		b := retry(evalBooking, seqAt(62))
		for name, retries := range map[string][]Retry{
			"basis":     {{First: "s0001-e000001", Retry: "s0001-e000002", Basis: "observed"}},
			"unknown":   {{First: "s0001-e000001", Retry: "s0001-e000999", Basis: "operator_reported_retry"}},
			"repeated":  {{First: "s0001-e000001", Retry: "s0001-e000002", Basis: "operator_reported_retry"}, {First: "s0001-e000001", Retry: "s0001-e000002", Basis: "operator_reported_retry"}},
			"identical": {{First: "s0001-e000001", Retry: "s0001-e000001", Basis: "operator_reported_retry"}},
		} {
			d := declaration(b.Identity, 5, window)
			d.Retries = retries
			if _, err := Evaluate(b, d, nil); err == nil {
				t.Fatalf("%s retry declaration accepted", name)
			}
		}
	})
}

// Declarations that cannot bind the evidence are refused, not explained away.
func TestEvaluateRefusesDeclarationsThatCannotBindTheEvidence(t *testing.T) {
	// Observed one second after each declared instant, so clock findings stay
	// out of the way of the window findings under test.
	message := bundle.Input{Path: "sender", Data: []byte(framed(evalBooking)), Options: options(),
		Observations: map[int]bundle.Observation{1: {Direction: bundle.Outbound, ObservedAt: seqAt(60)}}}
	b := evalCase(t, []bundle.Input{message})

	stale := declaration("another case", 5, []Window{day("s0001")})
	if _, err := Evaluate(b, stale, nil); err == nil {
		t.Fatal("a declaration for another case was evaluated")
	}

	strayDigest := declaration(b.Identity, 5, []Window{day("s0001")})
	strayDigest.RulesSHA256 = "rules-digest"
	if _, err := Evaluate(b, strayDigest, nil); err == nil {
		t.Fatal("a digest was checked against no report")
	}

	expectsDownstream := declaration(b.Identity, 5, []Window{day("s0001")})
	expectsDownstream.Downstream = []Downstream{{Occurrence: "s0001-e000001", Source: "s0002", Rule: "control"}}
	if _, err := Evaluate(b, expectsDownstream, nil); err == nil {
		t.Fatal("downstream expectations were assessed without correlation rules")
	}

	mysterySource := declaration(b.Identity, 5, []Window{{Source: "s9999", Start: *seqAt(0), End: *seqAt(1), Coverage: "complete"}})
	if _, err := Evaluate(b, mysterySource, nil); err == nil {
		t.Fatal("a window over an absent source was accepted")
	}

	reversed := declaration(b.Identity, 5, []Window{{Source: "s0001", Start: *seqAt(3600), End: *seqAt(0), Coverage: "complete"}})
	if _, err := Evaluate(b, reversed, nil); err == nil {
		t.Fatal("a window that ends before it opens was accepted")
	}

	twice := declaration(b.Identity, 5, []Window{day("s0001"), day("s0001")})
	if _, err := Evaluate(b, twice, nil); err == nil {
		t.Fatal("a source was covered by two windows")
	}

	mysteryCoverage := declaration(b.Identity, 5, []Window{{Source: "s0001", Start: *seqAt(0), End: *seqAt(1), Coverage: "mystery"}})
	if _, err := Evaluate(b, mysteryCoverage, nil); err == nil {
		t.Fatal("an unknown coverage claim was accepted")
	}
}
