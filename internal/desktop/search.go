package desktop

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/project"
)

// MatchKind says what one search result names: an entry the open folder
// declares, a case the project document in that folder registers, or
// an occurrence whose indexed content matched.
type MatchKind string

const (
	ArtifactMatch   MatchKind = "artifact"
	RegisteredMatch MatchKind = "registered_case"
	ContentMatch    MatchKind = "content"
)

// Match is one thing found and where to go to see it.
//
// Name and Label are how the window already names that thing: the entry name
// the listing shows, and for a registered case the title the project recorded.
// Field is the fixed name of the declared field that matched — "name", "title",
// "owner" and the rest — so a result says why it is here rather than echoing
// the text that matched. Occurrence and Selector are present when the match
// is inside an indexed case occurrence, routing directly to the inspector.
type Match struct {
	Kind       MatchKind `json:"kind"`
	Name       string    `json:"name"`
	Label      string    `json:"label"`
	Field      string    `json:"field"`
	Region     string    `json:"region"`
	Occurrence string    `json:"occurrence,omitzero"`
	Selector   string    `json:"selector,omitzero"`
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

// declaration is one declared field of a searchable thing: the fixed name a
// match reports, and the value the query is compared against.
type declaration struct {
	field string
	value string
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
	release, claimed := a.claim("")
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
		if field, hit := firstMatch(wanted, []declaration{
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

	at := time.Now().UTC()
	rawTerm := strings.TrimSpace(query)
	for _, artifact := range artifacts {
		if artifact.Kind != IndexArtifact {
			continue
		}
		indexPath := filepath.Join(root, artifact.Name)
		doc, err := index.Open(indexPath)
		if err != nil || doc.Usable(at) != nil {
			continue
		}
		caseName := ""
		if strings.HasSuffix(artifact.Name, ".index.json") {
			candidate := strings.TrimSuffix(artifact.Name, ".index.json")
			for _, art := range artifacts {
				if art.Name == candidate && art.Kind == CaseArtifact {
					caseName = candidate
					break
				}
			}
		}
		if caseName == "" {
			for _, art := range artifacts {
				if art.Kind == CaseArtifact {
					if opened, err := bundle.Open(filepath.Join(root, art.Name)); err == nil {
						if doc.Describes(opened) == nil {
							caseName = art.Name
							break
						}
					}
				}
			}
		}
		if caseName == "" {
			continue
		}

		var searchResult index.Result
		var searchErr error
		switch doc.Policy.Retention {
		case index.RetainValues:
			searchResult, searchErr = doc.Search(at, index.Query{Match: index.Contains, Term: []byte(rawTerm)})
		case index.RetainDigests:
			searchResult, searchErr = doc.Search(at, index.Query{Match: index.Equals, Term: []byte(rawTerm)})
		case index.RetainStates:
			switch wanted {
			case "present":
				searchResult, searchErr = doc.Search(at, index.Query{Match: index.State, State: hl7.Present})
			case "empty":
				searchResult, searchErr = doc.Search(at, index.Query{Match: index.State, State: hl7.Empty})
			case "null":
				searchResult, searchErr = doc.Search(at, index.Query{Match: index.State, State: hl7.Null})
			case "omitted":
				searchResult, searchErr = doc.Search(at, index.Query{Match: index.State, State: hl7.Omitted})
			}
		}
		if searchErr == nil && len(searchResult.Hits) > 0 {
			for _, hit := range searchResult.Hits {
				if len(matches) >= 64 {
					break
				}
				label := fmt.Sprintf("%s · %s · %s", caseName, hit.Record.ID, hit.Value.Selector)
				matches = append(matches, Match{
					Kind:       ContentMatch,
					Name:       caseName,
					Label:      label,
					Field:      hit.Value.Selector,
					Region:     InspectorRegion,
					Occurrence: hit.Record.ID,
					Selector:   hit.Value.Selector,
				})
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
func registeredFields(registered project.Case) []declaration {
	fields := []declaration{
		{"name", registered.Name},
		{"title", registered.Title},
		{"owner", registered.Owner},
	}
	for _, tag := range registered.Tags {
		fields = append(fields, declaration{"tag", tag})
	}
	for _, incident := range registered.Incidents {
		fields = append(fields, declaration{"incident", incident})
	}
	return append(fields,
		declaration{"status", string(registered.Status)},
		declaration{"interface version", registered.InterfaceVersion},
		declaration{"contract", registered.Schema},
		declaration{"provenance", registered.Provenance},
		declaration{"identity", registered.Identity},
	)
}

// firstMatch reports the first declared field holding wanted, which is already
// lowercased. One thing found is one result, so the search stops at the field
// that explains it rather than listing the same case once per field.
func firstMatch(wanted string, fields []declaration) (string, bool) {
	for _, declared := range fields {
		if declared.value != "" && strings.Contains(strings.ToLower(declared.value), wanted) {
			return declared.field, true
		}
	}
	return "", false
}
