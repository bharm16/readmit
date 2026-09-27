package desktop

import "testing"

// A saved reviewer name is the label an approval carries; it never enters the
// review binding, which stays the local account and the hub subject.
func TestTheSavedReviewerNamesApprovalsAndNeverTheBinding(t *testing.T) {
	app := New(nil, ShellDocuments{Folder: t.TempDir()})
	account, binding := app.reviewerName(), app.reviewer()
	if saved := app.SavePreferences(Preferences{Theme: SystemTheme, TextScale: 100, Reviewer: "Dana Reviewer"}); saved.State != Completed {
		t.Fatalf("save: %+v", saved)
	}
	if named := app.reviewerName(); named != "Dana Reviewer" {
		t.Fatalf("an approval is named %q", named)
	}
	if app.reviewer() != binding {
		t.Fatalf("the saved name changed the review binding: %q", app.reviewer())
	}
	if cleared := app.SavePreferences(Preferences{Theme: SystemTheme, TextScale: 100}); cleared.State != Completed || app.reviewerName() != account {
		t.Fatalf("clearing the name: %+v, named %q", cleared, app.reviewerName())
	}
}
