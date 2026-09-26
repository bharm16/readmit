package redact

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/testrunner"
)

// FuzzDerive states derive's whole claim as properties over arbitrary case
// bytes and policies, so none of them can be lost by widening the engine.
//
// A derivation that completes either reports every populated byte it did not
// rewrite — the finding lands at the field, the segment or the whole
// occurrence — or refuses; a fully handled derivation's correlations are the
// source's own, which is the gate seal holds the review to; and whatever
// derived bytes and spec it produces read back through the public readers.
func FuzzDerive(f *testing.F) {
	for _, name := range []string{"redact-booking.mllp", "redact-reschedule.mllp"} {
		raw, err := os.ReadFile("../../testdata/fixtures/" + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(raw), "")
	}
	f.Add("\x0bPLANTED-FRAME\r\x1c\r", "")
	f.Add("", "")
	policy, err := os.ReadFile("../../testdata/fixtures/redact-policy.json")
	if err != nil {
		f.Fatal(err)
	}
	fixturePolicy := string(policy)
	for _, seed := range []string{"booking", "reschedule"} {
		raw, err := os.ReadFile("../../testdata/fixtures/redact-" + seed + ".mllp")
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(raw), fixturePolicy)
	}
	f.Add("\x0bMSH|^~\\&|PLANTED|APP\r", fixturePolicy)

	f.Fuzz(func(t *testing.T, frames, policyDocument string) {
		// A mutated policy that no longer decodes does not starve the case
		// bytes: they are still derived against the shipped policy, so both
		// inputs stay exercised.
		wanted, err := DecodePolicy([]byte(policyDocument))
		if err != nil {
			if wanted, err = DecodePolicy([]byte(fixturePolicy)); err != nil {
				t.Fatal(err)
			}
		}
		root := t.TempDir()
		imported := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
		source, err := bundle.Write(filepath.Join(root, "case"), []bundle.Input{{Path: "fuzz-source.mllp", Data: []byte(frames), Options: hl7.Options{Format: hl7.MLLP}}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported})
		if err != nil {
			return
		}
		spec, err := testrunner.ReadSpec("../../testdata/fixtures/redact-spec.json")
		if err != nil {
			t.Fatal(err)
		}
		derived, err := derive(deriveInputs{Case: source, CasePath: filepath.Join(root, "case"), Spec: spec, Policy: wanted})
		if err != nil {
			return
		}
		reportPopulatedBytes(t, source, derived.Findings)

		written, err := bundle.Write(filepath.Join(root, "derived"), derived.Occurrences, bundle.Provenance{Mode: bundle.Derived, Derivation: "readmit-redact/v1"})
		if err != nil {
			t.Fatalf("derived occurrences do not read back: %v", err)
		}
		if !hasUnresolved(derived.Findings) && !reflect.DeepEqual(written.Correlations, source.Correlations) {
			t.Fatal("a fully handled derivation changed source correlations")
		}
		specBytes, err := encode(derived.Spec)
		if err != nil {
			t.Fatalf("a derived spec could not be written back: %v", err)
		}
		if _, err := testrunner.DecodeSpec(specBytes); err != nil {
			t.Fatalf("a derived spec does not read back: %v", err)
		}
	})
}

// reportPopulatedBytes checks, occurrence by occurrence, that every populated
// byte derive left unrewritten is reported unresolved: a finding must sit at
// the whole occurrence, at the segment, or at the populated field. It is the
// same obligation the engine's own unmapped-field scan owes, read back here
// from the outside so a refactor cannot quietly drop it.
func reportPopulatedBytes(t *testing.T, source *bundle.Bundle, findings []exportreview.Finding) {
	t.Helper()
	reported := make(map[string]bool, len(findings))
	for _, finding := range findings {
		reported[finding.Location] = true
	}
	for _, event := range source.Events {
		location := "case/" + event.ID
		if event.Kind == bundle.Unparsed {
			if !reported[location] {
				t.Fatalf("an unparsed occurrence was retained without its finding at %s", location)
			}
			continue
		}
		raw, err := source.Raw(event.ID)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := hl7.Parse(raw, hl7.Options{Terminator: event.Terminator})
		if err != nil {
			t.Fatalf("a verified occurrence no longer parses: %v", err)
		}
		message := doc.Messages[0]
		if !message.Delimiters.Standard() {
			if !reported[location] {
				t.Fatalf("an occurrence with unsupported delimiters was retained without its finding at %s", location)
			}
			continue
		}
		counts := map[string]int{}
		for _, segment := range message.Segments {
			counts[segment.ID]++
			segmentPath := location + "/" + segment.ID + "[" + strconv.Itoa(counts[segment.ID]) + "]"
			for _, field := range segment.Fields {
				if field.State == hl7.Empty {
					continue
				}
				populated := false
				for i := field.Span.Start; i < field.Span.End; i++ {
					separator := strings.ContainsRune("^~&", rune(raw[i])) && !(segment.ID == "MSH" && field.Number <= 2)
					if !separator {
						populated = true
						break
					}
				}
				if !populated {
					continue
				}
				fieldPath := segmentPath + "-" + strconv.Itoa(field.Number)
				if reported[location] || reported[segmentPath] || reported[fieldPath] || reportedAtRule(fieldPath, reported) {
					continue
				}
				t.Fatalf("a populated byte of %s was neither rewritten nor reported unresolved", fieldPath)
			}
		}
	}
}

// reportedAtRule accepts the findings a rule recorded on the field: they are
// normalized selectors, so they carry the field's path with an explicit
// repetition, and optionally a component below it.
func reportedAtRule(fieldPath string, reported map[string]bool) bool {
	for location := range reported {
		if strings.HasPrefix(location, fieldPath+"[") || strings.HasPrefix(location, fieldPath+".") {
			return true
		}
	}
	return false
}
