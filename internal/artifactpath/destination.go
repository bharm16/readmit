// Package artifactpath owns physical path resolution and protection of finalized
// evidence. Callers must use the returned path for the actual filesystem access.
package artifactpath

import (
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Destination resolves a new file or directory outside finalized evidence and
// any explicitly protected source directories. Creation remains exclusive at
// the writer; checking a name here never authorizes overwriting it later.
// Resolve the raw parent before lexical cleaning: alias/../out must traverse
// the symlink before .., exactly as the filesystem does when creating it.
func Destination(destination string, protected ...os.FileInfo) (string, error) {
	for len(destination) > 0 && os.IsPathSeparator(destination[len(destination)-1]) {
		destination = destination[:len(destination)-1]
	}
	parent, leaf := filepath.Split(destination)
	if leaf == "" || leaf == "." || leaf == ".." {
		return "", errors.New("artifact destination must name a new file or directory")
	}
	if parent == "" {
		parent = "."
	}
	parent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", errors.New("cannot resolve artifact output parent directory")
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return "", errors.New("cannot resolve artifact output directory")
	}
	for current := parent; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err != nil || !info.IsDir() {
			return "", errors.New("cannot inspect artifact output parent directory")
		}
		for _, source := range protected {
			if source != nil && source.IsDir() && os.SameFile(source, info) {
				return "", errors.New("output must be outside the immutable input case")
			}
		}
		if evidenceDirectory(current) {
			return "", errors.New("output must be outside the immutable input case or enclosing evidence")
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return filepath.Join(parent, leaf), nil
}

func evidenceDirectory(path string) bool {
	for _, marker := range []string{"identity.sha256", "family.json"} {
		if _, err := os.Lstat(filepath.Join(path, marker)); !os.IsNotExist(err) {
			return true
		}
	}
	// A missing completion marker does not make retained case/run/result
	// evidence writable. Preserve the former diff protection of raw payloads
	// selected from incomplete artifacts, without opening pipes or devices.
	for _, name := range []string{"manifest.json", "result.json"} {
		name = filepath.Join(path, name)
		info, err := os.Stat(name)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
			continue
		}
		file, err := os.Open(name)
		if err != nil {
			continue
		}
		opened, err := file.Stat()
		if err != nil || !opened.Mode().IsRegular() {
			file.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
		file.Close()
		var header struct {
			Schema string `json:"schema"`
		}
		if err == nil && len(data) <= 16<<20 && json.Unmarshal(data, &header) == nil {
			if IsEvidenceSchema(header.Schema) {
				return true
			}
		}
	}
	return false
}
