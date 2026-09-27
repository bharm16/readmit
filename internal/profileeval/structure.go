package profileeval

import (
	"errors"
	"github.com/bharm16/readmit/internal/hl7"
	"strconv"
)

// The matcher explores bounded alternatives instead of greedily assigning a
// repeated segment to the first optional group. Exhaustion is unsupported,
// never a successful parse of a retained prefix.
type matchKey struct {
	node  *Node
	start int
}
type matcher struct {
	segments  []hl7.Segment
	memo      map[matchKey]map[int]bool
	work      int
	exhausted bool
}

func (m *matcher) spend() bool {
	m.work++
	if m.work > 200000 {
		m.exhausted = true
		return false
	}
	return true
}
func (m *matcher) sequence(nodes []Node, start int) map[int]bool {
	positions := map[int]bool{start: true}
	for i := range nodes {
		next := map[int]bool{}
		for p := range positions {
			for end := range m.node(&nodes[i], p) {
				if !m.spend() {
					return nil
				}
				next[end] = true
			}
		}
		positions = next
		if len(positions) == 0 {
			break
		}
	}
	return positions
}
func (m *matcher) node(n *Node, start int) map[int]bool {
	key := matchKey{n, start}
	if known, ok := m.memo[key]; ok {
		return known
	}
	if !m.spend() {
		return nil
	}
	max := len(m.segments) + 1
	if n.Max != "*" {
		max, _ = strconv.Atoi(n.Max)
	}
	max = min(max, len(m.segments)+1)
	accepted := map[int]bool{}
	positions := map[int]bool{start: true}
	if n.Min == 0 {
		accepted[start] = true
	}
	for count := 1; count <= max && len(positions) > 0; count++ {
		next := map[int]bool{}
		for pos := range positions {
			if !m.spend() {
				return nil
			}
			ends := map[int]bool{}
			if n.Segment != "" {
				if pos < len(m.segments) && m.segments[pos].ID == n.Segment {
					ends[pos+1] = true
				}
			} else {
				ends = m.sequence(n.Children, pos)
			}
			for end := range ends {
				if count >= n.Min {
					accepted[end] = true
				}
				if end > pos {
					next[end] = true
				}
			}
		}
		positions = next
	}
	m.memo[key] = accepted
	return accepted
}
func (e *evaluator) structure(nodes []Node, origin string) {
	m := matcher{segments: e.doc.Messages[0].Segments, memo: map[matchKey]map[int]bool{}}
	ends := m.sequence(nodes, 0)
	e.remaining -= m.work
	if e.remaining < 0 {
		e.err = errors.New("profile evaluation work limit")
		return
	}
	if m.exhausted {
		e.add("structure-budget", origin, "unsupported", "", hl7.Reading{})
		return
	}
	if !ends[len(m.segments)] {
		e.add("segment-group-order-cardinality", origin, "fail", "", hl7.Reading{Value: hl7.Value{Span: e.doc.Messages[0].Span, State: hl7.Present}})
	}
}
