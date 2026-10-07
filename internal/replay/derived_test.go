package replay_test

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

type derivedObserver struct {
	after   func()
	records int
}

func (o *derivedObserver) BeforeSend(string) error   { return nil }
func (o *derivedObserver) Sent(string, []byte) error { return nil }
func (o *derivedObserver) Recorded(replay.Event) error {
	o.records++
	if o.records == 1 && o.after != nil {
		o.after()
	}
	return nil
}

func TestDerivedSequenceSendsFrozenBytesAndRechecksOriginalBeforeAnotherWrite(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		t.Run(strconv.FormatBool(tamper), func(t *testing.T) {
			sourcePath := caseAt(t, mllp.Frame(request("ORIGINAL")))
			source, err := bundle.Open(sourcePath)
			if err != nil {
				t.Fatal(err)
			}
			edits, err := reproducer.NewPlan(source.Identity)
			if err != nil {
				t.Fatal(err)
			}
			for _, step := range []reproducer.Step{{Operator: reproducer.SelectOccurrence, Occurrence: source.Events[0].ID}, {Operator: reproducer.SetField, Occurrence: source.Events[0].ID, Selector: "MSH-10", Value: "runtime-one"}} {
				edits, err = reproducer.Append(edits, step)
				if err != nil {
					t.Fatal(err)
				}
			}
			derived, err := reproducer.Prepare(sourcePath, edits)
			if err != nil {
				t.Fatal(err)
			}
			received := new(atomic.Int32)
			address := peer(t, func(conn net.Conn) {
				reader, _ := mllp.NewReader(conn, 4096)
				for {
					frame, err := reader.ReadFrame()
					if err != nil {
						return
					}
					if !bytes.Equal(frame, mllp.Frame(request("runtime-one"))) {
						t.Errorf("sender did not send independently expected derived frame: %q", frame)
						return
					}
					received.Add(1)
					if _, err := conn.Write(ack("AA", "runtime-one")); err != nil {
						return
					}
				}
			})
			receiver := target(address)
			receiver.MessageTimeout = "2s"
			sequence := replay.Options{Occurrences: []string{source.Events[0].ID, source.Events[0].ID}}
			prepared, err := replay.PrepareScopedDerivedSequence(sourcePath, receiver, sequence, derived)
			if err != nil {
				t.Fatal(err)
			}
			if prepared.SourceIdentity() != derived.Identity() || prepared.SourceIdentity() == source.Identity {
				t.Fatal("run source must be the actual derived case")
			}
			detached, err := reproducer.PrepareFiles(derived.OriginalFiles(), edits)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := replay.PrepareScopedDerivedSequence(sourcePath, receiver, sequence, detached); err == nil {
				t.Fatal("offline artifact substituted for live source")
			}
			_, port, _ := net.SplitHostPort(address)
			number, _ := strconv.Atoi(port)
			policy := sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "qa", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "receiver", Operation: sendpolicy.V2Stimulus, Port: number, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"}}}
			route, err := destination.AdmitScoped(t.Context(), destination.ScopedRequest{Policy: policy, Request: sendpolicy.ScopedRequest{Project: "lab", Environment: "qa", Endpoint: "receiver", Operation: sendpolicy.V2Stimulus, Classification: "nonproduction", Address: address}, Budget: 5 * time.Second, Authorize: func(context.Context) error { return derived.VerifyUnchanged() }, Record: func(sendpolicy.ScopedDecision) error { return nil }})
			if err != nil {
				t.Fatal(err)
			}
			observer := &derivedObserver{}
			if tamper {
				observer.after = func() {
					if err := os.WriteFile(filepath.Join(sourcePath, source.Events[0].Payload.Path), []byte("changed original"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			output := filepath.Join(t.TempDir(), "run")
			result, err := replay.SendScoped(t.Context(), prepared, output, route, nil, observer)
			if tamper {
				if err == nil || result != nil || received.Load() != 1 || observer.records != 1 {
					t.Fatal("changed original did not stop the next write", err)
				}
				if _, err := replay.Open(output); err == nil {
					t.Fatal("interrupted sequence became complete evidence")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if received.Load() != 2 {
				t.Fatalf("explicit repeated sequence sent %d frames", received.Load())
			}

			reopened, err := replay.Open(output)
			if err != nil || reopened.Identity != result.Identity || reopened.Manifest.SourceBundleIdentity != derived.Identity() {
				t.Fatal("ordinary offline sequence reader lost derived source", err)
			}
			if result.Events[0].ACK.Code != "AA" {
				t.Fatal("actual derived control ID did not correlate")
			}
		})
	}
}
