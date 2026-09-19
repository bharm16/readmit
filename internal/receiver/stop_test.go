package receiver_test

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/receiver"
)

// stopGatedConn holds the first read either before TCP consumes a byte or
// after it consumes one but before returning it to the collector. Expiring the
// read releases the gate, so both shutdown interleavings are deterministic.
type stopGatedConn struct {
	net.Conn
	entered, stopped    chan struct{}
	consumed, firstDone bool
	release             sync.Once
}

func (c *stopGatedConn) Read(p []byte) (int, error) {
	if c.firstDone {
		return c.Conn.Read(p)
	}
	c.firstDone = true
	if c.consumed {
		n, err := c.Conn.Read(p)
		close(c.entered)
		<-c.stopped
		return n, err
	}
	close(c.entered)
	<-c.stopped
	return c.Conn.Read(p)
}

func (c *stopGatedConn) SetReadDeadline(d time.Time) error {
	err := c.Conn.SetReadDeadline(d)
	if !d.IsZero() && !d.After(time.Now()) {
		c.release.Do(func() { close(c.stopped) })
	}
	return err
}

func (c *stopGatedConn) Close() error {
	c.release.Do(func() { close(c.stopped) })
	return c.Conn.Close()
}

func assertStoppedPrefix(t *testing.T, b *bundle.Bundle) {
	t.Helper()
	prefixes := 0
	for _, event := range b.Events {
		raw, err := b.Raw(event.ID)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) == "\x0b" {
			prefixes++
			if event.Direction != bundle.Inbound {
				t.Fatal("stopped prefix lost its inbound direction")
			}
		}
	}
	if prefixes != 1 {
		t.Fatalf("want exactly one retained start byte, got %d", prefixes)
	}
}

// Successful TCP writes do not prove application consumption. Stopping before
// the first socket read must not invent evidence; a byte actually consumed must
// survive even if the read returns only after the stop.
func TestCollectorControlledStopAtSocketReadBoundary(t *testing.T) {
	for _, consumed := range []bool{false, true} {
		name := "unread"
		if consumed {
			name = "consumed_before_return"
		}
		t.Run(name, func(t *testing.T) {
			listener := newPipeListener()
			h := serving(t, receiver.CollectorConfig{MaxConnections: 2, MaxFrameBytes: 4096,
				MaxCaptureBytes: 4096 + (16 << 10) + 64, IdleTimeout: 30 * time.Second}, listener)
			tcp, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer tcp.Close()
			idle, err := net.DialTimeout("tcp", tcp.Addr().String(), 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer idle.Close()
			_ = idle.SetDeadline(time.Now().Add(10 * time.Second))
			server, err := tcp.Accept()
			if err != nil {
				t.Fatal(err)
			}
			gate := &stopGatedConn{Conn: server, entered: make(chan struct{}), stopped: make(chan struct{}), consumed: consumed}
			defer gate.Close()
			listener.connections <- gate
			if _, err := idle.Write([]byte{0x0b}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-gate.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("collector did not reach the socket read boundary")
			}
			sender, server2 := net.Pipe()
			defer sender.Close()
			_ = sender.SetDeadline(time.Now().Add(10 * time.Second))
			listener.connections <- server2
			reader, _ := mllp.NewReader(sender, 4096)
			if _, err := exchange(sender, reader, "QUIET-001"); err != nil {
				t.Fatal(err)
			}
			b := h.finished(t)
			wantSources := 1
			if consumed {
				wantSources = 2
			}
			if len(b.Manifest.Sources) != wantSources {
				t.Fatalf("retained %d sources, want %d (socket consumed=%t)", len(b.Manifest.Sources), wantSources, consumed)
			}
			if record := b.Collection; record == nil || len(record.Received) != 1 || record.Received[0].ControlID != "QUIET-001" {
				t.Fatalf("stop claimed a partial or unread frame: %+v", record)
			}
			if consumed {
				assertStoppedPrefix(t, b)
			}
		})
	}
}
