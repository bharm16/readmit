package hub

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/runnerprotocol"
)

func managedTestEntry() runnerprotocol.ScheduleEntry {
	return runnerprotocol.ScheduleEntry{Name: "Scheduling smoke", Suite: "Scheduling smoke", Version: "Version 4", Environment: "Scheduling QA", Runner: "QA runner",
		RunnerConfig: "/private/runner.json", Spec: "/private/suite.json", Input: strings.Repeat("a", 64),
		Repeat: runnerprotocol.RepeatDaily, Days: []string{}, At: "12:00", Zone: "UTC", WindowMinutes: 10, Route: ""}
}

type managedHarness struct {
	t        *testing.T
	dir      string
	executed []string
	reason   string
	onRun    func(key string)
	s        *ManagedScheduler
}

func newManagedHarness(t *testing.T, now time.Time) *managedHarness {
	h := &managedHarness{t: t, dir: filepath.Join(t.TempDir(), "scheduler-managed")}
	h.open(now)
	return h
}

func (h *managedHarness) open(now time.Time) {
	h.t.Helper()
	s, err := OpenManagedScheduler(h.dir, now, func(_ context.Context, _ runnerprotocol.ScheduleEntry, key string) string {
		h.executed = append(h.executed, key)
		if h.onRun != nil {
			h.onRun(key)
		}
		return "passed"
	}, func(context.Context, string, runnerprotocol.ScheduleEntry) string { return h.reason }, nil, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	h.s = s
	h.t.Cleanup(func() { s.Close() })
}

func (h *managedHarness) apply(c runnerprotocol.ScheduleCommand, now time.Time) (runnerprotocol.ScheduleAck, bool) {
	h.t.Helper()
	c.Schema = runnerprotocol.ScheduleCommandSchema
	ack, replay, err := h.s.Apply("alpha", c, now)
	if err != nil {
		h.t.Fatalf("%s refused: %v", c.Kind, err)
	}
	return ack, replay
}

func (h *managedHarness) create(id string, entry runnerprotocol.ScheduleEntry, enable bool, now time.Time) runnerprotocol.ScheduleAck {
	ack, _ := h.apply(runnerprotocol.ScheduleCommand{Intent: "create-" + id, Kind: runnerprotocol.CommandCreate, Schedule: id, Enable: enable, Entry: &entry}, now)
	return ack
}

func (h *managedHarness) states(id string) []string {
	out := []string{}
	for _, s := range h.s.List("alpha", time.Now()).Schedules {
		if s.ID == id {
			for i := len(s.Recent) - 1; i >= 0; i-- {
				out = append(out, s.Recent[i].State)
			}
		}
	}
	return out
}

func TestManagedCommandsArePersistedAcknowledgedOnceAndRevisionChecked(t *testing.T) {
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	h := newManagedHarness(t, now)
	entry := managedTestEntry()
	command := runnerprotocol.ScheduleCommand{Intent: "i-create", Kind: runnerprotocol.CommandCreate, Schedule: "smoke", Enable: true, Entry: &entry}
	first, replay := h.apply(command, now)
	if replay || first.State != "enabled" || first.Revision != 1 || first.Next == nil || !first.Next.Equal(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("create ack %+v", first)
	}
	// A double click sends the same intent again: the first acknowledgement.
	again, replay := h.apply(command, now.Add(time.Second))
	if !replay || again != first || len(h.s.List("alpha", now).Schedules) != 1 {
		t.Fatalf("repeated intent changed state: %+v", again)
	}
	// The same intent for other content, and a stale revision, are conflicts.
	other := entry
	other.At = "13:00"
	for _, bad := range []runnerprotocol.ScheduleCommand{
		{Schema: runnerprotocol.ScheduleCommandSchema, Intent: "i-create", Kind: runnerprotocol.CommandCreate, Schedule: "smoke", Enable: true, Entry: &other},
		{Schema: runnerprotocol.ScheduleCommandSchema, Intent: "i-pause", Kind: runnerprotocol.CommandPause, Schedule: "smoke", Expected: 7},
	} {
		if _, _, err := h.s.Apply("alpha", bad, now); !errors.Is(err, ErrScheduleConflict) {
			t.Fatalf("%s accepted: %v", bad.Intent, err)
		}
	}
	if _, _, err := h.s.Apply("beta", runnerprotocol.ScheduleCommand{Schema: runnerprotocol.ScheduleCommandSchema, Intent: "i-x", Kind: runnerprotocol.CommandPause, Schedule: "smoke", Expected: 1}, now); !errors.Is(err, ErrScheduleMissing) {
		t.Fatalf("another project's schedule was reachable: %v", err)
	}
	// A pure rename keeps the generation; a time change starts a new one.
	renamed := entry
	renamed.Name = "Nightly smoke"
	ack, _ := h.apply(runnerprotocol.ScheduleCommand{Intent: "i-rename", Kind: runnerprotocol.CommandUpdate, Schedule: "smoke", Expected: 1, Entry: &renamed}, now)
	if got := h.s.List("alpha", now).Schedules[0]; got.Generation != 1 || got.Revision != 2 || ack.Revision != 2 {
		t.Fatalf("rename changed the execution: %+v", got)
	}
	h.apply(runnerprotocol.ScheduleCommand{Intent: "i-retime", Kind: runnerprotocol.CommandUpdate, Schedule: "smoke", Expected: 2, Entry: &other}, now)
	if got := h.s.List("alpha", now).Schedules[0]; got.Generation != 2 {
		t.Fatalf("retime kept the generation: %+v", got)
	}
	paused, _ := h.apply(runnerprotocol.ScheduleCommand{Intent: "i-pause", Kind: runnerprotocol.CommandPause, Schedule: "smoke", Expected: 3}, now)
	if paused.State != "paused" || paused.Next != nil {
		t.Fatalf("pause ack %+v", paused)
	}
	// Acknowledged state survives a restart.
	h.s.Close()
	h.open(now)
	if got := h.s.List("alpha", now).Schedules[0]; got.State != "paused" || got.Revision != 4 || got.Entry.Name != "Nightly smoke" && got.Entry.At != "13:00" {
		t.Fatalf("restart lost the acknowledged state: %+v", got)
	}
	deleted, _ := h.apply(runnerprotocol.ScheduleCommand{Intent: "i-delete", Kind: runnerprotocol.CommandDelete, Schedule: "smoke", Expected: 4}, now)
	if deleted.State != "deleted" || len(h.s.List("alpha", now).Schedules) != 0 {
		t.Fatalf("delete ack %+v", deleted)
	}
}

func TestManagedDispatchIsOncePerSlotAcrossTicksRestartsAndWithoutAnyApplication(t *testing.T) {
	start := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	h := newManagedHarness(t, start)
	h.create("smoke", managedTestEntry(), true, start)
	due := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	for _, at := range []time.Time{due.Add(-time.Second), due, due.Add(time.Second), due.Add(2 * time.Second)} {
		if err := h.s.Tick(ctx, at); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.executed) != 1 || strings.Join(h.states("smoke"), ",") != "passed" {
		t.Fatalf("executed %v, states %v", h.executed, h.states("smoke"))
	}
	// A run interrupted by a stop is uncertain after restart, never repeated.
	h.onRun = func(string) { h.s.Close() }
	next := due.Add(24 * time.Hour)
	_ = h.s.Tick(ctx, next)
	h.onRun = nil
	h.open(next.Add(time.Minute))
	if err := h.s.Tick(ctx, next.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(h.executed) != 2 || strings.Join(h.states("smoke"), ",") != "passed,uncertain" {
		t.Fatalf("restart repeated or lost the slot: %v %v", h.executed, h.states("smoke"))
	}
}

func TestManagedPauseAndDeleteStopLaterDispatchWhileActiveRunsFinish(t *testing.T) {
	start := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	h := newManagedHarness(t, start)
	h.create("first", managedTestEntry(), true, start)
	h.create("second", managedTestEntry(), true, start)
	due := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	// While the first slot runs, both schedules are paused: the active run
	// finishes, and the second slot, claimed but not dispatched, never runs.
	h.onRun = func(string) {
		for _, id := range []string{"first", "second"} {
			if _, _, err := h.s.Apply("alpha", runnerprotocol.ScheduleCommand{Schema: runnerprotocol.ScheduleCommandSchema, Intent: "pause-" + id, Kind: runnerprotocol.CommandPause, Schedule: id, Expected: 1}, due); err != nil {
				t.Errorf("pause during a run was not acknowledged: %v", err)
			}
		}
	}
	if err := h.s.Tick(context.Background(), due); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(h.states("first"), ",") + "|" + strings.Join(h.states("second"), ",")
	if len(h.executed) != 1 || got != "passed|cancelled" && got != "cancelled|passed" {
		t.Fatalf("executed %v states %s", h.executed, got)
	}
	h.onRun = nil
	if err := h.s.Tick(context.Background(), due.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(h.executed) != 1 {
		t.Fatalf("a paused schedule dispatched: %v", h.executed)
	}
	// Enabling authorizes only later slots: the day it missed is not caught up.
	h.apply(runnerprotocol.ScheduleCommand{Intent: "enable-first", Kind: runnerprotocol.CommandEnable, Schedule: "first", Expected: 2}, due.Add(24*time.Hour+time.Hour))
	if err := h.s.Tick(context.Background(), due.Add(24*time.Hour+2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(h.executed) != 1 {
		t.Fatalf("enable caught up an old slot: %v", h.executed)
	}
	h.apply(runnerprotocol.ScheduleCommand{Intent: "delete-first", Kind: runnerprotocol.CommandDelete, Schedule: "first", Expected: 3}, due.Add(25*time.Hour))
	if err := h.s.Tick(context.Background(), due.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(h.executed) != 1 {
		t.Fatalf("a deleted schedule dispatched: %v", h.executed)
	}
}

func TestManagedDaylightSavingExpiredWindowAndRevalidation(t *testing.T) {
	ctx := context.Background()
	// Spring forward: 02:30 does not exist in New York on 2026-03-08.
	start := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	h := newManagedHarness(t, start)
	gap := managedTestEntry()
	gap.Zone, gap.At = "America/New_York", "02:30"
	h.create("gap", gap, true, start)
	if err := h.s.Tick(ctx, time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if states := strings.Join(h.states("gap"), ","); !strings.Contains(states, "skipped") || len(h.executed) != 0 {
		t.Fatalf("gap %s executed %v", states, h.executed)
	}
	// Fall back: 01:30 happens twice on 2026-11-01; it runs once.
	start = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	h = newManagedHarness(t, start)
	fold := managedTestEntry()
	fold.Zone, fold.At, fold.WindowMinutes = "America/New_York", "01:30", 720
	h.create("fold", fold, true, start)
	for _, at := range []time.Time{time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC), time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)} {
		if err := h.s.Tick(ctx, at); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.executed) != 1 {
		t.Fatalf("repeated local minute ran %d times", len(h.executed))
	}
	if recent := h.s.List("alpha", start).Schedules[0].Recent; recent[0].Offset != "-04:00" {
		t.Fatalf("offset not recorded: %+v", recent)
	}
	// A service that was down past the window records Missed and does not run.
	start = time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	h = newManagedHarness(t, start)
	h.create("late", managedTestEntry(), true, start)
	if err := h.s.Tick(ctx, time.Date(2026, 9, 30, 12, 30, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(h.states("late"), ",") != "missed" || len(h.executed) != 0 {
		t.Fatalf("expired window: %v %v", h.states("late"), h.executed)
	}
	// A changed pin refuses the run and pauses the schedule with its reason.
	h.create("pinned", managedTestEntry(), true, start)
	h.reason = runnerprotocol.ReasonPinChanged
	if err := h.s.Tick(ctx, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	for _, s := range h.s.List("alpha", start).Schedules {
		if s.ID == "pinned" && (s.State != "paused" || s.Reason != runnerprotocol.ReasonPinChanged || s.Recent[0].State != "refused") {
			t.Fatalf("changed pin: %+v", s)
		}
	}
	if len(h.executed) != 0 {
		t.Fatalf("a changed pin ran: %v", h.executed)
	}
}

func TestManagedAuthorityLossRefusesAndPauses(t *testing.T) {
	start := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), "scheduler-managed")
	ran := 0
	s, err := OpenManagedScheduler(dir, start, func(context.Context, runnerprotocol.ScheduleEntry, string) string { ran++; return "passed" },
		func(context.Context, string, runnerprotocol.ScheduleEntry) string { return "" }, nil, func(string, runnerprotocol.ScheduleEntry) error { return ErrSchedule })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entry := managedTestEntry()
	if _, _, err := s.Apply("alpha", runnerprotocol.ScheduleCommand{Schema: runnerprotocol.ScheduleCommandSchema, Intent: "c", Kind: runnerprotocol.CommandCreate, Schedule: "smoke", Enable: true, Entry: &entry}, start); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(context.Background(), time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	got := s.List("alpha", start).Schedules[0]
	if ran != 0 || got.State != "paused" || got.Reason != runnerprotocol.ReasonNoAuthority || got.Recent[0].State != "refused" {
		t.Fatalf("authority loss: ran %d %+v", ran, got)
	}
}

// A slot claimed before a Pause is not run after an Enable that followed it:
// enabling authorizes only later slots. A clock stepping back is not an error.
func TestManagedReenableDuringARunAndClockStepBack(t *testing.T) {
	start := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	h := newManagedHarness(t, start)
	h.create("first", managedTestEntry(), true, start)
	h.create("second", managedTestEntry(), true, start)
	due := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	h.onRun = func(string) {
		h.onRun = nil
		for _, c := range []runnerprotocol.ScheduleCommand{
			{Schema: runnerprotocol.ScheduleCommandSchema, Intent: "p", Kind: runnerprotocol.CommandPause, Schedule: "second", Expected: 1},
			{Schema: runnerprotocol.ScheduleCommandSchema, Intent: "e", Kind: runnerprotocol.CommandEnable, Schedule: "second", Expected: 2},
		} {
			if _, _, err := h.s.Apply("alpha", c, due); err != nil {
				t.Errorf("%s: %v", c.Kind, err)
			}
		}
	}
	if err := h.s.Tick(context.Background(), due); err != nil {
		t.Fatal(err)
	}
	if len(h.executed) != 1 {
		t.Fatalf("a slot claimed before the pause ran after the enable: %v", h.executed)
	}
	if err := h.s.Tick(context.Background(), due.Add(-time.Minute)); err != nil {
		t.Fatalf("a clock stepping back stopped the scheduler: %v", err)
	}
}
