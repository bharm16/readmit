package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"

	"github.com/bharm16/readmit/internal/redact"
)

// RedactPolicyRequest and RedactInventoryRequest carry typed contract members,
// never an unchecked JSON document or inferred evidence values.
type RedactPolicyRequest struct {
	Workspace string        `json:"workspace"`
	Output    string        `json:"output"`
	Policy    redact.Policy `json:"policy"`
}

type RedactInventoryRequest struct {
	Workspace string           `json:"workspace"`
	Output    string           `json:"output"`
	Inventory redact.Inventory `json:"inventory"`
}

type RedactPolicyResult struct {
	State  State          `json:"state"`
	Reason string         `json:"reason,omitzero"`
	Entry  string         `json:"entry,omitzero"`
	Policy *redact.Policy `json:"policy,omitzero"`
}

func (r *RedactPolicyResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

type RedactInventoryResult struct {
	State     State             `json:"state"`
	Reason    string            `json:"reason,omitzero"`
	Entry     string            `json:"entry,omitzero"`
	Inventory *redact.Inventory `json:"inventory,omitzero"`
}

func (r *RedactInventoryResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SaveRedactPolicy writes a new canonical policy only after the same decoder
// used by readmit redact accepts its exact bytes. Existing entries are never
// replaced: reopening for editing means saving a new reviewed document.
func (a *App) SaveRedactPolicy(request RedactPolicyRequest) RedactPolicyResult {
	return run(a, false, true, func(context.Context) RedactPolicyResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return RedactPolicyResult{State: declined.state, Reason: declined.reason}
		}
		if request.Policy.Schema != "" && request.Policy.Schema != redact.PolicySchema {
			return RedactPolicyResult{State: Failed, Reason: "unsupported disclosure policy contract"}
		}
		request.Policy.Schema = redact.PolicySchema
		raw, err := canonicalRedactDocument(request.Policy)
		if err != nil {
			return RedactPolicyResult{State: Failed, Reason: "the disclosure policy could not be canonicalized"}
		}
		policy, err := redact.DecodePolicy(raw)
		if err != nil {
			return RedactPolicyResult{State: Failed, Reason: err.Error()}
		}
		if err := writeWorkspaceEntry(root, request.Output, raw); err != nil {
			return RedactPolicyResult{State: redactWriteState(err), Reason: redactWriteReason(err)}
		}
		return RedactPolicyResult{State: Completed, Entry: request.Output, Policy: &policy}
	})
}

func (a *App) ReadRedactPolicy(workspace, entry string) RedactPolicyResult {
	return run(a, false, false, func(context.Context) RedactPolicyResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return RedactPolicyResult{State: declined.state, Reason: declined.reason}
		}
		raw, declined := workspaceDocument(root, entry, privacyDocumentLimit, "the disclosure policy")
		if declined.state != "" {
			return RedactPolicyResult{State: declined.state, Reason: declined.reason}
		}
		policy, err := redact.DecodePolicy(raw)
		if err != nil {
			return RedactPolicyResult{State: Failed, Reason: err.Error()}
		}
		return RedactPolicyResult{State: Completed, Entry: entry, Policy: &policy}
	})
}

func (a *App) SaveRedactInventory(request RedactInventoryRequest) RedactInventoryResult {
	return run(a, false, true, func(context.Context) RedactInventoryResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return RedactInventoryResult{State: declined.state, Reason: declined.reason}
		}
		if request.Inventory.Schema != "" && request.Inventory.Schema != redact.InventorySchema {
			return RedactInventoryResult{State: Failed, Reason: "unsupported original-artifact inventory contract"}
		}
		request.Inventory.Schema = redact.InventorySchema
		raw, err := canonicalRedactDocument(request.Inventory)
		if err != nil {
			return RedactInventoryResult{State: Failed, Reason: "the original-artifact inventory could not be canonicalized"}
		}
		inventory, err := redact.DecodeInventory(raw)
		if err != nil {
			return RedactInventoryResult{State: Failed, Reason: err.Error()}
		}
		// Create has always checked each artifact kind and path later, while
		// resolving it against the original case. Authoring refuses an invalid
		// declaration before writing without changing that CLI reader's stage.
		for _, artifact := range inventory.Artifacts {
			switch artifact.Kind {
			case "run", "result", "diagnosis-json", "diagnosis-markdown":
			default:
				return RedactInventoryResult{State: Failed, Reason: "unsupported inventory artifact kind; no artifact was silently omitted"}
			}
			if artifact.Path == "" {
				return RedactInventoryResult{State: Failed, Reason: "inventory artifact requires a path"}
			}
		}
		if err := writeWorkspaceEntry(root, request.Output, raw); err != nil {
			return RedactInventoryResult{State: redactWriteState(err), Reason: redactWriteReason(err)}
		}
		return RedactInventoryResult{State: Completed, Entry: request.Output, Inventory: &inventory}
	})
}

func (a *App) ReadRedactInventory(workspace, entry string) RedactInventoryResult {
	return run(a, false, false, func(context.Context) RedactInventoryResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return RedactInventoryResult{State: declined.state, Reason: declined.reason}
		}
		raw, declined := workspaceDocument(root, entry, privacyDocumentLimit, "the original-artifact inventory")
		if declined.state != "" {
			return RedactInventoryResult{State: declined.state, Reason: declined.reason}
		}
		inventory, err := redact.DecodeInventory(raw)
		if err != nil {
			return RedactInventoryResult{State: Failed, Reason: err.Error()}
		}
		return RedactInventoryResult{State: Completed, Entry: entry, Inventory: &inventory}
	})
}

func canonicalRedactDocument(value any) ([]byte, error) {
	raw, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func redactWriteState(err error) State {
	if errors.Is(err, fs.ErrPermission) {
		return PermissionDenied
	}
	return Failed
}

func redactWriteReason(err error) string {
	if errors.Is(err, fs.ErrPermission) {
		return "this account cannot write into the open workspace"
	}
	return err.Error()
}
