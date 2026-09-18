package importer_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
)

// scanMessage is authored here rather than produced by anything under test, so
// the counts these tests expect are counts a person wrote down.
const scanMessage = "MSH|^~\\&|READMIT|SCAN|RECEIVER|LAB|20260101120000||SIU^S12|SCAN-%06d|T|2.5.1\r" +
	"PID|1||MRN-%06d^^^READMIT^MR||DOE^JANE\r"

func framedRecord(ordinal int) string {
	return "\x0b" + fmt.Sprintf(scanMessage, ordinal, ordinal) + "\x1c\r"
}

func batchRecord(ordinal int) string {
	return fmt.Sprintf(scanMessage, ordinal, ordinal)
}

func stream(records int, shape func(int) string) string {
	var out strings.Builder
	for i := 1; i <= records; i++ {
		out.WriteString(shape(i))
	}
	return out.String()
}

func mllpPlan(t *testing.T) importer.Plan {
	t.Helper()
	return declaredPlan(t, importer.MLLPFraming, "")
}

func batchPlan(t *testing.T) importer.Plan {
	t.Helper()
	return declaredPlan(t, importer.BatchFraming, importer.SegmentStart)
}

func declaredPlan(t *testing.T, framing importer.Framing, boundary importer.Boundary) importer.Plan {
	t.Helper()
	plan := importer.Plan{
		Schema: importer.PlanSchema, Framing: framing, BatchBoundary: boundary,
		Terminator: hl7.CR, Encoding: importer.USASCII, Direction: "inbound", Members: []string{},
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("declared plan: %v", err)
	}
	return plan
}

func scan(t *testing.T, source io.Reader, options importer.ScanOptions) importer.ScanResult {
	t.Helper()
	result, err := importer.Scan(context.Background(), source, options)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	return result
}

func TestScanDividesAStreamTheWayAnImportOfTheSameBytesWould(t *testing.T) {
	for _, c := range []struct {
		name   string
		plan   importer.Plan
		corpus string
		// An MLLP member is one source the case bundle divides into frames; a
		// batch member is one source per record. A scan divides at the frame
		// either way, so its record count is the occurrence count of the first
		// and the source count of the second.
		records int
	}{
		{"mllp frames", mllpPlan(t), stream(37, framedRecord), 37},
		{"batch records", batchPlan(t), stream(37, batchRecord), 37},
		// A batch envelope segment carries no message, so it is a record in its
		// own right at both ends: three messages between them are five records.
		{"hl7 batch envelope", declaredPlan(t, importer.BatchFraming, importer.HL7Batch),
			"BHS|^~\\&|READMIT\r" + stream(3, batchRecord) + "BTS|3\r", 5},
	} {
		t.Run(c.name, func(t *testing.T) {
			result := scan(t, strings.NewReader(c.corpus), importer.ScanOptions{Plan: c.plan})
			if result.Records != int64(c.records) || result.Occurrences != int64(c.records) {
				t.Errorf("records %d occurrences %d, want %d of each", result.Records, result.Occurrences, c.records)
			}
			if result.Decoded+result.Undecodable != int64(c.records) {
				t.Errorf("decoded %d undecodable %d, want %d between them", result.Decoded, result.Undecodable, c.records)
			}
			if result.Bytes != int64(len(c.corpus)) {
				t.Errorf("read %d bytes of a %d byte stream", result.Bytes, len(c.corpus))
			}
			whole := sha256.Sum256([]byte(c.corpus))
			if result.SHA256 != hex.EncodeToString(whole[:]) {
				t.Errorf("digest %s is not the digest of the stream", result.SHA256)
			}
			// The same bytes through the whole-member extractor: one reading of
			// framing, two ways of paying for it.
			extraction, err := importer.Extract(context.Background(), c.plan, []string{writeStream(t, c.corpus)}, nil, nil)
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			if int64(extraction.Totals.Occurrences) != result.Occurrences {
				t.Errorf("extract counted %d occurrences, scan counted %d", extraction.Totals.Occurrences, result.Occurrences)
			}
		})
	}
}

func TestScanOfAStreamWithNoBytesReportsNoRecords(t *testing.T) {
	// A scan names what it read, and there was nothing to read. An import of
	// the same member writes one empty quarantined source instead, because a
	// case names the member it was given; the two statements are different and
	// only one of them is about a stream.
	for _, plan := range []importer.Plan{batchPlan(t), declaredPlan(t, importer.RawFraming, "")} {
		result := scan(t, strings.NewReader(""), importer.ScanOptions{Plan: plan, Window: importer.Window{Limit: 1}})
		if result.Records != 0 || result.Occurrences != 0 || result.Bytes != 0 || len(result.Rows) != 0 {
			t.Fatalf("%s framing over no bytes: %+v", plan.Framing, result)
		}
		if past := result.ExceedsCase(plan); len(past) != 0 {
			t.Errorf("%s framing over no bytes exceeded %v", plan.Framing, past)
		}
	}
	// The framings whose first bytes are part of the declaration refuse an
	// empty stream, exactly as an import of an empty member does: there is no
	// start block and no batch envelope to be found in nothing.
	for _, plan := range []importer.Plan{mllpPlan(t), declaredPlan(t, importer.BatchFraming, importer.HL7Batch)} {
		if _, err := importer.Scan(context.Background(), strings.NewReader(""), importer.ScanOptions{Plan: plan}); !errors.Is(err, importer.ErrDeclaredFraming) {
			t.Errorf("%s framing over no bytes: %v, want %v", plan.Framing, err, importer.ErrDeclaredFraming)
		}
	}
}

func TestScanCountsWhatItCouldNotDecodeWithoutDroppingIt(t *testing.T) {
	// A complete frame this release cannot parse is retained evidence, so a
	// scan counts it as an occurrence it could not decode rather than omitting
	// it. Reporting it as absent would claim the stream does not hold it.
	corpus := framedRecord(1) + "\x0bNOT-HL7\x1c\r" + framedRecord(2)
	result := scan(t, strings.NewReader(corpus), importer.ScanOptions{Plan: mllpPlan(t), Window: importer.Window{Limit: 3}})
	if result.Records != 3 || result.Occurrences != 3 || result.Decoded != 2 || result.Undecodable != 1 {
		t.Fatalf("records %d occurrences %d decoded %d undecodable %d, want 3 3 2 1",
			result.Records, result.Occurrences, result.Decoded, result.Undecodable)
	}
	if len(result.Rows) != 3 || result.Rows[1].Undecodable != 1 || result.Rows[1].Decoded != 0 {
		t.Fatalf("rows %+v, want the second record undecodable", result.Rows)
	}
	if result.Rows[1].Offset != int64(len(framedRecord(1))) {
		t.Errorf("row offset %d, want %d", result.Rows[1].Offset, len(framedRecord(1)))
	}
}

func TestScanRendersOnlyTheRequestedWindowOfALargeStream(t *testing.T) {
	corpus := stream(400, framedRecord)
	result := scan(t, strings.NewReader(corpus), importer.ScanOptions{
		Plan: mllpPlan(t), Window: importer.Window{Offset: 300, Limit: 5},
	})
	if result.Records != 400 {
		t.Fatalf("records %d, want 400", result.Records)
	}
	if len(result.Rows) != 5 {
		t.Fatalf("rendered %d rows, want 5", len(result.Rows))
	}
	for i, row := range result.Rows {
		if row.Ordinal != 301+i {
			t.Errorf("row %d is record %d, want %d", i, row.Ordinal, 301+i)
		}
	}
	// A window nobody asked for renders nothing, and the counts still describe
	// the whole stream.
	counted := scan(t, strings.NewReader(corpus), importer.ScanOptions{Plan: mllpPlan(t)})
	if len(counted.Rows) != 0 || counted.Records != 400 {
		t.Errorf("rows %d records %d, want 0 and 400", len(counted.Rows), counted.Records)
	}
	// A window past the end is empty rather than an error: the stream really
	// does hold nothing there.
	past := scan(t, strings.NewReader(corpus), importer.ScanOptions{Plan: mllpPlan(t), Window: importer.Window{Offset: 400, Limit: 5}})
	if len(past.Rows) != 0 {
		t.Errorf("rendered %d rows past the last record", len(past.Rows))
	}
}

func TestScanHoldsOneParsingBatchWhateverTheStreamLength(t *testing.T) {
	for _, c := range []struct {
		name    string
		records int
		short   bool
	}{
		{"short-stream", 2_000, true},
		{"production-stream", 1_000_000, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if !c.short && testing.Short() {
				t.Skip("the production-size stream runs uninstrumented through make test-corpus")
			}
			record := framedRecord(1)
			source := &repeated{record: []byte(record), left: c.records}
			peakHeap := uint64(0)
			sampled := int64(0)
			result, err := importer.Scan(context.Background(), source, importer.ScanOptions{
				Plan: mllpPlan(t),
				Report: func(p importer.Progress) {
					// Reading memory statistics stops the world, so the heap is
					// sampled on a bounded schedule rather than once per batch.
					if sampled++; sampled%heapSampleBatches != 0 {
						return
					}
					var stats runtime.MemStats
					runtime.ReadMemStats(&stats)
					peakHeap = max(peakHeap, stats.HeapInuse)
				},
			})
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			streamed := int64(c.records) * int64(len(record))
			if result.Records != int64(c.records) || result.Occurrences != int64(c.records) || result.Bytes != streamed {
				t.Fatalf("records %d occurrences %d bytes %d, want %d %d %d",
					result.Records, result.Occurrences, result.Bytes, c.records, c.records, streamed)
			}
			if result.PeakResidentBytes > importer.ResidentBound {
				t.Errorf("peak resident %d is past the declared bound %d", result.PeakResidentBytes, importer.ResidentBound)
			}
			// The whole point: what the scan held does not follow the stream.
			if int64(result.PeakResidentBytes) >= streamed && streamed > heapCeiling {
				t.Errorf("peak resident %d scaled with the %d byte stream", result.PeakResidentBytes, streamed)
			}
			if peakHeap > heapCeiling {
				t.Errorf("heap in use reached %d bytes scanning %d bytes; a bounded scan stays under %d", peakHeap, streamed, heapCeiling)
			}
		})
	}
}

// heapCeiling is the heap a bounded scan stays under whatever it is reading,
// and heapSampleBatches is how often the heap is looked at. The ceiling is far
// above what the declared bounds require and far below a stream that has been
// buffered, so it answers the one question it is asked: did the scan keep the
// stream?
const (
	heapCeiling       = 64 << 20
	heapSampleBatches = 128
)

func TestScanPeakDoesNotMoveWhenTheStreamGrows(t *testing.T) {
	plan := mllpPlan(t)
	record := []byte(framedRecord(1))
	small := scan(t, &repeated{record: record, left: 1_000}, importer.ScanOptions{Plan: plan})
	large := scan(t, &repeated{record: record, left: 8_000}, importer.ScanOptions{Plan: plan})
	if large.Records != 8*small.Records {
		t.Fatalf("records %d and %d are not an eightfold stream", small.Records, large.Records)
	}
	if small.PeakResidentBytes != large.PeakResidentBytes {
		t.Errorf("peak resident moved from %d to %d across an eightfold stream", small.PeakResidentBytes, large.PeakResidentBytes)
	}
	if small.Batches*8 != large.Batches {
		t.Errorf("batches %d and %d: an eightfold stream is eightfold batches of the same size", small.Batches, large.Batches)
	}
}

func TestScanRefusesDeclarationsTheBytesContradict(t *testing.T) {
	for _, c := range []struct {
		name    string
		framing importer.Framing
		bound   importer.Boundary
		term    hl7.Terminator
		corpus  string
		want    error
	}{
		{"mllp declared over unframed bytes", importer.MLLPFraming, "", hl7.CR, stream(2, batchRecord), importer.ErrDeclaredFraming},
		{"raw declared over framed bytes", importer.RawFraming, "", hl7.CR, stream(2, framedRecord), importer.ErrDeclaredFraming},
		{"batch declared over framed bytes", importer.BatchFraming, importer.SegmentStart, hl7.CR, stream(2, framedRecord), importer.ErrDeclaredFraming},
		{"hl7 batch without its envelope", importer.BatchFraming, importer.HL7Batch, hl7.CR, stream(2, batchRecord), importer.ErrDeclaredFraming},
		{"a terminator that reaches no boundary", importer.BatchFraming, importer.SegmentStart, hl7.LF, stream(2, batchRecord), importer.ErrAmbiguousBatch},
		{"raw declared over more than one message", importer.RawFraming, "", hl7.CR, stream(2, batchRecord), importer.ErrAmbiguousBatch},
	} {
		t.Run(c.name, func(t *testing.T) {
			plan := declaredPlan(t, c.framing, c.bound)
			plan.Terminator = c.term
			_, err := importer.Scan(context.Background(), strings.NewReader(c.corpus), importer.ScanOptions{Plan: plan})
			if !errors.Is(err, c.want) {
				t.Fatalf("scan error %v, want %v", err, c.want)
			}
		})
	}
}

func TestScanRefusesBytesThatContradictTheDeclaredEncoding(t *testing.T) {
	plan := mllpPlan(t)
	corpus := framedRecord(1) + "\x0bMSH|^~\\&|\xffREADMIT\x1c\r"
	if _, err := importer.Scan(context.Background(), strings.NewReader(corpus), importer.ScanOptions{Plan: plan}); !errors.Is(err, importer.ErrDeclaredEncoding) {
		t.Fatalf("scan error %v, want %v", err, importer.ErrDeclaredEncoding)
	}
	// The same bytes under a declaration nothing can contradict are read.
	plan.Encoding = importer.Latin1
	if _, err := importer.Scan(context.Background(), strings.NewReader(corpus), importer.ScanOptions{Plan: plan}); err != nil {
		t.Fatalf("iso-8859-1 scan: %v", err)
	}
}

func TestScanRefusesAStreamThatNeverReachesItsDeclaredBoundary(t *testing.T) {
	// Raw framing declares that the whole stream is one message, so a stream
	// past the record bound is refused rather than read into memory. This is
	// the case that says a larger file is not fixed by reading a larger file.
	plan := declaredPlan(t, importer.RawFraming, "")
	filler := bytes.Repeat([]byte("A"), 4096)
	source := io.MultiReader(strings.NewReader("MSH|^~\\&|"), &repeated{record: filler, left: importer.MaxRecordBytes/len(filler) + 1})
	_, err := importer.Scan(context.Background(), source, importer.ScanOptions{Plan: plan})
	if !errors.Is(err, importer.ErrRecordBound) {
		t.Fatalf("scan error %v, want %v", err, importer.ErrRecordBound)
	}
}

func TestScanRefusesUndeclaredWindowsBatchesAndMembers(t *testing.T) {
	valid := mllpPlan(t)
	withMembers := valid
	withMembers.Members = []string{".hl7"}
	for _, c := range []struct {
		name    string
		options importer.ScanOptions
		want    string
	}{
		{"a negative window", importer.ScanOptions{Plan: valid, Window: importer.Window{Offset: -1}}, "window begins"},
		{"a window past the render bound", importer.ScanOptions{Plan: valid, Window: importer.Window{Limit: importer.MaxScanWindowRows + 1}}, "renders at most"},
		{"a batch past the record bound", importer.ScanOptions{Plan: valid, BatchRecords: importer.MaxBatchRecords + 1}, "between 1 and"},
		{"a batch past the byte bound", importer.ScanOptions{Plan: valid, BatchBytes: importer.MaxBatchBytes + 1}, "between 1 and"},
		{"a negative batch", importer.ScanOptions{Plan: valid, BatchRecords: -1}, "between 1 and"},
		{"declared container members", importer.ScanOptions{Plan: withMembers}, "one declared stream"},
		{"an undeclared plan", importer.ScanOptions{}, "unsupported import plan version"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := importer.Scan(context.Background(), strings.NewReader(framedRecord(1)), c.options)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("scan error %v, want one naming %q", err, c.want)
			}
		})
	}
}

func TestScanCancellationIsAcknowledgedWithTheCountsItReached(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	record := []byte(framedRecord(1))
	// Cancelling after a bounded number of bytes have been read, well past the
	// first window the reader fills, places the cancellation inside the stream
	// and proves the acknowledgement does not wait for the end of it.
	source := &repeated{record: record, left: 100_000, cancelAfter: 256 << 10, cancel: cancel}
	result, err := importer.Scan(ctx, source, importer.ScanOptions{Plan: mllpPlan(t), BatchRecords: 8})
	cancel()
	if !errors.Is(err, importer.ErrScanCancelled) {
		t.Fatalf("scan error %v, want %v", err, importer.ErrScanCancelled)
	}
	if result.Records == 0 {
		t.Error("a cancelled scan reports nothing about what it read")
	}
	if result.Records >= 100_000 {
		t.Errorf("a cancelled scan read the whole %d record stream", result.Records)
	}
	if result.Bytes >= int64(100_000*len(record)) {
		t.Errorf("a cancelled scan read %d of %d bytes", result.Bytes, 100_000*len(record))
	}
}

func TestScanNamesTheSourceByteBoundAFramedStreamIsPast(t *testing.T) {
	// An MLLP member is stored whole as one source, so a framed stream past the
	// 16 MiB source bound is past it however few frames it holds. A scanned
	// record is already held to that same bound, so no other framing can reach
	// it — which is why this is the one framing the check is about.
	plan := mllpPlan(t)
	filler := strings.Repeat("A", 1<<20)
	frame := []byte("\x0b" + fmt.Sprintf(scanMessage, 1, 1) + "ZFI|1|" + filler + "\r\x1c\r")
	frames := (importer.MaxRecordBytes / len(frame)) + 4
	result := scan(t, &repeated{record: frame, left: frames}, importer.ScanOptions{Plan: plan})
	if result.Bytes <= int64(importer.MaxRecordBytes) {
		t.Fatalf("the stream is %d bytes, which is not past the %d byte source bound", result.Bytes, importer.MaxRecordBytes)
	}
	past := result.ExceedsCase(plan)
	if len(past) != 1 || !strings.Contains(past[0], "source bytes") {
		t.Fatalf("exceeded %v, want the source byte bound alone", past)
	}
	// Every individual frame is well inside it, so the bound being named is a
	// fact about the member the case would store, not about any one record.
	if int64(len(frame))*int64(frames) != result.Bytes || len(frame) > importer.MaxRecordBytes {
		t.Fatalf("%d frames of %d bytes is not the %d byte stream that was read", frames, len(frame), result.Bytes)
	}
}

func TestScanNamesTheCaseBoundsAStreamIsAlreadyPast(t *testing.T) {
	// Batch framing stores one case bundle source per record, so a stream of
	// more records than a case holds sources is named as past that bound. The
	// bound is reported, never widened: nothing here writes a case.
	plan := batchPlan(t)
	result := scan(t, strings.NewReader(stream(200, batchRecord)), importer.ScanOptions{Plan: plan})
	past := result.ExceedsCase(plan)
	if len(past) != 1 || !strings.Contains(past[0], "sources") {
		t.Fatalf("exceeded %v, want the source bound alone", past)
	}
	// The same records under MLLP framing are one source, so nothing is past.
	framed := mllpPlan(t)
	inside := scan(t, strings.NewReader(stream(200, framedRecord)), importer.ScanOptions{Plan: framed})
	if past := inside.ExceedsCase(framed); len(past) != 0 {
		t.Fatalf("exceeded %v, want nothing", past)
	}
}

// repeated is a stream of one record repeated, so a test can read a large
// stream without holding one. cancelAfter cancels the reader's own context once
// that many bytes have been handed out, which is how a cancellation is placed
// inside a stream rather than before it.
type repeated struct {
	record      []byte
	left        int
	pos         int
	read        int
	cancelAfter int
	cancel      context.CancelFunc
}

func (r *repeated) Read(p []byte) (int, error) {
	if r.left == 0 && r.pos == 0 {
		return 0, io.EOF
	}
	written := 0
	for written < len(p) && (r.pos > 0 || r.left > 0) {
		if r.pos == 0 {
			r.left--
		}
		n := copy(p[written:], r.record[r.pos:])
		written += n
		if r.pos += n; r.pos == len(r.record) {
			r.pos = 0
		}
	}
	r.read += written
	if r.cancel != nil && r.read >= r.cancelAfter {
		r.cancel()
	}
	return written, nil
}

// writeStream puts a stream on disk so the same bytes can be read by the
// whole-member extractor, which reads containers rather than readers.
func writeStream(t *testing.T, corpus string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stream")
	if err := os.WriteFile(path, []byte(corpus), 0o600); err != nil {
		t.Fatalf("write stream: %v", err)
	}
	return path
}
