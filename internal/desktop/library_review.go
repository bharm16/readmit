package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/assertionauthor"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/scenariogen"
)

// A library object is reviewed where it is kept: two published versions of a
// profile are compared by the object's own revisions, a draft is shown and
// edited as the exact document its Save writes, and a profile is evaluated
// against a case of the project. Each is a read; nothing here writes.

// ProfileVersionsRequest names two published versions of one saved profile:
// Ref at the revision the comparison ends at, its current one when none is
// named, and From, the profile version the comparison starts from, as a
// test's pin records it.
type ProfileVersionsRequest struct {
	Context RequestContext `json:"context"`
	Ref     ItemRef        `json:"ref"`
	From    string         `json:"from"`
}

// ProfileComparisonResult is what differs between two versions of one
// profile, rule by rule.
type ProfileComparisonResult struct {
	State      State                      `json:"state"`
	Reason     string                     `json:"reason,omitzero"`
	Context    RequestContext             `json:"context"`
	Comparison *profileversion.Comparison `json:"comparison,omitzero"`
}

func (r *ProfileComparisonResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// CompareProfileVersions reports what changed between a version of a saved
// profile and a later one, from the versions the object itself published: the
// review an upgrade of a test's pin starts from. It moves no pin.
func (a *App) CompareProfileVersions(request ProfileVersionsRequest) ProfileComparisonResult {
	return run(a, false, false, func(ctx context.Context) ProfileComparisonResult {
		result := ProfileComparisonResult{Context: request.Context}
		if request.Ref.Kind != ProfileItem {
			result.refuse(Failed, "two versions of a profile are compared")
			return result
		}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Ref, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		whole := loaded.document.Items[loaded.document.Find(item.Ref.ID)]
		record, held := itemAt(whole, request.Ref.Revision)
		if !held {
			result.refuse(Failed, "the profile has no revision "+request.Ref.Revision)
			return result
		}
		paths, availability, reason := loaded.backing(record)
		if availability != ItemAvailable {
			result.refuse(Failed, reason)
			return result
		}
		to, _, err := readLocalProfile(paths)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		earlier := (draftScope{root: loaded.root, loaded: loaded, item: whole.ID}).earlierProfiles()
		at := slices.IndexFunc(earlier, func(profile localprofile.Profile) bool { return profile.Identity.Version == request.From })
		if at < 0 {
			result.refuse(Failed, "version "+request.From+" of this profile is not in the project, so nothing can say what changed since")
			return result
		}
		comparison, err := profileversion.Compare(earlier[at], to)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		result.State, result.Comparison = Completed, &comparison
		return result
	})
}

// LibraryDocumentResult is the document a library draft is saved as, as text
// an advanced editor shows: a check group's assertion set with every check it
// holds, a profile's local profile document, and a scenario's generator plan.
type LibraryDocumentResult struct {
	State    State          `json:"state"`
	Reason   string         `json:"reason,omitzero"`
	Context  RequestContext `json:"context"`
	Schema   string         `json:"schema,omitzero"`
	Document string         `json:"document,omitzero"`
}

func (r *LibraryDocumentResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// LibraryDocument answers the document one library draft holds, for Edit
// JSON. A check group's unsupported checks are written back exactly where
// they stood, so none is lost; a profile's origin and chosen pack stay with
// the draft, beside the document. It reads and writes nothing.
func (a *App) LibraryDocument(request DraftRequest) LibraryDocumentResult {
	return run(a, false, false, func(context.Context) LibraryDocumentResult {
		result := LibraryDocumentResult{Context: request.Context}
		draft := request.Draft
		var document any
		switch {
		case request.Kind == CheckGroupItem && draft.CheckGroup != nil:
			data, err := checkGroupDocument(draft.Name, *draft.CheckGroup)
			if err != nil {
				result.refuse(Failed, err.Error())
				return result
			}
			result.State, result.Schema, result.Document = Completed, assertion.Schema, string(data)
			return result
		case request.Kind == ProfileItem && draft.Profile != nil:
			profile := draft.Profile.Profile
			if profile.Schema == "" {
				profile.Schema = localprofile.Schema
			}
			// A profile that holds to its contract is shown as the canonical
			// document its Save writes; one that does not yet, as held.
			if _, canonical, err := localprofile.Canonical(profile); err == nil {
				result.State, result.Schema, result.Document = Completed, profile.Schema, string(canonical)
				return result
			}
			document = profile
		case request.Kind == ScenarioItem && draft.Scenario != nil:
			plan, err := scenarioPlanOf(draft)
			if err != nil {
				result.refuse(Failed, err.Error())
				return result
			}
			document = plan
		default:
			result.refuse(Failed, "a check group, a profile or a scenario draft is shown as its document")
			return result
		}
		data, err := json.Marshal(document, json.Deterministic(true), jsontext.WithIndent("  "))
		if err != nil {
			result.refuse(Failed, "the draft cannot be written as its document")
			return result
		}
		var declared struct {
			Schema string `json:"schema"`
		}
		_ = json.Unmarshal(data, &declared)
		result.State, result.Schema, result.Document = Completed, declared.Schema, string(data)+"\n"
		return result
	})
}

// checkGroupDocument is a check group's assertion set with its unsupported
// checks back at the positions they were read from. The checks this release
// evaluates are written as a save writes them; a group that does not yet hold
// to the contract is written as held, so it can be corrected here.
func checkGroupDocument(name string, group CheckGroupDraft) ([]byte, error) {
	set := group.Set
	if set.Schema == "" {
		set.Schema = assertionauthor.Schema
	}
	if set.Name == "" {
		set.Name = name
	}
	document := struct {
		Schema     string           `json:"schema"`
		Name       string           `json:"name"`
		Assertions []jsontext.Value `json:"assertions"`
	}{Schema: assertion.Schema, Name: set.Name, Assertions: []jsontext.Value{}}
	if generated, err := assertionauthor.Generate(set); err != nil || json.Unmarshal(generated, &document) != nil {
		document.Assertions = []jsontext.Value{}
		for _, clause := range set.Assertions {
			encoded, err := json.Marshal(clause, json.Deterministic(true))
			if err != nil {
				return nil, errors.New("a check cannot be written as its document")
			}
			document.Assertions = append(document.Assertions, encoded)
		}
	}
	unsupported := slices.Clone(group.Unsupported)
	slices.SortStableFunc(unsupported, func(x, y assertionauthor.UnsupportedClause) int { return x.Position - y.Position })
	for _, clause := range unsupported {
		at := min(max(clause.Position, 0), len(document.Assertions))
		document.Assertions = slices.Insert(document.Assertions, at, jsontext.Value(clause.Raw))
	}
	data, err := json.Marshal(document)
	if err == nil {
		err = (*jsontext.Value)(&data).Indent(jsontext.WithIndentPrefix(""), jsontext.WithIndent("  "))
	}
	if err != nil {
		return nil, errors.New("the check group cannot be written as its document")
	}
	return append(data, '\n'), nil
}

// scenarioPlanOf is the plan a scenario draft saves: its typed workflow, when
// it holds one, written into the plan's template.
func scenarioPlanOf(draft ItemDraft) (scenariogen.Plan, error) {
	if _, held, problems := validateScenarioDraft(draft, ""); len(problems) == 0 {
		return held.Plan, nil
	}
	plan := draft.Scenario.Plan
	if template := draft.Scenario.Template; template != nil {
		encoded, err := json.Marshal(*template, json.Deterministic(true))
		if err != nil {
			return scenariogen.Plan{}, errors.New("the workflow cannot be encoded")
		}
		plan.Template = encoded
	}
	return plan, nil
}

// LibraryDocumentRequest is text a person edited as one library draft's
// document, and the draft it replaces the document of.
type LibraryDocumentRequest struct {
	Context  RequestContext `json:"context"`
	Kind     ItemKind       `json:"kind"`
	Draft    ItemDraft      `json:"draft"`
	Document string         `json:"document"`
}

// ApplyLibraryDocument reads edited document text strictly into the draft it
// was shown from: the text replaces the draft's document and nothing else, so
// a profile keeps its chosen pack and origin and every draft keeps its name.
// A check the release does not evaluate is kept as unsupported, never
// dropped. Text that does not read leaves the draft as it was and answers the
// reader's reason at the document. It writes nothing; the editor's one Save
// publishes the result.
func (a *App) ApplyLibraryDocument(request LibraryDocumentRequest) ItemDraftResult {
	return run(a, false, false, func(context.Context) ItemDraftResult {
		result := ItemDraftResult{Context: request.Context}
		draft := request.Draft
		data := []byte(request.Document)
		var err error
		switch request.Kind {
		case CheckGroupItem:
			var set assertionauthor.Draft
			var unsupported []assertionauthor.UnsupportedClause
			if set, unsupported, err = assertionauthor.ReadLenient(data); err == nil {
				group := CheckGroupDraft{Set: set, Unsupported: unsupported}
				if draft.CheckGroup != nil {
					group.Names = namesFor(draft.CheckGroup.Names, group)
				}
				draft.CheckGroup = &group
			}
		case ProfileItem:
			var profile localprofile.Profile
			if profile, err = localprofile.Decode(data); err == nil {
				held := ProfileDraft{}
				if draft.Profile != nil {
					held = *draft.Profile
				}
				held.Profile, draft.Profile = profile, &held
			}
		case ScenarioItem:
			var plan scenariogen.Plan
			if schemaOf(data) != scenariogen.Schema {
				err = errors.New("the document is a " + scenariogen.Schema + " plan")
			} else if plan, err = scenariogen.Decode(data); err == nil && plan.Seed > maxWindowSeed {
				err = errors.New("a seed is a whole number no larger than this window carries exactly")
			}
			if err == nil {
				held := scenarioDraftOf(plan)
				if draft.Scenario != nil {
					held.Profile = draft.Scenario.Profile
				}
				draft.Scenario = held
			}
		default:
			result.refuse(Failed, "a check group, a profile or a scenario is edited as its document")
			return result
		}
		if err != nil {
			result.State, result.Draft = Failed, &request.Draft
			result.Reason = err.Error()
			result.Problems = []FieldProblem{{Field: "document", Problem: err.Error()}}
			return result
		}
		result.State, result.Draft = Completed, &draft
		return result
	})
}

// ProfileEvaluationRequest is one profile evaluated against one case of the
// project: the profile at the revision named, the metadata pack it is
// evaluated with, and whether the person declares the case captured every
// prerequisite workflow event. Pack may be left out for a local profile whose
// pinned pack the project holds.
type ProfileEvaluationRequest struct {
	Context         RequestContext `json:"context"`
	Profile         ItemRef        `json:"profile"`
	Pack            *ItemRef       `json:"pack,omitzero"`
	Case            ItemRef        `json:"case"`
	CompleteCapture bool           `json:"complete_capture"`
}

// ProfileEvaluationResult is the report `readmit profile evaluate` writes for
// the same profile, pack and case.
type ProfileEvaluationResult struct {
	State   State               `json:"state"`
	Reason  string              `json:"reason,omitzero"`
	Context RequestContext      `json:"context"`
	Report  *profileeval.Report `json:"report,omitzero"`
}

func (r *ProfileEvaluationResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// EvaluateProfile evaluates a profile's exact constraints against every
// message a verified case of the project retained, as `readmit profile
// evaluate` does. A v2 profile the library cannot edit is still evaluated. It
// reads; nothing is sent and nothing is written.
func (a *App) EvaluateProfile(request ProfileEvaluationRequest) ProfileEvaluationResult {
	return run(a, true, false, func(ctx context.Context) ProfileEvaluationResult {
		result := ProfileEvaluationResult{Context: request.Context}
		if request.Profile.Kind != ProfileItem || request.Case.Kind != CaseItem || request.Pack != nil && request.Pack.Kind != ProfileItem {
			result.refuse(Failed, "a profile is evaluated against a case, with a metadata pack")
			return result
		}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		profile, err := loaded.memberBytes(request.Profile, profileRole, profileeval.MaxBytes)
		if err != nil {
			result.refuse(Failed, "the profile cannot be read: "+err.Error())
			return result
		}
		if !strings.HasPrefix(schemaOf(profile), "readmit-local-profile/") {
			result.refuse(Failed, "a local profile is evaluated; a metadata pack or a package is not")
			return result
		}
		var pack []byte
		switch {
		case request.Pack != nil:
			pack, err = loaded.packBytes(*request.Pack)
		default:
			declared, decodeErr := profileeval.DecodeProfile(profile)
			if decodeErr != nil {
				result.refuse(Failed, "choose the metadata pack this profile is evaluated with")
				return result
			}
			ref, _ := loaded.pinnedPack(declared.Definition.Base.Pack)
			if ref == nil {
				result.refuse(Failed, "the metadata pack this profile pins is not in the project")
				return result
			}
			pack, err = loaded.packBytes(*ref)
		}
		if err != nil {
			result.refuse(Failed, "the metadata pack cannot be read: "+err.Error())
			return result
		}
		index := loaded.document.Find(request.Case.ID)
		if index < 0 || loaded.document.Items[index].Kind != string(CaseItem) || loaded.removed(loaded.document.Items[index]) {
			result.refuse(Failed, "the project holds no such case")
			return result
		}
		paths, availability, reason := loaded.backing(loaded.document.Items[index])
		if availability != ItemAvailable {
			result.refuse(Failed, reason)
			return result
		}
		report, err := profileeval.EvaluateBundle(ctx, profile, pack, paths[primaryRole(CaseItem)], profileeval.Options{CompleteCapture: request.CompleteCapture})
		if err != nil {
			result.refuse(refusalState(err), err.Error())
			return result
		}
		result.State, result.Report = Completed, &report
		return result
	})
}

// memberBytes is one member of a profile object at the revision its reference
// names, as bytes: whatever version of its contract the file declares.
func (c *loadedCatalog) memberBytes(ref ItemRef, role string, limit int) ([]byte, error) {
	index := c.document.Find(ref.ID)
	if index < 0 || c.document.Items[index].Kind != string(ref.Kind) || c.removed(c.document.Items[index]) {
		return nil, errors.New("the project holds no such object")
	}
	record, held := itemAt(c.document.Items[index], ref.Revision)
	if !held {
		return nil, errors.New("the object has no revision " + ref.Revision)
	}
	paths, availability, reason := c.backing(record)
	if availability != ItemAvailable {
		return nil, errors.New(reason)
	}
	return boundedFile(paths[role], limit)
}

// schemaOf is the contract a document declares.
func schemaOf(data []byte) string {
	var declared struct {
		Schema string `json:"schema"`
	}
	_ = json.Unmarshal(data, &declared)
	return declared.Schema
}

// ProfilePinsRequest upgrades the pin of each chosen test to one reviewed
// version of a profile: Profile names the profile at the revision reviewed,
// and each test names the revision of it that was reviewed. IntentID names
// the submission, so a repeated click upgrades once.
type ProfilePinsRequest struct {
	Context  RequestContext `json:"context"`
	Profile  ItemRef        `json:"profile"`
	Tests    []ItemRef      `json:"tests"`
	IntentID string         `json:"intent_id"`
}

// RefusedPin is one chosen test whose pin was not moved, and why.
type RefusedPin struct {
	Ref    ItemRef `json:"ref"`
	Reason string  `json:"reason"`
}

// ProfilePinsResult is the new revision of each test whose pin moved, and
// each test refused with its reason.
type ProfilePinsResult struct {
	State    State          `json:"state"`
	Reason   string         `json:"reason,omitzero"`
	Context  RequestContext `json:"context"`
	Upgraded []ItemRef      `json:"upgraded"`
	Refused  []RefusedPin   `json:"refused"`
}

func (r *ProfilePinsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// maxPinUpgrades bounds the tests one upgrade moves.
const maxPinUpgrades = 64

// upgradeRationale is what each new release records as the reason for its
// approval.
const upgradeRationale = "profile pin upgraded after reviewing the changes between the two versions"

// UpgradeProfilePins moves each chosen test's pin of one profile to the
// reviewed version of that profile, as a new release of the test whose parent
// is the release reviewed: its expectations are unchanged and only that pin
// moves, approved by the local reviewer, exactly as `readmit expectation
// approve` would approve it. A test whose revision changed since it was
// reviewed, or that pins no version of this profile, is refused and left as
// it is; nothing moves any other test.
func (a *App) UpgradeProfilePins(request ProfilePinsRequest) ProfilePinsResult {
	return run(a, false, true, func(ctx context.Context) ProfilePinsResult {
		result := ProfilePinsResult{Context: request.Context, Upgraded: []ItemRef{}, Refused: []RefusedPin{}}
		switch {
		case request.Profile.Kind != ProfileItem || len(request.Tests) == 0 || len(request.Tests) > maxPinUpgrades:
			result.refuse(Failed, "choose from 1 to "+strconv.Itoa(maxPinUpgrades)+" tests to upgrade to a reviewed version of a profile")
			return result
		case !catalog.ValidToken(request.IntentID) || len(request.IntentID) > 64:
			result.refuse(Failed, "an upgrade is made by one submission")
			return result
		}
		if err := a.admitAuthor(); err != nil {
			result.refuse(PermissionDenied, err.Error())
			return result
		}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Profile, true)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		record, held := itemAt(loaded.document.Items[loaded.document.Find(item.Ref.ID)], request.Profile.Revision)
		if !held {
			result.refuse(Failed, "the profile has no revision "+request.Profile.Revision)
			return result
		}
		paths, availability, reason := loaded.backing(record)
		if availability != ItemAvailable {
			result.refuse(Failed, reason)
			return result
		}
		profile, _, err := readLocalProfile(paths)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		seal, err := profileversion.Seal(profile)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		for _, test := range request.Tests {
			upgraded, reason := a.upgradePin(loaded, test, seal, request.IntentID)
			if upgraded == nil {
				result.Refused = append(result.Refused, RefusedPin{Ref: test, Reason: reason})
				continue
			}
			result.Upgraded = append(result.Upgraded, *upgraded)
		}
		result.State = Completed
		if len(result.Upgraded) == 0 {
			result.refuse(Failed, "no test was upgraded")
		}
		return result
	})
}

// upgradePin publishes one new revision of a test release whose pin of the
// sealed profile moves to that seal, from the revision reviewed.
func (a *App) upgradePin(loaded *loadedCatalog, test ItemRef, seal profileversion.Version, intent string) (*ItemRef, string) {
	index := loaded.document.Find(test.ID)
	if test.Kind != TestItem || index < 0 || loaded.document.Items[index].Kind != string(TestItem) || loaded.removed(loaded.document.Items[index]) {
		return nil, "the project holds no such test"
	}
	current := loaded.document.Items[index]
	// The same submission repeated is the revision it already published.
	for _, revision := range current.Revisions {
		if revision.Intent == intent+"-"+current.ID {
			return &ItemRef{Kind: TestItem, ID: current.ID, Revision: strconv.Itoa(revision.Number)}, ""
		}
	}
	reviewed, held := itemAt(current, test.Revision)
	if !held {
		return nil, "the test has no revision " + test.Revision
	}
	paths, availability, reason := loaded.backing(reviewed)
	if availability != ItemAvailable {
		return nil, reason
	}
	path := paths[primaryRole(TestItem)]
	if !declares(path, expectation.Schema) {
		return nil, "this test pins no profile; only an approved test release does"
	}
	release, err := expectation.Read(path)
	if err != nil {
		return nil, "the test's release cannot be read: " + err.Error()
	}
	pins := slices.Clone(release.Profiles)
	at := slices.IndexFunc(pins, func(pin profileversion.Version) bool { return pin.Profile.ID == seal.Profile.ID })
	switch {
	case at < 0:
		return nil, "this test pins no version of this profile"
	case pins[at] == seal:
		return nil, "this test already pins version " + seal.Profile.Version
	}
	pins[at] = seal
	spec, err := json.Marshal(release.Baseline.Spec)
	if err != nil {
		return nil, "the test's expectations cannot be read"
	}
	review, err := expectation.Review(release.ID, spec, pins, &release, false)
	if err != nil {
		return nil, err.Error()
	}
	next, err := expectation.Approve(release.ID, spec, pins, &release, review.Identity, a.reviewerName(), upgradeRationale)
	if err != nil {
		return nil, err.Error()
	}
	data, err := next.Encode()
	if err != nil {
		return nil, err.Error()
	}
	sum := sha256.Sum256(data)
	staged := []catalog.Staged{{Role: primaryRole(TestItem), File: "release.json", Data: data}}
	if links, held := paths["links"]; held {
		kept, err := boundedFile(links, maxLinksBytes)
		if err != nil {
			return nil, err.Error()
		}
		staged = append(staged, catalog.Staged{Role: "links", File: "links.json", Data: kept})
	}
	saved, err := loaded.store.Save(catalog.Draft{Kind: string(TestItem), ItemID: current.ID, Name: current.Name, Base: test.Revision,
		Intent: intent + "-" + current.ID, Digest: hex.EncodeToString(sum[:]), Author: a.reviewerName(), Members: staged},
		verifierFor(TestItem), catalog.Options{Now: a.now, Fault: a.saveFault})
	var conflict *catalog.Conflict
	switch {
	case errors.As(err, &conflict):
		return nil, "this test changed since it was reviewed; review its upgrade again"
	case err != nil:
		return nil, "the upgrade of this test did not complete; its reviewed revision is still current"
	}
	return &ItemRef{Kind: TestItem, ID: saved.Item.ID, Revision: revisionLabel(saved.Revision)}, ""
}
