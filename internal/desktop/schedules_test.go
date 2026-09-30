package desktop_test

import (
	"encoding/base64"
	"encoding/json/v2"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

func runnerDraft(root, assigned string) *desktop.RunnerDraft {
	return &desktop.RunnerDraft{Hub: "https://127.0.0.1:1", Project: "alpha", Environment: "lab", Root: root,
		CA: "/etc/readmit-runner/ca.pem", Certificate: "/etc/readmit-runner/client.pem",
		Key:       desktop.RunnerReferenceInput{Command: "/usr/local/bin/customer-secret-reader", Arguments: []string{"runner-key"}},
		Token:     desktop.RunnerReferenceInput{Command: "/usr/local/bin/customer-secret-reader", Arguments: []string{"runner-token"}},
		UpdateKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), UpdateEngine: "NEXT_APPROVED_BUILD", Assigned: assigned}
}

func saveRunner(t *testing.T, p *suiteProject, name string, draft *desktop.RunnerDraft) desktop.ItemRef {
	t.Helper()
	saved := p.app.SaveItem(desktop.SaveItemRequest{Context: p.context, Kind: desktop.RunnerItem, IntentID: "runner-" + strings.ToLower(strings.ReplaceAll(name, " ", "-")), Draft: desktop.ItemDraft{Name: name, Runner: draft}})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("save runner: %+v", saved)
	}
	return *saved.Saved
}

// A runner is a named object of the project. Its status is what an actual
// read or admission established, dated; one nothing has checked is Not
// checked, and exporting its setup installs nothing.
func TestNamedRunnersAreSavedListedAndCarryTheirEstablishedStatus(t *testing.T) {
	p := newSuiteProject(t, "127.0.0.1:2575")
	root := filepath.Join(t.TempDir(), "runs")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	invalid := runnerDraft(root, "")
	invalid.Hub = "http://hub.example"
	if refused := p.app.SaveItem(desktop.SaveItemRequest{Context: p.context, Kind: desktop.RunnerItem, IntentID: "bad", Draft: desktop.ItemDraft{Name: "Bad", Runner: invalid}}); refused.Outcome != desktop.InvalidOutcome {
		t.Fatalf("an incomplete runner saved: %+v", refused)
	}
	ref := saveRunner(t, p, "QA runner", runnerDraft(root, p.lab.ID))
	opened := p.app.OpenItemDraft(desktop.ItemRequest{Context: p.context, Ref: ref})
	if opened.State != desktop.Completed || opened.Draft.Runner == nil || opened.Draft.Runner.Assigned != p.lab.ID || opened.Draft.Runner.Root != root {
		t.Fatalf("the runner draft: %+v", opened)
	}
	list := p.app.ListRunners(p.context)
	if list.State != desktop.Completed || len(list.Runners) != 1 {
		t.Fatalf("runners: %+v", list)
	}
	row := list.Runners[0]
	if row.Name != "QA runner" || row.Environment != "Scheduling lab" || row.Status != desktop.RunnerNotChecked || row.LastSeen != "" || !row.Local {
		t.Fatalf("an unchecked runner: %+v", row)
	}
	// An admission that did not complete is recorded, dated, as what it was.
	if enrolled := p.app.EnrollRunner(row.Config); enrolled.State == desktop.Completed {
		t.Fatalf("an unreachable hub admitted: %+v", enrolled)
	}
	row = p.app.ListRunners(p.context).Runners[0]
	if row.Status != desktop.RunnerOffline || row.LastSeen == "" {
		t.Fatalf("after a failed admission: %+v", row)
	}
	p.dialog.destination = filepath.Join(t.TempDir(), "readmit-runner.json")
	opened = p.app.OpenItemDraft(desktop.ItemRequest{Context: p.context, Ref: ref})
	d := opened.Draft.Runner
	exported := p.app.SaveRunnerConfig(desktop.RunnerConfigRequest{Hub: d.Hub, Project: d.Project, Environment: d.Environment, Root: d.Root, CA: d.CA, Certificate: d.Certificate,
		Key: d.Key, Token: d.Token, UpdateKey: d.UpdateKey, UpdateEngine: d.UpdateEngine, Source: row.Config})
	if exported.State != desktop.Completed || !strings.HasSuffix(exported.Output, "/readmit-runner.json") {
		t.Fatalf("export: %+v", exported)
	}
	if row = p.app.ListRunners(p.context).Runners[0]; row.Status != desktop.RunnerSetupRequired {
		t.Fatalf("an exported runner claims more than setup required: %+v", row)
	}
}

// scheduleHub is the hub's managed schedule route as the window reaches it:
// acknowledged state per intent, revision checks, and the two ways a change
// goes unanswered — a dropped connection and a stopped schedule service.
type scheduleHub struct {
	mu        sync.Mutex
	schedules map[string]*runnerprotocol.ScheduleStatus
	intents   map[string]runnerprotocol.ScheduleAck
	drop      bool
	stopped   bool
	commands  int
}

func (h *scheduleHub) route(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	send := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.MarshalWrite(w, v)
	}
	if h.stopped {
		http.Error(w, "schedule service not running", 503)
		return
	}
	if r.Method == "GET" {
		list := runnerprotocol.ScheduleList{Schema: runnerprotocol.ScheduleListSchema, Project: teamProject, At: time.Now().UTC(), Schedules: []runnerprotocol.ScheduleStatus{}}
		for _, s := range h.schedules {
			list.Schedules = append(list.Schedules, *s)
		}
		send(200, list)
		return
	}
	data, _ := io.ReadAll(r.Body)
	c, err := runnerprotocol.DecodeScheduleCommand(data)
	if err != nil {
		http.Error(w, "refused", 400)
		return
	}
	h.commands++
	if ack, held := h.intents[c.Intent]; held {
		send(200, ack)
		return
	}
	now := time.Now().UTC()
	ack := runnerprotocol.ScheduleAck{Schema: runnerprotocol.ScheduleAckSchema, Intent: c.Intent, Schedule: c.Schedule, Acknowledged: now}
	held := h.schedules[c.Schedule]
	switch {
	case c.Kind == runnerprotocol.CommandCreate && held == nil:
		state := "paused"
		if c.Enable {
			state = "enabled"
		}
		held = &runnerprotocol.ScheduleStatus{ID: c.Schedule, Revision: 1, Generation: 1, State: state, Entry: *c.Entry, Updated: now, Recent: []runnerprotocol.ScheduleOccurrence{}}
		h.schedules[c.Schedule] = held
	case held == nil:
		http.Error(w, "no such schedule", 404)
		return
	case held.Revision != c.Expected || c.Kind == runnerprotocol.CommandCreate:
		http.Error(w, "conflict", 409)
		return
	case c.Kind == runnerprotocol.CommandPause:
		held.State, held.Revision = "paused", held.Revision+1
	case c.Kind == runnerprotocol.CommandEnable:
		held.State, held.Revision = "enabled", held.Revision+1
	case c.Kind == runnerprotocol.CommandUpdate:
		held.Entry, held.Revision = *c.Entry, held.Revision+1
	case c.Kind == runnerprotocol.CommandDelete:
		delete(h.schedules, c.Schedule)
		held = nil
	}
	ack.State = "deleted"
	if held != nil {
		ack.Revision, ack.State = held.Revision, held.State
		held.Next = nil
		if held.State == "enabled" {
			next := runnerprotocol.NextOccurrences(held.Entry, now, 1)[0]
			held.Next, ack.Next = &next, &next
		}
	}
	h.intents[c.Intent] = ack
	if h.drop {
		// Applied, and the answer is lost on the way back.
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
		return
	}
	send(201, ack)
}

// A schedule is created only by the scheduler's acknowledgement of the exact
// reviewed preparation. A change the hub does not answer stays pending and is
// never shown as done; sending it again applies it once. A stopped schedule
// service changes nothing.
func TestSchedulesAreAcknowledgedPendingUntilAnsweredAndPinnedToTheirReview(t *testing.T) {
	hub := &scheduleHub{schedules: map[string]*runnerprotocol.ScheduleStatus{}, intents: map[string]runnerprotocol.ScheduleAck{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects/"+teamProject+"/schedules", hub.route)
	for _, probe := range []string{"/health/live", "/health/ready"} {
		mux.HandleFunc(probe, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	}
	fixture := newAuthenticatedHubApp(t, mux, "ana", writerScopes)
	fixture.dialog.folder = t.TempDir()
	p := suiteProjectOn(t, fixture.app, fixture.dialog, "127.0.0.1:2575")
	suiteRef := p.saveSuite(t, "create", "", "", "Scheduling smoke", p.smokeSuite())
	runnerRef := saveRunner(t, p, "QA runner", runnerDraft(filepath.Join(t.TempDir(), "runs"), ""))
	draft := desktop.ScheduleDraft{Suite: suiteRef, Environment: "lab", Runner: runnerRef.ID, Repeat: runnerprotocol.RepeatWeekdays, Days: []string{},
		At: "02:30", Zone: "America/New_York", WindowMinutes: 30}

	// Every problem is named at its field; nothing is prepared.
	bad := draft
	bad.Repeat, bad.At, bad.Zone = runnerprotocol.RepeatDays, "2:30", ""
	if refused := p.app.PrepareSchedule(desktop.SchedulePrepareRequest{Context: p.context, Draft: bad}); refused.State != desktop.Failed || len(refused.Problems) != 3 {
		t.Fatalf("an incomplete schedule prepared: %+v", refused)
	}
	prepared := p.app.PrepareSchedule(desktop.SchedulePrepareRequest{Context: p.context, Draft: draft})
	review := prepared.Review
	if prepared.State != desktop.Completed || review == nil || review.Suite != "Scheduling smoke" || review.Runner != "QA runner" || len(review.Next) != 3 ||
		len(review.Tests) != 2 || review.Tests[0].Version == "" || len(review.Targets) == 0 || review.Token == "" ||
		review.Consequence != "Runs Scheduling smoke on "+review.Environment+" at the displayed times until paused." {
		t.Fatalf("the review: %+v", prepared)
	}
	for _, next := range review.Next {
		at, _ := time.Parse(time.RFC3339, next)
		if local := at.In(mustLocation(t, "America/New_York")); local.Format("15:04") != "02:30" || local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
			t.Fatalf("an occurrence is not a weekday 02:30 in its zone: %v", review.Next)
		}
	}
	if again := p.app.PrepareSchedule(desktop.SchedulePrepareRequest{Context: p.context, Draft: draft}); again.Review.Token != review.Token {
		t.Fatal("the same preparation reviewed differently")
	}

	command := func(request desktop.ScheduleCommandRequest) desktop.ScheduleCommandResult {
		request.Context = p.context
		return p.app.CommandSchedule(request)
	}
	// A review that does not match what would be sent is refused unsent.
	if stale := command(desktop.ScheduleCommandRequest{Kind: "create", Enable: true, Draft: &draft, Token: strings.Repeat("0", 64), Intent: "i-stale"}); stale.State != desktop.Failed || hub.commands != 0 {
		t.Fatalf("a stale review was sent: %+v", stale)
	}
	// Applied but unanswered: pending, never enabled.
	hub.drop = true
	lost := command(desktop.ScheduleCommandRequest{Kind: "create", Enable: true, Draft: &draft, Token: review.Token, Intent: "i-create"})
	if lost.State == desktop.Completed || !lost.Pending {
		t.Fatalf("an unanswered create read as done: %+v", lost)
	}
	hub.drop = false
	listing := p.app.ListSchedules(desktop.ScheduleListRequest{Context: p.context})
	if listing.State != desktop.Completed || len(listing.Schedules) != 1 || listing.Schedules[0].Pending != "create" {
		t.Fatalf("the pending create: %+v", listing)
	}
	// The same intent again: acknowledged once, with the first answer.
	sent := command(desktop.ScheduleCommandRequest{Kind: "create", Enable: true, Draft: &draft, Token: review.Token, Intent: "i-create"})
	if sent.State != desktop.Completed || !sent.Replayed || sent.Status != "enabled" || sent.Schedule != lost.Schedule || len(hub.schedules) != 1 {
		t.Fatalf("resend: %+v, hub holds %d", sent, len(hub.schedules))
	}
	row := p.app.ListSchedules(desktop.ScheduleListRequest{Context: p.context}).Schedules[0]
	if row.State != "enabled" || row.Pending != "" || row.Next == "" || row.Draft == nil || row.Draft.Suite.ID != suiteRef.ID || row.Suite != "Scheduling smoke" {
		t.Fatalf("the acknowledged schedule: %+v", row)
	}
	// A stopped service changes nothing and leaves nothing pending.
	hub.stopped = true
	if paused := command(desktop.ScheduleCommandRequest{Kind: "pause", Schedule: row.ID, Expected: row.Revision, Intent: "i-pause-1"}); paused.State == desktop.Completed || paused.Pending {
		t.Fatalf("a pause without the schedule service: %+v", paused)
	}
	hub.stopped = false
	if row = p.app.ListSchedules(desktop.ScheduleListRequest{Context: p.context}).Schedules[0]; row.State != "enabled" || row.Pending != "" {
		t.Fatalf("a refused pause changed the schedule: %+v", row)
	}
	hub.drop = true
	if paused := command(desktop.ScheduleCommandRequest{Kind: "pause", Schedule: row.ID, Expected: row.Revision, Intent: "i-pause-2"}); paused.State == desktop.Completed || !paused.Pending {
		t.Fatalf("an unanswered pause: %+v", paused)
	}
	hub.drop = false
	if row = p.app.ListSchedules(desktop.ScheduleListRequest{Context: p.context}).Schedules[0]; row.Pending != "pause" {
		t.Fatalf("the pending pause: %+v", row)
	}
	// Send again repeats exactly the kept change under its own intent.
	if paused := command(desktop.ScheduleCommandRequest{Kind: "resend", Schedule: row.ID, Intent: "i-resend"}); paused.State != desktop.Completed || paused.Status != "paused" || paused.Next != "" || !paused.Replayed {
		t.Fatalf("pause: %+v", paused)
	}
	// A stale revision is a conflict, not an overwrite.
	if stale := command(desktop.ScheduleCommandRequest{Kind: "enable", Schedule: row.ID, Expected: row.Revision - 1, Intent: "i-enable-stale"}); stale.State == desktop.Completed {
		t.Fatalf("a stale enable applied: %+v", stale)
	}
	// Filtering by suite and runner keeps only their schedules.
	if other := p.app.ListSchedules(desktop.ScheduleListRequest{Context: p.context, Suite: "0123456789abcdef01234567"}); len(other.Schedules) != 0 {
		t.Fatalf("another suite's filter: %+v", other.Schedules)
	}
	if mine := p.app.ListSchedules(desktop.ScheduleListRequest{Context: p.context, Runner: runnerRef.ID}); len(mine.Schedules) != 1 {
		t.Fatalf("the runner's filter: %+v", mine.Schedules)
	}
	row = p.app.ListSchedules(desktop.ScheduleListRequest{Context: p.context}).Schedules[0]
	if deleted := command(desktop.ScheduleCommandRequest{Kind: "delete", Schedule: row.ID, Expected: row.Revision, Intent: "i-delete"}); deleted.State != desktop.Completed || deleted.Status != "deleted" {
		t.Fatalf("delete: %+v", deleted)
	}
	if after := p.app.ListSchedules(desktop.ScheduleListRequest{Context: p.context}); len(after.Schedules) != 0 {
		t.Fatalf("a deleted schedule is listed: %+v", after.Schedules)
	}
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
