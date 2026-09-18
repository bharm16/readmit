// Package collection defines the declarative policy a generic MLLP receiver
// follows and the record of what that receiver actually collected. It
// acknowledges receipt of bytes only: no member of either contract reports that
// a downstream application processed a message.
package collection

import (
	"encoding/json/v2"
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"
)

const (
	PolicySchemaV1 = "readmit-receiver-policy/v1"
	PolicySchema   = "readmit-receiver-policy/v2"
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

// Enhanced acknowledgement operators, added by readmit-receiver-policy/v2.
// A v1 policy carries no enhanced rule and means EnhancedUnsupported.
const (
	EnhancedUnsupported = "unsupported"
	EnhancedFixedCodes  = "enhanced-mode-fixed-codes"
)

// Accept-stage codes. MSH-15 asks for a commit acknowledgement, whose MSA-1 is
// one of these. Commit-accept is not application-accept: CA states that bytes
// were committed for later processing, never that processing happened.
const (
	CommitAcceptCode = "CA"
	CommitErrorCode  = "CE"
	CommitRejectCode = "CR"
)

// Where an application-stage acknowledgement is sent. Enhanced acknowledgements
// do not all use one socket, so the destination is declared, never assumed.
const (
	SameConnection   = "same-connection"
	SeparateEndpoint = "separate-endpoint"
	NoDestination    = "none"
)

// Acknowledgement modes, as the record names them.
const (
	OriginalMode = "original"
	EnhancedMode = "enhanced"
)

// The two acknowledgement stages. Each has its own MSA-1 vocabulary and the two
// never merge, so a commit acceptance can never be read as an application
// result. StageCode is the single place that knows which codes belong where.
const (
	AcceptStage      = "accept"
	ApplicationStage = "application"
)

// StageCode reports whether code belongs to the named acknowledgement stage.
func StageCode(stage, code string) bool {
	if stage == AcceptStage {
		return code == CommitAcceptCode || code == CommitErrorCode || code == CommitRejectCode
	}
	return code == AcceptCode || code == ApplicationErrorCode || code == RejectCode
}

// The four declared MSH-15/MSH-16 conditions. Any other populated value is an
// acknowledgement mode this receiver does not support, never a silent accept.
const (
	Always    = "AL"
	Never     = "NE"
	OnError   = "ER"
	OnSuccess = "SU"
)

// ValidCondition reports whether a populated MSH-15/MSH-16 value is one of the
// four declared conditions.
func ValidCondition(value string) bool {
	return value == Always || value == Never || value == OnError || value == OnSuccess
}

// Requested reports whether a declared condition asks for this stage's
// acknowledgement, given whether the declared code for that stage is a success.
// An absent, empty, or unrecognized condition never asks for one.
func Requested(condition string, success bool) bool {
	switch condition {
	case Always:
		return true
	case OnError:
		return !success
	case OnSuccess:
		return success
	}
	return false
}

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

// EnhancedRule declares how the receiver answers a sender that asked for
// enhanced acknowledgement mode. The accept and application stages carry their
// own code vocabularies and the application stage names its own destination, so
// a commit acknowledgement can never be recorded as an application result.
type EnhancedRule struct {
	Operator            string `json:"operator"`
	AcceptCode          string `json:"accept_code"`
	ApplicationCode     string `json:"application_code"`
	ApplicationDelivery string `json:"application_delivery"`
	ApplicationEndpoint string `json:"application_endpoint"`
	ApprovedTransport   bool   `json:"approved_transport"`
}

type Policy struct {
	Schema               string          `json:"schema"`
	Name                 string          `json:"name"`
	SourceLabel          string          `json:"source_label"`
	Acknowledgement      AckRule         `json:"acknowledgement"`
	AcceptedMessageTypes MessageTypeRule `json:"accepted_message_types"`
	// Enhanced exists only in readmit-receiver-policy/v2. A v1 policy leaves it
	// absent, which keeps v1's member set frozen and its bytes unchanged.
	Enhanced *EnhancedRule `json:"enhanced_acknowledgement,omitzero"`
}

var (
	labelPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	messageTypePattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{2}(\^[A-Z0-9]{1,4})?$`)
)

// UnmarshalJSON requires every member explicitly, so an omitted acknowledgement
// rule cannot decode into a silently permissive zero value. The v2-only
// enhanced rule is required in v2 and refused in v1, including as an explicit
// null, which a typed decode alone would accept as an absent member.
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
	var members map[string]any
	if err := json.Unmarshal(data, &members); err != nil {
		return errors.New("invalid receiver policy JSON")
	}
	_, declared := members["enhanced_acknowledgement"]
	if declared != (*required.Schema == PolicySchema) {
		return errors.New("only readmit-receiver-policy/v2 declares an enhanced acknowledgement rule")
	}
	type plainPolicy Policy
	var value plainPolicy
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid receiver policy JSON")
	}
	if declared && value.Enhanced == nil {
		return errors.New("the enhanced acknowledgement rule cannot be null")
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

func (r *EnhancedRule) UnmarshalJSON(data []byte) error {
	var required struct {
		Operator            *string `json:"operator"`
		AcceptCode          *string `json:"accept_code"`
		ApplicationCode     *string `json:"application_code"`
		ApplicationDelivery *string `json:"application_delivery"`
		ApplicationEndpoint *string `json:"application_endpoint"`
		ApprovedTransport   *bool   `json:"approved_transport"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Operator == nil || required.AcceptCode == nil || required.ApplicationCode == nil || required.ApplicationDelivery == nil || required.ApplicationEndpoint == nil || required.ApprovedTransport == nil {
		return errors.New("enhanced acknowledgement rule requires an operator, both stage codes, a delivery, an endpoint, and an approval")
	}
	// A nested rule decodes itself, so it applies its own unknown-member
	// refusal rather than inheriting the enclosing document's.
	type plainRule EnhancedRule
	var value plainRule
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid enhanced acknowledgement rule")
	}
	*r = EnhancedRule(value)
	return nil
}

func (p Policy) Validate() error {
	if p.Schema != PolicySchemaV1 && p.Schema != PolicySchema {
		return errors.New("unsupported receiver policy schema version")
	}
	if (p.Enhanced != nil) != (p.Schema == PolicySchema) {
		return errors.New("only readmit-receiver-policy/v2 declares an enhanced acknowledgement rule")
	}
	if !labelPattern.MatchString(p.Name) || !labelPattern.MatchString(p.SourceLabel) {
		return errors.New("receiver policy name and source label must be short printable identifiers")
	}
	if p.Acknowledgement.Operator != FixedCodeOperator {
		return errors.New("unsupported acknowledgement operator")
	}
	if !StageCode(ApplicationStage, p.Acknowledgement.Code) {
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
	if p.Enhanced == nil {
		return nil
	}
	return p.Enhanced.validate()
}

func (r EnhancedRule) validate() error {
	switch r.Operator {
	case EnhancedUnsupported:
		if r.AcceptCode != "" || r.ApplicationCode != "" || r.ApplicationDelivery != "" || r.ApplicationEndpoint != "" || r.ApprovedTransport {
			return errors.New("an unsupported enhanced acknowledgement rule declares no codes, delivery, endpoint, or approval")
		}
		return nil
	case EnhancedFixedCodes:
	default:
		return errors.New("unsupported enhanced acknowledgement operator")
	}
	// The stages keep separate vocabularies in both directions, so a commit
	// code can never be declared as an application result and the reverse.
	if !StageCode(AcceptStage, r.AcceptCode) {
		return errors.New("enhanced accept code must be CA, CE, or CR")
	}
	if !StageCode(ApplicationStage, r.ApplicationCode) {
		return errors.New("enhanced application code must be AA, AE, or AR")
	}
	switch r.ApplicationDelivery {
	case SameConnection:
		if r.ApplicationEndpoint != "" || r.ApprovedTransport {
			return errors.New("same-connection delivery declares no application endpoint or transport approval")
		}
		return nil
	case SeparateEndpoint:
		return validEndpoint(r.ApplicationEndpoint, r.ApprovedTransport)
	}
	return errors.New("application delivery must be same-connection or separate-endpoint")
}

// validEndpoint mirrors the replay target's outbound address policy: an explicit
// host and numeric port, and a name or nonloopback address only with declared
// approval. Names are never resolved to decide whether approval is required.
func validEndpoint(address string, approved bool) error {
	host, port, err := net.SplitHostPort(address)
	number, portErr := strconv.Atoi(port)
	if err != nil || host == "" || len(host) > 253 || strings.ContainsAny(host, " /%\\") || portErr != nil || number < 1 || number > 65535 {
		return errors.New("a separate application endpoint requires an explicit host and numeric port")
	}
	for _, r := range address {
		if r < 33 || r > 126 {
			return errors.New("application endpoint must contain printable ASCII without spaces")
		}
	}
	if ip := net.ParseIP(host); (ip == nil || !ip.IsLoopback()) && !approved {
		return errors.New("a nonloopback or named application endpoint requires approved_transport true")
	}
	return nil
}

// SupportsEnhanced reports whether this policy answers a sender that asked for
// enhanced acknowledgement mode. A v1 policy and an explicit unsupported rule
// both refuse, with a named reason rather than a guessed reply.
func (p Policy) SupportsEnhanced() bool {
	return p.Enhanced != nil && p.Enhanced.Operator == EnhancedFixedCodes
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
