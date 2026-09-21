package tests

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
)

func replayCase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case")
	stdout, stderr, err := run(t, "capture", "../testdata/fixtures/listen-s12.hl7", "../testdata/fixtures/listen-s13.hl7", "--output", path)
	if err != nil || stderr != "" || !strings.Contains(stdout, "Messages: 2") {
		t.Fatalf("capture replay input: %v %s %s", err, stdout, stderr)
	}
	return path
}

func replayTarget(t *testing.T, address string) string {
	t.Helper()
	data, err := json.Marshal(replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "200ms", MaxACKBytes: 4096}, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReplayExecutableDryRunDoesNotConnectOrCreateOutput(t *testing.T) {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	source := replayCase(t)
	out := filepath.Join(t.TempDir(), "must-not-exist")
	stdout, stderr, err := run(t, "replay", source, "--target", replayTarget(t, l.Addr().String()), "--output", out)
	if err != nil || stderr != "" || !strings.Contains(stdout, "Dry run: no connection opened") || !strings.Contains(stdout, "Messages: 2") || !strings.Contains(stdout, l.Addr().String()) || !strings.Contains(stdout, "Transformations: none") {
		t.Fatalf("dry run: %v %s %s", err, stdout, stderr)
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Fatal("dry run created output")
	}
	_ = l.SetDeadline(time.Now().Add(100 * time.Millisecond))
	if c, err := l.Accept(); err == nil {
		_ = c.Close()
		t.Fatal("dry run opened connection")
	}
	for _, private := range []string{source, "SYNTH-001", "LISTEN-BOOK", "APPT-001"} {
		if strings.Contains(stdout+stderr, private) {
			t.Fatal("dry-run leaked source information")
		}
	}
}

func TestReplayExecutableAgainstListenPreservesCaseAndMapsOccurrences(t *testing.T) {
	source := replayCase(t)
	before, err := bundle.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	observationPath := filepath.Join(dir, "observation.json")
	receiver := startReceiver(t, 15*time.Second, "listen", "--address", "127.0.0.1:0", "--mode", "fixed", "--output", filepath.Join(dir, "recorded"), "--observation", observationPath, "--max-messages", "2", "--idle-timeout", "2s")
	output := filepath.Join(dir, "run")
	stdout, stderr, err := run(t, "replay", source, "--target", replayTarget(t, receiver.address), "--send", "--output", output)
	if err != nil || stderr != "" || strings.Count(stdout, "outcome=application_accepted") != 2 || !strings.Contains(stdout, "Schema: readmit-run/v1") {
		t.Fatalf("replay: %v %s %s", err, stdout, stderr)
	}
	receiver.wait(t)
	r, err := replay.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range r.Events {
		if r.Manifest.Mappings[i].SourceOccurrence != before.Events[i].ID {
			t.Fatal("source occurrence mapping lost")
		}
		sent, err := r.Raw(e.Sent)
		if err != nil {
			t.Fatal(err)
		}
		original, _ := before.Raw(before.Events[i].ID)
		if !bytes.Equal(sent, mllp.Frame(original)) {
			t.Fatal("default replay changed message payload bytes")
		}
	}
	after, err := bundle.Open(source)
	if err != nil || after.Identity != before.Identity {
		t.Fatal("source bytes changed")
	}
	snapshot, err := observation.Read(observationPath)
	if err != nil || len(snapshot.Records) != 1 || len(snapshot.Processed) != 2 || !snapshot.Consistent {
		t.Fatal("receiver scenario was not processed")
	}
	if snapshot.Processed[1].OccurrenceID == r.Events[1].SourceOccurrence || snapshot.Processed[1].OccurrenceID == r.Events[1].OutboundOccurrence {
		t.Fatal("test failed to exercise independent receiver occurrence IDs")
	}
	for _, private := range []string{source, output, "LISTEN-BOOK", "SYNTH-001", "APPT-001"} {
		if strings.Contains(stdout+stderr, private) {
			t.Fatal("replay output exposed source information")
		}
	}
}

func TestReplayExecutableFailureClassesAppearInConsole(t *testing.T) {
	for _, name := range []string{"timeout", "disconnect", "AE", "AR", "connection_refused"} {
		t.Run(name, func(t *testing.T) {
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := l.Addr().String()
			defer l.Close()
			done := make(chan struct{})
			if name == "connection_refused" {
				_ = l.Close()
				close(done)
			} else {
				go func() {
					defer close(done)
					c, err := l.Accept()
					if err != nil {
						return
					}
					defer c.Close()
					_ = c.SetDeadline(time.Now().Add(2 * time.Second))
					frames, _ := mllp.NewReader(c, 1<<20)
					if _, err := frames.ReadFrame(); err != nil {
						return
					}
					switch name {
					case "timeout":
						_, _ = io.Copy(io.Discard, c)
					case "disconnect":
						_, _ = c.Write([]byte("\x0bMSH|PARTIAL"))
					default:
						_, _ = c.Write(mllp.Frame([]byte("MSH|^~\\&|FIXTURE|TEST|||20260101120000||ACK^S12|A|P|2.5.1\rMSA|" + name + "|LISTEN-BOOK\r")))
					}
				}()
			}
			output := filepath.Join(t.TempDir(), "run")
			stdout, stderr, err := run(t, "replay", replayCase(t), "--target", replayTarget(t, address), "--message", "s0001-e000001", "--send", "--output", output)
			<-done
			want := name
			if name == "AE" {
				want = "application_error"
			}
			if name == "AR" {
				want = "application_rejected"
			}
			if err == nil || !strings.Contains(stdout, "outcome="+want) || stderr == "" || strings.Contains(stdout+stderr, "LISTEN-BOOK") {
				t.Fatalf("failure summary: %v %s %s", err, stdout, stderr)
			}
			if _, err := replay.Open(output); err != nil {
				t.Fatalf("failure run not finalized: %v", err)
			}
		})
	}
}

func TestReplayExecutableRejectsUnsafeArgumentsPrivately(t *testing.T) {
	source := replayCase(t)
	config := replayTarget(t, "127.0.0.1:2575")
	for _, args := range [][]string{
		{"replay", source},
		{"replay", source, "SECRET-HOST:2575", "--target", config},
		{"replay", source, "--target", config, "--send"},
		{"replay", source, "--target", "SECRET-TARGET"},
		{"replay", source, "--target", config, "--message", "SECRET-OCCURRENCE"},
		{"replay", source, "--target", config, "--transform", "SECRET-TRANSFORM"},
		{"replay", source, "--target", config, "--shift", "24h"},
	} {
		stdout, stderr, err := run(t, args...)
		if err == nil || stdout != "" || stderr == "" || len(stderr) > 300 || strings.Contains(stderr, "SECRET") || strings.Contains(stderr, source) {
			t.Fatalf("unsafe rejection: %v %s %s", err, stdout, stderr)
		}
	}
}

func TestDerivedCommandsRejectSymlinkThenParentInsideSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	for _, command := range []string{"replay", "diagnose"} {
		t.Run(command, func(t *testing.T) {
			source := replayCase(t)
			before, err := bundle.Open(source)
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(filepath.Join(source, "payloads"), alias); err != nil {
				t.Fatal(err)
			}
			// Joining these components would clean away the exact traversal
			// that must be evaluated by the filesystem, so retain the raw path.
			destination := alias + "/../new-artifact"
			args := []string{command, source, "--output", destination}
			if command == "replay" {
				args = append(args, "--send", "--target", replayTarget(t, "127.0.0.1:2575"))
			}
			stdout, stderr, err := run(t, args...)
			if err == nil || stdout != "" || !strings.Contains(stderr, "outside the immutable input case") {
				t.Fatalf("alias-parent traversal was allowed: %v %s %s", err, stdout, stderr)
			}
			after, err := bundle.Open(source)
			if err != nil || before.Identity != after.Identity {
				t.Fatal("source bundle was modified")
			}
		})
	}
}
