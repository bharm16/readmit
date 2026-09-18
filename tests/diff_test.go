package tests

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/testrunner"
)

func diffCases(t *testing.T) (string, string) {
	t.Helper()
	paths := []string{filepath.Join(t.TempDir(), "left"), filepath.Join(t.TempDir(), "right")}
	for i, fixture := range []string{"diff-before.mllp", "diff-after.mllp"} {
		if _, stderr, err := run(t, "capture", "../testdata/fixtures/"+fixture, "--output", paths[i]); err != nil {
			t.Fatalf("capture: %v %s", err, stderr)
		}
	}
	return paths[0], paths[1]
}

func diffJSON(t *testing.T, args ...string) (diff.Report, string) {
	t.Helper()
	args = append(append([]string{"diff"}, args...), "--format", "json")
	stdout, stderr, err := run(t, args...)
	if err != nil || stderr != "" {
		t.Fatalf("diff: %v %s", err, stderr)
	}
	var report diff.Report
	if err := json.Unmarshal([]byte(stdout), &report, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	return report, stdout
}

func TestDiffAcceptanceFixtureAndExplicitIgnore(t *testing.T) {
	left, right := diffCases(t)
	leftBefore, _ := bundle.Open(left)
	rightBefore, _ := bundle.Open(right)
	report, stdout := diffJSON(t, left, right, "--key", "MSH-10", "--ignore", "MSH-7")
	var expected struct {
		Summary          diff.Summary `json:"summary"`
		ChangedSelectors []string     `json:"changed_selectors"`
		NullSelector     string       `json:"null_selector"`
		Inserted         string       `json:"inserted_occurrence"`
		UnchangedLeft    string       `json:"unchanged_left"`
		UnchangedRight   string       `json:"unchanged_right"`
	}
	data, err := os.ReadFile("../testdata/fixtures/diff-expected.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &expected, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if report.Schema != "readmit-diff/v1" || report.Alignment != "declared-keys" || report.Summary != expected.Summary || len(report.Unsupported) != 0 {
		t.Fatalf("incorrect fixture summary: %+v", report)
	}
	var selectors []string
	for _, field := range report.Pairs[0].Fields {
		selectors = append(selectors, field.Selector)
		if field.Selector == expected.NullSelector && (field.Left.State != hl7.Null || field.Right.State != hl7.Omitted) {
			t.Fatal("null collapsed into omission")
		}
		if _, err := hl7.ParseSelector(field.Selector); err != nil {
			t.Fatal("diff selector cannot be used unchanged in a test spec")
		}
	}
	if !reflect.DeepEqual(selectors, expected.ChangedSelectors) || report.Inserted[0].Occurrence != expected.Inserted || report.Pairs[1].Left.Occurrence != expected.UnchangedLeft || report.Pairs[1].Right.Occurrence != expected.UnchangedRight {
		t.Fatalf("wrong field/occurrence comparison: %+v", report)
	}
	if len(report.Ignore) != 1 || report.Ignore[0].Selector != "MSH[1]-7[1]" || report.Ignore[0].Compared != 2 || report.Ignore[0].Suppressed != 1 {
		t.Fatal("ignore scope or application missing")
	}
	if strings.Contains(stdout, "SECRET") || strings.Contains(stdout, left) || strings.Contains(stdout, "diff-before") {
		t.Fatal("default report disclosed values or paths")
	}
	without, _ := diffJSON(t, left, right, "--key", "MSH-10")
	if without.Summary.FieldChanges != 4 || without.Pairs[0].Fields[0].Selector != "MSH[1]-7[1]" {
		t.Fatal("removing ignore did not expose timestamp change")
	}
	reverse, _ := diffJSON(t, right, left, "--key", "MSH-10", "--ignore", "MSH-7")
	if reverse.Summary.Missing != 1 || reverse.Summary.Inserted != 0 || reverse.Missing[0].Occurrence != "s0001-e000002" {
		t.Fatal("reverse comparison lost missing message")
	}
	leftAfter, err := bundle.Open(left)
	if err != nil || leftAfter.Identity != leftBefore.Identity {
		t.Fatal("diff changed left source")
	}
	rightAfter, err := bundle.Open(right)
	if err != nil || rightAfter.Identity != rightBefore.Identity {
		t.Fatal("diff changed right source")
	}
}

func TestDiffTerminalAndMarkdownContainTheSameReport(t *testing.T) {
	left, right := diffCases(t)
	terminal, stderr, err := run(t, "diff", left, right, "--key", "MSH-10", "--ignore", "MSH-7", "--show-values")
	if err != nil || stderr != "" {
		t.Fatalf("terminal: %v %s", err, stderr)
	}
	markdown, stderr, err := run(t, "diff", left, right, "--key", "MSH-10", "--ignore", "MSH-7", "--show-values", "--format", "markdown")
	if err != nil || stderr != "" {
		t.Fatalf("markdown: %v %s", err, stderr)
	}
	// Decode only the CommonMark headings, list prefixes, and punctuation
	// escapes this plain-text report uses, then compare every content line.
	var unmarked []string
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimPrefix(strings.TrimPrefix(line, "## "), "- ")
		var decoded strings.Builder
		for i := 0; i < len(line); i++ {
			if line[i] == '\\' && i+1 < len(line) && strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", rune(line[i+1])) {
				i++
			}
			decoded.WriteByte(line[i])
		}
		unmarked = append(unmarked, decoded.String())
	}
	if strings.Join(unmarked, "\n") != terminal {
		t.Fatal("terminal and Markdown content differ")
	}
	for _, want := range []string{"MSH[1]-7[1]: compared=2; suppressed differences=1", "OBX[2]-5[1] Observation Value", "Inserted in right", "SECRET-AFTER", "null", "omitted", "ledger correctness"} {
		if !strings.Contains(terminal, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestDiffUnrelatedCollectionsRequireKeysAndNeverGuessCollisions(t *testing.T) {
	data, err := os.ReadFile("../testdata/fixtures/diff-before.mllp")
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte("SECRET-B|P"), []byte("SECRET-A|P"))
	var cases []string
	for range 2 {
		dir := t.TempDir()
		path := filepath.Join(dir, "PRIVATE-PATH.mllp")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "case")
		if _, stderr, err := run(t, "capture", path, "--output", out); err != nil {
			t.Fatalf("capture: %v %s", err, stderr)
		}
		cases = append(cases, out)
	}
	if stdout, stderr, err := run(t, "diff", cases[0], cases[1]); err == nil || stdout != "" || !strings.Contains(stderr, "explicit --key") {
		t.Fatal("undeclared key was assumed")
	}
	report, stdout := diffJSON(t, cases[0], cases[1], "--key", "MSH-10")
	if report.Summary.Paired != 0 || report.Summary.Ambiguous != 1 || len(report.Ambiguous[0].Left) != 2 || len(report.Ambiguous[0].Right) != 2 || report.Summary.Missing != 0 || report.Summary.Inserted != 0 {
		t.Fatalf("duplicate keys were guessed: %+v", report)
	}
	if strings.Contains(stdout, "SECRET") {
		t.Fatal("alignment key leaked")
	}
}

func diffFiles(t *testing.T, left, right []byte) (string, string) {
	t.Helper()
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "SECRET-LEFT.hl7"), filepath.Join(dir, "SECRET-RIGHT.hl7")}
	for i, data := range [][]byte{left, right} {
		if err := os.WriteFile(paths[i], data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return paths[0], paths[1]
}

func TestDiffSelectorsStatesDecodingAndUnsupportedEvidence(t *testing.T) {
	header := "MSH|^~\\&|SYNTHETIC|LAB|READMIT|FIXTURE|20260101120000||ADT^A08|SECRET-CONTROL|P|2.5.1\r"
	left, right := diffFiles(t, []byte(header+"PID|1||ONE~SECOND^^^LAB&OLD||EXAMPLE|\"\"\rOBX|1|TX|NOTE||A\\F\\B\r"), []byte(header+"PID|1||ONE~SECOND^^^LAB&NEW||EXAMPLE|\rOBX|1|TX|NOTE||A\\X7c\\B\r"))
	report, _ := diffJSON(t, left, right, "--field", "PID-3[2].4.2", "--field", "PID-6", "--field", "OBX-5")
	if report.Alignment != "single-message" || report.Summary.FieldChanges != 2 || report.Pairs[0].Fields[0].Selector != "PID[1]-3[2].4.2" || report.Pairs[0].Fields[1].Left.State != hl7.Null || report.Pairs[0].Fields[1].Right.State != hl7.Empty {
		t.Fatalf("shared selector semantics lost: %+v", report)
	}
	left, right = diffFiles(t, []byte(header+"OBX|1|TX|NOTE||SECRET\\ZLOCAL\\\r"), []byte(header+"OBX|1|TX|NOTE||SECRET\\ZLOCAL\\\r"))
	report, stdout := diffJSON(t, left, right, "--field", "OBX-5", "--ignore", "OBX-5")
	if report.Summary.Uncompared != 1 || len(report.Unsupported) != 2 || report.Pairs[0].Fields[0].Status != "uncompared" || strings.Contains(stdout, "SECRET") {
		t.Fatal("unsupported escape hidden by equality/ignore")
	}
	left, right = diffFiles(t, []byte("SECRET broken payload"), []byte(header))
	report, stdout = diffJSON(t, left, right)
	if report.Summary.Uncompared != 1 || len(report.Unsupported) != 1 || report.Unsupported[0].Code != "unparsed" || strings.Contains(stdout, "SECRET") {
		t.Fatal("unparsed evidence skipped")
	}
	left, right = diffFiles(t, []byte(header+"OBX|1|TX|NOTE||\\X1b5b33316d\\<script>`\\Xff\\\r"), []byte(header+"OBX|1|TX|NOTE||OTHER\r"))
	report, stdout = diffJSON(t, left, right, "--field", "OBX-5", "--show-values")
	if len(report.Unsupported) != 1 || report.Unsupported[0].Code != "non_utf8" || !strings.Contains(*report.Pairs[0].Fields[0].Left.Display, `\x1b`) {
		t.Fatal("unsupported byte display lost")
	}
	markdown, stderr, err := run(t, "diff", left, right, "--field", "OBX-5", "--show-values", "--format", "markdown")
	if err != nil || stderr != "" || strings.Contains(markdown, "\x1b") || strings.Contains(markdown, "<script>") {
		t.Fatal("explicit values became terminal control or Markdown markup")
	}
}

func TestDiffOutputIsExclusiveAndOutsideEveryInputArtifact(t *testing.T) {
	left, right := diffCases(t)
	out := filepath.Join(t.TempDir(), "report.md")
	args := []string{"diff", left, right, "--key", "MSH-10", "--format", "markdown", "--output", out}
	if stdout, stderr, err := run(t, args...); err != nil || stdout != "" || stderr != "" {
		t.Fatalf("file output: %v %s %s", err, stdout, stderr)
	}
	before, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if stdout, _, err := run(t, args...); err == nil || stdout != "" {
		t.Fatal("existing diff overwritten")
	}
	after, _ := os.ReadFile(out)
	if !bytes.Equal(before, after) {
		t.Fatal("existing output changed")
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(out); info.Mode().Perm() != 0600 {
			t.Fatal("report permissions not private")
		}
	}
	for _, path := range []string{left, right} {
		for _, output := range []string{filepath.Join(path, "diff.md"), filepath.Join(path, "payloads", "diff.md")} {
			if stdout, stderr, err := run(t, "diff", left, right, "--key", "MSH-10", "--output", output); err == nil || stdout != "" || !strings.Contains(stderr, "outside the immutable input case") {
				t.Fatal("artifact was writable by diff")
			}
		}
		payload := filepath.Join(path, "payloads", "s0001-e000001.bin")
		if stdout, stderr, err := run(t, "diff", payload, payload, "--output", filepath.Join(path, "report.txt")); err == nil || stdout != "" || !strings.Contains(stderr, "outside the immutable input case") {
			t.Fatal("nested raw input lost artifact protection")
		}
	}
	if runtime.GOOS != "windows" {
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(filepath.Join(right, "payloads"), alias); err != nil {
			t.Fatal(err)
		}
		if _, _, err := run(t, "diff", left, right, "--key", "MSH-10", "--output", filepath.Join(alias, "diff.md")); err == nil {
			t.Fatal("output alias bypassed artifact protection")
		}
	}
	if _, err := bundle.Open(left); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Open(right); err != nil {
		t.Fatal(err)
	}
}

func TestDiffCorruptionAndOptionsFailPrivatelyBeforeWriting(t *testing.T) {
	left, right := diffCases(t)
	for _, tail := range [][]string{{"--key", "SECRET-BAD"}, {"--boundary", "SECRET"}, {"--format", "SECRET"}, {"--input-format", "SECRET"}, {"--terminator", "SECRET"}, {"--field", "MSH-7", "--field", "MSH[1]-7[1]"}, {"--output", ""}} {
		args := append([]string{"diff", left, right}, tail...)
		stdout, stderr, err := run(t, args...)
		if err == nil || stdout != "" || stderr == "" || strings.Contains(stderr, "SECRET") || strings.Contains(stderr, left) || len(stderr) > 300 {
			t.Fatalf("unsafe configuration failure: %v %s %s", err, stdout, stderr)
		}
	}
	if err := os.WriteFile(filepath.Join(right, "payloads", "s0001-e000001.bin"), []byte("SECRET-TAMPER"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "report.json")
	stdout, stderr, err := run(t, "diff", left, right, "--key", "MSH-10", "--output", output)
	if err == nil || stdout != "" || strings.Contains(stderr, "SECRET") {
		t.Fatal("corrupt input accepted or disclosed")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("corruption produced output")
	}
}

func TestDiffResultReportsWireBoundaryAndProtectsEnclosingResult(t *testing.T) {
	dir, spec := testSpecFixture(t)
	wait := testListener(t, dir, "defective")
	result := filepath.Join(dir, "result")
	_, stderr, err := run(t, "test", spec, "--send", "--output", result)
	if processCode(t, err) != 1 || stderr != "" {
		t.Fatalf("baseline fixture did not fail its ledger test: %v %s", err, stderr)
	}
	wait()
	runPath := filepath.Join(result, "run")
	report, _ := diffJSON(t, result, runPath, "--boundary", "acks", "--field", "MSA-1")
	if report.Left.ResultStatus != "assertion_failure" || report.Left.ResultBoundary != "appointment-ledger" || report.Summary.Unchanged != 2 || report.Boundary != diff.ACKs {
		t.Fatal("wire equality changed the recorded ledger verdict or boundary")
	}
	for _, input := range []string{runPath, filepath.Join(runPath, "payloads", "o000001-sent.bin")} {
		output := filepath.Join(result, "diff.md")
		stdout, stderr, err := run(t, "diff", input, input, "--output", output)
		if err == nil || stdout != "" || !strings.Contains(stderr, "outside the immutable input case") {
			t.Fatalf("nested input lost enclosing result protection: %v %s", err, stderr)
		}
	}
	if _, err := testrunner.Open(result); err != nil {
		t.Fatal("refused diff changed result evidence")
	}
}

func TestDiffEarlyResultErrorAndUnalignableKeysRemainVisible(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "invalid-spec.json")
	if err := os.WriteFile(spec, []byte(`{"schema":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	result := filepath.Join(dir, "error-result")
	if _, _, err := run(t, "test", spec, "--send", "--output", result); processCode(t, err) != 2 {
		t.Fatal("invalid spec did not produce execution-error evidence")
	}
	report, _ := diffJSON(t, result, "../testdata/fixtures/listen-s12.hl7")
	if report.Left.ResultStatus != "execution_error" || report.Alignment != "unavailable" || report.Summary.Paired != 0 || report.Summary.Unaligned != 1 || len(report.Unsupported) != 1 || report.Unsupported[0].Code != "result_without_run" {
		t.Fatalf("missing run became empty comparable evidence: %+v", report)
	}
	left, right := diffCases(t)
	report, _ = diffJSON(t, left, right, "--key", "PID-99")
	if report.Summary.Paired != 0 || report.Summary.Unaligned != 5 || report.Summary.Missing != 0 || report.Summary.Inserted != 0 {
		t.Fatal("omitted keys became empty-string alignments or invented insertions")
	}
}

func TestDiffUnknownDictionaryAndExactIgnoreScope(t *testing.T) {
	header := "MSH|^~\\&|SYNTHETIC|LAB|READMIT|FIXTURE|20260101120000||ADT^A08|CONTROL|P|2.3\r"
	left, right := diffFiles(t, []byte(header+"PID|1||FIRST~OLD\rZZZ\r"), []byte(header+"PID|1||FIRST~NEW\r"))
	report, _ := diffJSON(t, left, right, "--ignore", "PID-3")
	if report.Summary.FieldChanges != 1 || len(report.Pairs[0].Segments) != 1 || report.Pairs[0].Segments[0].Left != "ZZZ[1]" || report.Pairs[0].Fields[0].Name != "" || len(report.Unsupported) != 2 || report.Ignore[0].Suppressed != 0 {
		t.Fatalf("unknown labels, repeated ignore scope, or empty segment loss: %+v", report)
	}
	report, _ = diffJSON(t, left, right, "--field", "PID-3[2]", "--ignore", "PID-3")
	if report.Ignore[0].Compared != 0 || report.Summary.FieldChanges != 1 || len(report.Pairs[0].Segments) != 0 {
		t.Fatal("exact ignore or selected-field scope widened")
	}
}
