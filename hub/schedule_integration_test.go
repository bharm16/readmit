package hub_test

import (
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScheduleServiceWaitsForAdmissionCooldownAndStops(t *testing.T) {
	c := integrationConfig(t)
	certificates(t, &c)
	c.Listen = "127.0.0.1:0"
	db := testDatabase(t, c)
	reset(t, db)
	store := open(t, c)
	if e := store.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	access, _, _, _ := accessFixture(t)
	dir := t.TempDir()
	runnerPath := filepath.Join(dir, "runner-policy.json")
	raw, _ := json.Marshal(runnerprotocol.Policy{Schema: "readmit-runner-policy/v1", Runners: []runnerprotocol.Grant{}})
	if e := os.WriteFile(runnerPath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	due := time.Now().UTC().Truncate(time.Minute)
	policy := hub.SchedulePolicy{Schema: "readmit-hub-schedules/v1", Concurrency: "serial-skip-missed", Schedules: []hub.Schedule{{ID: "probe", Zone: "UTC", At: due.Format("15:04"), WindowSeconds: 3600, Runner: filepath.Join(dir, "absent-runner.json"), Spec: "/private/test.json", Input: strings.Repeat("a", 64)}}}
	policyPath := filepath.Join(dir, "schedule-policy.json")
	raw, _ = json.Marshal(policy)
	if e := os.WriteFile(policyPath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e := hub.InitializeSchedules(filepath.Join(c.Root, "scheduler"), policy, due.Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	clock := newHubClock()
	go func() { done <- store.ServeSchedulesWithClockForTest(ctx, access, runnerPath, policyPath, clock.now) }()
	history := func() hub.ScheduleHistory {
		t.Helper()
		b, e := os.ReadFile(filepath.Join(c.Root, "scheduler", "history.json"))
		if e != nil {
			t.Fatal(e)
		}
		var h hub.ScheduleHistory
		if e = json.Unmarshal(b, &h, json.RejectUnknownMembers(true)); e != nil {
			t.Fatal(e)
		}
		return h
	}
	select {
	case e := <-done:
		t.Fatal("service failed", e)
	case <-time.After(2 * time.Second):
	}
	if len(history().Records) != 0 {
		t.Fatal("restart cooldown consumed occurrence")
	}
	// Advancing the hub's clock ends the hold: the next ticks, well before the
	// host's clock would have ended it, process the occurrence.
	clock.advance(runnerHold)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current := history()
		if len(current.Records) == 1 && current.Records[0].State == "error" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	h := history()
	if len(h.Records) != 1 || h.Records[0].State != "error" {
		t.Fatalf("due occurrence not processed after readiness: %+v", h)
	}
	cancel()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("service did not stop")
	}
}
