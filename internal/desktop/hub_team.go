package desktop

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/hubclient"
	"github.com/bharm16/readmit/internal/hubprotocol"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/sharing"
)

// Team is the customer hub as a person works with it: one named team they
// connect to and sign in to, whose projects' activity, files and reviews are
// read through the session they deliberately started, and whose
// administration is a set of named tasks. Every read here is metadata: a list
// never transfers an artifact's bytes, and a file's bytes move only on an
// explicit Download, Upload, revision or resolution.

// hubTeamSchema is the shell document that remembers the name a person gave
// the team whose configuration they selected. It names the configuration it
// belongs to, so a name is never shown for another team.
const hubTeamSchema = "readmit-desktop-hub-team/v1"

// BrowserOpener opens a URL in the person's own browser, where a sign-in the
// application started continues with the customer's identity provider.
type BrowserOpener interface {
	OpenBrowser(url string) error
}

// HubConfigChoice is a hub configuration file the person chose in Connect
// team, read but not yet selected: where it points and the name the sheet
// offers for it.
type HubConfigChoice struct {
	State    State    `json:"state"`
	Reason   string   `json:"reason,omitzero"`
	Config   string   `json:"config,omitzero"`
	HubURL   string   `json:"hub_url,omitzero"`
	Projects []string `json:"projects,omitzero"`
	Name     string   `json:"name,omitzero"`
}

func (r *HubConfigChoice) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// HubTeamRequest is what Connect team saves: the team's name and the
// organization's configuration file.
type HubTeamRequest struct {
	Name   string `json:"name"`
	Config string `json:"config"`
}

// ChooseHubTeamConfig opens the host's file dialog for the configuration an
// organization provided and reads it. Nothing is selected, remembered or
// contacted: Save in Connect team does the selecting.
func (a *App) ChooseHubTeamConfig() HubConfigChoice {
	return run(a, true, false, func(ctx context.Context) HubConfigChoice {
		files, declined := a.chooseFiles(ctx, "Choose your team's configuration", "Team configuration (*.json)", "*.json")
		if len(files) == 0 {
			return HubConfigChoice{State: declined.state, Reason: declined.reason}
		}
		cfg, err := hubclient.ReadConfig(files[0])
		if err != nil {
			return HubConfigChoice{State: Failed, Reason: "this file is not a team configuration Readmit reads: " + err.Error()}
		}
		name := ""
		if address, err := url.Parse(cfg.Hub); err == nil {
			name = address.Hostname()
		}
		return HubConfigChoice{State: Completed, Config: files[0], HubURL: cfg.Hub, Projects: cfg.Projects, Name: name}
	})
}

// SaveHubTeam selects the configuration and remembers the team's name. Saving
// never connects: the next step is the person's Sign in.
func (a *App) SaveHubTeam(request HubTeamRequest) HubResult {
	return run(a, false, false, func(context.Context) HubResult {
		name := strings.TrimSpace(request.Name)
		if name == "" || len(name) > 100 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return HubResult{State: Failed, Reason: "Enter a name of at most 100 characters"}
		}
		if _, err := hubclient.ReadConfig(request.Config); err != nil {
			return HubResult{State: Failed, Reason: err.Error()}
		}
		if err := a.rememberHubTeam(request.Config, name); err != nil {
			return HubResult{State: Failed, Reason: err.Error()}
		}
		result := a.selectHubConfig(request.Config)
		if result.State != Completed {
			return result
		}
		result.Team = name
		return result
	})
}

type hubTeamDocument struct {
	Schema string `json:"schema"`
	Config string `json:"config"`
	Name   string `json:"name"`
}

// rememberHubTeam keeps the team's name for this configuration, in memory
// and, in a shell that remembers its selections, for the next window. A name
// the store cannot keep is refused, so the window never shows a name the next
// one will not.
func (a *App) rememberHubTeam(config, name string) error {
	document := hubTeamDocument{Schema: hubTeamSchema, Config: config, Name: name}
	if a.selections != nil {
		data, err := json.Marshal(document)
		if err == nil {
			err = a.documents.write(hubTeamName, append(data, '\n'))
		}
		if err != nil {
			return errors.New("the team's name cannot be kept; nothing was saved")
		}
	}
	a.hubTeamMu.Lock()
	a.hubTeam = document
	a.hubTeamMu.Unlock()
	return nil
}

// hubTeamLabel is the name remembered for the selected configuration, or
// empty when none was given for it. A remembered name that cannot be read is
// reported, never replaced until the person saves the team again.
func (a *App) hubTeamLabel(config string) (string, string) {
	if config == "" {
		return "", ""
	}
	a.hubTeamMu.Lock()
	held := a.hubTeam
	a.hubTeamMu.Unlock()
	if held.Config == "" && a.selections != nil {
		data, err := a.documents.read(hubTeamName, maxSelectionBytes)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return "", "the remembered team name cannot be read; edit the team to name it again"
		default:
			var document hubTeamDocument
			if json.Unmarshal(data, &document, json.RejectUnknownMembers(true)) != nil || document.Schema != hubTeamSchema {
				return "", "the remembered team name is not one this release reads; edit the team to name it again"
			}
			held = document
			a.hubTeamMu.Lock()
			a.hubTeam = document
			a.hubTeamMu.Unlock()
		}
	}
	if held.Config != config {
		return "", ""
	}
	return held.Name, ""
}

// openSignInPage opens the identity provider's sign-in page in the person's
// browser. A host without a browser opener leaves the page for the window's
// own link.
func (a *App) openSignInPage(address string) bool {
	opener, ok := a.chooser.(BrowserOpener)
	if !ok {
		return false
	}
	return opener.OpenBrowser(address) == nil
}

// HubTeamFile is one file a project links, named from what the hub verified
// about it.
type HubTeamFile struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
	// Name is what the project's own records call the file, such as the
	// resource a revision is of; empty when nothing does.
	Name string `json:"name,omitzero"`
	// Type is what the project's records say the file is.
	Type HubFileType `json:"type"`
	// AddedBy and AddedAt are who linked the file and when, where the hub
	// recorded that.
	AddedBy   string `json:"added_by,omitzero"`
	AddedAt   string `json:"added_at,omitzero"`
	KeepUntil string `json:"keep_until,omitzero"`
	Retired   bool   `json:"retired,omitzero"`
	// Resource is the revision history the file belongs to.
	Resource string `json:"resource,omitzero"`
}

// HubFileType is what a project's records say a file is.
type HubFileType string

const (
	FileTypeRevision       HubFileType = "revision"
	FileTypeTestRelease    HubFileType = "test-release"
	FileTypeEvidence       HubFileType = "evidence"
	FileTypeSharingPolicy  HubFileType = "sharing-policy"
	FileTypeSupportSummary HubFileType = "support-summary"
	FileTypeFile           HubFileType = "file"
)

// HubActivity is one recorded event, as Activity lists it.
type HubActivity struct {
	Key    string          `json:"key"`
	Actor  string          `json:"actor"`
	Action HubActivityKind `json:"action"`
	Object string          `json:"object,omitzero"`
	At     string          `json:"at"`
	Text   string          `json:"text,omitzero"`
	// ToMe says the event asks something of the signed-in person or answers
	// something they asked.
	ToMe bool `json:"to_me,omitzero"`
}

// HubActivityKind is what one recorded event did.
type HubActivityKind string

const (
	ActivityCommented        HubActivityKind = "comment"
	ActivityAssigned         HubActivityKind = "assignment"
	ActivityRequestedReview  HubActivityKind = "review-request"
	ActivityApproved         HubActivityKind = "approval"
	ActivityRequestedChanges HubActivityKind = "change-request"
	ActivityPublishedPolicy  HubActivityKind = "support-policy"
	ActivityRequestedSupport HubActivityKind = "support-request"
	ActivityApprovedSupport  HubActivityKind = "support-approval"
	ActivityRevised          HubActivityKind = "revision"
	ActivityResolved         HubActivityKind = "resolve"
	ActivityRemovedMember    HubActivityKind = "remove-user"
	ActivityRetention        HubActivityKind = "retention"
	ActivityRetired          HubActivityKind = "retire"
	ActivityAuditExport      HubActivityKind = "audit-export"
)

// HubReviewItem is one review request and where it stands.
type HubReviewItem struct {
	batch string
	ID    string `json:"id"`
	// Support is a support summary's approval request; otherwise a test
	// release's review.
	Support     bool            `json:"support,omitzero"`
	Item        string          `json:"item,omitzero"`
	Version     string          `json:"version,omitzero"`
	Suite       *ItemRef        `json:"suite,omitzero"`
	RequestedBy string          `json:"requested_by"`
	Recipient   string          `json:"recipient"`
	Requested   string          `json:"requested"`
	Updated     string          `json:"updated"`
	Status      HubReviewStatus `json:"status"`
	Reason      string          `json:"reason,omitzero"`
	Evidence    string          `json:"evidence"`
	Release     string          `json:"release"`
	// ToMe says the request asks the signed-in person.
	ToMe       bool               `json:"to_me,omitzero"`
	Discussion []HubReviewComment `json:"discussion"`
	// Requests are the hub's review requests this review is: one per test
	// release a suite version's request named, each answered on its own.
	Requests []HubReviewRequest `json:"requests"`
	// PolicyVersion is the sharing policy a support request was made under,
	// counted from the project's first, and PolicyCurrent whether it is still
	// the policy in force.
	PolicyVersion int  `json:"policy_version,omitzero"`
	PolicyCurrent bool `json:"policy_current,omitzero"`
}

// HubReviewRequest is one review request a review answers.
type HubReviewRequest struct {
	ID       string `json:"id"`
	Evidence string `json:"evidence"`
	Release  string `json:"release"`
}

// HubReviewStatus is where one review request stands.
type HubReviewStatus string

const (
	ReviewStatusRequested        HubReviewStatus = "requested"
	ReviewStatusApproved         HubReviewStatus = "approved"
	ReviewStatusChangesRequested HubReviewStatus = "changes-requested"
	ReviewStatusStale            HubReviewStatus = "stale"
)

// HubReviewComment is one recorded remark on a review: a comment, or the
// text a decision carried.
type HubReviewComment struct {
	Actor string          `json:"actor"`
	At    string          `json:"at"`
	Kind  HubActivityKind `json:"kind"`
	Text  string          `json:"text"`
}

// HubRevision is one recorded revision or resolution of a resource.
type HubRevision struct {
	ID       string   `json:"id"`
	Artifact string   `json:"artifact"`
	Actor    string   `json:"actor"`
	At       string   `json:"at"`
	Reason   string   `json:"reason"`
	Parents  []string `json:"parents"`
	Resolved bool     `json:"resolved,omitzero"`
}

// HubResource is one resource's revision history and its unresolved tips.
type HubResource struct {
	Resource  string        `json:"resource"`
	Revisions []HubRevision `json:"revisions"`
	Tips      []string      `json:"tips"`
}

// HubTeamResult is one project as the signed-in person may read it.
type HubTeamResult struct {
	State         State           `json:"state"`
	Reason        string          `json:"reason,omitzero"`
	Project       string          `json:"project,omitzero"`
	Me            string          `json:"me,omitzero"`
	Capabilities  []string        `json:"capabilities"`
	ReviewHead    int             `json:"review_head"`
	LifecycleHead int             `json:"lifecycle_head"`
	Activity      []HubActivity   `json:"activity"`
	Reviews       []HubReviewItem `json:"reviews"`
	Files         []HubTeamFile   `json:"files"`
	Resources     []HubResource   `json:"resources"`
}

func (r *HubTeamResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// HubTeamReadRequest names the project to read and the local project whose
// suite versions a review may be of.
type HubTeamReadRequest struct {
	Project   string `json:"project"`
	Workspace string `json:"workspace,omitzero"`
}

// ReadHubTeam reads one project through the signed-in session: its review
// and lifecycle logs and the files it links, all metadata. No artifact's
// bytes are read.
func (a *App) ReadHubTeam(request HubTeamReadRequest) HubTeamResult {
	return runNamed[HubTeamResult, *HubTeamResult](a, profiles["ReadHubTeam"], func(ctx context.Context) HubTeamResult {
		result := HubTeamResult{Project: request.Project, Capabilities: []string{}, Activity: []HubActivity{}, Reviews: []HubReviewItem{}, Files: []HubTeamFile{}, Resources: []HubResource{}}
		client, session, declined := a.signedIn()
		if declined.state != "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		for _, action := range []string{"evidence.read", "evidence.write", "approval", "export", "admin"} {
			if session.Allows(action) {
				result.Capabilities = append(result.Capabilities, action)
			}
		}
		result.Me = session.Subject
		history, err := client.ListHistory(ctx, request.Project)
		if err != nil {
			result.refuse(hubReadState(err), "the project's reviews cannot be read: "+err.Error())
			return result
		}
		lifecycle, err := client.GetLifecycle(ctx, request.Project)
		if err != nil {
			result.refuse(hubReadState(err), "the project's history cannot be read: "+err.Error())
			return result
		}
		files, err := client.ListFiles(ctx, request.Project)
		if err != nil {
			result.refuse(hubReadState(err), "the project's files cannot be read: "+err.Error())
			return result
		}
		versions := a.localReleaseVersions(ctx, request.Workspace)
		result.ReviewHead, result.LifecycleHead = history.Head, lifecycle.Head
		result.Files = teamFiles(files, history.Events, lifecycle, versions)
		names := map[string]string{}
		for _, file := range result.Files {
			names[file.Digest] = file.Name
		}
		result.Reviews = teamReviews(history.Events, session.Subject, versions)
		result.Activity = teamActivity(history.Events, lifecycle.Events, session.Subject, names)
		result.Resources = teamResources(lifecycle)
		result.State = Completed
		return result
	})
}

func hubReadState(err error) State {
	if errors.Is(err, hubclient.ErrAccessDenied) || errors.Is(err, hubclient.ErrExpired) {
		return PermissionDenied
	}
	return Failed
}

// localVersion is a suite version of the open project whose releases a team
// review names.
type localVersion struct {
	batch   string
	suite   ItemRef
	name    string
	version string
}

// localReleaseVersions maps each release digest the open project's suite
// approvals recorded onto the suite version that released it. A window with
// no open project, or one that cannot be read, matches nothing.
func (a *App) localReleaseVersions(ctx context.Context, workspace string) map[string]localVersion {
	versions := map[string]localVersion{}
	if workspace == "" {
		return versions
	}
	loaded, _ := a.loadCatalog(ctx, RequestContext{Project: workspace}, false)
	if loaded == nil {
		return versions
	}
	for _, item := range loaded.document.Items {
		if item.Kind != string(SuiteItem) || loaded.removed(item) {
			continue
		}
		for _, record := range loaded.suiteApprovals(item.ID) {
			if record.Scope != BaselineApproval && record.Scope != ReviewRequested {
				continue
			}
			commands := []string{}
			for _, event := range record.Hub {
				if event.Kind == "review-request" {
					commands = append(commands, event.Command)
				}
			}
			slices.Sort(commands)
			version := localVersion{suite: ItemRef{Kind: SuiteItem, ID: item.ID, Revision: record.Revision}, name: loaded.suiteName(item), version: record.Revision, batch: strings.Join(commands, ",")}
			for _, digest := range releaseDigests(record.Tests) {
				versions[digest] = version
			}
			for _, command := range commands {
				versions["request:"+command] = version
			}
		}
	}
	return versions
}

// teamFiles names each linked file from what the project's records say of
// it: a revision is named by its resource, a test release by the suite
// version that released it in the open project. Who added a file and when
// are the hub's own link record, never inferred.
func teamFiles(files hubprotocol.ProjectFiles, reviews []hubprotocol.ReviewEvent, lifecycle hubprotocol.LifecycleHistory, versions map[string]localVersion) []HubTeamFile {
	state := hubprotocol.DeriveLifecycle(lifecycle.Events)
	out := make([]HubTeamFile, 0, len(files.Files))
	for _, linked := range files.Files {
		file := HubTeamFile{Digest: linked.Digest, Size: linked.Size, Type: FileTypeFile, AddedBy: linked.Actor, AddedAt: linked.LinkedAt, Retired: state.Retired(linked.Digest)}
		for _, event := range lifecycle.Events {
			c := event.Command
			if c.Artifact != linked.Digest {
				continue
			}
			switch c.Kind {
			case "revision", "resolve":
				file.Type, file.Name, file.Resource = FileTypeRevision, c.Resource, c.Resource
			case "retention":
				file.KeepUntil = c.Until
			}
		}
		if file.Type == FileTypeFile {
			for _, event := range reviews {
				c := event.Command
				switch {
				case c.Kind == "support-policy" && c.Evidence == linked.Digest:
					file.Type = FileTypeSharingPolicy
				case c.Kind == "support-request" && c.Evidence == linked.Digest:
					file.Type = FileTypeSupportSummary
				case !hubprotocol.IsSupport(c) && c.Release == linked.Digest:
					file.Type = FileTypeTestRelease
				case !hubprotocol.IsSupport(c) && c.Evidence == linked.Digest && file.Type == FileTypeFile:
					file.Type = FileTypeEvidence
				}
			}
		}
		if version, held := versions[linked.Digest]; held && file.Name == "" {
			file.Name = version.name + " · Version " + version.version
		}
		out = append(out, file)
	}
	slices.SortStableFunc(out, func(x, y HubTeamFile) int { return cmp.Compare(y.AddedAt, x.AddedAt) })
	return out
}

// teamReviews derives each review request and where it stands: answered by
// an approval or a request for changes, stale once a later request for the
// same evidence replaced it (or, for a support summary, once another sharing
// policy was published), and requested otherwise.
func teamReviews(events []hubprotocol.ReviewEvent, me string, versions map[string]localVersion) []HubReviewItem {
	state := hubprotocol.DeriveReviews(events)
	items := []HubReviewItem{}
	policies := map[string]int{}
	for _, event := range events {
		if event.Command.Kind == "support-policy" {
			policies[event.Command.ID] = len(policies) + 1
		}
	}
	for i, event := range events {
		c := event.Command
		support := hubprotocol.IsSupportRequest(c)
		if c.Kind != "review-request" && !support {
			continue
		}
		item := HubReviewItem{ID: c.ID, Support: support, RequestedBy: event.Actor, Recipient: c.Recipient, Requested: event.At, Updated: event.At,
			Status: ReviewStatusRequested, Evidence: c.Evidence, Release: c.Release, ToMe: c.Recipient == me, Discussion: []HubReviewComment{},
			Requests: []HubReviewRequest{{ID: c.ID, Evidence: c.Evidence, Release: c.Release}}}
		if support {
			item.Item = "Support summary"
			item.PolicyVersion, item.PolicyCurrent = policies[c.Parent], state.Current(c)
		} else if version, held := versions["request:"+c.ID]; held {
			suite := version.suite
			item.Item, item.Version, item.Suite = version.name, version.version, &suite
			item.batch = version.batch
		}
		if !support && c.Text != "" {
			item.Reason = c.Text
		}
		for _, later := range events[i+1:] {
			l := later.Command
			switch {
			case l.Parent == c.ID && (l.Kind == "approval" || hubprotocol.IsSupportApproval(l)):
				item.Status, item.Updated = ReviewStatusApproved, later.At
				if !support {
					item.Discussion = append(item.Discussion, HubReviewComment{Actor: later.Actor, At: later.At, Kind: ActivityApproved, Text: l.Text})
				}
			case l.Parent == c.ID && l.Kind == "change-request":
				item.Status, item.Updated = ReviewStatusChangesRequested, later.At
				item.Discussion = append(item.Discussion, HubReviewComment{Actor: later.Actor, At: later.At, Kind: ActivityRequestedChanges, Text: l.Text})
			case l.Parent == c.ID && l.Kind == "comment":
				item.Updated = later.At
				item.Discussion = append(item.Discussion, HubReviewComment{Actor: later.Actor, At: later.At, Kind: ActivityCommented, Text: l.Text})
			case item.Status == ReviewStatusRequested && !support && l.Kind == "review-request" && l.Evidence == c.Evidence:
				item.Status = ReviewStatusStale
			}
		}
		if support && item.Status == ReviewStatusRequested && !state.Current(c) {
			item.Status = ReviewStatusStale
		}
		items = append(items, item)
	}
	items = groupSuiteRequests(items)
	slices.SortStableFunc(items, func(x, y HubReviewItem) int { return cmp.Compare(y.Updated, x.Updated) })
	return items
}

// groupSuiteRequests shows the requests one suite version's review made —
// one per test release, by the same person of the same reviewer with the same
// reason — as one review, which stands where its least settled request does.
func groupSuiteRequests(items []HubReviewItem) []HubReviewItem {
	out := []HubReviewItem{}
	at := map[string]int{}
	rank := map[HubReviewStatus]int{ReviewStatusApproved: 0, ReviewStatusRequested: 1, ReviewStatusStale: 2, ReviewStatusChangesRequested: 3}
	for _, item := range items {
		if item.Suite == nil {
			out = append(out, item)
			continue
		}
		key := strings.Join([]string{item.RequestedBy, item.Recipient, item.Reason, item.Suite.ID, item.Version, item.batch}, "\x00")
		index, held := at[key]
		if !held {
			at[key] = len(out)
			out = append(out, item)
			continue
		}
		group := &out[index]
		group.Requests = append(group.Requests, item.Requests...)
		group.Discussion = append(group.Discussion, item.Discussion...)
		group.Updated = max(group.Updated, item.Updated)
		if rank[item.Status] > rank[group.Status] {
			group.Status = item.Status
		}
	}
	for i := range out {
		slices.SortStableFunc(out[i].Discussion, func(x, y HubReviewComment) int { return cmp.Compare(x.At, y.At) })
	}
	return out
}

// teamActivity is every recorded event, newest first, with what it did and
// to what.
func teamActivity(reviews []hubprotocol.ReviewEvent, lifecycle []hubprotocol.LifecycleEvent, me string, names map[string]string) []HubActivity {
	requesters := map[string]string{}
	for _, event := range reviews {
		requesters[event.Command.ID] = event.Actor
	}
	object := func(digest string) string {
		if name := names[digest]; name != "" {
			return name
		}
		return ""
	}
	out := make([]HubActivity, 0, len(reviews)+len(lifecycle))
	for _, event := range reviews {
		c := event.Command
		text := c.Text
		if hubprotocol.IsSupport(c) {
			text = ""
		}
		toMe := c.Recipient == me || (c.Parent != "" && requesters[c.Parent] == me && event.Actor != me)
		out = append(out, HubActivity{Key: "review-" + strconv.Itoa(event.Sequence), Actor: event.Actor, Action: HubActivityKind(c.Kind), Object: object(cmp.Or(c.Release, c.Evidence)), At: event.At, Text: text, ToMe: toMe})
	}
	for _, event := range lifecycle {
		c := event.Command
		target := c.Resource
		switch c.Kind {
		case "remove-user":
			target = c.Subject
		case "retention", "retire":
			target = object(c.Artifact)
		}
		out = append(out, HubActivity{Key: "history-" + strconv.Itoa(event.Sequence), Actor: event.Actor, Action: HubActivityKind(c.Kind), Object: target, At: event.At, Text: c.Reason})
	}
	slices.SortStableFunc(out, func(x, y HubActivity) int { return cmp.Compare(y.At, x.At) })
	return out
}

// teamResources is each resource's revisions, oldest first, and its
// unresolved tips.
func teamResources(lifecycle hubprotocol.LifecycleHistory) []HubResource {
	byName := map[string]*HubResource{}
	order := []string{}
	for _, event := range lifecycle.Events {
		c := event.Command
		if c.Kind != "revision" && c.Kind != "resolve" {
			continue
		}
		resource, held := byName[c.Resource]
		if !held {
			resource = &HubResource{Resource: c.Resource, Revisions: []HubRevision{}, Tips: []string{}}
			byName[c.Resource] = resource
			order = append(order, c.Resource)
		}
		resource.Revisions = append(resource.Revisions, HubRevision{ID: c.ID, Artifact: c.Artifact, Actor: event.Actor, At: event.At, Reason: c.Reason, Parents: slices.Clone(c.Parents), Resolved: c.Kind == "resolve"})
	}
	out := []HubResource{}
	for _, name := range order {
		resource := byName[name]
		resource.Tips = append(resource.Tips, lifecycle.Tips[name]...)
		out = append(out, *resource)
	}
	return out
}

// HubMembersResult is a project's members as the hub reads them now.
type HubMembersResult struct {
	State   State       `json:"state"`
	Reason  string      `json:"reason,omitzero"`
	Project string      `json:"project,omitzero"`
	Members []HubMember `json:"members"`
}

func (r *HubMembersResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// HubMember is one person a project's access reaches.
type HubMember struct {
	Subject string `json:"subject"`
	Role    string `json:"role,omitzero"`
	Removed bool   `json:"removed,omitzero"`
}

// ListHubMembers reads the project's members, which the hub answers its
// administrators only.
func (a *App) ListHubMembers(project string) HubMembersResult {
	return runNamed[HubMembersResult, *HubMembersResult](a, profiles["ListHubMembers"], func(ctx context.Context) HubMembersResult {
		return a.hubMembers(ctx, project, false)
	})
}

// ListHubReviewers reads the active members a review request may ask.
func (a *App) ListHubReviewers(project string) HubMembersResult {
	return runNamed[HubMembersResult, *HubMembersResult](a, profiles["ListHubReviewers"], func(ctx context.Context) HubMembersResult {
		return a.hubMembers(ctx, project, true)
	})
}

func (a *App) hubMembers(ctx context.Context, project string, reviewers bool) HubMembersResult {
	result := HubMembersResult{Project: project, Members: []HubMember{}}
	client, session, declined := a.signedIn()
	if declined.state != "" {
		result.refuse(declined.state, declined.reason)
		return result
	}
	read := client.ListMembers
	if reviewers {
		read = client.ListReviewers
	}
	members, err := read(ctx, project)
	if err != nil {
		result.refuse(hubReadState(err), err.Error())
		return result
	}
	for _, member := range members.Members {
		if reviewers && member.Subject == session.Subject {
			continue
		}
		result.Members = append(result.Members, HubMember{Subject: member.Subject, Role: member.Role, Removed: member.Status == hubprotocol.MemberRemoved})
	}
	result.State = Completed
	return result
}

// HubFileRequest names one file of a project.
type HubFileRequest struct {
	Project string `json:"project"`
	Digest  string `json:"digest"`
	// Name is the name the save dialog offers.
	Name string `json:"name,omitzero"`
}

// DownloadHubFile saves one project file's verified bytes as a new file the
// person names first. Nothing is written until the bytes match their digest,
// and an existing file is never replaced. A failed or cancelled transfer
// writes nothing and is never repeated on its own.
func (a *App) DownloadHubFile(request HubFileRequest) HubTransferResult {
	return runNamed[HubTransferResult, *HubTransferResult](a, profiles["DownloadHubFile"], func(ctx context.Context) HubTransferResult {
		client, _, declined := a.signedIn()
		if declined.state != "" {
			return HubTransferResult{State: declined.state, Reason: declined.reason}
		}
		destination, declined := a.newDestination(ctx, "Download "+cmp.Or(request.Name, "file"), cmp.Or(request.Name, "file"))
		if destination == "" {
			return HubTransferResult{State: declined.state, Reason: declined.reason}
		}
		data, warning, err := client.ReadArtifact(ctx, request.Project, request.Digest)
		if err != nil {
			return HubTransferResult{State: hubReadState(err), Reason: err.Error(), TransferState: "failed"}
		}
		return writeDownload(destination, data, request.Digest, warning)
	})
}

// DownloadHubSummary saves an approved support summary — only the approved
// value-free summary the hub serves under its approval chain, never a packet
// or private state — as a new file the person names first.
func (a *App) DownloadHubSummary(request HubFileRequest) HubTransferResult {
	return runNamed[HubTransferResult, *HubTransferResult](a, profiles["DownloadHubSummary"], func(ctx context.Context) HubTransferResult {
		client, _, declined := a.signedIn()
		if declined.state != "" {
			return HubTransferResult{State: declined.state, Reason: declined.reason}
		}
		destination, declined := a.newDestination(ctx, "Download summary", "support-summary.json")
		if destination == "" {
			return HubTransferResult{State: declined.state, Reason: declined.reason}
		}
		data, warning, err := client.ReadExport(ctx, request.Project, request.Digest)
		if err != nil {
			return HubTransferResult{State: hubReadState(err), Reason: err.Error(), TransferState: "failed"}
		}
		return writeDownload(destination, data, request.Digest, warning)
	})
}

// newDestination is a new file the person names in the save dialog: a name
// already there is refused before anything is read.
func (a *App) newDestination(ctx context.Context, title, name string) (string, refusal) {
	named, declined := a.chooseNamedDestination(ctx, title, name)
	if named == "" {
		if declined.state == Cancelled {
			declined.reason = "no file was named"
		}
		return "", declined
	}
	destination, err := artifactpath.Destination(named)
	if err != nil {
		return "", refusal{Failed, err.Error()}
	}
	if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
		return "", refusal{Failed, "a file is already there; name a new file"}
	}
	return destination, noRefusal
}

func writeDownload(destination string, data []byte, digest, warning string) HubTransferResult {
	if err := operation.WriteNewFile(destination, data, "cannot create the file; nothing was written", "cannot write the file; nothing was kept"); err != nil {
		return HubTransferResult{State: Failed, Reason: err.Error(), TransferState: "failed"}
	}
	return HubTransferResult{State: Completed, TransferState: "completed", Digest: digest, Size: int64(len(data)), Path: destination, Warning: warning}
}

// HubSetupExport is one setup file an administrator task hands to the host
// operator: the name the save dialog offers and its exact contents.
type HubSetupExport struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ExportHubSetup writes an administrator task's setup — a prepared access
// policy or a reviewed host command — to a new owner-only file the person
// names. It runs nothing and contacts nothing.
func (a *App) ExportHubSetup(request HubSetupExport) HubTransferResult {
	return run(a, false, false, func(ctx context.Context) HubTransferResult {
		if request.Content == "" || len(request.Content) > 1<<20 {
			return HubTransferResult{State: Failed, Reason: "there is no setup to export"}
		}
		destination, declined := a.newDestination(ctx, "Export setup", cmp.Or(filepath.Base(request.Name), "setup.txt"))
		if destination == "" {
			return HubTransferResult{State: declined.state, Reason: declined.reason}
		}
		if err := operation.WriteNewFile(destination, []byte(request.Content), "cannot create the setup file; nothing was written", "cannot write the setup file; nothing was kept"); err != nil {
			return HubTransferResult{State: Failed, Reason: err.Error()}
		}
		sum := sha256.Sum256([]byte(request.Content))
		return HubTransferResult{State: Completed, TransferState: "completed", Digest: hex.EncodeToString(sum[:]), Size: int64(len(request.Content)), Path: destination}
	})
}

// ChooseHubLocalCopy opens the host's dialog for a local copy an
// administrator task reads: a file for a configuration, access policy,
// operation or schedule policy or test, and a folder for a backup.
func (a *App) ChooseHubLocalCopy(kind string) PathChoiceResult {
	return run(a, true, false, func(ctx context.Context) PathChoiceResult {
		titles := map[string]string{
			"config": "Choose a copy of the hub configuration", "access-policy": "Choose a copy of the access policy",
			"operation-policy": "Choose a copy of the operation policy", "schedule-policy": "Choose a copy of the schedule policy",
			"test": "Choose a copy of the test", "backup": "Choose a copy of the backup folder",
			"file": "Choose a file",
		}
		title, known := titles[kind]
		if !known {
			return PathChoiceResult{State: Failed, Reason: "unknown local copy"}
		}
		if kind == "backup" {
			folder, declined := a.chooseFolder(ctx, title)
			if folder == "" {
				return PathChoiceResult{State: declined.state, Reason: declined.reason}
			}
			return PathChoiceResult{State: Completed, Kind: kind, Paths: []string{folder}}
		}
		files, declined := a.chooseFiles(ctx, title, "All files (*.*)", "*.*")
		if len(files) == 0 {
			return PathChoiceResult{State: declined.state, Reason: declined.reason}
		}
		return PathChoiceResult{State: Completed, Kind: kind, Paths: files[:1]}
	})
}

// RetentionScope is which of a project's current files a retention change
// covers.
type RetentionScope string

const (
	RetentionAllFiles RetentionScope = "all"
	RetentionOfType   RetentionScope = "type"
	RetentionSelected RetentionScope = "selected"
)

// HubRetentionRequest is a retention change: keep the files in scope for a
// duration from now.
type HubRetentionRequest struct {
	Project string         `json:"project"`
	Count   int            `json:"count"`
	Unit    string         `json:"unit"`
	Scope   RetentionScope `json:"scope"`
	Type    HubFileType    `json:"type,omitzero"`
	Digests []string       `json:"digests,omitzero"`
}

// HubRetentionRow is one current file a retention change covers: its
// current and proposed keep-until, and what the change does to it.
type HubRetentionRow struct {
	Digest   string          `json:"digest"`
	Name     string          `json:"name,omitzero"`
	Type     HubFileType     `json:"type"`
	Current  string          `json:"current,omitzero"`
	Proposed string          `json:"proposed,omitzero"`
	Change   RetentionChange `json:"change"`
	Reason   string          `json:"reason,omitzero"`
}

// RetentionChange is what one retention change does, or did, to one file.
type RetentionChange string

const (
	// RetentionExtends: the file is kept until the proposed date.
	RetentionExtends RetentionChange = "extends"
	// RetentionUnchanged: the file is already kept at least that long; a
	// retention date is never shortened.
	RetentionUnchanged RetentionChange = "unchanged"
	// RetentionRetiredFile: the file was retired and takes no retention.
	RetentionRetiredFile RetentionChange = "retired"
	// RetentionApplied: the new date was recorded.
	RetentionApplied RetentionChange = "applied"
	// RetentionFailed: the new date was not recorded.
	RetentionFailed RetentionChange = "failed"
)

// HubRetentionResult is a retention change resolved to the project's
// current files, or what applying it recorded, file by file.
type HubRetentionResult struct {
	State  State             `json:"state"`
	Reason string            `json:"reason,omitzero"`
	Until  string            `json:"until,omitzero"`
	Rows   []HubRetentionRow `json:"rows"`
}

func (r *HubRetentionResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// PreviewHubRetention resolves a retention change to the project's current
// files: each one's current and proposed keep-until. Files added later are
// not covered. Nothing is recorded.
func (a *App) PreviewHubRetention(request HubRetentionRequest) HubRetentionResult {
	return runNamed[HubRetentionResult, *HubRetentionResult](a, profiles["PreviewHubRetention"], func(ctx context.Context) HubRetentionResult {
		result, _, _ := a.planRetention(ctx, request)
		return result
	})
}

// HubRetentionApply is a reviewed retention change: the change, the files
// its review showed extending, the reason, and the click's intent, which
// names each file's command so a retry records nothing twice.
type HubRetentionApply struct {
	Request  HubRetentionRequest `json:"request"`
	Until    string              `json:"until"`
	Digests  []string            `json:"digests"`
	Reason   string              `json:"reason"`
	IntentID string              `json:"intent_id"`
}

// ApplyHubRetention records one retention command per file the review
// showed extending, against the history as it stands now: a file whose date
// would shorten is left unchanged, and each file's outcome is its own — a
// batch where only some commands landed says which. It deletes and retires
// nothing, and reaches no copy already downloaded.
func (a *App) ApplyHubRetention(request HubRetentionApply) HubRetentionResult {
	return runNamed[HubRetentionResult, *HubRetentionResult](a, profiles["ApplyHubRetention"], func(ctx context.Context) HubRetentionResult {
		result := HubRetentionResult{Rows: []HubRetentionRow{}, Until: request.Until}
		if !catalog.ValidToken(request.IntentID) {
			result.refuse(Failed, "a retention change carries the identity of the click that made it")
			return result
		}
		if strings.TrimSpace(request.Reason) == "" {
			result.refuse(Failed, "Enter why the files are kept")
			return result
		}
		until, err := time.Parse(time.RFC3339, request.Until)
		if err != nil {
			result.refuse(Failed, "the reviewed date cannot be read")
			return result
		}
		client, _, declined := a.signedIn()
		if declined.state != "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		project := request.Request.Project
		history, err := client.GetLifecycle(ctx, project)
		if err != nil {
			result.refuse(hubReadState(err), err.Error())
			return result
		}
		head := history.Head
		failed := 0
		for _, digest := range request.Digests {
			row := HubRetentionRow{Digest: digest, Proposed: request.Until}
			id := retentionCommandID(request.IntentID, digest)
			command := hubprotocol.LifecycleCommand{Schema: hubprotocol.LifecycleCommandSchema, ID: id, Kind: "retention", Artifact: digest, Parents: []string{}, Until: request.Until, Reason: strings.TrimSpace(request.Reason)}
			for attempt := 0; ; attempt++ {
				// Each file is decided against the history as it stands now:
				// already recorded by this click, retired, or kept at least as
				// long already is left as it is; a date is never shortened.
				row.Current = keepUntil(history.Events, digest)
				switch {
				case recordedCommand(history.Events, id):
					row.Change = RetentionApplied
				case hubprotocol.DeriveLifecycle(history.Events).Retired(digest):
					row.Change = RetentionRetiredFile
				case row.Current != "" && !laterThan(until, row.Current):
					row.Change = RetentionUnchanged
				default:
					command.Expected = head
					written, err := client.PostLifecycle(ctx, project, command)
					if errors.Is(err, hubclient.ErrConflict) && attempt == 0 {
						// Another write moved the history: read it again and
						// decide this file against it once more.
						if fresh, readErr := client.GetLifecycle(ctx, project); readErr == nil {
							history, head = fresh, fresh.Head
							continue
						}
					}
					if err != nil {
						row.Change, row.Reason = RetentionFailed, err.Error()
						failed++
						break
					}
					row.Change = RetentionApplied
					if written.Event != nil {
						head = written.Event.Sequence
						history.Events = append(history.Events, *written.Event)
					}
				}
				break
			}
			result.Rows = append(result.Rows, row)
		}
		result.State = Completed
		if failed > 0 {
			result.refuse(Failed, strconv.Itoa(failed)+" of "+strconv.Itoa(len(request.Digests))+" files were not changed")
		}
		return result
	})
}

// planRetention resolves a retention change's scope to the project's current
// files and says what the change does to each.
func (a *App) planRetention(ctx context.Context, request HubRetentionRequest) (HubRetentionResult, []HubTeamFile, time.Time) {
	result := HubRetentionResult{Rows: []HubRetentionRow{}}
	if request.Count < 1 || request.Count > 1000 {
		result.refuse(Failed, "Enter how long to keep the files")
		return result, nil, time.Time{}
	}
	now := a.now().UTC().Truncate(time.Second)
	var until time.Time
	switch request.Unit {
	case "days":
		until = now.AddDate(0, 0, request.Count)
	case "months":
		until = now.AddDate(0, request.Count, 0)
	case "years":
		until = now.AddDate(request.Count, 0, 0)
	default:
		result.refuse(Failed, "Choose days, months or years")
		return result, nil, time.Time{}
	}
	client, _, declined := a.signedIn()
	if declined.state != "" {
		result.refuse(declined.state, declined.reason)
		return result, nil, until
	}
	history, err := client.ListHistory(ctx, request.Project)
	if err != nil {
		result.refuse(hubReadState(err), err.Error())
		return result, nil, until
	}
	lifecycle, err := client.GetLifecycle(ctx, request.Project)
	if err != nil {
		result.refuse(hubReadState(err), err.Error())
		return result, nil, until
	}
	linked, err := client.ListFiles(ctx, request.Project)
	if err != nil {
		result.refuse(hubReadState(err), err.Error())
		return result, nil, until
	}
	files := teamFiles(linked, history.Events, lifecycle, map[string]localVersion{})
	result.Until = until.Format(time.RFC3339)
	for _, file := range files {
		switch request.Scope {
		case RetentionAllFiles:
		case RetentionOfType:
			if file.Type != request.Type {
				continue
			}
		case RetentionSelected:
			if !slices.Contains(request.Digests, file.Digest) {
				continue
			}
		default:
			result.refuse(Failed, "Choose which files are kept")
			return result, nil, until
		}
		row := HubRetentionRow{Digest: file.Digest, Name: file.Name, Type: file.Type, Current: file.KeepUntil, Proposed: result.Until, Change: RetentionExtends}
		switch {
		case file.Retired:
			row.Change, row.Proposed = RetentionRetiredFile, ""
		case file.KeepUntil != "" && !laterThan(until, file.KeepUntil):
			row.Change, row.Proposed = RetentionUnchanged, ""
		}
		result.Rows = append(result.Rows, row)
	}
	if len(result.Rows) == 0 {
		result.refuse(Empty, "No current files match")
		return result, files, until
	}
	result.State = Completed
	return result, files, until
}

// recordedCommand reports whether the history records a command by its id.
func recordedCommand(events []hubprotocol.LifecycleEvent, id string) bool {
	for _, event := range events {
		if event.Command.ID == id {
			return true
		}
	}
	return false
}

// keepUntil is the latest retention date the history records for a file.
func keepUntil(events []hubprotocol.LifecycleEvent, digest string) string {
	until := ""
	for _, event := range events {
		if event.Command.Kind == "retention" && event.Command.Artifact == digest {
			until = event.Command.Until
		}
	}
	return until
}

// laterThan reports whether until is after the recorded date. A recorded
// date that cannot be read is never treated as earlier.
func laterThan(until time.Time, recorded string) bool {
	at, err := time.Parse(time.RFC3339Nano, recorded)
	return err == nil && until.After(at)
}

// retentionCommandID is the command one click records for one file, so a
// retry of the same click finds what it already recorded.
func retentionCommandID(intent, digest string) string {
	sum := sha256.Sum256([]byte(intent + "\x00" + digest))
	return "retention-" + hex.EncodeToString(sum[:])[:40]
}

// HubSupportSummaryResult is a support summary under review, as its
// value-free fields say: where it came from and how the run ended.
type HubSupportSummaryResult struct {
	State      State  `json:"state"`
	Reason     string `json:"reason,omitzero"`
	SourceKind string `json:"source_kind,omitzero"`
	Outcome    string `json:"outcome,omitzero"`
}

func (r *HubSupportSummaryResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ReadHubSupportSummary reads the support summary a review names — a
// deliberate read of that one value-free document, verified against its
// digest and decoded by the sharing reader — so the reviewer sees what they
// approve. It reads no packet or private state.
func (a *App) ReadHubSupportSummary(request HubFileRequest) HubSupportSummaryResult {
	return runNamed[HubSupportSummaryResult, *HubSupportSummaryResult](a, profiles["ReadHubSupportSummary"], func(ctx context.Context) HubSupportSummaryResult {
		client, _, declined := a.signedIn()
		if declined.state != "" {
			return HubSupportSummaryResult{State: declined.state, Reason: declined.reason}
		}
		data, _, err := client.ReadArtifact(ctx, request.Project, request.Digest)
		if err != nil {
			return HubSupportSummaryResult{State: hubReadState(err), Reason: err.Error()}
		}
		summary, err := sharing.Decode(data)
		if err != nil {
			return HubSupportSummaryResult{State: Failed, Reason: "the file is not a support summary"}
		}
		return HubSupportSummaryResult{State: Completed, SourceKind: summary.SourceKind, Outcome: summary.Outcome}
	})
}
