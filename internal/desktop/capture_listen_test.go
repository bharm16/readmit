package desktop_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/mllp"
)

// sendFixtureMessage sends one of the frozen SIU fixtures on conn in its MLLP
// frame and returns the acknowledgement the fixture answered with.
func sendFixtureMessage(t *testing.T, conn net.Conn, frames *mllp.Reader, fixture string) string {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", fixture))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(mllp.Frame(payload)); err != nil {
		t.Fatal(err)
	}
	ack, err := frames.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	return string(ack)
}

func dialFixture(t *testing.T, address string) (net.Conn, *mllp.Reader) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	frames, err := mllp.NewReader(conn, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return conn, frames
}
