package redact

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/hl7"
)

var occurrencePattern = regexp.MustCompile(`^s[0-9]{4}-e[0-9]{6}$`)

type transformer struct {
	policy   Policy
	local    localState
	findings []exportreview.Finding
	policies map[string]bool
	original map[string]*hl7.Document
	derived  map[string]*hl7.Document
}

type edit struct {
	span  hl7.Span
	value []byte
}

func (t *transformer) finding(location, class, reason, policy string, resolved bool) error {
	if len(t.findings) >= maxFindings {
		return errors.New("redaction review exceeds finding limit")
	}
	t.findings = append(t.findings, exportreview.Finding{Location: location, Class: class, Reason: reason, Policy: policy, Resolved: resolved})
	if resolved && policy != "" {
		t.policies[policy] = true
	}
	return nil
}

func (t *transformer) has(name string) bool { return slices.Contains(t.policy.PacketPolicies, name) }

func (t *transformer) transformCase(source *bundle.Bundle) ([]bundle.Input, error) {
	// A derived case is readmit-case/v3, which carries no collection record.
	// Refuse collected receiver evidence rather than drop its retained
	// receipts, session labels and literal control IDs without a finding.
	if source.Collection != nil {
		return nil, errors.New("derived redaction does not support collected receiver evidence")
	}
	inputs := make([]bundle.Input, len(source.Manifest.Sources))
	for i, src := range source.Manifest.Sources {
		inputs[i] = bundle.Input{Options: hl7.Options{Format: src.Format, Terminator: src.Terminator}, Observations: map[int]bundle.Observation{}}
		if err := t.finding("case/manifest/sources/"+src.ID+"/path", "other-unique-identifiers", "source-filename", Filenames, t.has(Filenames)); err != nil {
			return nil, err
		}
	}
	if err := t.finding("case/manifest/provenance-and-observed-times", "dates-and-ages", "source-metadata", Metadata, t.has(Metadata)); err != nil {
		return nil, err
	}
	if source.Observation != nil {
		if err := t.finding("case/observation.json", "other-unique-identifiers", "original-observation-excluded-and-rerun", Rerun, t.has(Rerun)); err != nil {
			return nil, err
		}
	}
	for _, event := range source.Events {
		raw, err := source.Raw(event.ID)
		if err != nil {
			return nil, err
		}
		data, err := t.transformOccurrence(event, raw)
		if err != nil {
			return nil, err
		}
		index := slices.IndexFunc(source.Manifest.Sources, func(s bundle.Source) bool { return s.ID == event.SourceID })
		if len(inputs[index].Data)+len(data) > bundle.MaxSourceBytes {
			return nil, errors.New("derived source exceeds size limit")
		}
		inputs[index].Data = append(inputs[index].Data, data...)
		inputs[index].Observations[event.Sequence] = bundle.Observation{Direction: event.Direction}
	}
	return inputs, nil
}

func (t *transformer) transformOccurrence(event bundle.Event, raw []byte) ([]byte, error) {
	location := "case/" + event.ID
	if event.Kind == bundle.Unparsed {
		return raw, t.finding(location, "other-unique-identifiers", "unparsed-occurrence", "", false)
	}
	doc, err := hl7.Parse(raw, hl7.Options{Terminator: event.Terminator})
	if err != nil {
		return nil, errors.New("cannot parse verified occurrence")
	}
	t.original[event.ID] = doc
	message := doc.Messages[0]
	if message.Delimiters != (hl7.Delimiters{Field: '|', Component: '^', Repetition: '~', Escape: '\\', Subcomponent: '&'}) {
		return raw, t.finding(location, "other-unique-identifiers", "unsupported-delimiters", "", false)
	}
	covered := make([]bool, len(raw))
	var edits []edit
	removed := map[string]bool{}
	counts := map[string]int{}
	for index, segment := range message.Segments {
		counts[segment.ID]++
		segmentPath := fmt.Sprintf("%s/%s[%d]", location, segment.ID, counts[segment.ID])
		if slices.Contains(t.policy.RemoveSegments, segment.ID) {
			end := message.Span.End
			if index+1 < len(message.Segments) {
				end = message.Segments[index+1].Span.Start
			}
			// Message.Span excludes MLLP framing; keep any framing intact.
			edits = append(edits, edit{span: hl7.Span{Start: segment.Span.Start, End: end}})
			for i := segment.Span.Start; i < end; i++ {
				covered[i] = true
			}
			removed[segment.ID] = true
			t.addTerm(raw[segment.Span.Start:segment.Span.End])
			if err := t.finding(segmentPath, "other-unique-identifiers", "segment-removed", RemoveSegment, true); err != nil {
				return nil, err
			}
			continue
		}
		if !slices.Contains([]string{"MSH", "SCH", "PID", "MSA", "NTE", "OBX", "ERR"}, segment.ID) {
			if err := t.finding(segmentPath, "other-unique-identifiers", "unknown-segment", "", false); err != nil {
				return nil, err
			}
		}
	}
	for _, rule := range t.policy.Fields {
		selector, _ := hl7.ParseSelector(rule.Selector)
		if removed[selector.Parts().Segment] {
			continue
		}
		value, _ := doc.Read(0, selector, hl7.IgnoreMSH18)
		if value.State == hl7.Omitted {
			continue
		}
		path := location + "/" + selector.String()
		if textField(selector) && rule.Policy != Remove && rule.Policy != Replace {
			if err := t.finding(path, rule.Class, "free-text-or-embedded-payload", "", false); err != nil {
				return nil, err
			}
			continue
		}
		if value.Literal {
			if rule.Policy != Retain {
				return nil, errors.New("delimiter declarations may only retain exact literals")
			}
		}
		for _, existing := range edits {
			if value.Span.Start < existing.span.End && existing.span.Start < value.Span.End {
				return nil, errors.New("redaction policies overlap")
			}
		}
		original := doc.Bytes(value.Span)
		replacement, handled, err := t.applyRule(doc, value, rule)
		if err != nil {
			return nil, err
		}
		if err := t.finding(path, rule.Class, "named-field", rule.Policy, handled); err != nil {
			return nil, err
		}
		if !handled {
			continue
		}
		for i := value.Span.Start; i < value.Span.End; i++ {
			covered[i] = true
		}
		edits = append(edits, edit{span: value.Span, value: replacement})
		if rule.Class != "structural" && !bytes.Equal(original, replacement) {
			t.addTerm(original)
		}
	}
	counts = map[string]int{}
	for _, segment := range message.Segments {
		counts[segment.ID]++
		for _, field := range segment.Fields {
			if field.State == hl7.Empty {
				continue
			}
			unhandled := false
			for i := field.Span.Start; i < field.Span.End; i++ {
				separator := strings.ContainsRune("^~&", rune(raw[i])) && !(segment.ID == "MSH" && field.Number <= 2)
				if !covered[i] && !separator {
					unhandled = true
					break
				}
			}
			if unhandled {
				path := fmt.Sprintf("%s/%s[%d]-%d", location, segment.ID, counts[segment.ID], field.Number)
				reason := "unmapped-field"
				// The parser admitted the segment identifier and every position.
				first, _ := hl7.NewSelector(hl7.Parts{Segment: segment.ID, Occurrence: counts[segment.ID], Field: field.Number, Repetition: 1})
				if textField(first) {
					reason = "free-text-or-embedded-payload"
				}
				if err := t.finding(path, "other-unique-identifiers", reason, "", false); err != nil {
					return nil, err
				}
			}
		}
	}
	slices.SortFunc(edits, func(a, b edit) int { return a.span.Start - b.span.Start })
	var output []byte
	position := 0
	for _, change := range edits {
		if change.span.Start < position {
			return nil, errors.New("redaction spans overlap")
		}
		output = append(output, raw[position:change.span.Start]...)
		output = append(output, change.value...)
		position = change.span.End
	}
	output = append(output, raw[position:]...)
	if len(output) > bundle.MaxSourceBytes {
		return nil, errors.New("derived occurrence exceeds size limit")
	}
	derived, err := hl7.Parse(output, hl7.Options{Terminator: event.Terminator})
	if err != nil {
		return nil, errors.New("transformation produced unsupported HL7 syntax")
	}
	t.derived[event.ID] = derived
	return output, nil
}

func textField(selector hl7.Selector) bool {
	position := selector.Parts()
	switch position.Segment {
	case "NTE":
		return position.Field == 3
	case "OBX":
		// V1 does not assign semantics to the observation's remaining values,
		// display labels or payload encodings. Require replacement/removal.
		return position.Field != 1 && position.Field != 2
	case "MSA":
		return position.Field != 1 && position.Field != 2
	case "ERR":
		if position.Field == 3 {
			return position.Subcomponent != 0 || position.Component != 1 && position.Component != 3
		}
		return position.Field != 2 && position.Field != 4
	}
	return false
}

func (t *transformer) applyRule(doc *hl7.Document, value hl7.Reading, rule FieldRule) ([]byte, bool, error) {
	raw := doc.Bytes(value.Span)
	if rule.Policy == Remove {
		return nil, true, nil
	}
	if rule.Policy == Replace {
		return []byte(*rule.Replacement), true, nil
	}
	if rule.Policy == Retain {
		return raw, slices.Contains(rule.Allowed, string(raw)), nil
	}
	if value.State == hl7.Empty || value.State == hl7.Null {
		return raw, true, nil
	}
	decoded, ok := value.Text()
	if !ok || len(decoded) > 4096 {
		return raw, false, nil
	}
	// Scalar surrogates cannot discard unselected components or repetitions.
	if bytes.ContainsAny(raw, "^~&") {
		return raw, false, nil
	}
	if rule.Policy == Surrogate {
		key, err := scopeKey(doc, rule.Scope, rule.Selector, rule.Authority)
		if err != nil {
			return raw, false, nil
		}
		for _, item := range t.local.Mappings {
			if item.Key == key {
				return []byte(item.Surrogate), true, nil
			}
		}
		var entropy [16]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			return nil, false, errors.New("cannot generate surrogate")
		}
		surrogate := "R" + hex.EncodeToString(entropy[:])
		t.local.Mappings = append(t.local.Mappings, mapping{Key: key, Source: decoded, Surrogate: surrogate})
		t.addTerm([]byte(decoded))
		return []byte(surrogate), true, nil
	}
	key, err := scopeKey(doc, "patient", t.policy.Patient.Selector, t.policy.Patient.Authority)
	if err != nil {
		return raw, false, nil
	}
	days := 0
	for _, item := range t.local.Shifts {
		if item.PatientKey == key {
			days = item.Days
		}
	}
	if days == 0 {
		n, err := rand.Int(rand.Reader, big.NewInt(730))
		if err != nil {
			return nil, false, errors.New("cannot select testing date shift")
		}
		days = int(n.Int64()) - 365
		if days >= 0 {
			days++
		}
		t.local.Shifts = append(t.local.Shifts, shift{PatientKey: key, Days: days})
	}
	shifted, ok := shiftDate(decoded, days)
	return []byte(shifted), ok, nil
}

func scopeKey(doc *hl7.Document, scope, selector string, authority []string) (string, error) {
	values := []string{scope}
	for index, path := range append([]string{selector}, authority...) {
		s, _ := hl7.ParseSelector(path)
		v, _ := doc.Read(0, s, hl7.IgnoreMSH18)
		if index == 0 && v.State != hl7.Present || v.State == hl7.Null {
			return "", errors.New("unknown identifier scope")
		}
		text := ""
		if v.State == hl7.Present {
			decoded, ok := v.Text()
			if !ok || len(decoded) > 4096 {
				return "", errors.New("unsupported identifier scope")
			}
			text = decoded
		}
		values = append(values, string(v.State), text)
	}
	data, err := json.Marshal(values, json.Deterministic(true))
	return string(data), err
}

func shiftDate(value string, days int) (string, bool) {
	layout := ""
	switch len(value) {
	case 8:
		layout = "20060102"
	case 14:
		layout = "20060102150405"
	case 19:
		layout = "20060102150405-0700"
	}
	if layout == "" {
		return "", false
	}
	parsed, err := time.Parse(layout, value)
	if err != nil || parsed.Format(layout) != value {
		return "", false
	}
	shifted := parsed.AddDate(0, 0, days)
	if shifted.Year() < 1 || shifted.Year() > 9999 {
		return "", false
	}
	return shifted.Format(layout), true
}

func (t *transformer) addTerm(value []byte) {
	if len(value) == 0 {
		return
	}
	for _, existing := range t.local.ResidualValues {
		if bytes.Equal(existing, value) {
			return
		}
	}
	t.local.ResidualValues = append(t.local.ResidualValues, bytes.Clone(value))
}
