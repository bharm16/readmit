package testrunner_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

const fixtureRoot = "../../testdata/fixtures/"
const currentSession = "1234567890abcdef1234567890abcdef"
const otherSession = "fedcba0987654321fedcba0987654321"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixtureRoot + name)
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

func jsonBytes(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func setup(t *testing.T) (string, string, testrunner.Spec) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.json")
	write(t, path, fixture(t, "test-reschedule.json"))
	var inputs []bundle.Input
	for _, name := range []string{"listen-s12.hl7", "listen-s13.hl7"} {
		inputs = append(inputs, bundle.Input{Data: fixture(t, name), Options: hl7.Options{Format: hl7.Raw}})
	}
	_, err := bundle.Write(filepath.Join(dir, "test-case"), inputs, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{Seed: 0, BaseTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), GeneratorVersion: "readmit-test-fixture-v1", ProfileVersion: "readmit-siu-v1"}})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := testrunner.ReadSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	return dir, path, spec
}

func target(t *testing.T, dir, address string) {
	t.Helper()
	write(t, filepath.Join(dir, "test-target.json"), jsonBytes(t, replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "100ms", MessageTimeout: "300ms", MaxACKBytes: 4096}))
}

type peer struct {
	listener net.Listener
	done     chan error
}

func serve(t *testing.T, dir string, count int, respond func(int, net.Conn) error) *peer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	target(t, dir, listener.Addr().String())
	p := &peer{listener: listener, done: make(chan error, 1)}
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			p.done <- nil
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		reader, _ := mllp.NewReader(conn, 1<<20)
		for i := 0; i < count; i++ {
			if _, err := reader.ReadFrame(); err != nil {
				p.done <- err
				return
			}
			if err := respond(i, conn); err != nil {
				p.done <- err
				return
			}
		}
		p.done <- nil
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return p
}

func (p *peer) wait(t *testing.T) {
	t.Helper()
	select {
	case err := <-p.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("peer did not finish")
	}
}

func initial() observation.Snapshot {
	return observation.Snapshot{Schema: observation.Schema, Profile: observation.Profile, SessionID: currentSession, Mode: observation.Fixed, Processed: []observation.Occurrence{}, Consistent: true, Records: []observation.Record{}}
}

func ledgerPeer(t *testing.T, dir, scenario string) *peer {
	t.Helper()
	path := filepath.Join(dir, "test-observation.json")
	start := initial()
	if err := observation.Create(path, start); err != nil {
		t.Fatal(err)
	}
	var records []observation.Record
	if err := json.Unmarshal(fixture(t, "listen-fixed.json"), &records, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if scenario == "large" {
		original := records[0]
		records = nil
		for i := 0; i < 64; i++ {
			record := original
			record.RecordID = fmt.Sprintf("r%06d", i+1)
			record.PatientID.Value = strings.Repeat("P", 1000)
			record.PlacerID.Value = strings.Repeat("L", 1000)
			record.FillerID.Value = strings.Repeat("F", 1000)
			records = append(records, record)
		}
	}
	return serve(t, dir, 2, func(i int, conn net.Conn) error {
		if i == 1 {
			final := initial()
			final.Records = records
			final.Processed = []observation.Occurrence{{OccurrenceID: "s0001-e000001", ControlID: "LISTEN-BOOK"}, {OccurrenceID: "s0001-e000003", ControlID: "LISTEN-MOVE"}}
			switch scenario {
			case "missing":
				if err := os.Remove(path); err != nil {
					return err
				}
			case "partial":
				if err := os.WriteFile(path, []byte(`{"schema":"readmit-observation/v1",`), 0600); err != nil {
					return err
				}
			case "stale":
			default:
				switch scenario {
				case "foreign":
					final.SessionID = otherSession
				case "wrong-mode":
					final.Mode = observation.Defective
				case "partial-list":
					final.Processed = final.Processed[:1]
				case "wrong-control":
					final.Processed[1].ControlID = "UNRELATED"
				case "reordered":
					final.Processed[0], final.Processed[1] = final.Processed[1], final.Processed[0]
				case "extra":
					final.Processed = append(final.Processed, observation.Occurrence{OccurrenceID: "s0001-e000005", ControlID: "EXTRA"})
				case "busy":
					final.Consistent = false
				}
				if err := observation.Write(path, final); err != nil {
					return err
				}
			}
		}
		session, id := currentSession, fmt.Sprintf("s0001-e%06d", 2*i+1)
		if scenario == "wrong-receipt-session" {
			session = otherSession
		}
		if scenario == "reused-receipt" {
			id = "s0001-e000001"
		}
		receipt := "ZRT|readmit-receipt/v1|" + session + "|" + id + "\r"
		if scenario == "missing-receipt" {
			receipt = ""
		}
		if scenario == "duplicate-receipt" {
			receipt += receipt
		}
		control := []string{"LISTEN-BOOK", "LISTEN-MOVE"}[i]
		_, err := conn.Write(mllp.Frame([]byte("MSH|^~\\&|FIXTURE|TEST|||20260101120000||ACK^S12|ACK|P|2.5.1\rMSA|AA|" + control + "\r" + receipt)))
		return err
	})
}

func execute(t *testing.T, path, output string) *testrunner.Artifact {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	artifact, err := testrunner.Run(ctx, path, output)
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func TestObservationAndReceiptFailuresCannotPass(t *testing.T) {
	for _, scenario := range []string{"missing", "partial", "stale", "foreign", "wrong-mode", "partial-list", "wrong-control", "reordered", "extra", "busy", "wrong-receipt-session", "reused-receipt", "missing-receipt", "duplicate-receipt"} {
		t.Run(scenario, func(t *testing.T) {
			dir, path, _ := setup(t)
			peer := ledgerPeer(t, dir, scenario)
			artifact := execute(t, path, filepath.Join(dir, "result"))
			peer.wait(t)
			if artifact.Result.Status != testrunner.ExecutionError || artifact.Run == nil || artifact.Result.ReceiverSessionID != "" {
				t.Fatalf("invalid observation passed: %+v", artifact.Result)
			}
			for _, a := range artifact.Result.Assertions {
				if a.Status != "not_evaluated" || a.Observed != nil {
					t.Fatal("assertion passed without trustworthy observation")
				}
			}
		})
	}
}

func TestInitialLedgerMustBeFreshAndEmptyBeforeConnecting(t *testing.T) {
	for _, scenario := range []string{"processed", "records", "busy", "partial"} {
		t.Run(scenario, func(t *testing.T) {
			dir, path, _ := setup(t)
			listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			target(t, dir, listener.Addr().String())
			snapshot := initial()
			if scenario == "processed" {
				snapshot.Processed = []observation.Occurrence{{OccurrenceID: "s0001-e000001", ControlID: "OLD"}}
			}
			if scenario == "records" {
				if err := json.Unmarshal(fixture(t, "listen-fixed.json"), &snapshot.Records); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "busy" {
				snapshot.Consistent = false
			}
			raw := jsonBytes(t, snapshot)
			if scenario == "partial" {
				raw = raw[:12]
			}
			write(t, filepath.Join(dir, "test-observation.json"), raw)
			artifact := execute(t, path, filepath.Join(dir, "result"))
			if artifact.Result.Status != testrunner.ExecutionError || artifact.Result.ErrorClass != "initial_observation" || artifact.Run != nil {
				t.Fatal("nonempty or invalid initial state was accepted")
			}
			_ = listener.SetDeadline(time.Now().Add(20 * time.Millisecond))
			if conn, err := listener.Accept(); err == nil {
				conn.Close()
				t.Fatal("invalid initial state caused a send")
			}
		})
	}
}

func ackSpec(t *testing.T, path string, spec testrunner.Spec, code string) testrunner.Spec {
	t.Helper()
	spec.Input.Messages = []string{"s0001-e000001"}
	spec.Observation = testrunner.Observation{Boundary: testrunner.ACKBoundary}
	spec.Setup.InitialState = "operator-declared"
	spec.Assertions = []testrunner.Assertion{{ID: "ack", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &code}}}}
	write(t, path, jsonBytes(t, spec))
	return spec
}

func TestACKAEARAndFieldSelectorStatesAreAssertable(t *testing.T) {
	for _, code := range []string{"AA", "AE", "AR"} {
		t.Run(code, func(t *testing.T) {
			dir, path, spec := setup(t)
			spec = ackSpec(t, path, spec, code)
			for i, field := range []struct {
				selector string
				state    hl7.State
				text     string
			}{{"MSA-3", hl7.Empty, ""}, {"MSA-4.1", hl7.Null, ""}, {"MSA-99", hl7.Omitted, ""}, {"ERR[2]-1[2].2", hl7.Present, "A|B"}, {"ERR-3", hl7.Omitted, ""}} {
				value := testrunner.FieldValue{State: field.state}
				if field.state == hl7.Present {
					value.Text = &field.text
				}
				spec.Assertions = append(spec.Assertions, testrunner.Assertion{ID: fmt.Sprintf("field-%d", i), Operator: "ack_field_equals", Message: "s0001-e000001", Selector: field.selector, Expected: testrunner.Value{Field: &value}})
			}
			write(t, path, jsonBytes(t, spec))
			peer := serve(t, dir, 1, func(_ int, conn net.Conn) error {
				_, err := conn.Write(mllp.Frame([]byte("MSH|^~\\&|FIXTURE|TEST|||20260101120000||ACK^S12|ACK|P|2.5.1\rMSA|" + code + "|LISTEN-BOOK||\"\"\rERR|first\rERR|one~second^A\\F\\B\r")))
				return err
			})
			artifact := execute(t, path, filepath.Join(dir, "result"))
			peer.wait(t)
			if artifact.Result.Status != testrunner.Pass || artifact.FinalObservation != nil {
				t.Fatalf("ACK field observations were not asserted: %+v", artifact.Result)
			}
		})
	}
}

func TestTransportUncertaintyAndMissingACKAreExecutionErrors(t *testing.T) {
	for _, scenario := range []string{"refused", "disconnect", "timeout", "mismatch", "unsupported-escape", "non-utf8"} {
		t.Run(scenario, func(t *testing.T) {
			dir, path, spec := setup(t)
			spec = ackSpec(t, path, spec, "AA")
			if scenario == "unsupported-escape" || scenario == "non-utf8" {
				spec.Assertions[0].Selector = "MSA-3"
				write(t, path, jsonBytes(t, spec))
			}
			peer := serve(t, dir, 1, func(_ int, conn net.Conn) error {
				switch scenario {
				case "disconnect":
					return nil
				case "timeout":
					_, _ = io.Copy(io.Discard, conn)
					return nil
				default:
					control, extra := "LISTEN-BOOK", ""
					if scenario == "mismatch" {
						control = "OTHER"
					}
					if scenario == "unsupported-escape" {
						extra = `|\Zprivate\`
					}
					if scenario == "non-utf8" {
						extra = "|\xff"
					}
					_, err := conn.Write(mllp.Frame([]byte("MSH|^~\\&|FIXTURE|TEST|||20260101120000||ACK^S12|ACK|P|2.5.1\rMSA|AA|" + control + extra + "\r")))
					return err
				}
			})
			if scenario == "refused" {
				_ = peer.listener.Close()
			}
			artifact := execute(t, path, filepath.Join(dir, "result"))
			peer.wait(t)
			if artifact.Result.Status != testrunner.ExecutionError {
				t.Fatal("uncertain delivery or invalid observed field passed")
			}
		})
	}
}

func TestSpecRejectsUnknownDuplicateAndUnsupportedContracts(t *testing.T) {
	raw := fixture(t, "test-reschedule.json")
	for _, change := range []struct{ old, new string }{
		{`"name":`, `"command":"rm -rf /", "name":`},
		{`"name":`, `"name":"duplicate", "name":`},
		{"readmit-test/v1", "readmit-test/v2"},
		{"ledger_count", "shell"},
		{`"count": 1`, `"count": null`},
		{`"count": 1`, `"count": -1`},
		{`"state": "present", "text": "AA"`, `"state": "null", "text": "AA"`},
		{"MSA-1", "MSA-0"},
		{"MSA-1", "PID-3.1"},
		{"empty-ledger", "assume-unchanged"},
	} {
		if _, err := testrunner.DecodeSpec(bytes.Replace(raw, []byte(change.old), []byte(change.new), 1)); err == nil {
			t.Fatalf("accepted unsupported contract change %q", change.new)
		}
	}
}

// Rehash independently follows the published directory identity, ensuring the
// reader checks semantic evidence as well as a superficial checksum mismatch.
func rehash(t *testing.T, dir string) {
	t.Helper()
	var paths []string
	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
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
		rel = filepath.ToSlash(rel)
		if rel != "identity.sha256" {
			paths = append(paths, rel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	h := sha256.New()
	_, _ = io.WriteString(h, "readmit-result/v1\n")
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range [][]byte{[]byte(path), data} {
			var size [8]byte
			binary.BigEndian.PutUint64(size[:], uint64(len(part)))
			_, _ = h.Write(size[:])
			_, _ = h.Write(part)
		}
	}
	write(t, filepath.Join(dir, "identity.sha256"), []byte(hex.EncodeToString(h.Sum(nil))+"\n"))
}

func TestOpenRejectsTamperedVerdictReferencesAndEvidence(t *testing.T) {
	for _, scenario := range []string{"verdict", "expected", "run-identity", "target", "observation", "payload", "extra", "missing-marker"} {
		t.Run(scenario, func(t *testing.T) {
			dir, path, _ := setup(t)
			peer := ledgerPeer(t, dir, "valid")
			output := filepath.Join(dir, "result")
			artifact := execute(t, path, output)
			peer.wait(t)
			if artifact.Result.Status != testrunner.Pass {
				t.Fatal("tamper baseline did not pass")
			}
			result := artifact.Result
			switch scenario {
			case "verdict":
				result.Status = testrunner.AssertionFailure
			case "expected":
				*result.Assertions[0].Assertion.Expected.Count = 2
			case "run-identity":
				result.Run.Identity = strings.Repeat("a", 64)
			case "target":
				result.Target.Address = "127.0.0.1:1"
			case "observation":
				snapshot := *artifact.FinalObservation
				snapshot.SessionID = otherSession
				data := jsonBytes(t, snapshot)
				write(t, filepath.Join(output, "observation.json"), data)
				sum := sha256.Sum256(data)
				result.FinalObservation.SHA256 = hex.EncodeToString(sum[:])
				result.FinalObservation.Size = len(data)
			case "payload":
				write(t, filepath.Join(output, "run", artifact.Run.Events[0].Received.Path), []byte("changed"))
			case "extra":
				write(t, filepath.Join(output, "extra.txt"), []byte("extra"))
			}
			write(t, filepath.Join(output, "result.json"), jsonBytes(t, result))
			rehash(t, output)
			if scenario == "missing-marker" {
				if err := os.Remove(filepath.Join(output, "identity.sha256")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := testrunner.Open(output); err == nil {
				t.Fatal("tampered result accepted")
			}
		})
	}
}

func TestResultOpensAfterRelocationWithoutOriginalFiles(t *testing.T) {
	dir, path, _ := setup(t)
	peer := ledgerPeer(t, dir, "valid")
	output := filepath.Join(dir, "result")
	before := execute(t, path, output)
	peer.wait(t)
	copyPath := filepath.Join(t.TempDir(), "copied")
	if err := os.CopyFS(copyPath, os.DirFS(output)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	after, err := testrunner.Open(copyPath)
	if err != nil || after.Identity != before.Identity || after.Result.Status != testrunner.Pass {
		t.Fatalf("offline relocated result rejected: %v", err)
	}
}

func TestTestOutputCannotAlterSourceAndPreparedSpecChangePreventsSend(t *testing.T) {
	dir, path, _ := setup(t)
	target(t, dir, "127.0.0.1:2575")
	plan, err := testrunner.Prepare(path)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "test-case")
	before, err := bundle.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []string{filepath.Join(source, "test-result")}
	if runtime.GOOS != "windows" {
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(filepath.Join(source, "payloads"), alias); err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, alias+"/../test-result")
	}
	for _, output := range outputs {
		if _, err := testrunner.Execute(context.Background(), plan, output); err == nil {
			t.Fatal("test modified immutable source")
		}
	}
	write(t, path, []byte("invalid"))
	artifact, err := testrunner.Execute(context.Background(), plan, filepath.Join(dir, "changed-result"))
	if err != nil || artifact.Result.ErrorClass != "configuration_changed" || artifact.Run != nil {
		t.Fatal("changed spec was executed")
	}
	if _, err := testrunner.Run(context.Background(), path, filepath.Join(source, "configuration-error")); err == nil {
		t.Fatal("invalid configuration wrote into immutable source")
	}
	after, err := bundle.Open(source)
	if err != nil || before.Identity != after.Identity {
		t.Fatal("source bytes changed")
	}
}

func TestRejectedACKCanFailAnAssertionWithoutBecomingExecutionError(t *testing.T) {
	dir, path, spec := setup(t)
	ackSpec(t, path, spec, "AA")
	peer := serve(t, dir, 1, func(_ int, conn net.Conn) error {
		_, err := conn.Write(mllp.Frame([]byte("MSH|^~\\&|FIXTURE|TEST|||20260101120000||ACK^S12|ACK|P|2.5.1\rMSA|AR|LISTEN-BOOK\r")))
		return err
	})
	artifact := execute(t, path, filepath.Join(dir, "result"))
	peer.wait(t)
	if artifact.Result.Status != testrunner.AssertionFailure || artifact.Result.ErrorClass != "" || *artifact.Result.Assertions[0].Observed.Field.Text != "AR" {
		t.Fatal("AR was not evaluated as actual ACK evidence")
	}
}

func TestUnattemptedMessagesAndCancelledExecutionCannotPass(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			dir, path, spec := setup(t)
			spec.Observation = testrunner.Observation{Boundary: testrunner.ACKBoundary}
			spec.Setup.InitialState = "operator-declared"
			spec.Assertions = spec.Assertions[2:]
			write(t, path, jsonBytes(t, spec))
			peer := serve(t, dir, 1, func(_ int, _ net.Conn) error { return nil })
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if cancelled {
				cancel()
			}
			artifact, err := testrunner.Run(ctx, path, filepath.Join(dir, "result"))
			if err != nil {
				t.Fatal(err)
			}
			if cancelled {
				_ = peer.listener.Close()
			}
			peer.wait(t)
			if artifact.Result.Status != testrunner.ExecutionError || artifact.Run.Events[1].Outcome != replay.NotAttempted {
				t.Fatal("unattempted messages did not fail execution")
			}
			for _, assertion := range artifact.Result.Assertions {
				if assertion.Status != "not_evaluated" {
					t.Fatal("an incomplete run evaluated assertions")
				}
			}
		})
	}
}

func TestUnknownSourceSelectionIsAConfigurationError(t *testing.T) {
	dir, path, spec := setup(t)
	target(t, dir, "127.0.0.1:2575")
	spec.Input.Messages[1] = "s9999-e999999"
	spec.Assertions = spec.Assertions[:2]
	write(t, path, jsonBytes(t, spec))
	artifact := execute(t, path, filepath.Join(dir, "result"))
	if artifact.Result.Status != testrunner.ExecutionError || artifact.Result.ErrorClass != "configuration" || artifact.Run != nil {
		t.Fatal("unknown source occurrence was silently dropped")
	}
}

func TestRepeatedLargeAssertionEvidenceProducesABoundedExecutionError(t *testing.T) {
	dir, path, spec := setup(t)
	spec.Assertions = nil
	empty := []observation.Record{}
	for i := 0; i < 64; i++ {
		spec.Assertions = append(spec.Assertions, testrunner.Assertion{ID: fmt.Sprintf("records-%d", i), Operator: "ledger_equals", Expected: testrunner.Value{Records: &empty}})
	}
	write(t, path, jsonBytes(t, spec))
	peer := ledgerPeer(t, dir, "large")
	artifact := execute(t, path, filepath.Join(dir, "result"))
	peer.wait(t)
	if artifact.Result.Status != testrunner.ExecutionError || artifact.Result.ErrorClass != "assertion_evidence_limit" || artifact.FinalObservation == nil || len(artifact.FinalObservation.Records) != 64 {
		t.Fatal("large assertions did not retain bounded error evidence")
	}
	for _, assertion := range artifact.Result.Assertions {
		if assertion.Observed != nil {
			t.Fatal("oversized repeated observation was retained in result")
		}
	}
}

func TestEmptyNullAndOmittedExpectedValuesAreDistinct(t *testing.T) {
	for _, state := range []hl7.State{hl7.Empty, hl7.Null, hl7.Omitted} {
		t.Run(string(state), func(t *testing.T) {
			dir, path, spec := setup(t)
			spec = ackSpec(t, path, spec, "AA")
			spec.Assertions[0].Selector = "MSA-3"
			spec.Assertions[0].Expected.Field = &testrunner.FieldValue{State: state}
			write(t, path, jsonBytes(t, spec))
			peer := serve(t, dir, 1, func(_ int, conn net.Conn) error {
				_, err := conn.Write(mllp.Frame([]byte("MSH|^~\\&|FIXTURE|TEST|||20260101120000||ACK^S12|ACK|P|2.5.1\rMSA|AA|LISTEN-BOOK|\"\"\r")))
				return err
			})
			artifact := execute(t, path, filepath.Join(dir, "result"))
			peer.wait(t)
			want := testrunner.AssertionFailure
			if state == hl7.Null {
				want = testrunner.Pass
			}
			if artifact.Result.Status != want {
				t.Fatal("field state comparison collapsed null, empty, or omitted")
			}
		})
	}
}
