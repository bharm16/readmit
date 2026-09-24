package corpus

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/bharm16/readmit/internal/artifactdir"
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
	if _, err := os.Lstat(manifestPath); err == nil {
		return Manifest{}, errors.New("the corpus manifest destination must be a new file")
	} else if !os.IsNotExist(err) {
		return Manifest{}, causedError{"the corpus manifest destination must be a new file", err}
	}
	file, err := os.OpenFile(corpusPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Manifest{}, causedError{"cannot create the corpus; destination must be new and parent writable", err}
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
	return manifestFile.Create(path, data)
}

// manifestFile is how a corpus manifest is created, through the shared
// document store, which keeps the filesystem's error behind the sentence.
var manifestFile = artifactdir.Document{
	Errors: artifactdir.DocumentErrors{
		Create: errors.New("cannot create the corpus manifest; destination must be new and parent writable"),
		Write:  errors.New("cannot write the corpus manifest"),
	},
}

// Open reads one bounded, regular corpus stream for scanning. The file is left
// open for the caller to read and close: a scan reads it through a bounded
// window, so nothing here reads it into memory first.
func Open(path string) (io.ReadCloser, error) {
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		// The resolver's own refusal names no cause; the stat that follows it
		// reads no content and says whether this account may reach the path.
		_, cause := os.Stat(path)
		return nil, causedError{"cannot resolve the declared stream", cause}
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, causedError{"a scanned stream must be a readable regular file", err}
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("a scanned stream must be a readable regular file")
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, causedError{"cannot open the declared stream", err}
	}
	opened, statErr := file.Stat()
	if statErr != nil || !opened.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("a scanned stream must be a regular file")
	}
	return file, nil
}

// causedError is a fixed diagnostic that keeps the filesystem's error behind
// it. Only the sentence is ever shown, so it never repeats a path; the cause
// lets a caller tell a file this account may not open or create from any
// other refusal without touching the file again.
type causedError struct {
	sentence string
	cause    error
}

func (e causedError) Error() string { return e.sentence }

func (e causedError) Unwrap() error { return e.cause }
