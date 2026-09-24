package desktop

import (
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// RecentSchema is the versioned contract of the recent workspace list. The list
// is local shell state, never evidence: it holds folder paths only, never
// message content, field values, identifiers, or anything read out of a case.
// It stays on this machine and is never sent anywhere.
const RecentSchema = "readmit-desktop-recent/v1"

// MaxRecentWorkspaces bounds the list the shell keeps and rereads.
const MaxRecentWorkspaces = 10

const (
	maxRecentBytes = 1 << 16
	maxRootBytes   = 4096
)

type recentList struct {
	Schema string   `json:"schema"`
	Roots  []string `json:"roots"`
}

// DefaultRecentPath is the owner-only file the shell keeps the list in.
func DefaultRecentPath() (string, error) { return configPath("recent.json") }

// configPath locates one file of local shell state. Every file the shell keeps
// lives beside the others under one owner-only directory, and none is derived
// from another: each is named explicitly where the application is wired up.
func configPath(name string) (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", errors.New("cannot resolve the user configuration directory")
	}
	return filepath.Join(directory, "readmit", name), nil
}

// readRecent treats a missing list as an empty one and returns every other
// failure so the caller can separate permission from an unreadable contract.
func readRecent(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeRecent(data)
}

// decodeRecent rejects unknown versions and members outright; there is no
// migration and no repair. A list this release cannot read stays as written.
func decodeRecent(data []byte) ([]string, error) {
	if len(data) > maxRecentBytes {
		return nil, errors.New("recent workspace list exceeds its size limit")
	}
	var list recentList
	if err := json.Unmarshal(data, &list, json.RejectUnknownMembers(true)); err != nil {
		return nil, errors.New("invalid recent workspace list")
	}
	if list.Schema != RecentSchema {
		return nil, errors.New("unsupported recent workspace list version")
	}
	if len(list.Roots) > MaxRecentWorkspaces {
		return nil, errors.New("recent workspace list exceeds its entry limit")
	}
	seen := make(map[string]bool, len(list.Roots))
	for _, root := range list.Roots {
		if !filepath.IsAbs(root) || len(root) > maxRootBytes || seen[root] {
			return nil, errors.New("invalid recent workspace entry")
		}
		seen[root] = true
	}
	return list.Roots, nil
}

// recordRecent moves root to the front of the bounded list. A list this release
// cannot read is never replaced: RecentWorkspaces reports the same refusal, so
// the person keeps whatever wrote it.
func (a *App) recordRecent(root string) {
	existing, err := readRecent(a.recentPath)
	if err != nil {
		return
	}
	roots := append([]string{root}, slices.DeleteFunc(existing, func(entry string) bool { return entry == root })...)
	if len(roots) > MaxRecentWorkspaces {
		roots = roots[:MaxRecentWorkspaces]
	}
	writeRecent(a.recentPath, roots)
}

// ForgetWorkspace removes one folder from the recent workspace list and
// reports the list as it now stands. Only the entry is forgotten: the folder
// and everything in it stay exactly where they are, and opening it again
// records it again. A folder the list no longer holds is refused rather than
// forgotten quietly, so a window showing a list that has changed since it was
// read says so and shows the list as it is now. A list this release cannot
// read is reported and never replaced.
//
// It writes one small local file and runs to completion once it starts, so it
// holds the operation slot but is not interruptible.
func (a *App) ForgetWorkspace(root string) RecentResult {
	release, claimed := a.claim("")
	if !claimed {
		result := a.RecentWorkspaces()
		result.State, result.Reason = busyRefusal.state, busyRefusal.reason
		return result
	}
	defer release()
	listed := a.RecentWorkspaces()
	if listed.State != Completed && listed.State != Empty {
		return listed
	}
	if !slices.Contains(listed.Roots, root) {
		listed.State, listed.Reason = Failed, "that folder is not in the recent workspace list any more"
		return listed
	}
	remaining := slices.DeleteFunc(slices.Clone(listed.Roots), func(entry string) bool { return entry == root })
	if err := writeRecent(a.recentPath, remaining); errors.Is(err, fs.ErrPermission) {
		listed.State, listed.Reason = PermissionDenied, "this account cannot write the recent workspace list"
		return listed
	} else if err != nil {
		listed.State, listed.Reason = Failed, "the recent workspace list could not be replaced; it is left as it was"
		return listed
	}
	return a.RecentWorkspaces()
}

// writeRecent installs a complete list or leaves the previous one in place. A
// reader never observes a partially written list. A failure is returned as the
// filesystem reported it, so a caller can tell a list this account cannot
// write from any other failure; recording a folder ignores it.
func writeRecent(path string, roots []string) error {
	data, err := json.Marshal(recentList{Schema: RecentSchema, Roots: roots}, json.Deterministic(true))
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".recent-*.incomplete")
	if err != nil {
		return err
	}
	incomplete := file.Name()
	_, err = file.Write(append(data, '\n'))
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(incomplete, path)
	}
	if err != nil {
		os.Remove(incomplete)
	}
	return err
}
