package desktop

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/hubclient"
	"github.com/bharm16/readmit/internal/hubprotocol"
	"github.com/bharm16/readmit/internal/textmerge"
)

// The team's reviewed transfers: adding a file to a project, adding a
// revision of a resource, and resolving a resource's conflicting revisions.
// Each is prepared, shown and executed through the one review lifecycle, so
// its final click is consent to exactly the bytes, team, project and base its
// review showed, sent once: a changed file, head, tip, role or session is a
// stale review, never a silent retry.

const (
	// TeamUploadAction adds one local file to a team project.
	TeamUploadAction ActionID = "team.upload"
	// TeamRevisionAction adds one local file as the next revision of a
	// project resource.
	TeamRevisionAction ActionID = "team.revision"
	// TeamResolveAction records one resolution of every unresolved revision
	// of a resource.
	TeamResolveAction ActionID = "team.resolve"
)

// UploadConsent: the displayed file is sent to the displayed team project.
const UploadConsent Consent = "upload"

// TeamActionOptions are what a team transfer is prepared for.
type TeamActionOptions struct {
	Project string `json:"project,omitzero"`
	// Source is the local file an upload or revision sends.
	Source string `json:"source,omitzero"`
	// Resource and Base are a revision's: the resource and the revision it
	// continues, empty for a resource's first.
	Resource string `json:"resource,omitzero"`
	Base     string `json:"base,omitzero"`
	// Resolution is how a resolve settles the conflicting revisions.
	Resolution *TeamResolution `json:"resolution,omitzero"`
}

// TeamResolution settles a resource's conflicting revisions: one whole
// revision chosen, or, for text two revisions changed, one side chosen for
// each conflicting part in order.
type TeamResolution struct {
	Scope   string        `json:"scope"`
	Whole   string        `json:"whole,omitzero"`
	Choices []MergeChoice `json:"choices,omitzero"`
}

// MergeChoice is the side one conflicting part of a merge takes.
type MergeChoice string

const (
	MergeBase    MergeChoice = "base"
	MergeYours   MergeChoice = "yours"
	MergeCurrent MergeChoice = "current"
)

// TeamActionReview is what a team transfer will send and where.
type TeamActionReview struct {
	PendingOperation string `json:"pending_operation,omitzero"`
	ConflictScope    string `json:"conflict_scope,omitzero"`
	Team             string `json:"team,omitzero"`
	Project          string `json:"project"`
	Name             string `json:"name,omitzero"`
	Size             int64  `json:"size"`
	// Resource, Base and Tips are a revision's or resolution's: the
	// revision it continues and the resource's unresolved revisions now.
	Resource string        `json:"resource,omitzero"`
	Base     *HubRevision  `json:"base,omitzero"`
	Tips     []HubRevision `json:"tips,omitzero"`
	// Conflicts are the parts of a merge still to choose a side for.
	Conflicts int `json:"conflicts,omitzero"`
}

// A file path and subject do not identify a connected destination. Reviews
// also bind the actual client/session and the configuration they use.
func (a *App) teamConnectionBinding(client *hubclient.Client, session *hubclient.Session) string {
	status := a.hub.Status()
	return binding(status.ConfigPath, hubAddress(a), fmt.Sprintf("%p", client), fmt.Sprintf("%p", session))
}

func (a *App) pendingTeamRevision(session *hubclient.Session, project, resource string) string {
	a.reviews.mu.Lock()
	defer a.reviews.mu.Unlock()
	for operation, intent := range a.reviews.intents {
		outcome := intent.result.Team
		if intent.result.Outcome == ActionUncertain && outcome != nil && outcome.expected != nil && outcome.Project == project && outcome.Resource == resource &&
			outcome.hub == hubAddress(a) && outcome.actor == session.Subject && outcome.issuer == session.Issuer {
			return operation
		}
	}
	return ""
}

// TeamActionOutcome is what a team transfer recorded.
type TeamActionOutcome struct {
	expected      *hubprotocol.LifecycleEvent
	hub           string
	issuer, actor string
	Digest        string `json:"digest"`
	Revision      string `json:"revision,omitzero"`
	Project       string `json:"project"`
	Resource      string `json:"resource,omitzero"`
	CommandID     string `json:"command_id,omitzero"`
	Uploaded      bool   `json:"uploaded,omitzero"`
}

// teamBinding is what a team transfer sends: the exact bytes, and a
// revision's or resolution's command parts.
type teamBinding struct {
	project  string
	data     []byte
	resource string
	parents  []string
	head     int
	kind     string
}

// teamSession is the signed-in session a team transfer is made in, and the
// project, which the selected configuration must name.
func (a *App) teamSession(request PrepareActionRequest) (*hubclient.Client, *hubclient.Session, TeamActionOptions, refusal) {
	if request.Team == nil {
		return nil, nil, TeamActionOptions{}, refusal{Failed, "a team transfer names its project"}
	}
	options := *request.Team
	client, session, declined := a.signedIn()
	if declined.state != "" {
		return nil, nil, options, declined
	}
	status := a.hub.Status()
	if status.Config == nil || !slices.Contains(status.Config.Projects, options.Project) {
		return nil, nil, options, refusal{Failed, "this team has no such project"}
	}
	return client, session, options, noRefusal
}

// teamDisplay starts a team transfer's review.
func (a *App) teamDisplay(options TeamActionOptions) TeamActionReview {
	name, _ := a.hubTeamLabel(a.hub.Status().ConfigPath)
	return TeamActionReview{Team: cmp.Or(name, hubAddress(a)), Project: options.Project}
}

func hubAddress(a *App) string {
	if cfg := a.hub.Status().Config; cfg != nil {
		return cfg.Hub
	}
	return ""
}

// readTeamSource reads the local file a transfer sends, whole.
func readTeamSource(path string) ([]byte, refusal) {
	if !filepath.IsAbs(path) {
		return nil, refusal{Failed, "choose the file to send"}
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, refusal{Failed, "the chosen file cannot be read"}
	}
	if info.Size() > hubclient.MaxArtifactBytes {
		return nil, refusal{Failed, "the file is larger than a team project holds"}
	}
	data, err := boundedFile(path, hubclient.MaxArtifactBytes)
	if err != nil {
		return nil, refusal{Failed, "the chosen file cannot be read"}
	}
	return data, noRefusal
}

func bindTeamUpload(a *App, _ context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	client, session, options, declined := a.teamSession(request)
	if declined.state != "" {
		return nil, declined
	}
	data, declined := readTeamSource(options.Source)
	if declined.state != "" {
		return nil, declined
	}
	display := a.teamDisplay(options)
	display.Name, display.Size = filepath.Base(options.Source), int64(len(data))
	review := ActionReview{Items: []CatalogItem{}, Ready: true, Team: &display}
	if !session.Allows("evidence.write") {
		review.Ready, review.Refusal = false, "Your role in "+options.Project+" cannot add files"
	}
	return &boundAction{action: TeamUploadAction, origin: request, review: review,
		team:    &teamBinding{project: options.Project, data: data},
		binding: binding(string(TeamUploadAction), a.teamConnectionBinding(client, session), options.Project, session.Issuer, session.Subject, digestOf(data), display.Name)}, noRefusal
}

func bindTeamRevision(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	client, session, options, declined := a.teamSession(request)
	if declined.state != "" {
		return nil, declined
	}
	if !hubprotocol.ValidProject(options.Resource) {
		return nil, refusal{Failed, "Name the resource with lowercase letters, digits and hyphens"}
	}
	data, declined := readTeamSource(options.Source)
	if declined.state != "" {
		return nil, declined
	}
	history, err := client.GetLifecycle(ctx, options.Project)
	if err != nil {
		return nil, refusal{hubReadState(err), "the project's history cannot be read: " + err.Error()}
	}
	resource := findResource(teamResources(history), options.Resource)
	display := a.teamDisplay(options)
	display.Name, display.Size, display.Resource = filepath.Base(options.Source), int64(len(data)), options.Resource
	review := ActionReview{Items: []CatalogItem{}, Ready: true, Team: &display}
	notReady := func(reason string) {
		if review.Ready {
			review.Ready, review.Refusal = false, reason
		}
	}
	parents := []string{}
	if resource != nil {
		display.Tips = revisionsNamed(resource, resource.Tips)
		base := revisionsNamed(resource, []string{options.Base})
		switch {
		case options.Base == "":
			notReady("Choose the revision this one continues")
		case len(base) == 0:
			notReady("The revision this one continues is not recorded for " + options.Resource)
		default:
			display.Base = &base[0]
			parents = []string{options.Base}
		}
	} else if options.Base != "" {
		notReady("The revision this one continues is not recorded for " + options.Resource)
	}
	if !session.Allows("evidence.write") {
		notReady("Your role in " + options.Project + " cannot add revisions")
	}
	if pending := a.pendingTeamRevision(session, options.Project, options.Resource); pending != "" {
		display.PendingOperation = pending
		notReady("a previous revision is unconfirmed; check its status before another submission")
	}
	return &boundAction{action: TeamRevisionAction, origin: request, review: review,
		team: &teamBinding{project: options.Project, data: data, resource: options.Resource, parents: parents, head: history.Head, kind: "revision"},
		binding: binding(string(TeamRevisionAction), a.teamConnectionBinding(client, session), options.Project, session.Issuer, session.Subject, digestOf(data), display.Name,
			options.Resource, strings.Join(parents, ","), strconv.Itoa(history.Head), strings.Join(display.tipIDs(), ","))}, noRefusal
}

func bindTeamResolve(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	client, session, options, declined := a.teamSession(request)
	if declined.state != "" {
		return nil, declined
	}
	conflict, declined := a.readConflict(ctx, client, session, options.Project, options.Resource)
	if declined.state != "" {
		return nil, declined
	}
	display := a.teamDisplay(options)
	display.Resource, display.Tips, display.ConflictScope = options.Resource, conflict.Tips, conflict.Scope
	review := ActionReview{Items: []CatalogItem{}, Ready: true, Team: &display}
	resolution := TeamResolution{}
	if pending := a.pendingTeamRevision(session, options.Project, options.Resource); pending != "" {
		display.PendingOperation = pending
		review.Ready, review.Refusal = false, "a previous revision is unconfirmed; check its status before another submission"
		return &boundAction{action: TeamResolveAction, origin: request, review: review, binding: binding(string(TeamResolveAction), conflict.Scope, pending)}, noRefusal
	}
	if options.Resolution != nil {
		resolution = *options.Resolution
	}
	if resolution.Scope != conflict.Scope {
		review.Ready, review.Refusal = false, "the competing revisions changed; read them again and choose a resolution"
		return &boundAction{action: TeamResolveAction, origin: request, review: review,
			binding: binding(string(TeamResolveAction), conflict.Scope)}, noRefusal
	}
	data, reason := conflict.resolve(resolution)
	if reason != "" {
		review.Ready, review.Refusal = false, reason
	}
	display.Size, display.Conflicts = int64(len(data)), conflict.conflicts()
	if !session.Allows("evidence.write") && review.Ready {
		review.Ready, review.Refusal = false, "Your role in "+options.Project+" cannot resolve revisions"
	}
	return &boundAction{action: TeamResolveAction, origin: request, review: review,
		team: &teamBinding{project: options.Project, data: data, resource: options.Resource, parents: display.tipIDs(), head: conflict.head, kind: "resolve"},
		binding: binding(string(TeamResolveAction), a.teamConnectionBinding(client, session), options.Project, session.Issuer, session.Subject, digestOf(data),
			options.Resource, strings.Join(display.tipIDs(), ","), strconv.Itoa(conflict.head))}, noRefusal
}

func (r TeamActionReview) tipIDs() []string {
	ids := []string{}
	for _, tip := range r.Tips {
		ids = append(ids, tip.ID)
	}
	slices.Sort(ids)
	return ids
}

// executeTeam sends the bound bytes and, for a revision or resolution, records
// its command under the click's own identity, so a click that arrives again is
// the same command.
func executeTeam(a *App, ctx context.Context, bound *boundAction, decisions ReviewDecisions) ReviewedActionResult {
	team := bound.team
	result := ReviewedActionResult{Outcome: ActionCompleted}
	client, session, declined := a.signedIn()
	if declined.state != "" {
		result.Outcome = ActionRefused
		result.refuse(declined.state, declined.reason)
		return result
	}
	outcome := &TeamActionOutcome{Digest: digestOf(team.data), Project: team.project, Resource: team.resource, hub: hubAddress(a), issuer: session.Issuer, actor: session.Subject}
	result.Team = outcome
	digest, err := client.UploadBytes(ctx, team.project, team.data)
	if err != nil {
		result.refuse(hubReadState(err), "the upload could not be confirmed; check the team's files before sending again")
		result.Outcome = ActionUncertain
		if definitiveTeamRefusal(err) {
			result.Outcome = ActionRefused
			result.Reason = "the upload was refused: " + err.Error()
		}
		return result
	}
	outcome.Digest, outcome.Uploaded = digest, true
	result.State, result.Team = Completed, outcome
	if team.kind == "" {
		return result
	}
	intent := ""
	if bound.executionReview != nil {
		intent = bound.executionReview.intent
	}
	sum := sha256.Sum256([]byte(intent + "\x00" + string(bound.action)))
	command := hubprotocol.LifecycleCommand{Schema: hubprotocol.LifecycleCommandSchema, ID: team.kind + "-" + hex.EncodeToString(sum[:])[:40], Expected: team.head,
		Kind: team.kind, Resource: team.resource, Artifact: digest, Parents: team.parents, Reason: strings.TrimSpace(decisions.Rationale)}
	outcome.CommandID = command.ID
	outcome.hub = hubAddress(a)
	outcome.expected = &hubprotocol.LifecycleEvent{Schema: hubprotocol.LifecycleEventSchema, Project: team.project, Sequence: command.Expected + 1,
		Issuer: session.Issuer, Actor: session.Subject, Command: command}
	written, err := client.PostLifecycle(ctx, team.project, command)
	if err != nil {
		result.Outcome = ActionUncertain
		result.refuse(hubReadState(err), "the file was uploaded; the revision's outcome is unconfirmed. Check its status before submitting again")
		if errors.Is(err, hubclient.ErrConflict) {
			result.Outcome = ActionRefused
			result.refuse(Failed, "the file was uploaded, but someone changed "+team.resource+" meanwhile; review it again")
		} else if definitiveTeamRefusal(err) {
			result.Outcome = ActionRefused
			result.refuse(hubReadState(err), "the file was uploaded; its revision was refused: "+err.Error())
		}
		return result
	}
	if written.Event != nil {
		outcome.Revision = written.Event.Command.ID
	}
	return result
}

func definitiveTeamRefusal(err error) bool {
	return errors.Is(err, hubclient.ErrAccessDenied) || errors.Is(err, hubclient.ErrExpired) || errors.Is(err, hubclient.ErrCommandRefused) || errors.Is(err, hubclient.ErrIntegrity)
}

// ReconcileTeamTransfer checks a held operation's exact command against
// authenticated lifecycle metadata. It sends no bytes and repeats no write.
func (a *App) ReconcileTeamTransfer(operation string) ReviewedActionResult {
	return runNamed[ReviewedActionResult, *ReviewedActionResult](a, profiles["ReconcileTeamTransfer"], func(ctx context.Context) ReviewedActionResult {
		a.reviews.mu.Lock()
		intent := a.reviews.intents[operation]
		var retained ReviewedActionResult
		if intent != nil {
			retained = intent.result
		}
		a.reviews.mu.Unlock()
		if retained.Team == nil || retained.Outcome != ActionUncertain && retained.Outcome != ActionCompleted {
			return ReviewedActionResult{State: Failed, Outcome: ActionRefused, Reason: "no unconfirmed revision is held for this operation"}
		}
		client, session, declined := a.signedIn()
		if declined.state != "" {
			retained.refuse(declined.state, declined.reason)
			return retained
		}
		expected := retained.Team.expected
		if hubAddress(a) != retained.Team.hub || session.Subject != retained.Team.actor || session.Issuer != retained.Team.issuer {
			retained.refuse(PermissionDenied, "this revision belongs to another team or person")
			return retained
		}
		if retained.Outcome == ActionCompleted {
			return retained
		}
		if expected == nil {
			files, err := client.ListFiles(ctx, retained.Team.Project)
			if err != nil {
				retained.refuse(hubReadState(err), "the upload is still unconfirmed")
				return retained
			}
			for _, file := range files.Files {
				if file.Digest != retained.Team.Digest {
					continue
				}
				copy := *retained.Team
				copy.Uploaded = true
				retained.Team, retained.State, retained.Reason = &copy, Completed, ""
				if copy.Resource == "" {
					retained.Outcome = ActionCompleted
				} else {
					retained.Outcome, retained.Reason = ActionRefused, "the file was uploaded; its revision was not attempted"
				}
				a.reviews.mu.Lock()
				if a.reviews.intents[operation] == intent {
					intent.result = retained
				}
				a.reviews.mu.Unlock()
				return retained
			}
			retained.Reason = "the upload is still unconfirmed; no bytes were resent"
			return retained
		}
		history, err := client.GetLifecycle(ctx, expected.Project)
		if err != nil {
			retained.refuse(hubReadState(err), "the revision is still unconfirmed")
			return retained
		}
		wanted, _ := json.Marshal(expected.Command, json.Deterministic(true))
		for _, event := range history.Events {
			actual, _ := json.Marshal(event.Command, json.Deterministic(true))
			if event.Schema != expected.Schema || event.Project != expected.Project || event.Sequence != expected.Sequence || event.Actor != expected.Actor ||
				event.Issuer != expected.Issuer || !bytes.Equal(actual, wanted) {
				continue
			}
			copy := *retained.Team
			copy.Revision = event.Command.ID
			retained.State, retained.Outcome, retained.Reason, retained.Team = Completed, ActionCompleted, "", &copy
			a.reviews.mu.Lock()
			if a.reviews.intents[operation] == intent {
				intent.result = retained
			}
			a.reviews.mu.Unlock()
			return retained
		}
		retained.Reason = "the revision is still unconfirmed; no write was repeated"
		return retained
	})
}

func findResource(resources []HubResource, name string) *HubResource {
	for i := range resources {
		if resources[i].Resource == name {
			return &resources[i]
		}
	}
	return nil
}

func revisionsNamed(resource *HubResource, ids []string) []HubRevision {
	out := []HubRevision{}
	for _, id := range ids {
		for _, revision := range resource.Revisions {
			if revision.ID == id {
				out = append(out, revision)
			}
		}
	}
	return out
}

// HubConflictRequest names a resource whose revisions conflict.
type HubConflictRequest struct {
	Project  string `json:"project"`
	Resource string `json:"resource"`
}

// HubConflictResult is a resource's conflicting revisions, side by side:
// the revision they share, the signed-in person's own and the other one, and,
// where both are text, the merge part by part.
type HubConflictResult struct {
	Scope   string        `json:"scope,omitzero"`
	State   State         `json:"state"`
	Reason  string        `json:"reason,omitzero"`
	Tips    []HubRevision `json:"tips"`
	Base    *HubRevision  `json:"base,omitzero"`
	Yours   string        `json:"yours,omitzero"`
	Current string        `json:"current,omitzero"`
	// Text says the two revisions and their base are text a merge reads;
	// otherwise one whole revision is chosen.
	Text  bool        `json:"text"`
	Hunks []MergeHunk `json:"hunks"`
}

func (r *HubConflictResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// MergeHunk is one part of a merge: lines both sides agree on, or a conflict
// where each side changed the base differently.
type MergeHunk struct {
	Conflict bool     `json:"conflict,omitzero"`
	Lines    []string `json:"lines,omitzero"`
	Base     []string `json:"base,omitzero"`
	Yours    []string `json:"yours,omitzero"`
	Current  []string `json:"current,omitzero"`
}

// OpenHubConflict reads a resource's conflicting revisions — a deliberate
// read of their bytes, each verified against its digest — and merges them
// where they are text.
func (a *App) OpenHubConflict(request HubConflictRequest) HubConflictResult {
	return runNamed[HubConflictResult, *HubConflictResult](a, profiles["OpenHubConflict"], func(ctx context.Context) HubConflictResult {
		client, session, declined := a.signedIn()
		if declined.state != "" {
			return HubConflictResult{State: declined.state, Reason: declined.reason, Tips: []HubRevision{}, Hunks: []MergeHunk{}}
		}
		conflict, declined := a.readConflict(ctx, client, session, request.Project, request.Resource)
		if declined.state != "" {
			return HubConflictResult{State: declined.state, Reason: declined.reason, Tips: []HubRevision{}, Hunks: []MergeHunk{}}
		}
		out := conflict.HubConflictResult
		out.State = Completed
		return out
	})
}

// conflict is a resource's conflicting revisions as read, with the bytes of
// each and the merge.
type conflict struct {
	HubConflictResult
	head  int
	bytes map[string][]byte
}

func (a *App) readConflict(ctx context.Context, client *hubclient.Client, session *hubclient.Session, project, name string) (out conflict, declined refusal) {
	out = conflict{HubConflictResult: HubConflictResult{Tips: []HubRevision{}, Hunks: []MergeHunk{}}, bytes: map[string][]byte{}}
	defer func() {
		parts := []string{a.teamConnectionBinding(client, session), project, name, strconv.Itoa(out.head), out.Yours, out.Current}
		for _, tip := range out.Tips {
			parts = append(parts, tip.ID, tip.Artifact)
		}
		if out.Base != nil {
			parts = append(parts, out.Base.ID, out.Base.Artifact)
		}
		out.Scope = binding(parts...)
	}()
	history, err := client.GetLifecycle(ctx, project)
	if err != nil {
		return out, refusal{hubReadState(err), "the project's history cannot be read: " + err.Error()}
	}
	resource := findResource(teamResources(history), name)
	if resource == nil || len(resource.Tips) < 2 {
		return out, refusal{Failed, name + " has no conflicting revisions"}
	}
	out.head = history.Head
	out.Tips = revisionsNamed(resource, resource.Tips)
	for _, tip := range out.Tips {
		data, _, err := client.ReadArtifact(ctx, project, tip.Artifact)
		if err != nil {
			return out, refusal{hubReadState(err), "a revision cannot be read: " + err.Error()}
		}
		out.bytes[tip.ID] = data
	}
	if len(out.Tips) != 2 {
		return out, noRefusal
	}
	out.Yours, out.Current = out.Tips[0].ID, out.Tips[1].ID
	if out.Tips[1].Actor == session.Subject && out.Tips[0].Actor != session.Subject {
		out.Yours, out.Current = out.Tips[1].ID, out.Tips[0].ID
	}
	base := commonAncestor(resource, out.Yours, out.Current)
	if base == nil {
		return out, noRefusal
	}
	out.Base = base
	data, _, err := client.ReadArtifact(ctx, project, base.Artifact)
	if err != nil {
		return out, refusal{hubReadState(err), "the shared revision cannot be read: " + err.Error()}
	}
	baseText, yours, current := textmerge.Lines(data), textmerge.Lines(out.bytes[out.Yours]), textmerge.Lines(out.bytes[out.Current])
	if baseText == nil || yours == nil || current == nil {
		return out, noRefusal
	}
	hunks, ok := textmerge.Merge(baseText, yours, current)
	if ok {
		out.Text = true
		for _, hunk := range hunks {
			out.Hunks = append(out.Hunks, MergeHunk{Conflict: hunk.Conflict, Lines: hunk.Lines, Base: hunk.Base, Yours: hunk.Yours, Current: hunk.Current})
		}
	}
	return out, noRefusal
}

func (c conflict) conflicts() int {
	count := 0
	for _, hunk := range c.Hunks {
		if hunk.Conflict {
			count++
		}
	}
	return count
}

// resolve is the resolution's bytes, or why it cannot be made yet. A whole
// revision is taken byte for byte; a merge takes each part's agreed lines
// and the chosen side of each conflict, and never invents a combination.
func (c conflict) resolve(resolution TeamResolution) ([]byte, string) {
	if resolution.Whole != "" {
		data, held := c.bytes[resolution.Whole]
		if !held {
			return nil, "Choose one of the conflicting revisions"
		}
		return data, ""
	}
	if !c.Text {
		return nil, "Choose one complete revision"
	}
	if len(resolution.Choices) != c.conflicts() {
		return nil, "Choose a side for every conflict"
	}
	var out strings.Builder
	at := 0
	for _, hunk := range c.Hunks {
		lines := hunk.Lines
		if hunk.Conflict {
			switch resolution.Choices[at] {
			case MergeBase:
				lines = hunk.Base
			case MergeYours:
				lines = hunk.Yours
			case MergeCurrent:
				lines = hunk.Current
			default:
				return nil, "Choose a side for every conflict"
			}
			at++
		}
		for _, line := range lines {
			out.WriteString(line)
		}
	}
	return []byte(out.String()), ""
}

// commonAncestor is the latest revision both tips descend from.
func commonAncestor(resource *HubResource, x, y string) *HubRevision {
	byID := map[string]HubRevision{}
	order := map[string]int{}
	for i, revision := range resource.Revisions {
		byID[revision.ID], order[revision.ID] = revision, i
	}
	ancestors := func(start string) map[string]bool {
		seen := map[string]bool{}
		stack := []string{start}
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[id] {
				continue
			}
			seen[id] = true
			stack = append(stack, byID[id].Parents...)
		}
		return seen
	}
	fromX, fromY := ancestors(x), ancestors(y)
	best := ""
	for id := range fromX {
		if fromY[id] && id != x && id != y && (best == "" || order[id] > order[best]) {
			best = id
		}
	}
	if best == "" {
		return nil
	}
	revision := byID[best]
	return &revision
}
