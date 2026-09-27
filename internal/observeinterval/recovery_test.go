package observeinterval_test

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"fmt"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKilledIntervalHelper(t *testing.T) {
	root := os.Getenv("READMIT_INTERVAL_KILL_HELPER")
	if root == "" {
		t.Skip("subprocess helper")
	}
	session, capture, _, _, _ := liveCapture(t, 10, captureOptions{directory: root})
	raw, _ := json.Marshal(struct{ Path, Address string }{session.Path(), capture.Address()})
	fmt.Println("INTERVAL:" + string(raw))
	select {}
}
func TestProcessKillPreservesSpoolAndRecoveryNeverResumes(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestKilledIntervalHelper$", "-test.v")
	cmd.Env = append(os.Environ(), "READMIT_INTERVAL_KILL_HELPER="+root)
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	ready := make(chan struct{ Path, Address string }, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "INTERVAL:") {
				var v struct{ Path, Address string }
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "INTERVAL:")), &v) == nil {
					ready <- v
				}
				return
			}
		}
	}()
	var live struct{ Path, Address string }
	select {
	case live = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not arm")
	}
	sendIndependent(t, live.Address, "run-one", 2)
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if cmd.Wait() == nil {
		t.Fatal("child was not killed")
	}
	before, err := os.ReadFile(filepath.Join(live.Path, "capture", "journal", "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		result, err := observeinterval.Recover(context.Background(), live.Path)
		if err != nil || result.Sufficient() || result.Reason != "interrupted" {
			t.Fatal(result, err)
		}
		spool, err := capturejournal.Open(filepath.Join(live.Path, "capture", "journal"))
		if err != nil || spool.Received != 2 || !spool.Recovered {
			t.Fatal(spool, err)
		}
	}
	after, err := os.ReadFile(filepath.Join(live.Path, "capture", "journal", "journal.jsonl"))
	if err != nil || string(before) != string(after) {
		t.Fatal("read-only recovery changed spool", err)
	}
	if _, err = observeinterval.Open(context.Background(), live.Path); err == nil {
		t.Fatal("killed interval became completed evidence")
	}
}
func TestLiveCaptureWrongRunDroppedFrameAndCapacityCannotPassAbsence(t *testing.T) {
	for _, variant := range []string{"healthy-empty", "wrong-run", "dropped-frame", "record-ceiling", "client-ceiling"} {
		t.Run(variant, func(t *testing.T) {
			max := 10
			if variant == "record-ceiling" {
				max = 2
			}
			settings := captureOptions{}
			if variant == "client-ceiling" {
				settings.sessions = 1
			}
			session, capture, clock, _, binding := liveCapture(t, max, settings)
			switch variant {
			case "wrong-run":
				sendIndependent(t, capture.Address(), "another-run", 1)
			case "client-ceiling":
				sendIndependent(t, capture.Address(), binding.Run, 1)
			case "record-ceiling":
				sendIndependent(t, capture.Address(), binding.Run, 2)
			case "dropped-frame":
				connection, err := net.Dial("tcp", capture.Address())
				if err != nil {
					t.Fatal(err)
				}
				_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
				if _, err = connection.Write([]byte("\x0bMSH|^~\\&|TRUNCATED")); err != nil {
					t.Fatal(err)
				}
				if err = connection.(*net.TCPConn).CloseWrite(); err != nil {
					t.Fatal(err)
				}
				if _, err = io.ReadAll(connection); err != nil {
					t.Fatal(err)
				}
				connection.Close()
			}
			if err := session.StimulusFinished(); err != nil {
				t.Fatal(err)
			}
			clock.elapsed = 100
			poll, _ := capture.Poll(context.Background())
			if err := session.Append(context.Background(), poll); err != nil {
				t.Fatal(err)
			}
			final, err := capture.Finalize(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err = session.Append(context.Background(), final); err != nil {
				t.Fatal(err)
			}
			result, err := session.Finish(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.Sufficient() != (variant == "healthy-empty") {
				t.Fatal(variant, result)
			}
			if variant == "wrong-run" && len(final.Excluded) != 1 {
				t.Fatal("wrong-run evidence disappeared")
			}
		})
	}
}

func TestCapturedSnapshotHealthIsUnaffectedByLaterValidDiskReplacement(t *testing.T) {
	session, capture, clock, _, binding := liveCapture(t, 10)
	sendIndependent(t, capture.Address(), binding.Run, 1)
	session.StimulusFinished()
	clock.elapsed = 100
	final, err := capture.Finalize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = session.Append(context.Background(), final); err != nil {
		t.Fatal(err)
	}
	if _, err = session.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(session.Path(), "capture")
	files := map[string][]byte{}
	if err = filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(name)], err = os.ReadFile(path)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	readback, err := networkaction.OpenCaptureEvidence(directory)
	if err != nil || !artifactdir.MatchesSubtree(mapWithPrefix(files, "capture"), "capture", networkaction.ResultSchema, readback.Identity) {
		t.Fatal(readback, err)
	}
	var changed networkaction.Result
	if err = json.Unmarshal(files["result.json"], &changed); err != nil {
		t.Fatal(err)
	}
	changed.Actor.ID = "other-reader"
	raw, _ := json.Marshal(changed, json.Deterministic(true))
	replacement := mapWithPrefix(files, "")
	replacement["result.json"] = raw
	replacement["identity.sha256"] = []byte(artifactdir.Identity(networkaction.ResultSchema, replacement) + "\n")
	for _, name := range []string{"result.json", "identity.sha256"} {
		if err = os.WriteFile(filepath.Join(directory, name), replacement[name], 0600); err != nil {
			t.Fatal(err)
		}
	}
	later, err := networkaction.OpenCaptureEvidence(directory)
	if err != nil || later.Result.Actor.ID != "other-reader" {
		t.Fatal(later, err)
	}
	if artifactdir.MatchesSubtree(mapWithPrefix(files, "capture"), "capture", networkaction.ResultSchema, later.Identity) {
		t.Fatal("mixed valid captures had a coherent identity")
	}
	original, err := capturejournal.Verify(artifactdir.Subtree(files, "journal"))
	if err != nil || original.Received != 1 {
		t.Fatal(original, err)
	}
	if err = os.WriteFile(filepath.Join(directory, "journal", "journal.jsonl"), []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	original, err = capturejournal.Verify(artifactdir.Subtree(files, "journal"))
	if err != nil || original.Received != 1 || original.JournalIncomplete {
		t.Fatal("retained journal was reread from changed path", original, err)
	}
}
func mapWithPrefix(files map[string][]byte, prefix string) map[string][]byte {
	out := map[string][]byte{}
	for name, raw := range files {
		if prefix != "" {
			name = prefix + "/" + name
		}
		out[name] = raw
	}
	return out
}
