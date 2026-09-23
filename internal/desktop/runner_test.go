package desktop_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
)

// runnerConfigInput is the structured form of a complete configuration. The
// runner root names a directory that does not exist here, because the file an
// application prepares is usually installed on the runner host afterwards.
func runnerConfigInput(t *testing.T, root string) desktop.RunnerConfigRequest {
	t.Helper()
	return desktop.RunnerConfigRequest{
		Hub: "https://hub.example:8443", Project: "alpha", Environment: "lab",
		Root: root, CA: "/etc/readmit-runner/ca.pem", Certificate: "/etc/readmit-runner/client.pem",
		Key:       desktop.RunnerReferenceInput{Command: "/usr/local/bin/customer-secret-reader", Arguments: []string{"runner-key"}},
		Token:     desktop.RunnerReferenceInput{Command: "/usr/local/bin/customer-secret-reader", Arguments: []string{"runner-token"}},
		UpdateKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), UpdateEngine: "NEXT_APPROVED_BUILD",
	}
}

func TestRunnerConfigGenerationProducesTheValidatedDocument(t *testing.T) {
	app := workspaceApp(t)
	request := runnerConfigInput(t, "/var/lib/readmit-runner/runs")
	preview := app.PreviewRunnerConfig(request)
	if preview.State != desktop.Completed || preview.Document == "" {
		t.Fatalf("preview: %+v", preview)
	}
	if !strings.Contains(preview.Document, `"schema": "readmit-runner/v1"`) {
		t.Fatalf("canonical form: %s", preview.Document)
	}
	var decoded map[string]any
	if json.Unmarshal([]byte(preview.Document), &decoded) != nil {
		t.Fatal("document is not JSON")
	}
	if _, ok := decoded["key"]; !ok {
		t.Fatal("document lost a member")
	}
	// A save writes the same canonical bytes to one new private file, once.
	repeat := runnerConfigInput(t, "/var/lib/readmit-runner/runs")
	repeat.Output = filepath.Join(t.TempDir(), "runner-config.json")
	written := app.SaveRunnerConfig(repeat)
	if written.State != desktop.Completed || written.Output != repeat.Output || written.SHA256 != preview.SHA256 {
		t.Fatalf("save: %+v", written)
	}
	info, err := os.Lstat(repeat.Output)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatalf("configuration must be a private file: %v %v", info, err)
	}
	again := app.SaveRunnerConfig(repeat)
	if again.State != desktop.Failed || !strings.Contains(again.Reason, "never replaced") {
		t.Fatalf("existing destination: %+v", again)
	}
}

func TestRunnerConfigGenerationRefusesIncompleteStructuredForms(t *testing.T) {
	app := workspaceApp(t)
	bad := []func(*desktop.RunnerConfigRequest){
		func(r *desktop.RunnerConfigRequest) { r.Hub = "http://hub.example:8443" },
		func(r *desktop.RunnerConfigRequest) { r.Project = "Alpha" },
		func(r *desktop.RunnerConfigRequest) { r.Root = "var/lib/readmit-runner/runs" },
		func(r *desktop.RunnerConfigRequest) { r.CA = "ca.pem" },
		func(r *desktop.RunnerConfigRequest) { r.UpdateEngine = "" },
		func(r *desktop.RunnerConfigRequest) { r.UpdateKey = "not-base64" },
		func(r *desktop.RunnerConfigRequest) { r.Key.Command = "" },
		func(r *desktop.RunnerConfigRequest) { r.Environment = "" },
	}
	for i, change := range bad {
		request := runnerConfigInput(t, "/var/lib/readmit-runner/runs")
		change(&request)
		if result := app.PreviewRunnerConfig(request); result.State != desktop.Failed {
			t.Fatalf("case %d accepted: %+v", i, result)
		}
	}
}

func TestReadRunnerConfigDisplaysAConfigurationTheCommandLineWrote(t *testing.T) {
	// Written exactly as the CLI documentation's example, not through the
	// generator, so a configuration installed by an administrator reads.
	existing := `{"schema":"readmit-runner/v1","hub":"https://hub.example:8443","project":"alpha",` +
		`"environment":"lab","root":"/var/lib/readmit-runner/runs","ca":"/etc/readmit-runner/ca.pem",` +
		`"certificate":"/etc/readmit-runner/client.pem",` +
		`"key":{"command":"/usr/local/bin/customer-secret-reader","arguments":["runner-key"]},` +
		`"token":{"command":"/usr/local/bin/customer-secret-reader","arguments":["runner-token"]},` +
		`"update_key":"` + base64.StdEncoding.EncodeToString(make([]byte, 32)) + `","update_engine":"NEXT_APPROVED_BUILD"}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	app := workspaceApp(t)
	result := app.ReadRunnerConfig(path)
	if result.State != desktop.Completed || result.Config == nil {
		t.Fatalf("read: %+v", result)
	}
	if result.Config.Project != "alpha" || result.Config.Environment != "lab" || result.Engine == "" {
		t.Fatalf("config view: %+v", result.Config)
	}
	if result.HealthNote == "" {
		t.Fatal("an absent runner root must be said, not shown as healthy")
	}
}

func TestSaveRunnerGrantValidatesThroughTheAdmissionProtocol(t *testing.T) {
	app := workspaceApp(t)
	output := filepath.Join(t.TempDir(), "runners.json")
	request := desktop.RunnerGrantRequest{Project: "alpha", Subject: "runner-subject", Environment: "lab", MaxSeconds: 300, MaxJobs: 100, Output: output}
	result := app.SaveRunnerGrant(request)
	if result.State != desktop.Completed {
		t.Fatalf("grant: %+v", result)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := runnerprotocol.DecodePolicy(raw)
	if err != nil {
		t.Fatalf("generated policy is not a valid readmit-runner-policy/v1 document: %v", err)
	}
	if len(policy.Runners) != 1 || policy.Runners[0].Spec != "readmit-test/v1" || policy.Runners[0].Profile != "readmit-siu-v1" {
		t.Fatalf("default pins: %+v", policy.Runners)
	}
	if policy.Runners[0].Engine == "" {
		t.Fatal("an empty engine must default to the running build's pin, never stay empty")
	}
	// A second grant for the same pair replaces the first; another pair adds.
	second := desktop.RunnerGrantRequest{Policy: output, Project: "alpha", Subject: "runner-subject", Environment: "lab", Engine: "dev", MaxSeconds: 30, MaxJobs: 2, Output: filepath.Join(t.TempDir(), "runners-2.json")}
	if result := app.SaveRunnerGrant(second); result.State != desktop.Completed {
		t.Fatalf("replace: %+v", result)
	}
	third := desktop.RunnerGrantRequest{Policy: output, Project: "alpha", Subject: "runner-subject", Environment: "prod", Engine: "dev", MaxSeconds: 30, MaxJobs: 2, Output: filepath.Join(t.TempDir(), "runners-3.json")}
	if result := app.SaveRunnerGrant(third); result.State != desktop.Completed {
		t.Fatalf("append: %+v", result)
	}
	raw, err = os.ReadFile(third.Output)
	if err != nil {
		t.Fatal(err)
	}
	policy, err = runnerprotocol.DecodePolicy(raw)
	if err != nil || len(policy.Runners) != 2 {
		t.Fatalf("policy after edit: %+v %v", policy.Runners, err)
	}
	// A grant the protocol refuses never reaches the file.
	refused := desktop.RunnerGrantRequest{Project: "alpha", Subject: "runner-subject", Environment: "lab", MaxSeconds: 0, MaxJobs: 1, Output: filepath.Join(t.TempDir(), "runners-4.json")}
	if result := app.SaveRunnerGrant(refused); result.State != desktop.Failed {
		t.Fatalf("refused grant: %+v", result)
	}
	if _, err := os.Stat(refused.Output); !os.IsNotExist(err) {
		t.Fatal("a refused grant left a file")
	}
	if result := app.SaveRunnerGrant(desktop.RunnerGrantRequest{Subject: " ", Environment: "lab", MaxSeconds: 30, MaxJobs: 1}); result.State != desktop.Failed {
		t.Fatal("a grant without a subject was accepted")
	}
}

// writableSpec authors one runnable spec the way the runner documentation's
// job flow does: a generated case, a nonproduction loopback target, and one
// ACK-boundary assertion. Preparation reads it without any network activity.
func writableSpec(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Write(filepath.Join(dir, "case"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	target := replay.Target{Schema: replay.TargetSchemaV3, TestEndpoint: true, Address: "127.0.0.1:1", Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "30s", MaxACKBytes: 4096, Name: "lab", Classification: replay.Nonproduction}
	val := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "ACK", Input: testrunner.Input{Case: "case", Messages: []string{"s0001-e000001"}}, Target: "target.json", Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset fixture"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "accepted", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &val}}}}}
	for name, v := range map[string]any{"target.json": target, "spec.json": spec} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "spec.json")
}

func scheduleEntry(pin string, approved bool) desktop.ScheduleEntryInput {
	return desktop.ScheduleEntryInput{
		ID: "nightly", Zone: "UTC", At: "02:30", WindowSeconds: 600,
		Runner: "/etc/readmit-runner/config.json", Spec: "/srv/readmit/spec.json",
		Input: pin, Route: "https://alerts.example/", Approved: approved,
	}
}

func TestSchedulePreviewShowsZoneOccurrenceAndAlertSemantics(t *testing.T) {
	app := workspaceApp(t)
	pin := strings.Repeat("a", 64)
	preview := app.PreviewSchedulePolicy(desktop.SchedulePolicyRequest{
		Anchor: "2026-03-07", Entries: []desktop.ScheduleEntryInput{scheduleEntry(pin, true)},
	})
	if preview.State != desktop.Completed || preview.Identity == "" || preview.Concurrency != "serial-skip-missed" {
		t.Fatalf("preview: %+v", preview)
	}
	if len(preview.Entries) != 1 || len(preview.Entries[0].Occurrences) != 3 {
		t.Fatalf("occurrences: %+v", preview.Entries)
	}
	// Occurrences are computed by the hub's own occurrence function; a day
	// wholly in the past is the contract's missed.
	if preview.Entries[0].Occurrences[0].UTC != "2026-03-07T02:30:00Z" || preview.Entries[0].Occurrences[0].State != "missed" {
		t.Fatalf("first occurrence: %+v", preview.Entries[0].Occurrences[0])
	}
	// In a zone where the scheduled minute does not exist on the
	// spring-forward day, the occurrence is the contract's dst-gap, never a
	// shifted instant.
	gapEntry := scheduleEntry(pin, true)
	gapEntry.Zone = "America/New_York"
	gapPreview := app.PreviewSchedulePolicy(desktop.SchedulePolicyRequest{Anchor: "2026-03-07", Entries: []desktop.ScheduleEntryInput{gapEntry}})
	if gapPreview.State != desktop.Completed {
		t.Fatalf("gap preview: %+v", gapPreview)
	}
	if gapPreview.Entries[0].Occurrences[0].UTC != "2026-03-07T07:30:00Z" {
		t.Fatalf("zoned occurrence: %+v", gapPreview.Entries[0].Occurrences[0])
	}
	if gapPreview.Entries[0].Occurrences[1].State != "dst-gap" || gapPreview.Entries[0].Occurrences[1].UTC != "" {
		t.Fatalf("spring-forward occurrence: %+v", gapPreview.Entries[0].Occurrences[1])
	}
	if preview.Entries[0].Notification == "" || preview.Alert != `{"schema":"readmit-hub-alert/v1","state":"failed","coverage":"not-assessed"}` {
		t.Fatalf("notification preview: %+v", preview)
	}
	// An unapproved entry disables notifications; the contract refuses an
	// approved entry without a route.
	unapproved := app.PreviewSchedulePolicy(desktop.SchedulePolicyRequest{Anchor: "2026-03-07", Entries: []desktop.ScheduleEntryInput{scheduleEntry(pin, false)}})
	if unapproved.State != desktop.Completed || !strings.Contains(unapproved.Entries[0].Notification, "disabled") {
		t.Fatalf("unapproved: %+v", unapproved)
	}
	noRoute := scheduleEntry(pin, true)
	noRoute.Route = ""
	if result := app.PreviewSchedulePolicy(desktop.SchedulePolicyRequest{Entries: []desktop.ScheduleEntryInput{noRoute}}); result.State != desktop.Failed {
		t.Fatalf("approved without route: %+v", result)
	}
	// An occurrence older than the entry's window is the contract's missed.
	// The entry fell due an hour ago, so the occurrence is past its window and
	// the next day's is ahead whatever time of day the test runs; an entry
	// due at midnight was still inside its window just after midnight UTC.
	due := time.Now().UTC().Add(-time.Hour)
	past := scheduleEntry(pin, false)
	past.Zone = "UTC"
	past.At = due.Format("15:04")
	pastPreview := app.PreviewSchedulePolicy(desktop.SchedulePolicyRequest{Anchor: due.AddDate(0, 0, -1).Format("2006-01-02"), Entries: []desktop.ScheduleEntryInput{past}})
	if pastPreview.State != desktop.Completed || pastPreview.Entries[0].Occurrences[1].State != "missed" || pastPreview.Entries[0].Occurrences[2].State != "scheduled" {
		t.Fatalf("missed marking: %+v", pastPreview.Entries[0].Occurrences)
	}
}

func TestSaveSchedulePolicyWritesRevisionAndRefusesStalePin(t *testing.T) {
	app := workspaceApp(t)
	// The spec is not readable here, so the pin is operator-provided and the
	// save accepts it; the hub will refuse a changed pin at execution.
	output := filepath.Join(t.TempDir(), "schedules.json")
	request := desktop.SchedulePolicyRequest{Output: output, Entries: []desktop.ScheduleEntryInput{scheduleEntry(strings.Repeat("b", 64), false)}}
	result := app.SaveSchedulePolicy(request)
	if result.State != desktop.Completed {
		t.Fatalf("save: %+v", result)
	}
	reopened := app.OpenSchedulePolicy(output)
	if reopened.State != desktop.Completed || reopened.Identity != result.Identity || len(reopened.Entries) != 1 {
		t.Fatalf("reopen: %+v", reopened)
	}
	info, err := os.Lstat(output)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("policy must be private: %v %v", info, err)
	}
	// A readable spec whose prepared inputs disagree with the declared pin is
	// refused before the hub ever sees the revision.
	spec := writableSpec(t, t.TempDir())
	stale := scheduleEntry(strings.Repeat("c", 64), false)
	stale.Spec = spec
	stale.Zone = "UTC"
	stale.Runner = "/etc/readmit-runner/config.json"
	if result := app.SaveSchedulePolicy(desktop.SchedulePolicyRequest{Entries: []desktop.ScheduleEntryInput{stale}}); result.State != desktop.Failed || !strings.Contains(result.Reason, "input pin does not match") {
		t.Fatalf("stale pin: %+v", result)
	}
}

func TestCIHandoffGeneratesTheDocumentedWorkflows(t *testing.T) {
	app := workspaceApp(t)
	base := desktop.CIHandoffRequest{
		Binary:          "/opt/readmit/readmit",
		OperationPolicy: "/etc/readmit/operation-policy.json",
		SuiteFile:       "/srv/readmit/suite.json",
		Environment:     "lab",
		RunDirectory:    "/var/lib/readmit-ci/run-1",
		CoverageFile:    "/srv/readmit/coverage.json",
	}
	for _, integration := range []string{"posix", "github", "azure"} {
		request := base
		request.Integration = integration
		request.Output = filepath.Join(t.TempDir(), "handoff-"+integration)
		result := app.SaveCIHandoff(request)
		if result.State != desktop.Completed {
			t.Fatalf("%s: %+v", integration, result)
		}
		if !strings.Contains(result.Document, `"$READMIT_BIN" --operation-policy "$OPERATION_POLICY" suite ci "$SUITE_FILE" --environment "$SUITE_ENVIRONMENT" --output "$RUN_DIRECTORY" --requirements "$COVERAGE_FILE" --send --deadline 5m`) {
			t.Fatalf("%s lost the documented command: %s", integration, result.Document)
		}
		workflow := result.Document[strings.Index(result.Document, "# --- reviewed workflow"):]
		for _, value := range []string{request.Binary, request.OperationPolicy, request.SuiteFile, request.RunDirectory, request.CoverageFile} {
			if strings.Contains(workflow, value) {
				t.Fatalf("%s leaked a customer value into the workflow: %s", integration, workflow)
			}
		}
		if !strings.Contains(result.Document, "No checkout, upload, retry") {
			t.Fatalf("%s lost the custody statement: %s", integration, result.Document)
		}
	}
	refusals := []func(*desktop.CIHandoffRequest){
		func(r *desktop.CIHandoffRequest) { r.Integration = "gitlab" },
		func(r *desktop.CIHandoffRequest) { r.Environment = "prod env" },
		func(r *desktop.CIHandoffRequest) { r.CoverageFile = "coverage.json" },
		func(r *desktop.CIHandoffRequest) { r.SuiteFile = "" },
	}
	for i, change := range refusals {
		request := base
		request.Output = filepath.Join(t.TempDir(), "handoff")
		change(&request)
		if result := app.SaveCIHandoff(request); result.State != desktop.Failed {
			t.Fatalf("case %d accepted: %+v", i, result)
		}
	}
}

func TestInspectCIResultsReadsRetainedSummaries(t *testing.T) {
	app := workspaceApp(t)
	if result := app.InspectCIResults("/tmp/definitely-absent-readmit-ci"); result.State != desktop.Failed {
		t.Fatalf("absent directory: %+v", result)
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	if result := app.InspectCIResults(dir); result.State != desktop.Failed || !strings.Contains(result.Reason, "process exit") {
		t.Fatalf("missing summary: %+v", result)
	}
	ci := suite.CIError()
	raw, _ := json.Marshal(ci)
	if err := os.WriteFile(filepath.Join(dir, "ci.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	gate := map[string]any{"schema": "readmit-ci-gate/v1", "state": "unknown", "exit_code": 2, "approval": "unknown", "pins": "unknown", "coverage": "unknown", "baseline": "unknown", "retention": "unknown", "target_revision": "unknown"}
	gateRaw, _ := json.Marshal(gate)
	if err := os.WriteFile(filepath.Join(dir, "gate.json"), gateRaw, 0600); err != nil {
		t.Fatal(err)
	}
	result := app.InspectCIResults(dir)
	if result.State != desktop.Completed || result.CI == nil || result.CI.State != "error" || result.Gate == nil || result.Gate.State != "unknown" {
		t.Fatalf("inspect: %+v", result)
	}
}

func TestInspectGatePolicyComputesTheCanonicalIdentity(t *testing.T) {
	app := workspaceApp(t)
	pin := strings.Repeat("d", 64)
	policy := suite.GatePolicy{
		Schema: suite.GatePolicySchema, Environment: "lab", Revision: "fixture-v1", Engine: "engine-v1",
		Promotion: strings.Repeat("e", 64),
		Coverage: suite.CoverageDocument{
			Schema: suite.CoverageSchema, SuiteSHA256: strings.Repeat("1", 64),
			Specifications: []suite.CoverageSpecification{{Job: "booking-one", SHA256: pin}},
			Requirements:   []suite.Requirement{{ID: "booking", Jobs: []string{"booking-one"}}},
		},
		Baseline: []suite.CoverageSpecification{{Job: "booking-one", SHA256: pin}},
		MaxBytes: 256 << 20, RetainUntil: "2036-01-01T00:00:00Z", Approver: "reviewer", Rationale: "reviewed fixture",
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "gate-policy.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	result := app.InspectGatePolicy(path)
	if result.State != desktop.Completed || result.Identity != policy.Identity() || result.Specifications != 1 {
		t.Fatalf("gate policy: %+v", result)
	}
	if result := app.InspectGatePolicy(filepath.Join(t.TempDir(), "absent.json")); result.State != desktop.Failed {
		t.Fatalf("absent policy: %+v", result)
	}
}

func TestInspectRunnerJobRefusesAnotherEnvironmentBeforeAnythingIsSent(t *testing.T) {
	app := workspaceApp(t)
	dir := t.TempDir()
	spec := writableSpec(t, dir)
	// A configuration for a different environment is structurally valid; the
	// prepared inputs still refuse to run under it.
	configRequest := runnerConfigInput(t, "/var/lib/readmit-runner/runs")
	configRequest.Environment = "other"
	configRequest.Output = filepath.Join(dir, "other.json")
	if saved := app.SaveRunnerConfig(configRequest); saved.State != desktop.Completed {
		t.Fatalf("config: %+v", saved)
	}
	jobPath := filepath.Join(dir, "job.json")
	if saved := app.SaveRunnerJob(desktop.RunnerJobRequest{ID: "nightly-001", Spec: spec, Output: jobPath}); saved.State != desktop.Completed {
		t.Fatalf("job: %+v", saved)
	}
	preview := app.InspectRunnerJob(configRequest.Output, jobPath)
	if preview.State != desktop.Failed || !strings.Contains(preview.Reason, "bind environment lab") || !strings.Contains(preview.Reason, "configured for other") {
		t.Fatalf("environment binding: %+v", preview)
	}
	if preview.InputIdentity == "" {
		t.Fatal("the refusal must still name the pin it computed")
	}
}

func TestVerifyRunnerUpdateChecksTheStagedCandidateWithoutExecutingIt(t *testing.T) {
	app := workspaceApp(t)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	configRequest := runnerConfigInput(t, "/var/lib/readmit-runner/runs")
	configRequest.UpdateKey = base64.StdEncoding.EncodeToString(public)
	configRequest.UpdateEngine = "v-next"
	configRequest.Output = filepath.Join(t.TempDir(), "config.json")
	if saved := app.SaveRunnerConfig(configRequest); saved.State != desktop.Completed {
		t.Fatalf("config: %+v", saved)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "candidate")
	if err := os.WriteFile(bin, []byte("synthetic candidate"), 0700); err != nil {
		t.Fatal(err)
	}
	claims := customerrunner.Update{Schema: "readmit-runner-update/v1", Engine: "v-next", OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic candidate")))}
	unsigned, err := json.Marshal(claims, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	claims.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, unsigned))
	manifest := filepath.Join(dir, "update.json")
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if result := app.VerifyRunnerUpdate(configRequest.Output, manifest, bin); result.State != desktop.Completed {
		t.Fatalf("verify: %+v", result)
	}
	if err := os.WriteFile(bin, []byte("altered candidate"), 0700); err != nil {
		t.Fatal(err)
	}
	if result := app.VerifyRunnerUpdate(configRequest.Output, manifest, bin); result.State != desktop.Failed {
		t.Fatalf("altered candidate: %+v", result)
	}
	if result := app.VerifyRunnerUpdate(configRequest.Output, filepath.Join(dir, "absent.json"), bin); result.State != desktop.Failed {
		t.Fatalf("absent manifest: %+v", result)
	}
}
