package desktop

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/hubclient"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/suite"
)

// Schedules (#564) are the hub scheduler's, changed only by commands it
// acknowledges. The window prepares the exact suite version a schedule runs
// inside the project, reviews it, and sends the command. A command the hub
// has not acknowledged stays pending in the project, beside what the hub
// holds, and is shown as pending: never as the state it asked for.

// ScheduleDraft is a schedule as its sheet holds it: what it runs, where and
// when, and where its fixed alert goes.
type ScheduleDraft struct {
	Name          string   `json:"name"`
	Suite         ItemRef  `json:"suite"`
	Environment   string   `json:"environment"`
	Runner        string   `json:"runner"`
	Repeat        string   `json:"repeat"`
	Days          []string `json:"days"`
	At            string   `json:"at"`
	Zone          string   `json:"zone"`
	WindowMinutes int      `json:"window_minutes"`
	Route         string   `json:"route"`
}

// SchedulePin is one exact version a schedule is pinned to.
type SchedulePin struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ScheduleReview is exactly what a schedule command would authorize. Token
// binds it: a command whose preparation no longer matches is refused.
type ScheduleReview struct {
	Name          string        `json:"name"`
	Suite         string        `json:"suite"`
	Version       string        `json:"version"`
	Environment   string        `json:"environment"`
	Runner        string        `json:"runner"`
	Tests         []SchedulePin `json:"tests"`
	Targets       []SchedulePin `json:"targets"`
	Resets        []string      `json:"resets"`
	Repeat        string        `json:"repeat"`
	Days          []string      `json:"days"`
	At            string        `json:"at"`
	Zone          string        `json:"zone"`
	Next          []string      `json:"next"`
	WindowMinutes int           `json:"window_minutes"`
	Route         string        `json:"route"`
	Notification  string        `json:"notification,omitzero"`
	Consequence   string        `json:"consequence"`
	Token         string        `json:"token"`
}

// SchedulePrepareRequest prepares a new schedule, or an edit of one.
type SchedulePrepareRequest struct {
	Context  RequestContext `json:"context"`
	Schedule string         `json:"schedule,omitzero"`
	Draft    ScheduleDraft  `json:"draft"`
}

// SchedulePrepareResult is the review, or every problem with the draft.
type SchedulePrepareResult struct {
	State    State           `json:"state"`
	Reason   string          `json:"reason,omitzero"`
	Context  RequestContext  `json:"context"`
	Review   *ScheduleReview `json:"review,omitzero"`
	Problems []FieldProblem  `json:"problems"`
}

func (r *SchedulePrepareResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ScheduleOccurrenceView is one recorded slot of a schedule.
type ScheduleOccurrenceView struct {
	Day    string `json:"day"`
	Due    string `json:"due"`
	Offset string `json:"offset,omitzero"`
	State  string `json:"state"`
}

// ScheduleRow is one schedule as the hub acknowledged it, with any change the
// window sent that the hub has not acknowledged yet.
type ScheduleRow struct {
	ID            string                   `json:"id"`
	Revision      int                      `json:"revision"`
	Name          string                   `json:"name"`
	Suite         string                   `json:"suite"`
	Version       string                   `json:"version"`
	Environment   string                   `json:"environment"`
	Runner        string                   `json:"runner"`
	Repeat        string                   `json:"repeat"`
	Days          []string                 `json:"days"`
	At            string                   `json:"at"`
	Zone          string                   `json:"zone"`
	WindowMinutes int                      `json:"window_minutes"`
	Route         string                   `json:"route"`
	State         string                   `json:"state"`
	Reason        string                   `json:"reason,omitzero"`
	Next          string                   `json:"next,omitzero"`
	Pending       string                   `json:"pending,omitzero"`
	PendingReason string                   `json:"pending_reason,omitzero"`
	Recent        []ScheduleOccurrenceView `json:"recent"`
	Draft         *ScheduleDraft           `json:"draft,omitzero"`
	// SuiteID and RunnerID are the suite and runner the schedule runs,
	// empty when they are not this project's.
	SuiteID  string `json:"suite_id,omitzero"`
	RunnerID string `json:"runner_id,omitzero"`
}

// ScheduleListRequest lists the project's schedules, only those of one suite
// or one runner when either is named.
type ScheduleListRequest struct {
	Context RequestContext `json:"context"`
	Suite   string         `json:"suite,omitzero"`
	Runner  string         `json:"runner,omitzero"`
}

// ScheduleListResult is the project's schedules, soonest next run first.
type ScheduleListResult struct {
	State     State          `json:"state"`
	Reason    string         `json:"reason,omitzero"`
	Context   RequestContext `json:"context"`
	Schedules []ScheduleRow  `json:"schedules"`
}

func (r *ScheduleListResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ScheduleCommandRequest is one change to one schedule. Intent is allocated
// once per submission and reused to send the same change again; Token is the
// review a create or an update was made from.
type ScheduleCommandRequest struct {
	Context  RequestContext `json:"context"`
	Kind     string         `json:"kind"`
	Schedule string         `json:"schedule,omitzero"`
	Expected int            `json:"expected_revision"`
	Enable   bool           `json:"enable"`
	Draft    *ScheduleDraft `json:"draft,omitzero"`
	Token    string         `json:"token,omitzero"`
	Intent   string         `json:"intent"`
}

// ScheduleCommandResult is the scheduler's acknowledgement, or why there is
// none. Pending says the change was sent and not acknowledged: it is kept,
// and sending the same intent again is safe.
type ScheduleCommandResult struct {
	State    State          `json:"state"`
	Reason   string         `json:"reason,omitzero"`
	Context  RequestContext `json:"context"`
	Schedule string         `json:"schedule,omitzero"`
	Revision int            `json:"revision"`
	Status   string         `json:"status,omitzero"`
	Next     string         `json:"next,omitzero"`
	Pending  bool           `json:"pending"`
	Replayed bool           `json:"replayed"`
}

func (r *ScheduleCommandResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// scheduleResend sends a pending change again, exactly as it was kept.
const scheduleResend = "resend"

// scheduleRecordsSchema is the project's own record of its schedules: what
// each was prepared from, and each change sent and not yet acknowledged.
const scheduleRecordsSchema = "readmit-schedule-intents/v1"

type schedulePending struct {
	Intent   string                        `json:"intent"`
	Kind     string                        `json:"kind"`
	Expected int                           `json:"expected_revision"`
	Enable   bool                          `json:"enable"`
	Entry    *runnerprotocol.ScheduleEntry `json:"entry,omitzero"`
	Reason   string                        `json:"reason"`
}

type scheduleRecord struct {
	ID      string           `json:"id"`
	Draft   *ScheduleDraft   `json:"draft,omitzero"`
	Pending *schedulePending `json:"pending,omitzero"`
}

type scheduleRecords struct {
	Schema    string           `json:"schema"`
	Schedules []scheduleRecord `json:"schedules"`
}

func scheduleRecordsPath(root string) string {
	return filepath.Join(root, ".readmit", "schedule-intents.json")
}

func readScheduleRecords(root string) (scheduleRecords, error) {
	records := scheduleRecords{Schema: scheduleRecordsSchema, Schedules: []scheduleRecord{}}
	if _, err := os.Lstat(scheduleRecordsPath(root)); errors.Is(err, fs.ErrNotExist) {
		return records, nil
	}
	data, err := boundedFile(scheduleRecordsPath(root), 1<<20)
	if err != nil || json.Unmarshal(data, &records, json.RejectUnknownMembers(true)) != nil || records.Schema != scheduleRecordsSchema || records.Schedules == nil {
		return records, errors.New("the project's schedule record cannot be read")
	}
	return records, nil
}

func writeScheduleRecords(root string, records scheduleRecords) error {
	data, err := json.Marshal(records, json.Deterministic(true))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, ".readmit"), 0700); err != nil {
		return err
	}
	return replaceDocument(scheduleRecordsPath(root), data)
}

func (r *scheduleRecords) at(id string) *scheduleRecord {
	for i := range r.Schedules {
		if r.Schedules[i].ID == id {
			return &r.Schedules[i]
		}
	}
	r.Schedules = append(r.Schedules, scheduleRecord{ID: id})
	return &r.Schedules[len(r.Schedules)-1]
}

func (r *scheduleRecords) drop(id string) {
	r.Schedules = slices.DeleteFunc(r.Schedules, func(s scheduleRecord) bool { return s.ID == id })
}

// preparedSchedule is a draft prepared for the scheduler: its entry and the
// review that shows it.
type preparedSchedule struct {
	entry  runnerprotocol.ScheduleEntry
	review ScheduleReview
}

// prepareSchedule compiles the suite version for the environment into the
// project's scheduled folder, pins it, and composes the review.
func (a *App) prepareSchedule(ctx context.Context, loaded *loadedCatalog, draft ScheduleDraft) (*preparedSchedule, []FieldProblem, refusal) {
	problems := []FieldProblem{}
	if draft.Days == nil {
		draft.Days = []string{}
	}
	item, err := loaded.suiteItem(draft.Suite)
	if err != nil {
		return nil, []FieldProblem{{Field: "suite", Problem: "choose a suite of this project"}}, refusal{}
	}
	version, err := loaded.suiteVersion(item, draft.Suite.Revision)
	if err != nil || !version.runnable() {
		return nil, []FieldProblem{{Field: "suite", Problem: "choose a runnable version of this suite"}}, refusal{}
	}
	at := slices.IndexFunc(version.draft.Environments, func(e SuiteEnvironment) bool { return e.ID == draft.Environment })
	if at < 0 {
		problems = append(problems, FieldProblem{Field: "environment", Problem: "choose one of this suite's environments"})
	}
	runnerAt := loaded.document.Find(draft.Runner)
	var runnerConfig string
	runnerName := ""
	if runnerAt < 0 || loaded.document.Items[runnerAt].Kind != string(RunnerItem) || loaded.removed(loaded.document.Items[runnerAt]) {
		problems = append(problems, FieldProblem{Field: "runner", Problem: "choose one of this project's runners"})
	} else {
		paths, availability, reason := loaded.backing(loaded.document.Items[runnerAt])
		if availability != ItemAvailable {
			problems = append(problems, FieldProblem{Field: "runner", Problem: reason})
		} else {
			runnerConfig, runnerName = paths[string(RunnerItem)], cmp.Or(loaded.document.Items[runnerAt].Name, "Runner")
		}
	}
	entry := runnerprotocol.ScheduleEntry{Name: cmp.Or(draft.Name, item.Name), Suite: cmp.Or(item.Name, "Suite"), Version: "Version " + version.label,
		Runner: cmp.Or(runnerName, "Runner"), RunnerConfig: cmp.Or(runnerConfig, "/"), Spec: "/", Input: strings.Repeat("0", 64),
		Repeat: draft.Repeat, Days: draft.Days, At: draft.At, Zone: draft.Zone, WindowMinutes: draft.WindowMinutes, Route: draft.Route}
	if version.label == originalVersion {
		entry.Version = "Original"
	}
	if at >= 0 {
		entry.Environment = cmp.Or(version.draft.Environments[at].Name, version.draft.Environments[at].ID)
	} else {
		entry.Environment = "Environment"
	}
	if !runnerprotocol.ValidRoute(draft.Route) {
		problems = append(problems, FieldProblem{Field: "route", Problem: "choose an approved HTTPS destination, or None"})
	}
	if _, err := time.LoadLocation(draft.Zone); err != nil || draft.Zone == "" || draft.Zone == "Local" {
		problems = append(problems, FieldProblem{Field: "zone", Problem: "choose a time zone"})
	}
	if clock, err := time.Parse("15:04", draft.At); err != nil || clock.Format("15:04") != draft.At {
		problems = append(problems, FieldProblem{Field: "at", Problem: "enter the time as HH:MM"})
	}
	if draft.WindowMinutes < 1 || draft.WindowMinutes > 720 {
		problems = append(problems, FieldProblem{Field: "window", Problem: "a run window is 1 to 720 minutes"})
	}
	if draft.Repeat == runnerprotocol.RepeatDays && len(draft.Days) == 0 {
		problems = append(problems, FieldProblem{Field: "days", Problem: "choose at least one day"})
	}
	if len(problems) == 0 && entry.Validate() != nil {
		problems = append(problems, FieldProblem{Field: "schedule", Problem: "the schedule is not complete"})
	}
	if len(problems) != 0 {
		return nil, problems, refusal{}
	}
	queue, declined := a.placeScheduledSuite(ctx, loaded, item, version, version.draft.Environments[at].ID)
	if declined.reason != "" {
		return nil, nil, declined
	}
	identity, _, err := runqueue.PinnedJobs(ctx, queue)
	if err != nil {
		return nil, nil, refusal{Failed, "the prepared suite could not be pinned: " + err.Error()}
	}
	entry.Spec, entry.Input = queue, identity
	review := ScheduleReview{Name: entry.Name, Suite: entry.Suite, Version: entry.Version, Environment: entry.Environment, Runner: entry.Runner,
		Tests: []SchedulePin{}, Targets: []SchedulePin{}, Resets: []string{}, Repeat: entry.Repeat, Days: entry.Days, At: entry.At, Zone: entry.Zone,
		Next: []string{}, WindowMinutes: entry.WindowMinutes, Route: entry.Route,
		Consequence: "Runs " + entry.Suite + " on " + entry.Environment + " at the displayed times until paused."}
	for _, test := range version.draft.Tests {
		name := test.ID
		if i := loaded.document.Find(test.Test.ID); i >= 0 {
			name = cmp.Or(loaded.document.Items[i].Name, name)
		}
		review.Tests = append(review.Tests, SchedulePin{Name: name, Version: "Version " + cmp.Or(test.Test.Revision, "1")})
	}
	seen := map[string]bool{}
	for _, binding := range version.draft.Environments[at].Bindings {
		i := loaded.document.Find(binding.Target.ID)
		if i < 0 || seen[binding.Target.ID] {
			continue
		}
		seen[binding.Target.ID] = true
		record := loaded.document.Items[i]
		review.Targets = append(review.Targets, SchedulePin{Name: record.Name, Version: "Version " + cmp.Or(record.RevisionLabel(), "1")})
		if members, err := loaded.environmentOf(record); err == nil && members.reset != nil {
			review.Resets = append(review.Resets, record.Name)
		}
	}
	for _, next := range runnerprotocol.NextOccurrences(entry, a.now(), 3) {
		review.Next = append(review.Next, next.UTC().Format(time.RFC3339))
	}
	if entry.Route != "" {
		review.Notification = string(runnerprotocol.ScheduleAlert("passed"))
	}
	token, _ := json.Marshal(entry, json.Deterministic(true))
	sum := sha256.Sum256(token)
	review.Token = hex.EncodeToString(sum[:])
	return &preparedSchedule{entry: entry, review: review}, nil, refusal{}
}

// placeScheduledSuite prepares one suite version for one environment into a
// folder of the project the schedule names, once per exact compiled content:
// the same content reuses its folder, and a changed one gets a new folder, so
// an acknowledged schedule's inputs are never rewritten under it.
func (a *App) placeScheduledSuite(ctx context.Context, loaded *loadedCatalog, item catalog.Item, version *suiteVersion, environment string) (string, refusal) {
	_, data, declined := loaded.compiled(version)
	if data == nil {
		return "", declined
	}
	var references []byte
	if pins := latestBaseline(loaded.suiteApprovals(item.ID), version.label); pins != nil {
		references, _ = json.Marshal(pins.Tests, json.Deterministic(true))
	}
	sum := sha256.Sum256(append(append(append([]byte{}, data...), 0), append([]byte(environment+"\x00"), references...)...))
	folder := filepath.Join(loaded.root, ".readmit", "scheduled", item.ID+"-"+hex.EncodeToString(sum[:12]))
	queue := filepath.Join(folder, "queue.json")
	if _, err := os.Lstat(queue); err == nil {
		return queue, refusal{}
	}
	if err := os.MkdirAll(filepath.Dir(folder), 0700); err != nil {
		return "", refusal{Failed, "the scheduled suite cannot be placed in the project"}
	}
	os.RemoveAll(folder)
	sidecar := ""
	if pins := latestBaseline(loaded.suiteApprovals(item.ID), version.label); pins != nil {
		_, held, remove, err := materializeReleases(pins.Tests)
		if err != nil {
			return "", refusal{Failed, err.Error()}
		}
		defer remove()
		sidecar = held
	}
	path, remove, err := placeCompiled(loaded.root, data)
	if err != nil {
		return "", refusal{Failed, err.Error()}
	}
	defer remove()
	if err := ctx.Err(); err != nil {
		return "", refusal{Cancelled, "the schedule was not prepared"}
	}
	if _, err := suite.Prepare(suite.Request{Path: path, Environment: environment, Output: folder, References: sidecar}); err != nil {
		os.RemoveAll(folder)
		return "", refusal{Failed, err.Error()}
	}
	return queue, refusal{}
}

// PrepareSchedule prepares the exact suite version, environment and runner a
// schedule would run, and answers the review a command must be made from.
// It sends nothing.
func (a *App) PrepareSchedule(request SchedulePrepareRequest) SchedulePrepareResult {
	return run(a, false, true, func(ctx context.Context) SchedulePrepareResult {
		result := SchedulePrepareResult{Context: request.Context, Problems: []FieldProblem{}}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		prepared, problems, declined := a.prepareSchedule(ctx, loaded, request.Draft)
		switch {
		case declined.reason != "":
			result.refuse(declined.state, declined.reason)
		case len(problems) != 0:
			result.State, result.Problems, result.Reason = Failed, problems, problems[0].Problem
		default:
			result.State, result.Review = Completed, &prepared.review
		}
		return result
	})
}

// scheduleHub is the signed-in hub client and the project schedules are kept
// in, as the window reports their absence.
func (a *App) scheduleHub() (*hubclient.Client, string, refusal) {
	client, _, err := a.hub.SignedIn()
	switch {
	case errors.Is(err, hubclient.ErrNotConnected) || errors.Is(err, hubclient.ErrNotSelected):
		return nil, "", refusal{Failed, "Schedules run on the team hub. Connect to it and sign in."}
	case err != nil:
		return nil, "", refusal{PermissionDenied, "Sign in to the team hub to see and change schedules."}
	}
	project, declined := a.hubProject()
	if declined.reason != "" {
		return nil, "", declined
	}
	return client, project, refusal{}
}

func scheduleHubRefusal(err error) refusal {
	switch {
	case errors.Is(err, hubclient.ErrScheduleServiceStopped):
		return refusal{Failed, "The hub's schedule service is not running. Nothing was changed."}
	case errors.Is(err, hubclient.ErrAccessDenied):
		return refusal{PermissionDenied, "Your hub role does not allow this."}
	case errors.Is(err, hubclient.ErrScheduleLimit):
		return refusal{Failed, "The hub holds as many schedules as it keeps. Nothing was changed."}
	case errors.Is(err, hubclient.ErrConflict):
		return refusal{Failed, "The schedule changed since it was read. Refresh and try again."}
	case errors.Is(err, hubclient.ErrScheduleMissing):
		return refusal{Failed, "The hub no longer holds this schedule."}
	case errors.Is(err, hubclient.ErrHubUnreachable), errors.Is(err, hubclient.ErrReadUnanswered):
		return refusal{Failed, "The hub could not be reached."}
	}
	return refusal{Failed, err.Error()}
}

// ListSchedules reads the project's schedules from the hub scheduler, with
// each change this project sent that the scheduler has not acknowledged.
func (a *App) ListSchedules(request ScheduleListRequest) ScheduleListResult {
	return runNamed[ScheduleListResult, *ScheduleListResult](a, profiles["ListSchedules"], func(ctx context.Context) ScheduleListResult {
		result := ScheduleListResult{Context: request.Context, Schedules: []ScheduleRow{}}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		records, err := readScheduleRecords(root)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		client, project, declined := a.scheduleHub()
		if declined.reason != "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		list, err := client.ListSchedules(ctx, project)
		if err != nil {
			declined := scheduleHubRefusal(err)
			result.refuse(declined.state, declined.reason)
			return result
		}
		// A schedule this project did not record is still attributed to its
		// suite and runner by what it runs: the suite's prepared folder and
		// the runner's saved configuration.
		loaded, _ := a.loadCatalog(ctx, request.Context, false)
		origin := func(entry runnerprotocol.ScheduleEntry) (string, string) {
			suiteID, runnerID := "", ""
			if loaded == nil {
				return "", ""
			}
			if folder := filepath.Dir(entry.Spec); filepath.Dir(folder) == filepath.Join(loaded.root, ".readmit", "scheduled") {
				suiteID, _, _ = strings.Cut(filepath.Base(folder), "-")
			}
			for _, item := range loaded.document.Items {
				if item.Kind != string(RunnerItem) {
					continue
				}
				if paths, availability, _ := loaded.backing(item); availability == ItemAvailable && paths[string(RunnerItem)] == entry.RunnerConfig {
					runnerID = item.ID
				}
			}
			return suiteID, runnerID
		}
		held := map[string]bool{}
		for _, s := range list.Schedules {
			held[s.ID] = true
			row := scheduleRow(s.ID, s.Revision, s.Entry)
			row.SuiteID, row.RunnerID = origin(s.Entry)
			row.State, row.Reason = s.State, s.Reason
			if s.Next != nil {
				row.Next = s.Next.UTC().Format(time.RFC3339)
			}
			for _, o := range s.Recent {
				row.Recent = append(row.Recent, ScheduleOccurrenceView{Day: o.Day, Due: o.Due.UTC().Format(time.RFC3339), Offset: o.Offset, State: o.State})
			}
			result.Schedules = append(result.Schedules, row)
		}
		for _, record := range records.Schedules {
			at := slices.IndexFunc(result.Schedules, func(r ScheduleRow) bool { return r.ID == record.ID })
			if at < 0 {
				// Only a create the hub has not acknowledged is listed without it.
				if record.Pending == nil || record.Pending.Kind != runnerprotocol.CommandCreate || record.Pending.Entry == nil {
					continue
				}
				result.Schedules = append(result.Schedules, scheduleRow(record.ID, 0, *record.Pending.Entry))
				at = len(result.Schedules) - 1
				result.Schedules[at].State = "pending"
			}
			row := &result.Schedules[at]
			row.Draft = record.Draft
			if record.Pending != nil {
				row.Pending, row.PendingReason = record.Pending.Kind, record.Pending.Reason
			}
		}
		result.Schedules = slices.DeleteFunc(result.Schedules, func(r ScheduleRow) bool {
			if r.Draft != nil {
				r.SuiteID, r.RunnerID = r.Draft.Suite.ID, r.Draft.Runner
			}
			return request.Suite != "" && r.SuiteID != request.Suite || request.Runner != "" && r.RunnerID != request.Runner
		})
		slices.SortStableFunc(result.Schedules, func(x, y ScheduleRow) int {
			if (x.Next == "") != (y.Next == "") {
				if x.Next == "" {
					return 1
				}
				return -1
			}
			return cmp.Or(strings.Compare(x.Next, y.Next), strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)), strings.Compare(x.ID, y.ID))
		})
		result.State = Completed
		return result
	})
}

func scheduleRow(id string, revision int, entry runnerprotocol.ScheduleEntry) ScheduleRow {
	return ScheduleRow{ID: id, Revision: revision, Name: entry.Name, Suite: entry.Suite, Version: entry.Version, Environment: entry.Environment,
		Runner: entry.Runner, Repeat: entry.Repeat, Days: entry.Days, At: entry.At, Zone: entry.Zone, WindowMinutes: entry.WindowMinutes,
		Route: entry.Route, Recent: []ScheduleOccurrenceView{}}
}

// CommandSchedule sends one change to the hub scheduler. A create or an update
// is prepared again and must match the review it was made from. The change is
// recorded as pending before it is sent and cleared only by the scheduler's
// acknowledgement or its refusal; a change the hub did not answer stays
// pending, and sending the same intent again applies it at most once.
func (a *App) CommandSchedule(request ScheduleCommandRequest) ScheduleCommandResult {
	return runNamed[ScheduleCommandResult, *ScheduleCommandResult](a, profiles["CommandSchedule"], func(ctx context.Context) ScheduleCommandResult {
		result := ScheduleCommandResult{Context: request.Context}
		if !runnerprotocol.ID(request.Intent) {
			result.refuse(Failed, "a schedule change is sent under its own intent")
			return result
		}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		records, err := readScheduleRecords(loaded.root)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		client, project, declined := a.scheduleHub()
		if declined.reason != "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		id := request.Schedule
		command := runnerprotocol.ScheduleCommand{Intent: request.Intent, Kind: request.Kind, Expected: request.Expected, Enable: request.Enable}
		if request.Kind == scheduleResend {
			// The change kept pending is sent again exactly, under its own
			// intent, so the scheduler applies it at most once.
			at := slices.IndexFunc(records.Schedules, func(r scheduleRecord) bool { return r.ID == id && r.Pending != nil })
			if at < 0 {
				result.refuse(Failed, "This schedule has no change waiting to be sent.")
				return result
			}
			held := records.Schedules[at].Pending
			command = runnerprotocol.ScheduleCommand{Intent: held.Intent, Kind: held.Kind, Expected: held.Expected, Enable: held.Enable, Entry: held.Entry}
			request.Kind, request.Draft = held.Kind, nil
		}
		switch request.Kind {
		case runnerprotocol.CommandCreate, runnerprotocol.CommandUpdate:
			if command.Entry != nil && request.Draft == nil {
				break
			}
			if request.Draft == nil {
				result.refuse(Failed, "a schedule is created or changed from its review")
				return result
			}
			prepared, problems, declined := a.prepareSchedule(ctx, loaded, *request.Draft)
			switch {
			case declined.reason != "":
				result.refuse(declined.state, declined.reason)
				return result
			case len(problems) != 0:
				result.refuse(Failed, problems[0].Problem)
				return result
			case prepared.review.Token != request.Token:
				result.refuse(Failed, "What this schedule runs changed since its review. Review it again.")
				return result
			}
			command.Entry = &prepared.entry
			if request.Kind == runnerprotocol.CommandCreate && id == "" {
				if held := slices.IndexFunc(records.Schedules, func(r scheduleRecord) bool { return r.Pending != nil && r.Pending.Intent == request.Intent }); held >= 0 {
					id = records.Schedules[held].ID
				} else {
					minted, err := catalog.NewID()
					if err != nil {
						result.refuse(Failed, err.Error())
						return result
					}
					id = "s-" + minted
				}
			}
		case runnerprotocol.CommandEnable, runnerprotocol.CommandPause, runnerprotocol.CommandDelete:
		default:
			result.refuse(Failed, "a schedule is created, changed, enabled, paused or deleted")
			return result
		}
		command.Schedule = id
		record := records.at(id)
		if request.Draft != nil {
			draft := *request.Draft
			if record.Draft == nil || request.Kind == runnerprotocol.CommandCreate {
				record.Draft = &draft
			}
		}
		record.Pending = &schedulePending{Intent: command.Intent, Kind: command.Kind, Expected: command.Expected, Enable: command.Enable, Entry: command.Entry, Reason: "Not acknowledged yet"}
		if err := writeScheduleRecords(loaded.root, records); err != nil {
			result.refuse(Failed, "the schedule change could not be recorded in the project; nothing was sent")
			return result
		}
		ack, replayed, err := client.CommandSchedule(ctx, project, command)
		result.Schedule = id
		if errors.Is(err, hubclient.ErrScheduleUnacknowledged) || err != nil && ctx.Err() != nil {
			record = records.at(id)
			record.Pending.Reason = "The hub did not acknowledge this change"
			_ = writeScheduleRecords(loaded.root, records)
			result.Pending = true
			result.refuse(Failed, "The hub did not acknowledge the change. It stays pending until it is sent again.")
			return result
		}
		record = records.at(id)
		record.Pending = nil
		if err != nil {
			if request.Kind == runnerprotocol.CommandCreate {
				records.drop(id)
			}
			_ = writeScheduleRecords(loaded.root, records)
			declined := scheduleHubRefusal(err)
			result.refuse(declined.state, declined.reason)
			return result
		}
		switch {
		case ack.State == "deleted":
			records.drop(id)
		case request.Kind == runnerprotocol.CommandUpdate && request.Draft != nil:
			draft := *request.Draft
			record.Draft = &draft
		}
		_ = writeScheduleRecords(loaded.root, records)
		result.State, result.Revision, result.Status, result.Replayed = Completed, ack.Revision, ack.State, replayed
		if ack.Next != nil {
			result.Next = ack.Next.UTC().Format(time.RFC3339)
		}
		return result
	})
}
