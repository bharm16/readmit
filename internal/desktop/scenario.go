package desktop

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/scenario"
	"github.com/bharm16/readmit/internal/scenariolibrary"
	"github.com/bharm16/readmit/internal/synth"
)

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

// readChosenFile reads one file a person chose by path under the bound its
// contract is held to, through the shared document store. It must be a
// regular file once any link is followed, so a FIFO, a device or an oversized
// file is refused rather than opened and read to its end, and a file this
// account cannot read keeps the filesystem's refusal behind the sentence.
func readChosenFile(path string, limit int64) ([]byte, error) {
	chosen := chosenFile
	chosen.MaxBytes = int(limit)
	return chosen.Read(path)
}

// chosenFile is how a file a person chose by path is read.
var chosenFile = artifactdir.Document{
	Links:    artifactdir.FollowLinks,
	Refusals: artifactdir.DocumentRefusals{Irregular: errors.New("not a regular file within the bound")},
}

// ScenarioLibraryRequest names library and expectations documents in a workspace.
type ScenarioLibraryRequest struct {
	Workspace    string `json:"workspace"`
	Library      string `json:"library"`
	Expectations string `json:"expectations,omitzero"`
}

// ScenarioLibraryResult reports library templates, digests and check outcomes.
type ScenarioLibraryResult struct {
	State     State                         `json:"state"`
	Reason    string                        `json:"reason,omitzero"`
	Document  string                        `json:"document,omitzero"`
	Templates []ScenarioLibraryTemplateView `json:"templates,omitzero"`
	Streams   int                           `json:"streams,omitzero"`
	Fields    int                           `json:"fields,omitzero"`
	Target    string                        `json:"target,omitzero"`
}

func (r *ScenarioLibraryResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ScenarioLibraryTemplateView is one pinned library entry.
type ScenarioLibraryTemplateView struct {
	ID       string   `json:"id"`
	Version  string   `json:"version"`
	Profile  string   `json:"profile"`
	Coverage []string `json:"coverage"`
	PlanSHA  string   `json:"plan_sha256"`
}

// SynthGenerateRequest writes the frozen SIU synthetic family from declared
// inputs. The seed crosses the facade as the text a person typed and is read
// as `readmit synth --seed` reads it, so every seed the command accepts, up to
// 2^64-1, is one the window can declare exactly.
type SynthGenerateRequest struct {
	Workspace        string `json:"workspace"`
	OutputName       string `json:"output_name"`
	Seed             string `json:"seed"`
	BaseTime         string `json:"base_time"`
	GeneratorVersion string `json:"generator_version"`
	ProfileVersion   string `json:"profile_version"`
}

// SynthGenerateResult reports the written family and case paths, and each
// case bundle's identity as `readmit synth` prints it.
type SynthGenerateResult struct {
	State      State              `json:"state"`
	Reason     string             `json:"reason,omitzero"`
	OutputPath string             `json:"output_path,omitzero"`
	Cases      []string           `json:"cases,omitzero"`
	Variants   []SynthVariantView `json:"variants,omitzero"`
}

func (r *SynthGenerateResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SynthVariantView is one case bundle of a written family, as the family's
// readmit-synth/v1 completion record lists it.
type SynthVariantView struct {
	Variant     string `json:"variant"`
	Path        string `json:"path"`
	Identity    string `json:"identity"`
	KnownDefect string `json:"known_defect,omitzero"`
}

// scenarioCheckOperation names a fixture check while it holds the slot, so
// the scenario panel's Cancel stops the check it started and nothing else.
const scenarioCheckOperation = "scenario-check"

// CheckScenarioLibrary is `readmit scenario check-library`: it reads the
// library and the independent expectations under the command's 4 MiB bound
// and runs scenariolibrary.Check on them. It is interruptible; a cancelled
// check passes nothing and retains nothing, because the check regenerates in
// memory and writes nothing anywhere.
func (a *App) CheckScenarioLibrary(request ScenarioLibraryRequest) ScenarioLibraryResult {
	return runNamed[ScenarioLibraryResult, *ScenarioLibraryResult](a, profiles["CheckScenarioLibrary"], func(ctx context.Context) ScenarioLibraryResult {
		library, err := a.resolveScenarioDocument(request.Workspace, request.Library, scenariolibrary.MaxBytes)
		if err != nil {
			return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
		}
		expectations, err := a.resolveScenarioDocument(request.Workspace, request.Expectations, scenariolibrary.MaxBytes)
		if err != nil {
			return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
		}
		result, err := scenariolibrary.Check(ctx, library, expectations)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				return ScenarioLibraryResult{State: Cancelled, Reason: "the fixture check was cancelled before it finished; it passed nothing"}
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

// GenerateSynth writes the reproducible SIU synthetic family from declared inputs.
func (a *App) GenerateSynth(request SynthGenerateRequest) SynthGenerateResult {
	return run(a, true, true, func(ctx context.Context) SynthGenerateResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return SynthGenerateResult{State: declined.state, Reason: declined.reason}
		}
		// The command reads --seed as its flag library reads every unsigned
		// number and --base-time through the shared declaration, so one
		// spelling is one input in both places and is refused in the same
		// words.
		seed, err := strconv.ParseUint(request.Seed, 0, 64)
		if err != nil {
			return SynthGenerateResult{State: Failed, Reason: "the seed must be a whole number from 0 to " + strconv.FormatUint(^uint64(0), 10)}
		}
		baseTime, err := operation.DeclaredBaseTime(request.BaseTime)
		if err != nil {
			return SynthGenerateResult{State: Failed, Reason: err.Error()}
		}
		if request.OutputName == "" {
			return SynthGenerateResult{State: Failed, Reason: "synth requires a new output directory name"}
		}
		if artifactpath.EntryName(request.OutputName) != nil {
			return SynthGenerateResult{State: Failed, Reason: "synth destination must be a new directory entry of the open workspace"}
		}
		outputPath, err := artifactpath.Destination(filepath.Join(root, request.OutputName))
		if err != nil {
			return SynthGenerateResult{State: Failed, Reason: "synth destination must be a new directory entry of the open workspace"}
		}
		if err := ctx.Err(); err != nil {
			return SynthGenerateResult{State: Cancelled, Reason: cancelledRefusal.reason}
		}
		manifest, err := synth.Write(outputPath, bundle.GeneratorInputs{
			Seed:             seed,
			BaseTime:         baseTime,
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
		variants := make([]SynthVariantView, 0, len(manifest.Cases))
		for _, item := range manifest.Cases {
			cases = append(cases, item.Path)
			variants = append(variants, SynthVariantView{Variant: item.Variant, Path: item.Path, Identity: item.Identity, KnownDefect: item.KnownDefect})
		}
		return SynthGenerateResult{State: Completed, OutputPath: outputPath, Cases: cases, Variants: variants}
	})
}

// resolveScenarioDocument reads a document given inline, by absolute path or
// as one entry of the workspace. An entry is held to the bound its caller
// names: a scenario or plan to the generator's, a library or expectations
// document to the 4 MiB the command's library reader accepts.
func (a *App) resolveScenarioDocument(workspace, fileOrDoc string, limit int) ([]byte, error) {
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
	data, refused := workspaceDocument(root, trimmed, limit, "the scenario document")
	if data == nil {
		return nil, errors.New(refused.reason)
	}
	return data, nil
}

// presentLibrary reads a library with the reader `readmit scenario
// check-library` uses, so the window opens, exports and imports exactly the
// libraries the command reads and refuses the rest in the command's words.
func presentLibrary(data []byte) ScenarioLibraryResult {
	library, err := scenariolibrary.Decode(data)
	if err != nil {
		return ScenarioLibraryResult{State: Failed, Reason: err.Error()}
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
