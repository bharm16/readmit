package tests

import (
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/transportsecurity"
)

func captureFrame(control string) []byte {
	return mllp.Frame([]byte(strings.Replace(collectADT, "COLLECT-001", control, 1)))
}

// TestCollectServesConcurrentPeersAndFinalizesItsJournal drives the new
// capacity flags through the executable: two peers at once, a declared message
// budget, and a journal that finalizes and reads back.
func TestCollectServesConcurrentPeersAndFinalizesItsJournal(t *testing.T) {
	dir := t.TempDir()
	output, journal := filepath.Join(dir, "case"), filepath.Join(dir, "journal")
	capture := startReceiver(t, 10*time.Second, "collect", "--address", "127.0.0.1:0", "--policy", policyFile(t, dir, collectAnyPolicy),
		"--output", output, "--journal", journal, "--max-connections", "2", "--max-messages", "2",
		"--max-sessions", "2", "--max-capture-bytes", "4194304", "--idle-timeout", "3s")
	startup := capture.startup(t, 9)
	for _, expected := range []string{"Transport: plain", "Client certificate: none", "Concurrent connections: 2", "Capture journal: enabled"} {
		if !strings.Contains(startup, expected) {
			t.Fatalf("startup output did not state %q: %q", expected, startup)
		}
	}
	answers := make([]string, 2)
	var group sync.WaitGroup
	for i := range answers {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			connection, err := net.DialTimeout("tcp", capture.address, 3*time.Second)
			if err != nil {
				t.Errorf("peer %d could not connect: %v", i, err)
				return
			}
			defer connection.Close()
			if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Errorf("peer %d: %v", i, err)
				return
			}
			if _, err := connection.Write(captureFrame([]string{"PEER-001", "PEER-002"}[i])); err != nil {
				t.Errorf("peer %d could not send: %v", i, err)
				return
			}
			reader, _ := mllp.NewReader(connection, 1<<20)
			frame, err := reader.ReadFrame()
			if err != nil {
				t.Errorf("peer %d was not answered: %v", i, err)
				return
			}
			answers[i] = string(frame)
		}(i)
	}
	group.Wait()
	for i, control := range []string{"PEER-001", "PEER-002"} {
		if !strings.Contains(answers[i], control) {
			t.Fatalf("peer %d received %q", i, answers[i])
		}
	}
	summary := capture.wait(t)
	for _, expected := range []string{"Capture state: finalized", "Acknowledgements sent: 2", "Acknowledgements unsent: 0",
		"Acknowledgements uncertain: 0", "Collected sessions: 2", "Received frames: 2"} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("capture summary did not state %q: %q", expected, summary)
		}
	}
	if capture.diagnostic.Len() != 0 {
		t.Fatalf("collect wrote diagnostics: %s", capture.diagnostic)
	}
	stdout, stderr, err := run(t, "collect", "status", journal, "--json")
	if err != nil || stderr != "" {
		t.Fatalf("collect status: %v %s", err, stderr)
	}
	for _, expected := range []string{`"state":"finalized"`, `"received":2`, `"acknowledged":2`, `"unsent":0`, `"uncertain":0`, `"recovered":false`} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("recovery did not state %q: %q", expected, stdout)
		}
	}
}

// TestCollectStatusRecoversAnInterruptedCaptureWithoutResending is the honest
// crash: the process is killed while capturing, so nothing finalizes. Recovery
// must show the frame that was received, must not call the capture finished,
// and must not send anything.
func TestCollectStatusRecoversAnInterruptedCaptureWithoutResending(t *testing.T) {
	dir := t.TempDir()
	output, journal := filepath.Join(dir, "case"), filepath.Join(dir, "journal")
	capture := startReceiver(t, 10*time.Second, "collect", "--address", "127.0.0.1:0", "--policy", policyFile(t, dir, collectAnyPolicy),
		"--output", output, "--journal", journal, "--idle-timeout", "5s")
	connection, err := net.DialTimeout("tcp", capture.address, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write(captureFrame("INTERRUPTED-001")); err != nil {
		t.Fatal(err)
	}
	reader, _ := mllp.NewReader(connection, 1<<20)
	if _, err := reader.ReadFrame(); err != nil {
		t.Fatal(err)
	}
	// A kill, not a cancellation: nothing runs afterwards in that process.
	capture.kill(t)
	if _, err := os.Stat(output); err == nil {
		t.Fatal("a killed capture sealed a case bundle")
	}
	retained, err := os.ReadFile(filepath.Join(journal, "received", "s0001-e000001.bin"))
	if err != nil || !strings.Contains(string(retained), "INTERRUPTED-001") {
		t.Fatalf("the frame received before the kill was lost: %q %v", retained, err)
	}
	before := journalListing(t, journal)
	stdout, stderr, err := run(t, "collect", "status", journal)
	if exitCode(t, err) != exitRefused {
		t.Fatalf("an interrupted capture did not exit 2: %v %s", err, stderr)
	}
	if strings.Contains(stdout, "finalized") {
		t.Fatalf("an interrupted capture was reported as finalized: %q", stdout)
	}
	for _, expected := range []string{"Received frames: 1", "Recovery never sends, resends or resumes."} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("recovery did not state %q: %q", expected, stdout)
		}
	}
	if after := journalListing(t, journal); after != before {
		t.Fatalf("recovery changed retained evidence:\n%s\n%s", before, after)
	}
}

// TestCollectRefusesAnIncompleteListenerConfiguration keeps every TLS refusal
// explicit and before anything binds.
func TestCollectRefusesAnIncompleteListenerConfiguration(t *testing.T) {
	dir := t.TempDir()
	declared := policyFile(t, dir, collectAnyPolicy)
	certificate := filepath.Join(dir, "certificate.pem")
	if err := os.WriteFile(certificate, []byte("-----BEGIN CERTIFICATE-----\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, tail := range map[string][]string{
		"certificate without a key reference":    {"--tls-certificate", certificate},
		"key reference without a store":          {"--tls-certificate", certificate, "--tls-key-reference", credentialReferences},
		"client authority without a certificate": {"--client-ca", certificate},
		"unreadable certificate": {"--tls-certificate", filepath.Join(dir, "absent.pem"),
			"--tls-key-reference", credentialReferences, "--secrets", filepath.Join(dir, "secrets.json")},
	} {
		args := append([]string{"collect", "--address", "127.0.0.1:0", "--policy", declared, "--output", filepath.Join(dir, name)}, tail...)
		stdout, stderr, err := run(t, args...)
		if err == nil {
			t.Fatalf("%s was accepted: %q", name, stdout)
		}
		if stderr == "" {
			t.Fatalf("%s was refused without a diagnostic", name)
		}
	}
}

// TestCollectCapturesOverMutualTLSWithAReferencedKey is the executable's own
// TLS contract: declaring a client authority requires and verifies a client
// certificate that authority issued, the listener's private key is read from
// the store its credential reference names, and no key material appears in any
// output.
func TestCollectCapturesOverMutualTLSWithAReferencedKey(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	dir := t.TempDir()
	address := freeLoopbackAddress(t, "tcp")
	serverCA, serverCertificate, serverKey := testOnlyPair(t, dir, "server")
	clientCA, clientCertificate, clientKey := testOnlyPair(t, dir, "client")
	store := filepath.Join(dir, "secrets.json")
	if _, stderr, err := run(t, "secret", "add", "--secrets", store, "--name", credentialReferences,
		"--store", "os-keychain", "--address", address, "--command", providerCommand(t), "--argument", serverKey); err != nil {
		t.Fatalf("secret add: %v %s", err, stderr)
	}
	capture := startReceiver(t, 10*time.Second, "collect", "--address", address, "--policy", policyFile(t, dir, collectAnyPolicy),
		"--output", filepath.Join(dir, "case"), "--max-messages", "1", "--idle-timeout", "3s",
		"--tls-certificate", serverCertificate, "--tls-key-reference", credentialReferences, "--secrets", store,
		"--client-ca", clientCA)
	startup := capture.startup(t, 9)
	for _, expected := range []string{"Transport: tls", "Client certificate: required"} {
		if !strings.Contains(startup, expected) {
			t.Fatalf("startup output did not state %q: %q", expected, startup)
		}
	}
	roots, err := os.ReadFile(serverCA)
	if err != nil {
		t.Fatal(err)
	}
	config, err := transportsecurity.ClientConfig("127.0.0.1", roots)
	if err != nil {
		t.Fatal(err)
	}
	config.Certificates = []tls.Certificate{loadPair(t, clientCertificate, clientKey)}
	connection, err := tls.Dial("tcp", address, config)
	if err != nil {
		t.Fatalf("a verified client was refused: %v %s", err, capture.diagnostics(t))
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write(captureFrame("SECURED-001")); err != nil {
		t.Fatal(err)
	}
	reader, _ := mllp.NewReader(connection, 1<<20)
	answer, err := reader.ReadFrame()
	if err != nil || !strings.Contains(string(answer), "SECURED-001") {
		t.Fatalf("the secured peer was not acknowledged: %q %v", answer, err)
	}
	remaining := capture.wait(t)
	if !strings.Contains(remaining, "Received frames: 1") {
		t.Fatalf("the secured frame was not captured: %q", remaining)
	}
	material, err := os.ReadFile(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.SplitN(strings.TrimSpace(string(material)), "\n", 2)[1]
	for where, text := range map[string]string{"stdout": startup + remaining, "stderr": capture.diagnostic.String()} {
		if strings.Contains(text, body) {
			t.Fatalf("collect disclosed key material on %s", where)
		}
	}
}

// journalListing describes everything a capture journal holds, so a test can
// state that recovery changed nothing.
func journalListing(t *testing.T, path string) string {
	t.Helper()
	var described strings.Builder
	err := filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			described.WriteString(name + "/\n")
			return nil
		}
		data, readErr := os.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		described.WriteString(name + " " + string(data) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return described.String()
}
