package desktop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/reportshare"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
)

const ExportSelectedMessagesAction ActionID = "capture.export-selected"
const SourceShareSchema = "readmit-report-share/v2"

type SelectedExportOptions struct {
	Messages       []string `json:"messages"`
	Identity       string   `json:"identity"`
	Transformation string   `json:"transformation"`
	Destination    string   `json:"destination,omitzero"`
	Reveal         bool     `json:"reveal,omitzero"`
}

type SelectedExportReview struct {
	Source         ItemRef                          `json:"source"`
	SourceName     string                           `json:"source_name"`
	SourceIdentity string                           `json:"source_identity"`
	Occurrences    []reportshare.SelectedOccurrence `json:"occurrences"`
	SourceValues   bool                             `json:"source_values"`
	Redacted       bool                             `json:"redacted"`
	Class          string                           `json:"class"`
	Output         ShareOutput                      `json:"output"`
	Destination    ShareDestinationView             `json:"destination"`
	Consequence    string                           `json:"consequence"`
}

type SelectedExportOutcome struct {
	Name           string   `json:"name"`
	Output         string   `json:"output,omitzero"`
	SHA256         string   `json:"sha256"`
	Source         ItemRef  `json:"source"`
	SourceIdentity string   `json:"source_identity"`
	History        *ItemRef `json:"history,omitzero"`
	Incomplete     bool     `json:"incomplete,omitzero"`
}

type selectedExportBinding struct {
	selection  *reportshare.SelectedOriginal
	source     ItemRef
	entry      string
	path       string
	outputName string
}

type SourceExportAssociation struct {
	Case        ItemRef                          `json:"case"`
	Identity    string                           `json:"identity"`
	Occurrences []reportshare.SelectedOccurrence `json:"occurrences"`
	Name        string                           `json:"name"`
	Bytes       int                              `json:"bytes"`
	SHA256      string                           `json:"sha256"`
	Class       string                           `json:"class"`
}

type SourceExportEntry struct {
	Ref          ItemRef                 `json:"ref"`
	Source       SourceExportAssociation `json:"source"`
	At           string                  `json:"at"`
	Actor        string                  `json:"actor"`
	SourceValues bool                    `json:"source_values"`
	Redacted     bool                    `json:"redacted"`
}

type SourceExportsResult struct {
	State   State               `json:"state"`
	Reason  string              `json:"reason,omitzero"`
	Context RequestContext      `json:"context"`
	Entries []SourceExportEntry `json:"entries"`
}

func (r *SourceExportsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ListSourceExports reads source-associated receipts, including history whose
// original source is now unavailable. Reading never re-exports any output.
func (a *App) ListSourceExports(request ItemRequest) SourceExportsResult {
	return runRead(a, false, func(ctx context.Context) SourceExportsResult {
		result := SourceExportsResult{Context: request.Context, Entries: []SourceExportEntry{}}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if request.Ref.Kind != CaseItem && request.Ref.Kind != VariantItem || !catalog.ValidID(request.Ref.ID) {
			result.refuse(Failed, "export history requires one source identity")
			return result
		}
		for _, item := range loaded.document.Items {
			if item.Kind != string(ReportShareItem) || loaded.removed(item) {
				continue
			}
			for _, revision := range item.Revisions {
				for _, member := range revision.Members {
					if err := ctx.Err(); err != nil {
						result.refuse(Cancelled, "history reading was stopped")
						return result
					}
					data, err := boundedFile(loaded.store.Path(member), catalog.MaxMemberBytes)
					if err != nil {
						continue
					}
					record, err := decodeReportShare(data)
					if err != nil || record.Source == nil || record.Source.Case.ID != request.Ref.ID || record.Source.Case.Kind != request.Ref.Kind {
						continue
					}
					detached := *record.Source
					detached.Occurrences = slices.Clone(record.Source.Occurrences)
					result.Entries = append(result.Entries, SourceExportEntry{Ref: ItemRef{Kind: ReportShareItem, ID: item.ID, Revision: item.RevisionLabel()}, Source: detached, At: record.At, Actor: record.Actor, SourceValues: record.SourceValues, Redacted: record.Redacted})
				}
			}
		}
		slices.Reverse(result.Entries)
		result.State = Completed
		return result
	})
}

func init() {
	actionPolicies[ExportSelectedMessagesAction] = actionPolicy{consent: ExportConsent, review: slot{}, perform: slot{writes: true}, bind: bindSelectedExport, execute: executeSelectedExport}
}

func bindSelectedExport(a *App, ctx context.Context, request PrepareActionRequest, _ bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.SelectedExport == nil {
		return nil, refusal{Failed, "selected-message export requires one retained source and an explicit selection"}
	}
	options := *request.SelectedExport
	if options.Transformation != "original" {
		return nil, refusal{Failed, "this export preserves original bytes only; unsupported transformation was refused"}
	}
	loaded, entry, declined := a.caseEntry(ctx, request.Context, request.Items[0], false)
	if loaded == nil {
		return nil, declined
	}
	_, source, declined := openedCase(loaded.root, entry, options.Identity)
	if source == nil {
		return nil, declined
	}
	selected, err := reportshare.SelectOriginal(source, options.Messages)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	item := loaded.read(loaded.document.Items[loaded.document.Find(request.Items[0].ID)])
	outputName := "Selected messages.hl7"
	share := &reportshare.Share{SourceValues: true, Files: []reportshare.File{{Name: outputName, Kind: reportshare.MessageType, Data: selected.Data}}}
	output := shareOutput(share, "original-message-bytes", ReportShareOptions{Format: "original-message-bytes"})
	if !options.Reveal {
		for i := range output.Files {
			output.Files[i].Text = ""
			output.Files[i].Data = ""
		}
	}
	display := &SelectedExportReview{Source: item.Ref, SourceName: item.Name, SourceIdentity: source.Identity, Occurrences: selected.Occurrences, SourceValues: true, Redacted: false, Class: "selected-original-message-bytes", Output: output, Destination: ShareDestinationView{Kind: "local"}, Consequence: "Exports the selected original payload bytes to a new local file. Contains original values; no redaction, minimization or replay equivalence is established."}
	bound := &selectedExportBinding{selection: selected, source: item.Ref, entry: entry, outputName: outputName}
	parts := []string{string(request.Action), loaded.root, loaded.document.Project.ID, item.Ref.ID, item.Ref.Revision, source.Identity, canonicalJSON(options.Messages), digestOf(selected.Data), options.Transformation, strconv.FormatBool(options.Reveal)}
	ready, reason := true, ""
	place, known := a.shareDestination(options.Destination)
	if options.Destination == "" {
		ready, reason = false, "choose where the output is written"
	} else if !known || place.folder || place.name != outputName {
		ready, reason = false, "choose a destination for this exact output"
	} else {
		path, err := artifactpath.Destination(place.path)
		if err != nil {
			ready, reason = false, "the output must be written outside retained evidence"
		} else if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			ready, reason = false, "something is already there; choose a new name"
		} else {
			parent, err := os.Stat(filepath.Dir(path))
			if err != nil || !parent.IsDir() {
				ready, reason = false, "the output folder is unavailable"
			} else {
				bound.path = path
				parts = append(parts, path, fmt.Sprintf("%v", parent.Sys()))
				display.Destination.Name, display.Destination.Location = filepath.Base(path), filepath.Base(filepath.Dir(path))
			}
		}
	}
	return &boundAction{action: request.Action, origin: request, selectedExport: bound, binding: binding(parts...), review: ActionReview{Items: []CatalogItem{item}, Ready: ready, Refusal: reason, SelectedExport: display, Destination: ReviewDestination{Output: display.Destination.Name}}}, noRefusal
}

func executeSelectedExport(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	result := ReviewedActionResult{Outcome: ActionRefused}
	export := bound.selectedExport
	if export == nil || export.path == "" {
		result.refuse(Failed, "the selected output is not prepared")
		return result
	}
	if err := ctx.Err(); err != nil {
		result.refuse(Cancelled, "the export was stopped before writing")
		return result
	}
	if err := operation.WriteNewFile(export.path, export.selection.Data, "cannot create the selected message file", "cannot write the selected message file"); err != nil {
		result.refuse(Failed, err.Error())
		return result
	}
	actual, err := operation.ReadInputFile(export.path, reportshare.MaxSelectedMessageBytes)
	if err != nil || !bytes.Equal(actual, export.selection.Data) {
		result.refuse(Failed, "the written output could not be verified; it is incomplete")
		result.SelectedExport = &SelectedExportOutcome{Name: markIncomplete(export.path), Incomplete: true}
		return result
	}
	result.State, result.Outcome = Completed, ActionCompleted
	result.SelectedExport = &SelectedExportOutcome{Name: filepath.Base(export.path), Output: a.rememberShareOutput(export.path), SHA256: digestOf(actual), Source: export.source, SourceIdentity: export.selection.SourceIdentity}
	history, notice := a.recordSelectedExport(ctx, bound)
	result.SelectedExport.History = history
	result.Reason = notice
	return result
}

func validateSourceExport(source SourceExportAssociation) error {
	if (source.Case.Kind != CaseItem && source.Case.Kind != VariantItem) || !catalog.ValidID(source.Case.ID) || !validExportDigest(source.Identity) || source.Name == "" || len(source.Name) > 255 || source.Bytes < 1 || source.Bytes > reportshare.MaxSelectedMessageBytes || !validExportDigest(source.SHA256) || source.Class != "selected-original-message-bytes" || len(source.Occurrences) == 0 || len(source.Occurrences) > reportshare.MaxSelectedMessages {
		return errors.New("invalid selected source export receipt")
	}
	offset := 0
	seen := map[string]bool{}
	for _, occurrence := range source.Occurrences {
		if occurrence.Occurrence == "" || len(occurrence.Occurrence) > 255 || !slices.Contains([]string{"message", "ack", "unparsed"}, occurrence.Kind) || seen[occurrence.Occurrence] || occurrence.Offset != offset || occurrence.Bytes < 1 || !validExportDigest(occurrence.SHA256) {
			return errors.New("invalid selected source export scope")
		}
		seen[occurrence.Occurrence] = true
		offset += occurrence.Bytes
	}
	if offset != source.Bytes {
		return errors.New("selected source export bytes disagree with its scope")
	}
	return nil
}

func (a *App) recordSelectedExport(ctx context.Context, bound *boundAction) (*ItemRef, string) {
	export := bound.selectedExport
	source := &SourceExportAssociation{Case: export.source, Identity: export.selection.SourceIdentity, Occurrences: slices.Clone(export.selection.Occurrences), Name: filepath.Base(export.path), Bytes: len(export.selection.Data), SHA256: digestOf(export.selection.Data), Class: "selected-original-message-bytes"}
	record := reportShareRecord{Schema: SourceShareSchema, Source: source, Revision: export.source.Revision, Actor: a.reviewerName(), At: catalog.Stamp(a.now()), Destination: "local-file", Output: ShareOutputFile, Format: "original-message-bytes", SourceValues: true}
	data, err := encodeMember(record)
	notice := "the export was written; its source-associated history could not be recorded"
	if err != nil {
		return nil, notice
	}
	loaded, _ := a.loadCatalog(ctx, bound.origin.Context, true)
	if loaded == nil {
		return nil, notice
	}
	intent := ""
	if bound.executionReview != nil {
		intent = bound.executionReview.intent
	}
	sum := sha256.Sum256([]byte("selected-message-export\x00" + intent))
	draft := catalog.Draft{Kind: string(ReportShareItem), Intent: "selected-export-" + hex.EncodeToString(sum[:16]), Digest: digestOf(data), Author: a.reviewerName(), Members: []catalog.Staged{{Role: string(ReportShareItem), File: "share.json", Data: data}}}
	saved, err := loaded.store.Save(draft, verifierFor(ReportShareItem), catalog.Options{Now: a.now, Fault: a.saveFault})
	if err != nil {
		return nil, notice
	}
	ref := ItemRef{Kind: ReportShareItem, ID: saved.Item.ID, Revision: saved.Item.RevisionLabel()}
	return &ref, ""
}

func sourceMemberPresent(data []byte) bool {
	var fields map[string]any
	if json.Unmarshal(data, &fields) != nil {
		return true
	}
	_, exists := fields["source"]
	return exists
}

type SourceExportPreviewResult struct {
	State   State                    `json:"state"`
	Reason  string                   `json:"reason,omitzero"`
	Context RequestContext           `json:"context"`
	Source  *SourceExportAssociation `json:"source,omitzero"`
	Output  *ShareOutput             `json:"output,omitzero"`
}

func (r *SourceExportPreviewResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// OpenSourceExport asks for the real output file again, including after restart,
// verifies its receipt digest and returns its exact byte preview. It never trusts
// a filename or reconstructs the output from the current source capture.
func (a *App) OpenSourceExport(request ItemRequest) SourceExportPreviewResult {
	return run(a, true, false, func(ctx context.Context) SourceExportPreviewResult {
		result := SourceExportPreviewResult{Context: request.Context}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if request.Ref.Kind != ReportShareItem {
			result.refuse(Failed, "choose one source-associated export receipt")
			return result
		}
		index := loaded.document.Find(request.Ref.ID)
		if index < 0 || loaded.document.Items[index].Kind != string(ReportShareItem) || loaded.removed(loaded.document.Items[index]) {
			result.refuse(Failed, "the project holds no such export receipt")
			return result
		}
		item := loaded.document.Items[index]
		if request.Ref.Revision != "" && item.RevisionLabel() != request.Ref.Revision {
			result.refuse(Failed, "the export receipt changed; read its history again")
			return result
		}
		var receipt *SourceExportAssociation
		for _, revision := range item.Revisions {
			for _, member := range revision.Members {
				data, err := boundedFile(loaded.store.Path(member), catalog.MaxMemberBytes)
				if err != nil {
					continue
				}
				record, err := decodeReportShare(data)
				if err == nil && record.Source != nil {
					copy := *record.Source
					copy.Occurrences = slices.Clone(record.Source.Occurrences)
					receipt = &copy
				}
			}
		}
		if receipt == nil {
			result.refuse(Failed, "this receipt is not a selected source-message export")
			return result
		}
		path, declined := a.chooseOneFile(ctx, "Open exported selected messages")
		if path == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		raw, err := operation.ReadInputFile(path, reportshare.MaxSelectedMessageBytes)
		if err != nil || len(raw) != receipt.Bytes || digestOf(raw) != receipt.SHA256 {
			result.refuse(Failed, "the selected file does not match this export's exact retained output identity")
			return result
		}
		share := &reportshare.Share{SourceValues: true, Files: []reportshare.File{{Name: filepath.Base(path), Kind: reportshare.MessageType, Data: raw}}}
		output := shareOutput(share, "original-message-bytes", ReportShareOptions{Format: "original-message-bytes"})
		result.State, result.Source, result.Output = Completed, receipt, &output
		return result
	})
}

func validExportDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
