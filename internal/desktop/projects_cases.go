package desktop

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
)

// Projects and Cases (#548): the details of a case and the settings of a
// project are saved whole into the project document, under the same
// compare-and-swap and click identity every editor Save has, and a case
// leaves a project without a byte of its evidence touched.

// CaseDraft is the whole of a case's details as Edit details holds them.
// Status is one of open, investigating, resolved and closed. Owner and
// InterfaceRevision may be empty: an owner is local metadata, not an
// authenticated reviewer, and a readmit-project/v2 case may leave its
// revision unassigned. Tags and Incidents are sets.
type CaseDraft struct {
	Name              string         `json:"name"`
	Status            project.Status `json:"status"`
	Owner             string         `json:"owner,omitzero"`
	Tags              []string       `json:"tags"`
	InterfaceRevision string         `json:"interface_revision,omitzero"`
	Incidents         []string       `json:"incidents"`
}

// ProjectDraft is the whole of a project's settings. Revisions are the
// interface revisions the project keeps, in order: one with an ID is the
// declared revision of that identity, renamed or not; one without is new and
// is given an identity when it is saved. A declared revision left out is
// removed, which is refused while a case is assigned to it unless Reassign
// moves those cases first.
type ProjectDraft struct {
	Name      string          `json:"name"`
	Owner     string          `json:"owner,omitzero"`
	Tags      []string        `json:"tags"`
	Revisions []RevisionDraft `json:"revisions"`
	Reassign  []Reassignment  `json:"reassign,omitzero"`
}

// RevisionDraft is one interface revision a project keeps.
type RevisionDraft struct {
	ID      string `json:"id,omitzero"`
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// Reassignment moves every case assigned to a revision the save removes to
// another declared revision, or, when To is empty, leaves them unassigned.
type Reassignment struct {
	From string `json:"from"`
	To   string `json:"to,omitzero"`
}

// metadataIntents remembers, for this process, what each click that saved
// a case's details, a project's settings or a note submitted, so the same
// click again is recognized and a different submission under it is refused.
// The oldest is forgotten first once catalog.MaxIntents are held.
type metadataIntents struct {
	mu    sync.Mutex
	held  map[string]string
	order []string
}

// admit answers whether intent may carry digest — it is new, or it carried
// exactly this digest before — and whether it already did, which makes this
// submission a repeat of one already answered.
func (m *metadataIntents) admit(intent, digest string) (admitted, repeated bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	held, ok := m.held[intent]
	return !ok || held == digest, ok && held == digest
}

func (m *metadataIntents) record(intent, digest string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.held == nil {
		m.held = map[string]string{}
	}
	if _, ok := m.held[intent]; !ok {
		m.order = append(m.order, intent)
	}
	m.held[intent] = digest
	for len(m.order) > catalog.MaxIntents {
		delete(m.held, m.order[0])
		m.order = m.order[1:]
	}
}

// claim checks a click's identity and admits its submission, answering
// whether it repeats one already answered, or the refusal.
func (m *metadataIntents) claim(intent, digest string) (bool, refusal) {
	if !catalog.ValidToken(intent) {
		return false, refusal{Failed, "a save names the click it was submitted by"}
	}
	admitted, repeated := m.admit(intent, digest)
	if !admitted {
		return false, refusal{Failed, "this submission was already used for different content; nothing was saved"}
	}
	return repeated, refusal{}
}

// nameRule is what a name is, wherever a person gives one.
const nameRule = "a name is 1 to 200 characters of printable text"

// validName reports a name the catalog and the document holding it both
// accept, check being that document's own title rule.
func validName(name string, check func(string) error) bool {
	return catalog.ValidName(name) && check(name) == nil
}

// writeRefusal answers a project document write that failed: a folder this
// account cannot write, or documents that could not be read to change.
func writeRefusal(root string, err error) refusal {
	if errors.Is(err, operation.ErrProjectWrite) || errors.Is(err, operation.ErrProjectNoteWrite) {
		return probeWriteFailure(root, "this account cannot write to the project folder",
			"the project's documents could not be replaced; an interrupted write may be retained beside them")
	}
	return refusal{Failed, "the project's documents cannot be read"}
}

// submissionOf is the digest of everything one submission asks for.
func submissionOf(parts ...any) string {
	data, _ := json.Marshal(parts, json.Deterministic(true))
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// caseRevision is the revision of a registered case's details: a digest of
// what the project records about it and the name the catalog gives it, so
// any change of either is a new revision.
func caseRevision(registered project.Case, name string) string {
	return submissionOf(registered.Name, registered.Identity, registered.Title, string(registered.Status), registered.Owner,
		registered.InterfaceVersion, orEmpty(registered.Tags), orEmpty(registered.Incidents), name)[:16]
}

// projectRevision is the revision of a project's settings.
func projectRevision(document project.Document) string {
	return submissionOf(document.Schema, document.Settings.Title, document.Settings.DefaultOwner, document.Settings.DefaultInterfaceVersion,
		orEmpty(document.Settings.Tags), orEmpty(document.InterfaceVersions), document.InterfaceVersionNames)[:16]
}

// interfaceRevisions are a project's declared revisions with their names.
func interfaceRevisions(document project.Document) []InterfaceRevision {
	revisions := []InterfaceRevision{}
	for _, version := range document.InterfaceVersions {
		revisions = append(revisions, InterfaceRevision{ID: version, Name: document.VersionNamed(version),
			Default: version == document.Settings.DefaultInterfaceVersion})
	}
	return revisions
}

// sortedSet is a set as the project stores it: trimmed, sorted, once each.
func sortedSet(values []string) []string {
	set := []string{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set = append(set, value)
		}
	}
	slices.Sort(set)
	return slices.Compact(set)
}

// validateDocumentDraft validates a case's details or a project's settings
// against the project as it is now, and writes nothing.
func (a *App) validateDocumentDraft(ctx context.Context, request DraftRequest) DraftValidation {
	result := DraftValidation{Context: request.Context, Problems: []FieldProblem{}}
	loaded, declined := a.loadCatalog(ctx, request.Context, false)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	var projection *ItemDraft
	var problems []FieldProblem
	switch request.Kind {
	case CaseItem:
		var draft CaseDraft
		draft, problems = planCase(loaded.project.Document, request.Draft.Case)
		projection = &ItemDraft{Case: &draft}
	case ProjectItem:
		var draft ProjectDraft
		_, draft, problems = loaded.planProject(request.Draft.Project)
		projection = &ItemDraft{Project: &draft}
	}
	result.State, result.Problems = Completed, problems
	if len(problems) == 0 {
		result.Projection = projection
	}
	return result
}

// saveDocumentItem saves a case's details or a project's settings.
func (a *App) saveDocumentItem(ctx context.Context, request SaveItemRequest) SaveItemResult {
	result := SaveItemResult{Context: request.Context, Problems: []FieldProblem{}}
	// A submission that names no click is refused before anything is
	// recorded.
	if !catalog.ValidToken(request.IntentID) {
		result.refuse(Failed, "a save names the click it was submitted by")
		return result
	}
	loaded, declined := a.loadCatalog(ctx, request.Context, true)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	if request.Kind == ProjectItem {
		return a.saveProjectSettings(loaded, request)
	}
	return a.saveCaseDetails(loaded, request)
}

// invalid answers a draft with problems; nothing was written.
func (r SaveItemResult) invalid(problems []FieldProblem) SaveItemResult {
	r.State, r.Outcome, r.Problems = Failed, InvalidOutcome, problems
	r.Reason = "the draft has problems to fix; nothing was saved"
	return r
}

// conflict answers a stale base; nothing was written and the draft is kept.
func (r SaveItemResult) conflict(current string) SaveItemResult {
	r.State, r.Outcome, r.CurrentRevision = Failed, ConflictOutcome, current
	r.Reason = "the object changed since this edit began; nothing was saved and the draft is kept"
	return r
}

// ownerRule says what an owner may be in a project of schema.
func ownerRule(schema string) string {
	if schema == project.SchemaV2 {
		return "an owner is 1 to 100 characters of text with no control characters"
	}
	return "a readmit-project/v1 owner is up to 64 letters, digits, '.', '_' and '-'"
}

// listRule says what tags or incidents may be in a project of schema.
func listRule(schema string, limit int, what string) string {
	if schema == project.SchemaV2 {
		return what + " are up to " + strconv.Itoa(limit) + " entries of 1 to 64 characters of text with no control characters"
	}
	return "a readmit-project/v1 project's " + what + " are up to " + strconv.Itoa(limit) + " entries of letters, digits, '.', '_' and '-'"
}

// planCase normalizes a case's details and reports every problem at its
// member, by the rules the project document holds them to.
func planCase(document project.Document, submitted *CaseDraft) (CaseDraft, []FieldProblem) {
	problems := []FieldProblem{}
	if submitted == nil {
		return CaseDraft{}, append(problems, FieldProblem{Field: "case", Problem: "a case's details are its name, status, owner, tags, revision and incidents"})
	}
	draft := CaseDraft{Name: strings.TrimSpace(submitted.Name), Status: submitted.Status, Owner: strings.TrimSpace(submitted.Owner),
		Tags: sortedSet(submitted.Tags), InterfaceRevision: strings.TrimSpace(submitted.InterfaceRevision), Incidents: sortedSet(submitted.Incidents)}
	if !validName(draft.Name, func(name string) error { return project.CheckTitle(document.Schema, name) }) {
		problems = append(problems, FieldProblem{Field: "case.name", Problem: nameRule})
	}
	if !slices.Contains([]project.Status{project.StatusOpen, project.StatusInvestigating, project.StatusResolved, project.StatusClosed}, draft.Status) {
		problems = append(problems, FieldProblem{Field: "case.status", Problem: "a status is Open, Investigating, Resolved or Closed"})
	}
	if draft.Owner != "" && project.CheckOwner(document.Schema, draft.Owner) != nil {
		problems = append(problems, FieldProblem{Field: "case.owner", Problem: ownerRule(document.Schema)})
	}
	if err := project.CheckTags(document.Schema, draft.Tags); err != nil {
		problems = append(problems, FieldProblem{Field: "case.tags", Problem: listRule(document.Schema, project.MaxTags, "tags")})
	}
	if err := project.CheckIncidents(document.Schema, draft.Incidents); err != nil {
		problems = append(problems, FieldProblem{Field: "case.incidents", Problem: listRule(document.Schema, project.MaxIncidents, "incidents")})
	}
	switch {
	case draft.InterfaceRevision != "" && !document.Declares(draft.InterfaceRevision):
		problems = append(problems, FieldProblem{Field: "case.interface_revision", Problem: "the revision is not one this project declares"})
	case draft.InterfaceRevision == "" && document.Schema == project.Schema:
		problems = append(problems, FieldProblem{Field: "case.interface_revision",
			Problem: "a readmit-project/v1 project assigns every case a revision; convert the project to leave one unassigned"})
	}
	return draft, problems
}

// saveCaseDetails saves the whole of one case's details: a registered case's
// metadata replaced, or an unregistered case registered under the identity
// it already has. The evidence facts, the entry and the catalog identity
// never change.
func (a *App) saveCaseDetails(loaded *loadedCatalog, request SaveItemRequest) SaveItemResult {
	result := SaveItemResult{Context: request.Context, Problems: []FieldProblem{}}
	item := loaded.listedItem(CaseItem, request.Item)
	if item == nil {
		result.refuse(Failed, "the project holds no such case; a new case is imported, never saved")
		return result
	}
	draft, problems := planCase(loaded.project.Document, request.Draft.Case)
	if len(problems) > 0 {
		return result.invalid(problems)
	}
	digest := submissionOf(string(CaseItem), request.Item, request.BaseRevision, draft)
	repeated, declined := a.intents.claim(request.IntentID, digest)
	if declined.state != "" {
		result.refuse(declined.state, declined.reason)
		return result
	}
	registered := loaded.registeredCase(item.ID)
	current := ""
	if registered != nil {
		current = caseRevision(*registered, item.Name)
	}
	switch {
	case repeated:
		// The same click, answered already in this process.
		result.Replayed = true
	case request.BaseRevision != current:
		// A stale base is a conflict, unless the case is already exactly what
		// this submission asks for: a retry after its answer was lost.
		if registered == nil || caseRevision(withDetails(*registered, draft), "") != current {
			return result.conflict(current)
		}
		result.Replayed = true
	default:
		stamped, err := a.writeCaseDetails(loaded, *item, registered, draft)
		if err != nil {
			return result.written(loaded.root, err)
		}
		if !stamped {
			result.refuse(Failed, "the case's details were saved, and the project's catalog could not record when")
			return result
		}
	}
	a.intents.record(request.IntentID, digest)
	return savedCase(result, loaded.root, item.ID, item.Entry, draft)
}

// listedItem is the object of kind with id the project holds and lists, or
// nil.
func (c *loadedCatalog) listedItem(kind ItemKind, id string) *catalog.Item {
	index := c.document.Find(id)
	if index < 0 || c.document.Items[index].Kind != string(kind) || c.removed(c.document.Items[index]) {
		return nil
	}
	return &c.document.Items[index]
}

// writeCaseDetails writes a case's details through the operation `readmit
// project update` runs, or registers the case through the one `readmit
// project add` runs, and records in the catalog when the application changed
// it; its name is the recorded title from then on. It answers the write's
// own error, and whether the catalog recorded the change.
func (a *App) writeCaseDetails(loaded *loadedCatalog, item catalog.Item, registered *project.Case, draft CaseDraft) (bool, error) {
	var err error
	if registered != nil {
		_, err = operation.UpdateRegisteredCase(loaded.root, registered.Name, operation.CaseChange{Title: &draft.Name, Owner: &draft.Owner,
			Status: &draft.Status, InterfaceVersion: &draft.InterfaceRevision, Tags: &draft.Tags, Incidents: &draft.Incidents})
	} else {
		_, err = operation.RegisterCase(loaded.root, item.Entry, operation.CaseRegistration{Title: draft.Name, Owner: draft.Owner,
			Status: draft.Status, InterfaceVersion: draft.InterfaceRevision, Tags: draft.Tags, Incidents: draft.Incidents})
	}
	if err != nil {
		return false, err
	}
	_, err = loaded.store.Update(a.now(), func(document *catalog.Document) (bool, error) {
		at := document.Find(item.ID)
		if at < 0 {
			return false, catalog.ErrNoItem
		}
		document.Items[at].Name, document.Items[at].UpdatedAt = "", catalog.Stamp(a.now())
		return true, nil
	})
	return err == nil, nil
}

// savedCase answers a saved case as the project document now records it.
func savedCase(result SaveItemResult, root, id, entry string, draft CaseDraft) SaveItemResult {
	opened, err := project.Open(root)
	if err != nil {
		result.refuse(Failed, "the case's details were saved, and the project document cannot be read again")
		return result
	}
	stored := slices.IndexFunc(opened.Document.Cases, func(held project.Case) bool { return held.Name == entry })
	if stored < 0 {
		result.refuse(Failed, "the case's details were saved, and the project no longer registers it")
		return result
	}
	projection := draft
	projection.Owner, projection.InterfaceRevision = opened.Document.Cases[stored].Owner, opened.Document.Cases[stored].InterfaceVersion
	result.State, result.Outcome, result.Projection = Completed, SavedOutcome, &ItemDraft{Case: &projection}
	result.Saved = &ItemRef{Kind: CaseItem, ID: id, Revision: caseRevision(opened.Document.Cases[stored], "")}
	return result
}

// withDetails is a registered case with a draft's details.
func withDetails(registered project.Case, draft CaseDraft) project.Case {
	registered.Title, registered.Status, registered.Owner, registered.InterfaceVersion = draft.Name, draft.Status, draft.Owner, draft.InterfaceRevision
	registered.Tags, registered.Incidents = draft.Tags, draft.Incidents
	return registered
}

// written answers a project document write that failed: a change the project
// refused, in the project's words, or a folder this account cannot write.
func (r SaveItemResult) written(root string, err error) SaveItemResult {
	if detail, ok := operation.InvalidDetail(err); ok {
		return r.invalid([]FieldProblem{{Field: "project", Problem: detail}})
	}
	declined := writeRefusal(root, err)
	r.refuse(declined.state, declined.reason+"; nothing was saved")
	return r
}

// planProject composes the project document a settings draft asks for, and
// every problem with it, from the project as it is now.
func (c *loadedCatalog) planProject(submitted *ProjectDraft) (project.Document, ProjectDraft, []FieldProblem) {
	problems := []FieldProblem{}
	if submitted == nil {
		return project.Document{}, ProjectDraft{}, append(problems, FieldProblem{Field: "project", Problem: "a project's settings are its name, owner, tags and revisions"})
	}
	current := c.project.Document
	v1 := current.Schema == project.Schema
	draft := ProjectDraft{Name: strings.TrimSpace(submitted.Name), Owner: strings.TrimSpace(submitted.Owner), Tags: sortedSet(submitted.Tags),
		Revisions: []RevisionDraft{}, Reassign: submitted.Reassign}
	title := func(name string) error { return project.CheckTitle(current.Schema, name) }
	if !validName(draft.Name, title) {
		problems = append(problems, FieldProblem{Field: "project.name", Problem: nameRule})
	}
	if draft.Owner != "" && project.CheckOwner(current.Schema, draft.Owner) != nil {
		problems = append(problems, FieldProblem{Field: "project.owner", Problem: ownerRule(current.Schema)})
	}
	switch {
	case v1 && len(draft.Tags) > 0:
		problems = append(problems, FieldProblem{Field: "project.tags", Problem: "a readmit-project/v1 project holds no tags; convert the project to tag it"})
	case project.CheckTags(current.Schema, draft.Tags) != nil:
		problems = append(problems, FieldProblem{Field: "project.tags", Problem: listRule(current.Schema, project.MaxTags, "tags")})
	}
	// Every revision kept, in order; a new one is given an identity no
	// revision of this project has had.
	taken := slices.Clone(current.InterfaceVersions)
	kept := []string{}
	names := []project.VersionName{}
	defaults := 0
	for i, revision := range submitted.Revisions {
		field := "project.revisions[" + strconv.Itoa(i) + "]"
		name := strings.TrimSpace(revision.Name)
		id := strings.TrimSpace(revision.ID)
		if !validName(name, func(name string) error { return project.CheckTitle(project.SchemaV2, name) }) || v1 && title(name) != nil {
			problems = append(problems, FieldProblem{Field: field + ".name", Problem: nameRule})
			continue
		}
		switch {
		case id != "" && !current.Declares(id):
			problems = append(problems, FieldProblem{Field: field, Problem: "the revision is not one this project declares"})
			continue
		case id != "" && slices.Contains(kept, id):
			problems = append(problems, FieldProblem{Field: field, Problem: "the revision is listed twice"})
			continue
		case id != "" && v1 && name != id:
			problems = append(problems, FieldProblem{Field: field + ".name", Problem: "a readmit-project/v1 project names a revision by its identifier; convert the project to rename one"})
			continue
		case id == "" && v1:
			if project.CheckIdentifier(name) != nil || slices.Contains(taken, name) {
				problems = append(problems, FieldProblem{Field: field + ".name",
					Problem: "a readmit-project/v1 revision is named by a new identifier of letters, digits, '.', '_' and '-'; convert the project to name it freely"})
				continue
			}
			id = name
		case id == "":
			id = freeRevisionID(name, taken)
		}
		taken = append(taken, id)
		kept = append(kept, id)
		if name != id {
			names = append(names, project.VersionName{Version: id, Name: name})
		}
		if revision.Default {
			defaults++
		}
		draft.Revisions = append(draft.Revisions, RevisionDraft{ID: id, Name: name, Default: revision.Default})
	}
	if defaults > 1 {
		problems = append(problems, FieldProblem{Field: "project.revisions", Problem: "at most one revision is the default"})
	}
	removed := slices.DeleteFunc(slices.Clone(current.InterfaceVersions), func(version string) bool { return slices.Contains(kept, version) })
	if v1 && len(removed) > 0 {
		problems = append(problems, FieldProblem{Field: "project.revisions", Problem: "a readmit-project/v1 project never removes a revision; convert the project to remove one"})
	}
	moves := map[string]string{}
	for i, move := range draft.Reassign {
		field := "project.reassign[" + strconv.Itoa(i) + "]"
		switch {
		case slices.ContainsFunc(draft.Reassign[:i], func(earlier Reassignment) bool { return earlier.From == move.From }):
			problems = append(problems, FieldProblem{Field: field, Problem: "the cases of one revision are reassigned once"})
		case !slices.Contains(removed, move.From):
			problems = append(problems, FieldProblem{Field: field, Problem: "only the cases of a revision this save removes are reassigned"})
		case move.To != "" && (!current.Declares(move.To) || !slices.Contains(kept, move.To)):
			problems = append(problems, FieldProblem{Field: field, Problem: "cases are reassigned to a revision the project keeps"})
		case move.To == "" && v1:
			problems = append(problems, FieldProblem{Field: field, Problem: "a readmit-project/v1 project assigns every case a revision"})
		default:
			moves[move.From] = move.To
		}
	}
	// A revision a case is still assigned to is never removed quietly.
	var referring []Referrer
	updated := current
	updated.Cases = slices.Clone(current.Cases)
	for i, registered := range updated.Cases {
		if !slices.Contains(removed, registered.InterfaceVersion) {
			continue
		}
		to, moved := moves[registered.InterfaceVersion]
		if !moved {
			if referrer := c.caseReferrer(registered); referrer != nil {
				referring = append(referring, *referrer)
			}
			continue
		}
		updated.Cases[i].InterfaceVersion = to
	}
	if len(referring) > 0 {
		problems = append(problems, FieldProblem{Field: "project.revisions", Problem: "cases are still assigned to a revision this save removes; reassign them or keep it",
			Referring: referring})
	}
	if len(problems) > 0 {
		return project.Document{}, draft, problems
	}
	slices.SortFunc(names, func(x, y project.VersionName) int { return strings.Compare(x.Version, y.Version) })
	updated.Settings = project.Settings{Title: draft.Name, DefaultOwner: draft.Owner}
	if !v1 {
		updated.Settings.Tags = draft.Tags
		updated.InterfaceVersionNames = names
	}
	for _, revision := range draft.Revisions {
		if revision.Default {
			updated.Settings.DefaultInterfaceVersion = revision.ID
		}
	}
	if len(updated.Settings.Tags) == 0 {
		updated.Settings.Tags = nil
	}
	updated.InterfaceVersions = kept
	if err := project.Validate(updated); err != nil {
		return project.Document{}, draft, append(problems, FieldProblem{Field: "project", Problem: err.Error()})
	}
	return updated, draft, problems
}

// caseReferrer names a registered case as the catalog lists it: its
// reference, and the name a person gave it here or its recorded title.
func (c *loadedCatalog) caseReferrer(registered project.Case) *Referrer {
	index := c.document.ByEntry(string(CaseItem), registered.Name)
	if index < 0 {
		return nil
	}
	item := c.document.Items[index]
	return &Referrer{Ref: ItemRef{Kind: CaseItem, ID: item.ID, Revision: caseRevision(registered, item.Name)}, Name: cmp.Or(item.Name, registered.Title)}
}

// freeRevisionID is a new revision identity made from its name: the name's
// letters, digits, '.', '_' and '-', then -2, -3 and so on until no revision
// of the project has had it.
func freeRevisionID(name string, taken []string) string {
	var base strings.Builder
	dash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_':
			base.WriteRune(r)
			dash = false
		case !dash && base.Len() > 0:
			base.WriteByte('-')
			dash = true
		}
		if base.Len() >= 48 {
			break
		}
	}
	stem := strings.Trim(base.String(), "-.")
	if stem == "" {
		stem = "revision"
	}
	for i := 1; ; i++ {
		candidate := stem
		if i > 1 {
			candidate += "-" + strconv.Itoa(i)
		}
		if !slices.Contains(taken, candidate) {
			return candidate
		}
	}
}

// saveProjectSettings saves the whole of a project's settings into its
// document. The folder is never moved or renamed.
func (a *App) saveProjectSettings(loaded *loadedCatalog, request SaveItemRequest) SaveItemResult {
	result := SaveItemResult{Context: request.Context, Problems: []FieldProblem{}}
	if request.Item != loaded.document.Project.ID {
		result.refuse(Failed, "a project's settings are saved in that project")
		return result
	}
	updated, draft, problems := loaded.planProject(request.Draft.Project)
	if len(problems) > 0 {
		return result.invalid(problems)
	}
	digest := submissionOf(string(ProjectItem), request.Item, request.BaseRevision, draft)
	repeated, declined := a.intents.claim(request.IntentID, digest)
	if declined.state != "" {
		result.refuse(declined.state, declined.reason)
		return result
	}
	current := projectRevision(loaded.project.Document)
	switch {
	case repeated:
		result.Replayed = true
		updated = loaded.project.Document
	case request.BaseRevision != current:
		if projectRevision(updated) != current {
			return result.conflict(current)
		}
		result.Replayed = true
	default:
		if err := operation.ReplaceProjectDocument(loaded.root, updated); err != nil {
			return result.written(loaded.root, err)
		}
	}
	a.intents.record(request.IntentID, digest)
	a.rememberProject(loaded.document.Project.ID, loaded.root, draft.Name)
	result.State, result.Outcome, result.Projection = Completed, SavedOutcome, &ItemDraft{Project: &draft}
	result.Saved = &ItemRef{Kind: ProjectItem, ID: loaded.document.Project.ID, Revision: projectRevision(updated)}
	return result
}

// RemoveCaseFromProject takes one case off the project: the catalog records
// that the case was removed, so it is no longer listed or discovered again,
// and then a registered case's registration is removed from the project
// document. Its files stay exactly where they are; deleting evidence is a
// different act. A case the project still registers is listed whatever the
// catalog records, so a removal whose second write fails leaves the case
// listed, registered and removable again, and says so. A case a note or a
// registered variant of the project names is refused before anything is
// written, since removing it would leave that note or lineage naming
// evidence the project no longer holds.
func (a *App) RemoveCaseFromProject(request ItemRequest) ItemResult {
	return run(a, false, true, func(ctx context.Context) ItemResult {
		result := ItemResult{Context: request.Context}
		if request.Ref.Kind != CaseItem {
			result.refuse(Failed, "only a case is removed from a project")
			return result
		}
		loaded, _, refused := a.catalogItem(ctx, request.Context, request.Ref, true)
		if loaded == nil {
			return refused
		}
		registered := loaded.registeredCase(request.Ref.ID)
		if registered != nil {
			if _, err := project.RemoveCase(loaded.project.Document, loaded.revisions, registered.Name); errors.Is(err, project.ErrCaseNamed) {
				result.refuse(Failed, "a note or a variant of this project names this case; remove the note or the variant first. Nothing was removed")
				return result
			}
		}
		if _, err := loaded.store.Update(a.now(), func(document *catalog.Document) (bool, error) {
			at := document.Find(request.Ref.ID)
			if at < 0 {
				return false, catalog.ErrNoItem
			}
			document.Items[at].RemovedAt = catalog.Stamp(a.now())
			return true, nil
		}); err != nil {
			result.refuse(Failed, "the project's catalog could not record that the case was removed; nothing was removed")
			return result
		}
		if registered != nil {
			if err := operation.UnregisterCase(loaded.root, registered.Name); err != nil {
				declined := writeRefusal(loaded.root, err)
				result.refuse(declined.state, declined.reason+"; the case is still registered and still listed, so it can be removed again")
				return result
			}
		}
		result.State = Completed
		return result
	})
}
