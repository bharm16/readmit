package suite_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"github.com/bharm16/readmit/internal/expectation"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/suite"
)

func coveragePolicy(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "suite.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	path := filepath.Join(t.TempDir(), "coverage.json")
	specRaw, err := os.ReadFile(filepath.Join(dir, "booking-one.json"))
	if err != nil {
		t.Fatal(err)
	}
	specSum := sha256.Sum256(specRaw)
	write(t, path, suite.CoverageDocument{Schema: suite.CoverageSchema, SuiteSHA256: hex.EncodeToString(sum[:]), Specifications: []suite.CoverageSpecification{{Job: "booking-one", SHA256: hex.EncodeToString(specSum[:])}}, Requirements: []suite.Requirement{{ID: "accept-booking", Jobs: []string{"booking-one"}}, {ID: "downstream", Jobs: []string{}}}, Exclusions: []suite.Exclusion{}})
	return path
}
func TestCoverageDeclaredDenominatorAndQuarantineNeverPass(t *testing.T) {
	dir, _ := fixture(t, peer(t, func(c net.Conn) { ack(c, "AA") }))
	out := filepath.Join(dir, "out")
	if _, err := suite.Run(t.Context(), filepath.Join(dir, "suite.json"), "east", out); err != nil {
		t.Fatal(err)
	}
	path := coveragePolicy(t, out)
	now := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	report, err := suite.AssessCoverage(t.Context(), out, path, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if report.Denominator != 2 || report.Passed != 1 || report.Percent != 50 || report.Requirements[1].State != "uncovered" || report.Jobs[0].Execution != "passed" {
		t.Fatalf("%+v", report)
	}
	raw, _ := os.ReadFile(path)
	doc, err := suite.DecodeCoverage(raw)
	if err != nil {
		t.Fatal(err)
	}
	doc.Exclusions = []suite.Exclusion{{Job: "booking-one", State: "quarantined", Reason: "unstable fixture", Expires: "2026-09-18T00:00:00Z"}}
	write(t, path, doc)
	report, err = suite.AssessCoverage(t.Context(), out, path, nil, now)
	if err != nil || report.Passed != 0 || report.Jobs[0].Execution != "passed" || report.Jobs[0].Exclusion != "quarantined" || !report.Jobs[0].Expired {
		t.Fatalf("%+v %v", report, err)
	}
}
func TestCoverageCancelledSuiteShowsSkippedAndMissingReportStaysUnknown(t *testing.T) {
	dir, _ := fixture(t, "127.0.0.1:1")
	out := filepath.Join(dir, "out")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := suite.Run(ctx, filepath.Join(dir, "suite.json"), "east", out); err != nil {
		t.Fatal(err)
	}
	path := coveragePolicy(t, out)
	report, err := suite.AssessCoverage(t.Context(), out, path, nil, time.Now())
	if err != nil || report.Jobs[0].Execution != "skipped" || report.Jobs[0].Reason == "" || report.Passed != 0 {
		t.Fatalf("%+v %v", report, err)
	}
	if err = os.Remove(filepath.Join(out, "report.json")); err != nil {
		t.Fatal(err)
	}
	report, err = suite.AssessCoverage(t.Context(), out, path, nil, time.Now())
	if err != nil || report.Jobs[0].Execution != "unknown" || report.Passed != 0 {
		t.Fatalf("%+v %v", report, err)
	}
}

func TestCoverageRefusesUnknownDuplicateNullAndMissingDeclarations(t *testing.T) {
	dir, _ := fixture(t, "127.0.0.1:1")
	out := filepath.Join(dir, "out")
	if _, err := suite.Prepare(filepath.Join(dir, "suite.json"), "east", out); err != nil {
		t.Fatal(err)
	}
	path := coveragePolicy(t, out)
	valid, _ := os.ReadFile(path)
	for _, kind := range []string{"unknown", "duplicate", "null", "missing", "nested-unknown", "nested-missing", "nested-null", "empty-denominator", "bad-state", "missing-expiry", "bad-expiry", "duplicate-requirement", "duplicate-job", "duplicate-exclusion", "wrong-suite", "unknown-job", "unknown-exclusion"} {
		t.Run(kind, func(t *testing.T) {
			var obj map[string]any
			_ = json.Unmarshal(valid, &obj)
			requirement := obj["requirements"].([]any)[0].(map[string]any)
			exclusion := map[string]any{"job": "booking-one", "state": "disabled", "reason": "fixture unavailable", "expires": "2026-09-20T00:00:00Z"}
			raw := valid
			switch kind {
			case "unknown":
				obj["silent"] = true
			case "duplicate":
				raw = bytes.Replace(valid, []byte(`"schema":`), []byte(`"schema":"readmit-suite-coverage/v1","schema":`), 1)
			case "null":
				obj["exclusions"] = nil
			case "missing":
				delete(obj, "requirements")
			case "nested-unknown":
				requirement["silent"] = true
			case "nested-missing":
				delete(requirement, "jobs")
			case "nested-null":
				requirement["jobs"] = nil
			case "empty-denominator":
				obj["requirements"] = []any{}
			case "bad-state":
				exclusion["state"] = "passed"
				obj["exclusions"] = []any{exclusion}
			case "missing-expiry":
				delete(exclusion, "expires")
				obj["exclusions"] = []any{exclusion}
			case "bad-expiry":
				exclusion["expires"] = "tomorrow"
				obj["exclusions"] = []any{exclusion}
			case "duplicate-requirement":
				obj["requirements"] = []any{requirement, requirement}
			case "duplicate-job":
				requirement["jobs"] = []string{"booking-one", "booking-one"}
			case "duplicate-exclusion":
				obj["exclusions"] = []any{exclusion, exclusion}
			case "wrong-suite":
				obj["suite_sha256"] = strings.Repeat("0", 64)
			case "unknown-job":
				requirement["jobs"] = []string{"ghost"}
			case "unknown-exclusion":
				exclusion["job"] = "ghost"
				obj["exclusions"] = []any{exclusion}
			}
			if kind != "duplicate" {
				raw, _ = json.Marshal(obj)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := suite.AssessCoverage(t.Context(), out, path, nil, time.Now()); err == nil {
				t.Fatal("invalid declaration accepted")
			}
		})
	}
}

func TestCoverageFlakinessUsesRetainedComparableRuns(t *testing.T) {
	var mu sync.Mutex
	code := "AE"
	dir, _ := fixture(t, peer(t, func(c net.Conn) { mu.Lock(); selected := code; mu.Unlock(); ack(c, selected) }))
	previous := filepath.Join(dir, "previous")
	current := filepath.Join(dir, "current")
	if _, err := suite.Run(t.Context(), filepath.Join(dir, "suite.json"), "east", previous); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	code = "AA"
	mu.Unlock()
	if _, err := suite.Run(t.Context(), filepath.Join(dir, "suite.json"), "east", current); err != nil {
		t.Fatal(err)
	}
	path := coveragePolicy(t, current)
	report, err := suite.AssessCoverage(t.Context(), current, path, []string{previous}, time.Now())
	if err != nil || report.Passed != 0 || report.Jobs[0].Execution != "passed" || report.Jobs[0].Stability.State != "possible_flakiness" || report.Jobs[0].Stability.Failures != 1 || report.Jobs[0].Stability.Passes != 1 {
		t.Fatalf("%+v %v", report, err)
	}
	for _, history := range [][]string{{current}, {previous, previous}} {
		if _, err = suite.AssessCoverage(t.Context(), current, path, history, time.Now()); err == nil {
			t.Fatal("duplicate history accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = suite.AssessCoverage(ctx, current, path, nil, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCoveragePublicCLIAndAllExclusions(t *testing.T) {
	dir, _ := fixture(t, peer(t, func(c net.Conn) { ack(c, "AA") }))
	out := filepath.Join(dir, "out")
	if _, err := suite.Run(t.Context(), filepath.Join(dir, "suite.json"), "east", out); err != nil {
		t.Fatal(err)
	}
	path := coveragePolicy(t, out)
	raw, _ := os.ReadFile(path)
	doc, _ := suite.DecodeCoverage(raw)
	doc.Requirements = doc.Requirements[:1]
	for _, state := range []string{"none", "skipped", "unsupported", "quarantined", "disabled"} {
		t.Run(state, func(t *testing.T) {
			doc.Exclusions = []suite.Exclusion{}
			if state != "none" {
				doc.Exclusions = []suite.Exclusion{{Job: "booking-one", State: state, Reason: "operator reason", Expires: "2026-09-20T00:00:00Z"}}
			}
			write(t, path, doc)
			var outJSON, stderr bytes.Buffer
			args := []string{"suite", "coverage", out, "--requirements", path, "--at", "2026-09-19T00:00:00Z", "--json"}
			err := licensedCLI(t, args, &outJSON, &stderr)
			if (err == nil) != (state == "none") {
				t.Fatalf("%s %v", state, err)
			}
			var report suite.CoverageReport
			if err = json.Unmarshal(outJSON.Bytes(), &report); err != nil {
				t.Fatalf("%v %s", err, outJSON.String())
			}
			if len(report.Jobs) != 1 || report.Jobs[0].Execution != "passed" || report.Jobs[0].Exclusion != state {
				t.Fatalf("%+v", report)
			}
			outJSON.Reset()
			_ = licensedCLI(t, args[:len(args)-1], &outJSON, &stderr)
			if !strings.Contains(outJSON.String(), "Declared requirement coverage:") || !strings.Contains(outJSON.String(), "execution=passed") {
				t.Fatal(outJSON.String())
			}
		})
	}
}

func TestCoverageRefusesTamperedSuiteAndRetainedEvidence(t *testing.T) {
	for _, kind := range []string{"selection", "queue", "generated-spec", "result", "report", "report-missing-job", "symlink-policy"} {
		t.Run(kind, func(t *testing.T) {
			dir, _ := fixture(t, peer(t, func(c net.Conn) { ack(c, "AA") }))
			out := filepath.Join(dir, "out")
			if _, err := suite.Run(t.Context(), filepath.Join(dir, "suite.json"), "east", out); err != nil {
				t.Fatal(err)
			}
			path := coveragePolicy(t, out)
			name := ""
			switch kind {
			case "selection":
				name = "selection.json"
			case "queue":
				name = "queue.json"
			case "generated-spec":
				name = "booking-one.json"
			case "result":
				name = "runs/booking-one/result/result.json"
			case "report", "report-missing-job":
				name = "report.json"
			case "symlink-policy":
				link := filepath.Join(dir, "policy-link")
				if err := os.Symlink(path, link); err != nil {
					t.Skip(err)
				}
				path = link
			}
			if name != "" {
				raw, _ := os.ReadFile(filepath.Join(out, name))
				var obj map[string]any
				_ = json.Unmarshal(raw, &obj)
				switch kind {
				case "selection":
					obj["environment"] = "other"
				case "queue":
					obj["parallelism"] = 16
				case "generated-spec":
					obj["name"] = "another"
				case "result":
					obj["status"] = "pass"
					obj["error_class"] = "corrupt"
				case "report":
					obj["executed"] = 0
				case "report-missing-job":
					obj["jobs"] = []any{}
				}
				write(t, filepath.Join(out, name), obj)
			}
			if _, err := suite.AssessCoverage(t.Context(), out, path, nil, time.Now()); err == nil {
				t.Fatal("tampered input accepted")
			}
		})
	}
}

func TestCoverageRejectsDeclaredEnvironmentRelabelAndCoherentTransplant(t *testing.T) {
	for _, kind := range []string{"valid-environment", "coherent-transplant", "missing-pin", "changed-pin-job", "unmapped-exclusion"} {
		t.Run(kind, func(t *testing.T) {
			dir, doc := fixture(t, peer(t, func(c net.Conn) { ack(c, "AA") }))
			doc.Environments = append(doc.Environments, suite.Environment{ID: "west", Site: "hospital-b", Bindings: []suite.Binding{{Parameter: "interface", Target: "west.json"}}})
			write(t, filepath.Join(dir, "suite.json"), doc)
			out := filepath.Join(dir, "out")
			if _, err := suite.Run(t.Context(), filepath.Join(dir, "suite.json"), "east", out); err != nil {
				t.Fatal(err)
			}
			path := coveragePolicy(t, out)
			switch kind {
			case "valid-environment":
				write(t, filepath.Join(out, "selection.json"), suite.Selection{Schema: suite.SelectionSchema, Suite: doc.ID, Environment: "west", Site: "hospital-b"})
			case "coherent-transplant":
				other, _ := fixture(t, peer(t, func(c net.Conn) { ack(c, "AA") }))
				otherOut := filepath.Join(other, "out")
				if _, err := suite.Run(t.Context(), filepath.Join(other, "suite.json"), "east", otherOut); err != nil {
					t.Fatal(err)
				}
				raw, err := os.ReadFile(filepath.Join(otherOut, "booking-one.json"))
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(out, "booking-one.json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
				destination := filepath.Join(out, "runs", "booking-one")
				if err = os.RemoveAll(destination); err != nil {
					t.Fatal(err)
				}
				if err = os.CopyFS(destination, os.DirFS(filepath.Join(otherOut, "runs", "booking-one"))); err != nil {
					t.Fatal(err)
				}
			case "missing-pin", "changed-pin-job", "unmapped-exclusion":
				raw, _ := os.ReadFile(path)
				policy, _ := suite.DecodeCoverage(raw)
				if kind == "missing-pin" {
					policy.Specifications = nil
				} else if kind == "changed-pin-job" {
					policy.Specifications[0].Job = "ghost"
				} else {
					policy.Requirements = []suite.Requirement{{ID: "unmapped", Jobs: []string{}}}
					policy.Exclusions = []suite.Exclusion{{Job: "booking-one", State: "disabled", Reason: "operator", Expires: "2026-09-20T00:00:00Z"}}
				}
				write(t, path, policy)
			}
			report, err := suite.AssessCoverage(t.Context(), out, path, nil, time.Now())
			if kind == "unmapped-exclusion" {
				if err != nil || report.Passed != 0 || len(report.Jobs) != 1 || report.Jobs[0].Eligible {
					t.Fatalf("%+v %v", report, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("accepted %s: %+v", kind, report)
			}
		})
	}
}

func FuzzDecodeCoverage(f *testing.F) {
	f.Add([]byte(`{"schema":"readmit-suite-coverage/v1","suite_sha256":"0000000000000000000000000000000000000000000000000000000000000000","specifications":[{"job":"booking-one","sha256":"0000000000000000000000000000000000000000000000000000000000000000"}],"requirements":[{"id":"booking","jobs":["booking-one"]}],"exclusions":[]}`))
	f.Fuzz(func(t *testing.T, raw []byte) { _, _ = suite.DecodeCoverage(raw) })
}

func TestCoverageInterruptedJournalNeverInheritsPassingResult(t *testing.T) {
	dir, _ := fixture(t, peer(t, func(c net.Conn) { ack(c, "AA") }))
	out := filepath.Join(dir, "out")
	if _, err := suite.Run(t.Context(), filepath.Join(dir, "suite.json"), "east", out); err != nil {
		t.Fatal(err)
	}
	path := coveragePolicy(t, out)
	journal := filepath.Join(out, "runs", "booking-one", "journal.jsonl")
	f, err := os.OpenFile(journal, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString(`{"sequence":`); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(out, "report.json")); err != nil {
		t.Fatal(err)
	}
	report, err := suite.AssessCoverage(t.Context(), out, path, nil, time.Now())
	if err != nil || report.Passed != 0 || report.Jobs[0].Eligible || report.Jobs[0].Execution == "passed" {
		t.Fatalf("%+v %v", report, err)
	}
}

func TestCoverageReadsApprovedSuiteWithoutChangingApproval(t *testing.T) {
	dir, _, release := releasedFixture(t)
	raw, err := os.ReadFile(filepath.Join(dir, "east.json"))
	if err != nil {
		t.Fatal(err)
	}
	var target map[string]any
	if err = json.Unmarshal(raw, &target); err != nil {
		t.Fatal(err)
	}
	target["address"] = peer(t, func(c net.Conn) { ack(c, "AA") })
	write(t, filepath.Join(dir, "east.json"), target)
	out := filepath.Join(dir, "out")
	var stdout, stderr bytes.Buffer
	args := []string{"suite", "run", filepath.Join(dir, "suite.json"), "--environment", "east", "--output", out, "--releases", filepath.Join(dir, "releases.json"), "--send", "--json"}
	if err = licensedCLI(t, args, &stdout, &stderr); err != nil {
		t.Fatalf("%v %s", err, stderr.String())
	}
	policy := coveragePolicy(t, out)
	report, err := suite.AssessCoverage(t.Context(), out, policy, nil, time.Now())
	if err != nil || report.Passed != 1 || !report.Jobs[0].Eligible {
		t.Fatalf("%+v %v", report, err)
	}
	retained, err := expectation.Read(filepath.Join(out, "release-booking.json"))
	if err != nil || retained.Identity() != release.Identity() {
		t.Fatalf("approval changed: %v", err)
	}
}
