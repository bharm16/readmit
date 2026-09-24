package runresult

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// Family is what an evidence directory is, named by the record it holds.
type Family string

const (
	CaseFamily   Family = "case"
	RunFamily    Family = "run"
	ResultFamily Family = "result"
	JobFamily    Family = "job"
)

// The fixed-name records that name an execution. A durable run retains its
// engine pin beside its plan and also holds a result, so the pin is looked
// for first; a plain result holds only its result record.
const (
	jobMarker    = "engine.json"
	resultMarker = "result.json"
	manifestName = "manifest.json"
)

// maxManifestBytes matches what the verified case and run readers accept, so
// naming a directory by its manifest never imposes a smaller limit than the
// reader that will be handed the same file.
const maxManifestBytes = 16 << 20

// Evidence is one evidence directory opened through its family's verified
// reader, stated once for every consumer: its identity, the input it declares,
// the target it retained with that target's identity, the engine pin and the
// lifecycle a durable run retained. A member a family does not retain is nil.
type Evidence struct {
	Family Family
	// Identity is the artifact's own content identity. A durable run that
	// stopped before it retained a result has none.
	Identity string
	// Case is a case family's verified bundle.
	Case *bundle.Bundle
	// Run is a run family's verified run, or the run a result retained.
	Run *replay.Run
	// Artifact is a result family's verified result, or a durable run's.
	Artifact *testrunner.Artifact
	// Lifecycle is a durable run's recovered journal. OpenAs leaves it nil
	// for a run that retained no result folder or whose engine this build
	// does not evaluate.
	Lifecycle *durablerun.Summary
	// Pin is the engine pin a durable run retained.
	Pin    *Pin
	Input  *Input
	Target *Target
}

// Input is the evidence that went in: the case identity an artifact names as
// its source, the replay operators a run declared over it, and how many field
// changes those operators made.
type Input struct {
	Identity        string
	Transformations []replay.Transformation
	Changes         int
}

// Target is the configuration a run or result retained and its identity.
type Target struct {
	Record   replay.TargetRecord
	Identity string
}

// Pin is the engine pin document a durable run retained. Digest is the
// SHA-256 of its bytes, which is what remains to compare when Document is nil
// because this build does not read the pin.
type Pin struct {
	Digest   string
	Document *engine.Pin
	err      error
}

// unsupported reports a pin written for an engine, spec or profile this build
// does not evaluate, as opposed to one that is unreadable for any other reason.
func (p *Pin) unsupported() bool {
	return errors.Is(p.err, engine.ErrUnsupportedVersion) || p.Document != nil && errors.Is(p.Document.Supported(), engine.ErrUnsupportedVersion)
}

// Marker is what an entry under a record's name must be to name a family.
type Marker uint8

const (
	// AnyEntry names the family by any entry under the record's name, and
	// leaves refusing one that is not a readable record to the family's
	// verified reader. It is the opener's rule.
	AnyEntry Marker = iota
	// RegularFile names the family only by a regular file, for a caller that
	// reads nothing and would otherwise name a folder or a link as a record.
	RegularFile
)

// ExecutionFamily names a durable run or a result by the fixed-name record the
// directory holds, and is "" for anything else. It reads nothing and verifies
// nothing: opening the directory remains the verification, so a listing may
// call it without reading any evidence.
func ExecutionFamily(directory string, marker Marker) Family {
	return executionFamily(func(name string) bool {
		info, err := os.Lstat(filepath.Join(directory, name))
		return err == nil && (marker == AnyEntry || info.Mode().IsRegular())
	})
}

// ExecutionFamilyIn is ExecutionFamily for a tree of regular files already
// read, such as a packet's, keyed by slash-separated names; directory "" is
// its root.
func ExecutionFamilyIn(files map[string][]byte, directory string) Family {
	return executionFamily(func(name string) bool { return files[path.Join(directory, name)] != nil })
}

func executionFamily(holds func(record string) bool) Family {
	switch {
	case holds(jobMarker):
		return JobFamily
	case holds(resultMarker):
		return ResultFamily
	}
	return ""
}

// Name reports which evidence family the directory at path declares: a
// durable run or a result by its record, otherwise a case or a run by the
// contract family its manifest declares. It verifies nothing. noun names the
// consumer in the refusals, which carry no path.
func Name(path, noun string) (Family, error) {
	directory, err := artifactpath.Resolve(path)
	if err != nil {
		return "", err
	}
	if family := ExecutionFamily(directory, AnyEntry); family != "" {
		return family, nil
	}
	data, err := readBounded(filepath.Join(directory, manifestName), maxManifestBytes)
	if err != nil {
		return "", errors.New("cannot read " + noun + " artifact manifest")
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &header) != nil {
		return "", errors.New("invalid " + noun + " artifact manifest")
	}
	// Dispatch by contract family — artifactpath owns the family list — and
	// let each reader own its own version support, including derived-case
	// versions added independently of this.
	switch artifactpath.EvidenceFamily(header.Schema) {
	case artifactpath.FamilyCase:
		return CaseFamily, nil
	case artifactpath.FamilyRun:
		return RunFamily, nil
	}
	return "", errors.New("unsupported " + noun + " artifact contract")
}

// OpenEvidence names the directory at path and opens it as that family.
func OpenEvidence(path, noun string) (*Evidence, error) {
	family, err := Name(path, noun)
	if err != nil {
		return nil, err
	}
	return OpenAs(path, family)
}

// OpenAs opens the directory at path through family's verified reader. A
// case, run or result reader is handed path itself and enforces its own root
// contract; a durable run is read at its resolved directory.
//
// A durable run is opened for what it retained, which is more than its
// lifecycle: one that stopped before it retained a result folder states its
// pin alone, unchecked against a journal that may promise a result, and one
// pinned to an engine this build does not evaluate states its result without
// a lifecycle. Open is the reader for a consumer that requires the lifecycle.
func OpenAs(path string, family Family) (*Evidence, error) {
	switch family {
	case CaseFamily:
		b, err := bundle.Open(path)
		if err != nil {
			return nil, err
		}
		return &Evidence{Family: CaseFamily, Identity: b.Identity, Case: b, Input: &Input{Identity: b.Identity}}, nil
	case RunFamily:
		r, err := replay.Open(path)
		if err != nil {
			return nil, err
		}
		return &Evidence{
			Family: RunFamily, Identity: r.Identity, Run: r,
			Input:  &Input{Identity: r.Manifest.SourceBundleIdentity, Transformations: r.Manifest.Transformations, Changes: len(r.Manifest.Changes)},
			Target: &Target{Record: r.Manifest.Target, Identity: r.Manifest.Target.Identity()},
		}, nil
	case ResultFamily:
		artifact, err := testrunner.Open(path)
		if err != nil {
			return nil, err
		}
		return (&Result{Artifact: artifact}).Evidence(), nil
	case JobFamily:
		directory, err := artifactpath.Resolve(path)
		if err != nil {
			return nil, err
		}
		return openJob(directory)
	}
	return nil, errors.New("unsupported evidence family")
}

// openJob reads a durable run: the engine pin it retained, and the result it
// retained if it got that far. The pin is read as its own document so a pin
// this build does not understand still leaves a digest to compare, rather
// than failing the whole read or vanishing from it.
func openJob(directory string) (*Evidence, error) {
	pin, err := readPin(directory)
	if err != nil {
		return nil, err
	}
	// A stopped job with no result still carries a useful engine pin. There
	// is no finalized result whose identity could be cross-checked.
	if _, err := os.Lstat(filepath.Join(directory, "result")); os.IsNotExist(err) {
		return &Evidence{Family: JobFamily, Pin: pin}, nil
	}
	retained, openErr := Open(directory)
	if openErr != nil {
		// A future engine pin remains useful as an unreadable digest, but this
		// build cannot recover its lifecycle. Its result is still opened
		// through the result contract; supported engines may never bypass the
		// lifecycle/result identity cross-check this way.
		if !pin.unsupported() {
			return nil, openErr
		}
		result, err := artifactpath.Child(directory, "result")
		if err != nil {
			return nil, err
		}
		artifact, err := testrunner.Open(result)
		if err != nil {
			return nil, err
		}
		opened := (&Result{Artifact: artifact}).Evidence()
		opened.Family, opened.Pin = JobFamily, pin
		return opened, nil
	}
	return retained.Evidence(), nil
}

// Evidence states an opened result or durable run as the evidence every
// comparison reads.
func (r *Result) Evidence() *Evidence {
	opened := &Evidence{Family: ResultFamily}
	if r.Durable {
		lifecycle := r.Lifecycle
		opened.Family, opened.Lifecycle, opened.Pin = JobFamily, &lifecycle, r.Pin
	}
	a := r.Artifact
	if a == nil {
		return opened
	}
	opened.Identity, opened.Artifact, opened.Run = a.Identity, a, a.Run
	// A result's input and target are separate declarations: a result whose
	// configuration never validated names neither, and is stated as naming
	// neither rather than as an empty one.
	if a.Result.InputBundleIdentity != "" {
		opened.Input = &Input{Identity: a.Result.InputBundleIdentity}
		if a.Run != nil {
			opened.Input.Transformations, opened.Input.Changes = a.Run.Manifest.Transformations, len(a.Run.Manifest.Changes)
		}
	}
	if a.Result.Target != nil {
		opened.Target = &Target{Record: *a.Result.Target, Identity: a.Result.Target.Identity()}
	}
	return opened
}

// readPin reads the engine pin a durable run retained, keeping its digest
// whether or not this build decodes it.
func readPin(directory string) (*Pin, error) {
	raw, err := readBounded(filepath.Join(directory, jobMarker), engine.MaxPinBytes)
	if err != nil {
		return nil, errors.New("cannot read the engine pin this durable run retained")
	}
	sum := sha256.Sum256(raw)
	pin := &Pin{Digest: hex.EncodeToString(sum[:])}
	decoded, err := engine.Decode(raw)
	if err != nil {
		pin.err = err
	} else {
		pin.Document = &decoded
	}
	return pin, nil
}

func readBounded(path string, limit int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("evidence record must be a bounded regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open evidence record")
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("evidence record must be a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(data) > limit {
		return nil, errors.New("cannot read evidence record within size limit")
	}
	return data, nil
}
