// Package profileeval evaluates explicitly pinned local v2 interface constraints
// over losslessly parsed evidence. It neither repairs messages nor contacts a
// terminology service. Legacy profile and pack readers remain unchanged.
package profileeval

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profilepack"
)

const (
	Schema          = "readmit-profile-evaluation/v1"
	ProfileSchema   = "readmit-local-profile/v2"
	PackSchema      = "readmit-profile-pack/v2"
	OperatorVersion = "readmit-profile-evaluator/v1"
	MaxBytes        = 16 << 20
)

// ProfileV2 embeds the exact existing local constraint vocabulary, adding only
// explicit ordered groups and reviewed workflow declarations under a new schema.
type ProfileV2 struct {
	Schema     string               `json:"schema"`
	Definition localprofile.Profile `json:"definition"`
	Structure  []Node               `json:"structure"`
	Workflows  []Workflow           `json:"workflows"`
}
type Node struct {
	Name     string `json:"name"`
	Segment  string `json:"segment,omitzero"`
	Min      int    `json:"min"`
	Max      string `json:"max"`
	Children []Node `json:"children,omitzero"`
}
type Transition struct {
	From string `json:"from"`
	To   string `json:"to"`
}
type Workflow struct {
	ID            string       `json:"id"`
	Version       string       `json:"version"`
	Kind          string       `json:"kind"`
	Identity      []string     `json:"identity"`
	Status        string       `json:"status"`
	RepeatSegment string       `json:"repeat_segment,omitzero"`
	Initial       []string     `json:"initial"`
	Transitions   []Transition `json:"transitions"`
}

// PackV2 adds executable structures to unchanged v1 metadata. Metadata's v1
// support declarations retain their meaning; actual rule evaluation is reported
// independently and never upgrades a labels-only pack.
type PackV2 struct {
	Schema   string           `json:"schema"`
	Metadata profilepack.Pack `json:"metadata"`
	Messages []MessageRule    `json:"messages"`
}
type MessageRule struct {
	HL7Version string        `json:"hl7_version"`
	Family     string        `json:"family"`
	Structure  string        `json:"structure"`
	Sequence   []Node        `json:"sequence"`
	Segments   []SegmentRule `json:"segments"`
}
type SegmentRule struct {
	ID     string      `json:"id"`
	Fields []FieldRule `json:"fields"`
}
type FieldRule struct {
	Position       int    `json:"position"`
	Required       bool   `json:"required"`
	MaxRepetitions int    `json:"max_repetitions"`
	DataType       string `json:"datatype"`
	MaxLength      int    `json:"max_length"`
}
type Occurrence struct {
	ID    string
	Bytes []byte
}
type Options struct{ CompleteCapture bool }
type Pin struct {
	Schema  string `json:"schema"`
	ID      string `json:"id"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}
type Finding struct {
	Occurrence string    `json:"occurrence"`
	Rule       string    `json:"rule"`
	Origin     string    `json:"origin"`
	Outcome    string    `json:"outcome"`
	Selector   string    `json:"selector,omitzero"`
	State      hl7.State `json:"state,omitzero"`
	Start      int       `json:"start"`
	End        int       `json:"end"`
}
type Report struct {
	CaseIdentity    string            `json:"case_identity,omitzero"`
	Verdict         string            `json:"verdict"`
	Schema          string            `json:"schema"`
	Operator        string            `json:"operator"`
	Profile         Pin               `json:"profile"`
	Pack            Pin               `json:"pack"`
	Inputs          map[string]string `json:"inputs"`
	CompleteCapture bool              `json:"complete_capture"`
	LocalVerdict    string            `json:"local_verdict"`
	BaseSupport     string            `json:"base_support"`
	WorkflowSupport string            `json:"workflow_support"`
	Findings        []Finding         `json:"findings"`
}

func digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
