package report

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/runcompare"
	"github.com/bharm16/readmit/internal/runexplain"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The structured report (#559): one typed document built from a sealed
// retained packet and the report's authored title and notes, from which every
// format is rendered. It restates what the retained evidence establishes and
// adds nothing: no conclusion is derived, an unobserved value stays
// unavailable with its reason, and a run whose lifecycle is not usable never
// reads as passed whatever its checks decided.

const (
	// DocumentSchema is the structured report every v3 rendering is made
	// from. readmit-portable-report/v1 remains the line-based document of a
	// v1 review.
	DocumentSchema = "readmit-portable-report/v3"
	// AuthoredSchema is what a person wrote for a report: its title and
	// notes. It never carries a run outcome.
	AuthoredSchema = "readmit-report-authored/v1"

	// MaxTitleBytes and MaxNotesBytes bound what a person writes.
	MaxTitleBytes = 400
	MaxNotesBytes = 64 << 10
)

// Authored is what a person wrote for a report, stored beside the evidence
// and never inside it.
type Authored struct {
	Schema string `json:"schema"`
	Title  string `json:"title"`
	Notes  string `json:"notes"`
}

// DecodeAuthored reads an authored document strictly.
func DecodeAuthored(data []byte) (Authored, error) {
	var authored Authored
	invalid := errors.New("invalid report title or notes document")
	if len(data) > MaxNotesBytes+MaxTitleBytes+4096 || json.Unmarshal(data, &authored, json.RejectUnknownMembers(true)) != nil || authored.Schema != AuthoredSchema {
		return Authored{}, invalid
	}
	if err := authored.Validate(); err != nil {
		return Authored{}, err
	}
	return authored, nil
}

// Validate reports the first reason an authored document cannot be kept.
func (a Authored) Validate() error {
	switch {
	case strings.TrimSpace(a.Title) == "":
		return errors.New("a report has a title")
	case len(a.Title) > MaxTitleBytes || strings.ContainsAny(a.Title, "\r\n"):
		return errors.New("a report title is one line of at most 400 bytes")
	case len(a.Notes) > MaxNotesBytes:
		return errors.New("report notes are at most 64 KiB")
	case !utf8.ValidString(a.Title) || !utf8.ValidString(a.Notes):
		return errors.New("a report title and notes are text")
	}
	return nil
}

// EncodeAuthored writes an authored document canonically.
func EncodeAuthored(a Authored) ([]byte, error) {
	a.Schema = AuthoredSchema
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return encode(a)
}

// Outcome is one run's result as a report states it. Passed only when the
// run decided pass and its lifecycle is usable; an execution error is Error,
// and a journal that is incomplete, an uncertain delivery or an undecided
// state is Incomplete, whatever the checks decided.
type Outcome string

const (
	OutcomePassed     Outcome = "passed"
	OutcomeFailed     Outcome = "failed"
	OutcomeError      Outcome = "error"
	OutcomeIncomplete Outcome = "incomplete"
)

// DocumentResult is one run's outcome and the lifecycle facts beside it.
type DocumentResult struct {
	Outcome           Outcome `json:"outcome"`
	Status            string  `json:"status"`
	ErrorClass        string  `json:"error_class"`
	RunState          string  `json:"run_state"`
	JournalIncomplete bool    `json:"journal_incomplete"`
	DeliveryUncertain bool    `json:"delivery_uncertain"`
}

// OutcomeOf is the outcome a report states for one retained run's facts.
func OutcomeOf(status, runState string, journalIncomplete, deliveryUncertain bool) Outcome {
	if usable, _ := runresult.UsableLifecycle(runState, journalIncomplete, deliveryUncertain); !usable {
		return OutcomeIncomplete
	}
	switch testrunner.Status(status) {
	case testrunner.Pass:
		return OutcomePassed
	case testrunner.AssertionFailure:
		return OutcomeFailed
	case testrunner.ExecutionError:
		return OutcomeError
	}
	return OutcomeIncomplete
}

func resultOf(run RetainedRun) DocumentResult {
	return DocumentResult{Outcome: OutcomeOf(run.Status, run.RunState, run.JournalIncomplete, run.DeliveryUncertain), Status: run.Status,
		ErrorClass: run.ErrorClass, RunState: run.RunState, JournalIncomplete: run.JournalIncomplete, DeliveryUncertain: run.DeliveryUncertain}
}

// DocumentRun is one run a report includes, with the exact versions it
// executed.
type DocumentRun struct {
	Role           string         `json:"role"`
	Test           string         `json:"test"`
	Result         DocumentResult `json:"result"`
	StartedAt      string         `json:"started_at"`
	CompletedAt    string         `json:"completed_at"`
	Boundary       string         `json:"boundary"`
	ResultIdentity string         `json:"result_identity"`
	SpecIdentity   string         `json:"spec_identity"`
	CaseIdentity   string         `json:"case_identity"`
	CaseProvenance string         `json:"case_provenance"`
	TargetIdentity string         `json:"target_identity"`
}

// Roles of the runs a report includes.
const (
	CurrentRole    = "current"
	ComparisonRole = "comparison"
)

// DocumentCheck is one check of the current run: its definition, what it
// expected and observed, and the result. Observed is null when nothing was
// observed, and Unavailable then says why; it is never zero.
type DocumentCheck struct {
	ID          string            `json:"id"`
	Operator    string            `json:"operator"`
	Message     string            `json:"message"`
	Selector    string            `json:"selector"`
	Result      string            `json:"result"`
	Expected    testrunner.Value  `json:"expected"`
	Observed    *testrunner.Value `json:"observed"`
	Unavailable string            `json:"unavailable"`
	Messages    []string          `json:"messages"`
}

// DocumentChange is one check compared across the comparison run (before)
// and the current run (after). Definition is unchanged, changed, added or
// removed; a changed definition is never compared as a behavior.
type DocumentChange struct {
	ID             string            `json:"id"`
	Operator       string            `json:"operator"`
	Message        string            `json:"message"`
	Selector       string            `json:"selector"`
	Definition     string            `json:"definition"`
	Before         string            `json:"before"`
	After          string            `json:"after"`
	BeforeObserved *testrunner.Value `json:"before_observed"`
	AfterObserved  *testrunner.Value `json:"after_observed"`
}

// DocumentComparison is what the comparison run establishes against the
// current one, from the two retained runs alone.
type DocumentComparison struct {
	SameCase      bool             `json:"same_case"`
	SameTarget    bool             `json:"same_target"`
	Specification string           `json:"specification"`
	Checks        []DocumentChange `json:"checks"`
}

// DocumentMessage is one message the current run sent: its type as the case
// declares it, its delivery and the acknowledgement code recorded.
type DocumentMessage struct {
	Source   string `json:"source"`
	Kind     string `json:"kind"`
	Code     string `json:"code"`
	Trigger  string `json:"trigger"`
	Delivery string `json:"delivery"`
	ACKCode  string `json:"ack_code"`
	Outcome  string `json:"outcome"`
}

// DocumentEvidence is one verified item the report is made from, by its
// typed kind and its place inside the sealed packet. Paths are inert
// references, never locations to open.
type DocumentEvidence struct {
	Kind   string `json:"kind"`
	Role   string `json:"role"`
	Source string `json:"source"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Document is the structured report. Notes is null when the person wrote
// none; Comparison is null when no comparison run was included.
type Document struct {
	Schema               string              `json:"schema"`
	Title                string              `json:"title"`
	PacketIdentity       string              `json:"packet_identity"`
	ExportPolicy         string              `json:"export_policy"`
	ContainsSourceValues bool                `json:"contains_source_values"`
	Result               DocumentResult      `json:"result"`
	Runs                 []DocumentRun       `json:"runs"`
	Checks               []DocumentCheck     `json:"checks"`
	Comparison           *DocumentComparison `json:"comparison"`
	Messages             []DocumentMessage   `json:"messages"`
	Notes                *string             `json:"notes"`
	Limitations          []string            `json:"limitations"`
	Evidence             []DocumentEvidence  `json:"evidence"`
}

// BuildDocument reads the structured report of one verified retained packet
// under what a person wrote for it. dir is the packet's folder and packet
// its verification.
func BuildDocument(ctx context.Context, dir string, packet *RetainedPacket, authored Authored) (*Document, error) {
	verified, err := packet.verified()
	if err != nil {
		return nil, err
	}
	if filepath.Clean(dir) != verified.snapshot.directory {
		resolved, err := artifactpath.Resolve(dir)
		if err != nil || resolved != verified.snapshot.directory {
			return nil, errors.New("the folder does not name this verified retained packet")
		}
	}
	return verified.Document(ctx, authored)
}

// Document constructs a report from the owned reading, independent of later
// source replacement, relocation or deletion. Summary fields cannot rewrite it.
func (packet *RetainedPacket) Document(ctx context.Context, authored Authored) (*Document, error) {
	packet, err := packet.verified()
	if err != nil {
		return nil, err
	}
	if err := authored.Validate(); err != nil {
		return nil, err
	}
	files, current, source := packet.snapshot.files, packet.snapshot.current, packet.snapshot.source
	doc := &Document{
		Schema: DocumentSchema, Title: authored.Title, PacketIdentity: packet.Identity,
		ExportPolicy: packet.Manifest.ExportPolicy, ContainsSourceValues: packet.Manifest.ContainsSourceValues,
		Result: resultOf(packet.Manifest.Current), Checks: []DocumentCheck{}, Messages: []DocumentMessage{},
		Limitations: []string{}, Evidence: []DocumentEvidence{},
	}
	if strings.TrimSpace(authored.Notes) != "" {
		notes := authored.Notes
		doc.Notes = &notes
	}
	doc.Runs = append(doc.Runs, documentRun(CurrentRole, packet.Manifest.Current, current))
	doc.Messages = documentMessages(current, source)
	doc.Checks = documentChecks(current, doc.Messages)
	doc.Evidence = append(doc.Evidence, documentEvidence(files, CurrentRole, "case", "current", current)...)
	if packet.Manifest.Baseline != nil {
		baseline := packet.snapshot.baseline
		doc.Runs = append(doc.Runs, documentRun(ComparisonRole, *packet.Manifest.Baseline, baseline))
		comparison, err := runcompare.CompareOpened(ctx, runcompare.OpenedInput{Baseline: baseline, Current: current})
		if err != nil {
			return nil, err
		}
		doc.Comparison = documentComparison(*packet.Manifest.Baseline, packet.Manifest.Current, comparison, baseline, current)
		doc.Evidence = append(doc.Evidence, documentEvidence(files, ComparisonRole, "baseline-case", "baseline", baseline)...)
	}
	doc.Limitations = documentLimitations(packet, current)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Values contain pointer unions and nested record maps. Return a detached
	// document so a caller cannot rewrite the privately verified run reading.
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var detached Document
	if err := json.Unmarshal(raw, &detached); err != nil {
		return nil, err
	}
	return &detached, nil
}

func documentRun(role string, facts RetainedRun, opened *runresult.Result) DocumentRun {
	run := DocumentRun{Role: role, Result: resultOf(facts), Boundary: facts.Boundary, ResultIdentity: facts.Identity, SpecIdentity: facts.SpecIdentity,
		CaseIdentity: facts.CaseIdentity, CaseProvenance: facts.CaseProvenance, TargetIdentity: facts.TargetIdentity}
	if opened.Spec != nil {
		run.Test = opened.Spec.Name
	}
	if opened.Run != nil {
		run.StartedAt = stamp(opened.Run.Manifest.StartedAt)
		run.CompletedAt = stamp(opened.Run.Manifest.CompletedAt)
	}
	return run
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// documentMessages are the messages the current run sent, typed from the
// retained case.
func documentMessages(current *runresult.Result, source *bundle.Bundle) []DocumentMessage {
	messages := []DocumentMessage{}
	if current.Run == nil {
		return messages
	}
	described, err := runexplain.DescribeRun(current.Run)
	if err != nil {
		return messages
	}
	types := messageTypes(source)
	for _, message := range described.Messages {
		declared := types[message.Source]
		messages = append(messages, DocumentMessage{Source: message.Source, Kind: declared.kind, Code: declared.code, Trigger: declared.trigger,
			Delivery: deliveryOf(message.Delivery), ACKCode: message.ACKCode, Outcome: string(message.Outcome)})
	}
	return messages
}

type messageType struct{ kind, code, trigger string }

// messageTypes reads each occurrence's MSH-9 message code and trigger event.
func messageTypes(source *bundle.Bundle) map[string]messageType {
	types := map[string]messageType{}
	code, _ := hl7.ParseSelector("MSH-9.1")
	trigger, _ := hl7.ParseSelector("MSH-9.2")
	for _, event := range source.Events {
		declared := messageType{kind: string(event.Kind)}
		if event.ParseError == "" && event.Kind != bundle.Unparsed {
			if document, err := source.Document(event); err == nil {
				declared.code, declared.trigger = typePart(document, code), typePart(document, trigger)
			}
		}
		types[event.ID] = declared
	}
	return types
}

func typePart(document *hl7.Document, selector hl7.Selector) string {
	value, err := document.Select(0, selector)
	if err != nil || value.State != hl7.Present {
		return ""
	}
	raw := document.Bytes(value.Span)
	if len(raw) > 64 {
		raw = raw[:64]
	}
	return Escape(string(raw))
}

// Deliveries a report states.
const (
	DeliveryAcknowledged = "acknowledged"
	DeliveryUncertain    = "uncertain"
	DeliveryNotAttempted = "not_attempted"
)

func deliveryOf(delivery string) string {
	switch delivery {
	case "acknowledged", "uncertain":
		return delivery
	}
	return DeliveryNotAttempted
}

// documentChecks are the current run's checks, failed and undecided first,
// then in the order the test declares them.
func documentChecks(current *runresult.Result, messages []DocumentMessage) []DocumentCheck {
	checks := []DocumentCheck{}
	artifact := current.Artifact
	readable := map[string]bool{}
	for _, message := range messages {
		readable[message.Source] = message.Delivery == DeliveryAcknowledged
	}
	for _, retained := range current.Assertions {
		declared := retained.Assertion
		check := DocumentCheck{ID: declared.ID, Operator: declared.Operator, Message: declared.Message, Selector: declared.Selector,
			Result: retained.Status, Expected: declared.Expected, Messages: supportingMessages(declared, messages)}
		if retained.Observed != nil {
			observed := *retained.Observed
			check.Observed = &observed
		} else {
			check.Unavailable = unavailableReason(declared, artifact, readable)
		}
		checks = append(checks, check)
	}
	slices.SortStableFunc(checks, func(x, y DocumentCheck) int { return cmp.Compare(checkRank(x.Result), checkRank(y.Result)) })
	return checks
}

func checkRank(result string) int {
	switch result {
	case testrunner.Failed:
		return 0
	case testrunner.NotEvaluated:
		return 1
	}
	return 2
}

// ACKCheck is the operator of an acknowledgement field check.
const ACKCheck = "ack_field_equals"

func unavailableReason(declared testrunner.Assertion, artifact *testrunner.Artifact, readable map[string]bool) string {
	switch {
	case artifact.Result.ErrorClass != "":
		return "the run stopped with an error before this check was evaluated"
	case declared.Operator == ACKCheck && !readable[declared.Message]:
		return "no readable acknowledgement was retained for this message"
	case declared.Operator != ACKCheck && artifact.FinalObservation == nil:
		return "the appointment records after the run were not observed"
	}
	return "the run retained no observed value for this check"
}

func supportingMessages(declared testrunner.Assertion, messages []DocumentMessage) []string {
	if declared.Operator == ACKCheck {
		return []string{declared.Message}
	}
	sources := []string{}
	for _, message := range messages {
		if message.Source != "" {
			sources = append(sources, message.Source)
		}
	}
	return sources
}

func documentComparison(before, after RetainedRun, comparison runcompare.Comparison, baseline, current *runresult.Result) *DocumentComparison {
	out := &DocumentComparison{SameCase: before.CaseIdentity == after.CaseIdentity, SameTarget: before.TargetIdentity == after.TargetIdentity,
		Specification: comparison.Specification, Checks: []DocumentChange{}}
	declared := map[string]testrunner.AssertionResult{}
	for _, result := range baseline.Assertions {
		declared["before\x00"+result.Assertion.ID] = result
	}
	for _, result := range current.Assertions {
		declared["after\x00"+result.Assertion.ID] = result
	}
	for _, row := range comparison.Assertions {
		change := DocumentChange{ID: row.ID, Definition: row.Definition, Before: row.Baseline, After: row.Current}
		left, hasLeft := declared["before\x00"+row.ID]
		right, hasRight := declared["after\x00"+row.ID]
		switch {
		case hasRight:
			change.Operator, change.Message, change.Selector = right.Assertion.Operator, right.Assertion.Message, right.Assertion.Selector
		case hasLeft:
			change.Operator, change.Message, change.Selector = left.Assertion.Operator, left.Assertion.Message, left.Assertion.Selector
		}
		if hasLeft {
			change.BeforeObserved = left.Observed
		}
		if hasRight {
			change.AfterObserved = right.Observed
		}
		out.Checks = append(out.Checks, change)
	}
	return out
}

// documentEvidence names the typed items one run of the packet is made from.
func documentEvidence(files map[string][]byte, role, casePrefix, runPrefix string, opened *runresult.Result) []DocumentEvidence {
	evidence := []DocumentEvidence{}
	resultPrefix := retainedResultPrefix(files, runPrefix)
	add := func(kind, source, name string) {
		if data, held := files[name]; held {
			evidence = append(evidence, DocumentEvidence{Kind: kind, Role: role, Source: source, Path: name, SHA256: digest(data)})
		}
	}
	add("case", "", casePrefix+"/manifest.json")
	add("specification", "", resultPrefix+"/spec.json")
	add("result", "", resultPrefix+"/result.json")
	artifact := opened.Artifact
	if artifact.Result.InitialObservation != nil {
		add("initial-observation", "", path.Join(resultPrefix, artifact.Result.InitialObservation.Path))
	}
	if artifact.Result.FinalObservation != nil {
		add("final-observation", "", path.Join(resultPrefix, artifact.Result.FinalObservation.Path))
	}
	if artifact.Result.Run != nil && opened.Run != nil {
		runDir := path.Join(resultPrefix, artifact.Result.Run.Path)
		for _, event := range opened.Run.Events {
			if event.Sent.Path != "" {
				add("sent-message", event.SourceOccurrence, path.Join(runDir, event.Sent.Path))
			}
			if event.Received.Path != "" {
				add("received-message", event.SourceOccurrence, path.Join(runDir, event.Received.Path))
			}
		}
	}
	return evidence
}

func documentLimitations(packet *RetainedPacket, current *runresult.Result) []string {
	limitations := []string{}
	manifest := packet.Manifest
	if manifest.Baseline == nil {
		limitations = append(limitations, "No comparison run is included; this report shows no before and after change.")
	} else {
		sameCase := manifest.Baseline.CaseIdentity == manifest.Current.CaseIdentity
		sameTarget := manifest.Baseline.TargetIdentity == manifest.Current.TargetIdentity
		switch {
		case sameCase && sameTarget:
			limitations = append(limitations, "The comparison run used the same case and target configuration; the outcomes differ only as the retained evidence records.")
		case sameCase:
			limitations = append(limitations, "The comparison run used the same case; the target configuration changed between the two runs.")
		default:
			limitations = append(limitations, "The comparison run sent a different case; the comparison spans changed inputs.")
		}
	}
	switch current.Artifact.Result.ObservationBoundary {
	case testrunner.ACKBoundary:
		limitations = append(limitations, "The appointment records were not observed by this acknowledgement-only test.")
	case testrunner.LedgerBoundary:
		if current.Artifact.FinalObservation == nil {
			limitations = append(limitations, "The appointment records after the run were not observed.")
		}
	}
	limitations = append(limitations,
		"Acknowledgements do not establish downstream clinical behavior; target software revisions and external setup remain unverified.",
		"Contains original source values; it is not de-identified and no disclosure is approved.",
		"Hashes establish integrity, not source authenticity, approval or causality.")
	return limitations
}

// Escape is a value as every rendering shows it: printable text kept as it
// is, a backslash doubled, and every control character, invalid byte and
// Unicode format or separator character written as a visible escape, so no
// value changes meaning, hides text or breaks a layout.
func Escape(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); {
		r, size := utf8.DecodeRuneInString(value[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			out.WriteString(`\x` + hexByte(value[i]))
		case r == '\\':
			out.WriteString(`\\`)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == 0x2028 || r == 0x2029 || (r >= 0x200b && r <= 0x200f) ||
			(r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0xfeff:
			out.WriteString(`\u{` + hexRune(r) + `}`)
		default:
			out.WriteString(value[i : i+size])
		}
		i += size
	}
	return out.String()
}

func hexByte(b byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[b>>4], digits[b&15]})
}

// hexRune is a code point in lowercase hexadecimal, at least four digits.
func hexRune(r rune) string {
	out := ""
	for shift := 20; shift >= 0; shift -= 4 {
		out += string("0123456789abcdef"[(r>>shift)&15])
	}
	return strings.TrimPrefix(strings.TrimPrefix(out, "0"), "0")
}
