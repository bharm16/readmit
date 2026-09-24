// Package diff compares selected HL7 fields in verified, local evidence. It
// reads no original artifact paths, opens no network connections, and never
// treats a wire comparison as a test of a receiver's business state.
package diff

import (
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/runresult"
)

const Schema = "readmit-diff/v1"

type Boundary string

const (
	Messages Boundary = "messages"
	ACKs     Boundary = "acks"
)

// Input is a message file, case directory, run directory, or test-result
// directory. Format and Terminator apply only to standalone message files;
// artifact readers use their recorded parsing declarations.
type Input struct {
	Path       string
	Format     hl7.Format
	Terminator hl7.Terminator
	// Opened is an artifact directory runresult's opener already opened,
	// compared as it was verified instead of opening Path again.
	Opened *runresult.Evidence
}

// Options selects an explicit comparison boundary. An empty Fields list visits
// every field repetition in both messages. Ignore rules address exactly one
// canonical selector, with no implicit wildcard or timestamp category.
type Options struct {
	Fields     []string
	Keys       []string
	Ignore     []string
	Boundary   Boundary
	ShowValues bool
}

type InputSummary struct {
	Kind           string `json:"kind"`
	Identity       string `json:"identity"`
	SourceIdentity string `json:"source_identity,omitzero"`
	TargetIdentity string `json:"target_identity,omitzero"`
	ResultStatus   string `json:"result_status,omitzero"`
	ResultBoundary string `json:"result_boundary,omitzero"`
	Payloads       string `json:"payloads"`
	Occurrences    int    `json:"occurrences"`
	Excluded       int    `json:"excluded"`
}

type Reference struct {
	Occurrence       string `json:"occurrence"`
	SourceOccurrence string `json:"source_occurrence,omitzero"`
	Kind             string `json:"kind"`
	PayloadState     string `json:"payload_state"`
	Outcome          string `json:"outcome,omitzero"`
	Delivery         string `json:"delivery,omitzero"`
}

// Value contains no value bytes by default. Display, when explicitly requested,
// is an ASCII-escaped, quoted string; unsupported bytes remain visibly raw.
type Value struct {
	State    hl7.State `json:"state"`
	Encoding string    `json:"encoding,omitzero"`
	Display  *string   `json:"display,omitzero"`
}

type FieldChange struct {
	Selector string `json:"selector"`
	Name     string `json:"name,omitzero"`
	Status   string `json:"status"` // changed or uncompared
	Left     Value  `json:"left"`
	Right    Value  `json:"right"`
}

type SegmentChange struct {
	Position int    `json:"position"`
	Left     string `json:"left"`
	Right    string `json:"right"`
}

type Pair struct {
	Left     Reference       `json:"left"`
	Right    Reference       `json:"right"`
	Status   string          `json:"status"` // unchanged, changed, or uncompared
	Fields   []FieldChange   `json:"fields"`
	Segments []SegmentChange `json:"segments"`
}

type Ambiguity struct {
	Reason string      `json:"reason"`
	Left   []Reference `json:"left"`
	Right  []Reference `json:"right"`
}

type Unaligned struct {
	Side      string    `json:"side"`
	Reference Reference `json:"reference"`
	Reason    string    `json:"reason"`
}

type Unsupported struct {
	Side       string `json:"side"`
	Occurrence string `json:"occurrence,omitzero"`
	Selector   string `json:"selector,omitzero"`
	Code       string `json:"code"`
}

type IgnoreRule struct {
	Selector   string `json:"selector"`
	Compared   int    `json:"compared"`
	Suppressed int    `json:"suppressed"`
}

type Summary struct {
	Paired       int `json:"paired"`
	Changed      int `json:"changed"`
	Unchanged    int `json:"unchanged"`
	Uncompared   int `json:"uncompared"`
	FieldChanges int `json:"field_changes"`
	Inserted     int `json:"inserted"`
	Missing      int `json:"missing"`
	Ambiguous    int `json:"ambiguous"`
	Unaligned    int `json:"unaligned"`
}

// Report is the inspectable consumer boundary for packet assembly. All three
// renderers consume this same report; none reopens evidence or reevaluates it.
// Identities detect content change, not authenticity. ShowValues does not grant
// permission to export a report or any referenced artifact.
type Report struct {
	Schema      string        `json:"schema"`
	Boundary    Boundary      `json:"boundary"`
	Scope       string        `json:"scope"`
	ShowValues  bool          `json:"show_values"`
	Left        InputSummary  `json:"left"`
	Right       InputSummary  `json:"right"`
	Alignment   string        `json:"alignment"`
	Keys        []string      `json:"keys"`
	Fields      []string      `json:"fields"`
	Ignore      []IgnoreRule  `json:"ignore"`
	Summary     Summary       `json:"summary"`
	Pairs       []Pair        `json:"pairs"`
	Missing     []Reference   `json:"missing"`
	Inserted    []Reference   `json:"inserted"`
	Ambiguous   []Ambiguity   `json:"ambiguous"`
	Unaligned   []Unaligned   `json:"unaligned"`
	Unsupported []Unsupported `json:"unsupported"`
}
