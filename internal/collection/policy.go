// Package collection defines the declarative policy a generic MLLP receiver
// follows and the record of what that receiver actually collected. It
// acknowledges receipt of bytes only: no member of either contract reports that
// a downstream application processed a message.
package collection

import (
	"encoding/json/v2"
	"errors"
	"regexp"
	"strings"
)

const (
	PolicySchema   = "readmit-receiver-policy/v1"
	MaxPolicyBytes = 64 << 10
	maxTypeValues  = 64
)

// Declared operators. A policy names one of these typed Go operations; it never
// carries an expression, a script, or a code reference.
const (
	FixedCodeOperator    = "original-mode-fixed-code"
	AnyMessageTypeRule   = "any-message-type"
	MessageTypeInRule    = "message-type-in"
	AcceptCode           = "AA"
	ApplicationErrorCode = "AE"
	RejectCode           = "AR"
)

// AckRule names the acknowledgement operation and the literal MSA-1 code it
// returns. The code is a transport acknowledgement of receipt, never a claim
// that the receiver applied the message.
type AckRule struct {
	Operator string `json:"operator"`
	Code     string `json:"code"`
}

// MessageTypeRule bounds the message types this receiver will acknowledge.
// Values are declared MSH-9 types, optionally with a trigger event.
type MessageTypeRule struct {
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
}

type Policy struct {
	Schema               string          `json:"schema"`
	Name                 string          `json:"name"`
	SourceLabel          string          `json:"source_label"`
	Acknowledgement      AckRule         `json:"acknowledgement"`
	AcceptedMessageTypes MessageTypeRule `json:"accepted_message_types"`
}

var (
	labelPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	messageTypePattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{2}(\^[A-Z0-9]{1,4})?$`)
)

// UnmarshalJSON requires every member explicitly, so an omitted acknowledgement
// rule cannot decode into a silently permissive zero value.
func (p *Policy) UnmarshalJSON(data []byte) error {
	var required struct {
		Schema               *string          `json:"schema"`
		Name                 *string          `json:"name"`
		SourceLabel          *string          `json:"source_label"`
		Acknowledgement      *AckRule         `json:"acknowledgement"`
		AcceptedMessageTypes *MessageTypeRule `json:"accepted_message_types"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Schema == nil || required.Name == nil || required.SourceLabel == nil || required.Acknowledgement == nil || required.AcceptedMessageTypes == nil {
		return errors.New("receiver policy requires schema, name, source label, acknowledgement, and accepted message types")
	}
	type plainPolicy Policy
	var value plainPolicy
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid receiver policy JSON")
	}
	*p = Policy(value)
	return nil
}

func (r *AckRule) UnmarshalJSON(data []byte) error {
	var required struct {
		Operator *string `json:"operator"`
		Code     *string `json:"code"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Operator == nil || required.Code == nil {
		return errors.New("acknowledgement rule requires an operator and a code")
	}
	*r = AckRule{Operator: *required.Operator, Code: *required.Code}
	return nil
}

func (r *MessageTypeRule) UnmarshalJSON(data []byte) error {
	var required struct {
		Operator *string   `json:"operator"`
		Values   *[]string `json:"values"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Operator == nil || required.Values == nil {
		return errors.New("message type rule requires an operator and an explicit value array")
	}
	*r = MessageTypeRule{Operator: *required.Operator, Values: *required.Values}
	return nil
}

func (p Policy) Validate() error {
	if p.Schema != PolicySchema {
		return errors.New("unsupported receiver policy schema version")
	}
	if !labelPattern.MatchString(p.Name) || !labelPattern.MatchString(p.SourceLabel) {
		return errors.New("receiver policy name and source label must be short printable identifiers")
	}
	if p.Acknowledgement.Operator != FixedCodeOperator {
		return errors.New("unsupported acknowledgement operator")
	}
	if p.Acknowledgement.Code != AcceptCode && p.Acknowledgement.Code != ApplicationErrorCode && p.Acknowledgement.Code != RejectCode {
		return errors.New("acknowledgement code must be AA, AE, or AR")
	}
	switch p.AcceptedMessageTypes.Operator {
	case AnyMessageTypeRule:
		if len(p.AcceptedMessageTypes.Values) != 0 {
			return errors.New("any-message-type declares no values")
		}
	case MessageTypeInRule:
		if len(p.AcceptedMessageTypes.Values) == 0 || len(p.AcceptedMessageTypes.Values) > maxTypeValues {
			return errors.New("message-type-in requires between 1 and 64 declared message types")
		}
		seen := make(map[string]bool, len(p.AcceptedMessageTypes.Values))
		for _, value := range p.AcceptedMessageTypes.Values {
			if !messageTypePattern.MatchString(value) || seen[value] {
				return errors.New("declared message types must be distinct TYPE or TYPE^TRIGGER values")
			}
			seen[value] = true
		}
	default:
		return errors.New("unsupported message type operator")
	}
	return nil
}

// Accepts reports whether the declared MSH-9 type and trigger are inside the
// policy's bounds. A declaration without a trigger matches the type alone.
func (p Policy) Accepts(messageType, trigger string) bool {
	if p.AcceptedMessageTypes.Operator == AnyMessageTypeRule {
		return true
	}
	for _, value := range p.AcceptedMessageTypes.Values {
		if strings.Contains(value, "^") {
			if value == messageType+"^"+trigger {
				return true
			}
			continue
		}
		if value == messageType {
			return true
		}
	}
	return false
}

func DecodePolicy(data []byte) (Policy, error) {
	var policy Policy
	if len(data) > MaxPolicyBytes {
		return policy, errors.New("receiver policy exceeds size limit")
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		// Diagnostics must not echo the declared file's contents.
		return policy, errors.New("invalid receiver policy JSON")
	}
	return policy, policy.Validate()
}

func EncodePolicy(policy Policy) ([]byte, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(policy, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode receiver policy")
	}
	return data, nil
}
