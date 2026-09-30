package runnerprotocol

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/url"
	"path/filepath"
	"slices"
	"time"
	"unicode"
	"unicode/utf8"
)

// Managed schedules are the hub scheduler's own collection, changed only by
// acknowledged commands. They sit beside the operator-installed
// readmit-hub-schedules/v1 policy, which is unchanged.
const (
	ScheduleCommandSchema = "readmit-hub-schedule-command/v1"
	ScheduleAckSchema     = "readmit-hub-schedule-ack/v1"
	ScheduleListSchema    = "readmit-hub-schedule-list/v1"
)

// The recurrences a managed schedule can declare. There is no expression
// language: a schedule runs daily, on weekdays, or on the days it names.
const (
	RepeatDaily    = "daily"
	RepeatWeekdays = "weekdays"
	RepeatDays     = "days"
)

// Weekdays are the day names a selected-days schedule uses, Monday first.
var Weekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// ScheduleEntry is everything one schedule executes and when. The names are
// what a person chose them by; the paths and the input pin are what the
// runner host reads and revalidates before every run.
type ScheduleEntry struct {
	Name          string   `json:"name"`
	Suite         string   `json:"suite"`
	Version       string   `json:"version"`
	Environment   string   `json:"environment"`
	Runner        string   `json:"runner"`
	RunnerConfig  string   `json:"runner_config"`
	Spec          string   `json:"spec"`
	Input         string   `json:"input_sha256"`
	Repeat        string   `json:"repeat"`
	Days          []string `json:"days"`
	At            string   `json:"at"`
	Zone          string   `json:"zone"`
	WindowMinutes int      `json:"window_minutes"`
	Route         string   `json:"route"`
}

var entryMembers = []string{"name", "suite", "version", "environment", "runner", "runner_config", "spec", "input_sha256", "repeat", "days", "at", "zone", "window_minutes", "route"}

// ScheduleCommand is one change to one schedule. Intent identifies the
// submission: the same intent sent again is answered with the first
// acknowledgement and changes nothing. Expected is the schedule revision the
// change was made against (zero to create); any other revision is a conflict.
type ScheduleCommand struct {
	Schema   string         `json:"schema"`
	Intent   string         `json:"intent"`
	Kind     string         `json:"kind"`
	Schedule string         `json:"schedule"`
	Expected int            `json:"expected_revision"`
	Enable   bool           `json:"enable"`
	Entry    *ScheduleEntry `json:"entry,omitzero"`
}

// The command kinds.
const (
	CommandCreate = "create"
	CommandUpdate = "update"
	CommandEnable = "enable"
	CommandPause  = "pause"
	CommandDelete = "delete"
)

// ScheduleAck is the scheduler's acknowledgement of one persisted command:
// the revision and state it now holds, its next occurrence when enabled, and
// when it was acknowledged. Deleted schedules answer state "deleted".
type ScheduleAck struct {
	Schema       string     `json:"schema"`
	Intent       string     `json:"intent"`
	Schedule     string     `json:"schedule"`
	Revision     int        `json:"revision"`
	State        string     `json:"state"`
	Next         *time.Time `json:"next,omitzero"`
	Acknowledged time.Time  `json:"acknowledged_at"`
}

// ScheduleOccurrence is one recorded slot of a schedule: its key, the local
// day and offset it fell on, the generation of the schedule it was claimed
// under and what became of it.
type ScheduleOccurrence struct {
	Key          string    `json:"key"`
	Day          string    `json:"day"`
	Due          time.Time `json:"due"`
	Offset       string    `json:"offset"`
	Generation   int       `json:"generation"`
	State        string    `json:"state"`
	Notification string    `json:"notification"`
}

// ScheduleStatus is one schedule as the scheduler holds it now. Reason names
// why the scheduler paused it on its own, empty otherwise.
type ScheduleStatus struct {
	ID         string               `json:"id"`
	Revision   int                  `json:"revision"`
	Generation int                  `json:"generation"`
	State      string               `json:"state"`
	Reason     string               `json:"reason"`
	Entry      ScheduleEntry        `json:"entry"`
	Next       *time.Time           `json:"next,omitzero"`
	Updated    time.Time            `json:"updated_at"`
	Recent     []ScheduleOccurrence `json:"recent"`
}

// ScheduleList is one project's schedules as the scheduler answered them.
type ScheduleList struct {
	Schema    string           `json:"schema"`
	Project   string           `json:"project"`
	At        time.Time        `json:"at"`
	Schedules []ScheduleStatus `json:"schedules"`
}

// Reasons the scheduler pauses a schedule by itself before a run.
const (
	ReasonPinChanged  = "pin-changed"
	ReasonUnreadable  = "unreadable"
	ReasonNoAuthority = "no-authority"
)

func displayName(v string) bool {
	if v == "" || len(v) > 128 || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func cleanAbsolute(p string) bool {
	return filepath.IsAbs(p) && filepath.Clean(p) == p && len(p) <= 4096
}

// ValidRoute reports whether route is an approved notification destination:
// empty for none, or exactly an HTTPS origin.
func ValidRoute(route string) bool {
	if route == "" {
		return true
	}
	u, e := url.Parse(route)
	return e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.Path == "/" && u.RawPath == ""
}

// Validate applies every rule of one entry.
func (e ScheduleEntry) Validate() error {
	_, zoneErr := time.LoadLocation(e.Zone)
	clock, clockErr := time.Parse("15:04", e.At)
	if !displayName(e.Name) || !displayName(e.Suite) || !displayName(e.Version) || !displayName(e.Environment) || !displayName(e.Runner) ||
		!cleanAbsolute(e.RunnerConfig) || !cleanAbsolute(e.Spec) || !validDigest(e.Input) ||
		zoneErr != nil || e.Zone == "" || e.Zone == "Local" || clockErr != nil || clock.Format("15:04") != e.At ||
		e.WindowMinutes < 1 || e.WindowMinutes > 720 || !ValidRoute(e.Route) || e.Days == nil {
		return ErrInvalid
	}
	switch e.Repeat {
	case RepeatDaily, RepeatWeekdays:
		if len(e.Days) != 0 {
			return ErrInvalid
		}
	case RepeatDays:
		if len(e.Days) == 0 {
			return ErrInvalid
		}
		last := -1
		for _, d := range e.Days {
			at := slices.Index(Weekdays, d)
			if at <= last {
				return ErrInvalid
			}
			last = at
		}
	default:
		return ErrInvalid
	}
	return nil
}

// SameExecution reports whether two entries execute the same thing at the
// same times: everything but the schedule's own name.
func (e ScheduleEntry) SameExecution(o ScheduleEntry) bool {
	e.Name, o.Name = "", ""
	a, _ := json.Marshal(e, json.Deterministic(true))
	b, _ := json.Marshal(o, json.Deterministic(true))
	return string(a) == string(b)
}

// OnDay reports whether the schedule runs on the local calendar day given as
// 2006-01-02. A calendar date's weekday does not depend on the zone.
func (e ScheduleEntry) OnDay(day string) bool {
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		return false
	}
	name := Weekdays[(int(d.Weekday())+6)%7]
	switch e.Repeat {
	case RepeatDaily:
		return true
	case RepeatWeekdays:
		return d.Weekday() != time.Saturday && d.Weekday() != time.Sunday
	case RepeatDays:
		return slices.Contains(e.Days, name)
	}
	return false
}

// NextOccurrences answers the next n instants strictly after from at which
// the schedule's local time exists on a day it runs. A nonexistent
// spring-forward minute is not an occurrence; a repeated local minute is one
// occurrence, its first instant.
func NextOccurrences(e ScheduleEntry, from time.Time, n int) []time.Time {
	loc, err := time.LoadLocation(e.Zone)
	if err != nil || n <= 0 {
		return nil
	}
	out := []time.Time{}
	day := from.In(loc).Format("2006-01-02")
	for i := 0; i < 800 && len(out) < n; i++ {
		if e.OnDay(day) {
			if due, ok := DailyOccurrence(day, e.At, e.Zone); ok && due.After(from) {
				out = append(out, due)
			}
		}
		day = NextDay(day)
	}
	return out
}

// NextDay is the calendar day after day, both as 2006-01-02.
func NextDay(day string) string {
	d, _ := time.Parse("2006-01-02", day)
	return d.AddDate(0, 0, 1).Format("2006-01-02")
}

// DecodeScheduleCommand reads one command strictly. Create and update carry
// an entry; enable, pause and delete carry none.
func DecodeScheduleCommand(data []byte) (ScheduleCommand, error) {
	var c ScheduleCommand
	if len(data) > 64<<10 || json.Unmarshal(data, &c, json.RejectUnknownMembers(true)) != nil || c.Schema != ScheduleCommandSchema || !ID(c.Intent) || !ID(c.Schedule) || c.Expected < 0 {
		return c, ErrInvalid
	}
	members := []string{"schema", "intent", "kind", "schedule", "expected_revision", "enable"}
	switch c.Kind {
	case CommandCreate, CommandUpdate:
		if Exact(data, append(members, "entry")...) != nil || c.Entry == nil {
			return c, ErrInvalid
		}
		var raw struct {
			Entry jsontext.Value `json:"entry"`
		}
		if json.Unmarshal(data, &raw) != nil || Exact(raw.Entry, entryMembers...) != nil || c.Entry.Validate() != nil {
			return c, ErrInvalid
		}
		if c.Kind == CommandCreate && c.Expected != 0 || c.Kind == CommandUpdate && (c.Expected == 0 || c.Enable) {
			return c, ErrInvalid
		}
	case CommandEnable, CommandPause, CommandDelete:
		if Exact(data, members...) != nil || c.Entry != nil || c.Expected == 0 || c.Enable {
			return c, ErrInvalid
		}
	default:
		return c, ErrInvalid
	}
	return c, nil
}

// DecodeScheduleAck reads one acknowledgement strictly.
func DecodeScheduleAck(data []byte) (ScheduleAck, error) {
	var a ScheduleAck
	if len(data) > 4096 || json.Unmarshal(data, &a, json.RejectUnknownMembers(true)) != nil || a.Schema != ScheduleAckSchema || !ID(a.Intent) || !ID(a.Schedule) || a.Acknowledged.IsZero() {
		return a, ErrInvalid
	}
	switch a.State {
	case "enabled":
	case "paused", "deleted":
		if a.Next != nil {
			return a, ErrInvalid
		}
	default:
		return a, ErrInvalid
	}
	if a.State == "deleted" && a.Revision != 0 || a.State != "deleted" && a.Revision < 1 {
		return a, ErrInvalid
	}
	return a, nil
}

// ValidManagedState reports whether v is a state a managed occurrence record
// can hold: the policy file's states, "skipped" for a nonexistent local
// minute and "refused" for a run its revalidation stopped.
func ValidManagedState(v string) bool {
	switch v {
	case "claimed", "passed", "failed", "error", "cancelled", "uncertain", "missed", "skipped", "refused":
		return true
	}
	return false
}

// DecodeScheduleList reads one project's schedule list strictly.
func DecodeScheduleList(data []byte) (ScheduleList, error) {
	var l ScheduleList
	if len(data) > 8<<20 || json.Unmarshal(data, &l, json.RejectUnknownMembers(true)) != nil || l.Schema != ScheduleListSchema || !ID(l.Project) || l.At.IsZero() || l.Schedules == nil || len(l.Schedules) > 1024 {
		return l, ErrInvalid
	}
	for _, s := range l.Schedules {
		if !ID(s.ID) || s.Revision < 1 || s.Generation < 1 || s.State != "enabled" && s.State != "paused" || s.Entry.Validate() != nil || s.Recent == nil {
			return l, ErrInvalid
		}
		switch s.Reason {
		case "", ReasonPinChanged, ReasonUnreadable, ReasonNoAuthority:
		default:
			return l, ErrInvalid
		}
		for _, o := range s.Recent {
			if !ValidManagedState(o.State) || !ValidNotificationState(o.Notification) {
				return l, ErrInvalid
			}
		}
	}
	return l, nil
}
