package tests

import (
	"encoding/json/v2"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/runqueue"
)

// queuePeer answers each frame with an accept and reports how many senders it
// ever had at once. It is the evidence behind a serialization claim: the queue
// says what it decided, and the receiver says what reached it. When pair is
// set it answers nothing until two senders are present, so a queue that
// serialized jobs declared isolated fails rather than passes slowly.
func queuePeer(t *testing.T, pair bool) (string, func() int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	var mutex sync.Mutex
	inside, peak := 0, 0
	both := make(chan struct{})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(20 * time.Second))
				reader, _ := mllp.NewReader(conn, 1<<20)
				if _, err := reader.ReadFrame(); err != nil {
					return
				}
				mutex.Lock()
				inside++
				if inside > peak {
					peak = inside
				}
				arrived := inside
				mutex.Unlock()
				if pair {
					if arrived == 2 {
						close(both)
					}
					select {
					case <-both:
					case <-time.After(15 * time.Second):
					}
				} else {
					time.Sleep(200 * time.Millisecond)
				}
				fmt.Fprint(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|AA|LISTEN-BOOK\r\x1c\r")
				mutex.Lock()
				inside--
				mutex.Unlock()
			}()
		}
	}()
	return listener.Addr().String(), func() int {
		mutex.Lock()
		defer mutex.Unlock()
		return peak
	}
}

// queueDocument writes a queue beside the spec durableSpec produced, so every
// job names one test spec inside the queue document's own directory.
func queueDocument(t *testing.T, directory string, parallelism int, jobs ...runqueue.Job) string {
	t.Helper()
	raw, err := json.Marshal(runqueue.Plan{Schema: runqueue.PlanSchema, Parallelism: parallelism, Jobs: jobs}, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "queue.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runsDirectory(t *testing.T, parent string) string {
	t.Helper()
	runs := filepath.Join(parent, "runs")
	if err := os.Mkdir(runs, 0700); err != nil {
		t.Fatal(err)
	}
	return runs
}

func decodeQueue(t *testing.T, stdout string) runqueue.Report {
	t.Helper()
	report := readStrictOutput[runqueue.Report](t, stdout)
	if report.Schema != runqueue.ReportSchema {
		t.Fatalf("%+v", report)
	}
	return report
}

// Two jobs that declare they share the target's state never reach it at once,
// even given a queue with room to run both, and each keeps its own durable run.
func TestRunExecutableQueueSerializesJobsSharingTargetState(t *testing.T) {
	address, peak := queuePeer(t, false)
	spec, dir := durableSpec(t, address)
	runs := runsDirectory(t, dir)
	plan := queueDocument(t, filepath.Dir(spec), 4,
		runqueue.Job{ID: "first", Spec: filepath.Base(spec), Isolation: runqueue.SharedState},
		runqueue.Job{ID: "second", Spec: filepath.Base(spec), Isolation: runqueue.SharedState})
	stdout, stderr, err := run(t, "run", "queue", plan, "--send", "--runs", runs, "--json")
	if err != nil || stderr != "" {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	report := decodeQueue(t, stdout)
	if report.Executed != 2 || report.Refused != 0 || report.Skipped != 0 || report.Parallelism != 4 {
		t.Fatalf("%+v", report)
	}
	for _, job := range report.Jobs {
		if job.Admission != runqueue.Executed || job.Run == nil || job.Run.State != durablerun.Passed {
			t.Fatalf("%+v", job)
		}
		if len(job.Resources) != 1 || job.Resources[0].Kind != durablerun.EndpointResource || job.Resources[0].Name != address {
			t.Fatalf("%+v", job.Resources)
		}
	}
	if got := peak(); got != 1 {
		t.Fatalf("%d shared jobs reached the endpoint at once", got)
	}
	if report.Jobs[1].WaitedFor != durablerun.EndpointResource+" "+address {
		t.Fatalf("the queue did not report what the second job waited for: %+v", report.Jobs)
	}
	// Each job kept its own durable run, readable by the ordinary recovery.
	for _, job := range []string{"first", "second"} {
		stdout, stderr, err := run(t, "run", "status", filepath.Join(runs, job), "--recovery", "--json")
		if err != nil || stderr != "" {
			t.Fatalf("%s: %v %s %s", job, err, stdout, stderr)
		}
		recovery := readStrictOutput[durablerun.Recovery](t, stdout)
		if !recovery.Terminal || recovery.Acknowledged != 1 || recovery.Lease != durablerun.LeaseReleased {
			t.Fatalf("%s: %+v", job, recovery)
		}
	}
}

// Declared state isolation is what permits two jobs on one environment to run
// together. The receiver answers nothing until both are present, so a queue
// that serialized them would fail rather than pass.
func TestRunExecutableQueueRunsExplicitlyIsolatedJobsTogether(t *testing.T) {
	address, peak := queuePeer(t, true)
	spec, dir := durableSpec(t, address)
	runs := runsDirectory(t, dir)
	plan := queueDocument(t, filepath.Dir(spec), 2,
		runqueue.Job{ID: "first", Spec: filepath.Base(spec), Isolation: runqueue.IsolatedState},
		runqueue.Job{ID: "second", Spec: filepath.Base(spec), Isolation: runqueue.IsolatedState})
	stdout, stderr, err := run(t, "run", "queue", plan, "--send", "--runs", runs, "--json")
	if err != nil || stderr != "" {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	report := decodeQueue(t, stdout)
	if report.Executed != 2 || report.ExitCode() != 0 {
		t.Fatalf("%+v", report)
	}
	if got := peak(); got != 2 {
		t.Fatalf("%d isolated jobs reached the endpoint at once", got)
	}
}

// A durable run outside the queue that still holds a lease refuses admission to
// a job declaring the same resource. Nothing executes for it, and run clean on
// the stale lease is what releases the resource again.
func TestRunExecutableQueueRefusesAdmissionAgainstAHeldLease(t *testing.T) {
	address, _ := queuePeer(t, false)
	spec, dir := durableSpec(t, address)
	runs := runsDirectory(t, dir)
	manual := filepath.Join(runs, "manual")
	if err := os.Mkdir(manual, 0700); err != nil {
		t.Fatal(err)
	}
	lease := durablerun.Lease{Schema: durablerun.LeaseSchema, Holder: durablerun.Holder{PID: 1, StartedAt: time.Now().UTC()},
		Resources: []durablerun.Resource{{Kind: durablerun.EndpointResource, Name: address}}}
	raw, err := json.Marshal(lease, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manual, "lease.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	plan := queueDocument(t, filepath.Dir(spec), 1,
		runqueue.Job{ID: "first", Spec: filepath.Base(spec), Isolation: runqueue.SharedState},
		runqueue.Job{ID: "second", Spec: filepath.Base(spec), Isolation: runqueue.SharedState, After: []string{"first"}})
	stdout, stderr, err := run(t, "run", "queue", plan, "--send", "--runs", runs)
	if exitCode(t, err) != exitRefused || stderr != "" {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Jobs: 0 executed, 0 not started, 1 refused, 1 skipped\n") ||
		!strings.Contains(stdout, "first: refused: another durable run in this runs directory still holds endpoint "+address) ||
		!strings.Contains(stdout, "second: skipped: a job this one depends on did not pass") {
		t.Fatalf("%s", stdout)
	}
	for _, job := range []string{"first", "second"} {
		if _, err := os.Lstat(filepath.Join(runs, job)); err == nil {
			t.Fatalf("a refused job wrote %s", job)
		}
	}
	for _, private := range []string{dir, "LISTEN-BOOK", "SYNTH-001"} {
		if strings.Contains(stdout+stderr, private) {
			t.Fatal("the queue console disclosed private paths or values")
		}
	}
}

// Every refusal the queue promises is refused before anything executes.
func TestRunExecutableQueueRefusesWhatItCannotSchedule(t *testing.T) {
	address, _ := queuePeer(t, false)
	spec, dir := durableSpec(t, address)
	runs := runsDirectory(t, dir)
	plan := queueDocument(t, filepath.Dir(spec), 1, runqueue.Job{ID: "first", Spec: filepath.Base(spec), Isolation: runqueue.SharedState})
	unreviewed := filepath.Join(dir, "unreviewed.json")
	if err := os.WriteFile(unreviewed, []byte(`{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"first","spec":"spec.json"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for name, arguments := range map[string][]string{
		"no send":           {"run", "queue", plan, "--runs", runs},
		"no runs":           {"run", "queue", plan, "--send"},
		"absent runs":       {"run", "queue", plan, "--send", "--runs", filepath.Join(dir, "absent")},
		"no isolation":      {"run", "queue", unreviewed, "--send", "--runs", runs},
		"absent queue":      {"run", "queue", filepath.Join(dir, "absent.json"), "--send", "--runs", runs},
		"negative deadline": {"run", "queue", plan, "--send", "--runs", runs, "--deadline", "0s"},
	} {
		stdout, stderr, err := run(t, arguments...)
		if exitCode(t, err) != exitRefused || stdout != "" || stderr == "" {
			t.Fatalf("%s: %v %s %s", name, err, stdout, stderr)
		}
		if _, err := os.Lstat(filepath.Join(runs, "first")); err == nil {
			t.Fatalf("%s executed a refused queue", name)
		}
	}
}

// A deadline stops the whole queue. The run in flight records its own stop and
// its own uncertainty exactly as a single run does, and the job that never
// started is reported as never started rather than as a run that did nothing.
func TestRunExecutableQueueDeadlineStartsNothingFurther(t *testing.T) {
	spec, dir := durableSpec(t, durablePeer(t, ""))
	runs := runsDirectory(t, dir)
	plan := queueDocument(t, filepath.Dir(spec), 1,
		runqueue.Job{ID: "first", Spec: filepath.Base(spec), Isolation: runqueue.SharedState},
		runqueue.Job{ID: "second", Spec: filepath.Base(spec), Isolation: runqueue.SharedState})
	stdout, stderr, err := run(t, "run", "queue", plan, "--send", "--runs", runs, "--deadline", "500ms", "--json")
	if exitCode(t, err) != exitRefused || stderr != "" {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	report := decodeQueue(t, stdout)
	if report.Executed != 1 || report.Skipped != 1 || report.Refused != 0 {
		t.Fatalf("%+v", report)
	}
	first := report.Jobs[0]
	if first.Admission != runqueue.Executed || first.Run == nil || first.Run.State != durablerun.DeliveryUncertain || first.Run.StopReason != durablerun.TimedOut || !first.Run.DeliveryUncertain {
		t.Fatalf("%+v", first)
	}
	second := report.Jobs[1]
	if second.Admission != runqueue.Skipped || second.Run != nil || second.Reason != "the queue stopped before this job started" {
		t.Fatalf("%+v", second)
	}
	if _, err := os.Lstat(filepath.Join(runs, "second")); err == nil {
		t.Fatal("a job the queue never started wrote a run directory")
	}
	// The run that did start is recovered as the uncertain delivery it is, and
	// nothing in the queue resent or resumed it.
	stdout, stderr, err = run(t, "run", "status", filepath.Join(runs, "first"), "--recovery", "--json")
	if exitCode(t, err) != exitRefused || stderr != "" {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	recovery := readStrictOutput[durablerun.Recovery](t, stdout)
	if !recovery.Terminal || recovery.Uncertain != 1 || recovery.SafeToRepeat || recovery.Lease != durablerun.LeaseReleased {
		t.Fatalf("%+v", recovery)
	}
}
