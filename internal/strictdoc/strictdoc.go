// Package strictdoc owns the one way a readmit versioned document is read:
// a size bound, a declared schema, required members present, and no member
// the contract does not declare. The separate documents stay separate — this
// is the reader underneath them, and the four refusals are tested here once
// instead of once per contract.
package strictdoc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"strings"
)

// Document states one contract's reading rules and its own refusal
// sentences. Every sentence is the caller's: this package decides which one a
// document has earned, never what it says.
type Document struct {
	MaxBytes int
	Schema   string
	// Required names the members besides schema whose absence refuses the
	// document; an explicit null refuses it the same way.
	Required []string
	// Invalid reports bytes that are not the contract's JSON: not parseable,
	// not an object, or carrying a member the target does not declare.
	Invalid string
	// Unknown, when set, reports only the strict pass's unknown-member
	// refusal, for contracts that name that refusal its own sentence. When
	// empty, Invalid reports it.
	Unknown string
	// TooLarge reports data beyond MaxBytes.
	TooLarge string
	// MustDeclare reports a schema member that is missing, null, or not the
	// contract's schema.
	MustDeclare string
	// Unsupported, when set, is returned when the schema member names another
	// version of the contract. A later release bumps the version precisely
	// because it adds members, so a version mismatch must read as unsupported
	// — never as invalid — and callers match it with errors.Is. When nil, a
	// mismatch reports MustDeclare.
	Unsupported error
	// Requires reports a required member besides schema that is missing or
	// null.
	Requires string
}

// Decode reads data into target through those rules. Target must be a
// pointer whose struct members all declare json tags; nothing else about it
// is this package's business.
func (d Document) Decode(data []byte, target any) error {
	if len(data) > d.MaxBytes {
		return errors.New(d.TooLarge)
	}
	var members map[string]jsontext.Value
	if err := json.Unmarshal(data, &members); err != nil {
		return errors.New(d.Invalid)
	}
	raw, ok := members["schema"]
	if !ok {
		return errors.New(d.MustDeclare)
	}
	var schema string
	if err := json.Unmarshal(raw, &schema); err != nil {
		return errors.New(d.Invalid)
	}
	if schema != d.Schema {
		if d.Unsupported != nil {
			return d.Unsupported
		}
		return errors.New(d.MustDeclare)
	}
	for _, name := range d.Required {
		value, ok := members[name]
		if !ok || strings.TrimSpace(string(value)) == "null" {
			return errors.New(d.Requires)
		}
	}
	if err := json.Unmarshal(data, target, json.RejectUnknownMembers(true)); err != nil {
		var semantic *json.SemanticError
		if d.Unknown != "" && errors.As(err, &semantic) && errors.Is(semantic.Err, json.ErrUnknownName) {
			return errors.New(d.Unknown)
		}
		return errors.New(d.Invalid)
	}
	return nil
}
