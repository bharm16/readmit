package profileeval_test

import (
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/profileeval"
	"testing"
)

func TestReferencePlacementNamesActualRepeatedGroupsAndRefusesIncompleteSequences(t *testing.T) {
	nodes := []profileeval.Node{{Name: "header", Segment: "MSH", Min: 1, Max: "1"}, {Name: "PATIENT", Min: 1, Max: "*", Children: []profileeval.Node{{Name: "patient", Segment: "PID", Min: 1, Max: "1"}, {Name: "visit", Segment: "PV1", Min: 0, Max: "1"}}}}
	segments := []hl7.Segment{{ID: "MSH"}, {ID: "PID"}, {ID: "PV1"}, {ID: "PID"}, {ID: "PV1"}}
	got := profileeval.LocateStructure(nodes, segments, 4)
	if got.State != "known" || len(got.Groups) != 1 || got.Groups[0].Name != "PATIENT" || got.Groups[0].Occurrence != 2 || got.SegmentMin != 0 || got.SegmentMax != "1" {
		t.Fatalf("wrong occurrence placement: %+v", got)
	}
	segments = append(segments, hl7.Segment{ID: "ZZZ"})
	if unknown := profileeval.LocateStructure(nodes, segments, 4); unknown.State != "unknown" {
		t.Fatalf("incomplete sequence guessed placement: %+v", unknown)
	}
}
