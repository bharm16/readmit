//go:build !windows

package durablerun

import (
	"errors"
	"os"

	"github.com/bharm16/readmit/internal/artifactdir"
)

// SyncDirectory flushes one directory's own entries. Every durable artifact
// this release writes uses it, so the platform rule is stated once, in
// artifactdir.
func SyncDirectory(root *os.Root, name string) error {
	if err := artifactdir.SyncDirectory(root, name); err != nil {
		return errors.New("cannot sync durable evidence directory")
	}
	return nil
}
