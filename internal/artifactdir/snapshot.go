package artifactdir

import (
	"bytes"
	"errors"
	"io/fs"
	"path"
)

// Snapshot checks a captured file map against the reader's layout and returns
// detached bytes. It establishes no filesystem provenance; Read remains the
// adapter that rejects links, special files and changes during acquisition.
func Snapshot(files map[string][]byte, layout Layout) (map[string][]byte, error) {
	refuse := func(declared error, fallback string) error {
		if declared != nil {
			return declared
		}
		return errors.New(fallback)
	}
	if layout.MaxFiles <= 0 || len(files) > layout.MaxFiles {
		return nil, refuse(layout.Refusals.Files, "artifact file limit exceeded")
	}
	total := 0
	for name, data := range files {
		if !fs.ValidPath(name) || name == "." || !layout.admitsFile(name) {
			return nil, errors.New("unexpected artifact file")
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if !layout.admitsDirectory(parent) {
				return nil, errors.New("unexpected artifact directory")
			}
		}
		if len(data) > layout.MaxFileBytes || len(data) > layout.MaxBytes-total {
			return nil, refuse(layout.Refusals.Size, "artifact contents exceed size limit")
		}
		total += len(data)
	}
	for _, name := range layout.RequiredFiles {
		if _, ok := files[name]; !ok {
			return nil, errors.New("required artifact file is missing")
		}
	}
	snapshot := make(map[string][]byte, len(files))
	for name, data := range files {
		snapshot[name] = bytes.Clone(data)
	}
	return snapshot, nil
}
