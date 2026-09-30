// The redesign's delivery registries: docs/product-redesign/implementation-map.json
// names one owner, route, source, fixtures and acceptance tests for each of the
// 46 numbered views, and catalog-migration.json reconciles each of the 785
// captures of the supplied catalog to what replaced, retained or removed it.
// Every reference either file makes is checked against the tree: a route the
// window has, a source that exists, a fixture the kit exports and a test by its
// exact name in the file it names.
import { readFileSync } from "node:fs";
import { expect, test } from "vitest";
import { CHILD_OF } from "./routes";

const root = `${process.cwd()}/../..`;
const read = (path: string) => readFileSync(`${root}/${path}`, "utf8");
const exists = (path: string) => {
  try {
    read(path);
    return true;
  } catch {
    return false;
  }
};

type Test = { file: string; test: string };
type Route = { destination: string; view?: string; surface?: string };
type View = { view: string; title: string; owner: { issue: string; program: string }; route: Route; sources: string[]; fixtures: string[]; acceptance: Test[]; captures: string[] };
type Shared = { component: string; owner: { issue: string; program: string }; sources: string[]; acceptance: Test[] };
type Row = {
  capture: string;
  file: string;
  title: string;
  context: string;
  kind: "page" | "component";
  owner: string;
  owner_view?: string;
  owner_issue: string;
  disposition: "replaced" | "retained" | "removed-presentation";
  replacement: { view?: string; component?: string; route?: Route; source?: string; name?: string };
  preserved_capability: string;
  covered_state: string;
  test: Test;
  tests?: Test[];
  production_sources?: string[];
};

const map = JSON.parse(read("docs/product-redesign/implementation-map.json")) as { views: View[]; shared: Shared[] };
const migration = JSON.parse(read("docs/product-redesign/catalog-migration.json")) as { records: Row[] };

// The redesign's issues are RD01–RD22, #546–#567.
const OWNERS = /^bharm16\/readmit#(54[6-9]|55\d|56[0-7])$/;
const PLACES = new Set<string>(["home", "cases", "tests", "runs", "environments", "reports", "tools", "settings", "help", ...Object.keys(CHILD_OF)]);
const FIXTURES = new Set([...read("desktop/frontend/src/testkit/fixtures.ts").matchAll(/^export (?:async )?(?:const|function) ([A-Za-z0-9_]+)/gm)].map((match) => match[1]!));
const PENDING = /\b(TBD|TODO|unassigned|review[- ]later|to be decided)\b/i;

const named = new Map<string, string>();
function hasTest({ file, test: name }: Test): boolean {
  if (!named.has(file)) named.set(file, exists(file) ? read(file) : "");
  const quoted = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`\\b(?:test|it)(?:\\.\\w+\\([^)]*\\))?\\(\\s*["\`]${quoted}["\`]`).test(named.get(file)!);
}

test("every numbered view 01–46 has one owner, a route the window has, its sources, fixtures and named acceptance tests", () => {
  expect(map.views.map((view) => view.view)).toEqual(Array.from({ length: 46 }, (_, at) => String(at + 1).padStart(2, "0")));
  for (const view of map.views) {
    expect(view.title.trim(), view.view).not.toBe("");
    expect(view.owner.issue, view.view).toMatch(OWNERS);
    expect(PLACES.has(view.route.destination), `${view.view} ${view.route.destination}`).toBe(true);
    expect(view.sources.length, view.view).toBeGreaterThan(0);
    for (const source of view.sources) expect(exists(source), source).toBe(true);
    for (const fixture of view.fixtures) expect(FIXTURES.has(fixture), `${view.view} ${fixture}`).toBe(true);
    expect(view.acceptance.length, view.view).toBeGreaterThan(0);
    for (const reference of view.acceptance) expect(hasTest(reference), `${view.view}: ${reference.file} › ${reference.test}`).toBe(true);
    expect(PENDING.test(JSON.stringify(view)), view.view).toBe(false);
  }
});

test("shared components are recorded apart from the views, each with its owner, sources and tests", () => {
  const components = map.shared.map((shared) => shared.component);
  expect(new Set(components).size).toBe(components.length);
  for (const shared of map.shared) {
    expect(shared.owner.issue).toMatch(OWNERS);
    for (const source of shared.sources) expect(exists(source), source).toBe(true);
    for (const reference of shared.acceptance) expect(hasTest(reference), `${shared.component}: ${reference.test}`).toBe(true);
  }
});

test("each of the 785 catalog captures is reconciled once to its owner, disposition, replacement, capability, state and test", () => {
  const views = new Map(map.views.map((view) => [view.view, view]));
  const components = new Set(map.shared.map((shared) => shared.component));
  expect(migration.records.map((row) => row.capture)).toEqual(Array.from({ length: 785 }, (_, at) => String(at + 1).padStart(4, "0")));
  for (const row of migration.records) {
    const where = `${row.capture} ${row.title}`;
    expect(row.file.startsWith(`${row.capture}-`) && row.file.endsWith(".png"), where).toBe(true);
    expect(["page", "component"]).toContain(row.kind);
    expect(row.owner.trim(), where).not.toBe("");
    expect(row.owner_issue, where).toMatch(OWNERS);
    expect(["replaced", "retained", "removed-presentation"]).toContain(row.disposition);
    const { view, component, source, name } = row.replacement;
    expect(view !== undefined || component !== undefined, where).toBe(true);
    if (view !== undefined) {
      expect(views.has(view), where).toBe(true);
      // Ownership of a numbered design view does not collapse its other
      // capabilities onto that view's landing route (for example case
      // comparison and run comparison both belong to comparison's owner).
      expect(row.replacement.route && PLACES.has(row.replacement.route.destination), where).toBe(true);
      expect(row.owner_view, where).toBe(view);
    }
    if (component !== undefined) expect(components.has(component), where).toBe(true);
    // A retained component is still where the catalog captured it.
    if (row.disposition === "retained") {
      expect(source && name, where).toBeTruthy();
      expect(read(source!), where).toMatch(new RegExp(`(function|const|class) ${name}\\b`));
    }
    expect(row.preserved_capability.trim(), where).not.toBe("");
    expect(row.covered_state.trim(), where).not.toBe("");
    expect(hasTest(row.test), `${where}: ${row.test.file} › ${row.test.test}`).toBe(true);
    for (const reference of row.tests ?? []) expect(hasTest(reference), `${where}: ${reference.file} › ${reference.test}`).toBe(true);
    for (const source of row.production_sources ?? []) expect(exists(source), `${where}: ${source}`).toBe(true);
    // No row is parked: nothing pending, and no generic Advanced or
    // command-line-only replacement.
    const replacement = JSON.stringify([row.replacement, row.preserved_capability]);
    expect(PENDING.test(JSON.stringify(row)), where).toBe(false);
    expect(/\bAdvanced\b|command line only|CLI[- ]only/i.test(replacement), where).toBe(false);
  }
});
