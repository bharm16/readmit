package desktop

import (
	"context"

	"github.com/bharm16/readmit/internal/engineexport"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/operation"
)

// ImportSourcesResult carries the outcome of a native source dialog.
type ImportSourcesResult struct {
	State  State    `json:"state"`
	Reason string   `json:"reason,omitzero"`
	Kind   string   `json:"kind,omitzero"`
	Paths  []string `json:"paths,omitzero"`
}

func (r *ImportSourcesResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// PastedSourceRequest names the project and the pasted content, staged in
// the project's own area under a generated identity and named Pasted
// messages unless Name says otherwise.
type PastedSourceRequest struct {
	Context  RequestContext `json:"context"`
	Name     string         `json:"name,omitzero"`
	Content  string         `json:"content"`
	Encoding string         `json:"encoding,omitzero"`
}

// PastedSourceResult is the staged source: its identity, name, size, digest
// and declared encoding, never where it is kept.
type PastedSourceResult struct {
	State    State  `json:"state"`
	StagedID string `json:"staged_id,omitzero"`
	Reason   string `json:"reason,omitzero"`
	Name     string `json:"name,omitzero"`
	Size     int    `json:"size,omitzero"`
	SHA256   string `json:"sha256,omitzero"`
	Encoding string `json:"encoding,omitzero"`
}

func (r *PastedSourceResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ImportRequest describes the declared sources and extraction configuration to preview.
//
// Context names the project an import flow belongs to, and Staged the pasted
// sources StagePastedContent staged in it, which are read as files.
type ImportRequest struct {
	Context    RequestContext     `json:"context,omitzero"`
	Workspace  string             `json:"workspace"`
	Project    string             `json:"project,omitzero"`
	Mode       string             `json:"mode"` // "plan", "recipe", "engine"
	Files      []string           `json:"files,omitzero"`
	Folders    []string           `json:"folders,omitzero"`
	Archives   []string           `json:"archives,omitzero"`
	Plan       *importer.Plan     `json:"plan,omitzero"`
	Recipe     *importer.Recipe   `json:"recipe,omitzero"`
	EnginePlan *engineexport.Plan `json:"engine_plan,omitzero"`
	Staged     []string           `json:"staged,omitzero"`
}

// ImportPreviewResult carries the preview outcome across plan, recipe or engine extraction.
type ImportPreviewResult struct {
	State         State                          `json:"state"`
	Reason        string                         `json:"reason,omitzero"`
	Mode          string                         `json:"mode,omitzero"`
	PlanPreview   *importer.Preview              `json:"plan_preview,omitzero"`
	RecipePreview *importer.MappingPreview       `json:"recipe_preview,omitzero"`
	EnginePreview *operation.EngineExportPreview `json:"engine_preview,omitzero"`
	// PreviewToken names exactly this preview: the declarations and the bytes
	// they read. ImportCase refuses a token that no longer matches.
	PreviewToken string             `json:"preview_token,omitzero"`
	Rows         []ImportPreviewRow `json:"rows,omitzero"`
	RowTotal     int                `json:"row_total,omitzero"`
	Problems     ImportProblems     `json:"problems,omitzero"`
}

func (r *ImportPreviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

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

// StagePastedContent retains explicitly pasted content as a newly declared
// source with its actual bytes, staged inside the project's own area under a
// generated identity (stagePasted).
func (a *App) StagePastedContent(request PastedSourceRequest) PastedSourceResult {
	return run(a, true, true, func(ctx context.Context) PastedSourceResult {
		return a.stagePasted(ctx, request)
	})
}

// importOperation names an import preview or commit while it holds the slot,
// so the import and capture panels' cancel controls stop exactly the import
// they started.
const importOperation = "import"

// PreviewImport extracts and previews records without writing any evidence.
// Besides the declared mode's own preview it answers every parsed row, the
// nonzero problems, and the token an import of exactly this preview names.
func (a *App) PreviewImport(request ImportRequest) ImportPreviewResult {
	return runNamedRead[ImportPreviewResult, *ImportPreviewResult](a, profiles["PreviewImport"], func(ctx context.Context) ImportPreviewResult {
		request = withSchemas(request)
		read, declined := a.extractImport(ctx, request)
		if read == nil {
			return ImportPreviewResult{State: declined.state, Reason: declined.reason}
		}
		result := ImportPreviewResult{State: Completed, Mode: request.Mode, PreviewToken: read.token, Problems: read.problems()}
		result.Rows, result.RowTotal = read.previewRows()
		switch request.Mode {
		case "plan":
			preview := importer.NewPreview(*request.Plan, read.extraction)
			result.PlanPreview = &preview
		case "recipe":
			preview, err := importer.NewMappingPreview(*request.Recipe, read.extraction)
			if err != nil {
				return ImportPreviewResult{State: Failed, Reason: err.Error()}
			}
			result.RecipePreview = &preview
		case "engine":
			result.EnginePreview = read.engine
		}
		return result
	})
}
