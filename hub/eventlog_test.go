package hub

import (
	"errors"
	"testing"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

func reviewCommand(id string, expected int) ReviewCommand {
	return ReviewCommand{Schema: "readmit-hub-review-command/v1", ID: id, Expected: expected, Kind: "comment", Evidence: "sha256", Text: "noted"}
}

func reviewEvent(sequence int, actor string, c ReviewCommand) ReviewEvent {
	return ReviewEvent{Schema: "readmit-hub-review-event/v1", Project: "case", Sequence: sequence, Issuer: "issuer", Actor: actor, Command: c}
}

func TestLogAdmitReplaysRecordedCommand(t *testing.T) {
	c := reviewCommand("cmd-1", 1)
	recorded := reviewEvent(1, "alice", c)
	events := []ReviewEvent{recorded}
	event, replay, err := reviewLog.admit(events, len(events), c, "alice", "issuer")
	if err != nil || !replay {
		t.Fatal("recorded command not replayed:", replay, err)
	}
	if event != recorded {
		t.Fatal("replay returned a different event")
	}
}

func TestLogAdmitRefusesConflictingID(t *testing.T) {
	c := reviewCommand("cmd-1", 1)
	events := []ReviewEvent{reviewEvent(1, "alice", c)}
	changed := c
	changed.Text = "edited"
	if _, _, err := reviewLog.admit(events, len(events), changed, "alice", "issuer"); !errors.Is(err, errLogIDConflict) {
		t.Fatal("changed command under a recorded id admitted:", err)
	}
	if _, _, err := reviewLog.admit(events, len(events), c, "bob", "issuer"); !errors.Is(err, errLogIDConflict) {
		t.Fatal("recorded id admitted under another actor:", err)
	}
}

func TestLogAdmitRefusesStaleHeadAndLimit(t *testing.T) {
	c := reviewCommand("cmd-2", 0)
	events := []ReviewEvent{reviewEvent(1, "alice", reviewCommand("cmd-1", 0))}
	if _, _, err := reviewLog.admit(events, len(events), c, "alice", "issuer"); !errors.Is(err, errLogHead) {
		t.Fatal("stale expected head admitted:", err)
	}
	fresh := reviewCommand("cmd-2", 1)
	if _, _, err := reviewLog.admit(events, hubprotocol.MaxReviews, fresh, "alice", "issuer"); !errors.Is(err, errLogHead) {
		t.Fatal("append at the log limit admitted:", err)
	}
}

func TestLogAdmitAcceptsFreshCommand(t *testing.T) {
	events := []ReviewEvent{reviewEvent(1, "alice", reviewCommand("cmd-1", 0))}
	_, replay, err := reviewLog.admit(events, len(events), reviewCommand("cmd-2", 1), "alice", "issuer")
	if err != nil || replay {
		t.Fatal("fresh command refused:", replay, err)
	}
}

func TestLifecycleLogAdmitComparesStructuredCommands(t *testing.T) {
	c := LifecycleCommand{Schema: "readmit-hub-lifecycle-command/v1", ID: "cmd-1", Expected: 0, Kind: "retire", Artifact: "sha256", Reason: "expired"}
	recorded := LifecycleEvent{Schema: "readmit-hub-lifecycle-event/v1", Project: "case", Sequence: 1, Issuer: "issuer", Actor: "alice", Command: c}
	events := []LifecycleEvent{recorded}
	if _, replay, err := lifecycleLog.admit(events, len(events), c, "alice", "issuer"); err != nil || !replay {
		t.Fatal("recorded lifecycle command not replayed:", replay, err)
	}
	changed := c
	changed.Parents = []string{"cmd-0"}
	if _, _, err := lifecycleLog.admit(events, len(events), changed, "alice", "issuer"); !errors.Is(err, errLogIDConflict) {
		t.Fatal("changed lifecycle command under a recorded id admitted:", err)
	}
}
