package receiver_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
)

type running struct {
	address string
	config  receiver.Config
	cancel  context.CancelFunc
	done    chan struct{}
	result  *bundle.Bundle
	err     error
}

func start(t *testing.T, mode observation.Mode, count, maxFrame int, idle time.Duration) *running {
	t.Helper()
	dir := t.TempDir()
	config := receiver.Config{Mode: mode, OutputPath: filepath.Join(dir, "case"), ObservationPath: filepath.Join(dir, "observation.json"), MaxMessages: count, MaxFrameBytes: maxFrame, IdleTimeout: idle}
	r, err := receiver.New(config)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &running{address: listener.Addr().String(), config: config, cancel: cancel, done: make(chan struct{})}
	go func() { defer close(h.done); h.result, h.err = r.Serve(ctx, listener) }()
	t.Cleanup(func() { cancel(); h.await(t) })
	return h
}

func (h *running) await(t *testing.T) {
	t.Helper()
	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
		t.Fatal("receiver did not terminate")
	}
}

func (h *running) connect(t *testing.T) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", h.address, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func send(t *testing.T, conn net.Conn, data []byte) {
	t.Helper()
	if _, err := conn.Write(data); err != nil {
		t.Fatal(err)
	}
}

func ack(t *testing.T, reader *mllp.Reader, code, control string) []byte {
	t.Helper()
	raw, err := reader.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
	if err != nil {
		t.Fatal(err)
	}
	msa := doc.Messages[0].Segments[1]
	if string(doc.Bytes(msa.Field(1).Span)) != code || string(doc.Bytes(msa.Field(2).Span)) != control {
		t.Fatalf("wrong ACK code or correlation: %q", raw)
	}
	return raw
}

func TestBothModesMatchIndependentLedgerAndRecordExactWireEvidence(t *testing.T) {
	for _, mode := range []observation.Mode{observation.Fixed, observation.Defective} {
		t.Run(string(mode), func(t *testing.T) {
			h := start(t, mode, 2, 1<<20, time.Second)
			initial, err := observation.Read(h.config.ObservationPath)
			if err != nil {
				t.Fatal(err)
			}
			conn := h.connect(t)
			reader, _ := mllp.NewReader(conn, 1<<20)
			book := mllp.Frame(fixture(t, "listen-s12.hl7"))
			move := mllp.Frame(fixture(t, "listen-s13.hl7"))
			if mode == observation.Fixed {
				for _, part := range [][]byte{book[:1], book[1:19], book[19:]} {
					send(t, conn, part)
				}
			} else {
				send(t, conn, append(bytes.Clone(book), move...))
			}
			firstACK := ack(t, reader, "AA", "LISTEN-BOOK")
			observed, err := observation.Read(h.config.ObservationPath)
			if err != nil || !observed.Consistent || len(observed.Processed) < 1 || observed.SessionID != initial.SessionID {
				t.Fatalf("ACK preceded committed observation: %+v %v", observed, err)
			}
			if mode == observation.Fixed {
				send(t, conn, move)
			}
			secondACK := ack(t, reader, "AA", "LISTEN-MOVE")
			h.await(t)
			if h.err != nil {
				t.Fatal(h.err)
			}
			observed, err = observation.Read(h.config.ObservationPath)
			if err != nil {
				t.Fatal(err)
			}
			var want []observation.Record
			if err := json.Unmarshal(fixture(t, "listen-"+string(mode)+".json"), &want, json.RejectUnknownMembers(true)); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(observed.Records, want) {
				t.Fatalf("ledger differs from hand-authored expected: got=%+v want=%+v", observed.Records, want)
			}
			if !reflect.DeepEqual(observed.Processed, []observation.Occurrence{{OccurrenceID: "s0001-e000001", ControlID: "LISTEN-BOOK"}, {OccurrenceID: "s0001-e000003", ControlID: "LISTEN-MOVE"}}) {
				t.Fatalf("wrong ordered occurrences: %+v", observed.Processed)
			}
			b, err := bundle.Open(h.config.OutputPath)
			if err != nil {
				t.Fatal(err)
			}
			if b.Manifest.Schema != bundle.RecordedSchema || b.Manifest.Provenance.Mode != bundle.Recorded || !reflect.DeepEqual(*b.Observation, observed) || len(b.Correlations) != 2 {
				t.Fatal("recorded bundle lost observation or correlation")
			}
			for i, want := range [][]byte{book, firstACK, move, secondACK} {
				event := b.Events[i]
				raw, _ := b.Raw(event.ID)
				if !bytes.Equal(raw, want) || event.ObservedAt == nil || event.ImportedAt != nil {
					t.Fatal("recorded bytes or observed times changed")
				}
			}
			for _, link := range b.Correlations {
				if link.Kind != bundle.Matched {
					t.Fatal("ACK lost MSA-2 correlation")
				}
			}
		})
	}
}

func TestEnhancedModeIsExplicitlyRejectedWithoutLedgerMutation(t *testing.T) {
	h := start(t, observation.Fixed, 1, 1<<20, time.Second)
	conn := h.connect(t)
	reader, _ := mllp.NewReader(conn, 1<<20)
	raw := bytes.Replace(fixture(t, "listen-s12.hl7"), []byte("|2.5.1\r"), []byte("|2.5.1|||AL|NE\r"), 1)
	send(t, conn, mllp.Frame(raw))
	response := ack(t, reader, "AR", "LISTEN-BOOK")
	if !bytes.Contains(response, []byte(`MSH-15="AL"`)) || !bytes.Contains(response, []byte(`MSH-16="NE"`)) {
		t.Fatalf("rejection does not name request fields: %q", response)
	}
	h.await(t)
	if h.err != nil {
		t.Fatal(h.err)
	}
	observed, err := observation.Read(h.config.ObservationPath)
	if err != nil || len(observed.Records) != 0 || len(observed.Processed) != 1 || !observed.Consistent {
		t.Fatal("rejected request changed workflow state")
	}
}

func TestIdleOversizeAndMalformedConnectionsCloseAndRetainEvidence(t *testing.T) {
	book := fixture(t, "listen-s12.hl7")
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"idle", nil},
		{"partial", []byte("\x0bMSH|partial")},
		{"oversize", mllp.Frame(bytes.Repeat([]byte{'X'}, len(book)+1))},
		{"bad-framing", []byte("\x0bMSH\x1cX")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := start(t, observation.Fixed, 1, len(book), 60*time.Millisecond)
			conn := h.connect(t)
			if len(tc.data) > 0 {
				send(t, conn, tc.data)
			}
			one := make([]byte, 1)
			if n, err := conn.Read(one); n != 0 || err == nil {
				t.Fatalf("bad connection was not closed: n=%d err=%v", n, err)
			} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("client deadline expired before receiver closed idle connection")
			}
			observed, err := observation.Read(h.config.ObservationPath)
			if err != nil || len(observed.Processed) != 0 || len(observed.Records) != 0 {
				t.Fatal("failed frame mutated observation")
			}
			conn.Close()
			good := h.connect(t)
			reader, _ := mllp.NewReader(good, 1<<20)
			send(t, good, mllp.Frame(book))
			ack(t, reader, "AA", "LISTEN-BOOK")
			h.await(t)
			if h.err != nil {
				t.Fatal(h.err)
			}
			b, err := bundle.Open(h.config.OutputPath)
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.data) > 0 {
				got, _ := b.Raw(b.Events[0].ID)
				if !bytes.HasPrefix(tc.data, got) || len(got) < min(len(tc.data), len(book)+2) {
					t.Fatalf("rejected input changed: got %q want %q", got, tc.data)
				}
			}
		})
	}
}

func TestCancellationFinalizesEmptyAndPartiallyReadSessions(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "accept", true: "read"}[partial], func(t *testing.T) {
			h := start(t, observation.Fixed, 0, 1<<20, time.Hour)
			if partial {
				conn := h.connect(t)
				send(t, conn, []byte("\x0bMSH|partial"))
				// The framing call may be before or after the underlying read when
				// cancellation arrives; both paths must close without a long wait.
			}
			h.cancel()
			h.await(t)
			if h.err != nil {
				t.Fatal(h.err)
			}
			b, err := bundle.Open(h.config.OutputPath)
			if err != nil {
				t.Fatal(err)
			}
			if b.Observation == nil || !b.Observation.Consistent || len(b.Observation.Processed) != 0 {
				t.Fatal("cancelled session has fabricated processing")
			}
		})
	}
}

func TestObservationWriteFailureDoesNotSendAA(t *testing.T) {
	h := start(t, observation.Fixed, 1, 1<<20, time.Second)
	if err := os.Remove(h.config.ObservationPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(h.config.ObservationPath, 0700); err != nil {
		t.Fatal(err)
	}
	conn := h.connect(t)
	send(t, conn, mllp.Frame(fixture(t, "listen-s12.hl7")))
	response := make([]byte, 4096)
	n, _ := conn.Read(response)
	if bytes.Contains(response[:n], []byte("MSA|AA")) {
		t.Fatal("AA sent without persistent observation")
	}
	h.await(t)
	if h.err == nil {
		t.Fatal("observation failure was hidden")
	}
	b, err := bundle.Open(h.config.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if b.Observation.Consistent || len(b.Observation.Processed) != 0 || len(b.Events) != 1 {
		t.Fatal("failed session claims complete processing")
	}
}

func TestRescheduleUsesNamespacedNonUniqueFillerLookup(t *testing.T) {
	for _, tc := range []struct {
		name          string
		replace, with string
	}{
		{"other-namespace", "FILL-001^READMIT", "FILL-001^OTHER"},
		{"other-patient", "SYNTH-001^^^READMIT", "SYNTH-002^^^READMIT"},
		{"bad-time", "20260103110000+0000", "20261303110000+0000"},
		{"cancellation-unsupported", "SIU^S13", "SIU^S15"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := start(t, observation.Fixed, 2, 1<<20, time.Second)
			conn := h.connect(t)
			reader, _ := mllp.NewReader(conn, 1<<20)
			send(t, conn, mllp.Frame(fixture(t, "listen-s12.hl7")))
			ack(t, reader, "AA", "LISTEN-BOOK")
			move := strings.Replace(string(fixture(t, "listen-s13.hl7")), tc.replace, tc.with, 1)
			send(t, conn, mllp.Frame([]byte(move)))
			ack(t, reader, "AR", "LISTEN-MOVE")
			h.await(t)
			if h.err != nil {
				t.Fatal(h.err)
			}
			observed, _ := observation.Read(h.config.ObservationPath)
			if len(observed.Records) != 1 || observed.Records[0].AppointmentStart != "20260102100000+0000" {
				t.Fatal("rejected reschedule altered original record")
			}
		})
	}
}

// The transport is an independent failure fixture at the public net.Conn
// boundary: one coalesced read and a write that fails after actual prefix bytes.
type failedACKConnection struct {
	*bytes.Reader
	cancel context.CancelFunc
}

func (c failedACKConnection) Write(data []byte) (int, error) {
	c.cancel()
	return min(10, len(data)), io.ErrClosedPipe
}
func (c failedACKConnection) Close() error                     { return nil }
func (c failedACKConnection) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c failedACKConnection) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c failedACKConnection) SetDeadline(time.Time) error      { return nil }
func (c failedACKConnection) SetReadDeadline(time.Time) error  { return nil }
func (c failedACKConnection) SetWriteDeadline(time.Time) error { return nil }

type oneConnectionListener struct{ connection net.Conn }

func (l oneConnectionListener) Accept() (net.Conn, error) { return l.connection, nil }
func (l oneConnectionListener) Close() error              { return nil }
func (l oneConnectionListener) Addr() net.Addr            { return &net.TCPAddr{} }

func TestPartialACKAndUnprocessedReadAheadRemainHonestEvidence(t *testing.T) {
	dir := t.TempDir()
	config := receiver.Config{Mode: observation.Fixed, OutputPath: filepath.Join(dir, "case"), ObservationPath: filepath.Join(dir, "observation.json"), MaxFrameBytes: 1 << 20, IdleTimeout: time.Second}
	r, err := receiver.New(config)
	if err != nil {
		t.Fatal(err)
	}
	book := mllp.Frame(fixture(t, "listen-s12.hl7"))
	move := mllp.Frame(fixture(t, "listen-s13.hl7"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connection := failedACKConnection{Reader: bytes.NewReader(append(bytes.Clone(book), move...)), cancel: cancel}
	_, err = r.Serve(ctx, oneConnectionListener{connection: connection})
	if err != nil {
		t.Fatal(err)
	}
	b, err := bundle.Open(config.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Events) != 2 || b.Events[1].Kind != bundle.Unparsed || b.Events[1].Direction != bundle.Unknown {
		t.Fatalf("partial ACK was invented as complete evidence: %+v", b.Events)
	}
	suffix, _ := b.Raw(b.Events[1].ID)
	if len(suffix) != 10+len(move) || !bytes.HasSuffix(suffix, move) {
		t.Fatal("partial ACK or received read-ahead bytes were dropped")
	}
	if len(b.Observation.Processed) != 1 || len(b.Observation.Records) != 1 || b.Observation.Records[0].AppointmentStart != "20260102100000+0000" {
		t.Fatal("read-ahead reschedule was falsely claimed as processed")
	}
}
