package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/reportshare"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
	"io/fs"
	"os"
	"path/filepath"
)

func (c *loadedCatalog) connectedReportView(item CatalogItem, record catalog.Item, backing reportBacking, reveal bool) (*ReportView, error) {
	doc, err := report.BuildConnectedReport(backing.connected)
	if err != nil {
		return nil, err
	}
	for i := range doc.Runs {
		for j := range doc.Runs[i].Phases {
			for k := range doc.Runs[i].Phases[j].Observations {
				o := &doc.Runs[i].Phases[j].Observations[k]
				if len(o.Records) > 200 {
					o.Records = o.Records[:200]
					doc.Limitations = append(doc.Limitations, "This reader previews the first 200 retained records per observation; the verified packet and export retain the complete bounded evidence.")
				}
			}
		}
	}
	if !reveal {
		for i := range doc.Runs {
			for j := range doc.Runs[i].Phases {
				phase := &doc.Runs[i].Phases[j]
				for k := range phase.Steps {
					phase.Steps[k].Request = ""
				}
				for k := range phase.Bindings {
					phase.Bindings[k].Value = ""
				}
				for k := range phase.Observations {
					for n := range phase.Observations[k].Records {
						for m := range phase.Observations[k].Records[n].Fields {
							phase.Observations[k].Records[n].Fields[m].Text = ""
						}
					}
				}
			}
		}
	}
	shown := &ReportView{Connected: &doc, Item: item, Form: backing.form, Revision: backing.revision, Current: backing.revision == "" || backing.revision == record.RevisionLabel(), Title: backing.authored.Title, Notes: backing.authored.Notes, Runs: []ReportRun{}, Checks: []RunCheck{}, Messages: []RunMessage{}, Limitations: doc.Limitations, Evidence: []report.DocumentEvidence{}, Packet: backing.connected.Identity, Versions: []ReportVersion{}, Review: "draft", Revealed: reveal}
	shown.Shares, _ = c.reportShares(record.ID)
	if backing.sources != nil {
		shown.Draft = &ReportDraft{Title: backing.authored.Title, Notes: backing.authored.Notes, Run: ItemRef{Kind: RunItem, ID: backing.sources.Runs[0].Run}, Job: backing.sources.Runs[0].Job}
		if len(backing.sources.Runs) > 1 {
			ref := ItemRef{Kind: RunItem, ID: backing.sources.Runs[1].Run}
			shown.Draft.Comparison = &ref
		}
	}
	return shown, nil
}

func (a *App) bindConnectedReportShare(ctx context.Context, request PrepareActionRequest, loaded *loadedCatalog, items []CatalogItem, record catalog.Item, backing reportBacking) (*boundAction, refusal) {
	options := *request.ReportShare
	mode := options.ConnectedMode
	if mode == "" {
		mode = "original-report"
	}
	if request.Action == SendReportAction || options.Encrypt != nil {
		return nil, refusal{Failed, "connected evidence uses the supported local reviewed export; remote send and encryption are unavailable here"}
	}
	if options.Template != "" || len(options.Overrides.Fields) > 0 || len(options.Overrides.Packet) > 0 || len(options.Overrides.Values) > 0 || options.Overrides.Title != nil || options.Overrides.Notes != nil {
		return nil, refusal{Failed, "HL7 share treatments do not apply to mixed connected evidence; choose its supported value-free extract"}
	}
	if options.Contents.Messages || options.Contents.Attachments || options.Contents.Original || len(options.Contents.Removed) > 0 {
		return nil, refusal{Failed, "connected report outputs do not attach original messages, resources or packets; inspect retained originals separately"}
	}
	files := []reportshare.File{}
	display := &ReportShareReview{ConnectedMode: mode, Report: backing.authored.Title, Version: backing.revision, Items: []reportshare.Item{}, Rows: []reportshare.Row{}, Issues: []reportshare.Issue{}, Templates: []ShareTemplate{}, Controls: []ShareControl{}, Projects: []string{}}
	bound := &reportShareBinding{item: record.ID, revision: backing.revision, title: backing.authored.Title, options: options, packetDir: backing.packetDir, authored: backing.authored}
	inventory, err := backing.connected.Inventory(ctx, backing.packetDir)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	sourceValues, redacted := true, false
	switch mode {
	case "original-report":
		if options.Paper != "" && options.Paper != "letter" {
			return nil, refusal{Failed, "this connected report renderer supports Letter PDF"}
		}
		raw, err := report.RenderConnectedReport(backing.connected, options.Format)
		if err != nil {
			return nil, refusal{Failed, err.Error()}
		}
		suffix := map[string]string{"json": "json", "html": "html", "markdown": "md", "junit": "xml", "pdf": "pdf"}[options.Format]
		files = append(files, reportshare.File{Name: "connected-report." + suffix, Kind: reportshare.ReportType, Data: raw})
	case "value-free-extract":
		if options.Format != "json" {
			return nil, refusal{Failed, "the supported connected value-free extract uses its sealed JSON folder"}
		}
		surfaces := map[string]string{}
		for _, surface := range report.DisclosureSurfaces {
			surfaces[surface] = "exclude"
		}
		policy, _ := json.Marshal(report.DisclosurePolicy{Schema: report.DisclosurePolicySchema, Surfaces: surfaces}, json.Deterministic(true))
		candidate, err := report.PrepareExtractPolicy(ctx, backing.packetDir, policy)
		if err != nil {
			return nil, refusal{Failed, err.Error()}
		}
		bound.connectedExtract = candidate
		sourceValues, redacted = false, len(candidate.Blocked) == 0
		files = append(files, reportshare.File{Name: "extract.json", Kind: reportshare.ReportType, Data: candidate.Bytes()})
		for _, reason := range candidate.Blocked {
			display.Issues = append(display.Issues, reportshare.Issue{Key: "connected-disclosure", Text: reason, Blocking: true})
		}
	default:
		return nil, refusal{Failed, "choose original report or value-free extract"}
	}
	for _, surface := range inventory {
		display.Items = append(display.Items, reportshare.Item{Key: surface.Surface, Type: "connected-evidence", Name: surface.Surface, Included: true})
		display.Rows = append(display.Rows, reportshare.Row{Key: surface.Surface, Kind: "connected-surface", Category: "Retained evidence", Field: surface.Surface, Occurrences: surface.Files, Treatment: map[bool]string{true: "exclude", false: "retain"}[mode == "value-free-extract"], Result: map[bool]string{true: "excluded", false: "original"}[mode == "value-free-extract"]})
	}
	share := &reportshare.Share{Document: &report.Document{Title: backing.authored.Title}, Files: files, SourceValues: sourceValues, Redacted: redacted}
	bound.share = share
	display.SourceValues, display.Redacted, display.Revealed = sourceValues, redacted, options.Reveal
	display.Output = shareOutput(share, options.Format, options)
	if bound.connectedExtract != nil {
		display.Output.Type = ShareOutputFolder
		display.Output.Name = "connected-extract"
	}
	parts := []string{string(request.Action), loaded.root, loaded.document.Project.ID, a.reviewer(), record.ID, backing.revision, backing.connected.Identity, canonicalJSON(options)}
	for _, file := range files {
		parts = append(parts, file.Name, digestOf(file.Data))
	}
	ready, reason := len(display.Issues) == 0, ""
	if !ready {
		reason = display.Issues[0].Text
	}
	place, known := a.shareDestination(options.Destination)
	if !known || place.name != display.Output.Name || place.folder != (display.Output.Type == ShareOutputFolder) {
		ready, reason = false, "choose where this exact output is written"
	} else {
		if _, err := os.Lstat(place.path); !errors.Is(err, fs.ErrNotExist) {
			ready, reason = false, "choose a new output name"
		}
		bound.path = place.path
		parts = append(parts, place.path)
		display.Destination = ShareDestinationView{Kind: "local", Name: filepath.Base(place.path), Location: filepath.Base(filepath.Dir(place.path))}
	}
	display.Consequence = "Exports the reviewed connected evidence to a new local output."
	if sourceValues {
		display.Consequence = "Exports a report containing original retained values; this is not a redacted extract."
	}
	return &boundAction{action: request.Action, origin: request, reportShare: bound, binding: binding(parts...), review: ActionReview{Items: items, Ready: ready, Refusal: reason, ReportShare: display, Destination: ReviewDestination{Output: display.Output.Name}}}, noRefusal
}

// Share drafts retain only authored choices, never destinations, previews,
// review tokens or execution permission. Legacy membership stays unchanged.
func validateReportShareDraft(retained EditorDraft) error {
	var members map[string]any
	if json.Unmarshal(retained.Content, &members) != nil {
		return errors.New("invalid report share draft")
	}
	allowed := map[string]bool{"report": true, "contents": true, "template": true, "overrides": true, "format": true, "paper": true, "project": true, "encrypt": true}
	if retained.ContentSchema == "readmit-report-share-draft/v2" {
		allowed["connectedMode"] = true
	}
	for member := range members {
		if !allowed[member] {
			return errors.New("unsupported report share draft member")
		}
	}
	if retained.ContentSchema == "readmit-report-share-draft/v2" {
		mode, ok := members["connectedMode"].(string)
		if !ok || mode != "original-report" && mode != "value-free-extract" {
			return errors.New("choose the retained connected output mode")
		}
	}
	return nil
}

// reportAssociations binds catalog links to the exact archived input pin in
// this report's verified packet. A copied/unlinked result contributes no test
// association, and this reads only bounded project publication sidecars.
func (c *loadedCatalog) reportAssociations(backing reportBacking, summary *ReportSummary) {
	if backing.sources == nil {
		return
	}
	seen := map[string]bool{}
	for i, source := range backing.sources.Runs {
		summary.SourceRuns = append(summary.SourceRuns, ItemRef{Kind: RunItem, ID: source.Run})
		index := c.document.Find(source.Run)
		if index < 0 || c.document.Items[index].Kind != string(RunItem) {
			continue
		}
		if source.Job != "" {
			if ref, held := c.reportSuiteJobTest(filepath.Join(c.root, c.document.Items[index].Entry), source.Job); held && !seen[ref.ID] {
				seen[ref.ID] = true
				summary.Tests = append(summary.Tests, ref)
			}
			continue
		}
		pin := ""
		if backing.connected != nil {
			if i == 0 {
				pin = backing.connected.Manifest.Current.Plan
			} else if backing.connected.Manifest.Baseline != nil {
				pin = backing.connected.Manifest.Baseline.Plan
			}
		} else if backing.packet != nil {
			role := report.CurrentRole
			if i > 0 {
				role = report.ComparisonRole
			}
			raw, err := backing.packet.RunSpecification(role)
			if err == nil {
				if spec, err := testrunner.DecodeSpec(raw); err == nil {
					pin = specDigest(spec)
				}
			}
		}
		if pin == "" {
			continue
		}
		origin, held, err := c.recordedRunOrigin(filepath.Join(c.root, c.document.Items[index].Entry), pin)
		if err == nil && held && origin.Source.Kind == TestItem && !seen[origin.Source.ID] {
			seen[origin.Source.ID] = true
			summary.Tests = append(summary.Tests, origin.Source)
		}
	}
}

func (c *loadedCatalog) reportSuiteJobTest(path, job string) (ItemRef, bool) {
	origin, held, err := c.recordedRunOrigin(path, fileDigest(filepath.Join(path, "suite.json")))
	if err != nil || !held || origin.Source.Kind != SuiteItem {
		return ItemRef{}, false
	}
	index := c.document.Find(origin.Source.ID)
	if index < 0 {
		return ItemRef{}, false
	}
	version, err := c.suiteVersion(c.document.Items[index], origin.Source.Revision)
	if err != nil || version.document == nil || digestOf(version.data) != origin.InputDigest {
		return ItemRef{}, false
	}
	execution, err := suite.OpenExecution(path)
	if err != nil {
		return ItemRef{}, false
	}
	test, err := execution.Suite.DeclaredJobTest(execution.Environment, job)
	if err != nil {
		return ItemRef{}, false
	}
	for _, selected := range version.draft.Tests {
		if selected.ID == test && selected.Test.Kind == TestItem && selected.Test.ID != "" {
			return selected.Test, true
		}
	}
	return ItemRef{}, false
}
