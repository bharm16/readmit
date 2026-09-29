// Package textmerge merges two edits of one text line by line against the
// text they both started from: a part only one side changed takes that
// change, a part both changed the same way takes it once, and a part they
// changed differently is a conflict left for a person to decide. It never
// invents a combination of two changes.
package textmerge

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// Hunk is one part of a merge: lines both sides agree on, or a conflict
// where each side changed the base differently.
type Hunk struct {
	Conflict bool
	Lines    []string
	Base     []string
	Yours    []string
	Current  []string
}

// maxText bounds the text a merge reads, each side.
const maxText = 1 << 20

// Lines splits UTF-8 text into lines that keep their endings, so joining
// them gives back the exact bytes; nil for anything that is not such text.
func Lines(data []byte) []string {
	if len(data) > maxText || !utf8.Valid(data) || slices.Contains(data, 0) {
		return nil
	}
	return strings.SplitAfter(string(data), "\n")
}

// maxCells bounds the comparison table a merge builds.
const maxCells = 4_000_000

// Merge merges two edits of one base line by line: a part only one side
// changed takes that change, a part both changed the same way takes it once,
// and a part they changed differently is a conflict. ok is false when the
// texts are too long to compare.
func Merge(base, yours, current []string) ([]Hunk, bool) {
	if len(base)*len(yours) > maxCells || len(base)*len(current) > maxCells {
		return nil, false
	}
	toYours, toCurrent := matchLines(base, yours), matchLines(base, current)
	hunks := []Hunk{}
	agreed := func(lines []string) {
		if len(lines) == 0 {
			return
		}
		if n := len(hunks); n > 0 && !hunks[n-1].Conflict {
			hunks[n-1].Lines = append(hunks[n-1].Lines, lines...)
			return
		}
		hunks = append(hunks, Hunk{Lines: slices.Clone(lines)})
	}
	i, a, b := 0, 0, 0
	for i < len(base) || a < len(yours) || b < len(current) {
		if i < len(base) && toYours[i] == a && toCurrent[i] == b {
			agreed(base[i : i+1])
			i, a, b = i+1, a+1, b+1
			continue
		}
		next := i
		for next < len(base) && (toYours[next] < 0 || toCurrent[next] < 0) {
			next++
		}
		nextA, nextB := len(yours), len(current)
		if next < len(base) {
			nextA, nextB = toYours[next], toCurrent[next]
		}
		was, mine, theirs := base[i:next], yours[a:nextA], current[b:nextB]
		switch {
		case slices.Equal(mine, was):
			agreed(theirs)
		case slices.Equal(theirs, was), slices.Equal(mine, theirs):
			agreed(mine)
		default:
			hunks = append(hunks, Hunk{Conflict: true, Base: slices.Clone(was), Yours: slices.Clone(mine), Current: slices.Clone(theirs)})
		}
		i, a, b = next, nextA, nextB
	}
	return hunks, true
}

// matchLines maps each base line onto the line of other a longest common
// subsequence pairs it with, or -1.
func matchLines(base, other []string) []int {
	n, m := len(base), len(other)
	table := make([]int32, (n+1)*(m+1))
	at := func(i, j int) int { return i*(m+1) + j }
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if base[i] == other[j] {
				table[at(i, j)] = table[at(i+1, j+1)] + 1
			} else {
				table[at(i, j)] = max(table[at(i+1, j)], table[at(i, j+1)])
			}
		}
	}
	match := make([]int, n)
	for i := range match {
		match[i] = -1
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case base[i] == other[j]:
			match[i] = j
			i, j = i+1, j+1
		case table[at(i+1, j)] >= table[at(i, j+1)]:
			i++
		default:
			j++
		}
	}
	return match
}
