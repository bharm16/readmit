// Package report creates and verifies sealed, synthetic engagement packets.
// It supports one committed scenario, never imported or customer-derived data.
package report

import (
	_ "embed"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/testrunner"
)

const (
	Schema   = "readmit-report/v1"
	Scenario = "siu-reschedule-v1"
	// Independently authored reference identity from docs/synth-v1-vector.md.
	caseIdentity   = "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df"
	maxFileBytes   = 16 << 20
	maxPacketBytes = 64 << 20
	maxFiles       = 256
)

//go:embed scenario.json
var scenarioSpec []byte

type RunLabel struct {
	Path                   string            `json:"path"`
	ResultIdentity         string            `json:"result_identity"`
	InputBundleIdentity    string            `json:"input_bundle_identity"`
	SpecIdentity           string            `json:"spec_identity"`
	TargetIdentity         string            `json:"target_configuration_identity"`
	ReceiverImplementation string            `json:"receiver_implementation"`
	ReceiverProfile        string            `json:"receiver_profile"`
	ReceiverMode           observation.Mode  `json:"receiver_mode"`
	ReceiverSession        string            `json:"receiver_session"`
	Status                 testrunner.Status `json:"status"`
	LedgerCount            int               `json:"ledger_count"`
}

type Manifest struct {
	Schema                  string                 `json:"schema"`
	State                   string                 `json:"state"`
	Scenario                string                 `json:"scenario"`
	Provenance              string                 `json:"provenance"`
	Generator               bundle.GeneratorInputs `json:"generator"`
	InputIdentity           string                 `json:"input_identity"`
	SpecIdentity            string                 `json:"spec_identity"`
	ObservationBoundary     string                 `json:"observation_boundary"`
	InputChanged            bool                   `json:"input_changed"`
	ReceiverBehaviorChanged bool                   `json:"receiver_behavior_changed"`
	Limitations             []string               `json:"limitations"`
	Runs                    []RunLabel             `json:"runs"`
	Files                   []bundle.Payload       `json:"files"`
}

// Packet is independently validated by Open. The retained file snapshot is
// private so Prepare cannot accidentally copy changed inputs after validation.
type Packet struct {
	Manifest Manifest
	Identity string
	files    map[string][]byte
}

var limitations = []string{
	"Synthetic-only evidence; no patient or customer-derived input. This does not establish readiness to accept customer PHI.",
	"The tested observation boundary is the built-in fixture's appointment ledger, bound to its ACK receipts and fresh empty session. No production receiver or downstream clinical workflow was tested.",
	"The sent-message field diff is unchanged: both modes receive the identical two messages. The ledger has two records in defective mode and one in fixed mode. AA acknowledgements alone do not establish workflow correctness.",
	"Target hashes identify configuration, including a loopback port; they do not identify receiver software. Receiver implementation, profile, mode and session are stated separately. ACK timestamps and receipts may differ without being the defect.",
	"Hashes detect changed content, not authenticity or approval. Packet sealing is an integrity contract, not filesystem write protection or a signature.",
}
