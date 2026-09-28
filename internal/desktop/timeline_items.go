package desktop

import (
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/sequenceanalysis"
)

// The timeline's named objects: link rules, the coverage declared over one
// case, and the relationship review of one case under one link rule version.
// Each is an object of the project's catalog like any other, read through
// the same strict readers `readmit correlate` and the sequence analysis use,
// and a timeline names the exact revision of each one it applied.

// LinkRulesSummary is how many rules a link rule version declares and the
// kinds of match they make, in declared order without repeats.
type LinkRulesSummary struct {
	Rules     int                  `json:"rules"`
	Operators []correlate.Operator `json:"operators"`
}

// CoverageSummary is the case a coverage declaration is bound to, when the
// project holds it, and the link rule version it is bound to. BindsLinkRules
// says whether it is bound to link rules at all: one that is not applies with
// or without them; one that is applies only with that exact version, which
// LinkRules names when the project still holds it.
type CoverageSummary struct {
	Case           *ItemRef `json:"case"`
	LinkRules      *ItemRef `json:"link_rules"`
	BindsLinkRules bool     `json:"binds_link_rules"`
	Windows        int      `json:"windows"`
	Retries        int      `json:"retries"`
	Expected       int      `json:"expected"`
}

// LinkReviewSummary is how many decisions a relationship review holds.
type LinkReviewSummary struct {
	Decisions int `json:"decisions"`
}

// CoverageDraft is a coverage declaration as its editor holds it. The case
// and the link rule version are named objects; the facade binds the case's
// identity and the rules' digest from them, so no digest is ever typed.
// Windows keep their instants as text, which is how a row the person has not
// finished is still a row with a problem rather than a request that cannot
// be read. Clock tolerance is how many seconds an occurrence's observed and
// declared instants may differ before a clock mismatch is reported; it aligns
// no clock with another.
type CoverageDraft struct {
	Case                  ItemRef                       `json:"case"`
	LinkRules             *ItemRef                      `json:"link_rules,omitzero"`
	ClockToleranceSeconds int                           `json:"clock_tolerance_seconds"`
	Windows               []CoverageWindow              `json:"windows"`
	Retries               []sequenceanalysis.Retry      `json:"retries"`
	Expected              []sequenceanalysis.Downstream `json:"expected"`
}

// CoverageWindow is one source window: the source, the declared start and
// end, the IANA time zone they were declared in, and whether the capture
// through it is declared partial or complete. With a time zone, Start and
// End are wall times in it (2006-01-02T15:04:05), and a saved window opens
// the same way; without one they are RFC 3339 with an explicit offset.
type CoverageWindow struct {
	Source   string                            `json:"source"`
	Start    string                            `json:"start"`
	End      string                            `json:"end"`
	TimeZone string                            `json:"time_zone,omitzero"`
	Coverage sequenceanalysis.DeclaredCoverage `json:"coverage"`
}

// Member roles and files of the timeline's saved objects. The role is the
// kind, which is the role a discovered object's one entry is read under.
const (
	linkRulesFile  = "rules.json"
	coverageFile   = "coverage.json"
	linkReviewFile = "decisions.json"
)

func readRulesFile(path string) (correlate.Rules, error) {
	data, err := boundedFile(path, correlate.MaxRulesBytes)
	if err != nil {
		return correlate.Rules{}, err
	}
	return correlate.ParseRules(data)
}

func readCoverageFile(path string) (sequenceanalysis.Declaration, error) {
	data, err := boundedFile(path, sequenceanalysis.MaxBytes)
	if err != nil {
		return sequenceanalysis.Declaration{}, err
	}
	return sequenceanalysis.Parse(data)
}

func readReviewFile(path string) (correlate.ReviewRevision, error) {
	data, err := boundedFile(path, correlate.MaxReviewBytes)
	if err != nil {
		return correlate.ReviewRevision{}, err
	}
	return correlate.DecodeReview(data)
}

func readLinkRules(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	rules, err := readRulesFile(paths[string(LinkRulesItem)])
	if err != nil {
		return view{}, err
	}
	summary := &LinkRulesSummary{Rules: len(rules.Rules), Operators: []correlate.Operator{}}
	for _, rule := range rules.Rules {
		if !slices.Contains(summary.Operators, rule.Operator) {
			summary.Operators = append(summary.Operators, rule.Operator)
		}
	}
	return view{summary: ItemSummary{LinkRules: summary}}, nil
}

func readCoverage(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	declaration, err := readCoverageFile(paths[string(CoverageItem)])
	if err != nil {
		return view{}, err
	}
	summary := &CoverageSummary{Case: c.caseByIdentity(declaration.CaseIdentity), BindsLinkRules: declaration.RulesSHA256 != "",
		Windows: len(declaration.Windows), Retries: len(declaration.Retries), Expected: len(declaration.Downstream)}
	if summary.BindsLinkRules {
		summary.LinkRules = c.linkRulesByDigest(declaration.RulesSHA256)
	}
	return view{summary: ItemSummary{Coverage: summary}}, nil
}

func readLinkReview(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	review, err := readReviewFile(paths[string(LinkReviewItem)])
	if err != nil {
		return view{}, err
	}
	return view{summary: ItemSummary{LinkReview: &LinkReviewSummary{Decisions: len(review.Decisions)}}}, nil
}

// held is the one object of kind a reference names, and the exact revision
// meant: the reference's own, or the current one when it names none.
func (c *loadedCatalog) held(kind ItemKind, ref ItemRef) (catalog.Item, ItemRef, map[string]string, error) {
	index := c.document.Find(ref.ID)
	if ref.Kind != kind || index < 0 || c.document.Items[index].Kind != string(kind) || c.removed(c.document.Items[index]) {
		return catalog.Item{}, ItemRef{}, nil, errors.New("the project holds no such " + strings.ReplaceAll(string(kind), "-", " "))
	}
	item := c.document.Items[index]
	exact := ItemRef{Kind: kind, ID: item.ID, Revision: ref.Revision}
	if exact.Revision == "" {
		exact.Revision = item.RevisionLabel()
	}
	paths, availability, reason := c.revisionBacking(item, exact.Revision)
	if availability != ItemAvailable {
		return catalog.Item{}, ItemRef{}, nil, errors.New(reason)
	}
	return item, exact, paths, nil
}

// linkRulesAt reads one exact link rule version, with its name.
func (c *loadedCatalog) linkRulesAt(ref ItemRef) (correlate.Rules, ItemRef, string, error) {
	item, exact, paths, err := c.held(LinkRulesItem, ref)
	if err != nil {
		return correlate.Rules{}, ItemRef{}, "", err
	}
	rules, err := readRulesFile(paths[string(LinkRulesItem)])
	if err != nil {
		return correlate.Rules{}, ItemRef{}, "", err
	}
	return rules, exact, item.Name, nil
}

// coverageAt reads one exact coverage version, with its name.
func (c *loadedCatalog) coverageAt(ref ItemRef) (sequenceanalysis.Declaration, ItemRef, string, error) {
	item, exact, paths, err := c.held(CoverageItem, ref)
	if err != nil {
		return sequenceanalysis.Declaration{}, ItemRef{}, "", err
	}
	declaration, err := readCoverageFile(paths[string(CoverageItem)])
	if err != nil {
		return sequenceanalysis.Declaration{}, ItemRef{}, "", err
	}
	return declaration, exact, item.Name, nil
}

// reviewAt reads one exact relationship review version.
func (c *loadedCatalog) reviewAt(ref ItemRef) (correlate.ReviewRevision, ItemRef, error) {
	_, exact, paths, err := c.held(LinkReviewItem, ref)
	if err != nil {
		return correlate.ReviewRevision{}, ItemRef{}, err
	}
	review, err := readReviewFile(paths[string(LinkReviewItem)])
	if err != nil {
		return correlate.ReviewRevision{}, ItemRef{}, err
	}
	return review, exact, nil
}

// boundReview is the relationship review of exactly this machine finding —
// this case under this link rule version — at its current revision, when the
// project holds one. A review of any other case or rules is never it.
func (c *loadedCatalog) boundReview(report correlate.Report) (*correlate.ReviewRevision, *ItemRef) {
	machine := correlate.MachineDigest(report)
	for _, item := range c.document.Items {
		if item.Kind != string(LinkReviewItem) || c.removed(item) || item.Current() == nil {
			continue
		}
		review, exact, err := c.reviewAt(ItemRef{Kind: LinkReviewItem, ID: item.ID})
		if err == nil && review.Machine == machine {
			return &review, &exact
		}
	}
	return nil, nil
}

// linkRulesByDigest is the link rule version whose rules digest is digest,
// when the project holds one.
func (c *loadedCatalog) linkRulesByDigest(digest string) *ItemRef {
	for _, item := range c.document.Items {
		if item.Kind != string(LinkRulesItem) || c.removed(item) {
			continue
		}
		labels := []string{""}
		if len(item.Revisions) > 0 {
			labels = labels[:0]
			for i := len(item.Revisions) - 1; i >= 0; i-- {
				labels = append(labels, strconv.Itoa(item.Revisions[i].Number))
			}
		}
		for _, label := range labels {
			rules, exact, _, err := c.linkRulesAt(ItemRef{Kind: LinkRulesItem, ID: item.ID, Revision: label})
			if err != nil {
				continue
			}
			if held, err := correlate.RulesDigest(rules); err == nil && held == digest {
				return &exact
			}
		}
	}
	return nil
}

// validateLinkRulesDraft validates a whole link rule draft. A rule the
// editor added has no identity yet and is given the next free one; every
// identity a rule already has — one a person imported with it included — is
// kept, and so is every authority mapping, shown or not.
func validateLinkRulesDraft(draft ItemDraft) ([]catalog.Staged, *correlate.Rules, []FieldProblem) {
	problems := []FieldProblem{}
	if strings.TrimSpace(draft.Name) == "" {
		problems = append(problems, FieldProblem{Field: "name", Problem: "link rules are saved under a name"})
	}
	if draft.LinkRules == nil {
		return nil, nil, append(problems, FieldProblem{Field: "link_rules", Problem: "link rules declare at least one rule"})
	}
	rules := *draft.LinkRules
	if rules.Schema == "" {
		rules.Schema = correlate.RulesSchema
	}
	rules.Rules = slices.Clone(rules.Rules)
	used := map[string]bool{}
	for _, rule := range rules.Rules {
		used[rule.ID] = true
	}
	next := 1
	for i := range rules.Rules {
		if rules.Rules[i].ID != "" {
			continue
		}
		for used["rule-"+strconv.Itoa(next)] {
			next++
		}
		rules.Rules[i].ID = "rule-" + strconv.Itoa(next)
		used[rules.Rules[i].ID] = true
	}
	if len(rules.Rules) == 0 {
		problems = append(problems, FieldProblem{Field: "link_rules.rules", Problem: "link rules declare at least one rule"})
	}
	seen := map[string]bool{}
	for i, rule := range rules.Rules {
		field := "link_rules.rules[" + strconv.Itoa(i) + "]"
		if seen[rule.ID] {
			problems = append(problems, FieldProblem{Field: field + ".id", Problem: "another rule already has this identity"})
			continue
		}
		seen[rule.ID] = true
		single, _ := json.Marshal(correlate.Rules{Schema: correlate.RulesSchema, Rules: []correlate.Rule{rule}}, json.Deterministic(true))
		if _, err := correlate.ParseRules(single); err != nil {
			problems = append(problems, FieldProblem{Field: field, Problem: err.Error()})
		}
	}
	if len(problems) > 0 {
		return nil, nil, problems
	}
	data, err := json.Marshal(rules, json.Deterministic(true))
	if err != nil {
		return nil, nil, []FieldProblem{{Field: "link_rules", Problem: "the link rules cannot be encoded"}}
	}
	data = append(data, '\n')
	if _, err := correlate.ParseRules(data); err != nil {
		field := "link_rules.authorities"
		if len(rules.Rules) > correlate.MaxRules {
			field = "link_rules.rules"
		}
		return nil, nil, []FieldProblem{{Field: field, Problem: err.Error()}}
	}
	return []catalog.Staged{{Role: string(LinkRulesItem), File: linkRulesFile, Data: data}}, &rules, nil
}

// coverageCase opens the case a coverage draft names, verified.
func coverageCase(loaded *loadedCatalog, ref ItemRef) (*bundle.Bundle, string, error) {
	index := loaded.document.Find(ref.ID)
	if ref.ID == "" || index < 0 || loaded.removed(loaded.document.Items[index]) {
		return nil, "", errors.New("coverage is declared over one case of this project")
	}
	item := loaded.document.Items[index]
	if (item.Kind != string(CaseItem) && item.Kind != string(VariantItem)) || item.Entry == "" {
		return nil, "", errors.New("coverage is declared over one case of this project")
	}
	path, err := artifactpath.Child(loaded.root, item.Entry)
	if err != nil {
		return nil, "", errors.New("coverage is declared over one case of this project")
	}
	opened, err := operation.OpenCase(path)
	if err != nil {
		return nil, "", err
	}
	return opened, path, nil
}

// validateCoverageDraft validates a whole coverage draft against the case it
// names and the link rule version it is bound to, and reports every row that
// cannot be saved at that row. The staged declaration binds the case's
// identity and the rules' digest; nothing about either is typed.
func validateCoverageDraft(scope draftScope, draft ItemDraft) ([]catalog.Staged, *CoverageDraft, []FieldProblem) {
	problems := []FieldProblem{}
	add := func(field, problem string) { problems = append(problems, FieldProblem{Field: field, Problem: problem}) }
	if strings.TrimSpace(draft.Name) == "" {
		add("name", "coverage is saved under a name")
	}
	if draft.Coverage == nil {
		add("coverage", "coverage is declared over one case")
		return nil, nil, problems
	}
	if scope.loaded == nil {
		add("coverage", "coverage is declared inside a project")
		return nil, nil, problems
	}
	coverage := *draft.Coverage
	declaration := sequenceanalysis.Declaration{Schema: sequenceanalysis.Schema, ClockToleranceSeconds: coverage.ClockToleranceSeconds,
		Windows: []sequenceanalysis.Window{}, Retries: []sequenceanalysis.Retry{}, Downstream: []sequenceanalysis.Downstream{}}
	opened, casePath, err := coverageCase(scope.loaded, coverage.Case)
	if err != nil {
		add("coverage.case", err.Error())
	} else {
		declaration.CaseIdentity = opened.Identity
	}
	var report *correlate.Report
	var rules *correlate.Rules
	if coverage.LinkRules != nil {
		held, exact, _, err := scope.loaded.linkRulesAt(*coverage.LinkRules)
		if err != nil {
			add("coverage.link_rules", err.Error())
		} else {
			rules = &held
			bound := exact
			coverage.LinkRules = &bound
			declaration.RulesSHA256, _ = correlate.RulesDigest(held)
		}
	}
	if coverage.ClockToleranceSeconds < 0 || coverage.ClockToleranceSeconds > sequenceanalysis.MaxClockToleranceSeconds {
		add("coverage.clock_tolerance_seconds", "clock tolerance is between 0 and "+strconv.Itoa(sequenceanalysis.MaxClockToleranceSeconds)+" seconds")
	}
	sources := map[string]bool{}
	events := map[string]bundle.Event{}
	if opened != nil {
		for _, source := range opened.Manifest.Sources {
			sources[source.ID] = true
		}
		for _, event := range opened.Events {
			events[event.ID] = event
		}
	}
	if len(coverage.Windows) == 0 || len(coverage.Windows) > 128 {
		add("coverage.windows", "coverage declares between 1 and 128 source windows")
	}
	windowed := map[string]bool{}
	for i, window := range coverage.Windows {
		field := "coverage.windows[" + strconv.Itoa(i) + "]"
		var location *time.Location
		var zoneErr error
		if window.TimeZone != "" {
			if location, zoneErr = sequenceanalysis.Zone(window.TimeZone); zoneErr != nil {
				add(field+".time_zone", zoneErr.Error())
			}
		}
		start, startErr := coverageInstant(window.Start, location)
		end, endErr := coverageInstant(window.End, location)
		if zoneErr != nil {
			// Instants are read in their zone; with no zone to read them in,
			// the zone is the one problem of the row's times.
			startErr, endErr = nil, nil
		}
		switch {
		case opened != nil && !sources[window.Source]:
			add(field+".source", "a window is declared over one source of the case")
		case windowed[window.Source]:
			add(field+".source", "another window is declared over this source")
		}
		windowed[window.Source] = true
		if startErr != nil {
			add(field+".start", startErr.Error())
		}
		if endErr != nil {
			add(field+".end", endErr.Error())
		} else if startErr == nil && zoneErr == nil && !start.Before(end) {
			add(field+".end", "a window ends after it starts")
		}
		if !slices.Contains(sequenceanalysis.DeclaredCoverages(), window.Coverage) {
			add(field+".coverage", "declared coverage is one the coverage reader accepts")
		}
		declaration.Windows = append(declaration.Windows, sequenceanalysis.Window{Source: window.Source, Start: start, End: end,
			TimeZone: window.TimeZone, Coverage: window.Coverage})
	}
	if len(coverage.Retries) > 128 {
		add("coverage.retries", "coverage declares at most 128 retries")
	}
	retried := map[string]bool{}
	for i, retry := range coverage.Retries {
		field := "coverage.retries[" + strconv.Itoa(i) + "]"
		if _, known := events[retry.First]; opened != nil && !known {
			add(field+".first", "the original is one message of the case")
		}
		if _, known := events[retry.Retry]; opened != nil && !known {
			add(field+".retry", "the retry is one message of the case")
		} else if retry.Retry == retry.First || retried[retry.Retry] {
			add(field+".retry", "the retry is another message, declared a retry once")
		}
		retried[retry.Retry] = true
		if !slices.Contains(sequenceanalysis.RetryBases(), retry.Basis) {
			add(field+".basis", "a retry is declared on a basis the coverage reader accepts")
		}
		declaration.Retries = append(declaration.Retries, retry)
	}
	if len(coverage.Expected) > 128 {
		add("coverage.expected", "coverage declares at most 128 expected outputs")
	}
	for i, expected := range coverage.Expected {
		field := "coverage.expected[" + strconv.Itoa(i) + "]"
		upstream, known := events[expected.Occurrence]
		if opened != nil && (!known || upstream.Kind != bundle.Message) {
			add(field+".occurrence", "the upstream message is one message of the case")
		}
		switch {
		case opened != nil && !sources[expected.Source]:
			add(field+".source", "the downstream source is one source of the case")
		case known && upstream.SourceID == expected.Source:
			add(field+".source", "the downstream source is another source than the upstream message's")
		case !windowed[expected.Source]:
			add(field+".source", "the downstream source needs a source window")
		}
		if rules == nil {
			add(field+".rule", "an expected output is linked by a rule of the chosen link rules")
		} else if !slices.ContainsFunc(rules.Rules, func(rule correlate.Rule) bool { return rule.ID == expected.Rule }) {
			add(field+".rule", "the link rule is one rule of the chosen link rules")
		}
		declaration.Downstream = append(declaration.Downstream, expected)
	}
	if len(problems) > 0 {
		return nil, nil, problems
	}
	data, err := json.Marshal(declaration, json.Deterministic(true))
	if err != nil {
		return nil, nil, []FieldProblem{{Field: "coverage", Problem: "the coverage cannot be encoded"}}
	}
	data = append(data, '\n')
	parsed, err := sequenceanalysis.Parse(data)
	if err == nil && rules != nil {
		var produced correlate.Report
		if produced, err = correlate.Run(casePath, *rules); err == nil {
			report = &produced
		}
	}
	// The declaration is evaluated over its case once, exactly as a timeline
	// will apply it, so a declaration no timeline can apply is never saved.
	if err == nil {
		_, err = sequenceanalysis.Evaluate(opened, parsed, report)
	}
	if err != nil {
		return nil, nil, []FieldProblem{{Field: "coverage", Problem: err.Error()}}
	}
	return []catalog.Staged{{Role: string(CoverageItem), File: coverageFile, Data: data}}, &coverage, nil
}

// wallTime is how a window instant declared in a time zone is written: the
// wall time there, whose offset the zone decides.
const wallTime = "2006-01-02T15:04:05"

// coverageInstant reads one window instant. Without a time zone it is RFC
// 3339 with an explicit, known offset: a time zone is never inferred for a
// window. In a time zone it is a wall time there, or RFC 3339 with the offset
// the zone has at it; a wall time the zone skips or repeats is refused rather
// than moved or guessed.
func coverageInstant(text string, location *time.Location) (time.Time, error) {
	if location == nil {
		at, err := time.Parse(time.RFC3339, text)
		if err != nil || strings.HasSuffix(text, "-00:00") || at.IsZero() {
			return time.Time{}, errors.New("a window instant is a date and time with its UTC offset")
		}
		return at, nil
	}
	if at, err := time.Parse(time.RFC3339, text); err == nil && !strings.HasSuffix(text, "-00:00") {
		if !sequenceanalysis.InZone(at, location) {
			return time.Time{}, errors.New("this offset is not the time zone's offset at this instant")
		}
		return at, nil
	}
	wall, err := time.Parse(wallTime, text)
	if err != nil || wall.IsZero() {
		return time.Time{}, errors.New("a window instant is a date and time")
	}
	// A wall time is the instant wall - offset for whichever offset the zone
	// has there; each offset the zone uses around it is tried, and exactly
	// one must give back this wall time.
	found := map[int64]time.Time{}
	for _, probe := range []time.Time{wall.Add(-48 * time.Hour), wall, wall.Add(48 * time.Hour)} {
		_, offset := probe.In(location).Zone()
		candidate := wall.Add(-time.Duration(offset) * time.Second).In(location)
		if candidate.Format(wallTime) == text {
			found[candidate.Unix()] = candidate
		}
	}
	switch len(found) {
	case 0:
		return time.Time{}, errors.New("this time does not occur in the time zone on that day")
	case 1:
		for _, at := range found {
			return at, nil
		}
	}
	return time.Time{}, errors.New("this time occurs twice in the time zone on that day; give it with its UTC offset")
}

// coverageDraftOf is the draft a saved or discovered coverage declaration
// opens as: its case and link rule version as the project's objects, when
// the project still holds them.
func (c *loadedCatalog) coverageDraftOf(declaration sequenceanalysis.Declaration) *CoverageDraft {
	draft := &CoverageDraft{ClockToleranceSeconds: declaration.ClockToleranceSeconds, Windows: []CoverageWindow{},
		Retries: slices.Clone(declaration.Retries), Expected: slices.Clone(declaration.Downstream)}
	if ref := c.caseByIdentity(declaration.CaseIdentity); ref != nil {
		draft.Case = *ref
	}
	if declaration.RulesSHA256 != "" {
		draft.LinkRules = c.linkRulesByDigest(declaration.RulesSHA256)
	}
	for _, window := range declaration.Windows {
		opened := CoverageWindow{Source: window.Source, Start: window.Start.Format(time.RFC3339),
			End: window.End.Format(time.RFC3339), TimeZone: window.TimeZone, Coverage: window.Coverage}
		if location, err := sequenceanalysis.Zone(window.TimeZone); window.TimeZone != "" && err == nil {
			opened.Start, opened.End = window.Start.In(location).Format(wallTime), window.End.In(location).Format(wallTime)
		}
		draft.Windows = append(draft.Windows, opened)
	}
	if draft.Retries == nil {
		draft.Retries = []sequenceanalysis.Retry{}
	}
	if draft.Expected == nil {
		draft.Expected = []sequenceanalysis.Downstream{}
	}
	return draft
}

// timelineDraft answers the draft of a link rule or coverage object: a new
// one's empty draft, or the members its revision declares.
func (c *loadedCatalog) timelineDraft(kind ItemKind, record catalog.Item) (*ItemDraft, error) {
	paths, availability, reason := c.backing(record)
	if availability != ItemAvailable {
		return nil, errors.New(reason)
	}
	draft := &ItemDraft{Name: record.Name}
	if kind == LinkRulesItem {
		rules, err := readRulesFile(paths[string(LinkRulesItem)])
		if err != nil {
			return nil, err
		}
		draft.LinkRules = &rules
		return draft, nil
	}
	declaration, err := readCoverageFile(paths[string(CoverageItem)])
	if err != nil {
		return nil, err
	}
	draft.Coverage = c.coverageDraftOf(declaration)
	return draft, nil
}

// newTimelineDraft is a new link rule or coverage object's starting draft:
// nothing declared yet, and nothing defaulted that a person did not choose.
func newTimelineDraft(kind ItemKind) *ItemDraft {
	if kind == LinkRulesItem {
		return &ItemDraft{LinkRules: &correlate.Rules{Schema: correlate.RulesSchema, Rules: []correlate.Rule{}}}
	}
	return &ItemDraft{Coverage: &CoverageDraft{Windows: []CoverageWindow{}, Retries: []sequenceanalysis.Retry{}, Expected: []sequenceanalysis.Downstream{}}}
}
