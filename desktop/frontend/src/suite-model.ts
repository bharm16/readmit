// A suite draft as its screens change it: new drafts from chosen tests,
// identifiers the person never types, the dependency and reference checks
// shown before a save, and exclusion times in a named zone. The facade
// validates the whole draft again on Save; these checks only keep an error at
// the row it is about while the person edits.
import type { CatalogItem, FieldProblem, ItemRef, SuiteDraft, SuiteTestDraft, SuiteTestVersion, TestRunnerValue } from "./bindings";

/** An empty suite: one concurrent job and nothing in it yet. */
export function emptySuite(): SuiteDraft {
  return { tags: [], concurrency: 1, tests: [], datasets: [], environments: [], requirements: [], exclusions: [] };
}

/** An identifier from a name: lowercase letters, digits and hyphens, starting
 * with a letter, unique among those taken. */
export function identifier(name: string, taken: Iterable<string>, fallback: string): string {
  const used = new Set(taken);
  const base =
    name
      .toLowerCase()
      .normalize("NFKD")
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^[^a-z]+/, "")
      .replace(/-+$/, "")
      .slice(0, 56) || fallback;
  if (!used.has(base)) return base;
  for (let n = 2; ; n++) {
    const next = `${base}-${n}`;
    if (!used.has(next)) return next;
  }
}

/** One saved test as a suite adds it: its current version, the dataset of
 * its own case and the parameter its outcome needs. */
export type AddedTest = {
  item: CatalogItem;
  version: SuiteTestVersion;
  caseItem: CatalogItem | null;
  environment: CatalogItem | null;
  observation: ItemRef | null;
};

/** The parameter a test is bound through: acknowledgement tests share one,
 * and record tests share one per observation they read. */
function parameterOf(added: AddedTest, draft: SuiteDraft): string {
  if (!added.version.ledger) return "acknowledgements";
  const same = draft.tests.find((test) => {
    const binding = draft.environments.flatMap((env) => env.bindings).find((b) => b.parameter === test.parameter);
    return binding?.observation?.id === added.observation?.id && binding?.observation !== undefined;
  });
  if (same) return same.parameter;
  return identifier("records", draft.tests.map((test) => test.parameter), "records");
}

/** Adds saved tests to a draft: each pinned to the version chosen, over a
 * dataset holding its own case (shared by tests of the same case), and bound
 * in each environment its saved setup names. Existing tests, datasets and
 * bindings are never changed. */
export function addTests(draft: SuiteDraft, added: AddedTest[]): SuiteDraft {
  let next: SuiteDraft = { ...draft, tests: [...draft.tests], datasets: [...draft.datasets], environments: draft.environments.map((env) => ({ ...env, bindings: [...env.bindings] })) };
  for (const entry of added) {
    let dataset = entry.caseItem ? next.datasets.find((set) => set.rows.length === 1 && set.rows[0]!.case.id === entry.caseItem!.ref.id) : undefined;
    if (!dataset) {
      const name = entry.caseItem?.name ?? entry.item.name;
      dataset = {
        id: identifier(name, next.datasets.map((set) => set.id), "dataset"),
        name,
        rows: entry.caseItem ? [{ id: identifier(entry.caseItem.name, [], "row"), case: { kind: "case", id: entry.caseItem.ref.id } }] : [],
      };
      next.datasets.push(dataset);
    }
    const parameter = parameterOf(entry, next);
    const test: SuiteTestDraft = {
      id: identifier(entry.item.name, next.tests.map((t) => t.id), "test"),
      test: entry.version.ref,
      dataset: dataset.id,
      parameter,
      after: [],
      isolation: "shared",
      sequence: [...entry.version.sequence],
      tags: [],
    };
    next.tests.push(test);
    if (entry.environment) {
      let env = next.environments.find((held) => held.bindings.some((b) => b.target.id === entry.environment!.ref.id));
      if (!env) {
        env = { id: identifier(entry.environment.name, next.environments.map((e) => e.id), "environment"), name: entry.environment.name, site: entry.environment.name, bindings: [] };
        next.environments.push(env);
      }
      if (!env.bindings.some((b) => b.parameter === parameter)) {
        env.bindings.push({ parameter, target: { kind: "environment", id: entry.environment.ref.id }, ...(entry.version.ledger && entry.observation ? { observation: entry.observation } : {}) });
      }
    }
  }
  next = { ...next };
  return next;
}

/** The suite tests a test's dependencies reach back to itself through, or
 * none. */
export function cycleOf(draft: SuiteDraft, start: string): string[] {
  const after = new Map(draft.tests.map((test) => [test.id, test.after]));
  const path: string[] = [];
  const seen = new Set<string>();
  const walk = (id: string): boolean => {
    if (id === start && path.length > 0) return true;
    if (seen.has(id)) return false;
    seen.add(id);
    path.push(id);
    for (const next of after.get(id) ?? []) {
      if (walk(next)) return true;
    }
    path.pop();
    return false;
  };
  return walk(start) ? path : [];
}

/** The problems a draft shows before it is saved: references to a removed
 * test, dependency cycles, a test's dataset or parameter missing, partial
 * requirements and exclusions. Each is at the member it is about, with the
 * same field names the facade reports. */
export function draftProblems(draft: SuiteDraft, name: string, testName: (id: string) => string): FieldProblem[] {
  const problems: FieldProblem[] = [];
  const ids = new Set(draft.tests.map((test) => test.id));
  if (name.trim() === "") problems.push({ field: "name", problem: "Enter a name." });
  draft.tests.forEach((test, index) => {
    for (const dependency of test.after) {
      if (!ids.has(dependency)) problems.push({ field: `tests.${index}.after`, problem: "Depends on a test this suite no longer has." });
      else if (dependency === test.id) problems.push({ field: `tests.${index}.after`, problem: "A test cannot depend on itself." });
    }
    if (cycleOf(draft, test.id).length > 0) problems.push({ field: `tests.${index}.after`, problem: "These dependencies form a cycle." });
    if (!draft.datasets.some((set) => set.id === test.dataset)) problems.push({ field: `tests.${index}.dataset`, problem: "Choose a dataset." });
    if (test.parameter.trim() === "") problems.push({ field: `tests.${index}.parameter`, problem: "Choose an environment parameter." });
    if (!test.test.id) problems.push({ field: `tests.${index}.test`, problem: `${test.source ? test.source : testName(test.id)} is not a saved test of this project.` });
  });
  draft.requirements.forEach((requirement, index) => {
    if (requirement.name.trim() === "") problems.push({ field: `requirements.${index}.name`, problem: "Enter a name." });
    if (requirement.tests.some((id) => !ids.has(id))) problems.push({ field: `requirements.${index}.tests`, problem: "Names a test this suite no longer has." });
  });
  draft.exclusions.forEach((exclusion, index) => {
    if (!ids.has(exclusion.test)) problems.push({ field: `exclusions.${index}.test`, problem: "Choose a test of this suite." });
    if (exclusion.reason.trim() === "") problems.push({ field: `exclusions.${index}.reason`, problem: "Enter a reason." });
    if (exclusion.until.trim() === "") problems.push({ field: `exclusions.${index}.until`, problem: "Choose when it ends." });
  });
  return problems;
}

/** The problems at one field, and at every field below it. */
export function problemsUnder(problems: FieldProblem[], field: string): string[] {
  return problems.filter((problem) => problem.field === field || problem.field.startsWith(`${field}.`)).map((problem) => problem.problem);
}

/** The tests, datasets, requirements and exclusions that name one suite test. */
export function referencesTo(draft: SuiteDraft, id: string, testName: (id: string) => string): string[] {
  const names: string[] = [];
  for (const test of draft.tests) if (test.after.includes(id)) names.push(testName(test.id));
  for (const requirement of draft.requirements) if (requirement.tests.includes(id)) names.push(requirement.name || "A requirement");
  for (const exclusion of draft.exclusions) if (exclusion.test === id) names.push(`Exclusion of ${testName(id)}`);
  return names;
}

/** The checks a dataset's rows can override: every check of the tests that
 * use it, by identity, and whether every one of those tests declares it. */
export function datasetChecks(draft: SuiteDraft, datasetId: string, versions: SuiteTestVersion[]) {
  const users = draft.tests.filter((test) => test.dataset === datasetId);
  const versionOf = (test: SuiteTestDraft) => versions.find((v) => v.ref.id === test.test.id && v.ref.revision === test.test.revision);
  const checks = new Map<string, { check: SuiteTestVersion["checks"][number]; messages: SuiteTestVersion["messages"]; declaredBy: number }>();
  for (const test of users) {
    const version = versionOf(test);
    for (const check of version?.checks ?? []) {
      const held = checks.get(check.id);
      if (held) held.declaredBy += 1;
      else checks.set(check.id, { check, messages: version?.messages ?? [], declaredBy: 1 });
    }
  }
  return { users: users.length, checks: [...checks.values()] };
}

/** Whether an override's value is the shape its check's operator takes. */
export function valueFits(operator: string, value: TestRunnerValue): boolean {
  const members = Object.keys(value).filter((key) => value[key as keyof TestRunnerValue] !== undefined);
  switch (operator) {
    case "ledger_count":
      return members.length === 1 && typeof value.count === "number";
    case "ledger_equals":
      return members.length === 1 && Array.isArray(value.records);
    case "ack_field_equals":
      return members.length === 1 && value.field !== undefined;
  }
  return false;
}

/** The value part of a check: what a data row may override. */
export function valueOf(check: { count?: number; records?: TestRunnerValue["records"]; field?: TestRunnerValue["field"] }): TestRunnerValue {
  if (check.field !== undefined) return { field: check.field };
  if (check.records !== undefined) return { records: check.records };
  return { count: check.count ?? 0 };
}

/** The UTC instant a wall-clock time names in a zone, as RFC 3339 to the
 * second: the form an exclusion is saved in. Null for a time that is not one. */
export function utcOf(local: string, zone: string): string | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(local);
  if (!match) return null;
  const [, y, mo, d, h, mi, s] = match;
  const wall = Date.UTC(Number(y), Number(mo) - 1, Number(d), Number(h), Number(mi), Number(s ?? "0"));
  // The zone's offset at that instant, found by formatting it there; twice,
  // so a wall time near a transition settles on the offset in force then.
  let guess = wall;
  for (let i = 0; i < 2; i++) {
    const offset = zoneOffset(guess, zone);
    if (offset === null) return null;
    guess = wall - offset;
  }
  return new Date(guess).toISOString().replace(/\.\d{3}Z$/, "Z");
}

function zoneOffset(instant: number, zone: string): number | null {
  try {
    const parts = new Intl.DateTimeFormat("en-US", { timeZone: zone, hourCycle: "h23", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit" }).formatToParts(new Date(instant));
    const part = (type: string) => Number(parts.find((p) => p.type === type)?.value);
    const asUTC = Date.UTC(part("year"), part("month") - 1, part("day"), part("hour") % 24, part("minute"), part("second"));
    return asUTC - Math.floor(instant / 1000) * 1000;
  } catch {
    return null;
  }
}

/** A saved UTC instant as the wall-clock time it is in a zone, for editing. */
export function localOf(utc: string, zone: string): string {
  const instant = Date.parse(utc);
  if (Number.isNaN(instant)) return "";
  const offset = zoneOffset(instant, zone);
  if (offset === null) return "";
  return new Date(instant + offset).toISOString().slice(0, 16);
}

/** The zone this computer is in. */
export function localZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
}

/** The zones a person can choose, this computer's first. */
export function zones(): string[] {
  const all = typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : ["UTC"];
  const here = localZone();
  return [here, ...all.filter((zone) => zone !== here && zone !== "UTC"), ...(here === "UTC" ? [] : ["UTC"])];
}
