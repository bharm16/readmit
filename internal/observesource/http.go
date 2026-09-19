package observesource

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/transportsecurity"
)

// newReader opens the one reader the declared source kind names. A file export
// reaches no destination and opens nothing until it is read; an HTTP endpoint
// is decided against the approved-destination policy before any socket exists.
func newReader(ctx context.Context, source Source, retained *snapshot, options Options) (reader, error) {
	maxAge, err := boundedDuration(source.Freshness.MaxAge, maxFreshness)
	if err != nil {
		return nil, errors.New("a freshness bound is a positive duration of at most one week")
	}
	switch source.Observes.Kind {
	case FileExport:
		return &fileReader{extraction: *source.Extraction, export: *source.File, maxAge: maxAge}, nil
	case DownstreamCapture:
		return &captureReader{capture: *source.Capture, maxAge: maxAge}, nil
	}
	return newHTTPReader(ctx, source, retained, options, maxAge)
}

// httpReader reads one approved API endpoint over TLS. Verification is always
// on, there is no plaintext mode, no redirect is followed, and no proxy is
// taken from the environment: an observation reads the endpoint an operator
// selected or it reads nothing.
type httpReader struct {
	extraction Extraction
	endpoint   HTTP
	client     *http.Client
	header     string
	locator    secret.Locator
	// value is the credential this command resolved, held for the duration of
	// the command that resolved it and never written anywhere. It is resolved
	// once, on the first read that needs it.
	value   *secret.Value
	maxAge  time.Duration
	timeout time.Duration
}

func (r *httpReader) close() { r.client.CloseIdleConnections() }

// newHTTPReader decides the destination, records that decision as evidence,
// and only then builds the client. The decision is reached before anything is
// opened, and a refused destination stops the collection rather than becoming
// an observation that the endpoint held nothing.
func newHTTPReader(ctx context.Context, source Source, retained *snapshot, options Options, maxAge time.Duration) (reader, error) {
	endpoint := *source.HTTP
	address, err := endpoint.Endpoint()
	if err != nil {
		return nil, err
	}
	locator, header, err := endpoint.BindCredential()
	if err != nil {
		return nil, err
	}
	request, err := endpoint.PolicyRequest()
	if err != nil {
		return nil, err
	}
	resolve := options.Resolve
	if resolve == nil {
		resolve = sendpolicy.SystemResolver
	}
	timeout, err := boundedDuration(endpoint.Timeout, maxTimeout)
	if err != nil {
		return nil, errors.New("an http timeout is a positive duration of at most five minutes")
	}
	decisionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	decision := sendpolicy.Decide(decisionCtx, options.Policy, request, resolve)
	// The decision is retained before it is acted on, and a failure to retain
	// it stops the collection: a destination check nobody can read afterwards
	// is not evidence that one was made.
	if err := retained.retainDecision(decision); err != nil {
		return nil, err
	}
	if !decision.Allowed || len(decision.ResolvedAddresses) != 1 {
		return nil, errors.New("the observation was refused by policy before anything was opened: " + string(decision.Reason))
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("an http endpoint names one explicit host and numeric port")
	}
	// The connection uses the exact address the policy checked, never a fresh
	// resolution of the configured name. TLS still verifies the configured
	// server name against the certificate the endpoint presents.
	checked := net.JoinHostPort(decision.ResolvedAddresses[0], port)
	config, err := endpoint.tlsConfig(address)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:           nil,
		TLSClientConfig: config,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, checked)
		},
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return &httpReader{extraction: *source.Extraction, endpoint: endpoint, client: client,
		header: header, locator: locator, maxAge: maxAge, timeout: timeout}, nil
}

// tlsConfig is the client configuration this endpoint is read under. The rule
// itself belongs to internal/transportsecurity, which every path readmit
// negotiates TLS on shares: TLS 1.2 is the floor, certificate verification is
// always on, there is no insecure mode, and an explicitly configured
// certificate authority replaces the platform roots. This decides only which
// name is verified and reads the authority the source declared.
func (h HTTP) tlsConfig(address string) (*tls.Config, error) {
	name := h.ServerName
	if name == "" {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("an http endpoint names one explicit host and numeric port")
		}
		name = host
	}
	var authorities []byte
	if h.CAFile != "" {
		read, err := readBounded(h.CAFile, MaxCABytes)
		if err != nil {
			return nil, errors.New("cannot read the configured CA certificates")
		}
		authorities = read
	}
	return transportsecurity.ClientConfig(name, authorities)
}

// read takes one bounded read of the endpoint, retrying only what is safe to
// retry. A read that produced no answer at all — a connection that was refused
// or lost, a read that ran out of its own time, and the four statuses that mean
// "ask again" — may be attempted again within the declared bound. Nothing else
// is: a stale, ambiguous, truncated, refused or unauthorized answer is an
// answer, and attempting it again until it changes would turn an uncertain read
// into a confident one.
func (r *httpReader) read(ctx context.Context) attempt {
	value, err := r.credential(ctx)
	if err != nil {
		refused := attempt{at: time.Now(), record: Evidence{Kind: HTTPAPI, Attempts: 1}}
		return failure(refused, observewindow.SampleFailed, err.Error())
	}
	delay, _ := time.ParseDuration(r.endpoint.Retry.Delay)
	for attempts := 1; ; attempts++ {
		result, retryable := r.once(ctx, value)
		result.record.Attempts = attempts
		result.record.Retries = attempts - 1
		if !retryable || attempts > r.endpoint.Retry.Attempts {
			return result
		}
		if err := waitFor(ctx, delay); err != nil {
			return result
		}
	}
}

// credential resolves the referenced value once, at the moment it is first
// needed, through the one mechanism readmit has for reading a value out of a
// store it does not own. The value lives only inside this command.
func (r *httpReader) credential(ctx context.Context) (*secret.Value, error) {
	if r.header == "" {
		return nil, nil
	}
	if r.value != nil {
		return r.value, nil
	}
	resolved, err := r.locator.Read(ctx)
	if err != nil {
		return nil, errors.New("the credential this endpoint presents could not be read from its declared store")
	}
	// The value is checked for what it is, never repeated. A credential a
	// request header cannot carry is refused here rather than at the transport,
	// where the diagnostic would describe it as an unreachable endpoint.
	for _, b := range resolved.Expose() {
		if b < 0x20 || b > 0x7e {
			return nil, errors.New("the credential this endpoint presents cannot be carried in a request header")
		}
	}
	r.value = &resolved
	return r.value, nil
}

// once performs exactly one bounded request and classifies what came back. The
// boolean reports whether the condition it hit is one a retry could honestly
// change.
func (r *httpReader) once(ctx context.Context, value *secret.Value) (attempt, bool) {
	taken := attempt{at: time.Now(), record: Evidence{Kind: HTTPAPI}}
	attemptCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, r.endpoint.URL, nil)
	if err != nil {
		return failure(taken, observewindow.SampleFailed, "the declared endpoint could not be requested"), false
	}
	if value != nil {
		request.Header.Set(r.header, string(value.Expose()))
	}
	response, err := r.client.Do(request)
	// The read is dated when the endpoint answered, not when it was asked, so
	// an answer generated while the request was in flight is never dated
	// before the response that carries it.
	taken.at = time.Now()
	if err != nil {
		// Nothing was answered, so nothing was observed. A further attempt can
		// honestly change that, within the declared bound.
		return failure(taken, observewindow.SampleFailed, "the endpoint could not be reached or the connection was lost"), true
	}
	defer response.Body.Close()
	taken.record.HTTPStatus = response.StatusCode
	if refusal, note, retryable := classify(response.StatusCode); refusal != "" {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, int64(r.endpoint.MaxBytes)))
		return failure(taken, refusal, note), retryable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(r.endpoint.MaxBytes)+1))
	if err != nil {
		return failure(taken, observewindow.SampleFailed, "the response body could not be read in full"), true
	}
	if len(body) > r.endpoint.MaxBytes {
		return failure(taken, observewindow.SampleTruncated, "the response is larger than the declared read bound, so a prefix of it would be read rather than the source"), false
	}
	taken.record.Bytes = len(body)
	age, stated := responseAge(response, taken.at)
	if !stated {
		return failure(taken, observewindow.SampleAmbiguous, "the response states neither its age nor when it was generated, so how current it is has no single reading"), false
	}
	if !dateState(&taken, taken.at.Add(-age), r.maxAge) {
		return failure(taken, observewindow.SampleStale, "the response states an age past the declared freshness bound, so it describes state from before this window"), false
	}
	taken.evidence = map[string][]byte{"body": body}
	keys, err := divide(r.extraction, body)
	if err != nil {
		return failure(taken, observewindow.SampleAmbiguous, err.Error()), false
	}
	taken.status = observewindow.Observed
	taken.keys = keys
	return taken, false
}

// classify reads one response status once: how it refused the read, in fixed
// wording that repeats no body and no header, and whether a retry could
// honestly change it. The empty status is the one status this collector reads
// as an answer, because only 200 is an answer. A redirection answered about
// some other resource, so it has no single reading; every other status refused,
// and four of them are the endpoint asking to be asked again.
func classify(status int) (observewindow.SampleStatus, string, bool) {
	switch {
	case status == http.StatusOK:
		return "", "", false
	case status >= 300 && status < 400:
		return observewindow.SampleAmbiguous, "the endpoint redirected the read, so which resource answered has no single reading", false
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return observewindow.SampleFailed, "the endpoint refused the read as unauthorized, which observed nothing rather than observing that nothing is there", false
	case status == http.StatusNotFound:
		return observewindow.SampleFailed, "the endpoint reports that the declared scope is not there to read, which is not a reading of an empty scope", false
	case status == http.StatusTooManyRequests, status == http.StatusBadGateway,
		status == http.StatusServiceUnavailable, status == http.StatusGatewayTimeout:
		return observewindow.SampleFailed, "the endpoint asked to be asked again and was, within the declared bound", true
	case status >= 500:
		return observewindow.SampleFailed, "the endpoint failed to answer the read", false
	default:
		return observewindow.SampleFailed, "the endpoint refused the read", false
	}
}

// responseAge is how old the state a response carries is, as the response
// itself states it. Age is preferred because a response carrying one came from
// a cache and says so. A response stating neither is not treated as current.
func responseAge(response *http.Response, at time.Time) (time.Duration, bool) {
	if stated := response.Header.Get("Age"); stated != "" {
		seconds, err := strconv.Atoi(stated)
		if err != nil || seconds < 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	generated, err := http.ParseTime(response.Header.Get("Date"))
	if err != nil {
		return 0, false
	}
	age := at.Sub(generated)
	if age < 0 {
		return 0, false
	}
	return age, true
}
