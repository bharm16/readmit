package receiver_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
)

// The frame loop's limit and refusal contracts, learned in bf22601a (#178),
// run against both handlers the loop serves. The fixture serves one connection
// at a time, so where the collector's scenario needs concurrency, the fixture's
// subtest states the same promise inside one connection.

// largeMessage builds one ADT whose frame is bigger than a read-ahead buffer,
// so a connection that has only glanced at it still has the rest of it waiting
// in the socket.
func largeMessage(control string, padding int) []byte {
	return mllp.Frame([]byte(strings.Replace(plainADT, "COLLECT-001", control, 1) + "\rZZZ|" + strings.Repeat("P", padding)))
}

// TestAnsweredPeerDoesNotSpendAMessageSlot is the starvation contract. A
// connection that has been answered goes back to waiting for a frame its peer
// may never send. It must not hold a slot in the declared message limit while
// it waits: the remaining slots belong to frames that are actually coming, and
// a peer that really sent one inside the limit must not be refused in favour of
// peers that have finished sending.
func TestAnsweredPeerDoesNotSpendAMessageSlot(t *testing.T) {
	t.Run("collector", func(t *testing.T) {
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
	})
	t.Run("fixture", func(t *testing.T) {
		// One connection at a time, so the peer that has been answered and
		// waits again is this connection. Its next frame inside the declared
		// limit is answered, never refused by a slot held while it waited.
		h := start(t, observation.Fixed, 2, 1<<20, time.Second)
		conn := h.connect(t)
		reader, _ := mllp.NewReader(conn, 1<<20)
		send(t, conn, mllp.Frame(fixture(t, "listen-s12.hl7")))
		ack(t, reader, "AA", "LISTEN-BOOK")
		send(t, conn, mllp.Frame(fixture(t, "listen-s13.hl7")))
		ack(t, reader, "AA", "LISTEN-MOVE")
		h.await(t)
		if h.err != nil {
			t.Fatal(h.err)
		}
		b, err := bundle.Open(h.config.OutputPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(b.Observation.Processed) != 2 {
			t.Fatalf("a frame inside the declared limit was not processed: %+v", b.Observation.Processed)
		}
	})
}

// TestRefusedPeerIsClosedRatherThanReset is the refusal's other half. When the
// room left is promised to a frame already in flight, a peer's frame is
// refused, and the refusal has to reach that peer as an orderly close. Closing
// a socket over bytes still in its receive queue emits RST, and a reset also
// discards whatever this connection had already written and the peer had not
// yet read, so a refusal would arrive as a destroyed connection rather than as
// the bound an operator declared.
func TestRefusedPeerIsClosedRatherThanReset(t *testing.T) {
	t.Run("collector", func(t *testing.T) {
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
	})
	t.Run("fixture", func(t *testing.T) {
		// One connection at a time, so the bound ends the session once its one
		// frame is answered. A second frame that arrived coalesced behind it was
		// consumed into evidence before the close, so the peer sees an orderly
		// close rather than a reset, and the frame is retained as received
		// without being claimed as processed.
		h := start(t, observation.Fixed, 1, 1<<20, time.Second)
		conn := h.connect(t)
		reader, _ := mllp.NewReader(conn, 1<<20)
		book := mllp.Frame(fixture(t, "listen-s12.hl7"))
		move := mllp.Frame(fixture(t, "listen-s13.hl7"))
		send(t, conn, append(bytes.Clone(book), move...))
		ack(t, reader, "AA", "LISTEN-BOOK")
		if _, err := reader.ReadFrame(); !errors.Is(err, io.EOF) {
			t.Fatalf("a peer whose next frame was beyond the declared limit was reset rather than closed: %v", err)
		}
		h.await(t)
		if h.err != nil {
			t.Fatal(h.err)
		}
		b, err := bundle.Open(h.config.OutputPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(b.Observation.Processed) != 1 || b.Observation.Processed[0].ControlID != "LISTEN-BOOK" {
			t.Fatalf("a frame beyond the declared limit was claimed as processed: %+v", b.Observation.Processed)
		}
		retained := false
		for _, event := range b.Events {
			raw, _ := b.Raw(event.ID)
			retained = retained || bytes.Contains(raw, move)
		}
		if !retained {
			t.Fatal("the frame beyond the declared limit was not retained as evidence")
		}
	})
}
