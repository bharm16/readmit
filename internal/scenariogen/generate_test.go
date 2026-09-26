package scenariogen_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/scenariogen"
)

func plan(t *testing.T) []byte {
	t.Helper()
	template, err := os.ReadFile("../../testdata/fixtures/scenario-siu.json")
	if err != nil {
		t.Fatal(err)
	}
	return []byte(`{"schema":"readmit-scenario-generator/v1","generator_version":"readmit-scenario-generator-v1","seed":0,"template":` + string(template) + `,"rows":[{"id":"plain","patient_name":"SYNTHETIC","notes":["First","Second"],"encoding":"utf-8"}],"variants":[{"id":"baseline","mutations":[]}]}`)
}

func TestGenerationRetainsInputsAndReproducesBytes(t *testing.T) {
	input := plan(t)
	a, err := scenariogen.Write(context.Background(), filepath.Join(t.TempDir(), "family"), input)
	if err != nil {
		t.Fatal(err)
	}
	b, err := scenariogen.Write(context.Background(), filepath.Join(t.TempDir(), "elsewhere"), input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("completion record depends on destination")
	}
	if !bytes.Contains(a, []byte(`"seed":0`)) || !bytes.Contains(a, []byte(`"template":`)) {
		t.Fatal("inputs lost")
	}
}

func TestBoundaryVariantsMaterializeIndependentBytes(t *testing.T) {
	input := plan(t)
	input = bytes.Replace(input, []byte(`"patient_name":"SYNTHETIC"`), []byte(`"patient_name":"Café"`), 1)
	input = bytes.Replace(input, []byte(`{"id":"baseline","mutations":[]}`), []byte(`
 {"id":"baseline","mutations":[]},
 {"id":"absent","mutations":[{"op":"field","step":"book","field":"PID-5","state":"absent"}]},
 {"id":"empty","mutations":[{"op":"field","step":"book","field":"PID-5","state":"empty"}]},
 {"id":"null","mutations":[{"op":"field","step":"book","field":"PID-5","state":"null"}]},
 {"id":"duplicate","mutations":[{"op":"duplicate","step":"book"}]},
 {"id":"delayed","mutations":[{"op":"delay","step":"book","after":"5m"}]},
 {"id":"latin","mutations":[{"op":"encoding","step":"book","encoding":"iso-8859-1"}]},
 {"id":"timezone","mutations":[{"op":"timezone","step":"book","offset":"-06:00"}]}
 `), 1)
	path := filepath.Join(t.TempDir(), "family")
	result, err := scenariogen.Write(context.Background(), path, input)
	if err != nil {
		t.Fatal(err)
	}
	var record scenariogen.Manifest
	if err := json.Unmarshal(result, &record, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	streams := map[string][]byte{}
	for _, s := range record.Streams {
		b, err := os.ReadFile(filepath.Join(path, s.File))
		if err != nil {
			t.Fatal(err)
		}
		streams[s.Variant] = b
		sum := sha256.Sum256(b)
		if s.SHA256 != hex.EncodeToString(sum[:]) || s.Bytes != len(b) {
			t.Fatal("wrong digest/length")
		}
	}
	// Fixed authored substrings distinguish absent, present-empty, and HL7 null.
	for variant, want := range map[string][]byte{
		"baseline": []byte("PID|1||SYNTH-PATIENT-A^^^READMIT||Café\r"),
		"absent":   []byte("PID|1||SYNTH-PATIENT-A^^^READMIT|\r"),
		"empty":    []byte("PID|1||SYNTH-PATIENT-A^^^READMIT||\r"),
		"null":     []byte("PID|1||SYNTH-PATIENT-A^^^READMIT||\"\"\r"),
		"latin":    []byte("PID|1||SYNTH-PATIENT-A^^^READMIT||Caf\xe9\r"),
		"timezone": []byte("|20260101060100-0600||SIU^S12|"),
	} {
		if !bytes.Contains(streams[variant], want) {
			t.Errorf("%s missing literal %q", variant, want)
		}
	}
	baseline := bytes.SplitAfter(streams["baseline"], []byte("\x1c\r"))
	duplicated := bytes.SplitAfter(streams["duplicate"], []byte("\x1c\r"))
	if len(duplicated) != len(baseline)+1 || !bytes.Equal(duplicated[1], baseline[1]) || !bytes.Equal(duplicated[2], baseline[1]) {
		t.Fatal("duplicate changed message bytes")
	}
	delayed := bytes.SplitAfter(streams["delayed"], []byte("\x1c\r"))
	if !bytes.Equal(delayed[4], baseline[1]) || !bytes.Equal(delayed[1], baseline[2]) {
		t.Fatal("delayed stream was not reordered without changing declared time")
	}
	if record.Streams[5].IntendedArrivals[4].After != "6m0s" {
		t.Fatal("delay schedule lost")
	}
	for _, name := range []string{"absent", "empty", "null", "duplicate", "delayed", "latin", "timezone"} {
		if bytes.Equal(streams[name], streams["baseline"]) {
			t.Errorf("%s is inert", name)
		}
	}
}

func TestGeneratorRefusesInvalidInputsBeforeCreatingOutput(t *testing.T) {
	for name, change := range map[string][2]string{
		"absent seed": {`"seed":0,`, ``}, "null seed": {`"seed":0`, `"seed":null`},
		"unknown top":             {`"seed":0`, `"seed":0,"hidden":1`},
		"unknown row":             {`"patient_name":"SYNTHETIC"`, `"patient_name":"SYNTHETIC","hidden":1`},
		"null notes":              {`"notes":["First","Second"]`, `"notes":null`},
		"null note":               {`"notes":["First","Second"]`, `"notes":[null]`},
		"absent mutation list":    {`,"mutations":[]`, ``},
		"null variant list":       {`"variants":[{"id":"baseline","mutations":[]}]`, `"variants":null`},
		"version":                 {`readmit-scenario-generator-v1`, `future`},
		"unknown template member": {`"base_time":`, `"private":true,"base_time":`},
		"row path":                {`"id":"plain"`, `"id":"../outside"`},
		"injection":               {`"patient_name":"SYNTHETIC"`, `"patient_name":"bad\rMSH"`},
		"unrepresentable":         {`"patient_name":"SYNTHETIC"`, `"patient_name":"日本語"`},
	} {
		t.Run(name, func(t *testing.T) {
			input := bytes.Replace(plan(t), []byte(change[0]), []byte(change[1]), 1)
			if name == "unrepresentable" {
				input = bytes.Replace(input, []byte(`"encoding":"utf-8"`), []byte(`"encoding":"iso-8859-1"`), 1)
			}
			if bytes.Equal(input, plan(t)) {
				t.Fatal("unchanged fixture")
			}
			path := filepath.Join(t.TempDir(), "family")
			if _, err := scenariogen.Write(context.Background(), path, input); err == nil {
				t.Fatal("invalid plan accepted")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("invalid plan created output")
			}
		})
	}
	for _, mutation := range []string{
		`{"op":"duplicate","step":"unknown"}`,
		`{"op":"duplicate","step":"book","after":"1s"}`,
		`{"op":"field","step":"book","field":"PID-5","state":null}`,
		`{"op":"field","step":"book","field":"MSH-1","state":"empty"}`,
		`{"op":"field","step":"book","field":"PID-5","state":"guess"}`,
		`{"op":"delay","step":"book","after":"0s"}`,
		`{"op":"delay","step":"book","after":"1ms"}`,
		`{"op":"delay","step":"book","after":"8761h"}`,
		`{"op":"encoding","step":"book","encoding":"guessed"}`,
		`{"op":"timezone","step":"book","offset":"+14:01"}`,
		`{"op":"timezone","step":"book","offset":"+12:60"}`,
		`{"op":"script","step":"book"}`,
		`{"op":"duplicate","step":"book"},{"op":"duplicate","step":"book"}`,
	} {
		input := bytes.Replace(plan(t), []byte(`"mutations":[]`), []byte(`"mutations":[`+mutation+`]`), 1)
		if _, err := scenariogen.Decode(input); err == nil {
			t.Errorf("accepted %s", mutation)
		}
	}
}

func TestGenerationCancellationAndRefusalNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "family")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scenariogen.Write(ctx, path, plan(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cancel created output")
	}
	first, err := scenariogen.Write(context.Background(), path, plan(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scenariogen.Write(context.Background(), path, plan(t)); err == nil {
		t.Fatal("overwrote destination")
	}
	retained, err := os.ReadFile(filepath.Join(path, "generation.json"))
	if err != nil || !bytes.Equal(first, retained) {
		t.Fatal("changed original generation")
	}
	incomplete := filepath.Join(t.TempDir(), "interrupted")
	if err := os.Mkdir(incomplete, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := scenariogen.Write(context.Background(), incomplete, plan(t)); err == nil {
		t.Fatal("resumed incomplete directory")
	}
	if _, err := os.Stat(filepath.Join(incomplete, "generation.json")); !os.IsNotExist(err) {
		t.Fatal("incomplete output acquired completion record")
	}
}

// The family a check holds in memory is exactly what Write installs: the
// same completion record and the same framed bytes per stream, produced
// without creating anything anywhere.
func TestProduceMatchesWrittenBytesWithoutCreatingAnything(t *testing.T) {
	parent := t.TempDir()
	t.Setenv("TMPDIR", parent)
	t.Setenv("TMP", parent)
	t.Setenv("TEMP", parent)
	input := plan(t)
	family, err := scenariogen.Produce(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(parent); err != nil || len(entries) != 0 {
		t.Fatalf("producing wrote %v: %v", entries, err)
	}
	path := filepath.Join(t.TempDir(), "family")
	record, err := scenariogen.Write(context.Background(), path, input)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(family.Manifest, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(encoded, '\n'), record) {
		t.Fatal("the in-memory manifest is not the completion record Write installs")
	}
	for i, row := range family.Manifest.Streams {
		written, err := os.ReadFile(filepath.Join(path, row.File))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(written, family.Stream(i)) {
			t.Fatalf("stream %d differs from the bytes Write installed", i)
		}
		sum := sha256.Sum256(family.Stream(i))
		if row.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatalf("stream %d digest disagrees with its bytes", i)
		}
	}
}

// WriteCase is the one operation for a generated case: the streams and their
// completion record are the ones Write writes, and the case bundle beside
// them opens as generated from the plan's declared generator inputs.
func TestWriteCaseWritesStreamsAndGeneratedCaseThroughOneOperation(t *testing.T) {
	root := t.TempDir()
	written, err := scenariogen.WriteCase(context.Background(), plan(t), filepath.Join(root, "family"), filepath.Join(root, "case"))
	if err != nil {
		t.Fatal(err)
	}
	if written.Streams != 1 || written.Identity == "" {
		t.Fatalf("written: %+v", written)
	}
	record, err := os.ReadFile(filepath.Join(root, "family", "generation.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written.Record, record) {
		t.Fatal("the operation did not retain the record it installed")
	}
	other, err := scenariogen.Write(context.Background(), filepath.Join(root, "elsewhere"), plan(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written.Record, other) {
		t.Fatal("WriteCase and Write disagree on the completion record")
	}
	if written.GeneratorVersion != "readmit-scenario-generator-v1" || written.ProfileVersion != "readmit-siu-lifecycle-v1" {
		t.Fatalf("the template's inputs: %+v", written)
	}
	if written.BaseTime.Format(time.RFC3339) != "2026-01-01T12:00:00Z" {
		t.Fatalf("the template's base time: %+v", written.BaseTime)
	}
	opened, err := bundle.Open(filepath.Join(root, "case"))
	if err != nil {
		t.Fatal(err)
	}
	provenance := opened.Manifest.Provenance
	if provenance.Mode != bundle.Generated || provenance.Generator == nil {
		t.Fatalf("the case's provenance: %+v", provenance)
	}
	generator := provenance.Generator
	if generator.Seed != 0 || generator.GeneratorVersion != written.GeneratorVersion ||
		generator.ProfileVersion != written.ProfileVersion ||
		!generator.BaseTime.Equal(written.BaseTime.UTC()) {
		t.Fatalf("the case's generator inputs: %+v", generator)
	}
}

// A cancelled case write retains nothing: nothing of the generation and no
// case bundle.
func TestWriteCaseCancelledCreatesNothing(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scenariogen.WriteCase(ctx, plan(t), filepath.Join(root, "family"), filepath.Join(root, "case")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("a cancelled case write retained %v: %v", entries, err)
	}
}

func TestGenerationPreservesAllFourWorkflowBindings(t *testing.T) {
	for _, family := range []string{"adt", "siu", "orm", "oru"} {
		t.Run(family, func(t *testing.T) {
			original, err := os.ReadFile("../../testdata/fixtures/scenario-" + family + ".json")
			if err != nil {
				t.Fatal(err)
			}
			p, err := scenariogen.Decode(plan(t))
			if err != nil {
				t.Fatal(err)
			}
			p.Template = original
			input, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "family")
			if _, err := scenariogen.Write(context.Background(), path, input); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(filepath.Join(path, "stream-001.mllp"))
			if err != nil {
				t.Fatal(err)
			}
			checks := map[string][]string{
				"adt": {"|ADT^A40|", "MRG|SYNTH-PATIENT-B^^^READMIT\r", "|SYNTH-VISIT-A^^^READMIT\r"},
				"siu": {"|SIU^S12|", "SCH|SYNTH-APPOINTMENT-A^READMIT|SYNTH-APPOINTMENT-A^READMIT\r"},
				"orm": {"|ORM^O01|", "ORC|NW|PLACER-001^SYNTH-PLACER|FILLER-001^SYNTH-FILLER\r"},
				"oru": {"|ORU^R01|", "ORC|RE|PLACER-001^SYNTH-PLACER|FILLER-001^SYNTH-FILLER\r", "OBX|1|TX|SYNTH-TEXT|1|Synthetic observation 1||||||P\r", "OBX|2|TX|SYNTH-TEXT|2|Synthetic observation 2||||||P\r"},
			}
			for _, want := range checks[family] {
				if !bytes.Contains(b, []byte(want)) {
					t.Errorf("missing %q", want)
				}
			}
			if !bytes.Contains(b, []byte("NTE|1|L|First\rNTE|2|L|Second\r")) {
				t.Fatal("repeated notes lost")
			}
		})
	}
}

func TestGenerationRecordsExplicitDSTBoundaryWithoutHostZone(t *testing.T) {
	p, err := scenariogen.Decode(plan(t))
	if err != nil {
		t.Fatal(err)
	}
	p.Template = []byte(`{"schema":"readmit-scenario/v1","scenario":{"id":"dst","version":"1"},"profile":"readmit-siu-lifecycle-v1","base_time":"2026-11-01T06:59:00Z","subjects":[{"id":"patient","kind":"patient","namespace":"READMIT","identifier":"SYNTH-PATIENT","initial_state":"active"},{"id":"appt","kind":"appointment","namespace":"READMIT","identifier":"SYNTH-APPT","patient":"patient","initial_state":"none"}],"steps":[{"id":"book","event":"S12","subject":"appt","after":"0s","expect":"accepted"},{"id":"reschedule","event":"S13","subject":"appt","after":"1m","expect":"accepted"}]}`)
	p.Variants = []scenariogen.Variant{{ID: "fallback", Mutations: []scenariogen.Mutation{{Op: "timezone", Step: "book", Offset: "-05:00"}, {Op: "timezone", Step: "reschedule", Offset: "-06:00"}}}}
	input, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "family")
	if _, err := scenariogen.Write(context.Background(), path, input); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(path, "stream-001.mllp"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"|20261101015900-0500||SIU^S12|", "|20261101010000-0600||SIU^S13|"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("missing boundary %q", want)
		}
	}
}

func TestGeneratorBoundsAndSeedAffectMaterializedOutput(t *testing.T) {
	p, err := scenariogen.Decode(plan(t))
	if err != nil {
		t.Fatal(err)
	}
	for label, edit := range map[string]func(*scenariogen.Plan){
		"rows": func(p *scenariogen.Plan) {
			for range 8 {
				p.Rows = append(p.Rows, p.Rows[0])
			}
		},
		"variants": func(p *scenariogen.Plan) {
			for range 16 {
				p.Variants = append(p.Variants, p.Variants[0])
			}
		},
		"notes":     func(p *scenariogen.Plan) { p.Rows[0].Notes = make([]string, 9) },
		"text":      func(p *scenariogen.Plan) { p.Rows[0].PatientName = string(bytes.Repeat([]byte("x"), 257)) },
		"mutations": func(p *scenariogen.Plan) { p.Variants[0].Mutations = make([]scenariogen.Mutation, 33) },
	} {
		t.Run(label, func(t *testing.T) {
			copy, err := scenariogen.Decode(plan(t))
			if err != nil {
				t.Fatal(err)
			}
			edit(&copy)
			b, err := json.Marshal(copy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := scenariogen.Decode(b); err == nil {
				t.Fatal("bound ignored")
			}
		})
	}
	if _, err := scenariogen.Decode(bytes.Repeat([]byte(" "), scenariogen.MaxBytes+1)); err == nil {
		t.Fatal("size limit ignored")
	}
	a, b := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	if _, err := scenariogen.Write(context.Background(), a, plan(t)); err != nil {
		t.Fatal(err)
	}
	p.Seed = 1
	input, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scenariogen.Write(context.Background(), b, input); err != nil {
		t.Fatal(err)
	}
	aa, err := os.ReadFile(filepath.Join(a, "stream-001.mllp"))
	if err != nil {
		t.Fatal(err)
	}
	bb, err := os.ReadFile(filepath.Join(b, "stream-001.mllp"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(aa, bb) {
		t.Fatal("seed has no effect")
	}
}

// Cancel when the public output's first stream exists. This controls the
// cancellation boundary without a timer, goroutine race or production hook.
type cancelAfterStream struct {
	context.Context
	cancel context.CancelFunc
	stream string
}

func (c cancelAfterStream) Err() error {
	if _, err := os.Stat(c.stream); err == nil {
		c.cancel()
	}
	return c.Context.Err()
}

func TestGenerationInterruptedAfterStreamNeverAcquiresCompletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "interrupted")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interrupted := cancelAfterStream{Context: ctx, cancel: cancel, stream: filepath.Join(path, "stream-001.mllp")}
	if _, err := scenariogen.Write(interrupted, path, plan(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel after writing: %v", err)
	}
	retained, err := os.ReadFile(interrupted.stream)
	if err != nil || len(retained) == 0 {
		t.Fatalf("missing retained bytes: %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, "generation.json")); !os.IsNotExist(err) {
		t.Fatal("cancelled generation appears complete")
	}
	if _, err := scenariogen.Write(context.Background(), path, plan(t)); err == nil {
		t.Fatal("resumed incomplete output")
	}
	after, err := os.ReadFile(interrupted.stream)
	if err != nil || !bytes.Equal(retained, after) {
		t.Fatal("retry changed retained bytes")
	}
	fresh := filepath.Join(t.TempDir(), "fresh")
	if _, err := scenariogen.Write(context.Background(), fresh, plan(t)); err != nil {
		t.Fatal(err)
	}
	regenerated, err := os.ReadFile(filepath.Join(fresh, "stream-001.mllp"))
	if err != nil || !bytes.Equal(retained, regenerated) {
		t.Fatal("fresh recovery changed stream")
	}
	if _, err := os.Stat(filepath.Join(fresh, "generation.json")); err != nil {
		t.Fatal("fresh recovery did not complete")
	}
}
