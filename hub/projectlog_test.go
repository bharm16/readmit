package hub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

// These tests decide review, lifecycle and support commands through the
// project log over its in-memory storage and a clock the test moves, so no
// database is needed.

const (
	logProject  = "case"
	logIssuer   = "https://idp.example.invalid"
	evidence    = "1111111111111111111111111111111111111111111111111111111111111111"
	unlinked    = "2222222222222222222222222222222222222222222222222222222222222222"
	retiredLink = "3333333333333333333333333333333333333333333333333333333333333333"
)

var logStart = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

type fakeClock struct{ at time.Time }

func (c *fakeClock) now() time.Time { return c.at }

func memoryLog(t *testing.T) (projectLog, *memoryStorage, *fakeClock) {
	t.Helper()
	storage := newMemoryStorage([]projectLink{{logProject, evidence}, {logProject, retiredLink}}, func(d string) ([]byte, error) {
		if d == evidence || d == retiredLink {
			return []byte("synthetic evidence"), nil
		}
		return nil, ErrMissing
	})
	clock := &fakeClock{at: logStart}
	return projectLog{storage: storage, now: clock.now}, storage, clock
}

func grants(roles map[string]string) accessPolicy {
	return func() (func(string) string, error) {
		return func(subject string) string { return roles[subject] }, nil
	}
}

var team = grants(map[string]string{"alice": "owner", "bob": "analyst", "carol": "reviewer", "robot": "runner"})

func person(subject string) Principal {
	return Principal{Kind: "oidc", Issuer: logIssuer, Subject: subject}
}

func opened(t *testing.T, l projectLog) projectView {
	t.Helper()
	p, e := l.open(context.Background(), logProject)
	if e != nil {
		t.Fatal(e)
	}
	return p
}

func recordReview(t *testing.T, l projectLog, actor string, c ReviewCommand, policy accessPolicy) (ReviewEvent, bool, error) {
	t.Helper()
	p := opened(t, l)
	r, e := l.reviews(context.Background(), p, true)
	if e != nil {
		t.Fatal(e)
	}
	return l.recordReview(context.Background(), p, r, person(actor), c, policy)
}

func recordLifecycle(t *testing.T, l projectLog, actor string, c LifecycleCommand, policy accessPolicy) (LifecycleEvent, []LifecycleEvent, bool, error) {
	t.Helper()
	return l.recordLifecycle(context.Background(), opened(t, l), person(actor), c, policy)
}

func comment(id string, expected int) ReviewCommand {
	return ReviewCommand{Schema: "readmit-hub-review-command/v1", ID: id, Expected: expected, Kind: "comment", Evidence: evidence, Text: "checked"}
}

func lifecycleCommand(id string, expected int, kind string) LifecycleCommand {
	return LifecycleCommand{Schema: "readmit-hub-lifecycle-command/v1", ID: id, Expected: expected, Kind: kind, Parents: []string{}, Reason: "synthetic"}
}

func TestReviewIsStampedByTheLogClockAndReplayedByID(t *testing.T) {
	l, storage, clock := memoryLog(t)
	event, replayed, e := recordReview(t, l, "bob", comment("c1", 0), team)
	if e != nil || replayed {
		t.Fatal("fresh comment refused:", replayed, e)
	}
	want := ReviewEvent{Schema: hubprotocol.ReviewEventV1, Project: logProject, Sequence: 1, Issuer: logIssuer, Actor: "bob", At: "2026-09-25T12:00:00Z", Command: comment("c1", 0)}
	if event != want || len(storage.reviewLogs[logProject]) != 1 {
		t.Fatalf("recorded %+v", event)
	}
	clock.at = clock.at.Add(time.Hour)
	again, replayed, e := recordReview(t, l, "bob", comment("c1", 0), team)
	if e != nil || !replayed || again != want || len(storage.reviewLogs[logProject]) != 1 {
		t.Fatal("a retried command was not answered with its recorded event:", replayed, e)
	}
	if _, _, e = recordReview(t, l, "carol", comment("c1", 0), team); !errors.Is(e, errLogIDConflict) {
		t.Fatal("a recorded id admitted for another actor:", e)
	}
	if _, _, e = recordReview(t, l, "bob", comment("c2", 0), team); !errors.Is(e, errLogHead) {
		t.Fatal("a stale head admitted:", e)
	}
	second, _, e := recordReview(t, l, "bob", comment("c2", 1), team)
	if e != nil || second.At != "2026-09-25T13:00:00Z" || second.Sequence != 2 {
		t.Fatal("the next comment was not stamped by the moved clock:", second, e)
	}
}

func TestReviewRecipientsAndEvidenceAreDecidedByTheLog(t *testing.T) {
	l, _, _ := memoryLog(t)
	request := func(id, recipient string) ReviewCommand {
		c := comment(id, 0)
		c.Recipient = recipient
		return c
	}
	for _, recipient := range []string{"stranger", "robot"} {
		if _, _, e := recordReview(t, l, "bob", request("r-"+recipient, recipient), team); !errors.Is(e, errRecipient) {
			t.Fatalf("recipient %s admitted: %v", recipient, e)
		}
	}
	unreadable := func() (func(string) string, error) { return nil, errAccess }
	if _, _, e := recordReview(t, l, "bob", comment("c1", 0), unreadable); !errors.Is(e, errPolicyUnavailable) {
		t.Fatal("a command admitted without a readable policy:", e)
	}
	missing := comment("c1", 0)
	missing.Evidence = unlinked
	if _, _, e := recordReview(t, l, "bob", missing, team); !errors.Is(e, errReviewRefused) || !errors.Is(e, ErrMissing) {
		t.Fatal("evidence the project does not link admitted:", e)
	}
	if _, _, e := recordReview(t, l, "bob", request("c1", "carol"), team); e != nil {
		t.Fatal("a comment addressed to a reviewer refused:", e)
	}
}

func TestSupportReadsNeedV2AndRetiredArtifactsAreNotSupport(t *testing.T) {
	l, storage, _ := memoryLog(t)
	storage.appendReview(context.Background(), ReviewEvent{Schema: hubprotocol.ReviewEventV2, Project: logProject, Sequence: 1, Issuer: logIssuer, Actor: "alice", At: "2026-09-25T12:00:00Z",
		Command: ReviewCommand{Schema: "readmit-hub-review-command/v2", ID: "p1", Kind: "support-policy", Evidence: retiredLink}})
	p := opened(t, l)
	if _, e := l.reviews(context.Background(), p, false); !errors.Is(e, errSupportNeedsV2) {
		t.Fatal("a v1 read of support history answered:", e)
	}
	if _, _, _, e := recordLifecycle(t, l, "alice", LifecycleCommand{Schema: "readmit-hub-lifecycle-command/v1", ID: "keep", Kind: "retention", Artifact: retiredLink, Until: "2026-09-25T12:00:00Z", Parents: []string{}, Reason: "synthetic"}, team); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e := recordLifecycle(t, l, "alice", LifecycleCommand{Schema: "readmit-hub-lifecycle-command/v1", ID: "retire", Expected: 1, Kind: "retire", Artifact: retiredLink, Parents: []string{}, Reason: "synthetic"}, team); e != nil {
		t.Fatal(e)
	}
	if _, e := l.supportArtifact(context.Background(), opened(t, l))(retiredLink); !errors.Is(e, ErrMissing) {
		t.Fatal("a retired artifact loaded as support content:", e)
	}
	if _, e := l.linkedArtifact(context.Background(), logProject)(retiredLink); e != nil {
		t.Fatal("a retired artifact stopped loading as evidence:", e)
	}
}

func TestRetirementWaitsForTheRetentionOnTheLogClock(t *testing.T) {
	l, _, clock := memoryLog(t)
	retention := lifecycleCommand("keep", 0, "retention")
	retention.Artifact, retention.Until = evidence, "2026-09-26T00:00:00Z"
	if _, _, _, e := recordLifecycle(t, l, "alice", retention, team); e != nil {
		t.Fatal(e)
	}
	retire := lifecycleCommand("retire", 1, "retire")
	retire.Artifact = evidence
	if _, _, _, e := recordLifecycle(t, l, "alice", retire, team); !errors.Is(e, errLifecycleConflict) {
		t.Fatal("retired before its retention lapsed:", e)
	}
	clock.at = time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	event, events, replayed, e := recordLifecycle(t, l, "alice", retire, team)
	if e != nil || replayed || event.At != "2026-09-26T00:00:00Z" || len(events) != 2 {
		t.Fatal("retirement refused once the retention lapsed:", e)
	}
	if !opened(t, l).retired(evidence) {
		t.Fatal("the recorded retirement is not derived")
	}
}

func TestRemovalRulesAndARemovedPrincipal(t *testing.T) {
	l, _, _ := memoryLog(t)
	for _, subject := range []string{"alice", "stranger"} {
		c := lifecycleCommand("rm-"+subject, 0, "remove-user")
		c.Subject = subject
		if _, _, _, e := recordLifecycle(t, l, "alice", c, team); !errors.Is(e, errRemoval) {
			t.Fatalf("removing %s admitted: %v", subject, e)
		}
	}
	owner := lifecycleCommand("rm-owner", 0, "remove-user")
	owner.Subject = "alice"
	if _, _, _, e := recordLifecycle(t, l, "bob", owner, team); !errors.Is(e, errRemoval) {
		t.Fatal("an owner removed:", e)
	}
	c := lifecycleCommand("rm-bob", 0, "remove-user")
	c.Subject = "bob"
	if _, _, _, e := recordLifecycle(t, l, "alice", c, team); e != nil {
		t.Fatal(e)
	}
	p := opened(t, l)
	if !p.removed(person("bob")) || p.removed(person("carol")) || p.removed(Principal{Issuer: "https://other.invalid", Subject: "bob"}) {
		t.Fatal("removal is not the issuer and subject the log recorded")
	}
}

func TestAuditExportStampsAndReproducesTheReviewHead(t *testing.T) {
	l, _, _ := memoryLog(t)
	if _, _, e := recordReview(t, l, "bob", comment("c1", 0), team); e != nil {
		t.Fatal(e)
	}
	event, events, _, e := recordLifecycle(t, l, "alice", lifecycleCommand("audit", 0, "audit-export"), team)
	if e != nil || event.ReviewHead != 1 {
		t.Fatal("audit export did not stamp the review head:", event.ReviewHead, e)
	}
	if _, _, e = recordReview(t, l, "bob", comment("c2", 1), team); e != nil {
		t.Fatal(e)
	}
	export, e := l.auditExport(context.Background(), event, events)
	if e != nil || export.ReviewHead != 1 || len(export.Reviews) != 1 || len(export.Lifecycle) != 1 {
		t.Fatal("a retried export does not reproduce the recorded prefixes:", export, e)
	}
}

// A backed-up event a live write would refuse is refused when it is
// replayed: each case records a history live, shows the live log refusing a
// command, and replays the event that command would have been stamped as.
func TestReplayRefusesWhatALiveWriteRefuses(t *testing.T) {
	type refusal struct {
		name    string
		history func(projectLog)
		actor   string
		command LifecycleCommand
	}
	removeBob := func(l projectLog) {
		c := lifecycleCommand("rm-bob", 0, "remove-user")
		c.Subject = "bob"
		if _, _, _, e := recordLifecycle(t, l, "alice", c, team); e != nil {
			t.Fatal(e)
		}
	}
	keep := func(l projectLog) {
		c := lifecycleCommand("keep", 0, "retention")
		c.Artifact, c.Until = evidence, "2026-09-26T00:00:00Z"
		if _, _, _, e := recordLifecycle(t, l, "alice", c, team); e != nil {
			t.Fatal(e)
		}
	}
	self := lifecycleCommand("rm-self", 0, "remove-user")
	self.Subject = "alice"
	early := lifecycleCommand("retire", 1, "retire")
	early.Artifact = evidence
	unlinkedRetention := lifecycleCommand("keep-unlinked", 0, "retention")
	unlinkedRetention.Artifact, unlinkedRetention.Until = unlinked, "2026-09-26T00:00:00Z"
	for _, c := range []refusal{
		{"stale head", keep, "alice", lifecycleCommand("audit", 0, "audit-export")},
		{"recorded id", keep, "bob", func() LifecycleCommand { c := lifecycleCommand("keep", 1, "audit-export"); return c }()},
		{"removed actor", removeBob, "bob", lifecycleCommand("audit", 1, "audit-export")},
		{"self removal", func(projectLog) {}, "alice", self},
		{"retire before retention lapses", keep, "alice", early},
		{"unlinked artifact", func(projectLog) {}, "alice", unlinkedRetention},
	} {
		t.Run(c.name, func(t *testing.T) {
			live, _, _ := memoryLog(t)
			c.history(live)
			p := opened(t, live)
			refused := p.removed(person(c.actor))
			if !refused {
				_, _, _, e := recordLifecycle(t, live, c.actor, c.command, team)
				refused = e != nil
			}
			if !refused {
				t.Fatal("the live log admitted the command")
			}
			replayed, _, _ := memoryLog(t)
			for _, event := range p.events {
				if e := replayed.replayLifecycle(context.Background(), event); e != nil {
					t.Fatal("a history the live log wrote does not replay:", e)
				}
			}
			stamp := LifecycleEvent{Schema: hubprotocol.LifecycleEventSchema, Project: logProject, Sequence: len(p.events) + 1, Issuer: logIssuer, Actor: c.actor, At: logStart.Format(time.RFC3339Nano), Command: c.command}
			if e := replayed.replayLifecycle(context.Background(), stamp); e == nil {
				t.Fatal("the replay admitted what the live log refused")
			}
		})
	}
}

func TestReplayRefusesAStampNoLiveWriteGives(t *testing.T) {
	l, _, _ := memoryLog(t)
	good := ReviewEvent{Schema: hubprotocol.ReviewEventV1, Project: logProject, Sequence: 1, Issuer: logIssuer, Actor: "bob", At: "2026-09-25T12:00:00Z", Command: comment("c1", 0)}
	for name, change := range map[string]func(*ReviewEvent){
		"skipped sequence": func(e *ReviewEvent) { e.Sequence = 2 },
		"no issuer":        func(e *ReviewEvent) { e.Issuer = "" },
		"no instant":       func(e *ReviewEvent) { e.At = "yesterday" },
		"stale head":       func(e *ReviewEvent) { e.Command.Expected = 1 },
		"unlinked":         func(e *ReviewEvent) { e.Command.Evidence = unlinked },
	} {
		event := good
		change(&event)
		if e := l.replayReview(context.Background(), event); e == nil {
			t.Fatalf("%s replayed", name)
		}
	}
	if e := l.replayReview(context.Background(), good); e != nil {
		t.Fatal(e)
	}
	if e := l.replayReview(context.Background(), ReviewEvent{Schema: good.Schema, Project: logProject, Sequence: 2, Issuer: logIssuer, Actor: "bob", At: good.At, Command: comment("c1", 1)}); e == nil {
		t.Fatal("a recorded id replayed twice")
	}
}

// The restore path reads a backup through the same replay: a lifecycle event
// by a principal the backup's own log removed is refused before anything is
// written, and the same backup without that event reads back.
func TestBackupRefusesAnEventByARemovedPrincipal(t *testing.T) {
	team := true
	remove := LifecycleEvent{Schema: hubprotocol.LifecycleEventSchema, Project: logProject, Sequence: 1, Issuer: logIssuer, Actor: "alice", At: "2026-09-25T12:00:00Z", Command: lifecycleCommand("rm-bob", 0, "remove-user")}
	remove.Command.Subject = "bob"
	after := LifecycleEvent{Schema: hubprotocol.LifecycleEventSchema, Project: logProject, Sequence: 2, Issuer: logIssuer, Actor: "bob", At: "2026-09-25T12:00:01Z", Command: lifecycleCommand("rm-carol", 1, "remove-user")}
	after.Command.Subject = "carol"
	read := func(lifecycle ...LifecycleEvent) error {
		m := backupManifest{Schema: "readmit-hub-backup/v4", MetadataVersion: 5, Team: &team, Artifacts: []backupEntry{}, Projects: []projectLink{}, Reviews: []ReviewEvent{}, Lifecycle: lifecycle}
		data, e := encodeBackupManifest(m)
		if e != nil {
			t.Fatal(e)
		}
		dir := t.TempDir()
		if e = os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0600); e != nil {
			t.Fatal(e)
		}
		root, e := os.OpenRoot(dir)
		if e != nil {
			t.Fatal(e)
		}
		defer root.Close()
		_, e = readBackup(root)
		return e
	}
	if e := read(remove); e != nil {
		t.Fatal("a backup its live log wrote does not read back:", e)
	}
	if e := read(remove, after); !errors.Is(e, ErrIntegrity) {
		t.Fatal(fmt.Sprint("an event by a removed principal was restored: ", e))
	}
}
