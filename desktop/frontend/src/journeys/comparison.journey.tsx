// Comparing two collections, and reading the comparison under a normalization
// policy, over the real facade.
//
// A laboratory interface was upgraded. The person captured what the analyser
// sent before and after the upgrade with the command line, and opens both
// cases in the window. After the upgrade every result carries a later message
// time, the white-cell count drifts by a hundredth or two — a drift the lab
// tolerates — every tenth result reads markedly higher, every twenty-fifth is
// still pending, and every fiftieth names the patient differently. The first
// result of the old feed is not in the new one, and the new feed holds one
// result the old one never sent.
//
// The window compares the two, is refused until the person names the field
// that identifies one result, and pages the 251 rows exactly as `readmit diff`
// reports them. A case a later release wrote is not offered, and a case whose
// stored bytes change after the window listed it is refused in the command
// line's words. Under a colleague's policy the window hides the time and the
// tolerated drift and keeps everything else visible, exactly as `readmit
// normalize` reads the same policy; the person opens that policy, extends it
// with a rule of their own and saves it as a new entry, and the command line
// reads what they saved. Policies the reader refuses are refused by the
// preview, the editor and the command line in one sentence, and an open that
// would replace unsaved rules is asked first and can be cancelled.
//
// Every message and value here is synthetic.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press, region, whenEnabled } from "../testkit/journey";
import { activateLicense, framed, tabTo } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

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

/** Results 2 to 250, which both feeds hold: result k is the kth message
 * before the upgrade and the (k-1)th after it. Result 1 is only before, and
 * result 251 only after. */
const SHARED = Array.from({ length: 249 }, (_, index) => index + 2);
const PAIRED = SHARED.length;
const ROWS = PAIRED + 2;
const PENDING = SHARED.filter(pending).length;
const HIGHER = SHARED.filter(higher).length;
const RENAMED = SHARED.filter(renamed).length;
/** Each paired result differs in its message time and its count, and a
 * renamed one in the patient's name too. */
const DIFFERENCES = PAIRED * 2 + RENAMED;

/** The occurrence the nth message of a captured feed is. */
const occurrence = (n: number) => `s0001-e${String(n).padStart(6, "0")}`;
const shared = (result: number) => `${occurrence(result)} ↔ ${occurrence(result - 1)}`;

/** The rows a comparison on the control ID holds, in the order a comparison
 * reports records: every paired result, result 1 that only the old feed
 * holds, then result 251 that only the new one holds. */
const ROW_PAIRS = [...SHARED.map(shared), `${occurrence(1)} ↔ `, ` ↔ ${occurrence(250)}`];

type Outcome = "suppressed" | "retained" | "undecided" | "unaddressed";

interface Difference {
  pair: string;
  selector: string;
  outcome: Outcome;
}

/** Every difference of a paired result, position by position in message
 * order, with what a policy of the time and the tolerated drift, and
 * optionally of the name, does about it. */
function expectedDifferences(nameIgnored: boolean): Difference[] {
  return SHARED.flatMap((result): Difference[] => [
    { pair: shared(result), selector: "MSH[1]-7[1]", outcome: "suppressed" },
    ...(renamed(result) ? [{ pair: shared(result), selector: "PID[1]-5[1]", outcome: nameIgnored ? "suppressed" : "unaddressed" } as const] : []),
    {
      pair: shared(result),
      selector: "OBX[1]-5[1]",
      outcome: pending(result) ? "undecided" : higher(result) ? "retained" : "suppressed",
    },
  ]);
}

/** How the preview words each outcome. */
const SHOWN: Record<Outcome, string> = {
  suppressed: "Hidden by the policy",
  retained: "Kept by the policy",
  undecided: "The policy could not decide",
  unaddressed: "No rule addresses this position",
};

/** Each rule's counts — compared, suppressed, retained, undecided: every
 * paired result has each position, and only the count's rule keeps some or
 * cannot decide some. */
const SUPPRESSED = PAIRED + (PAIRED - HIGHER - PENDING);
const COLLEAGUE_RULES = [
  `volatile-message-time ${PAIRED} ${PAIRED} 0 0`,
  `analyser-drift ${PAIRED} ${PAIRED - HIGHER - PENDING} ${HIGHER} ${PENDING}`,
];

/** A colleague's policy: the message time within the hour and the count
 * within a twentieth. */
const COLLEAGUE_POLICY = `${JSON.stringify(
  {
    schema: "readmit-normalization-policy/v1",
    rules: [
      { id: "volatile-message-time", selector: "MSH-7", operator: "timestamp", precision: "hour" },
      { id: "analyser-drift", selector: "OBX[1]-5", operator: "numeric", tolerance: "0.05" },
    ],
  },
  null,
  2,
)}\n`;

const MISMATCHED =
  "these collections are not copies of one another; name the fields that identify one record, such as MSH-10, to align them";
const TAMPERED = "bundle is incomplete or its identity does not match contents";
const UNDECODABLE = "invalid normalization policy JSON";
const UNSIGNED = "a numeric tolerance is an unsigned decimal distance";

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

interface NormalizationReport {
  summary: Record<string, number>;
  rules: { id: string; compared: number; suppressed: number; retained: number; undecided: number }[];
  differences: { left_occurrence: string; right_occurrence: string; selector: string; outcome: string; rule?: string }[];
}

/** The license's operation policy: capturing is licensed work. */
function operationPolicy(): string {
  return journey.path("vendor-delivered-license", "operation-policy.json");
}

/** Captures both feeds into case bundles with the command line, as the person
 * did before opening the window. */
async function captureFeeds(): Promise<void> {
  journey.writeFile("exports/before-upgrade.mllp", BEFORE);
  journey.writeFile("exports/after-upgrade.mllp", AFTER);
  journey.makeFolder("lab");
  for (const [feed, output] of [
    ["exports/before-upgrade.mllp", "lab/before"],
    ["exports/after-upgrade.mllp", "lab/after"],
  ] as const) {
    const captured = await journey.commandLine(["--operation-policy", operationPolicy(), "capture", feed, "--output", output]);
    expect(captured.code, captured.stderr).toBe(0);
  }
}

/** Opens the lab folder and verifies the case captured before the upgrade. */
async function openBefore(user: UserEvent) {
  await journey.chooseFolder(journey.path("lab"), "Open a readmit workspace folder");
  await press(user, screen.getAllByRole("button", { name: "Open workspace…" })[0] as HTMLElement);
  const navigation = within(region("Workspace"));
  const listed = (await navigation.findByText("before", { selector: ".name" })).closest("li") as HTMLElement;
  await press(user, within(listed).getByRole("button", { name: "Open case" }));
  return within(await screen.findByRole("region", { name: "Compare collections" }));
}

/** The key field of the comparison form. */
function keyField(panel: ReturnType<typeof within>): HTMLElement {
  return panel.getByLabelText("Record keys");
}

/** Asks for the comparison of the open case with another, on the keys typed,
 * pressing Enter in the key field, and waits for the answer. */
async function compareWith(user: UserEvent, panel: ReturnType<typeof within>, right: string, keys: string): Promise<void> {
  await user.selectOptions(await whenEnabled(panel.getByLabelText("Compare with")), right);
  await enter(user, keyField(panel), keys);
  const asked = journey.callsTo("Compare").length;
  await user.type(keyField(panel), "{Enter}");
  await waitFor(() => expect(journey.callsTo("Compare")[asked]?.settled).toBe(true));
}

/** The occurrences each drawn row holds, left and right, as `readmit diff`
 * lists the same records: paired, then missing, then inserted. */
function drawnRows(panel: ReturnType<typeof within>): string[] {
  return panel.getAllByRole("row").slice(1).map((row: HTMLElement) => {
    const left = row.querySelector(".pane-left .occurrence")?.textContent ?? "";
    const right = row.querySelector(".pane-right .occurrence")?.textContent ?? "";
    return `${left} ↔ ${right}`;
  });
}

function reportedRows(report: DiffReport): string[] {
  return [
    ...report.pairs.map((pair) => `${pair.left.occurrence} ↔ ${pair.right.occurrence}`),
    ...report.missing.map((missing) => `${missing.occurrence} ↔ `),
    ...report.inserted.map((inserted) => ` ↔ ${inserted.occurrence}`),
  ];
}

/** Presses a control from the keyboard: from the key field, Tab until the
 * control has focus, then Enter. */
async function activate(user: UserEvent, panel: ReturnType<typeof within>, control: HTMLElement): Promise<void> {
  keyField(panel).focus();
  await tabTo(user, control);
  await user.keyboard("{Enter}");
}

/** The differences the preview lists, as the person reads them: the pair,
 * the position and what the policy did. */
function drawnDifferences(section: ReturnType<typeof within>): { pair: string; selector: string; shown: string }[] {
  return section
    .getAllByRole("listitem")
    .filter((item: HTMLElement) => item.className.startsWith("outcome-"))
    .map((item: HTMLElement) => ({
      pair: item.querySelector(".occurrence")?.textContent ?? "",
      selector: item.querySelector(".selector")?.textContent ?? "",
      shown: item.querySelector(".status")?.textContent ?? "",
    }));
}

function shownDifferences(differences: Difference[]): { pair: string; selector: string; shown: string }[] {
  return differences.map((difference) => ({ pair: difference.pair, selector: difference.selector, shown: SHOWN[difference.outcome] }));
}

test("two collections are compared and paged exactly as readmit diff reports them, a mismatched pair is refused with the key to declare, and collections this release cannot compare are refused as the command line refuses them", async () => {
  const user = userEvent.setup();
  journey.provisionLicense("vendor-delivered-license");
  await captureFeeds();
  // A colleague's newer release wrote a case this release does not read.
  const newer = await journey.commandLine(["--operation-policy", operationPolicy(), "capture", "exports/after-upgrade.mllp", "--output", "lab/newer-case"]);
  expect(newer.code, newer.stderr).toBe(0);
  journey.changeFile("lab/newer-case/manifest.json", journey.readFile("lab/newer-case/manifest.json").replace('"schema":"readmit-case/v1"', '"schema":"readmit-case/v9"'));
  await journey.launch();
  const panel = await openBefore(user);

  // Only the two case bundles are offered; the newer case is listed, and
  // refused by the command line as well.
  expect(within(region("Workspace")).getByText("newer-case", { selector: ".name" })).toBeTruthy();
  const picker = panel.getByLabelText("Compare with") as HTMLSelectElement;
  expect(Array.from(picker.options).map((option) => option.value)).toEqual(["", "after", "before"]);
  const unsupported = await journey.commandLine(["diff", "lab/before", "lab/newer-case", "--key", "MSH-10"]);
  expect(unsupported.code).toBe(1);
  expect(unsupported.stderr).toBe("readmit: unsupported case bundle schema version\n");

  // Two collections that are not copies of one another are refused until the
  // field that identifies one result is named; the command line refuses them
  // too, naming its own option.
  await compareWith(user, panel, "after", "");
  expect(await panel.findByText(MISMATCHED)).toBeTruthy();
  expect(panel.queryByRole("table")).toBeNull();
  const unkeyed = await journey.commandLine(["diff", "lab/before", "lab/after"]);
  expect(unkeyed.code).toBe(1);
  expect(unkeyed.stderr).toBe("readmit: unrelated collections require explicit --key selectors for alignment\n");

  // Named, the results pair on their control IDs, and the counts are the
  // scenario's.
  await compareWith(user, panel, "after", "MSH-10");
  expect(await panel.findByText("Rows 1–200 of 251")).toBeTruthy();
  expect(panel.getByText(`${PAIRED} paired · ${PAIRED} changed · 0 unchanged`)).toBeTruthy();
  expect(panel.getByText("1 only in after · 1 not in after")).toBeTruthy();
  const diffed = await journey.commandLine(["diff", "lab/before", "lab/after", "--key", "MSH-10", "--format", "json"]);
  expect(diffed.code, diffed.stderr).toBe(0);
  const report = JSON.parse(diffed.stdout) as DiffReport;
  expect(report.summary).toMatchObject({ paired: PAIRED, changed: PAIRED, inserted: 1, missing: 1, ambiguous: 0, unaligned: 0 });
  expect(panel.getByText("aligned by declared-keys on MSH[1]-10[1]")).toBeTruthy();
  expect([report.alignment, ...report.keys]).toEqual(["declared-keys", "MSH[1]-10[1]"]);
  expect(ROW_PAIRS).toHaveLength(ROWS);
  expect(reportedRows(report)).toEqual(ROW_PAIRS);
  expect(drawnRows(panel)).toEqual(ROW_PAIRS.slice(0, 200));

  // Result 50 is row 49: it names the positions that differ — the time, the
  // renamed patient and the count — and no value.
  await activate(user, panel, panel.getByRole("button", { name: "49" }));
  const opened = within(panel.getByRole("region", { name: "Differences" }));
  const positions = ["MSH[1]-7[1]", "PID[1]-5[1]", "OBX[1]-5[1]"];
  expect(opened.getAllByText(/^[A-Z]{3}\[\d+\]-\d+\[\d+\]$/).map((selector) => selector.textContent)).toEqual(positions);
  expect(report.pairs[48]!.fields.map((field) => field.selector)).toEqual(positions);
  expect(opened.queryByText("RENAMED")).toBeNull();

  // The next window, from the keyboard, is the rest of the same comparison.
  await activate(user, panel, panel.getByRole("button", { name: "Next 200" }));
  expect(await panel.findByText("Rows 201–251 of 251")).toBeTruthy();
  expect(drawnRows(panel)).toEqual(ROW_PAIRS.slice(200));
  expect(panel.getByText("Not in the after collection")).toBeTruthy();
  expect(panel.getByText("Only in the after collection")).toBeTruthy();
  await activate(user, panel, panel.getByRole("button", { name: "Previous 200" }));
  expect(await panel.findByText("Rows 1–200 of 251")).toBeTruthy();

  // Something outside the window rewrites one of the new feed's stored
  // results. Comparing again is refused in the command line's words, and no
  // row of the comparison before it is left on screen.
  journey.changeFile("lab/after/payloads/s0001-e000001.bin", labResult(2, "20260101120512", "9.99", "SYNTHETIC"));
  await press(user, panel.getByRole("button", { name: "Compare" }));
  expect(await panel.findByText(TAMPERED)).toBeTruthy();
  expect(panel.queryByRole("table")).toBeNull();
  const tampered = await journey.commandLine(["diff", "lab/before", "lab/after", "--key", "MSH-10"]);
  expect(tampered.code).toBe(1);
  expect(tampered.stderr).toBe(`readmit: ${TAMPERED}\n`);
});

/** `readmit normalize` over the two cases on the control ID, under one
 * policy entry of the lab folder. */
function normalizeWith(policy: string) {
  return journey.commandLine(["normalize", "lab/before", "lab/after", "--key", "MSH-10", "--policy", `lab/${policy}`, "--format", "json"]);
}

/** What `readmit normalize` reports under a policy it reads. */
async function readUnder(policy: string): Promise<NormalizationReport> {
  const normalized = await normalizeWith(policy);
  expect(normalized.code, normalized.stderr).toBe(0);
  return JSON.parse(normalized.stdout) as NormalizationReport;
}

async function preview(user: UserEvent, section: ReturnType<typeof within>, policy: string): Promise<void> {
  const picker = section.getByLabelText("Normalization policy");
  // A policy saved a moment ago is offered once the folder is read again.
  await within(picker).findByRole("option", { name: policy });
  await user.selectOptions(await whenEnabled(picker), policy);
  const asked = journey.callsTo("NormalizeCompare").length;
  await press(user, section.getByRole("button", { name: "Preview" }));
  await waitFor(() => expect(journey.callsTo("NormalizeCompare")[asked]?.settled).toBe(true));
}

/** Each rule's row as the preview draws it: its ID and its four counts. */
function drawnRules(section: ReturnType<typeof within>): string[] {
  const table = section.getAllByRole("table")[0]!;
  return within(table).getAllByRole("row").slice(1).map((row: HTMLElement) => {
    const cells = Array.from(row.children).map((cell) => cell.textContent ?? "");
    return [cells[0], ...cells.slice(3)].join(" ");
  });
}

function reportedRules(report: NormalizationReport): string[] {
  return report.rules.map((rule) => [rule.id, rule.compared, rule.suppressed, rule.retained, rule.undecided].join(" "));
}

test("a comparison is read under a retained normalization policy exactly as readmit normalize reads it, the policy is opened, extended and saved as a new entry the command line reads, and undecodable policies and a cancelled open change nothing", async () => {
  const user = userEvent.setup();
  await journey.launch();
  await activateLicense(user, journey);
  await captureFeeds();
  journey.writeFile("lab/colleague-policy.json", COLLEAGUE_POLICY);
  journey.placeFixture("normalize-policy-refused.json", "lab/refused-policy.json");
  journey.writeFile("lab/truncated-policy.json", COLLEAGUE_POLICY.slice(0, 120));
  const colleagueDigest = journey.digest("lab/colleague-policy.json");
  const panel = await openBefore(user);
  await compareWith(user, panel, "after", "MSH-10");
  await panel.findByText("Rows 1–200 of 251");
  const section = within(panel.getByRole("region", { name: "Comparison under a normalization policy" }));

  // Under the colleague's policy the time and the tolerated drift are hidden;
  // the higher counts, the pending ones and the renamed patients stay visible.
  await preview(user, section, "colleague-policy.json");
  expect(await section.findByText(`${DIFFERENCES} differences · ${SUPPRESSED} suppressed · ${HIGHER} retained`)).toBeTruthy();
  expect(section.getByText(`${PENDING} undecided · ${RENAMED} not addressed by any rule`)).toBeTruthy();
  expect(section.getByText(`policy colleague-policy.json · exact bytes hash to ${colleagueDigest} · readmit-normalization/v1`)).toBeTruthy();
  const colleague = await readUnder("colleague-policy.json");
  expect(colleague.summary).toMatchObject({ differences: DIFFERENCES, suppressed: SUPPRESSED, retained: HIGHER, undecided: PENDING, unaddressed: RENAMED });
  expect(drawnRules(section)).toEqual(COLLEAGUE_RULES);
  expect(reportedRules(colleague)).toEqual(COLLEAGUE_RULES);
  const expected = expectedDifferences(false);
  expect(expected).toHaveLength(DIFFERENCES);
  expect(
    colleague.differences.map((difference) => ({
      pair: `${difference.left_occurrence} ↔ ${difference.right_occurrence}`,
      selector: difference.selector,
      outcome: difference.outcome,
    })),
  ).toEqual(expected);
  expect(drawnDifferences(section)).toEqual(shownDifferences(expected.slice(0, 200)));
  await press(user, section.getByRole("button", { name: "Next 200" }));
  expect(await section.findByText(`Differences 201–400 of ${DIFFERENCES}`)).toBeTruthy();
  expect(drawnDifferences(section)).toEqual(shownDifferences(expected.slice(200, 400)));

  // A policy the reader refuses is refused by the preview and the command
  // line in one sentence, and no reading stays beside the refusal.
  for (const [policy, sentence] of [
    ["refused-policy.json", UNSIGNED],
    ["truncated-policy.json", UNDECODABLE],
  ] as const) {
    await preview(user, section, policy);
    expect(await section.findByText(sentence)).toBeTruthy();
    expect(section.queryByText(/ differences · /)).toBeNull();
    const refused = await normalizeWith(policy);
    expect(refused.code).toBe(1);
    expect(refused.stdout).toBe("");
    expect(refused.stderr).toBe(`readmit: ${sentence}\n`);
  }

  // The person opens the colleague's policy into the editor: its rules are
  // the editor's rules, beside the identity of the bytes it read.
  await user.click(section.getByText("Normalization policy", { selector: "summary" }));
  const editor = within(section.getByRole("region", { name: "Normalization policy editor" }));
  const rules = () => editor.queryAllByRole("button", { name: /^Remove policy rule / }).map((button) => button.textContent);
  await user.selectOptions(editor.getByLabelText("Retained policy document"), "colleague-policy.json");
  await press(user, editor.getByRole("button", { name: "Open" }));
  await waitFor(() => expect(rules()).toEqual(["Remove policy rule volatile-message-time", "Remove policy rule analyser-drift"]));
  expect(editor.getByText(`Opened colleague-policy.json · exact bytes hash to ${colleagueDigest}`)).toBeTruthy();

  // They add a rule for the patient's name, from the keyboard.
  await enter(user, editor.getByLabelText("Policy rule ID"), "patient-name");
  await enter(user, editor.getByLabelText("Canonical selector"), "PID-5");
  await user.keyboard("{Enter}");
  expect(rules()).toEqual([
    "Remove policy rule volatile-message-time",
    "Remove policy rule analyser-drift",
    "Remove policy rule patient-name",
  ]);

  // Opening another policy now would replace that unsaved rule: the window
  // asks, and Escape keeps the rules and reads nothing.
  const opens = journey.callsTo("OpenNormalizationPolicy").length;
  await user.selectOptions(editor.getByLabelText("Retained policy document"), "truncated-policy.json");
  await press(user, editor.getByRole("button", { name: "Open" }));
  const question = within(editor.getByRole("group", { name: "Open truncated-policy.json in place of these rules?" }));
  expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep rules" }));
  await user.keyboard("{Escape}");
  expect(editor.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(document.activeElement).toBe(editor.getByRole("button", { name: "Open" }));
  expect(journey.callsTo("OpenNormalizationPolicy")).toHaveLength(opens);
  expect(rules()).toHaveLength(3);

  // Asked again and answered, the undecodable policy is refused in the
  // command line's sentence, and the rules stay as they were.
  await user.keyboard("{Enter}");
  await press(user, editor.getByRole("button", { name: "Replace rules" }));
  expect(await editor.findByText(UNDECODABLE)).toBeTruthy();
  expect(journey.callsTo("OpenNormalizationPolicy")).toHaveLength(opens + 1);
  expect(rules()).toHaveLength(3);

  // Saved as a new entry beside the colleague's, which is unchanged, the
  // extended policy is what the command line reads and what the preview
  // applies: the renamed patients are now hidden too.
  await enter(user, editor.getByLabelText("New normalization-policy entry"), "extended-policy.json");
  await press(user, editor.getByRole("button", { name: "Save as new" }));
  const saved = await editor.findByText(/^Saved to extended-policy\.json · exact bytes hash to /);
  expect(saved.textContent).toBe(`Saved to extended-policy.json · exact bytes hash to ${journey.digest("lab/extended-policy.json")}`);
  expect(journey.digest("lab/colleague-policy.json")).toBe(colleagueDigest);
  const extended = await readUnder("extended-policy.json");
  expect(extended.rules.map((rule) => rule.id)).toEqual(["volatile-message-time", "analyser-drift", "patient-name"]);
  expect(extended.summary).toMatchObject({ differences: DIFFERENCES, suppressed: SUPPRESSED + RENAMED, retained: HIGHER, undecided: PENDING, unaddressed: 0 });
  await preview(user, section, "extended-policy.json");
  expect(await section.findByText(`${DIFFERENCES} differences · ${SUPPRESSED + RENAMED} suppressed · ${HIGHER} retained`)).toBeTruthy();
  const extendedRules = [...COLLEAGUE_RULES, `patient-name ${PAIRED} ${RENAMED} 0 0`];
  expect(reportedRules(extended)).toEqual(extendedRules);
  expect(drawnRules(section)).toEqual(extendedRules);
  expect(drawnDifferences(section)).toEqual(shownDifferences(expectedDifferences(true).slice(0, 200)));
});
