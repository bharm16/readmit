package desktop

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/operationguard"
)

// operationSelectionSchema is the versioned contract of the document that
// remembers the operation policy a person selected. Its one path member is
// named "policy", and the document itself is written once, by the shell
// document store's remembered selection.
const operationSelectionSchema = "readmit-desktop-operation-selection/v1"

// OperationResult exposes local clock state even after expiry. Reason is fixed
// diagnostic text; it never renders signed identities or arbitrary paths.
type OperationResult struct {
	State     State                 `json:"state"`
	Reason    string                `json:"reason,omitzero"`
	Clock     *operationguard.State `json:"clock,omitzero"`
	Selected  bool                  `json:"selected"`
	Term      string                `json:"term,omitzero"`
	Expires   string                `json:"expires,omitzero"`
	GraceEnds string                `json:"grace_ends,omitzero"`
	Authors   int                   `json:"author_seats"`
	Runners   int                   `json:"runner_instances"`
}

func (r *OperationResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// NewWithOperationSelection restores only an explicit prior policy selection,
// remembered in the shell document store with the commercial destinations and
// customer hub selections beside it. A missing selection keeps reads available
// and paid work refused. An unreadable selection is reported until the person
// explicitly chooses again. Restoring is a local read that contacts nothing.
func NewWithOperationSelection(chooser FolderChooser, documents ShellDocuments) *App {
	a := New(chooser, documents)
	a.selections = documents.selections()
	a.restoreSelections()
	return a
}

// restoreSelections reads the three remembered selections, three local files
// and nothing else. A selection that is not there was never made; one that is
// there but cannot be read is reported, never replaced, and stays visible as
// a refusal until the person chooses again.
func (a *App) restoreSelections() {
	path, err := a.selections.operation.recall()
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		a.operationRestoreRefusal = "the remembered operation selection cannot be read; choose an activation folder again"
	default:
		a.operationPolicy = path
		a.operationGuard = operationguard.New(path)
	}
	a.restoreCommercialSelection()
	a.restoreHubSelection()
}

// readOperationFile reads one operator-supplied document the way the guard
// reads its controls: bounded, regular and never through a link. The policy a
// person chose lives in their activation folder, not in the shell document
// store; this is how it is read, never how it is remembered.
func readOperationFile(path string) ([]byte, error) {
	return operationguard.ReadDocument(path)
}
func (a *App) selectedOperation() (*operationguard.Guard, string) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	return a.operationGuard, a.operationPolicy
}

// admitAuthor admits the author part way through local work that writes only
// once its request has been read. Work that always writes is admitted before
// it starts, as it takes the slot: run's writes, or its declared profile.
func (a *App) admitAuthor() error {
	g, _ := a.selectedOperation()
	_, err := g.AdmitContext(context.Background(), "author")
	return err
}

// ChooseOperationPolicy uses the native folder chooser for the operator's
// supplied operation-policy.json. Selection is not activation or trial issuance.
func (a *App) ChooseOperationPolicy() OperationResult {
	return run(a, true, false, func(ctx context.Context) OperationResult {
		folder, declined := a.chooseFolder(ctx, "Choose the license activation folder")
		if folder == "" {
			return OperationResult{State: declined.state, Reason: declined.reason}
		}
		return a.SelectOperationPolicy(operationguard.ActivationPolicyIn(folder))
	})
}

// SelectOperationPolicy persists only the explicit policy path, outside evidence.
func (a *App) SelectOperationPolicy(path string) OperationResult {
	if !filepath.IsAbs(path) {
		return OperationResult{State: Failed, Reason: "select an absolute operation policy path"}
	}
	data, err := readOperationFile(path)
	if err != nil {
		return OperationResult{State: Failed, Reason: operationguard.ErrUnavailable.Error()}
	}
	if _, err = operationguard.DecodePolicy(data); err != nil {
		return OperationResult{State: Failed, Reason: err.Error()}
	}
	if err := a.retainOperationPolicy(path); err != nil {
		return OperationResult{State: Failed, Reason: err.Error()}
	}
	return OperationResult{State: Completed, Selected: true}
}
func (a *App) OperationStatus() OperationResult {
	a.operationMu.Lock()
	path, refusal := a.operationPolicy, a.operationRestoreRefusal
	a.operationMu.Unlock()
	if refusal != "" {
		return OperationResult{State: Failed, Reason: refusal}
	}
	state, err := operationguard.Read(path)
	if err != nil {
		return OperationResult{State: Failed, Reason: err.Error(), Selected: path != ""}
	}
	result := OperationResult{State: Completed, Clock: &state, Selected: true}
	if term, err := operationguard.Describe(path); err == nil {
		result.Authors = term.Authors
		result.Runners = term.Runners
		result.Term = string(term.State)
		result.Expires = term.Expires.Format(time.RFC3339)
		result.GraceEnds = term.GraceEnds.Format(time.RFC3339)
	} else {
		result.Reason = err.Error()
	}
	return result
}
func (a *App) changeOperation(apply func(string) error) OperationResult {
	return run(a, false, false, func(context.Context) OperationResult {
		_, path := a.selectedOperation()
		if err := apply(path); err != nil {
			return OperationResult{State: Failed, Reason: err.Error(), Selected: path != ""}
		}
		return a.OperationStatus()
	})
}
func (a *App) ActivateOperations() OperationResult { return a.changeOperation(operationguard.Activate) }
func (a *App) ResolveOperationClock() OperationResult {
	return a.changeOperation(operationguard.Resolve)
}
func (a *App) ReleaseOperations() OperationResult {
	return a.changeOperation(func(path string) error {
		// This computer's license is deactivated whole, as `readmit license
		// release` does, so its store and its operation state agree.
		if a.installedSelected(path) {
			return operationguard.ReleaseInstalledLicense(a.licenseRoot, time.Now().UTC().Truncate(time.Second))
		}
		return operationguard.Release(path)
	})
}
