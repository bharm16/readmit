package desktop_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hubprotocol"
	"github.com/bharm16/readmit/internal/sharing"
)

// teamHub is a customer hub's team routes as the desktop reads and writes
// them: one project's review and lifecycle logs, its linked files and who
// linked them, its members, and a count of every artifact download, so a
// test can tell a metadata read from a payload transfer.
type teamHub struct {
	mu                         sync.Mutex
	actor                      string
	reviews                    []hubprotocol.ReviewEvent
	lifecycle                  []hubprotocol.LifecycleEvent
	files                      map[string][]byte
	origins                    map[string]hubprotocol.ProjectFile
	downloads                  int
	uploads                    int
	refuse                     map[string]bool
	members                    []hubprotocol.ProjectMember
	malformedRevisionReply     bool
	wrongSequenceRevisionReply bool
}

const teamProject = "cardio-study"

func digestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func newTeamHub() *teamHub {
	return &teamHub{files: map[string][]byte{}, origins: map[string]hubprotocol.ProjectFile{}, refuse: map[string]bool{},
		members: []hubprotocol.ProjectMember{{Subject: "ana", Role: "analyst", Status: "active"}, {Subject: "rui", Role: "reviewer", Status: "active"}, {Subject: "old", Role: "viewer", Status: "removed"}}}
}

// link stores bytes as linked by actor, or with no recorded origin.
func (h *teamHub) link(data []byte, actor, at string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	digest := digestHex(data)
	h.files[digest] = data
	file := hubprotocol.ProjectFile{Digest: digest, Size: int64(len(data))}
	if actor != "" {
		file.Issuer, file.Actor, file.LinkedAt = "https://idp.hospital.org", actor, at
	}
	h.origins[digest] = file
	return digest
}

func (h *teamHub) review(actor, at string, c hubprotocol.ReviewCommand) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.Schema == "" {
		c.Schema, _ = hubprotocol.CommandSchema(c.Kind)
	}
	h.reviews = append(h.reviews, hubprotocol.ReviewEvent{Schema: hubprotocol.EventSchema(c), Project: teamProject, Sequence: len(h.reviews) + 1, Issuer: "https://idp.hospital.org", Actor: actor, At: at, Command: c})
}

func (h *teamHub) record(actor, at string, c hubprotocol.LifecycleCommand) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c.Schema = hubprotocol.LifecycleCommandSchema
	if c.Parents == nil {
		c.Parents = []string{}
	}
	h.lifecycle = append(h.lifecycle, hubprotocol.LifecycleEvent{Schema: hubprotocol.LifecycleEventSchema, Project: teamProject, Sequence: len(h.lifecycle) + 1, Issuer: "https://idp.hospital.org", Actor: actor, At: at, Command: c})
}

func (h *teamHub) mux() *http.ServeMux {
	mux := http.NewServeMux()
	send := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.MarshalWrite(w, v)
	}
	base := "/v2/projects/" + teamProject
	for _, probe := range []string{"/health/live", "/health/ready"} {
		mux.HandleFunc(probe, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	}
	mux.HandleFunc(base+"/history", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		send(w, 200, hubprotocol.ReviewHistory{Schema: hubprotocol.ReviewHistoryV1, Head: len(h.reviews), Events: slices.Clone(h.reviews)})
	})
	mux.HandleFunc(base+"/reviews", func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		c, err := hubprotocol.DecodeReviewCommand(data)
		if err != nil {
			http.Error(w, "invalid", 400)
			return
		}
		h.mu.Lock()
		if c.Expected != len(h.reviews) {
			h.mu.Unlock()
			http.Error(w, "stale", 409)
			return
		}
		h.mu.Unlock()
		h.review(h.actor, time.Now().UTC().Format(time.RFC3339Nano), c)
		h.mu.Lock()
		event := h.reviews[len(h.reviews)-1]
		h.mu.Unlock()
		send(w, 201, event)
	})
	mux.HandleFunc(base+"/lifecycle", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if r.Method == "GET" {
			send(w, 200, hubprotocol.LifecycleHistory{Schema: hubprotocol.LifecycleHistorySchema, Head: len(h.lifecycle), Events: slices.Clone(h.lifecycle),
				Tips: hubprotocol.DeriveLifecycle(h.lifecycle).Tips(), Warning: hubprotocol.CustodyWarning})
			return
		}
		data, _ := io.ReadAll(r.Body)
		c, err := hubprotocol.DecodeLifecycleCommand(data)
		if err != nil {
			http.Error(w, "invalid", 400)
			return
		}
		for _, event := range h.lifecycle {
			if event.Command.ID == c.ID {
				send(w, 200, event)
				return
			}
		}
		if h.refuse[c.Artifact] {
			http.Error(w, "refused", 403)
			return
		}
		if c.Expected != len(h.lifecycle) {
			http.Error(w, "stale", 409)
			return
		}
		h.lifecycle = append(h.lifecycle, hubprotocol.LifecycleEvent{Schema: hubprotocol.LifecycleEventSchema, Project: teamProject, Sequence: len(h.lifecycle) + 1,
			Issuer: "https://idp.hospital.org", Actor: h.actor, At: time.Now().UTC().Format(time.RFC3339Nano), Command: c})
		if h.malformedRevisionReply {
			send(w, 201, struct{}{})
			return
		}
		if h.wrongSequenceRevisionReply {
			wrong := h.lifecycle[len(h.lifecycle)-1]
			wrong.Sequence = 999
			send(w, 201, wrong)
			return
		}
		send(w, 201, h.lifecycle[len(h.lifecycle)-1])
	})
	mux.HandleFunc(base+"/files", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		files := []hubprotocol.ProjectFile{}
		for _, file := range h.origins {
			files = append(files, file)
		}
		slices.SortFunc(files, func(x, y hubprotocol.ProjectFile) int { return strings.Compare(x.Digest, y.Digest) })
		send(w, 200, hubprotocol.ProjectFiles{Schema: hubprotocol.ProjectFilesSchema, Project: teamProject, Files: files})
	})
	members := func(reviewers bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if !reviewers && h.actor != "ana" {
				http.Error(w, "access refused", 403)
				return
			}
			out := hubprotocol.ProjectMembers{Schema: hubprotocol.ProjectMembersSchema, Project: teamProject, Issuer: "https://idp.hospital.org", Members: []hubprotocol.ProjectMember{}}
			for _, member := range h.members {
				if !reviewers || member.Role == "reviewer" && member.Status == "active" {
					out.Members = append(out.Members, member)
				}
			}
			send(w, 200, out)
		}
	}
	mux.HandleFunc(base+"/exports/", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.downloads++
		data, held := h.files[strings.TrimPrefix(r.URL.Path, base+"/exports/")]
		if !held {
			http.Error(w, "exact authenticated support approval required", 403)
			return
		}
		w.Header().Set(hubprotocol.CustodyHeader, hubprotocol.CustodyWarning)
		_, _ = w.Write(data)
	})
	mux.HandleFunc(base+"/members", members(false))
	mux.HandleFunc(base+"/reviewers", members(true))
	mux.HandleFunc("/v1/projects/"+teamProject+"/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		digest := strings.TrimPrefix(r.URL.Path, "/v1/projects/"+teamProject+"/artifacts/")
		if r.Method == "PUT" {
			data, _ := io.ReadAll(r.Body)
			if digestHex(data) != digest {
				http.Error(w, "mismatch", 422)
				return
			}
			h.mu.Lock()
			h.uploads++
			h.mu.Unlock()
			h.link(data, h.actor, time.Now().UTC().Format(time.RFC3339Nano))
			w.WriteHeader(201)
			return
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		h.downloads++
		data, held := h.files[digest]
		if !held {
			http.NotFound(w, r)
			return
		}
		w.Header().Set(hubprotocol.CustodyHeader, hubprotocol.CustodyWarning)
		_, _ = w.Write(data)
	})
	return mux
}

func signedInTeam(t *testing.T, hub *teamHub, subject string, scopes []string) hubAuthFixture {
	t.Helper()
	hub.actor = subject
	return newAuthenticatedHubApp(t, hub.mux(), subject, scopes)
}

var writerScopes = []string{"evidence.read", "evidence.write", "approval", "export", "admin"}

// Opening a project reads its logs and file list only: files are named and
// typed from the project's own records, who added them is the hub's link
// record or nothing, reviews stand where their answers put them, and no
// artifact's bytes are fetched.
func TestTeamReadsProjectMetadataWithoutTransferringFileBytes(t *testing.T) {
	hub := newTeamHub()
	revision := hub.link([]byte("appointment rules v1\n"), "ana", "2026-09-01T10:00:00Z")
	unrecorded := hub.link([]byte("older upload"), "", "")
	release := hub.link([]byte(`{"release":"one"}`), "ana", "2026-09-02T10:00:00Z")
	later := hub.link([]byte(`{"release":"two"}`), "ana", "2026-09-03T10:00:00Z")
	hub.record("ana", "2026-09-01T10:00:01Z", hubprotocol.LifecycleCommand{ID: "rev-1", Kind: "revision", Resource: "booking-rules", Artifact: revision, Reason: "first"})
	hub.record("ana", "2026-09-01T10:00:02Z", hubprotocol.LifecycleCommand{ID: "keep-1", Kind: "retention", Artifact: revision, Until: "2027-01-01T00:00:00Z", Reason: "audit"})
	hub.review("ana", "2026-09-02T11:00:00Z", hubprotocol.ReviewCommand{ID: "ask-1", Kind: "review-request", Evidence: release, Recipient: "rui", Release: release, Text: "Reschedule keeps one appointment"})
	hub.review("rui", "2026-09-02T12:00:00Z", hubprotocol.ReviewCommand{ID: "changes-1", Kind: "change-request", Evidence: release, Parent: "ask-1", Release: release, Text: "Timeout too short"})
	hub.review("ana", "2026-09-03T11:00:00Z", hubprotocol.ReviewCommand{ID: "ask-2", Kind: "review-request", Evidence: later, Recipient: "rui", Release: later, Text: "Second try"})
	hub.review("ana", "2026-09-03T11:30:00Z", hubprotocol.ReviewCommand{ID: "ask-3", Kind: "review-request", Evidence: later, Recipient: "rui", Release: later, Text: "Third try"})
	fixture := signedInTeam(t, hub, "rui", writerScopes)

	team := fixture.app.ReadHubTeam(desktop.HubTeamReadRequest{Project: teamProject})
	if team.State != desktop.Completed || team.Me != "rui" || team.ReviewHead != 4 || team.LifecycleHead != 2 {
		t.Fatalf("the team read: %+v", team)
	}
	if hub.downloads != 0 {
		t.Fatalf("a metadata read downloaded %d files", hub.downloads)
	}
	files := map[string]desktop.HubTeamFile{}
	for _, file := range team.Files {
		files[file.Digest] = file
	}
	if got := files[revision]; got.Name != "booking-rules" || got.Type != desktop.FileTypeRevision || got.AddedBy != "ana" || got.AddedAt != "2026-09-01T10:00:00Z" || got.KeepUntil != "2027-01-01T00:00:00Z" {
		t.Fatalf("the revision file: %+v", got)
	}
	if got := files[unrecorded]; got.Name != "" || got.AddedBy != "" || got.AddedAt != "" || got.Type != desktop.FileTypeFile {
		t.Fatalf("a file whose origin the hub never recorded was given one: %+v", got)
	}
	if files[release].Type != desktop.FileTypeTestRelease {
		t.Fatalf("the release: %+v", files[release])
	}
	status := map[string]desktop.HubReviewStatus{}
	for _, item := range team.Reviews {
		status[item.ID] = item.Status
		if item.ID == "ask-1" && (len(item.Discussion) != 1 || item.Discussion[0].Text != "Timeout too short" || item.RequestedBy != "ana" || !item.ToMe) {
			t.Fatalf("the answered request: %+v", item)
		}
	}
	if status["ask-1"] != desktop.ReviewStatusChangesRequested || status["ask-2"] != desktop.ReviewStatusStale || status["ask-3"] != desktop.ReviewStatusRequested {
		t.Fatalf("the review statuses: %v", status)
	}
	if first := team.Activity[0]; first.Action != desktop.ActivityRequestedReview || first.At != "2026-09-03T11:30:00Z" || !first.ToMe {
		t.Fatalf("activity is not newest first: %+v", team.Activity)
	}
	if len(team.Resources) != 1 || team.Resources[0].Resource != "booking-rules" || !slices.Equal(team.Resources[0].Tips, []string{"rev-1"}) {
		t.Fatalf("the resources: %+v", team.Resources)
	}
	// A request for changes is its own command.
	posted := fixture.app.PostHubReview(desktop.HubReviewCommandRequest{Project: teamProject, ID: "changes-3", Expected: 4, Kind: "change-request", Evidence: later, Parent: "ask-3", Release: later, Text: "Not yet"})
	if posted.State != desktop.Completed || posted.Events[0].Kind != "change-request" {
		t.Fatalf("request changes: %+v", posted)
	}
	stale := fixture.app.PostHubReview(desktop.HubReviewCommandRequest{Project: teamProject, ID: "approve-3", Expected: 4, Kind: "approval", Evidence: later, Parent: "ask-3", Release: later, Text: "ok"})
	if stale.State == desktop.Completed {
		t.Fatal("a decision against a stale history was recorded")
	}
}

// An upload is reviewed for the exact bytes, team and project, sent once
// per click, and a changed file is a stale review.
func TestTeamUploadIsReviewedAndSentOnceUnderItsIntent(t *testing.T) {
	hub := newTeamHub()
	fixture := signedInTeam(t, hub, "ana", writerScopes)
	source := filepath.Join(t.TempDir(), "results.json")
	if err := os.WriteFile(source, []byte(`{"passed":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	request := desktop.PrepareActionRequest{Action: desktop.TeamUploadAction, Team: &desktop.TeamActionOptions{Project: teamProject, Source: source}}
	review := prepared(t, fixture.app, request)
	if review.Consent != desktop.UploadConsent || review.Team == nil || review.Team.Name != "results.json" || review.Team.Size != 15 || review.Team.Team == "" || review.Team.Project != teamProject {
		t.Fatalf("the upload review: %+v %+v", review, review.Team)
	}
	if hub.uploads != 0 {
		t.Fatal("preparing an upload sent it")
	}
	if err := os.WriteFile(source, []byte(`{"passed":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed := fixture.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Token: review.Token, IntentID: "upload-changed"}); changed.Outcome != desktop.ActionStale || hub.uploads != 0 {
		t.Fatalf("a changed file: %+v", changed)
	}
	review = prepared(t, fixture.app, request)
	if connected := fixture.app.ConnectHub(); connected.State != desktop.Completed {
		t.Fatalf("reconnecting: %+v", connected)
	}
	if changed := fixture.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Token: review.Token, IntentID: "upload-new-session"}); changed.Outcome != desktop.ActionStale || hub.uploads != 0 {
		t.Fatalf("a transfer under a replaced connection: %+v", changed)
	}
	review = prepared(t, fixture.app, request)
	click := desktop.ExecuteActionRequest{Token: review.Token, IntentID: "upload-once"}
	first := fixture.app.ExecuteReviewedAction(click)
	again := fixture.app.ExecuteReviewedAction(click)
	if first.Outcome != desktop.ActionCompleted || first.Team == nil || first.Team.Digest != digestHex([]byte(`{"passed":false}`)) || !again.Replayed || hub.uploads != 1 {
		t.Fatalf("the upload: %+v %+v uploads=%d", first, again, hub.uploads)
	}
	viewer := signedInTeam(t, newTeamHub(), "vic", []string{"evidence.read"})
	if refused := viewer.app.PrepareAction(request); refused.Review == nil || refused.Review.Ready || !strings.Contains(refused.Review.Refusal, "cannot add files") {
		t.Fatalf("a viewer's upload: %+v", refused)
	}
}

// A revision continues the base its review showed against the head it
// showed; two revisions of one base are a conflict, merged part by part
// where both are text, and resolved as one new revision naming both.
func TestTeamRevisionAndResolutionAreCheckedAgainstTheCurrentHead(t *testing.T) {
	hub := newTeamHub()
	base := hub.link([]byte("start 09:00\nroom A\nlength 30\n"), "ana", "2026-09-01T10:00:00Z")
	hub.record("ana", "2026-09-01T10:00:01Z", hubprotocol.LifecycleCommand{ID: "rev-1", Kind: "revision", Resource: "booking-rules", Artifact: base, Reason: "first"})
	fixture := signedInTeam(t, hub, "ana", writerScopes)
	mine := filepath.Join(t.TempDir(), "rules.txt")
	if err := os.WriteFile(mine, []byte("start 10:00\nroom A\nlength 30\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := desktop.PrepareActionRequest{Action: desktop.TeamRevisionAction, Team: &desktop.TeamActionOptions{Project: teamProject, Source: mine, Resource: "booking-rules", Base: "rev-1"}}
	review := prepared(t, fixture.app, request)
	if review.Team.Base == nil || review.Team.Base.ID != "rev-1" || len(review.Team.Tips) != 1 || !slices.Contains(review.Requirements, desktop.RationaleRequirement) {
		t.Fatalf("the revision review: %+v", review.Team)
	}
	// Someone else continues the same base first: the head moved.
	theirs := hub.link([]byte("start 09:00\nroom A\nlength 45\n"), "rui", "2026-09-02T10:00:00Z")
	hub.record("rui", "2026-09-02T10:00:01Z", hubprotocol.LifecycleCommand{ID: "rev-2", Kind: "revision", Resource: "booking-rules", Artifact: theirs, Parents: []string{"rev-1"}, Reason: "longer"})
	if moved := fixture.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Token: review.Token, IntentID: "revise-stale", Decisions: desktop.ReviewDecisions{Rationale: "earlier start"}}); moved.Outcome != desktop.ActionStale || moved.Refreshed == nil {
		t.Fatalf("a revision against a moved head: %+v", moved)
	}
	review = prepared(t, fixture.app, request)
	revised := fixture.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Token: review.Token, IntentID: "revise", Decisions: desktop.ReviewDecisions{Rationale: "earlier start"}})
	if revised.Outcome != desktop.ActionCompleted || revised.Team.Revision == "" {
		t.Fatalf("the revision: %+v", revised)
	}

	conflict := fixture.app.OpenHubConflict(desktop.HubConflictRequest{Project: teamProject, Resource: "booking-rules"})
	if conflict.State != desktop.Completed || !conflict.Text || conflict.Base == nil || conflict.Base.ID != "rev-1" || conflict.Yours != revised.Team.Revision || conflict.Current != "rev-2" {
		t.Fatalf("the conflict: %+v", conflict)
	}
	merged := ""
	for _, hunk := range conflict.Hunks {
		if hunk.Conflict {
			t.Fatalf("changes to different lines conflicted: %+v", conflict.Hunks)
		}
		merged += strings.Join(hunk.Lines, "")
	}
	if merged != "start 10:00\nroom A\nlength 45\n" {
		t.Fatalf("the merge: %q", merged)
	}
	resolve := desktop.PrepareActionRequest{Action: desktop.TeamResolveAction, Team: &desktop.TeamActionOptions{Project: teamProject, Resource: "booking-rules", Resolution: &desktop.TeamResolution{Scope: conflict.Scope}}}
	resolution := prepared(t, fixture.app, resolve)
	resolved := fixture.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Token: resolution.Token, IntentID: "resolve", Decisions: desktop.ReviewDecisions{Rationale: "both changes"}})
	if resolved.Outcome != desktop.ActionCompleted {
		t.Fatalf("the resolution: %+v", resolved)
	}
	last := hub.lifecycle[len(hub.lifecycle)-1].Command
	if last.Kind != "resolve" || len(last.Parents) != 2 || string(hub.files[last.Artifact]) != merged {
		t.Fatalf("the recorded resolution: %+v", last)
	}
}

func TestTeamAnUnconfirmedRevisionKeepsItsCommandAndDoesNotRepeatTheWrite(t *testing.T) {
	for name, wrongSequence := range map[string]bool{"malformed": false, "wrong-sequence": true} {
		t.Run(name, func(t *testing.T) {
			hub := newTeamHub()
			base := hub.link([]byte("original"), "ana", "2026-09-01T10:00:00Z")
			hub.record("ana", "2026-09-01T10:00:01Z", hubprotocol.LifecycleCommand{ID: "rev-1", Kind: "revision", Resource: "notes", Artifact: base, Reason: "first"})
			hub.malformedRevisionReply = !wrongSequence
			hub.wrongSequenceRevisionReply = wrongSequence
			fixture := signedInTeam(t, hub, "ana", writerScopes)
			source := filepath.Join(t.TempDir(), "notes.txt")
			if err := os.WriteFile(source, []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
			review := prepared(t, fixture.app, desktop.PrepareActionRequest{Action: desktop.TeamRevisionAction, Team: &desktop.TeamActionOptions{Project: teamProject, Resource: "notes", Base: "rev-1", Source: source}})
			click := desktop.ExecuteActionRequest{Token: review.Token, IntentID: "revision-with-lost-reply", Decisions: desktop.ReviewDecisions{Rationale: "change"}}
			uncertain := fixture.app.ExecuteReviewedAction(click)
			if uncertain.Outcome != desktop.ActionUncertain || uncertain.Team == nil || !uncertain.Team.Uploaded || uncertain.Team.CommandID == "" || !strings.Contains(uncertain.Reason, "unconfirmed") {
				t.Fatalf("a malformed successful reply claimed a verdict: %+v", uncertain)
			}
			again := fixture.app.ExecuteReviewedAction(click)
			if !again.Replayed || hub.uploads != 1 || len(hub.lifecycle) != 2 || again.Team.CommandID != uncertain.Team.CommandID {
				t.Fatalf("an uncertain write was repeated: %+v", again)
			}
			blocked := fixture.app.PrepareAction(desktop.PrepareActionRequest{Action: desktop.TeamRevisionAction, Team: &desktop.TeamActionOptions{Project: teamProject, Resource: "notes", Base: "rev-1", Source: source}})
			if blocked.Review == nil || blocked.Review.Ready || blocked.Review.Team.PendingOperation != click.IntentID {
				t.Fatalf("another revision could bypass the unresolved command: %+v", blocked)
			}
			metadata := fixture.app.ReadHubTeam(desktop.HubTeamReadRequest{Project: teamProject})
			if metadata.State != desktop.Completed || len(metadata.Resources) != 1 || metadata.Resources[0].Revisions[len(metadata.Resources[0].Revisions)-1].ID != uncertain.Team.CommandID {
				t.Fatalf("metadata could not identify the command already recorded: %+v", metadata)
			}
			hub.mu.Lock()
			last := &hub.lifecycle[len(hub.lifecycle)-1]
			last.Actor = "someone-else"
			hub.mu.Unlock()
			if mismatched := fixture.app.ReconcileTeamTransfer(click.IntentID); mismatched.Outcome != desktop.ActionUncertain {
				t.Fatalf("another actor's record confirmed the transfer: %+v", mismatched)
			}
			hub.mu.Lock()
			last.Actor = "ana"
			hub.mu.Unlock()
			confirmed := fixture.app.ReconcileTeamTransfer(click.IntentID)
			if confirmed.Outcome != desktop.ActionCompleted || confirmed.Team.Revision != uncertain.Team.CommandID || hub.uploads != 1 || len(hub.lifecycle) != 2 {
				t.Fatalf("metadata did not resolve the exact command without repeating it: %+v", confirmed)
			}
			if checkedAgain := fixture.app.ReconcileTeamTransfer(click.IntentID); checkedAgain.Outcome != desktop.ActionCompleted || checkedAgain.Team.Revision != confirmed.Team.Revision {
				t.Fatalf("another status check lost its confirmed receipt: %+v", checkedAgain)
			}
		})
	}

}

// A conflict both sides changed the same way is taken once; a conflict they
// changed differently needs a side chosen, and a whole revision can always
// be chosen instead.
func TestTeamConflictNeedsASideForEveryDifferingChange(t *testing.T) {
	hub := newTeamHub()
	base := hub.link([]byte("a\nb\nc\n"), "ana", "2026-09-01T10:00:00Z")
	mine := hub.link([]byte("a\nB1\nc\n"), "ana", "2026-09-01T11:00:00Z")
	theirs := hub.link([]byte("a\nB2\nc\n"), "rui", "2026-09-01T12:00:00Z")
	hub.record("ana", "2026-09-01T10:00:01Z", hubprotocol.LifecycleCommand{ID: "rev-1", Kind: "revision", Resource: "notes", Artifact: base, Reason: "first"})
	hub.record("ana", "2026-09-01T11:00:01Z", hubprotocol.LifecycleCommand{ID: "rev-2", Kind: "revision", Resource: "notes", Artifact: mine, Parents: []string{"rev-1"}, Reason: "mine"})
	hub.record("rui", "2026-09-01T12:00:01Z", hubprotocol.LifecycleCommand{ID: "rev-3", Kind: "revision", Resource: "notes", Artifact: theirs, Parents: []string{"rev-1"}, Reason: "theirs"})
	fixture := signedInTeam(t, hub, "ana", writerScopes)
	conflict := fixture.app.OpenHubConflict(desktop.HubConflictRequest{Project: teamProject, Resource: "notes"})
	if conflict.Yours != "rev-2" || conflict.Current != "rev-3" || len(conflict.Hunks) != 3 || !conflict.Hunks[1].Conflict {
		t.Fatalf("the conflict: %+v", conflict)
	}
	request := desktop.PrepareActionRequest{Action: desktop.TeamResolveAction, Team: &desktop.TeamActionOptions{Project: teamProject, Resource: "notes", Resolution: &desktop.TeamResolution{Scope: conflict.Scope}}}
	if unchosen := fixture.app.PrepareAction(request); unchosen.Review == nil || unchosen.Review.Ready {
		t.Fatalf("a resolution with a conflict unchosen: %+v", unchosen)
	}
	request.Team.Resolution = &desktop.TeamResolution{Scope: conflict.Scope, Choices: []desktop.MergeChoice{desktop.MergeCurrent}}
	if chosen := prepared(t, fixture.app, request); chosen.Team.Size != int64(len("a\nB2\nc\n")) {
		t.Fatalf("the chosen resolution: %+v", chosen.Team)
	}
	request.Team.Resolution = &desktop.TeamResolution{Scope: conflict.Scope, Whole: "rev-2"}
	if whole := prepared(t, fixture.app, request); whole.Team.Size != int64(len("a\nB1\nc\n")) {
		t.Fatalf("a whole revision: %+v", whole.Team)
	}
	newer := hub.link([]byte("a\nB3\nc\n"), "rui", "2026-09-01T13:00:00Z")
	hub.record("rui", "2026-09-01T13:00:01Z", hubprotocol.LifecycleCommand{ID: "rev-4", Kind: "revision", Resource: "notes", Artifact: newer, Parents: []string{"rev-3"}, Reason: "changed again"})
	request.Team.Resolution = &desktop.TeamResolution{Scope: conflict.Scope, Choices: []desktop.MergeChoice{desktop.MergeCurrent}}
	stale := fixture.app.PrepareAction(request)
	if stale.Review == nil || stale.Review.Ready || !strings.Contains(stale.Review.Refusal, "competing revisions changed") || hub.uploads != 0 {
		t.Fatalf("unseen conflicts reused old choices: %+v", stale)
	}
}

// Retention is resolved to the current files and applied one file at a
// time: a date is never shortened, a failure is the file's own, and a retry
// of the same click records nothing twice.
func TestTeamRetentionAppliesPerFileAndNeverShortens(t *testing.T) {
	hub := newTeamHub()
	one := hub.link([]byte("one"), "ana", "2026-09-01T10:00:00Z")
	two := hub.link([]byte("two"), "ana", "2026-09-01T10:00:00Z")
	kept := hub.link([]byte("kept long"), "ana", "2026-09-01T10:00:00Z")
	hub.record("ana", "2026-09-01T10:00:01Z", hubprotocol.LifecycleCommand{ID: "keep-long", Kind: "retention", Artifact: kept, Until: "2099-01-01T00:00:00Z", Reason: "legal hold"})
	fixture := signedInTeam(t, hub, "ana", writerScopes)
	preview := fixture.app.PreviewHubRetention(desktop.HubRetentionRequest{Project: teamProject, Count: 2, Unit: "years", Scope: desktop.RetentionAllFiles})
	if preview.State != desktop.Completed || len(preview.Rows) != 3 {
		t.Fatalf("the preview: %+v", preview)
	}
	extend := []string{}
	for _, row := range preview.Rows {
		if row.Digest == kept && row.Change != desktop.RetentionUnchanged {
			t.Fatalf("a longer date would be shortened: %+v", row)
		}
		if row.Change == desktop.RetentionExtends {
			extend = append(extend, row.Digest)
		}
	}
	hub.refuse[two] = true
	apply := desktop.HubRetentionApply{Request: desktop.HubRetentionRequest{Project: teamProject}, Until: preview.Until, Digests: append(extend, kept), Reason: "study close-out", IntentID: "retain-1"}
	partial := fixture.app.ApplyHubRetention(apply)
	outcome := map[string]desktop.RetentionChange{}
	for _, row := range partial.Rows {
		outcome[row.Digest] = row.Change
	}
	if partial.State != desktop.Failed || outcome[one] != desktop.RetentionApplied || outcome[two] != desktop.RetentionFailed || outcome[kept] != desktop.RetentionUnchanged {
		t.Fatalf("a partly applied change: %+v", partial)
	}
	hub.refuse[two] = false
	before := len(hub.lifecycle)
	retried := fixture.app.ApplyHubRetention(apply)
	if retried.State != desktop.Completed || len(hub.lifecycle) != before+1 {
		t.Fatalf("the retry recorded %d commands: %+v", len(hub.lifecycle)-before, retried)
	}
}

// A download names a new file first and writes only verified bytes; a file
// already there is never replaced.
func TestTeamDownloadNamesANewFileFirstAndVerifiesTheBytes(t *testing.T) {
	hub := newTeamHub()
	digest := hub.link([]byte("verified bytes"), "ana", "2026-09-01T10:00:00Z")
	fixture := signedInTeam(t, hub, "ana", writerScopes)
	existing := filepath.Join(t.TempDir(), "existing.txt")
	_ = os.WriteFile(existing, []byte("mine"), 0o600)
	fixture.dialog.destination = existing
	if refused := fixture.app.DownloadHubFile(desktop.HubFileRequest{Project: teamProject, Digest: digest, Name: "notes.txt"}); refused.State != desktop.Failed || hub.downloads != 0 {
		t.Fatalf("an existing file: %+v", refused)
	}
	fresh := filepath.Join(t.TempDir(), "notes.txt")
	fixture.dialog.destination = fresh
	saved := fixture.app.DownloadHubFile(desktop.HubFileRequest{Project: teamProject, Digest: digest, Name: "notes.txt"})
	if data, _ := os.ReadFile(fresh); saved.State != desktop.Completed || string(data) != "verified bytes" || saved.Warning == "" {
		t.Fatalf("the download: %+v", saved)
	}
	hub.files[digest] = []byte("tampered")
	damaged := filepath.Join(t.TempDir(), "damaged.txt")
	fixture.dialog.destination = damaged
	if failed := fixture.app.DownloadHubFile(desktop.HubFileRequest{Project: teamProject, Digest: digest}); failed.State != desktop.Failed {
		t.Fatalf("damaged bytes: %+v", failed)
	}
	if _, err := os.Stat(damaged); err == nil {
		t.Fatal("damaged bytes were written")
	}
}

// Members come from the hub, to administrators only; the reviewers a request
// may ask exclude the person asking.
func TestTeamMembersAndReviewersComeFromTheHub(t *testing.T) {
	hub := newTeamHub()
	admin := signedInTeam(t, hub, "ana", writerScopes)
	members := admin.app.ListHubMembers(teamProject)
	if members.State != desktop.Completed || len(members.Members) != 3 || !members.Members[2].Removed {
		t.Fatalf("the members: %+v", members)
	}
	reviewer := signedInTeam(t, hub, "rui", writerScopes)
	if refused := reviewer.app.ListHubMembers(teamProject); refused.State != desktop.PermissionDenied {
		t.Fatalf("members read by a non-administrator: %+v", refused)
	}
	if reviewers := reviewer.app.ListHubReviewers(teamProject); reviewers.State != desktop.Completed || len(reviewers.Members) != 0 {
		t.Fatalf("a reviewer asked to review their own request: %+v", reviewers)
	}
}

// Connect team reads the organization's file and saves a name; saving never
// connects, and an exported setup is a new file.
func TestConnectTeamSavesANameAndNeverConnects(t *testing.T) {
	hub := newTeamHub()
	fixture := signedInTeam(t, hub, "ana", writerScopes)
	config := filepath.Join(fixture.dir, "hub-client.json")
	fixture.dialog.files = []string{config}
	choice := fixture.app.ChooseHubTeamConfig()
	if choice.State != desktop.Completed || choice.Config != config || !slices.Equal(choice.Projects, []string{teamProject}) || choice.Name == "" {
		t.Fatalf("the configuration choice: %+v", choice)
	}
	if blank := fixture.app.SaveHubTeam(desktop.HubTeamRequest{Name: " ", Config: config}); blank.State != desktop.Failed {
		t.Fatalf("a team without a name: %+v", blank)
	}
	saved := fixture.app.SaveHubTeam(desktop.HubTeamRequest{Name: "Integration team", Config: config})
	if saved.State != desktop.Completed || saved.Team != "Integration team" || saved.Connected || saved.Authenticated {
		t.Fatalf("the saved team: %+v", saved)
	}
	setup := filepath.Join(t.TempDir(), "access.json")
	fixture.dialog.destination = setup
	if exported := fixture.app.ExportHubSetup(desktop.HubSetupExport{Name: "access.json", Content: "{}\n"}); exported.State != desktop.Completed {
		t.Fatalf("export: %+v", exported)
	}
	if again := fixture.app.ExportHubSetup(desktop.HubSetupExport{Name: "access.json", Content: "{}\n"}); again.State != desktop.Failed {
		t.Fatalf("an export over an existing file: %+v", again)
	}
}

// A support review reads the one value-free summary it names and nothing
// else, and Download summary saves the approved summary as a new file.
func TestTeamSupportSummaryIsReadAndDownloadedOnlyAsTheApprovedSummary(t *testing.T) {
	hub := newTeamHub()
	summary, err := json.Marshal(sharing.Summary{Schema: sharing.Schema, SourceKind: "retained-packet", SourceIdentity: strings.Repeat("1", 64), InputCommitment: strings.Repeat("2", 64),
		SpecIdentity: strings.Repeat("3", 64), PolicyIdentity: strings.Repeat("4", 64), Outcome: "assertion_failure", ExternalEquivalence: "declined", Scope: sharing.Scope}, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	digest := hub.link(summary, "ana", "2026-09-01T10:00:00Z")
	other := hub.link([]byte("not a summary"), "ana", "2026-09-01T10:00:00Z")
	fixture := signedInTeam(t, hub, "rui", writerScopes)
	read := fixture.app.ReadHubSupportSummary(desktop.HubFileRequest{Project: teamProject, Digest: digest})
	if read.State != desktop.Completed || read.SourceKind != "retained-packet" || read.Outcome != "assertion_failure" || hub.downloads != 1 {
		t.Fatalf("the summary: %+v", read)
	}
	if refused := fixture.app.ReadHubSupportSummary(desktop.HubFileRequest{Project: teamProject, Digest: other}); refused.State != desktop.Failed {
		t.Fatalf("a file that is not a summary: %+v", refused)
	}
	fresh := filepath.Join(t.TempDir(), "summary.json")
	fixture.dialog.destination = fresh
	saved := fixture.app.DownloadHubSummary(desktop.HubFileRequest{Project: teamProject, Digest: digest})
	if data, _ := os.ReadFile(fresh); saved.State != desktop.Completed || string(data) != string(summary) {
		t.Fatalf("the summary download: %+v", saved)
	}
	if again := fixture.app.DownloadHubSummary(desktop.HubFileRequest{Project: teamProject, Digest: digest}); again.State != desktop.Failed {
		t.Fatalf("a download over an existing file: %+v", again)
	}
	fixture.dialog.files = []string{fresh}
	if chosen := fixture.app.ChooseHubLocalCopy("access-policy"); chosen.State != desktop.Completed || chosen.Paths[0] != fresh {
		t.Fatalf("a local copy: %+v", chosen)
	}
	if unknown := fixture.app.ChooseHubLocalCopy("anything"); unknown.State != desktop.Failed {
		t.Fatalf("an unknown local copy: %+v", unknown)
	}
}
