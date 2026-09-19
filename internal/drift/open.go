package drift

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// maxManifestBytes matches what the verified readers themselves accept, so
// dispatching on a manifest's declared contract never imposes a smaller limit
// than the reader that will be handed the same file.
const maxManifestBytes = 16 << 20

// side is one half of a comparison: the report's own view of it, plus the
// retained records the comparison reads part by part. Those records stay here
// and never reach the report — a target address is read to say whether it
// changed and is not what is said.
type side struct {
	report          Side
	target          *replay.TargetRecord
	transformations []replay.Transformation
}

// openSide reads one artifact directory and states what it retains about each
// of the four causes. It opens no network connection, writes nothing, and
// reads each artifact through that artifact's own verified reader.
func openSide(path string) (*side, error) {
	directory, err := artifactpath.Resolve(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(directory)
	if err != nil {
		return nil, errors.New("cannot inspect drift input")
	}
	if !info.IsDir() {
		return nil, errors.New("a drift comparison reads a case, run, result or durable run directory; a message file retains no input identity, target, engine or rule of its own")
	}
	// A durable run is recognized by the engine pin it retains beside its
	// plan, which is the only place the environment and the rule are recorded
	// at all. It is checked first because a job also holds a result.
	if _, err := os.Lstat(filepath.Join(directory, "engine.json")); err == nil {
		return fromJob(directory)
	}
	if _, err := os.Lstat(filepath.Join(directory, "result.json")); err == nil {
		return fromResult(path, ResultKind)
	}
	data, err := readFile(filepath.Join(directory, "manifest.json"), maxManifestBytes)
	if err != nil {
		return nil, errors.New("cannot read drift artifact manifest")
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &header) != nil {
		return nil, errors.New("invalid drift artifact manifest")
	}
	// Dispatch by contract family and let each reader own its own version
	// support, including derived-case versions added independently of this.
	if strings.HasPrefix(header.Schema, "readmit-case/") {
		return fromCase(path)
	}
	if strings.HasPrefix(header.Schema, "readmit-run/") {
		return fromRun(path)
	}
	return nil, errors.New("unsupported drift artifact contract")
}

// newSide starts every cause at undeclared, so a reader that says nothing
// about a cause is reported as saying nothing rather than as agreeing.
func newSide(kind string) *side {
	return &side{report: Side{
		Kind:        kind,
		Input:       InputSide{State: Undeclared, Transformations: []string{}},
		Target:      TargetSide{State: Undeclared, Revision: UnknownRevision},
		Environment: EnvironmentSide{State: Undeclared},
		Rule:        RuleSide{State: Undeclared},
	}}
}

// fromCase reads a verified case bundle. A case is evidence, not an execution:
// it declares the input it is and nothing about a target, an engine or a rule.
func fromCase(path string) (*side, error) {
	b, err := bundle.Open(path)
	if err != nil {
		return nil, err
	}
	s := newSide(CaseKind)
	s.report.Identity = b.Identity
	s.report.Input = InputSide{State: Declared, Identity: b.Identity, Transformations: []string{}}
	return s, nil
}

// fromRun reads a replay run: the case it replayed, the transformations it
// declared over that case, and the target configuration it retained.
func fromRun(path string) (*side, error) {
	r, err := replay.Open(path)
	if err != nil {
		return nil, err
	}
	s := newSide(RunKind)
	s.report.Identity = r.Identity
	s.setInput(r.Manifest.SourceBundleIdentity, r.Manifest.Transformations, len(r.Manifest.Changes))
	record := r.Manifest.Target
	if err := s.setTarget(&record); err != nil {
		return nil, err
	}
	return s, nil
}

// fromResult reads a verified test result. Its input and its target are
// separate declarations: a result whose configuration never validated names
// neither, and it is reported as naming neither rather than as an empty one.
func fromResult(path, kind string) (*side, error) {
	artifact, err := testrunner.Open(path)
	if err != nil {
		return nil, err
	}
	s := newSide(kind)
	s.report.Identity = artifact.Identity
	if artifact.Result.InputBundleIdentity != "" {
		changes, transformations := 0, []replay.Transformation(nil)
		if artifact.Run != nil {
			transformations, changes = artifact.Run.Manifest.Transformations, len(artifact.Run.Manifest.Changes)
		}
		s.setInput(artifact.Result.InputBundleIdentity, transformations, changes)
	}
	if artifact.Result.Target != nil {
		if err := s.setTarget(artifact.Result.Target); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// fromJob reads a durable run: the engine pin it retained, and the result it
// retained if it got that far. The pin is read as its own document so a pin
// this build does not understand still leaves a fingerprint to compare, rather
// than failing the whole comparison or vanishing from it.
func fromJob(directory string) (*side, error) {
	raw, err := readFile(filepath.Join(directory, "engine.json"), engine.MaxPinBytes)
	if err != nil {
		return nil, errors.New("cannot read the engine pin this durable run retained")
	}
	s := newSide(JobKind)
	fingerprint := digest(raw)
	if pin, err := engine.Decode(raw); err != nil {
		s.report.Environment = EnvironmentSide{State: Unreadable, Fingerprint: fingerprint}
		s.report.Rule = RuleSide{State: Unreadable, Fingerprint: fingerprint}
	} else {
		s.report.Environment = EnvironmentSide{State: Declared, Fingerprint: fingerprint, Engine: pin.Engine, Spec: pin.Spec}
		s.report.Rule = RuleSide{State: Declared, Fingerprint: fingerprint, Profile: pin.Profile, Resolution: resolution(pin.Profile)}
	}
	// A job that never reached a result retains no input or target of its own,
	// and says so. The entry is resolved rather than joined, so a job that
	// names evidence outside the directory that was opened is refused rather
	// than read as one that retained nothing.
	if _, err := os.Lstat(filepath.Join(directory, "result")); err != nil {
		return s, nil
	}
	result, err := artifactpath.Child(directory, "result")
	if err != nil {
		return nil, err
	}
	retained, err := fromResult(result, JobKind)
	if err != nil {
		return nil, err
	}
	s.report.Identity = retained.report.Identity
	s.report.Input, s.report.Target = retained.report.Input, retained.report.Target
	s.target, s.transformations = retained.target, retained.transformations
	return s, nil
}

func (s *side) setInput(identity string, transformations []replay.Transformation, changes int) {
	s.transformations = transformations
	names := make([]string, 0, len(transformations))
	for _, transformation := range transformations {
		names = append(names, transformation.Name)
	}
	s.report.Input = InputSide{State: Declared, Identity: identity, Transformations: names, RecordedChanges: changes}
}

// setTarget records the configuration a run was pointed at.
//
// The fingerprint is the SHA-256 of the record's deterministic encoding, which
// is exactly the string `readmit-result/v1` already records as
// `target_identity` and the run explainer already computes, so one target
// retained by a run and the same target retained by a result fingerprint
// identically and can be compared across the two kinds.
func (s *side) setTarget(record *replay.TargetRecord) error {
	encoded, err := json.Marshal(record, json.Deterministic(true))
	if err != nil {
		return errors.New("cannot encode the target identity this artifact retained")
	}
	s.target = record
	s.report.Target = TargetSide{State: Declared, Fingerprint: digest(append(encoded, '\n')), Revision: UnknownRevision}
	return nil
}

// resolution reports whether this build holds the content a profile identity
// names. Only the profile this build implements resolves: no pack is extracted
// and no library is bundled in this release, so every other identity stands
// for content nothing here can read, however familiar the name looks.
func resolution(profile string) string {
	if profile == observation.Profile {
		return BundledProfile
	}
	return UnresolvedProfile
}

func readFile(path string, limit int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("drift input must be a bounded regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open drift input file")
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("drift input must be a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(data) > limit {
		return nil, errors.New("cannot read drift input within size limit")
	}
	return data, nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
