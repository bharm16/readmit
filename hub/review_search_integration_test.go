package hub_test

// The review routes' reads through the real team handler over PostgreSQL:
// what the application's history search, notification list and notification
// search reach (#311). A search filters the project's recorded events by its
// query and answers the whole head; a notification is an event addressed to
// the caller by the issuer that authenticated them; a query the contract
// cannot carry, a caller without a grant and a method the route does not
// serve are refused. The v1 addresses answer the same until the project
// carries a support command, and then refuse, which is why the application
// reaches these capabilities through v2 only.

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestPostgresReviewSearchAndNotificationsReadOnlyWhatTheCallerMaySee(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	s := open(t, c)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	a, _, key, _ := accessFixture(t)
	h := s.TeamHandler(a)
	call := func(method, path, subject, body string) *httptest.ResponseRecorder {
		r := request(signed(t, key, claims(subject), accessHeader))
		r.Method = method
		r.URL.Path = path
		r.Body = httpBody([]byte(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	put := func(payload string) string {
		t.Helper()
		d := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
		if w := call("PUT", "/v1/projects/alpha/artifacts/"+d, "analyst", payload); w.Code != 201 {
			t.Fatalf("put: %d %s", w.Code, w.Body)
		}
		return d
	}
	first := put("synthetic reschedule evidence")
	second := put("synthetic cancellation evidence")
	comment := func(subject, id string, expected int, evidence, recipient, text string) {
		t.Helper()
		body := fmt.Sprintf(`{"schema":"readmit-hub-review-command/v1","id":%q,"expected":%d,"kind":"comment","evidence":%q,"parent":"","recipient":%q,"text":%q,"release":""}`,
			id, expected, evidence, recipient, text)
		if w := call("POST", "/v2/projects/alpha/reviews", subject, body); w.Code != 201 {
			t.Fatalf("comment %s: %d %s", id, w.Code, w.Body)
		}
	}
	comment("analyst", "analyst-reschedule", 0, first, "reviewer", "The reschedule duplicates the booking")
	comment("analyst", "analyst-cancellation", 1, second, "viewer", "The cancellation arrives late")
	comment("reviewer", "reviewer-confirms", 2, first, "analyst", "Confirmed the RESCHEDULE on the lab fixture")

	query := func(after int, text, evidence string) string {
		return fmt.Sprintf(`{"schema":"readmit-hub-review-query/v1","after":%d,"text":%q,"evidence":%q}`, after, text, evidence)
	}
	// read answers the route and returns the history schema, its head and the
	// command ids of the events it holds, in order.
	read := func(method, path, subject, body string) (string, int, []string) {
		t.Helper()
		w := call(method, path, subject, body)
		if w.Code != 200 {
			t.Fatalf("%s %s as %s: %d %s", method, path, subject, w.Code, w.Body)
		}
		var history struct {
			Schema string `json:"schema"`
			Head   int    `json:"head"`
			Events []struct {
				Sequence int    `json:"sequence"`
				Issuer   string `json:"issuer"`
				Actor    string `json:"actor"`
				Command  struct {
					ID string `json:"id"`
				} `json:"command"`
			} `json:"events"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil || history.Events == nil {
			t.Fatalf("%s %s: %v %s", method, path, err, w.Body)
		}
		ids := []string{}
		for _, event := range history.Events {
			ids = append(ids, event.Command.ID)
		}
		return history.Schema, history.Head, ids
	}
	expect := func(name string, got []string, want ...string) {
		t.Helper()
		if !slices.Equal(got, append([]string{}, want...)) {
			t.Fatalf("%s holds %v, want %v", name, got, want)
		}
	}

	// History: the whole log, and a search of it by text (case-insensitive),
	// by evidence and after a sequence, each answering the whole head.
	schema, head, ids := read("GET", "/v2/projects/alpha/history", "viewer", "")
	if schema != "readmit-hub-review-history/v2" || head != 3 {
		t.Fatalf("history is %s at head %d", schema, head)
	}
	expect("the history", ids, "analyst-reschedule", "analyst-cancellation", "reviewer-confirms")
	for _, search := range []struct {
		name string
		body string
		want []string
	}{
		{"a text search", query(0, "reschedule", ""), []string{"analyst-reschedule", "reviewer-confirms"}},
		{"an evidence search after the first event", query(1, "", first), []string{"reviewer-confirms"}},
		{"a search after the head", query(3, "", ""), nil},
	} {
		schema, head, ids := read("POST", "/v2/projects/alpha/history", "viewer", search.body)
		if schema != "readmit-hub-review-history/v2" || head != 3 {
			t.Fatalf("%s answered %s at head %d", search.name, schema, head)
		}
		expect(search.name, ids, search.want...)
	}

	// Notifications: only what is addressed to the caller, listed and searched.
	for subject, want := range map[string][]string{
		"reviewer": {"analyst-reschedule"},
		"analyst":  {"reviewer-confirms"},
		"viewer":   {"analyst-cancellation"},
		"owner":    nil,
	} {
		_, head, ids := read("GET", "/v2/projects/alpha/notifications", subject, "")
		if head != 3 {
			t.Fatalf("%s's notifications answered head %d", subject, head)
		}
		expect(subject+"'s notifications", ids, want...)
	}
	_, _, ids = read("POST", "/v2/projects/alpha/notifications", "analyst", query(0, "lab", ""))
	expect("the analyst's notifications about the lab", ids, "reviewer-confirms")
	_, _, ids = read("POST", "/v2/projects/alpha/notifications", "analyst", query(0, "booking", ""))
	expect("the analyst's notifications about the booking, which were addressed to the reviewer", ids)

	// A query the contract cannot carry, a caller the policy grants nothing
	// and a method neither route serves are refused.
	for _, route := range []string{"/v2/projects/alpha/history", "/v2/projects/alpha/notifications"} {
		for problem, body := range map[string]string{
			"an unknown member":     strings.Replace(query(0, "", ""), `"evidence"`, `"extra":1,"evidence"`, 1),
			"a missing member":      `{"schema":"readmit-hub-review-query/v1","after":0,"text":""}`,
			"another version":       strings.Replace(query(0, "", ""), "review-query/v1", "review-query/v2", 1),
			"a negative sequence":   query(-1, "", ""),
			"evidence not digested": query(0, "", "the reschedule message"),
			"text beyond 256 bytes": query(0, strings.Repeat("r", 257), ""),
		} {
			if w := call("POST", route, "viewer", body); w.Code != 400 {
				t.Fatalf("%s with %s answered %d", route, problem, w.Code)
			}
		}
		for _, method := range []string{"GET", "POST"} {
			if w := call(method, route, "stranger", query(0, "", "")); w.Code != 403 {
				t.Fatalf("%s %s for a caller without a grant answered %d", method, route, w.Code)
			}
		}
		if w := call("PUT", route, "viewer", query(0, "", "")); w.Code != 405 {
			t.Fatalf("PUT %s answered %d", route, w.Code)
		}
	}

	// The v1 addresses answer the same reads under the v1 history contract
	// while the project carries no support command.
	schema, _, ids = read("POST", "/v1/projects/alpha/history", "viewer", query(0, "reschedule", ""))
	if schema != "readmit-hub-review-history/v1" {
		t.Fatalf("the v1 history search answered %s", schema)
	}
	expect("the v1 history search", ids, "analyst-reschedule", "reviewer-confirms")
	_, _, ids = read("GET", "/v1/projects/alpha/notifications", "reviewer", "")
	expect("the reviewer's v1 notifications", ids, "analyst-reschedule")
	_, _, ids = read("POST", "/v1/projects/alpha/notifications", "analyst", query(0, "lab", ""))
	expect("the analyst's v1 notification search", ids, "reviewer-confirms")

	// Once the project carries a support command the v1 contract cannot
	// express it, so the v1 addresses refuse and v2 reads on.
	policy := put(`{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["customer-hub-download"],"max_bytes":4096}`)
	announce := fmt.Sprintf(`{"schema":"readmit-hub-review-command/v2","id":"policy","expected":3,"kind":"support-policy","evidence":%q,"parent":"","recipient":"","text":"support","release":""}`, policy)
	if w := call("POST", "/v2/projects/alpha/reviews", "admin", announce); w.Code != 201 {
		t.Fatalf("support policy: %d %s", w.Code, w.Body)
	}
	for _, v1 := range []struct{ method, path string }{
		{"POST", "/v1/projects/alpha/history"}, {"GET", "/v1/projects/alpha/notifications"}, {"POST", "/v1/projects/alpha/notifications"},
	} {
		if w := call(v1.method, v1.path, "reviewer", query(0, "", "")); w.Code != 409 {
			t.Fatalf("%s %s after a support command answered %d", v1.method, v1.path, w.Code)
		}
	}
	_, head, ids = read("POST", "/v2/projects/alpha/history", "reviewer", query(3, "", ""))
	if head != 4 {
		t.Fatalf("the v2 history search after the support policy answered head %d", head)
	}
	expect("the v2 history search after the support policy", ids, "policy")
	_, _, ids = read("GET", "/v2/projects/alpha/notifications", "reviewer", "")
	expect("the reviewer's v2 notifications after the support policy", ids, "analyst-reschedule")
}
