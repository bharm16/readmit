// Package operation contains application operations shared by the command-line
// and desktop adapters. Presentation and state mapping stay in those adapters;
// verification, extraction, and admission rules live here once.
package operation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/engineexport"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/importer"
)

var (
	ErrImportCaseExists    = errors.New("the case bundle destination must be a new directory")
	ErrImportReceiptExists = errors.New("the import receipt destination must be a new file")
	ErrImportNoMembers     = errors.New("the declared containers hold no member to import; preview reports why each entry was excluded")
	ErrPastedContentLimit  = errors.New("pasted content exceeds maximum source size limit")
	ErrPastedNameInvalid   = errors.New("pasted content must be named by a valid entry name")
)

// The two ways an import's receipt can fail once its case exists. Both say the
// case was written, because by then it was.
const (
	importReceiptUncreated = "the case was written but its receipt was not; the receipt destination must be new and writable"
	importReceiptUnwritten = "the case was written but its receipt was not; retry the import with new destinations"
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
	return commitUnderPlan(ctx, plan, files, folders, archives, outputDir, receiptPath, nil)
}

// ImportCollectionCommit imports the folder one source collection staged,
// under the plan its receipt records, into a new case and import receipt. The
// receipt is read strictly and must name its own evidence; a collection that
// did not complete is refused before anything is read from the folder, because
// what it staged is not the whole of its scope; and the folder must hold
// exactly the entries the collection collected, with the bytes it recorded, or
// nothing is written.
func ImportCollectionCommit(ctx context.Context, collectionPath, folder, outputDir, receiptPath string) (*bundle.Bundle, importer.Receipt, error) {
	data, err := ReadInputFile(collectionPath, evidencesource.MaxCollectionBytes)
	if err != nil {
		return nil, importer.Receipt{}, err
	}
	collection, err := evidencesource.DecodeCollection(data)
	if err != nil {
		return nil, importer.Receipt{}, err
	}
	if err := collection.Importable(); err != nil {
		return nil, importer.Receipt{}, err
	}
	return commitUnderPlan(ctx, collection.Plan, nil, []string{folder}, nil, outputDir, receiptPath, func(extraction *importer.Extraction) error {
		return collection.VerifyStaged(extraction.Containers[0])
	})
}

// commitUnderPlan is one plan-driven import once its declaration is valid.
// Admit, when set, is shown the extraction before anything is written, and a
// refusal from it writes nothing.
func commitUnderPlan(ctx context.Context, plan importer.Plan, files, folders, archives []string, outputDir, receiptPath string, admit func(*importer.Extraction) error) (*bundle.Bundle, importer.Receipt, error) {
	if err := checkNewDestination(outputDir, receiptPath); err != nil {
		return nil, importer.Receipt{}, err
	}
	importedAt := time.Now().UTC()
	extraction, err := importer.Extract(ctx, plan, files, folders, archives)
	if err != nil {
		return nil, importer.Receipt{}, err
	}
	if admit != nil {
		if err := admit(extraction); err != nil {
			return nil, importer.Receipt{}, err
		}
	}
	if len(extraction.Inputs) == 0 {
		return nil, importer.Receipt{}, ErrImportNoMembers
	}
	b, err := bundle.WriteContext(ctx, outputDir, extraction.Inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
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
	if err := writeNewDocument(receiptPath, encoded, importReceiptUncreated, importReceiptUnwritten); err != nil {
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
	b, err := bundle.WriteContext(ctx, outputDir, extraction.Inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
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
	if err := writeNewDocument(receiptPath, encoded, importReceiptUncreated, importReceiptUnwritten); err != nil {
		return nil, importer.MappingReceipt{}, err
	}
	return b, receipt, nil
}

// ImportEnginePreview previews an engine export without writing evidence. The
// export is read through ReadInputFile, so no diagnostic repeats its path.
func ImportEnginePreview(ctx context.Context, plan engineexport.Plan, inputPath string) (EngineExportPreview, error) {
	if err := plan.Validate(); err != nil {
		return EngineExportPreview{}, err
	}
	data, err := ReadInputFile(inputPath, engineexport.MaxBytes)
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

// ImportEngineCommit writes an engine export into a new case bundle. The case
// destination is checked before the export is read, and no diagnostic repeats
// the export's path.
func ImportEngineCommit(ctx context.Context, plan engineexport.Plan, inputPath, outputDir string) (*bundle.Bundle, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if err := checkNewCase(outputDir); err != nil {
		return nil, err
	}
	data, err := ReadInputFile(inputPath, engineexport.MaxBytes)
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
	// The staging folder is refused inside retained evidence before it is
	// created, so a refused paste leaves a sealed case exactly as it was.
	if _, err := artifactpath.Destination(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	reserved, err := artifactpath.Destination(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	if err := pastedSource.Create(reserved, data); err != nil {
		return "", err
	}
	return reserved, nil
}

// pastedSource is how pasted bytes are staged as a new declared source,
// through the shared document store. Staging has always reported a failed
// creation or write as the filesystem worded it.
var pastedSource = artifactdir.Document{
	Errors: artifactdir.DocumentErrors{Create: artifactdir.FilesystemReport, Write: artifactdir.FilesystemReport},
}

// checkNewDestination refuses an import before anything is extracted unless
// both of its destinations are new: the case directory, and the receipt,
// whose missing folders the receipt writer will create.
func checkNewDestination(outputDir, receiptPath string) error {
	if err := checkNewCase(outputDir); err != nil {
		return err
	}
	return checkNewDocument(receiptPath, ErrImportReceiptExists)
}

// checkNewCase refuses a case destination that is taken or that the shared
// output reservation refuses.
func checkNewCase(outputDir string) error {
	reserved, err := artifactpath.Destination(outputDir)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(reserved); !os.IsNotExist(err) {
		return ErrImportCaseExists
	}
	return nil
}
