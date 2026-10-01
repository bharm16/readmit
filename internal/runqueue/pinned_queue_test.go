package runqueue_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/testrunner"
)

type queueWitness struct {
	sync.Mutex
	sent []string
	ack  string
}

func writeQueueDocument(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func realPinnedQueue(t *testing.T) (string, runqueue.Plan, *queueWitness) {
	t.Helper()
	root := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	witness := &queueWitness{}
	done := make(chan struct{})
	t.Cleanup(func() { listener.Close(); <-done })
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				reader, err := mllp.NewReader(conn, 65536)
				if err != nil {
					return
				}
				raw, err := reader.ReadFrame()
				if err != nil {
					return
				}
				control := "A"
				if strings.Contains(string(raw), "|B|P|") {
					control = "B"
				}
				witness.Lock()
				witness.sent = append(witness.sent, control)
				ack := witness.ack
				witness.Unlock()
				if ack == "drop" {
					return
				}
				if ack == "" {
					ack = "AA"
				}
				fmt.Fprintf(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|%s|%s\r\x1c\r", ack, control)
			}()
		}
	}()
	writeQueueDocument(t, filepath.Join(root, "target.json"), replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: listener.Addr().String(), Transport: "plain", ConnectTimeout: "5s", MessageTimeout: "5s", MaxACKBytes: 4096})
	for _, id := range []string{"a", "b"} {
		control := strings.ToUpper(id)
		raw := []byte("MSH|^~\\&|SENDER|LAB|ENGINE|LAB|20260101000000||SIU^S12|" + control + "|P|2.5.1\rSCH|CASE-" + control + "\r")
		_, err := bundle.Write(filepath.Join(root, "case-"+id), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}})
		if err != nil {
			t.Fatal(err)
		}
		accepted := "AA"
		writeQueueDocument(t, filepath.Join(root, id+".json"), testrunner.Spec{Schema: testrunner.SpecSchema, Name: id, Input: testrunner.Input{Case: "case-" + id, Messages: []string{"s0001-e000001"}}, Target: "target.json", Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset fixture"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "accepted", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &accepted}}}}})
	}
	return filepath.Join(root, "queue.json"), runqueue.Plan{Schema: runqueue.PlanSchema, Parallelism: 1, Jobs: []runqueue.Job{{ID: "a", Spec: "a.json", Isolation: runqueue.SharedState, After: []string{"b"}}, {ID: "b", Spec: "b.json", Isolation: runqueue.SharedState}}}, witness
}

func executePinnedLocally(ctx context.Context, job runqueue.PinnedJob, id string) (durablerun.Summary, error) {
	prepared, err := durablerun.Prepare(job.Spec)
	if err != nil {
		return durablerun.Summary{}, err
	}
	identity, err := prepared.InputIdentity()
	if err != nil || identity != job.Input {
		return durablerun.Summary{}, errors.New("prepared inputs changed")
	}
	return prepared.Start(ctx, filepath.Join(filepath.Dir(job.Spec), "runs", id))
}

func TestPreparedQueueDispatchOwnsOrderAndKeepsEverySkippedJob(t *testing.T) {
	for _, ack := range []string{"AA", "AE", "drop"} {
		t.Run(ack, func(t *testing.T) {
			path, plan, witness := realPinnedQueue(t)
			witness.ack = ack
			writeQueueDocument(t, path, plan)
			prepared, err := runqueue.PrepareQueue(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			// Mutating the source queue or its returned projection cannot
			// rewrite the already prepared execution.
			plan.Jobs[0].After = nil
			writeQueueDocument(t, path, plan)
			jobs := prepared.Jobs()
			jobs[0].After[0] = "a"
			runs := filepath.Join(filepath.Dir(path), "runs")
			if err := os.Mkdir(runs, 0700); err != nil {
				t.Fatal(err)
			}
			report, err := prepared.Dispatch(t.Context(), runs, "occurrence", executePinnedLocally)
			if err != nil || len(report.Jobs) != 2 {
				t.Fatal("dispatch lost declared jobs", err)
			}
			witness.Lock()
			sent := strings.Join(witness.sent, ",")
			witness.Unlock()
			if ack == "AA" {
				if sent != "B,A" || report.ExitCode() != 0 || report.Executed != 2 {
					t.Fatal("owned queue order or complete pass changed", sent, report)
				}
			} else if sent != "B" || report.Jobs[0].Admission != runqueue.Skipped || report.Skipped != 1 || report.ExitCode() != 2 {
				t.Fatal("a failed/uncertain dependency became a complete passing queue", sent, report)
			}
		})
	}
}

func TestPreparedQueueIdentityIncludesDependenciesIsolationAndParallelism(t *testing.T) {
	path, plan, witness := realPinnedQueue(t)
	writeQueueDocument(t, path, plan)
	original, _, err := runqueue.PinnedJobs(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"dependencies", "isolation", "parallelism"} {
		t.Run(change, func(t *testing.T) {
			changed := plan
			changed.Jobs = append([]runqueue.Job(nil), plan.Jobs...)
			switch change {
			case "dependencies":
				changed.Jobs[0].After = nil
			case "isolation":
				changed.Jobs[0].Isolation = runqueue.IsolatedState
			case "parallelism":
				changed.Parallelism = 2
			}
			writeQueueDocument(t, path, changed)
			identity, _, err := runqueue.PinnedJobs(t.Context(), path)
			if err != nil || identity == original {
				t.Fatal("changed execution configuration retained its approval pin", err)
			}
		})
	}
	witness.Lock()
	defer witness.Unlock()
	if len(witness.sent) != 0 {
		t.Fatal("queue preparation sent stimulus")
	}
}
