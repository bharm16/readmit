// Package connectedtest compiles local connected-test inputs without granting
// execution authority. Frozen legacy readers and evaluators keep their meaning.
package connectedtest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
)

const (
	TestSchema        = "readmit-connected-test/v1"
	PhaseTestSchema   = "readmit-connected-phase-test/v1"
	PhasePlanSchema   = "readmit-connected-phase-plan/v1"
	TestSchemaV3      = "readmit-connected-test/v3"
	PlanSchemaV3      = "readmit-execution-plan/v3"
	TestSchemaV2      = "readmit-connected-test/v2"
	PlanSchemaV2      = "readmit-execution-plan/v2"
	PlanSchema        = "readmit-execution-plan/v1"
	ResultSchema      = "readmit-execution-result/v1"
	AnalysisSchema    = "readmit-execution-analysis/v1"
	OperatorVersion   = "readmit-assertion-set/v1"
	OperatorVersionV2 = "readmit-dataset-assertion-set/v1"
	MaxBytes          = 16 << 20
)

// Reference binds an exact local dependency to one project. File is a relative
// member name, never a resolver instruction or credential locator.
type Reference struct {
	Project string `json:"project"`
	ID      string `json:"id"`
	Schema  string `json:"schema"`
	File    string `json:"file"`
	SHA256  string `json:"sha256"`
}
type TLS struct {
	Mode       string `json:"mode"`
	ServerName string `json:"server_name,omitzero"`
}
type TargetRevision struct {
	Value      string     `json:"value,omitzero"`
	Provenance string     `json:"provenance"`
	Evidence   *Reference `json:"evidence,omitzero"`
}

// Environment retains declarations, not proof of authorization or reachability.
// TLS server-name is the configured verification name, never a successful probe.
type Environment struct {
	Project               string         `json:"project"`
	ID                    string         `json:"id"`
	Revision              string         `json:"revision"`
	Name                  string         `json:"name"`
	Classification        string         `json:"classification"`
	Endpoint              string         `json:"endpoint"`
	TargetIdentity        string         `json:"target_identity"`
	AddressPolicyIdentity string         `json:"address_policy_identity"`
	Grants                []Reference    `json:"grants"`
	TLS                   TLS            `json:"tls"`
	TargetRevision        TargetRevision `json:"target_revision"`
}
type Variable struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitzero"`
	Value     string `json:"value,omitzero"`
	OffsetMS  int64  `json:"offset_ms,omitzero"`
}
type Generation struct {
	Seed     uint64 `json:"seed"`
	BaseTime string `json:"base_time"`
}
type Assignment struct {
	Selector string `json:"selector"`
	Variable string `json:"variable"`
}
type BusinessKey struct {
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}
type V2Stimulus struct {
	Input       Reference    `json:"input"`
	Occurrence  string       `json:"occurrence"`
	Assignments []Assignment `json:"assignments"`
}
type FHIRRequest struct {
	Version     string     `json:"version"`
	Method      string     `json:"method"`
	RelativeURL string     `json:"relative_url"`
	Body        *Reference `json:"body,omitzero"`
}
type Step struct {
	ID           string        `json:"id"`
	Endpoint     string        `json:"endpoint"`
	After        []string      `json:"after"`
	BusinessKeys []BusinessKey `json:"business_keys"`
	V2           *V2Stimulus   `json:"v2_message,omitzero"`
	FHIR         *FHIRRequest  `json:"fhir_request,omitzero"`
}
type Completion struct {
	Policy     *Reference `json:"policy,omitzero"`
	Kind       string     `json:"kind"`
	HorizonMS  int64      `json:"horizon_ms"`
	Barrier    *Reference `json:"barrier,omitzero"`
	MaxRecords int        `json:"max_records"`
	MaxBytes   int        `json:"max_bytes"`
}
type Dataset struct {
	Namespace  string     `json:"namespace,omitzero"`
	Projection *Reference `json:"projection,omitzero"`
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Phase      string     `json:"phase"`
	Source     string     `json:"source"`
	Completion Completion `json:"completion"`
}

// Bindings adapt named datasets to assertion/v1's frozen scopes. They never
// reinterpret an occurrence as a business key or a server resource identity.
type Bindings struct {
	Observed string `json:"observed,omitzero"`
	Before   string `json:"before,omitzero"`
	After    string `json:"after,omitzero"`
}
type Setup struct {
	Kind         string     `json:"kind"`
	Isolation    string     `json:"isolation"`
	Instructions string     `json:"instructions"`
	Plan         *Reference `json:"plan,omitzero"`
	Cleanup      *Reference `json:"cleanup,omitzero"`
}
type Limits struct {
	MaxSteps   int   `json:"max_steps"`
	MaxBytes   int   `json:"max_bytes"`
	DeadlineMS int64 `json:"deadline_ms"`
}
type Test struct {
	Schema          string      `json:"schema"`
	Project         string      `json:"project"`
	ID              string      `json:"id"`
	Revision        string      `json:"revision"`
	Ancestry        *Reference  `json:"ancestry,omitzero"`
	Environment     Environment `json:"environment"`
	Variables       []Variable  `json:"variables"`
	Setup           Setup       `json:"setup"`
	Steps           []Step      `json:"steps"`
	Datasets        []Dataset   `json:"datasets"`
	Bindings        Bindings    `json:"bindings"`
	Checks          Reference   `json:"checks"`
	Profiles        []Reference `json:"profiles"`
	OperatorVersion string      `json:"operator_version"`
	Limits          Limits      `json:"limits"`
}
type Effect struct {
	Step     string `json:"step"`
	Kind     string `json:"kind"`
	Endpoint string `json:"endpoint"`
}
type Member struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
}
type PlanDocument struct {
	Schema       string            `json:"schema"`
	TestIdentity string            `json:"test_identity"`
	Test         Test              `json:"test"`
	Environment  Environment       `json:"environment"`
	Generation   Generation        `json:"generation"`
	Resolution   map[string]string `json:"resolution"`
	Order        []string          `json:"order"`
	Effects      []Effect          `json:"effects"`
	Members      []Member          `json:"members"`
}

// Plan exposes defensive copies only; compilation owns the accepted bytes.
type Plan struct {
	document PlanDocument
	files    map[string][]byte
	identity string
}

func (p *Plan) Identity() string { return p.identity }
func (p *Plan) Document() PlanDocument {
	var d PlanDocument
	b, _ := json.Marshal(p.document)
	_ = json.Unmarshal(b, &d)
	return d
}
func (p *Plan) Files() map[string][]byte {
	out := make(map[string][]byte, len(p.files))
	for n, b := range p.files {
		out[n] = append([]byte(nil), b...)
	}
	return out
}
func Digest(b []byte) string       { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func encode(v any) ([]byte, error) { return json.Marshal(v, json.Deterministic(true)) }

// RevisionEvidence is an externally acquired preflight receipt supplied as a
// local dependency. Compilation checks its binding and bytes, not the collector's
// authenticity. Producing it requires a separate explicitly authorized preflight.
type RevisionEvidence struct {
	Schema                string    `json:"schema"`
	TargetIdentity        string    `json:"target_identity"`
	AddressPolicyIdentity string    `json:"address_policy_identity"`
	ServerName            string    `json:"server_name,omitzero"`
	Revision              string    `json:"revision"`
	ObservedAt            string    `json:"observed_at"`
	CollectorVersion      string    `json:"collector_version"`
	Source                Reference `json:"source"`
}

const RevisionEvidenceSchema = "readmit-target-revision-evidence/v1"

func intervalTest(schema string) bool { return schema == TestSchemaV3 || schema == PhaseTestSchema }
