package observesource

import (
	"errors"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/observewindow"
)

// Relinking a completed observation to the records it decided on.
//
// A completion record retains sample counts, state digests and the
// correlations a run declared — not the ordered list of keys an observation
// held. That is deliberate and it stays that way:
// `readmit-observation-completion/v1` gains no member here, exactly as
// [ADR-0003] requires of every contract that has shipped. So a caller that
// needs the keys themselves does not read them out of the record; it derives
// them again from the evidence the observation read, and then proves the
// derivation against what the record already retains.
//
// Only a downstream capture can be relinked, and the asymmetry is real rather
// than an unfinished case. A capture is evidence readmit itself retained under
// [ADR-0002]: a sealed case directory, opened through the one verifying reader
// every other command opens a case with, still present at the path the source
// declares. A file export and an HTTP response are somebody else's material at
// a moment that has passed — the snapshot keeps the bytes that were read, but
// those bytes are a copy nothing re-verifies against the source, and the
// source itself has moved on. Reporting keys derived from either as though
// they were the observation's own records would present a second reading as
// the reading that was made. So those two are refused by name.
//
// [ADR-0002]: ../../docs/adr/0002-case-bundles-are-directories-not-a-database.md
// [ADR-0003]: ../../docs/adr/0003-specs-are-strict-json-with-typed-operators.md

// ErrUnrelinkableSource is the refusal a file export and an HTTP API get. It
// is a named error rather than a sentence so a caller can tell "this release
// cannot reproduce these records" apart from "this capture disagrees with its
// completion", which are different facts about different evidence.
var ErrUnrelinkableSource = errors.New("only a downstream capture's records can be derived again from the evidence an observation read")

// ErrRelinkDisagrees is what a capture that no longer is the evidence its
// completion was decided from produces. It is never a count of zero and never
// a shorter list: evidence that changed underneath a verdict is unusable, not
// smaller.
var ErrRelinkDisagrees = errors.New("the declared capture no longer holds the records this completion was decided from")

// Relink returns the ordered record keys one completed observation decided on,
// derived again from the capture the source declares.
//
// It opens the capture through the verifying case reader, reads the same
// declared position out of the same declared scope the collector read, and
// then checks the result against the completion: the number of records the
// window settled on, and the state digest of the sample it settled on. Both
// are taken over the keys alone, so a capture that agrees with them held the
// same records. Order is not separately proven — the digest is over the sorted
// keys — but it is reproduced by the same derivation over the same verified,
// immutable bundle, which is what makes an ordered question answerable at all.
//
// Nothing is written, nothing is sent, and the capture is left exactly as it
// was. A completion that did not complete names no records and is refused
// here rather than answered with the count it never settled.
func Relink(source Source, completion observewindow.Completion) ([]string, error) {
	if err := source.Validate(); err != nil {
		return nil, err
	}
	if !completion.Trustworthy() {
		return nil, errors.New("an observation that did not complete names no records")
	}
	if source.Observes != completion.Source {
		return nil, errors.New("the declared source is not the source this completion observed")
	}
	if source.Capture == nil {
		return nil, ErrUnrelinkableSource
	}
	if len(completion.Samples) == 0 {
		return nil, errors.New("a completion carrying no sample settled on no state")
	}
	evidence, err := bundle.Open(source.Capture.Path)
	if err != nil {
		return nil, errors.New("the declared capture could not be opened and verified as retained case evidence")
	}
	if len(evidence.Events) > source.Capture.MaxOccurrences {
		return nil, errors.New(captureBoundRefusal)
	}
	keys, err := source.Capture.recordKeys(evidence)
	if err != nil {
		return nil, err
	}
	settled := completion.Samples[len(completion.Samples)-1]
	if len(keys) != completion.RecordsObserved || stateDigest(keys) != settled.StateDigest {
		return nil, ErrRelinkDisagrees
	}
	return keys, nil
}
