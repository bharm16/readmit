package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// One editor Save validates and publishes one logical revision. An object
// the application saves is published whole through internal/catalog: every
// member is validated first, staged as a new file of the project, read back
// through its own reader, and only then named current. Nothing here writes a
// file an older panel wrote, and nothing is reported saved until the whole
// revision is.

// savedKinds are the kinds this release saves whole. Each one's editor lives
// with its screen; the guarantees are these.
var savedKinds = []ItemKind{EnvironmentItem, TestItem, ObservationItem}

// ItemDraft is the whole of one object as its editor holds it: the name a
// person gives it and the one member of its kind. Environment is a target
// configuration, Test a test draft answered against its source case, and
// Observation a source together with the window it is collected through.
type ItemDraft struct {
	Name        string            `json:"name,omitzero"`
	Environment *replay.Target    `json:"environment,omitzero"`
	Test        *testauthor.Draft `json:"test,omitzero"`
	Observation *ObservationDraft `json:"observation,omitzero"`
}

// ObservationDraft is an observation source and its window, which only mean
// something together and are published together or not at all.
type ObservationDraft struct {
	Source observesource.Source `json:"source"`
	Window observewindow.Window `json:"window"`
}

// FieldProblem is one reason a draft cannot be saved, at the member it is
// about.
type FieldProblem struct {
	Field   string `json:"field"`
	Problem string `json:"problem"`
}

// DraftRequest is one draft to validate.
type DraftRequest struct {
	Context RequestContext `json:"context"`
	Kind    ItemKind       `json:"kind"`
	Draft   ItemDraft      `json:"draft"`
}

// DraftValidation is what validating a draft found: every problem, or the
// normalized draft a save would publish.
type DraftValidation struct {
	State      State          `json:"state"`
	Reason     string         `json:"reason,omitzero"`
	Context    RequestContext `json:"context"`
	Problems   []FieldProblem `json:"problems"`
	Projection *ItemDraft     `json:"projection,omitzero"`
}

func (r *DraftValidation) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ValidateDraft validates a whole draft through the readers a save stages it
// through, and writes nothing. Validation is also the first step of every
// save, so a window never needs to ask for it before saving.
func (a *App) ValidateDraft(request DraftRequest) DraftValidation {
	return run(a, false, false, func(ctx context.Context) DraftValidation {
		result := DraftValidation{Context: request.Context, Problems: []FieldProblem{}}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		staged, projection, problems := validateItemDraft(root, request.Kind, request.Draft)
		result.State, result.Problems = Completed, problems
		if staged != nil {
			result.Projection = projection
		}
		return result
	})
}

// SaveOutcome is what one save did.
type SaveOutcome string

const (
	// SavedOutcome: the whole revision is published and current.
	SavedOutcome SaveOutcome = "saved"
	// InvalidOutcome: the draft has field problems; nothing was written.
	InvalidOutcome SaveOutcome = "invalid"
	// ConflictOutcome: the object changed since the edit began; nothing was
	// published and the draft is the window's to keep.
	ConflictOutcome SaveOutcome = "conflict"
	// FailedOutcome: the save did not publish. When Operation is set, the work
	// it staged is recoverable under that operation; the previous revision
	// is current.
	FailedOutcome SaveOutcome = "failed"
)

// SaveItemRequest is one deliberate save of a whole draft. Item is empty to
// create an object, and to save a copy of one under a new identity.
// BaseRevision is the revision the edit began from. IntentID is allocated
// once when the person submits and reused for every retry of that submission.
type SaveItemRequest struct {
	Context      RequestContext `json:"context"`
	Kind         ItemKind       `json:"kind"`
	Item         string         `json:"item,omitzero"`
	BaseRevision string         `json:"base_revision,omitzero"`
	Draft        ItemDraft      `json:"draft"`
	IntentID     string         `json:"intent_id"`
}

// SaveItemResult answers one save. Saved names the published revision; the
// window refreshes its lists afterwards, and a refresh that fails does not
// unsay a save.
type SaveItemResult struct {
	State           State          `json:"state"`
	Reason          string         `json:"reason,omitzero"`
	Context         RequestContext `json:"context"`
	Outcome         SaveOutcome    `json:"outcome"`
	Saved           *ItemRef       `json:"saved,omitzero"`
	Replayed        bool           `json:"replayed"`
	Projection      *ItemDraft     `json:"projection,omitzero"`
	Problems        []FieldProblem `json:"problems"`
	CurrentRevision string         `json:"current_revision,omitzero"`
	Operation       string         `json:"operation,omitzero"`
}

func (r *SaveItemResult) refuse(state State, reason string) {
	r.State, r.Reason, r.Outcome = state, reason, FailedOutcome
}

// SaveItem publishes one whole draft as one revision, or nothing. The draft
// is validated first; a base that is not current is a conflict; a submission
// already published is answered with its revision and written nowhere again;
// and a different submission under the same identity is refused.
func (a *App) SaveItem(request SaveItemRequest) SaveItemResult {
	return run(a, false, true, func(ctx context.Context) SaveItemResult {
		result := SaveItemResult{Context: request.Context, Problems: []FieldProblem{}}
		if !slices.Contains(savedKinds, request.Kind) {
			result.refuse(Failed, "this release saves environments, tests and observations whole")
			return result
		}
		// Recording the catalog first settles interrupted saves and records
		// the object this save edits, if it was only discovered so far.
		loaded, declined := a.loadCatalog(ctx, request.Context, true)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		root, store := loaded.root, loaded.store
		staged, projection, problems := validateItemDraft(root, request.Kind, request.Draft)
		if staged == nil {
			result.State, result.Outcome, result.Problems = Failed, InvalidOutcome, problems
			result.Reason = "the draft has problems to fix; nothing was saved"
			return result
		}
		saved, err := store.Save(catalog.Draft{
			Kind: string(request.Kind), ItemID: request.Item, Name: request.Draft.Name, Base: request.BaseRevision,
			Intent: request.IntentID, Digest: submissionDigest(request, staged), Members: staged,
		}, verifierFor(request.Kind), catalog.Options{Now: a.now, Fault: a.saveFault})
		var conflict *catalog.Conflict
		switch {
		case errors.As(err, &conflict):
			result.State, result.Outcome, result.CurrentRevision = Failed, ConflictOutcome, conflict.Current
			result.Reason = "the object changed since this edit began; nothing was saved and the draft is kept"
			return result
		case errors.Is(err, catalog.ErrIntentReused):
			result.refuse(Failed, "this submission was already used for different content; nothing was saved")
			return result
		case errors.Is(err, catalog.ErrTooManyPending):
			result.refuse(Failed, "this project holds as many interrupted saves as it keeps; retry or discard one first. Nothing was saved")
			return result
		case errors.Is(err, catalog.ErrNoItem):
			result.refuse(Failed, "the project holds no such object")
			return result
		case err != nil:
			result.refuse(Failed, "the save did not complete; the previous revision is still current")
			if store.Pending(request.IntentID) {
				result.Operation = request.IntentID
			}
			return result
		}
		result.State, result.Outcome, result.Replayed, result.Projection = Completed, SavedOutcome, saved.Replayed, projection
		result.Saved = &ItemRef{Kind: request.Kind, ID: saved.Item.ID, Revision: revisionLabel(saved.Revision)}
		return result
	})
}

// IncompleteSaveRequest names one save an interruption left unpublished.
type IncompleteSaveRequest struct {
	Context   RequestContext `json:"context"`
	Operation string         `json:"operation"`
}

// DiscardIncompleteSave drops one unpublished save: the files it staged,
// which no revision names, and its pending record. The current revision is
// not touched.
func (a *App) DiscardIncompleteSave(request IncompleteSaveRequest) CatalogResult {
	return run(a, false, true, func(ctx context.Context) CatalogResult {
		result := CatalogResult{Context: request.Context}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		store, err := catalog.Open(root)
		if err == nil {
			err = store.Discard(request.Operation)
		}
		if err != nil {
			result.refuse(Failed, "no incomplete save of this project is held under that operation")
			return result
		}
		result.State = Completed
		return result
	})
}

// projectRoot resolves the project a request names and refuses a folder that
// now holds a different project than the window opened.
func (a *App) projectRoot(ctx context.Context, request RequestContext) (string, refusal) {
	opened, declined := openProjectFolder(request.Project)
	if opened == nil {
		return "", declined
	}
	if request.ProjectID != "" {
		store, err := catalog.Open(opened.Root)
		if err != nil {
			return "", refusal{Failed, "a project must be an existing folder that is not a symbolic link"}
		}
		document, present, err := store.Read()
		if err != nil {
			return "", refusal{Failed, "the project's catalog cannot be read; it is left exactly as written"}
		}
		if !present || document.Project.ID != request.ProjectID {
			return "", refusal{Failed, errForeignProject.Error()}
		}
	}
	return opened.Root, refusal{}
}

// submissionDigest is the digest of everything a submission asks for, so the
// same submission is recognized and another under the same identity is not.
func submissionDigest(request SaveItemRequest, staged []catalog.Staged) string {
	digest := sha256.New()
	for _, part := range []string{string(request.Kind), request.Item, request.BaseRevision, request.Draft.Name} {
		digest.Write([]byte(part + "\x00"))
	}
	for _, member := range staged {
		sum := sha256.Sum256(member.Data)
		digest.Write([]byte(member.Role + "\x00" + hex.EncodeToString(sum[:]) + "\x00"))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// validateItemDraft validates a whole draft of kind and answers the files a save
// stages and the normalized draft, or every problem found.
func validateItemDraft(root string, kind ItemKind, draft ItemDraft) ([]catalog.Staged, *ItemDraft, []FieldProblem) {
	problems := []FieldProblem{}
	if draft.Name != "" && !catalog.ValidName(draft.Name) {
		problems = append(problems, FieldProblem{"name", "a name is 1 to 200 bytes of printable text"})
	}
	var staged []catalog.Staged
	normalized := ItemDraft{Name: draft.Name}
	switch kind {
	case EnvironmentItem:
		if draft.Environment == nil {
			return nil, nil, append(problems, FieldProblem{"environment", "an environment is a target configuration"})
		}
		target, data, err := declaredTarget(*draft.Environment)
		if err != nil {
			problems = append(problems, FieldProblem{"environment", err.Error()})
			break
		}
		normalized.Environment = &target
		staged = []catalog.Staged{{Role: "target", File: "target.json", Data: data}}
	case TestItem:
		if draft.Test == nil {
			return nil, nil, append(problems, FieldProblem{"test", "a test is answered from its source case"})
		}
		_, source, declined := openedCase(root, draft.Test.Case.Entry, draft.Test.Case.Identity)
		if source == nil {
			problems = append(problems, FieldProblem{"test.case", declined.reason})
			break
		}
		if _, err := testauthor.Resolve(root, source, *draft.Test); err != nil {
			problems = append(problems, FieldProblem{"test", err.Error()})
			break
		}
		data, err := testauthor.Generate(*draft.Test)
		if err != nil {
			problems = append(problems, FieldProblem{"test", err.Error()})
			for i, stage := range testauthor.Missing(*draft.Test) {
				if i > 0 {
					problems = append(problems, FieldProblem{"test." + stage, "not answered yet"})
				}
			}
			break
		}
		test := *draft.Test
		normalized.Test = &test
		staged = []catalog.Staged{{Role: "test", File: "test.json", Data: data}}
	case ObservationItem:
		if draft.Observation == nil {
			return nil, nil, append(problems, FieldProblem{"observation", "an observation is a source and its window"})
		}
		source, err := observesource.EncodeSource(draft.Observation.Source)
		if err == nil {
			_, err = observesource.DecodeSource(source)
		}
		if err != nil {
			problems = append(problems, FieldProblem{"observation.source", err.Error()})
		}
		window := draft.Observation.Window
		if window.Schema == "" {
			window.Schema = observewindow.WindowSchema
		}
		windowData, windowErr := observewindow.EncodeWindow(window)
		if windowErr == nil {
			_, windowErr = observewindow.DecodeWindow(windowData)
		}
		if windowErr != nil {
			problems = append(problems, FieldProblem{"observation.window", windowErr.Error()})
		}
		if err == nil && windowErr == nil && draft.Observation.Source.Observes != window.Source {
			problems = append(problems, FieldProblem{"observation.window.source", operation.ErrObservationPairMismatch.Error()})
		}
		if len(problems) == 0 {
			normalized.Observation = &ObservationDraft{Source: draft.Observation.Source, Window: window}
			staged = []catalog.Staged{{Role: "source", File: "source.json", Data: source}, {Role: "window", File: "window.json", Data: windowData}}
		}
	default:
		return nil, nil, append(problems, FieldProblem{"kind", "this release saves environments, tests and observations whole"})
	}
	if len(problems) > 0 {
		return nil, nil, problems
	}
	return staged, &normalized, problems
}

// declaredTarget is a target configuration as the shared writer writes it:
// declared as a test endpoint, validated by the shared reader before a byte
// is kept, and encoded deterministically. References it declares stay as
// declared; they are resolved from the project folder when the saved file is
// read in place.
func declaredTarget(target replay.Target) (replay.Target, []byte, error) {
	target.TestEndpoint = true
	data, err := json.Marshal(target, json.Deterministic(true))
	if err != nil {
		return replay.Target{}, nil, errors.New("the target configuration cannot be encoded")
	}
	data = append(data, '\n')
	folder, err := os.MkdirTemp("", "readmit-validate-")
	if err != nil {
		return replay.Target{}, nil, errors.New("the target configuration cannot be validated")
	}
	defer os.RemoveAll(folder)
	path := filepath.Join(folder, "target.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return replay.Target{}, nil, errors.New("the target configuration cannot be validated")
	}
	declared, err := replay.ReadDeclaredTarget(path)
	if err != nil {
		return replay.Target{}, nil, err
	}
	return declared, data, nil
}

// verifierFor reads a staged revision of kind through the readers every
// other panel and the command line read the same documents with, in place.
func verifierFor(kind ItemKind) catalog.Verifier {
	return func(files map[string]string) error {
		switch kind {
		case EnvironmentItem:
			_, err := operation.ReadTarget(files["target"])
			return err
		case TestItem:
			_, err := testrunner.ReadSpec(files["test"])
			return err
		case ObservationItem:
			_, _, err := operation.ValidateObservationPair(files["source"], files["window"])
			return err
		}
		return errors.New("this release does not save this kind of object")
	}
}
