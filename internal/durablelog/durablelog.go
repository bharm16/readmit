// Package durablelog is the one append machine behind the journals that must
// survive a crash: a durable run's journal and a capture's journal share it
// rather than restating it. It owns what must never differ between them — the
// hash-chained envelope every record carries, the write-then-sync append with
// its short-write detection, the declared size bound, and the read-back scan
// that tells a torn trailing record from a broken chain. What the two journals
// legitimately differ in — which records mean what, and what a run or a
// capture does between them — stays with their callers.
//
// The chain starts at the digest of the plan document a journal opens beside,
// so the first record is bound to the plan it executes. A record that could
// not be written, synced or encoded is a sticky failure: the journal no longer
// states what happened, and every later append is refused with the same error.
package durablelog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"io"
	"time"
)

// Envelope is the part of every record this package owns: the record's place
// in the chain, the digest of the record before it, and the moment it was
// synced. A journal's record type embeds it as its first field, so the bytes a
// record marshals to are exactly what the caller's own struct would produce.
type Envelope struct {
	Sequence int       `json:"sequence"`
	Previous string    `json:"previous"`
	At       time.Time `json:"at"`
}

// GetEnvelope is promoted from an embedded Envelope. It is how this package
// stamps a record it knows nothing else about.
func (e *Envelope) GetEnvelope() *Envelope { return e }

// Record is one journal record: the caller's own fields over the Envelope
// this package stamps.
type Record interface {
	GetEnvelope() *Envelope
}

// File is what one journal append writes through. It is the narrowest surface
// a size-limited stand-in for a disk takes in tests.
type File interface {
	io.Writer
	Sync() error
}

// Messages are the errors a journal reports, in the words its caller owns. The
// kernel refuses with them rather than with its own, so a durable run and a
// capture keep the diagnostics their recovery reports today. Each is made
// sticky by the writer that returns it.
type Messages struct {
	Limit  error // the journal reached its declared size
	Sync   error // a record could not be written or synced
	Encode error // a record could not be marshaled
}

// Writer appends hash-chained, newline-terminated JSON records to one journal
// file. Every append is the caller's serialization boundary; a Writer is not
// safe for concurrent use by itself.
type Writer struct {
	journal  File
	limit    int
	bytes    int
	sequence int
	previous string
	failed   error
	messages Messages
}

// NewWriter begins a journal whose chain starts at previous, which is the
// digest of the plan document the journal opens beside. limit is the declared
// size of the whole file, checked before any byte of a refused record is
// written, so a send whose intent it refused was never attempted.
func NewWriter(journal File, previous string, limit int, messages Messages) *Writer {
	return &Writer{journal: journal, limit: limit, previous: previous, messages: messages}
}

// Append marshals one record, stamps its envelope, and syncs it before
// returning. The digest the chain carries is of the record's marshaled bytes
// without the trailing newline.
func (w *Writer) Append(r Record) error {
	if w.failed != nil {
		return w.failed
	}
	env := r.GetEnvelope()
	env.Sequence = w.sequence + 1
	env.Previous = w.previous
	env.At = time.Now().UTC()
	raw, err := json.Marshal(r, json.Deterministic(true))
	if err != nil {
		w.failed = w.messages.Encode
		return w.failed
	}
	if w.bytes+len(raw)+1 > w.limit {
		w.failed = w.messages.Limit
		return w.failed
	}
	n, err := w.journal.Write(append(raw, '\n'))
	if err == nil && n != len(raw)+1 {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = w.journal.Sync()
	}
	if err != nil {
		w.failed = w.messages.Sync
		return w.failed
	}
	w.bytes += len(raw) + 1
	w.sequence++
	w.previous = Digest(raw)
	return nil
}

// Err is the first durability failure this writer hit, if any. It is sticky:
// once a record could not be synced, the journal no longer states what
// happened.
func (w *Writer) Err() error { return w.failed }

// Sequence is the number of records appended so far.
func (w *Writer) Sequence() int { return w.sequence }

// Digest is the chain digest of one marshaled record or plan document.
func Digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

// Scan verifies the chain of one journal read back. It splits the complete
// lines of data, decodes each into a record — a nil return means the line does
// not decode — checks the record's envelope against the chain, and hands it to
// visit in order, with the sequence the record must be at. It reports whether
// the trailing record was torn mid-write, which a crash may have interrupted
// and which is never assumed harmless; a line that does not decode, or whose
// envelope breaks the chain, is corrupt, not torn.
func Scan(data []byte, previous string, corrupt error, decode func(line []byte) Record, visit func(sequence int, r Record) error) (truncated bool, err error) {
	lines := bytes.Split(data, []byte{'\n'})
	truncated = len(lines[len(lines)-1]) > 0
	sequence := 0
	for _, line := range lines[:len(lines)-1] {
		sequence++
		r := decode(line)
		if r == nil {
			return truncated, corrupt
		}
		env := r.GetEnvelope()
		if env.Sequence != sequence || env.Previous != previous || env.At.IsZero() {
			return truncated, corrupt
		}
		previous = Digest(line)
		if err = visit(sequence, r); err != nil {
			return truncated, err
		}
	}
	return truncated, nil
}
