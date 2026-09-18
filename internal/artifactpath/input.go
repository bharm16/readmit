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

// Child resolves one named entry of an already-resolved artifact directory.
// The name must be a single local element: never empty, ".", "..", a path with
// a separator, an absolute path, or a reserved device name. The entry itself
// must be a real directory, never a symbolic link, so a listing cannot be used
// to reach evidence outside the directory the person opened.
func Child(directory, name string) (string, error) {
	if name == "." || !filepath.IsLocal(name) || filepath.Base(name) != name {
		return "", errors.New("artifact entry must be one name inside the directory")
	}
	path := JoinReference(directory, name)
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("artifact entry must be a regular directory")
	}
	return path, nil
}

// JoinReference anchors a declared relative path without cleaning away raw
// symlink/.. traversal. Resolve or access the result before any lexical joins.
func JoinReference(directory, reference string) string {
	if filepath.IsAbs(reference) {
		return reference
	}
	return directory + string(os.PathSeparator) + reference
}
