package profileeval

import (
	"fmt"
	"strconv"

	"github.com/bharm16/readmit/internal/hl7"
)

// PlacementGroup records an actual matched group occurrence, separately from
// field repetitions and the segment's reusable definition.
type PlacementGroup struct {
	Name       string `json:"name"`
	Occurrence int    `json:"occurrence"`
	Min        int    `json:"min"`
	Max        string `json:"max"`
}
type Placement struct {
	State             string           `json:"state"`
	Reason            string           `json:"reason"`
	Path              string           `json:"path"`
	Groups            []PlacementGroup `json:"groups"`
	SegmentMin        int              `json:"segment_min"`
	SegmentMax        string           `json:"segment_max"`
	SegmentRepetition int              `json:"segment_repetition"`
}

// LocateStructure uses the same bounded matcher as evaluation. A placement is
// returned only when the whole original sequence matches and its selected
// occurrence has one unique placement. This read establishes no validation
// verdict and changes no profile, evaluator pin, or evidence.
func LocateStructure(nodes []Node, segments []hl7.Segment, target int) Placement {
	unknown := func(reason string) Placement {
		return Placement{State: "unknown", Reason: reason, Groups: []PlacementGroup{}}
	}
	if target < 0 || target >= len(segments) {
		return unknown("The selected segment occurrence is unavailable.")
	}
	m := matcher{segments: segments, memo: map[matchKey]map[int]bool{}}
	if !m.sequence(nodes, 0)[len(segments)] {
		return unknown("The message does not establish one complete matching reference structure.")
	}
	found := map[string]Placement{}
	var sequence func([]Node, int, int, string, []PlacementGroup)
	var node func(*Node, int, int, string, []PlacementGroup)
	sequence = func(list []Node, start, end int, path string, groups []PlacementGroup) {
		if !m.spend() || len(list) == 0 {
			return
		}
		first := &list[0]
		for finish := range m.node(first, start) {
			if finish > end || !m.sequence(list[1:], finish)[end] {
				continue
			}
			if target >= start && target < finish {
				node(first, start, finish, path, groups)
			} else if target >= finish {
				sequence(list[1:], finish, end, path, groups)
			}
		}
	}
	node = func(n *Node, start, end int, path string, groups []PlacementGroup) {
		if !m.spend() {
			return
		}
		if n.Segment != "" {
			if target >= start && target < end {
				key := fmt.Sprintf("%s/%s[%d]", path, n.Name, target-start+1)
				found[key] = Placement{State: "known", Path: key, Groups: append([]PlacementGroup{}, groups...), SegmentMin: n.Min, SegmentMax: n.Max, SegmentRepetition: target - start + 1}
			}
			return
		}
		maximum := len(segments) + 1
		if n.Max != "*" {
			maximum, _ = strconv.Atoi(n.Max)
		}
		maximum = min(maximum, len(segments)+1)
		// suffix consumes exactly the remaining occurrences of this same group.
		var suffix func(int, int) bool
		suffix = func(pos, count int) bool {
			if !m.spend() {
				return false
			}
			if pos == end && count >= n.Min {
				return true
			}
			if count >= maximum {
				return false
			}
			ends := map[int]bool{}
			if n.Choice {
				for i := range n.Children {
					for finish := range m.node(&n.Children[i], pos) {
						ends[finish] = true
					}
				}
			} else {
				ends = m.sequence(n.Children, pos)
			}
			for finish := range ends {
				if finish > pos && finish <= end && suffix(finish, count+1) {
					return true
				}
			}
			return false
		}
		positions := map[int]bool{start: true}
		for count := 1; count <= maximum && len(positions) > 0; count++ {
			next := map[int]bool{}
			for pos := range positions {
				ends := map[int]bool{}
				if n.Choice {
					for i := range n.Children {
						for finish := range m.node(&n.Children[i], pos) {
							ends[finish] = true
						}
					}
				} else {
					ends = m.sequence(n.Children, pos)
				}
				for finish := range ends {
					if !m.spend() {
						return
					}
					if finish <= pos || finish > end {
						continue
					}
					if target >= pos && target < finish && suffix(finish, count) {
						at := append(append([]PlacementGroup{}, groups...), PlacementGroup{Name: n.Name, Occurrence: count, Min: n.Min, Max: n.Max})
						groupPath := fmt.Sprintf("%s/%s[%d]", path, n.Name, count)
						if n.Choice {
							for i := range n.Children {
								if m.node(&n.Children[i], pos)[finish] {
									node(&n.Children[i], pos, finish, groupPath, at)
								}
							}
						} else {
							sequence(n.Children, pos, finish, groupPath, at)
						}
					}
					if target >= finish {
						next[finish] = true
					}
				}
			}
			positions = next
		}
	}
	sequence(nodes, 0, len(segments), "", nil)
	if m.exhausted {
		return unknown("Reference placement exceeds its bounded matching budget.")
	}
	if len(found) != 1 {
		return unknown("The source sequence has unavailable or ambiguous group placement.")
	}
	for _, placement := range found {
		return placement
	}
	return unknown("The selected occurrence has no matched placement.")
}
