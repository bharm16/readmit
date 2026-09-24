package profileversion

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/localprofile"
)

// VerifyFolder checks that saving a profile into one folder keeps every
// version sealed there true: each version seal among the folder's own regular
// *.json entries that records this profile's id and version must verify it,
// so a version sealed over one set of rules is never saved beside that seal
// with another.
//
// A folder that cannot be listed is refused, because a check that could not
// look has not found the version unsealed. Anything among its entries that is
// not a seal of this version is passed over: another document, a seal of
// another version or profile, a member this account cannot read or that is
// longer than a seal may be, a subdirectory and a symbolic link, which is never
// read through. A folder of the folder is not looked at.
func VerifyFolder(directory string, profile localprofile.Profile) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return errors.New("cannot read the folder the profile is saved into, so whether version " + profile.Identity.Version + " is already sealed cannot be checked")
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := readSeal(filepath.Join(directory, entry.Name()))
		if err != nil {
			continue
		}
		sealed, err := DecodeVersion(data)
		if err != nil || sealed.Profile != profile.Identity {
			continue
		}
		if sealed.Verify(profile) != nil {
			return errors.New("profile version " + profile.Identity.Version + " is already sealed with different content; increment version to save changes")
		}
	}
	return nil
}

// readSeal reads one folder member under the seal contract's own size limit,
// through the shared document store: a member that is not a regular file, or
// says it is longer than a seal may be, is passed over before its bytes are
// held in memory.
func readSeal(path string) ([]byte, error) {
	return sealFile.Read(path)
}

// errNotASeal is a folder member that is not a version seal this release
// reads.
var errNotASeal = errors.New("not a version seal this release reads")

// sealFile is how a version seal in a folder is read, through a link at its
// name. A member that cannot be opened reports the filesystem's own error.
var sealFile = artifactdir.Document{
	MaxBytes: MaxVersionBytes,
	Links:    artifactdir.FollowLinks,
	Refusals: artifactdir.DocumentRefusals{
		Inspect:   artifactdir.FilesystemReport,
		Irregular: errNotASeal,
		Open:      artifactdir.FilesystemReport,
		Read:      errNotASeal,
	},
}
