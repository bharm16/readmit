package profileversion

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

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

// readSeal reads one folder member under the seal contract's own size limit.
// The file is opened first and measured through that open file, so a member
// that is not a regular file, or is longer than a seal may be, is passed over
// before its bytes are held in memory.
func readSeal(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxVersionBytes {
		return nil, errors.New("not a version seal this release reads")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxVersionBytes+1))
	if err != nil || len(data) > MaxVersionBytes {
		return nil, errors.New("not a version seal this release reads")
	}
	return data, nil
}
