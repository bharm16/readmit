package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/engineexport"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
)

// ImportSourcesResult carries the outcome of a native source dialog.
type ImportSourcesResult struct {
	State  State    `json:"state"`
	Reason string   `json:"reason,omitzero"`
	Kind   string   `json:"kind,omitzero"`
	Paths  []string `json:"paths,omitzero"`
}

func (r *ImportSourcesResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// PastedSourceRequest names the destination project or workspace and the pasted content.
type PastedSourceRequest struct {
	Workspace string `json:"workspace"`
	Project   string `json:"project,omitzero"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	Encoding  string `json:"encoding,omitzero"`
}

// PastedSourceResult carries the retained declared source details.
type PastedSourceResult struct {
	State    State  `json:"state"`
	Reason   string `json:"reason,omitzero"`
	Path     string `json:"path,omitzero"`
	Name     string `json:"name,omitzero"`
	Size     int    `json:"size,omitzero"`
	SHA256   string `json:"sha256,omitzero"`
	Encoding string `json:"encoding,omitzero"`
}

func (r *PastedSourceResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ImportRequest describes the declared sources and extraction configuration to preview.
type ImportRequest struct {
	Workspace  string             `json:"workspace"`
	Project    string             `json:"project,omitzero"`
	Mode       string             `json:"mode"` // "plan", "recipe", "engine"
	Files      []string           `json:"files,omitzero"`
	Folders    []string           `json:"folders,omitzero"`
	Archives   []string           `json:"archives,omitzero"`
	Plan       *importer.Plan     `json:"plan,omitzero"`
	Recipe     *importer.Recipe   `json:"recipe,omitzero"`
	EnginePlan *engineexport.Plan `json:"engine_plan,omitzero"`
}

// ImportPreviewResult carries the preview outcome across plan, recipe or engine extraction.
type ImportPreviewResult struct {
	State         State                          `json:"state"`
	Reason        string                         `json:"reason,omitzero"`
	Mode          string                         `json:"mode,omitzero"`
	PlanPreview   *importer.Preview              `json:"plan_preview,omitzero"`
	RecipePreview *importer.MappingPreview       `json:"recipe_preview,omitzero"`
	EnginePreview *operation.EngineExportPreview `json:"engine_preview,omitzero"`
}

func (r *ImportPreviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ImportCommitRequest describes the parameters for committing an import into new evidence.
type ImportCommitRequest struct {
	Workspace         string             `json:"workspace"`
	Project           string             `json:"project,omitzero"`
	Mode              string             `json:"mode"` // "plan", "recipe", "engine"
	OutputName        string             `json:"output_name"`
	ReceiptName       string             `json:"receipt_name,omitzero"`
	Files             []string           `json:"files,omitzero"`
	Folders           []string           `json:"folders,omitzero"`
	Archives          []string           `json:"archives,omitzero"`
	Plan              *importer.Plan     `json:"plan,omitzero"`
	Recipe            *importer.Recipe   `json:"recipe,omitzero"`
	EnginePlan        *engineexport.Plan `json:"engine_plan,omitzero"`
	RegisterInProject bool               `json:"register_in_project,omitzero"`
	CaseTitle         string             `json:"case_title,omitzero"`
	CaseOwner         string             `json:"case_owner,omitzero"`
	CaseVersion       string             `json:"case_version,omitzero"`
}

// ImportCommitResult carries the verified case bundle facts, written paths, and registration status.
type ImportCommitResult struct {
	State       State             `json:"state"`
	Reason      string            `json:"reason,omitzero"`
	Case        *Case             `json:"case,omitzero"`
	CasePath    string            `json:"case_path,omitzero"`
	ReceiptPath string            `json:"receipt_path,omitzero"`
	Registered  bool              `json:"registered,omitzero"`
	Project     *project.Document `json:"project,omitzero"`
}

func (r *ImportCommitResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ChooseImportSources presents the host's native dialog to select files, a folder, or an archive.
func (a *App) ChooseImportSources(kind string) ImportSourcesResult {
	return run(a, true, false, func(ctx context.Context) ImportSourcesResult {
		switch kind {
		case "folder":
			folder, declined := a.chooseFolder(ctx, "Choose a folder to import")
			if folder == "" {
				return ImportSourcesResult{State: declined.state, Reason: declined.reason}
			}
			return ImportSourcesResult{State: Completed, Kind: "folder", Paths: []string{folder}}
		case "archive":
			archives, declined := a.chooseFiles(ctx, "Choose ZIP archives to import", "ZIP archives (*.zip)", "*.zip")
			if len(archives) == 0 {
				return ImportSourcesResult{State: declined.state, Reason: declined.reason}
			}
			return ImportSourcesResult{State: Completed, Kind: "archive", Paths: archives}
		default:
			files, declined := a.chooseFiles(ctx, "Choose evidence files to import", "All files (*.*)", "*.*")
			if len(files) == 0 {
				return ImportSourcesResult{State: declined.state, Reason: declined.reason}
			}
			return ImportSourcesResult{State: Completed, Kind: "files", Paths: files}
		}
	})
}

// StagePastedContent retains explicitly pasted content as a newly declared source with its actual bytes.
func (a *App) StagePastedContent(request PastedSourceRequest) PastedSourceResult {
	return run(a, true, true, func(ctx context.Context) PastedSourceResult {
		targetDir := request.Project
		if targetDir == "" {
			targetDir = request.Workspace
		}
		if targetDir == "" {
			return PastedSourceResult{State: Failed, Reason: "no target workspace or project provided"}
		}
		stagedDir := filepath.Join(targetDir, "staged-sources")
		name := request.Name
		if name == "" {
			name = "pasted-source.hl7"
		}
		bytesData := []byte(request.Content)
		path, err := operation.StagePastedContent(stagedDir, name, bytesData)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				return PastedSourceResult{State: PermissionDenied, Reason: "this account cannot write staged sources into the workspace"}
			}
			return PastedSourceResult{State: Failed, Reason: err.Error()}
		}
		digest := sha256.Sum256(bytesData)
		encoding := request.Encoding
		if encoding == "" {
			encoding = "utf-8"
		}
		return PastedSourceResult{
			State:    Completed,
			Path:     path,
			Name:     name,
			Size:     len(bytesData),
			SHA256:   hex.EncodeToString(digest[:]),
			Encoding: encoding,
		}
	})
}

// PreviewImport extracts and previews records without writing any evidence.
func (a *App) PreviewImport(request ImportRequest) ImportPreviewResult {
	return run(a, true, false, func(ctx context.Context) ImportPreviewResult {
		switch request.Mode {
		case "plan":
			if request.Plan == nil {
				return ImportPreviewResult{State: Failed, Reason: "an import plan is required"}
			}
			preview, err := operation.ImportPlanPreview(ctx, *request.Plan, request.Files, request.Folders, request.Archives)
			if err != nil {
				if ctx.Err() != nil {
					return ImportPreviewResult{State: Cancelled, Reason: cancelledRefusal.reason}
				}
				return ImportPreviewResult{State: Failed, Reason: err.Error()}
			}
			return ImportPreviewResult{
				State:       Completed,
				Mode:        "plan",
				PlanPreview: &preview,
			}
		case "recipe":
			if request.Recipe == nil {
				return ImportPreviewResult{State: Failed, Reason: "a mapping recipe is required"}
			}
			preview, err := operation.ImportRecipePreview(ctx, *request.Recipe, request.Files, request.Folders, request.Archives)
			if err != nil {
				if ctx.Err() != nil {
					return ImportPreviewResult{State: Cancelled, Reason: cancelledRefusal.reason}
				}
				return ImportPreviewResult{State: Failed, Reason: err.Error()}
			}
			return ImportPreviewResult{
				State:         Completed,
				Mode:          "recipe",
				RecipePreview: &preview,
			}
		case "engine":
			if request.EnginePlan == nil {
				return ImportPreviewResult{State: Failed, Reason: "an engine export adapter plan is required"}
			}
			if len(request.Files) != 1 {
				return ImportPreviewResult{State: Failed, Reason: "engine import requires exactly one input export file"}
			}
			preview, err := operation.ImportEnginePreview(ctx, *request.EnginePlan, request.Files[0])
			if err != nil {
				if ctx.Err() != nil {
					return ImportPreviewResult{State: Cancelled, Reason: cancelledRefusal.reason}
				}
				return ImportPreviewResult{State: Failed, Reason: err.Error()}
			}
			return ImportPreviewResult{
				State:         Completed,
				Mode:          "engine",
				EnginePreview: &preview,
			}
		default:
			return ImportPreviewResult{State: Failed, Reason: "unsupported import mode; choose plan, recipe, or engine"}
		}
	})
}

// CommitImport commits the extraction to new case and receipt destinations, and registers it if requested.
func (a *App) CommitImport(request ImportCommitRequest) ImportCommitResult {
	return run(a, true, true, func(ctx context.Context) ImportCommitResult {
		if err := artifactpath.EntryName(request.OutputName); err != nil {
			return ImportCommitResult{State: Failed, Reason: "the case destination must be one valid directory entry name"}
		}
		targetDir := request.Project
		if targetDir == "" {
			targetDir = request.Workspace
		}
		if targetDir == "" {
			return ImportCommitResult{State: Failed, Reason: "no target workspace or project provided"}
		}
		casePath := filepath.Join(targetDir, request.OutputName)

		receiptName := request.ReceiptName
		if receiptName == "" {
			receiptName = request.OutputName + "-receipt.json"
		}
		if err := artifactpath.EntryName(receiptName); err != nil {
			return ImportCommitResult{State: Failed, Reason: "the receipt destination must be one valid file entry name"}
		}
		receiptPath := filepath.Join(targetDir, receiptName)

		var b *bundle.Bundle
		var err error

		switch request.Mode {
		case "plan":
			if request.Plan == nil {
				return ImportCommitResult{State: Failed, Reason: "an import plan is required"}
			}
			b, _, err = operation.ImportPlanCommit(ctx, *request.Plan, request.Files, request.Folders, request.Archives, casePath, receiptPath)
		case "recipe":
			if request.Recipe == nil {
				return ImportCommitResult{State: Failed, Reason: "a mapping recipe is required"}
			}
			b, _, err = operation.ImportRecipeCommit(ctx, *request.Recipe, request.Files, request.Folders, request.Archives, casePath, receiptPath)
		case "engine":
			if request.EnginePlan == nil {
				return ImportCommitResult{State: Failed, Reason: "an engine export adapter plan is required"}
			}
			if len(request.Files) != 1 {
				return ImportCommitResult{State: Failed, Reason: "engine import requires exactly one input export file"}
			}
			b, err = operation.ImportEngineCommit(ctx, *request.EnginePlan, request.Files[0], casePath)
		default:
			return ImportCommitResult{State: Failed, Reason: "unsupported import mode"}
		}

		if err != nil {
			if ctx.Err() != nil {
				return ImportCommitResult{State: Cancelled, Reason: cancelledRefusal.reason}
			}
			if errors.Is(err, os.ErrPermission) {
				return ImportCommitResult{State: PermissionDenied, Reason: "this account cannot write case evidence into the selected directory"}
			}
			return ImportCommitResult{State: Failed, Reason: err.Error()}
		}

		counts := b.Counts()
		c := &Case{
			Name:             request.OutputName,
			Identity:         b.Identity,
			Schema:           b.Manifest.Schema,
			Provenance:       string(b.Manifest.Provenance.Mode),
			Sources:          len(b.Manifest.Sources),
			Occurrences:      len(b.Events),
			Messages:         counts[bundle.Message],
			Acknowledgements: counts[bundle.Acknowledgement],
			Unparsed:         counts[bundle.Unparsed],
		}

		registered := false
		var projDoc *project.Document
		if request.RegisterInProject && request.Project != "" {
			title := request.CaseTitle
			if title == "" {
				title = request.OutputName
			}
			reg := operation.CaseRegistration{
				Title:            title,
				Owner:            request.CaseOwner,
				InterfaceVersion: request.CaseVersion,
			}
			_, regErr := operation.RegisterCase(request.Project, request.OutputName, reg)
			if regErr == nil {
				registered = true
				if openedProj, openErr := project.Open(request.Project); openErr == nil {
					projDoc = &openedProj.Document
				}
			}
		}

		return ImportCommitResult{
			State:       Completed,
			Case:        c,
			CasePath:    casePath,
			ReceiptPath: receiptPath,
			Registered:  registered,
			Project:     projDoc,
		}
	})
}
