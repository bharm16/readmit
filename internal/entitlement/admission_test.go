package entitlement_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
)

func testGrantV2(t *testing.T) entitlement.GrantV2 {
	t.Helper()
	claims := testClaimsV2()
	claims.Runners = entitlement.Runners{Instances: 3, Authorities: []entitlement.Authority{
		{ID: "ci-pool-main", Instances: 2},
		{ID: "ci-pool-spare", Instances: 1},
	}}
	grant, err := entitlement.VerifyV2(signedV2(t, claims), testTrust(t, entitlement.KeyActive, time.Time{}))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return grant
}

func at(hour int) time.Time { return time.Date(2027, time.March, 1, hour, 0, 0, 0, time.UTC) }

// An authority admits up to the instances the entitlement grants it, and no
// more. Capacity is instances at once, not tests or messages: a released
// instance's capacity is free again for the next.
func TestRunnerAuthorityAdmitsUpToItsGrantedInstances(t *testing.T) {
	grant := testGrantV2(t)
	path := filepath.Join(t.TempDir(), "admissions.json")
	if _, err := entitlement.CreateAdmissions(path, grant, "ci-pool-other"); !errors.Is(err, entitlement.ErrAuthorityNotNamed) {
		t.Fatalf("a record was created for an authority the entitlement does not name: %v", err)
	}
	record, err := entitlement.CreateAdmissions(path, grant, "ci-pool-main")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := entitlement.CreateAdmissions(path, grant, "ci-pool-main"); err == nil {
		t.Fatal("a record overwrote an existing file")
	}
	held, err := record.Capacity(grant, at(9))
	if err != nil || held.Instances != 2 || held.Free() != 2 {
		t.Fatalf("unexpected capacity: %+v %v", held, err)
	}
	if err := record.Admit(grant, "run-001", at(9), at(10)); err != nil {
		t.Fatalf("first admission: %v", err)
	}
	if err := record.Admit(grant, "run-001", at(9), at(10)); !errors.Is(err, entitlement.ErrInstanceAdmitted) {
		t.Fatalf("an instance was admitted twice: %v", err)
	}
	if err := record.Admit(grant, "run-002", at(9), at(10)); err != nil {
		t.Fatalf("second admission: %v", err)
	}
	if err := record.Admit(grant, "run-003", at(9), at(10)); !errors.Is(err, entitlement.ErrCapacityExhausted) {
		t.Fatalf("a third instance was admitted against two: %v", err)
	}
	if err := record.Release("run-001", at(9).Add(30*time.Minute)); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := record.Release("run-001", at(9).Add(31*time.Minute)); !errors.Is(err, entitlement.ErrInstanceSettled) {
		t.Fatalf("an instance was released twice: %v", err)
	}
	if err := record.Release("run-999", at(9)); !errors.Is(err, entitlement.ErrInstanceUnknown) {
		t.Fatalf("an unadmitted instance was released: %v", err)
	}
	if err := record.Admit(grant, "run-003", at(9).Add(time.Hour/2), at(11)); err != nil {
		t.Fatalf("released capacity was not reused: %v", err)
	}
	held, err = record.Capacity(grant, at(9).Add(45*time.Minute))
	if err != nil || held.Active != 2 || held.Stale != 0 || held.Free() != 0 {
		t.Fatalf("unexpected capacity after release and readmission: %+v %v", held, err)
	}

	reopened, err := entitlement.OpenAdmissions(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(reopened.Record.Admissions) != 3 || reopened.Record.Admissions[0].StateAt(at(10)) != entitlement.AdmissionReleased {
		t.Fatalf("admissions were not retained: %+v", reopened.Record.Admissions)
	}
	// A record is bound to its organization and authority.
	other := testGrantV2(t)
	other.Claims.Organization = "other-hospital"
	if _, err := reopened.Capacity(other, at(9)); !errors.Is(err, entitlement.ErrDifferentOrganization) {
		t.Fatalf("another organization's entitlement was applied: %v", err)
	}
	if err := reopened.Admit(other, "run-004", at(9), at(10)); !errors.Is(err, entitlement.ErrDifferentOrganization) {
		t.Fatalf("another organization's entitlement admitted: %v", err)
	}
	dropped := testGrantV2(t)
	dropped.Claims.Runners.Authorities = dropped.Claims.Runners.Authorities[1:]
	if err := reopened.Admit(dropped, "run-004", at(9), at(10)); !errors.Is(err, entitlement.ErrAuthorityNotNamed) {
		t.Fatalf("a reissue that dropped this authority still admitted: %v", err)
	}
}

// An instance that stops without releasing leaves its admission stale once the
// lease ends. Stale capacity is held, not reused: the next admission is refused
// by name until an operator reconciles it or the instance reports in.
func TestInterruptedInstanceHoldsCapacityUntilReconciled(t *testing.T) {
	grant := testGrantV2(t)
	record, err := entitlement.CreateAdmissions(filepath.Join(t.TempDir(), "admissions.json"), grant, "ci-pool-spare")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := record.Admit(grant, "run-001", at(9), at(10)); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if err := record.Admit(grant, "run-002", at(9), at(10)); !errors.Is(err, entitlement.ErrCapacityExhausted) {
		t.Fatalf("capacity of one admitted two: %v", err)
	}
	// The lease ends and nothing was heard: the admission is stale, and still held.
	held, err := record.Capacity(grant, at(11))
	if err != nil || held.Active != 0 || held.Stale != 1 || held.Free() != 0 {
		t.Fatalf("a lapsed lease was not reported stale: %+v %v", held, err)
	}
	if err := record.Admit(grant, "run-002", at(11), at(12)); !errors.Is(err, entitlement.ErrCapacityUncertain) {
		t.Fatalf("stale capacity was reused: %v", err)
	}
	// The instance itself reporting in resolves the uncertainty.
	if err := record.Renew(grant, "run-001", at(11), at(13)); err != nil {
		t.Fatalf("a stale instance could not renew: %v", err)
	}
	if err := record.Renew(grant, "run-001", at(11), at(12)); err == nil {
		t.Fatal("a renewal shortened a lease")
	}
	if state := record.Record.Admissions[0].StateAt(at(12)); state != entitlement.AdmissionActive {
		t.Fatalf("a renewed admission is not active: %s", state)
	}
	if err := record.Admit(grant, "run-002", at(12), at(13)); !errors.Is(err, entitlement.ErrCapacityExhausted) {
		t.Fatalf("renewed capacity was reused: %v", err)
	}
	// It goes quiet again; this time an operator establishes it stopped and reconciles.
	if err := record.Reconcile("run-001", at(14)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := record.Reconcile("run-001", at(14)); !errors.Is(err, entitlement.ErrInstanceSettled) {
		t.Fatalf("an admission was reconciled twice: %v", err)
	}
	if err := record.Release("run-001", at(14)); !errors.Is(err, entitlement.ErrInstanceSettled) {
		t.Fatalf("a reconciled admission was released: %v", err)
	}
	if err := record.Renew(grant, "run-001", at(14), at(15)); !errors.Is(err, entitlement.ErrInstanceSettled) {
		t.Fatalf("a reconciled admission was renewed: %v", err)
	}
	if err := record.Admit(grant, "run-002", at(14), at(15)); err != nil {
		t.Fatalf("reconciled capacity was not free: %v", err)
	}
	// An operator may also reconcile an active admission they know is gone.
	if err := record.Reconcile("run-002", at(14).Add(time.Minute)); err != nil {
		t.Fatalf("an active admission could not be reconciled: %v", err)
	}
	held, err = record.Capacity(grant, at(14).Add(2*time.Minute))
	if err != nil || held.Free() != 1 {
		t.Fatalf("unexpected capacity after reconciliation: %+v %v", held, err)
	}
}

// Admission is a new operation and is refused outside the term; an admission
// made inside it is still renewed, released and reconciled afterwards, so
// already-started bounded work finishes and its capacity is settled honestly.
func TestAdmissionFollowsTheTermAndSettlementDoesNot(t *testing.T) {
	grant := testGrantV2(t)
	record, err := entitlement.CreateAdmissions(filepath.Join(t.TempDir(), "admissions.json"), grant, "ci-pool-main")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	early := moment(2026, time.September, 17)
	if err := record.Admit(grant, "run-000", early, early.Add(time.Hour)); !errors.Is(err, entitlement.ErrNotYetValid) {
		t.Fatalf("an instance was admitted before the term: %v", err)
	}
	grace := moment(2027, time.September, 20)
	if err := record.Admit(grant, "run-001", grace, grace.Add(time.Hour)); err != nil {
		t.Fatalf("an instance was refused inside grace: %v", err)
	}
	expired := moment(2027, time.October, 3)
	if err := record.Admit(grant, "run-002", expired, expired.Add(time.Hour)); !errors.Is(err, entitlement.ErrExpired) {
		t.Fatalf("an instance was admitted after expiry: %v", err)
	}
	if err := record.Renew(grant, "run-001", grace.Add(time.Hour), grace.Add(2*time.Hour)); err != nil {
		t.Fatalf("started work could not renew inside grace: %v", err)
	}
	if err := record.Renew(grant, "run-001", expired, expired.Add(time.Hour)); !errors.Is(err, entitlement.ErrExpired) {
		t.Fatalf("a lease was extended after the grace window closed: %v", err)
	}
	if err := record.Release("run-001", expired.Add(time.Hour)); err != nil {
		t.Fatalf("started work could not release after expiry: %v", err)
	}
	if err := record.Reconcile("run-001", expired.Add(time.Hour)); !errors.Is(err, entitlement.ErrInstanceSettled) {
		t.Fatalf("a released admission was reconciled: %v", err)
	}
	for _, c := range []struct {
		name  string
		at    time.Time
		until time.Time
	}{
		{"lease ending before it begins", at(9), at(8)},
		{"lease past the bound", at(9), at(9).Add(entitlement.MaxLease + time.Second)},
		{"sub-second lease end", at(9), at(10).Add(time.Millisecond)},
	} {
		if err := record.Admit(grant, "run-003", c.at, c.until); err == nil {
			t.Errorf("%s was admitted", c.name)
		}
	}
	if err := record.Admit(grant, "run/003", at(9), at(10)); err == nil {
		t.Error("an instance identifier with a separator was admitted")
	}
	if err := record.Admit(grant, "run-003", at(9), at(9).Add(entitlement.MaxLease+time.Second)); !errors.Is(err, entitlement.ErrLeaseTooLong) {
		t.Errorf("a lease past the bound was not refused by name: %v", err)
	}
}

// Two processes admitting against the last free instance at the same moment
// cannot both take it. Each handle decides against the file under its own
// exclusive update, so at most one admission is recorded, and the loser is
// told either that the capacity is taken or that another update was in
// progress, never that it was admitted.
func TestConcurrentAdmissionsNeverExceedCapacity(t *testing.T) {
	grant := testGrantV2(t)
	path := filepath.Join(t.TempDir(), "admissions.json")
	if _, err := entitlement.CreateAdmissions(path, grant, "ci-pool-spare"); err != nil {
		t.Fatalf("create: %v", err)
	}
	const writers = 8
	results := make(chan error, writers)
	for i := range writers {
		go func() {
			record, err := entitlement.OpenAdmissions(path)
			if err != nil {
				results <- err
				return
			}
			results <- record.Admit(grant, "run-"+string(rune('a'+i)), at(9), at(10))
		}()
	}
	admitted := 0
	for range writers {
		switch err := <-results; {
		case err == nil:
			admitted++
		case errors.Is(err, entitlement.ErrCapacityExhausted), errors.Is(err, entitlement.ErrAdmissionUpdate):
		default:
			t.Errorf("unexpected refusal: %v", err)
		}
	}
	if admitted != 1 {
		t.Fatalf("%d writers were admitted against one instance", admitted)
	}
	final, err := entitlement.OpenAdmissions(path)
	if err != nil || len(final.Record.Admissions) != 1 {
		t.Fatalf("the record holds %d admissions: %v", len(final.Record.Admissions), err)
	}
	if _, err := os.Stat(path + ".incomplete"); !os.IsNotExist(err) {
		t.Fatal("a refused update left its replacement behind")
	}
}

// Every decision is made against the record on disk under an exclusive
// update: a second writer that reads the same free slot is refused rather than
// both admitting, and an interrupted update is retained and reported.
func TestAdmissionUpdatesAreExclusiveAndInterruptionsAreRetained(t *testing.T) {
	grant := testGrantV2(t)
	path := filepath.Join(t.TempDir(), "admissions.json")
	first, err := entitlement.CreateAdmissions(path, grant, "ci-pool-spare")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	second, err := entitlement.OpenAdmissions(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Both handles saw one free instance; only the first write takes it.
	if err := first.Admit(grant, "run-001", at(9), at(10)); err != nil {
		t.Fatalf("first admission: %v", err)
	}
	if err := second.Admit(grant, "run-002", at(9), at(10)); !errors.Is(err, entitlement.ErrCapacityExhausted) {
		t.Fatalf("a stale handle admitted over a taken slot: %v", err)
	}
	if len(second.Record.Admissions) != 1 || second.Record.Admissions[0].Instance != "run-001" {
		t.Fatalf("a refused handle does not hold the record that refused it: %+v", second.Record.Admissions)
	}
	// A refused decision leaves the file exactly as it was.
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Admit(grant, "run-003", at(9), at(10)); !errors.Is(err, entitlement.ErrCapacityExhausted) {
		t.Fatalf("unexpected admission: %v", err)
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != string(before) {
		t.Fatal("a refused admission rewrote the record")
	}
	if _, err := os.Stat(path + ".incomplete"); !os.IsNotExist(err) {
		t.Fatal("a refused update left its replacement behind")
	}
	// An interrupted update is retained and every later update is refused.
	if err := os.WriteFile(path+".incomplete", []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := first.Release("run-001", at(10)); !errors.Is(err, entitlement.ErrAdmissionUpdate) {
		t.Fatalf("an interrupted update was overwritten: %v", err)
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != string(before) {
		t.Fatal("a refused update rewrote the record")
	}
	if reopened, err := entitlement.OpenAdmissions(path); err != nil || len(reopened.Record.Admissions) != 1 {
		t.Fatalf("the intact record stopped opening: %v", err)
	}
	if err := os.Remove(path + ".incomplete"); err != nil {
		t.Fatal(err)
	}
	if err := first.Release("run-001", at(10)); err != nil {
		t.Fatalf("release after the retained file was moved aside: %v", err)
	}
	// A record whose identity changed underneath a handle is refused.
	swapped := strings.Replace(string(before), `"authority":"ci-pool-spare"`, `"authority":"ci-pool-main"`, 1)
	if err := os.WriteFile(path, []byte(swapped), 0600); err != nil {
		t.Fatal(err)
	}
	if err := first.Admit(grant, "run-004", at(11), at(12)); err == nil {
		t.Fatal("a handle updated a record that no longer names its authority")
	}
}

// The record is strict JSON: unknown members, an unknown version, a duplicate
// instance and an admission both released and reconciled are each refused.
func TestAdmissionRecordIsStrict(t *testing.T) {
	grant := testGrantV2(t)
	path := filepath.Join(t.TempDir(), "admissions.json")
	record, err := entitlement.CreateAdmissions(path, grant, "ci-pool-main")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := record.Admit(grant, "run-001", at(9), at(10)); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if err := record.Release("run-001", at(10)); err != nil {
		t.Fatalf("release: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entitlement.OpenAdmissions(path); err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, c := range []struct {
		name    string
		altered string
	}{
		{"unknown top-level member", strings.Replace(string(data), `"authority":`, `"host":"x","authority":`, 1)},
		{"unknown admission member", strings.Replace(string(data), `"instance":"run-001"`, `"instance":"run-001","pid":4`, 1)},
		{"unsupported version", strings.Replace(string(data), "readmit-runner-admission/v1", "readmit-runner-admission/v2", 1)},
		{"released and reconciled", strings.Replace(string(data), `"released":"2027-03-01T10:00:00Z"`, `"released":"2027-03-01T10:00:00Z","reconciled":"2027-03-01T10:00:00Z"`, 1)},
		{"settled before admitted", strings.Replace(string(data), `"released":"2027-03-01T10:00:00Z"`, `"released":"2027-03-01T08:00:00Z"`, 1)},
		{"lease ending before admission", strings.Replace(string(data), `"lease_until":"2027-03-01T10:00:00Z"`, `"lease_until":"2027-03-01T09:00:00Z"`, 1)},
		{"duplicate instance", strings.Replace(string(data), `"admissions":[`, `"admissions":[{"instance":"run-001","admitted":"2027-03-01T09:00:00Z","lease_until":"2027-03-01T10:00:00Z"},`, 1)},
		{"missing admissions", `{"schema":"readmit-runner-admission/v1","organization":"example-hospital","authority":"ci-pool-main"}` + "\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.altered == string(data) {
				t.Fatal("the record was not altered")
			}
			altered := filepath.Join(t.TempDir(), "admissions.json")
			if err := os.WriteFile(altered, []byte(c.altered), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := entitlement.OpenAdmissions(altered); err == nil {
				t.Fatal("an invalid record was accepted")
			}
		})
	}
	for _, member := range []string{"case", "endpoint", "patient", "hash", "host", "path", "message", "serial", "mac", "pid"} {
		if strings.Contains(string(data), `"`+member+`"`) {
			t.Errorf("an admission record names %q", member)
		}
	}
}

// DecodeAdmissions must never panic, and whatever it accepts must re-encode to
// bytes it accepts again unchanged, because that is what an update writes.
func FuzzRunnerAdmission(f *testing.F) {
	f.Add([]byte(`{"schema":"readmit-runner-admission/v1","organization":"example-hospital","authority":"ci-pool-main","admissions":[]}` + "\n"))
	f.Add([]byte(`{"schema":"readmit-runner-admission/v1","organization":"example-hospital","authority":"ci-pool-main","admissions":[{"instance":"run-001","admitted":"2027-03-01T09:00:00Z","lease_until":"2027-03-01T10:00:00Z","released":"2027-03-01T09:30:00Z"}]}` + "\n"))
	f.Add([]byte(`{"schema":"readmit-runner-admission/v1"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := entitlement.DecodeAdmissions(data)
		if err != nil {
			return
		}
		encoded, err := entitlement.EncodeAdmissions(decoded)
		if err != nil {
			t.Fatalf("an accepted record could not be encoded: %v", err)
		}
		again, err := entitlement.DecodeAdmissions(encoded)
		if err != nil {
			t.Fatalf("an encoded record was not accepted: %v", err)
		}
		second, err := entitlement.EncodeAdmissions(again)
		if err != nil || string(second) != string(encoded) {
			t.Fatalf("encoding is not stable: %v", err)
		}
	})
}
