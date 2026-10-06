package desktop_test

import (
	"bytes"
	"context"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/report"
	"os"
	"path/filepath"
	"testing"
)

func TestConnectedReportAssemblesActualRunAndPreservesEvidenceIdentity(t *testing.T) {
	parallelLifecycleTest(t)
	f := newConnectedAuthoring(t)
	f.engine.SetMode("fixed")
	test := f.save(t, "Actual connected report", "actual-connected-report", f.reschedule(), f.v2.ID)
	review := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	actual := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: "connected-report-run"})
	if actual.Run == nil {
		t.Fatalf("actual execution absent: %+v", actual)
	}
	before := f.lab.Creates.Load()
	saved := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.ReportItem, IntentID: "connected-report-create", Draft: desktop.ItemDraft{Name: "Connected evidence", Report: &desktop.ReportDraft{Title: "Connected evidence", Run: *actual.Run}}})
	if saved.Saved == nil {
		t.Fatalf("actual connected report refused: %+v", saved)
	}
	opened := f.app.OpenReport(desktop.ReportRequest{Context: f.context, Ref: *saved.Saved})
	if opened.Report == nil || opened.Report.Connected == nil || opened.Report.Connected.PacketIdentity == "" || len(opened.Report.Connected.Runs) != 1 || opened.Report.Connected.Runs[0].Run.Verdict != "pass" {
		t.Fatalf("actual report evidence missing: %+v", opened)
	}

	dialogs := &chooser{folder: f.root}
	exporter := newApp(t, dialogs)
	originalPath := filepath.Join(t.TempDir(), "connected-report.json")
	original := prepareShare(t, exporter, dialogs, f.context, *saved.Saved, desktop.ReportShareOptions{Format: "json", ConnectedMode: "original-report"}, originalPath)
	if original.Review == nil || !original.Review.Ready || original.Review.ReportShare == nil || !original.Review.ReportShare.SourceValues {
		t.Fatalf("original report review: %+v", original)
	}
	expected := sharedBytes(t, original.Review.ReportShare.Output.Files[0])
	result := exporter.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: original.Review.Token, IntentID: "connected-report-export"})
	if result.Outcome != desktop.ActionCompleted {
		t.Fatalf("original export: %+v", result)
	}
	// The handle opens the exact exported output through its existing owner.
	if result.ReportShare == nil || result.ReportShare.Output == "" {
		t.Fatal("output handle missing")
	}
	extractPath := filepath.Join(t.TempDir(), "connected-extract")
	redacted := prepareShare(t, exporter, dialogs, f.context, *saved.Saved, desktop.ReportShareOptions{Format: "json", ConnectedMode: "value-free-extract"}, extractPath)
	if redacted.Review == nil || !redacted.Review.Ready || redacted.Review.ReportShare.SourceValues || !redacted.Review.ReportShare.Redacted {
		t.Fatalf("value-free disclosure review: %+v", redacted)
	}
	actualExtract := exporter.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: redacted.Review.Token, IntentID: "connected-extract-export"})
	if actualExtract.Outcome != desktop.ActionCompleted {
		t.Fatalf("extract publish: %+v", actualExtract)
	}
	x, err := report.OpenExtract(extractPath)
	if err != nil || x.PacketIdentity != opened.Report.Packet || x.Equivalence.State != report.EquivalenceUnverified {
		t.Fatalf("offline actual extract: %+v %v", x, err)
	}
	originalBytes, err := os.ReadFile(originalPath)
	if err != nil || len(expected) == 0 || !bytes.Equal(originalBytes, expected) {
		t.Fatal("original export differs from exact preview")
	}
	raw, err := os.ReadFile(filepath.Join(extractPath, "extract.json"))
	if err != nil || !bytes.Equal(raw, sharedBytes(t, redacted.Review.ReportShare.Output.Files[0])) {
		t.Fatal("export differs from exact preview")
	}
	if _, err := report.OpenConnected(context.Background(), filepath.Join(f.root, "report-001")); err != nil {
		t.Fatal(err)
	}
	if f.lab.Creates.Load() != before {
		t.Fatal("report reran the target")
	}
	for _, contents := range []desktop.ShareContents{{Attachments: true}, {Original: true}, {Messages: true}} {
		refused := exporter.PrepareAction(desktop.PrepareActionRequest{Context: f.context, Action: desktop.ShareReportAction, Items: []desktop.ItemRef{*saved.Saved}, ReportShare: &desktop.ReportShareOptions{Format: "json", ConnectedMode: "value-free-extract", Contents: contents}})
		if refused.State != desktop.Failed {
			t.Fatalf("original values could hide in extract: %+v", refused)
		}
	}
	stalePath := filepath.Join(t.TempDir(), "stale-connected-report.json")
	stale := prepareShare(t, exporter, dialogs, f.context, *saved.Saved, desktop.ReportShareOptions{Format: "json", ConnectedMode: "original-report"}, stalePath)
	changed := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.ReportItem, Item: saved.Saved.ID, BaseRevision: saved.Saved.Revision, IntentID: "changed-connected-report", Draft: desktop.ItemDraft{Name: "Connected evidence", Report: &desktop.ReportDraft{Title: "Changed report title", Run: *actual.Run}}})
	if changed.Saved == nil {
		t.Fatalf("authored report change: %+v", changed)
	}
	refused := exporter.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: stale.Review.Token, IntentID: "stale-report-export"})
	if refused.Outcome != desktop.ActionStale {
		t.Fatalf("changed report exported under old review: %+v", refused)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatal("stale output was written")
	}
	f.engine.SetMode("defective")
	laterReview := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	later := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: laterReview.Token, IntentID: "connected-report-comparison-run"})
	if later.Run == nil {
		t.Fatal("actual comparison run unavailable")
	}
	paired := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.ReportItem, IntentID: "actual-connected-paired-report", Draft: desktop.ItemDraft{Name: "Actual paired report", Report: &desktop.ReportDraft{Title: "Actual paired report", Run: *later.Run, Comparison: actual.Run}}})
	if paired.Saved == nil {
		t.Fatalf("supplied comparison refused: %+v", paired)
	}
	pairedView := f.app.OpenReport(desktop.ReportRequest{Context: f.context, Ref: *paired.Saved})
	if pairedView.Report == nil || pairedView.Report.Connected == nil || len(pairedView.Report.Connected.Runs) != 2 || pairedView.Report.Connected.Comparison == nil {
		t.Fatalf("supplied actual comparison disappeared: %+v", pairedView)
	}

}

func TestConnectedReportSummaryPinsActualTestAndLeavesCopiedRunUnlinked(t *testing.T) {
	parallelLifecycleTest(t)
	f := newConnectedAuthoring(t)
	f.engine.SetMode("fixed")
	test := f.save(t, "Exact report association", "report-association-test", f.reschedule(), f.v2.ID)
	review := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	actual := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: "report-association-run"})
	if actual.Run == nil {
		t.Fatal("actual run unavailable")
	}
	create := func(ref desktop.ItemRef, intent string) desktop.ItemRef {
		saved := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.ReportItem, IntentID: intent, Draft: desktop.ItemDraft{Name: intent, Report: &desktop.ReportDraft{Title: intent, Run: ref}}})
		if saved.Saved == nil {
			t.Fatalf("report failed: %+v", saved)
		}
		return *saved.Saved
	}
	original := create(*actual.Run, "known-origin-report")
	detail := f.app.OpenRun(desktop.RunRequest{Context: f.context, Run: *actual.Run})
	if detail.Run == nil {
		t.Fatal("run detail absent")
	}
	if os.CopyFS(filepath.Join(f.root, "copied-unlinked-report-run"), os.DirFS(filepath.Join(f.root, detail.Run.Item.Summary.Run.Entry))) != nil {
		t.Fatal("copy witness failed")
	}
	runs := f.app.ListCatalog(desktop.CatalogQuery{Context: f.context, Kind: desktop.RunItem})
	var copied desktop.ItemRef
	for _, item := range runs.Page.Items {
		if item.Summary.Run != nil && item.Summary.Run.Entry == "copied-unlinked-report-run" {
			copied = item.Ref
		}
	}
	if copied.ID == "" {
		t.Fatal("copied run absent")
	}
	unlinked := create(copied, "unlinked-origin-report")
	listing := f.app.ListCatalog(desktop.CatalogQuery{Context: f.context, Kind: desktop.ReportItem})
	if listing.Page == nil {
		t.Fatal("report summaries absent")
	}
	for _, item := range listing.Page.Items {
		summary := item.Summary.Report
		if summary == nil {
			continue
		}
		if item.Ref.ID == original.ID {
			if len(summary.Tests) != 1 || summary.Tests[0] != test || len(summary.SourceRuns) != 1 || summary.SourceRuns[0] != *actual.Run {
				t.Fatalf("genuine archive pins lost: %+v", summary)
			}
		}
		if item.Ref.ID == unlinked.ID && len(summary.Tests) != 0 {
			t.Fatalf("copied result invented test: %+v", summary)
		}
	}
}

func TestLegacySuiteJobReportSummaryPinsArchivedSavedTest(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	p := newSuiteProject(t, peer.address)
	draft := p.smokeSuite()
	draft.Tests = draft.Tests[:1]
	draft.Datasets[0].Rows = draft.Datasets[0].Rows[1:]
	draft.Datasets = draft.Datasets[:1]
	draft.Environments = draft.Environments[:1]
	draft.Environments[0].Bindings = draft.Environments[0].Bindings[:1]
	draft.Exclusions = []desktop.SuiteExclusion{}
	ref := p.saveSuite(t, "report-suite-create", "", "", "Actual suite report", draft)
	review := runReview(t, p.app, desktop.PrepareActionRequest{Context: p.context, Action: desktop.RunSuiteAction, Items: []desktop.ItemRef{ref}})
	confirmed := []string{}
	for _, step := range review.Run.Setup {
		confirmed = append(confirmed, step.ID)
	}
	actual := p.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: p.context, Token: review.Token, IntentID: "report-suite-run", Decisions: desktop.ReviewDecisions{Confirmed: confirmed}})
	if actual.Run == nil {
		t.Fatalf("actual suite unavailable: %+v", actual)
	}
	opened := p.app.OpenRun(desktop.RunRequest{Context: p.context, Run: *actual.Run})
	if opened.Run == nil || len(opened.Run.Jobs) != 1 {
		t.Fatalf("actual job unavailable: %+v", opened)
	}
	job := opened.Run.Jobs[0].ID
	saved := p.app.SaveItem(desktop.SaveItemRequest{Context: p.context, Kind: desktop.ReportItem, IntentID: "report-suite-job-create", Draft: desktop.ItemDraft{Name: "Actual suite job report", Report: &desktop.ReportDraft{Title: "Actual suite job report", Run: *actual.Run, Job: job}}})
	if saved.Saved == nil {
		t.Fatalf("actual job report refused: %+v", saved)
	}
	listing := p.app.ListCatalog(desktop.CatalogQuery{Context: p.context, Kind: desktop.ReportItem})
	if listing.Page == nil {
		t.Fatal("report summary missing")
	}
	for _, item := range listing.Page.Items {
		if item.Ref.ID == saved.Saved.ID {
			if item.Summary.Report == nil || len(item.Summary.Report.Tests) != 1 || item.Summary.Report.Tests[0] != p.booking {
				t.Fatalf("suite job guessed/lost test pin: %+v", item.Summary.Report)
			}
			return
		}
	}
	t.Fatal("report not discovered")
}

func TestLegacyIndividualReportSummaryPinsOriginalAndCopiedRunRemainsUnlinked(t *testing.T) {
	app, ctx, _, test, _ := runProject(t, "AA")
	review := runReview(t, app, desktop.PrepareActionRequest{Context: ctx, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	actual := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: ctx, Token: review.Token, IntentID: "legacy-report-original-run", Decisions: desktop.ReviewDecisions{Confirmed: []string{"reset"}}})
	if actual.Run == nil {
		t.Fatalf("actual run absent: %+v", actual)
	}
	opened := app.OpenRun(desktop.RunRequest{Context: ctx, Run: *actual.Run})
	if opened.Run == nil {
		t.Fatal("actual run unreadable")
	}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: ctx, Kind: desktop.ReportItem, IntentID: "legacy-original-report", Draft: desktop.ItemDraft{Name: "Legacy original report", Report: &desktop.ReportDraft{Title: "Legacy original report", Run: *actual.Run}}})
	if saved.Saved == nil {
		t.Fatalf("original report refused: %+v", saved)
	}
	if err := os.CopyFS(filepath.Join(ctx.Project, "copied-report-run"), os.DirFS(filepath.Join(ctx.Project, opened.Run.Item.Summary.Run.Entry))); err != nil {
		t.Fatal(err)
	}
	runs := app.ListCatalog(desktop.CatalogQuery{Context: ctx, Kind: desktop.RunItem})
	var copyRef desktop.ItemRef
	for _, item := range runs.Page.Items {
		if item.Summary.Run != nil && item.Summary.Run.Entry == "copied-report-run" {
			copyRef = item.Ref
		}
	}
	if copyRef.ID == "" {
		t.Fatal("copy absent")
	}
	copied := app.SaveItem(desktop.SaveItemRequest{Context: ctx, Kind: desktop.ReportItem, IntentID: "legacy-copy-report", Draft: desktop.ItemDraft{Name: "Legacy copied report", Report: &desktop.ReportDraft{Title: "Legacy copied report", Run: copyRef}}})
	if copied.Saved == nil {
		t.Fatalf("copied report refused: %+v", copied)
	}
	reports := app.ListCatalog(desktop.CatalogQuery{Context: ctx, Kind: desktop.ReportItem})
	if reports.Page == nil {
		t.Fatal("reports absent")
	}
	found := 0
	for _, item := range reports.Page.Items {
		if item.Ref.ID == saved.Saved.ID {
			found++
			if item.Summary.Report == nil || len(item.Summary.Report.Tests) != 1 || item.Summary.Report.Tests[0] != test {
				t.Fatalf("original report lost actual pin: %+v", item.Summary.Report)
			}
		}
		if item.Ref.ID == copied.Saved.ID {
			found++
			if item.Summary.Report == nil || len(item.Summary.Report.Tests) != 0 {
				t.Fatalf("copy acquired invented pin: %+v", item.Summary.Report)
			}
		}
	}
	if found != 2 {
		t.Fatal("actual reports missing")
	}
}
