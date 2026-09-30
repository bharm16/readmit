// Previewing and saving sequence changes of a case variant, against the real
// facade over real files.
//
// An interface engineer's export holds a booking and its reschedule. Before a
// reproducer leaves the room, they want its control IDs renamed and its dates
// moved by a day, and they want to see what that would do before anything is
// saved. The relation that matters — each message's control ID, within its
// source — is saved as named link rules from the case's Timeline, and the
// changes are added in Create variant. A shift the engine cannot read is
// refused in its own sentence, which `readmit transform` prints for the same
// step, and the changes on screen stay as they were. The preview lists every
// position the changes rewrite and what they leave alone, and Save writes one
// derived case: the command line's `project show` reads it as a revision the
// transformation made, and `readmit diff` finds exactly the positions the
// preview listed changed, and nothing else.
//
// Every message and value here is synthetic.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { Journey, press } from "../testkit/journey";
import { openCaseFlow, openView, page } from "../testkit/navigation";
import { namesIn } from "./probes.js";
import { EXPORTED_BOOKING, EXPORTED_RESCHEDULE, importExport, licensedProject, pressServed } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const RULES_NAME = "Interface rules";
const SHIFT_REFUSED = "a date shift is a nonzero whole-second duration within ten years, for example 24h or -2h";

/** The changes the variant lists, as each reads. */
function changes(): string[] {
  const list = screen.queryByRole("list", { name: "Changes in order" });
  if (!list) return [];
  return within(list)
    .getAllByRole("listitem")
    .map((item) => [...item.querySelectorAll(".variant-change-text > span")].map((part) => part.textContent ?? "").join(" | "));
}

/** Opens Add change, fills it and presses Add; returns the sheet. */
async function addChange(user: UserEvent, kind: string, fill: (sheet: ReturnType<typeof within>) => Promise<void>) {
  await press(user, page().getByRole("button", { name: "Add change" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Add change" }));
  await user.selectOptions(sheet.getByLabelText("Transformation"), kind);
  await fill(sheet);
  const asked = journey.callsTo("ResolveVariant").length;
  await press(user, sheet.getByRole("button", { name: "Add" }));
  await waitFor(() => expect(journey.callsTo("ResolveVariant")[asked]?.settled).toBe(true));
  return sheet;
}

/** A shift of whole days, later. */
function shiftDays(user: UserEvent, days: string) {
  return async (sheet: ReturnType<typeof within>) => {
    await user.type(sheet.getByLabelText("Amount"), days);
    await user.selectOptions(sheet.getByLabelText("Unit"), "days");
    await user.selectOptions(sheet.getByLabelText("Direction"), "later");
  };
}

test("sequence changes the engine refuses are refused in its words, and the rest are previewed and saved as the one derived case readmit diff and project show read", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  const project = await licensedProject(journey, user);
  const folder = project.slice(journey.path().length + 1);
  const incident = await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule is refused");

  // The relation the renaming keeps — each message's control ID, within its
  // source — saved as named link rules from the case's Timeline.
  await openView(user, "Timeline");
  await press(user, await page().findByRole("button", { name: "More timeline actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Manage link rules" }));
  await press(user, within(await screen.findByRole("dialog", { name: "Link rules" })).getByRole("button", { name: "New link rules" }));
  const editor = within(await screen.findByRole("dialog", { name: "New link rules" }));
  await user.type(editor.getByLabelText("Name"), RULES_NAME);
  await press(user, editor.getByRole("button", { name: "Add rule" }));
  await user.selectOptions(editor.getByLabelText("Match by"), "control-id");
  await user.selectOptions(editor.getByLabelText("Scope"), "source");
  await pressServed(user, journey, editor.getByRole("button", { name: "Save" }), "SaveItem");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "New link rules" })).toBeNull());
  const savedRules = journey.callsTo("SaveItem").at(-1)?.result as { outcome: string };
  expect(savedRules.outcome).toBe("saved");

  // A variant of both messages.
  await openCaseFlow(user, "Create variant");
  const picker = within(await screen.findByRole("dialog", { name: "Included messages" }));
  await user.click(await picker.findByRole("checkbox", { name: "1. SIU · S12" }));
  await user.click(picker.getByRole("checkbox", { name: "2. SIU · S13" }));
  await press(user, picker.getByRole("button", { name: "Apply" }));
  await screen.findByRole("table", { name: "Included messages" });

  // Control IDs renamed under the saved rule.
  await addChange(user, "rebase-identifiers/v1", async (sheet) => {
    await user.selectOptions(sheet.getByLabelText("Link rules"), RULES_NAME);
    await sheet.findByRole("option", { name: "rule-1" });
    await user.selectOptions(sheet.getByLabelText("Rule"), "rule-1");
  });
  await waitFor(() => expect(changes()).toEqual(["Rebase identifiers | All messages · rule-1"]));

  // A shift of four thousand days is past what the engine reads: refused in
  // its sentence, the sheet stays open and the changes are as they were.
  const refused = await addChange(user, "shift-dates/v1", shiftDays(user, "4000"));
  expect(await refused.findByText(SHIFT_REFUSED)).toBeTruthy();
  expect(journey.callsTo("ResolveVariant").at(-1)?.result).toMatchObject({ state: "failed", reason: SHIFT_REFUSED });
  await press(user, refused.getByRole("button", { name: "Cancel" }));
  expect(changes()).toEqual(["Rebase identifiers | All messages · rule-1"]);

  // The command line refuses the same step in the same words.
  journey.writeFile(
    `${folder}/rules.json`,
    JSON.stringify({ schema: "readmit-correlation-rules/v1", rules: [{ id: "rule-1", operator: "control-id", scope: "source" }] }),
  );
  journey.writeFile(
    `${folder}/refused.plan.json`,
    JSON.stringify({
      schema: "readmit-transform-plan/v1",
      case: journey.readFile(`${folder}/${incident}/identity.sha256`).trim(),
      rules: "b".repeat(64),
      steps: [{ operator: "shift-dates/v1", shift: "96000h" }],
    }),
  );
  const refusedByCommand = await journey.commandLine([
    "transform", `${project}/${incident}`, "--rules", `${project}/rules.json`, "--plan", `${project}/refused.plan.json`, "--format", "json",
  ]);
  expect(refusedByCommand.code).toBe(1);
  expect(refusedByCommand.stdout).toBe("");
  expect(refusedByCommand.stderr).toBe(`readmit: ${SHIFT_REFUSED}\n`);

  // A day later is accepted.
  await addChange(user, "shift-dates/v1", shiftDays(user, "1"));
  await waitFor(() => expect(changes()).toEqual(["Rebase identifiers | All messages · rule-1", "Shift dates | All messages | 1 day later"]));
  const resolved = journey.callsTo("ResolveVariant").at(-1)?.result as {
    variant: { changes: { operator: string; occurrence: string; selector: string }[]; notes: { code: string }[] };
  };
  const positions = resolved.variant.changes.map((change) => `${change.occurrence} ${change.selector}`).sort();
  expect(positions).toEqual(
    [
      "s0001-e000001 MSH[1]-10[1]", "s0002-e000001 MSH[1]-10[1]", "s0001-e000001 MSH[1]-7[1]", "s0002-e000001 MSH[1]-7[1]",
      "s0001-e000001 SCH[1]-11[1].4", "s0002-e000001 SCH[1]-11[1].4",
    ].sort(),
  );
  // What is left alone is stated: the date fields a shift does not read, and
  // the version and family no pinned pack verified.
  expect(resolved.variant.notes.map((note) => note.code)).toEqual(["unshifted-positions", "unverified-combination"]);

  // The preview lists each rewritten position, and what is left alone.
  await press(user, page().getByRole("button", { name: "Preview" }));
  const fields = within(await screen.findByRole("table", { name: "Changed fields" }));
  expect(fields.getAllByRole("row").slice(1)).toHaveLength(6);
  expect(screen.getByText("Only MSH-7 and SCH-11 start and end move")).toBeTruthy();
  expect(screen.getByText("Profile does not verify this message type")).toBeTruthy();

  // Saved, the variant is one derived case the project records as a
  // revision the transformation made from the imported case.
  await press(user, page().getByRole("button", { name: "Save variant" }));
  await waitFor(() => expect((journey.callsTo("SaveItem").at(-1)?.result as { outcome?: string } | undefined)?.outcome).toBe("saved"));
  const shown = await journey.commandLine(["project", "show", project]);
  expect(shown.code, shown.stderr).toBe(0);
  const lineage = new RegExp(`\\n {2}(\\S+) evidence=verified identity=([0-9a-f]{64}) [^\\n]*\\n {4}operation=readmit-transform/v1 parent=${incident} `).exec(shown.stdout);
  const written = lineage?.[1] ?? "";
  expect(namesIn(project)).toContain(written);
  expect(lineage?.[2]).toBe(journey.readFile(`${folder}/${written}/identity.sha256`).trim());

  // Compared by message type, the derived case differs from the imported
  // one in exactly the fields holding the positions the preview listed.
  const diffed = await journey.commandLine(["diff", `${project}/${incident}`, `${project}/${written}`, "--key", "MSH-9", "--format", "json"]);
  expect(diffed.code, diffed.stderr).toBe(0);
  const report = JSON.parse(diffed.stdout) as { pairs: { left: { occurrence: string }; fields: { selector: string }[] }[] };
  expect(report.pairs.flatMap((pair) => pair.fields.map((field) => `${pair.left.occurrence} ${field.selector}`)).sort()).toEqual(
    positions.map((position) => position.replace(/\.\d+$/, "")),
  );

  // Nothing here wrote into the imported case.
  const timeline = await journey.commandLine(["timeline", `${project}/${incident}`]);
  expect(timeline.code).toBe(0);
});
