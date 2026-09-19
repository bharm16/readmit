package hub

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDailyScheduleDST(t *testing.T) {
	for _, tc := range []struct {
		day, at, want string
		exists        bool
	}{
		{"2026-03-08", "02:30", "", false},
		{"2026-11-01", "01:30", "2026-11-01T06:30:00Z", true},
		{"2026-09-19", "09:00", "2026-09-19T14:00:00Z", true},
	} {
		got, ok := DailyOccurrence(tc.day, tc.at, "America/Chicago")
		if ok != tc.exists || ok && got.Format(time.RFC3339) != tc.want {
			t.Fatalf("%+v: %v %v", tc, got, ok)
		}
	}
}

func scheduleFixture(t *testing.T, approved bool) (string, SchedulePolicy, time.Time) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "journal")
	p := SchedulePolicy{Schema: "readmit-hub-schedules/v1", Concurrency: "serial-skip-missed", Schedules: []Schedule{{ID: "daily", Zone: "UTC", At: "12:00", WindowSeconds: 60, Runner: "/private/runner.json", Spec: "/private/PHI-secret-spec.json", Input: strings.Repeat("a", 64), Route: "https://approved.example/", Approved: approved}}}
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	if e := InitializeSchedules(root, p, now.Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	return root, p, now
}
func TestScheduledDispatchRestartNotificationAndPrivacy(t *testing.T) {
	root, p, now := scheduleFixture(t, true)
	executed, notified := 0, 0
	execute := func(context.Context, Schedule, string) string { executed++; return "failed" }
	notify := func(_ context.Context, route string, b []byte) error {
		notified++
		if route != "https://approved.example/" || string(b) != `{"schema":"readmit-hub-alert/v1","state":"failed","coverage":"not-assessed"}` {
			t.Fatalf("unsafe summary %s", b)
		}
		return errors.New("secret diagnostic must not persist")
	}
	s, e := OpenScheduler(root, p, execute, notify)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Tick(context.Background(), now); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = OpenScheduler(root, p, execute, notify)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Tick(context.Background(), now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	h := s.History()
	if executed != 1 || notified != 1 || len(h.Records) != 1 || h.Records[0].Notification != "uncertain" {
		t.Fatalf("duplicated or incorrect state %+v %d %d", h, executed, notified)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "history.json"))
	if bytes.Contains(raw, []byte("secret")) {
		t.Fatal("diagnostic leaked")
	}
}
func TestScheduleCrashClaimNeverDispatchesAgain(t *testing.T) {
	root, p, now := scheduleFixture(t, false)
	calls := 0
	s, e := OpenScheduler(root, p, func(context.Context, Schedule, string) string { calls++; panic("process interruption") }, nil)
	if e != nil {
		t.Fatal(e)
	}
	func() { defer func() { recover() }(); s.Tick(context.Background(), now) }()
	s.Close()
	s, e = OpenScheduler(root, p, func(context.Context, Schedule, string) string { calls++; return "passed" }, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Tick(context.Background(), now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	if calls != 1 || s.History().Records[0].State != "uncertain" {
		t.Fatal("uncertain work repeated")
	}
}
func TestScheduleMissedWindowCancelledContextAndUnapprovedRoute(t *testing.T) {
	root, p, now := scheduleFixture(t, false)
	calls := 0
	s, e := OpenScheduler(root, p, func(context.Context, Schedule, string) string { calls++; return "passed" }, func(context.Context, string, []byte) error { t.Fatal("unapproved egress"); return nil })
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.Tick(ctx, now) == nil || len(s.History().Records) != 0 {
		t.Fatal("cancelled tick mutated history")
	}
	if e = s.Tick(context.Background(), now.Add(2*time.Minute)); e != nil {
		t.Fatal(e)
	}
	if calls != 0 || s.History().Records[0].State != "missed" {
		t.Fatal("missed window executed")
	}
	if e = s.Tick(context.Background(), now.Add(24*time.Hour)); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal("next daily occurrence did not execute")
	}
	if s.Tick(context.Background(), now) == nil {
		t.Fatal("clock rollback accepted")
	}
}
func TestSchedulePolicyStrictnessAndMissingState(t *testing.T) {
	root, p, _ := scheduleFixture(t, false)
	raw, _ := json.Marshal(p)
	for _, bad := range []string{`{}`, strings.Replace(string(raw), `"approved":false`, `"approved":null`, 1), strings.Replace(string(raw), `"zone":"UTC"`, `"zone":"Local"`, 1), strings.Replace(string(raw), `https://approved.example/`, `http://approved.example/`, 1), strings.Replace(string(raw), `https://approved.example/`, `https://approved.example/token`, 1), strings.Replace(string(raw), `"at":"12:00"`, `"at":"12:00","extra":1`, 1)} {
		if _, e := DecodeSchedules([]byte(bad)); e == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	p.Schedules[0].Spec = "/changed/spec.json"
	if _, e := OpenScheduler(root, p, func(context.Context, Schedule, string) string { return "passed" }, nil); e == nil {
		t.Fatal("changed authority accepted")
	}
	if _, e := OpenScheduler(filepath.Join(root, "missing"), p, func(context.Context, Schedule, string) string { return "passed" }, nil); e == nil {
		t.Fatal("missing claims accepted")
	}
}

func TestScheduleBackupRefusesToDropClaims(t *testing.T) {
	directory := t.TempDir()
	if e := os.Mkdir(filepath.Join(directory, "scheduler"), 0700); e != nil {
		t.Fatal(e)
	}
	root, e := os.OpenRoot(directory)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	store := &Store{root: root}
	if e = store.Backup(context.Background(), filepath.Join(t.TempDir(), "backup")); !errors.Is(e, ErrSchedule) {
		t.Fatalf("scheduler claims omitted: %v", e)
	}
}
func TestScheduleCorruptJournalRefuses(t *testing.T) {
	root, p, now := scheduleFixture(t, false)
	s, e := OpenScheduler(root, p, func(context.Context, Schedule, string) string { return "passed" }, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Tick(context.Background(), now); e != nil {
		t.Fatal(e)
	}
	s.Close()
	path := filepath.Join(root, "history.json")
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	b = bytes.Replace(b, []byte(`"state":"passed"`), []byte(`"state":"unsupported"`), 1)
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = OpenScheduler(root, p, func(context.Context, Schedule, string) string { return "passed" }, nil); e == nil {
		t.Fatal("corrupt journal admitted")
	}
}

func TestScheduleWithdrawalDuringExecutionStopsLaterWorkAndEgress(t *testing.T) {
	root, p, now := scheduleFixture(t, true)
	// Replace initialized fixture before any scheduler has started.
	os.Remove(filepath.Join(root, "history.json"))
	os.Remove(root)
	other := p.Schedules[0]
	other.ID = "other"
	p.Schedules = append(p.Schedules, other)
	if e := InitializeSchedules(root, p, now.Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	authorized := true
	calls := 0
	authority := func() error {
		if !authorized {
			return ErrSchedule
		}
		return nil
	}
	s, e := OpenScheduler(root, p, func(context.Context, Schedule, string) string { calls++; authorized = false; return "passed" }, func(context.Context, string, []byte) error { t.Fatal("withdrawn egress approval used"); return nil }, authority)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Tick(context.Background(), now); !errors.Is(e, ErrSchedule) {
		t.Fatal("policy withdrawal ignored", e)
	}
	if calls != 1 {
		t.Fatal("later execution admitted")
	}
}
func TestScheduleActiveCancellationAndSerialMissedWindow(t *testing.T) {
	root, p, now := scheduleFixture(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	s, e := OpenScheduler(root, p, func(ctx context.Context, _ Schedule, _ string) string { cancel(); <-ctx.Done(); return "uncertain" }, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Tick(ctx, now); e != nil {
		t.Fatal(e)
	}
	if s.History().Records[0].State != "uncertain" {
		t.Fatal("cancellation erased possible delivery")
	}
}

func TestScheduleHTTPSAlertSuccessRedirectRefusalAndCancellation(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "POST" || r.Header.Get("Authorization") != "" {
			t.Error("unexpected method or credentials")
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != `{"schema":"readmit-hub-alert/v1","state":"failed","coverage":"not-assessed"}` {
			t.Error("changed summary")
		}
		if requests == 2 {
			w.Header().Set("Location", "https://elsewhere.invalid/")
			w.WriteHeader(307)
			return
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	body := []byte(`{"schema":"readmit-hub-alert/v1","state":"failed","coverage":"not-assessed"}`)
	if e := scheduleAlert(context.Background(), server.URL+"/", body, roots); e != nil {
		t.Fatal(e)
	}
	if e := scheduleAlert(context.Background(), server.URL+"/", body, roots); e == nil {
		t.Fatal("redirect accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := scheduleAlert(ctx, server.URL+"/", body, roots); e == nil {
		t.Fatal("cancelled request succeeded")
	}
	if requests != 2 {
		t.Fatal("redirect followed or cancelled notification delivered")
	}
}

func TestScheduleNotificationCrashNeverRetries(t *testing.T) {
	root, p, now := scheduleFixture(t, true)
	attempts := 0
	s, e := OpenScheduler(root, p, func(context.Context, Schedule, string) string { return "passed" }, func(context.Context, string, []byte) error { attempts++; panic("interrupted transport") })
	if e != nil {
		t.Fatal(e)
	}
	func() { defer func() { recover() }(); s.Tick(context.Background(), now) }()
	s.Close()
	s, e = OpenScheduler(root, p, func(context.Context, Schedule, string) string { t.Fatal("execution repeated"); return "error" }, func(context.Context, string, []byte) error { attempts++; return nil })
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Tick(context.Background(), now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	if attempts != 1 || s.History().Records[0].Notification != "uncertain" {
		t.Fatal("uncertain notification retried")
	}
}
func TestScheduleSerialQueueReportsExpiredWindow(t *testing.T) {
	root, p, now := scheduleFixture(t, false)
	os.Remove(filepath.Join(root, "history.json"))
	os.Remove(root)
	p.Schedules[0].WindowSeconds = 1
	second := p.Schedules[0]
	second.ID = "second"
	p.Schedules = append(p.Schedules, second)
	if e := InitializeSchedules(root, p, now.Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	calls := 0
	s, e := OpenScheduler(root, p, func(context.Context, Schedule, string) string {
		calls++
		time.Sleep(1100 * time.Millisecond)
		return "passed"
	}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Tick(context.Background(), now); e != nil {
		t.Fatal(e)
	}
	h := s.History()
	if calls != 1 || len(h.Records) != 2 || h.Records[1].State != "missed" {
		t.Fatalf("expired queued occurrence dispatched %+v", h)
	}
}

func TestScheduleMidnightGapWaitsForActualNextLocalDay(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "journal")
	p := SchedulePolicy{Schema: "readmit-hub-schedules/v1", Concurrency: "serial-skip-missed", Schedules: []Schedule{{ID: "daily", Zone: "America/Santiago", At: "00:30", WindowSeconds: 60, Runner: "/private/runner.json", Spec: "/private/spec.json", Input: strings.Repeat("a", 64)}}}
	start := time.Date(2026, 9, 6, 3, 59, 0, 0, time.UTC)
	if e := InitializeSchedules(dir, p, start); e != nil {
		t.Fatal(e)
	}
	s, e := OpenScheduler(dir, p, func(context.Context, Schedule, string) string {
		t.Fatal("nonexistent occurrence executed")
		return "error"
	}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Tick(context.Background(), time.Date(2026, 9, 7, 2, 30, 0, 0, time.UTC)); e != nil {
		t.Fatal(e)
	}
	if len(s.History().Records) != 0 {
		t.Fatal("gap reported before actual next local day")
	}
	boundary := time.Date(2026, 9, 7, 3, 0, 0, 0, time.UTC)
	if e = s.Tick(context.Background(), boundary); e != nil {
		t.Fatal(e)
	}
	h := s.History()
	if len(h.Records) != 1 || h.Records[0].State != "dst-gap" || !h.Records[0].Due.Equal(boundary) {
		t.Fatalf("wrong midnight gap boundary %+v", h)
	}
}

func TestScheduleMidnightFoldAndSkippedCivilDay(t *testing.T) {
	due, ok := DailyOccurrence("2026-11-01", "00:30", "America/Havana")
	if !ok || due.Format(time.RFC3339) != "2026-11-01T04:30:00Z" {
		t.Fatalf("midnight fold did not select first occurrence %v %v", due, ok)
	}
	if _, ok := DailyOccurrence("2011-12-30", "12:00", "Pacific/Apia"); ok {
		t.Fatal("skipped civil day acquired an occurrence")
	}

	for _, tc := range []struct{ day, want string }{
		{"2026-11-01", "2026-11-01T04:00:00Z"},
		{"2026-11-02", "2026-11-02T05:00:00Z"},
	} {
		got, ok := DailyOccurrence(tc.day, "00:00", "America/Havana")
		if !ok || got.Format(time.RFC3339) != tc.want {
			t.Fatalf("%+v: %v %v", tc, got, ok)
		}
	}
	dir := filepath.Join(t.TempDir(), "journal")
	p := SchedulePolicy{Schema: "readmit-hub-schedules/v1", Concurrency: "serial-skip-missed", Schedules: []Schedule{{ID: "daily", Zone: "Pacific/Apia", At: "12:00", WindowSeconds: 60, Runner: "/private/runner.json", Spec: "/private/spec.json", Input: strings.Repeat("a", 64)}}}
	boundary := time.Date(2011, 12, 30, 10, 0, 0, 0, time.UTC)
	if e := InitializeSchedules(dir, p, boundary.Add(-time.Minute)); e != nil {
		t.Fatal(e)
	}
	s, e := OpenScheduler(dir, p, func(context.Context, Schedule, string) string { t.Fatal("skipped civil day executed"); return "error" }, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Tick(context.Background(), boundary); e != nil {
		t.Fatal(e)
	}
	h := s.History()
	s.Close()
	if len(h.Records) != 1 || h.Records[0].Day != "2011-12-30" || h.Records[0].State != "dst-gap" || !h.Records[0].Due.Equal(boundary) {
		t.Fatalf("skipped day report %+v", h)
	}
	s, e = OpenScheduler(dir, p, func(context.Context, Schedule, string) string { return "error" }, nil)
	if e != nil {
		t.Fatal("skipped day history did not reopen", e)
	}
	s.Close()
}
