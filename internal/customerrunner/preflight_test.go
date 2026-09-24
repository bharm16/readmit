package customerrunner_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/operationguard"
)

// The preflight answers what a job would run as, and names the first rule
// that refuses it, without asking the hub or admitting anything: every
// refusal reads as the runner's one refusal sentence, which is all the
// command line prints.
func TestInspectNamesTheRuleThatRefusesAJob(t *testing.T) {
	c, _ := localConfig(t)
	job := func(id, spec string) customerrunner.Job {
		return customerrunner.Job{Schema: "readmit-runner-job/v1", ID: id, Spec: spec}
	}
	lab := jobSpec(t, "127.0.0.1:9", "lab")
	preflight, err := customerrunner.Inspect(c, job("nightly-001", lab))
	if err != nil || len(preflight.InputIdentity) != 64 || preflight.Environment != "lab" {
		t.Fatalf("a job that would run: %+v %v", preflight, err)
	}
	stage, err := customerrunner.Inspect(c, job("nightly-001", jobSpec(t, "127.0.0.1:9", "stage")))
	if !errors.Is(err, customerrunner.ErrOtherEnvironment) || stage.Environment != "stage" || len(stage.InputIdentity) != 64 {
		t.Fatalf("another environment: %+v %v", stage, err)
	}
	unbound, err := customerrunner.Inspect(c, job("nightly-001", jobSpec(t, "127.0.0.1:9", "")))
	if !errors.Is(err, customerrunner.ErrUnbound) || unbound.Environment != "" || len(unbound.InputIdentity) != 64 {
		t.Fatalf("no environment: %+v %v", unbound, err)
	}
	if _, err := customerrunner.Inspect(c, job("nightly-001", filepath.Join(t.TempDir(), "absent.json"))); !errors.Is(err, customerrunner.ErrUnprepared) {
		t.Fatalf("an absent spec: %v", err)
	}
	if _, err := customerrunner.Inspect(c, job("../nightly-001", lab)); !errors.Is(err, customerrunner.ErrRefused) {
		t.Fatalf("a path as a job id: %v", err)
	}
	os.Mkdir(filepath.Join(c.Root, "nightly-001"), 0700)
	if _, err := customerrunner.Inspect(c, job("nightly-001", lab)); !errors.Is(err, customerrunner.ErrRetained) {
		t.Fatalf("a retained id: %v", err)
	}
	// A root this machine cannot read decides nothing about retention.
	elsewhere := c
	elsewhere.Root = filepath.Join(t.TempDir(), "on-the-runner-host")
	if _, err := customerrunner.Inspect(elsewhere, job("nightly-001", lab)); err != nil {
		t.Fatalf("a root on another host: %v", err)
	}
	for _, refusal := range []error{customerrunner.ErrUnprepared, customerrunner.ErrNoIdentity, customerrunner.ErrChanged,
		customerrunner.ErrOtherEnvironment, customerrunner.ErrUnbound, customerrunner.ErrRetained} {
		if !errors.Is(refusal, customerrunner.ErrRefused) || refusal.Error() != customerrunner.ErrRefused.Error() {
			t.Fatalf("%#v does not read as the runner's refusal", refusal)
		}
	}
}

// A pinned job's inputs are compared with its pin before anything else: the
// refusal arrives without an admission in the context, so before admission is
// asked, and without the hub hearing of it. Inputs that match go on to
// admission, which refuses a context that carries none, still before the hub.
func TestRunPinnedComparesItsPinBeforeAdmission(t *testing.T) {
	c, _ := localConfig(t)
	clock := newFakeClock()
	hub := newMemoryHub(clock)
	lab := jobSpec(t, "127.0.0.1:9", "lab")
	pin, err := customerrunner.Inspect(c, customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "pinned", Spec: lab})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		spec string
		pin  string
		want error
	}{
		{"an absent spec", filepath.Join(t.TempDir(), "absent.json"), pin.InputIdentity, customerrunner.ErrUnprepared},
		{"changed inputs", jobSpec(t, "127.0.0.2:9", "lab"), pin.InputIdentity, customerrunner.ErrChanged},
		{"an empty pin", lab, "", customerrunner.ErrChanged},
		{"the pinned inputs", lab, pin.InputIdentity, operationguard.ErrUnavailable},
	} {
		job := customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "pinned", Spec: tc.spec}
		_, err := customerrunner.RunPinnedWithForTest(context.Background(), c, job, tc.pin, hub, clock)
		var declined *operationguard.Declined
		if !errors.Is(err, tc.want) || errors.As(err, &declined) != (tc.want == operationguard.ErrUnavailable) {
			t.Fatalf("%s: %v; want %v", tc.name, err, tc.want)
		}
		hub.quiet(t)
	}
}

// Every other rule refuses an admitted job once it holds the root and its
// lease, before it reserves the job's id or sends anything, and the lease is
// released.
func TestRunRefusesAnAdmittedJobThePreflightRefuses(t *testing.T) {
	c, _ := localConfig(t)
	clock := newFakeClock()
	hub := newMemoryHub(clock)
	os.Mkdir(filepath.Join(c.Root, "retained"), 0700)
	for _, tc := range []struct {
		id   string
		spec string
		want error
	}{
		{"absent", filepath.Join(t.TempDir(), "absent.json"), customerrunner.ErrUnprepared},
		{"stage", jobSpec(t, "127.0.0.1:9", "stage"), customerrunner.ErrOtherEnvironment},
		{"unbound", jobSpec(t, "127.0.0.1:9", ""), customerrunner.ErrUnbound},
		{"retained", jobSpec(t, "127.0.0.1:9", "lab"), customerrunner.ErrRetained},
	} {
		job := customerrunner.Job{Schema: "readmit-runner-job/v1", ID: tc.id, Spec: tc.spec}
		if run := admitted(t, c, job, hub, clock); !errors.Is(run.err, tc.want) || run.summary.Schema != "" {
			t.Fatalf("%s: %+v %v; want %v", tc.id, run.summary, run.err, tc.want)
		}
		hub.expect(t, "claim "+tc.id, "release "+tc.id)
		hub.quiet(t)
	}
	if jobs, err := customerrunner.Jobs(c.Root); err != nil || !slices.Equal(jobs, []string{"retained"}) {
		t.Fatalf("a refused job reserved its id: %v %v", jobs, err)
	}
}

// The root's retained jobs are read by their ids alone: the claim a running
// process holds and anything that is not a job id are not jobs, and a job's
// recovery is read from where the runner retains it.
func TestJobsAndRecoverReadRetainedJobsById(t *testing.T) {
	c, _ := localConfig(t)
	for _, dir := range []string{"nightly-002", "nightly-001", ".active", "Not-An-Id"} {
		os.Mkdir(filepath.Join(c.Root, dir), 0700)
	}
	os.WriteFile(filepath.Join(c.Root, "nightly-003"), nil, 0600)
	jobs, err := customerrunner.Jobs(c.Root)
	if err != nil || !slices.Equal(jobs, []string{"nightly-001", "nightly-002"}) {
		t.Fatalf("jobs: %v %v", jobs, err)
	}
	if got := customerrunner.RunPath(c.Root, "nightly-001"); got != filepath.Join(c.Root, "nightly-001", "run") {
		t.Fatalf("run path %s", got)
	}
	// A job whose run never began has no recovery to read, and a name that is
	// not a job id is refused before anything is opened.
	if _, err := customerrunner.Recover(c.Root, "nightly-001"); err == nil {
		t.Fatal("a job without a run recovered")
	}
	if _, err := customerrunner.Recover(c.Root, "../nightly-001"); !errors.Is(err, customerrunner.ErrRefused) {
		t.Fatalf("a path read as a job id: %v", err)
	}
	if _, err := customerrunner.Jobs(filepath.Join(c.Root, "nightly-003")); !errors.Is(err, customerrunner.ErrRefused) {
		t.Fatalf("a root that is not a directory: %v", err)
	}
}
