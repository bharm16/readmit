package networkaction

import (
	"encoding/json/v2"
	"net/http"
	"strings"
)

const RuntimeHTTPSchemaV2 = "readmit-runtime-http-action/v2"

// HTTPHeadersV2 is a closed set of noncredential FHIR interaction headers. They
// are private evidence and part of the action identity, never operational logs.
type HTTPHeadersV2 struct {
	IfMatch         string `json:"if_match,omitzero"`
	IfNoneMatch     string `json:"if_none_match,omitzero"`
	IfModifiedSince string `json:"if_modified_since,omitzero"`
	IfNoneExist     string `json:"if_none_exist,omitzero"`
	Prefer          string `json:"prefer,omitzero"`
}
type SearchPageScope struct {
	Resource           string `json:"resource"`
	FromResponseSHA256 string `json:"from_response_sha256"`
}
type RuntimeHTTPSpecV2 struct {
	Page          *SearchPageScope `json:"page,omitzero"`
	Schema        string           `json:"schema"`
	Authorization string           `json:"authorization"`
	HTTP          HTTPSpec         `json:"http"`
	Accept        string           `json:"accept"`
	Headers       HTTPHeadersV2    `json:"headers"`
}

func PrepareRuntimeHTTPV2(raw, policy []byte) (*RuntimeHTTPPlan, error) {
	var s RuntimeHTTPSpecV2
	if len(raw) > 24<<20 || json.Unmarshal(raw, &s, json.RejectUnknownMembers(true)) != nil || s.Schema != RuntimeHTTPSchemaV2 {
		return nil, refused
	}
	if s.Page != nil && (!ValidDigest(s.Page.FromResponseSHA256) || s.HTTP.Method != "GET" || len(s.HTTP.Body) != 0 || s.HTTP.Operation != "fhir-search" || s.Headers.IfMatch != "" || s.Headers.IfNoneMatch != "" || s.Headers.IfModifiedSince != "" || s.Headers.IfNoneExist != "") {
		return nil, refused
	}
	for _, v := range []string{s.Headers.IfMatch, s.Headers.IfNoneMatch, s.Headers.IfModifiedSince, s.Headers.IfNoneExist, s.Headers.Prefer} {
		if len(v) > 4096 || strings.ContainsAny(v, "\r\n") {
			return nil, refused
		}
	}
	old, _ := json.Marshal(RuntimeHTTPSpec{Schema: RuntimeHTTPSchema, Authorization: s.Authorization, HTTP: s.HTTP, Accept: s.Accept})
	p, e := PrepareRuntimeHTTP(old, policy)
	if e != nil {
		return nil, e
	}
	canonical, _ := json.Marshal(s, json.Deterministic(true))
	p.binding.Configuration = Digest(canonical)
	p.v2 = &s
	return p, nil
}
func (p *RuntimeHTTPPlan) DeclarationV2() (RuntimeHTTPSpecV2, bool) {
	if p == nil || p.v2 == nil {
		return RuntimeHTTPSpecV2{}, false
	}
	var s RuntimeHTTPSpecV2
	b, _ := json.Marshal(p.v2)
	_ = json.Unmarshal(b, &s)
	return s, true
}
func (h HTTPHeadersV2) apply(target http.Header) {
	for k, v := range map[string]string{"If-Match": h.IfMatch, "If-None-Match": h.IfNoneMatch, "If-Modified-Since": h.IfModifiedSince, "If-None-Exist": h.IfNoneExist, "Prefer": h.Prefer} {
		if v != "" {
			target.Set(k, v)
		}
	}
}
func safeHeadersV2(header http.Header) http.Header {
	out := safeHeaders(header)
	for _, k := range []string{"ETag", "Last-Modified", "Retry-After", "Preference-Applied"} {
		if v := header.Get(k); v != "" && len(v) <= 4096 && !strings.ContainsAny(v, "\r\n") {
			out.Set(k, v)
		}
	}
	return out
}

// Metadata returns only the version's allowlisted response metadata. It never
// returns authentication, cookies or arbitrary server headers.
func (r HTTPResponse) Metadata() map[string]string {
	out := map[string]string{}
	for _, name := range []string{"Content-Type", "Date", "Age", "Link", "Location", "ETag", "Last-Modified", "Retry-After", "Preference-Applied"} {
		if value := r.header.Get(name); value != "" {
			out[name] = value
		}
	}
	return out
}
