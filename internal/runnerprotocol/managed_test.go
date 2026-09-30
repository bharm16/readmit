package runnerprotocol

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"
)

func managedEntry() ScheduleEntry {
	return ScheduleEntry{Name: "Scheduling smoke", Suite: "Scheduling smoke", Version: "Version 4", Environment: "Scheduling QA", Runner: "QA runner",
		RunnerConfig: "/etc/readmit-runner/config.json", Spec: "/srv/project/.readmit/scheduled/smoke/suite.json", Input: strings.Repeat("a", 64),
		Repeat: RepeatDaily, Days: []string{}, At: "02:30", Zone: "UTC", WindowMinutes: 30, Route: ""}
}

func TestNextOccurrencesFollowRecurrenceZoneAndDaylightSaving(t *testing.T) {
	weekdays := managedEntry()
	weekdays.Repeat = RepeatWeekdays
	// Friday 2026-10-02 03:00 UTC: the next three weekday 02:30s skip the weekend.
	got := NextOccurrences(weekdays, time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC), 3)
	want := []string{"2026-10-05T02:30:00Z", "2026-10-06T02:30:00Z", "2026-10-07T02:30:00Z"}
	for i, at := range got {
		if at.Format(time.RFC3339) != want[i] {
			t.Fatalf("weekdays: %v", got)
		}
	}
	days := managedEntry()
	days.Repeat, days.Days = RepeatDays, []string{"tue", "sat"}
	got = NextOccurrences(days, time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC), 2)
	if len(got) != 2 || got[0].Weekday() != time.Saturday || got[1].Weekday() != time.Tuesday {
		t.Fatalf("selected days: %v", got)
	}
	// 2026-03-08 02:30 does not exist in New York; it is no occurrence.
	gap := managedEntry()
	gap.Zone = "America/New_York"
	got = NextOccurrences(gap, time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC), 2)
	if got[0].In(mustZone(t, "America/New_York")).Format("2006-01-02") != "2026-03-09" {
		t.Fatalf("spring-forward minute became an occurrence: %v", got)
	}
	// 2026-11-01 01:30 happens twice in New York; it is one occurrence, the first.
	fold := managedEntry()
	fold.Zone, fold.At = "America/New_York", "01:30"
	got = NextOccurrences(fold, time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), 2)
	if got[0] != time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC) || got[1].Format("2006-01-02") != "2026-11-02" {
		t.Fatalf("repeated minute: %v", got)
	}
}

func mustZone(t *testing.T, name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestScheduleCommandsAreStrict(t *testing.T) {
	entry := managedEntry()
	good := ScheduleCommand{Schema: ScheduleCommandSchema, Intent: "i-1", Kind: CommandCreate, Schedule: "smoke", Enable: true, Entry: &entry}
	data, _ := json.Marshal(good)
	if _, err := DecodeScheduleCommand(data); err != nil {
		t.Fatalf("create refused: %v", err)
	}
	pause, _ := json.Marshal(ScheduleCommand{Schema: ScheduleCommandSchema, Intent: "i-2", Kind: CommandPause, Schedule: "smoke", Expected: 1})
	if _, err := DecodeScheduleCommand(pause); err != nil {
		t.Fatalf("pause refused: %v", err)
	}
	mutate := func(change func(*ScheduleCommand)) []byte {
		c, e := good, entry
		c.Entry = &e
		change(&c)
		b, _ := json.Marshal(c)
		return b
	}
	refused := map[string][]byte{
		"create expecting a revision": mutate(func(c *ScheduleCommand) { c.Expected = 3 }),
		"update enabling":             mutate(func(c *ScheduleCommand) { c.Kind, c.Expected = CommandUpdate, 1 }),
		"unknown kind":                mutate(func(c *ScheduleCommand) { c.Kind = "replay" }),
		"cron-like time":              mutate(func(c *ScheduleCommand) { c.Entry.At = "*/5" }),
		"local zone":                  mutate(func(c *ScheduleCommand) { c.Entry.Zone = "Local" }),
		"no selected day":             mutate(func(c *ScheduleCommand) { c.Entry.Repeat = RepeatDays }),
		"unordered days":              mutate(func(c *ScheduleCommand) { c.Entry.Repeat, c.Entry.Days = RepeatDays, []string{"sat", "mon"} }),
		"daily with days":             mutate(func(c *ScheduleCommand) { c.Entry.Days = []string{"mon"} }),
		"window too long":             mutate(func(c *ScheduleCommand) { c.Entry.WindowMinutes = 721 }),
		"plain route":                 mutate(func(c *ScheduleCommand) { c.Entry.Route = "http://alerts.example/" }),
		"route with path":             mutate(func(c *ScheduleCommand) { c.Entry.Route = "https://alerts.example/hook" }),
		"relative spec":               mutate(func(c *ScheduleCommand) { c.Entry.Spec = "suite.json" }),
		"short pin":                   mutate(func(c *ScheduleCommand) { c.Entry.Input = "abc" }),
		"pause with an entry":         mutate(func(c *ScheduleCommand) { c.Kind, c.Expected = CommandPause, 1 }),
		"unknown member":              []byte(strings.Replace(string(data), `"schema"`, `"extra":1,"schema"`, 1)),
		"missing entry member":        []byte(strings.Replace(string(data), `,"route":""`, ``, 1)),
	}
	for name, raw := range refused {
		if _, err := DecodeScheduleCommand(raw); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSameExecutionIgnoresOnlyTheName(t *testing.T) {
	a, b := managedEntry(), managedEntry()
	b.Name = "Renamed"
	if !a.SameExecution(b) {
		t.Fatal("a name change changed the execution")
	}
	b.At = "03:30"
	if a.SameExecution(b) {
		t.Fatal("a time change kept the execution")
	}
}
