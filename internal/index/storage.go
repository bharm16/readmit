package index

import (
	"errors"
	"io"
	"os"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// Write creates one new index file at a reserved destination and returns the
// path it actually wrote. artifactpath owns where an index may go: it refuses a
// destination inside retained case, run, result, review or report evidence, and
// one reached through a symbolic link, so an index can never be written into
// the evidence it describes. Creation is exclusive, so an existing index is
// never overwritten in place; replacing one is deleting it and building again.
//
// A failed write removes its own partial file rather than leaving something a
// later read would have to guess about.
func Write(destination string, document Document) (string, error) {
	data, err := Encode(document)
	if err != nil {
		return "", err
	}
	path, err := artifactpath.Destination(destination)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", errors.New("cannot create index; destination must be new and parent writable")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(path)
		return "", errors.New("cannot write index")
	}
	return path, nil
}

// Open reads one bounded, regular index file. A file past the size limit is
// refused before it is decoded rather than read into memory first.
func Open(path string) (Document, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return Document{}, errors.New("an index must be a readable regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Document{}, errors.New("cannot open index")
	}
	opened, statErr := file.Stat()
	if statErr != nil || !opened.Mode().IsRegular() {
		file.Close()
		return Document{}, errors.New("an index must be a regular file")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, MaxIndexBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return Document{}, errors.New("cannot read index")
	}
	return Decode(data)
}
