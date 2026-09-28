// Package sequenceanalysis describes the limits of a verified event sequence
// under explicit operator declarations. It changes no evidence or linkage.
package sequenceanalysis

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
)

const Schema = "readmit-sequence-analysis/v1"
const MaxBytes = 1 << 20

// MaxClockToleranceSeconds bounds a declaration's clock tolerance: how far an
// occurrence's observed and message-declared instants may differ before the
// analysis reports a clock mismatch. It never aligns one clock with another.
const MaxClockToleranceSeconds = 86400

// DeclaredCoverage is what an operator declares about the capture through one
// source window: that it may have missed occurrences, or that it held all of
// them. Either is a declaration, never a verified completeness.
type DeclaredCoverage string

const (
	PartialCoverage  DeclaredCoverage = "partial"
	CompleteCoverage DeclaredCoverage = "complete"
)

// DeclaredCoverages are every declared coverage this reader accepts.
func DeclaredCoverages() []DeclaredCoverage {
	return []DeclaredCoverage{PartialCoverage, CompleteCoverage}
}

// RetryBasis is what a retry declaration rests on.
type RetryBasis string

// OperatorReportedRetry: an operator reported that the retry resent the
// original. It is the one basis this reader accepts.
const OperatorReportedRetry RetryBasis = "operator_reported_retry"

// RetryBases are every retry basis this reader accepts.
func RetryBases() []RetryBasis { return []RetryBasis{OperatorReportedRetry} }

// Window is one declared source window. TimeZone, when declared, is the IANA
// time zone the window was declared in: its start and end then carry the
// offset that zone has at each of them, so the window reads back as the wall
// times it was declared at. It places nothing and adjusts no observed time.
type Window struct {
	Source   string           `json:"source"`
	Start    time.Time        `json:"start"`
	End      time.Time        `json:"end"`
	TimeZone string           `json:"time_zone,omitzero"`
	Coverage DeclaredCoverage `json:"coverage"`
}
type Retry struct {
	First string     `json:"first"`
	Retry string     `json:"retry"`
	Basis RetryBasis `json:"basis"`
}

// Zone is the IANA time zone name reads as: a named zone this machine's zone
// database holds. The machine's own local zone is never a declared zone.
func Zone(name string) (*time.Location, error) {
	location, err := time.LoadLocation(name)
	if name == "" || name == "Local" || err != nil {
		return nil, errors.New("a time zone is an IANA time zone name such as America/Chicago or UTC")
	}
	return location, nil
}

// InZone reports whether an instant carries the offset the zone has at it.
func InZone(at time.Time, location *time.Location) bool {
	_, offset := at.Zone()
	_, zoned := at.In(location).Zone()
	return offset == zoned
}

type Downstream struct {
	Occurrence string `json:"occurrence"`
	Source     string `json:"source"`
	Rule       string `json:"rule"`
}
type Declaration struct {
	RulesSHA256           string       `json:"rules_sha256"`
	Schema                string       `json:"schema"`
	CaseIdentity          string       `json:"case_identity"`
	ClockToleranceSeconds int          `json:"clock_tolerance_seconds"`
	Windows               []Window     `json:"windows"`
	Retries               []Retry      `json:"retries"`
	Downstream            []Downstream `json:"downstream"`
}

func required(data []byte, fields ...string) error {
	var values map[string]jsontext.Value
	if err := json.Unmarshal(data, &values); err != nil {
		return errors.New("invalid sequence analysis JSON")
	}
	for _, field := range fields {
		if len(values[field]) == 0 || string(values[field]) == "null" {
			return errors.New("sequence analysis requires every declared member")
		}
	}
	return nil
}
func (w *Window) UnmarshalJSON(data []byte) error {
	if err := required(data, "source", "start", "end", "coverage"); err != nil {
		return err
	}
	var instants struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}
	if err := json.Unmarshal(data, &instants); err != nil || strings.HasSuffix(instants.Start, "-00:00") || strings.HasSuffix(instants.End, "-00:00") {
		return errors.New("observation windows require known UTC offsets")
	}
	type plain Window
	var decoded plain
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid observation window")
	}
	if decoded.TimeZone != "" {
		location, err := Zone(decoded.TimeZone)
		if err != nil || !InZone(decoded.Start, location) || !InZone(decoded.End, location) {
			return errors.New("an observation window's instants carry the offset of its declared time zone")
		}
	}
	*w = Window(decoded)
	return nil
}
func (r *Retry) UnmarshalJSON(data []byte) error {
	if err := required(data, "first", "retry", "basis"); err != nil {
		return err
	}
	type plain Retry
	var decoded plain
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid retry declaration")
	}
	*r = Retry(decoded)
	return nil
}
func (d *Downstream) UnmarshalJSON(data []byte) error {
	if err := required(data, "occurrence", "source", "rule"); err != nil {
		return err
	}
	type plain Downstream
	var decoded plain
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid downstream expectation")
	}
	*d = Downstream(decoded)
	return nil
}
func Parse(data []byte) (Declaration, error) {
	var d Declaration
	if len(data) > MaxBytes {
		return d, errors.New("sequence analysis exceeds 1 MiB")
	}
	if err := required(data, "schema", "case_identity", "rules_sha256", "clock_tolerance_seconds", "windows", "retries", "downstream"); err != nil {
		return d, err
	}
	if err := json.Unmarshal(data, &d, json.RejectUnknownMembers(true)); err != nil {
		return Declaration{}, errors.New("invalid sequence analysis document")
	}
	if d.Schema != Schema || d.CaseIdentity == "" || d.ClockToleranceSeconds < 0 || d.ClockToleranceSeconds > MaxClockToleranceSeconds || len(d.Windows) == 0 || len(d.Windows) > 128 || len(d.Retries) > 128 || len(d.Downstream) > 128 {
		return Declaration{}, errors.New("unsupported sequence analysis declaration or limits")
	}
	return d, nil
}

type Coverage struct {
	Source   string     `json:"source"`
	Coverage string     `json:"coverage"`
	Start    *time.Time `json:"start"`
	End      *time.Time `json:"end"`
	Outside  int        `json:"outside"`
	Untimed  int        `json:"untimed"`
}

// FindingKind is what one finding of the analysis says.
type FindingKind string

const (
	DuplicateOccurrence        FindingKind = "duplicate_occurrence"
	ClockMismatch              FindingKind = "clock_mismatch"
	ClockUnknown               FindingKind = "clock_unknown"
	AcceptACKStage             FindingKind = "accept_ack_stage"
	ApplicationACKStage        FindingKind = "application_ack_stage"
	ACKCoverageUnknown         FindingKind = "ack_coverage_unknown"
	MissingACK                 FindingKind = "missing_ack"
	LikelyRetransmission       FindingKind = "likely_retransmission"
	RetryUnresolved            FindingKind = "retry_unresolved"
	UnobservedDownstreamOutput FindingKind = "unobserved_downstream_output"
	DownstreamUnresolved       FindingKind = "downstream_unresolved"
	DownstreamLinkObserved     FindingKind = "downstream_link_observed"
)

type Finding struct {
	Kind       FindingKind `json:"kind"`
	Occurrence string      `json:"occurrence"`
	Related    string      `json:"related"`
	Source     string      `json:"source"`
	Detail     string      `json:"detail"`
}
type Report struct {
	ClockToleranceSeconds int        `json:"clock_tolerance_seconds"`
	TotalFindings         int        `json:"total_findings"`
	Coverage              []Coverage `json:"coverage"`
	Findings              []Finding  `json:"findings"`
	Boundary              string     `json:"boundary"`
}

func Evaluate(b *bundle.Bundle, d Declaration, links *correlate.Report) (*Report, error) {
	if len(d.Downstream) > 0 && (links == nil || d.RulesSHA256 == "" || d.RulesSHA256 != links.RulesSHA256) {
		return nil, errors.New("downstream expectations require the exact selected correlation rules digest")
	}
	if d.RulesSHA256 != "" && (links == nil || d.RulesSHA256 != links.RulesSHA256) {
		return nil, errors.New("sequence analysis correlation rules digest changed")
	}
	if d.CaseIdentity != b.Identity {
		return nil, errors.New("sequence analysis names a different case identity")
	}
	report := &Report{ClockToleranceSeconds: d.ClockToleranceSeconds, Coverage: []Coverage{}, Findings: []Finding{}, Boundary: "Observation windows and coverage are operator declarations, not independently verified capture completeness. Absence is missing evidence, never proof of a dropped message or failed downstream state. Times are not causality; import time is never an observed time. Automatic correlation rules alone inform this analysis; analyst review decisions are not applied. No clinical workflow or profile conformance is evaluated."}
	sources := map[string]bool{}
	for _, s := range b.Manifest.Sources {
		sources[s.ID] = true
	}
	windows := map[string]Window{}
	for _, w := range d.Windows {
		if !sources[w.Source] || !w.Start.Before(w.End) || w.Start.IsZero() || w.End.IsZero() || !slices.Contains(DeclaredCoverages(), w.Coverage) {
			return nil, errors.New("observation windows require a case source, increasing instants and partial or complete coverage")
		}
		if _, ok := windows[w.Source]; ok {
			return nil, errors.New("observation windows repeat a source")
		}
		windows[w.Source] = w
	}
	for _, s := range b.Manifest.Sources {
		c := Coverage{Source: s.ID, Coverage: "undeclared"}
		if w, ok := windows[s.ID]; ok {
			c.Coverage = string(w.Coverage)
			c.Start = &w.Start
			c.End = &w.End
		}
		for _, e := range b.Events {
			if e.SourceID != s.ID {
				continue
			}
			if e.ObservedAt == nil {
				c.Untimed++
			} else if c.Start != nil && (e.ObservedAt.Before(*c.Start) || e.ObservedAt.After(*c.End)) {
				c.Outside++
			}
		}
		report.Coverage = append(report.Coverage, c)
	}
	if err := explain(b, d, windows, report, links); err != nil {
		return nil, err
	}
	report.TotalFindings = len(report.Findings)
	return report, nil
}
