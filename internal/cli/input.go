package cli

import (
	"github.com/bharm16/readmit/internal/operation"
)

// readInputFile keeps input limits and private diagnostics shared by commands.
// It is the shared operation's reader, so the desktop facade refuses an input
// for the same reasons.
func readInputFile(path string, limit int) ([]byte, error) {
	return operation.ReadInputFile(path, limit)
}

// writeNewFile creates one new file exclusively at a reserved destination
// through the shared operation's writer, so a file the desktop facade writes
// is created and refused exactly as the command line's is.
func writeNewFile(path string, data []byte, cannotCreate, cannotWrite string) error {
	return operation.WriteNewFile(path, data, cannotCreate, cannotWrite)
}
