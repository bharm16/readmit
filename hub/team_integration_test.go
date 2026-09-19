package hub_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/hub"
)

func TestPostgresTeamIsolationRevocationAndBackup(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	s := open(t, c)
	ctx := context.Background()
	if e := s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	a, p, key, path := accessFixture(t)
	h := s.TeamHandler(a)
	payload := []byte("synthetic project alpha evidence")
	d := fmt.Sprintf("%x", sha256.Sum256(payload))
	call := func(handler http.Handler, method, path, role string) *httptest.ResponseRecorder {
		r := request(signed(t, key, claims(role), accessHeader))
		r.Method = method
		r.URL.Path = path
		r.Body = httpBody(payload)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	artifact := "/v1/projects/alpha/artifacts/" + d
	if w := call(h, "PUT", artifact, "analyst"); w.Code != 201 {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	if w := call(h, "GET", artifact, "viewer"); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), payload) {
		t.Fatalf("get %d", w.Code)
	}
	if w := call(h, "PUT", artifact, "viewer"); w.Code != 403 {
		t.Fatalf("viewer write %d", w.Code)
	}
	if w := call(h, "GET", "/v1/projects/alpha/exports/"+d, "viewer"); w.Code != 403 {
		t.Fatalf("viewer export %d", w.Code)
	}
	if w := call(h, "GET", "/v1/projects/alpha/exports/"+d, "reviewer"); w.Code != 403 {
		t.Fatalf("reviewer export %d", w.Code)
	}
	if w := call(h, "GET", "/v1/artifacts/"+d, "owner"); w.Code != 404 {
		t.Fatalf("unscoped team route %d", w.Code)
	}
	if w := call(s.Handler(), "GET", "/v1/artifacts/"+d, "owner"); w.Code != 403 {
		t.Fatalf("legacy bypass %d", w.Code)
	}
	p.Grants = append(p.Grants, hub.ProjectGrant{Project: "beta", Subject: "viewer", Role: "viewer"})
	writePolicy(t, path, p)
	if w := call(h, "GET", "/v1/projects/beta/artifacts/"+d, "viewer"); w.Code != 404 {
		t.Fatalf("cross-project digest %d", w.Code)
	}
	for _, test := range []struct {
		route, role string
		want        int
	}{{"execution", "viewer", 403}, {"execution", "analyst", 501}, {"approvals", "analyst", 403}, {"approvals", "reviewer", 501}, {"enrollment", "owner", 403}} {
		if w := call(h, "POST", "/v1/projects/alpha/"+test.route, test.role); w.Code != test.want {
			t.Fatalf("%s %s = %d", test.route, test.role, w.Code)
		}
	}
	// Prefix-related project IDs must preserve tuple order across backup/restore.
	p.Grants = append(p.Grants, hub.ProjectGrant{Project: "alpha-beta", Subject: "analyst", Role: "analyst"})
	writePolicy(t, path, p)
	if w := call(h, "PUT", "/v1/projects/alpha-beta/artifacts/"+d, "analyst"); w.Code != 201 {
		t.Fatalf("prefix project put %d", w.Code)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if e := s.Backup(ctx, backup); e != nil {
		t.Fatal(e)
	}
	s.Close()
	reset(t, db)
	c.Root = filepath.Join(t.TempDir(), "restored")
	s = open(t, c)
	if e := s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e := s.Restore(ctx, backup); e != nil {
		t.Fatal(e)
	}
	h = s.TeamHandler(a)
	if w := call(h, "GET", "/v1/projects/alpha-beta/artifacts/"+d, "analyst"); w.Code != 200 {
		t.Fatalf("prefix project restore %d", w.Code)
	}
	if w := call(h, "GET", artifact, "viewer"); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), payload) {
		t.Fatalf("restored project read %d", w.Code)
	}
	if w := call(s.Handler(), "GET", "/v1/artifacts/"+d, "owner"); w.Code != 403 {
		t.Fatalf("restored legacy bypass %d", w.Code)
	}
	p.Grants = []hub.ProjectGrant{}
	p.Tokens = []hub.ScopedToken{}
	writePolicy(t, path, p)
	if w := call(h, "GET", artifact, "viewer"); w.Code != 403 {
		t.Fatalf("removed user %d", w.Code)
	}
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if w := call(h, "GET", artifact, "owner"); w.Code != 403 {
		t.Fatalf("removed policy %d", w.Code)
	}
}

type bodyReader struct{ *bytes.Reader }

func (*bodyReader) Close() error       { return nil }
func httpBody(data []byte) *bodyReader { return &bodyReader{bytes.NewReader(data)} }

func TestPostgresOldBackupRestoresUnscopedWithoutGrantingProjects(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	s := open(t, c)
	ctx := context.Background()
	if e := s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	source := t.TempDir()
	payload := []byte("old synthetic object")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	if e := os.WriteFile(filepath.Join(source, digest), payload, 0600); e != nil {
		t.Fatal(e)
	}
	manifest := fmt.Sprintf(`{"schema":"readmit-hub-backup/v1","metadata_version":2,"artifacts":[{"sha256":%q,"size":%d,"retained_at":"2026-01-01T00:00:00Z"}]}`, digest, len(payload))
	if e := os.WriteFile(filepath.Join(source, "manifest.json"), []byte(manifest), 0600); e != nil {
		t.Fatal(e)
	}
	if e := s.Restore(ctx, source); e != nil {
		t.Fatal(e)
	}
	got, e := s.Get(ctx, digest)
	if e != nil || !bytes.Equal(got, payload) {
		t.Fatal("v1 lost evidence", e)
	}
	a, _, key, _ := accessFixture(t)
	r := request(signed(t, key, claims("viewer"), accessHeader))
	r.URL.Path = "/v1/projects/alpha/artifacts/" + digest
	w := httptest.NewRecorder()
	s.TeamHandler(a).ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("old object auto-granted: %d", w.Code)
	}
}

func TestPostgresTeamBackupRefusalsLeaveNoReadableArtifacts(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	s := open(t, c)
	ctx := context.Background()
	if e := s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	source := t.TempDir()
	payload := []byte("synthetic backup contract")
	d := fmt.Sprintf("%x", sha256.Sum256(payload))
	if e := os.WriteFile(filepath.Join(source, d), payload, 0600); e != nil {
		t.Fatal(e)
	}
	artifact := fmt.Sprintf(`[{"sha256":%q,"size":%d,"retained_at":"2026-01-01T00:00:00Z"}]`, d, len(payload))
	link := fmt.Sprintf(`{"project":"alpha","sha256":%q}`, d)
	cases := []struct{ name, links, team string }{
		{"duplicate", "[" + link + "," + link + "]", "true"},
		{"operator links", "[" + link + "]", "false"},
		{"null links", "null", "true"},
		{"null mode", "[" + link + "]", "null"},
		{"unknown", fmt.Sprintf(`[{"project":"alpha","sha256":%q,"extra":true}]`, d), "true"},
		{"missing", `[{"project":"alpha"}]`, "true"},
		{"dangling", fmt.Sprintf(`[{"project":"alpha","sha256":%q}]`, strings.Repeat("0", 64)), "true"},
		{"invalid project", fmt.Sprintf(`[{"project":"../alpha","sha256":%q}]`, d), "true"},
		{"order", fmt.Sprintf(`[{"project":"beta","sha256":%q},%s]`, d, link), "true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`{"schema":"readmit-hub-backup/v2","metadata_version":3,"artifacts":%s,"projects":%s,"team_enabled":%s}`, artifact, tc.links, tc.team)
			if e := os.WriteFile(filepath.Join(source, "manifest.json"), []byte(manifest), 0600); e != nil {
				t.Fatal(e)
			}
			if e := s.Restore(ctx, source); e == nil {
				t.Fatal("unsafe backup restored")
			}
			if _, e := s.Get(ctx, d); e == nil {
				t.Fatal("failed restore published evidence")
			}
		})
	}
	manifest := fmt.Sprintf(`{"schema":"readmit-hub-backup/v2","metadata_version":3,"artifacts":%s,"projects":[%s],"team_enabled":true}`, artifact, link)
	if e := os.WriteFile(filepath.Join(source, "manifest.json"), []byte(manifest), 0600); e != nil {
		t.Fatal(e)
	}
	if e := s.Restore(ctx, source); e != nil {
		t.Fatal("valid retry", e)
	}
}
