package observesource_test

import (
	"github.com/bharm16/readmit/internal/observesource"
	"strings"
	"testing"
)

const databaseSource = `{
 "schema":"readmit-observation-source/v3",
 "source":{"kind":"database-query","identity":"synthetic-lab","scope":"appointments"},
 "enabled":true,"freshness":{"max_age":"1h"},"extraction":null,"file":null,"http":null,"capture":null,
 "database":{"driver":"postgresql","address":"127.0.0.1:5432","classification":"nonproduction",
 "name":"synthetic","username":"observer","ca_file":"","server_name":"localhost",
 "credential":{"store":"customer-managed","address":"127.0.0.1:5432","purpose":"database-observation","command":"/usr/bin/true","arguments":[]},
 "view":["public","observed"],"record_key":"appointment","key_type":"text","filters":[{"column":"status","value":"ready"}],"limits":null}
}`

func TestDatabaseDeclarationRequiresReadCredentialsAndPreservesOlderContracts(t *testing.T) {
	if _, err := observesource.DecodeSource([]byte(databaseSource)); err != nil {
		t.Fatal(err)
	}
	for _, change := range [][2]string{
		{`"purpose":"database-observation"`, `"purpose":"database-reset"`},
		{`"limits":null`, `"limits":{"timeout":"30s","max_rows":10000}`},
		{`"view":["public","observed"]`, `"view":["public","observed;DELETE"]`},
		{`"key_type":"text"`, `"key_type":"guess"`},
		{`"limits":null`, `"limits":null,"query":"DELETE FROM observed"`},
		{`"schema":"readmit-observation-source/v3"`, `"schema":"readmit-observation-source/v2"`},
		{`"arguments":[]`, `"arguments":[],"password":"never"`},
		{`"limits":null`, `"limits":{"timeout":"0s","max_rows":10000,"max_bytes":10485760}`},
	} {
		if _, err := observesource.DecodeSource([]byte(strings.Replace(databaseSource, change[0], change[1], 1))); err == nil {
			t.Errorf("accepted %s", change[1])
		}
	}
}

func TestDatabaseResultReaderRefusesUnknownNullAndOverLimitEvidence(t *testing.T) {
	valid := `{"schema":"readmit-database-read/v1","driver":"postgresql","limits":{"timeout":"30s","max_rows":10000,"max_bytes":10485760},"key_type":"decimal","keys":["12345678901234567890.0123456789"]}`
	if _, err := observesource.DecodeDatabaseRead([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{
		{`"keys":["12345678901234567890.0123456789"]`, `"keys":[null]`},
		{`"max_rows":10000`, `"max_rows":0`},
		{`"max_bytes":10485760`, `"max_bytes":3`},
		{`"key_type":"decimal"`, `"key_type":"float"`},
		{`"schema":"readmit-database-read/v1"`, `"schema":"readmit-database-read/v2"`},
		{`"key_type":"decimal"`, `"key_type":"decimal","password":"hidden"`},
	} {
		if _, err := observesource.DecodeDatabaseRead([]byte(strings.Replace(valid, pair[0], pair[1], 1))); err == nil {
			t.Error("invalid database evidence accepted")
		}
	}
}
