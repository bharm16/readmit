package report

// The sealed v1 report vocabulary is a compatibility contract, not interface
// copy (#525 RP01-RP16). OpenReview regenerates every portable rendering and
// compares its exact bytes, and Open compares the packet instructions, so a
// label sweep that shortened a sealed heading, the PDF footer or a file name
// would reject every review and packet already written. These tests pin the
// renderers' bytes over fixed inputs as they were before the label cleanup;
// they fail on any change, however small, to what an existing reader
// regenerates. A deliberate new report format needs a new versioned schema,
// never a new digest here.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/testrunner"
)

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// sealedManifest is a representative v1 packet summary input: two runs whose
// identities are fixed placeholders, with the shipped limitations.
func sealedManifest() Manifest {
	run := func(path string, mode observation.Mode, status testrunner.Status, ledger int) RunLabel {
		return RunLabel{
			Path: path, ResultIdentity: strings.Repeat("1", 64), InputBundleIdentity: strings.Repeat("2", 64),
			SpecIdentity: strings.Repeat("3", 64), TargetIdentity: strings.Repeat("4", 64),
			ReceiverImplementation: "readmit-builtin-siu-receiver", ReceiverProfile: "readmit-siu-v1",
			ReceiverMode: mode, ReceiverSession: "session-" + path, Status: status, LedgerCount: ledger,
		}
	}
	return Manifest{
		InputIdentity: strings.Repeat("2", 64), SpecIdentity: strings.Repeat("3", 64), Limitations: limitations,
		Runs: []RunLabel{
			run("baseline", observation.Defective, testrunner.AssertionFailure, 2),
			run("post-fix", observation.Fixed, testrunner.Pass, 1),
		},
	}
}

// sealedPortable is a representative portable report: the review's own
// heading lines, both retained packet documents and a run section, long
// enough to span more than one PDF page, with a passing, a failing and an
// unresolved run.
func sealedPortable() (portableReport, []reviewRun) {
	lines := []string{
		"READMIT - RETAINED INVESTIGATION REVIEW",
		"Customer-local sensitive evidence. No disclosure approval or de-identification.",
		"Packet identity: " + strings.Repeat("5", 64),
		"Verify offline: readmit report review REVIEW_DIRECTORY",
		"Schemas: readmit-portable-review/v1, readmit-portable-report/v1,",
		"", "SUMMARY.md", `"# Synthetic engagement packet"`,
		"", "RERUN.md", `"# Reproduce with only the released binary and this packet"`,
		"", "Run: current", "Retained result (assertions, expected/observed values and evidence references):",
		"Result", `"{\"status\": \"assertion_failure\", \"value\": \"<script>(x)\\\\y</script>\"}"`,
		"Historical specification and setup instructions", "Replay events and timing",
	}
	for i := range 60 {
		lines = append(lines, strings.Repeat("evidence line ", 8)+string(rune('a'+i%26)))
	}
	lines = append(lines, "Evidence: packet/current/result/result.json")
	doc := portableReport{Schema: ReportSchema, PacketIdentity: strings.Repeat("5", 64), ExportPolicy: "customer-local", Lines: lines}
	return doc, []reviewRun{{Name: "current", Status: "pass"}, {Name: "baseline", Status: "assertion_failure"}, {Name: "interrupted", Status: "running"}}
}

func TestSealedV1RenderingsKeepTheirExactBytes(t *testing.T) {
	doc, runs := sealedPortable()
	portable, err := renderPortable(doc, runs)
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]byte{
		"SUMMARY.md":                    summary(sealedManifest()),
		"RERUN.md (packet)":             packetInstructions(),
		"RERUN.md (retained trials)":    trialInstructions("127.0.0.1:2575"),
		"RERUN.md (prepared workspace)": preparedTrialInstructions("127.0.0.1:2575"),
		"report.html":                   portable["report.html"],
		"report.md":                     portable["report.md"],
		"report.json":                   portable["report.json"],
		"junit.xml":                     portable["junit.xml"],
		"report.pdf":                    portable["report.pdf"],
	} {
		want, ok := sealedDigests[name]
		if !ok {
			t.Fatalf("no pinned digest for %s", name)
		}
		if digestOf(got) != want {
			t.Errorf("%s changed: sha256 %s, want %s; an existing v1 reader regenerates these bytes", name, digestOf(got), want)
		}
	}
	if len(portable) != 5 {
		t.Fatalf("a portable review renders %d files, want the five v1 renderings", len(portable))
	}
}

// The digests above were taken from the renderers before the #512 label
// cleanup and must not be regenerated to accept a change.
var sealedDigests = map[string]string{
	"SUMMARY.md":                    "53a3838b0880a7181afd632c5096cff4462a14e56a6446dfbca177fcd6f0277f",
	"RERUN.md (packet)":             "a1e854a67952fe3b4aed519b3ea33ecc75e4ae3dfc7014ae029c65f055f7d391",
	"RERUN.md (retained trials)":    "b5d8d1f37a9adfc952aa50d21810466176557a0515c953ffb2fb5d198ed8d205",
	"RERUN.md (prepared workspace)": "bc5a2616b5f420bfc4358d95a385f58f819a3028d38566a3a50c23cfa40eb1d5",
	"report.html":                   "cfbc090628a8800b605b280daec2771a22963690edfc4bc0cec1e3fbcb67d2fd",
	"report.md":                     "4d1215c1862042f46a6a1922961e73d53e04d91d6d1fffc9ac94ef1e987f6038",
	"report.json":                   "39ff59594fc2e19e5e4be5ac699be95d6d27fefbcad1130ab8b7a93bac724814",
	"junit.xml":                     "06f640e911e75a39a14de17ed70b8758615780fc505095a1a008e78306b935cb",
	"report.pdf":                    "c90d8bf52dda8794edf2ab98c16a54a607c9eaf9e3cd1d9d8fb1b3e88d41c024",
}

// Each reviewed Keep is stated here in words too, so a failure names the
// label a sweep reached rather than only a digest.
func TestSealedV1LabelsAreKeptExactly(t *testing.T) {
	doc, runs := sealedPortable()
	portable, err := renderPortable(doc, runs)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		id, where string
		text      []byte
		want      []string
	}{
		{"RP01-RP05", "SUMMARY.md", summary(sealedManifest()), []string{
			"# Synthetic engagement packet\n", "Scenario: ", "Input bundle: ", "Exact historical spec: ",
			"| Result | Receiver mode | Verdict | Ledger records |", "## baseline identities\n", "## post-fix identities\n",
			"- Result: ", "- Input bundle: ", "- Spec: ", "- Configured target: ", "- Receiver implementation: ",
			"- Receiver profile: ", "- Mode: ", "- Session: ", "## Observation boundary and limitations",
			"## Profiles and rules", "## Retained evidence", "## Integrity and rerun",
		}},
		{"RP06-RP08", "RERUN.md", packetInstructions(), []string{
			"# Reproduce with only the released binary and this packet\n", "## Reset and run each mode\n",
			"### baseline\n", "### post-fix\n", "### reintroduced\n", "Terminal A:", "Terminal B:", "Expected exit: ",
			"Expected result: ", "## Verify retained evidence\n", "## Interrupted or repeated trials\n",
			`./readmit report verify "packet"`, `./readmit report prepare "packet" --output "rerun" --address 127.0.0.1:2575`,
		}},
		{"RP09", "prepared RERUN.md", preparedTrialInstructions("127.0.0.1:2575"), []string{
			"## License selection for new manual executions\n",
		}},
		{"RP10", "report.html", portable["report.html"], []string{
			"<title>Readmit sensitive evidence review</title>",
			`content="default-src 'none'; base-uri 'none'; form-action 'none'; sandbox"`,
		}},
		{"RP14", "report.pdf", portable["report.pdf"], []string{
			"(Readmit - sensitive evidence - page 1 of 3) Tj", "(Readmit - sensitive evidence - page 3 of 3) Tj",
		}},
		{"RP15", "junit.xml", portable["junit.xml"], []string{
			`<testsuite name="retained-investigation" tests="3" failures="1" errors="1">`,
			`<testcase name="current"></testcase>`,
			`<failure message="Retained assertion failure; see sensitive report content"></failure>`,
			`<error message="Execution error or unresolved lifecycle; no successful regression proof"></error>`,
		}},
	} {
		for _, want := range check.want {
			if !strings.Contains(string(check.text), want) {
				t.Errorf("%s: %s no longer holds %q", check.id, check.where, want)
			}
		}
	}
	// RP16: the five rendering names and the verification interface.
	for format, name := range map[string]string{"html": "report.html", "pdf": "report.pdf", "markdown": "report.md", "json": "report.json", "junit": "junit.xml"} {
		if reviewFormats[format] != name {
			t.Errorf("RP16: the %s rendering is %q, want %q", format, reviewFormats[format], name)
		}
	}
	if ReviewSchema != "readmit-portable-review/v1" || ReportSchema != "readmit-portable-report/v1" {
		t.Errorf("RP16: the review schemas are %s and %s", ReviewSchema, ReportSchema)
	}
}

// RP11-RP13: the review's own heading lines and run sections, as an exported
// review of a real sealed packet writes them.
func TestReviewHeadingLinesAreKeptExactly(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "source")
	if _, err := Create(context.Background(), Scenario, source); err != nil {
		t.Fatal(err)
	}
	packet := filepath.Join(t.TempDir(), "packet")
	if _, err := Assemble(context.Background(), RetainedInput{Case: filepath.Join(source, "reproducer"), Spec: filepath.Join(source, "spec.json"), Current: filepath.Join(source, "post-fix"), Baseline: filepath.Join(source, "baseline")}, packet); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "review")
	review, err := ExportReview(context.Background(), packet, output)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(output, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	lines := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		lines[strings.TrimPrefix(line, "    ")] = true
	}
	for _, want := range []string{
		"READMIT - RETAINED INVESTIGATION REVIEW",
		"Packet identity: " + review.Manifest.PacketIdentity,
		"Verify offline: readmit report review REVIEW_DIRECTORY",
		"Schemas: readmit-portable-review/v1, readmit-portable-report/v1,",
		"SUMMARY.md", "RERUN.md", "Run: current", "Run: baseline",
		"Retained result (assertions, expected/observed values and evidence references):",
		"Result", "Historical specification and setup instructions", "Replay events and timing",
	} {
		if !lines[want] {
			t.Errorf("the portable review no longer holds the line %q", want)
		}
	}
	evidence := 0
	for line := range lines {
		if strings.HasPrefix(line, "Evidence: packet/") && strings.HasSuffix(line, "/result.json") {
			evidence++
		}
	}
	if evidence != 2 {
		t.Errorf("the portable review names %d retained results as Evidence: {path}, want 2", evidence)
	}
}
