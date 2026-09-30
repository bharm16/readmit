package report

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/runresult"
)

// reviewManifestV3Document is the readmit-portable-review/v3 manifest. Renderer binds
// the structured report version every rendering was made by, so a review is
// verified only by the renderer that made it.
type reviewManifestV3Document struct {
	Schema               string           `json:"schema"`
	State                string           `json:"state"`
	PacketIdentity       string           `json:"packet_identity"`
	Renderer             string           `json:"renderer"`
	ExportPolicy         string           `json:"export_policy"`
	ContainsSourceValues bool             `json:"contains_source_values"`
	Files                []bundle.Payload `json:"files"`
}

func reviewManifestV3(p *RetainedPacket, files map[string][]byte) reviewManifestV3Document {
	return reviewManifestV3Document{Schema: ReviewSchemaV3, State: "complete", PacketIdentity: p.Identity, Renderer: DocumentSchema,
		ExportPolicy: p.Manifest.ExportPolicy, ContainsSourceValues: p.Manifest.ContainsSourceValues, Files: index(files)}
}

// Formats a structured report renders as, and the file each is sealed in.
var documentFiles = map[string]string{"html": "report.html", "pdf": "report.pdf", "markdown": "report.md", "json": "report.json", "junit": "junit.xml"}

// RenderDocument renders one format of a structured report: html, pdf
// (Letter), pdf-a4, markdown, json or junit.
func RenderDocument(doc *Document, format string) ([]byte, error) {
	switch format {
	case "html":
		return renderHTML(doc), nil
	case "pdf":
		return renderPDF(doc, Letter), nil
	case "pdf-a4":
		return renderPDF(doc, A4), nil
	case "markdown":
		return renderMarkdown(doc), nil
	case "json":
		return encode(doc)
	case "junit":
		return renderJUnit(doc)
	}
	return nil, errors.New("unsupported report format")
}

// documentRenderings are what a v3 review seals beside its packet: the
// authored document and the five renderings of the structured report.
func documentRenderings(ctx context.Context, packetDir string, packet *RetainedPacket, authored Authored) (map[string][]byte, error) {
	raw, err := EncodeAuthored(authored)
	if err != nil {
		return nil, err
	}
	authored.Schema = AuthoredSchema
	doc, err := BuildDocument(ctx, packetDir, packet, authored)
	if err != nil {
		return nil, err
	}
	renders := map[string][]byte{"authored.json": raw}
	for format, name := range documentFiles {
		data, err := RenderDocument(doc, format)
		if err != nil {
			return nil, err
		}
		renders[name] = data
	}
	return renders, nil
}

// DefaultTitle is the title a report of a packet starts with: the name of
// the test its current run executed, followed by "report".
func DefaultTitle(packetDir string) string {
	if opened, err := runresult.Open(filepath.Join(packetDir, "current")); err == nil && opened.Spec != nil {
		return TitleFor(opened.Spec.Name)
	}
	return TitleFor("")
}

// TitleFor is the title a report of a run of test starts with: the test's
// name followed by "report", or "Run report" when that is empty or too long.
func TitleFor(test string) string {
	name := strings.Join(strings.Fields(test), " ")
	if title := name + " report"; name != "" && len(title) <= MaxTitleBytes {
		return title
	}
	return "Run report"
}

// openReviewV3 verifies a v3 review: its canonical manifest and seal, the
// packet through the retained verifier, the authored document strictly, and
// every rendering regenerated from those two by the bound renderer, so a
// changed verdict, value or limitation is refused even when every hash was
// recomputed.
func openReviewV3(ctx context.Context, dir string, files map[string][]byte) (*Review, error) {
	invalid := errors.New("invalid, incomplete, changed or unsupported portable review")
	raw := files["manifest.json"]
	var stored reviewManifestV3Document
	if string(files["identity.sha256"]) != digest(raw)+"\n" || json.Unmarshal(raw, &stored, json.RejectUnknownMembers(true)) != nil || stored.Renderer != DocumentSchema {
		return nil, invalid
	}
	packet, err := OpenRetained(ctx, filepath.Join(dir, "packet"))
	if err != nil {
		return nil, err
	}
	authored, err := DecodeAuthored(files["authored.json"])
	if err != nil {
		return nil, invalid
	}
	if canonical, err := EncodeAuthored(authored); err != nil || !bytes.Equal(canonical, files["authored.json"]) {
		return nil, invalid
	}
	canonical, err := encode(reviewManifestV3(packet, files))
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, invalid
	}
	doc, err := BuildDocument(ctx, filepath.Join(dir, "packet"), packet, authored)
	if err != nil {
		return nil, err
	}
	renders := map[string][]byte{}
	for format, name := range documentFiles {
		data, err := RenderDocument(doc, format)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(files[name], data) {
			return nil, invalid
		}
		renders[name] = data
	}
	for name := range files {
		if name == "manifest.json" || name == "identity.sha256" || name == "authored.json" || strings.HasPrefix(name, "packet/") {
			continue
		}
		if _, ok := renders[name]; !ok {
			return nil, invalid
		}
	}
	manifest := ReviewManifest{Schema: ReviewSchemaV3, State: stored.State, PacketIdentity: packet.Identity, ExportPolicy: packet.Manifest.ExportPolicy,
		ContainsSourceValues: packet.Manifest.ContainsSourceValues, Files: stored.Files}
	return &Review{Identity: digest(raw), Manifest: manifest, Document: doc, Authored: &authored, renderings: renders}, nil
}

// DocumentReviewFiles are the files ExportDocumentReview seals for the
// packet at source, byte for byte, held in memory rather than written: a
// share that encrypts original evidence packs them without a plaintext copy.
// The packet is read, verified in place, rendered and read again; a packet
// whose bytes changed meanwhile is refused.
func DocumentReviewFiles(ctx context.Context, source string, authored Authored) (map[string][]byte, error) {
	source, err := artifactpath.Directory(source)
	if err != nil {
		return nil, err
	}
	files, err := readTree(source)
	if err != nil {
		return nil, err
	}
	nested := make(map[string][]byte, len(files)+8)
	for name, data := range files {
		nested["packet/"+name] = data
	}
	if err := reviewBounds(nested); err != nil {
		return nil, err
	}
	packet, err := OpenRetained(ctx, source)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(authored.Title) == "" {
		authored.Title = DefaultTitle(source)
	}
	renders, err := documentRenderings(ctx, source, packet, authored)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	again, err := readTree(source)
	if err != nil || !sameTree(files, again) {
		return nil, errors.New("the retained evidence changed while it was read")
	}
	for name, data := range renders {
		nested[name] = data
	}
	raw, err := encode(reviewManifestV3(packet, nested))
	if err != nil {
		return nil, err
	}
	nested["manifest.json"] = raw
	nested["identity.sha256"] = []byte(digest(raw) + "\n")
	if err := reviewBounds(nested); err != nil {
		return nil, err
	}
	return nested, nil
}

func sameTree(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for name, data := range a {
		if other, ok := b[name]; !ok || !bytes.Equal(data, other) {
			return false
		}
	}
	return true
}
