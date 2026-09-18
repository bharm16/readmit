//go:build !windows

package replay_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/replay"
)

func TestConfigurationAndCARejectFIFOWithoutOpening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := replay.ReadTarget(path); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("target FIFO blocked")
	}
	config := target("127.0.0.1:2575")
	config.Transport = "tls"
	config.CAFile = path
	source := caseAt(t, request("FIFO"))
	go func() { _, err := replay.Prepare(source, config, replay.Options{}); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("CA FIFO accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("CA FIFO blocked")
	}
	// A symlink to a FIFO must receive the same preliminary file-type check.
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	go func() { _, err := replay.ReadTarget(alias); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO alias accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO alias blocked")
	}
}

func TestRelativeCAReferencePreservesPhysicalParentTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "physical", "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "physical", "nested"), filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	_, selectedCA := certificate(t)
	_, unselectedCA := certificate(t)
	for name, raw := range map[string][]byte{"physical/ca.pem": selectedCA, "ca.pem": unselectedCA} {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	config := target("127.0.0.1:2575")
	config.Transport, config.CAFile = "tls", "alias/../ca.pem"
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "target.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := replay.ReadTarget(path)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := replay.Prepare(caseAt(t, request("CA-REFERENCE")), loaded, replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(selectedCA)
	if plan.Target().CASHA256 != hex.EncodeToString(want[:]) {
		t.Fatal("replay prepared the lexical sibling instead of the explicitly selected CA")
	}
}
