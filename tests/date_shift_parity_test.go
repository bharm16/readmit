package tests

import (
	"encoding/json/v2"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/transform"
)

// Every message below is synthetic. Two appointment repetitions, an offset and
// a local timestamp, an explicit-null endpoint, and an SCH that declares no
// timing at all.
var shiftedMessages = []string{
	"MSH|^~\\&|SCHEDULE|SITE|RECEIVER|LAB|20260101120000||SIU^S12|MSG-001|P|2.5.1\r" +
		"SCH|APPT-001^LAB|FILL-001^LAB|||||||||^^^20260102100000+0000^20260102103000+0000~^^^20260103100000^\"\"\r" +
		"PID|1||PATIENT^^^LAB\r",
	"MSH|^~\\&|SCHEDULE|SITE|RECEIVER|LAB|20260101120100-0500||SIU^S13|MSG-002|P|2.5.1\r" +
		"SCH|APPT-001^LAB|FILL-001^LAB\r" +
		"SCH|APPT-002^LAB|FILL-002^LAB|||||||||^^^20260104090000\r" +
		"PID|1||PATIENT^^^LAB\r",
}

func shiftedCase(t *testing.T, messages ...string) string {
	t.Helper()
	inputs := make([]bundle.Input, len(messages))
	for i, message := range messages {
		inputs[i] = bundle.Input{Path: "SYNTHETIC-SHIFT", Data: []byte(message)}
	}
	imported := time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "case")
	if _, err := bundle.Write(path, inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported}); err != nil {
		t.Fatal(err)
	}
	return path
}

// previewShift previews one date shift of the case, and prepareShift prepares
// the replay of it, each through its own public entry point.
func previewShift(t *testing.T, path, shift string) (transform.Preview, error) {
	t.Helper()
	rules, err := correlate.ParseRules([]byte(`{"schema":"readmit-correlation-rules/v1","rules":[{"id":"same-message","operator":"control-id","scope":"source"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	report, err := correlate.Run(path, rules)
	if err != nil {
		t.Fatal(err)
	}
	document, err := json.Marshal(transform.Plan{Schema: transform.PlanSchema, Case: report.CaseIdentity, Rules: report.RulesSHA256,
		Steps: []transform.Step{{Operator: transform.ShiftDates, Shift: shift}}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := transform.DecodePlan(document)
	if err != nil {
		return transform.Preview{}, err
	}
	return transform.Run(path, plan, rules, nil)
}

func prepareShift(path, shift string) (*replay.Plan, error) {
	target := replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: "127.0.0.1:2575", Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "1s", MaxACKBytes: 4096}
	return replay.Prepare(path, target, replay.Options{Transformations: []replay.Transformation{{Name: "shift-timestamps", Shift: shift}}})
}

// A transform preview's date shift and a replay's --shift move the same
// positions of the same occurrences to the same bytes, and refuse the same
// durations and the same timestamps, because both ask the one definition in
// internal/hl7. The preview reports positions and lengths, never values; the
// replay records the bytes it would send, and those are what
// hl7.ShiftTimestamps yields.
func TestTransformPreviewAndReplayShiftByTheSharedDefinition(t *testing.T) {
	path := shiftedCase(t, shiftedMessages...)
	source, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, shift := range []string{"24h", "-90m", "87600h"} {
		preview, err := previewShift(t, path, shift)
		if err != nil {
			t.Fatalf("%s preview: %v", shift, err)
		}
		plan, err := prepareShift(path, shift)
		if err != nil {
			t.Fatalf("%s replay: %v", shift, err)
		}
		previewed := map[string]int{}
		for _, change := range preview.Changes {
			if change.Operator == transform.ShiftDates && change.State == hl7.Present {
				previewed[change.Parent+" "+change.Selector] = change.Length
			}
		}
		sent := map[string]string{}
		for _, change := range plan.Changes() {
			sent[change.SourceOccurrence+" "+change.Selector] = string(change.New)
		}
		by, err := hl7.ParseShift(shift)
		if err != nil {
			t.Fatal(err)
		}
		defined := map[string]string{}
		for _, event := range source.Events {
			raw, err := source.Raw(event.ID)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := hl7.Parse(raw, hl7.Options{})
			if err != nil {
				t.Fatal(err)
			}
			edits, err := doc.ShiftTimestamps(0, by)
			if err != nil {
				t.Fatal(err)
			}
			for _, edit := range edits {
				defined[event.ID+" "+edit.Selector.String()] = string(edit.Value)
			}
		}
		if len(defined) != 6 || len(previewed) != len(defined) || len(sent) != len(defined) {
			t.Fatalf("%s: defined %v, previewed %v, sent %v", shift, defined, previewed, sent)
		}
		for position, value := range defined {
			if sent[position] != value || previewed[position] != len(value) {
				t.Fatalf("%s %s: defined %q, sent %q, previewed %d bytes", shift, position, value, sent[position], previewed[position])
			}
		}
	}
	// The same durations are refused by both, each in its own words for the
	// one duration rule.
	for _, shift := range []string{"0s", "1.5s", "87600h1s", "one day"} {
		if _, err := transform.ParseShift(shift); err == nil || !strings.Contains(err.Error(), "whole-second duration within ten years") {
			t.Fatalf("the preview's refusal of %q: %v", shift, err)
		}
		if _, err := prepareShift(path, shift); err == nil || !strings.Contains(err.Error(), "whole seconds within ten years") {
			t.Fatalf("the replay's refusal of %q: %v", shift, err)
		}
	}
	// And the same timestamps: one the shift cannot move, and one it would move
	// out of years 1 to 9999, each refused by name.
	for _, test := range []struct{ from, to, preview, replay string }{
		{"20260101120000||SIU^S12", "202601011200||SIU^S12", "whole-second MSH-7 and SCH-11.4/5", "whole-second MSH-7 and SCH-11.4/5"},
		{"20260104090000", "99991231090000", "outside the supported year range", "exceeds supported year range"},
	} {
		refused := shiftedCase(t, strings.Replace(shiftedMessages[0], test.from, test.to, 1), strings.Replace(shiftedMessages[1], test.from, test.to, 1))
		if _, err := previewShift(t, refused, "24h"); err == nil || !strings.Contains(err.Error(), test.preview) {
			t.Fatalf("the preview's refusal of %q: %v", test.to, err)
		}
		if _, err := prepareShift(refused, "24h"); err == nil || !strings.Contains(err.Error(), test.replay) {
			t.Fatalf("the replay's refusal of %q: %v", test.to, err)
		}
	}
}
