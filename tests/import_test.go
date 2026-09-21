package tests

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/importer"
)

const importMessage = "MSH|^~\\&|READMIT|SYNTHETIC|RECEIVER|LAB|20260101120000||ADT^A08|IMPORT-%s|P|2.5.1\rPID|1||SYNTH-IMPORT-%s\r"

func importFixture(control string) string {
	return strings.ReplaceAll(importMessage, "%s", control)
}

func importFolder(t *testing.T) (string, string) {
	t.Helper()
	folder := t.TempDir()
	for name, content := range map[string]string{
		"a.hl7":    importFixture("AAA"),
		"b.hl7":    importFixture("BBB"),
		"notes.md": "operator notes, not evidence",
	} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan := writeDocument(t, t.TempDir(), "plan.json",
		`{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[".hl7"]}`)
	return folder, plan
}

func TestImportPreviewsWithoutWritingAndThenRecordsWhatItWrote(t *testing.T) {
	folder, plan := importFolder(t)
	stdout, stderr, err := run(t, "import", "--plan", plan, "--folder", folder, "--preview")
	if err != nil || stderr != "" {
		t.Fatalf("preview: %v %s", err, stderr)
	}
	preview := readStrictOutput[importer.Preview](t, stdout)
	if preview.Schema != importer.PreviewSchema || preview.Totals.Members != 3 || preview.Totals.Excluded != 1 || preview.Totals.Sources != 2 {
		t.Fatalf("preview totals: %+v", preview.Totals)
	}
	if strings.Contains(stdout, "SYNTH-IMPORT") {
		t.Fatal("the preview carried message evidence")
	}
	entries, err := os.ReadDir(folder)
	if err != nil || len(entries) != 3 {
		t.Fatalf("the preview changed the declared folder: %v %d", err, len(entries))
	}

	// The same declarations stated on the command line produce the same plan,
	// so nothing has to be hand-authored as JSON to run an import.
	declaredStdout, stderr, err := run(t, "import", "--folder", folder, "--preview",
		"--framing", "raw", "--terminator", "cr", "--encoding", "utf-8", "--direction", "inbound", "--member", ".hl7")
	if err != nil || stderr != "" || declaredStdout != stdout {
		t.Fatalf("declaration flags did not produce the saved plan: %v %s", err, stderr)
	}

	destination := filepath.Join(t.TempDir(), "incident.case")
	receiptPath := filepath.Join(t.TempDir(), "incident-import.json")
	stdout, stderr, err = run(t, "import", "--plan", plan, "--folder", folder, "--output", destination, "--receipt", receiptPath)
	if err != nil || stderr != "" {
		t.Fatalf("import: %v %s", err, stderr)
	}
	for _, want := range []string{"Import plan: readmit-import-plan/v1", "Framing: raw", "Batch boundary: not declared", "Terminator: cr", "Encoding: utf-8", "Direction: inbound", "Containers: 1", "Members: 3", "Excluded members: 1", "Extracted sources: 2", "Quarantined occurrences: 0", "Schema: readmit-case/v1", "Provenance: imported", "Messages: 2"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing import summary %q:\n%s", want, stdout)
		}
	}
	for _, secret := range []string{"SYNTH-IMPORT", "a.hl7", "notes.md", folder, destination} {
		if strings.Contains(stdout+stderr, secret) {
			t.Errorf("the import summary disclosed %q", secret)
		}
	}
	// The raw receipt bytes stay for the exclusion check below.
	data, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt := readStrictOutput[importer.Receipt](t, string(data))
	b, err := bundle.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Case.Identity != b.Identity || receipt.Case.Schema != bundle.Schema || receipt.Case.Provenance != string(bundle.Imported) {
		t.Fatalf("the receipt does not name the case beside it: %+v", receipt.Case)
	}
	if receipt.Totals != preview.Totals {
		t.Fatalf("the receipt disagrees with the preview: %+v %+v", receipt.Totals, preview.Totals)
	}
	if receipt.ImportedAt.IsZero() || b.Manifest.Provenance.ImportedAt == nil || !receipt.ImportedAt.Equal(*b.Manifest.Provenance.ImportedAt) {
		t.Fatal("the receipt and the case disagree about the import time")
	}
	// The receipt records where the evidence came from, which is what makes it
	// a receipt. That belongs in a file the person keeps, not on the terminal.
	if !strings.Contains(string(data), "notes.md") || !strings.Contains(string(data), importer.ReasonSuffix) {
		t.Fatal("the receipt does not account for the excluded member")
	}
	if strings.Contains(string(data), "SYNTH-IMPORT") {
		t.Fatal("the receipt carried message evidence")
	}
}

func TestImportRefusesAmbiguousSplittingAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	member := filepath.Join(dir, "batch.hl7")
	if err := os.WriteFile(member, []byte(importFixture("ONE")+importFixture("TWO")), 0600); err != nil {
		t.Fatal(err)
	}
	plan := writeDocument(t, dir, "raw.json",
		`{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"unknown","members":[]}`)
	destination := filepath.Join(dir, "refused.case")
	receiptPath := filepath.Join(dir, "refused.json")
	stdout, stderr, err := run(t, "import", "--plan", plan, "--file", member, "--output", destination, "--receipt", receiptPath)
	if err == nil || stdout != "" || !strings.Contains(stderr, "without guessing a message boundary") {
		t.Fatalf("an ambiguous split was not refused by name: %v %s %s", err, stdout, stderr)
	}
	if strings.Contains(stderr, "SYNTH-IMPORT") || strings.Contains(stderr, "batch.hl7") {
		t.Fatalf("the refusal echoed a name or a value: %s", stderr)
	}
	for _, path := range []string{destination, receiptPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("a refused import left %s behind", filepath.Base(path))
		}
	}
	// Declaring the boundary is the recovery: the same bytes import once the
	// operator says where a message begins.
	declared := writeDocument(t, dir, "batch.json",
		`{"schema":"readmit-import-plan/v1","framing":"batch","batch_boundary":"segment-start","terminator":"cr","encoding":"utf-8","direction":"unknown","members":[]}`)
	stdout, stderr, err = run(t, "import", "--plan", declared, "--file", member, "--output", destination, "--receipt", receiptPath)
	if err != nil || stderr != "" || !strings.Contains(stdout, "Extracted sources: 2") {
		t.Fatalf("a declared boundary did not import: %v %s %s", err, stderr, stdout)
	}
}

func TestImportRefusesUnsafeArchiveEntriesAndReusedDestinations(t *testing.T) {
	dir := t.TempDir()
	plan := writeDocument(t, dir, "plan.json",
		`{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"unknown","members":[".hl7"]}`)
	unsafe := filepath.Join(dir, "unsafe.zip")
	writeArchive(t, unsafe, map[string]string{"../escape.hl7": importFixture("ONE")})
	stdout, stderr, err := run(t, "import", "--plan", plan, "--archive", unsafe, "--output", filepath.Join(dir, "a.case"), "--receipt", filepath.Join(dir, "a.json"))
	if err == nil || stdout != "" || !strings.Contains(stderr, "one relative path of regular file bytes") {
		t.Fatalf("an archive traversal entry was not refused: %v %s %s", err, stdout, stderr)
	}

	safe := filepath.Join(dir, "safe.zip")
	writeArchive(t, safe, map[string]string{"exports/one.hl7": importFixture("ONE")})
	destination := filepath.Join(dir, "archive.case")
	receiptPath := filepath.Join(dir, "archive.json")
	if stdout, stderr, err = run(t, "import", "--plan", plan, "--archive", safe, "--output", destination, "--receipt", receiptPath); err != nil || stderr != "" {
		t.Fatalf("archive import: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Extracted sources: 1") {
		t.Fatalf("archive import summary: %s", stdout)
	}
	if _, _, err = run(t, "import", "--plan", plan, "--archive", safe, "--output", destination, "--receipt", filepath.Join(dir, "other.json")); err == nil {
		t.Fatal("an existing case directory was overwritten")
	}
	stdout, stderr, err = run(t, "import", "--plan", plan, "--archive", safe, "--output", filepath.Join(dir, "other.case"), "--receipt", receiptPath)
	if err == nil || !strings.Contains(stderr, "receipt destination must be a new file") {
		t.Fatalf("an existing receipt was not refused: %v %s", err, stderr)
	}
	if _, err := os.Lstat(filepath.Join(dir, "other.case")); !os.IsNotExist(err) {
		t.Fatal("a refused receipt destination still left a case behind")
	}
}

func TestImportRefusesUndeclaredConfigurationAndProtectedDestinations(t *testing.T) {
	dir := t.TempDir()
	member := filepath.Join(dir, "one.hl7")
	if err := os.WriteFile(member, []byte(importFixture("ONE")), 0600); err != nil {
		t.Fatal(err)
	}
	plan := writeDocument(t, dir, "plan.json",
		`{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"unknown","members":[]}`)
	unknown := writeDocument(t, dir, "unknown.json",
		`{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"unknown","members":[],"split":"auto"}`)
	later := writeDocument(t, dir, "later.json",
		`{"schema":"readmit-import-plan/v9","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"unknown","members":[]}`)
	for name, args := range map[string][]string{
		"unknown plan member": {"--plan", unknown, "--file", member, "--output", filepath.Join(dir, "u.case"), "--receipt", filepath.Join(dir, "u.json")},
		"later plan version":  {"--plan", later, "--file", member, "--output", filepath.Join(dir, "l.case"), "--receipt", filepath.Join(dir, "l.json")},
		"no plan":             {"--file", member, "--output", filepath.Join(dir, "n.case"), "--receipt", filepath.Join(dir, "n.json")},
		"plan and flags":      {"--plan", plan, "--framing", "raw", "--file", member, "--output", filepath.Join(dir, "b.case"), "--receipt", filepath.Join(dir, "b.json")},
		"partial declaration": {"--framing", "raw", "--file", member, "--output", filepath.Join(dir, "d.case"), "--receipt", filepath.Join(dir, "d.json")},
		"undeclared framing":  {"--framing", "auto", "--terminator", "cr", "--encoding", "utf-8", "--direction", "unknown", "--file", member, "--output", filepath.Join(dir, "f.case"), "--receipt", filepath.Join(dir, "f.json")},
		"no container":        {"--plan", plan, "--output", filepath.Join(dir, "c.case"), "--receipt", filepath.Join(dir, "c.json")},
		"no destination":      {"--plan", plan, "--file", member},
		"preview and output":  {"--plan", plan, "--file", member, "--preview", "--output", filepath.Join(dir, "p.case")},
		"receipt only":        {"--plan", plan, "--file", member, "--receipt", filepath.Join(dir, "r.json")},
	} {
		stdout, stderr, err := run(t, append([]string{"import"}, args...)...)
		if err == nil || stdout != "" || stderr == "" {
			t.Errorf("%s was accepted: %v %s", name, err, stdout)
		}
	}

	// A case is retained evidence, so an import can neither write its own
	// receipt inside one nor create a case inside one.
	evidence := filepath.Join(dir, "retained.case")
	if _, _, err := run(t, "capture", member, "--output", evidence); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "import", "--plan", plan, "--file", member, "--output", filepath.Join(dir, "inside.case"), "--receipt", filepath.Join(evidence, "receipt.json"))
	if err == nil || stdout != "" || !strings.Contains(stderr, "outside the immutable input case") {
		t.Fatalf("a receipt inside retained evidence was accepted: %v %s %s", err, stdout, stderr)
	}
	if _, err := os.Lstat(filepath.Join(dir, "inside.case")); !os.IsNotExist(err) {
		t.Fatal("a refused receipt destination was checked after the case was written")
	}
}

func TestImportQuarantinesMalformedEvidenceAndKeepsItsBytes(t *testing.T) {
	dir := t.TempDir()
	folder := t.TempDir()
	content := "\x0b" + importFixture("ONE") + "\x1c\r" + "\x0bTRUNCATED FRAME"
	if err := os.WriteFile(filepath.Join(folder, "session.mllp"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	plan := writeDocument(t, dir, "plan.json",
		`{"schema":"readmit-import-plan/v1","framing":"mllp","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[".mllp"]}`)
	destination := filepath.Join(dir, "malformed.case")
	receiptPath := filepath.Join(dir, "malformed.json")
	stdout, stderr, err := run(t, "import", "--plan", plan, "--folder", folder, "--output", destination, "--receipt", receiptPath)
	if err != nil || stderr != "" {
		t.Fatalf("import: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Quarantined occurrences: 1") || !strings.Contains(stdout, "Unparsed: 1") {
		t.Fatalf("malformed evidence was not reported as quarantined:\n%s", stdout)
	}
	data, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt := readStrictOutput[importer.Receipt](t, string(data))
	if len(receipt.Quarantined) != 1 || !strings.Contains(receipt.Quarantined[0].Reason, "invalid MLLP framing") {
		t.Fatalf("the receipt does not name why the record was quarantined: %+v", receipt.Quarantined)
	}
	b, err := bundle.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	var stored []byte
	for _, event := range b.Events {
		raw, err := b.Raw(event.ID)
		if err != nil {
			t.Fatal(err)
		}
		stored = append(stored, raw...)
	}
	if !bytes.Equal(stored, []byte(content)) {
		t.Fatal("quarantined evidence was repaired or dropped")
	}
	stdout, stderr, err = run(t, "timeline", destination, "--show-values")
	if err != nil || stderr != "" || !strings.Contains(stdout, "TRUNCATED FRAME") {
		t.Fatalf("quarantined bytes were not retained in the case: %v %s", err, stderr)
	}
}

func TestImportRefusesToWriteACaseWithNoMemberButStillPreviewsWhy(t *testing.T) {
	dir := t.TempDir()
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "notes.md"), []byte("operator notes"), 0600); err != nil {
		t.Fatal(err)
	}
	plan := writeDocument(t, dir, "plan.json",
		`{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"unknown","members":[".hl7"]}`)
	stdout, stderr, err := run(t, "import", "--plan", plan, "--folder", folder, "--preview")
	if err != nil || stderr != "" || !strings.Contains(stdout, importer.ReasonSuffix) {
		t.Fatalf("a preview of a plan that matches nothing did not say why: %v %s %s", err, stderr, stdout)
	}
	destination := filepath.Join(dir, "empty.case")
	stdout, stderr, err = run(t, "import", "--plan", plan, "--folder", folder, "--output", destination, "--receipt", filepath.Join(dir, "empty.json"))
	if err == nil || stdout != "" || !strings.Contains(stderr, "no member to import") {
		t.Fatalf("an import with no member was not refused: %v %s %s", err, stdout, stderr)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("an import with no member created a case")
	}
}

func writeArchive(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range entries {
		file, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}
