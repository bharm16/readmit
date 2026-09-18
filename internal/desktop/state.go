package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// This file holds what the shell's local documents share. They are three
// separate files — the recent workspace list, the saved filters, and the
// working session — and nothing derives one from another, but they are written
// and checked the same way, so the rule for doing that lives in one place
// rather than once per document.

// incompleteSuffix marks the partial file a replacement is written to first.
// One left behind means a previous write was interrupted; it is retained,
// never reused.
const incompleteSuffix = ".incomplete"

// writeShellDocument installs a complete local document or leaves the previous
// one in place, so a reader never observes a partial one and a failed write
// keeps exactly what was there before.
//
// Both files it touches are reserved by artifactpath, and the file it renames
// onto is the destination artifactpath itself returned, which is what refuses a
// write inside retained case, run, result, review or report evidence. Neither
// path is derived from the other, so there is one path policy here. The
// incomplete file must not already exist, so an interrupted write is reported
// rather than overwritten.
//
// The diagnostics it returns are never shown: every caller reports its own
// fixed sentence for the document it was writing, and reads only the error
// class to separate a file this account cannot write from any other failure.
func writeShellDocument(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return errors.New("cannot write local shell state here")
	}
	incomplete, err := artifactpath.Destination(path + incompleteSuffix)
	if err != nil {
		return errors.New("cannot write local shell state here; an interrupted write may be retained beside it")
	}
	file, err := os.OpenFile(incomplete, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(incomplete)
		return errors.New("cannot write local shell state")
	}
	if err := os.Rename(incomplete, destination); err != nil {
		os.Remove(incomplete)
		return errors.New("cannot replace local shell state")
	}
	return nil
}

// printable accepts bounded text a person typed or a path they opened. It is
// the rule internal/grid holds a saved filter's text to, for the same reason:
// control characters are refused rather than escaped, so nothing stored here
// can rewrite a terminal or a rendered line when it is displayed back.
func printable(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
