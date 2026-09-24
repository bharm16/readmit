package desktop_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/testlicense"
)

func TestResumeAndCleanDurableRunsAgreeWithCommandLine(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	root := ackWorkspace(t, peer.address)
	writeAckSpec(t, root, "spec.json", "AA")
	spec := filepath.Join(root, "spec.json")
	retained := filepath.Join(root, "retained")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // terminal before the first intent: every occurrence is unattempted
	if _, err := durablerun.Start(ctx, spec, retained); err != nil {
		t.Fatal(err)
	}
	recovery, err := durablerun.Recover(retained)
	if err != nil || !recovery.SafeToRepeat || recovery.NotAttempted != 1 {
		t.Fatalf("retained run: %+v %v", recovery, err)
	}
	before, err := os.ReadFile(filepath.Join(retained, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	app := workspaceApp(t)
	window := app.ResumeDurableRun(desktop.ResumeRunRequest{Workspace: root, Job: "retained", Spec: "spec.json", Output: "window-resumed"})
	if window.State != desktop.Completed || window.Resume == nil || window.Resume.Repeated != 1 || window.Resume.Run.State != durablerun.Passed {
		t.Fatalf("window resume: %+v", window)
	}
	var stdout, stderr strings.Builder
	if err := cli.Execute("dev", []string{"--operation-policy", testlicense.New(t), "run", "resume", retained, spec, "--send", "--output", filepath.Join(root, "cli-resumed"), "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("command resume: %v %s", err, stderr.String())
	}
	var command durablerun.Resumption
	if err := json.Unmarshal([]byte(stdout.String()), &command); err != nil {
		t.Fatal(err)
	}
	if command.Schema != window.Resume.Schema || command.Repeated != window.Resume.Repeated || command.ResumedFrom != window.Resume.ResumedFrom || command.Run.State != window.Resume.Run.State {
		t.Fatalf("facade and command line disagree: %+v vs %+v", window.Resume, command)
	}
	if peer.deliveries() != 2 {
		t.Fatalf("deliveries = %d, want one per deliberate new execution", peer.deliveries())
	}
	after, err := os.ReadFile(filepath.Join(retained, "journal.jsonl"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("resume changed the retained journal: %v", err)
	}
	if refused := app.ResumeDurableRun(desktop.ResumeRunRequest{Workspace: root, Job: "window-resumed", Spec: "spec.json", Output: "refused"}); refused.State != desktop.Failed || !strings.Contains(refused.Reason, "a delivery was acknowledged") {
		t.Fatalf("an acknowledged send was repeatable: %+v", refused)
	}
	if _, err := os.Lstat(filepath.Join(root, "refused")); !os.IsNotExist(err) {
		t.Fatalf("refused resume created an output: %v", err)
	}

	lease := durablerun.Lease{Schema: durablerun.LeaseSchema, Holder: durablerun.Holder{PID: 1, StartedAt: time.Now().UTC()}, Resources: []durablerun.Resource{{Kind: durablerun.EndpointResource, Name: peer.address}}}
	raw, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{"window-resumed", "cli-resumed"} {
		if err := os.WriteFile(filepath.Join(root, output, "lease.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cleaned := app.CleanDurableRun(root, "window-resumed")
	if cleaned.State != desktop.Completed || cleaned.Cleanup == nil || cleaned.Cleanup.Schema != durablerun.CleanupSchema || len(cleaned.Cleanup.Removed) != 1 || cleaned.Cleanup.Removed[0] != "lease.json" {
		t.Fatalf("window cleanup: %+v", cleaned)
	}
	stdout.Reset()
	stderr.Reset()
	if err := cli.Execute("dev", []string{"run", "clean", filepath.Join(root, "cli-resumed"), "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("command clean: %v %s", err, stderr.String())
	}
	var commandClean durablerun.Cleanup
	if err := json.Unmarshal([]byte(stdout.String()), &commandClean); err != nil {
		t.Fatal(err)
	}
	if commandClean.Schema != cleaned.Cleanup.Schema || !slicesEqual(commandClean.Removed, cleaned.Cleanup.Removed) || !slicesEqual(commandClean.Retained, cleaned.Cleanup.Retained) {
		t.Fatalf("cleanup parity: %+v vs %+v", cleaned.Cleanup, commandClean)
	}
	for _, output := range []string{"window-resumed", "cli-resumed"} {
		if _, err := os.Lstat(filepath.Join(root, output, "lease.json")); !os.IsNotExist(err) {
			t.Fatalf("%s lease survived cleanup: %v", output, err)
		}
		if _, err := os.Stat(filepath.Join(root, output, "journal.jsonl")); err != nil {
			t.Fatalf("%s evidence was removed: %v", output, err)
		}
	}
	// A completed journal with a torn trailing record does not prove the
	// writer stopped. Both paths refuse to remove its lease.
	f, err := os.OpenFile(filepath.Join(retained, "journal.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"sequence":99,"kind":"in`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(retained, "lease.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if refused := app.CleanDurableRun(root, "retained"); refused.State != desktop.Failed || !strings.Contains(refused.Reason, "completion was not recorded") {
		t.Fatalf("window cleaned a live lease: %+v", refused)
	}
	stdout.Reset()
	stderr.Reset()
	if err := cli.Execute("dev", []string{"run", "clean", retained, "--json"}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "completion was not recorded") {
		t.Fatalf("command cleaned a live lease: %v", err)
	}
	if _, err := os.Stat(filepath.Join(retained, "lease.json")); err != nil {
		t.Fatalf("refused cleanup removed the live lease: %v", err)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestResumeDurableRunRefusesUncertainDeliveryAsTheCommandDoes(t *testing.T) {
	peer := newDelayedAckingPeer(t, "AA", 2*time.Second)
	root := ackWorkspace(t, peer.address)
	writeAckSpec(t, root, "spec.json", "AA")
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, err := durablerun.Start(ctx, filepath.Join(root, "spec.json"), filepath.Join(root, "uncertain"))
		finished <- err
	}()
	deadline := time.After(5 * time.Second)
	for peer.deliveries() == 0 {
		select {
		case <-deadline:
			cancel()
			t.Fatal("the independent peer saw no first send")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	recovery, err := durablerun.Recover(filepath.Join(root, "uncertain"))
	if err != nil || recovery.Uncertain != 1 || recovery.SafeToRepeat {
		t.Fatalf("uncertain source: %+v %v", recovery, err)
	}
	app := workspaceApp(t)
	refused := app.ResumeDurableRun(desktop.ResumeRunRequest{Workspace: root, Job: "uncertain", Spec: "spec.json", Output: "window-refused"})
	if refused.State != desktop.Failed || refused.Resume != nil || !strings.Contains(refused.Reason, "an intent was synced without an acknowledged outcome") {
		t.Fatalf("window repeated uncertain delivery: %+v", refused)
	}
	var stdout, stderr strings.Builder
	err = cli.Execute("dev", []string{"--operation-policy", testlicense.New(t), "run", "resume", filepath.Join(root, "uncertain"), filepath.Join(root, "spec.json"), "--send", "--output", filepath.Join(root, "cli-refused"), "--json"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "an intent was synced without an acknowledged outcome") {
		t.Fatalf("command repeated uncertain delivery: %v", err)
	}
	for _, output := range []string{"window-refused", "cli-refused"} {
		if _, err := os.Lstat(filepath.Join(root, output)); !os.IsNotExist(err) {
			t.Fatalf("%s was created after refused resume: %v", output, err)
		}
	}
	if peer.deliveries() != 1 {
		t.Fatalf("refused resumes sent %d times", peer.deliveries())
	}
}

func TestChooseRunSpecRefusesOutsideOpenWorkspaceOnEveryPlatform(t *testing.T) {
	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	outside := filepath.Join(parent, "outside.json")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	choice := newApp(t, &chooser{files: []string{outside}}).ChooseRunSpec(workspace)
	if choice.State != desktop.Failed || choice.Entry != "" || choice.Reason != "a saved test or suite is one entry of the open workspace; advanced selection cannot reach outside it" {
		t.Fatalf("the native dialog selected a file outside the open workspace: %+v", choice)
	}
}
