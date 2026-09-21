package hub

import (
	"io"
	"os"
)

// One private-strict-file reader serves every authority document the store
// loads: the access policy, the operation policy, the runner policy and the
// schedule documents. The path form reads from a filesystem path; the root
// form reads from below an open root. Both hold the same rule — a regular
// file no wider than its bound that no one but the operator can read, opened
// and confirmed to be the same file that was stat'd, so a policy cannot be
// swapped or widened between the check and the read.

// readPrivatePolicy reads one bounded owner-only policy document from a path.
// The bound is the 1 MiB every root policy document shares.
func readPrivatePolicy(path string) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 1<<20 {
		return nil, errAccess
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, errAccess
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return nil, errAccess
	}
	b, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil {
		return nil, errAccess
	}
	return b, nil
}

// readPrivatePolicyRoot reads one bounded owner-only policy document from
// below root. A document wider than limit is refused rather than truncated.
func readPrivatePolicyRoot(root *os.Root, name string, limit int64) ([]byte, error) {
	info, e := root.Lstat(name)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > limit {
		return nil, ErrSchedule
	}
	f, e := root.Open(name)
	if e != nil {
		return nil, ErrSchedule
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return nil, ErrSchedule
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, ErrSchedule
	}
	return b, nil
}
