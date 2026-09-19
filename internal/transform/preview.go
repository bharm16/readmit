package transform

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/profilepack"
)

// boundaryStatement is the honest boundary every preview carries, in the words
// a reader needs before acting on it.
const boundaryStatement = "A preview states what this plan would do to the sequence a replay sends. " +
	"It writes nothing, sends nothing and produces no derived evidence. A preserved relation means the " +
	"declared rules related those occurrences and the transformation kept them related; it does not " +
	"establish that they describe one encounter. A supported profile outcome is the pinned pack's own " +
	"declaration, not a conformance verdict, and unknown, untested and unsupported never pass."

// standard is the only delimiter declaration a position is rewritten under. An
// occurrence declaring other delimiters is refused rather than rewritten
// against an assumption, exactly as the reproducer editor refuses one.
var standard = hl7.Delimiters{Field: '|', Component: '^', Repetition: '~', Escape: '\\', Subcomponent: '&'}

// controlIDSelector is the position a control-id rename rewrites, and
// acknowledgedSelector the reference a matched acknowledgement declares about
// it. A rename that moves one without the other strands the acknowledgement,
// which is the relation this package exists to keep.
const (
	controlIDSelector     = "MSH-10"
	acknowledgedSelector  = "MSA-2"
	entryNameFormat       = "t%06d"
	surrogateValueFormat  = "READMIT%06d"
	timestampLayout       = "20060102150405"
	timestampOffsetLayout = timestampLayout + "-0700"
)

// Run reports what one plan means over one verified case under one declared set
// of correlation rules.
//
// It opens and verifies the case through the same reader `readmit timeline`
// runs, leaves it exactly as it found it, opens no network connection and adds
// no member to any existing contract. The rules are applied by
// [correlate.Run]: which occurrences are related is that package's answer, and
// this one only preserves it.
func Run(path string, plan Plan, rules correlate.Rules, pack *profilepack.Pack) (Preview, error) {
	if err := validatePlan(plan); err != nil {
		return Preview{}, err
	}
	source, err := bundle.Open(path)
	if err != nil {
		return Preview{}, errors.New("the case could not be verified as complete, unmodified evidence")
	}
	if source.Identity != plan.Case {
		return Preview{}, errors.New("this plan was authored against different evidence than the case it was applied to")
	}
	if len(source.Events) == 0 {
		return Preview{}, errors.New("a transformation needs a case holding at least one occurrence")
	}
	if len(source.Events) > MaxEntries {
		return Preview{}, errors.New("a transformation sequence holds at most 1024 entries")
	}
	report, err := correlate.Run(path, rules)
	if err != nil {
		return Preview{}, err
	}
	if report.CaseIdentity != source.Identity {
		return Preview{}, errors.New("the correlation report describes different evidence than the case it was read from")
	}
	if report.RulesSHA256 != plan.Rules {
		return Preview{}, errors.New("this plan was authored against different correlation rules than the ones it was applied with")
	}
	declared := profilepack.Pack{}
	if pack != nil {
		declared = *pack
	}
	if plan.Profile == (profilepack.Identity{}) {
		if pack != nil {
			return Preview{}, errors.New("this plan pins no profile pack; a preview validates against the pack a plan named or against none")
		}
	} else if pack == nil {
		return Preview{}, errors.New("this plan pins a profile pack that was not supplied")
	} else if err := declared.Satisfies(plan.Profile); err != nil {
		return Preview{}, err
	}
	e := newEngine(source, report, rules)
	for _, step := range plan.Steps {
		if err := e.apply(step); err != nil {
			return Preview{}, err
		}
	}
	return e.finish(plan, declared)
}

type engine struct {
	source     *bundle.Bundle
	report     correlate.Report
	events     map[string]*bundle.Event
	rules      map[string]correlate.Rule
	applied    map[string]bool
	docs       map[string]*hl7.Document
	raws       map[string][]byte
	sequence   []Entry
	next       int
	surrogates int
	renames    []string
	shift      time.Duration
	shifting   bool
	notes      []Unsupported
}

func newEngine(source *bundle.Bundle, report correlate.Report, rules correlate.Rules) *engine {
	e := &engine{
		source:  source,
		report:  report,
		events:  make(map[string]*bundle.Event, len(source.Events)),
		rules:   make(map[string]correlate.Rule, len(rules.Rules)),
		applied: make(map[string]bool, len(rules.Rules)),
	}
	for i := range source.Events {
		e.events[source.Events[i].ID] = &source.Events[i]
		e.next++
		e.sequence = append(e.sequence, Entry{
			ID:     fmt.Sprintf(entryNameFormat, e.next),
			Parent: source.Events[i].ID,
			Source: source.Events[i].SourceID,
		})
	}
	for _, rule := range rules.Rules {
		e.rules[rule.ID] = rule
	}
	for _, reported := range report.Rules {
		e.applied[reported.ID] = reported.Applied
	}
	return e
}

func (e *engine) apply(step Step) error {
	switch step.Operator {
	case DropOccurrence:
		at, err := e.indexOf(step.Entry)
		if err != nil {
			return err
		}
		e.sequence = slices.Delete(e.sequence, at, at+1)
	case DuplicateOccurrence:
		at, err := e.indexOf(step.Entry)
		if err != nil {
			return err
		}
		if len(e.sequence) >= MaxEntries {
			return errors.New("a transformation sequence holds at most 1024 entries")
		}
		e.next++
		copied := e.sequence[at]
		copied.ID, copied.Copy = fmt.Sprintf(entryNameFormat, e.next), true
		e.sequence = slices.Insert(e.sequence, at+1, copied)
	case ReorderOccurrence:
		at, err := e.indexOf(step.Entry)
		if err != nil {
			return err
		}
		if step.Position > len(e.sequence) {
			return errors.New("a reorder names a position beyond the end of the sequence")
		}
		moved := e.sequence[at]
		e.sequence = slices.Insert(slices.Delete(e.sequence, at, at+1), step.Position-1, moved)
	case RebaseIdentifiers:
		rule, declared := e.rules[step.Rule]
		if !declared {
			return errors.New("a rename names a rule the declared correlation rules do not hold")
		}
		if rule.Operator == correlate.Acknowledges {
			return errors.New("an acknowledges rule declares no value of its own; renaming the messages is what repairs the references to them")
		}
		if !e.applied[step.Rule] {
			return errors.New("a rename names a rule this case supplied no scope for, so it related nothing to preserve")
		}
		if slices.Contains(e.renames, step.Rule) {
			return errors.New("a plan renames each rule once")
		}
		e.renames = append(e.renames, step.Rule)
	case ShiftDates:
		if e.shifting {
			return errors.New("a plan shifts dates once, by one explicit duration")
		}
		shift, err := ParseShift(step.Shift)
		if err != nil {
			return err
		}
		e.shift, e.shifting = shift, true
	}
	return nil
}

func (e *engine) indexOf(entry string) (int, error) {
	at := slices.IndexFunc(e.sequence, func(item Entry) bool { return item.ID == entry })
	if at < 0 {
		return 0, errors.New("a plan step names an entry this sequence does not hold")
	}
	return at, nil
}

// placement is one position the transformation rewrites in one parent
// occurrence: where it is, what it was, and the bytes that replace it.
type placement struct {
	selector hl7.Selector
	span     hl7.Span
	state    hl7.State
	value    []byte
	operator string
	rule     string
	group    int
}

func (e *engine) note(item Unsupported) {
	if !slices.Contains(e.notes, item) {
		e.notes = append(e.notes, item)
	}
}

// retained reports the parent occurrences the sequence still holds, in evidence
// order. A rename decides nothing about an occurrence no entry names.
func (e *engine) retained() []string {
	retained := make([]string, 0, len(e.sequence))
	for i := range e.source.Events {
		id := e.source.Events[i].ID
		if slices.ContainsFunc(e.sequence, func(item Entry) bool { return item.Parent == id }) {
			retained = append(retained, id)
		}
	}
	return retained
}

// inScope reports whether one occurrence is inside the boundary a rule compares
// within. Nothing is renamed outside it: the operator declared what to relate,
// and that is exactly what a rename may touch.
func inScope(rule correlate.Rule, event *bundle.Event) bool {
	if rule.Scope == correlate.DeclaredScope {
		return slices.Contains(rule.Sources, event.SourceID)
	}
	return true
}

// prepare reads every retained occurrence once, the way its own case source
// declares it. An occurrence nothing decoded has no field tree: it is left in
// the sequence exactly as it is, and every question about it is reported rather
// than answered.
func (e *engine) prepare(retained []string) error {
	e.docs = make(map[string]*hl7.Document, len(retained))
	e.raws = make(map[string][]byte, len(retained))
	for _, id := range retained {
		raw, err := e.source.Raw(id)
		if err != nil {
			return errors.New("the bytes of a retained occurrence are unavailable")
		}
		e.raws[id] = raw
		if e.events[id].Kind == bundle.Unparsed {
			continue
		}
		doc, err := hl7.Parse(raw, e.options(e.events[id]))
		if err != nil || len(doc.Messages) == 0 {
			continue
		}
		e.docs[id] = doc
	}
	return nil
}

func (e *engine) options(event *bundle.Event) hl7.Options {
	for _, source := range e.source.Manifest.Sources {
		if source.ID == event.SourceID {
			return hl7.Options{Format: source.Format, Terminator: event.Terminator}
		}
	}
	return hl7.Options{Terminator: event.Terminator}
}
