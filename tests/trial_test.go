package tests

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/trial"
)

// This exercises the issuer output through the shipped CLI with real wall UTC;
// an expired historical trial cannot revive simply by being activated today.
func TestSignedTrialRunsLocallyAndExpiryKeepsEvidenceReadableExportable(t *testing.T) {
	active := issuedTrial(t, time.Now().UTC().Truncate(time.Second))
	root := t.TempDir()
	family := filepath.Join(root, "family")
	if out, err := rawOperation(t, "--operation-policy", active, "sample", "synth", "--output", filepath.Join(root, "free-sample")); err != nil {
		t.Fatal(err, out)
	}
	if out, err := rawOperation(t, "--operation-policy", active, "synth", "--seed", "0", "--base-time", "2026-01-01T12:00:00Z", "--generator-version", "readmit-synth-v1", "--profile-version", "readmit-siu-v1", "--output", family); err != nil {
		t.Fatal(err, out)
	}
	expired := issuedTrial(t, time.Now().UTC().Truncate(time.Second).Add(-31*24*time.Hour))
	refused := filepath.Join(root, "refused")
	if out, err := rawOperation(t, "--operation-policy", expired, "project", "init", "--title", "trial", "--interface-version", "v1", "--output", refused); err == nil || !strings.Contains(out, "expired") {
		t.Fatalf("expired new work: %v %s", err, out)
	}
	if _, err := os.Stat(refused); !os.IsNotExist(err) {
		t.Fatal("expired trial created work")
	}
	if out, err := rawOperation(t, "--operation-policy", expired, "timeline", filepath.Join(family, "regression")); err != nil || !strings.Contains(out, "Provenance: generated") {
		t.Fatal(err, out)
	}
	exported := filepath.Join(root, "retained.hl7")
	if out, err := rawOperation(t, "--operation-policy", expired, "inspect", "../testdata/fixtures/adt-cr.hl7", "--roundtrip", exported); err != nil {
		t.Fatal(err, out)
	}
	original, _ := os.ReadFile("../testdata/fixtures/adt-cr.hl7")
	copy, _ := os.ReadFile(exported)
	if string(original) != string(copy) {
		t.Fatal("expired export changed bytes")
	}
}

func issuedTrial(t *testing.T, at time.Time) string {
	t.Helper()
	root := t.TempDir()
	public, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	policy := trial.Policy{Schema: trial.PolicySchema, Plan: "evaluation", Days: 30, ExtensionDays: 14, GraceDays: 0, Authors: 3, DevicesPerAuthor: 2, Runners: 1, Capabilities: []string{"author", "execute", "hub"}}
	request := trial.Request{Organization: "trial-org", ID: "trial", Sequence: 1, Mode: "activation", Starts: at, Authority: "local", Authors: []entitlement.Assignment{{Author: "alice", Devices: []string{"alice-one", "alice-two"}}, {Author: "bob", Devices: []string{"bob-one"}}, {Author: "carol", Devices: []string{"carol-one"}}}}
	_, signed, err := trial.Issue(context.Background(), policy, request, "test-trial-key", key, at)
	if err != nil {
		t.Fatal(err)
	}
	trust, err := entitlement.EncodeTrust(entitlement.Trust{Schema: entitlement.TrustSchema, Keys: []entitlement.Key{{ID: "test-trial-key", Algorithm: entitlement.Algorithm, PublicKey: base64.StdEncoding.EncodeToString(public), Status: entitlement.KeyActive}}})
	if err != nil {
		t.Fatal(err)
	}
	op := operationguard.Policy{Schema: operationguard.PolicySchema, Entitlement: filepath.Join(root, "entitlement.json"), Trust: filepath.Join(root, "trust.json"), State: filepath.Join(root, "clock.json"), Author: "alice", Device: "alice-one", Authority: "local", Admissions: filepath.Join(root, "admissions.json")}
	config, err := operationguard.EncodePolicy(op)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "operation-policy.json")
	for name, data := range map[string][]byte{op.Entitlement: signed, op.Trust: trust, path: config} {
		if err = os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := rawOperation(t, "--operation-policy", path, "license", "operation", "activate"); err != nil {
		t.Fatal(err, out)
	}
	return path
}
