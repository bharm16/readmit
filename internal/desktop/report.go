package desktop

import (
	"path/filepath"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/report"
)

// The report readers of the catalog: a report the application saved
// (reports.go), and the investigation packets, portable reviews, export
// reviews and synthetic demonstration packets earlier releases and the
// command line wrote, each read through its own verifier.

func readReport(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	if item.Current() != nil {
		return readSavedReport(c, item)
	}
	path := paths[primaryRole(ReportItem)]
	manifest := filepath.Join(path, "manifest.json")
	switch {
	case declares(filepath.Join(path, "review.json"), redact.ReviewSchema):
		review, private, err := c.exportReview(path)
		if err != nil {
			return view{}, err
		}
		return view{summary: ItemSummary{Report: &ReportSummary{Form: "export-review", RelatedCase: c.caseByIdentity(private.caseIdentity), Status: review.State}}}, nil
	case declares(manifest, report.RetainedSchema):
		packet, err := report.OpenRetained(c.ctx, path)
		if err != nil {
			return view{}, err
		}
		status := "not-reviewed"
		if c.packetRelations().reviewed[packet.Identity] {
			status = "reviewed"
		}
		return view{summary: ItemSummary{Report: &ReportSummary{Form: "packet", RelatedCase: c.caseByIdentity(packet.Manifest.Current.CaseIdentity), Status: status}}}, nil
	case declares(manifest, report.ReviewSchema), declares(manifest, report.ReviewSchemaV3):
		review, err := report.OpenReview(c.ctx, path)
		if err != nil {
			return view{}, err
		}
		related := c.caseByIdentity(c.packetRelations().cases[review.Manifest.PacketIdentity])
		return view{summary: ItemSummary{Report: &ReportSummary{Form: "portable-review", RelatedCase: related, Status: "sealed"}}}, nil
	}
	packet, err := report.Open(path)
	if err != nil {
		return view{}, err
	}
	return view{name: packet.Manifest.Scenario, summary: ItemSummary{Report: &ReportSummary{Form: "synthetic-packet", RelatedCase: c.caseByIdentity(packet.Manifest.InputIdentity), Status: "sealed"}}}, nil
}

// relations are the verified relationships between the project's sealed
// packets and the portable reviews exported from them.
type relations struct {
	cases    map[string]string
	reviewed map[string]bool
}

// packetRelations reads, once per load, which case each sealed packet of the
// project is about and which packets a portable review of the project was
// exported from, each through its own reader.
func (c *loadedCatalog) packetRelations() relations {
	if c.packets != nil {
		return *c.packets
	}
	found := relations{cases: map[string]string{}, reviewed: map[string]bool{}}
	for _, item := range c.document.Items {
		if item.Kind != string(ReportItem) || item.Entry == "" {
			continue
		}
		path := filepath.Join(c.root, item.Entry)
		manifest := filepath.Join(path, "manifest.json")
		switch {
		case declares(manifest, report.RetainedSchema):
			if packet, err := report.OpenRetained(c.ctx, path); err == nil {
				found.cases[packet.Identity] = packet.Manifest.Current.CaseIdentity
			}
		case declares(manifest, report.ReviewSchema), declares(manifest, report.ReviewSchemaV3):
			if review, err := report.OpenReview(c.ctx, path); err == nil {
				found.reviewed[review.Manifest.PacketIdentity] = true
			}
		}
	}
	c.packets = &found
	return found
}

// PacketPathResult is one new folder named natively for a portable review.
type PacketPathResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Path   string `json:"path,omitzero"`
}

func (r *PacketPathResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }
