package desktop

import (
	"errors"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/sequenceanalysis"
)

// MaxSequenceEvents bounds one window of a sequence. A case holds up to the
// bundle reader's own ten thousand occurrences, and a window of them is
// rendered; the whole sequence is never handed to the interface at once.
const MaxSequenceEvents = 200

// MaxEventReferences bounds the references reported beside one event, and
// MaxRelatedOccurrences the other occurrences named inside one of them. A rule
// may put thousands of occurrences in one link, so both are windows over a
// count that is always reported beside them rather than silent truncations.
const (
	MaxEventReferences    = 64
	MaxRelatedOccurrences = 32
)

// Ordering is how one event reached the position it occupies.
type Ordering string

const (
	// ObservedOrder: the case recorded an observed time for this occurrence,
	// and the sequence placed it by that time.
	ObservedOrder Ordering = "observed"
	// UnknownOrder: the case recorded no observed time for this occurrence, so
	// nothing places it against the events of another source. It keeps the
	// position its own source recorded it in, after every event that has a
	// time, and is never interleaved among them.
	UnknownOrder Ordering = "unknown"
)

// Gap is one place this case stops saying what happened. The set is closed, so
// the interface can be held to it the way it is held to an occurrence kind
// rather than to a bare string.
type Gap string

// Each gap is something the evidence already recorded, in the evidence's own
// words: the three acknowledgement outcomes are the case bundle's own link
// kinds, so the window and `readmit timeline` cannot disagree about which
// occurrence is unacknowledged. A gap is an absence in the evidence and never a
// finding about the interface that produced it.
const (
	// UnknownObservedTime: the case recorded no observed time, so this
	// occurrence is in no synchronized position.
	UnknownObservedTime Gap = "unknown_observed_time"
	// UnknownDeclaredTime: nothing decoded a declared time here — the
	// occurrence was preserved unparsed, or the position is empty, explicitly
	// null or omitted.
	UnknownDeclaredTime Gap = "unknown_declared_time"
	// UninterpretedDeclaredTime: a declared time is present and is not shaped
	// like a timestamp, so this view does not display it or order by it. Its
	// bytes are read in the inspector like every other value.
	UninterpretedDeclaredTime Gap = "uninterpreted_declared_time"
	// UnacknowledgedMessage: the case holds no acknowledgement of this
	// message. That is missing evidence, never proof that none was sent.
	UnacknowledgedMessage Gap = Gap(bundle.Unacknowledged)
	// UnmatchedAcknowledgement: this acknowledgement names a control ID no
	// occurrence of its source carries.
	UnmatchedAcknowledgement Gap = Gap(bundle.UnmatchedACK)
	// AmbiguousAcknowledgement: this acknowledgement names a control ID more
	// than one occurrence of its source carries, so none of them is selected.
	AmbiguousAcknowledgement Gap = Gap(bundle.AmbiguousACK)
)

// gapCodes are every gap this view reports, in the order it reports them. The
// counts beside them cover the whole case, so a window can never read as all
// of it.
var gapCodes = []Gap{
	UnknownObservedTime, UnknownDeclaredTime, UninterpretedDeclaredTime,
	UnacknowledgedMessage, UnmatchedAcknowledgement, AmbiguousAcknowledgement,
}

// ReferenceKind separates where one relation came from. An acknowledgement is
// the case bundle's own literal control-ID match inside one source, which
// evidence carries with no configuration at all; the other three are a
// declared rule's own reading of this case, and exist only when a rules
// document was named.
type ReferenceKind string

const (
	AcknowledgementReference ReferenceKind = "acknowledgement"
	LinkReference            ReferenceKind = "link"
	CollisionReference       ReferenceKind = "collision"
	UnsupportedReference     ReferenceKind = "unsupported"
)

// EvidenceReference is one recorded relation that names this occurrence.
//
// Linkage is the claim being made and is the engine's own: `observed` where one
// occurrence's bytes name what the other declares, `inferred` where a declared
// rule found equal keys and neither occurrence refers to the other. A collision
// and an unsupported item carry no linkage, because neither one links anything.
// Rule, Operator and Authority name the declaration that produced a link, so a
// person reading one can tell which rule they wrote produced it. Reason is the
// reporting engine's own word, one of three closed vocabularies: the case
// bundle's link kind for an acknowledgement, the collision reason for a
// collision, and the unsupported code for an unsupported item.
//
// Related names the other occurrences of the relation and Occurrences how many
// it holds in total, so a window of a large link never reads as the whole of
// it. No field value is here: which occurrences a rule placed together is a
// position, and reading what is at that position is the inspector.
type EvidenceReference struct {
	Kind        ReferenceKind      `json:"kind"`
	Linkage     correlate.Linkage  `json:"linkage,omitzero"`
	Rule        string             `json:"rule,omitzero"`
	Operator    correlate.Operator `json:"operator,omitzero"`
	Authority   string             `json:"authority,omitzero"`
	Reason      string             `json:"reason,omitzero"`
	Field       string             `json:"field,omitzero"`
	Related     []string           `json:"related"`
	Occurrences int                `json:"occurrences"`
}

// SequenceEvent is one occurrence in the sequence and in its source's lane.
//
// Ordering says how it reached its position, and it is the member that keeps
// this view honest: an occurrence the case recorded no time for is never sorted
// among the ones it did. ObservedAt is the time the capture recorded, absent
// where there is none. DeclaredState is the decoded state of the occurrence's
// own declared time, and DeclaredTime carries that time only when its bytes are
// shaped like a timestamp and can be nothing else — every other declared time
// is a value, read deliberately in the inspector like every other value.
//
// Offset and Size are the byte span in the source that holds the occurrence, so
// an event names where the original bytes are without carrying any of them.
type SequenceEvent struct {
	Position      int                 `json:"position"`
	Occurrence    string              `json:"occurrence"`
	SourceID      string              `json:"source_id"`
	Kind          bundle.EventKind    `json:"kind"`
	Direction     bundle.Direction    `json:"direction"`
	Offset        int                 `json:"offset"`
	Size          int                 `json:"size"`
	Ordering      Ordering            `json:"ordering"`
	ObservedAt    *time.Time          `json:"observed_at"`
	DeclaredState hl7.State           `json:"declared_state"`
	DeclaredTime  string              `json:"declared_time,omitzero"`
	Decoded       bool                `json:"decoded"`
	Gaps          []Gap               `json:"gaps"`
	References    []EvidenceReference `json:"references"`
	Referenced    int                 `json:"referenced"`
}

// Lane is one declared source of the case: one swimlane of the sequence. Every
// source the case declares has a lane, including one that holds no occurrence
// at all, because a source that contributed nothing is evidence about the
// capture rather than something to leave out of the picture.
//
// Earliest and Latest span only the occurrences this source recorded a time
// for. They are that source's own recorded times and are not comparable with
// another source's as a duration: no clock here is known to agree with another.
type Lane struct {
	SourceID         string     `json:"source_id"`
	Occurrences      int        `json:"occurrences"`
	Messages         int        `json:"messages"`
	Acknowledgements int        `json:"acknowledgements"`
	Unparsed         int        `json:"unparsed"`
	Ordered          int        `json:"ordered"`
	Unordered        int        `json:"unordered"`
	Earliest         *time.Time `json:"earliest"`
	Latest           *time.Time `json:"latest"`
}

// GapCount is how many occurrences of the whole case carry one gap. Every gap
// this view knows is reported, including the ones no occurrence carries, so a
// case with nothing missing says so rather than showing an empty list.
type GapCount struct {
	Gap   Gap `json:"gap"`
	Count int `json:"count"`
}

// SequenceSummary counts the whole case, never the window. Ordered and
// Unordered split the occurrences by whether anything places them in time.
type SequenceSummary struct {
	Occurrences      int `json:"occurrences"`
	Ordered          int `json:"ordered"`
	Unordered        int `json:"unordered"`
	Messages         int `json:"messages"`
	Acknowledgements int `json:"acknowledgements"`
	Unparsed         int `json:"unparsed"`
	Sent             int `json:"sent"`
	Received         int `json:"received"`
	UnknownDirection int `json:"unknown_direction"`
	Links            int `json:"links"`
	Collisions       int `json:"collisions"`
	Unsupported      int `json:"unsupported"`
}

// Sequence is one synchronized reading of one verified case: the events in the
// order their recorded times put them, the lane each one belongs to, and every
// relation the evidence and the declared rules recorded about it.
//
// It computes no correlation of its own. Declared, Links, Collisions and
// Unsupported are `readmit-correlation/v1` exactly as `readmit correlate`
// produces it over the same case and the same rules, and Rules names the entry
// that was applied — there is no default rule set here either, so a sequence
// asked for without one shows what the evidence itself recorded and nothing
// more. Boundary is the correlation report's own statement of what a link is
// and what it is not, carried rather than summarized.
//
// This result is a typed value the interface reads, not a stored document:
// nothing here is written anywhere, and no case gains a member from it.
type Sequence struct {
	Analysis        *sequenceanalysis.Report `json:"analysis,omitzero"`
	AnalysisEntry   string                   `json:"analysis_entry"`
	Case            string                   `json:"case"`
	Identity        string                   `json:"identity"`
	Rules           string                   `json:"rules"`
	Report          string                   `json:"report,omitzero"`
	RulesSHA256     string                   `json:"rules_sha256,omitzero"`
	SessionDeclared bool                     `json:"session_declared"`
	Declared        []correlate.RuleReport   `json:"declared"`
	Unsupported     []correlate.Unsupported  `json:"unsupported"`
	Lanes           []Lane                   `json:"lanes"`
	Gaps            []GapCount               `json:"gaps"`
	Summary         SequenceSummary          `json:"summary"`
	Offset          int                      `json:"offset"`
	Limit           int                      `json:"limit"`
	Total           int                      `json:"total"`
	Events          []SequenceEvent          `json:"events"`
	Clock           string                   `json:"clock"`
	Scope           string                   `json:"scope"`
	Boundary        string                   `json:"boundary,omitzero"`
}

// clockStatement is what a person has to know before reading two sources
// beside one another. It is a fact about this product: readmit records the time
// a capture observed an occurrence and never adjusts it.
const clockStatement = "An observed time is the time one capture recorded, on that machine's own clock and in the offset it recorded. No clock is assumed to agree with another, no offset is corrected, and no time zone is inferred. A declared time is what the message itself says, which is a claim by its sender."

// scopeStatement is the boundary of the ordering itself. It is the sentence
// that keeps a sequence from being read as a causal chain.
const scopeStatement = "Events are placed by the times this case recorded and by nothing else. An occurrence with no observed time is listed after the ones that have one, in the order its own source recorded it, and is never interleaved among them. Order is not causality: one event following another establishes neither that it was caused by it nor that it was late. A gap is evidence this case does not hold, never proof that nothing happened."

// SequenceRequest names the case to lay out and, optionally, the declared rules
// to read it under.
//
// Case is one entry of the open workspace and Identity the identity the window
// displayed for it, so a sequence is refused rather than drawn beside counts
// from evidence that has changed. Rules is one entry of the same workspace
// holding a `readmit-correlation-rules/v1` document; an empty one is not a
// default rule set but the absence of one, and the sequence then reports only
// what the evidence itself recorded.
type SequenceRequest struct {
	Analysis  string `json:"analysis"`
	Workspace string `json:"workspace"`
	Case      string `json:"case"`
	Identity  string `json:"identity"`
	Rules     string `json:"rules"`
	Offset    int    `json:"offset"`
	Limit     int    `json:"limit"`
}

// SequenceResult carries one state. Sequence is present whenever the case was
// verified and laid out, including when the window holds no event, because the
// lanes and the counts are the answer in that case.
type SequenceResult struct {
	State    State     `json:"state"`
	Reason   string    `json:"reason,omitzero"`
	Sequence *Sequence `json:"sequence,omitzero"`
}

func (r refusal) sequence() SequenceResult {
	return SequenceResult{State: r.state, Reason: r.reason}
}

// OpenSequence lays one verified case out as a synchronized event sequence over
// the lanes of its declared sources.
//
// It decides nothing about the evidence. The same reader `readmit timeline`
// runs verifies the case, and where a rules document is named the same engine
// `readmit correlate` runs produces the links, collisions and unsupported items
// beside each event — so this is a view of exactly what those two already
// report, laid out as a sequence rather than as a document. No correlation is
// computed here and none can be added here.
//
// It reads verified evidence under the case reader's own limits and runs to
// completion once it starts, so it holds the operation slot but is not
// interruptible. The case is not changed, and no message byte or field value
// crosses this boundary except a declared time whose bytes can be nothing but a
// timestamp.
func (a *App) OpenSequence(request SequenceRequest) SequenceResult {
	release, claimed := a.claim()
	if !claimed {
		return busyRefusal.sequence()
	}
	defer release()
	if request.Offset < 0 || request.Limit < 1 || request.Limit > MaxSequenceEvents {
		return SequenceResult{State: Failed, Reason: "a sequence renders a window beginning at or after its first event, of between 1 and " + strconv.Itoa(MaxSequenceEvents) + " events"}
	}
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return declined.sequence()
	}
	casePath, err := artifactpath.Child(root, request.Case)
	if err != nil {
		return SequenceResult{State: Failed, Reason: "a case must be named by one directory entry of the open workspace"}
	}
	opened, err := bundle.Open(casePath)
	if err != nil {
		return SequenceResult{State: Failed, Reason: "the case could not be verified as complete, unmodified evidence"}
	}
	if request.Identity == "" || request.Identity != opened.Identity {
		return SequenceResult{State: Failed, Reason: "the case identity changed; open the case again before reading its sequence"}
	}
	report, declined := correlated(root, casePath, request.Rules)
	if declined.state != "" {
		return declined.sequence()
	}
	result := laidOut(request, opened, report)
	if request.Analysis != "" {
		analysis, refused := analyzeSequence(root, request.Analysis, opened, report)
		if refused.state != "" {
			return refused.sequence()
		}
		visible := map[string]bool{}
		for _, event := range result.Sequence.Events {
			visible[event.Occurrence] = true
		}
		findings := make([]sequenceanalysis.Finding, 0)
		for _, finding := range analysis.Findings {
			if visible[finding.Occurrence] {
				findings = append(findings, finding)
			}
		}
		analysis.Findings = findings
		result.Sequence.Analysis = analysis
		result.Sequence.AnalysisEntry = request.Analysis
	}
	return result
}

// correlated applies the named rules, or none at all. There is no default rule
// set in the window for the same reason there is none on the command line:
// deciding that two identifiers denote one patient is a declaration somebody
// makes, never one readmit makes because nobody said otherwise.
func correlated(root, casePath, name string) (*correlate.Report, refusal) {
	if name == "" {
		return nil, refusal{}
	}
	if err := artifactpath.EntryName(name); err != nil {
		return nil, refusal{Failed, "correlation rules must be named by one entry of the open workspace"}
	}
	// The entry itself is inspected without following a symbolic link, exactly
	// as the grid inspects an index, so a listing can never be used to reach a
	// file outside the folder the person opened.
	path := artifactpath.JoinReference(root, name)
	entry, err := os.Lstat(path)
	switch {
	case err != nil || !entry.Mode().IsRegular():
		return nil, refusal{Failed, "correlation rules must be one regular file of the open workspace"}
	case entry.Size() > correlate.MaxRulesBytes:
		return nil, refusal{Failed, "the correlation rules document is larger than this release reads"}
	}
	declared, err := os.ReadFile(path)
	// A document this account cannot read is a different answer from one the
	// reader refused: there is nothing to fix in it, and writing it again is
	// not the remedy.
	if errors.Is(err, fs.ErrPermission) {
		return nil, refusal{PermissionDenied, "this account cannot read the chosen correlation rules"}
	}
	if err != nil {
		return nil, refusal{Failed, "the correlation rules document could not be read"}
	}
	rules, err := correlate.ParseRules(declared)
	if err != nil {
		// The reader's own diagnostic names the declaration at fault and never
		// repeats a value, so it is reported as it is rather than replaced by a
		// sentence that says less about what to fix.
		return nil, refusal{Failed, err.Error()}
	}
	produced, err := correlate.Run(casePath, rules)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	return &produced, refusal{}
}

// laidOut turns one verified case and one optional correlation report into the
// lanes, the counts and the requested window of the sequence.
func laidOut(request SequenceRequest, opened *bundle.Bundle, report *correlate.Report) SequenceResult {
	references, gaps := recorded(opened, report)
	all := ordered(opened, gaps)
	lanes, summary := counted(opened, all, report)
	described := &Sequence{
		Case: request.Case, Identity: opened.Identity, Rules: request.Rules,
		SessionDeclared: opened.Manifest.Provenance.SessionID != "",
		Declared:        []correlate.RuleReport{}, Unsupported: []correlate.Unsupported{},
		Lanes: lanes, Gaps: gapCounts(all), Summary: summary,
		Offset: request.Offset, Limit: request.Limit, Total: len(all),
		Clock: clockStatement, Scope: scopeStatement,
	}
	if report != nil {
		described.Report, described.RulesSHA256 = report.Schema, report.RulesSHA256
		described.Boundary = report.Scope
		// A report always carries both of these, but the shape the interface
		// draws is decided here rather than borrowed: a member it renders as a
		// list is a list, never null.
		if report.Rules != nil {
			described.Declared = report.Rules
		}
		if report.Unsupported != nil {
			described.Unsupported = report.Unsupported
		}
	}
	window := make([]SequenceEvent, 0, request.Limit)
	if request.Offset < len(all) {
		window = append(window, all[request.Offset:min(len(all), request.Offset+request.Limit)]...)
	}
	// References are attached to the window alone. Every occurrence of the case
	// is counted in Referenced, so an event that names more relations than this
	// window carries says how many rather than appearing to hold them all.
	for i := range window {
		named := references[window[i].Occurrence]
		window[i].Referenced = len(named)
		if len(named) > 0 {
			window[i].References = named[:min(len(named), MaxEventReferences)]
		}
	}
	described.Events = window
	switch {
	case len(all) == 0:
		return SequenceResult{State: Empty, Reason: "this case holds no occurrence", Sequence: described}
	case len(window) == 0:
		return SequenceResult{State: Empty, Reason: "this window begins past the last event of this sequence", Sequence: described}
	}
	return SequenceResult{State: Completed, Sequence: described}
}

// ordered places the events. Everything the case recorded a time for is sorted
// by that time, and everything it did not is kept in the order its own source
// recorded it and listed after them. Sorting an unknown time among known ones
// would invent a precision the evidence does not have, which is the one thing a
// sequence exists to avoid.
func ordered(opened *bundle.Bundle, gaps map[string][]Gap) []SequenceEvent {
	timed := make([]SequenceEvent, 0, len(opened.Events))
	untimed := make([]SequenceEvent, 0, len(opened.Events))
	for _, event := range opened.Events {
		described := sequenceEvent(opened, event, gaps)
		if described.Ordering == ObservedOrder {
			timed = append(timed, described)
			continue
		}
		untimed = append(untimed, described)
	}
	// A stable sort keeps two occurrences recorded at the same instant in the
	// order the case recorded them rather than in an order nothing established.
	slices.SortStableFunc(timed, func(a, b SequenceEvent) int { return a.ObservedAt.Compare(*b.ObservedAt) })
	all := append(timed, untimed...)
	for i := range all {
		all[i].Position = i + 1
	}
	return all
}

// counted reports one lane per declared source, in the order the case declares
// them and whether or not the source holds an occurrence, together with the
// counts of the whole case. Both come out of one traversal, so a lane and the
// summary beside it can never be tallied from two readings of one event.
func counted(opened *bundle.Bundle, all []SequenceEvent, report *correlate.Report) ([]Lane, SequenceSummary) {
	lanes := make([]Lane, 0, len(opened.Manifest.Sources))
	at := make(map[string]int, len(opened.Manifest.Sources))
	for _, source := range opened.Manifest.Sources {
		at[source.ID] = len(lanes)
		lanes = append(lanes, Lane{SourceID: source.ID})
	}
	summary := SequenceSummary{Occurrences: len(all)}
	if report != nil {
		summary.Links, summary.Collisions = report.Summary.Links, report.Summary.Collisions
		summary.Unsupported = report.Summary.Unsupported
	}
	// An occurrence naming a source the manifest does not declare is counted
	// here and reported in no lane: leaving it out of the whole-case counts
	// would hide evidence this case holds, and inventing a lane for it would
	// report a source the case does not declare.
	var undeclared Lane
	for _, described := range all {
		lane := &undeclared
		if index, known := at[described.SourceID]; known {
			lane = &lanes[index]
		}
		lane.Occurrences++
		switch described.Kind {
		case bundle.Message:
			summary.Messages, lane.Messages = summary.Messages+1, lane.Messages+1
		case bundle.Acknowledgement:
			summary.Acknowledgements, lane.Acknowledgements = summary.Acknowledgements+1, lane.Acknowledgements+1
		case bundle.Unparsed:
			summary.Unparsed, lane.Unparsed = summary.Unparsed+1, lane.Unparsed+1
		}
		switch described.Direction {
		case bundle.Outbound:
			summary.Sent++
		case bundle.Inbound:
			summary.Received++
		default:
			summary.UnknownDirection++
		}
		if described.Ordering != ObservedOrder {
			summary.Unordered, lane.Unordered = summary.Unordered+1, lane.Unordered+1
			continue
		}
		summary.Ordered, lane.Ordered = summary.Ordered+1, lane.Ordered+1
		if lane.Earliest == nil || described.ObservedAt.Before(*lane.Earliest) {
			lane.Earliest = described.ObservedAt
		}
		if lane.Latest == nil || described.ObservedAt.After(*lane.Latest) {
			lane.Latest = described.ObservedAt
		}
	}
	return lanes, summary
}

// gapCounts totals every gap over the whole case, in the fixed order this view
// reports them, so a window of the events never reads as all of the gaps.
func gapCounts(all []SequenceEvent) []GapCount {
	totals := make(map[Gap]int, len(gapCodes))
	for _, described := range all {
		for _, gap := range described.Gaps {
			totals[gap]++
		}
	}
	counted := make([]GapCount, 0, len(gapCodes))
	for _, code := range gapCodes {
		counted = append(counted, GapCount{Gap: code, Count: totals[code]})
	}
	return counted
}

// sequenceEvent describes one occurrence. The declared time is the one value
// this boundary carries, and only when its bytes are shaped like a timestamp
// and can be nothing else: that is the rule `readmit timeline` already applies
// to the same field, held in one place so the two cannot come to disagree.
// Every other declared time is reported as the state it decoded to and read in
// the inspector like any other value.
func sequenceEvent(opened *bundle.Bundle, event bundle.Event, gaps map[string][]Gap) SequenceEvent {
	described := SequenceEvent{
		Occurrence: event.ID, SourceID: event.SourceID,
		Kind: event.Kind, Direction: event.Direction,
		Offset: event.Offset, Size: event.Payload.Size,
		Ordering: UnknownOrder, ObservedAt: event.ObservedAt,
		DeclaredState: hl7.Omitted, Decoded: event.Fields != nil,
		Gaps: slices.Clone(gaps[event.ID]), References: []EvidenceReference{},
	}
	if described.Gaps == nil {
		described.Gaps = []Gap{}
	}
	if event.ObservedAt != nil {
		described.Ordering = ObservedOrder
	} else {
		described.Gaps = append(described.Gaps, UnknownObservedTime)
	}
	if event.Fields == nil {
		return declaredGap(described, UnknownDeclaredTime)
	}
	described.DeclaredState = event.Fields.DeclaredTime.State
	if described.DeclaredState != hl7.Present {
		return declaredGap(described, UnknownDeclaredTime)
	}
	value := opened.Value(event.ID, event.Fields.DeclaredTime)
	if !bundle.DeclaredTimestamp(value) {
		return declaredGap(described, UninterpretedDeclaredTime)
	}
	described.DeclaredTime = string(value)
	return described
}

func declaredGap(described SequenceEvent, gap Gap) SequenceEvent {
	described.Gaps = append(described.Gaps, gap)
	return described
}

// recorded collects, once, every relation and every acknowledgement gap the
// case and the correlation report already recorded, keyed by the occurrence
// they name. Nothing here reads a rule or compares a key: both readings were
// produced by the engines that own them, and this only puts each one beside the
// event it is about.
func recorded(opened *bundle.Bundle, report *correlate.Report) (map[string][]EvidenceReference, map[string][]Gap) {
	references := make(map[string][]EvidenceReference, len(opened.Events))
	gaps := make(map[string][]Gap, len(opened.Events))
	add := func(occurrence string, reference EvidenceReference) {
		references[occurrence] = append(references[occurrence], reference)
	}
	for _, link := range opened.Correlations {
		named := make([]string, 0, len(link.MessageIDs)+1)
		if link.ACKID != "" {
			named = append(named, link.ACKID)
		}
		named = append(named, link.MessageIDs...)
		reference := EvidenceReference{
			Kind: AcknowledgementReference, Reason: string(link.Kind), Occurrences: len(named),
		}
		// Only a matched acknowledgement is an observed linkage: its own bytes
		// name the control ID exactly one occurrence of its source carries.
		if link.Kind == bundle.Matched {
			reference.Linkage = correlate.Observed
		}
		spread(add, named, reference)
		// The gap belongs to the occurrence whose evidence is incomplete: the
		// acknowledgement that resolved to nothing or to more than one thing,
		// and the message this case holds no acknowledgement of.
		switch link.Kind {
		case bundle.UnmatchedACK, bundle.AmbiguousACK:
			gaps[link.ACKID] = append(gaps[link.ACKID], Gap(link.Kind))
		case bundle.Unacknowledged:
			for _, occurrence := range link.MessageIDs {
				gaps[occurrence] = append(gaps[occurrence], Gap(link.Kind))
			}
		}
	}
	if report == nil {
		return references, gaps
	}
	for _, link := range report.Links {
		named := occurrenceIDs(link.Occurrences)
		reference := EvidenceReference{
			Kind: LinkReference, Linkage: link.Linkage, Rule: link.Rule,
			Operator: link.Operator, Authority: link.Authority, Occurrences: len(named),
		}
		spread(add, named, reference)
	}
	for _, collision := range report.Collisions {
		named := occurrenceIDs(collision.Occurrences)
		if collision.Declaring != nil && !slices.Contains(named, collision.Declaring.Occurrence) {
			named = append(named, collision.Declaring.Occurrence)
		}
		reference := EvidenceReference{
			Kind: CollisionReference, Rule: collision.Rule, Operator: collision.Operator,
			Reason: collision.Reason, Occurrences: len(named),
		}
		spread(add, named, reference)
	}
	for _, unsupported := range report.Unsupported {
		if unsupported.Occurrence == "" {
			continue
		}
		add(unsupported.Occurrence, EvidenceReference{
			Kind: UnsupportedReference, Rule: unsupported.Rule,
			Reason: unsupported.Code, Field: unsupported.Field, Related: []string{},
		})
	}
	return references, gaps
}

// spread puts one relation beside every occurrence it names, each time as that
// occurrence sees it: the rest of the relation, and never itself.
func spread(add func(string, EvidenceReference), named []string, reference EvidenceReference) {
	for _, occurrence := range named {
		beside := reference
		beside.Related = others(named, occurrence)
		add(occurrence, beside)
	}
}

// occurrenceIDs takes the occurrence identifiers of correlation references and
// nothing else. A reference carries no field bytes, and neither does this.
func occurrenceIDs(named []correlate.Reference) []string {
	identifiers := make([]string, 0, len(named))
	for _, reference := range named {
		identifiers = append(identifiers, reference.Occurrence)
	}
	return identifiers
}

// others is the rest of a relation as seen from one of its occurrences, bounded
// so that one link over thousands of occurrences cannot become one row holding
// thousands of identifiers. How many there are in total is reported beside it.
func others(named []string, occurrence string) []string {
	rest := make([]string, 0, min(len(named), MaxRelatedOccurrences))
	for _, other := range named {
		if other == occurrence {
			continue
		}
		if len(rest) == MaxRelatedOccurrences {
			break
		}
		rest = append(rest, other)
	}
	return rest
}
