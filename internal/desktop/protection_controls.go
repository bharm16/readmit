package desktop

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strconv"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/protect"
)

// Encryption settings. A person manages named controls, not protection
// documents: the list gathers every control of every protection document of
// the open project, and the first control is registered into a document the
// application names, so no file name is ever asked for. Each operation here is
// the protect package's own; a check reads the key and records nothing, an
// edit that changes where the key is read from is a rotation, and an export
// carries the reference alone.

// protectionEntryName is the document a project's first control is
// registered into.
const protectionEntryName = "protection.json"

// maxProtectionNames bounds how many names beside protection.json are tried
// before the application refuses to choose one.
const maxProtectionNames = 100

// ListedProtectionControl is one registered control and the protection
// document entry it is registered in.
type ListedProtectionControl struct {
	Entry   string            `json:"entry"`
	Control ProtectionControl `json:"control"`
}

// ProtectionEntryProblem is a protection document of the project that could
// not be read, and why. Its controls are not listed; it is left as written.
type ProtectionEntryProblem struct {
	Entry  string `json:"entry"`
	Reason string `json:"reason"`
}

// ProtectionListResult lists every control of the open project. AddEntry is
// the document a new control is registered into: the first readable
// protection document, or a new one the application names.
type ProtectionListResult struct {
	State       State                     `json:"state"`
	Reason      string                    `json:"reason,omitzero"`
	Controls    []ListedProtectionControl `json:"controls"`
	Unreadable  []ProtectionEntryProblem  `json:"unreadable"`
	AddEntry    string                    `json:"add_entry,omitzero"`
	Limitations []string                  `json:"limitations"`
}

func (r *ProtectionListResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ListProtectionControls lists every registered control of every protection
// document in the open project, with the key masked and the locator counted.
// It runs no program, resolves no key and contacts nothing.
func (a *App) ListProtectionControls(workspace string) ProtectionListResult {
	return run(a, false, false, func(context.Context) ProtectionListResult {
		result := ProtectionListResult{Controls: []ListedProtectionControl{}, Unreadable: []ProtectionEntryProblem{}, Limitations: protectionLimitations}
		root, declined := resolveFolder(workspace)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		entries, err := os.ReadDir(root)
		switch {
		case errors.Is(err, fs.ErrPermission):
			result.refuse(PermissionDenied, "this account cannot read the project folder")
			return result
		case err != nil:
			result.refuse(Failed, "the project folder cannot be read")
			return result
		}
		for _, entry := range entries {
			name := entry.Name()
			if !entry.Type().IsRegular() || artifactpath.EntryName(name) != nil {
				continue
			}
			if kind, ok := classify(root, name, false); !ok || kind != ProtectionArtifact {
				continue
			}
			_, file, declined := protectionEntryFile(root, name)
			if declined.state != "" {
				result.Unreadable = append(result.Unreadable, ProtectionEntryProblem{Entry: name, Reason: declined.reason})
				continue
			}
			document, err := file.Read()
			if err != nil {
				result.Unreadable = append(result.Unreadable, ProtectionEntryProblem{Entry: name, Reason: err.Error()})
				continue
			}
			if result.AddEntry == "" {
				result.AddEntry = name
			}
			for _, control := range protectionView(name, document).Controls {
				result.Controls = append(result.Controls, ListedProtectionControl{Entry: name, Control: control})
			}
		}
		if result.AddEntry == "" {
			result.AddEntry = freeProtectionEntry(root)
		}
		result.State = Completed
		if len(result.Controls) == 0 && len(result.Unreadable) == 0 {
			result.State, result.Reason = Empty, "no encryption control is registered in this project"
		}
		return result
	})
}

// freeProtectionEntry is protection.json, or the first protection-N.json no
// entry of the project holds, or empty when none of the bounded names is free.
func freeProtectionEntry(root string) string {
	for i := 1; i <= maxProtectionNames; i++ {
		name := protectionEntryName
		if i > 1 {
			name = "protection-" + strconv.Itoa(i) + ".json"
		}
		if _, err := os.Lstat(artifactpath.JoinReference(root, name)); errors.Is(err, fs.ErrNotExist) {
			return name
		}
	}
	return ""
}

// ProtectionCheckResult is one explicit check of a control: the declared store
// answered for it at CheckedAt, or did not. Nothing is recorded either way.
type ProtectionCheckResult struct {
	State      State  `json:"state"`
	Reason     string `json:"reason,omitzero"`
	Name       string `json:"name"`
	Generation int    `json:"generation,omitzero"`
	CheckedAt  string `json:"checked_at,omitzero"`
}

func (r *ProtectionCheckResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// CheckProtectionControl asks the control's declared store to answer, through
// the same key read a rotation and a package use. The key it printed is
// discarded, never written, logged or shown, and nothing is recorded: the
// document is not written and no rotation is claimed.
func (a *App) CheckProtectionControl(workspace, entry, name string) ProtectionCheckResult {
	return runNamed[ProtectionCheckResult, *ProtectionCheckResult](a, profiles["CheckProtectionControl"], func(ctx context.Context) ProtectionCheckResult {
		result := ProtectionCheckResult{Name: name}
		_, file, declined := protectionEntryFile(workspace, entry)
		if declined.state != "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		document, err := file.Read()
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		control, err := protect.Find(document, name)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		result.Generation = control.Generation
		if _, err := protect.ReadKey(ctx, control); err != nil {
			if ctx.Err() != nil {
				result.refuse(Cancelled, "the check was stopped; nothing was recorded")
				return result
			}
			result.refuse(Failed, "the key did not resolve from its declared store; nothing was recorded")
			return result
		}
		result.State, result.CheckedAt = Completed, catalog.Stamp(a.now())
		return result
	})
}

// ProtectionControlUpdate edits one registered control. Storage, MaxAge and
// Retain are the values to keep, empty clearing an interval. Command is the
// key program to keep. Arguments replace the stored locator arguments only
// when given; absent, the stored ones are kept unseen.
type ProtectionControlUpdate struct {
	Workspace string    `json:"workspace"`
	Entry     string    `json:"entry"`
	Name      string    `json:"name"`
	Storage   string    `json:"storage"`
	Command   string    `json:"command"`
	Arguments *[]string `json:"arguments,omitzero"`
	MaxAge    string    `json:"max_age,omitzero"`
	Retain    string    `json:"retain,omitzero"`
}

// ProtectionUpdateResult is the document after an edit, and whether the edit
// was recorded as a rotation because it changed where the key is read from.
type ProtectionUpdateResult struct {
	State    State               `json:"state"`
	Reason   string              `json:"reason,omitzero"`
	Entry    string              `json:"entry,omitzero"`
	Rotated  bool                `json:"rotated"`
	Document *ProtectionDocument `json:"document,omitzero"`
}

func (r *ProtectionUpdateResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// UpdateProtectionControl edits one control through protect.File.Amend. An
// edit that changes the key program or its arguments changes which key new
// packages are written under, so it is recorded as a rotation: the key is read
// through the new locator first, the generation is bumped only when it
// answers, and otherwise nothing changes.
func (a *App) UpdateProtectionControl(request ProtectionControlUpdate) ProtectionUpdateResult {
	return runNamed[ProtectionUpdateResult, *ProtectionUpdateResult](a, profiles["UpdateProtectionControl"], func(ctx context.Context) ProtectionUpdateResult {
		_, file, declined := protectionEntryFile(request.Workspace, request.Entry)
		if declined.state != "" {
			return ProtectionUpdateResult{State: declined.state, Reason: declined.reason}
		}
		storage := protect.Storage(request.Storage)
		change := protect.Amendment{Storage: &storage, Command: &request.Command, MaxAge: &request.MaxAge, Retain: &request.Retain}
		if request.Arguments != nil {
			arguments := slices.Clone(*request.Arguments)
			change.Arguments = &arguments
		}
		updated, _, rotated, err := file.Amend(ctx, request.Name, change, a.now())
		if err != nil {
			if protect.StepOf(err) == protect.KeyStep && ctx.Err() != nil {
				return ProtectionUpdateResult{State: Cancelled, Reason: "the edit was cancelled; the control is unchanged"}
			}
			return ProtectionUpdateResult{State: Failed, Reason: err.Error()}
		}
		return ProtectionUpdateResult{State: Completed, Entry: request.Entry, Rotated: rotated, Document: protectionView(request.Entry, updated)}
	})
}

// ProtectionExportResult is where one control's reference was exported.
type ProtectionExportResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Path   string `json:"path,omitzero"`
}

func (r *ProtectionExportResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// exportControlTitle is the save dialog an export names its new file in.
const exportControlTitle = "Export encryption control"

// ExportProtectionControl writes one control, as it is registered, into a new
// readmit-protection/v1 document the person names in the host's save dialog.
// It is the reference alone: the key program and locator arguments, never key
// material — no program is run and no key is read to write it.
func (a *App) ExportProtectionControl(workspace, entry, name string) ProtectionExportResult {
	return run(a, true, false, func(ctx context.Context) ProtectionExportResult {
		_, file, declined := protectionEntryFile(workspace, entry)
		if declined.state != "" {
			return ProtectionExportResult{State: declined.state, Reason: declined.reason}
		}
		document, err := file.Read()
		if err != nil {
			return ProtectionExportResult{State: Failed, Reason: err.Error()}
		}
		control, err := protect.Find(document, name)
		if err != nil {
			return ProtectionExportResult{State: Failed, Reason: err.Error()}
		}
		data, err := protect.Encode(protect.Document{Schema: protect.Schema, Controls: []protect.Control{control}})
		if err != nil {
			return ProtectionExportResult{State: Failed, Reason: err.Error()}
		}
		named, declined := a.chooseNamedDestination(ctx, exportControlTitle, name+".protection.json")
		if named == "" {
			return ProtectionExportResult{State: declined.state, Reason: declined.reason}
		}
		destination, err := artifactpath.Destination(named)
		if err != nil {
			return ProtectionExportResult{State: Failed, Reason: err.Error()}
		}
		if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
			return ProtectionExportResult{State: Failed, Reason: "a file is already there; name a new file for the control"}
		}
		if err := operation.WriteNewFile(destination, data, "cannot create the file", "cannot write the file"); err != nil {
			return ProtectionExportResult{State: Failed, Reason: "the control could not be exported to the named file"}
		}
		return ProtectionExportResult{State: Completed, Path: destination}
	})
}
