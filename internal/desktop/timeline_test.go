package desktop_test

import (
	"encoding/json/v2"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/sequenceanalysis"
)

// A timeline is the sequence laid out for reading: lanes, one time basis,
// clocks that say which instants are comparable, the relationships of the
// window and the whole case's problems. These tests hold it to the evidence.

func occurrencesOf(events []desktop.SequenceEvent) []string {
	named := make([]string, 0, len(events))
	for _, event := range events {
		named = append(named, event.Occurrence)
	}
	return named
}

func lanesOf(sequence *desktop.Sequence) []string {
	named := []string{}
	for _, lane := range sequence.Lanes {
		named = append(named, lane.SourceID)
	}
	return named
}

// recordedIncident writes the incident's two captures as one recorded
// session: one recorder observed both, so its observed times share a clock.
func recordedIncident(t *testing.T, root, name string, inputs []bundle.Input) *bundle.Bundle {
	t.Helper()
	snapshot := observation.Snapshot{Schema: observation.Schema, Profile: observation.Profile, SessionID: strings.Repeat("d", 32),
		Mode: observation.Fixed, Processed: []observation.Occurrence{}, Consistent: true, Records: []observation.Record{}}
	inputs = slices.Clone(inputs)
	for i := range inputs {
		inputs[i].Path = ""
	}
	written, err := bundle.WriteRecorded(filepath.Join(root, name), inputs, *seqTime(0), snapshot)
	if err != nil {
		t.Fatalf("a recorded case: %v", err)
	}
	return written
}

// Lanes follow the order each source first appears in the evidence, and
// neither the time basis nor what the window selects reorders them: the
// receiver recorded its copy first, and is still the second lane.
func TestLanesFollowFirstAppearanceInEvidence(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	for _, basis := range []desktop.TimeBasis{desktop.ObservedBasis, desktop.MessageBasis, desktop.SourceBasis} {
		request := sequenceRequest(root, identity, seqRulesEntry)
		request.Basis = basis
		if got := lanesOf(laidOut(t, app, request)); !slices.Equal(got, []string{"s0001", "s0002"}) {
			t.Fatalf("%s lanes are %v", basis, got)
		}
	}
	request := sequenceRequest(root, identity, seqRulesEntry)
	request.Sources = []string{"s0002"}
	if got := lanesOf(laidOut(t, app, request)); !slices.Equal(got, []string{"s0001", "s0002"}) {
		t.Fatalf("selecting a source reordered or dropped lanes: %v", got)
	}
}

// Events at one instant keep the order their source recorded them in, and
// under one shared clock two sources at one instant stack in lane order.
func TestEqualInstantsKeepSourceSequenceOrder(t *testing.T) {
	app, root, _ := sequenceWorkspace(t)
	same := map[int]bundle.Observation{
		1: {Direction: bundle.Outbound, ObservedAt: seqTime(9)},
		2: {Direction: bundle.Outbound, ObservedAt: seqTime(9)},
		3: {Direction: bundle.Outbound, ObservedAt: seqTime(9)},
	}
	three := []byte(framed(seqBooking) + framed(strings.Replace(seqBooking, "CTL-1", "CTL-2", 1)) + framed(strings.Replace(seqBooking, "CTL-1", "CTL-3", 1)))
	written := recordedIncident(t, root, "same-instant", []bundle.Input{
		{Path: "a", Data: three, Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}, Observations: same},
		{Path: "b", Data: []byte(framed(seqBooking)), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
			Observations: map[int]bundle.Observation{1: {Direction: bundle.Inbound, ObservedAt: seqTime(9)}}},
	})
	request := sequenceRequest(root, written.Identity, "")
	request.Case = "same-instant"
	sequence := laidOut(t, app, request)
	want := []string{"s0001-e000001", "s0001-e000002", "s0001-e000003", "s0002-e000001"}
	if got := occurrencesOf(sequence.Events); !slices.Equal(got, want) {
		t.Fatalf("equal instants are in order %v, not %v", got, want)
	}
	for i, event := range sequence.Events {
		if event.Position != i+1 || event.At == nil || !event.At.Equal(*seqTime(9)) {
			t.Fatalf("an event at an equal instant: %+v", event)
		}
	}
	if sequence.Events[2].SourceSequence != 3 || sequence.Events[3].SourceSequence != 1 {
		t.Fatalf("source sequence numbers are not carried: %+v", sequence.Events)
	}
}

// Two imported captures are two clocks: each source is ordered on its own
// and nothing aligns them. One recorded session observed both, so its
// observed times are one clock and one axis, and interleave.
func TestSourcesShareATimeAxisOnlyUnderOneRecordedClock(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	imported := laidOut(t, app, sequenceRequest(root, identity, ""))
	if len(imported.Clocks) != 2 || imported.Clocks[0].Kind != desktop.SourceClock || !slices.Equal(imported.Clocks[0].Sources, []string{"s0001"}) ||
		imported.Clocks[1].Kind != desktop.SourceClock || !slices.Equal(imported.Clocks[1].Sources, []string{"s0002"}) {
		t.Fatalf("imported captures share a clock: %+v", imported.Clocks)
	}
	if got := occurrencesOf(imported.Events)[:4]; !slices.Equal(got, []string{"s0001-e000001", "s0001-e000002", "s0001-e000004", "s0002-e000001"}) {
		t.Fatalf("two clocks were interleaved: %v", got)
	}
	for _, event := range imported.Events {
		if event.At != nil && event.Clock != "source:"+event.SourceID {
			t.Fatalf("an event is placed on another source's clock: %+v", event)
		}
	}
	// A coverage declaration's clock tolerance aligns no clock with another.
	tolerant := `{"schema":"readmit-sequence-analysis/v1","rules_sha256":"","case_identity":"` + identity + `","clock_tolerance_seconds":3600,` +
		`"windows":[{"source":"s0001","start":"2026-01-01T11:00:00Z","end":"2026-01-01T13:00:00Z","coverage":"complete"},` +
		`{"source":"s0002","start":"2026-01-01T11:00:00Z","end":"2026-01-01T13:00:00Z","coverage":"complete"}],"retries":[],"downstream":[]}`
	if err := os.WriteFile(filepath.Join(root, "tolerant.json"), []byte(tolerant), 0o600); err != nil {
		t.Fatal(err)
	}
	withTolerance := sequenceRequest(root, identity, "")
	withTolerance.Analysis = "tolerant.json"
	tolerated := laidOut(t, app, withTolerance)
	if tolerated.Analysis == nil || len(tolerated.Clocks) != 2 || tolerated.Clocks[0].Kind != desktop.SourceClock ||
		!slices.Equal(occurrencesOf(tolerated.Events), occurrencesOf(imported.Events)) {
		t.Fatalf("a clock tolerance aligned two captures' clocks: %+v", tolerated.Clocks)
	}

	written := recordedIncident(t, root, "recorded", incidentInputs())
	request := sequenceRequest(root, written.Identity, "")
	request.Case = "recorded"
	recorded := laidOut(t, app, request)
	if len(recorded.Clocks) != 1 || recorded.Clocks[0].Kind != desktop.SessionClock || !slices.Equal(recorded.Clocks[0].Sources, []string{"s0001", "s0002"}) {
		t.Fatalf("one recorded session is not one clock: %+v", recorded.Clocks)
	}
	want := []string{"s0002-e000001", "s0001-e000001", "s0001-e000002", "s0001-e000004", "s0001-e000003", "s0002-e000002"}
	if got := occurrencesOf(recorded.Events); !slices.Equal(got, want) {
		t.Fatalf("one clock's instants were not interleaved: %v, not %v", got, want)
	}
	if recorded.Untimed != 2 || recorded.Events[4].At != nil || recorded.Events[4].Clock != "" {
		t.Fatalf("untimed events are not a section after the timed ones: %+v", recorded)
	}
}

// Message time is the instant the message declares, on its sender's clock;
// it never replaces the observed time, and a declared time with no known
// offset places nothing.
func TestMessageTimeIsTheDeclaredInstantAndNeverObservedTime(t *testing.T) {
	app, root, _ := sequenceWorkspace(t)
	declared := strings.Replace(seqBooking, "|20260101120000|", "|20260101113000+0100|", 1)
	written := writeInputs(t, root, "declared", []bundle.Input{
		{Path: "a", Data: []byte(framed(declared) + framed(seqBooking)), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
			Observations: map[int]bundle.Observation{1: {Direction: bundle.Outbound, ObservedAt: seqTime(7)}, 2: {Direction: bundle.Outbound, ObservedAt: seqTime(8)}}},
	})
	request := sequenceRequest(root, written.Identity, "")
	request.Case, request.Basis = "declared", desktop.MessageBasis
	sequence := laidOut(t, app, request)
	if sequence.Basis != desktop.MessageBasis || len(sequence.Clocks) != 1 || sequence.Clocks[0].Kind != desktop.SenderClock {
		t.Fatalf("message time is not the senders' clock: %+v", sequence.Clocks)
	}
	first := eventAt(t, sequence, "s0001-e000001")
	instant := time.Date(2026, 1, 1, 10, 30, 0, 0, time.UTC)
	if first.At == nil || !first.At.Equal(instant) || first.Ordering != desktop.MessageOrder {
		t.Fatalf("the declared instant was not the message time: %+v", first)
	}
	if first.ObservedAt == nil || !first.ObservedAt.Equal(*seqTime(7)) {
		t.Fatalf("message time replaced the observed time: %+v", first)
	}
	second := eventAt(t, sequence, "s0001-e000002")
	if second.At != nil || second.Ordering != desktop.UnknownOrder || sequence.Untimed != 1 {
		t.Fatalf("a declared time with no offset was placed: %+v", second)
	}
}

// Source order places nothing in time: no instant, no clock, no untimed
// section, and every event in its source's own order.
func TestSourceOrderPlacesNoEventInTime(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	request := sequenceRequest(root, identity, "")
	request.Basis = desktop.SourceBasis
	sequence := laidOut(t, app, request)
	want := []string{"s0001-e000001", "s0001-e000002", "s0001-e000003", "s0001-e000004", "s0002-e000001", "s0002-e000002"}
	if got := occurrencesOf(sequence.Events); !slices.Equal(got, want) {
		t.Fatalf("source order is %v, not %v", got, want)
	}
	for _, event := range sequence.Events {
		if event.At != nil || event.Clock != "" || event.Ordering != desktop.SourceSequenceOrder {
			t.Fatalf("source order placed an event in time: %+v", event)
		}
	}
	if len(sequence.Clocks) != 0 || sequence.Untimed != 0 {
		t.Fatalf("source order reported clocks or an untimed section: %+v", sequence)
	}
	request.Filter = desktop.UntimedFilter
	if got := app.OpenSequence(request); got.State != desktop.Failed {
		t.Fatalf("an untimed filter over source order: %+v", got)
	}
}

// Selecting sources or a filter narrows the events listed, never what is
// counted beside them.
func TestAFilteredWindowCountsTheWholeCase(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	whole := laidOut(t, app, sequenceRequest(root, identity, seqRulesEntry))
	request := sequenceRequest(root, identity, seqRulesEntry)
	request.Sources = []string{"s0002"}
	narrowed := laidOut(t, app, request)
	if got := occurrencesOf(narrowed.Events); !slices.Equal(got, []string{"s0002-e000001", "s0002-e000002"}) || narrowed.Total != 2 {
		t.Fatalf("selecting a source: %v of %d", got, narrowed.Total)
	}
	if narrowed.Summary != whole.Summary || narrowed.Problems != whole.Problems || narrowed.Untimed != whole.Untimed || len(narrowed.Lanes) != 2 {
		t.Fatalf("a narrowed window counted itself: %+v", narrowed)
	}
	request.Sources, request.Filter = nil, desktop.UntimedFilter
	untimed := laidOut(t, app, request)
	if got := occurrencesOf(untimed.Events); !slices.Equal(got, []string{"s0001-e000003", "s0002-e000002"}) || untimed.Summary != whole.Summary {
		t.Fatalf("the untimed filter: %v", got)
	}
	request.Filter, request.Sources = "", []string{"s0009"}
	if got := app.OpenSequence(request); got.State != desktop.Failed || got.Sequence != nil {
		t.Fatalf("a source the case does not declare was selected: %+v", got)
	}
	request.Sources, request.Filter = []string{"s0002"}, desktop.UnresolvedFilter
	if got := app.OpenSequence(request); got.State != desktop.Empty || got.Sequence == nil || got.Sequence.Problems != whole.Problems {
		t.Fatalf("a selection matching nothing: %+v", got)
	}
}

// Problems are the whole case's actual unresolved links and gaps, each
// filtering to its own events; a case with none reports zero, and the window
// then shows nothing.
func TestProblemsCountOnlyNonzeroUnresolvedLinksAndGaps(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	sequence := laidOut(t, app, sequenceRequest(root, identity, seqRulesEntry))
	if sequence.Problems != (desktop.TimelineProblems{UnresolvedLinks: 1, Gaps: 2}) {
		t.Fatalf("the incident's problems: %+v", sequence.Problems)
	}
	request := sequenceRequest(root, identity, seqRulesEntry)
	request.Filter = desktop.UnresolvedFilter
	if got := occurrencesOf(laidOut(t, app, request).Events); !slices.Equal(got, []string{"s0001-e000004"}) {
		t.Fatalf("unresolved links filter to %v", got)
	}
	request.Filter = desktop.GapsFilter
	if got := occurrencesOf(laidOut(t, app, request).Events); !slices.Equal(got, []string{"s0002-e000001", "s0001-e000003"}) {
		t.Fatalf("gaps filter to %v", got)
	}
	// An acknowledgement declared across both captures matches two messages:
	// the collision is one more unresolved link, and nothing is merged.
	ambiguous := strings.Replace(seqRules, `"operator": "acknowledges", "scope": "source"`, `"operator": "acknowledges", "scope": "declared", "sources": ["s0001", "s0002"]`, 1)
	if err := os.WriteFile(filepath.Join(root, "ambiguous.rules.json"), []byte(ambiguous), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := laidOut(t, app, sequenceRequest(root, identity, "ambiguous.rules.json")); got.Problems.UnresolvedLinks <= 1 {
		t.Fatalf("a collision is not an unresolved link: %+v", got.Problems)
	}
	written := writeInputs(t, root, "clean", []bundle.Input{{Path: "a", Data: []byte(framed(seqBooking) + framed(seqBookingACK)),
		Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}}})
	clean := sequenceRequest(root, written.Identity, "")
	clean.Case = "clean"
	if got := laidOut(t, app, clean); got.Problems != (desktop.TimelineProblems{}) {
		t.Fatalf("a clean case reports problems: %+v", got.Problems)
	}
}

// Every answer, a refusal included, carries the request it answers.
func TestATimelineAnswerCarriesItsRequestContext(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	context := desktop.RequestContext{Project: root, Generation: 7}
	request := sequenceRequest("", identity, seqRulesEntry)
	request.Context = context
	answered := app.OpenSequence(request)
	if answered.State != desktop.Completed || answered.Context != context || answered.Sequence.Context != context {
		t.Fatalf("a timeline answered without its context: %+v", answered)
	}
	request.Identity = "stale"
	if refused := app.OpenSequence(request); refused.State != desktop.Failed || refused.Context != context {
		t.Fatalf("a refusal answered without its context: %+v", refused)
	}
}

// timelineProject is the incident inside a named project, with the case's
// catalog reference.
func timelineProject(t *testing.T) (*desktop.App, desktop.RequestContext, string, desktop.ItemRef) {
	t.Helper()
	app, context := namedProject(t)
	written := writeInputs(t, context.Project, "incident", incidentInputs())
	listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
	if listed.Page == nil {
		t.Fatalf("cases: %+v", listed)
	}
	for _, item := range listed.Page.Items {
		if item.Summary.Case != nil && item.Summary.Case.Entry == "incident" {
			return app, context, written.Identity, item.Ref
		}
	}
	t.Fatalf("the incident is not listed: %+v", listed.Page.Items)
	return nil, context, "", desktop.ItemRef{}
}

func rulesOf(t *testing.T, document string) *correlate.Rules {
	t.Helper()
	var rules correlate.Rules
	if err := json.Unmarshal([]byte(document), &rules); err != nil {
		t.Fatal(err)
	}
	return &rules
}

// saveLinkRules publishes one link rule version and answers its reference.
func saveLinkRules(t *testing.T, app *desktop.App, context desktop.RequestContext, item, base, intent string, rules *correlate.Rules) desktop.ItemRef {
	t.Helper()
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.LinkRulesItem, Item: item, BaseRevision: base,
		Draft: desktop.ItemDraft{Name: "Interface links", LinkRules: rules}, IntentID: intent})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("link rules: %+v", saved)
	}
	return *saved.Saved
}

const acknowledgementsOnly = `{"schema":"readmit-correlation-rules/v1","rules":[{"id":"acknowledgements","operator":"acknowledges","scope":"source"}]}`

// A timeline names exactly the link rule and coverage versions it applied,
// and a coverage bound to one link rule version is never applied with
// another.
func TestATimelineAppliesTheExactLinkRuleAndCoverageRevisions(t *testing.T) {
	app, context, identity, incident := timelineProject(t)
	first := saveLinkRules(t, app, context, "", "", "rules-1", rulesOf(t, acknowledgementsOnly))
	second := saveLinkRules(t, app, context, first.ID, "1", "rules-2", rulesOf(t, seqRules))
	coverage := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.CoverageItem, IntentID: "coverage-1",
		Draft: desktop.ItemDraft{Name: "Sender window", Coverage: &desktop.CoverageDraft{Case: incident, LinkRules: &first,
			Windows:  []desktop.CoverageWindow{{Source: "s0001", Start: "2026-01-01T12:00:00Z", End: "2026-01-01T12:00:02Z", Coverage: "partial"}},
			Retries:  []sequenceanalysis.Retry{},
			Expected: []sequenceanalysis.Downstream{}}}})
	if coverage.Outcome != desktop.SavedOutcome {
		t.Fatalf("coverage: %+v", coverage)
	}
	request := desktop.SequenceRequest{Context: context, Case: "incident", Identity: identity, Limit: desktop.MaxSequenceEvents,
		LinkRules: &desktop.ItemRef{Kind: desktop.LinkRulesItem, ID: first.ID, Revision: "1"}, Coverage: coverage.Saved}
	applied := laidOut(t, app, request)
	if *applied.LinkRules != first || applied.LinkRulesName != "Interface links" || len(applied.Declared) != 1 ||
		*applied.Coverage != *coverage.Saved || applied.CoverageName != "Sender window" || applied.Analysis == nil {
		t.Fatalf("the versions applied: %+v", applied)
	}
	// The sender's booking is acknowledged only after the declared window
	// closes: inside the window its acknowledgement is missing, one more gap
	// beside the two unacknowledged messages.
	if applied.Problems.Gaps != 3 {
		t.Fatalf("coverage gaps: %+v", applied.Problems)
	}
	request.LinkRules = &desktop.ItemRef{Kind: desktop.LinkRulesItem, ID: first.ID}
	request.Coverage = nil
	current := laidOut(t, app, request)
	if *current.LinkRules != second || len(current.Declared) != 3 {
		t.Fatalf("no revision named is the current one, named exactly: %+v", current.LinkRules)
	}
	request.Coverage = coverage.Saved
	if got := app.OpenSequence(request); got.State != desktop.Failed || !strings.Contains(got.Reason, "digest") {
		t.Fatalf("coverage bound to other link rules was applied: %+v", got)
	}
	request.LinkRules = &desktop.ItemRef{Kind: desktop.LinkRulesItem, ID: first.ID, Revision: "9"}
	if got := app.OpenSequence(request); got.State != desktop.Failed {
		t.Fatalf("a version the project does not hold: %+v", got)
	}
	request.LinkRules, request.Rules = &first, seqRulesEntry
	if got := app.OpenSequence(request); got.State != desktop.Failed {
		t.Fatalf("named rules and a workspace document at once: %+v", got)
	}
}

// Link rules imported as a file open whole, keep every identity they were
// imported with and every authority mapping the editor never shows, and save
// once as the first revision of the same object.
func TestALinkRuleIsSavedWholeAndKeepsImportedIDsAndUnshownAuthorities(t *testing.T) {
	app, context, _, _ := timelineProject(t)
	if err := os.WriteFile(filepath.Join(context.Project, seqRulesEntry), []byte(seqRules), 0o600); err != nil {
		t.Fatal(err)
	}
	listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.LinkRulesItem})
	if listed.Page == nil || len(listed.Page.Items) != 1 || listed.Page.Items[0].Summary.LinkRules == nil || listed.Page.Items[0].Summary.LinkRules.Rules != 3 ||
		!slices.Contains(listed.Page.Items[0].Capabilities, desktop.SaveAction) {
		t.Fatalf("imported link rules are not listed: %+v", listed)
	}
	imported := listed.Page.Items[0].Ref
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: imported})
	if opened.State != desktop.Completed || opened.Draft == nil || opened.Draft.LinkRules == nil || len(opened.Draft.LinkRules.Authorities) != 1 {
		t.Fatalf("the imported draft: %+v", opened)
	}
	draft := *opened.Draft
	draft.Name = "Interface links"
	edited := *draft.LinkRules
	edited.Rules = append(slices.Clone(edited.Rules), correlate.Rule{Operator: correlate.ControlID, Scope: correlate.SourceScope})
	edited.Rules[1].Sources = []string{"s0002", "s0001"}
	draft.LinkRules = &edited
	request := desktop.SaveItemRequest{Context: context, Kind: desktop.LinkRulesItem, Item: imported.ID, Draft: draft, IntentID: "save-imported"}
	saved := app.SaveItem(request)
	if saved.Outcome != desktop.SavedOutcome || saved.Saved.ID != imported.ID || saved.Saved.Revision != "1" {
		t.Fatalf("the first revision of an imported object: %+v", saved)
	}
	if again := app.SaveItem(request); !again.Replayed || *again.Saved != *saved.Saved {
		t.Fatalf("one click saved twice: %+v", again)
	}
	reopened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	rules := reopened.Draft.LinkRules
	ids := []string{}
	for _, rule := range rules.Rules {
		ids = append(ids, rule.ID)
	}
	if !slices.Equal(ids, []string{"acknowledgements", "same-message", "patient", "rule-1"}) {
		t.Fatalf("identities after the save: %v", ids)
	}
	if len(rules.Authorities) != 1 || rules.Authorities[0].Key != "READMIT-MR" || rules.Rules[2].Value != "PID-3.1" ||
		!slices.Equal(rules.Rules[1].Sources, []string{"s0002", "s0001"}) || reopened.Draft.Name != "Interface links" {
		t.Fatalf("a member the editor did not change was lost: %+v", reopened.Draft)
	}
	invalid := draft
	broken := *draft.LinkRules
	broken.Rules = append(slices.Clone(broken.Rules), correlate.Rule{Operator: correlate.Identifier, Scope: correlate.SourceScope})
	invalid.LinkRules = &broken
	refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.LinkRulesItem, Item: imported.ID, BaseRevision: "1", Draft: invalid, IntentID: "broken"})
	if refused.Outcome != desktop.InvalidOutcome || len(refused.Problems) != 1 || refused.Problems[0].Field != "link_rules.rules[4]" {
		t.Fatalf("an incomplete rule row: %+v", refused)
	}
}

// Coverage names its case and link rules as the project's objects; what it
// saves binds that case's identity and those rules' digest, and opens back
// as the same objects.
func TestCoverageBindsTheCaseAndLinkRuleDigestFromContext(t *testing.T) {
	app, context, identity, incident := timelineProject(t)
	rules := saveLinkRules(t, app, context, "", "", "rules", rulesOf(t, seqRules))
	draft := desktop.CoverageDraft{Case: incident, LinkRules: &desktop.ItemRef{Kind: desktop.LinkRulesItem, ID: rules.ID}, ClockToleranceSeconds: 5,
		Windows: []desktop.CoverageWindow{
			{Source: "s0001", Start: "2026-01-01T13:00:00+01:00", End: "2026-01-01T12:01:00Z", Coverage: "partial"},
			{Source: "s0002", Start: "2026-01-01T12:00:00Z", End: "2026-01-01T12:01:00Z", Coverage: "complete"}},
		Retries:  []sequenceanalysis.Retry{},
		Expected: []sequenceanalysis.Downstream{{Occurrence: "s0001-e000001", Source: "s0002", Rule: seqControlRule}}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.CoverageItem, IntentID: "coverage",
		Draft: desktop.ItemDraft{Name: "Both captures", Coverage: &draft}})
	if saved.Outcome != desktop.SavedOutcome || saved.Projection.Coverage.LinkRules.Revision != "1" {
		t.Fatalf("coverage: %+v", saved)
	}
	var declaration sequenceanalysis.Declaration
	if err := filepath.WalkDir(context.Project, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(path, "coverage.json") {
			data, _ := os.ReadFile(path)
			declaration, err = sequenceanalysis.Parse(data)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	digest, err := correlate.RulesDigest(*rulesOf(t, seqRules))
	if err != nil || declaration.CaseIdentity != identity || declaration.RulesSHA256 != digest || len(declaration.Downstream) != 1 {
		t.Fatalf("the saved declaration is not bound to its case and rules: %+v", declaration)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if opened.Draft == nil || opened.Draft.Coverage.Case.ID != incident.ID || *opened.Draft.Coverage.LinkRules != rules ||
		opened.Draft.Coverage.Windows[0].Start != "2026-01-01T13:00:00+01:00" || opened.Draft.Coverage.ClockToleranceSeconds != 5 {
		t.Fatalf("the coverage draft reopened: %+v", opened.Draft)
	}
	listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CoverageItem})
	summary := listed.Page.Items[0].Summary.Coverage
	if summary == nil || summary.Case.ID != incident.ID || !summary.BindsLinkRules || *summary.LinkRules != rules || summary.Windows != 2 || summary.Expected != 1 {
		t.Fatalf("the coverage summary: %+v", summary)
	}
	fresh := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.CoverageItem}})
	if !fresh.New || fresh.Draft.Coverage == nil || len(fresh.Draft.Coverage.Windows) != 0 {
		t.Fatalf("a new coverage draft: %+v", fresh)
	}
}

// Every row that cannot be saved is a problem at that row, and nothing is
// written.
func TestAnInvalidCoverageRowIsAProblemAtItsRowAndSavesNothing(t *testing.T) {
	app, context, _, incident := timelineProject(t)
	before := entries(t, context.Project)
	draft := desktop.CoverageDraft{Case: incident, ClockToleranceSeconds: -1,
		Windows: []desktop.CoverageWindow{
			{Source: "s0001", Start: "2026-01-01T12:00:00Z", End: "2026-01-01T12:01:00Z", Coverage: "partial"},
			{Source: "s0009", Start: "2026-01-01T12:00:00Z", End: "2026-01-01T12:01:00Z", Coverage: "partial"},
			{Source: "s0002", Start: "2026-01-01T12:00:00Z", End: "2026-01-01T11:00:00Z", Coverage: "partial"},
			{Source: "s0003", Start: "2026-01-01T12:00:00", End: "2026-01-01T12:01:00", TimeZone: "Mars/Olympus", Coverage: "partial"},
			{Source: "s0004", Start: "2026-03-08T02:30:00", End: "2026-11-01T01:30:00", TimeZone: "America/Chicago", Coverage: "all"}},
		Retries:  []sequenceanalysis.Retry{{First: "s0001-e000001", Retry: "s0001-e000003", Basis: "because"}},
		Expected: []sequenceanalysis.Downstream{{Occurrence: "s0001-e000001", Source: "s0002", Rule: "same-message"}}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.CoverageItem, IntentID: "invalid",
		Draft: desktop.ItemDraft{Name: "Windows", Coverage: &draft}})
	fields := []string{}
	for _, problem := range saved.Problems {
		fields = append(fields, problem.Field)
	}
	want := []string{"coverage.clock_tolerance_seconds", "coverage.windows[1].source", "coverage.windows[2].end",
		"coverage.windows[3].time_zone", "coverage.windows[3].source",
		"coverage.windows[4].source", "coverage.windows[4].start", "coverage.windows[4].end", "coverage.windows[4].coverage",
		"coverage.retries[0].basis", "coverage.expected[0].rule"}
	if saved.Outcome != desktop.InvalidOutcome || saved.Saved != nil || !slices.Equal(fields, want) {
		t.Fatalf("problems at %v, not %v: %+v", fields, want, saved)
	}
	if after := entries(t, context.Project); !slices.Equal(after, before) {
		t.Fatalf("an invalid coverage wrote: %v", after)
	}
	if listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CoverageItem}); listed.Page == nil || len(listed.Page.Items) != 0 {
		t.Fatalf("an invalid coverage was listed: %+v", listed)
	}
	validated := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.CoverageItem, Draft: desktop.ItemDraft{Name: "Windows", Coverage: &draft}})
	if validated.State != desktop.Completed || len(validated.Problems) != len(want) {
		t.Fatalf("validating the draft: %+v", validated)
	}
}

// reviewRequest is a relationship review of the project's incident under one
// link rule version.
func linkReviewRequest(context desktop.RequestContext, identity string, rules desktop.ItemRef) desktop.CorrelationReviewRequest {
	return desktop.CorrelationReviewRequest{Context: context, Case: "incident", Identity: identity, LinkRules: &rules}
}

func decide(t *testing.T, app *desktop.App, request desktop.CorrelationReviewRequest, intent string, decision correlate.Decision) desktop.CorrelationReviewResult {
	t.Helper()
	request.IntentID, request.Decision = intent, decision
	return app.DecideCorrelation(request)
}

func linkStatus(view *correlate.ReviewedView, id string) correlate.ReviewStatus {
	for _, link := range view.Links {
		if link.ID == id {
			return link.Status
		}
	}
	return ""
}

// Each decision publishes one new revision of the one review of this case
// under these rules; every earlier revision stays readable, and the timeline
// applies the current one and names it.
func TestADecisionIsOneNewCatalogRevisionAndHistoryIsKept(t *testing.T) {
	app, context, identity, _ := timelineProject(t)
	rules := saveLinkRules(t, app, context, "", "", "rules", rulesOf(t, seqRules))
	request := linkReviewRequest(context, identity, rules)
	opened := app.OpenCorrelationReview(request)
	if opened.State != desktop.Completed || opened.Review != nil || opened.Context != context {
		t.Fatalf("a case never reviewed: %+v", opened)
	}
	link := opened.View.Links[0].ID
	request.Mapping = opened.View.Mapping
	rejected := decide(t, app, request, "reject", correlate.Decision{Action: "reject", Link: link, Actor: "Dana", Reason: "separate booking"})
	if rejected.State != desktop.Completed || rejected.Saved == nil || rejected.Saved.Kind != desktop.LinkReviewItem || rejected.Saved.Revision != "1" ||
		linkStatus(rejected.View, link) != "rejected" {
		t.Fatalf("the first decision: %+v", rejected)
	}
	if again := decide(t, app, request, "reject", correlate.Decision{Action: "reject", Link: link, Actor: "Dana", Reason: "separate booking"}); again.State != desktop.Completed || *again.Saved != *rejected.Saved {
		t.Fatalf("one click decided twice: %+v", again)
	}
	if stale := decide(t, app, request, "stale", correlate.Decision{Action: "accept", Link: link, Actor: "Dana", Reason: "same booking"}); stale.State != desktop.Failed || stale.Saved != nil {
		t.Fatalf("a decision on a review that changed: %+v", stale)
	}
	request.Review, request.Mapping = rejected.Saved, rejected.View.Mapping
	if saved := app.SavePreferences(desktop.Preferences{Theme: desktop.SystemTheme, TextScale: 100, Reviewer: "Casey"}); saved.State != desktop.Completed {
		t.Fatalf("the local Reviewer was not kept: %+v", saved)
	}
	accepted := decide(t, app, request, "accept", correlate.Decision{Action: "accept", Link: link, Reason: "same booking"})
	if accepted.State != desktop.Completed || accepted.Saved.ID != rejected.Saved.ID || accepted.Saved.Revision != "2" {
		t.Fatalf("the second decision: %+v", accepted)
	}
	request.Review, request.Mapping, request.ShowValues, request.Link = nil, "", true, link
	history := app.OpenCorrelationReview(request)
	if history.State != desktop.Completed || *history.Review != *accepted.Saved || len(history.View.History) != 2 || len(history.View.Links) != 1 {
		t.Fatalf("the link's history: %+v", history)
	}
	first, second := history.View.History[0], history.View.History[1]
	if first.Actor != "Dana" || first.Reason != "separate booking" || first.At.IsZero() || second.Actor != "Casey" || second.Action != "accept" {
		t.Fatalf("a decision lost its reviewer, time or reason: %+v", history.View.History)
	}
	request.Review, request.Link = rejected.Saved, ""
	if earlier := app.OpenCorrelationReview(request); earlier.State != desktop.Completed || linkStatus(earlier.View, link) != "rejected" {
		t.Fatalf("an earlier revision is no longer readable: %+v", earlier)
	}
	sequence := laidOut(t, app, desktop.SequenceRequest{Context: context, Case: "incident", Identity: identity, LinkRules: &rules, Limit: desktop.MaxSequenceEvents})
	if sequence.Review == nil || *sequence.Review != *accepted.Saved {
		t.Fatalf("the timeline did not apply the current review: %+v", sequence.Review)
	}
	for _, relation := range sequence.Relations {
		if relation.ID == link && relation.Status != "accepted" {
			t.Fatalf("the reviewed link: %+v", relation)
		}
	}
}

// Undo is a withdrawal: one more revision that restores the status before
// the decision it undoes, with every earlier revision kept.
func TestUndoRecordsAWithdrawalAndNeverRewritesHistory(t *testing.T) {
	app, context, identity, _ := timelineProject(t)
	rules := saveLinkRules(t, app, context, "", "", "rules", rulesOf(t, seqRules))
	request := linkReviewRequest(context, identity, rules)
	opened := app.OpenCorrelationReview(request)
	link := opened.View.Links[0].ID
	request.Mapping = opened.View.Mapping
	if nothing := decide(t, app, request, "early", correlate.Decision{Actor: "Dana", Action: "withdraw", Link: link, Reason: "undo"}); nothing.State != desktop.Failed {
		t.Fatalf("a withdrawal with nothing to withdraw: %+v", nothing)
	}
	rejected := decide(t, app, request, "reject", correlate.Decision{Actor: "Dana", Action: "reject", Link: link, Reason: "separate booking"})
	request.Review, request.Mapping = rejected.Saved, rejected.View.Mapping
	undone := decide(t, app, request, "undo", correlate.Decision{Actor: "Dana", Action: "withdraw", Link: link, Reason: "rejected the wrong link"})
	if undone.State != desktop.Completed || undone.Saved.Revision != "2" || linkStatus(undone.View, link) != "unreviewed" || undone.View.TotalDecisions != 2 {
		t.Fatalf("undo: %+v", undone)
	}
	request.Review, request.Mapping = rejected.Saved, ""
	if kept := app.OpenCorrelationReview(request); kept.State != desktop.Completed || linkStatus(kept.View, link) != "rejected" || kept.View.TotalDecisions != 1 {
		t.Fatalf("undo rewrote the revision it undid: %+v", kept)
	}
}

// A review belongs to one case under one link rule version; under any other
// it is never overlaid, and asking for it is refused.
func TestAReviewForOtherRulesOrCaseIsNeverOverlaid(t *testing.T) {
	app, context, identity, _ := timelineProject(t)
	rules := saveLinkRules(t, app, context, "", "", "rules", rulesOf(t, seqRules))
	request := linkReviewRequest(context, identity, rules)
	opened := app.OpenCorrelationReview(request)
	link := opened.View.Links[0].ID
	request.Mapping = opened.View.Mapping
	review := decide(t, app, request, "reject", correlate.Decision{Actor: "Dana", Action: "reject", Link: link, Reason: "separate booking"}).Saved
	other := saveLinkRules(t, app, context, rules.ID, "1", "narrow", rulesOf(t, strings.Replace(seqRules, `"PID-3.1"`, `"PID-5.1"`, 1)))
	sequence := desktop.SequenceRequest{Context: context, Case: "incident", Identity: identity, LinkRules: &other, Limit: desktop.MaxSequenceEvents}
	unreviewed := laidOut(t, app, sequence)
	if unreviewed.Review != nil {
		t.Fatalf("a review of other rules was overlaid: %+v", unreviewed.Review)
	}
	for _, relation := range unreviewed.Relations {
		if relation.Status == "rejected" {
			t.Fatalf("a decision about other rules was applied: %+v", relation)
		}
	}
	sequence.Review = review
	if got := app.OpenSequence(sequence); got.State != desktop.Failed || got.Sequence != nil {
		t.Fatalf("a review of other rules was applied on request: %+v", got)
	}
	named := linkReviewRequest(context, identity, other)
	named.Review = review
	if got := app.OpenCorrelationReview(named); got.State != desktop.Failed || got.View != nil {
		t.Fatalf("a review of other rules was opened: %+v", got)
	}
	written := writeInputs(t, context.Project, "another", incidentInputs()[:1])
	elsewhere := linkReviewRequest(context, written.Identity, rules)
	elsewhere.Case, elsewhere.Review = "another", review
	if got := app.OpenCorrelationReview(elsewhere); got.State != desktop.Failed || got.View != nil {
		t.Fatalf("a review of another case was opened: %+v", got)
	}
}

// Add link names two real, readable occurrences of the case, once.
func TestAddLinkRefusesNonMembersAndDuplicatePairs(t *testing.T) {
	app, context, identity, _ := timelineProject(t)
	rules := saveLinkRules(t, app, context, "", "", "rules", rulesOf(t, acknowledgementsOnly))
	request := linkReviewRequest(context, identity, rules)
	request.Mapping = app.OpenCorrelationReview(request).View.Mapping
	for name, pair := range map[string][2]string{
		"an occurrence of no case": {"s0001-e000001", "s9999-e000001"},
		"an unparsed occurrence":   {"s0001-e000001", "s0002-e000002"},
		"one occurrence twice":     {"s0001-e000001", "s0001-e000001"},
	} {
		if got := decide(t, app, request, "refused-"+name, correlate.Decision{Actor: "Dana", Action: "add", From: pair[0], To: pair[1], Reason: "checked"}); got.State != desktop.Failed || got.Saved != nil {
			t.Fatalf("%s was added: %+v", name, got)
		}
	}
	added := decide(t, app, request, "add", correlate.Decision{Actor: "Dana", Action: "add", From: "s0001-e000001", To: "s0002-e000001", Reason: "same booking"})
	if added.State != desktop.Completed || added.Saved == nil {
		t.Fatalf("add: %+v", added)
	}
	request.Review, request.Mapping = added.Saved, added.View.Mapping
	for intent, pair := range map[string][2]string{"again": {"s0001-e000001", "s0002-e000001"}, "reversed": {"s0002-e000001", "s0001-e000001"}} {
		if got := decide(t, app, request, intent, correlate.Decision{Actor: "Dana", Action: "add", From: pair[0], To: pair[1], Reason: "same booking"}); got.State != desktop.Failed {
			t.Fatalf("a duplicate pair was added: %+v", got)
		}
	}
	sequence := laidOut(t, app, desktop.SequenceRequest{Context: context, Case: "incident", Identity: identity, LinkRules: &rules, Limit: desktop.MaxSequenceEvents})
	found := false
	for _, relation := range sequence.Relations {
		if relation.Basis == desktop.ReviewedRelation {
			found = relation.Status == "accepted" && relation.Occurrences == 2 && relation.Endpoints[0].Occurrence == "s0001-e000001" &&
				relation.Endpoints[1].Occurrence == "s0002-e000001" && relation.Endpoints[1].InWindow
		}
	}
	if !found {
		t.Fatalf("the added link is not a reviewed relation: %+v", sequence.Relations)
	}
	request.Mapping = ""
	window := desktop.SequenceRequest{Context: context, Case: "incident", Identity: identity, LinkRules: &rules, Limit: desktop.MaxSequenceEvents, Sources: []string{"s0002"}}
	for _, relation := range laidOut(t, app, window).Relations {
		if relation.Basis == desktop.ReviewedRelation && (relation.Endpoints[0].InWindow || !relation.Endpoints[1].InWindow) {
			t.Fatalf("an endpoint outside the window was not marked so: %+v", relation)
		}
	}
}

// A relationship decision is recorded under the reviewer the window names,
// or else the configured local Reviewer. With neither, Review, Add link and
// Undo are each refused with a problem at the reviewer's field, nothing is
// saved, and the account's own name is never put in the reviewer's place.
func TestARelationshipDecisionWithNoReviewerAsksForOneAndNeverUsesTheAccountName(t *testing.T) {
	app, context, identity, _ := timelineProject(t)
	rules := saveLinkRules(t, app, context, "", "", "rules", rulesOf(t, seqRules))
	request := linkReviewRequest(context, identity, rules)
	opened := app.OpenCorrelationReview(request)
	link := opened.View.Links[0].ID
	request.Mapping = opened.View.Mapping
	asked := func(name string, got desktop.CorrelationReviewResult) {
		t.Helper()
		if got.State != desktop.Failed || got.Saved != nil || len(got.Problems) != 1 || got.Problems[0].Field != desktop.ReviewerField {
			t.Fatalf("%s with no reviewer was not asked for one: %+v", name, got)
		}
	}
	asked("Review", decide(t, app, request, "review", correlate.Decision{Action: "reject", Link: link, Reason: "separate booking"}))
	asked("Add link", decide(t, app, request, "add", correlate.Decision{Action: "add", From: "s0001-e000001", To: "s0002-e000001", Reason: "same booking"}))
	asked("An unprintable reviewer", decide(t, app, request, "unprintable", correlate.Decision{Action: "reject", Link: link, Actor: "Dana\x07", Reason: "separate booking"}))
	if unchanged := app.OpenCorrelationReview(request); unchanged.Review != nil {
		t.Fatalf("a refused decision was saved: %+v", unchanged.Review)
	}

	declared := decide(t, app, request, "declared", correlate.Decision{Action: "reject", Link: link, Actor: "Dana", Reason: "separate booking"})
	if declared.State != desktop.Completed || declared.Saved == nil {
		t.Fatalf("a decision naming its reviewer: %+v", declared)
	}
	request.Review, request.Mapping = declared.Saved, declared.View.Mapping
	asked("Undo", decide(t, app, request, "undo", correlate.Decision{Action: "withdraw", Link: link, Reason: "rejected the wrong link"}))

	if saved := app.SavePreferences(desktop.Preferences{Theme: desktop.SystemTheme, TextScale: 100, Reviewer: "Casey"}); saved.State != desktop.Completed {
		t.Fatalf("the local Reviewer was not kept: %+v", saved)
	}
	undone := decide(t, app, request, "undo-configured", correlate.Decision{Action: "withdraw", Link: link, Reason: "rejected the wrong link"})
	if undone.State != desktop.Completed || undone.Saved == nil {
		t.Fatalf("undo under the configured Reviewer: %+v", undone)
	}
	request.Review, request.Mapping, request.ShowValues, request.Link = nil, "", true, link
	history := app.OpenCorrelationReview(request).View.History
	if len(history) != 2 || history[0].Actor != "Dana" || history[1].Actor != "Casey" {
		t.Fatalf("decisions were not recorded under the reviewer named, then the configured one: %+v", history)
	}
}

// A source window keeps the IANA time zone it was declared in: its wall
// times there are saved as the instants they are, with that zone's offset,
// and open again as the same wall times in the same zone.
func TestACoverageWindowKeepsTheTimeZoneItWasDeclaredIn(t *testing.T) {
	app, context, _, incident := timelineProject(t)
	draft := desktop.CoverageDraft{Case: incident, ClockToleranceSeconds: 5,
		Windows: []desktop.CoverageWindow{{Source: "s0001", Start: "2026-01-01T06:00:00", End: "2026-01-01T06:01:00",
			TimeZone: "America/Chicago", Coverage: sequenceanalysis.CompleteCoverage}},
		Retries:  []sequenceanalysis.Retry{{First: "s0001-e000001", Retry: "s0001-e000003", Basis: sequenceanalysis.OperatorReportedRetry}},
		Expected: []sequenceanalysis.Downstream{}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.CoverageItem, IntentID: "zoned",
		Draft: desktop.ItemDraft{Name: "Chicago window", Coverage: &draft}})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("a zoned window: %+v", saved)
	}
	var declaration sequenceanalysis.Declaration
	if err := filepath.WalkDir(context.Project, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(path, "coverage.json") {
			data, _ := os.ReadFile(path)
			declaration, err = sequenceanalysis.Parse(data)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	window := declaration.Windows[0]
	if window.TimeZone != "America/Chicago" || !window.Start.Equal(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)) || window.Start.Format(time.RFC3339) != "2026-01-01T06:00:00-06:00" {
		t.Fatalf("the saved window is not the declared wall time in its zone: %+v", window)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	reopened := opened.Draft.Coverage.Windows[0]
	if reopened.TimeZone != "America/Chicago" || reopened.Start != "2026-01-01T06:00:00" || reopened.End != "2026-01-01T06:01:00" ||
		opened.Draft.Coverage.Retries[0].Basis != sequenceanalysis.OperatorReportedRetry {
		t.Fatalf("the window reopened: %+v", opened.Draft.Coverage)
	}
	// A declaration whose instants disagree with its declared zone is not a
	// declaration the reader accepts.
	disagreeing := `{"schema":"readmit-sequence-analysis/v1","rules_sha256":"","case_identity":"x","clock_tolerance_seconds":0,"retries":[],"downstream":[],` +
		`"windows":[{"source":"s0001","start":"2026-01-01T06:00:00Z","end":"2026-01-01T07:00:00Z","time_zone":"America/Chicago","coverage":"partial"}]}`
	if _, err := sequenceanalysis.Parse([]byte(disagreeing)); err == nil {
		t.Fatal("a window whose offsets are not its zone's was read")
	}
}

// A timeline reads by names: each lane by the name declared for its source
// (empty where nothing names it, so it reads by its ID), each parsed event by
// its message type and trigger, and it names the fields the case's parsed
// occurrences hold a value at, in segment then field order, so a link rule
// picks a field this case has.
func TestATimelineNamesItsLanesEventTypesAndFields(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	opened, err := bundle.Open(filepath.Join(root, "incident"))
	if err != nil {
		t.Fatal(err)
	}
	document := project.Document{Schema: project.SchemaV2, Settings: project.Settings{Title: "Scheduling QA"}, Cases: []project.Case{
		{Name: "incident", Identity: identity, Schema: opened.Manifest.Schema, Provenance: string(opened.Manifest.Provenance.Mode),
			Title: "Incident", Status: project.StatusOpen, Sources: []project.Source{{ID: "s0002", Name: "Receiver"}}},
	}}
	if err := project.WriteDocument(root, document); err != nil {
		t.Fatal(err)
	}
	sequence := laidOut(t, app, sequenceRequest(root, identity, ""))
	names := []string{}
	for _, lane := range sequence.Lanes {
		names = append(names, lane.SourceID+"="+lane.SourceName)
	}
	if !slices.Equal(names, []string{"s0001=", "s0002=Receiver"}) {
		t.Fatalf("lanes read by %v", names)
	}
	booking, acknowledgement, unparsed := eventAt(t, sequence, "s0001-e000001"), eventAt(t, sequence, "s0001-e000002"), eventAt(t, sequence, "s0002-e000002")
	if booking.MessageType != "SIU" || booking.Trigger != "S12" || acknowledgement.MessageType != "ACK" || unparsed.MessageType != "" || unparsed.Trigger != "" {
		t.Fatalf("event types: %+v %+v %+v", booking, acknowledgement, unparsed)
	}
	for _, field := range []string{"MSH-9", "MSH-10", "PID-3", "SCH-1", "MSA-2"} {
		if !slices.Contains(sequence.Fields, field) {
			t.Fatalf("the field %s the case holds is not named: %v", field, sequence.Fields)
		}
	}
	if slices.Index(sequence.Fields, "MSH-9") > slices.Index(sequence.Fields, "MSH-10") || !slices.IsSortedFunc(sequence.Fields, func(x, y string) int {
		return strings.Compare(x[:3], y[:3])
	}) || len(sequence.Fields) > desktop.MaxSequenceFields {
		t.Fatalf("fields are not in segment then field order: %v", sequence.Fields)
	}
}

// A relationship names its basis as a person reads it: the name of the link
// rules a rule's link came from, Recorded or Reviewed — never a rule's ID.
func TestARelationNamesItsBasisByTheLinkRulesNameRecordedOrReviewed(t *testing.T) {
	app, context, identity, _ := timelineProject(t)
	rules := saveLinkRules(t, app, context, "", "", "rules", rulesOf(t, seqRules))
	request := linkReviewRequest(context, identity, rules)
	request.Mapping = app.OpenCorrelationReview(request).View.Mapping
	if added := decide(t, app, request, "add", correlate.Decision{Action: "add", From: "s0001-e000001", To: "s0002-e000001", Actor: "Dana", Reason: "same booking"}); added.State != desktop.Completed {
		t.Fatalf("add: %+v", added)
	}
	sequence := laidOut(t, app, desktop.SequenceRequest{Context: context, Case: "incident", Identity: identity, LinkRules: &rules, Limit: desktop.MaxSequenceEvents})
	named := map[desktop.RelationBasis]string{}
	for _, relation := range sequence.Relations {
		if previous, seen := named[relation.Basis]; seen && previous != relation.BasisName {
			t.Fatalf("relations of one basis read by %q and %q", previous, relation.BasisName)
		}
		named[relation.Basis] = relation.BasisName
	}
	want := map[desktop.RelationBasis]string{desktop.RecordedRelation: "Recorded", desktop.RuleRelation: "Interface links", desktop.ReviewedRelation: "Reviewed"}
	if len(named) != len(want) || named[desktop.RecordedRelation] != want[desktop.RecordedRelation] ||
		named[desktop.RuleRelation] != want[desktop.RuleRelation] || named[desktop.ReviewedRelation] != want[desktop.ReviewedRelation] {
		t.Fatalf("bases read by %v, not %v", named, want)
	}
}
