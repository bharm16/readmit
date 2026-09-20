//go:build !windows

package tests

import (
	"bufio"
	"bytes"
	"context"
	"github.com/bharm16/readmit/internal/testlicense"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/testrunner"
)

func TestPreparedIPv6ListenerCommandRunsInStrictGlobbingShells(t *testing.T) {
	shells := []struct {
		name string
		args []string
	}{
		{"zsh", []string{"-f", "-c"}},
		{"bash", []string{"--noprofile", "--norc", "-O", "failglob", "-c"}},
	}
	for _, shell := range shells {
		t.Run(shell.name, func(t *testing.T) {
			shellPath, err := exec.LookPath(shell.name)
			if err != nil {
				t.Skip("shell is not installed")
			}
			listener, err := net.Listen("tcp6", "[::1]:0")
			if err != nil {
				t.Skipf("IPv6 loopback unavailable: %v", err)
			}
			address := listener.Addr().String()
			listener.Close()

			consumer := filepath.Join(t.TempDir(), "IPv6 consumer with spaces")
			if err := os.Mkdir(consumer, 0700); err != nil {
				t.Fatal(err)
			}
			movedBinary := filepath.Join(consumer, "readmit")
			executable, err := os.ReadFile(binary)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(movedBinary, executable, 0700); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{
				{"report", "--scenario", "siu-reschedule-v1", "--output", "packet"},
				{"report", "prepare", "packet", "--output", "rerun", "--address", address},
			} {
				out, diagnostic, code := runPacketBinary(t, movedBinary, consumer, args)
				if code != 0 || diagnostic != "" {
					t.Fatalf("prepare IPv6 procedure: %d %s %s", code, out, diagnostic)
				}
			}
			instructions, err := os.ReadFile(filepath.Join(consumer, "rerun", "RERUN.md"))
			if err != nil {
				t.Fatal(err)
			}
			var printedCommand string
			for _, line := range strings.Split(string(instructions), "\n") {
				if strings.HasPrefix(line, `./readmit --operation-policy "$READMIT_POLICY" listen `) {
					printedCommand = line
					break
				}
			}
			if printedCommand == "" {
				t.Fatal("prepared instructions omit listener command")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			policy := testlicense.New(t)
			cmd := exec.CommandContext(ctx, shellPath, append(shell.args, printedCommand)...)
			cmd.Dir, cmd.Env = consumer, append(packetEnvironment(), "READMIT_POLICY="+policy)
			// A timeout must stop both the shell and its actual listener child.
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
			var diagnostic bytes.Buffer
			cmd.Stderr = &diagnostic
			pipe, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			t.Cleanup(func() {
				cancel()
				if !waited {
					_ = cmd.Wait()
				}
			})
			reader := bufio.NewReader(pipe)
			ready, err := reader.ReadString('\n')
			if err != nil || strings.TrimSpace(ready) != "Listening: "+address {
				cancel()
				waitErr := cmd.Wait()
				waited = true
				t.Fatalf("printed IPv6 command did not start listener: %v; %s", waitErr, diagnostic.String())
			}
			out, stderr, code := runPacketBinary(t, movedBinary, consumer, []string{"--operation-policy", policy, "test", "rerun/baseline/spec.json", "--send", "--output", "rerun/baseline/result"})
			if code != 1 || stderr != "" {
				t.Fatalf("IPv6 baseline: %d %s %s", code, out, stderr)
			}
			_, _ = io.Copy(io.Discard, reader)
			waitErr := cmd.Wait()
			waited = true
			if waitErr != nil || diagnostic.Len() != 0 {
				t.Fatalf("IPv6 fixture: %v %s", waitErr, diagnostic.String())
			}
			result, err := testrunner.Open(filepath.Join(consumer, "rerun", "baseline", "result"))
			if err != nil || result.Result.Status != testrunner.AssertionFailure || len(result.FinalObservation.Records) != 2 {
				t.Fatalf("printed IPv6 command did not produce expected ledger evidence: %v", err)
			}
		})
	}
}
