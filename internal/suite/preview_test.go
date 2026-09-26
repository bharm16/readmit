package suite_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
)

// Preview answers what a suite would expand to without writing anything. The
// expansion prepare performs — exact jobs, effective inputs, bindings, release
// pins — is decided by the same rules here, so a window can show it before a
// directory is created and before anything is sent.
func TestPreviewExpandsExactJobsInDeclaredOrderWithoutWriting(t *testing.T) {
	dir, doc := fixture(t, "127.0.0.1:1")
	before, _ := os.ReadDir(dir)
	value := "AE"
	doc.Tables[0].Rows = append(doc.Tables[0].Rows, suite.Row{ID: "rejected", Case: "case-one", Expected: map[string]testrunner.Value{"ack": {Field: &testrunner.FieldValue{State: hl7.Present, Text: &value}}}})
	setup := doc.Tests[0]
	setup.ID = "setup"
	doc.Tests = append([]suite.Test{setup}, doc.Tests...)
	doc.Tests[1].After = []string{"setup"}
	expansion, e := suite.Preview(dir, doc, "east", "")
	if e != nil {
		t.Fatal(e)
	}
	if expansion.Suite.ID != "nightly" || expansion.Environment.ID != "east" || expansion.Environment.Site != "hospital-a" || expansion.Engine == "" {
		t.Fatalf("%+v", expansion)
	}
	if len(expansion.Jobs) != 4 {
		t.Fatalf("%+v", expansion.Jobs)
	}
	first := expansion.Jobs[0]
	if first.ID != "setup-one" || first.Test != "setup" || first.Row != "one" || first.Spec != "booking.json" ||
		first.Case != filepath.Join(dir, "case-one") || first.Target != filepath.Join(dir, "east.json") ||
		first.Boundary != testrunner.ACKBoundary || string(first.Isolation) != "shared" || first.Sequence[0] != "s0001-e000001" || len(first.After) != 0 {
		t.Fatalf("%+v", first)
	}
	third := expansion.Jobs[2]
	if third.ID != "booking-one" || len(third.After) != 2 || third.After[0] != "setup-one" || third.After[1] != "setup-rejected" {
		t.Fatalf("%+v", third)
	}
	if expansion.Jobs[1].ID != "setup-rejected" || expansion.Jobs[3].ID != "booking-rejected" {
		t.Fatalf("input order changed: %+v", expansion.Jobs)
	}
	if expansion.Sharing == "" || expansion.Order == "" {
		t.Fatal("preview must state the serialization and order rules it decided")
	}
	after, _ := os.ReadDir(dir)
	if len(after) != len(before) {
		t.Fatal("preview wrote something")
	}
}

// The preview's jobs are exactly the jobs prepare expands, in the same order
// with the same dependencies and isolation declarations.
func TestPreviewMatchesWhatPrepareExpands(t *testing.T) {
	dir, doc := fixture(t, "127.0.0.1:1")
	value := "AE"
	doc.Tables[0].Rows = append(doc.Tables[0].Rows, suite.Row{ID: "rejected", Case: "case-one", Expected: map[string]testrunner.Value{"ack": {Field: &testrunner.FieldValue{State: hl7.Present, Text: &value}}}})
	setup := doc.Tests[0]
	setup.ID = "setup"
	setup.Isolation = "isolated"
	doc.Tests = append([]suite.Test{setup}, doc.Tests...)
	doc.Tests[1].After = []string{"setup"}
	expansion, e := suite.Preview(dir, doc, "east", "")
	if e != nil {
		t.Fatal(e)
	}
	write(t, filepath.Join(dir, "suite.json"), doc)
	prepared, e := suite.Prepare(suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: filepath.Join(dir, "compiled")})
	if e != nil {
		t.Fatal(e)
	}
	if len(prepared.Queue.Jobs) != len(expansion.Jobs) {
		t.Fatalf("preview and prepare disagree: %+v %+v", expansion.Jobs, prepared.Queue.Jobs)
	}
	for i, job := range prepared.Queue.Jobs {
		if expansion.Jobs[i].ID != job.ID || expansion.Jobs[i].Isolation != string(job.Isolation) {
			t.Fatalf("preview and prepare disagree at %d: %+v %+v", i, expansion.Jobs[i], job)
		}
	}
}

// A released template's exact identity is part of what a preview shows: the
// expansion names the pin each job's expectations were approved under, and a
// ledger template's environment observation binding is part of the expansion.
func TestPreviewReportsReleasePinsAndLedgerBindings(t *testing.T) {
	dir, doc := fixture(t, "127.0.0.1:1")
	spec, e := testrunner.ReadSpec(filepath.Join(dir, "booking.json"))
	if e != nil {
		t.Fatal(e)
	}
	n := 1
	spec.Setup.InitialState = "empty-ledger"
	spec.Observation = testrunner.Observation{Boundary: testrunner.LedgerBoundary, Path: "unbound.json"}
	spec.Assertions = []testrunner.Assertion{{ID: "count", Operator: "ledger_count", Expected: testrunner.Value{Count: &n}}}
	write(t, filepath.Join(dir, "booking.json"), spec)
	raw, e := os.ReadFile(filepath.Join(dir, "booking.json"))
	if e != nil {
		t.Fatal(e)
	}
	commitment, e := expectation.Review("booking", raw, []profileversion.Version{}, nil, false)
	if e != nil {
		t.Fatal(e)
	}
	released, e := expectation.Approve("booking", raw, []profileversion.Version{}, nil, commitment.Identity, "reviewer", "fixture")
	if e != nil {
		t.Fatal(e)
	}
	if e = expectation.Save(filepath.Join(dir, "release.json"), released); e != nil {
		t.Fatal(e)
	}
	write(t, filepath.Join(dir, "releases.json"), suite.ReleaseReferences{Schema: suite.ReleasesSchema, Tests: []suite.ReleaseReference{{Test: "booking", Release: "release.json", Identity: released.Identity()}}})
	doc.Environments[0].Bindings[0].Observation = "east-observation.json"
	expansion, e := suite.Preview(dir, doc, "east", filepath.Join(dir, "releases.json"))
	if e != nil {
		t.Fatal(e)
	}
	job := expansion.Jobs[0]
	if job.Boundary != testrunner.LedgerBoundary || job.Observation != filepath.Join(dir, "east-observation.json") {
		t.Fatalf("%+v", job)
	}
	if job.Release != released.Identity() || len(expansion.Releases) != 1 || expansion.Releases[0].Test != "booking" || expansion.Releases[0].Identity != released.Identity() {
		t.Fatalf("%+v %+v", job, expansion.Releases)
	}
	// Without the sidecar the same suite is an ordinary one: no approval claim,
	// no release pin in the expansion, and still an exact expansion.
	ordinary, e := suite.Preview(dir, doc, "east", "")
	if e != nil {
		t.Fatal(e)
	}
	if ordinary.Jobs[0].Release != "" || len(ordinary.Releases) != 0 {
		t.Fatalf("%+v", ordinary.Jobs[0])
	}
}
