package operation

import (
	"errors"
	"os"
)

// ReportFile is one member of a report directory an operation creates whole.
type ReportFile struct {
	Name string
	Data []byte
}

// WriteReportDirectory creates one new directory and the files a report is
// made of. The directory must not exist, every file is created exclusively
// inside it, and a partial directory is retained rather than removed so an
// interrupted report is visible instead of looking like one that was never
// started. The subject names the report in the diagnostics, because what a
// failed write means is the same for every one of them but what was being
// written is not. Both entry points write every report directory through it.
func WriteReportDirectory(destination, subject string, files ...ReportFile) error {
	if err := os.Mkdir(destination, 0700); err != nil {
		return errors.New("cannot create report directory; destination must be new and parent writable")
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return errors.New("cannot open new report directory")
	}
	defer root.Close()
	for _, file := range files {
		f, err := root.OpenFile(file.Name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("cannot create " + subject + " file; incomplete report retained")
		}
		_, writeErr := f.Write(file.Data)
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
