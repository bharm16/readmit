//go:build !readmit_nosync

package artifactdir

import "os"

// flushToDevice flushes one real file, or one directory naming files, to the
// device. Every sync artifactdir makes of a real file ends here.
func flushToDevice(file *os.File) error { return file.Sync() }
