//go:build linux

package desktop

import "testing"

// The bound is checked against what was read, not the size the entry
// reported before it was opened: a regular file that reports no size and
// holds more than the bound, as a kernel status file does, is refused rather
// than handed back one byte past it (#473).
func TestReadBoundedEntryChecksWhatItReadNotTheReportedSize(t *testing.T) {
	if data, err := readBoundedEntry("/proc/self/status", 16); err == nil {
		t.Fatalf("an entry that reported no size was read as %d bytes past a 16-byte bound", len(data))
	}
}
