package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactdir"
)

// This file holds what the shell's seven local documents share: recent
// workspaces, saved filters, working session, editor drafts, and operation,
// commercial and hub selections. They are separate owner-only files written
// through one replacement rule. replaceDocument, beneath writeShellDocument,
// is also how a workspace save writes over an entry it may overwrite.

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
// place, through the shared document store, so a reader never observes a
// partial one and a failed write keeps exactly what was there before.
// writeShellDocument and every workspace save that may overwrite its entry
// write through it.
//
// It never opens the entry it replaces. The bytes are written in full to a new
// owner-only file beside it and renamed onto the name, and a rename replaces
// the entry itself, so a symbolic link at the name is replaced rather than
// written through to whatever it points at, and so are a hard link and a FIFO.
// A folder at the name is refused. The store refuses a write inside retained
// case, run, result, review or report evidence. The incomplete file must not
// already exist, so an interrupted write is reported rather than overwritten,
// and a link planted at its name is refused rather than followed. The
// document survives a power loss only once the folder naming it is synced
// too; every caller reports a document as retained only when this returns, so
// the store syncs it before then.
//
// A failure to create the incomplete file is returned as the filesystem
// reported it, so a caller can tell a file this account cannot write, or one
// already there, from any other failure. Every other diagnostic is a fixed
// sentence naming no path.
func replaceDocument(path string, data []byte) error {
	return shellDocument.Replace(path, data)
}

// shellDocument is how the shell's local documents, and a workspace entry a
// save may overwrite, are replaced.
var shellDocument = artifactdir.Document{
	Errors: artifactdir.DocumentErrors{
		Destination: errors.New("cannot write the document here"),
		Create:      artifactdir.FilesystemReport,
		Write:       errors.New("cannot write the document"),
		Install:     errors.New("cannot replace the document"),
		Sync:        errors.New("cannot confirm the document was retained durably"),
	},
}

// printable accepts bounded text a person typed or a path they opened. It is
// the rule internal/grid holds a saved filter's text to, for the same reason:
// control characters are refused rather than escaped, so nothing stored here
// can rewrite a terminal or a rendered line when it is displayed back.
func printable(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
