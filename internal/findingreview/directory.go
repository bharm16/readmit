package findingreview

import (
	"context"
	"errors"
	"os"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
)

// A review is retained as one new directory holding the strict record,
// review.json, beside its Markdown rendering, review.md. Both entry points
// record a review only through Write.
const (
	recordName   = "review.json"
	markdownName = "review.md"
)

// reviewFamily is a review directory. The rendering is written last, after
// review.json is written and synced in full. A write interrupted part way is
// retained, visible as incomplete, and a retry names a new directory.
var reviewFamily = artifactdir.Family{
	Layout: artifactdir.Layout{AllowFile: func(name string) bool { return name == recordName || name == markdownName }},
	Seal:   artifactdir.CompletionRecord(markdownName, ""),
	Errors: artifactdir.Errors{
		Reserve: errors.New("cannot create report directory; destination must be new and parent writable"),
		Open:    errors.New("cannot open new report directory"),
		Create:  errors.New("cannot create review file; incomplete report retained"),
		Write:   errors.New("cannot write review file; incomplete report retained"),
		Sync:    errors.New("cannot sync review report directory; the review was written in full but a power loss could still lose it"),
	},
}

// Write records one review as a new directory at destination, which must not
// exist. It must also lie outside both inputs the review was made over: inside
// the case at casePath it would invalidate the verified immutable evidence,
// and inside the diagnosis report directory it would put a judgment where a
// machine's findings are. Both are compared by filesystem identity, through
// every link, never by name.
func Write(destination string, record Record, casePath, reportDirectory string) error {
	caseInfo, err := os.Stat(casePath)
	if err != nil {
		return errors.New("cannot inspect the reviewed case directory")
	}
	reportInfo, err := os.Stat(reportDirectory)
	if err != nil {
		return errors.New("cannot inspect the diagnosis report directory")
	}
	resolved, err := artifactpath.Destination(destination, caseInfo, reportInfo)
	if err != nil {
		return err
	}
	data, err := JSON(record)
	if err != nil {
		return errors.New("cannot encode finding review")
	}
	_, err = artifactdir.Write(context.Background(), resolved, reviewFamily, artifactdir.Durable,
		map[string][]byte{recordName: data, markdownName: Markdown(record)})
	return err
}
