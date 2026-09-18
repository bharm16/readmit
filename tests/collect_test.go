package tests

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
)

const collectPolicy = `{"schema":"readmit-receiver-policy/v1","name":"downstream-sink","source_label":"downstream-test-endpoint","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"message-type-in","values":["ADT^A01"]}}`

const collectAnyPolicy = `{"schema":"readmit-receiver-policy/v1","name":"integration-sink","source_label":"replay-downstream","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]}}`

const collectADT = "MSH|^~\\&|SENDER|FACILITY|READMIT|COLLECT|20260101120000||ADT^A01|COLLECT-001|P|2.5.1\rPID|1||SYNTH-001\r"

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../testdata/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func policyFile(t *testing.T, dir, declared string) string {
	t.Helper()
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(path, []byte(declared), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCollectExecutableRetainsLabelledDownstreamEvidence(t *testing.T) {
	dir := t.TempDir()
	casePath := filepath.Join(dir, "collected")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "collect", "--address", "127.0.0.1:0", "--policy", policyFile(t, dir, collectPolicy), "--output", casePath, "--max-messages", "2", "--idle-timeout", "2s")
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	reader := bufio.NewReader(stdout)
	ready, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(ready, "Listening: ") {
		t.Fatalf("collector not ready: %q %v", ready, err)
	}
	conn, err := net.DialTimeout("tcp", strings.TrimSpace(strings.TrimPrefix(ready, "Listening: ")), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	frames, _ := mllp.NewReader(conn, 1<<20)
	for _, exchange := range []struct{ payload, code, control string }{
		{collectADT, "AA", "COLLECT-001"},
		{string(readFixture(t, "listen-s12.hl7")), "AR", "LISTEN-BOOK"},
	} {
		if _, err := conn.Write(mllp.Frame([]byte(exchange.payload))); err != nil {
			t.Fatal(err)
		}
		response, err := frames.ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		doc, err := hl7.Parse(response, hl7.Options{Format: hl7.MLLP})
		if err != nil {
			t.Fatal(err)
		}
		msa := doc.Messages[0].Segments[1]
		if string(doc.Bytes(msa.Field(1).Span)) != exchange.code || string(doc.Bytes(msa.Field(2).Span)) != exchange.control {
			t.Fatalf("wrong collector acknowledgement: %q", response)
		}
	}
	remaining, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("collect: %v %s", err, diagnostic.String())
	}
	if diagnostic.Len() != 0 {
		t.Fatalf("unexpected diagnostic: %s", diagnostic.String())
	}
	output := ready + string(remaining)
	for _, want := range []string{
		"Policy: downstream-sink", "Source label: downstream-test-endpoint",
		"Acknowledgement: original-mode-fixed-code AA", "Application processing: none",
		"Schema: readmit-case/v4", "Provenance: collected", "Messages: 2", "ACKs: 2",
		"Collection: readmit-collection/v2", "Collected sessions: 1", "Received frames: 2",
		"Enhanced acknowledgement: unsupported", "Accept acknowledgements: 0",
		"Application acknowledgements: 2", "c0001 source=s0001 label=downstream-test-endpoint",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("missing summary %q in %s", want, output)
		}
	}
	for _, private := range []string{"COLLECT-001", "LISTEN-BOOK", "SYNTH-001", dir} {
		if strings.Contains(output, private) {
			t.Errorf("default output disclosed %q", private)
		}
	}
	timeline, stderr, err := run(t, "timeline", casePath)
	if err != nil || stderr != "" || !strings.Contains(timeline, "Collection policy: downstream-sink") || strings.Contains(timeline, "SYNTH-001") {
		t.Fatalf("collected timeline: %v %s %s", err, stderr, timeline)
	}
	timeline, stderr, err = run(t, "timeline", casePath, "--show-values")
	if err != nil || stderr != "" || !strings.Contains(timeline, "Collection values:") || !strings.Contains(timeline, "COLLECT-001") {
		t.Fatal("explicit collection record values unavailable")
	}
}

func TestCollectInvalidArgumentsArePrivate(t *testing.T) {
	dir := t.TempDir()
	valid := policyFile(t, dir, collectPolicy)
	unsupported := filepath.Join(dir, "unsupported.json")
	if err := os.WriteFile(unsupported, []byte(strings.Replace(collectPolicy, "downstream-sink", "SECRET NAME", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tail := range [][]string{
		{},
		{"--output", filepath.Join(dir, "case")},
		{"--policy", valid},
		{"--policy", filepath.Join(dir, "SECRET-MISSING.json"), "--output", filepath.Join(dir, "case")},
		{"--policy", unsupported, "--output", filepath.Join(dir, "case")},
		{"--policy", valid, "--output", dir},
		{"--policy", valid, "--output", filepath.Join(dir, "case"), "--max-messages", "4001"},
		{"--policy", valid, "--output", filepath.Join(dir, "case"), "--max-frame-bytes", "0"},
		{"--policy", valid, "--output", filepath.Join(dir, "case"), "--idle-timeout", "0s"},
		{"--policy", valid, "--output", filepath.Join(dir, "case"), "--application-ack-timeout", "0s"},
	} {
		args := append([]string{"collect", "--address", "127.0.0.1:0"}, tail...)
		stdout, stderr, err := run(t, args...)
		if err == nil || stdout != "" || stderr == "" || len(stderr) > 300 || strings.Contains(stderr, "SECRET") {
			t.Fatalf("unsafe collect error: %v %q %q", err, stdout, stderr)
		}
	}
}

// The collector is the downstream sink of a real integration run: replay is an
// independently implemented client that knows nothing about this receiver.
func TestCollectExecutableIsTheDownstreamSinkOfARealReplayRun(t *testing.T) {
	source := replayCase(t)
	before, err := bundle.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	casePath := filepath.Join(dir, "collected")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "collect", "--address", "127.0.0.1:0", "--policy", policyFile(t, dir, collectAnyPolicy), "--output", casePath, "--max-messages", "2", "--idle-timeout", "2s")
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	pipe, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	reader := bufio.NewReader(pipe)
	ready, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(ready, "Listening: ") {
		t.Fatalf("collector not ready: %q %v", ready, err)
	}
	address := strings.TrimSpace(strings.TrimPrefix(ready, "Listening: "))
	output := filepath.Join(dir, "run")
	stdout, stderr, err := run(t, "replay", source, "--target", replayTarget(t, address), "--send", "--output", output)
	if err != nil || stderr != "" || strings.Count(stdout, "outcome=application_accepted") != 2 {
		t.Fatalf("replay against the collector: %v %s %s", err, stdout, stderr)
	}
	_, _ = io.Copy(io.Discard, reader)
	if err := command.Wait(); err != nil {
		t.Fatalf("collector: %v %s", err, diagnostic.String())
	}
	collected, err := bundle.Open(casePath)
	if err != nil {
		t.Fatal(err)
	}
	record := collected.Collection
	if record == nil || len(record.Received) != 2 || record.Received[0].ControlID != "LISTEN-BOOK" || record.Received[1].ControlID != "LISTEN-MOVE" {
		t.Fatalf("collector did not retain the replayed messages: %+v", record)
	}
	for _, received := range record.Received {
		if received.Mode != collection.OriginalMode || received.Application.Code != "AA" {
			t.Fatalf("replayed message was not acknowledged: %+v", received)
		}
	}
	// The client's accept outcome is a transport result. The collected evidence
	// still states that no application processed these messages.
	if record.ApplicationProcessing != collection.NoApplicationProcessing || len(record.Sessions) != 1 || record.Sessions[0].Label != "replay-downstream" {
		t.Fatalf("accept acknowledgements implied application processing: %+v", record)
	}
	after, err := bundle.Open(source)
	if err != nil || after.Identity != before.Identity {
		t.Fatal("the replayed source case changed")
	}
}

// A frame beyond --max-frame-bytes closes its connection. Its consumed prefix
// stays in the case, and no receipt claims it was received or acknowledged.
func TestCollectExecutableBoundsFrameSizeWithoutDiscardingEvidence(t *testing.T) {
	dir := t.TempDir()
	casePath := filepath.Join(dir, "collected")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "collect", "--address", "127.0.0.1:0", "--policy", policyFile(t, dir, collectAnyPolicy), "--output", casePath, "--max-messages", "1", "--max-frame-bytes", "64", "--idle-timeout", "2s")
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	pipe, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	reader := bufio.NewReader(pipe)
	ready, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(ready, "Listening: ") {
		t.Fatalf("collector not ready: %q %v", ready, err)
	}
	address := strings.TrimSpace(strings.TrimPrefix(ready, "Listening: "))
	oversized, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer oversized.Close()
	_ = oversized.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := oversized.Write(mllp.Frame([]byte(collectADT))); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(oversized); err != nil {
		t.Fatalf("oversized frame was not closed without an acknowledgement: %v", err)
	}
	accepted, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()
	_ = accepted.SetDeadline(time.Now().Add(5 * time.Second))
	small := "MSH|^~\\&|||||||ADT^A01|SMALL-1|P|2.5.1\rPID|1\r"
	if _, err := accepted.Write(mllp.Frame([]byte(small))); err != nil {
		t.Fatal(err)
	}
	frames, _ := mllp.NewReader(accepted, 1<<20)
	response, err := frames.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := hl7.Parse(response, hl7.Options{Format: hl7.MLLP})
	if err != nil {
		t.Fatal(err)
	}
	if msa := doc.Messages[0].Segments[1]; string(doc.Bytes(msa.Field(2).Span)) != "SMALL-1" {
		t.Fatalf("second session was not acknowledged: %q", response)
	}
	_, _ = io.Copy(io.Discard, reader)
	if err := command.Wait(); err != nil {
		t.Fatalf("collect: %v %s", err, diagnostic.String())
	}
	collected, err := bundle.Open(casePath)
	if err != nil {
		t.Fatal(err)
	}
	record := collected.Collection
	if len(collected.Manifest.Sources) != 2 || len(record.Sessions) != 2 || record.Sessions[1].SourceID != "s0002" {
		t.Fatalf("successive sessions were not retained separately: %+v", record.Sessions)
	}
	if len(record.Received) != 1 || record.Received[0].OccurrenceID != "s0002-e000001" || record.Received[0].SessionID != "c0002" {
		t.Fatalf("an oversized frame was claimed as received: %+v", record.Received)
	}
	prefix, err := collected.Raw("s0001-e000001")
	if err != nil || !bytes.HasPrefix(mllp.Frame([]byte(collectADT)), prefix) || len(prefix) == 0 {
		t.Fatal("the oversized frame's consumed prefix was discarded")
	}
}

const collectEnhancedPolicy = `{"schema":"readmit-receiver-policy/v2","name":"enhanced-sink","source_label":"downstream-test-endpoint",` +
	`"acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},` +
	`"accepted_message_types":{"operator":"any-message-type","values":[]},` +
	`"enhanced_acknowledgement":{"operator":"enhanced-mode-fixed-codes","accept_code":"CA","application_code":"AA",` +
	`"application_delivery":"separate-endpoint","application_endpoint":"ENDPOINT","approved_transport":false}}`

const collectEnhancedADT = "MSH|^~\\&|SENDER|FACILITY|READMIT|COLLECT|20260101120000||ADT^A01|ENHANCED-001|P|2.5.1|||AL|AL\rPID|1||SYNTH-002\r"

func stageCode(t *testing.T, raw []byte) (code, acknowledged string) {
	t.Helper()
	doc, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
	if err != nil {
		t.Fatalf("acknowledgement is not readable HL7: %v", err)
	}
	segment := doc.Messages[0].Segments[1]
	return string(doc.Bytes(segment.Field(1).Span)), string(doc.Bytes(segment.Field(2).Span))
}

// The commit acknowledgement comes back on the connection that delivered the
// message; the application acknowledgement arrives at a separately configured
// endpoint. Neither is evidence that an application processed anything.
func TestCollectExecutableSplitsAcknowledgementStagesAcrossEndpoints(t *testing.T) {
	dir := t.TempDir()
	casePath := filepath.Join(dir, "collected")
	sink, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	delivered := make(chan []byte, 1)
	go func() {
		conn, err := sink.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
		frames, _ := mllp.NewReader(conn, 1<<20)
		if raw, err := frames.ReadFrame(); err == nil {
			delivered <- raw
		}
	}()
	declared := strings.Replace(collectEnhancedPolicy, "ENDPOINT", sink.Addr().String(), 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "collect", "--address", "127.0.0.1:0", "--policy", policyFile(t, dir, declared),
		"--output", casePath, "--max-messages", "1", "--idle-timeout", "2s", "--application-ack-timeout", "3s")
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	reader := bufio.NewReader(stdout)
	ready, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(ready, "Listening: ") {
		t.Fatalf("collector not ready: %q %v", ready, err)
	}
	conn, err := net.DialTimeout("tcp", strings.TrimSpace(strings.TrimPrefix(ready, "Listening: ")), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	if _, err := conn.Write(mllp.Frame([]byte(collectEnhancedADT))); err != nil {
		t.Fatal(err)
	}
	frames, _ := mllp.NewReader(conn, 1<<20)
	accept, err := frames.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if code, acknowledged := stageCode(t, accept); code != "CA" || acknowledged != "ENHANCED-001" {
		t.Fatalf("the receiving connection did not get a correlated commit acknowledgement: %s %s", code, acknowledged)
	}
	var application []byte
	select {
	case application = <-delivered:
	case <-time.After(8 * time.Second):
		t.Fatal("the separate application endpoint received nothing")
	}
	if code, acknowledged := stageCode(t, application); code != "AA" || acknowledged != "ENHANCED-001" {
		t.Fatalf("the separate endpoint did not get a correlated application acknowledgement: %s %s", code, acknowledged)
	}
	remaining, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("collect: %v %s", err, diagnostic.String())
	}
	if diagnostic.Len() != 0 {
		t.Fatalf("unexpected diagnostic: %s", diagnostic.String())
	}
	output := ready + string(remaining)
	for _, want := range []string{
		"Enhanced acknowledgement: enhanced-mode-fixed-codes CA AA separate-endpoint",
		"Application processing: none", "Collection: readmit-collection/v2",
		"Accept acknowledgements: 1", "Application acknowledgements: 1",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("missing summary %q in %s", want, output)
		}
	}
	for _, private := range []string{"ENHANCED-001", "SYNTH-002", dir} {
		if strings.Contains(output, private) {
			t.Errorf("default output disclosed %q", private)
		}
	}
	collected, err := bundle.Open(casePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := collected.Collection.Received[0]
	if entry.Mode != collection.EnhancedMode || entry.Accept.Code != collection.CommitAcceptCode || entry.Accept.Destination != collection.SameConnection {
		t.Fatalf("the commit stage was not recorded as itself: %+v", entry)
	}
	if entry.Application.Code != collection.AcceptCode || entry.Application.Destination != collection.SeparateEndpoint {
		t.Fatalf("the separate application delivery was not recorded: %+v", entry.Application)
	}
	if collected.Collection.ApplicationProcessing != collection.NoApplicationProcessing {
		t.Fatal("an enhanced exchange claimed application processing")
	}
}
