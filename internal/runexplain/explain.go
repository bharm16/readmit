// Package runexplain explains what one run's evidence decided, assertion by
// assertion.
//
// It is the reading half of a verdict. internal/assertion decides; this
// package says what was read to decide it, where each value came from, how
// long the run took, and under which configuration and contract versions it
// happened — so somebody who did not run it can follow the reasoning without
// opening a JSON file.
//
// Three rules shape it.
//
// It re-decides rather than restates. Nothing here reports a stored verdict:
// the run bundle, the assertion set and the observation records are opened
// through their own verifying readers and evaluated again, exactly as
// `observe explain --window` re-decides a completion from the samples it
// retained. An explanation is therefore a pure function of evidence that
// already exists, and it opens nothing else, sends nothing and writes nothing.
//
// It links rather than copies. A result names the occurrence, the selector and
// the payload file its value was read out of, so the original evidence stays
// the thing to re-examine. What it carries of a value is what the evaluator
// read, and that is customer-local evidence in the same sense a result
// directory is; a caller decides whether to display it.
//
// It refuses rather than approximates. An assertion set that asks about
// records this release cannot derive again is refused before anything is
// evaluated, so a question nobody could answer never becomes an outcome.
package runexplain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/replay"
)

// ObservationInput names the two documents one observed scope is explained
// from: the record a collection retained, and the source document that says
// which evidence it read. Both are required together, because a completion
// says what an observation settled on and only the source says where the
// records it settled on can be read again.
type ObservationInput struct {
	Completion string
	Source     string
}

func (o ObservationInput) declared() bool { return o.Completion != "" || o.Source != "" }

// Input names the verified artifacts one explanation is assembled from. The
// run and the assertion set are always required; an observation is supplied
// for a scope the set actually reads, and supplying one the set never reads is
// refused rather than quietly ignored.
type Input struct {
	Run        string
	Assertions string
	Before     ObservationInput
	After      ObservationInput
}

// Link names where one value a result was decided on can be read again. It is
// a position in retained evidence — a payload file, an occurrence, a selector
// — and never a value.
type Link struct {
	// Scope is the assertion vocabulary the link belongs to. It is written as
	// text rather than typed because it spans both of them: a field link names
	// a message scope and a record link names an observation scope, and a
	// single type over the two would be a fifth vocabulary nothing else uses.
	Scope string
	// Message is the occurrence the value was read out of, for a field link.
	Message string
	// Selector is the shared-grammar position, for a field link.
	Selector string
	// Artifact is the retained directory or document holding the evidence.
	Artifact string
	// Payload is the file inside the run bundle holding the exact bytes, where
	// the link names one.
	Payload string
}

// Detail is one assertion and what deciding it read. The expectation and the
// reading are carried as the typed values the reader and the evaluator
// produced rather than as rendered text, so a renderer decides how much of a
// customer-local value it shows.
type Detail struct {
	ID       string
	Operator assertion.Operator
	Outcome  assertion.Outcome
	Subject  assertion.Subject
	When     *assertion.Condition
	Expected assertion.Expected
	Observed assertion.Reading
	Evidence []Link
}

// Message is one message of the run, as the run recorded it.
type Message struct {
	Source   string
	Outbound string
	Outcome  replay.Outcome
	Delivery string
	// ACKCorrelation and ACKCode are what the run recorded of the
	// acknowledgement. An absent code is absent, never an accepted one.
	ACKCorrelation string
	ACKCode        string
	Elapsed        time.Duration
	// Sent and Received name the payload files. An empty name is a payload the
	// run retained nothing for, which is an unattempted or refused delivery.
	Sent, Received string
	// SentReadable and ReceivedReadable report whether each payload is evidence
	// an assertion may read. A payload that did not parse as one message is
	// not evidence of an absent value.
	SentReadable, ReceivedReadable bool
	// SentPartial reports that fewer bytes reached the peer than the run
	// intended to send. What was written is a prefix of the message, so a field
	// it does not reach would read as omitted; that is a truncation, never an
	// observation about the message, and such a payload is never evidence.
	SentPartial bool
}

// RunContext is the run an explanation is about, and the configuration it ran
// under. It is the environmental half of the explanation: which endpoint,
// which transport, which case, and when.
type RunContext struct {
	Path     string
	Identity string
	Schema   string
	// State, ContainsSourceValues and ExportPolicy are the run's own lineage
	// and handling declarations, restated rather than recomputed. A reader has
	// to be able to see whether the bundle in front of them is original
	// customer-local evidence.
	State                string
	ContainsSourceValues bool
	ExportPolicy         string
	SourceBundleIdentity string
	TargetIdentity       string
	Target               replay.TargetRecord
	StartedAt            time.Time
	CompletedAt          time.Time
	Transformations      []replay.Transformation
	Messages             []Message
}

// Elapsed is how long the run took, as the run recorded its own span.
func (r RunContext) Elapsed() time.Duration { return r.CompletedAt.Sub(r.StartedAt) }

// SetContext is the assertion set an explanation was decided by.
type SetContext struct {
	Path     string
	Schema   string
	Name     string
	Identity string
	Count    int
}

// ObservationContext is one observed scope: the window it was collected for,
// what it settled on, and the capture its records were derived again from.
type ObservationContext struct {
	Scope          assertion.RecordScope
	CompletionPath string
	SourcePath     string
	CapturePath    string
	Schema         string
	SourceSchema   string
	WindowIdentity string
	Source         observewindow.Source
	Status         observewindow.Status
	OpenedAt       time.Time
	ClosedAt       time.Time
	StableSamples  int
	QuietPeriod    string
	Records        int
	// Correlations are what this observation recorded of the occurrences the
	// run that collected it declared it produced, and Matched, Unmatched and
	// Ambiguous count them by kind. Every declared occurrence has been checked
	// against what this run actually produced, so an observation recorded
	// beside some other run is refused rather than reported beside this one. A
	// record carrying none binds nothing, and the explanation says so rather
	// than implying a link nobody recorded.
	Correlations                  []observewindow.Correlation
	Matched, Unmatched, Ambiguous int
	// Keys are the records the observation settled on, derived again from the
	// capture and checked against the completion. They are source values.
	Keys []string
}

// Explanation is what one run's evidence decided and what deciding it read.
//
// A Verdict and a Failure are exclusive: an execution error produces no
// verdict at all, because a partial verdict over evidence the evaluator could
// not stand behind is the conflation these contracts exist to prevent.
type Explanation struct {
	Run          RunContext
	Set          SetContext
	Observations []ObservationContext
	Verdict      assertion.Verdict
	Failure      *assertion.Error
	Details      []Detail
	Passed       int
	Failed       int
	Undecided    int
	Skipped      int
}

// maxSetBytes bounds the authored document this package reads. It is the
// assertion contract's own bound, so a document that reader refuses is refused
// before it is held in memory rather than after.
const maxSetBytes = assertion.MaxSetBytes

// Explain opens the evidence, re-decides the set against it, and reports what
// was read. It returns an error only when the evidence could not be assembled
// at all; an execution error inside the evaluation is carried on the
// explanation, because what the evaluator could not read is itself something
// to explain.
func Explain(ctx context.Context, input Input) (Explanation, error) {
	if input.Run == "" || input.Assertions == "" {
		return Explanation{}, errors.New("an explanation requires one run bundle and one assertion set")
	}
	data, err := readBounded(input.Assertions, maxSetBytes)
	if err != nil {
		return Explanation{}, err
	}
	set, err := assertion.Decode(data)
	if err != nil {
		return Explanation{}, err
	}
	run, err := replay.Open(input.Run)
	if err != nil {
		return Explanation{}, err
	}
	explanation := Explanation{
		Run: runContext(input.Run, run),
		Set: SetContext{Path: input.Assertions, Schema: set.Schema, Name: set.Name, Identity: digest(data), Count: len(set.Assertions)},
	}
	evidence, messages, err := runEvidence(run)
	if err != nil {
		return Explanation{}, err
	}
	for i := range explanation.Run.Messages {
		readable := messages[explanation.Run.Messages[i].Source]
		explanation.Run.Messages[i].SentReadable = readable.input
		explanation.Run.Messages[i].ReceivedReadable = readable.observed
	}
	collected, err := readObservations(set, input, evidence)
	if err != nil {
		return Explanation{}, err
	}
	explanation.Observations = collected
	for _, observed := range collected {
		collection := assertion.Collection{Complete: true, Keys: observed.Keys}
		if observed.Scope == assertion.BeforeRecords {
			evidence.Before = collection
		} else {
			evidence.After = collection
		}
	}
	report, err := set.Evaluate(ctx, evidence)
	if err != nil {
		var failure *assertion.Error
		if !errors.As(err, &failure) {
			return Explanation{}, err
		}
		explanation.Failure = failure
		explanation.Details = links(set, nil, explanation)
		return explanation, nil
	}
	explanation.Verdict = report.Verdict
	explanation.Passed, explanation.Failed = report.Passed, report.Failed
	explanation.Undecided, explanation.Skipped = report.Undecided, report.Skipped
	explanation.Details = links(set, report.Results, explanation)
	return explanation, nil
}

// DescribeRun describes a verified replay and the readable response boundary.
// Consumers share the explainer's payload rules instead of parsing evidence again.
func DescribeRun(run *replay.Run) (RunContext, error) {
	description := runContext("", run)
	_, messages, err := runEvidence(run)
	if err != nil {
		return RunContext{}, err
	}
	for i := range description.Messages {
		readable := messages[description.Messages[i].Source]
		description.Messages[i].SentReadable = readable.input
		description.Messages[i].ReceivedReadable = readable.observed
	}
	return description, nil
}

// readable records which of one message's two payloads parsed. It exists so
// the explanation can say that a payload was retained and could not be read,
// which is a different fact from a value the message does not carry.
type readable struct{ input, observed bool }

// runContext restates the run's own manifest. Nothing is recomputed from the
// payloads: what the run recorded about itself is what an explanation reports,
// and the verifying reader has already checked it against the bytes.
func runContext(path string, run *replay.Run) RunContext {
	context := RunContext{
		Path: path, Identity: run.Identity, Schema: run.Manifest.Schema,
		State: run.Manifest.State, ContainsSourceValues: run.Manifest.ContainsSourceValues,
		ExportPolicy:         run.Manifest.ExportPolicy,
		SourceBundleIdentity: run.Manifest.SourceBundleIdentity,
		TargetIdentity:       targetIdentity(run.Manifest.Target),
		Target:               run.Manifest.Target,
		StartedAt:            run.Manifest.StartedAt, CompletedAt: run.Manifest.CompletedAt,
		Transformations: slices.Clone(run.Manifest.Transformations),
		Messages:        make([]Message, 0, len(run.Events)),
	}
	for _, event := range run.Events {
		context.Messages = append(context.Messages, Message{
			Source: event.SourceOccurrence, Outbound: event.OutboundOccurrence,
			Outcome: event.Outcome, Delivery: event.Delivery,
			ACKCorrelation: event.ACK.Correlation, ACKCode: event.ACK.Code,
			Elapsed: time.Duration(event.ElapsedNS),
			Sent:    payloadName(event.Sent.Path, event.Sent.Size), Received: payloadName(event.Received.Path, event.Received.Size),
			SentPartial: event.Sent.Size != event.Intended.Size,
		})
	}
	return context
}

// payloadName is the file a payload was retained in, and the empty string for
// one the run retained no bytes for. An unattempted delivery keeps an empty
// file, and naming it would offer a reader something to open that holds
// nothing.
func payloadName(path string, size int) string {
	if size == 0 {
		return ""
	}
	return path
}

// runEvidence reads the run's two message boundaries into the evidence an
// assertion set is evaluated against. The observed scope is what the interface
// produced — the acknowledgement it answered with — and the input scope is
// what the run sent. Both are addressed by the source occurrence id, which is
// the identity a case bundle, a test spec and an assertion already name a
// message by.
//
// A payload the run retained nothing for, and one that did not parse as a
// single message, are both left out. That is deliberate: an assertion naming
// one is an unknown_message execution error rather than an omitted value,
// because "we could not read this" is not "this is not there".
//
// A partial send needs no separate rule. Fewer bytes than the run intended is
// a truncated MLLP frame, which carries no end block and never parses, so it
// is already excluded here — and a field those bytes never reached can never
// read as a field the message omitted. The explanation still names it as the
// truncation it was rather than as evidence that could not be read.
func runEvidence(run *replay.Run) (assertion.Evidence, map[string]readable, error) {
	evidence := assertion.Evidence{
		Observed: make(map[string]assertion.Message, len(run.Events)),
		Input:    make(map[string]assertion.Message, len(run.Events)),
	}
	states := make(map[string]readable, len(run.Events))
	for _, event := range run.Events {
		sent, err := run.Raw(event.Sent)
		if err != nil {
			return assertion.Evidence{}, nil, errors.New("a run payload does not match the evidence the run recorded")
		}
		received, err := run.Raw(event.Received)
		if err != nil {
			return assertion.Evidence{}, nil, errors.New("a run payload does not match the evidence the run recorded")
		}
		state := readable{}
		if message, ok := single(sent); ok {
			evidence.Input[event.SourceOccurrence], state.input = message, true
		}
		if message, ok := single(received); ok {
			evidence.Observed[event.SourceOccurrence], state.observed = message, true
		}
		states[event.SourceOccurrence] = state
	}
	return evidence, states, nil
}

// single parses one retained payload as exactly one message. Anything else —
// no bytes, unparseable bytes, or several messages in one payload — is not a
// message an assertion can address.
func single(raw []byte) (assertion.Message, bool) {
	if len(raw) == 0 {
		return assertion.Message{}, false
	}
	document, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
	if err != nil || len(document.Messages) != 1 {
		return assertion.Message{}, false
	}
	return assertion.Message{Document: document, Index: 0}, true
}

// observations assembles the record scopes the set reads, and refuses every
// other arrangement rather than deciding an assertion from something narrower.
//
// A scope the set asks about and nobody supplied is refused; a scope somebody
// supplied and the set never asks about is refused; and a source whose records
// cannot be derived again is refused by name. None of the three becomes an
// incomplete observation: that class means a window that did not complete, and
// borrowing it for evidence this release cannot reproduce would report a
// collection failure that did not happen.
func readObservations(set assertion.Set, input Input, evidence assertion.Evidence) ([]ObservationContext, error) {
	declared := map[assertion.RecordScope]ObservationInput{
		assertion.BeforeRecords: input.Before,
		assertion.AfterRecords:  input.After,
	}
	read := scopes(set)
	assembled := make([]ObservationContext, 0, len(declared))
	for _, scope := range []assertion.RecordScope{assertion.BeforeRecords, assertion.AfterRecords} {
		supplied := declared[scope]
		switch {
		case read[scope] && !supplied.declared():
			return nil, errors.New("this set asks about the records of the " + string(scope) + " observation, which is explained only from the completion record and the observation source that produced it")
		case !read[scope] && supplied.declared():
			return nil, errors.New("this set asks nothing about the records of the " + string(scope) + " observation, so the documents supplied for it would explain nothing")
		case !read[scope]:
			continue
		}
		if supplied.Completion == "" || supplied.Source == "" {
			return nil, errors.New("explaining the records of the " + string(scope) + " observation requires both its completion record and its observation source")
		}
		context, err := readObservation(scope, supplied, evidence)
		if err != nil {
			return nil, err
		}
		assembled = append(assembled, context)
	}
	return assembled, nil
}

// readObservation reads one scope's two documents, derives the records it
// settled on again from the evidence it read, and checks that the record
// describes this run rather than some other one.
func readObservation(scope assertion.RecordScope, supplied ObservationInput, evidence assertion.Evidence) (ObservationContext, error) {
	completion, err := observewindow.ReadCompletion(supplied.Completion)
	if err != nil {
		return ObservationContext{}, err
	}
	if err := completion.Err(); err != nil {
		return ObservationContext{}, err
	}
	source, err := observesource.ReadSource(supplied.Source)
	if err != nil {
		return ObservationContext{}, err
	}
	keys, err := observesource.Relink(source, completion)
	if err != nil {
		return ObservationContext{}, err
	}
	context := ObservationContext{
		Scope: scope, CompletionPath: supplied.Completion, SourcePath: supplied.Source,
		Schema: completion.Schema, SourceSchema: source.Schema,
		WindowIdentity: completion.WindowIdentity, Source: completion.Source, Status: completion.Status,
		OpenedAt: completion.OpenedAt, ClosedAt: completion.ClosedAt,
		StableSamples: completion.StableSamples, QuietPeriod: completion.QuietPeriod,
		Records: completion.RecordsObserved, Keys: keys,
	}
	if source.Capture != nil {
		context.CapturePath = source.Capture.Path
	}
	if err := bind(&context, source, completion, evidence); err != nil {
		return ObservationContext{}, err
	}
	return context, nil
}

// bind checks the observation against the run it is being explained beside.
//
// A completion records the occurrences the run that collected it declared it
// produced, and what the window observed for each. That record is the only
// thing in it that names a run at all, so it is what an explanation checks: an
// occurrence the completion says was produced must be one this run actually
// produced, read out of this run's own messages at the position the source
// declares the record key sits at. An observation collected beside some other
// run is refused rather than reported under this run's identity.
//
// A completion that recorded no correlation binds nothing, and that is not an
// error: naming what a run produced is an operator's choice at collection
// time. The explanation reports the absence instead of implying a link nobody
// recorded.
func bind(context *ObservationContext, source observesource.Source, completion observewindow.Completion, evidence assertion.Evidence) error {
	context.Correlations = slices.Clone(completion.Correlations)
	for _, correlation := range completion.Correlations {
		switch correlation.Kind {
		case observewindow.Matched:
			context.Matched++
		case observewindow.AmbiguousMatch:
			context.Ambiguous++
		default:
			context.Unmatched++
		}
	}
	if len(completion.Correlations) == 0 || source.Capture == nil {
		return nil
	}
	selector, err := source.Capture.Selector()
	if err != nil {
		return err
	}
	produced := runRecordKeys(evidence, selector)
	for _, correlation := range completion.Correlations {
		if !produced[correlation.Produced] {
			return errors.New("the " + string(context.Scope) + " observation records an occurrence this run did not produce, so it does not describe this run")
		}
	}
	return nil
}

// runRecordKeys reads the declared record-key position out of every message of
// the run, on both sides of the exchange. Which side carries the key depends on
// the scope the capture declared — a booking's own identifier and the
// acknowledgement that answers it hold it in different places — so both are
// read and the union is what this run produced.
//
// A message that does not carry the position contributes nothing. This decides
// whether an observation describes this run, not what any value means.
func runRecordKeys(evidence assertion.Evidence, selector hl7.Selector) map[string]bool {
	keys := map[string]bool{}
	for _, side := range []map[string]assertion.Message{evidence.Input, evidence.Observed} {
		for _, message := range side {
			value, err := message.Document.Read(message.Index, selector, hl7.IgnoreMSH18)
			if err != nil {
				continue
			}
			if key, ok := value.Text(); ok {
				keys[key] = true
			}
		}
	}
	return keys
}

// scopes reports which record scopes a set reads. A collection, a quantified
// collection and a transition all address observations, and a transition
// addresses two.
func scopes(set assertion.Set) map[assertion.RecordScope]bool {
	read := map[assertion.RecordScope]bool{}
	for _, item := range set.Assertions {
		switch {
		case item.Subject.Collection != nil:
			read[item.Subject.Collection.Scope] = true
		case item.Subject.Each != nil:
			read[item.Subject.Each.Scope] = true
		case item.Subject.Transition != nil:
			read[item.Subject.Transition.From] = true
			read[item.Subject.Transition.To] = true
		}
	}
	return read
}

// links pairs every assertion with the evidence deciding it read. An
// evaluation that produced no results still produces the links, because an
// execution error is exactly the moment somebody needs to know which evidence
// an assertion was asking about.
func links(set assertion.Set, results []assertion.Result, explanation Explanation) []Detail {
	decided := make(map[string]assertion.Result, len(results))
	for _, result := range results {
		decided[result.ID] = result
	}
	details := make([]Detail, 0, len(set.Assertions))
	for _, item := range set.Assertions {
		detail := Detail{
			ID: item.ID, Operator: item.Operator, Subject: item.Subject,
			When: item.When, Expected: item.Expected, Evidence: evidenceLinks(item, explanation),
		}
		// Results are paired by the author-chosen id rather than by position.
		// An id is unique within a set, so the pairing cannot silently shift;
		// an assertion with no result is one the evaluation never reached.
		if result, evaluated := decided[item.ID]; evaluated {
			detail.Outcome, detail.Observed = result.Outcome, result.Observed
		}
		details = append(details, detail)
	}
	return details
}

// evidenceLinks names every place one assertion read from, including the field
// its condition read. The links are positions in retained evidence, in the
// order the assertion reads them.
func evidenceLinks(item assertion.Assertion, explanation Explanation) []Link {
	links := make([]Link, 0, 3)
	if item.When != nil {
		links = append(links, fieldLink(item.When.Field, explanation.Run))
	}
	switch {
	case item.Subject.Field != nil:
		links = append(links, fieldLink(*item.Subject.Field, explanation.Run))
	case item.Subject.Pair != nil:
		links = append(links, fieldLink(item.Subject.Pair.Left, explanation.Run), fieldLink(item.Subject.Pair.Right, explanation.Run))
	case item.Subject.Collection != nil:
		links = append(links, recordLink(item.Subject.Collection.Scope, explanation))
	case item.Subject.Each != nil:
		links = append(links, recordLink(item.Subject.Each.Scope, explanation))
	case item.Subject.Transition != nil:
		links = append(links, recordLink(item.Subject.Transition.From, explanation), recordLink(item.Subject.Transition.To, explanation))
	}
	return links
}

// fieldLink names the payload file one field reference addresses. A reference
// the run holds no occurrence for names no payload, which is what makes an
// unknown_message readable rather than mysterious.
func fieldLink(ref assertion.FieldRef, run RunContext) Link {
	link := Link{Scope: string(ref.Scope), Message: ref.Message, Selector: ref.Selector, Artifact: run.Path}
	for _, message := range run.Messages {
		if message.Source != ref.Message {
			continue
		}
		if ref.Scope == assertion.ObservedMessages {
			link.Payload = message.Received
		} else {
			link.Payload = message.Sent
		}
	}
	return link
}

// recordLink names the evidence one observed scope's records were derived
// again from: the capture itself, because that is what an ordered key list can
// be re-examined against.
func recordLink(scope assertion.RecordScope, explanation Explanation) Link {
	link := Link{Scope: string(scope)}
	for _, observed := range explanation.Observations {
		if observed.Scope == scope {
			link.Artifact = observed.CapturePath
		}
	}
	return link
}

// targetIdentity is the configuration identity the run was executed under. It
// is the deterministic encoding of the target record the run itself retained,
// digested the same way every other configuration identity in readmit is. It
// identifies configuration, not receiver software and not an authenticated
// endpoint.
func targetIdentity(record replay.TargetRecord) string {
	encoded, err := json.Marshal(record, json.Deterministic(true))
	if err != nil {
		return ""
	}
	return digest(append(encoded, '\n'))
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// readBounded reads one authored document under its own contract's bound. A
// document past the bound is refused rather than truncated, because a prefix
// of an assertion set is a different set.
func readBounded(path string, limit int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("an assertion set is one readable regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open the assertion set")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, errors.New("cannot read the assertion set")
	}
	if len(data) > limit {
		return nil, errors.New("the assertion set exceeds the size this contract reads")
	}
	return data, nil
}
