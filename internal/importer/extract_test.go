package importer_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
)

// message is one complete HL7 v2 message terminated with CR. Every value in it
// is synthetic and belongs to this test alone.
func message(control string) string {
	return "MSH|^~\\&|READMIT|SYNTHETIC|RECEIVER|LAB|20260101120000||ADT^A08|" + control + "|P|2.5.1\rPID|1||SYNTH-" + control + "\r"
}

func framed(control string) string { return "\x0b" + message(control) + "\x1c\r" }

func plan(t *testing.T, framing importer.Framing, boundary importer.Boundary, terminator string, encoding importer.Encoding, direction string, members ...string) importer.Plan {
	t.Helper()
	if members == nil {
		members = []string{}
	}
	declared := importer.Plan{Schema: importer.PlanSchema, Framing: framing, BatchBoundary: boundary, Terminator: hl7.Terminator(terminator), Encoding: encoding, Direction: bundle.Direction(direction), Members: members}
	if err := declared.Validate(); err != nil {
		t.Fatalf("test plan is not valid: %v", err)
	}
	return declared
}

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// extract runs one extraction and fails the test when it is refused.
func extract(t *testing.T, declared importer.Plan, files, folders, archives []string) *importer.Extraction {
	t.Helper()
	e, err := importer.Extract(t.Context(), declared, files, folders, archives)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return e
}

func TestFolderImportPreservesEveryMemberByteAndExcludesUndeclaredNames(t *testing.T) {
	folder := t.TempDir()
	first := framed("AAA") + framed("BBB")
	write(t, folder, "b.mllp", first)
	write(t, folder, "nested/a.mllp", framed("CCC"))
	write(t, folder, "notes.txt", "operator notes, not evidence")
	e := extract(t, plan(t, importer.MLLPFraming, "", "cr", importer.UTF8, "inbound", ".mllp"), nil, []string{folder}, nil)
	if len(e.Containers) != 1 || e.Containers[0].Kind != importer.FolderContainer {
		t.Fatalf("folder container not recorded: %+v", e.Containers)
	}
	if e.Totals.Members != 3 || e.Totals.Excluded != 1 || e.Totals.Sources != 2 || e.Totals.Occurrences != 3 {
		t.Fatalf("folder totals: %+v", e.Totals)
	}
	// fs.WalkDir walks in lexical order, so a folder import reads the same
	// members in the same order on every machine.
	var names []string
	for _, member := range e.Containers[0].Members {
		names = append(names, member.Name)
		if member.Name == "notes.txt" && (member.State != importer.Excluded || member.Reason != importer.ReasonSuffix) {
			t.Fatalf("undeclared name was not excluded with a reason: %+v", member)
		}
	}
	if strings.Join(names, ",") != "b.mllp,nested/a.mllp,notes.txt" {
		t.Fatalf("folder members were not read in lexical order: %v", names)
	}
	destination := filepath.Join(t.TempDir(), "case")
	importedAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	b, err := bundle.Write(destination, e.Inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	// Concatenating the stored occurrences of a source reproduces the record
	// the member contributed, byte for byte, including its framing.
	reopened, err := bundle.Open(destination)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := sourceBytes(t, reopened, "s0001"); got != first {
		t.Fatalf("an imported member lost bytes: %q", got)
	}
	for _, event := range reopened.Events {
		if event.Direction != bundle.Inbound {
			t.Fatalf("the declared direction did not reach every occurrence: %+v", event)
		}
		if event.ObservedAt != nil {
			t.Fatal("an import invented an observed time")
		}
	}
	receipt, err := importer.NewReceipt(plan(t, importer.MLLPFraming, "", "cr", importer.UTF8, "inbound", ".mllp"), e, importedAt, b)
	if err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if receipt.Case.Identity != b.Identity || receipt.Case.Provenance != string(bundle.Imported) || len(receipt.Quarantined) != 0 {
		t.Fatalf("receipt does not describe the evidence it names: %+v", receipt)
	}
}

func TestArchiveImportReadsEntriesInNameOrderAndRecordsTheContainerDigest(t *testing.T) {
	dir := t.TempDir()
	archive := buildArchive(t, filepath.Join(dir, "corpus.zip"), []archiveEntry{
		{name: "z.hl7", content: message("ZZZ")},
		{name: "a.hl7", content: message("AAA")},
		{name: "readme.md", content: "not evidence"},
		{name: "folder/", directory: true},
	})
	e := extract(t, plan(t, importer.RawFraming, "", "cr", importer.UTF8, "unknown", ".hl7"), nil, nil, []string{archive})
	container := e.Containers[0]
	if container.Kind != importer.ArchiveContainer || container.SHA256 == "" || container.Size == 0 {
		t.Fatalf("archive container bytes were not identified: %+v", container)
	}
	var order []string
	for _, member := range container.Members {
		order = append(order, member.Name)
	}
	if strings.Join(order, ",") != "a.hl7,folder,readme.md,z.hl7" {
		t.Fatalf("archive entries were not read in name order: %v", order)
	}
	if container.Members[1].Reason != importer.ReasonDirectory || container.Members[2].Reason != importer.ReasonSuffix {
		t.Fatalf("archive exclusions were not recorded with their reasons: %+v", container.Members)
	}
	if e.Totals.Sources != 2 || e.Totals.Excluded != 2 {
		t.Fatalf("archive totals: %+v", e.Totals)
	}
	// Every source of an archive names the archive, because that is the file
	// the import actually read; the entry is recorded in the receipt instead.
	for _, input := range e.Inputs {
		if input.Path != container.Path {
			t.Fatalf("an archive source named something other than the archive: %s", input.Path)
		}
	}
}

func TestDeclaredBatchBoundarySplitsWithoutLosingABytes(t *testing.T) {
	dir := t.TempDir()
	content := message("ONE") + message("TWO") + message("THREE")
	file := write(t, dir, "batch.hl7", content)
	e := extract(t, plan(t, importer.BatchFraming, importer.SegmentStart, "cr", importer.UTF8, "outbound"), []string{file}, nil, nil)
	records := e.Containers[0].Members[0].Records
	if len(records) != 3 {
		t.Fatalf("declared boundary produced %d records", len(records))
	}
	var rebuilt string
	for i, record := range records {
		if record.Offset != len(rebuilt) || record.Occurrences != 1 {
			t.Fatalf("record %d does not follow its predecessor: %+v", i, record)
		}
		rebuilt += string(e.Inputs[i].Data)
	}
	if rebuilt != content {
		t.Fatal("a declared batch split lost bytes of the member")
	}
}

func TestHL7BatchRetainsEnvelopeSegmentsAsTheirOwnRecords(t *testing.T) {
	dir := t.TempDir()
	content := "FHS|^~\\&|READMIT\rBHS|^~\\&|READMIT\r" + message("ONE") + message("TWO") + "BTS|2\rFTS|1\r"
	file := write(t, dir, "batch.hl7", content)
	e := extract(t, plan(t, importer.BatchFraming, importer.HL7Batch, "cr", importer.UTF8, "unknown"), []string{file}, nil, nil)
	records := e.Containers[0].Members[0].Records
	if len(records) != 6 {
		t.Fatalf("the batch envelope was not retained as its own records: %d", len(records))
	}
	destination := filepath.Join(t.TempDir(), "case")
	importedAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	b, err := bundle.Write(destination, e.Inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	receipt, err := importer.NewReceipt(plan(t, importer.BatchFraming, importer.HL7Batch, "cr", importer.UTF8, "unknown"), e, importedAt, b)
	if err != nil {
		t.Fatalf("receipt: %v", err)
	}
	// The four envelope segments carry no message, so they are retained as
	// quarantined evidence with the parser's own reason rather than dropped.
	if len(receipt.Quarantined) != 4 {
		t.Fatalf("envelope records were not quarantined: %+v", receipt.Quarantined)
	}
	for _, held := range receipt.Quarantined {
		if held.Reason == "" || strings.Contains(held.Reason, "READMIT") {
			t.Fatalf("quarantine reason is empty or repeats evidence: %+v", held)
		}
	}
	if counts := b.Counts(); counts[bundle.Message] != 2 || counts[bundle.Unparsed] != 4 {
		t.Fatalf("batch evidence was not classified: %v", counts)
	}
}

func TestPreviewReportsExactlyWhatTheImportWrites(t *testing.T) {
	dir := t.TempDir()
	file := write(t, dir, "session.mllp", framed("AAA")+framed("BBB")+"\x0bTRUNCATED")
	declared := plan(t, importer.MLLPFraming, "", "cr", importer.UTF8, "inbound")
	e := extract(t, declared, []string{file}, nil, nil)
	preview := importer.NewPreview(declared, e)
	if preview.Schema != importer.PreviewSchema || preview.Totals.Occurrences != 3 {
		t.Fatalf("preview did not report the occurrences it would extract: %+v", preview.Totals)
	}
	encoded, err := importer.EncodePreview(preview)
	if err != nil {
		t.Fatalf("encode preview: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"schema":"readmit-import-preview/v1"`)) || bytes.Contains(encoded, []byte("SYNTH-AAA")) {
		t.Fatalf("preview document is mislabelled or carries evidence: %s", encoded)
	}
	destination := filepath.Join(t.TempDir(), "case")
	importedAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	b, err := bundle.Write(destination, e.Inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	receipt, err := importer.NewReceipt(declared, e, importedAt, b)
	if err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if receipt.Totals != preview.Totals {
		t.Fatalf("the receipt disagrees with the preview: %+v %+v", receipt.Totals, preview.Totals)
	}
	if len(b.Manifest.Sources) != 1 || b.Manifest.Sources[0].Occurrences != preview.Containers[0].Members[0].Records[0].Occurrences {
		t.Fatalf("the preview predicted a different occurrence count: %+v", b.Manifest.Sources)
	}
	// The truncated frame is retained whole and quarantined; nothing guesses a
	// resynchronization point inside it.
	if len(receipt.Quarantined) != 1 {
		t.Fatalf("the truncated frame was not quarantined: %+v", receipt.Quarantined)
	}
}

func TestAmbiguousSplittingIsRefusedByName(t *testing.T) {
	dir := t.TempDir()
	for name, testcase := range map[string]struct {
		declared importer.Plan
		content  string
		want     error
	}{
		"two raw messages":           {plan(t, importer.RawFraming, "", "cr", importer.UTF8, "unknown"), message("ONE") + message("TWO"), importer.ErrAmbiguousBatch},
		"raw over framed bytes":      {plan(t, importer.RawFraming, "", "cr", importer.UTF8, "unknown"), framed("ONE"), importer.ErrDeclaredFraming},
		"batch over framed bytes":    {plan(t, importer.BatchFraming, importer.SegmentStart, "cr", importer.UTF8, "unknown"), framed("ONE"), importer.ErrDeclaredFraming},
		"framed over raw bytes":      {plan(t, importer.MLLPFraming, "", "cr", importer.UTF8, "unknown"), message("ONE"), importer.ErrDeclaredFraming},
		"hl7 batch with no envelope": {plan(t, importer.BatchFraming, importer.HL7Batch, "cr", importer.UTF8, "unknown"), message("ONE"), importer.ErrDeclaredFraming},
		"terminator misses a header": {plan(t, importer.BatchFraming, importer.SegmentStart, "crlf", importer.UTF8, "unknown"), message("ONE") + message("TWO"), importer.ErrAmbiguousBatch},
		"bytes are not utf-8":        {plan(t, importer.RawFraming, "", "cr", importer.UTF8, "unknown"), "MSH|^~\\&|\xff\xfe\rPID|1\r", importer.ErrDeclaredEncoding},
		"bytes are not ascii":        {plan(t, importer.RawFraming, "", "cr", importer.USASCII, "unknown"), "MSH|^~\\&|é\rPID|1\r", importer.ErrDeclaredEncoding},
	} {
		file := write(t, dir, strings.ReplaceAll(name, " ", "-")+".hl7", testcase.content)
		_, err := importer.Extract(t.Context(), testcase.declared, []string{file}, nil, nil)
		if !errors.Is(err, testcase.want) {
			t.Errorf("%s: want %v, got %v", name, testcase.want, err)
		}
		if err != nil && (strings.Contains(err.Error(), "SYNTH") || strings.Contains(err.Error(), file)) {
			t.Errorf("%s: refusal repeated a name or a value: %v", name, err)
		}
	}
}

// A declared boundary that finds no message is not an ambiguity: there is no
// boundary to guess, so the member stays one record and the case quarantines it
// with all of its bytes, exactly as malformed evidence is treated everywhere.
func TestABatchMemberWithNoMessageHeaderIsQuarantinedRatherThanRefused(t *testing.T) {
	dir := t.TempDir()
	file := write(t, dir, "orphan.hl7", "PID|1||SYNTH-ORPHAN\r")
	declared := plan(t, importer.BatchFraming, importer.SegmentStart, "cr", importer.UTF8, "unknown")
	e := extract(t, declared, []string{file}, nil, nil)
	records := e.Containers[0].Members[0].Records
	if len(records) != 1 || records[0].Offset != 0 || records[0].Size != len("PID|1||SYNTH-ORPHAN\r") {
		t.Fatalf("a member with no message header was divided: %+v", records)
	}
	importedAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	b, err := bundle.Write(filepath.Join(t.TempDir(), "case"), e.Inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := importer.NewReceipt(declared, e, importedAt, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Quarantined) != 1 {
		t.Fatalf("the member was not quarantined: %+v", receipt.Quarantined)
	}
	raw, err := b.Raw(receipt.Quarantined[0].EventID)
	if err != nil || string(raw) != "PID|1||SYNTH-ORPHAN\r" {
		t.Fatal("quarantined evidence lost its bytes")
	}
}

func TestMoreContainersThanOneImportReadsAreRefusedBeforeReading(t *testing.T) {
	dir := t.TempDir()
	file := write(t, dir, "one.hl7", message("ONE"))
	files := make([]string, importer.MaxContainers+1)
	for i := range files {
		files[i] = file
	}
	_, err := importer.Extract(t.Context(), plan(t, importer.RawFraming, "", "cr", importer.UTF8, "unknown"), files, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("an unbounded container list was accepted: %v", err)
	}
}

func TestUncheckableEncodingsAreRecordedRatherThanRefused(t *testing.T) {
	dir := t.TempDir()
	file := write(t, dir, "latin.hl7", "MSH|^~\\&|\xe9\rPID|1\r")
	for _, encoding := range []importer.Encoding{importer.Latin1, importer.UnknownEncoding} {
		e, err := importer.Extract(t.Context(), plan(t, importer.RawFraming, "", "cr", encoding, "unknown"), []string{file}, nil, nil)
		if err != nil {
			t.Fatalf("%s: %v", encoding, err)
		}
		if len(e.Inputs) != 1 || string(e.Inputs[0].Data) != "MSH|^~\\&|\xe9\rPID|1\r" {
			t.Fatalf("%s: bytes were transcoded or dropped", encoding)
		}
	}
}

func TestUnsafeArchiveEntriesAreRefused(t *testing.T) {
	dir := t.TempDir()
	for name, entries := range map[string][]archiveEntry{
		"absolute name":    {{name: "/etc/evidence.hl7", content: message("ONE")}},
		"parent traversal": {{name: "../escape.hl7", content: message("ONE")}},
		"nested traversal": {{name: "inside/../../escape.hl7", content: message("ONE")}},
		"symbolic link":    {{name: "link.hl7", content: "/etc/passwd", mode: fs.ModeSymlink | 0777}},
		"device entry":     {{name: "device.hl7", content: "", mode: fs.ModeDevice | 0600}},
		"duplicate names":  {{name: "same.hl7", content: message("ONE")}, {name: "same.hl7", content: message("TWO")}},
		"backslash name":   {{name: "a\\b.hl7", content: message("ONE")}},
	} {
		archive := buildArchive(t, filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".zip"), entries)
		_, err := importer.Extract(t.Context(), plan(t, importer.RawFraming, "", "cr", importer.UTF8, "unknown"), nil, nil, []string{archive})
		if !errors.Is(err, importer.ErrUnsafeEntry) {
			t.Errorf("%s was not refused as an unsafe entry: %v", name, err)
		}
	}
}

func TestDeclaredContainerKindIsCheckedAgainstTheFilesystem(t *testing.T) {
	dir := t.TempDir()
	file := write(t, dir, "one.hl7", message("ONE"))
	folder := t.TempDir()
	declared := plan(t, importer.RawFraming, "", "cr", importer.UTF8, "unknown")
	if _, err := importer.Extract(t.Context(), declared, []string{folder}, nil, nil); err == nil {
		t.Error("a folder declared as a file was imported")
	}
	if _, err := importer.Extract(t.Context(), declared, nil, []string{file}, nil); err == nil {
		t.Error("a file declared as a folder was imported")
	}
	if _, err := importer.Extract(t.Context(), declared, nil, nil, []string{file}); err == nil {
		t.Error("a message file declared as an archive was imported")
	}
	if _, err := importer.Extract(t.Context(), declared, nil, nil, nil); err == nil {
		t.Error("an import with no declared container was accepted")
	}
}

func TestCancellationStopsBeforeReadingAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	file := write(t, dir, "one.hl7", message("ONE"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := importer.Extract(ctx, plan(t, importer.RawFraming, "", "cr", importer.UTF8, "unknown"), []string{file}, nil, nil); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("a cancelled import was not refused: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("a cancelled import changed the filesystem: %v %d", err, len(entries))
	}
}

func TestReceiptRefusesEvidenceItDoesNotDescribe(t *testing.T) {
	dir := t.TempDir()
	file := write(t, dir, "one.hl7", message("ONE"))
	declared := plan(t, importer.RawFraming, "", "cr", importer.UTF8, "unknown")
	e := extract(t, declared, []string{file}, nil, nil)
	other := extract(t, declared, []string{write(t, dir, "two.hl7", message("TWO"))}, nil, nil)
	importedAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	b, err := bundle.Write(filepath.Join(t.TempDir(), "case"), e.Inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := importer.NewReceipt(declared, other, importedAt, b); err == nil {
		t.Fatal("a receipt described a case it was not extracted from")
	}
	if _, err := importer.EncodeReceipt(importer.Receipt{Schema: importer.ReceiptSchema}); err == nil {
		t.Fatal("a receipt naming no case was encoded")
	}
}

func sourceBytes(t *testing.T, b *bundle.Bundle, source string) string {
	t.Helper()
	var joined []byte
	for _, event := range b.Events {
		if event.SourceID != source {
			continue
		}
		raw, err := b.Raw(event.ID)
		if err != nil {
			t.Fatal(err)
		}
		joined = append(joined, raw...)
	}
	return string(joined)
}

type archiveEntry struct {
	name      string
	content   string
	mode      fs.FileMode
	directory bool
}

func buildArchive(t *testing.T, path string, entries []archiveEntry) string {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, item := range entries {
		header := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
		if item.directory {
			header.SetMode(fs.ModeDir | 0700)
		} else if item.mode != 0 {
			header.SetMode(item.mode)
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(item.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMembersBeyondTheArtifactLimitsAreRefused(t *testing.T) {
	dir := t.TempDir()
	var batch strings.Builder
	for i := range bundle.MaxSources + 1 {
		batch.WriteString(message("C" + strconv.Itoa(i)))
	}
	file := write(t, dir, "large.hl7", batch.String())
	_, err := importer.Extract(t.Context(), plan(t, importer.BatchFraming, importer.SegmentStart, "cr", importer.UTF8, "unknown"), []string{file}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "more records than one import writes") {
		t.Fatalf("a member past the source limit was not refused: %v", err)
	}
}

func TestAContainerWithNoDeclaredMemberExtractsNothing(t *testing.T) {
	folder := t.TempDir()
	write(t, folder, "notes.txt", "operator notes, not evidence")
	e := extract(t, plan(t, importer.RawFraming, "", "cr", importer.UTF8, "unknown", ".hl7"), nil, []string{folder}, nil)
	if len(e.Inputs) != 0 || e.Totals.Excluded != 1 {
		t.Fatalf("an unmatched entry was imported: %+v", e.Totals)
	}
	// A preview of a plan that matches nothing is exactly how an operator sees
	// that it matches nothing, so the extraction itself is not an error.
	preview := importer.NewPreview(plan(t, importer.RawFraming, "", "cr", importer.UTF8, "unknown", ".hl7"), e)
	if preview.Containers[0].Members[0].Reason != importer.ReasonSuffix {
		t.Fatalf("the preview does not say why nothing was selected: %+v", preview.Containers)
	}
}
