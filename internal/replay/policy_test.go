package replay_test

import (
	"context"

	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

func TestExecuteRequiresPolicyForNames(t *testing.T) {
	config := target("localhost:2575")
	config.ApprovedTransport = true
	plan, err := replay.Prepare(caseAt(t, request("BOOK")), config, replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "run")
	if _, err := replay.Execute(context.Background(), plan, output); err == nil || !strings.Contains(err.Error(), "policy_required") {
		t.Fatalf("name was not refused before connecting: %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("denied send created a run")
	}
}

func TestPolicyPinsTheCheckedAddress(t *testing.T) {
	recorded := false
	address := peer(t, func(conn net.Conn) {
		reader, _ := mllp.NewReader(conn, 4096)
		if _, err := reader.ReadFrame(); err != nil {
			t.Error(err)
			return
		}
		_, _ = conn.Write(ack("AA", "PINNED"))
	})
	_, port, _ := net.SplitHostPort(address)
	config := target(net.JoinHostPort("never-resolve.example.invalid", port))
	config.Schema, config.Name, config.Classification, config.ApprovedTransport = replay.TargetSchemaV3, "lab", replay.Nonproduction, true
	plan, err := replay.Prepare(caseAt(t, request("PINNED")), config, replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	policy := &sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"127.0.0.0/8"}}
	resolutions := 0
	resolve := func(context.Context, string) ([]netip.Addr, error) {
		resolutions++
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	record := func(d sendpolicy.Decision) error {
		if !d.Allowed || d.ResolvedAddresses[0] != "127.0.0.1" {
			t.Fatalf("wrong decision: %+v", d)
		}
		recorded = true
		return nil
	}
	output := filepath.Join(t.TempDir(), "run")
	run, err := replay.ExecuteWithResolverForTest(t.Context(), plan, output, policy, record, resolve)
	if err != nil || !run.Successful() || !recorded || resolutions != 1 {
		t.Fatalf("pinned exchange: run=%+v err=%v recorded=%v resolves=%d", run, err, recorded, resolutions)
	}
	if run.Manifest.Target.Address != config.Address {
		t.Fatal("pinning changed the recorded configuration")
	}
}

func TestPolicyRecordingFailurePreventsConnection(t *testing.T) {
	address := peer(t, func(net.Conn) { t.Error("connected after recording failed") })
	config := target(address)
	plan, err := replay.Prepare(caseAt(t, request("BOOK")), config, replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "run")
	refused := errors.New("cannot retain decision")
	_, err = replay.ExecuteWithPolicy(t.Context(), plan, output, nil, func(sendpolicy.Decision) error { return refused })
	if !errors.Is(err, refused) {
		t.Fatalf("record failure: %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("record failure created a run")
	}
}

func TestPolicyResolutionUsesConnectDeadline(t *testing.T) {
	config := target("never-resolve.example.invalid:2575")
	config.Schema, config.Name, config.Classification, config.ApprovedTransport = replay.TargetSchemaV3, "lab", replay.Nonproduction, true
	plan, err := replay.Prepare(caseAt(t, request("BOOK")), config, replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	policy := &sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"127.0.0.0/8"}}
	resolve := func(ctx context.Context, _ string) ([]netip.Addr, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Error("DNS escaped the connection deadline")
		}
		return nil, context.DeadlineExceeded
	}
	var decision sendpolicy.Decision
	output := filepath.Join(t.TempDir(), "run")
	_, err = replay.ExecuteWithResolverForTest(t.Context(), plan, output, policy, func(d sendpolicy.Decision) error { decision = d; return nil }, resolve)
	if err == nil || decision.Allowed || decision.Reason != sendpolicy.UnresolvableDestination {
		t.Fatalf("resolution failure: %v %+v", err, decision)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("resolution failure created a run")
	}
}
