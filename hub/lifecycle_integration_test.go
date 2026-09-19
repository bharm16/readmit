package hub_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"github.com/bharm16/readmit/hub"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostgresOfflineRevisionConflictResolution(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	s := open(t, c)
	if e := s.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	a, _, key, _ := accessFixture(t)
	h := s.TeamHandler(a)
	call := func(method, path, subject, body string) *httptest.ResponseRecorder {
		r := request(signed(t, key, claims(subject), accessHeader))
		r.Method = method
		r.URL.Path = "/v1/projects/alpha/" + path
		r.Body = httpBody([]byte(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	put := func(payload string) string {
		d := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
		if w := call("PUT", "artifacts/"+d, "analyst", payload); w.Code != 201 {
			t.Fatal(w.Code)
		}
		return d
	}
	first, left, right, merged := put("synthetic original"), put("synthetic left"), put("synthetic right"), put("synthetic merged")
	command := func(id, kind, d string, parents []string, expected int) string {
		b, _ := json.Marshal(map[string]any{"schema": "readmit-hub-lifecycle-command/v1", "id": id, "expected": expected, "kind": kind, "resource": "case-one", "artifact": d, "parents": parents, "subject": "", "until": "", "reason": "Synthetic offline edit"}, json.Deterministic(true))
		return string(b)
	}
	check := func(body, subject string, want int) {
		t.Helper()
		w := call("POST", "lifecycle", subject, body)
		if w.Code != want {
			t.Fatalf("want %d got %d %s", want, w.Code, w.Body)
		}
	}
	initial := command("first", "revision", first, []string{}, 0)
	for _, broken := range []string{
		strings.Replace(initial, `"expected":0,`, "", 1),
		strings.Replace(initial, `"expected":0`, `"expected":null`, 1),
		strings.Replace(initial, `"expected":0`, `"expected":0,"expected":0`, 1),
		strings.Replace(initial, `"schema":`, `"actor":"forged","schema":`, 1),
		strings.Replace(initial, `"parents":[]`, `"parents":null`, 1),
	} {
		if broken == initial {
			t.Fatal("ineffective mutation")
		}
		check(broken, "analyst", 400)
	}

	check(initial, "analyst", 201)
	check(initial, "analyst", 200)
	check(command("left", "revision", left, []string{"first"}, 1), "analyst", 201)
	check(command("right", "revision", right, []string{"first"}, 2), "owner", 201)
	check(command("stale", "resolve", merged, []string{"left", "right"}, 2), "analyst", 409)
	check(command("partial", "resolve", merged, []string{"left"}, 3), "analyst", 400)
	check(command("unknown", "resolve", merged, []string{"left", "missing"}, 3), "analyst", 409)
	check(command("resolve", "resolve", merged, []string{"left", "right"}, 3), "viewer", 403)
	check(command("resolve", "resolve", merged, []string{"left", "right"}, 3), "analyst", 201)
	w := call("GET", "lifecycle", "viewer", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"tips":{"case-one":["resolve"]}`) {
		t.Fatalf("history %d %s", w.Code, w.Body)
	}
	for _, d := range []string{first, left, right, merged} {
		if w := call("GET", "artifacts/"+d, "viewer", ""); w.Code != 200 {
			t.Fatal("lost original", w.Code)
		}
	}
	original := call("GET", "lifecycle", "viewer", "").Body.String()
	backup := filepath.Join(t.TempDir(), "revision-backup")
	if e := s.Backup(context.Background(), backup); e != nil {
		t.Fatal(e)
	}
	s.Close()
	reset(t, db)
	c.Root = filepath.Join(t.TempDir(), "revision-restored")
	s = open(t, c)
	if e := s.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	h = s.TeamHandler(a)
	if e := s.Restore(context.Background(), backup); e != nil {
		t.Fatal(e)
	}
	if w := call("GET", "lifecycle", "viewer", ""); w.Body.String() != original {
		t.Fatal("lost revision graph", w.Code, w.Body)
	}
	// Cancellation cannot leave a new revision event behind.
	r := request(signed(t, key, claims("analyst"), accessHeader))
	r.Method = "POST"
	r.URL.Path = "/v1/projects/alpha/lifecycle"
	r.Body = httpBody([]byte(command("cancelled", "revision", merged, []string{"resolve"}, 4)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r.WithContext(ctx))
	if w.Code != 403 {
		t.Fatal("cancelled mutation", w.Code)
	}
	if w := call("GET", "lifecycle", "viewer", ""); w.Body.String() != original {
		t.Fatal("cancelled changed history")
	}

}

func TestPostgresLifecycleAdministration(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	s := open(t, c)
	if e := s.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	a, policy, key, policyPath := accessFixture(t)
	h := s.TeamHandler(a)
	call := func(method, path, subject, body string) *httptest.ResponseRecorder {
		r := request(signed(t, key, claims(subject), accessHeader))
		r.Method = method
		r.URL.Path = "/v1/projects/alpha/" + path
		r.Body = httpBody([]byte(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	d := fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic retention")))
	if w := call("PUT", "artifacts/"+d, "analyst", "synthetic retention"); w.Code != 201 {
		t.Fatal(w.Code)
	}
	cmd := func(id, kind, artifact, subject, until string, expected int) string {
		b, _ := json.Marshal(map[string]any{"schema": "readmit-hub-lifecycle-command/v1", "id": id, "expected": expected, "kind": kind, "resource": "", "artifact": artifact, "parents": []string{}, "subject": subject, "until": until, "reason": "Synthetic administration"})
		return string(b)
	}
	check := func(body, subject string, want int) {
		t.Helper()
		w := call("POST", "lifecycle", subject, body)
		if w.Code != want {
			t.Fatalf("want %d got %d %s", want, w.Code, w.Body)
		}
	}
	check(cmd("remove", "remove-user", "", "viewer", "", 0), "analyst", 403)
	check(cmd("remove", "remove-user", "", "viewer", "", 0), "owner", 201)
	for _, path := range []string{"history", "lifecycle", "artifacts/" + d} {
		if w := call("GET", path, "viewer", ""); w.Code != 403 {
			t.Fatal("removed user access", path, w.Code)
		}
	}
	check(cmd("self", "remove-user", "", "owner", "", 1), "owner", 403)
	check(cmd("hold", "retention", d, "", "2099-01-01T00:00:00Z", 1), "owner", 201)
	check(cmd("early", "retire", d, "", "", 2), "owner", 409)
	check(cmd("shorten", "retention", d, "", "2000-01-01T00:00:00Z", 2), "owner", 409)
	check(cmd("export", "audit-export", "", "", "", 2), "analyst", 403)
	w := call("POST", "lifecycle", "owner", cmd("export", "audit-export", "", "", "", 2))
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"schema":"readmit-hub-audit/v1"`) || !strings.Contains(w.Body.String(), `"kind":"remove-user"`) {
		t.Fatalf("audit %d %s", w.Code, w.Body)
	}
	token := "rh_" + base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	policy.Tokens = []hub.ScopedToken{{Hash: fmt.Sprintf("%x", sha256.Sum256([]byte(token))), Subject: "runner", Project: "alpha", Actions: []string{"enrollment", "execution"}, Expires: "2099-01-01T00:00:00Z", Certificate: fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic-client-cert"))), Kind: "runner"}}
	writePolicy(t, policyPath, policy)
	runnerCall := func(path string) int {
		r := request(token)
		r.Method = "POST"
		r.URL.Path = "/v1/projects/alpha/" + path
		r.Body = httpBody([]byte(`{}`))
		w := httptest.NewRecorder()
		s.RunnerHandler(a, filepath.Join(t.TempDir(), "missing-policy")).ServeHTTP(w, r)
		return w.Code
	}
	if code := runnerCall("enrollment"); code != 204 {
		t.Fatal("initial runner admission", code)
	}
	check(cmd("remove-runner", "remove-user", "", "runner", "", 3), "owner", 201)
	for _, path := range []string{"enrollment", "runner"} {
		if code := runnerCall(path); code != 403 {
			t.Fatal("removed runner", path, code)
		}
	}
	check(cmd("remove-reviewer", "remove-user", "", "reviewer", "", 4), "owner", 201)
	reviewBody := fmt.Sprintf(`{"schema":"readmit-hub-review-command/v1","id":"removed-comment","expected":0,"kind":"comment","evidence":%q,"parent":"","recipient":"","text":"refused","release":""}`, d)
	if w := call("POST", "reviews", "reviewer", reviewBody); w.Code != 403 {
		t.Fatal("removed reviewer", w.Code)
	}

	policy.Issuer = "https://example.invalid/" + strings.Repeat("x", 2048)
	writePolicy(t, policyPath, policy)
	longClaims := claims("owner")
	longClaims["iss"] = policy.Issuer
	r := request(signed(t, key, longClaims, accessHeader))
	r.Method = "POST"
	r.URL.Path = "/v1/projects/alpha/lifecycle"
	r.Body = httpBody([]byte(cmd("oversize-issuer", "audit-export", "", "", "", 5)))
	over := httptest.NewRecorder()
	h.ServeHTTP(over, r)
	if over.Code != 403 {
		t.Fatal("persisted issuer beyond backup bound", over.Code)
	}
	policy.Issuer = "https://example.invalid/" + strings.Repeat("x", 2048-len("https://example.invalid/"))
	writePolicy(t, policyPath, policy)
	edgeClaims := claims("owner")
	edgeClaims["iss"] = policy.Issuer
	edge := request(signed(t, key, edgeClaims, accessHeader))
	edge.Method = "POST"
	edge.URL.Path = "/v1/projects/alpha/lifecycle"
	edge.Body = httpBody([]byte(cmd("maximum-issuer", "audit-export", "", "", "", 5)))
	accepted := httptest.NewRecorder()
	h.ServeHTTP(accepted, edge)
	if accepted.Code != 201 {
		t.Fatal("refused maximum issuer", accepted.Code)
	}
	backup := filepath.Join(t.TempDir(), "maximum-issuer-backup")
	if e := s.Backup(context.Background(), backup); e != nil {
		t.Fatal(e)
	}
	if e := hub.VerifyBackup(context.Background(), backup); e != nil {
		t.Fatal("maximum issuer unreadable", e)
	}

}

func TestPostgresLifecycleBackupRetirementRecovery(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	s := open(t, c)
	ctx := context.Background()
	if e := s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	a, _, key, _ := accessFixture(t)
	h := s.TeamHandler(a)
	call := func(method, path, subject, body string) *httptest.ResponseRecorder {
		r := request(signed(t, key, claims(subject), accessHeader))
		r.Method = method
		r.URL.Path = "/v1/projects/alpha/" + path
		r.Body = httpBody([]byte(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	payload := "synthetic retained original"
	d := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	if w := call("PUT", "artifacts/"+d, "analyst", payload); w.Code != 201 {
		t.Fatal(w.Code)
	}
	// The disconnected client obtains the exact bytes, edits its private copy,
	// and later submits an immutable revision document with explicit parents.
	if w := call("GET", "artifacts/"+d, "analyst", ""); w.Code != 200 || w.Body.String() != payload {
		t.Fatal("offline copy", w.Code)
	}
	command := func(id, kind, artifact, subject, until string, expected int) string {
		b, _ := json.Marshal(hub.LifecycleCommand{Schema: "readmit-hub-lifecycle-command/v1", ID: id, Kind: kind, Artifact: artifact, Subject: subject, Until: until, Expected: expected, Parents: []string{}, Reason: "Synthetic lifecycle"})
		return string(b)
	}
	check := func(body string, want int) {
		t.Helper()
		w := call("POST", "lifecycle", "owner", body)
		if w.Code != want {
			t.Fatalf("want %d got %d %s", want, w.Code, w.Body)
		}
	}
	check(command("retain", "retention", d, "", "2000-01-01T00:00:00Z", 0), 201)
	check(command("retire", "retire", d, "", "", 1), 201)
	if w := call("GET", "artifacts/"+d, "owner", ""); w.Code != 410 {
		t.Fatal("retirement", w.Code)
	}
	if w := call("PUT", "artifacts/"+d, "owner", payload); w.Code != 410 {
		t.Fatal("retirement undo", w.Code)
	}
	check(command("remove", "remove-user", "", "viewer", "", 2), 201)
	history := call("GET", "lifecycle", "owner", "").Body.String()
	backup := filepath.Join(t.TempDir(), "backup")
	if e := s.Backup(ctx, backup); e != nil {
		t.Fatal(e)
	}
	if e := hub.VerifyBackup(ctx, backup); e != nil {
		t.Fatal("verify", e)
	}
	manifest := filepath.Join(backup, "manifest.json")
	original, e := os.ReadFile(manifest)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(original), `"schema":"readmit-hub-backup/v4"`) {
		t.Fatal("missing new backup contract")
	}
	s.Close()
	reset(t, db)
	c.Root = filepath.Join(t.TempDir(), "restored")
	s = open(t, c)
	if e := s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	h = s.TeamHandler(a)
	for name, broken := range map[string]string{
		"missing review head": strings.Replace(string(original), `"review_head":0,`, "", 1),
		"self removal":        strings.Replace(string(original), `"subject":"viewer"`, `"subject":"owner"`, 1),
		"early retirement":    strings.Replace(string(original), "2000-01-01T00:00:00Z", "2099-01-01T00:00:00Z", 1),
		"head gap":            strings.Replace(string(original), `"sequence":1`, `"sequence":99`, 1),
	} {
		if broken == string(original) {
			t.Fatal("ineffective mutation", name)
		}
		if e := os.WriteFile(manifest, []byte(broken), 0600); e != nil {
			t.Fatal(e)
		}
		if e := hub.VerifyBackup(ctx, backup); e == nil {
			t.Fatal("verified corrupt", name)
		}
		if e := s.Restore(ctx, backup); e == nil {
			t.Fatal("restored corrupt", name)
		}
	}
	if e := os.WriteFile(manifest, original, 0600); e != nil {
		t.Fatal(e)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if e := hub.VerifyBackup(cancelled, backup); e == nil {
		t.Fatal("verified cancelled")
	}
	if e := s.Restore(cancelled, backup); e == nil {
		t.Fatal("restored cancelled")
	}
	if e := os.WriteFile(filepath.Join(backup, d), []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := hub.VerifyBackup(ctx, backup); e == nil {
		t.Fatal("verified corrupt bytes")
	}
	if e := s.Restore(ctx, backup); e == nil {
		t.Fatal("restored corrupt bytes")
	}
	if e := os.WriteFile(filepath.Join(backup, d), []byte(payload), 0600); e != nil {
		t.Fatal(e)
	}
	if e := s.Restore(ctx, backup); e != nil {
		t.Fatal(e)
	}
	if w := call("GET", "lifecycle", "owner", ""); w.Body.String() != history {
		t.Fatal("lost restored history", w.Code, w.Body)
	}
	if w := call("GET", "history", "viewer", ""); w.Code != 403 {
		t.Fatal("removal lost", w.Code)
	}
	if w := call("GET", "artifacts/"+d, "owner", ""); w.Code != 410 {
		t.Fatal("retirement lost", w.Code)
	}
	if data, e := s.Get(ctx, d); e != nil || string(data) != payload {
		t.Fatal("recovery bytes lost", e)
	}
}
