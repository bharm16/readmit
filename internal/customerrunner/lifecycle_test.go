package customerrunner_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/customerrunner"
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
	active := filepath.Join(c.Root, ".active")
	os.Mkdir(active, 0700)
	status, err := customerrunner.Health(c.Root)
	if err != nil || status.State != "recovery_required" {
		t.Fatal(status, err)
	}
	job := customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "interrupted", Spec: filepath.Join(c.Root, "missing-spec")}
	if _, err := customerrunner.Run(context.Background(), c, job); err == nil {
		t.Fatal("stale claim reassigned")
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatal("recovery claim deleted", err)
	}
	// Service restart skips a permanently claimed ID without reading its missing
	// spec or contacting the deliberately unavailable hub.
	os.Mkdir(filepath.Join(c.Root, job.ID), 0700)
	inbox := t.TempDir()
	os.Chmod(inbox, 0700)
	raw, _ := json.Marshal(job)
	os.WriteFile(filepath.Join(inbox, "job.json"), raw, 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := customerrunner.Serve(ctx, c, inbox); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Root, job.ID, "run")); !os.IsNotExist(err) {
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
func TestRunnerRefusesEvidenceRootBeforeCreatingClaim(t *testing.T) {
	c, _ := localConfig(t)
	os.WriteFile(filepath.Join(c.Root, "identity.sha256"), []byte("retained evidence"), 0600)
	if _, err := customerrunner.Run(context.Background(), c, customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "job", Spec: filepath.Join(c.Root, "spec.json")}); err == nil {
		t.Fatal("evidence root accepted")
	}
	if _, err := os.Lstat(filepath.Join(c.Root, ".active")); !os.IsNotExist(err) {
		t.Fatal("evidence changed")
	}
}
