package hub

import (
	"os"

	"github.com/bharm16/readmit/internal/artifactdir"
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
	return privatePolicy(1<<20, errAccess).Read(path)
}

// readPrivatePolicyRoot reads one bounded owner-only policy document from
// below root. A document wider than limit is refused rather than truncated.
func readPrivatePolicyRoot(root *os.Root, name string, limit int64) ([]byte, error) {
	return privatePolicy(int(limit), ErrSchedule).ReadIn(root, name)
}

// privatePolicy is the rule both forms hold, through the shared document
// store: a regular file no wider than its bound that no one but the operator
// can read, never through a link, refused with refusal.
func privatePolicy(limit int, refusal error) artifactdir.Document {
	return artifactdir.Document{
		MaxBytes:  limit,
		OwnerOnly: true,
		Refusals:  artifactdir.DocumentRefusals{Irregular: refusal, Read: refusal},
	}
}
