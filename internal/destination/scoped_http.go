package destination

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/sendpolicy"
)

// ScopedLink resolves a next, reference, discovery or Location link without
// effects. Every linked fetch still needs its own fresh scoped admission.
func ScopedLink(origin, link string) (string, error) {
	base, err := url.Parse(origin)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.Fragment != "" || strings.ContainsAny(origin, "\\\r\n") {
		return "", errors.New("invalid scoped HTTPS origin")
	}
	next, err := url.Parse(link)
	if err != nil || next.User != nil || next.Fragment != "" || strings.ContainsAny(link, "\\\r\n") {
		return "", errors.New("invalid scoped HTTPS link")
	}
	next = base.ResolveReference(next)
	if next.Scheme != "https" || next.Host != base.Host {
		return "", errors.New("cross-origin scoped link refused")
	}
	return next.String(), nil
}

// HTTPResult contains bounded response bytes. It never interprets HTTP success
// as application success, follows a redirect, or retries an uncertain write.
type HTTPResult struct {
	Status int
	Header http.Header
	Body   []byte
}

// ScopedHTTP makes exactly one HTTPS round trip to a newly admitted pinned
// address. Authorize must bind this request's method, URL, body and credentials
// to the approved action. It is checked again immediately before the write.
func ScopedHTTP(ctx context.Context, r ScopedRequest, request *http.Request, security Security, maxBytes int) (HTTPResult, error) {
	refused := errors.New("scoped HTTPS request refused")
	if r.Budget <= 0 || r.Budget > 5*time.Minute {
		return HTTPResult{}, refused
	}
	ctx, cancel := context.WithTimeout(ctx, r.Budget)
	defer cancel()
	if request == nil || request.URL == nil || maxBytes < 1 || maxBytes > 16<<20 {
		return HTTPResult{}, refused
	}
	u := *request.URL
	address := u.Host
	if u.Port() == "" {
		address = net.JoinHostPort(u.Hostname(), "443")
	}
	if address != r.Request.Address || request.Host != "" && request.Host != u.Host {
		return HTTPResult{}, refused
	}
	if _, err := ScopedLink(u.String(), u.String()); err != nil {
		return HTTPResult{}, refused
	}
	allowed := false
	switch r.Request.Operation {
	case sendpolicy.FHIRMetadata, sendpolicy.FHIRSearch, sendpolicy.ObservationRead:
		allowed = request.Method == http.MethodGet
	case sendpolicy.SMARTToken:
		allowed = request.Method == http.MethodPost
	case sendpolicy.FHIRAction, sendpolicy.SetupAction:
		allowed = request.Method == http.MethodPost || request.Method == http.MethodPut || request.Method == http.MethodPatch || request.Method == http.MethodDelete
	}
	if !allowed {
		return HTTPResult{}, refused
	}
	// Snapshot caller-owned request values before any callback. Secret values are
	// never put in a decision or an error. The bounded body cannot be replayed.
	copy := request.Clone(ctx)
	copy.URL = &u
	copy.GetBody = nil
	if request.Body != nil {
		body, err := io.ReadAll(io.LimitReader(request.Body, 16<<20+1))
		if err != nil || len(body) > 16<<20 {
			return HTTPResult{}, refused
		}
		copy.Body = io.NopCloser(bytes.NewReader(body))
		copy.ContentLength = int64(len(body))
	}
	route, err := AdmitScoped(ctx, r)
	if err != nil {
		return HTTPResult{}, refused
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: false, ResponseHeaderTimeout: r.Budget, MaxResponseHeaderBytes: 64 << 10, DialContext: func(context.Context, string, string) (net.Conn, error) { return nil, refused }, DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		conn, err := route.Open(ctx, &security)
		if err != nil {
			return nil, err
		}
		return &authorizedWrite{Conn: conn, ctx: ctx, check: route.Check}, nil
	}}
	defer transport.CloseIdleConnections()
	// An isolated transport has no cached connection on which net/http could
	// retry; route.Open additionally consumes the sole dial admission.
	if route.Check(ctx) != nil {
		return HTTPResult{}, refused
	}
	response, err := transport.RoundTrip(copy)
	if err != nil {
		return HTTPResult{}, refused
	}
	defer response.Body.Close()
	if location := response.Header.Get("Location"); location != "" {
		if _, err := ScopedLink(u.String(), location); err != nil {
			return HTTPResult{}, refused
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(maxBytes)+1))
	if err != nil || len(body) > maxBytes {
		return HTTPResult{}, refused
	}
	return HTTPResult{Status: response.StatusCode, Header: response.Header.Clone(), Body: body}, nil
}

type authorizedWrite struct {
	net.Conn
	ctx   context.Context
	check func(context.Context) error
}

func (c *authorizedWrite) Write(b []byte) (int, error) {
	if err := c.check(c.ctx); err != nil {
		return 0, err
	}
	return c.Conn.Write(b)
}
