package hub_test

import (
	"bytes"
	"github.com/bharm16/readmit/hub"
	"os"
	"testing"
)

func TestConfigurationRefusesMissingUnknownAndSecretMembers(t *testing.T) {
	for _, data := range []string{`{}`, `null`, `{"schema":"readmit-hub-config/v1","password":"forbidden"}`} {
		if _, err := hub.ReadConfig([]byte(data)); err == nil {
			t.Fatal("accepted incomplete or credential configuration")
		}
	}
}

// This suite uses a fresh disposable PostgreSQL database selected explicitly by
// the operator. No test touches a database without the exact test-only name.

func TestPackagedConfigurationContract(t *testing.T) {
	data, err := os.ReadFile("config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = hub.ReadConfig(data); err != nil {
		t.Fatal("packaged config rejected", err)
	}
	for _, bad := range [][]byte{bytes.Replace(data, []byte(`"postgres_port": 5432`), []byte(`"postgres_port": null`), 1), bytes.Replace(data, []byte(`"postgres_port": 5432`), []byte(`"postgres_port": 5432, "postgres_port": 5433`), 1), bytes.Replace(data, []byte(`"max_storage_bytes"`), []byte(`"unknown"`), 1)} {
		if _, err = hub.ReadConfig(bad); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}
