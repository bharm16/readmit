package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/spf13/cobra"
)

// declaration is the import plan stated on the command line. Every member of a
// plan is required, so each flag below starts empty and a missing one is an
// error: the wizard asks for the declaration rather than choosing a default.
type declaration struct {
	framing    string
	boundary   string
	terminator string
	encoding   string
	direction  string
	members    []string
}

// declarationFlags name every flag that states part of an import plan. One
// list, because two copies of it would drift the first time a declaration is
// added and quietly stop excluding what it excludes today.
var declarationFlags = []string{"framing", "batch-boundary", "terminator", "encoding", "direction", "member"}

func declarationStated(cmd *cobra.Command) bool {
	for _, name := range declarationFlags {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

// plan builds the declared plan, either by reading a saved one or from the
// declaration flags. The two are exclusive: a saved plan is the reusable form
// of the same declarations, so combining them would leave it unclear which
// declaration an import actually ran under.
func (d declaration) plan(cmd *cobra.Command, saved string) (importer.Plan, error) {
	stated := declarationStated(cmd)
	switch {
	case saved != "" && stated:
		return importer.Plan{}, usage("%s reads a saved plan or the declaration flags, never both", cmd.Name())
	case saved == "" && !stated:
		return importer.Plan{}, usage("%s requires --plan, or --framing, --terminator, --encoding and --direction declared on the command line", cmd.Name())
	case saved != "":
		data, err := readInputFile(saved, importer.MaxPlanBytes)
		if err != nil {
			return importer.Plan{}, err
		}
		return importer.DecodePlan(data)
	}
	members := d.members
	if members == nil {
		members = []string{}
	}
	declared := importer.Plan{
		Schema:        importer.PlanSchema,
		Framing:       importer.Framing(d.framing),
		BatchBoundary: importer.Boundary(d.boundary),
		Terminator:    hl7.Terminator(d.terminator),
		Encoding:      importer.Encoding(d.encoding),
		Direction:     bundle.Direction(d.direction),
		Members:       members,
	}
	return declared, declared.Validate()
}

// readRecipe reads the saved mapping recipe, which is exclusive with every plan
// declaration: a recipe states how an envelope divides into records and where
// each record's payload, observed time, source, direction and channel are, so
// combining it with a framing declaration would leave it unclear which one an
// import actually ran under.
func readRecipe(cmd *cobra.Command, path, saved string) (importer.Recipe, error) {
	if saved != "" || declarationStated(cmd) {
		return importer.Recipe{}, usage("import reads a mapping recipe or an import plan, never both")
	}
	data, err := readInputFile(path, importer.MaxRecipeBytes)
	if err != nil {
		return importer.Recipe{}, err
	}
	return importer.DecodeRecipe(data)
}

func importCommand() *cobra.Command {
	var saved, mapping, output, receipt string
	var files, folders, archives []string
	var preview bool
	var declared declaration
	cmd := &cobra.Command{
		Use:         "import --file FILE --framing raw --terminator cr --encoding utf-8 --direction inbound --output NEW_DIRECTORY --receipt NEW_FILE",
		Annotations: declareInterruptible(capabilityAuthor),
		Short:       "Import declared files, folders, and archives into a new case bundle",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if preview && (output != "" || receipt != "") {
				return usage("import previews what would be extracted or writes a case, never both")
			}
			if !preview && (output == "" || receipt == "") {
				return usage("import requires --preview, or --output with a new directory and --receipt with a new file")
			}
			var plan importer.Plan
			var recipe importer.Recipe
			var err error
			if mapping != "" {
				recipe, err = readRecipe(cmd, mapping, saved)
			} else {
				plan, err = declared.plan(cmd, saved)
			}
			if err != nil {
				return err
			}
			// The receipt destination is checked before any evidence is
			// written, so an unusable receipt location cannot leave a case
			// behind that nothing describes. Exclusive creation still owns the
			// guarantee; this only refuses the mistake early.
			if !preview {
				reserved, err := artifactpath.Destination(receipt)
				if err != nil {
					return err
				}
				if _, err := os.Lstat(reserved); !os.IsNotExist(err) {
					return errors.New("the import receipt destination must be a new file")
				}
			}
			ctx := cmd.Context()
			importedAt := time.Now().UTC()
			var extraction *importer.Extraction
			if mapping != "" {
				extraction, err = importer.ExtractMapped(ctx, recipe, files, folders, archives)
			} else {
				extraction, err = importer.Extract(ctx, plan, files, folders, archives)
			}
			if err != nil {
				return err
			}
			if preview {
				encoded, err := previewDocument(plan, recipe, mapping != "", extraction)
				if err != nil {
					return err
				}
				if _, err := cmd.OutOrStdout().Write(encoded); err != nil {
					return errors.New("cannot write the import preview")
				}
				return nil
			}
			if len(extraction.Inputs) == 0 {
				return errors.New("the declared containers hold no member to import; --preview reports why each entry was excluded")
			}
			b, err := bundle.Write(output, extraction.Inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
			if err != nil {
				return err
			}
			if mapping != "" {
				document, err := importer.NewMappingReceipt(recipe, extraction, importedAt, b)
				if err != nil {
					return err
				}
				encoded, err := importer.EncodeMappingReceipt(document)
				if err != nil {
					return err
				}
				if err := writeImportReceipt(receipt, encoded); err != nil {
					return err
				}
				return renderMapping(cmd.OutOrStdout(), document, b)
			}
			document, err := importer.NewReceipt(plan, extraction, importedAt, b)
			if err != nil {
				return err
			}
			encoded, err := importer.EncodeReceipt(document)
			if err != nil {
				return err
			}
			if err := writeImportReceipt(receipt, encoded); err != nil {
				return err
			}
			return renderImport(cmd.OutOrStdout(), document, b)
		},
	}
	cmd.AddCommand(engineImportCommand())
	cmd.Flags().StringVar(&mapping, "recipe", "", "Existing readmit-mapping-recipe/v1 JSON file mapping a CSV, JSON, XML, or text envelope")
	cmd.Flags().StringVar(&saved, "plan", "", "Existing readmit-import-plan/v1 JSON file holding the declarations below")
	addDeclarationFlags(cmd, &declared, true)
	cmd.Flags().StringArrayVar(&files, "file", nil, "One declared message file; repeatable")
	cmd.Flags().StringArrayVar(&folders, "folder", nil, "One declared folder whose regular files are members; repeatable")
	cmd.Flags().StringArrayVar(&archives, "archive", nil, "One declared ZIP archive whose entries are members; repeatable")
	cmd.Flags().BoolVar(&preview, "preview", false, "Write the preview document for the declaration in use to stdout and create nothing")
	cmd.Flags().StringVar(&output, "output", "", "New case bundle directory (never overwrite)")
	cmd.Flags().StringVar(&receipt, "receipt", "", "New receipt JSON file for the declaration in use (never overwrite)")
	return cmd
}

// previewDocument encodes whichever preview the declarations produced.
func previewDocument(plan importer.Plan, recipe importer.Recipe, mapped bool, e *importer.Extraction) ([]byte, error) {
	if !mapped {
		return importer.EncodePreview(importer.NewPreview(plan, e))
	}
	document, err := importer.NewMappingPreview(recipe, e)
	if err != nil {
		return nil, err
	}
	return importer.EncodeMappingPreview(document)
}

// writeImportReceipt writes the receipt beside the case that was just written.
// Both diagnostics say the case exists, because by this point it does.
func writeImportReceipt(path string, encoded []byte) error {
	return writeNewFile(path, encoded,
		"the case was written but its receipt was not; the receipt destination must be new and writable",
		"the case was written but its receipt was not; retry the import with new destinations")
}

// renderImport reports the declarations the import ran under and what it
// reconciled. It names no container path and no member name: those are recorded
// in the receipt, which is a file the person already chose to keep. It displays
// no occurrence bytes at all; `timeline --show-values` is where evidence is
// read, so this command has no way to print a value.
func renderImport(out io.Writer, receipt importer.Receipt, b *bundle.Bundle) error {
	w := bufio.NewWriter(out)
	boundary := receipt.Plan.BatchBoundary
	if boundary == "" {
		boundary = "not declared"
	}
	fmt.Fprintf(w, "Import plan: %s\nFraming: %s\nBatch boundary: %s\nTerminator: %s\nEncoding: %s\nDirection: %s\n",
		receipt.Plan.Schema, receipt.Plan.Framing, boundary, receipt.Plan.Terminator, receipt.Plan.Encoding, receipt.Plan.Direction)
	fmt.Fprintf(w, "Receipt: %s\nContainers: %d\nMembers: %d\nExcluded members: %d\nExtracted sources: %d\nQuarantined occurrences: %d\n",
		receipt.Schema, receipt.Totals.Containers, receipt.Totals.Members, receipt.Totals.Excluded, receipt.Totals.Sources, len(receipt.Quarantined))
	if err := w.Flush(); err != nil {
		return errors.New("cannot write import output")
	}
	return renderBundle(out, b, false, false)
}

// renderMapping reports the recipe a mapped import ran under and what it
// reconciled. Like renderImport it names no container path, no member name and
// no located value: those are in the receipt, which is a file the person
// already chose to keep. It reports how many records the recipe could not map,
// because that count is the one a person has to act on.
func renderMapping(out io.Writer, receipt importer.MappingReceipt, b *bundle.Bundle) error {
	w := bufio.NewWriter(out)
	recipe := receipt.Recipe
	fmt.Fprintf(w, "Mapping recipe: %s\nName: %s\nRevision: %d\nRecipe identity: %s\nEnvelope: %s\nEncoding: %s\n",
		recipe.Schema, recipe.Name, recipe.Revision, receipt.Identity, recipe.Envelope, recipe.Encoding)
	fmt.Fprintf(w, "Payload: %s\nObserved time: %s\nSource: %s\nDirection: %s\nChannel: %s\n",
		recipe.Payload.Operator, recipe.ObservedAt.Operator, recipe.Source.Operator,
		recipe.Direction.Operator, recipe.Channel.Operator)
	fmt.Fprintf(w, "Receipt: %s\nContainers: %d\nMembers: %d\nExcluded members: %d\nExtracted sources: %d\nUnmapped records: %d\nQuarantined occurrences: %d\n",
		receipt.Schema, receipt.Totals.Containers, receipt.Totals.Members, receipt.Totals.Excluded,
		receipt.Totals.Sources, receipt.UnmappedRecords, len(receipt.Quarantined))
	if err := w.Flush(); err != nil {
		return errors.New("cannot write import output")
	}
	return renderBundle(out, b, false, false)
}
