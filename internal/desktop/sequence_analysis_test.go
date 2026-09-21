package desktop_test

import (
	"encoding/json/v2"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSequenceAnalysisRequiresWindowsAndBindsEvidence(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	request := sequenceRequest(root, identity, seqRulesEntry)
	request.Analysis = "analysis.json"
	document := map[string]any{"schema": "readmit-sequence-analysis/v1", "rules_sha256": "", "case_identity": identity, "clock_tolerance_seconds": 5, "windows": []any{map[string]any{"source": "s0001", "start": "2026-01-01T12:00:00Z", "end": "2026-01-01T12:01:00Z", "coverage": "partial"}}, "retries": []any{}, "downstream": []any{}}
	data, _ := json.Marshal(document)
	if err := os.WriteFile(filepath.Join(root, request.Analysis), data, 0600); err != nil {
		t.Fatal(err)
	}
	result := app.OpenSequence(request)
	if result.Sequence == nil || result.Sequence.Analysis == nil {
		t.Fatalf("analysis missing: %+v", result)
	}
	if result.Sequence.Analysis.ClockToleranceSeconds != 5 {
		t.Fatal("clock assumption was not restated")
	}
	if len(result.Sequence.Analysis.Coverage) != 2 || result.Sequence.Analysis.Coverage[1].Coverage != "undeclared" {
		t.Fatalf("unstated window must stay unknown: %+v", result.Sequence.Analysis)
	}
	document["case_identity"] = "stale"
	data, _ = json.Marshal(document)
	_ = os.WriteFile(filepath.Join(root, request.Analysis), data, 0600)
	if got := app.OpenSequence(request); got.State != "failed" {
		t.Fatalf("stale analysis accepted: %+v", got)
	}
}

func TestSequenceExplainsDuplicatesWithoutInventingRetriesOrCausality(t *testing.T) {
	app, root, _ := sequenceWorkspace(t)
	message := strings.Replace(seqBooking, "20260101120000", "20260101120100+0000", 1)
	written := writeInputs(t, root, "duplicates", []bundle.Input{{Path: "private", Data: []byte(framed(message) + framed(message)), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}, Observations: map[int]bundle.Observation{1: {Direction: bundle.Outbound, ObservedAt: seqTime(1)}, 2: {Direction: bundle.Outbound, ObservedAt: seqTime(2)}}}})
	request := sequenceRequest(root, written.Identity, "")
	request.Case = "duplicates"
	request.Analysis = "analysis.json"
	document := map[string]any{"schema": "readmit-sequence-analysis/v1", "rules_sha256": "", "case_identity": written.Identity, "clock_tolerance_seconds": 5, "windows": []any{map[string]any{"source": "s0001", "start": "2026-01-01T12:00:00Z", "end": "2026-01-01T12:01:00Z", "coverage": "partial"}}, "retries": []any{}, "downstream": []any{}}
	save := func() {
		t.Helper()
		data, _ := json.Marshal(document)
		if err := os.WriteFile(filepath.Join(root, request.Analysis), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	save()
	result := app.OpenSequence(request)
	if result.Sequence == nil {
		t.Fatalf("refused: %+v", result)
	}
	kinds := map[string]int{}
	for _, f := range result.Sequence.Analysis.Findings {
		kinds[f.Kind]++
	}
	if kinds["duplicate_occurrence"] != 1 || kinds["likely_retransmission"] != 0 || kinds["clock_mismatch"] != 2 {
		t.Fatalf("wrong explanations: %+v", kinds)
	}
	document["retries"] = []any{map[string]any{"first": "s0001-e000001", "retry": "s0001-e000002", "basis": "operator_reported_retry"}}
	save()
	result = app.OpenSequence(request)
	found := false
	for _, f := range result.Sequence.Analysis.Findings {
		if f.Kind == "likely_retransmission" {
			found = true
		}
	}
	if !found {
		t.Fatalf("declared retry not explained: %+v", result.Sequence.Analysis)
	}
	encoded, _ := json.Marshal(result)
	for _, secret := range []string{"MRN-1", "DOE", "CTL-1", "private"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("leaked %q", secret)
		}
	}
}

func TestSequenceAnalysisDistinguishesUnknownClockAndDownstreamEvidence(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	req := sequenceRequest(root, identity, seqRulesEntry)
	req.Analysis = "analysis.json"
	document := map[string]any{"schema": "readmit-sequence-analysis/v1", "rules_sha256": "", "case_identity": identity, "clock_tolerance_seconds": 5, "windows": []any{map[string]any{"source": "s0001", "start": "2026-01-01T12:00:00Z", "end": "2026-01-01T12:01:00Z", "coverage": "partial"}, map[string]any{"source": "s0002", "start": "2026-01-01T12:00:00Z", "end": "2026-01-01T12:01:00Z", "coverage": "complete"}}, "retries": []any{}, "downstream": []any{map[string]any{"occurrence": "s0001-e000001", "source": "s0002", "rule": seqControlRule}}}
	document["rules_sha256"] = analysisRulesDigest(t, app, root, req.Case, req.Identity)
	save := func() {
		data, _ := json.Marshal(document)
		if err := os.WriteFile(filepath.Join(root, req.Analysis), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	save()
	result := app.OpenSequence(req)
	if result.Sequence == nil {
		t.Fatalf("refused: %+v", result)
	}
	kinds := map[string]int{}
	for _, f := range result.Sequence.Analysis.Findings {
		kinds[f.Kind]++
	}
	if kinds["downstream_link_observed"] != 1 || kinds["clock_unknown"] != 6 || kinds["clock_mismatch"] != 0 || kinds["ack_coverage_unknown"] == 0 {
		t.Fatalf("evidence limits lost: %+v", kinds)
	}
	if err := os.WriteFile(filepath.Join(root, seqRulesEntry), []byte(strings.Replace(seqRules, "PID-3.1", "PID-5.1", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if changed := app.OpenSequence(req); changed.State != desktop.Failed || !strings.Contains(changed.Reason, "digest") {
		t.Fatalf("changed named rules silently reused: %+v", changed)
	}
	if err := os.WriteFile(filepath.Join(root, seqRulesEntry), []byte(seqRules), 0600); err != nil {
		t.Fatal(err)
	}
	document["downstream"] = []any{map[string]any{"occurrence": "s0001-e000003", "source": "s0002", "rule": seqControlRule}}
	save()
	result = app.OpenSequence(req)
	for _, f := range result.Sequence.Analysis.Findings {
		if f.Kind == "downstream_unresolved" {
			return
		}
	}
	t.Fatalf("unparsed downstream silently treated as complete absence: %+v", result.Sequence.Analysis)
}

func TestSequenceAnalysisRefusalRecoveryAndWindowedACK(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	req := sequenceRequest(root, identity, "")
	req.Analysis = "analysis.json"
	document := `{"schema":"readmit-sequence-analysis/v1","case_identity":"` + identity + `","rules_sha256":"","clock_tolerance_seconds":5,"windows":[{"source":"s0001","start":"2026-01-01T12:00:00Z","end":"2026-01-01T12:00:02Z","coverage":"partial"}],"retries":[],"downstream":[]}`
	save := func(data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "analysis.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	save(document)
	before := evidenceDigest(t, filepath.Join(root, "incident"))
	for _, name := range []string{"../analysis.json", "missing.json", "incident"} {
		req.Analysis = name
		got := app.OpenSequence(req)
		if got.State != "failed" || got.Sequence != nil {
			t.Fatalf("invalid entry accepted: %+v", got)
		}
	}
	req.Analysis = "analysis.json"
	for _, data := range []string{
		strings.Replace(document, `"coverage":"partial"`, `"coverage":"patient"`, 1),
		strings.Replace(document, `"source":"s0001"`, `"source":"s9999"`, 1),
		strings.Replace(document, `12:00:02Z`, `11:00:02Z`, 1),
		strings.Replace(document, `"retries":[]`, `"retries":[{"first":"s9999","retry":"s8888","basis":"operator_reported_retry"}]`, 1),
		strings.Replace(document, `"downstream":[]`, `"downstream":[{"occurrence":"s0001-e000001","source":"s0001","rule":"absent"}]`, 1),
	} {
		save(data)
		got := app.OpenSequence(req)
		if got.State != "failed" || strings.Contains(got.Reason, "patient") {
			t.Fatalf("invalid declaration accepted or leaked: %+v", got)
		}
	}
	save(document)
	app.Cancel()
	got := app.OpenSequence(req)
	if got.Sequence == nil {
		t.Fatalf("read-only bounded recovery failed: %+v", got)
	}
	found := false
	for _, f := range got.Sequence.Analysis.Findings {
		if f.Kind == "missing_ack" && f.Occurrence == "s0001-e000001" {
			found = true
		}
	}
	if !found {
		t.Fatal("ACK after observation window incorrectly filled gap")
	}
	if after := evidenceDigest(t, filepath.Join(root, "incident")); after != before {
		t.Fatal("analysis changed evidence")
	}
	req.Offset = 1
	req.Limit = 1
	page := app.OpenSequence(req)
	for _, finding := range page.Sequence.Analysis.Findings {
		if finding.Occurrence != page.Sequence.Events[0].Occurrence {
			t.Fatal("findings were not windowed")
		}
	}
	if page.Sequence.Analysis.TotalFindings != got.Sequence.Analysis.TotalFindings {
		t.Fatal("paging changed total findings")
	}
}

func TestSequenceUnobservedOutputIsMissingEvidenceWithinDeclaredPartialWindow(t *testing.T) {
	app, root, _ := sequenceWorkspace(t)
	written := writeInputs(t, root, "partial", []bundle.Input{
		{Path: "sender", Data: []byte(framed(seqBooking)), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}, Observations: map[int]bundle.Observation{1: {Direction: bundle.Outbound, ObservedAt: seqTime(1)}}},
		{Path: "receiver", Data: []byte(framed(strings.ReplaceAll(seqBooking, "CTL-1", "CTL-OTHER"))), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}, Observations: map[int]bundle.Observation{1: {Direction: bundle.Inbound, ObservedAt: seqTime(2)}}},
	})
	req := sequenceRequest(root, written.Identity, seqRulesEntry)
	req.Case = "partial"
	req.Analysis = "analysis.json"
	data := `{"schema":"readmit-sequence-analysis/v1","case_identity":"` + written.Identity + `","rules_sha256":"","clock_tolerance_seconds":5,"windows":[{"source":"s0001","start":"2026-01-01T12:00:00Z","end":"2026-01-01T12:01:00Z","coverage":"partial"},{"source":"s0002","start":"2026-01-01T12:00:00Z","end":"2026-01-01T12:01:00Z","coverage":"partial"}],"retries":[],"downstream":[{"occurrence":"s0001-e000001","source":"s0002","rule":"same-message"}]}`
	if strings.Contains(data, `"rule":"same-message"`) {
		data = strings.Replace(data, `"rules_sha256":""`, `"rules_sha256":"`+analysisRulesDigest(t, app, root, req.Case, req.Identity)+`"`, 1)
	}
	if err := os.WriteFile(filepath.Join(root, req.Analysis), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	result := app.OpenSequence(req)
	if result.Sequence == nil {
		t.Fatalf("refused: %+v", result)
	}
	for _, finding := range result.Sequence.Analysis.Findings {
		if finding.Kind == "unobserved_downstream_output" {
			if !strings.Contains(finding.Detail, "not proof") {
				t.Fatal("absence became proof")
			}
			return
		}
	}
	t.Fatal("unobserved downstream output not distinguished")
}

func TestSequenceKeepsCollectionAcceptAndApplicationStagesDistinct(t *testing.T) {
	app, root, _ := sequenceWorkspace(t)
	policy, err := collection.DecodePolicy([]byte(`{"schema":"readmit-receiver-policy/v2","name":"sink","source_label":"sink","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]},"enhanced_acknowledgement":{"operator":"enhanced-mode-fixed-codes","accept_code":"CA","application_code":"AA","application_delivery":"same-connection","application_endpoint":"","approved_transport":false}}`))
	if err != nil {
		t.Fatal(err)
	}
	record := collection.Record{Schema: collection.Schema, SessionID: strings.Repeat("ab", 16), Policy: policy, ApplicationProcessing: collection.NoApplicationProcessing, Sessions: []collection.Session{{SessionID: "c0001", SourceID: "s0001", Label: "sink"}}, Received: []collection.Received{{SessionID: "c0001", OccurrenceID: "s0001-e000001", ControlID: "CTL-1", Mode: collection.EnhancedMode, Accept: collection.Stage{Code: "CA", ControlID: "PRIVATE-ACK-ID", Destination: collection.SameConnection}, Application: collection.Stage{Code: collection.NotAcknowledged, Destination: collection.NoDestination, Reason: "PRIVATE-REASON"}}}}
	message := strings.Replace(seqBooking, "2.5.1\r", "2.5.1|||AL|AL\r", 1)
	written, err := bundle.WriteCollected(filepath.Join(root, "collected"), []bundle.Input{{Data: []byte(framed(message)), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}, Observations: map[int]bundle.Observation{1: {Direction: bundle.Inbound, ObservedAt: seqTime(1)}}}}, *seqTime(0), record)
	if err != nil {
		t.Fatal(err)
	}
	req := sequenceRequest(root, written.Identity, "")
	req.Case = "collected"
	req.Analysis = "analysis.json"
	data := `{"schema":"readmit-sequence-analysis/v1","case_identity":"` + written.Identity + `","rules_sha256":"","clock_tolerance_seconds":5,"windows":[{"source":"s0001","start":"2026-01-01T12:00:00Z","end":"2026-01-01T12:01:00Z","coverage":"partial"}],"retries":[],"downstream":[]}`

	if err := os.WriteFile(filepath.Join(root, req.Analysis), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	result := app.OpenSequence(req)
	if result.Sequence == nil {
		t.Fatalf("refused: %+v", result)
	}
	kinds := map[string]int{}
	for _, f := range result.Sequence.Analysis.Findings {
		kinds[f.Kind]++
	}
	if kinds["accept_ack_stage"] != 1 || kinds["application_ack_stage"] != 1 || kinds["missing_ack"] != 0 || kinds["ack_coverage_unknown"] != 1 {
		t.Fatalf("ACK stages collapsed: %+v", kinds)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "PRIVATE-") {
		t.Fatal("stage identifier/reason leaked")
	}
}

func TestSequenceAnalysisHonorsDeclaredCrossSourceACKLink(t *testing.T) {
	app, root, _ := sequenceWorkspace(t)
	written := writeInputs(t, root, "crossack", []bundle.Input{
		{Path: "sender", Data: []byte(framed(seqBooking)), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}, Observations: map[int]bundle.Observation{1: {Direction: bundle.Outbound, ObservedAt: seqTime(1)}}},
		{Path: "ack", Data: []byte(framed(seqBookingACK)), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}, Observations: map[int]bundle.Observation{1: {Direction: bundle.Inbound, ObservedAt: seqTime(2)}}},
	})
	rules := `{"schema":"readmit-correlation-rules/v1","rules":[{"id":"cross-ack","operator":"acknowledges","scope":"declared","sources":["s0001","s0002"]}]}`
	if err := os.WriteFile(filepath.Join(root, "cross.rules.json"), []byte(rules), 0600); err != nil {
		t.Fatal(err)
	}
	req := sequenceRequest(root, written.Identity, "cross.rules.json")
	req.Case = "crossack"
	req.Analysis = "analysis.json"
	data := `{"schema":"readmit-sequence-analysis/v1","case_identity":"` + written.Identity + `","rules_sha256":"","clock_tolerance_seconds":5,"windows":[{"source":"s0001","start":"2026-01-01T12:00:00Z","end":"2026-01-01T12:01:00Z","coverage":"partial"},{"source":"s0002","start":"2026-01-01T12:00:00Z","end":"2026-01-01T12:01:00Z","coverage":"partial"}],"retries":[],"downstream":[]}`

	if err := os.WriteFile(filepath.Join(root, req.Analysis), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	result := app.OpenSequence(req)
	if result.Sequence == nil {
		t.Fatalf("refused: %+v", result)
	}
	for _, finding := range result.Sequence.Analysis.Findings {
		if finding.Kind == "missing_ack" {
			t.Fatal("selected rule linked cross-source ACK but analysis claimed missing")
		}
	}
	ambiguous := writeInputs(t, root, "ambiguous-ack", []bundle.Input{
		{Path: "sender", Data: []byte(framed(seqBooking) + framed(seqBooking)), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}, Observations: map[int]bundle.Observation{1: {Direction: bundle.Outbound, ObservedAt: seqTime(1)}, 2: {Direction: bundle.Outbound, ObservedAt: seqTime(2)}}},
		{Path: "ack", Data: []byte(framed(seqBookingACK)), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}, Observations: map[int]bundle.Observation{1: {Direction: bundle.Inbound, ObservedAt: seqTime(3)}}},
	})
	req.Case = "ambiguous-ack"
	req.Identity = ambiguous.Identity
	data = strings.Replace(data, written.Identity, ambiguous.Identity, 1)
	if err := os.WriteFile(filepath.Join(root, req.Analysis), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	result = app.OpenSequence(req)
	if result.Sequence == nil {
		t.Fatalf("refused ambiguous case: %+v", result)
	}
	unresolved := 0
	for _, finding := range result.Sequence.Analysis.Findings {
		if finding.Kind == "missing_ack" {
			t.Fatal("ambiguous ACK claimed missing")
		}
		if finding.Kind == "ack_coverage_unknown" {
			unresolved++
		}
	}
	if unresolved != 2 {
		t.Fatalf("ambiguous message candidates not left unresolved: %d", unresolved)
	}

}

func analysisRulesDigest(t *testing.T, app *desktop.App, root, name, identity string) string {
	t.Helper()
	request := sequenceRequest(root, identity, seqRulesEntry)
	request.Case = name
	result := app.OpenSequence(request)
	if result.Sequence == nil {
		t.Fatalf("rules sequence: %+v", result)
	}
	return result.Sequence.RulesSHA256
}
