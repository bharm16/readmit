package desktop_test

// The Findings view reads a case's analyses and a person's review of them as
// catalog objects. These tests drive the facade over a project holding the
// sample regression (a generated SIU case) and an imported acknowledged
// booking, and hold what it writes to what `readmit diagnose` and `readmit
// diagnose review` write and read.

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/findingreview"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/testlicense"
)

// findingsProject is the cases project with the acknowledged-booking fixture
// imported beside the sample cases, and a clock that moves one minute on each
// reading, so every analysis is dated after the one before it.
func findingsProject(t *testing.T) (*desktop.App, desktop.RequestContext, desktop.CatalogItem, string) {
	t.Helper()
	app, _, project := casesProject(t)
	wire, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "diagnose-acknowledged.mllp"))
	if err != nil {
		t.Fatal(err)
	}
	written := writeInputs(t, project.Project, "acked", []bundle.Input{{Path: "SYNTHETIC-FIXTURE", Data: wire,
		Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}}})
	var mu sync.Mutex
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	desktop.SetClockForTest(app, func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(time.Minute)
		return now
	})
	return app, project, caseAt(t, app, project, "acked"), written.Identity
}

// analyzed analyzes one case under one profile and fails unless an analysis
// was made.
func analyzed(t *testing.T, app *desktop.App, project desktop.RequestContext, item desktop.CatalogItem, identity string, profile desktop.AnalysisProfileRef, intent string) desktop.FindingsResult {
	t.Helper()
	result := app.AnalyzeCase(desktop.AnalyzeRequest{Context: project, Case: item.Ref, Identity: identity, Profile: profile, IntentID: intent})
	if result.Analysis == nil || (result.State != desktop.Completed && result.State != desktop.Empty) {
		t.Fatalf("analyze: %+v", result)
	}
	return result
}

// caseIdentityOf is the verified identity of the case at one entry.
func caseIdentityOf(t *testing.T, app *desktop.App, root, entry string) string {
	t.Helper()
	opened := app.OpenCase(root, entry)
	if opened.Case == nil {
		t.Fatalf("open %s: %+v", entry, opened)
	}
	return opened.Case.Identity
}

// analysisEntries are the project entries analyses were written into.
func analysisEntries(t *testing.T, root string) []string {
	t.Helper()
	return slices.DeleteFunc(entriesOf(t, root), func(name string) bool { return !strings.HasPrefix(name, "analysis-") })
}

func TestOpenCaseFindingsReadsTheLatestAnalysisOfThisExactCaseVersion(t *testing.T) {
	app, project, acked, identity := findingsProject(t)
	request := desktop.FindingsRequest{Context: project, Case: acked.Ref, Identity: identity}

	// Nothing analyzed: one empty state, and nothing to set up.
	if none := app.OpenCaseFindings(request); none.State != desktop.Empty || none.Analysis != nil || len(none.Rules) == 0 {
		t.Fatalf("a case never analyzed: %+v", none)
	}
	first := analyzed(t, app, project, acked, identity, desktop.AnalysisProfileRef{Builtin: "siu"}, "press-1")
	second := analyzed(t, app, project, acked, identity, desktop.AnalysisProfileRef{Builtin: "lifecycle"}, "press-2")
	if !first.Analysis.Current || !second.Analysis.Current || first.Analysis.Ref == second.Analysis.Ref {
		t.Fatalf("each analysis is current when made: %+v %+v", first.Analysis, second.Analysis)
	}
	// The latest analysis is read, as current.
	latest := app.OpenCaseFindings(request)
	if latest.Analysis == nil || latest.Analysis.Ref != second.Analysis.Ref || !latest.Analysis.Current || latest.Analysis.ProfileName != "Lifecycle" ||
		latest.Analysis.CreatedAt == nil || latest.Analysis.Diagnosis.CaseIdentity != identity {
		t.Fatalf("latest: %+v", latest.Analysis)
	}
	// An older one opens from History as history.
	older := request
	older.Analysis = &first.Analysis.Ref
	if opened := app.OpenCaseFindings(older); opened.Analysis == nil || opened.Analysis.Current || opened.Analysis.ProfileName != "SIU" ||
		opened.Analysis.ReportSHA256 != first.Analysis.ReportSHA256 {
		t.Fatalf("an older analysis: %+v", opened)
	}
	// History is the case's analyses, newest first; another case's analysis
	// is not among them and does not open under this case.
	regression := caseAt(t, app, project, "regression")
	other := analyzed(t, app, project, regression, caseIdentityOf(t, app, project.Project, "regression"), desktop.AnalysisProfileRef{Builtin: "siu"}, "press-3")
	history := app.ListCatalog(desktop.CatalogQuery{Context: project, Kind: desktop.AnalysisItem, Sort: desktop.SortByCreated,
		Filter: desktop.CatalogFilter{RelatedCase: &acked.Ref}})
	if history.Page == nil || len(history.Page.Items) != 2 || history.Page.Items[0].Ref.ID != second.Analysis.Ref.ID ||
		history.Page.Items[1].Ref.ID != first.Analysis.Ref.ID {
		t.Fatalf("history: %+v", history)
	}
	for _, item := range history.Page.Items {
		if summary := item.Summary.Analysis; summary == nil || summary.CaseIdentity != identity || summary.ConfigSHA256 == "" || summary.Profile == "" {
			t.Fatalf("an analysis summary: %+v", item.Summary.Analysis)
		}
	}
	// History names each analysis's configuration as a person reads it, never
	// by its contract token.
	historyName := func(ref desktop.ItemRef) string {
		t.Helper()
		listed := app.ListCatalog(desktop.CatalogQuery{Context: project, Kind: desktop.AnalysisItem, Filter: desktop.CatalogFilter{RelatedCase: &acked.Ref}})
		for _, item := range listed.Page.Items {
			if item.Ref.ID == ref.ID {
				return item.Summary.Analysis.ProfileName
			}
		}
		t.Fatalf("analysis %s is not in History", ref.ID)
		return ""
	}
	if first, second := historyName(first.Analysis.Ref), historyName(second.Analysis.Ref); first != "SIU" || second != "Lifecycle" {
		t.Fatalf("History names the built-in analyses %q and %q", first, second)
	}
	foreign := request
	foreign.Analysis = &other.Analysis.Ref
	if refused := app.OpenCaseFindings(foreign); refused.State != desktop.Failed {
		t.Fatalf("another case's analysis opened under this case: %+v", refused)
	}
	// Evidence that is not what the window displayed is refused.
	stale := request
	stale.Identity = strings.Repeat("0", 64)
	if refused := app.OpenCaseFindings(stale); refused.State != desktop.Failed {
		t.Fatalf("stale evidence: %+v", refused)
	}
	// Analyzed under saved settings that then change, the case reads as not
	// analyzed now; the analysis stays in History.
	config := diagnose.DefaultConfig()
	config.Rules = []string{diagnose.DuplicateControl, diagnose.ACKOutcome}
	settings := app.SaveItem(desktop.SaveItemRequest{Context: project, Kind: desktop.AnalysisSettingsItem, IntentID: "settings-1",
		Draft: desktop.ItemDraft{Name: "Acknowledgements only", AnalysisSettings: &config}})
	if settings.Saved == nil {
		t.Fatalf("settings: %+v", settings)
	}
	third := analyzed(t, app, project, acked, identity, desktop.AnalysisProfileRef{Settings: settings.Saved}, "press-4")
	if now := app.OpenCaseFindings(request); now.Analysis == nil || now.Analysis.Ref != third.Analysis.Ref || !now.Analysis.Current {
		t.Fatalf("after analyzing under settings: %+v", now)
	}
	if name := historyName(third.Analysis.Ref); name != "Acknowledgements only" {
		t.Fatalf("History names the analysis under saved settings %q", name)
	}
	config.Rules = []string{diagnose.DuplicateControl}
	if changed := app.SaveItem(desktop.SaveItemRequest{Context: project, Kind: desktop.AnalysisSettingsItem, Item: settings.Saved.ID, BaseRevision: "1",
		IntentID: "settings-2", Draft: desktop.ItemDraft{Name: "Acknowledgements only", AnalysisSettings: &config}}); changed.Saved == nil {
		t.Fatalf("settings change: %+v", changed)
	}
	if now := app.OpenCaseFindings(request); now.State != desktop.Empty || now.Analysis != nil {
		t.Fatalf("after the settings changed the old analysis still reads as current: %+v", now)
	}
	older.Analysis = &third.Analysis.Ref
	if kept := app.OpenCaseFindings(older); kept.Analysis == nil || kept.Analysis.Current {
		t.Fatalf("the analysis under the old settings: %+v", kept)
	}
	if name := historyName(third.Analysis.Ref); name != "" {
		t.Fatalf("History names an analysis under settings that changed since %q", name)
	}
}

func TestAnalyzeCaseWritesTheBytesReadmitDiagnoseWrites(t *testing.T) {
	app, project, acked, identity := findingsProject(t)
	root := project.Project
	made := analyzed(t, app, project, acked, identity, desktop.AnalysisProfileRef{Builtin: "siu"}, "press-1")
	entries := analysisEntries(t, root)
	if len(entries) != 1 {
		t.Fatalf("analysis entries: %v", entries)
	}
	written, err := os.ReadFile(filepath.Join(root, entries[0], diagnose.ReportName))
	if err != nil {
		t.Fatal(err)
	}
	markdown, err := os.ReadFile(filepath.Join(root, entries[0], diagnose.MarkdownName))
	if err != nil {
		t.Fatal(err)
	}
	// `readmit diagnose` over the same case writes the same two files.
	out := filepath.Join(t.TempDir(), "cli")
	if _, stderr, err := commandLine(t, "diagnose", filepath.Join(root, "acked"), "--output", out); err != nil {
		t.Fatalf("readmit diagnose: %v %s", err, stderr)
	}
	for name, got := range map[string][]byte{diagnose.ReportName: written, diagnose.MarkdownName: markdown} {
		want, err := os.ReadFile(filepath.Join(out, name))
		if err != nil || string(want) != string(got) {
			t.Fatalf("%s is not the bytes readmit diagnose writes: %v", name, err)
		}
	}
	if made.Analysis.ReportSHA256 != sha256Of(written) || made.Analysis.Diagnosis.Total == 0 || made.State != desktop.Completed {
		t.Fatalf("the analysis does not name the report it wrote: %+v", made)
	}
	// The same press again answers the same analysis and writes nothing.
	again := analyzed(t, app, project, acked, identity, desktop.AnalysisProfileRef{Builtin: "siu"}, "press-1")
	if again.Analysis.Ref != made.Analysis.Ref || len(analysisEntries(t, root)) != 1 {
		t.Fatalf("a repeated press analyzed again: %+v %v", again.Analysis, analysisEntries(t, root))
	}
	if reused := app.AnalyzeCase(desktop.AnalyzeRequest{Context: project, Case: acked.Ref, Identity: identity,
		Profile: desktop.AnalysisProfileRef{Builtin: "order"}, IntentID: "press-1"}); reused.State != desktop.Failed {
		t.Fatalf("one press asked for two analyses: %+v", reused)
	}
	// The object is the discovered entry, dated when it was made.
	opened := app.OpenItem(desktop.ItemRequest{Context: project, Ref: made.Analysis.Ref})
	if opened.Item == nil || opened.Item.CreatedAt == nil || opened.Item.Summary.Analysis.Form != "diagnosis" || opened.Item.Ref.Revision != "" {
		t.Fatalf("the analysis object: %+v", opened)
	}
	// Nothing is chosen for a person.
	if unnamed := app.AnalyzeCase(desktop.AnalyzeRequest{Context: project, Case: acked.Ref, Identity: identity, IntentID: "press-2"}); unnamed.State != desktop.Failed {
		t.Fatalf("an analysis ran under a profile nobody chose: %+v", unnamed)
	}
}

func TestAnalyzeCaseRefusesAProfileTheEngineRefuses(t *testing.T) {
	app, project, _, _ := findingsProject(t)
	root := project.Project
	regression := caseAt(t, app, project, "regression")
	// An imported configuration naming a pair this release does not define
	// is discovered as analysis settings and kept exactly as imported.
	writeDocument(t, root, "customer-config.json", `{"schema":"readmit-diagnose-config/v1","profile":"customer-profile-v9",`+
		`"ruleset":"customer-rules/v9","rules":["customer.rule"],"namespaces":[]}`)
	settings := listed(t, app, root, desktop.AnalysisSettingsItem)["@customer-config.json"]
	if settings.Summary.AnalysisSettings == nil || settings.Summary.AnalysisSettings.Supported || settings.Summary.AnalysisSettings.Profile != "customer-profile-v9" {
		t.Fatalf("imported settings: %+v", settings)
	}
	draft := app.OpenItemDraft(desktop.ItemRequest{Context: project, Ref: settings.Ref})
	if draft.Draft == nil || draft.Draft.AnalysisSettings == nil || draft.Draft.AnalysisSettings.Profile != "customer-profile-v9" ||
		draft.Draft.AnalysisSettings.Ruleset != "customer-rules/v9" {
		t.Fatalf("the imported settings' draft: %+v", draft)
	}

	profiles := app.ListAnalysisProfiles(desktop.ItemRequest{Context: project, Ref: regression.Ref})
	if profiles.State != desktop.Completed || len(profiles.Profiles) != 4 {
		t.Fatalf("profiles: %+v", profiles)
	}
	compatible := map[string]bool{}
	for _, profile := range profiles.Profiles {
		key := profile.Builtin
		if profile.Settings != nil {
			key = "settings"
		}
		compatible[key] = profile.Compatible
		if !profile.Compatible && !slices.ContainsFunc(profile.Refusals, diagnose.Refuses) {
			t.Fatalf("%s is incompatible without the engine's refusal: %+v", key, profile)
		}
	}
	// The sample regression was generated under the SIU profile.
	if !compatible["siu"] || compatible["lifecycle"] || compatible["order"] || compatible["settings"] {
		t.Fatalf("compatibility: %v", compatible)
	}
	identity := caseIdentityOf(t, app, root, "regression")
	for i, profile := range []desktop.AnalysisProfileRef{{Builtin: "lifecycle"}, {Settings: &settings.Ref}} {
		refused := app.AnalyzeCase(desktop.AnalyzeRequest{Context: project, Case: regression.Ref, Identity: identity, Profile: profile,
			IntentID: "refused-" + string(rune('a'+i))})
		if refused.State != desktop.Failed || refused.Analysis != nil || !strings.HasPrefix(refused.Reason, "the engine does not evaluate this profile over this case") {
			t.Fatalf("a refused profile ran: %+v", refused)
		}
	}
	if entries := analysisEntries(t, root); len(entries) != 0 {
		t.Fatalf("a refused analysis wrote %v", entries)
	}
	analyzed(t, app, project, regression, identity, desktop.AnalysisProfileRef{Builtin: "siu"}, "accepted")
}

func TestStoppedAnalysisWritesNoReport(t *testing.T) {
	app, project, acked, identity := findingsProject(t)
	profile := desktop.DeclaredProfilesForTest()["AnalyzeCase"]
	if !profile.Interruptible || profile.Name != desktop.AnalysisOperationForTest {
		t.Fatalf("Analyze cannot be stopped by name: %+v", profile)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stopped := desktop.AnalyzeCaseWithinForTest(app, ctx, desktop.AnalyzeRequest{Context: project, Case: acked.Ref, Identity: identity,
		Profile: desktop.AnalysisProfileRef{Builtin: "siu"}, IntentID: "press-1"})
	if stopped.State != desktop.Cancelled || stopped.Analysis != nil {
		t.Fatalf("a stopped analysis: %+v", stopped)
	}
	if entries := analysisEntries(t, project.Project); len(entries) != 0 {
		t.Fatalf("a stopped analysis wrote %v", entries)
	}
	if listedAnalyses := app.ListCatalog(desktop.CatalogQuery{Context: project, Kind: desktop.AnalysisItem}); listedAnalyses.Page == nil || len(listedAnalyses.Page.Items) != 0 {
		t.Fatalf("a stopped analysis is listed: %+v", listedAnalyses)
	}
	// The stopped press was never answered, so pressing again analyzes.
	analyzed(t, app, project, acked, identity, desktop.AnalysisProfileRef{Builtin: "siu"}, "press-1")
}

func TestMessagesReadsExactlyTheReferencedOccurrencesOffPage(t *testing.T) {
	app, project, _, identity := findingsProject(t)
	root := project.Project
	first := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: "acked", Identity: identity, Limit: 1})
	if first.State != desktop.Completed || len(first.Rows) != 1 || first.Total < 2 {
		t.Fatalf("the first page: %+v", first)
	}
	all := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: "acked", Identity: identity})
	last := all.Rows[len(all.Rows)-1]
	// The last message is off the one-row page, and is read exactly, even
	// under a query that would not keep it.
	referenced := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: "acked", Identity: identity, Limit: 1,
		Occurrences: []string{last.ID}})
	if referenced.State != desktop.Completed || len(referenced.Rows) != 1 || referenced.Rows[0] != last || referenced.Matched != 1 || referenced.Total != all.Total {
		t.Fatalf("the referenced message: %+v", referenced)
	}
	both := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: "acked", Identity: identity, Offset: 1,
		Occurrences: []string{last.ID, all.Rows[0].ID}})
	if len(both.Rows) != 2 || both.Rows[0] != all.Rows[0] || both.Rows[1] != last {
		t.Fatalf("two referenced messages are read in evidence order: %+v", both)
	}
	for _, refused := range [][]string{{"s0001-e999999"}, {last.ID, last.ID}} {
		if got := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: "acked", Identity: identity, Occurrences: refused}); got.State != desktop.Failed {
			t.Fatalf("%v was read: %+v", refused, got)
		}
	}
}

// reviewSaved saves one revision of a review and fails unless it published.
func reviewSaved(t *testing.T, app *desktop.App, project desktop.RequestContext, item, base, intent string, draft desktop.FindingReviewDraft) desktop.SaveItemResult {
	t.Helper()
	saved := app.SaveItem(desktop.SaveItemRequest{Context: project, Kind: desktop.FindingReviewItem, Item: item, BaseRevision: base,
		IntentID: intent, Draft: desktop.ItemDraft{FindingReview: &draft}})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("review save: %+v", saved)
	}
	return saved
}

func TestSavedFindingReviewIsTheDecisionsReadmitDiagnoseReviewReads(t *testing.T) {
	app, project, acked, identity := findingsProject(t)
	root := project.Project
	made := analyzed(t, app, project, acked, identity, desktop.AnalysisProfileRef{Builtin: "siu"}, "press-1")
	findings := made.Analysis.Diagnosis.Findings
	if len(findings) < 2 {
		t.Fatalf("the fixture's findings: %+v", findings)
	}
	decisions := []findingreview.Decision{
		{Finding: findings[1].ID, Verdict: findingreview.Suppressed, Scope: findingreview.ScopeCase, Rationale: "Known interface behaviour"},
		{Finding: findings[0].ID, Verdict: findingreview.Confirmed, Rationale: "Reproduced with the sending system"},
	}
	draft := desktop.FindingReviewDraft{Analysis: made.Analysis.Ref, ReportSHA256: made.Analysis.ReportSHA256, Decisions: decisions}
	// The preview writes nothing and shows what the suppression covers.
	before := entriesOf(t, root)
	preview := app.PreviewFindingReview(desktop.DraftRequest{Context: project, Kind: desktop.FindingReviewItem, Draft: desktop.ItemDraft{FindingReview: &draft}})
	if preview.State != desktop.Completed || len(preview.Problems) != 0 || len(preview.Effects) != 2 || len(preview.Effects[0].Findings) == 0 ||
		preview.Effects[0].Findings[0] != findings[1].ID || preview.Effects[1].Findings[0] != findings[0].ID {
		t.Fatalf("preview: %+v", preview)
	}
	if after := entriesOf(t, root); !slices.Equal(before, after) {
		t.Fatalf("a preview wrote %v", after)
	}
	saved := reviewSaved(t, app, project, "", "", "review-1", draft)
	// The saved revision is one decisions document, in the project, in the
	// order the decisions were made.
	var member string
	for _, name := range entriesOf(t, root) {
		if strings.HasPrefix(name, "finding-review-") {
			member = name
		}
	}
	data, err := os.ReadFile(filepath.Join(root, member))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := findingreview.ParseDecisions(data)
	if err != nil || parsed.Report != made.Analysis.ReportSHA256 || len(parsed.Decisions) != 2 || parsed.Decisions[0] != decisions[0] || parsed.Decisions[1] != decisions[1] {
		t.Fatalf("the saved decisions: %+v %v", parsed, err)
	}
	// `readmit diagnose review` reads that file unchanged, and records every
	// verdict the window reports.
	out := filepath.Join(t.TempDir(), "review")
	reportDir := filepath.Join(root, analysisEntries(t, root)[0])
	if _, stderr, err := commandLine(t, "--operation-policy", testlicense.New(t), "diagnose", "review", reportDir, "--case", filepath.Join(root, "acked"), "--decisions", filepath.Join(root, member), "--output", out); err != nil {
		t.Fatalf("readmit diagnose review: %v %s", err, stderr)
	}
	recorded, err := os.ReadFile(filepath.Join(out, "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record findingreview.Record
	if err := json.Unmarshal(recorded, &record); err != nil {
		t.Fatal(err)
	}
	history := app.FindingReviewHistory(desktop.ItemRequest{Context: project, Ref: made.Analysis.Ref})
	if history.State != desktop.Completed || history.Review == nil || history.Review.ID != saved.Saved.ID || len(history.Statuses) != len(record.Findings) {
		t.Fatalf("history: %+v", history)
	}
	for i, status := range record.Findings {
		got := history.Statuses[i]
		if got.Finding != status.Finding || got.Verdict != status.Verdict || got.Basis != status.Basis || got.Scope != status.Scope || got.SuppressedBy != status.SuppressedBy {
			t.Fatalf("finding %s: the window reads %+v, readmit diagnose review recorded %+v", status.Finding, got, status)
		}
	}
	if record.Decisions != sha256Of(data) {
		t.Fatal("the command line recorded other decisions")
	}
	// The analysis now names its review; the review names its analysis.
	if reread := app.OpenCaseFindings(desktop.FindingsRequest{Context: project, Case: acked.Ref, Identity: identity}); reread.Analysis == nil ||
		reread.Analysis.Review == nil || reread.Analysis.Review.ID != saved.Saved.ID {
		t.Fatalf("the analysis does not name its review: %+v", reread.Analysis)
	}
	listedReview := app.OpenItem(desktop.ItemRequest{Context: project, Ref: *saved.Saved})
	if listedReview.Item == nil || listedReview.Item.Summary.FindingReview == nil || listedReview.Item.Summary.FindingReview.Analysis == nil ||
		listedReview.Item.Summary.FindingReview.Analysis.ID != made.Analysis.Ref.ID || listedReview.Item.Summary.FindingReview.Decisions != 2 {
		t.Fatalf("the review object: %+v", listedReview)
	}
}

func TestFindingReviewRefusesAChangedAnalysisAndAStaleBase(t *testing.T) {
	app, project, acked, identity := findingsProject(t)
	made := analyzed(t, app, project, acked, identity, desktop.AnalysisProfileRef{Builtin: "siu"}, "press-1")
	finding := made.Analysis.Diagnosis.Findings[0].ID
	confirm := []findingreview.Decision{{Finding: finding, Verdict: findingreview.Confirmed, Rationale: "Seen in production"}}

	// A review made while looking at other report bytes is refused.
	changed := desktop.FindingReviewDraft{Analysis: made.Analysis.Ref, ReportSHA256: strings.Repeat("a", 64), Decisions: confirm}
	if refused := app.SaveItem(desktop.SaveItemRequest{Context: project, Kind: desktop.FindingReviewItem, IntentID: "review-0",
		Draft: desktop.ItemDraft{FindingReview: &changed}}); refused.Outcome != desktop.InvalidOutcome || len(refused.Problems) == 0 ||
		refused.Problems[0].Field != "finding_review.report_sha256" {
		t.Fatalf("a review of a changed analysis: %+v", refused)
	}
	// A reason is required, and a decision names a finding of this analysis.
	for _, invalid := range [][]findingreview.Decision{
		{{Finding: finding, Verdict: findingreview.Dismissed}},
		{{Finding: "f999999", Verdict: findingreview.Dismissed, Rationale: "Not ours"}},
	} {
		draft := desktop.FindingReviewDraft{Analysis: made.Analysis.Ref, ReportSHA256: made.Analysis.ReportSHA256, Decisions: invalid}
		if refused := app.SaveItem(desktop.SaveItemRequest{Context: project, Kind: desktop.FindingReviewItem, IntentID: "review-x",
			Draft: desktop.ItemDraft{FindingReview: &draft}}); refused.Outcome != desktop.InvalidOutcome {
			t.Fatalf("an invalid decision was saved: %+v", refused)
		}
	}
	draft := desktop.FindingReviewDraft{Analysis: made.Analysis.Ref, ReportSHA256: made.Analysis.ReportSHA256, Decisions: confirm}
	saved := reviewSaved(t, app, project, "", "", "review-1", draft)
	// A second review of the same analysis is not started beside the first.
	if second := app.SaveItem(desktop.SaveItemRequest{Context: project, Kind: desktop.FindingReviewItem, IntentID: "review-2",
		Draft: desktop.ItemDraft{FindingReview: &draft}}); second.Outcome != desktop.InvalidOutcome {
		t.Fatalf("a second review of one analysis: %+v", second)
	}
	reviewSaved(t, app, project, saved.Saved.ID, "1", "review-3", desktop.FindingReviewDraft{Analysis: made.Analysis.Ref,
		ReportSHA256: made.Analysis.ReportSHA256, Decisions: []findingreview.Decision{{Finding: finding, Verdict: findingreview.Dismissed, Rationale: "Test data"}}})
	// An edit begun from the first revision is now stale.
	stale := app.SaveItem(desktop.SaveItemRequest{Context: project, Kind: desktop.FindingReviewItem, Item: saved.Saved.ID, BaseRevision: "1",
		IntentID: "review-4", Draft: desktop.ItemDraft{FindingReview: &draft}})
	if stale.Outcome != desktop.ConflictOutcome || stale.CurrentRevision != "2" {
		t.Fatalf("a stale review save: %+v", stale)
	}
	// The report bytes change on disk: the review no longer finds the
	// analysis it was made of, and nothing reuses it.
	entry := analysisEntries(t, project.Project)[0]
	path := filepath.Join(project.Project, entry, diagnose.ReportName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if history := app.FindingReviewHistory(desktop.ItemRequest{Context: project, Ref: *saved.Saved}); history.State != desktop.Failed {
		t.Fatalf("a review of a changed analysis still reads: %+v", history)
	}
	if reread := app.OpenCaseFindings(desktop.FindingsRequest{Context: project, Case: acked.Ref, Identity: identity}); reread.Analysis != nil && reread.Analysis.Review != nil {
		t.Fatalf("a changed analysis reuses the earlier review: %+v", reread.Analysis)
	}
}

func TestUndoPublishesANewRevisionAndKeepsHistory(t *testing.T) {
	app, project, acked, identity := findingsProject(t)
	made := analyzed(t, app, project, acked, identity, desktop.AnalysisProfileRef{Builtin: "siu"}, "press-1")
	findings := made.Analysis.Diagnosis.Findings
	first := []findingreview.Decision{
		{Finding: findings[0].ID, Verdict: findingreview.Confirmed, Rationale: "Reproduced"},
		{Finding: findings[1].ID, Verdict: findingreview.Dismissed, Rationale: "Expected in test"},
	}
	saved := reviewSaved(t, app, project, "", "", "review-1", desktop.FindingReviewDraft{Analysis: made.Analysis.Ref,
		ReportSHA256: made.Analysis.ReportSHA256, Decisions: first})
	// Undo the dismissal: a new revision without it.
	reviewSaved(t, app, project, saved.Saved.ID, "1", "review-2", desktop.FindingReviewDraft{Analysis: made.Analysis.Ref,
		ReportSHA256: made.Analysis.ReportSHA256, Decisions: first[:1]})
	history := app.FindingReviewHistory(desktop.ItemRequest{Context: project, Ref: *saved.Saved})
	if history.State != desktop.Completed || len(history.Revisions) != 2 || history.Review == nil || history.Review.Revision != "2" {
		t.Fatalf("history: %+v", history)
	}
	newest, earlier := history.Revisions[0], history.Revisions[1]
	if newest.Revision != "2" || len(newest.Decisions) != 1 || earlier.Revision != "1" || len(earlier.Decisions) != 2 ||
		earlier.Decisions[1] != first[1] || newest.Author == "" || earlier.Author != newest.Author || newest.PublishedAt == nil {
		t.Fatalf("revisions: %+v", history.Revisions)
	}
	for _, status := range history.Statuses {
		switch status.Finding {
		case findings[0].ID:
			if status.Verdict != findingreview.Confirmed {
				t.Fatalf("the kept decision: %+v", status)
			}
		case findings[1].ID:
			if status.Verdict != findingreview.NotReviewed {
				t.Fatalf("the undone decision still applies: %+v", status)
			}
		}
	}
	// The analysis nobody else reviewed reads the same review.
	if byAnalysis := app.FindingReviewHistory(desktop.ItemRequest{Context: project, Ref: made.Analysis.Ref}); byAnalysis.Review == nil ||
		byAnalysis.Review.ID != saved.Saved.ID || len(byAnalysis.Revisions) != 2 {
		t.Fatalf("by analysis: %+v", byAnalysis)
	}
	draft := app.OpenItemDraft(desktop.ItemRequest{Context: project, Ref: *saved.Saved})
	if draft.Draft == nil || draft.Draft.FindingReview == nil || draft.Draft.FindingReview.Analysis.ID != made.Analysis.Ref.ID || len(draft.Draft.FindingReview.Decisions) != 1 {
		t.Fatalf("the review's draft: %+v", draft)
	}
}

func TestSimilarFindingsKeepsAnUnreadableCaseAsARowAndGroupsTheRest(t *testing.T) {
	app, project, acked, _ := findingsProject(t)
	root := project.Project
	regression := caseAt(t, app, project, "regression")
	// A third case whose evidence is damaged after it was imported.
	wire, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "diagnose-acknowledged.mllp"))
	if err != nil {
		t.Fatal(err)
	}
	writeInputs(t, root, "damaged", []bundle.Input{{Path: "SYNTHETIC-FIXTURE", Data: append(wire, wire...),
		Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}}})
	damaged := caseAt(t, app, project, "damaged")
	payloads, err := filepath.Glob(filepath.Join(root, "damaged", "payloads", "*"))
	if err != nil || len(payloads) == 0 {
		t.Fatalf("payloads: %v %v", payloads, err)
	}
	if err := os.WriteFile(payloads[0], []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := desktop.SimilarRequest{Context: project, Cases: []desktop.ItemRef{acked.Ref, damaged.Ref, regression.Ref},
		Profile: desktop.AnalysisProfileRef{Builtin: "siu"}}
	compared := app.FindSimilarFindings(request)
	if compared.State != desktop.Completed || len(compared.Members) != 3 {
		t.Fatalf("comparison: %+v", compared)
	}
	states := map[string]desktop.SimilarMemberState{}
	for _, member := range compared.Members {
		states[member.Case.ID] = member.State
		if member.State != desktop.SimilarAnalyzed && member.Reason == "" {
			t.Fatalf("a case left out without a reason: %+v", member)
		}
	}
	if states[acked.Ref.ID] != desktop.SimilarAnalyzed || states[regression.Ref.ID] != desktop.SimilarAnalyzed || states[damaged.Ref.ID] != desktop.SimilarUnavailable {
		t.Fatalf("members: %+v", compared.Members)
	}
	// The groups are the grouping `readmit diagnose groups` makes of the
	// cases that were analyzed, and name only those.
	grouped, err := diagnose.GroupCases(context.Background(), []string{filepath.Join(root, "acked"), filepath.Join(root, "regression")}, diagnose.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(compared.Groups) != len(grouped.Groups) || len(compared.Groups) == 0 {
		t.Fatalf("groups: %+v, want %d", compared.Groups, len(grouped.Groups))
	}
	ruleNames := map[string]string{}
	for _, rule := range diagnose.Rules() {
		ruleNames[rule.ID] = rule.Name
	}
	for i, group := range compared.Groups {
		if group.Signature != grouped.Groups[i].Signature || len(group.Members) != len(grouped.Groups[i].Members) || group.Classification == "" {
			t.Fatalf("group %d: %+v", i, group)
		}
		if group.RuleName == "" || group.RuleName != ruleNames[group.RuleID] {
			t.Fatalf("group %d names rule %s %q, not the engine's name", i, group.RuleID, group.RuleName)
		}
		for _, ref := range group.Cases {
			if ref.ID == damaged.Ref.ID {
				t.Fatalf("the unreadable case is grouped: %+v", group)
			}
		}
	}
	if compared.Saved != nil || len(slices.DeleteFunc(entriesOf(t, root), func(name string) bool { return !strings.HasPrefix(name, "grouping-") })) != 0 {
		t.Fatal("a comparison nobody saved was written")
	}
	// Saved, it is an analysis of the project, in each compared case's
	// History.
	request.Save = true
	saved := app.FindSimilarFindings(request)
	if saved.Saved == nil {
		t.Fatalf("saved comparison: %+v", saved)
	}
	history := app.ListCatalog(desktop.CatalogQuery{Context: project, Kind: desktop.AnalysisItem, Filter: desktop.CatalogFilter{RelatedCase: &regression.Ref}})
	if history.Page == nil || !slices.ContainsFunc(history.Page.Items, func(item desktop.CatalogItem) bool {
		return item.Ref.ID == saved.Saved.ID && item.Summary.Analysis.Form == "grouping" && len(item.Summary.Analysis.Cases) == 2 && item.CreatedAt != nil
	}) {
		t.Fatalf("the saved comparison is not in the case's history: %+v", history)
	}
	// A stopped comparison writes nothing.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if stopped := desktop.FindSimilarFindingsWithinForTest(app, ctx, request); stopped.State != desktop.Cancelled || stopped.Saved != nil {
		t.Fatalf("a stopped comparison: %+v", stopped)
	}
}
