package observesource

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// MaxProduced bounds how many outbound occurrences one collection may be asked
// to correlate. It sits inside the correlation bound a completion record holds.
const MaxProduced = 512

// Options are what a caller supplies beside the declared source and window: the
// new directory the original material is retained in, the approved-destination
// policy an operator explicitly selected, and the occurrences this run produced
// that the window should account for.
type Options struct {
	// Snapshot is a new directory. The material an observation read is written
	// there unchanged and is never rewritten afterwards, so a completed window
	// can be re-examined against the bytes it was decided from.
	Snapshot string
	// Policy is the explicitly selected approved-destination document, or nil
	// when the operator selected none. It governs an HTTP endpoint exactly as
	// it governs a send: a file export reaches no destination and asks nothing.
	Policy *sendpolicy.Policy
	// Resolve turns a configured name into the addresses it resolves to at the
	// moment the question is asked. It is a parameter so a decision is always
	// about a resolution readmit performed, and so tests never depend on a name
	// outside the test.
	Resolve sendpolicy.Resolver
	// Produced names what the run sent, so the window can report what it
	// observed of each one. A produced occurrence with no correlation is
	// unknown, and unknown is not a pass.
	Produced []string
}

// attempt is what one bounded read of the source produced, before the window's
// completion rule is applied to it. Exactly one of these becomes one
// [observewindow.Sample].
type attempt struct {
	status observewindow.SampleStatus
	at     time.Time
	// asOf is when the state this attempt read was current, which every
	// observation states. A read that cannot say how old its state is has no
	// single reading and is never treated as current.
	asOf time.Time
	// from is the earliest point the state this attempt read covers. Only a
	// source that is an ordered log of its own can say: a document answers with
	// one state of one age, so its readers leave this zero and are placed by
	// asOf alone. A capture can say, and a capture that reaches back past the
	// window's watermark returned evidence from before the window.
	from time.Time
	// keys are the record keys in scope, which only an observation has. No
	// other value is read out of a record, so nothing patient-identifying
	// reaches a digest, a correlation or a completion record.
	keys []string
	// evidence is the original material this attempt read, retained exactly as
	// it was read, keyed by the name it is retained under.
	evidence map[string][]byte
	record   Evidence
}

// reader is one source's bounded read. Both implementations report a failure as
// the failure it was; neither ever reports zero records for a read that did not
// happen.
type reader interface {
	read(ctx context.Context) attempt
	close()
}

// Collect observes one declared source against one declared window and returns
// what that means, as the retained completion record internal/observewindow
// decides.
//
// It returns an error only when the inputs are not well formed or the original
// material could not be retained. Collection that simply did not succeed is not
// an error here: it is a completion whose status names how it failed, which is
// an execution error wherever it is read and never an assertion that the source
// held nothing.
func Collect(ctx context.Context, source Source, window observewindow.Window, options Options) (observewindow.Completion, error) {
	if err := source.Validate(); err != nil {
		return observewindow.Completion{}, err
	}
	if err := window.Validate(); err != nil {
		return observewindow.Completion{}, err
	}
	if err := validProduced(options.Produced); err != nil {
		return observewindow.Completion{}, err
	}
	retained, err := openSnapshot(options.Snapshot)
	if err != nil {
		return observewindow.Completion{}, err
	}
	defer retained.close()
	// A source this collector does not reach, and one an operator disabled,
	// are each reported before anything is opened. Both observed nothing, and
	// neither is an observation that there is nothing there.
	if refusal, refused := refuse(source, window); refused {
		return decideOne(window, retained, refusal)
	}
	open, err := newReader(ctx, source, retained, options)
	if err != nil {
		return observewindow.Completion{}, err
	}
	defer open.close()
	return sample(ctx, window, retained, open, options.Produced)
}

// refuse reports the one attempt a source that cannot be collected from
// produces. Support is decided against the window's own declaration, so a
// collector never quietly observes a different source than the one declared.
func refuse(source Source, window observewindow.Window) (attempt, bool) {
	now := time.Now()
	if window.Source != source.Observes {
		return attempt{status: observewindow.SampleUnsupported, at: now,
			record: Evidence{Kind: source.Observes.Kind, Note: "the declared source is not the source this window observes"}}, true
	}
	if !source.Enabled {
		return attempt{status: observewindow.SampleMissing, at: now,
			record: Evidence{Kind: source.Observes.Kind, Note: "this collector is disabled, so no state could be obtained"}}, true
	}
	return attempt{}, false
}

// decideOne records one attempt that ended collection before the window opened
// and decides what it means. The window still opens and closes: a refusal is an
// attempt that was made, not one that never happened.
func decideOne(window observewindow.Window, retained *snapshot, only attempt) (observewindow.Completion, error) {
	opened := only.at.UTC()
	sampled, err := retain(retained, 0, only)
	if err != nil {
		return observewindow.Completion{}, err
	}
	return window.Decide(observewindow.Collection{
		OpenedAt: opened, ClosedAt: time.Now().UTC(), Stop: observewindow.StopRule,
		Samples: []observewindow.Sample{sampled},
	})
}

// sample runs the declared completion rule's own sampling. It stops when the
// rule says the window completed, when a read was not an observation, when the
// declared sample limit or deadline is reached, or when it is cancelled.
//
// Whether the rule completed is asked of internal/observewindow rather than
// recomputed here, so the condition a collector stops on is the condition a
// reader re-decides the record against, and a collector cannot stop at the
// first convenient answer by holding a slightly different rule.
func sample(ctx context.Context, window observewindow.Window, retained *snapshot, open reader, produced []string) (observewindow.Completion, error) {
	collection := observewindow.Collection{Stop: observewindow.StopRule}
	next := 0
	if window.PreExisting.Declaration == observewindow.RecordedBaseline {
		baseline, err := retain(retained, next, baselineAttempt(ctx, window, open))
		if err != nil {
			return observewindow.Completion{}, err
		}
		collection.Baseline = &baseline
		next++
	}
	collection.OpenedAt = time.Now().UTC()
	interval := samplingInterval(window.Completion)
	limit, _ := time.ParseDuration(window.Completion.Deadline)
	closes := collection.OpenedAt.Add(limit)
	var observed []string
	for {
		// Every read is bounded by what is left of the window as well as by
		// the source's own timeout, so a bounded retry can never outlast the
		// deadline it is being retried inside.
		readCtx, cancel := context.WithDeadline(ctx, closes)
		taken := withWatermark(window, collection.OpenedAt, open.read(readCtx))
		expired := readCtx.Err() != nil
		cancel()
		// A read the caller stopped never completed, so it is not recorded as
		// an observation that failed: the window reports the cancellation or
		// the timeout it was stopped by. A read the window's own deadline cut
		// short is not recorded either, and the rule it did not satisfy
		// reports the deadline.
		if err := ctx.Err(); err != nil {
			collection.Stop = stopFor(err)
			break
		}
		if expired {
			break
		}
		if taken.status == observewindow.Observed {
			observed = taken.keys
		}
		sampled, err := retain(retained, next, taken)
		if err != nil {
			return observewindow.Completion{}, err
		}
		next++
		collection.Samples = append(collection.Samples, sampled)
		collection.ClosedAt = time.Now().UTC()
		decided, err := window.Decide(collection)
		if err != nil {
			return observewindow.Completion{}, err
		}
		// Incomplete is the one verdict more sampling can still change. A
		// window that completed, one whose read was not an observation and one
		// already past a limit are each settled, and continuing would be
		// sampling until a convenient answer appeared.
		if decided.Status != observewindow.Incomplete {
			break
		}
		if len(collection.Samples) >= window.Completion.MaxSamples {
			break
		}
		// Stopping before a sample that would land past the deadline reports
		// the deadline honestly. Taking one anyway would record an observation
		// the rule must then discard.
		if time.Now().Add(interval).After(closes) {
			break
		}
		if err := waitFor(ctx, interval); err != nil {
			collection.Stop = stopFor(err)
			break
		}
	}
	collection.ClosedAt = time.Now().UTC()
	collection.Correlations = correlate(produced, observed, lastObserved(collection.Samples) >= 0)
	return window.Decide(collection)
}

// baselineAttempt reads the state the window opens on. A baseline is held to
// exactly the rules a sample is: only one that was itself observed counts, and
// a baseline observing a state other than the one the window named is a state
// the window cannot read one way.
func baselineAttempt(ctx context.Context, window observewindow.Window, open reader) attempt {
	taken := open.read(ctx)
	if taken.status != observewindow.Observed {
		return taken
	}
	if stateDigest(taken.keys) != window.PreExisting.BaselineIdentity {
		taken.status = observewindow.SampleAmbiguous
		taken.record.Note = "the state before the window opened is not the baseline the window named"
		taken.keys = nil
	}
	return taken
}

// withWatermark applies the window's start state to what an observation read.
// A source answering from before the watermark cannot describe this window, so
// it is stale evidence rather than evidence of a quiet source.
//
// A declared position is an RFC 3339 instant for both of this collector's
// sources, because both state when their material was current as a time. A
// window whose position is not one is unsupported here rather than compared
// against something it does not mean.
func withWatermark(window observewindow.Window, opened time.Time, taken attempt) attempt {
	if taken.status != observewindow.Observed {
		return taken
	}
	var floor time.Time
	switch window.Watermark.Kind {
	case observewindow.DeclaredPosition:
		position, err := time.Parse(time.RFC3339, window.Watermark.Position)
		if err != nil {
			taken.status = observewindow.SampleUnsupported
			taken.record.Note = "this collector compares a declared position as an RFC 3339 instant"
			taken.keys = nil
			return taken
		}
		floor = position
	case observewindow.CollectionStart:
		floor = opened
	default:
		return taken
	}
	if taken.asOf.IsZero() || taken.asOf.Before(floor) {
		taken.status = observewindow.SampleStale
		taken.record.Note = "the state read was current before the window's watermark"
		taken.keys = nil
		return taken
	}
	// A source that reaches back past the watermark returned evidence from
	// before the window as well as evidence from inside it. Counting the whole
	// of it would attribute what was already there to this run, and silently
	// dropping the earlier part would decide the count by a filter the retained
	// record cannot show, which a reader re-deciding from the samples alone
	// could never check. Reporting the stale evidence it returned is the one
	// answer that stays re-decidable.
	if !taken.from.IsZero() && taken.from.Before(floor) {
		taken.status = observewindow.SampleStale
		taken.record.Note = "the state read reaches back past the window's watermark, so it returned evidence from before the window"
		taken.keys = nil
	}
	return taken
}

// retain writes one attempt's original material into the snapshot and turns it
// into the sample the window decides on. An observation names the material it
// read; collection that failed names none, because evidence nobody kept cannot
// be re-examined and a failed read kept none.
func retain(retained *snapshot, index int, taken attempt) (observewindow.Sample, error) {
	taken.record.Schema = EvidenceSchema
	taken.record.Status = string(taken.status)
	taken.record.Records = len(taken.keys)
	document, err := encodeEvidence(taken.record)
	if err != nil {
		return observewindow.Sample{}, err
	}
	identity, err := retained.retain(index, taken.evidence, document)
	if err != nil {
		return observewindow.Sample{}, err
	}
	// The time is recorded with its monotonic reading stripped, exactly as the
	// retained record carries it. A verdict decided from a monotonic elapsed
	// time and re-decided from a wall-clock one would disagree with itself.
	sampled := observewindow.Sample{At: taken.at.UTC(), Status: taken.status}
	if taken.status != observewindow.Observed {
		return sampled, nil
	}
	sampled.RecordCount = len(taken.keys)
	sampled.StateDigest = stateDigest(taken.keys)
	sampled.EvidenceIdentity = identity
	return sampled, nil
}

// samplingInterval is how long the collector waits between reads: short enough
// that the declared number of stable samples spans the declared quiet period,
// and never zero.
func samplingInterval(rule observewindow.Rule) time.Duration {
	quiet, err := time.ParseDuration(rule.QuietPeriod)
	if err != nil || rule.StableSamples < 2 {
		return time.Millisecond
	}
	gaps := time.Duration(rule.StableSamples - 1)
	interval := quiet / gaps
	if interval*gaps < quiet {
		interval++
	}
	if interval < time.Millisecond {
		return time.Millisecond
	}
	return interval
}

func waitFor(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// stopFor names why sampling stopped outside the completion rule. A cancelled
// window is cancellation and one that ran out of time is a timeout; neither is
// a negative application result.
func stopFor(err error) observewindow.Stop {
	if errors.Is(err, context.DeadlineExceeded) {
		return observewindow.StopTimedOut
	}
	return observewindow.StopCancelled
}

func lastObserved(samples []observewindow.Sample) int {
	for index := len(samples) - 1; index >= 0; index-- {
		if samples[index].Status == observewindow.Observed {
			return index
		}
	}
	return -1
}

// correlate maps what the run produced onto what the window observed of it.
// Correlations are recorded only from an observation: a window that observed
// nothing records no correlation, because "we did not look" is not "we looked
// and found none".
func correlate(produced, keys []string, observed bool) []observewindow.Correlation {
	if !observed || len(produced) == 0 {
		return nil
	}
	correlations := make([]observewindow.Correlation, 0, len(produced))
	for _, occurrence := range produced {
		matches := 0
		for _, key := range keys {
			if key == occurrence {
				matches++
			}
		}
		correlation := observewindow.Correlation{Produced: occurrence, Kind: observewindow.Unmatched}
		switch {
		case matches == 1:
			correlation.Kind, correlation.Observed = observewindow.Matched, occurrence
		case matches > 1:
			correlation.Kind = observewindow.AmbiguousMatch
		}
		correlations = append(correlations, correlation)
	}
	return correlations
}

// stateDigest identifies one observed state so two samples can be compared for
// stability. It is taken over the record keys in scope, in sorted order, so a
// source that lists the same records in a different order is the same state. It
// identifies a state; it does not authenticate one.
func stateDigest(keys []string) string {
	ordered := slices.Clone(keys)
	slices.Sort(ordered)
	parts := make([][]byte, 0, len(ordered))
	for _, key := range ordered {
		parts = append(parts, []byte(key))
	}
	return digestOf(stateDomain, parts)
}

// recordKeys reads the declared key out of every record of one divided
// document. A record this reader could not divide, and one that does not hold
// the declared key, each leave the document without a single reading.
func recordKeys(extraction Extraction, records []importer.LocatedRecord) ([]string, error) {
	keys := make([]string, 0, len(records))
	for _, record := range records {
		if record.Reason != "" {
			return nil, errors.New("a record of this document could not be read into the declared schema")
		}
		key, ok := record.Value(extraction.RecordKey)
		if !ok {
			return nil, errors.New("a record of this document does not hold the declared record key")
		}
		if !printableKey(key) {
			return nil, errRecordKey
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func validProduced(produced []string) error {
	if len(produced) > MaxProduced {
		return errors.New("a collection correlates at most 512 produced occurrences")
	}
	seen := make(map[string]bool, len(produced))
	for _, occurrence := range produced {
		if !printableKey(occurrence) {
			return errors.New("a produced occurrence is one printable key of at most 128 bytes")
		}
		if seen[occurrence] {
			return errors.New("a produced occurrence is named twice")
		}
		seen[occurrence] = true
	}
	return nil
}

// errRecordKey is the one refusal every reader gives a value that cannot be a
// record key, so the same condition is reported in the same words wherever a
// source is read.
var errRecordKey = errors.New("a record key is one printable value of at most 128 bytes")

// dateState places the state one attempt read in time and reports whether it is
// still inside the declared freshness bound. Every source states how old what
// it read is in its own way — an export's modification time, a response's age,
// the times a capture recorded — and one place decides what that age means, so
// a third reader cannot acquire a fourth reading of "current".
func dateState(taken *attempt, current time.Time, maxAge time.Duration) bool {
	taken.asOf = current
	age := taken.at.Sub(current)
	taken.record.StatedAge = age.String()
	return age <= maxAge
}

func printableKey(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}
