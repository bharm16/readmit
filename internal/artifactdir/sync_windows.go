//go:build windows

package artifactdir

// Windows does not expose directory fsync through os.File.Sync. Every member
// is flushed before its handle closes; the identity marker remains last.
func syncDirectory(string) error { return nil }
