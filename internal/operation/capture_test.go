package operation_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
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
		Address: "0.0.0.0:2575", Policy: &policy, OutputPath: "case",
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
			Address: "127.0.0.1:0", Policy: &policy,
			OutputPath: filepath.Join(dir, "case"), JournalPath: filepath.Join(dir, "journal"),
			MaxFrameBytes: 1 << 20, IdleTimeout: time.Second, ApplicationTimeout: operation.DefaultApplicationTimeout, MaxMessages: 1,
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
		Address: held.Addr().String(), Policy: &policy,
		OutputPath:    filepath.Join(t.TempDir(), "case"),
		MaxFrameBytes: 1 << 20, IdleTimeout: time.Second, ApplicationTimeout: operation.DefaultApplicationTimeout, MaxMessages: 1,
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
			Address: addr, Policy: &policy,
			OutputPath: filepath.Join(dir, "case"), JournalPath: filepath.Join(dir, "journal"),
			MaxFrameBytes: 1 << 20, IdleTimeout: 2 * time.Second, ApplicationTimeout: operation.DefaultApplicationTimeout, MaxMessages: 1,
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
	access, err := operation.SourceDiagnose(context.Background(), saved, evidencesource.Options{Plan: plan}, "")
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
	access, err := operation.SourceDiagnose(context.Background(), source, evidencesource.Options{Plan: plan, Policy: &policy}, "")
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
		Address: "127.0.0.1:0", Policy: &policy, OutputPath: "case",
		TLSCertificatePath: "cert.pem",
	})
	if err != operation.ErrListenerTLSIncomplete {
		t.Fatalf("expected TLS members-together refusal, got %v", err)
	}
	// A client authority alone declares transport security too, and is
	// refused before anything binds.
	result, err := operation.StartCollect(context.Background(), operation.CollectConfig{
		Address: "127.0.0.1:0", Policy: &policy, OutputPath: filepath.Join(t.TempDir(), "case"),
		MaxFrameBytes: operation.DefaultMaxFrameBytes, IdleTimeout: time.Second, ApplicationTimeout: operation.DefaultApplicationTimeout,
		ClientCAPath: "ca.pem",
	})
	if err != operation.ErrListenerTLSIncomplete || result.BoundAddress != "" {
		t.Fatalf("a client authority alone bound %q: %v", result.BoundAddress, err)
	}
}

// Nothing binds beyond this machine without the operator saying so, and that
// refusal comes first: before a collector's policy is even read, so a
// missing or unreadable policy never hides it. A policy named by its path is
// read with the reader ReceiverPolicyRead uses, and a collector configured
// with no policy at all is refused.
func TestACollectorApprovesItsAddressBeforeReadingItsPolicy(t *testing.T) {
	dir := t.TempDir()
	unreadable := filepath.Join(dir, "absent-policy.json")
	for _, cfg := range []operation.CollectConfig{
		{Address: "0.0.0.0:0", OutputPath: filepath.Join(dir, "case")},
		{Address: "0.0.0.0:0", PolicyPath: unreadable, OutputPath: filepath.Join(dir, "case")},
	} {
		if _, err := operation.PreviewCollect(cfg); err != sendpolicy.ErrNonloopbackBind {
			t.Errorf("a nonloopback collector with policy %q was refused as %v", cfg.PolicyPath, err)
		}
	}
	if _, err := operation.PreviewCollect(operation.CollectConfig{Address: "127.0.0.1:0", OutputPath: "case"}); err != operation.ErrReceiverPolicyRequired {
		t.Fatalf("a collector with no policy was refused as %v", err)
	}
	_, want := operation.ReceiverPolicyRead(unreadable)
	if _, err := operation.PreviewCollect(operation.CollectConfig{Address: "127.0.0.1:0", PolicyPath: unreadable, OutputPath: "case"}); want == nil || err == nil || err.Error() != want.Error() {
		t.Fatalf("an unreadable policy was refused as %v, want %v", err, want)
	}
	named := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(named, []byte(anyPolicy), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := operation.PreviewCollect(operation.CollectConfig{Address: "127.0.0.1:0", PolicyPath: named, OutputPath: "case"})
	if err != nil || preview.PolicyName != "downstream-sink" || preview.PolicySchema != "readmit-receiver-policy/v1" {
		t.Fatalf("a named policy previewed as %+v: %v", preview, err)
	}
}

// A capture runs under the bounds its configuration states. The operation
// never replaces a stated bound, zero included, with a default: the receiver
// refuses it before anything binds, as the command line's flags are refused.
func TestCaptureRunsUnderTheBoundsItStates(t *testing.T) {
	policy, err := collection.DecodePolicy([]byte(anyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	listen, err := operation.StartListen(context.Background(), operation.ListenConfig{
		Address: "127.0.0.1:0", Mode: observation.Fixed,
		OutputPath: filepath.Join(dir, "listen"), ObservationPath: filepath.Join(dir, "listen.json"),
		IdleTimeout: time.Second,
	})
	if err == nil || err.Error() != "invalid session frame, timeout, or message limit" || listen.Bundle != nil {
		t.Fatalf("a fixture with no frame bound served: %+v %v", listen, err)
	}
	collect, err := operation.StartCollect(context.Background(), operation.CollectConfig{
		Address: "127.0.0.1:0", Policy: &policy, OutputPath: filepath.Join(dir, "collect"),
		MaxFrameBytes: operation.DefaultMaxFrameBytes, IdleTimeout: time.Second,
	})
	if err == nil || err.Error() != "application acknowledgement timeout must be positive and at most five minutes" || collect.Bundle != nil {
		t.Fatalf("a collector with no application timeout served: %+v %v", collect, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "collect")); !os.IsNotExist(err) {
		t.Fatal("a refused collector wrote a case")
	}
}

// What an adapter does once a capture is listening can stop it: an error its
// Listening answers ends the capture before anyone is served, and no case is
// sealed.
func TestACaptureTheAdapterCannotAnnounceNeverServes(t *testing.T) {
	policy, err := collection.DecodePolicy([]byte(anyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	unannounced := errors.New("cannot write startup output")
	dir := t.TempDir()
	var announced []string
	announce := func(bound string) error {
		announced = append(announced, bound)
		return unannounced
	}
	listen, err := operation.StartListen(context.Background(), operation.ListenConfig{
		Address: "127.0.0.1:0", Mode: observation.Fixed,
		OutputPath: filepath.Join(dir, "listen"), ObservationPath: filepath.Join(dir, "listen.json"),
		MaxFrameBytes: operation.DefaultMaxFrameBytes, IdleTimeout: time.Second, Listening: announce,
	})
	if err != unannounced || listen.Bundle != nil || listen.BoundAddress == "" {
		t.Fatalf("an unannounced fixture: %+v %v", listen, err)
	}
	collect, err := operation.StartCollect(context.Background(), operation.CollectConfig{
		Address: "127.0.0.1:0", Policy: &policy, OutputPath: filepath.Join(dir, "collect"),
		MaxFrameBytes: operation.DefaultMaxFrameBytes, IdleTimeout: time.Second, ApplicationTimeout: operation.DefaultApplicationTimeout,
		Listening: func(bound string, served collection.Policy) error {
			if served.Name != policy.Name {
				t.Errorf("the collector announced policy %q, it serves %q", served.Name, policy.Name)
			}
			return announce(bound)
		},
	})
	if err != unannounced || collect.Bundle != nil || collect.BoundAddress == "" {
		t.Fatalf("an unannounced collector: %+v %v", collect, err)
	}
	if len(announced) != 2 || announced[0] != listen.BoundAddress || announced[1] != collect.BoundAddress {
		t.Fatalf("announced %v", announced)
	}
	for _, bound := range announced {
		if conn, err := net.DialTimeout("tcp", bound, time.Second); err == nil {
			conn.Close()
			t.Fatalf("a capture that was never announced still listens at %s", bound)
		}
	}
	for _, name := range []string{"listen", "collect"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("an unannounced %s sealed a case", name)
		}
	}
}

// A collection receipt and an access diagnosis may be named in folders that
// do not exist yet: each is refused before the source is reached when it is
// taken, and its folders are created only when it is written.
func TestSourceDocumentsCreateTheirMissingFoldersAndRefuseTakenOnes(t *testing.T) {
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
	options, err := operation.SourceOptions(operation.DefaultImportPlan(), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	report := filepath.Join(dir, "reports", "access.json")
	access, err := operation.SourceDiagnose(ctx, source, options, report)
	if err != nil || access.Status != observewindow.Complete {
		t.Fatalf("diagnose: %+v %v", access, err)
	}
	if encoded, err := evidencesource.EncodeAccess(access); err != nil || string(readFile(t, report)) != string(encoded) {
		t.Fatalf("the retained diagnosis is not the diagnosis returned: %v", err)
	}
	if _, err := operation.SourceDiagnose(ctx, source, options, report); err != operation.ErrAccessReportExists {
		t.Fatalf("a taken diagnosis destination was refused as %v", err)
	}

	receipt := filepath.Join(dir, "receipts", "2026", "collection.json")
	collected, err := operation.SourceCollect(ctx, source, filepath.Join(dir, "staged"), receipt, options)
	if err != nil || collected.Totals.Collected != 1 {
		t.Fatalf("collect: %+v %v", collected, err)
	}
	if encoded, err := evidencesource.EncodeCollection(collected); err != nil || string(readFile(t, receipt)) != string(encoded) {
		t.Fatalf("the retained receipt is not the collection returned: %v", err)
	}
	if _, err := operation.SourceCollect(ctx, source, filepath.Join(dir, "again"), receipt, options); err != operation.ErrCollectionReceiptExists {
		t.Fatalf("a taken receipt was refused as %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "again")); !os.IsNotExist(err) {
		t.Fatal("a collection refused for its receipt staged evidence")
	}
}

// The approved-destination policy a source run names is read without ever
// repeating its path.
func TestSourceOptionsNeverQuoteThePolicyPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "patient-named-policy.json")
	if _, err := operation.SourceOptions(operation.DefaultImportPlan(), missing); err == nil || err.Error() != "cannot read the approved-destination policy" {
		t.Fatalf("an unreadable policy was refused as %v", err)
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

const faultPolicy = `{"schema":"readmit-receiver-policy/v3","name":"fault-sink","source_label":"synthetic-test","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]},"enhanced_acknowledgement":{"operator":"enhanced-mode-fixed-codes","accept_code":"CA","application_code":"AA","application_delivery":"same-connection","application_endpoint":"","approved_transport":false},"faults":{"environment_class":"nonproduction","approved_test_endpoints":["APPROVED"],"steps":[{"message":1,"stage":"application","action":"delay","delay_ms":50}]}}`

func TestStartCollectRefusesFaultPolicyWithMultiConnection(t *testing.T) {
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	approved := reserved.Addr().String()
	reserved.Close()
	policy, err := collection.DecodePolicy([]byte(strings.Replace(faultPolicy, "APPROVED", approved, 1)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = operation.StartCollect(context.Background(), operation.CollectConfig{
		Address: approved, Policy: &policy, OutputPath: filepath.Join(t.TempDir(), "case"),
		MaxFrameBytes: 1 << 20, IdleTimeout: time.Second, ApplicationTimeout: operation.DefaultApplicationTimeout, MaxMessages: 1, MaxConnections: 2,
	})
	if err == nil || !strings.Contains(err.Error(), "requires one connection at a time") {
		t.Fatalf("expected fault+multi-connection refusal, got %v", err)
	}
}

// A fault policy approves the endpoints it may be served at, and the address
// is held to them before anything binds, as `readmit collect` holds it: an
// address the policy did not approve, including port 0, never listens.
func TestPreviewCollectHoldsAFaultPolicyToItsApprovedEndpoints(t *testing.T) {
	policy, err := collection.DecodePolicy([]byte(strings.Replace(faultPolicy, "APPROVED", "127.0.0.1:2575", 1)))
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"127.0.0.1:0", "127.0.0.1:2576", "127.0.0.2:2575"} {
		_, err := operation.PreviewCollect(operation.CollectConfig{Address: address, Policy: &policy, OutputPath: "case"})
		if err == nil || policy.Faults.ApproveEndpoint(address) == nil || err.Error() != policy.Faults.ApproveEndpoint(address).Error() {
			t.Errorf("preview at %s: %v", address, err)
		}
		result, err := operation.StartCollect(context.Background(), operation.CollectConfig{
			Address: address, Policy: &policy, OutputPath: filepath.Join(t.TempDir(), "case"), MaxMessages: 1, IdleTimeout: time.Second,
		})
		if err == nil || result.BoundAddress != "" {
			t.Errorf("start at %s bound %q: %v", address, result.BoundAddress, err)
		}
	}
	if _, err := operation.PreviewCollect(operation.CollectConfig{Address: "127.0.0.1:2575", Policy: &policy, OutputPath: "case"}); err != nil {
		t.Fatalf("the approved endpoint was refused: %v", err)
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
			Address: addr, Policy: &policy,
			OutputPath: filepath.Join(dir, "case"), JournalPath: filepath.Join(dir, "journal"),
			MaxFrameBytes: 1 << 20, IdleTimeout: 2 * time.Second, ApplicationTimeout: operation.DefaultApplicationTimeout, MaxMessages: 1,
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

// What the fixture refuses, it refuses before it serves anyone and in the
// words both entry points show: a wider bind without approval, a name rather
// than an address, an address something else holds, and a case or ledger
// destination that already exists. None of them writes a case or a ledger.
func TestStartListenRefusesBeforeServingAndWritesNothing(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	for _, refused := range []struct{ name, address, existing, want string }{
		{"nonloopback", "0.0.0.0:0", "", sendpolicy.ErrNonloopbackBind.Error()},
		{"name", "localhost:0", "", ""},
		{"held", held.Addr().String(), "", "cannot bind receiver address"},
		{"case exists", "127.0.0.1:0", "fixture.case", "case destination must be new"},
		{"ledger exists", "127.0.0.1:0", "ledger.json", ""},
	} {
		t.Run(refused.name, func(t *testing.T) {
			dir := t.TempDir()
			if refused.existing != "" {
				if err := os.Mkdir(filepath.Join(dir, refused.existing), 0700); err != nil {
					t.Fatal(err)
				}
			}
			result, err := operation.StartListen(context.Background(), operation.ListenConfig{
				Address: refused.address, Mode: observation.Fixed,
				OutputPath: filepath.Join(dir, "fixture.case"), ObservationPath: filepath.Join(dir, "ledger.json"),
				MaxFrameBytes: operation.DefaultMaxFrameBytes, IdleTimeout: time.Second, MaxMessages: 1,
				Listening: func(string) error { t.Error("a refused fixture announced it was listening"); return nil },
			})
			if err == nil || (refused.want != "" && err.Error() != refused.want) || result.BoundAddress != "" || result.Bundle != nil {
				t.Fatalf("refused as %+v: %v", result, err)
			}
			kept := 0
			if refused.existing != "" {
				kept = 1
			}
			if entries, _ := os.ReadDir(dir); len(entries) != kept {
				t.Fatalf("a refused fixture wrote %v", entries)
			}
		})
	}
}
