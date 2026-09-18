package project_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/project"
)

// document is a complete, valid project used as the starting point of the tests
// below. Every member it carries is exercised somewhere in this file.
func document() project.Document {
	return project.Document{
		Schema: project.Schema,
		Settings: project.Settings{
			Title:                   "Epic scheduling interface",
			DefaultOwner:            "integration-team",
			DefaultInterfaceVersion: "siu-2.5.1-v1",
		},
		InterfaceVersions: []string{"siu-2.5.1-v1"},
		Cases: []project.Case{{
			Name:             "regression",
			Identity:         "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df",
			Schema:           "readmit-case/v1",
			Provenance:       "generated",
			InterfaceVersion: "siu-2.5.1-v1",
			Title:            "Duplicate appointment after reschedule",
			Status:           project.StatusInvestigating,
			Owner:            "integration-team",
			Tags:             []string{"duplicate", "scheduling"},
			Incidents:        []string{"INC-4821"},
		}},
	}
}

// The exact bytes below were authored by hand from the contract in
// docs/project.md, never by printing what Encode produced. They pin member
// order, the absence of insignificant whitespace, and the trailing newline.
const encodedDocument = `{"schema":"readmit-project/v1","settings":{"title":"Epic scheduling interface","default_owner":"integration-team","default_interface_version":"siu-2.5.1-v1"},"interface_versions":["siu-2.5.1-v1"],"cases":[{"name":"regression","identity":"7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df","schema":"readmit-case/v1","provenance":"generated","interface_version":"siu-2.5.1-v1","title":"Duplicate appointment after reschedule","status":"investigating","owner":"integration-team","tags":["duplicate","scheduling"],"incidents":["INC-4821"]}]}
`

func TestEncodeProducesTheIndependentlyAuthoredBytes(t *testing.T) {
	data, err := project.Encode(document())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != encodedDocument {
		t.Fatalf("encoded document is not the authored contract:\n%s", data)
	}
	decoded, err := project.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(decoded.Cases[0].Tags, []string{"duplicate", "scheduling"}) {
		t.Fatalf("decoding lost the recorded tags: %+v", decoded.Cases[0])
	}
	if decoded.Cases[0].Identity != document().Cases[0].Identity {
		t.Fatal("decoding changed the recorded case identity")
	}
}

func TestDecodeRejectsUnknownMembersAndUnsupportedVersions(t *testing.T) {
	for name, data := range map[string]string{
		"unknown document member": `{"schema":"readmit-project/v1","settings":{"title":"t"},"interface_versions":[],"cases":[],"notes":"x"}`,
		"unknown case member":     `{"schema":"readmit-project/v1","settings":{"title":"t"},"interface_versions":[],"cases":[{"name":"c","priority":1}]}`,
		"unknown settings member": `{"schema":"readmit-project/v1","settings":{"title":"t","quota":2},"interface_versions":[],"cases":[]}`,
		"unsupported version":     `{"schema":"readmit-project/v2","settings":{"title":"t"},"interface_versions":[],"cases":[]}`,
		"absent version":          `{"settings":{"title":"t"},"interface_versions":[],"cases":[]}`,
		"not an object":           `[]`,
		"empty":                   ``,
	} {
		if _, err := project.Decode([]byte(data)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// A project document is mutable metadata, so every value a person supplies is
// bounded and checked. None of these may reach disk.
func TestValidateRefusesUnusableDocuments(t *testing.T) {
	cases := map[string]func(*project.Document){
		"absent project title":       func(d *project.Document) { d.Settings.Title = "" },
		"control character in title": func(d *project.Document) { d.Cases[0].Title = "duplicate\nappointment" },
		"oversized title":            func(d *project.Document) { d.Cases[0].Title = strings.Repeat("x", 201) },
		"invalid utf8 title":         func(d *project.Document) { d.Cases[0].Title = "\xff\xfe" },
		"unknown status":             func(d *project.Document) { d.Cases[0].Status = "wontfix" },
		"undeclared interface":       func(d *project.Document) { d.Cases[0].InterfaceVersion = "siu-2.5.1-v2" },
		"undeclared default":         func(d *project.Document) { d.Settings.DefaultInterfaceVersion = "absent" },
		"short identity":             func(d *project.Document) { d.Cases[0].Identity = "7d266d0a" },
		"uppercase identity":         func(d *project.Document) { d.Cases[0].Identity = strings.ToUpper(d.Cases[0].Identity) },
		"absent case schema":         func(d *project.Document) { d.Cases[0].Schema = "" },
		"absent provenance":          func(d *project.Document) { d.Cases[0].Provenance = "" },
		"path in case name":          func(d *project.Document) { d.Cases[0].Name = "../regression" },
		"separator in case name":     func(d *project.Document) { d.Cases[0].Name = "cases/regression" },
		"space in owner":             func(d *project.Document) { d.Cases[0].Owner = "integration team" },
		"duplicate tag":              func(d *project.Document) { d.Cases[0].Tags = []string{"a", "a"} },
		"unsorted tag":               func(d *project.Document) { d.Cases[0].Tags = []string{"b", "a"} },
		"empty tag":                  func(d *project.Document) { d.Cases[0].Tags = []string{""} },
		"duplicate interface":        func(d *project.Document) { d.InterfaceVersions = append(d.InterfaceVersions, d.InterfaceVersions[0]) },
		"no interface versions":      func(d *project.Document) { d.InterfaceVersions = nil },
		"duplicate case name": func(d *project.Document) {
			d.Cases = append(d.Cases, d.Cases[0])
		},
		"duplicate case identity": func(d *project.Document) {
			second := d.Cases[0]
			second.Name = "second"
			d.Cases = append(d.Cases, second)
		},
	}
	for name, damage := range cases {
		d := document()
		damage(&d)
		if err := project.Validate(d); err == nil {
			t.Errorf("%s was accepted", name)
		}
		if _, err := project.Encode(d); err == nil {
			t.Errorf("%s was encoded", name)
		}
	}
}

func TestCreateOpenAndSaveRoundTripThroughDisk(t *testing.T) {
	root := filepath.Join(t.TempDir(), "investigation")
	created, err := project.Create(root, document())
	if err != nil {
		t.Fatal(err)
	}
	opened, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Root != created.Root {
		t.Fatalf("opening resolved a different root: %s and %s", opened.Root, created.Root)
	}
	if opened.Document.Cases[0].Identity != document().Cases[0].Identity {
		t.Fatal("the recorded case identity did not survive a round trip")
	}

	changed := opened.Document
	changed.Settings.Title = "Renamed investigation"
	if err := opened.Save(changed); err != nil {
		t.Fatal(err)
	}
	reopened, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Document.Settings.Title != "Renamed investigation" {
		t.Fatal("saving did not replace the document")
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 1 {
		t.Fatalf("saving left files beside the document: %v %v", entries, err)
	}
}

func TestCreateRefusesAnExistingDestination(t *testing.T) {
	root := filepath.Join(t.TempDir(), "investigation")
	if _, err := project.Create(root, document()); err != nil {
		t.Fatal(err)
	}
	if _, err := project.Create(root, document()); err == nil {
		t.Fatal("an existing project directory was overwritten")
	}
}

func TestCreateRefusesADocumentThatDoesNotValidate(t *testing.T) {
	invalid := document()
	invalid.Settings.Title = ""
	root := filepath.Join(t.TempDir(), "investigation")
	if _, err := project.Create(root, invalid); err == nil {
		t.Fatal("an unusable document was written")
	}
	if _, err := os.Lstat(root); err == nil {
		t.Fatal("a refused project left a directory behind")
	}
}

// An interrupted write leaves its incomplete document in place. The next write
// reports that rather than overwriting whatever the interrupted one left, and
// reading the project keeps working from the document that is still intact.
func TestRetainedIncompleteDocumentIsReportedAndNeverOverwritten(t *testing.T) {
	root := filepath.Join(t.TempDir(), "investigation")
	opened, err := project.Create(root, document())
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(opened.Root, project.IncompleteDocumentName)
	if err := os.WriteFile(retained, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	changed := opened.Document
	changed.Settings.Title = "Renamed investigation"
	if err := opened.Save(changed); err == nil {
		t.Fatal("an interrupted write was silently discarded")
	}
	reopened, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Document.Settings.Title != document().Settings.Title {
		t.Fatal("the intact document was replaced")
	}
	if data, err := os.ReadFile(retained); err != nil || string(data) != "partial" {
		t.Fatalf("the retained incomplete document was changed: %q %v", data, err)
	}
	if err := os.Remove(retained); err != nil {
		t.Fatal(err)
	}
	if err := opened.Save(changed); err != nil {
		t.Fatalf("recovery did not restore writing: %v", err)
	}
}

func TestOpenRefusesWhatIsNotAProjectDirectory(t *testing.T) {
	empty := t.TempDir()
	if _, err := project.Open(empty); err == nil {
		t.Fatal("a folder with no document was opened as a project")
	}
	if _, err := project.Open(filepath.Join(empty, "absent")); err == nil {
		t.Fatal("an absent folder was opened as a project")
	}
	file := filepath.Join(empty, "project.json")
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := project.Open(file); err == nil {
		t.Fatal("the document file itself was opened as a project directory")
	}
	if _, err := project.Open(empty); err == nil {
		t.Fatal("an unreadable document was accepted")
	}
}

// Retained evidence is never a place to keep mutable project metadata.
func TestCreateRefusesADestinationInsideRetainedEvidence(t *testing.T) {
	evidence := t.TempDir()
	if err := os.WriteFile(filepath.Join(evidence, "identity.sha256"), []byte("0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := project.Create(filepath.Join(evidence, "investigation"), document()); err == nil {
		t.Fatal("a project was created inside retained case evidence")
	}
}

func TestAddCaseAppliesTheProjectDefaultsAndRefusesDuplicates(t *testing.T) {
	base := document()
	entry := project.Case{
		Name:       "cancellation",
		Identity:   "96077b34226faa19325f01f3fbb0d728de88644c22ba8428659c883703d3f438",
		Schema:     "readmit-case/v1",
		Provenance: "generated",
		Title:      "Cancellation is not propagated",
		Status:     project.StatusOpen,
	}
	added, stored, err := project.AddCase(base, entry)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != entry.Name || stored.Identity != entry.Identity {
		t.Fatalf("the stored entry was not returned: %+v", stored)
	}
	if len(added.Cases) != 2 {
		t.Fatalf("the case was not registered: %+v", added.Cases)
	}
	registered := added.Cases[1]
	if registered.InterfaceVersion != base.Settings.DefaultInterfaceVersion || registered.Owner != base.Settings.DefaultOwner {
		t.Fatalf("project settings did not supply the defaults: %+v", registered)
	}
	if len(base.Cases) != 1 {
		t.Fatal("adding a case changed the document it was given")
	}
	if _, _, err := project.AddCase(added, entry); err == nil {
		t.Fatal("the same case was registered twice")
	}
	renamed := entry
	renamed.Name = "copy"
	if _, _, err := project.AddCase(added, renamed); err == nil {
		t.Fatal("the same evidence was registered under a second name")
	}
}

func TestUpdateCaseChangesOnlyTheMutableMetadata(t *testing.T) {
	base := document()
	title := "Reschedule duplicates the appointment"
	status := project.StatusResolved
	tags := []string{"reschedule"}
	updated, stored, err := project.UpdateCase(base, "regression", project.Change{Title: &title, Status: &status, Tags: &tags})
	if err != nil {
		t.Fatal(err)
	}
	changed := updated.Cases[0]
	if !reflect.DeepEqual(stored, changed) {
		t.Fatalf("the stored entry was not returned: %+v", stored)
	}
	if changed.Title != title || changed.Status != status || !slices.Equal(changed.Tags, tags) {
		t.Fatalf("the mutable metadata did not change: %+v", changed)
	}
	original := base.Cases[0]
	if changed.Identity != original.Identity || changed.Schema != original.Schema || changed.Provenance != original.Provenance || changed.Name != original.Name {
		t.Fatalf("updating changed recorded evidence facts: %+v", changed)
	}
	if !slices.Equal(changed.Incidents, original.Incidents) || changed.Owner != original.Owner {
		t.Fatalf("updating changed metadata it was not given: %+v", changed)
	}
	if base.Cases[0].Title != original.Title {
		t.Fatal("updating changed the document it was given")
	}
	if _, _, err := project.UpdateCase(base, "absent", project.Change{Title: &title}); err == nil {
		t.Fatal("an unregistered case was updated")
	}
	invalid := project.Status("wontfix")
	if _, _, err := project.UpdateCase(base, "regression", project.Change{Status: &invalid}); err == nil {
		t.Fatal("an unknown status was stored")
	}
}

// Decode is the reader a project document reaches this release through. It must
// never panic, and whatever it accepts must re-encode to exactly the bytes a
// second decode accepts again.
func FuzzProject(f *testing.F) {
	f.Add([]byte(encodedDocument))
	f.Add([]byte(`{"schema":"readmit-project/v1","settings":{"title":"t"},"interface_versions":["a"],"cases":[]}`))
	f.Add([]byte(`{"schema":"readmit-project/v1"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := project.Decode(data)
		if err != nil {
			return
		}
		encoded, err := project.Encode(decoded)
		if err != nil {
			t.Fatalf("an accepted document could not be encoded: %v", err)
		}
		again, err := project.Decode(encoded)
		if err != nil {
			t.Fatalf("an encoded document was not accepted: %v", err)
		}
		second, err := project.Encode(again)
		if err != nil || string(second) != string(encoded) {
			t.Fatalf("encoding is not stable: %v", err)
		}
	})
}
