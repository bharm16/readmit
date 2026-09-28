package desktop

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
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

// MaxSequenceFields bounds the field selectors a sequence names.
const MaxSequenceFields = 256

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
	// MessageOrder: the message declared an instant this view reads, and the
	// sequence placed it by that claim, on its sender's clock.
	MessageOrder Ordering = "message"
	// SourceSequenceOrder: the sequence places nothing in time; every event
	// keeps the position its own source recorded it in.
	SourceSequenceOrder Ordering = "source"
)

// TimeBasis is the time a timeline places its events by. Observed is the time
// a capture recorded; Message is the instant the message itself declares,
// which is its sender's claim and never becomes a capture time; Source places
// nothing in time and keeps each source's own order. Import time is never a
// basis: it is when a person imported the evidence, not when anything
// happened.
type TimeBasis string

const (
	ObservedBasis TimeBasis = "observed"
	MessageBasis  TimeBasis = "message"
	SourceBasis   TimeBasis = "source"
)

// TimelineFilter narrows the events a timeline window lists. The counts beside
// the window always cover the whole case.
type TimelineFilter string

const (
	// UnresolvedFilter lists the events of relationships the evidence left
	// unresolved: an acknowledgement matching nothing or more than one message,
	// and identifiers a rule found colliding.
	UnresolvedFilter TimelineFilter = "unresolved"
	// GapsFilter lists the events whose expected counterpart the evidence does
	// not hold: an unacknowledged message, or an absence a coverage
	// declaration found.
	GapsFilter TimelineFilter = "gaps"
	// UntimedFilter lists the events the active time basis places nowhere.
	UntimedFilter TimelineFilter = "untimed"
)

// ClockKind is whose clock placed the events of one clock.
type ClockKind string

const (
	// SessionClock: one recorded session observed every source, so their
	// observed times share one recorder's clock and one axis.
	SessionClock ClockKind = "session"
	// SourceClock: this source's observed times are its own capture's clock;
	// nothing establishes that it agrees with another source's.
	SourceClock ClockKind = "source"
	// SenderClock: instants the messages of this source declare, each on its
	// sender's own clock.
	SenderClock ClockKind = "sender"
)

// Clock is one set of comparable instants. Events share a time axis only when
// they carry the same clock; events of two clocks are never aligned against
// one another, and the order between them establishes nothing.
type Clock struct {
	ID      string    `json:"id"`
	Kind    ClockKind `json:"kind"`
	Sources []string  `json:"sources"`
}

// RelationBasis is where one relationship came from: the evidence itself, a
// named link rule, or an analyst's review.
type RelationBasis string

const (
	RecordedRelation RelationBasis = "recorded"
	RuleRelation     RelationBasis = "rule"
	ReviewedRelation RelationBasis = "reviewed"
)

// RelationEnd is one occurrence a relationship names. InWindow says whether
// the window carries its event; one that does not is an endpoint to show as
// leading out of the window, never a line drawn to a guessed place.
type RelationEnd struct {
	Occurrence string `json:"occurrence"`
	SourceID   string `json:"source_id"`
	InWindow   bool   `json:"in_window"`
}

// Relation is one relationship at least one event of the window takes part
// in. Endpoints lists at most MaxRelatedOccurrences of its occurrences and
// Occurrences counts them all: only a relationship of exactly two
// occurrences is one From→To edge, the first named first. Status is the
// relationship's review state under the applied review, for a rule's link
// and a reviewed one; Ambiguous marks equality or a reference nothing
// resolved to one counterpart, whose candidates stay listed.
//
// BasisName is the basis as a person reads it: the name of the link rules a
// rule's relationship came from, Recorded or Reviewed.
type Relation struct {
	ID          string                 `json:"id"`
	Basis       RelationBasis          `json:"basis"`
	BasisName   string                 `json:"basis_name"`
	Rule        string                 `json:"rule,omitzero"`
	Operator    correlate.Operator     `json:"operator,omitzero"`
	Linkage     correlate.Linkage      `json:"linkage,omitzero"`
	Reason      string                 `json:"reason,omitzero"`
	Status      correlate.ReviewStatus `json:"status,omitzero"`
	Ambiguous   bool                   `json:"ambiguous"`
	Endpoints   []RelationEnd          `json:"endpoints"`
	Occurrences int                    `json:"occurrences"`
}

// TimelineProblems count the whole case's actual problems. Unresolved links
// are acknowledgements matching no message or more than one, and rule
// collisions; gaps are the occurrences whose expected counterpart the
// evidence does not hold. Neither is proof of a failed delivery: a missing
// acknowledgement or observation is missing evidence.
type TimelineProblems struct {
	UnresolvedLinks int `json:"unresolved_links"`
	Gaps            int `json:"gaps"`
}

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
	Position   int              `json:"position"`
	Occurrence string           `json:"occurrence"`
	SourceID   string           `json:"source_id"`
	Kind       bundle.EventKind `json:"kind"`
	Direction  bundle.Direction `json:"direction"`
	Offset     int              `json:"offset"`
	Size       int              `json:"size"`
	Ordering   Ordering         `json:"ordering"`
	ObservedAt *time.Time       `json:"observed_at"`
	// SourceSequence is the position the occurrence's own source recorded it
	// at. At is the instant the active time basis places it at, absent where
	// that basis places it nowhere, and Clock names the clock At is on.
	SourceSequence int        `json:"source_sequence"`
	At             *time.Time `json:"at"`
	Clock          string     `json:"clock,omitzero"`
	// MessageType and Trigger are the parsed MSH-9.1 and MSH-9.2 of the
	// occurrence, escaped and bounded as the message list shows them, and
	// absent where it declares none or was not parsed. No other field value,
	// the control ID included, is here: that is the inspector's.
	MessageType   string              `json:"message_type,omitzero"`
	Trigger       string              `json:"trigger,omitzero"`
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
// Earliest and Latest span only the occurrences the active time basis placed
// in time. They are that source's own times and are not comparable with
// another source's as a duration unless one clock placed both.
//
// SourceName is the name declared for the source (sourceNames), empty when
// nothing names it and the lane reads by its exact SourceID.
type Lane struct {
	SourceID         string     `json:"source_id"`
	SourceName       string     `json:"source_name"`
	Occurrences      int        `json:"occurrences"`
	Messages         int        `json:"messages"`
	Acknowledgements int        `json:"acknowledgements"`
	Unparsed         int        `json:"unparsed"`
	Ordered          int        `json:"ordered"`
	Unordered        int        `json:"unordered"`
	Earliest         *time.Time `json:"earliest"`
	Latest           *time.Time `json:"latest"`
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
	// Context is the request this answers, so an answer for a selection the
	// window has since replaced is recognizably stale.
	Context RequestContext `json:"context"`
	// Basis is the time basis applied, and Clocks the clocks that placed at
	// least one event under it. Untimed counts the events of the whole case
	// a time basis places nowhere; source order has no untimed section.
	Basis   TimeBasis `json:"basis"`
	Clocks  []Clock   `json:"clocks"`
	Untimed int       `json:"untimed"`
	// Relations are the relationships the window's events take part in, and
	// Problems the whole case's actual problems.
	Relations []Relation       `json:"relations"`
	Problems  TimelineProblems `json:"problems"`
	// Fields are the field selectors (SEG-N) a parsed occurrence of the case
	// holds a present value at, in segment then field order, at most
	// MaxSequenceFields of them: the fields a link rule can name here.
	Fields []string `json:"fields"`
	// LinkRules, Coverage and Review are the exact named versions applied,
	// with the names of the first two. Mapping is the reviewed mapping's
	// identity, which a decision must name.
	LinkRules       *ItemRef                 `json:"link_rules,omitzero"`
	LinkRulesName   string                   `json:"link_rules_name,omitzero"`
	Coverage        *ItemRef                 `json:"coverage,omitzero"`
	CoverageName    string                   `json:"coverage_name,omitzero"`
	Review          *ItemRef                 `json:"review,omitzero"`
	Mapping         string                   `json:"mapping,omitzero"`
	Analysis        *sequenceanalysis.Report `json:"analysis,omitzero"`
	Case            string                   `json:"case"`
	Identity        string                   `json:"identity"`
	Rules           string                   `json:"rules"`
	Report          string                   `json:"report,omitzero"`
	RulesSHA256     string                   `json:"rules_sha256,omitzero"`
	SessionDeclared bool                     `json:"session_declared"`
	Declared        []correlate.RuleReport   `json:"declared"`
	Unsupported     []correlate.Unsupported  `json:"unsupported"`
	Lanes           []Lane                   `json:"lanes"`
	Summary         SequenceSummary          `json:"summary"`
	Offset          int                      `json:"offset"`
	Limit           int                      `json:"limit"`
	Total           int                      `json:"total"`
	Events          []SequenceEvent          `json:"events"`
	Boundary        string                   `json:"boundary,omitzero"`
}

// SequenceRequest names the case to lay out and, optionally, the declared rules
// to read it under.
//
// Case is one entry of the open workspace and Identity the identity the window
// displayed for it, so a sequence is refused rather than drawn beside counts
// from evidence that has changed. Rules is one entry of the same workspace
// holding a `readmit-correlation-rules/v1` document; an empty one is not a
// default rule set but the absence of one, and the sequence then reports only
// what the evidence itself recorded.
//
// A timeline of a project names its link rules, coverage and review as the
// project's objects instead: LinkRules, Coverage and Review are exact
// versions, Context the project, and Case the project entry that holds the
// case. Review, left empty, is the review the project holds of exactly this
// case under exactly these link rules, which the answer names. Basis is the
// time basis, Observed when empty; Sources and Filter select the events the
// window lists, and everything counted beside it still covers the whole case.
type SequenceRequest struct {
	Context   RequestContext `json:"context,omitzero"`
	Analysis  string         `json:"analysis"`
	Workspace string         `json:"workspace"`
	Case      string         `json:"case"`
	Identity  string         `json:"identity"`
	Rules     string         `json:"rules"`
	LinkRules *ItemRef       `json:"link_rules,omitzero"`
	Coverage  *ItemRef       `json:"coverage,omitzero"`
	Review    *ItemRef       `json:"review,omitzero"`
	Basis     TimeBasis      `json:"basis,omitzero"`
	Sources   []string       `json:"sources,omitzero"`
	Filter    TimelineFilter `json:"filter,omitzero"`
	Offset    int            `json:"offset"`
	Limit     int            `json:"limit"`
}

// SequenceResult carries one state. Sequence is present whenever the case was
// verified and laid out, including when the window holds no event, because the
// lanes and the counts are the answer in that case.
type SequenceResult struct {
	State    State          `json:"state"`
	Reason   string         `json:"reason,omitzero"`
	Context  RequestContext `json:"context"`
	Sequence *Sequence      `json:"sequence,omitzero"`
}

func (r *SequenceResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

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
// timestamp and each occurrence's escaped, bounded message type and trigger,
// as the message list shows them.
func (a *App) OpenSequence(request SequenceRequest) SequenceResult {
	return run(a, false, false, func(ctx context.Context) SequenceResult {
		result := a.openSequence(ctx, request)
		result.Context = request.Context
		if result.Sequence != nil {
			result.Sequence.Context = request.Context
		}
		return result
	})
}

// applied is what a timeline applied beyond the evidence itself: the exact
// named versions, their names, and the relationship review.
type applied struct {
	linkRules, coverage, review *ItemRef
	linkRulesName, coverageName string
	view                        *correlate.ReviewedView
	analysis                    *sequenceanalysis.Report
}

func (a *App) openSequence(ctx context.Context, request SequenceRequest) SequenceResult {
	failure := func(reason string) SequenceResult { return SequenceResult{State: Failed, Reason: reason} }
	if request.Offset < 0 || request.Limit < 1 || request.Limit > MaxSequenceEvents {
		return failure("a sequence renders a window beginning at or after its first event, of between 1 and " + strconv.Itoa(MaxSequenceEvents) + " events")
	}
	if request.Basis == "" {
		request.Basis = ObservedBasis
	}
	if !slices.Contains([]TimeBasis{ObservedBasis, MessageBasis, SourceBasis}, request.Basis) {
		return failure("a timeline places events by observed time, message time or source order")
	}
	if request.Filter != "" && !slices.Contains([]TimelineFilter{UnresolvedFilter, GapsFilter, UntimedFilter}, request.Filter) {
		return failure("a timeline filters to unresolved links, gaps or untimed events")
	}
	if request.Filter == UntimedFilter && request.Basis == SourceBasis {
		return failure("source order places no event in time, so nothing in it is untimed")
	}
	named := request.LinkRules != nil || request.Coverage != nil || request.Review != nil
	if named && (request.Rules != "" || request.Analysis != "") {
		return failure("a timeline applies named link rules and coverage, or workspace documents, never both")
	}
	workspace := request.Workspace
	if workspace == "" {
		workspace = request.Context.Project
	}
	var loaded *loadedCatalog
	switch {
	case named:
		held, declined := a.loadCatalog(ctx, request.Context, false)
		if held == nil {
			return declined.sequence()
		}
		loaded, workspace = held, held.root
	case request.Workspace == "" && request.Context.Project != "" && request.Rules == "" && request.Analysis == "":
		// The project's review of the case's recorded links is overlaid
		// when the project can be read; the evidence is laid out either way.
		if held, _ := a.loadCatalog(ctx, request.Context, false); held != nil {
			loaded, workspace = held, held.root
		}
	}
	root, opened, declined := openedCase(workspace, request.Case, request.Identity)
	if root == "" {
		return declined.sequence()
	}
	casePath := artifactpath.JoinReference(root, request.Case)
	var used applied
	var report *correlate.Report
	if request.LinkRules != nil {
		rules, exact, name, err := loaded.linkRulesAt(*request.LinkRules)
		if err != nil {
			return failure(err.Error())
		}
		produced, err := correlate.Run(casePath, rules)
		if err != nil {
			return failure(err.Error())
		}
		report, used.linkRules, used.linkRulesName = &produced, &exact, name
	} else {
		report, declined = correlated(root, casePath, request.Rules)
		if declined.state != "" {
			return declined.sequence()
		}
	}
	if report != nil {
		var prior *correlate.ReviewRevision
		switch {
		case request.Review != nil:
			held, exact, err := loaded.reviewAt(*request.Review)
			if err != nil {
				return failure(err.Error())
			}
			prior, used.review = &held, &exact
		case loaded != nil:
			prior, used.review = loaded.boundReview(*report)
		}
		_, view, err := correlate.Review(opened, *report, prior, false)
		if err != nil {
			return failure("this relationship review was made for other link rules or another case; it is not applied")
		}
		used.view = &view
	} else if loaded != nil {
		// With no link rules, the review applied is the one of the case's
		// recorded links: its own history, bound to no rules.
		recordedLinks := correlate.Recorded(opened)
		var prior *correlate.ReviewRevision
		if request.Review != nil {
			held, exact, err := loaded.reviewAt(*request.Review)
			if err != nil {
				return failure(err.Error())
			}
			prior, used.review = &held, &exact
		} else {
			prior, used.review = loaded.boundReview(recordedLinks)
		}
		_, view, err := correlate.Review(opened, recordedLinks, prior, false)
		if err != nil {
			return failure("this relationship review was made for other link rules or another case; it is not applied")
		}
		used.view = &view
	}
	if request.Coverage != nil {
		declaration, exact, name, err := loaded.coverageAt(*request.Coverage)
		if err != nil {
			return failure(err.Error())
		}
		if used.analysis, err = sequenceanalysis.Evaluate(opened, declaration, report); err != nil {
			return failure(err.Error())
		}
		used.coverage, used.coverageName = &exact, name
	} else if request.Analysis != "" {
		analysis, refused := analyzeSequence(root, request.Analysis, opened, report)
		if refused.state != "" {
			return refused.sequence()
		}
		used.analysis = analysis
	}
	result := laidOut(request, opened, report, used, sourceNames(root, request.Case, opened))
	if result.Sequence != nil && used.analysis != nil {
		visible := map[string]bool{}
		for _, event := range result.Sequence.Events {
			visible[event.Occurrence] = true
		}
		findings := make([]sequenceanalysis.Finding, 0)
		for _, finding := range used.analysis.Findings {
			if visible[finding.Occurrence] {
				findings = append(findings, finding)
			}
		}
		used.analysis.Findings = findings
		result.Sequence.Analysis = used.analysis
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
func laidOut(request SequenceRequest, opened *bundle.Bundle, report *correlate.Report, used applied, names map[string]string) SequenceResult {
	references, gaps := recorded(opened, report)
	order := laneOrder(opened)
	headers, fields := readHeaders(opened)
	all := placed(opened, gaps, request.Basis, order)
	for i := range all {
		header := headers[all[i].Occurrence]
		all[i].MessageType, all[i].Trigger = header.code, header.trigger
	}
	lanes, summary := counted(opened, all, report, order)
	for i := range lanes {
		lanes[i].SourceName = names[lanes[i].SourceID]
	}
	unresolved, absent, problems := problemsOf(opened, report, used.analysis)
	described := &Sequence{
		Case: request.Case, Identity: opened.Identity, Rules: request.Rules,
		SessionDeclared: opened.Manifest.Provenance.SessionID != "",
		Declared:        []correlate.RuleReport{}, Unsupported: []correlate.Unsupported{},
		Lanes: lanes, Summary: summary,
		Offset: request.Offset, Limit: request.Limit,
		Basis: request.Basis, Clocks: clocksOf(all, order),
		Relations: []Relation{}, Problems: problems, Fields: fields,
		LinkRules: used.linkRules, LinkRulesName: used.linkRulesName,
		Coverage: used.coverage, CoverageName: used.coverageName, Review: used.review,
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
	if used.view != nil {
		described.Mapping = used.view.Mapping
	}
	if request.Basis != SourceBasis {
		described.Untimed = summary.Unordered
	}
	for _, source := range request.Sources {
		if _, declared := order[source]; !declared {
			return SequenceResult{State: Failed, Reason: "a timeline selects sources this case declares"}
		}
	}
	selected := make([]SequenceEvent, 0, len(all))
	for _, event := range all {
		if len(request.Sources) > 0 && !slices.Contains(request.Sources, event.SourceID) {
			continue
		}
		switch request.Filter {
		case UnresolvedFilter:
			if !unresolved[event.Occurrence] {
				continue
			}
		case GapsFilter:
			if !absent[event.Occurrence] {
				continue
			}
		case UntimedFilter:
			if event.At != nil {
				continue
			}
		}
		selected = append(selected, event)
	}
	described.Total = len(selected)
	window := make([]SequenceEvent, 0, request.Limit)
	if request.Offset < len(selected) {
		window = append(window, selected[request.Offset:min(len(selected), request.Offset+request.Limit)]...)
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
	described.Relations = relationsOf(opened, report, used.view, window, used.linkRulesName)
	switch {
	case len(all) == 0:
		return SequenceResult{State: Empty, Reason: "this case holds no occurrence", Sequence: described}
	case len(selected) == 0:
		return SequenceResult{State: Empty, Reason: "no event matches the selected sources and filter", Sequence: described}
	case len(window) == 0:
		return SequenceResult{State: Empty, Reason: "this window begins past the last event of this sequence", Sequence: described}
	}
	return SequenceResult{State: Completed, Sequence: described}
}

// laneOrder is the order of a case's sources in the timeline: the order each
// first appears in the evidence, then every declared source that holds no
// occurrence, in the order the case declares them.
func laneOrder(opened *bundle.Bundle) map[string]int {
	order := make(map[string]int, len(opened.Manifest.Sources))
	declared := make(map[string]bool, len(opened.Manifest.Sources))
	for _, source := range opened.Manifest.Sources {
		declared[source.ID] = true
	}
	for _, event := range opened.Events {
		if _, seen := order[event.SourceID]; !seen && declared[event.SourceID] {
			order[event.SourceID] = len(order)
		}
	}
	for _, source := range opened.Manifest.Sources {
		if _, seen := order[source.ID]; !seen {
			order[source.ID] = len(order)
		}
	}
	return order
}

// lane is the position of source among the lanes; an occurrence of a source
// the case does not declare sorts after every lane.
func lane(order map[string]int, source string) int {
	if index, known := order[source]; known {
		return index
	}
	return len(order)
}

// placed places every event under one time basis. Events share one time axis
// only when one recorded session observed them all: otherwise each source's
// events are ordered on that source's own clock, source after source in lane
// order, and nothing aligns one source's instants against another's. Equal
// instants keep the order their source recorded them in. Every event the
// basis places nowhere follows in an untimed section, in source order, and is
// never interleaved among the timed ones.
func placed(opened *bundle.Bundle, gaps map[string][]Gap, basis TimeBasis, order map[string]int) []SequenceEvent {
	shared := basis == ObservedBasis && opened.Manifest.Provenance.SessionID != ""
	all := make([]SequenceEvent, 0, len(opened.Events))
	for _, event := range opened.Events {
		described := sequenceEvent(opened, event, gaps)
		described.SourceSequence = event.Sequence
		described.Ordering = UnknownOrder
		switch basis {
		case ObservedBasis:
			if event.ObservedAt != nil {
				at := *event.ObservedAt
				described.At, described.Ordering, described.Clock = &at, ObservedOrder, string(SourceClock)+":"+event.SourceID
				if shared {
					described.Clock = string(SessionClock)
				}
			}
		case MessageBasis:
			if at, ok := sequenceanalysis.DeclaredInstant(opened, event); ok {
				described.At, described.Ordering, described.Clock = &at, MessageOrder, string(SenderClock)+":"+event.SourceID
			}
		case SourceBasis:
			described.Ordering = SourceSequenceOrder
		}
		all = append(all, described)
	}
	slices.SortStableFunc(all, func(x, y SequenceEvent) int {
		if (x.At == nil) != (y.At == nil) {
			if x.At == nil {
				return 1
			}
			return -1
		}
		byLane := cmp.Compare(lane(order, x.SourceID), lane(order, y.SourceID))
		bySequence := cmp.Compare(x.SourceSequence, y.SourceSequence)
		switch {
		case x.At == nil:
			return cmp.Or(byLane, bySequence)
		case shared:
			return cmp.Or(x.At.Compare(*y.At), byLane, bySequence)
		}
		return cmp.Or(byLane, x.At.Compare(*y.At), bySequence)
	})
	for i := range all {
		all[i].Position = i + 1
	}
	return all
}

// clocksOf reports every clock that placed at least one event, in lane order,
// with the sources whose events it placed.
func clocksOf(all []SequenceEvent, order map[string]int) []Clock {
	clocks := []Clock{}
	for _, event := range all {
		if event.Clock == "" {
			continue
		}
		at := slices.IndexFunc(clocks, func(clock Clock) bool { return clock.ID == event.Clock })
		if at < 0 {
			kind, _, _ := strings.Cut(event.Clock, ":")
			clocks = append(clocks, Clock{ID: event.Clock, Kind: ClockKind(kind), Sources: []string{}})
			at = len(clocks) - 1
		}
		if !slices.Contains(clocks[at].Sources, event.SourceID) {
			clocks[at].Sources = append(clocks[at].Sources, event.SourceID)
		}
	}
	for i := range clocks {
		slices.SortFunc(clocks[i].Sources, func(x, y string) int { return cmp.Compare(lane(order, x), lane(order, y)) })
	}
	slices.SortStableFunc(clocks, func(x, y Clock) int { return cmp.Compare(lane(order, x.Sources[0]), lane(order, y.Sources[0])) })
	return clocks
}

// problemsOf counts the whole case's actual problems and names the events
// each takes in. An acknowledgement matching nothing or more than one message
// and a rule's collision are unresolved links; an unacknowledged message and
// an absence a coverage declaration found are gaps, counted once per
// occurrence however many readings found it.
func problemsOf(opened *bundle.Bundle, report *correlate.Report, analysis *sequenceanalysis.Report) (map[string]bool, map[string]bool, TimelineProblems) {
	unresolved, absent := map[string]bool{}, map[string]bool{}
	var problems TimelineProblems
	for _, link := range opened.Correlations {
		switch link.Kind {
		case bundle.UnmatchedACK, bundle.AmbiguousACK:
			problems.UnresolvedLinks++
			unresolved[link.ACKID] = true
			for _, occurrence := range link.MessageIDs {
				unresolved[occurrence] = true
			}
		case bundle.Unacknowledged:
			for _, occurrence := range link.MessageIDs {
				absent[occurrence] = true
			}
		}
	}
	if report != nil {
		for _, collision := range report.Collisions {
			problems.UnresolvedLinks++
			for _, reference := range collision.Occurrences {
				unresolved[reference.Occurrence] = true
			}
			if collision.Declaring != nil {
				unresolved[collision.Declaring.Occurrence] = true
			}
		}
	}
	if analysis != nil {
		for _, finding := range analysis.Findings {
			if finding.Kind == sequenceanalysis.MissingACK || finding.Kind == sequenceanalysis.UnobservedDownstreamOutput {
				absent[finding.Occurrence] = true
			}
		}
	}
	problems.Gaps = len(absent)
	return unresolved, absent, problems
}

// relationsOf lists every relationship an event of the window takes part in:
// the acknowledgements the evidence itself matched or left ambiguous, the
// links and collisions of the applied link rules under the applied review,
// and the links an analyst added. Nothing here compares a key: each reading
// is its engine's own, and an endpoint outside the window is only marked so.
func relationsOf(opened *bundle.Bundle, report *correlate.Report, view *correlate.ReviewedView, window []SequenceEvent, rulesName string) []Relation {
	shown := make(map[string]bool, len(window))
	for _, event := range window {
		shown[event.Occurrence] = true
	}
	sources := make(map[string]string, len(opened.Events))
	for _, event := range opened.Events {
		sources[event.ID] = event.SourceID
	}
	relations := []Relation{}
	// Under no link rules, the review applied is of the recorded links: each
	// takes its status from it.
	statuses := map[string]correlate.ReviewStatus{}
	if report == nil && view != nil {
		for _, link := range view.Links {
			statuses[link.ID] = link.Status
		}
	}
	add := func(relation Relation, named []string) {
		if !slices.ContainsFunc(named, func(occurrence string) bool { return shown[occurrence] }) {
			return
		}
		relation.Occurrences = len(named)
		relation.BasisName = rulesName
		switch relation.Basis {
		case RecordedRelation:
			relation.BasisName = "Recorded"
		case ReviewedRelation:
			relation.BasisName = "Reviewed"
		}
		relation.Endpoints = make([]RelationEnd, 0, min(len(named), MaxRelatedOccurrences))
		for _, occurrence := range named[:min(len(named), MaxRelatedOccurrences)] {
			relation.Endpoints = append(relation.Endpoints, RelationEnd{Occurrence: occurrence, SourceID: sources[occurrence], InWindow: shown[occurrence]})
		}
		relations = append(relations, relation)
	}
	for i, link := range opened.Correlations {
		if link.Kind != bundle.Matched && link.Kind != bundle.AmbiguousACK {
			continue
		}
		relation := Relation{ID: "recorded-" + strconv.Itoa(i+1), Basis: RecordedRelation, Reason: string(link.Kind), Ambiguous: link.Kind == bundle.AmbiguousACK}
		if report == nil && !relation.Ambiguous {
			relation.Status = statuses[relation.ID]
		}
		named := slices.Clone(link.MessageIDs)
		if link.Kind == bundle.Matched {
			relation.Linkage = correlate.Observed
			named = append(named, link.ACKID)
		} else {
			named = append([]string{link.ACKID}, named...)
		}
		add(relation, named)
	}
	operators := map[string]correlate.Operator{}
	if report != nil {
		for _, link := range report.Links {
			operators[link.ID] = link.Operator
		}
	}
	if view != nil {
		for _, link := range view.Links {
			if report == nil && link.Linkage != "manual" {
				continue
			}
			relation := Relation{ID: link.ID, Basis: RuleRelation, Rule: link.Rule, Operator: operators[link.ID], Linkage: correlate.Linkage(link.Linkage), Status: link.Status}
			if link.Linkage == "manual" {
				relation.Basis, relation.Linkage = ReviewedRelation, ""
			}
			add(relation, occurrenceIDs(link.Occurrences))
		}
	}
	if report == nil {
		return relations
	}
	for i, collision := range report.Collisions {
		named := occurrenceIDs(collision.Occurrences)
		if collision.Declaring != nil && !slices.Contains(named, collision.Declaring.Occurrence) {
			named = append([]string{collision.Declaring.Occurrence}, named...)
		}
		add(Relation{ID: "collision-" + strconv.Itoa(i+1), Basis: RuleRelation, Rule: collision.Rule, Operator: collision.Operator,
			Reason: collision.Reason, Ambiguous: true}, named)
	}
	return relations
}

// header is the message type and trigger one occurrence declares.
type header struct{ code, trigger string }

// readHeaders reads every parsed occurrence of the case once: its message
// type and trigger, and the field selectors it holds a present
// value at. An occurrence the case preserved unparsed contributes nothing.
func readHeaders(opened *bundle.Bundle) (map[string]header, []string) {
	code, _ := hl7.ParseSelector("MSH-9.1")
	trigger, _ := hl7.ParseSelector("MSH-9.2")
	headers := make(map[string]header, len(opened.Events))
	present := map[hl7.Parts]bool{}
	for _, event := range opened.Events {
		if event.ParseError != "" || event.Kind == bundle.Unparsed {
			continue
		}
		document, err := opened.Document(event)
		if err != nil {
			continue
		}
		headers[event.ID] = header{typePart(document, 0, code), typePart(document, 0, trigger)}
		for _, segment := range document.Messages[0].Segments {
			for _, field := range segment.Fields {
				if field.State != hl7.Present {
					continue
				}
				if _, err := hl7.NewSelector(hl7.Parts{Segment: segment.ID, Occurrence: 1, Field: field.Number, Repetition: 1}); err == nil {
					present[hl7.Parts{Segment: segment.ID, Field: field.Number}] = true
				}
			}
		}
	}
	positions := slices.Collect(maps.Keys(present))
	slices.SortFunc(positions, func(x, y hl7.Parts) int {
		return cmp.Or(cmp.Compare(x.Segment, y.Segment), cmp.Compare(x.Field, y.Field))
	})
	fields := make([]string, 0, min(len(positions), MaxSequenceFields))
	for _, position := range positions[:min(len(positions), MaxSequenceFields)] {
		fields = append(fields, position.Segment+"-"+strconv.Itoa(position.Field))
	}
	return headers, fields
}

// counted reports one lane per declared source, in lane order and whether or
// not the source holds an occurrence, together with the counts of the whole
// case. Both come out of one traversal, so a lane and the summary beside it
// can never be tallied from two readings of one event. Ordered counts the
// events the active basis placed in time.
func counted(opened *bundle.Bundle, all []SequenceEvent, report *correlate.Report, order map[string]int) ([]Lane, SequenceSummary) {
	lanes := make([]Lane, len(order))
	for source, index := range order {
		lanes[index] = Lane{SourceID: source}
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
		if index, known := order[described.SourceID]; known {
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
		if described.At == nil {
			summary.Unordered, lane.Unordered = summary.Unordered+1, lane.Unordered+1
			continue
		}
		summary.Ordered, lane.Ordered = summary.Ordered+1, lane.Ordered+1
		if lane.Earliest == nil || described.At.Before(*lane.Earliest) {
			lane.Earliest = described.At
		}
		if lane.Latest == nil || described.At.After(*lane.Latest) {
			lane.Latest = described.At
		}
	}
	return lanes, summary
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
