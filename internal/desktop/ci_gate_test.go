package desktop_test

// The reviewed change gate the CI panel hands to a customer's pipeline and the
// retained snapshots it verifies, against the command line. The generated
// step is the unchanged `readmit suite gate`, run after `suite ci` without
// taking over its exit status, so an agent running the file the window wrote
// retains the verdict a hand-run gate retains over the same runs. The
// window's verification is suite.VerifyGate, the operation
// `readmit suite verify-gate` runs, so it answers the same summary for an
// intact, a tampered and a foreign snapshot. Nothing here sends: the fixture's
// target counts every connection.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testlicense"
)

// gatedSuite is one released and promoted suite whose baseline run a reviewer
// accepted privately, and the gate policy that pins it: everything a customer
// provisions on the agent before the first gated invocation.
type gatedSuite struct {
	dir, bin, license, suiteFile, coverage string
	releases, promotion, pin               string
	baseline, policy, policyPin            string
	code                                   *atomic.Value
	connections                            *atomic.Int64
}

// gatePeer answers every framed message with the ACK code the fixture holds
// now, and counts every connection it accepts.
func gatePeer(t *testing.T, code *atomic.Value, connections *atomic.Int64) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				reader, _ := mllp.NewReader(conn, 1<<20)
				if _, err := reader.ReadFrame(); err != nil {
					return
				}
				_, _ = fmt.Fprintf(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|%s|LISTEN-BOOK\r\x1c\r", code.Load().(string))
			}()
		}
	}()
	t.Cleanup(func() { listener.Close(); <-done })
	return listener.Addr().String()
}

func putJSON(t *testing.T, path string, document any) {
	t.Helper()
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
}

// exitOf runs a command and returns its exit status and standard output.
func exitOf(t *testing.T, cmd *exec.Cmd) (int, []byte) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if cmd.ProcessState == nil {
		t.Fatalf("%v did not start: %v", cmd.Args, err)
	}
	return cmd.ProcessState.ExitCode(), stdout.Bytes()
}

// gatedSuiteFixture authors the suite, releases its one test, approves its
// promotion to the east environment under the revision fixture-v1, runs the
// baseline once through the command line as `suite ci` with that promotion,
// and writes the gate policy pinning the baseline's result, the coverage
// declaration and the promotion.
func gatedSuiteFixture(t *testing.T, bin string) gatedSuite {
	t.Helper()
	code := &atomic.Value{}
	code.Store("AA")
	connections := &atomic.Int64{}
	dir := suiteFixtureDir(t, gatePeer(t, code, connections))
	fixture := gatedSuite{dir: dir, bin: bin, code: code, connections: connections, license: testlicense.New(t)}
	fixture.suiteFile = filepath.Join(dir, "suite.json")
	canonical := `{"schema":"readmit-suite/v1","id":"nightly","owner":"interop","tags":["siu"],"parallelism":1,"environments":[{"id":"east","site":"hospital-a","bindings":[{"parameter":"interface","target":"east.json"}]}],"tables":[{"id":"patients","rows":[{"id":"one","case":"case-one"}]}],"tests":[{"id":"booking","spec":"booking.json","owner":"scheduling","tags":["smoke"],"parameter":"interface","table":"patients","isolation":"shared","sequence":["s0001-e000001"]}]}`
	if err := os.WriteFile(fixture.suiteFile, []byte(canonical), 0600); err != nil {
		t.Fatal(err)
	}

	spec, err := os.ReadFile(filepath.Join(dir, "booking.json"))
	if err != nil {
		t.Fatal(err)
	}
	pins := []profileversion.Version{}
	review, err := expectation.Review("booking", spec, pins, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	release, err := expectation.Approve("booking", spec, pins, nil, review.Identity, "reviewer", "reviewed synthetic booking")
	if err != nil {
		t.Fatal(err)
	}
	if err := expectation.Save(filepath.Join(dir, "release.json"), release); err != nil {
		t.Fatal(err)
	}
	fixture.releases = filepath.Join(dir, "releases.json")
	putJSON(t, fixture.releases, suite.ReleaseReferences{Schema: suite.ReleasesSchema, Tests: []suite.ReleaseReference{{Test: "booking", Release: "release.json", Identity: release.Identity()}}})
	promotionReview, err := suite.ReviewPromotion(fixture.suiteFile, "east", fixture.releases, "fixture-v1")
	if err != nil {
		t.Fatal(err)
	}
	fixture.promotion = filepath.Join(dir, "promotion.json")
	approval, err := suite.ApprovePromotion(fixture.suiteFile, "east", fixture.releases, "fixture-v1", promotionReview.Identity(), "reviewer", "reviewed synthetic promotion", fixture.promotion)
	if err != nil {
		t.Fatal(err)
	}
	fixture.pin = approval.Identity()

	fixture.baseline = filepath.Join(dir, "baseline")
	if exit, out := exitOf(t, fixture.suiteCI(fixture.baseline, "")); exit != 0 {
		t.Fatalf("baseline suite ci exited %d: %s", exit, out)
	}
	job := filepath.Join(fixture.baseline, "runs", "booking-one")
	summary, err := durablerun.Open(job)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := durablerun.Engine(job)
	if err != nil {
		t.Fatal(err)
	}
	coverage := suite.CoverageDocument{
		Schema: suite.CoverageSchema, SuiteSHA256: fileDigest(t, filepath.Join(fixture.baseline, "suite.json")),
		Specifications: []suite.CoverageSpecification{{Job: "booking-one", SHA256: fileDigest(t, filepath.Join(fixture.baseline, "booking-one.json"))}},
		Requirements:   []suite.Requirement{{ID: "booking", Jobs: []string{"booking-one"}}},
		Exclusions:     []suite.Exclusion{},
	}
	fixture.coverage = filepath.Join(dir, "coverage.json")
	putJSON(t, fixture.coverage, coverage)
	policy := suite.GatePolicy{
		Schema: suite.GatePolicySchema, Environment: "east", Revision: "fixture-v1", Engine: pin.Engine,
		Promotion: fixture.pin, Coverage: coverage,
		Baseline: []suite.CoverageSpecification{{Job: "booking-one", SHA256: summary.ResultIdentity}},
		MaxBytes: 256 << 20, RetainUntil: "2036-01-01T00:00:00Z", Approver: "reviewer", Rationale: "synthetic baseline reviewed",
	}
	fixture.policy = filepath.Join(dir, "gate-policy.json")
	putJSON(t, fixture.policy, policy)
	fixture.policyPin = policy.Identity()
	return fixture
}

// suiteCI is the documented gated `suite ci` invocation run by hand, with or
// without the coverage declaration.
func (f gatedSuite) suiteCI(output, coverage string) *exec.Cmd {
	args := []string{"--operation-policy", f.license, "suite", "ci", f.suiteFile, "--environment", "east", "--output", output}
	if coverage != "" {
		args = append(args, "--requirements", coverage)
	}
	args = append(args, "--releases", f.releases, "--promotion", f.promotion, "--promotion-identity", f.pin, "--revision", "fixture-v1", "--send", "--deadline", "5m")
	return exec.Command(f.bin, args...)
}

// gate is `readmit suite gate` run by hand over one current run.
func (f gatedSuite) gate(current, output string) *exec.Cmd {
	return exec.Command(f.bin, "suite", "gate", current, "--baseline", f.baseline, "--policy", f.policy, "--policy-identity", f.policyPin, "--output", output)
}

// handoff is the window's form for this fixture with the change gate added.
func (f gatedSuite) handoff(integration, run, snapshot, output string) desktop.CIHandoffRequest {
	return desktop.CIHandoffRequest{
		Integration: integration, Binary: f.bin, OperationPolicy: f.license, SuiteFile: f.suiteFile,
		Environment: "east", RunDirectory: run, CoverageFile: f.coverage, Output: output,
		Gate: &desktop.CIGateStep{
			Releases: f.releases, Promotion: f.promotion, PromotionIdentity: f.pin, Revision: "fixture-v1",
			Baseline: f.baseline, Policy: f.policy, PolicyIdentity: f.policyPin, SnapshotDirectory: snapshot,
		},
	}
}

// provisioned reads the variables the handoff's checklist tells the
// administrator to provision, as they copy them onto the agent.
func provisioned(t *testing.T, document string) []string {
	t.Helper()
	var variables []string
	for _, line := range strings.Split(document, "\n") {
		name, value, found := strings.Cut(strings.TrimPrefix(line, "#   "), "=")
		if !found || !strings.HasPrefix(line, "#   ") || strings.ToUpper(name) != name {
			continue
		}
		value, _, _ = strings.Cut(value, " (a new path")
		variables = append(variables, name+"="+value)
	}
	if len(variables) != 14 {
		t.Fatalf("the gated checklist names %d variables, want 14: %s", len(variables), document)
	}
	return variables
}

// reviewedWorkflow is the part of a handoff the agent installs as-is.
func reviewedWorkflow(t *testing.T, document string) string {
	t.Helper()
	_, workflow, found := strings.Cut(document, "# --- reviewed workflow (install as-is) ---\n# \n")
	if !found {
		t.Fatalf("no reviewed workflow: %s", document)
	}
	return workflow
}

// TestTheGeneratedGateStepRetainsTheVerdictAHandRunGateRetains runs the POSIX
// workflow the window writes on an agent: `suite ci` with the approved
// promotion, then the unchanged `readmit suite gate`. Against a current run
// that passes, the gate passes; against one whose assertion fails, execution
// fails and the gate still retains its snapshot, and the workflow exits with
// the execution's status, not the gate's. Either way the retained summary is
// the one `readmit suite gate` retains by hand over the same runs, the
// window's verification of it answers what `readmit suite verify-gate`
// prints, and neither the gate nor the verification sends. The GitHub and
// Azure workflows run the same two commands as separate steps, the gate
// after the suite whether or not it failed. The gated POSIX workflow is the
// one docs/customer-ci.md documents, byte for byte.
func TestTheGeneratedGateStepRetainsTheVerdictAHandRunGateRetains(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX workflow runs on Linux and macOS agents")
	}
	bin := cliExecutable(t)
	for _, current := range []struct {
		code                 string
		execution, gateState string
		gateExit             int
		unverified           string
	}{
		{code: "AA", execution: "passed", gateState: "passed", gateExit: 0},
		// A failed assertion leaves the requested coverage requirement
		// unqualified, so execution is an error (exit 2); the gate proves the
		// behavioral change and fails (exit 1), and assesses neither the pins
		// nor the coverage after the change it proved.
		{code: "AE", execution: "error", gateState: "failed", gateExit: 1, unverified: "pins,coverage"},
	} {
		t.Run(current.code, func(t *testing.T) {
			fixture := gatedSuiteFixture(t, bin)
			fixture.code.Store(current.code)
			app := workspaceApp(t)
			run, snapshot := filepath.Join(fixture.dir, "current"), filepath.Join(fixture.dir, "retained")
			handoff := app.SaveCIHandoff(fixture.handoff("posix", run, snapshot, filepath.Join(t.TempDir(), "handoff.sh")))
			if handoff.State != desktop.Completed {
				t.Fatalf("handoff: %+v", handoff)
			}
			documented, err := os.ReadFile("../../docs/customer-ci.md")
			if err != nil {
				t.Fatal(err)
			}
			if workflow := reviewedWorkflow(t, handoff.Document); !strings.Contains(string(documented), "```sh\n"+workflow+"```\n") {
				t.Fatalf("the gated POSIX workflow is not the documented one:\n%s", workflow)
			}

			sent := fixture.connections.Load()
			script := exec.Command("sh", handoff.Output)
			script.Env = append(os.Environ(), provisioned(t, handoff.Document)...)
			exit, _ := exitOf(t, script)
			if got := fixture.connections.Load() - sent; got != 1 {
				t.Fatalf("the workflow opened %d connections; its one suite job opens one and the gate none", got)
			}
			ci, err := suite.DecodeCI(mustReadFile(t, filepath.Join(run, "ci.json")))
			if err != nil || ci.State != current.execution {
				t.Fatalf("execution: %+v %v", ci, err)
			}
			if exit != ci.ExitCode {
				t.Fatalf("the workflow exited %d; the suite's execution exited %d and the gate never replaces it", exit, ci.ExitCode)
			}
			retained := mustReadFile(t, filepath.Join(snapshot, "gate.json"))
			report, err := suite.DecodeGateReport(retained)
			if err != nil || report.State != current.gateState || report.ExitCode != current.gateExit {
				t.Fatalf("retained gate: %s %v", retained, err)
			}

			// The gate run by hand over the same runs retains the same verdict.
			byHand := filepath.Join(fixture.dir, "by-hand")
			handExit, printed := exitOf(t, fixture.gate(run, byHand))
			if handExit != current.gateExit || string(printed) != string(retained)+"\n" || string(mustReadFile(t, filepath.Join(byHand, "gate.json"))) != string(retained) {
				t.Fatalf("hand-run gate exited %d printing %s; the workflow retained %s", handExit, printed, retained)
			}

			// The window verifies the snapshot the workflow retained as
			// `readmit suite verify-gate` does.
			verified := app.VerifyCIGate(snapshot, fixture.policyPin)
			verifyExit, printed := exitOf(t, exec.Command(bin, "suite", "verify-gate", snapshot, "--policy-identity", fixture.policyPin))
			encoded, _ := json.Marshal(verified.Gate)
			if verified.State != desktop.Completed || verified.Gate == nil || string(printed) != string(retained)+"\n" || verifyExit != current.gateExit || !sameReport(t, encoded, retained) || strings.Join(verified.Unverified, ",") != current.unverified {
				t.Fatalf("window %+v %+v; verify-gate exited %d printing %s", verified, verified.Gate, verifyExit, printed)
			}
			if fixture.connections.Load()-sent != 1 {
				t.Fatal("the gate or its verification sent")
			}

			// The hosted integrations run the same two commands, the gate as
			// its own step after the suite whether or not the suite failed.
			for integration, condition := range map[string]string{"github": "        if: ${{ !cancelled() }}\n", "azure": "    condition: succeededOrFailed()\n"} {
				hosted := app.SaveCIHandoff(fixture.handoff(integration, run, snapshot, filepath.Join(t.TempDir(), integration+".yml")))
				if hosted.State != desktop.Completed {
					t.Fatalf("%s: %+v", integration, hosted)
				}
				workflow := reviewedWorkflow(t, hosted.Document)
				posix := reviewedWorkflow(t, handoff.Document)
				lines := strings.Split(posix, "\n")
				suiteLine, gateLine := lines[0], lines[2]
				at, gateAt := strings.Index(workflow, suiteLine), strings.Index(workflow, gateLine)
				if strings.Count(workflow, suiteLine) != 1 || strings.Count(workflow, gateLine) != 1 || at < 0 || gateAt < at || strings.Count(workflow, condition) != 1 || strings.Index(workflow, condition) < at {
					t.Fatalf("%s does not run the suite and then the gate as its own step:\n%s", integration, workflow)
				}
				if strings.Contains(workflow, "|| true") || strings.Contains(workflow, "continue-on-error") {
					t.Fatalf("%s ignores a failure:\n%s", integration, workflow)
				}
				if !strings.Contains(workflow, documentedGateStep(t, string(documented), condition)) {
					t.Fatalf("%s's gate step is not the documented one:\n%s", integration, workflow)
				}
			}
		})
	}
}

// TestTheWindowVerifiesRetainedGatesAsReadmitSuiteVerifyGateDoes holds the
// window's verification to `readmit suite verify-gate` over one intact
// snapshot, the same snapshot tampered with, one retained under another
// reviewed policy, a run directory that was never a snapshot, and an intact
// snapshot whose retained verdict is unknown: the window answers the summary
// the command prints, completes only for the passing one, says whether the
// snapshot or its verdict is what could not be established, and names every
// part not verified. Past the policy's retention end
// the verification is refused as expired, as suite.VerifyGate refuses it at
// the same instant, and a cancelled verification reaches no verdict. None of
// it changes a retained byte or sends.
func TestTheWindowVerifiesRetainedGatesAsReadmitSuiteVerifyGateDoes(t *testing.T) {
	bin := cliExecutable(t)
	fixture := gatedSuiteFixture(t, bin)
	current := filepath.Join(fixture.dir, "current")
	if exit, out := exitOf(t, fixture.suiteCI(current, fixture.coverage)); exit != 0 {
		t.Fatalf("current suite ci exited %d: %s", exit, out)
	}
	intact := filepath.Join(fixture.dir, "intact")
	if exit, out := exitOf(t, fixture.gate(current, intact)); exit != 0 {
		t.Fatalf("gate exited %d: %s", exit, out)
	}
	sent := fixture.connections.Load()

	// Another reviewed policy over the same runs: a snapshot retained under it
	// is foreign to the first policy's identity.
	var other suite.GatePolicy
	if err := json.Unmarshal(mustReadFile(t, fixture.policy), &other); err != nil {
		t.Fatal(err)
	}
	other.Rationale = "a second review of the same baseline"
	otherPolicy := filepath.Join(fixture.dir, "other-policy.json")
	putJSON(t, otherPolicy, other)
	foreign := filepath.Join(fixture.dir, "foreign")
	if exit, out := exitOf(t, exec.Command(bin, "suite", "gate", current, "--baseline", fixture.baseline, "--policy", otherPolicy, "--policy-identity", other.Identity(), "--output", foreign)); exit != 0 {
		t.Fatalf("foreign gate exited %d: %s", exit, out)
	}
	// A policy pinning another execution build: the gate retains the runs
	// intact, and its assessment of them is unknown.
	otherBuild := other
	otherBuild.Rationale, otherBuild.Engine = "a review pinning another build", "another-build"
	otherBuildPolicy := filepath.Join(fixture.dir, "other-build-policy.json")
	putJSON(t, otherBuildPolicy, otherBuild)
	undecided := filepath.Join(fixture.dir, "undecided")
	if exit, out := exitOf(t, exec.Command(bin, "suite", "gate", current, "--baseline", fixture.baseline, "--policy", otherBuildPolicy, "--policy-identity", otherBuild.Identity(), "--output", undecided)); exit != 2 {
		t.Fatalf("a gate pinning another build exited %d: %s", exit, out)
	}
	tampered := filepath.Join(fixture.dir, "tampered")
	if err := os.CopyFS(tampered, os.DirFS(intact)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tampered, "current", "ci.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}

	app := workspaceApp(t)
	all := []string{"approval", "pins", "coverage", "baseline", "retention", "target_revision"}
	unverifiable := "could not be verified against this identity"
	for _, check := range []struct {
		name, directory, pin string
		state                desktop.State
		gate, reason         string
		unverified           []string
	}{
		{"intact", intact, fixture.policyPin, desktop.Completed, "passed", "", nil},
		{"tampered", tampered, fixture.policyPin, desktop.Failed, "unknown", unverifiable, all},
		{"foreign", foreign, fixture.policyPin, desktop.Failed, "unknown", unverifiable, all},
		{"not-a-snapshot", current, fixture.policyPin, desktop.Failed, "unknown", unverifiable, all},
		// Every byte of this snapshot verifies under its own identity; what is
		// unknown is the verdict it retained, and the window says so rather
		// than blaming the snapshot.
		{"unknown-verdict", undecided, otherBuild.Identity(), desktop.Failed, "unknown", "matches its manifest under this identity", []string{"pins", "coverage", "baseline"}},
	} {
		t.Run(check.name, func(t *testing.T) {
			before := treeDigest(t, check.directory)
			verified := app.VerifyCIGate(check.directory, check.pin)
			exit, printed := exitOf(t, exec.Command(bin, "suite", "verify-gate", check.directory, "--policy-identity", check.pin))
			encoded, _ := json.Marshal(verified.Gate)
			if verified.State != check.state || verified.Gate == nil || verified.Gate.State != check.gate || !sameReport(t, encoded, bytes.TrimSpace(printed)) || exit != verified.Gate.ExitCode {
				t.Fatalf("window %+v %+v; verify-gate exited %d printing %s", verified, verified.Gate, exit, printed)
			}
			if strings.Join(verified.Unverified, ",") != strings.Join(check.unverified, ",") {
				t.Fatalf("unverified %v, want %v", verified.Unverified, check.unverified)
			}
			if check.state != desktop.Completed && (!strings.Contains(verified.Reason, check.reason) || !strings.Contains(verified.Reason, "never a pass")) {
				t.Fatalf("an unknown gate was not refused for what it is: %+v", verified)
			}
			if treeDigest(t, check.directory) != before {
				t.Fatal("verification changed the snapshot")
			}
		})
	}

	// Past the retention end the verification is refused as expired, as the
	// shared operation refuses it at the same instant; nothing is deleted.
	later := time.Date(2037, 1, 1, 0, 0, 0, 0, time.UTC)
	expired := desktop.VerifyCIGateWithinForTest(t.Context(), intact, fixture.policyPin, later)
	want := suite.VerifyGate(t.Context(), intact, fixture.policyPin, later)
	if expired.State != desktop.Failed || expired.Gate == nil || *expired.Gate != want || want.Retention != "expired" || !strings.Contains(expired.Reason, "retention commitment has ended") ||
		strings.Join(expired.Unverified, ",") != "approval,pins,coverage,baseline,target_revision" {
		t.Fatalf("expired: %+v %+v, the operation answers %+v", expired, expired.Gate, want)
	}

	// A cancelled verification reaches no verdict and changes nothing.
	before := treeDigest(t, intact)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if cancelled := desktop.VerifyCIGateWithinForTest(ctx, intact, fixture.policyPin, time.Now().UTC()); cancelled.State != desktop.Cancelled || cancelled.Gate != nil {
		t.Fatalf("cancelled: %+v", cancelled)
	}
	if treeDigest(t, intact) != before {
		t.Fatal("a cancelled verification changed the snapshot")
	}
	if fixture.connections.Load() != sent {
		t.Fatal("verification sent")
	}
}

// documentedGateStep is the YAML example docs/customer-ci.md gives for one
// hosted integration's gate step, found by the condition only it carries.
func documentedGateStep(t *testing.T, documented, condition string) string {
	t.Helper()
	for _, block := range strings.Split(documented, "```yaml\n")[1:] {
		block, _, _ = strings.Cut(block, "```\n")
		if strings.Contains(block, condition) && strings.Contains(block, "suite gate") {
			return block
		}
	}
	t.Fatalf("docs/customer-ci.md documents no gate step with %q", condition)
	return ""
}

// sameReport decodes two readmit-ci-gate/v1 summaries through the strict
// reader and reports whether they are the same summary.
func sameReport(t *testing.T, left, right []byte) bool {
	t.Helper()
	one, err := suite.DecodeGateReport(left)
	if err != nil {
		t.Fatalf("%s: %v", left, err)
	}
	two, err := suite.DecodeGateReport(right)
	if err != nil {
		t.Fatalf("%s: %v", right, err)
	}
	return one == two
}

// treeDigest is one digest over every file's relative name and bytes.
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	sum := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		fmt.Fprintf(sum, "%s\x00%d\x00", rel, len(raw))
		sum.Write(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// The CI panel's cancel names the operation the facade runs a verification
// under, so it stops exactly that verification and nothing another panel
// started.
func TestTheCIPanelCancelsAVerificationByTheNameItRunsUnder(t *testing.T) {
	source := string(mustReadFile(t, filepath.Join("..", "..", "desktop", "frontend", "src", "RunnerPanel.tsx")))
	if !strings.Contains(source, `const CI_GATE_VERIFY = "`+desktop.CIGateVerifyOperationForTest+`";`) ||
		!strings.Contains(source, "cancel(CI_GATE_VERIFY)") {
		t.Fatalf("the CI panel does not cancel a verification by the name %q", desktop.CIGateVerifyOperationForTest)
	}
}
