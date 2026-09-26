// Package scenariolibrary pins reusable generator plans separately from
// hand-authored expectations. Passing proves only the declared fixture checks.
package scenariolibrary

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"regexp"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/scenario"
	"github.com/bharm16/readmit/internal/scenariogen"
)

const MaxBytes = 4 << 20

type Library struct {
	Schema    string     `json:"schema"`
	Templates []Template `json:"templates"`
}
type Template struct {
	ID       string         `json:"id"`
	Version  string         `json:"version"`
	Profile  string         `json:"profile"`
	Coverage []string       `json:"coverage"`
	Plan     jsontext.Value `json:"plan"`
}
type Expectations struct {
	Schema          string    `json:"schema"`
	ID              string    `json:"id"`
	Version         string    `json:"version"`
	Template        string    `json:"template"`
	TemplateVersion string    `json:"template_version"`
	PlanSHA256      string    `json:"plan_sha256"`
	Provenance      string    `json:"provenance"`
	Lifecycle       []Outcome `json:"lifecycle"`
	Streams         []Stream  `json:"streams"`
}
type Outcome struct {
	Step    string `json:"step"`
	Outcome string `json:"outcome"`
	From    string `json:"from"`
	To      string `json:"to"`
}
type Stream struct {
	Row      string    `json:"row"`
	Variant  string    `json:"variant"`
	Messages []Message `json:"messages"`
}
type Message struct {
	Step      string  `json:"step"`
	After     string  `json:"after"`
	Duplicate bool    `json:"duplicate"`
	Fields    []Field `json:"fields"`
}
type Field struct {
	Selector string `json:"selector"`
	State    string `json:"state"`
	// Hex retains byte expectations even for non-UTF-8 encodings.
	Hex string `json:"hex"`
}
type Result struct {
	Streams int
	Fields  int
	Target  string
}

// strict requires every member, including false, empty arrays and empty strings.
// Called before each typed decode, including every nested row.
func strict(data []byte, target any, names ...string) error {
	var members map[string]jsontext.Value
	if err := json.Unmarshal(data, &members); err != nil || members == nil {
		return errors.New("expected an object")
	}
	for _, name := range names {
		value, ok := members[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("library documents require explicit non-null members")
		}
	}
	if err := json.Unmarshal(data, target, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid library document members")
	}
	return nil
}
func (v *Template) UnmarshalJSON(b []byte) error {
	type wire Template
	var w wire
	if err := strict(b, &w, "id", "version", "profile", "coverage", "plan"); err != nil {
		return err
	}
	*v = Template(w)
	return nil
}
func (v *Outcome) UnmarshalJSON(b []byte) error {
	type wire Outcome
	var w wire
	if err := strict(b, &w, "step", "outcome", "from", "to"); err != nil {
		return err
	}
	*v = Outcome(w)
	return nil
}
func (v *Stream) UnmarshalJSON(b []byte) error {
	type wire Stream
	var w wire
	if err := strict(b, &w, "row", "variant", "messages"); err != nil {
		return err
	}
	*v = Stream(w)
	return nil
}
func (v *Message) UnmarshalJSON(b []byte) error {
	type wire Message
	var w wire
	if err := strict(b, &w, "step", "after", "duplicate", "fields"); err != nil {
		return err
	}
	*v = Message(w)
	return nil
}
func (v *Field) UnmarshalJSON(b []byte) error {
	type wire Field
	var w wire
	if err := strict(b, &w, "selector", "state", "hex"); err != nil {
		return err
	}
	*v = Field(w)
	return nil
}

var name = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// PlanDigest seals the validated, deterministic typed plan. Both the template
// and the oracle retain their own versions; a changed plan cannot reuse a pin.
func PlanDigest(data []byte) (string, error) {
	p, err := scenariogen.Decode(data)
	if err != nil {
		return "", errors.New("invalid library generator plan")
	}
	return digestPlan(p)
}

func digestPlan(p scenariogen.Plan) (string, error) {
	encoded, err := json.Marshal(p, json.Deterministic(true))
	if err != nil {
		return "", errors.New("cannot encode library plan")
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

type selection struct {
	Template Template
	Timeline scenario.Timeline
	Digest   string
}

// Decode reads a readmit-scenario-library/v1 document on its own, as Check
// reads the library it is given: bounded, every member present and none
// unknown, and every template held to the library rules — its identity, its
// coverage tags, a valid plan and the profile that plan actually previews on.
// A library Decode accepts is one Check accepts; which template an oracle
// selects, and whether it pins that template's plan, is Check's to decide.
func Decode(data []byte) (Library, error) {
	var l Library
	if len(data) > MaxBytes {
		return Library{}, errors.New("library document exceeds 4 MiB")
	}
	if err := strict(data, &l, "schema", "templates"); err != nil {
		return Library{}, err
	}
	if l.Schema != "readmit-scenario-library/v1" {
		return Library{}, errors.New("unsupported library schema")
	}
	if _, err := validTemplates(l); err != nil {
		return Library{}, err
	}
	return l, nil
}

// validatedTemplate is one template the library rules accept, with its
// decoded plan and the lifecycle that plan previews.
type validatedTemplate struct {
	template Template
	plan     scenariogen.Plan
	timeline scenario.Timeline
}

// validTemplates holds every template of a library to the library rules, in
// order, and stops at the first it refuses.
func validTemplates(l Library) ([]validatedTemplate, error) {
	if len(l.Templates) < 1 || len(l.Templates) > 16 {
		return nil, errors.New("library requires 1 to 16 templates")
	}
	seen := map[string]bool{}
	accepted := make([]validatedTemplate, 0, len(l.Templates))
	for _, t := range l.Templates {
		key := t.ID + "/" + t.Version
		if !name.MatchString(t.ID) || !name.MatchString(t.Version) || seen[key] {
			return nil, errors.New("invalid or duplicate template identity")
		}
		seen[key] = true
		if len(t.Coverage) < 1 || len(t.Coverage) > 32 {
			return nil, errors.New("template requires bounded coverage tags")
		}
		tags := map[string]bool{}
		for _, tag := range t.Coverage {
			if !name.MatchString(tag) || tags[tag] {
				return nil, errors.New("invalid or duplicate coverage tag")
			}
			tags[tag] = true
		}
		p, err := scenariogen.Decode(t.Plan)
		if err != nil {
			return nil, errors.New("invalid library generator plan")
		}
		timeline, err := scenario.PreviewDocument(p.Template)
		if err != nil || string(timeline.Profile) != t.Profile {
			return nil, errors.New("template profile differs from plan")
		}
		accepted = append(accepted, validatedTemplate{template: t, plan: p, timeline: timeline})
	}
	return accepted, nil
}

func read(library, oracle []byte) (selection, Expectations, error) {
	var l Library
	var e Expectations
	fail := func(s string) (selection, Expectations, error) { return selection{}, Expectations{}, errors.New(s) }
	if len(library) > MaxBytes || len(oracle) > MaxBytes {
		return fail("library document exceeds 4 MiB")
	}
	if err := strict(library, &l, "schema", "templates"); err != nil {
		return fail(err.Error())
	}
	if err := strict(oracle, &e, "schema", "id", "version", "template", "template_version", "plan_sha256", "provenance", "lifecycle", "streams"); err != nil {
		return fail(err.Error())
	}
	if l.Schema != "readmit-scenario-library/v1" || e.Schema != "readmit-scenario-expectations/v1" {
		return fail("unsupported library schema")
	}
	if !name.MatchString(e.ID) || !name.MatchString(e.Version) || !name.MatchString(e.Template) || !name.MatchString(e.TemplateVersion) || len(e.Provenance) == 0 || len(e.Provenance) > 1024 {
		return fail("invalid expectation identity or provenance")
	}
	templates, err := validTemplates(l)
	if err != nil {
		return fail(err.Error())
	}
	var selected selection
	for _, t := range templates {
		if t.template.ID == e.Template && t.template.Version == e.TemplateVersion {
			digest, err := digestPlan(t.plan)
			if err != nil {
				return fail(err.Error())
			}
			selected = selection{Template: t.template, Timeline: t.timeline, Digest: digest}
		}
	}
	if selected.Template.ID == "" {
		return fail("expectations require an exact template version")
	}
	if selected.Digest != e.PlanSHA256 {
		return fail("expectations pin a different generator plan")
	}
	if len(e.Streams) < 1 || len(e.Streams) > 128 || len(e.Lifecycle) < 1 || len(e.Lifecycle) > 64 {
		return fail("expectation coverage is empty or exceeds limits")
	}
	for _, s := range e.Streams {
		if !name.MatchString(s.Row) || !name.MatchString(s.Variant) || len(s.Messages) < 1 || len(s.Messages) > 128 {
			return fail("invalid expected stream")
		}
		for _, m := range s.Messages {
			if len(m.Fields) < 1 || len(m.Fields) > 64 {
				return fail("each expected occurrence requires bounded field checks")
			}
			selectors := map[string]bool{}
			for _, f := range m.Fields {
				selector, err := hl7.ParseSelector(f.Selector)
				if err != nil || selectors[selector.String()] {
					return fail("invalid or duplicate expectation selector")
				}
				selectors[selector.String()] = true
				value, err := hex.DecodeString(f.Hex)
				if err != nil || len(value) > 1024 {
					return fail("invalid expected field bytes")
				}
				switch hl7.State(f.State) {
				case hl7.Present:
					if len(value) == 0 {
						return fail("present field requires bytes")
					}
				case hl7.Empty, hl7.Omitted:
					if len(value) != 0 {
						return fail("empty or omitted field cannot carry bytes")
					}
				case hl7.Null:
					if string(value) != `""` {
						return fail("null field requires explicit null bytes")
					}
				default:
					return fail("unsupported expected field state")
				}
			}
		}
	}
	return selected, e, nil
}

// Check regenerates the selected plan in memory and compares it to
// independently supplied literal expectations. It writes nothing anywhere —
// there is no scratch generation and no target evidence is read or created —
// so target acceptance always remains unverified.
func Check(ctx context.Context, library, oracle []byte) (Result, error) {
	var result Result
	if err := ctx.Err(); err != nil {
		return result, err
	}
	t, e, err := read(library, oracle)
	if err != nil {
		return result, err
	}
	timeline := t.Timeline
	if len(timeline.Steps) != len(e.Lifecycle) {
		return result, errors.New("lifecycle oracle must cover every authored step")
	}
	for i, actual := range timeline.Steps {
		expected := e.Lifecycle[i]
		outcome := "accepted"
		if actual.Reason != "" {
			outcome = "refused"
		}
		if actual.ID != expected.Step || outcome != expected.Outcome || string(actual.From) != expected.From || string(actual.To) != expected.To {
			return result, fmt.Errorf("lifecycle expectation mismatch at step %d", i+1)
		}
	}
	family, err := scenariogen.Produce(ctx, t.Template.Plan)
	if err != nil {
		return result, err
	}
	if len(family.Manifest.Streams) != len(e.Streams) {
		return result, errors.New("oracle must cover every generated stream")
	}
	for i, actual := range family.Manifest.Streams {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		expected := e.Streams[i]
		if actual.Row != expected.Row || actual.Variant != expected.Variant || len(actual.IntendedArrivals) != len(expected.Messages) {
			return Result{}, fmt.Errorf("stream coverage mismatch at stream %d", i+1)
		}
		doc, err := hl7.Parse(family.Stream(i), hl7.Options{Format: hl7.MLLP})
		if err != nil || len(doc.Messages) != len(expected.Messages) {
			return Result{}, errors.New("generated message count or syntax differs from oracle")
		}
		for j, m := range expected.Messages {
			arrival := actual.IntendedArrivals[j]
			if arrival.Step != m.Step || arrival.After != m.After || arrival.Duplicate != m.Duplicate {
				return Result{}, fmt.Errorf("arrival expectation mismatch at stream %d occurrence %d", i+1, j+1)
			}
			for _, f := range m.Fields {
				selector, _ := hl7.ParseSelector(f.Selector)
				value, err := doc.Select(j, selector)
				want, _ := hex.DecodeString(f.Hex)
				if err != nil || string(value.State) != f.State || !bytes.Equal(doc.Bytes(value.Span), want) {
					return Result{}, fmt.Errorf("field expectation mismatch at stream %d occurrence %d", i+1, j+1)
				}
				result.Fields++
			}
		}
		result.Streams++
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result.Target = "unverified"
	return result, nil
}
