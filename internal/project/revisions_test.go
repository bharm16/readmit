package project_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/project"
)

// The identities below are literal 64-character values. A unit test never runs
// the bundle reader, so nothing here claims that any evidence carries them.
const (
	parentIdentity   = "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df"
	revisionIdentity = "3f0f2c2f7e0e5a7c1d9b8a6f4e2d0c8b6a4f2e0d8c6b4a2f0e8d6c4b2a0f8e6d"
)

// revisions is the editable document that matches document(): one note about
// the registered case and one revision derived from it.
func revisions() project.Revisions {
	return project.Revisions{
		Schema: project.RevisionsSchema,
		Notes: []project.Note{{
			Name:    "reschedule-theory",
			Subject: "regression",
			Title:   "Working theory",
			Body:    "The second S13 keeps the original filler identifier.",
		}},
		Revisions: []project.Revision{{
			Name:       "regression-redacted",
			Identity:   revisionIdentity,
			Schema:     "readmit-case/v3",
			Provenance: "derived",
			Operation: project.Operation{
				Name:           "readmit-redact/v1",
				Parent:         "regression",
				ParentIdentity: parentIdentity,
			},
		}},
	}
}

// The exact bytes below were authored by hand from the contract in
// docs/project.md, never by printing what EncodeRevisions produced.
const encodedRevisions = `{"schema":"readmit-revisions/v1","notes":[{"name":"reschedule-theory","subject":"regression","title":"Working theory","body":"The second S13 keeps the original filler identifier."}],"revisions":[{"name":"regression-redacted","identity":"3f0f2c2f7e0e5a7c1d9b8a6f4e2d0c8b6a4f2e0d8c6b4a2f0e8d6c4b2a0f8e6d","schema":"readmit-case/v3","provenance":"derived","operation":{"name":"readmit-redact/v1","parent":"regression","parent_identity":"7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df"}}]}
`

// An empty editable document is what a project that has recorded nothing yet
// reads as. Both collections are present and empty, never absent.
const encodedEmptyRevisions = `{"schema":"readmit-revisions/v1","notes":[],"revisions":[]}
`

func TestEncodeRevisionsProducesTheIndependentlyAuthoredBytes(t *testing.T) {
	data, err := project.EncodeRevisions(revisions())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != encodedRevisions {
		t.Fatalf("encoded document is not the authored contract:\n%s", data)
	}
	decoded, err := project.DecodeRevisions(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Revisions[0].Operation.ParentIdentity != parentIdentity {
		t.Fatalf("decoding lost the recorded parent identity: %+v", decoded.Revisions[0])
	}
	empty, err := project.EncodeRevisions(project.Revisions{Schema: project.RevisionsSchema})
	if err != nil || string(empty) != encodedEmptyRevisions {
		t.Fatalf("an empty editable document is not the authored contract: %v\n%s", err, empty)
	}
}

// A member this release does not know and a version it does not read are both
// errors. There is no migration, no repair, and no partial acceptance.
func TestDecodeRevisionsRefusesUnknownMembersAndVersions(t *testing.T) {
	future := `{"schema":"readmit-revisions/v2","notes":[],"revisions":[]}`
	if _, err := project.DecodeRevisions([]byte(future)); !errors.Is(err, project.ErrUnsupportedVersion) {
		t.Fatalf("a future contract version was not reported as unsupported: %v", err)
	}
	for name, data := range map[string]string{
		"unknown document member": `{"schema":"readmit-revisions/v1","notes":[],"revisions":[],"index":{}}`,
		"unknown note member":     `{"schema":"readmit-revisions/v1","notes":[{"name":"n","title":"t","body":"","pinned":true}],"revisions":[]}`,
		"unknown operation member": `{"schema":"readmit-revisions/v1","notes":[],"revisions":[{"name":"r","identity":"` + revisionIdentity +
			`","schema":"readmit-case/v3","provenance":"derived","operation":{"name":"readmit-redact/v1","parent":"regression","parent_identity":"` +
			parentIdentity + `","command":"redact"}}]}`,
		"revision without an operation": `{"schema":"readmit-revisions/v1","notes":[],"revisions":[{"name":"r","identity":"` + revisionIdentity +
			`","schema":"readmit-case/v3","provenance":"derived","operation":{"name":"","parent":"","parent_identity":""}}]}`,
		"revision that is not derived evidence": `{"schema":"readmit-revisions/v1","notes":[],"revisions":[{"name":"r","identity":"` + revisionIdentity +
			`","schema":"readmit-case/v1","provenance":"imported","operation":{"name":"readmit-redact/v1","parent":"regression","parent_identity":"` +
			parentIdentity + `"}}]}`,
		"revision that is its own parent": `{"schema":"readmit-revisions/v1","notes":[],"revisions":[{"name":"regression","identity":"` + revisionIdentity +
			`","schema":"readmit-case/v3","provenance":"derived","operation":{"name":"readmit-redact/v1","parent":"regression","parent_identity":"` +
			parentIdentity + `"}}]}`,
		"a case name that is a path": `{"schema":"readmit-revisions/v1","notes":[],"revisions":[{"name":"../elsewhere","identity":"` + revisionIdentity +
			`","schema":"readmit-case/v3","provenance":"derived","operation":{"name":"readmit-redact/v1","parent":"regression","parent_identity":"` +
			parentIdentity + `"}}]}`,
		"notes out of order":              `{"schema":"readmit-revisions/v1","notes":[{"name":"second","title":"t","body":""},{"name":"first","title":"t","body":""}],"revisions":[]}`,
		"a note with a control character": `{"schema":"readmit-revisions/v1","notes":[{"name":"n","title":"t","body":"one\ttwo"}],"revisions":[]}`,
	} {
		if _, err := project.DecodeRevisions([]byte(data)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// A note is editable working text. Replacing one replaces exactly that note,
// and a note about evidence must name evidence the project actually registered.
func TestSetNoteReplacesOnlyTheNamedNote(t *testing.T) {
	base := document()
	stored, note, err := project.SetNote(base, revisions(), project.Note{Name: "aaa-first", Title: "Draft", Body: "unfinished"})
	if err != nil {
		t.Fatal(err)
	}
	if note.Subject != "" {
		t.Fatalf("a draft gained a subject: %+v", note)
	}
	if len(stored.Notes) != 2 || stored.Notes[0].Name != "aaa-first" || stored.Notes[1].Name != "reschedule-theory" {
		t.Fatalf("notes are not held in one canonical order: %+v", stored.Notes)
	}
	replaced, note, err := project.SetNote(base, stored, project.Note{Name: "reschedule-theory", Subject: "regression-redacted", Title: "Confirmed", Body: "Reproduced on the derived case."})
	if err != nil {
		t.Fatal(err)
	}
	if len(replaced.Notes) != 2 || note.Title != "Confirmed" || replaced.Notes[1].Body != "Reproduced on the derived case." {
		t.Fatalf("replacing a note did not replace exactly that note: %+v", replaced.Notes)
	}
	if replaced.Notes[0].Body != "unfinished" {
		t.Fatal("replacing one note changed another")
	}
	if len(stored.Notes) != 2 || stored.Notes[1].Title != "Working theory" {
		t.Fatal("setting a note changed the document it was given")
	}
	if _, _, err := project.SetNote(base, revisions(), project.Note{Name: "stray", Subject: "not-registered", Title: "t", Body: ""}); err == nil {
		t.Fatal("a note was attached to evidence this project does not register")
	}
	if _, _, err := project.SetNote(base, revisions(), project.Note{Name: "empty-title", Title: "", Body: "b"}); err == nil {
		t.Fatal("a note without a title was stored")
	}
}

// Registering a revision records the parent identity the project already holds
// and the operation the derived evidence itself declares.
func TestAddRevisionRecordsParentIdentityAndOperation(t *testing.T) {
	base := document()
	second := project.Revision{
		Name:       "regression-redacted-2",
		Identity:   "1111111111111111111111111111111111111111111111111111111111111111",
		Schema:     "readmit-case/v3",
		Provenance: "derived",
		Operation:  project.Operation{Name: "readmit-redact/v1", Parent: "regression-redacted", ParentIdentity: revisionIdentity},
	}
	stored, entry, err := project.AddRevision(base, revisions(), second)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Revisions) != 2 || stored.Revisions[1].Name != "regression-redacted-2" {
		t.Fatalf("a revision of a revision was not recorded in registration order: %+v", stored.Revisions)
	}
	if entry.Operation.ParentIdentity != revisionIdentity {
		t.Fatalf("the recorded parent identity is not the parent's: %+v", entry)
	}
	if len(revisions().Revisions) != 1 {
		t.Fatal("registering a revision changed the document it was given")
	}

	unregistered := second
	unregistered.Operation.Parent = "never-registered"
	if _, _, err := project.AddRevision(base, revisions(), unregistered); err == nil {
		t.Fatal("a revision was derived from evidence this project does not register")
	}
	drifted := second
	drifted.Operation.ParentIdentity = "2222222222222222222222222222222222222222222222222222222222222222"
	if _, _, err := project.AddRevision(base, revisions(), drifted); err == nil {
		t.Fatal("a parent identity that is not the recorded one was stored")
	}
	duplicate := second
	duplicate.Name = "regression-redacted"
	if _, _, err := project.AddRevision(base, revisions(), duplicate); err == nil {
		t.Fatal("the same name was registered twice")
	}
	registeredCase := second
	registeredCase.Name = "regression"
	if _, _, err := project.AddRevision(base, revisions(), registeredCase); err == nil {
		t.Fatal("a revision took the name of a registered case")
	}
	sameEvidence := second
	sameEvidence.Identity = revisionIdentity
	if _, _, err := project.AddRevision(base, revisions(), sameEvidence); err == nil {
		t.Fatal("the same evidence was registered under two names")
	}
	sameAsCase := second
	sameAsCase.Identity = parentIdentity
	if _, _, err := project.AddRevision(base, revisions(), sameAsCase); err == nil {
		t.Fatal("evidence already registered as a case was registered again as a revision")
	}
	imported := second
	imported.Provenance, imported.Schema = "imported", "readmit-case/v1"
	if _, _, err := project.AddRevision(base, revisions(), imported); err == nil {
		t.Fatal("evidence that is not a transformation was registered as a revision")
	}
	anonymous := second
	anonymous.Operation.Name = ""
	if _, _, err := project.AddRevision(base, revisions(), anonymous); err == nil {
		t.Fatal("a transformation was recorded without an operation manifest")
	}
}

// The immutable and editable sides of a project stay disjoint, and evidence
// that is the output of a transformation is registered with its lineage or not
// at all.
func TestAddCaseRefusesDerivedEvidenceAndRegisteredNames(t *testing.T) {
	base := document()
	held := revisions()
	plain := project.Case{
		Name:       "booking",
		Identity:   "4444444444444444444444444444444444444444444444444444444444444444",
		Schema:     "readmit-case/v1",
		Provenance: "imported",
		Title:      "Booking",
	}
	if _, _, err := project.AddCase(base, held, plain); err != nil {
		t.Fatalf("ordinary evidence was refused: %v", err)
	}
	derived := plain
	derived.Schema, derived.Provenance = "readmit-case/v3", "derived"
	if _, _, err := project.AddCase(base, held, derived); err == nil {
		t.Fatal("a transformation was registered as a case, with no parent identity and no operation")
	}
	named := plain
	named.Name = "regression-redacted"
	if _, _, err := project.AddCase(base, held, named); err == nil {
		t.Fatal("a case took the name of a registered revision")
	}
	sameEvidence := plain
	sameEvidence.Identity = revisionIdentity
	if _, _, err := project.AddCase(base, held, sameEvidence); err == nil {
		t.Fatal("evidence already registered as a revision was registered again as a case")
	}
}

// The editable document is stored beside the project document, is replaced
// atomically, and a project that has recorded nothing yet reads as empty.
func TestRevisionsAreStoredBesideTheProjectDocument(t *testing.T) {
	root := filepath.Join(t.TempDir(), "investigation")
	created, err := project.Create(root, document())
	if err != nil {
		t.Fatal(err)
	}
	empty, err := project.ReadRevisions(created.Root)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Schema != project.RevisionsSchema || len(empty.Notes) != 0 || len(empty.Revisions) != 0 {
		t.Fatalf("a project with no editable document did not read as empty: %+v", empty)
	}
	if err := project.WriteRevisions(created.Root, revisions()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(created.Root, project.RevisionsDocumentName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != encodedRevisions {
		t.Fatalf("the stored document is not the authored contract:\n%s", data)
	}
	// The project document is beside it and was not touched.
	if _, err := project.Open(created.Root); err != nil {
		t.Fatalf("storing the editable document broke the project document: %v", err)
	}
	reopened, err := project.ReadRevisions(created.Root)
	if err != nil || len(reopened.Revisions) != 1 {
		t.Fatalf("the stored document did not read back: %v %+v", err, reopened)
	}

	// An interrupted write is retained, so recovery is an explicit decision
	// outside readmit rather than something the next write makes silently.
	retained := filepath.Join(created.Root, project.RevisionsIncompleteDocumentName)
	if err := os.WriteFile(retained, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteRevisions(created.Root, revisions()); err == nil {
		t.Fatal("a retained interrupted write was overwritten")
	}
	kept, err := os.ReadFile(retained)
	if err != nil || string(kept) != "partial" {
		t.Fatalf("the interrupted write was not retained: %v %s", err, kept)
	}
	if again, err := project.ReadRevisions(created.Root); err != nil || len(again.Revisions) != 1 {
		t.Fatalf("reading kept working from the intact document: %v %+v", err, again)
	}
}

// A document past a bound is refused rather than truncated.
func TestRevisionsDocumentBounds(t *testing.T) {
	base := document()
	oversize := revisions()
	oversize.Notes[0].Body = strings.Repeat("x", 4097)
	if err := project.ValidateRevisions(oversize); err == nil {
		t.Fatal("a note body past its limit was stored")
	}
	full := project.Revisions{Schema: project.RevisionsSchema}
	for i := range project.MaxNotes + 1 {
		full.Notes = append(full.Notes, project.Note{Name: "note-" + string(rune('a'+i/26)) + string(rune('a'+i%26)) + "-" + string(rune('0'+i%10)), Title: "t", Body: ""})
	}
	if _, _, err := project.SetNote(base, full, project.Note{Name: "zzz-last", Title: "t", Body: ""}); err == nil {
		t.Fatal("more notes than this release stores were accepted")
	}
}

// DecodeRevisions is the reader the editable document reaches this release
// through. It must never panic, and whatever it accepts must re-encode to
// exactly the bytes a second decode accepts again.
func FuzzRevisions(f *testing.F) {
	f.Add([]byte(encodedRevisions))
	f.Add([]byte(encodedEmptyRevisions))
	f.Add([]byte(`{"schema":"readmit-revisions/v1"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := project.DecodeRevisions(data)
		if err != nil {
			return
		}
		encoded, err := project.EncodeRevisions(decoded)
		if err != nil {
			t.Fatalf("an accepted document could not be encoded: %v", err)
		}
		again, err := project.DecodeRevisions(encoded)
		if err != nil {
			t.Fatalf("an encoded document was not accepted: %v", err)
		}
		second, err := project.EncodeRevisions(again)
		if err != nil || string(second) != string(encoded) {
			t.Fatalf("encoding is not stable: %v", err)
		}
	})
}
