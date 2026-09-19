package profileversion

import (
	"cmp"
	"slices"
)

// Impact is what one comparison says about one saved test. The set is closed
// and every value is a statement about a **contract**, never about a result:
// this package evaluates no message against a profile, so it cannot know what a
// changed rule would do to an assertion, and it does not pretend to.
type Impact string

const (
	// ImpactAffected: the saved test pins the version the comparison starts
	// from and the comparison found at least one difference, so the contract it
	// was written against is the one that changed. It stays on that version
	// until somebody upgrades it.
	ImpactAffected Impact = "affected"
	// ImpactUnaffected: the saved test pins the version the comparison starts
	// from and the comparison found no difference at all. The later version
	// declares the same rules, so nothing about this test's contract changed
	// and saying it was affected would be a claim the comparison did not make.
	ImpactUnaffected Impact = "unaffected"
	// ImpactCurrent: the saved test already pins the version the comparison
	// ends at, so there is nothing to upgrade.
	ImpactCurrent Impact = "current"
	// ImpactUnrelated: the saved test pins a version of this profile that this
	// comparison names neither side of. Nothing here says anything about it,
	// rather than assuming the change reaches it.
	ImpactUnrelated Impact = "unrelated"
)

// AssessedTest is one saved test with what the comparison says about it. The
// pin is repeated exactly as the index records it, checksum and all, because
// reading an assessment must never require trusting that something moved it.
type AssessedTest struct {
	Test   string `json:"test"`
	Case   string `json:"case"`
	Pinned Pin    `json:"pinned"`
	Impact Impact `json:"impact"`
}

// Assessment is one comparison read together with the saved tests that pin the
// profile it compares: what changed, and which saved tests and cases were
// written against the version it changed from.
//
// The comparison is carried rather than restated, so the changes an assessment
// reports and the changes it was built from cannot drift apart.
//
// It is an answer, not a document, and it is not a verdict. "Affected" says the
// contract a saved test was written against changed. It does not say the test
// would now fail, or now pass: no message is evaluated against a local profile
// in this release, and a report that implied a result would be claiming
// something nothing in the product could stand behind.
type Assessment struct {
	Comparison
	Tests []AssessedTest `json:"tests"`
}

// Assess reports which saved tests the comparison reaches.
//
// Only references to the profile the comparison names are read. A saved test
// that pins another profile is not in the report at all, so versioning one
// team's interface contract says nothing about another's.
//
// The index is held to its whole contract first: an assessment is never built
// over references a reader would have refused, so a report cannot say something
// about a saved test recorded twice or pinned to a version no profile could
// carry.
//
// Nothing is moved. The pin an index records is what the assessment repeats,
// and a saved test reported as affected is still pinned to the version it was
// written against afterwards: an upgrade is Upgrade, named by a person.
func Assess(comparison Comparison, references References) (Assessment, error) {
	if err := references.Validate(); err != nil {
		return Assessment{}, err
	}
	assessment := Assessment{Comparison: comparison, Tests: []AssessedTest{}}
	assessment.Changes = slices.Clone(comparison.Changes)
	if assessment.Changes == nil {
		assessment.Changes = []Change{}
	}
	for _, reference := range references.Tests {
		if reference.Pinned.ID != comparison.Profile {
			continue
		}
		assessment.Tests = append(assessment.Tests, AssessedTest{
			Test:   reference.Test,
			Case:   reference.Case,
			Pinned: reference.Pinned,
			Impact: impact(reference.Pinned.Version, assessment.Comparison),
		})
	}
	slices.SortFunc(assessment.Tests, func(a, b AssessedTest) int { return cmp.Compare(a.Test, b.Test) })
	return assessment, nil
}

func impact(pinned string, comparison Comparison) Impact {
	switch pinned {
	case comparison.From:
		if len(comparison.Changes) == 0 {
			return ImpactUnaffected
		}
		return ImpactAffected
	case comparison.To:
		return ImpactCurrent
	default:
		return ImpactUnrelated
	}
}

// Affected is the saved tests the comparison reaches, in the order the
// assessment reports them. It is the list an operator acts on: every one of
// them was written against the version that changed and none of them moves
// until it is upgraded by name.
func (a Assessment) Affected() []AssessedTest {
	affected := make([]AssessedTest, 0, len(a.Tests))
	for _, test := range a.Tests {
		if test.Impact == ImpactAffected {
			affected = append(affected, test)
		}
	}
	return affected
}
