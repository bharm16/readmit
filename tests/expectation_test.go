package tests

import (
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/expectation"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpectationPublicReleaseRequiresExactReview(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "test.json")
	out := filepath.Join(dir, "released.json")
	if e := os.WriteFile(spec, []byte(baselineSpec), 0600); e != nil {
		t.Fatal(e)
	}
	stdout, stderr, e := run(t, "expectation", "review", spec, "--id", "booking", "--profile", "../testdata/fixtures/local-profile.json")
	if e != nil || stderr != "" {
		t.Fatal(e, stderr)
	}
	if strings.Contains(stdout, "PRIVATE_SENTINEL") {
		t.Fatal("private value exposed")
	}
	var c expectation.Comparison
	if e = json.Unmarshal([]byte(stdout), &c); e != nil {
		t.Fatal(e)
	}
	args := []string{"expectation", "release", spec, "--id", "booking", "--profile", "../testdata/fixtures/local-profile.json", "--review", c.Identity, "--approver", "reviewer", "--rationale", "synthetic", "--output", out}
	if _, _, e = run(t, args...); e != nil {
		t.Fatal(e)
	}
	if _, _, e = run(t, args...); e == nil {
		t.Fatal("existing release overwritten")
	}
	if _, _, e = run(t, "expectation", "release", spec, "--id", "booking", "--output", filepath.Join(dir, "missing.json")); e == nil {
		t.Fatal("unreviewed release allowed")
	}
	if e = os.Remove(spec); e != nil {
		t.Fatal(e)
	}
	stdout, _, e = run(t, "expectation", "show", out)
	if e != nil || strings.Contains(stdout, "PRIVATE_SENTINEL") {
		t.Fatal("historical private inspection failed", e)
	}
	stdout, _, e = run(t, "expectation", "show", out, "--show-values")
	if e != nil || !strings.Contains(stdout, "PRIVATE_SENTINEL") {
		t.Fatal("historical values lost", e)
	}
}
