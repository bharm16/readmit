package testauthor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/testrunner"
)

// Import reads a canonical spec without translating it through the smaller
// guided draft vocabulary. Keeping the original bytes retains all supported
// assertions, explicit empty collections, ordering and the spec identity.
// The document is one regular workspace entry: exporting beside it preserves
// the base from which the headless runner resolves relative references.
func Import(root, entry string) ([]byte, error) {
	if artifactpath.EntryName(entry) != nil {
		return nil, errors.New("a test spec must be one regular entry of the workspace")
	}
	path := artifactpath.JoinReference(root, entry)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > testrunner.MaxSpecBytes {
		return nil, errors.New("cannot read a bounded regular test spec")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read test spec")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errors.New("cannot read a regular test spec")
	}
	data, err := io.ReadAll(io.LimitReader(f, testrunner.MaxSpecBytes+1))
	if err != nil {
		return nil, errors.New("cannot read test spec")
	}
	if _, err := testrunner.DecodeSpec(data); err != nil {
		return nil, err
	}
	return data, nil
}

// Export writes exactly the reviewed canonical bytes as a new workspace entry.
// It never serializes through a draft, changes path references, overwrites the
// imported document, prepares a replay, reads credentials or opens a connection.
func Export(root string, data []byte, output string) (Saved, error) {
	if _, err := testrunner.DecodeSpec(data); err != nil {
		return Saved{}, err
	}
	if artifactpath.EntryName(output) != nil {
		return Saved{}, errors.New("a test spec is written to one new entry of the open workspace")
	}
	destination, err := artifactpath.Destination(artifactpath.JoinReference(root, output))
	if err != nil {
		return Saved{}, errors.New("a test spec must be written outside retained evidence")
	}
	if err := write(destination, data); err != nil {
		return Saved{}, err
	}
	written, err := Import(root, output)
	if err != nil || string(written) != string(data) {
		return Saved{}, errors.New("the test spec could not be verified after writing")
	}
	digest := sha256.Sum256(written)
	return Saved{Output: output, Identity: hex.EncodeToString(digest[:])}, nil
}
