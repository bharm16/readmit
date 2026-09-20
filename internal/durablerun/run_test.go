package durablerun_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/testlicense"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

func setup(t *testing.T, address string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	raw, err := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	_, err = bundle.Write(filepath.Join(dir, "case"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	target := replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "200ms", MessageTimeout: "300ms", MaxACKBytes: 4096}
	val := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "ACK", Input: testrunner.Input{Case: "case", Messages: []string{"s0001-e000001"}}, Target: "target.json", Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset fixture"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "accepted", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &val}}}}}
	for name, v := range map[string]any{"target.json": target, "spec.json": spec} {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	return filepath.Join(dir, "spec.json"), filepath.Join(dir, "job")
}

func TestCancelledBeforeSendIsDurableAndNeverPasses(t *testing.T) {
	spec, out := setup(t, "127.0.0.1:1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := durablerun.Start(ctx, spec, out)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != durablerun.Cancelled || got.DeliveryUncertain {
		t.Fatalf("%+v", got)
	}
	reopened, err := durablerun.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.State != durablerun.Cancelled {
		t.Fatalf("%+v", reopened)
	}
	if _, err = durablerun.Start(context.Background(), spec, out); err == nil {
		t.Fatal("reused evidence directory")
	}
}

func peer(t *testing.T, code string) (string, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	received := make(chan struct{})
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(4 * time.Second))
		reader, _ := mllp.NewReader(conn, 1<<20)
		if _, e = reader.ReadFrame(); e != nil {
			return
		}
		close(received)
		if code != "" {
			fmt.Fprintf(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|%s|LISTEN-BOOK\r\x1c\r", code)
		} else {
			io.Copy(io.Discard, conn)
		}
	}()
	return listener.Addr().String(), received
}
func TestCompletedACKVerdictsAndCorruption(t *testing.T) {
	for _, test := range []struct {
		code  string
		state durablerun.State
	}{{"AA", durablerun.Passed}, {"AE", durablerun.AssertionFailed}} {
		t.Run(test.code, func(t *testing.T) {
			address, _ := peer(t, test.code)
			spec, out := setup(t, address)
			got, err := durablerun.Start(context.Background(), spec, out)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != test.state || got.Recorded != 1 || got.DeliveryUncertain {
				t.Fatalf("%+v", got)
			}
			recovered, err := durablerun.Open(out)
			if err != nil || recovered.State != test.state {
				t.Fatalf("%+v %v", recovered, err)
			}
			// A torn final append after completion must never retain a passing status.
			f, err := os.OpenFile(filepath.Join(out, "journal.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			f.WriteString("{\"partial\":")
			f.Close()
			recovered, err = durablerun.Open(out)
			if err != nil || recovered.State != durablerun.DeliveryUncertain || !recovered.JournalIncomplete || !recovered.DeliveryUncertain {
				t.Fatalf("%+v %v", recovered, err)
			}
			os.WriteFile(filepath.Join(out, "intended", "o000001.bin"), []byte("changed"), 0600)
			if _, err = durablerun.Open(out); err == nil {
				t.Fatal("changed intended evidence accepted")
			}
		})
	}
}
func TestCancellationAndTimeoutPreservePossibleDelivery(t *testing.T) {
	for _, cancelRun := range []bool{true, false} {
		t.Run(fmt.Sprint(cancelRun), func(t *testing.T) {
			address, received := peer(t, "")
			spec, out := setup(t, address)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelRun {
				go func() { <-received; cancel() }()
			}
			got, err := durablerun.Start(ctx, spec, out)
			if err != nil {
				t.Fatal(err)
			}
			want := durablerun.TimedOut
			if cancelRun {
				want = durablerun.Cancelled
			}
			if got.State != durablerun.DeliveryUncertain || got.StopReason != want || !got.DeliveryUncertain {
				t.Fatalf("%+v", got)
			}
			recovered, err := durablerun.Open(out)
			if err != nil || recovered.State != got.State || recovered.StopReason != want {
				t.Fatalf("%+v %v", recovered, err)
			}
		})
	}
}
func TestKilledRunnerRecoversWithoutResending(t *testing.T) {
	if os.Getenv("READMIT_DURABLE_CHILD") == "1" {
		_, err := durablerun.Start(context.Background(), os.Getenv("READMIT_DURABLE_SPEC"), os.Getenv("READMIT_DURABLE_OUTPUT"))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	address, received := peer(t, "")
	spec, out := setup(t, address)
	// Give the parent enough time to kill the writer after it records sent bytes.
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(spec), "target.json"))
	if err != nil {
		t.Fatal(err)
	}
	var target replay.Target
	if err = json.Unmarshal(raw, &target); err != nil {
		t.Fatal(err)
	}
	target.MessageTimeout = "1m"
	raw, _ = json.Marshal(target)
	os.WriteFile(filepath.Join(filepath.Dir(spec), "target.json"), raw, 0600)
	cmd := exec.Command(os.Args[0], "-test.run=^TestKilledRunnerRecoversWithoutResending$")
	cmd.Env = append(os.Environ(), "READMIT_DURABLE_CHILD=1", "READMIT_DURABLE_SPEC="+spec, "READMIT_DURABLE_OUTPUT="+out)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill() })
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("no send")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		raw, _ = os.ReadFile(filepath.Join(out, "journal.jsonl"))
		if bytes.Contains(raw, []byte(`"kind":"sent"`)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sent prefix not persisted before ACK")
		}
		time.Sleep(time.Millisecond)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	before, err := os.ReadFile(filepath.Join(out, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, e := durablerun.Open(out)
		if e != nil || got.State != durablerun.DeliveryUncertain || got.StopReason != durablerun.Interrupted || !got.Recovered {
			t.Fatalf("%+v %v", got, e)
		}
	}
	// A killed writer never released its lease, and recovery cannot say it is
	// gone: the lease is held, the send is uncertain, and neither a resume nor
	// a cleanup touches the job.
	recovery, err := durablerun.Recover(out)
	if err != nil || recovery.Terminal || recovery.Lease != durablerun.LeaseHeld || recovery.Uncertain != 1 || recovery.SafeToRepeat {
		t.Fatalf("%+v %v", recovery, err)
	}
	if _, err = durablerun.Resume(context.Background(), out, spec, filepath.Join(filepath.Dir(out), "resumed")); err == nil {
		t.Fatal("resumed an interrupted run")
	}
	if _, err = durablerun.Clean(out); err == nil {
		t.Fatal("cleaned an interrupted run")
	}
	if _, err = os.Lstat(filepath.Join(out, "lease.json")); err != nil {
		t.Fatal("a refused cleanup removed the held lease")
	}
	after, _ := os.ReadFile(filepath.Join(out, "journal.jsonl"))
	if !bytes.Equal(before, after) {
		t.Fatal("recovery changed evidence")
	}
	retained, err := os.ReadFile(filepath.Join(out, "sent", "o000001.bin"))
	if err != nil || len(retained) == 0 {
		t.Fatalf("missing sent evidence: %v", err)
	}
	if _, err = durablerun.Start(context.Background(), spec, out); err == nil {
		t.Fatal("automatically reused interrupted run")
	}
}

func TestCLIAndDesktopExposeTheSameLifecycle(t *testing.T) {
	address, _ := peer(t, "AA")
	spec, out := setup(t, address)
	var stdout, stderr bytes.Buffer
	err := cli.Execute("test", []string{"--operation-policy", testlicense.New(t), "run", "start", spec, "--send", "--output", out, "--json"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("%v %s", err, stderr.String())
	}
	var summary durablerun.Summary
	if err = json.Unmarshal(stdout.Bytes(), &summary, json.RejectUnknownMembers(true)); err != nil || summary.State != durablerun.Passed {
		t.Fatalf("%s %v", stdout.String(), err)
	}
	app := desktop.New(nil, "", "", "")
	recovered := app.OpenDurableRun(out)
	if recovered.State != desktop.Completed || recovered.Run == nil || recovered.Run.State != durablerun.Passed {
		t.Fatalf("%+v", recovered)
	}
	if selected := app.SelectOperationPolicy(testlicense.New(t)); selected.State != desktop.Completed {
		t.Fatal(selected)
	}
	address, received := peer(t, "")
	spec, out = setup(t, address)
	done := make(chan desktop.DurableRunResult, 1)
	go func() { done <- app.StartDurableRun(spec, out) }()
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("no desktop send")
	}
	if got := app.OpenDurableRun(out); got.State != desktop.Busy {
		t.Fatalf("overlapping operation %+v", got)
	}
	app.Cancel()
	select {
	case got := <-done:
		if got.State != desktop.Completed || got.Run == nil || got.Run.StopReason != durablerun.Cancelled || !got.Run.DeliveryUncertain {
			t.Fatalf("%+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not complete")
	}
	stdout.Reset()
	stderr.Reset()
	err = cli.Execute("test", []string{"run", "status", out, "--json"}, &stdout, &stderr)
	if cli.ExitCode(err) != 2 || stderr.Len() != 0 {
		t.Fatalf("%v %s", err, stderr.String())
	}
	if err = json.Unmarshal(stdout.Bytes(), &summary); err != nil || summary.StopReason != durablerun.Cancelled {
		t.Fatalf("%s %v", stdout.String(), err)
	}
}

type failingObserver struct {
	fail  string
	calls []string
}

func (o *failingObserver) check(kind string) error {
	o.calls = append(o.calls, kind)
	if o.fail == kind {
		return errors.New("storage unavailable")
	}
	return nil
}
func (o *failingObserver) BeforeSend(string) error     { return o.check("intent") }
func (o *failingObserver) Sent(string, []byte) error   { return o.check("sent") }
func (o *failingObserver) Recorded(replay.Event) error { return o.check("recorded") }
func TestObserverPersistenceFailureStopsExecution(t *testing.T) {
	for _, kind := range []string{"intent", "sent", "recorded"} {
		t.Run(kind, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			spec, out := setup(t, listener.Addr().String())
			count := make(chan int, 1)
			go func() {
				conn, e := listener.Accept()
				if e != nil {
					count <- 0
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				r, _ := mllp.NewReader(conn, 1<<20)
				n := 0
				for {
					_, e = r.ReadFrame()
					if e != nil {
						break
					}
					n++
					fmt.Fprint(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|AA|LISTEN-BOOK\r\x1c\r")
				}
				count <- n
			}()
			plan, err := testrunner.Prepare(spec)
			if err != nil {
				t.Fatal(err)
			}
			observer := &failingObserver{fail: kind}
			result, err := testrunner.ExecuteObserved(context.Background(), plan, out, observer)
			if err == nil || result != nil {
				t.Fatal("storage failure certified a completed result")
			}
			if observer.calls[len(observer.calls)-1] != kind {
				t.Fatalf("continued after failed observer: %v", observer.calls)
			}
			want := 1
			if kind == "intent" {
				want = 0
			}
			select {
			case got := <-count:
				if got != want {
					t.Fatalf("sent %d messages, want %d", got, want)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("connection not closed")
			}
		})
	}
}

func TestRecoveryResolvesPhysicalRootBeforeResult(t *testing.T) {
	address, _ := peer(t, "AA")
	spec, out := setup(t, address)
	if _, err := durablerun.Start(context.Background(), spec, out); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(filepath.Dir(out), "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(nested, alias); err != nil {
		t.Skip("symlink unavailable")
	}
	got, err := durablerun.Open(alias + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "job")
	if err != nil || got.State != durablerun.Passed {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestConnectionRefusalIsExecutionErrorWithoutDelivery(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	spec, out := setup(t, address)
	got, err := durablerun.Start(context.Background(), spec, out)
	if err != nil || got.State != durablerun.ExecutionError || got.DeliveryUncertain {
		t.Fatalf("%+v %v", got, err)
	}
	recovered, err := durablerun.Open(out)
	if err != nil || recovered.State != durablerun.ExecutionError || recovered.DeliveryUncertain {
		t.Fatalf("%+v %v", recovered, err)
	}
}
