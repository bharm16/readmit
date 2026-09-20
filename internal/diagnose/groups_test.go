package diagnose_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/diagnose"
)

func TestGroupsRetainEveryFindingAndRepresentativeCase(t *testing.T) {
	raw := fixture(t, "diagnose-reschedule.hl7")
	a := writeCase(t, raw)
	// An additional unsupported occurrence changes the case without hiding the hypothesis.
	b := writeCase(t, raw, []byte("not hl7"))
	result, err := diagnose.GroupCases(context.Background(), []string{a, b}, diagnose.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	found := false
	for _, g := range result.Groups {
		total += len(g.Members)
		if g.RuleID == diagnose.BookingNotObserved {
			found = true
			if len(g.Members) != 2 || len(g.Representatives) != 2 || len(g.Occurrences) != 2 {
				t.Fatalf("missing members: %+v", g)
			}
		}
	}
	if !found {
		t.Fatal("missing repeated hypothesis")
	}
	want := 0
	unsupported := 0
	for _, report := range result.Cases {
		want += len(report.Findings)
		unsupported += len(report.Unsupported)
		var path string
		if report.CaseIdentity == run(t, a, diagnose.DefaultConfig()).CaseIdentity {
			path = a
		} else {
			path = b
		}
		if !reflect.DeepEqual(report, run(t, path, diagnose.DefaultConfig())) {
			t.Fatal("grouping changed diagnosis")
		}
	}
	if total != want || unsupported == 0 {
		t.Fatal("finding or unsupported coverage lost")
	}
	reversed, err := diagnose.GroupCases(context.Background(), []string{b, a}, diagnose.DefaultConfig())
	if err != nil || !reflect.DeepEqual(result, reversed) {
		t.Fatal("input order changed grouping")
	}
}

func TestGroupsRefuseDuplicateUnreadableCancelledAndOverLimit(t *testing.T) {
	a := writeCase(t, fixture(t, "diagnose-reschedule.hl7"))
	for _, paths := range [][]string{nil, {a, a}, {a, "missing-private-path"}, make([]string, 17)} {
		r, err := diagnose.GroupCases(context.Background(), paths, diagnose.DefaultConfig())
		if err == nil || len(r.Cases) != 0 || len(r.Groups) != 0 {
			t.Fatalf("partial success: %+v %v", r, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := diagnose.GroupCases(ctx, []string{a}, diagnose.DefaultConfig()); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := diagnose.GroupCases(context.Background(), []string{a}, diagnose.DefaultConfig()); err != nil {
		t.Fatal("cancel prevented fresh retry", err)
	}
}

func TestGroupsSeparateDistinctOutcomes(t *testing.T) {
	raw := fixture(t, "diagnose-ack.hl7")
	// The wire outcomes are different failures under one rule, not one signature.
	a := writeCase(t, raw)
	b := writeCase(t, bytes.ReplaceAll(raw, []byte(`MSA|\X4145\|`), []byte("MSA|AR|")))
	result, err := diagnose.GroupCases(context.Background(), []string{a, b}, diagnose.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, g := range result.Groups {
		if g.RuleID == diagnose.ACKOutcome {
			count++
			if len(g.Members) != 1 {
				t.Fatal("distinct outcomes collapsed")
			}
		}
	}
	if count != 2 {
		t.Fatalf("wanted two outcome signatures, got %d", count)
	}
	data, err := diagnose.GroupsJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	markdown := diagnose.GroupsMarkdown(result)
	for _, secret := range []string{"DOE", "SYNTHETIC-FIXTURE", "SECRET"} {
		if bytes.Contains(data, []byte(secret)) || bytes.Contains(markdown, []byte(secret)) {
			t.Fatal("grouping leaked values")
		}
	}
	if !bytes.Contains(markdown, []byte("Representative comparison")) || !bytes.Contains(markdown, []byte("Unsupported coverage")) {
		t.Fatal("comparison or coverage missing")
	}
}

func TestGroupsKeepNullAndEmptyFieldsDistinctAndRetainEmptyCases(t *testing.T) {
	raw := fixture(t, "diagnose-missing-patient.hl7")
	a := writeCase(t, raw)
	b := writeCase(t, bytes.ReplaceAll(raw, []byte(`PID|1||""|`), []byte("PID|1|||")))
	c := writeCase(t, fixture(t, "diagnose-booking.hl7"))
	r, err := diagnose.GroupCases(context.Background(), []string{a, b, c}, diagnose.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, g := range r.Groups {
		if g.RuleID == diagnose.RequiredField {
			count++
			if len(g.Members) != 1 {
				t.Fatal("null and empty failures collapsed")
			}
		}
	}
	if count != 4 || len(r.Cases) != 3 {
		t.Fatalf("lost states or empty case: %+v", r)
	}
	noFindings := 0
	for _, c := range r.Cases {
		if c.NoFindings != "" {
			noFindings++
		}
	}
	if noFindings != 1 {
		t.Fatal("no-findings disclaimer lost")
	}
}
