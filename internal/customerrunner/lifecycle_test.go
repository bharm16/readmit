package customerrunner_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

func localConfig(t *testing.T) (customerrunner.Config, string) {
	t.Helper()
	root := t.TempDir()
	os.Chmod(root, 0700)
	exe, _ := os.Executable()
	c := customerrunner.Config{Schema: "readmit-runner/v1", Hub: "https://127.0.0.1:1", Project: "alpha", Environment: "lab", Root: root, CA: filepath.Join(root, "ca.pem"), Certificate: filepath.Join(root, "client.pem"), Key: customerrunner.Reference{Command: exe, Arguments: []string{}}, Token: customerrunner.Reference{Command: exe, Arguments: []string{}}, UpdateKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), UpdateEngine: "next"}
	raw, _ := json.Marshal(c)
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, raw, 0600)
	return c, path
}
func TestOfflineCLIStatusAndInterruptedClaimNeverResend(t *testing.T) {
	c, path := localConfig(t)
	var out, diagnostic bytes.Buffer
	if err := cli.Execute("test", []string{"runner", "status", "--config", path}, &out, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"state":"idle"`) || diagnostic.Len() != 0 {
		t.Fatal(out.String(), diagnostic.String())
	}
	// A killed runner leaves its claim and the last lease it stored, which
	// has since expired.
	active := filepath.Join(c.Root, ".active")
	os.Mkdir(active, 0700)
	lapsed, _ := json.Marshal(runnerprotocol.Lease{Schema: "readmit-runner-lease/v1", Expires: time.Now().Add(-time.Minute), MaxSeconds: 60, MaxJobs: 4})
	os.WriteFile(filepath.Join(active, "lease.json"), lapsed, 0600)
	status, err := customerrunner.Health(c.Root)
	if err != nil || status.State != "recovery_required" {
		t.Fatal(status, err)
	}
	// The job would run: its inputs prepare and bind the configured
	// environment, and it is admitted. Only the interrupted claim refuses it,
	// before the hub is asked for anything.
	clock := newFakeClock()
	hub := newMemoryHub(clock)
	job := customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "interrupted", Spec: jobSpec(t, "127.0.0.1:9", "lab")}
	if run := admitted(t, c, job, hub, clock); !errors.Is(run.err, customerrunner.ErrRefused) {
		t.Fatalf("stale claim reassigned: %v", run.err)
	}
	hub.quiet(t)
	if _, err := os.Stat(active); err != nil {
		t.Fatal("recovery claim deleted", err)
	}
	if retained, err := customerrunner.Retained(c.Root, job.ID); err != nil || retained {
		t.Fatalf("a refused job reserved its id: %v %v", retained, err)
	}
	// Once the operator has established that the interrupted process is gone,
	// the documented recovery removes only the stale claim, and the runner
	// serves its root again.
	if err := os.RemoveAll(active); err != nil {
		t.Fatal(err)
	}
	fixture := newReceiver(t)
	recovered := customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "recovered", Spec: jobSpec(t, fixture.address, "lab")}
	runs := start(t, c, recovered, hub, clock)
	fixture.waitReceived(t)
	close(fixture.ack)
	if run := finished(t, runs); run.err != nil || run.summary.State != durablerun.Passed {
		t.Fatalf("the runner after recovery: %+v %v", run.summary, run.err)
	}
	hub.expect(t, "claim recovered", "renew recovered", "release recovered")
	// Service restart skips a permanently claimed ID without reading its missing
	// spec or contacting the deliberately unavailable hub.
	claimed := customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "claimed", Spec: filepath.Join(c.Root, "missing-spec")}
	os.Mkdir(filepath.Join(c.Root, claimed.ID), 0700)
	inbox := t.TempDir()
	os.Chmod(inbox, 0700)
	raw, _ := json.Marshal(claimed)
	os.WriteFile(filepath.Join(inbox, "job.json"), raw, 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := customerrunner.Serve(ctx, c, inbox); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(customerrunner.RunPath(c.Root, claimed.ID)); !os.IsNotExist(err) {
		t.Fatal("interrupted job replayed")
	}
}
func TestRunnerConfigurationRefusesUnknownNullAndNestedOmissions(t *testing.T) {
	_, path := localConfig(t)
	raw, _ := os.ReadFile(path)
	if _, err := customerrunner.ReadConfig(path); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{}`, strings.Replace(string(raw), `"arguments":[]`, `"arguments":null`, 1), strings.Replace(string(raw), `,"arguments":[]`, "", 1), string(raw[:len(raw)-1]) + `,"extra":true}`} {
		os.WriteFile(path, []byte(bad), 0600)
		if _, err := customerrunner.ReadConfig(path); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
}

// A root that is itself evidence is never written to: an admitted job whose
// inputs would run is refused before the runner claims the root or asks the
// hub for anything.
func TestRunnerRefusesEvidenceRootBeforeCreatingClaim(t *testing.T) {
	c, _ := localConfig(t)
	os.WriteFile(filepath.Join(c.Root, "identity.sha256"), []byte("retained evidence"), 0600)
	clock := newFakeClock()
	hub := newMemoryHub(clock)
	job := customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "job", Spec: jobSpec(t, "127.0.0.1:9", "lab")}
	if run := admitted(t, c, job, hub, clock); !errors.Is(run.err, customerrunner.ErrRefused) {
		t.Fatalf("evidence root accepted: %v", run.err)
	}
	hub.quiet(t)
	for _, name := range []string{".active", job.ID} {
		if _, err := os.Lstat(filepath.Join(c.Root, name)); !os.IsNotExist(err) {
			t.Fatalf("evidence changed: %s %v", name, err)
		}
	}
}

// A job id the root holds is retained, whatever the entry became; an id the
// root does not hold is free; and a root that cannot be read is refused
// rather than reported free.
func TestRetainedReportsTheIdsTheRootReserves(t *testing.T) {
	c, _ := localConfig(t)
	if retained, err := customerrunner.Retained(c.Root, "nightly-001"); err != nil || retained {
		t.Fatalf("a free id: %v %v", retained, err)
	}
	os.Mkdir(filepath.Join(c.Root, "nightly-001"), 0700)
	if retained, err := customerrunner.Retained(c.Root, "nightly-001"); err != nil || !retained {
		t.Fatalf("a retained id: %v %v", retained, err)
	}
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if retained, err := customerrunner.Retained(blocked, "nightly-001"); err == nil || retained {
		t.Fatalf("a root that is not a directory: %v %v", retained, err)
	}
}
