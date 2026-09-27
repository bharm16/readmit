package assertionauthor

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"slices"

	"github.com/bharm16/readmit/internal/assertion"
)

// UnsupportedClause is one clause of an assertion set this release cannot
// evaluate: its identity and operator as written, its exact JSON text, where
// it stood among the set's clauses, and the shared reader's reason. It is
// kept beside a draft so an editor shows it rather than dropping it.
type UnsupportedClause struct {
	ID       string `json:"id"`
	Operator string `json:"operator"`
	Raw      string `json:"raw"`
	Position int    `json:"position"`
	Reason   string `json:"reason"`
}

// ReadLenient reads a readmit-assertion-set/v1 document clause by clause.
// Every clause the shared reader accepts on its own becomes a draft clause,
// in the set's order; every other clause — an operator this release does not
// evaluate, or members its operator does not take — is answered as an
// UnsupportedClause with the reader's reason, never dropped. The document
// itself must still declare the contract, a name and its clauses; one that
// does not is refused whole.
func ReadLenient(data []byte) (Draft, []UnsupportedClause, error) {
	if len(data) > MaxDraftBytes {
		return Draft{}, nil, errors.New("assertion set exceeds its size limit")
	}
	var document struct {
		Schema     *string           `json:"schema"`
		Name       *string           `json:"name"`
		Assertions *[]jsontext.Value `json:"assertions"`
	}
	if err := json.Unmarshal(data, &document, json.RejectUnknownMembers(true)); err != nil {
		return Draft{}, nil, errors.New("invalid assertion set JSON")
	}
	if document.Schema == nil || *document.Schema != assertion.Schema {
		return Draft{}, nil, errors.New("an assertion set must declare " + assertion.Schema)
	}
	if document.Name == nil || document.Assertions == nil {
		return Draft{}, nil, errors.New("an assertion set declares its name and assertions")
	}
	if len(*document.Assertions) > assertion.MaxAssertions {
		return Draft{}, nil, errors.New("an assertion set holds at most 256 assertions")
	}
	draft := Draft{Schema: Schema, Name: *document.Name, Assertions: []Clause{}}
	unsupported := []UnsupportedClause{}
	seen := map[string]bool{}
	for position, raw := range *document.Assertions {
		var named struct {
			ID       string `json:"id"`
			Operator string `json:"operator"`
		}
		_ = json.Unmarshal(raw, &named)
		if named.ID == "" || seen[named.ID] {
			return Draft{}, nil, errors.New("an assertion is named once")
		}
		seen[named.ID] = true
		clause, err := readClause(raw)
		if err != nil {
			reason := err.Error()
			if !slices.Contains(assertion.Operators(), assertion.Operator(named.Operator)) {
				reason = "this release does not evaluate the operator " + quoted(named.Operator)
			}
			unsupported = append(unsupported, UnsupportedClause{ID: named.ID, Operator: named.Operator, Raw: string(raw), Position: position, Reason: reason})
			continue
		}
		draft.Assertions = append(draft.Assertions, clause)
	}
	if err := check(draft); err != nil {
		return Draft{}, nil, err
	}
	return draft, unsupported, nil
}

// readClause reads one clause through the shared reader, as the only clause
// of a set, so it is accepted exactly when a set holding it would be.
func readClause(raw jsontext.Value) (Clause, error) {
	single, err := json.Marshal(struct {
		Schema     string           `json:"schema"`
		Name       string           `json:"name"`
		Assertions []jsontext.Value `json:"assertions"`
	}{Schema: assertion.Schema, Name: "clause", Assertions: []jsontext.Value{raw}})
	if err != nil {
		return Clause{}, errors.New("the clause cannot be read")
	}
	set, err := assertion.Decode(single)
	if err != nil {
		return Clause{}, err
	}
	item := set.Assertions[0]
	return Clause{ID: item.ID, Operator: item.Operator, Subject: item.Subject, When: item.When, Expected: item.Expected}, nil
}

func quoted(value string) string { return "\"" + value + "\"" }
