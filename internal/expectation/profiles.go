package expectation

import (
	"errors"
	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileversion"
)

// ReadProfiles seals the actual local profile documents selected for this
// review. It never resolves a newer version or interprets profile rules.
func ReadProfiles(paths []string) ([]profileversion.Version, error) {
	if len(paths) > 32 {
		return nil, errors.New("at most 32 profiles may be pinned")
	}
	out := []profileversion.Version{}
	for _, path := range paths {
		raw, err := baseline.ReadBytes(path, localprofile.MaxProfileBytes)
		if err != nil {
			return nil, err
		}
		p, err := localprofile.Decode(raw)
		if err != nil {
			return nil, errors.New("invalid selected local profile")
		}
		pin, err := profileversion.Seal(p)
		if err != nil {
			return nil, errors.New("cannot seal selected local profile")
		}
		out = append(out, pin)
	}
	return ordered(out)
}
