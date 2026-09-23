package artifactdir

import (
	"os"
	"path/filepath"
)

// ObserveDirectorySyncsForTest calls observe with each directory a write
// started afterwards syncs, just before it syncs it. An error from observe
// stands in for a failed sync. The returned function restores the real sync.
func ObserveDirectorySyncsForTest(observe func(directory string) error) (restore func()) {
	original := syncDirectory
	syncDirectory = func(root *os.Root, name string) error {
		if err := observe(filepath.Join(root.Name(), filepath.FromSlash(name))); err != nil {
			return err
		}
		return original(root, name)
	}
	return func() { syncDirectory = original }
}

// ObserveFileSyncsForTest calls observe with the path of each file a WriteFile
// or Durability.Sync started afterwards syncs, just before it syncs it. An
// error from observe stands in for a failed sync. The returned function
// restores the real sync.
func ObserveFileSyncsForTest(observe func(path string) error) (restore func()) {
	original := syncFile
	syncFile = func(file *os.File) error {
		if err := observe(file.Name()); err != nil {
			return err
		}
		return original(file)
	}
	return func() { syncFile = original }
}
