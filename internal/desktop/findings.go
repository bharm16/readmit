package desktop

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/dictionary"
	"github.com/bharm16/readmit/internal/findingreview"
	"github.com/bharm16/readmit/internal/hl7"
)

// The Findings view reads a case's analyses as catalog objects. An analysis
// is the readmit-diagnosis/v1 report directory `readmit diagnose` writes,
// retained as a new entry of the project and discovered there like any
// other; a person's review of it is a finding-review object whose every save
// is a new revision of the readmit-finding-decisions/v1 document `readmit
// diagnose review` reads. Nothing here rewrites an engine finding.

// analysisOperation names an analysis and a comparison of several cases while
// either holds the slot, so the Findings view's Stop stops exactly it.
const analysisOperation = "analysis"

// analysisOutput and groupingOutput name the new project entries an analysis
// and a saved comparison are written into.
var (
	analysisOutput = outputRule{prefix: "analysis",
		invalid:   "an analysis is written to one new entry of the project",
		exhausted: "the project holds more generated analyses than this release proposes",
		taken:     "an analysis is written to a new entry of the project; that one exists",
	}
	groupingOutput = outputRule{prefix: "grouping",
		invalid:   "a comparison is written to one new entry of the project",
		exhausted: "the project holds more generated comparisons than this release proposes",
		taken:     "a comparison is written to a new entry of the project; that one exists",
	}
)

// FindingsRequest opens the findings of one case. Identity is the verified
// identity the window displayed for the case. Analysis names one retained
// analysis of it, from History; left empty, the case's current analysis is
// read. Offset begins the window of findings. Severities, when not empty,
// lists only the findings whose rule declares one of them; a rule with no
// declared severity matches none.
type FindingsRequest struct {
	Context    RequestContext      `json:"context"`
	Case       ItemRef             `json:"case"`
	Identity   string              `json:"identity"`
	Analysis   *ItemRef            `json:"analysis,omitzero"`
	Offset     int                 `json:"offset"`
	Severities []diagnose.Severity `json:"severities,omitzero"`
}

// FindingRow is one finding as the Findings view lists it: the engine's
// finding unchanged, the severity its rule declares, and the bundled label
// of each evidence field, by field path, where the labels apply to every
// message that field is evidenced in.
type FindingRow struct {
	diagnose.Finding
	Severity diagnose.Severity `json:"severity,omitzero"`
	Labels   map[string]string `json:"labels"`
}

// FindingsAnalysis is one retained analysis as the Findings view shows it:
// the object, when it was made, the profile it ran under and the identity of
// the exact report, whether it is the case's current analysis or history, the
// person's review of it when there is one, and the report. Its findings are
// listed in Findings, not in Diagnosis: the findings the request's severities
// match (Matching of them), the error first, then the warning, then
// information and then a rule with no declared severity, each in the
// engine's order, windowed from the request's offset. Diagnosis.Total stays
// the engine's count of every finding.
type FindingsAnalysis struct {
	Ref          ItemRef  `json:"ref"`
	CreatedAt    *string  `json:"created_at"`
	ProfileName  string   `json:"profile_name"`
	ReportSHA256 string   `json:"report_sha256"`
	ConfigSHA256 string   `json:"config_sha256"`
	Current      bool     `json:"current"`
	Review       *ItemRef `json:"review,omitzero"`
	// Diagnosis is the report's summary: its total, ruleset and unsupported
	// evidence. Its own findings list is always empty; the window of rows is
	// Findings.
	Diagnosis Diagnosis    `json:"diagnosis"`
	Matching  int          `json:"matching"`
	Findings  []FindingRow `json:"findings"`
}

// FindingsResult answers one case's findings. Empty with no Analysis is a
// case with no current analysis (Not analyzed); Empty with an Analysis is an
// analysis that made no findings. Rules names every rule of every ruleset.
type FindingsResult struct {
	State    State               `json:"state"`
	Reason   string              `json:"reason,omitzero"`
	Context  RequestContext      `json:"context"`
	Analysis *FindingsAnalysis   `json:"analysis,omitzero"`
	Rules    []diagnose.RuleInfo `json:"rules"`
}

func (r *FindingsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// OpenCaseFindings reads the current analysis of one case, or the retained
// analysis the request names. The current analysis is the newest analysis
// of exactly this verified evidence, provided the configuration it ran under
// is still offered — a built-in selection or the current revision of saved
// analysis settings; after the settings change it is history, and the case
// reads as not analyzed until it is analyzed again. It runs nothing and
// writes nothing.
func (a *App) OpenCaseFindings(request FindingsRequest) FindingsResult {
	return run(a, false, false, func(ctx context.Context) FindingsResult {
		return a.openCaseFindings(ctx, request)
	})
}

func (a *App) openCaseFindings(ctx context.Context, request FindingsRequest) FindingsResult {
	result := FindingsResult{Context: request.Context, Rules: diagnose.Rules()}
	if request.Offset < 0 {
		result.refuse(Failed, "a findings window cannot begin before its first finding")
		return result
	}
	loaded, entry, declined := a.caseEntry(ctx, request.Context, request.Case, false)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	_, source, declined := openedCase(loaded.root, entry, request.Identity)
	if declined.reason != "" {
		result.refuse(declined.state, declined.reason)
		return result
	}
	current := loaded.currentAnalysis(request.Identity)
	chosen := current
	if request.Analysis != nil {
		index := loaded.document.Find(request.Analysis.ID)
		if index < 0 || loaded.document.Items[index].Kind != string(AnalysisItem) || loaded.removed(loaded.document.Items[index]) {
			result.refuse(Failed, "the project holds no such analysis")
			return result
		}
		item := loaded.read(loaded.document.Items[index])
		if item.Availability != ItemAvailable {
			result.refuse(Failed, "this analysis is "+string(item.Availability)+": "+item.Reason)
			return result
		}
		if item.Summary.Analysis == nil || item.Summary.Analysis.Form != "diagnosis" || item.Summary.Analysis.CaseIdentity != request.Identity {
			result.refuse(Failed, "this analysis was not made over this case's evidence")
			return result
		}
		chosen = &item
	}
	if chosen == nil {
		result.State, result.Reason = Empty, "this case has no current analysis"
		return result
	}
	retained, err := loaded.retainedAnalysis(loaded.entryOfID(chosen.Ref.ID), "")
	if err != nil {
		result.refuse(Failed, err.Error())
		return result
	}
	report := retained.Report
	windowed := windowedDiagnosis(entry, "", retained.Identity, report, 0)
	rows := findingRows(report.Findings, request.Severities)
	start := min(request.Offset, len(rows))
	window := rows[start : start+min(MaxDiagnosisFindings, len(rows)-start)]
	labelled := evidenceLabeller(source)
	for i := range window {
		window[i].Labels = labelled(window[i].Finding)
	}
	result.State, result.Reason = windowed.State, windowed.Reason
	switch {
	case len(report.Findings) == 0:
	case len(rows) == 0:
		result.State, result.Reason = Empty, "no finding of this analysis has the chosen severity"
	case len(window) == 0:
		result.State, result.Reason = Empty, "this window begins past the last finding of this diagnosis"
	}
	diagnosis := *windowed.Diagnosis
	diagnosis.Offset, diagnosis.Findings = request.Offset, []diagnose.Finding{}
	result.Analysis = &FindingsAnalysis{Ref: chosen.Ref, CreatedAt: chosen.CreatedAt,
		ProfileName:  cmp.Or(loaded.configName(report.ConfigSHA256), profileName(report.Profile)),
		ReportSHA256: retained.Identity, ConfigSHA256: report.ConfigSHA256, Current: current != nil && current.Ref.ID == chosen.Ref.ID,
		Review: loaded.reviewFor(retained.Identity), Diagnosis: diagnosis, Matching: len(rows), Findings: window}
	return result
}

// findingRows are the findings of one report whose rule declares one of the
// chosen severities, or every finding when none is chosen: the most severe
// first, and otherwise in the engine's order. The engine's order is total, so
// no two rows compare equal.
func findingRows(findings []diagnose.Finding, severities []diagnose.Severity) []FindingRow {
	rows := []FindingRow{}
	for _, finding := range findings {
		severity := diagnose.RuleSeverity(finding.RuleID)
		if len(severities) == 0 || severity != "" && slices.Contains(severities, severity) {
			rows = append(rows, FindingRow{Finding: finding, Severity: severity, Labels: map[string]string{}})
		}
	}
	slices.SortStableFunc(rows, func(x, y FindingRow) int { return cmp.Compare(y.Severity.Rank(), x.Severity.Rank()) })
	return rows
}

// evidenceLabeller names the evidence fields of a finding over one opened
// case with the bundled field labels, exactly as the inspector names a
// field: by its segment and field position, and only where the labels apply
// to the message it is evidenced in. A field evidenced in any message the
// labels do not apply to is left unnamed rather than named by another
// version's label. Each occurrence is read once.
func evidenceLabeller(source *bundle.Bundle) func(diagnose.Finding) map[string]string {
	events := map[string]bundle.Event{}
	for _, event := range source.Events {
		events[event.ID] = event
	}
	applies := map[string]*dictionary.Dictionary{}
	labelsOf := func(occurrence string) *dictionary.Dictionary {
		if held, ok := applies[occurrence]; ok {
			return held
		}
		var labels *dictionary.Dictionary
		if event, ok := events[occurrence]; ok {
			if document, err := source.Document(event); err == nil {
				labels = labelsFor(document, 0)
			}
		}
		applies[occurrence] = labels
		return labels
	}
	return func(finding diagnose.Finding) map[string]string {
		named, unnamed := map[string]string{}, map[string]bool{}
		for _, evidence := range finding.Evidence {
			selector, err := hl7.ParseSelector(evidence.Field)
			if err != nil || unnamed[evidence.Field] {
				continue
			}
			labels := labelsOf(evidence.Occurrence)
			parts := selector.Parts()
			label := ""
			if labels != nil {
				label = labels.Label(parts.Segment, parts.Field)
			}
			if label == "" {
				unnamed[evidence.Field] = true
				delete(named, evidence.Field)
				continue
			}
			named[evidence.Field] = label
		}
		return named
	}
}

// entryOfID is the project entry the object with id is discovered at.
func (c *loadedCatalog) entryOfID(id string) string {
	if index := c.document.Find(id); index >= 0 {
		return c.document.Items[index].Entry
	}
	return ""
}

// caseEntry loads the project and resolves one case object to the project
// entry that holds its evidence. The object must be available now and, when
// the reference names a revision, still at it.
func (a *App) caseEntry(ctx context.Context, request RequestContext, ref ItemRef, record bool) (*loadedCatalog, string, refusal) {
	if ref.Kind != CaseItem && ref.Kind != VariantItem {
		return nil, "", refusal{Failed, "findings are read for one case"}
	}
	loaded, declined := a.loadCatalog(ctx, request, record)
	if loaded == nil {
		return nil, "", declined
	}
	index := loaded.document.Find(ref.ID)
	if index < 0 || loaded.document.Items[index].Kind != string(ref.Kind) || loaded.removed(loaded.document.Items[index]) {
		return nil, "", refusal{Failed, "the project holds no such case"}
	}
	item := loaded.read(loaded.document.Items[index])
	if item.Availability != ItemAvailable {
		return nil, "", refusal{Failed, "this case is " + string(item.Availability) + ": " + item.Reason}
	}
	if ref.Revision != "" && ref.Revision != item.Ref.Revision {
		return nil, "", refusal{Failed, "the case changed since it was shown; look at it again"}
	}
	return loaded, loaded.document.Items[index].Entry, refusal{}
}

// retainedAnalysis reopens the diagnosis one analysis entry holds, once per
// load, through the strict reader a review uses.
func (c *loadedCatalog) retainedAnalysis(entry, path string) (diagnose.Retained, error) {
	if held, ok := c.analyses[entry]; ok {
		return held, nil
	}
	if path == "" {
		if artifactpath.EntryName(entry) != nil {
			return diagnose.Retained{}, errors.New("an analysis is one entry of the project")
		}
		path = filepath.Join(c.root, entry)
	}
	retained, err := diagnose.OpenReport(path, diagnose.Reading{})
	if err != nil {
		return diagnose.Retained{}, err
	}
	c.analyses[entry] = retained
	return retained, nil
}

// analysesOf lists the available diagnoses of exactly one verified evidence,
// newest first: by the date the application recorded, an unknown date after
// every known one, and then by the later entry.
func (c *loadedCatalog) analysesOf(identity string) []CatalogItem {
	var found []CatalogItem
	for _, record := range c.document.Items {
		if record.Kind != string(AnalysisItem) || record.Entry == "" || c.removed(record) {
			continue
		}
		item := c.read(record)
		if item.Availability == ItemAvailable && item.Summary.Analysis != nil && item.Summary.Analysis.Form == "diagnosis" &&
			item.Summary.Analysis.CaseIdentity == identity {
			found = append(found, item)
		}
	}
	date := func(value *string) string {
		if value == nil {
			return ""
		}
		return *value
	}
	slices.SortStableFunc(found, func(x, y CatalogItem) int {
		if order := cmp.Compare(date(y.CreatedAt), date(x.CreatedAt)); order != 0 {
			return order
		}
		return cmp.Compare(c.entryOfID(y.Ref.ID), c.entryOfID(x.Ref.ID))
	})
	return found
}

// currentAnalysis is the newest analysis of the evidence, when the
// configuration it ran under is still offered.
func (c *loadedCatalog) currentAnalysis(identity string) *CatalogItem {
	found := c.analysesOf(identity)
	if len(found) == 0 || !c.offeredConfigs()[found[0].Summary.Analysis.ConfigSHA256] {
		return nil
	}
	return &found[0]
}

// AnalyzeRequest analyzes one case under one chosen profile. Identity is the
// verified identity the window displayed; IntentID is allocated once when
// the person presses Analyze and reused for every retry of that press.
type AnalyzeRequest struct {
	Context  RequestContext     `json:"context"`
	Case     ItemRef            `json:"case"`
	Identity string             `json:"identity"`
	Profile  AnalysisProfileRef `json:"profile"`
	IntentID string             `json:"intent_id"`
}

// analysisIntents remembers, for this process, the analysis each Analyze
// press made, so a retry of the same press answers it rather than analyzing
// again.
type analysisIntents struct {
	mu   sync.Mutex
	made map[string]analysisIntent
}

type analysisIntent struct {
	digest   string
	analysis ItemRef
}

// AnalyzeCase runs one diagnosis of the case under the chosen profile,
// writes report.json and report.md into a new entry of the project exactly
// as `readmit diagnose` writes them, and records when the analysis was made.
// A profile the engine's preflight refuses for this case runs nothing. It is
// interruptible: stopped before its report is written, it writes nothing and
// answers cancelled. A press already answered is answered again.
func (a *App) AnalyzeCase(request AnalyzeRequest) FindingsResult {
	return runNamed[FindingsResult, *FindingsResult](a, profiles["AnalyzeCase"], func(ctx context.Context) FindingsResult {
		return a.analyzeCase(ctx, request)
	})
}

func (a *App) analyzeCase(ctx context.Context, request AnalyzeRequest) FindingsResult {
	result := FindingsResult{Context: request.Context, Rules: diagnose.Rules()}
	if !catalog.ValidToken(request.IntentID) {
		result.refuse(Failed, "an analysis names the press that asked for it")
		return result
	}
	digest := binding(request.Context.Project, request.Context.ProjectID, request.Case.ID, request.Identity,
		request.Profile.Builtin, refID(request.Profile.Settings), refRevision(request.Profile.Settings))
	a.analyses.mu.Lock()
	made, held := a.analyses.made[request.IntentID]
	a.analyses.mu.Unlock()
	if held {
		if made.digest != digest {
			result.refuse(Failed, "this press already asked for a different analysis; nothing was analyzed")
			return result
		}
		return a.openCaseFindings(ctx, FindingsRequest{Context: request.Context, Case: request.Case, Identity: request.Identity, Analysis: &made.analysis})
	}
	if ctx.Err() != nil {
		result.refuse(cancelledRefusal.state, cancelledRefusal.reason)
		return result
	}
	loaded, entry, declined := a.caseEntry(ctx, request.Context, request.Case, true)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	if _, _, declined := openedCase(loaded.root, entry, request.Identity); declined.reason != "" {
		result.refuse(declined.state, declined.reason)
		return result
	}
	config, declined := loaded.analysisProfile(request.Profile)
	if declined.reason != "" {
		result.refuse(declined.state, declined.reason)
		return result
	}
	path := filepath.Join(loaded.root, entry)
	checked, err := diagnose.Check(path, config)
	if err != nil {
		result.refuse(Failed, err.Error())
		return result
	}
	if at := slices.IndexFunc(checked, diagnose.Refuses); at >= 0 {
		result.refuse(Failed, "the engine does not evaluate this profile over this case: "+checked[at].Detail)
		return result
	}
	report, err := diagnose.RunContext(ctx, path, config)
	if ctx.Err() != nil {
		result.refuse(cancelledRefusal.state, cancelledRefusal.reason)
		return result
	}
	if err != nil {
		result.refuse(Failed, err.Error())
		return result
	}
	if report.CaseIdentity != request.Identity {
		result.refuse(Failed, "the case changed while it was analyzed; nothing was recorded")
		return result
	}
	destination, declined := analysisOutput.destination(loaded.root, "")
	if declined.reason != "" {
		result.refuse(declined.state, declined.reason)
		return result
	}
	if ctx.Err() != nil {
		result.refuse(cancelledRefusal.state, cancelledRefusal.reason)
		return result
	}
	if _, err := diagnose.WriteReport(filepath.Join(loaded.root, destination.Name), report); err != nil {
		result.refuse(Failed, err.Error())
		return result
	}
	ref, err := a.recordMade(loaded, destination.Name, "")
	if err != nil {
		result.refuse(Failed, "the analysis was written to the project but its date could not be recorded: "+err.Error())
		return result
	}
	a.analyses.mu.Lock()
	if a.analyses.made == nil {
		a.analyses.made = map[string]analysisIntent{}
	}
	a.analyses.made[request.IntentID] = analysisIntent{digest: digest, analysis: ref}
	a.analyses.mu.Unlock()
	return a.openCaseFindings(ctx, FindingsRequest{Context: request.Context, Case: request.Case, Identity: request.Identity, Analysis: &ref})
}

func refID(ref *ItemRef) string {
	if ref == nil {
		return ""
	}
	return ref.ID
}

func refRevision(ref *ItemRef) string {
	if ref == nil {
		return ""
	}
	return ref.Revision
}

// recordMade records the analysis object a new entry holds, when the
// application made it and, when one is given, the name a person gave it. The
// object stays a discovered one: its entry is its backing, and no revision is
// ever saved onto it.
func (a *App) recordMade(loaded *loadedCatalog, entry, name string) (ItemRef, error) {
	stamp := catalog.Stamp(a.now())
	var ref ItemRef
	_, err := loaded.store.Update(a.now(), func(document *catalog.Document) (bool, error) {
		associated(document, []found{{AnalysisItem, entry}}, catalog.MaxItems)
		index := document.ByEntry(string(AnalysisItem), entry)
		if index < 0 {
			return false, catalog.ErrNoItem
		}
		document.Items[index].CreatedAt, document.Items[index].UpdatedAt = stamp, stamp
		if name != "" {
			document.Items[index].Name = name
		}
		ref = ItemRef{Kind: AnalysisItem, ID: document.Items[index].ID}
		return true, nil
	})
	return ref, err
}

// FindingReviewDraft is a person's decisions about the findings of one
// analysis. ReportSHA256 is the identity of the report the decisions were
// made while looking at; an analysis whose report is no longer those bytes
// is refused rather than reviewed. Decisions keep the order a person made
// them in.
type FindingReviewDraft struct {
	Analysis     ItemRef                  `json:"analysis"`
	ReportSHA256 string                   `json:"report_sha256"`
	Decisions    []findingreview.Decision `json:"decisions"`
}

// FindingReviewSummary is one review: the analysis it is of, when the
// project holds it, the report identity its decisions name, and how many
// decisions its current revision records.
type FindingReviewSummary struct {
	Analysis     *ItemRef `json:"analysis"`
	ReportSHA256 string   `json:"report_sha256"`
	Decisions    int      `json:"decisions"`
}

// encodeDecisions writes decisions as the document `readmit diagnose
// review` reads: deterministic, indented, one trailing newline.
func encodeDecisions(report string, decisions []findingreview.Decision) ([]byte, error) {
	if decisions == nil {
		decisions = []findingreview.Decision{}
	}
	data, err := json.Marshal(findingreview.Decisions{Schema: findingreview.DecisionsSchema, Report: report, Decisions: decisions},
		json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, errors.New("cannot encode finding decisions")
	}
	return append(data, '\n'), nil
}

// readDecisionsFile reads one saved decisions document and names it by the
// identity of its exact bytes.
func readDecisionsFile(path string) (findingreview.Decisions, string, error) {
	data, err := boundedFile(path, findingreview.MaxDecisionsBytes)
	if err != nil {
		return findingreview.Decisions{}, "", err
	}
	decisions, err := findingreview.ParseDecisions(data)
	if err != nil {
		return findingreview.Decisions{}, "", err
	}
	return decisions, diagnose.Identity(data), nil
}

func readFindingReview(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	decisions, _, err := readDecisionsFile(paths[primaryRole(FindingReviewItem)])
	if err != nil {
		return view{}, err
	}
	return view{summary: ItemSummary{FindingReview: &FindingReviewSummary{Analysis: c.analysisByReport(decisions.Report),
		ReportSHA256: decisions.Report, Decisions: len(decisions.Decisions)}}}, nil
}

// analysisByReport is the analysis object whose report is exactly the bytes
// identity names, when the project holds one.
func (c *loadedCatalog) analysisByReport(identity string) *ItemRef {
	for _, record := range c.document.Items {
		if record.Kind != string(AnalysisItem) || record.Entry == "" || c.removed(record) || len(record.Revisions) > 0 {
			continue
		}
		if !declares(filepath.Join(c.root, record.Entry, diagnose.ReportName), diagnose.Schema) {
			continue
		}
		if retained, err := c.retainedAnalysis(record.Entry, ""); err == nil && retained.Identity == identity {
			return &ItemRef{Kind: AnalysisItem, ID: record.ID}
		}
	}
	return nil
}

// reviewFor is the review of the report identity names, when there is one.
func (c *loadedCatalog) reviewFor(identity string) *ItemRef {
	for _, record := range c.document.Items {
		if record.Kind != string(FindingReviewItem) || c.removed(record) {
			continue
		}
		paths, availability, _ := c.backing(record)
		if availability != ItemAvailable {
			continue
		}
		if decisions, _, err := readDecisionsFile(paths[primaryRole(FindingReviewItem)]); err == nil && decisions.Report == identity {
			return &ItemRef{Kind: FindingReviewItem, ID: record.ID, Revision: record.RevisionLabel()}
		}
	}
	return nil
}

// openedReview is one analysis opened for a review: the report, bound to the
// identity the window displayed, and the verified case it was run over.
type openedReview struct {
	analysis ItemRef
	retained diagnose.Retained
	source   *bundle.Bundle
	entry    string
}

// reviewedAnalysis reopens the analysis a review is of, refusing one whose
// report is not the bytes the window displayed, and verifies the case the
// report names.
func (c *loadedCatalog) reviewedAnalysis(ref ItemRef, displayed string) (*openedReview, FieldProblem) {
	index := c.document.Find(ref.ID)
	if ref.Kind != AnalysisItem || index < 0 || c.document.Items[index].Kind != string(AnalysisItem) || c.removed(c.document.Items[index]) ||
		c.document.Items[index].Entry == "" || len(c.document.Items[index].Revisions) > 0 {
		return nil, FieldProblem{Field: "finding_review.analysis", Problem: "the project holds no such analysis"}
	}
	entry := c.document.Items[index].Entry
	retained, err := diagnose.OpenReport(filepath.Join(c.root, entry), diagnose.Reading{Displayed: displayed})
	if err != nil {
		return nil, FieldProblem{Field: "finding_review.report_sha256", Problem: err.Error()}
	}
	caseRef := c.caseByIdentity(retained.Report.CaseIdentity)
	if caseRef == nil {
		return nil, FieldProblem{Field: "finding_review.analysis", Problem: "the case this analysis was run over is not in this project"}
	}
	caseEntry := c.document.Items[c.document.Find(caseRef.ID)].Entry
	_, source, declined := openedCase(c.root, caseEntry, retained.Report.CaseIdentity)
	if source == nil {
		return nil, FieldProblem{Field: "finding_review.analysis", Problem: declined.reason}
	}
	return &openedReview{analysis: ItemRef{Kind: AnalysisItem, ID: ref.ID}, retained: retained, source: source, entry: caseEntry}, FieldProblem{}
}

// review joins the decisions to the analysis exactly as `readmit diagnose
// review` does, through the decisions document's own strict reader.
func (r *openedReview) review(decisions []findingreview.Decision) ([]byte, findingreview.Record, error) {
	data, err := encodeDecisions(r.retained.Identity, decisions)
	if err != nil {
		return nil, findingreview.Record{}, err
	}
	parsed, err := findingreview.ParseDecisions(data)
	if err != nil {
		return nil, findingreview.Record{}, err
	}
	record, err := findingreview.Review(findingreview.Reviewed{Report: r.retained.Report, Identity: r.retained.Identity, Case: r.source, Entry: r.entry},
		parsed, diagnose.Identity(data))
	return data, record, err
}

// checkFindingReview validates a review draft against the analysis it is of
// and answers the file a save stages, the normalized draft and the review
// record, or every problem found. There is one review of one analysis: a new
// review of an analysis already reviewed is refused, and a review is never
// moved to another analysis.
func checkFindingReview(scope draftScope, draft FindingReviewDraft) ([]catalog.Staged, *FindingReviewDraft, *findingreview.Record, []FieldProblem) {
	if scope.loaded == nil {
		return nil, nil, nil, []FieldProblem{{Field: "finding_review", Problem: "a finding review is decided against its project"}}
	}
	opened, problem := scope.loaded.reviewedAnalysis(draft.Analysis, draft.ReportSHA256)
	if opened == nil {
		return nil, nil, nil, []FieldProblem{problem}
	}
	if draft.ReportSHA256 == "" {
		return nil, nil, nil, []FieldProblem{{Field: "finding_review.report_sha256", Problem: "a review names the report it was made while looking at"}}
	}
	existing := scope.loaded.reviewFor(opened.retained.Identity)
	switch {
	case scope.item == "" && existing != nil:
		return nil, nil, nil, []FieldProblem{{Field: "finding_review.analysis", Problem: "this analysis already has a review; save a new revision of it"}}
	case scope.item != "" && (existing == nil || existing.ID != scope.item):
		return nil, nil, nil, []FieldProblem{{Field: "finding_review.analysis", Problem: "a review stays with the analysis it was made of"}}
	}
	data, record, err := opened.review(draft.Decisions)
	if err != nil {
		return nil, nil, nil, []FieldProblem{{Field: "finding_review.decisions", Problem: err.Error()}}
	}
	normalized := FindingReviewDraft{Analysis: opened.analysis, ReportSHA256: opened.retained.Identity, Decisions: slices.Clone(draft.Decisions)}
	if normalized.Decisions == nil {
		normalized.Decisions = []findingreview.Decision{}
	}
	return []catalog.Staged{{Role: "decisions", File: "decisions.json", Data: data}}, &normalized, &record, nil
}

func validateFindingReview(scope draftScope, draft FindingReviewDraft) ([]catalog.Staged, *FindingReviewDraft, []FieldProblem) {
	staged, normalized, _, problems := checkFindingReview(scope, draft)
	return staged, normalized, problems
}

// FindingReviewEffect is what one decision of a draft covers: the finding it
// names and every finding its suppression scope reaches, so a person sees
// what a suppression covers before saving it.
type FindingReviewEffect struct {
	Finding  string                `json:"finding"`
	Verdict  findingreview.Verdict `json:"verdict"`
	Scope    findingreview.Scope   `json:"scope,omitzero"`
	Findings []string              `json:"findings"`
}

// FindingReviewPreview is what saving a review draft would record: its
// problems, or the effect of each decision and every finding's resulting
// verdict.
type FindingReviewPreview struct {
	State    State                  `json:"state"`
	Reason   string                 `json:"reason,omitzero"`
	Context  RequestContext         `json:"context"`
	Problems []FieldProblem         `json:"problems"`
	Effects  []FindingReviewEffect  `json:"effects"`
	Statuses []findingreview.Status `json:"statuses"`
}

func (r *FindingReviewPreview) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// PreviewFindingReview answers what a finding-review draft would record,
// through the same validation its save makes, and writes nothing.
func (a *App) PreviewFindingReview(request DraftRequest) FindingReviewPreview {
	return run(a, false, false, func(ctx context.Context) FindingReviewPreview {
		result := FindingReviewPreview{Context: request.Context, Problems: []FieldProblem{}, Effects: []FindingReviewEffect{}, Statuses: []findingreview.Status{}}
		if request.Kind != FindingReviewItem || request.Draft.FindingReview == nil {
			result.refuse(Failed, "a preview is of one finding-review draft")
			return result
		}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		_, _, record, problems := checkFindingReview(draftScope{root: loaded.root, loaded: loaded, item: request.Item}, *request.Draft.FindingReview)
		result.State = Completed
		if len(problems) > 0 {
			result.Problems = problems
			return result
		}
		result.Statuses = record.Findings
		for _, decision := range request.Draft.FindingReview.Decisions {
			effect := FindingReviewEffect{Finding: decision.Finding, Verdict: decision.Verdict, Scope: decision.Scope, Findings: []string{}}
			for _, status := range record.Findings {
				if status.Basis == findingreview.BasisDecision && status.Finding == decision.Finding || status.SuppressedBy == decision.Finding {
					effect.Findings = append(effect.Findings, status.Finding)
				}
			}
			result.Effects = append(result.Effects, effect)
		}
		return result
	})
}

// FindingReviewRevision is one saved revision of a review: when and by whom
// it was saved and the decisions it records, in the order they were made.
type FindingReviewRevision struct {
	Revision    string                   `json:"revision"`
	PublishedAt *string                  `json:"published_at"`
	Author      string                   `json:"author,omitzero"`
	Decisions   []findingreview.Decision `json:"decisions"`
}

// FindingReviewHistoryResult is the review of one analysis: every saved
// revision, newest first, and every finding's verdict as the current
// revision leaves it — not reviewed where nothing was decided.
type FindingReviewHistoryResult struct {
	State        State                   `json:"state"`
	Reason       string                  `json:"reason,omitzero"`
	Context      RequestContext          `json:"context"`
	Review       *ItemRef                `json:"review,omitzero"`
	Analysis     *ItemRef                `json:"analysis,omitzero"`
	ReportSHA256 string                  `json:"report_sha256,omitzero"`
	Revisions    []FindingReviewRevision `json:"revisions"`
	Statuses     []findingreview.Status  `json:"statuses"`
}

func (r *FindingReviewHistoryResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// FindingReviewHistory reads the review of one analysis, named by the
// analysis or by the review itself. An analysis nobody reviewed answers
// Empty, with every finding not reviewed. Undoing a decision is a new
// revision without it, so every earlier decision and its reason stays here.
func (a *App) FindingReviewHistory(request ItemRequest) FindingReviewHistoryResult {
	return run(a, false, false, func(ctx context.Context) FindingReviewHistoryResult {
		result := FindingReviewHistoryResult{Context: request.Context, Revisions: []FindingReviewRevision{}, Statuses: []findingreview.Status{}}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		index := loaded.document.Find(request.Ref.ID)
		if index < 0 || loaded.document.Items[index].Kind != string(request.Ref.Kind) || loaded.removed(loaded.document.Items[index]) {
			result.refuse(Failed, "the project holds no such object")
			return result
		}
		var review *catalog.Item
		var analysis ItemRef
		var displayed string
		switch request.Ref.Kind {
		case FindingReviewItem:
			review = &loaded.document.Items[index]
			paths, availability, reason := loaded.backing(*review)
			if availability != ItemAvailable {
				result.refuse(Failed, "this review is "+string(availability)+": "+reason)
				return result
			}
			decisions, _, err := readDecisionsFile(paths[primaryRole(FindingReviewItem)])
			if err != nil {
				result.refuse(Failed, err.Error())
				return result
			}
			found := loaded.analysisByReport(decisions.Report)
			if found == nil {
				result.refuse(Failed, "the analysis this review was made of is no longer in the project as it was reviewed")
				return result
			}
			analysis, displayed = *found, decisions.Report
		case AnalysisItem:
			analysis = ItemRef{Kind: AnalysisItem, ID: request.Ref.ID}
		default:
			result.refuse(Failed, "a review history is of one analysis or one review")
			return result
		}
		opened, problem := loaded.reviewedAnalysis(analysis, displayed)
		if opened == nil {
			result.refuse(Failed, problem.Problem)
			return result
		}
		if review == nil {
			if ref := loaded.reviewFor(opened.retained.Identity); ref != nil {
				review = &loaded.document.Items[loaded.document.Find(ref.ID)]
			}
		}
		result.Analysis, result.ReportSHA256 = &opened.analysis, opened.retained.Identity
		var current []findingreview.Decision
		if review != nil {
			result.Review = &ItemRef{Kind: FindingReviewItem, ID: review.ID, Revision: review.RevisionLabel()}
			for i := len(review.Revisions) - 1; i >= 0; i-- {
				revision := review.Revisions[i]
				decisions, err := loaded.savedDecisions(revision)
				if err != nil {
					result.refuse(Failed, err.Error())
					return result
				}
				result.Revisions = append(result.Revisions, FindingReviewRevision{Revision: revisionLabel(revision.Number),
					PublishedAt: stamped(revision.PublishedAt), Author: revision.Author, Decisions: decisions.Decisions})
				if i == len(review.Revisions)-1 {
					current = decisions.Decisions
				}
			}
		}
		_, record, err := opened.review(current)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		result.Statuses = record.Findings
		result.State = Completed
		if review == nil {
			result.State, result.Reason = Empty, "nobody has reviewed this analysis"
		}
		return result
	})
}

// savedDecisions reads the decisions one saved revision recorded, as the
// bytes that were published.
func (c *loadedCatalog) savedDecisions(revision catalog.Revision) (findingreview.Decisions, error) {
	for _, member := range revision.Members {
		if member.Role != primaryRole(FindingReviewItem) {
			continue
		}
		data, err := savedFile.Read(c.store.Path(member))
		if err != nil {
			return findingreview.Decisions{}, errors.New("a saved revision of this review cannot be read")
		}
		if diagnose.Identity(data) != member.SHA256 {
			return findingreview.Decisions{}, errors.New("a saved revision of this review changed after it was saved")
		}
		return findingreview.ParseDecisions(data)
	}
	return findingreview.Decisions{}, errors.New("a saved revision of this review records no decisions")
}

// The states one chosen case of a comparison ends in. Every chosen case is a
// row: one that could not be analyzed is kept as that, with its reason, and
// never dropped from the answer.
type SimilarMemberState string

const (
	SimilarAnalyzed    SimilarMemberState = "analyzed"
	SimilarUnavailable SimilarMemberState = "unavailable"
	SimilarUnsupported SimilarMemberState = "unsupported"
	SimilarOverLimit   SimilarMemberState = "over_limit"
)

// SimilarRequest compares the findings of the chosen cases under one profile.
// Save retains the comparison as a new analysis of the project under Name,
// which a saved comparison must have.
type SimilarRequest struct {
	Context RequestContext     `json:"context"`
	Cases   []ItemRef          `json:"cases"`
	Profile AnalysisProfileRef `json:"profile"`
	Save    bool               `json:"save"`
	Name    string             `json:"name,omitzero"`
}

// SimilarMember is one chosen case and what became of it.
type SimilarMember struct {
	Case   ItemRef            `json:"case"`
	Name   string             `json:"name"`
	State  SimilarMemberState `json:"state"`
	Reason string             `json:"reason,omitzero"`
}

// SimilarFinding is one finding of one compared case and the occurrences its
// evidence references, each once, in evidence order.
type SimilarFinding struct {
	Case ItemRef `json:"case"`
	// Member is the position of its case among the result's members, which
	// tells two cases the project no longer holds apart.
	Member      int      `json:"member"`
	Finding     string   `json:"finding"`
	Occurrences []string `json:"occurrences"`
}

// SimilarGroup is one signature the compared cases share: the rule and the
// name a person reads for it, the classification the engine gave it, the cases it occurs in and every
// finding with it. An equal signature is an equal diagnostic shape, not a
// shared root cause.
type SimilarGroup struct {
	Signature      string                  `json:"signature"`
	RuleID         string                  `json:"rule_id"`
	RuleName       string                  `json:"rule_name"`
	Classification diagnose.Classification `json:"classification"`
	Cases          []ItemRef               `json:"cases"`
	Members        []SimilarFinding        `json:"members"`
}

// SimilarResult answers one comparison: every chosen case as a row, the
// groups of the cases that were analyzed, and the retained comparison, with
// its name, when one was saved or reopened.
type SimilarResult struct {
	State   State           `json:"state"`
	Reason  string          `json:"reason,omitzero"`
	Context RequestContext  `json:"context"`
	Members []SimilarMember `json:"members"`
	Groups  []SimilarGroup  `json:"groups"`
	Saved   *ItemRef        `json:"saved,omitzero"`
	Name    string          `json:"name,omitzero"`
}

func (r *SimilarResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// FindSimilarFindings analyzes each chosen case under one profile and groups
// the findings of those it analyzed by signature, exactly as `readmit
// diagnose groups` groups them. A case that is unavailable, that the engine
// refuses the profile for, or past the sixteen a comparison reads, stays a
// row with its reason. It is interruptible; stopped, it answers cancelled and
// writes nothing. Saving retains the grouping as `readmit diagnose groups`
// writes it and takes the author admission.
func (a *App) FindSimilarFindings(request SimilarRequest) SimilarResult {
	return runNamed[SimilarResult, *SimilarResult](a, profiles["FindSimilarFindings"], func(ctx context.Context) SimilarResult {
		return a.findSimilarFindings(ctx, request)
	})
}

func (a *App) findSimilarFindings(ctx context.Context, request SimilarRequest) SimilarResult {
	result := SimilarResult{Context: request.Context, Members: []SimilarMember{}, Groups: []SimilarGroup{}}
	cancelled := func() SimilarResult {
		return SimilarResult{State: cancelledRefusal.state, Reason: cancelledRefusal.reason, Context: request.Context, Members: []SimilarMember{}, Groups: []SimilarGroup{}}
	}
	if len(request.Cases) == 0 {
		result.refuse(Failed, "a comparison reads the cases chosen for it; choose at least one")
		return result
	}
	name := strings.TrimSpace(request.Name)
	if request.Save {
		if err := a.admitAuthor(); err != nil {
			result.refuse(PermissionDenied, err.Error())
			return result
		}
		if !catalog.ValidName(name) {
			result.refuse(Failed, "a saved comparison is named: "+nameRule)
			return result
		}
	}
	loaded, declined := a.loadCatalog(ctx, request.Context, request.Save)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	config, declined := loaded.analysisProfile(request.Profile)
	if declined.reason != "" {
		result.refuse(declined.state, declined.reason)
		return result
	}
	var reports []diagnose.Report
	cases := map[string]int{}
	seen := map[string]bool{}
	for _, ref := range request.Cases {
		if seen[ref.ID] {
			continue
		}
		seen[ref.ID] = true
		if ctx.Err() != nil {
			return cancelled()
		}
		member := SimilarMember{Case: ItemRef{Kind: ref.Kind, ID: ref.ID}, State: SimilarUnavailable}
		index := loaded.document.Find(ref.ID)
		if (ref.Kind != CaseItem && ref.Kind != VariantItem) || index < 0 || loaded.document.Items[index].Kind != string(ref.Kind) || loaded.removed(loaded.document.Items[index]) {
			member.Reason = "the project holds no such case"
			result.Members = append(result.Members, member)
			continue
		}
		item := loaded.read(loaded.document.Items[index])
		member.Case, member.Name = item.Ref, item.Name
		if member.Name == "" && item.Summary.Case != nil {
			member.Name = item.Summary.Case.Entry
		}
		switch {
		case item.Availability != ItemAvailable:
			member.Reason = "this case is " + string(item.Availability) + ": " + item.Reason
		case len(reports) >= diagnose.MaxGroupCases:
			member.State, member.Reason = SimilarOverLimit, "a comparison reads at most 16 cases"
		default:
			path := filepath.Join(loaded.root, loaded.document.Items[index].Entry)
			checked, err := diagnose.Check(path, config)
			if err != nil {
				member.Reason = err.Error()
				break
			}
			if at := slices.IndexFunc(checked, diagnose.Refuses); at >= 0 {
				member.State, member.Reason = SimilarUnsupported, checked[at].Detail
				break
			}
			report, err := diagnose.RunContext(ctx, path, config)
			if ctx.Err() != nil {
				return cancelled()
			}
			if err != nil {
				member.Reason = err.Error()
				break
			}
			if _, duplicate := cases[report.CaseIdentity]; duplicate {
				member.Reason = "this is the same evidence as another chosen case"
				break
			}
			cases[report.CaseIdentity] = len(result.Members)
			reports = append(reports, report)
			member.State = SimilarAnalyzed
		}
		result.Members = append(result.Members, member)
	}
	if len(reports) == 0 {
		result.State, result.Reason = Empty, "no chosen case could be analyzed under this profile"
		return result
	}
	grouping, err := diagnose.GroupReports(ctx, reports)
	if ctx.Err() != nil {
		return cancelled()
	}
	if err != nil {
		result.refuse(Failed, err.Error())
		return result
	}
	result.Groups = similarGroups(grouping, func(identity string) (ItemRef, int) {
		at := cases[identity]
		return result.Members[at].Case, at
	})
	result.State = Completed
	if !request.Save {
		return result
	}
	destination, declined := groupingOutput.destination(loaded.root, "")
	if declined.reason != "" {
		result.refuse(declined.state, declined.reason)
		return result
	}
	if err := diagnose.WriteGroups(ctx, filepath.Join(loaded.root, destination.Name), grouping); err != nil {
		if ctx.Err() != nil {
			return cancelled()
		}
		result.refuse(Failed, err.Error())
		return result
	}
	saved, err := a.recordMade(loaded, destination.Name, name)
	if err != nil {
		result.refuse(Failed, "the comparison was written to the project but its date and name could not be recorded: "+err.Error())
		return result
	}
	result.Saved, result.Name = &saved, name
	return result
}

// similarGroups lays out the groups of one grouping, naming each case by the
// object caseOf answers for its evidence. Every group keeps a row per
// distinct evidence it occurs in, and every member finding the occurrences
// its evidence references.
func similarGroups(grouping diagnose.GroupsReport, caseOf func(identity string) (ItemRef, int)) []SimilarGroup {
	findings := map[diagnose.FindingReference]diagnose.Finding{}
	for _, report := range grouping.Cases {
		for _, finding := range report.Findings {
			findings[diagnose.FindingReference{CaseIdentity: report.CaseIdentity, FindingID: finding.ID}] = finding
		}
	}
	ruleNames := map[string]string{}
	for _, rule := range diagnose.Rules() {
		ruleNames[rule.ID] = rule.Name
	}
	groups := []SimilarGroup{}
	for _, group := range grouping.Groups {
		out := SimilarGroup{Signature: group.Signature, RuleID: group.RuleID, RuleName: ruleNames[group.RuleID], Classification: findings[group.Members[0]].Classification,
			Cases: []ItemRef{}, Members: []SimilarFinding{}}
		seen := map[string]bool{}
		for _, member := range group.Members {
			ref, at := caseOf(member.CaseIdentity)
			if !seen[member.CaseIdentity] {
				seen[member.CaseIdentity] = true
				out.Cases = append(out.Cases, ref)
			}
			occurrences := []string{}
			for _, evidence := range findings[member].Evidence {
				if !slices.Contains(occurrences, evidence.Occurrence) {
					occurrences = append(occurrences, evidence.Occurrence)
				}
			}
			out.Members = append(out.Members, SimilarFinding{Case: ref, Member: at, Finding: member.FindingID, Occurrences: occurrences})
		}
		groups = append(groups, out)
	}
	return groups
}

// OpenSimilarFindings reopens one saved comparison, from History, as the
// comparison it recorded: every case it compared, each the project's case
// with that evidence or, where the project no longer holds it, a row saying
// so, and its groups exactly as saved. A saved comparison records only the
// cases it compared, so a case that could not be compared when it was saved
// is not among them. It runs nothing and writes nothing.
func (a *App) OpenSimilarFindings(request ItemRequest) SimilarResult {
	return run(a, false, false, func(ctx context.Context) SimilarResult {
		result := SimilarResult{Context: request.Context, Members: []SimilarMember{}, Groups: []SimilarGroup{}}
		if request.Ref.Kind != AnalysisItem {
			result.refuse(Failed, "a saved comparison is one analysis of the project")
			return result
		}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Ref, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		if item.Summary.Analysis == nil || item.Summary.Analysis.Form != "grouping" {
			result.refuse(Failed, "this analysis is not a saved comparison")
			return result
		}
		paths, availability, reason := loaded.backing(loaded.document.Items[loaded.document.Find(item.Ref.ID)])
		if availability != ItemAvailable {
			result.refuse(Failed, reason)
			return result
		}
		grouping, err := diagnose.OpenGroups(paths[primaryRole(AnalysisItem)])
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		cases := map[string]int{}
		for _, report := range grouping.Cases {
			member := SimilarMember{Case: ItemRef{Kind: CaseItem}, Name: "Case no longer in this project", State: SimilarUnavailable,
				Reason: "the project no longer holds this evidence"}
			if ref := loaded.caseByIdentity(report.CaseIdentity); ref != nil {
				held := loaded.read(loaded.document.Items[loaded.document.Find(ref.ID)])
				member.Case, member.Name, member.State, member.Reason = held.Ref, held.Name, SimilarAnalyzed, ""
				if member.Name == "" && held.Summary.Case != nil {
					member.Name = held.Summary.Case.Entry
				}
			}
			cases[report.CaseIdentity] = len(result.Members)
			result.Members = append(result.Members, member)
		}
		result.Groups = similarGroups(grouping, func(identity string) (ItemRef, int) {
			at := cases[identity]
			return result.Members[at].Case, at
		})
		saved := item.Ref
		result.State, result.Saved, result.Name = Completed, &saved, item.Name
		return result
	})
}
