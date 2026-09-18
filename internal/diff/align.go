package diff

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/replay"
)

type alignedPair struct{ left, right *occurrence }

// Known mappings are checked before considering keys. A contradictory source
// claim is an evidence error, never a reason to silently fall back to a key.
func align(left, right *evidence, keys []hl7.Selector, boundary Boundary, report *Report) ([]alignedPair, error) {
	if left.summary.Kind == "result" && left.run == nil || right.summary.Kind == "result" && right.run == nil {
		report.Alignment = "unavailable"
		for _, side := range []struct {
			name     string
			evidence *evidence
		}{{"left", left}, {"right", right}} {
			for _, item := range side.evidence.items {
				report.Unaligned = append(report.Unaligned, Unaligned{side.name, item.ref, "missing_run_evidence"})
			}
		}
		return nil, nil
	}
	known, err := verifyMapping(left, right, boundary)
	if err != nil {
		return nil, err
	}
	if known {
		report.Alignment = "source-occurrence"
		return alignKnown(left, right, report), nil
	}
	if left.summary.Kind == "file" && right.summary.Kind == "file" && len(left.items) == 1 && len(right.items) == 1 && len(keys) == 0 {
		report.Alignment = "single-message"
		return []alignedPair{{left.items[0], right.items[0]}}, nil
	}
	if len(keys) == 0 {
		return nil, errors.New("unrelated collections require explicit --key selectors for alignment")
	}
	report.Alignment = "declared-keys"
	return alignKeys(left, right, keys, report), nil
}

func verifyMapping(left, right *evidence, boundary Boundary) (bool, error) {
	if left.run != nil && right.source != nil && left.run.Manifest.SourceBundleIdentity == right.source.Identity {
		if err := verifySource(left, right); err != nil {
			return false, err
		}
		// Run ACKs are identified by the initiating source message, whereas case
		// ACKs have their own occurrence IDs. No cross-boundary match is implied.
		return boundary == Messages, nil
	}
	if right.run != nil && left.source != nil && right.run.Manifest.SourceBundleIdentity == left.source.Identity {
		if err := verifySource(right, left); err != nil {
			return false, err
		}
		return boundary == Messages, nil
	}
	if left.source != nil && right.source != nil && left.source.Identity == right.source.Identity {
		return true, nil
	}
	if left.run == nil || right.run == nil || left.run.Manifest.SourceBundleIdentity != right.run.Manifest.SourceBundleIdentity {
		return false, nil
	}
	bySource := make(map[string]replay.Event, len(left.run.Events))
	for _, event := range left.run.Events {
		bySource[event.SourceOccurrence] = event
	}
	for _, r := range right.run.Events {
		l, exists := bySource[r.SourceOccurrence]
		if !exists {
			continue
		}
		leftRaw, leftErr := left.run.Raw(l.Source)
		rightRaw, rightErr := right.run.Raw(r.Source)
		if leftErr != nil || rightErr != nil || !bytes.Equal(leftRaw, rightRaw) {
			return false, errors.New("inconsistent original source bytes in common-source runs")
		}
	}
	return true, nil
}

func verifySource(run, source *evidence) error {
	for i, event := range run.run.Events {
		mapping := run.run.Manifest.Mappings[i]
		original, err := source.source.Raw(mapping.SourceOccurrence)
		stored, storedErr := run.run.Raw(event.Source)
		if err != nil || storedErr != nil || digest(original) != mapping.SourceSHA256 || !bytes.Equal(original, stored) {
			return errors.New("run source mapping does not agree with supplied source evidence")
		}
	}
	return nil
}

func sourceID(item *occurrence) string {
	if item.ref.SourceOccurrence != "" {
		return item.ref.SourceOccurrence
	}
	return item.ref.Occurrence
}

func alignKnown(left, right *evidence, report *Report) []alignedPair {
	byID := make(map[string]*occurrence, len(right.items))
	for _, item := range right.items {
		byID[sourceID(item)] = item
	}
	var pairs []alignedPair
	for _, item := range left.items {
		if other, exists := byID[sourceID(item)]; exists {
			pairs = append(pairs, alignedPair{item, other})
			delete(byID, sourceID(item))
		} else {
			report.Missing = append(report.Missing, item.ref)
		}
	}
	for _, item := range right.items {
		if _, exists := byID[sourceID(item)]; exists {
			report.Inserted = append(report.Inserted, item.ref)
		}
	}
	return pairs
}

func alignKeys(left, right *evidence, selectors []hl7.Selector, report *Report) []alignedPair {
	type group struct{ left, right []*occurrence }
	groups := make(map[string]*group)
	var order []string
	for _, side := range []struct {
		name     string
		evidence *evidence
	}{{"left", left}, {"right", right}} {
		for _, item := range side.evidence.items {
			key, reason := alignmentKey(item, selectors)
			if reason != "" {
				report.Unaligned = append(report.Unaligned, Unaligned{side.name, item.ref, reason})
				continue
			}
			g, exists := groups[key]
			if !exists {
				g = &group{}
				groups[key] = g
				order = append(order, key)
			}
			if side.name == "left" {
				g.left = append(g.left, item)
			} else {
				g.right = append(g.right, item)
			}
		}
	}
	var pairs []alignedPair
	missing, inserted := map[*occurrence]bool{}, map[*occurrence]bool{}
	for _, key := range order {
		g := groups[key]
		switch {
		case len(g.left) == 0:
			for _, item := range g.right {
				inserted[item] = true
			}
		case len(g.right) == 0:
			for _, item := range g.left {
				missing[item] = true
			}
		case len(g.left) > 1 || len(g.right) > 1:
			ambiguity := Ambiguity{Reason: "duplicate_key"}
			for _, item := range g.left {
				ambiguity.Left = append(ambiguity.Left, item.ref)
			}
			for _, item := range g.right {
				ambiguity.Right = append(ambiguity.Right, item.ref)
			}
			report.Ambiguous = append(report.Ambiguous, ambiguity)
		default:
			pairs = append(pairs, alignedPair{g.left[0], g.right[0]})
		}
	}
	// Duplicate keys can be interleaved with other one-sided keys. Preserve the
	// original occurrence order rather than regrouping definite insertions/gaps.
	for _, item := range left.items {
		if missing[item] {
			report.Missing = append(report.Missing, item.ref)
		}
	}
	for _, item := range right.items {
		if inserted[item] {
			report.Inserted = append(report.Inserted, item.ref)
		}
	}
	return pairs
}

func alignmentKey(item *occurrence, selectors []hl7.Selector) (string, string) {
	if item.doc == nil {
		return "", "payload_unavailable"
	}
	var key bytes.Buffer
	// Including kind prevents ACK and initiating-message collisions in raw files.
	key.WriteString(item.ref.Kind)
	var size [8]byte
	for _, selector := range selectors {
		value, raw := selectValue(item, selector, false)
		if value.State != hl7.Present {
			return "", "key_not_present"
		}
		if value.Encoding != "decoded-utf8" && value.Encoding != "literal-delimiters" {
			return "", "key_not_decodable"
		}
		binary.BigEndian.PutUint64(size[:], uint64(len(raw)))
		key.Write(size[:])
		key.Write(raw)
	}
	return key.String(), ""
}
