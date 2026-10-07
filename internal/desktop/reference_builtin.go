package desktop

import (
	"path/filepath"

	"github.com/bharm16/readmit/internal/hl7reference"
)

// NewWithBundledReferences is the production shell constructor. Standard HL7
// definitions are app resources, ready before a person opens their first message.
// Materializing them writes no manual installation or version-selection document.
// Explicit custom catalogs remain separate and retain their existing pins.
func NewWithBundledReferences(chooser FolderChooser, documents ShellDocuments, license string, archive []byte) *App {
	app := NewWithInstalledLicense(chooser, documents, license)
	bundle, err := hl7reference.OpenBundle(archive)
	if err != nil {
		app.bundledReferenceError = err
		return app
	}
	folder := filepath.Join(documents.Folder, "reference-catalogs")
	if err := bundle.Retain(folder); err != nil {
		app.bundledReferenceError = err
		return app
	}
	for _, entry := range bundle.Entries() {
		app.bundledReferences = append(app.bundledReferences, ReferenceEdition{
			Edition:  entry.Edition,
			Path:     filepath.Join(folder, entry.SHA256+".json"),
			Identity: "sha256:" + entry.SHA256,
		})
	}
	return app
}
