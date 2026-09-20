package tests

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/transportsecurity"
)

// freeLoopbackAddress reserves a loopback port and releases it, so a command
// that has to be told its own address in advance still binds only loopback and
// still closes its listener inside the test.
func freeLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	return address
}

// capturing starts one collect command and returns it with the address it
// printed, so a test drives the executable rather than the package.
type capturing struct {
	command    *exec.Cmd
	address    string
	stdout     *bufio.Reader
	diagnostic *bytes.Buffer
}

func startCapture(t *testing.T, ctx context.Context, args ...string) *capturing {
	t.Helper()
	command := testCommand(ctx, t, append([]string{"collect"}, args...)...)
	diagnostic := &bytes.Buffer{}
	command.Stderr = diagnostic
	pipe, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	reader := bufio.NewReader(pipe)
	capture := &capturing{command: command, stdout: reader, diagnostic: diagnostic}
	line, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "Listening: ") {
		t.Fatalf("collector not ready: %q %v %s", line, err, capture.diagnostics(t))
	}
	capture.address = strings.TrimSpace(strings.TrimPrefix(line, "Listening: "))
	return capture
}

// diagnostics stops the command and returns what it wrote to stderr. os/exec
// fills that buffer from its own copying goroutine, so it is read only once the
// process has exited.
func (c *capturing) diagnostics(t *testing.T) string {
	t.Helper()
	_ = c.command.Process.Kill()
	_ = c.command.Wait()
	return c.diagnostic.String()
}

func (c *capturing) startup(t *testing.T, lines int) string {
	t.Helper()
	var text strings.Builder
	for range lines {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			t.Fatalf("collector startup output ended early: %v %s", err, c.diagnostics(t))
		}
		text.WriteString(line)
	}
	return text.String()
}

func captureFrame(control string) []byte {
	return mllp.Frame([]byte(strings.Replace(collectADT, "COLLECT-001", control, 1)))
}

// TestCollectServesConcurrentPeersAndFinalizesItsJournal drives the new
// capacity flags through the executable: two peers at once, a declared message
// budget, and a journal that finalizes and reads back.
func TestCollectServesConcurrentPeersAndFinalizesItsJournal(t *testing.T) {
	dir := t.TempDir()
	output, journal := filepath.Join(dir, "case"), filepath.Join(dir, "journal")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	capture := startCapture(t, ctx, "--address", "127.0.0.1:0", "--policy", policyFile(t, dir, collectAnyPolicy),
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
	remaining, _ := io.ReadAll(capture.stdout)
	if err := capture.command.Wait(); err != nil {
		t.Fatalf("collect: %v %s", err, capture.diagnostic.String())
	}
	summary := string(remaining)
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	capture := startCapture(t, ctx, "--address", "127.0.0.1:0", "--policy", policyFile(t, dir, collectAnyPolicy),
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
	if err := capture.command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = capture.command.Wait()
	if _, err := os.Stat(output); err == nil {
		t.Fatal("a killed capture sealed a case bundle")
	}
	retained, err := os.ReadFile(filepath.Join(journal, "received", "s0001-e000001.bin"))
	if err != nil || !strings.Contains(string(retained), "INTERRUPTED-001") {
		t.Fatalf("the frame received before the kill was lost: %q %v", retained, err)
	}
	before := journalListing(t, journal)
	stdout, stderr, err := run(t, "collect", "status", journal)
	if exitCode(t, err) != 2 {
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
	address := freeLoopbackAddress(t)
	serverCA, serverCertificate, serverKey := testOnlyLoopbackPair(t, dir, "server")
	clientCA, clientCertificate, clientKey := testOnlyLoopbackPair(t, dir, "client")
	store := filepath.Join(dir, "secrets.json")
	if _, stderr, err := run(t, "secret", "add", "--secrets", store, "--name", credentialReferences,
		"--store", "os-keychain", "--address", address, "--command", providerCommand(t), "--argument", serverKey); err != nil {
		t.Fatalf("secret add: %v %s", err, stderr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	capture := startCapture(t, ctx, "--address", address, "--policy", policyFile(t, dir, collectAnyPolicy),
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
	remaining, _ := io.ReadAll(capture.stdout)
	if err := capture.command.Wait(); err != nil {
		t.Fatalf("collect: %v %s", err, capture.diagnostic.String())
	}
	if !strings.Contains(string(remaining), "Received frames: 1") {
		t.Fatalf("the secured frame was not captured: %q", remaining)
	}
	material, err := os.ReadFile(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.SplitN(strings.TrimSpace(string(material)), "\n", 2)[1]
	for where, text := range map[string]string{"stdout": startup + string(remaining), "stderr": capture.diagnostic.String()} {
		if strings.Contains(text, body) {
			t.Fatalf("collect disclosed key material on %s", where)
		}
	}
}

// testOnlyLoopbackPair writes a PEM authority, a leaf certificate it issued for
// loopback, and that leaf's key. Every file is generated by this test, under a
// temporary directory it removes, and none is committed.
func testOnlyLoopbackPair(t *testing.T, dir, role string) (authorityFile, certificateFile, keyFile string) {
	t.Helper()
	authorityKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	authority := &x509.Certificate{
		SerialNumber: serialNumber(t), Subject: pkix.Name{CommonName: "readmit test-only " + role + " authority"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, BasicConstraintsValid: true, IsCA: true,
	}
	authorityDER, err := x509.CreateCertificate(rand.Reader, authority, authority, authorityKey.Public(), authorityKey)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := x509.ParseCertificate(authorityDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	usage := x509.ExtKeyUsageClientAuth
	leaf := &x509.Certificate{
		SerialNumber: serialNumber(t), Subject: pkix.Name{CommonName: "readmit test-only " + role},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature,
	}
	if role == "server" {
		usage = x509.ExtKeyUsageServerAuth
		leaf.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	leaf.ExtKeyUsage = []x509.ExtKeyUsage{usage}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, signer, leafKey.Public(), authorityKey)
	if err != nil {
		t.Fatal(err)
	}
	encodedKey, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	authorityFile = filepath.Join(dir, role+"-authority.pem")
	certificateFile = filepath.Join(dir, role+"-certificate.pem")
	keyFile = filepath.Join(dir, role+"-key.pem")
	// The store this test stands in for prints the file it is given and one
	// trailing line ending is removed, so the key file carries an extra one.
	for name, data := range map[string][]byte{
		authorityFile:   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: authorityDER}),
		certificateFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		keyFile:         append(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: encodedKey}), '\n'),
	} {
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return authorityFile, certificateFile, keyFile
}

func loadPair(t *testing.T, certificateFile, keyFile string) tls.Certificate {
	t.Helper()
	certificate, err := os.ReadFile(certificateFile)
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(certificate, key)
	if err != nil {
		t.Fatal(err)
	}
	return pair
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
