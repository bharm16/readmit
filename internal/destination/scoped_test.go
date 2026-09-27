package destination_test

import (
	"context"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

func scopeRequest(address string, operation sendpolicy.Operation) destination.ScopedRequest {
	host, port, _ := net.SplitHostPort(address)
	n, _ := strconv.Atoi(port)
	ip := netip.MustParseAddr(host)
	bits := 128
	if ip.Is4() {
		bits = 32
	}
	return destination.ScopedRequest{Policy: sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "receiver", Operation: operation, Port: n, Destinations: []string{netip.PrefixFrom(ip, bits).String()}, Selection: "single-address"}}}, Request: sendpolicy.ScopedRequest{Project: "lab", Environment: "test", Endpoint: "receiver", Operation: operation, Classification: "nonproduction", Address: address}, Budget: time.Second, Authorize: func(context.Context) error { return nil }, Record: func(sendpolicy.ScopedDecision) error { return nil }}
}
func TestScopedHTTPPinsVerifiedTLSAndNeverFollowsRedirectsOrProxy(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "https://other.invalid/token")
			w.WriteHeader(302)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer server.Close()
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	r := scopeRequest(server.Listener.Addr().String(), sendpolicy.FHIRSearch)
	security := destination.Security{Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/search", nil)
	req.Header.Set("Authorization", "Bearer synthetic-secret")
	got, err := destination.ScopedHTTP(context.Background(), r, req, security, 100)
	if err != nil || string(got.Body) != "ok" || got.Status != 200 {
		t.Fatalf("request: %+v %v", got, err)
	}
	req.URL.Path = "/redirect"
	if _, err := destination.ScopedHTTP(context.Background(), r, req, security, 100); err == nil {
		t.Fatal("cross-origin Location accepted")
	}
	if calls.Load() != 2 {
		t.Fatal("redirect followed")
	}
	req.Method = http.MethodPost
	if _, err := destination.ScopedHTTP(context.Background(), r, req, security, 100); err == nil {
		t.Fatal("read grant allowed write")
	}
	security.ServerName = "wrong.invalid"
	req.Method = http.MethodGet
	if _, err := destination.ScopedHTTP(context.Background(), r, req, security, 100); err == nil {
		t.Fatal("wrong certificate name")
	}
	if calls.Load() != 2 {
		t.Fatal("payload sent without TLS verification")
	}
}
func TestScopedRouteRevalidatesAndConsumesAdmission(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	r := scopeRequest(listener.Addr().String(), sendpolicy.V2Stimulus)
	valid := true
	r.Authorize = func(context.Context) error {
		if !valid {
			return errors.New("revoked")
		}
		return nil
	}
	route, err := destination.AdmitScoped(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	valid = false
	if _, err := route.Open(context.Background(), nil); err == nil {
		t.Fatal("revoked admission")
	}
	valid = true
	if _, err := route.Open(context.Background(), nil); err == nil {
		t.Fatal("reused admission")
	}
	r.Request.Operation = sendpolicy.CaptureListen
	if _, err := destination.AdmitScoped(context.Background(), r); err == nil {
		t.Fatal("stimulus grant allowed capture")
	}
}
func TestScopedLinkRefusesCrossOriginReferences(t *testing.T) {
	for _, link := range []string{"https://other.invalid/path", "//other.invalid/path", "http://same.invalid/path", "https://user@same.invalid/path", "https://same.invalid/path#fragment", "\\\\other.invalid"} {
		if _, err := destination.ScopedLink("https://same.invalid/base", link); err == nil {
			t.Fatalf("accepted %s", link)
		}
	}
	if got, err := destination.ScopedLink("https://same.invalid/base", "?page=2"); err != nil || got != "https://same.invalid/base?page=2" {
		t.Fatal(got, err)
	}
}

func TestScopedRoutePinsDNSAndRechecksOnNextAdmission(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	r := scopeRequest(listener.Addr().String(), sendpolicy.V2Stimulus)
	r.Request.Address = net.JoinHostPort("receiver.invalid", port)
	r.Policy.Rules[0].Destinations = []string{"127.0.0.0/8"}
	r.Policy.Rules[0].Selection = "lowest-address"
	calls := 0
	r.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		calls++
		if calls == 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.2"), netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("198.51.100.1")}, nil
	}
	route, err := destination.AdmitScoped(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := route.Open(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
	if calls != 1 {
		t.Fatal("dial resolved again")
	}
	if _, err = destination.AdmitScoped(context.Background(), r); err == nil {
		t.Fatal("rebound address allowed")
	}
	if calls != 2 {
		t.Fatal("new admission did not resolve")
	}
}

func TestScopedAdmissionDoesNotResetConnectBudgetAfterDNS(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	r := scopeRequest(l.Addr().String(), sendpolicy.V2Stimulus)
	r.Budget = 100 * time.Millisecond
	r.Request.Address = net.JoinHostPort("receiver.invalid", port)
	r.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		time.Sleep(20 * time.Millisecond)
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	route, err := destination.AdmitScoped(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	// Time spent obtaining a private key after DNS consumes the same deadline.
	time.Sleep(r.Budget)
	if c, err := route.Open(context.Background(), nil); err == nil {
		c.Close()
		t.Fatal("dial received a fresh connection budget")
	}
}
