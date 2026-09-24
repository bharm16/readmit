package tests

import (
	"bytes"
	"context"
	"fmt"
	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/mllp"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/testlicense"
)

func rawOperation(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	return output.String(), err
}

func TestOperationAdmissionMissingAndReleasedRefuseAuthoring(t *testing.T) {
	// No policy named means this computer's license, so the account is one
	// with none installed rather than whoever runs the suite.
	isolateInstalledLicense(t)
	for _, policy := range []string{"", testlicense.New(t)} {
		args := []string{}
		if policy != "" {
			args = []string{"--operation-policy", policy}
			if out, err := rawOperation(t, append(args, "license", "operation", "release")...); err != nil {
				t.Fatalf("release: %v %s", err, out)
			}
		}
		output := filepath.Join(t.TempDir(), "new-project")
		out, err := rawOperation(t, append(args, "project", "init", "--title", "test", "--interface-version", "v1", "--output", output)...)
		if err == nil {
			t.Fatalf("unlicensed authoring accepted: %s", out)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("refusal created output")
		}
		out, err = rawOperation(t, append(args, "inspect", "../testdata/fixtures/adt-cr.hl7")...)
		if err != nil || !strings.Contains(out, "Messages: 1") {
			t.Fatalf("retained read gated: %v %s", err, out)
		}
	}
}

func TestOperationAdmissionExplicitActivationAllowsAuthoring(t *testing.T) {
	policy := testlicense.New(t)
	output := filepath.Join(t.TempDir(), "project")
	out, err := rawOperation(t, "--operation-policy", policy, "project", "init", "--title", "test", "--interface-version", "v1", "--output", output)
	if err != nil {
		t.Fatalf("activated authoring: %v %s", err, out)
	}
}

func TestOperationAdmissionFailureReleasesRunner(t *testing.T) {
	policy := testlicense.New(t)
	out, err := rawOperation(t, "--operation-policy", policy, "run", "start", filepath.Join(t.TempDir(), "absent"), "--send", "--output", filepath.Join(t.TempDir(), "run"))
	if err == nil {
		t.Fatal("missing spec accepted")
	}
	data, e := os.ReadFile(filepath.Join(filepath.Dir(policy), "admissions.json"))
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(data, []byte(`"released"`)) {
		t.Fatalf("failed operation leaked admission: %s; %s", data, out)
	}
}

func TestUnactivatedFrozenSampleRefusesArbitraryInputs(t *testing.T) {
	root := t.TempDir()
	out, err := rawOperation(t, "sample", "synth", "--output", filepath.Join(root, "family"))
	if err != nil {
		t.Fatal(err, out)
	}
	if out, err = rawOperation(t, "sample", "synth", "--seed", "1", "--output", filepath.Join(root, "other")); err == nil {
		t.Fatal("sample accepted a new seed", out)
	}
	if out, err = rawOperation(t, "sample", "index", filepath.Join(root, "family", "invalid"), "--output", filepath.Join(root, "index.json")); err == nil {
		t.Fatal("sample indexed another case", out)
	}
	fixtures := t.TempDir()
	for _, name := range []string{"listen-s12.hl7", "listen-s13.hl7"} {
		if err := os.WriteFile(filepath.Join(fixtures, name), []byte("customer message"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if out, err = rawOperation(t, "sample", "capture", "--fixtures", fixtures, "--output", filepath.Join(root, "capture")); err == nil {
		t.Fatal("sample imported arbitrary bytes", out)
	}
	if _, err = os.Stat(filepath.Join(root, "capture")); !os.IsNotExist(err) {
		t.Fatal("refused sample left capture")
	}
}

func TestUnactivatedCIAdmissionRetainsMachineReadableFailure(t *testing.T) {
	isolateInstalledLicense(t)
	out, err := rawOperation(t, "suite", "ci", "absent", "--send", "--environment", "lab", "--output", filepath.Join(t.TempDir(), "run"))
	if err == nil || !strings.Contains(out, `"schema":"readmit-suite-ci/v1"`) || !strings.Contains(out, `"exit_code":2`) {
		t.Fatalf("CI admission result: %v %s", err, out)
	}
}

func TestParallelProcessesSendOnceAndSettleSharedAdmissions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan struct{}, 8)
	finish := make(chan struct{})
	var sends atomic.Int32
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(15 * time.Second))
				reader, _ := mllp.NewReader(conn, 4096)
				if _, err := reader.ReadFrame(); err != nil {
					return
				}
				sends.Add(1)
				received <- struct{}{}
				select {
				case <-finish:
				case <-ctx.Done():
					return
				}
				fmt.Fprint(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|A|P|2.5.1\rMSA|AA|LISTEN-BOOK\r\x1c\r")
			}()
		}
	}()
	spec, _ := durableSpec(t, listener.Addr().String())
	policy := testlicense.New(t)
	results := make(chan error, 2)
	for range 2 {
		output := filepath.Join(t.TempDir(), "result")
		go func() {
			raw, err := exec.CommandContext(ctx, binary, "--operation-policy", policy, "test", spec, "--send", "--output", output).CombinedOutput()
			if err != nil {
				err = fmt.Errorf("%w: %s", err, raw)
			}
			results <- err
		}()
	}
	for range 2 {
		select {
		case <-received:
		case err := <-results:
			t.Fatalf("process ended before both sends: %v", err)
		case <-ctx.Done():
			t.Fatal("parallel executions did not reach fixture")
		}
	}
	close(finish)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if sends.Load() != 2 {
		t.Fatalf("metadata retries repeated execution: %d sends", sends.Load())
	}
	record, err := entitlement.OpenAdmissions(filepath.Join(filepath.Dir(policy), "admissions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Record.Admissions) != 2 {
		t.Fatalf("two processes retained %d admissions", len(record.Record.Admissions))
	}
	for _, held := range record.Record.Admissions {
		if held.StateAt(time.Now()) != entitlement.AdmissionReleased {
			t.Fatal("completed process retained active capacity")
		}
	}
}

func TestProfilePortabilityRemainsFreeWithMissingActivation(t *testing.T) {
	root := t.TempDir()
	packet := filepath.Join(root, "profile-package.json")
	args := []string{"--operation-policy", filepath.Join(root, "missing"), "profile", "export", "../testdata/fixtures/local-profile.json", "--pack", "../testdata/fixtures/profile-pack.json", "--version", "../testdata/fixtures/profile-version.json", "--origin", "../testdata/fixtures/profile-origin.json", "--output", packet, "--reviewed"}
	if out, err := rawOperation(t, args...); err != nil {
		t.Fatal("verified export was gated", err, out)
	}
	if out, err := rawOperation(t, "--operation-policy", filepath.Join(root, "missing"), "profile", "import", packet, "--output", filepath.Join(root, "imported")); err != nil {
		t.Fatal("immutable import was gated", err, out)
	}
}
