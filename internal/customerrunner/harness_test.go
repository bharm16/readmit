package customerrunner_test

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testlicense"
	"github.com/bharm16/readmit/internal/testrunner"
)

// receiver is a loopback MLLP endpoint standing in for the system under
// test: it reads the job's one message, reports it, and acknowledges it only
// once the test says so; a connection the runner ends goes unacknowledged.
type receiver struct {
	address  string
	received chan struct{}
	ack      chan struct{}
	ended    chan struct{}
}

const acceptedACK = "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|AA|LISTEN-BOOK\r\x1c\r"

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	r := &receiver{address: listener.Addr().String(), received: make(chan struct{}), ack: make(chan struct{}), ended: make(chan struct{})}
	go func() {
		defer close(r.ended)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(30 * time.Second))
		reader, _ := mllp.NewReader(conn, 1<<20)
		if _, err := reader.ReadFrame(); err != nil {
			return
		}
		close(r.received)
		ended := make(chan struct{})
		go func() {
			io.Copy(io.Discard, conn)
			close(ended)
		}()
		select {
		case <-r.ack:
			fmt.Fprint(conn, acceptedACK)
			<-ended
		case <-ended:
		}
	}()
	return r
}

// waitReceived waits for the job's message to reach the receiver.
func (r *receiver) waitReceived(t *testing.T) {
	t.Helper()
	select {
	case <-r.received:
	case <-time.After(10 * time.Second):
		t.Fatal("the job sent nothing for ten seconds")
	}
}

// jobSpec writes a one-message acknowledgement test of the synthetic
// listen-s12 fixture against address, whose target names environment, and
// returns the spec's path. An empty environment writes a readmit-target/v2
// target, which names none.
func jobSpec(t *testing.T, address, environment string) string {
	t.Helper()
	dir := t.TempDir()
	raw, err := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bundle.Write(filepath.Join(dir, "case"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	target := replay.Target{Schema: replay.TargetSchemaV3, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "30s", MaxACKBytes: 4096, Name: environment, Classification: replay.Nonproduction}
	if environment == "" {
		target = replay.Target{Schema: replay.TargetSchemaV2, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "30s", MaxACKBytes: 4096}
	}
	accepted := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "ACK", Input: testrunner.Input{Case: "case", Messages: []string{"s0001-e000001"}}, Target: "target.json", Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset fixture"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "accepted", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &accepted}}}}}
	for name, v := range map[string]any{"target.json": target, "spec.json": spec} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "spec.json")
}

// waitEnded waits for the connection to end without the test acknowledging
// the message: the runner stopped the job.
func (r *receiver) waitEnded(t *testing.T) {
	t.Helper()
	select {
	case <-r.ended:
	case <-time.After(10 * time.Second):
		t.Fatal("the job kept its connection open for ten seconds")
	}
}

// outcome is what one run answered.
type outcome struct {
	summary durablerun.Summary
	err     error
}

// admitted runs job through hub and clock as `readmit runner execute` runs
// one: admitted as its own execution, through a fresh activation.
func admitted(t *testing.T, c customerrunner.Config, job customerrunner.Job, hub customerrunner.Hub, clock customerrunner.Clock) outcome {
	t.Helper()
	return <-start(t, c, job, hub, clock)
}

// start is admitted in its own goroutine; the outcome arrives when the run
// ends.
func start(t *testing.T, c customerrunner.Config, job customerrunner.Job, hub customerrunner.Hub, clock customerrunner.Clock) <-chan outcome {
	t.Helper()
	guard := operationguard.New(testlicense.New(t))
	eachJob := operationguard.Profile{Name: "runner", Execution: operationguard.ExecuteEachJob}
	done := make(chan outcome, 1)
	go func() {
		var out outcome
		out.err = guard.Run(context.Background(), eachJob, func(ctx context.Context) (err error) {
			out.summary, err = customerrunner.RunWithForTest(ctx, c, job, hub, clock)
			return err
		})
		done <- out
	}()
	return done
}

// finished waits for a started run's outcome.
func finished(t *testing.T, runs <-chan outcome) outcome {
	t.Helper()
	select {
	case out := <-runs:
		return out
	case <-time.After(20 * time.Second):
		t.Fatal("the run did not end within twenty seconds")
		return outcome{}
	}
}
