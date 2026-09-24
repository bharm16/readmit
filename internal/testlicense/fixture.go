// Package testlicense creates explicit ephemeral activation fixtures for tests.
// It is never imported by a production entry point and embeds no signing key.
package testlicense

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/operationguard"
)

// Create writes a newly signed test entitlement and activates its local policy.
func Create(dir string) (string, error) {
	paths, err := CreateIssues([]Issue{{Dir: dir, Term: Term{Sequence: 1, Expires: 24 * time.Hour}}})
	if err != nil {
		return "", err
	}
	return paths[0], nil
}

// Term is what a test varies about one issue of the test entitlement: its
// issue sequence, when it expires relative to now (negative for a term that is
// already over) and its grace period in days.
type Term struct {
	Sequence  int
	Expires   time.Duration
	GraceDays int
}

// Issue is one activation folder and the term signed into it.
type Issue struct {
	Dir  string
	Term Term
}

// CreateIssues writes one activated folder per issue, as Create writes one,
// through the operation guard's activation-folder creation.
// Every issue is signed with the same newly generated key and trusted by the
// same trust document, so a later issue installs as a renewal of an earlier
// one; the key is discarded when CreateIssues returns. It returns each
// folder's operation policy path, in order.
func CreateIssues(issues []Issue) ([]string, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	trust, err := entitlement.EncodeTrust(entitlement.Trust{Schema: entitlement.TrustSchema, Keys: []entitlement.Key{{ID: "test-key", Algorithm: entitlement.Algorithm, PublicKey: base64.StdEncoding.EncodeToString(public), Status: entitlement.KeyActive}}})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	paths := make([]string, 0, len(issues))
	for _, issue := range issues {
		dir, err := filepath.Abs(issue.Dir)
		if err != nil {
			return nil, err
		}
		expires := now.Add(issue.Term.Expires)
		notBefore := now.Add(-time.Hour)
		if !expires.After(notBefore) {
			notBefore = expires.Add(-24 * time.Hour)
		}
		claims := entitlement.ClaimsV2{ID: "test-entitlement", Organization: "test-organization", Plan: "test-only", Sequence: issue.Term.Sequence, Issued: notBefore, NotBefore: notBefore, Expires: expires, GraceDays: issue.Term.GraceDays, Authors: entitlement.Authors{Seats: 1, DevicesPerSeat: 2, Assignments: []entitlement.Assignment{{Author: "test-author", Devices: []string{"test-device"}}}}, Runners: entitlement.Runners{Instances: 16, Authorities: []entitlement.Authority{{ID: "test-runner", Instances: 16}}}, Capabilities: []string{"author", "execute", "hub"}}
		signed, err := entitlement.SignV2(claims, "test-key", private)
		if err != nil {
			return nil, err
		}
		grant, err := entitlement.EncodeV2(signed)
		if err != nil {
			return nil, err
		}
		path, err := operationguard.CreateActivation(dir, grant, trust, "test-author", "test-device", "test-runner", now)
		if err != nil {
			return nil, err
		}
		if err := operationguard.Activate(path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
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
