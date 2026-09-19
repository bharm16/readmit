package hub_test

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/profileversion"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostgresCollaborationCASAndIdentity(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	s := open(t, c)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	a, _, key, _ := accessFixture(t)
	h := s.TeamHandler(a)
	call := func(method, path, subject, body string) *httptest.ResponseRecorder {
		r := request(signed(t, key, claims(subject), accessHeader))
		r.Method = method
		r.URL.Path = path
		r.Body = httpBody([]byte(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	payload := "synthetic case"
	d := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	if w := call("PUT", "/v1/projects/alpha/artifacts/"+d, "analyst", payload); w.Code != 201 {
		t.Fatal(w.Code)
	}
	body := fmt.Sprintf(`{"schema":"readmit-hub-review-command/v1","id":"first","expected":0,"kind":"comment","evidence":%q,"parent":"","recipient":"reviewer","text":"Investigate synthetic mismatch","release":""}`, d)
	if w := call("POST", "/v1/projects/alpha/reviews", "analyst", body); w.Code != 201 {
		t.Fatalf("comment: %d %s", w.Code, w.Body)
	}
	if w := call("POST", "/v1/projects/alpha/reviews", "analyst", body); w.Code != 200 {
		t.Fatalf("safe retry: %d", w.Code)
	}
	if w := call("GET", "/v1/projects/alpha/notifications", "reviewer", ""); w.Code != 200 {
		t.Fatalf("notifications: %d", w.Code)
	}
}

func TestPostgresApprovedReleaseHistoryAndRecovery(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	s := open(t, c)
	ctx := context.Background()
	if e := s.Migrate(ctx); e != nil {
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
	put := func(data []byte) string {
		d := fmt.Sprintf("%x", sha256.Sum256(data))
		if w := call("PUT", "artifacts/"+d, "analyst", string(data)); w.Code != 201 {
			t.Fatalf("put %d", w.Code)
		}
		return d
	}
	evidence := put([]byte("synthetic evidence"))
	spec := []byte(`{"schema":"readmit-test/v1","name":"synthetic","input":{"case":"case","messages":["s0001-e000001"]},"target":"target.json","setup":{"initial_state":"operator-declared","reset_instructions":"Reset fixture"},"observation":{"boundary":"ack-contract"},"assertions":[{"id":"ack","operator":"ack_field_equals","message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"AA"}}}]}`)
	review, e := expectation.Review("booking", spec, []profileversion.Version{}, nil, false)
	if e != nil {
		t.Fatal(e)
	}
	release, e := expectation.Approve("booking", spec, []profileversion.Version{}, nil, review.Identity, "Untrusted local label", "synthetic")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := release.Encode()
	if e != nil {
		t.Fatal(e)
	}
	digest := put(raw)
	command := func(id, kind, parent, recipient, rel, text string, expected int) string {
		b, e := json.Marshal(hub.ReviewCommand{Schema: "readmit-hub-review-command/v1", ID: id, Expected: expected, Kind: kind, Evidence: evidence, Parent: parent, Recipient: recipient, Release: rel, Text: text})
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	check := func(body, subject string, want int) {
		t.Helper()
		w := call("POST", "reviews", subject, body)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s want %d", subject, body, w.Code, w.Body, want)
		}
	}
	check(command("assign", "assignment", "", "analyst", "", "Investigate", 0), "analyst", 201)
	check(command("stale", "comment", "", "", "", "stale edit", 0), "analyst", 409)
	check(command("comment", "comment", "", "reviewer", "", "Synthetic needle", 1), "analyst", 201)
	check(command("reply", "comment", "comment", "analyst", "", "Reviewed needle", 2), "reviewer", 201)
	check(command("viewer-write", "comment", "", "", "", "refused", 3), "viewer", 403)
	check(command("missing-parent", "comment", "absent", "", "", "refused", 3), "analyst", 404)
	check(command("request", "review-request", "", "reviewer", digest, "Review exact release", 3), "analyst", 201)
	approval := command("approve", "approval", "request", "", digest, "Accepted exact expectations", 4)
	check(approval, "analyst", 403)
	check(approval, "owner", 403)
	check(approval, "reviewer", 201)
	check(approval, "reviewer", 200)
	check(command("repeat", "approval", "request", "", digest, "Second approval", 5), "reviewer", 409)
	// Same test identity cannot restart the chain after its first team approval.
	check(command("fork-request", "review-request", "", "reviewer", digest, "Old version", 5), "analyst", 201)
	check(command("fork-approve", "approval", "fork-request", "", digest, "Old version", 6), "reviewer", 409)
	changed := []byte(strings.Replace(string(spec), `"AA"`, `"AE"`, 1))
	nextReview, e := expectation.Review("booking", changed, []profileversion.Version{}, &release, false)
	if e != nil {
		t.Fatal(e)
	}
	next, e := expectation.Approve("booking", changed, []profileversion.Version{}, &release, nextReview.Identity, "Local second label", "synthetic changed")
	if e != nil {
		t.Fatal(e)
	}
	nextRaw, e := next.Encode()
	if e != nil {
		t.Fatal(e)
	}
	nextDigest := put(nextRaw)
	check(command("second-request", "review-request", "", "reviewer", nextDigest, "Changed exact release", 6), "analyst", 201)
	check(command("wrong-release", "approval", "second-request", "", digest, "Changed after request", 7), "reviewer", 403)
	check(command("second-approval", "approval", "second-request", "", nextDigest, "Accepted changed expectations", 7), "reviewer", 201)
	history := call("GET", "history", "viewer", "")
	if history.Code != 200 || !strings.Contains(history.Body.String(), `"actor":"reviewer"`) || strings.Contains(history.Body.String(), "Untrusted local label") {
		t.Fatalf("identity %d %s", history.Code, history.Body)
	}
	search := `{"schema":"readmit-hub-review-query/v1","after":0,"text":"needle","evidence":""}`
	w := call("POST", "history", "viewer", search)
	var result struct {
		Head   int               `json:"head"`
		Events []hub.ReviewEvent `json:"events"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil || w.Code != 200 || len(result.Events) != 2 || result.Head != 8 {
		t.Fatalf("search %d %s %v", w.Code, w.Body, e)
	}
	// Nested contract omissions and forged actor fields never reach storage.
	for _, body := range []string{strings.Replace(approval, `"expected":4,`, "", 1), strings.Replace(approval, `"schema":`, `"actor":"forged","schema":`, 1), strings.Replace(approval, `"expected":4`, `"expected":null`, 1)} {
		check(body, "reviewer", 400)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if e = s.Backup(ctx, backup); e != nil {
		t.Fatal(e)
	}
	original := history.Body.String()
	s.Close()
	reset(t, db)
	c.Root = filepath.Join(t.TempDir(), "restored")
	s = open(t, c)
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	h = s.TeamHandler(a)
	manifestPath := filepath.Join(backup, "manifest.json")
	originalManifest, e := os.ReadFile(manifestPath)
	if e != nil {
		t.Fatal(e)
	}
	for name, broken := range map[string]string{
		"missing nested expected": strings.Replace(string(originalManifest), `"expected":0,`, "", 1),
		"unknown event":           strings.Replace(string(originalManifest), `"actor":`, `"unexpected":true,"actor":`, 1),
		"dangling parent":         strings.Replace(string(originalManifest), `"parent":"comment"`, `"parent":"missing"`, 1),
		"forged approver":         strings.Replace(string(originalManifest), `"actor":"reviewer"`, `"actor":"analyst"`, -1),
		"sequence gap":            strings.Replace(string(originalManifest), `"sequence":1`, `"sequence":100`, 1),
	} {
		if broken == string(originalManifest) {
			t.Fatal("ineffective mutation", name)
		}
		if e = os.WriteFile(manifestPath, []byte(broken), 0600); e != nil {
			t.Fatal(e)
		}
		if e = s.Restore(ctx, backup); e == nil {
			t.Fatal("accepted broken backup", name)
		}
		if w := call("GET", "history", "viewer", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"events":[]`) {
			t.Fatal("rejected restore published reviews", w.Code, w.Body)
		}
		if w := call("GET", "artifacts/"+evidence, "viewer", ""); w.Code != 404 {
			t.Fatal("rejected restore published evidence", w.Code)
		}
	}
	if e = os.WriteFile(manifestPath, originalManifest, 0600); e != nil {
		t.Fatal(e)
	}
	cancelRestoreCtx, cancelRestore := context.WithCancel(ctx)
	cancelRestore()
	if e = s.Restore(cancelRestoreCtx, backup); e == nil {
		t.Fatal("cancelled restore succeeded")
	}
	evidencePath := filepath.Join(backup, evidence)
	originalEvidence, e := os.ReadFile(evidencePath)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(evidencePath, []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.Restore(ctx, backup); e == nil {
		t.Fatal("corrupt evidence restored")
	}
	if e = os.Remove(evidencePath); e != nil {
		t.Fatal(e)
	}
	if e = s.Restore(ctx, backup); e == nil {
		t.Fatal("missing evidence restored")
	}
	if w := call("GET", "history", "viewer", ""); !strings.Contains(w.Body.String(), `"events":[]`) {
		t.Fatal("failed restore changed history")
	}
	if e = os.WriteFile(evidencePath, originalEvidence, 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.Restore(ctx, backup); e != nil {
		t.Fatal(e)
	}
	h = s.TeamHandler(a)
	if w := call("GET", "history", "viewer", ""); w.Code != 200 || w.Body.String() != original {
		t.Fatalf("restored history %d %s", w.Code, w.Body)
	}
	// Removing a reviewer blocks a previously valid, idempotent approval retry.
	for i, g := range policy.Grants {
		if g.Subject == "reviewer" {
			policy.Grants = append(policy.Grants[:i], policy.Grants[i+1:]...)
			break
		}
	}
	writePolicy(t, policyPath, policy)
	check(approval, "reviewer", 403)
	r := request(signed(t, key, claims("analyst"), accessHeader))
	r.Method = "POST"
	r.URL.Path = "/v1/projects/alpha/reviews"
	r.Body = httpBody([]byte(command("cancelled", "comment", "", "", "", "Cancelled", 8)))
	cancelctx, cancel := context.WithCancel(ctx)
	cancel()
	r = r.WithContext(cancelctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Fatal("cancelled mutation", rec.Code)
	}
	if w := call("GET", "history", "viewer", ""); w.Body.String() != original {
		t.Fatal("cancelled write changed history")
	}
}

func TestPostgresConcurrentReviewConflictAndProjectIsolation(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	s := open(t, c)
	if e := s.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	a, p, key, path := accessFixture(t)
	p.Grants = append(p.Grants, hub.ProjectGrant{Project: "beta", Subject: "analyst", Role: "analyst"})
	writePolicy(t, path, p)
	h := s.TeamHandler(a)
	call := func(project, method, route, body string) int {
		r := request(signed(t, key, claims("analyst"), accessHeader))
		r.Method = method
		r.URL.Path = "/v1/projects/" + project + "/" + route
		r.Body = httpBody([]byte(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	payload := "synthetic concurrent case"
	d := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	if code := call("alpha", "PUT", "artifacts/"+d, payload); code != 201 {
		t.Fatal(code)
	}
	body := func(id string) string {
		return fmt.Sprintf(`{"schema":"readmit-hub-review-command/v1","id":%q,"expected":0,"kind":"comment","evidence":%q,"parent":"","recipient":"","text":"Concurrent edit","release":""}`, id, d)
	}
	if code := call("beta", "POST", "reviews", body("foreign")); code != 404 {
		t.Fatal("foreign artifact", code)
	}
	results := make(chan int, 2)
	start := make(chan struct{})
	for _, id := range []string{"one", "two"} {
		go func() { <-start; results <- call("alpha", "POST", "reviews", body(id)) }()
	}
	close(start)
	first, second := <-results, <-results
	if !((first == 201 && second == 409) || (first == 409 && second == 201)) {
		t.Fatalf("concurrent result %d %d", first, second)
	}
}
