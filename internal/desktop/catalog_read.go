package desktop

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/profilepackage"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/scenario"
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

	author, execute bool

	runViews    []runView
	runsRead    bool
	completions []observewindow.Completion
	doneRead    bool
	identities  map[string]string
	packets     *relations
}

// loadCatalog opens the project the request names and discovers what it
// holds through each object's own recognizer. A read (record false) writes
// nothing: an object the catalog has not recorded yet is listed under the
// identity derived from its kind and entry, which is the identity recording
// it keeps, and an interrupted save is reported, not settled. A write (record
// true), made only under the author admission, settles interrupted saves and
// records every new association first.
func (a *App) loadCatalog(ctx context.Context, request RequestContext, record bool) (*loadedCatalog, refusal) {
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
	loaded := &loadedCatalog{ctx: ctx, root: opened.Root, project: opened, revisions: *revisions, store: store, identities: map[string]string{}}
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
		loaded.incomplete, err = store.Recover(verifiers, catalog.Options{Now: a.now})
	} else {
		loaded.incomplete, err = store.Inspect(verifiers)
	}
	if err != nil {
		return nil, refusal{Failed, "an interrupted save of this project cannot be read"}
	}
	discovered, declined := discover(ctx, opened.Root, *revisions, &opened.Document)
	if declined.state != "" {
		return nil, declined
	}
	staged := store.PendingFiles()
	discovered = slices.DeleteFunc(discovered, func(entry found) bool { return slices.Contains(staged, entry.entry) })
	if record {
		loaded.document, err = store.Update(a.now(), func(document *catalog.Document) (bool, error) { return associated(document, discovered), nil })
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
		associated(&document, discovered)
		loaded.document = document
	}
	loaded.recorded = present
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

// discover lists the project's entries and names the kind each declares, by
// the listing's own recognizers and the declared contract; it verifies
// nothing. Registered cases and revisions are named whether or not their
// entry is still there, so a moved one stays a visible, missing object.
func discover(ctx context.Context, root string, revisions project.Revisions, document *project.Document) ([]found, refusal) {
	entries, err := os.ReadDir(root)
	switch {
	case errors.Is(err, fs.ErrPermission):
		return nil, refusal{PermissionDenied, "this account cannot read the project folder"}
	case err != nil:
		return nil, refusal{Failed, "the project folder cannot be read"}
	case len(entries) > MaxWorkspaceEntries:
		return nil, refusal{Failed, "the folder holds more entries than this release lists"}
	}
	var discovered []found
	for _, registered := range document.Cases {
		discovered = append(discovered, found{CaseItem, registered.Name})
	}
	for _, registered := range revisions.Revisions {
		discovered = append(discovered, found{VariantItem, registered.Name})
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, cancelledRefusal
		}
		if kind, ok := entryKind(root, entry); ok && !slices.Contains(discovered, found{kind, entry.Name()}) {
			discovered = append(discovered, found{kind, entry.Name()})
		}
	}
	return discovered, refusal{}
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
	"readmit-sequence-analysis/":  AnalysisItem,
	"readmit-transform-plan/":     VariantItem,
	"readmit-runner/":             RunnerItem,
	"readmit-hub-schedules/":      ScheduleItem,
}

// entryKind names the kind of object one entry declares. The project's own
// documents, the catalog's area and the files the application saved for an
// object are not objects of their own.
func entryKind(root string, entry fs.DirEntry) (ItemKind, bool) {
	name := entry.Name()
	switch {
	case name == catalog.Folder, name == project.DocumentName, name == project.RevisionsDocumentName, name == project.QuotaDocumentName:
		return "", false
	case strings.Contains(name, ".recovery-"), strings.HasSuffix(name, ".incomplete"):
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
		}
		return "", false
	case JobArtifact, ResultArtifact:
		return RunItem, true
	case TargetArtifact:
		return EnvironmentItem, true
	case PacketArtifact, PortableReviewArtifact, SyntheticPacketArtifact:
		return ReportItem, true
	case DiagnosisArtifact, AnalysisArtifact:
		return AnalysisItem, true
	case ProfileArtifact, PackArtifact, PackageArtifact:
		return ProfileItem, true
	case PlanArtifact:
		return VariantItem, true
	}
	path := filepath.Join(root, name)
	if entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
		switch {
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

// associated records every discovered object the catalog does not hold yet.
// An entry that is a file the application saved for an object is not
// discovered as another one.
func associated(document *catalog.Document, discovered []found) bool {
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

// list reads every object of one kind now.
func (c *loadedCatalog) list(kind ItemKind) []CatalogItem {
	items := []CatalogItem{}
	for _, item := range c.document.Items {
		if item.Kind == string(kind) && !c.removed(item) {
			items = append(items, c.read(item))
		}
	}
	return items
}

// incompleteSaves reports the saves recovery left unpublished.
func (c *loadedCatalog) incompleteSaves() []IncompleteSave {
	saves := []IncompleteSave{}
	for _, save := range c.incomplete {
		incomplete := IncompleteSave{Operation: save.Operation, Name: save.Name, Reason: save.Reason}
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
		LastOpenedAt: stamped(item.LastOpenedAt),
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
		if err != nil {
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
	out.Capabilities = c.capabilities(kind, availability)
	return out
}

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
	assertion.Schema, scenario.Schema, scenario.OrderSchema, "readmit-runner/v1", "readmit-hub-schedules/v1"}

// capabilities are the actions permitted on an object now: what its state
// allows, and what the backend admits. Being readable permits nothing more.
func (c *loadedCatalog) capabilities(kind ItemKind, availability Availability) []ActionID {
	return capabilitiesFor(kind, availability, admissions{author: c.author, execute: c.execute})
}

// admissions are what the operation guard admits now.
type admissions struct{ author, execute bool }

// admitted asks the guard now, admitting nothing.
func (a *App) admitted(ctx context.Context) admissions {
	return admissions{author: a.authorPreview(ctx), execute: a.admissionPreview(ctx).Admitted}
}

// capabilitiesFor is what a person may do to an object of kind in its state,
// as the guard admits now. Reading is always permitted; every change needs
// the author admission and a send the execute admission.
func capabilitiesFor(kind ItemKind, availability Availability, admitted admissions) []ActionID {
	actions := []ActionID{}
	if availability != ItemMissing {
		actions = append(actions, OpenAction)
	}
	if !admitted.author {
		return actions
	}
	if availability != ItemAvailable {
		return append(actions, LocateAction)
	}
	actions = append(actions, RenameAction)
	if slices.Contains(savedKinds, kind) {
		actions = append(actions, SaveAction)
	}
	if kind == SuiteItem {
		actions = append(actions, ApprovePromotionAction)
	}
	if admitted.execute && kind == CaseItem {
		actions = append(actions, ReplaySendAction)
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
		read = view{summary: ItemSummary{Variant: &VariantSummary{Form: "revision", Parent: parent, Operation: revision.Operation.Name}}}
	}
	c.identities[item.Entry] = facts.Identity
	state := operation.EvidenceState(c.root, facts)
	if read.summary.Case != nil {
		read.summary.Case.Evidence = state
	}
	if manifest, err := bundle.Describe(filepath.Join(c.root, item.Entry)); err == nil {
		read.createdAt = provenanceTime(manifest.Provenance)
		read.updatedAt = read.createdAt
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
	if declares(path, "readmit-test-release/v1") {
		release, err := expectation.Read(path)
		if err != nil {
			return view{}, err
		}
		spec := release.Baseline.Spec
		return view{name: spec.Name, summary: ItemSummary{Test: &TestSummary{SourceCase: c.specCase(spec),
			CurrentVersion: revisionLabel(release.Baseline.Revision), LatestRun: c.latestRun(spec), Assertions: len(spec.Assertions)}}}, nil
	}
	spec, err := testrunner.ReadSpec(path)
	if err != nil {
		return view{}, err
	}
	return view{name: spec.Name, summary: ItemSummary{Test: &TestSummary{SourceCase: c.specCase(spec),
		CurrentVersion: item.RevisionLabel(), LatestRun: c.latestRun(spec), Assertions: len(spec.Assertions)}}}, nil
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

// runView is one retained run's retained test and start, for finding a
// test's latest run.
type runView struct {
	id      string
	spec    *testrunner.Spec
	started time.Time
}

// latestRun is the most recently started run whose retained test is exactly
// this one.
func (c *loadedCatalog) latestRun(spec testrunner.Spec) *ItemRef {
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
			started := time.Time{}
			if opened.Run != nil {
				started = opened.Run.Manifest.StartedAt
			}
			c.runViews = append(c.runViews, runView{id: item.ID, spec: opened.Spec, started: started})
		}
	}
	var latest *runView
	for i, run := range c.runViews {
		if reflect.DeepEqual(*run.spec, spec) && (latest == nil || run.started.After(latest.started)) {
			latest = &c.runViews[i]
		}
	}
	if latest == nil {
		return nil
	}
	return &ItemRef{Kind: RunItem, ID: latest.id}
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
	return view{name: document.ID, summary: ItemSummary{Suite: &SuiteSummary{Tests: len(document.Tests), Environments: environments}}}, nil
}

func readRun(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	path := paths[primaryRole(RunItem)]
	summary := &RunSummary{}
	var run *replay.Run
	if declares(filepath.Join(path, "manifest.json"), replay.Schema) {
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
	target, err := operation.ReadTarget(paths[primaryRole(EnvironmentItem)])
	if err != nil {
		return view{}, err
	}
	environment := target.Environment()
	return view{name: target.Name, summary: ItemSummary{Environment: &EnvironmentSummary{
		Classification: string(environment.Classification), Address: target.Address, Transport: target.Transport}}}, nil
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

func readCheckGroup(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	data, err := boundedFile(paths[primaryRole(CheckGroupItem)], 1<<20)
	if err != nil {
		return view{}, err
	}
	set, err := assertion.Decode(data)
	if err != nil {
		return view{}, err
	}
	return view{name: set.Name, summary: ItemSummary{CheckGroup: &CheckGroupSummary{Assertions: len(set.Assertions)}}}, nil
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
	if declares(path, scenario.OrderSchema) {
		orders, err := scenario.DecodeOrders(data)
		if err != nil {
			return view{}, err
		}
		return view{name: orders.Scenario.ID, summary: ItemSummary{Scenario: &ScenarioSummary{Version: orders.Scenario.Version, Profile: string(orders.Profile)}}}, nil
	}
	declared, err := scenario.Decode(data)
	if err != nil {
		return view{}, err
	}
	return view{name: declared.Scenario.ID, summary: ItemSummary{Scenario: &ScenarioSummary{Version: declared.Scenario.Version, Profile: string(declared.Profile)}}}, nil
}

func readAnalysis(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	path := paths[primaryRole(AnalysisItem)]
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		retained, err := diagnose.OpenReport(path, diagnose.Reading{})
		if err != nil {
			return view{}, err
		}
		return view{summary: ItemSummary{Analysis: &AnalysisSummary{Form: "diagnosis", RelatedCase: c.caseByIdentity(retained.Report.CaseIdentity),
			Findings: len(retained.Report.Findings)}}}, nil
	}
	data, err := boundedFile(path, 4<<20)
	if err != nil {
		return view{}, err
	}
	declaration, err := sequenceanalysis.Parse(data)
	if err != nil {
		return view{}, err
	}
	return view{summary: ItemSummary{Analysis: &AnalysisSummary{Form: "sequence-analysis", RelatedCase: c.caseByIdentity(declaration.CaseIdentity)}}}, nil
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
	return view{summary: ItemSummary{Variant: &VariantSummary{Form: "derived-case"}}}, nil
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
