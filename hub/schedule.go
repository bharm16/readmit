package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

var ErrSchedule = errors.New("schedule refused; inspect private configuration and retained state")

// The schedule contract is the shared strict protocol both the hub service and
// the application that prepares a revision of it read. The hub keeps its own
// names so its callers and tests are unchanged; there is one implementation.
type (
	SchedulePolicy  = runnerprotocol.SchedulePolicy
	Schedule        = runnerprotocol.Schedule
	ScheduleRecord  = runnerprotocol.ScheduleRecord
	ScheduleHistory = runnerprotocol.ScheduleHistory
)

func DecodeSchedules(data []byte) (SchedulePolicy, error) {
	return runnerprotocol.DecodeSchedules(data)
}

// DailyOccurrence selects the first UTC instant in a folded local minute.
// A nonexistent spring-forward minute is explicitly absent, never shifted.
func DailyOccurrence(day, clock, zone string) (time.Time, bool) {
	return runnerprotocol.DailyOccurrence(day, clock, zone)
}

// nextLocalDay starts from a UTC date anchor and scans actual instants. Local
// midnight itself can be missing or repeated, so normalizing it with Date or
// AddDate is not a reliable boundary. This marker is reporting time, never an
// executable occurrence of the skipped minute.
func nextLocalDay(day string, loc *time.Location) (time.Time, bool) {
	anchor, err := time.Parse("2006-01-02", day)
	if err != nil {
		return time.Time{}, false
	}
	for instant := anchor.Add(-26 * time.Hour); instant.Before(anchor.Add(50 * time.Hour)); instant = instant.Add(time.Minute) {
		if instant.In(loc).Format("2006-01-02") > day {
			return instant.UTC(), true
		}
	}
	return time.Time{}, false
}

// ScheduleExecutor executes a single typed local job with ordinary runner
// admission. It never receives notification transport or destination authority.
type ScheduleExecutor func(context.Context, Schedule, string) string

// ScheduleNotifier receives only an approved destination and a fixed summary.
type ScheduleNotifier func(context.Context, string, []byte) error

type Scheduler struct {
	mu        sync.Mutex
	root      *os.Root
	policy    SchedulePolicy
	history   ScheduleHistory
	execute   ScheduleExecutor
	notify    ScheduleNotifier
	poisoned  bool
	authority func() error
}

func policyHash(p SchedulePolicy) string { return runnerprotocol.SchedulePolicyIdentity(p) }

// InitializeSchedules is an explicit one-time operation. Missing state during
// service startup is never interpreted as permission to repeat old work.
func InitializeSchedules(directory string, p SchedulePolicy, now time.Time) error {
	b, _ := json.Marshal(p)
	if _, e := DecodeSchedules(b); e != nil || now.IsZero() {
		return ErrSchedule
	}
	if os.Mkdir(directory, 0700) != nil {
		return ErrSchedule
	}
	root, e := os.OpenRoot(directory)
	if e != nil {
		return ErrSchedule
	}
	defer root.Close()
	h := ScheduleHistory{Schema: "readmit-hub-schedule-history/v1", Policy: policyHash(p), Through: now.UTC(), Records: []ScheduleRecord{}}
	if e = writeScheduleHistory(root, h); e != nil {
		return e
	}
	parent, e := os.OpenRoot(filepath.Dir(directory))
	if e != nil {
		return ErrSchedule
	}
	defer parent.Close()
	if artifactdir.SyncDirectory(parent, ".") != nil {
		return durablerun.ErrSyncDirectory
	}
	return nil
}

// OpenScheduler requires the caller to hold the hub database lease for its
// entire lifetime. ServeSchedules enforces that service ownership boundary.
func OpenScheduler(directory string, p SchedulePolicy, execute ScheduleExecutor, notify ScheduleNotifier, authority ...func() error) (*Scheduler, error) {
	b, _ := json.Marshal(p)
	if _, e := DecodeSchedules(b); e != nil || execute == nil {
		return nil, ErrSchedule
	}
	info, e := os.Lstat(directory)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, ErrSchedule
	}
	root, e := os.OpenRoot(directory)
	if e != nil {
		return nil, ErrSchedule
	}
	fail := func() (*Scheduler, error) { root.Close(); return nil, ErrSchedule }
	data, e := readScheduleFile(root, "history.json", 8<<20)
	if e != nil {
		return fail()
	}
	var h ScheduleHistory
	if requireExactMembers(data, "schema", "policy_sha256", "through", "records") != nil || json.Unmarshal(data, &h, json.RejectUnknownMembers(true)) != nil || h.Schema != "readmit-hub-schedule-history/v1" || h.Policy != policyHash(p) || h.Through.IsZero() || len(h.Records) > 10000 {
		return fail()
	}
	var raw struct {
		Records []jsontext.Value `json:"records"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return fail()
	}
	seen := map[string]bool{}
	for i, r := range h.Records {
		if requireExactMembers(raw.Records[i], "id", "schedule", "day", "due", "state", "notification") != nil || !runnerprotocol.ID(r.ID) || seen[r.ID] || !runnerprotocol.ID(r.Schedule) || r.Due.IsZero() || !scheduleState(r.State) || !notificationState(r.Notification) {
			return fail()
		}
		matched := false
		for _, spec := range p.Schedules {
			if spec.ID != r.Schedule {
				continue
			}
			due, exists := DailyOccurrence(r.Day, spec.At, spec.Zone)
			if !exists {
				loc, _ := time.LoadLocation(spec.Zone)
				var ok bool
				due, ok = nextLocalDay(r.Day, loc)
				if !ok {
					return fail()
				}
			}
			hash := sha256.Sum256([]byte(h.Policy + "/" + spec.ID + "/" + r.Day))
			if r.ID != "s-"+hex.EncodeToString(hash[:24]) || !r.Due.Equal(due) || r.Due.After(h.Through) || !spec.Approved && r.Notification != "disabled" || !exists && r.State != "dst-gap" {
				return fail()
			}
			matched = true
		}
		if !matched {
			return fail()
		}
		seen[r.ID] = true
		if r.State == "claimed" {
			h.Records[i].State = "uncertain"
		}
		if r.Notification == "sending" {
			h.Records[i].Notification = "uncertain"
		}
	}
	if len(authority) > 1 || len(authority) == 1 && authority[0] == nil {
		return fail()
	}
	s := &Scheduler{root: root, policy: p, history: h, execute: execute, notify: notify}
	if len(authority) == 1 {
		s.authority = authority[0]
	}
	if e = s.save(); e != nil {
		return fail()
	}
	return s, nil
}
func (s *Scheduler) Close() error { return s.root.Close() }
func (s *Scheduler) History() ScheduleHistory {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.history
	h.Records = append([]ScheduleRecord{}, h.Records...)
	return h
}
func (s *Scheduler) save() error {
	if s.poisoned {
		return ErrSchedule
	}
	if writeScheduleHistory(s.root, s.history) != nil {
		s.poisoned = true
		return ErrSchedule
	}
	return nil
}
func scheduleState(v string) bool     { return runnerprotocol.ValidScheduleState(v) }
func notificationState(v string) bool { return runnerprotocol.ValidNotificationState(v) }
func readScheduleFile(root *os.Root, name string, limit int64) ([]byte, error) {
	return readPrivatePolicyRoot(root, name, limit)
}
func writeScheduleHistory(root *os.Root, h ScheduleHistory) error {
	b, e := json.Marshal(h)
	if e != nil || len(b) > 8<<20 {
		return ErrSchedule
	}
	return historyFile.ReplaceIn(root, "history.json", b)
}

// historyFile is how the schedule history is replaced, through the shared
// document store. A leftover temporary file is not authority; the committed
// file is. The store never overwrites through a symlink or reuses another
// writer's temporary file, and removes its own when a write fails.
var historyFile = artifactdir.Document{
	Staging: artifactdir.StagingName("history-next"),
	Errors: artifactdir.DocumentErrors{
		Create: ErrSchedule,
		Write:  ErrSchedule,
		Sync:   durablerun.ErrSyncDirectory,
	},
}

// Tick persists occurrence claims before dispatch. It serializes execution and
// skips expired windows instead of creating a catch-up burst. Restart leaves
// claimed work uncertain; only a new daily occurrence may execute automatically.
func (s *Scheduler) Tick(ctx context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	started := time.Now()
	now = now.UTC()
	if s.poisoned || now.Before(s.history.Through) || now.Sub(s.history.Through) > 366*24*time.Hour {
		return ErrSchedule
	}
	first := len(s.history.Records)
	for _, spec := range s.policy.Schedules {
		loc, _ := time.LoadLocation(spec.Zone)
		day := s.history.Through.In(loc).Format("2006-01-02")
		end := now.In(loc).Format("2006-01-02")
		for day <= end {
			due, exists := DailyOccurrence(day, spec.At, spec.Zone)
			if !exists {
				var ok bool
				due, ok = nextLocalDay(day, loc)
				if !ok {
					return ErrSchedule
				}
			}
			if due.After(s.history.Through) && !due.After(now) {
				if len(s.history.Records) >= 10000 {
					s.poisoned = true
					return ErrLimit
				}
				state := "claimed"
				if !exists {
					state = "dst-gap"
				} else if now.Sub(due) > time.Duration(spec.WindowSeconds)*time.Second {
					state = "missed"
				}
				hash := sha256.Sum256([]byte(s.history.Policy + "/" + spec.ID + "/" + day))
				id := "s-" + hex.EncodeToString(hash[:24])
				for _, old := range s.history.Records {
					if old.ID == id {
						return ErrSchedule
					}
				}
				notification := "disabled"
				if spec.Approved {
					notification = "pending"
				}
				s.history.Records = append(s.history.Records, ScheduleRecord{ID: "s-" + hex.EncodeToString(hash[:24]), Schedule: spec.ID, Day: day, Due: due, State: state, Notification: notification})
			}
			d, _ := time.Parse("2006-01-02", day)
			day = d.AddDate(0, 0, 1).Format("2006-01-02")
		}
	}
	s.history.Through = now
	// Idle polls advance the in-process cursor without rewriting the bounded
	// history every second. The last durable checkpoint already contains every
	// occurrence through that instant; restart safely scans the idle interval.
	if first != len(s.history.Records) {
		if e := s.save(); e != nil {
			return e
		}
	}
	for i := first; i < len(s.history.Records); i++ {
		r := &s.history.Records[i]
		if r.State != "claimed" {
			continue
		}
		var spec Schedule
		for _, candidate := range s.policy.Schedules {
			if candidate.ID == r.Schedule {
				spec = candidate
				break
			}
		}
		if ctx.Err() != nil {
			r.State = "cancelled"
		} else if time.Since(started) > time.Duration(spec.WindowSeconds)*time.Second || time.Since(started)+now.Sub(r.Due) > time.Duration(spec.WindowSeconds)*time.Second {
			r.State = "missed"
		} else {
			if s.authority != nil && s.authority() != nil {
				return ErrSchedule
			}
			r.State = s.execute(ctx, spec, r.ID)
			if !scheduleState(r.State) || r.State == "claimed" {
				r.State = "error"
			}
		}
		if e := s.save(); e != nil {
			return e
		}
	}
	for i := range s.history.Records {
		r := &s.history.Records[i]
		if r.Notification != "pending" || r.State == "claimed" {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var spec Schedule
		for _, candidate := range s.policy.Schedules {
			if candidate.ID == r.Schedule {
				spec = candidate
				break
			}
		}
		if !spec.Approved || s.notify == nil {
			continue
		}
		// The shared finite template cannot acquire names, paths, values, errors,
		// digests, destinations or credential values from execution or retained
		// evidence; the application previews exactly these bytes.
		data := runnerprotocol.ScheduleAlert(r.State)
		if s.authority != nil && s.authority() != nil {
			return ErrSchedule
		}
		r.Notification = "sending"
		if e := s.save(); e != nil {
			return e
		}
		e := s.notify(ctx, spec.Route, data)
		r.Notification = "uncertain"
		if e == nil {
			r.Notification = "sent"
		}
		if e = s.save(); e != nil {
			return e
		}
	}
	return nil
}

func scheduleDirectory(root string) string { return filepath.Join(root, "scheduler") }
func schedulePolicyFile(path string) (SchedulePolicy, error) {
	var p SchedulePolicy
	if !filepath.IsAbs(path) {
		return p, ErrSchedule
	}
	root, e := os.OpenRoot(filepath.Dir(path))
	if e != nil {
		return p, ErrSchedule
	}
	defer root.Close()
	b, e := readScheduleFile(root, filepath.Base(path), 1<<20)
	if e != nil {
		return p, e
	}
	return DecodeSchedules(b)
}
func (s *Store) InitializeSchedulePolicy(ctx context.Context, path string) error {
	if e := s.Ready(ctx); e != nil {
		return e
	}
	p, e := schedulePolicyFile(path)
	if e != nil {
		return e
	}
	return InitializeSchedules(scheduleDirectory(s.config.Root), p, time.Now())
}

// ScheduleInputIdentity prepares and hashes the exact inputs without credentials
// or execution. An operator approves this pin in the private schedule policy.
func ScheduleInputIdentity(spec string) (string, error) {
	return ScheduleInputIdentityContext(context.Background(), spec)
}

// ScheduleInputIdentityContext is the same offline reader for an application
// preview that the operator may cancel. Preparation has bounded reads; a
// cancellation is observed at each shared reader boundary before an identity
// can be returned.
func ScheduleInputIdentityContext(ctx context.Context, spec string) (string, error) {
	if e := ctx.Err(); e != nil {
		return "", e
	}
	p, e := durablerun.Prepare(spec)
	if e != nil {
		return "", ErrSchedule
	}
	if e := ctx.Err(); e != nil {
		return "", e
	}
	id, e := p.InputIdentity()
	if e != nil {
		return "", ErrSchedule
	}
	if e := ctx.Err(); e != nil {
		return "", e
	}
	return id, nil
}
