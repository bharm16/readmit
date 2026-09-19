package diff_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/diff"
)

// message writes one raw HL7 message whose PID-7 carries the supplied value, so
// one selector can be driven through every operator outcome.
func message(t *testing.T, value string) diff.Input {
	t.Helper()
	path := filepath.Join(t.TempDir(), "message.hl7")
	text := "MSH|^~\\&|SYNTHETIC|LAB|READMIT|FIXTURE|20260101120000||ORU^R01|NORM-A|P|2.5.1\rPID|1||SECRET-ID-A||EXAMPLE||" + value + "\r"
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return diff.Input{Path: path}
}

func policyOf(t *testing.T, rule diff.Rule) diff.Policy {
	t.Helper()
	policy := diff.Policy{Schema: diff.PolicySchema, Rules: []diff.Rule{rule}}
	if err := policy.Validate(); err != nil {
		t.Fatalf("policy under test is itself invalid: %v", err)
	}
	return policy
}

func decide(t *testing.T, rule diff.Rule, left, right string) diff.Difference {
	t.Helper()
	report, err := diff.Normalize(message(t, left), message(t, right), diff.Options{}, policyOf(t, rule))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(report.Differences) != 1 {
		t.Fatalf("expected exactly one difference, got %+v", report.Differences)
	}
	return report.Differences[0]
}

func TestANormalizationRuleSuppressesOnlyWhatItsOperatorEstablishes(t *testing.T) {
	timestamp := func(precision string) diff.Rule {
		return diff.Rule{ID: "time", Selector: "PID-7", Operator: diff.TimestampOperator, Precision: precision}
	}
	numeric := func(tolerance string) diff.Rule {
		return diff.Rule{ID: "value", Selector: "PID-7", Operator: diff.NumericOperator, Tolerance: tolerance}
	}
	for _, c := range []struct {
		name        string
		rule        diff.Rule
		left, right string
		outcome     string
	}{
		{"jitter below the declared precision", timestamp("hour"), "20260101120000", "20260101125959", diff.Suppressed},
		{"jitter the declared precision can see", timestamp("minute"), "20260101120000", "20260101120100", diff.Retained},
		{"a coarser precision than the values carry", timestamp("day"), "20260101120000", "20260102000000", diff.Retained},
		{"identical declared offsets", timestamp("hour"), "20260101120000+0100", "20260101120500+0100", diff.Suppressed},
		{"fractional seconds below second precision", timestamp("second"), "20260101120000.5000", "20260101120000.9000", diff.Suppressed},
		{"a difference inside the tolerance", numeric("0.05"), "7.42", "7.45", diff.Suppressed},
		{"a difference exactly at the tolerance", numeric("0.03"), "7.42", "7.45", diff.Suppressed},
		{"a difference just outside the tolerance", numeric("0.02"), "7.42", "7.45", diff.Retained},
		{"a signed difference inside the tolerance", numeric("1"), "-0.5", "+0.4", diff.Suppressed},
		{"a zero tolerance on unequal numbers", numeric("0"), "7.42", "7.45", diff.Retained},
		{"a zero tolerance on the same number written twice", numeric("0"), "7.40", "7.4", diff.Suppressed},
		{"an unconditional volatile-field ignore", diff.Rule{ID: "volatile", Selector: "PID-7", Operator: diff.IgnoreOperator}, "RUN-0001", "RUN-0002", diff.Suppressed},
	} {
		t.Run(c.name, func(t *testing.T) {
			difference := decide(t, c.rule, c.left, c.right)
			if difference.Outcome != c.outcome || difference.Rule != c.rule.ID {
				t.Fatalf("expected %s by %s, got %+v", c.outcome, c.rule.ID, difference)
			}
			if difference.Reason != "" {
				t.Fatalf("a decided rule carries no undecided reason: %+v", difference)
			}
		})
	}
}

// A rule that cannot read the values it was scoped to settles nothing. None of
// these may be suppressed, and none may be folded into agreement.
func TestARuleThatCannotReadItsValuesIsUndecidedRatherThanSuppressing(t *testing.T) {
	numeric := diff.Rule{ID: "value", Selector: "PID-7", Operator: diff.NumericOperator, Tolerance: "1"}
	second := diff.Rule{ID: "time", Selector: "PID-7", Operator: diff.TimestampOperator, Precision: "second"}
	hour := diff.Rule{ID: "time", Selector: "PID-7", Operator: diff.TimestampOperator, Precision: "hour"}
	ignore := diff.Rule{ID: "volatile", Selector: "PID-7", Operator: diff.IgnoreOperator}
	for _, c := range []struct {
		name        string
		rule        diff.Rule
		left, right string
		reason      string
	}{
		{"text a numeric rule was scoped to", numeric, "STABLE", "CHANGED", diff.NotNumeric},
		{"a number with no digits after its point", numeric, "7.", "8", diff.NotNumeric},
		{"a value present on one side only", numeric, "7.42", "", diff.NotPresent},
		{"an explicit null against a number", numeric, "7.42", "\"\"", diff.NotPresent},
		{"a value coarser than the rule compares", second, "202601011200", "20260101120030", diff.CoarserThanRule},
		{"a date the calendar does not have", second, "20260231120000", "20260231120030", diff.NotTimestamp},
		{"a timestamp with an odd digit count", second, "2026010112000", "20260101120030", diff.NotTimestamp},
		{"an offset declared on one side only", hour, "20260101120000+0100", "20260101120500", diff.OffsetNotIdentical},
		{"two different declared offsets", hour, "20260101120000+0100", "20260101110500+0000", diff.OffsetNotIdentical},
		{"an escape this build does not decode", ignore, "A\\Zx\\B", "A\\Zx\\C", diff.UnsupportedEvidence},
	} {
		t.Run(c.name, func(t *testing.T) {
			difference := decide(t, c.rule, c.left, c.right)
			if difference.Outcome != diff.Undecided || difference.Reason != c.reason {
				t.Fatalf("expected undecided %s, got %+v", c.reason, difference)
			}
		})
	}
}

func TestANormalizationPolicyRefusesDocumentsThatCouldSilentlyDisableARule(t *testing.T) {
	for _, c := range []struct{ name, document string }{
		{"an unknown member", `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7","operator":"ignore","precisoin":"hour"}]}`},
		{"another contract's name", `{"schema":"readmit-normalization-policy/v2","rules":[{"id":"a","selector":"MSH-7","operator":"ignore"}]}`},
		{"no rules at all", `{"schema":"readmit-normalization-policy/v1","rules":[]}`},
		{"a repeated rule id", `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7","operator":"ignore"},{"id":"a","selector":"MSH-10","operator":"ignore"}]}`},
		{"two rules on one selection spelled differently", `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7","operator":"ignore"},{"id":"b","selector":"MSH[1]-7[1]","operator":"ignore"}]}`},
		{"a rule id that is not a name", `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"Volatile Time","selector":"MSH-7","operator":"ignore"}]}`},
		{"a selector that is not a selection", `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-0","operator":"ignore"}]}`},
		{"an operator this build does not have", `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7","operator":"regex","tolerance":"1"}]}`},
		{"a precision on an unconditional ignore", `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7","operator":"ignore","precision":"hour"}]}`},
		{"a tolerance on a timestamp rule", `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7","operator":"timestamp","precision":"hour","tolerance":"1"}]}`},
		{"a precision this build does not have", `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7","operator":"timestamp","precision":"fortnight"}]}`},
		{"a timestamp rule with no precision", `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7","operator":"timestamp"}]}`},
		{"a numeric rule with no tolerance", `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"OBX-5","operator":"numeric"}]}`},
		{"a tolerance that is a direction", `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"OBX-5","operator":"numeric","tolerance":"-0.05"}]}`},
		{"a tolerance that is not a number", `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"OBX-5","operator":"numeric","tolerance":"a lot"}]}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := diff.DecodePolicy([]byte(c.document)); err == nil {
				t.Fatal("an unreadable policy was accepted")
			}
		})
	}
}

// Both refusals exist so a normalization report can always answer "which rule
// suppressed this": an undeclared ignore could not be attributed to one, and a
// displayed value has no member in this contract to live in.
func TestANormalizationReportRefusesOptionsItCouldNotHonestlyReport(t *testing.T) {
	rules := policyOf(t, diff.Rule{ID: "volatile", Selector: "PID-7", Operator: diff.IgnoreOperator})
	left, right := message(t, "RUN-0001"), message(t, "RUN-0002")
	for _, c := range []struct {
		name    string
		options diff.Options
	}{
		{"an ignore selector no rule declared", diff.Options{Ignore: []string{"MSH-7"}}},
		{"displayed values", diff.Options{ShowValues: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := diff.Normalize(left, right, c.options, rules); err == nil {
				t.Fatal("an option the report could not describe was accepted")
			}
		})
	}
	if _, err := diff.Normalize(left, right, diff.Options{}, diff.Policy{Schema: diff.PolicySchema}); err == nil {
		t.Fatal("a policy with no rules was accepted at the comparison boundary")
	}
}

// The raw comparison is the one this report is read from, and it keeps the
// shape it shipped with: a policy adds no member to readmit-diff/v1 and changes
// nothing the same comparison would have said without one.
func TestANormalizationPolicyLeavesTheRawComparisonExactlyAsItWas(t *testing.T) {
	left, right := message(t, "7.42"), message(t, "7.45")
	rules := policyOf(t, diff.Rule{ID: "value", Selector: "PID-7", Operator: diff.NumericOperator, Tolerance: "1"})
	if _, err := diff.Normalize(left, right, diff.Options{}, rules); err != nil {
		t.Fatal(err)
	}
	raw, err := diff.Compare(left, right, diff.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if raw.Schema != "readmit-diff/v1" || raw.Summary.FieldChanges != 1 || raw.Pairs[0].Fields[0].Selector != "PID[1]-7[1]" {
		t.Fatalf("the raw comparison no longer reports what it found: %+v", raw)
	}
	encoded, err := diff.JSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"normalization", "policy", "suppressed", "rule"} {
		if strings.Contains(string(encoded), absent) {
			t.Fatalf("readmit-diff/v1 gained a %q member", absent)
		}
	}
}

// A field this build did not decode is an evidence gap, not a difference. The
// raw comparison lists it as `uncompared` and counts no field change; this
// report keeps that word rather than folding it into "a difference no rule
// addressed", which would read as two values that differ.
func TestAFieldThisBuildDidNotDecodeIsUncomparedRatherThanADifference(t *testing.T) {
	left, right := message(t, `A\Zx\B`), message(t, `A\Zx\B`)
	rules := policyOf(t, diff.Rule{ID: "elsewhere", Selector: "MSH-7", Operator: diff.IgnoreOperator})
	report, err := diff.Normalize(left, right, diff.Options{}, rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Differences) != 1 {
		t.Fatalf("expected one reported field, got %+v", report.Differences)
	}
	entry := report.Differences[0]
	if entry.Status != "uncompared" || entry.Outcome != diff.Undecided || entry.Reason != diff.UnsupportedEvidence || entry.Rule != "" {
		t.Fatalf("undecodable evidence was reported as a difference: %+v", entry)
	}
	if report.Summary.Differences != 0 || report.Summary.Uncompared != 1 || report.Summary.Undecided != 1 || report.Summary.Unaddressed != 0 {
		t.Fatalf("an evidence gap was counted as a difference: %+v", report.Summary)
	}
	if len(report.Unsupported) != 2 {
		t.Fatalf("both sides of the undecodable field are not listed: %+v", report.Unsupported)
	}
	raw, err := diff.Compare(left, right, diff.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if raw.Summary.FieldChanges != 0 || raw.Pairs[0].Fields[0].Status != "uncompared" {
		t.Fatalf("the two reports disagree about what the comparison found: %+v", raw.Summary)
	}
}
