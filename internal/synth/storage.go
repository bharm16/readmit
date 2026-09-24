package synth

import (
	"context"
	"encoding/json/v2"
	"errors"
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

// synthetic is the sealed directory of a synthetic family: one case bundle
// per variant, each sealed by itself, completed by family.json, which names
// their identities and is renamed into place only once written in full.
var synthetic = artifactdir.Family{
	Layout: artifactdir.Layout{
		Nested:    []string{"regression", "cancellation", "invalid"},
		AllowFile: func(name string) bool { return name == "family.json" },
	},
	Seal: artifactdir.CompletionRecord("family.json", ".family.json.incomplete"),
	Errors: artifactdir.Errors{
		Reserve:  errors.New("cannot create synthetic family; destination must be new and parent readable and writable"),
		Complete: errors.New("cannot write synthetic family completion record; incomplete output retained"),
		Sync:     errors.New("cannot sync synthetic family directory; the family was written in full but a power loss could still lose it"),
	},
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
	written, err := artifactdir.Create(path, synthetic, durability)
	if err != nil {
		return nil, err
	}
	defer written.Close()
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
	// Each case synced its own entry in the family. family.json and the
	// family's own entry in the folder holding it are synced before the family
	// is reported written; by then every file is, so a failure here leaves a
	// family that may open and says so.
	if _, err := written.Seal(append(data, '\n')); err != nil {
		return nil, err
	}
	return manifest, nil
}
