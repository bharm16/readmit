package receiver_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/receiver"
)

// message builds one ADT with a distinct control ID, so a concurrent capture
// can be checked frame by frame rather than only by count.
func message(control string) []byte {
	return mllp.Frame([]byte(strings.Replace(plainADT, "COLLECT-001", control, 1)))
}

// serving starts one collector over the listener the caller supplies, so a test
// can capture over loopback, over TLS, or over a synchronous pipe.
func serving(t *testing.T, config receiver.CollectorConfig, listener net.Listener) *collecting {
	t.Helper()
	if config.Policy.Schema == "" {
		config.Policy = policy(t, anyTypePolicy)
	}
	if config.OutputPath == "" {
		config.OutputPath = filepath.Join(t.TempDir(), "collected")
	}
	if config.MaxFrameBytes == 0 {
		config.MaxFrameBytes = 1 << 20
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 2 * time.Second
	}
	if config.ApplicationTimeout == 0 {
		config.ApplicationTimeout = 2 * time.Second
	}
	c, err := receiver.NewCollector(config)
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &collecting{address: listener.Addr().String(), output: config.OutputPath, collector: c, cancel: cancel, done: make(chan struct{})}
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

func loopback(t *testing.T, config receiver.CollectorConfig) *collecting {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return serving(t, config, listener)
}

// exchange sends one frame and returns the acknowledgement's MSA-2, which is
// the sender's own control ID echoed back.
func exchange(conn net.Conn, reader *mllp.Reader, control string) (string, error) {
	if _, err := conn.Write(message(control)); err != nil {
		return "", err
	}
	frame, err := reader.ReadFrame()
	if err != nil {
		return "", err
	}
	return string(frame), nil
}

// TestCollectorServesConcurrentConnectionsAsDistinctSources is the multi-client
// contract: four peers are served at once, each becomes its own case source,
// and every frame is answered on the connection that carried it.
func TestCollectorServesConcurrentConnectionsAsDistinctSources(t *testing.T) {
	const peers = 4
	h := loopback(t, receiver.CollectorConfig{MaxMessages: peers, MaxConnections: peers})
	connections := make([]net.Conn, peers)
	readers := make([]*mllp.Reader, peers)
	for i := range connections {
		connections[i], readers[i] = h.dial(t)
	}
	answers := make([]string, peers)
	failures := make([]error, peers)
	var group sync.WaitGroup
	for i := range connections {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			answers[i], failures[i] = exchange(connections[i], readers[i], fmt.Sprintf("CONCURRENT-%03d", i))
		}(i)
	}
	group.Wait()
	for i, err := range failures {
		if err != nil {
			t.Fatalf("peer %d was not answered: %v", i, err)
		}
		if !strings.Contains(answers[i], fmt.Sprintf("CONCURRENT-%03d", i)) {
			t.Fatalf("peer %d received another peer's acknowledgement: %q", i, answers[i])
		}
	}
	for _, conn := range connections {
		conn.Close()
	}
	b := h.finished(t)
	record := b.Collection
	if record == nil || len(record.Sessions) != peers || len(record.Received) != peers {
		t.Fatalf("concurrent capture did not retain one session and one frame per peer: %+v", record)
	}
	if len(b.Manifest.Sources) != peers {
		t.Fatalf("concurrent capture retained %d sources", len(b.Manifest.Sources))
	}
	sources := make(map[string]string, peers)
	for _, session := range record.Sessions {
		sources[session.SessionID] = session.SourceID
	}
	seen := make(map[string]bool, peers)
	for _, received := range record.Received {
		source, known := sources[received.SessionID]
		if !known || !strings.HasPrefix(received.OccurrenceID, source+"-") {
			t.Fatalf("a frame was attributed to a connection that did not carry it: %+v", received)
		}
		if received.Application.Code != collection.AcceptCode {
			t.Fatalf("a concurrently captured frame was not acknowledged: %+v", received)
		}
		seen[received.ControlID] = true
	}
	for i := range connections {
		if !seen[fmt.Sprintf("CONCURRENT-%03d", i)] {
			t.Fatalf("peer %d's frame is missing from the record", i)
		}
	}
}

// TestCollectorConnectionLimitDefersRatherThanDropsAPeer is the backpressure
// contract: a peer beyond the declared limit is not accepted while the limit is
// spent, and its message is captured in full once a slot frees. Waiting is the
// whole point; a dropped message would be exactly the silent loss this refuses.
func TestCollectorConnectionLimitDefersRatherThanDropsAPeer(t *testing.T) {
	h := loopback(t, receiver.CollectorConfig{MaxMessages: 2, MaxConnections: 1})
	first, firstReader := h.dial(t)
	second, secondReader := h.dial(t)
	if _, err := second.Write(message("DEFERRED-001")); err != nil {
		t.Fatal(err)
	}
	if err := second.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := secondReader.ReadFrame(); err == nil {
		t.Fatal("a peer beyond the declared connection limit was served while the limit was spent")
	}
	answer, err := exchange(first, firstReader, "ADMITTED-001")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "ADMITTED-001") {
		t.Fatalf("the admitted peer was answered for another frame: %q", answer)
	}
	first.Close()
	if err := second.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	deferred, err := secondReader.ReadFrame()
	if err != nil {
		t.Fatalf("the deferred peer was never served: %v", err)
	}
	if !strings.Contains(string(deferred), "DEFERRED-001") {
		t.Fatalf("the deferred peer received the wrong acknowledgement: %q", deferred)
	}
	second.Close()
	record := h.finished(t).Collection
	if record == nil || len(record.Received) != 2 || len(record.Sessions) != 2 {
		t.Fatalf("a deferred peer's message was lost: %+v", record)
	}
}

// TestCollectorMessageLimitIsExactUnderConcurrency is the limit concurrency can
// break most quietly. Every peer offers more frames than the capture was asked
// for, all of them at once. A frame is counted when a connection claims a slot
// to read it rather than when it lands, so the declared limit is exactly what is
// captured however many peers were reading at the time.
func TestCollectorMessageLimitIsExactUnderConcurrency(t *testing.T) {
	const peers = 4
	h := loopback(t, receiver.CollectorConfig{MaxMessages: peers, MaxConnections: peers, IdleTimeout: time.Second})
	connections := make([]net.Conn, peers)
	for i := range connections {
		connections[i], _ = h.dial(t)
	}
	var group sync.WaitGroup
	for i := range connections {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			for offer := range 2 {
				if _, err := connections[i].Write(message(fmt.Sprintf("EXACT-%03d-%d", i, offer))); err != nil {
					return
				}
			}
		}(i)
	}
	group.Wait()
	for _, conn := range connections {
		conn.Close()
	}
	record := h.finished(t).Collection
	if record == nil || len(record.Received) != peers {
		t.Fatalf("a declared limit of %d frames captured %d", peers, len(record.Received))
	}
}

// TestCollectorSessionBudgetStopsAcceptingAndFinalizes is the task limit: the
// capture serves the declared number of connections, stops accepting, and
// finalizes on its own. The listener is a pipe so "the next peer was never
// accepted" is decided by this test rather than by whether a loopback port
// happened to stay unbound.
func TestCollectorSessionBudgetStopsAcceptingAndFinalizes(t *testing.T) {
	listener := newPipeListener()
	h := serving(t, receiver.CollectorConfig{MaxConnections: 1, MaxSessions: 1, IdleTimeout: time.Second}, listener)
	client, server := net.Pipe()
	listener.connections <- server
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader, _ := mllp.NewReader(client, 1<<20)
	if _, err := exchange(client, reader, "BUDGET-001"); err != nil {
		t.Fatal(err)
	}
	client.Close()
	extra, offered := net.Pipe()
	defer extra.Close()
	defer offered.Close()
	select {
	case listener.connections <- offered:
		// The channel holds one connection, so the send alone proves nothing;
		// the accept loop taking it would.
	case <-time.After(time.Second):
		t.Fatal("the pipe listener never freed its slot")
	}
	record := h.finished(t).Collection
	if record == nil || len(record.Sessions) != 1 || len(record.Received) != 1 {
		t.Fatalf("the session budget did not finalize one served connection: %+v", record)
	}
	// A pipe write completes only when the peer reads it, so a write that
	// cannot finish is the exact statement that nothing accepted this peer.
	if err := offered.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	sent, err := offered.Write(message("BEYOND-001"))
	if err == nil || sent != 0 {
		t.Fatalf("a peer beyond the declared connection budget was served %d bytes", sent)
	}
}

// pipeListener hands the collector one synchronous connection. net.Pipe has no
// kernel buffer, so a write completes only when the collector actually reads
// it: that is what makes "these bytes were never accepted" an exact assertion
// rather than a guess about socket buffer sizes.
type pipeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	once        sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{connections: make(chan net.Conn, 1), closed: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case connection := <-l.connections:
		return connection, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddress{} }

type pipeAddress struct{}

func (pipeAddress) Network() string { return "pipe" }
func (pipeAddress) String() string  { return "pipe" }

// TestCollectorCaptureQuotaRefusesToReadWhatItCannotRetain is the quota
// contract. Reaching the declared quota stops the capture before the next frame
// is read, so the unread bytes stay in the sender's socket instead of being
// consumed by a receiver that would not keep them.
func TestCollectorCaptureQuotaRefusesToReadWhatItCannotRetain(t *testing.T) {
	const frame = 4096
	const reserve = 16 << 10
	listener := newPipeListener()
	h := serving(t, receiver.CollectorConfig{MaxFrameBytes: frame, MaxCaptureBytes: frame + reserve + 64, MaxConnections: 1}, listener)
	client, server := net.Pipe()
	listener.connections <- server
	reader, _ := mllp.NewReader(client, 1<<20)
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	answer, err := exchange(client, reader, "QUOTA-001")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "QUOTA-001") {
		t.Fatalf("the first frame was not acknowledged: %q", answer)
	}
	// net.Pipe reports a peer that has already closed from SetWriteDeadline as
	// well as from Write. The collector closing after its quota stop is the
	// outcome this test asserts next, so that report is not a failure here.
	if err := client.SetWriteDeadline(time.Now().Add(time.Second)); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	sent, err := client.Write(message("QUOTA-002"))
	if err == nil {
		t.Fatal("the collector read a frame its declared quota could not retain")
	}
	if sent != 0 {
		t.Fatalf("the collector accepted %d bytes beyond its declared quota", sent)
	}
	client.Close()
	record := h.finished(t).Collection
	if record == nil || len(record.Received) != 1 || record.Received[0].ControlID != "QUOTA-001" {
		t.Fatalf("the quota stop did not retain exactly the frames it accepted: %+v", record)
	}
}

// TestCollectorRefusesUnusableCapacity keeps every declared bound explicit. An
// unusable capacity is a startup refusal, never a quietly adjusted default.
func TestCollectorRefusesUnusableCapacity(t *testing.T) {
	base := func() receiver.CollectorConfig {
		return receiver.CollectorConfig{Policy: policy(t, anyTypePolicy), OutputPath: filepath.Join(t.TempDir(), "collected"),
			MaxFrameBytes: 1 << 20, IdleTimeout: time.Second, ApplicationTimeout: time.Second}
	}
	for name, adjust := range map[string]func(*receiver.CollectorConfig){
		"negative concurrency":     func(c *receiver.CollectorConfig) { c.MaxConnections = -1 },
		"concurrency beyond bound": func(c *receiver.CollectorConfig) { c.MaxConnections = receiver.MaxConnections + 1 },
		"session budget too large": func(c *receiver.CollectorConfig) { c.MaxSessions = bundle.MaxSources + 1 },
		"negative session budget":  func(c *receiver.CollectorConfig) { c.MaxSessions = -1 },
		"quota beyond evidence":    func(c *receiver.CollectorConfig) { c.MaxCaptureBytes = bundle.MaxEvidenceBytes + 1 },
		"quota below one frame":    func(c *receiver.CollectorConfig) { c.MaxCaptureBytes = 1 << 20 },
	} {
		config := base()
		adjust(&config)
		if _, err := receiver.NewCollector(config); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

// TestCollectorRefusesConcurrentFaultSimulation states an unsupported
// combination by name. A fault plan selects messages by session ordinal, and
// concurrent connections decide that ordinal by arrival, so the plan would name
// a different message on every run.
func TestCollectorRefusesConcurrentFaultSimulation(t *testing.T) {
	declared := faultPolicy("127.0.0.1:2575", "reject", "application", 0)
	config := receiver.CollectorConfig{Policy: policy(t, declared), OutputPath: filepath.Join(t.TempDir(), "collected"),
		MaxFrameBytes: 1 << 20, IdleTimeout: time.Second, ApplicationTimeout: time.Second, MaxConnections: 2}
	if _, err := receiver.NewCollector(config); err == nil {
		t.Fatal("a fault policy was accepted alongside concurrent connections")
	}
	config.MaxConnections = 1
	if _, err := receiver.NewCollector(config); err != nil {
		t.Fatalf("a fault policy was refused with one connection at a time: %v", err)
	}
}

// TestCollectorJournalRetainsFramesBeforeAcknowledging is the durability
// contract: the exact bytes of every frame are synced before the frame is
// answered, so an abrupt kill leaves the message retained rather than lost.
func TestCollectorJournalRetainsFramesBeforeAcknowledging(t *testing.T) {
	directory := t.TempDir()
	journal := filepath.Join(directory, "journal")
	h := loopback(t, receiver.CollectorConfig{OutputPath: filepath.Join(directory, "collected"), JournalPath: journal, MaxMessages: 1, MaxConnections: 2})
	conn, reader := h.dial(t)
	if _, err := exchange(conn, reader, "JOURNAL-001"); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	h.finished(t)
	summary := h.collector.Journal()
	if summary == nil {
		t.Fatal("a configured journal produced no summary")
	}
	if summary.State != capturejournal.Finalized || summary.Received != 1 || summary.Acknowledged != 1 || summary.Uncertain != 0 || summary.DeliveryUncertain {
		t.Fatalf("a controlled capture was not finalized cleanly: %+v", summary)
	}
	if summary.ExitCode() != 0 {
		t.Fatalf("a finalized capture reported exit code %d", summary.ExitCode())
	}
	retained, err := os.ReadFile(filepath.Join(journal, "received", "s0001-e000001.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(retained) != string(message("JOURNAL-001")) {
		t.Fatalf("the journal retained %q rather than the frame it answered", retained)
	}
	reopened, err := capturejournal.Open(journal)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.State != capturejournal.Finalized || reopened.Received != 1 || reopened.Acknowledged != 1 {
		t.Fatalf("reopening the journal disagreed with the capture: %+v", reopened)
	}
}

// TestCollectorJournalReportsCancellationWithoutClaimingCompletion keeps a
// stopped capture honest: cancellation is reported as cancellation, and is
// never a finalized capture.
func TestCollectorJournalReportsCancellationWithoutClaimingCompletion(t *testing.T) {
	directory := t.TempDir()
	journal := filepath.Join(directory, "journal")
	h := loopback(t, receiver.CollectorConfig{OutputPath: filepath.Join(directory, "collected"), JournalPath: journal, MaxConnections: 1, IdleTimeout: time.Second})
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
		t.Fatal("collector did not finalize after cancellation")
	}
	summary := h.collector.Journal()
	if summary == nil || summary.State != durablerun.Cancelled || summary.StopReason != durablerun.Cancelled {
		t.Fatalf("a cancelled capture did not report cancellation: %+v", summary)
	}
	if summary.ExitCode() == 0 {
		t.Fatal("a cancelled capture reported the finalized exit code")
	}
	reopened, err := capturejournal.Open(journal)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.State != durablerun.Cancelled || reopened.Received != 0 {
		t.Fatalf("reopening a cancelled capture disagreed with it: %+v", reopened)
	}
}

// TestCollectorJournalDestinationMustBeNew keeps the journal inside the one
// path policy: it cannot land on an existing artifact or inside retained case
// evidence.
func TestCollectorJournalDestinationMustBeNew(t *testing.T) {
	directory := t.TempDir()
	existing := filepath.Join(directory, "journal")
	if err := os.Mkdir(existing, 0700); err != nil {
		t.Fatal(err)
	}
	config := receiver.CollectorConfig{Policy: policy(t, anyTypePolicy), OutputPath: filepath.Join(directory, "collected"),
		JournalPath: existing, MaxFrameBytes: 1 << 20, IdleTimeout: time.Second, ApplicationTimeout: time.Second}
	if _, err := receiver.NewCollector(config); err == nil {
		t.Fatal("an existing directory was accepted as a capture journal")
	}
	first := loopback(t, receiver.CollectorConfig{OutputPath: filepath.Join(directory, "case"), JournalPath: filepath.Join(directory, "first"), MaxConnections: 1})
	first.cancel()
	first.finished(t)
	config.JournalPath = filepath.Join(directory, "case", "inside")
	if _, err := receiver.NewCollector(config); err == nil {
		t.Fatal("a capture journal was accepted inside retained case evidence")
	}
}

// TestCollectorRefusesAJournalThatWouldBecomeTheCase keeps the two destinations
// apart. Sealing the case over the journal directory would fail only at
// finalization, after peers had already been acknowledged, and would take the
// whole capture with it.
func TestCollectorRefusesAJournalThatWouldBecomeTheCase(t *testing.T) {
	directory := t.TempDir()
	shared := filepath.Join(directory, "collected")
	config := receiver.CollectorConfig{Policy: policy(t, anyTypePolicy), OutputPath: shared, JournalPath: shared,
		MaxFrameBytes: 1 << 20, IdleTimeout: time.Second, ApplicationTimeout: time.Second}
	if _, err := receiver.NewCollector(config); err == nil {
		t.Fatal("one destination was accepted as both the case and its journal")
	}
	if _, err := os.Lstat(shared); !os.IsNotExist(err) {
		t.Fatalf("a refused capture left a destination behind: %v", err)
	}
	config.JournalPath = filepath.Join(directory, "journal")
	if _, err := receiver.NewCollector(config); err != nil {
		t.Fatalf("distinct destinations were refused: %v", err)
	}
}

// TestCollectorServedOnceRejectsASecondSession keeps the one-session-per-
// collector rule while connections inside that session may run concurrently.
func TestCollectorServedOnceRejectsASecondSession(t *testing.T) {
	config := receiver.CollectorConfig{Policy: policy(t, anyTypePolicy), OutputPath: filepath.Join(t.TempDir(), "collected"),
		MaxFrameBytes: 1 << 20, IdleTimeout: time.Second, ApplicationTimeout: time.Second, MaxConnections: 4}
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
	if _, err := c.Serve(context.Background(), second); err == nil {
		t.Fatal("a collector served a second session")
	}
}

// TestCollectorNeverClaimsMoreSourcesThanTheCaseHolds is the limit a concurrent
// accept loop can break in a way a sequential one could not: several
// connections in flight may each still become a case source, so the room for
// them is reserved before they are served. Reaching the limit refuses the next
// peer and still seals everything already captured, rather than discovering at
// finalization that the case cannot hold what was accepted.
func TestCollectorNeverClaimsMoreSourcesThanTheCaseHolds(t *testing.T) {
	listener := newPipeListener()
	h := serving(t, receiver.CollectorConfig{MaxConnections: 8, IdleTimeout: time.Second}, listener)
offering:
	for i := range bundle.MaxSources + 8 {
		client, server := net.Pipe()
		select {
		case listener.connections <- server:
		case <-h.done:
			server.Close()
			client.Close()
			break offering
		}
		go func() {
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err := client.Write(message(fmt.Sprintf("SOURCE-%03d", i))); err != nil {
				return
			}
			reader, _ := mllp.NewReader(client, 1<<20)
			_, _ = reader.ReadFrame()
		}()
	}
	select {
	case <-h.done:
	case <-time.After(30 * time.Second):
		t.Fatal("collector did not stop at its source limit")
	}
	if h.err == nil || !strings.Contains(h.err.Error(), "source limit") {
		t.Fatalf("the source limit was not reported: %v", h.err)
	}
	b, err := bundle.Open(h.output)
	if err != nil {
		t.Fatalf("a session that reached its source limit sealed nothing: %v", err)
	}
	if len(b.Manifest.Sources) > bundle.MaxSources {
		t.Fatalf("a concurrent capture claimed %d sources", len(b.Manifest.Sources))
	}
	if record := b.Collection; record == nil || len(record.Sessions) != len(b.Manifest.Sources) {
		t.Fatalf("the sealed record does not describe every retained source: %+v", record)
	}
}

// TestCollectorControlledStopWakesAnIdlePeerPromptly is what makes a controlled
// stop controlled. One peer reaches the declared quota while another is between
// frames with a long idle timeout still to run. The session must finalize when
// the quota is reached, not when the idle peer's own timeout eventually expires.
func TestCollectorControlledStopWakesAnIdlePeerPromptly(t *testing.T) {
	const frameBytes = 4096
	const reserve = 16 << 10
	h := loopback(t, receiver.CollectorConfig{MaxConnections: 2, MaxFrameBytes: frameBytes,
		MaxCaptureBytes: frameBytes + reserve + 64, IdleTimeout: 30 * time.Second})
	idle, _ := h.dial(t)
	defer idle.Close()
	// A start byte with no frame behind it leaves this peer blocked inside a
	// partial frame rather than merely between frames, which is the harder
	// case: it has not finished the read a controlled stop has to expire.
	if _, err := idle.Write([]byte{0x0b}); err != nil {
		t.Fatal(err)
	}
	sender, reader := h.dial(t)
	if _, err := exchange(sender, reader, "QUIET-001"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
		t.Fatal("a controlled stop waited for an idle peer's own timeout")
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("the controlled stop took %s with a 30s idle timeout", elapsed)
	}
	if h.err != nil {
		t.Fatal(h.err)
	}
	sender.Close()
	b, err := bundle.Open(h.output)
	if err != nil {
		t.Fatal(err)
	}
	if record := b.Collection; record == nil || len(record.Received) != 1 {
		t.Fatalf("the quota stop did not retain exactly the frame it accepted: %+v", b.Collection)
	}
	// The interrupted peer's consumed prefix is retained as evidence and is
	// never listed as a received frame.
	if len(b.Manifest.Sources) != 2 {
		t.Fatalf("the interrupted peer's consumed bytes were discarded: %d sources", len(b.Manifest.Sources))
	}
}

// caseInsensitive reports whether this filesystem aliases distinct leaf
// spellings, which is what makes the alias re-check below meaningful.
func caseInsensitive(t *testing.T, directory string) bool {
	t.Helper()
	probe := filepath.Join(directory, "Alias-Probe")
	if err := os.WriteFile(probe, nil, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(probe)
	_, err := os.Lstat(filepath.Join(directory, "alias-probe"))
	return err == nil
}

// TestCollectorRefusesAJournalThatAliasesTheCase covers the other half of the
// destination rule: two spellings a case-insensitive filesystem resolves to one
// directory. The journal is created first, so the collision is only visible
// afterwards, exactly as it is for the fixture's observation destination.
func TestCollectorRefusesAJournalThatAliasesTheCase(t *testing.T) {
	directory := t.TempDir()
	if !caseInsensitive(t, directory) {
		t.Skip("this filesystem distinguishes leaf spellings, so no alias exists to refuse")
	}
	journal := filepath.Join(directory, "Capture")
	config := receiver.CollectorConfig{Policy: policy(t, anyTypePolicy), OutputPath: filepath.Join(directory, "capture"),
		JournalPath: journal, MaxFrameBytes: 1 << 20, IdleTimeout: time.Second, ApplicationTimeout: time.Second}
	if _, err := receiver.NewCollector(config); err == nil {
		t.Fatal("a journal aliasing the case destination was accepted")
	}
	// The journal this refusal created is removed, so a second attempt at a
	// distinct destination is not blocked by the first attempt's leftovers.
	if _, err := os.Lstat(journal); !os.IsNotExist(err) {
		t.Fatalf("a refused capture left its journal behind: %v", err)
	}
}

// largeMessage builds one ADT whose frame is bigger than a read-ahead buffer,
// so a connection that has only glanced at it still has the rest of it waiting
// in the socket.
func largeMessage(control string, padding int) []byte {
	return mllp.Frame([]byte(strings.Replace(plainADT, "COLLECT-001", control, 1) + "\rZZZ|" + strings.Repeat("P", padding)))
}

// TestCollectorDoesNotSpendAMessageSlotOnAnAnsweredPeer is the starvation
// contract. A connection that has been answered goes back to waiting for a
// frame its peer may never send. It must not hold a slot in the declared
// message limit while it waits: the remaining slots belong to frames that are
// actually coming, and a peer that really sent one inside the limit must not be
// refused in favour of peers that have finished sending.
func TestCollectorDoesNotSpendAMessageSlotOnAnAnsweredPeer(t *testing.T) {
	const peers = 4
	h := loopback(t, receiver.CollectorConfig{MaxMessages: peers, MaxConnections: peers})
	connections := make([]net.Conn, peers)
	for i := range connections {
		conn, reader := h.dial(t)
		connections[i] = conn
		// Every earlier peer has been answered and is waiting again by the time
		// this one connects, which is exactly when a slot held for a frame that
		// is not coming would refuse a frame that is.
		control := fmt.Sprintf("SEQUENTIAL-%03d", i)
		answer, err := exchange(conn, reader, control)
		if err != nil {
			t.Fatalf("peer %d sent a frame inside the declared limit and was not answered: %v", i, err)
		}
		if !strings.Contains(answer, control) {
			t.Fatalf("peer %d received another peer's acknowledgement: %q", i, answer)
		}
	}
	for _, conn := range connections {
		conn.Close()
	}
	record := h.finished(t).Collection
	if record == nil || len(record.Received) != peers || len(record.Sessions) != peers {
		t.Fatalf("a peer inside the declared limit was not captured: %+v", record)
	}
}

// TestCollectorRefusedPeerIsClosedRatherThanReset is the other half of the same
// defect. When the room left is promised to a frame already in flight, a later
// peer's frame is refused, and the refusal has to reach that peer as an orderly
// close. Closing a socket over bytes still in its receive queue emits RST, and a
// reset also discards whatever this connection had already written and the peer
// had not yet read, so a refusal would arrive as a destroyed connection rather
// than as the bound an operator declared.
func TestCollectorRefusedPeerIsClosedRatherThanReset(t *testing.T) {
	listener := newPipeListener()
	h := serving(t, receiver.CollectorConfig{MaxMessages: 1, MaxConnections: 2, MaxSessions: 2}, listener)
	// The peer that holds the one slot writes over a synchronous pipe, and
	// sends more of its frame than one read-ahead buffer holds, so its write
	// returns only once the collector has come back for the rest of that frame
	// -- which it does only after claiming the slot. Which peer holds the slot
	// is decided here rather than by the scheduler.
	holder, held := net.Pipe()
	defer holder.Close()
	listener.connections <- held
	if err := holder.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Write([]byte("\x0b" + strings.Repeat("H", 8<<10))); err != nil {
		t.Fatal(err)
	}
	// The refused peer is real TCP, because a reset is what this test is about,
	// and its frame is in the receive queue before the collector is ever given
	// the connection.
	endpoint, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	refused, err := net.DialTimeout("tcp", endpoint.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer refused.Close()
	accepted, err := endpoint.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if err := refused.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := refused.Write(largeMessage("SLOT-002", 16<<10)); err != nil {
		t.Fatal(err)
	}
	listener.connections <- accepted
	refusedReader, _ := mllp.NewReader(refused, 1<<20)
	if _, err := refusedReader.ReadFrame(); !errors.Is(err, io.EOF) {
		t.Fatalf("a refused peer was reset rather than closed: %v", err)
	}
	holder.Close()
	refused.Close()
	b := h.finished(t)
	if record := b.Collection; record == nil || len(record.Received) != 0 {
		t.Fatalf("a refused frame was claimed as acknowledged: %+v", b.Collection)
	}
	if len(b.Manifest.Sources) != 2 {
		t.Fatalf("a refused peer's bytes were discarded: %d sources", len(b.Manifest.Sources))
	}
	retained := false
	for _, event := range b.Events {
		raw, _ := b.Raw(event.ID)
		retained = retained || strings.Contains(string(raw), "SLOT-002")
	}
	if !retained {
		t.Fatal("a refused frame was drained out of the socket without being retained as evidence")
	}
}
