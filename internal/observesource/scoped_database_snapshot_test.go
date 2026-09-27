package observesource_test

import (
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestDatabaseVerificationUsesSuppliedSnapshotAfterValidDiskReplacement(t *testing.T) {
	address, ca := postgresFixture(t, [][]byte{[]byte("A1")}, 25, false, false)
	source := databaseDeclared(t, address, ca)
	projection := datasetProjection("database")
	projection.Order = "unordered"
	projection.Columns = projection.Columns[:1]
	_, port, _ := net.SplitHostPort(address)
	number, _ := strconv.Atoi(port)
	policy, _ := json.Marshal(sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "observation", Operation: sendpolicy.ObservationRead, Port: number, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"}}})
	action, err := observesource.PrepareDatabaseAction(source, projection, observesource.DatabaseActionContext{Plan: networkaction.Digest([]byte("plan")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "observation", CredentialGeneration: "1"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	request := datasetRequest(source, projection, filepath.Join(t.TempDir(), "scoped"))
	request.DatabaseNetwork = action
	request.NetworkAuthority = datasetAuthority{binding: action.Binding(), actor: networkaction.Actor{Kind: "runner", ID: "reader-a", Generation: "1", EvidenceIdentity: networkaction.Digest([]byte("grant")), Expires: time.Now().Add(time.Hour)}}
	if result, err := observesource.CollectDataset(context.Background(), request); err != nil || !result.Usable() {
		t.Fatal(err)
	}
	dir := filepath.Join(request.Output, "network")
	files := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		files[entry.Name()], err = os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
	}
	var changed networkaction.Result
	if err = json.Unmarshal(files["result.json"], &changed); err != nil {
		t.Fatal(err)
	}
	changed.Actor.ID = "reader-b"
	raw, _ := json.Marshal(changed, json.Deterministic(true))
	replacement := map[string][]byte{}
	for name, b := range files {
		replacement[name] = b
	}
	replacement["result.json"] = raw
	replacement["identity.sha256"] = []byte(artifactdir.Identity(networkaction.ResultSchema, replacement) + "\n")
	for _, name := range []string{"result.json", "identity.sha256"} {
		if err = os.WriteFile(filepath.Join(dir, name), replacement[name], 0600); err != nil {
			t.Fatal(err)
		}
	}
	disk, err := observesource.OpenDatabaseAction(dir)
	if err != nil || disk.Actor.ID != "reader-b" {
		t.Fatal(disk, err)
	}
	retained, err := observesource.VerifyDatabaseAction(files)
	if err != nil || retained.Actor.ID != "reader-a" {
		t.Fatal(retained, err)
	}
	files["unexpected"] = []byte("anything")
	if _, err = observesource.VerifyDatabaseAction(files); err == nil {
		t.Fatal("snapshot verifier bypassed layout")
	}
}
