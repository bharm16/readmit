package tests

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/synth"
)

func synthArgs(output string) []string {
	return []string{"synth", "--seed", "0", "--base-time", "2026-01-01T12:00:00Z", "--generator-version", "readmit-synth-v1", "--profile-version", "readmit-siu-v1", "--output", output}
}

func synthTree(t *testing.T, path string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	err := filepath.WalkDir(path, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(path, name)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(name)
		files[filepath.ToSlash(relative)] = data
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func createSynth(t *testing.T, args []string) {
	t.Helper()
	stdout, stderr, err := run(t, args...)
	if err != nil || stderr != "" {
		t.Fatalf("synth: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Synthetic SIU family: complete") || !strings.Contains(stdout, "invalid (S12, S13 with an unbooked filler identifier)") {
		t.Fatalf("output hid family completion or known defect: %s", stdout)
	}
}

func TestSynthReproducesEveryFamilyByteAcrossLocationsAndEnvironment(t *testing.T) {
	first := filepath.Join(t.TempDir(), "FIRST-PRIVATE-PATH")
	createSynth(t, synthArgs(first))
	second := filepath.Join(t.TempDir(), "SECOND-PRIVATE-PATH")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, synthArgs(second)...)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "TZ=Pacific/Honolulu")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synth in different working directory: %v %s", err, output)
	}
	left, right := synthTree(t, first), synthTree(t, second)
	if !reflect.DeepEqual(left, right) {
		t.Fatal("identical declared inputs changed file paths or bytes across output locations")
	}
	// Three complete bundles, each with four metadata files plus its payloads,
	// and one family completion record. No temporary, path, or clock files.
	if len(left) != 20 {
		t.Fatalf("unexpected family file count: %d", len(left))
	}
	for name, data := range left {
		if strings.Contains(name, "incomplete") || bytes.Contains(data, []byte(first)) || bytes.Contains(data, []byte(second)) {
			t.Fatalf("temporary or machine-specific data in %s", name)
		}
	}
	var family synth.Manifest
	if err := json.Unmarshal(left["family.json"], &family, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if family.Schema != "readmit-synth/v1" || family.State != "complete" || family.Generator.Seed != 0 || len(family.Cases) != 3 || !bytes.Contains(left["family.json"], []byte(`"seed":0`)) {
		t.Fatalf("incomplete family or missing explicitly declared zero seed: %+v", family)
	}
	for i, name := range []string{"regression", "cancellation", "invalid"} {
		c := family.Cases[i]
		if c.Variant != name || c.Path != name || (name == "invalid") != (c.KnownDefect != "") {
			t.Fatalf("incorrect family entry: %+v", c)
		}
		b := openSynthCase(t, first, name)
		if b.Identity != c.Identity || !reflect.DeepEqual(*b.Manifest.Provenance.Generator, family.Generator) {
			t.Fatal("family record disagrees with a child bundle")
		}
		stdout, stderr, err := run(t, "timeline", filepath.Join(first, name))
		if err != nil || stderr != "" || !strings.Contains(stdout, "Provenance: generated") || !strings.Contains(stdout, "observed=unknown") || !strings.Contains(stdout, "imported=unknown") {
			t.Fatalf("timeline did not open generated case: %v %s %s", err, stderr, stdout)
		}
	}
	stdout, stderr, err := run(t, synthArgs(first)...)
	if err == nil || stdout != "" || !strings.Contains(stderr, "destination must be new") || !reflect.DeepEqual(left, synthTree(t, first)) {
		t.Fatal("existing family was not preserved")
	}
}

func openSynthCase(t *testing.T, family, name string) *bundle.Bundle {
	t.Helper()
	b, err := bundle.Open(filepath.Join(family, name))
	if err != nil {
		t.Fatal(err)
	}
	if b.Manifest.Provenance.Mode != bundle.Generated || b.Manifest.Provenance.ImportedAt != nil || b.Manifest.Provenance.Generator == nil || len(b.Manifest.Sources) != 1 || b.Manifest.Sources[0].Path != "" {
		t.Fatal("generated provenance includes imported or machine-specific state")
	}
	return b
}

func synthField(t *testing.T, raw []byte, segment string, field int) string {
	t.Helper()
	doc, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR})
	if err != nil || len(doc.Messages) != 1 {
		t.Fatalf("generated occurrence is not one strict MLLP message: %v", err)
	}
	var matches []hl7.Segment
	for _, s := range doc.Messages[0].Segments {
		if s.ID == segment {
			matches = append(matches, s)
		}
	}
	if len(matches) != 1 || matches[0].Field(field).State != hl7.Present {
		t.Fatalf("expected one %s with a present field %d", segment, field)
	}
	return string(doc.Bytes(matches[0].Field(field).Span))
}

func synthPayloads(t *testing.T, b *bundle.Bundle) [][]byte {
	t.Helper()
	var raw [][]byte
	for i, event := range b.Events {
		if event.ID != fmt.Sprintf("s0001-e%06d", i+1) || event.Sequence != i+1 || event.Kind != bundle.Message || event.ParseError != "" || event.ImportedAt != nil || event.ObservedAt != nil || event.Direction != bundle.Unknown {
			t.Fatalf("generated event has random IDs, fabricated observations, or invalid syntax: %+v", event)
		}
		data, err := b.Raw(event.ID)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, data)
	}
	return raw
}

func TestSynthSequencesCorrelateAndIsolateTheKnownDefect(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "family")
	createSynth(t, synthArgs(destination))
	regression := synthPayloads(t, openSynthCase(t, destination, "regression"))
	cancellation := synthPayloads(t, openSynthCase(t, destination, "cancellation"))
	invalid := synthPayloads(t, openSynthCase(t, destination, "invalid"))
	if len(regression) != 2 || len(cancellation) != 3 || len(invalid) != 2 {
		t.Fatal("incorrect scenario event counts")
	}
	if !bytes.Equal(cancellation[0], regression[0]) || !bytes.Equal(cancellation[1], regression[1]) || !bytes.Equal(invalid[0], regression[0]) {
		t.Fatal("variants changed shared regression input")
	}
	placer := synthField(t, regression[0], "SCH", 1)
	filler := synthField(t, regression[0], "SCH", 2)
	if !strings.HasPrefix(placer, "PLACER-") || !strings.HasSuffix(placer, "^READMIT") || !strings.HasPrefix(filler, "FILLER-") || !strings.HasSuffix(filler, "^READMIT") {
		t.Fatal("identifiers do not declare the synthetic namespace")
	}
	for name, sequence := range map[string][][]byte{"regression": regression, "cancellation": cancellation, "invalid": invalid} {
		for i, raw := range sequence {
			if got := synthField(t, raw, "PID", 3); got != "SYNTH-0000000000000000^^^READMIT" {
				t.Fatalf("patient relationship changed: %s", got)
			}
			if synthField(t, raw, "SCH", 1) != placer {
				t.Fatal("placer relationship changed")
			}
			if (name != "invalid" || i != 1) && synthField(t, raw, "SCH", 2) != filler {
				t.Fatal("filler relationship changed outside documented defect")
			}
			if got := synthField(t, raw, "MSH", 9); got != []string{"SIU^S12", "SIU^S13", "SIU^S15"}[i] {
				t.Fatalf("wrong trigger: %s", got)
			}
			if synthField(t, raw, "MSH", 12) != "2.5.1" || synthField(t, raw, "MSH", 11) != "T" {
				t.Fatal("generated message does not declare the fixture HL7 version and test processing mode")
			}
			if got := synthField(t, raw, "MSH", 10); got != fmt.Sprintf("SYNTH-%06d", i+1) {
				t.Fatal("control ID is not sequence-based")
			}
			if got := synthField(t, raw, "MSH", 7); got != []string{"20260101120000+0000", "20260101120100+0000", "20260101120200+0000"}[i] {
				t.Fatalf("event time does not follow the declared base: %s", got)
			}
			wantAppointment := "^^^20260103120000+0000^20260103123000+0000"
			if i == 0 {
				wantAppointment = "^^^20260102120000+0000^20260102123000+0000"
			}
			if synthField(t, raw, "SCH", 11) != wantAppointment || synthField(t, raw, "SCH", 9) != "30" || synthField(t, raw, "SCH", 10) != "min" {
				t.Fatal("appointment start, end, and duration disagree")
			}
		}
	}
	unknownFiller := synthField(t, invalid[1], "SCH", 2)
	if unknownFiller == filler || !strings.HasSuffix(unknownFiller, "-UNBOOKED^READMIT") || bytes.Count(invalid[1], []byte(unknownFiller)) != 1 {
		t.Fatal("invalid reschedule does not have one unbooked filler identifier")
	}
	if repaired := bytes.Replace(invalid[1], []byte(unknownFiller), []byte(filler), 1); !bytes.Equal(repaired, regression[1]) {
		t.Fatal("invalid variant differs in more than its documented filler identifier")
	}
}

func TestSynthDeclaredInputsControlOutputAndVersionsCannotBeRelabeled(t *testing.T) {
	baseline := filepath.Join(t.TempDir(), "baseline")
	createSynth(t, synthArgs(baseline))
	for _, tc := range []struct{ flag, value string }{
		{"--seed", "1"}, {"--seed", "18446744073709551615"}, {"--base-time", "2026-01-01T12:00:01Z"},
	} {
		t.Run(tc.flag+tc.value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "changed")
			args := synthArgs(path)
			for i := range args {
				if args[i] == tc.flag {
					args[i+1] = tc.value
				}
			}
			createSynth(t, args)
			for _, name := range []string{"regression", "cancellation", "invalid"} {
				old, changed := openSynthCase(t, baseline, name), openSynthCase(t, path, name)
				if old.Identity == changed.Identity || reflect.DeepEqual(synthPayloads(t, old), synthPayloads(t, changed)) {
					t.Fatal("changed declared input did not affect both payload and bundle identity")
				}
			}
		})
	}
	// Different RFC3339 offsets naming the same instant are one base-time input.
	equivalent := filepath.Join(t.TempDir(), "equivalent")
	args := synthArgs(equivalent)
	args[4] = "2026-01-01T07:00:00-05:00"
	createSynth(t, args)
	if !reflect.DeepEqual(synthTree(t, baseline), synthTree(t, equivalent)) {
		t.Fatal("base-time representation depends on a local timezone")
	}
	for _, index := range []int{6, 8} {
		path := filepath.Join(t.TempDir(), "unsupported")
		args := synthArgs(path)
		args[index] = "SECRET-UNIMPLEMENTED-VERSION"
		stdout, stderr, err := run(t, args...)
		if err == nil || stdout != "" || !strings.Contains(stderr, "unsupported") || strings.Contains(stderr, "SECRET") {
			t.Fatal("unimplemented version was relabeled or echoed")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("unsupported version created output")
		}
	}
}

func TestSynthRejectsUndeclaredAndInvalidInputsBeforeWriting(t *testing.T) {
	for _, flagIndex := range []int{1, 3, 5, 7, 9} {
		path := filepath.Join(t.TempDir(), "missing")
		args := synthArgs(path)
		args = append(args[:flagIndex], args[flagIndex+2:]...)
		stdout, stderr, err := run(t, args...)
		if err == nil || stdout != "" || !strings.Contains(stderr, "synth requires") {
			t.Fatalf("missing declaration accepted: %v %s", err, stderr)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("missing declaration created output")
		}
	}
	for _, tc := range []struct {
		index int
		value string
	}{
		{2, "-1"}, {2, "18446744073709551616"}, {2, "SECRET-SEED"},
		{4, "SECRET-TIME"}, {4, "2026-01-01T12:00:00"}, {4, "2026-02-30T12:00:00Z"},
		{4, "2026-01-01T12:00:00.1Z"}, {4, "2026-01-01T12:00:00+24:00"}, {4, "2026-01-01T12:00:00+01:60"},
		{4, "0001-01-01T00:00:00Z"}, {4, "0001-01-01T00:00:00+01:00"}, {4, "9999-12-30T12:00:00Z"},
		{6, ""}, {8, ""}, {10, ""},
	} {
		t.Run(fmt.Sprintf("%d/%s", tc.index, tc.value), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "SECRET-OUTPUT")
			args := synthArgs(path)
			args[tc.index] = tc.value
			stdout, stderr, err := run(t, args...)
			if err == nil || stdout != "" || stderr == "" || len(stderr) > 300 || strings.Contains(stderr, "SECRET") || strings.Contains(stderr, path) {
				t.Fatalf("unsafe synth error: %v %q %q", err, stdout, stderr)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("invalid declaration created output")
			}
		})
	}
}
