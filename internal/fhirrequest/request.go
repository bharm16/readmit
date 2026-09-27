// Package fhirrequest derives a finite R4 interaction and its permissions from
// exact HTTP bytes. It performs no I/O and grants no execution authority.
package fhirrequest

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/fhirr4"
)

const Schema = "readmit-fhir-request/v1"

var Refused = errors.New("unsupported or invalid FHIR R4 request")
var id = regexp.MustCompile(`^[A-Za-z0-9.-]{1,64}$`)
var etag = regexp.MustCompile(`^W/"[A-Za-z0-9.-]{1,64}"$`)

type Headers struct {
	IfMatch         string `json:"if_match,omitzero"`
	IfNoneMatch     string `json:"if_none_match,omitzero"`
	IfModifiedSince string `json:"if_modified_since,omitzero"`
	IfNoneExist     string `json:"if_none_exist,omitzero"`
	Prefer          string `json:"prefer,omitzero"`
}
type Permission struct {
	Resource    string `json:"resource"`
	Interaction string `json:"interaction"`
}
type Request struct {
	VersionMatch    string              `json:"version_match,omitzero"`
	ConditionalRead bool                `json:"conditional_read"`
	Schema          string              `json:"schema"`
	Kind            string              `json:"kind"`
	Resource        string              `json:"resource,omitzero"`
	ID              string              `json:"id,omitzero"`
	Version         string              `json:"version,omitzero"`
	Parameters      map[string][]string `json:"parameters"`
	Permissions     []Permission        `json:"permissions"`
	Entries         []Request           `json:"entries"`
	SafeRead        bool                `json:"safe_read"`
}

func validID(v string) bool { return id.MatchString(v) && v != "." && v != ".." }
func ValidBase(base string) bool {
	u, e := url.Parse(base)
	return e == nil && len(base) <= 4096 && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.RawPath == "" && !strings.HasSuffix(base, "/") && (u.Path == "" || path.Clean(u.Path) == u.Path) && !strings.ContainsAny(base, "\\\r\n") && u.String() == base
}
func Parse(base, method, address, contentType string, body []byte, h Headers) (Request, error) {
	return parse(base, method, address, contentType, body, h, false)
}
func parse(base, method, address, contentType string, body []byte, h Headers, nested bool) (Request, error) {
	r := Request{VersionMatch: h.IfMatch, ConditionalRead: h.IfNoneMatch != "" || h.IfModifiedSince != "", Schema: Schema, Parameters: map[string][]string{}, Permissions: []Permission{}, Entries: []Request{}}
	b, be := url.Parse(base)
	u, e := url.Parse(address)
	if !ValidBase(base) || be != nil || e != nil || len(address) > 16384 || len(body) > fhirr4.MaxBytes || u.Scheme != b.Scheme || u.Host != b.Host || u.User != nil || u.Fragment != "" || u.RawPath != "" || u.String() != address || strings.ContainsAny(address, "\\\r\n") || (u.Path != "" && path.Clean(u.Path) != u.Path) || !(u.Path == b.Path || strings.HasPrefix(u.Path, b.Path+"/")) {
		return r, Refused
	}
	for _, v := range []string{h.IfMatch, h.IfNoneMatch, h.IfModifiedSince, h.IfNoneExist, h.Prefer} {
		if len(v) > 4096 || strings.ContainsAny(v, "\r\n") {
			return r, Refused
		}
	}
	if h.IfMatch != "" && !etag.MatchString(h.IfMatch) || h.IfNoneMatch != "" && !etag.MatchString(h.IfNoneMatch) || h.IfModifiedSince != "" && h.IfNoneMatch != "" {
		return r, Refused
	}
	if h.IfModifiedSince != "" {
		if _, e := http.ParseTime(h.IfModifiedSince); e != nil {
			return r, Refused
		}
	}
	switch h.Prefer {
	case "", "return=minimal", "return=representation", "return=OperationOutcome", "handling=strict":
	default:
		return r, Refused
	}
	query, e := url.ParseQuery(u.RawQuery)
	if e != nil || len(query) > 32 {
		return r, Refused
	}
	r.Parameters = query
	for name, values := range query {
		if !utf8.ValidString(name) || name == "" || len(name) > 128 || len(values) > 16 {
			return r, Refused
		}
		for _, v := range values {
			if !utf8.ValidString(v) || v == "" || len(v) > 2048 {
				return r, Refused
			}
		}
	}
	rel := strings.TrimPrefix(u.Path, b.Path)
	parts := strings.Split(strings.TrimPrefix(rel, "/"), "/")
	if rel == "/metadata" && method == "GET" && len(query) == 0 {
		r.Kind = "capabilities"
		r.SafeRead = true
	} else if rel == "" && method == "POST" && !nested && len(query) == 0 {
		if contentType != "application/fhir+json" {
			return r, Refused
		}
		d, e := resourceDocument(base, body)
		if e != nil {
			return r, e
		}
		bundle, e := d.HTTPBundle()
		if e != nil || (bundle.Type != "transaction" && bundle.Type != "batch") || len(bundle.Entries) > 64 {
			return r, Refused
		}
		r.Kind = bundle.Type
		created := map[string]bool{}
		fullURLs := map[string]bool{}
		writes := map[string]bool{}
		for _, entry := range bundle.Entries {
			if entry.FullURL != "" {
				if fullURLs[entry.FullURL] {
					return r, Refused
				}
				fullURLs[entry.FullURL] = true
				if entry.Request.Method == "POST" {
					created[entry.FullURL] = true
				}
			}
			if entry.Request.Method != "GET" && entry.Request.Method != "POST" {
				if writes[entry.Request.URL] {
					return r, Refused
				}
				writes[entry.Request.URL] = true
			}
		}
		if bundle.Type == "batch" {
			for _, entry := range bundle.Entries {
				if len(entry.Resource) == 0 {
					continue
				}
				resource, e := resourceDocument(base, entry.Resource)
				if e != nil {
					return r, e
				}
				for _, reference := range resource.ReferenceLiterals() {
					if created[reference] {
						return r, Refused
					}
				}
			}
		}
		for _, entry := range bundle.Entries {
			uri, e := url.Parse(entry.Request.URL)
			if e != nil || uri.IsAbs() || uri.Host != "" || strings.HasPrefix(entry.Request.URL, "/") || uri.Fragment != "" || uri.User != nil {
				return r, Refused
			}
			headers := Headers{IfMatch: entry.Request.IfMatch, IfNoneMatch: entry.Request.IfNoneMatch, IfModifiedSince: entry.Request.IfModifiedSince, IfNoneExist: entry.Request.IfNoneExist}
			ct := ""
			if len(entry.Resource) > 0 {
				ct = "application/fhir+json"
			}
			child, e := parse(base, entry.Request.Method, base+"/"+entry.Request.URL, ct, entry.Resource, headers, true)
			if e != nil {
				return r, e
			}
			r.Entries = append(r.Entries, child)
			r.Permissions = append(r.Permissions, child.Permissions...)
		}
	} else {
		if len(parts) < 1 || !fhirr4.SupportedHTTPResource(parts[0]) {
			return r, Refused
		}
		r.Resource = parts[0]
		switch method {
		case "GET":
			if len(parts) == 1 {
				r.Kind = "search"
				r.SafeRead = true
			} else if len(parts) == 2 && validID(parts[1]) {
				r.Kind = "read"
				r.ID = parts[1]
				r.SafeRead = true
			} else if len(parts) == 4 && validID(parts[1]) && parts[2] == "_history" && validID(parts[3]) {
				r.Kind = "vread"
				r.ID = parts[1]
				r.Version = parts[3]
				r.SafeRead = true
			}
		case "POST":
			if len(parts) == 2 && parts[1] == "_search" {
				if nested || contentType != "application/x-www-form-urlencoded" {
					return r, Refused
				}
				values, e := url.ParseQuery(string(body))
				if e != nil {
					return r, Refused
				}
				for k, v := range values {
					r.Parameters[k] = append(r.Parameters[k], v...)
				}
				r.Kind = "search"
				r.SafeRead = true
			} else if len(parts) == 1 && len(query) == 0 {
				r.Kind = "create"
				if h.IfNoneExist != "" {
					r.Kind = "conditional-create"
					values, e := url.ParseQuery(h.IfNoneExist)
					if e != nil || len(values) == 0 {
						return r, Refused
					}
					r.Parameters = values
				}
			}
		case "PUT":
			if len(parts) == 2 && validID(parts[1]) && len(query) == 0 {
				r.Kind = "update"
				r.ID = parts[1]
			} else if len(parts) == 1 && len(query) > 0 {
				r.Kind = "conditional-update"
			}
		case "PATCH":
			if len(parts) == 2 && validID(parts[1]) && len(query) == 0 {
				r.Kind = "patch"
				r.ID = parts[1]
			}
		case "DELETE":
			if len(parts) == 2 && validID(parts[1]) && len(query) == 0 {
				r.Kind = "delete"
				r.ID = parts[1]
			}
		}
		if r.Kind == "" {
			return r, Refused
		}
		permission := ""
		switch r.Kind {
		case "search":
			permission = "s"
		case "read", "vread":
			permission = "r"
		case "create", "conditional-create":
			permission = "c"
		case "update", "conditional-update", "patch":
			permission = "u"
		case "delete":
			permission = "d"
		}
		r.Permissions = append(r.Permissions, Permission{r.Resource, permission})
		if method == "POST" && r.Kind != "search" || method == "PUT" {
			if contentType != "application/fhir+json" {
				return r, Refused
			}
			d, e := resourceDocument(base, body)
			if e != nil {
				return r, e
			}
			resources := d.Resources()
			if len(resources) == 0 || resources[0].Type != r.Resource || (r.ID != "" && resources[0].LogicalID != r.ID) {
				return r, Refused
			}
		}
		if r.Kind == "patch" {
			if contentType != "application/json-patch+json" || validatePatch(body) != nil {
				return r, Refused
			}
		}
	}
	if (method == "GET" || method == "DELETE") && len(body) != 0 {
		return r, Refused
	}
	if !r.SafeRead && (h.IfNoneMatch != "" || h.IfModifiedSince != "") {
		return r, Refused
	}
	if r.Kind != "read" && (h.IfNoneMatch != "" || h.IfModifiedSince != "") {
		return r, Refused
	}
	if r.Kind != "update" && r.Kind != "patch" && r.Kind != "delete" && h.IfMatch != "" {
		return r, Refused
	}
	if r.Kind != "conditional-create" && h.IfNoneExist != "" {
		return r, Refused
	}
	if r.Kind == "read" || r.Kind == "vread" {
		for k := range query {
			if k != "_summary" && k != "_elements" {
				return r, Refused
			}
		}
	}
	if r.Kind == "search" || r.Kind == "conditional-create" || r.Kind == "conditional-update" {
		for k, values := range r.Parameters {
			if r.Kind != "search" && (k == "_count" || k == "_summary" || k == "_elements" || k == "_include" || k == "_revinclude" || k == "_total" || k == "_sort") {
				return r, Refused
			}
			if !utf8.ValidString(k) || k == "" || len(k) > 128 || len(values) > 16 {
				return r, Refused
			}
			for _, v := range values {
				if !utf8.ValidString(v) || v == "" || len(v) > 2048 {
					return r, Refused
				}
			}
		}
		if len(r.Parameters) > 32 {
			return r, Refused
		}
	}
	return r, nil
}
func resourceDocument(base string, body []byte) (*fhirr4.Document, error) {
	d, e := fhirr4.Decode(context.Background(), body, fhirr4.Context{Version: fhirr4.Version, Base: base, MediaType: "application/fhir+json"})
	if e != nil {
		return nil, Refused
	}
	for _, f := range d.Findings() {
		if f.State == "invalid" || f.State == "unsupported" {
			return nil, Refused
		}
	}
	return d, nil
}
func validatePatch(body []byte) error {
	var ops []struct {
		Op    string         `json:"op"`
		Path  *string        `json:"path"`
		From  *string        `json:"from,omitzero"`
		Value jsontext.Value `json:"value,omitzero"`
	}
	if len(body) > 1<<20 || json.Unmarshal(body, &ops, json.RejectUnknownMembers(true)) != nil || len(ops) == 0 || len(ops) > 64 {
		return Refused
	}
	for _, op := range ops {
		if op.Path == nil {
			return Refused
		}
		to, valid := pointerParts(*op.Path)
		if !valid {
			return Refused
		}
		switch op.Op {
		case "add", "replace", "test":
			if len(op.Value) == 0 || op.From != nil {
				return Refused
			}
		case "remove":
			if len(op.Value) != 0 || op.From != nil {
				return Refused
			}
		case "move", "copy":
			if op.From == nil || len(op.Value) != 0 {
				return Refused
			}
			from, valid := pointerParts(*op.From)
			if !valid {
				return Refused
			}
			if op.Op == "move" && len(to) > len(from) {
				ancestor := true
				for i, part := range from {
					if to[i] != part {
						ancestor = false
						break
					}
				}
				if ancestor {
					return Refused
				}
			}
		default:
			return Refused
		}
	}
	return nil
}
func MediaType(raw string) string {
	typ, params, e := mime.ParseMediaType(raw)
	if e != nil {
		return ""
	}
	for k, v := range params {
		if k == "charset" && strings.EqualFold(v, "utf-8") {
			continue
		}
		if k == "fhirversion" && v == "4.0" {
			continue
		}
		return ""
	}
	return typ
}

func pointerParts(pointer string) ([]string, bool) {
	if len(pointer) > 4096 {
		return nil, false
	}
	if pointer == "" {
		return []string{}, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	parts := strings.Split(pointer[1:], "/")
	for i, part := range parts {
		for j := 0; j < len(part); j++ {
			if part[j] == '~' {
				if j+1 >= len(part) || (part[j+1] != '0' && part[j+1] != '1') {
					return nil, false
				}
				j++
			}
		}
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return parts, true
}

// ValidID recognizes the finite FHIR id lexical form, excluding URL dot segments.
func ValidID(v string) bool { return validID(v) }

// Page derives only read-only search permission for a retained next-link step.
// The execution reader separately proves the link against its preceding page.
func Page(base, resource, address string) (Request, error) {
	r := Request{Schema: Schema, Kind: "search", Resource: resource, SafeRead: true, Parameters: map[string][]string{}, Permissions: []Permission{{Resource: resource, Interaction: "s"}}, Entries: []Request{}}
	if !ValidBase(base) || !fhirr4.SupportedHTTPResource(resource) {
		return r, Refused
	}
	b, _ := url.Parse(base)
	u, e := url.Parse(address)
	if e != nil || u.User != nil || u.Scheme != b.Scheme || u.Host != b.Host || u.Fragment != "" || u.RawPath != "" || u.String() != address || len(address) > 16384 || !(u.Path == b.Path || u.Path == b.Path+"/"+resource) || strings.ContainsAny(address, "\\\r\n") {
		return r, Refused
	}
	values, e := url.ParseQuery(u.RawQuery)
	if e != nil || len(values) > 32 {
		return r, Refused
	}
	if types := values["_type"]; len(types) > 0 && (len(types) != 1 || types[0] != resource) {
		return r, Refused
	}
	for key, items := range values {
		if key == "_query" || key == "_filter" || len(key) > 128 || len(items) > 16 {
			return r, Refused
		}
		for _, v := range items {
			if len(v) > 2048 {
				return r, Refused
			}
		}
	}
	return r, nil
}
