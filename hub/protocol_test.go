package hub

import (
	"errors"
	"testing"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

// A protocol decision's refusal reaches the handlers and a restore as the
// hub's own sentinel, so the status a handler maps it to and the message a
// backup verification reports are unchanged; a load's error passes through.
func TestProtocolRefusalsKeepTheHubsSentinels(t *testing.T) {
	for protocol, want := range map[error]error{
		hubprotocol.ErrConflict:  ErrConflict,
		hubprotocol.ErrMissing:   ErrMissing,
		hubprotocol.ErrIntegrity: ErrIntegrity,
		hubprotocol.ErrLimit:     ErrLimit,
		hubprotocol.ErrRefused:   errAccess,
	} {
		if got := hubSentinel(protocol); got != want {
			t.Errorf("%v reached the hub as %v, want %v", protocol, got, want)
		}
	}
	load := errors.New("artifact unreadable")
	if got := hubSentinel(load); got != load {
		t.Errorf("a load's error became %v", got)
	}
	if hubSentinel(nil) != nil {
		t.Error("an accepted command became a refusal")
	}
}
