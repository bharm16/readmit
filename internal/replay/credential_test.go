package replay_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
)

// credentialStore writes a secret reference document scoped to one address.
// Every reference here names a locator; no credential value exists in this test.
func credentialStore(t *testing.T, directory, address string) {
	t.Helper()
	command := "/usr/bin/true"
	if os.PathSeparator == '\\' {
		command = `C:\Windows\System32\cmd.exe`
	}
	document := secret.Document{Schema: secret.Schema, References: []secret.Reference{{
		Name:       "lab-mllp",
		Store:      secret.OSKeychain,
		Purpose:    secret.MLLPEndpoint,
		Address:    address,
		Command:    command,
		Arguments:  []string{"-s", "readmit-lab"},
		Generation: 1,
		RotatedAt:  time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC),
		MaxAge:     "720h",
	}}}
	if err := secret.WriteStore(filepath.Join(directory, "secrets.json"), document); err != nil {
		t.Fatal(err)
	}
}

func writeTarget(t *testing.T, directory string, config replay.Target) string {
	t.Helper()
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "target.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func credentialTarget(address string) replay.Target {
	config := target(address)
	config.Schema = replay.TargetSchemaV2
	config.Credential = replay.Credential{SecretsFile: "secrets.json", Reference: "lab-mllp"}
	return config
}

// A readmit-target/v2 configuration carries a reference to a credential and is
// read beside unchanged readmit-target/v1 configurations.
func TestTargetV2BindsACredentialReferenceScopedToItsAddress(t *testing.T) {
	directory := t.TempDir()
	credentialStore(t, directory, "127.0.0.1:2575")
	path := writeTarget(t, directory, credentialTarget("127.0.0.1:2575"))
	loaded, err := replay.ReadTarget(path)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !loaded.Credential.Declared() || loaded.Credential.Reference != "lab-mllp" {
		t.Fatalf("target did not carry the credential reference: %+v", loaded.Credential)
	}
	if !filepath.IsAbs(loaded.Credential.SecretsFile) {
		t.Fatal("the declared secrets document was not anchored to the target file")
	}
	bound, err := replay.BindCredential(loaded)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if bound.Name != "lab-mllp" || bound.Address != "127.0.0.1:2575" || bound.Purpose != secret.MLLPEndpoint {
		t.Fatalf("bound reference %+v", bound)
	}
	unchanged := writeTarget(t, t.TempDir(), target("127.0.0.1:2575"))
	previous, err := replay.ReadTarget(unchanged)
	if err != nil || previous.Schema != replay.TargetSchema || previous.Credential.Declared() {
		t.Fatalf("readmit-target/v1 no longer reads unchanged: %v", err)
	}
}

// Every way a declared credential can fall outside its contract or its scope is
// refused, and none of these refusals reads a credential value.
func TestTargetRefusesACredentialOutsideItsVersionOrScope(t *testing.T) {
	for name, build := range map[string]func(*testing.T) (string, string){
		"credential on the previous version": func(t *testing.T) (string, string) {
			directory := t.TempDir()
			credentialStore(t, directory, "127.0.0.1:2575")
			config := credentialTarget("127.0.0.1:2575")
			config.Schema = replay.TargetSchema
			return writeTarget(t, directory, config), "a credential reference requires readmit-target/v2"
		},
		"reference scoped to another endpoint": func(t *testing.T) (string, string) {
			directory := t.TempDir()
			credentialStore(t, directory, "127.0.0.1:9999")
			return writeTarget(t, directory, credentialTarget("127.0.0.1:2575")), "scoped to a different endpoint address"
		},
		"reference that is not registered": func(t *testing.T) (string, string) {
			directory := t.TempDir()
			credentialStore(t, directory, "127.0.0.1:2575")
			config := credentialTarget("127.0.0.1:2575")
			config.Credential.Reference = "absent"
			return writeTarget(t, directory, config), "no credential reference is registered under that name"
		},
		"secrets document that is not there": func(t *testing.T) (string, string) {
			return writeTarget(t, t.TempDir(), credentialTarget("127.0.0.1:2575")), "cannot resolve the secret reference document"
		},
		"credential naming no reference": func(t *testing.T) (string, string) {
			directory := t.TempDir()
			credentialStore(t, directory, "127.0.0.1:2575")
			config := credentialTarget("127.0.0.1:2575")
			config.Credential.Reference = ""
			return writeTarget(t, directory, config), "names both a secrets document and a reference in it"
		},
		"credential naming no secrets document": func(t *testing.T) (string, string) {
			directory := t.TempDir()
			credentialStore(t, directory, "127.0.0.1:2575")
			config := credentialTarget("127.0.0.1:2575")
			config.Credential.SecretsFile = ""
			return writeTarget(t, directory, config), "names both a secrets document and a reference in it"
		},
	} {
		path, want := build(t)
		_, err := replay.ReadTarget(path)
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s was refused as %q, want %q", name, err, want)
		}
	}
}

// Run evidence records the transport it used. readmit-run/v1 is unchanged here,
// so a run manifest names no credential reference and can hold no value.
func TestRunTargetRecordCarriesNoCredential(t *testing.T) {
	raw, err := json.Marshal(replay.TargetRecord{Address: "127.0.0.1:2575", Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "1s", MaxACKBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"credential", "secrets_file", "reference", "lab-mllp"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("the recorded transport named %q: %s", forbidden, raw)
		}
	}
}
