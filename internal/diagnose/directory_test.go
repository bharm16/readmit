package diagnose_test

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/diagnose"
)

// retainedReport diagnoses the reschedule capture, whose booking the capture
// never held, and retains the report in a new directory. It answers the
// report, the directory and the identity WriteReport named it by.
func retainedReport(t *testing.T) (diagnose.Report, string, string) {
	t.Helper()
	report := run(t, writeCase(t, fixture(t, "diagnose-reschedule.hl7")), diagnose.DefaultConfig())
	if len(report.Findings) == 0 {
		t.Fatal("the fixture produced no finding")
	}
	directory := filepath.Join(t.TempDir(), "diagnosis")
	identity, err := diagnose.WriteReport(directory, report)
	if err != nil {
		t.Fatal(err)
	}
	return report, directory, identity
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// refusedAs holds one reopening to the exact sentence it must be refused in.
func refusedAs(t *testing.T, what string, err error, sentence string) {
	t.Helper()
	if err == nil || err.Error() != sentence {
		t.Fatalf("%s: want %q, got %v", what, sentence, err)
	}
}

func TestIdentityIsTheSHA256OfTheExactBytes(t *testing.T) {
	for data, want := range map[string]string{
		"":    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"abc": "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
	} {
		if got := diagnose.Identity([]byte(data)); got != want {
			t.Fatalf("Identity(%q) = %s, want %s", data, got, want)
		}
	}
}

// A report directory holds the report and its rendering and nothing else,
// privately, and reopens as the report it holds under the identity its writer
// named: the digest of the exact report.json bytes.
func TestAReportDirectoryHoldsTheReportAndReopensAsItself(t *testing.T) {
	report, directory, identity := retainedReport(t)
	document, err := diagnose.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(directory, diagnose.ReportName)), document) {
		t.Fatal("report.json is not the report's own encoding")
	}
	if !bytes.Equal(mustRead(t, filepath.Join(directory, diagnose.MarkdownName)), diagnose.Markdown(report)) {
		t.Fatal("report.md is not the report's own rendering")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		t.Fatalf("a report directory holds something besides its two members: %v %v", entries, err)
	}
	if identity != diagnose.Identity(document) {
		t.Fatalf("the writer named the report %s, not the digest of its bytes", identity)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(directory); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("the report directory is not private: %v %v", info.Mode(), err)
		}
		for _, name := range []string{diagnose.ReportName, diagnose.MarkdownName} {
			if info, err := os.Stat(filepath.Join(directory, name)); err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("%s is not private: %v %v", name, info.Mode(), err)
			}
		}
	}
	for _, reading := range []diagnose.Reading{{}, {Displayed: identity}} {
		retained, err := diagnose.OpenReport(directory, reading)
		if err != nil {
			t.Fatal(err)
		}
		reencoded, err := diagnose.JSON(retained.Report)
		if err != nil || !bytes.Equal(reencoded, document) || retained.Identity != identity {
			t.Fatalf("the report did not reopen as itself under its identity: %s %v", retained.Identity, err)
		}
	}
}

// A report is written only where nothing is: an existing directory is
// refused by name and left as it was, and a directory inside the evidence the
// diagnosis read is refused before anything is created.
func TestAReportDirectoryIsWrittenOnlyWhereNothingIs(t *testing.T) {
	report, directory, _ := retainedReport(t)
	original := mustRead(t, filepath.Join(directory, diagnose.ReportName))
	_, err := diagnose.WriteReport(directory, report)
	if err == nil || !strings.Contains(err.Error(), "destination must be new") {
		t.Fatalf("an existing report directory was not refused by name: %v", err)
	}
	if strings.Contains(err.Error(), directory) {
		t.Fatalf("the refusal disclosed a path: %v", err)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(directory, diagnose.ReportName)), original) {
		t.Fatal("a refused write changed the retained report")
	}

	casePath := writeCase(t, fixture(t, "diagnose-reschedule.hl7"))
	inside := filepath.Join(casePath, "diagnosis")
	if _, err := diagnose.WriteReport(inside, report); err == nil {
		t.Fatal("a report was written inside the case it was run over")
	}
	if _, err := os.Lstat(inside); !os.IsNotExist(err) {
		t.Fatal("a refused report left a directory inside the case")
	}
}

// Reopening reads report.json as one regular file within the report bound,
// through the strict reader, and refuses anything else in the words of a
// report directory.
func TestReopeningRefusesWhatIsNotOneRetainedDiagnosis(t *testing.T) {
	_, directory, _ := retainedReport(t)
	document := filepath.Join(directory, diagnose.ReportName)
	data := mustRead(t, document)

	emptied := filepath.Join(t.TempDir(), "emptied")
	if err := os.Mkdir(emptied, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := diagnose.OpenReport(emptied, diagnose.Reading{})
	refusedAs(t, "a directory without report.json", err, "a diagnosis report directory holds report.json as one regular file")

	nested := filepath.Join(t.TempDir(), "nested")
	if err := os.MkdirAll(filepath.Join(nested, diagnose.ReportName), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = diagnose.OpenReport(nested, diagnose.Reading{})
	refusedAs(t, "a directory named report.json", err, "a diagnosis report directory holds report.json as one regular file")

	if runtime.GOOS != "windows" {
		linked := filepath.Join(t.TempDir(), "linked")
		if err := os.Mkdir(linked, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(document, filepath.Join(linked, diagnose.ReportName)); err != nil {
			t.Fatal(err)
		}
		_, err = diagnose.OpenReport(linked, diagnose.Reading{})
		refusedAs(t, "a link to a report", err, "a diagnosis report directory holds report.json as one regular file")
	}

	grouped, err := diagnose.GroupCases(context.Background(), []string{writeCase(t, fixture(t, "diagnose-reschedule.hl7"))}, diagnose.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	groups := filepath.Join(t.TempDir(), "groups")
	if err := diagnose.WriteGroups(context.Background(), groups, grouped); err != nil {
		t.Fatal(err)
	}
	_, err = diagnose.OpenReport(groups, diagnose.Reading{})
	refusedAs(t, "a grouping", err, "diagnosis report declares a contract version this release does not read")

	oversized := filepath.Join(t.TempDir(), "oversized")
	if err := os.Mkdir(oversized, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oversized, diagnose.ReportName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(filepath.Join(oversized, diagnose.ReportName), diagnose.MaxReportBytes+1); err != nil {
		t.Fatal(err)
	}
	_, err = diagnose.OpenReport(oversized, diagnose.Reading{})
	refusedAs(t, "a report past the bound", err, "the diagnosis report is larger than this release reads")
}

// A caller that reads every file a person names through its own reader reads
// report.json through it, asked for that one file within the report bound;
// its refusal is the reopening's, word for word, and its bytes are read as the
// module's own reading reads them.
func TestReopeningThroughTheCallersReaderAsksForTheReportWithinItsBound(t *testing.T) {
	_, directory, identity := retainedReport(t)
	refused := errors.New("input must be a readable regular file")
	var asked []string
	var limits []int
	_, err := diagnose.OpenReport(directory, diagnose.Reading{ReadFile: func(path string, limit int) ([]byte, error) {
		asked, limits = append(asked, path), append(limits, limit)
		return nil, refused
	}})
	if err != refused {
		t.Fatalf("the caller's refusal was not the reopening's: %v", err)
	}
	if !slices.Equal(asked, []string{filepath.Join(directory, diagnose.ReportName)}) || !slices.Equal(limits, []int{diagnose.MaxReportBytes}) {
		t.Fatalf("the caller's reader was asked for %v within %v", asked, limits)
	}
	retained, err := diagnose.OpenReport(directory, diagnose.Reading{ReadFile: func(path string, _ int) ([]byte, error) { return os.ReadFile(path) }})
	if err != nil || retained.Identity != identity {
		t.Fatalf("the caller's reader reopened another report: %s %v", retained.Identity, err)
	}
}

// A report whose bytes changed after it was displayed is refused, even when it
// still holds the same findings: a person decided about the bytes they saw.
// Reopened afresh, it is named by its new identity.
func TestReopeningRefusesAReportThatChangedSinceItWasDisplayed(t *testing.T) {
	_, directory, identity := retainedReport(t)
	document := filepath.Join(directory, diagnose.ReportName)
	var reformatted jsontext.Value = mustRead(t, document)
	if err := reformatted.Indent(jsontext.WithIndentPrefix(""), jsontext.WithIndent("\t")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(document, append(reformatted, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := diagnose.OpenReport(directory, diagnose.Reading{Displayed: identity})
	refusedAs(t, "a report changed since it was displayed", err, "the displayed diagnosis changed; reopen the report before reviewing")
	afresh, err := diagnose.OpenReport(directory, diagnose.Reading{})
	if err != nil || afresh.Identity == identity || afresh.Identity != diagnose.Identity(mustRead(t, document)) {
		t.Fatalf("the changed report did not reopen under its own identity: %s %v", afresh.Identity, err)
	}
}

// A grouping is retained as a report directory too, and reopens only through
// its own display reader, within its own bound.
func TestAGroupingIsRetainedAndReopenedThroughItsOwnReader(t *testing.T) {
	grouped, err := diagnose.GroupCases(context.Background(), []string{writeCase(t, fixture(t, "diagnose-reschedule.hl7"))}, diagnose.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "groups")
	if err := diagnose.WriteGroups(context.Background(), directory, grouped); err != nil {
		t.Fatal(err)
	}
	document, err := diagnose.GroupsJSON(grouped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(directory, diagnose.ReportName)), document) ||
		!bytes.Equal(mustRead(t, filepath.Join(directory, diagnose.MarkdownName)), diagnose.GroupsMarkdown(grouped)) {
		t.Fatal("the grouping directory does not hold the grouping's own encoding and rendering")
	}
	reopened, err := diagnose.OpenGroups(directory)
	if err != nil {
		t.Fatal(err)
	}
	if reencoded, err := diagnose.GroupsJSON(reopened); err != nil || !bytes.Equal(reencoded, document) {
		t.Fatalf("the grouping did not reopen as itself: %v", err)
	}
	if err := diagnose.WriteGroups(context.Background(), directory, grouped); err == nil || !strings.Contains(err.Error(), "destination must be new") {
		t.Fatalf("an existing grouping directory was not refused by name: %v", err)
	}

	_, single, _ := retainedReport(t)
	_, err = diagnose.OpenGroups(single)
	refusedAs(t, "a single diagnosis", err, "diagnosis grouping report declares a contract version this release does not read")
	_, err = diagnose.OpenGroups(t.TempDir())
	refusedAs(t, "a directory without report.json", err, "a diagnosis grouping report directory holds report.json as one regular file")
	oversized := filepath.Join(t.TempDir(), "oversized")
	if err := os.Mkdir(oversized, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oversized, diagnose.ReportName), document, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(filepath.Join(oversized, diagnose.ReportName), diagnose.MaxGroupsReportBytes+2); err != nil {
		t.Fatal(err)
	}
	_, err = diagnose.OpenGroups(oversized)
	refusedAs(t, "a grouping past the bound", err, "the diagnosis grouping report is larger than this release reads")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := filepath.Join(t.TempDir(), "cancelled")
	if err := diagnose.WriteGroups(ctx, cancelled, grouped); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled grouping was not reported cancelled: %v", err)
	}
	if _, err := os.Lstat(cancelled); !os.IsNotExist(err) {
		t.Fatal("a cancelled grouping created its directory")
	}
}

// The strict reader refuses a document that is not this release's diagnosis,
// and the configuration reader refuses one past the configuration bound.
func TestReportReaderRefusesADocumentThatIsNotThisDiagnosis(t *testing.T) {
	report := run(t, writeCase(t, fixture(t, "diagnose-reschedule.hl7")), diagnose.DefaultConfig())
	data, err := diagnose.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := diagnose.ParseReport(data); err != nil {
		t.Fatalf("the shipped diagnosis was refused: %v", err)
	}
	for name, document := range map[string]string{
		"unknown version":         strings.Replace(string(data), "readmit-diagnosis/v1", "readmit-diagnosis/v2", 1),
		"unknown member":          strings.Replace(string(data), `"findings":`, `"verdict":"fine","findings":`, 1),
		"renamed finding":         strings.Replace(string(data), `"f000001"`, `"first"`, 1),
		"case named by no digest": strings.Replace(string(data), report.CaseIdentity, "case", 1),
		"past the bound":          string(data) + strings.Repeat(" ", diagnose.MaxReportBytes),
	} {
		if _, err := diagnose.ParseReport([]byte(document)); err == nil {
			t.Fatalf("%s was accepted as a diagnosis report", name)
		}
	}
	if _, err := diagnose.ParseConfig(make([]byte, diagnose.MaxConfigBytes+1)); err == nil || err.Error() != "diagnosis configuration exceeds 1 MiB" {
		t.Fatalf("a configuration past the bound was not refused by it: %v", err)
	}
}
