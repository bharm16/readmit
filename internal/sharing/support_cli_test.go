package sharing_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/sharing"
)

func TestSupportRecipientVerifiesReviewedBundleWithoutSource(t *testing.T) {
	r, root := fixture(t)
	candidate, err := sharing.Prepare(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "support")
	if err = candidate.Publish(context.Background(), candidate.Identity(), out); err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(r.Source); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	verify := func() error {
		stdout.Reset()
		stderr.Reset()
		return cli.Execute("test", []string{"share", "verify", out}, &stdout, &stderr)
	}
	if err = verify(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "reviewed-extract-only") || !strings.Contains(stdout.String(), candidate.Identity()) || strings.Contains(stdout.String(), root) {
		t.Fatalf("unsafe or unhelpful verification: %q", stdout.String())
	}
	marker, err := os.ReadFile(filepath.Join(out, "identity.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(out, "identity.sha256")); err != nil {
		t.Fatal(err)
	}
	if err = verify(); err == nil || stdout.Len() != 0 {
		t.Fatal("incomplete bundle accepted")
	}
	write(t, filepath.Join(out, "identity.sha256"), marker)
	write(t, filepath.Join(out, "extra.html"), []byte("<script>PLANTED</script>"))
	if err = verify(); err == nil || stdout.Len() != 0 || strings.Contains(stderr.String(), "PLANTED") {
		t.Fatal("extra content accepted or leaked")
	}
	if err = os.Remove(filepath.Join(out, "extra.html")); err != nil {
		t.Fatal(err)
	}
	if err = verify(); err != nil {
		t.Fatal("valid bundle cannot recover", err)
	}
	write(t, filepath.Join(out, "support.json"), append(candidate.Bytes(), ' '))
	if err = verify(); err == nil || stdout.Len() != 0 {
		t.Fatal("alteration accepted")
	}
}

func TestSupportRunbookSyntheticWorkflow(t *testing.T) {
	root := t.TempDir()
	synthetic, retained, output := filepath.Join(root, "synthetic"), filepath.Join(root, "retained"), filepath.Join(root, "support")
	policy := filepath.Join(root, "sharing.json")
	write(t, policy, []byte(`{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["local-file"],"max_bytes":4096}`))
	var stdout, stderr bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		stdout.Reset()
		stderr.Reset()
		if err := cli.Execute("test", args, &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
	}
	run("report", "--scenario", "siu-reschedule-v1", "--output", synthetic)
	run("report", "verify", synthetic)
	run("report", "assemble", "--case", filepath.Join(synthetic, "reproducer"), "--spec", filepath.Join(synthetic, "spec.json"), "--current", filepath.Join(synthetic, "post-fix"), "--output", retained)
	run("share", retained, "--kind", "retained-packet", "--policy", policy)
	lines := strings.Split(stdout.String(), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[1], "Approval identity: ") {
		t.Fatal("missing preview approval")
	}
	identity := strings.TrimPrefix(lines[1], "Approval identity: ")
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("preview wrote output")
	}
	run("share", retained, "--kind", "retained-packet", "--policy", policy, "--approve", identity, "--output", output)
	run("share", "verify", output)
	if !strings.Contains(stdout.String(), `"outcome":"pass"`) || !strings.Contains(stdout.String(), identity) {
		t.Fatal("missing verified diagnosis")
	}
}
