package sendpolicy

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net"
	"net/netip"
	"slices"
	"time"
)

// DecisionSchema is the contract a retained policy decision declares.
const DecisionSchema = "readmit-send-decision/v1"

// The classes this package compares a recorded classification against. They are
// the spellings readmit-target records. This package keeps no classification
// type of its own, so a recorded class travels here exactly as it was written
// and an unrecognized one is simply not nonproduction.
const (
	// ProductionClass is the one recorded class that refuses on its own.
	ProductionClass = "production"
	nonproduction   = "nonproduction"
)

// RefusesEverySend reports whether a recorded classification refuses every send
// by itself, whatever else is configured and whatever a policy approves.
//
// It is the half of the rule that needs no policy document and that nothing can
// grant, so the point where bytes would leave applies it directly rather than
// keeping a second copy of the comparison.
func RefusesEverySend(classification string) bool {
	return classification == ProductionClass
}

// Reason names why one decision came out the way it did. The set is closed. An
// outcome this package cannot name is a denial, never a pass: unknown and
// unsupported are not approval.
type Reason string

const (
	// Approved is a destination an explicitly selected policy covers.
	Approved Reason = "approved"
	// LoopbackDestination is a literal loopback address, which cannot leave
	// this machine and therefore needs no approved-destination document.
	LoopbackDestination Reason = "loopback_destination"

	// ProductionClassification is the class an operator recorded as
	// production. readmit refuses it wherever it appears.
	ProductionClassification Reason = "production_classification"
	// UnrecordedClassification is a class nobody recorded. An absent claim is
	// not a nonproduction one, so it is denied rather than assumed.
	UnrecordedClassification Reason = "unrecorded_classification"
	// PolicyRequired is a destination that is not a literal loopback address
	// and that no explicitly selected policy authorized.
	PolicyRequired Reason = "policy_required"
	// UnresolvableDestination is an address readmit could not turn into one
	// address it can check.
	UnresolvableDestination Reason = "unresolvable_destination"
	// AmbiguousDestination is a name resolving to more than one address. Which
	// of them a send would reach is not established, so none of them is.
	AmbiguousDestination Reason = "ambiguous_destination"
	// UnapprovedDestination is an address no approved destination contains.
	UnapprovedDestination Reason = "unapproved_destination"
	// SendNotExplicit is a send nobody asked for. It is the last rule, so a
	// preview and a diagnosis report the destination refusal when there is
	// one, and this only when nothing about the destination refuses.
	SendNotExplicit Reason = "send_not_explicit"
)

// Request is the one send a policy is asked about: the address a configuration
// declares, the class recorded for its environment, and whether the operator
// explicitly asked to send.
type Request struct {
	Address        string
	Classification string
	Explicit       bool
}

// Decision is retained evidence of what was allowed or denied and why. It
// records the addresses that were actually checked and the destinations they
// were checked against, so a decision can be read later without the policy
// document and the configuration that produced it.
type Decision struct {
	Schema               string    `json:"schema"`
	Allowed              bool      `json:"allowed"`
	Reason               Reason    `json:"reason"`
	Address              string    `json:"address"`
	Classification       string    `json:"classification"`
	ExplicitSend         bool      `json:"explicit_send"`
	PolicySelected       bool      `json:"policy_selected"`
	ApprovedDestinations []string  `json:"approved_destinations"`
	ResolvedAddresses    []string  `json:"resolved_addresses"`
	DecidedAt            time.Time `json:"decided_at"`
}

// Resolver turns a configured name into the addresses it resolves to now. It is
// a parameter so a decision is always about a resolution readmit performed at
// the point of deciding, and so tests never depend on a name outside the test.
type Resolver func(ctx context.Context, host string) ([]netip.Addr, error)

// SystemResolver looks a configured name up through the operating system.
func SystemResolver(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// Decide answers whether readmit may send to this destination now.
//
// policy is nil when the operator selected none. A literal loopback address is
// then the only destination that remains available, because bytes sent to it
// cannot leave this machine; everything else is denied until a policy names it.
//
// A name is resolved here and nowhere else in the decision, at the moment the
// question is asked. That is the point of the rule: a name that resolves to a
// production address is a production send whatever its label says.
func Decide(ctx context.Context, policy *Policy, request Request, resolve Resolver) Decision {
	decision := Decision{
		Schema: DecisionSchema, Reason: UnresolvableDestination,
		Address: request.Address, Classification: request.Classification,
		ExplicitSend: request.Explicit, PolicySelected: policy != nil,
		ApprovedDestinations: []string{}, ResolvedAddresses: []string{},
		DecidedAt: time.Now().UTC(),
	}
	if decision.Classification == "" {
		decision.Classification = "unclassified"
	}
	if policy != nil {
		decision.ApprovedDestinations = slices.Clone(policy.ApprovedDestinations)
	}
	host, _, err := net.SplitHostPort(request.Address)
	if err != nil || host == "" {
		return decision
	}
	if RefusesEverySend(decision.Classification) {
		decision.Reason = ProductionClassification
		return decision
	}
	literal, literalErr := netip.ParseAddr(host)
	literal = literal.Unmap()
	if policy == nil {
		if literalErr != nil || !literal.IsLoopback() {
			decision.Reason = PolicyRequired
			return decision
		}
		decision.ResolvedAddresses = []string{literal.String()}
		return explicit(decision, LoopbackDestination, request.Explicit)
	}
	// Selecting a policy asks the whole question, so a class nobody recorded is
	// refused here even for an address that cannot leave this machine.
	if decision.Classification != nonproduction {
		decision.Reason = UnrecordedClassification
		return decision
	}
	addresses := []netip.Addr{literal}
	if literalErr != nil {
		found, err := resolve(ctx, host)
		if err != nil {
			return decision
		}
		addresses = unique(found)
	}
	for _, address := range addresses {
		decision.ResolvedAddresses = append(decision.ResolvedAddresses, address.String())
	}
	switch {
	case len(addresses) == 0:
		return decision
	case len(addresses) > 1:
		decision.Reason = AmbiguousDestination
		return decision
	case !policy.approves(addresses[0]):
		decision.Reason = UnapprovedDestination
		return decision
	}
	return explicit(decision, Approved, request.Explicit)
}

// explicit applies the last rule. Nothing about the destination refuses at this
// point, so the send itself is what remains to be asked for.
func explicit(decision Decision, reason Reason, requested bool) Decision {
	if !requested {
		decision.Reason = SendNotExplicit
		return decision
	}
	decision.Allowed, decision.Reason = true, reason
	return decision
}

// unique reports the distinct addresses a name resolved to. A name answered
// twice with one address is one destination; two distinct addresses are two,
// and which one a send would reach is not established by resolving again.
func unique(found []netip.Addr) []netip.Addr {
	addresses := make([]netip.Addr, 0, len(found))
	for _, address := range found {
		address = address.Unmap()
		if address.IsValid() && !slices.Contains(addresses, address) {
			addresses = append(addresses, address)
		}
	}
	return addresses
}

// EncodeDecision renders one decision as the document a command retains. The
// caller performs the write, so every artifact readmit produces is reserved by
// one path owner rather than by each package that has something to record.
func EncodeDecision(decision Decision) ([]byte, error) {
	data, err := json.Marshal(decision, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode the policy decision")
	}
	return append(data, '\n'), nil
}
