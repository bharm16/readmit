//go:build windows

package artifactdir

import "os"

// Windows does not expose directory fsync through os.File.Sync. Every member
// is flushed before its handle closes; the identity marker remains last.
func flushDirectory(*os.Root, string) error { return nil }
