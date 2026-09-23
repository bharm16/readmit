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

// EntryName reports whether name is a single local element: never empty, ".",
// "..", a path with a separator, an absolute path, or a reserved device name.
// Child enforces it against the filesystem; a caller holding only a recorded
// name checks it here, so the rule has one owner rather than two.
func EntryName(name string) error {
	if name == "." || !filepath.IsLocal(name) || filepath.Base(name) != name {
		return errors.New("artifact entry must be one name inside the directory")
	}
	return nil
}

// Child resolves one named entry of an already-resolved artifact directory.
// The name must satisfy EntryName, and the entry itself must be a real
// directory, never a symbolic link, so a listing cannot be used to reach
// evidence outside the directory the person opened.
func Child(directory, name string) (string, error) {
	if err := EntryName(name); err != nil {
		return "", err
	}
	path := JoinReference(directory, name)
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("artifact entry must be a regular directory")
	}
	return path, nil
}

// File resolves one named document entry of an already-resolved artifact
// directory. The name must satisfy EntryName, and the entry itself must be a
// regular file, never a symbolic link wherever it points, a folder or a
// device, so a name cannot be used to read anything but the one file the
// directory holds under it. It is Child's rule for a document.
func File(directory, name string) (string, error) {
	if err := EntryName(name); err != nil {
		return "", err
	}
	path := JoinReference(directory, name)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("artifact entry must be a regular file")
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
