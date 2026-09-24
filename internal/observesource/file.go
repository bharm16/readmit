package observesource

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observewindow"
)

// fileReader reads one bounded export on disk. It never retries: a read that
// found no export found none, and reading again until one appears is waiting
// for a convenient answer rather than observing the source.
type fileReader struct {
	extraction Extraction
	export     File
	maxAge     time.Duration
}

func (r *fileReader) close() {}

// read takes one bounded read of the export and reports exactly what it was.
// An export that is absent, unreadable, larger than its declared bound, older
// than the declared freshness bound, or structurally not what the extraction
// declares each report that condition; none of them reports zero records.
func (r *fileReader) read(_ context.Context) attempt {
	taken := attempt{at: time.Now(), record: Evidence{Kind: FileExport, Attempts: 1}}
	info, err := os.Stat(r.export.Path)
	switch {
	case err != nil && os.IsNotExist(err):
		return failure(taken, observewindow.SampleMissing, "the declared export is not present, so no state could be obtained")
	case err != nil:
		return failure(taken, observewindow.SampleFailed, "the declared export could not be inspected")
	case !info.Mode().IsRegular():
		return failure(taken, observewindow.SampleFailed, "the declared export is not a regular file")
	case info.Size() > int64(r.export.MaxBytes):
		return failure(taken, observewindow.SampleTruncated, "the export is larger than the declared read bound, so a prefix of it would be read rather than the source")
	}
	data, err := readBounded(r.export.Path, r.export.MaxBytes)
	if err != nil {
		if errors.Is(err, errReadBound) {
			return failure(taken, observewindow.SampleTruncated, "the export grew past the declared read bound while it was being read")
		}
		return failure(taken, observewindow.SampleFailed, "the declared export could not be read")
	}
	taken.record.Bytes = len(data)
	// The read is dated when it finished, so an export written while it was
	// being read is never dated after the read that returned its bytes. An
	// export that changed underneath the read is state this read cannot place
	// rather than state it observed.
	taken.at = time.Now()
	after, err := os.Stat(r.export.Path)
	if err != nil || !after.Mode().IsRegular() || !after.ModTime().Equal(info.ModTime()) || after.Size() != info.Size() {
		return failure(taken, observewindow.SampleAmbiguous, "the export changed while it was being read")
	}
	// An export states how old its state is through the time it was last
	// written. A file readmit cannot date, or one dated after the read, has no
	// single reading rather than a fresh one.
	modified := info.ModTime()
	if modified.IsZero() || modified.After(taken.at) {
		return failure(taken, observewindow.SampleAmbiguous, "the export states a modification time this read cannot place")
	}
	if !dateState(&taken, modified, r.maxAge) {
		return failure(taken, observewindow.SampleStale, "the export's state is older than the declared freshness bound")
	}
	taken.evidence = map[string][]byte{"body": data}
	keys, err := divide(r.extraction, data)
	if err != nil {
		return failure(taken, observewindow.SampleAmbiguous, err.Error())
	}
	taken.status = observewindow.Observed
	taken.keys = keys
	return taken
}

// divide reads one bounded document through internal/importer's own envelope
// readers under the declared extraction, and returns the record keys in scope.
// Nothing is parsed here: a document that contradicts its declaration is the
// same refusal a mapping recipe would make of it.
func divide(extraction Extraction, data []byte) ([]string, error) {
	records, err := extraction.Shape().Divide([]importer.Locator{extraction.RecordKey}, data)
	if err != nil {
		return nil, errors.New("the document does not divide into the declared schema")
	}
	return recordKeys(extraction, records)
}

// failure records one read that was not an observation. It clears every
// observed value, because collection that failed counted nothing and has no
// state to digest. Material the source did answer with is kept: a document that
// could not be read into the declared schema is exactly what an operator needs
// to correct the declaration, and it is retained beside that read's own record
// rather than named by a sample, because the completion contract reserves an
// evidence identity for an observation.
func failure(taken attempt, status observewindow.SampleStatus, note string) attempt {
	taken.status = status
	taken.record.Note = note
	taken.keys = nil
	taken.asOf, taken.from = time.Time{}, time.Time{}
	return taken
}

// errReadBound reports a document that exceeded the declared read bound, so a
// caller can report a truncated read rather than a failed one.
var errReadBound = errors.New("read exceeds the declared bound")

// errCannotOpenDeclared is a declared file that cannot be opened.
var errCannotOpenDeclared = errors.New("cannot open the declared file")

// readBounded reads one regular file within the declared bound. Its
// diagnostics name no path, and the caller names what it was reading, so one
// bounded read serves the export and the certificate authority alike. The
// store inspects the name before opening it and checks the opened file is the
// one inspected, because the path can change underneath it.
func readBounded(path string, limit int) ([]byte, error) {
	declared := declaredFile
	declared.MaxBytes = limit
	return declared.Read(path)
}

// declaredFile is a file a source declares, such as its export or its
// certificate authority, read through a link at its name.
var declaredFile = artifactdir.Document{
	Links: artifactdir.FollowLinks,
	Refusals: artifactdir.DocumentRefusals{
		Inspect:   errCannotOpenDeclared,
		Irregular: errors.New("the declared file is not a regular file"),
		Open:      errCannotOpenDeclared,
		Read:      errors.New("cannot read the declared file"),
		Size:      errReadBound,
	},
}
