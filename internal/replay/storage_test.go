package replay_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
)

func TestRunReaderRejectsTamperingEvenWithRehashedMetadata(t *testing.T) {
	address := peer(t, func(c net.Conn) {
		reader, _ := mllp.NewReader(c, 1<<20)
		_, _ = reader.ReadFrame()
		_, _ = c.Write(ack("AA", "READMIT000001"))
	})
	_, original := execute(t, caseAt(t, request("ORIGINAL")), target(address), replay.Options{Transformations: []replay.Transformation{{Name: "rebase-control-ids"}}})
	for _, tc := range []struct {
		name   string
		mutate func(string)
	}{
		{"marker", func(path string) { _ = os.Remove(filepath.Join(path, "identity.sha256")) }},
		{"payload", func(path string) {
			_ = os.WriteFile(filepath.Join(path, "payloads/o000001-sent.bin"), []byte("changed"), 0600)
		}},
		{"source-marker", func(path string) {
			editManifest(t, path, func(m *replay.Manifest) { m.ContainsSourceValues = false })
			rehash(t, path)
		}},
		{"mapping", func(path string) {
			editManifest(t, path, func(m *replay.Manifest) { m.Mappings[0].SourceOccurrence = "s0002-e000001" })
			rehash(t, path)
		}},
		{"old-value", func(path string) {
			editManifest(t, path, func(m *replay.Manifest) { m.Changes[0].Old = []byte("FABRICATED") })
			rehash(t, path)
		}},
		{"field-state", func(path string) {
			editManifest(t, path, func(m *replay.Manifest) { m.Changes[0].OldState = "omitted" })
			rehash(t, path)
		}},
		{"schema", func(path string) {
			editManifest(t, path, func(m *replay.Manifest) { m.Schema = "readmit-run/v99" })
			rehash(t, path)
		}},
		{"incomplete", func(path string) {
			editManifest(t, path, func(m *replay.Manifest) { m.State = "in_progress" })
			rehash(t, path)
		}},
		{"invented-timeout", func(path string) {
			data, err := os.ReadFile(filepath.Join(path, "events.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			var e replay.Event
			if err := json.Unmarshal(bytes.TrimSpace(data), &e); err != nil {
				t.Fatal(err)
			}
			e.Outcome = replay.Timeout
			e.Delivery = "uncertain"
			e.TransportError = &replay.TransportError{Phase: "read", Class: "timeout"}
			data, _ = json.Marshal(e, json.Deterministic(true))
			_ = os.WriteFile(filepath.Join(path, "events.jsonl"), append(data, '\n'), 0600)
			rehash(t, path)
		}},
		{"unknown-member", func(path string) {
			name := filepath.Join(path, "manifest.json")
			data, _ := os.ReadFile(name)
			data = bytes.TrimSpace(data)
			data = append(data[:len(data)-1], []byte(",\"ignored_assertion\":true}\n")...)
			_ = os.WriteFile(name, data, 0600)
			rehash(t, path)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "run")
			if err := os.CopyFS(path, os.DirFS(original)); err != nil {
				t.Fatal(err)
			}
			tc.mutate(path)
			if _, err := replay.Open(path); err == nil {
				t.Fatal("corrupt run accepted")
			}
		})
	}
}

func editManifest(t *testing.T, path string, edit func(*replay.Manifest)) {
	t.Helper()
	name := filepath.Join(path, "manifest.json")
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var m replay.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	edit(&m)
	data, err = json.Marshal(m, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

// This independently follows the documented ADR-0002 framing, allowing tests to
// distinguish content hashing from semantic verification rather than relying on
// the production identity implementation to manufacture an expected result.
func rehash(t *testing.T, path string) {
	t.Helper()
	var names []string
	err := filepath.WalkDir(path, func(name string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(path, name)
		if err != nil {
			return err
		}
		if rel != "identity.sha256" {
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(names)
	h := sha256.New()
	h.Write([]byte("readmit-run/v1\n"))
	var size [8]byte
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(path, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		binary.BigEndian.PutUint64(size[:], uint64(len(name)))
		h.Write(size[:])
		h.Write([]byte(name))
		binary.BigEndian.PutUint64(size[:], uint64(len(data)))
		h.Write(size[:])
		h.Write(data)
	}
	if err := os.WriteFile(filepath.Join(path, "identity.sha256"), []byte(hex.EncodeToString(h.Sum(nil))+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceReservationRejectsOversizedPlanBeforeSending(t *testing.T) {
	var framed []byte
	for range 70 {
		framed = append(framed, mllp.Frame(request("LIMIT"))...)
	}
	config := target("127.0.0.1:2575")
	config.MaxACKBytes = 1 << 20
	if _, err := replay.Prepare(caseAt(t, framed), config, replay.Options{}); err == nil || !strings.Contains(err.Error(), "reservation") {
		t.Fatalf("oversized replay was not preflighted: %v", err)
	}
}

func TestOutputFailureAndSourceChangesNeverConnect(t *testing.T) {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	source := caseAt(t, request("SAFE"))
	plan, err := replay.Prepare(source, target(l.Addr().String()), replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replay.Send(context.Background(), plan, t.TempDir(), replay.SendOptions{}); err == nil {
		t.Fatal("existing output overwritten")
	}
	if err := os.WriteFile(filepath.Join(source, "payloads/s0001-e000001.bin"), request("CHANGED"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := replay.Send(context.Background(), plan, filepath.Join(t.TempDir(), "run"), replay.SendOptions{}); err == nil {
		t.Fatal("changed source silently used")
	}
	_ = l.SetDeadline(time.Now().Add(50 * time.Millisecond))
	if c, err := l.Accept(); err == nil {
		_ = c.Close()
		t.Fatal("preflight failure still connected")
	}
}

func TestReplacedSourceDirectoryCannotReceiveNestedRun(t *testing.T) {
	source := caseAt(t, request("REPLACEMENT"))
	plan, err := replay.Prepare(source, target("127.0.0.1:2575"), replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	moved := source + "-original"
	if err := os.Rename(source, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(source, os.DirFS(moved)); err != nil {
		t.Fatal(err)
	}
	replacement, err := bundle.Open(source)
	if err != nil || replacement.Identity != plan.SourceIdentity() {
		t.Fatal("replacement must have exactly the prepared source contents")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	output := filepath.Join(source, "nested-run")
	if _, err := replay.Send(ctx, plan, output, replay.SendOptions{}); err == nil {
		t.Fatal("copied replacement bypassed immutable source containment")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("rejected run created an output inside the replacement source")
	}
	for _, path := range []string{source, moved} {
		after, err := bundle.Open(path)
		if err != nil || after.Identity != plan.SourceIdentity() {
			t.Fatal("source bytes or bundle identity changed")
		}
	}
}

func TestTimestampShiftPreservesAppointmentIntervalAndNullStates(t *testing.T) {
	message := bytes.Replace(request("TIME"), []byte("^^^20260102120000\r"), []byte("^^^20260102120000^20260102123000~^^^\"\"^\"\"\r"), 1)
	plan, err := replay.Prepare(caseAt(t, message), target("127.0.0.1:2575"), replay.Options{Transformations: []replay.Transformation{{Name: "shift-timestamps", Shift: "-2h"}}})
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := plan.Outbound("o000001")
	if !bytes.Contains(wire, []byte("^^^20260102100000^20260102103000~^^^\"\"^\"\"\r")) || !bytes.Contains(wire, []byte("|20260101100000||")) {
		t.Fatalf("shift altered interval or explicit null: %q", wire)
	}
}

func TestIdentifierScopeAndEscapedEquality(t *testing.T) {
	first := request("DUP")
	equivalent := bytes.Replace(request(`D\X55\P`), []byte("SYNTHETIC|LAB"), []byte(`SYNTHETIC|L\X41\B`), 1)
	otherScope := bytes.Replace(request("DUP"), []byte("SYNTHETIC|LAB"), []byte("SYNTHETIC|OTHER"), 1)
	plan, err := replay.Prepare(caseAt(t, first, equivalent, otherScope), target("127.0.0.1:2575"), replay.Options{Transformations: []replay.Transformation{{Name: "rebase-control-ids"}}})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"READMIT000001", "READMIT000001", "READMIT000002"} {
		wire, _ := plan.Outbound([]string{"o000001", "o000002", "o000003"}[i])
		if !bytes.Contains(wire, []byte("|"+want+"|")) {
			t.Fatalf("scoped equality changed: %q", wire)
		}
	}
}
