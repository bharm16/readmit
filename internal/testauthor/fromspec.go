package testauthor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/testrunner"
)

// Clause is one member of a saved spec a draft cannot hold, named by the
// stage it belongs to, with the reason. The spec keeps it exactly as written;
// only a draft cannot answer it, so an editor shows it read-only rather than
// dropping it.
type Clause struct {
	Stage  string `json:"stage"`
	Reason string `json:"reason"`
}

// FromSpec is the draft a readmit-test/v1 spec was generated from: the
// inverse of Generate. Identity is the verified identity of the case the spec
// names, or empty when it could not be verified.
//
// A member the draft vocabulary cannot hold — a reference that is not one
// entry of the workspace, text with a line break a person could not type into
// one field — is left unanswered in the draft and reported as a clause, never
// shortened or rewritten. Every assertion of the three operators maps to one
// expectation, in order, with every member it declares.
func FromSpec(spec testrunner.Spec, identity string) (Draft, []Clause, error) {
	if err := spec.Validate(); err != nil {
		return Draft{}, nil, err
	}
	if identity != "" && !identityPattern.MatchString(identity) {
		return Draft{}, nil, errors.New("a draft names the verified identity of the case it is authored against")
	}
	clauses := []Clause{}
	entry := func(stage, value, reason string) string {
		if artifactpath.EntryName(value) != nil || !printable(value, MaxEntryBytes) {
			clauses = append(clauses, Clause{Stage: stage, Reason: reason})
			return ""
		}
		return value
	}
	text := func(stage, value string, limit int, reason string) string {
		if !printable(value, limit) {
			clauses = append(clauses, Clause{Stage: stage, Reason: reason})
			return ""
		}
		return value
	}
	draft := Draft{
		Schema:       Schema,
		Case:         Evidence{Entry: entry(StageCase, spec.Input.Case, "the case is named by a path rather than one entry of the project"), Identity: identity},
		Name:         text(StageName, spec.Name, MaxNameBytes, "the name holds characters a name field cannot hold"),
		Messages:     slices.Clone(spec.Input.Messages),
		Target:       entry(StageTarget, spec.Target, "the target is named by a path rather than one entry of the project"),
		Boundary:     spec.Observation.Boundary,
		Reset:        text(StageReset, spec.Setup.ResetInstructions, MaxResetBytes, "the reset instructions hold line breaks or other characters one field cannot hold"),
		Expectations: make([]Expectation, 0, len(spec.Assertions)),
	}
	if spec.Observation.Boundary == testrunner.LedgerBoundary {
		draft.Observation = entry(StageObservation, spec.Observation.Path, "the observation is named by a path rather than one entry of the project")
	}
	for _, assertion := range spec.Assertions {
		expectation := Expectation{ID: assertion.ID, Operator: assertion.Operator, Message: assertion.Message, Selector: assertion.Selector}
		switch assertion.Operator {
		case LedgerCount:
			count := *assertion.Expected.Count
			expectation.Count = &count
		case LedgerEquals:
			records := slices.Clone(*assertion.Expected.Records)
			expectation.Records = &records
		case ACKFieldEquals:
			field := *assertion.Expected.Field
			expectation.Field = &field
		}
		draft.Expectations = append(draft.Expectations, expectation)
	}
	return draft, clauses, nil
}

// ExportTo writes exactly the bytes of a spec to a new file at destination,
// wherever a person chose it, and reads them back. It never serializes through
// a draft or changes a reference: what is written is the saved contract.
func ExportTo(data []byte, destination string) (Saved, error) {
	if _, err := testrunner.DecodeSpec(data); err != nil {
		return Saved{}, err
	}
	path, err := artifactpath.Destination(destination)
	if err != nil {
		return Saved{}, errors.New("a test spec must be written outside retained evidence")
	}
	if err := write(path, data); err != nil {
		return Saved{}, err
	}
	written, err := os.ReadFile(path)
	if err != nil || string(written) != string(data) {
		return Saved{}, errors.New("the test spec could not be verified after writing")
	}
	digest := sha256.Sum256(written)
	return Saved{Output: path, Identity: hex.EncodeToString(digest[:])}, nil
}
