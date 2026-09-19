package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var binary string

func TestMain(m *testing.M) {
	// Re-executed as the stand-in secret store of the credential tests. It must
	// answer before any test binary work happens, so no readmit build is run
	// for it. See tests/secret_test.go for what it emits and why.
	if mode, ok := os.LookupEnv(providerSwitch); ok {
		os.Exit(testOnlyProvider(mode, os.Args[1:]))
	}
	// Acceptance uses the exact archived executable, never a freshly rebuilt
	// substitute. Ordinary tests retain their existing isolated build.
	if selected, ok := os.LookupEnv("READMIT_ACCEPTANCE_BINARY"); ok {
		data, err := os.ReadFile(selected)
		info, statErr := os.Lstat(selected)
		if err != nil || statErr != nil || !info.Mode().IsRegular() || !filepath.IsAbs(selected) ||
			fmt.Sprintf("%x", sha256.Sum256(data)) != os.Getenv("READMIT_ACCEPTANCE_BINARY_SHA256") {
			fmt.Fprintln(os.Stderr, "acceptance executable identity refused")
			os.Exit(1)
		}
		binary = selected
		os.Exit(m.Run())
	}
	dir, err := os.MkdirTemp("", "readmit-cli-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "readmit")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "../cmd/readmit")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatal("CLI timed out")
	}
	return stdout.String(), stderr.String(), err
}

func TestInspectShowsStructureWithoutPayloadByDefault(t *testing.T) {
	stdout, stderr, err := run(t, "inspect", "../testdata/fixtures/adt-cr.hl7")
	if err != nil || stderr != "" {
		t.Fatalf("inspect: %v; stderr=%s", err, stderr)
	}
	for _, want := range []string{"Format: raw (detected)", "Messages: 1", "PID-3 Patient Identifier List", "PID-6 Mother's Maiden Name: null", "PID-7 Date/Time of Birth: empty", "PID-8 Administrative Sex: omitted", "ZPD-3: empty"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in output:\n%s", want, stdout)
		}
	}
	for _, secret := range []string{"SYNTH-001", "EXAMPLE", "CASE-001", "adt-cr.hl7", "literal"} {
		if strings.Contains(stdout+stderr, secret) {
			t.Errorf("default output disclosed %q", secret)
		}
	}
}

func TestInspectExplicitValuesAndExclusiveRoundTrip(t *testing.T) {
	source := "../testdata/fixtures/two-messages.mllp"
	target := filepath.Join(t.TempDir(), "copy.mllp")
	stdout, stderr, err := run(t, "inspect", source, "--format", "mllp", "--terminator", "cr", "--show-values", "--roundtrip", target)
	if err != nil || stderr != "" {
		t.Fatalf("inspect: %v; stderr=%s", err, stderr)
	}
	for _, want := range []string{"Format: mllp (declared)", "Messages: 2", "terminator=cr (declared)", "EXAMPLE^CHARLIE"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output missing %q", want)
		}
	}
	want, _ := os.ReadFile(source)
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("output did not preserve exact framed input")
	}
	for _, path := range []string{source, target} {
		if out, diagnostic, err := run(t, "inspect", source, "--roundtrip", path); err == nil || out != "" || diagnostic == "" {
			t.Fatalf("existing path was not refused: %v %q %q", err, out, diagnostic)
		}
		if after, _ := os.ReadFile(path); !bytes.Equal(after, want) {
			t.Fatal("existing evidence was changed")
		}
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(target)
		if info.Mode().Perm() != 0600 {
			t.Fatalf("evidence mode = %o, want 0600", info.Mode().Perm())
		}
	}
}

func TestErrorsAreBoundedAndNeverEchoPayloadOrPaths(t *testing.T) {
	dir := t.TempDir()
	malformed := filepath.Join(dir, "SECRET-PATIENT.hl7")
	if err := os.WriteFile(malformed, []byte("MSH|^~\\&|\rPID|SECRET-PATIENT\x00\r"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"inspect", malformed}, {"inspect", filepath.Join(dir, "SECRET-MISSING.hl7")},
		{"inspect"}, {"SECRET-COMMAND"}, {"inspect", malformed, "--SECRET-FLAG"},
		{"help", "SECRET-TOPIC"}, {"help", "inspect", "SECRET-TOPIC"},
		{"inspect", malformed, "--show-values=SECRET-VALUE"},
		{"inspect", "../testdata/fixtures/adt-cr.hl7", "--format", "SECRET-FORMAT"},
		{"inspect", dir},
	} {
		stdout, stderr, err := run(t, args...)
		if err == nil || stdout != "" || stderr == "" || len(stderr) > 300 || strings.Contains(stderr, "SECRET") || strings.Contains(stderr, dir) {
			t.Errorf("unsafe error for command: %v stdout=%q stderr=%q", err, stdout, stderr)
		}
	}
}

func TestUnsupportedVersionKeepsPositionalLabels(t *testing.T) {
	source, _ := os.ReadFile("../testdata/fixtures/adt-cr.hl7")
	source = bytes.Replace(source, []byte("|2.5.1|"), []byte("|2.3|"), 1)
	path := filepath.Join(t.TempDir(), "unsupported.hl7")
	if err := os.WriteFile(path, source, 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "inspect", path)
	if err != nil || stderr != "" || !strings.Contains(stdout, "PID-3: present") || strings.Contains(stdout, "Patient Identifier List") {
		t.Fatalf("invented version-specific meaning: %v %s %s", err, stderr, stdout)
	}
}

func TestExplicitValuesUseEscapedByteStrings(t *testing.T) {
	stdout, stderr, err := run(t, "inspect", "../testdata/fixtures/non-utf8.hl7", "--show-values")
	if err != nil || stderr != "" || !strings.Contains(stdout, `\xe9\xff`) {
		t.Fatalf("lost non-UTF-8 bytes in explicit display: %v %s %s", err, stderr, stdout)
	}
}

func TestDictionaryNamesKnownFieldsAndKeepsUnknownPositions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.hl7")
	raw := []byte("MSH|^~\\&|READMIT|SYNTHETIC|RECEIVER|LAB|20260101120000||ADT^A08|CASE|P|2.5.1\rOBX|1|TX|NOTE^Note||SYNTHETIC\rMSA|AA|CASE|||||UNKNOWN\r")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "inspect", path)
	if err != nil || stderr != "" {
		t.Fatalf("inspect: %v %s", err, stderr)
	}
	for _, want := range []string{"OBX-5 Observation Value: present", "MSA-7: present"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing field label %q", want)
		}
	}
}
