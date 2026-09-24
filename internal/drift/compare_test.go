package drift_test

import (
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/drift"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runresult"
)

// The decision table is exercised here on evidence built in memory, so every
// rule is reached without writing, running or sending anything.

func target() *runresult.Target {
	record := replay.TargetRecord{Address: "127.0.0.1:2575", Transport: "plain", TestEndpoint: true, ConnectTimeout: "1s", MessageTimeout: "30s", MaxACKBytes: 4096}
	return &runresult.Target{Record: record, Identity: record.Identity()}
}

func pin(digest, engineVersion, spec, profile string) *runresult.Pin {
	return &runresult.Pin{Digest: digest, Document: &engine.Pin{Schema: engine.Schema, Engine: engineVersion, Spec: spec, Profile: profile}}
}

// job is a durable run that retained all four causes, with the bundled
// profile, so each case below changes one thing about it.
func job() *runresult.Evidence {
	return &runresult.Evidence{
		Family:   runresult.JobFamily,
		Identity: "result-identity",
		Input:    &runresult.Input{Identity: "case-identity"},
		Target:   target(),
		Pin:      pin("pin", "1.0.0", "readmit-test/v1", observation.Profile),
	}
}

func outcome(t *testing.T, report drift.Report, cause string) drift.Drift {
	t.Helper()
	if len(report.Drift) != 4 {
		t.Fatalf("a report states four causes: %+v", report.Drift)
	}
	for _, item := range report.Drift {
		if item.Cause == cause {
			return item
		}
	}
	t.Fatalf("no %s cause: %+v", cause, report.Drift)
	return drift.Drift{}
}

func TestEachCauseIsSettledFromWhatBothSidesRetained(t *testing.T) {
	semantic := func(outcome string, parts ...string) drift.Drift {
		return drift.Drift{Outcome: outcome, Comparison: drift.Semantic, Parts: append([]string{}, parts...)}
	}
	notCompared := func(outcome, reason string) drift.Drift {
		return drift.Drift{Outcome: outcome, Comparison: drift.NotCompared, Parts: []string{}, Reason: reason}
	}
	raw := func(outcome, reason string) drift.Drift {
		return drift.Drift{Outcome: outcome, Comparison: drift.RawDocument, Parts: []string{}, Reason: reason}
	}
	unreadable := &runresult.Pin{Digest: "later-pin"}
	cases := []struct {
		name  string
		cause string
		right func(*runresult.Evidence)
		left  func(*runresult.Evidence)
		want  drift.Drift
	}{
		// Input: declared, undeclared, and declared on one side.
		{"same input", drift.InputCause, nil, nil, semantic(drift.Unchanged)},
		{"another case", drift.InputCause, func(e *runresult.Evidence) { e.Input.Identity = "other-case" }, nil, semantic(drift.Changed, "source")},
		{"a declared operator", drift.InputCause, func(e *runresult.Evidence) {
			e.Input.Transformations = []replay.Transformation{{Name: "shift-timestamps", Shift: "24h"}}
			e.Input.Changes = 3
		}, nil, semantic(drift.Changed, "transformations", "recorded_changes")},
		{"the same operator with another parameter", drift.InputCause,
			func(e *runresult.Evidence) {
				e.Input.Transformations = []replay.Transformation{{Name: "shift-timestamps", Shift: "48h"}}
			},
			func(e *runresult.Evidence) {
				e.Input.Transformations = []replay.Transformation{{Name: "shift-timestamps", Shift: "24h"}}
			},
			semantic(drift.Changed, "transformations")},
		{"input retained by neither", drift.InputCause, func(e *runresult.Evidence) { e.Input = nil }, func(e *runresult.Evidence) { e.Input = nil }, notCompared(drift.Undeclared, "")},
		{"input retained by one side", drift.InputCause, func(e *runresult.Evidence) { e.Input = nil }, nil, notCompared(drift.Undecided, drift.DeclaredOnOneSide)},

		// Target: every part is named, and none is shown.
		{"same target", drift.TargetCause, nil, nil, semantic(drift.Unchanged)},
		{"every target part", drift.TargetCause, func(e *runresult.Evidence) {
			e.Target.Record = replay.TargetRecord{Address: "other", Transport: "tls", ApprovedTransport: true, CASHA256: "ca", ConnectTimeout: "2s", MessageTimeout: "5s", MaxACKBytes: 1}
		}, nil, semantic(drift.Changed, "address", "transport", "test_endpoint", "approved_transport", "ca_sha256", "connect_timeout", "message_timeout", "max_ack_bytes")},
		{"target retained by neither", drift.TargetCause, func(e *runresult.Evidence) { e.Target = nil }, func(e *runresult.Evidence) { e.Target = nil }, notCompared(drift.Undeclared, "")},
		{"target retained by one side", drift.TargetCause, nil, func(e *runresult.Evidence) { e.Target = nil }, notCompared(drift.Undecided, drift.DeclaredOnOneSide)},

		// Environment: declared, undeclared, one-sided and unreadable.
		{"same engine", drift.EnvironmentCause, nil, nil, semantic(drift.Unchanged)},
		{"another build", drift.EnvironmentCause, func(e *runresult.Evidence) { e.Pin = pin("other", "2.0.0", "readmit-test/v1", observation.Profile) }, nil, semantic(drift.Changed, "engine")},
		{"another spec contract", drift.EnvironmentCause, func(e *runresult.Evidence) { e.Pin = pin("other", "1.0.0", "readmit-test/v2", observation.Profile) }, nil, semantic(drift.Changed, "spec")},
		{"a changed profile is not the engine's", drift.EnvironmentCause, func(e *runresult.Evidence) { e.Pin = pin("other", "1.0.0", "readmit-test/v1", "readmit-lifecycle-v1") }, nil, semantic(drift.Unchanged)},
		{"pin retained by neither", drift.EnvironmentCause, func(e *runresult.Evidence) { e.Pin = nil }, func(e *runresult.Evidence) { e.Pin = nil }, notCompared(drift.Undeclared, "")},
		{"pin retained by one side", drift.EnvironmentCause, func(e *runresult.Evidence) { e.Pin = nil }, nil, notCompared(drift.Undecided, drift.DeclaredOnOneSide)},
		{"the same unreadable pin", drift.EnvironmentCause, func(e *runresult.Evidence) { e.Pin = unreadable }, func(e *runresult.Evidence) { e.Pin = unreadable }, raw(drift.Unchanged, "")},
		{"differing unreadable pins", drift.EnvironmentCause, func(e *runresult.Evidence) { e.Pin = unreadable }, func(e *runresult.Evidence) { e.Pin = &runresult.Pin{Digest: "another-later-pin"} }, raw(drift.Undecided, drift.RecordUnreadable)},
		{"an unreadable pin against a readable one", drift.EnvironmentCause, func(e *runresult.Evidence) { e.Pin = unreadable }, nil, raw(drift.Undecided, drift.RecordUnreadable)},

		// Rule: resolution decides whether equal names are equal rules.
		{"the bundled profile", drift.RuleCause, nil, nil, semantic(drift.Unchanged)},
		{"an equal unresolvable profile", drift.RuleCause,
			func(e *runresult.Evidence) { e.Pin = pin("pin", "1.0.0", "readmit-test/v1", "readmit-lifecycle-v1") },
			func(e *runresult.Evidence) { e.Pin = pin("pin", "1.0.0", "readmit-test/v1", "readmit-lifecycle-v1") },
			drift.Drift{Outcome: drift.Undecided, Comparison: drift.Semantic, Parts: []string{}, Reason: drift.ProfileUnresolved}},
		{"differing profiles, resolvable or not", drift.RuleCause, func(e *runresult.Evidence) { e.Pin = pin("other", "1.0.0", "readmit-test/v1", "readmit-cardiology-v1") }, nil, semantic(drift.Changed, "profile")},
		{"a rebuilt engine is not a changed rule", drift.RuleCause, func(e *runresult.Evidence) { e.Pin = pin("other", "2.0.0", "readmit-test/v1", observation.Profile) }, nil, semantic(drift.Unchanged)},
		{"rule retained by neither", drift.RuleCause, func(e *runresult.Evidence) { e.Pin = nil }, func(e *runresult.Evidence) { e.Pin = nil }, notCompared(drift.Undeclared, "")},
		{"rule retained by one side", drift.RuleCause, nil, func(e *runresult.Evidence) { e.Pin = nil }, notCompared(drift.Undecided, drift.DeclaredOnOneSide)},
		{"the same unreadable rule", drift.RuleCause, func(e *runresult.Evidence) { e.Pin = unreadable }, func(e *runresult.Evidence) { e.Pin = unreadable }, raw(drift.Unchanged, "")},
		{"differing unreadable rules", drift.RuleCause, func(e *runresult.Evidence) { e.Pin = unreadable }, func(e *runresult.Evidence) { e.Pin = &runresult.Pin{Digest: "another-later-pin"} }, raw(drift.Undecided, drift.RecordUnreadable)},
		{"an unreadable rule against a readable one", drift.RuleCause, nil, func(e *runresult.Evidence) { e.Pin = unreadable }, raw(drift.Undecided, drift.RecordUnreadable)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			left, right := job(), job()
			if test.left != nil {
				test.left(left)
			}
			if test.right != nil {
				test.right(right)
			}
			got := outcome(t, drift.CompareOpened(left, right), test.cause)
			test.want.Cause = test.cause
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

// Each side is stated from what it retained: its kind and identity, the
// operators it declared by name, its target fingerprint without the address,
// and what its pin names, or that the pin could not be read here.
func TestEachSideStatesWhatItRetained(t *testing.T) {
	left := job()
	left.Input.Transformations = []replay.Transformation{{Name: "shift-timestamps", Shift: "24h"}}
	left.Input.Changes = 2
	right := &runresult.Evidence{Family: runresult.CaseFamily, Identity: "case-identity", Input: &runresult.Input{Identity: "case-identity"}}
	report := drift.CompareOpened(left, right)
	wantLeft := drift.Side{
		Kind: drift.JobKind, Identity: "result-identity",
		Input:       drift.InputSide{State: drift.Declared, Identity: "case-identity", Transformations: []string{"shift-timestamps"}, RecordedChanges: 2},
		Target:      drift.TargetSide{State: drift.Declared, Fingerprint: left.Target.Identity, Revision: drift.UnknownRevision},
		Environment: drift.EnvironmentSide{State: drift.Declared, Fingerprint: "pin", Engine: "1.0.0", Spec: "readmit-test/v1"},
		Rule:        drift.RuleSide{State: drift.Declared, Fingerprint: "pin", Profile: observation.Profile, Resolution: drift.BundledProfile},
	}
	wantRight := drift.Side{
		Kind: drift.CaseKind, Identity: "case-identity",
		Input:       drift.InputSide{State: drift.Declared, Identity: "case-identity", Transformations: []string{}},
		Target:      drift.TargetSide{State: drift.Undeclared, Revision: drift.UnknownRevision},
		Environment: drift.EnvironmentSide{State: drift.Undeclared},
		Rule:        drift.RuleSide{State: drift.Undeclared},
	}
	if !reflect.DeepEqual(report.Left, wantLeft) || !reflect.DeepEqual(report.Right, wantRight) {
		t.Fatalf("sides:\n%+v\n%+v", report.Left, report.Right)
	}
	if report.Schema != drift.Schema || report.Scope == "" {
		t.Fatalf("report header: %q %q", report.Schema, report.Scope)
	}
	stopped := &runresult.Evidence{Family: runresult.JobFamily, Pin: &runresult.Pin{Digest: "later-pin"}}
	side := drift.CompareOpened(stopped, stopped).Left
	if side.Identity != "" || side.Input.State != drift.Undeclared || side.Environment != (drift.EnvironmentSide{State: drift.Unreadable, Fingerprint: "later-pin"}) || side.Rule != (drift.RuleSide{State: drift.Unreadable, Fingerprint: "later-pin"}) {
		t.Fatalf("a stopped job with an unreadable pin claims only its fingerprint: %+v", side)
	}
}

// The four outcomes together support one statement: no change, one cause,
// several causes, or nothing at all while any cause is unsettled.
func TestAttributionNamesEveryChangedCauseAndSettlesNothingUnsettled(t *testing.T) {
	unretained := func(e *runresult.Evidence) { e.Target, e.Pin = nil, nil }
	cases := []struct {
		name       string
		right      func(*runresult.Evidence)
		left       func(*runresult.Evidence)
		outcome    string
		changed    []string
		unresolved []string
	}{
		{"nothing changed", func(*runresult.Evidence) {}, nil, drift.NoDeclaredChange, []string{}, []string{}},
		{"one cause changed", func(e *runresult.Evidence) { e.Target.Record.Address = "other" }, nil, drift.SingleCause, []string{"target"}, []string{}},
		{"several causes changed", func(e *runresult.Evidence) {
			e.Input.Identity = "other-case"
			e.Pin = pin("other", "2.0.0", "readmit-test/v1", "readmit-cardiology-v1")
		}, nil, drift.SeveralCauses, []string{"input", "environment", "rule"}, []string{}},
		{"a change beside an unsettled cause", func(e *runresult.Evidence) {
			e.Input.Identity = "other-case"
			e.Target = nil
		}, nil, drift.Undecided, []string{"input"}, []string{"target"}},
		{"nothing retained beyond the input", unretained, unretained, drift.Undecided, []string{}, []string{"target", "environment", "rule"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			left, right := job(), job()
			test.right(right)
			if test.left != nil {
				test.left(left)
			}
			got := drift.CompareOpened(left, right).Attribution
			want := drift.Attribution{Outcome: test.outcome, Changed: test.changed, Unresolved: test.unresolved}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
}
