package durablerun

import "os"

// Windows does not support fsync on directory handles opened through os.Root.
// File FlushFileBuffers is still required at every write boundary.
func syncDirectory(root *os.Root, name string) error { return nil }
