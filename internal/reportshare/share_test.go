package reportshare_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/reportshare"
)

type fixture struct {
	dir      string
	packet   *report.RetainedPacket
	authored report.Authored
	policy   redact.Policy
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "source")
	if _, err := report.Create(ctx, report.Scenario, source); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "packet")
	if _, err := report.AssembleRuns(ctx, report.RunsInput{Case: filepath.Join(source, "reproducer"), Current: filepath.Join(source, "baseline"), Comparison: filepath.Join(source, "post-fix")}, dir); err != nil {
		t.Fatal(err)
	}
	packet, err := report.OpenRetained(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../testdata/fixtures/redact-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := redact.DecodePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{dir: dir, packet: packet, authored: report.Authored{Title: "Reschedule regression", Notes: "Seen with a real patient."}, policy: policy}
}

func (f fixture) build(t *testing.T, template *redact.Policy, overrides reportshare.Overrides, contents reportshare.Contents, key byte) *reportshare.Share {
	t.Helper()
	share, err := reportshare.Build(context.Background(), reportshare.Request{PacketDir: f.dir, Packet: f.packet, Authored: f.authored, Template: template,
		Overrides: overrides, Key: bytes.Repeat([]byte{key}, redact.MinDeriverKeyBytes), Contents: contents, Format: "markdown"})
	if err != nil {
		t.Fatal(err)
	}
	return share
}

// originalValues are the values of the retained case's messages that the
// fixture policy derives: patient identifiers and names, which a redacted
// share never carries.
func (f fixture) originalValues(t *testing.T) []string {
	t.Helper()
	source, err := bundle.Open(filepath.Join(f.dir, "case"))
	if err != nil {
		t.Fatal(err)
	}
	deriver, err := redact.NewDeriver(f.policy, bytes.Repeat([]byte{1}, redact.MinDeriverKeyBytes))
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range source.Events {
		raw, _ := source.Raw(event.ID)
		if _, _, err := deriver.Message(event.ID, raw, event.Kind, event.Terminator); err != nil {
			t.Fatal(err)
		}
	}
	values := []string{}
	for _, term := range deriver.Terms() {
		if len(term) >= 4 {
			values = append(values, string(term))
		}
	}
	if len(values) == 0 {
		t.Fatal("the fixture policy replaces nothing")
	}
	return values
}

// Without a template nothing is claimed: every restated value keeps its
// original text and is unresolved, and the output holds source values.
func TestAShareWithoutATemplateClaimsNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	share := f.build(t, nil, reportshare.Overrides{}, reportshare.Contents{Messages: true}, 1)
	if share.Redacted || !share.SourceValues || len(share.Issues) == 0 {
		t.Fatalf("a share without a template claimed redaction: %+v", share.Issues)
	}
	for _, row := range share.Rows {
		if row.Result != reportshare.Unresolved {
			t.Fatalf("row %s is %s without a template", row.Field, row.Result)
		}
	}
	if len(share.Files) != 2 || share.Files[1].Name != "Messages.hl7" || !share.Folder() {
		t.Fatalf("files: %v", len(share.Files))
	}
	for _, value := range f.originalValues(t) {
		if !bytes.Contains(share.Files[0].Data, []byte(value)) && !bytes.Contains(share.Files[1].Data, []byte(value)) {
			continue
		}
		return
	}
	t.Fatal("an unredacted share holds none of the original values")
}

// A template derives the report's restated values from the same messages,
// reproducibly under one key, and the free text and metadata a person
// treats leave with their treatment; what remains unresolved is named.
func TestATemplateDerivesTheReportAndItsMessagesTogether(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	title := "Shared regression"
	overrides := reportshare.Overrides{Title: &title, Notes: &reportshare.ValueTreatment{Row: "notes", Remove: true}, Packet: []string{redact.Metadata}}
	contents := reportshare.Contents{Messages: true}
	share := f.build(t, &f.policy, overrides, contents, 1)
	again := f.build(t, &f.policy, overrides, contents, 1)
	other := f.build(t, &f.policy, overrides, contents, 2)
	if len(share.Files) != 2 || !bytes.Equal(share.Files[0].Data, again.Files[0].Data) || !bytes.Equal(share.Files[1].Data, again.Files[1].Data) {
		t.Fatal("the same draft generated different bytes")
	}
	if bytes.Equal(share.Files[1].Data, other.Files[1].Data) {
		t.Fatal("another key derived the same messages")
	}
	if share.Files[0].Name != "Shared regression.md" || share.Document.Notes != nil || share.Document.Title != title {
		t.Fatalf("title and notes: %q %v", share.Files[0].Name, share.Document.Notes)
	}
	report := string(share.Files[0].Data)
	if strings.Contains(report, "Reschedule regression") || strings.Contains(report, "real patient") {
		t.Fatal("replaced free text remains in the report")
	}
	derived := 0
	for _, row := range share.Rows {
		if row.Kind == reportshare.FieldRow && row.Result != reportshare.Unresolved {
			derived++
		}
	}
	if derived == 0 {
		t.Fatal("no field was derived")
	}
	for _, value := range f.originalValues(t) {
		residual := bytes.Contains(share.Files[0].Data, []byte(value)) || bytes.Contains(share.Files[1].Data, []byte(value))
		if residual && share.Redacted {
			t.Fatalf("a share claimed redaction while %q remains", value)
		}
	}
	if share.Redacted != !share.SourceValues {
		t.Fatalf("redacted %v with source values %v", share.Redacted, share.SourceValues)
	}
	if !share.Redacted {
		unresolved := false
		for _, row := range share.Rows {
			unresolved = unresolved || row.Result == reportshare.Unresolved
		}
		if !unresolved && len(share.Issues) == 0 {
			t.Fatal("a share that is not redacted names no reason")
		}
	}
}

// Attachments and original evidence are never called redacted, an
// unreadable attachment blocks every output until it is removed, and
// removing items takes them out of this share only.
func TestAttachmentsAndOriginalEvidenceAreNeverRedacted(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	share, err := reportshare.Build(context.Background(), reportshare.Request{PacketDir: f.dir, Packet: f.packet, Authored: f.authored, Template: &f.policy,
		Key: bytes.Repeat([]byte{1}, redact.MinDeriverKeyBytes), Format: "html",
		Contents:    reportshare.Contents{Attachments: true, Original: true},
		Attachments: []reportshare.Attachment{{Name: "scan.png", Data: []byte("image")}, {Name: "gone.txt", Unreadable: "missing"}}})
	if err != nil {
		t.Fatal(err)
	}
	if share.Redacted || !share.SourceValues || !share.Original || !share.Folder() {
		t.Fatalf("original evidence was called redacted: %+v", share)
	}
	blocking := false
	for _, issue := range share.Issues {
		blocking = blocking || issue.Blocking && strings.Contains(issue.Text, "gone.txt")
	}
	if !blocking {
		t.Fatalf("an unreadable attachment does not block: %+v", share.Issues)
	}
	removed, err := reportshare.Build(context.Background(), reportshare.Request{PacketDir: f.dir, Packet: f.packet, Authored: f.authored, Template: &f.policy,
		Key: bytes.Repeat([]byte{1}, redact.MinDeriverKeyBytes), Format: "html",
		Contents:    reportshare.Contents{Attachments: true, Original: true, Removed: []string{"attachment:gone.txt", "original"}},
		Attachments: []reportshare.Attachment{{Name: "scan.png", Data: []byte("image")}, {Name: "gone.txt", Unreadable: "missing"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range removed.Issues {
		if issue.Blocking {
			t.Fatalf("a removed item still blocks: %+v", issue)
		}
	}
	if removed.Original || len(removed.Files) != 2 || removed.Files[1].Name != "Attachments/scan.png" {
		t.Fatalf("removed items were kept: %v", removed.Files)
	}
}

// Once every restated value, the free text and the metadata are treated, a
// report-only share is redacted: it holds none of the original values.
func TestAFullyTreatedReportIsRedacted(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	title := "Shared regression"
	share := f.build(t, &f.policy, reportshare.Overrides{Title: &title, Notes: &reportshare.ValueTreatment{Row: "notes", Remove: true}, Packet: []string{redact.Metadata},
		Values: []reportshare.ValueTreatment{{Row: "value:Patient ID", Remove: true}, {Row: "value:Placer ID", Remove: true}, {Row: "value:Filler ID", Remove: true}}},
		reportshare.Contents{}, 2)
	if !share.Redacted || share.SourceValues || len(share.Issues) != 0 || share.Folder() {
		t.Fatalf("a fully treated report is not redacted: %+v", share.Issues)
	}
	for _, value := range f.originalValues(t) {
		if bytes.Contains(share.Files[0].Data, []byte(value)) {
			t.Fatalf("a redacted report holds %q", value)
		}
	}
}
