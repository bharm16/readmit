package hub_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"github.com/bharm16/readmit/internal/testlicense"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

// Executables are selected explicitly for candidate acceptance; ordinary local
// integration builds isolated binaries. The candidate CLI is never substituted.
func journeyExecutable(t *testing.T, variable, module, name string) string {
	t.Helper()
	if path := os.Getenv(variable); path != "" {
		data, err := os.ReadFile(path)
		info, statErr := os.Lstat(path)
		if err != nil || statErr != nil || !info.Mode().IsRegular() || !filepath.IsAbs(path) || fmt.Sprintf("%x", sha256.Sum256(data)) != os.Getenv(variable+"_SHA256") {
			t.Fatal("selected acceptance executable identity refused")
		}
		return path
	}
	path := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-ldflags", "-X github.com/bharm16/readmit/internal/engine.version="+engine.Version(), "-o", path, "./cmd/"+name)
	cmd.Dir = module
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	return path
}

func TestPublicRunnerAndHubProcessesExecuteAndRefuseReuse(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	cli := journeyExecutable(t, "READMIT_ACCEPTANCE_BINARY", "..", "readmit")
	service := journeyExecutable(t, "READMIT_HUB_TEST_BINARY", ".", "readmit-hub")
	cert := certificates(t, &c)
	_, policy, _, accessPath := accessFixture(t)
	token := "rh_" + base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("j", 32)))
	policy.Tokens = []hub.ScopedToken{{Hash: fmt.Sprintf("%x", sha256.Sum256([]byte(token))), Subject: "runner", Project: "alpha", Actions: []string{"enrollment", "execution"}, Expires: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Certificate: fmt.Sprintf("%x", sha256.Sum256(cert.Certificate[0])), Kind: "runner"}}
	writePolicy(t, accessPath, policy)
	dir := t.TempDir()
	write := func(name string, value any) string {
		t.Helper()
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		if err = os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	runners := write("runners.json", runnerprotocol.Policy{Schema: "readmit-runner-policy/v1", Runners: []runnerprotocol.Grant{{Project: "alpha", Subject: "runner", Environment: "lab", Engine: engine.Version(), Spec: "readmit-test/v1", Profile: "readmit-siu-v1", MaxSeconds: 30, MaxJobs: 4}}})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c.Listen = listener.Addr().String()
	listener.Close()
	c.Schema = "readmit-hub-config/v1"
	config := write("hub.json", c)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if output, err := exec.CommandContext(ctx, service, "-config", config, "migrate").CombinedOutput(); err != nil {
		t.Fatalf("migrate: %v %s", err, output)
	}
	operationPolicy := testlicense.New(t)
	hubOperation := write("hub-operation.json", hub.OperationPolicy{Schema: "readmit-hub-operation-policy/v1", Policy: operationPolicy, Bindings: []hub.OperationBinding{}})
	process := exec.CommandContext(ctx, service, "-operation-policy", hubOperation, "-config", config, "-access-policy", accessPath, "-runner-policy", runners, "serve")
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { process.Process.Kill(); process.Wait() })
	roots := x509.NewCertPool()
	ca, err := os.ReadFile(c.ClientCA)
	if err != nil {
		t.Fatal(err)
	}
	roots.AppendCertsFromPEM(ca)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	ready := time.Now().Add(10 * time.Second)
	for {
		r, e := client.Get("https://" + c.Listen + "/health/ready")
		if e == nil {
			r.Body.Close()
			if r.StatusCode == 204 {
				break
			}
		}
		if time.Now().After(ready) {
			t.Fatal("service did not become ready")
		}
		time.Sleep(25 * time.Millisecond)
	}
	// Native public service has a deliberate ten-second restart lease barrier.
	time.Sleep(10 * time.Second)
	clientPath := filepath.Join(dir, "client.pem")
	keyPath := filepath.Join(dir, "key.pem")
	tokenPath := filepath.Join(dir, "token")
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for path, b := range map[string][]byte{clientPath: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), keyPath: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), tokenPath: []byte(token)} {
		if err = os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	provider, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reference := func(path string) customerrunner.Reference {
		return customerrunner.Reference{Command: provider, Arguments: []string{"-test.run=^TestRunnerProvider$", "--", "--runner-fixture", path}}
	}
	root := filepath.Join(dir, "runs")
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	runnerConfig := write("runner.json", customerrunner.Config{Schema: "readmit-runner/v1", Hub: "https://" + c.Listen, Project: "alpha", Environment: "lab", Root: root, CA: c.ClientCA, Certificate: clientPath, Key: reference(keyPath), Token: reference(tokenPath), UpdateEngine: "next", UpdateKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	peer, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	received := make(chan []byte, 1)
	go func() {
		conn, e := peer.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(10 * time.Second))
		reader := bufio.NewReader(io.LimitReader(conn, 65536))
		data, e := reader.ReadBytes(0x1c)
		if e != nil {
			return
		}
		terminator, e := reader.ReadByte()
		if e != nil || terminator != '\r' {
			return
		}
		received <- append(data, terminator)
		fmt.Fprint(conn, "\x0bMSH|^~\\&|INDEPENDENT|LAB|READMIT|TEST|20260101120000||ACK|ACK-109|P|2.5.1\rMSA|AA|LISTEN-BOOK\r\x1c\r")
	}()
	spec := runnerSpec(t, dir, peer.Addr().String())
	job := write("job.json", customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "accepted", Spec: spec})
	command := func(args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, cli, append([]string{"--operation-policy", operationPolicy}, args...)...).CombinedOutput()
	}
	output, err := command("runner", "execute", job, "--config", runnerConfig, "--send")
	if err != nil {
		t.Fatalf("runner: %v %s", err, output)
	}
	select {
	case data := <-received:
		expected, e := os.ReadFile("../testdata/fixtures/listen-s12.hl7")
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(data, append(append([]byte{0x0b}, expected...), 0x1c, '\r')) {
			t.Fatal("independent peer received wrong message")
		}
	case <-ctx.Done():
		t.Fatal("no target bytes")
	}
	output, err = command("run", "status", filepath.Join(root, "accepted", "run"))
	if err != nil || !strings.HasPrefix(string(output), "Run state: passed\n") {
		t.Fatalf("retained verdict: %v %s", err, output)
	}
	if _, err = command("runner", "execute", job, "--config", runnerConfig, "--send"); err == nil {
		t.Fatal("same job executed twice")
	}
	// Revocation must prevent a new claim and target connection, not just hide a verdict.
	policy.Tokens = nil
	writePolicy(t, accessPath, policy)
	revoked := write("revoked.json", customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "revoked", Spec: spec})
	if _, err = command("runner", "execute", revoked, "--config", runnerConfig, "--send"); err == nil {
		t.Fatal("revoked credential executed")
	}
	if _, err = os.Stat(filepath.Join(root, "revoked")); !os.IsNotExist(err) {
		t.Fatal("revoked job claimed work", err)
	}
	peer.(*net.TCPListener).SetDeadline(time.Now().Add(150 * time.Millisecond))
	if extra, e := peer.Accept(); e == nil {
		extra.Close()
		t.Fatal("refused job sent bytes")
	}
}
