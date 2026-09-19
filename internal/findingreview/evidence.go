package findingreview

import "github.com/bharm16/readmit/internal/diagnose"

// The next-evidence suggestions. Each one says what to collect; none of them
// says what the answer will be, because a finding nobody has settled is not
// made truer by suggesting how to settle it. The prose is readmit-authored and
// fixed per rule, so a suggestion never carries a value, an identifier or a
// path out of the case it was produced from.
//
// A rule with no entry here falls back to the suggestion for its
// classification, which is the honest general answer: a hypothesis wants a
// wider window, a profile violation wants the sending system's intent, and an
// observed fact wants nothing further.
var ruleEvidence = map[string]string{
	diagnose.BookingNotObserved:         "Capture a wider window from the same source, one that would contain the S12 booking for this appointment's filler identifier, and diagnose the new case with the same configuration.",
	diagnose.AppointmentNotObserved:     "Capture a wider window from the same source, one that would contain the S12 booking for this appointment's filler identifier, and diagnose the new case with the same configuration.",
	diagnose.VisitNotObserved:           "Capture a wider window from the same source, one that would contain the A01 or A04 for this visit identifier, and diagnose the new case with the same configuration.",
	diagnose.MergeIdentifierNotObserved: "Capture a wider window from the same source, one that would contain an occurrence carrying the prior patient identifier this merge names, and diagnose the new case with the same configuration.",
	diagnose.OrderNotObserved:           "Capture a wider window from the same source, one that would contain the ORM O01 carrying this placer order identifier, and diagnose the new case with the same configuration.",
	diagnose.ACKStageNotObserved:        "Capture the acknowledgement traffic for this occurrence as well as its outbound messages; a stage the header asks for is decided by an acknowledgement this window does not contain.",
	diagnose.StatusProgression:          "Read the declared status of each occurrence this identity appears in, and confirm with the sending system which of them it intended; the capture supplies no chronology and readmit will not guess which came first.",
	diagnose.DuplicateControl:           "Confirm with the sending system whether the repeated control identifier is a retransmission or two distinct messages; the capture establishes only that the identifier repeats.",
	diagnose.DuplicateOutput:            "Compare the full content of the occurrences this identity appears in, and confirm with the receiving system whether both were delivered; the capture establishes only that the output repeats in it.",
}

// evidenceByClassification is the fallback, keyed by the three classifications
// readmit-diagnosis/v1 records.
var evidenceByClassification = map[string]string{
	"observed_fact":     "The capture already establishes this fact. What is left is the decision whether it should hold again, which is what promoting it to a regression test records.",
	"profile_violation": "Read the named field of the original occurrence with readmit timeline CASE --show-values where you are authorized to, and confirm with the sending system what it intended to send.",
	"hypothesis":        "Capture a wider window from the same source and diagnose the new case with the same configuration. This finding relates identities inside one observed window; an earlier or uncaptured occurrence may exist.",
}

// nextEvidence reports what would settle one finding.
func nextEvidence(finding diagnose.Finding) string {
	if suggestion, ok := ruleEvidence[finding.RuleID]; ok {
		return suggestion
	}
	if suggestion, ok := evidenceByClassification[finding.Classification]; ok {
		return suggestion
	}
	return "This release suggests no further evidence for this rule; read the finding's own evidence references against the original occurrences."
}
