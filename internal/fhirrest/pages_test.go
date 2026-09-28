package fhirrest

import (
	"errors"
	"testing"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const searchPageBody = `{"resourceType":"Bundle","type":"searchset","link":[{"relation":"self","url":"https://x.test/r4/Appointment?status=booked"},{"relation":"next","url":"https://x.test/r4/Appointment?status=booked&_page=2"}],"entry":[{"fullUrl":"https://x.test/r4/Appointment/a1","search":{"mode":"match"},"resource":{"resourceType":"Appointment","id":"a1","meta":{"versionId":"3"},"status":"booked","participant":[{"actor":{"reference":"Patient/p1"},"status":"accepted"}]}},{"fullUrl":"https://x.test/r4/Patient/p1","search":{"mode":"include"},"resource":{"resourceType":"Patient","id":"p1"}},{"search":{"mode":"outcome"},"resource":{"resourceType":"OperationOutcome","issue":[{"severity":"information","code":"informational","diagnostics":"page 1 of 2"}]}}]}`

// The executor and the offline reader derive paging through one function:
// the entry count, the single next link, and the refusal of anything else.
func TestPageLinksReadsOnePage(t *testing.T) {
	entries, next, err := pageLinks("https://x.test/r4", []byte(searchPageBody))
	if err != nil {
		t.Fatal(err)
	}
	if entries != 3 || next != "https://x.test/r4/Appointment?status=booked&_page=2" {
		t.Fatalf("page reads as %d entries, next %q", entries, next)
	}
	if _, _, err := pageLinks("https://x.test/r4", []byte(`{"resourceType":"Bundle","type":"searchset","link":[{"relation":"next","url":"https://x.test/r4/a"},{"relation":"next","url":"https://x.test/r4/b"}]}`)); !errors.Is(err, errDuplicateNextLink) {
		t.Fatalf("duplicate next links read as %v", err)
	}
	entries, next, err = pageLinks("https://x.test/r4", []byte(`{"resourceType":"Bundle","type":"searchset"}`))
	if err != nil || entries != 0 || next != "" {
		t.Fatalf("final page reads as %d entries, next %q, %v", entries, next, err)
	}
	if _, _, err := pageLinks("https://x.test/r4", []byte(`{"resourceType":"Bundle"`)); err == nil || errors.Is(err, errDuplicateNextLink) {
		t.Fatalf("malformed page reads as %v", err)
	}
}

// The follow-up request is derived once: the next link resolved against the
// declared base, sent as a bodyless search GET bound to this response.
func TestPageRequestFollowsOneNextLink(t *testing.T) {
	body := []byte(searchPageBody)
	followed, err := pageRequest("https://x.test/r4", body, "Appointment", networkaction.RuntimeHTTPSpecV2{}, nil, "https://x.test/r4/Appointment?status=booked&_page=2")
	if err != nil {
		t.Fatal(err)
	}
	if followed.HTTP.Method != "GET" || followed.HTTP.URL != "https://x.test/r4/Appointment?status=booked&_page=2" || followed.HTTP.Body != nil || followed.HTTP.ContentType != "" || followed.HTTP.Operation != sendpolicy.FHIRSearch {
		t.Fatalf("follow-up request is not a bodyless search GET: %+v", followed.HTTP)
	}
	if followed.Page == nil || followed.Page.Resource != "Appointment" || followed.Page.FromResponseSHA256 != dataset.Digest(body) {
		t.Fatalf("follow-up request is not bound to this response: %+v", followed.Page)
	}
	if _, err := pageRequest("https://x.test/r4", body, "Appointment", networkaction.RuntimeHTTPSpecV2{}, nil, "https://evil.test/r4/Appointment?_page=2"); err == nil {
		t.Fatal("off-base next link followed")
	}
}

// Search accounting and sampling correlate entries through one walk: every
// entry in order, the decoded resource at its pointer, one identity join.
func TestCorrelateEntriesAnswersEveryEntryInOrder(t *testing.T) {
	d, err := decode("https://x.test/r4", []byte(searchPageBody))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := d.HTTPBundle()
	if err != nil {
		t.Fatal(err)
	}
	got := CorrelateEntries(bundle, d.Resources())
	if len(got) != 3 {
		t.Fatalf("correlated %d entries", len(got))
	}
	if got[0].Index != 0 || got[0].Mode != "match" || !got[0].Found || got[0].Occurrence == "" {
		t.Fatalf("match entry uncorrelated: %+v", got[0])
	}
	if want := "https://x.test/r4/Appointment/a1"; got[0].Identity != want {
		t.Fatalf("match identity is %q, want %q", got[0].Identity, want)
	}
	if got[0].Type != "Appointment" || got[0].LogicalID != "a1" || got[0].VersionID != "3" {
		t.Fatalf("match resource lost: %+v", got[0])
	}
	if got[1].Mode != "include" || !got[1].Found || got[1].Type != "Patient" {
		t.Fatalf("include entry uncorrelated: %+v", got[1])
	}
	if got[2].Mode != "outcome" || !got[2].Found {
		t.Fatalf("outcome entry uncorrelated: %+v", got[2])
	}
	missing := CorrelateEntries(bundle, nil)
	for _, m := range missing {
		if m.Found || m.Identity != "" || m.Occurrence != "" {
			t.Fatalf("missing resource correlated: %+v", m)
		}
	}
	if len(missing) != 3 || missing[0].Mode != "match" || missing[0].Index != 0 {
		t.Fatalf("missing resources lost entry order: %+v", missing)
	}
}
