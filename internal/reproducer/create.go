package reproducer

import (
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

// ErrCannotWrite is the one refusal a caller separates from the rest: the
// destination could not be created at all. Everything else a build refuses is a
// statement about the plan or the evidence and has the same remedy whoever is
// asking; this one may mean the account cannot write where it was pointed.
var ErrCannotWrite = errors.New("cannot create reproducer; destination must be new and parent writable")

// Create writes one reproducer: a new derived case holding the occurrences the
// plan retained, and the transformation manifest beside it.
//
// The case it reads is not touched. The destination must be new and outside
// every retained artifact, so a reproducer can never be written into the
// evidence it came from, and the manifest is written last, so a directory left
// behind by an interrupted write has no manifest and is refused rather than
// read as a finished reproducer.
func Create(source *bundle.Bundle, casePath string, plan Plan, output string) (*Manifest, error) {
	resolved, err := resolve(source, plan)
	if err != nil {
		return nil, err
	}
	if len(resolved.retained) == 0 {
		return nil, errors.New("a reproducer retains at least one occurrence")
	}
	original, err := os.Stat(casePath)
	if err != nil {
		return nil, errors.New("cannot read the case this reproducer is derived from")
	}
	destination, err := artifactpath.Destination(output, original)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return nil, errors.New("reproducer destination must be new")
	}
	inputs, err := inputsFor(source, resolved)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return nil, ErrCannotWrite
	}
	derived, err := bundle.Write(filepath.Join(destination, CaseName), inputs, bundle.Provenance{Mode: bundle.Derived, Derivation: Derivation})
	if err != nil {
		return nil, err
	}
	if len(derived.Events) != len(resolved.retained) {
		return nil, errors.New("the derived case does not hold the occurrences this reproducer retained")
	}
	manifest := Manifest{
		Schema:      ManifestSchema,
		Parent:      Artifact{Schema: source.Manifest.Schema, Identity: source.Identity},
		Derived:     Artifact{Schema: derived.Manifest.Schema, Identity: derived.Identity},
		Plan:        plan,
		Occurrences: slices.Clone(resolved.resolution.Occurrences),
		Edits:       slices.Clone(resolved.resolution.Edits),
		Unresolved:  resolved.resolution.Unresolved,
	}
	assigned := make(map[string]string, len(resolved.retained))
	for i, parent := range resolved.retained {
		assigned[parent] = derived.Events[i].ID
		manifest.Occurrences[i].Derived = derived.Events[i].ID
	}
	for i := range manifest.Edits {
		manifest.Edits[i].Derived = assigned[manifest.Edits[i].Parent]
	}
	document, err := encode(manifest)
	if err != nil {
		return nil, errors.New("cannot record this transformation")
	}
	if err := writeDocument(filepath.Join(destination, ManifestName), document); err != nil {
		return nil, err
	}
	// The reproducer is reported from what was actually written, so a manifest
	// this release cannot read back is a failed build rather than a result.
	return Open(destination)
}

// inputsFor rebuilds one source per contributing source of the case, in the
// order the case records them, holding the retained occurrences in their own
// evidence order. Payload bytes carry framing, so concatenating them restores a
// readable source without reframing anything.
func inputsFor(source *bundle.Bundle, resolved *resolved) ([]bundle.Input, error) {
	inputs := make([]bundle.Input, 0, len(source.Manifest.Sources))
	for _, declared := range source.Manifest.Sources {
		input := bundle.Input{
			Options:      hl7.Options{Format: declared.Format, Terminator: declared.Terminator},
			Observations: map[int]bundle.Observation{},
		}
		for _, id := range resolved.retained {
			event := eventOf(source, id)
			if event.SourceID != declared.ID {
				continue
			}
			input.Data = append(input.Data, resolved.payloads[id]...)
			input.Observations[len(input.Observations)+1] = bundle.Observation{Direction: event.Direction}
		}
		if len(input.Observations) > 0 {
			inputs = append(inputs, input)
		}
	}
	if len(inputs) == 0 {
		return nil, errors.New("a reproducer retains at least one occurrence")
	}
	return inputs, nil
}

func eventOf(source *bundle.Bundle, id string) *bundle.Event {
	for i := range source.Events {
		if source.Events[i].ID == id {
			return &source.Events[i]
		}
	}
	return nil
}

// Open reads one reproducer directory and refuses it the moment the manifest
// and the derived evidence disagree. The evidence is verified by the same
// reader `readmit timeline` runs; the manifest is then checked against what
// that reader accepted, so a record of a transformation can never describe
// evidence that is no longer there.
func Open(path string) (*Manifest, error) {
	directory, err := artifactpath.Directory(path)
	if err != nil {
		return nil, errors.New("a reproducer is one directory holding a derived case and its manifest")
	}
	document, err := readDocument(filepath.Join(directory, ManifestName), maxManifestBytes)
	if err != nil {
		return nil, err
	}
	manifest, err := DecodeManifest(document)
	if err != nil {
		return nil, err
	}
	derived, err := bundle.Open(filepath.Join(directory, CaseName))
	if err != nil {
		return nil, errors.New("the derived case of this reproducer could not be verified as complete, unmodified evidence")
	}
	if manifest.Derived.Identity != derived.Identity || manifest.Derived.Schema != derived.Manifest.Schema ||
		derived.Manifest.Provenance.Derivation != Derivation || len(manifest.Occurrences) != len(derived.Events) {
		return nil, errors.New("this manifest describes different evidence than the derived case beside it")
	}
	assigned := make(map[string]bool, len(manifest.Occurrences))
	for i, retained := range manifest.Occurrences {
		if retained.Derived != derived.Events[i].ID {
			return nil, errors.New("this manifest describes different evidence than the derived case beside it")
		}
		assigned[retained.Derived] = true
	}
	for _, edit := range manifest.Edits {
		if !assigned[edit.Derived] {
			return nil, errors.New("this manifest records an edit of an occurrence the derived case does not hold")
		}
	}
	return &manifest, nil
}

// DecodeManifest reads a transformation manifest. Unknown members and unknown
// versions are errors; there is no migration and no repair.
func DecodeManifest(data []byte) (Manifest, error) {
	if len(data) > maxManifestBytes {
		return Manifest{}, errors.New("reproducer manifest exceeds its size limit")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &declared) != nil {
		return Manifest{}, errors.New("invalid reproducer manifest")
	}
	if declared.Schema != ManifestSchema {
		return Manifest{}, errors.New("reproducer manifest declares a contract version this release does not read")
	}
	var present struct {
		Parent      *Artifact     `json:"parent"`
		Derived     *Artifact     `json:"derived"`
		Plan        *Plan         `json:"plan"`
		Occurrences *[]Retained   `json:"occurrences"`
		Edits       *[]Edit       `json:"edits"`
		Unresolved  *[]Unresolved `json:"unresolved"`
	}
	if json.Unmarshal(data, &present) != nil || present.Parent == nil || present.Derived == nil ||
		present.Plan == nil || present.Occurrences == nil || present.Edits == nil || present.Unresolved == nil {
		return Manifest{}, errors.New("a reproducer manifest declares its parent, its derived case, the plan applied and what that plan resolved to")
	}
	var manifest Manifest
	if json.Unmarshal(data, &manifest, json.RejectUnknownMembers(true)) != nil {
		return Manifest{}, errors.New("invalid reproducer manifest")
	}
	if !identityPattern.MatchString(manifest.Parent.Identity) || !identityPattern.MatchString(manifest.Derived.Identity) {
		return Manifest{}, errors.New("a reproducer manifest names the verified identity of both cases")
	}
	if err := validatePlan(manifest.Plan); err != nil {
		return Manifest{}, err
	}
	if manifest.Plan.Case != manifest.Parent.Identity {
		return Manifest{}, errors.New("this manifest records a plan authored against different evidence than the parent it names")
	}
	if len(manifest.Occurrences) > MaxOccurrences {
		return Manifest{}, errors.New("a reproducer retains at most 256 occurrences")
	}
	return manifest, nil
}

func encode(value any) ([]byte, error) {
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func writeDocument(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create the reproducer manifest; incomplete reproducer retained")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if closeErr := file.Close(); writeErr != nil || closeErr != nil {
		return errors.New("cannot write the reproducer manifest; incomplete reproducer retained")
	}
	return nil
}

func readDocument(path string, limit int) ([]byte, error) {
	refused := errors.New("a reproducer is one directory holding a derived case and its manifest")
	entry, err := os.Lstat(path)
	if err != nil || !entry.Mode().IsRegular() || entry.Size() > int64(limit) {
		return nil, refused
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, refused
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(data) > limit {
		return nil, refused
	}
	return data, nil
}
