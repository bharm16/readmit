package desktop

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/scenario"
	"github.com/bharm16/readmit/internal/scenariogen"
	"github.com/bharm16/readmit/internal/scenariolibrary"
	"github.com/bharm16/readmit/internal/synth"
)

// ScenarioCatalogResult carries the closed set of lifecycle profiles and events
// the shared engine supports, including unavailable events with reasons.
type ScenarioCatalogResult struct {
	State   State             `json:"state"`
	Reason  string            `json:"reason,omitzero"`
	Catalog *scenario.Catalog `json:"catalog,omitzero"`
}

func (r *ScenarioCatalogResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ScenarioPreviewRequest carries a scenario or order-scenario document and
// whether identifiers may be revealed locally.
type ScenarioPreviewRequest struct {
	Workspace       string `json:"workspace"`
	Document        string `json:"document"`
	RevealSensitive bool   `json:"reveal_sensitive,omitzero"`
}

// ScenarioStepView is one previewed step without message bytes.
type ScenarioStepView struct {
	Ordinal     int    `json:"ordinal"`
	ID          string `json:"id"`
	At          string `json:"at"`
	Event       string `json:"event"`
	Description string `json:"description"`
	Subject     string `json:"subject"`
	Into        string `json:"into,omitzero"`
	Expect      string `json:"expect"`
	From        string `json:"from"`
	To          string `json:"to"`
	Reason      string `json:"reason,omitzero"`
}

// ScenarioSubjectView is one linked identity. Identifiers stay masked unless
// RevealSensitive was deliberately set on the request.
type ScenarioSubjectView struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	InitialState string `json:"initial_state"`
	Namespace    string `json:"namespace,omitzero"`
	Identifier   string `json:"identifier,omitzero"`
	Patient      string `json:"patient,omitzero"`
	Masked       bool   `json:"masked"`
}

// ScenarioPreviewResult carries the shared-engine timeline for a designed workflow.
type ScenarioPreviewResult struct {
	State    State                 `json:"state"`
	Reason   string                `json:"reason,omitzero"`
	Scenario string                `json:"scenario,omitzero"`
	Version  string                `json:"version,omitzero"`
	Profile  string                `json:"profile,omitzero"`
	BaseTime string                `json:"base_time,omitzero"`
	Accepted int                   `json:"accepted,omitzero"`
	Refused  int                   `json:"refused,omitzero"`
	Subjects []ScenarioSubjectView `json:"subjects,omitzero"`
	Steps    []ScenarioStepView    `json:"steps,omitzero"`
}

func (r *ScenarioPreviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ScenarioDocumentResult opens, saves or validates one scenario document.
type ScenarioDocumentResult struct {
	State    State  `json:"state"`
	Reason   string `json:"reason,omitzero"`
	Document string `json:"document,omitzero"`
	Output   string `json:"output,omitzero"`
	Profile  string `json:"profile,omitzero"`
	ID       string `json:"id,omitzero"`
	Version  string `json:"version,omitzero"`
}

func (r *ScenarioDocumentResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ScenarioSaveRequest writes a scenario document to a new workspace entry.
type ScenarioSaveRequest struct {
	Workspace string `json:"workspace"`
	Document  string `json:"document"`
	Output    string `json:"output"`
}

// ScenarioGenerateRequest materializes streams from a generator plan and
// optionally registers a new generated case for inspection or test authoring.
type ScenarioGenerateRequest struct {
	Workspace         string `json:"workspace"`
	Document          string `json:"document"`
	OutputName        string `json:"output_name"`
	CaseName          string `json:"case_name,omitzero"`
	RegisterInProject bool   `json:"register_in_project,omitzero"`
	CaseTitle         string `json:"case_title,omitzero"`
	CaseOwner         string `json:"case_owner,omitzero"`
	CaseVersion       string `json:"case_version,omitzero"`
}

// ScenarioGenerateResult reports the generation directory, streams and optional case.
type ScenarioGenerateResult struct {
	State            State  `json:"state"`
	Reason           string `json:"reason,omitzero"`
	OutputPath       string `json:"output_path,omitzero"`
	GenerationPath   string `json:"generation_path,omitzero"`
	StreamCount      int    `json:"stream_count,omitzero"`
	CaseName         string `json:"case_name,omitzero"`
	CaseIdentity     string `json:"case_identity,omitzero"`
	ProvenanceMode   string `json:"provenance_mode,omitzero"`
	Registered       bool   `json:"registered,omitzero"`
	GeneratorSeed    uint64 `json:"generator_seed,omitzero"`
	GeneratorVersion string `json:"generator_version,omitzero"`
	ProfileVersion   string `json:"profile_version,omitzero"`
	BaseTime         string `json:"base_time,omitzero"`
}

func (r *ScenarioGenerateResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ScenarioLibraryRequest names library and expectations documents in a workspace.
type ScenarioLibraryRequest struct {
	Workspace    string `json:"workspace"`
	Library      string `json:"library"`
	Expectations string `json:"expectations,omitzero"`
	Output       string `json:"output,omitzero"`
	TemplateID   string `json:"template_id,omitzero"`
	TemplateVer  string `json:"template_version,omitzero"`
	Plan         string `json:"plan,omitzero"`
	Coverage     string `json:"coverage,omitzero"`
	Profile      string `json:"profile,omitzero"`
}

// ScenarioLibraryResult reports library templates, digests and check outcomes.
type ScenarioLibraryResult struct {
	State     State                         `json:"state"`
	Reason    string                        `json:"reason,omitzero"`
	Document  string                        `json:"document,omitzero"`
	Output    string                        `json:"output,omitzero"`
	Templates []ScenarioLibraryTemplateView `json:"templates,omitzero"`
	Streams   int                           `json:"streams,omitzero"`
	Fields    int                           `json:"fields,omitzero"`
	Target    string                        `json:"target,omitzero"`
	Compared  []ScenarioLibraryCompareView  `json:"compared,omitzero"`
}

// ScenarioLibraryTemplateView is one pinned library entry.
type ScenarioLibraryTemplateView struct {
	ID       string   `json:"id"`
	Version  string   `json:"version"`
	Profile  string   `json:"profile"`
	Coverage []string `json:"coverage"`
	PlanSHA  string   `json:"plan_sha256"`
}

// ScenarioLibraryCompareView names how two revisions differ without rewriting either.
type ScenarioLibraryCompareView struct {
	ID          string `json:"id"`
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	SamePlan    bool   `json:"same_plan"`
	FromSHA     string `json:"from_sha256"`
	ToSHA       string `json:"to_sha256"`
}

func (r *ScenarioLibraryResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ScenarioProfileBindRequest maps a local interface profile (#252) onto a
// lifecycle template family without substituting an unsupported workflow.
type ScenarioProfileBindRequest struct {
	Workspace string `json:"workspace"`
	Entry     string `json:"entry"`
	PackEntry string `json:"pack_entry,omitzero"`
}

// ScenarioProfileBindResult reports the local profile identity and the
// lifecycle profile that family selects, or why none is available.
type ScenarioProfileBindResult struct {
	State            State  `json:"state"`
	Reason           string `json:"reason,omitzero"`
	ProfileID        string `json:"profile_id,omitzero"`
	ProfileVersion   string `json:"profile_version,omitzero"`
	Family           string `json:"family,omitzero"`
	HL7Version       string `json:"hl7_version,omitzero"`
	LifecycleProfile string `json:"lifecycle_profile,omitzero"`
	GeneratorVersion string `json:"generator_version,omitzero"`
	Available        bool   `json:"available,omitzero"`
}

func (r *ScenarioProfileBindResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// SynthGenerateRequest writes the frozen SIU synthetic family from declared inputs.
type SynthGenerateRequest struct {
	Workspace        string `json:"workspace"`
	OutputName       string `json:"output_name"`
	Seed             uint64 `json:"seed"`
	BaseTime         string `json:"base_time"`
	GeneratorVersion string `json:"generator_version"`
	ProfileVersion   string `json:"profile_version"`
}

// SynthGenerateResult reports the written family and case paths.
type SynthGenerateResult struct {
	State      State    `json:"state"`
	Reason     string   `json:"reason,omitzero"`
	OutputPath string   `json:"output_path,omitzero"`
	Cases      []string `json:"cases,omitzero"`
}

func (r *SynthGenerateResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ScenarioCatalog reports supported lifecycle profiles and event availability.
func (a *App) ScenarioCatalog() ScenarioCatalogResult {
	return run(a, false, false, func(context.Context) ScenarioCatalogResult {
		catalog := scenario.SupportedCatalog()
		return ScenarioCatalogResult{State: Completed, Catalog: &catalog}
	})
}

// BindScenarioProfile selects the lifecycle profile implied by a saved local
// interface profile without substituting another family silently.
func (a *App) BindScenarioProfile(request ScenarioProfileBindRequest) ScenarioProfileBindResult {
	return run(a, false, false, func(context.Context) ScenarioProfileBindResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return ScenarioProfileBindResult{State: declined.state, Reason: declined.reason}
		}
		data, refused := workspaceDocument(root, request.Entry, 256<<10, "the local profile")
		if data == nil {
			return ScenarioProfileBindResult{State: refused.state, Reason: refused.reason}
		}
		profile, err := localprofile.Decode(data)
		if err != nil {
			return ScenarioProfileBindResult{State: Failed, Reason: err.Error()}
		}
		family := profile.Base.Family
		lifecycle, ok := lifecycleForFamily(family)
		result := ScenarioProfileBindResult{
			State:            Completed,
			ProfileID:        profile.Identity.ID,
			ProfileVersion:   profile.Identity.Version,
			Family:           family,
			HL7Version:       profile.Base.HL7Version,
			GeneratorVersion: "readmit-scenario-generator-v1",
			Available:        ok,
		}
		if !ok {
			result.Reason = "family " + family + " has no supported fixture lifecycle profile in this release"
			return result
		}
		result.LifecycleProfile = string(lifecycle)
		return result
	})
}

// PreviewScenario walks a designed workflow through the shared engine.
func (a *App) PreviewScenario(request ScenarioPreviewRequest) ScenarioPreviewResult {
	return run(a, false, false, func(context.Context) ScenarioPreviewResult {
		data, err := a.resolveScenarioDocument(request.Workspace, request.Document)
		if err != nil {
			return ScenarioPreviewResult{State: Failed, Reason: err.Error()}
		}
		timeline, err := scenario.PreviewDocument(data)
		if err != nil {
			return ScenarioPreviewResult{State: Failed, Reason: err.Error()}
		}
		return presentTimeline(timeline, request.RevealSensitive)
	})
}

// OpenScenario reads one scenario or order-scenario document from the workspace.
func (a *App) OpenScenario(workspace, entry string) ScenarioDocumentResult {
	return run(a, false, false, func(context.Context) ScenarioDocumentResult {
		data, err := a.resolveScenarioDocument(workspace, entry)
		if err != nil {
			return ScenarioDocumentResult{State: Failed, Reason: err.Error()}
		}
		return presentScenarioDocument(data)
	})
}

// SaveScenario writes a validated scenario document to a new workspace entry.
func (a *App) SaveScenario(request ScenarioSaveRequest) ScenarioDocumentResult {
	return run(a, false, true, func(context.Context) ScenarioDocumentResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return ScenarioDocumentResult{State: declined.state, Reason: declined.reason}
		}
		presented := presentScenarioDocument([]byte(request.Document))
		if presented.State != Completed {
			return presented
		}
		canonical := []byte(presented.Document)
		if err := writeWorkspaceEntry(root, request.Output, canonical); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return ScenarioDocumentResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
			}
			return ScenarioDocumentResult{State: Failed, Reason: err.Error()}
		}
		presented.Output = request.Output
		return presented
	})
}

// GenerateScenario materializes deterministic streams, retains generation.json,
// writes a new generated case beside them, and may register that case.
func (a *App) GenerateScenario(request ScenarioGenerateRequest) ScenarioGenerateResult {
	return run(a, true, true, func(ctx context.Context) ScenarioGenerateResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return ScenarioGenerateResult{State: declined.state, Reason: declined.reason}
		}
		if request.OutputName == "" {
			return ScenarioGenerateResult{State: Failed, Reason: "generation requires a new output directory name"}
		}
		data, err := a.resolveScenarioDocument(root, request.Document)
		if err != nil {
			data = []byte(request.Document)
		}
		plan, err := scenariogen.Decode(data)
		if err != nil {
			return ScenarioGenerateResult{State: Failed, Reason: err.Error()}
		}
		outputPath, err := artifactpath.Destination(filepath.Join(root, request.OutputName))
		if err != nil {
			return ScenarioGenerateResult{State: Failed, Reason: "generation destination must be a new directory entry of the open workspace"}
		}
		if _, err := scenariogen.Write(ctx, outputPath, data); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				return ScenarioGenerateResult{State: Cancelled, Reason: cancelledRefusal.reason}
			}
			if errors.Is(err, fs.ErrPermission) {
				return ScenarioGenerateResult{State: PermissionDenied, Reason: "this account cannot write generation output into the open workspace"}
			}
			return ScenarioGenerateResult{State: Failed, Reason: err.Error()}
		}
		timeline, err := scenario.PreviewDocument(plan.Template)
		if err != nil {
			return ScenarioGenerateResult{State: Failed, Reason: err.Error()}
		}
		result := ScenarioGenerateResult{
			State:            Completed,
			OutputPath:       outputPath,
			GenerationPath:   filepath.Join(outputPath, "generation.json"),
			GeneratorSeed:    plan.Seed,
			GeneratorVersion: plan.GeneratorVersion,
			ProfileVersion:   string(timeline.Profile),
			BaseTime:         timeline.BaseTime.Format(time.RFC3339),
		}
		entries, err := os.ReadDir(outputPath)
		if err != nil {
			return ScenarioGenerateResult{State: Failed, Reason: "cannot read generation output"}
		}
		var streams []string
		for _, entry := range entries {
			if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".mllp") {
				continue
			}
			streams = append(streams, filepath.Join(outputPath, entry.Name()))
		}
		result.StreamCount = len(streams)
		caseName := request.CaseName
		if caseName == "" {
			caseName = request.OutputName + "-case"
		}
		casePath, err := artifactpath.Destination(filepath.Join(root, caseName))
		if err != nil {
			return ScenarioGenerateResult{State: Failed, Reason: "case destination must be a new directory entry of the open workspace"}
		}
		inputs := make([]bundle.Input, 0, len(streams))
		for _, stream := range streams {
			raw, err := os.ReadFile(stream)
			if err != nil {
				return ScenarioGenerateResult{State: Failed, Reason: "cannot read generated stream"}
			}
			inputs = append(inputs, bundle.Input{Data: raw, Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}})
		}
		if len(inputs) == 0 {
			return ScenarioGenerateResult{State: Failed, Reason: "generation produced no streams"}
		}
		generator := bundle.GeneratorInputs{
			Seed:             plan.Seed,
			BaseTime:         timeline.BaseTime.UTC(),
			GeneratorVersion: plan.GeneratorVersion,
			ProfileVersion:   string(timeline.Profile),
		}
		written, err := bundle.Write(casePath, inputs, bundle.Provenance{Mode: bundle.Generated, Generator: &generator})
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return ScenarioGenerateResult{State: PermissionDenied, Reason: "this account cannot write a generated case into the open workspace"}
			}
			return ScenarioGenerateResult{State: Failed, Reason: err.Error()}
		}
		result.CaseName = caseName
		result.CaseIdentity = written.Identity
		result.ProvenanceMode = string(bundle.Generated)
		if request.RegisterInProject {
			if _, err := operation.RegisterCase(root, caseName, operation.CaseRegistration{
				Title:            request.CaseTitle,
				Owner:            request.CaseOwner,
				InterfaceVersion: request.CaseVersion,
			}); err != nil {
				return ScenarioGenerateResult{State: Failed, Reason: err.Error()}
			}
			result.Registered = true
		}
		return result
	})
}

// OpenScenarioLibrary reads a reusable scenario library document.
func (a *App) OpenScenarioLibrary(workspace, entry string) ScenarioLibraryResult {
	return run(a, false, false, func(context.Context) ScenarioLibraryResult {
		data, err := a.resolveScenarioDocument(workspace, entry)
		if err != nil {
			return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
		}
		return presentLibrary(data)
	})
}

// SaveScenarioLibraryEntry appends or versions a template without overwriting
// another pinned revision or silently changing its plan digest.
func (a *App) SaveScenarioLibraryEntry(request ScenarioLibraryRequest) ScenarioLibraryResult {
	return run(a, false, true, func(context.Context) ScenarioLibraryResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return ScenarioLibraryResult{State: declined.state, Reason: declined.reason}
		}
		if request.TemplateID == "" || request.TemplateVer == "" || request.Plan == "" || request.Profile == "" {
			return ScenarioLibraryResult{State: Failed, Reason: "library entry requires id, version, profile and plan"}
		}
		var library scenariolibrary.Library
		if request.Library != "" {
			existing, err := a.resolveScenarioDocument(root, request.Library)
			if err != nil {
				return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
			}
			if err := json.Unmarshal(existing, &library, json.RejectUnknownMembers(true)); err != nil {
				return ScenarioLibraryResult{State: Failed, Reason: "invalid scenario library document"}
			}
		} else {
			library = scenariolibrary.Library{Schema: "readmit-scenario-library/v1"}
		}
		planData := []byte(request.Plan)
		if decoded, err := a.resolveScenarioDocument(root, request.Plan); err == nil {
			planData = decoded
		}
		plan, err := scenariogen.Decode(planData)
		if err != nil {
			return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
		}
		digest, err := scenariolibrary.PlanDigest(planData)
		if err != nil {
			return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
		}
		coverage := splitCSV(request.Coverage)
		if len(coverage) == 0 {
			coverage = []string{"desktop"}
		}
		for _, template := range library.Templates {
			if template.ID == request.TemplateID && template.Version == request.TemplateVer {
				existing, err := scenariolibrary.PlanDigest(template.Plan)
				if err != nil {
					return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
				}
				if existing == digest {
					return ScenarioLibraryResult{State: Failed, Reason: "library already holds this exact template revision"}
				}
				return ScenarioLibraryResult{State: Failed, Reason: "cannot overwrite another library revision; bump the template version"}
			}
		}
		encodedPlan, err := json.Marshal(plan, json.Deterministic(true))
		if err != nil {
			return ScenarioLibraryResult{State: Failed, Reason: "cannot encode library plan"}
		}
		library.Templates = append(library.Templates, scenariolibrary.Template{
			ID: request.TemplateID, Version: request.TemplateVer, Profile: request.Profile,
			Coverage: coverage, Plan: jsontext.Value(encodedPlan),
		})
		out, err := json.Marshal(library, json.Deterministic(true), jsontext.WithIndent("  "))
		if err != nil {
			return ScenarioLibraryResult{State: Failed, Reason: "cannot encode scenario library"}
		}
		out = append(out, '\n')
		destination := request.Output
		if destination == "" {
			destination = request.Library
		}
		if destination == "" {
			return ScenarioLibraryResult{State: Failed, Reason: "library save requires an output entry"}
		}
		overwrite := request.Library != "" && destination == request.Library
		if err := writeWorkspaceEntry(root, destination, out, overwrite); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return ScenarioLibraryResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
			}
			return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
		}
		presented := presentLibrary(out)
		presented.Output = destination
		return presented
	})
}

// CompareScenarioLibraryEntries reports whether two pinned revisions share a
// plan. TemplateVer is the from-version; Expectations holds the to-version
// string for this read-only comparison (not an expectations document path).
func (a *App) CompareScenarioLibraryEntries(request ScenarioLibraryRequest) ScenarioLibraryResult {
	return run(a, false, false, func(context.Context) ScenarioLibraryResult {
		data, err := a.resolveScenarioDocument(request.Workspace, request.Library)
		if err != nil {
			return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
		}
		presented := presentLibrary(data)
		if presented.State != Completed {
			return presented
		}
		if request.TemplateID == "" || request.TemplateVer == "" || request.Expectations == "" {
			return ScenarioLibraryResult{State: Failed, Reason: "compare requires template id, from version and to version"}
		}
		var from, to *ScenarioLibraryTemplateView
		for i := range presented.Templates {
			template := &presented.Templates[i]
			if template.ID != request.TemplateID {
				continue
			}
			if template.Version == request.TemplateVer {
				from = template
			}
			if template.Version == request.Expectations {
				to = template
			}
		}
		if from == nil || to == nil {
			return ScenarioLibraryResult{State: Failed, Reason: "both template revisions must exist in the library"}
		}
		presented.Compared = []ScenarioLibraryCompareView{{
			ID: request.TemplateID, FromVersion: from.Version, ToVersion: to.Version,
			SamePlan: from.PlanSHA == to.PlanSHA, FromSHA: from.PlanSHA, ToSHA: to.PlanSHA,
		}}
		return presented
	})
}

// CheckScenarioLibrary runs independent fixture expectations against a library pin.
func (a *App) CheckScenarioLibrary(request ScenarioLibraryRequest) ScenarioLibraryResult {
	return run(a, true, false, func(ctx context.Context) ScenarioLibraryResult {
		library, err := a.resolveScenarioDocument(request.Workspace, request.Library)
		if err != nil {
			return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
		}
		expectations, err := a.resolveScenarioDocument(request.Workspace, request.Expectations)
		if err != nil {
			return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
		}
		result, err := scenariolibrary.Check(ctx, library, expectations)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				return ScenarioLibraryResult{State: Cancelled, Reason: cancelledRefusal.reason}
			}
			return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
		}
		presented := presentLibrary(library)
		presented.Streams = result.Streams
		presented.Fields = result.Fields
		presented.Target = result.Target
		return presented
	})
}

// ExportScenarioLibrary copies a library document to a new destination without
// changing pinned revisions.
func (a *App) ExportScenarioLibrary(request ScenarioLibraryRequest) ScenarioLibraryResult {
	return run(a, false, true, func(context.Context) ScenarioLibraryResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return ScenarioLibraryResult{State: declined.state, Reason: declined.reason}
		}
		data, err := a.resolveScenarioDocument(root, request.Library)
		if err != nil {
			return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
		}
		if request.Output == "" {
			return ScenarioLibraryResult{State: Failed, Reason: "export requires a new destination entry"}
		}
		if err := writeWorkspaceEntry(root, request.Output, data); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return ScenarioLibraryResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
			}
			return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
		}
		presented := presentLibrary(data)
		presented.Output = request.Output
		return presented
	})
}

// ImportScenarioLibrary copies an external library document into the workspace
// as a new entry; existing revisions are never overwritten.
func (a *App) ImportScenarioLibrary(request ScenarioLibraryRequest) ScenarioLibraryResult {
	return run(a, false, true, func(context.Context) ScenarioLibraryResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return ScenarioLibraryResult{State: declined.state, Reason: declined.reason}
		}
		data, err := readChosenFile(request.Library, scenariolibrary.MaxBytes)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return ScenarioLibraryResult{State: PermissionDenied, Reason: "this account cannot read the selected library file"}
			}
			return ScenarioLibraryResult{State: Failed, Reason: "cannot read the selected library file"}
		}
		presented := presentLibrary(data)
		if presented.State != Completed {
			return presented
		}
		if request.Output == "" {
			return ScenarioLibraryResult{State: Failed, Reason: "import requires a new destination entry"}
		}
		if err := writeWorkspaceEntry(root, request.Output, data); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return ScenarioLibraryResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
			}
			return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
		}
		presented.Output = request.Output
		return presented
	})
}

// GenerateSynth writes the reproducible SIU synthetic family from declared inputs.
func (a *App) GenerateSynth(request SynthGenerateRequest) SynthGenerateResult {
	return run(a, true, true, func(ctx context.Context) SynthGenerateResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return SynthGenerateResult{State: declined.state, Reason: declined.reason}
		}
		baseTime, err := time.Parse(time.RFC3339, request.BaseTime)
		if err != nil {
			return SynthGenerateResult{State: Failed, Reason: "base time must be an RFC 3339 instant"}
		}
		if request.OutputName == "" {
			return SynthGenerateResult{State: Failed, Reason: "synth requires a new output directory name"}
		}
		outputPath, err := artifactpath.Destination(filepath.Join(root, request.OutputName))
		if err != nil {
			return SynthGenerateResult{State: Failed, Reason: "synth destination must be a new directory entry of the open workspace"}
		}
		if err := ctx.Err(); err != nil {
			return SynthGenerateResult{State: Cancelled, Reason: cancelledRefusal.reason}
		}
		manifest, err := synth.Write(outputPath, bundle.GeneratorInputs{
			Seed:             request.Seed,
			BaseTime:         baseTime.UTC(),
			GeneratorVersion: request.GeneratorVersion,
			ProfileVersion:   request.ProfileVersion,
		})
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return SynthGenerateResult{State: PermissionDenied, Reason: "this account cannot write synthetic fixtures into the open workspace"}
			}
			return SynthGenerateResult{State: Failed, Reason: err.Error()}
		}
		cases := make([]string, 0, len(manifest.Cases))
		for _, item := range manifest.Cases {
			cases = append(cases, item.Path)
		}
		return SynthGenerateResult{State: Completed, OutputPath: outputPath, Cases: cases}
	})
}

func lifecycleForFamily(family string) (scenario.ProfileName, bool) {
	switch strings.ToUpper(family) {
	case "ADT":
		return scenario.ADTLifecycle, true
	case "SIU":
		return scenario.SIULifecycle, true
	case "ORM":
		return scenario.ORMLifecycle, true
	case "ORU":
		return scenario.ORULifecycle, true
	default:
		return "", false
	}
}

func (a *App) resolveScenarioDocument(workspace, fileOrDoc string) ([]byte, error) {
	trimmed := strings.TrimSpace(fileOrDoc)
	if trimmed == "" {
		return nil, errors.New("a scenario document is required")
	}
	if strings.HasPrefix(trimmed, "{") {
		return []byte(trimmed), nil
	}
	if filepath.IsAbs(trimmed) {
		// A chosen file may be a library as well as a scenario, so it is held
		// to the larger of the two bounds; each reader applies its own.
		data, err := readChosenFile(trimmed, scenariolibrary.MaxBytes)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return nil, errors.New("this account cannot read the scenario document")
			}
			return nil, errors.New("cannot read scenario document")
		}
		return data, nil
	}
	root, declined := resolveFolder(workspace)
	if root == "" {
		return nil, errors.New(declined.reason)
	}
	data, refused := workspaceDocument(root, trimmed, scenariogen.MaxBytes, "the scenario document")
	if data == nil {
		return nil, errors.New(refused.reason)
	}
	return data, nil
}

// readChosenFile reads one file a person chose by path under the bound its
// contract is held to. It must be a regular file once any link is followed,
// so a FIFO, a device or an oversized file is refused rather than opened and
// read to its end.
func readChosenFile(path string, limit int64) ([]byte, error) {
	outside := errors.New("not a regular file within the bound")
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return nil, err
	case !info.Mode().IsRegular() || info.Size() > limit:
		return nil, outside
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err == nil && int64(len(data)) > limit {
		return nil, outside
	}
	return data, err
}

func presentTimeline(timeline scenario.Timeline, reveal bool) ScenarioPreviewResult {
	subjects := make([]ScenarioSubjectView, 0, len(timeline.Subjects))
	for _, subject := range timeline.Subjects {
		view := ScenarioSubjectView{
			ID: subject.ID, Kind: string(subject.Kind), InitialState: string(subject.InitialState),
			Patient: subject.Patient, Masked: !reveal,
		}
		if reveal {
			view.Namespace = subject.Namespace
			view.Identifier = subject.Identifier
		}
		subjects = append(subjects, view)
	}
	steps := make([]ScenarioStepView, 0, len(timeline.Steps))
	for _, step := range timeline.Steps {
		steps = append(steps, ScenarioStepView{
			Ordinal: step.Ordinal, ID: step.ID, At: step.At.Format(time.RFC3339),
			Event: string(step.Event), Description: step.Description, Subject: step.Subject,
			Into: step.Into, Expect: string(step.Expect), From: string(step.From),
			To: string(step.To), Reason: step.Reason,
		})
	}
	return ScenarioPreviewResult{
		State: Completed, Scenario: timeline.Scenario.ID, Version: timeline.Scenario.Version,
		Profile: string(timeline.Profile), BaseTime: timeline.BaseTime.Format(time.RFC3339),
		Accepted: timeline.Accepted, Refused: timeline.Refused, Subjects: subjects, Steps: steps,
	}
}

func presentScenarioDocument(data []byte) ScenarioDocumentResult {
	if orders, err := scenario.DecodeOrders(data); err == nil {
		canonical, err := json.Marshal(orders, json.Deterministic(true), jsontext.WithIndent("  "))
		if err != nil {
			return ScenarioDocumentResult{State: Failed, Reason: "scenario could not be canonicalized"}
		}
		return ScenarioDocumentResult{
			State: Completed, Document: string(append(canonical, '\n')),
			Profile: string(orders.Profile), ID: orders.Scenario.ID, Version: orders.Scenario.Version,
		}
	}
	designed, err := scenario.Decode(data)
	if err != nil {
		return ScenarioDocumentResult{State: Failed, Reason: err.Error()}
	}
	canonical, err := json.Marshal(designed, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return ScenarioDocumentResult{State: Failed, Reason: "scenario could not be canonicalized"}
	}
	return ScenarioDocumentResult{
		State: Completed, Document: string(append(canonical, '\n')),
		Profile: string(designed.Profile), ID: designed.Scenario.ID, Version: designed.Scenario.Version,
	}
}

func presentLibrary(data []byte) ScenarioLibraryResult {
	var library scenariolibrary.Library
	if err := json.Unmarshal(data, &library, json.RejectUnknownMembers(true)); err != nil {
		return ScenarioLibraryResult{State: Failed, Reason: "invalid scenario library document"}
	}
	if library.Schema != "readmit-scenario-library/v1" {
		return ScenarioLibraryResult{State: Failed, Reason: "unsupported library schema"}
	}
	views := make([]ScenarioLibraryTemplateView, 0, len(library.Templates))
	for _, template := range library.Templates {
		digest, err := scenariolibrary.PlanDigest(template.Plan)
		if err != nil {
			return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
		}
		views = append(views, ScenarioLibraryTemplateView{
			ID: template.ID, Version: template.Version, Profile: template.Profile,
			Coverage: append([]string{}, template.Coverage...), PlanSHA: digest,
		})
	}
	canonical, err := json.Marshal(library, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return ScenarioLibraryResult{State: Failed, Reason: "library could not be canonicalized"}
	}
	return ScenarioLibraryResult{
		State: Completed, Document: string(append(canonical, '\n')), Templates: views,
	}
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
