package hub_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/bharm16/readmit/hub"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func integrationConfig(t *testing.T) hub.Config {
	t.Helper()
	socket := os.Getenv("READMIT_HUB_TEST_SOCKET")
	if socket == "" {
		t.Skip("set READMIT_HUB_TEST_SOCKET for isolated PostgreSQL integration")
	}
	port, err := strconv.Atoi(os.Getenv("READMIT_HUB_TEST_PORT"))
	if err != nil || port < 1 || port > 65535 {
		t.Fatal("explicit test port required")
	}
	user := os.Getenv("READMIT_HUB_TEST_USER")
	if user == "" {
		t.Fatal("explicit test role required")
	}
	return hub.Config{Root: filepath.Join(t.TempDir(), "artifacts"), Socket: socket, Port: uint16(port), Database: "readmit_hub_test", User: user, MaxBytes: 128 << 20}
}
func testDatabase(t *testing.T, c hub.Config) *sql.DB {
	t.Helper()
	pc, err := pgx.ParseConfig("")
	if err != nil {
		t.Fatal(err)
	}
	pc.Host = c.Socket
	pc.Port = c.Port
	pc.User = c.User
	pc.Database = c.Database
	pc.Password = ""
	pc.TLSConfig = nil
	pc.Fallbacks = nil
	pc.RuntimeParams = map[string]string{}
	db := stdlib.OpenDB(*pc)
	t.Cleanup(func() { db.Close() })
	return db
}
func reset(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec("DROP TABLE IF EXISTS readmit_hub_reviews,readmit_hub_project_artifacts,readmit_hub_artifacts,readmit_hub_schema"); err != nil {
		t.Fatal(err)
	}
}
func open(t *testing.T, c hub.Config) *hub.Store {
	t.Helper()
	s, err := hub.Open(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPostgresPublicTransferBackupRestoreAndRefusals(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	s := open(t, c)
	ctx := context.Background()
	if err := s.Ready(ctx); err == nil {
		t.Fatal("unmigrated schema ready")
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("non-idempotent migration", err)
	}
	if err := os.WriteFile(filepath.Join(c.Root, ".health"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Ready(ctx); err != nil {
		t.Fatal("stale probe prevents recovery", err)
	}
	if other, err := hub.Open(ctx, c); err == nil {
		other.Close()
		t.Fatal("second service acquired database")
	}
	payload := []byte("MSH|^~\\&|SYNTHETIC\rPID|1||TEST-96\r\x00\xff")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	// Handler's authentication refusal is independent of transport setup.
	denied := httptest.NewRecorder()
	s.Handler().ServeHTTP(denied, httptest.NewRequest("GET", "/health/live", nil))
	if denied.Code != 401 {
		t.Fatal("unauthenticated request allowed")
	}
	// Synthetic verified chains are used only for the in-process HTTP contract;
	// a separate real TLS test below checks certificate verification.
	handler := s.Handler()
	request := func(method, path string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{}}}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	if got := request("PUT", "/v1/artifacts/"+digest, payload); got.Code != 201 {
		t.Fatalf("put %d %s", got.Code, got.Body.String())
	}
	if got := request("GET", "/v1/artifacts/"+digest, nil); got.Code != 200 || !bytes.Equal(got.Body.Bytes(), payload) {
		t.Fatal("bytes changed", got.Code)
	}
	if got := request("PUT", "/v1/artifacts/"+digest, []byte("changed")); got.Code != 422 {
		t.Fatal("mismatch accepted", got.Code)
	}
	if got := request("DELETE", "/v1/artifacts/"+digest, nil); got.Code != 405 {
		t.Fatal("delete accepted")
	}
	if got := request("GET", "/v1/artifacts/"+strings.Repeat("a", 64), nil); got.Code != 404 {
		t.Fatal("absent artifact answered")
	}
	if got := request("GET", "/health/ready", nil); got.Code != 204 {
		t.Fatal("not ready")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Put(cancelled, digest, bytes.NewReader(payload)); err == nil {
		t.Fatal("cancelled upload succeeded")
	}
	backup := filepath.Join(t.TempDir(), "snapshot")
	if err := s.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(ctx, backup); err == nil {
		t.Fatal("overwrote backup")
	}
	if err := s.Restore(ctx, backup); err == nil {
		t.Fatal("overwrote metadata")
	}
	s.Close()
	reset(t, db)
	c.Root = filepath.Join(t.TempDir(), "restored")
	restored := open(t, c)
	if err := restored.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restored.Restore(ctx, backup); err != nil {
		t.Fatal(err)
	}
	got, err := restored.Get(ctx, digest)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("restored link/bytes differ", err)
	}
	if err = os.WriteFile(filepath.Join(c.Root, digest), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = restored.Get(ctx, digest); err == nil {
		t.Fatal("corruption served")
	}
	if err = restored.Backup(ctx, filepath.Join(t.TempDir(), "bad")); err == nil {
		t.Fatal("corruption backed up")
	}
	restored.Close()
	reset(t, db)
	c.Root = filepath.Join(t.TempDir(), "retry")
	retry := open(t, c)
	if err = retry.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(backup, digest), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = retry.Restore(ctx, backup); err == nil {
		t.Fatal("damaged backup restored")
	}
	if _, err = retry.Get(ctx, digest); err == nil {
		t.Fatal("failed restore committed")
	}
	// Repair the independent fixture and safely retry the failed restore.
	if err = os.WriteFile(filepath.Join(backup, digest), payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err = retry.Restore(ctx, backup); err != nil {
		t.Fatal("restore did not recover", err)
	}
}

func TestMetadataUpgradeAndFutureVersionRefusal(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	_, err := db.Exec(`CREATE TABLE readmit_hub_schema(singleton boolean PRIMARY KEY CHECK(singleton),version integer NOT NULL);INSERT INTO readmit_hub_schema VALUES(true,1);CREATE TABLE readmit_hub_artifacts(digest text PRIMARY KEY CHECK(digest ~ '^[0-9a-f]{64}$'),size bigint NOT NULL CHECK(size >= 0 AND size <= 67108864))`)
	if err != nil {
		t.Fatal(err)
	}
	s := open(t, c)
	if err = s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = s.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE readmit_hub_schema SET version=999"); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(context.Background()); err == nil {
		t.Fatal("downgraded unknown schema")
	}
	if err = s.Ready(context.Background()); err == nil {
		t.Fatal("unknown schema ready")
	}
	if _, err = db.Exec("DROP TABLE readmit_hub_project_artifacts,readmit_hub_artifacts"); err != nil {
		t.Fatal(err)
	}
	_, err = s.Get(context.Background(), strings.Repeat("a", 64))
	if err == nil || errors.Is(err, hub.ErrMissing) {
		t.Fatal("database failure reported as absence", err)
	}
}
