package synth

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
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
	return WriteWithDurability(path, inputs, artifactdir.Durable)
}

// WriteWithDurability is Write with the durability its caller chose: Scratch
// only for a family in a throwaway workspace its owner removes before it
// answers. The family's bytes and case identities do not depend on it.
func WriteWithDurability(path string, inputs bundle.GeneratorInputs, durability artifactdir.Durability) (*Manifest, error) {
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
		b, err := bundle.WriteWithDurability(context.Background(), filepath.Join(path, variant.name), []bundle.Input{{
			Data: variant.data, Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
		}}, bundle.Provenance{Mode: bundle.Generated, Generator: &inputs}, durability)
		if err != nil {
			return nil, errors.New("cannot complete synthetic case bundle; incomplete family retained")
		}
		manifest.Cases = append(manifest.Cases, Case{Variant: variant.name, Path: variant.name, Identity: b.Identity, KnownDefect: variant.knownDefect})
	}
	data, err := json.Marshal(manifest, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode synthetic family; incomplete output retained")
	}
	if err := durability.Publish(root, ".family.json.incomplete", "family.json", append(data, '\n')); err != nil {
		return nil, errors.New("cannot write synthetic family completion record; incomplete output retained")
	}
	return manifest, nil
}
