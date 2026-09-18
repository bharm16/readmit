package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const indexMessage = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|IDX-%04d|P|2.5.1\rPID|1||MRN-%04d^^^READMIT^MR||DOE^JANE||\"\"|\rSCH|1||||||||||20260101130000\r"

// indexCorpus writes a case source of many framed occurrences. It is generated
// here rather than committed: the case is the point, not the bytes.
func indexCorpus(t *testing.T, occurrences int) string {
	t.Helper()
	var source strings.Builder
	for i := 1; i <= occurrences; i++ {
		source.WriteString("\x0b" + fmt.Sprintf(indexMessage, i, i) + "\x1c\r")
	}
	// One occurrence this release cannot decode, so every run exercises
	// evidence that carries no indexed field at all.
	source.WriteString("\x0bNOT-HL7\x1c\r")
	path := filepath.Join(t.TempDir(), "corpus.mllp")
	if err := os.WriteFile(path, []byte(source.String()), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// indexedCase captures a corpus and returns the case directory.
func indexedCase(t *testing.T, occurrences int) string {
	t.Helper()
	destination := filepath.Join(t.TempDir(), "case")
	if _, stderr, err := run(t, "capture", indexCorpus(t, occurrences), "--output", destination); err != nil || stderr != "" {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	return destination
}

func indexFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "search.json")
}

func TestIndexFindsOneOccurrenceOfALargeCaseWithoutDisclosingValues(t *testing.T) {
	evidence := indexedCase(t, 200)
	written := indexFile(t)
	stdout, stderr, err := run(t, "index", "build", evidence, "--output", written,
		"--field", "PID-3", "--field", "MSH-10", "--retain", "values", "--retain-until", "indefinite")
	if err != nil || stderr != "" {
		t.Fatalf("index build: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Index written: readmit-index/v1", "Case contract: readmit-case/v1", "Provenance: imported",
		"Retention: values", "Retain until: indefinite", "Retention state: active",
		"Fields: PID[1]-3[1] MSH[1]-10[1]", "Sources: 1", "Records: 201",
		"Decoded occurrences: 200", "Undecodable occurrences: 1",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing index summary %q:\n%s", want, stdout)
		}
	}
	for _, secret := range []string{"MRN-", "IDX-", "DOE", "corpus.mllp", written} {
		if strings.Contains(stdout+stderr, secret) {
			t.Errorf("index build disclosed %q", secret)
		}
	}

	stdout, stderr, err = run(t, "index", "show", evidence, written)
	if err != nil || stderr != "" || !strings.Contains(stdout, "Index: readmit-index/v1") {
		t.Fatalf("index show: %v %s %s", err, stderr, stdout)
	}

	// One value out of two hundred, found by the field an operator named.
	stdout, stderr, err = run(t, "index", "search", evidence, written, "--field", "PID-3", "--equals", "MRN-0137^^^READMIT^MR")
	if err != nil || stderr != "" {
		t.Fatalf("index search: %v %s", err, stderr)
	}
	for _, want := range []string{"Query: equals", "Field: PID[1]-3[1]", "Matches: 1", "Undecided: 0", "s0001-e000137 source=s0001 kind=message", "state=present", "truncated=false"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing match %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "MRN-0137") {
		t.Errorf("index search disclosed a retained value without --show-values:\n%s", stdout)
	}
	stdout, stderr, err = run(t, "index", "search", evidence, written, "--field", "PID-3", "--equals", "MRN-0137^^^READMIT^MR", "--show-values")
	if err != nil || stderr != "" || !strings.Contains(stdout, `value="MRN-0137^^^READMIT^MR"`) {
		t.Fatalf("explicit values were not displayed: %v %s %s", err, stderr, stdout)
	}

	// A substring reaches many occurrences; a state query reaches the fields
	// that are absent from every one of them.
	stdout, _, err = run(t, "index", "search", evidence, written, "--contains", "READMIT^MR")
	if err != nil || !strings.Contains(stdout, "Matches: 200") {
		t.Fatalf("substring search: %v %s", err, stdout)
	}
	stdout, _, err = run(t, "index", "search", evidence, written, "--field", "PID-3", "--state", "present")
	if err != nil || !strings.Contains(stdout, "Matches: 200") {
		t.Fatalf("state search: %v %s", err, stdout)
	}
}

func TestIndexRetainsNothingNobodyDeclared(t *testing.T) {
	evidence := indexedCase(t, 2)
	for name, args := range map[string][]string{
		"no fields":     {"--retain", "values", "--retain-until", "indefinite"},
		"no form":       {"--field", "PID-3", "--retain-until", "indefinite"},
		"no end":        {"--field", "PID-3", "--retain", "values"},
		"unknown form":  {"--field", "PID-3", "--retain", "everything", "--retain-until", "indefinite"},
		"unknown end":   {"--field", "PID-3", "--retain", "values", "--retain-until", "soon"},
		"unknown field": {"--field", "not a selector", "--retain", "values", "--retain-until", "indefinite"},
		"no output":     {"--field", "PID-3", "--retain", "values", "--retain-until", "indefinite", "--output", ""},
	} {
		t.Run(name, func(t *testing.T) {
			written := indexFile(t)
			stdout, stderr, err := run(t, append([]string{"index", "build", evidence, "--output", written}, args...)...)
			if err == nil || stdout != "" || stderr == "" {
				t.Fatalf("an index was built from an incomplete declaration: %s %s", stdout, stderr)
			}
			if _, err := os.Stat(written); !os.IsNotExist(err) {
				t.Fatal("a refused build left an index behind")
			}
		})
	}
	// A command group without a subcommand is refused the way every other one
	// is, rather than indexing something nobody asked to have indexed.
	if stdout, stderr, err := run(t, "index"); err == nil || stdout != "" || !strings.Contains(stderr, "invalid command or arguments") {
		t.Fatalf("index without a subcommand: %v %s %s", err, stdout, stderr)
	}
}

func TestIndexSearchAsksExactlyOneQuestion(t *testing.T) {
	evidence := indexedCase(t, 2)
	written := indexFile(t)
	if _, stderr, err := run(t, "index", "build", evidence, "--output", written,
		"--field", "PID-3", "--retain", "digests", "--retain-until", "indefinite"); err != nil || stderr != "" {
		t.Fatalf("index build: %v %s", err, stderr)
	}
	for name, args := range map[string][]string{
		"nothing asked": {},
		"two asked":     {"--equals", "a", "--contains", "b"},
		"unknown state": {"--state", "maybe"},
		"unknown field": {"--field", "PV1-3", "--state", "present"},
		// A digest index holds no value bytes, so a substring question is
		// refused rather than answered from something narrower.
		"substring of digests": {"--contains", "MRN"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, stderr, err := run(t, append([]string{"index", "search", evidence, written}, args...)...); err == nil || stderr == "" {
				t.Fatal("a question this index cannot answer was answered anyway")
			}
		})
	}
	stdout, _, err := run(t, "index", "search", evidence, written, "--equals", "MRN-0002^^^READMIT^MR")
	if err != nil || !strings.Contains(stdout, "Matches: 1") || strings.Contains(stdout, "MRN-0002") {
		t.Fatalf("a digest index did not answer an exact question privately: %v %s", err, stdout)
	}
}

func TestACorruptIndexIsRefusedAndTheEvidenceStaysReadable(t *testing.T) {
	evidence := indexedCase(t, 4)
	written := indexFile(t)
	if _, stderr, err := run(t, "index", "build", evidence, "--output", written,
		"--field", "PID-3", "--retain", "values", "--retain-until", "indefinite"); err != nil || stderr != "" {
		t.Fatalf("index build: %v %s", err, stderr)
	}
	before := evidenceDigest(t, evidence)
	sound, err := os.ReadFile(written)
	if err != nil {
		t.Fatal(err)
	}
	for name, damaged := range map[string][]byte{
		"altered value":    []byte(strings.Replace(string(sound), "TVJOLTAwMDJeXl5SRUFETUlUXk1S", "TVJOLTAwMDNeXl5SRUFETUlUXk1S", 1)),
		"truncated":        sound[:len(sound)/2],
		"unknown version":  []byte(strings.Replace(string(sound), "readmit-index/v1", "readmit-index/v9", 1)),
		"unknown member":   []byte(strings.Replace(string(sound), `"schema":"readmit-index/v1",`, `"schema":"readmit-index/v1","rebuilt":true,`, 1)),
		"emptied":          {},
		"not this release": []byte("{}\n"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(written, damaged, 0600); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := run(t, "index", "search", evidence, written, "--state", "present")
			if err == nil || strings.Contains(stdout, "Matches:") {
				t.Fatalf("a %s index answered a query: %s", name, stdout)
			}
			if stderr == "" {
				t.Fatal("a refused index reported no reason")
			}
			// The case is the canonical artifact; a damaged index is a rebuild.
			if _, timelineErr, err := run(t, "timeline", evidence); err != nil || timelineErr != "" {
				t.Fatalf("a damaged index blocked the evidence: %v %s", err, timelineErr)
			}
			if after := evidenceDigest(t, evidence); after != before {
				t.Fatal("a damaged index changed the evidence")
			}
		})
	}
	// Rebuilding from the canonical directory restores exactly what was lost.
	if err := os.Remove(written); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := run(t, "index", "build", evidence, "--output", written,
		"--field", "PID-3", "--retain", "values", "--retain-until", "indefinite"); err != nil || stderr != "" {
		t.Fatalf("rebuild: %v %s", err, stderr)
	}
	stdout, _, err := run(t, "index", "search", evidence, written, "--equals", "MRN-0002^^^READMIT^MR")
	if err != nil || !strings.Contains(stdout, "Matches: 1") {
		t.Fatalf("the rebuilt index did not answer: %v %s", err, stdout)
	}
}

func TestAnIndexOfOtherEvidenceAndAnExpiredOneAreBothRefused(t *testing.T) {
	first := indexedCase(t, 2)
	second := indexedCase(t, 3)
	written := indexFile(t)
	if _, stderr, err := run(t, "index", "build", first, "--output", written,
		"--field", "PID-3", "--retain", "states", "--retain-until", "indefinite"); err != nil || stderr != "" {
		t.Fatalf("index build: %v %s", err, stderr)
	}
	if _, stderr, err := run(t, "index", "search", second, written, "--state", "present"); err == nil || !strings.Contains(stderr, "different evidence") {
		t.Fatalf("an index of other evidence answered for this case: %s", stderr)
	}
	if _, stderr, err := run(t, "index", "show", second, written); err == nil || !strings.Contains(stderr, "different evidence") {
		t.Fatalf("an index of other evidence described this case: %s", stderr)
	}

	// A retention end already past is recorded as declared and reported as
	// ended, and the index is not served.
	expired := indexFile(t)
	stdout, stderr, err := run(t, "index", "build", first, "--output", expired,
		"--field", "PID-3", "--retain", "states", "--retain-until", "2026-01-01T00:00:00Z")
	if err != nil || stderr != "" {
		t.Fatalf("index build: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Retain until: 2026-01-01T00:00:00Z") || !strings.Contains(stdout, "Retention state: ended") {
		t.Fatalf("a past retention end was not reported: %s", stdout)
	}
	if _, stderr, err := run(t, "index", "search", first, expired, "--state", "present"); err == nil || !strings.Contains(stderr, "retention") {
		t.Fatalf("an index past its retention answered a query: %s", stderr)
	}
	// Reading what it retains is still possible, so an operator can see why.
	if stdout, _, err := run(t, "index", "show", first, expired); err != nil || !strings.Contains(stdout, "Retention state: ended") {
		t.Fatalf("an expired index could not be inspected: %v %s", err, stdout)
	}
}

func TestIndexIsNeverWrittenInsideEvidence(t *testing.T) {
	evidence := indexedCase(t, 2)
	before := evidenceDigest(t, evidence)
	for _, destination := range []string{
		filepath.Join(evidence, "search.json"),
		filepath.Join(evidence, "payloads", "search.json"),
	} {
		_, stderr, err := run(t, "index", "build", evidence, "--output", destination,
			"--field", "PID-3", "--retain", "values", "--retain-until", "indefinite")
		if err == nil || !strings.Contains(stderr, "outside the immutable input case") {
			t.Fatalf("an index was written inside retained evidence: %s", stderr)
		}
	}
	if after := evidenceDigest(t, evidence); after != before {
		t.Fatal("a refused index changed the evidence")
	}
}

// evidenceDigest is one value over every file of a case directory and its
// bytes, so a change anywhere inside it is a change of this value.
func evidenceDigest(t *testing.T, root string) string {
	t.Helper()
	sum := sha256.New()
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		fmt.Fprintf(sum, "%s\x00%d\x00", filepath.ToSlash(relative), len(data))
		sum.Write(data)
		return nil
	}); err != nil {
		t.Fatalf("read case evidence: %v", err)
	}
	return hex.EncodeToString(sum.Sum(nil))
}
