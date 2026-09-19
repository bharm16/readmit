package assertion

import "testing"

// operators restates the closed set this contract carries, so the named
// constants and the compatibility table the reader dispatches on cannot drift
// apart unnoticed. A seventeenth operator is a deliberate change to this list,
// to the committed fixture that exercises every operator, to the tables the
// fuzz target restates, and to docs/assertions.md.
var operators = []Operator{
	FieldEquals, FieldNotEquals, FieldState, TextMatches, NumericRange,
	NumericTolerance, DateWindow, ValuesEqual, RecordCount, RecordsUnique,
	RecordsContain, RecordsOrdered, RecordMultiplicity, RecordsAbsent,
	RecordKeyMatches, RecordsChanged,
}

var subjectKinds = []subjectKind{subjectField, subjectPair, subjectCollection, subjectEach, subjectTransition}

var expectedMembers = []expectedMember{
	expectField, expectState, expectPattern, expectRange, expectTolerance,
	expectWindow, expectHolds, expectCount, expectKeys, expectMultiplicity,
	expectChange,
}

func TestEveryOperatorIsBoundToOneSubjectAndOneExpectation(t *testing.T) {
	if len(compatibility) != len(operators) {
		t.Fatalf("the compatibility table binds %d operators and %d are named", len(compatibility), len(operators))
	}
	for _, operator := range operators {
		pairing, ok := compatibility[operator]
		if !ok {
			t.Fatalf("the operator %q is named and not bound", operator)
		}
		if !contains(subjectKinds, pairing.subject) {
			t.Errorf("the operator %q reads the subject %q, which is not one this contract carries", operator, pairing.subject)
		}
		if !contains(expectedMembers, pairing.expected) {
			t.Errorf("the operator %q takes the expectation %q, which is not one this contract carries", operator, pairing.expected)
		}
	}
}

// Every subject kind and every expectation member belongs to some operator, so
// a union member nothing reads cannot sit in the contract unnoticed.
func TestEverySubjectAndExpectationBelongsToAnOperator(t *testing.T) {
	subjects := make(map[subjectKind]bool, len(subjectKinds))
	expectations := make(map[expectedMember]bool, len(expectedMembers))
	for _, pairing := range compatibility {
		subjects[pairing.subject] = true
		expectations[pairing.expected] = true
	}
	for _, kind := range subjectKinds {
		if !subjects[kind] {
			t.Errorf("no operator reads the subject %q", kind)
		}
	}
	for _, member := range expectedMembers {
		if !expectations[member] {
			t.Errorf("no operator takes the expectation %q", member)
		}
	}
}

func contains[T comparable](values []T, wanted T) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
