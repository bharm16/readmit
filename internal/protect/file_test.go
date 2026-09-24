package protect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A File is the one interface the command line and the desktop window change a
// protection document through. These tests hold it to what both entry points
// promise: a new control's defaults, a rotation recorded only once the declared
// store answers, a retirement that changes nothing but the control's state, a
// package written only under an active control and opened under the control
// that wrote it, and a refusal at every step that leaves the document as it was.

var registeredAt = time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)

// registered registers each named control, in order, into a new document.
func registered(t *testing.T, names ...string) File {
	t.Helper()
	file := File{Path: filepath.Join(t.TempDir(), "protection.json")}
	for _, name := range names {
		if _, _, err := file.Register(control(t, name, testOnlyKey), registeredAt); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	return file
}

func held(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func encoded(t *testing.T, document Document) string {
	t.Helper()
	raw, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// unchanged fails when a refused operation changed the document or left a
// partial one beside it.
func unchanged(t *testing.T, file File, before, label string) {
	t.Helper()
	if after := held(t, file.Path); after != before {
		t.Errorf("%s changed the document:\n%s", label, after)
	}
	if _, err := os.Lstat(file.Path + ".incomplete"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s left a partial document beside it", label)
	}
}

func absent(t *testing.T, path, label string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s left %s behind", label, filepath.Base(path))
	}
}

func TestRegisterStartsTheDocumentAndGivesANewControlItsDefaults(t *testing.T) {
	file := File{Path: filepath.Join(t.TempDir(), "protection.json")}
	// Whatever lifecycle the entry claims, a new control starts it: active, at
	// generation 1, rotated at the registration instant to the second in UTC.
	entry := control(t, "lab-evidence", testOnlyKey)
	entry.State, entry.Generation = Retired, 7
	entry.RotatedAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, 9, 18, 11, 0, 0, 750_000_000, time.FixedZone("CEST", 2*60*60))
	document, stored, err := file.Register(entry, at)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if stored.State != Active || stored.Generation != 1 || !stored.RotatedAt.Equal(registeredAt) || stored.RotatedAt.Location() != time.UTC {
		t.Fatalf("a new control is %s at generation %d rotated %s", stored.State, stored.Generation, stored.RotatedAt)
	}
	// The document written is the document returned, and it holds a locator,
	// never the bytes the declared store answers with.
	written := held(t, file.Path)
	if written != encoded(t, document) {
		t.Fatalf("the document written is not the one returned:\n%s", written)
	}
	if strings.Contains(written, testOnlyKey) {
		t.Fatal("the protection document holds key material")
	}

	// A second registration under the name and an entry the contract refuses
	// change nothing.
	relative := control(t, "second", testOnlyKey)
	relative.Command = "security"
	for label, refused := range map[string]Control{
		"a name registered twice": control(t, "lab-evidence", testOnlyKey),
		"a relative command":      relative,
	} {
		if _, _, err := file.Register(refused, at); err == nil {
			t.Errorf("%s was registered", label)
		}
		unchanged(t, file, written, label)
	}
	if _, _, err := file.Register(control(t, "lab-evidence", testOnlyKey), at); err == nil || err.Error() != "that name is already registered in this protection document" {
		t.Errorf("a name registered twice answered %v", err)
	}

	// A path holding a document from a later release is reported, never
	// replaced, and a refused first registration starts no document.
	later := File{Path: filepath.Join(t.TempDir(), "later.json")}
	const laterDocument = `{"schema":"readmit-protection/v2","controls":[]}` + "\n"
	if err := os.WriteFile(later.Path, []byte(laterDocument), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := later.Register(control(t, "lab-evidence", testOnlyKey), at); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("a later version answered %v", err)
	}
	unchanged(t, later, laterDocument, "a later version")
	first := File{Path: filepath.Join(t.TempDir(), "first.json")}
	if _, _, err := first.Register(relative, at); err == nil {
		t.Error("an entry the contract refuses was registered")
	}
	absent(t, first.Path, "a refused first registration")
}

// A rotation is recorded only once the declared store answers for the control.
// A store that fails, one that answers with too little material and a caller
// that cancels while it is asked each leave the document exactly as it was,
// refused in the one sentence both entry points print; a store that answers
// records the next generation at the given instant and changes nothing else.
func TestARotationIsRecordedOnlyAfterTheDeclaredStoreAnswers(t *testing.T) {
	file := registered(t, "archive-2025", "lab-evidence")
	before := held(t, file.Path)
	const unanswered = "the key did not resolve from its declared store; the recorded rotation is unchanged"
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, refused := range []struct {
		label, mode string
		ctx         context.Context
	}{
		{"a store that fails", "fail", context.Background()},
		{"a store that answers with too little material", "short", context.Background()},
		{"a caller that cancels", "emit", cancelled},
	} {
		t.Setenv(storeSwitch, refused.mode)
		_, _, err := file.Rotate(refused.ctx, "lab-evidence", registeredAt.Add(time.Hour))
		if err == nil || err.Error() != unanswered || StepOf(err) != KeyStep {
			t.Errorf("%s: the rotation answered %v at step %d", refused.label, err, StepOf(err))
		}
		unchanged(t, file, before, refused.label)
	}

	t.Setenv(storeSwitch, "emit")
	if _, _, err := file.Rotate(context.Background(), "never-registered", registeredAt); err == nil ||
		err.Error() != "no protection control is registered under that name" || StepOf(err) != DocumentStep {
		t.Errorf("a control nobody registered answered %v", err)
	}
	unchanged(t, file, before, "a control nobody registered")

	at := time.Date(2026, 10, 1, 8, 30, 0, 900_000_000, time.UTC)
	document, stored, err := file.Rotate(context.Background(), "lab-evidence", at)
	if err != nil || stored.Generation != 2 || !stored.RotatedAt.Equal(at.Truncate(time.Second)) {
		t.Fatalf("the answered rotation recorded %+v (%v)", stored, err)
	}
	expected, err := Decode([]byte(before))
	if err != nil {
		t.Fatal(err)
	}
	expected.Controls[1].Generation, expected.Controls[1].RotatedAt = 2, at.Truncate(time.Second)
	if written := held(t, file.Path); written != encoded(t, expected) || written != encoded(t, document) {
		t.Fatalf("the rotation changed more than the control's generation and time:\n%s", written)
	}
}

func TestRetireChangesOnlyTheControlsStateAndARefusalChangesNothing(t *testing.T) {
	file := registered(t, "archive-2025", "lab-evidence")
	before := held(t, file.Path)
	document, stored, err := file.Retire("lab-evidence")
	if err != nil || stored.State != Retired {
		t.Fatalf("retire: %v state=%s", err, stored.State)
	}
	expected, err := Decode([]byte(before))
	if err != nil {
		t.Fatal(err)
	}
	expected.Controls[1].State = Retired
	if written := held(t, file.Path); written != encoded(t, expected) || written != encoded(t, document) {
		t.Fatalf("retirement changed more than the control's state:\n%s", written)
	}

	later := File{Path: filepath.Join(t.TempDir(), "later.json")}
	if err := os.WriteFile(later.Path, []byte(`{"schema":"readmit-protection/v2","controls":[]}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, refused := range []struct {
		file           File
		name, sentence string
	}{
		{file, "lab-evidence", "the protection control is already retired"},
		{file, "never-registered", "no protection control is registered under that name"},
		{later, "lab-evidence", "unsupported protection document version"},
	} {
		before := held(t, refused.file.Path)
		if _, _, err := refused.file.Retire(refused.name); err == nil || err.Error() != refused.sentence {
			t.Errorf("retiring %s answered %v, want %q", refused.name, err, refused.sentence)
		}
		unchanged(t, refused.file, before, "retiring "+refused.name)
	}
}

// Where no document exists yet, a File refuses the path to every operation but
// Register, as the command line refuses a --protection file that does not
// exist, unless it reads an absent document as empty, as the desktop window
// reads its entry. Either way nothing is created.
func TestAnAbsentDocumentIsRefusedOrEmptyAsTheFileSaysAndNothingIsCreated(t *testing.T) {
	t.Setenv(storeSwitch, "emit")
	root := t.TempDir()
	evidence := filepath.Join(root, "evidence.txt")
	if err := os.WriteFile(evidence, []byte("synthetic retained evidence bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, absentIsEmpty := range []bool{false, true} {
		file := File{Path: filepath.Join(root, "absent.json"), AbsentIsEmpty: absentIsEmpty}
		want := "cannot resolve the protection document"
		if absentIsEmpty {
			want = "no protection control is registered under that name"
		}
		read, err := file.Read()
		if absentIsEmpty && (err != nil || read.Schema != Schema || len(read.Controls) != 0) {
			t.Errorf("an absent document read as empty answered %+v (%v)", read, err)
		}
		if !absentIsEmpty && (err == nil || err.Error() != want) {
			t.Errorf("an absent document was read: %v", err)
		}
		for label, refused := range map[string]func() error{
			"rotate": func() error { _, _, err := file.Rotate(ctx, "lab-evidence", registeredAt); return err },
			"retire": func() error { _, _, err := file.Retire("lab-evidence"); return err },
			"pack": func() error {
				_, _, err := file.Pack(ctx, "lab-evidence", []string{evidence}, filepath.Join(root, "packed"), registeredAt)
				return err
			},
			"open": func() error {
				_, _, err := file.Open(ctx, "lab-evidence", filepath.Join(root, "packed"), filepath.Join(root, "opened"))
				return err
			},
		} {
			if err := refused(); err == nil || err.Error() != want || StepOf(err) != DocumentStep {
				t.Errorf("%s of an absent document (read as empty: %v) answered %v", label, absentIsEmpty, err)
			}
		}
		for _, name := range []string{"absent.json", "packed", "opened"} {
			absent(t, filepath.Join(root, name), "an absent document")
		}
	}
}

// A package is written only under a control the document registers as active,
// and opened under the control that wrote it: the package's own when none is
// named. Each refusal names its step, and none leaves a package behind.
func TestPackWritesUnderAnActiveControlAndOpenDefaultsToThePackagesOwn(t *testing.T) {
	root, _ := packed(t)
	evidence := filepath.Join(root, "run-2026-09-18")
	file := registered(t, "archive-2025", "lab-evidence")
	t.Setenv(storeSwitch, "emit")
	ctx := context.Background()
	at := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	packet := filepath.Join(root, "packet")

	descriptor, notRead, err := file.Pack(ctx, "lab-evidence", []string{evidence}, packet, at)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if descriptor.Control != "lab-evidence" || descriptor.Generation != 1 || len(descriptor.Entries) != 3 || notRead != 0 ||
		!descriptor.RetainUntil.Equal(at.Add(2160*time.Hour)) {
		t.Fatalf("the package declares %+v with %d not read", descriptor, notRead)
	}
	opened, index, err := file.Open(ctx, "", packet, filepath.Join(root, "opened"))
	if err != nil || opened.Package != descriptor.Package || len(index.Entries) != 3 {
		t.Fatalf("open under the package's own control: %+v (%v)", opened, err)
	}
	if raw := held(t, filepath.Join(root, "opened", "run-2026-09-18", "manifest.json")); raw != `{"schema":"readmit-run/v1"}` {
		t.Fatalf("the package did not open to the packed bytes: %q", raw)
	}

	pack := func(name string, roots ...string) func(string) error {
		return func(output string) error { _, _, err := file.Pack(ctx, name, roots, output, at); return err }
	}
	open := func(name, source string) func(string) error {
		return func(output string) error { _, _, err := file.Open(ctx, name, source, output); return err }
	}
	for _, refused := range []struct {
		label, want string
		step        Step
		run         func(string) error
	}{
		{"packing under a control nobody registered", "no protection control is registered under that name", DocumentStep, pack("never-registered", evidence)},
		{"packing a path that does not exist", "a path to pack could not be resolved", SourcesStep, pack("lab-evidence", filepath.Join(root, "missing"))},
		{"opening under a control nobody registered", "no protection control is registered under that name", DocumentStep, open("never-registered", packet)},
		{"opening under another registered control", "the package was written under a different protection control than the one named", PackageStep, open("archive-2025", packet)},
		{"opening what is not a package under its own control", "a transfer package must be a regular directory", DescriptorStep, open("", filepath.Join(root, "missing"))},
		{"opening what is not a package under a named control", "a transfer package must be a regular directory", PackageStep, open("lab-evidence", filepath.Join(root, "missing"))},
	} {
		output := filepath.Join(root, strings.ReplaceAll(refused.label, " ", "-"))
		if err := refused.run(output); err == nil || err.Error() != refused.want || StepOf(err) != refused.step {
			t.Errorf("%s answered %v at step %d, want %q at step %d", refused.label, err, StepOf(err), refused.want, refused.step)
		}
		absent(t, output, refused.label)
	}

	// A key the declared store does not answer with writes no package.
	t.Setenv(storeSwitch, "fail")
	unread := filepath.Join(root, "unread")
	if err := pack("lab-evidence", evidence)(unread); err == nil || err.Error() != "the key could not be read from its declared store" || StepOf(err) != PackageStep {
		t.Errorf("packing without a key answered %v at step %d", err, StepOf(err))
	}
	absent(t, unread, "packing without a key")

	// A retired control writes no new package and still opens what it wrote.
	t.Setenv(storeSwitch, "emit")
	if _, _, err := file.Retire("lab-evidence"); err != nil {
		t.Fatal(err)
	}
	after := filepath.Join(root, "after-retirement")
	if err := pack("lab-evidence", evidence)(after); err == nil ||
		err.Error() != "the protection control is retired; it opens the packages it wrote and writes no new one" || StepOf(err) != DocumentStep {
		t.Errorf("a retired control answered %v at step %d", err, StepOf(err))
	}
	absent(t, after, "a retired control")
	if _, _, err := file.Open(ctx, "", packet, filepath.Join(root, "reopened")); err != nil {
		t.Errorf("a retired control did not open what it wrote: %v", err)
	}
}

// An interrupted document write is retained beside the document and reported,
// never reused, and the previous document is left exactly as it was.
func TestAnInterruptedDocumentWriteIsReportedAndThePreviousDocumentKept(t *testing.T) {
	file := registered(t, "lab-evidence")
	before := held(t, file.Path)
	if err := os.WriteFile(file.Path+".incomplete", []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(storeSwitch, "emit")
	if _, _, err := file.Rotate(context.Background(), "lab-evidence", registeredAt.Add(time.Hour)); err == nil {
		t.Fatal("a retained interrupted write was reused")
	}
	if after := held(t, file.Path); after != before {
		t.Fatal("a refused write changed the previous document")
	}
	if err := os.Remove(file.Path + ".incomplete"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := file.Rotate(context.Background(), "lab-evidence", registeredAt.Add(time.Hour)); err != nil {
		t.Fatalf("rotate after clearing the interrupted write: %v", err)
	}
	reopened, err := file.Read()
	if err != nil || reopened.Controls[0].Generation != 2 {
		t.Fatalf("reopened %+v (%v)", reopened, err)
	}
}
