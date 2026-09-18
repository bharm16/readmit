package tests

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/report"
)

func TestArtifactCommandsRefuseOutputsInsideSealedPackets(t *testing.T) {
	packet := filepath.Join(t.TempDir(), "packet")
	if _, err := report.Create(context.Background(), report.Scenario, packet); err != nil {
		t.Fatal(err)
	}
	before := reportFileHashes(t, packet)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	target := filepath.Join(t.TempDir(), "target.json")
	redactJSON(t, target, replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: listener.Addr().String(), Transport: "plain", ConnectTimeout: "100ms", MessageTimeout: "100ms", MaxACKBytes: 4096})
	fixture := "../testdata/fixtures/listen-s12.hl7"
	for _, command := range []string{"capture", "inspect", "diagnose", "replay", "diff", "synth"} {
		t.Run(command, func(t *testing.T) {
			output := filepath.Join(packet, "new-"+command)
			var args []string
			switch command {
			case "capture":
				args = []string{command, fixture, "--output", output}
			case "inspect":
				args = []string{command, fixture, "--roundtrip", output}
			case "diagnose":
				args = []string{command, filepath.Join(packet, "reproducer"), "--output", output}
			case "replay":
				args = []string{command, filepath.Join(packet, "reproducer"), "--target", target, "--send", "--output", output}
			case "diff":
				args = []string{command, fixture, fixture, "--output", output}
			case "synth":
				args = []string{command, "--seed", "0", "--base-time", "2026-01-01T12:00:00Z", "--generator-version", "readmit-synth-v1", "--profile-version", "readmit-siu-v1", "--output", output}
			}
			if _, _, err := run(t, args...); err == nil {
				t.Error("command accepted an output inside finalized evidence")
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Error("refusal must precede creating any output")
			}
			if !reflect.DeepEqual(before, reportFileHashes(t, packet)) {
				t.Fatal("command changed the sealed packet")
			}
		})
	}
}

func TestArtifactOutputPreservesIncompleteCase(t *testing.T) {
	casePath := filepath.Join(t.TempDir(), "case")
	if _, _, err := run(t, "capture", "../testdata/fixtures/listen-s12.hl7", "--output", casePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(casePath, "identity.sha256")); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(casePath, "payloads", "s0001-e000001.bin")
	output := filepath.Join(casePath, "comparison.txt")
	if _, _, err := run(t, "diff", payload, payload, "--output", output); err == nil {
		t.Fatal("standalone payload comparison wrote into an incomplete case")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("incomplete evidence changed")
	}
}

func TestReceiverRefusesCaseInsensitiveDestinationCollision(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "PROBE")); os.IsNotExist(err) {
		t.Skip("filesystem is case sensitive")
	}
	_, err := receiver.New(receiver.Config{Mode: observation.Fixed, OutputPath: filepath.Join(dir, "CASE"), ObservationPath: filepath.Join(dir, "case"), MaxMessages: 1, MaxFrameBytes: 4096, IdleTimeout: time.Second})
	if err == nil {
		t.Fatal("receiver accepted case-insensitive aliases for its two outputs")
	}
	if _, err := os.Lstat(filepath.Join(dir, "case")); !os.IsNotExist(err) {
		t.Fatal("failed startup left its observation occupying the case destination")
	}
}
