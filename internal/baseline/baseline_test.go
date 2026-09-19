package baseline_test

import (
	"bytes"
	"github.com/bharm16/readmit/internal/baseline"
	"os"
	"path/filepath"
	"testing"
)

const spec = `{"schema":"readmit-test/v1","name":"synthetic","input":{"case":"case","messages":["s0001-e000001"]},"target":"target.json","setup":{"initial_state":"operator-declared","reset_instructions":"Reset fixture"},"observation":{"boundary":"ack-contract"},"assertions":[{"id":"ack","operator":"ack_field_equals","message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"AA"}}}]}`

func TestReviewApprovalAndImmutableParent(t *testing.T) {
	review, err := baseline.Review([]byte(spec), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Changes) != 7 || review.Changes[6].After != "" {
		t.Fatalf("private initial review: %+v", review)
	}
	first, err := baseline.Approve([]byte(spec), nil, review.Identity, "local reviewer", "Known synthetic contract")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := first.Encode()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := baseline.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace([]byte(spec), []byte(`"AA"`), []byte(`"AE"`), 1)
	next, err := baseline.Review(changed, &reopened, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Changes) != 1 || next.Changes[0].Before == next.Changes[0].After {
		t.Fatalf("exact delta: %+v", next)
	}
	if _, err := baseline.Approve(changed, &reopened, review.Identity, "reviewer", "changed"); err == nil {
		t.Fatal("stale review accepted")
	}
	second, err := baseline.Approve(changed, &reopened, next.Identity, "reviewer", "expected rejection")
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != 2 || second.Parent == "" {
		t.Fatal("parent not pinned")
	}
	if _, err := baseline.Approve(changed, &reopened, next.Identity, "", "reason"); err == nil {
		t.Fatal("missing reviewer accepted")
	}
}

func TestBaselineStrictReaderAndApprovalMutation(t *testing.T) {
	review, _ := baseline.Review([]byte(spec), nil, false)
	first, _ := baseline.Approve([]byte(spec), nil, review.Identity, "reviewer", "reason")
	data, _ := first.Encode()
	for name, invalid := range map[string][]byte{
		"unknown":             bytes.Replace(data, []byte(`"schema":`), []byte(`"unknown":true,"schema":`), 1),
		"null":                bytes.Replace(data, []byte(`"parent":""`), []byte(`"parent":null`), 1),
		"missing":             bytes.Replace(data, []byte(`"parent":"",`), nil, 1),
		"nested":              bytes.Replace(data, []byte(`"operator":"ack_field_equals"`), []byte(`"operator":"ack_field_equals","unknown":true`), 1),
		"changed expectation": bytes.Replace(data, []byte(`"AA"`), []byte(`"AE"`), 1),
		"truncated":           data[:len(data)/2],
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := baseline.Decode(invalid); err == nil {
				t.Fatal("invalid revision accepted")
			}
		})
	}
	if _, err := baseline.Review([]byte(`{"schema":"readmit-result/v1","status":"pass"}`), nil, false); err == nil {
		t.Fatal("passing result became a baseline")
	}
}

func TestBaselineReviewNamesScopeChangesAndRetainsHistoricalValues(t *testing.T) {
	review, _ := baseline.Review([]byte(spec), nil, false)
	parent, _ := baseline.Approve([]byte(spec), nil, review.Identity, "reviewer", "reason")
	for _, change := range []struct{ old, new, part string }{
		{`"MSA-1"`, `"MSA-2"`, "assertion:ack"},
		{`"target.json"`, `"other-target.json"`, "target"},
		{`"Reset fixture"`, `"Reset another fixture"`, "setup"},
		{`"synthetic"`, `"renamed"`, "name"},
		{`"case":"case"`, `"case":"other-case"`, "input"},
	} {
		candidate := bytes.Replace([]byte(spec), []byte(change.old), []byte(change.new), 1)
		compared, err := baseline.Review(candidate, &parent, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(compared.Changes) != 1 || compared.Changes[0].Part != change.part {
			t.Fatalf("scope change lost: %+v", compared)
		}
		if _, err := baseline.Approve(candidate, &parent, review.Identity, "reviewer", "reason"); err == nil {
			t.Fatal("changed candidate accepted with stale decision")
		}
	}
	view, err := baseline.Inspect(parent, true)
	if err != nil {
		t.Fatal(err)
	}
	if view.Revision != 1 || !bytes.Contains(view.JSON(), []byte(`AA`)) {
		t.Fatal("historical expectations lost")
	}
}

func FuzzBaselineReader(f *testing.F) {
	review, _ := baseline.Review([]byte(spec), nil, false)
	parent, _ := baseline.Approve([]byte(spec), nil, review.Identity, "reviewer", "reason")
	data, _ := parent.Encode()
	f.Add(data)
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		revision, err := baseline.Decode(data)
		if err != nil {
			return
		}
		encoded, err := revision.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := baseline.Decode(encoded); err != nil {
			t.Fatal(err)
		}
	})
}

func TestBaselineStorageRefusesOverwriteAndRecoversFromPartialFile(t *testing.T) {
	review, _ := baseline.Review([]byte(spec), nil, false)
	revision, _ := baseline.Approve([]byte(spec), nil, review.Identity, "reviewer", "reason")
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := baseline.Save(path, revision); err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(path, revision); err == nil {
		t.Fatal("overwrite accepted")
	}
	if err := os.WriteFile(path, []byte(`{"schema":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := baseline.Read(path); err == nil {
		t.Fatal("partial revision accepted")
	}
	if err := baseline.Save(path, revision); err == nil {
		t.Fatal("partial file silently replaced")
	}
	if err := baseline.Save(path+".recovered", revision); err != nil {
		t.Fatal(err)
	}
	if _, err := baseline.Read(path + ".recovered"); err != nil {
		t.Fatal(err)
	}
}

func TestBaselineCannotBeWrittenInsideEvidence(t *testing.T) {
	review, _ := baseline.Review([]byte(spec), nil, false)
	revision, _ := baseline.Approve([]byte(spec), nil, review.Identity, "reviewer", "reason")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "identity.sha256"), []byte("retained evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(filepath.Join(root, "baseline.json"), revision); err == nil {
		t.Fatal("wrote inside evidence")
	}
	if _, err := os.Stat(filepath.Join(root, "baseline.json")); !os.IsNotExist(err) {
		t.Fatal("refused write left file")
	}
}
