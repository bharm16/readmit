package observesource

// Opt-in synthetic server qualification. The manual database-lab workflow
// provisions one fresh TLS/password server and supplies only ephemeral env
// values. Ordinary PR and release suites skip this test.

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testlicense"
)

type databaseLab struct {
	driver, address, caFile, serverName, adminUser, adminPassword, observerPassword, output, container string
	admin, observer                                                                                    *sql.DB
	source                                                                                             Source
	window                                                                                             observewindow.Window
	policy                                                                                             *sendpolicy.Policy
}

func TestDatabaseQualifiedLab(t *testing.T) {
	driver := os.Getenv("READMIT_DB_LAB_DRIVER")
	if driver == "" {
		t.Skip("requires an explicitly selected synthetic database lab")
	}
	if driver != "postgresql" && driver != "sqlserver" {
		t.Fatal("lab driver must be postgresql or sqlserver")
	}
	lab := &databaseLab{
		driver: driver, address: os.Getenv("READMIT_DB_LAB_ADDRESS"),
		caFile: os.Getenv("READMIT_DB_LAB_CA_FILE"), serverName: os.Getenv("READMIT_DB_LAB_SERVER_NAME"),
		adminUser: os.Getenv("READMIT_DB_LAB_ADMIN_USER"), adminPassword: os.Getenv("READMIT_DB_LAB_ADMIN_PASSWORD"),
		observerPassword: os.Getenv("READMIT_DB_LAB_OBSERVER_PASSWORD"), output: os.Getenv("READMIT_DB_LAB_OUTPUT"),
		container: os.Getenv("READMIT_DB_LAB_CONTAINER"),
	}
	if lab.address == "" || lab.caFile == "" || lab.serverName == "" || lab.adminUser == "" || lab.adminPassword == "" || lab.observerPassword == "" || lab.container == "" || !filepath.IsAbs(lab.output) {
		t.Fatal("lab requires endpoint, CA, name, two distinct principals and absolute output")
	}
	if lab.adminPassword == lab.observerPassword {
		t.Fatal("setup and observation principals must have distinct credentials")
	}
	if err := os.MkdirAll(lab.output, 0700); err != nil {
		t.Fatal("cannot create lab evidence folder")
	}
	lab.prepare(t)
	t.Cleanup(func() { lab.admin.Close(); lab.observer.Close() })

	// The actual authenticated observation principal, not a driver flag or SQL
	// prefix check, is the authority boundary.
	lab.independentAccessChecks(t)
	lab.collect(t, "populated", observewindow.Complete, []string{"APT-1"})
	lab.source.Database.Filters[0].Value = "ready' OR '1'='1"
	lab.collect(t, "bound-filter", observewindow.Complete, nil)
	lab.source.Database.Filters[0].Value = "ready"

	lab.statement(t, "DELETE FROM "+lab.table())
	lab.collect(t, "empty", observewindow.Complete, nil)
	lab.statement(t, "INSERT INTO "+lab.table()+" (appointment,status) VALUES (NULL,'ready')")
	lab.collect(t, "null-key", observewindow.Ambiguous, nil)
	lab.statement(t, "DELETE FROM "+lab.table())
	if driver == "postgresql" {
		lab.statement(t, "INSERT INTO "+lab.table()+" (appointment,status) VALUES (repeat('X',129),'ready')")
	} else {
		lab.statement(t, "INSERT INTO "+lab.table()+" (appointment,status) VALUES (REPLICATE('X',129),'ready')")
	}
	lab.collect(t, "long-text", observewindow.Ambiguous, nil)

	lab.statement(t, "DELETE FROM "+lab.table())
	lab.statement(t, "INSERT INTO "+lab.table()+" (appointment,status) VALUES ('APT-1','ready'),('APT-2','ready')")
	lab.source.Database.Limits = &DatabaseLimits{Timeout: "30s", MaxRows: 1, MaxBytes: 10 << 20}
	lab.collect(t, "row-limit", observewindow.Truncated, nil)
	lab.source.Database.Limits = &DatabaseLimits{Timeout: "30s", MaxRows: 10000, MaxBytes: 3}
	lab.collect(t, "byte-limit", observewindow.Truncated, nil)
	lab.source.Database.Limits = nil

	lab.statement(t, "DELETE FROM "+lab.table())
	lab.statement(t, "INSERT INTO "+lab.table()+" (appointment,status) VALUES ('APT-1','ready')")
	lab.revokeView(t)
	lab.collect(t, "permission-refused", observewindow.Failed, nil)
	lab.grantView(t)
	lab.collect(t, "permission-recovered", observewindow.Complete, []string{"APT-1"})

	lab.mappingCases(t)
	lab.restoreTextView(t)
	lab.statement(t, "DELETE FROM "+lab.table())
	lab.statement(t, "INSERT INTO "+lab.table()+" (appointment,status) VALUES ('APT-1','ready')")
	lab.blockedQueries(t)
	lab.collect(t, "fresh-recovery", observewindow.Complete, []string{"APT-1"})
	lab.lostConnection(t)
	lab.collect(t, "connection-recovered", observewindow.Complete, []string{"APT-1"})

	// A wrong mapping and wrong account remain errors. Neither can turn an
	// incomplete read into a completed absence claim.
	lab.source.Database.RecordKey = "wrong_column"
	lab.collect(t, "invalid-mapping", observewindow.Failed, nil)
	lab.source.Database.RecordKey = "appointment"
	lab.source.Database.Username = lab.adminUser
	lab.collect(t, "wrong-principal", observewindow.Failed, nil)
	lab.source.Database.Username = "observer"
	lab.cliPublicRead(t)

	// The test emits only safe version and scenario names; exact images and
	// digest provenance are added by the manual runner outside the test.
	var version string
	versionSQL := "SHOW server_version"
	if driver == "sqlserver" {
		versionSQL = "SELECT @@VERSION"
	}
	if err := lab.admin.QueryRow(versionSQL).Scan(&version); err != nil {
		t.Fatal("cannot read server version")
	}
	if err := os.WriteFile(filepath.Join(lab.output, "server-version.txt"), []byte(strings.TrimSpace(version)+"\n"), 0600); err != nil {
		t.Fatal("cannot retain server version")
	}
	t.Log("qualified synthetic database read, independent grants, TLS/password and failure states")
}

func (l *databaseLab) cliPublicRead(t *testing.T) {
	t.Helper()
	declarations := t.TempDir()
	write := func(name string, value any) string {
		t.Helper()
		raw, err := json.Marshal(value, json.Deterministic(true))
		if err != nil {
			t.Fatal("cannot encode public lab declaration")
		}
		path := filepath.Join(declarations, name)
		if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal("cannot write public lab declaration")
		}
		return path
	}
	source := write("source.json", l.source)
	window := write("window.json", l.window)
	policy := write("policy.json", l.policy)
	root := filepath.Join(l.output, "cli-public-read")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal("cannot retain public read")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", "../../cmd/readmit", "--operation-policy", testlicense.New(t),
		"observe", "collect", source, "--window", window, "--policy", policy,
		"--out", filepath.Join(root, "completion.json"), "--snapshot", filepath.Join(root, "snapshot"), "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil || ctx.Err() != nil {
		t.Fatal("public observe collect did not complete")
	}
	completed, err := observewindow.ReadCompletion(filepath.Join(root, "completion.json"))
	if err != nil || completed.Status != observewindow.Complete || completed.RecordsObserved != 1 || l.window.Verify(completed) != nil {
		t.Fatal("public observe collect retained a different decision")
	}
	raw, err := observewindow.EncodeCompletion(completed)
	if err != nil || !bytes.Equal(bytes.TrimSpace(stdout.Bytes()), bytes.TrimSpace(raw)) || stderr.Len() != 0 {
		t.Fatal("public observe collect output disagreed with retained completion")
	}
}

func (l *databaseLab) prepare(t *testing.T) {
	t.Helper()
	ca, err := os.ReadFile(l.caFile)
	if err != nil {
		t.Fatal("cannot read lab CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("invalid lab CA")
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: l.serverName}
	name := "synthetic"
	if l.driver == "sqlserver" {
		name = "master"
	}
	l.admin = l.open(t, l.adminUser, l.adminPassword, name, config)
	if l.driver == "sqlserver" {
		l.statement(t, "CREATE DATABASE synthetic")
		l.statement(t, "CREATE LOGIN observer WITH PASSWORD = '"+l.observerPassword+"', CHECK_POLICY = OFF")
		l.admin.Close()
		l.admin = l.open(t, l.adminUser, l.adminPassword, "synthetic", config)
		l.statement(t, "CREATE USER observer FOR LOGIN observer")
		l.statement(t, "CREATE TABLE dbo.private_rows (appointment nvarchar(256) NULL, status nvarchar(32))")
		l.statement(t, "INSERT INTO dbo.private_rows (appointment,status) VALUES ('APT-1','ready')")
		l.statement(t, "CREATE VIEW dbo.observed AS SELECT appointment,status FROM dbo.private_rows")
	} else {
		l.statement(t, "CREATE ROLE observer LOGIN PASSWORD '"+l.observerPassword+"'")
		l.statement(t, "CREATE TABLE public.private_rows (appointment text, status text)")
		l.statement(t, "INSERT INTO public.private_rows (appointment,status) VALUES ('APT-1','ready')")
		l.statement(t, "CREATE VIEW public.observed AS SELECT appointment,status FROM public.private_rows")
		l.statement(t, "GRANT CONNECT ON DATABASE synthetic TO observer")
		l.statement(t, "GRANT USAGE ON SCHEMA public TO observer")
	}
	l.grantView(t)
	l.observer = l.open(t, "observer", l.observerPassword, "synthetic", config)
	provider := filepath.Join(t.TempDir(), "credential-provider")
	if err := os.WriteFile(provider, []byte("#!/bin/sh\nprintf '%s' \"$READMIT_DB_LAB_OBSERVER_PASSWORD\"\n"), 0700); err != nil {
		t.Fatal("cannot create synthetic credential provider")
	}
	l.source = Source{Schema: SchemaDatabase, Observes: observewindow.Source{Kind: DatabaseQuery, Identity: "synthetic-lab", Scope: "appointments"},
		Enabled: true, Freshness: Freshness{MaxAge: "1m"}, Database: &Database{
			Driver: l.driver, Address: l.address, Classification: "nonproduction", Name: "synthetic", Username: "observer",
			CAFile: l.caFile, ServerName: l.serverName, Credential: DatabaseCredential{Store: secret.CustomerManaged,
				Address: l.address, Purpose: "database-observation", Command: provider, Arguments: []string{}},
			View: []string{"public", "observed"}, RecordKey: "appointment", KeyType: "text",
			Filters: []DatabaseFilter{{Column: "status", Value: "ready"}},
		}}
	if l.driver == "sqlserver" {
		l.source.Database.View[0] = "dbo"
	}
	// Round trip both declarations through the same strict readers used by the
	// CLI, including explicit null transports and limits.
	encoded, err := json.Marshal(l.source)
	if err != nil {
		t.Fatal("cannot encode lab source")
	}
	l.source, err = DecodeSource(encoded)
	if err != nil {
		t.Fatal("lab source did not pass strict reader")
	}
	l.window = observewindow.Window{Schema: observewindow.WindowSchema, Source: l.source.Observes,
		Watermark:   observewindow.Watermark{Kind: "none", Position: ""},
		PreExisting: observewindow.PreExisting{Declaration: "declared-empty", BaselineIdentity: ""},
		Completion:  observewindow.Rule{Deadline: "15s", QuietPeriod: "10ms", StableSamples: 2, MaxRecords: 100, MaxSamples: 64}}
	windowRaw, err := json.Marshal(l.window)
	if err != nil {
		t.Fatal("cannot encode lab window")
	}
	l.window, err = observewindow.DecodeWindow(windowRaw)
	if err != nil {
		t.Fatal("lab window did not pass strict reader")
	}
	policy, err := sendpolicy.DecodePolicy([]byte(`{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.0/8"]}`))
	if err != nil {
		t.Fatal("cannot read loopback policy")
	}
	l.policy = &policy
	// A new endpoint with the wrong password, wrong verified name or untrusted
	// CA must fail before any query can be trusted.
	l.refusedConnection(t, "observer", l.observerPassword+"wrong", "synthetic", config)
	wrongName := config.Clone()
	wrongName.ServerName = "wrong-name.invalid"
	l.refusedConnection(t, "observer", l.observerPassword, "synthetic", wrongName)
	untrusted := config.Clone()
	untrusted.RootCAs = x509.NewCertPool()
	l.refusedConnection(t, "observer", l.observerPassword, "synthetic", untrusted)
}

func (l *databaseLab) open(t *testing.T, user, password, name string, config *tls.Config) *sql.DB {
	t.Helper()
	d := Database{Driver: l.driver, Address: l.address, Name: name, Username: user, ServerName: config.ServerName}
	connector, err := databaseConnector(d, password, l.address, config)
	if err != nil {
		t.Fatal("cannot configure lab connection")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(2)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = db.PingContext(ctx)
		cancel()
		if err == nil {
			return db
		}
		time.Sleep(500 * time.Millisecond)
	}
	db.Close()
	t.Fatal("TLS/password lab connection did not become ready")
	return nil
}

func (l *databaseLab) refusedConnection(t *testing.T, user, password, name string, config *tls.Config) {
	t.Helper()
	d := Database{Driver: l.driver, Address: l.address, Name: name, Username: user, ServerName: config.ServerName}
	connector, err := databaseConnector(d, password, l.address, config)
	if err != nil {
		t.Fatal("cannot configure negative lab connection")
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if db.PingContext(ctx) == nil {
		t.Fatal("wrong password, name or CA was accepted")
	}
}

func (l *databaseLab) statement(t *testing.T, statement string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := l.admin.ExecContext(ctx, statement); err != nil {
		t.Fatalf("synthetic setup statement failed (%T)", err)
	}
}

func (l *databaseLab) table() string {
	if l.driver == "sqlserver" {
		return "dbo.private_rows"
	}
	return "public.private_rows"
}
func (l *databaseLab) view() string {
	if l.driver == "sqlserver" {
		return "dbo.observed"
	}
	return "public.observed"
}
func (l *databaseLab) grantView(t *testing.T) {
	l.statement(t, "GRANT SELECT ON "+l.view()+" TO observer")
}
func (l *databaseLab) revokeView(t *testing.T) {
	l.statement(t, "REVOKE SELECT ON "+l.view()+" FROM observer")
}

func (l *databaseLab) independentAccessChecks(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var key string
	if err := l.observer.QueryRowContext(ctx, "SELECT appointment FROM "+l.view()+" WHERE status = 'ready'").Scan(&key); err != nil || key != "APT-1" {
		t.Fatal("SELECT-only principal could not read approved view")
	}
	// The setup principal checks the observation session's transport so the
	// SELECT-only principal needs no server-state privilege to prove TLS.
	connection, err := l.observer.Conn(ctx)
	if err != nil {
		t.Fatal("cannot inspect observation connection")
	}
	defer connection.Close()
	var session int
	sessionSQL := "SELECT pg_backend_pid()"
	if l.driver == "sqlserver" {
		sessionSQL = "SELECT @@SPID"
	}
	if err := connection.QueryRowContext(ctx, sessionSQL).Scan(&session); err != nil {
		t.Fatal("cannot identify observation session")
	}
	if l.driver == "postgresql" {
		var encrypted bool
		if err := l.admin.QueryRowContext(ctx, "SELECT ssl FROM pg_stat_ssl WHERE pid = $1", session).Scan(&encrypted); err != nil || !encrypted {
			t.Fatal("observation principal did not use server-verified TLS")
		}
	} else {
		var encrypted string
		if err := l.admin.QueryRowContext(ctx, "SELECT encrypt_option FROM sys.dm_exec_connections WHERE session_id = @p1", session).Scan(&encrypted); err != nil || encrypted != "TRUE" {
			t.Fatal("observation principal did not use server-verified TLS")
		}
	}
	for _, statement := range []string{
		"SELECT appointment FROM " + l.table(),
		"INSERT INTO " + l.table() + " (appointment,status) VALUES ('FORBIDDEN','ready')",
		"INSERT INTO " + l.view() + " (appointment,status) VALUES ('FORBIDDEN','ready')",
		"UPDATE " + l.table() + " SET status='changed'",
		"DELETE FROM " + l.table(),
		"CREATE TABLE lab_forbidden (value int)",
	} {
		if _, err := l.observer.ExecContext(ctx, statement); err == nil {
			t.Fatal("observation principal had forbidden table or write authority")
		}
	}
}

func (l *databaseLab) collect(t *testing.T, label string, want observewindow.Status, keys []string) {
	t.Helper()
	root := filepath.Join(l.output, label)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal("cannot create scenario evidence")
	}
	snapshot := filepath.Join(root, "snapshot")
	got, err := Collect(context.Background(), l.source, l.window, Options{Snapshot: snapshot, Policy: l.policy})
	if err != nil || got.Status != want {
		t.Fatalf("%s: collection status %s, want %s (%T)", label, got.Status, want, err)
	}
	if err := l.window.Verify(got); err != nil {
		t.Fatalf("%s: completion did not verify", label)
	}
	if want == observewindow.Complete {
		if err := got.AbsenceEvidence(); err != nil {
			t.Fatalf("%s: complete read lacked absence evidence", label)
		}
	} else if got.AbsenceEvidence() == nil {
		t.Fatalf("%s: failed read became absence evidence", label)
	}
	if err := observewindow.WriteCompletion(filepath.Join(root, "completion.json"), got); err != nil {
		t.Fatalf("%s: cannot retain completion", label)
	}
	opened, err := observewindow.ReadCompletion(filepath.Join(root, "completion.json"))
	if err != nil || opened.Status != want {
		t.Fatalf("%s: retained completion changed", label)
	}
	raw, err := os.ReadFile(filepath.Join(snapshot, "read-0000", "database.json"))
	if err != nil {
		t.Fatalf("%s: no retained typed database result", label)
	}
	read, err := DecodeDatabaseRead(raw)
	if err != nil || read.Limits != l.source.Database.limits() || read.Driver != l.driver || read.KeyType != l.source.Database.KeyType {
		t.Fatalf("%s: typed database result or effective limits changed", label)
	}
	if keys != nil {
		if err != nil || !sameLabKeys(read.Keys, keys) {
			t.Fatalf("%s: typed key mapping changed", label)
		}
	} else if want == observewindow.Complete && got.RecordsObserved != 0 {
		t.Fatalf("%s: expected a fully observed empty result", label)
	}
	for _, forbidden := range []string{l.adminPassword, l.observerPassword, "ready' OR '1'='1"} {
		if forbidden == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return walkErr
			}
			raw, readErr := os.ReadFile(path)
			if readErr == nil && strings.Contains(string(raw), forbidden) {
				t.Fatal("credential or bound filter reached retained evidence")
			}
			return readErr
		})
	}
}

func sameLabKeys(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func (l *databaseLab) mappingCases(t *testing.T) {
	t.Helper()
	decimal := "12345678901234567890.123456789"
	if l.driver == "postgresql" {
		l.statement(t, "DROP VIEW public.observed")
		l.statement(t, "CREATE VIEW public.observed AS SELECT 12345678901234567890.123456789::numeric::text AS appointment, 'ready'::text AS status")
		l.grantView(t)
	} else {
		l.statement(t, "CREATE OR ALTER VIEW dbo.observed AS SELECT CONVERT(varchar(128), CAST(12345678901234567890.123456789 AS decimal(30,9))) AS appointment, CAST('ready' AS nvarchar(32)) AS status")
	}
	l.source.Database.KeyType = "decimal"
	l.collect(t, "decimal", observewindow.Complete, []string{decimal})
	if l.driver == "postgresql" {
		l.statement(t, "DROP VIEW public.observed")
		l.statement(t, "CREATE VIEW public.observed AS SELECT TIMESTAMPTZ '2026-01-03 11:00:00.123456+05:30' AS appointment, 'ready'::text AS status")
		l.grantView(t)
	} else {
		l.statement(t, "CREATE OR ALTER VIEW dbo.observed AS SELECT CONVERT(datetimeoffset(7),'2026-01-03T11:00:00.1234560+05:30') AS appointment, CAST('ready' AS nvarchar(32)) AS status")
	}
	l.source.Database.KeyType = "timestamp"
	l.collect(t, "timezone", observewindow.Complete, []string{"2026-01-03T05:30:00.123456Z"})
}

func (l *databaseLab) restoreTextView(t *testing.T) {
	t.Helper()
	if l.driver == "postgresql" {
		l.statement(t, "DROP VIEW public.observed")
		l.statement(t, "CREATE VIEW public.observed AS SELECT appointment,status FROM public.private_rows")
		l.grantView(t)
	} else {
		l.statement(t, "CREATE OR ALTER VIEW dbo.observed AS SELECT appointment,status FROM dbo.private_rows")
	}
	l.source.Database.KeyType = "text"
}

func (l *databaseLab) blockedQueries(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := l.admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("cannot begin synthetic blocker")
	}
	defer tx.Rollback()
	lock := "LOCK TABLE public.private_rows IN ACCESS EXCLUSIVE MODE"
	if l.driver == "sqlserver" {
		lock = "SELECT appointment FROM dbo.private_rows WITH (TABLOCKX, HOLDLOCK)"
	}
	if l.driver == "postgresql" {
		if _, err := tx.ExecContext(ctx, lock); err != nil {
			t.Fatal("cannot hold synthetic query blocker")
		}
	} else {
		rows, err := tx.QueryContext(ctx, lock)
		if err != nil {
			t.Fatal("cannot hold synthetic query blocker")
		}
		rows.Close()
	}
	l.source.Database.Limits = &DatabaseLimits{Timeout: "250ms", MaxRows: 10000, MaxBytes: 10 << 20}
	l.collect(t, "query-deadline", observewindow.Failed, nil)
	cancelled, stop := context.WithCancel(context.Background())
	timer := time.AfterFunc(100*time.Millisecond, stop)
	root := filepath.Join(l.output, "cancelled")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal("cannot create cancellation evidence")
	}
	got, err := Collect(cancelled, l.source, l.window, Options{Snapshot: filepath.Join(root, "snapshot"), Policy: l.policy})
	timer.Stop()
	stop()
	if err != nil || got.Status != observewindow.Cancelled {
		t.Fatalf("cancelled database read became %s (%T)", got.Status, err)
	}
	if got.AbsenceEvidence() == nil {
		t.Fatal("cancelled read supported absence")
	}
	if err := observewindow.WriteCompletion(filepath.Join(root, "completion.json"), got); err != nil {
		t.Fatal("cannot retain cancellation")
	}
	l.source.Database.Limits = nil
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Fatal("cannot release synthetic blocker")
	}
}

func (l *databaseLab) lostConnection(t *testing.T) {
	t.Helper()
	tx, err := l.admin.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal("cannot start lost-connection query blocker")
	}
	defer tx.Rollback()
	lock := "LOCK TABLE public.private_rows IN ACCESS EXCLUSIVE MODE"
	if l.driver == "postgresql" {
		if _, err := tx.Exec(lock); err != nil {
			t.Fatal("cannot hold lost-connection blocker")
		}
	} else {
		rows, err := tx.Query("SELECT appointment FROM dbo.private_rows WITH (TABLOCKX, HOLDLOCK)")
		if err != nil {
			t.Fatal("cannot hold lost-connection blocker")
		}
		rows.Close()
	}
	root := filepath.Join(l.output, "lost-connection")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal("cannot retain lost-connection evidence")
	}
	type answer struct {
		completion observewindow.Completion
		err        error
	}
	finished := make(chan answer, 1)
	go func() {
		completion, err := Collect(context.Background(), l.source, l.window,
			Options{Snapshot: filepath.Join(root, "snapshot"), Policy: l.policy})
		finished <- answer{completion, err}
	}()
	// Observe the query actually waiting for the setup transaction's lock.
	blocking := "SELECT count(*) FROM pg_stat_activity WHERE usename='observer' AND wait_event_type='Lock'"
	if l.driver == "sqlserver" {
		blocking = "SELECT count(*) FROM sys.dm_exec_requests r JOIN sys.dm_exec_sessions s ON r.session_id=s.session_id WHERE s.login_name='observer' AND r.wait_type LIKE 'LCK_M_%'"
	}
	deadline := time.Now().Add(5 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		var count int
		if l.admin.QueryRow(blocking).Scan(&count) == nil && count > 0 {
			waiting = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("synthetic query never reached its lock before connection loss")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stop := exec.CommandContext(ctx, "docker", "stop", "-t", "1", l.container)
	if stop.Run() != nil {
		t.Fatal("cannot stop synthetic database during a query")
	}
	select {
	case result := <-finished:
		if result.err != nil || result.completion.Status != observewindow.Failed || result.completion.AbsenceEvidence() == nil {
			t.Fatalf("lost database connection became %s or trusted absence", result.completion.Status)
		}
		if err := observewindow.WriteCompletion(filepath.Join(root, "completion.json"), result.completion); err != nil {
			t.Fatal("cannot retain lost-connection completion")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("lost database connection did not stop the read")
	}
	start := exec.CommandContext(ctx, "docker", "start", l.container)
	if start.Run() != nil {
		t.Fatal("cannot restart synthetic database after connection loss")
	}
	recovered := false
	readyBy := time.Now().Add(120 * time.Second)
	for time.Now().Before(readyBy) {
		probe, stopProbe := context.WithTimeout(context.Background(), 2*time.Second)
		err := l.admin.PingContext(probe)
		stopProbe()
		if err == nil {
			recovered = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !recovered {
		t.Fatal("synthetic database did not recover after restart")
	}
}
