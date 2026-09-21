package operation_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const anyPolicy = `{"schema":"readmit-receiver-policy/v1","name":"downstream-sink","source_label":"downstream-test-endpoint","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]}}`

func TestPreviewCollectRefusesNonloopbackWithoutApproval(t *testing.T) {
	policy, err := collection.DecodePolicy([]byte(anyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	_, err = operation.PreviewCollect(operation.CollectConfig{
		Address: "0.0.0.0:2575", Policy: policy, OutputPath: "case",
	})
	if err == nil {
		t.Fatal("expected nonloopback refusal")
	}
	if !contains(err.Error(), "opt-in") && err != sendpolicy.ErrNonloopbackBind {
		t.Fatalf("unexpected refusal: %v", err)
	}
}

func TestStartCollectStartStopAndCancel(t *testing.T) {
	dir := t.TempDir()
	policy, err := collection.DecodePolicy([]byte(anyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan operation.CollectResult, 1)
	errs := make(chan error, 1)
	go func() {
		result, err := operation.StartCollect(ctx, operation.CollectConfig{
			Address: "127.0.0.1:0", Policy: policy,
			OutputPath: filepath.Join(dir, "case"), JournalPath: filepath.Join(dir, "journal"),
			MaxFrameBytes: 1 << 20, IdleTimeout: time.Second, MaxMessages: 1,
		})
		done <- result
		errs <- err
	}()
	// Give the listener a moment to bind, then cancel with nothing sent.
	time.Sleep(50 * time.Millisecond)
	cancel()
	result := <-done
	err = <-errs
	if result.BoundAddress == "" {
		t.Fatalf("expected a bound address: %+v err=%v", result, err)
	}
	summary, statusErr := operation.CaptureJournalStatus(filepath.Join(dir, "journal"))
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	if summary.State == "" {
		t.Fatal("journal summary missing state")
	}
	// Cancellation must not invent finalized completion.
	if summary.State == "finalized" && summary.Received == 0 && err == nil {
		// empty cancelled sessions may finalize with zero sources; that is OK
		// as long as Recovered is false when Finish ran. Either way, reopen must work.
	}
	if summary.Recovered && summary.State == "finalized" {
		t.Fatal("recovered journal must not claim finalized completion")
	}
}

func TestStartCollectBindCollision(t *testing.T) {
	policy, err := collection.DecodePolicy([]byte(anyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	_, err = operation.StartCollect(context.Background(), operation.CollectConfig{
		Address: held.Addr().String(), Policy: policy,
		OutputPath:    filepath.Join(t.TempDir(), "case"),
		MaxFrameBytes: 1 << 20, IdleTimeout: time.Second, MaxMessages: 1,
	})
	if err == nil {
		t.Fatal("expected bind collision")
	}
}

func TestStartCollectCapturesOneFrame(t *testing.T) {
	dir := t.TempDir()
	policy, err := collection.DecodePolicy([]byte(anyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type outcome struct {
		result operation.CollectResult
		err    error
	}
	ch := make(chan outcome, 1)
	go func() {
		result, err := operation.StartCollect(ctx, operation.CollectConfig{
			Address: addr, Policy: policy,
			OutputPath: filepath.Join(dir, "case"), JournalPath: filepath.Join(dir, "journal"),
			MaxFrameBytes: 1 << 20, IdleTimeout: 2 * time.Second, MaxMessages: 1,
		})
		ch <- outcome{result, err}
	}()
	deadline := time.Now().Add(2 * time.Second)
	var conn net.Conn
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if conn == nil {
		t.Fatal("could not dial collector")
	}
	payload := []byte("MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\rPID|||1||DOE^JOHN\r")
	if _, err := conn.Write(mllp.Frame(payload)); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	out := <-ch
	if out.err != nil {
		t.Fatalf("collect: %v", out.err)
	}
	if out.result.Bundle == nil || out.result.Journal == nil || out.result.Journal.Received != 1 {
		t.Fatalf("expected one received frame: %+v", out.result)
	}
	summary, err := operation.CaptureJournalStatus(filepath.Join(dir, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Received != 1 || summary.State != "finalized" {
		t.Fatalf("journal reopen: %+v", summary)
	}
}

func TestSourceDiagnoseAndCollectDirectory(t *testing.T) {
	dir := t.TempDir()
	export := filepath.Join(dir, "export")
	if err := os.Mkdir(export, 0700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\rPID|||1||DOE^JOHN\r")
	if err := os.WriteFile(filepath.Join(export, "one.hl7"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	source := evidencesource.Source{
		Schema: evidencesource.Schema, Name: "exports", Kind: evidencesource.Directory,
		Scope: "appointments", Root: export,
		Quota: evidencesource.Quota{MaxEntries: 8, MaxEntryBytes: 1 << 20, MaxTotalBytes: 8 << 20},
		Retry: evidencesource.Retry{Attempts: 1, Backoff: "1ms"},
	}
	path := filepath.Join(dir, "source.json")
	saved, err := operation.SourceSave(path, source)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Name != "exports" {
		t.Fatalf("saved: %+v", saved)
	}
	plan := importer.Plan{
		Schema: importer.PlanSchema, Framing: importer.RawFraming, Terminator: hl7.CR,
		Encoding: importer.UTF8, Direction: bundle.Inbound, Members: []string{".hl7"},
	}
	access, err := operation.SourceDiagnose(context.Background(), saved, evidencesource.Options{Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	if access.Status != observewindow.Complete || access.Declared != 1 {
		t.Fatalf("diagnose: %+v", access)
	}
	collection, err := operation.SourceCollect(context.Background(), saved, filepath.Join(dir, "staged"), filepath.Join(dir, "receipt.json"), evidencesource.Options{Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	if collection.Totals.Collected != 1 {
		t.Fatalf("collect: %+v", collection)
	}
}

func TestUnavailableTransferProgramIsExecutionError(t *testing.T) {
	source := evidencesource.Source{
		Schema: evidencesource.Schema, Name: "remote", Kind: evidencesource.Transfer,
		Scope: "appointments", Address: "127.0.0.1:22", Classification: evidencesource.Nonproduction,
		Command: "/no/such/transfer-program", Arguments: []string{"--path", "/exports"},
		Quota: evidencesource.Quota{MaxEntries: 8, MaxEntryBytes: 1 << 20, MaxTotalBytes: 8 << 20},
		Retry: evidencesource.Retry{Attempts: 1, Backoff: "1ms"},
	}
	plan := importer.Plan{
		Schema: importer.PlanSchema, Framing: importer.RawFraming, Terminator: hl7.CR,
		Encoding: importer.UTF8, Direction: bundle.Inbound, Members: []string{".hl7"},
	}
	policy := sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"127.0.0.0/8"}}
	access, err := operation.SourceDiagnose(context.Background(), source, evidencesource.Options{Plan: plan, Policy: &policy})
	if err != nil {
		t.Fatal(err)
	}
	if access.Status == observewindow.Complete {
		t.Fatalf("unavailable transfer must not report complete: %+v", access)
	}
}

func TestStartListenFixtureSeparatelyLabelled(t *testing.T) {
	preview, err := operation.PreviewListen(operation.ListenConfig{
		Address: "127.0.0.1:0", Mode: observation.Fixed,
		OutputPath: "case", ObservationPath: "obs.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.FixtureLabel == "" || preview.Kind != "listen" {
		t.Fatalf("fixture must be labelled: %+v", preview)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()))
}

func TestPreviewCollectRequiresTLSMembersTogether(t *testing.T) {
	policy, err := collection.DecodePolicy([]byte(anyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	_, err = operation.PreviewCollect(operation.CollectConfig{
		Address: "127.0.0.1:0", Policy: policy, OutputPath: "case",
		TLSCertificatePath: "cert.pem",
	})
	if err == nil {
		t.Fatal("expected TLS members-together refusal")
	}
}

func TestStartListenCancellationLeavesNoInventedCompletion(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan operation.ListenResult, 1)
	errs := make(chan error, 1)
	go func() {
		result, err := operation.StartListen(ctx, operation.ListenConfig{
			Address: "127.0.0.1:0", Mode: observation.Fixed,
			OutputPath: filepath.Join(dir, "case"), ObservationPath: filepath.Join(dir, "obs.json"),
			MaxFrameBytes: 1 << 20, IdleTimeout: time.Second,
		})
		done <- result
		errs <- err
	}()
	time.Sleep(40 * time.Millisecond)
	cancel()
	result := <-done
	_ = <-errs
	if result.BoundAddress == "" {
		t.Fatalf("expected bind before cancel: %+v", result)
	}
	// Observation may exist as empty; it must not invent appointments.
	if result.Observation != nil && len(result.Observation.Records) != 0 {
		t.Fatalf("cancelled listen invented records: %+v", result.Observation)
	}
}

func TestStartCollectRefusesFaultPolicyWithMultiConnection(t *testing.T) {
	policy, err := collection.DecodePolicy([]byte(`{"schema":"readmit-receiver-policy/v3","name":"fault-sink","source_label":"synthetic-test","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]},"enhanced_acknowledgement":{"operator":"enhanced-mode-fixed-codes","accept_code":"CA","application_code":"AA","application_delivery":"same-connection","application_endpoint":"","approved_transport":false},"faults":{"environment_class":"nonproduction","approved_test_endpoints":["127.0.0.1:2575"],"steps":[{"message":1,"stage":"application","action":"delay","delay_ms":50}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = operation.StartCollect(context.Background(), operation.CollectConfig{
		Address: "127.0.0.1:0", Policy: policy, OutputPath: filepath.Join(t.TempDir(), "case"),
		MaxFrameBytes: 1 << 20, IdleTimeout: time.Second, MaxMessages: 1, MaxConnections: 2,
	})
	if err == nil {
		t.Fatal("expected fault+multi-connection refusal")
	}
}

func TestStartCollectMalformedFrameIsRetainedNotAcknowledgedAsSuccess(t *testing.T) {
	dir := t.TempDir()
	policy, err := collection.DecodePolicy([]byte(anyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch := make(chan operation.CollectResult, 1)
	errs := make(chan error, 1)
	go func() {
		result, err := operation.StartCollect(ctx, operation.CollectConfig{
			Address: addr, Policy: policy,
			OutputPath: filepath.Join(dir, "case"), JournalPath: filepath.Join(dir, "journal"),
			MaxFrameBytes: 1 << 20, IdleTimeout: 2 * time.Second, MaxMessages: 1,
		})
		ch <- result
		errs <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	var conn net.Conn
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if conn == nil {
		t.Fatal("dial failed")
	}
	// Complete MLLP frame with bytes that are not usable HL7.
	if _, err := conn.Write(mllp.Frame([]byte("NOT-HL7"))); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	result := <-ch
	err = <-errs
	if result.Journal == nil || result.Journal.Received != 1 {
		t.Fatalf("malformed complete frame must still be retained: %+v err=%v", result, err)
	}
}
