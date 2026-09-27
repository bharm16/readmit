package desktop

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/operation"
)

// maxIndexNames bounds how many collision-safe names SaveSearchSettings tries
// beside a case before it refuses to choose one.
const maxIndexNames = 100

// SearchSettingsRequest declares what the persistent search index of one case
// retains: the fields, the form they are retained in, and until when. It names
// no file and asks for no replacement: Go chooses where the index is written.
type SearchSettingsRequest struct {
	Workspace   string          `json:"workspace"`
	Case        string          `json:"case"`
	Identity    string          `json:"identity"`
	Fields      []string        `json:"fields"`
	Retention   index.Retention `json:"retention"`
	RetainUntil string          `json:"retain_until"`
}

// SearchSettings are the declarations of the index this case already has, as
// the Search settings sheet is prefilled with them. Expired says its declared
// retention has ended; it is never extended by reading it.
type SearchSettings struct {
	Fields      []string        `json:"fields"`
	Retention   index.Retention `json:"retention"`
	RetainUntil *time.Time      `json:"retain_until"`
	Expired     bool            `json:"expired"`
}

// SearchSettingsResult is completed with the settings of this case's own
// index, or empty when the case has none. Repairable says an index of this
// case has the one failure Repair search rebuilds — it is stale against the
// case, so it is not the case's own index until it is rebuilt — and repair is
// offered for the case only while it has.
type SearchSettingsResult struct {
	State      State           `json:"state"`
	Reason     string          `json:"reason,omitzero"`
	Settings   *SearchSettings `json:"settings,omitzero"`
	Repairable bool            `json:"repairable,omitzero"`
}

func (r *SearchSettingsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SaveSearchSettings builds this case's persistent search index under the
// declared retention, through the same build BuildIndex runs. The destination
// is chosen here: an index verified as this case's own is replaced, and
// otherwise the index is written under a new name no other entry holds, so
// another case's index, or any other file, is never replaced.
func (a *App) SaveSearchSettings(request SearchSettingsRequest) BuildIndexResult {
	return run(a, true, true, func(ctx context.Context) BuildIndexResult {
		root, opened, declined := openedCase(request.Workspace, request.Case, request.Identity)
		if root == "" {
			return declined.buildIndex()
		}
		name, replace := ownIndex(root, opened, request.Case)
		if name == "" {
			return refusal{Failed, "no new index name is free beside this case; remove an unused index first"}.buildIndex()
		}
		return a.buildIndex(ctx, BuildIndexRequest{
			Workspace: request.Workspace, Case: request.Case, Identity: request.Identity, Output: name,
			Fields: request.Fields, Retention: request.Retention, RetainUntil: request.RetainUntil, Replace: replace,
		})
	})
}

// DescribeSearchSettings reports the declarations of this case's own index,
// for the Search settings sheet to be prefilled with. It reads and writes no
// other file, and an expired index is described, not extended.
func (a *App) DescribeSearchSettings(workspace, caseName, identity string) SearchSettingsResult {
	return run(a, false, false, func(context.Context) SearchSettingsResult {
		root, opened, declined := openedCase(workspace, caseName, identity)
		if root == "" {
			return SearchSettingsResult{State: declined.state, Reason: declined.reason}
		}
		name, replace := ownIndex(root, opened, caseName)
		if !replace {
			return SearchSettingsResult{State: Empty, Reason: "this case has no search index; messages are read directly", Repairable: a.repairable(root, caseName, opened)}
		}
		document, err := index.Open(artifactpath.JoinReference(root, name))
		if err != nil {
			return SearchSettingsResult{State: Empty, Reason: "this case has no search index; messages are read directly", Repairable: a.repairable(root, caseName, opened)}
		}
		return SearchSettingsResult{State: Completed, Settings: &SearchSettings{
			Fields: document.Policy.Fields, Retention: document.Policy.Retention, RetainUntil: document.Policy.RetainUntil,
			Expired: document.Usable(time.Now().UTC()) != nil,
		}}
	})
}

// ownIndex names where this case's index is written: the entry of an index
// verified as this case's own, with replace set, or else the first free name
// of the form CASE.index.json, CASE.index-2.json, and so on. It returns an
// empty name when none of the bounded names is free.
func ownIndex(root string, opened *bundle.Bundle, caseName string) (string, bool) {
	standard := caseName + ".index.json"
	entries, err := os.ReadDir(root)
	if err == nil {
		names := []string{standard}
		for _, entry := range entries {
			if entry.Name() != standard {
				names = append(names, entry.Name())
			}
		}
		for _, name := range names {
			if artifactpath.EntryName(name) != nil {
				continue
			}
			if kind, ok := classify(root, name, false); ok && kind == IndexArtifact &&
				indexReplacementReason(artifactpath.JoinReference(root, name), opened) == "" {
				return name, true
			}
		}
	}
	for i := 1; i <= maxIndexNames; i++ {
		name := standard
		if i > 1 {
			name = caseName + ".index-" + strconv.Itoa(i) + ".json"
		}
		if artifactpath.EntryName(name) != nil {
			return "", false
		}
		if _, err := os.Lstat(artifactpath.JoinReference(root, name)); os.IsNotExist(err) {
			return name, false
		}
	}
	return "", false
}

// CaseSearchSettings is one case of the open project and the settings of its
// own persistent search index, or nil when it has none and its messages are
// read directly. Reason says why a case could not be read. Repairable is
// SearchSettingsResult's: repair is offered for this case, and no other.
type CaseSearchSettings struct {
	Case       string          `json:"case"`
	Identity   string          `json:"identity,omitzero"`
	Settings   *SearchSettings `json:"settings"`
	Reason     string          `json:"reason,omitzero"`
	Repairable bool            `json:"repairable,omitzero"`
}

// SearchSettingsListResult lists every case of the open project with its
// search settings.
type SearchSettingsListResult struct {
	State  State                `json:"state"`
	Reason string               `json:"reason,omitzero"`
	Cases  []CaseSearchSettings `json:"cases"`
}

func (r *SearchSettingsListResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ListSearchSettings reports, for every case of the open project, the
// declarations of its own persistent search index, as DescribeSearchSettings
// reports one case's. It reads and writes no other file, and an expired
// index is described, not extended. Saving stays SaveSearchSettings'.
func (a *App) ListSearchSettings(workspace string) SearchSettingsListResult {
	return run(a, false, false, func(ctx context.Context) SearchSettingsListResult {
		result := SearchSettingsListResult{Cases: []CaseSearchSettings{}}
		root, declined := resolveFolder(workspace)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		entries, err := os.ReadDir(root)
		switch {
		case errors.Is(err, fs.ErrPermission):
			result.refuse(PermissionDenied, "this account cannot read the project folder")
			return result
		case err != nil:
			result.refuse(Failed, "the project folder cannot be read")
			return result
		}
		now := time.Now().UTC()
		for _, entry := range entries {
			if ctx.Err() != nil {
				result.refuse(Cancelled, "the search settings were not read")
				return result
			}
			name := entry.Name()
			if !entry.IsDir() || artifactpath.EntryName(name) != nil {
				continue
			}
			path, err := artifactpath.Child(root, name)
			if err != nil {
				continue
			}
			if _, err := bundle.Describe(path); err != nil {
				continue
			}
			row := CaseSearchSettings{Case: name}
			opened, err := operation.OpenCase(path)
			if err != nil {
				row.Reason = err.Error()
				result.Cases = append(result.Cases, row)
				continue
			}
			row.Identity = opened.Identity
			if indexName, own := ownIndex(root, opened, name); own {
				if document, err := index.Open(artifactpath.JoinReference(root, indexName)); err == nil {
					row.Settings = &SearchSettings{
						Fields: document.Policy.Fields, Retention: document.Policy.Retention, RetainUntil: document.Policy.RetainUntil,
						Expired: document.Usable(now) != nil,
					}
				}
			}
			row.Repairable = a.repairable(root, name, opened)
			result.Cases = append(result.Cases, row)
		}
		result.State = Completed
		if len(result.Cases) == 0 {
			result.State, result.Reason = Empty, "this project has no case"
		}
		return result
	})
}
