package runnerprotocol

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"
)

func schedulePolicy() SchedulePolicy {
	return SchedulePolicy{Schema: "readmit-hub-schedules/v1", Concurrency: ScheduleConcurrency, Schedules: []Schedule{{
		ID: "nightly", Zone: "UTC", At: "02:30", WindowSeconds: 600,
		Runner: "/etc/readmit-runner/config.json", Spec: "/srv/readmit/spec.json",
		Input: strings.Repeat("a", 64), Route: "https://alerts.example/", Approved: true,
	}}}
}

func TestDecodeSchedulesAcceptsExactPolicyAndRoundTrips(t *testing.T) {
	raw, err := json.Marshal(schedulePolicy())
	if err != nil {
		t.Fatal(err)
	}
	p, err := DecodeSchedules(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Schedules) != 1 || p.Schedules[0].ID != "nightly" || p.Schedules[0].Route != "https://alerts.example/" || !p.Schedules[0].Approved {
		t.Fatalf("unexpected policy %+v", p)
	}
}

func TestDecodeSchedulesRefusesUnknownMembersAndBadPins(t *testing.T) {
	base, _ := json.Marshal(schedulePolicy())
	var loose map[string]any
	if json.Unmarshal(base, &loose) != nil {
		t.Fatal("fixture")
	}
	loose["extra"] = 1
	withExtra, _ := json.Marshal(loose)
	cases := map[string][]byte{
		"unknown member":      withExtra,
		"other concurrency":   []byte(`{"schema":"readmit-hub-schedules/v1","concurrency":"parallel","schedules":[]}`),
		"no schedules":        []byte(`{"schema":"readmit-hub-schedules/v1","concurrency":"serial-skip-missed","schedules":[]}`),
		"approved no route":   []byte(`{"schema":"readmit-hub-schedules/v1","concurrency":"serial-skip-missed","schedules":[{"id":"s","zone":"UTC","at":"02:30","window_seconds":60,"runner_config":"/r","spec":"/s","input_sha256":"` + strings.Repeat("a", 64) + `","route":"","approved":true}]}`),
		"non-http route":      []byte(`{"schema":"readmit-hub-schedules/v1","concurrency":"serial-skip-missed","schedules":[{"id":"s","zone":"UTC","at":"02:30","window_seconds":60,"runner_config":"/r","spec":"/s","input_sha256":"` + strings.Repeat("a", 64) + `","route":"http://alerts.example/","approved":true}]}`),
		"relative runner":     []byte(`{"schema":"readmit-hub-schedules/v1","concurrency":"serial-skip-missed","schedules":[{"id":"s","zone":"UTC","at":"02:30","window_seconds":60,"runner_config":"r","spec":"/s","input_sha256":"` + strings.Repeat("a", 64) + `","route":"","approved":false}]}`),
		"short pin":           []byte(`{"schema":"readmit-hub-schedules/v1","concurrency":"serial-skip-missed","schedules":[{"id":"s","zone":"UTC","at":"02:30","window_seconds":60,"runner_config":"/r","spec":"/s","input_sha256":"abc","route":"","approved":false}]}`),
		"nonexistent zone":    []byte(`{"schema":"readmit-hub-schedules/v1","concurrency":"serial-skip-missed","schedules":[{"id":"s","zone":"Mars/Olympus","at":"02:30","window_seconds":60,"runner_config":"/r","spec":"/s","input_sha256":"` + strings.Repeat("a", 64) + `","route":"","approved":false}]}`),
		"local zone":          []byte(`{"schema":"readmit-hub-schedules/v1","concurrency":"serial-skip-missed","schedules":[{"id":"s","zone":"Local","at":"02:30","window_seconds":60,"runner_config":"/r","spec":"/s","input_sha256":"` + strings.Repeat("a", 64) + `","route":"","approved":false}]}`),
		"bad clock":           []byte(`{"schema":"readmit-hub-schedules/v1","concurrency":"serial-skip-missed","schedules":[{"id":"s","zone":"UTC","at":"2:30","window_seconds":60,"runner_config":"/r","spec":"/s","input_sha256":"` + strings.Repeat("a", 64) + `","route":"","approved":false}]}`),
		"window zero":         []byte(`{"schema":"readmit-hub-schedules/v1","concurrency":"serial-skip-missed","schedules":[{"id":"s","zone":"UTC","at":"02:30","window_seconds":0,"runner_config":"/r","spec":"/s","input_sha256":"` + strings.Repeat("a", 64) + `","route":"","approved":false}]}`),
		"duplicate schedule":  []byte(`{"schema":"readmit-hub-schedules/v1","concurrency":"serial-skip-missed","schedules":[{"id":"s","zone":"UTC","at":"02:30","window_seconds":60,"runner_config":"/r","spec":"/s","input_sha256":"` + strings.Repeat("a", 64) + `","route":"","approved":false},{"id":"s","zone":"UTC","at":"03:30","window_seconds":60,"runner_config":"/r","spec":"/s","input_sha256":"` + strings.Repeat("a", 64) + `","route":"","approved":false}]}`),
		"null nested member":  []byte(`{"schema":"readmit-hub-schedules/v1","concurrency":"serial-skip-missed","schedules":[{"id":"s","zone":null,"at":"02:30","window_seconds":60,"runner_config":"/r","spec":"/s","input_sha256":"` + strings.Repeat("a", 64) + `","route":"","approved":false}]}`),
		"missing nested name": []byte(`{"schema":"readmit-hub-schedules/v1","concurrency":"serial-skip-missed","schedules":[{"id":"s","zone":"UTC","window_seconds":60,"runner_config":"/r","spec":"/s","input_sha256":"` + strings.Repeat("a", 64) + `","route":"","approved":false}]}`),
	}
	for name, raw := range cases {
		if _, err := DecodeSchedules(raw); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

func TestDailyOccurrenceFoldsAndRefusesGapMinute(t *testing.T) {
	due, ok := DailyOccurrence("2026-01-01", "02:30", "UTC")
	if !ok || due != time.Date(2026, 1, 1, 2, 30, 0, 0, time.UTC) {
		t.Fatalf("utc occurrence: %v %v", due, ok)
	}
	// 02:30 local does not exist on the United States spring-forward day.
	if _, ok := DailyOccurrence("2026-03-08", "02:30", "America/New_York"); ok {
		t.Fatal("nonexistent minute reported as present")
	}
	// The repeated autumn minute folds to its first instant.
	folded, ok := DailyOccurrence("2026-11-01", "01:30", "America/New_York")
	if !ok {
		t.Fatal("folded minute absent")
	}
	if want := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC); !folded.Equal(want) {
		t.Fatalf("folded occurrence: got %v want %v", folded.UTC(), want)
	}
}

func TestSchedulePolicyIdentityIsDeterministicAndDiscriminating(t *testing.T) {
	p := schedulePolicy()
	if SchedulePolicyIdentity(p) != SchedulePolicyIdentity(p) {
		t.Fatal("identity not deterministic")
	}
	changed := SchedulePolicy{Schema: p.Schema, Concurrency: p.Concurrency, Schedules: append([]Schedule(nil), p.Schedules...)}
	changed.Schedules[0].At = "02:31"
	if SchedulePolicyIdentity(p) == SchedulePolicyIdentity(changed) {
		t.Fatal("different policies share an identity")
	}
	// The identity is over the deterministic encoding, so key order in a
	// hand-inspected document cannot change what the hub service binds to.
	reordered := []byte(`{"schedules":[{"approved":true,"at":"02:30","id":"nightly","input_sha256":"` +
		strings.Repeat("a", 64) + `","route":"https://alerts.example/","runner_config":"/etc/readmit-runner/config.json","spec":"/srv/readmit/spec.json","window_seconds":600,"zone":"UTC"}],"concurrency":"serial-skip-missed","schema":"readmit-hub-schedules/v1"}`)
	other, err := DecodeSchedules(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if SchedulePolicyIdentity(p) != SchedulePolicyIdentity(other) {
		t.Fatal("identity depends on member order")
	}
}

func TestScheduleAlertIsTheFixedNotificationBody(t *testing.T) {
	if got, want := string(ScheduleAlert("failed")), `{"schema":"readmit-hub-alert/v1","state":"failed","coverage":"not-assessed"}`; got != want {
		t.Fatalf("alert body: got %s want %s", got, want)
	}
	for _, state := range ScheduleAlertStates() {
		if !ValidScheduleState(state) {
			t.Fatalf("vocabulary state %q not valid", state)
		}
	}
	if ValidScheduleState("delivered") || ValidNotificationState("delivered") {
		t.Fatal("invented states accepted")
	}
}
