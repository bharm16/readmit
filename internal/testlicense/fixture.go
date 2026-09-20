// Package testlicense creates explicit ephemeral activation fixtures for tests.
// It is never imported by a production entry point and embeds no signing key.
package testlicense

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/operationguard"
)

// Create writes a newly signed test entitlement and activates its local policy.
func Create(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Truncate(time.Second)
	claims := entitlement.ClaimsV2{ID: "test-entitlement", Organization: "test-organization", Plan: "test-only", Sequence: 1, Issued: now.Add(-time.Hour), NotBefore: now.Add(-time.Hour), Expires: now.Add(24 * time.Hour), GraceDays: 0, Authors: entitlement.Authors{Seats: 1, DevicesPerSeat: 2, Assignments: []entitlement.Assignment{{Author: "test-author", Devices: []string{"test-device"}}}}, Runners: entitlement.Runners{Instances: 16, Authorities: []entitlement.Authority{{ID: "test-runner", Instances: 16}}}, Capabilities: []string{"author", "execute", "hub"}}
	signed, err := entitlement.SignV2(claims, "test-key", private)
	if err != nil {
		return "", err
	}
	grant, err := entitlement.EncodeV2(signed)
	if err != nil {
		return "", err
	}
	trust, err := entitlement.EncodeTrust(entitlement.Trust{Schema: entitlement.TrustSchema, Keys: []entitlement.Key{{ID: "test-key", Algorithm: entitlement.Algorithm, PublicKey: base64.StdEncoding.EncodeToString(public), Status: entitlement.KeyActive}}})
	if err != nil {
		return "", err
	}
	policy := operationguard.Policy{Schema: "readmit-operation-policy/v1", Entitlement: filepath.Join(dir, "entitlement.json"), Trust: filepath.Join(dir, "trust.json"), State: filepath.Join(dir, "clock.json"), Author: "test-author", Device: "test-device", Authority: "test-runner", Admissions: filepath.Join(dir, "admissions.json")}
	data, err := operationguard.EncodePolicy(policy)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "operation-policy.json")
	for name, content := range map[string][]byte{policy.Entitlement: grant, policy.Trust: trust, path: data} {
		if err := os.WriteFile(name, content, 0600); err != nil {
			return "", err
		}
	}
	if err := operationguard.Activate(path); err != nil {
		return "", err
	}
	return path, nil
}

// New creates an isolated explicit policy for a test.
func New(t testing.TB) string {
	t.Helper()
	path, err := Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}
