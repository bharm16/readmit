// Package testisolation executes reviewed synthetic fixture effects through a
// separately registered customer adapter. Imported definitions contain no code
// or transport configuration. Historical check-only reset operators are unchanged.
package testisolation

import (
	"context"
	"encoding/json/v2"
	"errors"
	"regexp"

	"github.com/bharm16/readmit/internal/networkaction"
)

const (
	ContractSchema = "readmit-test-isolation/v1"
	RegistrySchema = "readmit-fixture-adapter-registry/v1"
	ProtocolSchema = "readmit-fixture-adapter/v1"
	PlanSchema     = "readmit-isolation-plan/v1"
	ResultSchema   = "readmit-isolation-result/v1"
	ReviewSchema   = "readmit-isolation-review/v1"
	MaxBytes       = 1 << 20
)

var refused = errors.New("test isolation refused; inspect scoped prerequisites, retained effects and current authority")
var token = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type Contract struct {
	Schema      string        `json:"schema"`
	Project     string        `json:"project"`
	Environment string        `json:"environment"`
	Revision    string        `json:"revision"`
	Adapter     string        `json:"adapter"`
	Tenant      string        `json:"tenant"`
	Namespace   string        `json:"namespace"`
	Mode        string        `json:"mode"`        // isolated-tenant, reserved-namespace, recorded-baseline
	Concurrency string        `json:"concurrency"` // exclusive-target-lease
	Baseline    string        `json:"baseline,omitzero"`
	Resources   []Requirement `json:"resources"`
	Manual      []ManualStep  `json:"manual"`
}
type Requirement struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Template    string            `json:"template"`
	Ownership   string            `json:"ownership"` // create, claim, select
	LogicalID   string            `json:"logical_id,omitzero"`
	Version     string            `json:"version,omitzero"`
	DependsOn   []string          `json:"depends_on"`
	Attributes  map[string]string `json:"attributes"`
	Identifiers []Identifier      `json:"identifiers"`
}
type Identifier struct {
	Scope     string `json:"scope"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"` // Empty selects a deterministic allocation; authored duplicates survive.
}
type ManualStep struct {
	ID           string `json:"id"`
	Instructions string `json:"instructions"`
}
type Registry struct {
	Schema   string         `json:"schema"`
	Adapters []Registration `json:"adapters"`
}

// Registration is operator-controlled local configuration, never an imported
// dependency. Read, setup and cleanup use distinct credential references.
type Registration struct {
	ID                  string     `json:"id"`
	Revision            string     `json:"revision"`
	Project             string     `json:"project"`
	Environment         string     `json:"environment"`
	EnvironmentRevision string     `json:"environment_revision"`
	Classification      string     `json:"classification"`
	Tenant              string     `json:"tenant"`
	Namespace           string     `json:"namespace"`
	URL                 string     `json:"url"`
	ServerName          string     `json:"server_name"`
	Authorities         []byte     `json:"authorities"`
	Read                Credential `json:"read"`
	Setup               Credential `json:"setup"`
	Cleanup             Credential `json:"cleanup"`
	Templates           []Template `json:"templates"`
	TimeoutMS           int64      `json:"timeout_ms"`
}
type Credential struct {
	Endpoint  string                   `json:"endpoint"`
	Reference networkaction.Credential `json:"reference"`
}
type Template struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Attributes []string `json:"attributes"`
}
type Scope struct {
	Project         string `json:"project"`
	Environment     string `json:"environment"`
	Revision        string `json:"revision"`
	AdapterRevision string `json:"adapter_revision"`
	Tenant          string `json:"tenant"`
	Namespace       string `json:"namespace"`
	LeaseKey        string `json:"lease_key"`
	Owner           string `json:"owner"`
}
type Resource struct {
	Alias       string            `json:"alias"`
	Kind        string            `json:"kind"`
	ID          string            `json:"id"`
	Version     string            `json:"version"`
	Owner       string            `json:"owner"`
	Template    string            `json:"template"`
	Attributes  map[string]string `json:"attributes"`
	Identifiers []Identifier      `json:"identifiers"`
	References  []Reference       `json:"references"`
}
type Reference struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type Capabilities struct {
	Schema        string     `json:"schema"`
	Scope         Scope      `json:"scope"`
	Protocol      string     `json:"protocol"`
	LeaseMode     string     `json:"lease_mode"`
	VersionGuards bool       `json:"version_guards"`
	Templates     []Template `json:"templates"`
}
type Snapshot struct {
	Schema    string     `json:"schema"`
	Scope     Scope      `json:"scope"`
	Lease     Lease      `json:"lease"`
	Resources []Resource `json:"resources"`
}
type Lease struct {
	Key     string `json:"key"`
	Owner   string `json:"owner"`
	Version string `json:"version"`
}
type Request struct {
	Schema   string    `json:"schema"`
	Scope    Scope     `json:"scope"`
	Lease    Lease     `json:"lease"`
	Action   string    `json:"action"`
	Resource *Resource `json:"resource,omitzero"`
}
type Reply struct {
	Schema   string    `json:"schema"`
	Scope    Scope     `json:"scope"`
	Lease    Lease     `json:"lease"`
	Resource *Resource `json:"resource,omitzero"`
	Outcome  string    `json:"outcome"`
}
type Options struct {
	ParentPlan string `json:"parent_plan"`
	Instance   string `json:"instance"`
	Seed       uint64 `json:"seed"`
}
type planDocument struct {
	Schema    string     `json:"schema"`
	Contract  Contract   `json:"contract"`
	Registry  Registry   `json:"registry"`
	Policy    []byte     `json:"policy"`
	Options   Options    `json:"options"`
	Scope     Scope      `json:"scope"`
	Resources []Resource `json:"resources"`
}
type Prepared struct {
	document                     planDocument
	registration                 Registration
	identity                     string
	registryPath, policyPath     string
	registryDigest, policyDigest string
	recovery                     string
}
type Effect struct {
	Alias     string            `json:"alias"`
	Kind      string            `json:"kind"`
	ID        string            `json:"id"`
	Operation string            `json:"operation"`
	Expected  map[string]string `json:"expected"`
}

// Review is an engine-owned exact-action description for the existing RD review
// lifecycle or a separate runner grant. It is not a consent token.
type Review struct {
	Schema   string                `json:"schema"`
	Identity string                `json:"identity"`
	Phase    string                `json:"phase"`
	Binding  networkaction.Binding `json:"binding"`
	Scope    Scope                 `json:"scope"`
	Effects  []Effect              `json:"effects"`
	Manual   []ManualStep          `json:"manual"`
}
type Authorities struct{ Read, Setup, Cleanup networkaction.Authority }
type Confirmation struct {
	Plan     string
	Instance string
	Steps    []string
}
type ManualClaim struct {
	ID         string `json:"id"`
	Provenance string `json:"provenance"`
}
type Entry struct {
	Sequence int       `json:"sequence"`
	Action   string    `json:"action"`
	Alias    string    `json:"alias,omitzero"`
	State    string    `json:"state"`
	Request  Request   `json:"request"`
	Network  string    `json:"network"`
	Before   *Snapshot `json:"before,omitzero"`
	After    *Snapshot `json:"after,omitzero"`
}
type Result struct {
	Reconciliation string        `json:"reconciliation,omitzero"`
	Schema         string        `json:"schema"`
	Plan           string        `json:"plan"`
	Scope          Scope         `json:"scope"`
	Setup          string        `json:"setup"`
	Cleanup        string        `json:"cleanup"`
	Lease          Lease         `json:"lease"`
	Manual         []ManualClaim `json:"manual"`
	Entries        []Entry       `json:"entries"`
	Resources      []Resource    `json:"resources"`
	Complete       bool          `json:"complete"`
}

func canonical(value any) []byte {
	raw, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		panic(err)
	}
	return raw
}
func clone[T any](v T) T { var out T; _ = json.Unmarshal(canonical(v), &out); return out }
func decode(raw []byte, v any) error {
	if len(raw) > MaxBytes || json.Unmarshal(raw, v, json.RejectUnknownMembers(true)) != nil {
		return refused
	}
	return nil
}
func (p *Prepared) Identity() string {
	if p == nil {
		return ""
	}
	return p.identity
}
func (p *Prepared) Scope() Scope            { return p.document.Scope }
func (p *Prepared) Allocations() []Resource { return clone(p.document.Resources) }
func (p *Prepared) authority(a Authorities, phase string) networkaction.Authority {
	switch phase {
	case "read":
		return a.Read
	case "setup":
		return a.Setup
	case "cleanup":
		return a.Cleanup
	}
	return nil
}
func (p *Prepared) Check(ctx context.Context, a Authorities, phase string) (networkaction.Actor, error) {
	if p == nil || p.current() != nil || p.authority(a, phase) == nil {
		return networkaction.Actor{}, refused
	}
	actor, err := p.authority(a, phase).Check(ctx, p.Review(phase).Binding)
	if err != nil || !networkaction.CurrentActor(actor) {
		return networkaction.Actor{}, refused
	}
	return actor, nil
}
