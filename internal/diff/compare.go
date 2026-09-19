package diff

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/dictionary"
	"github.com/bharm16/readmit/internal/hl7"
)

const maxComparisons = 200000

// Compare verifies inputs before aligning or reporting them. Errors are bounded
// diagnostics without source paths or values. Malformed payloads and unsupported
// decoded values are reportable evidence gaps, not silent omissions.
func Compare(left, right Input, options Options) (Report, error) {
	return compare(left, right, options, nil)
}

// compare is the one comparison both reports are read from. A policy, when one
// is supplied, is told what the comparison found; it never changes what the
// readmit-diff/v1 report says, so the raw comparison stays exactly what it was
// before any rule was authored.
func compare(left, right Input, options Options, applied *appliedPolicy) (Report, error) {
	if options.Boundary == "" {
		options.Boundary = Messages
	}
	if options.Boundary != Messages && options.Boundary != ACKs {
		return Report{}, errors.New("diff boundary must be messages or acks")
	}
	for _, input := range []Input{left, right} {
		if input.Format != "" && input.Format != "auto" && input.Format != hl7.Raw && input.Format != hl7.MLLP {
			return Report{}, errors.New("diff input format must be auto, raw, or mllp")
		}
		if input.Terminator != "" && input.Terminator != "auto" && input.Terminator != hl7.CR && input.Terminator != hl7.LF && input.Terminator != hl7.CRLF {
			return Report{}, errors.New("diff terminator must be auto, cr, lf, or crlf")
		}
	}
	fields, err := parseSelectors(options.Fields, 256)
	if err != nil {
		return Report{}, err
	}
	keys, err := parseSelectors(options.Keys, 16)
	if err != nil {
		return Report{}, err
	}
	ignore, err := parseSelectors(options.Ignore, 256)
	if err != nil {
		return Report{}, err
	}
	l, err := open(left, options.Boundary)
	if err != nil {
		return Report{}, err
	}
	r, err := open(right, options.Boundary)
	if err != nil {
		return Report{}, err
	}
	labels, err := dictionary.Load()
	if err != nil {
		return Report{}, err
	}
	report := Report{Schema: Schema, Boundary: options.Boundary, ShowValues: options.ShowValues, Left: l.summary, Right: r.summary, Keys: canonical(keys), Fields: canonical(fields), Scope: "Field comparisons cover the listed payloads only. Framing and segment terminators are not compared. Unsupported evidence remains visible. Equal wire fields do not prove delivery, ledger correctness, or business-workflow correctness. Content identities are not authenticity signatures."}
	for _, selector := range ignore {
		report.Ignore = append(report.Ignore, IgnoreRule{Selector: selector.String()})
	}
	for _, side := range []struct {
		name     string
		evidence *evidence
	}{{"left", l}, {"right", r}} {
		for _, gap := range side.evidence.unsupported {
			gap.Side = side.name
			report.Unsupported = append(report.Unsupported, gap)
		}
		for _, item := range side.evidence.items {
			if item.doc == nil {
				report.Unsupported = append(report.Unsupported, Unsupported{Side: side.name, Occurrence: item.ref.Occurrence, Code: item.ref.PayloadState})
			} else if !hasDictionary(item, labels) {
				report.Unsupported = append(report.Unsupported, Unsupported{Side: side.name, Occurrence: item.ref.Occurrence, Selector: "MSH[1]-12[1].1", Code: "unknown_dictionary_version"})
			}
		}
	}
	pairs, err := align(l, r, keys, options.Boundary, &report)
	if err != nil {
		return Report{}, err
	}
	comparisons := 0
	for _, pair := range pairs {
		result, err := comparePair(pair, fields, labels, &report, &comparisons, applied)
		if err != nil {
			return Report{}, err
		}
		report.Pairs = append(report.Pairs, result)
		switch result.Status {
		case "changed":
			report.Summary.Changed++
		case "unchanged":
			report.Summary.Unchanged++
		case "uncompared":
			report.Summary.Uncompared++
		}
	}
	report.Summary.Paired = len(report.Pairs)
	report.Summary.Inserted, report.Summary.Missing = len(report.Inserted), len(report.Missing)
	report.Summary.Ambiguous, report.Summary.Unaligned = len(report.Ambiguous), len(report.Unaligned)
	return report, nil
}

func parseSelectors(paths []string, limit int) ([]hl7.Selector, error) {
	if len(paths) > limit {
		return nil, errors.New("too many diff selectors")
	}
	selectors := make([]hl7.Selector, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		selector, err := hl7.ParseSelector(path)
		if err != nil {
			return nil, err
		}
		if seen[selector.String()] {
			return nil, errors.New("duplicate diff selector")
		}
		seen[selector.String()] = true
		selectors = append(selectors, selector)
	}
	return selectors, nil
}

func canonical(selectors []hl7.Selector) []string {
	paths := make([]string, len(selectors))
	for i, selector := range selectors {
		paths[i] = selector.String()
	}
	return paths
}

func comparePair(pair alignedPair, fields []hl7.Selector, labels *dictionary.Dictionary, report *Report, comparisons *int, applied *appliedPolicy) (Pair, error) {
	result := Pair{Left: pair.left.ref, Right: pair.right.ref, Status: "unchanged"}
	if pair.left.doc == nil || pair.right.doc == nil {
		result.Status = "uncompared"
		return result, nil
	}
	if len(fields) == 0 {
		fields = encounteredFields(pair)
		result.Segments = segmentChanges(pair)
		if len(result.Segments) > 0 {
			result.Status = "changed"
		}
	}
	knownNames := hasDictionary(pair.left, labels) && hasDictionary(pair.right, labels)
	for _, selector := range fields {
		*comparisons++
		if *comparisons > maxComparisons {
			return Pair{}, errors.New("diff exceeds 200000 field comparisons; select a narrower field scope")
		}
		left, leftBytes := selectValue(pair.left, selector, report.ShowValues)
		right, rightBytes := selectValue(pair.right, selector, report.ShowValues)
		unsupported := false
		for _, side := range []struct {
			name  string
			ref   Reference
			value Value
		}{{"left", pair.left.ref, left}, {"right", pair.right.ref, right}} {
			if side.value.Encoding == "unsupported_escape" || side.value.Encoding == "non_utf8" {
				unsupported = true
				report.Unsupported = append(report.Unsupported, Unsupported{Side: side.name, Occurrence: side.ref.Occurrence, Selector: selector.String(), Code: side.value.Encoding})
			}
		}
		equal := left.State == right.State && bytes.Equal(leftBytes, rightBytes)
		ignored := false
		for i := range report.Ignore {
			if report.Ignore[i].Selector == selector.String() {
				ignored = true
				report.Ignore[i].Compared++
				if !equal && !unsupported {
					report.Ignore[i].Suppressed++
				}
			}
		}
		if applied != nil {
			applied.compared(selector.String())
		}
		// An ignore does not make an undecodable field look comparable.
		if unsupported || !equal && !ignored {
			change := FieldChange{Selector: selector.String(), Left: left, Right: right, Status: "changed"}
			if knownNames {
				change.Name = fieldName(selector, labels)
			}
			if unsupported {
				change.Status = "uncompared"
				if result.Status == "unchanged" {
					result.Status = "uncompared"
				}
			} else {
				result.Status = "changed"
				report.Summary.FieldChanges++
			}
			result.Fields = append(result.Fields, change)
			if applied != nil {
				applied.record(pair.left, pair.right, change, leftBytes, rightBytes)
			}
		}
	}
	return result, nil
}

func selectValue(item *occurrence, selector hl7.Selector, show bool) (Value, []byte) {
	selected, _ := item.doc.Select(item.index, selector) // selectors and index were validated at the input boundary
	value := Value{State: selected.State}
	if selected.State != hl7.Present {
		return value, nil
	}
	raw := item.doc.Bytes(selected.Span)
	decoded := raw
	path := selector.String()
	if path == "MSH[1]-1[1]" || path == "MSH[1]-2[1]" {
		value.Encoding = "literal-delimiters"
	} else {
		var err error
		decoded, err = hl7.Decode(raw, item.doc.Messages[item.index].Delimiters)
		if err != nil {
			value.Encoding, decoded = "unsupported_escape", raw
		} else if !utf8.Valid(decoded) {
			value.Encoding = "non_utf8"
		} else {
			value.Encoding = "decoded-utf8"
		}
	}
	if show {
		display := strconv.QuoteToASCII(string(decoded))
		value.Display = &display
	}
	return value, decoded
}

func encounteredFields(pair alignedPair) []hl7.Selector {
	seen := map[string]bool{}
	var fields []hl7.Selector
	for _, item := range []*occurrence{pair.left, pair.right} {
		counts := map[string]int{}
		for _, segment := range item.doc.Messages[item.index].Segments {
			counts[segment.ID]++
			for _, field := range segment.Fields {
				for repetition := 1; repetition <= max(1, len(field.Repetitions)); repetition++ {
					path := fmt.Sprintf("%s[%d]-%d[%d]", segment.ID, counts[segment.ID], field.Number, repetition)
					if !seen[path] {
						selector, _ := hl7.ParseSelector(path)
						fields, seen[path] = append(fields, selector), true
					}
				}
			}
		}
	}
	return fields
}

func segmentChanges(pair alignedPair) []SegmentChange {
	lists := [2][]string{}
	for i, item := range []*occurrence{pair.left, pair.right} {
		counts := map[string]int{}
		for _, segment := range item.doc.Messages[item.index].Segments {
			counts[segment.ID]++
			lists[i] = append(lists[i], fmt.Sprintf("%s[%d]", segment.ID, counts[segment.ID]))
		}
	}
	var changes []SegmentChange
	for i := 0; i < max(len(lists[0]), len(lists[1])); i++ {
		left, right := "omitted", "omitted"
		if i < len(lists[0]) {
			left = lists[0][i]
		}
		if i < len(lists[1]) {
			right = lists[1][i]
		}
		if left != right {
			changes = append(changes, SegmentChange{i + 1, left, right})
		}
	}
	return changes
}

func hasDictionary(item *occurrence, labels *dictionary.Dictionary) bool {
	selector, _ := hl7.ParseSelector("MSH-12.1")
	value, decoded := selectValue(item, selector, false)
	return value.State == hl7.Present && value.Encoding == "decoded-utf8" && string(decoded) == labels.HL7Version
}

func fieldName(selector hl7.Selector, labels *dictionary.Dictionary) string {
	segment, field, _ := strings.Cut(selector.String(), "-")
	field, _, _ = strings.Cut(field, "[")
	number, _ := strconv.Atoi(field)
	return labels.Segments[segment[:3]][number]
}
