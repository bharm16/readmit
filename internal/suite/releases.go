package suite

import (
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/testrunner"
)

const ReleasesSchema = "readmit-suite-releases/v1"

// ReleaseReferences is separate so readmit-suite/v1 keeps its exact member set.
// Paths resolve beside this file; each identity pins the complete local approval.
type ReleaseReferences struct {
	Schema string             `json:"schema"`
	Tests  []ReleaseReference `json:"tests"`
}
type ReleaseReference struct {
	Test     string `json:"test"`
	Release  string `json:"release"`
	Identity string `json:"identity"`
}

func (v *ReleaseReferences) UnmarshalJSON(data []byte) error {
	type plain ReleaseReferences
	return required(data, (*plain)(v), "schema", "tests")
}
func (v *ReleaseReference) UnmarshalJSON(data []byte) error {
	type plain ReleaseReference
	return required(data, (*plain)(v), "test", "release", "identity")
}
func DecodeReleases(data []byte) (ReleaseReferences, error) {
	var refs ReleaseReferences
	if len(data) > MaxBytes || json.Unmarshal(data, &refs, json.RejectUnknownMembers(true)) != nil || refs.Schema != ReleasesSchema || len(refs.Tests) < 1 || len(refs.Tests) > 64 {
		return refs, errors.New("invalid suite release references")
	}
	seen := map[string]bool{}
	for _, r := range refs.Tests {
		b, e := hex.DecodeString(r.Identity)
		if !identifier.MatchString(r.Test) || seen[r.Test] || !local(r.Release) || e != nil || len(b) != 32 || hex.EncodeToString(b) != r.Identity {
			return ReleaseReferences{}, errors.New("invalid or duplicate suite release pin")
		}
		seen[r.Test] = true
	}
	return refs, nil
}
func loadReleases(path string, doc Document) (map[string]expectation.Release, []byte, error) {
	raw, err := read(path, MaxBytes)
	if err != nil {
		return nil, nil, err
	}
	refs, err := DecodeReleases(raw)
	if err != nil {
		return nil, nil, err
	}
	if len(refs.Tests) != len(doc.Tests) {
		return nil, nil, errors.New("every suite test must have exactly one release pin")
	}
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]expectation.Release{}
	for _, ref := range refs.Tests {
		found := false
		for _, test := range doc.Tests {
			if test.ID == ref.Test {
				found = true
			}
		}
		if !found {
			return nil, nil, errors.New("release pin names an undeclared suite test")
		}
		r, err := expectation.Read(artifactpath.JoinReference(filepath.Dir(resolved), ref.Release))
		if err != nil {
			return nil, nil, err
		}
		if r.Identity() != ref.Identity {
			return nil, nil, errors.New("released test changed since it was pinned")
		}
		out[ref.Test] = r
	}
	return out, raw, nil
}

type ReleaseImpact struct {
	Test   string `json:"test"`
	Rows   int    `json:"rows"`
	Pinned string `json:"pinned"`
	State  string `json:"state"`
}
type ImpactReport struct {
	Schema     string                 `json:"schema"`
	From       string                 `json:"from"`
	To         string                 `json:"to"`
	Comparison expectation.Comparison `json:"comparison"`
	Tests      []ReleaseImpact        `json:"tests"`
}

// AssessReleases describes only the explicitly supplied suite. Affected means
// a declaration changed; no execution result is inferred and no pin is moved.
func AssessReleases(path, references string, from, to expectation.Release, show bool) (ImpactReport, error) {
	if from.Validate() != nil || to.Validate() != nil || from.ID != to.ID || to.Parent != from.Identity() {
		return ImpactReport{}, errors.New("impact requires a release and its direct successor")
	}
	parent, _ := from.Baseline.Identity()
	if to.Baseline.Parent != parent || to.Baseline.Revision != from.Baseline.Revision+1 {
		return ImpactReport{}, errors.New("release and baseline histories disagree")
	}
	spec, _ := json.Marshal(to.Baseline.Spec, json.Deterministic(true))
	comparison, err := expectation.Review(to.ID, spec, to.Profiles, &from, show)
	if err != nil {
		return ImpactReport{}, err
	}
	raw, err := read(path, MaxBytes)
	if err != nil {
		return ImpactReport{}, err
	}
	doc, err := Decode(raw)
	if err != nil {
		return ImpactReport{}, err
	}
	releases, _, err := loadReleases(references, doc)
	if err != nil {
		return ImpactReport{}, err
	}
	report := ImpactReport{Schema: "readmit-expectation-impact/v1", From: from.Identity(), To: to.Identity(), Comparison: comparison, Tests: []ReleaseImpact{}}
	for _, test := range doc.Tests {
		r := releases[test.ID]
		state := "unrelated"
		if r.Identity() == report.To {
			state = "current"
		} else if r.Identity() == report.From {
			state = "unaffected"
			if len(comparison.Baseline.Changes) > 0 || len(comparison.Profiles) > 0 {
				state = "affected"
			}
		}
		rows := 0
		for _, table := range doc.Tables {
			if table.ID == test.Table {
				rows = len(table.Rows)
			}
		}
		report.Tests = append(report.Tests, ReleaseImpact{Test: test.ID, Rows: rows, Pinned: r.Identity(), State: state})
	}
	return report, nil
}

// baselineSpec uses the existing complete specification reader and comparison;
// approval never grants a table permission to replace an expected value.
func baselineSpec(raw []byte, r expectation.Release) error {
	spec, err := testrunner.DecodeSpec(raw)
	if err != nil {
		return err
	}
	candidate, _ := json.Marshal(spec, json.Deterministic(true))
	released, _ := json.Marshal(r.Baseline.Spec, json.Deterministic(true))
	if string(candidate) != string(released) {
		return errors.New("suite template differs from its released specification")
	}
	return nil
}
