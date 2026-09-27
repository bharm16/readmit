package diagnose_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/diagnose"
)

// Check answers exactly the profile-level items a diagnosis records first,
// from the manifest alone, so a profile the engine would not evaluate is
// known before anything runs.
func TestCheckAnswersTheItemsADiagnosisRecordsFirst(t *testing.T) {
	path := writeCase(t, fixture(t, "diagnose-booking.hl7"))
	unknown := diagnose.DefaultConfig()
	unknown.Profile, unknown.Ruleset = "customer-profile-v9", "customer-rules/v9"
	unknown.Rules = append(unknown.Rules, "customer.rule")
	mixed := diagnose.LifecycleConfig()
	mixed.Profile = diagnose.Profile
	for _, config := range []diagnose.Config{diagnose.DefaultConfig(), diagnose.LifecycleConfig(), diagnose.OrderConfig(), unknown, mixed} {
		items, err := diagnose.Check(path, config)
		if err != nil {
			t.Fatal(err)
		}
		report := run(t, path, config)
		if len(report.Unsupported) < len(items) || !slices.Equal(report.Unsupported[:len(items)], items) {
			t.Fatalf("%s: check answered %+v, the report records %+v", config.Ruleset, items, report.Unsupported)
		}
		refused := slices.ContainsFunc(items, diagnose.Refuses)
		if want := config.Ruleset != diagnose.DefaultConfig().Ruleset && config.Ruleset != diagnose.LifecycleConfig().Ruleset && config.Ruleset != diagnose.OrderConfig().Ruleset || config.Profile != profileOf(config.Ruleset); refused != want {
			t.Fatalf("%s/%s: refused %v, want %v", config.Profile, config.Ruleset, refused, want)
		}
	}
	invalid := diagnose.DefaultConfig()
	invalid.Rules = nil
	if _, err := diagnose.Check(path, invalid); err == nil {
		t.Fatal("an invalid configuration was checked")
	}
}

func profileOf(ruleset string) string {
	for _, config := range []diagnose.Config{diagnose.DefaultConfig(), diagnose.LifecycleConfig(), diagnose.OrderConfig()} {
		if config.Ruleset == ruleset {
			return config.Profile
		}
	}
	return ""
}

// A cancelled diagnosis answers the cancellation and no report.
func TestACancelledDiagnosisProducesNoReport(t *testing.T) {
	path := writeCase(t, fixture(t, "diagnose-booking.hl7"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if report, err := diagnose.RunContext(ctx, path, diagnose.DefaultConfig()); !errors.Is(err, context.Canceled) || report.Schema != "" {
		t.Fatalf("a cancelled diagnosis answered %+v, %v", report, err)
	}
}

// Grouping diagnoses already made is the grouping GroupCases makes.
func TestGroupingReportsIsGroupingTheirCases(t *testing.T) {
	first, second := writeCase(t, fixture(t, "diagnose-booking.hl7")), writeCase(t, fixture(t, "diagnose-booking.hl7"), fixture(t, "diagnose-reschedule.hl7"))
	grouped, err := diagnose.GroupCases(context.Background(), []string{first, second}, diagnose.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	again, err := diagnose.GroupReports(context.Background(), []diagnose.Report{run(t, second, diagnose.DefaultConfig()), run(t, first, diagnose.DefaultConfig())})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := diagnose.GroupsJSON(grouped)
	got, _ := diagnose.GroupsJSON(again)
	if string(want) != string(got) {
		t.Fatal("grouping the diagnoses differs from grouping their cases")
	}
	if _, err := diagnose.GroupReports(context.Background(), []diagnose.Report{grouped.Cases[0], grouped.Cases[0]}); err == nil {
		t.Fatal("one case was grouped twice")
	}
}

// Every rule of every ruleset has a human name and exactly the severity this
// release declares for it.
func TestEveryRuleHasANameAndItsDeclaredSeverity(t *testing.T) {
	declared := map[string]diagnose.Severity{
		diagnose.RequiredField:              diagnose.SeverityError,
		diagnose.LifecycleRequiredField:     diagnose.SeverityError,
		diagnose.OrderRequiredField:         diagnose.SeverityError,
		diagnose.EventTypeMismatch:          diagnose.SeverityError,
		diagnose.ACKOutcome:                 diagnose.SeverityError,
		diagnose.ACKError:                   diagnose.SeverityError,
		diagnose.DuplicateControl:           diagnose.SeverityWarning,
		diagnose.BookingNotObserved:         diagnose.SeverityWarning,
		diagnose.VisitNotObserved:           diagnose.SeverityWarning,
		diagnose.AppointmentNotObserved:     diagnose.SeverityWarning,
		diagnose.MergeIdentifierNotObserved: diagnose.SeverityWarning,
		diagnose.OrderNotObserved:           diagnose.SeverityWarning,
		diagnose.ACKStageNotObserved:        diagnose.SeverityWarning,
		diagnose.ACKErrorLocation:           diagnose.SeverityInfo,
		diagnose.DuplicateOutput:            diagnose.SeverityWarning,
		diagnose.StatusProgression:          diagnose.SeverityError,
	}
	rules := diagnose.Rules()
	for _, config := range []diagnose.Config{diagnose.DefaultConfig(), diagnose.LifecycleConfig(), diagnose.OrderConfig()} {
		for _, rule := range config.Rules {
			want, known := declared[rule]
			if !known {
				t.Fatalf("%s of %s has no declared severity in this test", rule, config.Ruleset)
			}
			if !slices.ContainsFunc(rules, func(info diagnose.RuleInfo) bool {
				return info.ID == rule && info.Ruleset == config.Ruleset && info.Name != "" && info.Severity == want && diagnose.RuleSeverity(rule) == want
			}) {
				t.Fatalf("%s of %s has no name, or not severity %q: %+v", rule, config.Ruleset, want, rules)
			}
		}
	}
}
