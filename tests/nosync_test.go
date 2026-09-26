//go:build readmit_nosync

package tests

// A test binary built without device flushes builds the command line it runs
// the same way, so the tests' own writes and the command's match.
func init() { cliBuildTags = []string{"-tags", "readmit_nosync"} }
