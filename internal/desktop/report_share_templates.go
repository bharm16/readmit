package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/reportshare"
	"github.com/bharm16/readmit/internal/runresult"
)

// A disclosure template is a named readmit-redact-policy/v1 document of the
// project: the file is named after the template, and the contract is the
// one `readmit redact` reads, unchanged. Saving a template saves its
// configuration only; it approves no share. A template is edited as a whole
// document and replaced only when it is still the version that was read.

// ShareTemplateRequest saves a template: from a share draft (Report, Base
// and Overrides), or as a whole document (Policy). Entry names the template
// an edit replaces, with Digest the version the edit was made from; a new
// template has none.
type ShareTemplateRequest struct {
	Context   RequestContext        `json:"context"`
	Name      string                `json:"name,omitzero"`
	Report    *ItemRef              `json:"report,omitzero"`
	Base      string                `json:"base,omitzero"`
	Overrides reportshare.Overrides `json:"overrides"`
	Policy    *redact.Policy        `json:"policy,omitzero"`
	Entry     string                `json:"entry,omitzero"`
	Digest    string                `json:"digest,omitzero"`
}

// ShareTemplateResult is one template as it now stands.
type ShareTemplateResult struct {
	State    State          `json:"state"`
	Reason   string         `json:"reason,omitzero"`
	Context  RequestContext `json:"context"`
	Template *ShareTemplate `json:"template,omitzero"`
	Policy   *redact.Policy `json:"policy,omitzero"`
	Digest   string         `json:"digest,omitzero"`
}

func (r *ShareTemplateResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ShareTemplatesResult lists the project's templates by name.
type ShareTemplatesResult struct {
	State     State           `json:"state"`
	Reason    string          `json:"reason,omitzero"`
	Context   RequestContext  `json:"context"`
	Templates []ShareTemplate `json:"templates"`
}

func (r *ShareTemplatesResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ListShareTemplates lists the project's disclosure templates.
func (a *App) ListShareTemplates(request RequestContext) ShareTemplatesResult {
	return runRead(a, false, func(ctx context.Context) ShareTemplatesResult {
		result := ShareTemplatesResult{Context: request, Templates: []ShareTemplate{}}
		root, declined := a.projectRoot(ctx, request)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		result.Templates, result.State = shareTemplates(root), Completed
		if len(result.Templates) == 0 {
			result.State = Empty
		}
		return result
	})
}

// ReadShareTemplate reads one template through the reader `readmit redact`
// uses, with the version an edit of it is made from.
func (a *App) ReadShareTemplate(request ItemRequest) ShareTemplateResult {
	return runRead(a, false, func(ctx context.Context) ShareTemplateResult {
		result := ShareTemplateResult{Context: request.Context}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		data, declined := workspaceDocument(root, request.Ref.ID, privacyDocumentLimit, "the template")
		if declined.state != "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		policy, err := redact.DecodePolicy(data)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		result.State, result.Policy, result.Digest = Completed, &policy, digestOf(data)
		result.Template = &ShareTemplate{Entry: request.Ref.ID, Name: templateName(request.Ref.ID)}
		return result
	})
}

// SaveShareTemplate saves a new template, or replaces the one an edit was
// made from while it is still that version.
func (a *App) SaveShareTemplate(request ShareTemplateRequest) ShareTemplateResult {
	return run(a, false, true, func(ctx context.Context) ShareTemplateResult {
		result := ShareTemplateResult{Context: request.Context}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		policy, reason := a.templatePolicy(ctx, loaded, request)
		if policy == nil {
			result.refuse(Failed, reason)
			return result
		}
		data, err := canonicalRedactDocument(policy)
		if err != nil {
			result.refuse(Failed, "the template could not be written as canonical bytes")
			return result
		}
		if _, err := redact.DecodePolicy(data); err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		entry := request.Entry
		if entry == "" {
			name := strings.TrimSpace(request.Name)
			if name == "" || len(name) > 80 || strings.IndexFunc(name, func(r rune) bool { return unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) }) >= 0 ||
				strings.HasPrefix(name, ".") {
				result.refuse(Failed, "name the template with up to 80 letters, digits, spaces and punctuation other than / \\ : * ? \" < > |")
				return result
			}
			entry = name + ".json"
			if slices.ContainsFunc(shareTemplates(loaded.root), func(held ShareTemplate) bool { return strings.EqualFold(held.Name, name) }) {
				result.refuse(Failed, "a template with this name exists; choose another name")
				return result
			}
			if err := writeWorkspaceEntry(loaded.root, entry, data); err != nil {
				result.refuse(redactWriteState(err), redactWriteReason(err))
				return result
			}
		} else {
			held, declined := workspaceDocument(loaded.root, entry, privacyDocumentLimit, "the template")
			if declined.state != "" {
				result.refuse(declined.state, declined.reason)
				return result
			}
			if kind, ok := classify(loaded.root, entry, false); !ok || kind != RedactPolicyArtifact {
				result.refuse(Failed, "that entry is not a template")
				return result
			}
			if digestOf(held) != request.Digest {
				result.refuse(Failed, "the template changed since it was opened; open it again")
				return result
			}
			path, err := artifactpath.File(loaded.root, entry)
			if err != nil || shellDocument.Replace(path, data) != nil {
				result.refuse(Failed, "the template could not be replaced; it is left as it was")
				return result
			}
		}
		result.State, result.Policy, result.Digest = Completed, policy, digestOf(data)
		result.Template = &ShareTemplate{Entry: entry, Name: templateName(entry)}
		return result
	})
}

// templatePolicy is the whole template a request saves: its document, or
// the draft's template with its treatments applied. A template records the
// original failures a check must reproduce: the base template's, else the
// report's actual failed checks.
func (a *App) templatePolicy(ctx context.Context, loaded *loadedCatalog, request ShareTemplateRequest) (*redact.Policy, string) {
	if request.Policy != nil {
		policy := *request.Policy
		policy.Schema = redact.PolicySchema
		return &policy, ""
	}
	if request.Report == nil {
		return nil, "a template is saved from a share or as a whole template"
	}
	var base *redact.Policy
	if request.Base != "" {
		data, declined := workspaceDocument(loaded.root, request.Base, privacyDocumentLimit, "the template")
		if declined.state != "" {
			return nil, declined.reason
		}
		policy, err := redact.DecodePolicy(data)
		if err != nil {
			return nil, err.Error()
		}
		base = &policy
	}
	policy, err := reportshare.EffectivePolicy(base, request.Overrides)
	if err != nil {
		return nil, err.Error()
	}
	if policy == nil {
		return nil, "a template holds at least one treatment"
	}
	if len(policy.RequiredFailures) == 0 {
		index := loaded.document.Find(request.Report.ID)
		if index < 0 {
			return nil, "the project holds no such report"
		}
		backing, err := loaded.reportBacking(loaded.document.Items[index], "")
		if err != nil {
			return nil, err.Error()
		}
		failures, err := failedChecks(backing)
		if err != nil || len(failures) == 0 {
			return nil, "a template records the original failures a check reproduces, and this report's run has none; add them in Manage templates"
		}
		policy.RequiredFailures = failures
	}
	if policy.Patient.Authority == nil {
		policy.Patient.Authority = []string{}
	}
	return policy, ""
}

// failedChecks are the positions of the report's current run's failed
// checks.
func failedChecks(backing reportBacking) ([]int, error) {
	current, err := runresult.Open(filepath.Join(backing.packetDir, "current"))
	if err != nil || current.Artifact == nil {
		return nil, errors.New("the report's run cannot be read")
	}
	failed := []int{}
	for i, result := range current.Artifact.Result.Assertions {
		if result.Status == "failed" {
			failed = append(failed, i+1)
		}
	}
	return failed, nil
}
