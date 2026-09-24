package operation

import (
	"context"
	"errors"
	"os"
	"regexp"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/corpus"
	"github.com/bharm16/readmit/internal/importer"
)

// ErrBaseTime refuses a declared generator base time that is not one exact
// instant: a whole second, in RFC 3339, with its time zone stated.
var ErrBaseTime = errors.New("base time must be a whole-second RFC3339 timestamp with an explicit timezone")

var baseTimePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

// DeclaredBaseTime reads the base time a generator is declared with. The
// clock is never read in its place, and nothing is rounded or assumed: a
// fraction of a second or a missing zone is refused, because two generations
// from the same declarations must be the same generation.
func DeclaredBaseTime(value string) (time.Time, error) {
	base, err := time.Parse(time.RFC3339, value)
	if err != nil || !baseTimePattern.MatchString(value) {
		return time.Time{}, ErrBaseTime
	}
	return base, nil
}

// ErrBenchmarkCancelled refuses a benchmark of a cancelled scan: it measured
// part of a stream, and a number nothing stands behind is not published.
var ErrBenchmarkCancelled = errors.New("scan cancelled; no benchmark was written")

// CorpusScan is one scan of one declared stream: what importer.Scan counted,
// the bounds it held itself to, how long it took, and whether it was
// cancelled. A cancelled scan's counts are what it reached before it stopped,
// never a complete answer, and nothing about the unread remainder is known.
type CorpusScan struct {
	Plan      importer.Plan
	Result    importer.ScanResult
	Bounds    corpus.Bounds
	Elapsed   time.Duration
	Cancelled bool
}

// ScanCorpus is `readmit corpus scan`: it streams one regular file through
// importer.Scan under the declared plan and bounds, holding what the scanner's
// documented bounds hold and nothing more. A benchmark destination, when one
// is named, is checked before the stream is read, so an unusable destination
// is refused in a moment rather than after a long scan; exclusive creation in
// WriteBenchmark still owns the guarantee. A cancellation is reported as a
// cancelled scan with the counts it reached, not as an error.
func ScanCorpus(ctx context.Context, path string, options importer.ScanOptions, report string) (CorpusScan, error) {
	if report != "" {
		reserved, err := artifactpath.Destination(report)
		if err != nil {
			return CorpusScan{}, err
		}
		if _, err := os.Lstat(reserved); err == nil {
			return CorpusScan{}, errors.New("the benchmark destination must be a new file")
		} else if !os.IsNotExist(err) {
			// The destination could not be looked at, so it is not known to be
			// new; the cause says why, such as a folder this account cannot
			// search.
			return CorpusScan{}, fileError{"the benchmark destination must be a new file", err}
		}
	}
	stream, err := corpus.Open(path)
	if err != nil {
		return CorpusScan{}, err
	}
	defer stream.Close()
	started := time.Now()
	result, scanErr := importer.Scan(ctx, stream, options)
	elapsed := time.Since(started)
	if scanErr != nil && !errors.Is(scanErr, importer.ErrScanCancelled) {
		return CorpusScan{}, scanErr
	}
	return CorpusScan{
		Plan:   options.Plan,
		Result: result,
		Bounds: corpus.Bounds{
			BatchRecords:  batchOrDefault(options.BatchRecords, importer.MaxBatchRecords),
			BatchBytes:    batchOrDefault(options.BatchBytes, importer.MaxBatchBytes),
			RecordBytes:   importer.MaxRecordBytes,
			ResidentBound: importer.ResidentBound,
		},
		Elapsed:   elapsed,
		Cancelled: errors.Is(scanErr, importer.ErrScanCancelled),
	}, nil
}

// WriteBenchmark writes the readmit-benchmark/v1 document of one completed
// scan to a new file. A cancelled scan is refused, and nothing is written.
func WriteBenchmark(report string, scan CorpusScan) error {
	if scan.Cancelled {
		return ErrBenchmarkCancelled
	}
	encoded, err := corpus.EncodeBenchmark(corpus.NewBenchmark(scan.Plan, scan.Result, scan.Bounds, scan.Elapsed))
	if err != nil {
		return err
	}
	return WriteNewFile(report, encoded,
		"cannot create the benchmark; destination must be new and writable",
		"cannot write the benchmark")
}

func batchOrDefault(declared, fallback int) int {
	if declared == 0 {
		return fallback
	}
	return declared
}
