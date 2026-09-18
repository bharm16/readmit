package durablerun

import "os"

// SyncDirectory is a no-op on Windows: fsync on a directory handle opened
// through os.Root is not supported there. File FlushFileBuffers is still
// required at every write boundary.
func SyncDirectory(root *os.Root, name string) error { return nil }
