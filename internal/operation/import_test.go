package operation_test

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/engineexport"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/operation"
)

const sampleHL7 = "MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\rPID|||12345||DOE^JOHN|||M\r"

func validPlan() importer.Plan {
	return importer.Plan{
		Schema:     importer.PlanSchema,
		Framing:    importer.RawFraming,
		Terminator: hl7.CR,
		Encoding:   importer.UTF8,
		Direction:  bundle.Inbound,
		Members:    []string{".hl7"},
	}
}

func validRecipe() importer.Recipe {
	return importer.Recipe{
		Schema:   importer.RecipeSchema,
		Name:     "test-csv",
		Revision: 1,
		Envelope: importer.CSVEnvelope,
		Encoding: importer.UTF8,
		Members:  []string{".csv"},
		CSV: &importer.CSVDialect{
			Delimiter:       ",",
			RecordSeparator: importer.LFSeparator,
			Header:          importer.HeaderPresent,
			Fields:          4,
		},
		Payload: importer.PayloadMapping{
			Operator:   importer.VerbatimPayload,
			Locator:    importer.Locator{"payload"},
			Framing:    importer.RawFraming,
			Terminator: hl7.CR,
		},
		ObservedAt: importer.TimeMapping{
			Operator: importer.RFC3339Time,
			Locator:  importer.Locator{"time"},
		},
		Source: importer.LabelMapping{
			Operator: importer.DeclaredLabel,
			Declared: "test-system",
		},
		Direction: importer.DirectionMapping{
			Operator: importer.DeclaredDirection,
			Declared: bundle.Inbound,
		},
		Channel: importer.LabelMapping{
			Operator: importer.DeclaredLabel,
			Declared: "adt",
		},
	}
}

func TestImportPlanPreviewAndCommit(t *testing.T) {
	tempDir := t.TempDir()
	sourceFile := filepath.Join(tempDir, "msg1.hl7")
	if err := os.WriteFile(sourceFile, []byte(sampleHL7), 0600); err != nil {
		t.Fatal(err)
	}

	plan := validPlan()
	ctx := context.Background()

	// Preview
	preview, err := operation.ImportPlanPreview(ctx, plan, []string{sourceFile}, nil, nil)
	if err != nil {
		t.Fatalf("ImportPlanPreview failed: %v", err)
	}
	if preview.Totals.Sources != 1 || preview.Totals.Occurrences != 1 {
		t.Fatalf("unexpected preview totals: %+v", preview.Totals)
	}
	if len(preview.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(preview.Containers))
	}

	// Commit
	caseDir := filepath.Join(tempDir, "case-bundle")
	receiptFile := filepath.Join(tempDir, "receipt.json")
	b, receipt, err := operation.ImportPlanCommit(ctx, plan, []string{sourceFile}, nil, nil, caseDir, receiptFile)
	if err != nil {
		t.Fatalf("ImportPlanCommit failed: %v", err)
	}
	if b.Identity == "" {
		t.Fatal("empty bundle identity")
	}
	if receipt.Totals.Sources != 1 {
		t.Fatalf("unexpected receipt totals: %+v", receipt.Totals)
	}

	// Reopen verified case
	opened, err := operation.OpenCase(caseDir)
	if err != nil {
		t.Fatalf("OpenCase failed on committed case: %v", err)
	}
	if opened.Identity != b.Identity {
		t.Fatalf("expected identity %s, got %s", b.Identity, opened.Identity)
	}

	// Negative path: commit again to existing destinations should refuse
	_, _, err = operation.ImportPlanCommit(ctx, plan, []string{sourceFile}, nil, nil, caseDir, filepath.Join(tempDir, "receipt2.json"))
	if err == nil || !strings.Contains(err.Error(), "must be a new directory") {
		t.Fatalf("expected destination exists error, got %v", err)
	}
}

func TestImportRecipePreviewAndCommit(t *testing.T) {
	tempDir := t.TempDir()
	csvData := "time,payload,source,channel\n2026-01-01T12:00:00Z,\"" + sampleHL7 + "\",sys,ch\n"
	csvFile := filepath.Join(tempDir, "data.csv")
	if err := os.WriteFile(csvFile, []byte(csvData), 0600); err != nil {
		t.Fatal(err)
	}

	recipe := validRecipe()
	ctx := context.Background()

	// Preview
	preview, err := operation.ImportRecipePreview(ctx, recipe, []string{csvFile}, nil, nil)
	if err != nil {
		t.Fatalf("ImportRecipePreview failed: %v", err)
	}
	if preview.Totals.Sources != 1 {
		t.Fatalf("unexpected preview totals: %+v", preview.Totals)
	}
	if len(preview.Mappings) != 1 || preview.Mappings[0].State != importer.Mapped {
		t.Fatalf("expected 1 mapped record, got %+v", preview.Mappings)
	}

	// Commit
	caseDir := filepath.Join(tempDir, "recipe-case")
	receiptFile := filepath.Join(tempDir, "recipe-receipt.json")
	b, receipt, err := operation.ImportRecipeCommit(ctx, recipe, []string{csvFile}, nil, nil, caseDir, receiptFile)
	if err != nil {
		t.Fatalf("ImportRecipeCommit failed: %v", err)
	}
	if b.Identity == "" {
		t.Fatal("empty bundle identity")
	}
	if receipt.Totals.Sources != 1 || receipt.UnmappedRecords != 0 {
		t.Fatalf("unexpected receipt totals: %+v", receipt.Totals)
	}
}

func TestImportEnginePreviewAndCommit(t *testing.T) {
	tempDir := t.TempDir()
	exportFile := filepath.Join(tempDir, "export.dat")
	if err := os.WriteFile(exportFile, []byte(sampleHL7), 0600); err != nil {
		t.Fatal(err)
	}

	plan := engineexport.Plan{
		Schema:     engineexport.Schema,
		Engine:     "oie",
		Version:    "4.6.0",
		Format:     "raw",
		Terminator: "cr",
	}
	ctx := context.Background()

	// Preview
	preview, err := operation.ImportEnginePreview(ctx, plan, exportFile)
	if err != nil {
		t.Fatalf("ImportEnginePreview failed: %v", err)
	}
	if preview.Qualification != "unqualified" {
		t.Fatalf("expected unqualified qualification, got %s", preview.Qualification)
	}
	if len(preview.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(preview.Records))
	}

	// Commit
	caseDir := filepath.Join(tempDir, "engine-case")
	b, err := operation.ImportEngineCommit(ctx, plan, exportFile, caseDir)
	if err != nil {
		t.Fatalf("ImportEngineCommit failed: %v", err)
	}
	if b.Identity == "" {
		t.Fatal("empty bundle identity")
	}
}

func TestStagePastedContent(t *testing.T) {
	tempDir := t.TempDir()
	pastedData := []byte(sampleHL7)

	staged, err := operation.StagePastedContent(tempDir, "pasted-01.hl7", pastedData)
	if err != nil {
		t.Fatalf("StagePastedContent failed: %v", err)
	}
	readBack, err := os.ReadFile(staged)
	if err != nil {
		t.Fatal(err)
	}
	if string(readBack) != sampleHL7 {
		t.Fatalf("pasted content mismatch: expected %q, got %q", sampleHL7, string(readBack))
	}

	// Attempting to stage again with the same name should fail (exclusive creation)
	_, err = operation.StagePastedContent(tempDir, "pasted-01.hl7", pastedData)
	if err == nil {
		t.Fatal("expected error on existing staged file")
	}

	// Attempting path traversal name should fail
	_, err = operation.StagePastedContent(tempDir, "../traversal.hl7", pastedData)
	if err == nil {
		t.Fatal("expected error on path traversal")
	}
}

// Neither engine-import entry names the export it could not read: the
// diagnostic is the shared input reader's own, whichever way the file is
// unusable, so a path never reaches a terminal or a window.
func TestEngineImportDiagnosticsNeverQuoteTheExportPath(t *testing.T) {
	plan := engineexport.Plan{Schema: engineexport.Schema, Engine: "oie", Version: "4.6.0", Format: "raw", Terminator: "cr"}
	dir := t.TempDir()
	folder := filepath.Join(dir, "patient-named-folder")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"missing": filepath.Join(dir, "patient-named-export.dat"),
		"folder":  folder,
	} {
		t.Run(name, func(t *testing.T) {
			_, previewErr := operation.ImportEnginePreview(context.Background(), plan, path)
			_, commitErr := operation.ImportEngineCommit(context.Background(), plan, path, filepath.Join(dir, "case-"+name))
			for _, err := range []error{previewErr, commitErr} {
				if err == nil || err.Error() != "input must be a readable regular file" || strings.Contains(err.Error(), "patient-named") {
					t.Fatalf("an unreadable export was refused as %v", err)
				}
			}
			if _, err := os.Lstat(filepath.Join(dir, "case-"+name)); !os.IsNotExist(err) {
				t.Fatal("a refused engine import left a case behind")
			}
		})
	}
}

// Every import refuses a taken case or receipt destination before it reads
// or extracts anything, so a refusal never depends on what the evidence
// held: here every declared input is missing, and the destination is still
// what is refused.
func TestImportRefusesATakenDestinationBeforeReadingAnything(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	taken := filepath.Join(dir, "taken")
	if err := os.Mkdir(taken, 0700); err != nil {
		t.Fatal(err)
	}
	takenReceipt := filepath.Join(dir, "taken-receipt.json")
	if err := os.WriteFile(takenReceipt, []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	missing := []string{filepath.Join(dir, "missing.hl7")}
	engine := engineexport.Plan{Schema: engineexport.Schema, Engine: "oie", Version: "4.6.0", Format: "raw", Terminator: "cr"}
	for _, refused := range []struct {
		name   string
		want   error
		commit func(output, receipt string) error
	}{
		{"plan case", operation.ErrImportCaseExists, func(output, receipt string) error {
			_, _, err := operation.ImportPlanCommit(ctx, validPlan(), missing, nil, nil, taken, receipt)
			return err
		}},
		{"plan receipt", operation.ErrImportReceiptExists, func(output, receipt string) error {
			_, _, err := operation.ImportPlanCommit(ctx, validPlan(), missing, nil, nil, output, takenReceipt)
			return err
		}},
		{"recipe case", operation.ErrImportCaseExists, func(output, receipt string) error {
			_, _, err := operation.ImportRecipeCommit(ctx, validRecipe(), missing, nil, nil, taken, receipt)
			return err
		}},
		{"recipe receipt", operation.ErrImportReceiptExists, func(output, receipt string) error {
			_, _, err := operation.ImportRecipeCommit(ctx, validRecipe(), missing, nil, nil, output, takenReceipt)
			return err
		}},
		{"engine case", operation.ErrImportCaseExists, func(string, string) error {
			_, err := operation.ImportEngineCommit(ctx, engine, missing[0], taken)
			return err
		}},
	} {
		t.Run(refused.name, func(t *testing.T) {
			output, receipt := filepath.Join(dir, "fresh"), filepath.Join(dir, "fresh-receipt.json")
			if err := refused.commit(output, receipt); err != refused.want {
				t.Fatalf("refused as %v, want %v", err, refused.want)
			}
			for _, path := range []string{output, receipt} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("a refused import wrote %s", filepath.Base(path))
				}
			}
			if kept, err := os.ReadFile(takenReceipt); err != nil || string(kept) != "kept" {
				t.Fatal("a refused import touched the receipt already there")
			}
		})
	}
}

// A receipt may be named in folders that do not exist yet: the import creates
// them, owner-only, when it writes the receipt. It creates none when it is
// refused, and none inside retained evidence, which it refuses before
// extracting anything.
func TestImportCreatesAReceiptsMissingFoldersOnlyWhenItWritesOne(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	input := filepath.Join(dir, "one.hl7")
	if err := os.WriteFile(input, []byte(sampleHL7), 0600); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(dir, "receipts", "2026", "one-receipt.json")
	_, written, err := operation.ImportPlanCommit(ctx, validPlan(), []string{input}, nil, nil, filepath.Join(dir, "one"), receipt)
	if err != nil {
		t.Fatal(err)
	}
	if encoded, err := importer.EncodeReceipt(written); err != nil || !bytes.Equal(readFile(t, receipt), encoded) {
		t.Fatalf("the receipt in new folders is not the receipt the import made: %v", err)
	}
	for _, folder := range []string{filepath.Join(dir, "receipts"), filepath.Dir(receipt)} {
		if info, err := os.Stat(folder); err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("created folder %s: %v %v", filepath.Base(folder), info, err)
		}
	}

	// Nothing to import: refused after the destinations were checked, and no
	// folder is left behind for a receipt that was never written.
	notes := filepath.Join(dir, "notes")
	if err := os.Mkdir(notes, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notes, "notes.md"), []byte("not a member"), 0600); err != nil {
		t.Fatal(err)
	}
	refused := filepath.Join(dir, "refused", "receipt.json")
	if _, _, err := operation.ImportPlanCommit(ctx, validPlan(), nil, []string{notes}, nil, filepath.Join(dir, "empty"), refused); err != operation.ErrImportNoMembers {
		t.Fatalf("an import of no member was refused as %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(refused)); !os.IsNotExist(err) {
		t.Fatal("a refused import created the receipt's folder")
	}

	// Folders inside a sealed case are never created.
	inside := filepath.Join(dir, "one", "receipts", "receipt.json")
	if _, _, err := operation.ImportPlanCommit(ctx, validPlan(), []string{input}, nil, nil, filepath.Join(dir, "two"), inside); err == nil {
		t.Fatal("a receipt was accepted inside a sealed case")
	}
	for _, path := range []string{filepath.Dir(inside), filepath.Join(dir, "two")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("a receipt refused inside a sealed case left %s", path)
		}
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A mixed container imports as exactly what it holds, and the preview reports
// what the commit then writes: duplicate bytes stay distinct occurrences and
// non-members stay excluded, a malformed frame is quarantined with its bytes
// kept rather than repaired, and duplicate names inside an archive are
// refused before a case or receipt exists. The command line and the window
// both import through these operations, so these are the outcomes of each.
func TestAMixedContainerImportsAsItIsHeld(t *testing.T) {
	fixture := func(control string) string {
		return "MSH|^~\\&|READMIT|SYNTHETIC|RECEIVER|LAB|20260101120000||ADT^A08|IMPORT-" + control + "|P|2.5.1\rPID|1||SYNTH-IMPORT-" + control + "\r"
	}
	ctx := context.Background()
	importAndCompare := func(t *testing.T, plan importer.Plan, folder string) (importer.Receipt, *bundle.Bundle) {
		t.Helper()
		preview, err := operation.ImportPlanPreview(ctx, plan, nil, []string{folder}, nil)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		b, receipt, err := operation.ImportPlanCommit(ctx, plan, nil, []string{folder}, nil, filepath.Join(dir, "case"), filepath.Join(dir, "receipt.json"))
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Totals != preview.Totals {
			t.Fatalf("the commit wrote %+v, the preview reported %+v", receipt.Totals, preview.Totals)
		}
		return receipt, b
	}

	t.Run("duplicate bytes stay distinct and non-members stay excluded", func(t *testing.T) {
		folder := t.TempDir()
		for name, content := range map[string]string{"a.hl7": fixture("AAA"), "a-copy.hl7": fixture("AAA"), "notes.md": "operator notes, not evidence"} {
			if err := os.WriteFile(filepath.Join(folder, name), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		receipt, b := importAndCompare(t, validPlan(), folder)
		if receipt.Totals.Members != 3 || receipt.Totals.Excluded != 1 || receipt.Totals.Sources != 2 || len(b.Events) != 2 {
			t.Fatalf("totals %+v, %d occurrences", receipt.Totals, len(b.Events))
		}
		for _, event := range b.Events {
			if raw, err := b.Raw(event.ID); err != nil || string(raw) != fixture("AAA") {
				t.Fatalf("occurrence %s retained %q: %v", event.ID, raw, err)
			}
		}
	})

	t.Run("a malformed frame is quarantined without repairing its bytes", func(t *testing.T) {
		folder := t.TempDir()
		container := "\x0b" + fixture("ONE") + "\x1c\r" + "\x0bTRUNCATED FRAME"
		if err := os.WriteFile(filepath.Join(folder, "session.mllp"), []byte(container), 0600); err != nil {
			t.Fatal(err)
		}
		plan := validPlan()
		plan.Framing, plan.Members = importer.MLLPFraming, []string{".mllp"}
		receipt, b := importAndCompare(t, plan, folder)
		if len(receipt.Quarantined) == 0 {
			t.Fatalf("nothing was quarantined: %+v", receipt)
		}
		for _, quarantined := range receipt.Quarantined {
			raw, err := b.Raw(quarantined.EventID)
			if err != nil || len(raw) == 0 || !strings.Contains(container, string(raw)) || quarantined.Reason == "" {
				t.Fatalf("quarantined %+v retained %q: %v", quarantined, raw, err)
			}
		}
	})

	t.Run("duplicate names inside an archive are refused before a case exists", func(t *testing.T) {
		dir := t.TempDir()
		archive := filepath.Join(dir, "duplicate-names.zip")
		file, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		writer := zip.NewWriter(file)
		for _, control := range []string{"ONE", "TWO"} {
			member, err := writer.Create("same.hl7")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := member.Write([]byte(fixture(control))); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		file.Close()
		if _, err := operation.ImportPlanPreview(ctx, validPlan(), nil, nil, []string{archive}); err == nil || !strings.Contains(err.Error(), "one relative path of regular file bytes") {
			t.Fatalf("a preview accepted duplicate archive names: %v", err)
		}
		caseDir, receipt := filepath.Join(dir, "refused.case"), filepath.Join(dir, "refused.json")
		if _, _, err := operation.ImportPlanCommit(ctx, validPlan(), nil, nil, []string{archive}, caseDir, receipt); err == nil || !strings.Contains(err.Error(), "one relative path of regular file bytes") {
			t.Fatalf("a commit accepted duplicate archive names: %v", err)
		}
		for _, path := range []string{caseDir, receipt} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("a refused import left %s behind", filepath.Base(path))
			}
		}
	})
}
