package desktop

import (
	"cmp"
	"errors"
	"runtime"
	"slices"

	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/help"
)

// Help is the bundled articles of this build. Reading and searching them
// takes no operation slot, opens no file and reaches no network, so Help
// answers while any operation runs, and nothing about what a person looks
// for leaves the window.

// HelpTopicsResult lists the five task articles in Help's order.
type HelpTopicsResult struct {
	State  State          `json:"state"`
	Reason string         `json:"reason,omitzero"`
	Topics []help.Summary `json:"topics"`
}

// HelpArticleResult is one article, or Failed when this version has no
// article with that identity. Another article is never substituted.
type HelpArticleResult struct {
	State   State         `json:"state"`
	Reason  string        `json:"reason,omitzero"`
	Article *help.Article `json:"article,omitzero"`
}

// HelpSearchResult is every bundled article a query matches. It is Empty
// when none does and Failed for a query that is empty or too long.
type HelpSearchResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Matches []help.Summary `json:"matches"`
}

// HelpTopics lists the five task articles, in the order Help shows them.
func (a *App) HelpTopics() HelpTopicsResult {
	return HelpTopicsResult{State: Completed, Topics: help.Tasks()}
}

// HelpArticle reads one bundled article.
func (a *App) HelpArticle(id string) HelpArticleResult {
	article, err := help.Read(id)
	if err != nil {
		return HelpArticleResult{State: Failed, Reason: err.Error()}
	}
	return HelpArticleResult{State: Completed, Article: &article}
}

// SearchHelp matches a query against the titles and content of the bundled
// articles, ignoring case.
func (a *App) SearchHelp(query string) HelpSearchResult {
	matches, err := help.Search(query)
	switch {
	case errors.Is(err, help.ErrQuery):
		return HelpSearchResult{State: Failed, Reason: err.Error(), Matches: []help.Summary{}}
	case len(matches) == 0:
		return HelpSearchResult{State: Empty, Reason: "no bundled topic matches", Matches: matches}
	}
	return HelpSearchResult{State: Completed, Matches: matches}
}

// Diagnostics is what this build is and what it supports: its version, the
// Go release and platform it was built for, and every named operation with
// the admission it takes. It names no path, no project and no value.
type Diagnostics struct {
	Version    string               `json:"version"`
	GoVersion  string               `json:"go_version"`
	OS         string               `json:"os"`
	Arch       string               `json:"arch"`
	Operations []SupportedOperation `json:"operations"`
}

// SupportedOperation is one named operation of this build: the method that
// starts it, the name it runs under, whether it can be stopped, and the
// admissions ("author", "execute") it takes before its work starts.
type SupportedOperation struct {
	Method        string   `json:"method"`
	Name          string   `json:"name"`
	Interruptible bool     `json:"interruptible"`
	Prerequisites []string `json:"prerequisites"`
}

// DiagnosticsResult is always Completed.
type DiagnosticsResult struct {
	State       State        `json:"state"`
	Reason      string       `json:"reason,omitzero"`
	Diagnostics *Diagnostics `json:"diagnostics,omitzero"`
}

// Diagnostics reports this build and the operations it supports, on demand.
// It reads nothing, sends nothing and takes no operation slot.
func (a *App) Diagnostics() DiagnosticsResult {
	operations := make([]SupportedOperation, 0, len(profiles))
	for method, profile := range profiles {
		prerequisites := profile.Prerequisites()
		if prerequisites == nil {
			prerequisites = []string{}
		}
		operations = append(operations, SupportedOperation{Method: method, Name: profile.Name, Interruptible: profile.Interruptible, Prerequisites: prerequisites})
	}
	slices.SortFunc(operations, func(x, y SupportedOperation) int { return cmp.Compare(x.Method, y.Method) })
	return DiagnosticsResult{State: Completed, Diagnostics: &Diagnostics{
		Version: engine.Version(), GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, Operations: operations,
	}}
}
