package smartbackend

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"

	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const DiscoverySchema = "readmit-smart-discovery/v1"

type DiscoveryPlan struct {
	base   string
	plan   *networkaction.RuntimeHTTPPlan
	policy []byte
}
type Discovery struct {
	Schema   string                        `json:"schema"`
	FHIRBase string                        `json:"fhir_base"`
	Action   networkaction.RuntimeHTTPSpec `json:"action"`
	Policy   []byte                        `json:"policy"`
	Metadata jsontext.Value                `json:"metadata"`
	Receipt  networkaction.RuntimeReceipt  `json:"receipt"`
}

// PrepareDiscovery binds exactly the configured base's well-known URL. A token
// endpoint on a different origin remains separately configured and admitted.
func PrepareDiscovery(base string, raw, policy []byte) (*DiscoveryPlan, error) {
	var spec networkaction.HTTPSpec
	if !https(base) || strings.HasSuffix(base, "/") || len(raw) > 2<<20 || json.Unmarshal(raw, &spec, json.RejectUnknownMembers(true)) != nil || spec.URL != base+"/.well-known/smart-configuration" || spec.Method != "GET" || spec.Operation != sendpolicy.FHIRMetadata || spec.Credential != nil || spec.MaxBytes > 256<<10 {
		return nil, failure(ServerUnsupported)
	}
	b, _ := json.Marshal(networkaction.RuntimeHTTPSpec{Schema: networkaction.RuntimeHTTPSchema, Accept: "application/json", HTTP: spec})
	p, e := networkaction.PrepareRuntimeHTTP(b, policy)
	if e != nil {
		return nil, failure(ServerUnsupported)
	}
	return &DiscoveryPlan{base: base, plan: p, policy: append([]byte(nil), policy...)}, nil
}
func (p *DiscoveryPlan) Binding() networkaction.Binding { return p.plan.Binding() }
func (p *DiscoveryPlan) Execute(ctx context.Context, authority networkaction.Authority, resolve sendpolicy.Resolver) ([]byte, error) {
	response, receipt, e := p.plan.Execute(ctx, authority, resolve, nil)
	if e != nil || response.Status != 200 {
		return nil, failure(AuthUnavailable)
	}
	metadata := response.Body.Expose()
	var object map[string]jsontext.Value
	if len(metadata) > 256<<10 || json.Unmarshal(metadata, &object) != nil || object == nil {
		return nil, failure(ServerUnsupported)
	}
	// Retain only public protocol metadata consumed here, never arbitrary server
	// diagnostics, echoed headers, cookies or unknown token-like extensions.
	known := map[string]jsontext.Value{}
	for _, key := range []string{"token_endpoint", "grant_types_supported", "token_endpoint_auth_methods_supported", "token_endpoint_auth_signing_alg_values_supported", "capabilities"} {
		if value, ok := object[key]; ok {
			known[key] = value
		}
	}
	selected, _ := json.Marshal(known, json.Deterministic(true))
	d := Discovery{Schema: DiscoverySchema, FHIRBase: p.base, Action: p.plan.Declaration(), Policy: p.policy, Metadata: selected, Receipt: receipt}
	raw, e := json.Marshal(d, json.Deterministic(true))
	if e != nil {
		return nil, failure(ServerUnsupported)
	}
	return raw, nil
}

// DecodeDiscovery is offline and reconstructs the exact network binding. It
// never rediscovers metadata, resolves credentials, or grants authority.
func DecodeDiscovery(raw []byte) (Discovery, error) {
	var d Discovery
	if len(raw) > 3<<20 || json.Unmarshal(raw, &d, json.RejectUnknownMembers(true)) != nil || d.Schema != DiscoverySchema || d.Action.Authorization != "" || d.Action.Schema != networkaction.RuntimeHTTPSchema || d.Action.Accept != "application/json" {
		return Discovery{}, failure(ServerUnsupported)
	}
	spec, _ := json.Marshal(d.Action.HTTP)
	p, e := PrepareDiscovery(d.FHIRBase, spec, d.Policy)
	if e != nil || d.Receipt.Schema != networkaction.RuntimeReceiptSchema || d.Receipt.Binding != p.Binding() || d.Receipt.State != "responded" || d.Receipt.HTTPStatus != 200 || !d.Receipt.Decision.Allowed || d.Receipt.Decision.Schema != "readmit-network-operation/v1" || d.Receipt.Decision.Reason != "approved" || d.Receipt.Decision.Operation != sendpolicy.FHIRMetadata || !networkaction.RecordedActor(d.Receipt.Actor) {
		return Discovery{}, failure(ServerUnsupported)
	}
	var m map[string]jsontext.Value
	if len(d.Metadata) > 256<<10 || json.Unmarshal(d.Metadata, &m) != nil || m == nil {
		return Discovery{}, failure(ServerUnsupported)
	}
	return d, nil
}
