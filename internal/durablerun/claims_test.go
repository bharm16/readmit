package durablerun_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/durablerun"
)

func named(t *testing.T, spec, environment string) {
	t.Helper()
	path := filepath.Join(filepath.Dir(spec), "target.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var target map[string]any
	if err = json.Unmarshal(raw, &target); err != nil {
		t.Fatal(err)
	}
	target["schema"] = "readmit-target/v3"
	target["name"] = environment
	target["classification"] = "nonproduction"
	raw, _ = json.Marshal(target)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// A scheduler admits against what a run will declare, so what Prepare reports
// and what the run writes into its lease must be the same two resources.
func TestPreparedResourcesAreTheOnesTheRunLeases(t *testing.T) {
	address, _ := peer(t, "AA")
	spec, out := setup(t, address)
	named(t, spec, "staging")
	prepared, err := durablerun.Prepare(spec)
	if err != nil {
		t.Fatal(err)
	}
	declared := prepared.Resources()
	want := []durablerun.Resource{{Kind: durablerun.EnvironmentResource, Name: "staging"}, {Kind: durablerun.EndpointResource, Name: address}}
	if len(declared) != 2 || declared[0] != want[0] || declared[1] != want[1] {
		t.Fatalf("%+v", declared)
	}
	if _, err := prepared.Start(t.Context(), out); err != nil {
		t.Fatal(err)
	}
	// The run released its lease, so the runs directory claims nothing.
	claims, err := durablerun.Claims(filepath.Dir(out), nil)
	if err != nil || len(claims) != 0 {
		t.Fatalf("%+v %v", claims, err)
	}
}

// A lease a stopped writer could not release keeps claiming its resources,
// because refusing work is the conservative reading and joining a holder is
// not. Naming the job excludes it: a scheduler that already owns a run's
// admission never reads a lease that run may be writing at this moment.
func TestClaimsReportWhatEveryUnreleasedLeaseStillHolds(t *testing.T) {
	runs := t.TempDir()
	resources := []durablerun.Resource{{Kind: durablerun.EnvironmentResource, Name: "staging"}, {Kind: durablerun.EndpointResource, Name: "127.0.0.1:2575"}}
	write := func(job string, document any) {
		t.Helper()
		if err := os.Mkdir(filepath.Join(runs, job), 0700); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(document, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runs, job, "lease.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("holder", durablerun.Lease{Schema: durablerun.LeaseSchema, Holder: durablerun.Holder{PID: 1, StartedAt: time.Now().UTC()}, Resources: resources})
	if err := os.Mkdir(filepath.Join(runs, "released"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runs, "notes.txt"), []byte("not a run"), 0600); err != nil {
		t.Fatal(err)
	}
	claims, err := durablerun.Claims(runs, nil)
	if err != nil || len(claims) != 1 || claims[0].Job != "holder" || len(claims[0].Resources) != 2 || claims[0].Resources[0] != resources[0] {
		t.Fatalf("%+v %v", claims, err)
	}
	excluded, err := durablerun.Claims(runs, map[string]bool{"holder": true})
	if err != nil || len(excluded) != 0 {
		t.Fatalf("%+v %v", excluded, err)
	}
	// A lease that cannot be read is changed evidence, never an empty claim.
	if err := os.WriteFile(filepath.Join(runs, "holder", "lease.json"), []byte(`{"schema":"readmit-run-lease/v1","extra":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := durablerun.Claims(runs, nil); err == nil || err.Error() != "durable lease is invalid" {
		t.Fatalf("%v", err)
	}
	// A resource kind this release never writes is not a statement about what
	// a run is using, so the document it is in is refused rather than read.
	write("foreign", durablerun.Lease{Schema: durablerun.LeaseSchema, Holder: durablerun.Holder{PID: 1, StartedAt: time.Now().UTC()}, Resources: []durablerun.Resource{{Kind: "database", Name: "ledger"}}})
	if _, err := durablerun.Claims(runs, map[string]bool{"holder": true}); err == nil || err.Error() != "a durable lease names a resource this release does not read" {
		t.Fatalf("%v", err)
	}
	// Only admission is that strict. The lease state run status reports for
	// the same directory is unchanged.
	if _, err := durablerun.Open(filepath.Join(runs, "foreign")); err == nil || err.Error() == "durable lease is invalid" {
		t.Fatalf("reading a job changed with the scheduler's stricter rule: %v", err)
	}
	if _, err := durablerun.Claims(filepath.Join(runs, "absent"), nil); err == nil {
		t.Fatal("an unreadable runs directory was read as claiming nothing")
	}
}
