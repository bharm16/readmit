// Package operation contains application operations shared by the command-line
// and desktop adapters. Presentation and state mapping stay in those adapters;
// verification, extraction, and admission rules live here once.
package operation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/engineexport"
	"github.com/bharm16/readmit/internal/importer"
)

var (
	ErrImportCaseExists    = errors.New("the case bundle destination must be a new directory")
	ErrImportReceiptExists = errors.New("the import receipt destination must be a new file")
	ErrImportNoMembers     = errors.New("the declared containers hold no member to import; preview reports why each entry was excluded")
	ErrReceiptWrite        = errors.New("the case was written but its receipt was not; retry the import with new destinations")
	ErrPastedContentLimit  = errors.New("pasted content exceeds maximum source size limit")
	ErrPastedNameInvalid   = errors.New("pasted content must be named by a valid entry name")
)

// EngineExportPreview describes an engine export without writing evidence.
type EngineExportPreview struct {
	Schema        string                `json:"schema"`
	Plan          engineexport.Plan     `json:"plan"`
	Qualification string                `json:"qualification"`
	Records       []engineexport.Record `json:"records"`
}

// ImportPlanPreview extracts sources under an import plan without writing evidence.
func ImportPlanPreview(ctx context.Context, plan importer.Plan, files, folders, archives []string) (importer.Preview, error) {
	if err := plan.Validate(); err != nil {
		return importer.Preview{}, err
	}
	extraction, err := importer.Extract(ctx, plan, files, folders, archives)
	if err != nil {
		return importer.Preview{}, err
	}
	return importer.NewPreview(plan, extraction), nil
}

// ImportPlanCommit extracts sources under an import plan and writes the case bundle
// and receipt atomically to new destinations.
func ImportPlanCommit(ctx context.Context, plan importer.Plan, files, folders, archives []string, outputDir, receiptPath string) (*bundle.Bundle, importer.Receipt, error) {
	if err := plan.Validate(); err != nil {
		return nil, importer.Receipt{}, err
	}
	if err := checkNewDestination(outputDir, receiptPath); err != nil {
		return nil, importer.Receipt{}, err
	}
	importedAt := time.Now().UTC()
	extraction, err := importer.Extract(ctx, plan, files, folders, archives)
	if err != nil {
		return nil, importer.Receipt{}, err
	}
	if len(extraction.Inputs) == 0 {
		return nil, importer.Receipt{}, ErrImportNoMembers
	}
	b, err := bundle.Write(outputDir, extraction.Inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
	if err != nil {
		return nil, importer.Receipt{}, err
	}
	receipt, err := importer.NewReceipt(plan, extraction, importedAt, b)
	if err != nil {
		return nil, importer.Receipt{}, err
	}
	encoded, err := importer.EncodeReceipt(receipt)
	if err != nil {
		return nil, importer.Receipt{}, err
	}
	if err := writeNewReceipt(receiptPath, encoded); err != nil {
		return nil, importer.Receipt{}, err
	}
	return b, receipt, nil
}

// ImportRecipePreview extracts sources under a mapping recipe without writing evidence.
func ImportRecipePreview(ctx context.Context, recipe importer.Recipe, files, folders, archives []string) (importer.MappingPreview, error) {
	if err := recipe.Validate(); err != nil {
		return importer.MappingPreview{}, err
	}
	extraction, err := importer.ExtractMapped(ctx, recipe, files, folders, archives)
	if err != nil {
		return importer.MappingPreview{}, err
	}
	preview, err := importer.NewMappingPreview(recipe, extraction)
	if err != nil {
		return importer.MappingPreview{}, err
	}
	return preview, nil
}

// ImportRecipeCommit extracts sources under a mapping recipe and writes the case bundle
// and receipt atomically to new destinations.
func ImportRecipeCommit(ctx context.Context, recipe importer.Recipe, files, folders, archives []string, outputDir, receiptPath string) (*bundle.Bundle, importer.MappingReceipt, error) {
	if err := recipe.Validate(); err != nil {
		return nil, importer.MappingReceipt{}, err
	}
	if err := checkNewDestination(outputDir, receiptPath); err != nil {
		return nil, importer.MappingReceipt{}, err
	}
	importedAt := time.Now().UTC()
	extraction, err := importer.ExtractMapped(ctx, recipe, files, folders, archives)
	if err != nil {
		return nil, importer.MappingReceipt{}, err
	}
	if len(extraction.Inputs) == 0 {
		return nil, importer.MappingReceipt{}, ErrImportNoMembers
	}
	b, err := bundle.Write(outputDir, extraction.Inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
	if err != nil {
		return nil, importer.MappingReceipt{}, err
	}
	receipt, err := importer.NewMappingReceipt(recipe, extraction, importedAt, b)
	if err != nil {
		return nil, importer.MappingReceipt{}, err
	}
	encoded, err := importer.EncodeMappingReceipt(receipt)
	if err != nil {
		return nil, importer.MappingReceipt{}, err
	}
	if err := writeNewReceipt(receiptPath, encoded); err != nil {
		return nil, importer.MappingReceipt{}, err
	}
	return b, receipt, nil
}

// ImportEnginePreview previews an engine export without writing evidence.
func ImportEnginePreview(ctx context.Context, plan engineexport.Plan, inputPath string) (EngineExportPreview, error) {
	if err := plan.Validate(); err != nil {
		return EngineExportPreview{}, err
	}
	data, err := readImportFile(inputPath, engineexport.MaxBytes)
	if err != nil {
		return EngineExportPreview{}, err
	}
	records, err := engineexport.Extract(plan, data)
	if err != nil {
		return EngineExportPreview{}, err
	}
	return EngineExportPreview{
		Schema:        "readmit-engine-export-preview/v1",
		Plan:          plan,
		Qualification: "unqualified",
		Records:       records,
	}, nil
}

// ImportEngineCommit writes an engine export into a new case bundle.
func ImportEngineCommit(ctx context.Context, plan engineexport.Plan, inputPath, outputDir string) (*bundle.Bundle, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	reservedCase, err := artifactpath.Destination(outputDir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(reservedCase); !os.IsNotExist(err) {
		return nil, ErrImportCaseExists
	}
	data, err := readImportFile(inputPath, engineexport.MaxBytes)
	if err != nil {
		return nil, err
	}
	return bundle.WriteEngineExport(ctx, outputDir, inputPath, data, plan, time.Now().UTC())
}

// StagePastedContent writes pasted bytes to a new declared source file.
// The file is placed in dir under name, with exclusive creation and 0600 permissions,
// preserving actual bytes and never pretending to be a captured original file.
func StagePastedContent(dir, name string, data []byte) (string, error) {
	if len(data) > bundle.MaxSourceBytes {
		return "", ErrPastedContentLimit
	}
	if err := artifactpath.EntryName(name); err != nil {
		return "", ErrPastedNameInvalid
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	destPath := filepath.Join(dir, name)
	reserved, err := artifactpath.Destination(destPath)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(reserved, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if err := artifactdir.WriteFileSync(file, data); err != nil {
		os.Remove(reserved)
		return "", err
	}
	return reserved, nil
}

func checkNewDestination(outputDir, receiptPath string) error {
	reservedCase, err := artifactpath.Destination(outputDir)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(reservedCase); !os.IsNotExist(err) {
		return ErrImportCaseExists
	}
	reservedReceipt, err := artifactpath.Destination(receiptPath)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(reservedReceipt); !os.IsNotExist(err) {
		return ErrImportReceiptExists
	}
	return nil
}

func writeNewReceipt(path string, encoded []byte) error {
	reserved, err := artifactpath.Destination(path)
	if err != nil {
		return errors.New("cannot resolve receipt destination")
	}
	if err := os.MkdirAll(filepath.Dir(reserved), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(reserved, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrReceiptWrite
	}
	defer file.Close()
	if err := artifactdir.WriteFileSync(file, encoded); err != nil {
		os.Remove(reserved)
		return ErrReceiptWrite
	}
	return nil
}

func readImportFile(path string, limit int) ([]byte, error) {
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q is not a regular file", path)
	}
	if info.Size() > int64(limit) {
		return nil, fmt.Errorf("%q exceeds size limit of %d bytes", path, limit)
	}
	return os.ReadFile(resolved)
}
