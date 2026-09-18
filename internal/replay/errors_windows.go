package replay

import (
	"errors"
	"syscall"
)

// Winsock's refusal code is distinct from syscall's POSIX compatibility code.
// The standard library exposes reset/aborted constants but not WSAECONNREFUSED.
const wsaConnectionRefused syscall.Errno = 10061

func connectionRefused(err error) bool {
	return errors.Is(err, wsaConnectionRefused) || errors.Is(err, syscall.ECONNREFUSED)
}
func connectionDisconnected(err error) bool {
	return errors.Is(err, syscall.WSAECONNRESET) || errors.Is(err, syscall.WSAECONNABORTED) || errors.Is(err, syscall.ERROR_NETNAME_DELETED) || errors.Is(err, syscall.EPIPE)
}
