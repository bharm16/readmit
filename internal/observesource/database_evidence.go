package observesource

import (
	"encoding/json/v2"
	"errors"
)

// DecodeDatabaseRead reads the typed SQL result retained beside a sample. It
// validates representation and bounds, not authenticity or a completion; the
// observation window still owns those verdicts. SQL protocol bytes are not
// available through database/sql: this record explicitly preserves decoded
// keys, with decimal text unchanged and timestamps as UTC instants.
func DecodeDatabaseRead(data []byte) (DatabaseRead, error) {
	if len(data) > 32<<20 {
		return DatabaseRead{}, errors.New("database read exceeds artifact bound")
	}
	var record DatabaseRead
	if err := json.Unmarshal(data, &record); err != nil {
		return DatabaseRead{}, errors.New("invalid database read record")
	}
	if record.Schema != "readmit-database-read/v1" {
		return DatabaseRead{}, errors.New("unsupported database read version")
	}
	d := Database{Driver: record.Driver, KeyType: record.KeyType, Limits: &record.Limits}
	if record.Driver != "postgresql" && record.Driver != "sqlserver" && record.Driver != "oracle" {
		return DatabaseRead{}, errors.New("unknown database read driver")
	}
	if err := d.validateReading(); err != nil {
		return DatabaseRead{}, err
	}
	if len(record.Keys) > record.Limits.MaxRows {
		return DatabaseRead{}, errors.New("database read exceeds declared rows")
	}
	used := 0
	for _, key := range record.Keys {
		_, size, err := databaseKey(record.KeyType, key)
		if err != nil {
			return DatabaseRead{}, errors.New("invalid typed database key")
		}
		used += size
		if used > record.Limits.MaxBytes {
			return DatabaseRead{}, errors.New("database read exceeds declared bytes")
		}
	}
	return record, nil
}

func (d *DatabaseRead) UnmarshalJSON(data []byte) error {
	if err := requiredDatabaseMembers(data, "schema", "driver", "limits", "key_type", "keys"); err != nil {
		return err
	}
	type plain DatabaseRead
	var record plain
	if err := json.Unmarshal(data, &record, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid database read record")
	}
	*d = DatabaseRead(record)
	return nil
}
