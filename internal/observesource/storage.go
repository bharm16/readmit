package observesource

import (
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// DecodeSource reads one declared observation source exactly as written.
// Unknown members and unknown versions are errors; there is no migration and no
// repair, and a diagnostic names the declaration at fault without repeating the
// value that failed.
func DecodeSource(data []byte) (Source, error) {
	if len(data) > MaxSourceBytes {
		return Source{}, errors.New("observation source exceeds size limit")
	}
	var source Source
	if err := json.Unmarshal(data, &source); err != nil {
		if errors.Is(err, ErrUnsupportedVersion) {
			return Source{}, ErrUnsupportedVersion
		}
		return Source{}, errors.New("invalid observation source JSON")
	}
	if err := source.Validate(); err != nil {
		return Source{}, err
	}
	return source, nil
}

// ReadSource opens one declared observation source. Like every other input
// reader here it accepts a regular file only, and the export and certificate
// authority it names are resolved against the directory the document itself
// lives in, never the caller's working directory: the declared form is what the
// file holds, and this is what reading it means.
func ReadSource(path string) (Source, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return Source{}, errors.New("an observation source must be a readable regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Source{}, errors.New("cannot open observation source file")
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Source{}, errors.New("an observation source must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(MaxSourceBytes)+1))
	if err != nil {
		return Source{}, errors.New("cannot read observation source file")
	}
	if len(data) > MaxSourceBytes {
		return Source{}, errors.New("observation source exceeds size limit")
	}
	source, err := DecodeSource(data)
	if err != nil {
		return Source{}, err
	}
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return Source{}, errors.New("cannot resolve the observation source document")
	}
	return anchor(source, filepath.Dir(resolved)), nil
}

// anchor resolves the paths a source declares against the directory the
// document itself lives in. It preserves raw traversal rather than cleaning it
// away, so a reference through a symbolic link names what the filesystem names.
func anchor(source Source, directory string) Source {
	if source.File != nil && source.File.Path != "" {
		export := *source.File
		export.Path = artifactpath.JoinReference(directory, export.Path)
		source.File = &export
	}
	if source.HTTP != nil && source.HTTP.CAFile != "" {
		endpoint := *source.HTTP
		endpoint.CAFile = artifactpath.JoinReference(directory, endpoint.CAFile)
		source.HTTP = &endpoint
	}
	return source
}
