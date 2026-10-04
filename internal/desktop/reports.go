package desktop

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// Reports (#559): a report is a named document made from actual retained
// runs. Creating one assembles the retained packet of its runs internally —
// the exact specification each run executed, its case and its evidence,
// byte for byte — and publishes it as a new entry of the project beside
// what the person wrote, its title and notes, as one revision. Editing the
// title or notes publishes a new revision of what was written and never
// touches the runs or their outcomes. Opening a report verifies its packet
// and reads the structured report from it; exporting and sharing it
// (report_share.go, #560) write the exact bytes a person reviewed.

// ReportDraft is a report as its sheets hold it: its title and notes, the
// run it reports and, optionally, a distinct run it is compared with. Job
// names one job of a suite run. The runs are chosen when a report is
// created and are fixed afterwards.
type ReportDraft struct {
	Title         string   `json:"title"`
	Notes         string   `json:"notes"`
	Run           ItemRef  `json:"run"`
	Job           string   `json:"job,omitzero"`
	Comparison    *ItemRef `json:"comparison,omitzero"`
	ComparisonJob string   `json:"comparison_job,omitzero"`
}

// ReportSourcesSchema is the contract of the runs a saved report names.
const ReportSourcesSchema = "readmit-report-sources/v1"
const ReportConnectedSourcesSchema = "readmit-report-sources/v2"

// reportSources are the runs a saved report was made from, by the catalog
// identity the project gives them.
type reportSources struct {
	Family string         `json:"family,omitzero"`
	Schema string         `json:"schema"`
	Runs   []reportSource `json:"runs"`
}

type reportSource struct {
	Role string `json:"role"`
	Run  string `json:"run"`
	Job  string `json:"job"`
}

// reportPrefix names the entries a report's retained packet is published as.
const reportPrefix = "report"

// Member roles of a saved report.
const (
	authoredRole = "report"
	sourcesRole  = "sources"
)

func decodeReportSources(data []byte) (reportSources, error) {
	var sources reportSources
	invalid := errors.New("the runs of this report cannot be read")
	if len(data) > catalog.MaxMemberBytes || json.Unmarshal(data, &sources, json.RejectUnknownMembers(true)) != nil || (sources.Schema != ReportSourcesSchema && sources.Schema != ReportConnectedSourcesSchema) || len(sources.Runs) == 0 || len(sources.Runs) > 2 {
		return reportSources{}, invalid
	}
	if sources.Schema == ReportSourcesSchema {
		var members map[string]any
		if json.Unmarshal(data, &members) != nil {
			return reportSources{}, invalid
		}
		if _, held := members["family"]; held {
			return reportSources{}, invalid
		}
	} else if sources.Family != "connected-lifecycle" {
		return reportSources{}, invalid
	}
	for i, source := range sources.Runs {
		role := report.CurrentRole
		if i == 1 {
			role = report.ComparisonRole
		}
		if source.Role != role || !catalog.ValidID(source.Run) || strings.Contains(source.Job, "/") {
			return reportSources{}, invalid
		}
	}
	return sources, nil
}

// reportRun is a run a report draft names, verified now.
type reportRun struct {
	connected bool
	item      catalog.Item
	path      string
	casePath  string
	identity  string
	test      string
}

// resolveReportRun reads the run a draft names: a finished test run of the
// project, or one job of a suite run, whose result is final and whose
// lifecycle is decided, and the case it sent from.
func resolveReportRun(scope draftScope, ref ItemRef, job, field string) (reportRun, *FieldProblem) {
	problem := func(text string) *FieldProblem { return &FieldProblem{Field: field, Problem: text} }
	loaded := scope.loaded
	if loaded == nil || ref.Kind != RunItem {
		return reportRun{}, problem("Choose a run of this project")
	}
	index := loaded.document.Find(ref.ID)
	if index < 0 || loaded.document.Items[index].Kind != string(RunItem) || loaded.removed(loaded.document.Items[index]) || loaded.document.Items[index].Entry == "" {
		return reportRun{}, problem("This project holds no such run")
	}
	item := loaded.document.Items[index]
	name := item.Entry
	if job != "" {
		if strings.Contains(job, "/") {
			return reportRun{}, problem("This suite run holds no such test")
		}
		name = filepath.Join(item.Entry, "runs", job)
	}
	path, err := runEvidencePath(loaded.root, name)
	if err != nil {
		return reportRun{}, problem("This run cannot be read")
	}
	// Practice runs retain the result under their owned session, exactly as
	// the ordinary run-detail reader resolves them.
	path = runFolder(path)
	if connectedIndividualArtifact(path) {
		if job != "" {
			return reportRun{}, problem("a connected individual report does not select a suite job")
		}
		proof, err := connectedrun.OpenFlowEvidence(context.Background(), path)
		if err != nil {
			return reportRun{}, problem("the connected lifecycle cannot be verified; missing evidence cannot be replaced")
		}
		return reportRun{connected: true, item: item, path: path, identity: proof.Identity, test: proof.Plan.Document().Test.ID}, nil
	}
	opened, err := runresult.Open(path)
	switch {
	case err != nil:
		return reportRun{}, problem("This run cannot be verified")
	case opened.Artifact == nil:
		return reportRun{}, problem("This run did not finish; a report is made from a finished run")
	case opened.Spec == nil:
		return reportRun{}, problem("This run retained no test; it cannot form a report")
	}
	if usable, _ := opened.Usable(); !usable {
		return reportRun{}, problem("This run's delivery is uncertain or its record is incomplete; its outcome is not decided")
	}
	entry := loaded.caseEntry(opened.Artifact.Result.InputBundleIdentity)
	if entry == "" {
		return reportRun{}, problem("The case this run sent is no longer in this project")
	}
	casePath := filepath.Join(loaded.root, entry)
	return reportRun{item: item, path: path, casePath: casePath, identity: opened.Artifact.Identity, test: opened.Spec.Name}, nil
}

// caseEntry is the entry of the case or variant of the project whose
// evidence is the identity a run sent.
func (c *loadedCatalog) caseEntry(identity string) string {
	for _, item := range c.document.Items {
		if item.Kind != string(CaseItem) && item.Kind != string(VariantItem) || item.Entry == "" || c.removed(item) {
			continue
		}
		known, ok := c.identities[item.Entry]
		if !ok {
			if facts, _, err := operation.VerifiedCase(c.root, item.Entry); err == nil {
				known = facts.Identity
			}
			c.identities[item.Entry] = known
		}
		if known == identity {
			return item.Entry
		}
	}
	return ""
}

// validateReportDraft validates a whole report: its runs when it is created,
// the runs it keeps when it is edited, and what the person wrote.
func validateReportDraft(scope draftScope, draft ItemDraft) ([]catalog.Staged, ItemDraft, []FieldProblem) {
	normalized := ItemDraft{}
	if draft.Report == nil {
		return nil, normalized, []FieldProblem{{Field: "report", Problem: "a report names the run it reports"}}
	}
	given := *draft.Report
	problems := []FieldProblem{}
	title := strings.Join(strings.Fields(given.Title), " ")
	notes := strings.TrimRight(given.Notes, " \t\r\n")
	sources := reportSources{Schema: ReportSourcesSchema}
	if scope.item == "" {
		current, problem := resolveReportRun(scope, given.Run, given.Job, "report.run")
		if problem != nil {
			problems = append(problems, *problem)
		}
		if current.connected {
			sources.Schema, sources.Family = ReportConnectedSourcesSchema, "connected-lifecycle"
		}
		sources.Runs = append(sources.Runs, reportSource{Role: report.CurrentRole, Run: given.Run.ID, Job: given.Job})
		if given.Comparison != nil {
			comparison, problem := resolveReportRun(scope, *given.Comparison, given.ComparisonJob, "report.comparison")
			switch {
			case problem != nil:
				problems = append(problems, *problem)
			case current.connected != comparison.connected:
				problems = append(problems, FieldProblem{Field: "report.comparison", Problem: "the supplied runs use different report evidence contracts"})
			case given.Comparison.ID == given.Run.ID && given.ComparisonJob == given.Job || current.identity != "" && comparison.identity == current.identity:
				problems = append(problems, FieldProblem{Field: "report.comparison", Problem: "Choose a different run to compare with"})
			}
			sources.Runs = append(sources.Runs, reportSource{Role: report.ComparisonRole, Run: given.Comparison.ID, Job: given.ComparisonJob})
		}
		if title == "" {
			title = report.TitleFor(current.test)
		}
	} else {
		held, problem := scope.savedReportSources()
		if problem != nil {
			return nil, normalized, []FieldProblem{*problem}
		}
		sources = held
		if !sameReportRuns(held, given) {
			problems = append(problems, FieldProblem{Field: "report.run", Problem: "The runs of a report are fixed; create a new report for other runs"})
		}
	}
	authored := report.Authored{Title: title, Notes: notes}
	if err := (report.Authored{Title: title}).Validate(); err != nil {
		problems = append(problems, FieldProblem{Field: "report.title", Problem: err.Error()})
	} else if err := authored.Validate(); err != nil {
		problems = append(problems, FieldProblem{Field: "report.notes", Problem: err.Error()})
	}
	if len(problems) > 0 {
		return nil, normalized, problems
	}
	written, err := report.EncodeAuthored(authored)
	if err != nil {
		return nil, normalized, []FieldProblem{{Field: "report.title", Problem: err.Error()}}
	}
	runs, err := encodeMember(sources)
	if err != nil {
		return nil, normalized, []FieldProblem{{Field: "report", Problem: "the runs of this report cannot be encoded"}}
	}
	normalized.Report = &ReportDraft{Title: title, Notes: notes, Run: ItemRef{Kind: RunItem, ID: sources.Runs[0].Run}, Job: sources.Runs[0].Job}
	if len(sources.Runs) > 1 {
		normalized.Report.Comparison = &ItemRef{Kind: RunItem, ID: sources.Runs[1].Run}
		normalized.Report.ComparisonJob = sources.Runs[1].Job
	}
	return []catalog.Staged{{Role: authoredRole, File: "report.json", Data: written}, {Role: sourcesRole, File: "sources.json", Data: runs}}, normalized, nil
}

func sameReportRuns(held reportSources, given ReportDraft) bool {
	if held.Runs[0].Run != given.Run.ID || held.Runs[0].Job != given.Job {
		return false
	}
	if len(held.Runs) == 1 {
		return given.Comparison == nil
	}
	return given.Comparison != nil && held.Runs[1].Run == given.Comparison.ID && held.Runs[1].Job == given.ComparisonJob
}

// savedReportSources are the runs the saved report an edit began from names.
func (scope draftScope) savedReportSources() (reportSources, *FieldProblem) {
	refused := &FieldProblem{Field: "report", Problem: "This project holds no such report"}
	if scope.loaded == nil {
		return reportSources{}, refused
	}
	index := scope.loaded.document.Find(scope.item)
	if index < 0 || scope.loaded.document.Items[index].Kind != string(ReportItem) {
		return reportSources{}, refused
	}
	item := scope.loaded.document.Items[index]
	if item.Current() == nil {
		return reportSources{}, &FieldProblem{Field: "report", Problem: "This report was not made in this application; its title and notes are not edited"}
	}
	paths, availability, reason := scope.loaded.backing(item)
	if availability != ItemAvailable {
		return reportSources{}, &FieldProblem{Field: "report", Problem: reason}
	}
	data, err := boundedFile(paths[sourcesRole], catalog.MaxMemberBytes)
	if err != nil {
		return reportSources{}, &FieldProblem{Field: "report", Problem: "the runs of this report cannot be read"}
	}
	sources, err := decodeReportSources(data)
	if err != nil {
		return reportSources{}, &FieldProblem{Field: "report", Problem: err.Error()}
	}
	return sources, nil
}

// reportEntry is how a new report's retained packet is built: from its runs
// and their cases through the retained-packet assembly the command line
// runs, into the entry the save publishes.
func reportEntry(ctx context.Context, scope draftScope, draft *ReportDraft) (*catalog.Entry, *FieldProblem) {
	current, problem := resolveReportRun(scope, draft.Run, draft.Job, "report.run")
	if problem != nil {
		return nil, problem
	}
	if current.connected {
		input := report.ConnectedInput{Current: current.path}
		if draft.Comparison != nil {
			baseline, problem := resolveReportRun(scope, *draft.Comparison, draft.ComparisonJob, "report.comparison")
			if problem != nil {
				return nil, problem
			}
			if !baseline.connected {
				return nil, &FieldProblem{Field: "report.comparison", Problem: "select another connected lifecycle"}
			}
			input.Baseline = baseline.path
		}
		return &catalog.Entry{Prefix: reportPrefix, Owes: current.item.Entry, Build: func(path string) error { _, err := report.AssembleConnected(ctx, input, path); return err }}, nil
	}
	input := report.RunsInput{Case: current.casePath, Current: current.path}
	if draft.Comparison != nil {
		comparison, problem := resolveReportRun(scope, *draft.Comparison, draft.ComparisonJob, "report.comparison")
		if problem != nil {
			return nil, problem
		}
		input.Comparison, input.ComparisonCase = comparison.path, comparison.casePath
	}
	// The packet owes the project no association; it names the run it
	// reports as the entry it was made from.
	return &catalog.Entry{Prefix: reportPrefix, Owes: current.item.Entry, Build: func(path string) error {
		_, err := report.AssembleRuns(ctx, input, path)
		return err
	}}, nil
}

// verifyReport reads a staged report back: what was written, the runs it
// names and, when the save publishes it, its retained packet through the
// retained verifier.
func verifyReport(files map[string]string) error {
	data, err := boundedFile(files[authoredRole], catalog.MaxMemberBytes)
	if err != nil {
		return err
	}
	if _, err := report.DecodeAuthored(data); err != nil {
		return err
	}
	data, err = boundedFile(files[sourcesRole], catalog.MaxMemberBytes)
	if err != nil {
		return err
	}
	if _, err := decodeReportSources(data); err != nil {
		return err
	}
	if packet, held := files[catalog.EntryRole]; held {
		if declares(filepath.Join(packet, "manifest.json"), report.ConnectedSchema) {
			_, err := report.OpenConnected(context.Background(), packet)
			return err
		}
		if _, err := report.OpenRetained(context.Background(), packet); err != nil {
			return err
		}
	}
	return nil
}

// reportBacking is what one report is read from: its retained packet, what
// was written for it, the runs it names and the revision read.
type reportBacking struct {
	connected *report.ConnectedPacket
	packetDir string
	packet    *report.RetainedPacket
	authored  report.Authored
	sources   *reportSources
	revision  string
	form      string
}

// reportBacking reads one report at a revision, the current one when
// revision is empty. A report this application saved is read from its
// revision's files and its entry; an investigation packet or a portable
// review of earlier releases is read through its own verifier and titled
// after its test.
func (c *loadedCatalog) reportBacking(item catalog.Item, revision string) (reportBacking, error) {
	backing := reportBacking{}
	path := filepath.Join(c.root, item.Entry)
	if item.Entry == "" {
		return backing, errors.New("this report has no retained runs")
	}
	if current := item.Current(); current != nil {
		chosen := current
		if revision != "" {
			chosen = nil
			for i := range item.Revisions {
				if strconv.Itoa(item.Revisions[i].Number) == revision {
					chosen = &item.Revisions[i]
				}
			}
			if chosen == nil {
				return backing, errors.New("this report has no such version")
			}
		}
		files := map[string][]byte{}
		for _, member := range chosen.Members {
			data, err := savedFile.Read(c.store.Path(member))
			if err != nil {
				return backing, errors.New("a file this report was saved as cannot be read")
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != member.SHA256 {
				return backing, errors.New("a file this report was saved as changed after it was saved")
			}
			files[member.Role] = data
		}
		authored, err := report.DecodeAuthored(files[authoredRole])
		if err != nil {
			return backing, err
		}
		sources, err := decodeReportSources(files[sourcesRole])
		if err != nil {
			return backing, err
		}
		backing = reportBacking{packetDir: path, authored: authored, sources: &sources, revision: strconv.Itoa(chosen.Number), form: "report"}
	} else {
		manifest := filepath.Join(path, "manifest.json")
		switch {
		case declares(manifest, report.RetainedSchema), declares(manifest, report.ConnectedSchema):
			backing = reportBacking{packetDir: path, form: "packet"}
		case declares(manifest, report.ReviewSchema), declares(manifest, report.ReviewSchemaV3):
			review, err := report.OpenReview(c.ctx, path)
			if err != nil {
				return backing, err
			}
			backing = reportBacking{packetDir: filepath.Join(path, "packet"), form: "portable-review"}
			if review.Authored != nil {
				backing.authored = *review.Authored
			}
		default:
			return backing, errors.New("this report is not read as a document here")
		}
	}
	if declares(filepath.Join(backing.packetDir, "manifest.json"), report.ConnectedSchema) {
		packet, err := report.OpenConnected(c.ctx, backing.packetDir)
		if err != nil {
			return backing, err
		}
		backing.connected = packet
		if backing.authored.Title == "" {
			backing.authored = report.Authored{Schema: report.AuthoredSchema, Title: "Connected lifecycle report"}
		}
		return backing, nil
	}
	packet, err := report.OpenRetained(c.ctx, backing.packetDir)
	if err != nil {
		return backing, err
	}
	backing.packet = packet
	if backing.authored.Title == "" {
		backing.authored = report.Authored{Schema: report.AuthoredSchema, Title: report.DefaultTitle(backing.packetDir)}
	}
	return backing, nil
}

// readSavedReport reads a report this application saved for its row: its
// title, the case its current run sent and whether its current revision is
// reviewed.
func readSavedReport(c *loadedCatalog, item catalog.Item) (view, error) {
	backing, err := c.reportBacking(item, "")
	if err != nil {
		return view{}, err
	}
	summary := &ReportSummary{Form: "report", Status: "draft"}
	if c.reportReviewed(item.ID, backing.revision) {
		summary.Status = "reviewed"
	}
	if backing.packet != nil {
		summary.RelatedCase = c.caseByIdentity(backing.packet.Manifest.Current.CaseIdentity)
	}
	c.reportAssociations(backing, summary)
	return view{name: backing.authored.Title, summary: ItemSummary{Report: summary}}, nil
}

// ReportRequest opens one report, at one revision when Ref names one, and
// shows the text of its values only when Reveal is set.
type ReportRequest struct {
	Context RequestContext `json:"context"`
	Ref     ItemRef        `json:"ref"`
	Reveal  bool           `json:"reveal"`
}

// ReportResult carries one opened report.
type ReportResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Report  *ReportView    `json:"report,omitzero"`
}

func (r *ReportResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ReportRun is one run a report includes, as the report states it, and the
// run and case of the project it can be opened at, when the project still
// holds them.
type ReportRun struct {
	Role           string                `json:"role"`
	Test           string                `json:"test"`
	Result         report.DocumentResult `json:"result"`
	StartedAt      *string               `json:"started_at"`
	CompletedAt    *string               `json:"completed_at"`
	Boundary       string                `json:"boundary"`
	ResultIdentity string                `json:"result_identity"`
	SpecIdentity   string                `json:"spec_identity"`
	CaseIdentity   string                `json:"case_identity"`
	CaseProvenance string                `json:"case_provenance"`
	TargetIdentity string                `json:"target_identity"`
	Run            *ItemRef              `json:"run,omitzero"`
	Job            string                `json:"job,omitzero"`
	Case           *ItemRef              `json:"case,omitzero"`
}

// ReportChange is one check compared across the comparison run (before) and
// the report's run (after). A value's text is withheld as a check's is.
type ReportChange struct {
	Check           testauthor.Expectation `json:"check"`
	Definition      string                 `json:"definition"`
	Before          string                 `json:"before"`
	After           string                 `json:"after"`
	BeforeObserved  *testrunner.Value      `json:"before_observed,omitzero"`
	AfterObserved   *testrunner.Value      `json:"after_observed,omitzero"`
	BeforeRecords   *int                   `json:"before_records,omitzero"`
	AfterRecords    *int                   `json:"after_records,omitzero"`
	BeforeAvailable bool                   `json:"before_available"`
	AfterAvailable  bool                   `json:"after_available"`
}

// ReportComparison is what a report's comparison run establishes.
type ReportComparison struct {
	SameCase      bool           `json:"same_case"`
	SameTarget    bool           `json:"same_target"`
	Specification string         `json:"specification"`
	Checks        []ReportChange `json:"checks"`
}

// ReportVersion is one published revision of a saved report.
type ReportVersion struct {
	Revision    string   `json:"revision"`
	Title       string   `json:"title"`
	PublishedAt *string  `json:"published_at"`
	Author      string   `json:"author,omitzero"`
	Runs        []string `json:"runs"`
	Review      string   `json:"review"`
	Current     bool     `json:"current"`
}

// ReportView is one report as its page reads it: the structured report,
// its checks and comparison with text withheld until revealed, and its
// versions.
type ReportView struct {
	Connected   *report.ConnectedReport   `json:"connected,omitzero"`
	Item        CatalogItem               `json:"item"`
	Form        string                    `json:"form"`
	Revision    string                    `json:"revision,omitzero"`
	Current     bool                      `json:"current"`
	Title       string                    `json:"title"`
	Result      report.DocumentResult     `json:"result"`
	Runs        []ReportRun               `json:"runs"`
	Checks      []RunCheck                `json:"checks"`
	Comparison  *ReportComparison         `json:"comparison,omitzero"`
	Messages    []RunMessage              `json:"messages"`
	Notes       string                    `json:"notes,omitzero"`
	Limitations []string                  `json:"limitations"`
	Evidence    []report.DocumentEvidence `json:"evidence"`
	Packet      string                    `json:"packet"`
	Versions    []ReportVersion           `json:"versions"`
	Review      string                    `json:"review"`
	Draft       *ReportDraft              `json:"draft,omitzero"`
	Revealed    bool                      `json:"revealed"`
	// Shares are the completed shares of this report, newest first.
	Shares []ReportShareEntry `json:"shares"`
}

// OpenReport reads one report: it verifies the report's retained packet
// and reads the structured report from it. Reading changes nothing.
func (a *App) OpenReport(request ReportRequest) ReportResult {
	return runRead(a, false, func(ctx context.Context) ReportResult {
		result := ReportResult{Context: request.Context}
		loaded, item, refused := a.catalogItem(ctx, request.Context, ItemRef{Kind: request.Ref.Kind, ID: request.Ref.ID}, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		if item.Ref.Kind != ReportItem {
			result.refuse(Failed, "only a report is opened here")
			return result
		}
		if item.Availability != ItemAvailable {
			result.refuse(Failed, item.Reason)
			return result
		}
		shown, err := loaded.reportView(*item, request.Ref.Revision, request.Reveal)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		result.State, result.Report = Completed, shown
		return result
	})
}

// reportView reads one report for its page.
func (c *loadedCatalog) reportView(item CatalogItem, revision string, reveal bool) (*ReportView, error) {
	record := c.document.Items[c.document.Find(item.Ref.ID)]
	backing, err := c.reportBacking(record, revision)
	if err != nil {
		return nil, err
	}
	if backing.connected != nil {
		return c.connectedReportView(item, record, backing, reveal)
	}
	doc, err := report.BuildDocument(c.ctx, backing.packetDir, backing.packet, backing.authored)
	if err != nil {
		return nil, err
	}
	shown := &ReportView{Item: item, Form: backing.form, Revision: backing.revision, Current: backing.revision == "" || backing.revision == record.RevisionLabel(),
		Title: doc.Title, Result: doc.Result, Runs: []ReportRun{}, Checks: []RunCheck{}, Messages: []RunMessage{}, Limitations: doc.Limitations,
		Evidence: doc.Evidence, Packet: doc.PacketIdentity, Versions: []ReportVersion{}, Review: "draft", Revealed: reveal}
	shown.Shares, _ = c.reportShares(record.ID)
	if doc.Notes != nil {
		shown.Notes = *doc.Notes
	}
	for i, run := range doc.Runs {
		row := ReportRun{Role: run.Role, Test: run.Test, Result: run.Result, StartedAt: stamped(run.StartedAt), CompletedAt: stamped(run.CompletedAt),
			Boundary: run.Boundary, ResultIdentity: run.ResultIdentity, SpecIdentity: run.SpecIdentity, CaseIdentity: run.CaseIdentity,
			CaseProvenance: run.CaseProvenance, TargetIdentity: run.TargetIdentity, Case: c.caseByIdentity(run.CaseIdentity)}
		if backing.sources != nil && i < len(backing.sources.Runs) {
			source := backing.sources.Runs[i]
			if index := c.document.Find(source.Run); index >= 0 && !c.removed(c.document.Items[index]) {
				row.Run, row.Job = &ItemRef{Kind: RunItem, ID: source.Run}, source.Job
			}
		}
		shown.Runs = append(shown.Runs, row)
	}
	for _, message := range doc.Messages {
		shown.Messages = append(shown.Messages, RunMessage{Source: message.Source, Delivery: RunDelivery(message.Delivery), ACKCode: message.ACKCode,
			Outcome: replay.Outcome(message.Outcome), Message: &TestMessage{ID: message.Source, Kind: bundle.EventKind(message.Kind),
				MessageCode: message.Code, TriggerEvent: message.Trigger, Sendable: message.Kind == string(bundle.Message)}})
	}
	for _, check := range doc.Checks {
		row := RunCheck{Check: expectationOf(testrunner.Assertion{ID: check.ID, Operator: check.Operator, Message: check.Message, Selector: check.Selector, Expected: check.Expected}),
			Result: RunCheckResult(check.Result), Unavailable: check.Unavailable, Messages: slices.Clone(check.Messages)}
		if check.Observed != nil {
			observed := *check.Observed
			row.Observed = &observed
		}
		hide(&row, reveal)
		shown.Checks = append(shown.Checks, row)
	}
	if doc.Comparison != nil {
		shown.Comparison = &ReportComparison{SameCase: doc.Comparison.SameCase, SameTarget: doc.Comparison.SameTarget, Specification: doc.Comparison.Specification, Checks: []ReportChange{}}
		for _, change := range doc.Comparison.Checks {
			row := ReportChange{Definition: change.Definition, Before: change.Before, After: change.After,
				BeforeAvailable: change.BeforeObserved != nil, AfterAvailable: change.AfterObserved != nil,
				Check: expectationOf(testrunner.Assertion{ID: change.ID, Operator: change.Operator, Message: change.Message, Selector: change.Selector})}
			row.BeforeObserved, row.BeforeRecords = withheld(change.BeforeObserved, reveal)
			row.AfterObserved, row.AfterRecords = withheld(change.AfterObserved, reveal)
			shown.Comparison.Checks = append(shown.Comparison.Checks, row)
		}
	}
	if backing.sources != nil {
		shown.Review = c.reportReviewState(record.ID, backing.revision)
		shown.Draft = &ReportDraft{Title: backing.authored.Title, Notes: backing.authored.Notes, Run: ItemRef{Kind: RunItem, ID: backing.sources.Runs[0].Run}, Job: backing.sources.Runs[0].Job}
		if len(backing.sources.Runs) > 1 {
			shown.Draft.Comparison = &ItemRef{Kind: RunItem, ID: backing.sources.Runs[1].Run}
			shown.Draft.ComparisonJob = backing.sources.Runs[1].Job
		}
		shown.Versions = c.reportVersions(record)
	}
	return shown, nil
}

// withheld is an observed value as a page shows it before its text is
// revealed: a field's state without its text, a record list counted.
func withheld(value *testrunner.Value, reveal bool) (*testrunner.Value, *int) {
	if value == nil {
		return nil, nil
	}
	shown := *value
	var records *int
	if shown.Records != nil {
		count := len(*shown.Records)
		records = &count
	}
	if reveal {
		return &shown, records
	}
	if shown.Field != nil && shown.Field.Text != nil {
		shown.Field = &testrunner.FieldValue{State: shown.Field.State}
	}
	shown.Records = nil
	return &shown, records
}

// reportVersions are a saved report's revisions, newest first, each with
// the runs it includes and whether it was reviewed.
func (c *loadedCatalog) reportVersions(record catalog.Item) []ReportVersion {
	versions := []ReportVersion{}
	current := record.RevisionLabel()
	for _, revision := range slices.Backward(record.Revisions) {
		label := strconv.Itoa(revision.Number)
		version := ReportVersion{Revision: label, PublishedAt: stamped(revision.PublishedAt), Author: revision.Author, Runs: []string{}, Current: label == current,
			Review: c.reportReviewState(record.ID, label)}
		for _, member := range revision.Members {
			data, err := savedFile.Read(c.store.Path(member))
			if err != nil {
				continue
			}
			switch member.Role {
			case authoredRole:
				if authored, err := report.DecodeAuthored(data); err == nil {
					version.Title = authored.Title
				}
			case sourcesRole:
				if sources, err := decodeReportSources(data); err == nil {
					for _, source := range sources.Runs {
						name := "Removed run"
						if index := c.document.Find(source.Run); index >= 0 && !c.removed(c.document.Items[index]) {
							name = cmp.Or(c.read(c.document.Items[index]).Name, "Run")
						}
						version.Runs = append(version.Runs, name)
					}
				}
			}
		}
		versions = append(versions, version)
	}
	return versions
}

// A report is reviewed by a person, never by producing a file: Mark
// reviewed is a reviewed action of its own that records one decision bound
// to the version shown. A version saved afterwards is a Draft again, and a
// review prepared for the earlier version is stale.

// ReviewReportAction records that one version of a report was reviewed.
const ReviewReportAction ActionID = "report.review"

// ReportReviewView is what Mark reviewed records: the report and version.
type ReportReviewView struct {
	Report  string `json:"report"`
	Version string `json:"version"`
}

// ReportReviewItem is the review history of one saved report: each review
// is one revision, bound to the report version it reviewed.
const ReportReviewItem ItemKind = "report-review"

// ReportApprovalSchema is the contract of one recorded review.
const ReportApprovalSchema = "readmit-report-approval/v1"

type reportApproval struct {
	Schema   string `json:"schema"`
	Report   string `json:"report"`
	Revision string `json:"revision"`
	Actor    string `json:"actor"`
	At       string `json:"at"`
}

func decodeReportApproval(data []byte) (reportApproval, error) {
	var approval reportApproval
	invalid := errors.New("the report review record cannot be read")
	if len(data) > catalog.MaxMemberBytes || json.Unmarshal(data, &approval, json.RejectUnknownMembers(true)) != nil || approval.Schema != ReportApprovalSchema ||
		!catalog.ValidID(approval.Report) || approval.Revision == "" || strings.TrimSpace(approval.Actor) == "" {
		return reportApproval{}, invalid
	}
	if _, err := time.Parse(time.RFC3339, approval.At); err != nil {
		return reportApproval{}, invalid
	}
	return approval, nil
}

func readReportApprovalFile(path string) (reportApproval, error) {
	data, err := boundedFile(path, catalog.MaxMemberBytes)
	if err != nil {
		return reportApproval{}, err
	}
	return decodeReportApproval(data)
}

func readReportApprovalItem(_ *loadedCatalog, _ catalog.Item, paths map[string]string) (view, error) {
	_, err := readReportApprovalFile(paths[string(ReportReviewItem)])
	return view{}, err
}

// reportReviews are the recorded reviews of every report of the project,
// read once per load: each report's history item and its reviewed versions.
func (c *loadedCatalog) reportReviews() map[string]reportHistory {
	if c.reviews != nil {
		return c.reviews
	}
	c.reviews = map[string]reportHistory{}
	for i := range c.document.Items {
		item := c.document.Items[i]
		if item.Kind != string(ReportReviewItem) || c.removed(item) {
			continue
		}
		for _, revision := range item.Revisions {
			for _, member := range revision.Members {
				approval, err := readReportApprovalFile(c.store.Path(member))
				if err != nil {
					continue
				}
				history := c.reviews[approval.Report]
				if history.versions == nil {
					history = reportHistory{item: &c.document.Items[i], versions: map[string]bool{}}
				}
				history.versions[approval.Revision] = true
				c.reviews[approval.Report] = history
			}
		}
	}
	return c.reviews
}

// reportHistory is one report's review history.
type reportHistory struct {
	item     *catalog.Item
	versions map[string]bool
}

func (c *loadedCatalog) reportReviewed(report, revision string) bool {
	return c.reportReviews()[report].versions[revision]
}

func (c *loadedCatalog) reportReviewState(report, revision string) string {
	if c.reportReviewed(report, revision) {
		return "reviewed"
	}
	return "draft"
}

func bindReportReview(a *App, ctx context.Context, request PrepareActionRequest, _ bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != ReportItem {
		return nil, refusal{Failed, "a review is recorded for one report"}
	}
	loaded, items, records, declined := a.scoped(ctx, request.Context, request.Items)
	if loaded == nil {
		return nil, declined
	}
	backing, err := loaded.reportBacking(records[0], "")
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	if backing.sources == nil {
		return nil, refusal{Failed, "only a report made here records a review"}
	}
	ready, refused := !loaded.reportReviewed(records[0].ID, backing.revision), ""
	if !ready {
		refused = "this version is already reviewed"
	}
	return &boundAction{action: ReviewReportAction, origin: request, reportReview: &reportApproval{Schema: ReportApprovalSchema, Report: records[0].ID, Revision: backing.revision},
		binding: binding(string(ReviewReportAction), loaded.root, loaded.document.Project.ID, a.reviewer(), records[0].ID, backing.revision, backing.identity()),
		review:  ActionReview{Items: items, Ready: ready, Refusal: refused, ReportReview: &ReportReviewView{Report: backing.authored.Title, Version: backing.revision}}}, noRefusal
}

// executeReportReview records one review as the next revision of its
// report's review history.
func executeReportReview(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	result := ReviewedActionResult{Outcome: ActionRefused}
	approval := *bound.reportReview
	approval.Actor, approval.At = a.reviewerName(), catalog.Stamp(a.now())
	data, err := encodeMember(approval)
	if err != nil {
		result.refuse(Failed, "the review could not be recorded")
		return result
	}
	loaded, declined := a.loadCatalog(ctx, bound.origin.Context, true)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	intent := ""
	if bound.executionReview != nil {
		intent = bound.executionReview.intent
	}
	sum := sha256.Sum256([]byte("report-review\x00" + intent))
	draft := catalog.Draft{Kind: string(ReportReviewItem), Intent: "report-review-" + hex.EncodeToString(sum[:16]), Digest: digestOf(data),
		Author: a.reviewerName(), Members: []catalog.Staged{{Role: string(ReportReviewItem), File: "review.json", Data: data}}}
	if history := loaded.reportReviews()[approval.Report].item; history != nil {
		draft.ItemID, draft.Base = history.ID, history.RevisionLabel()
	}
	if _, err := loaded.store.Save(draft, verifierFor(ReportReviewItem), catalog.Options{Now: a.now, Fault: a.saveFault}); err != nil {
		result.refuse(Failed, "the review was not recorded; nothing changed")
		return result
	}
	result.State, result.Outcome = Completed, ActionCompleted
	result.Approved = &ItemRef{Kind: ReportItem, ID: approval.Report, Revision: approval.Revision}
	return result
}

func (b reportBacking) identity() string {
	if b.connected != nil {
		return b.connected.Identity
	}
	if b.packet != nil {
		return b.packet.Identity
	}
	return ""
}
