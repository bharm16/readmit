package profileeval

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/localprofile"
)

type evaluator struct {
	ctx       context.Context
	remaining int
	readBytes int
	err       error
	report    Report
	profile   ProfileV2
	pack      PackV2
	doc       *hl7.Document
	id        string
	overflow  bool
}

func Evaluate(ctx context.Context, profileBytes, packBytes []byte, inputs []Occurrence, options Options) (Report, error) {
	p, err := decodeProfile(profileBytes)
	if err != nil {
		return Report{}, err
	}
	pack, err := decodePack(packBytes)
	if err != nil {
		return Report{}, err
	}
	if p.Definition.Base.Pack != pack.Metadata.Identity {
		return Report{}, errors.New("profile pack pin mismatch")
	}
	if len(inputs) < 1 || len(inputs) > 1024 {
		return Report{}, invalid
	}
	canonical, err := profileCanonical(p)
	if err != nil {
		return Report{}, err
	}
	e := evaluator{ctx: ctx, remaining: 1000000, profile: p, pack: pack, report: Report{Schema: Schema, Operator: OperatorVersion, Profile: Pin{Schema: p.Schema, ID: p.Definition.Identity.ID, Version: p.Definition.Identity.Version, SHA256: digest(canonical)}, Pack: Pin{Schema: pack.Schema, ID: pack.Metadata.Identity.ID, Version: pack.Metadata.Identity.Version, SHA256: digest(packBytes)}, Inputs: map[string]string{}, CompleteCapture: options.CompleteCapture, LocalVerdict: "pass", BaseSupport: "unsupported", WorkflowSupport: "undeclared", Findings: []Finding{}}}
	if p.Schema == ProfileSchemaV3 || pack.Schema == PackSchemaV3 {
		e.report.Operator = ComponentOperatorVersion
	}
	if pack.Schema == PackSchema || pack.Schema == PackSchemaV3 {
		e.report.BaseSupport = "evaluated"
	}
	total := 0
	for _, in := range inputs {
		if ctx.Err() != nil {
			return Report{}, ctx.Err()
		}
		total += len(in.Bytes)
		if !token.MatchString(in.ID) || e.report.Inputs[in.ID] != "" || len(in.Bytes) > MaxBytes || total > 64<<20 {
			return Report{}, invalid
		}
		e.report.Inputs[in.ID] = digest(in.Bytes)
		e.id = in.ID
		doc, err := hl7.Parse(in.Bytes, hl7.Options{})
		if err != nil || len(doc.Messages) != 1 {
			return Report{}, errors.New("profile evaluation requires one losslessly parsed message per occurrence")
		}
		e.doc = doc
		if len(doc.Messages[0].Segments) > 4096 {
			return Report{}, errors.New("profile evaluation segment limit")
		}
		version := e.read("MSH-12.1")
		family := e.read("MSH-9.1")
		if version.Reason != "" || family.Reason != "" {
			e.add("message-charset", "local", "unsupported", "MSH-18", e.read("MSH-18"))
			continue
		}
		if string(version.Decoded) != p.Definition.Base.HL7Version || string(family.Decoded) != p.Definition.Base.Family {
			e.add("message-version-family", "local", "unsupported", "MSH-12.1", version)
			continue
		}
		e.evaluateLocal()
		for _, seg := range doc.Messages[0].Segments {
			if strings.HasPrefix(seg.ID, "Z") && !slices.ContainsFunc(p.Definition.Segments, func(s localprofile.Segment) bool { return s.ID == seg.ID }) {
				e.add("opaque-extension", "local", "unsupported", "", hl7.Reading{Value: hl7.Value{Span: seg.Span, State: hl7.Present}})
			}
		}
		if len(p.Structure) > 0 {
			e.structure(p.Structure, "local")
		}
		e.evaluateBase()
		if e.err != nil {
			return Report{}, e.err
		}
		if e.overflow {
			return Report{}, errors.New("profile finding limit")
		}
	}
	if err := e.workflow(ctx, inputs, options); err != nil {
		return Report{}, err
	}
	if e.err != nil {
		return Report{}, e.err
	}
	if e.overflow {
		return Report{}, errors.New("profile finding limit")
	}
	for _, f := range e.report.Findings {
		if f.Origin == "profile" {
			continue
		}
		if f.Outcome == "fail" {
			e.report.LocalVerdict = "fail"
		} else if e.report.LocalVerdict != "fail" {
			e.report.LocalVerdict = "undecided"
		}
	}
	e.report.Verdict = "pass"
	if e.report.BaseSupport != "evaluated" {
		e.report.Verdict = "undecided"
	}
	for _, f := range e.report.Findings {
		if f.Outcome == "fail" {
			e.report.Verdict = "fail"
		} else if e.report.Verdict != "fail" {
			e.report.Verdict = "undecided"
		}
	}
	return e.report, nil
}
func (e *evaluator) add(rule, origin, outcome, selector string, r hl7.Reading) {
	if len(e.report.Findings) >= 8192 {
		e.overflow = true
		return
	}
	start, end := r.Span.Start, r.Span.End
	if r.State == hl7.Omitted || r.State == hl7.NoState {
		start, end = -1, -1
	}
	e.report.Findings = append(e.report.Findings, Finding{Occurrence: e.id, Rule: rule, Origin: origin, Outcome: outcome, Selector: selector, State: r.State, Start: start, End: end})
}
func (e *evaluator) read(s string) hl7.Reading {
	if e.err != nil {
		return hl7.Reading{Reason: hl7.UnsupportedEscape}
	}
	if err := e.ctx.Err(); err != nil {
		e.err = err
		return hl7.Reading{Reason: hl7.UnsupportedEscape}
	}
	e.remaining -= len(e.doc.Messages[0].Segments) + 1
	if e.remaining < 0 {
		e.err = errors.New("profile evaluation work limit")
		return hl7.Reading{Reason: hl7.UnsupportedEscape}
	}
	selector, err := hl7.ParseSelector(s)
	if err != nil {
		return hl7.Reading{Reason: hl7.UnsupportedEscape}
	}
	r, err := e.doc.Read(0, selector, hl7.EnforceMSH18)
	if err != nil {
		return hl7.Reading{Reason: hl7.UnsupportedEscape}
	}
	e.readBytes += len(r.Decoded)
	if e.readBytes > 64<<20 {
		e.err = errors.New("profile decoded byte limit")
	}
	return r
}
func (e *evaluator) evaluateLocal() {
	for _, decl := range e.profile.Definition.Segments {
		count := 0
		for _, seg := range e.doc.Messages[0].Segments {
			if seg.ID == decl.ID {
				count++
			}
		}
		if decl.Cardinality != nil && !within(count, *decl.Cardinality) {
			e.add("segment-cardinality", "local", "fail", decl.ID, hl7.Reading{Value: hl7.Value{State: hl7.Omitted}})
		}
		for occ := 1; occ <= count; occ++ {
			for _, f := range decl.Fields {
				if e.err != nil || e.overflow {
					return
				}
				sel := fmt.Sprintf("%s[%d]-%d", decl.ID, occ, f.Position)
				r := e.read(sel)
				origin := e.localOrigin(decl.ID, f.Position)
				required := f.Usage == localprofile.UsageRequired
				if f.Condition != nil {
					cond := e.condition(*f.Condition)
					if cond < 0 {
						e.add("conditional-unreadable", origin, "undecided", sel, r)
						continue
					}
					required = cond == 1
				}
				if required && r.State != hl7.Present {
					e.add("required", origin, "fail", sel, r)
				}
				if f.Usage == localprofile.UsageNotSupported && r.State != hl7.Omitted {
					e.add("not-supported-field", origin, "fail", sel, r)
				}
				if r.Reason != "" {
					e.add(string(r.Reason), origin, "unsupported", sel, r)
					continue
				}
				var segment hl7.Segment
				at := 0
				for _, s := range e.doc.Messages[0].Segments {
					if s.ID == decl.ID {
						at++
						if at == occ {
							segment = s
							break
						}
					}
				}
				field := segment.Field(f.Position)
				reps := len(field.Repetitions)
				if field.State == hl7.Omitted {
					reps = 0
				}
				if f.Cardinality != nil && !within(reps, *f.Cardinality) {
					e.add("field-cardinality", origin, "fail", sel, r)
				}
				for rep := 1; rep <= reps; rep++ {
					s := fmt.Sprintf("%s[%d]-%d[%d]", decl.ID, occ, f.Position, rep)
					reading := e.read(s)
					if reading.State != hl7.Present {
						continue
					}
					if reading.Reason != "" {
						e.add(string(reading.Reason), origin, "unsupported", s, reading)
						continue
					}
					e.field(f, s, reading, origin)
				}
			}
		}
	}
}
func within(count int, c localprofile.Cardinality) bool {
	if count < c.Min {
		return false
	}
	max, bounded := c.Bounded()
	return !bounded || count <= max
}
func (e *evaluator) condition(c localprofile.Condition) int {
	count := 0
	for _, s := range e.doc.Messages[0].Segments {
		if s.ID == c.Segment {
			count++
		}
	}
	if count > 1 {
		return -1
	}
	r := e.read(fmt.Sprintf("%s-%d", c.Segment, c.Position))
	if r.Reason != "" {
		return -1
	}
	holds := false
	switch c.Operator {
	case localprofile.ConditionPresent:
		holds = r.State == hl7.Present
	case localprofile.ConditionAbsent:
		holds = r.State != hl7.Present
	case localprofile.ConditionValueIn:
		holds = r.State == hl7.Present && slices.Contains(c.Values, string(r.Decoded))
	}
	if holds {
		return 1
	}
	return 0
}
func (e *evaluator) field(f localprofile.Field, sel string, r hl7.Reading, origin string) {
	value := string(r.Decoded)
	if f.Type != "" {
		e.evaluateDatatype(string(f.Type), sel, origin, r, 0)
	}
	if f.Terminology != "" {
		for _, set := range e.profile.Definition.Terminology {
			if set.ID == f.Terminology && set.Binding == localprofile.BindingRequired {
				code := r
				if slices.Contains([]string{"CE", "CF", "CNE", "CWE"}, string(f.Type)) {
					code = e.read(sel + ".1")
				}
				if code.Reason != "" {
					e.add("terminology-unreadable", origin, "unsupported", sel, code)
				} else if !slices.ContainsFunc(set.Codes, func(c localprofile.Code) bool { return c.Code == string(code.Decoded) }) {
					e.add("local-code-set", "local", "fail", sel, code)
				}
			}
		}
	}
	if f.Authority != "" {
		e.authority(f, sel, "local")
	}
	if f.Date != "" {
		for _, date := range e.profile.Definition.Dates {
			if date.ID == f.Date {
				dateValue := value
				if f.Type == "TS" {
					if e.report.Operator == ComponentOperatorVersion {
						dateValue = string(e.read(sel + ".1").Decoded)
					} else {
						dateValue = strings.Split(value, "^")[0]
					}
				}
				if !dateMatches(dateValue, string(f.Type), date) {
					e.add("date-rule", "local", "fail", sel, r)
				}
			}
		}
	}
}
func (e *evaluator) authority(f localprofile.Field, sel, origin string) {
	for _, a := range e.profile.Definition.Authorities {
		if a.ID != f.Authority {
			continue
		}
		positions := []int{}
		switch f.Type {
		case "EI":
			positions = []int{2, 3, 4}
		case "HD":
			positions = []int{1, 2, 3}
		case "CX":
			positions = []int{4}
		case "PL":
			positions = []int{4}
		case "XCN":
			positions = []int{9}
		case "XON":
			positions = []int{6}
		}
		expected := []string{a.Namespace, a.UniversalID, a.UniversalIDType}
		for i, want := range expected {
			if want == "" {
				continue
			}
			path := sel + "." + strconv.Itoa(positions[0])
			if len(positions) > 1 {
				path = sel + "." + strconv.Itoa(positions[i])
			} else {
				path += "." + strconv.Itoa(i+1)
			}
			got := e.read(path)
			if got.Reason != "" {
				e.add("authority-unreadable", origin, "unsupported", path, got)
			} else if got.State != hl7.Present || string(got.Decoded) != want {
				e.add("assigning-authority", origin, "fail", path, got)
			}
		}
	}
}
