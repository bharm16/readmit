// Projects (view 01), New project (view 15) and Cases (view 02) as components:
// ordering, the one-name creation sheet, missing rows kept with Locate, the
// exact status choices, transient search and filters with their chips, and
// the empty and filtered-empty states.
import { expect, test, vi } from "vitest";
import { useState } from "react";
import type { SortState } from "./DataTable";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CatalogItem, CaseStatus } from "./bindings";
import { listDate, NewProjectSheet, ProjectList, sortProjects, validProjectName } from "./Projects";
import { applyView, CASE_STATUSES, CaseFilterSheet, CaseList, NO_VIEW, sortCases } from "./Cases";

const project = (id: string, name: string, opened: string | null, availability: CatalogItem["availability"] = "available"): CatalogItem => ({
  ref: { kind: "project", id },
  name,
  created_at: null,
  updated_at: null,
  last_opened_at: opened,
  availability,
  ...(availability === "available" ? {} : { reason: "The project folder is not where it was." }),
  capabilities: [],
  summary: { project: { folder: `/p/${id}`, schema: "readmit-project/v2", cases: 0, interface_versions: [], tags: [], revisions: [] } },
});

const kase = (id: string, name: string, status: CaseStatus, owner: string, updated: string | null, tags: string[] = []): CatalogItem => ({
  ref: { kind: "case", id },
  name,
  created_at: null,
  updated_at: updated,
  last_opened_at: null,
  availability: "available",
  capabilities: [],
  summary: { case: { registered: true, entry: id, status, owner, tags, incidents: [], evidence: "verified" } },
});

test("projects list newest opened first, never-opened last, then by name", () => {
  const items = [project("c", "Beta", null), project("a", "Alpha", "2026-09-24T10:00:00Z"), project("b", "Gamma", "2026-09-26T10:00:00Z"), project("d", "Alpha", null)];
  expect(sortProjects(items).map((p) => p.ref.id)).toEqual(["b", "a", "d", "c"]);
});

test("dates read Today, Yesterday or the day, and unknown is a dash", () => {
  const now = new Date(2026, 8, 26, 15);
  expect(listDate(new Date(2026, 8, 26, 9).toISOString(), now)).toBe("Today");
  expect(listDate(new Date(2026, 8, 25, 23).toISOString(), now)).toBe("Yesterday");
  expect(listDate(null, now)).toBe("—");
});

test("a project name is 1–200 printable characters once trimmed", () => {
  expect(validProjectName("  Scheduling investigation ")).toBe(true);
  expect(validProjectName("Registrierung — Übergang")).toBe(true);
  expect(validProjectName("   ")).toBe(false);
  expect(validProjectName("a".repeat(201))).toBe(false);
  expect(validProjectName("bad\u0007name")).toBe(false);
});

test("a row opens its project; a missing one stays listed with its reason and Locate", async () => {
  const user = userEvent.setup();
  const open = vi.fn();
  const locate = vi.fn();
  const forget = vi.fn();
  render(
    <ProjectList
      projects={[project("a", "Scheduling investigation", "2026-09-26T10:00:00Z"), project("m", "Moved away", null, "missing")]}
      busy={false}
      onOpen={open}
      onLocate={locate}
      onSettings={() => undefined}
      onReveal={() => undefined}
      onForget={forget}
      onNew={() => undefined}
    />,
  );
  await user.click(screen.getByRole("row", { name: "Scheduling investigation" }));
  expect(open).toHaveBeenCalledTimes(1);
  const missing = screen.getByRole("row", { name: "Moved away" });
  expect(within(missing).getByText("The project folder is not where it was.")).toBeTruthy();
  await user.click(missing);
  expect(open).toHaveBeenCalledTimes(1);
  await user.click(within(missing).getByRole("button", { name: "Locate" }));
  expect(locate).toHaveBeenCalledTimes(1);
  // No Open button per row; the rarer actions are in its menu.
  expect(screen.queryByRole("button", { name: /^Open/ })).toBeNull();
  await user.click(screen.getByRole("button", { name: "More actions for Scheduling investigation" }));
  expect(screen.getAllByRole("menuitem").map((item) => item.textContent)).toEqual(["Project settings", expect.stringMatching(/^Show in (Finder|folder)$/), "Remove from recents"]);
  await user.click(screen.getByRole("menuitem", { name: "Remove from recents" }));
  expect(forget).toHaveBeenCalledTimes(1);
});

test("no projects yet offers New project", async () => {
  const user = userEvent.setup();
  const onNew = vi.fn();
  render(<ProjectList projects={[]} busy={false} onOpen={() => undefined} onLocate={() => undefined} onSettings={() => undefined} onReveal={() => undefined} onForget={() => undefined} onNew={onNew} />);
  expect(screen.getByText("No projects yet")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "New project" }));
  expect(onNew).toHaveBeenCalled();
});

test("New project asks only for a name and a location, and a refused create keeps both", async () => {
  const user = userEvent.setup();
  let location: string | null = null;
  const create = vi.fn(async () => ({ reason: "This folder cannot be written." }));
  const { rerender } = render(
    <NewProjectSheet open location={location} onChangeLocation={async () => undefined} onCreate={create} onClose={() => undefined} />,
  );
  expect(screen.getAllByRole("textbox")).toHaveLength(1);
  expect(screen.getByText("Choose location")).toBeTruthy();
  await user.type(screen.getByLabelText("Name"), "Scheduling investigation");
  expect((screen.getByRole("button", { name: "Create" }) as HTMLButtonElement).disabled).toBe(true);
  location = "/Users/someone/Projects";
  rerender(<NewProjectSheet open location={location} onChangeLocation={async () => undefined} onCreate={create} onClose={() => undefined} />);
  expect(screen.getByText("/Users/someone/Projects")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Create" }));
  expect(create).toHaveBeenCalledWith("Scheduling investigation");
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("Scheduling investigation");
});

test("case statuses are exactly Open, Investigating, Resolved and Closed", () => {
  expect(CASE_STATUSES.map((status) => status.label)).toEqual(["Open", "Investigating", "Resolved", "Closed"]);
});

const CASES = [
  kase("1", "Duplicate appointment after reschedule", "investigating", "Integration team", "2026-09-26T10:00:00Z", ["scheduling"]),
  kase("2", "Cancellation rejected", "open", "", "2026-09-24T10:00:00Z"),
  kase("3", "Never touched", "closed", "Integration team", null),
];

test("cases sort by Updated newest first with unknown last, and search and filters narrow without saving", () => {
  expect(sortCases(CASES, null).map((c) => c.ref.id)).toEqual(["1", "2", "3"]);
  expect(applyView(CASES, { ...NO_VIEW, query: "cancel" }).map((c) => c.ref.id)).toEqual(["2"]);
  expect(applyView(CASES, { ...NO_VIEW, statuses: ["closed", "open"] }).map((c) => c.ref.id)).toEqual(["2", "3"]);
  expect(applyView(CASES, { ...NO_VIEW, owner: "Integration team", tags: ["scheduling"] }).map((c) => c.ref.id)).toEqual(["1"]);
});

function Cases({ view, onView = () => undefined, cases = CASES }: { view: typeof NO_VIEW; onView?: (v: typeof NO_VIEW) => void; cases?: CatalogItem[] }) {
  const [sort, setSort] = useState<SortState | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  return (
    <CaseList
      cases={cases}
      view={view}
      onView={onView}
      selected={selected}
      onSelect={setSelected}
      onOpen={() => undefined}
      onAction={() => undefined}
      onRetry={() => undefined}
      onLocate={() => undefined}
      onImport={() => undefined}
      sort={sort}
      onSort={setSort}
      busy={false}
    />
  );
}

test("applied filters show as chips, and a filtered empty list offers Clear filters", async () => {
  const user = userEvent.setup();
  const onView = vi.fn();
  render(<Cases view={{ ...NO_VIEW, query: "zzz", statuses: ["resolved"] }} onView={onView} />);
  expect(screen.getByText("No matching cases")).toBeTruthy();
  const chips = within(screen.getByRole("group", { name: "Applied filters" }));
  await user.click(chips.getByRole("button", { name: "Remove Resolved" }));
  expect(onView).toHaveBeenLastCalledWith({ ...NO_VIEW, query: "zzz", statuses: [] });
  await user.click(screen.getAllByRole("button", { name: "Clear filters" })[0]!);
  expect(onView).toHaveBeenLastCalledWith(NO_VIEW);
});

test("no cases yet offers Import, and the columns are Case, Status, Owner and Updated", () => {
  const { rerender } = render(<Cases view={NO_VIEW} cases={[]} />);
  expect(screen.getByText("No cases yet")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Import" })).toBeTruthy();
  rerender(<Cases view={NO_VIEW} />);
  expect(screen.getAllByRole("columnheader").map((header) => header.textContent).filter(Boolean)).toEqual(["Case", "Status", "Owner", "Updated"]);
  expect(within(screen.getByRole("row", { name: "Cancellation rejected" })).getByText("Unassigned")).toBeTruthy();
});

test("the filter sheet applies only when asked", async () => {
  const user = userEvent.setup();
  const apply = vi.fn();
  render(<CaseFilterSheet open view={NO_VIEW} owners={["Integration team"]} tags={["scheduling"]} onApply={apply} onClose={() => undefined} />);
  await user.click(screen.getByRole("checkbox", { name: "Investigating" }));
  await user.selectOptions(screen.getByLabelText("Owner"), "Integration team");
  expect(apply).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Apply" }));
  expect(apply).toHaveBeenCalledWith({ ...NO_VIEW, statuses: ["investigating"], owner: "Integration team" });
});

test("a column sorts ascending first and then descending", async () => {
  const user = userEvent.setup();
  render(<Cases view={NO_VIEW} />);
  const names = () => screen.getAllByRole("row").filter((row) => row.hasAttribute("data-row-id")).map((row) => row.getAttribute("aria-label"));
  await user.click(screen.getByRole("button", { name: "Case" }));
  expect(names()).toEqual(["Cancellation rejected", "Duplicate appointment after reschedule", "Never touched"]);
  await user.click(screen.getByRole("button", { name: /Case/ }));
  expect(names()).toEqual(["Never touched", "Duplicate appointment after reschedule", "Cancellation rejected"]);
});

test("a thousand cases with long names stay one keyboard-operable table", async () => {
  const user = userEvent.setup();
  // The list's measured height, which a real layout gives it and jsdom does not.
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockImplementation(function (this: HTMLElement) {
    return this.classList.contains("table-view") ? 440 : 0;
  });
  const many = Array.from({ length: 1000 }, (_, i) =>
    kase(String(i).padStart(4, "0"), `${"Very long case name that keeps going past the column ".repeat(3)}${i}`, "open", "", null),
  );
  render(<Cases view={NO_VIEW} cases={many} />);
  const rows = () => screen.getAllByRole("row").filter((row) => row.hasAttribute("data-row-id"));
  expect(rows().length).toBeLessThan(100);
  rows()[0]!.focus();
  await user.keyboard("{End}");
  expect(document.activeElement?.getAttribute("aria-label")).toMatch(/ 999$/);
  vi.restoreAllMocks();
});
