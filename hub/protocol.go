package hub

import (
	"errors"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

// The review and lifecycle protocol is the shared strict package both the hub
// service and the application's hub client read. The hub keeps its own names
// so its callers and tests are unchanged; there is one definition.
type (
	ReviewCommand    = hubprotocol.ReviewCommand
	ReviewEvent      = hubprotocol.ReviewEvent
	LifecycleCommand = hubprotocol.LifecycleCommand
	LifecycleEvent   = hubprotocol.LifecycleEvent
)

// errAccess is the protocol's refusal, so a strict reader here and one there
// refuse with the same error.
var errAccess = hubprotocol.ErrRefused

// The hub names documents in the protocol's grammar.
func validProject(s string) bool        { return hubprotocol.ValidProject(s) }
func validDigest(d string) bool         { return hubprotocol.ValidDigest(d) }
func reviewText(s string, max int) bool { return hubprotocol.ValidText(s, max) }
func requireExactMembers(data []byte, names ...string) error {
	return hubprotocol.RequireExactMembers(data, names...)
}

// hubSentinel is a protocol decision's refusal as the hub's own sentinel, so the
// status a handler maps it to and the message a restore reports are the ones
// they were. Any other error, such as a load's, is returned unchanged.
func hubSentinel(e error) error {
	switch {
	case errors.Is(e, hubprotocol.ErrConflict):
		return ErrConflict
	case errors.Is(e, hubprotocol.ErrMissing):
		return ErrMissing
	case errors.Is(e, hubprotocol.ErrIntegrity):
		return ErrIntegrity
	case errors.Is(e, hubprotocol.ErrLimit):
		return ErrLimit
	}
	return e
}
