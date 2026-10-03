package desktop_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/desktop"
	"os"
	"path/filepath"
	"testing"
)

func TestConnectedHistoryComparesExplicitRetainedRolesOffline(t *testing.T) {
	f := newConnectedAuthoring(t)
	test := f.save(t, "Actual connected history", "connected-history", f.reschedule(), f.v2.ID)
	runs := []desktop.ItemRef{}
	for i, mode := range []string{"defective", "fixed"} {
		f.engine.SetMode(mode)
		review := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
		actual := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: fmt.Sprintf("history-%d", i)})
		if actual.Run == nil {
			t.Fatalf("actual run absent: %+v", actual)
		}
		runs = append(runs, *actual.Run)
	}
	detail := f.app.OpenRun(desktop.RunRequest{Context: f.context, Run: runs[0]})
	if detail.Run == nil || detail.Run.Item.Summary.Run.TestAssociation != "linked" || len(detail.Run.Item.Summary.Run.SourceCases) != 1 || detail.Run.Item.Summary.Run.SourceCases[0].ID != f.messages.Ref.ID {
		t.Fatalf("genuine source association absent: %+v", detail)
	}
	// Copy the actual immutable result without this project's publication
	// sidecar. Equal plan IDs never establish a test association by themselves.
	retainedPath := filepath.Join(f.root, detail.Run.Item.Summary.Run.Entry)
	if err := os.CopyFS(filepath.Join(f.root, "unlinked-connected-result"), os.DirFS(retainedPath)); err != nil {
		t.Fatal(err)
	}
	listing := f.app.ListCatalog(desktop.CatalogQuery{Context: f.context, Kind: desktop.RunItem})
	unlinked := false
	for _, item := range listing.Page.Items {
		if item.Summary.Run != nil && item.Summary.Run.Entry == "unlinked-connected-result" {
			unlinked = item.Summary.Run.Test == nil && item.Summary.Run.TestAssociation == "unlinked" && item.Summary.Run.SourceAssociation == "unlinked" && len(item.Summary.Run.SourceCases) == 0
		}
	}
	if !unlinked {
		t.Fatal("copied result invented a test association")
	}
	related := f.app.ListCatalog(desktop.CatalogQuery{Context: f.context, Kind: desktop.RunItem, Filter: desktop.CatalogFilter{RelatedCase: &f.messages.Ref}})
	if related.Page == nil || len(related.Page.Items) != 2 {
		t.Fatalf("exact source history: %+v", related)
	}
	before := f.lab.Creates.Load()
	// Deliberately choose the newer result as Before. Roles are supplied, not
	// silently sorted by timestamp. Comparing reaches no current target.
	actual := f.app.CompareRunItems(desktop.RunComparisonItemsRequest{Context: f.context, Before: &runs[1], After: &runs[0]})
	if actual.Comparison == nil || actual.Comparison.Connected == nil {
		t.Fatalf("connected evidence refused: %+v", actual)
	}
	if actual.Comparison.Earlier.Run != runs[1] || actual.Comparison.Later.Run != runs[0] || actual.Comparison.Connected.Baseline.Verdict != "pass" || actual.Comparison.Connected.Current.Verdict != "fail" {
		t.Fatalf("roles/outcomes rewritten: %+v", actual.Comparison)
	}
	f.fixture.Close()
	offline := f.app.CompareRunItems(desktop.RunComparisonItemsRequest{Context: f.context, Before: &runs[1], After: &runs[0]})
	if offline.State != desktop.Completed || offline.Comparison == nil {
		t.Fatalf("offline history needed current target: %+v", offline)
	}
	store, err := catalog.Open(f.root)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	original := ""
	for _, member := range document.Items[document.Find(test.ID)].Current().Members {
		if member.Role == "test" {
			original = filepath.Join(f.root, member.Path)
		}
	}
	if original == "" {
		t.Fatal("original publication unavailable")
	}
	held := original + ".held-by-independent-fixture"
	if err := os.Rename(original, held); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(held, original)
	missing := f.app.OpenRun(desktop.RunRequest{Context: f.context, Run: runs[0]})
	if missing.Run == nil || missing.Run.Item.Summary.Run.TestAssociation != "missing" || missing.Run.Item.Summary.Run.Test == nil || missing.Run.Item.Summary.Run.Test.ID != test.ID {
		t.Fatalf("missing original publication rebound: %+v", missing)
	}
	if missing.Run.Item.Summary.Run.SourceAssociation != "linked" || len(missing.Run.Item.Summary.Run.SourceCases) != 1 || missing.Run.Item.Summary.Run.SourceCases[0] != f.messages.Ref {
		t.Fatalf("missing test erased actual retained capture origin: %+v", missing.Run.Item.Summary.Run)
	}
	cases := f.app.ListCatalog(desktop.CatalogQuery{Context: f.context, Kind: desktop.CaseItem})
	available := false
	if cases.Page != nil {
		for _, item := range cases.Page.Items {
			if item.Ref.ID == f.messages.Ref.ID && item.Availability == desktop.ItemAvailable {
				available = true
			}
		}
	}
	if !available {
		t.Fatalf("original capture became unavailable with the authored test: %+v", cases)
	}
	retainedHistory := f.app.ListCatalog(desktop.CatalogQuery{Context: f.context, Kind: desktop.RunItem, Filter: desktop.CatalogFilter{RelatedCase: &f.messages.Ref}})
	if retainedHistory.Page == nil || len(retainedHistory.Page.Items) != 2 {
		t.Fatalf("missing publication removed genuine capture history: %+v", retainedHistory)
	}
	for _, item := range retainedHistory.Page.Items {
		if item.Ref.ID != runs[0].ID && item.Ref.ID != runs[1].ID {
			t.Fatalf("copied or foreign result inherited capture history: %+v", item)
		}
	}
	if len(actual.Comparison.Connected.Dimensions) != 9 || len(actual.Comparison.Connected.Checks) == 0 || len(actual.Comparison.Connected.Records) == 0 || f.lab.Creates.Load() != before {
		t.Fatal("comparison lacked actual evidence or contacted target")
	}
}

func TestLegacyNormalRunSummaryPinsOriginalAndCopiedResultRemainsUnlinked(t *testing.T) {
	app, context, peer, test, _ := runProject(t, "AA")
	review := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	actual := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "known-legacy-original", Decisions: desktop.ReviewDecisions{Confirmed: []string{"reset"}}})
	if actual.Run == nil || peer.deliveries() != 1 {
		t.Fatalf("actual legacy run absent: %+v", actual)
	}
	opened := app.OpenRun(desktop.RunRequest{Context: context, Run: *actual.Run})
	if opened.Run == nil || opened.Run.Item.Summary.Run.TestAssociation != "linked" || opened.Run.Item.Summary.Run.Test == nil || *opened.Run.Item.Summary.Run.Test != test {
		t.Fatalf("original legacy publication lost: %+v", opened)
	}
	if os.CopyFS(filepath.Join(context.Project, "copied-legacy-original"), os.DirFS(filepath.Join(context.Project, opened.Run.Item.Summary.Run.Entry))) != nil {
		t.Fatal("copy witness failed")
	}
	listing := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.RunItem})
	for _, item := range listing.Page.Items {
		if item.Summary.Run != nil && item.Summary.Run.Entry == "copied-legacy-original" {
			if item.Summary.Run.Test != nil || item.Summary.Run.TestAssociation != "unlinked" {
				t.Fatalf("content matched original became a test pin: %+v", item.Summary.Run)
			}
			return
		}
	}
	t.Fatal("copied result missing")
}

func TestConnectedRunOriginKeepsLegacyMembershipAndRefusesUnboundSourcePins(t *testing.T) {
	f := newConnectedAuthoring(t)
	f.engine.SetMode("fixed")
	test := f.save(t, "Versioned history origin", "origin-versions", f.reschedule(), f.v2.ID)
	review := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	actual := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: "origin-versions-run"})
	if actual.Run == nil {
		t.Fatalf("actual run absent: %+v", actual)
	}
	files, err := filepath.Glob(filepath.Join(f.root, ".readmit", "run-origins", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("owned origin absent: %v %v", files, err)
	}
	original, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer os.WriteFile(files[0], original, 0600)
	var base map[string]jsontext.Value
	if err := json.Unmarshal(original, &base); err != nil {
		t.Fatal(err)
	}
	if string(base["schema"]) != `"readmit-run-origin/v2"` || len(base["source_cases"]) == 0 {
		t.Fatalf("source pins were not versioned: %s", original)
	}
	for _, trial := range []struct {
		name, schema, source string
		linked               bool
	}{
		{"v2-original", "readmit-run-origin/v2", string(base["source_cases"]), true},
		{"v1-readable", "readmit-run-origin/v1", "", true},
		{"v1-null", "readmit-run-origin/v1", "null", false},
		{"v1-empty", "readmit-run-origin/v1", "[]", false},
		{"v1-added", "readmit-run-origin/v1", string(base["source_cases"]), false},
		{"v2-null", "readmit-run-origin/v2", "null", false},
		{"v2-empty", "readmit-run-origin/v2", "[]", false},
		{"v2-foreign-kind", "readmit-run-origin/v2", `[{"kind":"test","id":"foreign"}]`, false},
		{"future", "readmit-run-origin/v99", string(base["source_cases"]), false},
	} {
		t.Run(trial.name, func(t *testing.T) {
			members := map[string]jsontext.Value{}
			if err := json.Unmarshal(original, &members); err != nil {
				t.Fatal(err)
			}
			members["schema"], _ = json.Marshal(trial.schema)
			if trial.source == "" {
				delete(members, "source_cases")
			} else {
				members["source_cases"] = jsontext.Value(trial.source)
			}
			raw, err := json.Marshal(members)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(files[0], raw, 0600); err != nil {
				t.Fatal(err)
			}
			opened := f.app.OpenRun(desktop.RunRequest{Context: f.context, Run: *actual.Run})
			if opened.Run == nil {
				t.Fatalf("retained execution unavailable: %+v", opened)
			}
			summary := opened.Run.Item.Summary.Run
			if trial.linked {
				if summary.TestAssociation != "linked" || summary.SourceAssociation != "linked" || len(summary.SourceCases) != 1 || summary.SourceCases[0] != f.messages.Ref {
					t.Fatalf("compatible source pin lost: %+v", summary)
				}
			} else if summary.TestAssociation != "unlinked" || summary.SourceAssociation != "unlinked" || len(summary.SourceCases) != 0 {
				t.Fatalf("unsupported membership inherited origin: %+v", summary)
			}
		})
	}
}
