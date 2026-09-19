package diagnose

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/hl7"
)

// grouped collects values under comparable keys and remembers the order their
// keys first appeared, so what a rule reports never depends on map iteration
// order.
type grouped[K comparable, V any] struct {
	values map[K][]V
	keys   []K
}

func newGrouped[K comparable, V any]() *grouped[K, V] {
	return &grouped[K, V]{values: make(map[K][]V)}
}

func (g *grouped[K, V]) add(key K, value V) {
	if _, seen := g.values[key]; !seen {
		g.keys = append(g.keys, key)
	}
	g.values[key] = append(g.values[key], value)
}

// statusCode is one code inside one declared vocabulary. Two vocabularies may
// spell the same letter and never mean the same thing, so nothing is keyed on a
// code alone.
type statusCode struct{ vocabulary, code string }

// statusLabels fixes the prose of every status a profile document may declare.
// The document decides which codes exist and which of them are final; it never
// supplies an explanation, and a document naming a code this package cannot
// explain is refused rather than reported with a bare token. These are concise
// readmit-authored explanations of the readmit fixture vocabulary, not a
// redistributed HL7 table.
var statusLabels = map[statusCode]string{
	{"order-status", "SC"}: "scheduled",
	{"order-status", "IP"}: "in process",
	{"order-status", "A"}:  "some results available",
	{"order-status", "HD"}: "on hold",
	{"order-status", "CM"}: "completed",
	{"order-status", "CA"}: "cancelled",
	{"order-status", "DC"}: "discontinued",
	{"result-status", "O"}: "order received, specimen not yet received",
	{"result-status", "I"}: "specimen received, procedure incomplete",
	{"result-status", "S"}: "procedure scheduled, no results available",
	{"result-status", "A"}: "some results available",
	{"result-status", "P"}: "preliminary",
	{"result-status", "R"}: "results stored, not verified",
	{"result-status", "C"}: "correction to results",
	{"result-status", "F"}: "final",
	{"result-status", "X"}: "no results available, order cancelled",
}

// outputKey is one identity reported by one occurrence, inside one configured
// namespace and one declared status vocabulary.
type outputKey struct {
	vocabulary string
	identity
}

// repetition is one identity reported in one status: everything that has to
// match before two occurrences are reporting the same output.
type repetition struct {
	outputKey
	code string
}

// reported is one occurrence's declaration that an identity stands in a status.
type reported struct {
	m      message
	decl   output
	key    outputKey
	status statusDefinition
}

// outputs evaluates the two rules that read declared outputs: the same identity
// and status reported by more than one occurrence, and one identity carrying
// more than one status the profile declares final.
//
// Both compare the set of declarations in the verified window. Neither orders
// occurrences: the capture supplies no business chronology, so a status
// sequence is never reconstructed and no status is called premature or late.
func (e *evaluator) outputs(messages []message) {
	if !e.rules[DuplicateOutput] && !e.rules[StatusProgression] {
		return
	}
	var records []reported
	for _, m := range messages {
		trigger, _ := e.profile.trigger(m.kind, m.trigger)
		if !trigger.Output.declared() {
			continue
		}
		if record, ok := e.reported(m, trigger.Output); ok {
			records = append(records, record)
		}
	}
	e.duplicateOutputs(records)
	e.statusProgression(records)
}

// reported decodes one output declaration. An identity that correlates with
// nothing, or a status the profile does not declare, is reported as unsupported
// and compared with nothing rather than treated as agreeing evidence.
func (e *evaluator) reported(m message, decl output) (reported, bool) {
	value, ok := e.text(m, decl.Identity)
	if !ok || value == "" {
		return reported{}, false
	}
	namespace, ok := e.authorityFor(m, decl.Authority, false)
	if !ok {
		return reported{}, false
	}
	v, supported := e.value(m, decl.Status)
	if !supported {
		return reported{}, false
	}
	if v.State != hl7.Present {
		e.unsupportedItem("missing_output_status", m.event.ID, decl.Status, "The occurrence declares no status for the identity it reports; its status was not compared.")
		return reported{}, false
	}
	code, ok := e.text(m, decl.Status)
	if !ok {
		return reported{}, false
	}
	status, declared := e.profile.status(decl.Vocabulary, code)
	if !declared {
		e.unsupportedItem("unsupported_status_value", m.event.ID, decl.Status, "The declared status is outside the "+decl.Vocabulary+" vocabulary of profile "+e.profile.Profile+"; it was not compared.")
		return reported{}, false
	}
	return reported{m: m, decl: decl, key: outputKey{decl.Vocabulary, identity{namespace, value}}, status: status}, true
}

// duplicateOutputs reports one identity reported in the same status by more than
// one occurrence. Two occurrences reporting different declared statuses are a
// progression, not a repetition, so the status is part of the key.
func (e *evaluator) duplicateOutputs(records []reported) {
	if !e.rules[DuplicateOutput] {
		return
	}
	groups := newGrouped[repetition, reported]()
	for _, record := range records {
		groups.add(repetition{record.key, record.status.Code}, record)
	}
	for _, key := range groups.keys {
		group := groups.values[key]
		if len(group) < 2 {
			continue
		}
		e.finding(DuplicateOutput, "observed_fact", fmt.Sprintf("%d captured occurrences report the same identity in status %s (%s) in the same configured namespace. This states that the output repeats in the capture; it does not establish that the reported content is identical, that any system received it twice, or that either occurrence is erroneous.", len(group), key.code, statusLabels[statusCode{key.vocabulary, key.code}]), e.findingWindow, e.outputEvidence(group)...)
	}
}

// statusProgression reports one identity carrying more than one status the
// profile declares final. Finality is what the named profile declares, so the
// same code may end an identity under one profile and not under another. This
// is a disagreement between occurrences, not a claim about which came first.
func (e *evaluator) statusProgression(records []reported) {
	if !e.rules[StatusProgression] {
		return
	}
	finals := newGrouped[outputKey, reported]()
	for _, record := range records {
		if record.status.Final {
			finals.add(record.key, record)
		}
	}
	for _, key := range finals.keys {
		group := finals.values[key]
		var codes []string
		for _, record := range group {
			if !slices.Contains(codes, record.status.Code) {
				codes = append(codes, record.status.Code)
			}
		}
		if len(codes) < 2 {
			continue
		}
		spelled := make([]string, 0, len(codes))
		for _, code := range codes {
			spelled = append(spelled, fmt.Sprintf("%s (%s)", code, statusLabels[statusCode{key.vocabulary, code}]))
		}
		e.finding(StatusProgression, "profile_violation", fmt.Sprintf("Profile %s declares at most one final %s for one identity; the captured occurrences declare %s for this identity in the same configured namespace. This reports a disagreement between occurrences, not which of them is correct and not the order they happened in.", e.profile.Profile, key.vocabulary, strings.Join(spelled, " and ")), e.findingWindow, e.outputEvidence(group)...)
	}
}

func (e *evaluator) outputEvidence(group []reported) []Evidence {
	refs := make([]Evidence, 0, len(group))
	for _, record := range group {
		for _, path := range record.decl.Evidence {
			refs = append(refs, record.m.evidence(path))
		}
	}
	return refs
}
