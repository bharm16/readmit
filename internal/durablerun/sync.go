package durablerun

import (
	"errors"
	"os"

	"github.com/bharm16/readmit/internal/artifactdir"
)

// SyncDirectory flushes one directory's own entries. Every durable artifact
// this release writes uses artifactdir's sync, so the platform rule is stated
// once, there: Windows exposes no directory flush through os.Root, and every
// file is still flushed at each write boundary.
func SyncDirectory(root *os.Root, name string) error {
	if err := artifactdir.SyncDirectory(root, name); err != nil {
		return errors.New("cannot sync durable evidence directory")
	}
	return nil
}
