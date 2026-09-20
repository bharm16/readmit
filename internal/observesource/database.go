package observesource

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/secret"
)

// Database declares one SELECT over an approved view. There is no SQL text,
// procedure, reset operation or connection-string escape hatch. Database grants,
// not this restricted query builder, enforce the read authorization boundary.
type Database struct {
	Driver         string             `json:"driver"`
	Address        string             `json:"address"`
	Classification string             `json:"classification"`
	Name           string             `json:"name"`
	Username       string             `json:"username"`
	CAFile         string             `json:"ca_file"`
	ServerName     string             `json:"server_name"`
	Credential     DatabaseCredential `json:"credential"`
	View           []string           `json:"view"`
	RecordKey      string             `json:"record_key"`
	KeyType        string             `json:"key_type"`
	Filters        []DatabaseFilter   `json:"filters"`
	Limits         *DatabaseLimits    `json:"limits"`
}

// DatabaseCredential intentionally cannot carry a setup/reset purpose. A store
// administrator must bind the reference to a SELECT-only database principal.
type DatabaseCredential struct {
	Store     secret.Store `json:"store"`
	Address   string       `json:"address"`
	Purpose   string       `json:"purpose"`
	Command   string       `json:"command"`
	Arguments []string     `json:"arguments"`
}

type DatabaseFilter struct {
	Column string `json:"column"`
	Value  string `json:"value"`
}

// DatabaseLimits are explicit per-query environment policy. Null selects the
// adopted defaults; effective limits are always retained with each attempt.
type DatabaseLimits struct {
	Timeout  string `json:"timeout"`
	MaxRows  int    `json:"max_rows"`
	MaxBytes int    `json:"max_bytes"`
}

func (d Database) limits() DatabaseLimits {
	if d.Limits != nil {
		return *d.Limits
	}
	return DatabaseLimits{Timeout: "30s", MaxRows: 10000, MaxBytes: 10 << 20}
}

// requiredDatabaseMembers checks presence before strict decoding. Null is only
// allowed for the explicitly defaulted limits member, never for a nested value.
func requiredDatabaseMembers(data []byte, names ...string) error {
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(data, &fields); err != nil {
		return errors.New("invalid database declaration")
	}
	for _, name := range names {
		value, ok := fields[name]
		if !ok || (string(value) == "null" && name != "limits") {
			return errors.New("database declaration requires every member")
		}
	}
	return nil
}
func (d *Database) UnmarshalJSON(data []byte) error {
	if err := requiredDatabaseMembers(data, "driver", "address", "classification", "name", "username", "ca_file", "server_name", "credential", "view", "record_key", "key_type", "filters", "limits"); err != nil {
		return err
	}
	type plain Database
	var v plain
	if err := json.Unmarshal(data, &v, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid database declaration")
	}
	*d = Database(v)
	return nil
}
func (c *DatabaseCredential) UnmarshalJSON(data []byte) error {
	if err := requiredDatabaseMembers(data, "store", "address", "purpose", "command", "arguments"); err != nil {
		return err
	}
	type plain DatabaseCredential
	var v plain
	if err := json.Unmarshal(data, &v, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid database credential reference")
	}
	*c = DatabaseCredential(v)
	return nil
}
func (f *DatabaseFilter) UnmarshalJSON(data []byte) error {
	if err := requiredDatabaseMembers(data, "column", "value"); err != nil {
		return err
	}
	type plain DatabaseFilter
	var v plain
	if err := json.Unmarshal(data, &v, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid database filter")
	}
	*f = DatabaseFilter(v)
	return nil
}
func (l *DatabaseLimits) UnmarshalJSON(data []byte) error {
	if err := requiredDatabaseMembers(data, "timeout", "max_rows", "max_bytes"); err != nil {
		return err
	}
	type plain DatabaseLimits
	var v plain
	if err := json.Unmarshal(data, &v, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid database limits")
	}
	*l = DatabaseLimits(v)
	return nil
}

var databaseIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

func (d Database) validate() error {
	if d.Driver != "postgresql" && d.Driver != "sqlserver" && d.Driver != "oracle" {
		return errors.New("database driver is postgresql, sqlserver or oracle")
	}
	if err := endpointAddress(d.Address); err != nil {
		return errors.New("database requires one endpoint")
	}
	if d.Classification == "" || !databaseIdentifier.MatchString(d.Name) || !databaseIdentifier.MatchString(d.Username) {
		return errors.New("database requires a class, database or service name, and username")
	}
	if len(d.CAFile) > maxPathBytes || d.ServerName == "" || len(d.ServerName) > maxHostBytes {
		return errors.New("database requires an explicit verified TLS server name and bounded CA reference")
	}
	c := d.Credential
	if c.Address != d.Address || c.Purpose != "database-observation" || (c.Store != secret.OSKeychain && c.Store != secret.CustomerManaged) {
		return errors.New("database requires an endpoint-bound observation credential; reset credentials are refused")
	}
	if err := (secret.Locator{Command: c.Command, Arguments: c.Arguments}).Validate(); err != nil {
		return errors.New("invalid database credential locator")
	}
	if len(d.View) < 1 || len(d.View) > 2 || !databaseIdentifier.MatchString(d.RecordKey) {
		return errors.New("database requires an approved view and record-key column")
	}
	for _, part := range d.View {
		if !databaseIdentifier.MatchString(part) {
			return errors.New("invalid database view identifier")
		}
	}
	if len(d.Filters) > 16 {
		return errors.New("at most 16 bound database filters")
	}
	for _, f := range d.Filters {
		if !databaseIdentifier.MatchString(f.Column) || len(f.Value) > 4096 || strings.IndexByte(f.Value, 0) >= 0 {
			return errors.New("invalid database filter")
		}
	}
	return d.validateReading()
}

func (d Database) validateReading() error {
	switch d.KeyType {
	case "text", "integer", "decimal", "timestamp":
	default:
		return errors.New("database key type is text, integer, decimal or timestamp")
	}
	limits := d.limits()
	if _, err := boundedDuration(limits.Timeout, 5*time.Minute); err != nil || limits.MaxRows < 1 || limits.MaxRows > 100000 || limits.MaxBytes < 1 || limits.MaxBytes > MaxReadBytes {
		return errors.New("database limits require a timeout up to five minutes, 1-100000 rows and 1-16777216 bytes")
	}
	return nil
}
