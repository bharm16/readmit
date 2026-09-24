package cli

import (
	"errors"
	"os"

	"github.com/bharm16/readmit/internal/operation"
)

// readInputFile keeps input limits and private diagnostics shared by commands.
// It is the shared operation's reader, so the desktop facade refuses an input
// for the same reasons.
func readInputFile(path string, limit int) ([]byte, error) {
	return operation.ReadInputFile(path, limit)
}

// writeNewFile creates one new file exclusively at a reserved destination
// through the shared operation's writer, so a file the desktop facade writes
// is created and refused exactly as the command line's is.
func writeNewFile(path string, data []byte, cannotCreate, cannotWrite string) error {
	return operation.WriteNewFile(path, data, cannotCreate, cannotWrite)
}

// outputFile is one member of a report directory a command creates whole.
type outputFile struct {
	name string
	data []byte
}

// writeNewReportDirectory creates one new directory and the files a report is
// made of. The directory must not exist, every file is created exclusively
// inside it, and a partial directory is retained rather than removed so an
// interrupted report is visible instead of looking like one that was never
// started. The subject names the report in the diagnostics, because what a
// failed write means is the same for every one of them but what was being
// written is not.
func writeNewReportDirectory(destination, subject string, files ...outputFile) error {
	if err := os.Mkdir(destination, 0700); err != nil {
		return errors.New("cannot create report directory; destination must be new and parent writable")
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return errors.New("cannot open new report directory")
	}
	defer root.Close()
	for _, file := range files {
		f, err := root.OpenFile(file.name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("cannot create " + subject + " file; incomplete report retained")
		}
		_, writeErr := f.Write(file.data)
		if writeErr == nil {
			writeErr = f.Sync()
		}
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			return errors.New("cannot write " + subject + " file; incomplete report retained")
		}
	}
	return nil
}
