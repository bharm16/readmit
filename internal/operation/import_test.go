package operation_test

import (
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
