package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/hubclient"
	"github.com/bharm16/readmit/internal/hubprotocol"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/sharing"
)

// HubRevisionDraftSchema is the offline hub revision draft retained locally
// until the operator reconnects and posts an explicit lifecycle revision.
const HubRevisionDraftSchema = "readmit-hub-revision-draft/v1"

const hubRevisionDraftKind = "hub-revision"

// HubReviewCommandRequest is the authenticated collaboration decision the window posts.
type HubReviewCommandRequest struct {
	Project   string `json:"project"`
	ID        string `json:"id"`
	Expected  int    `json:"expected"`
	Kind      string `json:"kind"`
	Evidence  string `json:"evidence"`
	Parent    string `json:"parent"`
	Recipient string `json:"recipient"`
	Text      string `json:"text"`
	Release   string `json:"release"`
}

// HubReviewEventView is one collaboration event with its authoritative actor.
type HubReviewEventView struct {
	Schema    string `json:"schema"`
	Project   string `json:"project"`
	Sequence  int    `json:"sequence"`
	Issuer    string `json:"issuer"`
	Actor     string `json:"actor"`
	At        string `json:"at"`
	Kind      string `json:"kind"`
	Evidence  string `json:"evidence"`
	Parent    string `json:"parent,omitzero"`
	Recipient string `json:"recipient,omitzero"`
	Text      string `json:"text"`
	Release   string `json:"release,omitzero"`
	CommandID string `json:"command_id"`
}

// HubReviewsResult carries collaboration history or notifications.
type HubReviewsResult struct {
	State   State                `json:"state"`
	Reason  string               `json:"reason,omitzero"`
	Project string               `json:"project,omitzero"`
	Head    int                  `json:"head,omitzero"`
	Events  []HubReviewEventView `json:"events,omitzero"`
	Replay  bool                 `json:"replay,omitzero"`
	Warning string               `json:"warning,omitzero"`
}

func (r *HubReviewsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// HubReviewQueryRequest searches history or notifications.
type HubReviewQueryRequest struct {
	Project  string `json:"project"`
	After    int    `json:"after"`
	Text     string `json:"text"`
	Evidence string `json:"evidence"`
}

// HubLifecycleCommandRequest is one lifecycle command the window posts.
type HubLifecycleCommandRequest struct {
	Project  string   `json:"project"`
	ID       string   `json:"id"`
	Expected int      `json:"expected"`
	Kind     string   `json:"kind"`
	Resource string   `json:"resource"`
	Artifact string   `json:"artifact"`
	Parents  []string `json:"parents"`
	Subject  string   `json:"subject"`
	Until    string   `json:"until"`
	Reason   string   `json:"reason"`
}

// HubLifecycleEventView is one lifecycle event with its authoritative actor.
type HubLifecycleEventView struct {
	Schema     string   `json:"schema"`
	Project    string   `json:"project"`
	Sequence   int      `json:"sequence"`
	Issuer     string   `json:"issuer"`
	Actor      string   `json:"actor"`
	At         string   `json:"at"`
	ReviewHead int      `json:"review_head,omitzero"`
	Kind       string   `json:"kind"`
	Resource   string   `json:"resource,omitzero"`
	Artifact   string   `json:"artifact,omitzero"`
	Parents    []string `json:"parents,omitzero"`
	Subject    string   `json:"subject,omitzero"`
	Until      string   `json:"until,omitzero"`
	Reason     string   `json:"reason"`
	CommandID  string   `json:"command_id"`
}

// HubLifecycleResult carries lifecycle history, tips, or a write outcome.
type HubLifecycleResult struct {
	State   State                   `json:"state"`
	Reason  string                  `json:"reason,omitzero"`
	Project string                  `json:"project,omitzero"`
	Head    int                     `json:"head,omitzero"`
	Events  []HubLifecycleEventView `json:"events,omitzero"`
	Tips    map[string][]string     `json:"tips,omitzero"`
	Event   *HubLifecycleEventView  `json:"event,omitzero"`
	Audit   *HubAuditExportView     `json:"audit,omitzero"`
	Replay  bool                    `json:"replay,omitzero"`
	Warning string                  `json:"warning,omitzero"`
}

func (r *HubLifecycleResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// HubAuditExportView is the audit document returned by audit-export.
type HubAuditExportView struct {
	Schema     string                  `json:"schema"`
	Project    string                  `json:"project"`
	Lifecycle  []HubLifecycleEventView `json:"lifecycle"`
	ReviewHead int                     `json:"review_head"`
	Reviews    []HubReviewEventView    `json:"reviews"`
	Warning    string                  `json:"warning"`
}

// HubRevisionDraft is the local offline draft content for a hub revision branch.
type HubRevisionDraft struct {
	Schema       string   `json:"schema"`
	Project      string   `json:"project"`
	Resource     string   `json:"resource"`
	ParentTips   []string `json:"parent_tips"`
	LocalPath    string   `json:"local_path"`
	ExpectedHead int      `json:"expected_head"`
	Note         string   `json:"note"`
}

// HubOfflineDraftRequest retains or discards an offline hub revision draft.
type HubOfflineDraftRequest struct {
	Workspace    string   `json:"workspace"`
	DraftID      string   `json:"draft_id,omitzero"`
	Project      string   `json:"project"`
	Resource     string   `json:"resource"`
	ParentTips   []string `json:"parent_tips"`
	LocalPath    string   `json:"local_path"`
	ExpectedHead int      `json:"expected_head"`
	Note         string   `json:"note"`
}

// ListHubReviews reads collaboration history for a project. Actor identity is
// whatever the hub recorded from the authenticated session.
func (a *App) ListHubReviews(project string) HubReviewsResult {
	return runNamed[HubReviewsResult, *HubReviewsResult](a, profiles["ListHubReviews"], func(ctx context.Context) HubReviewsResult {
		client, errRes := a.requireHubSession()
		if errRes != nil {
			return *errRes
		}
		history, err := client.ListHistory(ctx, project)
		return mapReviewHistory(project, history, err)
	})
}

// SearchHubReviews searches collaboration history with an explicit query.
func (a *App) SearchHubReviews(request HubReviewQueryRequest) HubReviewsResult {
	return runNamed[HubReviewsResult, *HubReviewsResult](a, profiles["SearchHubReviews"], func(ctx context.Context) HubReviewsResult {
		client, errRes := a.requireHubSession()
		if errRes != nil {
			return *errRes
		}
		history, err := client.SearchHistory(ctx, request.Project, hubprotocol.ReviewQuery{
			Schema: hubprotocol.ReviewQuerySchema, After: request.After, Text: request.Text, Evidence: request.Evidence,
		})
		return mapReviewHistory(request.Project, history, err)
	})
}

// ListHubNotifications reads collaboration events addressed to the signed-in subject.
func (a *App) ListHubNotifications(project string) HubReviewsResult {
	return runNamed[HubReviewsResult, *HubReviewsResult](a, profiles["ListHubNotifications"], func(ctx context.Context) HubReviewsResult {
		client, errRes := a.requireHubSession()
		if errRes != nil {
			return *errRes
		}
		history, err := client.ListNotifications(ctx, project)
		return mapReviewHistory(project, history, err)
	})
}

// SearchHubNotifications searches the subject's notifications.
func (a *App) SearchHubNotifications(request HubReviewQueryRequest) HubReviewsResult {
	return runNamed[HubReviewsResult, *HubReviewsResult](a, profiles["SearchHubNotifications"], func(ctx context.Context) HubReviewsResult {
		client, errRes := a.requireHubSession()
		if errRes != nil {
			return *errRes
		}
		history, err := client.SearchNotifications(ctx, request.Project, hubprotocol.ReviewQuery{
			Schema: hubprotocol.ReviewQuerySchema, After: request.After, Text: request.Text, Evidence: request.Evidence,
		})
		return mapReviewHistory(request.Project, history, err)
	})
}

// PostHubReview records a collaboration decision. Identity comes from the
// authenticated hub session; a local reviewer text field cannot substitute.
func (a *App) PostHubReview(request HubReviewCommandRequest) HubReviewsResult {
	return runNamed[HubReviewsResult, *HubReviewsResult](a, profiles["PostHubReview"], func(ctx context.Context) HubReviewsResult {
		// An unpostable kind is refused before anything is sent; the kind
		// names the command version that carries it, and the hub remains the
		// authority for shape, roles and permission.
		schema, supported := hubprotocol.CommandSchema(request.Kind)
		if !supported {
			return HubReviewsResult{
				State: Failed, Project: request.Project,
				Reason: "unsupported review kind; the hub accepts comment, assignment, review-request, approval, support-policy, support-request and support-approval",
			}
		}
		client, errRes := a.requireHubSession()
		if errRes != nil {
			return *errRes
		}
		event, replay, err := client.PostReview(ctx, request.Project, hubprotocol.ReviewCommand{
			Schema: schema,
			ID:     request.ID, Expected: request.Expected, Kind: request.Kind,
			Evidence: request.Evidence, Parent: request.Parent, Recipient: request.Recipient,
			Text: request.Text, Release: request.Release,
		})
		if err != nil {
			return mapReviewError(request.Project, err)
		}
		return reviewWriteResult(request.Project, event, replay)
	})
}

// HubReleaseReviewRequest drives the expectation-release review journey from
// the suite panel's release surface: one workspace entry holding a released
// expectation, posted either as a review request or as the approval of the
// outstanding request that names the same release content. The command id is
// the only typed field; the release digest is derived from the entry's exact
// bytes, so the decision is content-bound by construction.
type HubReleaseReviewRequest struct {
	Project   string `json:"project"`
	Workspace string `json:"workspace"`
	Entry     string `json:"entry"`
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Recipient string `json:"recipient"`
	Text      string `json:"text"`
}

// PostHubReleaseReview posts the released expectation named by one workspace
// entry as a hub review-request, or approves the outstanding review-request
// naming the same release content. The entry's exact bytes are verified as a
// released expectation before anything is sent, the upload and the command
// name the digest of those bytes, and identity comes from the authenticated
// session — the panel's local approver label never substitutes for it. The
// hub stays the authority: it re-reads the release and enforces the request
// and approval chain, so a stale head or changed grant fails there.
func (a *App) PostHubReleaseReview(request HubReleaseReviewRequest) HubReviewsResult {
	return runNamed[HubReviewsResult, *HubReviewsResult](a, profiles["PostHubReleaseReview"], func(ctx context.Context) HubReviewsResult {
		if request.Kind != "review-request" && request.Kind != "approval" {
			return HubReviewsResult{State: Failed, Project: request.Project,
				Reason: hubclient.ErrReleaseReviewKind.Error()}
		}
		if strings.TrimSpace(request.ID) == "" {
			return HubReviewsResult{State: Failed, Project: request.Project,
				Reason: "name the review command id the hub records"}
		}
		if strings.TrimSpace(request.Text) == "" {
			return HubReviewsResult{State: Failed, Project: request.Project,
				Reason: "the review command carries the rationale the team reads"}
		}
		if request.Kind == "review-request" && strings.TrimSpace(request.Recipient) == "" {
			return HubReviewsResult{State: Failed, Project: request.Project,
				Reason: "a review request names the subject it asks to review"}
		}
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return HubReviewsResult{State: declined.state, Project: request.Project, Reason: declined.reason}
		}
		data, declined := workspaceDocument(root, request.Entry, expectation.MaxBytes, "the released expectation")
		if data == nil {
			return HubReviewsResult{State: declined.state, Project: request.Project, Reason: declined.reason}
		}
		if _, err := expectation.Decode(data); err != nil {
			return HubReviewsResult{State: Failed, Project: request.Project,
				Reason: "the entry is not a released expectation the hub can verify: " + err.Error()}
		}
		sum := sha256.Sum256(data)
		client, errRes := a.requireHubSession()
		if errRes != nil {
			return *errRes
		}
		event, replay, err := client.PostReleaseReview(ctx, hubclient.ReleaseReview{
			Project: request.Project, ID: request.ID, Kind: request.Kind,
			Release: hex.EncodeToString(sum[:]), Path: filepath.Join(root, request.Entry),
			Recipient: request.Recipient, Text: request.Text,
		})
		if err != nil {
			return mapReviewError(request.Project, err)
		}
		return reviewWriteResult(request.Project, event, replay)
	})
}

// reviewWriteResult carries one recorded review decision with its session
// actor and the custody notice every collaboration result repeats.
func reviewWriteResult(project string, event hubprotocol.ReviewEvent, replay bool) HubReviewsResult {
	view := mapReviewEvent(event)
	return HubReviewsResult{
		State: Completed, Project: project, Head: event.Sequence,
		Events: []HubReviewEventView{view}, Replay: replay,
		Warning: custodyNotice,
	}
}

// HubSupportReviewRequest drives the sharing journey from the hub panel above
// the privacy screens: the project's sharing policy is announced by uploading
// its exact bytes and posting support-policy, a published value-free summary
// is requested for team review by uploading its support.json member, and the
// asked reviewer approves the request that names those same summary bytes.
// The hub's export route serves the summary only under that approval chain,
// and the privacy panels' local approval inputs are never involved: those
// approve a local publication by identity, while this journey is the team's
// authenticated decision about the same bytes.
type HubSupportReviewRequest struct {
	Project   string `json:"project"`
	Workspace string `json:"workspace"`
	Entry     string `json:"entry"`
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Recipient string `json:"recipient"`
}

// PostHubSupportReview posts one of the hub's v2 sharing commands. The bytes
// each command names are verified locally before anything is sent — the
// policy decodes as a sharing policy, the bundle's support.json decodes as a
// value-free summary — then uploaded, and the command names the digest of
// exactly those bytes. The policy in force is read from the hub's own
// history, so a request and its approval always bind to the policy the
// project recorded; identity is the signed-in session's and the hub stays
// the authority for roles, chains and the policy-in-force checks.
func (a *App) PostHubSupportReview(request HubSupportReviewRequest) HubReviewsResult {
	return runNamed[HubReviewsResult, *HubReviewsResult](a, profiles["PostHubSupportReview"], func(ctx context.Context) HubReviewsResult {
		switch request.Kind {
		case "support-policy", "support-request", "support-approval":
		default:
			return HubReviewsResult{State: Failed, Project: request.Project,
				Reason: hubclient.ErrSupportReviewKind.Error()}
		}
		if strings.TrimSpace(request.ID) == "" {
			return HubReviewsResult{State: Failed, Project: request.Project,
				Reason: "name the review command id the hub records"}
		}
		if request.Kind == "support-request" && strings.TrimSpace(request.Recipient) == "" {
			return HubReviewsResult{State: Failed, Project: request.Project,
				Reason: "a support request names the subject it asks to approve"}
		}
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return HubReviewsResult{State: declined.state, Project: request.Project, Reason: declined.reason}
		}
		var document []byte
		var summary sharing.Summary
		if request.Kind == "support-policy" {
			data, declined := workspaceDocument(root, request.Entry, sharingPolicyLimit, "the sharing policy")
			if data == nil {
				return HubReviewsResult{State: declined.state, Project: request.Project, Reason: declined.reason}
			}
			if _, err := sharing.DecodePolicy(data); err != nil {
				return HubReviewsResult{State: Failed, Project: request.Project,
					Reason: "the entry is not a sharing policy the hub can hold: " + err.Error()}
			}
			document = data
		} else {
			data, declined := workspaceSummary(root, request.Entry)
			if declined.state != "" {
				return HubReviewsResult{State: declined.state, Project: request.Project, Reason: declined.reason}
			}
			parsed, err := sharing.Decode(data)
			if err != nil {
				return HubReviewsResult{State: Failed, Project: request.Project,
					Reason: "the bundle's support.json is not a value-free summary the hub can verify: " + err.Error()}
			}
			summary, document = parsed, data
		}
		sum := sha256.Sum256(document)
		client, errRes := a.requireHubSession()
		if errRes != nil {
			return *errRes
		}
		upload := filepath.Join(root, request.Entry)
		if request.Kind != "support-policy" {
			upload = artifactpath.JoinReference(upload, "support.json")
		}
		event, replay, err := client.PostSupportReview(ctx, hubclient.SupportReview{
			Project: request.Project, ID: request.ID, Kind: request.Kind,
			Digest: hex.EncodeToString(sum[:]), Path: upload,
			Recipient: request.Recipient, SummaryPolicy: summary.PolicyIdentity,
		})
		if err != nil {
			return mapReviewError(request.Project, err)
		}
		return reviewWriteResult(request.Project, event, replay)
	})
}

// workspaceSummary reads one published support bundle's support.json member:
// one directory entry of the open workspace, the member a regular file inside
// the sharing summary's own bound.
func workspaceSummary(root, entry string) ([]byte, refusal) {
	if err := artifactpath.EntryName(entry); err != nil {
		return nil, refusal{Failed, "the published support bundle must be named by one directory entry of the open workspace"}
	}
	bundle := filepath.Join(root, entry)
	info, err := os.Lstat(bundle)
	if err != nil || !info.IsDir() {
		return nil, refusal{Failed, "the published support bundle must be one directory entry of the open workspace"}
	}
	path := artifactpath.JoinReference(bundle, "support.json")
	member, err := os.Lstat(path)
	if err != nil || !member.Mode().IsRegular() {
		return nil, refusal{Failed, "the published bundle carries no support.json; publish it locally before asking the team"}
	}
	if member.Size() > 65536 {
		return nil, refusal{Failed, "the support summary is larger than this release reads"}
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrPermission) {
		return nil, refusal{PermissionDenied, "this account cannot read the published support summary"}
	}
	if err != nil {
		return nil, refusal{Failed, "the support summary could not be read"}
	}
	return data, refusal{}
}

// ListHubLifecycle reads lifecycle history, unresolved tips and the custody warning.
func (a *App) ListHubLifecycle(project string) HubLifecycleResult {
	return runNamed[HubLifecycleResult, *HubLifecycleResult](a, profiles["ListHubLifecycle"], func(ctx context.Context) HubLifecycleResult {
		client, errRes := a.requireHubSession()
		if errRes != nil {
			return HubLifecycleResult{State: errRes.State, Reason: errRes.Reason}
		}
		history, err := client.GetLifecycle(ctx, project)
		if err != nil {
			return mapLifecycleError(project, err)
		}
		events := make([]HubLifecycleEventView, len(history.Events))
		for i, e := range history.Events {
			events[i] = mapLifecycleEvent(e)
		}
		return HubLifecycleResult{
			State: Completed, Project: project, Head: history.Head,
			Events: events, Tips: history.Tips, Warning: history.Warning,
		}
	})
}

// PostHubLifecycle records a revision, resolve, retention, retire, remove-user
// or audit-export command. Retried writes reuse the same id; a stale head fails.
func (a *App) PostHubLifecycle(request HubLifecycleCommandRequest) HubLifecycleResult {
	return a.postHubLifecycle(profiles["PostHubLifecycle"], request)
}

func (a *App) postHubLifecycle(profile operationguard.Profile, request HubLifecycleCommandRequest) HubLifecycleResult {
	// An audit export only reads; every other lifecycle command writes and
	// admits the author its profile declares.
	profile.Author = profile.Author && request.Kind != "audit-export"
	return runNamed[HubLifecycleResult, *HubLifecycleResult](a, profile, func(ctx context.Context) HubLifecycleResult {
		client, errRes := a.requireHubSession()
		if errRes != nil {
			return HubLifecycleResult{State: errRes.State, Reason: errRes.Reason}
		}
		parents := request.Parents
		if parents == nil {
			parents = []string{}
		}
		result, err := client.PostLifecycle(ctx, request.Project, hubprotocol.LifecycleCommand{
			Schema: hubprotocol.LifecycleCommandSchema,
			ID:     request.ID, Expected: request.Expected, Kind: request.Kind,
			Resource: request.Resource, Artifact: request.Artifact, Parents: parents,
			Subject: request.Subject, Until: request.Until, Reason: request.Reason,
		})
		if err != nil {
			return mapLifecycleError(request.Project, err)
		}
		out := HubLifecycleResult{State: Completed, Project: request.Project, Replay: result.Replay, Warning: custodyNotice}
		if result.Event != nil {
			view := mapLifecycleEvent(*result.Event)
			out.Event = &view
			out.Head = result.Event.Sequence
		}
		if result.Audit != nil {
			out.Audit = &HubAuditExportView{
				Schema: result.Audit.Schema, Project: result.Audit.Project,
				ReviewHead: result.Audit.ReviewHead, Warning: result.Audit.Warning,
			}
			out.Audit.Lifecycle = make([]HubLifecycleEventView, len(result.Audit.Lifecycle))
			for i, e := range result.Audit.Lifecycle {
				out.Audit.Lifecycle[i] = mapLifecycleEvent(e)
			}
			out.Audit.Reviews = make([]HubReviewEventView, len(result.Audit.Reviews))
			for i, e := range result.Audit.Reviews {
				out.Audit.Reviews[i] = mapReviewEvent(e)
			}
			out.Warning = result.Audit.Warning
		}
		return out
	})
}

// DownloadHubExport downloads an authorized support export with custody notice.
func (a *App) DownloadHubExport(request HubDownloadRequest) HubTransferResult {
	return runNamed[HubTransferResult, *HubTransferResult](a, profiles["DownloadHubExport"], func(ctx context.Context) HubTransferResult {
		client, errRes := a.requireHubSession()
		if errRes != nil {
			return HubTransferResult{State: errRes.State, Reason: errRes.Reason}
		}
		res, err := client.DownloadExport(ctx, request.Project, request.Digest, request.DestinationPath)
		if err != nil {
			if errors.Is(err, hubclient.ErrAccessDenied) || errors.Is(err, hubclient.ErrExpired) {
				return HubTransferResult{State: PermissionDenied, Reason: err.Error(), TransferState: res.State}
			}
			return HubTransferResult{State: Failed, Reason: err.Error(), TransferState: res.State}
		}
		return HubTransferResult{
			State: Completed, TransferState: res.State, Digest: res.Digest,
			Size: res.Size, Path: res.Path, Warning: res.Warning,
		}
	})
}

// SaveHubOfflineDraft retains an offline hub revision branch locally. It never
// stores approvals or credentials; reconnect requires an explicit PostHubLifecycle.
func (a *App) SaveHubOfflineDraft(request HubOfflineDraftRequest) EditorDraftsResult {
	content, err := json.Marshal(HubRevisionDraft{
		Schema:  HubRevisionDraftSchema,
		Project: request.Project, Resource: request.Resource,
		ParentTips: request.ParentTips, LocalPath: request.LocalPath,
		ExpectedHead: request.ExpectedHead, Note: request.Note,
	})
	if err != nil {
		return EditorDraftsResult{State: Failed, Reason: "cannot encode hub revision draft"}
	}
	draft := EditorDraft{
		ID: request.DraftID, Kind: hubRevisionDraftKind,
		Workspace: request.Workspace, Case: request.Resource, Identity: request.Project,
		ContentSchema: HubRevisionDraftSchema, Content: jsontext.Value(content),
	}
	return a.SaveEditorDraft(draft)
}

// ReconcileHubOfflineDraft posts a retained offline draft as a lifecycle revision
// after an explicit reconnect. A stale head or changed grant requires renewed action.
func (a *App) ReconcileHubOfflineDraft(request HubLifecycleCommandRequest) HubLifecycleResult {
	return a.postHubLifecycle(profiles["ReconcileHubOfflineDraft"], request)
}

// requireHubSession is the connection's session gate as the window reports
// it: no connection fails, and no current session is permission denied.
func (a *App) requireHubSession() (*hubclient.Client, *HubReviewsResult) {
	client, _, err := a.hub.SignedIn()
	switch {
	case errors.Is(err, hubclient.ErrNotConnected):
		return nil, &HubReviewsResult{State: Failed, Reason: err.Error()}
	case err != nil:
		return nil, &HubReviewsResult{State: PermissionDenied, Reason: err.Error()}
	}
	return client, nil
}

func mapReviewHistory(project string, history hubprotocol.ReviewHistory, err error) HubReviewsResult {
	if err != nil {
		return mapReviewError(project, err)
	}
	events := make([]HubReviewEventView, len(history.Events))
	for i, e := range history.Events {
		events[i] = mapReviewEvent(e)
	}
	return HubReviewsResult{
		State: Completed, Project: project, Head: history.Head, Events: events,
		Warning: custodyNotice,
	}
}

func mapReviewEvent(e hubprotocol.ReviewEvent) HubReviewEventView {
	return HubReviewEventView{
		Schema: e.Schema, Project: e.Project, Sequence: e.Sequence,
		Issuer: e.Issuer, Actor: e.Actor, At: e.At,
		Kind: e.Command.Kind, Evidence: e.Command.Evidence, Parent: e.Command.Parent,
		Recipient: e.Command.Recipient, Text: e.Command.Text, Release: e.Command.Release,
		CommandID: e.Command.ID,
	}
}

func mapReviewError(project string, err error) HubReviewsResult {
	switch {
	case errors.Is(err, hubclient.ErrAccessDenied), errors.Is(err, hubclient.ErrExpired):
		return HubReviewsResult{State: PermissionDenied, Project: project, Reason: err.Error(), Warning: custodyNotice}
	default:
		return HubReviewsResult{State: Failed, Project: project, Reason: err.Error(), Warning: custodyNotice}
	}
}

func mapLifecycleEvent(e hubprotocol.LifecycleEvent) HubLifecycleEventView {
	return HubLifecycleEventView{
		Schema: e.Schema, Project: e.Project, Sequence: e.Sequence,
		Issuer: e.Issuer, Actor: e.Actor, At: e.At, ReviewHead: e.ReviewHead,
		Kind: e.Command.Kind, Resource: e.Command.Resource, Artifact: e.Command.Artifact,
		Parents: e.Command.Parents, Subject: e.Command.Subject, Until: e.Command.Until,
		Reason: e.Command.Reason, CommandID: e.Command.ID,
	}
}

func mapLifecycleError(project string, err error) HubLifecycleResult {
	switch {
	case errors.Is(err, hubclient.ErrAccessDenied), errors.Is(err, hubclient.ErrExpired):
		return HubLifecycleResult{State: PermissionDenied, Project: project, Reason: err.Error(), Warning: custodyNotice}
	default:
		return HubLifecycleResult{State: Failed, Project: project, Reason: err.Error(), Warning: custodyNotice}
	}
}

// ExplainHubCustody returns the permanent custody notice for already-downloaded copies.
func (a *App) ExplainHubCustody() HubResult {
	return HubResult{
		State: Completed, CustodyWarning: custodyNotice,
		Reason: "Deleting a server grant or removing a user refuses new authorized requests; already-downloaded files and local authorized exports remain under local custody and cannot be revoked by the hub.",
	}
}

// FormatHubConflictSummary builds a readable tip conflict summary for the window.
func FormatHubConflictSummary(resource string, tips []string) string {
	if len(tips) == 0 {
		return fmt.Sprintf("resource %s has no unresolved revisions", resource)
	}
	return fmt.Sprintf("resource %s has %d unresolved tip(s): %s — compare, keep-both as a new revision, or resolve with every current tip; never overwrite silently",
		resource, len(tips), strings.Join(tips, ", "))
}
