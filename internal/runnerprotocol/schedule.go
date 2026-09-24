package runnerprotocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/url"
	"path"
	"time"
)

// validDigest accepts exactly the lowercase hex SHA-256 form every pin uses.
func validDigest(d string) bool {
	b, e := hex.DecodeString(d)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == d
}

// SchedulePolicy is the hub operator's installed recurring-regression authority,
// read identically by the hub service and by the application that prepares a
// revision of it. It contains only references; identifiers and paths remain
// private and never become notification content.
type SchedulePolicy struct {
	Schema      string     `json:"schema"`
	Concurrency string     `json:"concurrency"`
	Schedules   []Schedule `json:"schedules"`
}
type Schedule struct {
	ID            string `json:"id"`
	Zone          string `json:"zone"`
	At            string `json:"at"`
	WindowSeconds int    `json:"window_seconds"`
	Runner        string `json:"runner_config"`
	Spec          string `json:"spec"`
	Input         string `json:"input_sha256"`
	Route         string `json:"route"`
	Approved      bool   `json:"approved"`
}

// ScheduleConcurrency is the one execution discipline the contract defines.
// There is no second value to negotiate: serial execution, and a window missed
// while the scheduler was not running is recorded and skipped, never replayed.
const ScheduleConcurrency = "serial-skip-missed"

func DecodeSchedules(data []byte) (SchedulePolicy, error) {
	var p SchedulePolicy
	if len(data) > 1<<20 || Exact(data, "schema", "concurrency", "schedules") != nil || json.Unmarshal(data, &p, json.RejectUnknownMembers(true)) != nil || p.Schema != "readmit-hub-schedules/v1" || p.Concurrency != ScheduleConcurrency || len(p.Schedules) == 0 || len(p.Schedules) > 64 {
		return p, ErrInvalid
	}
	var raw struct {
		Schedules []jsontext.Value `json:"schedules"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return p, ErrInvalid
	}
	seen := map[string]bool{}
	for i, s := range p.Schedules {
		_, zoneErr := time.LoadLocation(s.Zone)
		clock, clockErr := time.Parse("15:04", s.At)
		if Exact(raw.Schedules[i], "id", "zone", "at", "window_seconds", "runner_config", "spec", "input_sha256", "route", "approved") != nil || !ID(s.ID) || seen[s.ID] || zoneErr != nil || s.Zone == "Local" || s.Zone == "" || clockErr != nil || clock.Format("15:04") != s.At || s.WindowSeconds < 1 || s.WindowSeconds > 3600 || !path.IsAbs(s.Runner) || !path.IsAbs(s.Spec) || !validDigest(s.Input) {
			return p, ErrInvalid
		}
		if s.Route != "" {
			u, e := url.Parse(s.Route)
			if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != "/" || u.RawPath != "" {
				return p, ErrInvalid
			}
		}
		if s.Approved && s.Route == "" {
			return p, ErrInvalid
		}
		seen[s.ID] = true
	}
	return p, nil
}

// DailyOccurrence selects the first UTC instant in a folded local minute.
// A nonexistent spring-forward minute is explicitly absent, never shifted.
func DailyOccurrence(day, clock, zone string) (time.Time, bool) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, false
	}
	d, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return time.Time{}, false
	}
	for t := d.Add(-3 * time.Hour); t.Before(d.Add(30 * time.Hour)); t = t.Add(time.Minute) {
		local := t.In(loc)
		if local.Format("2006-01-02") == day && local.Format("15:04") == clock {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

type ScheduleRecord struct {
	ID           string    `json:"id"`
	Schedule     string    `json:"schedule"`
	Day          string    `json:"day"`
	Due          time.Time `json:"due"`
	State        string    `json:"state"`
	Notification string    `json:"notification"`
}
type ScheduleHistory struct {
	Schema  string           `json:"schema"`
	Policy  string           `json:"policy_sha256"`
	Through time.Time        `json:"through"`
	Records []ScheduleRecord `json:"records"`
}

// SchedulePolicyIdentity is the exact identity the hub service binds its
// journal to. A changed identity stops admission rather than retaining
// authority from a stale in-memory configuration.
func SchedulePolicyIdentity(p SchedulePolicy) string {
	b, _ := json.Marshal(p, json.Deterministic(true))
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ScheduleAlert is the whole notification body an approved schedule may emit:
// fixed schema, the run's own state word, and a coverage word that never
// claims an assessment. This finite template cannot acquire names, paths,
// values, errors, digests, destinations or credential values from execution or
// retained evidence, and the application previews exactly these bytes.
func ScheduleAlert(state string) []byte {
	data, _ := json.Marshal(struct {
		Schema   string `json:"schema"`
		State    string `json:"state"`
		Coverage string `json:"coverage"`
	}{"readmit-hub-alert/v1", state, "not-assessed"})
	return data
}

// ScheduleAlertStates are the state words a scheduled record can carry, and
// the only words the alert template can name. ValidNotificationState reports
// whether a history notification word is one the contract defines.
func ScheduleAlertStates() []string {
	return []string{"claimed", "passed", "failed", "error", "cancelled", "uncertain", "missed", "dst-gap"}
}

func validScheduleState(v string) bool {
	switch v {
	case "claimed", "passed", "failed", "error", "cancelled", "uncertain", "missed", "dst-gap":
		return true
	}
	return false
}

// ValidScheduleState reports whether v is one of the record states the
// schedule history defines.
func ValidScheduleState(v string) bool { return validScheduleState(v) }

// ValidNotificationState reports whether v is one of the notification states
// the schedule history defines.
func ValidNotificationState(v string) bool {
	switch v {
	case "pending", "disabled", "sending", "sent", "uncertain":
		return true
	}
	return false
}
