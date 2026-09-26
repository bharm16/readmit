package suite_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func gateFixture(t *testing.T) (string, suite.GatePolicy) {
	return gateFixtureACK(t, "AA")
}
func gateFixtureACK(t *testing.T, currentACK string) (string, suite.GatePolicy) {
	t.Helper()
	dir, _, _ := releasedFixture(t)
	raw, _ := os.ReadFile(filepath.Join(dir, "east.json"))
	var target map[string]any
	json.Unmarshal(raw, &target)
	var current atomic.Bool
	target["address"] = peer(t, func(c net.Conn) {
		code := "AA"
		if current.Load() {
			code = currentACK
		}
		ack(c, code)
	})
	write(t, filepath.Join(dir, "east.json"), target)
	p := approveFixture(t, dir)
	for _, name := range []string{"baseline", "current"} {
		current.Store(name == "current")
		r := suite.RunCI(t.Context(), suite.CIRequest{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: filepath.Join(dir, name), Releases: filepath.Join(dir, "releases.json"), Promotion: filepath.Join(dir, "promotion.json"), PromotionIdentity: p.Identity(), Revision: "fixture-v1"})
		if r.ExitCode != 0 && !(name == "current" && currentACK != "AA") {
			t.Fatal(r)
		}
	}
	coverageRaw, _ := os.ReadFile(coveragePolicy(t, filepath.Join(dir, "current")))
	coverage, e := suite.DecodeCoverage(coverageRaw)
	if e != nil {
		t.Fatal(e)
	}
	coverage.Requirements = coverage.Requirements[:1]
	baseline, e := durablerun.Open(filepath.Join(dir, "baseline", "runs", "booking-one"))
	if e != nil {
		t.Fatal(e)
	}
	policy := suite.GatePolicy{Schema: suite.GatePolicySchema, Environment: "east", Revision: "fixture-v1", Engine: engine.Version(), Promotion: p.Identity(), Coverage: coverage, Baseline: []suite.CoverageSpecification{{Job: "booking-one", SHA256: baseline.ResultIdentity}}, MaxBytes: 256 << 20, RetainUntil: "2036-01-01T00:00:00Z", Approver: "reviewer", Rationale: "synthetic baseline reviewed"}
	write(t, filepath.Join(dir, "policy.json"), policy)
	return dir, policy
}
func TestGatePublicRetainsAndReassessesWithoutOriginalInputs(t *testing.T) {
	t.Parallel()
	dir, p := gateFixture(t)
	output := filepath.Join(t.TempDir(), "retained")
	var stdout, stderr bytes.Buffer
	err := licensedCLI(t, []string{"suite", "gate", filepath.Join(dir, "current"), "--baseline", filepath.Join(dir, "baseline"), "--policy", filepath.Join(dir, "policy.json"), "--policy-identity", p.Identity(), "--output", output}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err, stderr.String(), stdout.String())
	}
	r, e := suite.DecodeGateReport(stdout.Bytes())
	if e != nil || r.State != "passed" || r.Retention != "retained" {
		t.Fatalf("%+v %v", r, e)
	}
	if e = os.RemoveAll(dir); e != nil {
		t.Fatal(e)
	}
	r = suite.VerifyGate(t.Context(), output, p.Identity(), time.Now().UTC())
	if r.State != "passed" {
		t.Fatalf("retained gate depends on original source: %+v", r)
	}
}

func TestGateRefusesUnreviewedUnknownExcludedAndAlteredEvidence(t *testing.T) {
	t.Parallel()
	// The matrix changes retained evidence, not execution. Run the two
	// fixtures once, then give each mutation its own complete copy and policy.
	original, _ := gateFixture(t)
	copyFixture := func(t *testing.T) (string, suite.GatePolicy) {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "fixture")
		if err := os.CopyFS(dir, os.DirFS(original)); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "policy.json"))
		if err != nil {
			t.Fatal(err)
		}
		p, err := suite.DecodeGatePolicy(raw)
		if err != nil {
			t.Fatal(err)
		}
		return dir, p
	}
	// A relocated unchanged copy must pass, so later refusals cannot be
	// explained by broken fixture paths or identities after the copy.
	t.Run("unchanged-copy", func(t *testing.T) {
		dir, p := copyFixture(t)
		output := filepath.Join(t.TempDir(), "retained")
		r := suite.RetainGate(t.Context(), filepath.Join(dir, "current"), filepath.Join(dir, "baseline"), filepath.Join(dir, "policy.json"), p.Identity(), output, time.Now().UTC())
		if r.ExitCode != 0 || r.State != "passed" {
			t.Fatalf("copied fixture refused: %+v", r)
		}
		if verified := suite.VerifyGate(t.Context(), output, p.Identity(), time.Now().UTC()); verified.State != "passed" {
			t.Fatalf("copied fixture retained invalid evidence: %+v", verified)
		}
	})
	for _, kind := range []string{"policy-pin", "promotion-input", "promotion-job", "oversized", "baseline-pin", "environment", "revision", "engine", "approval", "quarantine", "expired-quarantine", "uncovered", "missing-report", "missing-engine", "missing-result", "altered-bytes", "symlink", "expired", "cancel", "existing-output"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			dir, p := copyFixture(t)
			pin := p.Identity()
			current := filepath.Join(dir, "current")
			baseline := filepath.Join(dir, "baseline")
			output := filepath.Join(t.TempDir(), "out")
			ctx := t.Context()
			switch kind {
			case "policy-pin":
				pin = strings.Repeat("a", 64)
			case "promotion-input", "promotion-job":
				raw, _ := os.ReadFile(filepath.Join(current, "promotion.json"))
				approval, e := suite.DecodePromotion(raw)
				if e != nil {
					t.Fatal(e)
				}
				if kind == "promotion-job" {
					for _, base := range []string{current, baseline} {
						if e := os.CopyFS(filepath.Join(base, "runs", "other"), os.DirFS(filepath.Join(base, "runs", "booking-one"))); e != nil {
							t.Fatal(e)
						}
					}
					approval.Review.Jobs[0].Job = "other"
				} else {
					approval.Review.Jobs[0].SHA256 = strings.Repeat("a", 64)
				}
				approval.Review.Commitment = approval.Review.Identity()
				approval.Reviewed = approval.Review.Commitment
				p.Promotion = approval.Identity()
				write(t, filepath.Join(current, "promotion.json"), approval)
				write(t, filepath.Join(baseline, "promotion.json"), approval)
			case "oversized":
				f, e := os.Create(filepath.Join(current, "large"))
				if e != nil {
					t.Fatal(e)
				}
				if e = f.Truncate(257 << 20); e != nil {
					t.Fatal(e)
				}
				f.Close()
			case "baseline-pin":
				p.Baseline[0].SHA256 = strings.Repeat("a", 64)
			case "environment":
				p.Environment = "west"
			case "revision":
				p.Revision = "other"
			case "engine":
				p.Engine = "other"
			case "approval":
				os.Remove(filepath.Join(current, "promotion.json"))
			case "quarantine", "expired-quarantine":
				p.Coverage.Exclusions = []suite.Exclusion{{Job: "booking-one", State: "quarantined", Reason: "PRIVATE", Expires: "2035-01-01T00:00:00Z"}}
				if kind == "expired-quarantine" {
					p.Coverage.Exclusions[0].Expires = "2020-01-01T00:00:00Z"
				}
			case "uncovered":
				p.Coverage.Requirements[0].Jobs = []string{}
			case "missing-report":
				os.Remove(filepath.Join(current, "report.json"))
			case "missing-engine":
				os.Remove(filepath.Join(current, "runs", "booking-one", "engine.json"))
			case "missing-result":
				os.RemoveAll(filepath.Join(current, "runs", "booking-one", "result"))
			case "altered-bytes":
				os.WriteFile(filepath.Join(current, "booking-one.json"), []byte(`{}`), 0600)
			case "symlink":
				if e := os.Symlink(filepath.Join(dir, "policy.json"), filepath.Join(current, "link")); e != nil {
					t.Skip(e)
				}
			case "expired":
				p.RetainUntil = "2020-01-01T00:00:00Z"
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "existing-output":
				os.Mkdir(output, 0700)
				os.WriteFile(filepath.Join(output, "sentinel"), []byte("unchanged"), 0600)
			}
			if kind != "policy-pin" {
				pin = p.Identity()
			}
			write(t, filepath.Join(dir, "policy.json"), p)
			r := suite.RetainGate(ctx, current, baseline, filepath.Join(dir, "policy.json"), pin, output, time.Now().UTC())
			if r.ExitCode == 0 || r.State == "passed" {
				t.Fatalf("%s passed: %+v", kind, r)
			}
			raw, _ := json.Marshal(r)
			if bytes.Contains(raw, []byte("PRIVATE")) {
				t.Fatal("private metadata leaked")
			}
			if _, e := suite.DecodeGateReport(raw); e != nil {
				t.Fatal(e, string(raw))
			}
			if kind == "existing-output" {
				raw, e := os.ReadFile(filepath.Join(output, "sentinel"))
				if e != nil || string(raw) != "unchanged" {
					t.Fatal("changed existing output")
				}
			}
		})
	}
}
func TestGateRetentionTamperMissingAndExpiryNeverPass(t *testing.T) {
	t.Parallel()
	dir, p := gateFixture(t)
	out := filepath.Join(t.TempDir(), "retained")
	now := time.Now().UTC()
	if r := suite.RetainGate(t.Context(), filepath.Join(dir, "current"), filepath.Join(dir, "baseline"), filepath.Join(dir, "policy.json"), p.Identity(), out, now); r.ExitCode != 0 {
		t.Fatal(r)
	}
	if r := suite.VerifyGate(t.Context(), out, p.Identity(), time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC)); r.Retention != "expired" || r.ExitCode == 0 {
		t.Fatal(r)
	}
	path := filepath.Join(out, "current", "ci.json")
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(path, []byte(`{"passed":true}`), 0600)
	if r := suite.VerifyGate(t.Context(), out, p.Identity(), time.Now().UTC()); r.ExitCode == 0 {
		t.Fatal("tampered snapshot passed")
	}
	os.WriteFile(path, raw, 0600)
	os.Remove(filepath.Join(out, "retention.json"))
	if r := suite.VerifyGate(t.Context(), out, p.Identity(), time.Now().UTC()); r.ExitCode == 0 {
		t.Fatal("partial snapshot passed")
	}
}
func TestGatePolicyStrictReader(t *testing.T) {
	t.Parallel()
	_, p := gateFixture(t)
	raw, _ := json.Marshal(p)
	for _, input := range [][]byte{bytes.Replace(raw, []byte(`"schema":"readmit-ci-gate-policy/v1"`), []byte(`"schema":"readmit-ci-gate-policy/v2"`), 1), bytes.Replace(raw, []byte(`"coverage":{`), []byte(`"coverage":{"unknown":1,`), 1), bytes.Replace(raw, []byte(`"approver":"reviewer"`), []byte(`"approver":null`), 1), bytes.Replace(raw, []byte(`"approver":"reviewer"`), []byte(`"approver":"reviewer","approver":"reviewer"`), 1), bytes.Replace(raw, []byte(`"approver":"reviewer",`), nil, 1)} {
		if _, e := suite.DecodeGatePolicy(input); e == nil {
			t.Fatal("accepted invalid policy", string(input))
		}
	}
}

func TestGateActualFailedExecutionCannotPass(t *testing.T) {
	t.Parallel()
	dir, p := gateFixtureACK(t, "AE")
	out := filepath.Join(t.TempDir(), "retained")
	r := suite.RetainGate(t.Context(), filepath.Join(dir, "current"), filepath.Join(dir, "baseline"), filepath.Join(dir, "policy.json"), p.Identity(), out, time.Now().UTC())
	if r.State != "failed" || r.ExitCode != 1 || r.Baseline != "failed" {
		t.Fatalf("%+v", r)
	}
	if r = suite.VerifyGate(t.Context(), out, p.Identity(), time.Now().UTC()); r.State != "failed" {
		t.Fatal(r)
	}
}
func TestGateCancelledSuiteRetainsSkippedEvidence(t *testing.T) {
	t.Parallel()
	dir, p := gateFixture(t)
	current := filepath.Join(dir, "current")
	os.RemoveAll(current)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e := suite.RunPromoted(ctx, filepath.Join(dir, "suite.json"), "east", current, filepath.Join(dir, "releases.json"), filepath.Join(dir, "promotion.json"), p.Promotion, p.Revision); e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(t.TempDir(), "retained")
	r := suite.RetainGate(t.Context(), current, filepath.Join(dir, "baseline"), filepath.Join(dir, "policy.json"), p.Identity(), out, time.Now().UTC())
	if r.ExitCode != 2 || r.State != "unknown" {
		t.Fatal(r)
	}
	if _, e := os.Stat(filepath.Join(out, "current", "report.json")); e != nil {
		t.Fatal("skipped evidence lost", e)
	}
	if r = suite.VerifyGate(t.Context(), out, p.Identity(), time.Now().UTC()); r.ExitCode != 2 {
		t.Fatal(r)
	}
}

func TestGateCollectorErrorCannotPassOrDisappearDuringRetention(t *testing.T) {
	t.Parallel()
	dir, doc := fixture(t, "127.0.0.1:1")
	const session = "0123456789abcdef0123456789abcdef"
	observationPath := filepath.Join(dir, "ledger.json")
	initial := observation.Snapshot{Schema: observation.Schema, Profile: observation.Profile, SessionID: session, Mode: observation.Fixed, Processed: []observation.Occurrence{}, Consistent: true, Records: []observation.Record{}}
	write(t, observationPath, initial)
	address := peer(t, func(c net.Conn) {
		reader, _ := mllp.NewReader(c, 1<<20)
		if _, e := reader.ReadFrame(); e != nil {
			return
		}
		final := initial
		final.Processed = []observation.Occurrence{{OccurrenceID: "s0001-e000001", ControlID: "LISTEN-BOOK"}}
		raw, _ := observation.Encode(final)
		if e := os.WriteFile(observationPath, raw, 0600); e != nil {
			return
		}
		c.Write(mllp.Frame([]byte("MSH|^~\\&|FIXTURE|TEST|||20260101120000||ACK^S12|ACK|P|2.5.1\rMSA|AA|LISTEN-BOOK\rZRT|readmit-receipt/v1|" + session + "|s0001-e000001\r")))
	})
	raw, _ := os.ReadFile(filepath.Join(dir, "east.json"))
	var target map[string]any
	json.Unmarshal(raw, &target)
	target["address"] = address
	write(t, filepath.Join(dir, "east.json"), target)
	spec, e := testrunner.ReadSpec(filepath.Join(dir, "booking.json"))
	if e != nil {
		t.Fatal(e)
	}
	zero := 0
	spec.Setup.InitialState = "empty-ledger"
	spec.Observation = testrunner.Observation{Boundary: testrunner.LedgerBoundary, Path: "unbound"}
	spec.Assertions = []testrunner.Assertion{{ID: "count", Operator: "ledger_count", Expected: testrunner.Value{Count: &zero}}}
	write(t, filepath.Join(dir, "booking.json"), spec)
	doc.Environments[0].Bindings[0].Observation = "ledger.json"
	doc.Tables[0].Rows[0].Expected = map[string]testrunner.Value{}
	write(t, filepath.Join(dir, "suite.json"), doc)
	raw, _ = json.Marshal(spec)
	pins := []profileversion.Version{}
	review, e := expectation.Review("booking", raw, pins, nil, false)
	if e != nil {
		t.Fatal(e)
	}
	release, e := expectation.Approve("booking", raw, pins, nil, review.Identity, "reviewer", "collector fixture")
	if e != nil {
		t.Fatal(e)
	}
	if e = expectation.Save(filepath.Join(dir, "release.json"), release); e != nil {
		t.Fatal(e)
	}
	write(t, filepath.Join(dir, "releases.json"), suite.ReleaseReferences{Schema: suite.ReleasesSchema, Tests: []suite.ReleaseReference{{Test: "booking", Release: "release.json", Identity: release.Identity()}}})
	promotion := approveFixture(t, dir)
	for _, name := range []string{"baseline", "current"} {
		if name == "current" {
			os.WriteFile(observationPath, []byte(`{"schema":`), 0600)
		}
		r := suite.RunCI(t.Context(), suite.CIRequest{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: filepath.Join(dir, name), Releases: filepath.Join(dir, "releases.json"), Promotion: filepath.Join(dir, "promotion.json"), PromotionIdentity: promotion.Identity(), Revision: "fixture-v1"})
		if name == "baseline" && r.ExitCode != 0 || name == "current" && r.ExitCode != 2 {
			t.Fatal(name, r)
		}
	}
	result, e := testrunner.Open(filepath.Join(dir, "current", "runs", "booking-one", "result"))
	if e != nil || result.Result.ErrorClass != "initial_observation" {
		t.Fatal(result, e)
	}
	raw, _ = os.ReadFile(coveragePolicy(t, filepath.Join(dir, "current")))
	coverage, e := suite.DecodeCoverage(raw)
	if e != nil {
		t.Fatal(e)
	}
	coverage.Requirements = coverage.Requirements[:1]
	baseline, e := durablerun.Open(filepath.Join(dir, "baseline", "runs", "booking-one"))
	if e != nil {
		t.Fatal(e)
	}
	p := suite.GatePolicy{Schema: suite.GatePolicySchema, Environment: "east", Revision: "fixture-v1", Engine: engine.Version(), Promotion: promotion.Identity(), Coverage: coverage, Baseline: []suite.CoverageSpecification{{Job: "booking-one", SHA256: baseline.ResultIdentity}}, MaxBytes: 256 << 20, RetainUntil: "2036-01-01T00:00:00Z", Approver: "reviewer", Rationale: "reviewed collector fixture"}
	write(t, filepath.Join(dir, "policy.json"), p)
	out := filepath.Join(t.TempDir(), "retained")
	r := suite.RetainGate(t.Context(), filepath.Join(dir, "current"), filepath.Join(dir, "baseline"), filepath.Join(dir, "policy.json"), p.Identity(), out, time.Now().UTC())
	if r.ExitCode != 2 || r.State != "unknown" {
		t.Fatal(r)
	}
	os.RemoveAll(dir)
	if r = suite.VerifyGate(t.Context(), out, p.Identity(), time.Now().UTC()); r.ExitCode != 2 {
		t.Fatal(r)
	}
}

func FuzzDecodeGatePolicy(f *testing.F) {
	f.Add([]byte(`{"schema":"readmit-ci-gate-policy/v1"}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		p, e := suite.DecodeGatePolicy(raw)
		if e != nil {
			return
		}
		encoded, e := json.Marshal(p, json.Deterministic(true))
		if e != nil {
			t.Fatal(e)
		}
		again, e := suite.DecodeGatePolicy(encoded)
		if e != nil || again.Identity() != p.Identity() {
			t.Fatal("policy roundtrip differs")
		}
	})
}
func TestGateSummaryStrictReader(t *testing.T) {
	t.Parallel()
	valid := `{"schema":"readmit-ci-gate/v1","state":"passed","exit_code":0,"approval":"passed","pins":"passed","coverage":"passed","baseline":"passed","retention":"retained","target_revision":"operator_asserted"}`
	if _, e := suite.DecodeGateReport([]byte(valid)); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{strings.Replace(valid, `"pins":"passed"`, `"pins":"unknown"`, 1), strings.Replace(valid, `"retention":"retained"`, `"retention":"expired"`, 1), strings.Replace(valid, `"approval":"passed"`, `"approval":null`, 1), strings.Replace(valid, `"exit_code":0`, `"exit_code":2`, 1), strings.Replace(valid, `"exit_code":0`, `"exit_code":0,"extra":true`, 1), strings.Replace(valid, `"exit_code":0`, `"exit_code":0,"exit_code":0`, 1), strings.Replace(valid, `"exit_code":0,`, ``, 1)} {
		if _, e := suite.DecodeGateReport([]byte(raw)); e == nil {
			t.Fatal("invalid report accepted")
		}
	}
}

func TestGateFinalMetadataMustFitDeclaredRetentionBudget(t *testing.T) {
	t.Parallel()
	dir, p := gateFixture(t)
	control := filepath.Join(t.TempDir(), "control")
	current, baseline, policy := filepath.Join(dir, "current"), filepath.Join(dir, "baseline"), filepath.Join(dir, "policy.json")
	if r := suite.RetainGate(t.Context(), current, baseline, policy, p.Identity(), control, time.Now().UTC()); r.ExitCode != 0 {
		t.Fatal(r)
	}
	var dataBytes int64
	if e := filepath.WalkDir(control, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() || path == filepath.Join(control, "retention.json") || path == filepath.Join(control, "gate.json") {
			return nil
		}
		info, e := d.Info()
		if e == nil {
			dataBytes += info.Size()
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
	// The evidence fits; the several-kilobyte final inventory does not. The small
	// margin also covers the changed numeric budget in the policy's own bytes.
	p.MaxBytes = dataBytes + 512
	write(t, policy, p)
	out := filepath.Join(t.TempDir(), "limited")
	if r := suite.RetainGate(t.Context(), current, baseline, policy, p.Identity(), out, time.Now().UTC()); r.ExitCode == 0 {
		t.Fatal("passed before accounting for final metadata")
	}
	if _, e := os.Stat(filepath.Join(out, "retention.json")); e != nil {
		t.Fatal("did not exercise final metadata boundary", e)
	}
	if r := suite.VerifyGate(t.Context(), out, p.Identity(), time.Now().UTC()); r.ExitCode == 0 {
		t.Fatal("oversized snapshot verified")
	}
}
