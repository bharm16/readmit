package fhirrest

import (
	"fmt"
	"io"
)

const SummarySchema = "readmit-fhir-http-summary/v1"

type Summary struct {
	ExecutionState   string `json:"execution_state"`
	Schema           string `json:"schema"`
	State            string `json:"state"`
	Attempts         int    `json:"attempts"`
	Pages            int    `json:"pages"`
	Matches          int    `json:"matches"`
	MatchOccurrences int    `json:"match_occurrences"`
	Includes         int    `json:"includes"`
	Outcomes         int    `json:"outcomes"`
	Overlaps         int    `json:"overlaps"`
	Coverage         string `json:"coverage"`
	Consistency      string `json:"consistency"`
}

func (r Result) Summary() Summary {
	return Summary{ExecutionState: r.ExecutionState, Schema: SummarySchema, State: r.State, Attempts: len(r.Attempts), Pages: r.Search.Pages, Matches: r.Search.Matches, MatchOccurrences: r.Search.MatchOccurrences, Includes: r.Search.Includes, Outcomes: r.Search.Outcomes, Overlaps: len(r.Search.Overlaps), Coverage: r.Search.Coverage, Consistency: r.Search.Consistency}
}
func (r Result) Format(s fmt.State, _ rune) {
	_, _ = fmt.Fprintf(s, "FHIR HTTP result (%s; %d attempts)", r.State, len(r.Attempts))
}
func (a Attempt) Format(s fmt.State, _ rune) {
	_, _ = fmt.Fprintf(s, "FHIR HTTP attempt (%d; %s)", a.Index, a.Outcome.State)
}
func (Spec) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, "FHIR HTTP plan (private)") }
