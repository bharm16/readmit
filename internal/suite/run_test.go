package suite_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
)

func peer(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				handle(conn)
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-done; wg.Wait() })
	return listener.Addr().String()
}
func ack(conn net.Conn, code string) {
	reader, _ := mllp.NewReader(conn, 1<<20)
	if _, err := reader.ReadFrame(); err != nil {
		return
	}
	_, _ = fmt.Fprintf(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|%s|LISTEN-BOOK\r\x1c\r", code)
}
func TestPublicSuiteRunsSameExpectationsAcrossTwoEnvironments(t *testing.T) {
	first := peer(t, func(conn net.Conn) { ack(conn, "AA") })
	second := peer(t, func(conn net.Conn) { ack(conn, "AA") })
	dir, doc := fixture(t, first)
	targetRaw, _ := os.ReadFile(filepath.Join(dir, "east.json"))
	var target map[string]any
	_ = json.Unmarshal(targetRaw, &target)
	target["address"] = second
	write(t, filepath.Join(dir, "west.json"), target)
	doc.Environments = append(doc.Environments, suite.Environment{ID: "west", Site: "hospital-b", Bindings: []suite.Binding{{Parameter: "interface", Target: "west.json"}}})
	doc.Tables[0].Rows = append(doc.Tables[0].Rows, suite.Row{ID: "two", Case: "case-one"})
	setup := doc.Tests[0]
	setup.ID = "setup"
	doc.Tests = append([]suite.Test{setup}, doc.Tests...)
	doc.Tests[1].After = []string{"setup"}
	write(t, filepath.Join(dir, "suite.json"), doc)
	original, _ := os.ReadFile(filepath.Join(dir, "suite.json"))
	for _, env := range []string{"east", "west"} {
		var stdout, stderr bytes.Buffer
		out := filepath.Join(dir, env)
		err := licensedCLI(t, []string{"suite", "run", filepath.Join(dir, "suite.json"), "--environment", env, "--output", out, "--send", "--json"}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("%v %s", err, stderr.String())
		}
		var report runqueue.Report
		if err = json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if report.Executed != 4 || report.ExitCode() != 0 {
			t.Fatalf("%+v", report)
		}
		for _, job := range report.Jobs {
			recovered, e := durablerun.Recover(filepath.Join(out, "runs", job.ID))
			if e != nil || !recovered.Terminal || recovered.Acknowledged != 1 || recovered.SafeToRepeat {
				t.Fatalf("%+v %v", recovered, e)
			}
		}
		generated, e := testrunner.ReadSpec(filepath.Join(out, "booking-two.json"))
		if e != nil || *generated.Assertions[0].Expected.Field.Text != "AA" {
			t.Fatalf("%+v %v", generated, e)
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, "suite.json"))
	if !bytes.Equal(original, after) {
		t.Fatal("suite changed across environments")
	}
}
func TestFailedSetupSkipsDependentAndNeverPassesSuite(t *testing.T) {
	dir, doc := fixture(t, peer(t, func(conn net.Conn) { ack(conn, "AE") }))
	setup := doc.Tests[0]
	setup.ID = "setup"
	doc.Tests = append([]suite.Test{setup}, doc.Tests...)
	doc.Tests[1].After = []string{"setup"}
	write(t, filepath.Join(dir, "suite.json"), doc)
	out := filepath.Join(dir, "failed")
	report, err := suite.Run(t.Context(), suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: out})
	if err != nil || report.ExitCode() != 2 || report.Executed != 1 || report.Skipped != 1 || report.Jobs[0].Run.State != durablerun.AssertionFailed {
		t.Fatalf("%+v %v", report, err)
	}
	if _, err = os.Stat(filepath.Join(out, "runs", "booking-one")); !os.IsNotExist(err) {
		t.Fatal("dependent sent after failed setup")
	}
}
func TestSuiteCancellationPreservesUncertainDeliveryAndRefusesResume(t *testing.T) {
	received := make(chan struct{})
	dir, doc := fixture(t, peer(t, func(conn net.Conn) {
		reader, _ := mllp.NewReader(conn, 1<<20)
		if _, err := reader.ReadFrame(); err == nil {
			close(received)
			_, _ = io.Copy(io.Discard, conn)
		}
	}))
	setup := doc.Tests[0]
	setup.ID = "setup"
	doc.Tests = append([]suite.Test{setup}, doc.Tests...)
	doc.Tests[1].After = []string{"setup"}
	write(t, filepath.Join(dir, "suite.json"), doc)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	acknowledged := make(chan time.Time, 1)
	go func() {
		select {
		case <-received:
			acknowledged <- time.Now()
			cancel()
		case <-ctx.Done():
		}
	}()
	out := filepath.Join(dir, "cancelled")
	report, err := suite.Run(ctx, suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: out})
	if err != nil || report.Skipped != 1 || report.Jobs[0].Run == nil || report.Jobs[0].Run.State != durablerun.DeliveryUncertain || report.Jobs[0].Run.StopReason != durablerun.Cancelled {
		t.Fatalf("%+v %v", report, err)
	}
	t.Logf("cancel request to durable suite return: %.3f ms", float64(time.Since(<-acknowledged).Nanoseconds())/1e6)
	recovered, err := durablerun.Recover(filepath.Join(out, "runs", "setup-one"))
	if err != nil || recovered.Uncertain != 1 || recovered.SafeToRepeat {
		t.Fatalf("%+v %v", recovered, err)
	}
	var stdout, stderr bytes.Buffer
	err = licensedCLI(t, []string{"run", "resume", filepath.Join(out, "runs", "setup-one"), filepath.Join(out, "setup-one.json"), "--send", "--output", filepath.Join(dir, "again")}, &stdout, &stderr)
	if err == nil {
		t.Fatal("uncertain send repeated")
	}
	if _, err = os.Stat(filepath.Join(dir, "again")); !os.IsNotExist(err) {
		t.Fatal("resume refusal wrote evidence")
	}
}

func TestSuiteUsesActualQueueStateIsolation(t *testing.T) {
	for _, isolated := range []bool{false, true} {
		name := "shared"
		if isolated {
			name = "isolated"
		}
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			active, peak, arrivals := 0, 0, 0
			allArrived := make(chan struct{})
			address := peer(t, func(conn net.Conn) {
				reader, _ := mllp.NewReader(conn, 1<<20)
				if _, err := reader.ReadFrame(); err != nil {
					return
				}
				mu.Lock()
				active++
				arrivals++
				if active > peak {
					peak = active
				}
				if arrivals == 8 {
					close(allArrived)
				}
				mu.Unlock()
				if isolated {
					select {
					case <-allArrived:
					case <-time.After(5 * time.Second):
					}
				} else {
					time.Sleep(5 * time.Millisecond)
				}
				mu.Lock()
				active--
				mu.Unlock()
				_, _ = fmt.Fprint(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|AA|LISTEN-BOOK\r\x1c\r")
			})
			dir, doc := fixture(t, address)
			doc.Parallelism = 8
			for i := 2; i <= 8; i++ {
				doc.Tables[0].Rows = append(doc.Tables[0].Rows, suite.Row{ID: fmt.Sprintf("row%d", i), Case: "case-one"})
			}
			if isolated {
				doc.Tests[0].Isolation = runqueue.IsolatedState
			}
			write(t, filepath.Join(dir, "suite.json"), doc)
			report, err := suite.Run(t.Context(), suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: filepath.Join(dir, "out")})
			if err != nil || report.ExitCode() != 0 {
				t.Fatalf("%+v %v", report, err)
			}
			mu.Lock()
			observed := peak
			mu.Unlock()
			want := 1
			if isolated {
				want = 8
			}
			if observed != want {
				t.Fatalf("target observed concurrency %d, wanted %d", observed, want)
			}
			if !isolated && report.Jobs[1].WaitedFor == "" {
				t.Fatal("shared-state serialization was not reported")
			}
		})
	}
}

func TestSuiteCancelledBeforeStartReportsEveryRowSkipped(t *testing.T) {
	dir, _ := fixture(t, "127.0.0.1:1")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	report, err := suite.Run(ctx, suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: filepath.Join(dir, "out")})
	if err != nil || report.Executed != 0 || report.Skipped != 1 || report.ExitCode() != 2 {
		t.Fatalf("%+v %v", report, err)
	}
}

func TestPublicSuiteRequiresSendAndPrepareNeverSends(t *testing.T) {
	dir, _ := fixture(t, "127.0.0.1:1")
	var stdout, stderr bytes.Buffer
	out := filepath.Join(dir, "out")
	args := []string{"suite", "run", filepath.Join(dir, "suite.json"), "--environment", "east", "--output", out}
	if err := licensedCLI(t, args, &stdout, &stderr); err == nil {
		t.Fatal("run authorized implicitly")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("missing authorization wrote output")
	}
	args[1] = "prepare"
	args = append(args, "--json")
	stdout.Reset()
	stderr.Reset()
	if err := licensedCLI(t, args, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var queue runqueue.Plan
	if err := json.Unmarshal(stdout.Bytes(), &queue); err != nil || len(queue.Jobs) != 1 {
		t.Fatalf("%+v %v", queue, err)
	}
	children, err := os.ReadDir(filepath.Join(out, "runs"))
	if err != nil || len(children) != 0 {
		t.Fatal("prepare executed a run")
	}
	raw, err := os.ReadFile(filepath.Join(out, "selection.json"))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := suite.DecodeSelection(raw)
	if err != nil || selection.Environment != "east" || selection.Site != "hospital-a" {
		t.Fatalf("%+v %v", selection, err)
	}
}

func TestSuiteCrashHelper(t *testing.T) {
	path := os.Getenv("READMIT_SUITE_CRASH_FIXTURE")
	if path == "" {
		return
	}
	_, _ = suite.Run(context.Background(), suite.Request{Path: filepath.Join(path, "suite.json"), Environment: "east", Output: filepath.Join(path, "crashed")})
}

func TestSuiteProcessCrashRetainsUncertainJobWithoutStartingDependent(t *testing.T) {
	received := make(chan struct{})
	dir, doc := fixture(t, peer(t, func(conn net.Conn) {
		reader, _ := mllp.NewReader(conn, 1<<20)
		if _, err := reader.ReadFrame(); err == nil {
			close(received)
			_, _ = io.Copy(io.Discard, conn)
		}
	}))
	setup := doc.Tests[0]
	setup.ID = "setup"
	doc.Tests = append([]suite.Test{setup}, doc.Tests...)
	doc.Tests[1].After = []string{"setup"}
	write(t, filepath.Join(dir, "suite.json"), doc)
	cmd := exec.Command(os.Args[0], "-test.run=^TestSuiteCrashHelper$")
	cmd.Env = append(os.Environ(), "READMIT_SUITE_CRASH_FIXTURE="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	select {
	case <-received:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("child never sent")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	out := filepath.Join(dir, "crashed")
	recovered, err := durablerun.Recover(filepath.Join(out, "runs", "setup-one"))
	if err != nil || recovered.Terminal || recovered.Uncertain != 1 || recovered.SafeToRepeat {
		t.Fatalf("%+v %v", recovered, err)
	}
	if _, err = os.Stat(filepath.Join(out, "runs", "booking-one")); !os.IsNotExist(err) {
		t.Fatal("dependent executed after crash")
	}
	if _, err = os.Stat(filepath.Join(out, "report.json")); !os.IsNotExist(err) {
		t.Fatal("crash fabricated a final report")
	}
	if _, err = suite.Run(t.Context(), suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: out}); err == nil {
		t.Fatal("crashed output restarted")
	}
}

// A blackholed ACK after an observed send models loss of the return path. The
// receiver may have committed; expiry must not turn that uncertainty into pass
// or permission to resend. This does not claim a physical firewall lab.
func TestSuiteNetworkBlackholeRetainsUncertaintyAndRecovers(t *testing.T) {
	received := make(chan struct{})
	dir, _ := fixture(t, peer(t, func(conn net.Conn) {
		reader, _ := mllp.NewReader(conn, 1<<20)
		if _, err := reader.ReadFrame(); err == nil {
			close(received)
			_, _ = io.Copy(io.Discard, conn)
		}
	}))
	out := filepath.Join(dir, "blackhole")
	started := time.Now()
	report, err := suite.Run(t.Context(), suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: out})
	select {
	case <-received:
	default:
		t.Fatal("blackhole never received the send")
	}
	if err != nil || report.ExitCode() == 0 || len(report.Jobs) != 1 || report.Jobs[0].Run == nil || report.Jobs[0].Run.State != durablerun.DeliveryUncertain || report.Jobs[0].Run.StopReason != durablerun.TimedOut {
		t.Fatalf("%+v %v", report, err)
	}
	recovered, err := durablerun.Recover(filepath.Join(out, "runs", "booking-one"))
	if err != nil || recovered.Uncertain != 1 || recovered.SafeToRepeat {
		t.Fatalf("%+v %v", recovered, err)
	}
	var stdout, stderr bytes.Buffer
	err = licensedCLI(t, []string{"run", "resume", filepath.Join(out, "runs", "booking-one"), filepath.Join(out, "booking-one.json"), "--send", "--output", filepath.Join(dir, "again")}, &stdout, &stderr)
	if err == nil {
		t.Fatal("blackholed send was repeated")
	}
	if _, err = os.Stat(filepath.Join(dir, "again")); !os.IsNotExist(err) {
		t.Fatal("resume refusal wrote evidence")
	}
	t.Logf("return-path blackhole to retained uncertainty and recovery: %.3f ms", float64(time.Since(started).Nanoseconds())/1e6)
}
