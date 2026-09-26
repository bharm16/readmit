package report_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"encoding/xml"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/testrunner"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/report"
)

func TestReviewExportsAndVerifiesActualPacketOffline(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "source")
	if _, err := report.Create(context.Background(), report.Scenario, source); err != nil {
		t.Fatal(err)
	}
	packet := filepath.Join(t.TempDir(), "packet")
	if _, err := report.Assemble(context.Background(), report.RetainedInput{Case: filepath.Join(source, "reproducer"), Spec: filepath.Join(source, "spec.json"), Current: filepath.Join(source, "post-fix"), Baseline: filepath.Join(source, "baseline")}, packet); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, packet)
	output := filepath.Join(t.TempDir(), "review")
	exported, err := report.ExportReview(context.Background(), packet, output)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot(t, filepath.Join(output, "packet"))) {
		t.Fatal("changed retained bytes")
	}
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(output, moved); err != nil {
		t.Fatal(err)
	}
	opened, err := report.OpenReview(context.Background(), moved)
	if err != nil || opened.Identity != exported.Identity {
		t.Fatalf("offline review: %v", err)
	}
	for _, name := range []string{"report.html", "report.pdf", "report.md", "report.json", "junit.xml"} {
		raw := read(t, filepath.Join(moved, name))
		if len(raw) == 0 {
			t.Fatalf("empty %s", name)
		}
	}
	if !strings.Contains(string(read(t, filepath.Join(moved, "junit.xml"))), "<failure") {
		t.Fatal("baseline failure lost")
	}
	if !reflect.DeepEqual(before, snapshot(t, packet)) {
		t.Fatal("source changed")
	}
	changed := clone(t, moved)
	write(t, filepath.Join(changed, "report.html"), []byte("<script>alert(1)</script>"))
	if _, err := report.OpenReview(context.Background(), changed); err == nil {
		t.Fatal("accepted changed report")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := report.ExportReview(ctx, packet, filepath.Join(t.TempDir(), "cancelled")); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := report.ExportReview(context.Background(), packet, moved); err == nil {
		t.Fatal("overwrote review")
	}
	if _, err := report.ExportReview(context.Background(), packet, filepath.Join(packet, "nested")); err == nil {
		t.Fatal("modified source")
	}
}

func TestReviewEscapesHostileEvidenceAndRejectsResealedReports(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "source")
	if _, err := report.Create(ctx, report.Scenario, source); err != nil {
		t.Fatal(err)
	}
	malicious := "</pre><script>alert(1)</script>[link](https://example.invalid)\\()\x00\r\n雪"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "Hostile <script> literal", Input: testrunner.Input{Case: filepath.Join(source, "reproducer"), Messages: []string{"s0001-e000001"}}, Target: filepath.Join(source, "post-fix-target.json"), Setup: testrunner.Setup{InitialState: testrunner.OperatorDeclared, ResetInstructions: "Read-only fixture"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "hostile", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &malicious}}}}}
	specPath := filepath.Join(t.TempDir(), "spec.json")
	write(t, specPath, marshal(t, spec))
	result := filepath.Join(t.TempDir(), "result")
	if _, err := testrunner.Run(ctx, specPath, result); err != nil {
		t.Fatal(err)
	}
	packet := filepath.Join(t.TempDir(), "packet")
	if _, err := report.Assemble(ctx, report.RetainedInput{Case: filepath.Join(source, "reproducer"), Spec: specPath, Current: result}, packet); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "review")
	r, err := report.ExportReview(ctx, packet, output)
	if err != nil {
		t.Fatal(err)
	}
	html, err := r.Render("html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), "<script>") || strings.Contains(string(html), "</pre><script>") || !strings.Contains(string(html), "default-src 'none'") {
		t.Fatal("HTML active content")
	}
	md, _ := r.Render("markdown")
	for _, line := range strings.Split(strings.TrimSuffix(string(md), "\n"), "\n") {
		if !strings.HasPrefix(line, "    ") {
			t.Fatal("Markdown left code context")
		}
	}
	junit, _ := r.Render("junit")
	var suite struct {
		Errors  int    `xml:"errors,attr"`
		Content string `xml:"system-out"`
	}
	if err := xml.Unmarshal(junit, &suite); err != nil || suite.Errors != 1 {
		t.Fatalf("JUnit lost execution error: %v %+v", err, suite)
	}
	var doc struct {
		Schema               string   `json:"schema"`
		PacketIdentity       string   `json:"packet_identity"`
		ExportPolicy         string   `json:"export_policy"`
		ContainsSourceValues bool     `json:"contains_source_values"`
		Lines                []string `json:"lines"`
	}
	raw, _ := r.Render("json")
	if err := json.Unmarshal(raw, &doc, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if suite.Content != strings.Join(doc.Lines, "\n")+"\n" || !doc.ContainsSourceValues || doc.ExportPolicy != "customer-local-only" {
		t.Fatal("content or privacy mismatch")
	}
	if !strings.Contains(suite.Content, `\u96ea`) || !strings.Contains(suite.Content, `\\u0000`) {
		t.Fatal("Unicode or control evidence lost")
	}
	pdf, _ := r.Render("pdf")
	if !strings.HasPrefix(string(pdf), "%PDF-1.4") || strings.Contains(string(pdf), "/JavaScript") || strings.Contains(string(pdf), "/URI") {
		t.Fatal("invalid active PDF")
	}
	if _, err := r.Render("../../packet/spec.json"); err == nil {
		t.Fatal("arbitrary rendering")
	}
	before := snapshot(t, output)
	for _, mutation := range []string{"rendering", "extra", "unknown", "null", "omitted", "missing", "unsealed", "packet"} {
		t.Run(mutation, func(t *testing.T) {
			dir := clone(t, output)
			manifest := read(t, filepath.Join(dir, "manifest.json"))
			switch mutation {
			case "rendering":
				write(t, filepath.Join(dir, "report.html"), []byte("<script>bad</script>"))
			case "extra":
				write(t, filepath.Join(dir, "extra.html"), []byte("extra"))
			case "unknown":
				manifest = []byte(strings.Replace(string(manifest), `"state":"complete"`, `"state":"complete","unknown":true`, 1))
			case "null":
				manifest = []byte(strings.Replace(string(manifest), `"contains_source_values":true`, `"contains_source_values":null`, 1))
			case "omitted":
				manifest = []byte(strings.Replace(string(manifest), `"contains_source_values":true,`, "", 1))
			case "missing":
				os.Remove(filepath.Join(dir, "report.pdf"))
			case "unsealed":
				os.Remove(filepath.Join(dir, "identity.sha256"))
			case "packet":
				write(t, filepath.Join(dir, "packet", "spec.json"), []byte("{}"))
			}
			if mutation == "rendering" || mutation == "extra" {
				var m report.ReviewManifest
				if err := json.Unmarshal(manifest, &m); err != nil {
					t.Fatal(err)
				}
				files := snapshot(t, dir)
				m.Files = nil
				names := []string{}
				for name := range files {
					if name != "manifest.json" && name != "identity.sha256" {
						names = append(names, name)
					}
				}
				sort.Strings(names)
				for _, name := range names {
					m.Files = append(m.Files, bundle.Payload{Path: name, Size: len(files[name]), SHA256: sha(files[name])})
				}
				manifest = marshal(t, m)
			}
			if mutation != "unsealed" {
				write(t, filepath.Join(dir, "manifest.json"), manifest)
				write(t, filepath.Join(dir, "identity.sha256"), []byte(sha(manifest)+"\n"))
			}
			if _, err := report.OpenReview(ctx, dir); err == nil {
				t.Fatal("accepted altered or invented report")
			}
		})
	}
	if !reflect.DeepEqual(before, snapshot(t, output)) {
		t.Fatal("review modified evidence")
	}
	// Explicit read-only command opens no target and emits no values by default.
	var stdout, stderr bytes.Buffer
	if code := cli.Execute("test", []string{"report", "review", output}, &stdout, &stderr); code != nil {
		t.Fatalf("CLI: %v %s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "Hostile") || strings.Contains(stdout.String(), "example.invalid") {
		t.Fatal("default console exposed values")
	}
}

func TestReviewCLIFormatsAndIncompleteRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "source")
	if _, err := report.Create(ctx, report.Scenario, source); err != nil {
		t.Fatal(err)
	}
	packet := filepath.Join(t.TempDir(), "packet")
	if _, err := report.Assemble(ctx, report.RetainedInput{Case: filepath.Join(source, "reproducer"), Spec: filepath.Join(source, "spec.json"), Current: filepath.Join(source, "post-fix")}, packet); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "review")
	var stdout, stderr bytes.Buffer
	if err := cli.Execute("test", []string{"report", "export", packet, "--output", output}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for format, name := range map[string]string{"html": "report.html", "pdf": "report.pdf", "markdown": "report.md", "json": "report.json", "junit": "junit.xml"} {
		stdout.Reset()
		stderr.Reset()
		if err := cli.Execute("test", []string{"report", "review", output, "--format", format}, &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(stdout.Bytes(), read(t, filepath.Join(output, name))) {
			t.Fatalf("CLI format mismatch: %s", format)
		}
	}
	for _, args := range [][]string{{"report", "review", output, "--format", "shell"}, {"report", "review", output, "--send"}} {
		stdout.Reset()
		stderr.Reset()
		if err := cli.Execute("test", args, &stdout, &stderr); err == nil {
			t.Fatal("accepted unsupported action")
		}
	}
	corrupt := clone(t, packet)
	write(t, filepath.Join(corrupt, "spec.json"), []byte("{}"))
	incomplete := filepath.Join(t.TempDir(), "incomplete")
	if _, err := report.ExportReview(ctx, corrupt, incomplete); err == nil {
		t.Fatal("accepted invalid source")
	}
	if _, err := report.OpenReview(ctx, incomplete); err == nil {
		t.Fatal("accepted incomplete output")
	}
	if _, err := report.ExportReview(ctx, packet, incomplete); err == nil {
		t.Fatal("overwrote incomplete output")
	}
	if _, err := report.ExportReview(ctx, packet, filepath.Join(t.TempDir(), "recovery")); err != nil {
		t.Fatal(err)
	}
}
