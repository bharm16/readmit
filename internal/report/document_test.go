package report_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"encoding/xml"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testrunner"
)

// A failed run and a distinct comparison run of the same test become one
// structured report whose five renderings agree, and a v3 review refuses a
// changed claim even once every hash is recomputed.
func TestStructuredReportOfAFailedRunAndItsComparison(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "source")
	if _, err := report.Create(ctx, report.Scenario, source); err != nil {
		t.Fatal(err)
	}
	packet := filepath.Join(t.TempDir(), "packet")
	if _, err := report.AssembleRuns(ctx, report.RunsInput{Case: filepath.Join(source, "reproducer"), Current: filepath.Join(source, "baseline"), Comparison: filepath.Join(source, "post-fix")}, packet); err != nil {
		t.Fatal(err)
	}
	if _, err := report.AssembleRuns(ctx, report.RunsInput{Case: filepath.Join(source, "reproducer"), Current: filepath.Join(source, "baseline"), Comparison: filepath.Join(source, "baseline")}, filepath.Join(t.TempDir(), "same")); err == nil {
		t.Fatal("a run was compared with itself")
	}
	output := filepath.Join(t.TempDir(), "review")
	review, err := report.ExportDocumentReview(ctx, packet, output, report.Authored{Title: "Reschedule regression", Notes: "Seen in QA.\nConclusion: the receiver duplicates."})
	if err != nil {
		t.Fatal(err)
	}
	doc := review.Document
	if review.Manifest.Schema != report.ReviewSchemaV3 || doc == nil || doc.Schema != report.DocumentSchema {
		t.Fatalf("not a structured review: %+v", review.Manifest)
	}
	if doc.Result.Outcome != report.OutcomeFailed || len(doc.Runs) != 2 || doc.Runs[0].Role != report.CurrentRole || doc.Runs[1].Result.Outcome != report.OutcomePassed {
		t.Fatalf("runs: %+v %+v", doc.Result, doc.Runs)
	}
	if len(doc.Checks) == 0 || doc.Checks[0].Result != testrunner.Failed || doc.Checks[0].Observed == nil {
		t.Fatalf("a failed check is not first with its observation: %+v", doc.Checks)
	}
	types := []string{}
	for _, message := range doc.Messages {
		types = append(types, message.Code+" "+message.Trigger)
		if message.Delivery != report.DeliveryAcknowledged || message.ACKCode != "AA" {
			t.Fatalf("message: %+v", message)
		}
	}
	if strings.Join(types, ",") != "SIU S12,SIU S13" {
		t.Fatalf("message types %v", types)
	}
	if doc.Comparison == nil || !doc.Comparison.SameCase || doc.Comparison.Specification != "unchanged" {
		t.Fatalf("comparison: %+v", doc.Comparison)
	}
	changed := false
	for _, check := range doc.Comparison.Checks {
		if check.Definition != "unchanged" {
			t.Fatalf("an unchanged test reports a changed definition: %+v", check)
		}
		changed = changed || check.Before != check.After
	}
	if !changed || doc.Notes == nil || !strings.Contains(*doc.Notes, "Conclusion") {
		t.Fatal("before and after or notes lost")
	}

	// Every rendering states the same verdicts, identities, values and
	// limitations.
	html := string(read(t, filepath.Join(output, "report.html")))
	markdown := string(read(t, filepath.Join(output, "report.md")))
	junit := read(t, filepath.Join(output, "junit.xml"))
	pdf := pdfText(read(t, filepath.Join(output, "report.pdf")))
	var suite struct {
		Failures int    `xml:"failures,attr"`
		Errors   int    `xml:"errors,attr"`
		Content  string `xml:"system-out"`
	}
	if err := xml.Unmarshal(junit, &suite); err != nil || suite.Failures == 0 || suite.Errors != 0 {
		t.Fatalf("JUnit: %v %+v", err, suite)
	}
	var decoded report.Document
	if err := json.Unmarshal(read(t, filepath.Join(output, "report.json")), &decoded, json.RejectUnknownMembers(true)); err != nil || !reflect.DeepEqual(decoded, *doc) {
		t.Fatalf("JSON: %v", err)
	}
	facts := []string{"Reschedule regression", "Failed", "Passed", doc.PacketIdentity, doc.Runs[0].ResultIdentity, doc.Runs[1].ResultIdentity, "SIU S12", "Record count", "Changed definitions"[:7]}
	facts = append(facts, doc.Limitations...)
	for _, fact := range facts {
		for format, text := range map[string]string{"html": html, "markdown": unescapeMarkdown(markdown), "junit": suite.Content, "pdf": pdf} {
			if format == "pdf" {
				if !strings.Contains(squeeze(text), squeeze(fact)) {
					t.Errorf("%s lost %q", format, fact)
				}
				continue
			}
			if !strings.Contains(text, fact) {
				t.Errorf("%s lost %q", format, fact)
			}
		}
	}
	if strings.Count(html, "<pre>") != 0 || !strings.Contains(html, "<h2>Checks</h2>") || !strings.Contains(html, "<thead>") {
		t.Fatal("HTML is not a structured document")
	}
	if strings.Contains(markdown, "    READMIT") || !strings.Contains(markdown, "| Check | Expected | Observed | Result |") {
		t.Fatal("Markdown is not a structured document")
	}

	// A changed verdict is refused however the seal is recomputed.
	for name, change := range map[string][2]string{
		"report.json": {`"outcome":"failed"`, `"outcome":"passed"`},
		"report.html": {"<h2>Result</h2>\n<p>Failed</p>", "<h2>Result</h2>\n<p>Passed</p>"},
		"report.md":   {"## Result\n\nFailed", "## Result\n\nPassed"},
	} {
		dir := clone(t, output)
		data := read(t, filepath.Join(dir, name))
		if !bytes.Contains(data, []byte(change[0])) {
			t.Fatalf("%s does not state %q", name, change[0])
		}
		write(t, filepath.Join(dir, name), bytes.Replace(data, []byte(change[0]), []byte(change[1]), 1))
		resealReview(t, dir)
		if _, err := report.OpenReview(ctx, dir); err == nil {
			t.Fatalf("accepted a resealed changed %s", name)
		}
	}
	// A renderer version other than the one bound is refused.
	dir := clone(t, output)
	manifest := bytes.Replace(read(t, filepath.Join(dir, "manifest.json")), []byte(`"renderer":"readmit-portable-report/v3"`), []byte(`"renderer":"readmit-portable-report/v1"`), 1)
	write(t, filepath.Join(dir, "manifest.json"), manifest)
	write(t, filepath.Join(dir, "identity.sha256"), []byte(sha(manifest)+"\n"))
	if _, err := report.OpenReview(ctx, dir); err == nil {
		t.Fatal("accepted a review under another renderer version")
	}

	// Rewriting what the person wrote changes no run evidence or outcome.
	again, err := report.ExportDocumentReview(ctx, packet, filepath.Join(t.TempDir(), "again"), report.Authored{Title: "Renamed"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Document.PacketIdentity != doc.PacketIdentity || again.Document.Result != doc.Result || again.Document.Notes != nil || again.Document.Title != "Renamed" {
		t.Fatal("authored change reached the evidence")
	}
	a4, err := review.Render("pdf-a4")
	if err != nil || !bytes.Contains(a4, []byte("/MediaBox [0 0 595.28 841.89]")) || !bytes.Contains(read(t, filepath.Join(output, "report.pdf")), []byte("/MediaBox [0 0 612 792]")) {
		t.Fatal("paper sizes")
	}

	// The command line exports the structured review by default.
	var stdout, stderr bytes.Buffer
	cliReview := filepath.Join(t.TempDir(), "cli")
	if err := cli.Execute("test", []string{"report", "export", packet, "--output", cliReview, "--title", "From the command line"}, &stdout, &stderr); err != nil {
		t.Fatal(err, stderr.String())
	}
	opened, err := report.OpenReview(ctx, cliReview)
	if err != nil || opened.Document == nil || opened.Document.Title != "From the command line" {
		t.Fatalf("CLI export: %v", err)
	}
	if def, err := report.ExportReview(ctx, packet, filepath.Join(t.TempDir(), "default")); err != nil || def.Document.Title != report.DefaultTitle(packet) || !strings.HasSuffix(def.Document.Title, " report") {
		t.Fatalf("default title: %v", err)
	}
}

// An incomplete or uncertain lifecycle never renders as passed, and an
// unobserved value is unavailable, never zero, in every format.
func TestReportNeverPassesAnUnusableRunOrInventsZero(t *testing.T) {
	t.Parallel()
	for _, facts := range []struct {
		state                     string
		journal, delivery, passed bool
	}{
		{runresult.NoRunState, false, false, true},
		{runresult.NoRunState, false, true, false},
		{runresult.NoRunState, true, false, false},
		{"running", false, false, false},
	} {
		if got := report.OutcomeOf("pass", facts.state, facts.journal, facts.delivery); (got == report.OutcomePassed) != facts.passed {
			t.Fatalf("%+v reads as %s", facts, got)
		}
	}
	zero := 0
	doc := sampleDocument()
	doc.Result = report.DocumentResult{Outcome: report.OutcomeIncomplete, Status: "pass", DeliveryUncertain: true}
	doc.Checks = []report.DocumentCheck{
		{ID: "a", Operator: "ledger_count", Result: testrunner.Passed, Expected: testrunner.Value{Count: &zero}, Observed: &testrunner.Value{Count: &zero}, Messages: []string{}},
		{ID: "b", Operator: report.ACKCheck, Selector: "MSA-1", Message: "m1", Result: testrunner.NotEvaluated, Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Omitted}},
			Unavailable: "no readable acknowledgement was retained for this message", Messages: []string{"m1"}},
	}
	html := string(mustRender(t, doc, "html"))
	if !strings.Contains(html, "<h2>Result</h2>\n<p>Incomplete</p>\n<p>Delivery uncertain</p>") || strings.Contains(html, "<h2>Result</h2>\n<p>Passed") {
		t.Fatal("an uncertain run rendered as passed")
	}
	if !strings.Contains(html, "<td>0</td><td>0</td>") || !strings.Contains(html, "Unavailable: no readable acknowledgement") || !strings.Contains(html, "Not present") {
		t.Fatal("a real zero, an unavailable value or an omitted field was lost")
	}
	if pdf := squeeze(pdfText(mustRender(t, doc, "pdf"))); !strings.Contains(pdf, "Incomplete") || !strings.Contains(pdf, squeeze("Unavailable: no readable")) {
		t.Fatal("PDF lost the lifecycle or the unavailable value")
	}
	var suite struct {
		Errors int `xml:"errors,attr"`
	}
	if err := xml.Unmarshal(mustRender(t, doc, "junit"), &suite); err != nil || suite.Errors != 2 {
		t.Fatalf("JUnit must error on the run and the unevaluated check: %v %+v", err, suite)
	}
}

// Unicode, control characters and adversarial markup survive every format as
// inert, visible text.
func TestReportEscapesUnicodeControlsAndMarkupInEveryFormat(t *testing.T) {
	t.Parallel()
	hostile := "</p><script>alert(1)</script><img src=x onerror=alert(1)>[x](javascript:alert(1)) https://evil.example | a\x00b\x1bc‮d 雪 \\ ``` #"
	if got := report.Escape("a\\b\x00\x1b‮\xff雪"); got != `a\\b\u{0000}\u{001b}\u{202e}\xff雪` {
		t.Fatalf("escape: %s", got)
	}
	doc := sampleDocument()
	doc.Title = "Report " + hostile
	notes := hostile
	doc.Notes = &notes
	long := strings.Repeat("0123456789", 30) + hostile
	doc.Checks = []report.DocumentCheck{{ID: "h", Operator: report.ACKCheck, Selector: "MSA-3", Message: "m1", Result: testrunner.Failed,
		Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &hostile}},
		Observed: &testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &long}}, Messages: []string{"m1"}}}

	html := string(mustRender(t, doc, "html"))
	for _, forbidden := range []string{"<script", "<img", "<a ", "href=", "src=\"", "<iframe", "<form", "<link", "@import", "url("} {
		if strings.Contains(strings.ToLower(html), forbidden) {
			t.Fatalf("HTML carries %q", forbidden)
		}
	}
	for _, kept := range []string{"&lt;script&gt;", `\u{0000}`, `\u{001b}`, `\u{202e}`, "雪", `\\`, "See appendix A1", "<pre>"} {
		if !strings.Contains(html, kept) {
			t.Fatalf("HTML lost %q", kept)
		}
	}
	markdown := string(mustRender(t, doc, "markdown"))
	outside := regexp.MustCompile("(?s)````+text\n.*?\n````+\n").ReplaceAllString(markdown, "")
	for i, r := range outside {
		if strings.ContainsRune("<>[]|`", r) && (i == 0 || outside[i-1] != '\\') && !strings.HasPrefix(outside[i:], "| ") && !strings.HasPrefix(outside[i:], "|\n") && !(r == '|' && i > 0 && outside[i-1] == ' ') {
			t.Fatalf("Markdown syntax %q unescaped at %d: %q", r, i, outside[max(0, i-20):min(len(outside), i+20)])
		}
	}
	if strings.Contains(outside, "https://") || !strings.Contains(markdown, `\u{0000}`) || !strings.Contains(markdown, "雪") {
		t.Fatal("Markdown autolink or lost Unicode/control escape")
	}
	pdf := mustRender(t, doc, "pdf")
	for _, b := range pdf {
		if b >= 0x80 || (b < 0x20 && b != '\n') {
			t.Fatalf("PDF carries byte %#x", b)
		}
	}
	for _, forbidden := range []string{"/JavaScript", "/JS", "/URI", "/Annots", "/Launch", "/EmbeddedFile", "/OpenAction", "/AA", "/AcroForm"} {
		if bytes.Contains(pdf, []byte(forbidden)) {
			t.Fatalf("PDF carries %s", forbidden)
		}
	}
	if text := pdfText(pdf); !strings.Contains(squeeze(text), squeeze(`\u{96ea}`)) || !strings.Contains(squeeze(text), squeeze(`<script>alert(1)</script>`)) {
		t.Fatal("PDF lost Unicode or markup text")
	}
	var suite struct {
		Name    string `xml:"name,attr"`
		Content string `xml:"system-out"`
	}
	if err := xml.Unmarshal(mustRender(t, doc, "junit"), &suite); err != nil || !strings.Contains(suite.Name, `\u{0000}`) || !strings.Contains(suite.Name, "<script>") {
		t.Fatalf("JUnit: %v %q", err, suite.Name)
	}
	var decoded report.Document
	if err := json.Unmarshal(mustRender(t, doc, "json"), &decoded, json.RejectUnknownMembers(true)); err != nil || decoded.Title != doc.Title || *decoded.Checks[0].Observed.Field.Text != long {
		t.Fatalf("JSON did not keep the exact values: %v", err)
	}
}

// A long report is paginated with repeated table headers and page numbers
// and wraps every value without dropping it.
func TestPDFPaginatesAndWrapsLongReports(t *testing.T) {
	t.Parallel()
	doc := sampleDocument()
	doc.Checks = nil
	for i := range 120 {
		text := strings.Repeat("wrapped value ", 8) + strings.Repeat("x", 40)
		doc.Checks = append(doc.Checks, report.DocumentCheck{ID: string(rune('a' + i%26)), Operator: report.ACKCheck, Selector: "MSA-1", Message: "m1", Result: testrunner.Passed,
			Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &text}}, Observed: &testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &text}}, Messages: []string{}})
	}
	records := []observation.Record{{RecordID: "r1", AppointmentStart: "2026-01-01T12:00:00Z"}}
	doc.Checks = append(doc.Checks, report.DocumentCheck{ID: "records", Operator: "ledger_equals", Result: testrunner.Passed, Expected: testrunner.Value{Records: &records}, Observed: &testrunner.Value{Records: &records}, Messages: []string{}})
	pdf := mustRender(t, doc, "pdf")
	pages := regexp.MustCompile(`/Type /Page /Parent`).FindAll(pdf, -1)
	if len(pages) < 3 {
		t.Fatalf("%d pages", len(pages))
	}
	for i := range pages {
		if !bytes.Contains(pdf, []byte("(Page "+strconv.Itoa(i+1)+" of "+strconv.Itoa(len(pages))+") Tj")) {
			t.Fatalf("page %d has no page number", i+1)
		}
	}
	if headers := bytes.Count(pdf, []byte("(Check) Tj")); headers < len(pages)-1 {
		t.Fatalf("table header repeated on %d of %d pages", headers, len(pages))
	}
	if text := squeeze(pdfText(pdf)); strings.Count(text, squeeze(strings.Repeat("x", 40))) < 240 || !strings.Contains(text, "r1") {
		t.Fatal("a wrapped value lost characters")
	}
	if !bytes.Contains(pdf, []byte("/BaseFont /Helvetica ")) || bytes.Contains(pdf, []byte("/Courier) Tj")) {
		t.Fatal("fonts")
	}
}

func sampleDocument() *report.Document {
	return &report.Document{Schema: report.DocumentSchema, Title: "Sample", PacketIdentity: strings.Repeat("a", 64), ExportPolicy: "customer-local-only", ContainsSourceValues: true,
		Result: report.DocumentResult{Outcome: report.OutcomePassed, Status: "pass", RunState: runresult.NoRunState},
		Runs:   []report.DocumentRun{{Role: report.CurrentRole, Test: "Sample test", Result: report.DocumentResult{Outcome: report.OutcomePassed}, CompletedAt: "2026-01-01T12:00:00Z", ResultIdentity: strings.Repeat("b", 64)}},
		Checks: []report.DocumentCheck{}, Messages: []report.DocumentMessage{{Source: "m1", Kind: string(bundle.Message), Code: "SIU", Trigger: "S12", Delivery: report.DeliveryAcknowledged, ACKCode: "AA"}},
		Limitations: []string{"Contains original source values."}, Evidence: []report.DocumentEvidence{}}
}

func mustRender(t *testing.T, doc *report.Document, format string) []byte {
	t.Helper()
	data, err := report.RenderDocument(doc, format)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

var pdfShow = regexp.MustCompile(`\(((?:\\.|[^\\)])*)\) Tj`)

var pageNumber = regexp.MustCompile(`^Page \d+ of \d+$`)

// pdfText is the text a PDF's content streams show, in order, without the
// page numbers between pages.
func pdfText(pdf []byte) string {
	parts := []string{}
	for _, match := range pdfShow.FindAllSubmatch(pdf, -1) {
		text := strings.NewReplacer(`\\`, `\`, `\(`, `(`, `\)`, `)`, `\267`, "·", `\227`, "—", `\226`, "–").Replace(string(match[1]))
		if !pageNumber.MatchString(text) {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func squeeze(text string) string { return strings.Join(strings.Fields(text), "") }

func unescapeMarkdown(text string) string {
	return regexp.MustCompile(`\\([[:punct:]])`).ReplaceAllString(text, "$1")
}

// resealReview recomputes a review's file index and seal after a change.
func resealReview(t *testing.T, dir string) {
	t.Helper()
	var manifest map[string]any
	if err := json.Unmarshal(read(t, filepath.Join(dir, "manifest.json")), &manifest); err != nil {
		t.Fatal(err)
	}
	files := snapshot(t, dir)
	names := []string{}
	for name := range files {
		if name != "manifest.json" && name != "identity.sha256" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	index := []bundle.Payload{}
	for _, name := range names {
		index = append(index, bundle.Payload{Path: name, Size: len(files[name]), SHA256: sha(files[name])})
	}
	manifest["files"] = index
	raw, err := json.Marshal(manifest, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	write(t, filepath.Join(dir, "manifest.json"), raw)
	write(t, filepath.Join(dir, "identity.sha256"), []byte(sha(raw)+"\n"))
}

// A comparison run of a test whose checks changed shows each changed
// definition apart and compares no behavior across it.
func TestReportComparisonShowsChangedDefinitionsApart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "source")
	if _, err := report.Create(ctx, report.Scenario, source); err != nil {
		t.Fatal(err)
	}
	text := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "Reschedule acknowledged", Input: testrunner.Input{Case: filepath.Join(source, "reproducer"), Messages: []string{"s0001-e000001"}},
		Target: filepath.Join(source, "post-fix-target.json"), Setup: testrunner.Setup{InitialState: testrunner.OperatorDeclared, ResetInstructions: "Read-only fixture"},
		Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "ack", Operator: report.ACKCheck, Message: "s0001-e000001", Selector: "MSA-1",
			Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &text}}}}}
	specPath := filepath.Join(t.TempDir(), "spec.json")
	write(t, specPath, marshal(t, spec))
	result := filepath.Join(t.TempDir(), "result")
	if _, err := testrunner.Run(ctx, specPath, result); err != nil {
		t.Fatal(err)
	}
	packet := filepath.Join(t.TempDir(), "packet")
	if _, err := report.AssembleRuns(ctx, report.RunsInput{Case: filepath.Join(source, "reproducer"), Current: result, Comparison: filepath.Join(source, "baseline")}, packet); err != nil {
		t.Fatal(err)
	}
	opened, err := report.OpenRetained(ctx, packet)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := report.BuildDocument(ctx, packet, opened, report.Authored{Title: "Changed checks"})
	if err != nil {
		t.Fatal(err)
	}
	definitions := map[string]string{}
	for _, check := range doc.Comparison.Checks {
		definitions[check.ID] = check.Definition
		if check.Definition != "unchanged" && (check.Before == check.After) {
			t.Fatalf("a changed definition compared as a behavior: %+v", check)
		}
	}
	if doc.Comparison.Specification != "changed" || definitions["ack"] != "added" || len(definitions) < 2 {
		t.Fatalf("the definitions: %+v %v", doc.Comparison, definitions)
	}
	html := string(mustRender(t, doc, "html"))
	if !strings.Contains(html, "<h3>Changed definitions</h3>") || !strings.Contains(html, "Not in this run") || !strings.Contains(html, "<td>Added</td>") || !strings.Contains(html, "<td>Removed</td>") {
		t.Fatal("changed definitions are not shown apart")
	}
}
