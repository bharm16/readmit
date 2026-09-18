package corpus

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// Write streams one corpus to a new file and writes its manifest beside it.
//
// Both destinations are reserved through artifactpath before anything is
// created, and both are created exclusively, so a corpus is never written into
// retained evidence, never reached through a symbolic link, and never written
// over something that already exists. The manifest is written last, so it is
// the completion marker: a corpus with a manifest beside it was written whole.
//
// A cancelled or failed generation removes the partial corpus it created and
// writes no manifest. That is not a claim that cancellation can retract bytes —
// it cannot, which is why generation writes to a new file it owns rather than
// to a stream somebody else is already reading.
func Write(ctx context.Context, destination, manifest string, inputs Inputs, report func(Progress)) (Manifest, error) {
	if err := inputs.Validate(); err != nil {
		return Manifest{}, err
	}
	corpusPath, err := artifactpath.Destination(destination)
	if err != nil {
		return Manifest{}, err
	}
	manifestPath, err := artifactpath.Destination(manifest)
	if err != nil {
		return Manifest{}, err
	}
	if corpusPath == manifestPath {
		return Manifest{}, errors.New("the corpus and its manifest are two new files")
	}
	if _, err := os.Lstat(manifestPath); !os.IsNotExist(err) {
		return Manifest{}, errors.New("the corpus manifest destination must be a new file")
	}
	file, err := os.OpenFile(corpusPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Manifest{}, errors.New("cannot create the corpus; destination must be new and parent writable")
	}
	summary, err := generateInto(ctx, file, inputs, report)
	if err != nil {
		os.Remove(corpusPath)
		return Manifest{}, err
	}
	document := Manifest{Schema: ManifestSchema, Inputs: inputs, Bytes: summary.Bytes, SHA256: summary.SHA256}
	encoded, err := EncodeManifest(document)
	if err != nil {
		os.Remove(corpusPath)
		return Manifest{}, err
	}
	if err := writeNew(manifestPath, encoded); err != nil {
		os.Remove(corpusPath)
		return Manifest{}, err
	}
	return document, nil
}

// generateInto owns the open corpus file: it flushes, syncs and closes it, so
// every path out of Write leaves either a complete corpus or no file at all.
func generateInto(ctx context.Context, file *os.File, inputs Inputs, report func(Progress)) (Summary, error) {
	summary, err := Generate(ctx, file, inputs, report)
	syncErr := error(nil)
	if err == nil {
		syncErr = file.Sync()
	}
	closeErr := file.Close()
	switch {
	case err != nil:
		return Summary{}, err
	case syncErr != nil || closeErr != nil:
		return Summary{}, errors.New("cannot write the corpus")
	}
	return summary, nil
}

func writeNew(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create the corpus manifest; destination must be new and parent writable")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(path)
		return errors.New("cannot write the corpus manifest")
	}
	return nil
}

// Open reads one bounded, regular corpus stream for scanning. The file is left
// open for the caller to read and close: a scan reads it through a bounded
// window, so nothing here reads it into memory first.
func Open(path string) (io.ReadCloser, error) {
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return nil, errors.New("cannot resolve the declared stream")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("a scanned stream must be a readable regular file")
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, errors.New("cannot open the declared stream")
	}
	opened, statErr := file.Stat()
	if statErr != nil || !opened.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("a scanned stream must be a regular file")
	}
	return file, nil
}
