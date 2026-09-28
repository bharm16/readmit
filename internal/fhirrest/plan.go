// Package fhirrest executes a finite reviewed R4 HTTP plan and retains the
// complete acquisition trail. HTTP outcomes are not clinical workflow verdicts.
package fhirrest

import (
	"context"
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrequest"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const PlanSchema = "readmit-fhir-http-plan/v1"
const ResultSchema = "readmit-fhir-http-result/v1"

var refused = errors.New("FHIR execution refused; verify the reviewed request, capability evidence and authority")

type Budget struct {
	Pages     int   `json:"pages"`
	Rows      int   `json:"rows"`
	Bytes     int   `json:"bytes"`
	TimeoutMS int64 `json:"timeout_ms"`
}
type Retry struct {
	MaxAttempts int   `json:"max_attempts"`
	MaxDelayMS  int64 `json:"max_delay_ms"`
}

// Valid reports whether a budget is within the executor's fixed bounds.
func (b Budget) Valid() bool {
	return b.Pages >= 1 && b.Pages <= 64 && b.Rows >= 1 && b.Rows <= 16384 && b.Bytes >= 1 && b.Bytes <= 128<<20 && b.TimeoutMS >= 1 && b.TimeoutMS <= 300000
}

// Valid reports whether a retry policy is within the executor's fixed bounds.
func (r Retry) Valid() bool {
	return r.MaxAttempts >= 1 && r.MaxAttempts <= 4 && r.MaxDelayMS >= 0 && r.MaxDelayMS <= 10000
}

type Prior struct {
	URL    string `json:"url"`
	ETag   string `json:"etag"`
	Body   []byte `json:"body"`
	SHA256 string `json:"sha256"`
}
type Spec struct {
	CapabilityHTTP *networkaction.RuntimeHTTPSpecV2 `json:"capability_http,omitzero"`
	PagePrivateKey *networkaction.Credential        `json:"page_private_key,omitzero"`
	Schema         string                           `json:"schema"`
	Base           string                           `json:"base"`
	HTTP           networkaction.RuntimeHTTPSpecV2  `json:"http"`
	Capability     []byte                           `json:"capability"`
	Budget         Budget                           `json:"budget"`
	Retry          Retry                            `json:"retry"`
	Prior          *Prior                           `json:"prior,omitzero"`
	Projection     *fhirr4.Projection               `json:"projection,omitzero"`
}
type Plan struct {
	spec        Spec
	raw, policy []byte
	binding     networkaction.Binding
	request     fhirrequest.Request
	identity    string
}

func encode(v any) []byte { b, _ := json.Marshal(v, json.Deterministic(true)); return b }
func parsedRequest(base string, s networkaction.RuntimeHTTPSpecV2) (fhirrequest.Request, error) {
	if s.Page != nil {
		return fhirrequest.Page(base, s.Page.Resource, s.HTTP.URL)
	}
	h := s.Headers
	return fhirrequest.Parse(base, s.HTTP.Method, s.HTTP.URL, s.HTTP.ContentType, s.HTTP.Body, fhirrequest.Headers{IfMatch: h.IfMatch, IfNoneMatch: h.IfNoneMatch, IfModifiedSince: h.IfModifiedSince, IfNoneExist: h.IfNoneExist, Prefer: h.Prefer})
}
func Prepare(raw, policy []byte) (*Plan, error) {
	var s Spec
	if len(raw) > 32<<20 || json.Unmarshal(raw, &s, json.RejectUnknownMembers(true)) != nil || s.Schema != PlanSchema || s.HTTP.Schema != networkaction.RuntimeHTTPSchemaV2 || s.HTTP.Page != nil || s.HTTP.Accept != "application/fhir+json" || !s.Budget.Valid() || !s.Retry.Valid() {
		return nil, refused
	}
	request, e := parsedRequest(s.Base, s.HTTP)
	if e != nil {
		return nil, refused
	}
	if s.HTTP.HTTP.Method == "GET" {
		if s.HTTP.HTTP.Operation != sendpolicy.FHIRSearch && s.HTTP.HTTP.Operation != sendpolicy.FHIRMetadata && s.HTTP.HTTP.Operation != sendpolicy.ObservationRead {
			return nil, refused
		}
	} else if s.HTTP.HTTP.Operation != sendpolicy.FHIRAction && s.HTTP.HTTP.Operation != sendpolicy.SetupAction {
		return nil, refused
	}
	needsCleanup := request.Kind == "delete"
	for _, entry := range request.Entries {
		if entry.Kind == "delete" {
			needsCleanup = true
		}
		if (entry.Kind == "update" || entry.Kind == "patch") && entry.VersionMatch == "" || entry.ConditionalRead {
			return nil, refused
		}
	}
	if needsCleanup && s.HTTP.HTTP.Operation != sendpolicy.SetupAction {
		return nil, refused
	}
	if (request.Kind == "update" || request.Kind == "patch") && s.HTTP.Headers.IfMatch == "" {
		return nil, refused
	}
	if s.Projection != nil && (s.Projection.Validate() != nil || s.Projection.ResourceType != request.Resource || request.Kind != "search" && request.Kind != "read" && request.Kind != "vread") {
		return nil, refused
	}
	if s.Prior != nil {
		priorRequest := request
		priorRequest.ConditionalRead = false
		priorOutcome := classify(s.Base, priorRequest, 200, map[string]string{"Content-Type": "application/fhir+json", "ETag": s.Prior.ETag}, s.Prior.Body, nil)
		if priorOutcome.State != "succeeded" {
			return nil, refused
		}
		if request.Kind != "read" || s.Prior.URL != s.HTTP.HTTP.URL || s.Prior.SHA256 != dataset.Digest(s.Prior.Body) || s.Prior.ETag == "" || s.HTTP.Headers.IfNoneMatch != s.Prior.ETag {
			return nil, refused
		}
		d, e := decode(s.Base, s.Prior.Body)
		if e != nil || len(d.Resources()) == 0 || d.Resources()[0].Type != request.Resource || d.Resources()[0].LogicalID != request.ID {
			return nil, refused
		}
	}
	if s.HTTP.Headers.IfNoneMatch != "" && s.Prior == nil || s.HTTP.Headers.IfModifiedSince != "" {
		return nil, refused
	}
	if _, e := capable(s.Base, s.Capability, request); e != nil {
		return nil, refused
	}
	transport, e := networkaction.PrepareRuntimeHTTPV2(encode(s.HTTP), policy)
	if e != nil {
		return nil, refused
	}

	if s.CapabilityHTTP != nil {
		pre := s.CapabilityHTTP
		if pre.HTTP.URL != s.Base+"/metadata" || pre.HTTP.Method != "GET" || pre.HTTP.Operation != sendpolicy.FHIRMetadata || len(pre.HTTP.Body) != 0 || pre.Accept != "application/fhir+json" || pre.Authorization != s.HTTP.Authorization || pre.Page != nil || pre.Headers != (networkaction.HTTPHeadersV2{}) || pre.HTTP.Plan != s.HTTP.HTTP.Plan || pre.HTTP.Project != s.HTTP.HTTP.Project || pre.HTTP.Environment != s.HTTP.HTTP.Environment || pre.HTTP.Revision != s.HTTP.HTTP.Revision {
			return nil, refused
		}
	}
	if s.PagePrivateKey != nil && (request.Kind != "search" || s.HTTP.HTTP.Certificate == nil) {
		return nil, refused
	}
	pre := s.HTTP
	if s.CapabilityHTTP != nil {
		pre = *s.CapabilityHTTP
	} else {
		pre.HTTP.Method = "GET"
		pre.HTTP.URL = s.Base + "/metadata"
		pre.HTTP.Operation = sendpolicy.FHIRMetadata
		pre.HTTP.Body = nil
		pre.HTTP.ContentType = ""
		pre.Headers = networkaction.HTTPHeadersV2{}
	}
	if _, e := networkaction.PrepareRuntimeHTTPV2(encode(pre), policy); e != nil {
		return nil, refused
	}
	if request.Kind == "search" {
		page := s.HTTP
		page.HTTP.Method = "GET"
		page.HTTP.URL = s.Base + "/" + request.Resource
		page.HTTP.Operation = sendpolicy.FHIRSearch
		page.HTTP.Body = nil
		page.HTTP.ContentType = ""
		if s.PagePrivateKey != nil {
			page.HTTP.PrivateKey = s.PagePrivateKey
		}
		if _, e := networkaction.PrepareRuntimeHTTPV2(encode(page), policy); e != nil {
			return nil, refused
		}
	}
	raw = encode(s)
	identity := dataset.Digest(append(append(append([]byte(nil), raw...), byte('\n')), policy...))
	binding := transport.Binding()
	binding.Configuration = identity
	return &Plan{spec: s, raw: raw, policy: append([]byte(nil), policy...), binding: binding, request: request, identity: identity}, nil
}
func (p *Plan) Binding() networkaction.Binding { return p.binding }
func (p *Plan) Identity() string               { return p.identity }
func (p *Plan) Declaration() Spec              { var s Spec; _ = json.Unmarshal(p.raw, &s); return s }
func decode(base string, body []byte) (*fhirr4.Document, error) {
	d, e := fhirr4.Decode(context.Background(), body, fhirr4.Context{Version: fhirr4.Version, Base: base, MediaType: "application/fhir+json"})
	if e != nil {
		return nil, refused
	}
	for _, f := range d.Findings() {
		if f.State == "invalid" || f.State == "unsupported" {
			return nil, refused
		}
	}
	return d, nil
}
func capable(base string, raw []byte, r fhirrequest.Request) (fhirr4.Capabilities, error) {
	d, e := decode(base, raw)
	if e != nil || len(d.Resources()) == 0 || d.Resources()[0].Type != "CapabilityStatement" {
		return fhirr4.Capabilities{}, refused
	}
	c, e := d.Capabilities(d.Resources()[0].Occurrence)
	if e != nil || c.FHIRVersion != fhirr4.Version || !slices.Contains(c.Formats, "application/fhir+json") && !slices.Contains(c.Formats, "json") {
		return c, refused
	}
	var check func(fhirrequest.Request) error
	check = func(r fhirrequest.Request) error {
		if r.Kind == "capabilities" {
			return nil
		}
		if r.Kind == "batch" || r.Kind == "transaction" {
			found := false
			for _, rest := range c.REST {
				if rest.Mode == "server" && slices.Contains(rest.Interactions, r.Kind) {
					found = true
				}
			}
			if !found {
				return refused
			}
			for _, entry := range r.Entries {
				if e := check(entry); e != nil {
					return e
				}
			}
			return nil
		}
		var claim *fhirr4.ResourceClaims
		for _, rest := range c.REST {
			if rest.Mode == "server" {
				for _, resource := range rest.Resources {
					if resource.Type == r.Resource {
						copy := resource
						claim = &copy
					}
				}
			}
		}
		if claim == nil {
			return refused
		}
		if r.ConditionalRead && claim.ConditionalRead != "full-support" && claim.ConditionalRead != "not-match" {
			return refused
		}
		interaction := r.Kind
		switch interaction {
		case "search":
			interaction = "search-type"
		case "conditional-create":
			interaction = "create"
			if claim.ConditionalCreate == nil || !*claim.ConditionalCreate {
				return refused
			}
		case "conditional-update":
			interaction = "update"
			if claim.ConditionalUpdate == nil || !*claim.ConditionalUpdate {
				return refused
			}
		}
		if !slices.Contains(claim.Interactions, interaction) {
			return refused
		}
		if r.Kind == "patch" && !slices.Contains(c.PatchFormats, "application/json-patch+json") {
			return refused
		}
		if (r.Kind == "update" || r.Kind == "patch") && claim.Versioning != "versioned-update" {
			return refused
		}
		for name, values := range r.Parameters {
			if name == "_count" || name == "_summary" || name == "_elements" || name == "_total" || name == "_id" {
				continue
			}
			if name == "_include" || name == "_revinclude" {
				ok := false
				for _, support := range d.HTTPResourceSupport() {
					if support.Type != r.Resource {
						continue
					}
					allowed := support.Includes
					if name == "_revinclude" {
						allowed = support.RevIncludes
					}
					ok = true
					for _, value := range values {
						if !slices.Contains(allowed, value) {
							ok = false
						}
					}
				}
				if !ok {
					return refused
				}
				continue
			}
			found := false
			for _, parameter := range claim.Search {
				if name == parameter.Name {
					found = true
				}
			}
			if !found || strings.ContainsAny(name, ":.") {
				return refused
			}
		}
		return nil
	}
	return c, check(r)
}

func requiredCapabilityIdentity(c fhirr4.Capabilities, r fhirrequest.Request) string {
	values := []any{c.FHIRVersion, r.Kind}
	if r.Kind == "batch" || r.Kind == "transaction" {
		for _, entry := range r.Entries {
			values = append(values, requiredCapabilityIdentity(c, entry))
		}
	}
	for _, rest := range c.REST {
		if rest.Mode != "server" {
			continue
		}
		for _, resource := range rest.Resources {
			if resource.Type != r.Resource {
				continue
			}
			values = append(values, resource.Type, resource.Versioning)
			for _, parameter := range resource.Search {
				if _, used := r.Parameters[parameter.Name]; used {
					values = append(values, parameter)
				}
			}
			if r.ConditionalRead {
				values = append(values, resource.ConditionalRead)
			}
		}
	}
	return dataset.Digest(encode(values))
}

// Interaction is an exact request shape whose capability needs are checked
// offline. Checking it never grants access or contacts the server.
type Interaction struct {
	Method, URL, ContentType string
	Body                     []byte
	Headers                  networkaction.HTTPHeadersV2
}

// Kind is the interaction class the shared request interpreter assigns, or
// empty when the request is refused.
func (i Interaction) Kind(base string) string {
	r, e := i.parse(base)
	if e != nil {
		return ""
	}
	return r.Kind
}
func (i Interaction) parse(base string) (fhirrequest.Request, error) {
	h := i.Headers
	return fhirrequest.Parse(base, i.Method, i.URL, i.ContentType, i.Body, fhirrequest.Headers{IfMatch: h.IfMatch, IfNoneMatch: h.IfNoneMatch, IfModifiedSince: h.IfModifiedSince, IfNoneExist: h.IfNoneExist, Prefer: h.Prefer})
}

// Admits reports whether a CapabilityStatement advertises everything the
// interaction needs, under the same rules execution's preflight applies, and
// whether the request keeps Prepare's version guards: updates and patches
// carry an authored If-Match and no unpinned conditional read is made.
func Admits(base string, capability []byte, i Interaction) error {
	r, e := i.parse(base)
	if e != nil || (r.Kind == "update" || r.Kind == "patch") && i.Headers.IfMatch == "" || i.Headers.IfNoneMatch != "" || i.Headers.IfModifiedSince != "" {
		return refused
	}
	for _, entry := range r.Entries {
		if (entry.Kind == "update" || entry.Kind == "patch") && entry.VersionMatch == "" || entry.ConditionalRead {
			return refused
		}
	}
	_, e = capable(base, capability, r)
	return e
}

// Unchanged reports whether current metadata still admits the interaction with
// the same claims the reviewed baseline declared. Changed claims refuse effects.
func Unchanged(base string, baseline, current []byte, i Interaction) error {
	r, e := i.parse(base)
	if e != nil {
		return refused
	}
	was, e := capable(base, baseline, r)
	if e != nil {
		return refused
	}
	now, e := capable(base, current, r)
	if e != nil || requiredCapabilityIdentity(now, r) != requiredCapabilityIdentity(was, r) {
		return refused
	}
	return nil
}
