package importer

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/bharm16/readmit/internal/hl7"
)

// FuzzStreamRecords holds the streaming divider to the contract the whole-member
// divider states: concatenating a stream's records in order reproduces the
// stream byte for byte. A divider that dropped, duplicated or overlapped bytes
// would still produce plausible counts, and nothing else would notice.
func FuzzStreamRecords(f *testing.F) {
	f.Add([]byte("\x0bMSH|^~\\&|A\x1c\r\x0bMSH|^~\\&|B\x1c\r"), uint8(0))
	f.Add([]byte("MSH|^~\\&|A\rPID|1\rMSH|^~\\&|B\r"), uint8(1))
	f.Add([]byte("BHS|^~\\&|\rMSH|^~\\&|A\rBTS|1\r"), uint8(2))
	f.Add([]byte("MSH|^~\\&|only\r"), uint8(3))
	f.Add([]byte{}, uint8(1))
	f.Add([]byte("\r\r\rMSH"), uint8(1))
	f.Fuzz(func(t *testing.T, data []byte, framing uint8) {
		plans := []Plan{
			{Framing: MLLPFraming},
			{Framing: BatchFraming, BatchBoundary: SegmentStart},
			{Framing: BatchFraming, BatchBoundary: HL7Batch},
			{Framing: RawFraming},
		}
		plan := plans[int(framing)%len(plans)]
		plan.Terminator = hl7.CR
		split, err := recordSplit(plan)
		if err != nil {
			t.Fatalf("declared framing %q has no divider: %v", plan.Framing, err)
		}
		reader := &recordReader{source: bytes.NewReader(data), split: split, buf: make([]byte, 0, readChunkBytes)}
		covered := int64(0)
		for records := 0; ; records++ {
			if records > len(data)+1 {
				t.Fatalf("divided %d bytes into more than %d records", len(data), records)
			}
			record, offset, err := reader.next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				// The only other refusal is the record bound, which a stream
				// this small cannot reach.
				t.Fatalf("dividing %d bytes: %v", len(data), err)
			}
			if len(record) == 0 {
				t.Fatal("a record with no bytes would never advance the stream")
			}
			if offset != covered {
				t.Fatalf("record begins at %d, want %d", offset, covered)
			}
			covered += int64(len(record))
		}
		if covered != int64(len(data)) {
			t.Fatalf("records cover %d of %d bytes", covered, len(data))
		}
	})
}
