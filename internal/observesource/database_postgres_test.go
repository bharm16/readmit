package observesource_test

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
)

// Opt-in real-server integration, isolated from every existing database. The
// selected executable's actual version is evidence, never an inferred target.
// READMIT_POSTGRES_BIN=/path/to/postgres/bin go test ./internal/observesource
//
//	-run '^TestDatabasePostgreSQLLab$' -count=1 -v
func TestDatabasePostgreSQLLab(t *testing.T) {
	bin := os.Getenv("READMIT_POSTGRES_BIN")
	if bin == "" {
		t.Skip("requires explicit local PostgreSQL binary directory")
	}
	if !filepath.IsAbs(bin) {
		t.Fatal("PostgreSQL binary directory must be absolute")
	}
	version, err := exec.Command(filepath.Join(bin, "postgres"), "--version").Output()
	if err != nil {
		t.Fatal("selected PostgreSQL cannot run")
	}
	t.Log(strings.TrimSpace(string(version)))
	root, err := os.MkdirTemp("", "readmit-pg-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	data := filepath.Join(root, "data")
	run := func(name string, args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, filepath.Join(bin, name), args...).CombinedOutput(); err != nil {
			_ = out
			t.Fatalf("synthetic lab %s failed: %v", name, err)
		}
	}
	run("initdb", "-D", data, "--auth-local=trust", "--auth-host=reject", "-U", "lab_owner", "--no-locale")
	ca := newAuthority(t)
	pair := ca.leaf(t, nil, []string{"localhost"})
	key, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	write(t, data, "server.crt", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pair.Certificate[0]})))
	write(t, data, "server.key", string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})))
	write(t, data, "pg_hba.conf", "local all all trust\nhostssl all observer 127.0.0.1/32 trust\nhost all all 127.0.0.1/32 reject\n")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_, port, _ := net.SplitHostPort(address)
	listener.Close()
	run("pg_ctl", "-D", data, "-l", filepath.Join(root, "server.log"), "-w", "-t", "10", "-o", "-h 127.0.0.1 -p "+port+" -k "+root+" -c ssl=on", "start")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, filepath.Join(bin, "pg_ctl"), "-D", data, "-m", "immediate", "-w", "stop").Run()
	}()
	sql := func(statement string) {
		t.Helper()
		run("psql", "-h", root, "-p", port, "-U", "lab_owner", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", statement)
	}
	sql(`CREATE ROLE observer LOGIN`)
	run("createdb", "-h", root, "-p", port, "-U", "lab_owner", "synthetic")
	setup := func(statement string) {
		t.Helper()
		run("psql", "-h", root, "-p", port, "-U", "lab_owner", "-d", "synthetic", "-v", "ON_ERROR_STOP=1", "-c", statement)
	}
	setup(`CREATE TABLE private_rows (appointment text,status text); INSERT INTO private_rows VALUES ('APT-1','ready'); CREATE VIEW observed AS SELECT appointment,status FROM private_rows; GRANT SELECT ON observed TO observer;`)
	source := databaseDeclared(t, address, ca.pem)
	source.Database.Filters[0].Value = "ready"
	w := window(t, strings.ReplaceAll(strings.ReplaceAll(declaredWindow, "file-export", "database-query"), "scheduling-archive", "synthetic-lab"))
	collect := func(want observewindow.Status) []string {
		t.Helper()
		snapshot := filepath.Join(t.TempDir(), "snapshot")
		got, err := observesource.Collect(context.Background(), source, w, observesource.Options{Snapshot: snapshot, Produced: []string{"APT-1"}})
		if err != nil || got.Status != want {
			t.Fatalf("real PostgreSQL collection got %s, want %s: %v", got.Status, want, err)
		}
		if err := w.Verify(got); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(snapshot, "read-0000", "database.json"))
		if err != nil {
			t.Fatal(err)
		}
		record, err := observesource.DecodeDatabaseRead(data)
		if err != nil {
			t.Fatal(err)
		}
		return record.Keys
	}
	collect(observewindow.Complete)
	// Independent grant denial: the actual observation principal cannot mutate
	// either the underlying table or the approved view.
	for _, target := range []string{"private_rows", "observed"} {
		cmd := exec.Command(filepath.Join(bin, "psql"), "-h", root, "-p", port, "-U", "observer", "-d", "synthetic", "-v", "ON_ERROR_STOP=1", "-c", "INSERT INTO "+target+" VALUES ('FORBIDDEN','ready')")
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "permission denied") {
			t.Fatal("observation principal did not enforce write denial")
		}
	}
	setup(`REVOKE SELECT ON observed FROM observer`)
	collect(observewindow.Failed)
	setup(`GRANT SELECT ON observed TO observer`)
	collect(observewindow.Complete)
	setup(`INSERT INTO private_rows VALUES (NULL,'ready')`)
	collect(observewindow.Ambiguous)
	setup(`DELETE FROM private_rows WHERE appointment IS NULL; INSERT INTO private_rows VALUES (repeat('x',129),'ready')`)
	collect(observewindow.Ambiguous)
	setup(`DELETE FROM private_rows`)
	collect(observewindow.Complete)
	for _, tc := range []struct{ kind, expression, expected string }{
		{"decimal", "12345678901234567890.123456789::numeric", "12345678901234567890.123456789"},
		{"timestamp", "TIMESTAMPTZ '2026-01-03 11:00:00.123456+05:30'", "2026-01-03T05:30:00.123456Z"},
		{"integer", "9223372036854775807::bigint", "9223372036854775807"},
	} {
		setup(`DROP VIEW observed; CREATE VIEW observed AS SELECT ` + tc.expression + ` AS appointment, 'ready'::text AS status; GRANT SELECT ON observed TO observer`)
		source.Database.KeyType = tc.kind
		keys := collect(observewindow.Complete)
		if len(keys) != 1 || keys[0] != tc.expected {
			t.Fatalf("real PostgreSQL %s mapping changed", tc.kind)
		}
	}
	setup(`DROP VIEW observed; CREATE VIEW observed AS SELECT 'APT-1'::text AS appointment, 'ready'::text AS status FROM pg_sleep(1); GRANT SELECT ON observed TO observer`)
	source.Database.KeyType = "text"
	source.Database.Limits = &observesource.DatabaseLimits{Timeout: "30ms", MaxRows: 10000, MaxBytes: 10 << 20}
	collect(observewindow.Failed)
	source.Database.Limits = nil
	setup(`DROP VIEW observed; CREATE VIEW observed AS SELECT 'APT-1'::text AS appointment, 'ready'::text AS status; GRANT SELECT ON observed TO observer`)
	collect(observewindow.Complete)
}
