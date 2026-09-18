package artifactpath

import (
	"errors"
	"os"
	"path/filepath"
)

// Resolve follows raw filesystem traversal before any lexical joins. In
// particular, alias/.. names the alias target's parent, not its lexical parent.
func Resolve(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errors.New("cannot resolve artifact input")
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", errors.New("cannot resolve artifact input")
	}
	return resolved, nil
}

// Directory preserves the stricter root contract of result, review, and report
// readers: parent aliases are allowed, but the named root cannot be a symlink.
func Directory(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("artifact must be a regular directory")
	}
	return Resolve(path)
}

// JoinReference anchors a declared relative path without cleaning away raw
// symlink/.. traversal. Resolve or access the result before any lexical joins.
func JoinReference(directory, reference string) string {
	if filepath.IsAbs(reference) {
		return reference
	}
	return directory + string(os.PathSeparator) + reference
}
