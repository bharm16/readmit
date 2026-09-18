package bundle_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func imported() bundle.Provenance {
	t := time.Date(2030, 3, 4, 5, 6, 7, 0, time.UTC)
	return bundle.Provenance{Mode: bundle.Imported, ImportedAt: &t}
}

func write(t testing.TB, inputs []bundle.Input, provenance bundle.Provenance) (string, *bundle.Bundle) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case")
	b, err := bundle.Write(path, inputs, provenance)
	if err != nil {
		t.Fatal(err)
	}
	return path, b
}

func TestCaptureReopensEveryOccurrenceByteAndGap(t *testing.T) {
	raw := fixture(t, "case-evidence.mllp")
	observed := time.Date(2026, 1, 2, 12, 5, 0, 0, time.UTC)
	path, original := write(t, []bundle.Input{{Path: "/evidence/synthetic.mllp", Data: raw, Observations: map[int]bundle.Observation{4: {Direction: bundle.Outbound, ObservedAt: &observed}}}}, imported())
	b, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if b.Identity != original.Identity || b.Manifest.Schema != "readmit-case/v1" || len(b.Events) != 8 {
		t.Fatalf("unexpected bundle: %+v", b.Manifest)
	}
	wantKinds := []bundle.EventKind{bundle.Message, bundle.Message, bundle.Acknowledgement, bundle.Message, bundle.Acknowledgement, bundle.Message, bundle.Acknowledgement, bundle.Unparsed}
	var reconstructed []byte
	for i, event := range b.Events {
		if event.ID != fmt.Sprintf("s0001-e%06d", i+1) || event.Sequence != i+1 || event.Offset != len(reconstructed) || event.Kind != wantKinds[i] {
			t.Fatalf("incorrect occurrence %d: %+v", i+1, event)
		}
		payload, err := b.Raw(event.ID)
		if err != nil {
			t.Fatal(err)
		}
		reconstructed = append(reconstructed, payload...)
		if event.ImportedAt == nil || !event.ImportedAt.Equal(*imported().ImportedAt) {
			t.Fatal("missing independent import time")
		}
		if i != 3 && (event.ObservedAt != nil || event.Direction != bundle.Unknown) {
			t.Fatal("fabricated an observed time or direction")
		}
	}
	if !bytes.Equal(reconstructed, raw) {
		t.Fatal("lost source bytes or framing")
	}
	if !b.Events[3].ObservedAt.Equal(observed) || b.Events[3].Direction != bundle.Outbound || string(b.Value(b.Events[3].ID, b.Events[3].Fields.DeclaredTime)) != "20260102120200" {
		t.Fatal("observed, declared, or imported times were conflated")
	}
	if b.Events[5].Fields.DeclaredTime.State != hl7.Empty {
		t.Fatal("missing declared time lost its empty state")
	}
	if b.Events[7].Fields != nil || b.Events[7].ParseError == "" {
		t.Fatal("malformed evidence was not flagged unparsed")
	}
	for _, i := range []int{0, 1} {
		if string(b.Value(b.Events[i].ID, b.Events[i].Fields.ControlID)) != "DUP" {
			t.Fatal("duplicate control ID changed")
		}
	}
	wantLinks := []bundle.Correlation{
		{Kind: bundle.AmbiguousACK, ACKID: "s0001-e000003", MessageIDs: []string{"s0001-e000001", "s0001-e000002"}},
		{Kind: bundle.Matched, ACKID: "s0001-e000005", MessageIDs: []string{"s0001-e000004"}},
		{Kind: bundle.UnmatchedACK, ACKID: "s0001-e000007"},
		{Kind: bundle.Unacknowledged, MessageIDs: []string{"s0001-e000001"}},
		{Kind: bundle.Unacknowledged, MessageIDs: []string{"s0001-e000002"}},
		{Kind: bundle.Unacknowledged, MessageIDs: []string{"s0001-e000006"}},
	}
	if !reflect.DeepEqual(b.Correlations, wantLinks) {
		t.Fatalf("correlations = %+v", b.Correlations)
	}
	copy, _ := b.Raw(b.Events[0].ID)
	copy[0] = 'X'
	again, _ := b.Raw(b.Events[0].ID)
	if again[0] != 0x0b {
		t.Fatal("raw evidence exposed through a mutable slice")
	}
}

func TestEveryInspectFormatAndMalformedRemaindersArePreserved(t *testing.T) {
	for _, name := range []string{"adt-cr.hl7", "siu-lf.hl7", "ack-crlf.hl7", "custom-delimiters.hl7", "two-messages.mllp", "non-utf8.hl7", "reduced-delimiters.hl7"} {
		t.Run(name, func(t *testing.T) {
			raw := fixture(t, name)
			path, _ := write(t, []bundle.Input{{Path: name, Data: raw}}, imported())
			b, err := bundle.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			var got []byte
			for _, event := range b.Events {
				if event.Kind == bundle.Unparsed {
					t.Fatal(event.ParseError)
				}
				payload, _ := b.Raw(event.ID)
				got = append(got, payload...)
			}
			if !bytes.Equal(got, raw) {
				t.Fatal("changed evidence")
			}
		})
	}
	frame := "\x0bMSH|^~\\&|APP\r\x1c\r"
	for _, tc := range []struct {
		name, raw string
		kinds     []bundle.EventKind
	}{
		{"empty", "", []bundle.EventKind{bundle.Unparsed}},
		{"garbage", "SECRET\x00\xff", []bundle.EventKind{bundle.Unparsed}},
		{"ambiguous raw", "MSH|^~\\&|APP\rMSH|^~\\&|APP\r", []bundle.EventKind{bundle.Unparsed}},
		{"bad payload then valid", "\x0bBAD\x00\xff\x1c\r" + frame, []bundle.EventKind{bundle.Unparsed, bundle.Message}},
		{"truncated suffix", frame + "\x0bMSH|SECRET", []bundle.EventKind{bundle.Message, bundle.Unparsed}},
		{"unframed suffix", frame + "SECRET" + frame, []bundle.EventKind{bundle.Message, bundle.Unparsed}},
		{"missing frame CR", "\x0bMSH|^~\\&|APP\r\x1c", []bundle.EventKind{bundle.Unparsed}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, _ := write(t, []bundle.Input{{Path: "synthetic", Data: []byte(tc.raw)}}, imported())
			b, err := bundle.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			var got []byte
			var kinds []bundle.EventKind
			for _, event := range b.Events {
				payload, _ := b.Raw(event.ID)
				got = append(got, payload...)
				kinds = append(kinds, event.Kind)
			}
			if string(got) != tc.raw || !reflect.DeepEqual(kinds, tc.kinds) {
				t.Fatal("lost malformed bytes or guessed boundaries")
			}
		})
	}
}

func TestCorrelationIsSourceScopedAndAcknowledgementModeAgnostic(t *testing.T) {
	message := "MSH*$%!@*APP*LAB*RX*LAB*20260101**SIU$S12*SAME*P*2.5.1***AL*AL\r"
	ack := "MSH*$%!@*RX*LAB*APP*LAB*20260101**ACK$S12*OWN-ACK-ID*P*2.5.1\rMSA*CA*SAME\r"
	frame := func(s string) string { return "\x0b" + s + "\x1c\r" }
	_, b := write(t, []bundle.Input{
		{Path: "one.hl7", Data: []byte(message)},
		{Path: "two.hl7", Data: []byte(ack)},
		{Path: "both.mllp", Data: []byte(frame(message) + frame(ack))},
	}, imported())
	if b.Correlations[0].Kind != bundle.UnmatchedACK || b.Correlations[1].Kind != bundle.Matched || b.Correlations[1].MessageIDs[0] != "s0003-e000001" {
		t.Fatalf("incorrect source scope or ACK interpretation: %+v", b.Correlations)
	}
}

func TestEmptyNullOmittedAndMultipleMSAAreNeverGuessed(t *testing.T) {
	for _, target := range []string{"", `""`, "ABSENT"} {
		message := "MSH|^~\\&|||||20260101||SIU^S12|" + target + "|P|2.5.1\r"
		ack := "MSH|^~\\&|||||20260101||ACK|OWN|P|2.5.1\rMSA|AA|" + target + "\r"
		if target == "ABSENT" {
			ack += "MSA|AA|ABSENT\r"
		}
		_, b := write(t, []bundle.Input{{Path: "case", Data: []byte("\x0b" + message + "\x1c\r\x0b" + ack + "\x1c\r")}}, imported())
		if b.Correlations[0].Kind != bundle.UnmatchedACK {
			t.Fatal("guessed correlation from absent, null, or multiple MSA fields")
		}
	}
}

func TestGeneratedBundlesAreReproducibleAndCopiesKeepIdentity(t *testing.T) {
	provenance := bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{Seed: 17, BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "v1", ProfileVersion: "siu-v1"}}
	input := []bundle.Input{{Data: fixture(t, "case-evidence.mllp")}}
	firstPath, first := write(t, input, provenance)
	secondPath, second := write(t, input, provenance)
	if first.Identity != second.Identity || !reflect.DeepEqual(directoryFiles(t, firstPath), directoryFiles(t, secondPath)) {
		t.Fatal("same declared inputs produced different bytes")
	}
	for _, data := range directoryFiles(t, firstPath) {
		if bytes.Contains(data, []byte(firstPath)) || bytes.Contains(data, []byte(`"imported_at":"`)) || bytes.Contains(data, []byte(`"observed_at":"`)) {
			t.Fatal("generated provenance contains machine or wall-clock values")
		}
	}
	for _, mutate := range []func(*bundle.GeneratorInputs){
		func(g *bundle.GeneratorInputs) { g.Seed++ },
		func(g *bundle.GeneratorInputs) { g.BaseTime = g.BaseTime.Add(time.Second) },
		func(g *bundle.GeneratorInputs) { g.GeneratorVersion = "v2" },
		func(g *bundle.GeneratorInputs) { g.ProfileVersion = "siu-v2" },
	} {
		changed := *provenance.Generator
		mutate(&changed)
		_, b := write(t, input, bundle.Provenance{Mode: bundle.Generated, Generator: &changed})
		if b.Identity == first.Identity {
			t.Fatal("identity ignored a declared generator input")
		}
	}
	importedPath, original := write(t, []bundle.Input{{Path: "/original/source.hl7", Data: fixture(t, "non-utf8.hl7")}}, imported())
	copyPath := filepath.Join(t.TempDir(), "renamed-case")
	if err := os.CopyFS(copyPath, os.DirFS(importedPath)); err != nil {
		t.Fatal(err)
	}
	for name := range directoryFiles(t, copyPath) {
		if err := os.Chtimes(filepath.Join(copyPath, filepath.FromSlash(name)), time.Unix(42, 0), time.Unix(99, 0)); err != nil {
			t.Fatal(err)
		}
	}
	copied, err := bundle.Open(copyPath)
	if err != nil || copied.Identity != original.Identity {
		t.Fatalf("copy changed identity: %v", err)
	}
}

func TestWriterRejectsInvalidDeclarationsAndExistingDestinations(t *testing.T) {
	raw := fixture(t, "adt-cr.hl7")
	base := bundle.Input{Path: "source", Data: raw}
	for _, input := range []bundle.Input{
		{Path: "source", Data: raw, Options: hl7.Options{Format: "secret"}},
		{Path: "source", Data: raw, Options: hl7.Options{Terminator: "secret"}},
		{Path: "source", Data: raw, Observations: map[int]bundle.Observation{2: {}}},
		{Path: "source", Data: raw, Observations: map[int]bundle.Observation{1: {Direction: "secret"}}},
		{Path: "source", Data: make([]byte, bundle.MaxSourceBytes+1)},
		{Data: raw},
	} {
		path := filepath.Join(t.TempDir(), "case")
		if _, err := bundle.Write(path, []bundle.Input{input}, imported()); err == nil {
			t.Fatal("accepted invalid capture configuration")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("invalid capture created an artifact")
		}
	}
	for _, provenance := range []bundle.Provenance{{}, {Mode: bundle.Imported}, {Mode: bundle.Generated}, {Mode: bundle.Generated, ImportedAt: imported().ImportedAt, Generator: &bundle.GeneratorInputs{}}} {
		if _, err := bundle.Write(filepath.Join(t.TempDir(), "case"), []bundle.Input{base}, provenance); err == nil {
			t.Fatal("accepted invalid provenance")
		}
	}
	path, _ := write(t, []bundle.Input{base}, imported())
	before := directoryFiles(t, path)
	if _, err := bundle.Write(path, []bundle.Input{base}, imported()); err == nil {
		t.Fatal("overwrote existing evidence")
	}
	if !reflect.DeepEqual(before, directoryFiles(t, path)) {
		t.Fatal("changed existing bundle")
	}
	if runtime.GOOS != "windows" {
		for name := range before {
			info, _ := os.Stat(filepath.Join(path, filepath.FromSlash(name)))
			if info.Mode().Perm() != 0600 {
				t.Fatalf("unsafe file permissions %o", info.Mode().Perm())
			}
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0700 {
			t.Fatal("unsafe bundle permissions")
		}
	}
}

func TestGeneratedReaderRequiresDeclaredSeedAndPreservesExplicitZero(t *testing.T) {
	provenance := bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{
		Seed: 0, BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "v1", ProfileVersion: "siu-v1",
	}}
	for _, tc := range []struct {
		name, replacement string
		valid             bool
	}{
		{"explicit zero", `"seed":0,`, true},
		{"omitted", "", false},
		{"null", `"seed":null,`, false},
		{"unknown generator member", `"seed":0,"unknown":true,`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, _ := write(t, []bundle.Input{{Data: fixture(t, "adt-cr.hl7")}}, provenance)
			manifestPath := filepath.Join(path, "manifest.json")
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			data = bytes.Replace(data, []byte(`"seed":0,`), []byte(tc.replacement), 1)
			if err := os.WriteFile(manifestPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			reseal(t, path)
			b, err := bundle.Open(path)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, open error=%v", tc.valid, err)
			}
			if tc.valid && b.Manifest.Provenance.Generator.Seed != 0 {
				t.Fatal("changed explicit seed zero")
			}
		})
	}
}

func directoryFiles(t testing.TB, path string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	err := fs.WalkDir(os.DirFS(path), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(path, filepath.FromSlash(name)))
		files[name] = data
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// Re-seal deliberately edited artifacts using the documented wire algorithm.
// This lets reader tests distinguish schema/evidence checks from hash checks.
func reseal(t testing.TB, path string) {
	t.Helper()
	files := directoryFiles(t, path)
	delete(files, "identity.sha256")
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	var stream bytes.Buffer
	stream.WriteString("readmit-case/v1\n")
	for _, name := range names {
		binary.Write(&stream, binary.BigEndian, uint64(len(name)))
		stream.WriteString(name)
		binary.Write(&stream, binary.BigEndian, uint64(len(files[name])))
		stream.Write(files[name])
	}
	sum := sha256.Sum256(stream.Bytes())
	if err := os.WriteFile(filepath.Join(path, "identity.sha256"), []byte(hex.EncodeToString(sum[:])+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestReaderRefusesIncompleteCorruptAndInconsistentBundles(t *testing.T) {
	for _, tc := range []struct {
		name, file, old, replacement string
		rehash                       bool
	}{
		{"payload corruption", "payloads/s0001-e000001.bin", "MSH", "BAD", false},
		{"future schema", "manifest.json", "readmit-case/v1", "readmit-case/v999", true},
		{"incomplete", "manifest.json", `"state":"complete"`, `"state":"writing"`, true},
		{"unknown member", "manifest.json", `"schema":`, `"secret":true,"schema":`, true},
		{"duplicate member", "manifest.json", `"schema":`, `"schema":"readmit-case/v1","schema":`, true},
		{"path traversal", "events.jsonl", "payloads/s0001-e000001.bin", "../SECRET", true},
		{"forged offset", "events.jsonl", `"offset":0`, `"offset":1`, true},
		{"forged event kind", "events.jsonl", `"kind":"message"`, `"kind":"ack"`, true},
		{"forged field", "events.jsonl", `"declared_time":{"state":"present"`, `"declared_time":{"state":"null"`, true},
		{"forged correlation", "correlations.jsonl", "unacknowledged_message", "matched", true},
		{"forged source hash", "manifest.json", `"sha256":"`, `"sha256":"0`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, _ := write(t, []bundle.Input{{Path: "source", Data: fixture(t, "adt-cr.hl7")}}, imported())
			file := filepath.Join(path, filepath.FromSlash(tc.file))
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte(tc.old)) {
				t.Fatalf("mutation target absent: %s", tc.old)
			}
			data = bytes.Replace(data, []byte(tc.old), []byte(tc.replacement), 1)
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			if tc.rehash {
				reseal(t, path)
			}
			if _, err := bundle.Open(path); err == nil || strings.Contains(err.Error(), "SECRET") {
				t.Fatal("accepted corrupt evidence or exposed path")
			}
		})
	}
	for _, missing := range []string{"manifest.json", "events.jsonl", "correlations.jsonl", "identity.sha256", "payloads/s0001-e000001.bin"} {
		path, _ := write(t, []bundle.Input{{Path: "source", Data: fixture(t, "adt-cr.hl7")}}, imported())
		if err := os.Remove(filepath.Join(path, filepath.FromSlash(missing))); err != nil {
			t.Fatal(err)
		}
		if _, err := bundle.Open(path); err == nil {
			t.Fatal("accepted missing bundle file")
		}
	}
}

func TestReaderRejectsSymlinksAndUnexpectedFiles(t *testing.T) {
	path, _ := write(t, []bundle.Input{{Path: "source", Data: fixture(t, "adt-cr.hl7")}}, imported())
	if err := os.WriteFile(filepath.Join(path, "unexpected.txt"), []byte("SECRET"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Open(path); err == nil {
		t.Fatal("accepted untracked bundle content")
	}
	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Remove(filepath.Join(path, "unexpected.txt")); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(path, "payloads", "s0001-e000001.bin")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../manifest.json", file); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Open(path); err == nil {
		t.Fatal("accepted symlink payload")
	}
}

func TestReaderAppliesWriterBudgetToRebuiltCorrelations(t *testing.T) {
	message := []byte("\x0bMSH|^~\\&|A|B|C|D|20260101120000||SIU^S12|SAME|P|2.5.1\r\x1c\r")
	ack := []byte("\x0bMSH|^~\\&|A|B|C|D|20260101120000||ACK|ACK|P|2.5.1\rMSA|AA|SAME\r\x1c\r")
	path, sample := write(t, []bundle.Input{{Path: "synthetic", Data: append(bytes.Clone(message), ack...)}}, imported())
	// 1,050 messages and ACKs fit the input/event limits, but their ambiguity
	// expands to 1,102,500 references, beyond the 16 MiB correlation-file budget.
	const count = 1050
	source := append(bytes.Repeat(message, count), bytes.Repeat(ack, count)...)
	if _, err := bundle.Write(filepath.Join(t.TempDir(), "too-large"), []bundle.Input{{Path: "synthetic", Data: source}}, imported()); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("writer did not enforce the record budget: %v", err)
	}
	// Independently repeat the valid occurrence descriptors, then seal an empty
	// correlation file. The reader must budget reconstruction, not only disk bytes.
	var events bytes.Buffer
	offset := 0
	for i := range 2 * count {
		event, raw := sample.Events[0], message
		if i >= count {
			event, raw = sample.Events[1], ack
		}
		event.ID, event.Sequence, event.Offset = fmt.Sprintf("s0001-e%06d", i+1), i+1, offset
		event.Payload.Path = "payloads/" + event.ID + ".bin"
		if err := os.WriteFile(filepath.Join(path, event.Payload.Path), raw, 0600); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(event, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		events.Write(encoded)
		events.WriteByte('\n')
		offset += len(raw)
	}
	manifest := sample.Manifest
	manifest.EventCount = 2 * count
	manifest.Sources[0].Occurrences, manifest.Sources[0].Size = 2*count, len(source)
	sum := sha256.Sum256(source)
	manifest.Sources[0].SHA256 = hex.EncodeToString(sum[:])
	raw, err := json.Marshal(manifest, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"manifest.json": raw, "events.jsonl": events.Bytes(), "correlations.jsonl": {}} {
		if err := os.WriteFile(filepath.Join(path, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	reseal(t, path)
	if _, err := bundle.Open(path); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("reader did not enforce the same reconstruction budget: %v", err)
	}
}

func FuzzOpen(f *testing.F) {
	path, _ := write(f, []bundle.Input{{Path: "fixture", Data: []byte("MSH|^~\\&|APP\r")}}, imported())
	files := directoryFiles(f, path)
	f.Add(files["manifest.json"], files["events.jsonl"], files["correlations.jsonl"])
	f.Add([]byte(`{"schema":"readmit-case/v999"}`), []byte("null\n"), []byte("{}\n"))
	f.Fuzz(func(t *testing.T, manifest, events, links []byte) {
		if len(manifest)+len(events)+len(links) > 65536 {
			return
		}
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "payloads"), 0700); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			switch name {
			case "manifest.json":
				data = manifest
			case "events.jsonl":
				data = events
			case "correlations.jsonl":
				data = links
			}
			if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		reseal(t, dir)
		if declared, describeErr := bundle.Describe(dir); describeErr != nil {
			if len(describeErr.Error()) > 256 {
				t.Fatal("unbounded describe diagnostic")
			}
		} else if declared.Schema == "" {
			t.Fatal("describe accepted a manifest without a contract version")
		}
		b, err := bundle.Open(dir)
		if err != nil {
			if len(err.Error()) > 256 {
				t.Fatal("unbounded diagnostic")
			}
			return
		}
		if len(b.Events) != 1 {
			t.Fatal("accepted invented occurrences")
		}
		payload, _ := b.Raw(b.Events[0].ID)
		if string(payload) != "MSH|^~\\&|APP\r" {
			t.Fatal("reader changed evidence")
		}
		encoded, err := json.Marshal(b.Manifest)
		if err != nil || len(encoded) == 0 {
			t.Fatal("invalid accepted manifest")
		}
	})
}

func TestDescribeReportsDeclaredContractWithoutVerifyingEvidence(t *testing.T) {
	path, original := write(t, []bundle.Input{{Path: "/evidence/synthetic.mllp", Data: fixture(t, "case-evidence.mllp")}}, imported())
	manifest, err := bundle.Describe(path)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != "readmit-case/v1" || manifest.Provenance.Mode != bundle.Imported || manifest.EventCount != len(original.Events) {
		t.Fatalf("unexpected declared manifest: %+v", manifest)
	}
	// Describe is a declaration, not a verification: a directory whose payloads
	// no longer match its identity must still be listable, and Open must still
	// refuse it. Listing a folder never promotes unverified bytes to evidence.
	if err := os.WriteFile(filepath.Join(path, "payloads", "s0001-e000001.bin"), []byte("MSH|^~\\&|TAMPERED\r"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Describe(path); err != nil {
		t.Fatalf("describe refused a readable manifest: %v", err)
	}
	if _, err := bundle.Open(path); err == nil {
		t.Fatal("Open accepted evidence that no longer matches its identity")
	}
}

func TestDescribeRefusesUnknownVersionsMembersAndIrregularManifests(t *testing.T) {
	path, _ := write(t, []bundle.Input{{Path: "/evidence/synthetic.mllp", Data: fixture(t, "case-evidence.mllp")}}, imported())
	manifest := filepath.Join(path, "manifest.json")
	for name, contents := range map[string]string{
		"unknown version": `{"schema":"readmit-case/v999","state":"complete","provenance":{"mode":"imported","imported_at":"2030-03-04T05:06:07Z"},"sources":[],"event_count":0}`,
		"unknown member":  `{"schema":"readmit-case/v1","state":"complete","provenance":{"mode":"imported","imported_at":"2030-03-04T05:06:07Z"},"sources":[],"event_count":0,"extra":1}`,
		"not JSON":        "{",
	} {
		if err := os.WriteFile(manifest, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := bundle.Describe(path); err == nil {
			t.Fatalf("describe accepted a manifest with an %s", name)
		}
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Describe(path); err == nil {
		t.Fatal("describe accepted a directory without a manifest")
	}
	if _, err := bundle.Describe(filepath.Join(path, "missing")); err == nil {
		t.Fatal("describe accepted a directory that does not exist")
	}
}

// Listing a folder and opening one of its entries must agree about which
// contract versions exist, so a version either reader gains reaches both.
func TestDescribeAcceptsEveryContractVersionTheReaderSupports(t *testing.T) {
	path, _ := write(t, []bundle.Input{{Path: "/evidence/synthetic.mllp", Data: fixture(t, "case-evidence.mllp")}}, imported())
	manifest := filepath.Join(path, "manifest.json")
	declared, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"readmit-case/v1", "readmit-case/v2", "readmit-case/v3", "readmit-case/v4"} {
		retagged := bytes.Replace(declared, []byte(`"schema":"readmit-case/v1"`), []byte(`"schema":"`+version+`"`), 1)
		if bytes.Equal(retagged, declared) && version != "readmit-case/v1" {
			t.Fatalf("could not retag the manifest as %s", version)
		}
		if err := os.WriteFile(manifest, retagged, 0600); err != nil {
			t.Fatal(err)
		}
		described, err := bundle.Describe(path)
		if err != nil {
			t.Fatalf("describe refused the supported contract %s: %v", version, err)
		}
		if described.Schema != version {
			t.Fatalf("describe reported %q for a manifest declaring %q", described.Schema, version)
		}
	}
}
