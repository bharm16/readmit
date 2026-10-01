package help_test

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/help"
)

// lockedTask is one task article exactly as the locked initial copy states it.
type lockedTask struct {
	title   string
	action  help.ActionID
	label   string
	steps   []string
	related []string
}

// lockedTasks is the locked initial copy, written out here a second time on
// purpose: a change to what Help says has to change this too.
var lockedTasks = []lockedTask{
	{"Import messages", "start-import", "Start import", []string{
		"Open a project and choose Import.",
		"Choose files, a folder or a ZIP, or paste messages.",
		"Check the detected format. Change it when the preview does not match your input.",
		"Review the messages and choose Import. The new case opens automatically.",
	}, []string{"Supported formats", "Unparsed messages"}},
	{"Investigate a case", "open-cases", "Open cases", []string{
		"Open a case to read its messages.",
		"Use Search or Filter to narrow the list.",
		"Select a message to open Fields, Raw or Hex.",
		"Use Timeline for event relationships and Findings for analysis results.",
	}, []string{"Message timestamps", "Missing evidence", "Hidden values"}},
	{"Create a regression test", "new-test", "New test", []string{
		"Select messages in a case and choose Create test, or choose New test in Tests.",
		"Name the test and choose its environment and outcome.",
		"Add the checks and any required observation or reset setup.",
		"Review the definition and choose Create test.",
		"Choose Run from the saved test to review its destination before Send.",
	}, []string{"ACK checks", "Observation results", "Reset setup"}},
	{"Connect a test system", "add-environment", "Add environment", []string{
		"Open Environments and choose Add environment.",
		"Enter the address, classification and transport settings.",
		"Save the environment, then choose Test connection.",
		"Add an observation when the test needs downstream records.",
	}, []string{"TLS certificates", "Credential references", "Production refusal", "Connection examples", "Network placement", "Local validator", "What a pass shows", "Connection problems"}},
	{"Share a report", "open-reports", "Open reports", []string{
		"Create a report from a saved run or open an existing report.",
		"Choose Share and select the contents and destination.",
		"Set the required redaction treatments and resolve outstanding items.",
		"Inspect the actual output in Preview.",
		"Choose Export for a local file or Send for the displayed remote destination.",
	}, []string{"Original evidence", "Redaction review", "Encryption"}},
}

// Help lists exactly the five locked tasks, in order, each with its exact
// steps, the task it launches and its related links.
func TestHelpHasTheFiveLockedTasksWithExactSteps(t *testing.T) {
	tasks := help.Tasks()
	if len(tasks) != len(lockedTasks) {
		t.Fatalf("Help lists %d tasks: %+v", len(tasks), tasks)
	}
	for i, locked := range lockedTasks {
		if tasks[i].Title != locked.title || tasks[i].Kind != help.Task {
			t.Fatalf("task %d is %+v, want %q", i, tasks[i], locked.title)
		}
		article, err := help.Read(tasks[i].ID)
		if err != nil {
			t.Fatal(err)
		}
		if article.Title != locked.title || !reflect.DeepEqual(article.Steps, locked.steps) || len(article.Body) != 0 {
			t.Errorf("%s says %+v", locked.title, article)
		}
		if article.Action == nil || article.Action.ID != locked.action || article.Action.Label != locked.label {
			t.Errorf("%s launches %+v", locked.title, article.Action)
		}
		var related []string
		for _, link := range article.Related {
			related = append(related, link.Title)
		}
		if !reflect.DeepEqual(related, locked.related) {
			t.Errorf("%s links %v, want %v", locked.title, related, locked.related)
		}
	}
}

// Every related link names an article this build ships, by its own title, and
// a troubleshooting article carries prose and launches nothing.
func TestEveryRelatedLinkResolvesToABundledArticle(t *testing.T) {
	for _, id := range help.IDs() {
		article, err := help.Read(id)
		if err != nil {
			t.Fatal(err)
		}
		if article.Kind == help.Troubleshooting && (len(article.Body) == 0 || len(article.Steps) != 0 || article.Action != nil) {
			t.Errorf("troubleshooting article %s: %+v", id, article)
		}
		if len(article.Related) == 0 {
			t.Errorf("%s links nothing", id)
		}
		for _, link := range article.Related {
			target, err := help.Read(link.ID)
			if err != nil || target.Title != link.Title || link.ID == id {
				t.Errorf("%s links %+v, which does not resolve: %v", id, link, err)
			}
		}
	}
}

// User Help names no source checkout path and no developer issue number.
func TestHelpNamesNoRepositoryPathOrIssueNumber(t *testing.T) {
	forbidden := regexp.MustCompile(`#\d+|docs/|\.md\b|internal/|github|RM-[A-Z]+`)
	for _, id := range help.IDs() {
		article, _ := help.Read(id)
		text := strings.Join(append(append([]string{article.Title}, article.Steps...), article.Body...), "\n")
		if found := forbidden.FindString(text); found != "" {
			t.Errorf("%s names %q", id, found)
		}
	}
}

// Search reads only the bundled articles: titles and content match without
// regard to case, tasks come first, and a query nothing matches answers no
// article at all.
func TestSearchHelpMatchesTitlesAndBodiesOffline(t *testing.T) {
	titled, err := help.Search("  ack CHECKS ")
	if err != nil || len(titled) == 0 || titled[0].ID != "ack-checks" {
		t.Fatalf("a title search: %+v %v", titled, err)
	}
	bodied, err := help.Search("msh-7")
	if err != nil || !contains(bodied, "message-timestamps") {
		t.Fatalf("a body search: %+v %v", bodied, err)
	}
	stepped, err := help.Search("Test connection")
	if err != nil || len(stepped) == 0 || stepped[0].ID != "connect-a-test-system" {
		t.Fatalf("a step search: %+v %v", stepped, err)
	}
	none, err := help.Search("patient 12345")
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("an unmatched search: %+v %v", none, err)
	}
	for _, refused := range []string{"", "   ", strings.Repeat("a", help.MaxQueryBytes+1), "\xff"} {
		if _, err := help.Search(refused); !errors.Is(err, help.ErrQuery) {
			t.Errorf("query %q answered %v", refused, err)
		}
	}
}

// An identity no article has is refused, never answered with another article.
func TestAMissingArticleIsRefusedNotSubstituted(t *testing.T) {
	for _, id := range []string{"", "import", "Import messages", "../import-messages"} {
		article, err := help.Read(id)
		if !errors.Is(err, help.ErrNoArticle) || article.ID != "" {
			t.Errorf("%q answered %+v %v", id, article, err)
		}
	}
}

func contains(matches []help.Summary, id string) bool {
	for _, match := range matches {
		if match.ID == id {
			return true
		}
	}
	return false
}
