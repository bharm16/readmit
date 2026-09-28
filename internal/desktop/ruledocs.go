package desktop

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"

	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/findingreview"
)

// This file is the authoring seam for the five declared documents the window
// offers editors for: correlation rules, a sequence analysis, a normalization
// policy, a diagnosis configuration and finding decisions. Every open reads
// one entry of the open workspace through the same strict parser the engines
// apply, and every save validates through that parser first, canonicalizes
// deterministically, and writes one new entry — editing means saving a new
// revision, exactly as a profile is saved, so nothing authored here ever
// rewrites a document another result was derived from.

// RuleDocumentSaveRequest saves one canonical authored document into one new
// entry of the open workspace. Document is the JSON text the editor holds.
type RuleDocumentSaveRequest struct {
	Workspace string `json:"workspace"`
	Document  string `json:"document"`
	Output    string `json:"output"`
}

// ruleDocument is one authored contract as this file works with it: the noun
// phrase its refusals name, the byte bound its own reader enforces, and that
// reader. The exported surface stays typed per contract, because the
// bindings, the ledger and the shell's kind vocabulary are all per-contract;
// this only keeps the read/canonicalize/write skeleton in one place.
type ruleDocument[T any] struct {
	what  string
	limit int
	parse func([]byte) (T, error)
}

var (
	normalizationPolicyDocument = ruleDocument[diff.Policy]{"the normalization policy", diff.MaxPolicyBytes, diff.DecodePolicy}
	diagnoseConfigDocument      = ruleDocument[diagnose.Config]{"the diagnosis configuration", diagnose.MaxConfigBytes, diagnose.ParseConfig}
	findingDecisionsDocument    = ruleDocument[findingreview.Decisions]{"the finding decisions document", findingreview.MaxDecisionsBytes, findingreview.ParseDecisions}
)

// canonicalDocument is the one deterministic form a document authored here is
// displayed and stored in, so two saves of one meaning are one byte sequence.
func canonicalDocument(value any) ([]byte, error) {
	data, err := json.Marshal(value, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// opened reads one declared document of the open workspace. The digest is of
// the bytes as they sit in the workspace, because that is what a sequence or
// a review binds to; the document returned is the canonical form an editor
// holds.
func (d ruleDocument[T]) opened(workspace, entry string) (string, string, *T, refusal) {
	root, declined := resolveFolder(workspace)
	if root == "" {
		return "", "", nil, declined
	}
	data, declined := workspaceDocument(root, entry, d.limit, d.what)
	if data == nil {
		return "", "", nil, declined
	}
	parsed, err := d.parse(data)
	if err != nil {
		// The reader's own diagnostic names the declaration at fault and never
		// repeats a value, so it is reported as it is.
		return "", "", nil, refusal{Failed, err.Error()}
	}
	canonical, err := canonicalDocument(parsed)
	if err != nil {
		return "", "", nil, refusal{Failed, d.what + " could not be canonicalized"}
	}
	return string(canonical), digestOf(data), &parsed, refusal{}
}

// saved validates the editor's text through the contract's own reader and
// writes the canonical form into one new entry. An entry that already exists
// is refused rather than replaced: a document a result was derived from is
// never edited in place, and the remedy is a new revision under a new name.
func (d ruleDocument[T]) saved(request RuleDocumentSaveRequest) (string, string, *T, refusal) {
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return "", "", nil, declined
	}
	parsed, err := d.parse([]byte(request.Document))
	if err != nil {
		return "", "", nil, refusal{Failed, err.Error()}
	}
	canonical, err := canonicalDocument(parsed)
	if err != nil {
		return "", "", nil, refusal{Failed, d.what + " could not be canonicalized"}
	}
	if err := writeWorkspaceEntry(root, request.Output, canonical); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return "", "", nil, refusal{PermissionDenied, "this account cannot write into the open workspace"}
		}
		return "", "", nil, refusal{Failed, err.Error()}
	}
	return string(canonical), digestOf(canonical), &parsed, refusal{}
}

// NormalizationPolicyResult carries one authored normalization policy. SHA256
// is the digest of the exact bytes the entry holds, so a normalization
// preview can state which policy revision it ran under.
type NormalizationPolicyResult struct {
	State    State        `json:"state"`
	Reason   string       `json:"reason,omitzero"`
	Document string       `json:"document,omitzero"`
	Output   string       `json:"output,omitzero"`
	SHA256   string       `json:"sha256,omitzero"`
	Policy   *diff.Policy `json:"policy,omitzero"`
}

func (r *NormalizationPolicyResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// OpenNormalizationPolicy reads one normalization policy of the open
// workspace through the same strict reader `readmit normalize` applies.
func (a *App) OpenNormalizationPolicy(workspace, entry string) NormalizationPolicyResult {
	return run(a, false, false, func(context.Context) NormalizationPolicyResult {
		document, sha, policy, declined := normalizationPolicyDocument.opened(workspace, entry)
		if policy == nil {
			return NormalizationPolicyResult{State: declined.state, Reason: declined.reason}
		}
		return NormalizationPolicyResult{State: Completed, Document: document, SHA256: sha, Policy: policy}
	})
}

// SaveNormalizationPolicy validates the editor's text and writes one
// canonical normalization policy into one new entry of the open workspace.
func (a *App) SaveNormalizationPolicy(request RuleDocumentSaveRequest) NormalizationPolicyResult {
	return run(a, false, true, func(context.Context) NormalizationPolicyResult {
		document, sha, policy, declined := normalizationPolicyDocument.saved(request)
		if policy == nil {
			return NormalizationPolicyResult{State: declined.state, Reason: declined.reason}
		}
		return NormalizationPolicyResult{State: Completed, Document: document, Output: request.Output, SHA256: sha, Policy: policy}
	})
}

// DiagnoseConfigResult carries one authored diagnosis configuration. SHA256
// is the digest of the exact bytes the entry holds, which names the file an
// editor opened or saved. It is not the identity a report records: a report's
// config_sha256 is computed by the engine over the configuration it ran.
type DiagnoseConfigResult struct {
	State    State            `json:"state"`
	Reason   string           `json:"reason,omitzero"`
	Document string           `json:"document,omitzero"`
	Output   string           `json:"output,omitzero"`
	SHA256   string           `json:"sha256,omitzero"`
	Config   *diagnose.Config `json:"config,omitzero"`
}

func (r *DiagnoseConfigResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// OpenDiagnoseConfig reads one diagnosis configuration of the open workspace
// through the same strict parser `readmit diagnose --config` applies.
func (a *App) OpenDiagnoseConfig(workspace, entry string) DiagnoseConfigResult {
	return run(a, false, false, func(context.Context) DiagnoseConfigResult {
		document, sha, config, declined := diagnoseConfigDocument.opened(workspace, entry)
		if config == nil {
			return DiagnoseConfigResult{State: declined.state, Reason: declined.reason}
		}
		return DiagnoseConfigResult{State: Completed, Document: document, SHA256: sha, Config: config}
	})
}

// SaveDiagnoseConfig validates the editor's text and writes one canonical
// diagnosis configuration into one new entry of the open workspace.
func (a *App) SaveDiagnoseConfig(request RuleDocumentSaveRequest) DiagnoseConfigResult {
	return run(a, false, true, func(context.Context) DiagnoseConfigResult {
		document, sha, config, declined := diagnoseConfigDocument.saved(request)
		if config == nil {
			return DiagnoseConfigResult{State: declined.state, Reason: declined.reason}
		}
		return DiagnoseConfigResult{State: Completed, Document: document, Output: request.Output, SHA256: sha, Config: config}
	})
}

// FindingDecisionsResult carries one authored finding-decisions document.
// SHA256 is the digest of the exact bytes the entry holds, which is the
// identity a finding review records the decisions under.
type FindingDecisionsResult struct {
	State     State                    `json:"state"`
	Reason    string                   `json:"reason,omitzero"`
	Document  string                   `json:"document,omitzero"`
	Output    string                   `json:"output,omitzero"`
	SHA256    string                   `json:"sha256,omitzero"`
	Decisions *findingreview.Decisions `json:"decisions,omitzero"`
}

func (r *FindingDecisionsResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// OpenFindingDecisions reads one finding-decisions document of the open
// workspace through the same strict parser `readmit diagnose review` applies.
func (a *App) OpenFindingDecisions(workspace, entry string) FindingDecisionsResult {
	return run(a, false, false, func(context.Context) FindingDecisionsResult {
		document, sha, decisions, declined := findingDecisionsDocument.opened(workspace, entry)
		if decisions == nil {
			return FindingDecisionsResult{State: declined.state, Reason: declined.reason}
		}
		return FindingDecisionsResult{State: Completed, Document: document, SHA256: sha, Decisions: decisions}
	})
}

// SaveFindingDecisions validates the editor's text and writes one canonical
// finding-decisions document into one new entry of the open workspace.
func (a *App) SaveFindingDecisions(request RuleDocumentSaveRequest) FindingDecisionsResult {
	return run(a, false, true, func(context.Context) FindingDecisionsResult {
		document, sha, decisions, declined := findingDecisionsDocument.saved(request)
		if decisions == nil {
			return FindingDecisionsResult{State: declined.state, Reason: declined.reason}
		}
		return FindingDecisionsResult{State: Completed, Document: document, Output: request.Output, SHA256: sha, Decisions: decisions}
	})
}
