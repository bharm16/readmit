package tests

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/testrunner"
)

func TestReportPrintedProcedureWorksWithRelocatedBinaryAndPacket(t *testing.T) {
	original := filepath.Join(t.TempDir(), "original packet")
	stdout, stderr, err := run(t, "report", "--scenario", "siu-reschedule-v1", "--output", original)
	if err != nil || stderr != "" {
		t.Fatalf("report: %v %s %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "fresh built-in") || !strings.Contains(stdout, "Input unchanged") {
		t.Fatal("report console omitted scope")
	}
	for _, private := range []string{original, "SYNTH-0000000000000000", "FILLER-", "PLACER-", "127.0.0.1"} {
		if strings.Contains(stdout+stderr, private) {
			t.Fatal("report console disclosed input or configured target")
		}
	}
	consumer := filepath.Join(t.TempDir(), "independent consumer with spaces")
	if err := os.Mkdir(consumer, 0700); err != nil {
		t.Fatal(err)
	}
	movedBinary := filepath.Join(consumer, filepath.Base(binary))
	executable, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(movedBinary, executable, 0700); err != nil {
		t.Fatal(err)
	}
	packet := filepath.Join(consumer, "packet")
	if err := os.CopyFS(packet, os.DirFS(original)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(original); err != nil {
		t.Fatal(err)
	}
	before := reportFileHashes(t, packet)
	raw, err := os.ReadFile(filepath.Join(packet, "RERUN.md"))
	if err != nil {
		t.Fatal(err)
	}
	instructions := string(raw)
	for _, required := range []string{"current working directory", "Windows PowerShell", "--max-messages 2", "Listening:", "empty appointment ledger", "Expected exit: 1", "Expected exit: 0"} {
		if !strings.Contains(instructions, required) {
			t.Fatalf("instructions omit %s", required)
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	var waitFixture func()
	testsRun := 0
	commands := 0
	for _, block := range strings.Split(instructions, "~~~sh\n")[1:] {
		for _, line := range strings.Split(strings.SplitN(block, "\n~~~", 2)[0], "\n") {
			args := strings.Fields(strings.ReplaceAll(line, "127.0.0.1:2575", address))
			if len(args) < 2 || args[0] != "./readmit" {
				t.Fatal("unsupported command in printed procedure")
			}
			args = args[1:]
			for i := range args {
				args[i] = strings.Trim(args[i], "\"")
			}
			commands++
			if args[0] == "listen" {
				if waitFixture != nil {
					t.Fatal("procedure did not finish previous receiver")
				}
				waitFixture = startPacketFixture(t, movedBinary, consumer, args)
				continue
			}
			out, diagnostic, code := runPacketBinary(t, movedBinary, consumer, args)
			want := 0
			if args[0] == "test" {
				if strings.Contains(args[1], "baseline") || strings.Contains(args[1], "reintroduced") {
					want = 1
				}
				testsRun++
			}
			if code != want || diagnostic != "" {
				t.Fatalf("printed command %v: exit %d want %d: %s %s", args, code, want, out, diagnostic)
			}
			if args[0] == "test" {
				if waitFixture == nil {
					t.Fatal("test before listener")
				}
				waitFixture()
				waitFixture = nil
			}
		}
	}
	if testsRun != 3 || commands != 10 {
		t.Fatalf("procedure ran %d commands and %d tests", commands, testsRun)
	}
	for _, trial := range []struct {
		name   string
		status testrunner.Status
		count  int
	}{{"baseline", testrunner.AssertionFailure, 2}, {"post-fix", testrunner.Pass, 1}, {"reintroduced", testrunner.AssertionFailure, 2}} {
		artifact, err := testrunner.Open(filepath.Join(consumer, "rerun", trial.name, "result"))
		if err != nil {
			t.Fatal(err)
		}
		if artifact.Result.Status != trial.status || len(artifact.FinalObservation.Records) != trial.count {
			t.Fatal("rerun did not reproduce expected ledger verdict")
		}
		for _, assertion := range artifact.Result.Assertions[2:] {
			if assertion.Status != "passed" {
				t.Fatal("ACK failed instead of ledger assertion")
			}
		}
	}
	if !reflect.DeepEqual(before, reportFileHashes(t, packet)) {
		t.Fatal("printed rerun procedure changed sealed packet")
	}
	if err := os.WriteFile(filepath.Join(packet, "extra.txt"), []byte("extra"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, code := runPacketBinary(t, movedBinary, consumer, []string{"report", "verify", "packet"})
	if code != 1 {
		t.Fatal("relocated verification accepted extra file")
	}
}

func TestReportCLIRejectsUnsupportedOrPrivateArgumentsWithoutDisclosure(t *testing.T) {
	for _, args := range [][]string{{"report"}, {"report", "--scenario", "customer-derived", "--output", "SECRET"}, {"report", "--synthetic", "--output", "SECRET"}, {"report", "verify", "SECRET"}, {"report", "prepare", "SECRET", "--output", "SECRET", "--address", "SECRET"}, {"report", "verify", "SECRET", "extra"}} {
		stdout, stderr, err := run(t, args...)
		if processCode(t, err) != 1 || stdout != "" || stderr == "" || len(stderr) > 300 || strings.Contains(stderr, "SECRET") {
			t.Fatalf("unsafe report error: %v %s %s", err, stdout, stderr)
		}
	}
}

func packetEnvironment() []string {
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "PATH=") {
			env = append(env, entry)
		}
	}
	return append(env, "PATH=")
}

func runPacketBinary(t *testing.T, binaryPath, cwd string, args []string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = cwd
	cmd.Env = packetEnvironment()
	var out, diagnostic bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostic
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatal("packet procedure timed out")
	}
	return out.String(), diagnostic.String(), processCode(t, err)
}

func startPacketFixture(t *testing.T, binaryPath, cwd string, args []string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = cwd
	cmd.Env = packetEnvironment()
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
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
	line, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "Listening: ") {
		t.Fatal("printed fixture procedure did not become ready")
	}
	return func() {
		t.Helper()
		_, _ = io.Copy(io.Discard, reader)
		err := cmd.Wait()
		waited = true
		cancel()
		if err != nil {
			t.Fatalf("fixture: %v %s", err, diagnostic.String())
		}
	}
}

func reportFileHashes(t *testing.T, dir string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		result[filepath.ToSlash(name)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
