package tests

// This file is the suite's own harness: the generic verbs every test file
// shares — running the executable, reading its machine output strictly,
// starting a receiver, writing a fixture document, naming exit statuses,
// snapshotting a directory, reserving a loopback port, and generating
// test-only key material. Topic files keep only topic-shaped fixtures and
// assertions; a helper with no topic belongs here, not in whichever file
// happened to need it first.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"github.com/bharm16/readmit/internal/testlicense"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testCommand exercises the release binary with explicit ephemeral signed admission.
func testCommand(ctx context.Context, t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	args = append([]string{"--operation-policy", testlicense.New(t)}, args...)
	return exec.CommandContext(ctx, binary, args...)
}

func run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	return runObserved(t, nil, args...)
}

// runObserved keeps the ordinary command path while letting a test distinguish
// signed policy setup from time spent starting and running the executable.
func runObserved(t *testing.T, observe func(policySetup, process time.Duration), args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	started := time.Now()
	cmd := testCommand(ctx, t, args...)
	policySetup := time.Since(started)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	started = time.Now()
	err := cmd.Run()
	if observe != nil {
		observe(policySetup, time.Since(started))
	}
	if ctx.Err() != nil {
		t.Fatal("CLI timed out")
	}
	return stdout.String(), stderr.String(), err
}

func TestObservedRunPreservesCLIResults(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"unknown-command"}} {
		wantOut, wantErr, wantStatus := run(t, args...)
		observed := false
		gotOut, gotErr, gotStatus := runObserved(t, func(policySetup, process time.Duration) {
			observed = true
		}, args...)
		if !observed || gotOut != wantOut || gotErr != wantErr || exitCode(t, gotStatus) != exitCode(t, wantStatus) {
			t.Fatalf("observed command changed result for %q", args[0])
		}
	}
}

// runJSON runs one command whose arguments already select machine-readable
// output and reads that output strictly. An unknown member is a contract
// change, so the suite refuses it through this one reader instead of silently
// ignoring the field everywhere.
func runJSON[T any](t *testing.T, args ...string) (T, string) {
	t.Helper()
	stdout, stderr, err := run(t, args...)
	if err != nil || stderr != "" {
		t.Fatalf("%s: %v %s", args[0], err, stderr)
	}
	return readStrictOutput[T](t, stdout), stdout
}

// readStrictOutput reads a command's machine output strictly.
func readStrictOutput[T any](t *testing.T, stdout string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal([]byte(stdout), &value, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("%s %v", stdout, err)
	}
	return value
}

// readStrictDocument reads one retained JSON document strictly.
func readStrictDocument[T any](t *testing.T, path string) T {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value T
	if err := json.Unmarshal(raw, &value, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	return value
}

// exitRefused is the CLI's refusal status: a named, deliberate "no" from an
// execution or configuration check. It is never an assertion failure and
// never a pass.
const exitRefused = 2

func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var status *exec.ExitError
	if !errors.As(err, &status) {
		t.Fatalf("command did not exit with a status: %v", err)
	}
	return status.ExitCode()
}

// startedReceiver is one receiver command (listen, collect, test …) after its
// readiness line: the loopback address it printed, the rest of its stdout,
// and its buffered diagnostics. The receiver owns its context: cancellation
// when the test ends is registered here, and a graceful completion is taken
// with wait.
type startedReceiver struct {
	command    *exec.Cmd
	cancel     context.CancelFunc
	address    string
	readyLine  string
	stdout     *bufio.Reader
	diagnostic *bytes.Buffer
	waited     bool
}

// startReceiver starts one receiver command and returns it once its first
// stdout line names the address it bound. The product behaviour this couples
// to is the readiness line itself: "Listening: <address>".
func startReceiver(t *testing.T, timeout time.Duration, name string, args ...string) *startedReceiver {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	command := testCommand(ctx, t, append([]string{name}, args...)...)
	diagnostic := &bytes.Buffer{}
	command.Stderr = diagnostic
	pipe, err := command.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	receiver := &startedReceiver{command: command, cancel: cancel, stdout: bufio.NewReader(pipe), diagnostic: diagnostic}
	t.Cleanup(func() {
		receiver.cancel()
		if !receiver.waited {
			_ = receiver.command.Wait()
		}
	})
	line, err := receiver.stdout.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "Listening: ") {
		t.Fatalf("%s receiver not ready: %q %v %s", name, line, err, receiver.diagnostics(t))
	}
	receiver.readyLine = line
	receiver.address = strings.TrimSpace(strings.TrimPrefix(line, "Listening: "))
	return receiver
}

// diagnostics stops the receiver and returns what it wrote to stderr. os/exec
// fills that buffer from its own copying goroutine, so it is read only once
// the process has exited.
func (r *startedReceiver) diagnostics(t *testing.T) string {
	t.Helper()
	_ = r.command.Process.Kill()
	_ = r.command.Wait()
	r.waited = true
	return r.diagnostic.String()
}

// kill stops the receiver the way a crash does, without any graceful
// shutdown, and waits so the bytes it retained are settled.
func (r *startedReceiver) kill(t *testing.T) {
	t.Helper()
	if err := r.command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = r.command.Wait()
	r.waited = true
}

// startup consumes the first lines the receiver printed after its address,
// which state the configuration it runs under.
func (r *startedReceiver) startup(t *testing.T, lines int) string {
	t.Helper()
	var text strings.Builder
	for range lines {
		line, err := r.stdout.ReadString('\n')
		if err != nil {
			t.Fatalf("receiver startup output ended early: %v %s", err, r.diagnostics(t))
		}
		text.WriteString(line)
	}
	return text.String()
}

// wait drains the receiver's remaining stdout, waits for the exit its own
// graceful completion produced, and returns what it drained.
func (r *startedReceiver) wait(t *testing.T) string {
	t.Helper()
	remaining, _ := io.ReadAll(r.stdout)
	if err := r.command.Wait(); err != nil {
		t.Fatalf("receiver: %v %s", err, r.diagnostic.String())
	}
	r.waited = true
	return string(remaining)
}

// writeDocument writes one fixture document under mode 0600 and returns its
// path, so permissions and destination joining are decided once.
func writeDocument(t *testing.T, dir, name, document string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// mustRead reads a file a test or a command wrote.
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// treeOf snapshots every file under a directory by its slash-separated
// relative path, the one path grammar a "this tree did not change" assertion
// uses regardless of platform.
func treeOf(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = raw
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// treeDigests digests the same snapshot treeOf takes, so a large tree is
// compared without holding every byte twice.
func treeDigests(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	for name, data := range treeOf(t, root) {
		sum := sha256.Sum256(data)
		files[name] = hex.EncodeToString(sum[:])
	}
	return files
}

// freeLoopbackAddress reserves a loopback port and releases it, so a command
// that has to be told its own address in advance still binds only loopback
// and still closes its listener inside the test. The network selects the
// loopback family the caller must match when it dials.
func freeLoopbackAddress(t *testing.T, network string) string {
	t.Helper()
	host := "127.0.0.1"
	if network == "tcp6" {
		host = "[::1]"
	}
	listener, err := net.Listen(network, host+":0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	return address
}

// serialNumber generates one certificate serial number.
func serialNumber(t *testing.T) *big.Int {
	t.Helper()
	number, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 96))
	if err != nil {
		t.Fatal(err)
	}
	return number
}

// testOnlyPair writes a PEM authority, a leaf certificate it issued, and that
// leaf's key; server-role leaves also carry the loopback address and any
// declared DNS names. Every file is generated by the test that uses it, under
// a temporary directory it removes, and none is committed.
func testOnlyPair(t *testing.T, dir, role string, dnsNames ...string) (authorityFile, certificateFile, keyFile string) {
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
	subject := "readmit test-only " + role
	leaf := &x509.Certificate{
		SerialNumber: serialNumber(t), Subject: pkix.Name{CommonName: subject},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature,
	}
	if role == "server" {
		usage = x509.ExtKeyUsageServerAuth
		leaf.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		leaf.DNSNames = dnsNames
		if len(dnsNames) > 0 {
			// A named server presents its name as the subject, the way a real
			// endpoint's certificate does.
			leaf.Subject = pkix.Name{CommonName: dnsNames[0]}
		}
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
	// The stand-in store this pair feeds prints the file it is given and one
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

// loadPair reads a certificate and key into one TLS pair.
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

// sha256Hex is the identity readmit names exact bytes by: their SHA-256, in
// lowercase hexadecimal.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
