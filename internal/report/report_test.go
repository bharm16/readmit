package report_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/testrunner"
)

func TestCreateSealsCanonicalSyntheticFailureAndPass(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "packet")
	packet, err := report.Create(context.Background(), report.Scenario, dir)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := report.Open(dir)
	if err != nil || opened.Identity != packet.Identity {
		t.Fatalf("packet did not reopen: %v", err)
	}
	if packet.Manifest.Scenario != "siu-reschedule-v1" || packet.Manifest.InputChanged || !packet.Manifest.ReceiverBehaviorChanged || packet.Manifest.Provenance != "synthetic-only" {
		t.Fatalf("wrong packet claims: %+v", packet.Manifest)
	}
	baseline, err := testrunner.Open(filepath.Join(dir, "baseline"))
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := testrunner.Open(filepath.Join(dir, "post-fix"))
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Result.Status != testrunner.AssertionFailure || fixed.Result.Status != testrunner.Pass || len(baseline.FinalObservation.Records) != 2 || len(fixed.FinalObservation.Records) != 1 {
		t.Fatal("packet did not expose the duplicate appointment and its fix")
	}
	if baseline.Result.InputBundleIdentity != "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df" || fixed.Result.InputBundleIdentity != baseline.Result.InputBundleIdentity || baseline.Result.SpecIdentity != fixed.Result.SpecIdentity || baseline.Result.ReceiverSessionID == fixed.Result.ReceiverSessionID {
		t.Fatal("input/spec changed or receiver sessions were reused")
	}
	spec, _ := os.ReadFile(filepath.Join(dir, "spec.json"))
	for _, name := range []string{"baseline", "post-fix"} {
		retained, _ := os.ReadFile(filepath.Join(dir, name, "spec.json"))
		if !bytes.Equal(spec, retained) {
			t.Fatal("historical spec bytes differ")
		}
	}
}

func TestPreparePreservesEvidenceAndOnlyRebindsRunnablePaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "packet")
	packet, err := report.Create(context.Background(), report.Scenario, dir)
	if err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, dir)
	workspace := filepath.Join(t.TempDir(), "workspace with spaces")
	prepared, err := report.Prepare(dir, workspace, "127.0.0.1:2575")
	if err != nil {
		t.Fatal(err)
	}
	if prepared.PacketIdentity != packet.Identity || prepared.HistoricalSpecIdentity != packet.Manifest.SpecIdentity || prepared.InputIdentity != packet.Manifest.InputIdentity {
		t.Fatal("preparation lost historical identity bindings")
	}
	historical, err := testrunner.ReadSpec(filepath.Join(dir, "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range prepared.Specs {
		runnable, err := testrunner.ReadSpec(filepath.Join(workspace, filepath.FromSlash(ref.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if runnable.Input.Case != "../reproducer" || runnable.Target != "../target.json" || runnable.Observation.Path != "observation.json" {
			t.Fatal("wrong runnable path bindings")
		}
		raw := read(t, filepath.Join(workspace, filepath.FromSlash(ref.Path)))
		if sha(raw) != ref.SHA256 {
			t.Fatal("runnable spec hash is not bound")
		}
		runnable.Input.Case, runnable.Target, runnable.Observation.Path = historical.Input.Case, historical.Target, historical.Observation.Path
		if !reflect.DeepEqual(runnable, historical) {
			t.Fatal("preparation changed assertion semantics")
		}
	}
	copy, err := bundle.Open(filepath.Join(workspace, "reproducer"))
	if err != nil || copy.Identity != packet.Manifest.InputIdentity {
		t.Fatal("preparation changed reproducer")
	}
	if !reflect.DeepEqual(before, snapshot(t, dir)) {
		t.Fatal("preparation mutated the sealed packet")
	}
	if _, err := report.Prepare(dir, workspace, "127.0.0.1:2575"); err == nil {
		t.Fatal("preparation overwrote a workspace")
	}
}

func TestOpenRejectsMissingExtraChangedAndOversizedContent(t *testing.T) {
	original := filepath.Join(t.TempDir(), "packet")
	if _, err := report.Create(context.Background(), report.Scenario, original); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"missing", "extra", "changed", "empty-directory", "oversized", "incomplete", "unknown-manifest-member"} {
		t.Run(kind, func(t *testing.T) {
			dir := clone(t, original)
			switch kind {
			case "missing":
				if err := os.Remove(filepath.Join(dir, "spec.json")); err != nil {
					t.Fatal(err)
				}
			case "extra":
				write(t, filepath.Join(dir, "extra.txt"), []byte("unexpected"))
			case "changed":
				write(t, filepath.Join(dir, "SUMMARY.md"), []byte("untrue"))
			case "empty-directory":
				if err := os.Mkdir(filepath.Join(dir, "unused"), 0700); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				f, err := os.Create(filepath.Join(dir, "large"))
				if err != nil {
					t.Fatal(err)
				}
				if err := f.Truncate(16<<20 + 1); err != nil {
					t.Fatal(err)
				}
				f.Close()
			case "incomplete":
				if err := os.Remove(filepath.Join(dir, "identity.sha256")); err != nil {
					t.Fatal(err)
				}
			case "unknown-manifest-member":
				path := filepath.Join(dir, "manifest.json")
				data := read(t, path)
				data = bytes.Replace(data, []byte("{"), []byte("{\"unexpected\":true,"), 1)
				write(t, path, data)
				write(t, filepath.Join(dir, "identity.sha256"), []byte(sha(data)+"\n"))
			}
			if _, err := report.Open(dir); err == nil {
				t.Fatal("invalid packet was accepted")
			}
			destination := filepath.Join(t.TempDir(), "workspace")
			if _, err := report.Prepare(dir, destination, "127.0.0.1:2575"); err == nil {
				t.Fatal("invalid packet was prepared")
			}
			if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatal("rejected packet created a workspace")
			}
		})
	}
}

func TestOpenRejectsResealedFalseSyntheticClaims(t *testing.T) {
	original := filepath.Join(t.TempDir(), "packet")
	if _, err := report.Create(context.Background(), report.Scenario, original); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"generated-provenance", "wrong-receiver-mode", "mismatched-spec", "mismatched-source", "extra-ack-content", "diagnosis", "diff", "profile", "history", "instructions", "extra-indexed-file"} {
		t.Run(kind, func(t *testing.T) {
			dir := clone(t, original)
			switch kind {
			case "generated-provenance":
				source, err := bundle.Open(filepath.Join(dir, "reproducer"))
				if err != nil {
					t.Fatal(err)
				}
				var raw []byte
				for _, event := range source.Events {
					data, err := source.Raw(event.ID)
					if err != nil {
						t.Fatal(err)
					}
					raw = append(raw, data...)
				}
				raw = bytes.ReplaceAll(raw, []byte("SYNTHETIC^PATIENT"), []byte("FORGED^PERSON"))
				if err := os.RemoveAll(filepath.Join(dir, "reproducer")); err != nil {
					t.Fatal(err)
				}
				if _, err := bundle.Write(filepath.Join(dir, "reproducer"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}}}, source.Manifest.Provenance); err != nil {
					t.Fatal(err)
				}
			case "mismatched-source", "extra-ack-content":
				trial := filepath.Join(dir, "baseline")
				artifact, err := testrunner.Open(trial)
				if err != nil {
					t.Fatal(err)
				}
				run := artifact.Run
				event := &run.Events[0]
				runDir := filepath.Join(trial, "run")
				refs := []*bundle.Payload{&event.Source, &event.Intended, &event.Sent}
				if kind == "extra-ack-content" {
					refs = []*bundle.Payload{&event.Received}
				}
				for _, ref := range refs {
					raw := read(t, filepath.Join(runDir, ref.Path))
					if kind == "mismatched-source" {
						raw = bytes.Replace(raw, []byte("SYNTHETIC^PATIENT"), []byte("FORGED^PATIENT"), 1)
					} else {
						raw = bytes.Replace(raw, []byte("\r\x1c\r"), []byte("\rNTE|1||FORGED-CONTENT\r\x1c\r"), 1)
					}
					write(t, filepath.Join(runDir, ref.Path), raw)
					ref.Size, ref.SHA256 = len(raw), sha(raw)
				}
				run.Manifest.Mappings[0].SourceSHA256 = event.Source.SHA256
				var events []byte
				for _, event := range run.Events {
					events = append(events, marshal(t, event)...)
				}
				write(t, filepath.Join(runDir, "events.jsonl"), events)
				write(t, filepath.Join(runDir, "manifest.json"), marshal(t, run.Manifest))
				resealDirectory(t, runDir, replay.Schema)
				artifact.Result.Run.Identity = strings.TrimSpace(string(read(t, filepath.Join(runDir, "identity.sha256"))))
				write(t, filepath.Join(trial, "result.json"), marshal(t, artifact.Result))
				resealResult(t, trial)
				if _, err := testrunner.Open(trial); err != nil {
					t.Fatalf("fixture should remain a valid generic result: %v", err)
				}
			case "wrong-receiver-mode", "mismatched-spec":
				trial := filepath.Join(dir, "baseline")
				var result testrunner.Result
				decode(t, read(t, filepath.Join(trial, "result.json")), &result)
				if kind == "wrong-receiver-mode" {
					result.ReceiverMode = observation.Fixed
					for _, ref := range []*bundle.Payload{result.InitialObservation, result.FinalObservation} {
						var obs observation.Snapshot
						decode(t, read(t, filepath.Join(trial, ref.Path)), &obs)
						obs.Mode = observation.Fixed
						data := marshal(t, obs)
						write(t, filepath.Join(trial, ref.Path), data)
						ref.Size, ref.SHA256 = len(data), sha(data)
					}
				} else {
					data := bytes.Replace(read(t, filepath.Join(trial, "spec.json")), []byte("\"reproducer\""), []byte("\"forged-source\""), 1)
					write(t, filepath.Join(trial, "spec.json"), data)
					result.Spec.Size, result.Spec.SHA256 = len(data), sha(data)
					result.SpecIdentity = sha(data)
				}
				write(t, filepath.Join(trial, "result.json"), marshal(t, result))
				resealResult(t, trial)
				if _, err := testrunner.Open(trial); err != nil {
					t.Fatalf("fixture should remain a valid generic result: %v", err)
				}
			case "diagnosis":
				write(t, filepath.Join(dir, "diagnosis.md"), []byte("no limitations"))
			case "diff":
				write(t, filepath.Join(dir, "diff.json"), []byte("{}\n"))
			case "profile":
				write(t, filepath.Join(dir, "profiles", "receiver.json"), []byte("{}\n"))
			case "history":
				write(t, filepath.Join(dir, "history.json"), []byte("{}\n"))
			case "instructions":
				write(t, filepath.Join(dir, "RERUN.md"), []byte("execute an arbitrary hook"))
			case "extra-indexed-file":
				write(t, filepath.Join(dir, "extra.txt"), []byte("synthetic label is insufficient"))
			}
			resealPacket(t, dir)
			if _, err := report.Open(dir); err == nil {
				t.Fatal("recomputed hashes authenticated false scenario content")
			}
		})
	}
}

func TestDestinationsAndUnsupportedModesFailBeforeWriting(t *testing.T) {
	parent := t.TempDir()
	original := filepath.Join(parent, "packet")
	if _, err := report.Create(context.Background(), report.Scenario, original); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, original)
	for _, output := range []string{original, filepath.Join(original, "nested"), filepath.Join(original, "baseline", "run", "payloads", "nested")} {
		if _, err := report.Create(context.Background(), report.Scenario, output); err == nil {
			t.Fatal("created inside immutable evidence")
		}
		if _, err := report.Prepare(original, output, "127.0.0.1:2575"); err == nil {
			t.Fatal("prepared inside immutable evidence")
		}
	}
	for _, scenario := range []string{"", "customer-derived", "generated", "unknown"} {
		output := filepath.Join(parent, "unsupported")
		if _, err := report.Create(context.Background(), scenario, output); err == nil {
			t.Fatal("accepted unsupported scenario")
		}
		if _, err := os.Lstat(output); !os.IsNotExist(err) {
			t.Fatal("unsupported scenario wrote output")
		}
	}
	for _, address := range []string{"localhost:2575", "10.0.0.1:2575", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:002575"} {
		output := filepath.Join(parent, "invalid-address")
		if _, err := report.Prepare(original, output, address); err == nil {
			t.Fatal("accepted noncanonical loopback address")
		}
		if _, err := os.Lstat(output); !os.IsNotExist(err) {
			t.Fatal("invalid address wrote output")
		}
	}
	if !reflect.DeepEqual(before, snapshot(t, original)) {
		t.Fatal("rejected output changed packet")
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func sha(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func decode(t *testing.T, data []byte, value any) {
	t.Helper()
	if err := json.Unmarshal(data, value, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
}
func marshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}
func clone(t *testing.T, source string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "packet")
	if err := os.CopyFS(dir, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	return dir
}
func snapshot(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = read(t, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// Resealing helpers implement the documented formats independently. They let
// tests distinguish an integrity failure from a false, internally sealed claim.
func resealPacket(t *testing.T, dir string) {
	var manifest report.Manifest
	decode(t, read(t, filepath.Join(dir, "manifest.json")), &manifest)
	files := snapshot(t, dir)
	names := make([]string, 0, len(files))
	for name := range files {
		if name != "manifest.json" && name != "identity.sha256" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	manifest.Files = []bundle.Payload{}
	for _, name := range names {
		manifest.Files = append(manifest.Files, bundle.Payload{Path: name, Size: len(files[name]), SHA256: sha(files[name])})
	}
	data := marshal(t, manifest)
	write(t, filepath.Join(dir, "manifest.json"), data)
	write(t, filepath.Join(dir, "identity.sha256"), []byte(sha(data)+"\n"))
}
func resealResult(t *testing.T, dir string) {
	t.Helper()
	resealDirectory(t, dir, testrunner.Schema)
}
func resealDirectory(t *testing.T, dir, schema string) {
	t.Helper()
	files := snapshot(t, dir)
	names := make([]string, 0, len(files))
	for name := range files {
		if name != "identity.sha256" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	hash := sha256.New()
	io.WriteString(hash, schema+"\n")
	for _, name := range names {
		for _, part := range [][]byte{[]byte(name), files[name]} {
			var size [8]byte
			binary.BigEndian.PutUint64(size[:], uint64(len(part)))
			hash.Write(size[:])
			hash.Write(part)
		}
	}
	write(t, filepath.Join(dir, "identity.sha256"), []byte(strings.ToLower(hex.EncodeToString(hash.Sum(nil)))+"\n"))
}
