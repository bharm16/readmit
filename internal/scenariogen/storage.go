package scenariogen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
)

// Manifest is the completion record. Intended arrivals are a generator plan,
// never observations, an executed delay or an assertion about a real target.
type Manifest struct {
	Schema  string   `json:"schema"`
	State   string   `json:"state"`
	Inputs  Plan     `json:"inputs"`
	Streams []Stream `json:"streams"`
}
type Stream struct {
	Row              string    `json:"row"`
	Variant          string    `json:"variant"`
	File             string    `json:"file"`
	SHA256           string    `json:"sha256"`
	Bytes            int       `json:"bytes"`
	IntendedArrivals []Arrival `json:"intended_arrivals"`
}

// Write validates and materializes all bounded streams before creating output.
// Completion is installed last. Cancellation or I/O failure retains incomplete
// output; retry requires a new destination, never an overwrite or partial resume.
func Write(ctx context.Context, path string, data []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := Decode(data)
	if err != nil {
		return nil, err
	}
	d, err := readTemplate(p.Template)
	if err != nil {
		return nil, err
	}
	manifest := Manifest{Schema: "readmit-scenario-generation/v1", State: "complete", Inputs: p, Streams: []Stream{}}
	streams := make([]stream, 0, len(p.Rows)*len(p.Variants))
	total := 0
	for _, r := range p.Rows {
		for _, v := range p.Variants {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			s, err := generate(p, d, r, v)
			if err != nil {
				return nil, err
			}
			total += len(s.data)
			if total > 16<<20 {
				return nil, errors.New("generated family exceeds 16 MiB")
			}
			sum := sha256.Sum256(s.data)
			manifest.Streams = append(manifest.Streams, Stream{Row: r.ID, Variant: v.ID, File: fmt.Sprintf("stream-%03d.mllp", len(streams)+1), SHA256: hex.EncodeToString(sum[:]), Bytes: len(s.data), IntendedArrivals: s.arrivals})
			streams = append(streams, s)
		}
	}
	encoded, err := json.Marshal(manifest, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode generation record")
	}
	encoded = append(encoded, '\n')
	path, err = artifactpath.Destination(path)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.Mkdir(path, 0700); err != nil {
		return nil, errors.New("generation destination must be new with an existing writable parent")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errors.New("cannot open generation output; incomplete output retained")
	}
	defer root.Close()
	for i, s := range streams {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := writeFile(root, manifest.Streams[i].File, s.data); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := writeFile(root, ".generation.json.incomplete", encoded); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := root.Rename(".generation.json.incomplete", "generation.json"); err != nil {
		return nil, errors.New("cannot finish generation record; incomplete output retained")
	}
	return encoded, nil
}
func writeFile(root *os.Root, name string, data []byte) error {
	err := artifactdir.WriteFile(root, name, data)
	if errors.Is(err, artifactdir.ErrCreateFile) {
		return errors.New("cannot create generation file; incomplete output retained")
	}
	if err != nil {
		return errors.New("cannot write generation file; incomplete output retained")
	}
	return nil
}
