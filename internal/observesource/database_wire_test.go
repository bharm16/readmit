package observesource_test

import (
	"context"
	"crypto/tls"
	"encoding/json/v2"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/jackc/pgx/v5/pgproto3"
)

// This is an external protocol fixture, not a PostgreSQL installation or grant
// acceptance. It exercises the real selected driver and collector end to end.
func postgresFixture(t *testing.T, values [][]byte, oid uint32, deny, stall bool, started ...chan<- struct{}) (string, []byte) {
	t.Helper()
	ca := newAuthority(t)
	pair := ca.leaf(t, nil, []string{"localhost"})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	done := make(chan struct{})
	t.Cleanup(func() { close(done); listener.Close(); wg.Wait() })
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(4 * time.Second))
				backend := pgproto3.NewBackend(conn, conn)
				initial, err := backend.ReceiveStartupMessage()
				if err != nil {
					return
				}
				if _, ok := initial.(*pgproto3.SSLRequest); !ok {
					return
				}
				if _, err := conn.Write([]byte("S")); err != nil {
					return
				}
				secured := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}})
				backend = pgproto3.NewBackend(secured, secured)
				if _, err := backend.ReceiveStartupMessage(); err != nil {
					return
				}
				backend.Send(&pgproto3.AuthenticationOk{})
				backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
				backend.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "16.0"})
				backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
				if backend.Flush() != nil {
					return
				}
				for {
					message, err := backend.Receive()
					if err != nil {
						return
					}
					switch m := message.(type) {
					case *pgproto3.Parse:
						// The independent fixture accepts only a bound read of its synthetic view.
						if m.Query != `SELECT "appointment" FROM "public"."observed" WHERE "status" = $1 LIMIT 10001` && !strings.HasPrefix(m.Query, `SELECT "appointment" FROM "public"."observed" WHERE "status" = $1 LIMIT `) {
							t.Errorf("unexpected query shape")
						}
						backend.Send(&pgproto3.ParseComplete{})
					case *pgproto3.Bind:
						if len(m.Parameters) != 1 || string(m.Parameters[0]) != "ready' OR '1'='1" {
							t.Errorf("filter was not bound separately")
						}
						backend.Send(&pgproto3.BindComplete{})
					case *pgproto3.Describe:
						backend.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{{Name: []byte("appointment"), DataTypeOID: oid, DataTypeSize: -1, TypeModifier: -1}}})
					case *pgproto3.Execute:
						if len(started) > 0 {
							select {
							case started[0] <- struct{}{}:
							default:
							}
						}
						if stall {
							select {
							case <-done:
								return
							case <-time.After(3 * time.Second):
								return
							}
						}
						if deny {
							backend.Send(&pgproto3.ErrorResponse{Severity: "ERROR", Code: "42501", Message: "synthetic-sensitive-driver-detail"})
						} else {
							for _, v := range values {
								backend.Send(&pgproto3.DataRow{Values: [][]byte{v}})
							}
							backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
						}
					case *pgproto3.Sync:
						backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
					case *pgproto3.Terminate:
						return
					}
					if backend.Flush() != nil {
						return
					}
				}
			}()
		}
	}()
	return listener.Addr().String(), ca.pem
}

func databaseDeclared(t *testing.T, address string, ca []byte) observesource.Source {
	t.Helper()
	t.Setenv(providerSwitch, "1")
	// The subprocess is only a credential provider; skip race runtime exit delay
	// so query-budget tests measure the query, not test-binary shutdown.
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	source, err := observesource.DecodeSource([]byte(databaseSource))
	if err != nil {
		t.Fatal(err)
	}
	d := source.Database
	d.Address = address
	d.Credential.Address = address
	d.CAFile = write(t, t.TempDir(), "ca.pem", string(ca))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	d.Credential.Command = executable
	d.Credential.Arguments = []string{write(t, t.TempDir(), "credential", "synthetic-test-password")}
	d.Filters[0].Value = "ready' OR '1'='1"
	return source
}

func TestDatabaseCollectionBindsValuesAndRetainsEffectiveBounds(t *testing.T) {
	address, ca := postgresFixture(t, [][]byte{[]byte("APT-1")}, 25, false, false)
	source := databaseDeclared(t, address, ca)
	w := window(t, strings.ReplaceAll(strings.ReplaceAll(declaredWindow, "file-export", "database-query"), "scheduling-archive", "synthetic-lab"))
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	got, err := observesource.Collect(context.Background(), source, w, observesource.Options{Snapshot: snapshot, Produced: []string{"APT-1"}})
	if err != nil || got.Status != observewindow.Complete {
		t.Fatalf("collection: %s %v", got.Status, err)
	}
	if err := w.Verify(got); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(snapshot, "read-0000", "database.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"max_bytes":10485760`, `"max_rows":10000`, `"timeout":"30s"`, `"keys":["APT-1"]`} {
		if !strings.Contains(string(data), expected) {
			t.Errorf("missing %s", expected)
		}
	}
	if strings.Contains(string(data), "synthetic-test-password") || strings.Contains(string(data), "ready'") {
		t.Fatal("credential/filter leaked")
	}
}

func TestDatabaseFailuresCannotBecomeAnObservedEmptyState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		values  [][]byte
		oid     uint32
		keyType string
		deny    bool
		limits  *observesource.DatabaseLimits
		want    observewindow.Status
	}{
		{name: "null", values: [][]byte{nil}, oid: 25, keyType: "text", want: observewindow.Ambiguous},
		{name: "long text", values: [][]byte{[]byte(strings.Repeat("x", 129))}, oid: 25, keyType: "text", want: observewindow.Ambiguous},
		{name: "permission", oid: 25, keyType: "text", deny: true, want: observewindow.Failed},
		{name: "row bound", values: [][]byte{[]byte("A"), []byte("B")}, oid: 25, keyType: "text", limits: &observesource.DatabaseLimits{Timeout: "1s", MaxRows: 1, MaxBytes: 100}, want: observewindow.Truncated},
		{name: "byte bound", values: [][]byte{[]byte("ABCDE")}, oid: 25, keyType: "text", limits: &observesource.DatabaseLimits{Timeout: "1s", MaxRows: 10, MaxBytes: 4}, want: observewindow.Truncated},
		{name: "zoneless timestamp", values: [][]byte{[]byte("2026-01-03 11:00:00")}, oid: 1114, keyType: "timestamp", want: observewindow.Ambiguous},
		{name: "lossy float", values: [][]byte{[]byte("0.123456789012345")}, oid: 701, keyType: "decimal", want: observewindow.Ambiguous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			address, ca := postgresFixture(t, tc.values, tc.oid, tc.deny, false)
			source := databaseDeclared(t, address, ca)
			source.Database.KeyType = tc.keyType
			source.Database.Limits = tc.limits
			w := window(t, strings.ReplaceAll(strings.ReplaceAll(declaredWindow, "file-export", "database-query"), "scheduling-archive", "synthetic-lab"))
			snapshot := filepath.Join(t.TempDir(), "snapshot")
			got, err := observesource.Collect(context.Background(), source, w, observesource.Options{Snapshot: snapshot})
			if err != nil || got.Status != tc.want {
				t.Fatalf("got %s want %s: %v", got.Status, tc.want, err)
			}
			filepath.WalkDir(snapshot, func(path string, entry os.DirEntry, err error) error {
				if err == nil && !entry.IsDir() {
					data, _ := os.ReadFile(path)
					if strings.Contains(string(data), "synthetic-sensitive-driver-detail") || strings.Contains(string(data), "synthetic-test-password") {
						t.Error("sensitive diagnostic leaked")
					}
				}
				return err
			})
		})
	}
}

func TestDatabaseExactTypesAndObservedEmptyState(t *testing.T) {
	for _, tc := range []struct {
		name, keyType string
		oid           uint32
		value         []byte
		produced      string
		count         int
	}{
		{"empty", "text", 25, nil, "", 0},
		{"decimal", "decimal", 1700, []byte("12345678901234567890.123456789"), "12345678901234567890.123456789", 1},
		{"integer", "integer", 20, []byte("9223372036854775807"), "9223372036854775807", 1},
		{"timezone", "timestamp", 1184, []byte("2026-01-03 11:00:00.123456+05:30"), "2026-01-03T05:30:00.123456Z", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rows [][]byte
			if tc.count > 0 {
				rows = [][]byte{tc.value}
			}
			address, ca := postgresFixture(t, rows, tc.oid, false, false)
			source := databaseDeclared(t, address, ca)
			source.Database.KeyType = tc.keyType
			w := window(t, strings.ReplaceAll(strings.ReplaceAll(declaredWindow, "file-export", "database-query"), "scheduling-archive", "synthetic-lab"))
			options := observesource.Options{Snapshot: filepath.Join(t.TempDir(), "snapshot")}
			if tc.produced != "" {
				options.Produced = []string{tc.produced}
			}
			got, err := observesource.Collect(context.Background(), source, w, options)
			if err != nil || got.Status != observewindow.Complete || got.RecordsObserved != tc.count {
				t.Fatalf("got %s %d %v", got.Status, got.RecordsObserved, err)
			}
			if tc.count > 0 && got.Correlations[0].Kind != observewindow.Matched {
				t.Fatalf("typed key changed: %+v", got.Correlations)
			}
		})
	}
}

func TestDatabaseTLSFailureCancellationAndRetryAreExplicit(t *testing.T) {
	for _, name := range []string{"untrusted", "wrongname", "cancel", "deadline"} {
		t.Run(name, func(t *testing.T) {
			stalled := name == "cancel" || name == "deadline"
			started := make(chan struct{}, 1)
			address, ca := postgresFixture(t, nil, 25, false, stalled, started)
			source := databaseDeclared(t, address, ca)
			switch name {
			case "untrusted":
				source.Database.CAFile = write(t, t.TempDir(), "other.pem", string(newAuthority(t).pem))
			case "wrongname":
				source.Database.ServerName = "another.invalid"
			case "deadline":
				source.Database.Limits = &observesource.DatabaseLimits{Timeout: "500ms", MaxRows: 10, MaxBytes: 100}
			}
			ctx := context.Background()
			want := observewindow.Failed
			if name == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				go func() {
					select {
					case <-started:
						cancel()
					case <-ctx.Done():
					}
				}()
				timer := time.AfterFunc(3*time.Second, cancel)
				defer timer.Stop()
				defer cancel()
				want = observewindow.Cancelled
			}
			w := window(t, strings.ReplaceAll(strings.ReplaceAll(declaredWindow, "file-export", "database-query"), "scheduling-archive", "synthetic-lab"))
			got, err := observesource.Collect(ctx, source, w, observesource.Options{Snapshot: filepath.Join(t.TempDir(), "snapshot")})
			if err != nil || got.Status != want {
				t.Fatalf("got %s want %s: %v", got.Status, want, err)
			}
		})
	}
	// A failed window never overwrites its evidence; recovery is a new attempt
	// against a newly selected healthy endpoint and a fresh output directory.
	address, ca := postgresFixture(t, [][]byte{[]byte("RECOVERED")}, 25, false, false)
	source := databaseDeclared(t, address, ca)
	w := window(t, strings.ReplaceAll(strings.ReplaceAll(declaredWindow, "file-export", "database-query"), "scheduling-archive", "synthetic-lab"))
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	got, err := observesource.Collect(context.Background(), source, w, observesource.Options{Snapshot: snapshot})
	if err != nil || !got.Trustworthy() {
		t.Fatal("fresh recovery did not complete", err)
	}
	if _, err := observesource.Collect(context.Background(), source, w, observesource.Options{Snapshot: snapshot}); err == nil {
		t.Fatal("overwrote retained evidence")
	}
}

func TestOracleTLSVerifiesTheDeclaredNameWithPinnedNetworkAddress(t *testing.T) {
	for _, tc := range []struct {
		name  string
		names []string
		ips   []net.IP
		want  bool
	}{
		{name: "declared name", names: []string{"localhost"}, want: true},
		{name: "IP only certificate", ips: []net.IP{net.ParseIP("127.0.0.1")}, want: false},
		{name: "wrong name", names: []string{"other.invalid"}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ca := newAuthority(t)
			pair := ca.leaf(t, tc.ips, tc.names)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			handshake := make(chan bool, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					handshake <- false
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				secured := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}})
				handshake <- secured.Handshake() == nil
			}()
			source := databaseDeclared(t, listener.Addr().String(), ca.pem)
			source.Database.Driver = "oracle"
			w := window(t, strings.ReplaceAll(strings.ReplaceAll(declaredWindow, "file-export", "database-query"), "scheduling-archive", "synthetic-lab"))
			got, err := observesource.Collect(context.Background(), source, w, observesource.Options{Snapshot: filepath.Join(t.TempDir(), "snapshot")})
			if err != nil || got.Status != observewindow.Failed {
				t.Fatalf("unfinished Oracle protocol should fail, got %s %v", got.Status, err)
			}
			select {
			case accepted := <-handshake:
				if accepted != tc.want {
					t.Fatalf("TLS accepted=%v want %v", accepted, tc.want)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Oracle never attempted bounded TLS")
			}
		})
	}
}

func TestDatabaseCollectionIgnoresAmbientPostgreSQLConfiguration(t *testing.T) {
	for _, name := range []string{"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGPASSFILE", "PGAPPNAME", "PGSSLMODE", "PGSSLSNI", "PGSSLPASSWORD", "PGTZ", "PGOPTIONS", "PGSERVICE", "PGSERVICEFILE", "PGSSLCERT", "PGSSLKEY", "PGSSLROOTCERT", "PGCONNECT_TIMEOUT", "PGTARGETSESSIONATTRS", "PGMINPROTOCOLVERSION", "PGMAXPROTOCOLVERSION", "PGSSLNEGOTIATION", "PGCHANNELBINDING", "PGREQUIREAUTH"} {
		t.Setenv(name, "undeclared-invalid-setting")
	}
	address, ca := postgresFixture(t, [][]byte{[]byte("APT-1")}, 25, false, false)
	source := databaseDeclared(t, address, ca)
	w := window(t, strings.ReplaceAll(strings.ReplaceAll(declaredWindow, "file-export", "database-query"), "scheduling-archive", "synthetic-lab"))
	got, err := observesource.Collect(context.Background(), source, w, observesource.Options{Snapshot: filepath.Join(t.TempDir(), "snapshot")})
	if err != nil || got.Status != observewindow.Complete {
		t.Fatalf("ambient configuration affected explicit source: %s %v", got.Status, err)
	}
}
