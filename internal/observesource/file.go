package observesource

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

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
	// An export states how old its state is through the time it was last
	// written. A file readmit cannot date, or one dated after the read, has no
	// single reading rather than a fresh one.
	modified := info.ModTime()
	if modified.IsZero() || modified.After(taken.at) {
		return failure(taken, observewindow.SampleAmbiguous, "the export states a modification time this read cannot place")
	}
	taken.asOf = modified
	age := taken.at.Sub(modified)
	taken.record.StatedAge = age.String()
	if age > r.maxAge {
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
// observed value, because collection that failed counted nothing, read no
// original material and has no state to digest.
func failure(taken attempt, status observewindow.SampleStatus, note string) attempt {
	taken.status = status
	taken.record.Note = note
	taken.keys = nil
	taken.asOf = time.Time{}
	return taken
}

// errReadBound reports a document that exceeded the declared read bound, so a
// caller can report a truncated read rather than a failed one.
var errReadBound = errors.New("read exceeds the declared bound")

// readBounded opens one regular file and reads at most the declared bound. Its
// diagnostics name no path, and the caller names what it was reading, so one
// bounded read serves the export and the certificate authority alike. Checking
// the opened descriptor again retains the check, because the path can change
// underneath it between the first check and the open.
func readBounded(path string, limit int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open the declared file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("the declared file is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, errors.New("cannot read the declared file")
	}
	if len(data) > limit {
		return nil, errReadBound
	}
	return data, nil
}
