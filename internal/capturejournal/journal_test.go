package capturejournal_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/durablerun"
)

const frame = "MSH|^~\\&|SENDER|FACILITY|READMIT|COLLECT|20260101120000||ADT^A01|CAPTURE-001|P|2.5.1\r"

func plan() capturejournal.Capture {
	sum := sha256.Sum256([]byte("policy"))
	return capturejournal.Capture{
		CreatedAt: time.Now().UTC(), SessionID: "0123456789abcdef0123456789abcdef",
		PolicyName: "downstream-sink", PolicySchema: "readmit-receiver-policy/v2", PolicySHA256: hex.EncodeToString(sum[:]),
		Limits:    capturejournal.Limits{MaxConnections: 4, MaxSessions: 0, MaxMessages: 2, MaxCaptureBytes: 0, MaxFrameBytes: 1 << 20},
		Transport: capturejournal.Transport{TLS: true, ClientCertificate: true},
	}
}

func create(t *testing.T) (*capturejournal.Writer, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journal")
	writer, err := capturejournal.Create(path, plan())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writer.Close() })
	return writer, path
}

// TestJournalReportsAFinalizedCapture is the ordinary path: everything received
// was retained, everything intended was sent, and nothing is left uncertain.
func TestJournalReportsAFinalizedCapture(t *testing.T) {
	writer, path := create(t)
	if err := writer.Received("c0001", "s0001-e000001", "CAPTURE-001", []byte(frame)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Intent("c0001", "s0001-e000001", capturejournal.ApplicationStage, capturejournal.SameConnection); err != nil {
		t.Fatal(err)
	}
	if err := writer.Sent("c0001", "s0001-e000001", capturejournal.ApplicationStage, 64); err != nil {
		t.Fatal(err)
	}
	final := writer.Finish(capturejournal.Finalized)
	if final.State != capturejournal.Finalized || final.Received != 1 || final.Acknowledged != 1 || final.Unsent != 0 || final.Uncertain != 0 || final.DeliveryUncertain {
		t.Fatalf("a completed capture was not finalized cleanly: %+v", final)
	}
	writer.Close()
	recovered, err := capturejournal.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != final {
		t.Fatalf("recovery disagreed with the capture: %+v vs %+v", recovered, final)
	}
	if recovered.ExitCode() != 0 {
		t.Fatalf("a finalized capture reported exit code %d", recovered.ExitCode())
	}
}

// TestJournalRetainsUncertainDeliveryAfterAKillMidSend is the invariant this
// journal exists for. The process stops between syncing the intent to send an
// acknowledgement and recording that the send returned, exactly as a kill in
// the middle of the write would. Recovery must say the effect is unknown, must
// not claim the capture finished, and must not send anything.
func TestJournalRetainsUncertainDeliveryAfterAKillMidSend(t *testing.T) {
	writer, path := create(t)
	if err := writer.Received("c0001", "s0001-e000001", "CAPTURE-001", []byte(frame)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Intent("c0001", "s0001-e000001", capturejournal.ApplicationStage, capturejournal.SameConnection); err != nil {
		t.Fatal(err)
	}
	// No Finish and no Sent: the writer stopped where a kill would stop it.
	writer.Close()
	before := listing(t, path)
	recovered, err := capturejournal.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != durablerun.DeliveryUncertain {
		t.Fatalf("an interrupted send was not reported as uncertain: %+v", recovered)
	}
	if recovered.StopReason != durablerun.Interrupted || !recovered.Recovered {
		t.Fatalf("an interrupted capture did not report interruption: %+v", recovered)
	}
	if recovered.Received != 1 || recovered.Acknowledged != 0 || recovered.Uncertain != 1 || !recovered.DeliveryUncertain {
		t.Fatalf("recovery lost the received frame or resolved the uncertain send: %+v", recovered)
	}
	if recovered.State == capturejournal.Finalized || recovered.ExitCode() == 0 {
		t.Fatal("an incomplete capture was reported as a finished one")
	}
	// Recovery reports; it never sends, resends, resumes or repairs.
	if after := listing(t, path); after != before {
		t.Fatalf("recovery changed retained evidence:\n%s\n%s", before, after)
	}
	again, err := capturejournal.Open(path)
	if err != nil || again != recovered {
		t.Fatalf("recovering twice changed the answer: %+v %v", again, err)
	}
	retained, err := os.ReadFile(filepath.Join(path, "received", "s0001-e000001.bin"))
	if err != nil || string(retained) != frame {
		t.Fatalf("the frame received before the interruption was lost: %q %v", retained, err)
	}
}

// TestJournalReportsATornTrailingRecordAsUncertain keeps a half-written record
// from being read as the capture's last word.
func TestJournalReportsATornTrailingRecordAsUncertain(t *testing.T) {
	writer, path := create(t)
	if err := writer.Received("c0001", "s0001-e000001", "CAPTURE-001", []byte(frame)); err != nil {
		t.Fatal(err)
	}
	writer.Finish(capturejournal.Finalized)
	writer.Close()
	name := filepath.Join(path, "journal.jsonl")
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, append(data, []byte(`{"sequence":5,"kind":"se`)...), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := capturejournal.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.JournalIncomplete || !recovered.DeliveryUncertain || recovered.State != durablerun.DeliveryUncertain {
		t.Fatalf("a torn trailing record was not reported as incomplete: %+v", recovered)
	}
	if recovered.StopReason != durablerun.Interrupted {
		t.Fatalf("a torn record kept the finished stop reason: %+v", recovered)
	}
}

// TestJournalRefusesChangedOrForgedEvidence keeps recovery from repairing what
// it cannot verify. Each case below is refused outright rather than reported as
// a partially trusted capture.
func TestJournalRefusesChangedOrForgedEvidence(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, path string){
		"changed retained frame": func(t *testing.T, path string) {
			if err := os.WriteFile(filepath.Join(path, "received", "s0001-e000001.bin"), []byte("other bytes"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"removed retained frame": func(t *testing.T, path string) {
			if err := os.Remove(filepath.Join(path, "received", "s0001-e000001.bin")); err != nil {
				t.Fatal(err)
			}
		},
		"changed plan": func(t *testing.T, path string) {
			if err := os.WriteFile(filepath.Join(path, "capture.json"), []byte(`{"schema":"readmit-capture-journal/v1"}`), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"broken chain": func(t *testing.T, path string) {
			name := filepath.Join(path, "journal.jsonl")
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.SplitAfter(string(data), "\n")
			if err := os.WriteFile(name, []byte(strings.Join(lines[1:], "")), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"unknown record member": func(t *testing.T, path string) {
			name := filepath.Join(path, "journal.jsonl")
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(name, append(data, []byte(`{"sequence":9,"previous":"x","at":"2026-09-18T00:00:00Z","kind":"received","invented":1}`+"\n")...), 0600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			writer, path := create(t)
			if err := writer.Received("c0001", "s0001-e000001", "CAPTURE-001", []byte(frame)); err != nil {
				t.Fatal(err)
			}
			writer.Finish(capturejournal.Finalized)
			writer.Close()
			damage(t, path)
			if _, err := capturejournal.Open(path); err == nil {
				t.Fatal("damaged capture evidence was accepted")
			}
		})
	}
}

// TestJournalRefusesAForgedFinishThatHidesAnUncertainSend is the one forgery
// that would matter most: a terminal record claiming a clean capture while an
// acknowledgement's effect was never resolved.
func TestJournalRefusesAForgedFinishThatHidesAnUncertainSend(t *testing.T) {
	writer, path := create(t)
	if err := writer.Received("c0001", "s0001-e000001", "CAPTURE-001", []byte(frame)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Intent("c0001", "s0001-e000001", capturejournal.ApplicationStage, capturejournal.SameConnection); err != nil {
		t.Fatal(err)
	}
	final := writer.Finish(capturejournal.Finalized)
	if final.State != durablerun.DeliveryUncertain || !final.DeliveryUncertain {
		t.Fatalf("finishing over an unresolved send claimed a clean capture: %+v", final)
	}
	writer.Close()
	recovered, err := capturejournal.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != durablerun.DeliveryUncertain || recovered.Uncertain != 1 || recovered.ExitCode() == 0 {
		t.Fatalf("an uncertain send was reported as a finished capture: %+v", recovered)
	}
}

// TestJournalRefusesAnUnusableDestination keeps the journal inside the one path
// policy and inside the contract it declares.
func TestJournalRefusesAnUnusableDestination(t *testing.T) {
	directory := t.TempDir()
	if _, err := capturejournal.Create("", plan()); err == nil {
		t.Fatal("an empty destination was accepted")
	}
	existing := filepath.Join(directory, "existing")
	if err := os.Mkdir(existing, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := capturejournal.Create(existing, plan()); err == nil {
		t.Fatal("an existing directory was accepted")
	}
	broken := plan()
	broken.PolicySHA256 = "not a digest"
	if _, err := capturejournal.Create(filepath.Join(directory, "broken"), plan()); err != nil {
		t.Fatal(err)
	}
	if _, err := capturejournal.Create(filepath.Join(directory, "refused"), broken); err == nil {
		t.Fatal("a plan without the declared policy digest was accepted")
	}
}

// TestJournalRecordsAFailedSendAsUnsentRatherThanAcknowledged keeps a write
// that did not complete out of the acknowledged count. The intent is resolved,
// because what happened is known, and the bytes that did reach the socket are
// retained: bytes already sent cannot be retracted, and a peer that never read
// them was not acknowledged.
func TestJournalRecordsAFailedSendAsUnsentRatherThanAcknowledged(t *testing.T) {
	writer, path := create(t)
	if err := writer.Received("c0001", "s0001-e000001", "CAPTURE-001", []byte(frame)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Intent("c0001", "s0001-e000001", capturejournal.AcceptStage, capturejournal.SameConnection); err != nil {
		t.Fatal(err)
	}
	if err := writer.Unsent("c0001", "s0001-e000001", capturejournal.AcceptStage, 12); err != nil {
		t.Fatal(err)
	}
	final := writer.Finish(capturejournal.Finalized)
	if final.Acknowledged != 0 || final.Unsent != 1 || final.Uncertain != 0 || final.DeliveryUncertain {
		t.Fatalf("a failed send was not recorded as unsent: %+v", final)
	}
	writer.Close()
	recovered, err := capturejournal.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Acknowledged != 0 || recovered.Unsent != 1 {
		t.Fatalf("recovery counted a failed send as an acknowledgement: %+v", recovered)
	}
}

// TestJournalRefusesAFrameItCannotName keeps every retained byte attributable
// to the connection and occurrence that produced it.
func TestJournalRefusesAFrameItCannotName(t *testing.T) {
	writer, _ := create(t)
	if err := writer.Received("session", "s0001-e000001", "CAPTURE-001", []byte(frame)); err == nil {
		t.Fatal("a frame naming no recorded connection was accepted")
	}
	if err := writer.Received("c0001", "occurrence", "CAPTURE-001", []byte(frame)); err == nil {
		t.Fatal("a frame naming no case occurrence was accepted")
	}
	if err := writer.Intent("c0001", "s0001-e000001", "invented", capturejournal.SameConnection); err == nil {
		t.Fatal("an acknowledgement stage outside the declared two was accepted")
	}
	if err := writer.Intent("c0001", "s0001-e000001", capturejournal.AcceptStage, "elsewhere"); err == nil {
		t.Fatal("an acknowledgement destination outside the declared set was accepted")
	}
	if err := writer.Sent("c0001", "s0001-e000001", capturejournal.AcceptStage, -1); err == nil {
		t.Fatal("a negative sent count was accepted")
	}
}

// listing is a stable description of everything the journal directory holds, so
// a test can state that recovery changed nothing.
func listing(t *testing.T, path string) string {
	t.Helper()
	var described strings.Builder
	err := filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			described.WriteString(name + "/\n")
			return nil
		}
		data, readErr := os.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(data)
		described.WriteString(name + " " + hex.EncodeToString(sum[:]) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return described.String()
}
