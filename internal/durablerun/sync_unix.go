//go:build !windows

package durablerun

import (
	"errors"
	"os"
)

// SyncDirectory flushes one directory's own entries. Every durable artifact
// this release writes uses it, so the platform rule below is stated once.
func SyncDirectory(root *os.Root, name string) error {
	f, err := root.Open(name)
	if err != nil {
		return errors.New("cannot open durable evidence directory for sync")
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return errors.New("cannot sync durable evidence directory")
	}
	return nil
}
