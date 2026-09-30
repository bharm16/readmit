package desktop_test

// Sharing a report (#560): one started flow over one report version whose
// preview is the exact output, whose redaction derives every value the report
// restates or leaves it unresolved, and whose final Export writes those bytes
// once, to a place chosen before the preview; any change withdraws it.

import (
	"bytes"
	"encoding/base64"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/protect"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/reportshare"
	"github.com/bharm16/readmit/internal/testrunner"
)

// prepareShare chooses path as the share's destination, as the save dialog
// answers, and prepares the share there.
func prepareShare(t *testing.T, app *desktop.App, dialogs *chooser, context desktop.RequestContext, ref desktop.ItemRef, options desktop.ReportShareOptions, path string) desktop.ActionReviewResult {
	t.Helper()
	if path != "" {
		// The window offers the output's own name and kind, as the preview
		// named them, and the save dialog answers path.
		first := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ShareReportAction, Items: []desktop.ItemRef{ref}, ReportShare: &options})
		if first.Review == nil || first.Review.ReportShare == nil {
			t.Fatalf("the share: %+v", first)
		}
		output := first.Review.ReportShare.Output
		dialogs.destination = path
		chosen := app.ChooseShareDestination(desktop.ShareDestinationRequest{Context: context, Name: output.Name, Folder: output.Type != desktop.ShareOutputFile})
		if chosen.State != desktop.Completed || chosen.Destination == "" || chosen.Name != filepath.Base(path) {
			t.Fatalf("the destination: %+v", chosen)
		}
		options.Destination = chosen.Destination
	}
	return app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ShareReportAction, Items: []desktop.ItemRef{ref}, ReportShare: &options})
}

// sharedBytes is the exact content the preview showed for one output file.
func sharedBytes(t *testing.T, file desktop.ShareFile) []byte {
	t.Helper()
	if file.Data == "" {
		return []byte(file.Text)
	}
	data, err := base64.StdEncoding.DecodeString(file.Data)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Without a template, an Export writes exactly the previewed bytes of every
// format and says it holds patient data; it is recorded in the report's
// history and is never a review. Original evidence is the portable review
// `readmit report review` verifies, and a place something is already at is
// never written.
func TestAReportShareWritesTheReviewedBytesAndOriginalEvidenceReadmitReviews(t *testing.T) {
	app, dialogs, context, runs := reportsProject(t)
	saved := createReport(t, app, context, "report-1", desktop.ReportDraft{Title: "Reschedule regression", Run: runs["@baseline"].Ref})
	var lastExport desktop.ReviewedActionResult
	var last string
	for _, format := range []string{"html", "pdf", "markdown", "json", "junit"} {
		options := desktop.ReportShareOptions{Format: format}
		if format == "pdf" {
			options.Paper = "a4"
		}
		name := "Reschedule regression" + map[string]string{"html": ".html", "pdf": ".pdf", "markdown": ".md", "json": ".json", "junit": ".xml"}[format]
		path := filepath.Join(t.TempDir(), name)
		prepared := prepareShare(t, app, dialogs, context, *saved.Saved, options, path)
		if prepared.Review == nil || prepared.Review.ReportShare == nil {
			t.Fatalf("%s: %+v", format, prepared)
		}
		share := prepared.Review.ReportShare
		if !prepared.Review.Ready || share.Output.Type != desktop.ShareOutputFile || share.Output.Name != name || !share.SourceValues ||
			share.Redacted || share.Consequence != "Exports a file containing patient data." || share.Destination.Name != name {
			t.Fatalf("%s review: %+v", format, prepared)
		}
		preview := sharedBytes(t, share.Output.Files[0])
		exported := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "export-" + format})
		if exported.Outcome != desktop.ActionCompleted || exported.ReportShare == nil || exported.ReportShare.Name != name || exported.ReportShare.Output == "" {
			t.Fatalf("%s export: %+v", format, exported)
		}
		written, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(written, preview) {
			t.Fatalf("%s: the written bytes are not the previewed bytes: %v", format, err)
		}
		if bytes.Contains(written, []byte("MSH|")) {
			t.Fatalf("the %s report carries original message bytes", format)
		}
		if format == "pdf" && !bytes.Contains(written, []byte("/MediaBox [0 0 595.28 841.89]")) {
			t.Fatal("the A4 PDF")
		}
		last, lastExport = path, exported
		again := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "export-" + format})
		if !again.Replayed || again.Outcome != desktop.ActionCompleted {
			t.Fatalf("a repeated click was not the original export: %+v", again)
		}
	}
	// Show in folder reaches only an output this window wrote.
	var revealed string
	desktop.SetRevealForTest(app, func(path string) error { revealed = path; return nil })
	shown := app.OpenSharedOutput(desktop.SharedOutputRequest{Output: lastExport.ReportShare.Output, Folder: true})
	want, _ := os.Stat(last)
	got, err := os.Stat(revealed)
	if shown.State != desktop.Completed || err != nil || !os.SameFile(want, got) {
		t.Fatalf("show in folder: %+v %s", shown, revealed)
	}
	if other := app.OpenSharedOutput(desktop.SharedOutputRequest{Output: "not-a-handle", Folder: true}); other.State != desktop.Failed {
		t.Fatalf("an output this window did not write was shown: %+v", other)
	}
	if opened := app.OpenSharedOutput(desktop.SharedOutputRequest{Output: lastExport.ReportShare.Output}); opened.State != desktop.Failed {
		t.Fatalf("a host without an opener opened the file: %+v", opened)
	}
	opened := app.OpenReport(desktop.ReportRequest{Context: context, Ref: *saved.Saved}).Report
	if opened.Review != "draft" || len(opened.Shares) != 5 || opened.Shares[0].Format != "junit" || opened.Shares[0].Destination != "local-file" || !opened.Shares[0].SourceValues {
		t.Fatalf("the report's history: %+v", opened.Shares)
	}

	path := filepath.Join(t.TempDir(), "Reschedule regression")
	prepared := prepareShare(t, app, dialogs, context, *saved.Saved, desktop.ReportShareOptions{Format: "html", Contents: desktop.ShareContents{Original: true}}, path)
	if prepared.Review == nil || !prepared.Review.Ready || prepared.Review.ReportShare.Output.Type != desktop.ShareOutputFolder || prepared.Review.ReportShare.Redacted ||
		prepared.Review.ReportShare.Output.Original == 0 {
		t.Fatalf("original evidence review: %+v", prepared)
	}
	if exported := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "export-original"}); exported.Outcome != desktop.ActionCompleted {
		t.Fatalf("original evidence: %+v", exported)
	}
	original := filepath.Join(path, "Original evidence")
	review, err := report.OpenReview(t.Context(), original)
	if err != nil || review.Document == nil || review.Document.Title != "Reschedule regression" || review.Manifest.Schema != report.ReviewSchemaV3 {
		t.Fatalf("the original evidence export: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if err := cli.Execute("test", []string{"report", "review", original}, &stdout, &stderr); err != nil || !strings.Contains(stdout.String(), review.Identity) {
		t.Fatalf("the command line: %v %s", err, stderr.String())
	}

	taken := filepath.Join(t.TempDir(), "taken.html")
	if err := os.WriteFile(taken, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	refused := prepareShare(t, app, dialogs, context, *saved.Saved, desktop.ReportShareOptions{Format: "html"}, taken)
	if refused.Review == nil || refused.Review.Ready || refused.Review.Token != "" && refused.Review.Ready {
		t.Fatalf("a place something is at: %+v", refused.Review)
	}
	if kept, _ := os.ReadFile(taken); string(kept) != "kept" {
		t.Fatal("an existing file was overwritten")
	}
	// A place chosen for one output is not where another is written.
	dialogs.destination = filepath.Join(t.TempDir(), "Reschedule regression.html")
	chosen := app.ChooseShareDestination(desktop.ShareDestinationRequest{Context: context, Name: "Reschedule regression.html"})
	for format, ready := range map[string]bool{"html": true, "markdown": false} {
		result := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ShareReportAction, Items: []desktop.ItemRef{*saved.Saved},
			ReportShare: &desktop.ReportShareOptions{Format: format, Destination: chosen.Destination}})
		if result.Review == nil || result.Review.Ready != ready {
			t.Fatalf("%s at a place chosen for the HTML file: %+v", format, result.Review)
		}
	}
	if unchosen := prepareShare(t, app, dialogs, context, *saved.Saved, desktop.ReportShareOptions{Format: "html"}, ""); unchosen.Review == nil || unchosen.Review.Ready {
		t.Fatalf("a share with no destination is ready: %+v", unchosen.Review)
	}
}

// templateProject is a reports project holding the synthetic disclosure
// template as a named template.
func templateProject(t *testing.T) (*desktop.App, *chooser, desktop.RequestContext, desktop.ItemRef, redact.Policy) {
	t.Helper()
	app, dialogs, context, runs := reportsProject(t)
	raw, err := os.ReadFile("../../testdata/fixtures/redact-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := redact.DecodePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	// Clinic A shares without a check run; Clinic B, the fixture as
	// written, requires one.
	checked := policy
	checked.RequiredFailures = []int{1}
	checked.SpecBindings = []redact.LiteralBinding{}
	policy.PacketPolicies = slices.DeleteFunc(slices.Clone(policy.PacketPolicies), func(name string) bool { return name == redact.Rerun })
	for name, template := range map[string]redact.Policy{"Clinic A.json": policy, "Clinic B.json": checked} {
		data, err := json.Marshal(template, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(context.Project, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	postFix := runs["@post-fix"].Ref
	saved := createReport(t, app, context, "report-1", desktop.ReportDraft{Title: "Reschedule regression", Notes: "Seen with a patient.", Run: runs["@baseline"].Ref, Comparison: &postFix})
	if saved.Saved == nil {
		t.Fatalf("create: %+v", saved)
	}
	return app, dialogs, context, *saved.Saved, policy
}

// A named template derives the values the report restates and the messages
// it holds, leaves what it does not reach unresolved and holding its original
// text, and reveals examples only when asked. A treatment changed after the
// preview withdraws it: the final click writes nothing and needs a fresh one.
func TestATemplateRedactsAShareAndAChangedTreatmentWithdrawsItsPreview(t *testing.T) {
	app, dialogs, context, ref, _ := templateProject(t)
	title := "Shared regression"
	options := desktop.ReportShareOptions{Format: "markdown", Template: "Clinic A.json", Contents: desktop.ShareContents{Messages: true},
		Overrides: reportshare.Overrides{Title: &title, Notes: &reportshare.ValueTreatment{Row: "notes", Remove: true}, Packet: []string{redact.Metadata}}}
	path := filepath.Join(t.TempDir(), "Shared regression")
	prepared := prepareShare(t, app, dialogs, context, ref, options, path)
	share := prepared.Review.ReportShare
	if prepared.Review == nil || share == nil || share.Template != "Clinic A" || share.Output.Type != desktop.ShareOutputFolder || len(share.Output.Files) != 2 {
		t.Fatalf("the templated share: %+v", prepared)
	}
	if share.Items == nil || share.Rows == nil || share.Issues == nil {
		t.Fatalf("a list of the share is absent rather than empty: %+v", share)
	}
	if len(share.Templates) != 2 || share.Templates[0].Name != "Clinic A" {
		t.Fatalf("the templates: %+v", share.Templates)
	}
	derived, unresolved := 0, 0
	for _, row := range share.Rows {
		if len(row.Examples) != 0 {
			t.Fatalf("examples were shown without asking: %+v", row)
		}
		switch row.Result {
		case reportshare.Unresolved:
			unresolved++
		case reportshare.Surrogated, reportshare.Removed, reportshare.Replaced, reportshare.Shifted:
			derived++
		}
	}
	if derived == 0 {
		t.Fatalf("no row was derived: %+v", share.Rows)
	}
	if unresolved > 0 && (share.Redacted || !share.SourceValues || share.Consequence != "Exports files containing patient data.") {
		t.Fatalf("an unresolved share claimed redaction: %+v", share)
	}
	report := string(sharedBytes(t, share.Output.Files[0]))
	if strings.Contains(report, "Reschedule regression") || strings.Contains(report, "Seen with a patient") {
		t.Fatal("replaced free text is in the preview")
	}
	revealed := options
	revealed.Reveal = true
	shown := prepareShare(t, app, dialogs, context, ref, revealed, path).Review.ReportShare
	examples := 0
	for _, row := range shown.Rows {
		examples += len(row.Examples)
	}
	if examples == 0 || !shown.Revealed {
		t.Fatal("Show values showed no examples")
	}

	changed := options
	changed.Overrides.Title = nil
	if other := prepareShare(t, app, dialogs, context, ref, changed, path); other.Review == nil || other.Review.ReportShare.Output.Files[0].Name == share.Output.Files[0].Name {
		t.Fatalf("a changed treatment previewed the same output: %+v", other.Review)
	}
	// Previewing another draft leaves this preview's binding as it was: its
	// click writes exactly what it showed.
	if exported := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "share-1"}); exported.Outcome != desktop.ActionCompleted {
		t.Fatalf("the unchanged preview: %+v", exported)
	}
	written, err := os.ReadFile(filepath.Join(path, "Messages.hl7"))
	if err != nil || !bytes.Equal(written, sharedBytes(t, share.Output.Files[1])) {
		t.Fatalf("the messages written are not the previewed messages: %v", err)
	}

	// Editing the template after a preview withdraws it.
	againPath := filepath.Join(t.TempDir(), "Again")
	second := prepareShare(t, app, dialogs, context, ref, options, againPath)
	raw, _ := os.ReadFile(filepath.Join(context.Project, "Clinic A.json"))
	if err := os.WriteFile(filepath.Join(context.Project, "Clinic A.json"), bytes.Replace(raw, []byte(`"structural"`), []byte(`"structural" `), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	withdrawn := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: second.Review.Token, IntentID: "share-2"})
	if withdrawn.Outcome != desktop.ActionStale {
		t.Fatalf("a changed template did not withdraw the preview: %+v", withdrawn)
	}
	if _, err := os.Stat(againPath); err == nil {
		t.Fatal("a withdrawn share wrote")
	}
}

// An encrypted package is written under an active control at the generation
// it was chosen at, holds the output, and opens with that control; a control
// rotated since is refused. A Send without a signed-in team is never ready.
func TestAnEncryptedShareIsWrittenUnderItsControlAndASendNeedsATeam(t *testing.T) {
	app, dialogs, context, ref, _ := templateProject(t)
	program := keyProgram(t, "test-only-not-a-real-key-7a1b2c3d4e5f6071", "")
	registered := app.SaveProtectionControl(desktop.ProtectionControlRequest{Workspace: context.Project, Entry: "protection.json", Name: "lab-evidence",
		Storage: "os-volume-encryption", Command: program, Arguments: []string{"find-generic-password"}, MaxAge: "720h", Retain: "1h"})
	if registered.State != desktop.Completed {
		t.Fatalf("control: %+v", registered)
	}
	generation := registered.Document.Controls[0].Generation
	path := filepath.Join(t.TempDir(), "Encrypted report")
	options := desktop.ReportShareOptions{Format: "pdf", Encrypt: &desktop.ShareEncryption{Entry: "protection.json", Control: "lab-evidence", Generation: generation}}
	prepared := prepareShare(t, app, dialogs, context, ref, options, path)
	share := prepared.Review.ReportShare
	if prepared.Review == nil || !prepared.Review.Ready || share.Output.Type != desktop.ShareOutputPackage || share.Encryption == nil || share.Encryption.Generation != generation ||
		len(share.Controls) != 1 {
		t.Fatalf("the encrypted share: %+v", prepared)
	}
	if exported := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "package-1"}); exported.Outcome != desktop.ActionCompleted {
		t.Fatalf("package: %+v", exported)
	}
	file := protect.File{Path: filepath.Join(context.Project, "protection.json")}
	opened := filepath.Join(t.TempDir(), "opened")
	if _, _, err := file.Open(t.Context(), "", path, opened); err != nil {
		t.Fatalf("the package does not open with its control: %v", err)
	}
	inside, err := os.ReadFile(filepath.Join(opened, share.Output.Files[0].Name))
	if err != nil || !bytes.Equal(inside, sharedBytes(t, share.Output.Files[0])) {
		t.Fatalf("the package does not hold the previewed report: %v", err)
	}

	rotated := app.RotateProtectionControl(context.Project, "protection.json", "lab-evidence")
	if rotated.State != desktop.Completed {
		t.Fatalf("rotate: %+v", rotated)
	}
	stale := prepareShare(t, app, dialogs, context, ref, options, filepath.Join(t.TempDir(), "Stale"))
	if stale.Review == nil || stale.Review.Ready {
		t.Fatalf("a rotated control was accepted: %+v", stale.Review)
	}

	send := desktop.ReportShareOptions{Format: "pdf", Project: "clinic"}
	sent := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.SendReportAction, Items: []desktop.ItemRef{ref}, ReportShare: &send})
	if sent.Review == nil || sent.Review.Ready || !strings.Contains(sent.Review.Refusal, "redacted") && !strings.Contains(sent.Review.Refusal, "sign in") {
		t.Fatalf("a Send without a team: %+v", sent.Review)
	}
}

// A template that requires a check run keeps a share that holds derived
// messages blocked until a retained run of exactly those messages reproduced
// the original result: preparing the check derives its review under the
// declaration of the inventory it shows, at the environment the original run
// reached, and hands a reviewed run to #555; nothing is sent by preparing it,
// and the share stays blocked until that run is retained.
func TestATemplatesCheckRunIsARealSeparatelyReviewedRun(t *testing.T) {
	app, dialogs, context, ref, _ := templateProject(t)
	// This retained acceptance fixture uses processing ID T. Permit that
	// known structural literal so preparation must actually succeed.
	path := filepath.Join(context.Project, "Clinic B.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := redact.DecodePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	for i := range policy.Fields {
		if policy.Fields[i].Selector == "MSH-11" {
			policy.Fields[i].Allowed = append(policy.Fields[i].Allowed, "T")
		}
	}
	raw, err = json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	options := desktop.ReportShareOptions{Format: "markdown", Template: "Clinic B.json", Contents: desktop.ShareContents{Messages: true}}
	prepared := prepareShare(t, app, dialogs, context, ref, options, filepath.Join(t.TempDir(), "Checked"))
	if prepared.Review == nil || prepared.Review.Ready || prepared.Review.Token != "" || prepared.Review.ReportShare.RunCheck == nil || prepared.Review.ReportShare.RunCheck.Satisfied {
		t.Fatalf("a share needing a check is ready: %+v", prepared.Review)
	}
	without := options
	without.Contents.Messages = false
	if other := prepareShare(t, app, dialogs, context, ref, without, filepath.Join(t.TempDir(), "Report only")); other.Review == nil || other.Review.ReportShare.RunCheck != nil {
		t.Fatalf("a report without its messages needs no check: %+v", other.Review)
	}

	check := func() *desktop.ActionReview {
		t.Helper()
		result := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.PrepareShareCheckAction, Items: []desktop.ItemRef{ref}, ReportShare: &options})
		if result.Review == nil || result.Review.ShareCheck == nil {
			t.Fatalf("the check: %+v", result)
		}
		return result.Review
	}
	if missing := check(); missing.Ready || !strings.Contains(missing.Refusal, "environment") {
		t.Fatalf("a check with no environment to run at: %+v", missing)
	}
	environment := environmentDraft("127.0.0.1:63356", "nonproduction", "plain")
	environment.Name = "Original"
	saveEnvironment(t, app, context, desktop.SaveItemRequest{Draft: desktop.ItemDraft{Name: "Original", Environment: environment}, IntentID: "environment-1"})
	review := check()
	if !review.Ready || review.ShareCheck.Phase != "failure" || review.ShareCheck.Messages != 2 || review.ShareCheck.Inventory.Digest == "" ||
		!slices.Equal(review.Requirements, []desktop.ReviewRequirement{desktop.InventoryDeclarationRequirement}) {
		t.Fatalf("the check review: %+v", review)
	}
	undeclared := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "check-undeclared"})
	if undeclared.Outcome == desktop.ActionCompleted {
		t.Fatalf("a check ran without the inventory declared: %+v", undeclared)
	}
	prepared2 := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "check-1",
		Decisions: desktop.ReviewDecisions{DeclaredInventory: review.ShareCheck.Inventory.Digest}})
	if prepared2.Outcome != desktop.ActionCompleted || prepared2.State != desktop.Completed {
		t.Fatalf("the check must be prepared successfully: %+v", prepared2)
	}
	if prepared2.ShareCheck == nil || prepared2.ShareCheck.Phase != "failure" || prepared2.ShareCheck.Packet.ID != ref.ID {
		t.Fatalf("the prepared check: %+v", prepared2.ShareCheck)
	}
	paths, err := filepath.Glob(filepath.Join(context.Project, ".readmit", "sharing", "run-*", "spec.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("one local check specification: %v %v", paths, err)
	}
	spec, err := testrunner.ReadSpec(paths[0])
	if err != nil || spec.Observation.Path != filepath.Join(context.Project, "observation.json") {
		t.Fatalf("the local check lost its original observation binding: %+v %v", spec, err)
	}
	run := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.RunReviewedTestAction,
		Items: []desktop.ItemRef{prepared2.ShareCheck.Review, prepared2.ShareCheck.Packet}, Run: &desktop.RunActionOptions{Phase: prepared2.ShareCheck.Phase}})
	if run.Review == nil || run.Review.Run == nil || run.Review.Run.Reviewed == nil || run.Review.Consent != desktop.SendConsent {
		t.Fatalf("the check's reviewed run: %+v", run)
	}
	if still := prepareShare(t, app, dialogs, context, ref, options, filepath.Join(t.TempDir(), "Still")); still.Review.Ready || still.Review.ReportShare.RunCheck.Satisfied {
		t.Fatal("a share is ready before its check ran")
	}
}

// A template is saved from a share draft as configuration only, named, and
// replaced by an edit only while it is the version the edit was made from.
func TestATemplateIsSavedFromADraftAndEditedOnlyFromItsCurrentVersion(t *testing.T) {
	app, _, context, ref, _ := templateProject(t)
	title := "Shared"
	saved := app.SaveShareTemplate(desktop.ShareTemplateRequest{Context: context, Name: "Clinic C", Report: &ref, Base: "Clinic A.json",
		Overrides: reportshare.Overrides{Title: &title, Packet: []string{redact.Metadata}, Fields: []redact.FieldRule{{Selector: "PID-7", Policy: redact.Remove, Class: "dates-and-ages"}}}})
	if saved.State != desktop.Completed || saved.Template == nil || saved.Template.Name != "Clinic C" || saved.Policy == nil {
		t.Fatalf("save template: %+v", saved)
	}
	if !slices.ContainsFunc(saved.Policy.Fields, func(rule redact.FieldRule) bool {
		return rule.Selector == "PID[1]-7[1]" && rule.Policy == redact.Remove
	}) {
		t.Fatalf("the draft's rule is not in the template: %+v", saved.Policy.Fields)
	}
	if again := app.SaveShareTemplate(desktop.ShareTemplateRequest{Context: context, Name: "clinic c", Report: &ref, Base: "Clinic A.json"}); again.State != desktop.Failed {
		t.Fatalf("a second template of the same name: %+v", again)
	}
	listedTemplates := app.ListShareTemplates(context)
	if len(listedTemplates.Templates) != 3 {
		t.Fatalf("templates: %+v", listedTemplates)
	}
	read := app.ReadShareTemplate(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{ID: "Clinic C.json"}})
	if read.State != desktop.Completed || read.Digest != saved.Digest {
		t.Fatalf("read template: %+v", read)
	}
	edited := *read.Policy
	edited.RemoveSegments = append(edited.RemoveSegments, "NTE")
	updated := app.SaveShareTemplate(desktop.ShareTemplateRequest{Context: context, Entry: "Clinic C.json", Digest: read.Digest, Policy: &edited})
	if updated.State != desktop.Completed || updated.Digest == read.Digest {
		t.Fatalf("edit template: %+v", updated)
	}
	if stale := app.SaveShareTemplate(desktop.ShareTemplateRequest{Context: context, Entry: "Clinic C.json", Digest: read.Digest, Policy: &edited}); stale.State != desktop.Failed {
		t.Fatalf("an edit of an earlier version replaced the template: %+v", stale)
	}
	invalid := edited
	invalid.Fields = append(slices.Clone(invalid.Fields), redact.FieldRule{Selector: "PID-8", Policy: redact.Replace, Class: "names"})
	if refused := app.SaveShareTemplate(desktop.ShareTemplateRequest{Context: context, Entry: "Clinic C.json", Digest: updated.Digest, Policy: &invalid}); refused.State != desktop.Failed || !strings.Contains(refused.Reason, "replacement") {
		t.Fatalf("an invalid template: %+v", refused)
	}
}

// A support summary is the share operation's own value-free summary of the
// report, previewed whole and exported once under its exact identity; the
// policy is set on its own and approves nothing, and a policy that denies
// support prepares nothing.
func TestASupportSummaryIsPreviewedAndExportedUnderTheProjectPolicy(t *testing.T) {
	app, dialogs, context, ref, _ := templateProject(t)
	prepare := func(path string) *desktop.ActionReview {
		t.Helper()
		options := &desktop.ReportSupportOptions{}
		if path != "" {
			dialogs.destination = path
			chosen := app.ChooseShareDestination(desktop.ShareDestinationRequest{Context: context, Name: filepath.Base(path), Folder: true})
			options.Destination = chosen.Destination
		}
		result := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ExportSupportAction, Items: []desktop.ItemRef{ref}, Support: options})
		if result.Review == nil || result.Review.ReportSupport == nil {
			t.Fatalf("support: %+v", result)
		}
		return result.Review
	}
	if none := prepare(""); none.Ready || none.ReportSupport.Policy != nil {
		t.Fatalf("a summary without a policy: %+v", none)
	}
	if denied := app.SaveProjectSharingPolicy(desktop.SharingPolicyRequest{Context: context, Support: false, Destinations: []string{"local-file"}, MaxBytes: 4096}); denied.State != desktop.Completed {
		t.Fatalf("policy: %+v", denied)
	}
	if denied := prepare(filepath.Join(t.TempDir(), "Denied")); denied.Ready || denied.ReportSupport.Summary != nil {
		t.Fatalf("a denied summary was prepared: %+v", denied)
	}
	if allowed := app.SaveProjectSharingPolicy(desktop.SharingPolicyRequest{Context: context, Support: true, Destinations: []string{"local-file"}, MaxBytes: 4096}); allowed.State != desktop.Completed {
		t.Fatalf("policy: %+v", allowed)
	}
	path := filepath.Join(t.TempDir(), "Support summary")
	review := prepare(path)
	if !review.Ready || review.ReportSupport.Summary == nil || review.ReportSupport.Size == 0 || review.ReportSupport.HubAllowed || review.ReportSupport.Summary.SourceKind != "retained-packet" {
		t.Fatalf("the summary: %+v", review)
	}
	exported := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "support-1"})
	if exported.Outcome != desktop.ActionCompleted || exported.Support == nil || exported.Support.Name != "Support summary" {
		t.Fatalf("export: %+v", exported)
	}
	verified := app.VerifySupportBundle(filepath.Dir(path), filepath.Base(path))
	if verified.State != desktop.Completed || verified.Summary.Identity != review.ReportSupport.Summary.Identity {
		t.Fatalf("the exported summary does not verify: %+v", verified)
	}
	if hub := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ExportSupportAction, Items: []desktop.ItemRef{ref}, Support: &desktop.ReportSupportOptions{Hub: true}}); hub.Review == nil || hub.Review.Ready {
		t.Fatalf("a hub summary without a team: %+v", hub.Review)
	}
}

// The project's encrypted packages are listed from their descriptors;
// Decrypt writes a fresh verified copy to a chosen place, and Delete obeys
// the declared retention unless overridden with a reason, then unlinks only
// the package's declared files.
func TestEncryptedPackagesAreListedDecryptedAndDeletedUnderTheirRetention(t *testing.T) {
	app, dialogs, context, ref, _ := templateProject(t)
	program := keyProgram(t, "test-only-not-a-real-key-0f1e2d3c4b5a6978", "")
	registered := app.SaveProtectionControl(desktop.ProtectionControlRequest{Workspace: context.Project, Entry: "protection.json", Name: "lab-evidence",
		Storage: "os-volume-encryption", Command: program, Arguments: []string{"find-generic-password"}, MaxAge: "720h", Retain: "1h"})
	if registered.State != desktop.Completed {
		t.Fatalf("control: %+v", registered)
	}
	packagePath := filepath.Join(context.Project, "protected-001")
	options := desktop.ReportShareOptions{Format: "html", Encrypt: &desktop.ShareEncryption{Entry: "protection.json", Control: "lab-evidence", Generation: registered.Document.Controls[0].Generation}}
	prepared := prepareShare(t, app, dialogs, context, ref, options, packagePath)
	if exported := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "package-1"}); exported.Outcome != desktop.ActionCompleted {
		t.Fatalf("package: %+v %+v", exported, prepared.Review)
	}
	listed := app.ListEncryptedPackages(context)
	if listed.State != desktop.Completed || len(listed.Packages) != 1 || listed.Packages[0].Entry != "protected-001" || listed.Packages[0].Retention != "within-retention" || listed.Packages[0].Control != "lab-evidence" {
		t.Fatalf("packages: %+v", listed)
	}
	action := func(id desktop.ActionID, options desktop.PackageActionOptions) *desktop.ActionReview {
		t.Helper()
		result := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: id, Package: &options})
		if result.Review == nil || result.Review.PackageAction == nil {
			t.Fatalf("%s: %+v", id, result)
		}
		return result.Review
	}
	copyPath := filepath.Join(t.TempDir(), "Decrypted")
	dialogs.destination = copyPath
	chosen := app.ChooseShareDestination(desktop.ShareDestinationRequest{Context: context, Name: "Decrypted", Folder: true})
	decrypt := action(desktop.DecryptPackageAction, desktop.PackageActionOptions{Package: "protected-001", Destination: chosen.Destination})
	if !decrypt.Ready || decrypt.PackageAction.Destination.Name != "Decrypted" {
		t.Fatalf("decrypt review: %+v", decrypt)
	}
	if decrypted := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: decrypt.Token, IntentID: "decrypt-1"}); decrypted.Outcome != desktop.ActionCompleted || decrypted.PackageAction == nil {
		t.Fatalf("decrypt: %+v", decrypted)
	}
	inside, err := os.ReadFile(filepath.Join(copyPath, prepared.Review.ReportShare.Output.Files[0].Name))
	if err != nil || !bytes.Equal(inside, sharedBytes(t, prepared.Review.ReportShare.Output.Files[0])) {
		t.Fatalf("the decrypted copy: %v", err)
	}

	if retained := action(desktop.DeletePackageAction, desktop.PackageActionOptions{Package: "protected-001"}); retained.Ready {
		t.Fatalf("a retained package can be deleted without an override: %+v", retained)
	}
	override := action(desktop.DeletePackageAction, desktop.PackageActionOptions{Package: "protected-001", Override: true})
	if !override.Ready || !slices.Contains(override.Requirements, desktop.RationaleRequirement) || len(override.PackageAction.Limitations) == 0 {
		t.Fatalf("an override review: %+v", override)
	}
	if unexplained := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: override.Token, IntentID: "delete-0"}); unexplained.Outcome == desktop.ActionCompleted {
		t.Fatalf("an override without a reason deleted: %+v", unexplained)
	}
	deleted := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: override.Token, IntentID: "delete-1", Decisions: desktop.ReviewDecisions{Rationale: "Sent to the vendor by other means."}})
	if deleted.Outcome != desktop.ActionCompleted || deleted.PackageAction == nil || deleted.PackageAction.Removed == 0 {
		t.Fatalf("delete: %+v", deleted)
	}
	if _, err := os.Stat(packagePath); !os.IsNotExist(err) {
		t.Fatal("the package is still there")
	}
	if _, err := os.Stat(copyPath); err != nil {
		t.Fatal("deleting the package touched its decrypted copy")
	}
}
