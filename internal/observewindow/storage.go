package observewindow

import (
	"errors"
	"io"
	"os"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// ReadWindow opens one declared observation window. Like every other input
// reader here, it accepts a regular file only: a window that came from a pipe
// or a device is not a document an operator selected.
func ReadWindow(path string) (Window, error) {
	data, err := readFile(path, MaxWindowBytes, "observation window")
	if err != nil {
		return Window{}, err
	}
	return DecodeWindow(data)
}

// ReadCompletion opens one retained completion record. What it returns is
// evidence of what a collector observed; deciding whether those observations
// support the verdict is Window.Verify's job, and a caller holding the declared
// window is expected to ask.
func ReadCompletion(path string) (Completion, error) {
	data, err := readFile(path, MaxCompletionBytes, "observation completion")
	if err != nil {
		return Completion{}, err
	}
	return DecodeCompletion(data)
}

// WriteWindow records one declared observation window. It validates before
// writing, so a window readmit could not read back is never produced, and it
// writes the file in full to a new owner-only file renamed onto the destination,
// so a reader never observes a partial document and a failed write leaves the
// previous one exactly as it was.
func WriteWindow(path string, w Window) error {
	data, err := EncodeWindow(w)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return errors.New("cannot write an observation window here")
	}
	incomplete, err := artifactpath.Destination(path + ".incomplete")
	if err != nil {
		return errors.New("cannot write an observation window here; an interrupted write may be retained beside it")
	}
	file, err := os.OpenFile(incomplete, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create the new observation window; an interrupted write is retained")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(incomplete)
		return errors.New("cannot write the new observation window")
	}
	if err := os.Rename(incomplete, destination); err != nil {
		os.Remove(incomplete)
		return errors.New("cannot replace the observation window")
	}
	return nil
}

// WriteCompletion retains one completion at a new destination. A completion is
// evidence, so the destination must not already exist: a window's verdict is
// never rewritten in place, and a second run writes a second record beside the
// first rather than replacing it.
func WriteCompletion(path string, c Completion) error {
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return err
	}
	data, err := EncodeCompletion(c)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create observation completion; destination must be new and writable")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(destination)
		return errors.New("cannot write observation completion")
	}
	return nil
}

// readFile keeps the limit and the private diagnostic in one place. Checking
// the mode before opening avoids blocking on a FIFO; checking the opened
// descriptor again retains the check because the path can change underneath it.
func readFile(path string, limit int, kind string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("an " + kind + " must be a readable regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open " + kind + " file")
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("an " + kind + " must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, errors.New("cannot read " + kind + " file")
	}
	if len(data) > limit {
		return nil, errors.New(kind + " exceeds size limit")
	}
	return data, nil
}
