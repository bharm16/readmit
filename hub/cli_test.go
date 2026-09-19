package hub_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json/v2"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestInstalledExecutableAndInterruptedRestore(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	binary := os.Getenv("READMIT_HUB_TEST_BINARY")
	if binary == "" {
		binary = filepath.Join(t.TempDir(), "readmit-hub")
		cmd := exec.Command("go", "build", "-o", binary, "./cmd/readmit-hub")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v %s", err, output)
		}
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	cert := certificates(t, &c)
	c.Schema = "readmit-hub-config/v1"
	c.Listen = "127.0.0.1:8443"
	payload := bytes.Repeat([]byte("synthetic-only-96\r"), 4096)
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	s := open(t, c)
	if err = s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = s.Put(context.Background(), digest, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "snapshot")
	if err = s.Backup(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	s.Close()
	reset(t, db)
	c.Root = filepath.Join(t.TempDir(), "restored")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c.Listen = listener.Addr().String()
	listener.Close()
	config := filepath.Join(t.TempDir(), "config.json")
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		args = append([]string{"-config", config}, args...)
		cmd := exec.Command(binary, args...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %v: %v %s", args, err, output)
		}
	}
	run("migrate")
	// Enforce a real OS short-write boundary rather than mocking storage. The
	// failed process must never publish a partial object under its final digest.
	interrupted := exec.Command("/bin/sh", "-c", `ulimit -f 1; exec "$1" -config "$2" -directory "$3" restore`, "restore-test", binary, config, backup)
	if err = interrupted.Run(); err == nil {
		t.Fatal("file-size-limited restore unexpectedly succeeded")
	}
	if _, err = os.Stat(filepath.Join(c.Root, digest)); !os.IsNotExist(err) {
		t.Fatal("interrupted restore published incomplete object", err)
	}
	run("-directory", backup, "restore")
	run("check")
	process := exec.Command(binary, "-config", config, "serve")
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			process.Process.Kill()
			process.Wait()
		}
	})
	roots := x509.NewCertPool()
	ca, _ := os.ReadFile(c.ClientCA)
	roots.AppendCertsFromPEM(ca)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	url := "https://" + c.Listen
	deadline := time.Now().Add(10 * time.Second)
	for {
		r, e := client.Get(url + "/health/ready")
		if e == nil {
			r.Body.Close()
			if r.StatusCode == 204 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("installed service failed to become ready", e)
		}
		time.Sleep(25 * time.Millisecond)
	}
	r, err := client.Get(url + "/v1/artifacts/" + digest)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil || r.StatusCode != 200 || !bytes.Equal(got, payload) {
		t.Fatal("installed service did not preserve restored evidence")
	}
	if err = process.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- process.Wait() }()
	select {
	case err = <-wait:
		stopped = true
		if err != nil {
			t.Fatal("unclean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not release service")
	}
	run("check")
}
