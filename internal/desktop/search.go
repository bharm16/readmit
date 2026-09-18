package desktop

import (
	"context"
	"strings"

	"github.com/bharm16/readmit/internal/project"
)

// MatchKind says what one search result names: an entry the open folder
// declares, or a case the project document in that folder registers. The same
// bundle can be both, and is named once as each.
type MatchKind string

const (
	ArtifactMatch   MatchKind = "artifact"
	RegisteredMatch MatchKind = "registered_case"
)

// Match is one thing found and where to go to see it. Field is the fixed name
// of the declared field that matched — "name", "title", "owner" and the rest —
// never the value that matched, so a result says why it is here without
// repeating anything out of a project or a case.
type Match struct {
	Kind   MatchKind `json:"kind"`
	Name   string    `json:"name"`
	Label  string    `json:"label"`
	Field  string    `json:"field"`
	Region string    `json:"region"`
}

// SearchResult carries one state. Matches is always present, empty when the
// state is anything but Completed.
type SearchResult struct {
	State   State   `json:"state"`
	Reason  string  `json:"reason,omitzero"`
	Matches []Match `json:"matches"`
}

func (r refusal) search() SearchResult {
	return SearchResult{State: r.state, Reason: r.reason, Matches: []Match{}}
}

// Search navigates one open workspace. It reads exactly what the listing reads
// — the contract each immediate entry declares — and, when the folder holds a
// project document, the cases that document registers. It verifies no evidence,
// derives nothing, opens nothing, and builds no index: every result is a place
// in this window to go and look. A project document this release cannot read is
// not searched for registered cases; the listing already reports that entry as
// unsupported, and it is still found here by its name.
//
// It runs to completion under the listing's own bounds once it starts, so it
// holds the operation slot but is not interruptible.
func (a *App) Search(path, query string) SearchResult {
	release, claimed := a.claim()
	if !claimed {
		return busyRefusal.search()
	}
	defer release()
	wanted := strings.ToLower(strings.TrimSpace(query))
	if wanted == "" {
		return SearchResult{State: Empty, Reason: "type something to search for", Matches: []Match{}}
	}
	root, declined := resolveFolder(path)
	if root == "" {
		return declined.search()
	}
	artifacts, declined := listArtifacts(context.Background(), root)
	if declined.state != "" {
		return declined.search()
	}

	matches := make([]Match, 0, len(artifacts))
	for _, artifact := range artifacts {
		if field, hit := firstMatch(wanted, [][2]string{
			{"name", artifact.Name},
			{"contract", artifact.Schema},
			{"provenance", artifact.Provenance},
		}); hit {
			matches = append(matches, Match{Kind: ArtifactMatch, Name: artifact.Name, Label: artifact.Name, Field: field, Region: NavigationRegion})
		}
	}
	if opened, err := project.Open(root); err == nil {
		for _, registered := range opened.Document.Cases {
			if field, hit := firstMatch(wanted, registeredFields(registered)); hit {
				label := registered.Title
				if label == "" {
					label = registered.Name
				}
				matches = append(matches, Match{Kind: RegisteredMatch, Name: registered.Name, Label: label, Field: field, Region: EvidenceRegion})
			}
		}
	}
	if len(matches) == 0 {
		return SearchResult{State: Empty, Reason: "nothing in this workspace matches", Matches: matches}
	}
	return SearchResult{State: Completed, Matches: matches}
}

// registeredFields are the declared fields of one registered case, in the order
// a match is attributed to them. Tags and incidents are lists, so each entry is
// offered under the same fixed field name.
func registeredFields(registered project.Case) [][2]string {
	fields := [][2]string{
		{"name", registered.Name},
		{"title", registered.Title},
		{"owner", registered.Owner},
	}
	for _, tag := range registered.Tags {
		fields = append(fields, [2]string{"tag", tag})
	}
	for _, incident := range registered.Incidents {
		fields = append(fields, [2]string{"incident", incident})
	}
	return append(fields,
		[2]string{"status", string(registered.Status)},
		[2]string{"interface version", registered.InterfaceVersion},
		[2]string{"contract", registered.Schema},
		[2]string{"provenance", registered.Provenance},
		[2]string{"identity", registered.Identity},
	)
}

// firstMatch reports the first declared field holding wanted, which is already
// lowercased. One thing found is one result, so the search stops at the field
// that explains it rather than listing the same case once per field.
func firstMatch(wanted string, fields [][2]string) (string, bool) {
	for _, field := range fields {
		if field[1] != "" && strings.Contains(strings.ToLower(field[1]), wanted) {
			return field[0], true
		}
	}
	return "", false
}
