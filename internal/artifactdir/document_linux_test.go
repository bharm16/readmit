//go:build linux

package artifactdir_test

import (
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
)

// What a read holds is checked against the bound, not only the size the file
// says it is: a kernel status file says it is empty and holds more.
func TestReadChecksWhatItReadAgainstTheBound(t *testing.T) {
	document := exampleDocument()
	document.Links = artifactdir.FollowLinks
	if data, err := document.Read("/proc/self/status"); err != errDocumentSize {
		t.Fatalf("a file that says it is empty was read as %d bytes past its bound: %v", len(data), err)
	}
}
