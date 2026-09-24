package observesource

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/secret"
)

type databaseReader struct {
	declaration Database
	route       destination.Route
	tls         *tls.Config
	db          *sql.DB
	maxAge      time.Duration
}

func (r *databaseReader) close() {
	if r.db != nil {
		r.db.Close()
	}
}

func newDatabaseReader(ctx context.Context, d Database, retained *snapshot, options Options, maxAge time.Duration) (reader, error) {
	timeout, _ := time.ParseDuration(d.limits().Timeout)
	decision, err := destination.Decide(ctx, destination.Request{
		Purpose: destination.Observe, Address: d.Address, Classification: d.Classification,
		Policy: options.Policy, Budget: timeout, Record: retained.retainDecision, Resolve: options.Resolve,
	})
	if err != nil {
		return nil, err
	}
	route, admitted := decision.Route()
	if !admitted {
		return nil, errors.New("database observation refused by destination policy")
	}
	ca, err := authorities(d.CAFile)
	if err != nil {
		return nil, err
	}
	config, err := route.ClientConfig(destination.Security{ServerName: d.ServerName, Authorities: ca})
	if err != nil {
		return nil, err
	}
	return &databaseReader{declaration: d, route: route, tls: config, maxAge: maxAge}, nil
}

// databaseQuery has a fixed SELECT shape. Values always travel separately as
// driver parameters. Validated and quoted identifiers cannot introduce SQL.
func databaseQuery(d Database) (string, []any) {
	quote := func(s string) string { return `"` + s + `"` }
	if d.Driver == "sqlserver" {
		quote = func(s string) string { return "[" + s + "]" }
	}
	parts := make([]string, len(d.View))
	for i, p := range d.View {
		parts[i] = quote(p)
	}
	prefix := "SELECT "
	limit := strconv.Itoa(d.limits().MaxRows + 1)
	if d.Driver == "sqlserver" {
		prefix += "TOP (" + limit + ") "
	}
	query := prefix + quote(d.RecordKey) + " FROM " + strings.Join(parts, ".")
	args := make([]any, 0, len(d.Filters))
	for i, f := range d.Filters {
		if i == 0 {
			query += " WHERE "
		} else {
			query += " AND "
		}
		parameter := "$" + strconv.Itoa(i+1)
		if d.Driver == "sqlserver" {
			parameter = "@p" + strconv.Itoa(i+1)
		}
		if d.Driver == "oracle" {
			parameter = ":" + strconv.Itoa(i+1)
		}
		query += quote(f.Column) + " = " + parameter
		args = append(args, f.Value)
	}
	if d.Driver == "postgresql" {
		query += " LIMIT " + limit
	}
	if d.Driver == "oracle" {
		query += " FETCH FIRST " + limit + " ROWS ONLY"
	}
	return query, args
}

// Each read keeps a new versioned record of effective bounds. It carries no
// connection string, credential, filter value, driver error or server version.
type DatabaseRead struct {
	Schema  string         `json:"schema"`
	Driver  string         `json:"driver"`
	Limits  DatabaseLimits `json:"limits"`
	KeyType string         `json:"key_type"`
	Keys    []string       `json:"keys"`
}

func (r *databaseReader) read(ctx context.Context) attempt {
	d := r.declaration
	limits := d.limits()
	timeout, _ := time.ParseDuration(limits.Timeout)
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	taken := attempt{at: time.Now(), record: Evidence{Kind: DatabaseQuery, Attempts: 1}}
	metadata := DatabaseRead{Schema: "readmit-database-read/v1", Driver: d.Driver, Limits: limits, KeyType: d.KeyType, Keys: []string{}}
	// Every attempt retains its bounds, including failed/cancelled queries. The
	// outer window omits interrupted attempts under its existing contract.
	if r.db == nil {
		value, err := (secret.Locator{Command: d.Credential.Command, Arguments: d.Credential.Arguments}).Read(bounded)
		if err != nil {
			return databaseFailure(taken, metadata, observewindow.SampleFailed, "database credential could not be resolved")
		}
		r.db, err = openDatabase(d, string(value.Expose()), r.route, r.tls)
		if err != nil {
			return databaseFailure(taken, metadata, observewindow.SampleFailed, "database connector could not be configured")
		}
	}
	query, args := databaseQuery(d)
	// A dedicated connection avoids database/sql's automatic query retry on
	// ErrBadConn. One sample means one attempted query.
	connection, err := r.db.Conn(bounded)
	if err != nil {
		return databaseFailure(taken, metadata, observewindow.SampleFailed, "database connection failed or its deadline expired")
	}
	defer connection.Close()
	rows, err := connection.QueryContext(bounded, query, args...)
	if err != nil {
		return databaseFailure(taken, metadata, observewindow.SampleFailed, "database query failed or its deadline expired")
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil || len(columns) != 1 || columns[0] != d.RecordKey {
		return databaseFailure(taken, metadata, observewindow.SampleAmbiguous, "database column mapping did not match")
	}
	columnTypes, err := rows.ColumnTypes()
	if err != nil || len(columnTypes) != 1 {
		return databaseFailure(taken, metadata, observewindow.SampleAmbiguous, "database column type was unavailable")
	}
	// The row cap is pushed to the server and independently enforced here. The
	// byte bound counts returned key bytes before appending; a driver can still
	// allocate a single wire value, so approved views must bound field sizes.
	for rows.Next() {
		if len(metadata.Keys) >= limits.MaxRows {
			return databaseFailure(taken, metadata, observewindow.SampleTruncated, "database row limit exceeded")
		}
		var value any
		if err := rows.Scan(&value); err != nil {
			return databaseFailure(taken, metadata, observewindow.SampleFailed, "database row could not be read")
		}
		if _, nativeTime := value.(time.Time); nativeTime && d.KeyType == "timestamp" {
			switch strings.ToUpper(columnTypes[0].DatabaseTypeName()) {
			case "TIMESTAMPTZ", "DATETIMEOFFSET", "TIMESTAMP WITH TIME ZONE", "TIMESTAMPTZ_DTY":
			default:
				return databaseFailure(taken, metadata, observewindow.SampleAmbiguous, "database timestamp lacks a verified timezone-aware type")
			}
		}
		key, size, err := databaseKey(d.KeyType, value)
		if size > limits.MaxBytes-taken.record.Bytes {
			return databaseFailure(taken, metadata, observewindow.SampleTruncated, "database byte limit exceeded")
		}
		taken.record.Bytes += size
		if err != nil {
			return databaseFailure(taken, metadata, observewindow.SampleAmbiguous, "database key was null, unsupported, lossy or outside the correlation-key contract")
		}
		metadata.Keys = append(metadata.Keys, key)
	}
	if rows.NextResultSet() {
		return databaseFailure(taken, metadata, observewindow.SampleAmbiguous, "database returned unexpected additional results")
	}
	if rows.Err() != nil || rows.Close() != nil || bounded.Err() != nil {
		return databaseFailure(taken, metadata, observewindow.SampleFailed, "database result did not finish within the declared bound")
	}
	queryStarted := taken.at
	taken.at = time.Now()
	maxAge := r.maxAge
	if !dateState(&taken, queryStarted, maxAge) {
		return databaseFailure(taken, metadata, observewindow.SampleStale, "database query snapshot exceeded freshness bound")
	}
	taken.status = observewindow.Observed
	taken.keys = metadata.Keys
	data, _ := json.Marshal(metadata, json.Deterministic(true))
	taken.evidence = map[string][]byte{"database.json": append(data, '\n')}
	return taken
}
func databaseFailure(taken attempt, metadata DatabaseRead, status observewindow.SampleStatus, note string) attempt {
	// A refused prefix is never retained as a settled key set.
	metadata.Keys = []string{}
	data, _ := json.Marshal(metadata, json.Deterministic(true))
	taken.evidence = map[string][]byte{"database.json": append(data, '\n')}
	return failure(taken, status, note)
}

var exactDecimal = regexp.MustCompile(`^[+-]?[0-9]+(?:\.[0-9]+)?$`)

func databaseKey(kind string, value any) (string, int, error) {
	var key string
	switch v := value.(type) {
	case string:
		key = v
	case []byte:
		if len(v) > 128 {
			return "", len(v), errRecordKey
		}
		key = string(v)
	case int64:
		if kind != "integer" {
			return "", 8, errors.New("unexpected integer")
		}
		key = strconv.FormatInt(v, 10)
	case time.Time:
		if kind != "timestamp" {
			return "", 16, errors.New("unexpected timestamp")
		}
		key = v.UTC().Format(time.RFC3339Nano)
	default:
		return "", 0, errors.New("null or unsupported database type")
	}
	size := len(key)
	valid := printableKey(key)
	switch kind {
	case "integer":
		_, err := strconv.ParseInt(key, 10, 64)
		valid = valid && err == nil
	case "decimal":
		valid = valid && exactDecimal.MatchString(key)
	case "timestamp":
		_, err := time.Parse(time.RFC3339Nano, key)
		valid = valid && err == nil
	}
	if !valid {
		return "", size, errRecordKey
	}
	return key, size, nil
}
