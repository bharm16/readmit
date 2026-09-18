package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"strconv"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

// Scanning a stream is the same reading of framing that Extract applies to a
// container member, arranged so that what it costs in memory is a property of
// the bounds below rather than of the stream's length. Extract answers "what
// would this import write", holds every extracted source, and is therefore
// bounded by the evidence one case may hold. Scan answers "what does this
// stream contain", holds one record and one parsing batch, and is therefore
// bounded by nothing the stream can state.
//
// These bounds extend the container bounds above; none of them replaces one. A
// scan writes no evidence, so the case bundle's own source, occurrence and
// evidence limits still decide what an import of the same bytes may write, and
// ExceedsCase reports which of them a scanned stream is already past.
const (
	// MaxRecordBytes holds one streamed record to the same 16 MiB bound a case
	// bundle source is held to. A record past it is refused rather than
	// buffered: a stream that never reaches its declared boundary is exactly
	// the input that would otherwise be read whole into memory.
	MaxRecordBytes = bundle.MaxSourceBytes
	// MaxBatchRecords and MaxBatchBytes bound one parsing batch. Records are
	// copied into a batch, parsed, counted and released before the next batch
	// is read, so decoding cost never accumulates across a stream.
	MaxBatchRecords = 256
	MaxBatchBytes   = 8 << 20
	// MaxStreamBytes bounds one scanned stream. The performance envelope
	// proposed in #25 is 5 GiB for one project, so this leaves that corpus
	// inside the bound and still refuses an unbounded stream rather than
	// reading one forever.
	MaxStreamBytes = 8 << 30
	// MaxScanWindowRows bounds the rows one scan retains for rendering. It is
	// the bound internal/grid renders one window of a case under: a scan of a
	// million records answers with a window of it, never with every row.
	MaxScanWindowRows = 200
	// readChunkBytes is one read from the stream and the initial capacity of
	// the read window.
	readChunkBytes = 64 << 10
)

// ResidentBound is the most one scan holds at once: the read window grown to
// its largest, one record copied into a parsing batch, and one batch. It is the
// property a scan demonstrates — the number does not move when the stream gets
// longer — and it is what PeakResidentBytes is compared against.
const ResidentBound = MaxRecordBytes + readChunkBytes + MaxBatchBytes + MaxRecordBytes

// The three named scan refusals, each distinct from a declaration the bytes
// contradict. A scan that stops for any of them has written nothing.
var (
	// ErrRecordBound reports a stream whose declared framing does not reach a
	// record boundary within the 16 MiB record bound.
	ErrRecordBound = errors.New("a scanned record exceeds the 16 MiB record limit before its declared boundary")
	// ErrStreamBound reports a stream longer than one scan reads.
	ErrStreamBound = errors.New("a scanned stream exceeds the " + strconv.Itoa(MaxStreamBytes>>30) + " GiB stream limit")
	// ErrScanCancelled reports a scan the caller cancelled. The counts reached
	// before the cancellation are returned with it, so a cancellation is
	// reported rather than quietly becoming a complete answer.
	ErrScanCancelled = errors.New("scan cancelled")
)

// Window is the bounded range of scanned records a caller renders. A scan
// retains the rows inside it and discards the rest as it goes, so rendering a
// window of a large stream costs what the window costs.
type Window struct {
	Offset int
	Limit  int
}

// Row is one scanned record as a window renders it: where it began in the
// stream, how large it was, and what the parsing batch found inside it. It
// carries no byte of the stream, no decoded value and no name.
type Row struct {
	Ordinal     int   `json:"ordinal"`
	Offset      int64 `json:"offset"`
	Size        int   `json:"size"`
	Occurrences int   `json:"occurrences"`
	Decoded     int   `json:"decoded"`
	Undecodable int   `json:"undecodable"`
}

// Progress is what a running scan reports. It is counts only: a progress report
// names no file, repeats no declaration and carries no byte of the stream, so
// it is safe on a diagnostic stream by construction.
type Progress struct {
	Bytes       int64
	Records     int64
	Occurrences int64
	Batches     int64
}

// ScanOptions are the declarations one scan runs under. Plan is the same
// readmit-import-plan/v1 an import declares, so a scan reports what an import
// of the same bytes would find rather than answering under a second reading of
// framing. Report is called once per completed parsing batch; the final counts
// are the returned result, and a nil Report reports nothing.
type ScanOptions struct {
	Plan         Plan
	Window       Window
	BatchRecords int
	BatchBytes   int
	Report       func(Progress)
}

// ScanResult is everything one scan learned. A stream with no bytes has no
// records, so every count is zero: an import of an empty member writes one
// empty quarantined source, because a case names what it was given, and a scan
// names what it read. Rows holds only the requested
// window; every other member is a count over the whole stream. Bytes is what
// was read from the stream, which on a cancelled scan is a little ahead of what
// was divided into records: a cancellation stops the reading, it does not
// unread the window that had already been filled.
type ScanResult struct {
	Bytes             int64
	Records           int64
	Occurrences       int64
	Decoded           int64
	Undecodable       int64
	Batches           int64
	PeakResidentBytes int
	SHA256            string
	Window            Window
	Rows              []Row
}

// ExceedsCase names the case bundle bounds this stream is already past, in the
// order the case bundle declares them, and is empty when an import of the same
// bytes would fit. A scan never widens those bounds: reading a larger stream
// with bounded memory is a different statement from writing one into a case,
// and reporting the second because the first succeeded would be a claim about
// evidence that nothing checked.
func (r ScanResult) ExceedsCase(plan Plan) []string {
	// An MLLP member is stored whole as one source the case bundle divides into
	// frames; every other framing stores one source per record, and a scanned
	// record is already held to the bound one source is, so the largest source
	// those would write cannot be past it.
	sources, sourceBytes := r.Records, int64(0)
	if plan.Framing == MLLPFraming {
		sources, sourceBytes = min(r.Records, 1), r.Bytes
	}
	var past []string
	if sources > int64(bundle.MaxSources) {
		past = append(past, "sources ("+strconv.Itoa(bundle.MaxSources)+")")
	}
	if r.Occurrences > int64(bundle.MaxEvents) {
		past = append(past, "occurrences ("+strconv.Itoa(bundle.MaxEvents)+")")
	}
	if sourceBytes > int64(bundle.MaxSourceBytes) {
		past = append(past, "source bytes ("+strconv.Itoa(bundle.MaxSourceBytes)+")")
	}
	if r.Bytes > int64(bundle.MaxEvidenceBytes) {
		past = append(past, "evidence bytes ("+strconv.Itoa(bundle.MaxEvidenceBytes)+")")
	}
	return past
}

// Scan reads one stream under one declared plan, dividing it into records by
// the declared framing, parsing them in bounded batches, and retaining only the
// requested window of rows.
//
// It never holds the stream: the read window, one record and one parsing batch
// are all that is resident, whatever the stream's length, and PeakResidentBytes
// reports the largest of that at any instant. A cancelled context stops before
// the next record and returns the counts reached so far with ErrScanCancelled,
// so a cancellation is acknowledged within one record rather than at the end of
// the stream.
func Scan(ctx context.Context, source io.Reader, options ScanOptions) (ScanResult, error) {
	if err := options.Plan.Validate(); err != nil {
		return ScanResult{}, err
	}
	if len(options.Plan.Members) != 0 {
		return ScanResult{}, errors.New("a scan reads one declared stream; declared members select the entries of a folder or archive import")
	}
	if options.Window.Offset < 0 {
		return ScanResult{}, errors.New("a window begins at or after the first scanned record")
	}
	if options.Window.Limit < 0 || options.Window.Limit > MaxScanWindowRows {
		return ScanResult{}, errors.New("a window renders at most " + strconv.Itoa(MaxScanWindowRows) + " scanned records")
	}
	records, size := options.BatchRecords, options.BatchBytes
	if records == 0 {
		records = MaxBatchRecords
	}
	if size == 0 {
		size = MaxBatchBytes
	}
	if records < 1 || records > MaxBatchRecords {
		return ScanResult{}, errors.New("a parsing batch holds between 1 and " + strconv.Itoa(MaxBatchRecords) + " records")
	}
	if size < 1 || size > MaxBatchBytes {
		return ScanResult{}, errors.New("a parsing batch holds between 1 and " + strconv.Itoa(MaxBatchBytes) + " bytes")
	}
	split, err := recordSplit(options.Plan)
	if err != nil {
		return ScanResult{}, err
	}
	s := &scanner{
		reader:       &recordReader{source: source, split: split, buf: make([]byte, 0, readChunkBytes)},
		plan:         options.Plan,
		batchRecords: records,
		batchBytes:   size,
		report:       options.Report,
		digest:       sha256.New(),
		result:       ScanResult{Window: options.Window, Rows: []Row{}},
		options:      hl7.Options{Format: options.Plan.Framing.storedFormat(), Terminator: options.Plan.Terminator},
	}
	return s.run(ctx)
}

// declaredStart refuses a stream whose leading bytes contradict the declared
// framing, before any of it is divided. It is the streaming form of the two
// checks recordStarts makes on a member's first bytes, applied as soon as they
// have been read rather than after a whole record has been buffered.
func (s *scanner) declaredStart() error {
	head, err := s.reader.peek(4)
	if err != nil {
		return err
	}
	if framed := len(head) > 0 && head[0] == 0x0b; framed != (s.plan.Framing == MLLPFraming) {
		return ErrDeclaredFraming
	}
	if s.plan.Framing == BatchFraming && s.plan.BatchBoundary == HL7Batch && !segmentAt(head, 0, []string{"FHS", "BHS"}) {
		return ErrDeclaredFraming
	}
	return nil
}

// declaredDivision refuses a record the declaration could only have produced by
// guessing a message boundary, which is the streaming form of recordStarts'
// check that the declared terminator reaches every message header the member
// plainly holds. A record may hold no message header — the bytes before the
// first boundary, and a batch envelope segment, are records in their own right
// — but a header it holds anywhere other than its own start is a header the
// declared terminator did not reach.
//
// It does not apply the case bundle's own source limit. A scan writes no
// evidence, so how many records one import may store is reported by
// ExceedsCase rather than refused here.
func (s *scanner) declaredDivision(record []byte) error {
	if s.plan.Framing == MLLPFraming {
		return nil
	}
	headers := headerStarts(record, 2)
	if len(headers) > 1 {
		return ErrAmbiguousBatch
	}
	if s.plan.Framing == BatchFraming && len(headers) == 1 && headers[0] != 0 {
		return ErrAmbiguousBatch
	}
	return nil
}

// batched is one record copied into the current parsing batch: where it began
// in the stream and where its bytes are in the batch's own arena.
type batched struct {
	offset int64
	from   int
	to     int
}

// scanner holds one scan: the bounded reader, the current parsing batch and the
// counts. The batch arena is reused between batches, so the copy a batch needs
// is made once rather than once per batch.
type scanner struct {
	reader       *recordReader
	plan         Plan
	batchRecords int
	batchBytes   int
	report       func(Progress)
	digest       hash.Hash
	result       ScanResult
	options      hl7.Options
	arena        []byte
	batch        []batched
}

func (s *scanner) run(ctx context.Context) (ScanResult, error) {
	if err := s.declaredStart(); err != nil {
		return ScanResult{}, err
	}
	for {
		if err := ctx.Err(); err != nil {
			// The records already read are records that were read. Parsing the
			// pending batch before stopping reports them rather than losing
			// them to the moment the cancellation arrived, and it is one more
			// bounded batch, never the rest of the stream.
			if len(s.batch) > 0 {
				s.parse()
			}
			return s.finish(), ErrScanCancelled
		}
		record, offset, err := s.reader.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ScanResult{}, err
		}
		if err := declaredEncoding(s.plan.Encoding, record); err != nil {
			return ScanResult{}, err
		}
		if err := s.declaredDivision(record); err != nil {
			return ScanResult{}, err
		}
		if len(s.batch) > 0 && (len(s.batch) >= s.batchRecords || len(s.arena)+len(record) > s.batchBytes) {
			s.parse()
		}
		s.batch = append(s.batch, batched{offset: offset, from: len(s.arena), to: len(s.arena) + len(record)})
		s.arena = append(s.arena, record...)
	}
	if len(s.batch) > 0 {
		s.parse()
	}
	return s.finish(), nil
}

// parse decodes one bounded batch, counts what it found, keeps the rows the
// window asked for, and releases the batch. Nothing a batch decoded outlives it.
func (s *scanner) parse() {
	for _, entry := range s.batch {
		record := s.arena[entry.from:entry.to]
		s.digest.Write(record)
		decoded, undecodable := 0, 0
		for start := 0; start < len(record) || decoded+undecodable == 0; {
			end, framing := bundle.NextOccurrence(record, start, s.options.Format)
			_, err := hl7.Parse(record[start:end], s.options)
			if framing != "" || err != nil {
				undecodable++
			} else {
				decoded++
			}
			start = end
		}
		count := decoded + undecodable
		s.result.Records++
		s.result.Occurrences += int64(count)
		s.result.Decoded += int64(decoded)
		s.result.Undecodable += int64(undecodable)
		ordinal := int(s.result.Records)
		if ordinal > s.result.Window.Offset && len(s.result.Rows) < s.result.Window.Limit {
			s.result.Rows = append(s.result.Rows, Row{
				Ordinal: ordinal, Offset: entry.offset, Size: len(record),
				Occurrences: count, Decoded: decoded, Undecodable: undecodable,
			})
		}
	}
	s.result.Batches++
	s.observe()
	s.arena, s.batch = s.arena[:0], s.batch[:0]
	s.announce()
}

// observe records the largest the read window and the parsing batch have been
// at once. Both are bounded by declared constants, so this number stops growing
// long before a stream does.
func (s *scanner) observe() {
	s.result.Bytes = s.reader.consumed
	if resident := cap(s.reader.buf) + cap(s.arena); resident > s.result.PeakResidentBytes {
		s.result.PeakResidentBytes = resident
	}
}

func (s *scanner) announce() {
	if s.report != nil {
		s.report(Progress{Bytes: s.result.Bytes, Records: s.result.Records, Occurrences: s.result.Occurrences, Batches: s.result.Batches})
	}
}

func (s *scanner) finish() ScanResult {
	s.observe()
	s.result.SHA256 = hex.EncodeToString(s.digest.Sum(nil))
	return s.result
}

// recordReader reads a stream through one bounded window and returns complete
// records. The window holds the record being assembled and nothing else: it is
// compacted before every read and never grows past one record plus one chunk.
type recordReader struct {
	source   io.Reader
	split    splitFunc
	buf      []byte
	pos      int
	eof      bool
	consumed int64
	idle     int
}

// splitFunc reports the length of the first complete record in data, or zero
// when the record's declared boundary has not been read yet.
type splitFunc func(data []byte) int

// next returns the next complete record and the offset it began at in the
// stream. The returned bytes belong to the read window and stay valid only
// until the next call, so a caller that retains a record copies it.
func (r *recordReader) next() ([]byte, int64, error) {
	for {
		pending := r.buf[r.pos:]
		if n := r.split(pending); n > 0 {
			r.pos += n
			return pending[:n], r.consumed - int64(len(pending)), nil
		}
		if r.eof {
			if len(pending) == 0 {
				return nil, 0, io.EOF
			}
			r.pos = len(r.buf)
			return pending, r.consumed - int64(len(pending)), nil
		}
		if len(pending) >= MaxRecordBytes {
			return nil, 0, ErrRecordBound
		}
		if err := r.fill(); err != nil {
			return nil, 0, err
		}
	}
}

// peek returns up to n bytes at the front of the stream without consuming them,
// so a declaration the very first bytes contradict is refused before a record
// is assembled out of them.
func (r *recordReader) peek(n int) ([]byte, error) {
	for len(r.buf)-r.pos < n && !r.eof {
		if err := r.fill(); err != nil {
			return nil, err
		}
	}
	return r.buf[r.pos:min(len(r.buf), r.pos+n)], nil
}

// fill compacts the window, grows it when the record being assembled fills it,
// and reads one chunk. Growth stops at one record plus one chunk, which is what
// makes ErrRecordBound reachable rather than the process's memory.
func (r *recordReader) fill() error {
	if r.pos > 0 {
		r.buf = append(r.buf[:0], r.buf[r.pos:]...)
		r.pos = 0
	}
	if len(r.buf) == cap(r.buf) {
		grown := make([]byte, len(r.buf), min(max(2*cap(r.buf), readChunkBytes), MaxRecordBytes+readChunkBytes))
		r.buf = grown[:copy(grown, r.buf)]
	}
	n, err := r.source.Read(r.buf[len(r.buf):cap(r.buf)])
	r.buf = r.buf[:len(r.buf)+n]
	r.consumed += int64(n)
	if r.consumed > MaxStreamBytes {
		return ErrStreamBound
	}
	switch {
	case err == io.EOF:
		r.eof = true
	case err != nil:
		return errors.New("cannot read the declared stream")
	case n == 0:
		// A reader that makes no progress would otherwise be read forever.
		if r.idle++; r.idle > 100 {
			return errors.New("the declared stream stopped returning data")
		}
		return nil
	}
	r.idle = 0
	return nil
}

// recordSplit is the streaming form of recordStarts: the same declared framing,
// answered one record at a time. Raw framing declares that the stream is one
// message, so it has no boundary and a stream past the record bound is refused
// rather than read whole — which is exactly what a raw declaration over a
// corpus means.
func recordSplit(plan Plan) (splitFunc, error) {
	switch plan.Framing {
	case MLLPFraming:
		return mllpRecord, nil
	case RawFraming:
		return func([]byte) int { return 0 }, nil
	case BatchFraming:
		boundaries := header
		if plan.BatchBoundary == HL7Batch {
			boundaries = append(append([]string{}, header...), batchEnvelope...)
		}
		return segmentRecord(terminator(plan.Terminator), boundaries), nil
	}
	return nil, errors.New("a scan declares raw, mllp, or batch framing")
}

// mllpRecord ends one record at an MLLP end block and its final carriage
// return, which is the frame boundary the case bundle divides a stored MLLP
// source into occurrences at.
func mllpRecord(data []byte) int {
	n := bytes.Index(data, []byte{0x1c, '\r'})
	if n < 0 {
		return 0
	}
	return n + 2
}

// segmentRecord ends one record where the next declared boundary segment
// begins. The boundary at the start of a record begins it rather than ends it,
// so the search starts after the first complete terminator. A candidate at the
// very end of the window is not decided yet — segmentAt reads the byte after
// the identifier — so the reader fills and is asked again.
func segmentRecord(separator []byte, ids []string) splitFunc {
	return func(data []byte) int {
		for at := 0; at < len(data); {
			next := bytes.Index(data[at:], separator)
			if next < 0 {
				return 0
			}
			at += next + len(separator)
			if at+3 >= len(data) {
				return 0
			}
			if segmentAt(data, at, ids) {
				return at
			}
		}
		return 0
	}
}
