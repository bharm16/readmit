// Package testrunner executes declarative tests against explicit test endpoints.
// Ledger testimony is bound to the receiver session by receipts in recorded ACKs.
package testrunner

import (
	"bytes"
	"os"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
)

const (
	SpecSchema     = "readmit-test/v1"
	Schema         = "readmit-result/v1"
	ReceiptSchema  = "readmit-receipt/v1"
	MaxSpecBytes   = 1 << 20
	maxResultBytes = 8 << 20
	maxAssertions  = 256
	LedgerBoundary = "appointment-ledger"
	ACKBoundary    = "ack-contract"
)

type Spec struct {
	Schema      string      `json:"schema"`
	Name        string      `json:"name"`
	Input       Input       `json:"input"`
	Target      string      `json:"target"`
	Setup       Setup       `json:"setup"`
	Observation Observation `json:"observation"`
	Assertions  []Assertion `json:"assertions"`
}

type Input struct {
	Case     string   `json:"case"`
	Messages []string `json:"messages"`
}

type Setup struct {
	InitialState      string `json:"initial_state"`
	ResetInstructions string `json:"reset_instructions"`
}

type Observation struct {
	Boundary string `json:"boundary"`
	Path     string `json:"path,omitzero"`
}

// FieldValue explicitly distinguishes omission, empty, and HL7 null. Text is
// decoded UTF-8 and is required only for a present field, never for other states.
type FieldValue struct {
	State hl7.State `json:"state"`
	Text  *string   `json:"text,omitzero"`
}

// Value is a typed union. Exactly one member belongs to each assertion operator.
type Value struct {
	Count   *int                  `json:"count,omitzero"`
	Records *[]observation.Record `json:"records,omitzero"`
	Field   *FieldValue           `json:"field,omitzero"`
}

type Assertion struct {
	ID       string `json:"id"`
	Operator string `json:"operator"`
	Message  string `json:"message,omitzero"`
	Selector string `json:"selector,omitzero"`
	Expected Value  `json:"expected"`
}

type Status string

const (
	Pass             Status = "pass"
	AssertionFailure Status = "assertion_failure"
	ExecutionError   Status = "execution_error"
)

func (s Status) ExitCode() int {
	switch s {
	case Pass:
		return 0
	case AssertionFailure:
		return 1
	default:
		return 2
	}
}

type AssertionResult struct {
	Assertion Assertion `json:"assertion"`
	Observed  *Value    `json:"observed"`
	Status    string    `json:"status"`
}

type RunReference struct {
	Path     string `json:"path"`
	Identity string `json:"identity"`
}

// Result is customer-local evidence. Open, rather than json.Unmarshal, is the
// consumer boundary: it verifies the retained files, run, and assertion verdicts.
type Result struct {
	Schema               string               `json:"schema"`
	State                string               `json:"state"`
	ContainsSourceValues bool                 `json:"contains_source_values"`
	ExportPolicy         string               `json:"export_policy"`
	Status               Status               `json:"status"`
	ErrorClass           string               `json:"error_class"`
	SpecIdentity         string               `json:"spec_identity"`
	Spec                 *bundle.Payload      `json:"spec"`
	InputBundleIdentity  string               `json:"input_bundle_identity"`
	TargetIdentity       string               `json:"target_identity"`
	Target               *replay.TargetRecord `json:"target"`
	ReceiverSessionID    string               `json:"receiver_session_id"`
	ReceiverMode         observation.Mode     `json:"receiver_mode"`
	ObservationBoundary  string               `json:"observation_boundary"`
	Run                  *RunReference        `json:"run"`
	InitialObservation   *bundle.Payload      `json:"initial_observation"`
	FinalObservation     *bundle.Payload      `json:"final_observation"`
	Assertions           []AssertionResult    `json:"assertions"`
}

// Artifact contains independently verified material for diff, report, and local
// export-review consumers. Original results are never share-approved artifacts.
type Artifact struct {
	Result   Result
	Identity string
	// Environment is the named environment this execution was pointed at. It
	// is empty when no configuration validated, and a reopened result carries
	// none: readmit-result/v1 is frozen and records the transport, not the
	// environment, so readmit does not invent one to print.
	Environment        replay.Environment
	Spec               *Spec
	Run                *replay.Run
	InitialObservation *observation.Snapshot
	FinalObservation   *observation.Snapshot
}

// Plan seals the spec bytes, selected messages, endpoint, and path resolution.
type Plan struct {
	spec            Spec
	raw             []byte
	specPath        string
	sourcePath      string
	sourceInfo      os.FileInfo
	observationPath string
	replay          *replay.Plan
}

func (p *Plan) Count() int                      { return p.replay.Count() }
func (p *Plan) SpecIdentity() string            { return digest(p.raw) }
func (p *Plan) SourceIdentity() string          { return p.replay.SourceIdentity() }
func (p *Plan) Boundary() string                { return p.spec.Observation.Boundary }
func (p *Plan) Target() replay.TargetRecord     { return p.replay.Target() }
func (p *Plan) Environment() replay.Environment { return p.replay.Environment() }

// PinnedInputs returns copies of the configuration and intended outbound bytes
// sealed by Prepare, for durable journals. It cannot authorize or replay them.
type PinnedInputs struct {
	Configuration  replay.Target       `json:"configuration"`
	Spec           []byte              `json:"spec_base64"`
	Target         replay.TargetRecord `json:"target"`
	Environment    replay.Environment  `json:"environment"`
	SourceIdentity string              `json:"source_identity"`
	Mappings       []replay.Mapping    `json:"mappings"`
}

func (p *Plan) PinnedInputs() PinnedInputs {
	return PinnedInputs{Configuration: p.replay.Configuration(), Spec: bytes.Clone(p.raw), Target: p.Target(), Environment: p.Environment(), SourceIdentity: p.SourceIdentity(), Mappings: p.replay.Mappings()}
}
func (p *Plan) Outbound(id string) ([]byte, error) { return p.replay.Outbound(id) }

// DurableDestination applies the same evidence-containment rule before a job
// wrapper reserves its directory around the result.
func (p *Plan) DurableDestination(output string) (string, error) {
	return artifactpath.Destination(output, p.sourceInfo)
}
