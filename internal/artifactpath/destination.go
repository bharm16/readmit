// Package artifactpath resolves new artifact destinations without allowing a
// derived artifact to be created inside its immutable source directory.
package artifactpath

import (
	"errors"
	"os"
	"path/filepath"
)

// Outside returns the resolved destination that the writer must actually use.
// Resolve the raw parent before lexical cleaning: alias/../out must traverse
// the symlink before .., exactly as the filesystem does when creating it.
func Outside(source os.FileInfo, destination string) (string, error) {
	for len(destination) > 0 && os.IsPathSeparator(destination[len(destination)-1]) {
		destination = destination[:len(destination)-1]
	}
	parent, leaf := filepath.Split(destination)
	if source == nil || !source.IsDir() || leaf == "" || leaf == "." || leaf == ".." {
		return "", errors.New("artifact destination must name a new directory")
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
		if os.SameFile(source, info) {
			return "", errors.New("output must be outside the immutable input case")
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return filepath.Join(parent, leaf), nil
}
