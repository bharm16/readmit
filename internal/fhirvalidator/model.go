// Package fhirvalidator owns a pinned optional offline validation capability.
// Imported requests select declared constraints, never a program or network URL
// to execute. Worker diagnostics remain private evidence, not operational logs.
package fhirvalidator

import (
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/fhirworker"
)

const CapabilitySchema = "readmit-fhir-validator-capability/v1"
const RequestSchema = "readmit-fhir-validation-request/v1"
const ResultSchema = "readmit-fhir-validation-result/v1"
const WorkerRequestSchema = "readmit-fhir-worker-request/v1"
const HeartbeatSchema = "readmit-fhir-worker-heartbeat/v1"
const WorkerResponseSchema = "readmit-fhir-worker-response/v1"

// Asset is an immutable component or metadata file inside the administrator's
// staged capability bundle. Paths are local relative members, never argv.
type Asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Role   string `json:"role"`
}
type Component struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	License string `json:"license"`
	SBOM    string `json:"sbom"`
}
type PackageRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}
type Package struct {
	FHIRVersions []string     `json:"fhir_versions"`
	ID           string       `json:"id"`
	Version      string       `json:"version"`
	SHA256       string       `json:"sha256"`
	License      string       `json:"license"`
	Dependencies []PackageRef `json:"dependencies"`
}
type Canonical struct {
	URL     string     `json:"url"`
	Version string     `json:"version"`
	SHA256  string     `json:"sha256"`
	Package PackageRef `json:"package"`
}
type CanonicalLink struct {
	URL     string `json:"url"`
	Version string `json:"version"`
}
type Terminology struct {
	References []CanonicalLink `json:"references"`
	Canonical  Canonical       `json:"canonical"`
	Kind       string          `json:"kind"`
	Content    string          `json:"content"`
}
type Adapter struct {
	SourceSHA256 string    `json:"source_sha256"`
	JarSHA256    string    `json:"jar_sha256"`
	Compiler     Component `json:"compiler"`
}
type Manifest struct {
	Adapter           Adapter       `json:"adapter"`
	ValidatorPackages []PackageRef  `json:"validator_packages"`
	Schema            string        `json:"schema"`
	Platform          string        `json:"platform"`
	Image             string        `json:"image"`
	BaseImage         string        `json:"base_image"`
	LauncherSHA256    string        `json:"launcher_sha256"`
	Validator         Component     `json:"validator"`
	Runtime           Component     `json:"runtime"`
	Base              Component     `json:"base"`
	Packages          []Package     `json:"packages"`
	Profiles          []Canonical   `json:"profiles"`
	Terminology       []Terminology `json:"terminology"`
	Assets            []Asset       `json:"assets"`
}
type Requirements struct {
	Terminology    string   `json:"terminology"`
	Invariants     string   `json:"invariants"`
	FailSeverities []string `json:"fail_severities"`
}
type Request struct {
	Schema         string       `json:"schema"`
	Capability     string       `json:"capability"`
	InputSHA256    string       `json:"input_sha256"`
	Profiles       []Canonical  `json:"profiles"`
	Requirements   Requirements `json:"requirements"`
	TimeoutMS      int64        `json:"timeout_ms"`
	MaxOutputBytes int64        `json:"max_output_bytes"`
}
type WorkerRequest = fhirworker.Request
type Heartbeat = fhirworker.Heartbeat
type WorkerResponse = fhirworker.Response
type Status struct {
	State       string `json:"state"`
	Requirement string `json:"requirement,omitzero"`
}
type Finding struct {
	Severity    string   `json:"severity"`
	Code        string   `json:"code"`
	MessageID   string   `json:"message_id,omitzero"`
	Expressions []string `json:"expressions"`
	Diagnostics string   `json:"diagnostics"`
	InputSHA256 string   `json:"input_sha256"`
}
type WorkerRecord struct {
	State            string `json:"state"`
	ExitCode         int    `json:"exit_code"`
	OutcomeSHA256    string `json:"outcome_sha256,omitzero"`
	DiagnosticSHA256 string `json:"diagnostic_sha256,omitzero"`
}

// EngineRecord names the local container engine that ran the worker.
type EngineRecord struct {
	Version      string `json:"version"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}
type Result struct {
	RuntimeStatus *Status           `json:"runtime_status,omitzero"`
	Engine        *EngineRecord     `json:"engine,omitzero"`
	Schema        string            `json:"schema"`
	Policy        string            `json:"policy"`
	InputSHA256   string            `json:"input_sha256"`
	Capability    string            `json:"capability"`
	RequestSHA256 string            `json:"request_sha256"`
	State         string            `json:"state"`
	Verdict       assertion.Verdict `json:"verdict"`
	Findings      []Finding         `json:"findings"`
	Coverage      map[string]string `json:"coverage"`
	Worker        WorkerRecord      `json:"worker"`
}
