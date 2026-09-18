package receiver_test

import (
	"bytes"
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/receiver"
)

// A plain ADT the SIU fixture profile would refuse, in original ACK mode.
const plainADT = "MSH|^~\\&|SENDER|FACILITY|READMIT|COLLECT|20260101120000||ADT^A01|COLLECT-001|P|2.5.1\rPID|1||SYNTH-001\r"

const anyTypePolicy = `{"schema":"readmit-receiver-policy/v1","name":"downstream-sink","source_label":"downstream-test-endpoint","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]}}`

func policy(t *testing.T, data string) collection.Policy {
	t.Helper()
	value, err := collection.DecodePolicy([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type collecting struct {
	address string
	output  string
	cancel  context.CancelFunc
	done    chan struct{}
	err     error
}

func collect(t *testing.T, declared string, count int) *collecting {
	t.Helper()
	config := receiver.CollectorConfig{Policy: policy(t, declared), OutputPath: filepath.Join(t.TempDir(), "collected"), MaxMessages: count, MaxFrameBytes: 1 << 20, IdleTimeout: 2 * time.Second}
	c, err := receiver.NewCollector(config)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &collecting{address: listener.Addr().String(), output: config.OutputPath, cancel: cancel, done: make(chan struct{})}
	go func() { defer close(h.done); _, h.err = c.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.done:
		case <-time.After(10 * time.Second):
			t.Error("collector did not terminate")
		}
	})
	return h
}

func (h *collecting) dial(t *testing.T) (net.Conn, *mllp.Reader) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", h.address, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	reader, _ := mllp.NewReader(conn, 1<<20)
	return conn, reader
}

func (h *collecting) finished(t *testing.T) *bundle.Bundle {
	t.Helper()
	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
		t.Fatal("collector did not finalize")
	}
	if h.err != nil {
		t.Fatal(h.err)
	}
	reopened, err := bundle.Open(h.output)
	if err != nil {
		t.Fatal(err)
	}
	return reopened
}

func TestCollectorAcceptsNonFixtureTrafficAndLabelsItsSource(t *testing.T) {
	h := collect(t, anyTypePolicy, 2)
	conn, reader := h.dial(t)
	adt := mllp.Frame([]byte(plainADT))
	siu := mllp.Frame(fixture(t, "listen-s12.hl7"))
	send(t, conn, adt)
	ack(t, reader, "AA", "COLLECT-001")
	send(t, conn, siu)
	ack(t, reader, "AA", "LISTEN-BOOK")
	collected := h.finished(t)
	if collected.Manifest.Schema != bundle.CollectedSchema || collected.Collection == nil {
		t.Fatalf("collector did not write collected evidence: %s", collected.Manifest.Schema)
	}
	record := collected.Collection
	if len(record.Sessions) != 1 || record.Sessions[0].Label != "downstream-test-endpoint" || record.Sessions[0].SourceID != "s0001" {
		t.Fatalf("collected source is not explicitly labelled: %+v", record.Sessions)
	}
	if record.ApplicationProcessing != collection.NoApplicationProcessing {
		t.Fatal("collector claimed application processing")
	}
	if len(record.Received) != 2 || record.Received[0].ControlID != "COLLECT-001" || record.Received[1].ControlID != "LISTEN-BOOK" {
		t.Fatalf("collected receipts do not name each message: %+v", record.Received)
	}
	for _, received := range record.Received {
		if received.Acknowledgement != "AA" || received.Reason != "" {
			t.Fatalf("unexpected receipt: %+v", received)
		}
	}
	raw, err := collected.Raw("s0001-e000001")
	if err != nil || !bytes.Equal(raw, adt) {
		t.Fatal("collector did not retain the exact received bytes")
	}
	if raw, err := collected.Raw("s0001-e000003"); err != nil || !bytes.Equal(raw, siu) {
		t.Fatal("collector did not retain the exact second message")
	}
}

func TestCollectorRejectsMessageTypesOutsideTheDeclaredPolicy(t *testing.T) {
	declared := strings.Replace(anyTypePolicy, `"operator":"any-message-type","values":[]`, `"operator":"message-type-in","values":["ADT^A01"]`, 1)
	h := collect(t, declared, 2)
	conn, reader := h.dial(t)
	send(t, conn, mllp.Frame([]byte(plainADT)))
	ack(t, reader, "AA", "COLLECT-001")
	send(t, conn, mllp.Frame(fixture(t, "listen-s12.hl7")))
	ack(t, reader, "AR", "LISTEN-BOOK")
	record := h.finished(t).Collection
	if len(record.Received) != 2 || record.Received[1].Acknowledgement != "AR" || record.Received[1].Reason == "" {
		t.Fatalf("policy rejection is not explicit: %+v", record.Received)
	}
}

// The declared code is the one configurable dimension of the policy: it must
// reach MSA-1 on the wire and the receipt that records it.
func TestCollectorReturnsTheDeclaredAcknowledgementCode(t *testing.T) {
	for _, code := range []string{"AA", "AE", "AR"} {
		t.Run(code, func(t *testing.T) {
			h := collect(t, strings.Replace(anyTypePolicy, `"code":"AA"`, `"code":"`+code+`"`, 1), 1)
			conn, reader := h.dial(t)
			send(t, conn, mllp.Frame([]byte(plainADT)))
			ack(t, reader, code, "COLLECT-001")
			record := h.finished(t).Collection
			if len(record.Received) != 1 || record.Received[0].Acknowledgement != code || record.Received[0].Reason != "" {
				t.Fatalf("declared code %s did not reach the receipt: %+v", code, record.Received)
			}
			if record.Policy.Acknowledgement.Code != code {
				t.Fatalf("collected record lost the policy that produced %s", code)
			}
		})
	}
}

func TestCollectorRefusesEnhancedAcknowledgementModeExplicitly(t *testing.T) {
	h := collect(t, anyTypePolicy, 1)
	conn, reader := h.dial(t)
	// The committed ADT fixture declares MSH-15 and MSH-16 acceptance modes.
	send(t, conn, mllp.Frame(fixture(t, "adt-cr.hl7")))
	ack(t, reader, "AR", "CASE-001")
	record := h.finished(t).Collection
	if len(record.Received) != 1 || !strings.Contains(record.Received[0].Reason, "enhanced") {
		t.Fatalf("enhanced acknowledgement mode was not refused explicitly: %+v", record.Received)
	}
}

func TestCollectorAcknowledgesWithTheSenderDeclaredDelimiters(t *testing.T) {
	h := collect(t, anyTypePolicy, 1)
	conn, reader := h.dial(t)
	send(t, conn, mllp.Frame(fixture(t, "custom-delimiters.hl7")))
	raw, err := reader.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
	if err != nil {
		t.Fatalf("acknowledgement is not parseable with the sender's delimiters: %v", err)
	}
	message := doc.Messages[0]
	if message.Delimiters.Field != '*' || message.Delimiters.Component != '$' {
		t.Fatalf("acknowledgement did not reuse the declared delimiters: %q", raw)
	}
	msa := message.Segments[1]
	if string(doc.Bytes(msa.Field(1).Span)) != "AA" || string(doc.Bytes(msa.Field(2).Span)) != "CASE-003" {
		t.Fatalf("acknowledgement lost its correlation: %q", raw)
	}
	record := h.finished(t).Collection
	if len(record.Received) != 1 || record.Received[0].ControlID != "CASE-003" {
		t.Fatalf("receipt lost the literal control ID: %+v", record.Received)
	}
}

func TestCollectorRetainsFramesItCannotAcknowledgeWithoutInventingAnOutcome(t *testing.T) {
	for name, payload := range map[string]string{
		"not HL7":           "definitely not a message",
		"absent control ID": "MSH|^~\\&|SENDER|FACILITY|READMIT|COLLECT|20260101120000||ADT^A01\rPID|1\r",
		"empty control ID":  "MSH|^~\\&|SENDER|FACILITY|READMIT|COLLECT|20260101120000||ADT^A01||P|2.5.1\rPID|1\r",
		"absent version":    "MSH|^~\\&|SENDER|FACILITY|READMIT|COLLECT|20260101120000||ADT^A01|NO-VERSION|P\rPID|1\r",
	} {
		t.Run(name, func(t *testing.T) {
			h := collect(t, anyTypePolicy, 1)
			conn, reader := h.dial(t)
			frame := mllp.Frame([]byte(payload))
			send(t, conn, frame)
			if _, err := reader.ReadFrame(); err == nil {
				t.Fatal("collector acknowledged a frame it could not read")
			}
			h.cancel()
			collected := h.finished(t)
			record := collected.Collection
			if len(record.Received) != 1 || record.Received[0].Acknowledgement != collection.NotAcknowledged || record.Received[0].Reason == "" {
				t.Fatalf("unacknowledged frame is not explicit: %+v", record.Received)
			}
			raw, err := collected.Raw("s0001-e000001")
			if err != nil || !bytes.Equal(raw, frame) {
				t.Fatal("collector discarded the unacknowledged evidence")
			}
		})
	}
}

func TestCollectorFinalizesCancelledAndEmptySessions(t *testing.T) {
	h := collect(t, anyTypePolicy, 0)
	conn, reader := h.dial(t)
	send(t, conn, mllp.Frame([]byte(plainADT)))
	ack(t, reader, "AA", "COLLECT-001")
	h.cancel()
	collected := h.finished(t)
	if len(collected.Collection.Received) != 1 || len(collected.Manifest.Sources) != 1 {
		t.Fatalf("cancellation discarded retained evidence: %+v", collected.Manifest.Sources)
	}

	empty := collect(t, anyTypePolicy, 0)
	empty.cancel()
	idle := empty.finished(t)
	if idle.Manifest.Schema != bundle.CollectedSchema || len(idle.Manifest.Sources) != 0 || len(idle.Collection.Received) != 0 || len(idle.Collection.Sessions) != 0 {
		t.Fatalf("empty session invented evidence: %+v", idle.Manifest)
	}
}

func TestCollectorRefusesUnusableConfiguration(t *testing.T) {
	valid := policy(t, anyTypePolicy)
	existing := t.TempDir()
	for name, config := range map[string]receiver.CollectorConfig{
		"invalid policy":       {Policy: collection.Policy{}, OutputPath: filepath.Join(t.TempDir(), "case"), MaxFrameBytes: 1 << 20, IdleTimeout: time.Second},
		"no destination":       {Policy: valid, MaxFrameBytes: 1 << 20, IdleTimeout: time.Second},
		"existing destination": {Policy: valid, OutputPath: existing, MaxFrameBytes: 1 << 20, IdleTimeout: time.Second},
		"unbounded frames":     {Policy: valid, OutputPath: filepath.Join(t.TempDir(), "case"), MaxFrameBytes: 0, IdleTimeout: time.Second},
		"unbounded idle":       {Policy: valid, OutputPath: filepath.Join(t.TempDir(), "case"), MaxFrameBytes: 1 << 20},
		"too many messages":    {Policy: valid, OutputPath: filepath.Join(t.TempDir(), "case"), MaxFrameBytes: 1 << 20, IdleTimeout: time.Second, MaxMessages: receiver.MaxMessages + 1},
	} {
		if _, err := receiver.NewCollector(config); err == nil {
			t.Errorf("collector accepted %s", name)
		}
	}
}

func TestCollectorServesOneSessionOnce(t *testing.T) {
	config := receiver.CollectorConfig{Policy: policy(t, anyTypePolicy), OutputPath: filepath.Join(t.TempDir(), "collected"), MaxFrameBytes: 1 << 20, IdleTimeout: time.Second}
	c, err := receiver.NewCollector(config)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Serve(ctx, listener); err != nil {
		t.Fatal(err)
	}
	second, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := c.Serve(ctx, second); err == nil {
		t.Fatal("collector served a second session into the same destination")
	}
}
