package hub_test

import (
	"context"
	"encoding/json/v2"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/sharing"
)

func TestPostgresReviewedSupportIdentityPolicyAndRecovery(t *testing.T) {
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
	call := func(method, route, actor string, data []byte) *httptest.ResponseRecorder {
		r := request(signed(t, key, claims(actor), accessHeader))
		r.Method = method
		r.URL.Path = route
		r.Body = httpBody(data)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	put := func(raw []byte) string {
		d := sharing.Digest(raw)
		if w := call("PUT", "/v1/projects/alpha/artifacts/"+d, "analyst", raw); w.Code != 201 {
			t.Fatalf("put %d %s", w.Code, w.Body)
		}
		return d
	}
	policy := []byte(`{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["customer-hub-download"],"max_bytes":4096}`)
	p := put(policy)
	summary := sharing.Summary{Schema: sharing.Schema, SourceKind: "retained-packet", SourceIdentity: strings.Repeat("1", 64), InputCommitment: strings.Repeat("2", 64), SpecIdentity: strings.Repeat("3", 64), PolicyIdentity: p, Outcome: "assertion_failure", ExternalEquivalence: "declined", Scope: sharing.Scope}
	raw, _ := json.Marshal(summary, json.Deterministic(true))
	d := put(raw)
	command := func(id, kind, evidence, policy, parent, recipient string, expected int) []byte {
		b, e := json.Marshal(hub.ReviewCommand{Schema: "readmit-hub-review-command/v2", ID: id, Expected: expected, Kind: kind, Evidence: evidence, Release: policy, Parent: parent, Recipient: recipient, Text: "support"})
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	post := func(actor string, b []byte, want int) {
		t.Helper()
		if w := call("POST", "/v2/projects/alpha/reviews", actor, b); w.Code != want {
			t.Fatalf("post %d want %d: %s", w.Code, want, w.Body)
		}
	}
	download := "/v2/projects/alpha/exports/" + d
	if w := call("GET", download, "reviewer", nil); w.Code != 403 {
		t.Fatalf("unapproved %d", w.Code)
	}
	policyCommand := command("policy", "support-policy", p, "", "", "", 0)
	post("analyst", policyCommand, 403)
	post("admin", policyCommand, 201)
	request := command("request", "support-request", d, p, "policy", "reviewer", 1)
	post("analyst", request, 201)
	approval := command("approve", "support-approval", d, p, "request", "", 2)
	post("analyst", approval, 403)
	post("owner", approval, 403)
	post("reviewer", approval, 201)
	post("reviewer", approval, 200)
	if w := call("GET", download, "viewer", nil); w.Code != 403 {
		t.Fatalf("viewer download %d", w.Code)
	}
	if w := call("GET", download, "reviewer", nil); w.Code != 200 || w.Body.String() != string(raw) || w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("approved download %d %s", w.Code, w.Body)
	}
	if w := call("GET", "/v1/projects/alpha/exports/"+d, "reviewer", nil); w.Code != 403 {
		t.Fatalf("legacy export bypass %d", w.Code)
	}
	if w := call("GET", "/v1/projects/alpha/history", "reviewer", nil); w.Code != 409 {
		t.Fatalf("v1 history widened %d", w.Code)
	}
	if w := call("GET", "/v2/projects/alpha/history", "reviewer", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "readmit-hub-review-history/v2") {
		t.Fatalf("v2 history %d", w.Code)
	}
	// Backup/restore preserves authenticated decisions and policy version, not a
	// local approval label. Old backup envelopes cannot carry v2 events.
	backup := filepath.Join(t.TempDir(), "backup")
	if e := s.Backup(ctx, backup); e != nil {
		t.Fatal(e)
	}
	manifestPath := filepath.Join(backup, "manifest.json")
	manifest, e := os.ReadFile(manifestPath)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(manifest), "readmit-hub-backup/v5") {
		t.Fatal("missing new backup boundary")
	}
	s.Close()
	reset(t, db)
	s = open(t, c)
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	h = s.TeamHandler(a)
	old := strings.Replace(string(manifest), "readmit-hub-backup/v5", "readmit-hub-backup/v4", 1)
	old = strings.Replace(old, `"metadata_version":6`, `"metadata_version":5`, 1)
	if e = os.WriteFile(manifestPath, []byte(old), 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.Restore(ctx, backup); e == nil {
		t.Fatal("v2 approval accepted in frozen v4 backup")
	}
	if e = os.WriteFile(manifestPath, manifest, 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.Restore(ctx, backup); e != nil {
		t.Fatal(e)
	}
	if w := call("GET", download, "reviewer", nil); w.Code != 200 {
		t.Fatalf("restored approval %d", w.Code)
	}
	// A new policy event invalidates every earlier approval, even identical bytes.
	post("admin", command("policy-next", "support-policy", p, "", "", "", 3), 201)
	if w := call("GET", download, "reviewer", nil); w.Code != 403 {
		t.Fatalf("old policy generation approval %d", w.Code)
	}
	post("reviewer", approval, 409)
	removal := []byte(`{"schema":"readmit-hub-lifecycle-command/v1","id":"remove-reviewer","expected":0,"kind":"remove-user","resource":"","artifact":"","parents":[],"subject":"reviewer","until":"","reason":"Synthetic removal"}`)
	if w := call("POST", "/v1/projects/alpha/lifecycle", "owner", removal); w.Code != 201 {
		t.Fatalf("removal %d %s", w.Code, w.Body)
	}
	post("reviewer", approval, 403)
	if w := call("GET", download, "reviewer", nil); w.Code != 403 {
		t.Fatalf("removed download %d", w.Code)
	}
	post("analyst", command("request-removed", "support-request", d, p, "policy-next", "reviewer", 4), 403)
	// Authorization must fail closed before idempotency when removal metadata
	// cannot be read. A stale access file cannot resurrect that actor.
	if _, e = db.Exec("DROP TABLE readmit_hub_lifecycle"); e != nil {
		t.Fatal(e)
	}
	post("admin", policyCommand, 403)
	if w := call("GET", download, "owner", nil); w.Code != 403 {
		t.Fatalf("metadata bypass %d", w.Code)
	}
}
