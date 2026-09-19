// Package expectation releases reviewed test templates with exact profile pins.
// Baseline owns specification comparisons and the local approval record. This
// envelope binds that record to a test identity, profile seals and release history.
package expectation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"os"
	"regexp"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/profileversion"
)

const Schema = "readmit-test-release/v1"
const MaxBytes = 3 << 20

// Release is sensitive local data, not proof of behavior or authenticated identity.
// Revision numbers belong to the supplied chain; Identity disambiguates forks.
type Release struct {
	Schema   string                   `json:"schema"`
	ID       string                   `json:"id"`
	Parent   string                   `json:"parent"`
	Profiles []profileversion.Version `json:"profiles"`
	Baseline baseline.Revision        `json:"baseline"`
	Review   string                   `json:"review"`
}
type Comparison struct {
	Schema   string              `json:"schema"`
	Identity string              `json:"identity"`
	ID       string              `json:"id"`
	Revision int                 `json:"revision"`
	Parent   string              `json:"parent"`
	Baseline baseline.Comparison `json:"baseline"`
	Profiles []baseline.Change   `json:"profiles"`
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func canonical(v any) []byte { b, _ := json.Marshal(v, json.Deterministic(true)); return b }
func digest(v any) string    { h := sha256.Sum256(canonical(v)); return hex.EncodeToString(h[:]) }
func isDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func commitment(id, parent string, pins []profileversion.Version, review string) string {
	return digest(struct {
		Schema, ID, Parent, Review string
		Profiles                   []profileversion.Version
	}{"readmit-expectation-review/v1", id, parent, review, pins})
}
func ordered(pins []profileversion.Version) ([]profileversion.Version, error) {
	if pins == nil || len(pins) > 32 {
		return nil, errors.New("profiles must be an explicit list of at most 32 seals")
	}
	out := slices.Clone(pins)
	slices.SortFunc(out, func(a, b profileversion.Version) int {
		if a.Profile.ID < b.Profile.ID {
			return -1
		}
		if a.Profile.ID > b.Profile.ID {
			return 1
		}
		return 0
	})
	for i, p := range out {
		if p.Validate() != nil || i > 0 && out[i-1].Profile.ID == p.Profile.ID {
			return nil, errors.New("invalid or duplicate profile pin")
		}
	}
	return out, nil
}
func (r Release) Validate() error {
	pins, err := ordered(r.Profiles)
	if err != nil {
		return err
	}
	if r.Schema != Schema || !identifier.MatchString(r.ID) {
		return errors.New("invalid release schema or test identity")
	}
	if err := r.Baseline.Validate(); err != nil {
		return err
	}
	if r.Baseline.Revision == 1 && r.Parent != "" || r.Baseline.Revision > 1 && !isDigest(r.Parent) {
		return errors.New("invalid release predecessor")
	}
	if string(canonical(pins)) != string(canonical(r.Profiles)) {
		return errors.New("release profiles must be ordered by identity")
	}
	if r.Review != commitment(r.ID, r.Parent, r.Profiles, r.Baseline.Review) {
		return errors.New("changed expectation review commitment")
	}
	return nil
}
func (r Release) Identity() string { return digest(r) }
func (r Release) Encode() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	data := canonical(r)
	if len(data) >= MaxBytes {
		return nil, errors.New("release exceeds size limit")
	}
	return append(data, '\n'), nil
}
func Decode(data []byte) (Release, error) {
	var r Release
	var required struct {
		Schema   *string           `json:"schema"`
		ID       *string           `json:"id"`
		Parent   *string           `json:"parent"`
		Profiles *[]jsontext.Value `json:"profiles"`
		Baseline jsontext.Value    `json:"baseline"`
		Review   *string           `json:"review"`
	}
	if len(data) > MaxBytes || json.Unmarshal(data, &required) != nil || required.Schema == nil || required.ID == nil || required.Parent == nil || required.Profiles == nil || len(required.Baseline) == 0 || required.Review == nil || json.Unmarshal(data, &r, json.RejectUnknownMembers(true)) != nil {
		return Release{}, errors.New("invalid release JSON or missing required member")
	}
	b, err := baseline.Decode(required.Baseline)
	if err != nil {
		return Release{}, err
	}
	r.Baseline = b
	r.Profiles = []profileversion.Version{}
	for _, raw := range *required.Profiles {
		p, e := profileversion.DecodeVersion(raw)
		if e != nil {
			return Release{}, errors.New("invalid released profile seal")
		}
		r.Profiles = append(r.Profiles, p)
	}
	return r, r.Validate()
}
func Read(path string) (Release, error) {
	raw, err := baseline.ReadBytes(path, MaxBytes)
	if err != nil {
		return Release{}, err
	}
	return Decode(raw)
}
func Review(id string, data []byte, pins []profileversion.Version, previous *Release, show bool) (Comparison, error) {
	if !identifier.MatchString(id) {
		return Comparison{}, errors.New("test identity must be a lowercase identifier")
	}
	sorted, err := ordered(pins)
	if err != nil {
		return Comparison{}, err
	}
	var parent *baseline.Revision
	previousID := ""
	if previous != nil {
		if previous.Validate() != nil || previous.ID != id {
			return Comparison{}, errors.New("invalid previous release or different test identity")
		}
		parent = &previous.Baseline
		previousID = previous.Identity()
		for _, p := range sorted {
			for _, old := range previous.Profiles {
				if old.Profile == p.Profile && old.Content != p.Content {
					return Comparison{}, errors.New("profile changed under its existing version")
				}
			}
		}
	}
	b, err := baseline.Review(data, parent, show)
	if err != nil {
		return Comparison{}, err
	}
	changes := []baseline.Change{}
	old := map[string]profileversion.Version{}
	if previous != nil {
		for _, p := range previous.Profiles {
			old[p.Profile.ID] = p
		}
	}
	for _, p := range sorted {
		prior, found := old[p.Profile.ID]
		delete(old, p.Profile.ID)
		if found && prior == p {
			continue
		}
		c := baseline.Change{Part: "profile:" + p.Profile.ID, Kind: "changed"}
		if !found {
			c.Kind = "added"
		}
		if show {
			c.After = string(canonical(p))
			if found {
				c.Before = string(canonical(prior))
			}
		}
		changes = append(changes, c)
	}
	if previous != nil {
		for _, p := range previous.Profiles {
			if _, found := old[p.Profile.ID]; found {
				c := baseline.Change{Part: "profile:" + p.Profile.ID, Kind: "removed"}
				if show {
					c.Before = string(canonical(p))
				}
				changes = append(changes, c)
			}
		}
	}
	return Comparison{Schema: "readmit-expectation-review/v1", Identity: commitment(id, previousID, sorted, b.Identity), ID: id, Revision: b.Revision, Parent: previousID, Baseline: b, Profiles: changes}, nil
}
func Approve(id string, data []byte, pins []profileversion.Version, previous *Release, review, approver, rationale string) (Release, error) {
	c, err := Review(id, data, pins, previous, false)
	if err != nil {
		return Release{}, err
	}
	if review == "" || review != c.Identity {
		return Release{}, errors.New("expectations, profiles or parent changed; review again")
	}
	var parent *baseline.Revision
	if previous != nil {
		parent = &previous.Baseline
	}
	b, err := baseline.Approve(data, parent, c.Baseline.Identity, approver, rationale)
	if err != nil {
		return Release{}, err
	}
	sorted, _ := ordered(pins)
	r := Release{Schema: Schema, ID: id, Parent: c.Parent, Profiles: sorted, Baseline: b, Review: review}
	return r, r.Validate()
}
func Save(path string, r Release) error {
	raw, err := r.Encode()
	if err != nil {
		return err
	}
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("release destination must be new and writable")
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err != nil || closed != nil {
		return errors.New("cannot finish release; incomplete file retained")
	}
	return nil
}

// Inspect retains baseline privacy behavior and includes every pinned profile.
// Its identity names the stored release, not a new approval proposal.
func Inspect(r Release, show bool) (baseline.Comparison, error) {
	if err := r.Validate(); err != nil {
		return baseline.Comparison{}, err
	}
	report, err := baseline.Inspect(r.Baseline, show)
	if err != nil {
		return report, err
	}
	report.Schema = "readmit-expectation-inspection/v1"
	report.Identity = r.Identity()
	report.Parent = r.Parent
	for _, p := range r.Profiles {
		c := baseline.Change{Part: "profile:" + p.Profile.ID, Kind: "pinned"}
		if show {
			c.After = string(canonical(p))
		}
		report.Changes = append(report.Changes, c)
	}
	return report, nil
}
