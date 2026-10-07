package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/suite"
)

// A suite of connected tests is edited as every suite is — its tests at
// exact saved versions, the datasets they run over and the expected values
// each row overrides, their dependencies, its environments and the named
// environment each binds at its exact version — and saved as one version
// whose definition is the readmit-suite-definition/v3 SuiteDraft. Saving
// writes nothing else and approves nothing.
//
// Approving a version's baseline is the one approval of its expectations:
// every job — a test, or one row of the dataset it runs over — is compiled
// against each environment the version binds, its definition must be the
// same in every one, and the baseline records the readmit-connected-release/v1
// of each job beside the readmit-suite/v2 document the customer runner
// executes. The lifecycles, selections and approvals that document names are
// placed under the project's own folder by their content, and removed again
// when the baseline is not recorded. An environment approval of that
// baseline promotes the version to one environment through
// suite.ApproveConnectedPromotion; it never changes an expectation.

// AuthoredSuiteDefinitionSchema is the definition of a suite of saved
// connected tests: the SuiteDraft it was edited as.
const AuthoredSuiteDefinitionSchema = "readmit-suite-definition/v3"

// connectedFolder is where compiled connected lifecycles, selections and
// approvals are kept, each under its content's identity.
var connectedFolder = filepath.Join(catalog.Folder, "connected")

// connectedJob is one job a suite of connected tests runs: one suite test,
// or one row of the dataset it runs over, with the test as that row sends it.
type connectedJob struct {
	id       string
	test     int
	name     string
	revision string
	draft    ConnectedTestDraft
	after    []string
}

// connectedSuitePlan is a suite of connected tests compiled against the
// project: its jobs, each job's expectation review, which every environment
// shares, and each environment's compiled lifecycle of each job.
type connectedSuitePlan struct {
	jobs     []connectedJob
	reviews  []expectation.ConnectedReview
	compiled [][]*connectedCompiled
}

// connectedSuiteFiles is what approving a suite of connected tests places
// under the project: every file by its path in the project.
type connectedSuiteFiles struct {
	plans map[string]*connectedtest.FlowPlan
	files map[string][]byte
	dirs  []string
}

// isConnectedSuiteDraft reports whether a suite draft pins saved connected
// tests: every test it pins must then be one.
func (c *loadedCatalog) isConnectedSuiteDraft(draft SuiteDraft) bool {
	for _, test := range draft.Tests {
		index := c.document.Find(test.Test.ID)
		if index >= 0 && c.document.Items[index].Kind == string(TestItem) {
			if _, err := c.connectedTestOf(c.document.Items[index], test.Test.Revision); err == nil {
				return true
			}
		}
	}
	return false
}

// connectedJobIDs are the jobs one suite test runs as: one, or one per row
// of the dataset it runs over, each with the identity its lifecycle carries.
func connectedJobIDs(draft SuiteDraft, test SuiteTestDraft) []string {
	id := connectedLifecycleID(test.Test.ID)
	if test.Dataset == "" {
		return []string{id}
	}
	ids := []string{}
	if at := slices.IndexFunc(draft.Datasets, func(set SuiteDataset) bool { return set.ID == test.Dataset }); at >= 0 {
		for _, row := range draft.Datasets[at].Rows {
			ids = append(ids, id+"-"+row.ID)
		}
	}
	return ids
}

// connectedRowKey names one expected value a row overrides: the phase and
// the check, by their identities in the test.
func connectedRowKey(phase, check string) string { return phase + "/" + check }

// rowDraft is a connected test as one row sends it: the case the row names
// in place of the one case its v2 messages come from, and each expected
// value the row overrides in place of the test's own.
func (c *loadedCatalog) rowDraft(d ConnectedTestDraft, row SuiteDataRow) (ConnectedTestDraft, []string) {
	problems := []string{}
	out := d
	out.Steps = slices.Clone(d.Steps)
	cases := []string{}
	for _, step := range d.Steps {
		if step.V2 != nil && !slices.Contains(cases, step.Source.Case.ID) {
			cases = append(cases, step.Source.Case.ID)
		}
	}
	switch {
	case len(cases) == 0:
		problems = append(problems, "a row sends its case in place of the test's v2 messages; this test sends none")
	case len(cases) > 1:
		problems = append(problems, "a row replaces the one case a test's v2 messages come from; this test sends messages of several cases")
	default:
		entry, reason := c.rowCase(row)
		identity := ""
		if reason == "" {
			facts, _, err := operation.VerifiedCase(c.root, entry)
			if err != nil {
				reason = "the case this row sends cannot be verified"
			}
			identity = facts.Identity
		}
		if reason != "" {
			problems = append(problems, reason)
			break
		}
		for i, step := range out.Steps {
			if step.V2 != nil {
				out.Steps[i].Source = ConnectedSource{Case: ItemRef{Kind: row.Case.Kind, ID: row.Case.ID}, Identity: identity, Occurrence: step.Source.Occurrence}
			}
		}
	}
	if len(row.Expected) > 0 {
		problems = append(problems, "a connected test's checks are overridden by the values of its own checks")
	}
	out.Phases = slices.Clone(d.Phases)
	overridden := map[string]bool{}
	for p, phase := range out.Phases {
		out.Phases[p].Checks = slices.Clone(phase.Checks)
		for k, check := range phase.Checks {
			value, held := row.ConnectedExpected[connectedRowKey(phase.ID, check.Check.ID)]
			if !held {
				continue
			}
			overridden[connectedRowKey(phase.ID, check.Check.ID)] = true
			if check.Check.Expected == nil {
				problems = append(problems, check.Name+" expects no single value a row can override")
				continue
			}
			out.Phases[p].Checks[k].Check.Expected = &value
		}
	}
	for _, key := range slices.Sorted(mapKeys(row.ConnectedExpected)) {
		if !overridden[key] {
			problems = append(problems, "this test has no check "+key+" a row can override")
		}
	}
	return out, problems
}

// planConnectedSuite compiles a suite draft of connected tests against the
// project: every job against every environment, at the exact environment
// versions its bindings pin. Nothing is written.
func (c *loadedCatalog) planConnectedSuite(draft SuiteDraft) (*connectedSuitePlan, []FieldProblem) {
	problems := []FieldProblem{}
	add := func(field, problem string) {
		if !slices.ContainsFunc(problems, func(p FieldProblem) bool { return p.Field == field && p.Problem == problem }) {
			problems = append(problems, FieldProblem{Field: field, Problem: problem})
		}
	}
	plan := &connectedSuitePlan{}
	jobsOf := map[string][]string{}
	taken := map[string]bool{}
	for i, test := range draft.Tests {
		field := "suite.tests." + strconv.Itoa(i)
		if !suiteIdentifier.MatchString(test.ID) || jobsOf[test.ID] != nil {
			add(field+".id", "a test's identifier is a lowercase word of letters, digits and hyphens, once per suite")
		}
		index := c.document.Find(test.Test.ID)
		if index < 0 || c.document.Items[index].Kind != string(TestItem) || c.removed(c.document.Items[index]) {
			add(field, "the project holds no such test")
			continue
		}
		item := c.document.Items[index]
		saved, err := c.connectedTestOf(item, test.Test.Revision)
		if err != nil {
			add(field, "a suite of connected tests runs connected tests only; this one is not")
			continue
		}
		if len(test.Sequence) > 0 {
			add(field, "a connected test sends its own inputs in their own order")
		}
		if !suiteIdentifier.MatchString(test.Parameter) {
			add(field+".parameter", "choose the environment parameter this test is bound through")
		}
		ids := connectedJobIDs(draft, test)
		jobsOf[test.ID] = ids
		rows := []*SuiteDataRow{nil}
		if test.Dataset != "" {
			at := slices.IndexFunc(draft.Datasets, func(set SuiteDataset) bool { return set.ID == test.Dataset })
			switch {
			case at < 0:
				add(field+".dataset", "the suite holds no such dataset")
				continue
			case len(draft.Datasets[at].Rows) == 0:
				add(field+".dataset", "a dataset a test runs over holds at least one row")
				continue
			}
			rows = rows[:0]
			for m := range draft.Datasets[at].Rows {
				rows = append(rows, &draft.Datasets[at].Rows[m])
			}
		}
		for r, row := range rows {
			job := connectedJob{id: ids[r], test: i, name: item.Name, revision: test.Test.Revision, draft: saved.draft}
			jobField := field
			if row != nil {
				at := slices.IndexFunc(draft.Datasets, func(set SuiteDataset) bool { return set.ID == test.Dataset })
				jobField = "suite.datasets." + strconv.Itoa(at) + ".rows." + strconv.Itoa(r)
				job.name = item.Name + " · " + row.ID
				var found []string
				job.draft, found = c.rowDraft(saved.draft, *row)
				for _, problem := range found {
					add(jobField, problem)
				}
				if !suiteIdentifier.MatchString(row.ID) || len(job.id) > 64 {
					add(jobField+".id", "a row's identifier is a short lowercase word of letters, digits and hyphens")
				}
			}
			if taken[job.id] {
				add(field, "a suite runs a connected test once, or once per row of a dataset")
			}
			taken[job.id] = true
			for _, problem := range c.connectedProblems(job.draft, "") {
				add(jobField, job.name+": "+problem.Problem)
			}
			plan.jobs = append(plan.jobs, job)
		}
	}
	for j := range plan.jobs {
		for _, dependency := range draft.Tests[plan.jobs[j].test].After {
			if jobsOf[dependency] == nil {
				add("suite.tests."+strconv.Itoa(plan.jobs[j].test)+".after", "a test waits only for the suite's other tests")
				continue
			}
			plan.jobs[j].after = append(plan.jobs[j].after, jobsOf[dependency]...)
		}
	}
	if len(draft.Environments) == 0 {
		add("suite.environments", "add the environment this suite runs in")
	}
	if len(problems) > 0 {
		return nil, problems
	}
	plan.reviews = make([]expectation.ConnectedReview, len(plan.jobs))
	for n, environment := range draft.Environments {
		field := "suite.environments." + strconv.Itoa(n) + ".bindings"
		compiled := make([]*connectedCompiled, len(plan.jobs))
		for j, job := range plan.jobs {
			test := draft.Tests[job.test]
			at := slices.IndexFunc(environment.Bindings, func(b SuiteBinding) bool { return b.Parameter == test.Parameter })
			if at < 0 || environment.Bindings[at].Target.ID == "" {
				add(field, "bind every test's environment in "+cmpOr(environment.Name, environment.ID))
				continue
			}
			binding := environment.Bindings[at]
			if reason := c.pinnedBinding(binding); reason != "" {
				add(field+"."+strconv.Itoa(at), reason)
				continue
			}
			server := job.draft.Server
			if binding.Server != nil {
				server = binding.Server.ID
			}
			lifecycle, found := c.compileConnected(job.draft, job.id, job.revision, connectedEnvironments{environment: binding.Target.ID, server: server, promoted: true, runtimeTemplate: true})
			for _, problem := range found {
				add(field+"."+strconv.Itoa(at), job.name+": "+problem.Problem)
			}
			if lifecycle == nil {
				continue
			}
			review, err := reviewCompiled(lifecycle)
			switch {
			case err != nil:
				add(field+"."+strconv.Itoa(at), job.name+": "+err.Error())
				continue
			case n == 0:
				plan.reviews[j] = review
			case review != plan.reviews[j]:
				add(field+"."+strconv.Itoa(at), job.name+": in this environment the test's expectations differ from the suite's other environments; an environment may change only the binding")
				continue
			}
			compiled[j] = lifecycle
		}
		plan.compiled = append(plan.compiled, compiled)
	}
	if len(problems) > 0 {
		return nil, problems
	}
	return plan, nil
}

// pinnedBinding says why a binding's pinned environment versions are not the
// ones the project holds now, or nothing: a suite of connected tests keeps
// the exact versions it was saved with and never follows a later one.
func (c *loadedCatalog) pinnedBinding(binding SuiteBinding) string {
	for _, ref := range []*ItemRef{&binding.Target, binding.Server} {
		if ref == nil {
			continue
		}
		index := c.document.Find(ref.ID)
		if index < 0 || c.document.Items[index].Kind != string(EnvironmentItem) || c.removed(c.document.Items[index]) {
			return "the project holds no such environment"
		}
		if current := c.document.Items[index].RevisionLabel(); ref.Revision != current {
			return c.read(c.document.Items[index]).Name + " is now version " + current + "; this suite pins version " + ref.Revision + ". Choose it again to use its current version"
		}
	}
	return ""
}

// reviewCompiled is the expectation review of one compiled lifecycle.
func reviewCompiled(compiled *connectedCompiled) (expectation.ConnectedReview, error) {
	folder, err := os.MkdirTemp("", "readmit-connected-review-")
	if err != nil {
		return expectation.ConnectedReview{}, err
	}
	defer os.RemoveAll(folder)
	plan := filepath.Join(folder, "plan")
	if err := compiled.plan.Write(context.Background(), plan); err != nil {
		return expectation.ConnectedReview{}, err
	}
	return expectation.ReviewConnected(plan)
}

// approveCompiled approves one compiled lifecycle's reviewed expectations,
// continuing the release parent holds, if any, and answers its exact bytes.
func approveCompiled(compiled *connectedCompiled, parent []byte, reviewed, approver, rationale string) ([]byte, error) {
	folder, err := os.MkdirTemp("", "readmit-connected-approval-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(folder)
	plan := filepath.Join(folder, "plan")
	if err := compiled.plan.Write(context.Background(), plan); err != nil {
		return nil, err
	}
	previous := ""
	if parent != nil {
		previous = filepath.Join(folder, "parent.json")
		if err := os.WriteFile(previous, parent, 0o600); err != nil {
			return nil, err
		}
	}
	output := filepath.Join(folder, "release.json")
	if _, err := expectation.ApproveConnected(plan, previous, reviewed, approver, rationale, output); err != nil {
		return nil, err
	}
	return os.ReadFile(output)
}

// connectedDocument lays a planned suite out as the readmit-suite/v2
// document the customer runner executes, pinning each job's release, and
// answers the files it names.
func (c *loadedCatalog) connectedDocument(draft SuiteDraft, plan *connectedSuitePlan, releases [][]byte) (suite.ConnectedDocument, *connectedSuiteFiles, error) {
	document := suite.ConnectedDocument{Schema: suite.ConnectedSchema, ID: draft.ID, Owner: cmpOr(draft.Owner, unassignedOwner), Tags: orEmpty(slices.Clone(draft.Tags)), Parallelism: max(draft.Concurrency, 1), Tests: []suite.ConnectedTest{}, Environments: []suite.ConnectedEnvironment{}}
	out := &connectedSuiteFiles{plans: map[string]*connectedtest.FlowPlan{}, files: map[string][]byte{}}
	for j, job := range plan.jobs {
		release, err := expectation.DecodeConnected(releases[j])
		if err != nil {
			return document, nil, err
		}
		name := path.Join(connectedFolder, "releases", release.Identity()+".json")
		out.files[name] = releases[j]
		document.Tests = append(document.Tests, suite.ConnectedTest{ID: job.id, Revision: plan.reviews[j].Revision, Definition: plan.reviews[j].Definition, Release: filepath.ToSlash(name), ReleaseIdentity: release.Identity(), After: orEmpty(job.after), State: "enabled"})
	}
	for n, environment := range draft.Environments {
		compiled := suite.ConnectedEnvironment{ID: environment.ID, Bindings: []suite.ConnectedBinding{}}
		for j, job := range plan.jobs {
			lifecycle := plan.compiled[n][j]
			name := path.Join(connectedFolder, "plans", lifecycle.plan.Identity())
			out.plans[name] = lifecycle.plan
			config, err := c.layoutSelection(lifecycle, out)
			if err != nil {
				return document, nil, err
			}
			compiled.Bindings = append(compiled.Bindings, suite.ConnectedBinding{Test: job.id, Plan: name, PlanIdentity: lifecycle.plan.Identity(), Config: config})
		}
		document.Environments = append(document.Environments, compiled)
	}
	raw, err := json.Marshal(document, json.Deterministic(true))
	if err == nil {
		_, err = suite.DecodeConnected(raw)
	}
	return document, out, err
}

// layoutSelection lays a compiled lifecycle's runtime selection out under the
// project's connected folder, named by its content, and answers the path of
// its config.json. Files of the project it reads in place are named from its
// folder, which is at the same depth whatever its name.
func (c *loadedCatalog) layoutSelection(compiled *connectedCompiled, out *connectedSuiteFiles) (string, error) {
	placeholder := filepath.Join(c.root, connectedFolder, "selections", "selection")
	place := func(target string) string {
		if relative, err := filepath.Rel(placeholder, target); err == nil {
			if inside, err := filepath.Rel(c.root, target); err == nil && filepath.IsLocal(inside) {
				return filepath.ToSlash(relative)
			}
		}
		return target
	}
	selected, err := compiled.selection(place)
	if err != nil {
		return "", err
	}
	config, err := encodeMember(selected)
	if err != nil {
		return "", err
	}
	files := map[string][]byte{"config.json": config}
	for name, data := range compiled.files {
		files[name] = data
	}
	identity := []byte{}
	for _, name := range slices.Sorted(mapKeys(files)) {
		identity = append(append(identity, name...), 0)
		identity = append(append(identity, dataset.Digest(files[name])...), 0)
	}
	folder := path.Join(connectedFolder, "selections", dataset.Digest(identity))
	for name, data := range files {
		out.files[path.Join(folder, name)] = data
	}
	out.dirs = append(out.dirs, path.Join(folder, "grants"))
	return path.Join(folder, "config.json"), nil
}

// write places every file under the project, once: a file the project
// already holds under its content's name is the same file. It answers the
// function that removes exactly what this call created, which a caller runs
// when what names the files is not recorded.
func (f *connectedSuiteFiles) write(ctx context.Context, root string) (func(), error) {
	created := []string{}
	remove := func() {
		for _, name := range slices.Backward(created) {
			os.RemoveAll(name)
		}
	}
	// mkdir creates a folder and records each one it created, outermost first.
	mkdir := func(dir string) error {
		missing := []string{}
		for at := dir; at != root && at != filepath.Dir(at); at = filepath.Dir(at) {
			if _, err := os.Lstat(at); err == nil {
				break
			}
			missing = append(missing, at)
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		slices.Reverse(missing)
		created = append(created, missing...)
		return nil
	}
	fail := func(err error) (func(), error) {
		remove()
		return func() {}, err
	}
	for _, name := range slices.Sorted(mapKeys(f.plans)) {
		target := filepath.Join(root, filepath.FromSlash(name))
		if _, err := connectedtest.OpenFlowPlan(target); err == nil {
			continue
		}
		if err := mkdir(filepath.Dir(target)); err != nil {
			return fail(err)
		}
		staging, err := os.MkdirTemp(filepath.Dir(target), ".staging-")
		if err != nil {
			return fail(err)
		}
		written := filepath.Join(staging, "plan")
		err = f.plans[name].Write(ctx, written)
		if err == nil {
			err = os.Rename(written, target)
		}
		os.RemoveAll(staging)
		if err != nil {
			if _, opened := connectedtest.OpenFlowPlan(target); opened == nil {
				continue
			}
			return fail(err)
		}
		created = append(created, target)
	}
	for _, name := range slices.Sorted(mapKeys(f.files)) {
		target := filepath.Join(root, filepath.FromSlash(name))
		if held, err := os.ReadFile(target); err == nil && string(held) == string(f.files[name]) {
			continue
		}
		if err := mkdir(filepath.Dir(target)); err != nil {
			return fail(err)
		}
		if err := (artifactdir.Document{MaxBytes: catalog.MaxMemberBytes}).Create(target, f.files[name]); err != nil {
			return fail(err)
		}
		created = append(created, target)
	}
	for _, name := range f.dirs {
		if err := mkdir(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			return fail(err)
		}
	}
	return remove, nil
}

// validateAuthoredConnectedSuite validates a suite of saved connected tests
// and answers the one member a save publishes: its definition, with every
// binding pinned at the environment version it compiled against.
func validateAuthoredConnectedSuite(scope draftScope, draft ItemDraft) ([]catalog.Staged, ItemDraft, []FieldProblem) {
	problems := []FieldProblem{}
	if draft.Name == "" {
		problems = append(problems, FieldProblem{Field: "name", Problem: nameRule})
	}
	suiteDraft := normalizedSuite(*draft.Suite)
	suiteDraft.Connected = nil
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
	// A binding chosen in the editor names an environment's current version;
	// a pinned one is kept exactly as it was saved.
	for n := range suiteDraft.Environments {
		for m := range suiteDraft.Environments[n].Bindings {
			binding := &suiteDraft.Environments[n].Bindings[m]
			for _, ref := range []*ItemRef{&binding.Target, binding.Server} {
				if ref == nil || ref.Revision != "" {
					continue
				}
				if index := scope.loaded.document.Find(ref.ID); index >= 0 {
					ref.Revision = scope.loaded.document.Items[index].RevisionLabel()
				}
			}
		}
	}
	normalized := ItemDraft{Name: draft.Name, Suite: &suiteDraft}
	_, found := scope.loaded.planConnectedSuite(suiteDraft)
	problems = append(problems, found...)
	if len(problems) > 0 {
		return nil, normalized, problems
	}
	definition, err := encodeMember(suiteDefinition{Schema: AuthoredSuiteDefinitionSchema, Draft: suiteDraft})
	if err != nil || len(definition) > suite.MaxBytes {
		return nil, normalized, []FieldProblem{{Field: "suite", Problem: "the suite's definition cannot be encoded within its bound"}}
	}
	return []catalog.Staged{{Role: "definition", File: "definition.json", Data: definition}}, normalized, nil
}

// verifyAuthoredDefinition reads a v3 definition: the authored draft of a
// suite of connected tests, which holds no compiled document.
func verifyAuthoredDefinition(draft SuiteDraft) error {
	if draft.Connected != nil || len(draft.Tests) == 0 {
		return errors.New("invalid connected suite definition")
	}
	return nil
}
