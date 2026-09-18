// Package sendpolicy decides whether readmit may send to one configured
// destination, and refuses a listening socket that would accept connections
// from beyond this machine.
//
// A recorded classification is a claim, never an authorization: labelling an
// endpoint nonproduction is not proof that the address is safe to send to. A
// decision therefore checks what the configured address actually resolves to at
// the moment of the send, against destinations an operator approved in an
// explicitly selected document, and denies everything it cannot name. A class
// nobody recorded, a name that does not resolve, a name that resolves to
// several addresses and an address no approved destination contains are each
// denied rather than allowed with a warning.
//
// One rule has one implementation. A preview, a connectivity diagnosis and a
// send ask this package the same question, so a passing check cannot predict an
// answer the send path does not give.
//
// A decision is reached before any byte leaves, and that bounds what it can
// promise: cancellation cannot retract bytes already sent, and neither can a
// policy. Nothing here is a kill switch.
package sendpolicy

import (
	"encoding/json/v2"
	"errors"
	"net/netip"
)

// PolicySchema is the contract an approved-destination document declares.
const PolicySchema = "readmit-send-policy/v1"

const (
	// MaxPolicyBytes bounds the document a command reads before decoding it.
	MaxPolicyBytes = 64 << 10
	// maxDestinations bounds the approved set. A policy nobody can read is not
	// a control, and an unbounded list is refused rather than truncated.
	maxDestinations = 64
)

// Policy is the set of destinations an operator approved for sending. It is
// explicitly selected on the command line, never discovered: readmit has no
// default approved destination, no implicit policy file and no environment
// variable that supplies one.
type Policy struct {
	Schema string `json:"schema"`
	// ApprovedDestinations holds CIDR prefixes in canonical masked form. A
	// prefix carrying bits outside its own mask is refused rather than masked
	// on the operator's behalf: readmit does not decide what an ambiguous
	// range was meant to cover.
	ApprovedDestinations []string `json:"approved_destinations"`
}

// DecodePolicy reads one approved-destination document exactly as written.
// Unknown members are refused, so a policy authored against a later contract is
// never read as though this one had always allowed it.
func DecodePolicy(data []byte) (Policy, error) {
	var policy Policy
	if err := json.Unmarshal(data, &policy, json.RejectUnknownMembers(true)); err != nil {
		return Policy{}, errors.New("invalid approved-destination policy JSON")
	}
	if policy.Schema != PolicySchema {
		return Policy{}, errors.New("an approved-destination policy must declare " + PolicySchema)
	}
	if len(policy.ApprovedDestinations) == 0 || len(policy.ApprovedDestinations) > maxDestinations {
		return Policy{}, errors.New("an approved-destination policy declares between 1 and 64 approved destinations")
	}
	declared := make(map[string]bool, len(policy.ApprovedDestinations))
	for _, destination := range policy.ApprovedDestinations {
		if _, err := parsePrefix(destination); err != nil {
			return Policy{}, err
		}
		if declared[destination] {
			return Policy{}, errors.New("an approved destination is declared twice")
		}
		declared[destination] = true
	}
	return policy, nil
}

// parsePrefix holds the one spelling an approved destination may take, so the
// reader that refuses a prefix and the decision that applies one agree.
func parsePrefix(destination string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(destination)
	if err != nil || prefix.Addr().Is4In6() || prefix.Addr().Zone() != "" || prefix.Masked() != prefix {
		return netip.Prefix{}, errors.New("every approved destination is one CIDR prefix in canonical masked form, such as 127.0.0.0/8 or 10.1.0.0/16")
	}
	return prefix, nil
}

// approves reports whether any declared destination contains this address. A
// policy whose prefixes were never validated approves nothing, so a value
// assembled outside DecodePolicy cannot widen the approved set by accident.
func (p Policy) approves(address netip.Addr) bool {
	for _, destination := range p.ApprovedDestinations {
		prefix, err := parsePrefix(destination)
		if err == nil && prefix.Contains(address) {
			return true
		}
	}
	return false
}
