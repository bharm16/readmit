package desktop_test

// Reports (#559): a report is created from actual retained runs, verified and
// read as one structured document, edited as its title and notes only, and
// exported as the exact bytes a person reviewed.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// packetWorkspace is one workspace holding actual retained evidence: one case,
// the exact historical specification that names it, and two distinct durable
// executions against an independent acking peer — a baseline and a current —
// exactly as the durable-run panels retain them.
func packetWorkspace(t *testing.T, app *desktop.App) (root, spec, baseline, current string) {
	t.Helper()
	peer := newAckingPeer(t, "AA")
	root = ackWorkspace(t, peer.address)
	writeAckSpec(t, root, "reschedule.json", "AA")
	identity := preflighted(t, app, desktop.RunPreflightRequest{Workspace: root, Spec: "reschedule.json"})
	for _, name := range []string{"baseline-run", "current-run"} {
		executed := app.StartDurableRun(desktop.DurableRunRequest{Workspace: root, Spec: "reschedule.json", Output: name, Expected: identity})
		if executed.State != desktop.Completed || executed.Run == nil || executed.Run.State != durablerun.Passed {
			t.Fatalf("retained execution %s: %+v", name, executed)
		}
	}
	return root, "reschedule.json", "baseline-run", "current-run"
}

// packetInput names a workspace's retained evidence as the retained-packet
// assembly reads it.
func packetInput(root, spec, baseline, current string) report.RetainedInput {
	return report.RetainedInput{Case: filepath.Join(root, "case"), Spec: filepath.Join(root, spec), Current: filepath.Join(root, current), Baseline: filepath.Join(root, baseline)}
}

func listingKind(t *testing.T, app *desktop.App, root, name string) string {
	t.Helper()
	listing := app.OpenWorkspace(root)
	if listing.State != desktop.Completed || listing.Workspace == nil {
		t.Fatalf("listing: %+v", listing)
	}
	for _, artifact := range listing.Workspace.Artifacts {
		if artifact.Name == name {
			return string(artifact.Kind)
		}
	}
	return ""
}

// reportsProject is a project holding the retained native-acceptance case and
// both of its runs — the baseline whose record check failed and the post-fix
// run that passed — under a window whose dialogs the test answers.
func reportsProject(t *testing.T) (*desktop.App, *chooser, desktop.RequestContext, map[string]desktop.CatalogItem) {
	t.Helper()
	dialogs := &chooser{folder: t.TempDir()}
	app := newApp(t, dialogs)
	created := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Scheduling QA", Location: app.ChooseProjectLocation().Location})
	if created.State != desktop.Completed {
		t.Fatalf("create: %+v", created)
	}
	root := created.Context.Project
	for _, name := range []string{"baseline", "post-fix", "regression"} {
		copyEntry(t, filepath.Join(nativeAcceptance, name), filepath.Join(root, name))
	}
	return app, dialogs, created.Context, listed(t, app, root, desktop.RunItem)
}

// createReport saves a new report of run compared with comparison.
func createReport(t *testing.T, app *desktop.App, context desktop.RequestContext, intent string, draft desktop.ReportDraft) desktop.SaveItemResult {
	t.Helper()
	return app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ReportItem, Draft: desktop.ItemDraft{Report: &draft}, IntentID: intent})
}

// A report is created from a failed run and a distinct passed run with no
// packet, specification file or output path: the window's report is the
// retained packet `readmit report verify-retained` verifies, and it reads as
// the failed check expected and observed beside acknowledged messages, with
// the comparison run before it.
func TestAReportIsCreatedFromRunsAsTheRetainedPacketReadmitVerifies(t *testing.T) {
	app, _, context, runs := reportsProject(t)
	root := context.Project
	baseline, postFix := runs["@baseline"].Ref, runs["@post-fix"].Ref
	saved := createReport(t, app, context, "report-1", desktop.ReportDraft{Run: baseline, Comparison: &postFix})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil || saved.Saved.Revision != "1" {
		t.Fatalf("create: %+v", saved)
	}
	listedReport := listed(t, app, root, desktop.ReportItem)["Native acceptance reschedule report"]
	summary := listedReport.Summary.Report
	if summary == nil || summary.Form != "report" || summary.Status != "draft" || summary.RelatedCase == nil ||
		summary.RelatedCase.ID != listed(t, app, root, desktop.CaseItem)["@regression"].Ref.ID {
		t.Fatalf("the listed report: %+v", listedReport)
	}
	opened := app.OpenReport(desktop.ReportRequest{Context: context, Ref: *saved.Saved})
	view := opened.Report
	if opened.State != desktop.Completed || view == nil || view.Result.Outcome != report.OutcomeFailed || view.Title != "Native acceptance reschedule report" || view.Review != "draft" {
		t.Fatalf("the report: %+v", opened)
	}
	check := view.Checks[0]
	if check.Result != desktop.CheckFailed || check.Check.Operator != testauthor.LedgerCount || check.Observed == nil || *check.Observed.Count != 2 || *check.Check.Count != 1 {
		t.Fatalf("the failed check: %+v", check)
	}
	if len(view.Messages) == 0 || view.Messages[0].Message == nil || view.Messages[0].Message.MessageCode != "SIU" || view.Messages[0].Delivery != desktop.DeliveryAcknowledged {
		t.Fatalf("the messages: %+v", view.Messages)
	}
	if view.Comparison == nil || len(view.Comparison.Checks) != 1 || view.Comparison.Checks[0].Before != testrunner.Passed || view.Comparison.Checks[0].After != testrunner.Failed ||
		view.Comparison.Checks[0].Definition != "unchanged" || len(view.Runs) != 2 || view.Runs[0].Run == nil || view.Runs[0].Run.ID != baseline.ID || view.Runs[1].Run.ID != postFix.ID ||
		view.Runs[0].Case == nil || len(view.Versions) != 1 || view.Draft == nil {
		t.Fatalf("the comparison and runs: %+v", view)
	}

	// The report's retained packet is the packet the command line verifies.
	store, err := catalog.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	entry := document.Items[document.Find(saved.Saved.ID)].Entry
	packet, err := report.OpenRetained(t.Context(), filepath.Join(root, entry))
	if err != nil || packet.Identity != view.Packet || !strings.HasPrefix(entry, "report-") {
		t.Fatalf("the retained packet %s: %v", entry, err)
	}
	var stdout, stderr bytes.Buffer
	if err := cli.Execute("test", []string{"report", "verify-retained", filepath.Join(root, entry)}, &stdout, &stderr); err != nil || !strings.Contains(stdout.String(), packet.Identity) {
		t.Fatalf("the command line: %v %s", err, stderr.String())
	}

	// A comparison with the same run, and a run the project does not hold,
	// are refused at their own field and nothing is written.
	for name, draft := range map[string]desktop.ReportDraft{
		"the same run": {Run: baseline, Comparison: &baseline},
		"no such run":  {Run: desktop.ItemRef{Kind: desktop.RunItem, ID: strings.Repeat("a", 24)}},
		"not a run":    {Run: desktop.ItemRef{Kind: desktop.CaseItem, ID: summary.RelatedCase.ID}},
		"a long title": {Run: postFix, Title: strings.Repeat("x", 401)},
	} {
		refused := createReport(t, app, context, "refused-"+strings.ReplaceAll(name, " ", "-"), draft)
		if refused.Outcome != desktop.InvalidOutcome || len(refused.Problems) == 0 || !strings.HasPrefix(refused.Problems[0].Field, "report.") {
			t.Fatalf("%s: %+v", name, refused)
		}
	}
	if reports := listed(t, app, root, desktop.ReportItem); len(reports) != 1 {
		t.Fatalf("a refused report was listed: %+v", reports)
	}
}

// Editing a report's title and notes publishes a new version and changes no
// run or outcome; an export prepared for the earlier version is stale and
// does nothing, and a review recorded for it stays with that version.
func TestEditingAReportWithdrawsItsExportAndKeepsItsRuns(t *testing.T) {
	app, dialogs, context, runs := reportsProject(t)
	baseline, postFix := runs["@baseline"].Ref, runs["@post-fix"].Ref
	saved := createReport(t, app, context, "report-1", desktop.ReportDraft{Title: "Reschedule regression", Run: baseline, Comparison: &postFix})
	first := app.OpenReport(desktop.ReportRequest{Context: context, Ref: *saved.Saved}).Report
	prepared := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ExportReportAction, Items: []desktop.ItemRef{*saved.Saved},
		ReportExport: &desktop.ReportExportOptions{Format: "html"}})
	if prepared.Review == nil || !prepared.Review.Ready || prepared.Review.ReportExport == nil || prepared.Review.ReportExport.File != "Reschedule regression.html" ||
		prepared.Review.ReportExport.Size == 0 || prepared.Review.ReportExport.Version != "1" {
		t.Fatalf("the export review: %+v", prepared)
	}

	// Mark reviewed records the version shown; a file written is not a review.
	marked := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ReviewReportAction, Items: []desktop.ItemRef{*saved.Saved}})
	if marked.Review == nil || !marked.Review.Ready || marked.Review.ReportReview == nil || marked.Review.ReportReview.Version != "1" {
		t.Fatalf("the review: %+v", marked)
	}
	if recorded := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: marked.Review.Token, IntentID: "review-1"}); recorded.Outcome != desktop.ActionCompleted {
		t.Fatalf("mark reviewed: %+v", recorded)
	}
	if reviewed := app.OpenReport(desktop.ReportRequest{Context: context, Ref: *saved.Saved}).Report; reviewed.Review != "reviewed" {
		t.Fatalf("a marked version is not reviewed: %s", reviewed.Review)
	}
	if again := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ReviewReportAction, Items: []desktop.ItemRef{*saved.Saved}}); again.Review == nil || again.Review.Ready {
		t.Fatalf("a reviewed version is reviewed again: %+v", again)
	}

	edit := desktop.ReportDraft{Title: "Reschedule regression, duplicate booking", Notes: "Seen twice in QA.", Run: baseline, Comparison: &postFix}
	edited := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ReportItem, Item: saved.Saved.ID, BaseRevision: "1", Draft: desktop.ItemDraft{Report: &edit}, IntentID: "edit-1"})
	if edited.Outcome != desktop.SavedOutcome || edited.Saved.Revision != "2" {
		t.Fatalf("edit: %+v", edited)
	}
	second := app.OpenReport(desktop.ReportRequest{Context: context, Ref: *edited.Saved}).Report
	if second.Title != edit.Title || second.Notes != edit.Notes || second.Packet != first.Packet || second.Result != first.Result || len(second.Versions) != 2 || second.Versions[1].Title != "Reschedule regression" {
		t.Fatalf("the edited report: %+v", second)
	}
	if again := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ReportItem, Item: saved.Saved.ID, BaseRevision: "1", Draft: desktop.ItemDraft{Report: &edit}, IntentID: "edit-2"}); again.Outcome != desktop.ConflictOutcome {
		t.Fatalf("an edit of an earlier version: %+v", again)
	}
	moved := edit
	moved.Comparison = nil
	if other := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ReportItem, Item: saved.Saved.ID, BaseRevision: "2", Draft: desktop.ItemDraft{Report: &moved}, IntentID: "edit-3"}); other.Outcome != desktop.InvalidOutcome || other.Problems[0].Field != "report.run" {
		t.Fatalf("an edit that changes the runs: %+v", other)
	}

	dialogs.destination, dialogs.opened = filepath.Join(t.TempDir(), "stale.html"), nil
	stale := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "export-stale"})
	if stale.Outcome != desktop.ActionStale || len(dialogs.opened) != 0 {
		t.Fatalf("an export of an earlier version: %+v %v", stale, dialogs.opened)
	}
	if _, err := os.Stat(dialogs.destination); !os.IsNotExist(err) {
		t.Fatal("a stale export wrote")
	}
	if second.Review != "draft" || second.Versions[1].Review != "reviewed" {
		t.Fatalf("a new version keeps the earlier review: %+v", second.Versions)
	}
}

// An export writes exactly the bytes its review rendered to the file a person
// names, and is never itself a review; original evidence is the portable
// review `readmit report review` verifies, and a rendered report never
// carries the original message bytes.
func TestAReportExportWritesTheReviewedBytesAndOriginalEvidenceReadmitReviews(t *testing.T) {
	app, dialogs, context, runs := reportsProject(t)
	baseline := runs["@baseline"].Ref
	saved := createReport(t, app, context, "report-1", desktop.ReportDraft{Title: "Reschedule regression", Run: baseline})
	for _, format := range []string{"html", "pdf", "markdown", "json", "junit"} {
		options := &desktop.ReportExportOptions{Format: format}
		if format == "pdf" {
			options.Paper = "a4"
		}
		prepared := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ExportReportAction, Items: []desktop.ItemRef{*saved.Saved}, ReportExport: options})
		if prepared.Review == nil || !prepared.Review.Ready {
			t.Fatalf("%s review: %+v", format, prepared)
		}
		dialogs.destination = filepath.Join(t.TempDir(), prepared.Review.ReportExport.File)
		exported := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "export-" + format})
		if exported.Outcome != desktop.ActionCompleted || exported.ReportExport == nil || exported.ReportExport.File != filepath.Base(dialogs.destination) {
			t.Fatalf("%s export: %+v", format, exported)
		}
		written, err := os.ReadFile(dialogs.destination)
		if err != nil || len(written) != prepared.Review.ReportExport.Size {
			t.Fatalf("%s written: %v", format, err)
		}
		if bytes.Contains(written, []byte("MSH|")) {
			t.Fatalf("the %s report carries original message bytes", format)
		}
		if format == "pdf" && !bytes.Contains(written, []byte("/MediaBox [0 0 595.28 841.89]")) {
			t.Fatal("the A4 PDF")
		}
	}
	// A file written is not a review.
	if exported := app.OpenReport(desktop.ReportRequest{Context: context, Ref: *saved.Saved}).Report; exported.Review != "draft" {
		t.Fatalf("an exported version reads as reviewed: %+v", exported.Versions)
	}
	if listed(t, app, context.Project, desktop.ReportItem)["Reschedule regression"].Summary.Report.Status != "draft" {
		t.Fatal("the listed report reads as reviewed")
	}

	prepared := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ExportReportAction, Items: []desktop.ItemRef{*saved.Saved},
		ReportExport: &desktop.ReportExportOptions{Format: "original"}})
	if prepared.Review == nil || !prepared.Review.ReportExport.Original || !prepared.Review.ReportExport.ContainsSourceValues {
		t.Fatalf("original evidence review: %+v", prepared)
	}
	dialogs.destination = filepath.Join(t.TempDir(), "original")
	if exported := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "export-original"}); exported.Outcome != desktop.ActionCompleted {
		t.Fatalf("original evidence: %+v", exported)
	}
	review, err := report.OpenReview(t.Context(), dialogs.destination)
	if err != nil || review.Document == nil || review.Document.Title != "Reschedule regression" || review.Manifest.Schema != report.ReviewSchemaV3 {
		t.Fatalf("the original evidence export: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if err := cli.Execute("test", []string{"report", "review", dialogs.destination}, &stdout, &stderr); err != nil || !strings.Contains(stdout.String(), review.Identity) {
		t.Fatalf("the command line: %v %s", err, stderr.String())
	}

	// A file already there is never overwritten.
	prepared = app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ExportReportAction, Items: []desktop.ItemRef{*saved.Saved}, ReportExport: &desktop.ReportExportOptions{Format: "html"}})
	dialogs.destination = filepath.Join(t.TempDir(), "taken.html")
	if err := os.WriteFile(dialogs.destination, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if refused := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "export-taken"}); refused.State != desktop.Failed {
		t.Fatalf("an existing file: %+v", refused)
	}
	if kept, _ := os.ReadFile(dialogs.destination); string(kept) != "kept" {
		t.Fatal("an existing file was overwritten")
	}
}
