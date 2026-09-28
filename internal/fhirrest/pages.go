package fhirrest

import (
	"errors"
	"fmt"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// errDuplicateNextLink reports a search page carrying more than one next
// link. The executor reads it as a protocol fault; the offline reader reads
// it as a refusal.
var errDuplicateNextLink = errors.New("search page carries more than one next link")

// pageLinks reads one search response body as a page: its entry count and
// its single next link, "" when the search ends here. The executor and the
// offline reader derive both through it, so the reader provably walks the
// same paging loop it verifies; each applies only its own verdict policy
// to the answer.
func pageLinks(base string, body []byte) (entries int, next string, err error) {
	d, err := decode(base, body)
	if err != nil {
		return 0, "", err
	}
	bundle, err := d.HTTPBundle()
	if err != nil {
		return 0, "", err
	}
	for _, link := range bundle.Links {
		if link.Relation == "next" {
			if next != "" {
				return 0, "", errDuplicateNextLink
			}
			next = link.URL
		}
	}
	return len(bundle.Entries), next, nil
}

// pageRequest resolves one page's next link against the declared base and
// builds the GET request that follows it from the search template: no body,
// the search operation, the paging credential and a scope binding the
// request to this response's bytes. The executor sends what it derives and
// the offline reader demands what it derives, so a page the executor could
// not send is a page the reader refuses.
func pageRequest(base string, body []byte, resource string, template networkaction.RuntimeHTTPSpecV2, key *networkaction.Credential, next string) (networkaction.RuntimeHTTPSpecV2, error) {
	target, err := destination.ScopedLink(base+"/", next)
	if err != nil {
		return networkaction.RuntimeHTTPSpecV2{}, err
	}
	followed := template
	followed.HTTP.Method = "GET"
	followed.HTTP.URL = target
	followed.HTTP.Body = nil
	followed.HTTP.ContentType = ""
	followed.HTTP.Operation = sendpolicy.FHIRSearch
	if key != nil {
		followed.HTTP.PrivateKey = key
	}
	followed.Page = &networkaction.SearchPageScope{Resource: resource, FromResponseSHA256: dataset.Digest(body)}
	if _, err := parsedRequest(base, followed); err != nil {
		return networkaction.RuntimeHTTPSpecV2{}, err
	}
	return followed, nil
}

// EntryMatch correlates one search bundle entry with the decoded resource
// it holds. Identity joins the resource authority the same way for the
// executor's search accounting and the observer's samples, so equal logical
// resources are one entity in both.
type EntryMatch struct {
	Index      int
	Mode       string
	Found      bool
	Occurrence string
	Identity   string
	Type       string
	LogicalID  string
	VersionID  string
}

// entryPointer is the JSON pointer of the resource the i-th bundle entry holds.
func entryPointer(i int) string {
	return fmt.Sprintf("/entry/%d/resource", i)
}

// resourceIdentity joins one decoded resource's authority: base, type and
// logical ID. Search accounting and observations join pages on it.
func resourceIdentity(resource fhirr4.Resource) string {
	return resource.Base + "/" + resource.Type + "/" + resource.LogicalID
}

// CorrelateEntries walks one bundle's entries in order and correlates each
// with the decoded resource at its pointer. Found is false when no decoded
// resource sits there. Whether an include, a mistyped match, an empty
// identity or a missing resource is skipped or recorded unknown is the
// caller's verdict policy.
func CorrelateEntries(bundle fhirr4.HTTPBundle, resources []fhirr4.Resource) []EntryMatch {
	out := make([]EntryMatch, 0, len(bundle.Entries))
	for i, entry := range bundle.Entries {
		match := EntryMatch{Index: i, Mode: entry.SearchMode}
		pointer := entryPointer(i)
		for _, resource := range resources {
			if resource.Pointer != pointer {
				continue
			}
			match.Found = true
			match.Occurrence = resource.Occurrence
			match.Identity = resourceIdentity(resource)
			match.Type = resource.Type
			match.LogicalID = resource.LogicalID
			match.VersionID = resource.VersionID
			break
		}
		out = append(out, match)
	}
	return out
}
