package sendpolicy

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const ScopedPolicySchema = "readmit-network-policy/v1"
const ScopedDecisionSchema = "readmit-network-decision/v1"

type Operation string

const (
	V2Stimulus      Operation = "v2-stimulus"
	CaptureListen   Operation = "capture-listen"
	ObservationRead Operation = "observation-read"
	FHIRMetadata    Operation = "fhir-metadata"
	FHIRSearch      Operation = "fhir-search"
	FHIRAction      Operation = "fhir-action"
	SMARTToken      Operation = "smart-token"
	SetupAction     Operation = "setup-action"
)

func ValidOperation(o Operation) bool {
	return slices.Contains([]Operation{V2Stimulus, CaptureListen, ObservationRead, FHIRMetadata, FHIRSearch, FHIRAction, SMARTToken, SetupAction}, o)
}

type ScopeRule struct {
	Endpoint     string    `json:"endpoint"`
	Operation    Operation `json:"operation"`
	Port         int       `json:"port"`
	Destinations []string  `json:"destinations"`
	Selection    string    `json:"selection"`
}
type ScopedPolicy struct {
	Schema      string      `json:"schema"`
	Project     string      `json:"project"`
	Environment string      `json:"environment"`
	Revision    string      `json:"revision"`
	Rules       []ScopeRule `json:"rules"`
}

var scopeID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func DecodeScopedPolicy(raw []byte) (ScopedPolicy, error) {
	var p ScopedPolicy
	if len(raw) > MaxPolicyBytes || json.Unmarshal(raw, &p, json.RejectUnknownMembers(true)) != nil {
		return p, errors.New("invalid scoped network policy")
	}
	return p, p.Validate()
}
func (p ScopedPolicy) Validate() error {
	invalid := errors.New("invalid scoped network policy")
	if p.Schema != ScopedPolicySchema || !scopeID.MatchString(p.Project) || !scopeID.MatchString(p.Environment) || !scopeID.MatchString(p.Revision) || len(p.Rules) < 1 || len(p.Rules) > 64 {
		return invalid
	}
	seen := map[string]bool{}
	for _, r := range p.Rules {
		key := r.Endpoint + "/" + string(r.Operation)
		if !scopeID.MatchString(r.Endpoint) || !ValidOperation(r.Operation) || r.Port < 1 || r.Port > 65535 || seen[key] || r.Selection != "single-address" && r.Selection != "lowest-address" {
			return invalid
		}
		seen[key] = true
		if validatePolicy(Policy{Schema: PolicySchema, ApprovedDestinations: r.Destinations}) != nil {
			return invalid
		}
	}
	return nil
}

type ScopedRequest struct {
	Project, Environment, Endpoint, Classification, Address string
	Operation                                               Operation
}
type ScopedDecision struct {
	Schema          string    `json:"schema"`
	Allowed         bool      `json:"allowed"`
	Reason          string    `json:"reason"`
	Operation       Operation `json:"operation"`
	Endpoint        string    `json:"endpoint"`
	Address         string    `json:"address"`
	Candidates      []string  `json:"candidates"`
	SelectedAddress string    `json:"selected_address,omitzero"`
}

// DecideScoped validates the entire finite DNS answer before selecting one IP.
// It never falls back to another IP and never relaxes legacy Decide semantics.
func DecideScoped(ctx context.Context, p ScopedPolicy, r ScopedRequest, resolve Resolver) ScopedDecision {
	d := ScopedDecision{Schema: ScopedDecisionSchema, Reason: "policy-refused", Operation: r.Operation, Endpoint: r.Endpoint, Address: r.Address, Candidates: []string{}}
	if p.Validate() != nil || p.Project != r.Project || p.Environment != r.Environment || r.Classification != "nonproduction" {
		return d
	}
	host, port, err := net.SplitHostPort(r.Address)
	if err != nil || host == "" || len(host) > 253 || strings.ContainsAny(host, " /%\\\r\n") {
		return d
	}
	n, err := strconv.Atoi(port)
	if err != nil || strconv.Itoa(n) != port {
		return d
	}
	var rule *ScopeRule
	for _, candidate := range p.Rules {
		if candidate.Endpoint == r.Endpoint && candidate.Operation == r.Operation && candidate.Port == n {
			copy := candidate
			rule = &copy
			break
		}
	}
	if rule == nil {
		return d
	}
	addresses := []netip.Addr{}
	if ip, err := netip.ParseAddr(host); err == nil {
		addresses = append(addresses, ip)
	} else {
		if resolve == nil {
			resolve = SystemResolver
		}
		found, err := resolve(ctx, host)
		if err != nil {
			d.Reason = "resolution-failed"
			return d
		}
		addresses = found
	}
	if ctx.Err() != nil {
		d.Reason = "resolution-cancelled"
		return d
	}
	if len(addresses) < 1 || len(addresses) > 8 {
		d.Reason = "resolution-limit"
		return d
	}
	approved := Policy{Schema: PolicySchema, ApprovedDestinations: rule.Destinations}
	for _, ip := range addresses {
		if !ip.IsValid() || ip.Zone() != "" || ip.Is4In6() || ip.IsUnspecified() || ip.IsMulticast() || !approved.approves(ip) {
			d.Reason = "unapproved-address"
			return d
		}
	}
	addresses = unique(addresses)
	slices.SortFunc(addresses, func(a, b netip.Addr) int { return a.Compare(b) })
	for _, ip := range addresses {
		d.Candidates = append(d.Candidates, ip.String())
	}
	if len(addresses) > 1 && rule.Selection == "single-address" {
		d.Reason = "ambiguous-address"
		return d
	}
	d.SelectedAddress = addresses[0].String()
	d.Allowed = true
	d.Reason = "approved"
	return d
}

// OperationalDecision is safe for operational output; private resolved addresses
// and policy details stay in the protected execution artifact.
type OperationalDecision struct {
	Schema    string    `json:"schema"`
	Allowed   bool      `json:"allowed"`
	Reason    string    `json:"reason"`
	Operation Operation `json:"operation"`
}

func (d ScopedDecision) Redacted() OperationalDecision {
	return OperationalDecision{Schema: "readmit-network-operation/v1", Allowed: d.Allowed, Reason: d.Reason, Operation: d.Operation}
}
