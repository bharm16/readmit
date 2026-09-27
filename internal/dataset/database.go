package dataset

import (
	"encoding/json/v2"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/importer"
)

type DatabaseColumn struct {
	Name             string `json:"name"`
	Type             string `json:"database_type"`
	Precision        int64  `json:"precision"`
	Scale            int64  `json:"scale"`
	DecimalSizeKnown bool   `json:"decimal_size_known"`
}
type DriverValue struct {
	Kind  string `json:"kind"`
	Text  string `json:"text,omitzero"`
	Bytes []byte `json:"bytes,omitzero"`
}
type DatabaseRead struct {
	Schema  string           `json:"schema"`
	Driver  string           `json:"driver"`
	Columns []DatabaseColumn `json:"columns"`
	Rows    [][]DriverValue  `json:"rows"`
}

// FromDriver retains the database/sql value as returned. Decimal float64 values
// remain floats and cannot later masquerade as exact decimal text.
func FromDriver(value any) (DriverValue, error) {
	switch v := value.(type) {
	case nil:
		return DriverValue{Kind: "null"}, nil
	case string:
		if !utf8.ValidString(v) {
			return DriverValue{Kind: "string-bytes", Bytes: []byte(v)}, nil
		}
		return DriverValue{Kind: "string", Text: v}, nil
	case []byte:
		return DriverValue{Kind: "bytes", Bytes: append([]byte(nil), v...)}, nil
	case int64:
		return DriverValue{Kind: "int64", Text: strconv.FormatInt(v, 10)}, nil
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return DriverValue{}, invalid
		}
		return DriverValue{Kind: "float64", Text: strconv.FormatFloat(v, 'g', -1, 64)}, nil
	case bool:
		return DriverValue{Kind: "boolean", Text: strconv.FormatBool(v)}, nil
	case time.Time:
		return DriverValue{Kind: "time", Text: v.Format(time.RFC3339Nano)}, nil
	}
	return DriverValue{}, invalid
}
func projectDatabase(p Projection, raw []byte) ([]Row, error) {
	var d DatabaseRead
	if json.Unmarshal(raw, &d, json.RejectUnknownMembers(true)) != nil || d.Schema != DatabaseSchema || !contains([]string{"postgresql", "sqlserver", "oracle"}, d.Driver) || len(d.Columns) != len(p.Columns) || len(d.Rows) > 10001 {
		return nil, invalid
	}
	for i, c := range d.Columns {
		if c.Name != p.Columns[i].Locator[0] || c.Type == "" || len(c.Type) > 128 {
			return nil, invalid
		}
	}
	rows := []Row{}
	budget := rowBudget{}
	for i, values := range d.Rows {
		if len(values) != len(p.Columns) {
			return nil, invalid
		}
		row := Row{ID: fmt.Sprintf("row-%06d", i+1), Provenance: Provenance{Record: i + 1, SourceRecord: strconv.Itoa(i + 1)}, Values: []Value{}}
		for j, value := range values {
			column := p.Columns[j]
			source := importer.ProjectedValue{State: "present", Kind: "string", Text: value.Text}
			switch value.Kind {
			case "null":
				source.State = "null"
			case "string":
				if value.Text == "" {
					source.State = "empty"
				}
			case "bytes", "string-bytes":
				source.Text = string(value.Bytes)
				if len(value.Bytes) == 0 {
					source.State = "empty"
				}
			case "int64":
				if _, err := strconv.ParseInt(value.Text, 10, 64); err != nil {
					return nil, invalid
				}
				source.Kind = "number"
			case "float64":
				source.State = "invalid"
			case "boolean":
				source.Kind = "boolean"
			case "time":
				t, err := time.Parse(time.RFC3339Nano, value.Text)
				if err != nil {
					return nil, invalid
				}
				switch strings.ToUpper(d.Columns[j].Type) {
				case "DATE":
					if d.Driver == "oracle" {
						source.Text = t.Format("2006-01-02T15:04:05")
					} else {
						source.Text = t.Format("2006-01-02")
					}
				case "TIMESTAMP", "TIMESTAMP WITHOUT TIME ZONE", "DATETIME", "DATETIME2":
					source.Text = t.Format("2006-01-02T15:04:05.999999999")
				case "TIMESTAMPTZ", "TIMESTAMP WITH TIME ZONE", "DATETIMEOFFSET", "TIMESTAMPTZ_DTY":
				default:
					source.State = "invalid"
				}
			default:
				return nil, invalid
			}
			if value.Kind != "bytes" && value.Kind != "string-bytes" && len(value.Bytes) != 0 || (value.Kind == "bytes" || value.Kind == "string-bytes") && value.Text != "" || value.Kind == "null" && value.Text != "" {
				return nil, invalid
			}
			row.Values = append(row.Values, projectValue(column, source))
		}
		if !budget.take(row) {
			return nil, importer.ErrProjectionLimit
		}
		rows = append(rows, row)
		if i >= p.Limits.MaxRows {
			break
		}
	}
	return rows, nil
}
