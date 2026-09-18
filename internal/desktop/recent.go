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

// configPath locates one file of local shell state. Both files the shell keeps
// live beside each other under one owner-only directory, and neither is derived
// from the other: each is named explicitly where the application is wired up.
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

// writeRecent installs a complete list or leaves the previous one in place. A
// reader never observes a partially written list.
func writeRecent(path string, roots []string) {
	data, err := json.Marshal(recentList{Schema: RecentSchema, Roots: roots}, json.Deterministic(true))
	if err != nil {
		return
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return
	}
	file, err := os.CreateTemp(directory, ".recent-*.incomplete")
	if err != nil {
		return
	}
	incomplete := file.Name()
	_, err = file.Write(append(data, '\n'))
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err != nil || closeErr != nil || os.Rename(incomplete, path) != nil {
		os.Remove(incomplete)
	}
}
