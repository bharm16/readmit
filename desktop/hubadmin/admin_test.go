package hubadmin

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

func configCopy(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../hub/config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestHubAdministrationPreviewUsesTheHubConfigReaderAndNeverExecutes(t *testing.T) {
	copy := configCopy(t)
	admin := new(Admin)
	base := Request{Operation: "migrate", ConfigCopy: copy, ConfigPath: "/etc/readmit-hub/config.json"}
	result := admin.Preview(base)
	if result.State != "completed" || result.Command != "readmit-hub -config '/etc/readmit-hub/config.json' migrate" {
		t.Fatalf("valid preview: %+v", result)
	}
	if len(result.Prerequisites) == 0 || len(result.Touches) == 0 || len(result.DoesNotTouch) == 0 {
		t.Fatalf("the handoff omitted its boundaries: %+v", result)
	}
	for name, change := range map[string]func(*Request){
		"relative host path": func(r *Request) { r.ConfigPath = "config.json" },
		"missing config":     func(r *Request) { r.ConfigCopy = filepath.Join(t.TempDir(), "missing.json") },
		"relative copy":      func(r *Request) { r.ConfigCopy = "config.json" },
		"unsupported schema": func(r *Request) {
			bad := strings.ReplaceAll(string(mustRead(t, copy)), "readmit-hub-config/v1", "readmit-hub-config/v9")
			file := filepath.Join(t.TempDir(), "bad.json")
			if err := os.WriteFile(file, []byte(bad), 0600); err != nil {
				t.Fatal(err)
			}
			r.ConfigCopy = file
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := base
			change(&request)
			got := admin.Preview(request)
			if got.State != "failed" || got.Command != "" {
				t.Fatalf("refusal: %+v", got)
			}
		})
	}
}

func TestHubAdministrationPreviewsEveryHostCommand(t *testing.T) {
	copy := configCopy(t)
	for _, operation := range []string{"migrate", "check", "backup", "restore"} {
		t.Run(operation, func(t *testing.T) {
			request := Request{Operation: operation, ConfigCopy: copy, ConfigPath: "/etc/readmit-hub/config.json"}
			if operation == "backup" || operation == "restore" {
				request.Directory = "/customer-backups/hub-1"
			}
			if operation == "restore" {
				request.LocalCopy = emptyHistoricalBackup(t)
			}
			got := new(Admin).Preview(request)
			if got.State != "completed" || !strings.HasSuffix(got.Command, " "+operation) {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func emptyHistoricalBackup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"schema":"readmit-hub-backup/v1","metadata_version":2,"artifacts":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The local verification is the same exported reader readmit-hub invokes.
// A historical v1 backup stays readable, and damage is refused without any
// mutation to the copied backup.
func TestHubAdministrationVerifyBackupMatchesTheHubReaderForHistoricalArtifacts(t *testing.T) {
	config := configCopy(t)
	backup := t.TempDir()
	payload := []byte("synthetic hub object")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	manifest := fmt.Sprintf(`{"schema":"readmit-hub-backup/v1","metadata_version":2,"artifacts":[{"sha256":%q,"size":%d,"retained_at":"2026-01-01T00:00:00Z"}]}`, digest, len(payload))
	if err := os.WriteFile(filepath.Join(backup, digest), payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "manifest.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	request := Request{Operation: "verify-backup", ConfigCopy: config, ConfigPath: "/etc/readmit-hub/config.json", Directory: "/customer-backups/historical", LocalCopy: backup}
	if err := hub.VerifyBackup(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	result := new(Admin).Preview(request)
	if result.State != "completed" || !strings.Contains(result.LocalResult, "passed") || !strings.Contains(result.Command, "-directory '/customer-backups/historical' verify-backup") {
		t.Fatalf("%+v", result)
	}
	if string(mustRead(t, filepath.Join(backup, digest))) != string(payload) || string(mustRead(t, filepath.Join(backup, "manifest.json"))) != manifest {
		t.Fatal("preview changed historical backup bytes")
	}
	if err := os.WriteFile(filepath.Join(backup, digest), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if hub.VerifyBackup(context.Background(), backup) == nil {
		t.Fatal("hub reader accepted damaged backup")
	}
	if refused := new(Admin).Preview(request); refused.State != "failed" || refused.Command != "" {
		t.Fatalf("%+v", refused)
	}
}

func TestHubAdministrationScheduleInitUsesBothHubPolicyReaders(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		file := filepath.Join(dir, name)
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
		return file
	}
	operation := write("operations.json", []byte(`{"schema":"readmit-hub-operation-policy/v1","operation_policy":"/private/activation.json","bindings":[]}`))
	policy := hub.SchedulePolicy{Schema: "readmit-hub-schedules/v1", Concurrency: "serial-skip-missed", Schedules: []hub.Schedule{{ID: "synthetic", Zone: "UTC", At: "12:00", WindowSeconds: 60, Runner: "/private/runner.json", Spec: "/private/spec.json", Input: strings.Repeat("a", 64)}}}
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	schedule := write("schedules.json", data)
	if _, err := hub.ReadOperationPolicy(mustRead(t, operation)); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.DecodeSchedules(data); err != nil {
		t.Fatal(err)
	}
	request := Request{Operation: "schedule-init", ConfigCopy: configCopy(t), ConfigPath: "/etc/readmit-hub/config.json", OperationPolicyCopy: operation, OperationPolicyPath: "/etc/readmit-hub/operations.json", SchedulePolicyCopy: schedule, SchedulePolicyPath: "/etc/readmit-hub/schedules.json"}
	result := new(Admin).Preview(request)
	if result.State != "completed" || !strings.Contains(result.Command, "-operation-policy '/etc/readmit-hub/operations.json' -schedule-policy '/etc/readmit-hub/schedules.json' schedule-init") {
		t.Fatalf("%+v", result)
	}
	if err := os.WriteFile(schedule, []byte(strings.ReplaceAll(string(data), "readmit-hub-schedules/v1", "readmit-hub-schedules/v9")), 0600); err != nil {
		t.Fatal(err)
	}
	if refused := new(Admin).Preview(request); refused.State != "failed" || refused.Command != "" {
		t.Fatalf("%+v", refused)
	}
}

func TestHubAdministrationSchedulePinMatchesTheHubInputIdentity(t *testing.T) {
	dir := t.TempDir()
	raw, err := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	_, err = bundle.Write(filepath.Join(dir, "case"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	target := replay.Target{Schema: replay.TargetSchemaV3, TestEndpoint: true, Address: "127.0.0.1:1234", Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "30s", MaxACKBytes: 4096, Name: "lab", Classification: replay.Nonproduction}
	accepted := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "synthetic ACK", Input: testrunner.Input{Case: "case", Messages: []string{"s0001-e000001"}}, Target: "target.json", Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset fixture"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "accepted", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &accepted}}}}}
	for name, value := range map[string]any{"target.json": target, "spec.json": spec} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	localSpec := filepath.Join(dir, "spec.json")
	identity, err := hub.ScheduleInputIdentity(localSpec)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Operation: "schedule-pin", ConfigCopy: configCopy(t), ConfigPath: "/etc/readmit-hub/config.json", Directory: "/private/spec.json", LocalCopy: localSpec}
	result := new(Admin).Preview(request)
	if result.State != "completed" || result.LocalResult != identity || result.Command != "readmit-hub -config '/etc/readmit-hub/config.json' -directory '/private/spec.json' schedule-pin" {
		t.Fatalf("%+v", result)
	}
	if err := os.WriteFile(localSpec, []byte(`{"schema":"readmit-test/v9"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if refused := new(Admin).Preview(request); refused.State != "failed" || refused.LocalResult != "" {
		t.Fatalf("%+v", refused)
	}
}

func TestHubAdministrationCancelPreviewDoesNotPerformHostWork(t *testing.T) {
	a := new(Admin)
	if idle := a.CancelPreview(); idle.State != "empty" {
		t.Fatalf("%+v", idle)
	}
	active, stop := context.WithCancel(context.Background())
	a.mu.Lock()
	a.running, a.cancel = true, stop
	a.mu.Unlock()
	if requested := a.CancelPreview(); requested.State != "busy" || active.Err() != context.Canceled {
		t.Fatalf("cancellation was not requested against the active local reader: %+v, %v", requested, active.Err())
	}
	a.mu.Lock()
	a.running, a.cancel = false, nil
	a.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := Request{Operation: "check", ConfigCopy: configCopy(t), ConfigPath: "/etc/readmit-hub/config.json"}
	if result := preview(ctx, request); result.State != "cancelled" || result.Command != "" {
		t.Fatalf("%+v", result)
	}
}

func TestHubAdministrationRefusesBackupPathsInsideFilesystemRoot(t *testing.T) {
	copy := configCopy(t)
	var config hub.Config
	if err := json.Unmarshal(mustRead(t, copy), &config); err != nil {
		t.Fatal(err)
	}
	config.Root = "/"
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copy, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"backup", "restore"} {
		request := Request{Operation: operation, ConfigCopy: copy, ConfigPath: "/etc/readmit-hub/config.json", Directory: "/customer-backups/hub-1", LocalCopy: emptyHistoricalBackup(t)}
		if result := new(Admin).Preview(request); result.State != "failed" || result.Command != "" {
			t.Fatalf("%s: %+v", operation, result)
		}
	}
}

func TestHubAdministrationLinuxHostPathsRejectWindowsDrivePaths(t *testing.T) {
	if hostPath(`C:\readmit-hub\config.json`) || hostPath(`C:/readmit-hub/config.json`) {
		t.Fatal("a Windows desktop path was mistaken for a Linux hub-host path")
	}
	if hostPath("/etc/readmit-hub/\x1b[2Jconfig.json") {
		t.Fatal("a control character entered a reviewed command")
	}
	if quoted := quote("/etc/readmit-hub/operator's config.json"); quoted != "'/etc/readmit-hub/operator'\\''s config.json'" {
		t.Fatalf("host path was not shell-quoted as one argument: %q", quoted)
	}
	var config hub.Config
	if err := json.Unmarshal(mustRead(t, configCopy(t)), &config); err != nil {
		t.Fatal(err)
	}
	config.Root = `C:\readmit-hub\artifacts`
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hub.ReadConfig(raw); err == nil {
		t.Fatal("hub configuration accepted a Windows artifact path")
	}
	operation := hub.OperationPolicy{Schema: "readmit-hub-operation-policy/v1", Policy: `C:\private\activation.json`, Bindings: []hub.OperationBinding{}}
	raw, err = json.Marshal(operation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hub.ReadOperationPolicy(raw); err == nil {
		t.Fatal("hub operation policy accepted a Windows path")
	}
	schedule := hub.SchedulePolicy{Schema: "readmit-hub-schedules/v1", Concurrency: "serial-skip-missed", Schedules: []hub.Schedule{{ID: "synthetic", Zone: "UTC", At: "12:00", WindowSeconds: 60, Runner: `C:\private\runner.json`, Spec: "/private/spec.json", Input: strings.Repeat("a", 64)}}}
	raw, err = json.Marshal(schedule)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hub.DecodeSchedules(raw); err == nil {
		t.Fatal("hub schedule accepted a Windows runner path")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
