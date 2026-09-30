package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

// Managed schedules are changed only by acknowledged commands. The scheduler
// persists every command before acknowledging it and every occurrence claim
// before dispatching it, and serializes both with dispatch decisions, so an
// acknowledged Pause or Delete is seen by every later dispatch.

var (
	// ErrScheduleConflict answers a command made against another revision, a
	// create of an existing schedule, or an intent reused for other content.
	ErrScheduleConflict = errors.New("schedule changed since it was read")
	// ErrScheduleMissing answers a command naming no schedule of the project.
	ErrScheduleMissing = errors.New("no such schedule")
)

const managedStateSchema = "readmit-hub-schedule-state/v1"

// managedLimit bounds the records, intents and schedules the state holds.
const (
	maxManagedRecords   = 10000
	maxManagedIntents   = 1024
	maxManagedSchedules = 1024
	managedRecent       = 5
)

type managedSchedule struct {
	Project    string                       `json:"project"`
	ID         string                       `json:"id"`
	Revision   int                          `json:"revision"`
	Generation int                          `json:"generation"`
	State      string                       `json:"state"`
	Reason     string                       `json:"reason"`
	Since      time.Time                    `json:"since"`
	Updated    time.Time                    `json:"updated_at"`
	Entry      runnerprotocol.ScheduleEntry `json:"entry"`
}

type managedRecord struct {
	Project                           string `json:"project"`
	runnerprotocol.ScheduleOccurrence `json:",inline"`
	Schedule                          string `json:"schedule"`
}

type managedIntent struct {
	Project string                     `json:"project"`
	Intent  string                     `json:"intent"`
	Digest  string                     `json:"digest"`
	Ack     runnerprotocol.ScheduleAck `json:"ack"`
}

type managedState struct {
	Schema    string            `json:"schema"`
	Through   time.Time         `json:"through"`
	Schedules []managedSchedule `json:"schedules"`
	Records   []managedRecord   `json:"records"`
	Intents   []managedIntent   `json:"intents"`
}

// ManagedExecutor runs one occurrence and answers its record state.
type ManagedExecutor func(context.Context, runnerprotocol.ScheduleEntry, string) string

// ManagedRevalidator checks, before every run, that the runner host can read
// the schedule's runner configuration and that the suite still prepares to
// the pinned input. It answers "" or a reason the schedule is paused for.
type ManagedRevalidator func(ctx context.Context, project string, entry runnerprotocol.ScheduleEntry) string

type ManagedScheduler struct {
	mu         sync.Mutex
	root       *os.Root
	state      managedState
	execute    ManagedExecutor
	revalidate ManagedRevalidator
	notify     ScheduleNotifier
	authority  ManagedAuthority
	poisoned   bool
}

// ManagedAuthority answers whether current grants and license admit one
// schedule of one project now; nil admits.
type ManagedAuthority func(project string, entry runnerprotocol.ScheduleEntry) error

var managedFile = artifactdir.Document{
	Staging: artifactdir.StagingName("state-next"),
	Errors:  artifactdir.DocumentErrors{Create: ErrSchedule, Write: ErrSchedule, Sync: durablerun.ErrSyncDirectory},
}

func managedDirectory(root string) string { return filepath.Join(root, "scheduler-managed") }

// OpenManagedScheduler opens, or creates empty, the managed schedule state. An
// empty state holds no schedule, so creating it can never repeat old work. A
// claim left by a stopped service becomes uncertain and is never dispatched
// again; a notification left sending becomes uncertain and is not resent.
func OpenManagedScheduler(directory string, now time.Time, execute ManagedExecutor, revalidate ManagedRevalidator, notify ScheduleNotifier, authority ManagedAuthority) (*ManagedScheduler, error) {
	if execute == nil || revalidate == nil || now.IsZero() {
		return nil, ErrSchedule
	}
	if _, e := os.Lstat(directory); errors.Is(e, os.ErrNotExist) {
		if os.Mkdir(directory, 0700) != nil {
			return nil, ErrSchedule
		}
	}
	info, e := os.Lstat(directory)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, ErrSchedule
	}
	root, e := os.OpenRoot(directory)
	if e != nil {
		return nil, ErrSchedule
	}
	s := &ManagedScheduler{root: root, execute: execute, revalidate: revalidate, notify: notify, authority: authority}
	data, e := readPrivatePolicyRoot(root, "state.json", 16<<20)
	switch {
	case errors.Is(e, os.ErrNotExist) || e != nil && !fileExists(root, "state.json"):
		s.state = managedState{Schema: managedStateSchema, Through: now.UTC(), Schedules: []managedSchedule{}, Records: []managedRecord{}, Intents: []managedIntent{}}
	case e != nil:
		root.Close()
		return nil, ErrSchedule
	default:
		if json.Unmarshal(data, &s.state, json.RejectUnknownMembers(true)) != nil || !s.state.valid() {
			root.Close()
			return nil, ErrSchedule
		}
	}
	for i := range s.state.Records {
		r := &s.state.Records[i]
		if r.State == "claimed" {
			r.State = "uncertain"
		}
		if r.Notification == "sending" {
			r.Notification = "uncertain"
		}
	}
	if e = s.save(); e != nil {
		root.Close()
		return nil, e
	}
	return s, nil
}

func fileExists(root *os.Root, name string) bool {
	_, e := root.Lstat(name)
	return e == nil
}

func (st managedState) valid() bool {
	if st.Schema != managedStateSchema || st.Through.IsZero() || st.Schedules == nil || st.Records == nil || st.Intents == nil ||
		len(st.Schedules) > maxManagedSchedules || len(st.Records) > maxManagedRecords || len(st.Intents) > maxManagedIntents {
		return false
	}
	seen := map[string]bool{}
	for _, s := range st.Schedules {
		key := s.Project + "/" + s.ID
		if !validProject(s.Project) || !runnerprotocol.ID(s.ID) || seen[key] || s.Revision < 1 || s.Generation < 1 ||
			s.State != "enabled" && s.State != "paused" || s.Entry.Validate() != nil || s.Since.IsZero() {
			return false
		}
		seen[key] = true
	}
	keys := map[string]bool{}
	for _, r := range st.Records {
		if keys[r.Key] || !runnerprotocol.ValidManagedState(r.State) || !runnerprotocol.ValidNotificationState(r.Notification) || r.Due.IsZero() {
			return false
		}
		keys[r.Key] = true
	}
	return true
}

func (s *ManagedScheduler) Close() error { return s.root.Close() }

func (s *ManagedScheduler) save() error {
	if s.poisoned {
		return ErrSchedule
	}
	b, e := json.Marshal(s.state)
	if e != nil || len(b) > 16<<20 || managedFile.ReplaceIn(s.root, "state.json", b) != nil {
		s.poisoned = true
		return ErrSchedule
	}
	return nil
}

func (s *ManagedScheduler) find(project, id string) int {
	return slices.IndexFunc(s.state.Schedules, func(m managedSchedule) bool { return m.Project == project && m.ID == id })
}

func next(entry runnerprotocol.ScheduleEntry, state string, now time.Time) *time.Time {
	if state != "enabled" {
		return nil
	}
	if at := runnerprotocol.NextOccurrences(entry, now, 1); len(at) == 1 {
		return &at[0]
	}
	return nil
}

func commandDigest(c runnerprotocol.ScheduleCommand) string {
	b, _ := json.Marshal(c, json.Deterministic(true))
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Apply persists one command and answers its acknowledgement. The same intent
// with the same content answers the first acknowledgement and replay true;
// nothing is changed twice.
func (s *ManagedScheduler) Apply(project string, c runnerprotocol.ScheduleCommand, now time.Time) (runnerprotocol.ScheduleAck, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now = now.UTC()
	var zero runnerprotocol.ScheduleAck
	if s.poisoned {
		return zero, false, ErrSchedule
	}
	digest := commandDigest(c)
	for _, held := range s.state.Intents {
		if held.Project == project && held.Intent == c.Intent {
			if held.Digest != digest {
				return zero, false, ErrScheduleConflict
			}
			return held.Ack, true, nil
		}
	}
	at := s.find(project, c.Schedule)
	ack := runnerprotocol.ScheduleAck{Schema: runnerprotocol.ScheduleAckSchema, Intent: c.Intent, Schedule: c.Schedule, Acknowledged: now}
	if c.Kind == runnerprotocol.CommandCreate {
		if at >= 0 {
			return zero, false, ErrScheduleConflict
		}
		if len(s.state.Schedules) >= maxManagedSchedules {
			return zero, false, ErrLimit
		}
		state := "paused"
		if c.Enable {
			state = "enabled"
		}
		s.state.Schedules = append(s.state.Schedules, managedSchedule{Project: project, ID: c.Schedule, Revision: 1, Generation: 1, State: state, Since: now, Updated: now, Entry: *c.Entry})
		ack.Revision, ack.State, ack.Next = 1, state, next(*c.Entry, state, now)
	} else {
		if at < 0 {
			return zero, false, ErrScheduleMissing
		}
		held := &s.state.Schedules[at]
		if held.Revision != c.Expected {
			return zero, false, ErrScheduleConflict
		}
		switch c.Kind {
		case runnerprotocol.CommandUpdate:
			if !held.Entry.SameExecution(*c.Entry) {
				held.Generation++
			}
			held.Entry = *c.Entry
		case runnerprotocol.CommandEnable:
			// Enabling authorizes only occurrences after this acknowledgement.
			held.State, held.Reason, held.Since = "enabled", "", now
		case runnerprotocol.CommandPause:
			held.State = "paused"
		}
		held.Revision++
		held.Updated = now
		ack.Revision, ack.State, ack.Next = held.Revision, held.State, next(held.Entry, held.State, now)
		if c.Kind == runnerprotocol.CommandDelete {
			s.state.Schedules = slices.Delete(s.state.Schedules, at, at+1)
			ack.Revision, ack.State, ack.Next = 0, "deleted", nil
		}
	}
	s.state.Intents = append(s.state.Intents, managedIntent{Project: project, Intent: c.Intent, Digest: digest, Ack: ack})
	if len(s.state.Intents) > maxManagedIntents {
		s.state.Intents = s.state.Intents[len(s.state.Intents)-maxManagedIntents:]
	}
	if e := s.save(); e != nil {
		return zero, false, e
	}
	return ack, false, nil
}

// List answers one project's schedules as the scheduler holds them now.
func (s *ManagedScheduler) List(project string, now time.Time) runnerprotocol.ScheduleList {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := runnerprotocol.ScheduleList{Schema: runnerprotocol.ScheduleListSchema, Project: project, At: now.UTC(), Schedules: []runnerprotocol.ScheduleStatus{}}
	for _, m := range s.state.Schedules {
		if m.Project != project {
			continue
		}
		recent := []runnerprotocol.ScheduleOccurrence{}
		for i := len(s.state.Records) - 1; i >= 0 && len(recent) < managedRecent; i-- {
			if r := s.state.Records[i]; r.Project == project && r.Schedule == m.ID {
				recent = append(recent, r.ScheduleOccurrence)
			}
		}
		out.Schedules = append(out.Schedules, runnerprotocol.ScheduleStatus{ID: m.ID, Revision: m.Revision, Generation: m.Generation, State: m.State, Reason: m.Reason,
			Entry: m.Entry, Next: next(m.Entry, m.State, now), Updated: m.Updated, Recent: recent})
	}
	return out
}

func occurrenceKey(project, id, day string) string {
	h := sha256.Sum256([]byte(project + "/" + id + "/" + day))
	return "o-" + hex.EncodeToString(h[:24])
}

// claim records every slot due since the last tick for every enabled
// schedule, and answers the keys to dispatch. A slot already recorded under
// any generation is never recorded again; a slot past its window is missed; a
// nonexistent local minute is skipped.
func (s *ManagedScheduler) claim(now time.Time) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poisoned {
		return nil, ErrSchedule
	}
	if now.Before(s.state.Through) {
		// The clock stepped back: nothing is due that was not already
		// considered, and the cursor never moves backwards.
		return nil, nil
	}
	recorded := map[string]bool{}
	for _, r := range s.state.Records {
		recorded[r.Key] = true
	}
	var claimed []string
	added := false
	for _, m := range s.state.Schedules {
		if m.State != "enabled" {
			continue
		}
		from := s.state.Through
		if m.Since.After(from) {
			from = m.Since
		}
		if earliest := now.Add(-400 * 24 * time.Hour); from.Before(earliest) {
			from = earliest
		}
		loc, err := time.LoadLocation(m.Entry.Zone)
		if err != nil {
			continue
		}
		end := now.In(loc).Format("2006-01-02")
		for day := from.In(loc).Format("2006-01-02"); day <= end; day = runnerprotocol.NextDay(day) {
			if !m.Entry.OnDay(day) {
				continue
			}
			due, exists := DailyOccurrence(day, m.Entry.At, m.Entry.Zone)
			if !exists {
				var ok bool
				if due, ok = nextLocalDay(day, loc); !ok {
					continue
				}
			}
			key := occurrenceKey(m.Project, m.ID, day)
			if !due.After(from) || due.After(now) || recorded[key] {
				continue
			}
			state, offset := "claimed", due.In(loc).Format("-07:00")
			switch {
			case !exists:
				state, offset = "skipped", ""
			case now.Sub(due) > time.Duration(m.Entry.WindowMinutes)*time.Minute:
				state = "missed"
			}
			notification := "disabled"
			if m.Entry.Route != "" {
				notification = "pending"
			}
			if !s.trim() {
				// The bounded history is full of unsettled records: nothing
				// more is claimed until they settle.
				continue
			}
			s.state.Records = append(s.state.Records, managedRecord{Project: m.Project, Schedule: m.ID, ScheduleOccurrence: runnerprotocol.ScheduleOccurrence{
				Key: key, Day: day, Due: due, Offset: offset, Generation: m.Generation, State: state, Notification: notification}})
			recorded[key], added = true, true
			if state == "claimed" {
				claimed = append(claimed, key)
			}
		}
	}
	s.state.Through = now
	if added {
		if e := s.save(); e != nil {
			return nil, e
		}
	}
	return claimed, nil
}

// trim makes room for one record, dropping the oldest settled one once the
// bound is reached, and reports whether there is room.
func (s *ManagedScheduler) trim() bool {
	if len(s.state.Records) < maxManagedRecords {
		return true
	}
	at := slices.IndexFunc(s.state.Records, func(r managedRecord) bool {
		return r.State != "claimed" && r.Notification != "pending" && r.Notification != "sending"
	})
	if at < 0 {
		return false
	}
	s.state.Records = slices.Delete(s.state.Records, at, at+1)
	return true
}

func (s *ManagedScheduler) record(key string) *managedRecord {
	at := slices.IndexFunc(s.state.Records, func(r managedRecord) bool { return r.Key == key })
	if at < 0 {
		return nil
	}
	return &s.state.Records[at]
}

// pauseFor is the scheduler pausing a schedule by itself, with its reason.
func (s *ManagedScheduler) pauseFor(at int, reason string, now time.Time) {
	m := &s.state.Schedules[at]
	m.State, m.Reason, m.Revision, m.Updated = "paused", reason, m.Revision+1, now
}

// dispatch decides one claimed occurrence under the lock, then runs it
// outside the lock, so commands are acknowledged while it runs. A schedule
// paused, deleted or changed after the claim never dispatches the slot.
func (s *ManagedScheduler) dispatch(ctx context.Context, key string, now time.Time, started time.Time) error {
	s.mu.Lock()
	r := s.record(key)
	if r == nil || r.State != "claimed" {
		s.mu.Unlock()
		return nil
	}
	at := s.find(r.Project, r.Schedule)
	var entry runnerprotocol.ScheduleEntry
	switch {
	case ctx.Err() != nil || at < 0 || s.state.Schedules[at].State != "enabled" || s.state.Schedules[at].Generation != r.Generation || !r.Due.After(s.state.Schedules[at].Since):
		// Paused, deleted or changed since the claim, or paused and enabled
		// again, which authorizes only slots after it.
		r.State = "cancelled"
	case now.Add(time.Since(started)).Sub(r.Due) > time.Duration(s.state.Schedules[at].Entry.WindowMinutes)*time.Minute:
		r.State = "missed"
	case s.authority != nil && s.authority(r.Project, s.state.Schedules[at].Entry) != nil:
		r.State = "refused"
		s.pauseFor(at, runnerprotocol.ReasonNoAuthority, now)
	default:
		entry = s.state.Schedules[at].Entry
		if reason := s.revalidate(ctx, r.Project, entry); reason != "" {
			r.State = "refused"
			s.pauseFor(at, reason, now)
		}
	}
	if r.State != "claimed" {
		e := s.save()
		s.mu.Unlock()
		return e
	}
	s.mu.Unlock()
	state := s.execute(ctx, entry, key)
	if !runnerprotocol.ValidManagedState(state) || state == "claimed" {
		state = "error"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r = s.record(key); r != nil {
		r.State = state
	}
	return s.save()
}

// Tick claims the slots due now, dispatches them one at a time, then sends
// each pending notification once. It never catches up: a slot is dispatched
// only within its window, and a restart leaves an interrupted claim uncertain.
func (s *ManagedScheduler) Tick(ctx context.Context, now time.Time) error {
	started := time.Now()
	now = now.UTC()
	keys, e := s.claim(now)
	if e != nil {
		return e
	}
	sort.Strings(keys)
	for _, key := range keys {
		if e := s.dispatch(ctx, key, now, started); e != nil {
			return e
		}
	}
	return s.sendNotifications(ctx)
}

func (s *ManagedScheduler) sendNotifications(ctx context.Context) error {
	for {
		s.mu.Lock()
		at := slices.IndexFunc(s.state.Records, func(r managedRecord) bool { return r.Notification == "pending" && r.State != "claimed" })
		if at < 0 || ctx.Err() != nil {
			s.mu.Unlock()
			return nil
		}
		r := &s.state.Records[at]
		key := r.Key
		sched := s.find(r.Project, r.Schedule)
		if sched < 0 || s.state.Schedules[sched].Entry.Route == "" || s.notify == nil {
			r.Notification = "disabled"
			e := s.save()
			s.mu.Unlock()
			if e != nil {
				return e
			}
			continue
		}
		route, data := s.state.Schedules[sched].Entry.Route, runnerprotocol.ScheduleAlert(r.State)
		r.Notification = "sending"
		if e := s.save(); e != nil {
			s.mu.Unlock()
			return e
		}
		s.mu.Unlock()
		// One attempt: an uncertain delivery is recorded, never retried, and
		// never causes the run itself to be retried.
		sent := s.notify(ctx, route, data) == nil
		s.mu.Lock()
		if r := s.record(key); r != nil {
			r.Notification = "uncertain"
			if sent {
				r.Notification = "sent"
			}
		}
		e := s.save()
		s.mu.Unlock()
		if e != nil {
			return e
		}
	}
}
