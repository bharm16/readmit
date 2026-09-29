package hub_test

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/hubprotocol"
)

// The project directory answers metadata only: who linked each file and
// when, recorded for new links and never filled in for older ones, and the
// members the installed policy grants, to administrators only.
func TestPostgresProjectDirectoryRecordsLinkOriginAndCarriesItThroughBackup(t *testing.T) {
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
	call := func(handler http.Handler, method, path, role string, body []byte) *httptest.ResponseRecorder {
		r := request(signed(t, key, claims(role), accessHeader))
		r.Method = method
		r.URL.Path = path
		r.Body = httpBody(body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	payload := []byte("synthetic directory evidence")
	d := fmt.Sprintf("%x", sha256.Sum256(payload))
	if w := call(h, "PUT", "/v1/projects/alpha/artifacts/"+d, "analyst", payload); w.Code != 201 {
		t.Fatalf("put %d", w.Code)
	}
	// A link made before the hub recorded origins keeps none.
	older := []byte("synthetic older link")
	od := fmt.Sprintf("%x", sha256.Sum256(older))
	if w := call(h, "PUT", "/v1/projects/alpha/artifacts/"+od, "analyst", older); w.Code != 201 {
		t.Fatalf("put %d", w.Code)
	}
	if _, e := db.Exec("UPDATE readmit_hub_project_artifacts SET linked_issuer=NULL,linked_actor=NULL,linked_at=NULL WHERE digest=$1", od); e != nil {
		t.Fatal(e)
	}
	files := func(handler http.Handler) map[string]hubprotocol.ProjectFile {
		t.Helper()
		w := call(handler, "GET", "/v2/projects/alpha/files", "analyst", nil)
		if w.Code != 200 {
			t.Fatalf("files %d", w.Code)
		}
		list, e := hubprotocol.DecodeProjectFiles(w.Body.Bytes())
		if e != nil {
			t.Fatal(e, w.Body.String())
		}
		out := map[string]hubprotocol.ProjectFile{}
		for _, file := range list.Files {
			out[file.Digest] = file
		}
		return out
	}
	listed := files(h)
	if got := listed[d]; got.Actor != "analyst" || got.Issuer != "https://idp.example" || got.LinkedAt == "" || got.Size != int64(len(payload)) {
		t.Fatalf("recorded origin %+v", got)
	}
	if got := listed[od]; got.Actor != "" || got.Issuer != "" || got.LinkedAt != "" {
		t.Fatalf("unrecorded origin filled in %+v", got)
	}
	if w := call(h, "GET", "/v1/projects/alpha/files", "viewer", nil); w.Code != 404 {
		t.Fatalf("v1 files %d", w.Code)
	}

	// Members are the administrators'; reviewers are for those who may ask.
	for _, check := range []struct {
		route, role string
		want        int
	}{{"members", "admin", 200}, {"members", "analyst", 403}, {"members", "reviewer", 403}, {"reviewers", "analyst", 200}, {"reviewers", "viewer", 403}} {
		if w := call(h, "GET", "/v2/projects/alpha/"+check.route, check.role, nil); w.Code != check.want {
			t.Fatalf("%s as %s: %d", check.route, check.role, w.Code)
		}
	}
	remove := `{"schema":"readmit-hub-lifecycle-command/v1","id":"remove-viewer","expected":0,"kind":"remove-user","resource":"","artifact":"","parents":[],"subject":"viewer","until":"","reason":"left the team"}`
	if w := call(h, "POST", "/v1/projects/alpha/lifecycle", "admin", []byte(remove)); w.Code != 201 {
		t.Fatalf("remove %d %s", w.Code, w.Body.String())
	}
	w := call(h, "GET", "/v2/projects/alpha/members", "admin", nil)
	members, e := hubprotocol.DecodeProjectMembers(w.Body.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	status := map[string]string{}
	for _, member := range members.Members {
		status[member.Subject] = member.Role + ":" + member.Status
	}
	if status["viewer"] != "viewer:removed" || status["analyst"] != "analyst:active" || len(status) != 6 || members.Issuer != "https://idp.example" {
		t.Fatalf("members %v", status)
	}
	w = call(h, "GET", "/v2/projects/alpha/reviewers", "analyst", nil)
	reviewers, e := hubprotocol.DecodeProjectMembers(w.Body.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	named := []string{}
	for _, member := range reviewers.Members {
		named = append(named, member.Subject)
	}
	if strings.Join(named, ",") != "owner,reviewer" {
		t.Fatalf("reviewers %v", named)
	}

	// Backup/v6 carries each link's origin; an older version restores none.
	backup := filepath.Join(t.TempDir(), "backup")
	if e := s.Backup(ctx, backup); e != nil {
		t.Fatal(e)
	}
	manifestPath := filepath.Join(backup, "manifest.json")
	manifest, e := os.ReadFile(manifestPath)
	if e != nil {
		t.Fatal(e)
	}
	var shape struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(manifest, &shape) != nil || shape.Schema != "readmit-hub-backup/v6" {
		t.Fatal("backup version", shape.Schema)
	}
	restore := func(data []byte) {
		t.Helper()
		if e := os.WriteFile(manifestPath, data, 0600); e != nil {
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
	}
	restore(manifest)
	if got := files(h)[d]; got.Actor != "analyst" || got.LinkedAt != listed[d].LinkedAt {
		t.Fatalf("restored origin %+v", got)
	}
	if got := files(h)[od]; got.Actor != "" || got.LinkedAt != "" {
		t.Fatalf("restored unrecorded origin %+v", got)
	}
	v5 := strings.Replace(string(manifest), "readmit-hub-backup/v6", "readmit-hub-backup/v5", 1)
	v5 = strings.Replace(v5, `"metadata_version":7`, `"metadata_version":6`, 1)
	v5 = regexp.MustCompile(`,"issuer":"[^"]*","actor":"[^"]*","linked_at":"[^"]*"`).ReplaceAllString(v5, "")
	restore([]byte(v5))
	if got := files(h)[d]; got.Actor != "" || got.Issuer != "" || got.LinkedAt != "" {
		t.Fatalf("older backup restored an origin %+v", got)
	}
	// A v6 link with half an origin is refused before anything is restored.
	broken := strings.Replace(string(manifest), `"actor":"analyst"`, `"actor":""`, 1)
	if broken == string(manifest) {
		t.Fatal("ineffective mutation")
	}
	if e := os.WriteFile(manifestPath, []byte(broken), 0600); e != nil {
		t.Fatal(e)
	}
	if e := hub.VerifyBackup(ctx, backup); e == nil {
		t.Fatal("verified half an origin")
	}
}
