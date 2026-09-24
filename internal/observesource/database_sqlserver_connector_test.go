package observesource

import (
	"context"
	"crypto/tls"
	"database/sql"
	"net"
	"testing"
	"time"
)

// The pinned SQL Server driver needs an explicit protocol list when a typed
// Config is constructed directly. A refused TCP endpoint must return an error
// within its bound, never a nil connection that panics during prelogin.
func TestSQLServerConnectorRefusesClosedTCPWithoutPanic(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	d := Database{Driver: "sqlserver", Address: address, Name: "synthetic", Username: "observer",
		ServerName: "database.lab.invalid", Limits: &DatabaseLimits{Timeout: "500ms", MaxRows: 1, MaxBytes: 128}}
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: d.ServerName}
	connector, err := databaseConnector(d, "synthetic-only", address, config)
	if err != nil {
		t.Fatal("cannot configure bounded SQL Server connection")
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if db.PingContext(ctx) == nil {
		t.Fatal("closed TCP endpoint was accepted")
	}
}
