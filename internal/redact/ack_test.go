package redact

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

func TestFixtureACKRecognitionRequiresTheReceiverRequestSubset(t *testing.T) {
	for _, test := range []struct {
		family, trigger, version string
		accepted                 bool
	}{
		{"SIU", "S12", "2.5.1", true},
		{"SIU", "S13", "2.5.1", true},
		{"SIU", "S15", "2.5.1", false},
		{"ADT", "S12", "2.5.1", false},
		{"SIU", "S12", "2.3", false},
	} {
		t.Run(test.family+"-"+test.trigger+"-"+test.version, func(t *testing.T) {
			dir := t.TempDir()
			request := fmt.Sprintf("MSH|^~\\&|INVENTED|TEST|READMIT|FIXTURE|20260101120000+0000||%s^%s|CONTROL-1|P|%s\rSCH|APPT^READMIT|FILL^READMIT|||||||||^^^20260102120000+0000\rPID|1||PATIENT^^^READMIT\r", test.family, test.trigger, test.version)
			source, err := bundle.Write(filepath.Join(dir, "case"), []bundle.Input{{Data: []byte(request)}}, bundle.Provenance{Mode: bundle.Derived, Derivation: "readmit-redact/v1"})
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			const session = "0123456789abcdef0123456789abcdef"
			// The scripted peer deliberately acknowledges unsupported requests.
			// Transport success alone cannot establish the fixture capability.
			ack := mllp.Frame([]byte(fmt.Sprintf("MSH|^~\\&|READMIT|FIXTURE|||20260101120000+0000||ACK^%s|READMITACK000001|P|2.5.1\rMSA|AA|CONTROL-1\rZRT|readmit-receipt/v1|%s|s0001-e000001\r", test.trigger, session)))
			done := make(chan error, 1)
			go func() {
				connection, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer connection.Close()
				if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
					done <- err
					return
				}
				reader, err := mllp.NewReader(connection, 4096)
				if err != nil {
					done <- err
					return
				}
				if _, err := reader.ReadFrame(); err != nil {
					done <- err
					return
				}
				_, err = connection.Write(ack)
				done <- err
			}()
			plan, err := replay.Prepare(filepath.Join(dir, "case"), fixtureTarget(listener.Addr().String()), replay.Options{})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			run, err := replay.Send(ctx, plan, filepath.Join(dir, "run"), replay.SendOptions{})
			if err != nil || !run.Successful() {
				t.Fatalf("scripted transport must succeed: %v", err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			artifact := &testrunner.Artifact{Run: run, Result: testrunner.Result{ReceiverSessionID: session}, FinalObservation: &observation.Snapshot{Processed: []observation.Occurrence{{OccurrenceID: "s0001-e000001", ControlID: "CONTROL-1"}}}}
			if accepted := verifyFixtureACKs(source, artifact) == nil; accepted != test.accepted {
				t.Fatalf("fixture ACK acceptance = %v, want %v", accepted, test.accepted)
			}
		})
	}
}
