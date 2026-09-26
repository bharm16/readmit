//go:build !windows

package artifactdir

import "os"

// flushDirectory syncs a directory entry below root, so the names of the files
// just written become visible after a crash.
func flushDirectory(root *os.Root, name string) error {
	directory, err := root.Open(name)
	if err != nil {
		return err
	}
	defer directory.Close()
	return flushToDevice(directory)
}
