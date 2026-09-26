//go:build readmit_nosync

package artifactdir

import "os"

// flushToDevice skips the device flush in a test build, where a power loss
// cannot happen and the flush is most of a slow test's time. The bytes, the
// exclusive creates and the renames are unchanged, and the sync seams the
// package tests take are still called. A release is never built with this tag.
func flushToDevice(*os.File) error { return nil }
