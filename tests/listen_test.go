package tests

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
)

func TestListenExecutableExportsBothLedgersAndReopensRecordedCase(t *testing.T) {
	for _, mode := range []string{"fixed", "defective"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			casePath := filepath.Join(dir, "case")
			observationPath := filepath.Join(dir, "observation.json")
			receiver := startReceiver(t, 15*time.Second, "listen", "--address", "127.0.0.1:0", "--mode", mode, "--output", casePath, "--observation", observationPath, "--max-messages", "2", "--idle-timeout", "2s")
			initial, err := observation.Read(observationPath)
			if err != nil {
				t.Fatal(err)
			}
			conn, err := net.DialTimeout("tcp", receiver.address, 2*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			frames, _ := mllp.NewReader(conn, 1<<20)
			for i, name := range []string{"listen-s12.hl7", "listen-s13.hl7"} {
				payload, err := os.ReadFile("../testdata/fixtures/" + name)
				if err != nil {
					t.Fatal(err)
				}
				frame := mllp.Frame(payload)
				for _, part := range [][]byte{frame[:2], frame[2:21], frame[21:]} {
					if _, err := conn.Write(part); err != nil {
						t.Fatal(err)
					}
				}
				response, err := frames.ReadFrame()
				if err != nil {
					t.Fatal(err)
				}
				doc, err := hl7.Parse(response, hl7.Options{Format: hl7.MLLP})
				if err != nil {
					t.Fatal(err)
				}
				msa := doc.Messages[0].Segments[1]
				wantControl := []string{"LISTEN-BOOK", "LISTEN-MOVE"}[i]
				if string(doc.Bytes(msa.Field(1).Span)) != "AA" || string(doc.Bytes(msa.Field(2).Span)) != wantControl {
					t.Fatalf("wrong executable ACK: %q", response)
				}
				observed, err := observation.Read(observationPath)
				if err != nil || observed.SessionID != initial.SessionID || !observed.Consistent || len(observed.Processed) != i+1 {
					t.Fatalf("incomplete handoff after ACK: %+v %v", observed, err)
				}
			}
			remaining := receiver.wait(t)
			if receiver.diagnostic.Len() != 0 {
				t.Fatalf("unexpected diagnostic: %s", receiver.diagnostic.String())
			}
			output := receiver.readyLine + remaining
			for _, want := range []string{"Schema: readmit-case/v2", "Provenance: recorded", "Messages: 2", "ACKs: 2", "Matched ACKs: 2", "Observation: readmit-observation/v1", "Processed occurrences: 2", "Consistent: true"} {
				if !strings.Contains(output, want) {
					t.Errorf("missing summary %q in %s", want, output)
				}
			}
			for _, private := range []string{"SYNTH-001", "LISTEN-BOOK", "APPT-001", dir} {
				if strings.Contains(output, private) {
					t.Errorf("default output disclosed %q", private)
				}
			}
			b, err := bundle.Open(casePath)
			if err != nil {
				t.Fatal(err)
			}
			records := readStrictDocument[[]observation.Record](t, "../testdata/fixtures/listen-"+mode+".json")
			if !reflect.DeepEqual(b.Observation.Records, records) {
				t.Fatal("executable ledger does not match hand-authored expectation")
			}
			timeline, stderr, err := run(t, "timeline", casePath)
			if err != nil || stderr != "" || !strings.Contains(timeline, "Receiver mode: "+mode) || !strings.Contains(timeline, "Observation: readmit-observation/v1") || strings.Contains(timeline, "SYNTH") {
				t.Fatalf("recorded timeline: %v %s %s", err, stderr, timeline)
			}
			timeline, stderr, err = run(t, "timeline", casePath, "--show-values")
			if err != nil || stderr != "" || !strings.Contains(timeline, "SYNTH-001") || !strings.Contains(timeline, "Observation values:") {
				t.Fatal("explicit recorded observation values unavailable")
			}
		})
	}
}

func TestListenInvalidArgumentsArePrivate(t *testing.T) {
	for _, tail := range [][]string{
		{}, {"--mode", "SECRET"}, {"--max-frame-bytes", "0"}, {"--max-messages", "4001"}, {"--idle-timeout", "0s"}, {"--output", "SECRET-MISSING/child", "--observation", "SECRET-MISSING/other"},
	} {
		args := append([]string{"listen", "--address", "127.0.0.1:0"}, tail...)
		stdout, stderr, err := run(t, args...)
		if err == nil || stdout != "" || stderr == "" || len(stderr) > 300 || strings.Contains(stderr, "SECRET") {
			t.Fatalf("unsafe listen error: %v %q %q", err, stdout, stderr)
		}
	}
}
