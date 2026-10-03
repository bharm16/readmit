package desktop

import (
	"bytes"
	"errors"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"path/filepath"
	"slices"
)

// ImportInvestigation pins a loose-file investigation to its original bytes
// and zero-based selected indexes. It is navigation, never evidence values.
type ImportInvestigation struct {
	File       string `json:"file"`
	Identity   string `json:"identity"`
	Format     string `json:"format"`
	Terminator string `json:"terminator"`
	Messages   []int  `json:"messages"`
	Selected   *int   `json:"selected,omitzero"`
	Path       string `json:"path,omitzero"`
	NodeOffset int    `json:"node_offset"`
}

// RetainedImportSelection maps investigated positions to verified retained
// identities, preserving checked order independently of list filters/pages.
type RetainedImportSelection struct {
	Identity   string   `json:"identity"`
	Messages   []string `json:"messages"`
	Selected   string   `json:"selected,omitzero"`
	Path       string   `json:"path,omitzero"`
	NodeOffset int      `json:"node_offset"`
}

func validateImportInvestigation(source ImportRequest, read *importRead, held *ImportInvestigation) refusal {
	if held == nil {
		return noRefusal
	}
	if source.Mode != "plan" || len(source.Files) != 1 || source.Files[0] != held.File || len(source.Folders)+len(source.Archives)+len(source.Staged) != 0 || held.NodeOffset < 0 || len(held.Path) > 4096 || len(held.Messages) > 1024 {
		return refusal{Failed, "retaining an investigation requires its single original message file and bounded selection"}
	}
	data, declined := expectedFile(held.File, held.Identity)
	if declined.state != "" {
		return declined
	}
	options, declined := inspectionOptions(held.File, held.Format, held.Terminator)
	if declined.state != "" {
		return declined
	}
	document, err := hl7.Parse(data, options)
	if err != nil {
		return refusal{Failed, "the investigated source no longer parses under its declaration"}
	}
	if read.extraction == nil || len(read.extraction.Inputs) != 1 || !bytes.Equal(read.extraction.Inputs[0].Data, data) {
		return refusal{Failed, "this import mapping changes the investigated source; choose its original-byte declaration to retain selection"}
	}
	seen := map[int]bool{}
	for _, index := range held.Messages {
		if index < 0 || index >= len(document.Messages) || seen[index] {
			return refusal{Failed, "selected messages must be distinct positions of the investigated file"}
		}
		seen[index] = true
	}
	if held.Selected != nil && (*held.Selected < 0 || *held.Selected >= len(document.Messages)) {
		return refusal{Failed, "the selected message is outside the investigated file"}
	}
	return noRefusal
}

func retainedImportSelection(root, entry string, held *ImportInvestigation) (*RetainedImportSelection, error) {
	if held == nil {
		return nil, nil
	}
	source, err := bundle.Open(filepath.Join(root, entry))
	if err != nil {
		return nil, err
	}
	var sourceID string
	for _, input := range source.Manifest.Sources {
		if input.SHA256 == held.Identity {
			if sourceID != "" {
				return nil, errors.New("the investigated source is ambiguous in the capture")
			}
			sourceID = input.ID
		}
	}
	if sourceID == "" {
		return nil, errors.New("the retained capture does not contain the investigated original source")
	}
	find := func(index int) string {
		for _, event := range source.Events {
			if event.SourceID == sourceID && event.Sequence == index+1 {
				return event.ID
			}
		}
		return ""
	}
	result := &RetainedImportSelection{Identity: source.Identity, Messages: []string{}, Path: held.Path, NodeOffset: held.NodeOffset}
	for _, index := range slices.Clone(held.Messages) {
		id := find(index)
		if id == "" {
			return nil, errors.New("a selected message was not retained")
		}
		result.Messages = append(result.Messages, id)
	}
	if held.Selected != nil {
		result.Selected = find(*held.Selected)
		if result.Selected == "" {
			return nil, errors.New("the inspected message was not retained")
		}
	}
	return result, nil
}
