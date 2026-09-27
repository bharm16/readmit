package desktop

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// A saved test is one object: its readmit-test/v1 spec and, when it has
// them, its links — the named environment it runs against, how its reset is
// decided, its tags and where it came from. The editor holds the whole test
// as one draft over its case, and one Save validates and publishes both
// members in one revision through the catalog, or neither.
//
// A test pins the target of the environment revision it was saved against:
// its spec names that revision's target file, as every readmit-test/v1 spec
// names a file, so a later edit of the environment reaches the test when the
// test is saved again.

// TestLinksSchema is the contract of a test's links member.
const TestLinksSchema = "readmit-test-links/v1"

// TestReset is how a test's reset instructions are decided: followed from
// its environment's named reset, or written by a person.
type TestReset string

const (
	ResetFromEnvironment TestReset = "environment"
	ResetManual          TestReset = "manual"
)

// TestSourceKind is the kind of source a test records it was created from.
type TestSourceKind string

const (
	SourceCase    TestSourceKind = "case"
	SourceFinding TestSourceKind = "finding"
	SourceVariant TestSourceKind = "variant"
)

// TestProposalSource is where one proposed check came from.
type TestProposalSource string

const (
	ProposalFromRun     TestProposalSource = "run"
	ProposalFromFinding TestProposalSource = "finding"
)

// TestChange names one part of a test a version changed from the version
// before, in the order History lists them.
type TestChange string

const (
	ChangeName        TestChange = "name"
	ChangeCase        TestChange = "case"
	ChangeMessages    TestChange = "messages"
	ChangeEnvironment TestChange = "environment"
	ChangeBoundary    TestChange = "boundary"
	ChangeObservation TestChange = "observation"
	ChangeReset       TestChange = "reset"
	ChangeChecks      TestChange = "checks"
	ChangeTags        TestChange = "tags"
)

// maxSourceBytes bounds a source's references.
const maxSourceBytes = 256

// TestLinks is what a test names beside its spec: the catalog identity of the
// environment it runs against, how its reset is decided, the tags a person
// gave it, sorted, and where it was created from.
type TestLinks struct {
	Schema      string      `json:"schema,omitzero"`
	Environment string      `json:"environment,omitzero"`
	Reset       TestReset   `json:"reset,omitzero"`
	Tags        []string    `json:"tags,omitzero"`
	Source      *TestSource `json:"source,omitzero"`
}

// TestSource is where a test was created from: a case, a confirmed finding of
// a review, or a variant.
type TestSource struct {
	Kind         TestSourceKind `json:"kind"`
	Finding      string         `json:"finding,omitzero"`
	Review       string         `json:"review,omitzero"`
	ReportSHA256 string         `json:"report_sha256,omitzero"`
	Variant      *ItemRef       `json:"variant,omitzero"`
}

// TestOrigin is where a new test starts: the case, the occurrences selected
// in it (none selects every one a test can send), the title proposed for
// the test, the source it records and checks proposed for it. Proposals reach the editor undecided and never the draft.
type TestOrigin struct {
	Case      ItemRef        `json:"case"`
	Messages  []string       `json:"messages"`
	Title     string         `json:"title,omitzero"`
	Source    *TestSource    `json:"source,omitzero"`
	Proposals []TestProposal `json:"proposals,omitzero"`
}

// TestProposal is one check proposed from a run or a confirmed finding. It is
// not part of any draft until a person accepts it. Reason says why a
// proposal nothing justifies carries no expected value.
type TestProposal struct {
	ID     string                 `json:"id"`
	Source TestProposalSource     `json:"source"`
	Check  testauthor.Expectation `json:"check"`
	Reason string                 `json:"reason,omitzero"`
}

// TestContext is what a test editor shows beside the draft: the case it
// sends, every occurrence of that case in source order with whether a test
// can send it, the clauses of the saved spec the editor cannot represent,
// the checks proposed and undecided, the exact bytes of the revision opened,
// and whether it opens read-only: a historical revision or a test release.
type TestContext struct {
	Case        *ItemRef       `json:"case"`
	CaseName    string         `json:"case_name"`
	Messages    []TestMessage  `json:"messages"`
	Unsupported []TestClause   `json:"unsupported"`
	Proposals   []TestProposal `json:"proposals"`
	Document    string         `json:"document,omitzero"`
	ReadOnly    bool           `json:"read_only"`
}

// TestMessage is one occurrence of the case a test sends: its identity, its
// kind, its parsed MSH-9 code and trigger, and whether a test can send it.
type TestMessage struct {
	ID           string           `json:"id"`
	Kind         bundle.EventKind `json:"kind"`
	MessageCode  string           `json:"message_code"`
	TriggerEvent string           `json:"trigger_event"`
	Sendable     bool             `json:"sendable"`
}

// TestClause is one clause of a saved spec the editor cannot represent, at
// the field it would be, with the reason. It stays in the spec as written.
type TestClause struct {
	Clause string `json:"clause"`
	Reason string `json:"reason"`
}

// testField is the draft field one stage of a test is reported at.
func testField(stage string, index int) string {
	if stage == testauthor.StageTarget {
		stage = "environment"
	}
	if index >= 0 {
		return "test." + stage + "." + strconv.Itoa(index)
	}
	return "test." + stage
}

// validateTestDraft validates a whole test draft: its links against the
// project, the environment it names resolved to that environment's target,
// and the draft through the same rules Generate holds it to, or its exact
// document through the reader and preparation a run makes of it.
func validateTestDraft(scope draftScope, draft ItemDraft) ([]catalog.Staged, ItemDraft, []FieldProblem) {
	problems := []FieldProblem{}
	normalized := ItemDraft{Name: draft.Name}
	switch {
	case draft.Test == nil && draft.TestDocument == "":
		return nil, normalized, append(problems, FieldProblem{Field: "test", Problem: "a test is answered from its source case"})
	case draft.Test != nil && draft.TestDocument != "":
		return nil, normalized, append(problems, FieldProblem{Field: "test", Problem: "a test is saved from its draft or from its document, not both"})
	}
	links := TestLinks{}
	if draft.TestLinks != nil {
		links = *draft.TestLinks
		links.Tags = slices.Sorted(slices.Values(links.Tags))
	}
	links.Schema = TestLinksSchema
	problems = append(problems, checkTestLinks(links)...)

	var environment *environmentMembers
	target := ""
	if links.Environment != "" {
		members, entry, problem := scope.environmentOfTest(links.Environment)
		if problem != "" {
			problems = append(problems, FieldProblem{Field: "test.environment", Problem: problem})
		}
		environment, target = members, entry
	}

	var data []byte
	if draft.Test != nil {
		test := *draft.Test
		if target != "" {
			test.Target = target
		}
		if links.Reset == ResetFromEnvironment && environment != nil {
			reset, err := environmentReset(environment)
			if err != nil {
				problems = append(problems, FieldProblem{Field: "test.reset", Problem: err.Error()})
			} else {
				test.Reset = reset
			}
		}
		_, source, declined := openedCase(scope.root, test.Case.Entry, test.Case.Identity)
		if source == nil {
			problems = append(problems, FieldProblem{Field: "test.case", Problem: declined.reason})
		}
		named := testNameProblem(test.Name)
		for _, problem := range testauthor.Problems(scope.root, source, test) {
			if source == nil && problem.Stage == testauthor.StageCase || named != nil && problem.Stage == testauthor.StageName {
				continue
			}
			if named != nil && problem.Stage != testauthor.StageCase {
				problems, named = append(problems, *named), nil
			}
			problems = append(problems, FieldProblem{Field: testField(problem.Stage, problem.Index), Problem: problem.Message})
		}
		if named != nil {
			problems = append(problems, *named)
		}
		if scope.lossyEdit() {
			problems = append(problems, FieldProblem{Field: "test", Problem: "this test holds clauses the editor cannot represent; save it through Edit JSON so none is lost"})
		}
		if len(problems) == 0 {
			generated, err := testauthor.Generate(test)
			if err != nil {
				problems = append(problems, FieldProblem{Field: "test", Problem: err.Error()})
			}
			data = generated
		}
		normalized.Test = &test
		if normalized.Name == "" && catalog.ValidName(test.Name) {
			normalized.Name = test.Name
		}
	} else {
		data = []byte(draft.TestDocument)
		spec, err := testrunner.DecodeSpec(data)
		switch {
		case err != nil:
			problems = append(problems, FieldProblem{Field: "test_document", Problem: err.Error()})
		case target != "" && spec.Target != target:
			problems = append(problems, FieldProblem{Field: "test.environment", Problem: "the document names another target than the environment this test runs against"})
		default:
			if named := testNameProblem(spec.Name); named != nil {
				problems = append(problems, *named)
			}
			if _, err := testrunner.PrepareSpec(data, scope.root); err != nil {
				problems = append(problems, FieldProblem{Field: "test_document", Problem: err.Error()})
			}
		}
		normalized.TestDocument = draft.TestDocument
		if normalized.Name == "" && catalog.ValidName(spec.Name) {
			normalized.Name = spec.Name
		}
	}
	if len(problems) > 0 {
		return nil, normalized, problems
	}
	staged := []catalog.Staged{{Role: "test", File: "test.json", Data: data}}
	if links.Environment != "" || links.Reset != "" || len(links.Tags) > 0 || links.Source != nil {
		encoded, err := encodeMember(links)
		if err != nil {
			return nil, normalized, append(problems, FieldProblem{Field: "test_links", Problem: err.Error()})
		}
		normalized.TestLinks = &links
		staged = append(staged, catalog.Staged{Role: "links", File: "links.json", Data: encoded})
	}
	return staged, normalized, problems
}

// maxTestNameRunes bounds a test's name in characters, as the window counts
// them; the spec's own bound in bytes still holds beside it.
const maxTestNameRunes = 200

// testNameProblem is the problem of a test name that is not 1 to 200
// printable characters, or nil. An empty name is the draft's unanswered name
// stage, which the draft's own rules report.
func testNameProblem(name string) *FieldProblem {
	if name == "" || utf8.ValidString(name) && utf8.RuneCountInString(name) <= maxTestNameRunes &&
		strings.TrimSpace(name) != "" && !strings.ContainsFunc(name, unicode.IsControl) {
		return nil
	}
	return &FieldProblem{Field: "test.name", Problem: "a test name is 1 to 200 printable characters"}
}

// checkTestLinks reports every problem of a test's links as the reader of
// the saved member would.
func checkTestLinks(links TestLinks) []FieldProblem {
	problems := []FieldProblem{}
	if links.Environment != "" && !catalog.ValidID(links.Environment) {
		problems = append(problems, FieldProblem{Field: "test.environment", Problem: "an environment is named by its identity in the project"})
	}
	switch links.Reset {
	case "", ResetFromEnvironment, ResetManual:
	default:
		problems = append(problems, FieldProblem{Field: "test.reset", Problem: "a reset follows the environment's reset or is written as instructions"})
	}
	if links.Reset == ResetFromEnvironment && links.Environment == "" {
		problems = append(problems, FieldProblem{Field: "test.reset", Problem: "a test follows the reset of the environment it runs against; choose the environment first"})
	}
	if err := project.CheckTags(project.SchemaV2, links.Tags); err != nil {
		problems = append(problems, FieldProblem{Field: "test_links.tags", Problem: err.Error()})
	}
	if source := links.Source; source != nil {
		valid := slices.Contains([]TestSourceKind{SourceCase, SourceFinding, SourceVariant}, source.Kind)
		for _, reference := range []string{source.Finding, source.Review, source.ReportSHA256} {
			valid = valid && len(reference) <= maxSourceBytes && catalog.ValidToken(reference) == (reference != "")
		}
		if source.Variant != nil {
			valid = valid && source.Variant.Kind == VariantItem && catalog.ValidID(source.Variant.ID)
		}
		if !valid {
			problems = append(problems, FieldProblem{Field: "test_links.source", Problem: "a test's source is a case, a confirmed finding or a variant, named by its references"})
		}
	}
	return problems
}

// decodeTestLinks reads one test links member exactly as written.
func decodeTestLinks(data []byte) (TestLinks, error) {
	var links TestLinks
	if len(data) > maxLinksBytes || json.Unmarshal(data, &links, json.RejectUnknownMembers(true)) != nil || links.Schema != TestLinksSchema {
		return TestLinks{}, errors.New("the test's links cannot be read")
	}
	if len(checkTestLinks(links)) > 0 || !slices.IsSorted(links.Tags) {
		return TestLinks{}, errors.New("the test's links name something this release does not read")
	}
	return links, nil
}

func readTestLinks(path string) (TestLinks, error) {
	data, err := boundedFile(path, maxLinksBytes)
	if err != nil {
		return TestLinks{}, err
	}
	return decodeTestLinks(data)
}

// verifyTest reads a staged test revision through the readers the command
// line reads its spec with, and its links.
func verifyTest(files map[string]string) error {
	if _, err := testrunner.ReadSpec(files["test"]); err != nil {
		return err
	}
	if path, held := files["links"]; held {
		if _, err := readTestLinks(path); err != nil {
			return err
		}
	}
	return nil
}

// environmentOfTest resolves an environment a test names to its members and
// the project entry its target is, which the test's spec names.
func (s draftScope) environmentOfTest(id string) (*environmentMembers, string, string) {
	if s.loaded == nil {
		return nil, "", "the project holds no such environment"
	}
	index := s.loaded.document.Find(id)
	if index < 0 || s.loaded.document.Items[index].Kind != string(EnvironmentItem) || s.loaded.removed(s.loaded.document.Items[index]) {
		return nil, "", "the project holds no such environment"
	}
	members, err := s.loaded.environmentOf(s.loaded.document.Items[index])
	if err != nil {
		return nil, "", "that environment cannot be read: " + err.Error()
	}
	entry := s.loaded.entryOf(members.paths["target"])
	if entry == "" {
		return nil, "", "that environment's target is not one entry of the project"
	}
	return members, entry, ""
}

// environmentReset is the reset instructions an environment's named reset
// gives a test: its name and each action's instructions, in order, as one
// field of prose. They are read by an operator and never executed.
func environmentReset(members *environmentMembers) (string, error) {
	if members.reset == nil || len(members.reset.Actions) == 0 {
		return "", errors.New("that environment has no reset to follow; write the reset instructions instead")
	}
	parts := []string{}
	if members.links.ResetName != "" {
		parts = append(parts, members.links.ResetName+":")
	}
	for _, action := range members.reset.Actions {
		parts = append(parts, action.Instructions)
	}
	reset := strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
	if len(reset) > testauthor.MaxResetBytes {
		return "", errors.New("that environment's reset is longer than a test's reset instructions hold; write them instead")
	}
	return reset, nil
}

// lossyEdit reports whether the test an edit began from holds what its
// editor cannot represent — a clause a draft cannot hold, or a test release —
// which a save from the draft would lose.
func (s draftScope) lossyEdit() bool {
	if s.item == "" || s.loaded == nil {
		return false
	}
	index := s.loaded.document.Find(s.item)
	if index < 0 || s.loaded.document.Items[index].Kind != string(TestItem) {
		return false
	}
	saved, err := s.loaded.testOf(s.loaded.document.Items[index], "")
	if err != nil {
		return false
	}
	_, clauses, err := testauthor.FromSpec(saved.spec, "")
	return saved.release || err != nil || len(clauses) > 0
}

// sampleSave admits the guided sample's own test without a license: the
// frozen sample case at the exact bundled practice target, as authoring the
// sample always was. Its case is verified by that identity before anything
// is written.
func sampleSave(request SaveItemRequest) bool {
	draft := request.Draft
	if request.Kind != TestItem || draft.Test == nil || draft.TestDocument != "" || draft.TestLinks != nil && draft.TestLinks.Environment != "" {
		return false
	}
	if draft.Test.Target != guide.TargetName {
		return false
	}
	root, _ := resolveFolder(request.Context.Project)
	if root == "" {
		return false
	}
	return sampleAuthoring(TestRequest{Case: draft.Test.Case.Entry, Draft: *draft.Test}, root, &bundle.Bundle{Identity: draft.Test.Case.Identity})
}

// savedTest is one revision of a test as saved: the exact bytes of its
// document, the spec it holds, whether it is a test release, and its links.
type savedTest struct {
	data    []byte
	spec    testrunner.Spec
	release bool
	links   *TestLinks
}

// revisionBacking resolves the files of one revision of an object, named by
// its number, or of its current revision when revision is empty.
func (c *loadedCatalog) revisionBacking(item catalog.Item, revision string) (map[string]string, Availability, string) {
	if revision == "" || revision == item.RevisionLabel() {
		return c.backing(item)
	}
	at := slices.IndexFunc(item.Revisions, func(saved catalog.Revision) bool { return strconv.Itoa(saved.Number) == revision })
	if at < 0 {
		return nil, ItemMissing, "the project holds no such version of this object"
	}
	historical := item
	historical.Revisions = item.Revisions[at : at+1]
	return c.backing(historical)
}

// testOf reads one revision of a test.
func (c *loadedCatalog) testOf(item catalog.Item, revision string) (*savedTest, error) {
	paths, availability, reason := c.revisionBacking(item, revision)
	if availability != ItemAvailable {
		return nil, errors.New(reason)
	}
	path := paths[primaryRole(TestItem)]
	data, err := boundedFile(path, testrunner.MaxSpecBytes)
	if err != nil {
		return nil, err
	}
	saved := &savedTest{data: data}
	if declares(path, "readmit-test-release/v1") {
		release, err := expectation.Read(path)
		if err != nil {
			return nil, err
		}
		saved.spec, saved.release = release.Baseline.Spec, true
	} else if saved.spec, err = testrunner.DecodeSpec(data); err != nil {
		return nil, err
	}
	if path, held := paths["links"]; held {
		links, err := readTestLinks(path)
		if err != nil {
			return nil, err
		}
		saved.links = &links
	}
	return saved, nil
}

// entryOf is the project entry a file of the project is, as a spec names
// it, or empty for a file that is not one entry of the project.
func (c *loadedCatalog) entryOf(path string) string {
	entry, err := filepath.Rel(c.root, path)
	if err != nil || artifactpath.EntryName(entry) != nil {
		return ""
	}
	return entry
}

// caseMessages reads every occurrence of a verified case in source order.
func caseMessages(source *bundle.Bundle) []TestMessage {
	facts := readFacts(source)
	messages := make([]TestMessage, 0, len(source.Events))
	for _, event := range source.Events {
		declared := facts.types[event.ID]
		messages = append(messages, TestMessage{ID: event.ID, Kind: event.Kind, MessageCode: declared.Code,
			TriggerEvent: declared.Trigger, Sendable: event.Kind == bundle.Message})
	}
	return messages
}

// caseOf is the project's case or variant at one entry, with its name, the
// identity its evidence verifies as and the verified evidence.
func (c *loadedCatalog) caseOf(entry string) (*ItemRef, string, *bundle.Bundle) {
	ref := c.entryRef(CaseItem, entry)
	if ref == nil {
		ref = c.entryRef(VariantItem, entry)
	}
	name := ""
	if ref != nil {
		name = c.read(c.document.Items[c.document.Find(ref.ID)]).Name
	}
	facts, _, err := operation.VerifiedCase(c.root, entry)
	if err != nil {
		return ref, name, nil
	}
	_, source, _ := openedCase(c.root, entry, facts.Identity)
	return ref, name, source
}

// environmentByTarget is the environment whose current target is the entry
// a spec names, for a test saved before it recorded its environment.
func (c *loadedCatalog) environmentByTarget(entry string) string {
	for _, item := range c.document.Items {
		if item.Kind != string(EnvironmentItem) || c.removed(item) {
			continue
		}
		if paths, availability, _ := c.backing(item); availability == ItemAvailable && c.entryOf(paths["target"]) == entry {
			return item.ID
		}
	}
	return ""
}

// clausesOf are the clauses of a spec its editor cannot represent, at their
// fields.
func clausesOf(clauses []testauthor.Clause) []TestClause {
	out := make([]TestClause, 0, len(clauses))
	for _, clause := range clauses {
		out = append(out, TestClause{Clause: testField(clause.Stage, -1), Reason: clause.Reason})
	}
	return out
}

// openTestDraft answers the draft a test editor starts from: a new test over
// the case its origin names, or a revision of a saved one exactly as saved.
func (a *App) openTestDraft(ctx context.Context, request ItemRequest) ItemDraftResult {
	result := ItemDraftResult{Context: request.Context}
	loaded, declined := a.loadCatalog(ctx, request.Context, false)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	if request.Ref.ID == "" {
		return loaded.newTestDraft(result, request.From)
	}
	index := loaded.document.Find(request.Ref.ID)
	if index < 0 || loaded.document.Items[index].Kind != string(TestItem) || loaded.removed(loaded.document.Items[index]) {
		result.refuse(Failed, "the project holds no such object")
		return result
	}
	record := loaded.document.Items[index]
	saved, err := loaded.testOf(record, request.Ref.Revision)
	if err != nil {
		result.refuse(Failed, "this test cannot be read: "+err.Error())
		return result
	}
	test, shown := loaded.testDraftOf(saved)
	shown.ReadOnly = saved.release || request.Ref.Revision != "" && request.Ref.Revision != record.RevisionLabel() || len(shown.Unsupported) > 0
	draft := ItemDraft{Name: cmp.Or(record.Name, saved.spec.Name), Test: test, TestLinks: saved.links}
	if saved.links == nil {
		if environment := loaded.environmentByTarget(saved.spec.Target); environment != "" {
			draft.TestLinks = &TestLinks{Schema: TestLinksSchema, Environment: environment}
		}
	}
	ref := ItemRef{Kind: TestItem, ID: record.ID, Revision: cmp.Or(request.Ref.Revision, record.RevisionLabel())}
	result.State, result.Ref, result.Draft, result.Test = Completed, &ref, &draft, shown
	return result
}

// testDraftOf is the draft a saved spec reopens as, over the project's case
// it names, and what the editor shows beside it.
func (c *loadedCatalog) testDraftOf(saved *savedTest) (*testauthor.Draft, *TestContext) {
	context := &TestContext{Messages: []TestMessage{}, Unsupported: []TestClause{}, Proposals: []TestProposal{}, Document: string(saved.data)}
	identity := ""
	if artifactpath.EntryName(saved.spec.Input.Case) == nil {
		ref, name, source := c.caseOf(saved.spec.Input.Case)
		context.Case, context.CaseName = ref, name
		if source != nil {
			identity, context.Messages = source.Identity, caseMessages(source)
		}
	}
	draft, clauses, err := testauthor.FromSpec(saved.spec, identity)
	if err != nil {
		context.Unsupported = append(context.Unsupported, TestClause{Clause: "test", Reason: err.Error()})
		return nil, context
	}
	if identity == "" && draft.Case.Entry != "" {
		context.Unsupported = append(context.Unsupported, TestClause{Clause: "test.case", Reason: "the case this test sends cannot be verified"})
	}
	context.Unsupported = append(context.Unsupported, clausesOf(clauses)...)
	return &draft, context
}

// newTestDraft starts a test over the case its origin names, sending the
// selected occurrences that a test can send, in the order the case records
// them; with none selected, it sends every one a test can send. Proposals are handed to the editor undecided; none is in the draft.
func (c *loadedCatalog) newTestDraft(result ItemDraftResult, origin *TestOrigin) ItemDraftResult {
	result.New, result.Ref = true, &ItemRef{Kind: TestItem}
	if origin == nil {
		result.State, result.Draft = Completed, &ItemDraft{}
		return result
	}
	index := c.document.Find(origin.Case.ID)
	if index < 0 || origin.Case.Kind != CaseItem && origin.Case.Kind != VariantItem ||
		c.document.Items[index].Kind != string(origin.Case.Kind) || c.removed(c.document.Items[index]) || c.document.Items[index].Entry == "" {
		result.refuse(Failed, "the project holds no such case")
		return result
	}
	entry := c.document.Items[index].Entry
	ref, name, source := c.caseOf(entry)
	if source == nil {
		result.refuse(Failed, "the case this test sends cannot be verified")
		return result
	}
	draft, err := testauthor.NewDraft(entry, source.Identity)
	if err != nil {
		result.refuse(Failed, err.Error())
		return result
	}
	selected := map[string]bool{}
	for _, id := range origin.Messages {
		selected[id] = true
	}
	context := &TestContext{Case: ref, CaseName: name, Messages: caseMessages(source), Unsupported: []TestClause{}, Proposals: []TestProposal{}}
	all := len(origin.Messages) == 0
	for _, message := range context.Messages {
		if (all || selected[message.ID]) && message.Sendable {
			draft.Messages = append(draft.Messages, message.ID)
		}
		delete(selected, message.ID)
	}
	if left := len(origin.Messages) - len(draft.Messages); left > 0 {
		result.Problems = append(result.Problems, FieldProblem{Field: "test.messages",
			Problem: strconv.Itoa(left) + " selected occurrences are not messages of this case a test can send, and are not selected"})
	}
	if title := cmp.Or(origin.Title, name); catalog.ValidName(title) && len(title) <= testauthor.MaxNameBytes {
		draft.Name = title
	}
	if origin.Proposals != nil {
		context.Proposals = slices.Clone(origin.Proposals)
	}
	item := ItemDraft{Name: draft.Name, Test: &draft}
	if origin.Source != nil {
		item.TestLinks = &TestLinks{Schema: TestLinksSchema, Source: origin.Source}
	}
	result.State, result.Draft, result.Test = Completed, &item, context
	return result
}

// TestVersion is one saved version of a test: its revision, when and by
// whom it was saved, what changed from the version before — name, case,
// messages, environment, boundary, observation, reset, checks, tags — and
// whether it is current. The first version has no changes.
type TestVersion struct {
	Revision    string       `json:"revision"`
	PublishedAt *string      `json:"published_at"`
	Author      string       `json:"author,omitzero"`
	Changes     []TestChange `json:"changes"`
	Current     bool         `json:"current"`
}

// TestRunRow is one retained run of a test: the run, the version of the test
// it executed, when it started and the outcome it records.
type TestRunRow struct {
	Run       ItemRef           `json:"run"`
	Revision  string            `json:"revision,omitzero"`
	StartedAt *string           `json:"started_at"`
	Outcome   testrunner.Status `json:"outcome,omitzero"`
}

// TestHistoryResult is a test's versions, newest first, and the runs of each,
// most recently started first.
type TestHistoryResult struct {
	State    State          `json:"state"`
	Reason   string         `json:"reason,omitzero"`
	Context  RequestContext `json:"context"`
	Versions []TestVersion  `json:"versions"`
	Runs     []TestRunRow   `json:"runs"`
}

func (r *TestHistoryResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// TestHistory lists a test's saved versions with what changed in each, and
// the retained runs that executed each version exactly. It is a read: nothing
// runs.
func (a *App) TestHistory(request ItemRequest) TestHistoryResult {
	return run(a, false, false, func(ctx context.Context) TestHistoryResult {
		result := TestHistoryResult{Context: request.Context, Versions: []TestVersion{}, Runs: []TestRunRow{}}
		if request.Ref.Kind != TestItem {
			result.refuse(Failed, "only a test has versions and runs")
			return result
		}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Ref, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		record := loaded.document.Items[loaded.document.Find(item.Ref.ID)]
		labels := []string{""}
		if len(record.Revisions) > 0 {
			labels = labels[:0]
			for _, revision := range record.Revisions {
				labels = append(labels, strconv.Itoa(revision.Number))
			}
		}
		runs := map[string]TestRunRow{}
		var previous *savedTest
		for i, label := range labels {
			saved, err := loaded.testOf(record, label)
			if err != nil && label == "" {
				result.refuse(Failed, "this test cannot be read: "+err.Error())
				return result
			}
			if label != "" {
				revision := record.Revisions[i]
				version := TestVersion{Revision: label, PublishedAt: stamped(revision.PublishedAt), Author: revision.Author,
					Changes: []TestChange{}, Current: i == len(labels)-1}
				if saved != nil && previous != nil {
					version.Changes = testChanges(previous, saved)
				}
				result.Versions = append(result.Versions, version)
			}
			if saved == nil {
				continue
			}
			previous = saved
			for _, matched := range loaded.runsOf(saved.spec) {
				row := TestRunRow{Run: ItemRef{Kind: RunItem, ID: matched.id}, Revision: label, Outcome: matched.outcome}
				if !matched.started.IsZero() {
					row.StartedAt = stampedTime(matched.started)
				}
				runs[matched.id] = row
			}
		}
		slices.Reverse(result.Versions)
		for _, row := range runs {
			result.Runs = append(result.Runs, row)
		}
		slices.SortFunc(result.Runs, func(x, y TestRunRow) int {
			return cmp.Or(cmp.Compare(stampOf(y.StartedAt), stampOf(x.StartedAt)), cmp.Compare(x.Run.ID, y.Run.ID))
		})
		result.State = Completed
		return result
	})
}

// testChanges names what one version of a test changed from the one before.
func testChanges(before, after *savedTest) []TestChange {
	links := func(saved *savedTest) TestLinks {
		if saved.links == nil {
			return TestLinks{}
		}
		return *saved.links
	}
	x, y := before.spec, after.spec
	lx, ly := links(before), links(after)
	changes := []TestChange{}
	for _, change := range []struct {
		name    TestChange
		changed bool
	}{
		{ChangeName, x.Name != y.Name},
		{ChangeCase, x.Input.Case != y.Input.Case},
		{ChangeMessages, !slices.Equal(x.Input.Messages, y.Input.Messages)},
		{ChangeEnvironment, x.Target != y.Target || lx.Environment != ly.Environment},
		{ChangeBoundary, x.Observation.Boundary != y.Observation.Boundary},
		{ChangeObservation, x.Observation.Path != y.Observation.Path},
		{ChangeReset, x.Setup.ResetInstructions != y.Setup.ResetInstructions || lx.Reset != ly.Reset},
		{ChangeChecks, !reflect.DeepEqual(x.Assertions, y.Assertions)},
		{ChangeTags, !slices.Equal(lx.Tags, ly.Tags)},
	} {
		if change.changed {
			changes = append(changes, change.name)
		}
	}
	return changes
}

// ItemRevision is one published revision of a saved object: the reference
// that names it, its number, when and by whom it was published, and whether
// it is current.
type ItemRevision struct {
	Ref         ItemRef `json:"ref"`
	Number      int     `json:"number"`
	PublishedAt *string `json:"published_at"`
	Author      string  `json:"author,omitzero"`
	Current     bool    `json:"current"`
}

// ItemHistoryResult is an object's published revisions, newest first.
type ItemHistoryResult struct {
	State     State          `json:"state"`
	Reason    string         `json:"reason,omitzero"`
	Context   RequestContext `json:"context"`
	Revisions []ItemRevision `json:"revisions"`
}

func (r *ItemHistoryResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ItemHistory lists every revision the application published of one saved
// object, whatever its kind, with who saved each. An object the project only
// holds as a discovered file, or whose state a project document holds, has
// no revisions. It is a read.
func (a *App) ItemHistory(request ItemRequest) ItemHistoryResult {
	return run(a, false, false, func(ctx context.Context) ItemHistoryResult {
		result := ItemHistoryResult{Context: request.Context, Revisions: []ItemRevision{}}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Ref, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		result.State = Empty
		index := loaded.document.Find(item.Ref.ID)
		if index < 0 {
			return result
		}
		record := loaded.document.Items[index]
		for i, revision := range slices.Backward(record.Revisions) {
			result.Revisions = append(result.Revisions, ItemRevision{
				Ref:    ItemRef{Kind: request.Ref.Kind, ID: record.ID, Revision: strconv.Itoa(revision.Number)},
				Number: revision.Number, PublishedAt: stamped(revision.PublishedAt), Author: revision.Author,
				Current: i == len(record.Revisions)-1,
			})
		}
		if len(result.Revisions) > 0 {
			result.State = Completed
		}
		return result
	})
}

// ImportTestDraft opens a readmit-test/v1 file a person chose as a new test
// draft of the open project. The file is read strictly; its case is bound to
// the project's case with the same verified evidence, and a reference the
// project cannot resolve is a problem at its field, never dropped. Every
// check is kept, and nothing is saved.
func (a *App) ImportTestDraft(request RequestContext) ItemDraftResult {
	return run(a, true, false, func(ctx context.Context) ItemDraftResult {
		result := ItemDraftResult{Context: request}
		loaded, declined := a.loadCatalog(ctx, request, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		files, declined := a.chooseFiles(ctx, "Import test", "Test", "*.json")
		if len(files) == 0 {
			result.refuse(declined.state, declined.reason)
			return result
		}
		data, err := boundedFile(files[0], testrunner.MaxSpecBytes)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		spec, err := testrunner.DecodeSpec(data)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		return loaded.importedTest(result, spec, data, filepath.Dir(files[0]))
	})
}

// importedTest binds an imported spec's references to the project: its case
// by verified evidence, its target and observation only when they are
// entries of this project.
func (c *loadedCatalog) importedTest(result ItemDraftResult, spec testrunner.Spec, data []byte, folder string) ItemDraftResult {
	result.New, result.Ref, result.Problems = true, &ItemRef{Kind: TestItem}, []FieldProblem{}
	context := &TestContext{Messages: []TestMessage{}, Unsupported: []TestClause{}, Proposals: []TestProposal{}, Document: string(data)}
	identity, entry := "", ""
	if source, err := bundle.Open(artifactpath.JoinReference(folder, spec.Input.Case)); err == nil {
		identity = source.Identity
		if ref := c.caseByIdentity(identity); ref != nil {
			entry = c.document.Items[c.document.Find(ref.ID)].Entry
		}
	}
	draft, clauses, err := testauthor.FromSpec(spec, identity)
	if err != nil {
		result.refuse(Failed, err.Error())
		return result
	}
	local, _ := filepath.EvalSymlinks(folder)
	here := local == c.root
	draft.Case.Entry = entry
	if entry == "" {
		result.Problems = append(result.Problems, FieldProblem{Field: "test.case", Problem: "the project holds no case with the evidence this test sends; add the case to the project first"})
	} else {
		ref, name, source := c.caseOf(entry)
		context.Case, context.CaseName = ref, name
		if source != nil {
			context.Messages = caseMessages(source)
		}
	}
	if named := testNameProblem(draft.Name); named != nil {
		result.Problems = append(result.Problems, *named)
	}
	links := &TestLinks{Schema: TestLinksSchema}
	if draft.Target != "" && here {
		links.Environment = c.environmentByTarget(draft.Target)
	}
	if links.Environment == "" {
		draft.Target = ""
		result.Problems = append(result.Problems, FieldProblem{Field: "test.environment", Problem: "choose the environment this test runs against in this project"})
	}
	if spec.Observation.Boundary == testrunner.LedgerBoundary && (draft.Observation == "" || !here) {
		draft.Observation = ""
		result.Problems = append(result.Problems, FieldProblem{Field: "test.observation", Problem: "choose the observation this test reads in this project"})
	}
	for _, clause := range clauses {
		if clause.Stage != testauthor.StageCase && clause.Stage != testauthor.StageTarget && clause.Stage != testauthor.StageObservation {
			context.Unsupported = append(context.Unsupported, clausesOf([]testauthor.Clause{clause})...)
		}
	}
	item := ItemDraft{Test: &draft}
	if catalog.ValidName(spec.Name) {
		item.Name = spec.Name
	}
	if links.Environment != "" {
		item.TestLinks = links
	}
	result.State, result.Draft, result.Test = Completed, &item, context
	return result
}

// ExportTestResult is where an exported test was written.
type ExportTestResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Path    string         `json:"path,omitzero"`
}

func (r *ExportTestResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ExportTestItem writes the exact bytes of a saved test's revision — its
// current one unless the reference names another — to a new file the person
// names in the save dialog. The saved contract is written as it is, never
// regenerated, so every reader of it reads what the project holds.
func (a *App) ExportTestItem(request ItemRequest) ExportTestResult {
	return run(a, true, true, func(ctx context.Context) ExportTestResult {
		result := ExportTestResult{Context: request.Context}
		if request.Ref.Kind != TestItem {
			result.refuse(Failed, "only a test is exported as a test")
			return result
		}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Ref, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		saved, err := loaded.testOf(loaded.document.Items[loaded.document.Find(item.Ref.ID)], request.Ref.Revision)
		if err != nil {
			result.refuse(Failed, "this test cannot be read: "+err.Error())
			return result
		}
		if saved.release {
			result.refuse(Failed, "a test release is shared as its release, not exported as a test")
			return result
		}
		name := strings.Map(func(r rune) rune {
			if r == '/' || r == '\\' || r == ':' {
				return '-'
			}
			return r
		}, cmp.Or(item.Name, "test"))
		destination, declined := a.chooseNamedDestination(ctx, "Export test", name+".json")
		if destination == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		written, err := testauthor.ExportTo(saved.data, destination)
		if errors.Is(err, testauthor.ErrCannotWrite) {
			result.refuse(Failed, "the test must be exported to a new file in a folder this account can write")
			return result
		}
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		result.State, result.Path = Completed, written.Output
		return result
	})
}
