package testrunner

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"

	"github.com/bharm16/readmit/internal/hl7"
)

// UnmarshalJSON checks the union's member presence before nullable pointers can
// erase an explicitly supplied null or irrelevant member.
func (v *Value) UnmarshalJSON(data []byte) error {
	invalid := errors.New("assertion value requires exactly one non-null typed member")
	var members struct {
		Count   jsontext.Value `json:"count"`
		Records jsontext.Value `json:"records"`
		Field   jsontext.Value `json:"field"`
	}
	if err := json.Unmarshal(data, &members, json.RejectUnknownMembers(true)); err != nil {
		return invalid
	}
	present := 0
	for _, raw := range []jsontext.Value{members.Count, members.Records, members.Field} {
		if len(raw) != 0 {
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return invalid
			}
			present++
		}
	}
	if present != 1 {
		return invalid
	}
	type plainValue Value
	var decoded plainValue
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return invalid
	}
	*v = Value(decoded)
	return nil
}

// UnmarshalJSON keeps JSON null separate from the supported HL7 null state.
// Only present fields carry text; all other states must omit that member.
func (v *FieldValue) UnmarshalJSON(data []byte) error {
	invalid := errors.New("field value requires a supported state and text only when present")
	var members struct {
		State *hl7.State     `json:"state"`
		Text  jsontext.Value `json:"text"`
	}
	if err := json.Unmarshal(data, &members, json.RejectUnknownMembers(true)); err != nil || members.State == nil {
		return invalid
	}
	decoded := FieldValue{State: *members.State}
	switch decoded.State {
	case hl7.Present:
		if len(members.Text) == 0 || bytes.Equal(bytes.TrimSpace(members.Text), []byte("null")) {
			return invalid
		}
		var text string
		if err := json.Unmarshal(members.Text, &text); err != nil {
			return invalid
		}
		decoded.Text = &text
	case hl7.Empty, hl7.Null, hl7.Omitted:
		if len(members.Text) != 0 {
			return invalid
		}
	default:
		return invalid
	}
	*v = decoded
	return nil
}
