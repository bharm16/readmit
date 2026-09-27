// Package fhirworker is the fixed, bounded IPC between the validation engine
// and the worker entrypoint (cmd/readmit-validator-worker). It contains no
// executable or filesystem path selected by evidence.
package fhirworker

const RequestSchema = "readmit-fhir-worker-request/v1"
const HeartbeatSchema = "readmit-fhir-worker-heartbeat/v1"
const ResponseSchema = "readmit-fhir-worker-response/v1"

type Request struct {
	Packages       []string `json:"packages"`
	Schema         string   `json:"schema"`
	Job            string   `json:"job"`
	InputSHA256    string   `json:"input_sha256"`
	Profiles       []string `json:"profiles"`
	TimeoutMS      int64    `json:"timeout_ms"`
	MaxOutputBytes int64    `json:"max_output_bytes"`
}
type Heartbeat struct {
	Schema string `json:"schema"`
	Job    string `json:"job"`
}
type Response struct {
	Schema           string `json:"schema"`
	Job              string `json:"job"`
	State            string `json:"state"`
	ExitCode         int    `json:"exit_code"`
	Outcome          []byte `json:"outcome"`
	DiagnosticSHA256 string `json:"diagnostic_sha256"`
}
