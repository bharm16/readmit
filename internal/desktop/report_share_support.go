package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/sharing"
)

// A support summary of a report (#560) goes through the same framework as a
// share: the actual value-free summary the share operation prepares from the
// report's retained packet under the project's sharing policy, previewed
// whole, then exported once under a review that binds its exact identity.
// Exporting writes a new local folder; a summary for the customer hub is
// written into the project, where the team's authenticated request and
// approval (#562) take it. Saving the policy approves nothing.

// ExportSupportAction exports one report's support summary.
const ExportSupportAction ActionID = "report.support"

func init() {
	actionPolicies[ExportSupportAction] = actionPolicy{consent: ExportConsent, review: slot{}, perform: slot{profile: "PublishSupportSummary"},
		bind: bindReportSupport, execute: executeReportSupport}
}

// projectSharingPolicy is the project's sharing policy entry.
const projectSharingPolicy = "sharing-policy.json"

// ReportSupportOptions say where a support summary goes: a place chosen for
// a local export, or the project, for the team's review.
type ReportSupportOptions struct {
	Destination string `json:"destination,omitzero"`
	Hub         bool   `json:"hub,omitzero"`
}

// ReportSupportReview is one support summary as its preview shows it.
type ReportSupportReview struct {
	Report      string               `json:"report"`
	Policy      *SupportPolicy       `json:"policy,omitzero"`
	Summary     *SupportSummary      `json:"summary,omitzero"`
	Size        int                  `json:"size"`
	Destination ShareDestinationView `json:"destination"`
	// HubAllowed says the policy lets a summary go to the customer hub and a
	// team is signed in.
	HubAllowed bool `json:"hub_allowed"`
}

// ReportSupportOutcome is what an export wrote.
type ReportSupportOutcome struct {
	Name   string `json:"name"`
	Output string `json:"output,omitzero"`
	Hub    bool   `json:"hub,omitzero"`
}

type supportBinding struct {
	candidate *sharing.Candidate
	path      string
	hub       bool
}

// sharingPolicyEntry is the project's sharing policy: its own, else the
// first the project holds.
func sharingPolicyEntry(root string) string {
	if kind, ok := classify(root, projectSharingPolicy, false); ok && kind == SharingPolicyArtifact {
		return projectSharingPolicy
	}
	names, _ := projectEntries(root, MaxWorkspaceEntries, func(name string) bool {
		kind, ok := classify(root, name, false)
		return ok && kind == SharingPolicyArtifact
	})
	slices.Sort(names)
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

func bindReportSupport(a *App, ctx context.Context, request PrepareActionRequest, _ bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != ReportItem {
		return nil, refusal{Failed, "a support summary is prepared for one report"}
	}
	options := ReportSupportOptions{}
	if request.Support != nil {
		options = *request.Support
	}
	loaded, items, records, declined := a.scoped(ctx, request.Context, request.Items)
	if loaded == nil {
		return nil, declined
	}
	backing, err := loaded.reportBacking(records[0], "")
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	display := &ReportSupportReview{Report: backing.authored.Title}
	review := ActionReview{Items: items, Ready: true, ReportSupport: display}
	notReady := func(reason string) {
		if review.Ready {
			review.Ready, review.Refusal = false, reason
		}
	}
	entry := sharingPolicyEntry(loaded.root)
	if entry == "" {
		notReady("set the sharing policy first")
		return &boundAction{action: ExportSupportAction, origin: request, review: review, binding: binding(string(ExportSupportAction), loaded.root)}, noRefusal
	}
	data, declined := workspaceDocument(loaded.root, entry, sharingPolicyLimit, "the sharing policy")
	if declined.state != "" {
		return nil, declined
	}
	policy, err := sharing.DecodePolicy(data)
	if err != nil {
		return nil, refusal{Failed, "the project's sharing policy cannot be read"}
	}
	display.Policy = &SupportPolicy{Entry: entry, Schema: policy.Schema, Support: policy.Support, Destinations: policy.Destinations, MaxBytes: policy.MaxBytes}
	parts := []string{string(ExportSupportAction), loaded.root, loaded.document.Project.ID, a.reviewer(), records[0].ID, backing.revision, backing.packet.Identity, digestOf(data)}
	_, _, signedIn := a.hub.SignedIn()
	display.HubAllowed = signedIn == nil && slices.Contains(policy.Destinations, "customer-hub-download")
	if !policy.Support {
		notReady("the sharing policy denies support preparation")
		return &boundAction{action: ExportSupportAction, origin: request, review: review, binding: binding(parts...)}, noRefusal
	}
	candidate, err := sharing.Prepare(ctx, sharing.Request{Source: backing.packetDir, Kind: "retained-packet", Policy: filepath.Join(loaded.root, entry)})
	if err != nil {
		notReady("the summary exceeds the policy's size limit, or the report's evidence does not verify")
		return &boundAction{action: ExportSupportAction, origin: request, review: review, binding: binding(parts...)}, noRefusal
	}
	display.Summary, display.Size = supportSummaryView(candidate, policy), len(candidate.Bytes())
	bound := &supportBinding{candidate: candidate, hub: options.Hub}
	parts = append(parts, candidate.Identity())
	switch {
	case !slices.Contains(policy.Destinations, "local-file"):
		notReady("the sharing policy allows no local summary")
	case options.Hub:
		if !display.HubAllowed {
			notReady("the sharing policy or the team does not allow a summary for the customer hub")
		}
		destination, refused := destinationFor(loaded.root, "", "support")
		if refused.state != "" {
			return nil, refused
		}
		bound.path = filepath.Join(loaded.root, destination.Name)
		display.Destination = ShareDestinationView{Kind: "hub", Name: "Support summary"}
		parts = append(parts, "hub", destination.Name)
	default:
		place, known := a.shareDestination(options.Destination)
		path := place.path
		if options.Destination == "" || !known || !place.folder {
			notReady("choose where the summary is exported")
			break
		}
		if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			notReady("something is already there; choose a new name")
		}
		bound.path = path
		display.Destination = ShareDestinationView{Kind: "local", Name: filepath.Base(path), Location: filepath.Base(filepath.Dir(path))}
		parts = append(parts, path)
	}
	return &boundAction{action: ExportSupportAction, origin: request, support: bound, binding: binding(parts...), review: review}, noRefusal
}

func executeReportSupport(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	result := ReviewedActionResult{Outcome: ActionRefused}
	support := bound.support
	destination, err := artifactpath.Destination(support.path)
	if err != nil {
		result.refuse(Failed, "the summary must be exported outside retained evidence")
		return result
	}
	if err := support.candidate.Publish(ctx, support.candidate.Identity(), destination); err != nil {
		if ctx.Err() != nil {
			result.refuse(Cancelled, "the export was cancelled; an incomplete folder has no completion marker and is never read as a summary")
			result.Outcome = ActionCancelled
			return result
		}
		result.refuse(Failed, "the summary was not exported; an incomplete folder has no completion marker, and a retry needs a new place")
		return result
	}
	result.State, result.Outcome = Completed, ActionCompleted
	result.Support = &ReportSupportOutcome{Name: filepath.Base(destination), Output: a.rememberShareOutput(destination), Hub: support.hub}
	return result
}

// SharingPolicyRequest sets the project's sharing policy: whether support
// summaries may be prepared, where they may go, and their size limit.
type SharingPolicyRequest struct {
	Context      RequestContext `json:"context"`
	Support      bool           `json:"support"`
	Destinations []string       `json:"destinations"`
	MaxBytes     int            `json:"max_bytes"`
}

// SaveProjectSharingPolicy writes the project's sharing policy through the
// sharing contract's own reader, replacing the one the project held. Saving
// it approves no summary.
func (a *App) SaveProjectSharingPolicy(request SharingPolicyRequest) SupportPolicyResult {
	return run(a, false, true, func(ctx context.Context) SupportPolicyResult {
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			return SupportPolicyResult{State: declined.state, Reason: declined.reason}
		}
		policy := sharing.Policy{Schema: sharing.PolicySchema, Support: request.Support, Destinations: request.Destinations, MaxBytes: request.MaxBytes}
		if policy.Destinations == nil {
			policy.Destinations = []string{}
		}
		data, err := json.Marshal(policy, json.Deterministic(true))
		if err != nil {
			return SupportPolicyResult{State: Failed, Reason: "the sharing policy could not be written"}
		}
		data = append(data, '\n')
		if _, err := sharing.DecodePolicy(data); err != nil {
			return SupportPolicyResult{State: Failed, Reason: sharingRefusalReason(request.Support, request.Destinations, request.MaxBytes)}
		}
		path := filepath.Join(root, projectSharingPolicy)
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			err = writeWorkspaceEntry(root, projectSharingPolicy, data)
			if err != nil {
				return SupportPolicyResult{State: redactWriteState(err), Reason: redactWriteReason(err)}
			}
		} else if kind, ok := classify(root, projectSharingPolicy, false); !ok || kind != SharingPolicyArtifact {
			return SupportPolicyResult{State: Failed, Reason: "the project holds another file under the sharing policy's name"}
		} else if err := shellDocument.Replace(path, data); err != nil {
			return SupportPolicyResult{State: Failed, Reason: "the sharing policy could not be replaced; it is left as it was"}
		}
		return SupportPolicyResult{State: Completed, Policy: &SupportPolicy{Entry: projectSharingPolicy, Schema: policy.Schema, Support: policy.Support,
			Destinations: policy.Destinations, MaxBytes: policy.MaxBytes}}
	})
}
