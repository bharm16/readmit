package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
)

// This file holds what the shell's local documents share. They are three
// separate files — the recent workspace list, the saved filters, and the
// working session — and nothing derives one from another, but they are written
// and checked the same way, so the rule for doing that lives in one place
// rather than once per document. replaceDocument, beneath writeShellDocument,
// is also how a workspace save writes over an entry it may overwrite.

// incompleteSuffix marks the partial file a replacement is written to first.
// One left behind means a previous write was interrupted; it is retained,
// never reused.
const incompleteSuffix = ".incomplete"

// writeShellDocument installs a complete local document, creating the folder
// that holds the shell's state if it is not there yet, through
// replaceDocument.
//
// The diagnostics it returns are never shown: every caller reports its own
// fixed sentence for the document it was writing, and reads only the error
// class to separate a file this account cannot write from any other failure.
func writeShellDocument(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return replaceDocument(path, data)
}

// replaceDocument installs a complete document or leaves the previous one in
// place, so a reader never observes a partial one and a failed write keeps
// exactly what was there before. writeShellDocument and every workspace save
// that may overwrite its entry write through it.
//
// It never opens the entry it replaces. The bytes are written in full to a new
// owner-only file beside it and renamed onto the name, and a rename replaces
// the entry itself, so a symbolic link at the name is replaced rather than
// written through to whatever it points at, and so are a hard link and a FIFO.
// A folder at the name is refused.
//
// Both files it touches are reserved by artifactpath, and the file it renames
// onto is the destination artifactpath itself returned, which is what refuses a
// write inside retained case, run, result, review or report evidence. Neither
// path is derived from the other, so there is one path policy here. The
// incomplete file must not already exist, so an interrupted write is reported
// rather than overwritten, and a link planted at its name is refused rather
// than followed.
//
// A failure to create the incomplete file is returned as the filesystem
// reported it, so a caller can tell a file this account cannot write, or one
// already there, from any other failure. Every other diagnostic is a fixed
// sentence naming no path.
func replaceDocument(path string, data []byte) error {
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return errors.New("cannot write the document here")
	}
	incomplete, err := artifactpath.Destination(path + incompleteSuffix)
	if err != nil {
		return errors.New("cannot write the document here; an interrupted write may be retained beside it")
	}
	file, err := os.OpenFile(incomplete, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	writeErr := artifactdir.WriteFileSync(file, data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(incomplete)
		return errors.New("cannot write the document")
	}
	if err := os.Rename(incomplete, destination); err != nil {
		os.Remove(incomplete)
		return errors.New("cannot replace the document")
	}
	// The rename installs the document, and it survives a power loss only
	// once the directory entry naming it is synced too. Every caller reports a
	// document as retained only when this returns, so it syncs before then.
	if err := syncParent(destination); err != nil {
		return errors.New("cannot confirm the document was retained durably")
	}
	return nil
}

func syncParent(path string) error {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	return artifactdir.SyncDirectory(root, ".")
}

// printable accepts bounded text a person typed or a path they opened. It is
// the rule internal/grid holds a saved filter's text to, for the same reason:
// control characters are refused rather than escaped, so nothing stored here
// can rewrite a terminal or a rendered line when it is displayed back.
func printable(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
