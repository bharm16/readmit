package desktop

import (
	"encoding/json/v2"
	"errors"
	"io/fs"
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

// maxRecentBytes bounds the list this release reads; the store refuses a
// larger file before it is read into memory.
const maxRecentBytes = 1 << 16

const maxRootBytes = 4096

type recentList struct {
	Schema string   `json:"schema"`
	Roots  []string `json:"roots"`
}

// readRecent reads the recent workspace list out of the shell document store:
// the store's bounded, never-through-a-link read, then this document's
// decoder. It treats a missing list as an empty one and returns every other
// failure so the caller can separate permission from an unreadable contract.
func (a *App) readRecent() ([]string, error) {
	data, err := a.documents.read(recentName, maxRecentBytes)
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
	existing, err := a.readRecent()
	if err != nil {
		return
	}
	roots := append([]string{root}, slices.DeleteFunc(existing, func(entry string) bool { return entry == root })...)
	if len(roots) > MaxRecentWorkspaces {
		roots = roots[:MaxRecentWorkspaces]
	}
	a.writeRecentList(roots)
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
	if err := a.writeRecentList(remaining); errors.Is(err, fs.ErrPermission) {
		listed.State, listed.Reason = PermissionDenied, "this account cannot write the recent workspace list"
		return listed
	} else if err != nil {
		listed.State, listed.Reason = Failed, "the recent workspace list could not be replaced; it is left as it was"
		return listed
	}
	return a.RecentWorkspaces()
}

// writeRecentList installs a complete list or leaves the previous one in
// place, through the shell document store, exactly as every other shell
// document is replaced: a reader never observes a partially written list, an
// interrupted replacement is reported rather than reused, and the folder
// naming the list is synced before the write answers. A failure is returned
// as the store reported it, so a caller can tell a list this account cannot
// write from any other failure; recording a folder ignores it.
func (a *App) writeRecentList(roots []string) error {
	data, err := json.Marshal(recentList{Schema: RecentSchema, Roots: roots}, json.Deterministic(true))
	if err != nil {
		return err
	}
	return a.documents.write(recentName, append(data, '\n'))
}
