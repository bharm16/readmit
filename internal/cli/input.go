package cli

import (
	"errors"
	"io"
	"os"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// readInputFile keeps input limits and private diagnostics shared by commands.
func readInputFile(path string, limit int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("input must be a readable regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open input file")
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

// writeNewFile creates one new file exclusively at a reserved destination and
// removes a partial one rather than leaving it behind. The caller supplies the
// two diagnostics because what a failed creation means differs by command:
// artifactpath owns where the file may go, and this owns how it is written.
func writeNewFile(path string, data []byte, cannotCreate, cannotWrite string) error {
	path, err := artifactpath.Destination(path)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New(cannotCreate)
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
