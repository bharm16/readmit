package hubprotocol_test

import (
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

// The wire bytes are the hub's: every member is always present, in the
// declared order, and a lifecycle event carries review_head even at zero, as
// the hub has always written it and a backup reader requires it.
func TestTheWireDocumentsKeepTheHubsBytes(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for name, c := range map[string]struct {
		value any
		want  string
	}{
		"review event": {hubprotocol.ReviewEvent{Schema: hubprotocol.ReviewEventV1, Project: "alpha", Sequence: 1, Issuer: "https://idp", Actor: "ana", At: "2026-09-24T12:00:00Z",
			Command: hubprotocol.ReviewCommand{Schema: hubprotocol.ReviewCommandV1, ID: "note", Kind: "comment", Evidence: digest, Text: "seen"}},
			`{"schema":"readmit-hub-review-event/v1","project":"alpha","sequence":1,"issuer":"https://idp","actor":"ana","at":"2026-09-24T12:00:00Z",` +
				`"command":{"schema":"readmit-hub-review-command/v1","id":"note","expected":0,"kind":"comment","evidence":"` + digest + `","parent":"","recipient":"","text":"seen","release":""}}`},
		"lifecycle event": {hubprotocol.LifecycleEvent{Schema: hubprotocol.LifecycleEventSchema, Project: "alpha", Sequence: 1, Issuer: "https://idp", Actor: "ana", At: "2026-09-24T12:00:00Z",
			Command: hubprotocol.LifecycleCommand{Schema: hubprotocol.LifecycleCommandSchema, ID: "edit", Kind: "revision", Resource: "case", Artifact: digest, Parents: []string{}, Reason: "edit"}},
			`{"schema":"readmit-hub-lifecycle-event/v1","project":"alpha","sequence":1,"issuer":"https://idp","actor":"ana","at":"2026-09-24T12:00:00Z","review_head":0,` +
				`"command":{"schema":"readmit-hub-lifecycle-command/v1","id":"edit","expected":0,"kind":"revision","resource":"case","artifact":"` + digest + `","parents":[],"subject":"","until":"","reason":"edit"}}`},
		"review history": {hubprotocol.ReviewHistory{Schema: hubprotocol.ReviewHistoryV2, Head: 0, Events: []hubprotocol.ReviewEvent{}},
			`{"schema":"readmit-hub-review-history/v2","head":0,"events":[]}`},
		"review query": {hubprotocol.ReviewQuery{Schema: hubprotocol.ReviewQuerySchema, After: 1, Text: "x"},
			`{"schema":"readmit-hub-review-query/v1","after":1,"text":"x","evidence":""}`},
		"lifecycle history": {hubprotocol.LifecycleHistory{Schema: hubprotocol.LifecycleHistorySchema, Events: []hubprotocol.LifecycleEvent{}, Tips: map[string][]string{}, Warning: hubprotocol.CustodyWarning},
			`{"schema":"readmit-hub-lifecycle-history/v1","head":0,"events":[],"tips":{},"warning":"Downloaded copies remain under local custody and cannot be revoked."}`},
		"audit export": {hubprotocol.AuditExport{Schema: hubprotocol.AuditV1, Project: "alpha", Lifecycle: []hubprotocol.LifecycleEvent{}, Reviews: []hubprotocol.ReviewEvent{}, Warning: hubprotocol.CustodyWarning},
			`{"schema":"readmit-hub-audit/v1","project":"alpha","lifecycle":[],"review_head":0,"reviews":[],"warning":"Downloaded copies remain under local custody and cannot be revoked."}`},
	} {
		got, err := json.Marshal(c.value)
		if err != nil || string(got) != c.want {
			t.Errorf("%s encodes as\n%s\nwant\n%s (%v)", name, got, c.want, err)
		}
	}
}

func TestEachReviewKindRidesTheVersionOfItsFamily(t *testing.T) {
	for kind, want := range map[string]string{
		"comment": hubprotocol.ReviewCommandV1, "assignment": hubprotocol.ReviewCommandV1,
		"review-request": hubprotocol.ReviewCommandV1, "approval": hubprotocol.ReviewCommandV1,
		"support-policy": hubprotocol.ReviewCommandV2, "support-request": hubprotocol.ReviewCommandV2,
		"support-approval": hubprotocol.ReviewCommandV2,
	} {
		if got, ok := hubprotocol.CommandSchema(kind); !ok || got != want {
			t.Errorf("%s rides %q (%v), want %q", kind, got, ok, want)
		}
	}
	if _, ok := hubprotocol.CommandSchema("grant-everything"); ok {
		t.Error("an unknown kind has a command version")
	}
}

func TestTheGrammarNamesProjectsDigestsAndText(t *testing.T) {
	for _, ok := range []string{"a", "cardio-study-2", strings.Repeat("z", 64)} {
		if !hubprotocol.ValidProject(ok) {
			t.Errorf("project %q refused", ok)
		}
	}
	for _, bad := range []string{"", "Cardio", "cardio_study", "cardio study", strings.Repeat("z", 65), "é"} {
		if hubprotocol.ValidProject(bad) {
			t.Errorf("project %q accepted", bad)
		}
	}
	if !hubprotocol.ValidDigest(strings.Repeat("0f", 32)) {
		t.Error("a whole lowercase digest refused")
	}
	for _, bad := range []string{strings.Repeat("0F", 32), strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("g", 64)} {
		if hubprotocol.ValidDigest(bad) {
			t.Errorf("digest %q accepted", bad)
		}
	}
	if !hubprotocol.ValidText("résumé", 8) || hubprotocol.ValidText("résumé", 7) || hubprotocol.ValidText("a\x00b", 8) || hubprotocol.ValidText("\xff", 8) {
		t.Error("text bound, NUL or UTF-8 misread")
	}
	if hubprotocol.RequireExactMembers([]byte(`{"a":1,"b":"x"}`), "a", "b") != nil {
		t.Error("an exact document refused")
	}
	for _, bad := range []string{`{"a":1}`, `{"a":1,"b":"x","c":2}`, `{"a":1,"b":null}`, `[]`} {
		if hubprotocol.RequireExactMembers([]byte(bad), "a", "b") == nil {
			t.Errorf("%s accepted as exactly a and b", bad)
		}
	}
}

func encoded(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestReviewCommandsAreReadStrictlyByKind(t *testing.T) {
	digest, release := strings.Repeat("a", 64), strings.Repeat("b", 64)
	base := hubprotocol.ReviewCommand{Schema: hubprotocol.ReviewCommandV1, ID: "note", Kind: "comment", Evidence: digest, Text: "seen"}
	accepted := map[string]hubprotocol.ReviewCommand{
		"comment":        base,
		"assignment":     {Schema: hubprotocol.ReviewCommandV1, ID: "assign", Kind: "assignment", Evidence: digest, Recipient: "rui", Text: "look"},
		"review-request": {Schema: hubprotocol.ReviewCommandV1, ID: "ask", Kind: "review-request", Evidence: digest, Recipient: "rui", Release: release, Text: "review"},
		"approval":       {Schema: hubprotocol.ReviewCommandV1, ID: "ok", Kind: "approval", Evidence: digest, Parent: "ask", Release: release, Text: "approved"},
		"support-policy": {Schema: hubprotocol.ReviewCommandV2, ID: "policy", Kind: "support-policy", Evidence: digest, Text: "support"},
	}
	for name, c := range accepted {
		got, err := hubprotocol.DecodeReviewCommand([]byte(encoded(t, c)))
		if err != nil || got != c {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
	}
	raw := encoded(t, base)
	refused := map[string]string{
		"an unknown member":             strings.TrimSuffix(raw, "}") + `,"actor":"owner"}`,
		"a missing member":              strings.Replace(raw, `"expected":0,`, "", 1),
		"a null member":                 strings.Replace(raw, `"parent":""`, `"parent":null`, 1),
		"an unknown version":            strings.Replace(raw, hubprotocol.ReviewCommandV1, "readmit-hub-review-command/v3", 1),
		"an id outside the grammar":     strings.Replace(raw, `"id":"note"`, `"id":"Note"`, 1),
		"a head below zero":             strings.Replace(raw, `"expected":0`, `"expected":-1`, 1),
		"a head at the log's bound":     strings.Replace(raw, `"expected":0`, `"expected":1024`, 1),
		"partial evidence":              strings.Replace(raw, digest, digest[:63], 1),
		"blank text":                    strings.Replace(raw, `"text":"seen"`, `"text":"  "`, 1),
		"a comment naming a release":    strings.Replace(raw, `"release":""`, `"release":"`+release+`"`, 1),
		"an unknown kind":               strings.Replace(raw, `"kind":"comment"`, `"kind":"grant"`, 1),
		"a support kind under v1":       strings.Replace(encoded(t, accepted["support-policy"]), hubprotocol.ReviewCommandV2, hubprotocol.ReviewCommandV1, 1),
		"support with free text":        strings.Replace(encoded(t, accepted["support-policy"]), `"support"`, `"PLANTED-PRIVATE-TEXT"`, 1),
		"a request without a recipient": strings.Replace(encoded(t, accepted["review-request"]), `"recipient":"rui"`, `"recipient":""`, 1),
		"an approval without a parent":  strings.Replace(encoded(t, accepted["approval"]), `"parent":"ask"`, `"parent":""`, 1),
		"an oversized document":         strings.Replace(raw, `"text":"seen"`, `"text":"`+strings.Repeat("s", hubprotocol.MaxCommandBytes)+`"`, 1),
	}
	for name, data := range refused {
		if _, err := hubprotocol.DecodeReviewCommand([]byte(data)); !errors.Is(err, hubprotocol.ErrRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestLifecycleCommandsAreReadStrictlyByKind(t *testing.T) {
	digest := strings.Repeat("a", 64)
	accepted := map[string]hubprotocol.LifecycleCommand{
		"revision":     {Schema: hubprotocol.LifecycleCommandSchema, ID: "edit", Kind: "revision", Resource: "case", Artifact: digest, Parents: []string{}, Reason: "edit"},
		"resolve":      {Schema: hubprotocol.LifecycleCommandSchema, ID: "merge", Kind: "resolve", Resource: "case", Artifact: digest, Parents: []string{"a", "b"}, Reason: "merge"},
		"remove-user":  {Schema: hubprotocol.LifecycleCommandSchema, ID: "remove", Kind: "remove-user", Parents: []string{}, Subject: "ana", Reason: "left"},
		"retention":    {Schema: hubprotocol.LifecycleCommandSchema, ID: "keep", Kind: "retention", Artifact: digest, Parents: []string{}, Until: "2027-01-01T00:00:00Z", Reason: "hold"},
		"retire":       {Schema: hubprotocol.LifecycleCommandSchema, ID: "retire", Kind: "retire", Artifact: digest, Parents: []string{}, Reason: "done"},
		"audit-export": {Schema: hubprotocol.LifecycleCommandSchema, ID: "audit", Kind: "audit-export", Parents: []string{}, Reason: "audit"},
	}
	for name, c := range accepted {
		if _, err := hubprotocol.DecodeLifecycleCommand([]byte(encoded(t, c))); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	refused := map[string]hubprotocol.LifecycleCommand{
		"a revision with two parents": {Schema: hubprotocol.LifecycleCommandSchema, ID: "edit", Kind: "revision", Resource: "case", Artifact: digest, Parents: []string{"a", "b"}, Reason: "edit"},
		"a resolve with one parent":   {Schema: hubprotocol.LifecycleCommandSchema, ID: "merge", Kind: "resolve", Resource: "case", Artifact: digest, Parents: []string{"a"}, Reason: "merge"},
		"parents out of order":        {Schema: hubprotocol.LifecycleCommandSchema, ID: "merge", Kind: "resolve", Resource: "case", Artifact: digest, Parents: []string{"b", "a"}, Reason: "merge"},
		"a removal naming a resource": {Schema: hubprotocol.LifecycleCommandSchema, ID: "remove", Kind: "remove-user", Resource: "case", Parents: []string{}, Subject: "ana", Reason: "left"},
		"a retention with no instant": {Schema: hubprotocol.LifecycleCommandSchema, ID: "keep", Kind: "retention", Artifact: digest, Parents: []string{}, Reason: "hold"},
		"an audit naming an artifact": {Schema: hubprotocol.LifecycleCommandSchema, ID: "audit", Kind: "audit-export", Artifact: digest, Parents: []string{}, Reason: "audit"},
		"no reason":                   {Schema: hubprotocol.LifecycleCommandSchema, ID: "audit", Kind: "audit-export", Parents: []string{}, Reason: " "},
		"an unknown kind":             {Schema: hubprotocol.LifecycleCommandSchema, ID: "purge", Kind: "purge", Parents: []string{}, Reason: "x"},
		"an unknown version":          {Schema: "readmit-hub-lifecycle-command/v2", ID: "audit", Kind: "audit-export", Parents: []string{}, Reason: "audit"},
	}
	for name, c := range refused {
		if _, err := hubprotocol.DecodeLifecycleCommand([]byte(encoded(t, c))); !errors.Is(err, hubprotocol.ErrRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := hubprotocol.DecodeLifecycleCommand([]byte(`{"schema":"readmit-hub-lifecycle-command/v1","id":"audit","expected":0,"kind":"audit-export","resource":"","artifact":"","subject":"","until":"","reason":"audit"}`)); err == nil {
		t.Error("a lifecycle command without parents accepted")
	}
}

// A query the contract cannot carry is refused with what to fix, and the
// hub's strict reader refuses the same queries.
func TestReviewQueriesSayWhatTheContractCannotCarry(t *testing.T) {
	good := hubprotocol.ReviewQuery{Schema: hubprotocol.ReviewQuerySchema, After: 2, Text: "reschedule", Evidence: strings.Repeat("c", 64)}
	if good.Refusal() != nil {
		t.Fatal(good.Refusal())
	}
	if got, err := hubprotocol.DecodeReviewQuery([]byte(encoded(t, good))); err != nil || got != good {
		t.Fatalf("%+v, %v", got, err)
	}
	for problem, q := range map[string]hubprotocol.ReviewQuery{
		"sequence of 0 or more": {Schema: hubprotocol.ReviewQuerySchema, After: -1},
		"at most 256 bytes":     {Schema: hubprotocol.ReviewQuerySchema, Text: strings.Repeat("r", 257)},
		"no NUL character":      {Schema: hubprotocol.ReviewQuerySchema, Text: "re\x00lease"},
		"whole SHA-256 digest":  {Schema: hubprotocol.ReviewQuerySchema, Evidence: strings.Repeat("c", 63)},
	} {
		if err := q.Refusal(); err == nil || !strings.Contains(err.Error(), strings.Split(problem, " ")[0]) {
			t.Errorf("%s: %v", problem, err)
		}
		if _, err := hubprotocol.DecodeReviewQuery([]byte(encoded(t, q))); !errors.Is(err, hubprotocol.ErrRefused) {
			t.Errorf("the strict reader accepted a query needing %s: %v", problem, err)
		}
	}
	for name, data := range map[string]string{
		"an unknown member":  `{"schema":"readmit-hub-review-query/v1","after":0,"text":"","evidence":"","actor":"x"}`,
		"a missing member":   `{"schema":"readmit-hub-review-query/v1","after":0,"text":""}`,
		"an unknown version": `{"schema":"readmit-hub-review-query/v2","after":0,"text":"","evidence":""}`,
	} {
		if _, err := hubprotocol.DecodeReviewQuery([]byte(data)); !errors.Is(err, hubprotocol.ErrRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestARecordedEventCarriesItsFamilysEventVersion(t *testing.T) {
	support := hubprotocol.ReviewEvent{Schema: hubprotocol.ReviewEventV2, Command: hubprotocol.ReviewCommand{Schema: hubprotocol.ReviewCommandV2, Kind: "support-policy"}}
	if hubprotocol.ValidEventVersion(support, false) || !hubprotocol.ValidEventVersion(support, true) {
		t.Fatal("the frozen event boundary widened")
	}
	support.Schema = hubprotocol.ReviewEventV1
	if hubprotocol.ValidEventVersion(support, true) {
		t.Fatal("a support command under the v1 event")
	}
	comment := hubprotocol.ReviewEvent{Schema: hubprotocol.ReviewEventV1, Command: hubprotocol.ReviewCommand{Schema: hubprotocol.ReviewCommandV1, Kind: "comment"}}
	if !hubprotocol.ValidEventVersion(comment, false) {
		t.Fatal("a v1 comment refused")
	}
	comment.Schema = hubprotocol.ReviewEventV2
	if hubprotocol.ValidEventVersion(comment, true) {
		t.Fatal("a v1 command under the v2 event")
	}
}
