package desktop

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// A suite version is read from its definition, the SuiteDraft it was saved
// from, and compiled against the project: each test at the exact version it
// pins, each row's case, and each binding's named environment and
// observation at their current revisions. The compiled readmit-suite/v1
// document names project entries relative to the project root, so the one
// suite reader, preview and preparation `readmit suite` uses read it exactly
// as they read a file a person placed there. A version whose draft has no
// test or no environment is saved as not runnable: its definition alone.

// originalVersion names the version an original readmit-suite/v1 file of
// the project is: the file as it is, with no number.
const originalVersion = "original"

// unassignedOwner is the owner a compiled suite declares when its draft names
// none: the suite contract requires one, and a suite's owner is optional.
const unassignedOwner = "unassigned"

// notRunnable is why a version with no test or no environment cannot run,
// be approved, exported or assessed.
const notRunnable = "Add tests and an environment to run this suite"

// maxConcurrency bounds a suite's concurrent jobs, as the run queue does.
const maxConcurrency = 16

// suiteIdentifier is the rule every identifier a suite declares follows.
var suiteIdentifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// exclusionStates are the declarations an exclusion can make.
var exclusionStates = []string{"skipped", "unsupported", "quarantined", "disabled"}

// suiteDefinition is the definition member of a suite version.
type suiteDefinition struct {
	Schema string     `json:"schema"`
	Draft  SuiteDraft `json:"draft"`
}

// decodeSuiteDefinition reads one definition member exactly as written.
func decodeSuiteDefinition(data []byte) (SuiteDraft, error) {
	var definition suiteDefinition
	if len(data) > suite.MaxBytes || json.Unmarshal(data, &definition, json.RejectUnknownMembers(true)) != nil {
		return SuiteDraft{}, errors.New("the suite's definition cannot be read")
	}
	if definition.Schema == ConnectedSuiteDefinitionSchema {
		if err := verifyConnectedDefinition(definition.Draft); err != nil {
			return SuiteDraft{}, err
		}
	} else if definition.Schema != SuiteDefinitionSchema || definition.Draft.Connected != nil {
		return SuiteDraft{}, errors.New("the suite's definition cannot be read")
	}
	return normalizedSuite(definition.Draft), nil
}

// verifySuite reads a staged suite version through the readers its members
// are read with: the definition, and the compiled document through the suite
// reader `readmit suite` applies.
func verifySuite(files map[string]string) error {
	data, err := boundedFile(files["definition"], suite.MaxBytes)
	if err != nil {
		return err
	}
	if _, err := decodeSuiteDefinition(data); err != nil {
		return err
	}
	if path, held := files["suite"]; held {
		data, err := boundedFile(path, suite.MaxBytes)
		if err != nil {
			return err
		}
		if declaresConnectedSuite(data) {
			return verifyConnectedSuiteDocument(data)
		}
		if _, err := suite.Decode(data); err != nil {
			return err
		}
	}
	return nil
}

// normalizedSuite is a draft with every list declared, empty where it holds
// nothing, so a saved definition and its projection never read a list as
// absent.
func normalizedSuite(draft SuiteDraft) SuiteDraft {
	draft.Tags = orEmpty(slices.Clone(draft.Tags))
	draft.Tests = orEmpty(slices.Clone(draft.Tests))
	for i := range draft.Tests {
		test := &draft.Tests[i]
		test.After, test.Sequence, test.Tags = orEmpty(slices.Clone(test.After)), orEmpty(slices.Clone(test.Sequence)), orEmpty(slices.Clone(test.Tags))
	}
	draft.Datasets = orEmpty(slices.Clone(draft.Datasets))
	for i := range draft.Datasets {
		draft.Datasets[i].Rows = orEmpty(slices.Clone(draft.Datasets[i].Rows))
	}
	draft.Environments = orEmpty(slices.Clone(draft.Environments))
	for i := range draft.Environments {
		draft.Environments[i].Bindings = orEmpty(slices.Clone(draft.Environments[i].Bindings))
	}
	draft.Requirements = orEmpty(slices.Clone(draft.Requirements))
	for i := range draft.Requirements {
		draft.Requirements[i].Tests = orEmpty(slices.Clone(draft.Requirements[i].Tests))
	}
	draft.Exclusions = orEmpty(slices.Clone(draft.Exclusions))
	return draft
}

// suiteVersion is one version of a suite: a managed revision, or the
// original file the project held before any. data is the version's suite
// document exactly as saved — the compiled member, or the original file —
// and entry the project entry holding it; both are empty for a version that
// is not runnable.
type suiteVersion struct {
	item      catalog.Item
	label     string
	revision  *catalog.Revision
	draft     SuiteDraft
	data      []byte
	entry     string
	document  *suite.Document
	connected *suite.ConnectedDocument
}

func (v *suiteVersion) original() bool { return v.revision == nil }
func (v *suiteVersion) runnable() bool { return v.document != nil || v.connected != nil }

// suiteItem is the suite a reference names, as this load's catalog holds it.
func (c *loadedCatalog) suiteItem(ref ItemRef) (catalog.Item, error) {
	index := c.document.Find(ref.ID)
	if ref.Kind != SuiteItem || index < 0 || c.document.Items[index].Kind != string(SuiteItem) || c.removed(c.document.Items[index]) {
		return catalog.Item{}, errors.New("the project holds no such suite")
	}
	return c.document.Items[index], nil
}

// currentLabel is the version of a suite that is current: its latest
// revision, or its original file when it has none.
func currentLabel(item catalog.Item) string {
	return cmp.Or(item.RevisionLabel(), originalVersion)
}

// suiteLabels are every version of a suite, oldest first: the original file,
// when there is one, and then each managed revision.
func suiteLabels(item catalog.Item) []string {
	labels := []string{}
	if item.Entry != "" {
		labels = append(labels, originalVersion)
	}
	for _, revision := range item.Revisions {
		labels = append(labels, strconv.Itoa(revision.Number))
	}
	return labels
}

// suiteVersion reads one version of a suite: label names a revision, the
// original file, or, empty, the current version.
func (c *loadedCatalog) suiteVersion(item catalog.Item, label string) (*suiteVersion, error) {
	if label == "" {
		label = currentLabel(item)
	}
	if label == originalVersion {
		if item.Entry == "" {
			return nil, errors.New("this suite has no original file")
		}
		original := item
		original.Revisions = nil
		paths, availability, reason := c.backing(original)
		if availability != ItemAvailable {
			return nil, errors.New(reason)
		}
		data, err := boundedFile(paths[primaryRole(SuiteItem)], suite.MaxBytes)
		if err != nil {
			return nil, err
		}
		if declaresConnectedSuite(data) {
			document, err := suite.DecodeConnected(data)
			if err != nil {
				return nil, err
			}
			return &suiteVersion{item: item, label: label, draft: connectedDraft(document), data: data, entry: item.Entry, connected: &document}, nil
		}
		document, err := suite.Decode(data)
		if err != nil {
			return nil, err
		}
		draft := c.draftOfDocument(document, c.root)
		return &suiteVersion{item: item, label: label, draft: draft, data: data, entry: item.Entry, document: &document}, nil
	}
	at := slices.IndexFunc(item.Revisions, func(revision catalog.Revision) bool { return strconv.Itoa(revision.Number) == label })
	if at < 0 {
		return nil, errors.New("the project holds no such version of this suite")
	}
	historical := item
	historical.Revisions = item.Revisions[at : at+1]
	paths, availability, reason := c.backing(historical)
	if availability != ItemAvailable {
		return nil, errors.New(reason)
	}
	data, err := boundedFile(paths["definition"], suite.MaxBytes)
	if err != nil {
		return nil, err
	}
	draft, err := decodeSuiteDefinition(data)
	if err != nil {
		return nil, err
	}
	version := &suiteVersion{item: item, label: label, revision: &item.Revisions[at], draft: draft}
	if path, held := paths["suite"]; held {
		compiled, err := boundedFile(path, suite.MaxBytes)
		if err != nil {
			return nil, err
		}
		if draft.Connected != nil {
			document, err := suite.DecodeConnected(compiled)
			if err != nil {
				return nil, err
			}
			a, _ := json.Marshal(document, json.Deterministic(true))
			b, _ := json.Marshal(draft.Connected.Document, json.Deterministic(true))
			if string(a) != string(b) {
				return nil, errors.New("the connected suite differs from its saved definition")
			}
			version.data, version.entry, version.connected = compiled, c.entryOf(path), &document
			return version, nil
		}
		document, err := suite.Decode(compiled)
		if err != nil {
			return nil, err
		}
		version.data, version.entry, version.document = compiled, c.entryOf(path), &document
	}
	return version, nil
}

// suiteName is the name a suite is shown by: the name a person gave it, or
// the identifier its original file declares.
func (c *loadedCatalog) suiteName(item catalog.Item) string {
	if item.Name != "" {
		return item.Name
	}
	if version, err := c.suiteVersion(item, originalVersion); err == nil {
		if version.connected != nil {
			return version.connected.ID
		}
		return version.document.ID
	}
	return "Suite"
}

// readableName is an identifier made readable, as an imported suite is
// named from the identifier its document declares: hyphens are spaces and
// the first letter is a capital.
func readableName(id string) string {
	words := strings.TrimSpace(strings.ReplaceAll(id, "-", " "))
	if words == "" {
		return id
	}
	first, size := utf8.DecodeRuneInString(words)
	return string(unicode.ToUpper(first)) + words[size:]
}

// suiteID derives a suite document's identifier from its name: its letters
// and digits lowercased, every other run of characters one hyphen, starting
// with a letter and at most 64 characters long.
func suiteID(name string) string {
	var built strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if hyphen && built.Len() > 0 {
				built.WriteByte('-')
			}
			built.WriteRune(r)
			hyphen = false
		default:
			hyphen = true
		}
	}
	id := built.String()
	if id == "" || id[0] < 'a' || id[0] > 'z' {
		id = "suite-" + id
	}
	if len(id) > 64 {
		id = strings.TrimRight(id[:64], "-")
	}
	return strings.TrimRight(id, "-")
}

// draftOfDocument is the draft a readmit-suite/v1 document reads as, every
// reference resolved against folder, where its relative references resolve
// from: a spec to the test version holding it, a case to the project's case,
// a target to the environment holding it and an observation to the one that
// reads its ledger. A reference that resolves to nothing stays as the
// document declares it, in its Source; nothing is dropped. Datasets and
// environments are named by their identifiers, and the draft
// declares no requirement or exclusion, which the suite contract has no room
// for.
func (c *loadedCatalog) draftOfDocument(document suite.Document, folder string) SuiteDraft {
	local, _ := filepath.EvalSymlinks(folder)
	here := local == c.root
	draft := SuiteDraft{ID: document.ID, Owner: document.Owner, Tags: slices.Clone(document.Tags), Concurrency: document.Parallelism}
	for _, test := range document.Tests {
		shown := SuiteTestDraft{ID: test.ID, Test: ItemRef{Kind: TestItem}, Dataset: test.Table, Parameter: test.Parameter,
			After: slices.Clone(test.After), Isolation: test.Isolation, Sequence: slices.Clone(test.Sequence), Owner: test.Owner, Tags: slices.Clone(test.Tags)}
		if ref := c.memberHolding(TestItem, "test", folder, test.Spec, here); ref != nil {
			shown.Test = *ref
		} else {
			shown.Source = test.Spec
		}
		draft.Tests = append(draft.Tests, shown)
	}
	for _, table := range document.Tables {
		dataset := SuiteDataset{ID: table.ID, Name: table.ID}
		for _, row := range table.Rows {
			shown := SuiteDataRow{ID: row.ID, Case: ItemRef{Kind: CaseItem}, Expected: row.Expected}
			if ref := c.caseHolding(folder, row.Case, here); ref != nil {
				shown.Case = *ref
			} else {
				shown.Source = row.Case
			}
			dataset.Rows = append(dataset.Rows, shown)
		}
		draft.Datasets = append(draft.Datasets, dataset)
	}
	for _, environment := range document.Environments {
		shown := SuiteEnvironment{ID: environment.ID, Name: environment.ID, Site: environment.Site}
		for _, binding := range environment.Bindings {
			bound := SuiteBinding{Parameter: binding.Parameter, Target: ItemRef{Kind: EnvironmentItem}}
			if ref := c.memberHolding(EnvironmentItem, "target", folder, binding.Target, here); ref != nil {
				bound.Target = ItemRef{Kind: EnvironmentItem, ID: ref.ID}
			} else {
				bound.TargetSource = binding.Target
			}
			if binding.Observation != "" {
				observed := ""
				if here && artifactpath.EntryName(binding.Observation) == nil {
					observed = c.observationByLedger(binding.Observation)
				}
				if observed != "" {
					bound.Observation = &ItemRef{Kind: ObservationItem, ID: observed}
				} else {
					bound.ObservationSource = binding.Observation
				}
			}
			shown.Bindings = append(shown.Bindings, bound)
		}
		draft.Environments = append(draft.Environments, shown)
	}
	return normalizedSuite(draft)
}

// memberHolding is the saved object of kind whose revision holds the file a
// document references from folder: the revision whose member of role is that
// very entry of the project, or else the one holding exactly its bytes.
func (c *loadedCatalog) memberHolding(kind ItemKind, role, folder, reference string, here bool) *ItemRef {
	entry := ""
	if here && artifactpath.EntryName(reference) == nil {
		entry = reference
	}
	digest := ""
	if data, err := boundedFile(artifactpath.JoinReference(folder, reference), catalog.MaxMemberBytes); err == nil {
		sum := sha256.Sum256(data)
		digest = hex.EncodeToString(sum[:])
	}
	var byBytes *ItemRef
	for _, item := range c.document.Items {
		if item.Kind != string(kind) || c.removed(item) {
			continue
		}
		for _, revision := range slices.Backward(item.Revisions) {
			for _, member := range revision.Members {
				if member.Role != role {
					continue
				}
				ref := &ItemRef{Kind: kind, ID: item.ID, Revision: strconv.Itoa(revision.Number)}
				if entry != "" && member.Path == entry {
					return ref
				}
				if byBytes == nil && digest != "" && member.SHA256 == digest {
					byBytes = ref
				}
			}
		}
	}
	return byBytes
}

// caseHolding is the project's case a document's row references from folder:
// the case at that entry of the project, or else the case whose evidence
// verifies as the same identity.
func (c *loadedCatalog) caseHolding(folder, reference string, here bool) *ItemRef {
	if here && artifactpath.EntryName(reference) == nil {
		if ref := c.entryRef(CaseItem, reference); ref != nil {
			return &ItemRef{Kind: CaseItem, ID: ref.ID}
		}
		if ref := c.entryRef(VariantItem, reference); ref != nil {
			return &ItemRef{Kind: VariantItem, ID: ref.ID}
		}
	}
	if source, err := bundle.Open(artifactpath.JoinReference(folder, reference)); err == nil {
		return c.caseByIdentity(source.Identity)
	}
	return nil
}

// suitePlan is a suite draft validated and resolved against the project:
// the version of each test it pins and the project entries its tests, rows
// and bindings compile to, with the environment revision each binding
// follows now.
type suitePlan struct {
	draft        SuiteDraft
	tests        []*savedTest
	names        []string
	specs        []string
	cases        [][]string
	targets      [][]string
	environments [][]ItemRef
	observations [][]string
}

// runnable reports whether the plan compiles to a suite that can run: one
// with a test and an environment.
func (p *suitePlan) runnable() bool {
	return len(p.draft.Tests) > 0 && len(p.draft.Environments) > 0
}

// planSuite validates a whole suite draft against the project and resolves
// it, or answers every problem it has, each at the dotted path of the member
// it is about. It never filters: a partial row, an unresolved reference or
// a dependency on a removed test stays in the draft and is a problem at its
// member.
func (c *loadedCatalog) planSuite(draft SuiteDraft) (*suitePlan, []FieldProblem) {
	draft = normalizedSuite(draft)
	problems := []FieldProblem{}
	problem := func(field, text string) { problems = append(problems, FieldProblem{Field: field, Problem: text}) }
	plan := &suitePlan{draft: draft}

	if !suiteIdentifier.MatchString(draft.ID) {
		problem("id", "a suite's identifier is a lowercase word of letters, digits and hyphens")
	}
	if draft.Owner != "" && !suiteText(draft.Owner, 256) {
		problem("owner", "an owner is at most 256 bytes of printable text")
	}
	if !suiteTags(draft.Tags) {
		problem("tags", "tags are at most 32 distinct lowercase words")
	}
	if draft.Concurrency < 1 || draft.Concurrency > maxConcurrency {
		problem("concurrency", "a suite runs 1 to "+strconv.Itoa(maxConcurrency)+" jobs at once")
	}
	if len(draft.Tests) > 64 {
		problem("tests", "a suite holds at most 64 tests")
	}
	if len(draft.Datasets) > 64 {
		problem("datasets", "a suite holds at most 64 datasets")
	}
	if len(draft.Environments) > 32 {
		problem("environments", "a suite holds at most 32 environments")
	}
	if len(draft.Requirements) > 256 {
		problem("requirements", "a suite declares at most 256 requirements")
	}
	if len(draft.Exclusions) > 64 {
		problem("exclusions", "a suite declares at most 64 exclusions")
	}

	// Tests: each at the exact saved version it pins.
	testIndex := map[string]int{}
	datasetIndex := map[string]int{}
	for i, dataset := range draft.Datasets {
		if suiteIdentifier.MatchString(dataset.ID) {
			if _, taken := datasetIndex[dataset.ID]; !taken {
				datasetIndex[dataset.ID] = i
			}
		}
	}
	plan.tests, plan.names, plan.specs = make([]*savedTest, len(draft.Tests)), make([]string, len(draft.Tests)), make([]string, len(draft.Tests))
	for i, test := range draft.Tests {
		at := "tests." + strconv.Itoa(i)
		plan.names[i] = cmp.Or(test.ID, "test "+strconv.Itoa(i+1))
		switch _, taken := testIndex[test.ID]; {
		case !suiteIdentifier.MatchString(test.ID):
			problem(at+".id", "a test's identifier is a lowercase word of letters, digits and hyphens")
		case taken:
			problem(at+".id", "two tests of this suite share an identifier")
		default:
			testIndex[test.ID] = i
		}
		saved, name, spec, reason := c.pinnedTest(test)
		if reason != "" {
			problem(at+".test", reason)
		}
		plan.tests[i], plan.specs[i] = saved, spec
		if name != "" {
			plan.names[i] = name
		}
		if saved != nil && !slices.Equal(test.Sequence, saved.spec.Input.Messages) {
			problem(at+".sequence", "the send order comes from the test: this version sends its messages in the order it declares them, and a suite cannot change it")
		}
		switch _, held := datasetIndex[test.Dataset]; {
		case test.Dataset == "":
			problem(at+".dataset", "choose the dataset this test runs over")
		case !held:
			problem(at+".dataset", "the suite holds no such dataset")
		}
		if !suiteIdentifier.MatchString(test.Parameter) {
			problem(at+".parameter", "choose the environment parameter this test is bound through")
		}
		if test.Isolation != runqueue.SharedState && test.Isolation != runqueue.IsolatedState {
			problem(at+".isolation", "a test's state is shared or isolated")
		}
		if test.Owner != "" && !suiteText(test.Owner, 256) {
			problem(at+".owner", "an owner is at most 256 bytes of printable text")
		}
		if !suiteTags(test.Tags) {
			problem(at+".tags", "tags are at most 32 distinct lowercase words")
		}
	}
	// Dependencies name other tests of the suite, once each, and never
	// depend on each other in a cycle.
	edges := make([][]int, len(draft.Tests))
	for i, test := range draft.Tests {
		at := "tests." + strconv.Itoa(i) + ".after"
		seen := map[string]bool{}
		for _, dependency := range test.After {
			target, held := testIndex[dependency]
			switch {
			case seen[dependency]:
				problem(at, "this test names a dependency twice")
			case dependency == test.ID:
				problem(at, "a test cannot depend on itself")
			case !held:
				problem(at, "this test depends on a test this suite does not hold")
			default:
				edges[i] = append(edges[i], target)
			}
			seen[dependency] = true
		}
	}
	for i, cycle := range dependencyCycles(edges) {
		if len(cycle) == 0 {
			continue
		}
		named := []string{}
		for _, member := range cycle {
			named = append(named, plan.names[member])
		}
		problem("tests."+strconv.Itoa(i)+".after", "this test is in a dependency cycle with "+strings.Join(named, ", ")+"; tests that wait for each other never run")
	}

	// Datasets: rows of the project's cases, each overriding checks every
	// test over the dataset declares, with a value of the check's own type.
	users := map[string][]int{}
	for i, test := range draft.Tests {
		users[test.Dataset] = append(users[test.Dataset], i)
	}
	names := map[string]bool{}
	plan.cases = make([][]string, len(draft.Datasets))
	for n, dataset := range draft.Datasets {
		at := "datasets." + strconv.Itoa(n)
		switch first := datasetIndex[dataset.ID]; {
		case !suiteIdentifier.MatchString(dataset.ID):
			problem(at+".id", "a dataset's identifier is a lowercase word of letters, digits and hyphens")
		case first != n:
			problem(at+".id", "two datasets of this suite share an identifier")
		}
		switch {
		case !catalog.ValidName(dataset.Name):
			problem(at+".name", "a dataset is named")
		case names[dataset.Name]:
			problem(at+".name", "two datasets of this suite share a name")
		}
		names[dataset.Name] = true
		used := datasetIndex[dataset.ID] == n && len(users[dataset.ID]) > 0
		switch {
		case len(dataset.Rows) > 64:
			problem(at+".rows", "a dataset holds at most 64 rows")
		case len(dataset.Rows) == 0 && used:
			problem(at+".rows", "a dataset a test runs over holds at least one row")
		}
		rows := map[string]bool{}
		plan.cases[n] = make([]string, len(dataset.Rows))
		for m, row := range dataset.Rows {
			rowAt := at + ".rows." + strconv.Itoa(m)
			switch {
			case !suiteIdentifier.MatchString(row.ID):
				problem(rowAt+".id", "a row's identifier is a lowercase word of letters, digits and hyphens")
			case rows[row.ID]:
				problem(rowAt+".id", "two rows of this dataset share an identifier")
			}
			rows[row.ID] = true
			entry, reason := c.rowCase(row)
			if reason != "" {
				problem(rowAt+".case", reason)
			}
			plan.cases[n][m] = entry
			for _, check := range slices.Sorted(mapKeys(row.Expected)) {
				if !suiteIdentifier.MatchString(check) {
					problem(rowAt+".expected."+check, "no check has this identifier")
				} else if reason := expectedProblem(check, row.Expected[check], users[dataset.ID], plan); reason != "" && used {
					problem(rowAt+".expected."+check, reason)
				}
			}
		}
	}

	// Environments: every parameter the tests use, bound once to a named
	// environment and, where a test reads appointment records, a named
	// observation.
	parameters := map[string][]int{}
	for i, test := range draft.Tests {
		parameters[test.Parameter] = append(parameters[test.Parameter], i)
	}
	environmentIDs, environmentNames := map[string]bool{}, map[string]bool{}
	plan.targets, plan.observations, plan.environments = make([][]string, len(draft.Environments)), make([][]string, len(draft.Environments)), make([][]ItemRef, len(draft.Environments))
	for n, environment := range draft.Environments {
		at := "environments." + strconv.Itoa(n)
		switch {
		case !suiteIdentifier.MatchString(environment.ID):
			problem(at+".id", "an environment's identifier is a lowercase word of letters, digits and hyphens")
		case environmentIDs[environment.ID]:
			problem(at+".id", "two environments of this suite share an identifier")
		}
		environmentIDs[environment.ID] = true
		switch {
		case !catalog.ValidName(environment.Name):
			problem(at+".name", "an environment of the suite is named")
		case environmentNames[environment.Name]:
			problem(at+".name", "two environments of this suite share a name")
		}
		environmentNames[environment.Name] = true
		if !suiteText(environment.Site, 256) {
			problem(at+".site", "an environment names its site in at most 256 bytes")
		}
		bound := map[string]bool{}
		plan.targets[n], plan.observations[n], plan.environments[n] = make([]string, len(environment.Bindings)), make([]string, len(environment.Bindings)), make([]ItemRef, len(environment.Bindings))
		for m, binding := range environment.Bindings {
			bindingAt := at + ".bindings." + strconv.Itoa(m)
			through, held := parameters[binding.Parameter]
			switch {
			case bound[binding.Parameter]:
				problem(bindingAt+".parameter", "this parameter is bound twice in one environment")
			case !held:
				problem(bindingAt+".parameter", "no test of this suite is bound through this parameter")
			}
			bound[binding.Parameter] = true
			target, revision, reason := c.bindingTarget(binding)
			if reason != "" {
				problem(bindingAt+".target", reason)
			}
			plan.targets[n][m], plan.environments[n][m] = target, revision
			ledger := slices.ContainsFunc(through, func(i int) bool {
				return plan.tests[i] != nil && plan.tests[i].spec.Observation.Boundary == testrunner.LedgerBoundary
			})
			observation, reason := c.bindingObservation(binding, ledger)
			if reason != "" {
				problem(bindingAt+".observation", reason)
			}
			plan.observations[n][m] = observation
		}
		for _, parameter := range slices.Sorted(mapKeys(parameters)) {
			if !bound[parameter] && suiteIdentifier.MatchString(parameter) {
				problem(at+".bindings", "this environment binds nothing to "+parameter+", which tests of the suite are bound through")
			}
		}
	}

	// Requirements and exclusions: declarations over the suite's tests.
	requirementIDs, requirementNames := map[string]bool{}, map[string]bool{}
	for n, requirement := range draft.Requirements {
		at := "requirements." + strconv.Itoa(n)
		switch {
		case !suiteIdentifier.MatchString(requirement.ID):
			problem(at+".id", "a requirement's identifier is a lowercase word of letters, digits and hyphens")
		case requirementIDs[requirement.ID]:
			problem(at+".id", "two requirements of this suite share an identifier")
		}
		requirementIDs[requirement.ID] = true
		switch {
		case !catalog.ValidName(requirement.Name):
			problem(at+".name", "a requirement is named")
		case requirementNames[requirement.Name]:
			problem(at+".name", "two requirements of this suite share a name")
		}
		requirementNames[requirement.Name] = true
		seen := map[string]bool{}
		for _, test := range requirement.Tests {
			if _, held := testIndex[test]; !held || seen[test] {
				problem(at+".tests", "a requirement names tests of this suite, each once")
				break
			}
			seen[test] = true
		}
	}
	excluded := map[string]bool{}
	for n, exclusion := range draft.Exclusions {
		at := "exclusions." + strconv.Itoa(n)
		switch _, held := testIndex[exclusion.Test]; {
		case !held:
			problem(at+".test", "choose a test of this suite")
		case excluded[exclusion.Test]:
			problem(at+".test", "a test is excluded once")
		}
		excluded[exclusion.Test] = true
		if !slices.Contains(exclusionStates, exclusion.State) {
			problem(at+".state", "an exclusion declares a test skipped, unsupported, quarantined or disabled")
		}
		if !suiteText(exclusion.Reason, 1024) {
			problem(at+".reason", "an exclusion gives its reason in 1 to 1024 bytes")
		}
		if until, err := time.Parse(time.RFC3339, exclusion.Until); err != nil || until.UTC().Format(time.RFC3339) != exclusion.Until {
			problem(at+".until", "an exclusion lasts until an exact time in UTC, to the second")
		}
	}
	return plan, problems
}

// pinnedTest reads the exact saved test version a suite test pins: the
// version, its name and the project entry its spec is, or why it cannot be
// pinned.
func (c *loadedCatalog) pinnedTest(test SuiteTestDraft) (*savedTest, string, string, string) {
	ref := test.Test
	switch {
	case ref.ID == "" && test.Source != "":
		return nil, "", "", "this suite names a test the project does not hold as a saved version (" + test.Source + "); choose a saved test"
	case ref.ID == "":
		return nil, "", "", "choose the saved test this suite runs"
	}
	index := c.document.Find(ref.ID)
	if ref.Kind != TestItem || index < 0 || c.document.Items[index].Kind != string(TestItem) || c.removed(c.document.Items[index]) {
		return nil, "", "", "the project holds no such test"
	}
	item := c.document.Items[index]
	if ref.Revision == "" || len(item.Revisions) == 0 {
		return nil, "", "", "choose the saved version of this test the suite runs"
	}
	saved, err := c.testOf(item, ref.Revision)
	if err != nil {
		return nil, "", "", "that version of the test cannot be read: " + err.Error()
	}
	name := cmp.Or(item.Name, saved.spec.Name)
	if saved.release {
		return nil, name, "", "this version is a test release, which a suite cannot run as its template; choose a saved version of the test"
	}
	paths, _, _ := c.revisionBacking(item, ref.Revision)
	entry := c.entryOf(paths[primaryRole(TestItem)])
	if entry == "" {
		return nil, name, "", "that version of the test is not one entry of the project"
	}
	return saved, name, entry, ""
}

// rowCase is the project entry of the case a row sends, or why it has none.
func (c *loadedCatalog) rowCase(row SuiteDataRow) (string, string) {
	ref := row.Case
	switch {
	case ref.ID == "" && row.Source != "":
		return "", "this row names a case the project does not hold (" + row.Source + "); choose a case of the project"
	case ref.ID == "":
		return "", "choose the case this row sends"
	}
	index := c.document.Find(ref.ID)
	if index < 0 || ref.Kind != CaseItem && ref.Kind != VariantItem || c.document.Items[index].Kind != string(ref.Kind) ||
		c.removed(c.document.Items[index]) || c.document.Items[index].Entry == "" {
		return "", "the project holds no such case"
	}
	return c.document.Items[index].Entry, ""
}

// bindingTarget is the target entry of the current revision of the named
// environment a binding follows, with that revision, or why it has none.
func (c *loadedCatalog) bindingTarget(binding SuiteBinding) (string, ItemRef, string) {
	switch {
	case binding.Target.ID == "" && binding.TargetSource != "":
		return "", ItemRef{}, "this binding names a target the project does not hold as an environment (" + binding.TargetSource + "); choose an environment"
	case binding.Target.ID == "":
		return "", ItemRef{}, "choose the environment this parameter is bound to"
	case binding.Target.Kind != EnvironmentItem:
		return "", ItemRef{}, "the project holds no such environment"
	}
	_, entry, reason := draftScope{root: c.root, loaded: c}.environmentOfTest(binding.Target.ID)
	if reason != "" {
		return "", ItemRef{}, reason
	}
	item := c.document.Items[c.document.Find(binding.Target.ID)]
	return entry, ItemRef{Kind: EnvironmentItem, ID: item.ID, Revision: item.RevisionLabel()}, ""
}

// bindingObservation is the ledger entry of the named observation a binding
// reads, which it names exactly when a test bound through its parameter
// reads appointment records, or why it cannot.
func (c *loadedCatalog) bindingObservation(binding SuiteBinding, ledger bool) (string, string) {
	named := binding.Observation != nil && binding.Observation.ID != ""
	switch {
	case !ledger && (named || binding.ObservationSource != ""):
		return "", "no test bound through this parameter reads appointment records, so it reads no observation"
	case !ledger:
		return "", ""
	case !named && binding.ObservationSource != "":
		return "", "this binding names an observation the project does not hold (" + binding.ObservationSource + "); choose an observation"
	case !named:
		return "", "a test bound through this parameter reads appointment records; choose the observation it reads"
	case binding.Observation.Kind != ObservationItem:
		return "", "the project holds no such observation"
	}
	entry, reason := draftScope{root: c.root, loaded: c}.observationOfTest(binding.Observation.ID)
	return entry, reason
}

// expectedProblem is why one row's expected value cannot replace a check of
// the tests over its dataset: a check one of them does not declare, or a
// value that is not of the check's type.
func expectedProblem(check string, value testrunner.Value, users []int, plan *suitePlan) string {
	for _, i := range users {
		saved := plan.tests[i]
		if saved == nil {
			continue
		}
		at := slices.IndexFunc(saved.spec.Assertions, func(assertion testrunner.Assertion) bool { return assertion.ID == check })
		if at < 0 {
			return plan.names[i] + " declares no check " + check + "; a row overrides only checks every test over its dataset declares"
		}
		members := 0
		for _, set := range []bool{value.Count != nil, value.Records != nil, value.Field != nil} {
			if set {
				members++
			}
		}
		switch operator := saved.spec.Assertions[at].Operator; {
		case operator == testauthor.LedgerCount && (value.Count == nil || members != 1):
			return "this check expects a count of appointment records"
		case operator == testauthor.LedgerCount && *value.Count < 0:
			return "a count is zero or more"
		case operator == testauthor.LedgerEquals && (value.Records == nil || members != 1):
			return "this check expects a list of appointment records"
		case operator == testauthor.ACKFieldEquals && (value.Field == nil || members != 1):
			return "this check expects an acknowledgement field's value"
		case operator == testauthor.ACKFieldEquals && value.Field.Validate() != nil:
			return "an acknowledgement field is present with its text, or empty, null or not present"
		}
	}
	return ""
}

// dependencyCycles answers, for each test, the tests it waits for in a cycle
// with — itself included — or nothing when it is in none.
func dependencyCycles(edges [][]int) [][]int {
	reach := make([][]bool, len(edges))
	for i := range edges {
		reach[i] = make([]bool, len(edges))
		stack := slices.Clone(edges[i])
		for len(stack) > 0 {
			next := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if reach[i][next] {
				continue
			}
			reach[i][next] = true
			stack = append(stack, edges[next]...)
		}
	}
	cycles := make([][]int, len(edges))
	for i := range edges {
		if !reach[i][i] {
			continue
		}
		for j := range edges {
			if reach[i][j] && reach[j][i] {
				cycles[i] = append(cycles[i], j)
			}
		}
	}
	return cycles
}

// suiteText is bounded printable text, as the suite contract holds an owner,
// a site and a reason.
func suiteText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

// suiteTags are at most 32 distinct identifiers.
func suiteTags(values []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if !suiteIdentifier.MatchString(value) || seen[value] {
			return false
		}
		seen[value] = true
	}
	return len(values) <= 32
}

func mapKeys[V any](values map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for key := range values {
			if !yield(key) {
				return
			}
		}
	}
}

// document compiles a runnable plan to the readmit-suite/v1 document it
// is, every reference an entry of the project, and its canonical bytes.
func (p *suitePlan) document() (suite.Document, []byte, error) {
	draft := p.draft
	owner := cmp.Or(draft.Owner, unassignedOwner)
	document := suite.Document{Schema: suite.Schema, ID: draft.ID, Owner: owner, Tags: orEmpty(draft.Tags), Parallelism: draft.Concurrency,
		Environments: []suite.Environment{}, Tables: []suite.Table{}, Tests: []suite.Test{}}
	for n, environment := range draft.Environments {
		compiled := suite.Environment{ID: environment.ID, Site: environment.Site, Bindings: []suite.Binding{}}
		for m, binding := range environment.Bindings {
			compiled.Bindings = append(compiled.Bindings, suite.Binding{Parameter: binding.Parameter, Target: p.targets[n][m], Observation: p.observations[n][m]})
		}
		document.Environments = append(document.Environments, compiled)
	}
	for n, dataset := range draft.Datasets {
		if len(dataset.Rows) == 0 {
			// Only a dataset no test runs over holds no row, and the suite
			// contract has no room for one.
			continue
		}
		table := suite.Table{ID: dataset.ID, Rows: []suite.Row{}}
		for m, row := range dataset.Rows {
			compiled := suite.Row{ID: row.ID, Case: p.cases[n][m]}
			if len(row.Expected) > 0 {
				compiled.Expected = row.Expected
			}
			table.Rows = append(table.Rows, compiled)
		}
		document.Tables = append(document.Tables, table)
	}
	for i, test := range draft.Tests {
		compiled := suite.Test{ID: test.ID, Spec: p.specs[i], Owner: cmp.Or(test.Owner, owner), Tags: orEmpty(test.Tags), Parameter: test.Parameter,
			Table: test.Dataset, Isolation: test.Isolation, Sequence: test.Sequence}
		if len(test.After) > 0 {
			compiled.After = test.After
		}
		document.Tests = append(document.Tests, compiled)
	}
	data, err := encodeMember(document)
	if err != nil {
		return suite.Document{}, nil, err
	}
	decoded, err := suite.Decode(data)
	if err != nil {
		return suite.Document{}, nil, err
	}
	return decoded, data, nil
}

// compile compiles a runnable plan and previews it against each of its
// environments, as preparation would expand it, without writing anything.
// A refusal of the suite reader or the expansion is a problem at "suite".
func (c *loadedCatalog) compile(plan *suitePlan) (*suite.Document, []byte, []FieldProblem) {
	document, data, err := plan.document()
	if err != nil {
		return nil, nil, []FieldProblem{{Field: "suite", Problem: "the suite cannot be compiled: " + err.Error()}}
	}
	for _, environment := range plan.draft.Environments {
		if _, err := suite.Preview(c.root, document, environment.ID, ""); err != nil {
			return nil, nil, []FieldProblem{{Field: "suite", Problem: environment.Name + ": " + err.Error()}}
		}
	}
	return &document, data, nil
}

// compiled is a version compiled against the project as it is now: the
// original file exactly as it is, or a managed version's definition against
// the current revision of each environment it binds. A version that is not
// runnable, or that no longer compiles, is refused with the reason.
func (c *loadedCatalog) compiled(version *suiteVersion) (*suite.Document, []byte, refusal) {
	if !version.runnable() {
		return nil, nil, refusal{Failed, notRunnable}
	}
	if version.connected != nil {
		return nil, version.data, refusal{}
	}
	if version.original() {
		return version.document, version.data, refusal{}
	}
	plan, problems := c.planSuite(version.draft)
	if len(problems) == 0 && plan.runnable() {
		_, data, err := plan.document()
		if err == nil {
			document, err := suite.Decode(data)
			if err == nil {
				return &document, data, refusal{}
			}
		}
		problems = append(problems, FieldProblem{Field: "suite", Problem: "the suite cannot be compiled"})
	}
	return nil, nil, refusal{Failed, "this version no longer compiles against the project: " + firstProblem(problems)}
}

// firstProblem is the text of the first problem found.
func firstProblem(problems []FieldProblem) string {
	if len(problems) == 0 {
		return notRunnable
	}
	return problems[0].Problem
}

// validateSuiteItem validates a whole suite draft for a save: every problem
// at its member, or the members one version publishes — its definition and,
// when it is runnable, the compiled suite — and the normalized draft.
func validateSuiteItem(scope draftScope, draft ItemDraft) ([]catalog.Staged, ItemDraft, []FieldProblem) {
	if draft.Suite.Connected != nil {
		return validateConnectedSuiteItem(scope, draft)
	}
	problems := []FieldProblem{}
	if draft.Name == "" {
		problems = append(problems, FieldProblem{Field: "name", Problem: nameRule})
	}
	suiteDraft := normalizedSuite(*draft.Suite)
	if suiteDraft.ID == "" && scope.item != "" {
		if index := scope.loaded.document.Find(scope.item); index >= 0 && scope.loaded.document.Items[index].Kind == string(SuiteItem) {
			if current, err := scope.loaded.suiteVersion(scope.loaded.document.Items[index], ""); err == nil {
				suiteDraft.ID = current.draft.ID
			}
		}
	}
	if suiteDraft.ID == "" && draft.Name != "" {
		suiteDraft.ID = suiteID(draft.Name)
	}
	// A binding follows its named environment and observation: the saved
	// definition names each by identity alone.
	for n := range suiteDraft.Environments {
		for m := range suiteDraft.Environments[n].Bindings {
			binding := &suiteDraft.Environments[n].Bindings[m]
			binding.Target.Revision = ""
			if binding.Observation != nil {
				observed := *binding.Observation
				observed.Revision = ""
				binding.Observation = &observed
			}
		}
	}
	normalized := ItemDraft{Name: draft.Name, Suite: &suiteDraft}
	plan, found := scope.loaded.planSuite(suiteDraft)
	problems = append(problems, found...)
	if len(problems) > 0 {
		return nil, normalized, problems
	}
	var staged []catalog.Staged
	if plan.runnable() {
		_, data, compiledProblems := scope.loaded.compile(plan)
		if len(compiledProblems) > 0 {
			return nil, normalized, compiledProblems
		}
		staged = append(staged, catalog.Staged{Role: "suite", File: "suite.json", Data: data})
	}
	definition, err := encodeMember(suiteDefinition{Schema: SuiteDefinitionSchema, Draft: suiteDraft})
	if err != nil || len(definition) > suite.MaxBytes {
		return nil, normalized, []FieldProblem{{Field: "suite", Problem: "the suite's definition cannot be encoded within its bound"}}
	}
	return append(staged, catalog.Staged{Role: "definition", File: "definition.json", Data: definition}), normalized, problems
}

// readSuite reads a suite through its current version: its tests, the names
// of its environments, the entry of its suite document and the latest run of
// exactly this version.
func readSuite(c *loadedCatalog, item catalog.Item, _ map[string]string) (view, error) {
	version, err := c.suiteVersion(item, "")
	if err != nil {
		return view{}, err
	}
	environments := []string{}
	for _, environment := range version.draft.Environments {
		environments = append(environments, environment.Name)
	}
	summary := &SuiteSummary{Tests: len(version.draft.Tests), Environments: environments, Entry: version.entry, Runnable: version.runnable()}
	if version.connected != nil {
		summary.Tests = len(version.connected.Tests)
		for _, environment := range version.connected.Environments {
			summary.Environments = append(summary.Environments, environment.ID)
		}
	}
	if runs := c.suiteRunsOf(item, version.label); len(runs) > 0 {
		summary.LatestRun = &ItemRef{Kind: RunItem, ID: runs[0].id}
		summary.LatestRunAt, summary.LatestOutcome = stampedTime(runs[0].started), runs[0].outcome
	}
	name := ""
	if version.original() {
		if version.connected != nil {
			name = version.connected.ID
		} else {
			name = version.document.ID
		}
	}
	return view{name: name, summary: ItemSummary{Suite: summary}}, nil
}

// suiteFingerprint is a suite document with every binding's target and
// observation cleared: a run follows each named environment to its current
// revision, so the runs of one version differ from it only there.
func suiteFingerprint(document suite.Document) string {
	document.Environments = slices.Clone(document.Environments)
	for i := range document.Environments {
		bindings := slices.Clone(document.Environments[i].Bindings)
		for j := range bindings {
			bindings[j].Target, bindings[j].Observation = "", ""
		}
		document.Environments[i].Bindings = bindings
	}
	data, _ := json.Marshal(document, json.Deterministic(true))
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// suiteView is one suite of the project: its identity, the identifier its
// current version declares and the fingerprint of each of its runnable
// versions, by version.
type suiteView struct {
	item         catalog.Item
	name         string
	fingerprints map[string]string
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
		held := suiteView{item: item, fingerprints: map[string]string{}}
		for _, label := range suiteLabels(item) {
			version, err := c.suiteVersion(item, label)
			if err != nil || !version.runnable() {
				continue
			}
			if version.connected != nil {
				data, err := json.Marshal(version.connected, json.Deterministic(true))
				if err == nil {
					held.fingerprints[label] = suite.Identity(data)
					held.name = version.connected.ID
				}
				continue
			}
			held.fingerprints[label] = suiteFingerprint(*version.document)
			held.name = version.document.ID
		}
		c.suiteViews = append(c.suiteViews, held)
	}
	return c.suiteViews
}

// versionOf is the newest version of a suite a run executed, or empty.
func (s suiteView) versionOf(run suiteRunView) string {
	labels := suiteLabels(s.item)
	for _, label := range slices.Backward(labels) {
		if fingerprint, held := s.fingerprints[label]; held && fingerprint == run.fingerprint {
			return label
		}
	}
	return ""
}

// suiteOf is the project's suite an execution ran, at the version it ran:
// the suite one of whose versions it executed, or else the one suite that
// declares its identifier.
func (c *loadedCatalog) suiteOf(run suiteRunView) *ItemRef {
	var named []ItemRef
	for _, held := range c.suites() {
		if label := held.versionOf(run); label != "" {
			ref := ItemRef{Kind: SuiteItem, ID: held.item.ID}
			if label != originalVersion {
				ref.Revision = label
			}
			return &ref
		}
		if held.name == run.suite {
			named = append(named, ItemRef{Kind: SuiteItem, ID: held.item.ID})
		}
	}
	if len(named) == 1 {
		return &named[0]
	}
	return nil
}

// suiteRunsOf are the retained executions of one version of a suite, the
// most recently started first: those for which it is the newest version
// they executed.
func (c *loadedCatalog) suiteRunsOf(item catalog.Item, label string) []suiteRunView {
	var held *suiteView
	for _, view := range c.suites() {
		if view.item.ID == item.ID {
			held = &view
		}
	}
	matched := []suiteRunView{}
	if held == nil {
		return matched
	}
	for _, run := range c.suiteExecutions() {
		if held.versionOf(run) == label {
			matched = append(matched, run)
		}
	}
	slices.SortStableFunc(matched, func(x, y suiteRunView) int {
		return cmp.Or(y.started.Compare(x.started), cmp.Compare(x.id, y.id))
	})
	return matched
}

// openSuiteDraft answers the draft a suite editor starts from: a new suite
// with nothing in it yet, one version of a managed suite as it was saved,
// read-only unless it is current, or an original file read losslessly and
// read-only, with the problems its first save must resolve.
func (a *App) openSuiteDraft(ctx context.Context, request ItemRequest) ItemDraftResult {
	result := ItemDraftResult{Context: request.Context}
	if request.Ref.ID == "" {
		if _, declined := a.projectRoot(ctx, request.Context); declined.state != "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		draft := normalizedSuite(SuiteDraft{Concurrency: 1})
		result.State, result.New, result.Ref = Completed, true, &ItemRef{Kind: SuiteItem}
		result.Draft, result.Suite = &ItemDraft{Suite: &draft}, &SuiteContext{Tests: []SuiteTestVersion{}}
		return result
	}
	loaded, _, refused := a.catalogItem(ctx, request.Context, ItemRef{Kind: SuiteItem, ID: request.Ref.ID}, false)
	if loaded == nil {
		result.refuse(refused.State, refused.Reason)
		return result
	}
	item := loaded.document.Items[loaded.document.Find(request.Ref.ID)]
	version, err := loaded.suiteVersion(item, request.Ref.Revision)
	if err != nil {
		result.refuse(Failed, "this suite cannot be read: "+err.Error())
		return result
	}
	draft := version.draft
	shown := &SuiteContext{Original: version.original(), ReadOnly: version.original() || version.label != currentLabel(item),
		Tests: loaded.suiteTestVersions(suiteTestRefs(draft)), Document: string(version.data), Runnable: version.runnable()}
	name := item.Name
	if name == "" && version.document != nil {
		name = version.document.ID
	}
	if name == "" && version.connected != nil {
		name = version.connected.ID
	}
	ref := ItemRef{Kind: SuiteItem, ID: item.ID, Revision: version.label}
	if version.original() && len(item.Revisions) == 0 {
		ref.Revision = ""
	}
	result.State, result.Ref, result.Draft, result.Suite = Completed, &ref, &ItemDraft{Name: name, Suite: &draft}, shown
	if version.original() && version.connected == nil {
		// What the first save of this original must resolve is shown now.
		if _, problems := loaded.planSuite(draft); len(problems) > 0 {
			result.Problems = problems
		}
	}
	return result
}

// suiteTestRefs are the test versions a draft pins, in its order.
func suiteTestRefs(draft SuiteDraft) []ItemRef {
	refs := []ItemRef{}
	for _, test := range draft.Tests {
		refs = append(refs, test.Test)
	}
	return refs
}

// compiledSuitePrefix begins the name of a compiled suite a run, a
// preflight or an approval reads from the project root while it runs: never
// an object of the project, and removed when it is done.
const compiledSuitePrefix = ".readmit-compiled-suite-"

// placeCompiled writes compiled suite bytes as a new private file of the
// project root, where their references resolve, and answers its path and
// the function that removes it.
func placeCompiled(root string, data []byte) (string, func(), error) {
	file, err := os.CreateTemp(root, compiledSuitePrefix+"*.json")
	if err != nil {
		return "", func() {}, errors.New("the compiled suite cannot be placed in the project")
	}
	path := file.Name()
	remove := func() { os.Remove(path) }
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	if closed := file.Close(); err == nil {
		err = closed
	}
	if err != nil {
		remove()
		return "", func() {}, errors.New("the compiled suite cannot be placed in the project")
	}
	return path, remove, nil
}
