package desktop

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/testrunner"
)

// A screen receives named objects and their current readable state; the
// application resolves the files, versions and relations behind each one.
// This file is that catalog's read contract: one envelope for every kind of
// object a project holds, populated by the same readers the command line and
// every older panel use, with each read failure kept on its own named row.
// The catalog's own store, readmit-catalog/v1, is internal/catalog.

// ItemKind is the kind of one named object. The set is closed.
type ItemKind string

const (
	ProjectItem     ItemKind = "project"
	CaseItem        ItemKind = "case"
	TestItem        ItemKind = "test"
	SuiteItem       ItemKind = "suite"
	RunItem         ItemKind = "run"
	EnvironmentItem ItemKind = "environment"
	ObservationItem ItemKind = "observation"
	ReportItem      ItemKind = "report"
	CheckGroupItem  ItemKind = "check-group"
	ProfileItem     ItemKind = "profile"
	ScenarioItem    ItemKind = "scenario"
	AnalysisItem    ItemKind = "analysis"
	VariantItem     ItemKind = "variant"
	BackupItem      ItemKind = "backup"
	RunnerItem      ItemKind = "runner"
	ScheduleItem    ItemKind = "schedule"
	// AnalysisSettingsItem is a named diagnosis configuration
	// (readmit-diagnose-config/v1) an analysis can run under.
	AnalysisSettingsItem ItemKind = "analysis-settings"
	// FindingReviewItem is what a person decided about the findings of one
	// analysis (readmit-finding-decisions/v1), each save a new revision.
	FindingReviewItem ItemKind = "finding-review"
	// AttachmentItem is a file attached to a case. It is named by a
	// reference like any object, and listed only as its case's attachments.
	AttachmentItem ItemKind = "attachment"
)

var itemKinds = []ItemKind{ProjectItem, CaseItem, TestItem, SuiteItem, RunItem, EnvironmentItem, ObservationItem,
	ReportItem, CheckGroupItem, ProfileItem, ScenarioItem, AnalysisItem, VariantItem, BackupItem, RunnerItem, ScheduleItem,
	AnalysisSettingsItem, FindingReviewItem}

// Availability is whether an object's backing can be read now. Readability
// grants nothing: whether an action is permitted is Capabilities.
type Availability string

const (
	// ItemAvailable: its reader accepted it.
	ItemAvailable Availability = "available"
	// ItemMissing: nothing is where it was recorded.
	ItemMissing Availability = "missing"
	// ItemUnreadable: something is there, and its reader refused it or this
	// account cannot read it; Reason says which.
	ItemUnreadable Availability = "unreadable"
	// ItemUnsupported: it declares a contract this release does not read.
	ItemUnsupported Availability = "unsupported"
)

// ActionID names one action a person can take on an object. Capabilities list
// the ones permitted now, as the backend decides it.
type ActionID string

const (
	OpenAction             ActionID = "item.open"
	RenameAction           ActionID = "item.rename"
	LocateAction           ActionID = "item.locate"
	SaveAction             ActionID = "item.save"
	ReplaySendAction       ActionID = "replay.send"
	ExportPacketAction     ActionID = "export.derived-packet"
	ApprovePromotionAction ActionID = "suite.approve-promotion"
	// CollectObservationAction reads an observation's source once.
	CollectObservationAction ActionID = "observation.collect"
	// ResetEnvironmentAction runs an environment's reset plan.
	ResetEnvironmentAction ActionID = "environment.reset"
	// ScanSecretsAction scans the project's configuration files for its
	// registered credentials.
	ScanSecretsAction ActionID = "secret.scan"
	// RemoveAction removes a named environment or observation from its
	// project.
	RemoveAction ActionID = "item.remove"
)

// ItemRef names one object: its kind, its stable application identity, and,
// for an object the application saves, the revision meant. The identity is
// the application's own key: it is not a display name, not a file name and
// not an evidence identity.
type ItemRef struct {
	Kind     ItemKind `json:"kind"`
	ID       string   `json:"id"`
	Revision string   `json:"revision,omitzero"`
}

// RequestContext is what a request was asked for: the project folder the
// window has open, the project identity it expects that folder to hold, and
// the window's own request generation. Every result carries it back, so an
// answer that arrives after the window moved on is recognizably stale and a
// folder that now holds a different project is refused.
type RequestContext struct {
	Project    string `json:"project"`
	ProjectID  string `json:"project_id,omitzero"`
	Generation int    `json:"generation"`
}

// CatalogItem is one named object and its current readable state. Dates are
// RFC 3339 and null when unknown: a date is either one the evidence declares
// or one the application recorded when it did the thing itself, never a file
// modification time.
type CatalogItem struct {
	Ref          ItemRef      `json:"ref"`
	Name         string       `json:"name"`
	ProjectID    string       `json:"project_id,omitzero"`
	CreatedAt    *string      `json:"created_at"`
	UpdatedAt    *string      `json:"updated_at"`
	LastOpenedAt *string      `json:"last_opened_at"`
	Availability Availability `json:"availability"`
	Reason       string       `json:"reason,omitzero"`
	Capabilities []ActionID   `json:"capabilities"`
	Summary      ItemSummary  `json:"summary"`
}

// ItemSummary is the one typed summary of an item's kind; every other member
// is null.
type ItemSummary struct {
	Project     *ProjectSummary     `json:"project,omitzero"`
	Case        *CaseSummary        `json:"case,omitzero"`
	Test        *TestSummary        `json:"test,omitzero"`
	Suite       *SuiteSummary       `json:"suite,omitzero"`
	Run         *RunSummary         `json:"run,omitzero"`
	Environment *EnvironmentSummary `json:"environment,omitzero"`
	Observation *ObservationSummary `json:"observation,omitzero"`
	Report      *ReportSummary      `json:"report,omitzero"`
	CheckGroup  *CheckGroupSummary  `json:"check_group,omitzero"`
	Profile     *ProfileSummary     `json:"profile,omitzero"`
	Scenario    *ScenarioSummary    `json:"scenario,omitzero"`
	Analysis    *AnalysisSummary    `json:"analysis,omitzero"`
	Variant     *VariantSummary     `json:"variant,omitzero"`
	Backup      *BackupSummary      `json:"backup,omitzero"`
	Runner      *RunnerSummary      `json:"runner,omitzero"`
	Schedule    *ScheduleSummary    `json:"schedule,omitzero"`
	// AnalysisSettings and FindingReview are the two kinds the Findings
	// view saves.
	AnalysisSettings *AnalysisSettingsSummary `json:"analysis_settings,omitzero"`
	FindingReview    *FindingReviewSummary    `json:"finding_review,omitzero"`
}

// ProjectSummary is a project as the project document declares it. Folder is
// where the project is now, which a person opens it from; Schema is the
// project contract it declares.
type ProjectSummary struct {
	Folder            string   `json:"folder"`
	Schema            string   `json:"schema"`
	Cases             int      `json:"cases"`
	InterfaceVersions []string `json:"interface_versions"`
	// Owner is the project's default owner and Tags its own tags.
	Owner string   `json:"owner,omitzero"`
	Tags  []string `json:"tags"`
	// Revisions are the declared interface revisions with the names people
	// gave them, in declared order.
	Revisions []InterfaceRevision `json:"revisions"`
}

// InterfaceRevision is one declared interface revision: its identity, which
// every case assigned to it records and which never changes, the name a
// person gave it, and whether new cases are assigned to it by default.
type InterfaceRevision struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// CaseSummary is a case's investigation state as the project records it,
// beside what verifying its evidence found. A case the project has not
// registered has no status or owner. Entry is the project entry that holds
// the case's evidence, the name the case readers take; it is never a path.
// InterfaceRevision is the identity of the revision assigned, the same value
// as InterfaceVersion. Provenance is a marker, present only for a case whose
// evidence is not a person's own: synthetic (generated) or variant (derived).
type CaseSummary struct {
	Registered        bool           `json:"registered"`
	Entry             string         `json:"entry"`
	Status            project.Status `json:"status,omitzero"`
	Owner             string         `json:"owner,omitzero"`
	Tags              []string       `json:"tags"`
	Incidents         []string       `json:"incidents"`
	InterfaceVersion  string         `json:"interface_version,omitzero"`
	InterfaceRevision string         `json:"interface_revision,omitzero"`
	Evidence          string         `json:"evidence"`
	Provenance        string         `json:"provenance,omitzero"`
}

// provenanceMarker is the one provenance a case row shows: synthetic for
// generated evidence, variant for derived, and nothing for a person's own.
func provenanceMarker(mode string) string {
	switch mode {
	case string(bundle.Generated):
		return "synthetic"
	case project.DerivedProvenance:
		return "variant"
	}
	return ""
}

// TestSummary is a test's source case, its current version and the latest
// run whose retained test is exactly this one: the status its result records
// (pass, assertion_failure or execution_error) and when it started. A test
// no run has executed has none, never a manufactured result. Boundary is the
// outcome boundary the test decides at, Tags the tags a person gave it, and
// Entry the project entry its current revision's spec is, which a run names.
type TestSummary struct {
	SourceCase     *ItemRef          `json:"source_case"`
	CurrentVersion string            `json:"current_version,omitzero"`
	LatestRun      *ItemRef          `json:"latest_run"`
	LatestResult   testrunner.Status `json:"latest_result,omitzero"`
	LatestRunAt    *string           `json:"latest_run_at,omitzero"`
	Assertions     int               `json:"assertions"`
	Boundary       string            `json:"boundary,omitzero"`
	Tags           []string          `json:"tags,omitzero"`
	Entry          string            `json:"entry,omitzero"`
}

// SuiteSummary is how many tests a suite includes, the environments it binds
// and the latest retained execution of its current version: when it started
// and its outcome, as RunSummary.Outcome says it. A suite whose current
// version no execution ran has none; an execution of an earlier version is
// not one of this version's.
type SuiteSummary struct {
	Tests         int      `json:"tests"`
	Environments  []string `json:"environments"`
	LatestRun     *ItemRef `json:"latest_run"`
	LatestRunAt   *string  `json:"latest_run_at,omitzero"`
	LatestOutcome string   `json:"latest_outcome,omitzero"`
}

// RunSummary is what one run actually did: the address it reached, when it
// started and finished, its outcome, and how many deliveries no
// acknowledgement settled. A test run also names the boundary it was
// observed at and the project's case whose evidence it replayed, when the
// project holds it.
//
// A suite execution names its suite, when the project holds it, and how many
// jobs its queue held; it reaches several targets, so Target is empty, and
// Uncertain counts its jobs with a delivery no acknowledgement settled. Its
// Outcome is executed (the queue executed every job, whatever each found),
// stopped (the queue stopped before executing every job) or incomplete (the
// execution retained no queue report: it was interrupted). Its start and
// completion are the earliest and latest its jobs' runs record.
type RunSummary struct {
	Target            string   `json:"target,omitzero"`
	StartedAt         *string  `json:"started_at"`
	CompletedAt       *string  `json:"completed_at"`
	Outcome           string   `json:"outcome,omitzero"`
	Uncertain         int      `json:"uncertain"`
	DeliveryUncertain bool     `json:"delivery_uncertain"`
	Boundary          string   `json:"boundary,omitzero"`
	SourceCase        *ItemRef `json:"source_case,omitzero"`
	Suite             *ItemRef `json:"suite,omitzero"`
	Jobs              int      `json:"jobs,omitzero"`
}

// EnvironmentSummary is an environment's declared classification, address
// and transport, and the latest explicit check the application retained for
// it: when it ran, the outcome it reported, and the revision it checked,
// which is older than the current one after a later save. An environment
// never checked has no check. Observation is the observation its links
// name, with that observation's name, and ResetName the name its reset was
// given.
type EnvironmentSummary struct {
	Classification    string   `json:"classification"`
	Address           string   `json:"address"`
	Transport         string   `json:"transport"`
	LastCheckedAt     *string  `json:"last_checked_at"`
	LastCheckOutcome  string   `json:"last_check_outcome,omitzero"`
	LastCheckRevision string   `json:"last_check_revision,omitzero"`
	Observation       *ItemRef `json:"observation"`
	ObservationName   string   `json:"observation_name,omitzero"`
	HasPolicy         bool     `json:"has_policy"`
	ResetName         string   `json:"reset_name,omitzero"`
	ResetActions      int      `json:"reset_actions"`
}

// ObservationSummary is an observation source's type and its latest completed
// collection among the project's retained completion records.
type ObservationSummary struct {
	SourceType       string  `json:"source_type"`
	Enabled          bool    `json:"enabled"`
	LatestCollection *string `json:"latest_collection"`
	LatestStatus     string  `json:"latest_status,omitzero"`
}

// ReportSummary is the case a report is about, when the project holds it,
// and the form and state of the report.
type ReportSummary struct {
	Form        string   `json:"form"`
	RelatedCase *ItemRef `json:"related_case"`
	Status      string   `json:"status,omitzero"`
}

type CheckGroupSummary struct {
	Assertions int `json:"assertions"`
}

// ProfileSummary is the family and protocol version a profile covers and the
// version it publishes.
type ProfileSummary struct {
	Form             string `json:"form"`
	Family           string `json:"family,omitzero"`
	ProtocolVersion  string `json:"protocol_version,omitzero"`
	PublishedVersion string `json:"published_version,omitzero"`
}

type ScenarioSummary struct {
	Version string `json:"version"`
	Profile string `json:"profile"`
}

// AnalysisSummary is one retained analysis. A diagnosis ("diagnosis") names
// the case it was run over, the verified identity of that case, the profile,
// ruleset and configuration identity it ran under, the name a person reads for
// that configuration (empty when the project offers it under no name), how
// many findings it made and how many items it could not evaluate. A grouping ("grouping") names
// every case of the project it compared, in Cases; a sequence analysis
// ("sequence-analysis") names its case alone.
type AnalysisSummary struct {
	Form         string    `json:"form"`
	RelatedCase  *ItemRef  `json:"related_case"`
	Cases        []ItemRef `json:"cases"`
	Findings     int       `json:"findings"`
	CaseIdentity string    `json:"case_identity,omitzero"`
	Profile      string    `json:"profile,omitzero"`
	ProfileName  string    `json:"profile_name"`
	Ruleset      string    `json:"ruleset,omitzero"`
	ConfigSHA256 string    `json:"config_sha256,omitzero"`
	Unsupported  int       `json:"unsupported"`
}

type VariantSummary struct {
	Form      string   `json:"form"`
	Parent    *ItemRef `json:"parent"`
	Operation string   `json:"operation,omitzero"`
}

type BackupSummary struct {
	Complete bool `json:"complete"`
	Evidence int  `json:"evidence"`
}

type RunnerSummary struct {
	Environment string `json:"environment"`
}

type ScheduleSummary struct {
	Schedules int `json:"schedules"`
}

// CatalogSort is the order a page is listed in. Ties are broken by identity.
type CatalogSort string

const (
	SortByName    CatalogSort = "name"
	SortByUpdated CatalogSort = "updated"
	SortByCreated CatalogSort = "created"
)

// CatalogFilter narrows a list to typed values. A member left empty filters
// nothing.
// RelatedCase keeps the objects about one case: its analyses, the groupings
// that compared it and the reports about it.
type CatalogFilter struct {
	Availability []Availability   `json:"availability,omitzero"`
	Status       []project.Status `json:"status,omitzero"`
	Owner        string           `json:"owner,omitzero"`
	RelatedCase  *ItemRef         `json:"related_case,omitzero"`
}

// CatalogQuery is one page of one kind. Name matches display names only,
// case-insensitively; it is never matched against evidence.
type CatalogQuery struct {
	Context RequestContext `json:"context"`
	Kind    ItemKind       `json:"kind"`
	Name    string         `json:"name,omitzero"`
	Filter  CatalogFilter  `json:"filter"`
	Sort    CatalogSort    `json:"sort,omitzero"`
	Cursor  string         `json:"cursor,omitzero"`
	Limit   int            `json:"limit,omitzero"`
}

// CatalogPageSize is the default page, and the largest a query may ask for.
const CatalogPageSize = 200

// IncompleteSave is one save an interruption left unpublished. The object's
// previous revision is current; Operation retries or discards it.
type IncompleteSave struct {
	Operation string   `json:"operation"`
	Item      *ItemRef `json:"item"`
	Name      string   `json:"name,omitzero"`
	Reason    string   `json:"reason"`
}

// CatalogPage is one page of a list. Snapshot names the list the page was cut
// from; a cursor continues only that list. Recorded is false when the
// project's catalog could not be written, so the identities listed are the
// derived ones rather than recorded ones.
//
// A project folder with more entries than one discovery window reads is
// listed window by window: while a later window is still to be read, Partial
// is true, Reason says so and Total is null, never the count of what was read
// so far. NextCursor continues into the next window once this one's pages
// are listed, and the last window's pages carry the whole list's Total.
type CatalogPage struct {
	Items      []CatalogItem    `json:"items"`
	NextCursor string           `json:"next_cursor,omitzero"`
	Total      *int             `json:"total"`
	Partial    bool             `json:"partial,omitzero"`
	Reason     string           `json:"reason,omitzero"`
	Snapshot   string           `json:"snapshot"`
	Recorded   bool             `json:"recorded"`
	Incomplete []IncompleteSave `json:"incomplete"`
}

// windowReason is why a page of a project read window by window is partial.
const windowReason = "the project holds more entries than this release lists at once; later entries are listed on the next pages"

// CatalogResult carries one state and the context it answers.
type CatalogResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Page    *CatalogPage   `json:"page,omitzero"`
}

func (r *CatalogResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ListCatalog lists one kind of object of the open project, or, for the
// project kind, the projects this viewer has opened. Every object is read
// through its own reader now; one that is missing, unreadable or unsupported
// stays a named row with its reason. It is a read: it writes nothing into
// the project, and an object the catalog has not recorded yet is listed
// under the identity recording it will keep.
//
// The first page is cut from a snapshot of the whole ordered list, which
// this process holds for a while; NextCursor continues exactly that
// snapshot, whatever changed on disk since. A cursor whose snapshot is no
// longer held is refused, and the list is read again from its start.
func (a *App) ListCatalog(query CatalogQuery) CatalogResult {
	return run(a, false, false, func(ctx context.Context) CatalogResult {
		result := CatalogResult{Context: query.Context}
		if !slices.Contains(itemKinds, query.Kind) {
			result.refuse(Failed, "the catalog lists one of the object kinds this release names")
			return result
		}
		limit := query.Limit
		if limit <= 0 || limit > CatalogPageSize {
			limit = CatalogPageSize
		}
		var list *heldList
		start := 0
		if query.Cursor != "" {
			cursor, ok := decodeCursor(query.Cursor)
			if ok && cursor.After != "" {
				// The next discovery window of the list the cursor ended.
				if cursor.Key != keyDigest(queryKey(query)) {
					ok = false
				} else {
					read, declined := a.readList(ctx, query, cursor.After, cursor.Prior)
					if read == nil {
						result.refuse(declined.state, declined.reason)
						return result
					}
					list = read
				}
			} else if ok {
				held := a.snapshots.held(cursor.Snapshot, queryKey(query))
				if held == nil || cursor.Offset > len(held.items) {
					ok = false
				}
				list, start = held, cursor.Offset
			}
			if !ok {
				result.refuse(Failed, "the list this page continues is no longer held; read it again from its first page")
				return result
			}
		} else {
			read, declined := a.readList(ctx, query, "", 0)
			if read == nil {
				result.refuse(declined.state, declined.reason)
				return result
			}
			list = read
		}
		page := &CatalogPage{Snapshot: list.snapshot, Recorded: list.recorded, Incomplete: list.incomplete}
		if list.upper == "" {
			total := list.prior + len(list.items)
			page.Total = &total
		} else {
			page.Partial, page.Reason = true, windowReason
		}
		end := min(start+limit, len(list.items))
		// The objects are the snapshot's; what may be done to them is what
		// the guard admits now.
		admitted := a.admitted(ctx)
		page.Items = make([]CatalogItem, 0, end-start)
		for _, item := range list.items[start:end] {
			item.Capabilities = capabilitiesFor(item, admitted)
			page.Items = append(page.Items, item)
		}
		switch {
		case end < len(list.items):
			page.NextCursor = encodeCursor(listCursor{Snapshot: list.snapshot, Offset: end})
		case list.upper != "":
			page.NextCursor = encodeCursor(listCursor{After: list.upper, Prior: list.prior + len(list.items), Key: keyDigest(list.key)})
		}
		result.State, result.Page = Completed, page
		if len(list.items) == 0 && list.prior == 0 && list.upper == "" {
			result.State = Empty
		}
		return result
	})
}

// readList reads, filters and orders one whole list — of the project's
// discovery window after the entry name after, which prior objects of the
// list preceded — and holds it as a snapshot.
func (a *App) readList(ctx context.Context, query CatalogQuery, after string, prior int) (*heldList, refusal) {
	list := &heldList{recorded: true, incomplete: []IncompleteSave{}, key: queryKey(query), prior: prior}
	var items []CatalogItem
	if query.Kind == ProjectItem {
		items = a.knownProjects(ctx)
	} else {
		loaded, declined := a.loadWindow(ctx, query.Context, false, after)
		if loaded == nil {
			return nil, declined
		}
		items = loaded.list(query.Kind)
		list.recorded, list.incomplete, list.upper = loaded.recorded, loaded.incompleteSaves(), loaded.upper
	}
	items = filtered(items, query)
	sortItems(items, query.Sort)
	list.items, list.snapshot = items, snapshotOf(items)
	a.snapshots.hold(list, a.now())
	return list, refusal{}
}

// snapshotLifetime is how long a list snapshot is held for its later pages,
// and heldSnapshots how many are held at once.
const (
	snapshotLifetime = 10 * time.Minute
	heldSnapshots    = 16
)

// heldList is one ordered list a first page was cut from: of one discovery
// window, which prior objects of the list preceded, and which more entries
// follow when upper, the last entry name it covers, is set.
type heldList struct {
	key        string
	snapshot   string
	items      []CatalogItem
	recorded   bool
	incomplete []IncompleteSave
	prior      int
	upper      string
	at         time.Time
}

// snapshotStore holds the lists later pages continue, in this process only.
type snapshotStore struct {
	mu    sync.Mutex
	lists []*heldList
}

func (s *snapshotStore) hold(list *heldList, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list.at = now
	s.lists = slices.DeleteFunc(s.lists, func(held *heldList) bool {
		return now.Sub(held.at) > snapshotLifetime || held.snapshot == list.snapshot && held.key == list.key
	})
	s.lists = append(s.lists, list)
	if len(s.lists) > heldSnapshots {
		s.lists = s.lists[len(s.lists)-heldSnapshots:]
	}
}

func (s *snapshotStore) held(snapshot, key string) *heldList {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, list := range s.lists {
		if list.snapshot == snapshot && list.key == key {
			return list
		}
	}
	return nil
}

// queryKey names what a list was asked for, apart from its page.
func queryKey(query CatalogQuery) string {
	page := query
	page.Cursor, page.Limit, page.Context.Generation = "", 0, 0
	data, _ := json.Marshal(page, json.Deterministic(true))
	return string(data)
}

// ItemRequest names one object of the open project. From is where a new
// test starts from, read by OpenItemDraft alone.
type ItemRequest struct {
	Context RequestContext `json:"context"`
	Ref     ItemRef        `json:"ref"`
	From    *TestOrigin    `json:"from,omitzero"`
}

// ItemResult carries one object as it reads now, and the context it answers.
type ItemResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Item    *CatalogItem   `json:"item,omitzero"`
}

func (r *ItemResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// OpenItem reads one object now, through its reader. An object that is
// missing or unreadable is answered as that, with its reason; opening it
// verifies nothing more than listing it did, and writes nothing into the
// project. It is the one explicit open: this viewer's record of when it last
// opened the object (readmit-desktop-opened/v1) is written here, and the
// catalog reads it back as the object's last_opened_at.
func (a *App) OpenItem(request ItemRequest) ItemResult {
	return run(a, false, false, func(ctx context.Context) ItemResult {
		result := a.reread(ctx, request.Context, request.Ref)
		if result.Item != nil && request.Ref.Kind != ProjectItem {
			if stamp := a.recordOpened(result.Item.ProjectID, result.Item.Ref.ID); stamp != "" {
				result.Item.LastOpenedAt = &stamp
			}
		}
		return result
	})
}

// RenameRequest gives one object a new display name. Nothing but the name
// changes: no backing file is renamed or rewritten.
type RenameRequest struct {
	Context RequestContext `json:"context"`
	Ref     ItemRef        `json:"ref"`
	Name    string         `json:"name"`
}

// RenameItem changes one object's display name. A project's name is the
// project document's title and a registered case's name its recorded title;
// every other object's name is catalog metadata. Evidence is never touched,
// and two objects may share a name.
func (a *App) RenameItem(request RenameRequest) ItemResult {
	return run(a, false, true, func(ctx context.Context) ItemResult {
		result := ItemResult{Context: request.Context}
		name := strings.TrimSpace(request.Name)
		if !catalog.ValidName(name) {
			result.refuse(Failed, nameRule)
			return result
		}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Ref, true)
		if loaded == nil {
			return refused
		}
		root := loaded.root
		switch registered := loaded.registeredCase(item.Ref.ID); {
		case request.Ref.Kind == ProjectItem:
			if _, err := operation.UpdateProjectSettings(root, operation.SettingsChange{Title: &name}); err != nil {
				overview := refusedOverview(root, err)
				result.refuse(overview.State, overview.Reason)
				return result
			}
			a.rememberProject(loaded.document.Project.ID, root, name)
		case registered != nil:
			if _, err := operation.UpdateRegisteredCase(root, registered.Name, operation.CaseChange{Title: &name}); err != nil {
				overview := refusedOverview(root, err)
				result.refuse(overview.State, overview.Reason)
				return result
			}
		default:
			if _, err := loaded.store.Update(a.now(), func(document *catalog.Document) (bool, error) {
				index := document.Find(item.Ref.ID)
				if index < 0 {
					return false, catalog.ErrNoItem
				}
				document.Items[index].Name = name
				return true, nil
			}); err != nil {
				result.refuse(Failed, "the name could not be recorded in the project's catalog")
				return result
			}
		}
		return a.reread(ctx, request.Context, request.Ref)
	})
}

// LocateRequest names where an object whose recorded place is missing is
// now: the project entry that holds it, or, for a project, the folder it was
// moved to. Left empty, the host's folder dialog asks for it.
type LocateRequest struct {
	Context RequestContext `json:"context"`
	Ref     ItemRef        `json:"ref"`
	Entry   string         `json:"entry,omitzero"`
	Folder  string         `json:"folder,omitzero"`
}

// LocateItem associates an object that is missing with the place that holds
// it now, once that place reads as the same object: a project folder whose
// catalog records the same project identity; an entry that reads as the
// same kind of object and, for a case, as the very evidence the project
// recorded. Nothing is moved, copied or rewritten; the catalog, or for a
// project the viewer's remembered projects, records where it is.
//
// A request that names no place asks the host's folder dialog for it —
// "Locate project" or "Locate case" — once the object is known to be
// missing; a dismissed dialog changes nothing. A case's folder is one entry
// of the project's folder.
func (a *App) LocateItem(request LocateRequest) ItemResult {
	return run(a, false, true, func(ctx context.Context) ItemResult {
		result := ItemResult{Context: request.Context}
		if request.Ref.Kind == ProjectItem {
			if request.Folder == "" {
				folder, declined := a.chooseFolder(ctx, "Locate project")
				if folder == "" {
					result.refuse(declined.state, declined.reason)
					return result
				}
				request.Folder = folder
			}
			return a.locateProject(ctx, request)
		}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Ref, true)
		if loaded == nil {
			return refused
		}
		if item.Availability != ItemMissing {
			result.refuse(Failed, "only an object that is missing is located")
			return result
		}
		if request.Entry == "" {
			title := "Locate " + strings.ReplaceAll(string(request.Ref.Kind), "-", " ")
			if request.Ref.Kind == VariantItem {
				title = "Locate case"
			}
			folder, declined := a.chooseFolder(ctx, title)
			if folder == "" {
				result.refuse(declined.state, declined.reason)
				return result
			}
			parent, err := filepath.EvalSymlinks(filepath.Dir(filepath.Clean(folder)))
			if err != nil || parent != loaded.root {
				result.refuse(Failed, "the folder chosen is not an entry of this project's folder")
				return result
			}
			request.Entry = filepath.Base(filepath.Clean(folder))
		}
		if artifactpath.EntryName(request.Entry) != nil {
			result.refuse(Failed, "an object is located at one entry of the project")
			return result
		}
		if held := loaded.document.ByEntry(string(request.Ref.Kind), request.Entry); held >= 0 && loaded.document.Items[held].ID != request.Ref.ID {
			// The entry was discovered on its own while the object was
			// missing: the discovered association gives way to the recorded
			// object it turns out to be.
			if len(loaded.document.Items[held].Revisions) > 0 {
				result.refuse(Failed, "that entry is another object of this project")
				return result
			}
		}
		candidate := loaded.read(catalog.Item{Kind: string(request.Ref.Kind), ID: request.Ref.ID, Entry: request.Entry})
		if candidate.Availability != ItemAvailable {
			result.refuse(Failed, "that entry does not read as this kind of object")
			return result
		}
		if (request.Ref.Kind == CaseItem || request.Ref.Kind == VariantItem) && !loaded.sameEvidence(item.Ref.ID, request.Entry) {
			result.refuse(Failed, "that entry is not the evidence the project recorded for this object")
			return result
		}
		if _, err := loaded.store.Update(a.now(), func(document *catalog.Document) (bool, error) {
			if held := document.ByEntry(string(request.Ref.Kind), request.Entry); held >= 0 && document.Items[held].ID != request.Ref.ID {
				document.Items = slices.Delete(document.Items, held, held+1)
			}
			index := document.Find(request.Ref.ID)
			if index < 0 {
				return false, catalog.ErrNoItem
			}
			document.Items[index].Entry = request.Entry
			return true, nil
		}); err != nil {
			result.refuse(Failed, "the location could not be recorded in the project's catalog")
			return result
		}
		return a.reread(ctx, request.Context, request.Ref)
	})
}

// locateProject finds a moved project in the folder named, by the identity
// its catalog records.
func (a *App) locateProject(ctx context.Context, request LocateRequest) ItemResult {
	result := ItemResult{Context: request.Context}
	loaded, declined := a.loadCatalog(ctx, RequestContext{Project: request.Folder, ProjectID: request.Ref.ID}, false)
	if loaded == nil {
		result.refuse(declined.state, "that folder does not hold this project: "+declined.reason)
		return result
	}
	a.rememberProject(request.Ref.ID, loaded.root, loaded.project.Document.Settings.Title)
	item := projectItem(loaded)
	result.State, result.Item = Completed, &item
	return result
}

// reread answers one object as it reads now.
func (a *App) reread(ctx context.Context, context RequestContext, ref ItemRef) ItemResult {
	loaded, item, result := a.catalogItem(ctx, context, ref, false)
	if loaded == nil {
		return result
	}
	result.State, result.Item = Completed, item
	return result
}

// catalogItem loads the project's catalog, recording it when record is set,
// and reads the one object named.
func (a *App) catalogItem(ctx context.Context, request RequestContext, ref ItemRef, record bool) (*loadedCatalog, *CatalogItem, ItemResult) {
	result := ItemResult{Context: request}
	if ref.Kind == ProjectItem {
		folder := a.rememberedFolder(ref.ID)
		if folder == "" {
			result.refuse(Failed, "this viewer has not opened that project")
			return nil, nil, result
		}
		loaded, declined := a.loadCatalog(ctx, RequestContext{Project: folder, ProjectID: ref.ID, Generation: request.Generation}, record)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return nil, nil, result
		}
		item := projectItem(loaded)
		return loaded, &item, result
	}
	loaded, declined := a.loadCatalog(ctx, request, record)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return nil, nil, result
	}
	index := loaded.document.Find(ref.ID)
	if loaded, index, declined = a.findAcross(ctx, request, loaded, ref.ID, record); loaded == nil {
		result.refuse(declined.state, declined.reason)
		return nil, nil, result
	}
	if index < 0 || loaded.document.Items[index].Kind != string(ref.Kind) || loaded.removed(loaded.document.Items[index]) {
		result.refuse(Failed, "the project holds no such object")
		return nil, nil, result
	}
	item := loaded.read(loaded.document.Items[index])
	return loaded, &item, result
}

// findAcross finds the object id in the discovery window loaded covers or a
// later one, recording each window it loads when record is set, and answers
// the window that holds it and its index there, -1 when none does.
func (a *App) findAcross(ctx context.Context, request RequestContext, loaded *loadedCatalog, id string, record bool) (*loadedCatalog, int, refusal) {
	index := loaded.document.Find(id)
	for index < 0 && loaded.upper != "" {
		next, declined := a.loadWindow(ctx, request, record, loaded.upper)
		if next == nil {
			return nil, -1, declined
		}
		loaded, index = next, next.document.Find(id)
	}
	return loaded, index, refusal{}
}

// now is the facade's clock.
func (a *App) now() time.Time {
	if a.clock != nil {
		return a.clock()
	}
	return time.Now()
}

func filtered(items []CatalogItem, query CatalogQuery) []CatalogItem {
	name := strings.ToLower(strings.TrimSpace(query.Name))
	return slices.DeleteFunc(slices.Clone(items), func(item CatalogItem) bool {
		if name != "" && !strings.Contains(strings.ToLower(item.Name), name) {
			return true
		}
		if len(query.Filter.Availability) > 0 && !slices.Contains(query.Filter.Availability, item.Availability) {
			return true
		}
		if len(query.Filter.Status) > 0 && (item.Summary.Case == nil || !slices.Contains(query.Filter.Status, item.Summary.Case.Status)) {
			return true
		}
		if query.Filter.Owner != "" && (item.Summary.Case == nil || item.Summary.Case.Owner != query.Filter.Owner) {
			return true
		}
		if query.Filter.RelatedCase != nil && !relatedTo(item, query.Filter.RelatedCase.ID) {
			return true
		}
		return false
	})
}

// relatedTo reports whether an object is about the case with identity id.
func relatedTo(item CatalogItem, id string) bool {
	var related []ItemRef
	if analysis := item.Summary.Analysis; analysis != nil {
		related = append(related, analysis.Cases...)
		if analysis.RelatedCase != nil {
			related = append(related, *analysis.RelatedCase)
		}
	}
	if report := item.Summary.Report; report != nil && report.RelatedCase != nil {
		related = append(related, *report.RelatedCase)
	}
	return slices.ContainsFunc(related, func(ref ItemRef) bool { return ref.ID == id })
}

// sortItems orders a list; a date nobody knows sorts after every known one,
// and the identity breaks every tie.
func sortItems(items []CatalogItem, by CatalogSort) {
	date := func(value *string) string {
		if value == nil {
			return ""
		}
		return *value
	}
	slices.SortStableFunc(items, func(x, y CatalogItem) int {
		var order int
		switch by {
		case SortByUpdated:
			order = cmp.Compare(date(y.UpdatedAt), date(x.UpdatedAt))
		case SortByCreated:
			order = cmp.Compare(date(y.CreatedAt), date(x.CreatedAt))
		default:
			order = cmp.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
		}
		if order != 0 {
			return order
		}
		return cmp.Compare(x.Ref.ID, y.Ref.ID)
	})
}

// snapshotOf names a whole ordered list: the objects, their revisions, their
// availability and their names, so a cursor never continues a different list.
func snapshotOf(items []CatalogItem) string {
	digest := sha256.New()
	for _, item := range items {
		digest.Write([]byte(string(item.Ref.Kind) + "\x00" + item.Ref.ID + "\x00" + item.Ref.Revision + "\x00" +
			string(item.Availability) + "\x00" + item.Name + "\x01"))
	}
	return hex.EncodeToString(digest.Sum(nil))[:16]
}

// listCursor is where a later page begins: at Offset of a held snapshot, or,
// once a window's pages are listed, at the discovery window after the entry
// name After, which Prior objects of the list preceded. Key names the list
// such a cursor continues.
type listCursor struct {
	Snapshot string `json:"s,omitzero"`
	Offset   int    `json:"o,omitzero"`
	After    string `json:"a,omitzero"`
	Prior    int    `json:"p,omitzero"`
	Key      string `json:"k,omitzero"`
}

func encodeCursor(cursor listCursor) string {
	data, _ := json.Marshal(cursor, json.Deterministic(true))
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursor(encoded string) (listCursor, bool) {
	var cursor listCursor
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || json.Unmarshal(raw, &cursor, json.RejectUnknownMembers(true)) != nil || cursor.Offset < 0 || cursor.Prior < 0 {
		return listCursor{}, false
	}
	return cursor, cursor.After != "" || cursor.Snapshot != ""
}

// keyDigest names a list's query in a cursor without carrying it.
func keyDigest(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:16]
}

var errForeignProject = errors.New("the folder holds a different project than the one the window opened")
