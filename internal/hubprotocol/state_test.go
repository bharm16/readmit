package hubprotocol_test

import (
	"encoding/json/v2"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/hubprotocol"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/sharing"
)

var errUnstored = errors.New("not stored in the project")

// stored is a project's artifacts by digest, loaded the way the hub loads them.
type stored map[string][]byte

func (s stored) load(digest string) ([]byte, error) {
	data, ok := s[digest]
	if !ok {
		return nil, errUnstored
	}
	return data, nil
}

func support(id, kind, evidence, parent, recipient, release string) hubprotocol.ReviewCommand {
	return hubprotocol.ReviewCommand{Schema: hubprotocol.ReviewCommandV2, ID: id, Kind: kind, Evidence: evidence, Parent: parent, Recipient: recipient, Release: release, Text: "support"}
}

func recorded(actor string, c hubprotocol.ReviewCommand) hubprotocol.ReviewEvent {
	schema := hubprotocol.ReviewEventV1
	if hubprotocol.IsSupport(c) {
		schema = hubprotocol.ReviewEventV2
	}
	return hubprotocol.ReviewEvent{Schema: schema, Issuer: "issuer", Actor: actor, Command: c}
}

func TestThePolicyInForceIsTheLastOneRecorded(t *testing.T) {
	first := support("policy-1", "support-policy", "digest-1", "", "", "")
	second := support("policy-2", "support-policy", "digest-2", "", "", "")
	comment := hubprotocol.ReviewCommand{Schema: hubprotocol.ReviewCommandV1, ID: "c1", Kind: "comment", Text: "hello"}
	state := hubprotocol.DeriveReviews([]hubprotocol.ReviewEvent{recorded("admin", first), recorded("admin", comment), recorded("admin", second)})
	if policy, ok := state.PolicyInForce(); !ok || policy.ID != "policy-2" || policy.Evidence != "digest-2" {
		t.Fatalf("the last policy must be in force: %+v %v", policy, ok)
	}
	if !state.HasSupport() || state.AuditSchema() != hubprotocol.AuditV2 {
		t.Fatal("support commands went unseen")
	}
	empty := hubprotocol.DeriveReviews(nil)
	if _, ok := empty.PolicyInForce(); ok || empty.HasSupport() || empty.AuditSchema() != hubprotocol.AuditV1 {
		t.Fatalf("an empty log carries no policy: %+v", empty)
	}
	if state.HistorySchema(false) != hubprotocol.ReviewHistoryV1 || state.HistorySchema(true) != hubprotocol.ReviewHistoryV2 {
		t.Fatal("history version misread")
	}
	// A request under the replaced policy is history, not current.
	if state.Current(support("req", "support-request", "", "policy-1", "rui", "digest-1")) {
		t.Fatal("a request under a replaced policy read as current")
	}
	if !state.Current(support("req", "support-request", "", "policy-2", "rui", "digest-2")) {
		t.Fatal("a request under the policy in force read as stale")
	}
}

// The support workflow: a policy announced, a summary requested under it and
// approved by the asked reviewer only, exported only under that chain, and a
// new policy leaving the old approval as history.
func TestASupportSummaryIsApprovedAndExportedOnlyUnderThePolicyInForce(t *testing.T) {
	policy := []byte(`{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["customer-hub-download"],"max_bytes":4096}`)
	p := sharing.Digest(policy)
	summary := sharing.Summary{Schema: sharing.Schema, SourceKind: "retained-packet", SourceIdentity: strings.Repeat("1", 64), InputCommitment: strings.Repeat("2", 64), SpecIdentity: strings.Repeat("3", 64), PolicyIdentity: p, Outcome: "assertion_failure", ExternalEquivalence: "declined", Scope: sharing.Scope}
	raw, _ := json.Marshal(summary, json.Deterministic(true))
	d := sharing.Digest(raw)
	files := stored{p: policy, d: raw}

	announce := support("policy", "support-policy", p, "", "", "")
	if err := hubprotocol.DeriveReviews(nil).Validate(announce, "admin", "issuer", files.load); err != nil {
		t.Fatal(err)
	}
	events := []hubprotocol.ReviewEvent{recorded("admin", announce)}
	request := support("request", "support-request", d, "policy", "reviewer", p)
	if err := hubprotocol.DeriveReviews(events).Validate(request, "analyst", "issuer", files.load); err != nil {
		t.Fatal(err)
	}
	stale := request
	stale.Parent = "elsewhere"
	if err := hubprotocol.DeriveReviews(events).Validate(stale, "analyst", "issuer", files.load); !errors.Is(err, hubprotocol.ErrConflict) {
		t.Fatalf("a request off the policy in force: %v", err)
	}
	events = append(events, recorded("analyst", request))
	if picked, ok := hubprotocol.DeriveReviews(events).SupportRequest(d); !ok || picked.Command.ID != "request" {
		t.Fatalf("the request naming the summary: %+v %v", picked, ok)
	}
	if _, ok := hubprotocol.DeriveReviews(events).SupportRequest(p); ok {
		t.Fatal("a request picked for bytes it does not name")
	}

	approval := support("approve", "support-approval", d, "request", "", p)
	if _, err := hubprotocol.DeriveReviews(events).Approved(d, files.load); err == nil {
		t.Fatal("an unapproved summary exported")
	}
	for _, actor := range []string{"analyst", "owner", "runner"} {
		if err := hubprotocol.DeriveReviews(events).Validate(approval, actor, "issuer", files.load); !errors.Is(err, hubprotocol.ErrRefused) {
			t.Fatalf("%s approved a request addressed to someone else: %v", actor, err)
		}
	}
	if err := hubprotocol.DeriveReviews(events).Validate(approval, "reviewer", "wrong-issuer", files.load); !errors.Is(err, hubprotocol.ErrRefused) {
		t.Fatalf("an approval from another issuer: %v", err)
	}
	orphan := approval
	orphan.Parent = "nobody"
	if err := hubprotocol.DeriveReviews(events).Validate(orphan, "reviewer", "issuer", files.load); !errors.Is(err, hubprotocol.ErrMissing) {
		t.Fatalf("an approval of an unrecorded request: %v", err)
	}
	if err := hubprotocol.DeriveReviews(events).Validate(approval, "reviewer", "issuer", files.load); err != nil {
		t.Fatal(err)
	}
	events = append(events, recorded("reviewer", approval))
	second := approval
	second.ID = "approve-again"
	if err := hubprotocol.DeriveReviews(events).Validate(second, "reviewer", "issuer", files.load); !errors.Is(err, hubprotocol.ErrConflict) {
		t.Fatalf("a request approved twice: %v", err)
	}
	if got, err := hubprotocol.DeriveReviews(events).Approved(d, files.load); err != nil || string(got) != string(raw) {
		t.Fatalf("the approved summary is unavailable: %v", err)
	}

	changed := append(policy, ' ')
	files[sharing.Digest(changed)] = changed
	events = append(events, recorded("admin", support("policy-next", "support-policy", sharing.Digest(changed), "", "", "")))
	if _, err := hubprotocol.DeriveReviews(events).Approved(d, files.load); err == nil {
		t.Fatal("an old approval survived a policy version change")
	}
	if hubprotocol.DeriveReviews(events).Current(approval) {
		t.Fatal("an approval under a replaced policy read as current")
	}
	if err := hubprotocol.DeriveReviews(events).Validate(request, "analyst", "issuer", files.load); !errors.Is(err, hubprotocol.ErrConflict) {
		t.Fatalf("a request under a replaced policy: %v", err)
	}
	misnamed := support("request-2", "support-request", d, "policy-next", "reviewer", sharing.Digest(changed))
	if err := hubprotocol.DeriveReviews(events).Validate(misnamed, "analyst", "issuer", files.load); !errors.Is(err, hubprotocol.ErrIntegrity) {
		t.Fatalf("a summary naming another policy: %v", err)
	}
}

func TestAReleaseApprovalAnswersTheRequestNamingTheSameContent(t *testing.T) {
	release, other := strings.Repeat("b", 64), strings.Repeat("c", 64)
	request := func(id, digest string) hubprotocol.ReviewCommand {
		return hubprotocol.ReviewCommand{Schema: hubprotocol.ReviewCommandV1, ID: id, Kind: "review-request", Evidence: digest, Recipient: "rui", Release: digest, Text: "review"}
	}
	events := []hubprotocol.ReviewEvent{recorded("ana", request("first", release)), recorded("ana", request("elsewhere", other)), recorded("ana", request("again", release))}
	picked, ok := hubprotocol.DeriveReviews(events).ReleaseRequest(release)
	if !ok || picked.Command.ID != "again" {
		t.Fatalf("the last request naming the release: %+v %v", picked, ok)
	}
	if _, ok := hubprotocol.DeriveReviews(events[:0]).ReleaseRequest(release); ok {
		t.Fatal("a request picked from an empty log")
	}
	supportRequest := support("support", "support-request", release, "policy", "rui", release)
	if _, ok := hubprotocol.DeriveReviews([]hubprotocol.ReviewEvent{recorded("ana", supportRequest)}).ReleaseRequest(release); ok {
		t.Fatal("a support request picked as a release review request")
	}
}

func TestRemovalAndRetirementAreReadOnceFromTheLifecycleLog(t *testing.T) {
	events := []hubprotocol.LifecycleEvent{
		{Issuer: "issuer", Command: hubprotocol.LifecycleCommand{Kind: "remove-user", Subject: "alice"}},
		{Issuer: "other", Command: hubprotocol.LifecycleCommand{Kind: "remove-user", Subject: "alice"}},
		{Issuer: "issuer", Command: hubprotocol.LifecycleCommand{Kind: "retire", Artifact: "digest"}},
	}
	state := hubprotocol.DeriveLifecycle(events)
	if !state.Removed("issuer", "alice") || !state.Removed("other", "alice") {
		t.Fatal("a removed principal reads as present")
	}
	if state.Removed("other", "bob") || state.Removed("issuer", "bob") || state.Removed("third", "alice") {
		t.Fatal("an unremoved principal reads as removed")
	}
	if !state.Retired("digest") || state.Retired("other") {
		t.Fatal("retirement misread")
	}
	history := hubprotocol.LifecycleHistory{Events: events}
	if !history.Removed("issuer", "alice") || history.Removed("issuer", "bob") {
		t.Fatal("a history answers removal differently from the log it carries")
	}
}

func TestRevisionTipsAreTheUnresolvedRevisionsOfEachResource(t *testing.T) {
	revision := func(id, resource string, parents ...string) hubprotocol.LifecycleEvent {
		kind := "revision"
		if len(parents) > 1 {
			kind = "resolve"
		}
		return hubprotocol.LifecycleEvent{Command: hubprotocol.LifecycleCommand{ID: id, Kind: kind, Resource: resource, Parents: parents}}
	}
	events := []hubprotocol.LifecycleEvent{
		revision("one", "case"), revision("b-branch", "case", "one"), revision("a-branch", "case", "one"),
		revision("other", "second"),
	}
	want := map[string][]string{"case": {"a-branch", "b-branch"}, "second": {"other"}}
	if got := hubprotocol.DeriveLifecycle(events).Tips(); !reflect.DeepEqual(got, want) {
		t.Fatalf("tips %v, want %v", got, want)
	}
	events = append(events, revision("merged", "case", "a-branch", "b-branch"))
	if got := hubprotocol.DeriveLifecycle(events).Tips()["case"]; !reflect.DeepEqual(got, []string{"merged"}) {
		t.Fatalf("a resolve left tips %v", got)
	}
	if got := hubprotocol.DeriveLifecycle(nil).Tips(); len(got) != 0 {
		t.Fatalf("an empty log has tips %v", got)
	}
}

func TestLifecycleCommandsContinueTheLogTheyAreDecidedAgainst(t *testing.T) {
	artifact := strings.Repeat("a", 64)
	files := stored{artifact: []byte("x")}
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	command := func(id, kind string, parents ...string) hubprotocol.LifecycleCommand {
		return hubprotocol.LifecycleCommand{ID: id, Kind: kind, Resource: "case", Artifact: artifact, Parents: parents}
	}
	var events []hubprotocol.LifecycleEvent
	decide := func(c hubprotocol.LifecycleCommand) error {
		return hubprotocol.DeriveLifecycle(events).Validate(c, files.load, at)
	}
	record := func(c hubprotocol.LifecycleCommand) {
		t.Helper()
		if err := decide(c); err != nil {
			t.Fatalf("%s: %v", c.ID, err)
		}
		events = append(events, hubprotocol.LifecycleEvent{Command: c})
	}
	record(command("one", "revision"))
	if err := decide(command("rootless", "revision")); !errors.Is(err, hubprotocol.ErrConflict) {
		t.Fatalf("a second root revision: %v", err)
	}
	record(command("a", "revision", "one"))
	record(command("b", "revision", "one"))
	if err := decide(command("partial", "resolve", "a")); !errors.Is(err, hubprotocol.ErrConflict) {
		t.Fatalf("a resolve naming one of two tips: %v", err)
	}
	record(command("merged", "resolve", "a", "b"))
	missing := command("ghost", "revision", "merged")
	missing.Artifact = strings.Repeat("f", 64)
	if err := decide(missing); !errors.Is(err, errUnstored) {
		t.Fatalf("a revision naming an unstored artifact: %v", err)
	}

	retain := hubprotocol.LifecycleCommand{ID: "keep", Kind: "retention", Artifact: artifact, Until: at.Add(time.Hour).Format(time.RFC3339Nano)}
	record(retain)
	shorter := retain
	shorter.ID, shorter.Until = "shorter", at.Format(time.RFC3339Nano)
	if err := decide(shorter); !errors.Is(err, hubprotocol.ErrConflict) {
		t.Fatalf("a retention shortened: %v", err)
	}
	retire := hubprotocol.LifecycleCommand{ID: "retire", Kind: "retire", Artifact: artifact}
	if err := decide(retire); !errors.Is(err, hubprotocol.ErrConflict) {
		t.Fatalf("a retirement inside its retention: %v", err)
	}
	if err := hubprotocol.DeriveLifecycle(events).Validate(retire, files.load, at.Add(2*time.Hour)); err != nil {
		t.Fatalf("a retirement after its retention: %v", err)
	}
	events = append(events, hubprotocol.LifecycleEvent{Command: retire})
	if err := decide(command("after", "revision", "merged")); !errors.Is(err, hubprotocol.ErrConflict) {
		t.Fatalf("a revision naming a retired artifact: %v", err)
	}
}

// releaseBytes is one released expectation for id, continuing previous.
func releaseBytes(t *testing.T, id string, previous *expectation.Release, spec string) (expectation.Release, []byte) {
	t.Helper()
	review, err := expectation.Review(id, []byte(spec), []profileversion.Version{}, previous, false)
	if err != nil {
		t.Fatal(err)
	}
	release, err := expectation.Approve(id, []byte(spec), []profileversion.Version{}, previous, review.Identity, "Local approver label", "synthetic rationale")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := release.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return release, raw
}

// An approval answers a request addressed to the approver by someone else,
// once, and continues the release chain the earlier approvals recorded.
func TestAReleaseApprovalContinuesTheChainItsRequestNamed(t *testing.T) {
	spec := `{"schema":"readmit-test/v1","name":"synthetic","input":{"case":"case","messages":["s0001-e000001"]},"target":"target.json","setup":{"initial_state":"operator-declared","reset_instructions":"Reset fixture"},"observation":{"boundary":"ack-contract"},"assertions":[{"id":"ack","operator":"ack_field_equals","message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"AA"}}}]}`
	first, firstBytes := releaseBytes(t, "booking", nil, spec)
	_, secondBytes := releaseBytes(t, "booking", &first, strings.Replace(spec, `"AA"`, `"AE"`, 1))
	files := stored{sharing.Digest(firstBytes): firstBytes, sharing.Digest(secondBytes): secondBytes}
	d1, d2 := sharing.Digest(firstBytes), sharing.Digest(secondBytes)
	v1 := func(id, kind, evidence, parent, recipient, release string) hubprotocol.ReviewCommand {
		return hubprotocol.ReviewCommand{Schema: hubprotocol.ReviewCommandV1, ID: id, Kind: kind, Evidence: evidence, Parent: parent, Recipient: recipient, Release: release, Text: "review"}
	}
	var events []hubprotocol.ReviewEvent
	decide := func(c hubprotocol.ReviewCommand, actor string) error {
		return hubprotocol.DeriveReviews(events).Validate(c, actor, "issuer", files.load)
	}
	// The second release cannot be approved first: its chain starts at the first.
	events = append(events, recorded("ana", v1("ask-2", "review-request", d2, "", "rui", d2)))
	if err := decide(v1("early", "approval", d2, "ask-2", "", d2), "rui"); !errors.Is(err, hubprotocol.ErrConflict) {
		t.Fatalf("a successor approved before its predecessor: %v", err)
	}
	events = append(events, recorded("ana", v1("ask-1", "review-request", d1, "", "rui", d1)))
	if err := decide(v1("self", "approval", d1, "ask-1", "", d1), "ana"); !errors.Is(err, hubprotocol.ErrRefused) {
		t.Fatalf("an approval by someone the request did not ask: %v", err)
	}
	if err := decide(v1("mismatch", "approval", d2, "ask-1", "", d2), "rui"); !errors.Is(err, hubprotocol.ErrMissing) {
		t.Fatalf("an approval naming other evidence than its request: %v", err)
	}
	approve1 := v1("ok-1", "approval", d1, "ask-1", "", d1)
	if err := decide(approve1, "rui"); err != nil {
		t.Fatal(err)
	}
	events = append(events, recorded("rui", approve1))
	if err := decide(v1("ok-1-again", "approval", d1, "ask-1", "", d1), "rui"); !errors.Is(err, hubprotocol.ErrConflict) {
		t.Fatalf("a request approved twice: %v", err)
	}
	if err := decide(v1("ok-2", "approval", d2, "ask-2", "", d2), "rui"); err != nil {
		t.Fatalf("the successor after its predecessor: %v", err)
	}
	if err := decide(v1("unstored", "comment", strings.Repeat("e", 64), "", "", ""), "rui"); !errors.Is(err, errUnstored) {
		t.Fatalf("a comment on unstored evidence: %v", err)
	}
}
