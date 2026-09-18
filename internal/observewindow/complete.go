package observewindow

import (
	"encoding/json/v2"
	"errors"
	"time"
)

// CompletionSchema is the contract a retained completion record carries.
const CompletionSchema = "readmit-observation-completion/v1"

// Boundary is the name every completion states. A verdict that cites one names
// the boundary it evaluated and, through the samples, correlations and source
// it carries, the evidence it evaluated it on.
const Boundary = "observation-window"

const (
	// MaxCompletionBytes bounds the record a command reads before decoding it.
	MaxCompletionBytes = 512 << 10
	// maxCorrelations bounds how many things one window may account for.
	maxCorrelations = 5000
	// maxKey bounds a collector-assigned identity.
	maxKey = 128
)

// SampleStatus is what one attempt to observe the source produced. Observed is
// the only one of these that is an observation; the rest are ways collection
// failed, and none of them is an empty source.
type SampleStatus string

const (
	// Observed means the source answered, the answer was read in full, and the
	// original material it was read from was retained. Its record count may be
	// zero: an observed empty state is evidence, and it is the only kind of
	// emptiness that is.
	Observed SampleStatus = "observed"
	// SampleMissing means no state could be obtained at all, which is what a
	// collector that was disabled or never ran reports.
	SampleMissing SampleStatus = "missing"
	// SampleAmbiguous means the source answered with something that has no
	// single reading.
	SampleAmbiguous SampleStatus = "ambiguous"
	// SampleStale means the state the source returned predates the window's
	// watermark, so it cannot describe this window.
	SampleStale SampleStatus = "stale"
	// SampleTruncated means a limit cut the state short, so what was read is a
	// prefix of the source rather than the source.
	SampleTruncated SampleStatus = "truncated"
	// SampleUnsupported means this collector does not support the declared
	// source kind or scope. Unsupported is not an empty result.
	SampleUnsupported SampleStatus = "unsupported"
	// SampleFailed means the attempt itself errored, which is what a lost
	// connection, a refused read or a broken query reports.
	SampleFailed SampleStatus = "failed"
)

// Status is what one window produced. Complete is the only trustworthy one;
// the rest name how the window failed and are execution errors, never negative
// application results.
type Status string

const (
	// Complete means collection completed for the declared window.
	Complete Status = "complete"
	// Incomplete means the deadline passed before the source held still.
	Incomplete Status = "incomplete"
	// Missing means no observation was obtained.
	Missing Status = "missing"
	// Ambiguous means the window cannot be read one way.
	Ambiguous Status = "ambiguous"
	// Stale means evidence predating the watermark was returned.
	Stale Status = "stale"
	// Truncated means a limit cut the observation short.
	Truncated Status = "truncated"
	// Unsupported means the declared source kind or scope is not supported.
	Unsupported Status = "unsupported"
	// Failed means collection itself errored.
	Failed Status = "failed"
	// Cancelled means sampling was cancelled before the rule could decide.
	Cancelled Status = "cancelled"
	// TimedOut means sampling ran out of time before the rule could decide.
	TimedOut Status = "timed_out"
	// Interrupted means sampling stopped without recording how it ended, which
	// is what a collector killed mid-window leaves behind.
	Interrupted Status = "interrupted"
)

// Stop is why sampling stopped, when it stopped for a reason outside the rule.
// It is separate from Status for the same reason a durable run separates its
// stop reason from its state: what interrupted a window and what the window
// proved are two different facts.
type Stop string

const (
	// StopRule means sampling stopped because the completion rule decided.
	StopRule Stop = "none"
	// StopCancelled means the operator or caller cancelled sampling.
	StopCancelled Stop = "cancelled"
	// StopTimedOut means sampling ran out of time.
	StopTimedOut Stop = "timed_out"
	// StopInterrupted means the collector stopped without recording an ending.
	StopInterrupted Stop = "interrupted"
)

// How one thing the run produced relates to what the window observed.
const (
	// Matched names exactly one observed record.
	Matched = "matched"
	// Unmatched means the window observed nothing for it. On a completed
	// window that is a fact about the source; on any other it is not.
	Unmatched = "unmatched"
	// AmbiguousMatch means several observed records could be it, and the
	// window does not pick one.
	AmbiguousMatch = "ambiguous"
)

// failureStatus is the one table mapping a sample that was not an observation
// to the window status it produces. Observed is absent by construction: it is
// the only sample status that is not a failure.
var failureStatus = map[SampleStatus]Status{
	SampleMissing:     Missing,
	SampleAmbiguous:   Ambiguous,
	SampleStale:       Stale,
	SampleTruncated:   Truncated,
	SampleUnsupported: Unsupported,
	SampleFailed:      Failed,
}

// stopStatus is the one table mapping an interruption outside the completion
// rule to the window status it produces. StopRule is absent: a window that
// stopped by its own rule is decided by that rule, not by how it stopped.
var stopStatus = map[Stop]Status{
	StopCancelled:   Cancelled,
	StopTimedOut:    TimedOut,
	StopInterrupted: Interrupted,
}

// statusReason is the one table naming every window status and why it is not
// evidence. Complete carries the empty reason because it is the only
// trustworthy one; a status absent from this table is not a status.
var statusReason = map[Status]string{
	Complete:    "",
	Incomplete:  "the deadline passed before the source held still for the declared quiet period",
	Missing:     "no observation of the source was obtained",
	Ambiguous:   "the source's state cannot be read one way",
	Stale:       "the source returned state predating the window's watermark",
	Truncated:   "a limit cut the observation short, so a prefix of the source was read rather than the source",
	Unsupported: "the declared source kind or scope is not supported",
	Failed:      "collection itself failed",
	Cancelled:   "sampling was cancelled before the completion rule decided",
	TimedOut:    "sampling ran out of time before the completion rule decided",
	Interrupted: "sampling stopped without recording how it ended",
}

// namedRunState is the one table binding the statuses that have a durable-run
// state of their own. Every other status is an execution error, which is what
// RunState returns for anything absent here, including a status it does not
// recognize.
var namedRunState = map[Status]string{
	Complete:    "",
	Cancelled:   "cancelled",
	TimedOut:    "timed_out",
	Interrupted: "interrupted",
}

// RunState names the durable-run state this window status produces, in the
// vocabulary internal/durablerun already uses rather than a parallel one. A
// completed window produces no run state and returns the empty string: it is
// trustworthy evidence, and what an assertion makes of it is the run's
// business, not the window's.
//
// It returns a string rather than a durablerun.State on purpose. The test
// runner and every collector will depend on this package, and importing the
// runner's own vocabulary back into the contract is the dependency cycle this
// contract exists to avoid. An external test pins these strings to
// durablerun's constants, so the two can never drift apart.
//
// No window status ever produces a pass or an assertion failure. Unknown and
// unsupported are not pass, a timeout is not a negative application result,
// and neither is a window that could not be read. There is no analogue of
// delivery_uncertain: reading a source sends nothing, so a window is never
// uncertain about an effect it did not cause.
func (s Status) RunState() string {
	if state, named := namedRunState[s]; named {
		return state
	}
	return "execution_error"
}

// Sample is one observation of the source, as its collector reported it. A
// baseline taken before the window opened is one of these too: the same shape
// is held to the same rules whether it was taken inside the window or before
// it.
type Sample struct {
	At     time.Time    `json:"at"`
	Status SampleStatus `json:"status"`
	// RecordCount is how many records in scope this observation held. It is
	// meaningful only for an observed sample; collection that failed counted
	// nothing.
	RecordCount int `json:"record_count"`
	// StateDigest identifies the observed state so two samples can be compared
	// for stability. It identifies a state; it does not authenticate one.
	StateDigest string `json:"state_digest"`
	// EvidenceIdentity names the original material this observation was read
	// from, which its collector retained unchanged. It is a reference, never a
	// copy: no message value and no source path crosses this boundary. An
	// observation must carry one, because evidence nobody kept cannot be
	// re-examined, and collection that failed read no original material.
	EvidenceIdentity string `json:"evidence_identity"`
}

// Correlation maps one thing the run produced to what the window observed of
// it. Both sides are identities a collector assigns, never field values:
// nothing here carries patient data.
type Correlation struct {
	// Produced names the outbound occurrence the run sent.
	Produced string `json:"produced"`
	// Observed is the collector's key for the record it matched, and is empty
	// for every kind but matched.
	Observed string `json:"observed"`
	Kind     string `json:"kind"`
}

// Collection is what a collector reports about one window it attempted. It is
// the collector's testimony, not a verdict: what it means is decided by the
// window's completion rule.
type Collection struct {
	OpenedAt time.Time
	ClosedAt time.Time
	Stop     Stop
	// Baseline is the observation taken before the window opened. It belongs
	// only to a window declaring recorded-baseline, and stays nil there when
	// the collector never took one, which is itself a failure to collect.
	Baseline     *Sample
	Samples      []Sample
	Correlations []Correlation
}

// Completion is the retained record of one evaluated window. It carries no
// message values and no source paths: counts, declared labels, identities and
// statuses only.
type Completion struct {
	Schema         string `json:"schema"`
	Boundary       string `json:"boundary"`
	WindowIdentity string `json:"window_identity"`
	Source         Source `json:"source"`
	// Watermark is copied from the window so a reader of this record alone can
	// see what start state the window was opened at.
	Watermark Watermark `json:"watermark"`
	Status    Status    `json:"status"`
	Stop      Stop      `json:"stop"`
	OpenedAt  time.Time `json:"opened_at"`
	ClosedAt  time.Time `json:"closed_at"`
	// Baseline is retained exactly as the collector reported it, including a
	// baseline whose own collection failed. Like every other member it is
	// always declared, and it is explicitly null for a window that declared no
	// baseline and for one whose required baseline was never taken. Retaining
	// it is what lets this record be re-decided from evidence rather than from
	// an assumption that a baseline succeeded.
	Baseline     *Sample       `json:"baseline"`
	Samples      []Sample      `json:"samples"`
	Correlations []Correlation `json:"correlations"`
	// StableSamples and QuietPeriod are what was observed, not what was
	// declared: the length and span of the run of identical observations the
	// window ended on.
	StableSamples int    `json:"stable_samples"`
	QuietPeriod   string `json:"quiet_period"`
	// RecordsObserved is the settled count. It is meaningful only when the
	// status is complete; every other status leaves it zero, because a window
	// that did not complete never settled on a count and a count read off an
	// incomplete window is exactly the false answer this package exists to
	// prevent.
	RecordsObserved int `json:"records_observed"`
	// PreExistingBasis is the window's declaration, carried into the verdict:
	// declared-empty, recorded-baseline or unknown.
	PreExistingBasis string `json:"pre_existing_basis"`
}

// Decide evaluates one attempted collection against this window and produces
// the completion record of what it means.
//
// It returns an error only when the window or the collector's report is not
// well formed, and the zero Completion with it, which is untrustworthy and
// carries its own execution error. Collection that simply did not succeed is
// not an error here: it is a Completion whose status names how it failed.
//
// Precedence, in order: the earliest sample that was not an observation or
// that carries a time outside the window; a cancellation, timeout or
// interruption that stopped sampling; no samples at all; a baseline the window
// required and did not get, or one whose own collection failed; a record
// limit; and finally the deadline and the quiet period. The earliest point at
// which the window stopped being trustworthy is the one it reports.
func (w Window) Decide(c Collection) (Completion, error) {
	if err := w.Validate(); err != nil {
		return Completion{}, err
	}
	if err := w.shape(c); err != nil {
		return Completion{}, err
	}
	record := Completion{
		Schema: CompletionSchema, Boundary: Boundary, WindowIdentity: w.Identity(),
		Source: w.Source, Watermark: w.Watermark, Stop: c.Stop,
		OpenedAt: c.OpenedAt.UTC(), ClosedAt: c.ClosedAt.UTC(),
		Samples: make([]Sample, 0, len(c.Samples)), Correlations: append([]Correlation{}, c.Correlations...),
		QuietPeriod: time.Duration(0).String(), PreExistingBasis: w.PreExisting.Declaration,
	}
	for _, sample := range c.Samples {
		sample.At = sample.At.UTC()
		record.Samples = append(record.Samples, sample)
	}
	if c.Baseline != nil {
		baseline := *c.Baseline
		baseline.At = baseline.At.UTC()
		record.Baseline = &baseline
	}
	record.Status = w.status(c)
	if record.Status != Complete {
		return record, nil
	}
	record.RecordsObserved = c.Samples[len(c.Samples)-1].RecordCount
	record.StableSamples, record.QuietPeriod = stableRun(c.Samples)
	return record, nil
}

// shape refuses a report a collector could not honestly have produced: a
// negative count, a digest on a sample that was not an observation, more
// samples than the window allows, or a baseline the window did not declare.
// These are defects in the caller, not observations about the source.
func (w Window) shape(c Collection) error {
	if err := validSpan(c.OpenedAt, c.ClosedAt); err != nil {
		return err
	}
	if err := validStop(c.Stop); err != nil {
		return err
	}
	if len(c.Samples) > w.Completion.MaxSamples {
		return errors.New("a collection reported more samples than its window allows")
	}
	if c.Baseline != nil && w.PreExisting.Declaration != RecordedBaseline {
		return errors.New("only a window declaring recorded-baseline carries a baseline observation")
	}
	if err := validBaseline(c.Baseline, c.OpenedAt); err != nil {
		return err
	}
	if err := validSamples(c.Samples); err != nil {
		return err
	}
	return validCorrelations(c.Correlations)
}

// status applies the precedence Decide documents.
func (w Window) status(c Collection) Status {
	previous := c.OpenedAt
	for _, sample := range c.Samples {
		if sample.Status != Observed {
			return failureStatus[sample.Status]
		}
		// A sample taken before the window opened, after it closed, or before
		// the sample ahead of it describes a sequence the window cannot read
		// one way. A clock is an observed fact, not a malformed report.
		if sample.At.Before(previous) || sample.At.After(c.ClosedAt) {
			return Ambiguous
		}
		previous = sample.At
	}
	if status, stopped := stopStatus[c.Stop]; stopped {
		return status
	}
	if len(c.Samples) == 0 {
		return Missing
	}
	if w.PreExisting.Declaration == RecordedBaseline {
		if c.Baseline == nil {
			return Missing
		}
		if c.Baseline.Status != Observed {
			return failureStatus[c.Baseline.Status]
		}
	}
	for _, sample := range c.Samples {
		if sample.RecordCount > w.Completion.MaxRecords {
			return Truncated
		}
	}
	stable, quiet := stableRun(c.Samples)
	span, _ := time.ParseDuration(quiet)
	last := c.Samples[len(c.Samples)-1]
	if stable < w.Completion.StableSamples || span < w.Completion.quietPeriod() || last.At.Sub(c.OpenedAt) > w.Completion.deadline() {
		return Incomplete
	}
	// Records the window observed cannot be fewer than the baseline it opened
	// on without something having been removed, which no completion rule here
	// can account for.
	if c.Baseline != nil && c.Baseline.RecordCount > last.RecordCount {
		return Ambiguous
	}
	return Complete
}

// stableRun measures the run of identical observations the samples end on: how
// many there are and how long it spans. A run of one spans nothing, which is
// why a completion rule requires at least two.
func stableRun(samples []Sample) (int, string) {
	if len(samples) == 0 {
		return 0, time.Duration(0).String()
	}
	last := len(samples) - 1
	first := last
	for first > 0 && samples[first-1].StateDigest == samples[last].StateDigest {
		first--
	}
	return last - first + 1, samples[last].At.Sub(samples[first].At).String()
}

// Trustworthy reports whether collection completed for the declared window.
func (c Completion) Trustworthy() bool { return c.Schema == CompletionSchema && c.Status == Complete }

// Err returns the execution error this completion is, or nil when collection
// completed. A completion that was never evaluated is untrustworthy too, so the
// zero value fails closed rather than reading as a pass.
func (c Completion) Err() error {
	if c.Schema != CompletionSchema {
		return errors.New("no observation window was evaluated")
	}
	reason, named := statusReason[c.Status]
	if !named {
		return errors.New("observation window: unrecognized status")
	}
	if reason == "" {
		return nil
	}
	return errors.New("observation window: " + reason)
}

// AbsenceEvidence reports whether this completion may support a claim that
// something is absent from the declared scope. It is the guard this package
// exists for: a collector that never ran, one that returned stale data, one
// whose capture was truncated, one whose connection was lost and one whose
// source answered ambiguously all observed no records, and none of them
// observed that there are none.
func (c Completion) AbsenceEvidence() error {
	if err := c.Err(); err != nil {
		return errors.New("an absence assertion requires evidence that collection completed for the declared window, and " + err.Error())
	}
	return nil
}

// ProducedInWindow reports how many observed records the window can attribute
// to what happened inside it. State that was already there when the window
// opened is not evidence that this run produced it, so a window that completed
// over an unknown pre-existing state can still show what is present and absent
// and can still not attribute anything.
func (c Completion) ProducedInWindow() (int, error) {
	if err := c.Err(); err != nil {
		return 0, err
	}
	switch c.PreExistingBasis {
	case DeclaredEmpty:
		return c.RecordsObserved, nil
	case RecordedBaseline:
		if c.Baseline == nil || c.Baseline.Status != Observed {
			return 0, errors.New("observation window: the baseline this window declared was never observed, so no observed record can be attributed to this run")
		}
		return c.RecordsObserved - c.Baseline.RecordCount, nil
	}
	return 0, errors.New("observation window: the state before the window opened is declared unknown, so no observed record can be attributed to this run")
}

// Correlated reports whether the window observed what one thing the run
// produced became. Only a matched correlation is an answer: an unmatched or
// ambiguous correlation, and a produced occurrence this window recorded no
// correlation for at all, are each unknown, and unknown is not a pass.
func (c Completion) Correlated(produced string) error {
	if err := c.Err(); err != nil {
		return err
	}
	for _, correlation := range c.Correlations {
		if correlation.Produced != produced {
			continue
		}
		if correlation.Kind == Matched {
			return nil
		}
		return errors.New("observation window: what the run produced correlates " + correlation.Kind + " in this window")
	}
	return errors.New("observation window: this window recorded no correlation for what the run produced")
}

// Verify refuses a retained completion that does not belong to this window or
// that its own evidence does not support. It re-decides the record from what
// the collector reported — every sample and the baseline exactly as retained,
// including one whose own collection failed — so re-deciding can never invent
// the evidence it exists to check, and a window that honestly failed on its
// baseline is reproduced rather than called inconsistent.
func (w Window) Verify(c Completion) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.WindowIdentity != w.Identity() {
		return errors.New("this completion records a different observation window")
	}
	again, err := w.Decide(Collection{
		OpenedAt: c.OpenedAt, ClosedAt: c.ClosedAt, Stop: c.Stop,
		Baseline: c.Baseline, Samples: c.Samples, Correlations: c.Correlations,
	})
	if err != nil {
		return err
	}
	if again.Status != c.Status || again.RecordsObserved != c.RecordsObserved || again.StableSamples != c.StableSamples || again.QuietPeriod != c.QuietPeriod {
		return errors.New("this completion's evidence does not support the verdict it records")
	}
	return nil
}

// UnmarshalJSON requires every member explicitly and then re-decodes rejecting
// unknown members, so a record authored against a later contract is never read
// as though this one had always allowed it. The baseline is required as an
// explicit null where there is none, which a typed decode alone would accept
// as an absent member.
func (c *Completion) UnmarshalJSON(data []byte) error {
	var required struct {
		Schema           *string        `json:"schema"`
		Boundary         *string        `json:"boundary"`
		WindowIdentity   *string        `json:"window_identity"`
		Source           *Source        `json:"source"`
		Watermark        *Watermark     `json:"watermark"`
		Status           *Status        `json:"status"`
		Stop             *Stop          `json:"stop"`
		OpenedAt         *time.Time     `json:"opened_at"`
		ClosedAt         *time.Time     `json:"closed_at"`
		Samples          *[]Sample      `json:"samples"`
		Correlations     *[]Correlation `json:"correlations"`
		StableSamples    *int           `json:"stable_samples"`
		QuietPeriod      *string        `json:"quiet_period"`
		RecordsObserved  *int           `json:"records_observed"`
		PreExistingBasis *string        `json:"pre_existing_basis"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Schema == nil || required.Boundary == nil || required.WindowIdentity == nil || required.Source == nil || required.Watermark == nil || required.Status == nil || required.Stop == nil || required.OpenedAt == nil || required.ClosedAt == nil || required.Samples == nil || required.Correlations == nil || required.StableSamples == nil || required.QuietPeriod == nil || required.RecordsObserved == nil || required.PreExistingBasis == nil {
		return errors.New("an observation completion requires every member explicitly")
	}
	var members map[string]any
	if err := json.Unmarshal(data, &members); err != nil {
		return errors.New("invalid observation completion JSON")
	}
	if _, declared := members["baseline"]; !declared {
		return errors.New("an observation completion declares its baseline, explicitly null where none was taken")
	}
	type plainCompletion Completion
	var value plainCompletion
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid observation completion JSON")
	}
	*c = Completion(value)
	return nil
}

func (s *Sample) UnmarshalJSON(data []byte) error {
	var required struct {
		At               *time.Time    `json:"at"`
		Status           *SampleStatus `json:"status"`
		RecordCount      *int          `json:"record_count"`
		StateDigest      *string       `json:"state_digest"`
		EvidenceIdentity *string       `json:"evidence_identity"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.At == nil || required.Status == nil || required.RecordCount == nil || required.StateDigest == nil || required.EvidenceIdentity == nil {
		return errors.New("a sample requires a time, status, record count, state digest, and evidence identity, explicitly empty where it has none")
	}
	type plainSample Sample
	var value plainSample
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid observation sample")
	}
	*s = Sample(value)
	return nil
}

func (c *Correlation) UnmarshalJSON(data []byte) error {
	var required struct {
		Produced *string `json:"produced"`
		Observed *string `json:"observed"`
		Kind     *string `json:"kind"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Produced == nil || required.Observed == nil || required.Kind == nil {
		return errors.New("a correlation requires what the run produced, what was observed of it, and a kind")
	}
	type plainCorrelation Correlation
	var value plainCorrelation
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid correlation")
	}
	*c = Correlation(value)
	return nil
}

// Validate holds a retained completion to its own shape and to the parts of
// its verdict that stand on their own. It does not decide whether the evidence
// supports the verdict; Window.Verify does that, because only the window that
// was declared can re-apply its completion rule.
func (c Completion) Validate() error {
	if c.Schema != CompletionSchema || c.Boundary != Boundary {
		return errors.New("an observation completion must declare " + CompletionSchema + " at the " + Boundary + " boundary")
	}
	if !digestPattern.MatchString(c.WindowIdentity) {
		return errors.New("an observation completion names its window's lowercase hexadecimal identity")
	}
	if err := c.Source.validate(); err != nil {
		return err
	}
	if err := c.Watermark.validate(); err != nil {
		return err
	}
	if _, named := statusReason[c.Status]; !named {
		return errors.New("unrecognized observation completion status")
	}
	if err := validStop(c.Stop); err != nil {
		return err
	}
	if err := validSpan(c.OpenedAt, c.ClosedAt); err != nil {
		return err
	}
	if len(c.Samples) > maxSamples {
		return errors.New("an observation completion exceeds the sample limit")
	}
	if err := validSamples(c.Samples); err != nil {
		return err
	}
	if err := validBaseline(c.Baseline, c.OpenedAt); err != nil {
		return err
	}
	if err := validCorrelations(c.Correlations); err != nil {
		return err
	}
	if _, err := time.ParseDuration(c.QuietPeriod); err != nil {
		return errors.New("an observed quiet period is a duration")
	}
	if !validDeclaration(c.PreExistingBasis) {
		return errors.New("a pre-existing-state basis is declared-empty, recorded-baseline, or unknown")
	}
	if c.Baseline != nil && c.PreExistingBasis != RecordedBaseline {
		return errors.New("only a completion over a recorded baseline retains a baseline observation")
	}
	if c.StableSamples < 0 || c.StableSamples > len(c.Samples) || c.RecordsObserved < 0 || c.RecordsObserved > maxRecords {
		return errors.New("an observation completion's counts are bounded by what it sampled")
	}
	// A status other than complete never carries a settled count. Refusing it
	// here means a hand-authored record cannot smuggle one past a reader.
	if c.Status != Complete {
		if c.RecordsObserved != 0 || c.StableSamples != 0 {
			return errors.New("only a completed observation window reports settled counts")
		}
		return nil
	}
	if c.PreExistingBasis == RecordedBaseline {
		if c.Baseline == nil || c.Baseline.Status != Observed {
			return errors.New("a completed window over a recorded baseline retains the observation of that baseline")
		}
		if c.Baseline.RecordCount > c.RecordsObserved {
			return errors.New("a completed window cannot observe fewer records than the baseline it opened on")
		}
	}
	return nil
}

// The four checks below are shared by the collector's report and the retained
// record, so the shape a collection is held to and the shape a reader accepts
// cannot drift apart.

func validSpan(opened, closed time.Time) error {
	if opened.IsZero() || closed.IsZero() || closed.Before(opened) {
		return errors.New("an observation window opens and closes at explicit times, and cannot close before it opened")
	}
	return nil
}

func validStop(stop Stop) error {
	if stop != StopRule {
		if _, stopped := stopStatus[stop]; !stopped {
			return errors.New("a collection stops by rule, by cancellation, by timeout, or by interruption")
		}
	}
	return nil
}

func validSamples(samples []Sample) error {
	for _, sample := range samples {
		if err := sample.validate(); err != nil {
			return err
		}
	}
	return nil
}

// validBaseline holds a baseline to a sample's shape and to the one thing that
// distinguishes it: it was taken before the window opened.
func validBaseline(baseline *Sample, opened time.Time) error {
	if baseline == nil {
		return nil
	}
	if err := baseline.validate(); err != nil {
		return err
	}
	if baseline.At.After(opened) {
		return errors.New("a baseline is observed before the window opens")
	}
	return nil
}

func validCorrelations(correlations []Correlation) error {
	if len(correlations) > maxCorrelations {
		return errors.New("an observation window exceeds the correlation limit")
	}
	produced := make(map[string]bool, len(correlations))
	for _, correlation := range correlations {
		if !key(correlation.Produced) {
			return errors.New("a correlation names what the run produced as one printable key of at most 128 bytes")
		}
		if produced[correlation.Produced] {
			return errors.New("a correlation names the same produced occurrence twice")
		}
		produced[correlation.Produced] = true
		switch correlation.Kind {
		case Matched:
			if !key(correlation.Observed) {
				return errors.New("a matched correlation names the one observed record it matched")
			}
		case Unmatched, AmbiguousMatch:
			// An unmatched correlation observed nothing, and an ambiguous one
			// will not pick between several candidates. Neither names one.
			if correlation.Observed != "" {
				return errors.New("only a matched correlation names an observed record")
			}
		default:
			return errors.New("a correlation is matched, unmatched, or ambiguous")
		}
	}
	return nil
}

// validate holds one observation to what a collector could honestly report.
func (s Sample) validate() error {
	if s.At.IsZero() {
		return errors.New("every observation carries the time it was taken")
	}
	observed := s.Status == Observed
	if !observed {
		if _, failure := failureStatus[s.Status]; !failure {
			return errors.New("an observation status is observed, missing, ambiguous, stale, truncated, unsupported, or failed")
		}
	}
	if s.RecordCount < 0 || s.RecordCount > maxRecords {
		return errors.New("an observation record count is between 0 and 1048576")
	}
	if !observed && s.RecordCount != 0 {
		return errors.New("only an observation reports a record count; collection that failed counted nothing")
	}
	if observed != digestPattern.MatchString(s.StateDigest) {
		return errors.New("an observation carries its lowercase hexadecimal state digest, and collection that failed carries none")
	}
	if observed != digestPattern.MatchString(s.EvidenceIdentity) {
		return errors.New("an observation names the original evidence its collector retained, and collection that failed read none")
	}
	return nil
}

func key(value string) bool {
	return value != "" && len(value) <= maxKey && printable(value)
}

// EncodeCompletion writes one validated completion deterministically.
func EncodeCompletion(c Completion) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(c, json.Deterministic(true))
	if err != nil || len(data) >= MaxCompletionBytes {
		return nil, errors.New("cannot encode bounded observation completion")
	}
	return append(data, '\n'), nil
}

// DecodeCompletion reads one retained completion exactly as written.
func DecodeCompletion(data []byte) (Completion, error) {
	var completion Completion
	if len(data) > MaxCompletionBytes {
		return completion, errors.New("observation completion exceeds size limit")
	}
	if err := json.Unmarshal(data, &completion); err != nil {
		return Completion{}, errors.New("invalid observation completion JSON")
	}
	if err := completion.Validate(); err != nil {
		return Completion{}, err
	}
	return completion, nil
}
