package expectation_test

import (
	"bytes"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/profileversion"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const spec = `{"schema":"readmit-test/v1","name":"synthetic","input":{"case":"case","messages":["s0001-e000001"]},"target":"target.json","setup":{"initial_state":"operator-declared","reset_instructions":"Reset fixture"},"observation":{"boundary":"ack-contract"},"assertions":[{"id":"ack","operator":"ack_field_equals","message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"AA"}}}]}`

func TestReleasedExpectationsRequireExactReviewAndKeepHistory(t *testing.T) {
	pins := []profileversion.Version{}
	review, err := expectation.Review("booking", []byte(spec), pins, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	first, err := expectation.Approve("booking", []byte(spec), pins, nil, review.Identity, "reviewer", "synthetic fixture")
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := expectation.Inspect(first, false)
	if err != nil || inspection.Schema != "readmit-expectation-inspection/v1" {
		t.Fatal("released inspection reused baseline contract", err)
	}
	raw, err := first.Encode()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := expectation.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace([]byte(spec), []byte(`"AA"`), []byte(`"AE"`), 1)
	next, err := expectation.Review("booking", changed, pins, &reopened, true)
	if err != nil {
		t.Fatal(err)
	}
	if next.Revision != 2 || len(next.Baseline.Changes) != 1 || next.Baseline.Changes[0].Part != "assertion:ack" {
		t.Fatalf("%+v", next)
	}
	if _, err := expectation.Approve("booking", changed, pins, &reopened, review.Identity, "reviewer", "reason"); err == nil {
		t.Fatal("stale review accepted")
	}
	second, err := expectation.Approve("booking", changed, pins, &reopened, next.Identity, "reviewer", "changed rejection")
	if err != nil {
		t.Fatal(err)
	}
	if second.Parent != first.Identity() || second.Baseline.Parent == "" {
		t.Fatal("history lost")
	}
	if _, err := expectation.Review("different", changed, pins, &reopened, false); err == nil {
		t.Fatal("different test history joined")
	}
	for _, invalid := range [][]byte{bytes.Replace(raw, []byte(`"AA"`), []byte(`"AE"`), 1), bytes.Replace(raw, []byte(`"profiles":[]`), []byte(`"profiles":null`), 1), raw[:len(raw)/2]} {
		if _, err := expectation.Decode(invalid); err == nil {
			t.Fatal("invalid release accepted")
		}
	}
}

func TestProfilesInvalidateApprovalAndCannotReuseVersion(t *testing.T) {
	pins, err := expectation.ReadProfiles([]string{"../../testdata/fixtures/local-profile.json"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := expectation.Review("booking", []byte(spec), pins, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	r, err := expectation.Approve("booking", []byte(spec), pins, nil, c.Identity, "reviewer", "reason")
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]profileversion.Version{}, pins...)
	changed[0].Content.SHA256 = strings.Repeat("a", 64)
	if _, err := expectation.Review("booking", []byte(spec), changed, &r, false); err == nil {
		t.Fatal("rewritten profile accepted")
	}
	changed[0].Profile.Version = "2"
	next, err := expectation.Review("booking", []byte(spec), changed, &r, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Profiles) != 1 || len(next.Baseline.Changes) != 0 || next.Profiles[0].After != "" {
		t.Fatalf("%+v", next)
	}
	if _, err := expectation.Approve("booking", []byte(spec), changed, nil, c.Identity, "reviewer", "reason"); err == nil {
		t.Fatal("changed pin accepted stale review")
	}
	raw, _ := r.Encode()
	for _, edit := range [][2]string{{`"profiles":`, `"unknown":true,"profiles":`}, {`"content":{`, `"content":{"unknown":true,`}, {`"bytes":`, `"no_bytes":`}, {`"revision":1`, `"revision":null`}, {`"profiles":[`, `"profiles":[null,`}} {
		broken := bytes.Replace(raw, []byte(edit[0]), []byte(edit[1]), 1)
		if bytes.Equal(raw, broken) {
			t.Fatalf("mutation missed: %s in %s", edit[0], raw)
		}
		if _, err := expectation.Decode(broken); err == nil {
			t.Fatal("malformed release accepted")
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "release.json")
	if err := expectation.Save(path, r); err != nil {
		t.Fatal(err)
	}
	if err := expectation.Save(path, r); err == nil {
		t.Fatal("release overwritten")
	}
	if err := os.WriteFile(filepath.Join(dir, "partial.json"), raw[:len(raw)/2], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := expectation.Read(filepath.Join(dir, "partial.json")); err == nil {
		t.Fatal("partial release read")
	}
	if err := os.Symlink(path, filepath.Join(dir, "link.json")); err == nil {
		if _, err := expectation.Read(filepath.Join(dir, "link.json")); err == nil {
			t.Fatal("symlink read")
		}
	}
}
func FuzzReleaseReader(f *testing.F) {
	c, _ := expectation.Review("booking", []byte(spec), []profileversion.Version{}, nil, false)
	r, _ := expectation.Approve("booking", []byte(spec), []profileversion.Version{}, nil, c.Identity, "reviewer", "reason")
	raw, _ := r.Encode()
	f.Add(raw)
	f.Fuzz(func(t *testing.T, data []byte) {
		r, e := expectation.Decode(data)
		if e != nil {
			return
		}
		b, e := r.Encode()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = expectation.Decode(b); e != nil {
			t.Fatal(e)
		}
	})
}
