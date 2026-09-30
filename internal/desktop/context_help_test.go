package desktop_test

import (
	"strings"
	"testing"
)

// Help reads only the articles this build bundles, through the facade: the
// screen holds no copy of an article, reaches no network, keeps nothing in
// the browser and appends no generic help code to a status. This checks
// shell wiring only; rendered interaction is exercised by the frontend tests.
func TestHelpIsOfflineAndReadsOnlyBundledArticles(t *testing.T) {
	help := read(t, frontendDirectory+"/Help.tsx")
	status := read(t, frontendDirectory+"/shell.tsx")
	if !strings.Contains(help, "helpTopics()") || !strings.Contains(help, "helpArticle(") || !strings.Contains(help, "searchHelp(") {
		t.Fatal("Help must read its topics, articles and search from the bundled articles")
	}
	if strings.Contains(status, "StateHelp") || strings.Contains(help, "RM-") {
		t.Fatal("a status must not append a generic help disclosure")
	}
	for _, unsafe := range []string{"fetch(", "https://", "localStorage", "docs/"} {
		if strings.Contains(help, unsafe) {
			t.Errorf("help must stay offline and name no repository path: %s", unsafe)
		}
	}
}
