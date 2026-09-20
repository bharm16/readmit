package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic hostile text crosses the actual executable boundary. Display is
// allowed only by explicit opt-in, and even then terminal controls stay inert.
func TestAdversarialInspectionPreservesBytesWithoutTerminalControl(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "PLANTED-PRIVATE-PATH.hl7")
	marker := "PLANTED-PATIENT-111"
	hostile := marker + "<script>alert(111)</script>\u202e\xff"
	raw := []byte("MSH|^~\\&|SYNTHETIC|LAB|RECEIVER|LAB|20260101120000||ADT^A08|TEST111|P|2.5.1\rPID|1||" + hostile + "\r")
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "inspect", input)
	if err != nil || stderr != "" {
		t.Fatalf("inspect failed: %v %s", err, stderr)
	}
	for _, value := range []string{marker, "<script>", input, "\x1b", "\x00", "\x07"} {
		if strings.Contains(stdout+stderr, value) {
			t.Fatal("default display disclosed hostile input")
		}
	}
	output := filepath.Join(root, "copy.hl7")
	stdout, stderr, err = run(t, "inspect", input, "--show-values", "--roundtrip", output)
	if err != nil || stderr != "" || !strings.Contains(stdout, marker) {
		t.Fatal("explicit display failed")
	}
	for _, control := range []string{"\x1b", "\x00", "\x07", "\xff"} {
		if strings.Contains(stdout, control) {
			t.Fatal("active terminal control in explicit output")
		}
	}
	for _, p := range []string{input, output} {
		got, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatal("evidence bytes changed")
		}
	}
	for _, control := range []byte{0x1b, 0x00, 0x07} {
		malformed := append(append([]byte{}, raw[:len(raw)-1]...), control, '\r')
		if err := os.WriteFile(input, malformed, 0600); err != nil {
			t.Fatal(err)
		}
		out, diagnostic, err := run(t, "inspect", input, "--show-values")
		if err == nil || out != "" || strings.Contains(diagnostic, marker) || strings.Contains(diagnostic, input) || strings.ContainsRune(diagnostic, rune(control)) {
			t.Fatal("unsafe malformed-input refusal")
		}
		if got, err := os.ReadFile(input); err != nil || !bytes.Equal(got, malformed) {
			t.Fatal("refusal changed evidence")
		}
	}
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = run(t, "inspect", input, "--roundtrip", output)
	if err == nil || stdout != "" || strings.Contains(stderr, marker) || strings.Contains(stderr, input) {
		t.Fatal("unsafe overwrite refusal")
	}
}
