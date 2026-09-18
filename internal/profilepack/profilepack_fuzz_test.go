package profilepack_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/profilepack"
)

// FuzzProfilePackDocument exercises the pack reader: the strict decode that
// refuses unknown members, the nested presence-then-strict decoders, and every
// bound an accepted pack is held to.
//
// No input may panic, and no bytes at all may produce a pack that answers
// outside the closed sets, passes a level it did not declare supported,
// claims structural or workflow support this contract carries no content for,
// or hands a field name to a combination whose labels are not supported. That
// is ADR-0009 stated as a property: if the reader cannot be talked into any
// of them, an unsupported combination can never acquire a passing verdict.
func FuzzProfilePackDocument(f *testing.F) {
	positive, err := os.ReadFile(fixtureRoot + "profile-pack.json")
	if err != nil {
		f.Fatalf("read fixture: %v", err)
	}
	negative, err := os.ReadFile(fixtureRoot + "profile-pack-refused.json")
	if err != nil {
		f.Fatalf("read fixture: %v", err)
	}
	fixture := string(positive)
	for _, seed := range []string{
		fixture,
		string(negative),
		strings.Replace(fixture, `"structural": "unsupported"`, `"structural": "supported"`, 1),
		strings.Replace(fixture, `"family": "ADT"`, `"family": "SIU"`, 1),
		strings.Replace(fixture, `"hl7_version": "2.4"`, `"hl7_version": "2.9"`, 1),
		strings.Replace(fixture, `"labels": "untested"`, `"labels": "supported"`, 1),
		strings.Replace(fixture, `"status": "approved"`, `"status": "pending"`, 1),
		strings.Replace(fixture, `"coverage": [`, `"command": "sh -c true", "coverage": [`, 1),
		strings.Replace(fixture, "readmit-profile-pack/v1", "readmit-profile-pack/v2", 1),
		`{"schema":"readmit-profile-pack/v1"}`,
		`{}`,
	} {
		f.Add([]byte(seed))
	}
	versions := profilepack.HL7Versions()
	families := profilepack.Families()
	levels := []profilepack.Level{profilepack.LevelParse, profilepack.LevelLabels, profilepack.LevelStructural, profilepack.LevelWorkflow}
	f.Fuzz(func(t *testing.T, data []byte) {
		pack, err := profilepack.Decode(data)
		if err != nil {
			return
		}
		if pack.Schema != profilepack.Schema || len(pack.Coverage) == 0 {
			t.Fatalf("accepted %+v", pack)
		}
		if err := pack.Satisfies(pack.Identity); err != nil {
			t.Fatalf("a pack does not satisfy its own identity: %v", err)
		}
		if status := pack.Provenance.RightsReview.Status; status == profilepack.ReviewApproved && pack.Provenance.RightsReview.Reference == "" {
			t.Fatal("accepted an approved rights review with no record")
		} else if status != profilepack.ReviewApproved && status != profilepack.ReviewPending {
			t.Fatalf("accepted the rights review status %q", status)
		}
		labelled := map[string]bool{}
		for _, labels := range pack.Labels {
			if !slices.Contains(versions, labels.HL7Version) || labelled[labels.HL7Version] {
				t.Fatalf("accepted labels for %q", labels.HL7Version)
			}
			labelled[labels.HL7Version] = true
		}
		seen := map[profilepack.Combination]bool{}
		for _, coverage := range pack.Coverage {
			combination := profilepack.Combination{Version: coverage.HL7Version, Family: coverage.Family}
			if !slices.Contains(versions, coverage.HL7Version) || !slices.Contains(families, coverage.Family) || seen[combination] {
				t.Fatalf("accepted the coverage %+v", coverage)
			}
			seen[combination] = true
			if coverage.Structural == profilepack.Supported || coverage.Workflow == profilepack.Supported {
				t.Fatalf("accepted a claim this contract carries no content for: %+v", coverage)
			}
			if coverage.Parse != profilepack.Supported && coverage.Labels == profilepack.Supported {
				t.Fatalf("accepted labels without parse: %+v", coverage)
			}
			if coverage.Labels == profilepack.Supported && !labelled[coverage.HL7Version] {
				t.Fatalf("accepted labels support with no labels content: %+v", coverage)
			}
			// Each declared level answers exactly as declared, and passes only
			// when declared supported.
			for _, level := range levels {
				outcome := pack.Support(coverage.HL7Version, coverage.Family, level)
				if outcome == profilepack.OutcomeUnknown {
					t.Fatalf("a declared combination answered unknown for %q: %+v", level, coverage)
				}
				if outcome.Passing() != (outcome == profilepack.OutcomeSupported) {
					t.Fatalf("outcome %q passing %v", outcome, outcome.Passing())
				}
			}
			label := pack.Label(coverage.HL7Version, coverage.Family, "MSH", 9)
			if label.Name != "" && !label.Outcome.Passing() {
				t.Fatalf("a name reached a combination whose labels are %q: %+v", label.Outcome, coverage)
			}
		}
		// Every combination the pack did not declare is unknown at every level.
		for _, version := range versions {
			for _, family := range families {
				if seen[profilepack.Combination{Version: version, Family: family}] {
					continue
				}
				for _, level := range levels {
					if outcome := pack.Support(version, family, level); outcome != profilepack.OutcomeUnknown {
						t.Fatalf("an undeclared combination %s %s answered %q for %q", version, family, outcome, level)
					}
				}
				if label := pack.Label(version, family, "MSH", 9); label != (profilepack.FieldLabel{Outcome: profilepack.OutcomeUnknown}) {
					t.Fatalf("an undeclared combination %s %s answered %+v", version, family, label)
				}
			}
		}
	})
}
