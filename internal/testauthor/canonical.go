package testauthor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/bharm16/readmit/internal/artifactdir"
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
	data, err := specEntry.Read(path)
	if err != nil {
		return nil, err
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

// specEntry is how a test spec entry of the workspace is read: never through
// a link, and never past its bound.
var specEntry = artifactdir.Document{
	MaxBytes: testrunner.MaxSpecBytes,
	Refusals: artifactdir.DocumentRefusals{
		Irregular: errors.New("cannot read a bounded regular test spec"),
		Open:      errors.New("cannot read test spec"),
		Changed:   errors.New("cannot read a regular test spec"),
		Read:      errors.New("cannot read test spec"),
		Size:      errors.New("cannot read a bounded regular test spec"),
	},
}
