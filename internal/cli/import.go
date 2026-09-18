package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
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

// plan builds the declared plan, either by reading a saved one or from the
// declaration flags. The two are exclusive: a saved plan is the reusable form
// of the same declarations, so combining them would leave it unclear which
// declaration an import actually ran under.
func (d declaration) plan(cmd *cobra.Command, saved string) (importer.Plan, error) {
	stated := false
	for _, name := range []string{"framing", "batch-boundary", "terminator", "encoding", "direction", "member"} {
		stated = stated || cmd.Flags().Changed(name)
	}
	switch {
	case saved != "" && stated:
		return importer.Plan{}, errors.New("import reads a saved plan or the declaration flags, never both")
	case saved == "" && !stated:
		return importer.Plan{}, errors.New("import requires --plan, or --framing, --terminator, --encoding and --direction declared on the command line")
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

func importCommand(ran *bool) *cobra.Command {
	var saved, output, receipt string
	var files, folders, archives []string
	var preview bool
	var declared declaration
	cmd := &cobra.Command{
		Use:   "import --file FILE --framing raw --terminator cr --encoding utf-8 --direction inbound --output NEW_DIRECTORY --receipt NEW_FILE",
		Short: "Import declared files, folders, and archives into a new case bundle",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if preview && (output != "" || receipt != "") {
				return errors.New("import previews what would be extracted or writes a case, never both")
			}
			if !preview && (output == "" || receipt == "") {
				return errors.New("import requires --preview, or --output with a new directory and --receipt with a new file")
			}
			plan, err := declared.plan(cmd, saved)
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
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			importedAt := time.Now().UTC()
			extraction, err := importer.Extract(ctx, plan, files, folders, archives)
			if err != nil {
				return err
			}
			if preview {
				encoded, err := importer.EncodePreview(importer.NewPreview(plan, extraction))
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
			document, err := importer.NewReceipt(plan, extraction, importedAt, b)
			if err != nil {
				return err
			}
			encoded, err := importer.EncodeReceipt(document)
			if err != nil {
				return err
			}
			if err := writeNewFile(receipt, encoded,
				"the case was written but its receipt was not; the receipt destination must be new and writable",
				"the case was written but its receipt was not; retry the import with new destinations"); err != nil {
				return err
			}
			return renderImport(cmd.OutOrStdout(), document, b)
		},
	}
	cmd.Flags().StringVar(&saved, "plan", "", "Existing readmit-import-plan/v1 JSON file holding the declarations below")
	cmd.Flags().StringVar(&declared.framing, "framing", "", "Declared message framing: raw, mllp, or batch")
	cmd.Flags().StringVar(&declared.boundary, "batch-boundary", "", "Declared batch boundary, with --framing batch: segment-start or hl7-batch")
	cmd.Flags().StringVar(&declared.terminator, "terminator", "", "Declared segment terminator: cr, lf, or crlf")
	cmd.Flags().StringVar(&declared.encoding, "encoding", "", "Declared source encoding: utf-8, us-ascii, iso-8859-1, or unknown")
	cmd.Flags().StringVar(&declared.direction, "direction", "", "Declared direction of every imported occurrence: unknown, inbound, or outbound")
	cmd.Flags().StringArrayVar(&declared.members, "member", nil, "One declared lowercase file-name suffix a folder or archive entry must end with; repeatable")
	cmd.Flags().StringArrayVar(&files, "file", nil, "One declared message file; repeatable")
	cmd.Flags().StringArrayVar(&folders, "folder", nil, "One declared folder whose regular files are members; repeatable")
	cmd.Flags().StringArrayVar(&archives, "archive", nil, "One declared ZIP archive whose entries are members; repeatable")
	cmd.Flags().BoolVar(&preview, "preview", false, "Write the readmit-import-preview/v1 document to stdout and create nothing")
	cmd.Flags().StringVar(&output, "output", "", "New case bundle directory (never overwrite)")
	cmd.Flags().StringVar(&receipt, "receipt", "", "New readmit-import-receipt/v1 JSON file (never overwrite)")
	return cmd
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
