package desktop

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/assertionauthor"
	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/lifecycle"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/profilepackage"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/scenario"
	"github.com/bharm16/readmit/internal/scenariogen"
	"github.com/bharm16/readmit/internal/sequenceanalysis"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
	"github.com/bharm16/readmit/internal/transform"
)

// loadedCatalog is one project read for its catalog: the project documents,
// the catalog as recorded (with every discovered object associated), the
// saves recovery left incomplete, and the backend's admission decisions the
// capabilities are drawn from. Companion reads a summary needs — the runs a
// test's latest run is found among, the completion records an observation's
// latest collection is found among, the identities of the project's cases —
// are made at most once per load.
type loadedCatalog struct {
	ctx        context.Context
	root       string
	project    *project.Project
	revisions  project.Revisions
	store      *catalog.Store
	document   catalog.Document
	recorded   bool
	incomplete []catalog.Incomplete
	// opened is when this viewer last opened each object, by identity.
	opened map[string]string
	// uncatalogued are the objects a read lists that the catalog has no room
	// to record, by identity: they are opened and changed nowhere.
	uncatalogued map[string]bool
	// lower and upper are the entry names this load's discovery window
	// covers: every name after lower up to upper, and past it when upper is
	// empty, which is the last window.
	lower, upper string

	author, execute bool

	// origins are the recorded sources of generated objects, by identity,
	// read once when a case is first read.
	origins     map[string]catalog.Origin
	originsRead bool

	runViews    []runView
	runsRead    bool
	suiteRuns   []suiteRunView
	suitesRead  bool
	suiteViews  []suiteView
	completions []observewindow.Completion
	doneRead    bool
	identities  map[string]string
	packets     *relations
	// private are the project's private local-state folders by the
	// commitment an export review records for each, read once per load.
	private    map[string][]privateState
	privateErr error
	// analyses are the retained diagnoses read during this load, by entry.
	analyses map[string]diagnose.Retained
	// configNames are the names a person reads for the configurations the
	// project offers now, by configuration identity, read once per load.
	configNames map[string]string
}

// loadCatalog opens the project the request names and discovers what it
// holds through each object's own recognizer. A read (record false) writes
// nothing: an object the catalog has not recorded yet is listed under the
// identity derived from its kind and entry, which is the identity recording
// it keeps, and an interrupted save is reported, not settled. A write (record
// true), made only under the author admission, settles interrupted saves and
// records every new association first.
func (a *App) loadCatalog(ctx context.Context, request RequestContext, record bool) (*loadedCatalog, refusal) {
	return a.loadWindow(ctx, request, record, "")
}

// loadWindow is loadCatalog over the discovery window after the entry name
// lower: the project's first window when lower is empty. A project folder
// holds more entries than one load reads only past catalog.MaxItems; each
// later window is read by its own load, and nothing is recorded from it.
func (a *App) loadWindow(ctx context.Context, request RequestContext, record bool, lower string) (*loadedCatalog, refusal) {
	opened, declined := openProjectFolder(request.Project)
	if opened == nil {
		return nil, declined
	}
	revisions, declined := readRevisions(opened.Root)
	if revisions == nil {
		return nil, declined
	}
	store, err := catalog.Open(opened.Root)
	if err != nil {
		return nil, refusal{Failed, "a project must be an existing folder that is not a symbolic link"}
	}
	loaded := &loadedCatalog{ctx: ctx, root: opened.Root, project: opened, revisions: *revisions, store: store, identities: map[string]string{}, analyses: map[string]diagnose.Retained{}, uncatalogued: map[string]bool{}}
	document, present, err := store.Read()
	if errors.Is(err, catalog.ErrUnsupportedVersion) {
		return nil, refusal{Failed, "the project's catalog was written by a version this release cannot read"}
	}
	if err != nil {
		return nil, refusal{Failed, "the project's catalog cannot be read; it is left exactly as written"}
	}
	if request.ProjectID != "" && (!present || document.Project.ID != request.ProjectID) {
		return nil, refusal{Failed, errForeignProject.Error()}
	}
	verifiers := func(kind string) catalog.Verifier { return verifierFor(ItemKind(kind)) }
	if record {
		loaded.incomplete, err = store.Recover(verifiers, catalog.Options{Now: a.now, Associate: associateEntry(opened.Root)})
	} else {
		loaded.incomplete, err = store.Inspect(verifiers)
	}
	if err != nil {
		return nil, refusal{Failed, "an interrupted save of this project cannot be read"}
	}
	discovered, upper, declined := discover(ctx, opened.Root, *revisions, &opened.Document, lower)
	if declined.state != "" {
		return nil, declined
	}
	loaded.lower, loaded.upper = lower, upper
	staged := store.PendingFiles()
	discovered = slices.DeleteFunc(discovered, func(entry found) bool { return slices.Contains(staged, entry.entry) })
	if record {
		loaded.document, err = store.Update(a.now(), func(document *catalog.Document) (bool, error) {
			return associated(document, discovered, catalog.MaxItems), nil
		})
		if err != nil {
			return nil, probeWriteFailure(opened.Root,
				"this account cannot write the project's catalog",
				"the project's catalog could not be recorded; it is left as it was")
		}
		present = true
	} else {
		if !present {
			document = catalog.Document{Schema: catalog.Schema, Items: []catalog.Item{}}
		}
		// Every discovered object is listed; the ones past what the catalog
		// records are named, exactly as recording would leave them.
		recorded := len(document.Items)
		associated(&document, discovered, -1)
		for i := max(recorded, catalog.MaxItems); i < len(document.Items); i++ {
			loaded.uncatalogued[document.Items[i].ID] = true
		}
		loaded.document = document
	}
	loaded.recorded = present
	loaded.opened = a.openedIn(loaded.document.Project.ID)
	if present {
		a.rememberProject(loaded.document.Project.ID, opened.Root, opened.Document.Settings.Title)
	}
	now := a.admitted(ctx)
	loaded.author, loaded.execute = now.author, now.execute
	return loaded, refusal{}
}

// found is one project entry and the kind of object it declares.
type found struct {
	kind  ItemKind
	entry string
}

// discover lists the project's entries in one discovery window and names the
// kind each declares, by the listing's own recognizers and the declared
// contract; it verifies nothing. Registered cases and revisions are named
// whether or not their entry is still there, so a moved one stays a visible,
// missing object.
//
// A window is the first catalog.MaxItems entry names after lower, in name
// order. The folder is read in bounded chunks, and no more names than two
// windows are held at once. upper is the last name of a window more entries
// follow, and empty for the last window.
func discover(ctx context.Context, root string, revisions project.Revisions, document *project.Document, lower string) ([]found, string, refusal) {
	names, upper, declined := window(ctx, root, lower)
	if declined.state != "" {
		return nil, "", declined
	}
	var discovered []found
	for _, registered := range document.Cases {
		if inWindow(registered.Name, lower, upper) {
			discovered = append(discovered, found{CaseItem, registered.Name})
		}
	}
	for _, registered := range revisions.Revisions {
		if inWindow(registered.Name, lower, upper) {
			discovered = append(discovered, found{VariantItem, registered.Name})
		}
	}
	for _, name := range names {
		if ctx.Err() != nil {
			return nil, "", cancelledRefusal
		}
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil {
			continue
		}
		if kind, ok := entryKind(root, fs.FileInfoToDirEntry(info)); ok && !slices.Contains(discovered, found{kind, name}) {
			discovered = append(discovered, found{kind, name})
		}
	}
	return discovered, upper, refusal{}
}

// window reads the entry names of one discovery window.
func window(ctx context.Context, root, lower string) ([]string, string, refusal) {
	folder, err := os.Open(root)
	switch {
	case errors.Is(err, fs.ErrPermission):
		return nil, "", refusal{PermissionDenied, "this account cannot read the project folder"}
	case err != nil:
		return nil, "", refusal{Failed, "the project folder cannot be read"}
	}
	defer folder.Close()
	var names []string
	keep := func() {
		slices.Sort(names)
		if len(names) > catalog.MaxItems+1 {
			names = names[:catalog.MaxItems+1]
		}
	}
	for {
		if ctx.Err() != nil {
			return nil, "", cancelledRefusal
		}
		chunk, err := folder.Readdirnames(MaxWorkspaceEntries)
		for _, name := range chunk {
			if name > lower {
				names = append(names, name)
			}
		}
		if len(names) > 2*catalog.MaxItems {
			keep()
		}
		if errors.Is(err, io.EOF) || err == nil && len(chunk) == 0 {
			break
		}
		if errors.Is(err, fs.ErrPermission) {
			return nil, "", refusal{PermissionDenied, "this account cannot read the project folder"}
		}
		if err != nil {
			return nil, "", refusal{Failed, "the project folder cannot be read"}
		}
	}
	keep()
	if len(names) > catalog.MaxItems {
		return names[:catalog.MaxItems], names[catalog.MaxItems-1], refusal{}
	}
	return names, "", refusal{}
}

// inWindow reports whether an entry name falls in the window after lower up
// to upper, or past it when upper is empty.
func inWindow(name, lower, upper string) bool {
	return name > lower && (upper == "" || name <= upper)
}

// familyKinds name the object kind a declared contract family belongs to, so
// a file declaring a version this release does not read is still listed as
// that kind of object — unsupported — rather than dropped.
var familyKinds = map[string]ItemKind{
	"readmit-target/":             EnvironmentItem,
	"readmit-test/":               TestItem,
	"readmit-test-release/":       TestItem,
	"readmit-suite/":              SuiteItem,
	"readmit-observation-source/": ObservationItem,
	"readmit-assertion-set/":      CheckGroupItem,
	"readmit-local-profile/":      ProfileItem,
	"readmit-profile-pack/":       ProfileItem,
	"readmit-profile-package/":    ProfileItem,
	"readmit-scenario/":           ScenarioItem,
	"readmit-order-scenario/":     ScenarioItem,
	"readmit-scenario-generator/": ScenarioItem,
	"readmit-sequence-analysis/":  AnalysisItem,
	"readmit-transform-plan/":     VariantItem,
	"readmit-runner/":             RunnerItem,
	"readmit-hub-schedules/":      ScheduleItem,
	"readmit-diagnose-config/":    AnalysisSettingsItem,
}

// entryKind names the kind of object one entry declares. The project's own
// documents, the catalog's area, the files the application saved for an
// object and what a case's deletion could not remove are not objects of their
// own.
func entryKind(root string, entry fs.DirEntry) (ItemKind, bool) {
	name := entry.Name()
	switch {
	case name == catalog.Folder, name == project.DocumentName, name == project.RevisionsDocumentName, name == project.QuotaDocumentName,
		name == project.RecoveryRecordName:
		return "", false
	case strings.Contains(name, ".recovery-"), strings.HasSuffix(name, ".incomplete"), strings.HasPrefix(name, lifecycle.CaseRemainderPrefix):
		return "", false
	}
	artifact := describe(root, entry)
	switch artifact.Kind {
	case CaseArtifact:
		if artifact.Provenance == project.DerivedProvenance {
			return VariantItem, true
		}
		return CaseItem, true
	case SpecArtifact:
		return TestItem, true
	case SuiteArtifact:
		switch artifact.Role {
		case SuiteDefinitionRole:
			return SuiteItem, true
		case TestReleaseRole:
			return TestItem, true
		case PreparedSuiteRole:
			// A prepared suite is one execution of the suite it retains.
			return RunItem, true
		}
		return "", false
	case JobArtifact, ResultArtifact:
		return RunItem, true
	case TargetArtifact:
		return EnvironmentItem, true
	case PacketArtifact, PortableReviewArtifact, SyntheticPacketArtifact:
		return ReportItem, true
	case ReviewArtifact:
		// An export review is the report a derived packet is exported from.
		if declares(filepath.Join(root, name, "review.json"), redact.ReviewSchema) {
			return ReportItem, true
		}
		return "", false
	case DiagnosisArtifact, DiagnosisGroupsArtifact, AnalysisArtifact:
		return AnalysisItem, true
	case ProfileArtifact, PackArtifact, PackageArtifact:
		return ProfileItem, true
	case PlanArtifact:
		return VariantItem, true
	}
	path := filepath.Join(root, name)
	if entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
		switch {
		case declares(filepath.Join(path, "receipt.json"), connectedtransport.ReceiptSchema):
			return RunItem, true
		case declares(filepath.Join(path, "manifest.json"), replay.Schema):
			return RunItem, true
		case regular(filepath.Join(path, reproducer.ManifestName)):
			return VariantItem, true
		case regular(filepath.Join(path, backup.DocumentName)):
			return BackupItem, true
		}
		return "", false
	}
	if !entry.Type().IsRegular() {
		return "", false
	}
	schema, ok := sniffSchema(path)
	if !ok {
		return "", false
	}
	for family, kind := range familyKinds {
		if strings.HasPrefix(schema, family) {
			return kind, true
		}
	}
	return "", false
}

func declares(path, schema string) bool {
	declared, ok := sniffSchema(path)
	return ok && declared == schema
}

func regular(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// associated records every discovered object the catalog does not hold yet,
// while it holds fewer than capacity objects (a negative capacity bounds
// nothing). An entry that is a file the application saved for an object is
// not discovered as another one.
func associated(document *catalog.Document, discovered []found, capacity int) bool {
	managed := map[string]bool{}
	for _, item := range document.Items {
		for _, revision := range item.Revisions {
			for _, member := range revision.Members {
				managed[member.Path] = true
			}
		}
	}
	changed := false
	for _, entry := range discovered {
		if managed[entry.entry] || document.ByEntry(string(entry.kind), entry.entry) >= 0 {
			continue
		}
		if capacity >= 0 && len(document.Items) >= capacity {
			break
		}
		// The identity this entry would derive can belong to an object that
		// has since been located elsewhere; this is then a different object,
		// derived again from the one it is not, so every read derives the
		// same identity a later write records.
		id := catalog.DiscoveredID(string(entry.kind), entry.entry)
		for document.Find(id) >= 0 {
			id = catalog.DiscoveredID(string(entry.kind), entry.entry+"\x00after:"+id)
		}
		document.Items = append(document.Items, catalog.Item{Kind: string(entry.kind), ID: id, Entry: entry.entry, Revisions: []catalog.Revision{}})
		changed = true
	}
	return changed
}

// list reads every object of one kind in this load's window now. An object
// the application saved without a project entry of its own is in the first.
func (c *loadedCatalog) list(kind ItemKind) []CatalogItem {
	items := []CatalogItem{}
	for _, item := range c.document.Items {
		if item.Kind == string(kind) && !c.removed(item) && c.windowed(item) {
			items = append(items, c.read(item))
		}
	}
	return items
}

// windowed reports whether an object is listed in this load's window.
func (c *loadedCatalog) windowed(item catalog.Item) bool {
	if item.Entry == "" {
		return c.lower == ""
	}
	return inWindow(item.Entry, c.lower, c.upper)
}

// incompleteSaves reports the saves recovery left unpublished.
func (c *loadedCatalog) incompleteSaves() []IncompleteSave {
	saves := []IncompleteSave{}
	for _, save := range c.incomplete {
		incomplete := IncompleteSave{Operation: save.Operation, Kind: ItemKind(save.Kind), Name: save.Name, Reason: save.Reason}
		if c.document.Find(save.Item) >= 0 {
			incomplete.Item = &ItemRef{Kind: ItemKind(save.Kind), ID: save.Item}
		}
		saves = append(saves, incomplete)
	}
	return saves
}

// view is what one object's reader established.
type view struct {
	name string
	// revision is the revision of an object whose current state the
	// project document holds rather than a saved revision.
	revision  string
	summary   ItemSummary
	createdAt *time.Time
	updatedAt *time.Time
}

// read answers one object as it reads now.
func (c *loadedCatalog) read(item catalog.Item) CatalogItem {
	out := CatalogItem{
		Ref:          ItemRef{Kind: ItemKind(item.Kind), ID: item.ID, Revision: item.RevisionLabel()},
		ProjectID:    c.document.Project.ID,
		CreatedAt:    stamped(item.CreatedAt),
		UpdatedAt:    stamped(item.UpdatedAt),
		LastOpenedAt: stamped(cmp.Or(item.LastOpenedAt, c.opened[item.ID])),
		Availability: ItemAvailable,
		Capabilities: []ActionID{},
	}
	kind := ItemKind(item.Kind)
	paths, availability, reason := c.backing(item)
	var read view
	var err error
	switch {
	case (kind == CaseItem || kind == VariantItem) && c.registered(item.Entry):
		// A registered case or revision is read against what the project
		// recorded, whether or not its entry is still there.
		read, availability, reason = c.readRegistered(item)
	case availability == ItemAvailable:
		read, err = readers[kind](c, item, paths)
		var partly *unsupportedClauses
		switch {
		case errors.As(err, &partly):
			// Every clause is kept and shown; the ones this release cannot
			// evaluate make the object unsupported, not unreadable.
			availability, reason = ItemUnsupported, err.Error()
		case err != nil:
			availability, reason = ItemUnreadable, err.Error()
			if unsupportedDeclaration(paths[primaryRole(kind)]) {
				availability, reason = ItemUnsupported, "it declares a contract version this release does not read"
			}
		}
	}
	out.Availability, out.Reason = availability, reason
	if kind == CaseItem && read.summary.Case == nil {
		// A case that cannot be read keeps its entry, so its row can still
		// be located and its reason shown where it is.
		evidence := operation.EvidenceUnreadable
		if availability == ItemMissing {
			evidence = operation.EvidenceMissing
		}
		read.summary.Case = &CaseSummary{Entry: item.Entry, Tags: []string{}, Incidents: []string{}, Evidence: evidence}
	}
	if kind == CaseItem && read.summary.Case != nil {
		c.caseOrigin(item.ID, read.summary.Case)
	}
	// A name is one a person gave the object here or one the object declares
	// itself; a file name is never made into one.
	out.Name = cmp.Or(item.Name, read.name)
	out.Summary = read.summary
	if read.revision != "" {
		out.Ref.Revision = read.revision
	}
	if out.CreatedAt == nil && read.createdAt != nil {
		out.CreatedAt = stampedTime(*read.createdAt)
	}
	if out.UpdatedAt == nil && read.updatedAt != nil {
		out.UpdatedAt = stampedTime(*read.updatedAt)
	}
	if c.uncatalogued[item.ID] && out.Availability == ItemAvailable {
		out.Reason = uncataloguedReason
	}
	out.Capabilities = capabilitiesFor(out, admissions{author: c.author, execute: c.execute})
	return out
}

// caseOrigin adds where a generated case came from, as the project records
// it: the scenario and exact revision it was generated from, or the sample
// fixture run and the ledger it wrote. Either is synthetic.
func (c *loadedCatalog) caseOrigin(id string, summary *CaseSummary) {
	if !c.originsRead {
		c.originsRead, c.origins = true, map[string]catalog.Origin{}
		if held, err := c.store.Origins(); err == nil {
			for _, origin := range held {
				c.origins[origin.Item] = origin
			}
		}
	}
	origin, held := c.origins[id]
	switch {
	case !held:
		return
	case origin.Kind == catalog.OriginScenario:
		scenario := &CaseScenario{Ref: ItemRef{Kind: ScenarioItem, ID: origin.Source, Revision: strconv.Itoa(origin.Revision)}}
		if index := c.document.Find(origin.Source); index >= 0 {
			scenario.Name = c.document.Items[index].Name
		}
		summary.Scenario = scenario
	case origin.Kind == catalog.OriginFixture:
		summary.Fixture = &CaseFixture{Mode: origin.Mode, Observation: origin.Observation}
	}
	summary.Provenance = SyntheticOrigin
}

// uncataloguedReason is why an object the catalog cannot record offers
// nothing beyond opening it.
const uncataloguedReason = "the project holds more objects than its catalog records; this one can be opened, and is changed only once fewer objects are kept here"

// backing resolves the files behind one object: a saved object's revision
// files, each still the bytes that were published, or a discovered object's
// one project entry.
func (c *loadedCatalog) backing(item catalog.Item) (map[string]string, Availability, string) {
	if current := item.Current(); current != nil {
		paths := map[string]string{}
		for _, member := range current.Members {
			path := c.store.Path(member)
			data, err := savedFile.Read(path)
			switch {
			case errors.Is(err, fs.ErrNotExist):
				return nil, ItemMissing, "a file this object was saved as is no longer in the project"
			case errors.Is(err, fs.ErrPermission):
				return nil, ItemUnreadable, "this account cannot read a file this object was saved as"
			case err != nil:
				return nil, ItemUnreadable, "a file this object was saved as cannot be read"
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != member.SHA256 {
				return nil, ItemUnreadable, "a file this object was saved as changed after it was saved"
			}
			paths[member.Role] = path
		}
		return paths, ItemAvailable, ""
	}
	path := filepath.Join(c.root, item.Entry)
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, ItemMissing, "the project no longer holds this object where it was recorded"
	case errors.Is(err, fs.ErrPermission):
		return nil, ItemUnreadable, "this account cannot read this object"
	case err != nil:
		return nil, ItemUnreadable, "this object cannot be inspected"
	case info.Mode()&fs.ModeSymlink != 0:
		return nil, ItemUnsupported, "symbolic links are not opened as objects of a project"
	}
	return map[string]string{primaryRole(ItemKind(item.Kind)): path}, ItemAvailable, ""
}

// savedFile reads one file an object was saved as: bounded, regular, never a
// link.
var savedFile = artifactdir.Document{MaxBytes: catalog.MaxMemberBytes}

// primaryRole is the role of the file a kind is read from.
func primaryRole(kind ItemKind) string {
	switch kind {
	case EnvironmentItem:
		return "target"
	case ObservationItem:
		return "source"
	case AnalysisSettingsItem:
		return "config"
	case FindingReviewItem:
		return "decisions"
	}
	return string(kind)
}

// unsupportedDeclaration reports a regular file that declares a contract of a
// known family in a version this release does not read.
func unsupportedDeclaration(path string) bool {
	schema, ok := sniffSchema(path)
	if !ok {
		return false
	}
	if _, known := declaredSchemas[schema]; known {
		return false
	}
	for family := range familyKinds {
		if strings.HasPrefix(schema, family) {
			return !slices.Contains(extraSchemas, schema)
		}
	}
	return false
}

// extraSchemas are the contracts the catalog reads beyond the listing's own.
var extraSchemas = []string{observesource.SchemaV1, observesource.Schema, observesource.SchemaDatabase,
	assertion.Schema, scenario.Schema, scenario.OrderSchema, scenariogen.Schema, "readmit-runner/v1", "readmit-hub-schedules/v1"}

// admissions are what the operation guard admits now.
type admissions struct{ author, execute bool }

// admitted asks the guard now, admitting nothing.
func (a *App) admitted(ctx context.Context) admissions {
	return admissions{author: a.authorPreview(ctx), execute: a.admissionPreview(ctx).Admitted}
}

// capabilitiesFor is what a person may do to one object in its state, as the
// guard admits now. Reading is always permitted; every change needs the
// author admission and a send the execute admission. Being readable permits
// nothing more.
func capabilitiesFor(item CatalogItem, admitted admissions) []ActionID {
	kind, availability := item.Ref.Kind, item.Availability
	actions := []ActionID{}
	if availability != ItemMissing {
		actions = append(actions, OpenAction)
	}
	if availability == ItemAvailable && item.Reason == uncataloguedReason {
		// Nothing can be recorded of it, so nothing is changed.
		return actions
	}
	if !admitted.author {
		return actions
	}
	if availability != ItemAvailable {
		return append(actions, LocateAction)
	}
	actions = append(actions, RenameAction)
	// A variant is saved as a new case derived from another; its own
	// evidence is never saved over. Of the profiles, only a local profile is
	// saved; a metadata pack or a package is read-only.
	if slices.Contains(savedKinds, kind) && kind != VariantItem && (kind != ProfileItem || item.Summary.Profile != nil && item.Summary.Profile.Form == "local-profile") {
		actions = append(actions, SaveAction)
	}
	if kind == SuiteItem {
		actions = append(actions, ApprovePromotionAction)
	}
	if kind == EnvironmentItem || kind == ObservationItem {
		actions = append(actions, RemoveAction)
	}
	// An export review exports only once it is ready for approval; a case is
	// what an export review is derived from.
	if report := item.Summary.Report; kind == ReportItem && report != nil && report.Form == "export-review" && report.Status == readyForApproval {
		actions = append(actions, ExportPacketAction)
	}
	if kind == CaseItem {
		actions = append(actions, DeriveReviewAction)
	}
	if admitted.execute {
		switch kind {
		case CaseItem:
			actions = append(actions, ReplaySendAction)
		case ObservationItem:
			actions = append(actions, CollectObservationAction)
		case EnvironmentItem:
			actions = append(actions, ResetEnvironmentAction)
		}
	}
	return actions
}

// recorded is what the project records about the case or revision held at
// one entry: its evidence facts, and the registration itself.
func (c *loadedCatalog) recordedAt(entry string) (operation.Facts, *project.Case, *project.Revision) {
	for i, registered := range c.project.Document.Cases {
		if registered.Name == entry {
			return operation.Facts{Name: registered.Name, Identity: registered.Identity, Schema: registered.Schema, Provenance: registered.Provenance},
				&c.project.Document.Cases[i], nil
		}
	}
	for i, registered := range c.revisions.Revisions {
		if registered.Name == entry {
			return operation.Facts{Name: registered.Name, Identity: registered.Identity, Schema: registered.Schema, Provenance: registered.Provenance},
				nil, &c.revisions.Revisions[i]
		}
	}
	return operation.Facts{Name: entry}, nil, nil
}

func (c *loadedCatalog) registered(entry string) bool {
	_, registered, revision := c.recordedAt(entry)
	return registered != nil || revision != nil
}

// removed reports an object a person removed from the project. A case the
// project document still registers is not removed, whatever the catalog
// marks: removing one records the mark and then unregisters it, so a removal
// interrupted between the two leaves the case listed, registered, and
// removable again.
func (c *loadedCatalog) removed(item catalog.Item) bool {
	return item.RemovedAt != "" && !(item.Kind == string(CaseItem) && c.registered(item.Entry))
}

// registeredCase is the project's registration of the case object id, or nil.
func (c *loadedCatalog) registeredCase(id string) *project.Case {
	index := c.document.Find(id)
	if index < 0 || c.document.Items[index].Kind != string(CaseItem) {
		return nil
	}
	_, registered, _ := c.recordedAt(c.document.Items[index].Entry)
	return registered
}

// readRegistered reads a registered case or revision against what the project
// recorded, through the reader `readmit project show` uses.
func (c *loadedCatalog) readRegistered(item catalog.Item) (view, Availability, string) {
	facts, registered, revision := c.recordedAt(item.Entry)
	var read view
	switch {
	case registered != nil:
		read = view{name: registered.Title, revision: caseRevision(*registered, item.Name), summary: ItemSummary{Case: &CaseSummary{Registered: true,
			Entry: item.Entry, Status: registered.Status, Owner: registered.Owner, Tags: orEmpty(registered.Tags), Incidents: orEmpty(registered.Incidents),
			InterfaceVersion: registered.InterfaceVersion, InterfaceRevision: registered.InterfaceVersion, Provenance: provenanceMarker(registered.Provenance)}}}
	case revision != nil:
		parent := c.entryRef(CaseItem, revision.Operation.Parent)
		if parent == nil {
			parent = c.entryRef(VariantItem, revision.Operation.Parent)
		}
		read = view{summary: ItemSummary{Variant: &VariantSummary{Form: "revision", Parent: parent, Operation: revision.Operation.Name, Entry: item.Entry}}}
	}
	c.identities[item.Entry] = facts.Identity
	state := operation.EvidenceState(c.root, facts)
	if read.summary.Case != nil {
		read.summary.Case.Evidence = state
	}
	if manifest, err := bundle.Describe(filepath.Join(c.root, item.Entry)); err == nil {
		read.createdAt = provenanceTime(manifest.Provenance)
		read.updatedAt = read.createdAt
		if registered != nil {
			read.summary.Case.Sources = []project.Source{}
			for _, source := range manifest.Sources {
				read.summary.Case.Sources = append(read.summary.Case.Sources, project.Source{ID: source.ID, Name: registered.SourceNamed(source.ID)})
			}
		}
	}
	switch state {
	case operation.EvidenceVerified:
		return read, ItemAvailable, ""
	case operation.EvidenceMissing:
		return read, ItemMissing, "the project no longer holds this evidence where it was registered"
	case operation.EvidenceChanged:
		return read, ItemUnreadable, "the evidence there is not the evidence the project recorded"
	}
	return read, ItemUnreadable, "the evidence there cannot be verified as complete, unmodified evidence"
}

// sameEvidence reports whether entry holds the evidence the project recorded
// for the object with id.
func (c *loadedCatalog) sameEvidence(id, entry string) bool {
	index := c.document.Find(id)
	if index < 0 {
		return false
	}
	recorded, _, _ := c.recordedAt(c.document.Items[index].Entry)
	if recorded.Identity == "" {
		return false
	}
	facts, _, err := operation.VerifiedCase(c.root, entry)
	return err == nil && facts.Identity == recorded.Identity
}

// entryRef is the reference to the object of kind at one project entry.
func (c *loadedCatalog) entryRef(kind ItemKind, entry string) *ItemRef {
	if entry == "" {
		return nil
	}
	index := c.document.ByEntry(string(kind), entry)
	if index < 0 || c.removed(c.document.Items[index]) {
		return nil
	}
	item := c.document.Items[index]
	return &ItemRef{Kind: kind, ID: item.ID, Revision: item.RevisionLabel()}
}

// caseByIdentity is the reference to the project's case whose evidence is
// identity, when the project holds one.
func (c *loadedCatalog) caseByIdentity(identity string) *ItemRef {
	if identity == "" {
		return nil
	}
	for _, item := range c.document.Items {
		if item.Kind != string(CaseItem) || item.Entry == "" || c.removed(item) {
			continue
		}
		known, ok := c.identities[item.Entry]
		if !ok {
			recorded, _, _ := c.recordedAt(item.Entry)
			if known = recorded.Identity; known == "" {
				if facts, _, err := operation.VerifiedCase(c.root, item.Entry); err == nil {
					known = facts.Identity
				}
			}
			c.identities[item.Entry] = known
		}
		if known == identity {
			return &ItemRef{Kind: CaseItem, ID: item.ID}
		}
	}
	return nil
}

// readers read one available object of each kind through its own reader.
var readers = map[ItemKind]func(*loadedCatalog, catalog.Item, map[string]string) (view, error){
	CaseItem:        readCase,
	TestItem:        readTest,
	SuiteItem:       readSuite,
	RunItem:         readRun,
	EnvironmentItem: readEnvironment,
	ObservationItem: readObservation,
	ReportItem:      readReport,
	CheckGroupItem:  readCheckGroup,
	ProfileItem:     readProfile,
	ScenarioItem:    readScenario,
	AnalysisItem:    readAnalysis,
	VariantItem:     readVariant,
	BackupItem:      readBackup,
	RunnerItem:      readRunner,
	ScheduleItem:    readSchedule,

	AnalysisSettingsItem: readAnalysisSettings,
	FindingReviewItem:    readFindingReview,
}

// readCase verifies a case the project has not registered.
func readCase(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	facts, state, err := operation.VerifiedCase(c.root, item.Entry)
	if err != nil {
		return view{}, err
	}
	c.identities[item.Entry] = facts.Identity
	read := view{summary: ItemSummary{Case: &CaseSummary{Entry: item.Entry, Tags: []string{}, Incidents: []string{}, Evidence: state,
		Provenance: provenanceMarker(facts.Provenance)}}}
	if manifest, err := bundle.Describe(paths[primaryRole(CaseItem)]); err == nil {
		read.createdAt = provenanceTime(manifest.Provenance)
		read.updatedAt = read.createdAt
	}
	return read, nil
}

func readTest(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	path := paths[primaryRole(TestItem)]
	var spec testrunner.Spec
	version := item.RevisionLabel()
	if declares(path, "readmit-test-release/v1") {
		release, err := expectation.Read(path)
		if err != nil {
			return view{}, err
		}
		spec, version = release.Baseline.Spec, revisionLabel(release.Baseline.Revision)
	} else {
		read, err := testrunner.ReadSpec(path)
		if err != nil {
			return view{}, err
		}
		spec = read
	}
	summary := &TestSummary{SourceCase: c.specCase(spec), CurrentVersion: version, Assertions: len(spec.Assertions),
		Boundary: spec.Observation.Boundary, Entry: c.entryOf(path)}
	environment := ""
	if links, held := paths["links"]; held {
		read, err := readTestLinks(links)
		if err != nil {
			return view{}, err
		}
		summary.Tags, environment = read.Tags, read.Environment
	}
	if latest := c.latestRun(spec, environment); latest != nil {
		summary.LatestRun = &ItemRef{Kind: RunItem, ID: latest.id}
		summary.LatestResult = latest.outcome
		if !latest.started.IsZero() {
			summary.LatestRunAt = stampedTime(latest.started)
		}
	}
	return view{name: spec.Name, summary: ItemSummary{Test: summary}}, nil
}

// specCase is the project's case a test names, when it names one entry of
// the project.
func (c *loadedCatalog) specCase(spec testrunner.Spec) *ItemRef {
	if artifactpath := filepath.Clean(spec.Input.Case); filepath.IsLocal(artifactpath) && filepath.Base(artifactpath) == artifactpath {
		if ref := c.entryRef(CaseItem, artifactpath); ref != nil {
			return ref
		}
		return c.entryRef(VariantItem, artifactpath)
	}
	return nil
}

// runView is one retained run's retained test, its start and the status its
// result records, for finding a test's runs. Only a run with a result retains
// its test: a durable run with none is not one of any test's runs.
type runView struct {
	id      string
	spec    *testrunner.Spec
	started time.Time
	outcome testrunner.Status
}

// runs reads every retained run's test once per load.
func (c *loadedCatalog) runs() []runView {
	if !c.runsRead {
		c.runsRead = true
		for _, item := range c.document.Items {
			if item.Kind != string(RunItem) || item.Entry == "" {
				continue
			}
			opened, err := runresult.Open(filepath.Join(c.root, item.Entry))
			if err != nil || opened.Spec == nil {
				continue
			}
			view := runView{id: item.ID, spec: opened.Spec}
			if opened.Run != nil {
				view.started = opened.Run.Manifest.StartedAt
			}
			view.outcome = opened.Artifact.Result.Status
			c.runViews = append(c.runViews, view)
		}
	}
	return c.runViews
}

// runsOf are the runs whose retained test is exactly this one, the most
// recently started first. A test that follows the environment it names
// executed exactly when its retained test differs from it in nothing but the
// target, which is the target of a revision of that environment.
func (c *loadedCatalog) runsOf(spec testrunner.Spec, environment string) []runView {
	matched := []runView{}
	targets := c.environmentTargets(environment)
	for _, run := range c.runs() {
		retained := *run.spec
		if targets[retained.Target] {
			retained.Target = spec.Target
		}
		if reflect.DeepEqual(retained, spec) {
			matched = append(matched, run)
		}
	}
	slices.SortStableFunc(matched, func(x, y runView) int {
		return cmp.Or(y.started.Compare(x.started), cmp.Compare(x.id, y.id))
	})
	return matched
}

// latestRun is the most recently started run whose retained test is exactly
// this one.
func (c *loadedCatalog) latestRun(spec testrunner.Spec, environment string) *runView {
	if matched := c.runsOf(spec, environment); len(matched) > 0 {
		return &matched[0]
	}
	return nil
}

// environmentTargets are the targets every revision of an environment
// declares, by their entries.
func (c *loadedCatalog) environmentTargets(id string) map[string]bool {
	targets := map[string]bool{}
	index := c.document.Find(id)
	if id == "" || index < 0 || c.document.Items[index].Kind != string(EnvironmentItem) {
		return targets
	}
	item := c.document.Items[index]
	for _, revision := range item.Revisions {
		if paths, availability, _ := c.revisionBacking(item, strconv.Itoa(revision.Number)); availability == ItemAvailable {
			if entry := c.entryOf(paths["target"]); entry != "" {
				targets[entry] = true
			}
		}
	}
	return targets
}

func readSuite(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	data, err := boundedFile(paths[primaryRole(SuiteItem)], suite.MaxBytes)
	if err != nil {
		return view{}, err
	}
	document, err := suite.Decode(data)
	if err != nil {
		return view{}, err
	}
	environments := []string{}
	for _, environment := range document.Environments {
		environments = append(environments, environment.ID)
	}
	summary := &SuiteSummary{Tests: len(document.Tests), Environments: environments}
	// The latest run of a suite is the latest execution of its current
	// version: an execution of an earlier version is not one of this one.
	identity := suite.Identity(data)
	for _, run := range c.suiteExecutions() {
		if run.identity == identity && (summary.LatestRun == nil || startedLater(run, *summary)) {
			summary.LatestRun = &ItemRef{Kind: RunItem, ID: run.id}
			summary.LatestRunAt, summary.LatestOutcome = stampedTime(run.started), run.outcome
		}
	}
	return view{name: document.ID, summary: ItemSummary{Suite: summary}}, nil
}

// startedLater reports whether an execution started after the one a summary
// names; a start nobody knows is earlier than every known one, and the
// identity breaks every tie.
func startedLater(run suiteRunView, summary SuiteSummary) bool {
	named := ""
	if summary.LatestRunAt != nil {
		named = *summary.LatestRunAt
	}
	started := ""
	if at := stampedTime(run.started); at != nil {
		started = *at
	}
	return cmp.Or(cmp.Compare(named, started), cmp.Compare(run.id, summary.LatestRun.ID)) < 0
}

// suiteRunView is one retained suite execution: the identity of the suite
// document it executed, that suite's own id, and what it did.
type suiteRunView struct {
	id, identity, suite, outcome string
	started, completed           time.Time
	jobs, uncertain              int
}

// suiteExecutions reads every retained suite execution of the project once
// per load.
func (c *loadedCatalog) suiteExecutions() []suiteRunView {
	if c.suitesRead {
		return c.suiteRuns
	}
	c.suitesRead = true
	for _, item := range c.document.Items {
		if item.Kind != string(RunItem) || item.Entry == "" || c.removed(item) {
			continue
		}
		path := filepath.Join(c.root, item.Entry)
		if !regular(filepath.Join(path, "suite.json")) {
			continue
		}
		if run, err := suiteExecution(item.ID, path); err == nil {
			c.suiteRuns = append(c.suiteRuns, run)
		}
	}
	return c.suiteRuns
}

// suiteExecution reads one retained suite execution through the suite's own
// reader. An execution without its queue report was interrupted; one whose
// queue did not execute every job was stopped.
func suiteExecution(id, path string) (suiteRunView, error) {
	execution, err := suite.OpenExecution(path)
	if err != nil {
		return suiteRunView{}, err
	}
	run := suiteRunView{id: id, identity: execution.Identity, suite: execution.Suite.ID, jobs: len(execution.Queue.Jobs), outcome: "incomplete"}
	if execution.Report != nil {
		run.outcome = "stopped"
		if execution.Report.Executed == len(execution.Report.Jobs) {
			run.outcome = "executed"
		}
	}
	for _, job := range execution.Jobs {
		if !job.StartedAt.IsZero() && (run.started.IsZero() || job.StartedAt.Before(run.started)) {
			run.started = job.StartedAt
		}
		if job.CompletedAt.After(run.completed) {
			run.completed = job.CompletedAt
		}
		if job.DeliveryUncertain {
			run.uncertain++
		}
	}
	return run, nil
}

// suiteView is one suite of the project: its identity and the id it declares.
type suiteView struct {
	ref            ItemRef
	identity, name string
}

// suites reads every suite of the project once per load.
func (c *loadedCatalog) suites() []suiteView {
	if c.suiteViews != nil {
		return c.suiteViews
	}
	c.suiteViews = []suiteView{}
	for _, item := range c.document.Items {
		if item.Kind != string(SuiteItem) || c.removed(item) {
			continue
		}
		paths, availability, _ := c.backing(item)
		if availability != ItemAvailable {
			continue
		}
		data, err := boundedFile(paths[primaryRole(SuiteItem)], suite.MaxBytes)
		if err != nil {
			continue
		}
		if document, err := suite.Decode(data); err == nil {
			c.suiteViews = append(c.suiteViews, suiteView{ref: ItemRef{Kind: SuiteItem, ID: item.ID, Revision: item.RevisionLabel()},
				identity: suite.Identity(data), name: document.ID})
		}
	}
	return c.suiteViews
}

// suiteOf is the project's suite an execution ran: the suite whose current
// version it executed, or else the one suite that declares its id.
func (c *loadedCatalog) suiteOf(run suiteRunView) *ItemRef {
	var named []ItemRef
	for _, held := range c.suites() {
		if held.identity == run.identity {
			return &held.ref
		}
		if held.name == run.suite {
			named = append(named, held.ref)
		}
	}
	if len(named) == 1 {
		return &named[0]
	}
	return nil
}

func readRun(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	path := paths[primaryRole(RunItem)]
	summary := &RunSummary{}
	if regular(filepath.Join(path, "suite.json")) {
		// One execution of a suite: several jobs, each to its own target.
		execution, err := suiteExecution(item.ID, path)
		if err != nil {
			return view{}, err
		}
		summary.Outcome, summary.Jobs, summary.Uncertain = execution.outcome, execution.jobs, execution.uncertain
		summary.DeliveryUncertain = execution.uncertain > 0
		summary.StartedAt, summary.CompletedAt = stampedTime(execution.started), stampedTime(execution.completed)
		summary.Suite = c.suiteOf(execution)
		read := view{summary: ItemSummary{Run: summary}}
		if !execution.started.IsZero() {
			read.createdAt = &execution.started
		}
		return read, nil
	}
	var run *replay.Run
	if declares(filepath.Join(path, "receipt.json"), connectedtransport.ReceiptSchema) {
		receipt, err := connectedtransport.Open(path)
		if err != nil {
			return view{}, err
		}
		run, err = replay.Open(filepath.Join(path, "run"))
		if err != nil {
			return view{}, err
		}
		summary.Outcome = receipt.State
		summary.Boundary = "transport-only"
		summary.SourceCase = c.caseByIdentity(receipt.Binding.Source)
	} else if declares(filepath.Join(path, "manifest.json"), replay.Schema) {
		opened, err := replay.Open(path)
		if err != nil {
			return view{}, err
		}
		run = opened
		summary.Outcome = "not-accepted"
		if opened.Successful() {
			summary.Outcome = "accepted"
		}
	} else {
		opened, err := runresult.Open(path)
		if err != nil {
			return view{}, err
		}
		run = opened.Run
		summary.DeliveryUncertain = opened.Lifecycle.DeliveryUncertain
		switch {
		case opened.Artifact != nil:
			summary.Outcome = string(opened.Artifact.Result.Status)
			if opened.Artifact.Result.Target != nil {
				summary.Target = opened.Artifact.Result.Target.Address
			}
			summary.Boundary = opened.Artifact.Result.ObservationBoundary
			summary.SourceCase = c.caseByIdentity(opened.Artifact.Result.InputBundleIdentity)
		case opened.Durable:
			summary.Outcome = string(opened.Lifecycle.State)
		}
	}
	read := view{summary: ItemSummary{Run: summary}}
	if run != nil {
		summary.Target = run.Manifest.Target.Address
		summary.StartedAt, summary.CompletedAt = stampedTime(run.Manifest.StartedAt), stampedTime(run.Manifest.CompletedAt)
		for _, event := range run.Events {
			if event.Delivery == "uncertain" {
				summary.Uncertain++
			}
		}
		started := run.Manifest.StartedAt
		read.createdAt = &started
	}
	return read, nil
}

func readEnvironment(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	if err := verifyEnvironment(paths); err != nil {
		return view{}, err
	}
	target, err := operation.ReadTarget(paths[primaryRole(EnvironmentItem)])
	if err != nil {
		return view{}, err
	}
	environment := target.Environment()
	summary := &EnvironmentSummary{Classification: string(environment.Classification), Address: target.Address, Transport: target.Transport}
	if _, held := paths["policy"]; held {
		summary.HasPolicy = true
	}
	if path, held := paths["reset"]; held {
		if plan, err := operation.ReadResetPlan(path); err == nil {
			summary.ResetActions = len(plan.Actions)
		}
	}
	if path, held := paths["links"]; held {
		if links, err := readLinks(path); err == nil {
			summary.ResetName = links.ResetName
			if index := c.document.Find(links.Observation); links.Observation != "" && index >= 0 && !c.removed(c.document.Items[index]) {
				linked := c.document.Items[index]
				summary.Observation = &ItemRef{Kind: ObservationItem, ID: linked.ID, Revision: linked.RevisionLabel()}
				summary.ObservationName = linked.Name
				if linked.Name == "" {
					if paths, availability, _ := c.backing(linked); availability == ItemAvailable {
						if source, _, err := operation.ValidateObservationSource(paths["source"]); err == nil {
							summary.ObservationName = source.Observes.Identity
						}
					}
				}
			}
		}
	}
	if check, err := readCheck(c.root, item.ID); err == nil {
		summary.LastCheckedAt, summary.LastCheckOutcome, summary.LastCheckRevision = &check.CheckedAt, check.Outcome, check.Revision
	}
	return view{name: target.Name, summary: ItemSummary{Environment: summary}}, nil
}

func readObservation(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	source, _, err := operation.ValidateObservationSource(paths[primaryRole(ObservationItem)])
	if err != nil {
		return view{}, err
	}
	if window, held := paths["window"]; held {
		if _, _, err := operation.ValidateObservationPair(paths["source"], window); err != nil {
			return view{}, err
		}
	}
	summary := &ObservationSummary{SourceType: source.Observes.Kind, Enabled: source.Enabled}
	var latest *observewindow.Completion
	for i, completion := range c.completionRecords() {
		if completion.Source == source.Observes && completion.Trustworthy() && (latest == nil || completion.ClosedAt.After(latest.ClosedAt)) {
			latest = &c.completions[i]
		}
	}
	if latest != nil {
		summary.LatestCollection, summary.LatestStatus = stampedTime(latest.ClosedAt), string(latest.Status)
	}
	return view{name: source.Observes.Identity, summary: ItemSummary{Observation: summary}}, nil
}

// completionRecords are the project's retained completion records.
func (c *loadedCatalog) completionRecords() []observewindow.Completion {
	if c.doneRead {
		return c.completions
	}
	c.doneRead = true
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		path := filepath.Join(c.root, entry.Name())
		if entry.Type().IsRegular() && declares(path, observewindow.CompletionSchema) {
			if completion, err := observewindow.ReadCompletion(path); err == nil {
				c.completions = append(c.completions, completion)
			}
		}
	}
	return c.completions
}

func readReport(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	path := paths[primaryRole(ReportItem)]
	manifest := filepath.Join(path, "manifest.json")
	switch {
	case declares(filepath.Join(path, "review.json"), redact.ReviewSchema):
		review, private, err := c.exportReview(path)
		if err != nil {
			return view{}, err
		}
		return view{summary: ItemSummary{Report: &ReportSummary{Form: "export-review", RelatedCase: c.caseByIdentity(private.caseIdentity), Status: review.State}}}, nil
	case declares(manifest, report.RetainedSchema):
		packet, err := report.OpenRetained(c.ctx, path)
		if err != nil {
			return view{}, err
		}
		status := "not-reviewed"
		if c.packetRelations().reviewed[packet.Identity] {
			status = "reviewed"
		}
		return view{summary: ItemSummary{Report: &ReportSummary{Form: "packet", RelatedCase: c.caseByIdentity(packet.Manifest.Current.CaseIdentity), Status: status}}}, nil
	case declares(manifest, report.ReviewSchema):
		review, err := report.OpenReview(c.ctx, path)
		if err != nil {
			return view{}, err
		}
		related := c.caseByIdentity(c.packetRelations().cases[review.Manifest.PacketIdentity])
		return view{summary: ItemSummary{Report: &ReportSummary{Form: "portable-review", RelatedCase: related, Status: "sealed"}}}, nil
	}
	packet, err := report.Open(path)
	if err != nil {
		return view{}, err
	}
	return view{name: packet.Manifest.Scenario, summary: ItemSummary{Report: &ReportSummary{Form: "synthetic-packet", RelatedCase: c.caseByIdentity(packet.Manifest.InputIdentity), Status: "sealed"}}}, nil
}

// privateState is one private local-state folder of the project: its entry
// and the identity of the case it was derived from.
type privateState struct {
	entry, caseIdentity string
}

// exportReview reads the export review at path and the one private
// local-state folder of the project its commitment names. A review whose
// private state is absent, or held by more than one folder, is not one that
// can be exported, and says so.
func (c *loadedCatalog) exportReview(path string) (*redact.Review, privateState, error) {
	review, err := redact.OpenReview(path)
	if err != nil {
		return nil, privateState{}, err
	}
	if c.private == nil {
		c.private = map[string][]privateState{}
		folders, err := projectEntries(c.root, catalog.MaxItems, func(name string) bool {
			info, err := os.Lstat(filepath.Join(c.root, name))
			return err == nil && info.IsDir() && regular(filepath.Join(c.root, name, "state.json"))
		})
		c.privateErr = err
		for _, name := range folders {
			if commitment, identity, err := redact.OpenPrivateState(filepath.Join(c.root, name)); err == nil {
				c.private[commitment] = append(c.private[commitment], privateState{entry: name, caseIdentity: identity})
			}
		}
	}
	if c.privateErr != nil {
		return nil, privateState{}, c.privateErr
	}
	switch held := c.private[review.LocalStateCommitment]; len(held) {
	case 0:
		return nil, privateState{}, errors.New("the private local state this export review was derived with is not in the project")
	case 1:
		return review, held[0], nil
	}
	return nil, privateState{}, errors.New("more than one folder of the project holds this export review's private local state")
}

// projectEntries are the names of the project folder's entries keep accepts,
// read in bounded chunks. More than limit of them is refused rather than
// read in part.
func projectEntries(root string, limit int, keep func(name string) bool) ([]string, error) {
	folder, err := os.Open(root)
	if err != nil {
		return nil, errors.New("the project folder cannot be read")
	}
	defer folder.Close()
	var kept []string
	for {
		chunk, err := folder.Readdirnames(MaxWorkspaceEntries)
		for _, name := range chunk {
			if keep(name) {
				if kept = append(kept, name); len(kept) > limit {
					return nil, errors.New("the project holds more of these than this release reads")
				}
			}
		}
		if errors.Is(err, io.EOF) {
			slices.Sort(kept)
			return kept, nil
		}
		if err != nil {
			return nil, errors.New("the project folder cannot be read")
		}
	}
}

// relations are the verified relationships between the project's sealed
// packets and the portable reviews exported from them.
type relations struct {
	cases    map[string]string
	reviewed map[string]bool
}

// packetRelations reads, once per load, which case each sealed packet of the
// project is about and which packets a portable review of the project was
// exported from, each through its own reader.
func (c *loadedCatalog) packetRelations() relations {
	if c.packets != nil {
		return *c.packets
	}
	found := relations{cases: map[string]string{}, reviewed: map[string]bool{}}
	for _, item := range c.document.Items {
		if item.Kind != string(ReportItem) || item.Entry == "" {
			continue
		}
		path := filepath.Join(c.root, item.Entry)
		manifest := filepath.Join(path, "manifest.json")
		switch {
		case declares(manifest, report.RetainedSchema):
			if packet, err := report.OpenRetained(c.ctx, path); err == nil {
				found.cases[packet.Identity] = packet.Manifest.Current.CaseIdentity
			}
		case declares(manifest, report.ReviewSchema):
			if review, err := report.OpenReview(c.ctx, path); err == nil {
				found.reviewed[review.Manifest.PacketIdentity] = true
			}
		}
	}
	c.packets = &found
	return found
}

// readCheckGroup reads a check group clause by clause, so a group holding a
// check this release cannot evaluate is still listed, with every check it
// holds counted and the unsupported ones named as its reason.
func readCheckGroup(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	data, err := boundedFile(paths[primaryRole(CheckGroupItem)], 1<<20)
	if err != nil {
		return view{}, err
	}
	set, unsupported, err := assertionauthor.ReadLenient(data)
	if err != nil {
		return view{}, err
	}
	summary := &CheckGroupSummary{Assertions: len(set.Assertions) + len(unsupported), Revision: item.RevisionLabel(), Unsupported: len(unsupported)}
	read := view{name: set.Name, summary: ItemSummary{CheckGroup: summary}}
	if len(unsupported) > 0 {
		return read, &unsupportedClauses{count: len(unsupported)}
	}
	return read, nil
}

// unsupportedClauses is a check group read whole whose count checks use an
// operator, or members, this release does not evaluate.
type unsupportedClauses struct{ count int }

func (u *unsupportedClauses) Error() string {
	if u.count == 1 {
		return "one check uses an operator this release does not evaluate"
	}
	return strconv.Itoa(u.count) + " checks use an operator this release does not evaluate"
}

func readProfile(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	path := paths[primaryRole(ProfileItem)]
	data, err := boundedFile(path, 4<<20)
	if err != nil {
		return view{}, err
	}
	schema, _ := sniffSchema(path)
	switch {
	case strings.HasPrefix(schema, "readmit-local-profile/"):
		profile, err := localprofile.Decode(data)
		if err != nil {
			return view{}, err
		}
		return view{name: profile.Identity.ID, summary: ItemSummary{Profile: &ProfileSummary{Form: "local-profile", Family: profile.Base.Family,
			ProtocolVersion: profile.Base.HL7Version, PublishedVersion: profile.Identity.Version}}}, nil
	case strings.HasPrefix(schema, "readmit-profile-pack/"):
		pack, err := profilepack.Decode(data)
		if err != nil {
			return view{}, err
		}
		summary := &ProfileSummary{Form: "profile-pack", PublishedVersion: pack.Identity.Version}
		if len(pack.Coverage) == 1 {
			summary.Family, summary.ProtocolVersion = pack.Coverage[0].Family, pack.Coverage[0].HL7Version
		}
		return view{name: pack.Identity.ID, summary: ItemSummary{Profile: summary}}, nil
	}
	if _, err := profilepackage.Decode(data); err != nil {
		return view{}, err
	}
	return view{summary: ItemSummary{Profile: &ProfileSummary{Form: "profile-package"}}}, nil
}

func readScenario(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	path := paths[primaryRole(ScenarioItem)]
	data, err := boundedFile(path, 4<<20)
	if err != nil {
		return view{}, err
	}
	if declares(path, scenariogen.Schema) {
		plan, err := scenariogen.Decode(data)
		if err != nil {
			return view{}, err
		}
		workflow, err := scenario.DecodeDocument(plan.Template)
		if err != nil {
			return view{}, err
		}
		identity := workflow.Identity()
		seed, base := plan.Seed, catalog.Stamp(workflow.BaseTime)
		metadata, err := readMetadata(paths)
		if err != nil {
			return view{}, err
		}
		return view{name: identity.ID, summary: ItemSummary{Scenario: &ScenarioSummary{Version: identity.Version, Profile: string(workflow.Profile),
			Family: lifecycleFamily(workflow.Profile), Plan: true, Seed: &seed, BaseTime: &base, GeneratorVersion: plan.GeneratorVersion,
			LocalProfile: metadata.Profile}}}, nil
	}
	if declares(path, scenario.OrderSchema) {
		orders, err := scenario.DecodeOrders(data)
		if err != nil {
			return view{}, err
		}
		return view{name: orders.Scenario.ID, summary: ItemSummary{Scenario: &ScenarioSummary{Version: orders.Scenario.Version, Profile: string(orders.Profile),
			Family: lifecycleFamily(orders.Profile)}}}, nil
	}
	declared, err := scenario.Decode(data)
	if err != nil {
		return view{}, err
	}
	return view{name: declared.Scenario.ID, summary: ItemSummary{Scenario: &ScenarioSummary{Version: declared.Scenario.Version, Profile: string(declared.Profile),
		Family: lifecycleFamily(declared.Profile)}}}, nil
}

// lifecycleFamily is the message family a lifecycle profile generates, the
// inverse of lifecycleForFamily; empty for a profile this release does not
// implement.
func lifecycleFamily(profile scenario.ProfileName) string {
	for _, family := range []string{"ADT", "SIU", "ORM", "ORU"} {
		if named, _ := lifecycleForFamily(family); named == profile {
			return family
		}
	}
	return ""
}

func readAnalysis(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	path := paths[primaryRole(AnalysisItem)]
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		if declares(filepath.Join(path, diagnose.ReportName), diagnose.GroupsSchema) {
			grouping, err := diagnose.OpenGroups(path)
			if err != nil {
				return view{}, err
			}
			summary := &AnalysisSummary{Form: "grouping", Cases: []ItemRef{}}
			if len(grouping.Cases) > 0 {
				summary.ProfileName = c.configName(grouping.Cases[0].ConfigSHA256)
			}
			for _, report := range grouping.Cases {
				summary.Findings += len(report.Findings)
				summary.Unsupported += len(report.Unsupported)
				if ref := c.caseByIdentity(report.CaseIdentity); ref != nil {
					summary.Cases = append(summary.Cases, *ref)
				}
			}
			return view{summary: ItemSummary{Analysis: summary}}, nil
		}
		retained, err := c.retainedAnalysis(item.Entry, path)
		if err != nil {
			return view{}, err
		}
		report := retained.Report
		return view{summary: ItemSummary{Analysis: &AnalysisSummary{Form: "diagnosis", RelatedCase: c.caseByIdentity(report.CaseIdentity), Cases: []ItemRef{},
			Findings: len(report.Findings), CaseIdentity: report.CaseIdentity, Profile: report.Profile, ProfileName: c.configName(report.ConfigSHA256), Ruleset: report.Ruleset,
			ConfigSHA256: report.ConfigSHA256, Unsupported: len(report.Unsupported)}}}, nil
	}
	data, err := boundedFile(path, 4<<20)
	if err != nil {
		return view{}, err
	}
	declaration, err := sequenceanalysis.Parse(data)
	if err != nil {
		return view{}, err
	}
	return view{summary: ItemSummary{Analysis: &AnalysisSummary{Form: "sequence-analysis", RelatedCase: c.caseByIdentity(declaration.CaseIdentity), Cases: []ItemRef{}}}}, nil
}

func readVariant(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	path := paths[primaryRole(VariantItem)]
	info, err := os.Stat(path)
	if err != nil {
		return view{}, errors.New("the variant cannot be inspected")
	}
	if !info.IsDir() {
		data, err := boundedFile(path, 4<<20)
		if err != nil {
			return view{}, err
		}
		plan, err := transform.DecodePlan(data)
		if err != nil {
			return view{}, err
		}
		return view{summary: ItemSummary{Variant: &VariantSummary{Form: "transform-plan", Parent: c.caseByIdentity(plan.Case)}}}, nil
	}
	if regular(filepath.Join(path, reproducer.ManifestName)) {
		manifest, err := reproducer.Open(path)
		if err != nil {
			return view{}, err
		}
		return view{summary: ItemSummary{Variant: &VariantSummary{Form: "reproducer", Parent: c.caseByIdentity(manifest.Parent.Identity)}}}, nil
	}
	// A derived case the project has not registered as a revision.
	if _, _, err := operation.VerifiedCase(c.root, item.Entry); err != nil {
		return view{}, err
	}
	return view{summary: ItemSummary{Variant: &VariantSummary{Form: "derived-case", Entry: item.Entry}}}, nil
}

func readBackup(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	document, err := backup.Verify(paths[primaryRole(BackupItem)])
	if err != nil {
		return view{}, err
	}
	return view{summary: ItemSummary{Backup: &BackupSummary{Complete: document.Complete(), Evidence: len(document.Evidence)}}}, nil
}

func readRunner(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	data, err := boundedFile(paths[primaryRole(RunnerItem)], 1<<20)
	if err != nil {
		return view{}, err
	}
	config, err := customerrunner.DecodeConfig(data)
	if err != nil {
		return view{}, err
	}
	return view{summary: ItemSummary{Runner: &RunnerSummary{Environment: config.Environment}}}, nil
}

func readSchedule(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	data, err := boundedFile(paths[primaryRole(ScheduleItem)], 1<<20)
	if err != nil {
		return view{}, err
	}
	policy, err := runnerprotocol.DecodeSchedules(data)
	if err != nil {
		return view{}, err
	}
	return view{summary: ItemSummary{Schedule: &ScheduleSummary{Schedules: len(policy.Schedules)}}}, nil
}

// boundedFile reads one regular file within limit, never through a link.
func boundedFile(path string, limit int) ([]byte, error) {
	data, err := artifactdir.Document{MaxBytes: limit}.Read(path)
	if err != nil {
		return nil, errors.New("the file cannot be read as a bounded regular file")
	}
	return data, nil
}

// provenanceTime is when evidence says it was imported or recorded.
func provenanceTime(provenance bundle.Provenance) *time.Time {
	switch {
	case provenance.ImportedAt != nil:
		return provenance.ImportedAt
	case provenance.StartedAt != nil:
		return provenance.StartedAt
	}
	return nil
}

func stamped(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stampedTime(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	stamp := catalog.Stamp(value)
	return &stamp
}

// revisionLabel is a revision number as a reference carries it: empty for
// none.
func revisionLabel(value int) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(value)
}
