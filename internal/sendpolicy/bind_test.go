package sendpolicy

import (
	"errors"
	"testing"
)

// TestBindAddressRestrictsNonloopbackBinds checks the default: a socket that
// would accept connections from beyond this machine is refused until the
// operator explicitly approves it.
func TestBindAddressRestrictsNonloopbackBinds(t *testing.T) {
	for _, c := range []struct {
		address string
		want    error
	}{
		{"127.0.0.1:2575", nil},
		{"127.0.0.1:0", nil},
		{"127.9.9.9:2575", nil},
		{"[::1]:2575", nil},
		{"0.0.0.0:2575", ErrNonloopbackBind},
		{"[::]:2575", ErrNonloopbackBind},
		{":2575", ErrNonloopbackBind},
		{"198.51.100.7:2575", ErrNonloopbackBind},
		{"localhost:2575", ErrNonloopbackBind},
		{"127.0.0.1", ErrBindAddress},
		{"", ErrBindAddress},
		{"127.0.0.1:mllp", ErrBindAddress},
		{"127.0.0.1:65536", ErrBindAddress},
		{"127.0.0.1:-1", ErrBindAddress},
	} {
		t.Run(c.address, func(t *testing.T) {
			if err := BindAddress(c.address, false); !errors.Is(err, c.want) {
				t.Fatalf("BindAddress(%q, false) = %v, want %v", c.address, err, c.want)
			}
		})
	}
}

// TestApprovedBindIsExplicit checks that approval changes exactly one thing:
// the loopback requirement, never the address shape a socket needs.
func TestApprovedBindIsExplicit(t *testing.T) {
	for _, address := range []string{"0.0.0.0:2575", ":2575", "198.51.100.7:0", "localhost:2575"} {
		if err := BindAddress(address, true); err != nil {
			t.Fatalf("BindAddress(%q, true) = %v", address, err)
		}
	}
	if err := BindAddress("0.0.0.0:mllp", true); !errors.Is(err, ErrBindAddress) {
		t.Fatalf("an approved bind accepted an address that is not one: %v", err)
	}
}
