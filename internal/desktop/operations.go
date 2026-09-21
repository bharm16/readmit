package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/operationguard"
)

const operationSelectionSchema = "readmit-desktop-operation-selection/v1"

type operationSelection struct {
	Schema string `json:"schema"`
	Policy string `json:"policy"`
}

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

// NewWithOperationSelection restores only an explicit prior policy selection.
// Missing or invalid selection keeps reads available and paid work refused.
func NewWithOperationSelection(chooser FolderChooser, recent, filters, session, drafts, selection string) *App {
	a := New(chooser, recent, filters, session, drafts)
	a.operationSelectionPath = selection
	data, err := readOperationFile(selection)
	if err == nil {
		var selected operationSelection
		if json.Unmarshal(data, &selected, json.RejectUnknownMembers(true)) == nil && selected.Schema == operationSelectionSchema && filepath.IsAbs(selected.Policy) {
			a.operationPolicy = selected.Policy
			a.operationGuard = operationguard.New(selected.Policy)
		}
	}
	return a
}
func readOperationFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, operationguard.ErrUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, operationguard.ErrUnavailable
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return nil, operationguard.ErrUnavailable
	}
	return data, nil
}
func (a *App) selectedOperation() (*operationguard.Guard, string) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	return a.operationGuard, a.operationPolicy
}
func (a *App) admitAuthor() error {
	g, _ := a.selectedOperation()
	_, err := g.Admit("author")
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
		return a.SelectOperationPolicy(filepath.Join(folder, "operation-policy.json"))
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
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	if a.operationSelectionPath != "" {
		encoded, err := json.Marshal(operationSelection{Schema: operationSelectionSchema, Policy: path})
		if err != nil || writeShellDocument(a.operationSelectionPath, append(encoded, '\n')) != nil {
			return OperationResult{State: Failed, Reason: "cannot retain operation policy selection"}
		}
	}
	a.operationPolicy = path
	a.operationGuard = operationguard.New(path)
	return OperationResult{State: Completed, Selected: true}
}
func (a *App) OperationStatus() OperationResult {
	_, path := a.selectedOperation()
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
func (a *App) ReleaseOperations() OperationResult { return a.changeOperation(operationguard.Release) }

// DefaultOperationSelectionPath identifies only viewer configuration. Policy,
// clock and admission files remain where the operator explicitly selected them.
func DefaultOperationSelectionPath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", errors.New("cannot locate operation selection directory")
	}
	return filepath.Join(root, "readmit", "operations.json"), nil
}
