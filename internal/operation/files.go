package operation

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// ReadInputFile reads one regular file whole within a limit, with the fixed
// diagnostics every command reports: none of them repeats the path or a byte
// of what was read. The command line and the desktop facade read an input the
// same way through it, so a file one entry point refuses the other refuses for
// the same reason.
func ReadInputFile(path string, limit int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fileError{"input must be a readable regular file", err}
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("input must be a readable regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fileError{"cannot open input file", err}
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, errors.New("cannot read input file")
	}
	if len(data) > limit {
		return nil, errors.New("input exceeds size limit")
	}
	return data, nil
}

// WriteNewFile creates one new file exclusively at a reserved destination and
// removes a partial one rather than leaving it behind. The caller supplies the
// two diagnostics because what a failed creation means differs by command:
// artifactpath owns where the file may go, and this owns how it is written.
func WriteNewFile(path string, data []byte, cannotCreate, cannotWrite string) error {
	path, err := artifactpath.Destination(path)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fileError{cannotCreate, err}
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(path)
		return errors.New(cannotWrite)
	}
	return nil
}

// checkNewDocument refuses a receipt or report destination that is taken, or
// whose first missing folder the shared output reservation would not let
// writeNewDocument create. It creates nothing, so a run refused after it
// leaves no folder behind.
func checkNewDocument(path string, taken error) error {
	if missing := firstMissingFolder(path); missing != "" {
		_, err := artifactpath.Destination(missing)
		return err
	}
	reserved, err := artifactpath.Destination(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(reserved); !os.IsNotExist(err) {
		return taken
	}
	return nil
}

// writeNewDocument creates a receipt or report document's missing folders,
// where the shared output reservation allows the first of them, and then the
// document itself through WriteNewFile. The caller names what a failed
// creation and a failed write mean, because by then the work the document
// describes has been done.
func writeNewDocument(path string, data []byte, cannotCreate, cannotWrite string) error {
	if missing := firstMissingFolder(path); missing != "" {
		if _, err := artifactpath.Destination(missing); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return fileError{cannotCreate, err}
		}
	}
	return WriteNewFile(path, data, cannotCreate, cannotWrite)
}

// firstMissingFolder returns the outermost folder above path that does not
// exist yet, or "" when its parent folder is already there.
func firstMissingFolder(path string) string {
	missing := ""
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(dir); err == nil || filepath.Dir(dir) == dir {
			return missing
		}
		missing = dir
	}
}

// fileError is a fixed diagnostic that keeps the filesystem's own error
// behind it. The sentence is all a person or a terminal is shown, so it never
// repeats a path; the cause lets a caller tell a file this account may not
// open or create, such as fs.ErrPermission, from any other failure.
type fileError struct {
	sentence string
	cause    error
}

func (e fileError) Error() string { return e.sentence }

func (e fileError) Unwrap() error { return e.cause }
