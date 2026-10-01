package networkaction

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const RuntimeHTTPSchema = "readmit-runtime-http-action/v1"
const RuntimeReceiptSchema = "readmit-runtime-http-receipt/v1"

// RuntimeHTTPSpec binds a memory-only credential provider without changing v1
// direct credential-reference execution. It contains no credential values.
type RuntimeHTTPSpec struct {
	Accept        string   `json:"accept,omitzero"`
	Schema        string   `json:"schema"`
	Authorization string   `json:"authorization"`
	HTTP          HTTPSpec `json:"http"`
}
type RuntimeHTTPPlan struct {
	v2      *RuntimeHTTPSpecV2
	base    *HTTPPlan
	spec    RuntimeHTTPSpec
	binding Binding
}
type RuntimeTarget struct {
	Page        *SearchPageScope
	Contract    string
	ContentType string
	Body        []byte
	Headers     HTTPHeadersV2
	URL, Method string
	Operation   sendpolicy.Operation
	Binding     Binding
	Actor       Actor
	Check       func(context.Context) error
}
type RuntimeProvider interface {
	Identity() string
	Material(context.Context, RuntimeTarget) (RuntimeMaterial, error)
}

// RuntimeMaterial only travels from a trusted protocol adapter into the shared
// transport. Its bytes cannot be formatted or serialized into retained inputs.
type RuntimeMaterial struct {
	bearer, assertion []byte
	check             func(context.Context) error
}

// WhileAuthorized rechecks provider lifecycle at every actual transport write.
func (m RuntimeMaterial) WhileAuthorized(check func(context.Context) error) RuntimeMaterial {
	m.check = check
	return m
}
func BearerMaterial(value []byte) RuntimeMaterial { return RuntimeMaterial{bearer: bytes.Clone(value)} }
func AssertionMaterial(value []byte) RuntimeMaterial {
	return RuntimeMaterial{assertion: bytes.Clone(value)}
}
func (RuntimeMaterial) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, secret.Mask) }
func (RuntimeMaterial) MarshalJSON() ([]byte, error) {
	return nil, errors.New("runtime credentials cannot be serialized")
}

// CredentialRefusal carries only a closed redacted category across adapters.
type CredentialRefusal struct {
	Code string `json:"code"`
}

func (e CredentialRefusal) Error() string {
	return "runtime credential refused: " + safeCredentialCode(e.Code)
}
func safeCredentialCode(code string) string {
	switch code {
	case "registration-missing", "grant-insufficient", "clock-outside-validity", "key-unavailable", "server-unsupported", "authority-changed", "auth-unavailable", "disconnected":
		return code
	}
	return "auth-unavailable"
}
func credentialRefusal(err error) error {
	var coded interface{ CredentialCode() string }
	if errors.As(err, &coded) {
		return CredentialRefusal{Code: safeCredentialCode(coded.CredentialCode())}
	}
	return CredentialRefusal{Code: "auth-unavailable"}
}
func (RuntimeMaterial) MarshalJSONTo(*jsontext.Encoder) error {
	return errors.New("runtime credentials cannot be serialized")
}

type RuntimeReceipt struct {
	Schema     string                         `json:"schema"`
	Binding    Binding                        `json:"binding"`
	Actor      Actor                          `json:"actor"`
	State      string                         `json:"state"`
	HTTPStatus int                            `json:"http_status"`
	Decision   sendpolicy.OperationalDecision `json:"decision"`
}

func PrepareRuntimeHTTP(raw, policy []byte) (*RuntimeHTTPPlan, error) {
	var s RuntimeHTTPSpec
	if len(raw) > 24<<20 || json.Unmarshal(raw, &s, json.RejectUnknownMembers(true)) != nil || s.Schema != RuntimeHTTPSchema || (s.Accept != "" && s.Accept != "application/json" && s.Accept != "application/fhir+json") || s.Authorization != "" && !ValidDigest(s.Authorization) || s.HTTP.Credential != nil {
		return nil, refused
	}
	b, err := json.Marshal(s.HTTP)
	if err != nil {
		return nil, refused
	}
	base, err := PrepareHTTP(b, policy)
	if err != nil {
		return nil, err
	}
	canonical, _ := json.Marshal(s, json.Deterministic(true))
	binding := base.binding
	binding.Configuration = Digest(canonical)
	credential, _ := json.Marshal(struct {
		Authorization string
		PrivateKey    *Credential
	}{s.Authorization, s.HTTP.PrivateKey}, json.Deterministic(true))
	binding.Credentials = Digest(credential)
	return &RuntimeHTTPPlan{base: base, spec: s, binding: binding}, nil
}
func (p *RuntimeHTTPPlan) Binding() Binding { return p.binding }
func (p *RuntimeHTTPPlan) Declaration() RuntimeHTTPSpec {
	var s RuntimeHTTPSpec
	b, _ := json.Marshal(p.spec)
	_ = json.Unmarshal(b, &s)
	return s
}

// Execute performs exactly one request. The protocol adapter owns any explicit
// safe-read retry; this layer never follows redirects or retries a write.
func (p *RuntimeHTTPPlan) Execute(ctx context.Context, authority Authority, resolve sendpolicy.Resolver, provider RuntimeProvider) (HTTPResponse, RuntimeReceipt, error) {
	if p == nil || authority == nil || (p.spec.Authorization == "") != (provider == nil) || provider != nil && provider.Identity() != p.spec.Authorization {
		return HTTPResponse{}, RuntimeReceipt{}, refused
	}
	s := p.spec.HTTP
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.TimeoutMS)*time.Millisecond)
	defer cancel()
	actor, err := authority.Check(ctx, p.binding)
	if err != nil || !CurrentActor(actor) {
		return HTTPResponse{}, RuntimeReceipt{}, refused
	}
	authorityCheck := func(ctx context.Context) error {
		a, err := authority.Check(ctx, p.binding)
		if err != nil || a != actor || !CurrentActor(a) || ctx.Err() != nil {
			return refused
		}
		return nil
	}
	var materialCheck func(context.Context) error
	check := func(ctx context.Context) error {
		if err := authorityCheck(ctx); err != nil {
			return err
		}
		if materialCheck != nil {
			return materialCheck(ctx)
		}
		return nil
	}
	receipt := RuntimeReceipt{Schema: RuntimeReceiptSchema, Binding: p.binding, Actor: actor, State: "refused"}
	u, _ := url.Parse(s.URL)
	address := u.Host
	if u.Port() == "" {
		address = net.JoinHostPort(u.Hostname(), "443")
	}
	route, err := destination.AdmitScoped(ctx, destination.ScopedRequest{Policy: p.base.policy, Request: sendpolicy.ScopedRequest{Project: s.Project, Environment: s.Environment, Endpoint: s.Endpoint, Classification: s.Classification, Address: address, Operation: s.Operation}, Budget: time.Duration(s.TimeoutMS) * time.Millisecond, Resolve: resolve, Authorize: check, Record: func(d sendpolicy.ScopedDecision) error { receipt.Decision = d.Redacted(); return nil }})
	if err != nil {
		return HTTPResponse{}, receipt, refused
	}
	security := destination.Security{ServerName: s.ServerName, Authorities: s.Authorities}
	credentials := [][]byte{}
	if s.PrivateKey != nil {
		if check(ctx) != nil {
			return HTTPResponse{}, receipt, refused
		}
		value, err := (secret.Locator{Command: s.PrivateKey.Locator.Command, Arguments: s.PrivateKey.Locator.Arguments}).Read(ctx)
		if err != nil {
			return HTTPResponse{}, receipt, refused
		}
		cert, err := tls.X509KeyPair(s.Certificate, value.Expose())
		if err != nil {
			return HTTPResponse{}, receipt, refused
		}
		security.Certificate = &cert
		credentials = append(credentials, value.Expose())
	}
	material := RuntimeMaterial{}
	if provider != nil {
		if check(ctx) != nil {
			return HTTPResponse{}, receipt, refused
		}
		target := RuntimeTarget{URL: s.URL, Method: s.Method, Operation: s.Operation, Binding: p.binding, Actor: actor, Check: authorityCheck, Contract: RuntimeHTTPSchema}
		if p.v2 != nil {
			target.Contract = RuntimeHTTPSchemaV2
			target.Body = bytes.Clone(s.Body)
			target.ContentType = s.ContentType
			target.Headers = p.v2.Headers
			if p.v2.Page != nil {
				copy := *p.v2.Page
				target.Page = &copy
			}
		}
		material, err = provider.Material(ctx, target)
		if err != nil {
			return HTTPResponse{}, receipt, credentialRefusal(err)
		}
	}
	materialCheck = material.check
	if check(ctx) != nil {
		return HTTPResponse{}, receipt, refused
	}
	for _, value := range [][]byte{material.bearer, material.assertion} {
		if len(value) > 64<<10 {
			return HTTPResponse{}, receipt, refused
		}
		for _, b := range value {
			if b < 33 || b > 126 {
				return HTTPResponse{}, receipt, refused
			}
		}
	}
	if provider != nil && (len(material.bearer) == 0) == (len(material.assertion) == 0) {
		return HTTPResponse{}, receipt, refused
	}
	body := bytes.Clone(s.Body)
	if len(material.assertion) > 0 {
		if s.Operation != sendpolicy.SMARTToken || len(material.bearer) > 0 {
			return HTTPResponse{}, receipt, refused
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return HTTPResponse{}, receipt, refused
		}
		form.Set("client_assertion", string(material.assertion))
		body = []byte(form.Encode())
	}
	if s.Operation == sendpolicy.SMARTToken && len(material.bearer) > 0 {
		return HTTPResponse{}, receipt, refused
	}
	req, err := http.NewRequestWithContext(ctx, s.Method, s.URL, bytes.NewReader(body))
	if err != nil {
		return HTTPResponse{}, receipt, refused
	}
	if p.v2 != nil {
		p.v2.Headers.apply(req.Header)
	}
	if p.spec.Accept != "" {
		req.Header.Set("Accept", p.spec.Accept)
	}
	if s.ContentType != "" {
		req.Header.Set("Content-Type", s.ContentType)
	}
	if len(material.bearer) > 0 {
		req.Header.Set("Authorization", "Bearer "+string(material.bearer))
	}
	receipt.State = "uncertain"
	response, err := destination.HTTPOnScopedRoute(ctx, route, req, security, s.MaxBytes, time.Duration(s.TimeoutMS)*time.Millisecond)
	if err != nil {
		return HTTPResponse{}, receipt, refused
	}
	receipt.State = "responded"
	receipt.HTTPStatus = response.Status
	// An echo cannot escape into the protocol adapter's raw evidence. Token
	// responses stay opaque and are never themselves part of this receipt.
	credentials = append(credentials, material.bearer, material.assertion)
	released, err := releaseHTTPResponse(response, credentials, p.v2 != nil)
	return released, receipt, err
}
