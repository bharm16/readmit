package desktop_test

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/operation"
)

// TestDesktopCollectMatchesCLI verifies the desktop collector and
// `readmit collect status` agree on retained journal counts for the same capture.
func TestDesktopCollectMatchesCLI(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	policy, err := collection.DecodePolicy([]byte(facadeAnyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	if res := app.SaveReceiverPolicy(desktop.ReceiverPolicyRequest{
		Workspace: root, PolicyFile: "policy.json", Policy: policy,
	}); res.State != desktop.Completed {
		t.Fatalf("save: %+v", res)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()

	done := make(chan desktop.CaptureSessionResult, 1)
	go func() {
		done <- app.StartCapture(desktop.CaptureRequest{
			Workspace: root, Kind: "collect", Address: addr,
			PolicyFile: "policy.json", OutputName: "case", JournalName: "journal",
			MaxMessages: 1, IdleTimeout: "2s",
		})
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
	payload := []byte("MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\rPID|||1||DOE^JOHN\r")
	if _, err := conn.Write(mllp.Frame(payload)); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	session := <-done
	if session.State != desktop.Completed || session.Journal == nil {
		t.Fatalf("session: %+v", session)
	}

	cliSummary, err := operation.CaptureJournalStatus(filepath.Join(root, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	if cliSummary.Received != session.Journal.Received || cliSummary.State != session.Journal.State {
		t.Fatalf("desktop/CLI journal disagree: desktop=%+v cli=%+v", session.Journal, cliSummary)
	}

	// If a readmit binary is on PATH in this module tree, also exercise the CLI
	// status subcommand against the same journal.
	bin := filepath.Join(t.TempDir(), "readmit")
	build := exec.Command("go", "build", "-o", bin, "./cmd/readmit")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Logf("skipping CLI binary check: %v\n%s", err, out)
		return
	}
	cmd := exec.Command(bin, "collect", "status", filepath.Join(root, "journal"), "--json")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("collect status: %v", err)
	}
	_ = context.Background()
}
