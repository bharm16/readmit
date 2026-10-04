// Package fieldvalues counts exact selectors over an explicitly selected,
// verified occurrence scope. It reads original evidence through the shared
// parser; counts and display values are never a persistent evidence artifact.
package fieldvalues

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

const MaxValueBytes = 4096
const MaxGroupingBytes = 8 << 20

type Counts struct {
	Present     int `json:"present"`
	Empty       int `json:"empty"`
	Null        int `json:"null"`
	Omitted     int `json:"omitted"`
	Undecodable int `json:"undecodable"`
	Undecided   int `json:"undecided"`
}

type Group struct {
	State       string
	Value       string
	Hidden      bool
	Occurrences []string
}

type Result struct {
	Selector string
	Counts   Counts
	Groups   []Group
	Complete bool
	Scanned  int
}

// Count counts one exact selector per message occurrence (an explicit segment,
// repetition/component selector never means every repetition). Hidden mode
// collapses all readable present values into one bucket without raw labels,
// deterministic value digests or distinct-value fingerprints.
func Count(ctx context.Context, source *bundle.Bundle, occurrences []string, selector string, reveal bool) (Result, error) {
	selected, err := hl7.ParseSelector(selector)
	if err != nil {
		return Result{}, errors.New("choose one exact HL7 field or component selector")
	}
	if len(occurrences) > bundle.MaxEvents {
		return Result{}, errors.New("field count scope exceeds the case occurrence bound")
	}
	result := Result{Selector: selected.String(), Groups: []Group{}, Complete: true}
	events := make(map[string]bundle.Event, len(source.Events))
	for _, event := range source.Events {
		events[event.ID] = event
	}
	groups := map[string]*Group{}
	groupedBytes := 0
	seen := map[string]bool{}
	add := func(state, value, id string, hidden bool) {
		key := state + "\x00" + value
		group := groups[key]
		if group == nil {
			group = &Group{State: state, Value: value, Hidden: hidden, Occurrences: []string{}}
			groups[key] = group
			groupedBytes += len(value)
		}
		group.Occurrences = append(group.Occurrences, id)
	}
	for _, id := range occurrences {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		event, ok := events[id]
		if !ok || seen[id] {
			return Result{}, errors.New("field count scope names each verified occurrence exactly once")
		}
		seen[id] = true
		result.Scanned++
		if event.Kind == bundle.Unparsed || event.ParseError != "" {
			result.Complete = false
			result.Counts.Undecodable++
			add("undecodable", "", id, false)
			continue
		}
		doc, err := source.Document(event)
		if err != nil {
			return Result{}, errors.New("field scope no longer reads consistently with its case")
		}
		reading, err := doc.Read(0, selected, hl7.EnforceMSH18)
		if err != nil {
			result.Counts.Undecided++
			result.Complete = false
			add("undecided", "", id, false)
			continue
		}
		switch reading.State {
		case hl7.Empty:
			result.Counts.Empty++
			add("empty", "", id, false)
		case hl7.Null:
			result.Counts.Null++
			add("null", "", id, false)
		case hl7.Omitted:
			result.Counts.Omitted++
			add("omitted", "", id, false)
		case hl7.Present:
			if reading.Reason != "" {
				result.Complete = false
				result.Counts.Undecodable++
				add("undecodable", "", id, false)
				continue
			}
			if !reveal {
				result.Counts.Present++
				add("present", "", id, true)
				continue
			}
			text := string(reading.Decoded)
			_, already := groups["present\x00"+text]
			if len(text) > MaxValueBytes || !already && groupedBytes+len(text) > MaxGroupingBytes {
				result.Counts.Undecided++
				result.Complete = false
				add("undecided", "", id, false)
				continue
			}
			result.Counts.Present++
			add("present", text, id, false)
		default:
			result.Counts.Undecided++
			result.Complete = false
			add("undecided", "", id, false)
		}
	}
	for _, group := range groups {
		result.Groups = append(result.Groups, *group)
	}
	slices.SortFunc(result.Groups, func(a, b Group) int {
		if a.State != b.State {
			return strings.Compare(a.State, b.State)
		}
		return strings.Compare(a.Value, b.Value)
	})
	return result, nil
}
