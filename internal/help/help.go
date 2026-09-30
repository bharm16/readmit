// Package help is the Help the application ships: five task articles, in the
// order Help lists them, and the troubleshooting articles those tasks link
// to. The articles are part of this build, so a version's Help describes that
// version, and reading or searching them opens no file and reaches no network.
//
// A task article carries numbered steps and the one task it launches. A
// troubleshooting article explains one limit of the product in plain prose,
// adapted from the product's own documentation of that limit. Every related
// link names an article of this package.
package help

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

// Kind separates the task articles Help lists from the troubleshooting
// articles they link to.
type Kind string

const (
	// Task is an article Help lists, with steps and a task it launches.
	Task Kind = "task"
	// Troubleshooting is an article one or more tasks link to.
	Troubleshooting Kind = "troubleshooting"
)

// ActionID names a task an article can launch. The window maps each to the
// screen that performs it.
type ActionID string

// The tasks an article can launch.
const (
	StartImport    ActionID = "start-import"
	OpenCases      ActionID = "open-cases"
	NewTest        ActionID = "new-test"
	AddEnvironment ActionID = "add-environment"
	OpenReports    ActionID = "open-reports"
)

// Action is the task an article launches: the task's identity and the label
// its control shows.
type Action struct {
	ID    ActionID `json:"id"`
	Label string   `json:"label"`
}

// Link is one related article, by identity and title.
type Link struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Article is one bundled Help article. Steps are a task's numbered steps and
// Body is a troubleshooting article's paragraphs; each kind carries one of
// them. Action is present only on a task.
type Article struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Kind    Kind     `json:"kind"`
	Steps   []string `json:"steps"`
	Body    []string `json:"body"`
	Action  *Action  `json:"action,omitzero"`
	Related []Link   `json:"related"`
}

// Summary names one article in a list: Help's task list or search results.
type Summary struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  Kind   `json:"kind"`
}

// MaxQueryBytes bounds one search query.
const MaxQueryBytes = 200

// ErrNoArticle is an identity no article of this version has. It is never
// answered with another article.
var ErrNoArticle = errors.New("this help article is not in this version")

// ErrQuery is a query that is empty or past MaxQueryBytes.
var ErrQuery = errors.New("a help search is between 1 and 200 bytes of text")

// Tasks is the five task articles, in the order Help lists them.
func Tasks() []Summary {
	tasks := make([]Summary, 0, len(taskOrder))
	for _, id := range taskOrder {
		tasks = append(tasks, summary(articles[id]))
	}
	return tasks
}

// Read answers the article with this identity, or ErrNoArticle.
func Read(id string) (Article, error) {
	stored, ok := articles[id]
	if !ok {
		return Article{}, ErrNoArticle
	}
	article := Article{ID: id, Title: stored.title, Kind: stored.kind, Steps: append([]string{}, stored.steps...),
		Body: append([]string{}, stored.body...), Related: []Link{}}
	if stored.action != nil {
		action := *stored.action
		article.Action = &action
	}
	for _, related := range stored.related {
		article.Related = append(article.Related, Link{ID: related, Title: articles[related].title})
	}
	return article, nil
}

// Search answers every article whose title, steps or body contain the query,
// ignoring case, tasks first in Help's order and then troubleshooting
// articles in title order. It reads only the articles of this build.
func Search(query string) ([]Summary, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > MaxQueryBytes || !utf8.ValidString(query) {
		return nil, ErrQuery
	}
	needle := strings.ToLower(query)
	matches := []Summary{}
	for _, id := range searchOrder() {
		stored := articles[id]
		text := strings.ToLower(stored.title + "\n" + strings.Join(stored.steps, "\n") + "\n" + strings.Join(stored.body, "\n"))
		if strings.Contains(text, needle) {
			matches = append(matches, summary(stored))
		}
	}
	return matches, nil
}

// IDs is every article's identity, in search order.
func IDs() []string { return searchOrder() }

func summary(stored article) Summary {
	return Summary{ID: stored.id, Title: stored.title, Kind: stored.kind}
}

func searchOrder() []string {
	order := append([]string{}, taskOrder...)
	var troubleshooting []article
	for _, stored := range articles {
		if stored.kind == Troubleshooting {
			troubleshooting = append(troubleshooting, stored)
		}
	}
	slices.SortFunc(troubleshooting, func(a, b article) int { return strings.Compare(a.title, b.title) })
	for _, stored := range troubleshooting {
		order = append(order, stored.id)
	}
	return order
}
