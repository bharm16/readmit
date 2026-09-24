package protect

import (
	"errors"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
)

// readDocument reads one bounded, regular protection document.
func readDocument(path string) (Document, error) {
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return Document{}, errors.New("cannot resolve the protection document")
	}
	data, err := protectionDocument.Read(resolved)
	if err != nil {
		return Document{}, errors.New("the protection document must be a readable regular file within its size limit")
	}
	return Decode(data)
}

// writeDocument replaces the document atomically through the shared document
// store, the way a secret reference document is replaced: written in full to
// a new owner-only file and renamed over the previous one, so a reader never
// observes a partial document and a failed write leaves the previous one
// exactly as it was. The incomplete file must not exist, so an interrupted
// write is reported rather than overwritten. [File] is its only caller, so a
// document changes only through File.
func writeDocument(path string, document Document) error {
	data, err := Encode(document)
	if err != nil {
		return err
	}
	return protectionDocument.Replace(path, data)
}

// protectionDocument is how the protection document is written and read.
var protectionDocument = artifactdir.Document{
	MaxBytes: maxDocumentBytes,
	Errors: artifactdir.DocumentErrors{
		Destination: errors.New("cannot write the protection document here"),
		Create:      errors.New("cannot create the new protection document; an interrupted write is retained"),
		Write:       errors.New("cannot write the new protection document"),
		Install:     errors.New("cannot replace the protection document"),
	},
}
