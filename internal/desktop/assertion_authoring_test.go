package desktop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/assertionauthor"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
)

func TestAuthoringAnAssertionSetAnswersAndWrites(t *testing.T) {
	app, root, _ := authoringWorkspace(t)

	opened := app.AuthorAssertionSet(desktop.AssertionSetRequest{Workspace: root})
	if opened.State != desktop.Empty || opened.Set == nil {
		t.Fatalf("an empty draft should open empty: %+v", opened)
	}

	named := app.AuthorAssertionSet(desktop.AssertionSetRequest{
		Workspace: root,
		Draft:     opened.Set.Draft,
		Name:      "Synthetic expectations",
	})
	if named.State != desktop.Completed || named.Set == nil || named.Set.Draft.Name != "Synthetic expectations" {
		t.Fatalf("naming the set failed: %+v", named)
	}

	clause := assertionauthor.Clause{
		ID:       "ack-accepted",
		Operator: assertion.FieldEquals,
		Subject: assertion.Subject{Field: &assertion.FieldRef{
			Scope: assertion.ObservedMessages, Message: "s0001-e000001", Selector: "MSA-1",
		}},
		When: nil,
		Expected: assertion.Expected{Field: &assertion.FieldValue{
			State: hl7.Present, Text: ptr("AA"),
		}},
	}
	answered := app.AuthorAssertionSet(desktop.AssertionSetRequest{
		Workspace:  root,
		Draft:      named.Set.Draft,
		Assertions: []assertionauthor.Clause{clause},
	})
	if answered.State != desktop.Completed || answered.Set == nil || len(answered.Set.Draft.Assertions) != 1 {
		t.Fatalf("adding a clause failed: %+v", answered)
	}

	saved := app.SaveAssertionSet(desktop.AssertionSetRequest{
		Workspace: root,
		Draft:     answered.Set.Draft,
		Output:    "expectations.json",
	})
	if saved.State != desktop.Completed || saved.Set == nil || saved.Set.Output != "expectations.json" || saved.Set.Identity == "" {
		t.Fatalf("save failed: %+v", saved)
	}
	data, err := os.ReadFile(filepath.Join(root, "expectations.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assertion.Decode(data); err != nil {
		t.Fatalf("saved bytes are not an assertion set: %v", err)
	}

	imported := app.ImportAssertionSet(root, "expectations.json")
	if imported.State != desktop.Completed || imported.Set == nil || imported.Set.Draft.Name != "Synthetic expectations" {
		t.Fatalf("import failed: %+v", imported)
	}
	if len(imported.Set.Draft.Assertions) != 1 || imported.Set.Draft.Assertions[0].Operator != assertion.FieldEquals {
		t.Fatalf("import lost clauses: %+v", imported.Set.Draft.Assertions)
	}
}

func TestAssertionSetAuthoringRefusals(t *testing.T) {
	app, root, _ := authoringWorkspace(t)

	if got := app.SaveAssertionSet(desktop.AssertionSetRequest{
		Workspace: root,
		Draft:     assertionauthor.Draft{},
		Output:    "out.json",
	}); got.State != desktop.Failed {
		t.Fatalf("saving without a draft should fail: %+v", got)
	}

	draft := assertionauthor.NewDraft()
	named, err := draft.SetName("Named")
	if err != nil {
		t.Fatal(err)
	}
	if got := app.SaveAssertionSet(desktop.AssertionSetRequest{
		Workspace: root,
		Draft:     named,
		Output:    "out.json",
	}); got.State != desktop.Failed {
		t.Fatalf("saving without assertions should fail: %+v", got)
	}

	if got := app.ImportAssertionSet(root, "missing.json"); got.State != desktop.Failed {
		t.Fatalf("missing entry should fail: %+v", got)
	}

	if got := app.ValidateAssertionSet(`{"schema":"readmit-assertion-set/v9000","name":"x","assertions":[]}`); got.State != desktop.Failed {
		t.Fatalf("unknown version should fail: %+v", got)
	}

	if got := app.ExportAssertionSet(desktop.CanonicalAssertionRequest{
		Workspace: root,
		Document:  `{"schema":"readmit-assertion-set/v9000","name":"x","assertions":[]}`,
		Output:    "bad.json",
	}); got.State != desktop.Failed {
		t.Fatalf("export of invalid document should fail: %+v", got)
	}

	if got := app.AuthorAssertionSet(desktop.AssertionSetRequest{Workspace: ""}); got.State == desktop.Completed {
		t.Fatalf("empty workspace should refuse: %+v", got)
	}
}

func TestValidateAndExportAssertionSetHappyPath(t *testing.T) {
	app, root, _ := authoringWorkspace(t)
	data, err := os.ReadFile("../../testdata/fixtures/assertion-set.json")
	if err != nil {
		t.Fatal(err)
	}
	validated := app.ValidateAssertionSet(string(data))
	if validated.State != desktop.Completed || validated.Document == "" {
		t.Fatalf("validate failed: %+v", validated)
	}
	exported := app.ExportAssertionSet(desktop.CanonicalAssertionRequest{
		Workspace: root,
		Document:  string(data),
		Output:    "exported-assertions.json",
	})
	if exported.State != desktop.Completed || exported.Output != "exported-assertions.json" || exported.Identity == "" {
		t.Fatalf("export failed: %+v", exported)
	}
	written, err := os.ReadFile(filepath.Join(root, "exported-assertions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(data) {
		t.Fatal("export changed document bytes")
	}
}

func ptr(s string) *string { return &s }
