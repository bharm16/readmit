package trial_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/trial"
	"testing"
	"time"
)

func TestExplicitActivationIssuesNamedCapacityAndExactlyOneApprovedExtension(t *testing.T) {
	at := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	public, key, _ := ed25519.GenerateKey(nil)
	policy := trial.Policy{Schema: trial.PolicySchema, Plan: "evaluation", Days: 30, ExtensionDays: 14, GraceDays: 0, Authors: 3, DevicesPerAuthor: 2, Runners: 1, Capabilities: []string{"author", "execute", "hub"}}
	request := trial.Request{Organization: "org", ID: "trial-1", Sequence: 1, Mode: "activation", Starts: at, Authors: []entitlement.Assignment{{Author: "a", Devices: []string{"a1", "a2"}}, {Author: "b", Devices: []string{"b1"}}, {Author: "c", Devices: []string{"c1"}}}, Authority: "runner"}
	account, data, err := trial.Issue(context.Background(), policy, request, "test", key, at)
	if err != nil {
		t.Fatal(err)
	}
	trust := entitlement.Trust{Schema: entitlement.TrustSchema, Keys: []entitlement.Key{{ID: "test", Algorithm: "ed25519", PublicKey: base64.StdEncoding.EncodeToString(public), Status: entitlement.KeyActive}}}
	grant, err := entitlement.VerifyV2(data, trust)
	if err != nil {
		t.Fatal(err)
	}
	if grant.Claims.Expires.Format(time.RFC3339) != "2026-10-20T00:00:00Z" || grant.Claims.Authors.Seats != 3 || grant.Claims.Runners.Instances != 1 || grant.Claims.GraceDays != 0 {
		t.Fatalf("wrong trial: %+v", grant.Claims)
	}
	if _, _, err = account.Extend(context.Background(), "trial-2", 2, false, "test", key, at.Add(24*time.Hour)); err == nil {
		t.Fatal("unapproved extension")
	}
	next, data, err := account.Extend(context.Background(), "trial-2", 2, true, "test", key, at.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	grant, err = entitlement.VerifyV2(data, trust)
	if err != nil {
		t.Fatal(err)
	}
	if grant.Claims.Expires.Format(time.RFC3339) != "2026-11-03T00:00:00Z" || grant.Claims.GraceDays != 0 {
		t.Fatal("extension did not preserve original term", grant.Claims)
	}
	if _, _, err = next.Extend(context.Background(), "trial-3", 3, true, "test", key, at.Add(48*time.Hour)); err == nil {
		t.Fatal("second extension")
	}
	encoded, err := trial.Encode(next)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := trial.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = restored.Extend(context.Background(), "trial-3", 3, true, "test", key, at.Add(48*time.Hour)); err == nil {
		t.Fatal("reload lost extension approval")
	}
}

func TestTrialPolicyRequiresEveryMemberAndFullFeatureCapabilities(t *testing.T) {
	raw := []byte(`{"schema":"readmit-trial-policy/v1","plan":"evaluation","days":30,"extension_days":14,"grace_days":0,"authors":3,"devices_per_author":2,"runners":1,"capabilities":["author","execute","hub"]}`)
	if _, err := trial.DecodePolicy(raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []struct{ from, to string }{
		{`"grace_days":0,`, ``}, {`"grace_days":0`, `"grace_days":null`}, {`"grace_days":0`, `"grace_days":14`}, {`"days":30`, `"days":0`}, {`"devices_per_author":2`, `"devices_per_author":3`}, {`"capabilities":["author","execute","hub"]`, `"capabilities":["author"]`}, {`"days":30`, `"days":30,"extra":true`}, {`"days":30`, `"days":30,"days":30`},
	} {
		if _, err := trial.DecodePolicy(bytes.Replace(raw, []byte(bad.from), []byte(bad.to), 1)); err == nil {
			t.Fatalf("accepted %s", bad.to)
		}
	}
}

func TestScheduledIssuanceStartsAtChosenUTCAndCancellationReturnsNoDelivery(t *testing.T) {
	at := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	_, key, _ := ed25519.GenerateKey(nil)
	policy := trial.Policy{Schema: trial.PolicySchema, Plan: "evaluation", Days: 30, ExtensionDays: 14, GraceDays: 0, Authors: 3, DevicesPerAuthor: 2, Runners: 1, Capabilities: []string{"author", "execute", "hub"}}
	request := trial.Request{Organization: "org", ID: "scheduled", Sequence: 4, Mode: "scheduled", Starts: at.Add(48 * time.Hour), Authors: []entitlement.Assignment{{Author: "a", Devices: []string{"a"}}, {Author: "b", Devices: []string{"b"}}, {Author: "c", Devices: []string{"c"}}}, Authority: "runner"}
	account, data, err := trial.Issue(context.Background(), policy, request, "test", key, at)
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := entitlement.DecodeV2(data)
	if doc.Entitlement.NotBefore.Format(time.RFC3339) != "2026-09-22T00:00:00Z" || doc.Entitlement.Expires.Format(time.RFC3339) != "2026-10-22T00:00:00Z" {
		t.Fatal("scheduled date changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, data, err = trial.Issue(ctx, policy, request, "test", key, at); err == nil || data != nil {
		t.Fatal("cancelled issuance delivered")
	}
	if _, data, err = account.Extend(ctx, "extension", 5, true, "test", key, at); err == nil || data != nil {
		t.Fatal("cancelled extension delivered")
	}
	if _, data, err = account.Extend(context.Background(), "extension", 5, true, "test", key, time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)); err == nil || data != nil {
		t.Fatal("late extension created new term")
	}
	request.Mode = "activation"
	if _, _, err = trial.Issue(context.Background(), policy, request, "test", key, at); err == nil {
		t.Fatal("activation silently scheduled")
	}
	request.Mode = "scheduled"
	request.Authors = request.Authors[:2]
	if _, _, err = trial.Issue(context.Background(), policy, request, "test", key, at); err == nil {
		t.Fatal("trial missing third named author")
	}
}
