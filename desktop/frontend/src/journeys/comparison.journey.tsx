// Comparing two cases, and reading the comparison under a named normalization
// policy, over the real facade.
//
// A laboratory interface was upgraded. The person imported what the analyser
// sent before and after the upgrade as two cases of one project. After the
// upgrade every result carries a later message time, the white-cell count
// drifts by a hundredth or two — a drift the lab tolerates — every tenth
// result reads markedly higher, every twenty-fifth is still pending, and
// every fiftieth names the patient differently. The first result of the old
// feed is not in the new one, and the new feed holds one result the old one
// never sent.
//
// Compare is refused until the person names the field that identifies one
// result, and then lists the differences exactly as `readmit diff` reports
// them. A case whose stored bytes change after it was imported is refused, as
// the command line refuses it. Under a policy the person names — the time
// within the hour and the tolerated drift — the window hides what the policy
// ignores and keeps everything else, exactly as `readmit normalize` reads the
// saved policy; a rule the policy reader refuses is refused before anything is
// saved, and the policy extended with a rule for the name is what the command
// line then reads.
//
// Every message and value here is synthetic.
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { ROW_REM } from "../geometry";
import { rootFontSize } from "../measure";
import { Journey, press } from "../testkit/journey";
import { goTo, openCaseFlow, openListedCase, page } from "../testkit/navigation";
import { filesUnder } from "./probes.js";
import { declareMllpImport, finishImport, framed, licensedProject } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  vi.restoreAllMocks();
  await journey.dispose();
});

/** The list's height in pixels, as a window of ordinary size lays it out. */
const HEIGHT = 440;

/** One analyser result, MLLP-framed. Every value is synthetic. */
function labResult(control: number, time: string, count: string, family: string): string {
  return framed(
    `MSH|^~\\&|ANALYSER|SYNTHETIC|READMIT|LAB|${time}||ORU^R01|CMP-${control}|P|2.5.1\r` +
      `PID|1||SYNTH-${control}^^^READMIT||${family}^ONLY\r` +
      `OBX|1|NM|WBC||${count}\r`,
  );
}

/** What changed after the upgrade, result by result: every twenty-fifth count
 * is still pending, every other tenth reads markedly higher, and every
 * fiftieth patient is named differently. Every other count drifts by 0.02. */
const pending = (result: number) => result % 25 === 0;
const higher = (result: number) => result % 10 === 0 && !pending(result);
const renamed = (result: number) => result % 50 === 0;

/** Results 1 to 250 before the upgrade; 2 to 251 after it. */
const BEFORE = Array.from({ length: 250 }, (_, index) => labResult(index + 1, "20260101120000", "7.40", "SYNTHETIC")).join("");
const AFTER = Array.from({ length: 250 }, (_, index) => {
  const result = index + 2;
  const count = pending(result) ? "PENDING" : higher(result) ? "7.90" : "7.42";
  return labResult(result, "20260101120512", count, renamed(result) ? "RENAMED" : "SYNTHETIC");
}).join("");

/** Results 2 to 250, which both feeds hold. */
const SHARED = Array.from({ length: 249 }, (_, index) => index + 2);
const PAIRED = SHARED.length;
const PENDING = SHARED.filter(pending).length;
const HIGHER = SHARED.filter(higher).length;
const RENAMED = SHARED.filter(renamed).length;
/** Each paired result differs in its message time and its count, and a
 * renamed one in the patient's name too. */
const DIFFERENCES = PAIRED * 2 + RENAMED;
/** What a policy of the time within the hour and the count within a
 * twentieth hides: every time, and every count that only drifted. */
const SUPPRESSED = PAIRED + (PAIRED - HIGHER - PENDING);

const BEFORE_CASE = "Before upgrade";
const AFTER_CASE = "After upgrade";
const POLICY = "Analyser drift";
const MISMATCHED =
  "these collections are not copies of one another; name the fields that identify one record, such as MSH-10, to align them";
const TAMPERED = "bundle is incomplete or its identity does not match contents";
const UNSIGNED = "a numeric tolerance is an unsigned decimal distance";
const NUMBER_RULE = "a number rule compares within one tolerance of zero or more";

interface Reference {
  occurrence: string;
}

interface DiffReport {
  alignment: string;
  keys: string[];
  summary: Record<string, number>;
  pairs: { left: Reference; right: Reference; fields: { selector: string }[] }[];
  missing: Reference[];
  inserted: Reference[];
}

interface RuleReport {
  id: string;
  compared: number;
  suppressed: number;
  retained: number;
  undecided: number;
}

interface NormalizationReport {
  summary: Record<string, number>;
  rules: RuleReport[];
  differences: { left_occurrence: string; right_occurrence: string; selector: string; outcome: string }[];
}

interface Compared {
  state: string;
  reason?: string;
  comparison?: {
    summary: Record<string, number>;
    keys: string[];
    alignment: string;
    rules: RuleReport[];
    suppressed: number;
    total: number;
    rows: { kind: string; earlier?: { id: string }; later?: { id: string }; field?: string; outcome?: string }[];
  };
}

/** Imports both feeds into one licensed project as two cases, and returns
 * the project and each case's entry. */
async function labProject(user: UserEvent): Promise<{ project: string; before: string; after: string }> {
  journey.writeFile("exports/before-upgrade.mllp", BEFORE);
  journey.writeFile("exports/after-upgrade.mllp", AFTER);
  const project = await licensedProject(journey, user);
  await declareMllpImport(user, journey, "exports/before-upgrade.mllp", BEFORE_CASE);
  const before = await finishImport(user, journey);
  await casesList(user);
  await declareMllpImport(user, journey, "exports/after-upgrade.mllp", AFTER_CASE);
  const after = await finishImport(user, journey);
  return { project, before, after };
}

/** Back from the open case to the project's Cases, where Import is. */
async function casesList(user: UserEvent): Promise<void> {
  await goTo(user, "Cases");
  for (let step = 0; step < 3 && !screen.queryByRole("table", { name: "Cases" }); step++) {
    const back = page().queryAllByRole("button", { name: /^Back to / })[0];
    if (!back) break;
    await user.click(back);
  }
  await screen.findByRole("table", { name: "Cases" });
}

/** Chooses a field path in a field picker: listed, or typed as another. */
async function pickField(user: UserEvent, scope: ReturnType<typeof within>, label: string, selector: string): Promise<void> {
  const picker = scope.getByRole("combobox", { name: label }) as HTMLSelectElement;
  await waitFor(() => expect(picker.disabled).toBe(false));
  if ([...picker.options].some((option) => option.value === selector)) {
    await user.selectOptions(picker, selector);
    return;
  }
  await user.selectOptions(picker, "\u0000other");
  await user.type(scope.getByRole("textbox", { name: `${label} path` }), selector);
}

/** The comparison the window was last answered, once every read it asked
 * after the first `from` has settled, a busy read asked again included. */
async function answered(from: number): Promise<Compared> {
  await waitFor(() => expect(journey.callsTo("CompareCases").length).toBeGreaterThan(from));
  await journey.settled();
  return journey.callsTo("CompareCases").at(-1)!.result as Compared;
}

/** Opens Compare from the case imported before the upgrade, with the one
 * after it, and returns the comparison the window was answered. */
async function compareBeforeWithAfter(user: UserEvent): Promise<Compared> {
  await openListedCase(user, BEFORE_CASE);
  await screen.findByRole("button", { name: "More case actions" }, { timeout: 10_000 });
  await openCaseFlow(user, "Compare");
  const chooser = within(await screen.findByRole("dialog", { name: "Compare with" }));
  await user.selectOptions(await chooser.findByLabelText("Case"), await chooser.findByRole("option", { name: AFTER_CASE }));
  // A person compares once the case has drawn; its reads take the window's
  // one operation slot while it opens.
  await journey.settled();
  const asked = journey.callsTo("CompareCases").length;
  await press(user, chooser.getByRole("button", { name: "Compare" }));
  return answered(asked);
}

/** Opens Comparison options from the page's header. */
async function options(user: UserEvent) {
  await press(user, page().getAllByRole("button", { name: "Comparison options" })[0]!);
  return within(await screen.findByRole("dialog", { name: "Comparison options" }));
}

/** Names MSH-10 as the field that identifies one result, and returns the
 * comparison answered under it. */
async function keyedOnControlId(user: UserEvent): Promise<Compared> {
  const sheet = await options(user);
  await pickField(user, sheet, "Add to record keys", "MSH-10");
  await press(user, sheet.getByRole("button", { name: "Add" }));
  await journey.settled();
  const asked = journey.callsTo("CompareCases").length;
  await press(user, sheet.getByRole("button", { name: "Apply" }));
  return answered(asked);
}

/** A comparison's rows as `occurrence ↔ occurrence field`, missing and
 * inserted messages with no field. */
function rowsOf(compared: Compared): string[] {
  return compared.comparison!.rows.map((row) => `${row.earlier?.id ?? ""} ↔ ${row.later?.id ?? ""} ${row.field ?? ""}`.trim());
}

function reportedRows(report: DiffReport): string[] {
  return [
    ...report.pairs.flatMap((pair) => pair.fields.map((field) => `${pair.left.occurrence} ↔ ${pair.right.occurrence} ${field.selector}`)),
    ...report.missing.map((missing) => `${missing.occurrence} ↔`),
    ...report.inserted.map((inserted) => `↔ ${inserted.occurrence}`),
  ];
}

test("two cases are compared exactly as readmit diff reports them, a mismatched pair is refused with the key to declare, and a case changed after import is refused as the command line refuses it", async () => {
  const user = userEvent.setup();
  // The list's measured height, which a real layout gives it and jsdom does
  // not: without one the list draws a fixed first handful of rows.
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockImplementation(function (this: HTMLElement) {
    return this.classList.contains("table-view") ? HEIGHT : 0;
  });
  const { project, before, after } = await labProject(user);

  // Two collections that are not copies of one another are refused until the
  // field that identifies one result is named; the command line refuses them
  // too, naming its own option.
  const unkeyed = await compareBeforeWithAfter(user);
  expect(unkeyed).toMatchObject({ state: "failed", reason: MISMATCHED });
  expect(await page().findByText(MISMATCHED)).toBeTruthy();
  expect(screen.queryByRole("table", { name: "Differences" })).toBeNull();
  const refused = await journey.commandLine(["diff", `${project}/${before}`, `${project}/${after}`]);
  expect(refused.code).toBe(1);
  expect(refused.stderr).toBe("readmit: unrelated collections require explicit --key selectors for alignment\n");

  // Named, the results pair on their control IDs, and every difference is
  // the one `readmit diff` reports, in its order.
  const keyed = await keyedOnControlId(user);
  expect(keyed.state).toBe("completed");
  const diffed = await journey.commandLine(["diff", `${project}/${before}`, `${project}/${after}`, "--key", "MSH-10", "--format", "json"]);
  expect(diffed.code, diffed.stderr).toBe(0);
  const report = JSON.parse(diffed.stdout) as DiffReport;
  expect(report.summary).toMatchObject({ paired: PAIRED, changed: PAIRED, inserted: 1, missing: 1, ambiguous: 0, unaligned: 0 });
  expect(keyed.comparison!.summary).toEqual(report.summary);
  expect([keyed.comparison!.alignment, ...keyed.comparison!.keys]).toEqual([report.alignment, ...report.keys]);
  const rows = reportedRows(report);
  expect(rows).toHaveLength(DIFFERENCES + 2);
  expect(keyed.comparison!.total).toBe(rows.length);
  // Scrolled to its end, the list reads the next window from where the rows
  // it holds end, until it holds every row the report lists, in order.
  const windows = () =>
    journey
      .callsTo("CompareCases")
      .filter((call) => call.settled && (call.args[0] as { keys: string[] }).keys.length === 1 && (call.result as Compared).state === "completed");
  const differences = await screen.findByRole("table", { name: "Differences" });
  const viewport = differences.closest(".table-view") as HTMLElement;
  for (let held = keyed.comparison!.rows.length; held < rows.length; held = windows().reduce((sum, call) => sum + rowsOf(call.result as Compared).length, 0)) {
    const asked = windows().length;
    viewport.scrollTop = Number(differences.getAttribute("aria-rowcount")) * ROW_REM * rootFontSize() - HEIGHT;
    fireEvent.scroll(viewport);
    await waitFor(() => expect(windows().length).toBe(asked + 1), { timeout: 10_000 });
    expect(windows()[asked]!.args[0]).toMatchObject({ offset: held });
    await waitFor(() => expect(Number(differences.getAttribute("aria-rowcount")) - 1).toBe(held + rowsOf(windows()[asked]!.result as Compared).length));
  }
  const paged = windows().flatMap((call) => rowsOf(call.result as Compared));
  expect(paged).toEqual(rows);
  expect(await page().findByText(`${PAIRED} matched · 1 only in earlier · 1 only in later`)).toBeTruthy();
  const table = within(await screen.findByRole("table", { name: "Differences" }));
  expect(table.getAllByText("Hidden").length).toBeGreaterThan(0);
  expect(table.queryByText("RENAMED")).toBeNull();

  // Something outside the window rewrites one of the new feed's stored
  // results. Compare no longer offers it as a case to compare with, and the
  // command line refuses it.
  journey.changeFile(`${project.slice(journey.path().length + 1)}/${after}/payloads/s0001-e000001.bin`, labResult(2, "20260101120512", "9.99", "SYNTHETIC"));
  await openCaseFlow(user, "Compare");
  const chooser = within(await screen.findByRole("dialog", { name: "Compare with" }));
  expect(await chooser.findByText("No other case")).toBeTruthy();
  expect(chooser.queryByRole("option", { name: AFTER_CASE })).toBeNull();
  const byCommand = await journey.commandLine(["diff", `${project}/${before}`, `${project}/${after}`, "--key", "MSH-10"]);
  expect(byCommand.code).toBe(1);
  expect(byCommand.stderr).toBe(`readmit: ${TAMPERED}\n`);
});

/** The one saved policy document that holds this many rules. */
function savedPolicy(project: string, rules: number): string {
  const folder = project.slice(journey.path().length + 1);
  const held = filesUnder(project)
    .filter((file) => file.endsWith(".json"))
    .filter((file) => {
      const text = journey.readFile(`${folder}/${file}`);
      if (!text.includes('"readmit-normalization-policy/v1"')) return false;
      return (JSON.parse(text) as { rules: unknown[] }).rules.length === rules;
    });
  expect(held).toHaveLength(1);
  return `${project}/${held[0]}`;
}

/** What `readmit normalize` reports under one policy document. */
async function normalizeWith(project: string, before: string, after: string, policy: string) {
  return journey.commandLine(["normalize", `${project}/${before}`, `${project}/${after}`, "--key", "MSH-10", "--policy", policy, "--format", "json"]);
}

/** Saves the policy sheet open now, and returns the facade's answer. */
async function savePolicy(user: UserEvent, sheet: ReturnType<typeof within>) {
  const asked = journey.callsTo("SaveItem").length;
  await press(user, sheet.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(journey.callsTo("SaveItem")[asked]?.settled).toBe(true));
  return journey.callsTo("SaveItem")[asked]!.result as { outcome: string; problems: { problem: string }[] };
}

test("a comparison is read under a named normalization policy exactly as readmit normalize reads it, a rule the reader refuses is refused before anything is saved, and the extended policy is what the command line reads", async () => {
  const user = userEvent.setup();
  const { project, before, after } = await labProject(user);
  await compareBeforeWithAfter(user);
  expect((await keyedOnControlId(user)).state).toBe("completed");

  // A new policy: the message time within the hour and the count within a
  // tolerance typed with a sign, which the policy reader refuses. Nothing is
  // saved, and the command line refuses the same rule.
  await press(user, (await options(user)).getByRole("button", { name: "New policy" }));
  const sheet = within(await screen.findByRole("dialog", { name: "New policy" }));
  await user.type(sheet.getByLabelText("Name"), POLICY);
  await pickField(user, sheet, "Field path of rule 1", "MSH-7");
  await user.selectOptions(sheet.getAllByLabelText("Comparison")[0]!, "timestamp");
  await user.selectOptions(sheet.getByLabelText("Precision"), "hour");
  await press(user, sheet.getByRole("button", { name: "Add rule" }));
  await pickField(user, sheet, "Field path of rule 2", "OBX-5");
  await user.selectOptions(sheet.getAllByLabelText("Comparison")[1]!, "numeric");
  await user.clear(sheet.getByLabelText("Tolerance"));
  await user.type(sheet.getByLabelText("Tolerance"), "-0.05");
  const refusedSave = await savePolicy(user, sheet);
  expect(refusedSave.outcome).not.toBe("saved");
  expect(refusedSave.problems.map((problem) => problem.problem)).toEqual([NUMBER_RULE]);
  expect(await sheet.findByText(NUMBER_RULE)).toBeTruthy();
  journey.placeFixture("normalize-policy-refused.json", "exports/refused-policy.json");
  const refused = await normalizeWith(project, before, after, journey.path("exports", "refused-policy.json"));
  expect(refused.code).toBe(1);
  expect(refused.stderr).toBe(`readmit: ${UNSIGNED}\n`);

  // Corrected and saved, the policy is applied at once: the times and the
  // tolerated drift are hidden, the higher counts, the pending ones and the
  // renamed patients stay, exactly as `readmit normalize` reads the saved
  // document.
  await user.clear(sheet.getByLabelText("Tolerance"));
  await user.type(sheet.getByLabelText("Tolerance"), "0.05");
  const asked = journey.callsTo("CompareCases").length;
  expect((await savePolicy(user, sheet)).outcome).toBe("saved");
  const under = await answered(asked);
  const colleague = await normalizeWith(project, before, after, savedPolicy(project, 2));
  expect(colleague.code, colleague.stderr).toBe(0);
  const read = JSON.parse(colleague.stdout) as NormalizationReport;
  expect(read.summary).toMatchObject({ differences: DIFFERENCES, suppressed: SUPPRESSED, retained: HIGHER, undecided: PENDING, unaddressed: RENAMED });
  expect(under.comparison!.rules).toEqual(read.rules);
  expect(under.comparison!.suppressed).toBe(SUPPRESSED);
  expect(under.comparison!.total).toBe(DIFFERENCES - SUPPRESSED + 2);
  const kept = read.differences.filter((difference) => difference.outcome !== "suppressed");
  expect(under.comparison!.rows.filter((row) => row.kind === "paired").map((row) => `${row.earlier!.id} ${row.later!.id} ${row.field} ${row.outcome ?? "unaddressed"}`)).toEqual(
    kept.slice(0, under.comparison!.rows.filter((row) => row.kind === "paired").length).map((difference) => `${difference.left_occurrence} ${difference.right_occurrence} ${difference.selector} ${difference.outcome}`),
  );
  expect(await page().findByText(`${PAIRED} matched · 1 only in earlier · 1 only in later · ${SUPPRESSED} ignored by ${POLICY}`)).toBeTruthy();
  const rules = within(screen.getByRole("table", { name: "Policy rules" }));
  expect(rules.getAllByRole("row").slice(1).map((row) => [...row.querySelectorAll("td")].map((cell) => cell.textContent).join(" "))).toEqual(
    read.rules.map((rule, index) => `${index === 0 ? "Timestamp" : "Number"} ${index === 0 ? "MSH[1]-7[1]" : "OBX[1]-5[1]"} ${rule.suppressed} ${rule.retained} ${rule.undecided}`),
  );

  // The original differences stay one choice away.
  const originalAsked = journey.callsTo("CompareCases").length;
  await user.click(page().getByRole("checkbox", { name: "Original differences" }));
  const original = await answered(originalAsked);
  expect(original.comparison!.total).toBe(DIFFERENCES + 2);

  // The policy is extended with a rule that ignores the patient's name.
  // Saved, it is what the command line reads and what the window applies:
  // the renamed patients are now hidden too.
  await user.click(page().getByRole("checkbox", { name: "Original differences" }));
  await press(user, (await options(user)).getByRole("button", { name: "Edit policy" }));
  const editor = within(await screen.findByRole("dialog", { name: "Edit policy" }));
  await waitFor(() => expect(editor.getAllByRole("group")).toHaveLength(2));
  await press(user, editor.getByRole("button", { name: "Add rule" }));
  await pickField(user, editor, "Field path of rule 3", "PID-5");
  const extendedAsked = journey.callsTo("CompareCases").length;
  expect((await savePolicy(user, editor)).outcome).toBe("saved");
  const extended = await answered(extendedAsked);
  const extendedRead = await normalizeWith(project, before, after, savedPolicy(project, 3));
  expect(extendedRead.code, extendedRead.stderr).toBe(0);
  const extendedReport = JSON.parse(extendedRead.stdout) as NormalizationReport;
  expect(extendedReport.summary).toMatchObject({ differences: DIFFERENCES, suppressed: SUPPRESSED + RENAMED, retained: HIGHER, undecided: PENDING, unaddressed: 0 });
  expect(extended.comparison!.rules).toEqual(extendedReport.rules);
  expect(extended.comparison!.suppressed).toBe(SUPPRESSED + RENAMED);
  expect(await page().findByText(`${PAIRED} matched · 1 only in earlier · 1 only in later · ${SUPPRESSED + RENAMED} ignored by ${POLICY}`)).toBeTruthy();
});
