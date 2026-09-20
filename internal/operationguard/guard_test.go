package operationguard_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/operationguard"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestUnconfiguredOperationRefusesWithoutStartingTrial(t *testing.T) {
	_, err := operationguard.New("").Admit("author")
	if !errors.Is(err, operationguard.ErrUnavailable) {
		t.Fatalf("unconfigured admission: %v", err)
	}
}

func TestActiveAdmissionRecordsHighWaterAndRefusesExpiry(t *testing.T) {
	fixture := activated(t)
	guard := operationguard.NewWithClock(fixture.policy, fixture.clock)
	release, err := guard.Admit("author")
	if err != nil {
		t.Fatal(err)
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
	fixture.wall = fixture.wall.Add(31 * 24 * time.Hour)
	if _, err = guard.Admit("author"); !errors.Is(err, entitlement.ErrExpired) {
		t.Fatalf("expiry: %v", err)
	}
	state, err := operationguard.Read(fixture.policy)
	if err != nil || !state.HighWater.Equal(fixture.wall) {
		t.Fatalf("high-water: %+v %v", state, err)
	}
}

type fixture struct {
	policy  string
	wall    time.Time
	elapsed time.Duration
	key     ed25519.PrivateKey
	claims  entitlement.ClaimsV2
}

func (f *fixture) clock() operationguard.Instant {
	return operationguard.Instant{UTC: f.wall, Elapsed: f.elapsed}
}
func activated(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	at := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	trust := entitlement.Trust{Schema: entitlement.TrustSchema, Keys: []entitlement.Key{{ID: "test", Algorithm: "ed25519", PublicKey: base64.StdEncoding.EncodeToString(pub), Status: entitlement.KeyActive}}}
	claims := entitlement.ClaimsV2{ID: "trial", Organization: "org", Plan: "trial", Sequence: 1, Issued: at, NotBefore: at, Expires: at.Add(30 * 24 * time.Hour), GraceDays: 0, Authors: entitlement.Authors{Seats: 3, DevicesPerSeat: 2, Assignments: []entitlement.Assignment{{Author: "alice", Devices: []string{"device"}}}}, Runners: entitlement.Runners{Instances: 1, Authorities: []entitlement.Authority{{ID: "local", Instances: 1}}}, Capabilities: []string{"author", "execute", "hub"}}
	doc, err := entitlement.SignV2(claims, "test", key)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := entitlement.EncodeV2(doc)
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := entitlement.EncodeTrust(trust)
	if err != nil {
		t.Fatal(err)
	}
	p := operationguard.Policy{Schema: operationguard.PolicySchema, Entitlement: filepath.Join(root, "entitlement.json"), Trust: filepath.Join(root, "trust.json"), State: filepath.Join(root, "clock.json"), Author: "alice", Device: "device", Authority: "local", Admissions: filepath.Join(root, "admissions.json")}
	data, err := operationguard.EncodePolicy(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "policy.json")
	for name, bytes := range map[string][]byte{path: data, p.Entitlement: signed, p.Trust: trusted} {
		if err := os.WriteFile(name, bytes, 0600); err != nil {
			t.Fatal(err)
		}
	}
	f := &fixture{policy: path, wall: at, key: key, claims: claims}
	if err := operationguard.ActivateWithClock(path, f.clock); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRollbackLatchRequiresExplicitResolutionAndNeverReducesTime(t *testing.T) {
	f := activated(t)
	g := operationguard.NewWithClock(f.policy, f.clock)
	f.wall = f.wall.Add(-5 * time.Minute)
	release, err := g.Admit("author")
	if err != nil {
		t.Fatal(err)
	}
	release()
	before, _ := operationguard.Read(f.policy)
	f.wall = f.wall.Add(-time.Second)
	if _, err = g.Admit("author"); !errors.Is(err, operationguard.ErrRollback) {
		t.Fatalf("rollback: %v", err)
	}
	f.wall = before.HighWater
	if _, err = g.Admit("author"); !errors.Is(err, operationguard.ErrRollback) {
		t.Fatal("rollback latch cleared implicitly", err)
	}
	f.wall = f.wall.Add(-time.Second)
	if err = operationguard.ResolveWithClock(f.policy, f.clock); !errors.Is(err, operationguard.ErrRollback) {
		t.Fatal("resolved backward clock", err)
	}
	f.wall = before.HighWater
	if err = operationguard.ResolveWithClock(f.policy, f.clock); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Admit("author"); err != nil {
		t.Fatal(err)
	}
	after, _ := operationguard.Read(f.policy)
	if after.HighWater.Before(before.HighWater) {
		t.Fatal("highwater decreased")
	}
}

func TestMonotonicSubsecondsAccumulateWithoutWallProgress(t *testing.T) {
	f := activated(t)
	g := operationguard.NewWithClock(f.policy, f.clock)
	for i := 1; i <= 20; i++ {
		f.elapsed = time.Duration(i) * 100 * time.Millisecond
		if _, err := g.Admit("author"); err != nil {
			t.Fatal(err)
		}
	}
	state, err := operationguard.Read(f.policy)
	if err != nil || !state.HighWater.Equal(f.wall.Add(2*time.Second)) {
		t.Fatalf("elapsed time lost: %+v %v", state, err)
	}
}

func TestRunnerCapacityAndReleaseAfterExpiry(t *testing.T) {
	f := activated(t)
	g := operationguard.NewWithClock(f.policy, f.clock)
	release, err := g.Admit("execute")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = operationguard.NewWithClock(f.policy, f.clock).Admit("execute"); !errors.Is(err, entitlement.ErrCapacityExhausted) {
		t.Fatalf("second job: %v", err)
	}
	f.wall = f.wall.Add(31 * 24 * time.Hour)
	if err = release(); err != nil {
		t.Fatal(err)
	}
	if err = release(); err != nil {
		t.Fatal("release not idempotent", err)
	}
	if _, err = g.Admit("execute"); !errors.Is(err, entitlement.ErrExpired) {
		t.Fatalf("new expired job: %v", err)
	}
}

func TestMissingCorruptReleasedAndInterruptedStateFailClosed(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt", "released", "interrupted"} {
		t.Run(mode, func(t *testing.T) {
			f := activated(t)
			data, _ := os.ReadFile(f.policy)
			p, _ := operationguard.DecodePolicy(data)
			switch mode {
			case "missing":
				os.Remove(p.State)
			case "corrupt":
				os.WriteFile(p.State, []byte(`{"schema":"readmit-operation-clock/v1"}`), 0600)
			case "released":
				if err := operationguard.Release(f.policy); err != nil {
					t.Fatal(err)
				}
			case "interrupted":
				os.WriteFile(p.State+".incomplete", []byte("retained"), 0600)
			}
			if _, err := operationguard.NewWithClock(f.policy, f.clock).Admit("author"); err == nil {
				t.Fatal("admitted invalid state")
			}
			if mode != "missing" {
				if err := operationguard.ActivateWithClock(f.policy, f.clock); err == nil {
					t.Fatal("activation repaired existing state")
				}
			}
		})
	}
}

func TestHubAuthorCannotSubstituteConfiguredIdentity(t *testing.T) {
	f := activated(t)
	g := operationguard.NewWithClock(f.policy, f.clock)
	for _, pair := range [][2]string{{"bob", "device"}, {"alice", "other"}, {"", ""}} {
		if _, err := g.AdmitAuthor(pair[0], pair[1]); err == nil {
			t.Fatal("unassigned author admitted")
		}
	}
	if _, err := g.AdmitAuthor("alice", "device"); err != nil {
		t.Fatal(err)
	}
}

func TestIndependentGuardsNeverOversubscribeOneAuthority(t *testing.T) {
	f := activated(t)
	var wg sync.WaitGroup
	accepted := make(chan func() error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := operationguard.NewWithClock(f.policy, f.clock).Admit("execute")
			if err == nil {
				accepted <- release
			}
		}()
	}
	wg.Wait()
	close(accepted)
	count := 0
	for release := range accepted {
		count++
		if err := release(); err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatalf("admitted %d concurrent jobs for one signed instance", count)
	}
}

func TestPolicyStrictShapeAndSymlinkRefusal(t *testing.T) {
	f := activated(t)
	data, _ := os.ReadFile(f.policy)
	for _, changed := range [][]byte{
		bytes.Replace(data, []byte(`"author":"alice"`), []byte(`"author":null`), 1),
		bytes.Replace(data, []byte(`"author":"alice",`), nil, 1),
		bytes.Replace(data, []byte(`"schema":"readmit-operation-policy/v1"`), []byte(`"schema":"readmit-operation-policy/v2"`), 1),
		bytes.Replace(data, []byte(`"author":"alice"`), []byte(`"author":"alice","other":true`), 1),
	} {
		if _, err := operationguard.DecodePolicy(changed); err == nil {
			t.Fatalf("accepted malformed policy %s", changed)
		}
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(f.policy, alias); err != nil {
		t.Skip("symlink not available")
	}
	if _, err := operationguard.NewWithClock(alias, f.clock).Admit("author"); err == nil {
		t.Fatal("symbolic policy accepted")
	}
}

func (f *fixture) replaceGrant(t *testing.T) {
	t.Helper()
	doc, err := entitlement.SignV2(f.claims, "test", f.key)
	if err != nil {
		t.Fatal(err)
	}
	data, err := entitlement.EncodeV2(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(f.policy), "entitlement.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestPaidFourteenDayGraceAndSupersededSequence(t *testing.T) {
	f := activated(t)
	f.claims.GraceDays = 14
	f.claims.Sequence = 2
	f.replaceGrant(t)
	g := operationguard.NewWithClock(f.policy, f.clock)
	f.wall = f.claims.Expires
	if _, err := g.Admit("author"); err != nil {
		t.Fatal("paid grace refused", err)
	}
	f.wall = f.claims.Expires.Add(14*24*time.Hour - time.Second)
	if _, err := g.Admit("author"); err != nil {
		t.Fatal("last grace second refused", err)
	}
	f.wall = f.wall.Add(time.Second)
	if _, err := g.Admit("author"); !errors.Is(err, entitlement.ErrExpired) {
		t.Fatal("paid grace extended", err)
	}
	f.claims.Sequence = 1
	f.replaceGrant(t)
	if _, err := g.Admit("author"); !errors.Is(err, entitlement.ErrSuperseded) {
		t.Fatal("stale signed issue admitted", err)
	}
}

func TestEightJobsShareOneInstanceButEveryJobRechecksTerm(t *testing.T) {
	f := activated(t)
	g := operationguard.NewWithClock(f.policy, f.clock)
	release, err := g.Admit("execute")
	if err != nil {
		t.Fatal(err)
	}
	for range 8 {
		if err = g.CheckExecution(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = operationguard.NewWithClock(f.policy, f.clock).Admit("execute"); !errors.Is(err, entitlement.ErrCapacityExhausted) {
		t.Fatal("second instance admitted", err)
	}
	f.wall = f.claims.Expires
	if err = g.CheckExecution(); !errors.Is(err, entitlement.ErrExpired) {
		t.Fatal("new queued job admitted after expiry", err)
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
}

func TestExactlyFiveMinuteCorrectionWithFractionalElapsedIsTolerated(t *testing.T) {
	f := activated(t)
	g := operationguard.NewWithClock(f.policy, f.clock)
	f.elapsed = 100 * time.Millisecond
	f.wall = f.wall.Add(-5 * time.Minute)
	if _, err := g.Admit("author"); err != nil {
		t.Fatalf("exact tolerated correction: %v", err)
	}
	f.wall = f.wall.Add(-time.Second)
	if _, err := g.Admit("author"); !errors.Is(err, operationguard.ErrRollback) {
		t.Fatalf("larger correction accepted: %v", err)
	}
}

func TestCompletedExecutionSettlesAfterAnotherShortRecordUpdate(t *testing.T) {
	f := activated(t)
	g := operationguard.NewWithClock(f.policy, f.clock)
	release, err := g.Admit("execute")
	if err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(filepath.Dir(f.policy), "admissions.json.incomplete")
	if err = os.WriteFile(lock, []byte("another update"), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); time.Sleep(30 * time.Millisecond); os.Remove(lock) }()
	err = release()
	<-done
	if err != nil {
		t.Fatalf("ordinary settlement contention became a failed operation: %v", err)
	}
	if _, err = operationguard.NewWithClock(f.policy, f.clock).Admit("execute"); err != nil {
		t.Fatal("settled capacity not reusable", err)
	}
}

func TestAdmissionCancellationRetainsBusyStateWithoutAdmittingWork(t *testing.T) {
	f := activated(t)
	lock := filepath.Join(filepath.Dir(f.policy), "clock.json.incomplete")
	if err := os.WriteFile(lock, []byte("retained update"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := operationguard.NewWithClock(f.policy, f.clock).AdmitContext(ctx, "execute"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled metadata admission: %v", err)
	}
	if bytes, err := os.ReadFile(lock); err != nil || string(bytes) != "retained update" {
		t.Fatal("cancellation removed retained lock")
	}
	admissions, err := entitlement.OpenAdmissions(filepath.Join(filepath.Dir(f.policy), "admissions.json"))
	if err != nil || len(admissions.Record.Admissions) != 0 {
		t.Fatal("cancelled request admitted work", err)
	}
}
