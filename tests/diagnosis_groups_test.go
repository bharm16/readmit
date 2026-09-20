package tests

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diagnose"
)

func TestDiagnosisGroupsExecutableComparisonAndRefusals(t *testing.T) {
	a := diagnoseCase(t, "diagnose-reschedule.hl7")
	b := diagnoseCase(t, "diagnose-reschedule.hl7")
	c := diagnoseCase(t, "diagnose-missing-patient.hl7")
	output := filepath.Join(t.TempDir(), "groups")
	stdout, stderr, err := run(t, "diagnose", "groups", a, b, c, "--output", output)
	if err != nil {
		t.Fatalf("grouping: %v %s %s", err, stdout, stderr)
	}
	raw, err := os.ReadFile(filepath.Join(output, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report diagnose.GroupsReport
	if err := json.Unmarshal(raw, &report, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if len(report.Cases) != 3 || len(report.Groups) == 0 {
		t.Fatal("missing grouped results")
	}
	identities := map[string]bool{}
	for _, path := range []string{a, b} {
		opened, err := bundle.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		identities[opened.Identity] = true
	}
	recurring := 0
	memberCount := 0
	findingCount := 0
	for _, c := range report.Cases {
		findingCount += len(c.Findings)
	}
	for _, g := range report.Groups {
		memberCount += len(g.Members)
		if g.RuleID != diagnose.BookingNotObserved {
			continue
		}
		recurring++
		if len(g.Members) != 2 || len(g.Representatives) != 2 || len(g.Occurrences) != 2 {
			t.Fatalf("recurring evidence not retained: %+v", g)
		}
		for _, ref := range g.Members {
			if !identities[ref.CaseIdentity] || ref.FindingID != "f000001" {
				t.Fatalf("wrong member: %+v", ref)
			}
		}
		for _, ref := range g.Representatives {
			if !identities[ref.CaseIdentity] || ref.FindingID != "f000001" {
				t.Fatalf("wrong representative: %+v", ref)
			}
		}
		for _, ref := range g.Occurrences {
			if !identities[ref.CaseIdentity] || ref.Occurrence != "s0001-e000001" {
				t.Fatalf("wrong occurrence: %+v", ref)
			}
		}
	}
	if recurring != 1 || memberCount != findingCount {
		t.Fatal("grouping split a recurring signature or lost a finding")
	}
	markdown, err := os.ReadFile(filepath.Join(output, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Representative comparison", "All affected occurrences", "Unsupported coverage", "never population-wide rates"} {
		if !strings.Contains(string(markdown), text) {
			t.Fatalf("missing comparison: %s", text)
		}
	}
	for identity := range identities {
		if !strings.Contains(string(markdown), identity) {
			t.Fatal("representative case omitted from comparison")
		}
	}

	for _, path := range []string{a, b, c} {
		if _, err := bundle.Open(path); err != nil {
			t.Fatal("changed original evidence", err)
		}
	}
	for _, args := range [][]string{
		{a, b, "--output", output},
		{a, a, "--output", filepath.Join(t.TempDir(), "duplicate")},
		{a, "--output", filepath.Join(a, "nested")},
		{a, "--config", "", "--output", filepath.Join(t.TempDir(), "bad-config")},
		{a, "PRIVATE-MISSING", "--output", filepath.Join(t.TempDir(), "partial")},
		{a},
	} {
		out, errout, err := run(t, append([]string{"diagnose", "groups"}, args...)...)
		if err == nil || out != "" || strings.Contains(errout, "PRIVATE-MISSING") {
			t.Fatalf("unsafe refusal: %v %s %s", err, out, errout)
		}
	}
}
