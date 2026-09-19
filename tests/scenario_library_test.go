package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScenarioLibraryPublicInterface(t *testing.T) {
	library := "../testdata/fixtures/scenario-library.json"
	oracle := "../testdata/fixtures/scenario-expectations.json"
	out, stderr, err := run(t, "scenario", "check-library", library, oracle)
	if err != nil || stderr != "" || !strings.Contains(out, "2 streams, 11 fields") || !strings.Contains(out, "External target outcomes: unverified") {
		t.Fatalf("%q %q %v", out, stderr, err)
	}
	if strings.Contains(out, "SYNTH") {
		t.Fatal("disclosed fixture values")
	}
	b, _ := os.ReadFile(oracle)
	bad := filepath.Join(t.TempDir(), "wrong.json")
	os.WriteFile(bad, []byte(strings.Replace(string(b), "5349555e533132", "5349555e533133", 1)), 0600)
	out, _, err = run(t, "scenario", "check-library", library, bad)
	if err == nil || out != "" {
		t.Fatal("oracle mismatch passed")
	}
	for _, args := range [][]string{{"scenario", "check-library"}, {"scenario", "check-library", library}, {"scenario", "check-library", library, "missing"}} {
		if _, _, err := run(t, args...); err == nil {
			t.Fatal("accepted incomplete command")
		}
	}
}
