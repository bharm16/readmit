package tests

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/diagnose"
)

func diagnoseCase(t *testing.T, fixture string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case")
	if _, stderr, err := run(t, "capture", "../testdata/fixtures/"+fixture, "--output", path); err != nil {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	return path
}

func TestDiagnoseExecutableWritesMatchingReportsAndPreservesCase(t *testing.T) {
	for _, fixture := range []string{"diagnose-booking.hl7", "diagnose-reschedule.hl7", "diagnose-missing-patient.hl7", "diagnose-ack.hl7"} {
		t.Run(fixture, func(t *testing.T) {
			casePath := diagnoseCase(t, fixture)
			before, err := os.ReadFile(filepath.Join(casePath, "identity.sha256"))
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "reports")
			stdout, stderr, err := run(t, "diagnose", casePath, "--output", output)
			if err != nil || stderr != "" || !strings.Contains(stdout, "Diagnosis complete:") {
				t.Fatalf("diagnose: %v %s %s", err, stdout, stderr)
			}
			data, err := os.ReadFile(filepath.Join(output, "report.json"))
			if err != nil {
				t.Fatal(err)
			}
			var report diagnose.Report
			if err := json.Unmarshal(data, &report, json.RejectUnknownMembers(true)); err != nil {
				t.Fatal(err)
			}
			markdown, err := os.ReadFile(filepath.Join(output, "report.md"))
			if err != nil {
				t.Fatal(err)
			}
			if fixture == "diagnose-reschedule.hl7" && (len(report.Findings) != 1 || report.Findings[0].Classification != "hypothesis" || report.Findings[0].Window == "") {
				t.Fatalf("partial window overclaimed: %+v", report)
			}
			if fixture == "diagnose-booking.hl7" && (len(report.Findings) != 0 || report.NoFindings == "" || !strings.Contains(string(markdown), "not proof of correctness")) {
				t.Fatal("no-findings statement missing")
			}
			for _, finding := range report.Findings {
				if !strings.Contains(string(markdown), finding.Summary) || !strings.Contains(string(markdown), finding.RuleID) {
					t.Fatal("report formats disagree")
				}
			}
			if strings.Contains(stdout+stderr+string(data)+string(markdown), "SECRET-") || strings.Contains(stdout+stderr, casePath) {
				t.Fatal("diagnosis disclosed raw data or paths")
			}
			after, err := os.ReadFile(filepath.Join(casePath, "identity.sha256"))
			if err != nil || string(after) != string(before) {
				t.Fatal("diagnosis changed input case")
			}
			if runtime.GOOS != "windows" {
				for _, file := range []string{"report.json", "report.md"} {
					info, err := os.Stat(filepath.Join(output, file))
					if err != nil || info.Mode().Perm() != 0600 {
						t.Fatal("report permissions are not private")
					}
				}
			}
			if out, errout, err := run(t, "diagnose", casePath, "--output", output); err == nil || out != "" || !strings.Contains(errout, "destination must be new") {
				t.Fatal("diagnosis overwrote existing output")
			}
		})
	}
}

func TestDiagnoseConfigAndFailuresAreStrictAndPrivate(t *testing.T) {
	casePath := diagnoseCase(t, "diagnose-booking.hl7")
	configPath := filepath.Join(t.TempDir(), "SECRET-CONFIG.json")
	if err := os.WriteFile(configPath, []byte(`{"schema":"readmit-diagnose-config/v1","secret":"SECRET-PATIENT"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"diagnose"}, {"diagnose", casePath}, {"diagnose", "SECRET-MISSING", "--output", filepath.Join(t.TempDir(), "new")},
		{"diagnose", casePath, "--output", filepath.Join(t.TempDir(), "new"), "--config", configPath},
		{"diagnose", casePath, "--output", filepath.Join(t.TempDir(), "new"), "--config", ""},
	} {
		stdout, stderr, err := run(t, args...)
		if err == nil || stdout != "" || stderr == "" || len(stderr) > 300 || strings.Contains(stderr, "SECRET") || strings.Contains(stderr, casePath) {
			t.Fatalf("unsafe diagnostic: %v %q %q", err, stdout, stderr)
		}
	}
	config := diagnose.DefaultConfig()
	config.Profile = "future-profile"
	data, _ := json.Marshal(config)
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "reports")
	if _, stderr, err := run(t, "diagnose", casePath, "--output", output, "--config", configPath); err != nil {
		t.Fatalf("unsupported profile not reported: %v %s", err, stderr)
	}
	data, err := os.ReadFile(filepath.Join(output, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "unsupported_profile") {
		t.Fatal("unsupported profile disappeared")
	}
}

func TestDiagnoseTamperedBundleProducesNoReports(t *testing.T) {
	casePath := diagnoseCase(t, "diagnose-booking.hl7")
	if err := os.WriteFile(filepath.Join(casePath, "payloads", "s0001-e000001.bin"), []byte("SECRET-TAMPER"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "reports")
	stdout, stderr, err := run(t, "diagnose", casePath, "--output", output)
	if err == nil || stdout != "" || strings.Contains(stderr, "SECRET") {
		t.Fatal("tampered evidence rendered")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("tampered evidence produced partial output")
	}
}

func TestDiagnoseOutputCannotMutateInputBundle(t *testing.T) {
	casePath := diagnoseCase(t, "diagnose-booking.hl7")
	for _, output := range []string{filepath.Join(casePath, "report"), filepath.Join(casePath, "payloads", "report")} {
		stdout, stderr, err := run(t, "diagnose", casePath, "--output", output)
		if err == nil || stdout != "" || !strings.Contains(stderr, "outside the immutable input case") {
			t.Fatalf("nested report output allowed: %v %q %q", err, stdout, stderr)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("nested report directory created")
		}
	}
	if runtime.GOOS != "windows" {
		alias := filepath.Join(t.TempDir(), "case-payload-alias")
		if err := os.Symlink(filepath.Join(casePath, "payloads"), alias); err != nil {
			t.Fatal(err)
		}
		if stdout, stderr, err := run(t, "diagnose", casePath, "--output", filepath.Join(alias, "report")); err == nil || stdout != "" || !strings.Contains(stderr, "outside the immutable input case") {
			t.Fatal("symlinked nested report output allowed")
		}
	}
	if _, stderr, err := run(t, "timeline", casePath); err != nil {
		t.Fatalf("refused output changed input: %v %s", err, stderr)
	}
}

func TestDiagnoseImportedWireProfilesArePrivateAndVisibleInBothReports(t *testing.T) {
	for _, repeated := range []bool{false, true} {
		raw, err := os.ReadFile("../testdata/fixtures/diagnose-unsupported-profile.hl7")
		if err != nil {
			t.Fatal(err)
		}
		wantFields := []string{"MSH-21[1]"}
		if repeated {
			raw = []byte(strings.Replace(string(raw), "SECRET-PROFILE^SECRET-VENDOR", "SECRET-FIRST^SECRET-A~SECRET-SECOND^SECRET-B", 1))
			wantFields = append(wantFields, "MSH-21[2]")
		}
		dir := t.TempDir()
		source := filepath.Join(dir, "input.hl7")
		casePath := filepath.Join(dir, "case")
		output := filepath.Join(dir, "report")
		if err := os.WriteFile(source, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, stderr, err := run(t, "capture", source, "--output", casePath); err != nil {
			t.Fatalf("capture: %v %s", err, stderr)
		}
		stdout, stderr, err := run(t, "diagnose", casePath, "--output", output)
		if err != nil || stderr != "" {
			t.Fatalf("diagnose: %v %s", err, stderr)
		}
		data, err := os.ReadFile(filepath.Join(output, "report.json"))
		if err != nil {
			t.Fatal(err)
		}
		markdown, err := os.ReadFile(filepath.Join(output, "report.md"))
		if err != nil {
			t.Fatal(err)
		}
		var report diagnose.Report
		if err := json.Unmarshal(data, &report); err != nil {
			t.Fatal(err)
		}
		if len(report.Unsupported) != len(wantFields) || len(report.Findings) != 0 {
			t.Fatalf("unsupported wire profiles missing: %+v", report)
		}
		for i, field := range wantFields {
			item := report.Unsupported[i]
			if item.Code != "unsupported_message_profile" || item.Field != field || item.Occurrence != "s0001-e000001" || !strings.Contains(string(markdown), field) || !strings.Contains(string(markdown), item.Code) {
				t.Fatal("JSON/Markdown wire profile references disagree")
			}
		}
		if strings.Contains(stdout+stderr+string(data)+string(markdown), "SECRET") {
			t.Fatal("wire profile declaration leaked into report or console")
		}
	}
}

func TestDiagnoseLifecycleRulesetIsSelectedThroughTheConfigurationFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "input.hl7")
	raw, err := os.ReadFile("../testdata/fixtures/diagnose-admit.hl7")
	if err != nil {
		t.Fatal(err)
	}
	discharge := strings.NewReplacer("A01", "A03", "DIAGNOSE-ADMIT", "DIAGNOSE-DISCHARGE", "VISIT-001", "VISIT-002").Replace(string(raw))
	if err := os.WriteFile(source, []byte(discharge), 0600); err != nil {
		t.Fatal(err)
	}
	casePath := filepath.Join(dir, "case")
	if _, stderr, err := run(t, "capture", source, "--output", casePath); err != nil {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	configPath := filepath.Join(dir, "lifecycle.json")
	configData, err := json.Marshal(diagnose.LifecycleConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, configData, 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "report")
	stdout, stderr, err := run(t, "diagnose", casePath, "--output", output, "--config", configPath)
	if err != nil || stderr != "" || !strings.Contains(stdout, diagnose.LifecycleRuleset) {
		t.Fatalf("diagnose: %v %q %q", err, stdout, stderr)
	}
	data, err := os.ReadFile(filepath.Join(output, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	markdown, err := os.ReadFile(filepath.Join(output, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	var report diagnose.Report
	if err := json.Unmarshal(data, &report, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if report.Schema != diagnose.Schema || report.Profile != diagnose.LifecycleProfile || len(report.Findings) != 1 {
		t.Fatalf("lifecycle ruleset not applied: %+v", report)
	}
	finding := report.Findings[0]
	if finding.RuleID != diagnose.VisitNotObserved || finding.Classification != "hypothesis" || finding.Window != report.Window.Description {
		t.Fatalf("unbounded lifecycle finding: %+v", finding)
	}
	if !strings.Contains(string(markdown), finding.Summary) || !strings.Contains(string(markdown), finding.Window) {
		t.Fatal("report formats disagree")
	}
	// The default SIU contract keeps its own meaning over the same evidence.
	defaultOutput := filepath.Join(dir, "siu-report")
	if _, stderr, err := run(t, "diagnose", casePath, "--output", defaultOutput); err != nil {
		t.Fatalf("default diagnosis: %v %s", err, stderr)
	}
	defaultData, err := os.ReadFile(filepath.Join(defaultOutput, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(defaultData), "unsupported_message_type") || strings.Contains(string(defaultData), diagnose.LifecycleRuleset) {
		t.Fatal("default contract interpreted ADT evidence")
	}
	if strings.Contains(stdout+stderr+string(data)+string(markdown), "VISIT-002") || strings.Contains(stdout+stderr, casePath) {
		t.Fatal("lifecycle diagnosis disclosed identifiers or paths")
	}
}
