package replay_test

import (
	"encoding/json/v2"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

func TestPreviewRetainsOneDecisionWithoutConnecting(t *testing.T) {
	address := peer(t, func(net.Conn) { t.Error("preview opened a connection") })
	path := filepath.Join(t.TempDir(), "preview"+replay.DecisionSuffix)
	calls := 0
	plan, err := replay.Preview(t.Context(), caseAt(t, request("PREVIEW")), target(address), replay.Options{}, replay.SendOptions{
		DecisionPath: path,
		Record: func(d sendpolicy.Decision) error {
			calls++
			if d.ExplicitSend {
				t.Error("preview requested a send")
			}
			return nil
		},
	})
	if err != nil || plan == nil || calls != 1 {
		t.Fatalf("preview: plan=%v calls=%d err=%v", plan, calls, err)
	}
	if d := retainedDecision(t, path); d.ExplicitSend {
		t.Fatal("preview retained a send decision")
	}
}

func TestPreparedSendRetainsExactlyOneDecisionBeforeConnecting(t *testing.T) {
	var calls atomic.Int32
	output := filepath.Join(t.TempDir(), "run")
	path := output + replay.DecisionSuffix
	address := peer(t, func(conn net.Conn) {
		if calls.Load() != 1 {
			t.Error("connection preceded its one recorded decision")
		}
		if d := retainedDecision(t, path); !d.Allowed || !d.ExplicitSend {
			t.Error("connection preceded retained send approval")
		}
		reader, _ := mllp.NewReader(conn, 4096)
		if _, err := reader.ReadFrame(); err != nil {
			t.Error(err)
			return
		}
		_, _ = conn.Write(ack("AA", "SEND"))
	})
	options := replay.SendOptions{DecisionPath: path, Record: func(sendpolicy.Decision) error { calls.Add(1); return nil }}
	plan, err := replay.PrepareSend(t.Context(), caseAt(t, request("SEND")), target(address), replay.Options{}, options)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("preparation prematurely recorded send approval")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("preparation wrote a decision")
	}
	run, err := replay.Send(t.Context(), plan, output, options)
	if err != nil || run == nil || !run.Successful() || calls.Load() != 1 {
		t.Fatalf("send: run=%v calls=%d err=%v", run, calls.Load(), err)
	}
}

func TestPrepareSendRetainsOneProductionRefusalBeforeReadingEvidence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "refused"+replay.DecisionSuffix)
	calls := 0
	plan, err := replay.PrepareSend(t.Context(), filepath.Join(dir, "absent-case"), productionTarget(), replay.Options{}, replay.SendOptions{
		DecisionPath: path, Record: func(sendpolicy.Decision) error { calls++; return nil },
	})
	if err == nil || plan != nil || calls != 1 {
		t.Fatalf("production refusal: plan=%v calls=%d err=%v", plan, calls, err)
	}
	if d := retainedDecision(t, path); d.Allowed || !d.ExplicitSend || d.Reason != sendpolicy.ProductionClassification {
		t.Fatalf("wrong refusal: %+v", d)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("refusal created evidence beyond its decision", err)
	}
}

func retainedDecision(t *testing.T, path string) sendpolicy.Decision {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var d sendpolicy.Decision
	if err := json.Unmarshal(raw, &d, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	return d
}
