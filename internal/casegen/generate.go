package casegen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/scenario"
)

// RecordSchema is the completion record a written generation installs.
const RecordSchema = "readmit-case-generation-record/v1"

// Options are a generation's context that is not material: where the request
// came from and who hears progress. Neither changes a byte generated.
type Options struct {
	// Source is the saved scenario revision the request was generated from,
	// recorded as ancestry.
	Source *Source
	// Progress hears each case as it is generated, validated and written.
	Progress func(Progress)
}

// Source names a saved scenario by its catalog item and revision.
type Source struct {
	Item     string `json:"item"`
	Revision string `json:"revision"`
}

// Progress is how far a generation has come. It carries counts only.
type Progress struct {
	Stage    string `json:"stage"`
	Cases    int    `json:"cases"`
	Done     int    `json:"done"`
	Messages int    `json:"messages"`
}

// Progress stages.
const (
	Encoding = "encoding"
	Writing  = "writing"
)

// Record is the generation record: every input, the ancestry that identifies
// them, each event's support, and for every case its step-to-message mapping,
// phases, variant ledger and evaluation.
type Record struct {
	Schema           string    `json:"schema"`
	State            string    `json:"state"`
	GeneratorVersion string    `json:"generator_version"`
	ContentIdentity  string    `json:"content_identity"`
	Request          Request   `json:"request"`
	Ancestry         Ancestry  `json:"ancestry"`
	Support          []Support `json:"support"`
	Cases            []Case    `json:"cases"`
}

// Ancestry is every material input by digest. Changing one input changes
// exactly its member here; nothing else about the machine, the clock or the
// path a generation was written to is among them.
type Ancestry struct {
	Request     string        `json:"request_sha256"`
	Scenario    ScenarioInput `json:"scenario"`
	Source      *Source       `json:"source,omitzero"`
	Profile     Pin           `json:"profile"`
	Pack        Pin           `json:"pack"`
	HL7Version  string        `json:"hl7_version"`
	Family      string        `json:"family"`
	Generator   string        `json:"generator_version"`
	Seed        uint64        `json:"seed"`
	BaseTime    string        `json:"base_time"`
	Wire        string        `json:"wire_sha256"`
	Bindings    string        `json:"bindings_sha256"`
	Rows        []Part        `json:"rows"`
	Variants    []Part        `json:"variants"`
	DerivedFrom string        `json:"derived_from,omitzero"`
}

// ScenarioInput is the embedded scenario's identity and digest.
type ScenarioInput struct {
	Schema    string `json:"schema"`
	ID        string `json:"id"`
	Version   string `json:"version"`
	Lifecycle string `json:"lifecycle"`
	SHA256    string `json:"sha256"`
}

// Part is one row's or variant's digest.
type Part struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

// Case is one row under one variant: an immutable case bundle, the messages
// it holds in arrival order, the phase each belongs to and what the variant
// changed.
type Case struct {
	Row         string       `json:"row"`
	Variant     string       `json:"variant"`
	Polarity    string       `json:"polarity"`
	Entry       string       `json:"entry"`
	Identity    string       `json:"identity"`
	Occurrences []Occurrence `json:"occurrences"`
	Phases      []Phase      `json:"phases"`
	Ledger      []Applied    `json:"ledger"`
	Validation  Validation   `json:"validation"`
}

// Occurrence is one message of a case, in arrival order: the step it was
// generated for, its intended arrival after the base time, the transport delay
// its variant declared before it is sent, whether it retransmits the previous
// one, the case bundle event and payload holding its bytes, and the identities
// it carries. The delay is execution timing, never a byte of the message.
type Occurrence struct {
	Ordinal   int           `json:"ordinal"`
	Step      string        `json:"step"`
	Event     string        `json:"event"`
	Structure string        `json:"structure"`
	After     string        `json:"after"`
	Delay     string        `json:"delay"`
	Duplicate bool          `json:"duplicate"`
	ControlID string        `json:"control_id"`
	Charset   string        `json:"charset"`
	SHA256    string        `json:"sha256"`
	Bytes     int           `json:"bytes"`
	CaseEvent string        `json:"case_event"`
	Payload   string        `json:"payload"`
	Keys      []BusinessKey `json:"business_keys"`
}

// Phase is one designed step's boundary: its occurrences, sent together and
// assertable before the next phase runs. An omitted step has no phase.
type Phase struct {
	ID          string `json:"id"`
	Event       string `json:"event"`
	Expect      string `json:"expect"`
	Occurrences []int  `json:"occurrences"`
}

// Applied is one declared mutation and the occurrences it changed or placed.
type Applied struct {
	Mutation    Mutation `json:"mutation"`
	Occurrences []int    `json:"occurrences"`
}

// Validation is the profile evaluator's report on a case's messages. A
// positive case is evaluated always and fails nothing outside workflow
// findings; a negative case is evaluated when every message parses, and its
// report is retained whatever it says.
type Validation struct {
	Evaluated bool                `json:"evaluated"`
	Reason    string              `json:"reason,omitzero"`
	Verdict   string              `json:"verdict,omitzero"`
	Report    *profileeval.Report `json:"report,omitzero"`
}

// Generation is one request's cases in memory, ready to write.
type Generation struct {
	Record    Record
	scenario  scenario.Workflow
	messages  [][][]byte
	profileID string
}

// Messages returns the bytes of each occurrence of case i, in arrival order.
func (g *Generation) Messages(i int) [][]byte {
	out := make([][]byte, len(g.messages[i]))
	for k, m := range g.messages[i] {
		out[k] = bytes.Clone(m)
	}
	return out
}

// Generate produces every case of the request in memory, writing nothing. It
// is a pure function of the three documents: the same bytes generate the same
// cases on any machine. An event the selected profile and pack do not support
// refuses the whole generation with an *UnsupportedError; cancellation is
// observed between messages.
func Generate(ctx context.Context, request, profile, pack []byte, options Options) (*Generation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, w, err := Decode(request)
	if err != nil {
		return nil, err
	}
	t, err := open(r, w.Profile, profile, pack)
	if err != nil {
		return nil, err
	}
	support := t.supportOf(w)
	rules := map[scenario.Event]*profileeval.MessageRule{}
	supports := map[scenario.Event]Support{}
	for _, s := range support {
		if s.Status != Supported {
			return nil, &UnsupportedError{Support: support}
		}
		_, rule := t.support(scenario.Event(s.Event))
		rules[scenario.Event(s.Event)], supports[scenario.Event(s.Event)] = rule, s
	}
	m, err := newModel(r, w)
	if err != nil {
		return nil, err
	}
	g := &Generation{scenario: w, profileID: r.Profile.ID + "+" + r.Profile.Version}
	g.Record = Record{Schema: RecordSchema, State: "complete", GeneratorVersion: Version, Request: r, Support: support, Cases: []Case{}, Ancestry: ancestry(r, w, t, options.Source)}
	total, done, messages, size := len(r.Rows)*len(r.Variants), 0, 0, 0
	report := func(stage string) {
		if options.Progress != nil {
			options.Progress(Progress{Stage: stage, Cases: total, Done: done, Messages: messages})
		}
	}
	report(Encoding)
	for _, row := range r.Rows {
		facts := m.facts(row)
		for _, v := range r.Variants {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			c, bodies, err := m.generateCase(ctx, t, rules, supports, row, facts, v)
			if err != nil {
				return nil, err
			}
			for _, b := range bodies {
				size += len(b)
			}
			if size > maxOutputBytes {
				return nil, errors.New("the generated cases exceed 16 MiB")
			}
			g.Record.Cases = append(g.Record.Cases, c)
			g.messages = append(g.messages, bodies)
			done++
			messages += len(bodies)
			report(Encoding)
		}
	}
	g.Record.ContentIdentity = contentIdentity(g.Record)
	return g, nil
}

// contentIdentity is the digest of everything material: the ancestry, the
// saved scenario revision included, and each case's messages, phases and
// ledger. Where the cases were written and the bundle identities their writing
// gave are not part of it.
func contentIdentity(r Record) string {
	type material struct {
		Ancestry Ancestry
		Cases    []Case
	}
	cases := make([]Case, len(r.Cases))
	for i, c := range r.Cases {
		c.Entry, c.Identity = "", ""
		cases[i] = c
	}
	return digest(mustCanonical(material{Ancestry: r.Ancestry, Cases: cases}))
}

func ancestry(r Request, w scenario.Workflow, t *target, source *Source) Ancestry {
	a := Ancestry{Request: digest(mustCanonical(r)), Scenario: ScenarioInput{Schema: w.Schema, ID: w.Scenario.Scenario.ID, Version: w.Scenario.Scenario.Version, Lifecycle: string(w.Profile), SHA256: digest(r.Scenario)},
		Source: source, Profile: r.Profile, Pack: r.Pack, HL7Version: t.version, Family: t.family, Generator: Version, Seed: r.Seed, BaseTime: w.BaseTime.UTC().Format(time.RFC3339),
		Wire: digest(mustCanonical(r.Wire)), Bindings: digest(mustCanonical(r.Bindings)), DerivedFrom: r.DerivedFrom, Rows: []Part{}, Variants: []Part{}}
	for _, row := range r.Rows {
		a.Rows = append(a.Rows, Part{ID: row.ID, SHA256: digest(mustCanonical(row))})
	}
	for _, v := range r.Variants {
		a.Variants = append(a.Variants, Part{ID: v.ID, SHA256: digest(mustCanonical(v))})
	}
	return a
}

// pending is one authored step's message before arrival order is decided.
type pending struct {
	built
	event   scenario.Event
	expect  scenario.Expectation
	data    []byte
	arrival time.Duration
	order   int
}

func (m *model) generateCase(ctx context.Context, t *target, rules map[scenario.Event]*profileeval.MessageRule, supports map[scenario.Event]Support, row Row, facts map[string]stepFacts, v Variant) (Case, [][]byte, error) {
	charsets, offsets, delays := map[string]string{}, map[string]string{}, map[string]time.Duration{}
	duplicates, omitted, moves := map[string]bool{}, map[string]bool{}, map[string]string{}
	changes, fields := map[string]alteration{}, map[string][]Mutation{}
	for _, mu := range v.Mutations {
		change := changes[mu.Step]
		switch mu.Op {
		case CharsetOp:
			charsets[mu.Step] = mu.Charset
		case OffsetOp:
			offsets[mu.Step] = mu.Offset
		case DelayOp:
			d, _ := delay(mu.After)
			delays[mu.Step] = d
		case DuplicateOp:
			duplicates[mu.Step] = true
		case OmitOp:
			omitted[mu.Step] = true
		case MoveOp:
			moves[mu.Step] = mu.Before
		case IdentifierOp:
			if change.identifiers == nil {
				change.identifiers = map[string]string{}
			}
			change.identifiers[mu.Subject] = mu.Value
		case NamespaceOp:
			if change.namespaces == nil {
				change.namespaces = map[string]string{}
			}
			change.namespaces[mu.Subject] = mu.Value
		case FieldOp:
			fields[mu.Step] = append(fields[mu.Step], mu)
		case UnlinkOp:
			change.unlink = true
		}
		changes[mu.Step] = change
	}
	steps := []*pending{}
	for i, step := range m.w.Steps {
		if err := ctx.Err(); err != nil {
			return Case{}, nil, err
		}
		charset := row.Charset
		if c, ok := charsets[step.ID]; ok {
			charset = c
		}
		offset := m.r.Wire.Offset
		if o, ok := offsets[step.ID]; ok {
			offset = o
		}
		b, err := m.message(t, rules[step.Event], supports[step.Event], step, row, facts[step.ID], charset, offset, changes[step.ID])
		if err != nil {
			return Case{}, nil, m.unsupported(t, step.Event, err.Error())
		}
		for _, fm := range fields[step.ID] {
			if err := applyField(rules[step.Event], b.segments, fm); err != nil {
				return Case{}, nil, err
			}
		}
		data, err := transcode(m.delimiter.encode(b.segments), charset)
		if err != nil {
			return Case{}, nil, err
		}
		after, _ := time.ParseDuration(step.After)
		steps = append(steps, &pending{built: b, event: step.Event, expect: step.Expect, data: data, arrival: after + delays[step.ID], order: i})
	}
	// Intended arrival alone decides order, with authored order breaking ties.
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].arrival < steps[j].arrival })
	// A move places its step immediately before another, arriving with it.
	for _, step := range m.w.Steps {
		before, ok := moves[step.ID]
		if !ok {
			continue
		}
		from := slices.IndexFunc(steps, func(p *pending) bool { return p.step.ID == step.ID })
		moved := steps[from]
		steps = slices.Delete(steps, from, from+1)
		to := slices.IndexFunc(steps, func(p *pending) bool { return p.step.ID == before })
		moved.arrival = steps[to].arrival
		steps = slices.Insert(steps, to, moved)
	}
	c := Case{Row: row.ID, Variant: v.ID, Polarity: v.Polarity, Occurrences: []Occurrence{}, Phases: []Phase{}, Ledger: []Applied{}}
	bodies := [][]byte{}
	placed := map[string][]int{}
	for _, p := range steps {
		if omitted[p.step.ID] {
			continue
		}
		copies := 1
		if duplicates[p.step.ID] {
			copies = 2
		}
		phase := Phase{ID: p.step.ID, Event: string(p.event), Expect: string(p.expect), Occurrences: []int{}}
		for k := 0; k < copies; k++ {
			ordinal := len(c.Occurrences) + 1
			source := fmt.Sprintf("s%04d", ordinal)
			c.Occurrences = append(c.Occurrences, Occurrence{Ordinal: ordinal, Step: p.step.ID, Event: string(p.event), Structure: supports[p.event].Structure,
				After: p.arrival.String(), Delay: delays[p.step.ID].String(), Duplicate: k > 0, ControlID: p.control, Charset: p.charset, SHA256: digest(p.data), Bytes: len(p.data),
				CaseEvent: source + "-e000001", Payload: "payloads/" + source + "-e000001.bin", Keys: p.keys})
			bodies = append(bodies, p.data)
			phase.Occurrences = append(phase.Occurrences, ordinal)
			placed[p.step.ID] = append(placed[p.step.ID], ordinal)
		}
		c.Phases = append(c.Phases, phase)
	}
	if len(bodies) > maxOccurrences {
		return Case{}, nil, errors.New("a case holds at most 128 messages")
	}
	if len(bodies) == 0 {
		return Case{}, nil, errors.New("a variant omits every step; a case holds at least one message")
	}
	for _, mu := range v.Mutations {
		applied := Applied{Mutation: mu, Occurrences: append([]int{}, placed[mu.Step]...)}
		c.Ledger = append(c.Ledger, applied)
	}
	validation, err := m.validate(ctx, t, c, bodies, steps, v.Polarity)
	if err != nil {
		return Case{}, nil, err
	}
	c.Validation = validation
	return c, bodies, nil
}

// unsupported refuses a generation because one event cannot be written under
// the selected profile, naming why.
func (m *model) unsupported(t *target, event scenario.Event, reason string) error {
	support := t.supportOf(m.w)
	for i := range support {
		if support[i].Event == string(event) {
			support[i].Status, support[i].Structure, support[i].Reason = Unsupported, "", reason
		}
	}
	return &UnsupportedError{Support: support}
}

// validate reads each message back through the lossless reader and evaluates
// the case under the pinned profile and pack. A positive case whose message
// does not read back exactly, or that fails any requirement besides a
// workflow transition, refuses its event as unsupported under that profile:
// it is never repaired and never emitted as positive.
func (m *model) validate(ctx context.Context, t *target, c Case, bodies [][]byte, steps []*pending, polarity string) (Validation, error) {
	byStep := map[string]*pending{}
	for _, p := range steps {
		byStep[p.step.ID] = p
	}
	parsed := true
	for i, body := range bodies {
		doc, err := hl7.Parse(body, hl7.Options{Format: hl7.Raw, Terminator: hl7.CR})
		if err != nil || len(doc.Messages) != 1 {
			if polarity == Positive {
				return Validation{}, m.unsupported(t, scenario.Event(c.Occurrences[i].Event), "the generated message does not read back as one message")
			}
			parsed = false
			continue
		}
		if polarity == Positive {
			p := byStep[c.Occurrences[i].Step]
			if err := readsBack(doc, p.segments, p.charset); err != nil {
				return Validation{}, m.unsupported(t, p.event, "the generated message does not read back: "+err.Error())
			}
		}
	}
	if !parsed {
		return Validation{Reason: "an occurrence is not one losslessly parsed message"}, nil
	}
	inputs := make([]profileeval.Occurrence, len(bodies))
	for i, body := range bodies {
		inputs[i] = profileeval.Occurrence{ID: fmt.Sprintf("o%03d", i+1), Bytes: body}
	}
	report, err := profileeval.Evaluate(ctx, t.profile, t.pack, inputs, profileeval.Options{})
	if err != nil {
		if ctx.Err() != nil {
			return Validation{}, ctx.Err()
		}
		if polarity == Positive {
			return Validation{}, err
		}
		return Validation{Reason: "the profile evaluator refused the case: " + err.Error()}, nil
	}
	if polarity == Positive {
		failed := map[string][]string{}
		for _, f := range report.Findings {
			if f.Outcome != "fail" || f.Origin == "workflow" {
				continue
			}
			index, _ := strconv.Atoi(strings.TrimPrefix(f.Occurrence, "o"))
			event := c.Occurrences[index-1].Event
			if len(failed[event]) < 4 {
				failed[event] = append(failed[event], f.Rule+" at "+f.Selector)
			}
		}
		if len(failed) > 0 {
			support := t.supportOf(m.w)
			for i := range support {
				if reasons, ok := failed[support[i].Event]; ok {
					support[i].Status, support[i].Structure = Unsupported, ""
					support[i].Reason = "the generated message fails the profile: " + strings.Join(reasons, ", ")
				}
			}
			return Validation{}, &UnsupportedError{Support: support}
		}
	}
	return Validation{Evaluated: true, Verdict: report.Verdict, Report: &report}, nil
}

// readsBack holds a positive message to exactly what was meant: every written
// value selects with its state and decodes to its text in the declared
// character set, and every unwritten position selects as not present.
func readsBack(doc *hl7.Document, segments []*segment, charset string) error {
	occurrences := map[string]int{}
	for _, s := range segments {
		occurrences[s.id]++
		occurrence := occurrences[s.id]
		for position, f := range s.fields {
			position++
			if s.id == "MSH" && position < 3 {
				continue
			}
			base := hl7.Parts{Segment: s.id, Occurrence: occurrence, Field: position, Repetition: 1}
			if f == nil || f.state != hl7.Present {
				want := hl7.Empty
				if f != nil && f.state == hl7.Null {
					want = hl7.Null
				}
				selector, _ := hl7.NewSelector(base)
				got, err := doc.Select(0, selector)
				if err != nil || got.State != want && !(f == nil && got.State == hl7.Omitted) {
					return errors.New(selector.String() + " does not read back as " + string(want))
				}
				continue
			}
			for r, rep := range f.reps {
				for k, component := range rep {
					for sub, text := range component {
						parts := base
						parts.Repetition, parts.Component, parts.Subcomponent = r+1, k+1, sub+1
						selector, _ := hl7.NewSelector(parts)
						reading, err := doc.Read(0, selector, hl7.EnforceMSH18)
						want, _ := transcode(text, charset)
						if err != nil {
							return errors.New(selector.String() + " cannot be selected")
						}
						if text == "" {
							if reading.State == hl7.Present {
								return errors.New(selector.String() + " reads back as a value")
							}
							continue
						}
						if reading.State != hl7.Present || !bytes.Equal(reading.Decoded, want) {
							return errors.New(selector.String() + " does not read back as written")
						}
					}
				}
			}
		}
	}
	return nil
}

// fieldTarget reads a field mutation's selector: one field of one segment
// occurrence, never a repetition or a component.
func fieldTarget(s string) (hl7.Parts, error) {
	selector, err := hl7.ParseSelector(s)
	if err != nil {
		return hl7.Parts{}, errors.New("a field mutation names SEG-field or SEG[occurrence]-field")
	}
	if dash := strings.IndexByte(s, '-'); strings.ContainsAny(s[dash:], "[.") {
		return hl7.Parts{}, errors.New("a field mutation names a whole field, never a repetition or a component")
	}
	parts := selector.Parts()
	if parts.Segment == "MSH" && parts.Field < 3 {
		return hl7.Parts{}, errors.New("MSH-1 and MSH-2 declare the delimiters and are never mutated")
	}
	return parts, nil
}

// invalidValues are the declared invalid values a field mutation writes, by
// the data type the selected edition gives the field: a date/time with month
// 13, and a number that is not one. They are written literally, never escaped
// or repaired.
var invalidValues = map[string]string{"TS": "20261301000000", "DTM": "20261301000000", "DT": "20261301", "NM": "NaN", "SI": "NaN"}

// applyField writes one declared field state into a built message. absent
// clears the position (a trailing field disappears, an inner one is written
// empty), empty and null write those states, and invalid writes a value its
// edition's data type refuses.
func applyField(rule *profileeval.MessageRule, segments []*segment, m Mutation) error {
	parts, _ := fieldTarget(m.Selector)
	seen := 0
	for _, s := range segments {
		if s.id != parts.Segment {
			continue
		}
		seen++
		if seen != parts.Occurrence {
			continue
		}
		switch m.State {
		case AbsentState:
			s.clear(parts.Field)
		case EmptyState:
			s.set(parts.Field, &field{state: hl7.Empty})
		case NullState:
			s.set(parts.Field, &field{state: hl7.Null})
		case InvalidState:
			declared, ok := fieldRule(rule, s.id, parts.Field)
			value, typed := invalidValues[declared.DataType]
			if !ok || !typed {
				return errors.New("an invalid field mutation targets a date/time or numeric field of the selected edition")
			}
			s.set(parts.Field, &field{state: hl7.Present, literal: value})
		}
		return nil
	}
	return errors.New("a field mutation names a segment its step's message does not carry")
}
