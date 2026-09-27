package networkaction

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const HTTPSchema = "readmit-network-http-action/v1"
const ResultSchema = "readmit-network-action-result/v1"

type Credential struct {
	Endpoint   string               `json:"endpoint"`
	Purpose    sendpolicy.Operation `json:"purpose"`
	Generation string               `json:"generation"`
	Header     string               `json:"header"`
	Prefix     string               `json:"prefix"`
	Locator    Provider             `json:"locator"`
}
type Provider struct {
	Command   string   `json:"command"`
	Arguments []string `json:"arguments"`
}
type HTTPSpec struct {
	Schema         string               `json:"schema"`
	Plan           string               `json:"plan"`
	Source         string               `json:"source"`
	Environment    string               `json:"environment"`
	Project        string               `json:"project"`
	Revision       string               `json:"revision"`
	Endpoint       string               `json:"endpoint"`
	Classification string               `json:"classification"`
	Operation      sendpolicy.Operation `json:"operation"`
	Method         string               `json:"method"`
	URL            string               `json:"url"`
	Body           []byte               `json:"body"`
	ContentType    string               `json:"content_type"`
	ServerName     string               `json:"server_name"`
	Authorities    []byte               `json:"authorities"`
	Certificate    []byte               `json:"certificate"`
	PrivateKey     *Credential          `json:"private_key,omitzero"`
	Credential     *Credential          `json:"credential,omitzero"`
	TimeoutMS      int64                `json:"timeout_ms"`
	MaxBytes       int                  `json:"max_bytes"`
}
type HTTPPlan struct {
	spec    HTTPSpec
	policy  sendpolicy.ScopedPolicy
	binding Binding
	files   map[string][]byte
}

var hash = regexp.MustCompile(`^[a-f0-9]{64}$`)
var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func PrepareHTTP(raw, policyRaw []byte) (*HTTPPlan, error) {
	var spec HTTPSpec
	if len(raw) > 24<<20 || json.Unmarshal(raw, &spec, json.RejectUnknownMembers(true)) != nil || spec.Schema != HTTPSchema || !hash.MatchString(spec.Plan) || !hash.MatchString(spec.Source) || !identifier.MatchString(spec.Endpoint) || spec.Classification != "nonproduction" || spec.TimeoutMS < 1 || spec.TimeoutMS > 300000 || spec.MaxBytes < 1 || spec.MaxBytes > 16<<20 || len(spec.Body) > 16<<20 || len(spec.Authorities) > 1<<20 || len(spec.Certificate) > 1<<20 {
		return nil, refused
	}
	policy, err := sendpolicy.DecodeScopedPolicy(policyRaw)
	if err != nil || policy.Project != spec.Project || policy.Environment != spec.Environment || policy.Revision != spec.Revision {
		return nil, refused
	}
	u, err := url.Parse(spec.URL)
	if err != nil {
		return nil, refused
	}
	if _, err = destination.ScopedLink(spec.URL, spec.URL); err != nil || len(spec.URL) > 4096 || spec.ServerName == "" {
		return nil, refused
	}
	allowed := false
	switch spec.Operation {
	case sendpolicy.ObservationRead, sendpolicy.FHIRMetadata, sendpolicy.FHIRSearch:
		allowed = spec.Method == "GET" && len(spec.Body) == 0
	case sendpolicy.SMARTToken:
		allowed = spec.Method == "POST"
	case sendpolicy.FHIRAction, sendpolicy.SetupAction:
		allowed = spec.Method == "POST" || spec.Method == "PUT" || spec.Method == "PATCH" || spec.Method == "DELETE"
	}
	if !allowed {
		return nil, refused
	}
	if spec.Operation == sendpolicy.SMARTToken && len(spec.Body) > 0 {
		form, err := url.ParseQuery(string(spec.Body))
		if err != nil || spec.ContentType != "application/x-www-form-urlencoded" {
			return nil, refused
		}
		for key, values := range form {
			if len(values) != 1 {
				return nil, refused
			}
			switch key {
			case "grant_type", "scope", "client_id", "client_assertion_type":
			default:
				return nil, refused
			}
		}
	}

	address := u.Host
	if u.Port() == "" {
		address = net.JoinHostPort(u.Hostname(), "443")
	}
	for i, c := range []*Credential{spec.Credential, spec.PrivateKey} {
		if c == nil {
			continue
		}
		if c.Endpoint != address || c.Purpose != spec.Operation || !identifier.MatchString(c.Generation) || (secret.Locator{Command: c.Locator.Command, Arguments: c.Locator.Arguments}).Validate() != nil {
			return nil, refused
		}
		if i == 0 && !((c.Header == "Authorization" && (c.Prefix == "" || c.Prefix == "Bearer " || c.Prefix == "Basic ")) || (spec.Operation == sendpolicy.SMARTToken && c.Header == "client_assertion" && c.Prefix == "")) {
			return nil, refused
		}
		if i == 1 && (c.Header != "" || c.Prefix != "") {
			return nil, refused
		}
	}
	if (len(spec.Certificate) > 0) != (spec.PrivateKey != nil) {
		return nil, refused
	}
	if len(spec.ContentType) > 128 || bytes.ContainsAny([]byte(spec.ContentType), "\r\n") {
		return nil, refused
	}
	config, _ := json.Marshal(spec, json.Deterministic(true))
	credential, _ := json.Marshal([]*Credential{spec.Credential, spec.PrivateKey}, json.Deterministic(true))
	return &HTTPPlan{spec: spec, policy: policy, files: map[string][]byte{"action.json": config, "policy.json": bytes.Clone(policyRaw)}, binding: Binding{Plan: spec.Plan, Source: spec.Source, Configuration: Digest(config), Policy: Digest(policyRaw), Credentials: Digest(credential), Project: spec.Project, Environment: spec.Environment, Revision: spec.Revision, Endpoint: spec.Endpoint, Operation: spec.Operation}}, nil
}
func (p *HTTPPlan) Binding() Binding { return p.binding }

// ResponseBody must be explicitly exposed to its protocol adapter. Formatting
// and JSON cannot accidentally log token responses or echoed credentials.
type ResponseBody struct{ raw []byte }

func (b ResponseBody) Expose() []byte           { return bytes.Clone(b.raw) }
func (ResponseBody) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, secret.Mask) }
func (ResponseBody) MarshalJSON() ([]byte, error) {
	return nil, errors.New("response bytes require explicit private handling")
}

type HTTPResponse struct {
	header http.Header
	Status int
	Body   ResponseBody
}
type Result struct {
	ResponseDigest   string  `json:"response_digest,omitzero"`
	Schema           string  `json:"schema"`
	Binding          Binding `json:"binding"`
	Actor            Actor   `json:"actor"`
	State            string  `json:"state"`
	HTTPStatus       int     `json:"http_status"`
	ResponseRetained bool    `json:"response_retained"`
}

var family = artifactdir.Family{Layout: artifactdir.Layout{Noun: "network action", RequiredFiles: []string{"action.json", "policy.json", "result.json", "identity.sha256"}, AllowFile: func(n string) bool {
	switch n {
	case "action.json", "policy.json", "intent.json", "decision.json", "operation.json", "response.bin", "result.json", "identity.sha256":
		return true
	}
	return false
}, MaxFiles: 8, MaxFileBytes: 24 << 20, MaxBytes: 48 << 20}, Seal: artifactdir.DirectoryHash(ResultSchema)}

func put(w *artifactdir.Writer, name string, v any) error {
	b, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return err
	}
	return w.WriteFile(name, b)
}
func (p *HTTPPlan) Execute(ctx context.Context, authority Authority, output string, resolve sendpolicy.Resolver) (HTTPResponse, Result, error) {
	if p == nil || authority == nil {
		return HTTPResponse{}, Result{}, refused
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.spec.TimeoutMS)*time.Millisecond)
	defer cancel()
	actor, err := authority.Check(ctx, p.binding)
	if err != nil || !validActor(actor) {
		return HTTPResponse{}, Result{}, refused
	}
	check := func(ctx context.Context) error {
		current, err := authority.Check(ctx, p.binding)
		if err != nil || current != actor || !validActor(current) || ctx.Err() != nil {
			return refused
		}
		return nil
	}
	w, err := artifactdir.Create(output, family, artifactdir.Durable)
	if err != nil {
		return HTTPResponse{}, Result{}, refused
	}
	defer w.Close()
	for n, b := range p.files {
		if w.WriteFile(n, b) != nil {
			return HTTPResponse{}, Result{}, refused
		}
	}
	result := Result{Schema: ResultSchema, Binding: p.binding, Actor: actor, State: "refused"}
	spec := p.spec
	u, _ := url.Parse(spec.URL)
	address := u.Host
	if u.Port() == "" {
		address = net.JoinHostPort(u.Hostname(), "443")
	}
	decisionRecorded := false
	request := destination.ScopedRequest{Policy: p.policy, Request: sendpolicy.ScopedRequest{Project: spec.Project, Environment: spec.Environment, Endpoint: spec.Endpoint, Classification: spec.Classification, Address: address, Operation: spec.Operation}, Budget: time.Duration(spec.TimeoutMS) * time.Millisecond, Resolve: resolve, Authorize: check, Record: func(d sendpolicy.ScopedDecision) error {
		if err := put(w, "decision.json", d); err != nil {
			return err
		}
		if err := put(w, "operation.json", d.Redacted()); err != nil {
			return err
		}
		if err := w.Sync(); err != nil {
			return err
		}
		decisionRecorded = true
		return nil
	}}
	// Destination admission precedes provider invocation. The sealed one-use route
	// is passed to the same HTTPS implementation; there is no second DNS lookup.
	route, err := destination.AdmitScoped(ctx, request)
	if err != nil {
		if !decisionRecorded {
			return HTTPResponse{}, result, refused
		}
		if finish(w, result) != nil {
			return HTTPResponse{}, Result{}, refused
		}
		return HTTPResponse{}, result, refused
	}
	security := destination.Security{ServerName: spec.ServerName, Authorities: spec.Authorities}
	secrets := [][]byte{}
	resolveCredential := func(c *Credential) ([]byte, error) {
		if check(ctx) != nil {
			return nil, refused
		}
		v, err := (secret.Locator{Command: c.Locator.Command, Arguments: c.Locator.Arguments}).Read(ctx)
		if err != nil {
			return nil, refused
		}
		secrets = append(secrets, v.Expose())
		return v.Expose(), nil
	}
	req, err := http.NewRequestWithContext(ctx, spec.Method, spec.URL, bytes.NewReader(spec.Body))
	if err != nil {
		return HTTPResponse{}, Result{}, refused
	}
	if spec.ContentType != "" {
		req.Header.Set("Content-Type", spec.ContentType)
	}
	if spec.PrivateKey != nil {
		key, err := resolveCredential(spec.PrivateKey)
		if err != nil {
			return HTTPResponse{}, Result{}, refused
		}
		cert, err := tls.X509KeyPair(spec.Certificate, key)
		if err != nil {
			return HTTPResponse{}, Result{}, refused
		}
		security.Certificate = &cert
	}
	if spec.Credential != nil {
		value, err := resolveCredential(spec.Credential)
		if err != nil {
			return HTTPResponse{}, Result{}, refused
		}
		for _, c := range value {
			if c < 32 || c > 126 {
				return HTTPResponse{}, Result{}, refused
			}
		}
		if spec.Credential.Header == "client_assertion" {
			form, _ := url.ParseQuery(string(spec.Body))
			form.Set("client_assertion", string(value))
			body := []byte(form.Encode())
			req.Body = io.NopCloser(bytes.NewReader(body))
			req.ContentLength = int64(len(body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			req.Header.Set(spec.Credential.Header, spec.Credential.Prefix+string(value))
		}
	}
	if check(ctx) != nil {
		return HTTPResponse{}, Result{}, refused
	}
	if put(w, "intent.json", struct {
		Binding Binding `json:"binding"`
		State   string  `json:"state"`
	}{p.binding, "uncertain-until-settled"}) != nil || w.Sync() != nil {
		return HTTPResponse{}, Result{}, refused
	}
	response, requestErr := destination.HTTPOnScopedRoute(ctx, route, req, security, spec.MaxBytes, time.Duration(spec.TimeoutMS)*time.Millisecond)
	result.State = "uncertain"
	if requestErr == nil {
		result.State = "responded"
		result.HTTPStatus = response.Status
		retain := spec.Operation != sendpolicy.SMARTToken
		for _, value := range secrets {
			if len(value) > 0 && bytes.Contains(response.Body, value) {
				retain = false
			}
		}
		if retain {
			if w.WriteFile("response.bin", response.Body) != nil {
				return HTTPResponse{}, Result{}, refused
			}
			result.ResponseRetained = true
			result.ResponseDigest = Digest(response.Body)
		}
	}
	if finish(w, result) != nil {
		return HTTPResponse{}, Result{}, refused
	}
	if requestErr != nil {
		return HTTPResponse{}, result, refused
	}
	return HTTPResponse{Status: response.Status, Body: ResponseBody{raw: response.Body}, header: safeHeaders(response.Header)}, result, nil
}
func validActor(a Actor) bool {
	return RecordedActor(a) && a.Expires.After(time.Now())
}
func finish(w *artifactdir.Writer, r Result) error {
	if err := put(w, "result.json", r); err != nil {
		return err
	}
	_, err := w.Seal(nil)
	return err
}

func (p *HTTPPlan) Declaration() HTTPSpec {
	var s HTTPSpec
	raw, _ := json.Marshal(p.spec)
	_ = json.Unmarshal(raw, &s)
	return s
}
func (r HTTPResponse) Header(name string) string {
	switch name {
	case "Date", "Age", "Content-Type", "Link", "Location":
		return r.header.Get(name)
	}
	return ""
}

func safeHeaders(header http.Header) http.Header {
	out := http.Header{}
	for _, name := range []string{"Date", "Age", "Content-Type", "Link", "Location"} {
		if value := header.Get(name); value != "" {
			out.Set(name, value)
		}
	}
	return out
}
func (HTTPResponse) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, "HTTP response (private)") }
func (HTTPResponse) MarshalJSON() ([]byte, error) {
	return nil, errors.New("HTTP response requires explicit private handling")
}

func CurrentActor(a Actor) bool { return validActor(a) }

func RecordedActor(a Actor) bool {
	return (a.Kind == "runner" || a.Kind == "action-review") && identifier.MatchString(a.ID) && identifier.MatchString(a.Generation) && hash.MatchString(a.EvidenceIdentity) && !a.Expires.IsZero()
}
func ValidDigest(value string) bool { return hash.MatchString(value) }
