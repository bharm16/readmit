package findingreview_test

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/findingreview"
	"github.com/bharm16/readmit/internal/hl7"
)

func TestDecisionsReaderRefusesWhatItCannotMean(t *testing.T) {
	valid := `{"schema":"readmit-finding-decisions/v1","report_sha256":"` + strings.Repeat("a", 64) +
		`","decisions":[{"finding":"f000001","verdict":"confirmed","rationale":"keep it"}]}`
	if _, err := findingreview.ParseDecisions([]byte(valid)); err != nil {
		t.Fatalf("a complete decisions document was refused: %v", err)
	}
	for name, document := range map[string]string{
		"unknown version":                    strings.Replace(valid, "decisions/v1", "decisions/v2", 1),
		"unknown member":                     strings.Replace(valid, `"decisions":[`, `"boundary":"ack-contract","decisions":[`, 1),
		"absent report identity":             strings.Replace(valid, `"report_sha256":"`+strings.Repeat("a", 64)+`",`, "", 1),
		"absent decisions":                   `{"schema":"readmit-finding-decisions/v1","report_sha256":"` + strings.Repeat("a", 64) + `"}`,
		"absent verdict":                     strings.Replace(valid, `"verdict":"confirmed",`, "", 1),
		"absent rationale":                   strings.Replace(valid, `,"rationale":"keep it"`, "", 1),
		"empty rationale":                    strings.Replace(valid, `"keep it"`, `""`, 1),
		"rationale holding a control byte":   strings.Replace(valid, `"keep it"`, `"keepit"`, 1),
		"unknown verdict":                    strings.Replace(valid, `"confirmed"`, `"probably"`, 1),
		"undecided verdict":                  strings.Replace(valid, `"confirmed"`, `"not_reviewed"`, 1),
		"scope on a confirmation":            strings.Replace(valid, `"verdict":"confirmed"`, `"verdict":"confirmed","scope":"case"`, 1),
		"suppression with no scope":          strings.Replace(valid, `"confirmed"`, `"suppressed"`, 1),
		"scope beyond the case":              strings.Replace(valid, `"verdict":"confirmed"`, `"verdict":"suppressed","scope":"everywhere"`, 1),
		"short report identity":              strings.Replace(valid, strings.Repeat("a", 64), "abc", 1),
		"finding named twice":                strings.Replace(valid, `]}`, `,{"finding":"f000001","verdict":"dismissed","rationale":"no"}]}`, 1),
		"finding outside the report grammar": strings.Replace(valid, `"f000001"`, `"first"`, 1),
		"unbounded rationale":                strings.Replace(valid, `"keep it"`, `"`+strings.Repeat("k", findingreview.MaxRationaleBytes+1)+`"`, 1),
	} {
		if _, err := findingreview.ParseDecisions([]byte(document)); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestReportReaderRefusesADocumentThatIsNotThisDiagnosis(t *testing.T) {
	path, _ := openCase(t, frame(booking, acknowledgement))
	report, _ := diagnosed(t, path)
	data, err := diagnose.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := findingreview.ParseReport(data); err != nil {
		t.Fatalf("the shipped diagnosis was refused: %v", err)
	}
	for name, document := range map[string]string{
		"unknown version": strings.Replace(string(data), "readmit-diagnosis/v1", "readmit-diagnosis/v2", 1),
		"unknown member":  strings.Replace(string(data), `"findings":`, `"verdict":"fine","findings":`, 1),
		"renamed finding": strings.Replace(string(data), `"f000001"`, `"first"`, 1),
	} {
		if _, err := findingreview.ParseReport([]byte(document)); err == nil {
			t.Fatalf("%s was accepted as a diagnosis report", name)
		}
	}
}

func TestAFindingNobodyReviewedIsNeverPromoted(t *testing.T) {
	path, source := openCase(t, frame(booking, acknowledgement))
	report, identity := diagnosed(t, path)
	if len(report.Findings) == 0 {
		t.Fatal("the fixture produced no findings to leave unreviewed")
	}
	for _, reported := range review(t, report, identity, source).Findings {
		if reported.Verdict != findingreview.NotReviewed || reported.Basis != findingreview.BasisUnreviewed {
			t.Fatalf("a finding nobody decided was given a verdict: %+v", reported)
		}
		if reported.Promotion != nil {
			t.Fatalf("an unreviewed finding was promoted: %+v", reported)
		}
		if reported.NextEvidence == "" {
			t.Fatalf("a finding was reported without the evidence that would settle it: %+v", reported)
		}
	}
}

func TestAConfirmedAcknowledgementFindingBecomesOneDraftAssertion(t *testing.T) {
	path, source := openCase(t, frame(booking, acknowledgement))
	report, identity := diagnosed(t, path)
	record := review(t, report, identity, source,
		findingreview.Decision{Finding: "f000001", Verdict: findingreview.Confirmed, Rationale: "the rejection must keep holding"})
	confirmed := status(t, record, "f000001")
	// The finding references MSA-1 and MSA-2. One becomes an expectation and
	// the other is named, so partial acceptance inside one finding is ordinary
	// rather than a choice between a whole test and nothing.
	if confirmed.Promotion == nil || len(confirmed.Promotion.Expectations) != 1 {
		t.Fatalf("a confirmed acknowledgement finding did not promote: %+v", confirmed)
	}
	expectation := confirmed.Promotion.Expectations[0]
	if expectation.Operator != "ack_field_equals" || expectation.ID != "f000001-1" {
		t.Fatalf("the promoted assertion does not name its finding: %+v", expectation)
	}
	if expectation.Field == nil || expectation.Field.State != hl7.Present || expectation.Field.Text == nil || *expectation.Field.Text != "AE" {
		t.Fatalf("the promoted assertion does not hold the captured outcome: %+v", expectation)
	}
	// The assertion reads the acknowledgement of a message the test sends,
	// never the acknowledgement occurrence itself.
	if len(confirmed.Promotion.Messages) != 1 || expectation.Message != confirmed.Promotion.Messages[0] {
		t.Fatalf("the promoted assertion does not read a message the test sends: %+v", confirmed.Promotion)
	}
	if expectation.Message == report.Findings[0].Evidence[0].Occurrence {
		t.Fatal("the promoted assertion sends the acknowledgement rather than the message it answers")
	}
	// MSA-2 is the echoed control identifier. It is refused by construction and
	// named, rather than dropped or copied into a document.
	if len(confirmed.Promotion.Unsupported) != 1 || confirmed.Promotion.Unsupported[0].Code != findingreview.PositionHasNoVocabulary {
		t.Fatalf("the identifier position was not refused by name: %+v", confirmed.Promotion.Unsupported)
	}
	encoded, err := findingreview.JSON(record)
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(encoded) + string(findingreview.Markdown(record))
	if strings.Contains(rendered, "FREE-TEXT-NEVER-COPIED") || strings.Contains(rendered, "BOOK-1") {
		t.Fatal("a review copied free text or an identifier out of the evidence")
	}
}

func TestADismissedOrSuppressedFindingPromotesNothing(t *testing.T) {
	path, source := openCase(t, frame(booking, acknowledgement))
	report, identity := diagnosed(t, path)
	for _, decision := range []findingreview.Decision{
		{Finding: "f000001", Verdict: findingreview.Dismissed, Rationale: "the vendor already fixed this"},
		{Finding: "f000001", Verdict: findingreview.Suppressed, Scope: findingreview.ScopeFinding, Rationale: "known and accepted here"},
	} {
		record := review(t, report, identity, source, decision)
		if reported := status(t, record, "f000001"); reported.Promotion != nil {
			t.Fatalf("a %s finding was promoted: %+v", decision.Verdict, reported)
		}
	}
}

func TestAnOccurrenceScopedSuppressionReachesTheSameRuleOnThatOccurrence(t *testing.T) {
	// Two ERR segments in one acknowledgement produce two findings of the same
	// rule on the same occurrence.
	path, source := openCase(t, frame(booking, twoErrorACK))
	report, identity := diagnosed(t, path)
	errorOutcomes := findingsOf(report, diagnose.ACKError)
	if len(errorOutcomes) != 2 {
		t.Fatalf("the fixture produced %d error outcomes, not two", len(errorOutcomes))
	}
	record := review(t, report, identity, source,
		findingreview.Decision{Finding: errorOutcomes[0], Verdict: findingreview.Suppressed, Scope: findingreview.ScopeOccurrence, Rationale: "this acknowledgement's error detail is accepted"})
	reached := status(t, record, errorOutcomes[1])
	if reached.Verdict != findingreview.Suppressed || reached.Basis != findingreview.BasisScope || reached.SuppressedBy != errorOutcomes[0] {
		t.Fatalf("an occurrence-scoped suppression did not reach the same rule on that occurrence: %+v", reached)
	}
	// A different rule on the same occurrence keeps its own verdict.
	outcomes := findingsOf(report, diagnose.ACKOutcome)
	if len(outcomes) != 1 {
		t.Fatalf("the fixture produced %d acknowledgement outcomes, not one", len(outcomes))
	}
	if other := status(t, record, outcomes[0]); other.Verdict != findingreview.NotReviewed {
		t.Fatalf("a suppression of one rule silenced another: %+v", other)
	}
}

func TestSuppressionReachesOnlyAsFarAsItsScope(t *testing.T) {
	// Two sources, each with its own acknowledgement, produce two findings of
	// the same rule on different occurrences.
	path, source := openCase(t,
		frame(booking, acknowledgement),
		frame(strings.ReplaceAll(booking, "BOOK-1", "BOOK-2"), strings.ReplaceAll(acknowledgement, "BOOK-1", "BOOK-2")))
	report, identity := diagnosed(t, path)
	outcomes := findingsOf(report, diagnose.ACKOutcome)
	if len(outcomes) != 2 {
		t.Fatalf("the fixture produced %d acknowledgement outcomes, not two", len(outcomes))
	}
	first, other := outcomes[0], outcomes[1]
	for name, scope := range map[string]string{"finding": findingreview.ScopeFinding, "occurrence": findingreview.ScopeOccurrence} {
		record := review(t, report, identity, source,
			findingreview.Decision{Finding: first, Verdict: findingreview.Suppressed, Scope: scope, Rationale: "accepted for this one"})
		if reported := status(t, record, other); reported.Verdict != findingreview.NotReviewed {
			t.Fatalf("a %s-scoped suppression reached another occurrence: %+v", name, reported)
		}
	}
	record := review(t, report, identity, source,
		findingreview.Decision{Finding: first, Verdict: findingreview.Suppressed, Scope: findingreview.ScopeCase, Rationale: "this rule is noise in this case"})
	reached := status(t, record, other)
	if reached.Verdict != findingreview.Suppressed || reached.Basis != findingreview.BasisScope || reached.SuppressedBy != first {
		t.Fatalf("a case-scoped suppression did not reach the same rule, or did not say where it came from: %+v", reached)
	}
	// A judgment somebody made about this finding is never overruled by one
	// made about another.
	record = review(t, report, identity, source,
		findingreview.Decision{Finding: first, Verdict: findingreview.Suppressed, Scope: findingreview.ScopeCase, Rationale: "this rule is noise in this case"},
		findingreview.Decision{Finding: other, Verdict: findingreview.Confirmed, Rationale: "this one is the incident"})
	if decided := status(t, record, other); decided.Verdict != findingreview.Confirmed || decided.Basis != findingreview.BasisDecision {
		t.Fatalf("a decision about the finding lost to a scope: %+v", decided)
	}
}
