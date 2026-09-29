package desktop

import (
	"cmp"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/suite"
)

// A suite leaves the project, or arrives in it, only where a person chooses:
// a readmit-suite/v1 file imported into a new draft, one version's suite
// document exported to a new file, and one version compiled for one of its
// environments into a new run configuration folder, exactly as `readmit
// suite prepare` writes one. Nothing here sends.

// importSuite reads the readmit-suite/v1 file a person chose into a new
// suite draft: every table, test, binding and expected override kept, each
// reference resolved from the file's own folder where the project holds
// what it names and kept as declared where it does not. Nothing is saved.
func (a *App) importSuite(ctx context.Context, request RequestContext) ItemDraftResult {
	result := ItemDraftResult{Context: request}
	loaded, declined := a.loadCatalog(ctx, request, false)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	files, declined := a.chooseFiles(ctx, "Import suite", "Suite", "*.json")
	if len(files) == 0 {
		result.refuse(declined.state, declined.reason)
		return result
	}
	data, err := boundedFile(files[0], suite.MaxBytes)
	if err != nil {
		result.refuse(Failed, err.Error())
		return result
	}
	document, err := suite.Decode(data)
	if err != nil {
		result.refuse(Failed, "that file is not a suite this release reads: "+err.Error())
		return result
	}
	draft := loaded.draftOfDocument(document, filepath.Dir(files[0]))
	result.State, result.New, result.Ref = Completed, true, &ItemRef{Kind: SuiteItem}
	result.Draft = &ItemDraft{Name: readableName(document.ID), Suite: &draft}
	result.Suite = &SuiteContext{Tests: loaded.suiteTestVersions(suiteTestRefs(draft)), Document: string(data), Runnable: len(draft.Tests) > 0 && len(draft.Environments) > 0}
	if _, problems := loaded.planSuite(draft); len(problems) > 0 {
		result.Problems = problems
	}
	return result
}

// exportedSuite is how an exported suite document is written: as a new file,
// never over one.
var exportedSuite = artifactdir.Document{
	Errors: artifactdir.DocumentErrors{
		Create: errors.New("the suite must be exported to a new file in a folder this account can write"),
		Write:  errors.New("the suite could not be written completely; nothing is left there"),
	},
}

// exportSuite writes one version's suite document — the compiled member of
// a managed version, or an original file's own bytes — to a new file the
// person names.
func (a *App) exportSuite(ctx context.Context, request SuiteExportRequest) SuiteExportResult {
	result := SuiteExportResult{Context: request.Context}
	loaded, item, version, declined := a.exportedVersion(ctx, request)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	destination, declined := a.chooseNamedDestination(ctx, "Export suite", fileName(loaded.suiteName(item), "suite")+".json")
	if destination == "" {
		result.refuse(declined.state, declined.reason)
		return result
	}
	if err := exportedSuite.Create(destination, version.data); err != nil {
		result.refuse(Failed, err.Error())
		return result
	}
	result.State, result.Output = Completed, destination
	return result
}

// exportRunConfiguration compiles one version against the current revision
// of each environment it binds, for one of its environments, into a new
// folder the person names, through the one preparation `readmit suite
// prepare` makes, with the release pins of the version's latest baseline
// when it has one. Nothing is sent.
func (a *App) exportRunConfiguration(ctx context.Context, request SuiteExportRequest) SuiteExportResult {
	result := SuiteExportResult{Context: request.Context}
	if request.Environment == "" {
		result.refuse(Failed, "choose the environment this run configuration is compiled for")
		return result
	}
	loaded, item, version, declined := a.exportedVersion(ctx, request)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	at := slices.IndexFunc(version.draft.Environments, func(environment SuiteEnvironment) bool {
		return environment.ID == request.Environment || environment.Name == request.Environment
	})
	if at < 0 {
		result.refuse(Failed, "choose one of this suite's environments")
		return result
	}
	_, data, declined := loaded.compiled(version)
	if data == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	destination, declined := a.chooseDestination(ctx, "Export run configuration")
	if destination == "" {
		result.refuse(declined.state, declined.reason)
		return result
	}
	references := ""
	if pins := latestBaseline(loaded.suiteApprovals(item.ID), version.label); pins != nil {
		_, sidecar, remove, err := materializeReleases(pins.Tests)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		defer remove()
		references = sidecar
	}
	path, remove, err := placeCompiled(loaded.root, data)
	if err != nil {
		result.refuse(Failed, err.Error())
		return result
	}
	defer remove()
	if _, err := suite.Prepare(suite.Request{Path: path, Environment: version.draft.Environments[at].ID, Output: destination, References: references}); err != nil {
		result.refuse(Failed, err.Error())
		return result
	}
	result.State, result.Output = Completed, destination
	return result
}

// exportedVersion is the runnable suite version an export names.
func (a *App) exportedVersion(ctx context.Context, request SuiteExportRequest) (*loadedCatalog, catalog.Item, *suiteVersion, refusal) {
	if request.Suite.Kind != SuiteItem {
		return nil, catalog.Item{}, nil, refusal{Failed, "only a suite is exported as a suite"}
	}
	loaded, _, refused := a.catalogItem(ctx, request.Context, request.Suite, false)
	if loaded == nil {
		return nil, catalog.Item{}, nil, refusal{refused.State, refused.Reason}
	}
	item := loaded.document.Items[loaded.document.Find(request.Suite.ID)]
	version, err := loaded.suiteVersion(item, request.Suite.Revision)
	if err != nil {
		return nil, catalog.Item{}, nil, refusal{Failed, "this suite cannot be read: " + err.Error()}
	}
	if !version.runnable() {
		return nil, catalog.Item{}, nil, refusal{Failed, notRunnable}
	}
	return loaded, item, version, refusal{}
}

// fileName is a name offered for a new file: the object's name with every
// path separator replaced.
func fileName(name, fallback string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '-'
		}
		return r
	}, cmp.Or(name, fallback))
}

// suiteForRun is the suite version a run is handed, compiled against the
// current revision of each environment it binds: the project it is read
// from, the version and its compiled bytes.
func (a *App) suiteForRun(ctx context.Context, target SuiteRunTarget) (*loadedCatalog, *suiteVersion, []byte, refusal) {
	if target.Suite.Kind != SuiteItem {
		return nil, nil, nil, refusal{Failed, "a suite run names one saved suite version"}
	}
	loaded, _, refused := a.catalogItem(ctx, target.Context, target.Suite, false)
	if loaded == nil {
		return nil, nil, nil, refusal{refused.State, refused.Reason}
	}
	item := loaded.document.Items[loaded.document.Find(target.Suite.ID)]
	version, err := loaded.suiteVersion(item, target.Suite.Revision)
	if err != nil {
		return nil, nil, nil, refusal{Failed, "this suite cannot be read: " + err.Error()}
	}
	_, data, declined := loaded.compiled(version)
	if data == nil {
		return nil, nil, nil, declined
	}
	return loaded, version, data, refusal{}
}
