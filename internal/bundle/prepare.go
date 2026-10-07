package bundle

import (
	"bytes"
	"context"
	"errors"

	"github.com/bharm16/readmit/internal/artifactdir"
)

// Prepared owns verified case bytes without requiring a directory. Its copies
// and explicit writer use the same bounded codec and reader as ordinary cases.
type Prepared struct {
	files            map[string][]byte
	identity, schema string
}

// Prepare computes the exact case an ordinary Write would create, with no I/O.
// Recorded and collected sources still require their dedicated writers.
func Prepare(inputs []Input, provenance Provenance) (*Prepared, error) {
	if provenance.Mode == Recorded || provenance.Mode == Collected {
		return nil, errors.New("recorded and collected sessions require their dedicated writers")
	}
	b, err := build(inputs, provenance)
	if err != nil {
		return nil, err
	}
	files, err := encode(b)
	if err != nil {
		return nil, err
	}
	files["identity.sha256"] = []byte(identityFor(b.Manifest.Schema, files) + "\n")
	return CaptureFiles(files)
}

// Capture freezes exact verified on-disk bytes, including noncanonical but
// valid metadata encodings. It neither rewrites nor normalizes the source.
func Capture(path string) (*Prepared, error) {
	files, err := readFiles(path)
	if err != nil {
		return nil, err
	}
	return CaptureFiles(files)
}

// CaptureFiles owns and verifies one detached original snapshot. The exact
// captured bytes remain available for provenance, without a second caller read.
func CaptureFiles(files map[string][]byte) (*Prepared, error) {
	snapshot, err := artifactdir.Snapshot(files, family.Layout)
	if err != nil {
		return nil, err
	}
	b, err := Verify(snapshot)
	if err != nil {
		return nil, err
	}
	return &Prepared{files: snapshot, identity: b.Identity, schema: b.Manifest.Schema}, nil
}
func copyPreparedFiles(files map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(files))
	for name, raw := range files {
		out[name] = bytes.Clone(raw)
	}
	return out
}
func (p *Prepared) Identity() string         { return p.identity }
func (p *Prepared) Files() map[string][]byte { return copyPreparedFiles(p.files) }

// Bundle returns an independently verified value, never a mutable alias.
func (p *Prepared) Bundle() (*Bundle, error) { return Verify(p.files) }

// Write explicitly retains the prepared case through the ordinary writer,
// whose completion marker is regenerated last from these exact bytes.
func (p *Prepared) Write(ctx context.Context, path string, durability artifactdir.Durability) (*Bundle, error) {
	files := p.Files()
	delete(files, "identity.sha256")
	sealed := family
	sealed.Seal = artifactdir.DirectoryHash(p.schema)
	identity, err := artifactdir.Write(ctx, path, sealed, durability, files)
	if err != nil {
		return nil, err
	}
	if identity != p.identity {
		return nil, errors.New("prepared case identity changed while writing")
	}
	return p.Bundle()
}
