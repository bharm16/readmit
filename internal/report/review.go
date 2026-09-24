package report

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testrunner"
)

const ReviewSchema = "readmit-portable-review/v1"
const ReportSchema = "readmit-portable-report/v1"

// ReviewManifest binds every rendering to the unchanged, independently verified
// retained packet. A seal proves integrity, never disclosure approval.
type ReviewManifest struct {
	Schema               string           `json:"schema"`
	State                string           `json:"state"`
	PacketIdentity       string           `json:"packet_identity"`
	ExportPolicy         string           `json:"export_policy"`
	ContainsSourceValues bool             `json:"contains_source_values"`
	Files                []bundle.Payload `json:"files"`
}

// Review has no execute or mutate method. Render returns fresh bytes only for
// known inert formats, and cannot read a historical target or credential path.
type Review struct {
	Identity   string
	Manifest   ReviewManifest
	renderings map[string][]byte
}

func (r *Review) Render(format string) ([]byte, error) {
	name, ok := reviewFormats[format]
	if !ok {
		return nil, errors.New("unsupported report format")
	}
	return bytes.Clone(r.renderings[name]), nil
}

var reviewFormats = map[string]string{"html": "report.html", "pdf": "report.pdf", "markdown": "report.md", "json": "report.json", "junit": "junit.xml"}

type portableReport struct {
	Schema               string   `json:"schema"`
	PacketIdentity       string   `json:"packet_identity"`
	ExportPolicy         string   `json:"export_policy"`
	ContainsSourceValues bool     `json:"contains_source_values"`
	Lines                []string `json:"lines"`
}

// ExportReview creates a new private sealed directory, without changing or
// transmitting evidence. Interrupted output remains unsealed; retry elsewhere.
func ExportReview(ctx context.Context, source, output string) (*Review, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, err := artifactpath.Directory(source)
	if err != nil {
		return nil, err
	}
	// Snapshot first, then validate the copied bytes rather than trusting an
	// earlier successful read of a source which another process could change.
	files, err := readTree(source)
	if err != nil {
		return nil, err
	}
	nested := make(map[string][]byte, len(files))
	for name, data := range files {
		nested["packet/"+name] = data
	}
	if err := reviewBounds(nested); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	review, err := artifactdir.Create(output, reviewFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer review.Close()
	dir := review.Path()
	if err := copyFiles(review, nested, "", ""); err != nil {
		return nil, err
	}
	packet, err := OpenRetained(ctx, filepath.Join(dir, "packet"))
	if err != nil {
		return nil, err
	}
	renders, err := reviewRenderings(ctx, dir, packet, nested)
	if err != nil {
		return nil, err
	}
	for name, data := range renders {
		nested[name] = data
	}
	manifest := reviewManifest(packet, nested)
	raw, err := encode(manifest)
	if err != nil {
		return nil, err
	}
	nested["manifest.json"] = raw
	nested["identity.sha256"] = []byte(digest(raw) + "\n")
	if err := reviewBounds(nested); err != nil {
		return nil, err
	}
	for name, data := range renders {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := review.WriteFile(name, data); err != nil {
			return nil, err
		}
	}
	if err := review.WriteFile("manifest.json", raw); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := review.Seal(nil); err != nil {
		return nil, err
	}
	return OpenReview(ctx, dir)
}

// OpenReview validates the complete snapshot and regenerates every rendering.
// Resealing an injected link/script or an invented verdict cannot bypass it.
func OpenReview(ctx context.Context, dir string) (*Review, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := artifactpath.Directory(dir)
	if err != nil {
		return nil, err
	}
	files, err := readTree(dir)
	if err != nil {
		return nil, err
	}
	invalid := errors.New("invalid, incomplete, changed or unsupported portable review")
	raw := files["manifest.json"]
	var stored ReviewManifest
	if len(raw) > 1<<20 || string(files["identity.sha256"]) != digest(raw)+"\n" || json.Unmarshal(raw, &stored, json.RejectUnknownMembers(true)) != nil {
		return nil, invalid
	}
	packet, err := OpenRetained(ctx, filepath.Join(dir, "packet"))
	if err != nil {
		return nil, err
	}
	expected := reviewManifest(packet, files)
	canonical, err := encode(expected)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, invalid
	}
	renders, err := reviewRenderings(ctx, dir, packet, files)
	if err != nil {
		return nil, err
	}
	for name, data := range renders {
		if !bytes.Equal(files[name], data) {
			return nil, invalid
		}
	}
	for name := range files {
		if name == "manifest.json" || name == "identity.sha256" || strings.HasPrefix(name, "packet/") {
			continue
		}
		if _, ok := renders[name]; !ok {
			return nil, invalid
		}
	}
	return &Review{Identity: digest(raw), Manifest: expected, renderings: renders}, nil
}
func reviewManifest(p *RetainedPacket, files map[string][]byte) ReviewManifest {
	return ReviewManifest{Schema: ReviewSchema, State: "complete", PacketIdentity: p.Identity, ExportPolicy: p.Manifest.ExportPolicy, ContainsSourceValues: p.Manifest.ContainsSourceValues, Files: index(files)}
}
func reviewBounds(files map[string][]byte) error {
	total := 0
	for name, data := range files {
		total += len(data)
		if len(files) > maxFiles || len(data) > maxFileBytes || total > maxPacketBytes || len(name) > 200 || strings.Count(name, "/") > 5 {
			return errors.New("portable review exceeds file, path or byte limits")
		}
	}
	return nil
}
func reviewRenderings(ctx context.Context, dir string, p *RetainedPacket, files map[string][]byte) (map[string][]byte, error) {
	doc := portableReport{Schema: ReportSchema, PacketIdentity: p.Identity, ExportPolicy: p.Manifest.ExportPolicy, ContainsSourceValues: p.Manifest.ContainsSourceValues, Lines: []string{
		"READMIT - RETAINED INVESTIGATION REVIEW",
		"Customer-local sensitive evidence. No disclosure approval or de-identification.",
		"Read-only review: no transmit, reset, execute or evidence modification.",
		"Values use quoted ASCII escapes (including Unicode); no value is omitted.",
		"Packet identity: " + p.Identity,
		"Verify offline: readmit report review REVIEW_DIRECTORY",
		"Schemas: readmit-portable-review/v1, readmit-portable-report/v1,",
		"readmit-retained-packet/v1; nested evidence retains its original schema.",
		"Hashes prove integrity, not authenticity, disclosure approval or causality.",
	}}
	for _, name := range []string{"SUMMARY.md", "RERUN.md"} {
		doc.Lines = append(doc.Lines, "", name)
		for _, line := range strings.Split(string(files["packet/"+name]), "\n") {
			doc.Lines = append(doc.Lines, strconv.QuoteToASCII(line))
		}
	}
	runs := []reviewRun{}
	names := []string{"current"}
	if p.Manifest.Baseline != nil {
		names = append(names, "baseline")
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		prefix := retainedResultPrefix(files, "packet/"+name)
		artifact, err := testrunner.Open(filepath.Join(dir, filepath.FromSlash(prefix)))
		if err != nil {
			return nil, err
		}
		meta := p.Manifest.Current
		if name == "baseline" {
			meta = *p.Manifest.Baseline
		}
		run := reviewRun{Name: name, Status: string(artifact.Result.Status)}
		if usable, _ := runresult.UsableLifecycle(meta.RunState, meta.JournalIncomplete, meta.DeliveryUncertain); !usable {
			run.Status = "execution_error"
		}
		runs = append(runs, run)
		doc.Lines = append(doc.Lines, "", "Run: "+name, "Retained result (assertions, expected/observed values and evidence references):")
		for _, section := range []struct {
			title string
			value any
		}{
			{"Result", artifact.Result}, {"Historical specification and setup instructions", artifact.Spec}, {"Replay events and timing", artifact.Run.Events},
		} {
			doc.Lines = append(doc.Lines, section.title)
			raw, err := json.Marshal(section.value, json.Deterministic(true), jsontext.WithIndent("  "))
			if err != nil {
				return nil, err
			}
			for _, line := range strings.Split(string(raw), "\n") {
				doc.Lines = append(doc.Lines, strconv.QuoteToASCII(line))
			}
		}
		doc.Lines = append(doc.Lines, "Evidence: "+prefix+"/result.json")
	}
	return renderPortable(doc, runs)
}

func printableLines(lines []string) []string {
	result := []string{}
	for _, line := range lines {
		for len(line) > 96 {
			result = append(result, line[:96])
			line = line[96:]
		}
		result = append(result, line)
	}
	return result
}

// pdfReport writes only a fixed PDF object graph and escaped printable text.
// It deliberately supports no annotation, URL, embedded file, form or action.
func pdfReport(lines []string) []byte {
	lines = printableLines(lines)
	pages := (len(lines) + 49) / 50
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "", "<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>"}
	kids := []string{}
	for page := 0; page < pages; page++ {
		pageID := len(objects) + 1
		contentID := pageID + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", pageID))
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", contentID))
		var content strings.Builder
		content.WriteString("BT /F1 8 Tf 12 TL 36 750 Td\n")
		end := min((page+1)*50, len(lines))
		for _, line := range lines[page*50 : end] {
			fmt.Fprintf(&content, "(%s) Tj T*\n", strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)").Replace(line))
		}
		fmt.Fprintf(&content, "ET\nBT /F1 8 Tf 36 30 Td (Readmit - sensitive evidence - page %d of %d) Tj ET\n", page+1, pages)
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", content.Len(), content.String()))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", pages, strings.Join(kids, " "))
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	start := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), start)
	return out.Bytes()
}
