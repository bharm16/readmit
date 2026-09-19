package observesource

import (
	"context"
	"errors"
	"os"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observewindow"
)

// captureReader observes a downstream HL7 capture: the case bundle a receiver
// already sealed, read back through the one verifying reader every other
// command opens a case with. It captures nothing itself and writes nothing into
// the case, so the evidence an observation is decided from is the evidence as
// it was retained.
//
// This is what lets a regression assertion inspect the system actually under
// test. The downstream system accepts HL7 the way it always did and is asked
// for no acknowledgement receipt, no ledger export and no readmit-specific
// message; readmit captures what was sent to it, and the observation reads that
// capture under a declared field mapping.
//
// Like a file export it is never retried inside one read: a read that found no
// capture found none, and reading again until one appears is waiting for a
// convenient answer rather than observing the source. A capture is a sealed,
// bounded artifact — a receiver writes it when its own declared capacity is
// spent — so what repeated sampling establishes here is that the state held
// still, exactly as it does for every other source.
type captureReader struct {
	capture Capture
	maxAge  time.Duration
}

func (r *captureReader) close() {}

// read takes one bounded read of the capture and reports exactly what it was.
// A capture that is absent, one that could not be opened and verified, one
// holding more occurrences than the declared bound, one whose state is older
// than the declared freshness bound, one holding an occurrence the case itself
// could not parse, and one whose occurrences in scope do not hold the declared
// key each report that condition; none of them reports zero records.
func (r *captureReader) read(_ context.Context) attempt {
	taken := attempt{at: time.Now(), record: Evidence{Kind: DownstreamCapture, Attempts: 1}}
	info, err := os.Stat(r.capture.Path)
	switch {
	case err != nil && os.IsNotExist(err):
		return failure(taken, observewindow.SampleMissing, "the declared capture is not present, so no state could be obtained")
	case err != nil:
		return failure(taken, observewindow.SampleFailed, "the declared capture could not be inspected")
	case !info.IsDir():
		return failure(taken, observewindow.SampleFailed, "the declared capture is not a retained case directory")
	}
	// Opening verifies the manifest, the identity, every payload digest and
	// the metadata derived from the bytes before any evidence is returned. A
	// capture that does not verify is a read that errored, never a capture
	// that held nothing.
	evidence, err := bundle.Open(r.capture.Path)
	if err != nil {
		return failure(taken, observewindow.SampleFailed, "the declared capture could not be opened and verified as retained case evidence")
	}
	taken.at = time.Now()
	taken.record.Bytes = evidenceBytes(evidence)
	if len(evidence.Events) > r.capture.MaxOccurrences {
		return failure(taken, observewindow.SampleTruncated, captureBoundRefusal)
	}
	// A capture states how old its state is through the times it recorded. A
	// capture that recorded none cannot place its own state, which is a
	// reading with no single meaning rather than a fresh one.
	earliest, current, ok := recordedSpan(evidence)
	if !ok || current.After(taken.at) {
		return failure(taken, observewindow.SampleAmbiguous, "the capture records no time this read can place its state at")
	}
	// Unlike a document, a capture is an ordered log: it says not only when its
	// state was current but how far back that state reaches. The window's
	// watermark is applied to both ends, so a capture that also holds evidence
	// from before the window is reported rather than counted.
	taken.from = earliest
	if !dateState(&taken, current, r.maxAge) {
		return failure(taken, observewindow.SampleStale, "the capture's state is older than the declared freshness bound")
	}
	// The material is the capture's own identity marker, retained byte for
	// byte. Where the other readers retain the bytes a source answered with
	// because nothing else kept them, a capture is already immutable evidence
	// readmit retained under [ADR-0002], reachable at the path the declaration
	// names. Copying it here would put the same messages in a second place that
	// nothing verifies and nothing else reads, so the snapshot names which
	// evidence a read observed and the case itself stays the thing to
	// re-examine. Two reads of an unchanged capture name the same material.
	//
	// [ADR-0002]: ../../docs/adr/0002-case-bundles-are-directories-not-a-database.md
	taken.evidence = map[string][]byte{"identity.sha256": []byte(evidence.Identity + "\n")}
	keys, err := r.capture.recordKeys(evidence)
	if err != nil {
		return failure(taken, observewindow.SampleAmbiguous, err.Error())
	}
	taken.status = observewindow.Observed
	taken.keys = keys
	return taken
}

// captureBoundRefusal is the one refusal a capture past its declared read
// bound gets, wherever that bound is applied. Reading the capture live and
// deriving its records again later are the same question about the same
// declaration, so they answer it in the same words.
const captureBoundRefusal = "the capture holds more occurrences than the declared read bound, so a prefix of it would be read rather than the source"

// recordKeys maps the declared position onto every occurrence in scope and
// returns the keys it read. It is the whole of what leaves the capture: no
// other field is read, so nothing else about a message reaches a digest, a
// correlation or a completion record.
//
// It belongs to the declaration rather than to the reader because it is a
// function of what was declared and what the case holds, and it is asked twice:
// once when the capture is observed, and once when the records that observation
// settled on are derived again.
//
// An occurrence the case preserved without parsing leaves the capture without a
// single reading, whatever scope was declared. Whether it belonged to the scope
// is exactly what nobody could decode, and reporting the records around it as
// the capture's state would present "we could not read this" as "this is not
// there" — the one conflation these contracts exist to prevent.
func (c Capture) recordKeys(evidence *bundle.Bundle) ([]string, error) {
	selector, err := c.Selector()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(evidence.Events))
	for _, event := range evidence.Events {
		if event.Kind == bundle.Unparsed || event.Fields == nil {
			return nil, errors.New("the capture holds an occurrence it could not parse, so what is in scope cannot be read one way")
		}
		if !c.inScope(string(event.Kind)) {
			continue
		}
		key, err := captureKey(evidence, event, selector)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// captureKey reads one occurrence's declared position as comparable text. A
// position that is not present, one that does not decode, and one that is not a
// bounded printable key each leave the capture without a single reading, in the
// same words a document whose records do not hold the declared key is refused
// in.
func captureKey(evidence *bundle.Bundle, event bundle.Event, selector hl7.Selector) (string, error) {
	raw, err := evidence.Raw(event.ID)
	if err != nil {
		return "", errors.New("a verified occurrence of this capture could not be read back")
	}
	document, err := hl7.Parse(raw, hl7.Options{Terminator: event.Terminator})
	if err != nil {
		return "", errors.New("a verified occurrence of this capture could not be parsed")
	}
	value, err := document.Select(0, selector)
	if err != nil || value.State != hl7.Present {
		return "", errors.New("an occurrence of this capture does not hold the declared record key")
	}
	decoded, err := hl7.Decode(document.Bytes(value.Span), document.Messages[0].Delimiters)
	if err != nil || !utf8.Valid(decoded) {
		return "", errors.New("a record key of this capture does not decode to text")
	}
	key := string(decoded)
	if !printableKey(key) {
		return "", errRecordKey
	}
	return key, nil
}

// recordedSpan is the stretch of time the capture's state covers: the earliest
// and the latest time the case recorded anywhere in it. The latest is when that
// state was current; the earliest is how far back it reaches, which is what
// decides whether the capture also holds evidence from before the window.
//
// A session a receiver declared but recorded no occurrence in covers only its
// own start. An imported, generated or derived case records neither, because
// nothing in it observed when a downstream system received anything, and a
// capture that cannot date its own state is ambiguous rather than current.
func recordedSpan(evidence *bundle.Bundle) (earliest, latest time.Time, dated bool) {
	for _, event := range evidence.Events {
		if event.ObservedAt == nil {
			continue
		}
		if earliest.IsZero() || event.ObservedAt.Before(earliest) {
			earliest = *event.ObservedAt
		}
		if event.ObservedAt.After(latest) {
			latest = *event.ObservedAt
		}
	}
	if !latest.IsZero() {
		return earliest, latest, true
	}
	if started := evidence.Manifest.Provenance.StartedAt; started != nil && !started.IsZero() {
		return *started, *started, true
	}
	return time.Time{}, time.Time{}, false
}

// evidenceBytes is how much retained evidence this read covered, as the case's
// own manifest records it. It is a count, never a value: no payload, no field
// and no path crosses into the record beside a retained read.
func evidenceBytes(evidence *bundle.Bundle) int {
	total := 0
	for _, source := range evidence.Manifest.Sources {
		total += source.Size
	}
	return total
}
