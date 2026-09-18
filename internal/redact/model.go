// Package redact derives testing evidence and generates an explicitly reviewed
// fixture reproducer. Source artifacts and re-identification material stay local.
package redact

import (
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/testrunner"
)

const (
	PolicySchema    = "readmit-redact-policy/v1"
	InventorySchema = "readmit-redact-inventory/v1"
	ReviewSchema    = "readmit-export-review/v1"
	ExportSchema    = "readmit-derived-export/v1"
	PrivateSchema   = "readmit-redact-local/v1"
	Surrogate       = "scoped-surrogate/v1"
	DateShift       = "patient-date-shift/v1"
	Remove          = "remove-field/v1"
	Replace         = "replace-field/v1"
	Retain          = "retain-literal/v1"
	RemoveSegment   = "remove-segment/v1"
	Filenames       = "regenerate-filenames/v1"
	Metadata        = "regenerate-metadata/v1"
	SpecLiterals    = "rewrite-spec-literals/v1"
	Diagnosis       = "regenerate-diagnosis/v1"
	Rerun           = "rerun-derived-tests/v1"
	maxConfigBytes  = 1 << 20
	maxReviewBytes  = 16 << 20
	maxPacketBytes  = 256 << 20
	maxPacketFiles  = 40000
	maxFindings     = 50000
)

// IdentifierScope includes the complete authority tuple, in explicit order.
// Scope keys and source values are retained only in the private state.
type IdentifierScope struct {
	Selector  string   `json:"selector"`
	Authority []string `json:"authority"`
}

type FieldRule struct {
	Selector    string   `json:"selector"`
	Policy      string   `json:"policy"`
	Class       string   `json:"class"`
	Scope       string   `json:"scope,omitzero"`
	Authority   []string `json:"authority,omitzero"`
	Replacement *string  `json:"replacement,omitzero"`
	Allowed     []string `json:"allowed,omitzero"`
}

// LiteralBinding ties an expected literal to the same source field's derived
// value. Only enumerated spec value locations are accepted; no expression runs.
type LiteralBinding struct {
	Location   string  `json:"location"`
	Occurrence string  `json:"occurrence,omitzero"`
	Selector   string  `json:"selector,omitzero"`
	Constant   *string `json:"constant,omitzero"`
}

type Policy struct {
	Schema           string           `json:"schema"`
	Patient          IdentifierScope  `json:"patient"`
	Fields           []FieldRule      `json:"fields"`
	RemoveSegments   []string         `json:"remove_segments"`
	PacketPolicies   []string         `json:"packet_policies"`
	SpecBindings     []LiteralBinding `json:"spec_bindings"`
	RequiredFailures []int            `json:"required_failures"`
}

type OriginalArtifact struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

type Inventory struct {
	Schema         string             `json:"schema"`
	Complete       bool               `json:"complete"`
	Artifacts      []OriginalArtifact `json:"artifacts"`
	ResidualValues []string           `json:"residual_values"`
}

type Inputs struct {
	PolicySHA256    string `json:"policy_sha256"`
	InventorySHA256 string `json:"inventory_sha256"`
	SpecSHA256      string `json:"spec_sha256"`
}

type Review struct {
	Schema                   string                  `json:"schema"`
	State                    string                  `json:"state"`
	DataOrigin               string                  `json:"data_origin"`
	InputCommitment          string                  `json:"input_commitment"`
	LocalStateCommitment     string                  `json:"local_state_commitment"`
	DerivedIdentity          string                  `json:"derived_case_identity"`
	DerivedSpecSHA256        string                  `json:"derived_spec_sha256"`
	Policies                 []string                `json:"policies_applied"`
	Findings                 []exportreview.Finding  `json:"findings"`
	Coverage                 []exportreview.Coverage `json:"coverage"`
	Uncovered                []string                `json:"uncovered_classes"`
	Residual                 exportreview.Scan       `json:"residual_scan"`
	RequiredFailures         []int                   `json:"required_failures"`
	OriginalFailedAssertions []int                   `json:"original_failed_assertions"`
	Scope                    string                  `json:"scope"`
	Identity                 string                  `json:"-"`
}

type Request struct {
	CasePath, SpecPath, PolicyPath, InventoryPath string
	Output, LocalState                            string
}

type ExportRequest struct {
	ReviewPath, LocalState, Approval, Output string
}

type Proof struct {
	Profile          string            `json:"profile"`
	Boundary         string            `json:"observation_boundary"`
	BaselineIdentity string            `json:"baseline_identity"`
	PostfixIdentity  string            `json:"postfix_identity"`
	BaselineStatus   testrunner.Status `json:"baseline_status"`
	PostfixStatus    testrunner.Status `json:"postfix_status"`
	FailedAssertions []int             `json:"failed_assertions"`
}

type File struct {
	Path   string `json:"path"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

type ExportManifest struct {
	Schema         string            `json:"schema"`
	State          string            `json:"state"`
	DataOrigin     string            `json:"data_origin"`
	ApprovedReview string            `json:"approved_review_identity"`
	Review         Review            `json:"review"`
	Proof          Proof             `json:"proof"`
	Residual       exportreview.Scan `json:"residual_scan"`
	Files          []File            `json:"files"`
	Scope          string            `json:"scope"`
}

type sourceReference struct {
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	Identity string `json:"identity"`
}

type mapping struct {
	Key       string `json:"key"`
	Source    string `json:"source"`
	Surrogate string `json:"surrogate"`
}

type shift struct {
	PatientKey string `json:"patient_key"`
	Days       int    `json:"days"`
}

type localState struct {
	Schema         string            `json:"schema"`
	ReviewIdentity string            `json:"review_identity"`
	Sources        []sourceReference `json:"sources"`
	Mappings       []mapping         `json:"mappings"`
	Shifts         []shift           `json:"shifts"`
	ResidualValues [][]byte          `json:"residual_values"`
	OriginalProof  Proof             `json:"original_proof"`
}
