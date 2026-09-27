package observesource

import (
	"context"
	"encoding/json/v2"
	"time"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/secret"
)

func (r *databaseReader) readDataset(ctx context.Context, p dataset.Projection) (taken attempt) {
	taken = attempt{at: time.Now(), record: Evidence{Kind: DatabaseQuery, Attempts: 1}}
	defer func() {
		if r.complete != nil && r.complete(taken) != nil {
			taken = failure(taken, observewindow.SampleFailed, "scoped database evidence could not be finalized")
		}
	}()
	check := func() bool { return ctx.Err() == nil && (r.check == nil || r.check(ctx) == nil) }
	if !check() {
		return failure(taken, observewindow.SampleFailed, "database authority is not current")
	}
	record := dataset.DatabaseRead{Schema: dataset.DatabaseSchema, Driver: r.declaration.Driver, Columns: []dataset.DatabaseColumn{}, Rows: [][]dataset.DriverValue{}}
	fail := func(status observewindow.SampleStatus) attempt {
		raw, _ := json.Marshal(record, json.Deterministic(true))
		if len(raw) <= dataset.MaxBytes {
			taken.evidence = map[string][]byte{"dataset-database.json": raw}
		}
		return failure(taken, status, "typed database snapshot did not complete")
	}
	d := r.declaration
	limits := d.limits()
	limits.MaxRows = min(limits.MaxRows, p.Limits.MaxRows)
	limits.MaxBytes = min(limits.MaxBytes, p.Limits.MaxBytes)
	d.Limits = &limits
	timeout, _ := time.ParseDuration(limits.Timeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if r.db == nil {
		key, err := (secret.Locator{Command: d.Credential.Command, Arguments: d.Credential.Arguments}).Read(ctx)
		if err != nil {
			return fail(observewindow.SampleFailed)
		}
		r.db, err = openDatabase(d, string(key.Expose()), r.route, r.tls)
		if err != nil {
			return fail(observewindow.SampleFailed)
		}
	}
	if !check() {
		return fail(observewindow.SampleFailed)
	}
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return fail(observewindow.SampleFailed)
	}
	defer conn.Close()
	names := []string{}
	for _, c := range p.Columns {
		names = append(names, c.Locator[0])
	}
	query, args := databaseProjectionQuery(d, names)
	if !check() {
		return fail(observewindow.SampleFailed)
	}
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return fail(observewindow.SampleFailed)
	}
	defer rows.Close()
	columns, err := rows.ColumnTypes()
	if err != nil || len(columns) != len(names) {
		return fail(observewindow.SampleAmbiguous)
	}

	for i, c := range columns {
		if c.Name() != names[i] {
			return fail(observewindow.SampleAmbiguous)
		}
		precision, scale, known := c.DecimalSize()
		record.Columns = append(record.Columns, dataset.DatabaseColumn{Name: c.Name(), Type: c.DatabaseTypeName(), Precision: precision, Scale: scale, DecimalSizeKnown: known})
	}
	used := 0
	for rows.Next() {
		if len(record.Rows) >= limits.MaxRows {
			return fail(observewindow.SampleTruncated)
		}
		values := make([]any, len(names))
		pointers := make([]any, len(names))
		for i := range values {
			pointers[i] = &values[i]
		}
		if rows.Scan(pointers...) != nil {
			return fail(observewindow.SampleFailed)
		}
		row := []dataset.DriverValue{}
		for _, v := range values {
			value, err := dataset.FromDriver(v)
			if err != nil {
				return fail(observewindow.SampleAmbiguous)
			}
			row = append(row, value)
		}
		raw, err := json.Marshal(row, json.Deterministic(true))
		if err != nil {
			return fail(observewindow.SampleAmbiguous)
		}
		used += len(raw)
		if used > limits.MaxBytes {
			return fail(observewindow.SampleTruncated)
		}
		record.Rows = append(record.Rows, row)
	}
	if rows.NextResultSet() || rows.Err() != nil || rows.Close() != nil || ctx.Err() != nil {
		return fail(observewindow.SampleFailed)
	}
	raw, err := json.Marshal(record, json.Deterministic(true))
	if err != nil || len(raw) > limits.MaxBytes {
		return fail(observewindow.SampleTruncated)
	}
	queryStarted := taken.at
	taken.at = time.Now()
	taken.record.Bytes = len(raw)
	taken.record.Records = len(record.Rows)
	taken.evidence = map[string][]byte{"dataset-database.json": raw}
	if !dateState(&taken, queryStarted, r.maxAge) {
		return fail(observewindow.SampleStale)
	}
	taken.status = observewindow.Observed
	return taken
}
