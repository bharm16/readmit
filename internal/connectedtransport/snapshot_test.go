package connectedtransport

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

func TestConfigurationBindingUsesRetainedSnapshotDespiteSelectedFileSubstitution(t *testing.T) {
	dir := t.TempDir()
	a := replay.Target{Schema: replay.TargetSchemaV3, Name: "Lab", Classification: replay.Nonproduction, Address: "127.0.0.1:2575", Transport: "tls", TestEndpoint: true, ConnectTimeout: "1s", MessageTimeout: "1s", MaxACKBytes: 4096, CAFile: "ca-a.pem", ServerName: "receiver.test", ClientCertificate: "client.pem", Credential: replay.Credential{SecretsFile: "secrets.json", Reference: "client"}}
	rawA, _ := json.Marshal(a)
	b := a
	b.CAFile = "ca-b.pem"
	b.Credential.SecretsFile = "other.json"
	rawB, _ := json.Marshal(b)
	os.WriteFile(filepath.Join(dir, "target.json"), rawB, 0600)
	effective, err := replay.DecodeTarget(rawA, dir)
	if err != nil || effective.CAFile != filepath.Join(dir, "ca-a.pem") || effective.Credential.SecretsFile != filepath.Join(dir, "secrets.json") {
		t.Fatalf("substituted target affected binding: %+v %v", effective, err)
	}
	env := connectedtest.Environment{Project: "lab", ID: "test", Endpoint: "receiver"}
	scope := Credential{Schema: CredentialSchema, Project: "lab", Environment: "test", Endpoint: "receiver", Operation: sendpolicy.V2Stimulus, Address: a.Address, Reference: "client", Generation: 1}
	scopeRaw, _ := json.Marshal(scope)
	store := secret.Document{Schema: secret.Schema, References: []secret.Reference{{Name: "client", Store: secret.CustomerManaged, Purpose: secret.MLLPEndpoint, Address: a.Address, Command: filepath.Join(dir, "provider-a"), Arguments: []string{}, Generation: 1, RotatedAt: time.Now()}}}
	retained, _ := json.Marshal(store)
	store.References[0].Command = filepath.Join(dir, "provider-b")
	substitute, _ := json.Marshal(store)
	os.WriteFile(effective.Credential.SecretsFile, substitute, 0600)
	_, locator, err := bindCredentialSnapshot(scopeRaw, retained, env, effective)
	if err != nil || locator.Command != filepath.Join(dir, "provider-a") {
		t.Fatalf("substituted secret store affected locator: %+v %v", locator, err)
	}
}
