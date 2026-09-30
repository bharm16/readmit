// The primary journey, end to end through the real facade: a new project, a
// person's own export imported, a field chosen and filtered by, a test created
// from the chosen messages, a named nonproduction environment with its
// observation, a reviewed run whose Send reaches an independent receiver with
// a real reschedule defect, the report of that run, its shared preview and
// export, and a restart that finds every object again and resumes nothing.
// At no step does the person name an internal file, a schema, an index, a
// command or an approval hash.
import { afterEach, beforeEach, expect, test } from "vitest";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { RedactPolicy } from "../bindings";
import { enter, Journey, press, whenEnabled } from "../testkit/journey";
import { goTo, page } from "../testkit/navigation";
import {
  activateLicense,
  configureEnvironment,
  createProject,
  createRecordTest,
  EXPORTED_BOOKING,
  EXPORTED_RESCHEDULE,
  importExport,
  openedCase,
  pressServed,
  reviewRun,
  selectMessage,
} from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

test.each([false, true])("a new project completes import, duplicate regression, report, redacted export and restart (required derived check: %s)", async (requiredCheck) => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await journey.launch();
  await activateLicense(user, journey);

  // 1. New project: a name and the remembered location, and it opens on Cases.
  const project = await createProject(user, journey, "investigations", "scheduling", "Scheduling interface");
  // The receiver keeps its ledger handoff where the project can read it.
  const ledger = `${project.slice(journey.path().length + 1)}/appointments.json`;
  const downstream = await journey.startDownstream("downstream/appointments.csv", "duplicating", ledger);

  // 2. Import the export; the case opens on its Messages.
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule duplicates");
  const messages = await openedCase();
  const table = await messages.findByRole("table", { name: "Messages" });
  const rows = () => table.querySelectorAll("tr[data-row-id]").length;
  await waitFor(() => expect(rows()).toBe(2));

  // 3. A field of the reschedule, filtered by: the filter is transient.
  const reschedule = table.querySelectorAll("tr[data-row-id]")[1]!.getAttribute("data-row-id")!;
  const reader = await selectMessage(user, reschedule);
  await press(user, reader.getByRole("button", { name: "Show values" }));
  await press(user, await reader.findByRole("button", { name: /^MSH/ }));
  await press(user, await reader.findByRole("button", { name: /MSH\[1\]-9/ }));
  await press(user, await reader.findByRole("button", { name: "Filter by this field" }));
  const filter = within(await screen.findByRole("dialog", { name: "Filter" }));
  await press(user, filter.getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(rows()).toBe(1));
  expect(journey.callsTo("SaveView")).toHaveLength(0);
  await press(user, (await messages.findAllByRole("button", { name: "Clear filters" }))[0]!);
  await waitFor(() => expect(rows()).toBe(2));
  expect(journey.callsTo("SaveView")).toHaveLength(0);

  // 5. The receiver as a named nonproduction environment with its observation.
  const environment = await configureEnvironment(user, journey, downstream.address, ledger);
  expect(downstream.received()).toHaveLength(0);

  // 4. A test from the chosen messages of the case.
  await goTo(user, "Cases");
  await press(user, await page().findByText("Reschedule duplicates"));
  await openedCase();
  await createRecordTest(user, journey, environment, "Reschedule keeps one appointment");

  // 6. Run: the review states exactly what it sends; nothing is sent before
  // Send. The receiver acknowledges both messages AA and still books the
  // reschedule as a second appointment: the count check fails 1 against 2.
  const review = await reviewRun(user, journey, /^Reschedule keeps one appointment/);
  expect(await review.findByText(`Sends 2 messages to ${environment} once.`, undefined, { timeout: 10_000 })).toBeTruthy();
  expect(downstream.received()).toHaveLength(0);
  // A second click on Send while the first is answered sends nothing more.
  await user.dblClick(review.getByRole("button", { name: "Send" }));
  await waitFor(() => expect(journey.callsTo("ExecuteReviewedAction")[0]?.settled).toBe(true), { timeout: 120_000 });
  await page().findByRole("table", { name: "Checks" }, { timeout: 30_000 });
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(1);
  expect(downstream.received()).toHaveLength(2);
  const checks = page().getByRole("table", { name: "Checks" });
  await waitFor(() => expect(rowsOf(checks)).toContainEqual(["Record count", "1", "2", "Failed"]));
  await press(user, page().getByRole("tab", { name: "Messages" }));
  expect(rowsOf(await page().findByRole("table", { name: "Messages" }))).toEqual([
    ["SIU · S12", "Acknowledged", "AA"],
    ["SIU · S13", "Acknowledged", "AA"],
  ]);
  const runTitle = page().getByRole("heading", { level: 1 }).textContent;

  // 7. A report straight from the run: no packet, specification or folder.
  await press(user, page().getByRole("button", { name: "Create report" }));
  const creating = within(await screen.findByRole("dialog", { name: "New report" }));
  for (const gone of [/packet/i, /historical/i, /folder/i, /output/i, /specification/i]) expect(creating.queryByLabelText(gone)).toBeNull();
  // The run it was created from is chosen, and names the report.
  await waitFor(() => expect((creating.getByLabelText("Name") as HTMLInputElement).value, `Run: ${(creating.getByLabelText("Run") as HTMLSelectElement).value}`).not.toBe(""), { timeout: 10_000 });
  const reportName = (creating.getByLabelText("Name") as HTMLInputElement).value;
  await journey.settled();
  await pressServed(user, journey, creating.getByRole("button", { name: "Create" }), "SaveItem");
  expect(await page().findByRole("heading", { level: 1, name: reportName }, { timeout: 30_000 })).toBeTruthy();
  expect(runTitle).toBeTruthy();

  if (requiredCheck) {
    await authorCheckTemplate(user, journey);
    await goTo(user, "Reports");
    if (!page().queryByRole("heading", { level: 1, name: "Reports" })) await goTo(user, "Reports");
    await press(user, await page().findByText(reportName));
  }

  // 8. Share: Contents, Redaction, Preview of the actual output, and Export to
  // a new file this person names. The messages go in; their patient values do
  // not come out.
  await press(user, await page().findByRole("button", { name: "Share" }, { timeout: 30_000 }));
  await page().findByRole("heading", { level: 1, name: "Share report" });
  await page().findByRole("table", { name: "Contents" }, { timeout: 30_000 });
  const included = page().queryByRole("checkbox", { name: "Selected messages" });
  if (included && !(included as HTMLInputElement).checked) await press(user, included);
  await press(user, page().getAllByRole("button", { name: "Change" })[0]!);
  const format = within(await screen.findByRole("dialog", { name: "Format" }));
  await press(user, format.getByRole("radio", { name: "Markdown" }));
  await press(user, format.getByRole("button", { name: "Apply" }));
  await press(user, page().getByRole("button", { name: "Redaction" }));
  if (requiredCheck) {
    await press(user, within(page().getByLabelText("Template", { selector: "dl" })).getByRole("button", { name: "Change" }));
    const choosing = within(await screen.findByRole("dialog", { name: "Template" }));
    await press(user, await choosing.findByRole("radio", { name: "Checked regression" }));
    await press(user, choosing.getByRole("button", { name: "Apply" }));
  }
  const redaction = await page().findByRole("table", { name: "Redaction" }, { timeout: 30_000 });
  for (let row = unresolved(redaction); row; row = unresolved(redaction)) {
    const label = row.querySelector("th, td")?.textContent ?? "";
    await press(user, row);
    await user.keyboard("{Enter}");
    const treatment = within(await screen.findByRole("dialog"));
    // Each row takes the treatment its category offers: removal where there
    // is one, words a person chooses for free text, otherwise the one
    // derived treatment offered, such as regenerated metadata.
    const remove = treatment.queryAllByRole("radio", { name: /^Remove/ })[0];
    const replace = treatment.queryByRole("radio", { name: "Replace" });
    if (remove) await press(user, remove);
    else if (replace) {
      await press(user, replace);
      await enter(user, treatment.getByLabelText("Replacement"), "Reschedule regression");
    } else await press(user, treatment.getAllByRole("radio")[0]!);
    await press(user, treatment.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(unresolved(redaction)?.querySelector("th, td")?.textContent).not.toBe(label), { timeout: 30_000 });
  }
  if (requiredCheck) {
    expect(await page().findByText("Not run")).toBeTruthy();
    await press(user, page().getByRole("button", { name: "Run check" }));
    let checking = within(await screen.findByRole("dialog", { name: "Run check" }));
    await checking.findByText("The failed checks");
    expect(checking.getByRole("button", { name: "Run check" })).toHaveProperty("disabled", true);
    await press(user, checking.getByRole("checkbox", { name: "Contents reviewed" }));
    await pressServed(user, journey, checking.getByRole("button", { name: "Run check" }), "ExecuteReviewedAction");
    const prepared = journey.callsTo("ExecuteReviewedAction").at(-1)?.result;
    expect(prepared, JSON.stringify(prepared)).toMatchObject({ outcome: "completed", share_check: { phase: "failure" } });
    checking = within(await screen.findByRole("dialog", { name: "Run check" }));
    await checking.findByText(/^Sends 2 messages to .* once\.$/, undefined, { timeout: 30_000 });
    const send = checking.getByRole("button", { name: "Send" });
    expect(downstream.received()).toHaveLength(2);
    downstream.reset();
    for (const box of checking.queryAllByRole("checkbox", { name: "Mark complete" })) await press(user, box);
    await pressServed(user, journey, send, "ExecuteReviewedAction").catch(error => { throw new Error(`${String(error)}; derived review: ${screen.queryByRole("dialog", { name: "Run check" })?.textContent}; latest: ${JSON.stringify(journey.callsTo("PrepareAction").at(-1)?.result)}`); });
    expect(journey.callsTo("ExecuteReviewedAction").at(-1)?.result).toMatchObject({ outcome: "completed" });
    await waitFor(() => expect(journey.callsTo("OpenRun").at(-1)?.settled).toBe(true), { timeout: 30_000 });
    expect(downstream.received(), JSON.stringify({ execute: journey.callsTo("ExecuteReviewedAction").at(-1)?.result, run: journey.callsTo("OpenRun").at(-1)?.result })).toHaveLength(4);
    await goTo(user, "Reports");
    if (!page().queryByRole("heading", { level: 1, name: "Reports" })) await goTo(user, "Reports");
    await press(user, await page().findByText(reportName));
    await press(user, await page().findByRole("button", { name: "Share" }));
    await page().findByRole("heading", { level: 1, name: "Share report" });
    if (!page().queryByRole("table", { name: "Redaction" })) await press(user, await page().findByRole("button", { name: "Redaction" }));
    expect(await page().findByText("Matched", undefined, { timeout: 30_000 })).toBeTruthy();
  }
  await press(user, page().getByRole("button", { name: "Preview" }));
  const preview = await page().findByLabelText(/\.md$/, undefined, { timeout: 30_000 });
  const previewed = preview.textContent ?? "";
  for (const value of PATIENT_VALUES) expect(previewed).not.toContain(value);
  journey.makeFolder("shared");
  const exported = requiredCheck ? "shared/Checked regression" : "shared/Scheduling regression.md";
  await journey.nameNewFolder(journey.path(exported), requiredCheck ? "Export package" : "Export report");
  await press(user, page().getByRole("button", { name: "Choose" }));
  expect(await page().findByText("Exports a file containing patient data.").catch(() => null)).toBeNull();
  const exports = journey.callsTo("ExecuteReviewedAction").length;
  // A second click on Export writes nothing more.
  await user.dblClick(await whenEnabled(page().getByRole("button", { name: "Export" })));
  expect(await page().findByText(/^Exported /, undefined, { timeout: 30_000 })).toBeTruthy();
  expect(journey.callsTo("ExecuteReviewedAction").slice(exports)).toHaveLength(1);
  const reportOutput = requiredCheck ? `${exported}/${preview.getAttribute("aria-label")!}` : exported;
  const written = journey.readFile(reportOutput);
  expect(written.trim()).toBe(previewed.trim());
  for (const value of PATIENT_VALUES) expect(written).not.toContain(value);
  if (requiredCheck) {
    expect(existsSync(journey.path(`${exported}/Derived test`))).toBe(true);
    for (const entry of readdirSync(journey.path(exported), { recursive: true, withFileTypes: true })) {
      if (!entry.isFile()) continue;
      const bytes = readFileSync(join(entry.parentPath, entry.name), "utf8");
      for (const value of PATIENT_VALUES) expect(bytes.includes(value), entry.name).toBe(false);
    }
    for (const bytes of downstream.received().slice(2)) for (const value of PATIENT_VALUES) expect(bytes.toString()).not.toContain(value);
    // A second editor changes the template after this window's fresh export
    // review. The stale final click must neither export nor reuse the old match.
    const nextOutput = "shared/Changed policy";
    await journey.nameNewFolder(journey.path(nextOutput), "Export package");
    await press(user, page().getByRole("button", { name: "Change" }));
    await waitFor(() => expect(page().getByRole("button", { name: "Export" }).matches(":disabled"), JSON.stringify(journey.callsTo("PrepareAction").at(-1)?.result)).toBe(false));
    const saved = journey.callsTo("SaveShareTemplate").at(-1)?.result as { template: { entry: string }; policy: RedactPolicy };
    const changed = structuredClone(saved.policy);
    const nameRule = changed.fields.find(rule => rule.selector === "PID[1]-5[1]" || rule.selector === "PID-5")!;
    nameRule.policy = "replace-field/v1";
    nameRule.replacement = "Redacted patient";
    journey.changeFile(`${project.slice(journey.path().length + 1)}/${saved.template.entry}`, JSON.stringify(changed));
    await pressServed(user, journey, page().getByRole("button", { name: "Export" }), "ExecuteReviewedAction");
    expect(journey.callsTo("ExecuteReviewedAction").at(-1)?.result).toMatchObject({ outcome: "stale" });
    expect(existsSync(journey.path(nextOutput))).toBe(false);
    expect(downstream.received()).toHaveLength(4);
    await press(user, page().getByRole("button", { name: "Back" }));
    expect(await page().findByText("Not run")).toBeTruthy();
  }


  // 9. A second run is reviewed and left unsent when the application ends at
  // once. After the restart the project, its case, test, environment, run
  // and report are all read again from what was saved; the review's consent
  // did not survive, and nothing is sent, exported or reviewed on its own.
  await reviewRun(user, journey, /^Reschedule keeps one appointment/);
  expect(downstream.received()).toHaveLength(requiredCheck ? 4 : 2);
  await journey.crash();
  const before = journey.calls.length;
  await journey.launch();
  const projects = await page().findByRole("table", { name: "Projects" }, { timeout: 30_000 });
  await press(user, await within(projects).findByRole("row", { name: "Scheduling interface" }));
  expect(await page().findByRole("heading", { level: 1, name: "Cases" }, { timeout: 30_000 })).toBeTruthy();
  expect(await page().findByText("Reschedule duplicates")).toBeTruthy();
  await goTo(user, "Tests");
  expect(await page().findByText("Reschedule keeps one appointment")).toBeTruthy();
  await goTo(user, "Environments");
  expect(await page().findByText(environment)).toBeTruthy();
  await goTo(user, "Runs");
  const history = await page().findByRole("table", { name: "Runs" }, { timeout: 30_000 });
  await waitFor(() => expect(rowsOf(history).some((row) => row.includes("Failed"))).toBe(true));
  await goTo(user, "Reports");
  await press(user, await page().findByText(reportName));
  expect(await page().findByRole("heading", { level: 1, name: reportName }, { timeout: 30_000 })).toBeTruthy();
  await journey.settled();
  const after = journey.calls.slice(before).map((call) => call.method);
  for (const final of ["ExecuteReviewedAction", "SaveItem", "CheckEnvironment"]) expect(after).not.toContain(final);
  expect(downstream.received()).toHaveLength(requiredCheck ? 4 : 2);
  expect(journey.readFile(reportOutput)).toBe(written);
});

/** The synthetic patient values the exported messages carry. */
const PATIENT_VALUES = ["SYNTH-101", "SYNTHETIC^ONLY"];

/** The first redaction row still unresolved, if any. */
function unresolved(table: HTMLElement): HTMLElement | undefined {
  return Array.from(table.querySelectorAll<HTMLElement>("tbody tr")).find((row) => row.textContent?.includes("Unresolved"));
}

/** The text of each body row of a table, cell by cell. */
function rowsOf(table: HTMLElement): string[][] {
  return Array.from(table.querySelectorAll("tbody tr")).map((row) => Array.from(row.querySelectorAll("th, td")).map((cell) => cell.textContent ?? ""));
}


/** Authors the whole disclosure policy through its real editor. The fixture is
 * source material outside the project, never precreated project configuration. */
async function authorCheckTemplate(user: ReturnType<typeof userEvent.setup>, journey: Journey) {
  journey.placeFixture("redact-policy.json", "source-policy.json");
  const policy = JSON.parse(journey.readFile("source-policy.json")) as RedactPolicy;
  await goTo(user, "Reports");
  await press(user, page().getByRole("button", { name: "More report actions" }));
  await press(user, screen.getByRole("menuitem", { name: "Templates" }));
  await press(user, await page().findByRole("button", { name: "New template" }));
  const naming = within(await screen.findByRole("dialog", { name: "New template" }));
  await enter(user, naming.getByLabelText("Name"), "Checked regression");
  await press(user, naming.getByRole("button", { name: "Create" }));
  const editPart = async (label: string) => {
    const row = page().getByText(label, { selector: "dt" }).closest(".value-row")!;
    await press(user, within(row as HTMLElement).getByRole("button", { name: "Edit" }));
  };
  await editPart("Patient identity");
  let sheet = within(await screen.findByRole("dialog", { name: "Patient identity" }));
  await enter(user, sheet.getByLabelText("Authority fields"), policy.patient.authority.join(", "));
  await press(user, sheet.getByRole("button", { name: "Apply" }));
  await editPart("Segments removed");
  sheet = within(await screen.findByRole("dialog", { name: "Segments removed" }));
  await enter(user, sheet.getByLabelText("Segments"), policy.remove_segments.join(", "));
  await press(user, sheet.getByRole("button", { name: "Apply" }));
  await editPart("Regenerated");
  sheet = within(await screen.findByRole("dialog", { name: "Regeneration" }));
  for (const label of ["File names", "Metadata", "Test literals", "Diagnosis", "Check derived tests by a run"]) await press(user, sheet.getByRole("checkbox", { name: label }));
  await press(user, sheet.getByRole("button", { name: "Apply" }));
  const labels: Record<string, string> = { "remove-field/v1": "Remove", "replace-field/v1": "Replace", "scoped-surrogate/v1": "Scoped surrogate", "patient-date-shift/v1": "Shift dates", "retain-literal/v1": "Keep allowed values" };
  for (const rule of policy.fields) {
    await press(user, page().getByRole("button", { name: "Add field" }));
    sheet = within(await screen.findByRole("dialog", { name: "Add field" }));
    await enter(user, sheet.getByLabelText("Field"), rule.selector);
    await press(user, sheet.getByRole("radio", { name: labels[rule.policy]! }));
    const category = sheet.queryByLabelText("Category");
    if (category) await user.selectOptions(category, rule.class);
    if (rule.replacement !== undefined) await enter(user, sheet.getByLabelText("Replacement"), rule.replacement);
    if (rule.scope) {
      await enter(user, sheet.getByLabelText("Scope"), rule.scope);
      await enter(user, sheet.getByLabelText("Authority fields"), (rule.authority ?? []).join(", "));
    }
    if (rule.allowed) await enter(user, sheet.getByLabelText("Allowed values"), rule.allowed.join("\n"));
    await press(user, sheet.getByRole("button", { name: "Apply" }));
  }
  await pressServed(user, journey, page().getByRole("button", { name: "Save" }), "SaveShareTemplate");
  expect(journey.callsTo("SaveShareTemplate").at(-1)?.result).toMatchObject({ state: "completed" });
  await press(user, page().getByRole("button", { name: "Close" }));
}
