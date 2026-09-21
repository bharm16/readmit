package protect

import (
	"errors"
	"os"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
)

// incompleteSuffix marks the partial file a replacement document is written to
// before it is renamed into place.
const incompleteSuffix = ".incomplete"

// ReadDocument reads one bounded, regular protection document.
func ReadDocument(path string) (Document, error) {
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return Document{}, errors.New("cannot resolve the protection document")
	}
	data, err := readLocal(resolved, maxDocumentBytes)
	if err != nil {
		return Document{}, errors.New("the protection document must be a readable regular file within its size limit")
	}
	return Decode(data)
}

// WriteDocument replaces the document atomically, the way a secret reference
// document is replaced: written in full to a new owner-only file and renamed
// over the previous one, so a reader never observes a partial document and a
// failed write leaves the previous one exactly as it was. The incomplete file
// must not exist, so an interrupted write is reported rather than overwritten.
func WriteDocument(path string, document Document) error {
	data, err := Encode(document)
	if err != nil {
		return err
	}
	// Both files this writes are reserved by artifactpath, and the file it
	// renames onto is the destination artifactpath itself returned. Neither
	// path is derived from the other, so there is one path policy here.
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return errors.New("cannot write the protection document here")
	}
	incomplete, err := artifactpath.Destination(path + incompleteSuffix)
	if err != nil {
		return errors.New("cannot write the protection document here; an interrupted write may be retained beside it")
	}
	file, err := os.OpenFile(incomplete, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create the new protection document; an interrupted write is retained")
	}
	writeErr := artifactdir.WriteFileSync(file, data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(incomplete)
		return errors.New("cannot write the new protection document")
	}
	if err := os.Rename(incomplete, destination); err != nil {
		os.Remove(incomplete)
		return errors.New("cannot replace the protection document")
	}
	return nil
}
