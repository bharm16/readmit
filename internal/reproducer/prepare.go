package reproducer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
)

// Prepared separates immutable derived evidence from the optional live source
// guard. A detached derivation can be verified or retained, never sent merely
// because its bytes were opened offline.
type Prepared struct {
	sourcePath     string
	sourceInfo     os.FileInfo
	sourceIdentity string
	original       map[string][]byte
	derived        *bundle.Prepared
	manifest       []byte
}

// Prepare performs bounded local reads and derives a case entirely in memory.
// It retains the original physical directory and content identity for rechecks.
func Prepare(sourcePath string, plan Plan) (*Prepared, error) {
	path, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return nil, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return nil, errors.New("original case is not a directory")
	}
	captured, err := bundle.Capture(path)
	if err != nil {
		return nil, err
	}
	prepared, err := PrepareFiles(captured.Files(), plan)
	if err != nil {
		return nil, err
	}
	prepared.sourcePath, prepared.sourceInfo = path, info
	if err := prepared.VerifyUnchanged(); err != nil {
		return nil, err
	}
	return prepared, nil
}

// PrepareFiles recomputes a derivation from detached original evidence. No live
// filesystem guard is installed, so this is not an executable source selection.
func PrepareFiles(original map[string][]byte, plan Plan) (*Prepared, error) {
	captured, err := bundle.CaptureFiles(original)
	if err != nil {
		return nil, err
	}
	source, err := captured.Bundle()
	if err != nil {
		return nil, err
	}
	prepared, err := prepareSource(source, plan)
	if err != nil {
		return nil, err
	}
	prepared.original = captured.Files()
	return prepared, nil
}
func prepareSource(source *bundle.Bundle, plan Plan) (*Prepared, error) {
	resolved, err := resolve(source, plan)
	if err != nil {
		return nil, err
	}
	if len(resolved.retained) == 0 {
		return nil, errors.New("a reproducer retains at least one occurrence")
	}
	inputs, err := inputsFor(source, resolved)
	if err != nil {
		return nil, err
	}
	derived, err := bundle.Prepare(inputs, bundle.Provenance{Mode: bundle.Derived, Derivation: Derivation})
	if err != nil {
		return nil, err
	}
	caseValue, err := derived.Bundle()
	if err != nil {
		return nil, err
	}
	if len(caseValue.Events) != len(resolved.retained) {
		return nil, errors.New("the derived case does not hold the occurrences this reproducer retained")
	}
	manifest := Manifest{Schema: ManifestSchema, Parent: Artifact{Schema: source.Manifest.Schema, Identity: source.Identity}, Derived: Artifact{Schema: caseValue.Manifest.Schema, Identity: derived.Identity()}, Plan: plan, Occurrences: slices.Clone(resolved.resolution.Occurrences), Edits: slices.Clone(resolved.resolution.Edits), Unresolved: resolved.resolution.Unresolved}
	assigned := make(map[string]string, len(resolved.retained))
	for i, parent := range resolved.retained {
		assigned[parent] = caseValue.Events[i].ID
		manifest.Occurrences[i].Derived = caseValue.Events[i].ID
	}
	for i := range manifest.Edits {
		manifest.Edits[i].Derived = assigned[manifest.Edits[i].Parent]
	}
	raw, err := encode(manifest)
	if err != nil {
		return nil, err
	}
	if _, err := DecodeManifest(raw); err != nil {
		return nil, err
	}
	return &Prepared{sourceIdentity: source.Identity, derived: derived, manifest: raw}, nil
}
func copyFiles(files map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(files))
	for name, raw := range files {
		out[name] = bytes.Clone(raw)
	}
	return out
}
func (p *Prepared) Identity() string                 { return p.derived.Identity() }
func (p *Prepared) SourceIdentity() string           { return p.sourceIdentity }
func (p *Prepared) SourcePath() string               { return p.sourcePath }
func (p *Prepared) OriginalFiles() map[string][]byte { return copyFiles(p.original) }
func (p *Prepared) CaseFiles() map[string][]byte     { return p.derived.Files() }
func (p *Prepared) Case() (*bundle.Bundle, error)    { return p.derived.Bundle() }
func (p *Prepared) Manifest() Manifest               { m, _ := DecodeManifest(p.manifest); return m }
func (p *Prepared) Files() map[string][]byte {
	files := map[string][]byte{ManifestName: bytes.Clone(p.manifest)}
	for name, raw := range p.derived.Files() {
		files[CaseName+"/"+name] = raw
	}
	return files
}

// VerifyUnchanged requires a live source selection and checks physical as well
// as byte identity. It resolves no secret and grants no network authority.
func (p *Prepared) VerifyUnchanged() error {
	if p == nil || p.sourcePath == "" || p.sourceInfo == nil {
		return errors.New("a detached derivation has no live source selection")
	}
	info, err := os.Stat(p.sourcePath)
	if err != nil || !os.SameFile(info, p.sourceInfo) {
		return errors.New("original case directory changed after preparation")
	}
	source, err := bundle.Open(p.sourcePath)
	if err != nil || source.Identity != p.sourceIdentity {
		return errors.New("original case changed after preparation")
	}
	return nil
}

// Write retains only the frozen derived case and ordinary transformation
// manifest. Writing grants no execution authority and never changes a source.
func (p *Prepared) Write(ctx context.Context, output string) error {
	destination := output
	if p.sourceInfo != nil {
		var err error
		destination, err = artifactpath.Destination(output, p.sourceInfo)
		if err != nil {
			return err
		}
	}
	written, err := artifactdir.Create(destination, family, artifactdir.Durable)
	if err != nil {
		return err
	}
	defer written.Close()
	if _, err := p.derived.Write(ctx, filepath.Join(written.Path(), CaseName), artifactdir.Durable); err != nil {
		return err
	}
	_, err = written.Seal(p.manifest)
	return err
}
