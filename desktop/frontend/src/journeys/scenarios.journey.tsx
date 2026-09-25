// Synthetic scenarios as a person designs, keeps and checks them in the
// window, over the real facade and real files. A scenario is bound to a
// local interface profile — an unsupported one is refused and pins nothing —
// saved, refused an overwrite and reopened; library templates are saved,
// versioned, compared, checked against independent expectations, exported
// and imported; the SIU fixture family is generated from declared inputs;
// and a fixture check is cancelled while it regenerates, from its own control
// and from the keyboard. Every document the window writes is read by the
// command line as the window read it: `readmit scenario preview` previews
// what it saved, `readmit scenario check-library` passes and refuses what
// the window passed and refused in the same words, and `readmit synth` writes
// the same family byte for byte from the same inputs. Every value is
// synthetic.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press, region } from "../testkit/journey";
import { entries, filesUnder, namesIn } from "./probes.js";
import { activateLicense, logTiming } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** The synthetic scenario panel of the open workspace. */
function scenarios() {
  return within(region("Synthetic scenario authoring"));
}

/** The panel's canonical scenario document as it stands. */
function scenarioDocument(): string {
  return (scenarios().getByLabelText("Scenario document") as HTMLTextAreaElement).value;
}

/** The sentence the command line refused with, without its program name. */
function refusal(stderr: string): string {
  return stderr.replace(/^readmit: /, "").trimEnd();
}

/** The command line over the journey's root, admitted by the activation the
 * vendor delivered, for the authoring commands the license gates. */
function licensedCommandLine(args: string[]) {
  return journey.commandLine(["--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json"), ...args]);
}

/** Opens a folder of this machine as the workspace, from the window. */
async function openWorkspace(user: UserEvent, folder: string): Promise<void> {
  await journey.chooseFolder(journey.path(folder), "Open a readmit workspace folder");
  await press(user, screen.getAllByRole("button", { name: "Open workspace…" })[0] as HTMLElement);
  expect(await screen.findByRole("region", { name: "Synthetic scenario authoring" })).toBeTruthy();
}

/** Opens one of the panel's tabs. */
async function openTab(user: UserEvent, name: string): Promise<void> {
  await press(user, scenarios().getByRole("button", { name }));
}

test("a scenario is bound to a local profile, refused an unsupported one, saved, refused an overwrite and reopened in the window, and the command line previews what it saved", async () => {
  const user = userEvent.setup();
  journey.makeFolder("work");
  journey.placeFixture("local-profile.json", "work/siu-profile.json");
  const profile = journey.readFile("work/siu-profile.json");
  journey.writeFile("work/adt-profile.json", profile.replace('"family": "SIU"', '"family": "ADT"'));
  journey.writeFile("work/mdm-profile.json", profile.replace('"family": "SIU"', '"family": "MDM"'));
  journey.placeFixture("scenario-siu.json", "work/designed.json");
  const designed = journey.readFile("work/designed.json");
  journey.writeFile("work/unsupported.json", designed.replace('"event": "S12"', '"event": "A01"'));
  await journey.launch();
  await activateLicense(user, journey);
  await openWorkspace(user, "work");
  const blank = scenarioDocument();

  // A local profile of a family the reader does not know is refused in its
  // words and pins nothing: the document keeps the profile it had.
  await enter(user, scenarios().getByLabelText("Local profile entry"), "mdm-profile.json");
  await press(user, scenarios().getByRole("button", { name: "Use local profile" }));
  expect(await scenarios().findByText("a local profile names one of the message families ADT, SIU, ORM, ORU")).toBeTruthy();
  expect(scenarioDocument()).toBe(blank);

  // An ADT profile pins the ADT lifecycle; the SIU steps the document holds
  // are then refused on save exactly as the command refuses them.
  await enter(user, scenarios().getByLabelText("Local profile entry"), "adt-profile.json");
  await press(user, scenarios().getByRole("button", { name: "Use local profile" }));
  expect(await scenarios().findByText("Pinned fixture-local-siu@1 → readmit-adt-lifecycle-v1 (readmit-scenario-generator-v1)")).toBeTruthy();
  expect(scenarioDocument()).toContain('"profile": "readmit-adt-lifecycle-v1"');
  journey.writeFile("as-bound.json", scenarioDocument());
  const unbound = await journey.commandLine(["scenario", "preview", "as-bound.json"]);
  expect(unbound.code).not.toBe(0);
  await press(user, scenarios().getByRole("button", { name: "Save scenario" }));
  expect(await scenarios().findByText(refusal(unbound.stderr))).toBeTruthy();

  // The SIU profile pins the SIU lifecycle back, and the scenario is saved
  // from the keyboard: Tab from its name reaches Save, and Enter saves.
  await enter(user, scenarios().getByLabelText("Local profile entry"), "siu-profile.json");
  await press(user, scenarios().getByRole("button", { name: "Use local profile" }));
  expect(await scenarios().findByText("Pinned fixture-local-siu@1 → readmit-siu-lifecycle-v1 (readmit-scenario-generator-v1)")).toBeTruthy();
  await enter(user, scenarios().getByLabelText("Save as"), "scenario.json");
  await user.tab();
  expect(document.activeElement).toBe(scenarios().getByRole("button", { name: "Save scenario" }));
  await user.keyboard("{Enter}");
  expect(await scenarios().findByText("Saved siu-draft version 1 (readmit-siu-lifecycle-v1) as scenario.json.")).toBeTruthy();
  const previewed = await journey.commandLine(["scenario", "preview", "work/scenario.json"]);
  expect(previewed.code).toBe(0);
  expect(previewed.stdout).toMatch(/^Scenario: siu-draft \(version 1\)\nProfile: readmit-siu-lifecycle-v1\n/);
  expect(journey.readFile("work/scenario.json")).toBe(scenarioDocument());

  // Saving over it is refused, and the saved document keeps its bytes.
  const saved = journey.digest("work/scenario.json");
  await waitFor(() => expect(document.activeElement).toBe(scenarios().getByRole("button", { name: "Save scenario" })));
  await user.keyboard("{Enter}");
  expect(await scenarios().findByText("cannot create destination; file already exists in workspace")).toBeTruthy();
  expect(journey.digest("work/scenario.json")).toBe(saved);

  // A document the reader refuses does not open, in the command's words,
  // and the panel keeps the document it had.
  const kept = scenarioDocument();
  const unsupported = await journey.commandLine(["scenario", "preview", "work/unsupported.json"]);
  expect(unsupported.code).not.toBe(0);
  await enter(user, scenarios().getByLabelText("Open entry"), "unsupported.json");
  await press(user, scenarios().getByRole("button", { name: "Open scenario" }));
  expect(await scenarios().findByText(refusal(unsupported.stderr))).toBeTruthy();
  expect(scenarioDocument()).toBe(kept);

  // A designed scenario opens in its canonical form and, saved again,
  // previews exactly as the original does.
  await enter(user, scenarios().getByLabelText("Open entry"), "designed.json");
  await press(user, scenarios().getByRole("button", { name: "Open scenario" }));
  expect(await scenarios().findByText("Opened siu-appointment-lifecycle version 1 (readmit-siu-lifecycle-v1) from designed.json.")).toBeTruthy();
  await enter(user, scenarios().getByLabelText("Save as"), "designed-copy.json");
  await press(user, scenarios().getByRole("button", { name: "Save scenario" }));
  expect(await scenarios().findByText("Saved siu-appointment-lifecycle version 1 (readmit-siu-lifecycle-v1) as designed-copy.json.")).toBeTruthy();
  const original = await journey.commandLine(["scenario", "preview", "work/designed.json"]);
  const copy = await journey.commandLine(["scenario", "preview", "work/designed-copy.json"]);
  expect(original.code).toBe(0);
  expect(copy.stdout).toBe(original.stdout);
});

test("library templates are saved, versioned, compared, checked, exported and imported in the window, and scenario check-library reads every library the window wrote as the window did", async () => {
  const user = userEvent.setup();
  journey.makeFolder("work");
  journey.placeFixture("scenario-library.json", "work/shipped-library.json");
  const outside = journey.placeFixture("scenario-library.json", "elsewhere/shipped-library.json");
  journey.placeFixture("scenario-expectations.json", "work/expectations.json");
  const expectations = journey.readFile("work/expectations.json");
  journey.writeFile("work/wrong-expectations.json", expectations.replace("5349555e533132", "5349555e533133"));
  const shipped = JSON.parse(journey.readFile("work/shipped-library.json")) as { templates: { plan: { seed: number } }[] };
  const plan = shipped.templates[0]!.plan;
  journey.writeFile("work/cancel-book-plan.json", JSON.stringify(plan, null, 2));
  journey.writeFile("work/reseeded-plan.json", JSON.stringify({ ...plan, seed: 7 }, null, 2));
  // The digest the independent expectations pin, written by hand beside the
  // shipped library.
  const pinned = (JSON.parse(expectations) as { plan_sha256: string }).plan_sha256;
  await journey.launch();
  await activateLicense(user, journey);
  await openWorkspace(user, "work");
  await openTab(user, "Library");

  // A new library of one template of the shipped plan: the template carries
  // the digest the expectations pin.
  await enter(user, scenarios().getByLabelText("Library entry"), "authored.json");
  await enter(user, scenarios().getByLabelText("Template id"), "siu-cancel-book");
  await enter(user, scenarios().getByLabelText("Template version"), "1");
  await user.selectOptions(scenarios().getByLabelText("Template profile"), "readmit-siu-lifecycle-v1");
  await enter(user, scenarios().getByLabelText("Plan document or entry"), "cancel-book-plan.json");
  await enter(user, scenarios().getByLabelText("Coverage tags, comma-separated"), "cancel-before-book, booking, absent-patient-name");
  await press(user, scenarios().getByRole("button", { name: "Save library entry" }));
  expect(await scenarios().findByText("Saved the template into authored.json; it now holds 1 template.")).toBeTruthy();
  const templates = () => within(scenarios().getByRole("list", { name: "Library templates" }));
  expect(
    templates().getByText(`siu-cancel-book version 1 · readmit-siu-lifecycle-v1 · coverage cancel-before-book, booking, absent-patient-name · plan ${pinned}`),
  ).toBeTruthy();

  // The check passes as the command passes it, and a wrong expectation fails
  // in the command's words.
  await press(user, scenarios().getByRole("button", { name: "Check expectations" }));
  const passed = await journey.commandLine(["scenario", "check-library", "work/authored.json", "work/expectations.json"]);
  expect(passed.code).toBe(0);
  expect(await scenarios().findByText(passed.stdout.trimEnd())).toBeTruthy();
  const wrong = await journey.commandLine(["scenario", "check-library", "work/authored.json", "work/wrong-expectations.json"]);
  expect(wrong.code).not.toBe(0);
  await enter(user, scenarios().getByLabelText("Expectations entry"), "wrong-expectations.json");
  await press(user, scenarios().getByRole("button", { name: "Check expectations" }));
  expect(await scenarios().findByText(refusal(wrong.stderr))).toBeTruthy();
  expect(scenarios().queryByText(/^Fixture checks passed/)).toBeNull();

  // A second revision of the same plan goes into the saved library; another
  // plan under that revision is refused and the library keeps its bytes; a
  // third revision holds the other plan.
  await enter(user, scenarios().getByLabelText("Template version"), "2");
  await press(user, scenarios().getByRole("button", { name: "Save library entry" }));
  expect(await scenarios().findByText("Saved the template into authored.json; it now holds 2 templates.")).toBeTruthy();
  const versioned = journey.digest("work/authored.json");
  await enter(user, scenarios().getByLabelText("Plan document or entry"), "reseeded-plan.json");
  await press(user, scenarios().getByRole("button", { name: "Save library entry" }));
  expect(await scenarios().findByText("cannot overwrite another library revision; bump the template version")).toBeTruthy();
  expect(journey.digest("work/authored.json")).toBe(versioned);
  await enter(user, scenarios().getByLabelText("Template version"), "3");
  await press(user, scenarios().getByRole("button", { name: "Save library entry" }));
  expect(await scenarios().findByText("Saved the template into authored.json; it now holds 3 templates.")).toBeTruthy();

  await enter(user, scenarios().getByLabelText("From version"), "1");
  await enter(user, scenarios().getByLabelText("To version"), "2");
  await press(user, scenarios().getByRole("button", { name: "Compare revisions" }));
  expect(await scenarios().findByText("siu-cancel-book version 1 and version 2: the same plan.")).toBeTruthy();
  await enter(user, scenarios().getByLabelText("To version"), "3");
  await press(user, scenarios().getByRole("button", { name: "Compare revisions" }));
  expect(await scenarios().findByText("siu-cancel-book version 1 and version 3: different plans.")).toBeTruthy();
  expect(within(scenarios().getByLabelText("Revisions 1 and 3")).getByText(pinned)).toBeTruthy();

  // An export is the library's exact bytes and is checked by the command;
  // exporting over it again is refused.
  await enter(user, scenarios().getByLabelText("Export as"), "exported.json");
  await press(user, scenarios().getByRole("button", { name: "Export library" }));
  expect(await scenarios().findByText("Exported authored.json to exported.json, byte for byte.")).toBeTruthy();
  const exported = journey.digest("work/exported.json");
  expect(exported).toBe(journey.digest("work/authored.json"));
  const checkedExport = await journey.commandLine(["scenario", "check-library", "work/exported.json", "work/expectations.json"]);
  expect(checkedExport.stdout).toBe(passed.stdout);
  await press(user, scenarios().getByRole("button", { name: "Export library" }));
  expect(await scenarios().findByText("cannot create destination; file already exists in workspace")).toBeTruthy();
  expect(journey.digest("work/exported.json")).toBe(exported);

  // A library from elsewhere on the machine is imported by its absolute
  // path as its exact bytes, and the command checks the import.
  await enter(user, scenarios().getByLabelText("Library file to import (absolute path)"), outside);
  await enter(user, scenarios().getByLabelText("Import as"), "imported.json");
  await press(user, scenarios().getByRole("button", { name: "Import library" }));
  expect(await scenarios().findByText(`Imported ${outside} as imported.json, byte for byte.`)).toBeTruthy();
  expect(journey.digest("work/imported.json")).toBe(journey.digest("elsewhere/shipped-library.json"));
  const checkedImport = await journey.commandLine(["scenario", "check-library", "work/imported.json", "work/expectations.json"]);
  expect(checkedImport.stdout).toBe(passed.stdout);

  // A library entry the panel never opened is saved as a new library, so an
  // existing entry at that name is refused rather than replaced.
  await enter(user, scenarios().getByLabelText("Library entry"), "imported.json");
  await press(user, scenarios().getByRole("button", { name: "Save library entry" }));
  expect(await scenarios().findByText("cannot create destination; file already exists in workspace")).toBeTruthy();
  expect(journey.digest("work/imported.json")).toBe(journey.digest("elsewhere/shipped-library.json"));
});

test("SIU fixtures generated in the window are the command line's family byte for byte, and a base time the command refuses or an existing family is refused in its words", async () => {
  const user = userEvent.setup();
  journey.makeFolder("work");
  await journey.launch();
  await activateLicense(user, journey);
  await openWorkspace(user, "work");
  await openTab(user, "SIU fixtures");

  const generate = scenarios().getByRole("button", { name: "Generate fixtures" });
  await enter(user, scenarios().getByLabelText("Seed"), "7");
  await enter(user, scenarios().getByLabelText(/^Base time/), "2026-01-02T03:04:05");
  await user.selectOptions(scenarios().getByLabelText("Generator version"), "readmit-synth-v1");
  await user.selectOptions(scenarios().getByLabelText("Profile version"), "readmit-siu-v1");
  await enter(user, scenarios().getByLabelText("Output directory"), "siu-family");

  // A base time without its zone is refused as the command refuses it.
  const zoneless = await licensedCommandLine([
    "synth", "--seed", "7", "--base-time", "2026-01-02T03:04:05", "--generator-version", "readmit-synth-v1",
    "--profile-version", "readmit-siu-v1", "--output", "zoneless-family",
  ]);
  expect(zoneless.code).not.toBe(0);
  await press(user, generate);
  expect(await scenarios().findByText(refusal(zoneless.stderr))).toBeTruthy();
  expect(namesIn(journey.path("work"))).toEqual([]);

  await enter(user, scenarios().getByLabelText(/^Base time/), "2026-01-02T03:04:05Z");
  await press(user, generate);
  const cases = within(await scenarios().findByRole("list", { name: "Written SIU cases" }));
  const command = await licensedCommandLine([
    "synth", "--seed", "7", "--base-time", "2026-01-02T03:04:05Z", "--generator-version", "readmit-synth-v1",
    "--profile-version", "readmit-siu-v1", "--output", "command-family",
  ]);
  expect(command.code).toBe(0);
  // Three case bundles of four metadata files and their payloads each, and
  // the family's completion record: the same paths and the same bytes.
  const written = filesUnder(journey.path("work", "siu-family"));
  expect(written).toEqual(filesUnder(journey.path("command-family")));
  expect(written).toHaveLength(20);
  for (const file of written) {
    expect(journey.digest(`work/siu-family/${file}`)).toBe(journey.digest(`command-family/${file}`));
  }
  // The window names each case bundle by the identity the command prints.
  const printed = [...command.stdout.matchAll(/^(regression|cancellation|invalid): ([0-9a-f]{64})$/gm)];
  expect(printed).toHaveLength(3);
  for (const [, variant, identity] of printed) {
    expect(cases.getByText(new RegExp(`^${variant} · ${identity}`))).toBeTruthy();
  }
  expect(cases.getByText(/^invalid · [0-9a-f]{64} · known defect: /)).toBeTruthy();

  // The same family again is refused in the command's words, and neither
  // family changes.
  const again = await licensedCommandLine([
    "synth", "--seed", "7", "--base-time", "2026-01-02T03:04:05Z", "--generator-version", "readmit-synth-v1",
    "--profile-version", "readmit-siu-v1", "--output", "work/siu-family",
  ]);
  expect(again.code).not.toBe(0);
  const family = journey.digest("work/siu-family/family.json");
  await press(user, generate);
  expect(await scenarios().findByText(refusal(again.stderr))).toBeTruthy();
  expect(scenarios().queryByRole("list", { name: "Written SIU cases" })).toBeNull();
  expect(journey.digest("work/siu-family/family.json")).toBe(family);
  expect(filesUnder(journey.path("work", "siu-family"))).toEqual(written);
});

/** Waits until a fixture check has begun writing its private regeneration:
 * stream files in a readmit-library folder of the temporary folder every
 * process of the journey is given. */
async function regenerating(): Promise<void> {
  await waitFor(
    () => {
      const scratch = namesIn(journey.path("tmp")).filter((name) => name.startsWith("readmit-library-"));
      if (!scratch.some((name) => entries(journey.path("tmp", name, "generation")) > 0)) {
        throw new Error("the check is not regenerating yet");
      }
    },
    { interval: 2, timeout: 30_000 },
  );
}

test("a fixture check cancelled while it regenerates, from its control and from the keyboard, passes nothing and keeps no private streams, and the next check starts afresh", async () => {
  const user = userEvent.setup();
  journey.makeFolder("work");
  journey.placeFixture("scenario-library.json", "work/shipped-library.json");
  journey.placeFixture("scenario-expectations.json", "work/expectations.json");
  const shipped = JSON.parse(journey.readFile("work/shipped-library.json")) as {
    schema: string;
    templates: { id: string; coverage: string[]; plan: { rows: { id: string }[]; variants: { id: string }[] } }[];
  };
  const expectations = journey.readFile("work/expectations.json");
  // The shipped template widened to the most streams a plan holds: eight rows
  // by sixteen variants, each regenerated and synced before anything is
  // compared, so the check is still regenerating when the person cancels it.
  const template = shipped.templates[0]!;
  const plan = template.plan;
  const wide = {
    schema: shipped.schema,
    templates: [
      {
        ...template,
        id: "siu-cancel-book-wide",
        coverage: ["wide"],
        plan: {
          ...plan,
          rows: Array.from({ length: 8 }, (_, i) => ({ ...plan.rows[0]!, id: `row-${i + 1}` })),
          variants: Array.from({ length: 16 }, (_, i) => ({ ...plan.variants[0]!, id: `variant-${i + 1}` })),
        },
      },
    ],
  };
  journey.writeFile("work/wide-library.json", JSON.stringify(wide, null, 2));
  await journey.launch();
  await openWorkspace(user, "work");
  await openTab(user, "Library");

  // The person reads the wide template's plan digest from the window and pins
  // it in their own expectations of it.
  await enter(user, scenarios().getByLabelText("Library entry"), "wide-library.json");
  await press(user, scenarios().getByRole("button", { name: "Open library" }));
  expect(await scenarios().findByText("Opened wide-library.json: 1 template.")).toBeTruthy();
  const listed = within(scenarios().getByRole("list", { name: "Library templates" })).getByText(/^siu-cancel-book-wide version 1 /);
  const digest = /plan ([0-9a-f]{64})$/.exec(listed.textContent ?? "")?.[1] ?? "";
  expect(digest).toMatch(/^[0-9a-f]{64}$/);
  journey.writeFile(
    "work/wide-expectations.json",
    JSON.stringify({ ...(JSON.parse(expectations) as object), template: "siu-cancel-book-wide", plan_sha256: digest }, null, 2),
  );
  await enter(user, scenarios().getByLabelText("Expectations entry"), "wide-expectations.json");

  // Checking writes nothing of the workspace, so it needs no activation; an
  // export is new authoring, denied without one exactly as the command line
  // denies its own authoring on this machine, and nothing is written.
  const unlicensed = await journey.commandLine([
    "synth", "--seed", "0", "--base-time", "2026-01-01T12:00:00Z", "--generator-version", "readmit-synth-v1",
    "--profile-version", "readmit-siu-v1", "--output", "unlicensed-family",
  ]);
  expect(unlicensed.code).not.toBe(0);
  await enter(user, scenarios().getByLabelText("Export as"), "wide-export.json");
  await press(user, scenarios().getByRole("button", { name: "Export library" }));
  expect(await scenarios().findByText(refusal(unlicensed.stderr))).toBeTruthy();
  expect(journey.callsTo("ExportScenarioLibrary")[0]?.result).toMatchObject({ state: "permission_denied" });
  expect(namesIn(journey.path("work"))).not.toContain("wide-export.json");
  const cancelled = "the fixture check was cancelled before it finished; its private regeneration was removed and it passed nothing";

  // From the check's own control.
  await press(user, scenarios().getByRole("button", { name: "Check expectations" }));
  await regenerating();
  let started = performance.now();
  await press(user, scenarios().getByRole("button", { name: "Cancel check" }));
  expect(await scenarios().findByText(cancelled)).toBeTruthy();
  logTiming("fixture check Cancel to cancelled shown", [performance.now() - started]);
  expect(journey.callsTo("Cancel").at(-1)?.args).toEqual(["scenario-check"]);
  expect(namesIn(journey.path("tmp")).filter((name) => name.startsWith("readmit-library-"))).toEqual([]);
  expect(scenarios().queryByText(/^Fixture checks passed/)).toBeNull();

  // From the keyboard: Escape cancels whatever the window is running.
  await press(user, scenarios().getByRole("button", { name: "Check expectations" }));
  await regenerating();
  started = performance.now();
  await user.keyboard("{Escape}");
  await waitFor(() => expect(journey.callsTo("CheckScenarioLibrary").at(-1)?.settled).toBe(true));
  expect(await scenarios().findByText(cancelled)).toBeTruthy();
  logTiming("fixture check Escape to cancelled shown", [performance.now() - started]);
  expect(journey.callsTo("Cancel").at(-1)?.args).toEqual([""]);
  expect(namesIn(journey.path("tmp")).filter((name) => name.startsWith("readmit-library-"))).toEqual([]);

  // Nothing was retained, so the next check starts afresh and passes as the
  // command passes it.
  await enter(user, scenarios().getByLabelText("Library entry"), "shipped-library.json");
  await enter(user, scenarios().getByLabelText("Expectations entry"), "expectations.json");
  await press(user, scenarios().getByRole("button", { name: "Check expectations" }));
  const passed = await journey.commandLine(["scenario", "check-library", "work/shipped-library.json", "work/expectations.json"]);
  expect(passed.code).toBe(0);
  expect(await scenarios().findByText(passed.stdout.trimEnd())).toBeTruthy();
  expect(scenarios().queryByText(cancelled)).toBeNull();
});
