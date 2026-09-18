package sendpolicy

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
)

// ErrBindAddress and ErrNonloopbackBind name the two ways a listen address is
// refused. They are exported so a command can report the refusal as its own
// diagnostic without restating the rule.
var (
	ErrBindAddress     = errors.New("a listen address is a literal IP address and a numeric port, such as 127.0.0.1:2575")
	ErrNonloopbackBind = errors.New("accepting connections from beyond this machine is opt-in: pass --approved-bind to bind a nonloopback address")
)

// BindAddress refuses a listening socket that would accept connections from
// beyond this machine unless the operator explicitly approved it. A nonloopback
// bind is never a default.
//
// A name is not resolved to decide this. A name commonly used for loopback is
// still a name, and resolving one would rest the decision on DNS; the same
// reasoning refuses an unapproved hostname in a target configuration.
func BindAddress(address string, approved bool) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return ErrBindAddress
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 0 || number > 65535 {
		return ErrBindAddress
	}
	if approved {
		return nil
	}
	// An empty host is every interface, which is the widest bind there is.
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Unmap().IsLoopback() {
		return ErrNonloopbackBind
	}
	return nil
}
