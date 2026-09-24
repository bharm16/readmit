package diagnose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
)

// A diagnosis is retained as a report directory: one new directory holding
// the strict document, report.json, beside its Markdown rendering, report.md,
// which renders the same report. A single diagnosis (readmit-diagnosis/v1)
// and a grouping of several (readmit-diagnosis-groups/v1) are both retained
// this way. This file owns the layout, the bounds a retained one is read
// within and the identity it is named by; both entry points write and reopen
// a report directory only through it.

// ReportName and MarkdownName are the two members of a report directory. A
// directory holding ReportName says what it holds by the contract that
// document declares.
const (
	ReportName   = "report.json"
	MarkdownName = "report.md"
)

// MaxReportBytes bounds a retained readmit-diagnosis/v1 report. One past it is
// refused, never truncated.
const MaxReportBytes = 16 << 20

// Identity is what a retained report is named by, and what every document
// bound to one names it by: the SHA-256 of its exact bytes, in lowercase
// hexadecimal. A review binds the report and the decisions made over it this
// way.
func Identity(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// reportFamily is a report directory whose subject names it in the refusals
// of a failed write. The rendering is written last, after report.json is
// written and synced in full; readers read only report.json and never ask for
// the rendering. A write interrupted part way is retained, visible as
// incomplete rather than looking like a report that was never started, and a
// retry names a new directory.
func reportFamily(subject string) artifactdir.Family {
	return artifactdir.Family{
		Layout: artifactdir.Layout{AllowFile: func(name string) bool { return name == ReportName || name == MarkdownName }},
		Seal:   artifactdir.CompletionRecord(MarkdownName, ""),
		Errors: artifactdir.Errors{
			Reserve: errors.New("cannot create report directory; destination must be new and parent writable"),
			Open:    errors.New("cannot open new report directory"),
			Create:  errors.New("cannot create " + subject + " file; incomplete report retained"),
			Write:   errors.New("cannot write " + subject + " file; incomplete report retained"),
			Sync:    errors.New("cannot sync " + subject + " report directory; the report was written in full but a power loss could still lose it"),
		},
	}
}

func writeDirectory(destination, subject string, document, rendering []byte) error {
	_, err := artifactdir.Write(context.Background(), destination, reportFamily(subject), artifactdir.Durable,
		map[string][]byte{ReportName: document, MarkdownName: rendering})
	return err
}

// WriteReport retains one diagnosis as a new report directory at
// destination, which must not exist, and answers the identity of the
// report.json it wrote: the identity a review of it names.
func WriteReport(destination string, report Report) (string, error) {
	data, err := JSON(report)
	if err != nil {
		return "", errors.New("cannot encode diagnosis report")
	}
	if err := writeDirectory(destination, "diagnosis", data, Markdown(report)); err != nil {
		return "", err
	}
	return Identity(data), nil
}

// WriteGroups retains one grouping as a new report directory at destination,
// which must not exist. A cancellation that arrives before it starts writing
// writes nothing; once it starts, it writes the whole directory.
func WriteGroups(ctx context.Context, destination string, report GroupsReport) error {
	data, err := GroupsJSON(report)
	if err != nil {
		return err
	}
	markdown := GroupsMarkdown(report)
	if err := ctx.Err(); err != nil {
		return err
	}
	return writeDirectory(destination, "diagnosis groups", data, markdown)
}

// Retained is one retained diagnosis as it was reopened: the report, read
// through ParseReport, and the identity of the exact bytes it was read from.
type Retained struct {
	Report   Report
	Identity string
}

// Reading is how a caller reopens a retained diagnosis. Its zero value reads
// report.json as the one regular file of that name in the directory, never
// through a link, and refuses in the words of a report directory.
type Reading struct {
	// Displayed, when set, is the identity the report was displayed under
	// before a person decided anything about it. A report whose bytes have
	// changed since is refused rather than read as the one they saw.
	Displayed string
	// ReadFile, when set, reads report.json in its caller's own words: a
	// caller that reads every file a person names through one reader of its
	// own, as the command line does, reads this one through it too. It must
	// refuse a file larger than limit bytes.
	ReadFile func(path string, limit int) ([]byte, error)
}

// OpenReport reopens the diagnosis one retained report directory holds,
// through the strict reader, within MaxReportBytes, and names it by the
// identity of the exact bytes it read.
func OpenReport(directory string, reading Reading) (Retained, error) {
	var data []byte
	var err error
	if reading.ReadFile != nil {
		data, err = reading.ReadFile(filepath.Join(directory, ReportName), MaxReportBytes)
	} else {
		data, err = readReport(directory, "diagnosis report", MaxReportBytes)
	}
	if err != nil {
		return Retained{}, err
	}
	report, err := ParseReport(data)
	if err != nil {
		return Retained{}, err
	}
	identity := Identity(data)
	if reading.Displayed != "" && reading.Displayed != identity {
		return Retained{}, errors.New("the displayed diagnosis changed; reopen the report before reviewing")
	}
	return Retained{Report: report, Identity: identity}, nil
}

// OpenGroups reopens the grouping one retained report directory holds, for
// display, through its own strict reader. A grouping is never a diagnosis a
// review reads: OpenReport refuses it by the contract it declares.
func OpenGroups(directory string) (GroupsReport, error) {
	// GroupsJSON bounds the encoded grouping before appending one newline.
	data, err := readReport(directory, "diagnosis grouping report", MaxGroupsReportBytes+1)
	if err != nil {
		return GroupsReport{}, err
	}
	return ParseGroups(data)
}

// readReport reads the report.json of one report directory as one regular
// file, never through a link, no larger than limit bytes. noun names the
// report in its refusals.
func readReport(directory, noun string, limit int) ([]byte, error) {
	path := filepath.Join(directory, ReportName)
	info, err := os.Lstat(path)
	switch {
	case err != nil || !info.Mode().IsRegular():
		return nil, errors.New("a " + noun + " directory holds " + ReportName + " as one regular file")
	case info.Size() > int64(limit):
		return nil, errors.New("the " + noun + " is larger than this release reads")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("the " + noun + " could not be read")
	}
	return data, nil
}
