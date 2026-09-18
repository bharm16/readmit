package synth

import (
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

// Manifest is the completion record for a family, not a case bundle manifest.
// Each listed relative path is an independently verifiable case bundle.
type Manifest struct {
	Schema    string                 `json:"schema"`
	State     string                 `json:"state"`
	Generator bundle.GeneratorInputs `json:"generator"`
	Cases     []Case                 `json:"cases"`
}

type Case struct {
	Variant     string `json:"variant"`
	Path        string `json:"path"`
	Identity    string `json:"identity"`
	KnownDefect string `json:"known_defect,omitzero"`
}

// Write creates all three case bundles in a new directory. family.json appears
// only after every case is complete. A failed write retains incomplete output
// without that completion record; an existing directory is never overwritten.
func Write(path string, inputs bundle.GeneratorInputs) (*Manifest, error) {
	path, err := artifactpath.Destination(path)
	if err != nil {
		return nil, err
	}
	inputs.BaseTime = inputs.BaseTime.UTC()
	variants, err := generate(inputs)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(path, 0700); err != nil {
		return nil, errors.New("cannot create synthetic family; destination must be new and parent writable")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errors.New("cannot open synthetic family; incomplete output retained")
	}
	defer root.Close()
	manifest := &Manifest{Schema: "readmit-synth/v1", State: "complete", Generator: inputs}
	for _, variant := range variants {
		b, err := bundle.Write(filepath.Join(path, variant.name), []bundle.Input{{
			Data: variant.data, Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
		}}, bundle.Provenance{Mode: bundle.Generated, Generator: &inputs})
		if err != nil {
			return nil, errors.New("cannot complete synthetic case bundle; incomplete family retained")
		}
		manifest.Cases = append(manifest.Cases, Case{Variant: variant.name, Path: variant.name, Identity: b.Identity, KnownDefect: variant.knownDefect})
	}
	data, err := json.Marshal(manifest, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode synthetic family; incomplete output retained")
	}
	if err := writeCompletion(root, append(data, '\n')); err != nil {
		return nil, err
	}
	return manifest, nil
}

func writeCompletion(root *os.Root, data []byte) error {
	failed := errors.New("cannot write synthetic family completion record; incomplete output retained")
	file, err := root.OpenFile(".family.json.incomplete", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return failed
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return failed
	}
	if err := root.Rename(".family.json.incomplete", "family.json"); err != nil {
		return failed
	}
	return nil
}
