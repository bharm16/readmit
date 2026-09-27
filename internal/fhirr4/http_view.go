package fhirr4

import "strconv"

// HTTPBundle is a pure view over the same bounded JSON tree used by projections.
// Entry bodies remain byte slices of the original resource, never reserialized.
type HTTPBundle struct {
	Type    string
	Total   *int64
	Links   []HTTPLink
	Entries []HTTPEntry
}
type HTTPLink struct{ Relation, URL string }
type HTTPEntry struct {
	FullURL, SearchMode string
	Resource            []byte
	Request             HTTPEntryRequest
	Response            HTTPEntryResponse
}
type HTTPEntryRequest struct{ Method, URL, IfMatch, IfNoneMatch, IfModifiedSince, IfNoneExist string }
type HTTPEntryResponse struct {
	Status, Location, ETag, LastModified string
	Outcome                              []byte
}

func (d *Document) HTTPBundle() (HTTPBundle, error) {
	if d.root.field("resourceType").string() != "Bundle" {
		return HTTPBundle{}, invalid
	}
	b := HTTPBundle{Type: d.root.field("type").string(), Links: []HTTPLink{}, Entries: []HTTPEntry{}}
	if n := d.root.field("total"); n != nil {
		v, e := strconv.ParseInt(n.text, 10, 64)
		if e != nil || v < 0 {
			return b, invalid
		}
		b.Total = &v
	}
	for _, n := range d.root.field("link").array() {
		b.Links = append(b.Links, HTTPLink{n.field("relation").string(), n.field("url").string()})
	}
	for _, n := range d.root.field("entry").array() {
		r := n.field("request")
		p := n.field("response")
		e := HTTPEntry{FullURL: n.field("fullUrl").string(), SearchMode: n.field("search").field("mode").string(), Request: HTTPEntryRequest{r.field("method").string(), r.field("url").string(), r.field("ifMatch").string(), r.field("ifNoneMatch").string(), r.field("ifModifiedSince").string(), r.field("ifNoneExist").string()}, Response: HTTPEntryResponse{Status: p.field("status").string(), Location: p.field("location").string(), ETag: p.field("etag").string(), LastModified: p.field("lastModified").string()}}
		if resource := n.field("resource"); resource != nil {
			e.Resource = append([]byte(nil), d.raw[resource.start:resource.end]...)
		}
		if outcome := p.field("outcome"); outcome != nil {
			e.Response.Outcome = append([]byte(nil), d.raw[outcome.start:outcome.end]...)
		}
		b.Entries = append(b.Entries, e)
	}
	return b, nil
}
func (d *Document) OutcomeSeverities() ([]string, error) {
	if d.root.field("resourceType").string() != "OperationOutcome" {
		return nil, invalid
	}
	values := []string{}
	for _, n := range d.root.field("issue").array() {
		values = append(values, n.field("severity").string())
	}
	return values, nil
}
func (d *Document) Subsetted() bool {
	for _, r := range d.resources {
		for _, tag := range r.node.field("meta").field("tag").array() {
			if tag.field("code").string() == "SUBSETTED" {
				return true
			}
		}
	}
	return false
}

// HTTPResourceSupport adds transport-oriented flags without widening the frozen
// capability-claims/v1 representation.
type HTTPResourceSupport struct {
	Type                      string
	ReadHistory, UpdateCreate bool
	Includes, RevIncludes     []string
}

func (d *Document) HTTPResourceSupport() []HTTPResourceSupport {
	out := []HTTPResourceSupport{}
	for _, rest := range d.root.field("rest").array() {
		if rest.field("mode").string() != "server" {
			continue
		}
		for _, r := range rest.field("resource").array() {
			out = append(out, HTTPResourceSupport{Type: r.field("type").string(), ReadHistory: r.field("readHistory") != nil && r.field("readHistory").kind == 't', UpdateCreate: r.field("updateCreate") != nil && r.field("updateCreate").kind == 't', Includes: stringsOf(r.field("searchInclude")), RevIncludes: stringsOf(r.field("searchRevInclude"))})
		}
	}
	return out
}

// ReferenceLiterals reads literal Reference.reference members from the existing
// bounded tree; it performs no resolution or external lookup.
func (d *Document) ReferenceLiterals() []string {
	out := []string{}
	var walk func(*node)
	walk = func(n *node) {
		if n == nil {
			return
		}
		for _, m := range n.members {
			if m.key == "reference" && m.value.kind == '"' {
				out = append(out, m.value.text)
			}
			walk(m.value)
		}
		for _, item := range n.items {
			walk(item)
		}
	}
	walk(d.root)
	return out
}

// SupportedHTTPResource reports the finite resource model available to typed
// request/response execution; it is narrower than the R4 ResourceType vocabulary.
func SupportedHTTPResource(name string) bool { return supported(name) }
